package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
	"github.com/tidwall/gjson"
)

// monitorHTTPClient 共享一个 http.Client，避免每次检测重建 transport。
// 自定义 Transport 在 dial 时强制再次校验 IP，防止 DNS rebinding 绕过 validateEndpoint。
var monitorHTTPClient = newSSRFSafeHTTPClient(monitorRequestTimeout)

// monitorPingHTTPClient 用于 endpoint origin 的 HEAD ping，超时更短。
var monitorPingHTTPClient = newSSRFSafeHTTPClient(monitorPingTimeout)

// newSSRFSafeHTTPClient 返回一个使用 safeDialContext 的 http.Client。
// 仅供监控模块对外发起请求使用——所有目标都应是公网 endpoint。
func newSSRFSafeHTTPClient(timeout time.Duration) *http.Client {
	tr := &http.Transport{
		DialContext:           safeDialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          16,
		IdleConnTimeout:       monitorIdleConnTimeout,
		TLSHandshakeTimeout:   monitorTLSHandshakeTimeout,
		ResponseHeaderTimeout: monitorResponseHeaderTimeout,
	}
	return &http.Client{Timeout: timeout, Transport: servertiming.WrapRoundTripper(tr)}
}

// CheckOptions 承载一次检测的自定义入参。
// 所有字段都是可选（零值即等价于"用默认行为"）。
type CheckOptions struct {
	// APIMode 仅对 OpenAI provider 生效；空串等同 chat_completions。
	APIMode string
	// ExtraHeaders 用户自定义 HTTP 头（merge 到 adapter 默认 headers，用户优先）。
	ExtraHeaders map[string]string
	// BodyOverrideMode: off | merge | replace
	BodyOverrideMode string
	// BodyOverride 在 merge 模式下做浅合并（key 命中黑名单时静默丢弃），
	// 在 replace 模式下直接当作完整 body。
	BodyOverride map[string]any
}

// runCheckForModel 对单个 (provider, model) 做一次完整检测。
// 不返回 error：所有失败都包装进 CheckResult.Status=error/failed。
//
// opts 承载模板 / 监控快照带来的自定义配置。nil 等同于 "off + 无 extra headers"。
func runCheckForModel(ctx context.Context, provider, endpoint, apiKey, model string, opts *CheckOptions) *CheckResult {
	return runCheckForModelWithCall(ctx, provider, model, opts, func(callCtx context.Context, prompt string) (monitorProviderCallResult, error) {
		return callProvider(callCtx, provider, endpoint, apiKey, model, prompt, opts)
	})
}

// runCheckForModelWithCall applies the common challenge validation and result
// normalization to either the legacy direct checker or the shared gateway
// forwarding path. Keeping this layer independent from transport details means
// both paths produce identical monitor history and usage-log fields.
func runCheckForModelWithCall(
	ctx context.Context,
	provider, model string,
	opts *CheckOptions,
	call func(context.Context, string) (monitorProviderCallResult, error),
) *CheckResult {
	res := &CheckResult{
		Model:     model,
		Status:    MonitorStatusError,
		CheckedAt: time.Now(),
	}

	challenge := generateChallenge()
	mode := bodyOverrideMode(opts)

	start := time.Now()
	if call == nil {
		res.Message = "monitor call is not configured"
		return res
	}
	callResult, err := call(ctx, challenge.Prompt)
	latency := time.Since(start)
	latencyMs := int(latency / time.Millisecond)
	res.LatencyMs = &latencyMs
	res.FirstTokenMs = callResult.firstTokenMs
	res.Stream = callResult.stream
	res.TokensPerSecond = monitorTokensPerSecond(callResult.outputTokens, callResult.firstTokenMs, latencyMs)
	res.UpstreamEndpoint = callResult.upstreamEndpoint
	if callResult.usageOK {
		usage := callResult.usage
		res.Usage = &usage
	}

	if err != nil {
		res.Status = MonitorStatusError
		res.Message = truncateMessage(sanitizeErrorMessage(err.Error()))
		return res
	}
	respText, rawBody, statusCode := callResult.extractedText, callResult.rawBody, callResult.status
	if statusCode < 200 || statusCode >= 300 {
		// 错误路径：用 rawBody 而非 respText（gjson textPath 抽取在错误响应里通常为空，
		// 会丢掉真正的上游错误信息，例如 `{"error":{"message":"No available accounts ..."}}`）。
		res.Status = MonitorStatusError
		bodySnippet := truncateForErrorBody(rawBody)
		res.Message = truncateMessage(sanitizeErrorMessage(fmt.Sprintf("upstream HTTP %d: %s", statusCode, bodySnippet)))
		return res
	}

	// Replace 模式：跳过 challenge 校验（用户 body 是静态的，challenge 没法嵌入）。
	// 改用「HTTP 2xx + 响应文本（adapter.textPath 抽取）非空」作为 operational 判定。
	// 响应文本为空则降级为 failed（视为上游回了 200 但没实际内容）。
	if mode == MonitorBodyOverrideModeReplace {
		if strings.TrimSpace(respText) == "" {
			res.Status = MonitorStatusFailed
			res.Message = truncateMessage("replace-mode: upstream returned 2xx with empty text")
			return res
		}
		return finalizeOperationalOrDegraded(res, latency, latencyMs)
	}

	if !validateChallenge(respText, challenge.Expected) {
		res.Status = MonitorStatusFailed
		res.Message = truncateMessage(sanitizeErrorMessage(fmt.Sprintf("challenge mismatch (expected %s, got %q)", challenge.Expected, respText)))
		return res
	}

	return finalizeOperationalOrDegraded(res, latency, latencyMs)
}

