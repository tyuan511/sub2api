package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEvaluateRoutingCanaryBlocksOnSuccessAndDataQuality(t *testing.T) {
	guardrails := DefaultRoutingCanaryGuardrails()
	baseline := RoutingCanaryMetrics{
		Decisions: 2000, ObservationDuration: 2 * time.Hour, EventCoverage: 1,
		FinalSuccessRate: 0.99, P95LatencyMS: 1000, CostPerSuccess: 1, SwitchRate: 0.01,
		CacheColdRate: 0.01, CriticalSlicesHealthy: true,
	}
	candidate := baseline
	candidate.FinalSuccessRate = 0.97
	candidate.EventCoverage = 0.95

	evaluation := EvaluateRoutingCanary(guardrails, baseline, candidate)
	require.True(t, evaluation.Ready)
	require.True(t, evaluation.Rollback)
	require.ElementsMatch(t, []string{"event_coverage_incomplete", "final_success_rate"}, evaluation.Violations)
}

func TestEvaluateRoutingCanaryBlocksP99CalibrationMissingFeaturesAndSchemaDrift(t *testing.T) {
	guardrails := DefaultRoutingCanaryGuardrails()
	baseline := RoutingCanaryMetrics{
		Decisions: 2000, ObservationDuration: 2 * time.Hour, EventCoverage: 1,
		FinalSuccessRate: 0.99, P95LatencyMS: 1000, P99LatencyMS: 2000,
		ExpectedTimeToSuccessMS: 1000, CostPerSuccess: 1, CriticalSlicesHealthy: true,
	}
	candidate := baseline
	candidate.P99LatencyMS = 2500
	candidate.ExpectedTimeToSuccessMS = 1250
	candidate.PredictionCalibrationError = .3
	candidate.MissingFeatureRate = .1
	candidate.FeatureDriftDetected = true

	evaluation := EvaluateRoutingCanaryForPreference(guardrails, APIKeySmartPreferencePrice, baseline, candidate)
	require.True(t, evaluation.Rollback)
	require.ElementsMatch(t, []string{
		"p99_latency", "feature_schema_drift", "feature_missing_rate", "prediction_calibration",
	}, evaluation.Violations)
	require.Equal(t, 0.0, evaluation.SuccessRateDrift)
	require.InDelta(t, .25, evaluation.LatencyDriftRatio, 1e-9)
}

func TestEvaluateRoutingCanaryWaitsForMinimumEvidence(t *testing.T) {
	guardrails := DefaultRoutingCanaryGuardrails()
	metrics := RoutingCanaryMetrics{Decisions: 50, ObservationDuration: 2 * time.Hour, EventCoverage: 1, CriticalSlicesHealthy: true}
	evaluation := EvaluateRoutingCanary(guardrails, metrics, metrics)
	require.False(t, evaluation.Ready)
	require.False(t, evaluation.Rollback)
}

func TestEvaluateRoutingCanaryStopsWhenExpectedExperimentFactsAreMissing(t *testing.T) {
	guardrails := DefaultRoutingCanaryGuardrails()
	baseline := RoutingCanaryMetrics{
		Decisions: 20_000, ObservationDuration: 2 * time.Hour, EventCoverage: 1,
		FinalSuccessRate: 0.99, CriticalSlicesHealthy: true,
	}
	candidate := RoutingCanaryMetrics{
		Decisions: 10, ExpectedDecisions: 1000, ObservationDuration: 2 * time.Hour,
		EventCoverage: 1, FinalSuccessRate: 0.99, CriticalSlicesHealthy: true,
	}
	evaluation := EvaluateRoutingCanary(guardrails, baseline, candidate)
	require.True(t, evaluation.Ready)
	require.True(t, evaluation.Rollback)
	require.Equal(t, []string{"event_coverage_incomplete"}, evaluation.Violations)
}

func TestEvaluateRoutingCanaryUsesPreferenceSpecificPromotionMetric(t *testing.T) {
	guardrails := DefaultRoutingCanaryGuardrails()
	baseline := RoutingCanaryMetrics{
		Decisions: 2000, ObservationDuration: 2 * time.Hour, EventCoverage: 1,
		FinalSuccessRate: 0.99, P95LatencyMS: 1000, P95TTFTMS: 200, CostPerSuccess: 2,
		SwitchRate: 0.01, CacheColdRate: 0.01, CriticalSlicesHealthy: true,
	}
	candidate := baseline
	candidate.CostPerSuccess = 1.8
	candidate.P95TTFTMS = 220

	price := EvaluateRoutingCanaryForPreference(guardrails, APIKeySmartPreferencePrice, baseline, candidate)
	require.Equal(t, "cost_per_success", price.PrimaryMetric)
	require.True(t, price.PromotionEligible)

	speed := EvaluateRoutingCanaryForPreference(guardrails, APIKeySmartPreferenceSpeed, baseline, candidate)
	require.Equal(t, "successful_p95_ttft_ms", speed.PrimaryMetric)
	require.False(t, speed.PromotionEligible)

	candidate.P95TTFTMS = 180
	speed = EvaluateRoutingCanaryForPreference(guardrails, APIKeySmartPreferenceSpeed, baseline, candidate)
	require.True(t, speed.PromotionEligible)
}

