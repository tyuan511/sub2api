// Command api-key-routing-bench runs a bounded OpenAI-compatible streaming
// chat benchmark for the API-key multi-group routing release gate.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const apiKeyEnvironment = "SUB2API_BENCH_API_KEY"

type options struct {
	baseURL     string
	path        string
	model       string
	scenario    string
	prompt      string
	requests    int
	concurrency int
	maxTokens   int
	timeout     time.Duration
}

type tokenUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

type requestSample struct {
	latency   time.Duration
	ttft      time.Duration
	hasTTFT   bool
	usage     tokenUsage
	hasUsage  bool
	success   bool
	errorKind string
}

type quantiles struct {
	Samples int     `json:"samples"`
	P50MS   float64 `json:"p50_ms"`
	P95MS   float64 `json:"p95_ms"`
	P99MS   float64 `json:"p99_ms"`
	MaxMS   float64 `json:"max_ms"`
}

type tokenReport struct {
	UsageSamples          int     `json:"usage_samples"`
	InputTokens           int64   `json:"input_tokens"`
	OutputTokens          int64   `json:"output_tokens"`
	TotalTokens           int64   `json:"total_tokens"`
	InputTokensPerSecond  float64 `json:"input_tokens_per_second"`
	OutputTokensPerSecond float64 `json:"output_tokens_per_second"`
	TotalTokensPerSecond  float64 `json:"total_tokens_per_second"`
}

type benchmarkReport struct {
	Scenario      string         `json:"scenario"`
	Requests      int            `json:"requests"`
	Successes     int            `json:"successes"`
	Failures      int            `json:"failures"`
	SuccessRate   float64        `json:"success_rate"`
	WallTimeMS    float64        `json:"wall_time_ms"`
	CompletedRPS  float64        `json:"completed_rps"`
	SuccessfulRPS float64        `json:"successful_rps"`
	Latency       quantiles      `json:"latency_ms"`
	TTFT          quantiles      `json:"ttft_ms"`
	Tokens        tokenReport    `json:"tokens"`
	Errors        map[string]int `json:"errors,omitempty"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content          json.RawMessage `json:"content"`
			Reasoning        json.RawMessage `json:"reasoning"`
			ReasoningContent json.RawMessage `json:"reasoning_content"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *tokenUsage     `json:"usage"`
	Error json.RawMessage `json:"error"`
}

func main() {
	opts := parseFlags()
	if err := validateOptions(opts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	apiKey := strings.TrimSpace(os.Getenv(apiKeyEnvironment))
	if apiKey == "" {
		fmt.Fprintf(os.Stderr, "%s is required; the key is intentionally not accepted as a command-line flag\n", apiKeyEnvironment)
		os.Exit(2)
	}

	report, err := runBenchmark(context.Background(), opts, apiKey)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseFlags() options {
	var opts options
	flag.StringVar(&opts.baseURL, "base-url", "", "Sub2API base URL, for example http://127.0.0.1:18080")
	flag.StringVar(&opts.path, "path", "/v1/chat/completions", "OpenAI-compatible streaming chat path")
	flag.StringVar(&opts.model, "model", "", "model name")
	flag.StringVar(&opts.scenario, "scenario", "unnamed", "non-secret scenario label")
	flag.StringVar(&opts.prompt, "prompt", "Reply with the single word OK.", "fixed benchmark prompt; prefer SUB2API_BENCH_PROMPT for non-public text")
	flag.IntVar(&opts.requests, "requests", 100, "total request count")
	flag.IntVar(&opts.concurrency, "concurrency", 10, "worker concurrency")
	flag.IntVar(&opts.maxTokens, "max-tokens", 16, "maximum output tokens")
	flag.DurationVar(&opts.timeout, "timeout", 120*time.Second, "per-request timeout")
	flag.Parse()
	if prompt := os.Getenv("SUB2API_BENCH_PROMPT"); prompt != "" {
		opts.prompt = prompt
	}
	return opts
}

func validateOptions(opts options) error {
	parsed, err := url.Parse(opts.baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("-base-url must be an absolute URL without query or fragment")
	}
	if !strings.HasPrefix(opts.path, "/") || strings.ContainsAny(opts.path, "?#") {
		return errors.New("-path must start with / and must not contain query or fragment")
	}
	if strings.TrimSpace(opts.model) == "" {
		return errors.New("-model is required")
	}
	if opts.requests <= 0 || opts.requests > 1_000_000 {
		return errors.New("-requests must be between 1 and 1000000")
	}
	if opts.concurrency <= 0 || opts.concurrency > 10_000 || opts.concurrency > opts.requests {
		return errors.New("-concurrency must be between 1 and requests, with a maximum of 10000")
	}
	if opts.maxTokens <= 0 || opts.maxTokens > 100_000 {
		return errors.New("-max-tokens must be between 1 and 100000")
	}
	if opts.timeout <= 0 || opts.timeout > 30*time.Minute {
		return errors.New("-timeout must be between 1ns and 30m")
	}
	return nil
}

func runBenchmark(ctx context.Context, opts options, apiKey string) (benchmarkReport, error) {
	endpoint := strings.TrimRight(opts.baseURL, "/") + opts.path
	payload, err := json.Marshal(map[string]any{
		"model":          opts.model,
		"messages":       []map[string]string{{"role": "user", "content": opts.prompt}},
		"stream":         true,
		"stream_options": map[string]bool{"include_usage": true},
		"max_tokens":     opts.maxTokens,
	})
	if err != nil {
		return benchmarkReport{}, err
	}
	transport := &http.Transport{
		MaxIdleConns:        opts.concurrency * 2,
		MaxIdleConnsPerHost: opts.concurrency,
		MaxConnsPerHost:     opts.concurrency,
		IdleConnTimeout:     30 * time.Second,
	}
	client := &http.Client{Transport: transport}
	defer transport.CloseIdleConnections()

	jobs := make(chan struct{})
	samples := make(chan requestSample, opts.requests)
	var workers sync.WaitGroup
	for index := 0; index < opts.concurrency; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range jobs {
				samples <- executeRequest(ctx, client, endpoint, apiKey, payload, opts.timeout)
			}
		}()
	}

	wallStarted := time.Now()
	go func() {
		defer close(jobs)
		for index := 0; index < opts.requests; index++ {
			jobs <- struct{}{}
		}
	}()
	workers.Wait()
	close(samples)
	wallTime := time.Since(wallStarted)

	collected := make([]requestSample, 0, opts.requests)
	for sample := range samples {
		collected = append(collected, sample)
	}
	return buildReport(opts.scenario, collected, wallTime), nil
}

