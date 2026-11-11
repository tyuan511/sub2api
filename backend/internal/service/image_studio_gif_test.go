package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
	"golang.org/x/image/webp"
)

// encodeAnimatedGIF 生成两帧动图 GIF，模拟 gpt-image 的动图（GIF）输出。
func encodeAnimatedGIF(t *testing.T, w, h int) []byte {
	t.Helper()
	palette := color.Palette{
		color.RGBA{250, 240, 220, 255},
		color.RGBA{200, 120, 40, 255},
	}
	frame := image.NewPaletted(image.Rect(0, 0, w, h), palette)
	var buf bytes.Buffer
	require.NoError(t, gif.EncodeAll(&buf, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{10, 10}}))
	return buf.Bytes()
}

type studioSaveStubRepo struct {
	ImageStudioRepository
	added *StudioFile
}

func (r *studioSaveStubRepo) AddFile(_ context.Context, f *StudioFile) error { r.added = f; return nil }

type studioSaveStubStorage struct {
	savedKeys []string
}

func (s *studioSaveStubStorage) Save(_ context.Context, key, _ string, _ []byte) (string, error) {
	s.savedKeys = append(s.savedKeys, key)
	return "https://s3.test/" + key, nil
}
func (s *studioSaveStubStorage) URL(_ context.Context, key string) (string, error) {
	return "https://s3.test/" + key, nil
}
func (s *studioSaveStubStorage) Read(_ context.Context, _ string, _ int64) ([]byte, error) {
	return nil, errors.New("unused in this test")
}
func (s *studioSaveStubStorage) Delete(_ context.Context, _ string) error { return nil }

func TestStudioSaveFileAcceptsAnimatedGIF(t *testing.T) {
	svc := &ImageStudioService{repo: &studioSaveStubRepo{}}
	storage := &studioSaveStubStorage{}
	profile := &StudioStorageProfile{ID: 1, Config: ImageStorageSettings{Prefix: "images/", MaxDownloadBytes: 32 << 20}}

	data := encodeAnimatedGIF(t, 64, 32)
	file, err := svc.saveFile(context.Background(), profile, storage, "task-1", "output", 0, "", "image/gif", data)
	require.NoError(t, err)
	require.Equal(t, "image/gif", file.ContentType)
	require.Equal(t, int64(len(data)), file.Size)
	require.True(t, file.ThumbnailReady, "animated GIF thumbnail should be generated from the first frame")
	require.Contains(t, file.Filename, ".gif")
	require.Contains(t, file.ObjectKey, ".gif")
	require.Len(t, storage.savedKeys, 2, "original asset plus thumbnail")
	require.Contains(t, storage.savedKeys[1], ".thumb-v1.webp")
}

func TestStudioThumbnailAnimatedGIF(t *testing.T) {
	data := encodeAnimatedGIF(t, 64, 32)
	thumb, mime, err := makeStudioThumbnail(context.Background(), data)
	require.NoError(t, err)
	require.Equal(t, "image/webp", mime)
	decoded, err := webp.Decode(bytes.NewReader(thumb))
	require.NoError(t, err)
	require.Equal(t, image.Rect(0, 0, 64, 32), decoded.Bounds())
}

func TestStudioSaveFileStillRejectsNonImages(t *testing.T) {
	svc := &ImageStudioService{repo: &studioSaveStubRepo{}}
	storage := &studioSaveStubStorage{}
	profile := &StudioStorageProfile{ID: 1, Config: ImageStorageSettings{Prefix: "images/", MaxDownloadBytes: 32 << 20}}

	_, err := svc.saveFile(context.Background(), profile, storage, "task-1", "output", 0, "", "image/png", []byte("not an image"))
	require.Error(t, err)
	require.Equal(t, "INVALID_IMAGE", infraerrors.Reason(err))
}

func TestStudioExtension(t *testing.T) {
	require.Equal(t, ".png", studioExtension("image/png"))
	require.Equal(t, ".jpg", studioExtension("image/jpeg"))
	require.Equal(t, ".webp", studioExtension("image/webp"))
	require.Equal(t, ".gif", studioExtension("image/gif"))
	require.Equal(t, ".png", studioExtension("video/mp4"))
}
