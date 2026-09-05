package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrStudioStorage = infraerrors.BadRequest("IMAGE_STUDIO_STORAGE", "请先在设置的数据备份中配置并启用异步生图对象存储")

type ImageStudioService struct {
	repo        ImageStudioRepository
	encryptor   SecretEncryptor
	factory     ImageStorageFactory
	settings    *ImageStorageSettingService
	syncMu      sync.Mutex
	legacyTasks ImageTaskStore
	fixedKey    bool
}

func NewImageStudioService(repo ImageStudioRepository, encryptor SecretEncryptor, factory ImageStorageFactory, settings *ImageStorageSettingService, legacyTasks ImageTaskStore, cfg *config.Config) *ImageStudioService {
	return &ImageStudioService{repo: repo, encryptor: encryptor, factory: factory, settings: settings, legacyTasks: legacyTasks, fixedKey: cfg.Totp.EncryptionKeyConfigured}
}

// The existing async image settings are the only editable configuration.
// Retained snapshots are just file-location metadata: changing the shared
// settings affects new tasks without rewriting credentials used by old files.
func (s *ImageStudioService) syncStorage(ctx context.Context) error {
	if s.settings == nil {
		return nil
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	cfg, err := s.settings.effectiveConfig(ctx)
	if err != nil {
		return err
	}
	state, err := s.repo.StorageState(ctx)
	if err != nil {
		return err
	}
	if !cfg.Active() {
		if state.Enabled {
			return s.repo.ActivateStorage(ctx, state.ActiveID, false)
		}
		return nil
	}
	wanted := *settingsFromConfig(*cfg)
	normalizeImageStorageSettings(&wanted)
	profiles, err := s.repo.ListStorage(ctx)
	if err != nil {
		return err
	}
	for _, profile := range profiles {
		candidate := profile.Config
		candidate.SecretAccessKey, err = s.encryptor.Decrypt(candidate.SecretAccessKey)
		if err != nil {
			continue
		}
		normalizeImageStorageSettings(&candidate)
		candidate.Enabled, candidate.ReuseBackupS3 = true, false
		if candidate != wanted {
			continue
		}
		if state.ActiveID != profile.ID || !state.Enabled {
			return s.repo.ActivateStorage(ctx, profile.ID, true)
		}
		return nil
	}
	if !s.fixedKey {
		return ErrSecretEncryptionKeyNotConfigured
	}
	wanted.SecretAccessKey, err = s.encryptor.Encrypt(wanted.SecretAccessKey)
	if err != nil {
		return err
	}
	return s.repo.AddStorage(ctx, &StudioStorageProfile{Name: "异步生图存储", Config: wanted}, false)
}
func (s *ImageStudioService) StorageSettings(ctx context.Context) (StudioStorageState, []StudioStorageProfile, error) {
	if err := s.syncStorage(ctx); err != nil {
		return StudioStorageState{}, nil, err
	}
	state, err := s.repo.StorageState(ctx)
	if err != nil {
		return state, nil, err
	}
	profiles, err := s.repo.ListStorage(ctx)
	for i := range profiles {
		profiles[i].SecretConfigured = profiles[i].Config.SecretAccessKey != ""
		profiles[i].Config.SecretAccessKey = ""
	}
	return state, profiles, err
}
func (s *ImageStudioService) objectStorage(ctx context.Context, p *StudioStorageProfile) (StudioObjectStorage, error) {
	cfg := p.Config
	secret, err := s.encryptor.Decrypt(cfg.SecretAccessKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt studio storage credentials: %w", err)
	}
	storage, err := s.factory(ctx, &config.ImageStorageConfig{Enabled: true, Endpoint: cfg.Endpoint, Region: cfg.Region, Bucket: cfg.Bucket, Prefix: cfg.Prefix, PublicBaseURL: cfg.PublicBaseURL, AccessKeyID: cfg.AccessKeyID, SecretAccessKey: secret, ForcePathStyle: cfg.ForcePathStyle, PresignExpiry: cfg.PresignExpiry, MaxDownloadByte: cfg.MaxDownloadBytes})
	if err != nil {
		return nil, err
	}
	result, ok := storage.(StudioObjectStorage)
	if !ok {
		return nil, ErrStudioStorage
	}
	return result, nil
}
func (s *ImageStudioService) activeStorage(ctx context.Context) (*StudioStorageProfile, StudioObjectStorage, error) {
	if err := s.syncStorage(ctx); err != nil {
		return nil, nil, err
	}
	state, err := s.repo.StorageState(ctx)
	if err != nil {
		return nil, nil, err
	}
	if !state.Enabled || state.ActiveID <= 0 {
		return nil, nil, ErrStudioStorage
	}
	p, err := s.repo.GetStorage(ctx, state.ActiveID)
	if err != nil {
		return nil, nil, err
	}
	storage, err := s.objectStorage(ctx, p)
	return p, storage, err
}
func (s *ImageStudioService) Available(ctx context.Context) bool {
	_, _, err := s.activeStorage(ctx)
	return err == nil
}
func studioDigest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func (s *ImageStudioService) MigrateStorage(ctx context.Context, from, to int64) (int, int64, error) {
	if err := s.syncStorage(ctx); err != nil {
		return 0, 0, err
	}
	if from == to || from <= 0 || to <= 0 {
		return 0, 0, ErrStudioStorage
	}
	state, err := s.repo.StorageState(ctx)
	if err != nil {
		return 0, 0, err
	}
	if from == state.ActiveID || to != state.ActiveID {
		return 0, 0, infraerrors.Conflict("IMAGE_STORAGE_ACTIVE", "请先切换到新的生图存储，再迁移原存储的历史图片")
	}
	source, err := s.repo.GetStorage(ctx, from)
	if err != nil {
		return 0, 0, err
	}
	target, err := s.repo.GetStorage(ctx, to)
	if err != nil {
		return 0, 0, err
	}
	reader, err := s.objectStorage(ctx, source)
	if err != nil {
		return 0, 0, err
	}
	writer, err := s.objectStorage(ctx, target)
	if err != nil {
		return 0, 0, err
	}
	files, total, err := s.repo.StorageFiles(ctx, from, 3)
	if err != nil {
		return 0, 0, err
	}
	moved := 0
	for _, f := range files {
		key := target.Config.Prefix + "assets/" + f.ID + studioExtension(f.ContentType)
		err := s.repo.MoveFile(ctx, f.ID, from, to, key, func(file StudioFile) error {
			data, err := reader.Read(ctx, file.ObjectKey, 64*1024*1024)
			if err != nil {
				return err
			}
			if studioDigest(data) != file.SHA256 {
				return fmt.Errorf("source image checksum mismatch")
			}
			if _, err = writer.Save(ctx, key, file.ContentType, data); err != nil {
				return err
			}
			check, err := writer.Read(ctx, key, 64*1024*1024)
			if err != nil {
				return err
			}
			if studioDigest(check) != file.SHA256 {
				return fmt.Errorf("copied image checksum mismatch")
			}
			if file.ThumbnailReady {
				thumbnail, err := reader.Read(ctx, StudioThumbnailKey(file.ObjectKey), 8*1024*1024)
				if err != nil {
					return err
				}
				if _, err = writer.Save(ctx, StudioThumbnailKey(key), http.DetectContentType(thumbnail), thumbnail); err != nil {
					return err
				}
				copied, err := writer.Read(ctx, StudioThumbnailKey(key), 8*1024*1024)
				if err != nil {
					return err
				}
				if studioDigest(copied) != studioDigest(thumbnail) {
					return fmt.Errorf("copied thumbnail checksum mismatch")
				}
			}
			return nil
		})
		if err != nil {
			return moved, total - int64(moved), err
		}
		moved++
	}
	return moved, total - int64(moved), nil
}
