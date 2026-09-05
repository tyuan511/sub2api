package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/gen2brain/webp"
	"github.com/google/uuid"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const studioThumbnailEdge = 768

// Limit decoded image memory, including concurrent legacy backfills.
var studioThumbnailSlots = make(chan struct{}, 2)
var studioBackfillSlots = make(chan struct{}, 2)

// Deterministic keys let deletion clean up even an interrupted thumbnail upload.
func StudioThumbnailKey(original string) string { return original + ".thumb-v1.webp" }

func makeStudioThumbnail(ctx context.Context, data []byte) ([]byte, string, error) {
	select {
	case studioThumbnailSlots <- struct{}{}:
		defer func() { <-studioThumbnailSlots }()
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 32*1024*1024 {
		return nil, "", fmt.Errorf("image dimensions exceed thumbnail limit")
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	w, h := cfg.Width, cfg.Height
	if longest := max(w, h); longest > studioThumbnailEdge {
		w = max(1, w*studioThumbnailEdge/longest)
		h = max(1, h*studioThumbnailEdge/longest)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	var out bytes.Buffer
	err = webp.Encode(&out, dst, webp.Options{Quality: 78, Method: 4})
	return out.Bytes(), "image/webp", err
}

func saveStudioThumbnail(ctx context.Context, storage StudioObjectStorage, key string, data []byte) error {
	thumb, mime, err := makeStudioThumbnail(ctx, data)
	if err != nil {
		return err
	}
	_, err = storage.Save(ctx, StudioThumbnailKey(key), mime, thumb)
	return err
}

// Legacy images are read only after an authenticated user views their thumbnail.
// The repository serializes this against other backfills, migration and deletion.
func (s *ImageStudioService) Thumbnail(ctx context.Context, id string, userID int64) (*StudioFile, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrImageTaskNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	file, err := s.repo.GetFile(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if file.ThumbnailReady {
		return s.File(ctx, id, userID)
	}
	select {
	case studioBackfillSlots <- struct{}{}:
		defer func() { <-studioBackfillSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	profile, err := s.repo.GetStorage(ctx, file.StorageID)
	if err != nil {
		return nil, err
	}
	storage, err := s.objectStorage(ctx, profile)
	if err != nil {
		return nil, err
	}
	_, err = s.repo.EnsureThumbnail(ctx, id, userID, func(locked StudioFile) error {
		if locked.StorageID != file.StorageID || locked.ObjectKey != file.ObjectKey || locked.SHA256 != file.SHA256 {
			return fmt.Errorf("image storage changed; retry thumbnail request")
		}
		data, err := storage.Read(ctx, file.ObjectKey, 64*1024*1024)
		if err != nil {
			return err
		}
		if studioDigest(data) != file.SHA256 {
			return fmt.Errorf("source image checksum mismatch")
		}
		return saveStudioThumbnail(ctx, storage, file.ObjectKey, data)
	})
	if err != nil {
		if errors.Is(err, ErrImageTaskNotFound) {
			return nil, err
		}
		return nil, infraerrors.New(502, "IMAGE_THUMBNAIL_FAILED", "缩略图暂时不可用，请稍后重试").WithCause(err)
	}
	return s.File(ctx, id, userID)
}
