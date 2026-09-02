package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestLiveLeaseReplacesRegularSlotsAndCountsTowardLimits(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	regular := NewConcurrencyCache(client, 15, 900)
	live, ok := regular.(service.LiveConcurrencyCache)
	require.True(t, ok)
	ctx := context.Background()

	accountAcquired, err := regular.AcquireAccountSlot(ctx, 10, 1, "regular-account")
	require.NoError(t, err)
	require.True(t, accountAcquired)
	userAcquired, err := regular.AcquireUserSlot(ctx, 20, 1, "regular-user")
	require.NoError(t, err)
	require.True(t, userAcquired)

	acquired, err := live.AcquireLiveLease(ctx, 10, 1, 20, 1, 30, "live-lease", true)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NoError(t, regular.ReleaseAccountSlot(ctx, 10, "regular-account"))
	require.NoError(t, regular.ReleaseUserSlot(ctx, 20, "regular-user"))

	accountCount, err := regular.GetAccountConcurrency(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, accountCount)
	userCount, err := regular.GetUserConcurrency(ctx, 20)
	require.NoError(t, err)
	require.Equal(t, 1, userCount)
	accountAcquired, err = regular.AcquireAccountSlot(ctx, 10, 1, "ordinary-blocked")
	require.NoError(t, err)
	require.False(t, accountAcquired)

	refreshed, err := live.RefreshLiveLease(ctx, 10, 20, 30, "live-lease")
	require.NoError(t, err)
	require.True(t, refreshed)
	require.NoError(t, live.ReleaseLiveLease(ctx, 10, 20, 30, "live-lease"))
	accountAcquired, err = regular.AcquireAccountSlot(ctx, 10, 1, "ordinary-allowed")
	require.NoError(t, err)
	require.True(t, accountAcquired)
}

func TestLiveLeaseExpiresWithoutRefresh(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	regular := NewConcurrencyCache(client, 15, 900)
	live, ok := regular.(service.LiveConcurrencyCache)
	require.True(t, ok)
	ctx := context.Background()

	acquired, err := live.AcquireLiveLease(ctx, 10, 1, 20, 1, 30, "expired-live", false)
	require.NoError(t, err)
	require.True(t, acquired)

	redisServer.FastForward(61 * time.Second)
	acquired, err = regular.AcquireAccountSlot(ctx, 10, 1, "ordinary-after-expiry")
	require.NoError(t, err)
	require.True(t, acquired)
	refreshed, err := live.RefreshLiveLease(ctx, 10, 20, 30, "expired-live")
	require.NoError(t, err)
	require.False(t, refreshed)
}

func TestAccountProxyLiveLeaseReplacesRegularSlotAndCountsTowardBindingLimit(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	regular := NewConcurrencyCache(client, 15, 900)
	proxyRegular, ok := regular.(service.AccountProxyConcurrencyCache)
	require.True(t, ok)
	proxyLive, ok := regular.(service.AccountProxyLiveConcurrencyCache)
	require.True(t, ok)
	ctx := context.Background()

	acquired, err := proxyRegular.AcquireAccountProxySlot(ctx, 10, 20, 1, "regular-proxy")
	require.NoError(t, err)
	require.True(t, acquired)
	redisServer.FastForward(61 * time.Second)

	acquired, err = proxyLive.AcquireAccountProxyLiveLease(ctx, 10, 20, 1, "live-proxy", true)
	require.NoError(t, err)
	require.True(t, acquired)
	_, err = client.ZScore(ctx, accountProxySlotKey(10, 20), "regular-proxy").Result()
	require.NoError(t, err, "promoting a Live call must not expire a valid ordinary proxy slot after 60 seconds")
	require.NoError(t, proxyRegular.ReleaseAccountProxySlot(ctx, 10, 20, "regular-proxy"))

	// The live lease occupies the only proxy binding slot. The allowance is
	// only for the regular slot being replaced by the same Live call.
	acquired, err = proxyRegular.AcquireAccountProxySlot(ctx, 10, 20, 1, "ordinary-blocked")
	require.NoError(t, err)
	require.False(t, acquired)
	acquired, err = proxyLive.AcquireAccountProxyLiveLease(ctx, 10, 20, 1, "second-live", false)
	require.NoError(t, err)
	require.False(t, acquired)

	refreshed, err := proxyLive.RefreshAccountProxyLiveLease(ctx, 10, 20, "live-proxy")
	require.NoError(t, err)
	require.True(t, refreshed)
	require.NoError(t, proxyLive.ReleaseAccountProxyLiveLease(ctx, 10, 20, "live-proxy"))

	acquired, err = proxyRegular.AcquireAccountProxySlot(ctx, 10, 20, 1, "ordinary-allowed")
	require.NoError(t, err)
	require.True(t, acquired)
}

func TestAccountProxyLiveLeaseExpiresWithoutRefresh(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	regular := NewConcurrencyCache(client, 15, 900)
	proxyRegular, ok := regular.(service.AccountProxyConcurrencyCache)
	require.True(t, ok)
	proxyLive, ok := regular.(service.AccountProxyLiveConcurrencyCache)
	require.True(t, ok)
	ctx := context.Background()

	acquired, err := proxyLive.AcquireAccountProxyLiveLease(ctx, 10, 20, 1, "expired-proxy-live", false)
	require.NoError(t, err)
	require.True(t, acquired)

	redisServer.FastForward(61 * time.Second)
	acquired, err = proxyRegular.AcquireAccountProxySlot(ctx, 10, 20, 1, "ordinary-after-expiry")
	require.NoError(t, err)
	require.True(t, acquired)
	refreshed, err := proxyLive.RefreshAccountProxyLiveLease(ctx, 10, 20, "expired-proxy-live")
	require.NoError(t, err)
	require.False(t, refreshed)
}

func TestCleanupStaleProcessSlotsCleansAccountProxyRegularAndLiveSlots(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	cache := NewConcurrencyCache(client, 15, 900).(*concurrencyCache)
	ctx := context.Background()
	accountID, proxyID := int64(31), int64(41)
	regularKey := accountProxySlotKey(accountID, proxyID)
	liveKey := regularKey + ":live"
	member := accountProxyActiveIndexMember(accountID, proxyID)
	now, err := cache.redisUnixSeconds(ctx)
	require.NoError(t, err)

	require.NoError(t, client.ZAdd(ctx, regularKey, redis.Z{Score: float64(now), Member: "dead-regular"}).Err())
	require.NoError(t, client.ZAdd(ctx, liveKey, redis.Z{Score: float64(now), Member: "dead-live"}).Err())
	require.NoError(t, client.ZAdd(ctx, accountProxyActiveIndexKey, redis.Z{
		Score:  float64(now + 60),
		Member: member,
	}).Err())

	require.NoError(t, cache.CleanupStaleProcessSlots(ctx, "current-"))
	require.EqualValues(t, 0, client.Exists(ctx, regularKey).Val())
	require.EqualValues(t, 0, client.Exists(ctx, liveKey).Val())
	_, err = client.ZScore(ctx, accountProxyActiveIndexKey, member).Result()
	require.ErrorIs(t, err, redis.Nil)
}
