//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeAccountProxyCache struct {
	slots  map[int64]int // proxyID -> in-flight
	limits map[int64]int // proxyID -> max reached during acquire
	sticky map[string]int64
	acqErr error
}

func newFakeAccountProxyCache() *fakeAccountProxyCache {
	return &fakeAccountProxyCache{
		slots:  map[int64]int{},
		limits: map[int64]int{},
		sticky: map[string]int64{},
	}
}

func (f *fakeAccountProxyCache) AcquireAccountProxySlot(_ context.Context, _, proxyID int64, maxConcurrency int, _ string) (bool, error) {
	if f.acqErr != nil {
		return false, f.acqErr
	}
	if maxConcurrency > 0 && f.slots[proxyID] >= maxConcurrency {
		return false, nil
	}
	f.slots[proxyID]++
	f.limits[proxyID] = maxConcurrency
	return true, nil
}

func (f *fakeAccountProxyCache) ReleaseAccountProxySlot(_ context.Context, _, proxyID int64, _ string) error {
	if f.slots[proxyID] > 0 {
		f.slots[proxyID]--
	}
	return nil
}

func (f *fakeAccountProxyCache) GetAccountProxyConcurrencyBatch(_ context.Context, _ int64, proxyIDs []int64) (map[int64]int, error) {
	out := make(map[int64]int, len(proxyIDs))
	for _, id := range proxyIDs {
		out[id] = f.slots[id]
	}
	return out, nil
}

func (f *fakeAccountProxyCache) GetStickyProxyID(_ context.Context, accountID int64, sessionHash string) (int64, error) {
	proxyID, ok := f.sticky[stickyKey(accountID, sessionHash)]
	if !ok {
		return 0, ErrStickyProxyNotFound
	}
	return proxyID, nil
}

func (f *fakeAccountProxyCache) SetStickyProxyID(_ context.Context, accountID, proxyID int64, sessionHash string, _ time.Duration) error {
	f.sticky[stickyKey(accountID, sessionHash)] = proxyID
	return nil
}

func stickyKey(accountID int64, sessionHash string) string {
	return fmt.Sprintf("%d:%s", accountID, sessionHash)
}

func testProxy(id int64, name string) *Proxy {
	return &Proxy{ID: id, Name: name, Protocol: "http", Host: "10.0.0." + name, Port: 8080, Status: StatusActive}
}

func accountWithPool(bindings ...AccountProxy) *Account {
	total := 0
	for i := range bindings {
		total += bindings[i].Concurrency
	}
	first := bindings[0].ProxyID
	return &Account{
		ID:          7,
		Concurrency: total,
		ProxyID:     &first,
		Proxy:       bindings[0].Proxy,
		Proxies:     bindings,
	}
}

func TestBindLeavesSingleProxyAccountsUntouched(t *testing.T) {
	binder := NewAccountProxyBinder(newFakeAccountProxyCache())
	proxyID := int64(3)
	account := &Account{ID: 1, ProxyID: &proxyID, Proxy: testProxy(3, "a"), Concurrency: 5}

	ctx, release := ContextWithProxyLeases(context.Background())
	defer release()

	bound, err := binder.Bind(ctx, account)
	require.NoError(t, err)
	require.Same(t, account, bound, "accounts without a proxy pool must be returned as-is")
	require.Equal(t, int64(3), *bound.ProxyID)
}

func TestBindLeavesProxylessAccountsUntouched(t *testing.T) {
	binder := NewAccountProxyBinder(newFakeAccountProxyCache())
	account := &Account{ID: 1, Concurrency: 5}

	ctx, release := ContextWithProxyLeases(context.Background())
	defer release()

	bound, err := binder.Bind(ctx, account)
	require.NoError(t, err)
	require.Same(t, account, bound)
	require.Nil(t, bound.ProxyID)
}

