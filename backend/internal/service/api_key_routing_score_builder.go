package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

const (
	routingScoreBuilderLeaderKey       = "route:score:builder:leader"
	routingPriceMinimumRequests        = int64(10)
	routingPriceMinimumLogicalInput    = int64(10_000)
	routingPriceFullConfidenceRequests = float64(100)
	routingPriceFullConfidenceTokens   = float64(100_000)
)

type APIKeyRoutingMetricAggregate struct {
	Platform                 string
	GroupID                  int64
	Model                    string
	EndpointKind             string
	SuccessRequests          int64
	FailedRequests           int64
	CapacityOverflowRequests int64
	FailureCategoryCounts    map[string]int64
	InputTokens              int64
	OutputTokens             int64
	CacheCreationTokens      int64
	CacheReadTokens          int64
	TTFTSumMs                int64
	TTFTCount                int64
	DurationSumMs            int64
	DurationCount            int64
	RateMultiplier           float64
	AccountPoolDomain        string
	DataThrough              time.Time
	Group                    *Group
	PriceSamples             []APIKeyRoutingPriceSample
}

// APIKeyRoutingPriceSample is a bounded, current-price-replayable workload
// slice. It contains supplier-reported quantities only; user billing rewrites
// and cache-failover compensation never enter this input.
type APIKeyRoutingPriceSample struct {
	Model                 string `json:"model"`
	EndpointKind          string `json:"endpoint_kind"`
	ServiceTier           string `json:"service_tier"`
	ContextBucket         int    `json:"context_bucket"`
	SuccessRequests       int64  `json:"success_requests"`
	InputTokens           int64  `json:"input_tokens"`
	ImageInputTokens      int64  `json:"image_input_tokens"`
	OutputTokens          int64  `json:"output_tokens"`
	CacheCreationTokens   int64  `json:"cache_creation_tokens"`
	CacheCreation5mTokens int64  `json:"cache_creation_5m_tokens"`
	CacheCreation1hTokens int64  `json:"cache_creation_1h_tokens"`
	CacheReadTokens       int64  `json:"cache_read_tokens"`
	ImageOutputTokens     int64  `json:"image_output_tokens"`
}

type APIKeyRoutingScoreObservationSource interface {
	// Refresh owns routing buckets independently of the optional Channel Monitor.
	// Only the elected builder calls it; it must bound raw-fact scans and writes.
	RefreshAPIKeyRoutingMetricBuckets(ctx context.Context, now time.Time) error
	LoadAPIKeyRoutingMetricAggregates(ctx context.Context, start, end time.Time) ([]APIKeyRoutingMetricAggregate, error)
}

// RoutingBackgroundDatabase keeps the score builder's advisory-lock fallback
// on the same bounded pool as its aggregation queries instead of consuming a
// request-serving connection when Redis coordination is degraded.
type RoutingBackgroundDatabase interface {
	SQLDB() *sql.DB
}

type RoutingScoreBuilder struct {
	source   APIKeyRoutingScoreObservationSource
	cache    APIKeyRoutingScoreCache
	store    *AtomicAPIKeyRoutingScoreStore
	lock     LeaderLockCache
	db       *sql.DB
	billing  *BillingService
	resolver *ModelPricingResolver
	owner    string
	interval time.Duration
	ttl      time.Duration
	now      func() time.Time

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
	running  atomic.Bool
	builds   atomic.Uint64
	failures atomic.Uint64
}

// SetCurrentPricing enables current-price replay for observed workload slices.
// It is kept separate from the constructor so repository/unit stubs that only
// exercise health scoring do not need to construct the full billing graph.
func (b *RoutingScoreBuilder) SetCurrentPricing(billing *BillingService, resolver *ModelPricingResolver) {
	if b == nil {
		return
	}
	b.billing = billing
	b.resolver = resolver
}

func NewRoutingScoreBuilder(source APIKeyRoutingScoreObservationSource, cache APIKeyRoutingScoreCache, store *AtomicAPIKeyRoutingScoreStore, lock LeaderLockCache, db *sql.DB) *RoutingScoreBuilder {
	if store == nil {
		store = DefaultAPIKeyRoutingScoreStore()
	}
	return &RoutingScoreBuilder{
		source: source, cache: cache, store: store, lock: lock, db: db, owner: uuid.NewString(),
		interval: time.Minute, ttl: 3 * time.Minute, now: time.Now,
		stopCh: make(chan struct{}), doneCh: make(chan struct{}),
	}
}

