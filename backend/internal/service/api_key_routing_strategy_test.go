package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAPIKeyRoutingStrategyArtifactEnforcesPreferenceEnvelope(t *testing.T) {
	preference := APIKeySmartPreferencePrice
	payload := json.RawMessage(`{"weights":{"success":0.5,"price":0.35,"speed":0.05,"capacity":0.1},"success_rate_hard_gate":0.5,"minimum_samples":20,"max_snapshot_age_seconds":120,"stability":{"minimum_score_difference":0.02,"minimum_residence_seconds":600,"max_traffic_change_bps":500}}`)
	sum := sha256.Sum256(payload)
	artifact := &RoutingArtifactVersion{
		ArtifactKind: RoutingArtifactStrategy, Version: "price-v2", Platform: PlatformOpenAI,
		ModelFamily: "gpt-5", EndpointKind: "responses", Preference: &preference, Status: RoutingLifecycleDraft,
		SchemaVersion: "routing-strategy-v1", Checksum: hex.EncodeToString(sum[:]), Payload: payload,
		Dependencies: json.RawMessage(`[]`), Lineage: json.RawMessage(`{"parent":"price-v1"}`),
	}

	policy, err := ParseAPIKeyRoutingStrategyArtifact(artifact)
	require.NoError(t, err)
	require.Equal(t, "price-v2", policy.Version)
	require.EqualValues(t, 20, policy.MinimumSamples)
	require.Equal(t, 0.35, policy.Weights.Price)

	artifact.Payload = json.RawMessage(`{"weights":{"success":0.4,"price":0.5,"speed":0.05,"capacity":0.05},"success_rate_hard_gate":0.49,"minimum_samples":20,"max_snapshot_age_seconds":120,"stability":{"minimum_score_difference":0.02,"minimum_residence_seconds":600,"max_traffic_change_bps":500}}`)
	_, err = ParseAPIKeyRoutingStrategyArtifact(artifact)
	require.ErrorIs(t, err, ErrRoutingArtifactInvalid)
}

func TestParseAPIKeyRoutingStrategyArtifactIgnoresUnknownFieldsButRejectsMissingCriticalFields(t *testing.T) {
	preference := APIKeySmartPreferencePrice
	artifact := &RoutingArtifactVersion{
		ArtifactKind: RoutingArtifactStrategy, Version: "price-forward-compatible", Preference: &preference,
		SchemaVersion: RoutingStrategySchemaVersion,
		Payload:       json.RawMessage(`{"weights":{"success":0.5,"price":0.35,"speed":0.05,"capacity":0.1},"success_rate_hard_gate":0.5,"minimum_samples":20,"max_snapshot_age_seconds":120,"stability":{"minimum_score_difference":0.02,"minimum_residence_seconds":600,"max_traffic_change_bps":500,"future_stability_field":true},"future_top_level_field":{"enabled":true}}`),
	}

	policy, err := ParseAPIKeyRoutingStrategyArtifact(artifact)
	require.NoError(t, err)
	require.Equal(t, "price-forward-compatible", policy.Version)

	artifact.Payload = json.RawMessage(`{"weights":{"success":0.5,"price":0.35,"speed":0.05,"capacity":0.1},"success_rate_hard_gate":0.5,"max_snapshot_age_seconds":120,"stability":{"minimum_score_difference":0.02,"minimum_residence_seconds":600,"max_traffic_change_bps":500}}`)
	_, err = ParseAPIKeyRoutingStrategyArtifact(artifact)
	require.ErrorIs(t, err, ErrRoutingArtifactInvalid)

	artifact.SchemaVersion = "routing-strategy-v99"
	_, err = ParseAPIKeyRoutingStrategyArtifact(artifact)
	require.ErrorIs(t, err, ErrRoutingArtifactInvalid)
}

func TestRankAPIKeyRoutingCandidatesWithPolicyNeverScoresBelowHardGate(t *testing.T) {
	groups := []APIKeyRouteCandidate{
		{GroupID: 1, Priority: 0, Group: &Group{ID: 1, RateMultiplier: 10}},
		{GroupID: 2, Priority: 1, Group: &Group{ID: 2, RateMultiplier: 1}},
	}
	snapshot := &APIKeyRoutingScoreSnapshot{Groups: map[int64]APIKeyRoutingGroupObservation{
		1: {GroupID: 1, SuccessRequests: 90, FailedRequests: 10, NormalizedRate: 10, Confidence: 1},
		2: {GroupID: 2, SuccessRequests: 49, FailedRequests: 51, NormalizedRate: 1, Confidence: 1},
	}}
	policy := DefaultAPIKeyRoutingStrategyPolicy(APIKeySmartPreferencePrice)
	policy.Weights = APIKeyRoutingScoreWeights{Success: 0.5, Price: 0.45, Speed: 0, Capacity: 0.05}

	ranked := RankAPIKeyRoutingCandidatesWithPolicy(groups, snapshot, policy)
	require.Equal(t, int64(1), ranked[0].GroupID)
	require.True(t, ranked[0].Eligible)
	require.Equal(t, int64(2), ranked[1].GroupID)
	require.False(t, ranked[1].Eligible, "a cheap group below 50% must remain excluded")
}
