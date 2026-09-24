package service

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/typesafe"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountTestService_TypeSafeUsesOfficialSystemOneExample(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/7/test", nil)

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"model":"jev-latest","answers":{"is_urgent":{"type":"noul","noul":0.99}},"usage":{"input_tokens":12,"output_tokens":3}}`)),
	}}
	svc := &AccountTestService{
		accountRepo:  nil,
		httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled: false,
		}}},
	}
	account := &Account{
		ID:       7,
		Platform: PlatformTypeSafe,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "typesafe-secret",
		},
	}

	err := svc.testTypeSafeAccountConnection(c, account, typesafe.JevLatestModel, "")
	require.NoError(t, err)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "https://api.typesafe.ai/v1/systemone", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer typesafe-secret", upstream.lastReq.Header.Get("Authorization"))

	var body map[string]any
	require.NoError(t, json.Unmarshal(upstream.lastBody, &body))
	require.Equal(t, "Help! My payouts have been failing for 3 days.", body["state"])
	require.Equal(t, typesafe.JevLatestModel, body["model"])
	require.Equal(t, map[string]any{
		"type":         "noul",
		"instructions": "Does this convey urgency?",
	}, body["questions"].(map[string]any)["is_urgent"])
	require.Contains(t, recorder.Body.String(), `"type":"test_start"`)
	require.Contains(t, recorder.Body.String(), `TypeSafe System One succeeded`)
	require.Contains(t, recorder.Body.String(), `"success":true`)
}
