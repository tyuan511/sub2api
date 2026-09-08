package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type routingArtifactManagerRepo struct {
	artifacts   map[string]*RoutingArtifactVersion
	experiments map[string]*RoutingExperiment
	metrics     map[string]RoutingCanaryMetrics
	replays     []RoutingReplayDecision
	promotes    int
}

func (r *routingArtifactManagerRepo) CreateRoutingAttempts(context.Context, []*RoutingAttemptFact) error {
	return nil
}

func (r *routingArtifactManagerRepo) CreateArtifact(_ context.Context, artifact *RoutingArtifactVersion) error {
	r.artifacts[artifact.Version] = cloneRoutingArtifactForManagerTest(artifact)
	return nil
}

func (r *routingArtifactManagerRepo) GetArtifact(_ context.Context, kind, version string) (*RoutingArtifactVersion, error) {
	artifact := r.artifacts[version]
	if artifact == nil || artifact.ArtifactKind != kind {
		return nil, ErrRoutingArtifactNotFound
	}
	return cloneRoutingArtifactForManagerTest(artifact), nil
}

func (r *routingArtifactManagerRepo) ListArtifacts(context.Context, string, string, int) ([]*RoutingArtifactVersion, error) {
	return nil, nil
}

func (r *routingArtifactManagerRepo) TransitionArtifact(_ context.Context, id int64, from, to string, at time.Time) error {
	for _, artifact := range r.artifacts {
		if artifact.ID == id && artifact.Status == from {
			artifact.Status = to
			if to == RoutingLifecycleRetired {
				artifact.RetiredAt = &at
			}
			return nil
		}
	}
	return errors.New("unexpected direct transition")
}

func (r *routingArtifactManagerRepo) PromoteArtifact(_ context.Context, id int64, fromStatus, toStatus string, expectedActiveVersion *string, at time.Time) error {
	var target *RoutingArtifactVersion
	for _, artifact := range r.artifacts {
		if artifact.ID == id {
			target = artifact
			break
		}
	}
	if target == nil || target.Status != fromStatus {
		return ErrRoutingLifecycleConflict
	}
	active := ""
	for _, artifact := range r.artifacts {
		if routingArtifactScopeEqual(RoutingArtifactScopeFromVersion(target), RoutingArtifactScopeFromVersion(artifact)) && artifact.Status == RoutingLifecycleActive {
			active = artifact.Version
		}
	}
	if expectedActiveVersion != nil && active != *expectedActiveVersion {
		return ErrRoutingLifecycleConflict
	}
	if toStatus == RoutingLifecycleActive {
		for _, artifact := range r.artifacts {
			if artifact.ID != target.ID && routingArtifactScopeEqual(RoutingArtifactScopeFromVersion(target), RoutingArtifactScopeFromVersion(artifact)) && artifact.Status == RoutingLifecycleActive {
				artifact.Status = RoutingLifecyclePaused
			}
		}
		target.ActivatedAt = &at
	}
	target.Status = toStatus
	r.promotes++
	return nil
}

func (r *routingArtifactManagerRepo) CreateExperiment(_ context.Context, experiment *RoutingExperiment) error {
	if r.experiments == nil {
		r.experiments = make(map[string]*RoutingExperiment)
	}
	copy := *experiment
	if copy.ID == 0 {
		copy.ID = int64(len(r.experiments) + 1)
	}
	r.experiments[copy.ExperimentKey] = &copy
	experiment.ID = copy.ID
	return nil
}

func (r *routingArtifactManagerRepo) GetExperiment(_ context.Context, key string) (*RoutingExperiment, error) {
	if r.experiments[key] == nil {
		return nil, ErrRoutingArtifactNotFound
	}
	copy := *r.experiments[key]
	return &copy, nil
}