func TestEvaluateRoutingCanaryPrefersFullChainSuccessMetrics(t *testing.T) {
	guardrails := DefaultRoutingCanaryGuardrails()
	baseline := RoutingCanaryMetrics{
		Decisions: 2000, ObservationDuration: 2 * time.Hour, EventCoverage: 1,
		FinalSuccessRate: 0.99, ExpectedSuccessfulCost: 2, ExpectedTTFTToSuccessMS: 240,
		ExpectedTimeToSuccessMS: 1100, P95LatencyMS: 1500, P99LatencyMS: 2000,
		CostPerSuccess: 99, CriticalSlicesHealthy: true,
	}
	candidate := baseline
	candidate.ExpectedSuccessfulCost = 1.8
	candidate.ExpectedTTFTToSuccessMS = 220

	price := EvaluateRoutingCanaryForPreference(guardrails, APIKeySmartPreferencePrice, baseline, candidate)
	require.Equal(t, "expected_successful_cost", price.PrimaryMetric)
	require.True(t, price.PromotionEligible)

	speed := EvaluateRoutingCanaryForPreference(guardrails, APIKeySmartPreferenceSpeed, baseline, candidate)
	require.Equal(t, "expected_ttft_to_success_ms", speed.PrimaryMetric)
	require.True(t, speed.PromotionEligible)
}

func TestEvaluateRoutingCanaryDetectsFeatureSchemaVersionDrift(t *testing.T) {
	guardrails := DefaultRoutingCanaryGuardrails()
	baseline := RoutingCanaryMetrics{
		Decisions: 2000, ObservationDuration: 2 * time.Hour, EventCoverage: 1,
		FinalSuccessRate: 0.99, P95LatencyMS: 1000, CostPerSuccess: 1,
		FeatureSchemaVersion: "features-v1", CriticalSlicesHealthy: true,
	}
	candidate := baseline
	candidate.FeatureSchemaVersion = "features-v2"

	evaluation := EvaluateRoutingCanaryForPreference(guardrails, APIKeySmartPreferencePrice, baseline, candidate)
	require.Contains(t, evaluation.Violations, "feature_schema_drift")
}

func TestEvaluateRoutingCanaryBalancedUsesVersionedExperienceLoss(t *testing.T) {
	guardrails := DefaultRoutingCanaryGuardrails()
	baseline := RoutingCanaryMetrics{
		Decisions: 2000, ObservationDuration: 2 * time.Hour, EventCoverage: 1,
		FinalSuccessRate: 0.99, P95LatencyMS: 1000, CostPerSuccess: 2,
		SwitchRate: 0.01, CacheColdRate: 0.01, CriticalSlicesHealthy: true,
	}
	candidate := baseline
	candidate.P95LatencyMS = 900
	candidate.CostPerSuccess = 1.9
	evaluation := EvaluateRoutingCanaryForPreference(guardrails, APIKeySmartPreferenceBalanced, baseline, candidate)
	require.Equal(t, "routing-promotion-metrics-v1", evaluation.MetricPolicyVersion)
	require.Equal(t, "balanced_experience_loss", evaluation.PrimaryMetric)
	require.Less(t, evaluation.BalancedLossDifference, 0.0)
	require.True(t, evaluation.PromotionEligible)
}

func TestRoutingScoreLossMappingUsesTheSameImmutableStrategyWeights(t *testing.T) {
	policy := DefaultAPIKeyRoutingStrategyPolicy(APIKeySmartPreferencePrice)
	policy.Version = "price-strategy-v7"

	mapping, err := RoutingScoreLossMappingForPolicy(policy)
	require.NoError(t, err)
	require.Equal(t, RoutingScoreLossMappingVersion, mapping.MappingVersion)
	require.Equal(t, policy.Version, mapping.StrategyVersion)
	require.Equal(t, policy.Weights, mapping.OnlineScoreWeights)
	require.Equal(t, policy.Weights.Success, mapping.FailureRiskWeight)
	require.Equal(t, policy.Weights.Price, mapping.CostWeight)
	require.Equal(t, policy.Weights.Speed, mapping.TimeWeight)
	require.Equal(t, policy.Weights.Capacity, mapping.CapacityRiskWeight)
	require.Equal(t, "hard_guardrail", mapping.StabilityMode)
}

