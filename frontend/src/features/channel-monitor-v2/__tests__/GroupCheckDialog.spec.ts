import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import GroupCheckDialog from '../GroupCheckDialog.vue'
import { getGroupChecks, startGroupCheck } from '@/api/channelMonitorV2'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: Record<string, string | number>) => `${key} ${params?.value || params?.elapsed || ''}` }) }))
vi.mock('@/api/client', () => ({ apiClient: {} }))
vi.mock('@/api/groups', () => ({ userGroupsAPI: { getAvailable: vi.fn().mockResolvedValue([{ id: 7, platform: 'openai' }, { id: 8, platform: 'anthropic' }]) } }))
vi.mock('@/api/channelMonitorV2', () => ({
  getGroupChecks: vi.fn(),
  startGroupCheck: vi.fn(),
  groupCheckModelOptions: (platform: string) => {
    const values = platform === 'openai' ? ['gpt-6-astra', 'gpt-6.1-sol'] : ['claude-opus-5.5', 'claude-sonnet-5.5']
    return values.map(value => ({ value, label: value }))
  },
}))

const base = { group_id: 7, kind: 'connectivity', created_at: '2026-01-01T00:00:00Z' }
const props = { show: true, groupId: 7, groupName: 'Group', initialModel: 'gpt-6-astra' }
const mountDialog = () => mount(GroupCheckDialog, { props, global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } } })
afterEach(() => { vi.clearAllMocks(); document.body.innerHTML = '' })

