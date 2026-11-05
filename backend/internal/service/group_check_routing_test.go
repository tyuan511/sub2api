package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestGroupCheckPinnedGroupRejectsFallback(t *testing.T) {
	groupID, fallbackID := int64(10), int64(11)
	group := &Group{ID: groupID, Platform: PlatformAnthropic, Status: StatusActive, Hydrated: true, ClaudeCodeOnly: true, FallbackGroupID: &fallbackID}
	svc := &GatewayService{}
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)
	ctx = WithGroupCheckPinnedGroup(ctx, groupID)
	_, _, err := svc.resolveGatewayGroup(ctx, &groupID)
	require.ErrorIs(t, err, ErrClaudeCodeOnly)
	_, _, err = svc.resolveGatewayGroup(ctx, &fallbackID)
	require.ErrorContains(t, err, "group mismatch")
}
