import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AccountCapacityCell from '../AccountCapacityCell.vue'
import type { Account } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

function makeAccount(overrides: Partial<Account>): Account {
  return {
    id: 1,
    name: 'acc',
    platform: 'anthropic',
    type: 'oauth',
    credentials: {},
    extra: {},
    proxy_id: null,
    concurrency: 8,
    priority: 50,
    status: 'active',
    error_message: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: true,
    created_at: '',
    updated_at: '',
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    ...overrides
  } as Account
}

describe('AccountCapacityCell proxy pool', () => {
  it('shows the pool total as the capacity max', () => {
    const wrapper = mount(AccountCapacityCell, {
      props: {
        account: makeAccount({
          concurrency: 8,
          current_concurrency: 3,
          proxies: [
            { proxy_id: 1, concurrency: 5, name: 'hk', current_concurrency: 2 },
            { proxy_id: 2, concurrency: 3, name: 'us', current_concurrency: 1 }
          ]
        })
      }
    })

    const badge = wrapper.find('span[title]')
    expect(badge.text()).toContain('3')
    expect(badge.text()).toContain('8')
  })

  it('breaks the capacity down per proxy in the hover tooltip', () => {
    const wrapper = mount(AccountCapacityCell, {
      props: {
        account: makeAccount({
          concurrency: 8,
          current_concurrency: 3,
          proxies: [
            { proxy_id: 1, concurrency: 5, name: 'hk', current_concurrency: 2 },
            { proxy_id: 2, concurrency: 3, name: 'us', current_concurrency: 1 }
          ]
        })
      }
    })

    const title = wrapper.find('span[title]').attributes('title') ?? ''
    expect(title).toContain('hk: 2/5')
    expect(title).toContain('us: 1/3')
    expect(title).toContain('3/8')
  })

  it('leaves single-proxy accounts without a capacity tooltip', () => {
    const wrapper = mount(AccountCapacityCell, {
      props: { account: makeAccount({ proxy_id: 4, concurrency: 3, current_concurrency: 1 }) }
    })

    expect(wrapper.find('span[title]').attributes('title')).toBeFalsy()
  })
})
