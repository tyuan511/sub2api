package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

const bazaarLinkProbeURL = "https://bazaarlink.ai/api/probe/run"

const (
	bazaarLinkTaskStatusQueued    = "queued"
	bazaarLinkTaskStatusRunning   = "running"
	bazaarLinkTaskStatusCompleted = "completed"
	bazaarLinkTaskStatusFailed    = "failed"
	bazaarLinkTaskStatusTimedOut  = "timed_out"
)

var bazaarLinkHTTPClient = func() *http.Client {
	client := newSSRFSafeHTTPClient(monitorBazaarLinkRunTimeout)
	// The API key is in the request body. Do not forward it to a redirected
	// host; treat redirects as an unsuccessful probe instead.
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client
}()

func bazaarLinkProviderSupported(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case MonitorProviderOpenAI, MonitorProviderAnthropic, MonitorProviderGrok,
		MonitorProviderKimi, MonitorProviderZhipu, MonitorProviderDeepseek:
		return true
	default:
		return false
	}
}

// BazaarLink documents roughly five runs per minute per source IP. All
// monitors in one gateway share that source IP, so serialize admission with a
// conservative 12-second spacing instead of letting independent schedules
// create bursts and turn valid groups into rate-limit errors.
var bazaarLinkProbePacer struct {
	sync.Mutex
	next time.Time
}

func waitForBazaarLinkProbe(ctx context.Context) error {
	bazaarLinkProbePacer.Lock()
	now := time.Now()
	start := now
	if bazaarLinkProbePacer.next.After(start) {
		start = bazaarLinkProbePacer.next
	}
	bazaarLinkProbePacer.next = start.Add(12 * time.Second)
	bazaarLinkProbePacer.Unlock()
	if delay := time.Until(start); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

type bazaarLinkProbeRequest struct {
	BaseURL        string `json:"baseUrl"`
	APIKey         string `json:"apiKey"`
	ModelID        string `json:"modelId"`
	ClaimedModel   string `json:"claimedModel"`
	UpstreamFormat string `json:"upstreamFormat"`
	QuickMode      bool   `json:"quickMode"`
	IdentityOnly   bool   `json:"identityOnly"`
	Sync           bool   `json:"sync"`
	Lang           string `json:"lang"`
}

type bazaarLinkProbeResponse struct {
	RunID              string `json:"runId"`
	Status             string `json:"status"`
	Score              *int   `json:"score"`
	IdentityAssessment *struct {
		Status              string   `json:"status"`
		Confidence          *float64 `json:"confidence"`
		ClaimedModel        string   `json:"claimedModel"`
		PredictedFamily     string   `json:"predictedFamily"`
		PredictedCandidates []struct {
			Model string   `json:"modelId"`
			Score *float64 `json:"score"`
		} `json:"predictedCandidates"`
		SubModelMatchV3F *struct {
			ModelID string   `json:"modelId"`
			Score   *float64 `json:"score"`
		} `json:"subModelMatchV3F"`
		RiskFlags []string `json:"riskFlags"`
	} `json:"identityAssessment"`
	TotalInputTokens  *int `json:"totalInputTokens"`
	TotalOutputTokens *int `json:"totalOutputTokens"`
}

func bazaarLinkTaskStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case bazaarLinkTaskStatusQueued, "pending", "accepted":
		return bazaarLinkTaskStatusQueued
	case bazaarLinkTaskStatusRunning, "processing", "in_progress":
		return bazaarLinkTaskStatusRunning
	case bazaarLinkTaskStatusCompleted, "succeeded", "success", "done":
		return bazaarLinkTaskStatusCompleted
	case bazaarLinkTaskStatusTimedOut, "timeout", "timedout":
		return bazaarLinkTaskStatusTimedOut
	case bazaarLinkTaskStatusFailed, "error", "errored", "cancelled", "canceled":
		return bazaarLinkTaskStatusFailed
	default:
		// Unknown remote states are not safe to classify as terminal failure:
		// BazaarLink can add an intermediate state without invalidating a run.
		return bazaarLinkTaskStatusRunning
	}
}

