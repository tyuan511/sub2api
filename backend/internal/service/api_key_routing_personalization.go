package service

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	RoutingPersonalizationSchemaVersion = "routing-personalization-v1"
	maxRoutingPersonalizationPayload    = 512 * 1024
	maxRoutingPersonalizationEntries    = 2048
	routingCohortBuckets                = 64
	maxRoutingPersonalizationTTL        = 7 * 24 * time.Hour
)

// APIKeyRoutingResidualDataQuality is deliberately small and versioned. A
// producer must explicitly attest that its point-in-time dataset passed quality
// checks; a stale, drifting, or badly calibrated residual is a no-op.
type APIKeyRoutingResidualDataQuality struct {
	Healthy             bool    `json:"healthy"`
	Drifted             bool    `json:"drifted"`
	CalibrationError    float64 `json:"calibration_error"`
	MaxCalibrationError float64 `json:"max_calibration_error"`
}

// APIKeyRoutingResidual contains corrections only. Raw group health and the
// 50% hard gate always come from the shared score snapshot and are never
// rewritten by a Key/cohort artifact.
type APIKeyRoutingResidual struct {
	APIKeyID                 int64     `json:"api_key_id,omitempty"`
	CohortBucket             *int      `json:"cohort_bucket,omitempty"`
	GroupID                  int64     `json:"group_id"`
	Samples                  int64     `json:"samples"`
	Confidence               float64   `json:"confidence"`
	SuccessProbabilityDelta  float64   `json:"success_probability_delta"`
	LatencyRelativeDelta     float64   `json:"latency_relative_delta"`
	NormalizedCostRelDelta   float64   `json:"normalized_cost_relative_delta"`
	CapacityScoreDelta       float64   `json:"capacity_score_delta"`
	CacheHitProbabilityDelta float64   `json:"cache_hit_probability_delta"`
	ExpiresAt                time.Time `json:"expires_at"`
}

type APIKeyRoutingPersonalization struct {
	Version                     string
	FeatureSchemaVersion        string
	GeneratedAt                 time.Time
	ExpiresAt                   time.Time
	MinimumSamples              int64
	MinimumConfidence           float64
	ShrinkageStrength           float64
	MaxSuccessProbabilityDelta  float64
	MaxLatencyRelativeDelta     float64
	MaxNormalizedCostRelDelta   float64
	MaxCapacityScoreDelta       float64
	MaxCacheHitProbabilityDelta float64
	DataQuality                 APIKeyRoutingResidualDataQuality
	cohortResiduals             map[routingCohortResidualKey]APIKeyRoutingResidual
	keyResiduals                map[routingKeyResidualKey]APIKeyRoutingResidual
}

type apiKeyRoutingPersonalizationPayload struct {
	FeatureSchemaVersion        string                           `json:"feature_schema_version"`
	GeneratedAt                 time.Time                        `json:"generated_at"`
	ExpiresAt                   time.Time                        `json:"expires_at"`
	MinimumSamples              int64                            `json:"minimum_samples"`
	MinimumConfidence           float64                          `json:"minimum_confidence"`
	ShrinkageStrength           float64                          `json:"shrinkage_strength"`
	MaxSuccessProbabilityDelta  float64                          `json:"max_success_probability_delta"`
	MaxLatencyRelativeDelta     float64                          `json:"max_latency_relative_delta"`
	MaxNormalizedCostRelDelta   float64                          `json:"max_normalized_cost_relative_delta"`
	MaxCapacityScoreDelta       float64                          `json:"max_capacity_score_delta"`
	MaxCacheHitProbabilityDelta float64                          `json:"max_cache_hit_probability_delta"`
	DataQuality                 APIKeyRoutingResidualDataQuality `json:"data_quality"`
	CohortResiduals             []APIKeyRoutingResidual          `json:"cohort_residuals"`
	KeyResiduals                []APIKeyRoutingResidual          `json:"key_residuals"`
}

