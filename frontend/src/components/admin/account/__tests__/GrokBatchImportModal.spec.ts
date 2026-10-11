import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'

import GrokBatchImportModal from '../GrokBatchImportModal.vue'

const { validateSSOToken, createAccount, showError, showWarning, showSuccess } = vi.hoisted(() => ({
  validateSSOToken: vi.fn(),
  createAccount: vi.fn(),
  showError: vi.fn(),
  showWarning: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: { create: createAccount },
    grok: { validateSSOToken }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showWarning, showSuccess, showInfo: vi.fn() })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const BaseDialogStub = defineComponent({
  props: { show: Boolean, title: String },
  template: '<div v-if="show" data-test="dialog"><slot /><slot name="footer" /></div>'
})

const GroupSelectorStub = defineComponent({
  props: { modelValue: { type: Array, default: () => [] }, groups: { type: Array, default: () => [] } },
  emits: ['update:modelValue'],
  template: '<div data-test="group-selector">{{ modelValue.join(",") }}</div>'
})

const grokGroups = [
  { id: 42, name: 'grok-1', platform: 'grok', rate_multiplier: 0.1 },
  { id: 11, name: 'grok', platform: 'grok', rate_multiplier: 0.01 },
  { id: 9, name: 'free-codex', platform: 'openai', rate_multiplier: 0.01 }
]

function mountModal() {
  return mount(GrokBatchImportModal, {
    props: { show: true, groups: grokGroups as never },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        GroupSelector: GroupSelectorStub,
        Teleport: true
      }
    }
  })
}

const emailBySSO: Record<string, string> = {
  'sso-a': 'a@example.com',
  'sso-b': 'b@example.com',
  'sso-ok': 'ok@example.com'
}

function tokenInfo(email: string) {
  return {
    access_token: `at-${email}`,
    refresh_token: `rt-${email}`,
    token_type: 'Bearer',
    expires_at: '2030-01-01T00:00:00Z',
    email,
    subscription_tier: 'heavy'
  }
}

describe('GrokBatchImportModal', () => {
  beforeEach(() => {
    validateSSOToken
      .mockReset()
      .mockImplementation(async (token: string) => tokenInfo(emailBySSO[token] ?? `${token}@example.com`))
    createAccount.mockReset().mockResolvedValue({ id: 1 })
    showError.mockReset()
    showWarning.mockReset()
    showSuccess.mockReset()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('preselects the first grok group only', async () => {
    const wrapper = mountModal()
    await flushPromises()

    expect(wrapper.get('[data-test="group-selector"]').text()).toBe('42')
    wrapper.unmount()
  })

  it('creates one oauth account per line named after the email, with fixed grok models', async () => {
    const wrapper = mountModal()
    await flushPromises()

    await wrapper.get('textarea').setValue(
      ['a@example.com----pw----sso-a', 'b@example.com----pw----sso-b'].join('\n')
    )
    await wrapper.get('button.btn-primary').trigger('click')
    await flushPromises()

    expect(validateSSOToken).toHaveBeenCalledTimes(2)
    expect(createAccount).toHaveBeenCalledTimes(2)
    const payloads = createAccount.mock.calls.map((call) => call[0])
    expect(payloads.map((payload) => payload.name).sort()).toEqual(['a@example.com', 'b@example.com'])
    for (const payload of payloads) {
      expect(payload.platform).toBe('grok')
      expect(payload.type).toBe('oauth')
      expect(payload.credentials.model_mapping).toEqual({ 'grok-4.6': 'grok-4.6', 'grok-4.7': 'grok-4.7' })
      // 密码与 SSO Cookie 绝不写入账号凭据。
      expect(payload.credentials.password).toBeUndefined()
      expect(payload.credentials.sso_token).toBeUndefined()
      expect(payload.concurrency).toBe(100)
      expect(payload.priority).toBe(1)
      expect(payload.group_ids).toEqual([42])
    }
    expect(showSuccess).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('reports per-line failures without stopping the batch', async () => {
    validateSSOToken.mockImplementation(async (token: string) => {
      if (token === 'sso-bad') return null
      return tokenInfo(emailBySSO[token] ?? `${token}@example.com`)
    })
    const wrapper = mountModal()
    await flushPromises()

    await wrapper.get('textarea').setValue(
      ['good----pw----sso-ok', 'bad----pw----sso-bad'].join('\n')
    )
    await wrapper.get('button.btn-primary').trigger('click')
    await flushPromises()

    expect(createAccount).toHaveBeenCalledTimes(1)
    expect(showWarning).toHaveBeenCalled()
    expect(wrapper.text()).toContain('#2')
    wrapper.unmount()
  })
})
