package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyRoutingPersonalizationColdStartShrinkageAndKeyIsolation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	bucket := APIKeyRoutingCohortBucket(70)
	artifact := routingPersonalizationArtifactForTest(t, "personal-v1", RoutingLifecycleActive, now, []APIKeyRoutingResidual{
		{CohortBucket: &bucket, GroupID: 1, Samples: 100, Confidence: 1, SuccessProbabilityDelta: .02, ExpiresAt: now.Add(time.Hour)},
	}, []APIKeyRoutingResidual{
		{APIKeyID: 11, GroupID: 1, Samples: 100, Confidence: 1, SuccessProbabilityDelta: .10, ExpiresAt: now.Add(time.Hour)},
		{APIKeyID: 22, GroupID: 1, Samples: 100, Confidence: 1, SuccessProbabilityDelta: -.10, ExpiresAt: now.Add(time.Hour)},
		{APIKeyID: 33, GroupID: 1, Samples: 9, Confidence: 1, SuccessProbabilityDelta: .10, ExpiresAt: now.Add(time.Hour)},
	})
	policy, err := ParseAPIKeyRoutingPersonalizationArtifact(artifact)
	require.NoError(t, err)
	baseline := routingLearningSnapshotForTest(now)

	eligible := map[int64]bool{1: true}
	key11, result11 := ApplyAPIKeyRoutingPersonalization(baseline, policy, 11, 70, eligible, now)
	key22, result22 := ApplyAPIKeyRoutingPersonalization(baseline, policy, 22, 71, eligible, now)
	cold, coldResult := ApplyAPIKeyRoutingPersonalization(baseline, policy, 999, 0, eligible, now)
	lowSample, lowResult := ApplyAPIKeyRoutingPersonalization(baseline, policy, 33, 0, eligible, now)

	require.Empty(t, result11.Reason)
	require.Empty(t, result22.Reason)
	require.InDelta(t, .86, key11.Groups[1].SmoothedSuccessRate, 1e-9, "cohort and Key residuals shrink independently")
	require.InDelta(t, .75, key22.Groups[1].SmoothedSuccessRate, 1e-9)
	require.Same(t, baseline, cold, "a new Key must use the immutable shared baseline")
	require.Equal(t, RoutingLearningFallbackLowSamples, coldResult.Reason)
	require.Same(t, baseline, lowSample)
	require.Equal(t, RoutingLearningFallbackLowSamples, lowResult.Reason)
	require.Equal(t, .80, baseline.Groups[1].SmoothedSuccessRate, "one Key must not mutate global or another Key's snapshot")
}

func TestAPIKeyRoutingPersonalizationDisablesDriftedArtifact(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	artifact := routingPersonalizationArtifactForTest(t, "personal-drift", RoutingLifecycleActive, now, nil, []APIKeyRoutingResidual{
		{APIKeyID: 11, GroupID: 1, Samples: 1000, Confidence: 1, SuccessProbabilityDelta: .10, ExpiresAt: now.Add(time.Hour)},
	})
	var payload apiKeyRoutingPersonalizationPayload
	require.NoError(t, json.Unmarshal(artifact.Payload, &payload))
	payload.DataQuality.Drifted = true
	setRoutingArtifactPayloadForTest(t, artifact, payload)
	policy, err := ParseAPIKeyRoutingPersonalizationArtifact(artifact)
	require.NoError(t, err)
	baseline := routingLearningSnapshotForTest(now)
	actual, result := ApplyAPIKeyRoutingPersonalization(baseline, policy, 11, 1, map[int64]bool{1: true}, now)
	require.Same(t, baseline, actual)
	require.Equal(t, RoutingLearningFallbackDrift, result.Reason)
}

func TestAPIKeyRoutingPredictionIsBoundedAndOnlyTouchesRuleEligibleCandidates(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	artifact := routingPredictionArtifactForTest(t, "model-v1", RoutingLifecycleActive, now)
	model, err := ParseAPIKeyRoutingPredictionModel(artifact)
	require.NoError(t, err)
	baseline := routingLearningSnapshotForTest(now)
	baseline.Groups[2] = APIKeyRoutingGroupObservation{
		GroupID: 2, SuccessRequests: 40, FailedRequests: 60, SmoothedSuccessRate: .40,
		TTFTP50Ms: 200, DurationP50Ms: 800, CapacityScore: .7, CacheHitRate: .4,
		NormalizedRate: 1.2, Confidence: .9, PriceConfidence: .9,
	}

	actual, result := ApplyAPIKeyRoutingPredictionModel(baseline, model, map[int64]bool{1: true}, now)
	require.Empty(t, result.Reason)
	require.Equal(t, "model-v1", result.Version)
	require.NotNil(t, actual.ModelVersion)
	require.Equal(t, "model-v1", *actual.ModelVersion)
	require.InDelta(t, .85, actual.Groups[1].SmoothedSuccessRate, 1e-9)
	require.Equal(t, baseline.Groups[2], actual.Groups[2], "an ineligible candidate must never enter model evaluation")
	require.Equal(t, .80, baseline.Groups[1].SmoothedSuccessRate, "prediction must not mutate the shared snapshot")
}

