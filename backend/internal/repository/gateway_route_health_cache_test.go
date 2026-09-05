package repository

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type routeRuntimePipelineHook struct {
	pipelines atomic.Int64
	commands  atomic.Int64
}

func (*routeRuntimePipelineHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (*routeRuntimePipelineHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook { return next }

func (h *routeRuntimePipelineHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		h.pipelines.Add(1)
		h.commands.Add(int64(len(cmds)))
		return next(ctx, cmds)
	}
}

func newRouteHealthCacheTest(t *testing.T) *gatewayCache {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return &gatewayCache{rdb: client}
}

func TestAPIKeyRouteBreakerOpensOnlyBelowHalfSuccess(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0)
	window := 5 * time.Minute

	equalCache := newRouteHealthCacheTest(t)
	for i := 0; i < 5; i++ {
		state, err := equalCache.RecordAPIKeyRouteResult(ctx, "health:{1}:equal", true, now, window, 10, 10)
		require.NoError(t, err)
		require.Equal(t, "CLOSED", state)
	}
	for i := 0; i < 5; i++ {
		state, err := equalCache.RecordAPIKeyRouteResult(ctx, "health:{1}:equal", false, now, window, 10, 10)
		require.NoError(t, err)
		require.Equal(t, "CLOSED", state, "exactly 50 percent must not trip the breaker")
	}

	belowCache := newRouteHealthCacheTest(t)
	for i := 0; i < 4; i++ {
		_, err := belowCache.RecordAPIKeyRouteResult(ctx, "health:{1}:below", true, now, window, 10, 10)
		require.NoError(t, err)
	}
	var state string
	for i := 0; i < 6; i++ {
		var err error
		state, err = belowCache.RecordAPIKeyRouteResult(ctx, "health:{1}:below", false, now, window, 10, 10)
		require.NoError(t, err)
	}
	require.Equal(t, "OPEN", state)
}

func TestAPIKeyRouteBreakerSingleHalfOpenProbeAndRecoveryRamp(t *testing.T) {
	cache := newRouteHealthCacheTest(t)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0)
	key := "health:{9}:recovery"
	for i := 0; i < 6; i++ {
		_, err := cache.RecordAPIKeyRouteResult(ctx, key, false, now, 5*time.Minute, 6, 10)
		require.NoError(t, err)
	}

	allowed, state, err := cache.AllowAPIKeyRoute(ctx, key, now.Add(20*time.Second), 30*time.Second, 10*time.Second)
	require.NoError(t, err)
	require.False(t, allowed)
	require.Equal(t, "OPEN", state)

	allowed, state, err = cache.AllowAPIKeyRoute(ctx, key, now.Add(31*time.Second), 30*time.Second, 10*time.Second)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, "HALF_OPEN", state)
	allowed, state, err = cache.AllowAPIKeyRoute(ctx, key, now.Add(31*time.Second), 30*time.Second, 10*time.Second)
	require.NoError(t, err)
	require.False(t, allowed, "only one instance may own the half-open probe")
	require.Equal(t, "HALF_OPEN", state)

	state, err = cache.RecordAPIKeyRouteResult(ctx, key, true, now.Add(32*time.Second), 5*time.Minute, 6, 10)
	require.NoError(t, err)
	require.Equal(t, "RECOVERING", state)
	for i := 1; i <= 4; i++ {
		allowed, state, err = cache.AllowAPIKeyRoute(ctx, key, now.Add(33*time.Second), 30*time.Second, 10*time.Second)
		require.NoError(t, err)
		require.Equal(t, "RECOVERING", state)
		require.Equal(t, i == 4, allowed, "early recovery must ramp at one in four admissions")
	}

	state, err = cache.RecordAPIKeyRouteResult(ctx, key, false, now.Add(34*time.Second), 5*time.Minute, 6, 10)
	require.NoError(t, err)
	require.Equal(t, "OPEN", state, "a recovery failure must reopen immediately")
}

