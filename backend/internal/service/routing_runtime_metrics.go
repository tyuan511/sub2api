package service

import (
	"math"
	"sync/atomic"
	"time"
)

const (
	RoutingLatencyPhaseAuthLookup   = "auth_lookup"
	RoutingLatencyPhasePlanBuild    = "plan_build"
	RoutingLatencyPhaseStateRead    = "state_read"
	RoutingLatencyPhaseSmartRanking = "smart_ranking"
	RoutingLatencyPhaseStateWrite   = "state_write"
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

type RoutingBackgroundQueryMetrics struct {
	Queries         uint64  `json:"queries"`
	Failures        uint64  `json:"failures"`
	ScannedRows     uint64  `json:"scanned_rows"`
	AverageDuration float64 `json:"average_duration_ms"`
	MaxDuration     float64 `json:"max_duration_ms"`
}

var routingLearningFallbackReasons = [...]string{
	RoutingLearningFallbackMissing,
	RoutingLearningFallbackStale,
	RoutingLearningFallbackSchema,
	RoutingLearningFallbackDataQuality,
	RoutingLearningFallbackDrift,
	RoutingLearningFallbackCalibration,
	RoutingLearningFallbackLowSamples,
	RoutingLearningFallbackFeatures,
	RoutingLearningFallbackTimeout,
	RoutingLearningFallbackNonFinite,
	RoutingLearningFallbackOutOfRange,
}

type RoutingLearningMetrics struct {
	Attempts             uint64                  `json:"attempts"`
	Applications         uint64                  `json:"applications"`
	AppliedGroups        uint64                  `json:"applied_groups,omitempty"`
	Fallbacks            map[string]uint64       `json:"fallbacks"`
	LastCalibrationError float64                 `json:"last_calibration_error"`
	InferenceLatency     RoutingLatencyQuantiles `json:"inference_latency,omitempty"`
}

// RoutingRuntimeMetrics intentionally has no dynamic labels. Per-group and
// per-request drill-down belongs in bounded routing facts; these counters stay
// safe for process health polling regardless of key/group cardinality.
type RoutingRuntimeMetrics struct {
	plans                        atomic.Uint64
	excludedCandidates           atomic.Uint64
	candidateBuckets             [DefaultMaxAPIKeyGroupRoutes + 1]atomic.Uint64
	switches                     atomic.Uint64
	stickyBreaks                 atomic.Uint64
	stickyHits                   atomic.Uint64
	stickyMisses                 atomic.Uint64
	stickyErrors                 atomic.Uint64
	stickyBinds                  atomic.Uint64
	breakerClosed                atomic.Uint64
	breakerOpen                  atomic.Uint64
	breakerHalfOpen              atomic.Uint64
	breakerRecovering            atomic.Uint64
	halfOpenProbes               atomic.Uint64
	redisDegraded                atomic.Uint64
	scoreHits                    atomic.Uint64
	scoreMisses                  atomic.Uint64
	scoreAgeCount                atomic.Uint64
	scoreAgeTotalMS              atomic.Uint64
	scoreAgeMaxMS                atomic.Uint64
	authLookupLatency            routingLatencyHistogram
	planBuildLatency             routingLatencyHistogram
	stateReadLatency             routingLatencyHistogram
	smartRankLatency             routingLatencyHistogram
	stateWriteLatency            routingLatencyHistogram
	backgroundQueries            atomic.Uint64
	backgroundFailures           atomic.Uint64
	backgroundRows               atomic.Uint64
	backgroundTotalNS            atomic.Uint64
	backgroundMaxNS              atomic.Uint64
	personalizationAttempts      atomic.Uint64
	personalizationApplications  atomic.Uint64
	personalizationAppliedGroups atomic.Uint64
	personalizationFallbacks     [len(routingLearningFallbackReasons)]atomic.Uint64
	personalizationCalibration   atomic.Uint64
	modelAttempts                atomic.Uint64
	modelApplications            atomic.Uint64
	modelFallbacks               [len(routingLearningFallbackReasons)]atomic.Uint64
	modelCalibration             atomic.Uint64
	modelInferenceLatency        routingLatencyHistogram
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
	ScoreSnapshotHits     uint64                             `json:"score_snapshot_hits"`
	ScoreSnapshotMisses   uint64                             `json:"score_snapshot_misses"`
	ScoreAgeSamples       uint64                             `json:"score_age_samples"`
	ScoreAgeAverageMS     float64                            `json:"score_age_average_ms"`
	ScoreAgeMaxMS         uint64                             `json:"score_age_max_ms"`
	PhaseLatency          map[string]RoutingLatencyQuantiles `json:"phase_latency"`
	BackgroundQueries     RoutingBackgroundQueryMetrics      `json:"background_queries"`
	Personalization       RoutingLearningMetrics             `json:"personalization"`
	ModelPrediction       RoutingLearningMetrics             `json:"model_prediction"`
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

func (m *RoutingRuntimeMetrics) RecordScoreSnapshot(found bool, age time.Duration) {
	if m == nil {
		return
	}
	if !found {
		m.scoreMisses.Add(1)
		return
	}
	m.scoreHits.Add(1)
	if age < 0 {
		age = 0
	}
	ageMS := uint64(age / time.Millisecond)
	m.scoreAgeCount.Add(1)
	m.scoreAgeTotalMS.Add(ageMS)
	for current := m.scoreAgeMaxMS.Load(); ageMS > current; current = m.scoreAgeMaxMS.Load() {
		if m.scoreAgeMaxMS.CompareAndSwap(current, ageMS) {
			break
		}
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
	case RoutingLatencyPhaseSmartRanking:
		histogram = &m.smartRankLatency
	case RoutingLatencyPhaseStateWrite:
		histogram = &m.stateWriteLatency
	default:
		return
	}
	histogram.record(duration)
}

func (m *RoutingRuntimeMetrics) RecordBackgroundQuery(duration time.Duration, scannedRows int64, failed bool) {
	if m == nil {
		return
	}
	if duration < 0 {
		duration = 0
	}
	if scannedRows < 0 {
		scannedRows = 0
	}
	m.backgroundQueries.Add(1)
	m.backgroundRows.Add(uint64(scannedRows))
	if failed {
		m.backgroundFailures.Add(1)
	}
	nanos := uint64(duration)
	m.backgroundTotalNS.Add(nanos)
	for current := m.backgroundMaxNS.Load(); nanos > current; current = m.backgroundMaxNS.Load() {
		if m.backgroundMaxNS.CompareAndSwap(current, nanos) {
			break
		}
	}
}

func (m *RoutingRuntimeMetrics) RecordPersonalization(reason string, calibration float64, appliedGroups int) {
	if m == nil {
		return
	}
	m.personalizationAttempts.Add(1)
	if reason == "" {
		m.personalizationApplications.Add(1)
		if appliedGroups > 0 {
			m.personalizationAppliedGroups.Add(uint64(appliedGroups))
		}
	} else if index := routingLearningFallbackIndex(reason); index >= 0 {
		m.personalizationFallbacks[index].Add(1)
	}
	if !math.IsNaN(calibration) && !math.IsInf(calibration, 0) && calibration >= 0 {
		m.personalizationCalibration.Store(math.Float64bits(calibration))
	}
}

func (m *RoutingRuntimeMetrics) RecordModelPrediction(reason string, duration time.Duration, calibration float64) {
	if m == nil {
		return
	}
	m.modelAttempts.Add(1)
	if reason == "" {
		m.modelApplications.Add(1)
	} else if index := routingLearningFallbackIndex(reason); index >= 0 {
		m.modelFallbacks[index].Add(1)
	}
	m.modelInferenceLatency.record(duration)
	if !math.IsNaN(calibration) && !math.IsInf(calibration, 0) && calibration >= 0 {
		m.modelCalibration.Store(math.Float64bits(calibration))
	}
}

func routingLearningFallbackIndex(reason string) int {
	for index, candidate := range routingLearningFallbackReasons {
		if reason == candidate {
			return index
		}
	}
	return -1
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
		PhaseLatency:          make(map[string]RoutingLatencyQuantiles, 5),
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
	snapshot.ScoreSnapshotHits = m.scoreHits.Load()
	snapshot.ScoreSnapshotMisses = m.scoreMisses.Load()
	snapshot.ScoreAgeSamples = m.scoreAgeCount.Load()
	if snapshot.ScoreAgeSamples > 0 {
		snapshot.ScoreAgeAverageMS = float64(m.scoreAgeTotalMS.Load()) / float64(snapshot.ScoreAgeSamples)
	}
	snapshot.ScoreAgeMaxMS = m.scoreAgeMaxMS.Load()
	snapshot.PhaseLatency[RoutingLatencyPhaseAuthLookup] = m.authLookupLatency.snapshot()
	snapshot.PhaseLatency[RoutingLatencyPhasePlanBuild] = m.planBuildLatency.snapshot()
	snapshot.PhaseLatency[RoutingLatencyPhaseStateRead] = m.stateReadLatency.snapshot()
	snapshot.PhaseLatency[RoutingLatencyPhaseSmartRanking] = m.smartRankLatency.snapshot()
	snapshot.PhaseLatency[RoutingLatencyPhaseStateWrite] = m.stateWriteLatency.snapshot()
	snapshot.BackgroundQueries.Queries = m.backgroundQueries.Load()
	snapshot.BackgroundQueries.Failures = m.backgroundFailures.Load()
	snapshot.BackgroundQueries.ScannedRows = m.backgroundRows.Load()
	if snapshot.BackgroundQueries.Queries > 0 {
		snapshot.BackgroundQueries.AverageDuration = float64(m.backgroundTotalNS.Load()) / float64(snapshot.BackgroundQueries.Queries) / float64(time.Millisecond)
	}
	snapshot.BackgroundQueries.MaxDuration = float64(m.backgroundMaxNS.Load()) / float64(time.Millisecond)
	snapshot.Personalization = m.personalizationSnapshot()
	snapshot.ModelPrediction = m.modelPredictionSnapshot()
	defaultRoutingFactSink.RLock()
	if recorder, ok := defaultRoutingFactSink.sink.(*RoutingFactRecorder); ok {
		snapshot.FactRecorder = recorder.Stats()
	}
	defaultRoutingFactSink.RUnlock()
	return snapshot
}

func (m *RoutingRuntimeMetrics) personalizationSnapshot() RoutingLearningMetrics {
	result := RoutingLearningMetrics{
		Attempts: m.personalizationAttempts.Load(), Applications: m.personalizationApplications.Load(),
		AppliedGroups: m.personalizationAppliedGroups.Load(), Fallbacks: make(map[string]uint64, len(routingLearningFallbackReasons)),
		LastCalibrationError: math.Float64frombits(m.personalizationCalibration.Load()),
	}
	for index, reason := range routingLearningFallbackReasons {
		result.Fallbacks[reason] = m.personalizationFallbacks[index].Load()
	}
	return result
}

func (m *RoutingRuntimeMetrics) modelPredictionSnapshot() RoutingLearningMetrics {
	result := RoutingLearningMetrics{
		Attempts: m.modelAttempts.Load(), Applications: m.modelApplications.Load(),
		Fallbacks:            make(map[string]uint64, len(routingLearningFallbackReasons)),
		LastCalibrationError: math.Float64frombits(m.modelCalibration.Load()),
		InferenceLatency:     m.modelInferenceLatency.snapshot(),
	}
	for index, reason := range routingLearningFallbackReasons {
		result.Fallbacks[reason] = m.modelFallbacks[index].Load()
	}
	return result
}