func TestAPIKeyRoutingPredictionTimeoutFallsBackAtomically(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	artifact := routingPredictionArtifactForTest(t, "model-timeout", RoutingLifecycleActive, now)
	model, err := ParseAPIKeyRoutingPredictionModel(artifact)
	require.NoError(t, err)
	model.MaxInferenceMicros = 10
	baseline := routingLearningSnapshotForTest(now)
	call := 0
	clock := func() time.Time {
		value := now.Add(time.Duration(call) * 20 * time.Microsecond)
		call++
		return value
	}
	actual, result := applyAPIKeyRoutingPredictionModelWithClock(baseline, model, map[int64]bool{1: true}, now, clock)
	require.Same(t, baseline, actual)
	require.Equal(t, RoutingLearningFallbackTimeout, result.Reason)
	require.GreaterOrEqual(t, result.Duration, 20*time.Microsecond)
	require.Nil(t, baseline.ModelVersion)
}

func TestRoutingPredictionArtifactRejectsCorruptionAndUnknownFeatures(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	artifact := routingPredictionArtifactForTest(t, "model-corrupt", RoutingLifecycleDraft, now)
	require.NoError(t, ValidateRoutingArtifact(artifact))
	artifact.Checksum = "00"
	require.ErrorContains(t, ValidateRoutingArtifact(artifact), "checksum")

	artifact = routingPredictionArtifactForTest(t, "model-feature", RoutingLifecycleDraft, now)
	var payload apiKeyRoutingPredictionModelPayload
	require.NoError(t, json.Unmarshal(artifact.Payload, &payload))
	payload.RequiredFeatures = []string{"prompt_text"}
	setRoutingArtifactPayloadForTest(t, artifact, payload)
	require.ErrorContains(t, ValidateRoutingArtifact(artifact), "unapproved prediction feature")
}

func TestRoutingLearningArtifactLifecycleRequiresShadowCanaryAndApproval(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	baseline := routingPredictionArtifactForTest(t, "model-baseline", RoutingLifecycleActive, now)
	baseline.ID = 1
	candidate := routingPredictionArtifactForTest(t, "model-candidate", RoutingLifecycleDraft, now)
	candidate.ID = 2
	preference := APIKeySmartPreferenceBalanced
	baseline.Preference, candidate.Preference = &preference, &preference
	salt := sha256.Sum256([]byte("learning-canary"))
	governance := &RoutingExperiment{
		ID: 7, ExperimentKey: "model-canary-audit-1", Platform: PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses",
		Preference: preference, Status: RoutingLifecycleShadow, AllocationBPS: 500, BucketSaltChecksum: hex.EncodeToString(salt[:]),
	}
	repo := &routingArtifactManagerRepo{artifacts: map[string]*RoutingArtifactVersion{
		baseline.Version: baseline, candidate.Version: candidate,
	}, experiments: map[string]*RoutingExperiment{governance.ExperimentKey: governance}}
	cache := &routingArtifactManagerCache{objects: make(map[string]*RoutingArtifactVersion)}
	manager := NewRoutingArtifactManager(repo, cache)

	_, err := manager.Promote(context.Background(), RoutingArtifactPromotion{
		ArtifactKind: RoutingArtifactModel, Version: candidate.Version, TargetStatus: RoutingLifecycleActive,
		BaselineVersion: baseline.Version,
	})
	require.ErrorIs(t, err, ErrRoutingPromotionEvidence)

	_, err = manager.Promote(context.Background(), RoutingArtifactPromotion{
		ArtifactKind: RoutingArtifactModel, Version: candidate.Version, TargetStatus: RoutingLifecycleShadow,
		BaselineVersion: baseline.Version,
	})
	require.NoError(t, err)
	pointers, err := manager.Promote(context.Background(), RoutingArtifactPromotion{
		ArtifactKind: RoutingArtifactModel, Version: candidate.Version, TargetStatus: RoutingLifecycleCanary,
		CanaryAllocationBPS: 500, CanaryExperimentID: "model-canary-audit-1", CanaryBucketSaltChecksum: hex.EncodeToString(salt[:]),
	})
	require.NoError(t, err)
	require.Equal(t, candidate.Version, pointers.CanaryVersion)

	approver := int64(99)
	pointers, err = manager.Promote(context.Background(), RoutingArtifactPromotion{
		ArtifactKind: RoutingArtifactModel, Version: candidate.Version, TargetStatus: RoutingLifecycleActive, ApprovedBy: &approver,
	})
	require.NoError(t, err)
	require.Equal(t, candidate.Version, pointers.ActiveVersion)
	require.Empty(t, pointers.CanaryVersion)
}

