package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type apiKeyRouteStickyCacheStub struct {
	GatewayCache
	values  map[string]int64
	lastNS  int64
	lastKey string
	lastTTL time.Duration
}

func (s *apiKeyRouteStickyCacheStub) GetSessionAccountID(_ context.Context, namespace int64, key string) (int64, error) {
	s.lastNS, s.lastKey = namespace, key
	value, ok := s.values[key]
	if !ok {
		return 0, ErrStickySessionNotFound
	}
	return value, nil
}

func (s *apiKeyRouteStickyCacheStub) SetSessionAccountID(_ context.Context, namespace int64, key string, value int64, ttl time.Duration) error {
	s.lastNS, s.lastKey, s.lastTTL = namespace, key, ttl
	if s.values == nil {
		s.values = make(map[string]int64)
	}
	s.values[key] = value
	return nil
}

func TestAPIKeyGroupStickyIsVersionIsolatedAndClusterTagged(t *testing.T) {
	cache := &apiKeyRouteStickyCacheStub{values: make(map[string]int64)}
	ctx := context.Background()
	require.NoError(t, bindAPIKeyGroupSticky(ctx, cache, 91, 3, "gpt-5", "responses", "session-a", 17, 12*time.Minute))
	require.Equal(t, int64(91), cache.lastNS)
	require.Contains(t, cache.lastKey, "api_key_group:{91}:v3:")
	require.Equal(t, 12*time.Minute, cache.lastTTL)

	groupID, err := getAPIKeyGroupSticky(ctx, cache, 91, 3, "gpt-5", "responses", "session-a")
	require.NoError(t, err)
	require.Equal(t, int64(17), groupID)

	groupID, err = getAPIKeyGroupSticky(ctx, cache, 91, 4, "gpt-5", "responses", "session-a")
	require.NoError(t, err)
	require.Zero(t, groupID, "a prior route_version must never influence the new route set")
}

func TestAPIKeyGroupStickyKeyDoesNotExposeSessionValue(t *testing.T) {
	key := apiKeyGroupStickyCacheKey(5, 2, "gpt-5", "responses", "customer-sensitive-session-id")
	require.NotContains(t, key, "customer-sensitive")
	require.NotContains(t, key, "gpt-5")
	require.NotContains(t, key, "responses")
	require.Equal(t, key, apiKeyGroupStickyCacheKey(5, 2, "gpt-5", "responses", "customer-sensitive-session-id"))
	require.NotEqual(t, key, apiKeyGroupStickyCacheKey(5, 2, "gpt-5", "responses", "another-session"))
	require.NotEqual(t, key, apiKeyGroupStickyCacheKey(5, 2, "gpt-4", "responses", "customer-sensitive-session-id"))
	require.NotEqual(t, key, apiKeyGroupStickyCacheKey(5, 2, "gpt-5", "messages", "customer-sensitive-session-id"))
}

func TestAPIKeyGroupStickyDefaultsToOneHour(t *testing.T) {
	require.Equal(t, time.Hour, apiKeyGroupStickyTTL(0))
}

func TestAPIKeyGroupStickySurvivesControlEditsButNotRouteSetEdits(t *testing.T) {
	cache := &apiKeyRouteStickyCacheStub{}
	plan := &APIKeyRoutePlan{APIKeyID: 91, RouteVersion: 3, RoutingStateVersion: 3, RoutingEnabled: true,
		Candidates: []APIKeyRouteCandidate{{GroupID: 17}}}
	ctx := WithAPIKeyRouteRequestRuntimeState(context.Background(), plan)
	require.NoError(t, bindAPIKeyGroupSticky(ctx, cache, 91, 3, "gpt-5", "responses", "session-a", 17, time.Hour))
	plan.RouteVersion = 4
	plan.RoutingMinSuccessRate = 85
	ctx = WithAPIKeyRouteRequestRuntimeState(context.Background(), plan)
	group, err := getAPIKeyGroupSticky(ctx, cache, 91, 4, "gpt-5", "responses", "session-a")
	require.NoError(t, err)
	require.Equal(t, int64(17), group)
	plan.RouteVersion = 5
	plan.RoutingStateVersion = 4
	ctx = WithAPIKeyRouteRequestRuntimeState(context.Background(), plan)
	group, err = getAPIKeyGroupSticky(ctx, cache, 91, 5, "gpt-5", "responses", "session-a")
	require.NoError(t, err)
	require.Zero(t, group)
}
