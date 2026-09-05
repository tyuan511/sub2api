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

func legacyRoutingTestService(key *service.APIKey, enabled, allowed bool) (*service.APIKeyService, *config.Config) {
	cfg := &config.Config{RunMode: config.RunModeStandard, Gateway: config.GatewayConfig{APIKeyMultiGroupRoutingEnabled: enabled}}
	repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil }}
	svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
	rollout := `{"user_ids":[]}`
	if allowed { rollout = `{"user_ids":[7]}` }
	svc.SetRoutingRolloutSettings(service.NewSettingService(&fakeSettingRepo{values: map[string]string{service.SettingKeyAPIKeyRoutingRollout: rollout}}, cfg))
	return svc, cfg
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
		protocol := "anthropic_openai"
		if google { protocol = "google" }
		for _, scenario := range []struct { name, message string; status int }{
			{"active", "", 200}, {"disabled", "API Key 所属分组已停用", 403},
			{"deleted", "API Key 所属分组已删除", 403}, {"permission_revoked", "API Key 所属专属分组不再允许当前用户使用", 403},
		} {
			t.Run(protocol+"/"+scenario.name, func(t *testing.T) {
				var baseline string
				for _, mode := range []string{"disabled", "empty_allowlist", "withdrawn_multi", "listed_single"} {
					key := legacyRoutingTestKey()
					switch scenario.name {
					case "disabled": key.Group.Status = "disabled"
					case "deleted": key.Group = nil; key.GroupRoutes[0].Group = nil
					case "permission_revoked": key.Group.IsExclusive = true
					}
					if mode == "withdrawn_multi" {
						fallback := middlewareRouteGroup(12, service.StatusActive)
						key.GroupRoutes = append(key.GroupRoutes, service.APIKeyGroupRoute{GroupID: 12, Group: fallback, Priority: 1, Enabled: true})
					}
					svc, cfg := legacyRoutingTestService(key, mode != "disabled", mode == "listed_single")
					auth := apiKeyAuthWithSubscription(svc, nil, cfg)
					if google { auth = APIKeyAuthWithSubscriptionGoogle(svc, nil, cfg) }
					router := gin.New()
					router.Use(auth)
					router.POST("/test", func(c *gin.Context) {
						got, ok := GetAPIKeyFromContext(c); require.True(t, ok)
						require.Equal(t, int64(11), *got.GroupID)
						_, hasRoutingUsage := service.APIKeyRoutingUsageContextFromContext(c.Request.Context())
						require.False(t, hasRoutingUsage)
						c.JSON(200, gin.H{"group_id": *got.GroupID})
					})
					req := httptest.NewRequest("POST", "/test", nil); req.Header.Set("x-api-key", key.Key)
					w := httptest.NewRecorder(); router.ServeHTTP(w, req)
					require.Equal(t, scenario.status, w.Code, "%s: %s", mode, w.Body.String())
					if scenario.message != "" { require.Contains(t, w.Body.String(), scenario.message) }
					if mode == "disabled" { baseline = w.Body.String() } else { require.JSONEq(t, baseline, w.Body.String(), mode) }
				}
			})
		}
	}
}

