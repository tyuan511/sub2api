package service

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type memorySupportAttachmentStore struct{ data map[string][]byte }

func (s *memorySupportAttachmentStore) Put(_ context.Context, key, _ string, data []byte) error {
	if s.data == nil {
		s.data = map[string][]byte{}
	}
	s.data[key] = append([]byte(nil), data...)
	return nil
}
func (s *memorySupportAttachmentStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.data[key])), nil
}
func (s *memorySupportAttachmentStore) Delete(_ context.Context, key string) error {
	delete(s.data, key)
	return nil
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 3))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var out bytes.Buffer
	require.NoError(t, png.Encode(&out, img))
	return out.Bytes()
}

func TestValidateSupportCreate(t *testing.T) {
	valid := SupportCreateInput{Content: " details "}
	require.NoError(t, validateSupportCreate(&valid))
	require.Equal(t, "details", valid.Content)
	require.Error(t, validateSupportCreate(&SupportCreateInput{}))
}

func TestSupportMessagePreview(t *testing.T) {
	if got := supportMessagePreview("  第一行\n\n第二行  "); got != "第一行 第二行" {
		t.Fatalf("unexpected normalized preview: %q", got)
	}
	if got := supportMessagePreview("  \n "); got != "[图片]" {
		t.Fatalf("unexpected image preview: %q", got)
	}
	long := strings.Repeat("长", 121)
	if got := supportMessagePreview(long); got != strings.Repeat("长", 120)+"..." {
		t.Fatalf("unexpected truncated preview length: %d", len([]rune(got)))
	}
}

func TestPrepareSupportAttachmentsValidatesRealImage(t *testing.T) {
	store := &memorySupportAttachmentStore{}
	svc := NewSupportService(nil, nil, store)
	data := testPNG(t)
	items, err := inspectSupportAttachments([]SupportUpload{{Name: "screen.png", Data: data}})
	require.NoError(t, err)
	require.NoError(t, svc.storePreparedAttachments(context.Background(), items))
	require.Len(t, items, 1)
	require.Equal(t, int64(len(data)), items[0].size)
	require.Equal(t, 2, items[0].width)
	require.Equal(t, 3, items[0].height)
	require.Equal(t, "image/png", items[0].ContentType)
	require.Equal(t, data, store.data[items[0].key])
}

func TestPrepareSupportAttachmentsRejectsSpoofedAndExcessFiles(t *testing.T) {
	store := &memorySupportAttachmentStore{}
	_, err := inspectSupportAttachments([]SupportUpload{{Name: "fake.png", ContentType: "image/png", Data: []byte("not an image")}})
	require.Error(t, err)
	require.Empty(t, store.data)

	uploads := make([]SupportUpload, SupportMaxAttachments+1)
	_, err = inspectSupportAttachments(uploads)
	require.Error(t, err)
}

func TestInspectSupportAttachmentsRejectsOversizedImage(t *testing.T) {
	_, err := inspectSupportAttachments([]SupportUpload{{Name: "large.png", Data: make([]byte, SupportMaxAttachmentBytes+1)}})
	require.Error(t, err)
}
