package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fixedGroupRouteCacheSpy struct {
	GatewayCache
	calls int
}

func (s *fixedGroupRouteCacheSpy) GetSessionAccountID(context.Context, int64, string) (int64, error) {
	s.calls++
	return 999, nil
}
func (s *fixedGroupRouteCacheSpy) SetSessionAccountID(context.Context, int64, string, int64, time.Duration) error {
	s.calls++
	return nil
}
func (s *fixedGroupRouteCacheSpy) AllowAPIKeyRoute(context.Context, string, time.Time, time.Duration, time.Duration) (bool, string, error) {
	s.calls++
	return false, APIKeyRouteBreakerOpen, nil
}
func (s *fixedGroupRouteCacheSpy) RecordAPIKeyRouteResult(context.Context, string, bool, time.Time, time.Duration, int, int) (string, error) {
	s.calls++
	return APIKeyRouteBreakerOpen, nil
}
func (s *fixedGroupRouteCacheSpy) LoadAPIKeyRouteRuntimeState(context.Context, int64, string, []string) (int64, []APIKeyRouteBreakerSnapshot, error) {
	s.calls++
	return 999, []APIKeyRouteBreakerSnapshot{{State: APIKeyRouteBreakerOpen, Failures: 100}}, nil
}

func TestSingleGroupKeyBypassesGroupControlsWithoutRedisIO(t *testing.T) {
	for _, shape := range []string{"explicit", "legacy", "one-enabled"} {
		t.Run(shape, func(t *testing.T) {
			group := routeTestGroup(11, PlatformOpenAI, SubscriptionTypeStandard, StatusActive)
			preference := APIKeySmartPreferencePrice
			key := &APIKey{ID: 9, RouteVersion: 4, RoutingStateVersion: 2, GroupID: &group.ID, Group: group,
				ScheduleMode: APIKeyScheduleModeSmart, SmartPreference: &preference, RoutingMinSuccessRate: 95,
				GroupRoutes: []APIKeyGroupRoute{{GroupID: group.ID, Group: group, Enabled: true}}}
			if shape == "legacy" {
				key.GroupRoutes = nil
			} else if shape == "one-enabled" {
				key.GroupRoutes = append(key.GroupRoutes, APIKeyGroupRoute{GroupID: 12, Priority: 1, Enabled: false})
			}
			plan, err := NewAPIKeyRouteCoordinator(true).BuildPlan(key, nil)
			require.NoError(t, err)
			require.False(t, plan.RoutingEnabled)
			require.Equal(t, APIKeyScheduleModeSequential, plan.ScheduleMode)
			require.Nil(t, plan.SmartPreference)
			require.Len(t, plan.Candidates, 1)
			require.Equal(t, int64(11), plan.Candidates[0].GroupID)
			require.Equal(t, APIKeyScheduleModeSmart, key.ScheduleMode, "do not mutate the auth snapshot")
			ctx := WithAPIKeyRouteRequestRuntimeState(context.Background(), plan)
			require.True(t, apiKeyRouteControlsDisabled(ctx, 9, 4))
			require.False(t, apiKeyRouteControlsDisabled(ctx, 10, 4))
			require.False(t, apiKeyRouteControlsDisabled(ctx, 9, 5))
			cache := &fixedGroupRouteCacheSpy{}
			sticky, err := getAPIKeyGroupSticky(ctx, cache, 9, 4, "gpt-5", "responses", "session")
			require.NoError(t, err)
			require.Zero(t, sticky)
			require.NoError(t, bindAPIKeyGroupSticky(ctx, cache, 9, 4, "gpt-5", "responses", "session", 11, time.Hour))
			allowed, state, err := allowAPIKeyRoute(ctx, cache, DefaultAPIKeyRouteHealthPolicy(nil), 9, 4, 11, "gpt-5", "responses")
			require.NoError(t, err)
			require.True(t, allowed, "an old OPEN breaker must not block the only selected group")
			require.Equal(t, APIKeyRouteBreakerClosed, state)
			for _, success := range []bool{true, false} {
				_, err = recordAPIKeyRouteResult(ctx, cache, DefaultAPIKeyRouteHealthPolicy(nil), 9, 4, 11, "gpt-5", "responses", success)
				require.NoError(t, err)
			}
			require.Zero(t, cache.calls, "fixed groups do not read/write group sticky or breaker state")
			allowed, _, err = allowAPIKeyRoute(ctx, &unavailableRouteHealthCacheStub{}, DefaultAPIKeyRouteHealthPolicy(nil), 9, 4, 11, "gpt-5", "responses")
			require.NoError(t, err)
			require.True(t, allowed, "a group breaker Redis outage must not block a fixed group")
		})
	}
}

func TestMultiGroupKeyRetainsControlsWhenFilteringLeavesOneCandidate(t *testing.T) {
	first := routeTestGroup(11, PlatformOpenAI, SubscriptionTypeStandard, StatusActive)
	second := routeTestGroup(12, PlatformOpenAI, SubscriptionTypeStandard, StatusActive)
	key := &APIKey{ID: 9, RouteVersion: 4, GroupID: &first.ID, Group: first, RoutingMinSuccessRate: 95,
		GroupRoutes: []APIKeyGroupRoute{{GroupID: 11, Enabled: true, Group: first}, {GroupID: 12, Priority: 1, Enabled: true, Group: second}}}
	plan, err := NewAPIKeyRouteCoordinator(true).BuildPlan(key, func(group *Group) (bool, string) {
		return group.ID == 11, "model_unsupported"
	})
	require.NoError(t, err)
	require.Len(t, plan.Candidates, 1)
	require.True(t, plan.RoutingEnabled, "configured multi-group intent survives request-local filtering")
	ctx := WithAPIKeyRouteRequestRuntimeState(context.Background(), plan)
	allowed, state, err := allowAPIKeyRoute(ctx, &unavailableRouteHealthCacheStub{}, DefaultAPIKeyRouteHealthPolicy(nil), 9, 4, 11, "gpt-5", "responses")
	require.NoError(t, err)
	require.False(t, allowed)
	require.Equal(t, "STATE_UNAVAILABLE", state)
}

func TestSingleGroupBypassRetainsFailureObservations(t *testing.T) {
	sink := &routingFactCaptureSink{}
	SetDefaultRoutingFactSink(sink)
	t.Cleanup(func() { SetDefaultRoutingFactSink(nil) })
	ctx := WithAPIKeyRouteRequestRuntimeState(context.Background(), &APIKeyRoutePlan{APIKeyID: 9, RouteVersion: 4})
	ctx = WithAPIKeyRoutingUsageContext(ctx, APIKeyRoutingUsageContext{
		DecisionID: "single-group-failure", APIKeyID: 9, RouteVersion: 4, InitialGroupID: 11, EffectiveGroupID: 11,
		Platform: PlatformOpenAI, ScheduleMode: APIKeyScheduleModeSequential,
	})
	cache := &fixedGroupRouteCacheSpy{}
	_, err := recordAPIKeyRouteResult(ctx, cache, DefaultAPIKeyRouteHealthPolicy(nil), 9, 4, 11, "gpt-5", "responses", false)
	require.NoError(t, err)
	require.Zero(t, cache.calls)
	require.NotNil(t, sink.fact)
	require.Equal(t, RoutingFactOutcomeRouteAttemptFailed, *sink.fact.OutcomeCategory)
}
