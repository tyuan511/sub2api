import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import GroupCheckDialog from '../GroupCheckDialog.vue'
import GroupCheckHistoryPopover from '../GroupCheckHistoryPopover.vue'
import { getGroupChecks, startGroupCheck } from '@/api/channelMonitorV2'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: Record<string, string>) => `${key} ${params?.value || ''}` }) }))
vi.mock('@/api/channelMonitorV2', () => ({ getGroupChecks: vi.fn(), startGroupCheck: vi.fn() }))

const base = { group_id: 7, kind: 'connectivity', created_at: '2026-01-01T00:00:00Z' }
afterEach(() => { vi.clearAllMocks(); document.body.innerHTML = '' })

describe('group checks', () => {
  it('submits a test and displays only its result in the dialog', async () => {
    vi.mocked(startGroupCheck).mockResolvedValue({ ...base, id: 1, model: 'model', kind: 'pelican', status: 'succeeded', is_mine: true, actual_cost: 0.0123 })
    const wrapper = mount(GroupCheckDialog, {
      props: { show: true, groupId: 7, groupName: 'Group', initialModel: 'model' },
      global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } },
    })
    expect(getGroupChecks).not.toHaveBeenCalled()
    await wrapper.get('input[value="pelican"]').setValue()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(startGroupCheck).toHaveBeenCalledWith(7, 'pelican', 'model')
    expect(wrapper.text()).toContain('$0.012300')
    expect(wrapper.text()).not.toContain('groupCheck.history')
    wrapper.unmount()
  })

  it('polls a terminal own result until its delayed cost arrives', async () => {
    vi.useFakeTimers()
    try {
      vi.mocked(startGroupCheck).mockResolvedValue({ ...base, id: 1, model: 'model', status: 'succeeded', is_mine: true, actual_cost: null })
      vi.mocked(getGroupChecks)
        .mockResolvedValueOnce([{ ...base, id: 1, model: 'model', status: 'succeeded', is_mine: true, actual_cost: null }])
        .mockResolvedValueOnce([{ ...base, id: 1, model: 'model', status: 'succeeded', is_mine: true, actual_cost: 0.0042 }])
      const wrapper = mount(GroupCheckDialog, {
        props: { show: true, groupId: 7, groupName: 'Group', initialModel: 'model' },
        global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } },
      })
      await wrapper.get('form').trigger('submit')
      await flushPromises()
      expect(getGroupChecks).toHaveBeenCalledTimes(1)
      await vi.advanceTimersByTimeAsync(5000)
      await flushPromises()
      expect(getGroupChecks).toHaveBeenCalledTimes(2)
      expect(wrapper.text()).toContain('$0.004200')
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('stops polling the dialog after the bounded billing retry window', async () => {
    vi.useFakeTimers()
    try {
      vi.mocked(startGroupCheck).mockResolvedValue({ ...base, id: 1, model: 'model', status: 'succeeded', is_mine: true, actual_cost: null })
      vi.mocked(getGroupChecks).mockResolvedValue([{ ...base, id: 1, model: 'model', status: 'succeeded', is_mine: true, actual_cost: null }])
      const wrapper = mount(GroupCheckDialog, {
        props: { show: true, groupId: 7, groupName: 'Group', initialModel: 'model' },
        global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } },
      })
      await wrapper.get('form').trigger('submit')
      await flushPromises()
      await vi.advanceTimersByTimeAsync(60_000)
      await flushPromises()
      const callsAtLimit = vi.mocked(getGroupChecks).mock.calls.length
      await vi.advanceTimersByTimeAsync(15_000)
      expect(getGroupChecks).toHaveBeenCalledTimes(callsAtLimit)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('polls history until a delayed own terminal cost arrives', async () => {
    vi.useFakeTimers()
    try {
      vi.mocked(getGroupChecks)
        .mockResolvedValueOnce([{ ...base, id: 1, model: 'my-done', status: 'succeeded', is_mine: true, actual_cost: null }])
        .mockResolvedValueOnce([{ ...base, id: 1, model: 'my-done', status: 'succeeded', is_mine: true, actual_cost: 0.007 }])
      const wrapper = mount(GroupCheckHistoryPopover, { props: { groupId: 7 }, attachTo: document.body })
      await wrapper.get('button').trigger('click')
      await flushPromises()
      expect(document.body.textContent).not.toContain('$0.007000')
      await vi.advanceTimersByTimeAsync(5000)
      await flushPromises()
      expect(document.body.textContent).toContain('$0.007000')
      expect(getGroupChecks).toHaveBeenCalledTimes(2)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })


  it('stops polling history after the billing retry window', async () => {
    vi.useFakeTimers()
    try {
      vi.mocked(getGroupChecks).mockResolvedValue([{ ...base, id: 1, model: 'my-done', status: 'failed', is_mine: true, actual_cost: null }])
      const wrapper = mount(GroupCheckHistoryPopover, { props: { groupId: 7 }, attachTo: document.body })
      await wrapper.get('button').trigger('click')
      await flushPromises()
      await vi.advanceTimersByTimeAsync(60_000)
      await flushPromises()
      const callsAtLimit = vi.mocked(getGroupChecks).mock.calls.length
      expect(callsAtLimit).toBeGreaterThan(1)
      await vi.advanceTimersByTimeAsync(15_000)
      expect(getGroupChecks).toHaveBeenCalledTimes(callsAtLimit)
      wrapper.unmount()
    } finally {
      vi.useRealTimers()
    }
  })

  it('shows own running and terminal records, hides other running records and their costs, and closes with Escape', async () => {
    vi.mocked(getGroupChecks).mockResolvedValue([
      { ...base, id: 1, model: 'my-running', status: 'running', is_mine: true, actual_cost: 0 },
      { ...base, id: 2, model: 'other-running', status: 'running', is_mine: false },
      { ...base, id: 3, model: 'other-done', status: 'succeeded', is_mine: false, actual_cost: 5, svg: '<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script><circle r="2"/></svg>' },
    ])
    const wrapper = mount(GroupCheckHistoryPopover, { props: { groupId: 7 }, attachTo: document.body })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('my-running')
    expect(document.body.textContent).toContain('other-done')
    expect(document.body.textContent).not.toContain('other-running')
    expect(document.body.textContent).toContain('$0.000000')
    expect(document.body.textContent).not.toContain('$5.000000')
    const image = document.body.querySelector('img')!
    expect(image.src).toContain('data:image/svg+xml')
    expect(decodeURIComponent(image.src)).not.toContain('<script')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    expect(document.activeElement).toBe(wrapper.get('button').element)
    wrapper.unmount()
  })

  it('closes on outside pointer interaction', async () => {
    vi.mocked(getGroupChecks).mockResolvedValue([])
    const wrapper = mount(GroupCheckHistoryPopover, { props: { groupId: 7 }, attachTo: document.body })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await flushPromises()
    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    wrapper.unmount()
  })
})