func (r *routingArtifactManagerRepo) ListExperiments(_ context.Context, status string, _ int) ([]*RoutingExperiment, error) {
	result := make([]*RoutingExperiment, 0, len(r.experiments))
	for _, experiment := range r.experiments {
		if status != "" && experiment.Status != status {
			continue
		}
		copy := *experiment
		result = append(result, &copy)
	}
	return result, nil
}

func (r *routingArtifactManagerRepo) TransitionExperiment(_ context.Context, id int64, fromStatus, toStatus string, approvedBy *int64, stopReason *string, at time.Time) error {
	for _, experiment := range r.experiments {
		if experiment.ID != id || experiment.Status != fromStatus {
			continue
		}
		experiment.Status = toStatus
		experiment.ApprovedBy = approvedBy
		if toStatus == RoutingLifecycleCanary {
			experiment.StartedAt = &at
		}
		if toStatus == RoutingLifecyclePaused || toStatus == RoutingLifecycleRetired {
			experiment.StoppedAt = &at
			experiment.StopReason = stopReason
		}
		return nil
	}
	return ErrRoutingLifecycleConflict
}

func (r *routingArtifactManagerRepo) UpdateExperimentEvidence(_ context.Context, id int64, expectedStatus string, offlineReplay, evaluation json.RawMessage, at time.Time) error {
	for _, experiment := range r.experiments {
		if experiment.ID != id || experiment.Status != expectedStatus {
			continue
		}
		if len(offlineReplay) > 0 {
			experiment.OfflineReplay = append(json.RawMessage(nil), offlineReplay...)
		}
		if len(evaluation) > 0 {
			experiment.LastEvaluation = append(json.RawMessage(nil), evaluation...)
		}
		experiment.LastEvaluatedAt = &at
		return nil
	}
	return ErrRoutingLifecycleConflict
}

func (r *routingArtifactManagerRepo) LoadCanaryMetrics(_ context.Context, _ string, strategyVersion string, _ time.Time) (RoutingCanaryMetrics, error) {
	return r.metrics[strategyVersion], nil
}

func (r *routingArtifactManagerRepo) LoadRoutingReplayDecisions(_ context.Context, _ RoutingArtifactScope, _ string, _, _ time.Time, limit int) ([]RoutingReplayDecision, error) {
	if limit > 0 && len(r.replays) > limit {
		return append([]RoutingReplayDecision(nil), r.replays[:limit]...), nil
	}
	return append([]RoutingReplayDecision(nil), r.replays...), nil
}

type routingArtifactManagerCache struct {
	objects      map[string]*RoutingArtifactVersion
	pointers     RoutingArtifactPointers
	pointerReady bool
	failSwap     bool
}

func (c *routingArtifactManagerCache) PublishArtifact(_ context.Context, artifact *RoutingArtifactVersion) error {
	if existing := c.objects[artifact.Version]; existing != nil && existing.Checksum != artifact.Checksum {
		return ErrRoutingArtifactPointerConflict
	}
	c.objects[artifact.Version] = cloneRoutingArtifactForManagerTest(artifact)
	return nil
}

func (c *routingArtifactManagerCache) SwapPointers(_ context.Context, _ RoutingArtifactScope, pointers RoutingArtifactPointers, expectedActive *string) error {
	if err := ValidateRoutingArtifactPointers(pointers); err != nil {
		return err
	}
	if c.failSwap {
		c.failSwap = false
		return errors.New("redis unavailable")
	}
	current := ""
	if c.pointerReady {
		current = c.pointers.ActiveVersion
	}
	if expectedActive != nil && current != *expectedActive {
		return ErrRoutingArtifactPointerConflict
	}
	if c.objects[pointers.BaselineVersion] == nil || c.objects[pointers.ActiveVersion] == nil ||
		(pointers.CanaryVersion != "" && c.objects[pointers.CanaryVersion] == nil) {
		return ErrRoutingArtifactUnavailable
	}
	c.pointers = pointers
	c.pointerReady = true
	return nil
}

