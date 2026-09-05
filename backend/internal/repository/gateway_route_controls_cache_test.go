package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRouteControlsEveryThresholdAndMinimumSamples(t *testing.T) {
	for minimum := 50; minimum <= 95; minimum += 5 {
		t.Run(fmt.Sprint(minimum), func(t *testing.T) {
			cache := newRouteHealthCacheTest(t)
			ctx, now := context.Background(), time.Now()
			key := service.APIKeyRouteHealthKey(1, 1, 2, "gpt-5", "responses")
			for index := 0; index < 20; index++ {
				state, err := cache.RecordAPIKeyRouteResultWithThreshold(ctx, key, index < minimum/5, now, 5*time.Minute, 20, 10, minimum, 1)
				require.NoError(t, err)
				require.Equal(t, "CLOSED", state, "not enough samples or exactly at threshold")
			}
			state, err := cache.RecordAPIKeyRouteResultWithThreshold(ctx, key, false, now, 5*time.Minute, 20, 10, minimum, 1)
			require.NoError(t, err)
			require.Equal(t, "OPEN", state)
		})
	}
}

func TestAPIKeyRouteControlsAreIsolatedAndReuseHistory(t *testing.T) {
	cache := newRouteHealthCacheTest(t)
	ctx, now := context.Background(), time.Now()
	first := service.APIKeyRouteHealthKey(1, 1, 2, "gpt-5", "responses")
	second := service.APIKeyRouteHealthKey(2, 1, 2, "gpt-5", "responses")
	for _, key := range []string{first, second} {
		for i := 0; i < 20; i++ {
			state, err := cache.RecordAPIKeyRouteResultWithThreshold(ctx, key, i < 13, now, 5*time.Minute, 20, 10, 50, 1)
			require.NoError(t, err)
			require.Equal(t, "CLOSED", state)
		}
	}
	allowed, state, err := cache.AllowAPIKeyRouteWithThreshold(ctx, first, now, 30*time.Second, 10*time.Second, 5*time.Minute, 20, 80, 2, false)
	require.NoError(t, err)
	require.False(t, allowed)
	require.Equal(t, "OPEN", state)
	allowed, state, err = cache.AllowAPIKeyRouteWithThreshold(ctx, second, now, 30*time.Second, 10*time.Second, 5*time.Minute, 20, 50, 1, false)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, "CLOSED", state)
	_, err = cache.RecordAPIKeyRouteResultWithThreshold(ctx, first, true, now, 5*time.Minute, 20, 10, 50, 1)
	require.NoError(t, err)
	require.Equal(t, "80", cache.rdb.HGet(ctx, first, "threshold_percent").Val(), "late old-policy requests cannot overwrite the new threshold")
	allowed, state, err = cache.AllowAPIKeyRouteWithThreshold(ctx, first, now, 30*time.Second, 10*time.Second, 5*time.Minute, 20, 50, 3, false)
	require.NoError(t, err)
	require.False(t, allowed)
	require.Equal(t, "OPEN", state, "lowering does not bypass cooldown")
	allowed, state, err = cache.AllowAPIKeyRouteWithThreshold(ctx, first, now.Add(31*time.Second), 30*time.Second, 10*time.Second, 5*time.Minute, 20, 50, 3, false)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, "HALF_OPEN", state)
}

func TestAPIKeyRouteControlsExpiredWindowDoesNotTripOnThresholdChange(t *testing.T) {
	cache := newRouteHealthCacheTest(t)
	ctx, now := context.Background(), time.Now()
	for i := 0; i < 20; i++ {
		_, err := cache.RecordAPIKeyRouteResultWithThreshold(ctx, "health:{7}:stale", i < 10, now, 5*time.Minute, 20, 10, 50, 1)
		require.NoError(t, err)
	}
	allowed, state, err := cache.AllowAPIKeyRouteWithThreshold(ctx, "health:{7}:stale", now.Add(6*time.Minute), 30*time.Second, 10*time.Second, 5*time.Minute, 20, 95, 2, false)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, "CLOSED", state)
}

func TestAPIKeyRouteControlsSharedGateStillAllowsBoundedRecovery(t *testing.T) {
	cache := newRouteHealthCacheTest(t)
	ctx, now := context.Background(), time.Now()
	key := "health:{4}:shared-gate"
	allowed, state, err := cache.AllowAPIKeyRouteWithThreshold(ctx, key, now, 30*time.Second, 10*time.Second, 5*time.Minute, 10, 85, 3, true)
	require.NoError(t, err)
	require.False(t, allowed)
	require.Equal(t, "OPEN", state)
	allowed, state, err = cache.AllowAPIKeyRouteWithThreshold(ctx, key, now.Add(31*time.Second), 30*time.Second, 10*time.Second, 5*time.Minute, 10, 85, 3, true)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, "HALF_OPEN", state)
	allowed, _, err = cache.AllowAPIKeyRouteWithThreshold(ctx, key, now.Add(31*time.Second), 30*time.Second, 10*time.Second, 5*time.Minute, 10, 85, 3, true)
	require.NoError(t, err)
	require.False(t, allowed, "one probe owner across instances")
}
