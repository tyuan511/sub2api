//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

type bazaarLinkRoundTripper func(*http.Request) (*http.Response, error)

func (fn bazaarLinkRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestRunBazaarLinkProbeKeepsOnlyCompactVerdict(t *testing.T) {
	originalClient := bazaarLinkHTTPClient
	t.Cleanup(func() { bazaarLinkHTTPClient = originalClient })
	bazaarLinkHTTPClient = &http.Client{Transport: bazaarLinkRoundTripper(func(req *http.Request) (*http.Response, error) {
		var payload bazaarLinkProbeRequest
		require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
		require.Equal(t, "https://relay.example/v1", payload.BaseURL)
		require.Equal(t, "secret-key", payload.APIKey)
		require.True(t, payload.QuickMode)
		require.True(t, payload.IdentityOnly)
		require.False(t, payload.Sync)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(`{
				"runId":"run-1","status":"completed","score":91,
				"identityAssessment":{"status":"confirmed","confidence":0.94,"claimedModel":"gpt-5.6-sol","predictedFamily":"gpt","subModelMatchV3F":{"modelId":"gpt-5.6-sol","score":0.92},"riskFlags":[]},
				"totalInputTokens":100,"totalOutputTokens":50
			}`)),
			Header: make(http.Header),
		}, nil
	})}

	result := runBazaarLinkProbe(context.Background(), &ChannelMonitor{
		Provider: MonitorProviderOpenAI, Endpoint: "https://relay.example",
		APIKey: "secret-key", PrimaryModel: "gpt-5.6-sol",
	})
	require.Equal(t, "completed", result.Status)
	require.Equal(t, 91, *result.Score)
	require.Equal(t, "gpt-5.6-sol", result.PredictedModel)
	stored, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(stored), "secret-key")
}

func TestNormalizeBazaarLinkIdentityStatus(t *testing.T) {
	require.Equal(t, "confirmed", normalizeBazaarLinkIdentityStatus("match"))
	require.Equal(t, "confirmed", normalizeBazaarLinkIdentityStatus("MATCHED"))
	require.Equal(t, "confirmed", normalizeBazaarLinkIdentityStatus("confirmed"))
	require.Equal(t, "mismatch", normalizeBazaarLinkIdentityStatus("no_match"))
	require.Equal(t, "insufficient_data", normalizeBazaarLinkIdentityStatus("insufficient_data"))
	require.Empty(t, normalizeBazaarLinkIdentityStatus("  "))
}

func TestBazaarLinkProbeReportURL(t *testing.T) {
	require.Empty(t, BazaarLinkProbeReportURL("  "))
	require.Equal(t, "https://bazaarlink.ai/probe?runId=e9475a75-9d1d-4624-9efd-0f3787eb84ee", BazaarLinkProbeReportURL(" e9475a75-9d1d-4624-9efd-0f3787eb84ee "))
	require.Equal(t, "https://bazaarlink.ai/probe?runId=a+b", BazaarLinkProbeReportURL("a b"))
}

func TestPollBazaarLinkProbeUsesRunIDAndKeepsVerdict(t *testing.T) {
	originalClient := bazaarLinkHTTPClient
	t.Cleanup(func() { bazaarLinkHTTPClient = originalClient })
	bazaarLinkHTTPClient = &http.Client{Transport: bazaarLinkRoundTripper(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, req.Method)
		require.Equal(t, "/api/probe/run/run-42", req.URL.Path)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(`{
				"runId":"run-42","status":"completed","score":88,
				"identityAssessment":{"status":"mismatch","confidence":0.71,"riskFlags":["family_mismatch"]}
			}`)),
			Header: make(http.Header),
		}, nil
	})}

	result, err := pollBazaarLinkProbe(context.Background(), "run-42")
	require.NoError(t, err)
	require.Equal(t, "run-42", result.RunID)
	require.Equal(t, "completed", result.Status)
	require.Equal(t, "model identity mismatch", result.Error)
	require.Equal(t, []string{"family_mismatch"}, result.RiskFlags)
}

func TestCompactBazaarLinkProbeTreatsRunIDOnlySubmissionAsQueued(t *testing.T) {
	result := compactBazaarLinkProbeResult(bazaarLinkProbeResponse{RunID: "run-queued"}, time.Now())
	require.Equal(t, "run-queued", result.RunID)
	require.Equal(t, bazaarLinkTaskStatusQueued, result.Status)
	require.Empty(t, result.Error)
}

func TestCompactBazaarLinkProbeTreatsVerdictWithoutStatusAsCompleted(t *testing.T) {
	result := compactBazaarLinkProbeResult(bazaarLinkProbeResponse{
		RunID: "run-complete",
		IdentityAssessment: &struct {
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
		}{Status: "confirmed"},
	}, time.Now())
	if result.Status != bazaarLinkTaskStatusCompleted {
		t.Fatalf("status = %q, want completed", result.Status)
	}
}

