package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGroupCheckGatewayRejectsChangedIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(42)
	key := &service.APIKey{ID: 1, UserID: 2, GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformAnthropic}}
	a := &groupCheckGatewayAdapter{gateway: &GatewayHandler{}, auth: func(*service.APIKey) gin.HandlerFunc {
		return func(c *gin.Context) {
			require.Equal(t, groupID, service.GroupCheckPinnedGroup(c.Request.Context()))
			other := *key
			other.ID++
			c.Set(string(middleware.ContextKeyAPIKey), &other)
			c.Next()
		}
	}}
	resp, err := a.RunGroupCheck(context.Background(), key, groupID, "model", "hello")
	require.NoError(t, err)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestGroupCheckGatewayRequiresInternalAuthenticationAndRejectsComposite(t *testing.T) {
	groupID := int64(42)
	key := &service.APIKey{ID: 1, UserID: 2, GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformAnthropic}}
	a := &groupCheckGatewayAdapter{gateway: &GatewayHandler{}}
	_, err := a.RunGroupCheck(context.Background(), key, groupID, "model", "hello")
	require.Error(t, err)
	a.auth = func(*service.APIKey) gin.HandlerFunc {
		return func(c *gin.Context) { c.AbortWithStatus(http.StatusPaymentRequired) }
	}
	resp, err := a.RunGroupCheck(context.Background(), key, groupID, "model", "hello")
	require.NoError(t, err)
	require.Equal(t, http.StatusPaymentRequired, resp.StatusCode)
	key.Group.Platform = service.PlatformComposite
	_, err = a.RunGroupCheck(context.Background(), key, groupID, "model", "hello")
	require.Error(t, err)
}
