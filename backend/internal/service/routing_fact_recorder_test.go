package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type routingFactRepoStub struct {
	mu       sync.Mutex
	facts    []*RoutingAttemptFact
	attempts int
	err      error
	dedupe   bool
	notify   chan struct{}
}

func (s *routingFactRepoStub) CreateRoutingAttempts(_ context.Context, facts []*RoutingAttemptFact) error {
	s.mu.Lock()
	s.attempts++
	if s.err == nil {
		for _, fact := range facts {
			if s.dedupe && s.hasEventLocked(fact.EventID) {
				continue
			}
			s.facts = append(s.facts, fact)
		}
	}
	err := s.err
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
	return err
}

func (s *routingFactRepoStub) hasEventLocked(eventID string) bool {
	for _, fact := range s.facts {
		if fact != nil && fact.EventID == eventID {
			return true
		}
	}
	return false
}

type unavailableRoutingFactStream struct{}

func (unavailableRoutingFactStream) Publish(context.Context, []byte) error {
	return ErrRoutingFactStreamUnavailable
}
func (unavailableRoutingFactStream) Read(ctx context.Context, _ string, _ int64, _ time.Duration) ([]RoutingFactStreamEntry, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(10 * time.Millisecond):
		return nil, errors.New("redis unavailable")
	}
}
func (unavailableRoutingFactStream) Ack(context.Context, ...string) error { return nil }

type replayRoutingFactStream struct {
	mu            sync.Mutex
	entry         RoutingFactStreamEntry
	acked         bool
	ackFailures   int
	maxDeliveries int
	deliveries    int
	readNotify    chan struct{}
	ackNotify     chan struct{}
}

func (s *replayRoutingFactStream) Publish(context.Context, []byte) error { return nil }

func (s *replayRoutingFactStream) Read(ctx context.Context, _ string, _ int64, _ time.Duration) ([]RoutingFactStreamEntry, error) {
	s.mu.Lock()
	if !s.acked && (s.maxDeliveries <= 0 || s.deliveries < s.maxDeliveries) {
		s.deliveries++
		entry := s.entry
		s.mu.Unlock()
		select {
		case s.readNotify <- struct{}{}:
		default:
		}
		return []RoutingFactStreamEntry{entry}, nil
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Millisecond):
		return nil, nil
	}
}

func (s *replayRoutingFactStream) Ack(_ context.Context, _ ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ackFailures > 0 {
		s.ackFailures--
		return errors.New("ack failed")
	}
	s.acked = true
	select {
	case s.ackNotify <- struct{}{}:
	default:
	}
	return nil
}

func TestRoutingFactRecorderRedisFailureUsesBoundedDBFallback(t *testing.T) {
	repo := &routingFactRepoStub{notify: make(chan struct{}, 1)}
	recorder := NewRoutingFactRecorder(repo, unavailableRoutingFactStream{}, 1)
	recorder.Start()
	defer recorder.Stop()

	fact := validRoutingAttemptFactForTest()
	fact.SwitchedGroup = true
	recorder.RecordRoutingFact(fact)

	select {
	case <-repo.notify:
	case <-time.After(2 * time.Second):
		t.Fatal("routing fact was not persisted through Redis fallback")
	}
	stats := recorder.Stats()
	require.Equal(t, uint64(1), stats.Queued)
	require.Equal(t, uint64(1), stats.Persisted)
	require.Equal(t, uint64(1), stats.StreamFallbacks)
}

func TestStableRoutingFactSampleIsDeterministic(t *testing.T) {
	first := stableRoutingFactSample("decision-stable", 0.37)
	for i := 0; i < 20; i++ {
		require.Equal(t, first, stableRoutingFactSample("decision-stable", 0.37))
	}
	require.True(t, stableRoutingFactSample("decision-stable", 1))
	require.False(t, stableRoutingFactSample("decision-stable", 0))
}

