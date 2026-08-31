import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import AdminSupportWidget from '../AdminSupportWidget.vue'

vi.mock('@/stores/support', () => ({
  useSupportStore: () => ({ unreadCount: 7 })
}))

afterEach(() => {
  document.body.innerHTML = ''
})

function mountWidget() {
  return mount(AdminSupportWidget, {
    attachTo: document.body,
    global: {
      stubs: {
        Icon: { template: '<span data-icon />' },
        SupportView: { template: '<div data-support-view />' }
      }
    }
  })
}

describe('AdminSupportWidget', () => {
  it('opens the admin support workspace from the floating message button', async () => {
    mountWidget()

    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
    const trigger = document.body.querySelector<HTMLButtonElement>('[aria-label="打开客服工作台"]')!
    expect(trigger.textContent).toContain('7')

    trigger.click()
    await nextTick()

    expect(document.body.querySelector('[role="dialog"]')).not.toBeNull()
    expect(document.body.querySelector('[data-support-view]')).not.toBeNull()
  })

  it('maximizes, restores, and closes the workspace', async () => {
    mountWidget()
    document.body.querySelector<HTMLButtonElement>('[aria-label="打开客服工作台"]')!.click()
    await nextTick()

    const dialog = document.body.querySelector<HTMLElement>('[role="dialog"]')!
    const maximize = document.body.querySelector<HTMLButtonElement>('[aria-label="全屏显示客服窗口"]')!
    maximize.click()
    await nextTick()

    expect(dialog.className).toContain('fixed inset-2')
    expect(document.body.querySelector('[aria-label="还原客服窗口"]')).not.toBeNull()

    const close = document.body.querySelector<HTMLButtonElement>('[aria-label="关闭客服工作台"]')!
    close.click()
    await nextTick()
    await new Promise((resolve) => setTimeout(resolve, 200))

    expect(document.body.querySelector('[role="dialog"]')).toBeNull()
  })
})
