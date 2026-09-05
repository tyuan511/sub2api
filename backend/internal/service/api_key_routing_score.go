package service

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type APIKeyRoutingScoreWeights struct {
	Success  float64 `json:"success"`
	Price    float64 `json:"price"`
	Speed    float64 `json:"speed"`
	Capacity float64 `json:"capacity"`
}

func APIKeyRoutingWeights(preference string) APIKeyRoutingScoreWeights {
	switch preference {
	case APIKeySmartPreferencePrice:
		return APIKeyRoutingScoreWeights{Success: 0.50, Price: 0.35, Speed: 0.05, Capacity: 0.10}
	case APIKeySmartPreferenceSpeed:
		return APIKeyRoutingScoreWeights{Success: 0.50, Price: 0.05, Speed: 0.35, Capacity: 0.10}
	default:
		return APIKeyRoutingScoreWeights{Success: 0.50, Price: 0.20, Speed: 0.20, Capacity: 0.10}
	}
}

// NormalizeAPIKeyRoutingModelFamily keeps score-cardinality bounded while
// retaining the product families whose behavior and pricing are meaningfully
// different. Unknown names share a platform baseline instead of becoming an
// attacker-controlled Redis/metrics dimension.
func NormalizeAPIKeyRoutingModelFamily(platform, model string) string {
	value := strings.ToLower(strings.TrimSpace(model))
	if index := strings.LastIndex(value, "/models/"); index >= 0 {
		value = value[index+len("/models/"):]
	}
	value = strings.TrimPrefix(value, "models/")
	families := []struct {
		needle string
		name   string
	}{
		{"claude-opus", "claude-opus"},
		{"claude-sonnet", "claude-sonnet"},
		{"claude-haiku", "claude-haiku"},
		{"gpt-5", "gpt-5"},
		{"gpt-4.1", "gpt-4.1"},
		{"gpt-4o", "gpt-4o"},
		{"o4-", "o4"},
		{"o3-", "o3"},
		{"gemini-3", "gemini-3"},
		{"gemini-2.5", "gemini-2.5"},
		{"gemini-2.0", "gemini-2.0"},
		{"grok-4", "grok-4"},
		{"grok-3", "grok-3"},
	}
	for _, family := range families {
		if strings.Contains(value, family.needle) {
			return family.name
		}
	}
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform == "" {
		platform = "unknown"
	}
	return platform + "-other"
}

// NormalizeAPIKeyRoutingEndpointKind converts concrete route paths into a
// small enum suitable for score snapshots and breaker diagnostics.
func NormalizeAPIKeyRoutingEndpointKind(path string) string {
	value := strings.ToLower(strings.TrimSpace(path))
	switch value {
	case "messages", "chat_completions", "responses", "embeddings", "images", "video", "audio", "live", "generate_content", "count_tokens", "other":
		return value
	}
	switch {
	case strings.Contains(value, "counttokens") || strings.Contains(value, "count_tokens"):
		return "count_tokens"
	case strings.Contains(value, "streamgeneratecontent") || strings.Contains(value, "generatecontent"):
		return "generate_content"
	case strings.Contains(value, "chat/completions"):
		return "chat_completions"
	case strings.Contains(value, "responses"):
		return "responses"
	case strings.Contains(value, "embeddings"):
		return "embeddings"
	case strings.Contains(value, "images"):
		return "images"
	case strings.Contains(value, "video"):
		return "video"
	case strings.Contains(value, "audio"):
		return "audio"
	case strings.Contains(value, "live") || strings.Contains(value, "realtime"):
		return "live"
	case strings.Contains(value, "messages"):
		return "messages"
	default:
		return "other"
	}
}