func TestBindSpreadsRequestsAcrossProxiesWithEqualWeight(t *testing.T) {
	cache := newFakeAccountProxyCache()
	binder := NewAccountProxyBinder(cache)
	account := accountWithPool(
		AccountProxy{AccountID: 7, ProxyID: 11, Concurrency: 4, SortOrder: 0, Proxy: testProxy(11, "a")},
		AccountProxy{AccountID: 7, ProxyID: 12, Concurrency: 4, SortOrder: 1, Proxy: testProxy(12, "b")},
	)

	picked := map[int64]int{}
	releases := make([]func(), 0, 8)
	for i := 0; i < 8; i++ {
		ctx, release := ContextWithProxyLeases(context.Background())
		releases = append(releases, release)
		bound, err := binder.Bind(ctx, account)
		require.NoError(t, err)
		picked[*bound.ProxyID]++
	}
	for _, release := range releases {
		release()
	}

	require.Equal(t, 4, picked[11], "equal-capacity proxies must receive equal shares")
	require.Equal(t, 4, picked[12])
	require.Equal(t, 0, cache.slots[11], "request-scoped leases must be released")
	require.Equal(t, 0, cache.slots[12])
}

func TestBindRespectsPerProxyConcurrency(t *testing.T) {
	cache := newFakeAccountProxyCache()
	binder := NewAccountProxyBinder(cache)
	account := accountWithPool(
		AccountProxy{AccountID: 7, ProxyID: 11, Concurrency: 1, SortOrder: 0, Proxy: testProxy(11, "a")},
		AccountProxy{AccountID: 7, ProxyID: 12, Concurrency: 3, SortOrder: 1, Proxy: testProxy(12, "b")},
	)

	picked := map[int64]int{}
	releases := make([]func(), 0, 4)
	for i := 0; i < 4; i++ {
		ctx, release := ContextWithProxyLeases(context.Background())
		releases = append(releases, release)
		bound, err := binder.Bind(ctx, account)
		require.NoError(t, err)
		picked[*bound.ProxyID]++
	}

	require.Equal(t, 1, picked[11], "a proxy must never exceed its own concurrency")
	require.Equal(t, 3, picked[12])

	// 四个请求仍在途、池内每个代理都满了：第五个请求必须被拒绝，
	// 而不是无租约地塞给某个满载代理。
	ctx, release := ContextWithProxyLeases(context.Background())
	bound, err := binder.Bind(ctx, account)
	require.ErrorIs(t, err, ErrAccountProxyPoolExhausted)
	require.ErrorIs(t, err, ErrNoAvailableAccounts, "exhaustion must read as no-available-accounts to the gateway")
	require.Nil(t, bound)
	require.Equal(t, 1, cache.slots[11], "a rejected request must not leave a slot behind")
	require.Equal(t, 3, cache.slots[12])
	release()

	for _, release := range releases {
		release()
	}
	require.Equal(t, 0, cache.slots[11])
	require.Equal(t, 0, cache.slots[12])
}

func TestBindExhaustedPoolRecoversAfterRelease(t *testing.T) {
	cache := newFakeAccountProxyCache()
	binder := NewAccountProxyBinder(cache)
	account := accountWithPool(
		AccountProxy{AccountID: 7, ProxyID: 11, Concurrency: 1, SortOrder: 0, Proxy: testProxy(11, "a")},
	)

	ctx1, release1 := ContextWithProxyLeases(context.Background())
	_, err := binder.Bind(ctx1, account)
	require.NoError(t, err)

	ctx2, release2 := ContextWithProxyLeases(context.Background())
	defer release2()
	_, err = binder.Bind(ctx2, account)
	require.ErrorIs(t, err, ErrAccountProxyPoolExhausted)

	release1()
	ctx3, release3 := ContextWithProxyLeases(context.Background())
	defer release3()
	bound, err := binder.Bind(ctx3, account)
	require.NoError(t, err)
	require.Equal(t, int64(11), *bound.ProxyID)
}

