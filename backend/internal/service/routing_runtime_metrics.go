package service

import (
	"math"
	"sync/atomic"
	"time"
)

const (
	RoutingLatencyPhaseAuthLookup = "auth_lookup"
	RoutingLatencyPhasePlanBuild  = "plan_build"
	RoutingLatencyPhaseStateRead  = "state_read"
	RoutingLatencyPhaseStateWrite = "state_write"
)

var routingLatencyUpperBounds = [...]time.Duration{
	100 * time.Nanosecond,
	250 * time.Nanosecond,
	500 * time.Nanosecond,
	time.Microsecond,
	2 * time.Microsecond,
	5 * time.Microsecond,
	10 * time.Microsecond,
	25 * time.Microsecond,
	50 * time.Microsecond,
	100 * time.Microsecond,
	250 * time.Microsecond,
	500 * time.Microsecond,
	time.Millisecond,
	2 * time.Millisecond,
	5 * time.Millisecond,
	10 * time.Millisecond,
	25 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	250 * time.Millisecond,
	500 * time.Millisecond,
	time.Second,
	2500 * time.Millisecond,
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
	60 * time.Second,
}

type routingLatencyHistogram struct {
	buckets  [len(routingLatencyUpperBounds)]atomic.Uint64
	maxNanos atomic.Uint64
}

type RoutingLatencyQuantiles struct {
	Samples uint64  `json:"samples"`
	P50MS   float64 `json:"p50_ms"`
	P95MS   float64 `json:"p95_ms"`
	P99MS   float64 `json:"p99_ms"`
	MaxMS   float64 `json:"max_ms"`
}

// RoutingRuntimeMetrics intentionally has no dynamic labels. Per-group and
// per-request drill-down belongs in bounded routing facts; these counters stay
// safe for process health polling regardless of key/group cardinality.
type RoutingRuntimeMetrics struct {
	plans              atomic.Uint64
	excludedCandidates atomic.Uint64
	candidateBuckets   [DefaultMaxAPIKeyGroupRoutes + 1]atomic.Uint64
	switches           atomic.Uint64
	stickyBreaks       atomic.Uint64
	stickyHits         atomic.Uint64
	stickyMisses       atomic.Uint64
	stickyErrors       atomic.Uint64
	stickyBinds        atomic.Uint64
	breakerClosed      atomic.Uint64
	breakerOpen        atomic.Uint64
	breakerHalfOpen    atomic.Uint64
	breakerRecovering  atomic.Uint64
	halfOpenProbes     atomic.Uint64
	redisDegraded      atomic.Uint64
	authLookupLatency  routingLatencyHistogram
	planBuildLatency   routingLatencyHistogram
	stateReadLatency   routingLatencyHistogram
	stateWriteLatency  routingLatencyHistogram
}

type RoutingRuntimeMetricsSnapshot struct {
	Plans                 uint64                             `json:"plans"`
	CandidateCountBuckets map[int]uint64                     `json:"candidate_count_buckets"`
	ExcludedCandidates    uint64                             `json:"excluded_candidates"`
	GroupSwitches         uint64                             `json:"group_switches"`
	StickyBreaks          uint64                             `json:"sticky_breaks"`
	StickyCacheHits       uint64                             `json:"sticky_cache_hits"`
	StickyCacheMisses     uint64                             `json:"sticky_cache_misses"`
	StickyCacheErrors     uint64                             `json:"sticky_cache_errors"`
	StickyBinds           uint64                             `json:"sticky_binds"`
	BreakerClosed         uint64                             `json:"breaker_closed"`
	BreakerOpen           uint64                             `json:"breaker_open"`
	BreakerHalfOpen       uint64                             `json:"breaker_half_open"`
	BreakerRecovering     uint64                             `json:"breaker_recovering"`
	HalfOpenProbes        uint64                             `json:"half_open_probes"`
	RedisDegraded         uint64                             `json:"redis_degraded"`
	PhaseLatency          map[string]RoutingLatencyQuantiles `json:"phase_latency"`
	FactRecorder          RoutingFactRecorderStats           `json:"fact_recorder"`
}

var defaultRoutingRuntimeMetrics = &RoutingRuntimeMetrics{}

func DefaultRoutingRuntimeMetrics() *RoutingRuntimeMetrics { return defaultRoutingRuntimeMetrics }

func (m *RoutingRuntimeMetrics) RecordPlan(candidateCount, excludedCount int) {
	if m == nil {
		return
	}
	if candidateCount < 0 {
		candidateCount = 0
	}
	if candidateCount > DefaultMaxAPIKeyGroupRoutes {
		candidateCount = DefaultMaxAPIKeyGroupRoutes
	}
	m.plans.Add(1)
	m.candidateBuckets[candidateCount].Add(1)
	if excludedCount > 0 {
		m.excludedCandidates.Add(uint64(excludedCount))
	}
}

func (m *RoutingRuntimeMetrics) RecordSwitch(stickyBroken bool) {
	if m == nil {
		return
	}
	m.switches.Add(1)
	if stickyBroken {
		m.stickyBreaks.Add(1)
	}
}

func (m *RoutingRuntimeMetrics) RecordSticky(result string) {
	if m == nil {
		return
	}
	switch result {
	case "hit":
		m.stickyHits.Add(1)
	case "miss":
		m.stickyMisses.Add(1)
	case "bind":
		m.stickyBinds.Add(1)
	default:
		m.stickyErrors.Add(1)
	}
}

