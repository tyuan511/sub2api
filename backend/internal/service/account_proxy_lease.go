package service

import (
	"context"
	"sync"
)

// proxyLeaseRegistry 收集单次请求内占用的「账号 × 代理」并发槽位，
// 由请求结束时的 cleanup 统一释放。
//
// 之所以做成请求作用域而不是挂在选号结果上：一次请求可能因故障转移经过
// 多个账号/代理，而释放点分散在各个网关 handler 里；请求级登记处让释放
// 时机唯一且不会漏（即使漏了，槽位仍有与账号槽位一致的 TTL 兜底）。
type proxyLeaseRegistry struct {
	mu          sync.Mutex
	sessionHash string
	released    bool
	// leases 按账号记录当前持有的「账号 × 代理」槽位租约：一个账号在同一请求里
	// 同一时刻只持有一个代理租约。租约随账号槽位一起释放（ChainProxyLeaseRelease），
	// 请求结束的 releaseAll 只是兜底。
	leases map[int64]*proxyLease
}

type proxyLease struct {
	proxyID int64
	release func()
}

type proxyLeaseRegistryKeyType struct{}

var proxyLeaseRegistryKey = proxyLeaseRegistryKeyType{}

// ContextWithProxyLeases 在请求上下文中安装代理租约登记处。
// 返回的 cleanup 必须在请求结束时调用（重复调用安全）。
func ContextWithProxyLeases(ctx context.Context) (context.Context, func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Value(proxyLeaseRegistryKey).(*proxyLeaseRegistry); ok {
		return ctx, func() {}
	}
	reg := &proxyLeaseRegistry{}
	return context.WithValue(ctx, proxyLeaseRegistryKey, reg), reg.releaseAll
}

func proxyLeaseRegistryFromContext(ctx context.Context) *proxyLeaseRegistry {
	if ctx == nil {
		return nil
	}
	reg, _ := ctx.Value(proxyLeaseRegistryKey).(*proxyLeaseRegistry)
	return reg
}

// SetProxyLeaseSessionHash 记录本次请求的会话标识，供代理粘性使用。
// 上下文里没有登记处（非网关路径）时是安全的空操作。
func SetProxyLeaseSessionHash(ctx context.Context, sessionHash string) {
	reg := proxyLeaseRegistryFromContext(ctx)
	if reg == nil || sessionHash == "" {
		return
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.sessionHash = sessionHash
}

func proxyLeaseSessionHash(ctx context.Context) string {
	reg := proxyLeaseRegistryFromContext(ctx)
	if reg == nil {
		return ""
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	return reg.sessionHash
}

// newProxyLease 生成本次请求的槽位标识；第二个返回值为 false 时
// 表示上下文里没有登记处，调用方不应占用槽位。
func newProxyLease(ctx context.Context) (string, bool) {
	if proxyLeaseRegistryFromContext(ctx) == nil {
		return "", false
	}
	return generateRequestID(), true
}

// leasedProxyForRequest 返回本次请求内该账号当前持有租约的代理（0 表示没有）。
// 同一请求里对同一账号的重复选号（粘性命中后再 hydrate、同一连接的后续 turn 等）
// 复用它，避免重复占用槽位。
func leasedProxyForRequest(ctx context.Context, accountID int64) int64 {
	reg := proxyLeaseRegistryFromContext(ctx)
	if reg == nil {
		return 0
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if lease := reg.leases[accountID]; lease != nil {
		return lease.proxyID
	}
	return 0
}

// registerProxyLease 记录账号当前持有的代理租约。同一账号已有租约时先释放旧的
// （正常不会发生：调用方先查 leasedProxyForRequest）。登记处已整体释放时立即释放。
func registerProxyLease(ctx context.Context, accountID, proxyID int64, release func()) {
	reg := proxyLeaseRegistryFromContext(ctx)
	if reg == nil || release == nil || accountID <= 0 {
		return
	}
	reg.mu.Lock()
	if reg.released {
		reg.mu.Unlock()
		release()
		return
	}
	if reg.leases == nil {
		reg.leases = make(map[int64]*proxyLease, 1)
	}
	previous := reg.leases[accountID]
	reg.leases[accountID] = &proxyLease{proxyID: proxyID, release: release}
	reg.mu.Unlock()
	if previous != nil && previous.release != nil {
		previous.release()
	}
}

// ReleaseProxyLease 释放账号在本次请求内持有的代理租约（无租约时为空操作）。
// 与账号槽位同生命周期：账号槽位释放（请求完成、failover 换号、WS 一个 turn 结束）
// 时必须一起调用，否则空闲期间代理容量会被无谓占用。
func ReleaseProxyLease(ctx context.Context, accountID int64) {
	reg := proxyLeaseRegistryFromContext(ctx)
	if reg == nil {
		return
	}
	reg.mu.Lock()
	lease := reg.leases[accountID]
	delete(reg.leases, accountID)
	reg.mu.Unlock()
	if lease != nil && lease.release != nil {
		lease.release()
	}
}

// ChainProxyLeaseRelease 把代理租约的释放挂到账号槽位的释放函数上，
// 保证两者同时归还。release 可为 nil。返回的函数可重复调用。
func ChainProxyLeaseRelease(ctx context.Context, accountID int64, release func()) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			if release != nil {
				release()
			}
			ReleaseProxyLease(ctx, accountID)
		})
	}
}

func (r *proxyLeaseRegistry) releaseAll() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.released {
		r.mu.Unlock()
		return
	}
	r.released = true
	leases := r.leases
	r.leases = nil
	r.mu.Unlock()

	for _, lease := range leases {
		if lease != nil && lease.release != nil {
			lease.release()
		}
	}
}