// finalizeOperationalOrDegraded 负责走到最后一步的 operational/degraded 判定。
// 拆出来是为了让 runCheckForModel 不超过 30 行。
func finalizeOperationalOrDegraded(res *CheckResult, latency time.Duration, latencyMs int) *CheckResult {
	if latency >= monitorDegradedThreshold {
		res.Status = MonitorStatusDegraded
		res.Message = truncateMessage(fmt.Sprintf("slow response: %dms", latencyMs))
		return res
	}
	res.Status = MonitorStatusOperational
	return res
}

// bodyOverrideMode 归一取 opts.BodyOverrideMode，nil opts / 空串都视为 off。
func bodyOverrideMode(opts *CheckOptions) string {
	if opts == nil || opts.BodyOverrideMode == "" {
		return MonitorBodyOverrideModeOff
	}
	return opts.BodyOverrideMode
}

// pingEndpointOrigin 对 endpoint 的 origin (scheme://host) 发起 HEAD 请求，返回耗时。
// 失败时返回 nil（不影响主状态判定）。
func pingEndpointOrigin(ctx context.Context, endpoint string) *int {
	origin, err := extractOrigin(endpoint)
	if err != nil || origin == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, origin, nil)
	if err != nil {
		return nil
	}
	start := time.Now()
	resp, err := monitorPingHTTPClient.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, monitorPingDiscardMaxBytes))
	ms := int(time.Since(start) / time.Millisecond)
	return &ms
}

// providerAdapter 描述某个 provider 在 challenge 检测中需要的几件事：
//   - 拼出请求路径（含 model 占位）
//   - 序列化请求体
//   - 构造鉴权头
//   - 从响应 JSON 中提取文本（默认按 gjson path；需要时可自定义）
//
// 加新 provider 只需要在 providerAdapters 里增加一个条目，无需触碰 callProvider / validateProvider。
type providerAdapter struct {
	buildPath    func(model string) string
	buildBody    func(model, prompt string) ([]byte, error)
	buildHeaders func(apiKey string) map[string]string
	textPath     string // gjson 提取响应文本的 path
	extractText  func([]byte) string
}

// monitorProviderCallResult carries both the existing end-to-end latency
// measurement and the first visible output timestamp for streaming Responses
// probes. Keeping these separate preserves the meaning of latency_ms while
// allowing the UI to use the same TTFT health bands as usage records.
type monitorProviderCallResult struct {
	extractedText    string
	rawBody          string
	status           int
	stream           bool
	firstTokenMs     *int
	outputTokens     *int
	usage            OpenAIUsage
	usageOK          bool
	upstreamEndpoint string
}

// providerAdapters 全部已支持的 provider。键值即 MonitorProvider* 字符串。
//
//nolint:gochecknoglobals // 适配器表是只读静态数据，初始化后不变更。
var providerAdapters = map[string]providerAdapter{
	MonitorProviderOpenAI: providerOpenAIChatAdapter,
	MonitorProviderGrok:   providerGrokChatAdapter,
	// 国产 3 家（配额模式引入）：均为 OpenAI 兼容 Chat Completions，
	// 仅智谱路径前缀不同（/api/paas/v4/chat/completions）。
	MonitorProviderKimi:     providerKimiChatAdapter,
	MonitorProviderZhipu:    providerZhipuChatAdapter,
	MonitorProviderDeepseek: providerDeepseekChatAdapter,
	MonitorProviderAnthropic: {
		buildPath: func(string) string { return providerAnthropicPath },
		buildBody: func(model, prompt string) ([]byte, error) {
			return json.Marshal(map[string]any{
				"model":      model,
				"messages":   []map[string]string{{"role": "user", "content": prompt}},
				"max_tokens": monitorChallengeMaxTokens,
			})
		},
		buildHeaders: func(apiKey string) map[string]string {
			return map[string]string{
				"x-api-key":         apiKey,
				"anthropic-version": monitorAnthropicAPIVersion,
			}
		},
		extractText: extractAnthropicMonitorText,
	},
	MonitorProviderGemini: {
		// Gemini 把 model 名写在 URL path 上：/v1beta/models/{model}:generateContent
		buildPath: func(model string) string { return fmt.Sprintf(providerGeminiPathTemplate, model) },
		buildBody: func(_, prompt string) ([]byte, error) {
			return json.Marshal(map[string]any{
				"contents": []map[string]any{
					{"parts": []map[string]any{{"text": prompt}}},
				},
				"generationConfig": map[string]any{"maxOutputTokens": monitorChallengeMaxTokens},
			})
		},
		// 使用 x-goog-api-key header 而不是 ?key= query，避免 *url.Error 把 key 回填到错误日志。
		buildHeaders: func(apiKey string) map[string]string {
			return map[string]string{"x-goog-api-key": apiKey}
		},
		textPath: "candidates.0.content.parts.0.text",
	},
}

