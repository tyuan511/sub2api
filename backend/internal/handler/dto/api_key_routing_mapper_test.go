package dto

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyFromServiceProjectsUserRatesAndRoutingExplanation(t *testing.T) {
	preference := service.APIKeySmartPreferencePrice
	groups := []*service.Group{
		{ID: 11, Name: "primary", Platform: service.PlatformOpenAI, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 2},
		{ID: 12, Name: "fallback", Platform: service.PlatformOpenAI, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 1},
	}
	now := time.Now().UTC()
	store := service.DefaultAPIKeyRoutingScoreStore()
	require.NoError(t, store.Replace([]*service.APIKeyRoutingScoreSnapshot{{
		Version: "score-user-rate", StrategyVersion: "strategy-user-rate", FeatureVersion: "features-v1",
		Platform: service.PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "other", GeneratedAt: now,
		Groups: map[int64]service.APIKeyRoutingGroupObservation{
			11: {GroupID: 11, SuccessRequests: 90, FailedRequests: 10, SmoothedSuccessRate: .9, Confidence: 1, CapacityScore: 1, NormalizedRate: 3, PriceNormalizationFactor: 1.5, TTFTP50Ms: 120, DurationP50Ms: 800, CacheHitRate: .8, LogicalInputTokens: 1000, ActualOutputTokens: 100, ObservationWindow: "1h"},
			12: {GroupID: 12, SuccessRequests: 90, FailedRequests: 10, SmoothedSuccessRate: .9, Confidence: 1, CapacityScore: 1, NormalizedRate: 1.5, PriceNormalizationFactor: 1.5, TTFTP50Ms: 120, DurationP50Ms: 800, CacheHitRate: .8, LogicalInputTokens: 1000, ActualOutputTokens: 100, ObservationWindow: "1h"},
		},
	}}))
	t.Cleanup(func() { _ = store.Replace(nil) })

	key := &service.APIKey{
		ID: 7, UserID: 9, ScheduleMode: service.APIKeyScheduleModeSmart, SmartPreference: &preference, RouteVersion: 2,
		User: &service.User{ID: 9, GroupRates: map[int64]float64{11: .5}},
		RoutingSelectionObservations: []service.APIKeyRoutingSelectionObservation{
			{APIKeyID: 7, RouteVersion: 2, Platform: service.PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "other", StrategyVersion: "strategy-user-rate", SmartPreference: preference, GroupID: 11, SampledSelections: 20, WeightedSelections: 20, WeightSquares: 20, DataThrough: now},
			{APIKeyID: 7, RouteVersion: 2, Platform: service.PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "other", StrategyVersion: "strategy-user-rate", SmartPreference: preference, GroupID: 12, SampledSelections: 80, WeightedSelections: 80, WeightSquares: 80, DataThrough: now},
		},
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 11, Priority: 0, Enabled: true, Group: groups[0]},
			{GroupID: 12, Priority: 1, Enabled: true, Group: groups[1]},
		},
	}
	out := APIKeyFromService(key)
	require.NotNil(t, out.RoutingPolicy)
	require.NotNil(t, out.EstimatedRate)
	require.False(t, out.EstimatedRate.Guaranteed)
	require.Equal(t, "actual_routed_group", out.EstimatedRate.Settlement)
	require.Equal(t, "blended", out.EstimatedRate.SelectionSource)
	require.EqualValues(t, 100, out.EstimatedRate.SelectionSamples)
	require.InDelta(t, 100, out.EstimatedRate.SelectionEffectiveN, 1e-12)
	require.Equal(t, "gpt-5", out.EstimatedRate.ModelFamily)
	require.NotNil(t, out.GroupRoutes[0].CurrentRate)
	require.InDelta(t, .5, *out.GroupRoutes[0].CurrentRate, 1e-12)
	require.NotNil(t, out.GroupRoutes[0].NormalizedEffectiveRate)
	require.InDelta(t, .75, *out.GroupRoutes[0].NormalizedEffectiveRate, 1e-12)
	require.NotNil(t, out.GroupRoutes[0].PredictedShare)
	require.NotNil(t, out.GroupRoutes[1].PredictedShare)
	require.Greater(t, *out.GroupRoutes[1].PredictedShare, *out.GroupRoutes[0].PredictedShare)
	require.NotNil(t, out.GroupRoutes[0].TTFTMS)
	require.NotNil(t, out.GroupRoutes[0].DurationMS)
}
