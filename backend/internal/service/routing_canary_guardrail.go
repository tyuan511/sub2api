package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

type RoutingCanaryGuardrails struct {
	MetricPolicyVersion               string  `json:"metric_policy_version"`
	MinimumDecisions                  int64   `json:"minimum_decisions"`
	MinimumObservationSeconds         int64   `json:"minimum_observation_seconds"`
	MinimumEventCoverage              float64 `json:"minimum_event_coverage"`
	MinimumCriticalSliceDecisions     int64   `json:"minimum_critical_slice_decisions"`
	MinimumFinalSuccessRate           float64 `json:"minimum_final_success_rate"`
	MaximumSuccessRateDrop            float64 `json:"maximum_success_rate_drop"`
	MaximumCriticalSliceSuccessDrop   float64 `json:"maximum_critical_slice_success_rate_drop"`
	MaximumP95LatencyIncreaseRatio    float64 `json:"maximum_p95_latency_increase_ratio"`
	MaximumP99LatencyIncreaseRatio    float64 `json:"maximum_p99_latency_increase_ratio"`
	MaximumP95TTFTIncreaseRatio       float64 `json:"maximum_p95_ttft_increase_ratio"`
	MaximumP99TTFTIncreaseRatio       float64 `json:"maximum_p99_ttft_increase_ratio"`
	MaximumCostIncreaseRatio          float64 `json:"maximum_cost_per_success_increase_ratio"`
	MaximumSwitchRateIncrease         float64 `json:"maximum_switch_rate_increase"`
	MaximumCacheColdRateIncrease      float64 `json:"maximum_cache_cold_rate_increase"`
	MaximumBalancedLossIncrease       float64 `json:"maximum_balanced_loss_increase"`
	MaximumPredictionCalibrationError float64 `json:"maximum_prediction_calibration_error"`
	MaximumMissingFeatureRate         float64 `json:"maximum_missing_feature_rate"`
}

type RoutingCanaryMetrics struct {
	StrategyVersion            string                     `json:"strategy_version"`
	Decisions                  int64                      `json:"decisions"`
	ExpectedDecisions          int64                      `json:"expected_decisions"`
	FinalEvents                int64                      `json:"final_events"`
	ErrorEvents                int64                      `json:"error_events"`
	BillingEvents              int64                      `json:"billing_events"`
	LatencyEvents              int64                      `json:"latency_events"`
	OrphanFinalEvents          int64                      `json:"orphan_final_events"`
	ObservationDuration        time.Duration              `json:"observation_duration"`
	EventCoverage              float64                    `json:"event_coverage"`
	BillingCoverage            float64                    `json:"billing_coverage"`
	LatencyCoverage            float64                    `json:"latency_coverage"`
	FinalSuccessRate           float64                    `json:"final_success_rate"`
	FailureRisk                float64                    `json:"failure_risk"`
	SuccessRateLowerBound      float64                    `json:"success_rate_lower_bound"`
	SuccessRateUpperBound      float64                    `json:"success_rate_upper_bound"`
	P95LatencyMS               float64                    `json:"p95_latency_ms"`
	P95TTFTMS                  float64                    `json:"p95_ttft_ms"`
	P99LatencyMS               float64                    `json:"p99_latency_ms"`
	P99TTFTMS                  float64                    `json:"p99_ttft_ms"`
	CostPerSuccess             float64                    `json:"cost_per_success"`
	ExpectedSuccessfulCost     float64                    `json:"expected_successful_cost"`
	ExpectedTimeToSuccessMS    float64                    `json:"expected_time_to_success_ms"`
	ExpectedTTFTToSuccessMS    float64                    `json:"expected_ttft_to_success_ms"`
	SupplierCostPerDecision    float64                    `json:"supplier_cost_per_decision"`
	SwitchRate                 float64                    `json:"switch_rate"`
	AverageGroupSwitches       float64                    `json:"average_group_switches"`
	StickyBreakRate            float64                    `json:"sticky_break_rate"`
	CacheColdRate              float64                    `json:"cache_cold_rate"`
	StabilityLoss              float64                    `json:"stability_loss"`
	ScoreLossMappingVersion    string                     `json:"score_loss_mapping_version"`
	PredictionCalibrationError float64                    `json:"prediction_calibration_error"`
	MissingFeatureRate         float64                    `json:"missing_feature_rate"`
	FeatureSchemaVersionCount  int64                      `json:"feature_schema_version_count"`
	FeatureSchemaVersion       string                     `json:"feature_schema_version"`
	FeatureDriftDetected       bool                       `json:"feature_drift_detected"`
	CriticalSlicesHealthy      bool                       `json:"critical_slices_healthy"`
	CriticalSlices             []RoutingCanarySliceMetric `json:"critical_slices,omitempty"`
}

