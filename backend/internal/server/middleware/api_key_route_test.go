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

func TestPrepareInitialAPIKeyRouteDisabledKeepsPrimary(t *testing.T) {
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

	routed, state, err := prepareInitialAPIKeyRoute(key, service.NewAPIKeyRouteCoordinator(false))
	require.NoError(t, err)
	require.Same(t, key, routed)
	require.Nil(t, state)
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
	require.True(t, service.IsForceCacheBilling(c.Request.Context()))
	require.True(t, service.IsAPIKeyGroupCacheCompensation(c.Request.Context()))
	require.Equal(t, service.DefaultAPIKeyGroupCacheCompensationMaxTokens, meta.CacheCompensationMaxTokens)
	require.Equal(t, service.DefaultAPIKeyGroupCacheCompensationMaxSwitches, meta.CacheCompensationMaxSwitches)
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
