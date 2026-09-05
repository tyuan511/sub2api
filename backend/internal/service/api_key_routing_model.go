package service

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	RoutingPredictionModelSchemaVersion = "routing-prediction-model-v1"
	RoutingPredictionModelType          = "bounded-linear-components-v1"
	maxRoutingPredictionPayload         = 256 * 1024
	maxRoutingPredictionFeatures        = 8
	maxRoutingPredictionCandidates      = 8
	maxRoutingPredictionTTL             = 7 * 24 * time.Hour
)

const (
	routingModelFeatureSuccess         = "baseline_success_probability"
	routingModelFeatureTTFT            = "log1p_ttft_ms"
	routingModelFeatureDuration        = "log1p_duration_ms"
	routingModelFeatureCapacity        = "capacity_score"
	routingModelFeatureCacheHit        = "cache_hit_rate"
	routingModelFeatureNormalizedCost  = "normalized_rate"
	routingModelFeatureConfidence      = "confidence"
	routingModelFeaturePriceConfidence = "price_confidence"
)

const (
	routingModelOutputSuccess        = "success_probability_delta"
	routingModelOutputTTFT           = "ttft_relative_delta"
	routingModelOutputDuration       = "duration_relative_delta"
	routingModelOutputOverflow       = "capacity_overflow_probability_delta"
	routingModelOutputCacheHit       = "cache_hit_probability_delta"
	routingModelOutputNormalizedCost = "normalized_cost_relative_delta"
)

var approvedRoutingPredictionFeatures = map[string]struct{}{
	routingModelFeatureSuccess: {}, routingModelFeatureTTFT: {}, routingModelFeatureDuration: {},
	routingModelFeatureCapacity: {}, routingModelFeatureCacheHit: {}, routingModelFeatureNormalizedCost: {},
	routingModelFeatureConfidence: {}, routingModelFeaturePriceConfidence: {},
}

var requiredRoutingPredictionOutputs = [...]string{
	routingModelOutputSuccess, routingModelOutputTTFT, routingModelOutputDuration,
	routingModelOutputOverflow, routingModelOutputCacheHit, routingModelOutputNormalizedCost,
}

type APIKeyRoutingLinearComponent struct {
	Intercept    float64            `json:"intercept"`
	Coefficients map[string]float64 `json:"coefficients"`
}

type APIKeyRoutingPredictionBounds struct {
	SuccessProbabilityDelta          float64 `json:"success_probability_delta"`
	TTFTRelativeDelta                float64 `json:"ttft_relative_delta"`
	DurationRelativeDelta            float64 `json:"duration_relative_delta"`
	CapacityOverflowProbabilityDelta float64 `json:"capacity_overflow_probability_delta"`
	CacheHitProbabilityDelta         float64 `json:"cache_hit_probability_delta"`
	NormalizedCostRelativeDelta      float64 `json:"normalized_cost_relative_delta"`
}

type APIKeyRoutingPredictionModel struct {
	Version              string
	FeatureSchemaVersion string
	GeneratedAt          time.Time
	ExpiresAt            time.Time
	MaxInferenceMicros   int64
	MinimumConfidence    float64
	DataQuality          APIKeyRoutingResidualDataQuality
	RequiredFeatures     []string
	Outputs              map[string]APIKeyRoutingLinearComponent
	Bounds               APIKeyRoutingPredictionBounds
}

type apiKeyRoutingPredictionModelPayload struct {
	ModelType            string                                  `json:"model_type"`
	FeatureSchemaVersion string                                  `json:"feature_schema_version"`
	GeneratedAt          time.Time                               `json:"generated_at"`
	ExpiresAt            time.Time                               `json:"expires_at"`
	MaxInferenceMicros   int64                                   `json:"max_inference_micros"`
	MinimumConfidence    float64                                 `json:"minimum_confidence"`
	DataQuality          APIKeyRoutingResidualDataQuality        `json:"data_quality"`
	RequiredFeatures     []string                                `json:"required_features"`
	Outputs              map[string]APIKeyRoutingLinearComponent `json:"outputs"`
	Bounds               APIKeyRoutingPredictionBounds           `json:"bounds"`
}

type APIKeyRoutingPredictionResult struct {
	Version       string
	Reason        string
	Duration      time.Duration
	Calibration   float64
	AppliedGroups map[int64]struct{}
}