describe('group checks', () => {
  it('uses the real platform allowlist without accepting arbitrary initial models', async () => {
    const api = await vi.importActual<typeof import('@/api/channelMonitorV2')>('@/api/channelMonitorV2')
    expect(api.groupCheckModelOptions('openai').map(option => option.value)).toEqual(['gpt-6-astra', 'gpt-6.1-sol'])
    expect(api.groupCheckModelOptions('anthropic').map(option => option.value)).toEqual(['claude-opus-5.5', 'claude-sonnet-5.5'])
    expect(api.groupCheckModelOptions('gemini')).toEqual([])
  })

  it('uses the actual platform for the model dropdown and starts a test', async () => {
    vi.mocked(getGroupChecks).mockResolvedValue([])
    vi.mocked(startGroupCheck).mockResolvedValue({ ...base, id: 1, model: 'gpt-6.1-sol', status: 'succeeded', is_mine: true, actual_cost: 0.0123 })
    const wrapper = mountDialog()
    await flushPromises()
    expect(wrapper.find('select').findAll('option').map(option => option.text())).toEqual(['gpt-6-astra', 'gpt-6.1-sol'])
    await wrapper.get('select').setValue('gpt-6.1-sol')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(startGroupCheck).toHaveBeenCalledWith(7, 'connectivity', 'gpt-6.1-sol')
    expect(wrapper.text()).toContain('$0.012300')
    wrapper.unmount()
  })

  it('opens on the run tab and restores an own running record returned first by history', async () => {
    vi.mocked(getGroupChecks).mockResolvedValue([
      { ...base, id: 9, model: 'gpt-6-astra', status: 'running', is_mine: true, duration_ms: null },
      ...Array.from({ length: 50 }, (_, index) => ({ ...base, id: index + 20, model: `old-${index}`, status: 'succeeded' as const, is_mine: false })),
    ])
    const wrapper = mountDialog()
    await flushPromises()
    expect(getGroupChecks).toHaveBeenCalledWith(7)
    expect(wrapper.text()).toContain('channelMonitorV2.groupCheck.inProgress')
    expect(wrapper.text()).toContain('gpt-6-astra')
    expect(wrapper.text()).toMatch(/\d+\.\d+s/)
    expect(wrapper.text()).not.toContain('old-0')
    expect(wrapper.find('[role="tab"]').attributes('aria-selected')).toBe('true')
    wrapper.unmount()
  })

  it('accepts a concurrent response containing an existing running record and protects duplicate starts', async () => {
    vi.mocked(getGroupChecks).mockResolvedValue([])
    vi.mocked(startGroupCheck).mockResolvedValue({ ...base, id: 3, model: 'gpt-6-astra', status: 'running', is_mine: true })
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('form').trigger('submit')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(startGroupCheck).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('channelMonitorV2.groupCheck.inProgress')
    wrapper.unmount()
  })

  it('falls back to the first whitelisted model for an unsupported initial model', async () => {
    vi.mocked(getGroupChecks).mockResolvedValue([])
    const wrapper = mount(GroupCheckDialog, { props: { ...props, initialModel: 'unsupported-model' }, global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } } })
    await flushPromises()
    expect((wrapper.get('select').element as HTMLSelectElement).value).toBe('gpt-6-astra')
    expect(wrapper.findAll('option').map(option => option.text())).not.toContain('unsupported-model')
    wrapper.unmount()
  })

  it('updates a running current record to terminal state and keeps polling other delayed bills', async () => {
    vi.useFakeTimers()
    try {
      vi.mocked(getGroupChecks)
        .mockResolvedValueOnce([{ ...base, id: 1, model: 'gpt-6-astra', status: 'running', is_mine: true }])
        .mockResolvedValueOnce([
          { ...base, id: 1, model: 'gpt-6-astra', status: 'succeeded', is_mine: true, actual_cost: 0.01 },
          { ...base, id: 2, model: 'gpt-6.1-sol', status: 'failed', is_mine: true, actual_cost: null },
        ])
        .mockResolvedValueOnce([
          { ...base, id: 1, model: 'gpt-6-astra', status: 'succeeded', is_mine: true, actual_cost: 0.01 },
          { ...base, id: 2, model: 'gpt-6.1-sol', status: 'failed', is_mine: true, actual_cost: 0.02 },
        ])
      const wrapper = mountDialog()
      await flushPromises()
      await vi.advanceTimersByTimeAsync(5000)
      await flushPromises()
      expect(wrapper.text()).toContain('$0.010000')
      await wrapper.findAll('[role="tab"]')[1].trigger('click')
      await vi.advanceTimersByTimeAsync(5000)
      await flushPromises()
      expect(wrapper.text()).toContain('$0.020000')
      wrapper.unmount()
    } finally { vi.useRealTimers() }
  })
  it('refreshes a delayed history bill without a current record and keeps the history tab open while running', async () => {
    vi.useFakeTimers()
    try {
      const bill = { ...base, id: 2, model: 'gpt-6.1-sol', status: 'succeeded' as const, is_mine: true, actual_cost: null }
      vi.mocked(getGroupChecks).mockResolvedValueOnce([bill]).mockResolvedValueOnce([{ ...bill, actual_cost: 0.03 }])
      const wrapper = mountDialog()
      await flushPromises()
      await wrapper.findAll('[role="tab"]')[1].trigger('click')
      await vi.advanceTimersByTimeAsync(5000)
      await flushPromises()
      expect(wrapper.text()).toContain('$0.030000')
      wrapper.unmount()

      const running = { ...base, id: 3, model: 'gpt-6-astra', status: 'running' as const, is_mine: true }
      vi.mocked(getGroupChecks).mockResolvedValue([running])
      const second = mountDialog()
      await flushPromises()
      await second.findAll('[role="tab"]')[1].trigger('click')
      await vi.advanceTimersByTimeAsync(5000)
      await flushPromises()
      expect(second.findAll('[role="tab"]')[1].attributes('aria-selected')).toBe('true')
      second.unmount()
    } finally { vi.useRealTimers() }
  })

  it('clears the previous clock when the group changes', async () => {
    vi.useFakeTimers()
    try {
      vi.mocked(getGroupChecks).mockResolvedValue([])
      const wrapper = mountDialog()
      await flushPromises()
      expect(vi.getTimerCount()).toBe(1)
      await wrapper.setProps({ groupId: 8 })
      await flushPromises()
      expect(vi.getTimerCount()).toBe(1)
      wrapper.unmount()
      expect(vi.getTimerCount()).toBe(0)
    } finally { vi.useRealTimers() }
  })

  it('stops retrying an unpaid history record after one minute', async () => {
    vi.useFakeTimers()
    try {
      vi.mocked(getGroupChecks).mockResolvedValue([{ ...base, id: 1, model: 'gpt-6-astra', status: 'failed', is_mine: true, actual_cost: null }])
      const wrapper = mountDialog()
      await flushPromises()
      await vi.advanceTimersByTimeAsync(60_000)
      const calls = vi.mocked(getGroupChecks).mock.calls.length
      expect(calls).toBeGreaterThan(1)
      await vi.advanceTimersByTimeAsync(15_000)
      expect(vi.mocked(getGroupChecks)).toHaveBeenCalledTimes(calls)
      wrapper.unmount()
    } finally { vi.useRealTimers() }
  })

  it('polls a terminal own result until delayed billing arrives, then stops at the bound', async () => {
    vi.useFakeTimers()
    try {
      vi.mocked(startGroupCheck).mockResolvedValue({ ...base, id: 1, model: 'gpt-6-astra', status: 'succeeded', is_mine: true, actual_cost: null })
      vi.mocked(getGroupChecks)
        .mockResolvedValueOnce([{ ...base, id: 1, model: 'gpt-6-astra', status: 'succeeded', is_mine: true, actual_cost: null }])
        .mockResolvedValueOnce([{ ...base, id: 1, model: 'gpt-6-astra', status: 'succeeded', is_mine: true, actual_cost: 0.0042 }])
      const wrapper = mountDialog()
      await flushPromises()
      await wrapper.get('form').trigger('submit')
      await flushPromises()
      await vi.advanceTimersByTimeAsync(5000)
      await flushPromises()
      expect(wrapper.text()).toContain('$0.004200')
      const callsAfterCost = vi.mocked(getGroupChecks).mock.calls.length
      await vi.advanceTimersByTimeAsync(60_000)
      expect(vi.mocked(getGroupChecks).mock.calls.length).toBe(callsAfterCost)
      wrapper.unmount()
    } finally { vi.useRealTimers() }
  })
})
