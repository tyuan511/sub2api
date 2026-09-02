package repository

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestFilterSchedulerCredentialsKeepsSubscriptionPlanType(t *testing.T) {
	filtered := filterSchedulerCredentials(map[string]any{
		"plan_type":     "plus",
		"access_token":  "secret-access-token",
		"refresh_token": "secret-refresh-token",
	})

	require.Equal(t, "plus", filtered["plan_type"])
	require.NotContains(t, filtered, "access_token")
	require.NotContains(t, filtered, "refresh_token")
}

func TestSchedulerMetadataAccountKeepsOpenAISubscriptionIdentity(t *testing.T) {
	account := service.Account{
		ID:       24,
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeOAuth,
		Credentials: map[string]any{
			"plan_type":    "plus",
			"access_token": "secret-access-token",
		},
	}

	metadata := buildSchedulerMetadataAccount(account)

	require.True(t, metadata.IsOpenAIChatGPTSubscription())
	require.Empty(t, metadata.GetCredential("access_token"))
}

func TestSchedulerMetadataAccountKeepsCompleteProxyPool(t *testing.T) {
	firstID := int64(101)
	secondID := int64(202)
	account := service.Account{
		ID:      24,
		ProxyID: &firstID,
		Proxy:   &service.Proxy{ID: firstID, Host: "first.example"},
		ProxyPool: []service.AccountProxyConfig{
			{ProxyID: firstID, Concurrency: 2, Position: 0, Proxy: &service.Proxy{ID: firstID, Host: "first.example"}},
			{ProxyID: secondID, Concurrency: 5, Position: 1, Proxy: &service.Proxy{ID: secondID, Host: "second.example"}},
		},
	}

	metadata := buildSchedulerMetadataAccount(account)

	require.NotNil(t, metadata.ProxyID)
	require.Equal(t, firstID, *metadata.ProxyID)
	require.NotNil(t, metadata.Proxy)
	require.Equal(t, firstID, metadata.Proxy.ID)
	require.Len(t, metadata.ProxyPool, 2)
	require.Equal(t, []int64{firstID, secondID}, []int64{metadata.ProxyPool[0].ProxyID, metadata.ProxyPool[1].ProxyID})
	require.Equal(t, []int{2, 5}, []int{metadata.ProxyPool[0].Concurrency, metadata.ProxyPool[1].Concurrency})
	require.Equal(t, []int{0, 1}, []int{metadata.ProxyPool[0].Position, metadata.ProxyPool[1].Position})
	require.Equal(t, "second.example", metadata.ProxyPool[1].Proxy.Host)

	_, metaPayload, err := marshalSchedulerCacheAccount(account)
	require.NoError(t, err)
	var decoded service.Account
	require.NoError(t, json.Unmarshal(metaPayload, &decoded))
	require.Len(t, decoded.ProxyPool, 2, "the Redis scheduler metadata payload must carry the full pool")
	require.Equal(t, secondID, decoded.ProxyPool[1].ProxyID)
	require.Equal(t, 5, decoded.ProxyPool[1].Concurrency)
}

func TestSchedulerMetadataAccountProjectsUpstreamBillingProbe(t *testing.T) {
	lastError := strings.Repeat("upstream diagnostic ", 512)
	probe := map[string]any{
		"status": "ok",
		"data": map[string]any{
			"billing_scope":             "token",
			"resolved_rate_multiplier":  0.03,
			"peak_rate_enabled":         true,
			"peak_start":                "09:00",
			"peak_end":                  "18:00",
			"peak_rate_multiplier":      2.0,
			"timezone":                  "Asia/Shanghai",
			"effective_rate_multiplier": 0.03,
			"remote_diagnostic":         lastError,
		},
		"received_at":   "2026-07-13T10:00:00Z",
		"fresh_until":   "2026-07-13T11:00:00Z",
		"next_probe_at": "2026-07-13T10:30:00Z",
		"http_status":   502,
		"last_error":    lastError,
	}
	account := service.Account{
		ID: 42,
		Extra: map[string]any{
			"upstream_billing_probe": probe,
			"unused_large_field":     "drop-me",
		},
	}

	metadata := buildSchedulerMetadataAccount(account)
	fullPayload, metaPayload, err := marshalSchedulerCacheAccount(account)
	require.NoError(t, err)

	filtered, ok := metadata.Extra["upstream_billing_probe"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "ok", filtered["status"])
	require.Equal(t, "2026-07-13T10:00:00Z", filtered["received_at"])
	require.Equal(t, "2026-07-13T11:00:00Z", filtered["fresh_until"])
	require.Equal(t, "2026-07-13T10:30:00Z", filtered["next_probe_at"])
	require.NotContains(t, filtered, "http_status")
	require.NotContains(t, filtered, "last_error")
	filteredData, ok := filtered["data"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "token", filteredData["billing_scope"])
	require.Equal(t, 0.03, filteredData["resolved_rate_multiplier"])
	require.Equal(t, true, filteredData["peak_rate_enabled"])
	require.Equal(t, "09:00", filteredData["peak_start"])
	require.Equal(t, "18:00", filteredData["peak_end"])
	require.Equal(t, 2.0, filteredData["peak_rate_multiplier"])
	require.Equal(t, "Asia/Shanghai", filteredData["timezone"])
	require.NotContains(t, filteredData, "effective_rate_multiplier")
	require.NotContains(t, filteredData, "remote_diagnostic")
	require.NotContains(t, metadata.Extra, "unused_large_field")
	require.Contains(t, string(fullPayload), lastError)
	require.NotContains(t, string(metaPayload), "last_error")
	require.Less(t, len(metaPayload)*4, len(fullPayload))
}

func TestSchedulerMetadataAccountDropsInvalidUpstreamBillingProbe(t *testing.T) {
	for _, probe := range []any{
		"invalid",
		map[string]any{},
		map[string]any{"status": ""},
	} {
		metadata := buildSchedulerMetadataAccount(service.Account{
			Extra: map[string]any{service.UpstreamBillingProbeExtraKey: probe},
		})

		require.NotContains(t, metadata.Extra, service.UpstreamBillingProbeExtraKey)
	}
}
