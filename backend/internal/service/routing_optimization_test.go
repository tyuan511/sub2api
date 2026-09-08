package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestValidateRoutingArtifactChecksumAndLifecycle(t *testing.T) {
	payload := json.RawMessage(`{"weights":{"success":0.5,"price":0.2,"speed":0.2,"capacity":0.1},"success_rate_hard_gate":0.5,"minimum_samples":10,"max_snapshot_age_seconds":180,"stability":{"minimum_score_difference":0.01,"minimum_residence_seconds":300,"max_traffic_change_bps":1000}}`)
	sum := sha256.Sum256(payload)
	preference := APIKeySmartPreferenceBalanced
	artifact := &RoutingArtifactVersion{
		ArtifactKind: RoutingArtifactStrategy, Version: "strategy-v1", Platform: PlatformOpenAI,
		ModelFamily: "gpt-5", EndpointKind: "responses", Preference: &preference, Status: RoutingLifecycleDraft,
		SchemaVersion: "strategy-schema-v1", Checksum: hex.EncodeToString(sum[:]), Payload: payload,
		Dependencies: json.RawMessage(`[]`), Lineage: json.RawMessage(`{"source":"baseline"}`),
	}
	if err := ValidateRoutingArtifact(artifact); err != nil {
		t.Fatal(err)
	}
	artifact.Payload = json.RawMessage(`{"weights":{}}`)
	if err := ValidateRoutingArtifact(artifact); err == nil {
		t.Fatal("mutated payload must fail checksum validation")
	}
	if err := ValidateRoutingLifecycleTransition(RoutingLifecycleDraft, RoutingLifecycleActive); err == nil {
		t.Fatal("draft must not skip shadow and canary")
	}
	if err := ValidateRoutingLifecycleTransition(RoutingLifecycleCanary, RoutingLifecycleActive); err != nil {
		t.Fatal(err)
	}
}

func TestStableRoutingExperimentBucket(t *testing.T) {
	first := StableRoutingExperimentBucket(11, 22, []byte("salt"))
	if second := StableRoutingExperimentBucket(11, 22, []byte("salt")); first != second {
		t.Fatalf("bucket changed: %d != %d", first, second)
	}
	if first < 0 || first >= 10000 {
		t.Fatalf("bucket out of range: %d", first)
	}
}
