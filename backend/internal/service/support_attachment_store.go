package service

import (
	"context"
	"io"
)

// SupportAttachmentStore keeps ticket images private. Objects are only exposed
// through authenticated download endpoints.
type SupportAttachmentStore interface {
	Put(ctx context.Context, key, contentType string, data []byte) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}
