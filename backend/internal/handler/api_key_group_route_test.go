package handler

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRouteRuntimeAdvanceUsesRequestLocalActualGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	primaryID, secondaryID := int64(11), int64(12)
	override := 3
	apiKey := &service.APIKey{
		ID:           9,
		GroupID:      &primaryID,
		Group:        &service.Group{ID: primaryID, Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard},
		User:         &service.User{ID: 7, UserGroupRPMOverride: &override},
		RouteVersion: 4,
		ScheduleMode: service.APIKeyScheduleModeSequential,
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: primaryID, Priority: 0, Enabled: true, Group: &service.Group{ID: primaryID, Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard}},
			{GroupID: secondaryID, Priority: 1, Enabled: true, Group: &service.Group{ID: secondaryID, Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard}},
		},
	}
	plan, err := service.NewAPIKeyRouteCoordinator(true).BuildPlan(apiKey, nil)
	require.NoError(t, err)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(context.Background())
	middleware2.SetAPIKeyRouteState(c, &middleware2.APIKeyRouteState{Plan: plan, InitialGroupID: primaryID})
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)

	actual, subscription, advanced, err := (apiKeyRouteRuntime{}).advance(c, "gpt-5", "/v1/responses", nil)
	require.NoError(t, err)
	require.True(t, advanced)
	require.Nil(t, subscription)
	require.Equal(t, secondaryID, *actual.GroupID)
	require.Equal(t, secondaryID, actual.Group.ID)
	require.NotSame(t, apiKey, actual)
	require.NotSame(t, apiKey.User, actual.User)
	require.Nil(t, actual.User.UserGroupRPMOverride)
	require.Equal(t, primaryID, *apiKey.GroupID)
	require.NotNil(t, apiKey.User.UserGroupRPMOverride)

	state, ok := middleware2.GetAPIKeyRouteState(c)
	require.True(t, ok)
	require.Equal(t, 1, state.Index)
	require.Equal(t, 1, state.SwitchCount)
	fromContext, ok := middleware2.GetAPIKeyFromContext(c)
	require.True(t, ok)
	require.Equal(t, secondaryID, *fromContext.GroupID)
}

func TestAPIKeyRouteRuntimeAdvanceSkipsRequestIneligibleCandidate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ids := []int64{21, 22, 23}
	routes := make([]service.APIKeyGroupRoute, 0, len(ids))
	for priority, id := range ids {
		routes = append(routes, service.APIKeyGroupRoute{
			GroupID: id, Priority: priority, Enabled: true,
			Group: &service.Group{ID: id, Status: service.StatusActive, Platform: service.PlatformAnthropic, SubscriptionType: service.SubscriptionTypeStandard},
		})
	}
	apiKey := &service.APIKey{ID: 19, GroupID: &ids[0], Group: routes[0].Group, User: &service.User{ID: 17}, RouteVersion: 2, GroupRoutes: routes}
	plan, err := service.NewAPIKeyRouteCoordinator(true).BuildPlan(apiKey, nil)
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	middleware2.SetAPIKeyRouteState(c, &middleware2.APIKeyRouteState{Plan: plan, InitialGroupID: ids[0]})

	actual, _, advanced, err := (apiKeyRouteRuntime{}).advance(c, "gpt-5", "/v1/responses", func(candidate *service.APIKey) error {
		if candidate.Group.ID == ids[1] {
			return service.ErrNoEligibleAPIKeyRoute
		}
		return nil
	})
	require.NoError(t, err)
	require.True(t, advanced)
	require.Equal(t, ids[2], actual.Group.ID)
	state, _ := middleware2.GetAPIKeyRouteState(c)
	require.Equal(t, 2, state.SwitchCount)
}

