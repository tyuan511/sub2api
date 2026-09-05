package service

import (
	"math"
	"testing"
	"time"
)

func TestAPIKeyRoutingWeightsKeepSuccessAsCommonBaseline(t *testing.T) {
	for _, preference := range []string{APIKeySmartPreferencePrice, APIKeySmartPreferenceSpeed, APIKeySmartPreferenceBalanced} {
		weights := APIKeyRoutingWeights(preference)
		if weights.Success != 0.50 {
			t.Fatalf("preference %s success weight = %v, want 0.50", preference, weights.Success)
		}
		if got := weights.Success + weights.Price + weights.Speed + weights.Capacity; math.Abs(got-1) > 1e-12 {
			t.Fatalf("preference %s weights sum = %v, want 1", preference, got)
		}
	}
}

func TestRankAPIKeyRoutingCandidatesPreferenceOnlyChangesWeights(t *testing.T) {
	candidates := []APIKeyRouteCandidate{
		{GroupID: 11, Priority: 0},
		{GroupID: 22, Priority: 1},
		{GroupID: 33, Priority: 2},
	}
	snapshot := &APIKeyRoutingScoreSnapshot{Groups: map[int64]APIKeyRoutingGroupObservation{
		11: {GroupID: 11, SuccessRequests: 95, FailedRequests: 5, NormalizedRate: 1, TTFTP50Ms: 1000, CapacityScore: 0.8, Confidence: 1},
		22: {GroupID: 22, SuccessRequests: 95, FailedRequests: 5, NormalizedRate: 2, TTFTP50Ms: 100, CapacityScore: 0.8, Confidence: 1},
		33: {GroupID: 33, SuccessRequests: 49, FailedRequests: 51, NormalizedRate: 0.1, TTFTP50Ms: 10, CapacityScore: 1, Confidence: 1},
	}}

	price := RankAPIKeyRoutingCandidates(candidates, snapshot, APIKeySmartPreferencePrice, 10)
	speed := RankAPIKeyRoutingCandidates(candidates, snapshot, APIKeySmartPreferenceSpeed, 10)
	if price[0].GroupID != 11 {
		t.Fatalf("price first group = %d, want cheap healthy group 11", price[0].GroupID)
	}
	if speed[0].GroupID != 22 {
		t.Fatalf("speed first group = %d, want fast healthy group 22", speed[0].GroupID)
	}
	for _, ranked := range [][]APIKeyRoutingCandidateScore{price, speed} {
		if ranked[len(ranked)-1].GroupID != 33 || ranked[len(ranked)-1].Eligible {
			t.Fatalf("below-50%% group must be hard-excluded: %+v", ranked)
		}
	}
}

func TestRankAPIKeyRoutingCandidatesFiftyPercentRemainsEligible(t *testing.T) {
	ranked := RankAPIKeyRoutingCandidates(
		[]APIKeyRouteCandidate{{GroupID: 2, Priority: 0}, {GroupID: 1, Priority: 0}},
		&APIKeyRoutingScoreSnapshot{Groups: map[int64]APIKeyRoutingGroupObservation{
			1: {SuccessRequests: 5, FailedRequests: 5, Confidence: 1},
			2: {SuccessRequests: 4, FailedRequests: 6, Confidence: 1},
		}},
		APIKeySmartPreferenceBalanced,
		10,
	)
	if ranked[0].GroupID != 1 || !ranked[0].Eligible {
		t.Fatalf("exactly 50%% candidate should remain eligible: %+v", ranked)
	}
	if ranked[1].GroupID != 2 || ranked[1].Eligible {
		t.Fatalf("below 50%% candidate should be excluded: %+v", ranked)
	}
}