func (b *RoutingScoreBuilder) Start() {
	if b == nil || b.source == nil || b.cache == nil || !b.running.CompareAndSwap(false, true) {
		return
	}
	go b.run()
}

func (b *RoutingScoreBuilder) Stop() {
	if b == nil || !b.running.Load() {
		return
	}
	b.stopOnce.Do(func() { close(b.stopCh) })
	<-b.doneCh
}

func (b *RoutingScoreBuilder) run() {
	defer close(b.doneCh)
	defer b.running.Store(false)
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		_ = b.BuildOnce(ctx)
		cancel()
		select {
		case <-ticker.C:
		case <-b.stopCh:
			return
		}
	}
}

func (b *RoutingScoreBuilder) BuildOnce(ctx context.Context) error {
	if b == nil || b.source == nil || b.cache == nil {
		return errors.New("routing score builder dependencies are unavailable")
	}
	release, acquired := tryAcquireSingletonLeaderLock(ctx, b.lock, b.db, routingScoreBuilderLeaderKey, b.owner, 2*time.Minute)
	if !acquired {
		return b.RefreshFromCache(ctx)
	}
	if release != nil {
		defer release()
	}
	now := b.now().UTC()
	if err := b.source.RefreshAPIKeyRoutingMetricBuckets(ctx, now); err != nil {
		b.failures.Add(1)
		return fmt.Errorf("refresh routing metric buckets: %w", err)
	}
	oneHour, err := b.source.LoadAPIKeyRoutingMetricAggregates(ctx, now.Add(-time.Hour), now)
	if err != nil {
		b.failures.Add(1)
		return fmt.Errorf("load 1h routing metrics: %w", err)
	}
	twentyFourHours, err := b.source.LoadAPIKeyRoutingMetricAggregates(ctx, now.Add(-24*time.Hour), now)
	if err != nil {
		b.failures.Add(1)
		return fmt.Errorf("load 24h routing metrics: %w", err)
	}
	snapshots := BuildAPIKeyRoutingScoreSnapshotsWithPricing(ctx, oneHour, twentyFourHours, now, b.billing, b.resolver)
	if len(snapshots) == 0 {
		return nil
	}
	for _, snapshot := range snapshots {
		scope := APIKeyRoutingScoreScope{Platform: snapshot.Platform, ModelFamily: snapshot.ModelFamily, EndpointKind: snapshot.EndpointKind}
		if previous, ok := b.store.Lookup(scope, 0, now); ok {
			SmoothAPIKeyRoutingScoreSnapshot(previous, snapshot, 0.35)
		}
	}
	for _, snapshot := range snapshots {
		if err := b.cache.PublishAPIKeyRoutingScoreSnapshot(ctx, snapshot, b.ttl); err != nil {
			b.failures.Add(1)
			return fmt.Errorf("publish routing score snapshot %s: %w", snapshot.Version, err)
		}
	}
	if err := b.store.Replace(snapshots); err != nil {
		b.failures.Add(1)
		return fmt.Errorf("install routing score snapshot catalog: %w", err)
	}
	b.builds.Add(1)
	return nil
}

// RefreshFromCache is the follower path. It performs bounded batched Redis
// reads, validates every complete version, then swaps the whole local catalog.
func (b *RoutingScoreBuilder) RefreshFromCache(ctx context.Context) error {
	if b == nil || b.cache == nil || b.store == nil {
		return errors.New("routing score cache refresh dependencies are unavailable")
	}
	snapshots, err := b.cache.LoadAllCurrentAPIKeyRoutingScoreSnapshots(ctx)
	if err != nil {
		b.failures.Add(1)
		return err
	}
	if len(snapshots) == 0 {
		return nil
	}
	if err := b.store.Replace(snapshots); err != nil {
		b.failures.Add(1)
		return err
	}
	return nil
}

type routingScoreAggregateKey struct {
	platform string
	family   string
	groupID  int64
	endpoint string
}