func TestAPIKeyRouteRuntimeEnsureInitialFiltersWithoutCountingSwitch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ids := []int64{24, 25, 26}
	routes := make([]service.APIKeyGroupRoute, 0, len(ids))
	for priority, id := range ids {
		routes = append(routes, service.APIKeyGroupRoute{
			GroupID: id, Priority: priority, Enabled: true,
			Group: &service.Group{ID: id, Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard},
		})
	}
	apiKey := &service.APIKey{ID: 20, GroupID: &ids[0], Group: routes[0].Group, User: &service.User{ID: 18}, RouteVersion: 3, GroupRoutes: routes}
	plan, err := service.NewAPIKeyRouteCoordinator(true).BuildPlan(apiKey, nil)
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	middleware2.SetAPIKeyRouteState(c, &middleware2.APIKeyRouteState{Plan: plan, Order: []int{0, 1, 2}, InitialGroupID: ids[0]})
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)

	actual, _, changed, err := (apiKeyRouteRuntime{}).ensureInitial(c, func(candidate *service.APIKey) error {
		if candidate.Group.ID == ids[0] {
			return service.ErrNoEligibleAPIKeyRoute
		}
		return nil
	})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, ids[1], actual.Group.ID)
	state, ok := middleware2.GetAPIKeyRouteState(c)
	require.True(t, ok)
	require.Equal(t, ids[1], state.InitialGroupID)
	require.Zero(t, state.SwitchCount)
	require.Equal(t, []int{1, 2}, state.Order, "rejected primary must not reappear after runtime failover")

	next, ok := middleware2.AdvanceAPIKeyRoute(c)
	require.True(t, ok)
	require.Equal(t, ids[2], next.Group.ID)
	require.Equal(t, 1, state.SwitchCount)
}

func TestAPIKeyRouteRuntimeValidateInitialNeverMovesStateOwnedContinuation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	primaryID, secondaryID := int64(27), int64(28)
	routes := []service.APIKeyGroupRoute{
		{GroupID: primaryID, Priority: 0, Enabled: true, Group: &service.Group{ID: primaryID, Status: service.StatusActive, Platform: service.PlatformOpenAI}},
		{GroupID: secondaryID, Priority: 1, Enabled: true, Group: &service.Group{ID: secondaryID, Status: service.StatusActive, Platform: service.PlatformOpenAI}},
	}
	apiKey := &service.APIKey{ID: 21, GroupID: &primaryID, Group: routes[0].Group, User: &service.User{ID: 19}, RouteVersion: 4, GroupRoutes: routes}
	plan, err := service.NewAPIKeyRouteCoordinator(true).BuildPlan(apiKey, nil)
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	middleware2.SetAPIKeyRouteState(c, &middleware2.APIKeyRouteState{Plan: plan, Order: []int{0, 1}, InitialGroupID: primaryID})
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)

	actual, _, err := (apiKeyRouteRuntime{}).validateInitial(c, func(*service.APIKey) error {
		return service.ErrNoEligibleAPIKeyRoute
	})
	require.Error(t, err)
	require.Nil(t, actual)
	state, ok := middleware2.GetAPIKeyRouteState(c)
	require.True(t, ok)
	require.Equal(t, []int{0, 1}, state.Order)
	require.Equal(t, primaryID, state.InitialGroupID)
	require.Zero(t, state.SwitchCount)
}

func TestActivateAPIKeyRouteOwnedGroupPinsConfiguredPhysicalGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	primaryID, ownerID := int64(29), int64(30)
	routes := []service.APIKeyGroupRoute{
		{GroupID: primaryID, Priority: 0, Enabled: true, Group: &service.Group{ID: primaryID, Status: service.StatusActive, Platform: service.PlatformOpenAI}},
		{GroupID: ownerID, Priority: 1, Enabled: true, Group: &service.Group{ID: ownerID, Status: service.StatusActive, Platform: service.PlatformOpenAI}},
	}
	apiKey := &service.APIKey{ID: 22, GroupID: &primaryID, Group: routes[0].Group, User: &service.User{ID: 20}, RouteVersion: 5, GroupRoutes: routes}
	plan, err := service.NewAPIKeyRouteCoordinator(true).BuildPlan(apiKey, nil)
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	middleware2.SetAPIKeyRouteState(c, &middleware2.APIKeyRouteState{Plan: plan, Order: []int{0, 1}, InitialGroupID: primaryID})
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)

	actual, changed, err := activateAPIKeyRouteOwnedGroup(c, func(groupID int64) (bool, error) {
		return groupID == ownerID, nil
	})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, ownerID, actual.Group.ID)
	state, ok := middleware2.GetAPIKeyRouteState(c)
	require.True(t, ok)
	require.Equal(t, ownerID, state.InitialGroupID)
	require.Equal(t, []int{1, 0}, state.Order)
	require.Zero(t, state.SwitchCount)
}