const RoutingScoreLossMappingVersion = "routing-score-loss-map-v1"

type RoutingCanarySliceMetric struct {
	APIKeyID              int64   `json:"api_key_id"`
	Decisions             int64   `json:"decisions"`
	FinalEvents           int64   `json:"final_events"`
	FinalSuccessRate      float64 `json:"final_success_rate"`
	SuccessRateLowerBound float64 `json:"success_rate_lower_bound"`
	SuccessRateUpperBound float64 `json:"success_rate_upper_bound"`
}

type RoutingCanaryEvaluation struct {
	Ready                    bool                 `json:"ready"`
	Rollback                 bool                 `json:"rollback"`
	PromotionEligible        bool                 `json:"promotion_eligible"`
	Preference               string               `json:"preference"`
	MetricPolicyVersion      string               `json:"metric_policy_version"`
	ScoreLossMappingVersion  string               `json:"score_loss_mapping_version"`
	BaselineStrategyVersion  string               `json:"baseline_strategy_version,omitempty"`
	CandidateStrategyVersion string               `json:"candidate_strategy_version,omitempty"`
	PrimaryMetric            string               `json:"primary_metric"`
	BaselinePrimaryValue     float64              `json:"baseline_primary_value"`
	CandidatePrimaryValue    float64              `json:"candidate_primary_value"`
	BalancedLossDifference   float64              `json:"balanced_loss_difference"`
	SuccessRateDrift         float64              `json:"success_rate_drift"`
	CostDriftRatio           float64              `json:"cost_drift_ratio"`
	LatencyDriftRatio        float64              `json:"latency_drift_ratio"`
	Violations               []string             `json:"violations"`
	BaselineSamples          int64                `json:"baseline_samples"`
	CandidateSamples         int64                `json:"candidate_samples"`
	BaselineMetrics          RoutingCanaryMetrics `json:"baseline_metrics"`
	CandidateMetrics         RoutingCanaryMetrics `json:"candidate_metrics"`
}

type RoutingScoreLossMapping struct {
	MappingVersion     string                    `json:"mapping_version"`
	StrategyVersion    string                    `json:"strategy_version"`
	Preference         string                    `json:"preference"`
	OnlineScoreWeights APIKeyRoutingScoreWeights `json:"online_score_weights"`
	FailureRiskWeight  float64                   `json:"failure_risk_weight"`
	CostWeight         float64                   `json:"cost_weight"`
	TimeWeight         float64                   `json:"time_weight"`
	CapacityRiskWeight float64                   `json:"capacity_risk_weight"`
	StabilityMode      string                    `json:"stability_mode"`
}

// RoutingScoreLossMappingForPolicy makes the online-positive/offline-loss
// mapping auditable under the same immutable strategy version. Stability is a
// separate hard promotion guardrail because it is enforced online through
// stickiness, hysteresis and traffic-change limits rather than a candidate
// score component.
func RoutingScoreLossMappingForPolicy(policy APIKeyRoutingStrategyPolicy) (RoutingScoreLossMapping, error) {
	if err := ValidateAPIKeyRoutingStrategyPolicy(policy); err != nil {
		return RoutingScoreLossMapping{}, err
	}
	return RoutingScoreLossMapping{
		MappingVersion: RoutingScoreLossMappingVersion, StrategyVersion: policy.Version, Preference: policy.Preference,
		OnlineScoreWeights: policy.Weights, FailureRiskWeight: policy.Weights.Success,
		CostWeight: policy.Weights.Price, TimeWeight: policy.Weights.Speed,
		CapacityRiskWeight: policy.Weights.Capacity, StabilityMode: "hard_guardrail",
	}, nil
}

