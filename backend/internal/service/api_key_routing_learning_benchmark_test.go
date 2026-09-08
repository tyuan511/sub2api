package service

import (
	"fmt"
	"testing"
	"time"
)

func BenchmarkAPIKeyRoutingLearningApplyEightCandidates(b *testing.B) {
	now := time.Now().UTC()
	snapshot := &APIKeyRoutingScoreSnapshot{
		Version: "score-bench", FeatureVersion: "routing-features-v2", StrategyVersion: BuiltinAPIKeyRoutingStrategyVersion,
		Platform: PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses", GeneratedAt: now,
		Groups: make(map[int64]APIKeyRoutingGroupObservation, maxRoutingPredictionCandidates),
	}
	eligible := make(map[int64]bool, maxRoutingPredictionCandidates)
	residuals := make(map[routingKeyResidualKey]APIKeyRoutingResidual, maxRoutingPredictionCandidates)
	for groupID := int64(1); groupID <= maxRoutingPredictionCandidates; groupID++ {
		snapshot.Groups[groupID] = APIKeyRoutingGroupObservation{
			GroupID: groupID, SuccessRequests: 900, FailedRequests: 100, SmoothedSuccessRate: .9,
			TTFTP50Ms: 300 + float64(groupID), DurationP50Ms: 1500 + float64(groupID),
			CapacityScore: .8, CacheHitRate: .6, NormalizedRate: 1 + float64(groupID)/10,
			Confidence: .9, PriceConfidence: .9,
		}
		eligible[groupID] = true
		residuals[routingKeyResidualKey{APIKeyID: 11, GroupID: groupID}] = APIKeyRoutingResidual{
			APIKeyID: 11, GroupID: groupID, Samples: 1000, Confidence: .9,
			SuccessProbabilityDelta: .01, ExpiresAt: now.Add(time.Hour),
		}
	}
	personalization := &APIKeyRoutingPersonalization{
		Version: "personal-bench", FeatureSchemaVersion: snapshot.FeatureVersion,
		GeneratedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), MinimumSamples: 10,
		MinimumConfidence: .5, ShrinkageStrength: 100, MaxSuccessProbabilityDelta: .1,
		MaxLatencyRelativeDelta: .2, MaxNormalizedCostRelDelta: .2, MaxCapacityScoreDelta: .1,
		MaxCacheHitProbabilityDelta: .1,
		DataQuality:                 APIKeyRoutingResidualDataQuality{Healthy: true, MaxCalibrationError: .1},
		cohortResiduals:             map[routingCohortResidualKey]APIKeyRoutingResidual{}, keyResiduals: residuals,
	}
	outputs := make(map[string]APIKeyRoutingLinearComponent, len(requiredRoutingPredictionOutputs))
	for _, output := range requiredRoutingPredictionOutputs {
		outputs[output] = APIKeyRoutingLinearComponent{Coefficients: map[string]float64{routingModelFeatureConfidence: .001}}
	}
	model := &APIKeyRoutingPredictionModel{
		Version: "model-bench", FeatureSchemaVersion: snapshot.FeatureVersion,
		GeneratedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), MaxInferenceMicros: 5000,
		DataQuality:      APIKeyRoutingResidualDataQuality{Healthy: true, MaxCalibrationError: .1},
		RequiredFeatures: []string{routingModelFeatureConfidence}, Outputs: outputs,
		Bounds: APIKeyRoutingPredictionBounds{
			SuccessProbabilityDelta: .1, TTFTRelativeDelta: .2, DurationRelativeDelta: .2,
			CapacityOverflowProbabilityDelta: .1, CacheHitProbabilityDelta: .1, NormalizedCostRelativeDelta: .2,
		},
	}

	for _, benchmark := range []struct {
		name string
		fn   func()
	}{
		{"personalization", func() { _, _ = ApplyAPIKeyRoutingPersonalization(snapshot, personalization, 11, 70, eligible, now) }},
		{"model", func() { _, _ = ApplyAPIKeyRoutingPredictionModel(snapshot, model, eligible, now) }},
		{"personalization_then_model", func() {
			personalized, _ := ApplyAPIKeyRoutingPersonalization(snapshot, personalization, 11, 70, eligible, now)
			_, _ = ApplyAPIKeyRoutingPredictionModel(personalized, model, eligible, now)
		}},
	} {
		b.Run(fmt.Sprintf("%s/%d", benchmark.name, maxRoutingPredictionCandidates), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				benchmark.fn()
			}
		})
	}
}
