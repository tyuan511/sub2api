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
		name        string
		contextFlag bool
		wantInput   int
		wantCache   int
	}{
		{"legacy_detached_context", false, 0, 1000},
		{"legacy_context_flag", true, 0, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &openAIRecordUsageLogRepoStub{}
			svc := newGatewayRecordUsageServiceForTest(repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
			ctx := context.Background()
			if tc.contextFlag {
				ctx = WithForceCacheBilling(ctx)
			}
			groupID := int64(11)
			err := svc.RecordUsage(ctx, &RecordUsageInput{
				Result: &ForwardResult{RequestID: tc.name, Model: "claude-sonnet-4", Duration: time.Second, Usage: ClaudeUsage{InputTokens: 1000, OutputTokens: 10}},
				APIKey: &APIKey{ID: 501, GroupID: &groupID, Group: &Group{ID: groupID, RateMultiplier: 1.1}},
				User:   &User{ID: 601}, Account: &Account{ID: 701}, ForceCacheBilling: true,
			})
			require.NoError(t, err)
			require.NotNil(t, repo.lastLog)
			require.Equal(t, tc.wantInput, repo.lastLog.InputTokens)
			require.Equal(t, tc.wantCache, repo.lastLog.CacheReadTokens)
			require.InDelta(t, .000495, repo.lastLog.ActualCost, 1e-12)
			require.Nil(t, repo.lastLog.AccountStatsCost, "legacy failover must not introduce a new account cost override")
			require.Empty(t, repo.lastLog.ActualUsage)
			require.Empty(t, repo.lastLog.BillableUsage)
		})
	}
}

func TestOpenAILegacyRoutingForceFlagDoesNotChangeBilling(t *testing.T) {
	var baseline *UsageLog
	for _, forceFlag := range []bool{false, true} {
		repo := &openAIRecordUsageLogRepoStub{}
		svc := newOpenAIRecordUsageServiceForTest(repo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
		ctx := context.Background()
		if forceFlag {
			ctx = WithForceCacheBilling(ctx)
		}
		groupID := int64(11)
		err := svc.RecordUsage(ctx, &OpenAIRecordUsageInput{
			Result: &OpenAIForwardResult{RequestID: "legacy-openai", Model: "gpt-5.1", Duration: time.Second,
				Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 20, CacheReadInputTokens: 100}},
			APIKey: &APIKey{ID: 501, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformOpenAI, RateMultiplier: 1}},
			User:   &User{ID: 601}, Account: &Account{ID: 701, Platform: PlatformOpenAI},
		})
		require.NoError(t, err)
		require.NotNil(t, repo.lastLog)
		require.Empty(t, repo.lastLog.ActualUsage)
		require.Empty(t, repo.lastLog.BillableUsage)
		require.Nil(t, repo.lastLog.AccountStatsCost)
		if !forceFlag {
			baseline = repo.lastLog
		} else {
			require.Equal(t, baseline.TotalCost, repo.lastLog.TotalCost)
			require.Equal(t, baseline.ActualCost, repo.lastLog.ActualCost)
			require.Equal(t, baseline.InputTokens, repo.lastLog.InputTokens)
			require.Equal(t, baseline.CacheReadTokens, repo.lastLog.CacheReadTokens)
		}
	}
}
