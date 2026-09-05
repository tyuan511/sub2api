package service

import (
	"context"
	"encoding/json"
	"time"
)

const (
	DefaultAPIKeyGroupCacheCompensationMaxTokens   = 200_000
	DefaultAPIKeyGroupCacheCompensationMaxSwitches = 1
)

// APIKeyRoutingUsageContext is the bounded, request-local routing projection
// copied into asynchronous usage workers. It deliberately contains no API key
// plaintext, request body, response body, account credential, or session hash.
type APIKeyRoutingUsageContext struct {
	DecisionID                   string
	APIKeyID                     int64
	RouteVersion                 int64
	InitialGroupID               int64
	EffectiveGroupID             int64
	Platform                     string
	ScheduleMode                 string
	SmartPreference              *string
	SmartBalanceBPS              *int
	RoutingMinSuccessRate        int
	RoutingStateVersion          int64
	SwitchCount                  int
	StrategyVersion              string
	ScoreVersion                 string
	FeatureVersion               string
	ModelVersion                 *string
	ExperimentID                 *string
	ExperimentBucket             *int
	AssignmentReason             string
	StickyBroken                 bool
	CacheCompensationMaxTokens   int
	CacheCompensationMaxSwitches int
	AttemptStartedAt             time.Time
	Candidates                   []APIKeyRoutingDecisionCandidate
}

// APIKeyRoutingDecisionCandidate is a replay-safe candidate projection. Every
// field is an ID, enum, boolean, rank, or bounded numeric score.
type APIKeyRoutingDecisionCandidate struct {
	GroupID               int64                      `json:"group_id"`
	ConfiguredPriority    int                        `json:"configured_priority"`
	Admitted              bool                       `json:"admitted"`
	ExclusionReason       string                     `json:"exclusion_reason,omitempty"`
	Rank                  *int                       `json:"rank,omitempty"`
	SuccessRate           *float64                   `json:"success_rate,omitempty"`
	SmoothedSuccessRate   *float64                   `json:"smoothed_success_rate,omitempty"`
	Confidence            *float64                   `json:"confidence,omitempty"`
	Score                 *float64                   `json:"score,omitempty"`
	ScoreBreakdown        *APIKeyRoutingScoreWeights `json:"score_breakdown,omitempty"`
	SharedBaselineScore   *float64                   `json:"shared_baseline_score,omitempty"`
	LearningAdjustment    *float64                   `json:"learning_adjustment,omitempty"`
	PersonalizationWeight *float64                   `json:"personalization_weight,omitempty"`
	NormalizedRate        *float64                   `json:"normalized_rate,omitempty"`
	TTFTMS                *float64                   `json:"ttft_ms,omitempty"`
	DurationMS            *float64                   `json:"duration_ms,omitempty"`
	CapacityScore         *float64                   `json:"capacity_score,omitempty"`
	CacheHitRate          *float64                   `json:"cache_hit_rate,omitempty"`
	ObservationWindow     string                     `json:"observation_window,omitempty"`
	DependencyDomains     []string                   `json:"dependency_domains,omitempty"`
	OutcomeVisibility     string                     `json:"outcome_visibility"`
}

type apiKeyRoutingUsageContextKey struct{}
type apiKeyGroupCacheCompensationContextKey struct{}

func WithAPIKeyGroupCacheCompensation(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, apiKeyGroupCacheCompensationContextKey{}, true)
}

func IsAPIKeyGroupCacheCompensation(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, _ := ctx.Value(apiKeyGroupCacheCompensationContextKey{}).(bool)
	return value
}

