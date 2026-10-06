package handler

import (
	"context"
	"net/http/httptest"
	"testing"

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

func TestAPIKeyRouteRuntimeAdvanceDoesNotSpendBudgetOnHardFilterSkip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ids := []int64{31, 32, 33}
	routes := make([]service.APIKeyGroupRoute, 0, len(ids))
	for priority, id := range ids {
		routes = append(routes, service.APIKeyGroupRoute{
			GroupID: id, Priority: priority, Enabled: true,
			Group: &service.Group{ID: id, Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard},
		})
	}
	apiKey := &service.APIKey{ID: 29, GroupID: &ids[0], Group: routes[0].Group, User: &service.User{ID: 27}, RouteVersion: 2, GroupRoutes: routes}
	plan, err := service.NewAPIKeyRouteCoordinator(true).BuildPlan(apiKey, nil)
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	middleware2.SetAPIKeyRouteState(c, &middleware2.APIKeyRouteState{Plan: plan, InitialGroupID: ids[0]})
	failoverTransitionBudgetFor(c, 1)

	actual, _, advanced, err := (apiKeyRouteRuntime{}).advance(c, "gpt-5", "/v1/responses", func(candidate *service.APIKey) error {
		if candidate.Group.ID == ids[1] {
			return service.ErrNoEligibleAPIKeyRoute
		}
		return nil
	})
	require.NoError(t, err)
	require.True(t, advanced)
	require.Equal(t, ids[2], actual.Group.ID)
	require.False(t, reserveFailoverTransition(c), "the successful group switch should be the only budgeted transition")
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

type routeModelAvailabilityStub struct {
	unsupported map[int64]struct{}
}

func (s routeModelAvailabilityStub) DiagnoseModelAvailabilityForPlatform(_ context.Context, groupID *int64, _ string, _ string) service.ModelAvailabilityDiagnosis {
	if groupID != nil {
		if _, skip := s.unsupported[*groupID]; skip {
			return service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}
		}
	}
	return service.ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
}

func TestAPIKeyRouteRuntimeEnsureInitialSkipsGroupsWithoutRequestedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	primaryID, secondaryID := int64(31), int64(32)
	routes := []service.APIKeyGroupRoute{
		{GroupID: primaryID, Priority: 0, Enabled: true, Group: &service.Group{ID: primaryID, Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard}},
		{GroupID: secondaryID, Priority: 1, Enabled: true, Group: &service.Group{ID: secondaryID, Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard}},
	}
	apiKey := &service.APIKey{ID: 22, GroupID: &primaryID, Group: routes[0].Group, User: &service.User{ID: 20}, RouteVersion: 5, GroupRoutes: routes}
	plan, err := service.NewAPIKeyRouteCoordinator(true).BuildPlan(apiKey, nil)
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	middleware2.SetAPIKeyRouteState(c, &middleware2.APIKeyRouteState{Plan: plan, Order: []int{0, 1}, InitialGroupID: primaryID})
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)

	diag := routeModelAvailabilityStub{unsupported: map[int64]struct{}{primaryID: {}}}
	actual, _, changed, err := (apiKeyRouteRuntime{}).ensureInitial(c, func(candidate *service.APIKey) error {
		return rejectAPIKeyRouteUnsupportedModel(c, diag, candidate, "gpt-4o")
	})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, secondaryID, actual.Group.ID)
	state, ok := middleware2.GetAPIKeyRouteState(c)
	require.True(t, ok)
	require.Equal(t, secondaryID, state.InitialGroupID)
	require.Zero(t, state.SwitchCount)
}

func TestRejectAPIKeyRouteUnsupportedModelSkipsOtherPlatforms(t *testing.T) {
	gin.SetMode(gin.TestMode)
	openaiID, grokID := int64(41), int64(42)
	openai := &service.Group{ID: openaiID, Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard}
	grok := &service.Group{ID: grokID, Status: service.StatusActive, Platform: service.PlatformGrok, SubscriptionType: service.SubscriptionTypeStandard}
	apiKey := &service.APIKey{
		ID: 23, GroupID: &openaiID, Group: openai, User: &service.User{ID: 21}, RouteVersion: 6,
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: openaiID, Priority: 0, Enabled: true, Group: openai},
			{GroupID: grokID, Priority: 1, Enabled: true, Group: grok},
		},
	}
	plan, err := service.NewAPIKeyRouteCoordinator(true).BuildPlan(apiKey, nil)
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	middleware2.SetAPIKeyRouteState(c, &middleware2.APIKeyRouteState{Plan: plan, Order: []int{0, 1}, InitialGroupID: openaiID})
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)

	diag := routeModelAvailabilityStub{unsupported: map[int64]struct{}{openaiID: {}}}
	actual, _, changed, err := (apiKeyRouteRuntime{}).ensureInitial(c, func(candidate *service.APIKey) error {
		return rejectAPIKeyRouteUnsupportedModel(c, diag, candidate, "company-grok")
	})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, grokID, actual.Group.ID)
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

