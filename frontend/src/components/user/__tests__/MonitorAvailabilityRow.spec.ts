import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import MonitorAvailabilityRow from '../monitor/MonitorAvailabilityRow.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

function mountRow(value: number | null, firstTokenMs: number | null, cacheHitRate: number | null) {
  return mount(MonitorAvailabilityRow, {
    props: {
      windowLabel: '可用率 · 7d',
      value,
      firstTokenMs,
      cacheHitRate,
    },
  })
}

describe('MonitorAvailabilityRow', () => {
  it('uses the same green/amber/orange/red hierarchy for all three metrics', () => {
    const wrapper = mountRow(75, 35_000, 75)
    const values = wrapper.findAll('.font-mono')

    expect(values[0].classes()).toContain('text-amber-600')
    expect(values[1].classes()).toContain('text-orange-600')
    expect(values[2].classes()).toContain('text-amber-600')
  })

  it('keeps missing rate data neutral instead of making the card look degraded', () => {
    const wrapper = mountRow(null, 1_000, null)
    const values = wrapper.findAll('.font-mono')

    expect(values[0].classes()).toContain('text-gray-400')
    expect(values[1].classes()).toContain('text-emerald-600')
    expect(values[2].classes()).toContain('text-gray-400')
  })
})
