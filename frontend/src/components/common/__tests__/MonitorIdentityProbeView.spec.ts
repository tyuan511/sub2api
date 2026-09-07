import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import MonitorIdentityProbeView from '../MonitorIdentityProbeView.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }),
  }
})

const stubs = {
  HelpTooltip: { template: '<div><slot name="trigger" /><slot /></div>' },
  Icon: true,
}

describe('MonitorIdentityProbeView', () => {
  it('links to the public BazaarLink report from report_url', () => {
    const wrapper = mount(MonitorIdentityProbeView, {
      props: {
        result: {
          claimed_model: 'gpt-5',
          report_url: 'https://bazaarlink.ai/probe?runId=e9475a75-9d1d-4624-9efd-0f3787eb84ee',
          checked_at: '2026-09-07T00:00:00Z',
        },
      },
      global: { stubs },
    })
    expect(wrapper.get('a').attributes('href')).toBe(
      'https://bazaarlink.ai/probe?runId=e9475a75-9d1d-4624-9efd-0f3787eb84ee',
    )
    expect(wrapper.get('a').attributes('target')).toBe('_blank')
  })

  it('builds the report URL from an admin run_id when report_url is absent', () => {
    const wrapper = mount(MonitorIdentityProbeView, {
      props: {
        result: {
          run_id: 'e9475a75-9d1d-4624-9efd-0f3787eb84ee',
          checked_at: '2026-09-07T00:00:00Z',
        },
      },
      global: { stubs },
    })
    expect(wrapper.get('a').attributes('href')).toBe(
      'https://bazaarlink.ai/probe?runId=e9475a75-9d1d-4624-9efd-0f3787eb84ee',
    )
  })

  it('maps BazaarLink match to the compact confirmed label', () => {
    const wrapper = mount(MonitorIdentityProbeView, {
      props: {
        result: {
          identity_status: 'match',
          claimed_model: 'gpt-5.6-sol',
          checked_at: '2026-09-07T00:00:00Z',
        },
      },
      global: { stubs },
    })
    const status = wrapper.get('[data-testid="identity-status"]')
    expect(status.text()).toBe('monitorCommon.identityProbe.status.confirmed')
    expect(status.classes()).toContain('text-emerald-400')
    expect(status.classes().some((name) => name.startsWith('bg-') || name.startsWith('rounded-full'))).toBe(false)
  })

  it('hides the report link when no run id is available', () => {
    const wrapper = mount(MonitorIdentityProbeView, {
      props: {
        result: {
          claimed_model: 'gpt-5',
          checked_at: '2026-09-07T00:00:00Z',
        },
      },
      global: { stubs },
    })
    expect(wrapper.find('a').exists()).toBe(false)
  })
})
