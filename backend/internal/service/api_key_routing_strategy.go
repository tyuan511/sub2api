package service

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const BuiltinAPIKeyRoutingStrategyVersion = "routing-rules-v1"

const RoutingStrategySchemaVersion = "routing-strategy-v1"

// APIKeyRoutingStrategyPolicy is the immutable, request-executable subset of
// a strategy artifact. It contains only bounded numeric controls; candidate
// membership, breaker admission, sticky state, billing, and retry safety stay
// outside the policy and therefore cannot be weakened by an optimized version.
type APIKeyRoutingStrategyPolicy struct {
	Version               string
	Preference            string
	Weights               APIKeyRoutingScoreWeights
	SuccessRateHardGate   float64
	MinimumSamples        int64
	MaxSnapshotAgeSeconds int
	Stability             APIKeyRoutingStabilityPolicy
}

type APIKeyRoutingStabilityPolicy struct {
	MinimumScoreDifference  float64 `json:"minimum_score_difference"`
	MinimumResidenceSeconds int     `json:"minimum_residence_seconds"`
	MaxTrafficChangeBPS     int     `json:"max_traffic_change_bps"`
}

type apiKeyRoutingStrategyPayload struct {
	Weights               APIKeyRoutingScoreWeights    `json:"weights"`
	SuccessRateHardGate   float64                      `json:"success_rate_hard_gate"`
	MinimumSamples        int64                        `json:"minimum_samples"`
	MaxSnapshotAgeSeconds int                          `json:"max_snapshot_age_seconds"`
	Stability             APIKeyRoutingStabilityPolicy `json:"stability"`
}

func DefaultAPIKeyRoutingStrategyPolicy(preference string) APIKeyRoutingStrategyPolicy {
	if !oneOf(preference, APIKeySmartPreferencePrice, APIKeySmartPreferenceSpeed, APIKeySmartPreferenceBalanced) {
		preference = APIKeySmartPreferenceBalanced
	}
	return APIKeyRoutingStrategyPolicy{
		Version: BuiltinAPIKeyRoutingStrategyVersion, Preference: preference, Weights: APIKeyRoutingWeights(preference),
		SuccessRateHardGate: 0.5, MinimumSamples: 10, MaxSnapshotAgeSeconds: 180,
		Stability: APIKeyRoutingStabilityPolicy{
			MinimumScoreDifference: 0.01, MinimumResidenceSeconds: 300, MaxTrafficChangeBPS: 1000,
		},
	}
}

// ParseAPIKeyRoutingStrategyArtifact validates an immutable strategy before it
// can enter a gateway's local runtime. Unknown fields are ignored for rolling
// forward compatibility; every field this binary executes remains required
// and range-checked below, so a producer cannot weaken hard constraints.
func ParseAPIKeyRoutingStrategyArtifact(artifact *RoutingArtifactVersion) (APIKeyRoutingStrategyPolicy, error) {
	if artifact == nil || artifact.ArtifactKind != RoutingArtifactStrategy || artifact.Preference == nil {
		return APIKeyRoutingStrategyPolicy{}, fmt.Errorf("%w: strategy preference is required", ErrRoutingArtifactInvalid)
	}
	if artifact.SchemaVersion != RoutingStrategySchemaVersion && artifact.SchemaVersion != "strategy-schema-v1" {
		return APIKeyRoutingStrategyPolicy{}, fmt.Errorf("%w: incompatible strategy schema %q", ErrRoutingArtifactInvalid, artifact.SchemaVersion)
	}
	decoder := json.NewDecoder(strings.NewReader(string(artifact.Payload)))
	var payload apiKeyRoutingStrategyPayload
	if err := decoder.Decode(&payload); err != nil {
		return APIKeyRoutingStrategyPolicy{}, fmt.Errorf("%w: strategy payload: %v", ErrRoutingArtifactInvalid, err)
	}
	policy := APIKeyRoutingStrategyPolicy{
		Version: artifact.Version, Preference: *artifact.Preference, Weights: payload.Weights,
		SuccessRateHardGate: payload.SuccessRateHardGate, MinimumSamples: payload.MinimumSamples,
		MaxSnapshotAgeSeconds: payload.MaxSnapshotAgeSeconds, Stability: payload.Stability,
	}
	if err := ValidateAPIKeyRoutingStrategyPolicy(policy); err != nil {
		return APIKeyRoutingStrategyPolicy{}, err
	}
	return policy, nil
}

func ValidateAPIKeyRoutingStrategyPolicy(policy APIKeyRoutingStrategyPolicy) error {
	if strings.TrimSpace(policy.Version) == "" ||
		!oneOf(policy.Preference, APIKeySmartPreferencePrice, APIKeySmartPreferenceSpeed, APIKeySmartPreferenceBalanced) {
		return fmt.Errorf("%w: invalid strategy identity", ErrRoutingArtifactInvalid)
	}
	weights := policy.Weights
	values := []float64{weights.Success, weights.Price, weights.Speed, weights.Capacity}
	for _, value := range values {
		if value < 0 || value > 1 || math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("%w: invalid strategy weight", ErrRoutingArtifactInvalid)
		}
	}
	if math.Abs(weights.Success+weights.Price+weights.Speed+weights.Capacity-1) > 1e-9 || weights.Success < 0.5 {
		return fmt.Errorf("%w: weights must sum to one and success must remain primary", ErrRoutingArtifactInvalid)
	}
	if policy.Preference == APIKeySmartPreferencePrice && weights.Price < weights.Speed {
		return fmt.Errorf("%w: price preference envelope violated", ErrRoutingArtifactInvalid)
	}
	if policy.Preference == APIKeySmartPreferenceSpeed && weights.Speed < weights.Price {
		return fmt.Errorf("%w: speed preference envelope violated", ErrRoutingArtifactInvalid)
	}
	if policy.Preference == APIKeySmartPreferenceBalanced && math.Abs(weights.Price-weights.Speed) > 0.15 {
		return fmt.Errorf("%w: balanced preference envelope violated", ErrRoutingArtifactInvalid)
	}
	if policy.SuccessRateHardGate < 0.5 || policy.SuccessRateHardGate > 0.95 ||
		policy.MinimumSamples < 1 || policy.MinimumSamples > 10000 ||
		policy.MaxSnapshotAgeSeconds < 30 || policy.MaxSnapshotAgeSeconds > 3600 {
		return fmt.Errorf("%w: invalid reliability or window controls", ErrRoutingArtifactInvalid)
	}
	stability := policy.Stability
	if stability.MinimumScoreDifference < 0 || stability.MinimumScoreDifference > 0.25 ||
		stability.MinimumResidenceSeconds < 0 || stability.MinimumResidenceSeconds > 86400 ||
		stability.MaxTrafficChangeBPS < 1 || stability.MaxTrafficChangeBPS > 10000 {
		return fmt.Errorf("%w: invalid stability controls", ErrRoutingArtifactInvalid)
	}
	return nil
}
