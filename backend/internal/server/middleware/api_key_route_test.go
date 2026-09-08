package middleware

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func middlewareRouteGroup(id int64, status string) *service.Group {
	return &service.Group{
		ID: id, Name: "group", Platform: service.PlatformOpenAI,
		SubscriptionType: service.SubscriptionTypeStandard, Status: status,
	}
}

func TestPrepareInitialAPIKeyRouteAndAdvance(t *testing.T) {
	first := middlewareRouteGroup(10, "disabled")
	second := middlewareRouteGroup(20, service.StatusActive)
	third := middlewareRouteGroup(30, service.StatusActive)
	firstID := first.ID
	key := &service.APIKey{
		ID: 1, GroupID: &firstID, Group: first, RouteVersion: 7,
		User: &service.User{ID: 2, Status: service.StatusActive},
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 10, Priority: 0, Enabled: true, Group: first},
			{GroupID: 20, Priority: 1, Enabled: true, Group: second},
			{GroupID: 30, Priority: 2, Enabled: true, Group: third},
		},
	}

	routed, state, err := prepareInitialAPIKeyRoute(key, service.NewAPIKeyRouteCoordinator(true))
	require.NoError(t, err)
	require.Equal(t, int64(20), *routed.GroupID)
	require.Equal(t, 0, state.Index)
	require.Equal(t, int64(10), *key.GroupID, "shared auth snapshot must remain unchanged")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Set(string(ContextKeyAPIKey), routed)
	SetAPIKeyRouteState(c, state)

	next, ok := AdvanceAPIKeyRoute(c)
	require.True(t, ok)
	require.Equal(t, int64(30), *next.GroupID)
	stored, ok := GetAPIKeyFromContext(c)
	require.True(t, ok)
	require.Same(t, next, stored)
	_, ok = AdvanceAPIKeyRoute(c)
	require.False(t, ok)
}

func TestPrepareInitialAPIKeyRouteAlwaysUsesConfiguredCandidates(t *testing.T) {
	first := middlewareRouteGroup(10, service.StatusActive)
	second := middlewareRouteGroup(20, service.StatusActive)
	firstID := first.ID
	key := &service.APIKey{
		ID: 1, GroupID: &firstID, Group: first, User: &service.User{ID: 2, Status: service.StatusActive},
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 10, Priority: 0, Enabled: true, Group: first},
			{GroupID: 20, Priority: 1, Enabled: true, Group: second},
		},
	}

	routed, state, err := prepareInitialAPIKeyRoute(key, service.NewAPIKeyRouteCoordinator())
	require.NoError(t, err)
	require.Equal(t, int64(10), *routed.GroupID)
	require.NotNil(t, state)
	require.True(t, state.Plan.RoutingEnabled)
}

func TestAPIKeyRouteStickyBreakPropagatesToFinalRoutingContext(t *testing.T) {
	first := middlewareRouteGroup(10, service.StatusActive)
	second := middlewareRouteGroup(20, service.StatusActive)
	firstID := first.ID
	key := &service.APIKey{
		ID: 1, GroupID: &firstID, Group: first, RouteVersion: 7,
		User: &service.User{ID: 2, Status: service.StatusActive},
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 10, Priority: 0, Enabled: true, Group: first},
			{GroupID: 20, Priority: 1, Enabled: true, Group: second},
		},
	}
	routed, state, err := prepareInitialAPIKeyRoute(key, service.NewAPIKeyRouteCoordinator(true))
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Set(string(ContextKeyAPIKey), routed)
	SetAPIKeyRouteState(c, state)
	firstStartedAt := state.AttemptStartedAt
	time.Sleep(time.Millisecond)
	MarkAPIKeyRouteStickySelected(c)

	_, ok := AdvanceAPIKeyRoute(c)
	require.True(t, ok)
	meta, ok := service.APIKeyRoutingUsageContextFromContext(c.Request.Context())
	require.True(t, ok)
	require.True(t, meta.StickyBroken)
	require.Equal(t, 1, meta.SwitchCount)
	require.False(t, service.IsForceCacheBilling(c.Request.Context()))
	require.True(t, meta.AttemptStartedAt.After(firstStartedAt))
}

func TestLockAPIKeyRoutePreventsDetachedRequestFromChangingPhysicalGroup(t *testing.T) {
	first := middlewareRouteGroup(10, service.StatusActive)
	second := middlewareRouteGroup(20, service.StatusActive)
	firstID := first.ID
	key := &service.APIKey{
		ID: 1, GroupID: &firstID, Group: first, RouteVersion: 7,
		User: &service.User{ID: 2, Status: service.StatusActive},
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 10, Priority: 0, Enabled: true, Group: first},
			{GroupID: 20, Priority: 1, Enabled: true, Group: second},
		},
	}
	routed, state, err := prepareInitialAPIKeyRoute(key, service.NewAPIKeyRouteCoordinator(true))
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/images/generations/async", nil)
	c.Set(string(ContextKeyAPIKey), routed)
	SetAPIKeyRouteState(c, state)

	LockAPIKeyRoute(c)
	require.True(t, state.Locked)
	_, ok := AdvanceAPIKeyRoute(c)
	require.False(t, ok)
	_, ok = RejectInitialAPIKeyRoute(c)
	require.False(t, ok)
	_, ok = ActivateInitialAPIKeyRouteIndex(c, 1)
	require.False(t, ok)
	stored, ok := GetAPIKeyFromContext(c)
	require.True(t, ok)
	require.Equal(t, int64(10), *stored.GroupID)
}

