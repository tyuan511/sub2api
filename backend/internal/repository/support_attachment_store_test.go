package repository

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocalSupportAttachmentStoreRoundTripAndTraversalProtection(t *testing.T) {
	store := &localSupportAttachmentStore{root: t.TempDir()}
	ctx := context.Background()
	require.NoError(t, store.Put(ctx, "support/2026/image.png", "image/png", []byte("private")))
	body, err := store.Open(ctx, "support/2026/image.png")
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	require.Equal(t, []byte("private"), data)

	_, err = store.Open(ctx, "../../outside")
	require.Error(t, err)
	require.Error(t, store.Put(ctx, "/../../outside", "image/png", []byte("x")))
}