func (c *routingArtifactManagerCache) LoadPointers(context.Context, RoutingArtifactScope) (RoutingArtifactPointers, error) {
	if !c.pointerReady {
		return RoutingArtifactPointers{}, ErrRoutingArtifactUnavailable
	}
	return c.pointers, nil
}

func (c *routingArtifactManagerCache) LoadArtifact(_ context.Context, _ RoutingArtifactScope, version string) (*RoutingArtifactVersion, error) {
	if c.objects[version] == nil {
		return nil, ErrRoutingArtifactUnavailable
	}
	return cloneRoutingArtifactForManagerTest(c.objects[version]), nil
}

func TestRoutingArtifactManagerBootstrapsShadowWithoutChangingExecution(t *testing.T) {
	baseline := routingArtifactForManagerTest(1, "baseline-v1", RoutingLifecycleActive)
	shadow := routingArtifactForManagerTest(2, "candidate-v2", RoutingLifecycleDraft)
	repo := &routingArtifactManagerRepo{artifacts: map[string]*RoutingArtifactVersion{baseline.Version: baseline, shadow.Version: shadow}}
	cache := &routingArtifactManagerCache{objects: make(map[string]*RoutingArtifactVersion)}
	manager := NewRoutingArtifactManager(repo, cache)

	pointers, err := manager.Promote(context.Background(), RoutingArtifactPromotion{
		ArtifactKind: RoutingArtifactStrategy, Version: shadow.Version, TargetStatus: RoutingLifecycleShadow, BaselineVersion: baseline.Version,
	})
	require.NoError(t, err)
	require.Equal(t, baseline.Version, pointers.ActiveVersion)
	require.Empty(t, pointers.CanaryVersion)
	require.Equal(t, shadow.Version, pointers.ShadowVersion)
	require.Equal(t, RoutingLifecycleShadow, repo.artifacts[shadow.Version].Status)
	require.True(t, cache.pointerReady)
}

func TestRoutingArtifactManagerCanaryAndActivePreserveBaseline(t *testing.T) {
	baseline := routingArtifactForManagerTest(1, "baseline-v1", RoutingLifecycleActive)
	candidate := routingArtifactForManagerTest(2, "candidate-v2", RoutingLifecycleShadow)
	repo := &routingArtifactManagerRepo{artifacts: map[string]*RoutingArtifactVersion{baseline.Version: baseline, candidate.Version: candidate}}
	repo.experiments = map[string]*RoutingExperiment{"experiment-1": routingExperimentForManagerTest(baseline, candidate)}
	repo.metrics = promotableRoutingMetrics(baseline.Version, candidate.Version)
	cache := &routingArtifactManagerCache{objects: make(map[string]*RoutingArtifactVersion)}
	manager := NewRoutingArtifactManager(repo, cache)

	canary, err := manager.Promote(context.Background(), RoutingArtifactPromotion{
		ArtifactKind: RoutingArtifactStrategy, Version: candidate.Version, TargetStatus: RoutingLifecycleCanary, BaselineVersion: baseline.Version,
		CanaryAllocationBPS: 500, CanaryExperimentID: "experiment-1", CanaryBucketSaltChecksum: strings.Repeat("a", 64),
	})
	require.NoError(t, err)
	require.Equal(t, baseline.Version, canary.ActiveVersion)
	require.Equal(t, candidate.Version, canary.CanaryVersion)

	approver := int64(99)
	active, err := manager.Promote(context.Background(), RoutingArtifactPromotion{
		ArtifactKind: RoutingArtifactStrategy, Version: candidate.Version, TargetStatus: RoutingLifecycleActive,
		ApprovedBy: &approver,
	})
	require.NoError(t, err)
	require.Equal(t, baseline.Version, active.BaselineVersion)
	require.Equal(t, candidate.Version, active.ActiveVersion)
	require.Empty(t, active.CanaryVersion)
	require.Equal(t, RoutingLifecyclePaused, repo.artifacts[baseline.Version].Status)
	require.Equal(t, RoutingLifecycleActive, repo.experiments["experiment-1"].Status)
}