func TestRoutingLearningRuntimeRefreshesOffPathAndAppliesFromLocalMemory(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	preference := APIKeySmartPreferenceBalanced
	baseScope := RoutingArtifactScope{
		ArtifactKind: RoutingArtifactStrategy, Platform: PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses", Preference: &preference,
	}
	feature := routingPersonalizationArtifactForTest(t, "personal-active", RoutingLifecycleActive, now, nil, []APIKeyRoutingResidual{
		{APIKeyID: 11, GroupID: 1, Samples: 1000, Confidence: 1, SuccessProbabilityDelta: .05, ExpiresAt: now.Add(time.Hour)},
	})
	feature.Preference = &preference
	model := routingPredictionArtifactForTest(t, "model-active", RoutingLifecycleActive, now)
	model.Preference = &preference
	cache := &routingLearningCacheForTest{
		pointers: map[string]RoutingArtifactPointers{}, objects: map[string]map[string]*RoutingArtifactVersion{},
	}
	cache.add(feature)
	cache.add(model)
	runtime := NewRoutingLearningRuntime(cache, true, true)
	require.NoError(t, runtime.Refresh(context.Background(), baseScope))
	loadsAfterRefresh := cache.loads

	application := runtime.Apply(baseScope, 11, 70, nil, routingLearningSnapshotForTest(now), map[int64]bool{1: true}, now)
	require.Equal(t, loadsAfterRefresh, cache.loads, "request-path Apply must not read Redis, SQL, or another remote store")
	require.Empty(t, application.Personalization.Reason)
	require.Empty(t, application.Prediction.Reason)
	require.NotNil(t, application.Snapshot.ModelVersion)
	require.Equal(t, "model-active", *application.Snapshot.ModelVersion)
	require.Greater(t, application.Snapshot.Groups[1].SmoothedSuccessRate, .80)
}

func TestRoutingLearningCanaryRequiresMatchingStrategyExperiment(t *testing.T) {
	active, canary := "active", "canary"
	checksum := sha256.Sum256([]byte("shared-canary"))
	pointers := RoutingArtifactPointers{
		CanaryVersion: "canary", CanaryAllocationBPS: 10000, CanaryExperimentID: "strategy-exp-1",
		CanaryBucketSaltChecksum: hex.EncodeToString(checksum[:]),
	}
	selected, experiment, _ := selectRoutingLearningCanary(&active, &canary, pointers, 1, 2, nil)
	require.Equal(t, "active", *selected)
	require.Nil(t, experiment)

	mismatch := "strategy-exp-2"
	selected, experiment, _ = selectRoutingLearningCanary(&active, &canary, pointers, 1, 2, &mismatch)
	require.Equal(t, "active", *selected)
	require.Nil(t, experiment)

	match := "strategy-exp-1"
	selected, experiment, _ = selectRoutingLearningCanary(&active, &canary, pointers, 1, 2, &match)
	require.Equal(t, "canary", *selected)
	require.Equal(t, match, *experiment)
}

type routingLearningCacheForTest struct {
	pointers map[string]RoutingArtifactPointers
	objects  map[string]map[string]*RoutingArtifactVersion
	loads    int
}

func (c *routingLearningCacheForTest) add(artifact *RoutingArtifactVersion) {
	scope := RoutingArtifactScopeFromVersion(artifact)
	key := routingLearningCacheKeyForTest(scope)
	if c.objects[key] == nil {
		c.objects[key] = make(map[string]*RoutingArtifactVersion)
	}
	c.objects[key][artifact.Version] = artifact
	c.pointers[key] = RoutingArtifactPointers{
		BaselineVersion: artifact.Version, ActiveVersion: artifact.Version, UpdatedAt: time.Now().UTC(),
	}
}

func (c *routingLearningCacheForTest) PublishArtifact(context.Context, *RoutingArtifactVersion) error {
	return nil
}

func (c *routingLearningCacheForTest) SwapPointers(context.Context, RoutingArtifactScope, RoutingArtifactPointers, *string) error {
	return nil
}

func (c *routingLearningCacheForTest) LoadPointers(_ context.Context, scope RoutingArtifactScope) (RoutingArtifactPointers, error) {
	c.loads++
	value, ok := c.pointers[routingLearningCacheKeyForTest(scope)]
	if !ok {
		return RoutingArtifactPointers{}, ErrRoutingArtifactUnavailable
	}
	return value, nil
}

func (c *routingLearningCacheForTest) LoadArtifact(_ context.Context, scope RoutingArtifactScope, version string) (*RoutingArtifactVersion, error) {
	c.loads++
	value := c.objects[routingLearningCacheKeyForTest(scope)][version]
	if value == nil {
		return nil, ErrRoutingArtifactUnavailable
	}
	return cloneRoutingArtifactForManagerTest(value), nil
}