type APIKeyRoutingGroupObservation struct {
	GroupID         int64   `json:"group_id"`
	SuccessRequests int64   `json:"success_requests"`
	FailedRequests  int64   `json:"failed_requests"`
	TTFTP50Ms       float64 `json:"ttft_p50_ms"`
	DurationP50Ms   float64 `json:"duration_p50_ms"`
	CapacityScore   float64 `json:"capacity_score"`
	NormalizedRate  float64 `json:"normalized_rate"`
	// PriceNormalizationFactor is the observed workload cost divided by the
	// full-cache reference cost before applying any group/user multiplier.
	// Keeping it separate allows one shared snapshot to be projected with each
	// user's effective rate without rebuilding global observations.
	PriceNormalizationFactor float64   `json:"price_normalization_factor"`
	Confidence               float64   `json:"confidence"`
	PriceConfidence          float64   `json:"price_confidence"`
	PriceFallback            bool      `json:"price_fallback,omitempty"`
	SmoothedSuccessRate      float64   `json:"smoothed_success_rate"`
	CacheHitRate             float64   `json:"cache_hit_rate"`
	LogicalInputTokens       int64     `json:"logical_input_tokens"`
	ActualOutputTokens       int64     `json:"actual_output_tokens"`
	ObservationWindow        string    `json:"observation_window"`
	ObservationGeneratedAt   time.Time `json:"observation_generated_at"`
	DependencyDomains        []string  `json:"dependency_domains,omitempty"`
}

type APIKeyRoutingScoreSnapshot struct {
	Version         string                                  `json:"version"`
	FeatureVersion  string                                  `json:"feature_version"`
	StrategyVersion string                                  `json:"strategy_version"`
	ModelVersion    *string                                 `json:"model_version,omitempty"`
	ExperimentID    *string                                 `json:"experiment_id,omitempty"`
	Platform        string                                  `json:"platform"`
	ModelFamily     string                                  `json:"model_family"`
	EndpointKind    string                                  `json:"endpoint_kind"`
	GeneratedAt     time.Time                               `json:"generated_at"`
	Groups          map[int64]APIKeyRoutingGroupObservation `json:"groups"`
}

type APIKeyRoutingCandidateScore struct {
	GroupID               int64                     `json:"group_id"`
	Priority              int                       `json:"priority"`
	Eligible              bool                      `json:"eligible"`
	Exclusion             string                    `json:"exclusion,omitempty"`
	Score                 float64                   `json:"score"`
	SuccessRate           float64                   `json:"success_rate"`
	SmoothedSuccessRate   float64                   `json:"smoothed_success_rate"`
	NormalizedRate        float64                   `json:"normalized_rate"`
	Confidence            float64                   `json:"confidence"`
	PriceConfidence       float64                   `json:"price_confidence"`
	TTFTMS                float64                   `json:"ttft_ms"`
	DurationMS            float64                   `json:"duration_ms"`
	CapacityScore         float64                   `json:"capacity_score"`
	CacheHitRate          float64                   `json:"cache_hit_rate"`
	ObservationWindow     string                    `json:"observation_window"`
	DependencyDomains     []string                  `json:"dependency_domains,omitempty"`
	Breakdown             APIKeyRoutingScoreWeights `json:"score_breakdown"`
	SharedBaselineScore   *float64                  `json:"shared_baseline_score,omitempty"`
	LearningAdjustment    *float64                  `json:"learning_adjustment,omitempty"`
	PersonalizationWeight *float64                  `json:"personalization_weight,omitempty"`
}

type APIKeyRoutingRateEstimate struct {
	Value               float64
	Low                 float64
	High                float64
	Confidence          float64
	Window              string
	SelectionSource     string
	SelectionWindow     string
	SelectionSamples    int64
	SelectionEffectiveN float64
	SelectionConfidence float64
	CacheHitRate        float64
	LogicalInputTokens  int64
	OutputTokens        int64
	PredictedGroupShare map[int64]float64
}

type APIKeyRoutingSelectionEvidence struct {
	WeightedSelections map[int64]float64
	WeightSquares      float64
	SampledSelections  int64
	DataThrough        time.Time
	Window             string
}

// RankAPIKeyRoutingCandidates applies one explainable scoring model. Every
// preference shares the same 50% success hard gate and success weight; only the
// price/speed emphasis changes.
func RankAPIKeyRoutingCandidates(candidates []APIKeyRouteCandidate, snapshot *APIKeyRoutingScoreSnapshot, preference string, minimumSamples int64) []APIKeyRoutingCandidateScore {
	policy := DefaultAPIKeyRoutingStrategyPolicy(preference)
	if minimumSamples > 0 {
		policy.MinimumSamples = minimumSamples
	}
	return RankAPIKeyRoutingCandidatesWithPolicy(candidates, snapshot, policy)
}

