package service

import (
	"sort"
	"time"
)

// AccountProxy 是账号与代理的一条绑定，携带该代理单独的并发上限。
//
// 一个账号可以绑定多个代理。绑定列表为空时账号完全退回旧行为：
// 只使用 Account.ProxyID / Account.Proxy 与 Account.Concurrency，
// 调度链路上的任何判断都不受影响。
type AccountProxy struct {
	AccountID   int64
	ProxyID     int64
	Concurrency int
	SortOrder   int

	Proxy *Proxy
}

// DefaultAccountProxyConcurrency 是单条代理绑定缺省的并发上限。
const DefaultAccountProxyConcurrency = 3

// HasProxyPool 报告账号是否配置了多代理池。
// 只有显式配置过绑定列表的账号才返回 true；历史账号（只有 proxy_id）返回 false。
func (a *Account) HasProxyPool() bool {
	return a != nil && len(a.Proxies) > 0
}

// HasUsableProxyPool 报告代理池里是否还有可用于出站的代理（状态可用且未过期）。
// 未配置代理池时返回 false。
func (a *Account) HasUsableProxyPool(now time.Time) bool {
	if a == nil || len(a.Proxies) == 0 {
		return false
	}
	return len(usableProxyPool(a.Proxies, now)) > 0
}

// ProxyPoolConcurrency 返回各代理并发之和。未配置代理池时返回 0。
func (a *Account) ProxyPoolConcurrency() int {
	if a == nil {
		return 0
	}
	total := 0
	for i := range a.Proxies {
		c := a.Proxies[i].Concurrency
		if c <= 0 {
			c = DefaultAccountProxyConcurrency
		}
		total += c
	}
	return total
}

// SortedProxyPool 返回按 sort_order（再按 proxy_id）排序后的绑定副本，
// 保证轮询与展示顺序在所有节点上一致。
func (a *Account) SortedProxyPool() []AccountProxy {
	if a == nil || len(a.Proxies) == 0 {
		return nil
	}
	out := make([]AccountProxy, len(a.Proxies))
	copy(out, a.Proxies)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SortOrder != out[j].SortOrder {
			return out[i].SortOrder < out[j].SortOrder
		}
		return out[i].ProxyID < out[j].ProxyID
	})
	return out
}

// NormalizeAccountProxies 去重、补默认值并重排 sort_order，用于写库前的入参归一化。
// 返回的切片按传入顺序保序，重复 proxy_id 只保留第一条。
func NormalizeAccountProxies(accountID int64, in []AccountProxy) []AccountProxy {
	if len(in) == 0 {
		return nil
	}
	out := make([]AccountProxy, 0, len(in))
	seen := make(map[int64]struct{}, len(in))
	for _, item := range in {
		if item.ProxyID <= 0 {
			continue
		}
		if _, dup := seen[item.ProxyID]; dup {
			continue
		}
		seen[item.ProxyID] = struct{}{}
		concurrency := item.Concurrency
		if concurrency <= 0 {
			concurrency = DefaultAccountProxyConcurrency
		}
		out = append(out, AccountProxy{
			AccountID:   accountID,
			ProxyID:     item.ProxyID,
			Concurrency: concurrency,
			SortOrder:   len(out),
			Proxy:       item.Proxy,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
