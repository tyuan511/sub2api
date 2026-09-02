import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import MultiProxySelector from '../MultiProxySelector.vue'
import type { AccountProxyBinding, Proxy } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key}:${JSON.stringify(params)}` : key
    })
  }
})

function makeProxy(id: number, name: string): Proxy {
  return {
    id,
    name,
    protocol: 'http',
    host: `10.0.0.${id}`,
    port: 8080,
    status: 'active',
    created_at: '',
    updated_at: ''
  } as Proxy
}

describe('MultiProxySelector', () => {
  it('adds a proxy with the default concurrency when picked', async () => {
    const wrapper = mount(MultiProxySelector, {
      props: { modelValue: [] as AccountProxyBinding[], proxies: [makeProxy(1, 'hk'), makeProxy(2, 'us')] },
      global: { stubs: { Icon: true } }
    })

    await wrapper.find('.select-trigger').trigger('click')
    await wrapper.findAll('.select-option')[0].trigger('click')

    const emitted = wrapper.emitted('update:modelValue')
    expect(emitted).toBeTruthy()
    expect(emitted![0][0]).toEqual([{ proxy_id: 1, concurrency: 3 }])
  })

  it('removes an already selected proxy when picked again', async () => {
    const wrapper = mount(MultiProxySelector, {
      props: {
        modelValue: [{ proxy_id: 1, concurrency: 5 }] as AccountProxyBinding[],
        proxies: [makeProxy(1, 'hk'), makeProxy(2, 'us')]
      },
      global: { stubs: { Icon: true } }
    })

    await wrapper.find('.select-trigger').trigger('click')
    await wrapper.findAll('.select-option')[0].trigger('click')

    expect(wrapper.emitted('update:modelValue')![0][0]).toEqual([])
  })

  it('reports the pool total as the sum of the per-proxy concurrency', () => {
    const wrapper = mount(MultiProxySelector, {
      props: {
        modelValue: [
          { proxy_id: 1, concurrency: 5 },
          { proxy_id: 2, concurrency: 3 }
        ] as AccountProxyBinding[],
        proxies: [makeProxy(1, 'hk'), makeProxy(2, 'us')]
      },
      global: { stubs: { Icon: true } }
    })

    expect(wrapper.find('.proxy-pool-total').text()).toContain('"concurrency":8')
    expect(wrapper.find('.proxy-pool-total').text()).toContain('"count":2')
  })

  it('edits only the targeted proxy concurrency', async () => {
    const wrapper = mount(MultiProxySelector, {
      props: {
        modelValue: [
          { proxy_id: 1, concurrency: 5 },
          { proxy_id: 2, concurrency: 3 }
        ] as AccountProxyBinding[],
        proxies: [makeProxy(1, 'hk'), makeProxy(2, 'us')]
      },
      global: { stubs: { Icon: true } }
    })

    const input = wrapper.findAll('.proxy-pool-row input')[1]
    await input.setValue('9')

    expect(wrapper.emitted('update:modelValue')![0][0]).toEqual([
      { proxy_id: 1, concurrency: 5 },
      { proxy_id: 2, concurrency: 9 }
    ])
  })

  it('treats a single proxy as legacy mode: no per-proxy concurrency or total', () => {
    const wrapper = mount(MultiProxySelector, {
      props: {
        modelValue: [{ proxy_id: 1, concurrency: 5 }] as AccountProxyBinding[],
        proxies: [makeProxy(1, 'hk'), makeProxy(2, 'us')]
      },
      global: { stubs: { Icon: true } }
    })

    expect(wrapper.findAll('.proxy-pool-row')).toHaveLength(1)
    expect(wrapper.find('.proxy-pool-row input').exists()).toBe(false)
    expect(wrapper.find('.proxy-pool-total').exists()).toBe(false)
  })

  it('lets a newly picked proxy inherit the account concurrency instead of a fixed default', async () => {
    const wrapper = mount(MultiProxySelector, {
      props: {
        modelValue: [] as AccountProxyBinding[],
        proxies: [makeProxy(1, 'hk'), makeProxy(2, 'us')],
        defaultConcurrency: 10
      },
      global: { stubs: { Icon: true } }
    })

    await wrapper.find('.select-trigger').trigger('click')
    await wrapper.findAll('.select-option')[0].trigger('click')

    expect(wrapper.emitted('update:modelValue')![0][0]).toEqual([{ proxy_id: 1, concurrency: 10 }])
  })
})
