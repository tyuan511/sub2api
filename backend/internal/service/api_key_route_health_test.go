package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type routeHealthOutcomeCacheStub struct {
	GatewayCache
	recorded int
}

type unavailableRouteHealthCacheStub struct {
	GatewayCache
}

type batchedRouteRuntimeCacheStub struct {
	GatewayCache
	stickyGroupID int64
	breakers      []APIKeyRouteBreakerSnapshot
	batchErr      error
	batchCalls    int
	allowCalls    int
	allowResult   bool
	allowState    string
}

func (s *batchedRouteRuntimeCacheStub) LoadAPIKeyRouteRuntimeState(context.Context, int64, string, []string) (int64, []APIKeyRouteBreakerSnapshot, error) {
	s.batchCalls++
	return s.stickyGroupID, append([]APIKeyRouteBreakerSnapshot(nil), s.breakers...), s.batchErr
}

func (s *batchedRouteRuntimeCacheStub) AllowAPIKeyRoute(context.Context, string, time.Time, time.Duration, time.Duration) (bool, string, error) {
	s.allowCalls++
	return s.allowResult, s.allowState, nil
}

func (*batchedRouteRuntimeCacheStub) RecordAPIKeyRouteResult(context.Context, string, bool, time.Time, time.Duration, int, int) (string, error) {
	return APIKeyRouteBreakerClosed, nil
}

func (*unavailableRouteHealthCacheStub) AllowAPIKeyRoute(context.Context, string, time.Time, time.Duration, time.Duration) (bool, string, error) {
	return false, "", errors.New("redis unavailable")
}

func (*unavailableRouteHealthCacheStub) RecordAPIKeyRouteResult(context.Context, string, bool, time.Time, time.Duration, int, int) (string, error) {
	return "", errors.New("redis unavailable")
}

func (*routeHealthOutcomeCacheStub) AllowAPIKeyRoute(context.Context, string, time.Time, time.Duration, time.Duration) (bool, string, error) {
	return true, APIKeyRouteBreakerClosed, nil
}

func (s *routeHealthOutcomeCacheStub) RecordAPIKeyRouteResult(context.Context, string, bool, time.Time, time.Duration, int, int) (string, error) {
	s.recorded++
	return APIKeyRouteBreakerClosed, nil
}

func TestAPIKeyRouteCapacityOverflowDoesNotAffectHealthBreaker(t *testing.T) {
	cache := &routeHealthOutcomeCacheStub{}
	sink := &routingFactCaptureSink{}
	SetDefaultRoutingFactSink(sink)
	defer SetDefaultRoutingFactSink(nil)
	ctx := WithAPIKeyRoutingUsageContext(context.Background(), APIKeyRoutingUsageContext{
		DecisionID: "decision-capacity", APIKeyID: 7, RouteVersion: 2, InitialGroupID: 11, EffectiveGroupID: 11,
		Platform: PlatformOpenAI, ScheduleMode: APIKeyScheduleModeSequential,
	})

	state, err := recordAPIKeyRouteFailure(ctx, cache, APIKeyRouteHealthPolicy{}, 7, 2, 11, "gpt-5", "/v1/responses", ErrNoAvailableAccounts)
	require.NoError(t, err)
	require.Equal(t, APIKeyRouteBreakerClosed, state)
	require.Zero(t, cache.recorded)
	require.NotNil(t, sink.fact)
	require.Equal(t, RoutingFactOutcomeCapacityOverflow, *sink.fact.OutcomeCategory)
}

func TestAPIKeyRouteFailureClassificationSeparatesCapacityAndHealth(t *testing.T) {
	require.Equal(t, APIKeyRouteOutcomeCapacityOverflow, classifyAPIKeyRouteFailure(ErrAccountProxyPoolExhausted))
	require.Equal(t, APIKeyRouteOutcomeCapacityOverflow, classifyAPIKeyRouteFailure(&UpstreamFailoverError{StatusCode: http.StatusTooManyRequests}))
	require.Equal(t, APIKeyRouteOutcomeCapacityOverflow, classifyAPIKeyRouteFailure(&UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable}))
	require.Equal(t, APIKeyRouteOutcomeHealthFailure, classifyAPIKeyRouteFailure(&UpstreamFailoverError{StatusCode: http.StatusBadGateway}))
	require.Equal(t, APIKeyRouteOutcomeHealthFailure, classifyAPIKeyRouteFailure(errors.New("transport failed")))
	require.Empty(t, classifyAPIKeyRouteFailure(nil))
}