func TestBindReusesTheSameProxyWithinOneRequest(t *testing.T) {
	cache := newFakeAccountProxyCache()
	binder := NewAccountProxyBinder(cache)
	account := accountWithPool(
		AccountProxy{AccountID: 7, ProxyID: 11, Concurrency: 4, SortOrder: 0, Proxy: testProxy(11, "a")},
		AccountProxy{AccountID: 7, ProxyID: 12, Concurrency: 4, SortOrder: 1, Proxy: testProxy(12, "b")},
	)

	ctx, release := ContextWithProxyLeases(context.Background())
	defer release()

	first, err := binder.Bind(ctx, account)
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		again, err := binder.Bind(ctx, account)
		require.NoError(t, err)
		require.Equal(t, *first.ProxyID, *again.ProxyID,
			"re-selecting the same account in one request must reuse its proxy")
	}
	require.Equal(t, 1, cache.slots[*first.ProxyID], "one request must hold exactly one proxy slot")
}

func TestBindKeepsSessionOnTheSameProxy(t *testing.T) {
	cache := newFakeAccountProxyCache()
	binder := NewAccountProxyBinder(cache)
	account := accountWithPool(
		AccountProxy{AccountID: 7, ProxyID: 11, Concurrency: 4, SortOrder: 0, Proxy: testProxy(11, "a")},
		AccountProxy{AccountID: 7, ProxyID: 12, Concurrency: 4, SortOrder: 1, Proxy: testProxy(12, "b")},
		AccountProxy{AccountID: 7, ProxyID: 13, Concurrency: 4, SortOrder: 2, Proxy: testProxy(13, "c")},
	)

	first := int64(0)
	for i := 0; i < 5; i++ {
		ctx, release := ContextWithProxyLeases(context.Background())
		SetProxyLeaseSessionHash(ctx, "session-abc")
		bound, err := binder.Bind(ctx, account)
		require.NoError(t, err)
		if i == 0 {
			first = *bound.ProxyID
		} else {
			require.Equal(t, first, *bound.ProxyID, "the same session must stay on the same proxy")
		}
		release()
	}
}

func TestBindSkipsUnusableProxies(t *testing.T) {
	cache := newFakeAccountProxyCache()
	binder := NewAccountProxyBinder(cache)
	disabled := testProxy(11, "a")
	disabled.Status = "inactive"
	expired := testProxy(12, "b")
	past := time.Now().Add(-time.Hour)
	expired.ExpiresAt = &past

	account := accountWithPool(
		AccountProxy{AccountID: 7, ProxyID: 11, Concurrency: 4, SortOrder: 0, Proxy: disabled},
		AccountProxy{AccountID: 7, ProxyID: 12, Concurrency: 4, SortOrder: 1, Proxy: expired},
		AccountProxy{AccountID: 7, ProxyID: 13, Concurrency: 4, SortOrder: 2, Proxy: testProxy(13, "c")},
	)

	for i := 0; i < 3; i++ {
		ctx, release := ContextWithProxyLeases(context.Background())
		bound, err := binder.Bind(ctx, account)
		require.NoError(t, err)
		require.Equal(t, int64(13), *bound.ProxyID)
		release()
	}
}

func TestBindWithoutLeaseRegistryDoesNotConsumeSlots(t *testing.T) {
	cache := newFakeAccountProxyCache()
	binder := NewAccountProxyBinder(cache)
	account := accountWithPool(
		AccountProxy{AccountID: 7, ProxyID: 11, Concurrency: 1, SortOrder: 0, Proxy: testProxy(11, "a")},
	)

	bound, err := binder.Bind(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, int64(11), *bound.ProxyID)
	require.Equal(t, 0, cache.slots[11], "non-request paths must not consume proxy capacity")
}

func TestNormalizeAccountProxiesDedupesAndOrders(t *testing.T) {
	out := NormalizeAccountProxies(9, []AccountProxy{
		{ProxyID: 5, Concurrency: 0},
		{ProxyID: 5, Concurrency: 7},
		{ProxyID: 0, Concurrency: 2},
		{ProxyID: 6, Concurrency: 2},
	})

	require.Len(t, out, 2)
	require.Equal(t, int64(9), out[0].AccountID)
	require.Equal(t, int64(5), out[0].ProxyID)
	require.Equal(t, DefaultAccountProxyConcurrency, out[0].Concurrency)
	require.Equal(t, 0, out[0].SortOrder)
	require.Equal(t, int64(6), out[1].ProxyID)
	require.Equal(t, 2, out[1].Concurrency)
	require.Equal(t, 1, out[1].SortOrder)
}