func routingLearningCacheKeyForTest(scope RoutingArtifactScope) string {
	return scope.ArtifactKind + "\x00" + routingLearningScopeKey(scope)
}

func routingLearningSnapshotForTest(now time.Time) *APIKeyRoutingScoreSnapshot {
	return &APIKeyRoutingScoreSnapshot{
		Version: "score-v1", FeatureVersion: "routing-features-v2", StrategyVersion: BuiltinAPIKeyRoutingStrategyVersion,
		Platform: PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses", GeneratedAt: now,
		Groups: map[int64]APIKeyRoutingGroupObservation{
			1: {GroupID: 1, SuccessRequests: 80, FailedRequests: 20, SmoothedSuccessRate: .80,
				TTFTP50Ms: 100, DurationP50Ms: 500, CapacityScore: .8, CacheHitRate: .5,
				NormalizedRate: 1, Confidence: .9, PriceConfidence: .9},
		},
	}
}

func routingPersonalizationArtifactForTest(t *testing.T, version, status string, now time.Time, cohorts, keys []APIKeyRoutingResidual) *RoutingArtifactVersion {
	t.Helper()
	payload := apiKeyRoutingPersonalizationPayload{
		FeatureSchemaVersion: "routing-features-v2", GeneratedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
		MinimumSamples: 10, MinimumConfidence: .6, ShrinkageStrength: 100,
		MaxSuccessProbabilityDelta: .20, MaxLatencyRelativeDelta: .50, MaxNormalizedCostRelDelta: .50,
		MaxCapacityScoreDelta: .20, MaxCacheHitProbabilityDelta: .20,
		DataQuality:     APIKeyRoutingResidualDataQuality{Healthy: true, CalibrationError: .02, MaxCalibrationError: .10},
		CohortResiduals: cohorts, KeyResiduals: keys,
	}
	artifact := &RoutingArtifactVersion{
		ArtifactKind: RoutingArtifactFeature, Version: version, Platform: PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses",
		Status: status, SchemaVersion: RoutingPersonalizationSchemaVersion,
		Dependencies: json.RawMessage(`[{"artifact_kind":"score","version":"score-v1"}]`),
		Lineage:      json.RawMessage(`{"dataset_checksum":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","query_version":"routing-residual-v1","training_through":"2026-09-01T00:00:00Z"}`),
	}
	setRoutingArtifactPayloadForTest(t, artifact, payload)
	return artifact
}

func routingPredictionArtifactForTest(t *testing.T, version, status string, now time.Time) *RoutingArtifactVersion {
	t.Helper()
	outputs := make(map[string]APIKeyRoutingLinearComponent, len(requiredRoutingPredictionOutputs))
	for _, output := range requiredRoutingPredictionOutputs {
		outputs[output] = APIKeyRoutingLinearComponent{Coefficients: map[string]float64{}}
	}
	outputs[routingModelOutputSuccess] = APIKeyRoutingLinearComponent{Intercept: .05, Coefficients: map[string]float64{}}
	payload := apiKeyRoutingPredictionModelPayload{
		ModelType: RoutingPredictionModelType, FeatureSchemaVersion: "routing-features-v2",
		GeneratedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), MaxInferenceMicros: 5000, MinimumConfidence: .5,
		DataQuality:      APIKeyRoutingResidualDataQuality{Healthy: true, CalibrationError: .02, MaxCalibrationError: .10},
		RequiredFeatures: []string{routingModelFeatureSuccess}, Outputs: outputs,
		Bounds: APIKeyRoutingPredictionBounds{
			SuccessProbabilityDelta: .10, TTFTRelativeDelta: .20, DurationRelativeDelta: .20,
			CapacityOverflowProbabilityDelta: .10, CacheHitProbabilityDelta: .10, NormalizedCostRelativeDelta: .20,
		},
	}
	artifact := &RoutingArtifactVersion{
		ArtifactKind: RoutingArtifactModel, Version: version, Platform: PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses",
		Status: status, SchemaVersion: RoutingPredictionModelSchemaVersion,
		Dependencies: json.RawMessage(`[{"artifact_kind":"feature","version":"routing-features-v2"}]`),
		Lineage:      json.RawMessage(`{"dataset_checksum":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","query_version":"routing-training-v1","training_through":"2026-09-01T00:00:00Z"}`),
	}
	setRoutingArtifactPayloadForTest(t, artifact, payload)
	return artifact
}

func setRoutingArtifactPayloadForTest(t *testing.T, artifact *RoutingArtifactVersion, payload any) {
	t.Helper()
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	artifact.Payload = encoded
	sum := sha256.Sum256(encoded)
	artifact.Checksum = hex.EncodeToString(sum[:])
}