//nolint:gochecknoglobals // 适配器表是只读静态数据，初始化后不变更。
var providerOpenAIChatAdapter = newOpenAICompatibleChatAdapter(providerOpenAIPath)

//nolint:gochecknoglobals // 适配器表是只读静态数据，初始化后不变更。
var providerGrokChatAdapter = newOpenAICompatibleChatAdapter(providerGrokPath)

//nolint:gochecknoglobals // 适配器表是只读静态数据，初始化后不变更。
var providerKimiChatAdapter = newOpenAICompatibleChatAdapter(providerOpenAIPath)

//nolint:gochecknoglobals // 适配器表是只读静态数据，初始化后不变更。
var providerZhipuChatAdapter = newOpenAICompatibleChatAdapter(providerZhipuPath)

//nolint:gochecknoglobals // 适配器表是只读静态数据，初始化后不变更。
var providerDeepseekChatAdapter = newOpenAICompatibleChatAdapter(providerOpenAIPath)

func newOpenAICompatibleChatAdapter(path string) providerAdapter {
	return providerAdapter{
		buildPath: func(string) string { return path },
		buildBody: func(model, prompt string) ([]byte, error) {
			return json.Marshal(map[string]any{
				"model":      model,
				"messages":   []map[string]string{{"role": "user", "content": prompt}},
				"max_tokens": monitorChallengeMaxTokens,
				"stream":     false,
			})
		},
		buildHeaders: func(apiKey string) map[string]string {
			return map[string]string{"Authorization": "Bearer " + apiKey}
		},
		textPath: "choices.0.message.content",
	}
}

//nolint:gochecknoglobals // 适配器表是只读静态数据，初始化后不变更。
var providerOpenAIResponsesAdapter = providerAdapter{
	buildPath: func(string) string { return providerOpenAIResponsesPath },
	buildBody: func(model, prompt string) ([]byte, error) {
		return json.Marshal(map[string]any{
			"model":             model,
			"instructions":      "You are a channel health-check endpoint. Answer the arithmetic challenge exactly and briefly.",
			"input":             prompt,
			"max_output_tokens": monitorChallengeMaxTokens,
			"stream":            false,
		})
	},
	buildHeaders: func(apiKey string) map[string]string {
		return map[string]string{"Authorization": "Bearer " + apiKey}
	},
	textPath: "output.0.content.0.text",
}

// providerAdapterFor 按 provider + api_mode 选择具体 adapter。
func providerAdapterFor(provider, apiMode string) (providerAdapter, string, bool) {
	if provider == MonitorProviderOpenAI && defaultAPIMode(apiMode) == MonitorAPIModeResponses {
		return providerOpenAIResponsesAdapter, MonitorAPIModeResponses, true
	}
	adapter, ok := providerAdapters[provider]
	return adapter, MonitorAPIModeChatCompletions, ok
}