func DefaultRoutingCanaryGuardrails() RoutingCanaryGuardrails {
	return RoutingCanaryGuardrails{
		MetricPolicyVersion: "routing-promotion-metrics-v1",
		MinimumDecisions:    1000, MinimumObservationSeconds: 3600, MinimumEventCoverage: 0.99,
		MinimumCriticalSliceDecisions: 100,
		MinimumFinalSuccessRate:       0.5, MaximumSuccessRateDrop: 0.01,
		MaximumCriticalSliceSuccessDrop: 0.02,
		MaximumP95LatencyIncreaseRatio:  0.10, MaximumP99LatencyIncreaseRatio: 0.15,
		MaximumP95TTFTIncreaseRatio: 0.10, MaximumP99TTFTIncreaseRatio: 0.15,
		MaximumCostIncreaseRatio:  0.10,
		MaximumSwitchRateIncrease: 0.02, MaximumCacheColdRateIncrease: 0.02,
		MaximumBalancedLossIncrease: 0.02, MaximumPredictionCalibrationError: 0.25,
		MaximumMissingFeatureRate: 0.05,
	}
}

func ParseRoutingCanaryGuardrails(raw json.RawMessage) (RoutingCanaryGuardrails, error) {
	guardrails := DefaultRoutingCanaryGuardrails()
	if len(raw) == 0 || string(raw) == "{}" {
		return guardrails, nil
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&guardrails); err != nil {
		return RoutingCanaryGuardrails{}, fmt.Errorf("%w: canary guardrails: %v", ErrRoutingArtifactInvalid, err)
	}
	if strings.TrimSpace(guardrails.MetricPolicyVersion) == "" || len(guardrails.MetricPolicyVersion) > 64 ||
		guardrails.MinimumDecisions < 10 || guardrails.MinimumDecisions > 10_000_000 ||
		guardrails.MinimumCriticalSliceDecisions < 10 || guardrails.MinimumCriticalSliceDecisions > 1_000_000 ||
		guardrails.MinimumObservationSeconds < 60 || guardrails.MinimumObservationSeconds > 30*24*3600 ||
		!routingGuardrailRate(guardrails.MinimumEventCoverage) || guardrails.MinimumEventCoverage < 0.9 ||
		!routingGuardrailRate(guardrails.MinimumFinalSuccessRate) || guardrails.MinimumFinalSuccessRate < 0.5 ||
		!routingGuardrailDelta(guardrails.MaximumSuccessRateDrop) ||
		!routingGuardrailDelta(guardrails.MaximumCriticalSliceSuccessDrop) ||
		!routingGuardrailDelta(guardrails.MaximumP95LatencyIncreaseRatio) ||
		!routingGuardrailDelta(guardrails.MaximumP99LatencyIncreaseRatio) ||
		!routingGuardrailDelta(guardrails.MaximumP95TTFTIncreaseRatio) ||
		!routingGuardrailDelta(guardrails.MaximumP99TTFTIncreaseRatio) ||
		!routingGuardrailDelta(guardrails.MaximumCostIncreaseRatio) ||
		!routingGuardrailDelta(guardrails.MaximumSwitchRateIncrease) ||
		!routingGuardrailDelta(guardrails.MaximumCacheColdRateIncrease) ||
		!routingGuardrailDelta(guardrails.MaximumBalancedLossIncrease) ||
		!routingGuardrailRate(guardrails.MaximumPredictionCalibrationError) ||
		!routingGuardrailRate(guardrails.MaximumMissingFeatureRate) {
		return RoutingCanaryGuardrails{}, fmt.Errorf("%w: canary guardrails out of bounds", ErrRoutingArtifactInvalid)
	}
	return guardrails, nil
}