func TestEvaluateRoutingCanaryBlocksCriticalKeySliceRegression(t *testing.T) {
	guardrails := DefaultRoutingCanaryGuardrails()
	lower, upper := RoutingWilsonInterval(1980, 2000)
	baseline := RoutingCanaryMetrics{
		Decisions: 2000, FinalEvents: 2000, ObservationDuration: 2 * time.Hour, EventCoverage: 1,
		BillingCoverage: 1, LatencyCoverage: 1, FinalSuccessRate: 0.99,
		SuccessRateLowerBound: lower, SuccessRateUpperBound: upper,
		P95LatencyMS: 1000, CostPerSuccess: 2, CriticalSlicesHealthy: true,
	}
	candidate := baseline
	sliceLower, sliceUpper := RoutingWilsonInterval(60, 100)
	candidate.CriticalSlices = []RoutingCanarySliceMetric{{
		APIKeyID: 7, Decisions: 100, FinalEvents: 100, FinalSuccessRate: 0.6,
		SuccessRateLowerBound: sliceLower, SuccessRateUpperBound: sliceUpper,
	}}
	evaluation := EvaluateRoutingCanaryForPreference(guardrails, APIKeySmartPreferencePrice, baseline, candidate)
	require.True(t, evaluation.Rollback)
	require.Contains(t, evaluation.Violations, "critical_slice_regression")
}

func TestRoutingWilsonIntervalShrinksWithEvidence(t *testing.T) {
	smallLower, smallUpper := RoutingWilsonInterval(9, 10)
	largeLower, largeUpper := RoutingWilsonInterval(900, 1000)
	require.Less(t, smallLower, largeLower)
	require.Greater(t, smallUpper-smallLower, largeUpper-largeLower)
}

func TestRoutingArtifactManagerCanaryGuardrailRollsBackAndRecordsReason(t *testing.T) {
	baseline := routingArtifactForManagerTest(1, "baseline-v1", RoutingLifecycleActive)
	candidate := routingArtifactForManagerTest(2, "canary-v2", RoutingLifecycleCanary)
	experiment := routingExperimentForManagerTest(baseline, candidate)
	experiment.Status = RoutingLifecycleCanary
	repo := &routingArtifactManagerRepo{
		artifacts:   map[string]*RoutingArtifactVersion{baseline.Version: baseline, candidate.Version: candidate},
		experiments: map[string]*RoutingExperiment{experiment.ExperimentKey: experiment},
	}
	cache := &routingArtifactManagerCache{
		objects: map[string]*RoutingArtifactVersion{
			baseline.Version: cloneRoutingArtifactForManagerTest(baseline), candidate.Version: cloneRoutingArtifactForManagerTest(candidate),
		},
		pointers: RoutingArtifactPointers{
			BaselineVersion: baseline.Version, ActiveVersion: baseline.Version, CanaryVersion: candidate.Version,
			CanaryAllocationBPS: experiment.AllocationBPS, CanaryExperimentID: experiment.ExperimentKey,
			CanaryBucketSaltChecksum: experiment.BucketSaltChecksum, UpdatedAt: time.Now(),
		},
		pointerReady: true,
	}
	manager := NewRoutingArtifactManager(repo, cache)
	baselineMetrics := RoutingCanaryMetrics{
		Decisions: 2000, ObservationDuration: 2 * time.Hour, EventCoverage: 1, FinalSuccessRate: 0.99,
		P95LatencyMS: 1000, CostPerSuccess: 1, CriticalSlicesHealthy: true,
	}
	candidateMetrics := baselineMetrics
	candidateMetrics.FinalSuccessRate = 0.4

	evaluation, err := manager.EvaluateCanaryAndRollback(context.Background(), experiment, baselineMetrics, candidateMetrics)
	require.NoError(t, err)
	require.True(t, evaluation.Rollback)
	require.Empty(t, cache.pointers.CanaryVersion)
	require.Equal(t, baseline.Version, cache.pointers.ActiveVersion)
	require.Equal(t, RoutingLifecyclePaused, repo.experiments[experiment.ExperimentKey].Status)
	require.Contains(t, *repo.experiments[experiment.ExperimentKey].StopReason, "final_success_rate")
}
