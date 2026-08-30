package repository

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type localSupportAttachmentStore struct{ root string }

func (s *localSupportAttachmentStore) path(key string) (string, error) {
	clean := filepath.Clean(strings.TrimLeft(key, "/"))
	if clean == "." || clean == "" || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("invalid attachment key")
	}
	full := filepath.Join(s.root, clean)
	rel, err := filepath.Rel(s.root, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("attachment key escapes storage root")
	}
	return full, nil
}

func (s *localSupportAttachmentStore) Put(_ context.Context, key, _ string, data []byte) error {
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create attachment directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return fmt.Errorf("create temporary attachment: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write attachment: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close attachment: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("commit attachment: %w", err)
	}
	return nil
}

func (s *localSupportAttachmentStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	path, err := s.path(key)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (s *localSupportAttachmentStore) Delete(_ context.Context, key string) error {
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

type s3SupportAttachmentStore struct {
	client *s3.Client
	bucket string
}

func (s *s3SupportAttachmentStore) Put(ctx context.Context, key, contentType string, data []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &s.bucket, Key: &key, Body: bytes.NewReader(data), ContentType: &contentType,
	})
	return err
}

func (s *s3SupportAttachmentStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}

func (s *s3SupportAttachmentStore) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &s.bucket, Key: &key})
	return err
}

// ProvideSupportAttachmentStore uses the configured S3-compatible image bucket
// when available, otherwise it falls back to a private persistent data folder.
func ProvideSupportAttachmentStore(cfg *config.Config) (service.SupportAttachmentStore, error) {
	if cfg != nil && cfg.ImageStorage.Active() {
		client, err := newS3Client(context.Background(), s3ClientParams{
			Endpoint: cfg.ImageStorage.Endpoint, Region: cfg.ImageStorage.Region,
			AccessKeyID: cfg.ImageStorage.AccessKeyID, SecretAccessKey: cfg.ImageStorage.SecretAccessKey,
			ForcePathStyle: cfg.ImageStorage.ForcePathStyle,
		})
		if err != nil {
			return nil, err
		}
		return &s3SupportAttachmentStore{client: client, bucket: cfg.ImageStorage.Bucket}, nil
	}
	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir == "" {
		dataDir = "."
		if info, err := os.Stat("/app/data"); err == nil && info.IsDir() {
			dataDir = "/app/data"
		}
	}
	root := filepath.Join(dataDir, "support-attachments")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create support attachment storage: %w", err)
	}
	return &localSupportAttachmentStore{root: root}, nil
}