func WithAPIKeyRoutingUsageContext(ctx context.Context, value APIKeyRoutingUsageContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	value.SmartPreference = cloneStringPtr(value.SmartPreference)
	value.SmartBalanceBPS = cloneIntPtr(value.SmartBalanceBPS)
	value.ModelVersion = cloneStringPtr(value.ModelVersion)
	value.ExperimentID = cloneStringPtr(value.ExperimentID)
	value.ExperimentBucket = cloneIntPtr(value.ExperimentBucket)
	value.Candidates = cloneAPIKeyRoutingDecisionCandidates(value.Candidates)
	return context.WithValue(ctx, apiKeyRoutingUsageContextKey{}, value)
}

func APIKeyRoutingUsageContextFromContext(ctx context.Context) (APIKeyRoutingUsageContext, bool) {
	if ctx == nil {
		return APIKeyRoutingUsageContext{}, false
	}
	value, ok := ctx.Value(apiKeyRoutingUsageContextKey{}).(APIKeyRoutingUsageContext)
	if !ok || value.DecisionID == "" || value.RouteVersion < 1 {
		return APIKeyRoutingUsageContext{}, false
	}
	value.SmartPreference = cloneStringPtr(value.SmartPreference)
	value.SmartBalanceBPS = cloneIntPtr(value.SmartBalanceBPS)
	value.ModelVersion = cloneStringPtr(value.ModelVersion)
	value.ExperimentID = cloneStringPtr(value.ExperimentID)
	value.ExperimentBucket = cloneIntPtr(value.ExperimentBucket)
	value.Candidates = cloneAPIKeyRoutingDecisionCandidates(value.Candidates)
	return value, true
}

func cloneAPIKeyRoutingDecisionCandidates(values []APIKeyRoutingDecisionCandidate) []APIKeyRoutingDecisionCandidate {
	if len(values) == 0 {
		return nil
	}
	out := make([]APIKeyRoutingDecisionCandidate, len(values))
	copy(out, values)
	for i := range out {
		if values[i].Rank != nil {
			value := *values[i].Rank
			out[i].Rank = &value
		}
		if values[i].SuccessRate != nil {
			value := *values[i].SuccessRate
			out[i].SuccessRate = &value
		}
		out[i].SmoothedSuccessRate = cloneFloat64Ptr(values[i].SmoothedSuccessRate)
		if values[i].Confidence != nil {
			value := *values[i].Confidence
			out[i].Confidence = &value
		}
		if values[i].Score != nil {
			value := *values[i].Score
			out[i].Score = &value
		}
		if values[i].ScoreBreakdown != nil {
			value := *values[i].ScoreBreakdown
			out[i].ScoreBreakdown = &value
		}
		out[i].NormalizedRate = cloneFloat64Ptr(values[i].NormalizedRate)
		out[i].TTFTMS = cloneFloat64Ptr(values[i].TTFTMS)
		out[i].DurationMS = cloneFloat64Ptr(values[i].DurationMS)
		out[i].CapacityScore = cloneFloat64Ptr(values[i].CapacityScore)
		out[i].CacheHitRate = cloneFloat64Ptr(values[i].CacheHitRate)
		out[i].SharedBaselineScore = cloneFloat64Ptr(values[i].SharedBaselineScore)
		out[i].LearningAdjustment = cloneFloat64Ptr(values[i].LearningAdjustment)
		out[i].PersonalizationWeight = cloneFloat64Ptr(values[i].PersonalizationWeight)
		out[i].DependencyDomains = append([]string(nil), values[i].DependencyDomains...)
	}
	return out
}

func cloneFloat64Ptr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

// CopyAPIKeyRoutingUsageContext keeps routing attribution when billing work is
// detached from the request cancellation context.
func CopyAPIKeyRoutingUsageContext(from, to context.Context) context.Context {
	value, ok := APIKeyRoutingUsageContextFromContext(from)
	if !ok {
		return to
	}
	return WithAPIKeyRoutingUsageContext(to, value)
}

