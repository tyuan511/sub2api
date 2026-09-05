package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type routingScoreSourceStub struct {
	calls      int
	refreshes  int
	refreshErr error
	one        []APIKeyRoutingMetricAggregate
	day        []APIKeyRoutingMetricAggregate
}

func (s *routingScoreSourceStub) RefreshAPIKeyRoutingMetricBuckets(context.Context, time.Time) error {
	s.refreshes++
	return s.refreshErr
}

func (s *routingScoreSourceStub) LoadAPIKeyRoutingMetricAggregates(_ context.Context, start, end time.Time) ([]APIKeyRoutingMetricAggregate, error) {
	s.calls++
	if end.Sub(start) <= time.Hour+time.Second {
		return append([]APIKeyRoutingMetricAggregate(nil), s.one...), nil
	}
	return append([]APIKeyRoutingMetricAggregate(nil), s.day...), nil
}

type routingScoreCacheStub struct {
	published []*APIKeyRoutingScoreSnapshot
	ttls      []time.Duration
	current   []*APIKeyRoutingScoreSnapshot
}

func (s *routingScoreCacheStub) PublishAPIKeyRoutingScoreSnapshot(_ context.Context, snapshot *APIKeyRoutingScoreSnapshot, ttl time.Duration) error {
	s.published = append(s.published, cloneAPIKeyRoutingScoreSnapshot(snapshot))
	s.ttls = append(s.ttls, ttl)
	return nil
}

func (*routingScoreCacheStub) LoadCurrentAPIKeyRoutingScoreSnapshot(context.Context, APIKeyRoutingScoreScope) (*APIKeyRoutingScoreSnapshot, error) {
	return nil, ErrAPIKeyRoutingScoreSnapshotNotFound
}

func (s *routingScoreCacheStub) LoadAllCurrentAPIKeyRoutingScoreSnapshots(context.Context) ([]*APIKeyRoutingScoreSnapshot, error) {
	return append([]*APIKeyRoutingScoreSnapshot(nil), s.current...), nil
}

func TestBuildAPIKeyRoutingScoreSnapshotsUsesOneHourThenDayFallback(t *testing.T) {
	now := time.Now().UTC()
	one := []APIKeyRoutingMetricAggregate{
		{Platform: PlatformOpenAI, GroupID: 1, Model: "gpt-5.6-sol", SuccessRequests: 9, RateMultiplier: 2, PriceSamples: []APIKeyRoutingPriceSample{{Model: "gpt-5.6-sol", EndpointKind: "responses", ServiceTier: "default", SuccessRequests: 9, InputTokens: 1000, CacheReadTokens: 9000}}},
		{Platform: PlatformOpenAI, GroupID: 2, Model: "gpt-5.6-sol", SuccessRequests: 10, RateMultiplier: 1, PriceSamples: []APIKeyRoutingPriceSample{{Model: "gpt-5.6-sol", EndpointKind: "responses", ServiceTier: "default", SuccessRequests: 10, InputTokens: 9000, CacheReadTokens: 1000}}},
	}
	day := []APIKeyRoutingMetricAggregate{
		{Platform: PlatformOpenAI, GroupID: 1, Model: "gpt-5.6-sol", SuccessRequests: 90, FailedRequests: 10, RateMultiplier: 2, PriceSamples: []APIKeyRoutingPriceSample{{Model: "gpt-5.6-sol", EndpointKind: "responses", ServiceTier: "default", SuccessRequests: 90, InputTokens: 10000, CacheReadTokens: 90000}}},
		{Platform: PlatformOpenAI, GroupID: 2, Model: "gpt-5.6-sol", SuccessRequests: 90, FailedRequests: 10, RateMultiplier: 1, PriceSamples: []APIKeyRoutingPriceSample{{Model: "gpt-5.6-sol", EndpointKind: "responses", ServiceTier: "default", SuccessRequests: 90, InputTokens: 90000, CacheReadTokens: 10000}}},
	}
	billing := &BillingService{fallbackPrices: map[string]*ModelPricing{
		"gpt-5.6-sol": {InputPricePerToken: 1, OutputPricePerToken: 5, CacheCreationPricePerToken: 1.25, CacheReadPricePerToken: .1},
	}}
	snapshots := BuildAPIKeyRoutingScoreSnapshotsWithPricing(context.Background(), one, day, now, billing, NewModelPricingResolver(nil, billing))
	var responses *APIKeyRoutingScoreSnapshot
	for _, snapshot := range snapshots {
		if snapshot.EndpointKind == "responses" {
			responses = snapshot
		}
	}
	require.NotNil(t, responses)
	groups := responses.Groups
	if groups[1].ObservationWindow != "24h" {
		t.Fatalf("low-volume group window = %q, want 24h", groups[1].ObservationWindow)
	}
	if groups[2].ObservationWindow != "1h" {
		t.Fatalf("sufficient-volume group window = %q, want 1h", groups[2].ObservationWindow)
	}
	require.False(t, groups[1].PriceFallback)
	require.False(t, groups[2].PriceFallback)
	require.Greater(t, groups[1].PriceConfidence, groups[2].PriceConfidence)
	require.Greater(t, groups[1].NormalizedRate, 2.0)
	require.Greater(t, groups[2].NormalizedRate, 1.0)
}