func TestRoutingArtifactManagerRetriesPointerPublicationAfterDatabasePromotion(t *testing.T) {
	baseline := routingArtifactForManagerTest(1, "baseline-v1", RoutingLifecycleActive)
	candidate := routingArtifactForManagerTest(2, "candidate-v2", RoutingLifecycleCanary)
	repo := &routingArtifactManagerRepo{artifacts: map[string]*RoutingArtifactVersion{baseline.Version: baseline, candidate.Version: candidate}}
	repo.experiments = map[string]*RoutingExperiment{"experiment-1": routingExperimentForManagerTest(baseline, candidate)}
	repo.experiments["experiment-1"].Status = RoutingLifecycleCanary
	repo.metrics = promotableRoutingMetrics(baseline.Version, candidate.Version)
	cache := &routingArtifactManagerCache{
		objects: map[string]*RoutingArtifactVersion{baseline.Version: cloneRoutingArtifactForManagerTest(baseline)},
		pointers: RoutingArtifactPointers{
			BaselineVersion: baseline.Version, ActiveVersion: baseline.Version, CanaryVersion: candidate.Version,
			CanaryAllocationBPS: 500, CanaryExperimentID: "experiment-1", CanaryBucketSaltChecksum: strings.Repeat("a", 64), UpdatedAt: time.Now(),
		},
		pointerReady: true, failSwap: true,
	}
	cache.objects[candidate.Version] = cloneRoutingArtifactForManagerTest(candidate)
	manager := NewRoutingArtifactManager(repo, cache)

	approver := int64(99)
	_, err := manager.Promote(context.Background(), RoutingArtifactPromotion{
		ArtifactKind: RoutingArtifactStrategy, Version: candidate.Version, TargetStatus: RoutingLifecycleActive,
		ApprovedBy: &approver,
	})
	require.Error(t, err)
	require.Equal(t, RoutingLifecycleActive, repo.artifacts[candidate.Version].Status)
	require.Equal(t, baseline.Version, cache.pointers.ActiveVersion, "gateways remain on the old pointer")
	require.Equal(t, 1, repo.promotes)

	pointers, err := manager.Promote(context.Background(), RoutingArtifactPromotion{
		ArtifactKind: RoutingArtifactStrategy, Version: candidate.Version, TargetStatus: RoutingLifecycleActive,
		ApprovedBy: &approver,
	})
	require.NoError(t, err)
	require.Equal(t, candidate.Version, pointers.ActiveVersion)
	require.Equal(t, 1, repo.promotes, "retry only reconciles Redis and must not transition twice")
}

func TestRoutingArtifactManagerRollbackReturnsToImmutableBaseline(t *testing.T) {
	baseline := routingArtifactForManagerTest(1, "baseline-v1", RoutingLifecyclePaused)
	active := routingArtifactForManagerTest(2, "active-v2", RoutingLifecycleActive)
	repo := &routingArtifactManagerRepo{artifacts: map[string]*RoutingArtifactVersion{baseline.Version: baseline, active.Version: active}}
	repo.experiments = map[string]*RoutingExperiment{"experiment-1": routingExperimentForManagerTest(baseline, active)}
	repo.experiments["experiment-1"].Status = RoutingLifecycleActive
	cache := &routingArtifactManagerCache{
		objects:      map[string]*RoutingArtifactVersion{baseline.Version: cloneRoutingArtifactForManagerTest(baseline), active.Version: cloneRoutingArtifactForManagerTest(active)},
		pointers:     RoutingArtifactPointers{BaselineVersion: baseline.Version, ActiveVersion: active.Version, CanaryVersion: "", UpdatedAt: time.Now()},
		pointerReady: true,
	}
	manager := NewRoutingArtifactManager(repo, cache)

	pointers, err := manager.RollbackToBaseline(context.Background(), RoutingArtifactScopeFromVersion(active))
	require.NoError(t, err)
	require.Equal(t, baseline.Version, pointers.ActiveVersion)
	require.Empty(t, pointers.CanaryVersion)
	require.Equal(t, RoutingLifecycleActive, repo.artifacts[baseline.Version].Status)
	require.Equal(t, RoutingLifecyclePaused, repo.artifacts[active.Version].Status)
	require.Equal(t, RoutingLifecyclePaused, repo.experiments["experiment-1"].Status)
	require.Equal(t, "manual_baseline_rollback", *repo.experiments["experiment-1"].StopReason)
}