// ParseAPIKeyRoutingPredictionModel validates the executable model, its data
// lineage, approved feature contract, and output envelope before it can reach a
// gateway-local catalog.
func ParseAPIKeyRoutingPredictionModel(artifact *RoutingArtifactVersion) (*APIKeyRoutingPredictionModel, error) {
	if artifact == nil || artifact.ArtifactKind != RoutingArtifactModel {
		return nil, fmt.Errorf("%w: prediction model artifact required", ErrRoutingArtifactInvalid)
	}
	if artifact.SchemaVersion != RoutingPredictionModelSchemaVersion {
		return nil, fmt.Errorf("%w: incompatible prediction model schema %q", ErrRoutingArtifactInvalid, artifact.SchemaVersion)
	}
	if len(artifact.Payload) == 0 || len(artifact.Payload) > maxRoutingPredictionPayload {
		return nil, fmt.Errorf("%w: prediction payload exceeds local memory envelope", ErrRoutingArtifactInvalid)
	}
	lineage, lineageOK := parseRoutingLearningLineage(artifact.Lineage)
	dependencies, dependenciesOK := parseRoutingLearningDependencies(artifact.Dependencies)
	if !lineageOK || !dependenciesOK {
		return nil, fmt.Errorf("%w: model lineage or dependency contract is incomplete", ErrRoutingArtifactInvalid)
	}
	decoder := json.NewDecoder(strings.NewReader(string(artifact.Payload)))
	decoder.DisallowUnknownFields()
	var payload apiKeyRoutingPredictionModelPayload
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: prediction payload: %v", ErrRoutingArtifactInvalid, err)
	}
	if payload.ModelType != RoutingPredictionModelType || payload.FeatureSchemaVersion == "" ||
		payload.GeneratedAt.IsZero() || payload.ExpiresAt.IsZero() || !payload.ExpiresAt.After(payload.GeneratedAt) ||
		payload.ExpiresAt.Sub(payload.GeneratedAt) > maxRoutingPredictionTTL ||
		payload.MaxInferenceMicros < 10 || payload.MaxInferenceMicros > 5000 ||
		payload.MinimumConfidence < 0 || payload.MinimumConfidence > 1 ||
		!validRoutingResidualDataQuality(payload.DataQuality) ||
		len(payload.RequiredFeatures) == 0 || len(payload.RequiredFeatures) > maxRoutingPredictionFeatures {
		return nil, fmt.Errorf("%w: invalid prediction model envelope", ErrRoutingArtifactInvalid)
	}
	if lineage.TrainingThrough.After(payload.GeneratedAt) || !hasRoutingLearningDependency(dependencies, RoutingArtifactFeature, payload.FeatureSchemaVersion) {
		return nil, fmt.Errorf("%w: model lineage is not point-in-time compatible with its feature schema", ErrRoutingArtifactInvalid)
	}
	featureSet := make(map[string]struct{}, len(payload.RequiredFeatures))
	for _, feature := range payload.RequiredFeatures {
		if _, approved := approvedRoutingPredictionFeatures[feature]; !approved {
			return nil, fmt.Errorf("%w: unapproved prediction feature %q", ErrRoutingArtifactInvalid, feature)
		}
		if _, duplicate := featureSet[feature]; duplicate {
			return nil, fmt.Errorf("%w: duplicate prediction feature %q", ErrRoutingArtifactInvalid, feature)
		}
		featureSet[feature] = struct{}{}
	}
	if len(payload.Outputs) != len(requiredRoutingPredictionOutputs) || !validRoutingPredictionBounds(payload.Bounds) {
		return nil, fmt.Errorf("%w: prediction outputs or bounds are incomplete", ErrRoutingArtifactInvalid)
	}
	for _, output := range requiredRoutingPredictionOutputs {
		component, exists := payload.Outputs[output]
		if !exists || !finiteWithin(component.Intercept, 10) || len(component.Coefficients) > maxRoutingPredictionFeatures {
			return nil, fmt.Errorf("%w: invalid prediction output %q", ErrRoutingArtifactInvalid, output)
		}
		for feature, coefficient := range component.Coefficients {
			if _, required := featureSet[feature]; !required || !finiteWithin(coefficient, 10) {
				return nil, fmt.Errorf("%w: invalid coefficient for %q", ErrRoutingArtifactInvalid, feature)
			}
		}
	}
	return &APIKeyRoutingPredictionModel{
		Version: artifact.Version, FeatureSchemaVersion: payload.FeatureSchemaVersion,
		GeneratedAt: payload.GeneratedAt.UTC(), ExpiresAt: payload.ExpiresAt.UTC(),
		MaxInferenceMicros: payload.MaxInferenceMicros, MinimumConfidence: payload.MinimumConfidence,
		DataQuality: payload.DataQuality, RequiredFeatures: append([]string(nil), payload.RequiredFeatures...),
		Outputs: cloneRoutingPredictionOutputs(payload.Outputs), Bounds: payload.Bounds,
	}, nil
}

type routingLearningLineage struct {
	DatasetChecksum string    `json:"dataset_checksum"`
	QueryVersion    string    `json:"query_version"`
	TrainingThrough time.Time `json:"training_through"`
}

type routingLearningDependency struct {
	ArtifactKind string `json:"artifact_kind"`
	Version      string `json:"version"`
}

