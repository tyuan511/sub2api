package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// channelMonitorContextKey marks requests created by the active monitor. The
// marker lets shared gateway code retain its normal protocol/retry behavior
// while suppressing account-health and billing side effects for the synthetic
// account used by a monitor.
type channelMonitorContextKey struct{}

func withChannelMonitorContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, channelMonitorContextKey{}, true)
}

// IsChannelMonitorContext reports whether a gateway call belongs to a monitor
// probe. It is exported for the few shared side-effect helpers that need to
// avoid persisting health state for the synthetic account.
func IsChannelMonitorContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(channelMonitorContextKey{}).(bool)
	return v
}

// ChannelMonitorGatewayForwarder routes active probes through the ordinary
// OpenAI/Anthropic/Gemini gateway services. It deliberately does not call
// RecordUsage or any handler-level billing code; ChannelMonitorService writes
// one informational usage_logs row after the probe completes.
type ChannelMonitorGatewayForwarder struct {
	gatewayService              *GatewayService
	openAIGatewayService        *OpenAIGatewayService
	geminiMessagesCompatService *GeminiMessagesCompatService
}

func NewChannelMonitorGatewayForwarder(
	gatewayService *GatewayService,
	openAIGatewayService *OpenAIGatewayService,
	geminiMessagesCompatService *GeminiMessagesCompatService,
) *ChannelMonitorGatewayForwarder {
	return &ChannelMonitorGatewayForwarder{
		gatewayService:              gatewayService,
		openAIGatewayService:        openAIGatewayService,
		geminiMessagesCompatService: geminiMessagesCompatService,
	}
}

func (f *ChannelMonitorGatewayForwarder) ForwardMonitor(
	ctx context.Context,
	m *ChannelMonitor,
	model, prompt string,
	opts *CheckOptions,
) (monitorProviderCallResult, error) {
	if f == nil || m == nil {
		return monitorProviderCallResult{}, errors.New("channel monitor gateway forwarder is not configured")
	}
	if strings.TrimSpace(m.APIKey) == "" {
		return monitorProviderCallResult{}, errors.New("monitor api key is empty")
	}
	provider := strings.TrimSpace(m.Provider)
	apiMode := defaultAPIMode(m.APIMode)
	body, err := buildSharedMonitorBody(provider, apiMode, model, prompt, opts)
	if err != nil {
		return monitorProviderCallResult{}, err
	}
	account := newSyntheticMonitorAccount(m, apiMode, opts)
	requestCtx := withChannelMonitorContext(ctx)
	path := monitorSharedInboundPath(provider, apiMode)
	ginCtx, recorder := newMonitorGatewayContext(requestCtx, path, m.ExtraHeaders)

	var (
		callErr       error
		openAIResult  *OpenAIForwardResult
		gatewayResult *ForwardResult
	)
	switch provider {
	case MonitorProviderAnthropic:
		if f.gatewayService == nil {
			return monitorProviderCallResult{}, errors.New("anthropic gateway service is not configured")
		}
		parsed, parseErr := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
		if parseErr != nil {
			return monitorProviderCallResult{}, parseErr
		}
		gatewayResult, callErr = f.gatewayService.Forward(requestCtx, ginCtx, account, parsed)
	case MonitorProviderGemini:
		if f.geminiMessagesCompatService == nil {
			return monitorProviderCallResult{}, errors.New("gemini compatibility service is not configured")
		}
		// Gemini's normal public compatibility route is Chat Completions. The
		// compatibility service then performs the same CC→Anthropic→Gemini
		// conversion as a real user request.
		gatewayResult, callErr = f.geminiMessagesCompatService.ForwardAsChatCompletions(requestCtx, ginCtx, account, body)
	case MonitorProviderOpenAI, MonitorProviderGrok, MonitorProviderKimi, MonitorProviderZhipu, MonitorProviderDeepseek:
		if f.openAIGatewayService == nil {
			return monitorProviderCallResult{}, errors.New("openai gateway service is not configured")
		}
		if provider == MonitorProviderOpenAI && apiMode == MonitorAPIModeResponses {
			openAIResult, callErr = f.openAIGatewayService.Forward(requestCtx, ginCtx, account, body)
		} else {
			openAIResult, callErr = f.openAIGatewayService.ForwardAsChatCompletions(requestCtx, ginCtx, account, body, "", "")
		}
	default:
		return monitorProviderCallResult{}, fmt.Errorf("unsupported shared monitor provider %q", provider)
	}

	result := monitorProviderCallResult{
		status:  recorder.Code,
		rawBody: recorder.Body.String(),
		stream:  true,
	}
	if result.status == 0 {
		result.status = http.StatusBadGateway
	}
	if openAIResult != nil {
		result.firstTokenMs = openAIResult.FirstTokenMs
		result.usage = openAIResult.Usage
		result.usageOK = monitorUsageHasTokens(result.usage)
		result.outputTokens = monitorOutputTokens(result.usage)
		result.upstreamEndpoint = strings.TrimSpace(openAIResult.UpstreamEndpoint)
		if result.upstreamEndpoint == "" {
			result.upstreamEndpoint = GetActualOpenAIUpstreamEndpoint(ginCtx)
		}
		result.extractedText = extractSharedMonitorText(provider, apiMode, []byte(result.rawBody))
		result.stream = openAIResult.Stream
	}
	if gatewayResult != nil {
		result.firstTokenMs = gatewayResult.FirstTokenMs
		result.usage = claudeUsageToOpenAIUsage(&gatewayResult.Usage)
		result.usageOK = monitorUsageHasTokens(result.usage)
		result.outputTokens = monitorOutputTokens(result.usage)
		result.upstreamEndpoint = monitorSharedUpstreamEndpoint(provider, ginCtx)
		result.extractedText = extractSharedMonitorText(provider, apiMode, []byte(result.rawBody))
		result.stream = gatewayResult.Stream
	}
	if result.status < 100 || result.status > 599 {
		result.status = http.StatusBadGateway
	}
	if callErr != nil {
		var failoverErr *UpstreamFailoverError
		if errors.As(callErr, &failoverErr) {
			if failoverErr.StatusCode > 0 {
				result.status = failoverErr.StatusCode
			}
			if len(failoverErr.ResponseBody) > 0 {
				result.rawBody = string(failoverErr.ResponseBody)
			}
		}
		return result, callErr
	}
	return result, nil
}

