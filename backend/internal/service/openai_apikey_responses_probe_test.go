package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestProbeOpenAIAPIKeyResponsesSupportUsesCodexProbeHeaders(t *testing.T) {
	updateCalls := make(chan map[string]any, 1)
	account := Account{
		ID:          96,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://compat-upstream.example/v1",
		},
	}
	repo := &snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		updateExtraCalls:      updateCalls,
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"output":[{"type":"function_call","name":"probe_ping"}]}`)),
	}}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
	}

	svc.ProbeOpenAIAPIKeyResponsesSupport(context.Background(), account.ID)

	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "https://compat-upstream.example/v1/responses", upstream.lastReq.URL.String())
	requireOpenAICodexProbeHeaders(t, upstream.lastReq.Header)
	updates := <-updateCalls
	require.Equal(t, true, updates[openai_compat.ExtraKeyResponsesSupported])
}

func TestProbeOpenAIAPIKeyResponsesSupportCNProviders(t *testing.T) {
	tests := []struct {
		name        string
		id          int64
		platform    string
		protocol    string
		wantSupport bool
		wantMode    string
	}{
		{name: "deepseek adaptive supports responses", id: 201, platform: PlatformDeepseek, protocol: APIProtocolAdaptive, wantSupport: true, wantMode: string(openai_compat.ResponsesSupportModeForceResponses)},
		{name: "deepseek chat clears forced responses", id: 202, platform: PlatformDeepseek, protocol: APIProtocolChatCompletions, wantSupport: false, wantMode: string(openai_compat.ResponsesSupportModeAuto)},
		{name: "kimi adaptive falls back to chat", id: 203, platform: PlatformKimi, protocol: APIProtocolAdaptive, wantSupport: false, wantMode: string(openai_compat.ResponsesSupportModeAuto)},
		{name: "zhipu adaptive falls back to chat", id: 204, platform: PlatformZhipu, protocol: APIProtocolAdaptive, wantSupport: false, wantMode: string(openai_compat.ResponsesSupportModeAuto)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			updateCalls := make(chan map[string]any, 1)
			account := Account{
				ID: tc.id, Platform: tc.platform, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "sk-test", "api_protocol": tc.protocol},
				Extra: map[string]any{
					openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses),
				},
			}
			repo := &snapshotUpdateAccountRepo{
				stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
				updateExtraCalls:      updateCalls,
			}
			svc := &AccountTestService{accountRepo: repo}

			svc.ProbeOpenAIAPIKeyResponsesSupport(context.Background(), account.ID)

			updates := <-updateCalls
			require.Equal(t, tc.wantSupport, updates[openai_compat.ExtraKeyResponsesSupported])
			require.Equal(t, tc.wantMode, updates[openai_compat.ExtraKeyResponsesMode])
		})
	}
}

func TestDecideResponsesProbeSupport(t *testing.T) {
	fnCall := []byte(`{"output":[{"type":"reasoning"},{"type":"function_call","name":"probe_ping"}]}`)
	reasoningOnly := []byte(`{"output":[{"type":"reasoning"}]}`)

	cases := []struct {
		name   string
		status int
		body   []byte
		want   bool
	}{
		// Endpoint clearly absent on third-party OpenAI-compatible upstreams.
		{"404 endpoint absent", 404, fnCall, false},
		{"405 method not allowed", 405, fnCall, false},
		// 2xx: tool capability is judged by presence of a function_call output item.
		{"200 with function_call", 200, fnCall, true},
		// Volcengine Ark coding/v3 × kimi-k2.6: reasoning only, no function_call.
		{"200 reasoning only", 200, reasoningOnly, false},
		{"200 invalid json", 200, []byte("not-json"), false},
		{"200 no output field", 200, []byte(`{"status":"completed"}`), false},
		// Non-2xx (other than 404/405): endpoint exists, capability undecidable -> conservative true.
		{"400 conservative true", 400, reasoningOnly, true},
		{"401 conservative true", 401, nil, true},
		{"500 conservative true", 500, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, decideResponsesProbeSupport(tc.status, tc.body))
		})
	}
}

func TestResponsesProbeBodyHasFunctionCall(t *testing.T) {
	require.True(t, responsesProbeBodyHasFunctionCall([]byte(`{"output":[{"type":"function_call"}]}`)))
	require.True(t, responsesProbeBodyHasFunctionCall([]byte(`{"output":[{"type":"reasoning"},{"type":"function_call"}]}`)))
	require.False(t, responsesProbeBodyHasFunctionCall([]byte(`{"output":[{"type":"reasoning"}]}`)))
	require.False(t, responsesProbeBodyHasFunctionCall([]byte(`{"output":[]}`)))
	require.False(t, responsesProbeBodyHasFunctionCall([]byte(`{}`)))
	require.False(t, responsesProbeBodyHasFunctionCall([]byte(`garbage`)))
}

func TestSelectResponsesProbeModel(t *testing.T) {
	// No model_mapping -> fall back to DefaultTestModel (OpenAI official APIKey).
	require.Equal(t, openai.DefaultTestModel, selectResponsesProbeModel(&Account{}))

	// model_mapping values are upstream models; pick first by sort for reproducibility.
	acct := &Account{Credentials: map[string]any{
		"model_mapping": map[string]any{
			"client-b": "zeta-model",
			"client-a": "alpha-model",
		},
	}}
	require.Equal(t, "alpha-model", selectResponsesProbeModel(acct))

	// Wildcard / blank upstream values are skipped.
	acctWild := &Account{Credentials: map[string]any{
		"model_mapping": map[string]any{
			"a": "*",
			"b": "  ",
			"c": "real-model",
		},
	}}
	require.Equal(t, "real-model", selectResponsesProbeModel(acctWild))

	// Only wildcard mappings -> DefaultTestModel.
	acctAllWild := &Account{Credentials: map[string]any{
		"model_mapping": map[string]any{"a": "gpt-*"},
	}}
	require.Equal(t, openai.DefaultTestModel, selectResponsesProbeModel(acctAllWild))
}

func probeHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestProbeOpenAIAPIKeyEndpointSupportPersistsIndependentCapabilities(t *testing.T) {
	account := newResponsesProbeAccount(4300)
	updates := make(chan map[string]any, 1)
	repo := &snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		updateExtraCalls:      updates,
	}
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		probeHTTPResponse(http.StatusOK, `{"id":"chatcmpl_probe","choices":[]}`),
		probeHTTPResponse(http.StatusOK, `{"status":"completed","output":[{"type":"function_call","name":"probe_ping"}]}`),
	}}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
	}

	svc.ProbeOpenAIAPIKeyEndpointSupport(context.Background(), account.ID)

	got := <-updates
	require.Equal(t, true, got[openai_compat.ExtraKeyChatCompletionsSupported])
	require.Equal(t, true, got[openai_compat.ExtraKeyResponsesSupported])
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "https://compat-upstream.example/v1/chat/completions", upstream.requests[0].URL.String())
	require.Equal(t, "https://compat-upstream.example/v1/responses", upstream.requests[1].URL.String())
	require.Equal(t, "gpt-5.4", gjson.GetBytes(upstream.bodies[0], "model").String())
	require.Equal(t, "user", gjson.GetBytes(upstream.bodies[0], "messages.#(role==\"user\").role").String())
}

func TestProbeOpenAIAPIKeyEndpointSupportDoesNotCoupleEndpointResults(t *testing.T) {
	cases := []struct {
		name          string
		chatStatus    int
		chatBody      string
		responsesBody string
		responsesCode int
		wantChat      bool
		wantResponses bool
	}{
		{
			name:          "chat absent responses present",
			chatStatus:    http.StatusNotFound,
			chatBody:      `{"error":{"message":"Not Found"}}`,
			responsesCode: http.StatusOK,
			responsesBody: `{"status":"completed","output":[{"type":"function_call"}]}`,
			wantChat:      false,
			wantResponses: true,
		},
		{
			name:          "chat present responses absent",
			chatStatus:    http.StatusOK,
			chatBody:      `{"choices":[]}`,
			responsesCode: http.StatusMethodNotAllowed,
			responsesBody: `{"error":{"message":"Method Not Allowed"}}`,
			wantChat:      true,
			wantResponses: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			account := newResponsesProbeAccount(4301)
			updates := make(chan map[string]any, 1)
			repo := &snapshotUpdateAccountRepo{
				stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
				updateExtraCalls:      updates,
			}
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				probeHTTPResponse(tc.chatStatus, tc.chatBody),
				probeHTTPResponse(tc.responsesCode, tc.responsesBody),
			}}
			svc := &AccountTestService{
				accountRepo:  repo,
				httpUpstream: upstream,
				cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
			}

			svc.ProbeOpenAIAPIKeyEndpointSupport(context.Background(), account.ID)

			got := <-updates
			require.Equal(t, tc.wantChat, got[openai_compat.ExtraKeyChatCompletionsSupported])
			require.Equal(t, tc.wantResponses, got[openai_compat.ExtraKeyResponsesSupported])
		})
	}
}

func TestProbeOpenAIAPIKeyEndpointSupportNetworkFailureKeepsFlagsUnknown(t *testing.T) {
	account := newResponsesProbeAccount(4302)
	updates := make(chan map[string]any, 1)
	repo := &snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		updateExtraCalls:      updates,
	}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: &httpUpstreamRecorder{err: errors.New("dial failed")},
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
	}

	svc.ProbeOpenAIAPIKeyEndpointSupport(context.Background(), account.ID)

	select {
	case got := <-updates:
		t.Fatalf("network failure must not persist capability flags: %#v", got)
	default:
	}
}
