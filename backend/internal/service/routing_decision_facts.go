package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	RoutingOutcomeObserved   = "observed"
	RoutingOutcomeUnobserved = "unobserved"

	RoutingEventPrioritySample     = "sample"
	RoutingEventPriorityDiagnostic = "diagnostic"
	RoutingEventPriorityCritical   = "critical"

	RoutingAssignmentDeterministic = "deterministic"
	RoutingAssignmentShadow        = "shadow"
	RoutingAssignmentCanary        = "canary"
	RoutingAssignmentExploration   = "exploration"

	RoutingFactOutcomeDecision            = "routing_decision"
	RoutingFactOutcomeShadowDecision      = "shadow_decision"
	RoutingFactOutcomeSuccess             = "success"
	RoutingFactOutcomeRouteAttemptFailed  = "route_attempt_failed"
	RoutingFactOutcomeCapacityOverflow    = "capacity_overflow"
	RoutingFactOutcomeAllCandidatesFailed = "all_candidates_failed"
	RoutingFactOutcomePartialBilling      = "partial_billing"
)

var ErrRoutingFactInvalid = errors.New("routing decision fact is invalid")

// RoutingAttemptFact is an append-only replay fact. Its strongly typed shape
// prevents prompts, responses, credentials, or arbitrary caller metadata from
// entering the routing event stream.
type RoutingAttemptFact struct {
	EventID                string                           `json:"event_id"`
	RoutingDecisionID      string                           `json:"routing_decision_id"`
	RequestID              *string                          `json:"request_id,omitempty"`
	APIKeyID               *int64                           `json:"api_key_id,omitempty"`
	RouteVersion           int64                            `json:"route_version"`
	InitialGroupID         *int64                           `json:"initial_group_id,omitempty"`
	AttemptedGroupID       *int64                           `json:"attempted_group_id,omitempty"`
	EffectiveGroupID       *int64                           `json:"effective_group_id,omitempty"`
	SelectedGroupID        *int64                           `json:"selected_group_id,omitempty"`
	ScheduleMode           string                           `json:"schedule_mode"`
	SmartPreference        *string                          `json:"smart_preference,omitempty"`
	SmartBalanceBPS        *int                             `json:"smart_balance_bps"`
	RoutingMinSuccessRate  int                              `json:"routing_min_success_rate"`
	RoutingStateVersion    int64                            `json:"routing_state_version"`
	AttemptIndex           int                              `json:"attempt_index"`
	Platform               string                           `json:"platform"`
	ModelFamily            string                           `json:"model_family"`
	EndpointKind           string                           `json:"endpoint_kind"`
	StrategyVersion        string                           `json:"strategy_version"`
	ScoreVersion           string                           `json:"score_version"`
	FeatureSchemaVersion   string                           `json:"feature_schema_version"`
	ModelVersion           *string                          `json:"model_version,omitempty"`
	ExperimentID           *string                          `json:"experiment_id,omitempty"`
	ExperimentBucket       *int                             `json:"experiment_bucket,omitempty"`
	SampleProbability      float64                          `json:"sample_probability"`
	ActionPropensity       *float64                         `json:"action_propensity,omitempty"`
	AssignmentReason       string                           `json:"assignment_reason"`
	Candidates             []APIKeyRoutingDecisionCandidate `json:"candidates"`
	SelectedReason         *string                          `json:"selected_reason,omitempty"`
	OutcomeVisibility      string                           `json:"outcome_visibility"`
	OutcomeCategory        *string                          `json:"outcome_category,omitempty"`
	Retryable              bool                             `json:"retryable"`
	SemanticOutput         bool                             `json:"semantic_output"`
	SwitchedGroup          bool                             `json:"switched_group"`
	StickyBroken           bool                             `json:"sticky_broken"`
	BreakerTransition      *string                          `json:"breaker_transition,omitempty"`
	QueueMS                *int                             `json:"queue_ms,omitempty"`
	TTFTMS                 *int                             `json:"ttft_ms,omitempty"`
	DurationMS             *int                             `json:"duration_ms,omitempty"`
	ActualUsage            json.RawMessage                  `json:"actual_usage,omitempty"`
	BillableUsage          json.RawMessage                  `json:"billable_usage,omitempty"`
	ActualCost             *float64                         `json:"actual_cost,omitempty"`
	BilledCost             *float64                         `json:"billed_cost,omitempty"`
	CacheColdDueToFailover bool                             `json:"cache_cold_due_to_failover"`
	EventPriority          string                           `json:"event_priority"`
	OccurredAt             time.Time                        `json:"occurred_at"`
}