// callProvider 通过 providerAdapters 分发到具体实现。
// opts 承载用户的自定义 headers / body 覆盖（可为 nil）。
//
// 返回值包含抽取文本、原始响应、HTTP 状态和流式 Responses 的首个可见输出时间。
// 对非流式 provider，firstTokenMs 为空；err 仅表示网络/序列化/流读取错误。
func callProvider(ctx context.Context, provider, endpoint, apiKey, model, prompt string, opts *CheckOptions) (monitorProviderCallResult, error) {
	requestedAPIMode := checkAPIMode(opts)
	if err := validateAPIMode(provider, requestedAPIMode); err != nil {
		return monitorProviderCallResult{}, err
	}
	adapter, apiMode, ok := providerAdapterFor(provider, requestedAPIMode)
	if !ok {
		return monitorProviderCallResult{}, fmt.Errorf("unsupported provider %q", provider)
	}
	body, err := buildRequestBody(adapter, provider, apiMode, model, prompt, opts)
	if err != nil {
		return monitorProviderCallResult{}, err
	}
	// V1 Responses probes intentionally use the same streaming transport as a
	// normal user request. Replace-mode templates may contain stream:false, but
	// measuring TTFT requires overriding that one transport flag.
	if provider == MonitorProviderOpenAI && apiMode == MonitorAPIModeResponses {
		body, err = forceResponsesStreamingBody(body)
		if err != nil {
			return monitorProviderCallResult{}, err
		}
	}
	headers := mergeHeaders(adapter.buildHeaders(apiKey), opts)
	full := joinURL(endpoint, adapter.buildPath(model))
	upstreamEndpoint := adapter.buildPath(model)
	if provider == MonitorProviderOpenAI && apiMode == MonitorAPIModeResponses {
		respBytes, streamText, firstTokenMs, outputTokens, usage, usageOK, status, err := postStreamingResponses(ctx, full, body, headers)
		if err != nil {
			return monitorProviderCallResult{rawBody: string(respBytes), status: status, firstTokenMs: firstTokenMs, outputTokens: outputTokens, usage: usage, usageOK: usageOK, upstreamEndpoint: upstreamEndpoint}, err
		}
		if status >= 200 && status < 300 {
			return monitorProviderCallResult{
				extractedText:    streamText,
				rawBody:          string(respBytes),
				status:           status,
				firstTokenMs:     firstTokenMs,
				outputTokens:     outputTokens,
				usage:            usage,
				usageOK:          usageOK,
				upstreamEndpoint: upstreamEndpoint,
			}, nil
		}
		return monitorProviderCallResult{rawBody: string(respBytes), status: status, outputTokens: outputTokens, usage: usage, usageOK: usageOK, upstreamEndpoint: upstreamEndpoint}, nil
	}
	respBytes, status, err := postRawJSON(ctx, full, body, headers)
	if err != nil {
		return monitorProviderCallResult{rawBody: string(respBytes), status: status, upstreamEndpoint: upstreamEndpoint}, err
	}
	usage, usageOK := extractMonitorUsage(provider, respBytes)
	var outputTokens *int
	if usageOK && usage.OutputTokens > 0 {
		output := usage.OutputTokens
		outputTokens = &output
	}
	return monitorProviderCallResult{
		extractedText:    extractMonitorResponseText(adapter, respBytes),
		rawBody:          string(respBytes),
		status:           status,
		outputTokens:     outputTokens,
		usage:            usage,
		usageOK:          usageOK,
		upstreamEndpoint: upstreamEndpoint,
	}, nil
}

func extractMonitorResponseText(adapter providerAdapter, respBytes []byte) string {
	if adapter.extractText != nil {
		return adapter.extractText(respBytes)
	}
	return gjson.GetBytes(respBytes, adapter.textPath).String()
}

// extractMonitorUsage accepts the usage shapes emitted by the providers used
// by channel monitors. OpenAI-compatible and Anthropic responses use the
// shared token names; Gemini uses usageMetadata with camel-case names.
func extractMonitorUsage(provider string, body []byte) (OpenAIUsage, bool) {
	if provider == MonitorProviderGemini {
		value := gjson.GetBytes(body, "usageMetadata")
		if !value.Exists() || !value.IsObject() {
			return OpenAIUsage{}, false
		}
		usage := OpenAIUsage{
			InputTokens:          int(value.Get("promptTokenCount").Int()),
			OutputTokens:         int(value.Get("candidatesTokenCount").Int()),
			CacheReadInputTokens: int(value.Get("cachedContentTokenCount").Int()),
		}
		return usage, monitorUsageHasTokens(usage)
	}
	usage, ok := extractOpenAIUsageFromJSONBytes(body)
	return usage, ok && monitorUsageHasTokens(usage)
}

func monitorUsageHasTokens(usage OpenAIUsage) bool {
	return usage.InputTokens > 0 || usage.OutputTokens > 0 ||
		usage.CacheCreationInputTokens > 0 || usage.CacheReadInputTokens > 0 ||
		usage.ImageInputTokens > 0 || usage.ImageOutputTokens > 0
}

func mergeMonitorUsage(dst *OpenAIUsage, src OpenAIUsage) {
	if dst == nil {
		return
	}
	if src.InputTokens > 0 {
		dst.InputTokens = src.InputTokens
	}
	if src.OutputTokens > 0 {
		dst.OutputTokens = src.OutputTokens
	}
	if src.CacheCreationInputTokens > 0 {
		dst.CacheCreationInputTokens = src.CacheCreationInputTokens
	}
	if src.CacheReadInputTokens > 0 {
		dst.CacheReadInputTokens = src.CacheReadInputTokens
	}
	if src.ImageInputTokens > 0 {
		dst.ImageInputTokens = src.ImageInputTokens
	}
	if src.ImageOutputTokens > 0 {
		dst.ImageOutputTokens = src.ImageOutputTokens
	}
}

