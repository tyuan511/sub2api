package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func routingControlInt(value int) *int { return &value }

func TestNewAPIKeyRoutingDefaultsToEightyAndPreservesExplicitThresholds(t *testing.T) {
	groupID := int64(1)
	for _, request := range []CreateAPIKeyRequest{
		{},
		{GroupID: &groupID},
		{GroupRoutes: routeInputs(APIKeyGroupRouteInput{GroupID: 1})},
	} {
		routing, err := normalizeCreateAPIKeyRouting(request)
		require.NoError(t, err)
		require.Equal(t, 80, routing.MinSuccessRate)
	}
	for minimum := 50; minimum <= 95; minimum += 5 {
		routing, err := normalizeCreateAPIKeyRouting(CreateAPIKeyRequest{GroupID: &groupID, RoutingMinSuccessRate: &minimum})
		require.NoError(t, err)
		require.Equal(t, minimum, routing.MinSuccessRate)
	}
}

func TestNormalizeCreateAPIKeyRoutingAlwaysSequential(t *testing.T) {
	// Smart routing has been removed: even a legacy client that still sends the
	// old smart fields must be normalized to a fixed sequential order.
	routing, err := normalizeCreateAPIKeyRouting(CreateAPIKeyRequest{
		GroupRoutes:           routeInputs(APIKeyGroupRouteInput{GroupID: 1}),
		RoutingMinSuccessRate: routingControlInt(85),
	})
	require.NoError(t, err)
	require.Equal(t, APIKeyScheduleModeSequential, routing.ScheduleMode)
	require.Equal(t, 85, routing.MinSuccessRate)
}

func TestAPIKeyRoutingNewDefaultDoesNotChangeExistingThresholds(t *testing.T) {
	for _, stored := range []int{0, 50, 80, 95} {
		key := &APIKey{ScheduleMode: APIKeyScheduleModeSequential, RoutingMinSuccessRate: stored,
			GroupRoutes: []APIKeyGroupRoute{{GroupID: 1, Enabled: true}}}
		// A route edit without an explicit threshold must preserve the stored
		// value rather than resetting it to the creation default.
		routes := routeInputs(APIKeyGroupRouteInput{GroupID: 2, Priority: 0})
		routing, changed, err := normalizeUpdateAPIKeyRouting(key, UpdateAPIKeyRequest{GroupRoutes: routes})
		require.NoError(t, err)
		require.True(t, changed)
		expected := stored
		if expected == 0 {
			expected = 50 // Legacy objects without this field retain the old runtime contract.
		}
		require.Equal(t, expected, routing.MinSuccessRate)
	}
}

func TestNormalizeUpdateAPIKeyRoutingTreatsEquivalentLegacySingleGroupAsNoOp(t *testing.T) {
	groupID := int64(42)
	key := &APIKey{GroupID: &groupID, GroupRoutes: nil, ScheduleMode: APIKeyScheduleModeSequential}
	routes := routeInputs(APIKeyGroupRouteInput{GroupID: groupID, Priority: 0})
	routing, changed, err := normalizeUpdateAPIKeyRouting(key, UpdateAPIKeyRequest{GroupRoutes: routes})
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, []APIKeyGroupRoute{{GroupID: groupID, Priority: 0, Enabled: true}}, routing.Routes)
}

func TestNormalizeUpdateAPIKeyRoutingDetectsActualConfigurationChanges(t *testing.T) {
	first, second := int64(42), int64(43)
	key := &APIKey{
		GroupID: &first, ScheduleMode: APIKeyScheduleModeSequential,
		GroupRoutes: []APIKeyGroupRoute{{GroupID: first, Priority: 0, Enabled: true}},
	}
	routes := routeInputs(
		APIKeyGroupRouteInput{GroupID: first, Priority: 0},
		APIKeyGroupRouteInput{GroupID: second, Priority: 1},
	)
	_, changed, err := normalizeUpdateAPIKeyRouting(key, UpdateAPIKeyRequest{GroupRoutes: routes})
	require.NoError(t, err)
	require.True(t, changed)
}