// RankAPIKeyRoutingCandidatesWithPolicy applies versioned weights and bounded
// reliability controls. Candidate membership has already been frozen by the
// route plan, and the raw-success hard gate is evaluated before every score.
func RankAPIKeyRoutingCandidatesWithPolicy(candidates []APIKeyRouteCandidate, snapshot *APIKeyRoutingScoreSnapshot, policy APIKeyRoutingStrategyPolicy) []APIKeyRoutingCandidateScore {
	if ValidateAPIKeyRoutingStrategyPolicy(policy) != nil {
		policy = DefaultAPIKeyRoutingStrategyPolicy(policy.Preference)
	}
	minimumSamples := policy.MinimumSamples
	weights := policy.Weights
	result := make([]APIKeyRoutingCandidateScore, 0, len(candidates))
	observations := make([]APIKeyRoutingGroupObservation, len(candidates))
	eligible := make([]bool, len(candidates))
	for i, candidate := range candidates {
		if snapshot != nil {
			observations[i] = snapshot.Groups[candidate.GroupID]
		}
		if observations[i].GroupID == 0 {
			observations[i].GroupID = candidate.GroupID
			if candidate.Group != nil {
				observations[i].NormalizedRate = nonNegativeFiniteOr(candidate.Group.RateMultiplier, 1)
			}
		}
		total := observations[i].SuccessRequests + observations[i].FailedRequests
		eligible[i] = total < minimumSamples || float64(observations[i].SuccessRequests)/float64(total) >= policy.SuccessRateHardGate
	}
	dependencyCounts := make(map[string]int)
	for index, observation := range observations {
		if !eligible[index] {
			continue
		}
		for _, domain := range observation.DependencyDomains {
			dependencyCounts[domain]++
		}
	}
	priceScores := inverseMinMaxEligible(observations, eligible, func(o APIKeyRoutingGroupObservation) float64 { return nonNegativeFiniteOr(o.NormalizedRate, 1) })
	speedScores := inverseMinMaxEligible(observations, eligible, func(o APIKeyRoutingGroupObservation) float64 {
		if o.TTFTP50Ms > 0 {
			return o.TTFTP50Ms
		}
		return positiveOr(o.DurationP50Ms, 1)
	})
	for i, candidate := range candidates {
		observation := observations[i]
		total := observation.SuccessRequests + observation.FailedRequests
		successRate := 1.0
		if total > 0 {
			successRate = float64(observation.SuccessRequests) / float64(total)
		}
		fallbackRate := 1.0
		if candidate.Group != nil {
			fallbackRate = nonNegativeFiniteOr(candidate.Group.RateMultiplier, 1)
		}
		score := APIKeyRoutingCandidateScore{
			GroupID: candidate.GroupID, Priority: candidate.Priority, Eligible: true,
			SuccessRate: successRate, SmoothedSuccessRate: routeClamp01(observation.SmoothedSuccessRate), NormalizedRate: nonNegativeFiniteOr(observation.NormalizedRate, fallbackRate),
			Confidence: routeClamp01(observation.Confidence), PriceConfidence: routeClamp01(observation.PriceConfidence), TTFTMS: observation.TTFTP50Ms,
			DurationMS: observation.DurationP50Ms, CapacityScore: correlationAdjustedCapacity(observation, dependencyCounts),
			CacheHitRate: routeClamp01(observation.CacheHitRate), ObservationWindow: observation.ObservationWindow,
			DependencyDomains: append([]string(nil), observation.DependencyDomains...),
		}
		if total >= minimumSamples && successRate < policy.SuccessRateHardGate {
			score.Eligible = false
			score.Exclusion = fmt.Sprintf("success_rate_below_%d_percent", int(math.Round(policy.SuccessRateHardGate*100)))
			result = append(result, score)
			continue
		}
		confidence := score.Confidence
		if total > 0 && confidence == 0 {
			confidence = math.Min(1, float64(total)/float64(minimumSamples*4))
		}
		score.Confidence = confidence
		priceConfidence := score.PriceConfidence
		if priceConfidence == 0 && !observation.PriceFallback {
			// Backward-compatible v1 snapshots had only one confidence field.
			priceConfidence = confidence
		}
		score.PriceConfidence = priceConfidence
		scoringSuccessRate := score.SmoothedSuccessRate
		if scoringSuccessRate == 0 && successRate > 0 {
			scoringSuccessRate = successRate
		}
		score.SmoothedSuccessRate = scoringSuccessRate
		shrunkSuccess := confidence*scoringSuccessRate + (1-confidence)*0.5
		shrunkPrice := priceConfidence*priceScores[i] + (1-priceConfidence)*0.5
		shrunkSpeed := confidence*speedScores[i] + (1-confidence)*0.5
		shrunkCapacity := confidence*score.CapacityScore + (1-confidence)*0.5
		score.Breakdown = APIKeyRoutingScoreWeights{
			Success: shrunkSuccess * weights.Success, Price: shrunkPrice * weights.Price,
			Speed: shrunkSpeed * weights.Speed, Capacity: shrunkCapacity * weights.Capacity,
		}
		score.Score = score.Breakdown.Success + score.Breakdown.Price + score.Breakdown.Speed + score.Breakdown.Capacity
		result = append(result, score)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Eligible != result[j].Eligible {
			return result[i].Eligible
		}
		if math.Abs(result[i].Score-result[j].Score) > 1e-9 {
			return result[i].Score > result[j].Score
		}
		if result[i].Priority != result[j].Priority {
			return result[i].Priority < result[j].Priority
		}
		return result[i].GroupID < result[j].GroupID
	})
	return result
}