// forceResponsesStreamingBody normalizes a monitor Responses body to the
// streaming transport required for TTFT measurement. It intentionally leaves
// every other user-supplied field untouched.
func forceResponsesStreamingBody(body []byte) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode responses monitor body for streaming: %w", err)
	}
	payload["stream"] = true
	streamBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal responses monitor body for streaming: %w", err)
	}
	return streamBody, nil
}

// postStreamingResponses sends a Responses request with stream=true and
// incrementally parses SSE frames. The returned firstTokenMs starts at the
// same point as the normal gateway's request timer and is set on the first
// visible output event, not on response.created/in_progress preamble events.
func postStreamingResponses(
	ctx context.Context,
	fullURL string,
	payload []byte,
	headers map[string]string,
) (rawBody []byte, streamText string, firstTokenMs *int, outputTokens *int, usage OpenAIUsage, usageOK bool, status int, err error) {
	req, err := newMonitorJSONRequest(ctx, fullURL, payload, headers)
	if err != nil {
		return nil, "", nil, nil, OpenAIUsage{}, false, 0, err
	}
	// Streaming is part of the Responses probe contract; do not let a custom
	// Accept header downgrade the transport to a buffered JSON response.
	req.Header.Set("Accept", "text/event-stream")
	// Start before Do so TTFT includes connection setup and upstream queueing,
	// matching the request timer used by the normal gateway path.
	start := time.Now()
	resp, err := monitorHTTPClient.Do(req)
	if err != nil {
		return nil, "", nil, nil, OpenAIUsage{}, false, 0, fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	status = resp.StatusCode

	if status < 200 || status >= 300 {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, monitorResponseMaxBytes))
		if readErr != nil {
			return body, "", nil, nil, OpenAIUsage{}, false, status, fmt.Errorf("read body: %w", readErr)
		}
		return body, "", nil, nil, OpenAIUsage{}, false, status, nil
	}

	var raw bytes.Buffer
	var text strings.Builder
	var parser openAICompatSSEFrameParser
	var streamErr error
	sawTerminal := false
	hadVisibleOutput := false
	hadTextDelta := false

	processData := func(eventType string, data []byte) {
		trimmed := strings.TrimSpace(string(data))
		if trimmed == "" || trimmed == "[DONE]" {
			if trimmed == "[DONE]" {
				sawTerminal = true
			}
			return
		}
		eventType = effectiveOpenAISSEEventType(data, eventType)
		if parsedUsage, ok := extractOpenAIUsageFromJSONBytes(data); ok && monitorUsageHasTokens(parsedUsage) {
			mergeMonitorUsage(&usage, parsedUsage)
			usageOK = true
		}
		if usageOK && usage.OutputTokens > 0 {
			tokens := usage.OutputTokens
			outputTokens = &tokens
		}
		if openAIStreamDataStartsVisibleOutput(trimmed, eventType) {
			hadVisibleOutput = true
			if firstTokenMs == nil {
				ms := int(time.Since(start).Milliseconds())
				firstTokenMs = &ms
			}
		}
		switch eventType {
		case "response.output_text.delta":
			if delta := gjson.GetBytes(data, "delta").String(); delta != "" {
				_, _ = text.WriteString(delta)
				hadTextDelta = true
			}
		case "response.output_text.done":
			if !hadTextDelta {
				_, _ = text.WriteString(gjson.GetBytes(data, "text").String())
			}
		case "response.completed", "response.done":
			if !hadTextDelta && text.Len() == 0 {
				_, _ = text.WriteString(extractOpenAIResponsesText(data))
			}
		}
		if openAIStreamEventTypeIsTerminal(eventType) {
			sawTerminal = true
		}
		if eventType == "error" || eventType == "response.failed" || eventType == "response.incomplete" || eventType == "response.cancelled" || eventType == "response.canceled" {
			message := extractOpenAISSEErrorMessage(data)
			if message == "" {
				message = eventType
			}
			streamErr = fmt.Errorf("upstream %s: %s", eventType, message)
		}
	}

	processFrame := func(frame openAICompatSSEFrame, ok bool) {
		if !ok {
			return
		}
		data := strings.TrimSpace(frame.Data)
		if data == "" {
			return
		}
		if gjson.Valid(data) {
			processData(frame.EventType, []byte(data))
			return
		}
		for _, line := range strings.Split(data, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && gjson.Valid(line) {
				processData(frame.EventType, []byte(line))
			}
		}
	}

	scanner := bufio.NewScanner(io.LimitReader(resp.Body, monitorResponseMaxBytes))
	scanner.Buffer(make([]byte, 0, 64*1024), defaultMaxLineSize)
	for scanner.Scan() {
		line := scanner.Text()
		_, _ = raw.WriteString(line)
		_ = raw.WriteByte('\n')
		// Scanner strips LF but retains CR from CRLF-delimited SSE frames;
		// normalize before feeding the frame parser so blank lines dispatch.
		line = strings.TrimSuffix(line, "\r")
		frame, ok := parser.AddLine(line)
		processFrame(frame, ok)
	}
	frame, ok := parser.Finish()
	processFrame(frame, ok)
	if scanErr := scanner.Err(); scanErr != nil {
		return raw.Bytes(), text.String(), firstTokenMs, outputTokens, usage, usageOK, status, fmt.Errorf("read streaming response: %w", scanErr)
	}
	if streamErr != nil {
		return raw.Bytes(), text.String(), firstTokenMs, outputTokens, usage, usageOK, status, streamErr
	}
	if !sawTerminal {
		return raw.Bytes(), text.String(), firstTokenMs, outputTokens, usage, usageOK, status, errors.New("streaming response ended without a terminal event")
	}
	if !hadVisibleOutput {
		return raw.Bytes(), text.String(), firstTokenMs, outputTokens, usage, usageOK, status, errors.New("streaming response contained no visible output")
	}
	return raw.Bytes(), strings.TrimSpace(text.String()), firstTokenMs, outputTokens, usage, usageOK, status, nil
}

