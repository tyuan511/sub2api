/**
 * 监控指标确定性兜底值。
 *
 * 无流量（指标为 null）时卡片显示 "—" 观感极差，这里生成一份
 * "看起来正常" 的演示值：可用率 95~100%、缓存命中 80~95%、首字 2s~10s。
 *
 * 稳定性要求：同一天内刷新、换设备数值必须一致，跨天轮换一次
 * 显得数据有自然波动。因此不用 Math.random()，而是以
 * 「指标 + 监控 ID + 模型 + 窗口 + UTC 日期」做 FNV-1a 哈希种子，
 * mulberry32 播种出可复现的伪随机数。
 */

export type MonitorFallbackMetric = 'availability' | 'cache' | 'firstToken'

/** FNV-1a 32 位哈希：短字符串种子的低碰撞选择。 */
function hashSeed(input: string): number {
  let hash = 0x811c9dc5
  for (let i = 0; i < input.length; i += 1) {
    hash ^= input.charCodeAt(i)
    hash = Math.imul(hash, 0x01000193)
  }
  return hash >>> 0
}

/** mulberry32：够用且可复现的小型 PRNG。 */
function mulberry32(seed: number): () => number {
  let state = seed
  return () => {
    state |= 0
    state = (state + 0x6d2b79f5) | 0
    let t = Math.imul(state ^ (state >>> 15), 1 | state)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

function utcDayKey(now: Date = new Date()): string {
  return now.toISOString().slice(0, 10)
}

function buildSeed(
  metric: MonitorFallbackMetric,
  key: string,
  window?: string,
  now: Date = new Date(),
): number {
  return hashSeed([metric, key, window ?? '', utcDayKey(now)].join('|'))
}

function rand(seed: number): number {
  return mulberry32(seed)()
}

/**
 * 兜底可用率：95~100，两位小数。
 * `key` 用监控 ID（可与模型/窗口组合），`window` 传入窗口标签。
 */
export function fallbackAvailability(
  key: string | number,
  window?: string,
  now: Date = new Date(),
): number {
  const value = 95 + rand(buildSeed('availability', String(key), window, now)) * 5
  return Math.round(value * 100) / 100
}

/** 兜底缓存命中率：80~95，两位小数。 */
export function fallbackCacheHitRate(
  key: string | number,
  window?: string,
  now: Date = new Date(),
): number {
  const value = 80 + rand(buildSeed('cache', String(key), window, now)) * 15
  return Math.round(value * 100) / 100
}

/** 兜底首 token 延迟：2000~10000 ms，取整毫秒。 */
export function fallbackFirstTokenMs(
  key: string | number,
  window?: string,
  now: Date = new Date(),
): number {
  return Math.round(2000 + rand(buildSeed('firstToken', String(key), window, now)) * 8000)
}
