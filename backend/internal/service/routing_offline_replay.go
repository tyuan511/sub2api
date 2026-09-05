package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const RoutingOfflineReplayVersion = "routing-offline-replay-v1"
const RoutingReplayDatasetQueryVersion = "routing-replay-decisions-v3"

type RoutingReplayDecision struct {
	SmartBalanceBPS       *int
	RoutingMinSuccessRate int
	RoutingDecisionID     string
	RouteVersion          int64
	SelectedGroupID       int64
	Candidates            []APIKeyRoutingDecisionCandidate
	FeatureSchemaVersion  string
	SampleProbability     float64
	OccurredAt            time.Time
}

type RoutingDatasetManifest struct {
	Purpose                  string    `json:"purpose"`
	QueryVersion             string    `json:"query_version"`
	Since                    time.Time `json:"since"`
	Until                    time.Time `json:"until"`
	FeatureSchemaVersions    []string  `json:"feature_schema_versions"`
	SamplingRule             string    `json:"sampling_rule"`
	ExclusionRules           []string  `json:"exclusion_rules"`
	PointInTimeJoin          string    `json:"point_in_time_join"`
	RowCount                 int64     `json:"row_count"`
	MinimumSampleProbability float64   `json:"minimum_sample_probability"`
	MaximumSampleProbability float64   `json:"maximum_sample_probability"`
	Checksum                 string    `json:"checksum"`
	CreatedAt                time.Time `json:"created_at"`
}

type RoutingOfflineReplaySource interface {
	LoadRoutingReplayDecisions(ctx context.Context, scope RoutingArtifactScope, strategyVersion string, since, until time.Time, limit int) ([]RoutingReplayDecision, error)
}

type RoutingOfflineReplayReport struct {
	ReplayVersion            string                 `json:"replay_version"`
	CandidateStrategyVersion string                 `json:"candidate_strategy_version"`
	SourceStrategyVersion    string                 `json:"source_strategy_version"`
	Since                    time.Time              `json:"since"`
	Until                    time.Time              `json:"until"`
	MinimumDecisions         int64                  `json:"minimum_decisions"`
	DecisionCount            int64                  `json:"decision_count"`
	ReplayedDecisions        int64                  `json:"replayed_decisions"`
	ChangedTopChoice         int64                  `json:"changed_top_choice"`
	MissingFeatureDecisions  int64                  `json:"missing_feature_decisions"`
	InvalidDecisions         int64                  `json:"invalid_decisions"`
	HardConstraintViolations int64                  `json:"hard_constraint_violations"`
	Passed                   bool                   `json:"passed"`
	CausalClaimAllowed       bool                   `json:"causal_claim_allowed"`
	SelectionBiasNotice      string                 `json:"selection_bias_notice"`
	DatasetManifest          RoutingDatasetManifest `json:"dataset_manifest"`
	EvaluatedAt              time.Time              `json:"evaluated_at"`
}