func TestRoutingFactRecorderWithOptimizationSamplingDisabledKeepsHealthFailures(t *testing.T) {
	recorder := NewRoutingFactRecorder(&routingFactRepoStub{}, nil, 0)
	defer recorder.Stop()
	normal := validRoutingAttemptFactForTest()
	recorder.RecordRoutingFact(normal)
	decision := validRoutingAttemptFactForTest()
	decision.OutcomeCategory = optionalStringPtr(RoutingFactOutcomeDecision)
	decision.OutcomeVisibility = RoutingOutcomeUnobserved
	decision.EventPriority = RoutingEventPriorityDiagnostic
	recorder.RecordRoutingFact(decision)
	require.Zero(t, recorder.Stats().Queued)
	failure := validRoutingAttemptFactForTest()
	outcome := "route_attempt_failed"
	failure.OutcomeCategory = &outcome
	recorder.RecordRoutingFact(failure)
	require.EqualValues(t, 1, recorder.Stats().Queued)
	require.Len(t, recorder.high, 1)
	require.Empty(t, recorder.normal)
}

func TestRoutingFactRecorderSamplesDecisionAndOutcomeWithSameProbability(t *testing.T) {
	recorder := NewRoutingFactRecorder(&routingFactRepoStub{}, nil, .25)
	defer recorder.Stop()
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("decision-%d", i)
		decision := validRoutingAttemptFactForTest()
		decision.RoutingDecisionID = id
		decision.OutcomeCategory = optionalStringPtr(RoutingFactOutcomeDecision)
		decision.OutcomeVisibility = RoutingOutcomeUnobserved
		decision.EventPriority = RoutingEventPriorityDiagnostic
		recorder.RecordRoutingFact(decision)
		outcome := validRoutingAttemptFactForTest()
		outcome.RoutingDecisionID = id
		outcome.OutcomeCategory = optionalStringPtr(RoutingFactOutcomeSuccess)
		recorder.RecordRoutingFact(outcome)
	}
	require.Empty(t, recorder.high, "ordinary smart decisions are not health failures")
	require.NotEmpty(t, recorder.normal)
	require.Less(t, len(recorder.normal), 100)
	for len(recorder.normal) > 0 {
		decision, outcome := <-recorder.normal, <-recorder.normal
		require.Equal(t, decision.RoutingDecisionID, outcome.RoutingDecisionID)
		require.Equal(t, .25, decision.SampleProbability)
		require.Equal(t, .25, outcome.SampleProbability)
	}
}

