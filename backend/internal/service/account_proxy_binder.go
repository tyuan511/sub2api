package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// ErrStickyProxyNotFound 表示会话在该账号上还没有绑定过代理。
var ErrStickyProxyNotFound = errors.New("sticky proxy not found")

// ErrAccountProxyPoolExhausted 表示账号多代理池里的每个代理都已达到各自的并发上限。
// 它包装了 ErrNoAvailableAccounts，网关按「无可用账号」处理（503 / 排队 / 换号），
// 绝不会在没有租约的情况下把请求压到已经满载的代理上。
var ErrAccountProxyPoolExhausted = fmt.Errorf("%w: account proxy pool exhausted", ErrNoAvailableAccounts)

// ErrAccountProxyPoolUnavailable 表示账号配置了多代理池，但池内没有任何可用代理
// （全部停用或过期）。代理池是该账号唯一出口，不会退回主 proxy_id 或直连。
var ErrAccountProxyPoolUnavailable = fmt.Errorf("%w: account proxy pool has no usable proxy", ErrNoAvailableAccounts)

// stickyProxyTTL 与账号粘性会话（stickySessionTTL）保持一致：
// 同一会话在同一账号上应尽量固定走同一个出口代理。
const stickyProxyTTL = time.Hour

// AccountProxyCache 是多代理池运行期需要的缓存能力。
// 它由并发缓存的具体实现附带提供（可选能力），未实现时多代理池退化为
// 「按顺序等权轮询、不做每代理并发限制」，不影响任何既有调度行为。
type AccountProxyCache interface {
	AcquireAccountProxySlot(ctx context.Context, accountID, proxyID int64, maxConcurrency int, requestID string) (bool, error)
	ReleaseAccountProxySlot(ctx context.Context, accountID, proxyID int64, requestID string) error
	GetAccountProxyConcurrencyBatch(ctx context.Context, accountID int64, proxyIDs []int64) (map[int64]int, error)
	GetStickyProxyID(ctx context.Context, accountID int64, sessionHash string) (int64, error)
	SetStickyProxyID(ctx context.Context, accountID, proxyID int64, sessionHash string, ttl time.Duration) error
}

// AccountProxyBinder 在账号被选中后为本次请求挑选一个出口代理。
//
// 调度策略与账号调度对齐：
//  1. 粘性优先——同一会话（session_hash）在同一账号上固定走同一个代理，
//     除非该代理已被移出代理池、不可用或已占满；
//  2. 否则在可用代理里按负载率最低优先挑选，负载率相同的代理等权轮询，
//     即每个代理权重相同；
//  3. 每个代理各自的并发上限由「账号 × 代理」维度的槽位保证。
//
// 账号没有配置多代理池时该组件完全不介入，账号继续使用 proxy_id 单代理。
type AccountProxyBinder struct {
	cache AccountProxyCache

	// rr 是进程内的等权轮询游标，key 为 accountID。
	// 仅用于打破负载相同时的平局，无需跨实例强一致。
	rr sync.Map
}

func NewAccountProxyBinder(cache AccountProxyCache) *AccountProxyBinder {
	if cache == nil {
		return nil
	}
	return &AccountProxyBinder{cache: cache}
}

func (b *AccountProxyBinder) nextRoundRobin(accountID int64, n int) int {
	if n <= 0 {
		return 0
	}
	v, _ := b.rr.LoadOrStore(accountID, new(uint64))
	counter, ok := v.(*uint64)
	if !ok {
		return 0
	}
	return int((atomic.AddUint64(counter, 1) - 1) % uint64(n))
}

// usableProxyPool 过滤出当前可用于出站的绑定：代理实体已加载、状态可用且未过期。
func usableProxyPool(pool []AccountProxy, now time.Time) []AccountProxy {
	out := make([]AccountProxy, 0, len(pool))
	for _, b := range pool {
		if b.Proxy == nil || b.ProxyID <= 0 {
			continue
		}
		if !b.Proxy.IsActive() || b.Proxy.IsExpired(now) {
			continue
		}
		out = append(out, b)
	}
	return out
}

// Bind 为本次请求在账号的多代理池里选出一个代理并占用「账号 × 代理」槽位，
// 把它写回账号的 ProxyID/Proxy 字段，使所有既有出站链路无需改动即可生效。
// 租约通过 ChainProxyLeaseRelease / ReleaseProxyLease 与账号槽位一起归还。
//
// 返回的账号是入参的浅拷贝；账号未配置多代理池时原样返回。
//
// 池里每个代理都已达到各自并发上限时返回 ErrAccountProxyPoolExhausted；池内没有
// 任何可用代理时返回 ErrAccountProxyPoolUnavailable；Redis 占槽失败时返回该错误
// （fail-close）。每代理并发是硬上限，任何情况下都不会无租约地使用某个代理。
func (b *AccountProxyBinder) Bind(ctx context.Context, account *Account) (*Account, error) {
	return b.bind(ctx, account, true)
}

// Route 只选路、不占槽位：用于不消耗生成容量的调用（token 计数、模型列表、
// 抢账号槽位之前的预选等）。已持有租约的账号复用其租约代理，保持一致。
func (b *AccountProxyBinder) Route(ctx context.Context, account *Account) (*Account, error) {
	return b.bind(ctx, account, false)
}

