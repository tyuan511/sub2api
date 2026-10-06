package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type routingFactCaptureSink struct{ fact *RoutingAttemptFact }

func (s *routingFactCaptureSink) RecordRoutingFact(fact *RoutingAttemptFact) { s.fact = fact }

type routingFactListSink struct{ facts []*RoutingAttemptFact }

func (s *routingFactListSink) RecordRoutingFact(fact *RoutingAttemptFact) {
	s.facts = append(s.facts, fact)
}

func TestApplyAPIKeyRoutingUsageSeparatesActualAndBillableFacts(t *testing.T) {
	ctx := WithAPIKeyRoutingUsageContext(context.Background(), APIKeyRoutingUsageContext{
		DecisionID: "decision-1", RouteVersion: 7, InitialGroupID: 10, EffectiveGroupID: 20,
		Platform: PlatformAnthropic, ScheduleMode: APIKeyScheduleModeSequential,
		SwitchCount: 1, StrategyVersion: "strategy-3", ScoreVersion: "score-9", FeatureVersion: "features-2", StickyBroken: true,
	})
	groupID := int64(20)
	log := &UsageLog{APIKeyID: 3, GroupID: &groupID, Model: "claude-sonnet-4", RequestID: "req-1", CreatedAt: time.Now()}
	ApplyAPIKeyRoutingUsage(ctx, log,
		RoutingTokenUsage{InputTokens: 100, OutputTokens: 20, CacheCreation5mTokens: 30, CacheCreation1hTokens: 70},
		RoutingTokenUsage{CacheReadTokens: 100, OutputTokens: 20})

	require.Equal(t, int64(10), *log.InitialGroupID)
	require.Equal(t, int64(7), *log.RouteVersion)
	require.Equal(t, 1, log.GroupSwitchCount)
	require.False(t, log.CacheColdDueToFailover)
	require.Zero(t, log.CacheCompensationTokens)
	require.Nil(t, log.CacheCompensationReason)

	var actual, billable RoutingTokenUsage
	require.NoError(t, json.Unmarshal(log.ActualUsage, &actual))
	require.NoError(t, json.Unmarshal(log.BillableUsage, &billable))
	require.Equal(t, 100, actual.InputTokens)
	require.Equal(t, 30, actual.CacheCreation5mTokens)
	require.Equal(t, 70, actual.CacheCreation1hTokens)
	require.Equal(t, 100, billable.CacheReadTokens)

	fact, ok := RoutingFactFromUsage(ctx, log)
	require.True(t, ok)
	require.Equal(t, "strategy-3", fact.StrategyVersion)
	require.Equal(t, "score-9", fact.ScoreVersion)
	require.True(t, fact.SwitchedGroup)
	require.Nil(t, fact.ActionPropensity)
}

func TestEmitAPIKeyRoutingUsageFactUsesFinalCostsAndMarksPartialBillingCritical(t *testing.T) {
	sink := &routingFactCaptureSink{}
	SetDefaultRoutingFactSink(sink)
	defer SetDefaultRoutingFactSink(nil)
	ctx := WithAPIKeyRoutingUsageContext(context.Background(), APIKeyRoutingUsageContext{
		DecisionID: "decision-partial-billing", APIKeyID: 3, RouteVersion: 2,
		InitialGroupID: 10, EffectiveGroupID: 20, Platform: PlatformOpenAI,
		ScheduleMode: APIKeyScheduleModeSequential, SwitchCount: 1,
	})
	groupID := int64(20)
	accountCost := 0.007
	log := &UsageLog{
		APIKeyID: 3, GroupID: &groupID, Model: "gpt-5", RequestID: "req-partial",
		TotalCost: 0.01, ActualCost: 0, AccountStatsCost: &accountCost, CreatedAt: time.Now(),
	}
	EmitAPIKeyRoutingUsageFact(ctx, log, true)

	require.NotNil(t, sink.fact)
	require.Equal(t, RoutingFactOutcomePartialBilling, *sink.fact.OutcomeCategory)
	require.Equal(t, RoutingEventPriorityCritical, sink.fact.EventPriority)
	require.InDelta(t, accountCost, *sink.fact.ActualCost, 1e-12)
	require.Zero(t, *sink.fact.BilledCost)
}

func TestForceCacheBillingInputTokensPreservesLegacyAccountFailover(t *testing.T) {
	legacy := WithForceCacheBilling(context.Background())
	require.Equal(t, 300_000, ForceCacheBillingInputTokens(legacy, 300_000))
}

func TestValidateRoutingAttemptFactRejectsFakeDeterministicPropensity(t *testing.T) {
	propensity := 0.5
	fact := validRoutingAttemptFactForTest()
	fact.ActionPropensity = &propensity
	require.ErrorIs(t, ValidateRoutingAttemptFact(fact), ErrRoutingFactInvalid)
}

func TestValidateRoutingAttemptFactRejectsNonNumericUsageShape(t *testing.T) {
	fact := validRoutingAttemptFactForTest()
	fact.ActualUsage = json.RawMessage(`{"input_tokens":1,"prompt":"secret"}`)
	require.ErrorIs(t, ValidateRoutingAttemptFact(fact), ErrRoutingFactInvalid)
}

func TestValidateRoutingAttemptFactRejectsUnboundedOutcomeText(t *testing.T) {
	fact := validRoutingAttemptFactForTest()
	fact.OutcomeCategory = optionalStringPtr("upstream error contained user prompt")
	require.ErrorIs(t, ValidateRoutingAttemptFact(fact), ErrRoutingFactInvalid)
}

