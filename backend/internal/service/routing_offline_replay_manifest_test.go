package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildRoutingReplayDatasetManifestPinsQueryWindowSchemaSamplingAndChecksum(t *testing.T) {
	preference := APIKeySmartPreferencePrice
	scope := RoutingArtifactScope{
		ArtifactKind: RoutingArtifactStrategy, Platform: PlatformOpenAI,
		ModelFamily: "gpt-5", EndpointKind: "responses", Preference: &preference,
	}
	since := time.Unix(1_800_000_000, 0).UTC()
	until := since.Add(24 * time.Hour)
	decisions := []RoutingReplayDecision{{
		RoutingDecisionID: "decision-1", RouteVersion: 2, SelectedGroupID: 11,
		FeatureSchemaVersion: "routing-features-v2", SampleProbability: .01,
		OccurredAt: since.Add(time.Hour), Candidates: []APIKeyRoutingDecisionCandidate{{
			GroupID: 11, ConfiguredPriority: 0, Admitted: true, OutcomeVisibility: RoutingOutcomeObserved,
		}},
	}}

	first, err := BuildRoutingReplayDatasetManifest(scope, "strategy-v1", decisions, since, until, until)
	require.NoError(t, err)
	second, err := BuildRoutingReplayDatasetManifest(scope, "strategy-v1", decisions, since, until, until)
	require.NoError(t, err)
	require.Equal(t, first.Checksum, second.Checksum)
	require.Equal(t, RoutingReplayDatasetQueryVersion, first.QueryVersion)
	require.Equal(t, since, first.Since)
	require.Equal(t, until, first.Until)
	require.Equal(t, []string{"routing-features-v2"}, first.FeatureSchemaVersions)
	require.EqualValues(t, 1, first.RowCount)
	require.Contains(t, first.SamplingRule, "sample_probability")
	require.NotEmpty(t, first.ExclusionRules)

	decisions[0].SelectedGroupID = 12
	changed, err := BuildRoutingReplayDatasetManifest(scope, "strategy-v1", decisions, since, until, until)
	require.NoError(t, err)
	require.NotEqual(t, first.Checksum, changed.Checksum)
}