func TestAPIKeyRouteRuntimeSmartOrderIsFrozenForRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ids := []int64{31, 32}
	preference := service.APIKeySmartPreferenceSpeed
	routes := []service.APIKeyGroupRoute{
		{GroupID: ids[0], Priority: 0, Enabled: true, Group: &service.Group{ID: ids[0], Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard}},
		{GroupID: ids[1], Priority: 1, Enabled: true, Group: &service.Group{ID: ids[1], Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard}},
	}
	apiKey := &service.APIKey{
		ID: 29, GroupID: &ids[0], Group: routes[0].Group, User: &service.User{ID: 27}, RouteVersion: 3,
		ScheduleMode: service.APIKeyScheduleModeSmart, SmartPreference: &preference, GroupRoutes: routes,
	}
	plan, err := service.NewAPIKeyRouteCoordinator(true).BuildPlan(apiKey, nil)
	require.NoError(t, err)
	now := time.Now().UTC()
	store := service.DefaultAPIKeyRoutingScoreStore()
	require.NoError(t, store.Replace([]*service.APIKeyRoutingScoreSnapshot{{
		Version: "score-1", StrategyVersion: "strategy-1", FeatureVersion: "feature-1",
		Platform: service.PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses", GeneratedAt: now,
		Groups: map[int64]service.APIKeyRoutingGroupObservation{
			ids[0]: {GroupID: ids[0], SuccessRequests: 100, NormalizedRate: 1, TTFTP50Ms: 900, CapacityScore: 1, Confidence: 1},
			ids[1]: {GroupID: ids[1], SuccessRequests: 100, NormalizedRate: 1, TTFTP50Ms: 100, CapacityScore: 1, Confidence: 1},
		},
	}}))
	t.Cleanup(func() { _ = store.Replace(nil) })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	middleware2.SetAPIKeyRouteState(c, &middleware2.APIKeyRouteState{Plan: plan, Order: []int{0, 1}, InitialGroupID: ids[0]})
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)

	actual, _, ranked, changed, err := (apiKeyRouteRuntime{}).activateSmart(c, apiKey, "gpt-5.6-sol", "/v1/responses", "session-smart-order", nil)
	require.NoError(t, err)
	require.True(t, changed)
	require.NotEmpty(t, ranked)
	require.Equal(t, ids[1], *actual.GroupID)
	state, ok := middleware2.GetAPIKeyRouteState(c)
	require.True(t, ok)
	require.Equal(t, "score-1", state.ScoreVersion)
	require.Equal(t, ids[1], state.InitialGroupID)
	require.Zero(t, state.SwitchCount, "initial smart choice is not a failover")

	next, ok := middleware2.AdvanceAPIKeyRoute(c)
	require.True(t, ok)
	require.Equal(t, ids[0], *next.GroupID)
	require.Equal(t, 1, state.SwitchCount)
}