func TestRoutingFactContractIgnoresUnknownFieldsButRejectsMissingCriticalFields(t *testing.T) {
	body, err := json.Marshal(validRoutingAttemptFactForTest())
	require.NoError(t, err)
	var object map[string]any
	require.NoError(t, json.Unmarshal(body, &object))
	object["future_numeric_feature"] = 42.0
	body, err = json.Marshal(object)
	require.NoError(t, err)
	var decoded RoutingAttemptFact
	require.NoError(t, json.Unmarshal(body, &decoded))
	require.NoError(t, ValidateRoutingAttemptFact(&decoded))

	delete(object, "feature_schema_version")
	body, err = json.Marshal(object)
	require.NoError(t, err)
	decoded = RoutingAttemptFact{}
	require.NoError(t, json.Unmarshal(body, &decoded))
	require.ErrorIs(t, ValidateRoutingAttemptFact(&decoded), ErrRoutingFactInvalid)
}

func TestRoutingFactContractRejectsOutOfRangeCriticalCandidateFeature(t *testing.T) {
	fact := validRoutingAttemptFactForTest()
	invalidSuccessRate := 1.2
	fact.Candidates = []APIKeyRoutingDecisionCandidate{{
		GroupID: 2, ConfiguredPriority: 0, Admitted: true,
		SuccessRate: &invalidSuccessRate, OutcomeVisibility: RoutingOutcomeObserved,
	}}
	require.ErrorIs(t, ValidateRoutingAttemptFact(fact), ErrRoutingFactInvalid)
}

func TestRoutingTerminalFailureEventIsIdempotentlyAddressed(t *testing.T) {
	sink := &routingFactListSink{}
	SetDefaultRoutingFactSink(sink)
	defer SetDefaultRoutingFactSink(nil)
	ctx := WithAPIKeyRoutingUsageContext(context.Background(), APIKeyRoutingUsageContext{
		DecisionID: "decision-terminal", APIKeyID: 8, RouteVersion: 4, InitialGroupID: 9, EffectiveGroupID: 10,
		Platform: PlatformOpenAI, ScheduleMode: APIKeyScheduleModeSequential, SwitchCount: 1,
	})
	RecordAPIKeyRoutingTerminalFailure(ctx, "gpt-5", "responses")
	RecordAPIKeyRoutingTerminalFailure(ctx, "gpt-5", "responses")

	require.Len(t, sink.facts, 2)
	require.Equal(t, sink.facts[0].EventID, sink.facts[1].EventID)
	require.Len(t, sink.facts[0].EventID, 64)
	require.Equal(t, RoutingFactOutcomeAllCandidatesFailed, *sink.facts[0].OutcomeCategory)
	require.Equal(t, RoutingEventPriorityCritical, sink.facts[0].EventPriority)
}

func TestRecordAPIKeyRouteFailureEmitsReplayFact(t *testing.T) {
	sink := &routingFactCaptureSink{}
	SetDefaultRoutingFactSink(sink)
	defer SetDefaultRoutingFactSink(nil)
	ctx := WithAPIKeyRoutingUsageContext(context.Background(), APIKeyRoutingUsageContext{
		DecisionID: "decision-failure", RouteVersion: 4, InitialGroupID: 11, EffectiveGroupID: 12,
		Platform: PlatformOpenAI, ScheduleMode: APIKeyScheduleModeSequential, SwitchCount: 1,
		AttemptStartedAt: time.Now().Add(-25 * time.Millisecond),
	})
	state, err := recordAPIKeyRouteResult(ctx, nil, APIKeyRouteHealthPolicy{}, 7, 4, 12, "gpt-5", "/v1/responses", false)
	require.NoError(t, err)
	require.Equal(t, APIKeyRouteBreakerClosed, state)
	require.NotNil(t, sink.fact)
	require.Equal(t, int64(12), *sink.fact.AttemptedGroupID)
	require.Nil(t, sink.fact.EffectiveGroupID)
	require.Equal(t, "route_attempt_failed", *sink.fact.OutcomeCategory)
	require.True(t, sink.fact.Retryable)
	require.NotNil(t, sink.fact.DurationMS)
	require.GreaterOrEqual(t, *sink.fact.DurationMS, 20)
	require.Equal(t, RoutingEventPriorityCritical, sink.fact.EventPriority)
}

func validRoutingAttemptFactForTest() *RoutingAttemptFact {
	groupID := int64(2)
	return &RoutingAttemptFact{
		EventID: "event-1", RoutingDecisionID: "decision-1", APIKeyID: positiveInt64Ptr(1), RouteVersion: 1,
		InitialGroupID: &groupID, EffectiveGroupID: &groupID, SelectedGroupID: &groupID,
		ScheduleMode: APIKeyScheduleModeSequential, AttemptIndex: 0, Platform: PlatformOpenAI,
		ModelFamily: "gpt-5", EndpointKind: "responses", StrategyVersion: "sequential-v1",
		ScoreVersion: "none", FeatureSchemaVersion: "routing-facts-v1", SampleProbability: 1,
		AssignmentReason: RoutingAssignmentDeterministic, OutcomeVisibility: RoutingOutcomeObserved,
		EventPriority: RoutingEventPriorityDiagnostic, OccurredAt: time.Now(),
	}
}
