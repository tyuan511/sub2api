package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
)

type studioTaskStore struct {
	repo      ImageStudioRepository
	meta      *StudioMetadata
	storageID int64
}

func (s *studioTaskStore) Save(ctx context.Context, t *ImageTaskRecord, _ time.Duration) error {
	meta := s.meta
	s.meta = nil
	return s.repo.SaveCreation(ctx, t, meta, s.storageID)
}
func (s *studioTaskStore) Get(ctx context.Context, id string) (*ImageTaskRecord, error) {
	r, err := s.repo.GetCreation(ctx, id, 0)
	if err != nil {
		return nil, err
	}
	return &r.Task, nil
}

type studioAssetWriter struct {
	service    *ImageStudioService
	profile    *StudioStorageProfile
	storage    StudioObjectStorage
	creationID string
	position   int
}

func (w *studioAssetWriter) Save(ctx context.Context, key, mime string, data []byte) (string, error) {
	return w.SaveAsset(ctx, key, mime, data)
}
func (w *studioAssetWriter) SaveAsset(ctx context.Context, _ string, mime string, data []byte) (string, error) {
	file, err := w.service.saveFile(ctx, w.profile, w.storage, w.creationID, "output", w.position, "", mime, data)
	if err != nil {
		return "", err
	}
	w.position++
	return file.ID, nil
}
func studioExtension(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}
func (s *ImageStudioService) saveFile(ctx context.Context, profile *StudioStorageProfile, storage StudioObjectStorage, creationID, kind string, position int, name, mime string, data []byte) (*StudioFile, error) {
	detected := http.DetectContentType(data)
	if detected != "image/png" && detected != "image/jpeg" && detected != "image/webp" {
		return nil, infraerrors.BadRequest("INVALID_IMAGE", "仅支持 PNG、JPEG 和 WebP 图片")
	}
	if int64(len(data)) > profile.Config.MaxDownloadBytes {
		return nil, infraerrors.BadRequest("IMAGE_TOO_LARGE", "图片超出存储大小上限")
	}
	mime = detected
	id := uuid.NewSHA1(uuid.NameSpaceURL, []byte(creationID+"/"+kind+fmt.Sprint(position))).String()
	key := profile.Config.Prefix + "assets/" + id + studioExtension(mime)
	if _, err := storage.Save(ctx, key, mime, data); err != nil {
		return nil, err
	}
	if name == "" {
		name = id + studioExtension(mime)
	}
	name = filepath.Base(name)
	if len(name) > 255 {
		name = id + studioExtension(mime)
	}
	file := &StudioFile{ID: id, CreationID: creationID, StorageID: profile.ID, ObjectKey: key, Kind: kind, Position: position, Filename: name, ContentType: mime, Size: int64(len(data)), SHA256: studioDigest(data)}
	// A derivative failure must not discard a successfully generated original.
	file.ThumbnailReady = saveStudioThumbnail(ctx, storage, key, data) == nil
	if err := s.repo.AddFile(ctx, file); err != nil {
		return nil, err
	}
	return file, nil
}
func (s *ImageStudioService) Start(ctx context.Context, owner ImageTaskOwner, meta StudioMetadata, refs []OpenAIImagesUpload) (*ImageTask, *ImageTaskService, error) {
	profile, storage, err := s.activeStorage(ctx)
	if err != nil {
		return nil, nil, err
	}
	taskStore := &studioTaskStore{repo: s.repo, meta: &meta, storageID: profile.ID}
	writer := &studioAssetWriter{service: s, profile: profile, storage: storage}
	tasks := NewImageTaskServiceWithUploader(taskStore, NewImageResultUploader(writer, profile.Config.Prefix, profile.Config.MaxDownloadBytes, nil), 24*time.Hour, 30*time.Minute)
	task, err := tasks.Create(ctx, owner)
	if err != nil {
		return nil, nil, err
	}
	writer.creationID = task.ID
	for i, ref := range refs {
		if _, err = s.saveFile(ctx, profile, storage, task.ID, "reference", i, ref.FileName, ref.ContentType, ref.Data); err != nil {
			_ = tasks.Fail(context.WithoutCancel(ctx), task.ID, http.StatusBadGateway, imageTaskErrorJSON("storage_error", "参考图保存失败，未提交生图请求"))
			return nil, nil, infraerrors.BadRequest("REFERENCE_STORAGE_FAILED", "参考图保存失败，未提交生图请求").WithCause(err)
		}
	}
	return task, tasks, nil
}
func (s *ImageStudioService) File(ctx context.Context, id string, userID int64) (*StudioFile, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrImageTaskNotFound
	}
	file, err := s.repo.GetFile(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	profile, err := s.repo.GetStorage(ctx, file.StorageID)
	if err != nil {
		return nil, err
	}
	storage, err := s.objectStorage(ctx, profile)
	if err != nil {
		return nil, err
	}
	file.URL, err = storage.URL(ctx, file.ObjectKey)
	if err == nil && file.ThumbnailReady {
		file.ThumbnailURL, _ = storage.URL(ctx, StudioThumbnailKey(file.ObjectKey))
	}
	return file, err
}
func (s *ImageStudioService) creationView(ctx context.Context, r *StudioRecord) (*StudioCreation, error) {
	out := &StudioCreation{ID: r.Task.ID, StudioMetadata: r.Metadata, KeyID: r.Task.APIKeyID, CreatedAt: r.Task.CreatedAt * 1000, Status: r.Task.Status, Images: []StudioFile{}, References: []StudioFile{}}
	if out.Status == ImageTaskStatusProcessing && time.Since(time.Unix(r.Task.CreatedAt, 0)) > 35*time.Minute {
		out.Status = ImageTaskStatusFailed
		out.Error = "任务执行已中断或超时，请查看用量记录后再决定是否重新生成"
	}
	if len(r.Task.Error) > 0 {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(r.Task.Error, &e)
		out.Error = e.Message
	}
	files, err := s.repo.ListFiles(ctx, r.Task.ID)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []struct {
			ID     string `json:"image_id"`
			Prompt string `json:"revised_prompt"`
		} `json:"data"`
	}
	_ = json.Unmarshal(r.Task.Result, &result)
	storages := map[int64]StudioObjectStorage{}
	for _, file := range files {
		if file.Kind == "output" && out.Status != ImageTaskStatusCompleted {
			continue
		}
		storage, ok := storages[file.StorageID]
		if !ok {
			p, e := s.repo.GetStorage(ctx, file.StorageID)
			if e == nil {
				storage, e = s.objectStorage(ctx, p)
			}
			if e == nil {
				storages[file.StorageID] = storage
			}
		}
		// A broken storage configuration must not hide or delete the history itself.
		if storage != nil {
			file.URL, _ = storage.URL(ctx, file.ObjectKey)
			if file.ThumbnailReady {
				file.ThumbnailURL, _ = storage.URL(ctx, StudioThumbnailKey(file.ObjectKey))
			}
		}
		for _, image := range result.Data {
			if image.ID == file.ID {
				file.RevisedPrompt = image.Prompt
			}
		}
		if file.Kind == "reference" {
			out.References = append(out.References, file)
		} else {
			out.Images = append(out.Images, file)
		}
	}
	return out, nil
}
func (s *ImageStudioService) Get(ctx context.Context, id string, userID int64) (*StudioCreation, error) {
	r, err := s.repo.GetCreation(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	return s.creationView(ctx, r)
}
func (s *ImageStudioService) List(ctx context.Context, userID int64, page int) ([]StudioCreation, bool, error) {
	records, err := s.repo.ListCreations(ctx, userID, (page-1)*20, 21)
	if err != nil {
		return nil, false, err
	}
	more := len(records) > 20
	if more {
		records = records[:20]
	}
	out := []StudioCreation{}
	for i := range records {
		view, err := s.creationView(ctx, &records[i])
		if err != nil {
			return nil, false, err
		}
		out = append(out, *view)
	}
	return out, more, nil
}
func (s *ImageStudioService) Delete(ctx context.Context, id string, userID int64) error {
	profiles, err := s.repo.ListStorage(ctx)
	if err != nil {
		return err
	}
	byID := make(map[int64]*StudioStorageProfile, len(profiles))
	for i := range profiles {
		byID[profiles[i].ID] = &profiles[i]
	}
	return s.repo.DeleteCreation(ctx, id, userID, func(files []StudioFile) error {
		storages := map[int64]StudioObjectStorage{}
		for _, file := range files {
			locations := append(file.Locations, StudioFileLocation{StorageID: file.StorageID, ObjectKey: file.ObjectKey})
			seen := map[StudioFileLocation]bool{}
			for _, location := range locations {
				if seen[location] {
					continue
				}
				seen[location] = true
				storage := storages[location.StorageID]
				if storage == nil {
					profile := byID[location.StorageID]
					if profile == nil {
						return ErrStudioStorage
					}
					var err error
					storage, err = s.objectStorage(ctx, profile)
					if err != nil {
						return err
					}
					storages[location.StorageID] = storage
				}
				for _, key := range []string{StudioThumbnailKey(location.ObjectKey), location.ObjectKey} {
					if err := storage.Delete(ctx, key); err != nil {
						return infraerrors.New(http.StatusBadGateway, "IMAGE_DELETE_FAILED", "图片清理未完成，历史记录已保留，请稍后重试删除").WithCause(err)
					}
				}
			}
		}
		return nil
	})
}

// Import accepts image bytes supplied by the signed-in browser. It never fetches
// arbitrary client URLs or assigns another user's existing object key.
func (s *ImageStudioService) Import(ctx context.Context, userID int64, meta StudioMetadata, status, message string, createdAt int64, refs, outputs []OpenAIImagesUpload) (*StudioCreation, error) {
	if strings.TrimSpace(meta.LegacyID) == "" || len(meta.LegacyID) > 100 {
		return nil, infraerrors.BadRequest("INVALID_HISTORY_ID", "无效的历史记录 ID")
	}
	existing, err := s.repo.FindLegacy(ctx, userID, meta.LegacyID)
	if err != nil && !errors.Is(err, ErrImageTaskNotFound) {
		return nil, err
	}
	var taskID string
	var tasks *ImageTaskService
	var profile *StudioStorageProfile
	var storage StudioObjectStorage
	if existing != nil {
		if existing.Task.Status == ImageTaskStatusCompleted {
			return s.creationView(ctx, existing)
		}
		taskID = existing.Task.ID
		profile, err = s.repo.GetStorage(ctx, existing.StorageID)
		if err != nil {
			return nil, err
		}
		storage, err = s.objectStorage(ctx, profile)
		if err != nil {
			return nil, err
		}
		tasks = NewImageTaskServiceWithOptions(&studioTaskStore{repo: s.repo}, 24*time.Hour, time.Minute)
	} else {
		profile, storage, err = s.activeStorage(ctx)
		if err != nil {
			return nil, err
		}
		tasks = NewImageTaskServiceWithOptions(&studioTaskStore{repo: s.repo, meta: &meta, storageID: profile.ID}, 24*time.Hour, time.Minute)
		task, err := tasks.Create(ctx, ImageTaskOwner{UserID: userID, APIKeyID: 0})
		if err != nil {
			return nil, err
		}
		taskID = task.ID
		if createdAt > 0 && createdAt <= time.Now().UnixMilli() {
			record, getErr := s.repo.GetCreation(ctx, taskID, userID)
			if getErr != nil {
				return nil, getErr
			}
			record.Task.CreatedAt = createdAt / 1000
			if err = s.repo.SaveCreation(ctx, &record.Task, nil, 0); err != nil {
				return nil, err
			}
		}
	}
	persistFailed := func(err error) (*StudioCreation, error) {
		_ = tasks.Fail(context.WithoutCancel(ctx), taskID, 502, imageTaskErrorJSON("import_failed", "旧记录导入未完成，可重试"))
		return nil, err
	}
	for i, file := range refs {
		if _, err = s.saveFile(ctx, profile, storage, taskID, "reference", i, file.FileName, file.ContentType, file.Data); err != nil {
			return persistFailed(err)
		}
	}
	result := []map[string]string{}
	for i, file := range outputs {
		asset, err := s.saveFile(ctx, profile, storage, taskID, "output", i, file.FileName, file.ContentType, file.Data)
		if err != nil {
			return persistFailed(err)
		}
		result = append(result, map[string]string{"image_id": asset.ID})
	}
	if status == ImageTaskStatusCompleted && len(result) > 0 {
		body, _ := json.Marshal(map[string]any{"data": result})
		err = tasks.Complete(ctx, taskID, 200, body)
	} else {
		if message == "" {
			message = "旧任务结果无法确认，请查看用量记录"
		}
		err = tasks.Fail(ctx, taskID, 400, imageTaskErrorJSON("legacy_task", message))
	}
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, taskID, userID)
}