func TestAPIKeyRouteSmartControlsCannotReviveBelowThresholdWithoutProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pref, balance := service.APIKeySmartPreferencePrice, 0
	id := int64(81)
	routes := []service.APIKeyGroupRoute{
		{GroupID: 81, Enabled: true, Group: &service.Group{ID: 81, Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard}},
		{GroupID: 82, Priority: 1, Enabled: true, Group: &service.Group{ID: 82, Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard}},
	}
	key := &service.APIKey{ID: 80, RouteVersion: 1, GroupID: &id, Group: routes[0].Group, GroupRoutes: routes,
		User: &service.User{ID: 80}, ScheduleMode: service.APIKeyScheduleModeSmart, SmartPreference: &pref, SmartBalanceBPS: &balance, RoutingMinSuccessRate: 95}
	plan, err := service.NewAPIKeyRouteCoordinator(true).BuildPlan(key, nil)
	require.NoError(t, err)
	store := service.DefaultAPIKeyRoutingScoreStore()
	require.NoError(t, store.Replace([]*service.APIKeyRoutingScoreSnapshot{{
		Version: "controls-score", StrategyVersion: "controls-strategy", FeatureVersion: "controls-feature",
		Platform: service.PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses", GeneratedAt: time.Now(),
		Groups: map[int64]service.APIKeyRoutingGroupObservation{
			81: {GroupID: 81, SuccessRequests: 90, FailedRequests: 10, NormalizedRate: .1, Confidence: 1},
			82: {GroupID: 82, SuccessRequests: 85, FailedRequests: 15, NormalizedRate: 1, Confidence: 1},
		},
	}}))
	t.Cleanup(func() { _ = store.Replace(nil) })
	for _, check := range []apiKeyRouteCandidateCheck{nil, func(*service.APIKey) error { return nil }} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		middleware2.SetAPIKeyRouteState(c, &middleware2.APIKeyRouteState{Plan: plan, Order: []int{0, 1}, InitialGroupID: id})
		c.Set(string(middleware2.ContextKeyAPIKey), key)
		_, _, ranked, _, err := (apiKeyRouteRuntime{}).activateSmart(c, key, "gpt-5.6-sol", "/v1/responses", "controls-session", check)
		require.ErrorIs(t, err, service.ErrNoEligibleAPIKeyRoute)
		for _, score := range ranked {
			require.False(t, score.Eligible)
		}
	}
}

func TestAPIKeyRouteSingleGroupPreservesLegacyRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	group := &service.Group{ID: 81, Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard}
	pref := service.APIKeySmartPreferencePrice
	key := &service.APIKey{ID: 80, RouteVersion: 1, GroupID: &group.ID, Group: group, User: &service.User{ID: 80},
		ScheduleMode: service.APIKeyScheduleModeSmart, SmartPreference: &pref, RoutingMinSuccessRate: 95,
		GroupRoutes: []service.APIKeyGroupRoute{{GroupID: group.ID, Enabled: true, Group: group}}}
	plan, err := service.NewAPIKeyRouteCoordinator(true).BuildPlan(key, nil)
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	middleware2.SetAPIKeyRouteState(c, &middleware2.APIKeyRouteState{Plan: plan, Order: []int{0}, InitialGroupID: group.ID})
	c.Set(string(middleware2.ContextKeyAPIKey), key)
	require.False(t, shouldActivateSmartRoute(c, false, false))
	actual, _, ranked, changed, err := (apiKeyRouteRuntime{}).activateSmart(c, key, "gpt-5", "responses", "session", nil)
	require.NoError(t, err)
	require.Same(t, key, actual)
	require.False(t, changed)
	require.Empty(t, ranked)
	// Legacy endpoint checks remain in the handlers; the route runtime must
	// neither repeat subscription checks nor add candidate-only restrictions.
	subscription := &service.UserSubscription{ID: 19, DailyUsageUSD: 100}
	middleware2.SetSubscriptionInContext(c, subscription)
	originalContext := c.Request.Context()
	actual, gotSubscription, changed, err := (apiKeyRouteRuntime{}).ensureInitial(c, func(*service.APIKey) error {
		t.Fatal("fixed key entered multi-group candidate validation")
		return service.ErrNoEligibleAPIKeyRoute
	})
	require.NoError(t, err)
	require.Same(t, key, actual)
	require.Same(t, subscription, gotSubscription)
	require.Same(t, originalContext, c.Request.Context())
	require.False(t, changed)
}

func TestAPIKeyRoutingLearningCannotResurrectHardExcludedCandidate(t *testing.T) {
	baseline := []service.APIKeyRoutingCandidateScore{
		{GroupID: 1, Eligible: true, Score: .70},
		{GroupID: 2, Eligible: false, Exclusion: "success_rate_below_50_percent", Score: 0},
	}
	predicted := []service.APIKeyRoutingCandidateScore{
		{GroupID: 2, Eligible: true, Score: .99},
		{GroupID: 1, Eligible: true, Score: .69},
	}
	actual := applyAPIKeyRoutingBaselineEligibility(predicted, baseline)
	require.Equal(t, int64(1), actual[0].GroupID)
	require.True(t, actual[0].Eligible)
	require.Equal(t, int64(2), actual[1].GroupID)
	require.False(t, actual[1].Eligible)
	require.Equal(t, "success_rate_below_50_percent", actual[1].Exclusion)
}