// RunOfflineReplay performs a point-in-time safety replay. It deliberately
// does not claim counterfactual lift: only the actually chosen route has an
// observed outcome in deterministic history. A passing report proves schema,
// feature availability, bounded candidates, and hard-filter preservation, and
// advances the experiment from draft to shadow.
func (m *RoutingArtifactManager) RunOfflineReplay(ctx context.Context, experimentKey string, since, until time.Time) (RoutingOfflineReplayReport, error) {
	if m == nil || m.repo == nil {
		return RoutingOfflineReplayReport{}, ErrRoutingArtifactUnavailable
	}
	source, ok := m.repo.(RoutingOfflineReplaySource)
	if !ok {
		return RoutingOfflineReplayReport{}, ErrRoutingArtifactUnavailable
	}
	experiment, err := m.repo.GetExperiment(ctx, experimentKey)
	if err != nil {
		return RoutingOfflineReplayReport{}, err
	}
	if experiment.Status != RoutingLifecycleDraft {
		return RoutingOfflineReplayReport{}, ErrRoutingLifecycleConflict
	}
	candidate, err := m.repo.GetArtifact(ctx, RoutingArtifactStrategy, experiment.CandidateStrategyVersion)
	if err != nil {
		return RoutingOfflineReplayReport{}, err
	}
	if candidate.Status != RoutingLifecycleShadow {
		return RoutingOfflineReplayReport{}, fmt.Errorf("%w: candidate must be shadow before replay", ErrRoutingLifecycleConflict)
	}
	policy, err := ParseAPIKeyRoutingStrategyArtifact(candidate)
	if err != nil {
		return RoutingOfflineReplayReport{}, err
	}
	guardrails, err := ParseRoutingCanaryGuardrails(experiment.Guardrails)
	if err != nil {
		return RoutingOfflineReplayReport{}, err
	}
	now := m.now().UTC()
	if until.IsZero() || until.After(now) {
		until = now
	}
	if since.IsZero() {
		since = until.Add(-7 * 24 * time.Hour)
	}
	if !since.Before(until) || until.Sub(since) > 90*24*time.Hour {
		return RoutingOfflineReplayReport{}, fmt.Errorf("%w: replay window must be within retained point-in-time facts", ErrRoutingArtifactInvalid)
	}
	scope := RoutingArtifactScope{
		ArtifactKind: RoutingArtifactStrategy, Platform: experiment.Platform, ModelFamily: experiment.ModelFamily,
		EndpointKind: experiment.EndpointKind, Preference: &experiment.Preference,
	}
	decisions, err := source.LoadRoutingReplayDecisions(ctx, scope, experiment.BaselineStrategyVersion, since, until, 50_000)
	if err != nil {
		return RoutingOfflineReplayReport{}, err
	}
	manifest, err := BuildRoutingReplayDatasetManifest(scope, experiment.BaselineStrategyVersion, decisions, since, until, now)
	if err != nil {
		return RoutingOfflineReplayReport{}, err
	}
	report := RoutingOfflineReplayReport{
		ReplayVersion: RoutingOfflineReplayVersion, CandidateStrategyVersion: candidate.Version,
		SourceStrategyVersion: experiment.BaselineStrategyVersion, Since: since.UTC(), Until: until.UTC(),
		MinimumDecisions: guardrails.MinimumDecisions, DecisionCount: int64(len(decisions)),
		CausalClaimAllowed:  false,
		SelectionBiasNotice: "deterministic historical outcomes are observational; replay validates safety and ordering only, not causal lift",
		DatasetManifest:     manifest,
		EvaluatedAt:         now,
	}
	for _, decision := range decisions {
		if decision.RouteVersion < 1 || decision.SelectedGroupID <= 0 || decision.OccurredAt.Before(since) || !decision.OccurredAt.Before(until) ||
			strings.TrimSpace(decision.FeatureSchemaVersion) == "" || decision.SampleProbability <= 0 || decision.SampleProbability > 1 ||
			len(decision.Candidates) == 0 || len(decision.Candidates) > DefaultMaxAPIKeyGroupRoutes {
			report.InvalidDecisions++
			continue
		}
		decisionPolicy := ApplyAPIKeyRoutingControls(policy, &APIKey{SmartBalanceBPS: decision.SmartBalanceBPS, RoutingMinSuccessRate: decision.RoutingMinSuccessRate})
		ranked, complete := ReplayAPIKeyRoutingCandidates(decision.Candidates, decisionPolicy, experiment.Platform, experiment.ModelFamily, experiment.EndpointKind, decision.OccurredAt)
		if !complete {
			report.MissingFeatureDecisions++
			continue
		}
		if len(ranked) == 0 || !ranked[0].Eligible {
			report.HardConstraintViolations++
			continue
		}
		for _, sourceCandidate := range decision.Candidates {
			if sourceCandidate.GroupID == ranked[0].GroupID && (!sourceCandidate.Admitted ||
				(sourceCandidate.SuccessRate != nil && *sourceCandidate.SuccessRate < decisionPolicy.SuccessRateHardGate)) {
				report.HardConstraintViolations++
			}
		}
		report.ReplayedDecisions++
		if ranked[0].GroupID != decision.SelectedGroupID {
			report.ChangedTopChoice++
		}
	}
	report.Passed = report.DecisionCount >= guardrails.MinimumDecisions && report.ReplayedDecisions >= guardrails.MinimumDecisions &&
		report.InvalidDecisions == 0 && report.HardConstraintViolations == 0 &&
		report.MissingFeatureDecisions*100 <= maxInt64(1, report.DecisionCount)*5
	payload, err := json.Marshal(report)
	if err != nil {
		return RoutingOfflineReplayReport{}, err
	}
	if err := m.repo.UpdateExperimentEvidence(ctx, experiment.ID, experiment.Status, payload, nil, now); err != nil {
		return RoutingOfflineReplayReport{}, err
	}
	if report.Passed {
		if err := m.repo.TransitionExperiment(ctx, experiment.ID, RoutingLifecycleDraft, RoutingLifecycleShadow, nil, nil, now); err != nil {
			return RoutingOfflineReplayReport{}, err
		}
	}
	return report, nil
}