type RoutingFactRepository interface {
	CreateRoutingAttempts(ctx context.Context, facts []*RoutingAttemptFact) error
}

// RoutingFactRetentionRepository is optional so lightweight test and fallback
// repositories can still record facts. Production implements bounded pruning;
// experiment windows must fit within the 90-day diagnostic retention window;
// critical anomalies are retained for 180 days.
type RoutingFactRetentionRepository interface {
	PruneRoutingAttempts(ctx context.Context, sampleBefore, diagnosticBefore, criticalBefore time.Time, limit int) (int64, error)
}

type RoutingFactSink interface {
	RecordRoutingFact(fact *RoutingAttemptFact)
}

type noopRoutingFactSink struct{}

func (noopRoutingFactSink) RecordRoutingFact(*RoutingAttemptFact) {}

var defaultRoutingFactSink = struct {
	sync.RWMutex
	sink RoutingFactSink
}{sink: noopRoutingFactSink{}}

func SetDefaultRoutingFactSink(sink RoutingFactSink) {
	if sink == nil {
		sink = noopRoutingFactSink{}
	}
	defaultRoutingFactSink.Lock()
	defaultRoutingFactSink.sink = sink
	defaultRoutingFactSink.Unlock()
}

func emitRoutingFact(fact *RoutingAttemptFact) {
	defaultRoutingFactSink.RLock()
	sink := defaultRoutingFactSink.sink
	defaultRoutingFactSink.RUnlock()
	sink.RecordRoutingFact(fact)
}