func monitorOutputTokens(usage OpenAIUsage) *int {
	if usage.OutputTokens <= 0 {
		return nil
	}
	n := usage.OutputTokens
	return &n
}

func monitorPlatform(provider string) string {
	switch provider {
	case MonitorProviderAnthropic:
		return PlatformAnthropic
	case MonitorProviderGemini:
		return PlatformGemini
	case MonitorProviderGrok:
		return PlatformGrok
	case MonitorProviderKimi:
		return PlatformKimi
	case MonitorProviderZhipu:
		return PlatformZhipu
	case MonitorProviderDeepseek:
		return PlatformDeepseek
	default:
		return PlatformOpenAI
	}
}

func newSyntheticMonitorAccount(m *ChannelMonitor, apiMode string, opts *CheckOptions) *Account {
	credentials := map[string]any{
		"api_key":  strings.TrimSpace(m.APIKey),
		"base_url": strings.TrimSpace(m.Endpoint),
	}
	provider := strings.TrimSpace(m.Provider)
	if IsCNProvider(provider) {
		credentials["api_protocol"] = APIProtocolChatCompletions
	}
	if len(m.ExtraHeaders) > 0 {
		overrides := make(map[string]any, len(m.ExtraHeaders))
		for key, value := range m.ExtraHeaders {
			overrides[key] = value
		}
		credentials["header_override_enabled"] = true
		credentials["header_overrides"] = overrides
	}
	extra := map[string]any{}
	if provider == MonitorProviderOpenAI {
		if apiMode == MonitorAPIModeResponses {
			extra[openai_compat.ExtraKeyResponsesMode] = string(openai_compat.ResponsesSupportModeForceResponses)
		} else {
			extra[openai_compat.ExtraKeyResponsesMode] = string(openai_compat.ResponsesSupportModeForceChatCompletions)
		}
	}
	// A synthetic ID is intentionally zero. All persistent monitor auditing is
	// performed by persistMonitorUsageLogs, never by normal billing handlers.
	return &Account{
		ID:          0,
		Name:        "channel-monitor",
		Platform:    monitorPlatform(provider),
		Type:        AccountTypeAPIKey,
		Credentials: credentials,
		Extra:       extra,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    0,
	}
}

func buildSharedMonitorBody(provider, apiMode, model, prompt string, opts *CheckOptions) ([]byte, error) {
	if provider == MonitorProviderGemini {
		// The Gemini compatibility service accepts Anthropic-style messages only
		// through Forward; its public OpenAI route is handled by
		// ForwardAsChatCompletions, so use the same CC challenge body here.
		adapter := providerOpenAIChatAdapter
		body, err := buildRequestBody(adapter, MonitorProviderOpenAI, MonitorAPIModeChatCompletions, model, prompt, opts)
		if err != nil {
			return nil, err
		}
		return forceMonitorStreamBody(body)
	}
	adapter, effectiveMode, ok := providerAdapterFor(provider, apiMode)
	if !ok {
		return nil, fmt.Errorf("unsupported shared monitor provider %q", provider)
	}
	body, err := buildRequestBody(adapter, provider, effectiveMode, model, prompt, opts)
	if err != nil {
		return nil, err
	}
	return forceMonitorStreamBody(body)
}