func BuildRoutingReplayDatasetManifest(
	scope RoutingArtifactScope,
	strategyVersion string,
	decisions []RoutingReplayDecision,
	since, until, createdAt time.Time,
) (RoutingDatasetManifest, error) {
	if err := scope.Validate(); err != nil || strings.TrimSpace(strategyVersion) == "" || !since.Before(until) {
		return RoutingDatasetManifest{}, fmt.Errorf("%w: invalid replay dataset scope or window", ErrRoutingArtifactInvalid)
	}
	schemas := make(map[string]struct{})
	minimumProbability, maximumProbability := 1.0, 0.0
	hasValidProbability := false
	for _, decision := range decisions {
		if strings.TrimSpace(decision.FeatureSchemaVersion) != "" {
			schemas[decision.FeatureSchemaVersion] = struct{}{}
		}
		if decision.SampleProbability > 0 && decision.SampleProbability <= 1 {
			hasValidProbability = true
			minimumProbability = math.Min(minimumProbability, decision.SampleProbability)
			maximumProbability = math.Max(maximumProbability, decision.SampleProbability)
		}
	}
	if !hasValidProbability {
		minimumProbability = 0
	}
	featureSchemas := make([]string, 0, len(schemas))
	for schema := range schemas {
		featureSchemas = append(featureSchemas, schema)
	}
	sort.Strings(featureSchemas)
	checksumInput := struct {
		QueryVersion    string                  `json:"query_version"`
		Scope           RoutingArtifactScope    `json:"scope"`
		StrategyVersion string                  `json:"strategy_version"`
		Since           time.Time               `json:"since"`
		Until           time.Time               `json:"until"`
		Decisions       []RoutingReplayDecision `json:"decisions"`
	}{
		QueryVersion: RoutingReplayDatasetQueryVersion, Scope: scope, StrategyVersion: strategyVersion,
		Since: since.UTC(), Until: until.UTC(), Decisions: decisions,
	}
	body, err := json.Marshal(checksumInput)
	if err != nil {
		return RoutingDatasetManifest{}, fmt.Errorf("marshal replay dataset checksum input: %w", err)
	}
	sum := sha256.Sum256(body)
	manifest := RoutingDatasetManifest{
		Purpose: "offline_replay", QueryVersion: RoutingReplayDatasetQueryVersion,
		Since: since.UTC(), Until: until.UTC(), FeatureSchemaVersions: featureSchemas,
		SamplingRule: "stable decision sampling with recorded sample_probability; no causal lift claim",
		ExclusionRules: []string{
			"outcome_category must equal routing_decision",
			"scope, preference, and source strategy_version must match exactly",
			"occurred_at must be inside the half-open manifest window",
			"invalid, missing-feature, and hard-constraint rows remain counted, never silently dropped",
		},
		PointInTimeJoin: "decision-time embedded candidate features only; no post-decision feature join",
		RowCount:        int64(len(decisions)), MinimumSampleProbability: minimumProbability,
		MaximumSampleProbability: maximumProbability, Checksum: hex.EncodeToString(sum[:]), CreatedAt: createdAt.UTC(),
	}
	if err := ValidateRoutingDatasetManifest(manifest); err != nil {
		return RoutingDatasetManifest{}, err
	}
	return manifest, nil
}