func TestAPIKeyRouteSingleGroupPreservesLegacyRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	group := &service.Group{ID: 81, Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard}
	key := &service.APIKey{ID: 80, RouteVersion: 1, GroupID: &group.ID, Group: group, User: &service.User{ID: 80},
		ScheduleMode: service.APIKeyScheduleModeSequential, RoutingMinSuccessRate: 95,
		GroupRoutes: []service.APIKeyGroupRoute{{GroupID: group.ID, Enabled: true, Group: group}}}
	plan, err := service.NewAPIKeyRouteCoordinator(true).BuildPlan(key, nil)
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	middleware2.SetAPIKeyRouteState(c, &middleware2.APIKeyRouteState{Plan: plan, Order: []int{0}, InitialGroupID: group.ID})
	c.Set(string(middleware2.ContextKeyAPIKey), key)
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

func TestActivateStickyIgnoresMissingGroupAndKeepsFailoverOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ids := []int64{61, 62, 63}
	routes := make([]service.APIKeyGroupRoute, 0, len(ids))
	for priority, id := range ids {
		routes = append(routes, service.APIKeyGroupRoute{
			GroupID: id, Priority: priority, Enabled: true,
			Group: &service.Group{ID: id, Status: service.StatusActive, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard},
		})
	}
	apiKey := &service.APIKey{ID: 60, GroupID: &ids[1], Group: routes[1].Group, RouteVersion: 5, GroupRoutes: routes}
	plan, err := service.NewAPIKeyRouteCoordinator(true).BuildPlan(apiKey, nil)
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	middleware2.SetAPIKeyRouteState(c, &middleware2.APIKeyRouteState{Plan: plan, Order: []int{1, 2}, Index: 1, InitialGroupID: ids[1]})
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)

	actual, _, activated, err := (apiKeyRouteRuntime{}).activateSticky(c, ids[0], nil)
	require.NoError(t, err)
	require.False(t, activated)
	require.Nil(t, actual)

	state, ok := middleware2.GetAPIKeyRouteState(c)
	require.True(t, ok)
	require.Equal(t, []int{1, 2}, state.Order)
	require.Equal(t, 1, state.Index)

	next, advanced := middleware2.AdvanceAPIKeyRoute(c)
	require.True(t, advanced)
	require.Equal(t, ids[2], *next.GroupID)
}

func TestActiveFallbackStickyDrainsUntilThatRouteFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	primaryID, fallbackID := int64(51), int64(52)
	apiKey := &service.APIKey{ID: 49, RouteVersion: 8, ScheduleMode: service.APIKeyScheduleModeSequential,
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

func TestObserveAPIKeyRouteFailureRecordsAfterSemanticOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ids := []int64{71, 72}
	routes := []service.APIKeyGroupRoute{
		{GroupID: ids[0], Priority: 0, Enabled: true, Group: &service.Group{ID: ids[0], Status: service.StatusActive, Platform: service.PlatformOpenAI}},
		{GroupID: ids[1], Priority: 1, Enabled: true, Group: &service.Group{ID: ids[1], Status: service.StatusActive, Platform: service.PlatformOpenAI}},
	}
	apiKey := &service.APIKey{ID: 70, GroupID: &ids[0], Group: routes[0].Group, RouteVersion: 4, GroupRoutes: routes}
	plan, err := service.NewAPIKeyRouteCoordinator(true).BuildPlan(apiKey, nil)
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	middleware2.SetAPIKeyRouteState(c, &middleware2.APIKeyRouteState{Plan: plan, Order: []int{0, 1}, InitialGroupID: ids[0]})
	_, _ = c.Writer.Write([]byte("data: {\"type\":\"response.output_text.delta\"}\n\n"))
	require.False(t, apiKeyRouteFailureAllowsAdvanceBeforeSemanticOutput(c, service.ErrNoAvailableAccounts))

	var recordedGroup int64
	state, observeErr := observeAPIKeyRouteFailure(c, apiKey, "gpt-5", "/v1/responses", &service.UpstreamFailoverError{StatusCode: 502},
		func(_ context.Context, _, _, groupID int64, _, _ string, _ error) (string, error) {
			recordedGroup = groupID
			return service.APIKeyRouteBreakerOpen, nil
		})
	require.NoError(t, observeErr)
	require.Equal(t, service.APIKeyRouteBreakerOpen, state)
	require.Equal(t, ids[0], recordedGroup)
	routeState, _ := middleware2.GetAPIKeyRouteState(c)
	require.True(t, routeState.StickyBroken)
}

func TestAPIKeyRouteAdvanceErrorOnlyClassifiesSubscriptionAndBillingFailuresAsBilling(t *testing.T) {
	capabilityErr := &apiKeyRouteAdvanceError{Last: service.ErrNoEligibleAPIKeyRoute}
	require.False(t, isAPIKeyRouteAdvanceBillingError(capabilityErr))

	subscriptionErr := &apiKeyRouteAdvanceError{Last: service.ErrSubscriptionExpired, Billing: true}
	require.True(t, isAPIKeyRouteAdvanceBillingError(subscriptionErr))
	require.ErrorIs(t, subscriptionErr, service.ErrSubscriptionExpired)
}
