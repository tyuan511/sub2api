//go:build unit

package repository

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestS3ImageStorage_SaveBrowserCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPut, r.Method)
		require.Equal(t, "/images/images/asset.png", r.URL.Path)
		require.Equal(t, "private, max-age=86400, immutable", r.Header.Get("Cache-Control"))
		require.Equal(t, "image/png", r.Header.Get("Content-Type"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, "image-content", string(body))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	storage, err := NewS3ImageStorage(context.Background(), &config.ImageStorageConfig{
		Endpoint: server.URL, Bucket: "images", Region: "auto", AccessKeyID: "test", SecretAccessKey: "test",
		ForcePathStyle: true, PublicBaseURL: "https://blob.example.test/",
	})
	require.NoError(t, err)
	url, err := storage.Save(context.Background(), "images/asset.png", "image/png", []byte("image-content"))
	require.NoError(t, err)
	require.Equal(t, "https://blob.example.test/images/asset.png", url)
}

func TestS3ImageStorage_SignedURLCacheWithinExpiry(t *testing.T) {
	storage, err := NewS3ImageStorage(context.Background(), &config.ImageStorageConfig{
		Endpoint: "https://s3.example.test", Bucket: "images", Region: "auto", AccessKeyID: "test", SecretAccessKey: "test",
		ForcePathStyle: true, PresignExpiry: 1,
	})
	require.NoError(t, err)
	signed, err := storage.URL(context.Background(), "images/asset.png")
	require.NoError(t, err)
	parsed, err := url.Parse(signed)
	require.NoError(t, err)
	require.Equal(t, "3600", parsed.Query().Get("X-Amz-Expires"))
	require.Equal(t, "private, max-age=3540, immutable", parsed.Query().Get("response-cache-control"))
}
