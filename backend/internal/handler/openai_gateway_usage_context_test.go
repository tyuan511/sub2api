package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSubmitUsageRecordTaskCopiesRequestContext(t *testing.T) {
	parent := context.WithValue(context.Background(), ctxkey.ClientRequestID, "client-request-123")
	parent = context.WithValue(parent, ctxkey.RequestID, "request-456")

	var gotClientRequestID string
	var gotRequestID string
	h := &GatewayHandler{}
	h.submitUsageRecordTask(parent, func(ctx context.Context) {
		gotClientRequestID, _ = ctx.Value(ctxkey.ClientRequestID).(string)
		gotRequestID, _ = ctx.Value(ctxkey.RequestID).(string)
	})

	require.Equal(t, "client-request-123", gotClientRequestID)
	require.Equal(t, "request-456", gotRequestID)
}

func TestOpenAISubmitUsageRecordTaskCopiesRequestContext(t *testing.T) {
	parent := context.WithValue(context.Background(), ctxkey.ClientRequestID, "openai-client-request-123")
	parent = context.WithValue(parent, ctxkey.RequestID, "openai-request-456")

	var gotClientRequestID string
	var gotRequestID string
	h := &OpenAIGatewayHandler{}
	h.submitUsageRecordTask(parent, func(ctx context.Context) {
		gotClientRequestID, _ = ctx.Value(ctxkey.ClientRequestID).(string)
		gotRequestID, _ = ctx.Value(ctxkey.RequestID).(string)
	})

	require.Equal(t, "openai-client-request-123", gotClientRequestID)
	require.Equal(t, "openai-request-456", gotRequestID)
}

func TestUsageRecordContextCopiesGroupCacheCompensationState(t *testing.T) {
	parent := service.WithAPIKeyRoutingUsageContext(context.Background(), service.APIKeyRoutingUsageContext{
		DecisionID: "decision-detached", APIKeyID: 7, RouteVersion: 3,
		InitialGroupID: 10, EffectiveGroupID: 20,
		ScheduleMode: service.APIKeyScheduleModeSequential,
		StickyBroken: true, SwitchCount: 1,
		CacheCompensationMaxTokens: 50_000, CacheCompensationMaxSwitches: 1,
	})
	parent = service.WithForceCacheBilling(parent)
	parent = service.WithAPIKeyGroupCacheCompensation(parent)

	detached := usageRecordContext(parent, context.Background())
	require.True(t, service.IsForceCacheBilling(detached))
	require.True(t, service.IsAPIKeyGroupCacheCompensation(detached))
	meta, ok := service.APIKeyRoutingUsageContextFromContext(detached)
	require.True(t, ok)
	require.Equal(t, 50_000, meta.CacheCompensationMaxTokens)
	require.Equal(t, 1, meta.CacheCompensationMaxSwitches)
}