type bazaarLinkTargetReaderStub struct {
	target *BazaarLinkProbeTarget
}

func (r *bazaarLinkTargetReaderStub) GetBazaarLinkProbeTarget(context.Context, int64) (*BazaarLinkProbeTarget, error) {
	return r.target, nil
}

func (r *bazaarLinkTargetReaderStub) ListEnabledBazaarLinkProbeTargets(context.Context) ([]*BazaarLinkProbeTarget, error) {
	return []*BazaarLinkProbeTarget{r.target}, nil
}

func TestBazaarLinkProbeRejectsQuotaPlaceholder(t *testing.T) {
	svc := NewBazaarLinkProbeService(
		&bazaarLinkTargetReaderStub{target: &BazaarLinkProbeTarget{
			ID: 1, Provider: MonitorProviderOpenAI, Endpoint: "https://relay.example",
			APIKey: "secret-key", Model: MonitorDefaultQuotaModel,
		}},
		&bazaarLinkTaskRepoStub{},
		nil,
	)
	_, err := svc.Run(context.Background(), 1)
	require.ErrorIs(t, err, ErrChannelMonitorBazaarLinkUnsupported)
}

type bazaarLinkRuntimeStub struct {
	rt BazaarLinkProbeRuntime
}

func (s bazaarLinkRuntimeStub) GetBazaarLinkProbeRuntime(context.Context) BazaarLinkProbeRuntime {
	return s.rt
}

func TestBazaarLinkProbeRunRespectsDisabledSetting(t *testing.T) {
	svc := NewBazaarLinkProbeService(
		&bazaarLinkTargetReaderStub{target: &BazaarLinkProbeTarget{
			ID: 1, Provider: MonitorProviderOpenAI, Endpoint: "https://relay.example",
			APIKey: "secret-key", Model: "gpt-4.1",
		}},
		&bazaarLinkTaskRepoStub{},
		bazaarLinkRuntimeStub{rt: BazaarLinkProbeRuntime{Enabled: false, Cron: DefaultBazaarLinkProbeCron}},
	)
	_, err := svc.Run(context.Background(), 1)
	require.ErrorIs(t, err, ErrChannelMonitorBazaarLinkDisabled)
}

type bazaarLinkMultiTargetReaderStub struct {
	targets []*BazaarLinkProbeTarget
}

func (r *bazaarLinkMultiTargetReaderStub) GetBazaarLinkProbeTarget(_ context.Context, id int64) (*BazaarLinkProbeTarget, error) {
	for _, t := range r.targets {
		if t != nil && t.ID == id {
			return t, nil
		}
	}
	return nil, ErrChannelMonitorNotFound
}

func (r *bazaarLinkMultiTargetReaderStub) ListEnabledBazaarLinkProbeTargets(context.Context) ([]*BazaarLinkProbeTarget, error) {
	return r.targets, nil
}

func TestBazaarLinkProbeListGroupsAndBatchSelection(t *testing.T) {
	targets := []*BazaarLinkProbeTarget{
		{ID: 1, GroupName: "alpha", Provider: MonitorProviderOpenAI, Endpoint: "https://a.example", APIKey: "k1", Model: "gpt-4.1"},
		{ID: 2, GroupName: "alpha", Provider: MonitorProviderOpenAI, Endpoint: "https://a.example", APIKey: "k2", Model: MonitorDefaultQuotaModel},
		{ID: 3, GroupName: "beta", Provider: MonitorProviderOpenAI, Endpoint: "https://b.example", APIKey: "k3", Model: "gpt-4o"},
		{ID: 4, GroupName: "", Provider: MonitorProviderOpenAI, Endpoint: "https://c.example", APIKey: "k4", Model: "gpt-4o"},
		{ID: 5, GroupName: "gamma", Provider: "gemini", Endpoint: "https://g.example", APIKey: "k5", Model: "gemini-pro"},
	}
	svc := NewBazaarLinkProbeService(&bazaarLinkMultiTargetReaderStub{targets: targets}, &bazaarLinkTaskRepoStub{}, nil)

	groups, err := svc.ListProbeGroups(context.Background())
	require.NoError(t, err)
	require.Len(t, groups, 3)
	require.Equal(t, "alpha", groups[0].GroupName)
	require.Equal(t, 2, groups[0].MonitorCount)
	require.Equal(t, 1, groups[0].EligibleCount)
	require.Equal(t, "beta", groups[1].GroupName)
	require.Equal(t, 1, groups[1].EligibleCount)
	require.Equal(t, "gamma", groups[2].GroupName)
	require.Equal(t, 0, groups[2].EligibleCount)

	_, err = svc.RunByGroupNames(context.Background(), nil)
	require.ErrorIs(t, err, ErrChannelMonitorBazaarLinkEmptyGroups)

	// Ineligible monitors (quota placeholder / unsupported provider) are skipped.
	// The remaining eligible target is attempted (submitted or failed depending on
	// remote admission); we only assert selection/eligibility accounting here.
	batch, err := svc.RunByGroupNames(context.Background(), []string{"alpha", "gamma", " missing "})
	require.NoError(t, err)
	require.Equal(t, 3, batch.Total) // alpha×2 + gamma×1
	require.Equal(t, 2, batch.Skipped)
	require.Equal(t, 1, batch.Submitted+batch.Failed)
	require.Len(t, batch.Items, 3)
}