func TestResolveAccountProxyPoolSumsConcurrency(t *testing.T) {
	pool, primary, total := resolveAccountProxyPool(9, []AccountProxy{
		{ProxyID: 5, Concurrency: 3},
		{ProxyID: 6, Concurrency: 4},
	})

	require.Len(t, pool, 2)
	require.NotNil(t, primary)
	require.Equal(t, int64(5), *primary, "primary proxy keeps proxy_id populated for legacy paths")
	require.Equal(t, 7, total)

	// 只选一个代理时退化为单代理：不建池，但仍写回 proxy_id / concurrency。
	pool, primary, total = resolveAccountProxyPool(9, []AccountProxy{{ProxyID: 5, Concurrency: 6}})
	require.Nil(t, pool, "a single proxy must not create a pool")
	require.NotNil(t, primary)
	require.Equal(t, int64(5), *primary)
	require.Equal(t, 6, total)

	pool, primary, total = resolveAccountProxyPool(9, nil)
	require.Nil(t, pool)
	require.Nil(t, primary)
	require.Zero(t, total)
}

func TestProxyLeaseIsReleasedWithTheAccountSlot(t *testing.T) {
	cache := newFakeAccountProxyCache()
	binder := NewAccountProxyBinder(cache)
	account := accountWithPool(
		AccountProxy{AccountID: 7, ProxyID: 11, Concurrency: 1, SortOrder: 0, Proxy: testProxy(11, "a")},
	)

	ctx, releaseAll := ContextWithProxyLeases(context.Background())
	defer releaseAll()

	accountSlotReleased := false
	bound, err := binder.Bind(ctx, account)
	require.NoError(t, err)
	require.Equal(t, 1, cache.slots[*bound.ProxyID])

	// 账号槽位释放（请求完成 / failover / WS turn 结束）必须同时归还代理租约。
	release := ChainProxyLeaseRelease(ctx, account.ID, func() { accountSlotReleased = true })
	release()
	require.True(t, accountSlotReleased)
	require.Equal(t, 0, cache.slots[11], "proxy lease must not outlive the account slot")
	require.Zero(t, leasedProxyForRequest(ctx, account.ID), "released lease must clear the per-request binding")

	// 同一请求（同一 WS 连接）的下一个 turn 可以重新拿到这个代理。
	again, err := binder.Bind(ctx, account)
	require.NoError(t, err)
	require.Equal(t, int64(11), *again.ProxyID)
	require.Equal(t, 1, cache.slots[11])

	release() // 重复调用安全
	require.Equal(t, 1, cache.slots[11], "a chained release runs only once")
}

func TestRouteDoesNotConsumeProxySlots(t *testing.T) {
	cache := newFakeAccountProxyCache()
	binder := NewAccountProxyBinder(cache)
	account := accountWithPool(
		AccountProxy{AccountID: 7, ProxyID: 11, Concurrency: 1, SortOrder: 0, Proxy: testProxy(11, "a")},
		AccountProxy{AccountID: 7, ProxyID: 12, Concurrency: 1, SortOrder: 1, Proxy: testProxy(12, "b")},
	)

	ctx, release := ContextWithProxyLeases(context.Background())
	defer release()

	// 预选 / token 计数 / 模型列表：只选路，不占槽。
	for i := 0; i < 5; i++ {
		routed, err := binder.Route(ctx, account)
		require.NoError(t, err)
		require.NotNil(t, routed.ProxyID)
	}
	require.Equal(t, 0, cache.slots[11]+cache.slots[12])

	// 之后真正绑定时才占槽，且 Route 与 Bind 之间不会互相占用对方的租约。
	bound, err := binder.Bind(ctx, account)
	require.NoError(t, err)
	require.Equal(t, 1, cache.slots[*bound.ProxyID])

	// 已持有租约后 Route 复用同一个代理，保持一致。
	routed, err := binder.Route(ctx, account)
	require.NoError(t, err)
	require.Equal(t, *bound.ProxyID, *routed.ProxyID)
	require.Equal(t, 1, cache.slots[11]+cache.slots[12])
}