func ValidateRoutingDatasetManifest(manifest RoutingDatasetManifest) error {
	checksum, err := hex.DecodeString(manifest.Checksum)
	if manifest.Purpose == "" || manifest.QueryVersion == "" || !manifest.Since.Before(manifest.Until) ||
		manifest.CreatedAt.IsZero() || manifest.RowCount < 0 || manifest.MinimumSampleProbability < 0 ||
		manifest.MaximumSampleProbability > 1 || manifest.MinimumSampleProbability > manifest.MaximumSampleProbability ||
		(manifest.RowCount > 0 && (len(manifest.FeatureSchemaVersions) == 0 || manifest.MinimumSampleProbability <= 0)) ||
		manifest.SamplingRule == "" || len(manifest.ExclusionRules) == 0 || manifest.PointInTimeJoin == "" ||
		err != nil || len(checksum) != sha256.Size {
		return fmt.Errorf("%w: invalid routing dataset manifest", ErrRoutingArtifactInvalid)
	}
	return nil
}

func ReplayAPIKeyRoutingCandidates(recorded []APIKeyRoutingDecisionCandidate, policy APIKeyRoutingStrategyPolicy, platform, modelFamily, endpointKind string, generatedAt time.Time) ([]APIKeyRoutingCandidateScore, bool) {
	candidates := make([]APIKeyRouteCandidate, 0, len(recorded))
	snapshot := &APIKeyRoutingScoreSnapshot{
		Version: "point-in-time-replay", FeatureVersion: "recorded", StrategyVersion: policy.Version,
		Platform: platform, ModelFamily: modelFamily, EndpointKind: endpointKind, GeneratedAt: generatedAt,
		Groups: make(map[int64]APIKeyRoutingGroupObservation, len(recorded)),
	}
	complete := true
	for _, item := range recorded {
		if !item.Admitted {
			continue
		}
		if item.SuccessRate == nil || item.Confidence == nil || item.NormalizedRate == nil ||
			item.CapacityScore == nil || (item.TTFTMS == nil && item.DurationMS == nil) {
			complete = false
			continue
		}
		total := maxInt64(policy.MinimumSamples, 1000)
		successes := int64(math.Round(routeClamp01(*item.SuccessRate) * float64(total)))
		observation := APIKeyRoutingGroupObservation{
			GroupID: item.GroupID, SuccessRequests: successes, FailedRequests: total - successes,
			NormalizedRate: *item.NormalizedRate, Confidence: routeClamp01(*item.Confidence),
			CapacityScore: routeClamp01(*item.CapacityScore), ObservationWindow: item.ObservationWindow,
			ObservationGeneratedAt: generatedAt,
			DependencyDomains:      append([]string(nil), item.DependencyDomains...),
		}
		if item.SmoothedSuccessRate != nil {
			observation.SmoothedSuccessRate = routeClamp01(*item.SmoothedSuccessRate)
		} else {
			observation.SmoothedSuccessRate = routeClamp01(*item.SuccessRate)
		}
		if item.TTFTMS != nil {
			observation.TTFTP50Ms = *item.TTFTMS
		}
		if item.DurationMS != nil {
			observation.DurationP50Ms = *item.DurationMS
		}
		snapshot.Groups[item.GroupID] = observation
		candidates = append(candidates, APIKeyRouteCandidate{GroupID: item.GroupID, Priority: item.ConfiguredPriority})
	}
	if len(candidates) == 0 {
		return nil, false
	}
	return RankAPIKeyRoutingCandidatesWithPolicy(candidates, snapshot, policy), complete
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