func EvaluateRoutingCanary(guardrails RoutingCanaryGuardrails, baseline, candidate RoutingCanaryMetrics) RoutingCanaryEvaluation {
	return EvaluateRoutingCanaryForPreference(guardrails, APIKeySmartPreferenceBalanced, baseline, candidate)
}

// EvaluateRoutingCanaryForPreference keeps reliability as a common hard gate,
// then evaluates the user-selected objective with a versioned metric contract:
// price uses cost per successful request; speed uses successful TTFT when
// available (otherwise completion P95) under a cost ceiling; balanced uses an
// explainable dimensionless experience-loss delta.
func EvaluateRoutingCanaryForPreference(guardrails RoutingCanaryGuardrails, preference string, baseline, candidate RoutingCanaryMetrics) RoutingCanaryEvaluation {
	if !oneOf(preference, APIKeySmartPreferencePrice, APIKeySmartPreferenceSpeed, APIKeySmartPreferenceBalanced) {
		preference = APIKeySmartPreferenceBalanced
	}
	evaluation := RoutingCanaryEvaluation{
		Preference: preference, MetricPolicyVersion: guardrails.MetricPolicyVersion,
		ScoreLossMappingVersion: RoutingScoreLossMappingVersion,
		BaselineStrategyVersion: baseline.StrategyVersion, CandidateStrategyVersion: candidate.StrategyVersion,
		BaselineSamples: baseline.Decisions, CandidateSamples: candidate.Decisions,
		BaselineMetrics: baseline, CandidateMetrics: candidate,
	}
	evaluation.SuccessRateDrift = candidate.FinalSuccessRate - baseline.FinalSuccessRate
	evaluation.CostDriftRatio = routingIncreaseRatio(baseline.ExpectedSuccessfulCost, candidate.ExpectedSuccessfulCost)
	if baseline.ExpectedSuccessfulCost <= 0 || candidate.ExpectedSuccessfulCost <= 0 {
		evaluation.CostDriftRatio = routingIncreaseRatio(baseline.CostPerSuccess, candidate.CostPerSuccess)
	}
	evaluation.LatencyDriftRatio = routingIncreaseRatio(baseline.ExpectedTimeToSuccessMS, candidate.ExpectedTimeToSuccessMS)
	if baseline.ExpectedTimeToSuccessMS <= 0 || candidate.ExpectedTimeToSuccessMS <= 0 {
		evaluation.LatencyDriftRatio = routingIncreaseRatio(baseline.P95LatencyMS, candidate.P95LatencyMS)
	}
	minimumDuration := time.Duration(guardrails.MinimumObservationSeconds) * time.Second
	if baseline.Decisions >= guardrails.MinimumDecisions && baseline.ObservationDuration >= minimumDuration &&
		candidate.ObservationDuration >= minimumDuration && candidate.ExpectedDecisions >= guardrails.MinimumDecisions &&
		candidate.Decisions*2 < candidate.ExpectedDecisions {
		evaluation.Ready = true
		evaluation.Rollback = true
		evaluation.Violations = []string{"event_coverage_incomplete"}
		return evaluation
	}
	if baseline.Decisions < guardrails.MinimumDecisions || candidate.Decisions < guardrails.MinimumDecisions ||
		baseline.ObservationDuration < minimumDuration || candidate.ObservationDuration < minimumDuration {
		return evaluation
	}
	evaluation.Ready = true
	if baseline.EventCoverage < guardrails.MinimumEventCoverage || candidate.EventCoverage < guardrails.MinimumEventCoverage {
		evaluation.Violations = append(evaluation.Violations, "event_coverage_incomplete")
	}
	if (baseline.FinalEvents > 0 && baseline.BillingCoverage < guardrails.MinimumEventCoverage) ||
		(candidate.FinalEvents > 0 && candidate.BillingCoverage < guardrails.MinimumEventCoverage) {
		evaluation.Violations = append(evaluation.Violations, "billing_coverage_incomplete")
	}
	if (baseline.FinalEvents > 0 && baseline.LatencyCoverage < guardrails.MinimumEventCoverage) ||
		(candidate.FinalEvents > 0 && candidate.LatencyCoverage < guardrails.MinimumEventCoverage) {
		evaluation.Violations = append(evaluation.Violations, "latency_coverage_incomplete")
	}
	if !baseline.CriticalSlicesHealthy || !candidate.CriticalSlicesHealthy {
		evaluation.Violations = append(evaluation.Violations, "critical_slice_unhealthy")
	}
	if !routingCanarySlicesHealthy(guardrails, baseline) || !routingCanarySlicesHealthy(guardrails, candidate) {
		evaluation.Violations = append(evaluation.Violations, "critical_slice_regression")
	}
	if candidate.FinalSuccessRate < guardrails.MinimumFinalSuccessRate ||
		baseline.FinalSuccessRate-candidate.FinalSuccessRate > guardrails.MaximumSuccessRateDrop {
		evaluation.Violations = append(evaluation.Violations, "final_success_rate")
	}
	if candidate.FinalEvents > 0 && (candidate.SuccessRateLowerBound < guardrails.MinimumFinalSuccessRate ||
		baseline.SuccessRateLowerBound-candidate.SuccessRateLowerBound > guardrails.MaximumSuccessRateDrop) {
		evaluation.Violations = append(evaluation.Violations, "success_confidence_interval")
	}
	if routingIncreaseRatio(baseline.P95LatencyMS, candidate.P95LatencyMS) > guardrails.MaximumP95LatencyIncreaseRatio {
		evaluation.Violations = append(evaluation.Violations, "p95_latency")
	}
	if routingIncreaseRatio(baseline.P99LatencyMS, candidate.P99LatencyMS) > guardrails.MaximumP99LatencyIncreaseRatio {
		evaluation.Violations = append(evaluation.Violations, "p99_latency")
	}
	if routingIncreaseRatio(baseline.P95TTFTMS, candidate.P95TTFTMS) > guardrails.MaximumP95TTFTIncreaseRatio {
		evaluation.Violations = append(evaluation.Violations, "p95_ttft")
	}
	if routingIncreaseRatio(baseline.P99TTFTMS, candidate.P99TTFTMS) > guardrails.MaximumP99TTFTIncreaseRatio {
		evaluation.Violations = append(evaluation.Violations, "p99_ttft")
	}
	if routingIncreaseRatio(baseline.CostPerSuccess, candidate.CostPerSuccess) > guardrails.MaximumCostIncreaseRatio {
		evaluation.Violations = append(evaluation.Violations, "cost_per_success")
	}
	if candidate.SwitchRate-baseline.SwitchRate > guardrails.MaximumSwitchRateIncrease {
		evaluation.Violations = append(evaluation.Violations, "switch_rate")
	}
	if candidate.CacheColdRate-baseline.CacheColdRate > guardrails.MaximumCacheColdRateIncrease {
		evaluation.Violations = append(evaluation.Violations, "cache_cold_rate")
	}
	if baseline.FeatureDriftDetected || candidate.FeatureDriftDetected ||
		(baseline.FeatureSchemaVersion != "" && candidate.FeatureSchemaVersion != "" && baseline.FeatureSchemaVersion != candidate.FeatureSchemaVersion) {
		evaluation.Violations = append(evaluation.Violations, "feature_schema_drift")
	}
	if baseline.MissingFeatureRate > guardrails.MaximumMissingFeatureRate || candidate.MissingFeatureRate > guardrails.MaximumMissingFeatureRate {
		evaluation.Violations = append(evaluation.Violations, "feature_missing_rate")
	}
	if baseline.PredictionCalibrationError > guardrails.MaximumPredictionCalibrationError ||
		candidate.PredictionCalibrationError > guardrails.MaximumPredictionCalibrationError {
		evaluation.Violations = append(evaluation.Violations, "prediction_calibration")
	}
	switch preference {
	case APIKeySmartPreferencePrice:
		if baseline.ExpectedSuccessfulCost > 0 && candidate.ExpectedSuccessfulCost > 0 {
			evaluation.PrimaryMetric = "expected_successful_cost"
			evaluation.BaselinePrimaryValue, evaluation.CandidatePrimaryValue = baseline.ExpectedSuccessfulCost, candidate.ExpectedSuccessfulCost
		} else {
			evaluation.PrimaryMetric = "cost_per_success"
			evaluation.BaselinePrimaryValue, evaluation.CandidatePrimaryValue = baseline.CostPerSuccess, candidate.CostPerSuccess
		}
	case APIKeySmartPreferenceSpeed:
		if baseline.ExpectedTTFTToSuccessMS > 0 && candidate.ExpectedTTFTToSuccessMS > 0 {
			evaluation.PrimaryMetric = "expected_ttft_to_success_ms"
			evaluation.BaselinePrimaryValue, evaluation.CandidatePrimaryValue = baseline.ExpectedTTFTToSuccessMS, candidate.ExpectedTTFTToSuccessMS
		} else if baseline.ExpectedTimeToSuccessMS > 0 && candidate.ExpectedTimeToSuccessMS > 0 {
			evaluation.PrimaryMetric = "expected_time_to_success_ms"
			evaluation.BaselinePrimaryValue, evaluation.CandidatePrimaryValue = baseline.ExpectedTimeToSuccessMS, candidate.ExpectedTimeToSuccessMS
		} else if baseline.P95TTFTMS > 0 && candidate.P95TTFTMS > 0 {
			evaluation.PrimaryMetric = "successful_p95_ttft_ms"
			evaluation.BaselinePrimaryValue, evaluation.CandidatePrimaryValue = baseline.P95TTFTMS, candidate.P95TTFTMS
		} else {
			evaluation.PrimaryMetric = "successful_p95_duration_ms"
			evaluation.BaselinePrimaryValue, evaluation.CandidatePrimaryValue = baseline.P95LatencyMS, candidate.P95LatencyMS
		}
	case APIKeySmartPreferenceBalanced:
		evaluation.PrimaryMetric = "balanced_experience_loss"
		evaluation.BalancedLossDifference = routingBalancedLossDifference(baseline, candidate)
		evaluation.BaselinePrimaryValue, evaluation.CandidatePrimaryValue = 0, evaluation.BalancedLossDifference
		if evaluation.BalancedLossDifference > guardrails.MaximumBalancedLossIncrease {
			evaluation.Violations = append(evaluation.Violations, "balanced_experience_loss")
		}
	}
	evaluation.Rollback = len(evaluation.Violations) > 0
	primaryComparable := evaluation.PrimaryMetric == "balanced_experience_loss" ||
		(evaluation.BaselinePrimaryValue > 0 && evaluation.CandidatePrimaryValue > 0)
	evaluation.PromotionEligible = !evaluation.Rollback && primaryComparable &&
		evaluation.CandidatePrimaryValue <= evaluation.BaselinePrimaryValue
	return evaluation
}

