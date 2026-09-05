package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type legacyRouteGuardCacheSpy struct {
	APIKeyCache
	reads int
}

func (s *legacyRouteGuardCacheSpy) GetAPIKeyRoutingGuards(context.Context, int64) (APIKeyRoutingGuards, error) {
	s.reads++
	return APIKeyRoutingGuards{}, errors.New("routing Redis unavailable")
}

func TestAPIKeyRoutingLegacyAuthAvoidsRoutingIO(t *testing.T) {
	for _, tc := range []struct {
		name string
		enabled, allowed, multiple bool
		wantReads int
	}{
		{"disabled_single", false, false, false, 0},
		{"disabled_multi", false, true, true, 0},
		{"unlisted_single", true, false, false, 0},
		{"listed_single", true, true, false, 0},
		{"withdrawn_multi", true, false, true, 0},
		{"listed_multi", true, true, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &legacyRouteGuardCacheSpy{}
			settingsRepo := &routingRolloutRepoStub{}
			if tc.allowed { settingsRepo.value = `{"user_ids":[7]}` }
			cfg := &config.Config{Gateway: config.GatewayConfig{APIKeyMultiGroupRoutingEnabled: tc.enabled}}
			svc := &APIKeyService{cache: cache, cfg: cfg, routingRolloutSettings: NewSettingService(settingsRepo, cfg)}
			entry := &APIKeyAuthCacheEntry{Snapshot: &APIKeyAuthSnapshot{APIKeyID: 9, UserID: 7, RouteVersion: 1, RoutingDependencyVersion: 1,
				GroupRoutes: []APIKeyAuthGroupRouteSnapshot{{GroupID: 11, Enabled: true}}}}
			if tc.multiple { entry.Snapshot.GroupRoutes = append(entry.Snapshot.GroupRoutes, APIKeyAuthGroupRouteSnapshot{GroupID: 12, Priority: 1, Enabled: true}) }
			require.True(t, svc.authCacheRouteVersionCurrent(context.Background(), "digest", entry))
			require.Equal(t, tc.wantReads, cache.reads)
			if !tc.enabled || !tc.multiple { require.Zero(t, settingsRepo.reads, "legacy auth must not query rollout settings") }
		})
	}
}

func TestAPIKeyRoutingSingleGroupSnapshotSkipsCandidateRateQuery(t *testing.T) {
	repo := &candidateRateAuthRepoStub{rates: map[int64]float64{50: .7}}
	svc := &APIKeyService{userGroupRateRepo: repo}
	key := profitAuthTestAPIKey()
	key.GroupRoutes = []APIKeyGroupRoute{{GroupID: 50, Group: key.Group, Enabled: true}, {GroupID: 51, Priority: 1, Enabled: false}}
	snapshot := svc.snapshotFromAPIKey(context.Background(), key)
	require.NotNil(t, snapshot)
	require.Empty(t, repo.requestedID)
	require.Empty(t, snapshot.User.GroupRates)
}