func correlationAdjustedCapacity(observation APIKeyRoutingGroupObservation, counts map[string]int) float64 {
	shared := 1
	for _, domain := range observation.DependencyDomains {
		// A common provider is expected because a Key's groups are same-platform;
		// account/proxy/network domains indicate correlated physical capacity.
		if strings.HasPrefix(domain, "provider:") {
			continue
		}
		if counts[domain] > shared {
			shared = counts[domain]
		}
	}
	return routeClamp01(observation.CapacityScore) / math.Sqrt(float64(shared))
}

type APIKeyRoutingPriceProjection struct {
	GroupRateMultiplier   float64
	UncachedInputTokens   int64
	CacheReadInputTokens  int64
	CacheCreationTokens   int64
	OutputTokens          int64
	InputUnitCost         float64
	CacheReadUnitCost     float64
	CacheCreationUnitCost float64
	OutputUnitCost        float64
}

// NormalizeAPIKeyRoutingRate projects observed token composition onto a
// full-cache baseline. A workload with 100% cache reads returns exactly the
// configured group multiplier; less effective caching yields a higher value.
func NormalizeAPIKeyRoutingRate(input APIKeyRoutingPriceProjection) float64 {
	rate := nonNegativeFiniteOr(input.GroupRateMultiplier, 1)
	logicalInput := input.UncachedInputTokens + input.CacheReadInputTokens + input.CacheCreationTokens
	if logicalInput <= 0 && input.OutputTokens <= 0 {
		return rate
	}
	cacheUnit := positiveOr(input.CacheReadUnitCost, input.InputUnitCost)
	actual := float64(input.UncachedInputTokens)*input.InputUnitCost +
		float64(input.CacheReadInputTokens)*cacheUnit +
		float64(input.CacheCreationTokens)*positiveOr(input.CacheCreationUnitCost, input.InputUnitCost) +
		float64(input.OutputTokens)*input.OutputUnitCost
	fullCache := float64(logicalInput)*cacheUnit + float64(input.OutputTokens)*input.OutputUnitCost
	if fullCache <= 0 {
		return rate
	}
	return rate * actual / fullCache
}

// ProjectAPIKeyRoutingScoreSnapshot applies the current effective rate for a
// key's bounded candidate set to a shared observation snapshot. The returned
// snapshot is request-local; the immutable shared version is never mutated.
func ProjectAPIKeyRoutingScoreSnapshot(candidates []APIKeyRouteCandidate, snapshot *APIKeyRoutingScoreSnapshot, userRates map[int64]float64) *APIKeyRoutingScoreSnapshot {
	return ProjectAPIKeyRoutingScoreSnapshotAt(candidates, snapshot, userRates, time.Now())
}

