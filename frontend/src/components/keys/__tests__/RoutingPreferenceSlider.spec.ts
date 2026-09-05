import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import RoutingPreferenceSlider from '../RoutingPreferenceSlider.vue'

const props = {
  modelValue: 50, min: 50, max: 95, step: 5, defaultValue: 50,
  label: 'Success threshold', valueLabel: '50%', description: 'Enough samples required',
  leftLabel: '50%', rightLabel: '95%', resetLabel: 'Reset to 50%', ticks: 10
}

describe('RoutingPreferenceSlider', () => {
  it('exposes a labeled native keyboard-accessible range with bounded steps', () => {
    const wrapper = mount(RoutingPreferenceSlider, { props })
    expect(wrapper.get('input').attributes()).toMatchObject({
      type: 'range', min: '50', max: '95', step: '5', 'aria-label': 'Success threshold',
      'aria-valuetext': '50%. Enough samples required'
    })
  })

  it('halves the visible track and thumb while retaining a 40px hit area', () => {
    const wrapper = mount(RoutingPreferenceSlider, { props })
    expect(wrapper.get('[data-test="slider-track"]').classes()).toContain('h-5')
    expect(wrapper.get('[data-test="slider-thumb"]').classes()).toEqual(expect.arrayContaining(['h-5', 'w-5']))
    expect(wrapper.get('input').classes()).toEqual(expect.arrayContaining(['h-10', '-top-2.5']))
  })

  it.each([[0, 'calc(0% + 10px)'], [5000, 'calc(50% + 0px)'], [10000, 'calc(100% - 10px)']])(
    'keeps the smaller thumb aligned at position %i', (value, left) => {
      const wrapper = mount(RoutingPreferenceSlider, { props: { ...props, min: 0, max: 10000, modelValue: Number(value) } })
      expect((wrapper.get('[data-test="slider-thumb"]').element as HTMLElement).style.left).toBe(left)
    }
  )

  it('emits the selected threshold and resets without submitting its form', async () => {
    const wrapper = mount(RoutingPreferenceSlider, { props })
    await wrapper.get('input').setValue(95)
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([95])
    expect(wrapper.get('button').attributes('type')).toBe('button')
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([50])
  })

  it.each(Array.from({ length: 21 }, (_, index) => index * 500))('selects price/speed position %i in 5 percent increments', async (value) => {
    const wrapper = mount(RoutingPreferenceSlider, { props: { ...props, min: 0, max: 10000, step: 500, modelValue: 5000, defaultValue: 5000 } })
    expect(wrapper.get('input').attributes('step')).toBe('500')
    await wrapper.get('input').setValue(value)
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([value])
  })

  it.each([[1250, 1500], [7350, 7500], [8750, 9000]])('preserves legacy value %i until adjusted, then snaps to %i', async (value, snapped) => {
    const wrapper = mount(RoutingPreferenceSlider, { props: { ...props, min: 0, max: 10000, step: 500, modelValue: value, defaultValue: 5000 } })
    expect(wrapper.props('modelValue')).toBe(value)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.get('input').setValue(value)
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([snapped])
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([5000])
  })
})
