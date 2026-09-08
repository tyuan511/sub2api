import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import RoutingPreferencePresets, { snapSmartBalanceBps } from '../RoutingPreferencePresets.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

describe('snapSmartBalanceBps', () => {
  it.each([
    [0, 0],
    [1250, 0],
    [2500, 2500],
    [3000, 2500],
    [5000, 5000],
    [7350, 5000],
    [8750, 10000],
    [10000, 10000]
  ])('maps %i to preset %i', (value, expected) => {
    expect(snapSmartBalanceBps(value)).toBe(expected)
  })
})

describe('RoutingPreferencePresets', () => {
  it('highlights the nearest preset and redraws the radar on change', async () => {
    const wrapper = mount(RoutingPreferencePresets, { props: { modelValue: 5000 } })
    expect(wrapper.get('[data-test="smart-balance-preset-5000"]').classes()).toContain('border-primary-500')
    const balanced = wrapper.get('[data-test="routing-score-radar-shape"]').attributes('points')

    await wrapper.get('[data-test="smart-balance-preset-0"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([0])
    await wrapper.setProps({ modelValue: 0 })
    const lowest = wrapper.get('[data-test="routing-score-radar-shape"]').attributes('points')
    expect(lowest).not.toBe(balanced)

    await wrapper.get('[data-test="smart-balance-preset-10000"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([10000])
    await wrapper.setProps({ modelValue: 10000 })
    const rock = wrapper.get('[data-test="routing-score-radar-shape"]').attributes('points')
    expect(rock).not.toBe(lowest)
    expect(rock).not.toBe(balanced)
  })

  it('treats a legacy slider value as the nearest preset', () => {
    const wrapper = mount(RoutingPreferencePresets, { props: { modelValue: 7350 } })
    expect(wrapper.get('[data-test="smart-balance-preset-5000"]').classes()).toContain('border-primary-500')
  })
})