func TestRankAPIKeyRoutingCandidatesShrinksLowConfidenceAndUsesStableTieBreak(t *testing.T) {
	ranked := RankAPIKeyRoutingCandidates(
		[]APIKeyRouteCandidate{{GroupID: 9, Priority: 0}, {GroupID: 7, Priority: 0}},
		&APIKeyRoutingScoreSnapshot{Groups: map[int64]APIKeyRoutingGroupObservation{
			7: {SuccessRequests: 1, NormalizedRate: 1, TTFTP50Ms: 100, CapacityScore: 1},
			9: {SuccessRequests: 1, NormalizedRate: 1, TTFTP50Ms: 100, CapacityScore: 1},
		}},
		APIKeySmartPreferenceBalanced,
		10,
	)
	if ranked[0].GroupID != 7 {
		t.Fatalf("equal score/priority should use group id: %+v", ranked)
	}
	if ranked[0].Confidence <= 0 || ranked[0].Confidence >= 1 {
		t.Fatalf("low sample confidence = %v, want a shrunken value", ranked[0].Confidence)
	}
}

func TestNormalizeAPIKeyRoutingRateUsesFullCacheBaseline(t *testing.T) {
	fullyCached := NormalizeAPIKeyRoutingRate(APIKeyRoutingPriceProjection{
		GroupRateMultiplier:  2,
		CacheReadInputTokens: 1000,
		OutputTokens:         100,
		InputUnitCost:        1,
		CacheReadUnitCost:    0.1,
		OutputUnitCost:       2,
	})
	if math.Abs(fullyCached-2) > 1e-12 {
		t.Fatalf("fully cached normalized rate = %v, want group multiplier 2", fullyCached)
	}
	uncached := NormalizeAPIKeyRoutingRate(APIKeyRoutingPriceProjection{
		GroupRateMultiplier: 2,
		UncachedInputTokens: 1000,
		OutputTokens:        100,
		InputUnitCost:       1,
		CacheReadUnitCost:   0.1,
		OutputUnitCost:      2,
	})
	if uncached <= fullyCached {
		t.Fatalf("uncached normalized rate = %v, want > fully cached %v", uncached, fullyCached)
	}
}

func TestNormalizeAPIKeyRoutingRatePreservesFreeMultiplier(t *testing.T) {
	got := NormalizeAPIKeyRoutingRate(APIKeyRoutingPriceProjection{
		GroupRateMultiplier: 0,
		UncachedInputTokens: 1000,
		OutputTokens:        100,
		InputUnitCost:       1,
		CacheReadUnitCost:   0.1,
		OutputUnitCost:      2,
	})
	if got != 0 {
		t.Fatalf("free group normalized rate = %v, want 0", got)
	}
}

func TestProjectAPIKeyRoutingScoreSnapshotAppliesOnlyCurrentUsersRates(t *testing.T) {
	candidates := []APIKeyRouteCandidate{
		{GroupID: 1, Priority: 0, Group: &Group{ID: 1, RateMultiplier: 2}},
		{GroupID: 2, Priority: 1, Group: &Group{ID: 2, RateMultiplier: 1}},
	}
	snapshot := &APIKeyRoutingScoreSnapshot{Groups: map[int64]APIKeyRoutingGroupObservation{
		1: {GroupID: 1, SuccessRequests: 100, SmoothedSuccessRate: 1, Confidence: 1, CapacityScore: 1, TTFTP50Ms: 100, NormalizedRate: 3, PriceNormalizationFactor: 1.5},
		2: {GroupID: 2, SuccessRequests: 100, SmoothedSuccessRate: 1, Confidence: 1, CapacityScore: 1, TTFTP50Ms: 100, NormalizedRate: 1.5, PriceNormalizationFactor: 1.5},
	}}

	defaultRank := RankAPIKeyRoutingCandidates(candidates, snapshot, APIKeySmartPreferencePrice, 10)
	if defaultRank[0].GroupID != 2 {
		t.Fatalf("default ranking first = %d, want group 2", defaultRank[0].GroupID)
	}
	projected := ProjectAPIKeyRoutingScoreSnapshot(candidates, snapshot, map[int64]float64{1: 0.5})
	userRank := RankAPIKeyRoutingCandidates(candidates, projected, APIKeySmartPreferencePrice, 10)
	if userRank[0].GroupID != 1 {
		t.Fatalf("user-projected ranking first = %d, want group 1: %+v", userRank[0].GroupID, userRank)
	}
	if math.Abs(projected.Groups[1].NormalizedRate-0.75) > 1e-12 {
		t.Fatalf("projected rate = %v, want 0.75", projected.Groups[1].NormalizedRate)
	}
	if snapshot.Groups[1].NormalizedRate != 3 {
		t.Fatalf("shared snapshot was mutated: %+v", snapshot.Groups[1])
	}
}