func TestBazaarLinkProbeBatchRespectsDisabledSetting(t *testing.T) {
	svc := NewBazaarLinkProbeService(
		&bazaarLinkMultiTargetReaderStub{targets: []*BazaarLinkProbeTarget{
			{ID: 1, GroupName: "alpha", Provider: MonitorProviderOpenAI, Endpoint: "https://a.example", APIKey: "k", Model: "gpt-4.1"},
		}},
		&bazaarLinkTaskRepoStub{},
		bazaarLinkRuntimeStub{rt: BazaarLinkProbeRuntime{Enabled: false, Cron: DefaultBazaarLinkProbeCron}},
	)
	_, err := svc.RunByGroupNames(context.Background(), []string{"alpha"})
	require.ErrorIs(t, err, ErrChannelMonitorBazaarLinkDisabled)
}

func TestNormalizeAndNextBazaarLinkProbeCron(t *testing.T) {
	require.Equal(t, DefaultBazaarLinkProbeCron, normalizeBazaarLinkProbeCron(""))
	require.Equal(t, DefaultBazaarLinkProbeCron, normalizeBazaarLinkProbeCron("not a cron"))
	require.Equal(t, "15 3 * * 1", normalizeBazaarLinkProbeCron(" 15 3 * * 1 "))
	require.NoError(t, validateBazaarLinkProbeCron(DefaultBazaarLinkProbeCron))
	require.Error(t, validateBazaarLinkProbeCron("99 99 * * *"))

	now := time.Date(2026, 3, 10, 1, 0, 0, 0, time.UTC)
	next, err := nextBazaarLinkProbeAt(now, "0 2 * * *")
	require.NoError(t, err)
	require.True(t, next.After(now))
}

type bazaarLinkTaskUpdate struct {
	id         int64
	status     string
	result     *domain.BazaarLinkProbeResult
	nextPollAt *time.Time
}

type bazaarLinkTaskRepoStub struct {
	ChannelMonitorRepository
	tasks   []*domain.BazaarLinkProbeTask
	updates []bazaarLinkTaskUpdate
}

func (r *bazaarLinkTaskRepoStub) InsertBazaarLinkProbe(context.Context, int64, *domain.BazaarLinkProbeResult) error {
	return nil
}

func (r *bazaarLinkTaskRepoStub) ListLatestBazaarLinkProbes(context.Context, []int64) (map[int64]*domain.BazaarLinkProbeResult, error) {
	return map[int64]*domain.BazaarLinkProbeResult{}, nil
}

func (r *bazaarLinkTaskRepoStub) FindActiveBazaarLinkProbeTask(context.Context, int64, time.Time) (*domain.BazaarLinkProbeTask, error) {
	return nil, nil
}

func (r *bazaarLinkTaskRepoStub) InsertBazaarLinkProbeTask(_ context.Context, task *domain.BazaarLinkProbeTask) error {
	r.tasks = append(r.tasks, task)
	return nil
}

func (r *bazaarLinkTaskRepoStub) ListActiveBazaarLinkProbeTasks(context.Context) ([]*domain.BazaarLinkProbeTask, error) {
	return r.tasks, nil
}

func (r *bazaarLinkTaskRepoStub) UpdateBazaarLinkProbeTask(_ context.Context, id int64, status string, result *domain.BazaarLinkProbeResult, nextPollAt *time.Time) error {
	r.updates = append(r.updates, bazaarLinkTaskUpdate{id: id, status: status, result: result, nextPollAt: nextPollAt})
	return nil
}

func TestPollBazaarLinkProbesClosesExpiredTasks(t *testing.T) {
	repo := &bazaarLinkTaskRepoStub{tasks: []*domain.BazaarLinkProbeTask{{
		ID:        9,
		RunID:     "run-expired",
		Status:    bazaarLinkTaskStatusRunning,
		Result:    &domain.BazaarLinkProbeResult{RunID: "run-expired", Status: bazaarLinkTaskStatusRunning},
		ExpiresAt: time.Now().Add(-time.Minute),
	}}}
	svc := NewBazaarLinkProbeService(nil, repo, nil)

	require.NoError(t, svc.Poll(context.Background()))
	require.Len(t, repo.updates, 1)
	require.Equal(t, int64(9), repo.updates[0].id)
	require.Equal(t, bazaarLinkTaskStatusTimedOut, repo.updates[0].status)
	require.Equal(t, bazaarLinkTaskStatusTimedOut, repo.updates[0].result.Status)
	require.Contains(t, repo.updates[0].result.Error, "2 hours")
	require.Nil(t, repo.updates[0].nextPollAt)
}
