//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type legacyUngroupedSettings struct{ service.SettingRepository }

func (*legacyUngroupedSettings) GetValue(_ context.Context, key string) (string, error) {
	if key == service.SettingKeyAllowUngroupedKeyScheduling {
		return "true", nil
	}
	return "", service.ErrSettingNotFound
}

type legacyStickyCache struct {
	service.GatewayCache
	t                  *testing.T
	groupID, accountID int64
	reads              int
}

func (s *legacyStickyCache) GetSessionAccountID(_ context.Context, groupID int64, _ string) (int64, error) {
	if s.groupID > 0 {
		// The warmup path may use the API-key namespace before the request's
		// physical group is resolved; both are valid legacy cache lookups.
		require.Positive(s.t, groupID)
	}
	s.reads++
	if s.reads > 1 {
		return 0, errors.New("Redis became unavailable after prefetch")
	}
	return s.accountID, nil
}
func (*legacyStickyCache) SetSessionAccountID(context.Context, int64, string, int64, time.Duration) error {
	return nil
}
func (*legacyStickyCache) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}
func (*legacyStickyCache) DeleteSessionAccountID(context.Context, int64, string) error { return nil }

func TestGatewayLegacyRoutingPreservesUngroupedAndStickySelection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"ungrouped", "fixed"} {
		t.Run(mode, func(t *testing.T) {
			var group *service.Group
			var groupID *int64
			if strings.HasPrefix(mode, "fixed") {
				group = &service.Group{ID: 11, Hydrated: true, Platform: service.PlatformAnthropic, Status: service.StatusActive}
				groupID = &group.ID
			}
			account := &service.Account{ID: 101, Name: "legacy-sticky", Platform: service.PlatformAntigravity,
				Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "test-token", "intercept_warmup_requests": true},
				Extra: map[string]any{"mixed_scheduling": true}, Concurrency: 1, Priority: 10, Status: service.StatusActive, Schedulable: true}
			if group != nil {
				account.AccountGroups = []service.AccountGroup{{AccountID: account.ID, GroupID: group.ID}}
			}
			cache := &legacyStickyCache{t: t, accountID: account.ID}
			if group != nil {
				cache.groupID = group.ID
			}
			h, cleanup := newTestGatewayHandler(t, group, []*service.Account{account}, cache)
			t.Cleanup(cleanup)
			h.cfg = &config.Config{RunMode: config.RunModeSimple}
			settings := service.NewSettingService(&legacyUngroupedSettings{}, h.cfg)
			h.settingService = settings
			key := &service.APIKey{ID: 9, UserID: 7, GroupID: groupID, Group: group, RouteVersion: 1,
				Status: service.StatusActive, User: &service.User{ID: 7, Status: service.StatusActive, Concurrency: 1, Balance: 10}}
			if group != nil {
				key.GroupRoutes = []service.APIKeyGroupRoute{{GroupID: group.ID, Group: group, Enabled: true}}
			}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAPIKey), key)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7, Concurrency: 1})
				c.Next()
				selected, ok := c.Get(opsAccountIDKey)
				require.True(t, ok)
				require.Equal(t, account.ID, selected)
			})
			router.Use(middleware.RequireGroupAssignment(settings, middleware.AnthropicErrorWriter))
			router.POST("/v1/messages", h.Messages)
			body := `{"model":"claude-sonnet-4-5","max_tokens":256,"messages":[{"role":"user","content":[{"type":"text","text":"Warmup"}]}]}`
			req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), "New Conversation")
			require.GreaterOrEqual(t, cache.reads, 1, "legacy scheduling should retain its existing sticky lookup path")
		})
	}
}
