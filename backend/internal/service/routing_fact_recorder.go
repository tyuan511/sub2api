package service

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var ErrRoutingFactStreamUnavailable = errors.New("routing fact stream unavailable")

const (
	routingFactSampleRetention     = 30 * 24 * time.Hour
	routingFactDiagnosticRetention = 90 * 24 * time.Hour
	routingFactCriticalRetention   = 180 * 24 * time.Hour
	routingFactPruneBatch          = 5000
	routingFactHighQueueSize       = 2048
	routingFactNormalQueueSize     = 8192
)

type RoutingFactStreamEntry struct {
	ID      string
	Payload []byte
}

type RoutingFactStream interface {
	Publish(ctx context.Context, payload []byte) error
	Read(ctx context.Context, consumer string, count int64, block time.Duration) ([]RoutingFactStreamEntry, error)
	Ack(ctx context.Context, ids ...string) error
}

type RoutingFactRecorderStats struct {
	Queued          uint64
	Persisted       uint64
	StreamFallbacks uint64
	DroppedSamples  uint64
	DroppedCritical uint64
	Invalid         uint64
}

// RoutingFactRecorder keeps request handlers off Redis and PostgreSQL. Redis
// Streams are the normal multi-instance buffer; a bounded in-process queue and
// direct idempotent PostgreSQL batches are the explicit Redis outage fallback.
type RoutingFactRecorder struct {
	repo       RoutingFactRepository
	stream     RoutingFactStream
	consumer   string
	sampleRate float64
	high       chan *RoutingAttemptFact
	normal     chan *RoutingAttemptFact
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup

	queued          atomic.Uint64
	persisted       atomic.Uint64
	streamFallbacks atomic.Uint64
	droppedSamples  atomic.Uint64
	droppedCritical atomic.Uint64
	invalid         atomic.Uint64
}

func NewRoutingFactRecorder(repo RoutingFactRepository, stream RoutingFactStream, sampleRate float64) *RoutingFactRecorder {
	// Zero disables ordinary optimization samples, not mandatory health facts.
	if sampleRate < 0 || sampleRate > 1 {
		sampleRate = 0.01
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &RoutingFactRecorder{
		repo: repo, stream: stream, consumer: "routing-facts-" + uuidSuffix(), sampleRate: sampleRate,
		high: make(chan *RoutingAttemptFact, routingFactHighQueueSize), normal: make(chan *RoutingAttemptFact, routingFactNormalQueueSize), ctx: ctx, cancel: cancel,
	}
}

func (r *RoutingFactRecorder) Start() {
	if r == nil || r.repo == nil {
		return
	}
	SetDefaultRoutingFactSink(r)
	r.wg.Add(1)
	go r.runPublisher()
	if r.stream != nil {
		r.wg.Add(1)
		go r.runConsumer()
	}
	if _, ok := r.repo.(RoutingFactRetentionRepository); ok {
		r.wg.Add(1)
		go r.runRetention()
	}
}

func (r *RoutingFactRecorder) runRetention() {
	defer r.wg.Done()
	pruner := r.repo.(RoutingFactRetentionRepository)
	prune := func() {
		ctx, cancel := context.WithTimeout(r.ctx, 15*time.Second)
		now := time.Now().UTC()
		_, _ = pruner.PruneRoutingAttempts(
			ctx,
			now.Add(-routingFactSampleRetention),
			now.Add(-routingFactDiagnosticRetention),
			now.Add(-routingFactCriticalRetention),
			routingFactPruneBatch,
		)
		cancel()
	}
	prune()
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			prune()
		}
	}
}

func (r *RoutingFactRecorder) Stop() {
	if r == nil || r.cancel == nil {
		return
	}
	SetDefaultRoutingFactSink(nil)
	r.cancel()
	r.wg.Wait()
}

func (r *RoutingFactRecorder) Stats() RoutingFactRecorderStats {
	if r == nil {
		return RoutingFactRecorderStats{}
	}
	return RoutingFactRecorderStats{
		Queued: r.queued.Load(), Persisted: r.persisted.Load(), StreamFallbacks: r.streamFallbacks.Load(),
		DroppedSamples: r.droppedSamples.Load(), DroppedCritical: r.droppedCritical.Load(), Invalid: r.invalid.Load(),
	}
}