func (m *RoutingRuntimeMetrics) RecordBreaker(state string, probe, degraded bool) {
	if m == nil {
		return
	}
	if degraded {
		m.redisDegraded.Add(1)
	}
	if probe {
		m.halfOpenProbes.Add(1)
	}
	switch state {
	case APIKeyRouteBreakerOpen:
		m.breakerOpen.Add(1)
	case APIKeyRouteBreakerHalfOpen:
		m.breakerHalfOpen.Add(1)
	case APIKeyRouteBreakerRecovering:
		m.breakerRecovering.Add(1)
	default:
		m.breakerClosed.Add(1)
	}
}

// RecordPhaseLatency accepts only a fixed set of process-wide phase names, so
// the admin metric remains bounded regardless of API-key/group cardinality.
func (m *RoutingRuntimeMetrics) RecordPhaseLatency(phase string, duration time.Duration) {
	if m == nil {
		return
	}
	var histogram *routingLatencyHistogram
	switch phase {
	case RoutingLatencyPhaseAuthLookup:
		histogram = &m.authLookupLatency
	case RoutingLatencyPhasePlanBuild:
		histogram = &m.planBuildLatency
	case RoutingLatencyPhaseStateRead:
		histogram = &m.stateReadLatency
	case RoutingLatencyPhaseStateWrite:
		histogram = &m.stateWriteLatency
	default:
		return
	}
	histogram.record(duration)
}

func (h *routingLatencyHistogram) record(duration time.Duration) {
	if h == nil {
		return
	}
	if duration < 0 {
		duration = 0
	}
	index := len(routingLatencyUpperBounds) - 1
	for candidate, upper := range routingLatencyUpperBounds {
		if duration <= upper {
			index = candidate
			break
		}
	}
	h.buckets[index].Add(1)
	nanos := uint64(duration)
	for current := h.maxNanos.Load(); nanos > current; current = h.maxNanos.Load() {
		if h.maxNanos.CompareAndSwap(current, nanos) {
			break
		}
	}
}

func (h *routingLatencyHistogram) snapshot() RoutingLatencyQuantiles {
	if h == nil {
		return RoutingLatencyQuantiles{}
	}
	counts := make([]uint64, len(h.buckets))
	var total uint64
	for index := range h.buckets {
		counts[index] = h.buckets[index].Load()
		total += counts[index]
	}
	return RoutingLatencyQuantiles{
		Samples: total,
		P50MS:   routingLatencyQuantileMS(counts, total, 0.50),
		P95MS:   routingLatencyQuantileMS(counts, total, 0.95),
		P99MS:   routingLatencyQuantileMS(counts, total, 0.99),
		MaxMS:   float64(h.maxNanos.Load()) / float64(time.Millisecond),
	}
}

func routingLatencyQuantileMS(counts []uint64, total uint64, quantile float64) float64 {
	if total == 0 || len(counts) == 0 {
		return 0
	}
	target := uint64(math.Ceil(float64(total) * quantile))
	if target == 0 {
		target = 1
	}
	var cumulative uint64
	for index, count := range counts {
		cumulative += count
		if cumulative >= target {
			if index >= len(routingLatencyUpperBounds) {
				index = len(routingLatencyUpperBounds) - 1
			}
			return float64(routingLatencyUpperBounds[index]) / float64(time.Millisecond)
		}
	}
	return float64(routingLatencyUpperBounds[len(routingLatencyUpperBounds)-1]) / float64(time.Millisecond)
}

func (m *RoutingRuntimeMetrics) Snapshot() RoutingRuntimeMetricsSnapshot {
	snapshot := RoutingRuntimeMetricsSnapshot{
		CandidateCountBuckets: make(map[int]uint64, DefaultMaxAPIKeyGroupRoutes+1),
		PhaseLatency:          make(map[string]RoutingLatencyQuantiles, 4),
	}
	if m == nil {
		return snapshot
	}
	snapshot.Plans = m.plans.Load()
	for index := range m.candidateBuckets {
		snapshot.CandidateCountBuckets[index] = m.candidateBuckets[index].Load()
	}
	snapshot.ExcludedCandidates = m.excludedCandidates.Load()
	snapshot.GroupSwitches = m.switches.Load()
	snapshot.StickyBreaks = m.stickyBreaks.Load()
	snapshot.StickyCacheHits = m.stickyHits.Load()
	snapshot.StickyCacheMisses = m.stickyMisses.Load()
	snapshot.StickyCacheErrors = m.stickyErrors.Load()
	snapshot.StickyBinds = m.stickyBinds.Load()
	snapshot.BreakerClosed = m.breakerClosed.Load()
	snapshot.BreakerOpen = m.breakerOpen.Load()
	snapshot.BreakerHalfOpen = m.breakerHalfOpen.Load()
	snapshot.BreakerRecovering = m.breakerRecovering.Load()
	snapshot.HalfOpenProbes = m.halfOpenProbes.Load()
	snapshot.RedisDegraded = m.redisDegraded.Load()
	snapshot.PhaseLatency[RoutingLatencyPhaseAuthLookup] = m.authLookupLatency.snapshot()
	snapshot.PhaseLatency[RoutingLatencyPhasePlanBuild] = m.planBuildLatency.snapshot()
	snapshot.PhaseLatency[RoutingLatencyPhaseStateRead] = m.stateReadLatency.snapshot()
	snapshot.PhaseLatency[RoutingLatencyPhaseStateWrite] = m.stateWriteLatency.snapshot()
	defaultRoutingFactSink.RLock()
	if recorder, ok := defaultRoutingFactSink.sink.(*RoutingFactRecorder); ok {
		snapshot.FactRecorder = recorder.Stats()
	}
	defaultRoutingFactSink.RUnlock()
	return snapshot
}
