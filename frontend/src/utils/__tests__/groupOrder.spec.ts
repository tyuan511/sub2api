import { describe, expect, it } from 'vitest'
import type { Group } from '@/types'
import {
  compareGroupsForDisplay,
  effectiveGroupRate,
  platformDisplayRank,
  sortByPlatformDisplayOrder,
  sortGroupsForDisplay,
} from '../groupOrder'

const group = (id: number, name: string, platform: string, rate: number, sortOrder?: number): Group =>
  ({ id, name, platform, rate_multiplier: rate, sort_order: sortOrder, description: '' }) as Group

describe('groupOrder platform rank', () => {
  it('ranks the curated platforms first, everything else by name', () => {
    expect(platformDisplayRank('openai')).toBe(0)
    expect(platformDisplayRank('Anthropic ')).toBe(1)
    expect(platformDisplayRank('grok')).toBe(2)
    expect(platformDisplayRank('gemini')).toBe(3)
    expect(platformDisplayRank('kimi')).toBe(4)
    expect(platformDisplayRank('')).toBe(4)
    expect(platformDisplayRank(null)).toBe(4)
  })

  it('sorts platform names by display order', () => {
    expect(sortByPlatformDisplayOrder(['zhipu', 'gemini', 'openai', 'antigravity', 'anthropic', 'grok', 'openai']))
      .toEqual(['openai', 'anthropic', 'grok', 'gemini', 'antigravity', 'zhipu'])
  })
})

describe('groupOrder sortGroupsForDisplay', () => {
  it('groups by platform display order, then rate ascending', () => {
    const groups = [
      group(1, 'GPT 050', 'openai', 0.5),
      group(2, 'Claude Max', 'anthropic', 0.8),
      group(3, 'GPT Plus', 'openai', 0.1),
      group(4, 'GPT Pro', 'openai', 0.15),
      group(5, 'Grok Heavy', 'grok', 0.1),
      group(6, 'Kimi 低价', 'kimi', 0.01),
    ]

    // 平台定制顺序：openai → anthropic → grok → 其余（kimi 按字母序垫底）；同平台内倍率低优先。
    expect(sortGroupsForDisplay(groups).map((g) => g.name)).toEqual([
      'GPT Plus',
      'GPT Pro',
      'GPT 050',
      'Claude Max',
      'Grok Heavy',
      'Kimi 低价',
    ])
  })

  it('uses the user-specific rate when present', () => {
    const groups = [group(1, 'A', 'openai', 0.5), group(2, 'B', 'openai', 0.2)]
    expect(sortGroupsForDisplay(groups).map((g) => g.name)).toEqual(['B', 'A'])
    expect(sortGroupsForDisplay(groups, { ratesByGroupId: { 1: 0.05, 2: 0.9 } }).map((g) => g.name)).toEqual(['A', 'B'])
    expect(effectiveGroupRate(groups[0], { 1: 0.05 })).toBe(0.05)
    expect(effectiveGroupRate(groups[0], null)).toBe(0.5)
  })

  it('honours a custom rate accessor (model plaza peak rate)', () => {
    const groups = [group(1, 'A', 'openai', 0.5), group(2, 'B', 'openai', 0.2)]
    const sorted = sortGroupsForDisplay(groups, { rateOf: (g) => (g.id === 1 ? 0.01 : 9) })
    expect(sorted.map((g) => g.name)).toEqual(['A', 'B'])
  })

  it('keeps manual sort_order ahead of platform and rate', () => {
    const groups = [
      group(1, 'GPT Plus', 'openai', 0.1, 0),
      group(2, 'Grok Heavy', 'grok', 0.2, 5),
      group(3, 'Kimi', 'kimi', 0.05, -5),
    ]
    // -5 < 0 < 5，即使 kimi/openai 的平台顺序不同也按手动排序。
    expect(sortGroupsForDisplay(groups).map((g) => g.name)).toEqual(['Kimi', 'GPT Plus', 'Grok Heavy'])
  })

  it('falls back to name then id so the order stays stable', () => {
    const groups = [group(9, 'b-group', 'grok', 0.1), group(3, 'a-group', 'grok', 0.1), group(4, 'a-group', 'grok', 0.1)]
    expect(sortGroupsForDisplay(groups).map((g) => g.id)).toEqual([3, 4, 9])
    expect(compareGroupsForDisplay(groups[0], groups[0])).toBe(0)
  })

  it('tolerates missing platform and non-numeric rates', () => {
    const groups = [group(1, 'A', '', 0.1), group(2, 'B', 'openai', Number.NaN), group(3, 'C', 'anthropic', 0.2)]
    expect(sortGroupsForDisplay(groups).map((g) => g.name)).toEqual(['B', 'C', 'A'])
    expect(effectiveGroupRate(group(1, 'A', 'openai', Number.POSITIVE_INFINITY))).toBe(0)
  })

  it('does not mutate the input array', () => {
    const groups = [group(1, 'A', 'openai', 0.5), group(2, 'B', 'anthropic', 0.2)]
    const sorted = sortGroupsForDisplay(groups)
    expect(groups.map((g) => g.id)).toEqual([1, 2])
    expect(sorted.map((g) => g.id)).toEqual([1, 2])
  })
})