func TestFailoverReleasingOldAccountFreesItsProxy(t *testing.T) {
	cache := newFakeAccountProxyCache()
	binder := NewAccountProxyBinder(cache)
	first := accountWithPool(
		AccountProxy{AccountID: 7, ProxyID: 11, Concurrency: 1, SortOrder: 0, Proxy: testProxy(11, "a")},
	)
	second := &Account{
		ID:          8,
		Concurrency: 1,
		Proxies: []AccountProxy{
			{AccountID: 8, ProxyID: 11, Concurrency: 1, SortOrder: 0, Proxy: testProxy(11, "a")},
		},
	}

	ctx, releaseAll := ContextWithProxyLeases(context.Background())
	defer releaseAll()

	_, err := binder.Bind(ctx, first)
	require.NoError(t, err)
	releaseFirst := ChainProxyLeaseRelease(ctx, first.ID, nil)

	// failover：释放旧账号槽位后再绑定新账号，旧账号的代理槽位必须已经归还。
	releaseFirst()
	require.Equal(t, 0, cache.slots[11])
	_, err = binder.Bind(ctx, second)
	require.NoError(t, err)
	require.Equal(t, 1, cache.slots[11])
}

func TestBindRejectsPoolWithNoUsableProxy(t *testing.T) {
	cache := newFakeAccountProxyCache()
	binder := NewAccountProxyBinder(cache)
	disabled := testProxy(11, "a")
	disabled.Status = "inactive"
	expired := testProxy(12, "b")
	past := time.Now().Add(-time.Hour)
	expired.ExpiresAt = &past
	account := accountWithPool(
		AccountProxy{AccountID: 7, ProxyID: 11, Concurrency: 4, SortOrder: 0, Proxy: disabled},
		AccountProxy{AccountID: 7, ProxyID: 12, Concurrency: 4, SortOrder: 1, Proxy: expired},
	)

	ctx, release := ContextWithProxyLeases(context.Background())
	defer release()

	// 配置了池就以池为唯一出口：全部不可用时拒绝，不退回主 proxy_id / 直连。
	bound, err := binder.Bind(ctx, account)
	require.ErrorIs(t, err, ErrAccountProxyPoolUnavailable)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Nil(t, bound)

	routed, err := binder.Route(ctx, account)
	require.ErrorIs(t, err, ErrAccountProxyPoolUnavailable)
	require.Nil(t, routed)

	// 调度前置过滤同样把它挡掉，让选号切到别的账号。
	account.Status = StatusActive
	account.Schedulable = true
	require.False(t, account.IsSchedulable())

	// 没有配置池的账号不受影响。
	legacyProxyID := int64(3)
	legacy := &Account{ID: 2, Status: StatusActive, Schedulable: true, ProxyID: &legacyProxyID, Proxy: disabled, Concurrency: 1}
	require.True(t, legacy.IsSchedulable())
}

func TestBindFailsClosedWhenSlotAcquireErrors(t *testing.T) {
	cache := newFakeAccountProxyCache()
	cache.acqErr = errors.New("redis: connection refused")
	binder := NewAccountProxyBinder(cache)
	account := accountWithPool(
		AccountProxy{AccountID: 7, ProxyID: 11, Concurrency: 4, SortOrder: 0, Proxy: testProxy(11, "a")},
		AccountProxy{AccountID: 7, ProxyID: 12, Concurrency: 4, SortOrder: 1, Proxy: testProxy(12, "b")},
	)

	ctx, release := ContextWithProxyLeases(context.Background())
	defer release()

	// 与账号级槽位一致：无法计数时不放行，不能突破每代理并发上限。
	bound, err := binder.Bind(ctx, account)
	require.Error(t, err)
	require.ErrorIs(t, err, cache.acqErr)
	require.Nil(t, bound)
	require.Zero(t, leasedProxyForRequest(ctx, account.ID))

	// 只选路（不占槽）的调用不受 Redis 占槽失败影响。
	routed, err := binder.Route(ctx, account)
	require.NoError(t, err)
	require.NotNil(t, routed.ProxyID)
}