func TestRoutingCapacityScoreExcludesHealthFailuresAndUsesOverflow(t *testing.T) {
	aggregate := APIKeyRoutingMetricAggregate{
		SuccessRequests: 80, FailedRequests: 20, CapacityOverflowRequests: 80,
	}
	require.InDelta(t, 0.5, confidenceWeightedCapacity(aggregate), 1e-12)

	snapshots := BuildAPIKeyRoutingScoreSnapshots([]APIKeyRoutingMetricAggregate{{
		Platform: PlatformOpenAI, GroupID: 1, Model: "gpt-5.6-sol", EndpointKind: "responses",
		SuccessRequests: 80, FailedRequests: 20, CapacityOverflowRequests: 80, RateMultiplier: 1,
	}}, nil, time.Now())
	require.Len(t, snapshots, 1)
	observation := snapshots[0].Groups[1]
	require.InDelta(t, 0.8, observation.SmoothedSuccessRate, 1e-12)
	require.InDelta(t, 0.5, observation.CapacityScore, 1e-12)
}

func TestBuildAPIKeyRoutingScoreSnapshotsFallsBackToPlatformBaseline(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	two := 2.0
	group := &Group{
		ID: 1, Platform: PlatformOpenAI, RateMultiplier: 1,
		ModelPricing: []ChannelModelPricing{{
			Models: []string{"gpt-5.6-sol"}, BillingMode: BillingModeToken,
			InputPrice: &two, OutputPrice: &two, CacheReadPrice: &two,
		}},
	}
	one := []APIKeyRoutingMetricAggregate{
		{Platform: PlatformOpenAI, GroupID: 1, Model: "gpt-5.6-sol", SuccessRequests: 10, RateMultiplier: 1, Group: group},
		{Platform: PlatformOpenAI, GroupID: 2, Model: "gpt-5.6-sol", SuccessRequests: 10, RateMultiplier: 1},
	}
	day := []APIKeyRoutingMetricAggregate{
		{Platform: PlatformOpenAI, GroupID: 1, Model: "gpt-5.6-sol", SuccessRequests: 100, RateMultiplier: 1, Group: group},
		{
			Platform: PlatformOpenAI, GroupID: 2, Model: "gpt-5.6-sol", SuccessRequests: 100, RateMultiplier: 1,
			PriceSamples: []APIKeyRoutingPriceSample{{
				Model: "gpt-5.6-sol", EndpointKind: "responses", ServiceTier: "default",
				SuccessRequests: 100, InputTokens: 100_000,
			}},
		},
	}
	billing := &BillingService{fallbackPrices: map[string]*ModelPricing{
		"gpt-5.6-sol": {InputPricePerToken: 1, OutputPricePerToken: 1, CacheReadPricePerToken: 1},
	}}

	snapshots := BuildAPIKeyRoutingScoreSnapshotsWithPricing(context.Background(), one, day, now, billing, NewModelPricingResolver(nil, billing))
	var responses *APIKeyRoutingScoreSnapshot
	for _, snapshot := range snapshots {
		if snapshot.EndpointKind == "responses" {
			responses = snapshot
		}
	}
	require.NotNil(t, responses)
	observation := responses.Groups[1]
	require.Equal(t, "platform_baseline", observation.ObservationWindow)
	require.False(t, observation.PriceFallback)
	require.InDelta(t, 2, observation.PriceNormalizationFactor, 1e-9)
	require.InDelta(t, .5, observation.PriceConfidence, 1e-9)
}