func TestAPIKeyRoutingControlsValidationAndLegacyCompatibility(t *testing.T) {
	for minimum := 50; minimum <= 95; minimum += 5 {
		require.NoError(t, ValidateAPIKeyRoutingControls(&minimum))
	}
	for _, minimum := range []int{0, 49, 51, 94, 96, 100} {
		require.ErrorIs(t, ValidateAPIKeyRoutingControls(&minimum), ErrAPIKeyRoutesInvalid)
	}
	created, err := normalizeCreateAPIKeyRouting(CreateAPIKeyRequest{
		GroupRoutes: routeInputs(APIKeyGroupRouteInput{GroupID: 1}), RoutingMinSuccessRate: routingControlInt(85),
	})
	require.NoError(t, err)
	require.Equal(t, APIKeyScheduleModeSequential, created.ScheduleMode)
	require.Equal(t, 85, created.MinSuccessRate)
	key := &APIKey{ScheduleMode: APIKeyScheduleModeSequential, GroupRoutes: created.Routes,
		RoutingMinSuccessRate: 85}
	_, changed, err := normalizeUpdateAPIKeyRouting(key, UpdateAPIKeyRequest{})
	require.NoError(t, err)
	require.False(t, changed)
	updated, changed, err := normalizeUpdateAPIKeyRouting(key, UpdateAPIKeyRequest{RoutingMinSuccessRate: routingControlInt(95)})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, 95, updated.MinSuccessRate)
}

func TestAPIKeyRoutingControlsRuntimeVersionAndStrictOutage(t *testing.T) {
	plan := &APIKeyRoutePlan{APIKeyID: 7, RouteVersion: 9, RoutingStateVersion: 3, RoutingMinSuccessRate: 95, RoutingEnabled: true,
		Candidates: []APIKeyRouteCandidate{{GroupID: 11, Group: &Group{Platform: PlatformOpenAI}}}}
	ctx := WithAPIKeyRouteRequestRuntimeState(context.Background(), plan)
	require.Equal(t, int64(3), apiKeyRoutingRuntimeVersion(ctx, 7, 9))
	require.Equal(t, int64(8), apiKeyRoutingRuntimeVersion(ctx, 7, 8), "a different frozen request must not borrow state")
	allowed, state, err := allowAPIKeyRoute(ctx, &unavailableRouteHealthCacheStub{}, DefaultAPIKeyRouteHealthPolicy(nil), 7, 9, 11, "gpt-5", "responses")
	require.Error(t, err)
	require.True(t, allowed, "user success rate is not a hard intercept, so Redis outage fail-opens")
	require.Equal(t, APIKeyRouteBreakerClosed, state)
}

func TestAPIKeyRoutingControlsProbeAdmissionIsRequestScoped(t *testing.T) {
	cache := &batchedRouteRuntimeCacheStub{breakers: []APIKeyRouteBreakerSnapshot{{State: APIKeyRouteBreakerOpen}}, allowResult: true, allowState: APIKeyRouteBreakerHalfOpen}
	plan := &APIKeyRoutePlan{APIKeyID: 7, RouteVersion: 3, RoutingEnabled: true, Candidates: []APIKeyRouteCandidate{{GroupID: 11, Group: &Group{Platform: PlatformOpenAI}}}}
	ctx := WithAPIKeyRouteRequestRuntimeState(context.Background(), plan)
	for i := 0; i < 3; i++ {
		allowed, state, err := allowAPIKeyRoute(ctx, cache, DefaultAPIKeyRouteHealthPolicy(nil), 7, 3, 11, "gpt-5", "responses")
		require.NoError(t, err)
		require.True(t, allowed)
		require.Equal(t, APIKeyRouteBreakerHalfOpen, state)
	}
	require.Equal(t, 1, cache.allowCalls)
	breaker, found := APIKeyRoutePreloadedBreaker(ctx, "gpt-5", "responses", 11)
	require.True(t, found)
	require.Equal(t, APIKeyRouteBreakerHalfOpen, breaker.State)
}