func parseRoutingLearningLineage(raw json.RawMessage) (routingLearningLineage, bool) {
	var lineage routingLearningLineage
	if json.Unmarshal(raw, &lineage) != nil {
		return routingLearningLineage{}, false
	}
	checksum, err := hex.DecodeString(strings.TrimSpace(lineage.DatasetChecksum))
	ok := err == nil && len(checksum) == 32 && strings.TrimSpace(lineage.QueryVersion) != "" && !lineage.TrainingThrough.IsZero()
	return lineage, ok
}

func parseRoutingLearningDependencies(raw json.RawMessage) ([]routingLearningDependency, bool) {
	var dependencies []routingLearningDependency
	if json.Unmarshal(raw, &dependencies) != nil || len(dependencies) == 0 || len(dependencies) > 8 {
		return nil, false
	}
	for _, dependency := range dependencies {
		if !oneOf(dependency.ArtifactKind, RoutingArtifactFeature, RoutingArtifactScore, RoutingArtifactStrategy) || strings.TrimSpace(dependency.Version) == "" {
			return nil, false
		}
	}
	return dependencies, true
}

func hasRoutingLearningDependency(dependencies []routingLearningDependency, kind, version string) bool {
	for _, dependency := range dependencies {
		if dependency.ArtifactKind == kind && dependency.Version == version {
			return true
		}
	}
	return false
}

func validRoutingPredictionBounds(value APIKeyRoutingPredictionBounds) bool {
	checks := []struct{ value, maximum float64 }{
		{value.SuccessProbabilityDelta, .20}, {value.TTFTRelativeDelta, .50},
		{value.DurationRelativeDelta, .50}, {value.CapacityOverflowProbabilityDelta, .20},
		{value.CacheHitProbabilityDelta, .20}, {value.NormalizedCostRelativeDelta, .50},
	}
	for _, check := range checks {
		if !validRoutingResidualBound(check.value, check.maximum) {
			return false
		}
	}
	return true
}

func finiteWithin(value, bound float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && math.Abs(value) <= bound
}

func cloneRoutingPredictionOutputs(input map[string]APIKeyRoutingLinearComponent) map[string]APIKeyRoutingLinearComponent {
	output := make(map[string]APIKeyRoutingLinearComponent, len(input))
	for name, component := range input {
		copy := component
		copy.Coefficients = make(map[string]float64, len(component.Coefficients))
		for feature, coefficient := range component.Coefficients {
			copy.Coefficients[feature] = coefficient
		}
		output[name] = copy
	}
	return output
}

// ApplyAPIKeyRoutingPredictionModel runs only on candidates already admitted by
// rule hard filters. It is pure CPU/local memory and applies all outputs
// atomically; one missing/non-finite/out-of-range value returns the untouched
// deterministic snapshot.
func ApplyAPIKeyRoutingPredictionModel(snapshot *APIKeyRoutingScoreSnapshot, model *APIKeyRoutingPredictionModel, eligible map[int64]bool, now time.Time) (*APIKeyRoutingScoreSnapshot, APIKeyRoutingPredictionResult) {
	return applyAPIKeyRoutingPredictionModelWithClock(snapshot, model, eligible, now, time.Now)
}