func (b *AccountProxyBinder) bind(ctx context.Context, account *Account, lease bool) (*Account, error) {
	if b == nil || account == nil || !account.HasProxyPool() {
		return account, nil
	}
	candidates := usableProxyPool(account.SortedProxyPool(), time.Now())
	if len(candidates) == 0 {
		// 配置了代理池就以池为唯一出口：全部停用/过期时拒绝，而不是退回
		// 可能同样已失效的主 proxy_id 或直连。正常情况下 IsSchedulable 已把
		// 这种账号挡在选号之外，这里是快照滞后时的兜底。
		slog.Debug("account_proxy.pool_unavailable", "account_id", account.ID, "pool_size", len(account.Proxies))
		return nil, ErrAccountProxyPoolUnavailable
	}

	// 同一请求内该账号已持有代理租约：复用，不再占槽。
	if leasedID := leasedProxyForRequest(ctx, account.ID); leasedID > 0 {
		for _, candidate := range candidates {
			if candidate.ProxyID == leasedID {
				return bindProxyToAccount(account, candidate), nil
			}
		}
	}

	sessionHash := proxyLeaseSessionHash(ctx)
	ordered := b.orderCandidates(ctx, account, candidates, sessionHash)

	// 只有要求租约、且请求作用域内存在租约登记处时才真正占用「账号 × 代理」槽位，
	// 否则（选路预览、后台任务、账号测试等）只做选路，不消耗容量。
	requestID, canLease := newProxyLease(ctx)
	canLease = canLease && lease

	for _, candidate := range ordered {
		if !canLease {
			return bindProxyToAccount(account, candidate), nil
		}
		acquired, err := b.cache.AcquireAccountProxySlot(ctx, account.ID, candidate.ProxyID, candidate.Concurrency, requestID)
		if err != nil {
			// 与账号级槽位（ConcurrencyService.AcquireAccountSlot）一致：Redis 不可用时
			// fail-close。每代理并发是硬上限，不能在无法计数时放行。
			slog.Warn("account_proxy.acquire_failed",
				"account_id", account.ID, "proxy_id", candidate.ProxyID, "error", err)
			return nil, fmt.Errorf("acquire account %d proxy %d slot: %w", account.ID, candidate.ProxyID, err)
		}
		if !acquired {
			continue
		}
		proxyID := candidate.ProxyID
		registerProxyLease(ctx, account.ID, proxyID, func() {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := b.cache.ReleaseAccountProxySlot(releaseCtx, account.ID, proxyID, requestID); err != nil {
				slog.Debug("account_proxy.release_failed",
					"account_id", account.ID, "proxy_id", proxyID, "error", err)
			}
		})
		if sessionHash != "" {
			_ = b.cache.SetStickyProxyID(ctx, account.ID, proxyID, sessionHash, stickyProxyTTL)
		}
		return bindProxyToAccount(account, candidate), nil
	}

	// 所有代理都占满：账号级并发正常时不会走到这里（账号总并发 = 各代理之和），
	// 出现即说明账号级/代理级计数暂时不一致或调用方没有账号级租约。
	// 这时拒绝而不是硬塞，交给上层按「无可用账号」排队或换号。
	slog.Debug("account_proxy.pool_exhausted", "account_id", account.ID, "candidates", len(ordered))
	return nil, ErrAccountProxyPoolExhausted
}

// orderCandidates 返回按「粘性 → 负载率 → 等权轮询」排序后的候选代理。
func (b *AccountProxyBinder) orderCandidates(ctx context.Context, account *Account, candidates []AccountProxy, sessionHash string) []AccountProxy {
	proxyIDs := make([]int64, 0, len(candidates))
	for _, c := range candidates {
		proxyIDs = append(proxyIDs, c.ProxyID)
	}

	loads, err := b.cache.GetAccountProxyConcurrencyBatch(ctx, account.ID, proxyIDs)
	if err != nil {
		slog.Debug("account_proxy.load_batch_failed", "account_id", account.ID, "error", err)
		loads = nil
	}

	// 等权轮询：以轮询游标为起点旋转候选顺序，负载率相同的代理机会均等。
	offset := b.nextRoundRobin(account.ID, len(candidates))
	rotated := make([]AccountProxy, 0, len(candidates))
	for i := range candidates {
		rotated = append(rotated, candidates[(offset+i)%len(candidates)])
	}

	// 稳定排序保留旋转后的相对顺序，只把负载率更低的代理提前。
	loadRate := func(c AccountProxy) int {
		max := c.Concurrency
		if max <= 0 {
			max = DefaultAccountProxyConcurrency
		}
		return loads[c.ProxyID] * 100 / max
	}
	stableSortByKey(rotated, loadRate)

	if sessionHash == "" {
		return rotated
	}
	stickyProxyID, err := b.cache.GetStickyProxyID(ctx, account.ID, sessionHash)
	if err != nil || stickyProxyID <= 0 {
		return rotated
	}
	for i, c := range rotated {
		if c.ProxyID != stickyProxyID {
			continue
		}
		// 命中粘性：把绑定的代理提到最前，其余顺序不变。
		out := make([]AccountProxy, 0, len(rotated))
		out = append(out, c)
		out = append(out, rotated[:i]...)
		out = append(out, rotated[i+1:]...)
		return out
	}
	return rotated
}

// stableSortByKey 是一个不引入额外依赖的稳定插入排序，候选代理数量很小。
func stableSortByKey(items []AccountProxy, key func(AccountProxy) int) {
	for i := 1; i < len(items); i++ {
		cur := items[i]
		curKey := key(cur)
		j := i - 1
		for j >= 0 && key(items[j]) > curKey {
			items[j+1] = items[j]
			j--
		}
		items[j+1] = cur
	}
}

// bindProxyToAccount 返回把出站代理换成 binding 的账号浅拷贝。
func bindProxyToAccount(account *Account, binding AccountProxy) *Account {
	bound := *account
	proxyID := binding.ProxyID
	bound.ProxyID = &proxyID
	bound.Proxy = binding.Proxy
	return &bound
}