func ValidateRoutingAttemptFact(fact *RoutingAttemptFact) error {
	if fact != nil {
		var minimum *int
		if fact.RoutingMinSuccessRate != 0 {
			minimum = &fact.RoutingMinSuccessRate
		}
		if ValidateAPIKeyRoutingControls(fact.SmartBalanceBPS, minimum) != nil || fact.RoutingStateVersion < 0 {
			return fmt.Errorf("%w: invalid routing controls", ErrRoutingFactInvalid)
		}
	}
	if fact == nil || strings.TrimSpace(fact.EventID) == "" || len(fact.EventID) > 64 ||
		strings.TrimSpace(fact.RoutingDecisionID) == "" || len(fact.RoutingDecisionID) > 64 ||
		fact.RouteVersion < 1 || fact.AttemptIndex < 0 || fact.AttemptIndex >= DefaultMaxAPIKeyGroupRoutes {
		return ErrRoutingFactInvalid
	}
	if !oneOf(fact.ScheduleMode, APIKeyScheduleModeSequential, APIKeyScheduleModeSmart) ||
		(fact.ScheduleMode == APIKeyScheduleModeSmart && (fact.SmartPreference == nil || !oneOf(*fact.SmartPreference, APIKeySmartPreferencePrice, APIKeySmartPreferenceSpeed, APIKeySmartPreferenceBalanced))) ||
		(fact.ScheduleMode == APIKeyScheduleModeSequential && fact.SmartPreference != nil) {
		return fmt.Errorf("%w: invalid scheduling policy", ErrRoutingFactInvalid)
	}
	if strings.TrimSpace(fact.Platform) == "" || strings.TrimSpace(fact.ModelFamily) == "" || strings.TrimSpace(fact.EndpointKind) == "" ||
		strings.TrimSpace(fact.StrategyVersion) == "" || strings.TrimSpace(fact.ScoreVersion) == "" || strings.TrimSpace(fact.FeatureSchemaVersion) == "" {
		return fmt.Errorf("%w: incomplete versioned scope", ErrRoutingFactInvalid)
	}
	if fact.SampleProbability <= 0 || fact.SampleProbability > 1 ||
		(fact.ActionPropensity != nil && (*fact.ActionPropensity <= 0 || *fact.ActionPropensity > 1)) ||
		(fact.AssignmentReason == RoutingAssignmentDeterministic && fact.ActionPropensity != nil) {
		return fmt.Errorf("%w: invalid propensity", ErrRoutingFactInvalid)
	}
	if !oneOf(fact.AssignmentReason, RoutingAssignmentDeterministic, RoutingAssignmentShadow, RoutingAssignmentCanary, RoutingAssignmentExploration) ||
		!oneOf(fact.OutcomeVisibility, RoutingOutcomeObserved, RoutingOutcomeUnobserved) ||
		!oneOf(fact.EventPriority, RoutingEventPrioritySample, RoutingEventPriorityDiagnostic, RoutingEventPriorityCritical) || len(fact.Candidates) > DefaultMaxAPIKeyGroupRoutes {
		return fmt.Errorf("%w: invalid enum or candidate count", ErrRoutingFactInvalid)
	}
	if fact.OutcomeCategory != nil && !oneOf(*fact.OutcomeCategory,
		RoutingFactOutcomeDecision, RoutingFactOutcomeShadowDecision, RoutingFactOutcomeSuccess,
		RoutingFactOutcomeRouteAttemptFailed, RoutingFactOutcomeCapacityOverflow, RoutingFactOutcomeAllCandidatesFailed,
		RoutingFactOutcomePartialBilling) {
		return fmt.Errorf("%w: invalid outcome category", ErrRoutingFactInvalid)
	}
	if !validRoutingFactJSON(fact.ActualUsage) || !validRoutingFactJSON(fact.BillableUsage) {
		return fmt.Errorf("%w: invalid usage object", ErrRoutingFactInvalid)
	}
	for _, candidate := range fact.Candidates {
		if candidate.GroupID <= 0 || candidate.ConfiguredPriority < 0 || candidate.ConfiguredPriority >= DefaultMaxAPIKeyGroupRoutes ||
			!oneOf(candidate.OutcomeVisibility, RoutingOutcomeObserved, RoutingOutcomeUnobserved) {
			return fmt.Errorf("%w: invalid candidate", ErrRoutingFactInvalid)
		}
		for _, value := range []*float64{candidate.SuccessRate, candidate.SmoothedSuccessRate, candidate.Confidence, candidate.Score, candidate.NormalizedRate,
			candidate.TTFTMS, candidate.DurationMS, candidate.CapacityScore, candidate.CacheHitRate, candidate.SharedBaselineScore,
			candidate.LearningAdjustment, candidate.PersonalizationWeight} {
			if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0)) {
				return fmt.Errorf("%w: non-finite candidate feature", ErrRoutingFactInvalid)
			}
		}
		for _, value := range []*float64{candidate.SuccessRate, candidate.SmoothedSuccessRate, candidate.Confidence, candidate.Score, candidate.CapacityScore, candidate.CacheHitRate,
			candidate.SharedBaselineScore, candidate.PersonalizationWeight} {
			if value != nil && (*value < 0 || *value > 1) {
				return fmt.Errorf("%w: candidate ratio out of bounds", ErrRoutingFactInvalid)
			}
		}
		for _, value := range []*float64{candidate.NormalizedRate, candidate.TTFTMS, candidate.DurationMS} {
			if value != nil && *value < 0 {
				return fmt.Errorf("%w: negative candidate metric", ErrRoutingFactInvalid)
			}
		}
		if candidate.LearningAdjustment != nil && (*candidate.LearningAdjustment < -1 || *candidate.LearningAdjustment > 1) {
			return fmt.Errorf("%w: learning adjustment out of bounds", ErrRoutingFactInvalid)
		}
		if candidate.ObservationWindow != "" && !oneOf(candidate.ObservationWindow, "1h", "24h", "platform_baseline", "nominal") {
			return fmt.Errorf("%w: invalid observation window", ErrRoutingFactInvalid)
		}
		if len(candidate.DependencyDomains) > 4 {
			return fmt.Errorf("%w: too many dependency domains", ErrRoutingFactInvalid)
		}
		for _, domain := range candidate.DependencyDomains {
			if !boundedRoutingDimension(domain) || !(strings.HasPrefix(domain, "provider:") || strings.HasPrefix(domain, "account_pool:") || strings.HasPrefix(domain, "proxy_pool:") || strings.HasPrefix(domain, "network:") || strings.HasPrefix(domain, "region:")) {
				return fmt.Errorf("%w: invalid dependency domain", ErrRoutingFactInvalid)
			}
		}
	}
	return nil
}