func TestAllowAPIKeyRouteRedisFailureStaysInsideFrozenCandidatePlan(t *testing.T) {
	allowed, state, err := allowAPIKeyRoute(context.Background(), &unavailableRouteHealthCacheStub{}, APIKeyRouteHealthPolicy{}, 7, 2, 11, "gpt-5", "/v1/responses")
	require.Error(t, err)
	require.True(t, allowed, "an unavailable breaker must not turn the configured candidate set into an empty route")
	require.Equal(t, APIKeyRouteBreakerClosed, state)
}

func TestAPIKeyRouteRuntimeStatePreloadsStickyAndClosedBreakersOnce(t *testing.T) {
	cache := &batchedRouteRuntimeCacheStub{
		stickyGroupID: 22,
		breakers: []APIKeyRouteBreakerSnapshot{
			{State: APIKeyRouteBreakerClosed},
			{State: APIKeyRouteBreakerClosed},
		},
	}
	plan := &APIKeyRoutePlan{
		APIKeyID: 7, RouteVersion: 3, RoutingEnabled: true,
		Candidates: []APIKeyRouteCandidate{
			{GroupID: 11, Group: &Group{ID: 11, Platform: PlatformOpenAI}},
			{GroupID: 22, Group: &Group{ID: 22, Platform: PlatformOpenAI}},
		},
	}
	ctx := WithAPIKeyRouteRequestRuntimeState(context.Background(), plan)

	sticky, err := getAPIKeyGroupSticky(ctx, cache, 7, 3, "gpt-5", "/v1/responses", "session")
	require.NoError(t, err)
	require.Equal(t, int64(22), sticky)
	for _, groupID := range []int64{11, 22} {
		allowed, state, allowErr := allowAPIKeyRoute(ctx, cache, APIKeyRouteHealthPolicy{}, 7, 3, groupID, "gpt-5", "/v1/responses")
		require.NoError(t, allowErr)
		require.True(t, allowed)
		require.Equal(t, APIKeyRouteBreakerClosed, state)
	}
	require.Equal(t, 1, cache.batchCalls, "sticky plus all CLOSED breakers must use one batch read")
	require.Zero(t, cache.allowCalls, "CLOSED candidates must not add per-group Redis reads")
}

func TestAPIKeyRouteRuntimeStateKeepsAtomicAdmissionForNonClosedBreaker(t *testing.T) {
	cache := &batchedRouteRuntimeCacheStub{
		breakers:    []APIKeyRouteBreakerSnapshot{{State: APIKeyRouteBreakerOpen}},
		allowResult: false,
		allowState:  APIKeyRouteBreakerOpen,
	}
	plan := &APIKeyRoutePlan{
		APIKeyID: 7, RouteVersion: 3, RoutingEnabled: true,
		Candidates: []APIKeyRouteCandidate{{GroupID: 11, Group: &Group{ID: 11, Platform: PlatformOpenAI}}},
	}
	ctx := WithAPIKeyRouteRequestRuntimeState(context.Background(), plan)

	allowed, state, err := allowAPIKeyRoute(ctx, cache, APIKeyRouteHealthPolicy{}, 7, 3, 11, "gpt-5", "/v1/responses")
	require.NoError(t, err)
	require.False(t, allowed)
	require.Equal(t, APIKeyRouteBreakerOpen, state)
	require.Equal(t, 1, cache.batchCalls)
	require.Equal(t, 1, cache.allowCalls, "OPEN/HALF_OPEN/RECOVERING must retain their atomic gate")
}