func applyAPIKeyRoutingPredictionModelWithClock(snapshot *APIKeyRoutingScoreSnapshot, model *APIKeyRoutingPredictionModel, eligible map[int64]bool, now time.Time, clock func() time.Time) (output *APIKeyRoutingScoreSnapshot, result APIKeyRoutingPredictionResult) {
	started := clock()
	result = APIKeyRoutingPredictionResult{AppliedGroups: make(map[int64]struct{})}
	defer func() { result.Duration = clock().Sub(started) }()
	if snapshot == nil || model == nil {
		result.Reason = RoutingLearningFallbackMissing
		return snapshot, result
	}
	result.Version = model.Version
	result.Calibration = model.DataQuality.CalibrationError
	if snapshot.FeatureVersion != model.FeatureSchemaVersion {
		result.Reason = RoutingLearningFallbackSchema
		return snapshot, result
	}
	if now.Before(model.GeneratedAt) || !now.Before(model.ExpiresAt) {
		result.Reason = RoutingLearningFallbackStale
		return snapshot, result
	}
	if !model.DataQuality.Healthy {
		result.Reason = RoutingLearningFallbackDataQuality
		return snapshot, result
	}
	if model.DataQuality.Drifted {
		result.Reason = RoutingLearningFallbackDrift
		return snapshot, result
	}
	if model.DataQuality.CalibrationError > model.DataQuality.MaxCalibrationError {
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

	budget := time.Duration(model.MaxInferenceMicros) * time.Microsecond
	predicted := cloneAPIKeyRoutingScoreSnapshot(snapshot)
	for groupID := range eligible {
		if !eligible[groupID] {
			continue
		}
		if clock().Sub(started) > budget {
			result.Reason = RoutingLearningFallbackTimeout
			return snapshot, result
		}
		observation, exists := snapshot.Groups[groupID]
		if !exists || observation.Confidence < model.MinimumConfidence {
			result.Reason = RoutingLearningFallbackFeatures
			return snapshot, result
		}
		features, ok := routingPredictionFeatures(observation, model.RequiredFeatures)
		if !ok {
			result.Reason = RoutingLearningFallbackFeatures
			return snapshot, result
		}
		outputs := make(map[string]float64, len(requiredRoutingPredictionOutputs))
		for _, output := range requiredRoutingPredictionOutputs {
			component := model.Outputs[output]
			value := component.Intercept
			for _, feature := range model.RequiredFeatures {
				value += component.Coefficients[feature] * features[feature]
			}
			if math.IsNaN(value) || math.IsInf(value, 0) {
				result.Reason = RoutingLearningFallbackNonFinite
				return snapshot, result
			}
			if math.Abs(value) > routingPredictionOutputBound(output, model.Bounds) {
				result.Reason = RoutingLearningFallbackOutOfRange
				return snapshot, result
			}
			outputs[output] = value
		}
		updated := observation
		updated.SmoothedSuccessRate = routeClamp01(observation.SmoothedSuccessRate + outputs[routingModelOutputSuccess])
		updated.TTFTP50Ms = nonNegativeFiniteOr(observation.TTFTP50Ms*(1+outputs[routingModelOutputTTFT]), observation.TTFTP50Ms)
		updated.DurationP50Ms = nonNegativeFiniteOr(observation.DurationP50Ms*(1+outputs[routingModelOutputDuration]), observation.DurationP50Ms)
		updated.CapacityScore = routeClamp01(observation.CapacityScore - outputs[routingModelOutputOverflow])
		updated.CacheHitRate = routeClamp01(observation.CacheHitRate + outputs[routingModelOutputCacheHit])
		updated.NormalizedRate = nonNegativeFiniteOr(observation.NormalizedRate*(1+outputs[routingModelOutputNormalizedCost]), observation.NormalizedRate)
		predicted.Groups[groupID] = updated
		result.AppliedGroups[groupID] = struct{}{}
	}
	if clock().Sub(started) > budget {
		result.Reason = RoutingLearningFallbackTimeout
		return snapshot, result
	}
	predicted.ModelVersion = optionalStringPtr(model.Version)
	return predicted, result
}

func routingPredictionFeatures(observation APIKeyRoutingGroupObservation, required []string) (map[string]float64, bool) {
	total := observation.SuccessRequests + observation.FailedRequests
	success := observation.SmoothedSuccessRate
	if success == 0 && total > 0 {
		success = float64(observation.SuccessRequests) / float64(total)
	}
	all := map[string]float64{
		routingModelFeatureSuccess:         success,
		routingModelFeatureTTFT:            math.Log1p(observation.TTFTP50Ms) / 10,
		routingModelFeatureDuration:        math.Log1p(observation.DurationP50Ms) / 12,
		routingModelFeatureCapacity:        observation.CapacityScore,
		routingModelFeatureCacheHit:        observation.CacheHitRate,
		routingModelFeatureNormalizedCost:  math.Log1p(observation.NormalizedRate) / 4,
		routingModelFeatureConfidence:      observation.Confidence,
		routingModelFeaturePriceConfidence: observation.PriceConfidence,
	}
	result := make(map[string]float64, len(required))
	for _, feature := range required {
		value, exists := all[feature]
		if !exists || math.IsNaN(value) || math.IsInf(value, 0) ||
			(feature == routingModelFeatureSuccess && total == 0) ||
			(feature == routingModelFeatureTTFT && observation.TTFTP50Ms <= 0) ||
			(feature == routingModelFeatureDuration && observation.DurationP50Ms <= 0) ||
			(feature == routingModelFeatureNormalizedCost && observation.NormalizedRate <= 0) {
			return nil, false
		}
		result[feature] = value
	}
	return result, true
}

func routingPredictionOutputBound(output string, bounds APIKeyRoutingPredictionBounds) float64 {
	switch output {
	case routingModelOutputSuccess:
		return bounds.SuccessProbabilityDelta
	case routingModelOutputTTFT:
		return bounds.TTFTRelativeDelta
	case routingModelOutputDuration:
		return bounds.DurationRelativeDelta
	case routingModelOutputOverflow:
		return bounds.CapacityOverflowProbabilityDelta
	case routingModelOutputCacheHit:
		return bounds.CacheHitProbabilityDelta
	case routingModelOutputNormalizedCost:
		return bounds.NormalizedCostRelativeDelta
	default:
		return -1
	}
}