func TestProjectAPIKeyRoutingScoreSnapshotAppliesPeakMultiplierAtFixedInstant(t *testing.T) {
	group := &Group{
		ID: 1, RateMultiplier: 2, SubscriptionType: SubscriptionTypeSubscription,
		PeakRateEnabled: true, PeakStart: "00:00", PeakEnd: "23:59", PeakRateMultiplier: 3,
	}
	snapshot := &APIKeyRoutingScoreSnapshot{Groups: map[int64]APIKeyRoutingGroupObservation{
		1: {GroupID: 1, NormalizedRate: 3, PriceNormalizationFactor: 1.5},
	}}
	projected := ProjectAPIKeyRoutingScoreSnapshotAt(
		[]APIKeyRouteCandidate{{GroupID: 1, Group: group}}, snapshot, map[int64]float64{1: 0.5},
		time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
	)
	if math.Abs(projected.Groups[1].NormalizedRate-2.25) > 1e-12 {
		t.Fatalf("projected peak rate = %v, want user 0.5 * peak 3 * factor 1.5", projected.Groups[1].NormalizedRate)
	}
}

func TestRankAPIKeyRoutingCandidatesEqualPriceScoresAreBest(t *testing.T) {
	values := []APIKeyRoutingGroupObservation{{NormalizedRate: 1}, {NormalizedRate: 1}}
	scores := inverseMinMaxEligible(values, []bool{true, true}, func(item APIKeyRoutingGroupObservation) float64 {
		return item.NormalizedRate
	})
	if scores[0] != 1 || scores[1] != 1 {
		t.Fatalf("equal price scores = %v, want both 1", scores)
	}
}

func TestEstimateAPIKeyRoutingRateUsesOrderedFailoverProbability(t *testing.T) {
	snapshot := &APIKeyRoutingScoreSnapshot{Groups: map[int64]APIKeyRoutingGroupObservation{
		1: {GroupID: 1, CacheHitRate: 0.8, LogicalInputTokens: 1000, ActualOutputTokens: 100},
		2: {GroupID: 2, CacheHitRate: 0.2, LogicalInputTokens: 2000, ActualOutputTokens: 200},
	}}
	ranked := []APIKeyRoutingCandidateScore{
		{GroupID: 1, Eligible: true, NormalizedRate: 1, SmoothedSuccessRate: 0.8, CapacityScore: 1, Confidence: 1, ObservationWindow: "1h"},
		{GroupID: 2, Eligible: true, NormalizedRate: 3, SmoothedSuccessRate: 1, CapacityScore: 1, Confidence: 1, ObservationWindow: "1h"},
	}
	estimate, ok := EstimateAPIKeyRoutingRate(ranked, snapshot)
	if !ok {
		t.Fatal("expected an estimate")
	}
	if math.Abs(estimate.PredictedGroupShare[1]-0.8) > 1e-12 || math.Abs(estimate.PredictedGroupShare[2]-0.2) > 1e-12 {
		t.Fatalf("predicted shares = %+v, want 0.8/0.2", estimate.PredictedGroupShare)
	}
	if math.Abs(estimate.Value-1.4) > 1e-12 {
		t.Fatalf("estimated rate = %v, want 1.4", estimate.Value)
	}
	if estimate.Low > estimate.Value || estimate.High < estimate.Value || estimate.Window != "1h" {
		t.Fatalf("invalid estimate interval/window: %+v", estimate)
	}
	if estimate.LogicalInputTokens != 3000 || estimate.OutputTokens != 300 {
		t.Fatalf("sample volumes = %d/%d, want 3000/300", estimate.LogicalInputTokens, estimate.OutputTokens)
	}
}