func TestAPIKeyLegacyRoutingUsageRetainsExhaustedSubscriptionWithoutMaintenance(t *testing.T) {
	for _, mode := range []string{"disabled", "empty_allowlist", "withdrawn_multi", "listed_multi"} {
		t.Run(mode, func(t *testing.T) {
			key := legacyRoutingTestKey()
			limit := 1.0
			key.Group.SubscriptionType = service.SubscriptionTypeSubscription
			key.Group.DailyLimitUSD = &limit
			if mode == "withdrawn_multi" || mode == "listed_multi" {
				fallback := middlewareRouteGroup(12, service.StatusActive)
				key.GroupRoutes = append(key.GroupRoutes, service.APIKeyGroupRoute{GroupID: 12, Group: fallback, Priority: 1, Enabled: true})
			}
			svc, cfg := legacyRoutingTestService(key, mode != "disabled", mode == "listed_multi")
			now := time.Now()
			sub := &service.UserSubscription{ID: 55, UserID: 7, GroupID: 11, Status: service.SubscriptionStatusActive,
				ExpiresAt: now.Add(time.Hour), DailyUsageUSD: 2}
			// Nil windows would require activation on a billable request. A read
			// must neither activate them nor discard the exhausted subscription.
			repo := &stubUserSubscriptionRepo{getActive: func(context.Context, int64, int64) (*service.UserSubscription, error) { return sub, nil },
				activateWindow: func(context.Context, int64, time.Time, time.Time) error { t.Fatal("usage query activated billing windows"); return nil }}
			subs := service.NewSubscriptionService(nil, repo, nil, nil, cfg); t.Cleanup(subs.Stop)
			router := gin.New(); router.Use(apiKeyAuthWithSubscription(svc, subs, cfg))
			router.GET("/v1/usage", func(c *gin.Context) {
				got, ok := GetSubscriptionFromContext(c); require.True(t, ok); require.NotNil(t, got)
				require.Equal(t, sub.ID, got.ID); require.Equal(t, 2.0, got.DailyUsageUSD); require.Nil(t, got.DailyWindowStart)
				c.Status(200)
			})
			req := httptest.NewRequest("GET", "/v1/usage", nil); req.Header.Set("x-api-key", key.Key)
			w := httptest.NewRecorder(); router.ServeHTTP(w, req); require.Equal(t, 200, w.Code, w.Body.String())
		})
	}
}

func TestAPIKeyRoutingListedSubscriptionFailoverKeepsSelectedGroup(t *testing.T) {
	for _, google := range []bool{false, true} {
		key := legacyRoutingTestKey()
		key.Group.SubscriptionType = service.SubscriptionTypeSubscription
		fallback := middlewareRouteGroup(12, service.StatusActive)
		key.GroupRoutes = append(key.GroupRoutes, service.APIKeyGroupRoute{GroupID: 12, Group: fallback, Priority: 1, Enabled: true})
		svc, cfg := legacyRoutingTestService(key, true, true)
		repo := &stubUserSubscriptionRepo{getActive: func(context.Context, int64, int64) (*service.UserSubscription, error) { return nil, service.ErrSubscriptionNotFound }}
		subs := service.NewSubscriptionService(nil, repo, nil, nil, cfg); t.Cleanup(subs.Stop)
		auth := apiKeyAuthWithSubscription(svc, subs, cfg)
		if google { auth = APIKeyAuthWithSubscriptionGoogle(svc, subs, cfg) }
		router := gin.New(); router.Use(auth)
		router.POST("/test", func(c *gin.Context) {
			got, ok := GetAPIKeyFromContext(c); require.True(t, ok); require.Equal(t, int64(12), *got.GroupID)
			state, ok := GetAPIKeyRouteState(c); require.True(t, ok); require.True(t, state.Plan.RoutingEnabled)
			c.Status(200)
		})
		req := httptest.NewRequest("POST", "/test", nil); req.Header.Set("x-api-key", key.Key)
		w := httptest.NewRecorder(); router.ServeHTTP(w, req); require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, int64(11), *key.GroupID, "shared key must not change")
	}
}

func TestAPIKeyRoutingWithdrawnMissingPrimaryCannotBecomeUnscoped(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		key := legacyRoutingTestKey()
		fallback := middlewareRouteGroup(12, service.StatusActive)
		key.GroupRoutes = []service.APIKeyGroupRoute{{GroupID: 12, Group: fallback, Enabled: true}, {GroupID: 13, Enabled: true}}
		svc, _ := legacyRoutingTestService(key, enabled, false)
		projected := svc.ProjectAPIKeyRoutingForUser(context.Background(), key)
		_, _, err := prepareInitialAPIKeyRoute(projected, service.NewAPIKeyRouteCoordinator(enabled))
		require.ErrorIs(t, err, service.ErrInvalidAPIKeyRouteSet)
	}
}