func validRoutingFactJSON(value json.RawMessage) bool {
	if len(value) == 0 {
		return true
	}
	var object map[string]float64
	return json.Unmarshal(value, &object) == nil && object != nil
}

// RoutingFactFromUsage creates only a final outcome fact. Failure-attempt and
// breaker-transition facts use the same contract through the event recorder.
func RoutingFactFromUsage(ctx context.Context, log *UsageLog) (*RoutingAttemptFact, bool) {
	meta, ok := APIKeyRoutingUsageContextFromContext(ctx)
	if !ok || log == nil || log.APIKeyID <= 0 || log.GroupID == nil || *log.GroupID <= 0 {
		return nil, false
	}
	strategyVersion, scoreVersion, featureVersion := meta.StrategyVersion, meta.ScoreVersion, meta.FeatureVersion
	if strategyVersion == "" {
		strategyVersion = "sequential-v1"
	}
	if scoreVersion == "" {
		scoreVersion = "none"
	}
	if featureVersion == "" {
		featureVersion = "routing-facts-v1"
	}
	platform := meta.Platform
	if platform == "" {
		platform = PlatformFromAPIKey(log.APIKey)
	}
	if platform == "" && log.Group != nil {
		platform = log.Group.Platform
	}
	if platform == "" {
		platform = "unknown"
	}
	modelFamily := NormalizeAPIKeyRoutingModelFamily(platform, log.Model)
	endpoint := ""
	if log.InboundEndpoint != nil {
		endpoint = *log.InboundEndpoint
	}
	actualCost := log.TotalCost
	if log.AccountStatsCost != nil {
		actualCost = *log.AccountStatsCost
	} else if log.AccountRateMultiplier != nil {
		actualCost *= *log.AccountRateMultiplier
	}
	billedCost := log.ActualCost
	fact := &RoutingAttemptFact{
		EventID:           routingFactEventID(meta.DecisionID, RoutingFactOutcomeSuccess, minInt(meta.SwitchCount, DefaultMaxAPIKeyGroupRoutes-1), *log.GroupID),
		RoutingDecisionID: meta.DecisionID, RequestID: optionalStringPtr(log.RequestID),
		APIKeyID: positiveInt64Ptr(log.APIKeyID), RouteVersion: meta.RouteVersion,
		InitialGroupID: positiveInt64Ptr(meta.InitialGroupID), AttemptedGroupID: positiveInt64Ptr(*log.GroupID),
		EffectiveGroupID: positiveInt64Ptr(*log.GroupID), SelectedGroupID: positiveInt64Ptr(*log.GroupID),
		ScheduleMode: meta.ScheduleMode, SmartPreference: cloneStringPtr(meta.SmartPreference), AttemptIndex: minInt(meta.SwitchCount, DefaultMaxAPIKeyGroupRoutes-1),
		SmartBalanceBPS: cloneIntPtr(meta.SmartBalanceBPS), RoutingMinSuccessRate: meta.RoutingMinSuccessRate, RoutingStateVersion: meta.RoutingStateVersion,
		Platform: platform, ModelFamily: modelFamily, EndpointKind: NormalizeAPIKeyRoutingEndpointKind(endpoint),
		StrategyVersion: strategyVersion, ScoreVersion: scoreVersion, FeatureSchemaVersion: featureVersion,
		ModelVersion: cloneStringPtr(meta.ModelVersion), ExperimentID: cloneStringPtr(meta.ExperimentID), ExperimentBucket: cloneIntPtr(meta.ExperimentBucket),
		SampleProbability: 1, AssignmentReason: routingAssignmentReason(meta.AssignmentReason),
		Candidates: cloneAPIKeyRoutingDecisionCandidates(meta.Candidates), OutcomeVisibility: RoutingOutcomeObserved,
		OutcomeCategory: optionalStringPtr(RoutingFactOutcomeSuccess), SemanticOutput: true, SwitchedGroup: meta.SwitchCount > 0,
		StickyBroken: meta.StickyBroken,
		TTFTMS:       log.FirstTokenMs, DurationMS: log.DurationMs, ActualUsage: append(json.RawMessage(nil), log.ActualUsage...),
		BillableUsage: append(json.RawMessage(nil), log.BillableUsage...), ActualCost: &actualCost, BilledCost: &billedCost,
		CacheColdDueToFailover: log.CacheColdDueToFailover, EventPriority: RoutingEventPriorityDiagnostic, OccurredAt: log.CreatedAt,
	}
	if fact.OccurredAt.IsZero() {
		fact.OccurredAt = time.Now()
	}
	return fact, true
}

