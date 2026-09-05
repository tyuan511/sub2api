import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import APIKeyRoutingRolloutCard from '../APIKeyRoutingRolloutCard.vue'

const { get, save, list, getUser, success, error } = vi.hoisted(() => ({
  get: vi.fn(), save: vi.fn(), list: vi.fn(), getUser: vi.fn(), success: vi.fn(), error: vi.fn()
}))
vi.mock('@/api/admin', () => ({ adminAPI: {
  settings: { getAPIKeyRoutingRollout: get, updateAPIKeyRoutingRollout: save },
  users: { list, getById: getUser }
} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: success, showError: error }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const user = (id: number) => ({ id, username: `User ${id}`, email: `user${id}@example.test` })

beforeEach(() => {
  vi.clearAllMocks()
  get.mockResolvedValue({ user_ids: [5] })
  save.mockImplementation(async payload => payload)
  list.mockResolvedValue({ items: [user(5), user(9)] })
  getUser.mockImplementation(async id => user(id))
})
afterEach(() => vi.useRealTimers())

describe('API key routing beta allowlist', () => {
  it('loads users, adds/removes exact IDs and saves the normalized selection', async () => {
    const wrapper = mount(APIKeyRoutingRolloutCard)
    await flushPromises()
    expect(wrapper.get('[data-test="rollout-user-5"]').attributes('aria-pressed')).toBe('true')
    await wrapper.get('[data-test="rollout-user-9"]').trigger('click')
    await wrapper.get('[data-test="rollout-remove-5"]').trigger('click')
    await wrapper.get('[data-test="rollout-save"]').trigger('click')
    await flushPromises()
    expect(save).toHaveBeenCalledWith({ user_ids: [9] })
    expect(success).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('uses an explicit empty list to withdraw access without writing until save', async () => {
    const wrapper = mount(APIKeyRoutingRolloutCard)
    await flushPromises()
    await wrapper.get('[data-test="rollout-clear"]').trigger('click')
    expect(wrapper.find('[data-test="rollout-empty"]').exists()).toBe(true)
    expect(save).not.toHaveBeenCalled()
    await wrapper.get('[data-test="rollout-save"]').trigger('click')
    await flushPromises()
    expect(save).toHaveBeenCalledWith({ user_ids: [] })
    wrapper.unmount()
  })

  it('supports exact numeric IDs as well as debounced username/email search', async () => {
    vi.useFakeTimers()
    const wrapper = mount(APIKeyRoutingRolloutCard)
    await flushPromises()
    await wrapper.get('[data-test="rollout-search"]').setValue('42')
    await vi.advanceTimersByTimeAsync(250)
    expect(getUser).toHaveBeenCalledWith(42)
    expect(wrapper.find('[data-test="rollout-user-42"]').exists()).toBe(true)
    await wrapper.get('[data-test="rollout-search"]').setValue('alice')
    await vi.advanceTimersByTimeAsync(250)
    expect(list).toHaveBeenLastCalledWith(1, 20, { search: 'alice', include_subscriptions: false })
    wrapper.unmount()
  })

  it('does not replace results with a late response from an older search', async () => {
    vi.useFakeTimers()
    const wrapper = mount(APIKeyRoutingRolloutCard)
    await flushPromises()
    let resolveOld!: (value: { items: ReturnType<typeof user>[] }) => void
    list.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    await wrapper.get('[data-test="rollout-search"]').setValue('old')
    await vi.advanceTimersByTimeAsync(250)
    await wrapper.get('[data-test="rollout-search"]').setValue('42')
    await vi.advanceTimersByTimeAsync(250)
    resolveOld({ items: [user(88)] })
    await flushPromises()
    expect(wrapper.find('[data-test="rollout-user-42"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="rollout-user-88"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('does not expose an editable empty form when settings cannot be loaded', async () => {
    get.mockRejectedValue(new Error('offline'))
    const wrapper = mount(APIKeyRoutingRolloutCard)
    await flushPromises()
    expect(wrapper.find('[data-test="rollout-save"]').exists()).toBe(false)
    expect(save).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps the selected IDs and reports failure when saving is rejected', async () => {
    save.mockRejectedValue({ response: { status: 400 } })
    const wrapper = mount(APIKeyRoutingRolloutCard)
    await flushPromises()
    await wrapper.get('[data-test="rollout-save"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="rollout-remove-5"]').exists()).toBe(true)
    expect(error).toHaveBeenCalledTimes(1)
    expect(success).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
