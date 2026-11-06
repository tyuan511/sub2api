import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import { DOMWrapper } from '@vue/test-utils'
import DateRangePicker from '../DateRangePicker.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, locale: ref('en') }) }))
let wrapper: ReturnType<typeof mount<typeof DateRangePicker>>
beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(new Date(2026, 8, 13, 12)) })
afterEach(() => { wrapper?.unmount(); document.body.innerHTML = ''; vi.useRealTimers() })

async function chooseDraft() {
  wrapper = mount(DateRangePicker, {
    props: {
      startDate: '2026-09-13', endDate: '2026-09-13',
      'onUpdate:startDate': (startDate: string) => { void wrapper.setProps({ startDate }) },
      'onUpdate:endDate': (endDate: string) => { void wrapper.setProps({ endDate }) },
    },
  })
  await wrapper.get('.date-picker-trigger').trigger('click')
  // 面板 teleport 到 body，需从 document 中查询
  const preset = Array.from(document.body.querySelectorAll<HTMLElement>('.date-picker-preset'))
    .find(node => node.textContent === 'dates.last7Days')!
  await new DOMWrapper(preset).trigger('click')
}

const draftDateInputs = () =>
  Array.from(document.body.querySelectorAll<HTMLInputElement>('input[type="date"]')).map(input => input.value)

describe('DateRangePicker unapplied changes', () => {
  it.each(['outside', 'escape', 'toggle'])('discards draft dates when dismissed with %s', async (method) => {
    await chooseDraft()
    if (method === 'outside') document.body.click()
    else if (method === 'escape') document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    else await wrapper.get('.date-picker-trigger').trigger('click')
    await nextTick()
    expect(wrapper.emitted('change')).toBeUndefined()
    expect(wrapper.get('.date-picker-trigger').text()).toContain('dates.today')
    await wrapper.get('.date-picker-trigger').trigger('click')
    expect(draftDateInputs()).toEqual(['2026-09-13', '2026-09-13'])
  })

  it('retains a newly applied range after the parent accepts both updates', async () => {
    await chooseDraft()
    await new DOMWrapper(document.body.querySelector<HTMLElement>('.date-picker-apply')!).trigger('click')
    await nextTick()
    expect(wrapper.emitted('change')).toEqual([[{ startDate: '2026-09-07', endDate: '2026-09-13', preset: '7days' }]])
    expect(wrapper.get('.date-picker-trigger').text()).toContain('dates.last7Days')
    await wrapper.get('.date-picker-trigger').trigger('click')
    expect(draftDateInputs()).toEqual(['2026-09-07', '2026-09-13'])
  })
})