// ProjectAPIKeyRoutingScoreSnapshotAt also applies the current group peak
// multiplier at one fixed request/control-plane instant. This mirrors billing
// without mutating the shared snapshot or allowing a retry-chain refresh.
func ProjectAPIKeyRoutingScoreSnapshotAt(candidates []APIKeyRouteCandidate, snapshot *APIKeyRoutingScoreSnapshot, userRates map[int64]float64, pricingAt time.Time) *APIKeyRoutingScoreSnapshot {
	if snapshot == nil {
		return nil
	}
	projected := *snapshot
	projected.ModelVersion = cloneStringPtr(snapshot.ModelVersion)
	projected.ExperimentID = cloneStringPtr(snapshot.ExperimentID)
	projected.Groups = make(map[int64]APIKeyRoutingGroupObservation, len(candidates))
	for _, candidate := range candidates {
		observation, exists := snapshot.Groups[candidate.GroupID]
		if !exists {
			continue
		}
		observation.DependencyDomains = append([]string(nil), observation.DependencyDomains...)
		effectiveRate := 1.0
		groupRate := 1.0
		if candidate.Group != nil {
			groupRate = nonNegativeFiniteOr(candidate.Group.RateMultiplier, 1)
			effectiveRate = groupRate
		}
		if userRate, overridden := userRates[candidate.GroupID]; overridden {
			effectiveRate = nonNegativeFiniteOr(userRate, effectiveRate)
		}
		if candidate.Group != nil {
			effectiveRate *= candidate.Group.PeakMultiplierAt(pricingAt)
		}
		factor := observation.PriceNormalizationFactor
		if factor <= 0 || math.IsNaN(factor) || math.IsInf(factor, 0) {
			if groupRate > 0 {
				factor = observation.NormalizedRate / groupRate
			}
			if factor <= 0 || math.IsNaN(factor) || math.IsInf(factor, 0) {
				factor = 1
			}
		}
		observation.PriceNormalizationFactor = factor
		observation.NormalizedRate = effectiveRate * factor
		projected.Groups[candidate.GroupID] = observation
	}
	return &projected
}

// EstimateAPIKeyRoutingRate models which candidate is expected to produce the
// successful response in the fixed retry order. Scores are not interpreted as
// random traffic weights: each later group's share is the probability that all
// earlier candidates fail availability/capacity and that this candidate then
// succeeds. The result is conditioned on at least one candidate succeeding.
func EstimateAPIKeyRoutingRate(ranked []APIKeyRoutingCandidateScore, snapshot *APIKeyRoutingScoreSnapshot) (APIKeyRoutingRateEstimate, bool) {
	return EstimateAPIKeyRoutingRateWithSelectionEvidence(ranked, snapshot, nil, time.Now().UTC())
}