// ForceCacheBillingInputTokens returns the billable input-token portion that
// may be reclassified as cache-read. Legacy account-level failover keeps its
// existing all-input behavior; API-key group failover is additionally bounded
// and only eligible after a healthy group-sticky binding was broken.
func ForceCacheBillingInputTokens(ctx context.Context, inputTokens int) int {
	if inputTokens <= 0 || !IsForceCacheBilling(ctx) {
		return 0
	}
	if !IsAPIKeyGroupCacheCompensation(ctx) {
		return inputTokens
	}
	meta, ok := APIKeyRoutingUsageContextFromContext(ctx)
	if !ok {
		return 0
	}
	if !meta.StickyBroken || meta.SwitchCount > positiveOrDefaultInt(meta.CacheCompensationMaxSwitches, DefaultAPIKeyGroupCacheCompensationMaxSwitches) {
		return 0
	}
	limit := positiveOrDefaultInt(meta.CacheCompensationMaxTokens, DefaultAPIKeyGroupCacheCompensationMaxTokens)
	if inputTokens > limit {
		return limit
	}
	return inputTokens
}

func positiveOrDefaultInt(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

// RoutingTokenUsage is the only token shape persisted in routing facts. All
// fields are numeric and bounded by the provider response parser.
type RoutingTokenUsage struct {
	InputTokens           int `json:"input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	CacheCreationTokens   int `json:"cache_creation_tokens"`
	CacheReadTokens       int `json:"cache_read_tokens"`
	CacheCreation5mTokens int `json:"cache_creation_5m_tokens,omitempty"`
	CacheCreation1hTokens int `json:"cache_creation_1h_tokens,omitempty"`
	ImageInputTokens      int `json:"image_input_tokens,omitempty"`
	ImageOutputTokens     int `json:"image_output_tokens,omitempty"`
	// CacheCompensationAmountUSD is audit metadata for the user-side discount.
	// It belongs only to billable_usage; supplier actual_usage never carries it.
	CacheCompensationAmountUSD float64 `json:"cache_compensation_amount_usd,omitempty"`
}

func routingTokenUsageJSON(usage RoutingTokenUsage) json.RawMessage {
	payload, err := json.Marshal(usage)
	if err != nil {
		return nil
	}
	return payload
}

// ApplyAPIKeyRoutingUsage decorates the usage log. actual is the supplier-
// reported token composition before any cache compensation rewrite; billable
// is the user-facing composition after that rewrite. The final routing fact is
// emitted separately, after the billing transaction has reached a final state.
func ApplyAPIKeyRoutingUsage(ctx context.Context, log *UsageLog, actual, billable RoutingTokenUsage, cacheCompensationTokens int, cacheCompensationAmountUSD float64) {
	if log == nil {
		return
	}
	log.ActualUsage = routingTokenUsageJSON(actual)
	log.BillableUsage = routingTokenUsageJSON(billable)
	meta, ok := APIKeyRoutingUsageContextFromContext(ctx)
	if !ok {
		return
	}
	log.InitialGroupID = positiveInt64Ptr(meta.InitialGroupID)
	log.RouteVersion = positiveInt64Ptr(meta.RouteVersion)
	log.ScheduleMode = optionalStringPtr(meta.ScheduleMode)
	log.SmartPreference = cloneStringPtr(meta.SmartPreference)
	log.GroupSwitchCount = meta.SwitchCount
	log.RoutingDecisionID = optionalStringPtr(meta.DecisionID)
	if cacheCompensationTokens > 0 && meta.StickyBroken {
		log.CacheColdDueToFailover = true
		log.CacheCompensationTokens = cacheCompensationTokens
		log.CacheCompensationReason = optionalStringPtr("group_failover_cache_cold")
		if cacheCompensationAmountUSD > 0 {
			billable.CacheCompensationAmountUSD = cacheCompensationAmountUSD
			log.BillableUsage = routingTokenUsageJSON(billable)
		}
	}
}

func positiveInt64Ptr(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	copy := value
	return &copy
}

func optionalStringPtr(value string) *string {
	if value == "" {
		return nil
	}
	copy := value
	return &copy
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
