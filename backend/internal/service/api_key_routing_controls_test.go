package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func routingControlInt(value int) *int { return &value }

func TestNewAPIKeyRoutingDefaultsToEightyAndPreservesExplicitThresholds(t *testing.T) {
	groupID := int64(1)
	mode, preference := APIKeyScheduleModeSmart, APIKeySmartPreferenceBalanced
	for _, request := range []CreateAPIKeyRequest{
		{},
		{GroupID: &groupID},
		{GroupRoutes: routeInputs(APIKeyGroupRouteInput{GroupID: 1})},
		{GroupRoutes: routeInputs(APIKeyGroupRouteInput{GroupID: 1}), ScheduleMode: &mode, SmartPreference: &preference},
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

func TestAPIKeyRoutingNewDefaultDoesNotChangeExistingThresholds(t *testing.T) {
	mode := APIKeyScheduleModeSequential
	for _, stored := range []int{0, 50, 80, 95} {
		key := &APIKey{ScheduleMode: mode, RoutingMinSuccessRate: stored,
			GroupRoutes: []APIKeyGroupRoute{{GroupID: 1, Enabled: true}}}
		routing, changed, err := normalizeUpdateAPIKeyRouting(key, UpdateAPIKeyRequest{ScheduleMode: &mode})
		require.NoError(t, err)
		require.True(t, changed)
		expected := stored
		if expected == 0 {
			expected = 50 // Legacy objects without this field retain the old runtime contract.
		}
		require.Equal(t, expected, routing.MinSuccessRate)
	}
}

func TestAPIKeyRoutingControlsValidationAndLegacyCompatibility(t *testing.T) {
	for minimum := 50; minimum <= 95; minimum += 5 {
		require.NoError(t, ValidateAPIKeyRoutingControls(routingControlInt(0), &minimum))
		require.NoError(t, ValidateAPIKeyRoutingControls(routingControlInt(10000), &minimum))
	}
	for _, minimum := range []int{0, 49, 51, 94, 96, 100} {
		require.ErrorIs(t, ValidateAPIKeyRoutingControls(nil, &minimum), ErrAPIKeyRoutesInvalid)
	}
	for _, balance := range []int{-1, 10001} {
		require.ErrorIs(t, ValidateAPIKeyRoutingControls(&balance, nil), ErrAPIKeyRoutesInvalid)
	}
	mode := APIKeyScheduleModeSmart
	created, err := normalizeCreateAPIKeyRouting(CreateAPIKeyRequest{
		GroupRoutes: routeInputs(APIKeyGroupRouteInput{GroupID: 1}), ScheduleMode: &mode,
		SmartBalanceBPS: routingControlInt(3000), RoutingMinSuccessRate: routingControlInt(85),
	})
	require.NoError(t, err)
	require.Equal(t, APIKeySmartPreferencePrice, *created.SmartPreference)
	require.Equal(t, 3000, *created.SmartBalanceBPS)
	require.Equal(t, 85, created.MinSuccessRate)
	key := &APIKey{ScheduleMode: mode, SmartPreference: created.SmartPreference, GroupRoutes: created.Routes,
		SmartBalanceBPS: created.SmartBalanceBPS, RoutingMinSuccessRate: 85}
	_, changed, err := normalizeUpdateAPIKeyRouting(key, UpdateAPIKeyRequest{})
	require.NoError(t, err)
	require.False(t, changed)
	updated, changed, err := normalizeUpdateAPIKeyRouting(key, UpdateAPIKeyRequest{RoutingMinSuccessRate: routingControlInt(95)})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, 3000, *updated.SmartBalanceBPS)
	require.Equal(t, 95, updated.MinSuccessRate)
	legacy := APIKeySmartPreferenceSpeed
	updated, _, err = normalizeUpdateAPIKeyRouting(key, UpdateAPIKeyRequest{SmartPreference: &legacy})
	require.NoError(t, err)
	require.Nil(t, updated.SmartBalanceBPS)
	require.Equal(t, &legacy, updated.SmartPreference)
}

func TestAPIKeyRoutingControlsWeightsAndThresholdAreHardBoundaries(t *testing.T) {
	for balance := 0; balance <= 10000; balance += 50 {
		policy := ApplyAPIKeyRoutingControls(DefaultAPIKeyRoutingStrategyPolicy(APIKeySmartPreferenceBalanced),
			&APIKey{SmartBalanceBPS: &balance, RoutingMinSuccessRate: 85})
		require.NoError(t, ValidateAPIKeyRoutingStrategyPolicy(policy))
		require.Equal(t, .5, policy.Weights.Success)
		require.Equal(t, .1, policy.Weights.Capacity)
		require.InDelta(t, .4, policy.Weights.Price+policy.Weights.Speed, 1e-12)
		require.Equal(t, .85, policy.SuccessRateHardGate)
	}
	weights := APIKeyRoutingBalanceWeights(3000)
	require.InDelta(t, .28, weights.Price, 1e-12)
	require.InDelta(t, .12, weights.Speed, 1e-12)
	for preference, balance := range map[string]int{APIKeySmartPreferencePrice: 1250, APIKeySmartPreferenceSpeed: 8750, APIKeySmartPreferenceBalanced: 5000} {
		old, actual := APIKeyRoutingWeights(preference), APIKeyRoutingBalanceWeights(balance)
		require.InDelta(t, old.Price, actual.Price, 1e-12)
		require.InDelta(t, old.Speed, actual.Speed, 1e-12)
	}
}

func TestAPIKeyRoutingControlsChangeActualRankingAndExactGate(t *testing.T) {
	candidates := []APIKeyRouteCandidate{{GroupID: 1}, {GroupID: 2, Priority: 1}}
	snapshot := &APIKeyRoutingScoreSnapshot{Groups: map[int64]APIKeyRoutingGroupObservation{
		1: {GroupID: 1, SuccessRequests: 85, FailedRequests: 15, NormalizedRate: 1, TTFTP50Ms: 1000, Confidence: 1, PriceConfidence: 1, CapacityScore: 1},
		2: {GroupID: 2, SuccessRequests: 85, FailedRequests: 15, NormalizedRate: 2, TTFTP50Ms: 100, Confidence: 1, PriceConfidence: 1, CapacityScore: 1},
	}}
	for _, test := range []struct {
		balance int
		first   int64
	}{{0, 1}, {3000, 1}, {7000, 2}, {10000, 2}} {
		policy := ApplyAPIKeyRoutingControls(DefaultAPIKeyRoutingStrategyPolicy("balanced"), &APIKey{SmartBalanceBPS: &test.balance, RoutingMinSuccessRate: 85})
		ranked := RankAPIKeyRoutingCandidatesWithPolicy(candidates, snapshot, policy)
		require.True(t, ranked[0].Eligible, "equal to threshold is admitted")
		require.Equal(t, test.first, ranked[0].GroupID)
		policy.SuccessRateHardGate = .9
		ranked = RankAPIKeyRoutingCandidatesWithPolicy(candidates, snapshot, policy)
		for _, score := range ranked {
			require.False(t, score.Eligible)
			require.Equal(t, "success_rate_below_90_percent", score.Exclusion)
		}
	}
}

func TestAPIKeyRoutingControlsRuntimeVersionAndStrictOutage(t *testing.T) {
	plan := &APIKeyRoutePlan{APIKeyID: 7, RouteVersion: 9, RoutingStateVersion: 3, RoutingMinSuccessRate: 95, RoutingEnabled: true,
		Candidates: []APIKeyRouteCandidate{{GroupID: 11, Group: &Group{Platform: PlatformOpenAI}}}}
	ctx := WithAPIKeyRouteRequestRuntimeState(context.Background(), plan)
	require.Equal(t, int64(3), apiKeyRoutingRuntimeVersion(ctx, 7, 9))
	require.Equal(t, int64(8), apiKeyRoutingRuntimeVersion(ctx, 7, 8), "a different frozen request must not borrow state")
	allowed, state, err := allowAPIKeyRoute(ctx, &unavailableRouteHealthCacheStub{}, DefaultAPIKeyRouteHealthPolicy(nil), 7, 9, 11, "gpt-5", "responses")
	require.NoError(t, err)
	require.False(t, allowed, "a strict user threshold must not degrade to unchecked sequential routing")
	require.Equal(t, "STATE_UNAVAILABLE", state)
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