type routingPriceBaselineKey struct {
	platform string
	family   string
}

type routingPriceBaseline struct {
	samples     []APIKeyRoutingPriceSample
	dataThrough time.Time
}

type routingPriceEvidence struct {
	samples     []APIKeyRoutingPriceSample
	window      string
	dataThrough time.Time
}

func BuildAPIKeyRoutingScoreSnapshots(oneHour, twentyFourHours []APIKeyRoutingMetricAggregate, now time.Time) []*APIKeyRoutingScoreSnapshot {
	return BuildAPIKeyRoutingScoreSnapshotsWithPricing(context.Background(), oneHour, twentyFourHours, now, nil, nil)
}

// BuildAPIKeyRoutingScoreSnapshotsWithPricing converts passive health facts and
// actual supplier token slices into immutable score snapshots. Health evidence
// remains shared by model family; price composition is additionally projected
// per bounded endpoint kind and replayed against the current price card.
func BuildAPIKeyRoutingScoreSnapshotsWithPricing(
	ctx context.Context,
	oneHour, twentyFourHours []APIKeyRoutingMetricAggregate,
	now time.Time,
	billing *BillingService,
	resolver *ModelPricingResolver,
) []*APIKeyRoutingScoreSnapshot {
	short := mergeRoutingScoreAggregates(oneHour)
	long := mergeRoutingScoreAggregates(twentyFourHours)
	priceBaselines := buildRoutingPriceBaselines(long)
	selected := make(map[routingScoreAggregateKey]APIKeyRoutingMetricAggregate, len(long)+len(short))
	healthWindow := make(map[routingScoreAggregateKey]string, len(long)+len(short))
	for key, aggregate := range long {
		selected[key] = aggregate
		healthWindow[key] = "24h"
	}
	for key, aggregate := range short {
		if aggregate.SuccessRequests+aggregate.FailedRequests >= 10 {
			selected[key] = aggregate
			healthWindow[key] = "1h"
		}
	}
	byScope := make(map[string]*APIKeyRoutingScoreSnapshot)
	version := fmt.Sprintf("score-%d", now.Unix())
	for key, aggregate := range selected {
		total := aggregate.SuccessRequests + aggregate.FailedRequests
		observedAt := aggregate.DataThrough
		if observedAt.IsZero() {
			observedAt = now
		}
		confidence := math.Min(1, float64(total)/100) * routingObservationFreshness(observedAt, now, healthWindow[key])
		shortAggregate := short[key]
		longAggregate := long[key]
		baseline := priceBaselines[routingPriceBaselineKey{platform: key.platform, family: key.family}]
		endpointKinds := []string{key.endpoint}
		if strings.TrimSpace(aggregate.EndpointKind) == "" {
			endpointKinds = routingPriceEndpointKinds(shortAggregate.PriceSamples, longAggregate.PriceSamples, baseline.samples)
		}
		for _, endpointKind := range endpointKinds {
			evidence := selectRoutingPriceEvidence(shortAggregate, longAggregate, baseline, endpointKind)
			samples := evidence.samples
			scope := APIKeyRoutingScoreScope{Platform: key.platform, ModelFamily: key.family, EndpointKind: endpointKind}
			snapshot := byScope[scope.Key()]
			if snapshot == nil {
				snapshot = &APIKeyRoutingScoreSnapshot{
					Version: version, FeatureVersion: "routing-features-v2", StrategyVersion: BuiltinAPIKeyRoutingStrategyVersion,
					Platform: scope.Platform, ModelFamily: scope.ModelFamily, EndpointKind: scope.EndpointKind,
					GeneratedAt: now, Groups: make(map[int64]APIKeyRoutingGroupObservation),
				}
				byScope[scope.Key()] = snapshot
			}
			logicalInput, outputTokens, cacheHitRate := routingPriceSampleTotals(samples)
			priceFactor, exactPrice := currentRoutingPriceNormalizationFactor(ctx, aggregate, samples, now, billing, resolver)
			priceConfidence := 0.0
			if exactPrice {
				priceConfidence = routingPriceEvidenceConfidence(evidence, now)
			} else {
				evidence.window = "nominal"
			}
			observation := APIKeyRoutingGroupObservation{
				GroupID: key.groupID, SuccessRequests: aggregate.SuccessRequests, FailedRequests: aggregate.FailedRequests,
				CapacityScore: confidenceWeightedCapacity(aggregate), Confidence: confidence,
				CacheHitRate: cacheHitRate, LogicalInputTokens: logicalInput, ActualOutputTokens: outputTokens,
				ObservationWindow: evidence.window, ObservationGeneratedAt: observedAt,
				PriceNormalizationFactor: priceFactor,
				NormalizedRate:           nonNegativeFiniteOr(aggregate.RateMultiplier, 1) * priceFactor,
				PriceConfidence:          priceConfidence,
				PriceFallback:            !exactPrice,
			}
			observation.DependencyDomains = boundedRoutingDependencyDomains(aggregate.Platform, aggregate.AccountPoolDomain)
			if total > 0 {
				observation.SmoothedSuccessRate = float64(aggregate.SuccessRequests) / float64(total)
			}
			if aggregate.TTFTCount > 0 {
				observation.TTFTP50Ms = float64(aggregate.TTFTSumMs) / float64(aggregate.TTFTCount)
			}
			if aggregate.DurationCount > 0 {
				observation.DurationP50Ms = float64(aggregate.DurationSumMs) / float64(aggregate.DurationCount)
			}
			snapshot.Groups[key.groupID] = observation
		}
	}
	result := make([]*APIKeyRoutingScoreSnapshot, 0, len(byScope))
	for _, snapshot := range byScope {
		result = append(result, snapshot)
	}
	sort.Slice(result, func(i, j int) bool {
		return (APIKeyRoutingScoreScope{Platform: result[i].Platform, ModelFamily: result[i].ModelFamily, EndpointKind: result[i].EndpointKind}).Key() <
			(APIKeyRoutingScoreScope{Platform: result[j].Platform, ModelFamily: result[j].ModelFamily, EndpointKind: result[j].EndpointKind}).Key()
	})
	return result
}

