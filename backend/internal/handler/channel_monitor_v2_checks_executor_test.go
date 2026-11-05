package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type probeTestKeys struct{ key *service.APIKey }

func (k probeTestKeys) GroupCheckInternalKey(context.Context, int64, int64) (*service.APIKey, error) {
	return k.key, nil
}

type probeTestGateway struct {
	called      *bool
	body        string
	contentType string
}

func (g probeTestGateway) RunGroupCheck(context.Context, *service.APIKey, int64, string, string) (*http.Response, error) {
	*g.called = true
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{g.contentType}}, Body: io.NopCloser(strings.NewReader(g.body))}, nil
}
func TestGroupCheckExecutorValidatesKeyAndParsesSVG(t *testing.T) {
	id := int64(4)
	called := false
	key := &service.APIKey{ID: 9, UserID: 3, GroupID: &id, Group: &service.Group{ID: id}}
	e := groupCheckExecutor{keys: probeTestKeys{key}, gateway: probeTestGateway{&called, `{"choices":[{"message":{"content":"<svg></svg>"}}]}`, "application/json"}}
	svg, err := e.execute(context.Background(), 3, 4, "pelican", "model")
	require.NoError(t, err)
	require.Equal(t, "<svg></svg>", svg)
	require.True(t, called)
	called = false
	_, err = e.execute(context.Background(), 3, 5, "pelican", "model")
	require.Error(t, err)
	require.False(t, called)
}

type probeIdentityStore struct {
	requestID string
	keyID     int64
	fail      bool
	result    repository.ChannelGroupProbeResult
}

func (s *probeIdentityStore) SetBillingIdentity(_ context.Context, _, _, _, keyID int64, requestID string) error {
	if s.fail {
		return errors.New("database unavailable")
	}
	s.keyID, s.requestID = keyID, requestID
	return nil
}
func (s *probeIdentityStore) Complete(_ context.Context, _, _, _ int64, result repository.ChannelGroupProbeResult) error {
	s.result = result
	return nil
}

type probeIdentityGateway struct {
	store  *probeIdentityStore
	called bool
}

func (g *probeIdentityGateway) RunGroupCheck(ctx context.Context, key *service.APIKey, _ int64, _, _ string) (*http.Response, error) {
	g.called = true
	if g.store.keyID != key.ID || g.store.requestID != "client:"+ctx.Value(ctxkey.ClientRequestID).(string) {
		return nil, errors.New("billing identity was not persisted before gateway")
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"OK"}}]}`))}, nil
}
func TestGroupCheckRunPersistsUniqueIdentityBeforeBilling(t *testing.T) {
	groupID := int64(4)
	key := &service.APIKey{ID: 9, UserID: 3, GroupID: &groupID, Group: &service.Group{ID: groupID}}
	store := &probeIdentityStore{}
	gateway := &probeIdentityGateway{store: store}
	e := groupCheckExecutor{keys: probeTestKeys{key}, gateway: gateway}
	record := &repository.ChannelGroupProbeRecord{ID: 1, UserID: 3, GroupID: 4, Kind: "connectivity", Model: "model"}
	e.run(store, record)
	require.True(t, gateway.called)
	require.Equal(t, "succeeded", store.result.Status)
	first := store.requestID
	e.run(store, record)
	require.NotEqual(t, first, store.requestID)
	store.fail = true
	gateway.called = false
	e.run(store, record)
	require.False(t, gateway.called)
	require.Equal(t, "failed", store.result.Status)
}

func TestGroupCheckClientRequestIDSurvivesMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.NewString()
	r := gin.New()
	r.Use(middleware.ClientRequestID())
	r.POST("/", func(c *gin.Context) {
		require.Equal(t, id, c.Request.Context().Value(ctxkey.ClientRequestID))
		c.Status(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.ClientRequestID, id))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, id, w.Header().Get("X-Client-Request-ID"))
}

func TestGroupCheckResponseTextStreamError(t *testing.T) {
	text, err := groupCheckResponseText([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\ndata: [DONE]\n"), "text/event-stream")
	require.NoError(t, err)
	require.Equal(t, "OK", text)
	_, err = groupCheckResponseText([]byte("data: {\"error\":{\"message\":\"bad\"}}\n"), "text/event-stream")
	require.Error(t, err)
}