func TestBuildAPIKeyRoutingScoreSnapshotsFallsBackToNominalRate(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	aggregates := []APIKeyRoutingMetricAggregate{{
		Platform: PlatformOpenAI, GroupID: 1, Model: "gpt-5.6-sol",
		SuccessRequests: 100, RateMultiplier: 1.75,
	}}

	snapshots := BuildAPIKeyRoutingScoreSnapshotsWithPricing(context.Background(), aggregates, aggregates, now, nil, nil)
	require.Len(t, snapshots, 1)
	observation := snapshots[0].Groups[1]
	require.Equal(t, "nominal", observation.ObservationWindow)
	require.True(t, observation.PriceFallback)
	require.Zero(t, observation.PriceConfidence)
	require.InDelta(t, 1, observation.PriceNormalizationFactor, 1e-9)
	require.InDelta(t, 1.75, observation.NormalizedRate, 1e-9)
}

func TestBuildAPIKeyRoutingScoreSnapshotsReplaysCurrentDetailedCachePricing(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	one, tenth, two, three, five := 1.0, 0.1, 2.0, 3.0, 5.0
	group := &Group{
		ID: 1, Platform: PlatformAnthropic, RateMultiplier: 2, LongContextPricingEnabled: true,
		ModelPricing: []ChannelModelPricing{{
			Models: []string{"claude-sonnet-4"}, BillingMode: BillingModeToken,
			InputPrice: &one, OutputPrice: &five, CacheWritePrice: &two,
			CacheWrite1hPrice: &three, CacheReadPrice: &tenth,
		}},
	}
	aggregates := []APIKeyRoutingMetricAggregate{{
		Platform: PlatformAnthropic, GroupID: 1, Model: "claude-sonnet-4",
		SuccessRequests: 10, CacheCreationTokens: 1000, RateMultiplier: 2, Group: group,
		PriceSamples: []APIKeyRoutingPriceSample{{
			Model: "claude-sonnet-4", EndpointKind: "messages", ServiceTier: "default", SuccessRequests: 10,
			CacheCreationTokens: 10000, CacheCreation5mTokens: 5000, CacheCreation1hTokens: 5000,
		}},
	}}
	billing := &BillingService{fallbackPrices: map[string]*ModelPricing{
		"claude-sonnet-4": {
			InputPricePerToken: 1, OutputPricePerToken: 5,
			CacheCreationPricePerToken: 1.25, CacheCreation5mPrice: 1.25,
			CacheCreation1hPrice: 2, CacheReadPricePerToken: 0.1, SupportsCacheBreakdown: true,
		},
	}}
	resolver := NewModelPricingResolver(nil, billing)
	groupID := int64(1)
	actualCost, actualErr := billing.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "claude-sonnet-4", GroupID: &groupID, Group: group,
		Tokens:         UsageTokens{CacheCreationTokens: 100, CacheCreation5mTokens: 50, CacheCreation1hTokens: 50},
		RateMultiplier: 1, Resolver: resolver,
	})
	fullCacheCost, cacheErr := billing.CalculateCostUnified(CostInput{
		Ctx: context.Background(), Model: "claude-sonnet-4",
		Tokens: UsageTokens{CacheReadTokens: 100}, RateMultiplier: 1, Resolver: resolver,
	})
	require.NoError(t, actualErr)
	require.NoError(t, cacheErr)
	require.InDelta(t, 250, actualCost.ActualCost, 1e-9)
	require.InDelta(t, 10, fullCacheCost.ActualCost, 1e-9)

	snapshots := BuildAPIKeyRoutingScoreSnapshotsWithPricing(context.Background(), aggregates, aggregates, now, billing, resolver)
	var messages, fallback *APIKeyRoutingScoreSnapshot
	for _, snapshot := range snapshots {
		switch snapshot.EndpointKind {
		case "messages":
			messages = snapshot
		case "other":
			fallback = snapshot
		}
	}
	require.NotNil(t, messages)
	require.NotNil(t, fallback)
	// Per average request: 50*2 + 50*3 actual versus 100*0.1 at
	// the full-cache reference. The group's 2x multiplier applies after that.
	require.InDelta(t, 25, messages.Groups[1].PriceNormalizationFactor, 1e-9)
	require.InDelta(t, 50, messages.Groups[1].NormalizedRate, 1e-9)
	require.InDelta(t, .1, messages.Groups[1].PriceConfidence, 1e-9)
	require.Equal(t, "routing-features-v2", messages.FeatureVersion)
}