// EstimateAPIKeyRoutingRateWithSelectionEvidence blends the causal retry-order
// model with recent, version-matched successful landing shares. Observational
// shares are inverse-probability weighted and confidence-bounded; they can
// refine but never replace the current candidate whitelist or health gate.
func EstimateAPIKeyRoutingRateWithSelectionEvidence(
	ranked []APIKeyRoutingCandidateScore,
	snapshot *APIKeyRoutingScoreSnapshot,
	evidence *APIKeyRoutingSelectionEvidence,
	now time.Time,
) (APIKeyRoutingRateEstimate, bool) {
	estimate := APIKeyRoutingRateEstimate{PredictedGroupShare: make(map[int64]float64)}
	eligible := make([]APIKeyRoutingCandidateScore, 0, len(ranked))
	for _, candidate := range ranked {
		if candidate.Eligible && candidate.NormalizedRate >= 0 && !math.IsNaN(candidate.NormalizedRate) && !math.IsInf(candidate.NormalizedRate, 0) {
			eligible = append(eligible, candidate)
		}
	}
	if len(eligible) == 0 {
		return APIKeyRoutingRateEstimate{}, false
	}

	masses := make([]float64, len(eligible))
	reach, observedMass := 1.0, 0.0
	for i, candidate := range eligible {
		success := candidate.SmoothedSuccessRate
		if success <= 0 {
			success = candidate.SuccessRate
		}
		confidence := routeClamp01(candidate.Confidence)
		// Unknown evidence shrinks toward a neutral prior and widens the
		// interval below; it must not make an unknown candidate look perfect.
		success = confidence*routeClamp01(success) + (1-confidence)*0.5
		capacity := confidence*routeClamp01(candidate.CapacityScore) + (1-confidence)*0.5
		serveProbability := routeClamp01(success * math.Sqrt(capacity))
		masses[i] = reach * serveProbability
		observedMass += masses[i]
		reach *= 1 - serveProbability
	}
	forcedFallback := observedMass <= 1e-12
	if forcedFallback {
		masses[0], observedMass = 1, 1
	}
	shares := make([]float64, len(eligible))
	for i := range eligible {
		shares[i] = masses[i] / observedMass
	}
	estimate.SelectionSource = "modeled"
	estimate.SelectionWindow = "none"
	if evidence != nil && len(evidence.WeightedSelections) > 0 {
		var totalWeight, eligibleWeight float64
		for _, weight := range evidence.WeightedSelections {
			if weight > 0 && !math.IsNaN(weight) && !math.IsInf(weight, 0) {
				totalWeight += weight
			}
		}
		for _, candidate := range eligible {
			weight := evidence.WeightedSelections[candidate.GroupID]
			if weight > 0 && !math.IsNaN(weight) && !math.IsInf(weight, 0) {
				eligibleWeight += weight
			}
		}
		if totalWeight > 0 && eligibleWeight > 0 && evidence.WeightSquares > 0 {
			effectiveN := totalWeight * totalWeight / evidence.WeightSquares
			coverage := routeClamp01(eligibleWeight / totalWeight)
			freshness := routingSelectionFreshness(evidence.DataThrough, now)
			selectionConfidence := routeClamp01(math.Min(1, effectiveN/100) * coverage * freshness)
			blend := math.Min(.85, selectionConfidence)
			for i, candidate := range eligible {
				observedShare := math.Max(0, evidence.WeightedSelections[candidate.GroupID]) / eligibleWeight
				shares[i] = (1-blend)*shares[i] + blend*observedShare
			}
			estimate.SelectionSource = "blended"
			estimate.SelectionWindow = evidence.Window
			if estimate.SelectionWindow == "" {
				estimate.SelectionWindow = "24h"
			}
			estimate.SelectionSamples = evidence.SampledSelections
			estimate.SelectionEffectiveN = effectiveN
			estimate.SelectionConfidence = selectionConfidence
		}
	}

	window := ""
	weightedConfidence := 0.0
	for i, candidate := range eligible {
		share := shares[i]
		estimate.PredictedGroupShare[candidate.GroupID] = share
		estimate.Value += share * candidate.NormalizedRate
		weightedConfidence += share * math.Min(routeClamp01(candidate.Confidence), routeClamp01(candidate.PriceConfidence))
		if candidate.ObservationWindow != "" {
			if window == "" {
				window = candidate.ObservationWindow
			} else if window != candidate.ObservationWindow {
				window = "mixed"
			}
		}
		if snapshot != nil {
			if observation, found := snapshot.Groups[candidate.GroupID]; found {
				estimate.CacheHitRate += share * routeClamp01(observation.CacheHitRate)
				estimate.LogicalInputTokens += observation.LogicalInputTokens
				estimate.OutputTokens += observation.ActualOutputTokens
			}
		}
	}
	if window == "" {
		window = "nominal"
	}
	estimate.Window = window
	// A large modeled all-candidates-failed tail is itself uncertainty.
	estimate.Confidence = routeClamp01(weightedConfidence * observedMass)
	if forcedFallback {
		estimate.Confidence = 0
	}
	if estimate.SelectionSource == "blended" {
		// Real landing shares are observational and sampled. Until their
		// effective sample size is strong, they cap the confidence of the
		// user-facing aggregate even if per-group price evidence is excellent.
		estimate.Confidence = math.Min(estimate.Confidence, .25+.75*estimate.SelectionConfidence)
	} else {
		estimate.Confidence = math.Min(estimate.Confidence, .25)
	}
	variance := 0.0
	for i, candidate := range eligible {
		share := shares[i]
		delta := candidate.NormalizedRate - estimate.Value
		variance += share * delta * delta
	}
	uncertainty := math.Sqrt(math.Max(0, variance)) + (1-estimate.Confidence)*0.15*math.Max(1, estimate.Value)
	estimate.Low = math.Max(0, estimate.Value-uncertainty)
	estimate.High = estimate.Value + uncertainty
	return estimate, true
}