func decodeBazaarLinkProbeResponse(body []byte) (bazaarLinkProbeResponse, error) {
	var decoded bazaarLinkProbeResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return decoded, err
	}
	if strings.TrimSpace(decoded.Status) != "" || strings.TrimSpace(decoded.RunID) != "" || decoded.IdentityAssessment != nil {
		return decoded, nil
	}
	var wrapper struct {
		Data   *bazaarLinkProbeResponse `json:"data"`
		Result *bazaarLinkProbeResponse `json:"result"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return decoded, err
	}
	if wrapper.Data != nil {
		return *wrapper.Data, nil
	}
	if wrapper.Result != nil {
		return *wrapper.Result, nil
	}
	return decoded, nil
}

func cloneBazaarLinkProbeResult(in *domain.BazaarLinkProbeResult) *domain.BazaarLinkProbeResult {
	if in == nil {
		return nil
	}
	out := *in
	out.RiskFlags = append([]string(nil), in.RiskFlags...)
	return &out
}

// compactBazaarLinkProbeResult copies only the small verdict fields used by
// the monitoring card. Probe items and response text are intentionally
// discarded so they cannot become a second credential-bearing history store.
func compactBazaarLinkProbeResult(decoded bazaarLinkProbeResponse, checkedAt time.Time) *domain.BazaarLinkProbeResult {
	status := strings.TrimSpace(decoded.Status)
	result := &domain.BazaarLinkProbeResult{
		RunID:             decoded.RunID,
		Status:            bazaarLinkTaskStatus(status),
		Score:             decoded.Score,
		TotalInputTokens:  decoded.TotalInputTokens,
		TotalOutputTokens: decoded.TotalOutputTokens,
		CheckedAt:         checkedAt,
	}
	if assessment := decoded.IdentityAssessment; assessment != nil {
		result.IdentityStatus = assessment.Status
		result.Confidence = assessment.Confidence
		result.ClaimedModel = assessment.ClaimedModel
		result.PredictedFamily = assessment.PredictedFamily
		result.RiskFlags = append([]string(nil), assessment.RiskFlags...)
		if assessment.SubModelMatchV3F != nil {
			result.PredictedModel = assessment.SubModelMatchV3F.ModelID
			result.PredictedModelScore = assessment.SubModelMatchV3F.Score
		} else if len(assessment.PredictedCandidates) > 0 {
			result.PredictedModel = assessment.PredictedCandidates[0].Model
			result.PredictedModelScore = assessment.PredictedCandidates[0].Score
		}
	}
	if status == "" && result.IdentityStatus != "" {
		// Some responses omit the lifecycle field when they already contain the
		// identity verdict. That is a completed probe, regardless of runId.
		result.Status = bazaarLinkTaskStatusCompleted
	} else if status == "" && strings.TrimSpace(decoded.RunID) != "" {
		// The submit endpoint may return only a run ID while the task is being
		// queued. Keep it pollable until the first status response arrives.
		result.Status = bazaarLinkTaskStatusQueued
	}
	switch result.Status {
	case bazaarLinkTaskStatusCompleted:
		switch {
		case result.IdentityStatus == "mismatch":
			result.Error = "model identity mismatch"
		case result.IdentityStatus == "insufficient_data":
			result.Error = "identity probe returned insufficient data"
		case len(result.RiskFlags) > 0:
			result.Error = "identity probe reported risk flags"
		case result.IdentityStatus != "confirmed":
			result.Error = "identity probe returned no confirmation"
		}
	case bazaarLinkTaskStatusFailed:
		result.Error = "identity probe failed"
	}
	return result
}

// runBazaarLinkProbe submits one redacted asynchronous identity probe for a
// monitor's primary model. Only the compact submission result is returned;
// the upstream credential and raw probe response never leave this function.
func runBazaarLinkProbeTarget(ctx context.Context, target *BazaarLinkProbeTarget) *domain.BazaarLinkProbeResult {
	checkedAt := time.Now()
	result := &domain.BazaarLinkProbeResult{Status: bazaarLinkTaskStatusFailed, CheckedAt: checkedAt}
	if target == nil {
		result.Error = "identity probe monitor is missing"
		return result
	}
	endpoint := strings.TrimRight(strings.TrimSpace(target.Endpoint), "/")
	if endpoint == "" || strings.TrimSpace(target.APIKey) == "" {
		result.Error = "identity probe requires endpoint and api key"
		return result
	}
	model := strings.TrimSpace(target.Model)
	if model == "" {
		result.Error = "identity probe requires a primary model"
		return result
	}
	if err := waitForBazaarLinkProbe(ctx); err != nil {
		result.Error = "identity probe was cancelled"
		return result
	}
	format := "openai"
	if strings.EqualFold(strings.TrimSpace(target.Provider), MonitorProviderAnthropic) {
		format = "anthropic"
	}
	payload, err := json.Marshal(bazaarLinkProbeRequest{
		BaseURL:        endpoint + "/v1",
		APIKey:         target.APIKey,
		ModelID:        model,
		ClaimedModel:   model,
		UpstreamFormat: format,
		QuickMode:      true,
		IdentityOnly:   true,
		Sync:           false,
		Lang:           "zh",
	})
	if err != nil {
		result.Error = "identity probe request encoding failed"
		return result
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, bazaarLinkProbeURL, bytes.NewReader(payload))
	if err != nil {
		result.Error = "identity probe request creation failed"
		return result
	}
	req.Header.Set("Content-Type", "application/json")
	started := time.Now()
	resp, err := bazaarLinkHTTPClient.Do(req)
	latency := int(time.Since(started) / time.Millisecond)
	result.LatencyMs = &latency
	if err != nil {
		// Keep transport details out of the persisted verdict. In particular,
		// Go URL errors can contain BazaarLink's resolved IP and port.
		result.Error = "identity probe request failed"
		return result
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if readErr != nil {
		result.Error = "identity probe response read failed"
		return result
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Error = fmt.Sprintf("identity probe returned HTTP %d", resp.StatusCode)
		return result
	}
	decoded, err := decodeBazaarLinkProbeResponse(body)
	if err != nil {
		result.Error = "identity probe response was invalid"
		return result
	}
	compact := compactBazaarLinkProbeResult(decoded, checkedAt)
	compact.LatencyMs = result.LatencyMs
	if compact.ClaimedModel == "" {
		compact.ClaimedModel = model
	}
	if compact.RunID == "" && compact.Status != bazaarLinkTaskStatusCompleted {
		compact.Status = bazaarLinkTaskStatusFailed
		compact.Error = "identity probe response did not include a run id"
	}
	return compact
}

// runBazaarLinkProbe is retained as a small unit-test adapter. Production
// callers use runBazaarLinkProbeTarget through BazaarLinkProbeService.
func runBazaarLinkProbe(ctx context.Context, monitor *ChannelMonitor) *domain.BazaarLinkProbeResult {
	if monitor == nil {
		return runBazaarLinkProbeTarget(ctx, nil)
	}
	return runBazaarLinkProbeTarget(ctx, &BazaarLinkProbeTarget{
		ID: monitor.ID, Provider: monitor.Provider, Endpoint: monitor.Endpoint,
		APIKey: monitor.APIKey, APIKeyDecryptFailed: monitor.APIKeyDecryptFailed,
		Model: monitor.PrimaryModel,
	})
}

// pollBazaarLinkProbe fetches one asynchronous run by ID. BazaarLink's
// documented API exposes one-run polling, so callers batch several of these
// requests in the five-minute scheduler tick.
func pollBazaarLinkProbe(ctx context.Context, runID string) (*domain.BazaarLinkProbeResult, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("empty bazaarlink run id")
	}
	started := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, bazaarLinkProbeURL+"/"+url.PathEscape(runID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := bazaarLinkHTTPClient.Do(req)
	latency := int(time.Since(started) / time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("poll bazaarlink probe: %w", err)
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if readErr != nil {
		return nil, fmt.Errorf("read bazaarlink probe result: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("bazaarlink probe result returned HTTP %d", resp.StatusCode)
	}
	decoded, err := decodeBazaarLinkProbeResponse(body)
	if err != nil {
		return nil, fmt.Errorf("decode bazaarlink probe result: %w", err)
	}
	result := compactBazaarLinkProbeResult(decoded, time.Now())
	result.LatencyMs = &latency
	if result.RunID == "" {
		result.RunID = runID
	}
	return result, nil
}