func buildRoutingPriceBaselines(long map[routingScoreAggregateKey]APIKeyRoutingMetricAggregate) map[routingPriceBaselineKey]routingPriceBaseline {
	result := make(map[routingPriceBaselineKey]routingPriceBaseline)
	for key, aggregate := range long {
		baselineKey := routingPriceBaselineKey{platform: key.platform, family: key.family}
		baseline := result[baselineKey]
		baseline.samples = append(baseline.samples, aggregate.PriceSamples...)
		if aggregate.DataThrough.After(baseline.dataThrough) {
			baseline.dataThrough = aggregate.DataThrough
		}
		result[baselineKey] = baseline
	}
	return result
}

func routingPriceEndpointKinds(sampleSets ...[]APIKeyRoutingPriceSample) []string {
	seen := map[string]struct{}{"other": {}}
	for _, samples := range sampleSets {
		for _, sample := range samples {
			endpoint := NormalizeAPIKeyRoutingEndpointKind(sample.EndpointKind)
			seen[endpoint] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for endpoint := range seen {
		result = append(result, endpoint)
	}
	sort.Strings(result)
	return result
}

func selectRoutingPriceEvidence(short, long APIKeyRoutingMetricAggregate, baseline routingPriceBaseline, endpoint string) routingPriceEvidence {
	shortSamples := routingPriceSamplesForEndpoint(short.PriceSamples, endpoint)
	if routingPriceEvidenceSufficient(shortSamples) {
		return routingPriceEvidence{samples: shortSamples, window: "1h", dataThrough: short.DataThrough}
	}
	longSamples := routingPriceSamplesForEndpoint(long.PriceSamples, endpoint)
	if routingPriceEvidenceSufficient(longSamples) {
		return routingPriceEvidence{samples: longSamples, window: "24h", dataThrough: long.DataThrough}
	}
	baselineSamples := routingPriceSamplesForEndpoint(baseline.samples, endpoint)
	if routingPriceEvidenceSufficient(baselineSamples) {
		return routingPriceEvidence{samples: baselineSamples, window: "platform_baseline", dataThrough: baseline.dataThrough}
	}
	return routingPriceEvidence{window: "nominal"}
}

func routingPriceSamplesForEndpoint(samples []APIKeyRoutingPriceSample, endpoint string) []APIKeyRoutingPriceSample {
	endpoint = NormalizeAPIKeyRoutingEndpointKind(endpoint)
	if endpoint == "other" {
		return append([]APIKeyRoutingPriceSample(nil), samples...)
	}
	result := make([]APIKeyRoutingPriceSample, 0, len(samples))
	for _, sample := range samples {
		if NormalizeAPIKeyRoutingEndpointKind(sample.EndpointKind) == endpoint {
			result = append(result, sample)
		}
	}
	return result
}

func routingPriceEvidenceSufficient(samples []APIKeyRoutingPriceSample) bool {
	var requests int64
	for _, sample := range samples {
		requests += sample.SuccessRequests
	}
	logicalInput, _, _ := routingPriceSampleTotals(samples)
	return requests >= routingPriceMinimumRequests && logicalInput >= routingPriceMinimumLogicalInput
}

func routingPriceEvidenceConfidence(evidence routingPriceEvidence, now time.Time) float64 {
	var requests int64
	for _, sample := range evidence.samples {
		requests += sample.SuccessRequests
	}
	logicalInput, _, _ := routingPriceSampleTotals(evidence.samples)
	confidence := math.Min(1, float64(requests)/routingPriceFullConfidenceRequests)
	confidence = math.Min(confidence, math.Min(1, float64(logicalInput)/routingPriceFullConfidenceTokens))
	multiplier, freshnessWindow := 1.0, evidence.window
	switch evidence.window {
	case "24h":
		multiplier = 0.8
	case "platform_baseline":
		multiplier = 0.5
		freshnessWindow = "24h"
	case "1h":
	default:
		return 0
	}
	observedAt := evidence.dataThrough
	if observedAt.IsZero() {
		observedAt = now
	}
	return routeClamp01(confidence * multiplier * routingObservationFreshness(observedAt, now, freshnessWindow))
}

func routingPriceSampleTotals(samples []APIKeyRoutingPriceSample) (logicalInput, output int64, cacheHitRate float64) {
	var cacheRead int64
	for _, sample := range samples {
		logicalInput += sample.InputTokens + sample.CacheCreationTokens + sample.CacheReadTokens
		output += sample.OutputTokens
		cacheRead += sample.CacheReadTokens
	}
	if logicalInput > 0 {
		cacheHitRate = routeClamp01(float64(cacheRead) / float64(logicalInput))
	}
	return logicalInput, output, cacheHitRate
}

func currentRoutingPriceNormalizationFactor(
	ctx context.Context,
	aggregate APIKeyRoutingMetricAggregate,
	samples []APIKeyRoutingPriceSample,
	pricingAt time.Time,
	billing *BillingService,
	resolver *ModelPricingResolver,
) (float64, bool) {
	if billing != nil && resolver != nil && len(samples) > 0 {
		var actualCost, fullCacheCost float64
		for _, sample := range samples {
			requests := sample.SuccessRequests
			if requests <= 0 {
				continue
			}
			actualTokens := averageRoutingPriceTokens(sample, requests)
			fullCacheTokens := actualTokens
			fullCacheTokens.CacheReadTokens = actualTokens.InputTokens + actualTokens.CacheCreationTokens + actualTokens.CacheReadTokens
			fullCacheTokens.InputTokens = 0
			fullCacheTokens.CacheCreationTokens = 0
			fullCacheTokens.CacheCreation5mTokens = 0
			fullCacheTokens.CacheCreation1hTokens = 0
			groupID := aggregate.GroupID
			serviceTier := sample.ServiceTier
			if serviceTier == "default" || serviceTier == "other" {
				serviceTier = ""
			}
			model := strings.TrimSpace(sample.Model)
			if model == "" {
				model = aggregate.Model
			}
			base := CostInput{
				Ctx: ctx, Model: model, GroupID: &groupID, Group: aggregate.Group,
				Tokens: actualTokens, RequestCount: 1, RateMultiplier: 1, PricingAt: pricingAt,
				ServiceTier: serviceTier, Resolver: resolver,
			}
			actual, actualErr := billing.CalculateCostUnified(base)
			// The denominator is one common 1.0x reference price for the
			// platform/model/tier. It must not inherit this candidate's group or
			// channel override, otherwise candidate price differences cancel out.
			reference := base
			reference.GroupID = nil
			reference.Group = nil
			reference.Tokens = fullCacheTokens
			fullCache, cacheErr := billing.CalculateCostUnified(reference)
			if actualErr != nil || cacheErr != nil || actual == nil || fullCache == nil || fullCache.ActualCost <= 0 {
				return 1, false
			}
			actualCost += actual.ActualCost * float64(requests)
			fullCacheCost += fullCache.ActualCost * float64(requests)
		}
		if fullCacheCost > 0 && actualCost >= 0 && !math.IsNaN(actualCost) && !math.IsInf(actualCost, 0) {
			return actualCost / fullCacheCost, true
		}
	}
	// Missing/incompatible current pricing is a nominal 1x workload factor,
	// explicitly marked as fallback with zero price confidence. Inventing generic
	// unit costs here could make an unknown candidate appear artificially cheap.
	return 1, false
}

func averageRoutingPriceTokens(sample APIKeyRoutingPriceSample, requests int64) UsageTokens {
	average := func(value int64) int {
		if value <= 0 || requests <= 0 {
			return 0
		}
		return int(math.Round(float64(value) / float64(requests)))
	}
	return UsageTokens{
		InputTokens: average(sample.InputTokens), ImageInputTokens: average(sample.ImageInputTokens),
		OutputTokens: average(sample.OutputTokens), CacheCreationTokens: average(sample.CacheCreationTokens),
		CacheReadTokens: average(sample.CacheReadTokens), CacheCreation5mTokens: average(sample.CacheCreation5mTokens),
		CacheCreation1hTokens: average(sample.CacheCreation1hTokens), ImageOutputTokens: average(sample.ImageOutputTokens),
	}
}

func boundedRoutingDependencyDomains(platform, accountPool string) []string {
	result := make([]string, 0, 2)
	platform = strings.ToLower(strings.TrimSpace(platform))
	if boundedRoutingDimension(platform) {
		result = append(result, "provider:"+platform)
	}
	accountPool = strings.ToLower(strings.TrimSpace(accountPool))
	if len(accountPool) == 32 {
		result = append(result, "account_pool:"+accountPool)
	}
	return result
}

// SmoothAPIKeyRoutingScoreSnapshot applies bounded EMA updates to the scoring
// features while retaining raw success/failure counts for the <50% hard gate.
// This prevents a transient bucket from moving all new-session rankings in one
// build, without hiding a genuinely unhealthy raw success rate.
func SmoothAPIKeyRoutingScoreSnapshot(previous, current *APIKeyRoutingScoreSnapshot, alpha float64) {
	if previous == nil || current == nil {
		return
	}
	if alpha <= 0 || alpha > 1 {
		alpha = 0.35
	}
	for groupID, next := range current.Groups {
		prior, ok := previous.Groups[groupID]
		if !ok {
			continue
		}
		rawSuccess := next.SmoothedSuccessRate
		if total := next.SuccessRequests + next.FailedRequests; total > 0 {
			rawSuccess = float64(next.SuccessRequests) / float64(total)
		}
		priorSuccess := prior.SmoothedSuccessRate
		if priorSuccess == 0 {
			if total := prior.SuccessRequests + prior.FailedRequests; total > 0 {
				priorSuccess = float64(prior.SuccessRequests) / float64(total)
			}
		}
		next.SmoothedSuccessRate = boundedEMA(priorSuccess, rawSuccess, alpha, 0.10, false)
		next.CapacityScore = boundedEMA(prior.CapacityScore, next.CapacityScore, alpha, 0.15, false)
		next.CacheHitRate = boundedEMA(prior.CacheHitRate, next.CacheHitRate, alpha, 0.15, false)
		next.PriceConfidence = boundedEMA(prior.PriceConfidence, next.PriceConfidence, alpha, 0.15, false)
		next.NormalizedRate = boundedEMA(prior.NormalizedRate, next.NormalizedRate, alpha, 0.25, true)
		next.PriceNormalizationFactor = boundedEMA(prior.PriceNormalizationFactor, next.PriceNormalizationFactor, alpha, 0.25, true)
		next.TTFTP50Ms = boundedEMA(prior.TTFTP50Ms, next.TTFTP50Ms, alpha, 0.25, true)
		next.DurationP50Ms = boundedEMA(prior.DurationP50Ms, next.DurationP50Ms, alpha, 0.25, true)
		current.Groups[groupID] = next
	}
}

func boundedEMA(previous, current, alpha, maxChange float64, relative bool) float64 {
	if math.IsNaN(current) || math.IsInf(current, 0) || current < 0 {
		return previous
	}
	if previous <= 0 || math.IsNaN(previous) || math.IsInf(previous, 0) {
		return current
	}
	value := previous + alpha*(current-previous)
	limit := maxChange
	if relative {
		limit = previous * maxChange
	}
	if value > previous+limit {
		value = previous + limit
	}
	if value < previous-limit {
		value = previous - limit
	}
	if value < 0 {
		return 0
	}
	return value
}

func mergeRoutingScoreAggregates(input []APIKeyRoutingMetricAggregate) map[routingScoreAggregateKey]APIKeyRoutingMetricAggregate {
	result := make(map[routingScoreAggregateKey]APIKeyRoutingMetricAggregate)
	for _, aggregate := range input {
		if aggregate.GroupID <= 0 || aggregate.Platform == "" {
			continue
		}
		key := routingScoreAggregateKey{
			platform: aggregate.Platform,
			family:   NormalizeAPIKeyRoutingModelFamily(aggregate.Platform, aggregate.Model),
			groupID:  aggregate.GroupID,
			endpoint: NormalizeAPIKeyRoutingEndpointKind(aggregate.EndpointKind),
		}
		current := result[key]
		first := current.GroupID == 0
		current.Platform, current.GroupID, current.Model = key.platform, key.groupID, key.family
		if strings.TrimSpace(aggregate.EndpointKind) != "" {
			current.EndpointKind = key.endpoint
		}
		current.SuccessRequests += aggregate.SuccessRequests
		current.FailedRequests += aggregate.FailedRequests
		current.CapacityOverflowRequests += aggregate.CapacityOverflowRequests
		if current.FailureCategoryCounts == nil {
			current.FailureCategoryCounts = make(map[string]int64)
		}
		for category, count := range aggregate.FailureCategoryCounts {
			current.FailureCategoryCounts[category] += count
		}
		current.InputTokens += aggregate.InputTokens
		current.OutputTokens += aggregate.OutputTokens
		current.CacheCreationTokens += aggregate.CacheCreationTokens
		current.CacheReadTokens += aggregate.CacheReadTokens
		current.TTFTSumMs += aggregate.TTFTSumMs
		current.TTFTCount += aggregate.TTFTCount
		current.DurationSumMs += aggregate.DurationSumMs
		current.DurationCount += aggregate.DurationCount
		if first {
			current.RateMultiplier = nonNegativeFiniteOr(aggregate.RateMultiplier, 1)
			current.Group = aggregate.Group
		}
		current.PriceSamples = append(current.PriceSamples, aggregate.PriceSamples...)
		if current.AccountPoolDomain == "" {
			current.AccountPoolDomain = aggregate.AccountPoolDomain
		}
		if aggregate.DataThrough.After(current.DataThrough) {
			current.DataThrough = aggregate.DataThrough
		}
		result[key] = current
	}
	return result
}

func confidenceWeightedCapacity(aggregate APIKeyRoutingMetricAggregate) float64 {
	total := aggregate.SuccessRequests + aggregate.CapacityOverflowRequests
	if total <= 0 {
		return 0.5
	}
	return routeClamp01(float64(aggregate.SuccessRequests) / float64(total))
}

func routingObservationFreshness(observedAt, now time.Time, window string) float64 {
	age := now.Sub(observedAt)
	if age <= 0 {
		return 1
	}
	halfLife := 15 * time.Minute
	if window == "24h" {
		halfLife = 6 * time.Hour
	}
	// Smooth exponential decay preserves a little passive evidence while making
	// old low-traffic observations converge toward the neutral prior.
	return math.Exp(-math.Ln2 * float64(age) / float64(halfLife))
}
