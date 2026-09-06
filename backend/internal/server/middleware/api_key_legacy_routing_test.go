//go:build unit

package middleware

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func legacyRoutingTestService(key *service.APIKey) (*service.APIKeyService, *config.Config) {
	cfg := &config.Config{RunMode: config.RunModeStandard}
	repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil }}
	return service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg), cfg
}

func legacyRoutingTestKey() *service.APIKey {
	g := middlewareRouteGroup(11, service.StatusActive)
	return &service.APIKey{ID: 9, UserID: 7, Key: "legacy-test-key", Status: service.StatusActive,
		User: &service.User{ID: 7, Status: service.StatusActive, Balance: 10}, GroupID: &g.ID, Group: g, RouteVersion: 1,
		GroupRoutes: []service.APIKeyGroupRoute{{GroupID: g.ID, Group: g, Enabled: true}}}
}

func TestAPIKeyLegacyRoutingKeepsAuthResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, google := range []bool{false, true} {
		for _, scenario := range []struct {
			name, message string
			status        int
		}{
			{"active", "", 200}, {"disabled", "API Key 所属分组已停用", 403},
			{"deleted", "API Key 所属分组已删除", 403}, {"permission_revoked", "API Key 所属专属分组不再允许当前用户使用", 403},
		} {
			t.Run(scenario.name, func(t *testing.T) {
				key := legacyRoutingTestKey()
				switch scenario.name {
				case "disabled":
					key.Group.Status = "disabled"
				case "deleted":
					key.Group = nil
					key.GroupRoutes[0].Group = nil
				case "permission_revoked":
					key.Group.IsExclusive = true
				}
				svc, cfg := legacyRoutingTestService(key)
				auth := apiKeyAuthWithSubscription(svc, nil, cfg)
				if google {
					auth = APIKeyAuthWithSubscriptionGoogle(svc, nil, cfg)
				}
				router := gin.New()
				router.Use(auth)
				router.POST("/test", func(c *gin.Context) {
					got, ok := GetAPIKeyFromContext(c)
					require.True(t, ok)
					require.Equal(t, int64(11), *got.GroupID)
					_, hasRoutingUsage := service.APIKeyRoutingUsageContextFromContext(c.Request.Context())
					require.False(t, hasRoutingUsage)
					c.JSON(200, gin.H{"group_id": *got.GroupID})
				})
				req := httptest.NewRequest("POST", "/test", nil)
				req.Header.Set("x-api-key", key.Key)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				require.Equal(t, scenario.status, w.Code, w.Body.String())
				if scenario.message != "" {
					require.Contains(t, w.Body.String(), scenario.message)
				}
			})
		}
	}
}

func TestAPIKeyLegacyRoutingUsageRetainsExhaustedSubscriptionWithoutMaintenance(t *testing.T) {
	key := legacyRoutingTestKey()
	limit := 1.0
	key.Group.SubscriptionType = service.SubscriptionTypeSubscription
	key.Group.DailyLimitUSD = &limit
	svc, cfg := legacyRoutingTestService(key)
	now := time.Now()
	sub := &service.UserSubscription{ID: 55, UserID: 7, GroupID: 11, Status: service.SubscriptionStatusActive,
		ExpiresAt: now.Add(time.Hour), DailyUsageUSD: 2}
	repo := &stubUserSubscriptionRepo{getActive: func(context.Context, int64, int64) (*service.UserSubscription, error) { return sub, nil },
		activateWindow: func(context.Context, int64, time.Time, time.Time) error {
			t.Fatal("usage query activated billing windows")
			return nil
		}}
	subs := service.NewSubscriptionService(nil, repo, nil, nil, cfg)
	t.Cleanup(subs.Stop)
	router := gin.New()
	router.Use(apiKeyAuthWithSubscription(svc, subs, cfg))
	router.GET("/v1/usage", func(c *gin.Context) {
		got, ok := GetSubscriptionFromContext(c)
		require.True(t, ok)
		require.Equal(t, sub.ID, got.ID)
		require.Equal(t, 2.0, got.DailyUsageUSD)
		require.Nil(t, got.DailyWindowStart)
		c.Status(200)
	})
	req := httptest.NewRequest("GET", "/v1/usage", nil)
	req.Header.Set("x-api-key", key.Key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code, w.Body.String())
}

func TestAPIKeyRoutingSubscriptionFailoverKeepsSelectedGroupForEveryUser(t *testing.T) {
	key := legacyRoutingTestKey()
	key.Group.SubscriptionType = service.SubscriptionTypeSubscription
	fallback := middlewareRouteGroup(12, service.StatusActive)
	fallback.SubscriptionType = service.SubscriptionTypeSubscription
	key.GroupRoutes = append(key.GroupRoutes, service.APIKeyGroupRoute{GroupID: 12, Group: fallback, Priority: 1, Enabled: true})
	svc, cfg := legacyRoutingTestService(key)
	now := time.Now()
	repo := &stubUserSubscriptionRepo{getActive: func(_ context.Context, userID, groupID int64) (*service.UserSubscription, error) {
		if groupID == 11 {
			return nil, service.ErrSubscriptionNotFound
		}
		return &service.UserSubscription{ID: 55, UserID: userID, GroupID: groupID,
			Status: service.SubscriptionStatusActive, ExpiresAt: now.Add(time.Hour),
			DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now}, nil
	}}
	subs := service.NewSubscriptionService(nil, repo, nil, nil, cfg)
	t.Cleanup(subs.Stop)
	router := gin.New()
	router.Use(apiKeyAuthWithSubscription(svc, subs, cfg))
	router.POST("/test", func(c *gin.Context) {
		got, ok := GetAPIKeyFromContext(c)
		require.True(t, ok)
		require.Equal(t, int64(12), *got.GroupID)
		subscription, ok := GetSubscriptionFromContext(c)
		require.True(t, ok)
		require.Equal(t, int64(12), subscription.GroupID)
		state, ok := GetAPIKeyRouteState(c)
		require.True(t, ok)
		require.True(t, state.Plan.RoutingEnabled)
		c.Status(200)
	})
	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("x-api-key", key.Key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, int64(11), *key.GroupID, "shared key must not change")
}

func TestAPIKeyRoutingMissingPrimaryCannotBecomeUnscoped(t *testing.T) {
	key := legacyRoutingTestKey()
	fallback := middlewareRouteGroup(12, service.StatusActive)
	key.GroupID = nil
	key.GroupRoutes = []service.APIKeyGroupRoute{{GroupID: fallback.ID, Group: fallback, Priority: 0, Enabled: true}, {GroupID: 13, Enabled: true, Priority: 1}}
	_, _, err := prepareInitialAPIKeyRoute(key, service.NewAPIKeyRouteCoordinator())
	require.ErrorIs(t, err, service.ErrInvalidAPIKeyRouteSet)
}