func (m *RoutingArtifactManager) EvaluateCanaryAndRollback(ctx context.Context, experiment *RoutingExperiment, baseline, candidate RoutingCanaryMetrics) (RoutingCanaryEvaluation, error) {
	if experiment == nil || experiment.Status != RoutingLifecycleCanary {
		return RoutingCanaryEvaluation{}, ErrRoutingLifecycleConflict
	}
	guardrails, err := ParseRoutingCanaryGuardrails(experiment.Guardrails)
	if err != nil {
		return RoutingCanaryEvaluation{}, err
	}
	evaluation := EvaluateRoutingCanaryForPreference(guardrails, experiment.Preference, baseline, candidate)
	evidence, marshalErr := json.Marshal(evaluation)
	if marshalErr != nil {
		return evaluation, fmt.Errorf("marshal routing canary evidence: %w", marshalErr)
	}
	if err := m.repo.UpdateExperimentEvidence(ctx, experiment.ID, experiment.Status, nil, evidence, m.now()); err != nil {
		return evaluation, err
	}
	if !evaluation.Rollback {
		return evaluation, nil
	}
	scope := RoutingArtifactScope{
		ArtifactKind: RoutingArtifactStrategy, Platform: experiment.Platform, ModelFamily: experiment.ModelFamily,
		EndpointKind: experiment.EndpointKind, Preference: &experiment.Preference,
	}
	reason := "canary_guardrail:" + strings.Join(evaluation.Violations, ",")
	_, err = m.RollbackToBaselineWithReason(ctx, scope, reason)
	return evaluation, err
}