// monitorTokensPerSecond converts a Responses usage count and the measured
// stream timings into generation throughput. The interval starts at the first
// visible token, so TTFT remains a separate metric. A nil result means the
// upstream did not provide output-token usage or the interval was too short to
// produce a meaningful rate.
func monitorTokensPerSecond(outputTokens *int, firstTokenMs *int, latencyMs int) *float64 {
	if outputTokens == nil || *outputTokens <= 0 || firstTokenMs == nil || latencyMs <= *firstTokenMs {
		return nil
	}
	generationMs := latencyMs - *firstTokenMs
	if generationMs <= 0 {
		return nil
	}
	rate := float64(*outputTokens) * 1000 / float64(generationMs)
	return &rate
}

func newMonitorJSONRequest(ctx context.Context, fullURL string, payload []byte, headers map[string]string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

func extractAnthropicMonitorText(respBytes []byte) string {
	content := gjson.GetBytes(respBytes, "content")
	if !content.IsArray() {
		return ""
	}

	parts := make([]string, 0, 1)
	content.ForEach(func(_, item gjson.Result) bool {
		if item.Get("type").String() != "text" {
			return true
		}
		text := strings.TrimSpace(item.Get("text").String())
		if text != "" {
			parts = append(parts, text)
		}
		return true
	})
	return strings.Join(parts, "\n")
}

// extractOpenAIResponsesText 聚合 Responses API 的最终 assistant 文本。
// Responses 的 output 数组顺序由模型决定：reasoning / tool-call item 可能排在 message 前面，
// 因此不能假设文本永远在 output.0.content.0.text。
func extractOpenAIResponsesText(respBytes []byte) string {
	if text := gjson.GetBytes(respBytes, "output_text").String(); strings.TrimSpace(text) != "" {
		return text
	}

	var texts []string
	outputs := gjson.GetBytes(respBytes, "output")
	if outputs.IsArray() {
		outputs.ForEach(func(_, output gjson.Result) bool {
			outputType := output.Get("type").String()
			if outputType != "" && outputType != "message" {
				return true
			}

			content := output.Get("content")
			if !content.IsArray() {
				return true
			}

			content.ForEach(func(_, block gjson.Result) bool {
				blockType := block.Get("type").String()
				if blockType != "" && blockType != "output_text" {
					return true
				}
				if text := block.Get("text").String(); strings.TrimSpace(text) != "" {
					texts = append(texts, text)
				}
				return true
			})
			return true
		})
	}

	if len(texts) > 0 {
		return strings.Join(texts, "")
	}
	return gjson.GetBytes(respBytes, providerOpenAIResponsesAdapter.textPath).String()
}

// mergeHeaders 把用户自定义 headers 合并到 adapter 默认 headers 上。
// 用户值覆盖默认；命中黑名单（hop-by-hop / 由 http.Client 自管的）的 key 静默丢弃。
func mergeHeaders(base map[string]string, opts *CheckOptions) map[string]string {
	if opts == nil || len(opts.ExtraHeaders) == 0 {
		return base
	}
	out := make(map[string]string, len(base)+len(opts.ExtraHeaders))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range opts.ExtraHeaders {
		if IsForbiddenHeaderName(k) {
			continue
		}
		out[k] = v
	}
	return out
}

// buildRequestBody 根据 body_override_mode 构造请求 body。
//
//   - off:     adapter 默认 body
//   - merge:   adapter 默认 body 与 BodyOverride 浅合并；BodyOverride 中命中
//     bodyMergeKeyDenyList[provider] 的 key 会被静默丢弃，避免破坏 challenge / model 路由
//   - replace: 直接 marshal BodyOverride 作为完整 body
//
// 任何 mode 返回的 []byte 都已经是合法 JSON，可直接送入 postRawJSON。
func buildRequestBody(adapter providerAdapter, provider, apiMode, model, prompt string, opts *CheckOptions) ([]byte, error) {
	mode := bodyOverrideMode(opts)

	if mode == MonitorBodyOverrideModeReplace {
		if opts == nil || len(opts.BodyOverride) == 0 {
			return nil, fmt.Errorf("replace mode: body_override is empty")
		}
		if err := validateReplaceRequestBody(provider, apiMode, opts.BodyOverride); err != nil {
			return nil, err
		}
		body, err := json.Marshal(opts.BodyOverride)
		if err != nil {
			return nil, fmt.Errorf("marshal body_override (replace): %w", err)
		}
		return body, nil
	}

	defaultBody, err := adapter.buildBody(model, prompt)
	if err != nil {
		return nil, fmt.Errorf("marshal default body: %w", err)
	}
	if mode != MonitorBodyOverrideModeMerge || opts == nil || len(opts.BodyOverride) == 0 {
		return defaultBody, nil
	}

	var defaultMap map[string]any
	if err := json.Unmarshal(defaultBody, &defaultMap); err != nil {
		return nil, fmt.Errorf("unmarshal default body for merge: %w", err)
	}
	deny := bodyMergeKeyDenyList[bodyMergeDenyKey(provider, apiMode)]
	for k, v := range opts.BodyOverride {
		if deny[k] {
			continue
		}
		defaultMap[k] = v
	}
	merged, err := json.Marshal(defaultMap)
	if err != nil {
		return nil, fmt.Errorf("marshal merged body: %w", err)
	}
	return merged, nil
}

// bodyMergeKeyDenyList 在 merge 模式下，禁止用户覆盖这些 provider-specific 的关键字段。
// 思路抄 check-cx 的 EXCLUDED_METADATA_KEYS：保护 challenge / model 路由不被用户误伤。
// 用户想动这些字段就用 replace 模式（已知会跳 challenge 校验）。
//
//nolint:gochecknoglobals // 静态查表，初始化后不变。
var bodyMergeKeyDenyList = map[string]map[string]bool{
	MonitorProviderOpenAI + ":" + MonitorAPIModeChatCompletions: {"model": true, "messages": true, "stream": true},
	MonitorProviderOpenAI + ":" + MonitorAPIModeResponses:       {"model": true, "instructions": true, "input": true, "stream": true},
	MonitorProviderGrok:      {"model": true, "messages": true, "stream": true},
	MonitorProviderAnthropic: {"model": true, "messages": true, "stream": true},
	MonitorProviderGemini:    {"contents": true},
	// 国产 3 家与 OpenAI Chat Completions 同构。
	MonitorProviderKimi:     {"model": true, "messages": true, "stream": true},
	MonitorProviderZhipu:    {"model": true, "messages": true, "stream": true},
	MonitorProviderDeepseek: {"model": true, "messages": true, "stream": true},
}

func checkAPIMode(opts *CheckOptions) string {
	if opts == nil {
		return MonitorAPIModeChatCompletions
	}
	return defaultAPIMode(opts.APIMode)
}

func bodyMergeDenyKey(provider, apiMode string) string {
	if provider == MonitorProviderOpenAI {
		return provider + ":" + defaultAPIMode(apiMode)
	}
	return provider
}

// isOpenAICompatibleChatProvider 该 provider 的探活请求是否为 OpenAI Chat
// Completions 同构（replace 模式的 body 校验按 messages 必填处理）。
func isOpenAICompatibleChatProvider(provider string) bool {
	switch provider {
	case MonitorProviderOpenAI, MonitorProviderGrok,
		MonitorProviderKimi, MonitorProviderZhipu, MonitorProviderDeepseek:
		return true
	default:
		return false
	}
}

func validateReplaceRequestBody(provider, apiMode string, body map[string]any) error {
	if !isOpenAICompatibleChatProvider(provider) {
		return nil
	}
	switch defaultAPIMode(apiMode) {
	case MonitorAPIModeResponses:
		if strings.TrimSpace(stringFromAny(body["instructions"])) == "" || !hasNonEmptyBodyValue(body["input"]) {
			return fmt.Errorf("replace mode responses body: instructions and input are required")
		}
	case MonitorAPIModeChatCompletions:
		if !hasNonEmptyBodyValue(body["messages"]) {
			return fmt.Errorf("replace mode chat_completions body: messages are required")
		}
	}
	return nil
}

func stringFromAny(v any) string {
	s, _ := v.(string)
	return s
}

func hasNonEmptyBodyValue(v any) bool {
	switch val := v.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(val) != ""
	case []any:
		return len(val) > 0
	case []map[string]any:
		return len(val) > 0
	case []map[string]string:
		return len(val) > 0
	default:
		return true
	}
}