func routingArtifactForManagerTest(id int64, version, status string) *RoutingArtifactVersion {
	payload := json.RawMessage(`{"weights":{"success":0.5,"price":0.2,"speed":0.2,"capacity":0.1},"success_rate_hard_gate":0.5,"minimum_samples":10,"max_snapshot_age_seconds":180,"stability":{"minimum_score_difference":0.01,"minimum_residence_seconds":300,"max_traffic_change_bps":1000}}`)
	sum := sha256.Sum256(payload)
	preference := APIKeySmartPreferenceBalanced
	return &RoutingArtifactVersion{
		ID: id, ArtifactKind: RoutingArtifactStrategy, Version: version, Platform: PlatformOpenAI,
		ModelFamily: "gpt-5", EndpointKind: "responses", Preference: &preference, Status: status,
		SchemaVersion: "routing-strategy-v1", Checksum: hex.EncodeToString(sum[:]), Payload: payload,
		Dependencies: json.RawMessage(`[]`), Lineage: json.RawMessage(`{"source":"test"}`), CreatedAt: time.Now(),
	}
}

func cloneRoutingArtifactForManagerTest(artifact *RoutingArtifactVersion) *RoutingArtifactVersion {
	if artifact == nil {
		return nil
	}
	cloned := *artifact
	cloned.Preference = cloneStringPtr(artifact.Preference)
	cloned.Payload = append(json.RawMessage(nil), artifact.Payload...)
	cloned.Dependencies = append(json.RawMessage(nil), artifact.Dependencies...)
	cloned.Lineage = append(json.RawMessage(nil), artifact.Lineage...)
	return &cloned
}

func routingExperimentForManagerTest(baseline, candidate *RoutingArtifactVersion) *RoutingExperiment {
	startedAt := time.Now().Add(-2 * time.Hour)
	return &RoutingExperiment{
		ID: 1, ExperimentKey: "experiment-1", Platform: baseline.Platform, ModelFamily: baseline.ModelFamily,
		EndpointKind: baseline.EndpointKind, Preference: *baseline.Preference,
		BaselineStrategyVersion: baseline.Version, CandidateStrategyVersion: candidate.Version,
		Status: RoutingLifecycleShadow, AllocationBPS: 500, BucketSaltChecksum: strings.Repeat("a", 64),
		Guardrails: json.RawMessage(`{}`), StartedAt: &startedAt, CreatedAt: startedAt,
	}
}

func promotableRoutingMetrics(baselineVersion, candidateVersion string) map[string]RoutingCanaryMetrics {
	baseline := RoutingCanaryMetrics{
		Decisions: 20_000, FinalEvents: 20_000, ObservationDuration: 2 * time.Hour, EventCoverage: 1,
		BillingCoverage: 1, LatencyCoverage: 1, FinalSuccessRate: 0.99,
		P95LatencyMS: 1000, P95TTFTMS: 200, CostPerSuccess: 2,
		SwitchRate: 0.01, CacheColdRate: 0.01, CriticalSlicesHealthy: true,
	}
	baseline.SuccessRateLowerBound, baseline.SuccessRateUpperBound = RoutingWilsonInterval(19_800, 20_000)
	candidate := baseline
	candidate.Decisions, candidate.FinalEvents = 1100, 1100
	candidate.P95LatencyMS, candidate.P95TTFTMS, candidate.CostPerSuccess = 950, 190, 1.9
	candidate.SuccessRateLowerBound, candidate.SuccessRateUpperBound = RoutingWilsonInterval(1089, 1100)
	return map[string]RoutingCanaryMetrics{baselineVersion: baseline, candidateVersion: candidate}
}

