package service

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRoutingScoreSnapshotContractIgnoresUnknownFieldsButRejectsInvalidCriticalFields(t *testing.T) {
	now := time.Now().UTC()
	snapshot := routingScoreSnapshotForTest(now)
	body, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(body, &object); err != nil {
		t.Fatal(err)
	}
	object["future_snapshot_feature"] = map[string]any{"value": 1}
	body, err = json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	var decoded APIKeyRoutingScoreSnapshot
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAPIKeyRoutingScoreSnapshot(&decoded); err != nil {
		t.Fatalf("unknown field should be ignored: %v", err)
	}

	group := decoded.Groups[1]
	group.Confidence = 1.1
	decoded.Groups[1] = group
	if err := ValidateAPIKeyRoutingScoreSnapshot(&decoded); err == nil {
		t.Fatal("out-of-range critical feature should be rejected")
	}
}

func routingScoreSnapshotForTest(now time.Time) *APIKeyRoutingScoreSnapshot {
	return &APIKeyRoutingScoreSnapshot{
		Version: "score-v1", StrategyVersion: "strategy-v1", FeatureVersion: "features-v1",
		Platform: PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses", GeneratedAt: now,
		Groups: map[int64]APIKeyRoutingGroupObservation{1: {GroupID: 1, SuccessRequests: 10, NormalizedRate: 1}},
	}
}

func TestAtomicAPIKeyRoutingScoreStorePublishesImmutableCatalog(t *testing.T) {
	now := time.Now().UTC()
	snapshot := routingScoreSnapshotForTest(now)
	store := NewAtomicAPIKeyRoutingScoreStore()
	if err := store.Replace([]*APIKeyRoutingScoreSnapshot{snapshot}); err != nil {
		t.Fatal(err)
	}
	snapshot.Groups[1] = APIKeyRoutingGroupObservation{GroupID: 1, SuccessRequests: 999}
	got, ok := store.Lookup(APIKeyRoutingScoreScope{Platform: PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses"}, time.Minute, now)
	if !ok || got.Groups[1].SuccessRequests != 10 {
		t.Fatalf("published snapshot was mutated: %#v", got)
	}
	if _, ok := store.Lookup(APIKeyRoutingScoreScope{Platform: PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses"}, time.Second, now.Add(2*time.Second)); ok {
		t.Fatal("stale score snapshot should not be returned")
	}
}

func TestAtomicAPIKeyRoutingScoreStoreRejectsInvalidReplacementWithoutLosingCurrent(t *testing.T) {
	now := time.Now().UTC()
	store := NewAtomicAPIKeyRoutingScoreStore()
	if err := store.Replace([]*APIKeyRoutingScoreSnapshot{routingScoreSnapshotForTest(now)}); err != nil {
		t.Fatal(err)
	}
	if err := store.Replace([]*APIKeyRoutingScoreSnapshot{{Version: "broken"}}); err == nil {
		t.Fatal("invalid snapshot should be rejected")
	}
	if _, ok := store.Lookup(APIKeyRoutingScoreScope{Platform: PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses"}, 0, now); !ok {
		t.Fatal("failed replacement must preserve last known good catalog")
	}
}