// EmitAPIKeyRoutingUsageFact closes the decision chain only after billing has
// reached a final state. A failed atomic billing transaction is retained as a
// critical partial-billing fact instead of being mislabeled as success.
func EmitAPIKeyRoutingUsageFact(ctx context.Context, log *UsageLog, billingFailed bool) {
	fact, ok := RoutingFactFromUsage(ctx, log)
	if !ok {
		return
	}
	if billingFailed {
		fact.EventID = routingFactEventID(fact.RoutingDecisionID, RoutingFactOutcomePartialBilling, fact.AttemptIndex, *fact.EffectiveGroupID)
		fact.OutcomeCategory = optionalStringPtr(RoutingFactOutcomePartialBilling)
		fact.EventPriority = RoutingEventPriorityCritical
	}
	emitRoutingFact(fact)
	logAPIKeyRoutingOutcome(ctx, fact)
}

func emitAPIKeyRoutingFailureFact(ctx context.Context, apiKeyID, routeVersion, groupID int64, model, endpoint, breakerState, outcomeCategory string) {
	meta, ok := APIKeyRoutingUsageContextFromContext(ctx)
	if !ok || apiKeyID <= 0 || routeVersion < 1 || groupID <= 0 {
		return
	}
	strategyVersion, scoreVersion, featureVersion := meta.StrategyVersion, meta.ScoreVersion, meta.FeatureVersion
	if strategyVersion == "" {
		strategyVersion = "sequential-v1"
	}
	if scoreVersion == "" {
		scoreVersion = "none"
	}
	if featureVersion == "" {
		featureVersion = "routing-facts-v1"
	}
	platform := meta.Platform
	if platform == "" {
		platform = "unknown"
	}
	var transition *string
	if breakerState != "" && breakerState != APIKeyRouteBreakerClosed {
		transition = optionalStringPtr(breakerState)
	}
	var durationMS *int
	if !meta.AttemptStartedAt.IsZero() {
		value := int(time.Since(meta.AttemptStartedAt).Milliseconds())
		if value < 0 {
			value = 0
		}
		durationMS = &value
	}
	emitRoutingFact(&RoutingAttemptFact{
		EventID: uuid.NewString(), RoutingDecisionID: meta.DecisionID, APIKeyID: positiveInt64Ptr(apiKeyID),
		RouteVersion: routeVersion, InitialGroupID: positiveInt64Ptr(meta.InitialGroupID), AttemptedGroupID: positiveInt64Ptr(groupID),
		SelectedGroupID: positiveInt64Ptr(groupID), ScheduleMode: meta.ScheduleMode, SmartPreference: cloneStringPtr(meta.SmartPreference),
		SmartBalanceBPS: cloneIntPtr(meta.SmartBalanceBPS), RoutingMinSuccessRate: meta.RoutingMinSuccessRate, RoutingStateVersion: meta.RoutingStateVersion,
		AttemptIndex: minInt(meta.SwitchCount, DefaultMaxAPIKeyGroupRoutes-1), Platform: platform,
		ModelFamily: NormalizeAPIKeyRoutingModelFamily(platform, model), EndpointKind: NormalizeAPIKeyRoutingEndpointKind(endpoint),
		StrategyVersion: strategyVersion, ScoreVersion: scoreVersion, FeatureSchemaVersion: featureVersion,
		ModelVersion: cloneStringPtr(meta.ModelVersion), ExperimentID: cloneStringPtr(meta.ExperimentID), ExperimentBucket: cloneIntPtr(meta.ExperimentBucket),
		SampleProbability: 1, AssignmentReason: routingAssignmentReason(meta.AssignmentReason),
		Candidates: cloneAPIKeyRoutingDecisionCandidates(meta.Candidates), OutcomeVisibility: RoutingOutcomeObserved,
		OutcomeCategory: optionalStringPtr(outcomeCategory), Retryable: true, SemanticOutput: false,
		SwitchedGroup: meta.SwitchCount > 0, StickyBroken: meta.StickyBroken, BreakerTransition: transition, EventPriority: RoutingEventPriorityCritical,
		DurationMS: durationMS,
		OccurredAt: time.Now(),
	})
}

