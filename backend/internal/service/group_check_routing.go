package service

import "context"

type groupCheckPinnedGroupKey struct{}

// WithGroupCheckPinnedGroup prevents the ordinary gateway's group-level fallback
// from changing the billed group. Only internal callers can set this context.
func WithGroupCheckPinnedGroup(ctx context.Context, groupID int64) context.Context {
	return context.WithValue(ctx, groupCheckPinnedGroupKey{}, groupID)
}

func GroupCheckPinnedGroup(ctx context.Context) int64 {
	id, _ := ctx.Value(groupCheckPinnedGroupKey{}).(int64)
	return id
}
