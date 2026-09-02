import type { Account, AccountProxyBinding } from '@/types'

/** 单条代理绑定的默认并发上限，与后端 DefaultAccountProxyConcurrency 保持一致 */
export const DEFAULT_PROXY_CONCURRENCY = 3

/**
 * 把账号的代理配置读成多代理池绑定列表。
 *
 * 历史账号只有 proxy_id（没有代理池），这里把它展示为「一条绑定 + 账号原并发」，
 * 保存后与原来的单代理行为完全一致；没有配置代理的账号返回空列表。
 */
export function accountProxyBindings(account: Account | null | undefined): AccountProxyBinding[] {
  if (!account) return []
  if (account.proxies && account.proxies.length > 0) {
    return account.proxies.map((item) => ({
      proxy_id: item.proxy_id,
      concurrency: Math.max(1, item.concurrency || DEFAULT_PROXY_CONCURRENCY)
    }))
  }
  if (account.proxy_id) {
    return [{ proxy_id: account.proxy_id, concurrency: Math.max(1, account.concurrency || 1) }]
  }
  return []
}

/** 只有 ≥2 个代理才构成代理池；单个代理走旧的单代理逻辑，历史账号零变化 */
export function isProxyPool(bindings: AccountProxyBinding[]): boolean {
  return bindings.length >= 2
}

/** 代理池的容量 = 各代理并发之和 */
export function proxyPoolConcurrency(bindings: AccountProxyBinding[]): number {
  return bindings.reduce((sum, item) => sum + Math.max(1, item.concurrency || 1), 0)
}