func routingBalancedLossDifference(baseline, candidate RoutingCanaryMetrics) float64 {
	// Weights are part of routing-promotion-metrics-v1. Reliability remains the
	// largest component, matching the execution policy's common hard baseline.
	failureLoss := candidate.FailureRisk - baseline.FailureRisk
	if baseline.FailureRisk == 0 && candidate.FailureRisk == 0 {
		failureLoss = baseline.FinalSuccessRate - candidate.FinalSuccessRate
	}
	baselineTime, candidateTime := baseline.ExpectedTimeToSuccessMS, candidate.ExpectedTimeToSuccessMS
	if baselineTime <= 0 || candidateTime <= 0 {
		baselineTime, candidateTime = baseline.P95LatencyMS, candidate.P95LatencyMS
	}
	baselineCost, candidateCost := baseline.ExpectedSuccessfulCost, candidate.ExpectedSuccessfulCost
	if baselineCost <= 0 || candidateCost <= 0 {
		baselineCost, candidateCost = baseline.CostPerSuccess, candidate.CostPerSuccess
	}
	timeLoss := routingBoundedIncreaseRatio(baselineTime, candidateTime)
	costLoss := routingBoundedIncreaseRatio(baselineCost, candidateCost)
	stabilityLoss := candidate.StabilityLoss - baseline.StabilityLoss
	if baseline.StabilityLoss == 0 && candidate.StabilityLoss == 0 {
		stabilityLoss = .5*(candidate.SwitchRate-baseline.SwitchRate) + .5*(candidate.CacheColdRate-baseline.CacheColdRate)
	}
	return 0.50*failureLoss + 0.20*timeLoss + 0.15*costLoss + 0.15*stabilityLoss
}