func TestSmartCanarySelectionOnlyAppliesToNewSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupIDs := []int64{41, 42}
	apiKey := &service.APIKey{ID: 39, RouteVersion: 7, ScheduleMode: service.APIKeyScheduleModeSmart,
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: groupIDs[0], Priority: 0, Enabled: true, Group: &service.Group{ID: groupIDs[0], Status: service.StatusActive, Platform: service.PlatformOpenAI}},
			{GroupID: groupIDs[1], Priority: 1, Enabled: true, Group: &service.Group{ID: groupIDs[1], Status: service.StatusActive, Platform: service.PlatformOpenAI}},
		},
	}
	plan, err := service.NewAPIKeyRouteCoordinator(true).BuildPlan(apiKey, nil)
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	middleware2.SetAPIKeyRouteState(c, &middleware2.APIKeyRouteState{Plan: plan, Order: []int{0, 1}})

	require.False(t, shouldActivateSmartRoute(c, true, false), "accepted healthy sticky session must bypass canary strategy selection")
	require.True(t, shouldActivateSmartRoute(c, false, false), "new session may use the active/canary strategy")
	require.False(t, shouldActivateSmartRoute(c, false, true), "Redis route-state failure must degrade to frozen sequential order")
}

func TestActiveFallbackStickyDrainsUntilThatRouteFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	primaryID, fallbackID := int64(51), int64(52)
	preference := service.APIKeySmartPreferenceBalanced
	apiKey := &service.APIKey{ID: 49, RouteVersion: 8, ScheduleMode: service.APIKeyScheduleModeSmart, SmartPreference: &preference,
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: primaryID, Priority: 0, Enabled: true, Group: &service.Group{ID: primaryID, Status: service.StatusActive, Platform: service.PlatformOpenAI}},
			{GroupID: fallbackID, Priority: 1, Enabled: true, Group: &service.Group{ID: fallbackID, Status: service.StatusActive, Platform: service.PlatformOpenAI}},
		},
	}
	plan, err := service.NewAPIKeyRouteCoordinator(true).BuildPlan(apiKey, nil)
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	middleware2.SetAPIKeyRouteState(c, &middleware2.APIKeyRouteState{Plan: plan, Order: []int{0, 1}, InitialGroupID: primaryID})

	actual, ok := middleware2.ActivateInitialAPIKeyRouteIndex(c, 1)
	require.True(t, ok)
	require.Equal(t, fallbackID, *actual.GroupID)
	middleware2.MarkAPIKeyRouteStickySelected(c)
	require.False(t, shouldActivateSmartRoute(c, true, false), "a recovered primary must not re-rank an active fallback session")
	state, _ := middleware2.GetAPIKeyRouteState(c)
	require.Zero(t, state.SwitchCount)
	require.False(t, state.StickyBroken)

	// Only an actual failure of the sticky fallback advances the request.
	actual, ok = middleware2.AdvanceAPIKeyRoute(c)
	require.True(t, ok)
	require.Equal(t, primaryID, *actual.GroupID)
	require.True(t, state.StickyBroken)
	require.Equal(t, 1, state.SwitchCount)
}