func (r *RoutingFactRecorder) RecordRoutingFact(fact *RoutingAttemptFact) {
	if r == nil || fact == nil {
		return
	}
	highPriority := fact.SwitchedGroup || fact.CacheColdDueToFailover || fact.EventPriority == RoutingEventPriorityCritical ||
		fact.AssignmentReason == RoutingAssignmentShadow || fact.AssignmentReason == RoutingAssignmentCanary ||
		fact.ExperimentID != nil ||
		(fact.OutcomeCategory != nil && *fact.OutcomeCategory != RoutingFactOutcomeSuccess && *fact.OutcomeCategory != RoutingFactOutcomeDecision)
	if !highPriority {
		if !stableRoutingFactSample(fact.RoutingDecisionID, r.sampleRate) {
			return
		}
		fact.SampleProbability = r.sampleRate
		fact.EventPriority = RoutingEventPrioritySample
	}
	if err := ValidateRoutingAttemptFact(fact); err != nil {
		r.invalid.Add(1)
		return
	}
	fact = cloneRoutingAttemptFact(fact)
	if highPriority {
		select {
		case r.high <- fact:
			r.queued.Add(1)
		default:
			r.droppedCritical.Add(1)
		}
		return
	}
	select {
	case r.normal <- fact:
		r.queued.Add(1)
	default:
		r.droppedSamples.Add(1)
	}
}

func (r *RoutingFactRecorder) runPublisher() {
	defer r.wg.Done()
	for {
		fact, ok := r.nextFact()
		if !ok {
			return
		}
		payload, err := json.Marshal(fact)
		if err != nil {
			r.invalid.Add(1)
			continue
		}
		if r.stream != nil {
			publishCtx, cancel := context.WithTimeout(r.ctx, 750*time.Millisecond)
			err = r.stream.Publish(publishCtx, payload)
			cancel()
			if err == nil {
				continue
			}
			r.streamFallbacks.Add(1)
		}
		r.persistDirect(fact)
	}
}

func (r *RoutingFactRecorder) nextFact() (*RoutingAttemptFact, bool) {
	select {
	case fact := <-r.high:
		return fact, true
	default:
	}
	select {
	case <-r.ctx.Done():
		return nil, false
	case fact := <-r.high:
		return fact, true
	case fact := <-r.normal:
		return fact, true
	}
}

func (r *RoutingFactRecorder) persistDirect(fact *RoutingAttemptFact) {
	for attempt := 0; attempt < 3; attempt++ {
		ctx, cancel := context.WithTimeout(r.ctx, 2*time.Second)
		err := r.repo.CreateRoutingAttempts(ctx, []*RoutingAttemptFact{fact})
		cancel()
		if err == nil {
			r.persisted.Add(1)
			return
		}
		if r.ctx.Err() != nil {
			return
		}
		time.Sleep(time.Duration(attempt+1) * 25 * time.Millisecond)
	}
	if fact.EventPriority == RoutingEventPrioritySample {
		r.droppedSamples.Add(1)
	} else {
		r.droppedCritical.Add(1)
	}
}

func (r *RoutingFactRecorder) runConsumer() {
	defer r.wg.Done()
	for r.ctx.Err() == nil {
		entries, err := r.stream.Read(r.ctx, r.consumer, 128, time.Second)
		if err != nil {
			if r.ctx.Err() != nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if len(entries) == 0 {
			continue
		}
		facts := make([]*RoutingAttemptFact, 0, len(entries))
		ackIDs := make([]string, 0, len(entries))
		for _, entry := range entries {
			var fact RoutingAttemptFact
			if json.Unmarshal(entry.Payload, &fact) != nil || ValidateRoutingAttemptFact(&fact) != nil {
				r.invalid.Add(1)
				ackIDs = append(ackIDs, entry.ID)
				continue
			}
			facts = append(facts, &fact)
			ackIDs = append(ackIDs, entry.ID)
		}
		if len(facts) > 0 {
			ctx, cancel := context.WithTimeout(r.ctx, 3*time.Second)
			err = r.repo.CreateRoutingAttempts(ctx, facts)
			cancel()
			if err != nil {
				continue
			}
			r.persisted.Add(uint64(len(facts)))
		}
		if len(ackIDs) > 0 {
			ctx, cancel := context.WithTimeout(r.ctx, time.Second)
			_ = r.stream.Ack(ctx, ackIDs...)
			cancel()
		}
	}
}

func stableRoutingFactSample(decisionID string, probability float64) bool {
	if probability >= 1 {
		return true
	}
	sum := sha256.Sum256([]byte(decisionID))
	bucket := float64(binary.BigEndian.Uint64(sum[:8])) / float64(^uint64(0))
	return bucket < probability
}

func cloneRoutingAttemptFact(fact *RoutingAttemptFact) *RoutingAttemptFact {
	if fact == nil {
		return nil
	}
	copy := *fact
	copy.Candidates = cloneAPIKeyRoutingDecisionCandidates(fact.Candidates)
	copy.ActualUsage = append(json.RawMessage(nil), fact.ActualUsage...)
	copy.BillableUsage = append(json.RawMessage(nil), fact.BillableUsage...)
	return &copy
}

func uuidSuffix() string {
	// A random suffix is only a Redis consumer identity and is never persisted as
	// a decision fact. Reusing the existing request-ID generator avoids secrets.
	return generateRequestID()
}