func TestActivateAPIKeyRouteForPlatformPinsMatchingMixedGroup(t *testing.T) {
	openai := middlewareRouteGroup(10, service.StatusActive)
	grok := middlewareRouteGroup(20, service.StatusActive)
	grok.Platform = service.PlatformGrok
	openaiID := openai.ID
	key := &service.APIKey{
		ID: 1, GroupID: &openaiID, Group: openai, RouteVersion: 7,
		User: &service.User{ID: 2, Status: service.StatusActive},
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 10, Priority: 0, Enabled: true, Group: openai},
			{GroupID: 20, Priority: 1, Enabled: true, Group: grok},
		},
	}
	routed, state, err := prepareInitialAPIKeyRoute(key, service.NewAPIKeyRouteCoordinator(true))
	require.NoError(t, err)
	require.Equal(t, int64(10), *routed.GroupID)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Set(string(ContextKeyAPIKey), routed)
	SetAPIKeyRouteState(c, state)

	actual, ok := FilterAPIKeyRouteOrder(c, func(group *service.Group) bool {
		return group != nil && group.Platform == service.PlatformGrok
	})
	require.True(t, ok)
	require.Equal(t, int64(20), *actual.GroupID)
	require.Zero(t, state.SwitchCount)
	require.Equal(t, int64(20), state.InitialGroupID)
	require.Equal(t, []int{1}, state.Order)
}

func TestFilterAPIKeyRouteOrderKeepsUserOrderAmongSupportingGroups(t *testing.T) {
	grok := middlewareRouteGroup(10, service.StatusActive)
	grok.Platform = service.PlatformGrok
	first := middlewareRouteGroup(20, service.StatusActive)
	second := middlewareRouteGroup(30, service.StatusActive)
	grokID := grok.ID
	key := &service.APIKey{
		ID: 1, GroupID: &grokID, Group: grok, RouteVersion: 7,
		User: &service.User{ID: 2, Status: service.StatusActive},
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 10, Priority: 0, Enabled: true, Group: grok},
			{GroupID: 20, Priority: 1, Enabled: true, Group: first},
			{GroupID: 30, Priority: 2, Enabled: true, Group: second},
		},
	}
	routed, state, err := prepareInitialAPIKeyRoute(key, service.NewAPIKeyRouteCoordinator(true))
	require.NoError(t, err)
	require.Equal(t, int64(10), *routed.GroupID)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Set(string(ContextKeyAPIKey), routed)
	SetAPIKeyRouteState(c, state)

	actual, ok := FilterAPIKeyRouteOrder(c, func(group *service.Group) bool {
		return group != nil && group.Platform == service.PlatformOpenAI
	})
	require.True(t, ok)
	require.Equal(t, int64(20), *actual.GroupID)
	require.Equal(t, []int{1, 2}, state.Order)
	require.Zero(t, state.SwitchCount)
}

func TestFindAPIKeyRouteGroupIgnoresFilteredStickyTarget(t *testing.T) {
	first := middlewareRouteGroup(10, service.StatusActive)
	second := middlewareRouteGroup(20, service.StatusActive)
	third := middlewareRouteGroup(30, service.StatusActive)
	firstID := first.ID
	key := &service.APIKey{
		ID: 1, GroupID: &firstID, Group: first, RouteVersion: 7,
		User: &service.User{ID: 2, Status: service.StatusActive},
		GroupRoutes: []service.APIKeyGroupRoute{
			{GroupID: 10, Priority: 0, Enabled: true, Group: first},
			{GroupID: 20, Priority: 1, Enabled: true, Group: second},
			{GroupID: 30, Priority: 2, Enabled: true, Group: third},
		},
	}
	routed, state, err := prepareInitialAPIKeyRoute(key, service.NewAPIKeyRouteCoordinator(true))
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Set(string(ContextKeyAPIKey), routed)
	SetAPIKeyRouteState(c, state)
	_, ok := FilterAPIKeyRouteOrder(c, func(group *service.Group) bool {
		return group != nil && group.ID != 10
	})
	require.True(t, ok)

	_, _, found := FindAPIKeyRouteGroup(c, 10)
	require.False(t, found)
	candidate, index, found := FindAPIKeyRouteGroup(c, 20)
	require.True(t, found)
	require.Equal(t, 1, index)
	require.Equal(t, int64(20), *candidate.GroupID)
	_, _, found = FindAPIKeyRouteGroup(c, 99)
	require.False(t, found)
}