func TestAPIKeyRouteBreakerReclaimsExpiredHalfOpenProbeAcrossInstances(t *testing.T) {
	mr := miniredis.RunT(t)
	firstClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	secondClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = firstClient.Close()
		_ = secondClient.Close()
	})
	first := &gatewayCache{rdb: firstClient}
	second := &gatewayCache{rdb: secondClient}
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0)
	key := "api_key_route_health:{77}:v3:g9:test"
	for i := 0; i < 6; i++ {
		_, err := first.RecordAPIKeyRouteResult(ctx, key, false, now, 5*time.Minute, 6, 10)
		require.NoError(t, err)
	}

	allowed, state, err := first.AllowAPIKeyRoute(ctx, key, now.Add(31*time.Second), 30*time.Second, 10*time.Second)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, "HALF_OPEN", state)
	allowed, _, err = second.AllowAPIKeyRoute(ctx, key, now.Add(31*time.Second), 30*time.Second, 10*time.Second)
	require.NoError(t, err)
	require.False(t, allowed, "a live probe lease must exclude every other instance")

	mr.FastForward(11 * time.Second)
	allowed, state, err = second.AllowAPIKeyRoute(ctx, key, now.Add(42*time.Second), 30*time.Second, 10*time.Second)
	require.NoError(t, err)
	require.True(t, allowed, "an expired owner lease must not strand HALF_OPEN forever")
	require.Equal(t, "HALF_OPEN", state)
}

func TestAPIKeyRouteBreakerAllowsExactlyOneConcurrentHalfOpenProbe(t *testing.T) {
	mr := miniredis.RunT(t)
	const instances = 16
	clients := make([]*redis.Client, 0, instances)
	caches := make([]*gatewayCache, 0, instances)
	for i := 0; i < instances; i++ {
		client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		clients = append(clients, client)
		caches = append(caches, &gatewayCache{rdb: client})
	}
	t.Cleanup(func() {
		for _, client := range clients {
			_ = client.Close()
		}
	})
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0)
	key := "api_key_route_health:{101}:v4:g7:concurrent-probe"
	for i := 0; i < 6; i++ {
		_, err := caches[0].RecordAPIKeyRouteResult(ctx, key, false, now, 5*time.Minute, 6, 10)
		require.NoError(t, err)
	}

	var winners atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	type probeResult struct {
		allowed bool
		state   string
		err     error
	}
	results := make(chan probeResult, instances)
	for _, cache := range caches {
		wg.Add(1)
		go func(cache *gatewayCache) {
			defer wg.Done()
			<-start
			allowed, state, err := cache.AllowAPIKeyRoute(ctx, key, now.Add(31*time.Second), 30*time.Second, 10*time.Second)
			results <- probeResult{allowed: allowed, state: state, err: err}
			if allowed {
				winners.Add(1)
			}
		}(cache)
	}
	close(start)
	wg.Wait()
	close(results)
	for result := range results {
		require.NoError(t, result.err)
		require.Equal(t, "HALF_OPEN", result.state)
	}
	require.Equal(t, int64(1), winners.Load())
}

func TestAPIKeyRouteBreakerUsesRollingWindowBuckets(t *testing.T) {
	cache := newRouteHealthCacheTest(t)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0)
	key := "health:{12}:rolling"
	for i := 0; i < 6; i++ {
		state, err := cache.RecordAPIKeyRouteResult(ctx, key, true, now, 100*time.Second, 10, 10)
		require.NoError(t, err)
		require.Equal(t, "CLOSED", state)
	}
	// The old successes are outside the rolling window. Ten current failures
	// alone now cross the minimum sample and below-50% threshold.
	for i := 0; i < 10; i++ {
		state, err := cache.RecordAPIKeyRouteResult(ctx, key, false, now.Add(101*time.Second), 100*time.Second, 10, 10)
		require.NoError(t, err)
		if i == 9 {
			require.Equal(t, "OPEN", state)
		}
	}
}

func TestAPIKeyRouteBreakerRecoveryClosesAfterRequiredSuccesses(t *testing.T) {
	cache := newRouteHealthCacheTest(t)
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0)
	key := "api_key_route_health:{88}:v2:g5:recovery-close"
	for i := 0; i < 6; i++ {
		_, err := cache.RecordAPIKeyRouteResult(ctx, key, false, now, 5*time.Minute, 6, 4)
		require.NoError(t, err)
	}
	allowed, state, err := cache.AllowAPIKeyRoute(ctx, key, now.Add(31*time.Second), 30*time.Second, 10*time.Second)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, "HALF_OPEN", state)

	for i := 0; i < 4; i++ {
		state, err = cache.RecordAPIKeyRouteResult(ctx, key, true, now.Add(time.Duration(32+i)*time.Second), 5*time.Minute, 6, 4)
		require.NoError(t, err)
	}
	require.Equal(t, "CLOSED", state)
	allowed, state, err = cache.AllowAPIKeyRoute(ctx, key, now.Add(40*time.Second), 30*time.Second, 10*time.Second)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, "CLOSED", state)
}