type routingCohortResidualKey struct {
	Bucket  int
	GroupID int64
}

type routingKeyResidualKey struct {
	APIKeyID int64
	GroupID  int64
}

type APIKeyRoutingPersonalizationResult struct {
	Version       string
	AppliedGroups map[int64]float64
	Reason        string
	Calibration   float64
}

const (
	RoutingLearningFallbackMissing     = "missing"
	RoutingLearningFallbackStale       = "stale"
	RoutingLearningFallbackSchema      = "schema"
	RoutingLearningFallbackDataQuality = "data_quality"
	RoutingLearningFallbackDrift       = "drift"
	RoutingLearningFallbackCalibration = "calibration"
	RoutingLearningFallbackLowSamples  = "low_samples"
	RoutingLearningFallbackFeatures    = "missing_features"
	RoutingLearningFallbackTimeout     = "timeout"
	RoutingLearningFallbackNonFinite   = "non_finite"
	RoutingLearningFallbackOutOfRange  = "out_of_range"
)

// ParseAPIKeyRoutingPersonalizationArtifact converts a bounded feature artifact
// into two immutable local lookup maps. Neither map is stored in Redis per Key;
// Redis only distributes the one versioned artifact for the scope.
func ParseAPIKeyRoutingPersonalizationArtifact(artifact *RoutingArtifactVersion) (*APIKeyRoutingPersonalization, error) {
	if artifact == nil || artifact.ArtifactKind != RoutingArtifactFeature {
		return nil, fmt.Errorf("%w: personalization feature artifact required", ErrRoutingArtifactInvalid)
	}
	if artifact.SchemaVersion != RoutingPersonalizationSchemaVersion {
		return nil, fmt.Errorf("%w: incompatible personalization schema %q", ErrRoutingArtifactInvalid, artifact.SchemaVersion)
	}
	if len(artifact.Payload) == 0 || len(artifact.Payload) > maxRoutingPersonalizationPayload {
		return nil, fmt.Errorf("%w: personalization payload exceeds local memory envelope", ErrRoutingArtifactInvalid)
	}
	lineage, lineageOK := parseRoutingLearningLineage(artifact.Lineage)
	_, dependenciesOK := parseRoutingLearningDependencies(artifact.Dependencies)
	if !lineageOK || !dependenciesOK {
		return nil, fmt.Errorf("%w: personalization lineage or dependency contract is incomplete", ErrRoutingArtifactInvalid)
	}
	decoder := json.NewDecoder(strings.NewReader(string(artifact.Payload)))
	decoder.DisallowUnknownFields()
	var payload apiKeyRoutingPersonalizationPayload
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: personalization payload: %v", ErrRoutingArtifactInvalid, err)
	}
	if payload.FeatureSchemaVersion == "" || payload.GeneratedAt.IsZero() || payload.ExpiresAt.IsZero() || !payload.ExpiresAt.After(payload.GeneratedAt) ||
		payload.ExpiresAt.Sub(payload.GeneratedAt) > maxRoutingPersonalizationTTL ||
		payload.MinimumSamples < 1 || payload.MinimumSamples > 10000 ||
		payload.MinimumConfidence < 0.5 || payload.MinimumConfidence > 1 ||
		payload.ShrinkageStrength < 1 || payload.ShrinkageStrength > 100000 ||
		!validRoutingResidualBound(payload.MaxSuccessProbabilityDelta, 0.20) ||
		!validRoutingResidualBound(payload.MaxLatencyRelativeDelta, 0.50) ||
		!validRoutingResidualBound(payload.MaxNormalizedCostRelDelta, 0.50) ||
		!validRoutingResidualBound(payload.MaxCapacityScoreDelta, 0.20) ||
		!validRoutingResidualBound(payload.MaxCacheHitProbabilityDelta, 0.20) ||
		!validRoutingResidualDataQuality(payload.DataQuality) ||
		len(payload.CohortResiduals)+len(payload.KeyResiduals) > maxRoutingPersonalizationEntries {
		return nil, fmt.Errorf("%w: invalid personalization envelope", ErrRoutingArtifactInvalid)
	}
	if lineage.TrainingThrough.After(payload.GeneratedAt) {
		return nil, fmt.Errorf("%w: personalization lineage is not point-in-time compatible", ErrRoutingArtifactInvalid)
	}
	result := &APIKeyRoutingPersonalization{
		Version: artifact.Version, FeatureSchemaVersion: payload.FeatureSchemaVersion,
		GeneratedAt: payload.GeneratedAt.UTC(), ExpiresAt: payload.ExpiresAt.UTC(),
		MinimumSamples: payload.MinimumSamples, MinimumConfidence: payload.MinimumConfidence,
		ShrinkageStrength:           payload.ShrinkageStrength,
		MaxSuccessProbabilityDelta:  payload.MaxSuccessProbabilityDelta,
		MaxLatencyRelativeDelta:     payload.MaxLatencyRelativeDelta,
		MaxNormalizedCostRelDelta:   payload.MaxNormalizedCostRelDelta,
		MaxCapacityScoreDelta:       payload.MaxCapacityScoreDelta,
		MaxCacheHitProbabilityDelta: payload.MaxCacheHitProbabilityDelta,
		DataQuality:                 payload.DataQuality,
		cohortResiduals:             make(map[routingCohortResidualKey]APIKeyRoutingResidual, len(payload.CohortResiduals)),
		keyResiduals:                make(map[routingKeyResidualKey]APIKeyRoutingResidual, len(payload.KeyResiduals)),
	}
	for _, residual := range payload.CohortResiduals {
		if residual.APIKeyID != 0 || residual.CohortBucket == nil || *residual.CohortBucket < 0 || *residual.CohortBucket >= routingCohortBuckets ||
			!validAPIKeyRoutingResidual(residual, result) {
			return nil, fmt.Errorf("%w: invalid cohort residual", ErrRoutingArtifactInvalid)
		}
		key := routingCohortResidualKey{Bucket: *residual.CohortBucket, GroupID: residual.GroupID}
		if _, duplicate := result.cohortResiduals[key]; duplicate {
			return nil, fmt.Errorf("%w: duplicate cohort residual", ErrRoutingArtifactInvalid)
		}
		result.cohortResiduals[key] = residual
	}
	for _, residual := range payload.KeyResiduals {
		if residual.APIKeyID <= 0 || residual.CohortBucket != nil || !validAPIKeyRoutingResidual(residual, result) {
			return nil, fmt.Errorf("%w: invalid Key residual", ErrRoutingArtifactInvalid)
		}
		key := routingKeyResidualKey{APIKeyID: residual.APIKeyID, GroupID: residual.GroupID}
		if _, duplicate := result.keyResiduals[key]; duplicate {
			return nil, fmt.Errorf("%w: duplicate Key residual", ErrRoutingArtifactInvalid)
		}
		result.keyResiduals[key] = residual
	}
	return result, nil
}