func routingBoundedIncreaseRatio(baseline, candidate float64) float64 {
	value := routingIncreaseRatio(baseline, candidate)
	if math.IsInf(value, 1) {
		return 10
	}
	if math.IsInf(value, -1) {
		return -10
	}
	return math.Max(-10, math.Min(10, value))
}

// RoutingWilsonInterval returns a bounded two-sided 95% interval. Promotion
// decisions use this conservative interval instead of trusting point estimates
// from small or unusually clean samples.
func RoutingWilsonInterval(successes, total int64) (float64, float64) {
	if total <= 0 || successes < 0 || successes > total {
		return 0, 1
	}
	const z = 1.959963984540054
	n := float64(total)
	p := float64(successes) / n
	z2 := z * z
	center := (p + z2/(2*n)) / (1 + z2/n)
	half := z * math.Sqrt((p*(1-p)+z2/(4*n))/n) / (1 + z2/n)
	return math.Max(0, center-half), math.Min(1, center+half)
}

func routingCanarySlicesHealthy(guardrails RoutingCanaryGuardrails, metrics RoutingCanaryMetrics) bool {
	for _, slice := range metrics.CriticalSlices {
		if slice.Decisions < guardrails.MinimumCriticalSliceDecisions {
			continue
		}
		if slice.FinalEvents < guardrails.MinimumCriticalSliceDecisions ||
			slice.SuccessRateLowerBound < guardrails.MinimumFinalSuccessRate ||
			metrics.SuccessRateLowerBound-slice.SuccessRateLowerBound > guardrails.MaximumCriticalSliceSuccessDrop {
			return false
		}
	}
	return true
}

func routingIncreaseRatio(baseline, candidate float64) float64 {
	if baseline <= 0 || math.IsNaN(baseline) || math.IsInf(baseline, 0) {
		if candidate > baseline {
			return math.Inf(1)
		}
		return 0
	}
	return (candidate - baseline) / baseline
}

func routingGuardrailRate(value float64) bool {
	return value >= 0 && value <= 1 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func routingGuardrailDelta(value float64) bool {
	return value >= 0 && value <= 1 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