func forceMonitorStreamBody(body []byte) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode monitor body for streaming: %w", err)
	}
	payload["stream"] = true
	return json.Marshal(payload)
}

func monitorSharedInboundPath(provider, apiMode string) string {
	switch {
	case provider == MonitorProviderAnthropic:
		return "/v1/messages"
	case provider == MonitorProviderOpenAI && apiMode == MonitorAPIModeResponses:
		return "/v1/responses"
	default:
		return "/v1/chat/completions"
	}
}

func monitorSharedUpstreamEndpoint(provider string, c *gin.Context) string {
	if provider == MonitorProviderOpenAI || provider == MonitorProviderGrok || IsCNProvider(provider) {
		if endpoint := GetActualOpenAIUpstreamEndpoint(c); endpoint != "" {
			return endpoint
		}
	}
	switch provider {
	case MonitorProviderAnthropic:
		return "/v1/messages"
	case MonitorProviderGemini:
		return "/v1beta/models/*:streamGenerateContent"
	default:
		return "/v1/chat/completions"
	}
}

func newMonitorGatewayContext(ctx context.Context, path string, headers map[string]string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://channel-monitor.local"+path, bytes.NewReader(nil))
	if err != nil {
		req = &http.Request{Header: make(http.Header)}
	}
	req.Header.Set("User-Agent", "sub2api-channel-monitor")
	for key, value := range headers {
		if strings.TrimSpace(key) != "" {
			req.Header.Set(key, value)
		}
	}
	c.Request = req
	return c, recorder
}

// extractSharedMonitorText handles the wire formats emitted by all shared
// gateway services: JSON, OpenAI-compatible SSE, and Anthropic-compatible SSE
// (also used by Gemini's compatibility response).
func extractSharedMonitorText(provider, apiMode string, body []byte) string {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return ""
	}
	if gjson.ValidBytes(trimmed) {
		switch provider {
		case MonitorProviderAnthropic, MonitorProviderGemini:
			if text := extractAnthropicMonitorText(trimmed); text != "" {
				return text
			}
		case MonitorProviderOpenAI:
			if apiMode == MonitorAPIModeResponses {
				if text := extractOpenAIResponsesText(trimmed); text != "" {
					return text
				}
			}
		}
		if text := gjson.GetBytes(trimmed, "choices.0.message.content").String(); text != "" {
			return text
		}
		if text := gjson.GetBytes(trimmed, "choices.0.text").String(); text != "" {
			return text
		}
	}

	var parser openAICompatSSEFrameParser
	var text strings.Builder
	for _, line := range strings.Split(string(body), "\n") {
		frame, ok := parser.AddLine(strings.TrimSuffix(line, "\r"))
		if !ok {
			continue
		}
		appendSharedMonitorFrameText(&text, frame.Data, frame.EventType)
	}
	if frame, ok := parser.Finish(); ok {
		appendSharedMonitorFrameText(&text, frame.Data, frame.EventType)
	}
	return strings.TrimSpace(text.String())
}

func appendSharedMonitorFrameText(out *strings.Builder, raw, eventType string) {
	data := strings.TrimSpace(raw)
	if data == "" || data == "[DONE]" || !gjson.Valid(data) {
		return
	}
	if strings.TrimSpace(eventType) == "" {
		eventType = effectiveOpenAISSEEventType([]byte(data), "")
	}
	for _, path := range []string{
		"delta",
		"choices.0.delta.content",
		"choices.0.text",
		"choices.0.message.content",
		"text",
	} {
		value := gjson.Get(data, path)
		if value.Type == gjson.String && value.String() != "" {
			// Responses emits output_text.done with the complete text after one
			// or more delta events. The deltas are already appended; adding the
			// done payload here would duplicate the challenge answer.
			if eventType == "response.output_text.done" && path == "text" {
				continue
			}
			// A Responses event has a top-level delta string; an Anthropic event
			// stores it under delta.text, handled separately below.
			if path == "delta" && eventType != "response.output_text.delta" {
				continue
			}
			_, _ = out.WriteString(value.String())
		}
	}
	if value := gjson.Get(data, "delta.text"); value.Type == gjson.String {
		_, _ = out.WriteString(value.String())
	}
	if value := gjson.Get(data, "content_block.text"); value.Type == gjson.String {
		_, _ = out.WriteString(value.String())
	}
	if eventType == "response.completed" || eventType == "response.done" {
		if text := extractOpenAIResponsesText([]byte(data)); text != "" && out.Len() == 0 {
			_, _ = out.WriteString(text)
		}
	}
}
