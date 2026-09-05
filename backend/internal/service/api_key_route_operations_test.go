package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type routeOperationsAPIKeyRepoStub struct {
	APIKeyRepository
	key *APIKey
}

func (r *routeOperationsAPIKeyRepoStub) GetByID(context.Context, int64) (*APIKey, error) {
	return r.key, nil
}

type routeOperationsGatewayCacheStub struct {
	GatewayCache
	stickyGroup     int64
	deletedSticky   []string
	breakerStates   []APIKeyRouteBreakerSnapshot
	deletedBreakers []string
	readCalls       int
}

func (c *routeOperationsGatewayCacheStub) GetSessionAccountID(context.Context, int64, string) (int64, error) {
	c.readCalls++
	if c.stickyGroup <= 0 {
		return 0, ErrStickySessionNotFound
	}
	return c.stickyGroup, nil
}

func (c *routeOperationsGatewayCacheStub) DeleteSessionAccountID(_ context.Context, _ int64, sessionHash string) error {
	c.deletedSticky = append(c.deletedSticky, sessionHash)
	return nil
}

func (c *routeOperationsGatewayCacheStub) LoadAPIKeyRouteBreakers(_ context.Context, keys []string) ([]APIKeyRouteBreakerSnapshot, error) {
	c.readCalls++
	if len(c.breakerStates) == len(keys) {
		return append([]APIKeyRouteBreakerSnapshot(nil), c.breakerStates...), nil
	}
	return make([]APIKeyRouteBreakerSnapshot, len(keys)), nil
}

func (c *routeOperationsGatewayCacheStub) DeleteAPIKeyRouteBreakers(_ context.Context, keys []string) (int64, error) {
	c.deletedBreakers = append(c.deletedBreakers, keys...)
	return int64(len(keys) * 2), nil
}

func testRouteOperationsAPIKey() *APIKey {
	group1 := &Group{ID: 11, Name: "one", Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard}
	group2 := &Group{ID: 12, Name: "two", Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard}
	groupID := group1.ID
	return &APIKey{
		ID: 7, UserID: 5, GroupID: &groupID, Group: group1, RouteVersion: 4,
		ScheduleMode: APIKeyScheduleModeSequential, Status: StatusActive,
		User: &User{ID: 5, Status: StatusActive},
		GroupRoutes: []APIKeyGroupRoute{
			{GroupID: group1.ID, Priority: 0, Enabled: true, Group: group1},
			{GroupID: group2.ID, Priority: 1, Enabled: true, Group: group2},
		},
	}
}

func TestAPIKeyRouteOperationsExplainIsReadOnlyAndVersionScoped(t *testing.T) {
	key := testRouteOperationsAPIKey()
	cache := &routeOperationsGatewayCacheStub{
		stickyGroup: key.GroupRoutes[1].GroupID,
		breakerStates: []APIKeyRouteBreakerSnapshot{
			{State: APIKeyRouteBreakerClosed, Successes: 8},
			{State: APIKeyRouteBreakerOpen, Failures: 7},
		},
	}
	apiKeys := &APIKeyService{apiKeyRepo: &routeOperationsAPIKeyRepoStub{key: key}}
	allowRoutingUsersForTest(t, apiKeys, key.UserID)
	operations := NewAPIKeyRouteOperationsService(apiKeys, cache)

	explanation, err := operations.Explain(context.Background(), key.ID, "gpt-5", "responses", "session-digest")
	require.NoError(t, err)
	require.Equal(t, key.RouteVersion, explanation.RouteVersion)
	require.Equal(t, &cache.stickyGroup, explanation.StickyGroupID)
	require.Len(t, explanation.Candidates, 2)
	require.True(t, explanation.Candidates[0].Admitted)
	require.False(t, explanation.Candidates[1].Admitted)
	require.Equal(t, "breaker_open", explanation.Candidates[1].ExclusionReason)
}

func TestAPIKeyRouteOperationsSingleGroupExplanationIgnoresDormantControls(t *testing.T) {
	key := testRouteOperationsAPIKey()
	key.GroupRoutes = key.GroupRoutes[:1]
	preference := APIKeySmartPreferencePrice
	key.ScheduleMode, key.SmartPreference, key.RoutingMinSuccessRate = APIKeyScheduleModeSmart, &preference, 95
	cache := &routeOperationsGatewayCacheStub{stickyGroup: 12,
		breakerStates: []APIKeyRouteBreakerSnapshot{{State: APIKeyRouteBreakerOpen, Failures: 100}}}
	operations := NewAPIKeyRouteOperationsService(&APIKeyService{apiKeyRepo: &routeOperationsAPIKeyRepoStub{key: key}}, cache)
	explanation, err := operations.Explain(context.Background(), key.ID, "gpt-5", "responses", "old-session")
	require.NoError(t, err)
	require.False(t, explanation.RoutingEnabled)
	require.Equal(t, APIKeyScheduleModeSequential, explanation.ScheduleMode)
	require.Len(t, explanation.Candidates, 1)
	require.True(t, explanation.Candidates[0].Admitted)
	require.Nil(t, explanation.Candidates[0].Score)
	require.Nil(t, explanation.StickyGroupID)
	require.Zero(t, cache.readCalls)
}

func TestAPIKeyRouteOperationsClearRequiresCurrentVersionAndConfiguredGroup(t *testing.T) {
	key := testRouteOperationsAPIKey()
	cache := &routeOperationsGatewayCacheStub{}
	apiKeys := &APIKeyService{apiKeyRepo: &routeOperationsAPIKeyRepoStub{key: key}}
	operations := NewAPIKeyRouteOperationsService(apiKeys, cache)
	groupID := int64(12)
	request := APIKeyRouteClearRequest{
		APIKeyID: key.ID, RouteVersion: key.RouteVersion, GroupID: &groupID,
		ModelFamily: "gpt-5", EndpointKind: "responses", SessionHash: "session-digest",
		ClearSticky: true, ClearBreaker: true,
	}

	result, err := operations.ClearState(context.Background(), request)
	require.NoError(t, err)
	require.True(t, result.StickyDeleted)
	require.Equal(t, int64(2), result.BreakersDeleted)
	require.Len(t, cache.deletedSticky, 1)
	require.Len(t, cache.deletedBreakers, 1)
	require.Contains(t, cache.deletedBreakers[0], ":v4:g12:")

	request.RouteVersion--
	_, err = operations.ClearState(context.Background(), request)
	require.ErrorIs(t, err, ErrAPIKeyRouteVersionStale)
	request.RouteVersion = key.RouteVersion
	unknown := int64(999)
	request.GroupID = &unknown
	_, err = operations.ClearState(context.Background(), request)
	require.ErrorIs(t, err, ErrAPIKeyRouteOperationInvalid)
}
