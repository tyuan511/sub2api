import { describe, expect, it } from 'vitest'

import {
  fallbackAvailability,
  fallbackCacheHitRate,
  fallbackFirstTokenMs,
} from '../monitorFallback'

// 固定“当天”，避免测试在 UTC 日期翻转时偶发失败。
const NOW = new Date('2026-11-01T08:00:00Z')

describe('monitorFallback determinism', () => {
  it('returns the same value for the same key within a day', () => {
    const a = fallbackAvailability(7, undefined, NOW)
    const b = fallbackAvailability(7, undefined, NOW)
    expect(a).toBe(b)
  })

  it('returns the same value regardless of call-time time of day', () => {
    const morning = fallbackCacheHitRate(3, '3d', new Date('2026-11-01T01:00:00Z'))
    const evening = fallbackCacheHitRate(3, '3d', new Date('2026-11-01T23:00:00Z'))
    expect(morning).toBe(evening)
  })

  it('varies by monitor key, window and metric', () => {
    const values = [
      fallbackAvailability(1, undefined, NOW),
      fallbackAvailability(2, undefined, NOW),
      fallbackAvailability(1, '7d', NOW),
      fallbackCacheHitRate(1, '3d', NOW),
      fallbackFirstTokenMs(1, undefined, NOW),
    ]
    expect(new Set(values).size).toBe(values.length)
  })

  it('changes across UTC days', () => {
    const today = fallbackFirstTokenMs(5, undefined, new Date('2026-11-01T00:00:00Z'))
    const tomorrow = fallbackFirstTokenMs(5, undefined, new Date('2026-11-02T00:00:00Z'))
    expect(today).not.toBe(tomorrow)
  })
})

describe('monitorFallback ranges', () => {
  it('keeps availability within 95-100', () => {
    for (let id = 1; id <= 50; id += 1) {
      const value = fallbackAvailability(id, undefined, NOW)
      expect(value).toBeGreaterThanOrEqual(95)
      expect(value).toBeLessThanOrEqual(100)
      expect(Number.isInteger(value * 100)).toBe(true)
    }
  })

  it('keeps cache hit rate within 80-95', () => {
    for (let id = 1; id <= 50; id += 1) {
      const value = fallbackCacheHitRate(id, '3d', NOW)
      expect(value).toBeGreaterThanOrEqual(80)
      expect(value).toBeLessThanOrEqual(95)
    }
  })

  it('keeps first token within 2000-10000 ms as integers', () => {
    for (let id = 1; id <= 50; id += 1) {
      const value = fallbackFirstTokenMs(id, undefined, NOW)
      expect(value).toBeGreaterThanOrEqual(2000)
      expect(value).toBeLessThanOrEqual(10000)
      expect(Number.isInteger(value)).toBe(true)
    }
  })

  it('accepts composite string keys', () => {
    const value = fallbackFirstTokenMs('12:claude-sonnet-4-5', undefined, NOW)
    expect(value).toBe(fallbackFirstTokenMs('12:claude-sonnet-4-5', undefined, NOW))
    expect(value).toBeGreaterThanOrEqual(2000)
    expect(value).toBeLessThanOrEqual(10000)
  })
})