func TestAPIKeyRouteFailureAllowsAdvanceOnlyForReplaySafeFailures(t *testing.T) {
	decision := classifyAPIKeyRouteReplay(nil)
	require.True(t, decision.Allow, "pre-upstream candidate skip is safe")
	require.Equal(t, "pre_upstream_candidate_skip", decision.Reason)
	require.True(t, apiKeyRouteFailureAllowsAdvance(service.ErrNoAvailableAccounts))
	require.True(t, apiKeyRouteFailureAllowsAdvance(service.ErrAccountProxyPoolExhausted))
	require.True(t, apiKeyRouteFailureAllowsAdvance(&service.UpstreamFailoverError{
		NextAccountAction: service.NextAccountRetry,
	}))
	require.True(t, apiKeyRouteFailureAllowsAdvance(&service.UpstreamFailoverError{
		StatusCode: 429, NextAccountAction: service.NextAccountRetry,
	}))
	require.True(t, apiKeyRouteFailureAllowsAdvance(&service.UpstreamFailoverError{
		StatusCode: 413, Scope: service.GatewayFailureScopeAccount, NextAccountAction: service.NextAccountRetry,
	}))
	require.False(t, apiKeyRouteFailureAllowsAdvance(&service.UpstreamFailoverError{
		NextAccountAction: service.NextAccountStop,
	}))
	require.False(t, apiKeyRouteFailureAllowsAdvance(&service.UpstreamFailoverError{
		StatusCode: 400, NextAccountAction: service.NextAccountRetry,
	}))
	require.False(t, apiKeyRouteFailureAllowsAdvance(&service.UpstreamFailoverError{
		StatusCode: 529, Scope: service.GatewayFailureScopeRequest, NextAccountAction: service.NextAccountRetry,
	}))
	require.False(t, apiKeyRouteFailureAllowsAdvance(&service.UpstreamFailoverError{
		StatusCode: 503, RequestScopedTransient: true, NextAccountAction: service.NextAccountRetry,
	}))
	require.False(t, apiKeyRouteFailureAllowsAdvance(&service.UpstreamFailoverError{
		StatusCode: 503, Scope: service.GatewayFailureScopeProvider, NextAccountAction: service.NextAccountRetry,
	}))
	require.False(t, apiKeyRouteFailureAllowsAdvance(context.Canceled))
	require.False(t, apiKeyRouteFailureAllowsAdvance(service.ErrSubscriptionExpired))
	require.Equal(t, "unclassified_failure", classifyAPIKeyRouteReplay(service.ErrSubscriptionExpired).Reason)
}

func TestAPIKeyRouteReplayLatchBlocksAfterSemanticOutputButAllowsHeartbeats(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)

	require.True(t, apiKeyRouteFailureAllowsAdvanceBeforeSemanticOutput(c, service.ErrNoAvailableAccounts))

	headerOnly, _ := gin.CreateTestContext(httptest.NewRecorder())
	headerOnly.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	headerOnly.Writer.WriteHeader(502)
	headerOnly.Writer.WriteHeaderNow()
	require.False(t, apiKeyRouteFailureAllowsAdvanceBeforeSemanticOutput(headerOnly, service.ErrNoAvailableAccounts), "a committed HTTP status is semantic output")

	heartbeat := []byte(":\n\n")
	written, err := c.Writer.Write(heartbeat)
	require.NoError(t, err)
	recordGatewayStreamHeartbeat(c, written)
	require.True(t, apiKeyRouteFailureAllowsAdvanceBeforeSemanticOutput(c, service.ErrNoAvailableAccounts), "transport-only heartbeat must not pin a physical group")

	_, err = c.Writer.Write([]byte("data: {\"type\":\"response.output_text.delta\"}\n\n"))
	require.NoError(t, err)
	require.False(t, apiKeyRouteFailureAllowsAdvanceBeforeSemanticOutput(c, service.ErrNoAvailableAccounts), "semantic output permanently closes cross-group replay")
	require.False(t, apiKeyRouteFailureAllowsAdvanceBeforeSemanticOutput(c, service.ErrSubscriptionExpired), "non-replayable failures stay blocked before and after output")
}

func TestAPIKeyRouteAdvanceErrorOnlyClassifiesSubscriptionAndBillingFailuresAsBilling(t *testing.T) {
	capabilityErr := &apiKeyRouteAdvanceError{Last: service.ErrNoEligibleAPIKeyRoute}
	require.False(t, isAPIKeyRouteAdvanceBillingError(capabilityErr))

	subscriptionErr := &apiKeyRouteAdvanceError{Last: service.ErrSubscriptionExpired, Billing: true}
	require.True(t, isAPIKeyRouteAdvanceBillingError(subscriptionErr))
	require.ErrorIs(t, subscriptionErr, service.ErrSubscriptionExpired)
}
