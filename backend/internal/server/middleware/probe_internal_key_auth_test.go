//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestProbeInternalKeyRejectedBeforeLookup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	calls := 0
	repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) {
		calls++
		return nil, service.ErrAPIKeyNotFound
	}}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
	for _, header := range []string{"Authorization", "x-api-key", "x-goog-api-key"} {
		r := gin.New()
		r.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)))
		r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		value := "sub2api-internal-probe-test"
		if header == "Authorization" {
			value = "Bearer " + value
		}
		req.Header.Set(header, value)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Code)
	}
	require.Zero(t, calls)
}