func RecordAPIKeyRoutingShadowDecision(ctx context.Context, shadowPolicy APIKeyRoutingStrategyPolicy, snapshot *APIKeyRoutingScoreSnapshot, ranked []APIKeyRoutingCandidateScore) {
	meta, ok := APIKeyRoutingUsageContextFromContext(ctx)
	if !ok || snapshot == nil || meta.APIKeyID <= 0 || shadowPolicy.Version == "" || len(ranked) == 0 {
		return
	}
	candidates := make([]APIKeyRoutingDecisionCandidate, 0, len(ranked))
	var selectedGroupID *int64
	for rank, score := range ranked {
		rankCopy := rank
		success, smoothedSuccess, confidence, total, breakdown := score.SuccessRate, score.SmoothedSuccessRate, score.Confidence, score.Score, score.Breakdown
		candidate := APIKeyRoutingDecisionCandidate{
			GroupID: score.GroupID, ConfiguredPriority: score.Priority, Admitted: score.Eligible, Rank: &rankCopy,
			SuccessRate: &success, SmoothedSuccessRate: &smoothedSuccess, Confidence: &confidence, Score: &total, ScoreBreakdown: &breakdown,
			ExclusionReason: score.Exclusion, OutcomeVisibility: RoutingOutcomeUnobserved,
		}
		normalizedRate, ttft, duration := score.NormalizedRate, score.TTFTMS, score.DurationMS
		capacity, cacheHit := score.CapacityScore, score.CacheHitRate
		candidate.NormalizedRate, candidate.TTFTMS, candidate.DurationMS = &normalizedRate, &ttft, &duration
		candidate.CapacityScore, candidate.CacheHitRate, candidate.ObservationWindow = &capacity, &cacheHit, score.ObservationWindow
		candidate.DependencyDomains = append([]string(nil), score.DependencyDomains...)
		candidate.SharedBaselineScore = cloneFloat64Ptr(score.SharedBaselineScore)
		candidate.LearningAdjustment = cloneFloat64Ptr(score.LearningAdjustment)
		candidate.PersonalizationWeight = cloneFloat64Ptr(score.PersonalizationWeight)
		candidates = append(candidates, candidate)
		if selectedGroupID == nil && score.Eligible {
			selectedGroupID = positiveInt64Ptr(score.GroupID)
		}
	}
	if selectedGroupID == nil {
		return
	}
	emitRoutingFact(&RoutingAttemptFact{
		EventID: uuid.NewString(), RoutingDecisionID: meta.DecisionID, APIKeyID: positiveInt64Ptr(meta.APIKeyID),
		RouteVersion: meta.RouteVersion, InitialGroupID: positiveInt64Ptr(meta.InitialGroupID), SelectedGroupID: selectedGroupID,
		ScheduleMode: meta.ScheduleMode, SmartPreference: cloneStringPtr(meta.SmartPreference), AttemptIndex: 0,
		SmartBalanceBPS: cloneIntPtr(meta.SmartBalanceBPS), RoutingMinSuccessRate: meta.RoutingMinSuccessRate, RoutingStateVersion: meta.RoutingStateVersion,
		Platform: snapshot.Platform, ModelFamily: snapshot.ModelFamily, EndpointKind: snapshot.EndpointKind,
		StrategyVersion: shadowPolicy.Version, ScoreVersion: snapshot.Version, FeatureSchemaVersion: snapshot.FeatureVersion,
		ModelVersion: cloneStringPtr(snapshot.ModelVersion), SampleProbability: 1, AssignmentReason: RoutingAssignmentShadow,
		Candidates: candidates, SelectedReason: optionalStringPtr("shadow_top_rank"), OutcomeVisibility: RoutingOutcomeUnobserved,
		OutcomeCategory: optionalStringPtr(RoutingFactOutcomeShadowDecision), EventPriority: RoutingEventPriorityDiagnostic, OccurredAt: time.Now(),
	})
}