func TestRoutingScoreBuilderFollowerLoadsCompleteRedisCatalog(t *testing.T) {
	now := time.Now().UTC()
	snapshot := routingScoreSnapshotForTest(now)
	cache := &routingScoreCacheStub{current: []*APIKeyRoutingScoreSnapshot{snapshot}}
	store := NewAtomicAPIKeyRoutingScoreStore()
	builder := NewRoutingScoreBuilder(&routingScoreSourceStub{}, cache, store, nil, nil)
	if err := builder.RefreshFromCache(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Lookup(APIKeyRoutingScoreScope{Platform: PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses"}, time.Minute, now); !ok {
		t.Fatal("follower did not install Redis score catalog")
	}
}

func TestRoutingScoreBuilderPublishesThenAtomicallyInstallsCatalog(t *testing.T) {
	now := time.Now().UTC()
	source := &routingScoreSourceStub{
		one: []APIKeyRoutingMetricAggregate{{Platform: PlatformOpenAI, GroupID: 1, Model: "gpt-5", SuccessRequests: 10, RateMultiplier: 1}},
		day: []APIKeyRoutingMetricAggregate{{Platform: PlatformOpenAI, GroupID: 1, Model: "gpt-5", SuccessRequests: 100, RateMultiplier: 1}},
	}
	cache := &routingScoreCacheStub{}
	store := NewAtomicAPIKeyRoutingScoreStore()
	builder := NewRoutingScoreBuilder(source, cache, store, nil, nil)
	builder.now = func() time.Time { return now }
	if err := builder.BuildOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	require.Equal(t, 1, source.refreshes, "leader must generate baseline buckets without a monitor dependency")
	if len(cache.published) != 1 {
		t.Fatalf("published snapshots = %d, want 1", len(cache.published))
	}
	require.Equal(t, []time.Duration{3 * time.Minute}, cache.ttls, "snapshot TTL must cover at least three one-minute build periods")
	if _, ok := store.Lookup(APIKeyRoutingScoreScope{Platform: PlatformOpenAI, ModelFamily: "gpt-5", EndpointKind: "responses"}, time.Minute, now); !ok {
		t.Fatal("request store did not receive published catalog with endpoint fallback")
	}
}

func TestRoutingScoreBuilderRefreshFailureDoesNotPublishIncompleteMetrics(t *testing.T) {
	source := &routingScoreSourceStub{refreshErr: errors.New("aggregation timeout")}
	cache := &routingScoreCacheStub{}
	builder := NewRoutingScoreBuilder(source, cache, NewAtomicAPIKeyRoutingScoreStore(), nil, nil)
	require.ErrorContains(t, builder.BuildOnce(context.Background()), "refresh routing metric buckets")
	require.Zero(t, source.calls)
	require.Empty(t, cache.published)
	require.EqualValues(t, 1, builder.failures.Load())
}

func TestRoutingScoreBuilderFollowerLoadsSnapshotThenTakesOverAfterLeaderRelease(t *testing.T) {
	now := time.Now().UTC()
	current := routingScoreSnapshotForTest(now)
	source := &routingScoreSourceStub{
		one: []APIKeyRoutingMetricAggregate{{Platform: PlatformOpenAI, GroupID: 1, Model: "gpt-5", SuccessRequests: 10, RateMultiplier: 1}},
		day: []APIKeyRoutingMetricAggregate{{Platform: PlatformOpenAI, GroupID: 1, Model: "gpt-5", SuccessRequests: 100, RateMultiplier: 1}},
	}
	cache := &routingScoreCacheStub{current: []*APIKeyRoutingScoreSnapshot{current}}
	store := NewAtomicAPIKeyRoutingScoreStore()
	lock := &fakeLeaderLockCache{}
	peerRelease, acquired := tryAcquireSingletonLeaderLock(context.Background(), lock, nil, routingScoreBuilderLeaderKey, "peer", 2*time.Minute)
	require.True(t, acquired)

	builder := NewRoutingScoreBuilder(source, cache, store, lock, nil)
	builder.now = func() time.Time { return now }
	require.NoError(t, builder.BuildOnce(context.Background()))
	require.Zero(t, source.calls, "a follower must not run the aggregation queries")
	require.Zero(t, source.refreshes, "a follower must not rewrite shared buckets")
	_, found := store.Lookup(APIKeyRoutingScoreScope{Platform: current.Platform, ModelFamily: current.ModelFamily, EndpointKind: current.EndpointKind}, time.Minute, now)
	require.True(t, found, "the follower must atomically install the current Redis catalog")

	peerRelease()
	require.NoError(t, builder.BuildOnce(context.Background()))
	require.Equal(t, 2, source.calls, "the follower must take over both 1h and 24h loads after leadership is released")
	require.NotEmpty(t, cache.published)
}

func TestSmoothRoutingScoreSnapshotBoundsTransientChangesButKeepsRawCounts(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	previous := &APIKeyRoutingScoreSnapshot{Groups: map[int64]APIKeyRoutingGroupObservation{
		1: {
			GroupID: 1, SuccessRequests: 95, FailedRequests: 5, SmoothedSuccessRate: .95,
			CapacityScore: .9, CacheHitRate: .8, NormalizedRate: 1, TTFTP50Ms: 100, DurationP50Ms: 1000,
		},
	}}
	current := &APIKeyRoutingScoreSnapshot{GeneratedAt: now, Groups: map[int64]APIKeyRoutingGroupObservation{
		1: {
			GroupID: 1, SuccessRequests: 40, FailedRequests: 60, SmoothedSuccessRate: .4,
			CapacityScore: .2, CacheHitRate: .1, NormalizedRate: 3, TTFTP50Ms: 500, DurationP50Ms: 5000,
		},
	}}

	SmoothAPIKeyRoutingScoreSnapshot(previous, current, .35)
	got := current.Groups[1]
	require.EqualValues(t, 40, got.SuccessRequests)
	require.EqualValues(t, 60, got.FailedRequests)
	require.InDelta(t, .85, got.SmoothedSuccessRate, 1e-9)
	require.InDelta(t, .75, got.CapacityScore, 1e-9)
	require.InDelta(t, .65, got.CacheHitRate, 1e-9)
	require.InDelta(t, 1.25, got.NormalizedRate, 1e-9)
	require.InDelta(t, 125, got.TTFTP50Ms, 1e-9)
	require.InDelta(t, 1250, got.DurationP50Ms, 1e-9)
}

func TestRoutingHardGateUsesRawRateEvenWhenSmoothedRateIsHealthy(t *testing.T) {
	policy := DefaultAPIKeyRoutingStrategyPolicy(APIKeySmartPreferenceBalanced)
	snapshot := &APIKeyRoutingScoreSnapshot{Groups: map[int64]APIKeyRoutingGroupObservation{
		1: {GroupID: 1, SuccessRequests: 4, FailedRequests: 6, SmoothedSuccessRate: .9, Confidence: 1, NormalizedRate: 1},
		2: {GroupID: 2, SuccessRequests: 9, FailedRequests: 1, SmoothedSuccessRate: .8, Confidence: 1, NormalizedRate: 1},
	}}
	ranked := RankAPIKeyRoutingCandidatesWithPolicy([]APIKeyRouteCandidate{{GroupID: 1}, {GroupID: 2}}, snapshot, policy)
	require.Equal(t, int64(2), ranked[0].GroupID)
	require.False(t, ranked[1].Eligible)
	require.Equal(t, "success_rate_below_50_percent", ranked[1].Exclusion)
}

func TestRoutingObservationFreshnessDecaysLowTrafficEvidence(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	require.InDelta(t, .5, routingObservationFreshness(now.Add(-15*time.Minute), now, "1h"), 1e-9)
	require.InDelta(t, .5, routingObservationFreshness(now.Add(-6*time.Hour), now, "24h"), 1e-9)
	require.Less(t, routingObservationFreshness(now.Add(-23*time.Hour), now, "24h"), .1)
}