// postRawJSON 发送 POST + 已序列化好的 JSON 字节，限制响应体大小，返回响应字节、HTTP status、错误。
// adapter 自行 marshal 是为了精确控制字段顺序与类型，所以这里直接收 []byte 而不是 any。
func postRawJSON(ctx context.Context, fullURL string, payload []byte, headers map[string]string) ([]byte, int, error) {
	req, err := newMonitorJSONRequest(ctx, fullURL, payload, headers)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := monitorHTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, monitorResponseMaxBytes))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read body: %w", err)
	}
	return respBody, resp.StatusCode, nil
}

// joinURL 把 base origin 与 path 拼成完整 URL。
// 容忍 base 末尾有/无斜杠，path 必带前导斜杠。
func joinURL(base, path string) string {
	base = strings.TrimRight(base, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

// extractOrigin 从一个 endpoint URL 中提取 scheme://host[:port] 部分。
func extractOrigin(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", errors.New("endpoint missing scheme or host")
	}
	return u.Scheme + "://" + u.Host, nil
}

// monitorSensitiveQueryParamRegex 匹配 URL query 中可能泄露凭证的参数：
// key / api_key / api-key / access_token / token / authorization / x-api-key。
// 大小写不敏感，匹配 `?name=value` 或 `&name=value` 形式（value 截到 & 或字符串末尾）。
var monitorSensitiveQueryParamRegex = regexp.MustCompile(`(?i)([?&](?:key|api[_-]?key|access[_-]?token|token|authorization|x-api-key)=)[^&\s"']+`)

// monitorAPIKeyPatterns 匹配常见 provider 的 API key 字面量。
// 顺序敏感：sk-ant- 必须放在 sk- 之前，否则会被通用 sk- 模式先消费。
var monitorAPIKeyPatterns = []struct {
	pattern *regexp.Regexp
	replace string
}{
	// Anthropic（带前缀，必须先匹配）：sk-ant-xxxxxxx
	{regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{20,}`), "sk-ant-***REDACTED***"},
	// OpenAI / Anthropic 通用 sk-: sk-xxxxxxx
	{regexp.MustCompile(`sk-[A-Za-z0-9-]{20,}`), "sk-***REDACTED***"},
	// xAI API Key：xai-xxxxxxx
	{regexp.MustCompile(`xai-[A-Za-z0-9_-]{6,}`), "xai-***REDACTED***"},
	// Gemini / Google API Key：固定前缀 + 35 位
	{regexp.MustCompile(`AIza[A-Za-z0-9_-]{35}`), "AIza***REDACTED***"},
	// JWT 三段式（Bearer 后常出现）：eyJxxx.eyJxxx.signature
	{regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`), "eyJ***REDACTED.JWT***"},
}