// RecordAPIKeyRoutingDecision emits the point-in-time input half of the event
// chain. A final usage or terminal-failure fact with the same decision ID must
// follow. Canary/control decisions are retained in full; ordinary baseline
// traffic remains subject to deterministic sampling in RoutingFactRecorder.
func RecordAPIKeyRoutingDecision(ctx context.Context, modelFamily, endpointKind string) {
	meta, ok := APIKeyRoutingUsageContextFromContext(ctx)
	if !ok || meta.APIKeyID <= 0 || len(meta.Candidates) == 0 {
		return
	}
	selectedGroupID := positiveInt64Ptr(meta.EffectiveGroupID)
	if selectedGroupID == nil {
		return
	}
	strategyVersion, scoreVersion, featureVersion := routingFactVersions(meta)
	emitRoutingFact(&RoutingAttemptFact{
		EventID:           routingFactEventID(meta.DecisionID, RoutingFactOutcomeDecision, 0, meta.EffectiveGroupID),
		RoutingDecisionID: meta.DecisionID, APIKeyID: positiveInt64Ptr(meta.APIKeyID), RouteVersion: meta.RouteVersion,
		InitialGroupID: positiveInt64Ptr(meta.InitialGroupID), SelectedGroupID: selectedGroupID,
		ScheduleMode: meta.ScheduleMode, SmartPreference: cloneStringPtr(meta.SmartPreference), AttemptIndex: 0,
		SmartBalanceBPS: cloneIntPtr(meta.SmartBalanceBPS), RoutingMinSuccessRate: meta.RoutingMinSuccessRate, RoutingStateVersion: meta.RoutingStateVersion,
		Platform: meta.Platform, ModelFamily: modelFamily, EndpointKind: endpointKind,
		StrategyVersion: strategyVersion, ScoreVersion: scoreVersion, FeatureSchemaVersion: featureVersion,
		ModelVersion: cloneStringPtr(meta.ModelVersion), ExperimentID: cloneStringPtr(meta.ExperimentID),
		ExperimentBucket: cloneIntPtr(meta.ExperimentBucket), SampleProbability: 1,
		AssignmentReason: routingAssignmentReason(meta.AssignmentReason), Candidates: cloneAPIKeyRoutingDecisionCandidates(meta.Candidates),
		SelectedReason: optionalStringPtr("ranked_top_candidate"), OutcomeVisibility: RoutingOutcomeUnobserved,
		OutcomeCategory: optionalStringPtr(RoutingFactOutcomeDecision), EventPriority: RoutingEventPriorityDiagnostic, OccurredAt: time.Now(),
	})
}