func TestAPIKeyRouteBreakerStateExpiresAndVersionedKeysAreIndependent(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := &gatewayCache{rdb: client}
	ctx := context.Background()
	now := time.Unix(1_800_000_000, 0)
	v1 := "api_key_route_health:{99}:v1:g5:test"
	v2 := "api_key_route_health:{99}:v2:g5:test"

	for i := 0; i < 6; i++ {
		_, err := cache.RecordAPIKeyRouteResult(ctx, v1, false, now, time.Minute, 6, 4)
		require.NoError(t, err)
	}
	allowed, state, err := cache.AllowAPIKeyRoute(ctx, v1, now, 30*time.Second, 10*time.Second)
	require.NoError(t, err)
	require.False(t, allowed)
	require.Equal(t, "OPEN", state)
	allowed, state, err = cache.AllowAPIKeyRoute(ctx, v2, now, 30*time.Second, 10*time.Second)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, "CLOSED", state)

	mr.FastForward(61 * time.Second)
	require.False(t, mr.Exists(v1), "sparse breaker state must expire after its rolling window")
}

func TestAPIKeyRouteBreakerAdminOperationsAreBatchedAndExact(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := &gatewayCache{rdb: client}
	ctx := context.Background()
	keys := []string{
		"api_key_route_health:{7}:v4:g11:scope",
		"api_key_route_health:{7}:v4:g12:scope",
	}
	require.NoError(t, client.HSet(ctx, keys[0], "state", "OPEN", "successes", 4, "failures", 7, "opened_ms", 123).Err())
	require.NoError(t, client.HSet(ctx, keys[1], "state", "CLOSED").Err())
	require.NoError(t, client.Set(ctx, keys[0]+":probe", "owner", time.Minute).Err())

	states, err := cache.LoadAPIKeyRouteBreakers(ctx, keys)
	require.NoError(t, err)
	require.Len(t, states, 2)
	require.Equal(t, "OPEN", states[0].State)
	require.Equal(t, int64(4), states[0].Successes)
	require.Equal(t, int64(7), states[0].Failures)
	require.Equal(t, int64(123), states[0].OpenedAtUnixMS)
	require.Equal(t, "CLOSED", states[1].State)

	deleted, err := cache.DeleteAPIKeyRouteBreakers(ctx, []string{keys[0]})
	require.NoError(t, err)
	require.Equal(t, int64(2), deleted)
	require.False(t, mr.Exists(keys[0]))
	require.False(t, mr.Exists(keys[0]+":probe"))
	require.True(t, mr.Exists(keys[1]), "unrequested scope remains untouched")
}

func TestAPIKeyRouteRuntimeStateLoadsStickyAndEightBreakersInOnePipeline(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := &gatewayCache{rdb: client}
	ctx := context.Background()
	const apiKeyID int64 = 7
	const stickyKey = "api_key_group:{7}:v4:scope"
	require.NoError(t, cache.SetSessionAccountID(ctx, apiKeyID, stickyKey, 18, time.Minute))

	breakerKeys := make([]string, 8)
	for index := range breakerKeys {
		breakerKeys[index] = service.APIKeyRouteHealthKey(apiKeyID, 4, int64(index+11), "gpt-5", "responses")
		require.NoError(t, client.HSet(ctx, breakerKeys[index], "state", service.APIKeyRouteBreakerClosed, "successes", index+1).Err())
	}
	hook := &routeRuntimePipelineHook{}
	client.AddHook(hook)

	stickyGroupID, breakers, err := cache.LoadAPIKeyRouteRuntimeState(ctx, apiKeyID, stickyKey, breakerKeys)
	require.NoError(t, err)
	require.Equal(t, int64(18), stickyGroupID)
	require.Len(t, breakers, 8)
	for index, breaker := range breakers {
		require.Equal(t, service.APIKeyRouteBreakerClosed, breaker.State)
		require.Equal(t, int64(index+1), breaker.Successes)
	}
	require.Equal(t, int64(1), hook.pipelines.Load())
	require.Equal(t, int64(9), hook.commands.Load(), "one sticky GET plus eight breaker HGETALL commands share one round trip")
}