func TestRoutingArtifactManagerRejectsActivePromotionWithoutEvidenceOrApprover(t *testing.T) {
	baseline := routingArtifactForManagerTest(1, "baseline-v1", RoutingLifecycleActive)
	candidate := routingArtifactForManagerTest(2, "candidate-v2", RoutingLifecycleCanary)
	experiment := routingExperimentForManagerTest(baseline, candidate)
	experiment.Status = RoutingLifecycleCanary
	repo := &routingArtifactManagerRepo{
		artifacts:   map[string]*RoutingArtifactVersion{baseline.Version: baseline, candidate.Version: candidate},
		experiments: map[string]*RoutingExperiment{"experiment-1": experiment},
		metrics:     promotableRoutingMetrics(baseline.Version, candidate.Version),
	}
	cache := &routingArtifactManagerCache{
		objects: map[string]*RoutingArtifactVersion{baseline.Version: baseline, candidate.Version: candidate},
		pointers: RoutingArtifactPointers{BaselineVersion: baseline.Version, ActiveVersion: baseline.Version,
			CanaryVersion: candidate.Version, CanaryAllocationBPS: 500, CanaryExperimentID: "experiment-1",
			CanaryBucketSaltChecksum: strings.Repeat("a", 64)}, pointerReady: true,
	}
	manager := NewRoutingArtifactManager(repo, cache)
	_, err := manager.Promote(context.Background(), RoutingArtifactPromotion{
		ArtifactKind: RoutingArtifactStrategy, Version: candidate.Version, TargetStatus: RoutingLifecycleActive,
	})
	require.ErrorIs(t, err, ErrRoutingPromotionEvidence)

	approver := int64(99)
	repo.metrics[candidate.Version] = RoutingCanaryMetrics{Decisions: 10, ObservationDuration: 2 * time.Hour}
	_, err = manager.Promote(context.Background(), RoutingArtifactPromotion{
		ArtifactKind: RoutingArtifactStrategy, Version: candidate.Version, TargetStatus: RoutingLifecycleActive, ApprovedBy: &approver,
	})
	require.ErrorIs(t, err, ErrRoutingPromotionEvidence)
}