func validRoutingResidualBound(value, maximum float64) bool {
	return value >= 0 && value <= maximum && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func validRoutingResidualDataQuality(value APIKeyRoutingResidualDataQuality) bool {
	return value.CalibrationError >= 0 && value.MaxCalibrationError >= 0 && value.MaxCalibrationError <= 1 &&
		!math.IsNaN(value.CalibrationError) && !math.IsInf(value.CalibrationError, 0) &&
		!math.IsNaN(value.MaxCalibrationError) && !math.IsInf(value.MaxCalibrationError, 0)
}

func validAPIKeyRoutingResidual(value APIKeyRoutingResidual, policy *APIKeyRoutingPersonalization) bool {
	if policy == nil || value.GroupID <= 0 || value.Samples < 0 || value.Samples > 1_000_000_000 ||
		value.Confidence < 0 || value.Confidence > 1 || math.IsNaN(value.Confidence) || math.IsInf(value.Confidence, 0) ||
		value.ExpiresAt.IsZero() || !value.ExpiresAt.After(policy.GeneratedAt) || value.ExpiresAt.After(policy.ExpiresAt) {
		return false
	}
	checks := []struct{ value, bound float64 }{
		{value.SuccessProbabilityDelta, policy.MaxSuccessProbabilityDelta},
		{value.LatencyRelativeDelta, policy.MaxLatencyRelativeDelta},
		{value.NormalizedCostRelDelta, policy.MaxNormalizedCostRelDelta},
		{value.CapacityScoreDelta, policy.MaxCapacityScoreDelta},
		{value.CacheHitProbabilityDelta, policy.MaxCacheHitProbabilityDelta},
	}
	for _, check := range checks {
		if math.IsNaN(check.value) || math.IsInf(check.value, 0) || math.Abs(check.value) > check.bound {
			return false
		}
	}
	return true
}

// APIKeyRoutingCohortBucket is stable, bounded, and based only on the numeric
// user identity. It is not a user-controlled or metrics-cardinality dimension.
func APIKeyRoutingCohortBucket(userID int64) int {
	if userID <= 0 {
		return -1
	}
	return StableRoutingExperimentBucket(userID, 0, []byte("routing-cohort-v1")) % routingCohortBuckets
}

// ApplyAPIKeyRoutingPersonalization layers cohort and Key residuals on a cloned
// request snapshot. Key entries are looked up by exact numeric identity, so an
// anomalous Key can never mutate another Key or the shared group observations.
func ApplyAPIKeyRoutingPersonalization(snapshot *APIKeyRoutingScoreSnapshot, policy *APIKeyRoutingPersonalization, apiKeyID, userID int64, eligible map[int64]bool, now time.Time) (*APIKeyRoutingScoreSnapshot, APIKeyRoutingPersonalizationResult) {
	result := APIKeyRoutingPersonalizationResult{AppliedGroups: make(map[int64]float64)}
	if snapshot == nil || policy == nil {
		result.Reason = RoutingLearningFallbackMissing
		return snapshot, result
	}
	result.Version = policy.Version
	result.Calibration = policy.DataQuality.CalibrationError
	if snapshot.FeatureVersion != policy.FeatureSchemaVersion {
		result.Reason = RoutingLearningFallbackSchema
		return snapshot, result
	}
	if now.Before(policy.GeneratedAt) || !now.Before(policy.ExpiresAt) {
		result.Reason = RoutingLearningFallbackStale
		return snapshot, result
	}
	if !policy.DataQuality.Healthy {
		result.Reason = RoutingLearningFallbackDataQuality
		return snapshot, result
	}
	if policy.DataQuality.Drifted {
		result.Reason = RoutingLearningFallbackDrift
		return snapshot, result
	}
	if policy.DataQuality.CalibrationError > policy.DataQuality.MaxCalibrationError {
		result.Reason = RoutingLearningFallbackCalibration
		return snapshot, result
	}
	eligibleCount := 0
	for _, admitted := range eligible {
		if admitted {
			eligibleCount++
		}
	}
	if eligibleCount == 0 || eligibleCount > maxRoutingPredictionCandidates {
		result.Reason = RoutingLearningFallbackOutOfRange
		return snapshot, result
	}

	projected := cloneAPIKeyRoutingScoreSnapshot(snapshot)
	bucket := APIKeyRoutingCohortBucket(userID)
	for groupID, admitted := range eligible {
		if !admitted {
			continue
		}
		observation, exists := projected.Groups[groupID]
		if !exists {
			continue
		}
		appliedWeight := 0.0
		combined := APIKeyRoutingResidual{}
		if bucket >= 0 {
			if residual, ok := policy.cohortResiduals[routingCohortResidualKey{Bucket: bucket, GroupID: groupID}]; ok {
				weight := routingResidualWeight(residual, policy, now)
				if weight > 0 {
					accumulateRoutingResidual(&combined, residual, weight)
					appliedWeight += weight
				}
			}
		}
		if apiKeyID > 0 {
			if residual, ok := policy.keyResiduals[routingKeyResidualKey{APIKeyID: apiKeyID, GroupID: groupID}]; ok {
				weight := routingResidualWeight(residual, policy, now)
				if weight > 0 {
					accumulateRoutingResidual(&combined, residual, weight)
					appliedWeight += weight
				}
			}
		}
		if appliedWeight > 0 {
			combined.SuccessProbabilityDelta = clampRoutingResidual(combined.SuccessProbabilityDelta, policy.MaxSuccessProbabilityDelta)
			combined.LatencyRelativeDelta = clampRoutingResidual(combined.LatencyRelativeDelta, policy.MaxLatencyRelativeDelta)
			combined.NormalizedCostRelDelta = clampRoutingResidual(combined.NormalizedCostRelDelta, policy.MaxNormalizedCostRelDelta)
			combined.CapacityScoreDelta = clampRoutingResidual(combined.CapacityScoreDelta, policy.MaxCapacityScoreDelta)
			combined.CacheHitProbabilityDelta = clampRoutingResidual(combined.CacheHitProbabilityDelta, policy.MaxCacheHitProbabilityDelta)
			observation = applyRoutingResidual(observation, combined, 1)
			projected.Groups[groupID] = observation
			result.AppliedGroups[groupID] = math.Min(1, appliedWeight)
		}
	}
	if len(result.AppliedGroups) == 0 {
		result.Reason = RoutingLearningFallbackLowSamples
		return snapshot, result
	}
	return projected, result
}

func accumulateRoutingResidual(target *APIKeyRoutingResidual, residual APIKeyRoutingResidual, weight float64) {
	if target == nil {
		return
	}
	target.SuccessProbabilityDelta += residual.SuccessProbabilityDelta * weight
	target.LatencyRelativeDelta += residual.LatencyRelativeDelta * weight
	target.NormalizedCostRelDelta += residual.NormalizedCostRelDelta * weight
	target.CapacityScoreDelta += residual.CapacityScoreDelta * weight
	target.CacheHitProbabilityDelta += residual.CacheHitProbabilityDelta * weight
}

func clampRoutingResidual(value, bound float64) float64 {
	return math.Max(-bound, math.Min(bound, value))
}

func routingResidualWeight(residual APIKeyRoutingResidual, policy *APIKeyRoutingPersonalization, now time.Time) float64 {
	if policy == nil || residual.Samples < policy.MinimumSamples || residual.Confidence < policy.MinimumConfidence || !now.Before(residual.ExpiresAt) {
		return 0
	}
	return routeClamp01(residual.Confidence * float64(residual.Samples) / (float64(residual.Samples) + policy.ShrinkageStrength))
}

func applyRoutingResidual(observation APIKeyRoutingGroupObservation, residual APIKeyRoutingResidual, weight float64) APIKeyRoutingGroupObservation {
	observation.SmoothedSuccessRate = routeClamp01(observation.SmoothedSuccessRate + residual.SuccessProbabilityDelta*weight)
	observation.TTFTP50Ms = nonNegativeFiniteOr(observation.TTFTP50Ms*(1+residual.LatencyRelativeDelta*weight), observation.TTFTP50Ms)
	observation.DurationP50Ms = nonNegativeFiniteOr(observation.DurationP50Ms*(1+residual.LatencyRelativeDelta*weight), observation.DurationP50Ms)
	observation.NormalizedRate = nonNegativeFiniteOr(observation.NormalizedRate*(1+residual.NormalizedCostRelDelta*weight), observation.NormalizedRate)
	observation.CapacityScore = routeClamp01(observation.CapacityScore + residual.CapacityScoreDelta*weight)
	observation.CacheHitRate = routeClamp01(observation.CacheHitRate + residual.CacheHitProbabilityDelta*weight)
	return observation
}
