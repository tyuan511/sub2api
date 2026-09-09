import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { VueDraggable } from 'vue-draggable-plus'
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

  it('limits additional choices to the first group billing type', async () => {
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
    expect(wrapper.get('[data-test="route-group-option-3"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-test="route-group-option-4"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-test="route-group-option-2"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[1, 2]])
  })

  it('sorts dropdown options by platform and then group name', async () => {
    const openaiZ = makeGroup(1, 'openai')
    openaiZ.name = 'zeta'
    const anthropic = makeGroup(2, 'anthropic')
    anthropic.name = 'alpha'
    const openaiA = makeGroup(3, 'openai')
    openaiA.name = 'alpha'
    const wrapper = mount(ApiKeyGroupRouteSelector, {
      props: { modelValue: [], groups: [openaiZ, anthropic, openaiA] }
    })

    await wrapper.get('[data-test="route-group-trigger"]').trigger('click')
    expect(wrapper.findAll('[role="option"]').map((option) => option.attributes('data-test'))).toEqual([
      'route-group-option-2', 'route-group-option-3', 'route-group-option-1'
    ])
  })

  it('reorders and removes groups without mutating the input array', async () => {
    const initial = [1, 2, 3]
    const wrapper = mount(ApiKeyGroupRouteSelector, {
      props: {
        modelValue: initial,
        groups: [makeGroup(1), makeGroup(2), makeGroup(3)]
      }
    })

    await wrapper.get('[data-test="drag-route-2"]').trigger('keydown', { key: 'ArrowUp' })
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[2, 1, 3]])
    expect(initial).toEqual([1, 2, 3])

    await wrapper.get('[data-test="remove-route-group-2"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[1, 3]])
  })

  it('shows drag handles for multiple groups and reorders from drag updates', async () => {
    const initial = [1, 2, 3]
    const wrapper = mount(ApiKeyGroupRouteSelector, {
      props: {
        modelValue: initial,
        groups: [makeGroup(1), makeGroup(2), makeGroup(3)]
      }
    })

    expect(wrapper.get('[data-test="drag-route-1"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="drag-route-2"]').exists()).toBe(true)

    await wrapper.get('[data-test="drag-route-2"]').trigger('keydown', { key: 'ArrowUp' })
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[2, 1, 3]])

    wrapper.getComponent(VueDraggable).vm.$emit('update:modelValue', [
      { id: 3 },
      { id: 1 },
      { id: 2 }
    ])
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[3, 1, 2]])
    expect(initial).toEqual([1, 2, 3])
  })

  it('hides drag handles when only one group is selected', () => {
    const wrapper = mount(ApiKeyGroupRouteSelector, {
      props: {
        modelValue: [1],
        groups: [makeGroup(1), makeGroup(2)]
      }
    })

    expect(wrapper.find('[data-test="drag-route-1"]').exists()).toBe(false)
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

  it('supports keyboard selection and escape', async () => {
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
  })

  it('shows compact selected groups with platform color, name and rate', () => {
    const wrapper = mount(ApiKeyGroupRouteSelector, {
      props: {
        modelValue: [1, 2],
        groups: [makeGroup(1, 'openai'), makeGroup(2, 'grok')]
      }
    })

    const openai = wrapper.get('[data-test="selected-route-group-1"]')
    const grok = wrapper.get('[data-test="selected-route-group-2"]')
    expect(openai.text()).toContain('group-1')
    expect(openai.text()).toContain('1x')
    expect(openai.text()).not.toContain('keys.routePrimaryGroup')
    expect(openai.text()).not.toContain('keys.routeGroupRate')
    expect(openai.classes().join(' ')).toContain('green')
    expect(grok.classes().join(' ')).toContain('zinc')
    expect(wrapper.find('[data-test="route-group-detail-1"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('keys.routeCurrentRank')
    expect(wrapper.text()).not.toContain('keys.routeConfidence.high')
  })
})
