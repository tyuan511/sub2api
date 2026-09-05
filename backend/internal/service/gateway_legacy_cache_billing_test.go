//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGatewayLegacyForceCacheBillingBooleanRemainsAuthoritative(t *testing.T) {
	for _, tc := range []struct {
		name string
		contextFlag, groupCompensation bool
		wantInput, wantCache int
	}{
		{"legacy_detached_context", false, false, 0, 1000},
		{"legacy_context_flag", true, false, 0, 1000},
		{"group_compensation_stays_bounded", true, true, 750, 250},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &openAIRecordUsageLogRepoStub{}
			svc := newGatewayRecordUsageServiceForTest(repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
			ctx := context.Background()
			if tc.contextFlag { ctx = WithForceCacheBilling(ctx) }
			if tc.groupCompensation {
				ctx = WithAPIKeyGroupCacheCompensation(ctx)
				ctx = WithAPIKeyRoutingUsageContext(ctx, APIKeyRoutingUsageContext{DecisionID: "bounded", RouteVersion: 1, StickyBroken: true, SwitchCount: 1, CacheCompensationMaxTokens: 250})
			}
			err := svc.RecordUsage(ctx, &RecordUsageInput{
				Result: &ForwardResult{RequestID: tc.name, Model: "claude-sonnet-4", Duration: time.Second, Usage: ClaudeUsage{InputTokens: 1000, OutputTokens: 10}},
				APIKey: &APIKey{ID: 501}, User: &User{ID: 601}, Account: &Account{ID: 701}, ForceCacheBilling: true,
			})
			require.NoError(t, err)
			require.NotNil(t, repo.lastLog)
			require.Equal(t, tc.wantInput, repo.lastLog.InputTokens)
			require.Equal(t, tc.wantCache, repo.lastLog.CacheReadTokens)
			if !tc.groupCompensation { require.InDelta(t, .000495, repo.lastLog.ActualCost, 1e-12) }
		})
	}
}