// sanitizeErrorMessage 擦除错误/响应文本中可能泄露的 API key。
// 处理两类来源：
//  1. URL query 中的 ?key= / ?api_key= 等（Go *url.Error 会回填完整 URL）
//  2. 上游 HTTP body 文本里直接出现的 sk-* / xai-* / AIza* / JWT 等密钥碎片
//
// 注意：与 gemini_messages_compat_service.go 的 sanitizeUpstreamErrorMessage 关注点类似但参数集更广，
// 监控模块独立维护，避免互相耦合。
func sanitizeErrorMessage(msg string) string {
	if msg == "" {
		return msg
	}
	msg = monitorSensitiveQueryParamRegex.ReplaceAllString(msg, `${1}REDACTED`)
	for _, p := range monitorAPIKeyPatterns {
		msg = p.pattern.ReplaceAllString(msg, p.replace)
	}
	return msg
}

// truncateMessage 把消息按 monitorMessageMaxBytes 截断，避免 DB 列溢出与日志过长。
func truncateMessage(msg string) string {
	if len(msg) <= monitorMessageMaxBytes {
		return msg
	}
	const ellipsis = "...(truncated)"
	cutoff := monitorMessageMaxBytes - len(ellipsis)
	if cutoff < 0 {
		cutoff = 0
	}
	return msg[:cutoff] + ellipsis
}

// truncateForErrorBody 把上游错误响应 body 压到 monitorErrorBodySnippetMaxBytes 以内，
// 并顺手把连续空白折成一个空格：上游 HTML 错误页常含大量缩进/换行，保留会浪费预算。
// 被 truncateMessage 做最终总截断兜底，所以这里只负责 body 自身的精简。
func truncateForErrorBody(body string) string {
	body = strings.Join(strings.Fields(body), " ")
	if len(body) <= monitorErrorBodySnippetMaxBytes {
		return body
	}
	const ellipsis = "...(body truncated)"
	cutoff := monitorErrorBodySnippetMaxBytes - len(ellipsis)
	if cutoff < 0 {
		cutoff = 0
	}
	return body[:cutoff] + ellipsis
}
