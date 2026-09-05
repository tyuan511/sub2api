import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { Group } from '@/types'
import ApiKeyGroupRouteSelector from '../ApiKeyGroupRouteSelector.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}:${JSON.stringify(params)}` : key
  })
}))

function makeGroup(
  id: number,
  platform = 'openai',
  subscriptionType = 'standard',
  status = 'active'
): Group {
  return {
    id,
    name: `group-${id}`,
    description: '',
    platform,
    subscription_type: subscriptionType,
    status,
    rate_multiplier: id,
    peak_rate_enabled: false,
    peak_start: '',
    peak_end: '',
    peak_rate_multiplier: 1
  } as Group
}

describe('ApiKeyGroupRouteSelector', () => {
  it('styles its trigger and popup without relying on another select component scoped styles', async () => {
    const wrapper = mount(ApiKeyGroupRouteSelector, {
      props: { modelValue: [], groups: [makeGroup(1)] }
    })

    const trigger = wrapper.get('[data-test="route-group-trigger"]')
    expect(trigger.classes()).toEqual(expect.arrayContaining(['input', 'flex', 'items-center', 'justify-between']))
    expect(trigger.get('span').classes()).toEqual(expect.arrayContaining(['min-w-0', 'truncate', 'text-gray-400']))
    expect(trigger.get('svg').classes()).toContain('shrink-0')

    await trigger.trigger('click')
    expect(trigger.classes()).toContain('ring-2')
    expect(wrapper.get('[data-test="route-group-options"]').classes()).toEqual(expect.arrayContaining([
      'absolute', 'inset-x-0', 'top-full', 'border', 'bg-white', 'dark:bg-dark-800', 'shadow-lg'
    ]))
    expect(wrapper.get('input').classes()).toEqual(expect.arrayContaining(['min-w-0', 'flex-1']))

    await wrapper.setProps({ modelValue: [1] })
    expect(trigger.get('span').classes()).not.toContain('text-gray-400')
  })

  it('limits additional choices to the first group platform and billing type', async () => {
    const wrapper = mount(ApiKeyGroupRouteSelector, {
      props: {
        modelValue: [1],
        groups: [
          makeGroup(1),
          makeGroup(2),
          makeGroup(3, 'anthropic'),
          makeGroup(4, 'openai', 'subscription')
        ]
      }
    })

    await wrapper.get('[data-test="route-group-trigger"]').trigger('click')

    expect(wrapper.get('[data-test="route-group-option-2"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-test="route-group-option-3"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="route-group-option-4"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-test="route-group-option-2"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[1, 2]])
  })

  it('reorders and removes groups without mutating the input array', async () => {
    const initial = [1, 2, 3]
    const wrapper = mount(ApiKeyGroupRouteSelector, {
      props: {
        modelValue: initial,
        groups: [makeGroup(1), makeGroup(2), makeGroup(3)]
      }
    })

    await wrapper.get('[data-test="move-route-up-2"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[2, 1, 3]])
    expect(initial).toEqual([1, 2, 3])

    await wrapper.get('[data-test="remove-route-group-2"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[1, 3]])
  })

  it('keeps a selected unavailable group removable but blocks adding another one', async () => {
    const wrapper = mount(ApiKeyGroupRouteSelector, {
      props: {
        modelValue: [1],
        groups: [makeGroup(1, 'openai', 'standard', 'disabled'), makeGroup(2, 'openai', 'standard', 'disabled')]
      }
    })

    await wrapper.get('[data-test="route-group-trigger"]').trigger('click')
    expect(wrapper.get('[data-test="route-group-option-1"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-test="route-group-option-2"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-test="route-group-option-1"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[]])
  })

  it('supports keyboard selection, escape, and responsive wrapped controls', async () => {
    const wrapper = mount(ApiKeyGroupRouteSelector, {
      props: {
        modelValue: [1],
        groups: [makeGroup(1), makeGroup(2), makeGroup(3)]
      }
    })

    await wrapper.get('[data-test="route-group-trigger"]').trigger('keydown', { key: 'ArrowDown' })
    const search = wrapper.get('input')
    expect(search.attributes('aria-activedescendant')).toBe('route-group-option-1')
    await search.trigger('keydown', { key: 'ArrowDown' })
    expect(search.attributes('aria-activedescendant')).toBe('route-group-option-2')
    await search.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[1, 2]])
    await search.trigger('keydown', { key: 'Escape' })
    expect(wrapper.get('[data-test="route-group-trigger"]').attributes('aria-expanded')).toBe('false')
    expect(wrapper.get('[data-test="selected-route-group-1"]').classes()).toContain('flex-wrap')
  })

  it('shows current smart-routing explanation for an existing binding', () => {
    const wrapper = mount(ApiKeyGroupRouteSelector, {
      props: {
        modelValue: [1],
        groups: [makeGroup(1)],
        routeDetails: [{
          group_id: 1,
          priority: 0,
          enabled: true,
          current_rank: 1,
          normalized_effective_rate: 0.75,
          success_rate: 0.98,
          ttft_ms: 120,
          duration_ms: 800,
          cache_hit_rate: 0.8,
          predicted_share: 0.9,
          price_confidence: 'high'
        }]
      }
    })

    const detail = wrapper.get('[data-test="route-group-detail-1"]').text()
    expect(detail).toContain('keys.routeCurrentRank')
    expect(detail).toContain('keys.routeEffectiveRate')
    expect(detail).toContain('98.0%')
    expect(detail).toContain('keys.routeTTFT')
    expect(detail).toContain('keys.routeCacheHit')
    expect(detail).toContain('keys.routeConfidence.high')
  })
})
