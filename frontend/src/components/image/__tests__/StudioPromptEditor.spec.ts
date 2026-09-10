import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import StudioPromptEditor from '../StudioPromptEditor.vue'

vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<object>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

describe('StudioPromptEditor paste', () => {
  afterEach(() => vi.restoreAllMocks())
  it('turns a pasted mention token into a filename chip', async () => {
    const wrapper = mount(StudioPromptEditor, {
      props: { modelValue: '', references: [{ name: 'cat.png' }, { name: 'dog.png' }] },
      global: { stubs: { Teleport: true } },
    })
    const editor = wrapper.get('.studio-prompt-input').element as HTMLElement
    document.execCommand = vi.fn((_command: string, _ui: boolean, value?: string) => {
      editor.textContent = value || ''
      return true
    }) as unknown as typeof document.execCommand
    await wrapper.get('.studio-prompt-input').trigger('paste', { clipboardData: { getData: () => '用参考图1的风格改参考图2' } })
    expect(Array.from(wrapper.findAll('.prompt-mention')).map(el => el.text())).toEqual(['@cat.png', '@dog.png'])
  })
  it('warns when a pasted prompt references images with no matching reference', async () => {
    const wrapper = mount(StudioPromptEditor, {
      props: { modelValue: '', references: [] },
      global: { stubs: { Teleport: true } },
    })
    const editor = wrapper.get('.studio-prompt-input').element as HTMLElement
    document.execCommand = vi.fn((_command: string, _ui: boolean, value?: string) => {
      editor.textContent = value || ''
      return true
    }) as unknown as typeof document.execCommand
    await wrapper.get('.studio-prompt-input').trigger('paste', { clipboardData: { getData: () => '用@cat.png的风格' } })
    expect(wrapper.emitted('mentionsUnresolved')).toHaveLength(1)
  })
})