func TestEstimateAPIKeyRoutingRateBlendsVersionMatchedObservedLandingShare(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	ranked := []APIKeyRoutingCandidateScore{
		{GroupID: 1, Eligible: true, NormalizedRate: 1, SmoothedSuccessRate: .8, CapacityScore: 1, Confidence: 1, PriceConfidence: 1, ObservationWindow: "1h"},
		{GroupID: 2, Eligible: true, NormalizedRate: 3, SmoothedSuccessRate: 1, CapacityScore: 1, Confidence: 1, PriceConfidence: 1, ObservationWindow: "1h"},
	}
	evidence := &APIKeyRoutingSelectionEvidence{
		WeightedSelections: map[int64]float64{1: 20, 2: 80},
		WeightSquares:      100, SampledSelections: 100, DataThrough: now, Window: "24h",
	}

	estimate, ok := EstimateAPIKeyRoutingRateWithSelectionEvidence(ranked, nil, evidence, now)
	if !ok {
		t.Fatal("expected an estimate")
	}
	if math.Abs(estimate.PredictedGroupShare[1]-.29) > 1e-12 || math.Abs(estimate.PredictedGroupShare[2]-.71) > 1e-12 {
		t.Fatalf("blended shares = %+v, want .29/.71", estimate.PredictedGroupShare)
	}
	if math.Abs(estimate.Value-2.42) > 1e-12 {
		t.Fatalf("blended estimated rate = %v, want 2.42", estimate.Value)
	}
	if estimate.SelectionSource != "blended" || estimate.SelectionWindow != "24h" || estimate.SelectionSamples != 100 || estimate.SelectionEffectiveN != 100 {
		t.Fatalf("unexpected selection evidence: %+v", estimate)
	}
	if estimate.Confidence != 1 {
		t.Fatalf("estimate confidence = %v, want 1", estimate.Confidence)
	}
}

func TestAPIKeyRoutingSelectionEvidenceForSnapshotRejectsOldRouteVersion(t *testing.T) {
	preference := APIKeySmartPreferencePrice
	key := &APIKey{
		ID: 7, RouteVersion: 3, SmartPreference: &preference,
		RoutingSelectionObservations: []APIKeyRoutingSelectionObservation{
			{APIKeyID: 7, RouteVersion: 2, Platform: PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses", StrategyVersion: "s1", SmartPreference: preference, GroupID: 1, SampledSelections: 100, WeightedSelections: 100, WeightSquares: 100},
			{APIKeyID: 7, RouteVersion: 3, Platform: PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses", StrategyVersion: "s1", SmartPreference: preference, GroupID: 2, SampledSelections: 20, WeightedSelections: 20, WeightSquares: 20},
		},
	}
	snapshot := &APIKeyRoutingScoreSnapshot{Platform: PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses", StrategyVersion: "s1"}

	evidence, ok := APIKeyRoutingSelectionEvidenceForSnapshot(key, snapshot)
	if !ok {
		t.Fatal("expected current-version evidence")
	}
	if len(evidence.WeightedSelections) != 1 || evidence.WeightedSelections[2] != 20 {
		t.Fatalf("old route version leaked into evidence: %+v", evidence.WeightedSelections)
	}
}

func TestRoutingCapacityDiscountsSharedDependencyDomain(t *testing.T) {
	domain := "account_pool:0123456789abcdef0123456789abcdef"
	observations := []APIKeyRoutingGroupObservation{
		{GroupID: 1, CapacityScore: 1, DependencyDomains: []string{"provider:openai", domain}},
		{GroupID: 2, CapacityScore: 1, DependencyDomains: []string{"provider:openai", domain}},
		{GroupID: 3, CapacityScore: 1, DependencyDomains: []string{"provider:openai", "account_pool:abcdef0123456789abcdef0123456789"}},
	}
	counts := map[string]int{
		"provider:openai": 3,
		domain:            2,
		"account_pool:abcdef0123456789abcdef0123456789": 1,
	}
	shared := correlationAdjustedCapacity(observations[0], counts)
	independent := correlationAdjustedCapacity(observations[2], counts)
	if math.Abs(shared-1/math.Sqrt2) > 1e-12 {
		t.Fatalf("shared capacity = %v, want 1/sqrt(2)", shared)
	}
	if independent != 1 {
		t.Fatalf("independent capacity = %v, want 1", independent)
	}
}