func TestRoutingFactRecorderQueuesCriticalFactsAheadOfOrdinarySamples(t *testing.T) {
	repo := &routingFactRepoStub{notify: make(chan struct{}, 2)}
	recorder := NewRoutingFactRecorder(repo, nil, 1)
	normal := validRoutingAttemptFactForTest()
	normal.EventID = "normal-event"
	normal.RoutingDecisionID = "normal-decision"
	critical := validRoutingAttemptFactForTest()
	critical.EventID = "critical-event"
	critical.RoutingDecisionID = "critical-decision"
	critical.EventPriority = RoutingEventPriorityCritical
	recorder.RecordRoutingFact(normal)
	recorder.RecordRoutingFact(critical)
	recorder.Start()
	defer recorder.Stop()

	for i := 0; i < 2; i++ {
		select {
		case <-repo.notify:
		case <-time.After(2 * time.Second):
			t.Fatal("routing facts were not persisted")
		}
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Len(t, repo.facts, 2)
	require.Equal(t, "critical-event", repo.facts[0].EventID)
	require.Equal(t, "normal-event", repo.facts[1].EventID)
}

func TestRoutingFactRecorderQueueSaturationDropsSamplesBeforeCriticalFacts(t *testing.T) {
	recorder := NewRoutingFactRecorder(&routingFactRepoStub{}, nil, 1)
	for i := 0; i < routingFactNormalQueueSize; i++ {
		fact := validRoutingAttemptFactForTest()
		fact.EventID = "normal"
		fact.RoutingDecisionID = "normal"
		recorder.RecordRoutingFact(fact)
	}
	overflowSample := validRoutingAttemptFactForTest()
	overflowSample.EventID = "normal-overflow"
	overflowSample.RoutingDecisionID = "normal-overflow"
	recorder.RecordRoutingFact(overflowSample)

	critical := validRoutingAttemptFactForTest()
	critical.EventPriority = RoutingEventPriorityCritical
	for i := 0; i < routingFactHighQueueSize; i++ {
		recorder.RecordRoutingFact(critical)
	}
	recorder.RecordRoutingFact(critical)

	stats := recorder.Stats()
	require.Equal(t, uint64(routingFactNormalQueueSize+routingFactHighQueueSize), stats.Queued)
	require.Equal(t, uint64(1), stats.DroppedSamples)
	require.Equal(t, uint64(1), stats.DroppedCritical)
}

func TestRoutingFactConsumerRestartReclaimsUnackedEntry(t *testing.T) {
	fact := validRoutingAttemptFactForTest()
	fact.EventID = "restart-event"
	payload, err := json.Marshal(fact)
	require.NoError(t, err)
	stream := &replayRoutingFactStream{
		entry:      RoutingFactStreamEntry{ID: "1-0", Payload: payload},
		readNotify: make(chan struct{}, 2), ackNotify: make(chan struct{}, 1),
	}

	failingRepo := &routingFactRepoStub{err: errors.New("postgres unavailable"), notify: make(chan struct{}, 1)}
	first := NewRoutingFactRecorder(failingRepo, stream, 1)
	first.Start()
	select {
	case <-failingRepo.notify:
	case <-time.After(2 * time.Second):
		t.Fatal("first consumer did not attempt persistence")
	}
	first.Stop()
	stream.mu.Lock()
	require.False(t, stream.acked)
	stream.mu.Unlock()

	successRepo := &routingFactRepoStub{notify: make(chan struct{}, 1)}
	second := NewRoutingFactRecorder(successRepo, stream, 1)
	second.Start()
	defer second.Stop()
	select {
	case <-stream.ackNotify:
	case <-time.After(2 * time.Second):
		t.Fatal("replacement consumer did not persist and ack the pending event")
	}
	successRepo.mu.Lock()
	require.Len(t, successRepo.facts, 1)
	require.Equal(t, "restart-event", successRepo.facts[0].EventID)
	successRepo.mu.Unlock()
}

func TestRoutingFactAckFailureRedeliveryIsIdempotent(t *testing.T) {
	fact := validRoutingAttemptFactForTest()
	fact.EventID = "redelivered-event"
	payload, err := json.Marshal(fact)
	require.NoError(t, err)
	stream := &replayRoutingFactStream{
		entry: RoutingFactStreamEntry{ID: "2-0", Payload: payload}, ackFailures: 1, maxDeliveries: 2,
		readNotify: make(chan struct{}, 2), ackNotify: make(chan struct{}, 1),
	}
	repo := &routingFactRepoStub{dedupe: true, notify: make(chan struct{}, 2)}
	recorder := NewRoutingFactRecorder(repo, stream, 1)
	recorder.Start()
	defer recorder.Stop()

	select {
	case <-stream.ackNotify:
	case <-time.After(2 * time.Second):
		t.Fatal("redelivered event was not eventually acked")
	}
	repo.mu.Lock()
	require.Equal(t, 2, repo.attempts)
	require.Len(t, repo.facts, 1)
	require.Equal(t, "redelivered-event", repo.facts[0].EventID)
	repo.mu.Unlock()
}

func TestRoutingFactBackpressureDoesNotBlockFinancialUsagePersistence(t *testing.T) {
	recorder := NewRoutingFactRecorder(&routingFactRepoStub{}, nil, 1)
	critical := validRoutingAttemptFactForTest()
	critical.EventPriority = RoutingEventPriorityCritical
	for i := 0; i < routingFactHighQueueSize; i++ {
		recorder.RecordRoutingFact(critical)
	}
	recorder.RecordRoutingFact(critical)
	require.Equal(t, uint64(1), recorder.Stats().DroppedCritical)

	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	writeUsageLogBestEffort(context.Background(), usageRepo, &UsageLog{RequestID: "financial-usage", APIKeyID: 1}, "test")
	require.Equal(t, 1, usageRepo.calls)
	require.Equal(t, "financial-usage", usageRepo.lastLog.RequestID)
}