func executeRequest(parent context.Context, client *http.Client, endpoint, apiKey string, payload []byte, timeout time.Duration) requestSample {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return requestSample{latency: time.Since(started), errorKind: "request_build"}
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	response, err := client.Do(req)
	if err != nil {
		kind := "transport"
		if ctx.Err() != nil {
			kind = "timeout"
		}
		return requestSample{latency: time.Since(started), errorKind: kind}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
		return requestSample{latency: time.Since(started), errorKind: fmt.Sprintf("http_%d", response.StatusCode)}
	}

	sample := requestSample{}
	sawSSEData := false
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		sawSSEData = true
		if bytes.Equal(data, []byte("[DONE]")) || len(data) == 0 {
			continue
		}
		var chunk streamChunk
		if json.Unmarshal(data, &chunk) != nil {
			continue
		}
		if len(bytes.TrimSpace(chunk.Error)) > 0 && !bytes.Equal(bytes.TrimSpace(chunk.Error), []byte("null")) {
			sample.errorKind = "stream_error"
			break
		}
		if chunk.Usage != nil {
			sample.usage = *chunk.Usage
			sample.hasUsage = true
		}
		if !sample.hasTTFT && chunkHasSemanticOutput(chunk) {
			sample.ttft = time.Since(started)
			sample.hasTTFT = true
		}
	}
	sample.latency = time.Since(started)
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			sample.errorKind = "timeout"
		} else {
			sample.errorKind = "stream_read"
		}
	} else if !sawSSEData {
		sample.errorKind = "invalid_stream"
	}
	sample.success = sample.errorKind == ""
	return sample
}

func chunkHasSemanticOutput(chunk streamChunk) bool {
	for _, choice := range chunk.Choices {
		if rawJSONHasValue(choice.Delta.Content) || rawJSONHasValue(choice.Delta.Reasoning) || rawJSONHasValue(choice.Delta.ReasoningContent) {
			return true
		}
	}
	return false
}

func rawJSONHasValue(value json.RawMessage) bool {
	trimmed := bytes.TrimSpace(value)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) && !bytes.Equal(trimmed, []byte(`""`)) && !bytes.Equal(trimmed, []byte("[]")) && !bytes.Equal(trimmed, []byte("{}"))
}

func buildReport(scenario string, samples []requestSample, wallTime time.Duration) benchmarkReport {
	report := benchmarkReport{Scenario: scenario, Requests: len(samples), Errors: make(map[string]int)}
	latencies := make([]time.Duration, 0, len(samples))
	ttfts := make([]time.Duration, 0, len(samples))
	for _, sample := range samples {
		latencies = append(latencies, sample.latency)
		if sample.hasTTFT {
			ttfts = append(ttfts, sample.ttft)
		}
		if sample.success {
			report.Successes++
		} else {
			report.Errors[sample.errorKind]++
		}
		if sample.hasUsage {
			report.Tokens.UsageSamples++
			report.Tokens.InputTokens += sample.usage.PromptTokens
			report.Tokens.OutputTokens += sample.usage.CompletionTokens
			report.Tokens.TotalTokens += sample.usage.TotalTokens
		}
	}
	report.Failures = report.Requests - report.Successes
	if report.Requests > 0 {
		report.SuccessRate = float64(report.Successes) / float64(report.Requests)
	}
	report.WallTimeMS = float64(wallTime) / float64(time.Millisecond)
	seconds := wallTime.Seconds()
	if seconds > 0 {
		report.CompletedRPS = float64(report.Requests) / seconds
		report.SuccessfulRPS = float64(report.Successes) / seconds
		report.Tokens.InputTokensPerSecond = float64(report.Tokens.InputTokens) / seconds
		report.Tokens.OutputTokensPerSecond = float64(report.Tokens.OutputTokens) / seconds
		report.Tokens.TotalTokensPerSecond = float64(report.Tokens.TotalTokens) / seconds
	}
	report.Latency = durationQuantiles(latencies)
	report.TTFT = durationQuantiles(ttfts)
	if len(report.Errors) == 0 {
		report.Errors = nil
	}
	return report
}

func durationQuantiles(values []time.Duration) quantiles {
	if len(values) == 0 {
		return quantiles{}
	}
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	valueAt := func(percentile float64) float64 {
		index := int(float64(len(sorted))*percentile+0.999999999) - 1
		if index < 0 {
			index = 0
		}
		if index >= len(sorted) {
			index = len(sorted) - 1
		}
		return float64(sorted[index]) / float64(time.Millisecond)
	}
	return quantiles{
		Samples: len(sorted),
		P50MS:   valueAt(0.50),
		P95MS:   valueAt(0.95),
		P99MS:   valueAt(0.99),
		MaxMS:   float64(sorted[len(sorted)-1]) / float64(time.Millisecond),
	}
}
