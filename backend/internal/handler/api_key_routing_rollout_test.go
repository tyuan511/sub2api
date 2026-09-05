package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type routingCapabilitySettingsRepo struct{ service.SettingRepository }

func (*routingCapabilitySettingsRepo) GetValue(context.Context, string) (string, error) {
	return `{"user_ids":[42]}`, nil
}

func TestAPIKeyRoutingCapabilitiesArePrivateAndUserScoped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{Gateway: config.GatewayConfig{APIKeyMultiGroupRoutingEnabled: true}}
	svc := service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg)
	svc.SetRoutingRolloutSettings(service.NewSettingService(&routingCapabilitySettingsRepo{}, cfg))
	h := NewAPIKeyHandler(svc)
	for _, id := range []int64{0, 7, 42} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("GET", "/keys/routing-capabilities?user_id=42", nil)
		if id > 0 {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: id})
		}
		h.GetRoutingCapabilities(c)
		if id == 0 {
			require.Equal(t, 401, rec.Code)
			continue
		}
		require.Equal(t, 200, rec.Code)
		var response struct {
			Data map[string]bool `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		require.Equal(t, map[string]bool{"multi_group_routing_enabled": id == 42}, response.Data)
		require.NotContains(t, rec.Body.String(), "user_ids")
	}
}

func TestAPIKeyRoutingHTTPRejectsSpoofedUserAndAdvancedFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{Gateway: config.GatewayConfig{APIKeyMultiGroupRoutingEnabled: true}}
	svc := service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg)
	svc.SetRoutingRolloutSettings(service.NewSettingService(&routingCapabilitySettingsRepo{}, cfg))
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
	c.Request = httptest.NewRequest("POST", "/keys", bytes.NewBufferString(`{"name":"not-listed","user_id":42,"group_routes":[{"group_id":11,"priority":0},{"group_id":12,"priority":1}],"schedule_mode":"smart","smart_preference":"balanced"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	NewAPIKeyHandler(svc).Create(c)
	require.Equal(t, 403, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "API_KEY_ROUTING_NOT_ENABLED")
}