func TestRoutingArtifactManagerOfflineReplayAdvancesDraftExperimentToShadow(t *testing.T) {
	baseline := routingArtifactForManagerTest(1, "baseline-v1", RoutingLifecycleActive)
	candidate := routingArtifactForManagerTest(2, "candidate-v2", RoutingLifecycleShadow)
	experiment := routingExperimentForManagerTest(baseline, candidate)
	experiment.Status = RoutingLifecycleDraft
	experiment.Guardrails = json.RawMessage(`{"minimum_decisions":10}`)
	now := time.Now().UTC()
	successRate, confidence, normalizedRate := 0.99, 1.0, 1.2
	ttft, duration, capacity := 200.0, 1000.0, 0.9
	replays := make([]RoutingReplayDecision, 10)
	for index := range replays {
		replays[index] = RoutingReplayDecision{
			RoutingDecisionID: fmt.Sprintf("decision-%d", index), RouteVersion: 1, SelectedGroupID: 10,
			FeatureSchemaVersion: "features-v1", SampleProbability: .01,
			OccurredAt: now.Add(-time.Hour), Candidates: []APIKeyRoutingDecisionCandidate{{
				GroupID: 10, ConfiguredPriority: 0, Admitted: true, SuccessRate: &successRate,
				Confidence: &confidence, NormalizedRate: &normalizedRate, TTFTMS: &ttft,
				DurationMS: &duration, CapacityScore: &capacity, ObservationWindow: "1h",
				OutcomeVisibility: RoutingOutcomeObserved,
			}},
		}
	}
	repo := &routingArtifactManagerRepo{
		artifacts:   map[string]*RoutingArtifactVersion{baseline.Version: baseline, candidate.Version: candidate},
		experiments: map[string]*RoutingExperiment{experiment.ExperimentKey: experiment}, replays: replays,
	}
	manager := NewRoutingArtifactManager(repo, &routingArtifactManagerCache{})
	manager.now = func() time.Time { return now }

	report, err := manager.RunOfflineReplay(context.Background(), experiment.ExperimentKey, now.Add(-24*time.Hour), now)
	require.NoError(t, err)
	require.True(t, report.Passed)
	require.False(t, report.CausalClaimAllowed)
	require.EqualValues(t, 10, report.ReplayedDecisions)
	require.Equal(t, RoutingReplayDatasetQueryVersion, report.DatasetManifest.QueryVersion)
	require.Equal(t, []string{"features-v1"}, report.DatasetManifest.FeatureSchemaVersions)
	require.InDelta(t, .01, report.DatasetManifest.MinimumSampleProbability, 1e-12)
	require.Len(t, report.DatasetManifest.Checksum, 64)
	require.Contains(t, report.DatasetManifest.PointInTimeJoin, "decision-time")
	require.Equal(t, RoutingLifecycleShadow, repo.experiments[experiment.ExperimentKey].Status)
	require.NotEmpty(t, repo.experiments[experiment.ExperimentKey].OfflineReplay)
}

func TestRoutingArtifactManagerPauseAndResumeShadowAreAuditable(t *testing.T) {
	baseline := routingArtifactForManagerTest(1, "baseline-v1", RoutingLifecycleActive)
	candidate := routingArtifactForManagerTest(2, "candidate-v2", RoutingLifecycleShadow)
	experiment := routingExperimentForManagerTest(baseline, candidate)
	replay, err := json.Marshal(RoutingOfflineReplayReport{
		ReplayVersion: RoutingOfflineReplayVersion, CandidateStrategyVersion: candidate.Version, Passed: true,
	})
	require.NoError(t, err)
	experiment.OfflineReplay = replay
	repo := &routingArtifactManagerRepo{
		artifacts:   map[string]*RoutingArtifactVersion{baseline.Version: baseline, candidate.Version: candidate},
		experiments: map[string]*RoutingExperiment{experiment.ExperimentKey: experiment},
	}
	cache := &routingArtifactManagerCache{
		objects: map[string]*RoutingArtifactVersion{baseline.Version: baseline, candidate.Version: candidate},
		pointers: RoutingArtifactPointers{BaselineVersion: baseline.Version, ActiveVersion: baseline.Version,
			ShadowVersion: candidate.Version}, pointerReady: true,
	}
	manager := NewRoutingArtifactManager(repo, cache)

	paused, err := manager.PauseExperiment(context.Background(), experiment.ExperimentKey, "operator_investigation")
	require.NoError(t, err)
	require.Equal(t, RoutingLifecyclePaused, paused.Status)
	require.Equal(t, RoutingLifecyclePaused, repo.artifacts[candidate.Version].Status)
	require.Empty(t, cache.pointers.ShadowVersion)
	require.Equal(t, "operator_investigation", *paused.StopReason)

	resumed, err := manager.ResumeExperimentShadow(context.Background(), experiment.ExperimentKey)
	require.NoError(t, err)
	require.Equal(t, RoutingLifecycleShadow, resumed.Status)
	require.Equal(t, RoutingLifecycleShadow, repo.artifacts[candidate.Version].Status)
	require.Equal(t, candidate.Version, cache.pointers.ShadowVersion)
}