// APIKeyRoutingSelectionEvidenceForSnapshot selects only observations that
// match the current config and strategy versions plus the exact scoring scope.
// This prevents traffic from an old candidate set or policy from biasing a new
// estimate after an edit or strategy promotion.
func APIKeyRoutingSelectionEvidenceForSnapshot(key *APIKey, snapshot *APIKeyRoutingScoreSnapshot) (*APIKeyRoutingSelectionEvidence, bool) {
	if key == nil || snapshot == nil || key.RouteVersion < 1 || len(key.RoutingSelectionObservations) == 0 {
		return nil, false
	}
	preference := ""
	if key.SmartPreference != nil {
		preference = *key.SmartPreference
	}
	evidence := &APIKeyRoutingSelectionEvidence{
		WeightedSelections: make(map[int64]float64),
		Window:             "24h",
	}
	for _, observation := range key.RoutingSelectionObservations {
		if observation.RouteVersion != key.RouteVersion ||
			observation.Platform != snapshot.Platform || observation.ModelFamily != snapshot.ModelFamily ||
			observation.EndpointKind != snapshot.EndpointKind || observation.StrategyVersion != snapshot.StrategyVersion ||
			observation.SmartPreference != preference || observation.GroupID <= 0 || observation.WeightedSelections <= 0 {
			continue
		}
		evidence.WeightedSelections[observation.GroupID] += observation.WeightedSelections
		evidence.WeightSquares += observation.WeightSquares
		evidence.SampledSelections += observation.SampledSelections
		if observation.DataThrough.After(evidence.DataThrough) {
			evidence.DataThrough = observation.DataThrough
		}
	}
	if len(evidence.WeightedSelections) == 0 || evidence.WeightSquares <= 0 {
		return nil, false
	}
	return evidence, true
}

func routingSelectionFreshness(observedAt, now time.Time) float64 {
	if observedAt.IsZero() || !now.After(observedAt) {
		return 1
	}
	return math.Exp(-math.Ln2 * float64(now.Sub(observedAt)) / float64(12*time.Hour))
}

func inverseMinMaxEligible(values []APIKeyRoutingGroupObservation, eligible []bool, value func(APIKeyRoutingGroupObservation) float64) []float64 {
	result := make([]float64, len(values))
	if len(values) == 0 {
		return result
	}
	minValue, maxValue := math.Inf(1), math.Inf(-1)
	for i, item := range values {
		if i >= len(eligible) || !eligible[i] {
			continue
		}
		v := value(item)
		minValue, maxValue = math.Min(minValue, v), math.Max(maxValue, v)
	}
	if math.IsInf(minValue, 1) {
		return result
	}
	if math.Abs(maxValue-minValue) < 1e-12 {
		for i := range values {
			if i < len(eligible) && eligible[i] {
				// Equal comparable prices/speeds are not mediocre; they are tied
				// for best and the stable priority/group-id tie-break decides.
				result[i] = 1
			}
		}
		return result
	}
	for i, item := range values {
		if i >= len(eligible) || !eligible[i] {
			continue
		}
		result[i] = 1 - (value(item)-minValue)/(maxValue-minValue)
	}
	return result
}

func positiveOr(value, fallback float64) float64 {
	if value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
		return value
	}
	return fallback
}

func nonNegativeFiniteOr(value, fallback float64) float64 {
	if value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
		return value
	}
	return fallback
}

func routeClamp01(value float64) float64 {
	if value < 0 || math.IsNaN(value) {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
