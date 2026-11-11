import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import type { UserMonitorView } from '@/api/channelMonitor'
import MonitorCardGrid from '../monitor/MonitorCardGrid.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

function makeItem(status: UserMonitorView['primary_status']): UserMonitorView {
  return {
    id: 1,
    name: 'main',
    provider: 'openai',
    group_name: '',
    primary_model: 'gpt-5.6',
    primary_status: status,
    primary_latency_ms: null,
    primary_ping_latency_ms: null,
    availability_3d: 12,
    availability_7d: 34,
    cache_hit_rate_3d: null,
    cache_hit_rate_7d: null,
    cache_hit_rate_15d: null,
    cache_hit_rate_30d: null,
    extra_models: [],
    timeline: [],
  }
}

describe('MonitorCardGrid latest availability', () => {
  it('passes the latest status score to the card instead of the selected window aggregate', () => {
    const wrapper = mount(MonitorCardGrid, {
      props: {
        items: [makeItem('degraded')],
        window: '7d',
        countdownSeconds: 0,
        loading: false,
        detailCache: {},
      },
      global: {
        stubs: {
          EmptyState: true,
          MonitorCard: {
            props: ['availabilityValue'],
            template: '<div data-test="card">{{ availabilityValue }}</div>',
          },
        },
      },
    })

    expect(wrapper.get('[data-test="card"]').text()).toBe('80')
  })
})

describe('MonitorCardGrid no-traffic display', () => {
  function mountGrid(item: UserMonitorView) {
    return mount(MonitorCardGrid, {
      props: {
        items: [item],
        window: '3d' as const,
        countdownSeconds: 0,
        loading: false,
        detailCache: {},
      },
      global: {
        stubs: {
          EmptyState: true,
          MonitorCard: {
            props: ['availabilityValue', 'cacheHitRate'],
            template:
              '<div data-test="card">{{ availabilityValue }}|{{ cacheHitRate }}</div>',
          },
        },
      },
    })
  }

  it('shows no fabricated values when the monitor has never been probed', () => {
    // 空状态 = 后端无任何探测历史（''），可用率与缓存率如实留空，不再兜底演示值。
    const item = makeItem('' as UserMonitorView['primary_status'])
    const wrapper = mountGrid(item)
    const [availability, cache] = wrapper.get('[data-test="card"]').text().split('|')
    expect(availability).toBe('')
    expect(cache).toBe('')
  })

  it('keeps real failure scores instead of masking them', () => {
    const wrapper = mountGrid(makeItem('error'))
    const [availability] = wrapper.get('[data-test="card"]').text().split('|')
    expect(Number(availability)).toBe(0)
  })
})