// RecordAPIKeyRoutingTerminalFailure closes an otherwise incomplete decision
// chain when all configured candidates have been exhausted before any billable
// success. The deterministic event ID makes repeated terminal paths idempotent.
func RecordAPIKeyRoutingTerminalFailure(ctx context.Context, modelFamily, endpointKind string) {
	meta, ok := APIKeyRoutingUsageContextFromContext(ctx)
	if !ok || meta.APIKeyID <= 0 {
		return
	}
	strategyVersion, scoreVersion, featureVersion := routingFactVersions(meta)
	fact := &RoutingAttemptFact{
		EventID:           routingFactEventID(meta.DecisionID, RoutingFactOutcomeAllCandidatesFailed, meta.SwitchCount, meta.EffectiveGroupID),
		RoutingDecisionID: meta.DecisionID, APIKeyID: positiveInt64Ptr(meta.APIKeyID), RouteVersion: meta.RouteVersion,
		InitialGroupID: positiveInt64Ptr(meta.InitialGroupID), AttemptedGroupID: positiveInt64Ptr(meta.EffectiveGroupID),
		SelectedGroupID: positiveInt64Ptr(meta.EffectiveGroupID), ScheduleMode: meta.ScheduleMode,
		SmartPreference: cloneStringPtr(meta.SmartPreference), AttemptIndex: minInt(meta.SwitchCount, DefaultMaxAPIKeyGroupRoutes-1),
		SmartBalanceBPS: cloneIntPtr(meta.SmartBalanceBPS), RoutingMinSuccessRate: meta.RoutingMinSuccessRate, RoutingStateVersion: meta.RoutingStateVersion,
		Platform: meta.Platform, ModelFamily: modelFamily, EndpointKind: endpointKind,
		StrategyVersion: strategyVersion, ScoreVersion: scoreVersion, FeatureSchemaVersion: featureVersion,
		ModelVersion: cloneStringPtr(meta.ModelVersion), ExperimentID: cloneStringPtr(meta.ExperimentID),
		ExperimentBucket: cloneIntPtr(meta.ExperimentBucket), SampleProbability: 1,
		AssignmentReason: routingAssignmentReason(meta.AssignmentReason), Candidates: cloneAPIKeyRoutingDecisionCandidates(meta.Candidates),
		OutcomeVisibility: RoutingOutcomeObserved, OutcomeCategory: optionalStringPtr(RoutingFactOutcomeAllCandidatesFailed),
		Retryable: false, SemanticOutput: false, SwitchedGroup: meta.SwitchCount > 0,
		EventPriority: RoutingEventPriorityCritical, OccurredAt: time.Now(),
	}
	emitRoutingFact(fact)
	logAPIKeyRoutingOutcome(ctx, fact)
}

func logAPIKeyRoutingOutcome(ctx context.Context, fact *RoutingAttemptFact) {
	if fact == nil || fact.APIKeyID == nil || fact.OutcomeCategory == nil {
		return
	}
	attributes := []any{
		"routing_decision_id", fact.RoutingDecisionID,
		"api_key_id", *fact.APIKeyID,
		"route_version", fact.RouteVersion,
		"schedule_mode", fact.ScheduleMode,
		"platform", fact.Platform,
		"model_family", fact.ModelFamily,
		"endpoint_kind", fact.EndpointKind,
		"switch_count", fact.AttemptIndex,
		"outcome", *fact.OutcomeCategory,
		"sticky_broken", fact.StickyBroken,
	}
	if fact.RequestID != nil {
		attributes = append(attributes, "request_id", *fact.RequestID)
	}
	if fact.InitialGroupID != nil {
		attributes = append(attributes, "initial_group_id", *fact.InitialGroupID)
	}
	if fact.EffectiveGroupID != nil {
		attributes = append(attributes, "effective_group_id", *fact.EffectiveGroupID)
	}
	if fact.SmartPreference != nil {
		attributes = append(attributes, "smart_preference", *fact.SmartPreference)
	}
	slog.InfoContext(ctx, "API key routing decision finalized", attributes...)
}

func routingFactVersions(meta APIKeyRoutingUsageContext) (strategy, score, feature string) {
	strategy, score, feature = meta.StrategyVersion, meta.ScoreVersion, meta.FeatureVersion
	if strategy == "" {
		strategy = "sequential-v1"
	}
	if score == "" {
		score = "none"
	}
	if feature == "" {
		feature = "routing-facts-v1"
	}
	return strategy, score, feature
}

func routingFactEventID(decisionID, category string, attempt int, groupID int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%d", decisionID, category, attempt, groupID)))
	return fmt.Sprintf("%x", sum[:])
}

func routingAssignmentReason(value string) string {
	if oneOf(value, RoutingAssignmentCanary, RoutingAssignmentExploration) {
		return value
	}
	return RoutingAssignmentDeterministic
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
