package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// groupCheckGatewayAdapter requires an internal authentication middleware that
// reloads the persisted identity and performs the normal user, group, balance
// and subscription checks. It must not accept credentials from HTTP clients.
// Composite groups are deliberately rejected until their route middleware is
// available here. The authentication dependency is intentionally mandatory.
type groupCheckGatewayAdapter struct {
	auth    func(*service.APIKey) gin.HandlerFunc
	gateway *GatewayHandler
	openAI  *OpenAIGatewayHandler
}

func (a *groupCheckGatewayAdapter) RunGroupCheck(ctx context.Context, key *service.APIKey, groupID int64, model, prompt string) (*http.Response, error) {
	if a == nil || a.auth == nil || key == nil || key.ID <= 0 || key.UserID <= 0 || key.GroupID == nil || *key.GroupID != groupID || key.Group == nil || key.Group.ID != groupID {
		return nil, errors.New("invalid group check gateway dependencies")
	}
	_, knownPlatform := domain.LookupPlatform(key.Group.Platform)
	if !knownPlatform || key.Group.Platform == service.PlatformComposite {
		return nil, errors.New("unsupported group check platform")
	}
	useOpenAI := domain.UsesOpenAIGateway(key.Group.Platform)
	if (useOpenAI && a.openAI == nil) || (!useOpenAI && a.gateway == nil) {
		return nil, errors.New("group check gateway unavailable")
	}
	body, err := json.Marshal(gin.H{"model": model, "messages": []gin.H{{"role": "user", "content": prompt}}, "max_tokens": 2048, "stream": false})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(service.WithGroupCheckPinnedGroup(ctx, groupID), http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	auth := a.auth(key)
	if auth == nil {
		return nil, errors.New("internal authentication unavailable")
	}
	r := gin.New()
	r.POST("/v1/chat/completions", middleware.ClientRequestID(), InboundEndpointMiddleware(), auth, func(c *gin.Context) {
		authKey, ok := middleware.GetAPIKeyFromContext(c)
		if !ok || authKey == nil || authKey.ID != key.ID || authKey.UserID != key.UserID || authKey.GroupID == nil || *authKey.GroupID != groupID || authKey.Group == nil || authKey.Group.ID != groupID {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		middleware.LockAPIKeyRoute(c)
		c.Next()
	}, middleware.GroupModelAllowlist(), func(c *gin.Context) {
		if useOpenAI {
			a.openAI.ChatCompletions(c)
		} else {
			a.gateway.ChatCompletions(c)
		}
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if w.Body.Len() > 2_000_000 {
		return nil, fmt.Errorf("group check response too large")
	}
	resp := w.Result()
	resp.Body = io.NopCloser(bytes.NewReader(w.Body.Bytes()))
	return resp, nil
}
