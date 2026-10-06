import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'

import type { ApiKey, Group } from '@/types'
import { keysAPI } from '@/api'
import KeysView from '../KeysView.vue'

const {
  listKeys,
  createKeyWithRequest,
  updateKey,
  getPublicSettings,
  getDashboardApiKeysUsage,
  getAvailableGroups,
  getUserGroupRates,
  showError,
  showSuccess,
  copyToClipboard,
  isCurrentStep,
  nextStep,
} = vi.hoisted(() => ({
  listKeys: vi.fn(),
  createKeyWithRequest: vi.fn(),
  updateKey: vi.fn(),
  getPublicSettings: vi.fn(),
  getDashboardApiKeysUsage: vi.fn(),
  getAvailableGroups: vi.fn(),
  getUserGroupRates: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  copyToClipboard: vi.fn(),
  isCurrentStep: vi.fn(),
  nextStep: vi.fn(),
}))

const messages: Record<string, string> = {
  'common.actions': 'Actions',
  'common.name': 'Name',
  'common.refresh': 'Refresh',
  'common.status': 'Status',
  'keys.apiKey': 'API Key',
  'keys.allGroups': 'All Groups',
  'keys.allStatus': 'All Status',
  'keys.columnSettings': 'Column Settings',
  'keys.createKey': 'Create API Key',
  'keys.created': 'Created',
  'keys.expiresAt': 'Expires',
  'keys.group': 'Group',
  'keys.id': 'ID',
  'keys.currentConcurrency': 'Current Concurrency',
  'keys.lastUsedAt': 'Last Used',
  'keys.lastUsedIP': 'Last Used IP',
  'keys.rateLimitColumn': 'Rate Limit',
  'keys.searchPlaceholder': 'Search name or key...',
  'keys.status.active': 'Active',
  'keys.status.expired': 'Expired',
  'keys.status.inactive': 'Inactive',
  'keys.status.quota_exhausted': 'Quota exhausted',
  'keys.usage': 'Usage',
}

vi.mock('@/api', () => ({
  keysAPI: {
    list: listKeys,
    create: vi.fn(),
    createWithRequest: createKeyWithRequest,
    update: updateKey,
    delete: vi.fn(),
    toggleStatus: vi.fn(),
  },
  authAPI: {
    getPublicSettings,
  },
  usageAPI: {
    getDashboardApiKeysUsage,
  },
  userGroupsAPI: {
    getAvailable: getAvailableGroups,
    getUserGroupRates,
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({
    isCurrentStep,
    nextStep,
  }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard,
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

const createApiKey = (): ApiKey => ({
  id: 1,
  user_id: 1,
  key: 'sk-test-key',
  name: 'test-key',
  group_id: null,
  group_routes: [],
  schedule_mode: 'sequential',
  route_version: 1,
  status: 'active',
  ip_whitelist: [],
  ip_blacklist: [],
  last_used_at: null,
  last_used_ip: null,
  quota: 0,
  quota_used: 0,
  expires_at: null,
  created_at: '2026-06-27T00:00:00Z',
  updated_at: '2026-06-27T00:00:00Z',
  current_concurrency: 3,
  rate_limit_5h: 0,
  rate_limit_1d: 0,
  rate_limit_7d: 0,
  usage_5h: 0,
  usage_1d: 0,
  usage_7d: 0,
  window_5h_start: null,
  window_1d_start: null,
  window_7d_start: null,
  reset_5h_at: null,
  reset_1d_at: null,
  reset_7d_at: null,
})

const createGroup = (id: number, rate = 1): Group => ({
  id,
  name: `group-${id}`,
  description: '',
  platform: 'openai',
  subscription_type: 'standard',
  status: 'active',
  rate_multiplier: rate,
  peak_rate_enabled: false,
  peak_start: '',
  peak_end: '',
  peak_rate_multiplier: 1,
} as Group)

const AppLayoutStub = {
  template: '<div><slot /></div>',
}

const TablePageLayoutStub = {
  template: `
    <div>
      <slot name="filters" />
      <slot name="actions" />
      <slot name="table" />
      <slot name="pagination" />
    </div>
  `,
}

const DataTableStub = {
  name: 'DataTable',
  props: { columns: Array, data: Array, selectedKeys: Array, selectable: Boolean },
  emits: ['sort', 'update:selectedKeys'],
  template: `
    <div>
      <div data-test="columns">{{ columns.map((col) => col.key).join(',') }}</div>
      <div data-test="columns-meta">{{ JSON.stringify(columns.map((col) => ({ key: col.key, sortable: !!col.sortable }))) }}</div>
      <button data-test="sort-current-concurrency" @click="$emit('sort', 'current_concurrency', 'asc')">
        Sort Current Concurrency
      </button>
      <div v-for="row in data" :key="row.id">
        <div
          v-if="columns.some((col) => col.key === 'id')"
          data-test="key-id"
        >
          <slot name="cell-id" :value="row.id" :row="row" />
        </div>
        <slot name="cell-name" :value="row.name" :row="row" />
        <slot name="cell-group" :value="row.group" :row="row" />
        <slot name="cell-actions" :row="row" />
        <div data-test="current-concurrency">
          <slot name="cell-current_concurrency" :value="row.current_concurrency" :row="row" />
        </div>
        <div
          v-if="columns.some((col) => col.key === 'last_used_ip')"
          data-test="last-used-ip"
        >
          <slot name="cell-last_used_ip" :value="row.last_used_ip" :row="row" />
        </div>
        <slot name="cell-actions" :value="row" :row="row" />
      </div>
      <slot name="empty" />
    </div>
  `,
}

const SelectStub = {
  name: 'Select',
  props: ['modelValue', 'options'],
  emits: ['update:modelValue'],
  template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"></select>',
}

const SearchInputStub = {
  name: 'SearchInput',
  props: ['modelValue'],
  emits: ['update:modelValue', 'search'],
  template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
}

const PaginationStub = {
  name: 'Pagination',
  props: ['page', 'total', 'pageSize'],
  emits: ['update:page', 'update:pageSize'],
  template: `
    <div>
      <button data-test="page-size-50" @click="$emit('update:pageSize', 50)">50</button>
      <button data-test="page-2" @click="$emit('update:page', 2)">Page 2</button>
    </div>
  `,
}

const IconStub = {
  props: ['name'],
  template: '<span data-test="icon">{{ name }}</span>',
}

const BaseDialogStub = {
  props: ['show'],
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
}

const mountView = async (renderDialogs = false) => {
  const wrapper = mount(KeysView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: PaginationStub,
        BaseDialog: renderDialogs
          ? BaseDialogStub
          : {
              props: ['show', 'title'],
              emits: ['close'],
              template:
                '<div v-if="show" role="dialog"><button data-test="close-dialog" @click="$emit(\'close\')">Close</button><slot /><slot name="footer" /></div>',
            },
        ConfirmDialog: true,
        EmptyState: true,
        Select: SelectStub,
        SearchInput: SearchInputStub,
        Icon: IconStub,
        UseKeyModal: true,
        BulkEditKeysModal: true,
        EndpointPopover: true,
        GroupBadge: true,
        GroupOptionItem: true,
        Teleport: true,
      },
    },
  })
  await flushPromises()
  await nextTick()
  return wrapper
}

const visibleColumnKeys = (wrapper: VueWrapper) =>
  wrapper.get('[data-test="columns"]').text().split(',').filter(Boolean)

const visibleColumnMeta = (wrapper: VueWrapper): Array<{ key: string; sortable: boolean }> =>
  JSON.parse(wrapper.get('[data-test="columns-meta"]').text())

const getButtonByText = (wrapper: VueWrapper, text: string) => {
  const button = wrapper.findAll('button').find((item) => item.text().includes(text))
  if (!button) {
    throw new Error(`Button not found: ${text}`)
  }
  return button
}

describe('user KeysView column settings', () => {
  beforeEach(() => {
    localStorage.clear()

    listKeys.mockReset()
    createKeyWithRequest.mockReset()
    updateKey.mockReset()
    updateKey.mockReset()
    vi.mocked(keysAPI.create).mockReset()
    getPublicSettings.mockReset()
    getDashboardApiKeysUsage.mockReset()
    getAvailableGroups.mockReset()
    getUserGroupRates.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    copyToClipboard.mockReset()
    isCurrentStep.mockReset()
    nextStep.mockReset()

    listKeys.mockResolvedValue({
      items: [createApiKey()],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    getPublicSettings.mockResolvedValue({})
    getDashboardApiKeysUsage.mockResolvedValue({ stats: {} })
    getAvailableGroups.mockResolvedValue([])
    getUserGroupRates.mockResolvedValue({})
    isCurrentStep.mockReturnValue(false)
    createKeyWithRequest.mockResolvedValue(createApiKey())
    updateKey.mockResolvedValue(createApiKey())
  })

  it.each([
    { initialStatus: 'quota_exhausted', status: 'active', formStatus: 'active' },
    { initialStatus: 'inactive', status: 'inactive', formStatus: 'inactive' },
    { initialStatus: 'active', status: 'active', formStatus: 'inactive' },
  ] as const)('syncs quota reset from $initialStatus to $status with form status $formStatus', async ({ initialStatus, status, formStatus }) => {
    const key: ApiKey = {
      ...createApiKey(), group_id: 1, quota: 10, quota_used: 10,
      status: initialStatus,
    }
    listKeys.mockResolvedValueOnce({ items: [key], total: 1, page: 1, page_size: 20, pages: 1 })
    updateKey.mockResolvedValue({ ...key, status, quota_used: 0 })
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    await wrapper.get('[data-tour="key-form-name"]').setValue('Unsaved name')
    const statusSelect = wrapper.findAllComponents({ name: 'Select' })
      .find((select) => select.props('options').length === 2 &&
        select.props('options')[0].value === 'active')!
    statusSelect.vm.$emit('update:modelValue', 'inactive')
    await wrapper.get('button[title="keys.resetQuotaUsed"]').trigger('click')
    const confirmation = wrapper.findAllComponents({ name: 'ConfirmDialog' })
      .find((dialog) => dialog.props('title') === 'keys.resetQuotaTitle')!
    confirmation.vm.$emit('confirm')
    await flushPromises()

    expect(updateKey).toHaveBeenNthCalledWith(1, key.id, { reset_quota: true })
    expect(wrapper.findComponent({ name: 'DataTable' }).props('data')[0])
      .toMatchObject({ status, quota_used: 0 })
    expect(statusSelect.props('modelValue')).toBe(formStatus)
    expect((wrapper.get('[data-tour="key-form-name"]').element as HTMLInputElement).value)
      .toBe('Unsaved name')

    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    expect(updateKey).toHaveBeenNthCalledWith(2, key.id, expect.objectContaining({ name: 'Unsaved name', status: formStatus }))
    wrapper.unmount()
  })

  it('uses the default API key columns with low-frequency columns hidden', async () => {
    const wrapper = await mountView()

    expect(visibleColumnKeys(wrapper)).toEqual([
      'name',
      'key',
      'group',
      'current_concurrency',
      'usage',
      'expires_at',
      'status',
      'created_at',
      'actions',
    ])
    expect(visibleColumnKeys(wrapper)).not.toContain('rate_limit')
    expect(visibleColumnKeys(wrapper)).not.toContain('last_used_at')
    expect(visibleColumnKeys(wrapper)).not.toContain('last_used_ip')
    expect(visibleColumnKeys(wrapper)).not.toContain('id')
  })

  it('opens bulk editing with only selected visible keys', async () => {
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    expect(table.props('selectable')).toBe(true)
    table.vm.$emit('update:selectedKeys', [1, 99])
    await nextTick()
    await wrapper.get('[data-test="bulk-edit-keys"]').trigger('click')
    const modal = wrapper.findComponent({ name: 'BulkEditKeysModal' })
    expect(modal.props('show')).toBe(true)
    expect(modal.props('selectedKeys').map((key: ApiKey) => key.id)).toEqual([1])
    wrapper.unmount()
  })

  it.each(['filter', 'page size', 'sort'])('clears selection on %s changes', async (change) => {
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    table.vm.$emit('update:selectedKeys', [1])
    await nextTick()
    if (change === 'filter') {
      wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('search')
    } else if (change === 'page size') {
      await wrapper.get('[data-test="page-size-50"]').trigger('click')
    } else {
      table.vm.$emit('sort', 'created_at', 'asc')
    }
    await flushPromises()
    expect(table.props('selectedKeys')).toEqual([])
    expect(wrapper.find('[data-test="bulk-edit-keys"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('removes successful keys from the selection and refreshes the table', async () => {
    listKeys.mockResolvedValue({
      items: [createApiKey(), { ...createApiKey(), id: 2, name: 'Second' }],
      total: 2, pages: 1
    })
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    table.vm.$emit('update:selectedKeys', [1, 2])
    await nextTick()
    await wrapper.get('[data-test="bulk-edit-keys"]').trigger('click')
    wrapper.findComponent({ name: 'BulkEditKeysModal' }).vm.$emit('updated', [1])
    await flushPromises()
    expect(listKeys).toHaveBeenCalledTimes(2)
    expect(table.props('selectedKeys')).toEqual([2])
    wrapper.unmount()
  })

  it('drops keys that are no longer visible after a refresh', async () => {
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })
    table.vm.$emit('update:selectedKeys', [1])
    await nextTick()
    listKeys.mockResolvedValue({ items: [], total: 0, pages: 0 })
    await wrapper.get('button[title="Refresh"]').trigger('click')
    await flushPromises()
    expect(table.props('selectedKeys')).toEqual([])
    wrapper.unmount()
  })

  it('shows a hidden column when toggled and persists the preference', async () => {
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await getButtonByText(wrapper, 'Rate Limit').trigger('click')
    await nextTick()

    expect(visibleColumnKeys(wrapper)).toContain('rate_limit')
    expect(localStorage.getItem('api-key-hidden-columns')).toBe(
      JSON.stringify(['id', 'last_used_at', 'last_used_ip'])
    )
    expect(localStorage.getItem('api-key-column-settings-version')).toBe('3')
  })

  it('shows the API key ID column when toggled', async () => {
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await getButtonByText(wrapper, 'ID').trigger('click')
    await nextTick()

    expect(visibleColumnKeys(wrapper)).toContain('id')
    expect(wrapper.get('[data-test="key-id"]').text()).toBe('#1')
    expect(visibleColumnMeta(wrapper).find((column) => column.key === 'id')?.sortable).toBe(true)
  })

  it('shows the last used IP column when toggled', async () => {
    listKeys.mockResolvedValueOnce({
      items: [{ ...createApiKey(), last_used_ip: '203.0.113.10' }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await getButtonByText(wrapper, 'Last Used IP').trigger('click')
    await nextTick()

    expect(visibleColumnKeys(wrapper)).toContain('last_used_ip')
    expect(wrapper.get('[data-test="last-used-ip"]').text()).toBe('203.0.113.10')
  })

  it('restores column preferences from localStorage on mount', async () => {
    localStorage.setItem('api-key-hidden-columns', JSON.stringify(['group', 'created_at']))
    localStorage.setItem('api-key-column-settings-version', '1')

    const wrapper = await mountView()

    expect(visibleColumnKeys(wrapper)).toEqual([
      'name',
      'key',
      'current_concurrency',
      'usage',
      'rate_limit',
      'expires_at',
      'status',
      'last_used_at',
      'actions',
    ])
    expect(localStorage.getItem('api-key-hidden-columns')).toBe(
      JSON.stringify(['group', 'created_at', 'last_used_ip', 'id'])
    )
    expect(localStorage.getItem('api-key-column-settings-version')).toBe('3')
  })

  it('does not include always-visible columns in the toggleable menu', async () => {
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await nextTick()

    const columnMenuText = wrapper.text()
    expect(columnMenuText).toContain('API Key')
    expect(columnMenuText).toContain('ID')
    expect(columnMenuText).toContain('Current Concurrency')
    expect(columnMenuText).toContain('Rate Limit')
    expect(columnMenuText).toContain('Last Used IP')
    expect(columnMenuText).not.toContain('Name')
    expect(columnMenuText).not.toContain('Actions')
  })

  it('renders the current concurrency value', async () => {
    const wrapper = await mountView()

    expect(wrapper.get('[data-test="current-concurrency"]').text()).toBe('3')
  })

  it('marks current concurrency as sortable', async () => {
    const wrapper = await mountView()

    const currentConcurrencyColumn = visibleColumnMeta(wrapper).find(
      (column) => column.key === 'current_concurrency'
    )
    expect(currentConcurrencyColumn?.sortable).toBe(true)
  })

  it.each([
    { key: 'current_concurrency', order: 'asc' },
    { key: 'group', order: 'asc' },
    { key: 'group', order: 'desc' },
  ] as const)('keeps filters and resets pagination and selection when sorting $key $order', async ({ key, order }) => {
    getAvailableGroups.mockResolvedValue([{ id: 42, name: 'OpenAI' }])
    const wrapper = await mountView()

    await wrapper.get('[data-test="page-size-50"]').trigger('click')
    await flushPromises()

    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('update:modelValue', 'target')
    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('search')
    await flushPromises()

    const selects = wrapper.findAllComponents({ name: 'Select' })
    await selects[0].vm.$emit('update:modelValue', 42)
    await flushPromises()
    await selects[1].vm.$emit('update:modelValue', 'active')
    await flushPromises()

    await wrapper.get('[data-test="page-2"]').trigger('click')
    await flushPromises()
    const table = wrapper.findComponent({ name: 'DataTable' })
    table.vm.$emit('update:selectedKeys', [1])
    await nextTick()
    expect(table.props('selectedKeys')).toEqual([1])
    expect(visibleColumnMeta(wrapper).find((column) => column.key === key)?.sortable).toBe(true)
    listKeys.mockClear()

    table.vm.$emit('sort', key, order)
    await flushPromises()

    expect(table.props('selectedKeys')).toEqual([])
    expect(listKeys).toHaveBeenLastCalledWith(
      1,
      50,
      {
        search: 'target',
        status: 'active',
        group_id: 42,
        sort_by: key,
        sort_order: order,
      },
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
  })

  describe('create provider selection', () => {
    const groupFor = (id: number, platform: Group['platform']) => ({
      ...createGroup(id),
      name: `group-${platform}-${id}`,
      platform,
    })

    const availableGroups = [
      groupFor(1, 'anthropic'),
      groupFor(2, 'openai'),
      groupFor(3, 'kimi'),
      groupFor(4, 'zhipu'),
      groupFor(5, 'deepseek'),
      groupFor(6, 'minimax'),
      groupFor(7, 'gemini'),
      groupFor(8, 'grok'),
      groupFor(9, 'antigravity'),
      groupFor(10, 'composite'),
      groupFor(11, 'opencode_go'),
      groupFor(12, 'typesafe'),
    ]

    const routeSelector = (wrapper: VueWrapper) =>
      wrapper.findComponent({ name: 'ApiKeyGroupRouteSelector' })
    const routeGroupIds = (wrapper: VueWrapper) =>
      (routeSelector(wrapper).props('groups') as Group[]).map((group) => group.id)

    beforeEach(() => {
      getAvailableGroups.mockResolvedValue(availableGroups)
    })

    it('filters route groups by provider family and classifies configured platforms', async () => {
      const wrapper = await mountView(true)
      await getButtonByText(wrapper, 'Create API Key').trigger('click')
      await nextTick()

      expect(wrapper.findAll('input[name="key-provider"]')).toHaveLength(4)
      expect(routeGroupIds(wrapper)).toEqual([1])

      await wrapper.get('input[name="key-provider"][value="openai"]').setValue()
      expect(routeGroupIds(wrapper)).toEqual([2])

      await wrapper.get('input[name="key-provider"][value="domestic"]').setValue()
      expect(routeGroupIds(wrapper).sort((a, b) => a - b)).toEqual([3, 4, 5, 6])

      await wrapper.get('input[name="key-provider"][value="other"]').setValue()
      expect(routeGroupIds(wrapper).sort((a, b) => a - b)).toEqual([7, 8, 9, 10, 11, 12])
    })

    it('clears routes when switching provider and submits only the selected family', async () => {
      const wrapper = await mountView(true)
      await getButtonByText(wrapper, 'Create API Key').trigger('click')
      await wrapper.get('[data-test="route-group-trigger"]').trigger('click')
      await wrapper.get('[data-test="route-group-option-1"]').trigger('click')
      await wrapper.get('input[name="key-provider"][value="openai"]').setValue()

      expect(routeSelector(wrapper).props('modelValue')).toEqual([])
      await wrapper.get('[data-tour="key-form-name"]').setValue('openai-key')
      await wrapper.get('[data-test="route-group-trigger"]').trigger('click')
      await wrapper.get('[data-test="route-group-option-2"]').trigger('click')
      await wrapper.get('#key-form').trigger('submit')
      await flushPromises()

      expect(createKeyWithRequest).toHaveBeenCalledWith(expect.objectContaining({
        name: 'openai-key',
        group_id: 2,
        group_routes: [{ group_id: 2, priority: 0 }],
      }))
    })

    it('defaults to an available provider and keeps all groups while editing', async () => {
      getAvailableGroups.mockResolvedValue([groupFor(2, 'openai'), groupFor(3, 'kimi')])
      const wrapper = await mountView(true)
      await getButtonByText(wrapper, 'Create API Key').trigger('click')
      expect(wrapper.get<HTMLInputElement>('input[name="key-provider"][value="openai"]').element.checked).toBe(true)
      expect(wrapper.get<HTMLInputElement>('input[name="key-provider"][value="anthropic"]').element.disabled).toBe(true)
      expect(routeGroupIds(wrapper)).toEqual([2])

      listKeys.mockResolvedValue({
        items: [{ ...createApiKey(), group_id: 2, group: groupFor(2, 'openai'),
          group_routes: [{ group_id: 2, priority: 0, enabled: true, group: groupFor(2, 'openai') }] }],
        total: 1, page: 1, page_size: 20, pages: 1,
      })
      await getButtonByText(wrapper, 'common.cancel').trigger('click')
      await wrapper.get('[data-test="edit-api-key-1"]').trigger('click')
      expect(wrapper.find('[data-tour="key-form-provider"]').exists()).toBe(false)
      expect(routeGroupIds(wrapper).sort((a, b) => a - b)).toEqual([2, 3])
    })
  })

  it('keeps single-group keys free of routing controls while allowing the common selector', async () => {
    getAvailableGroups.mockResolvedValue([createGroup(10), createGroup(20)])
    const wrapper = await mountView(true)
    await getButtonByText(wrapper, 'Create API Key').trigger('click')
    expect(wrapper.find('[data-test="route-group-trigger"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="smart-balance-presets"]').exists()).toBe(false)
    await wrapper.get('[data-test="route-group-trigger"]').trigger('click')
    await wrapper.get('[data-test="route-group-option-10"]').trigger('click')
    await nextTick()
    await wrapper.get('[data-tour="key-form-name"]').setValue('legacy')
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    const payload = createKeyWithRequest.mock.calls.at(-1)?.[0]
    expect(payload.group_id).toBe(10)
    expect(payload.group_routes).toEqual([{ group_id: 10, priority: 0 }])
    expect(payload.schedule_mode).toBeUndefined()
    expect(payload.smart_preference).toBeUndefined()
    expect(payload.routing_min_success_rate).toBeUndefined()
  })

  it('keeps multi-group controls available for every user', async () => {
    const group = createGroup(10), second = createGroup(20)
    getAvailableGroups.mockResolvedValue([group, second])
    listKeys.mockResolvedValue({ items: [{ ...createApiKey(), group_id: 10, group,
      group_routes: [{ group_id: 10, priority: 0, enabled: true, group }, { group_id: 20, priority: 1, enabled: true, group: second }],
      routing_min_success_rate: 95 }],
      total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = await mountView(true)
    expect(wrapper.get('[data-test="api-key-groups-1"]').text()).toContain('+1')
    await wrapper.get('[data-test="edit-api-key-1"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="route-group-trigger"]').exists()).toBe(true)
    await wrapper.get('[data-tour="key-form-name"]').setValue('renamed')
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    const payload = updateKey.mock.calls.at(-1)?.[1]
    expect(payload.name).toBe('renamed')
    expect(payload.group_routes).toEqual([{ group_id: 10, priority: 0 }, { group_id: 20, priority: 1 }])
    expect(payload.schedule_mode).toBeUndefined()
  })

  it('creates an ordered sequential route set without failover-mode controls', async () => {
    getAvailableGroups.mockResolvedValue([createGroup(10, 1.2), createGroup(20, 0.8)])
    const wrapper = await mountView(true)

    await getButtonByText(wrapper, 'Create API Key').trigger('click')
    await nextTick()
    await wrapper.get('[data-tour="key-form-name"]').setValue('multi-route-key')
    await wrapper.get('[data-test="route-group-trigger"]').trigger('click')
    await wrapper.get('[data-test="route-group-option-10"]').trigger('click')
    await wrapper.get('[data-test="route-group-option-20"]').trigger('click')
    await wrapper.get('[data-test="drag-route-20"]').trigger('keydown', { key: 'ArrowUp' })
    // Failover mode is no longer user-selectable; the order is the policy.
    expect(wrapper.find('[data-test="schedule-mode-smart"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="schedule-mode-sequential"]').exists()).toBe(false)
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()

    expect(createKeyWithRequest).toHaveBeenCalledWith(expect.objectContaining({
      name: 'multi-route-key',
      group_id: 20,
      group_routes: [
        { group_id: 20, priority: 0 },
        { group_id: 10, priority: 1 },
      ],
    }))
    const payload = createKeyWithRequest.mock.calls.at(-1)?.[0]
    expect(payload.schedule_mode).toBeUndefined()
    expect(payload.smart_preference).toBeUndefined()
    expect(payload.smart_balance_bps).toBeUndefined()
    expect(payload.routing_min_success_rate).toBeUndefined()
  })

  it('shows the multi-group badge without any failover-mode badge', async () => {
    const groups = [createGroup(10), createGroup(20)].slice(0, 2)
    listKeys.mockResolvedValue({ items: [{ ...createApiKey(),
      group_id: groups[0]?.id ?? null, group: groups[0] ?? null,
      group_routes: groups.map((group, priority) => ({ group_id: group.id, priority, enabled: true, group })) }],
      total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = await mountView()
    const groupCell = wrapper.get('[data-test="api-key-groups-1"]')
    expect(groupCell.text()).toContain('+1')
    expect(groupCell.text().includes('keys.scheduleSmart')).toBe(false)
  })

  it('hides failover-mode settings and does not submit routing policy fields', async () => {
    const group = createGroup(10)
    getAvailableGroups.mockResolvedValue([group])
    listKeys.mockResolvedValue({ items: [{ ...createApiKey(), group_id: 10, group,
      group_routes: [{ group_id: 10, priority: 0, enabled: true, group }],
      routing_min_success_rate: 95 }],
      total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = await mountView(true)
    await wrapper.get('[data-test="edit-api-key-1"]').trigger('click')
    expect(wrapper.find('[data-test="schedule-mode-smart"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="smart-balance-presets"]').exists()).toBe(false)
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    const payload = updateKey.mock.calls.at(-1)?.[1]
    expect(payload.schedule_mode).toBeUndefined()
    expect(payload.smart_preference).toBeUndefined()
    expect(payload.routing_min_success_rate).toBeUndefined()
  })

  it('uses route-version CAS and reloads after an edit conflict', async () => {
    const group = createGroup(10)
    listKeys.mockResolvedValue({
      items: [{
        ...createApiKey(),
        group_id: group.id,
        group,
        group_routes: [{ group_id: group.id, priority: 0, enabled: true, group }],
        route_version: 7,
      }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    getAvailableGroups.mockResolvedValue([group])
    updateKey.mockRejectedValue({ status: 409 })
    const wrapper = await mountView(true)

    await wrapper.get('[data-test="edit-api-key-1"]').trigger('click')
    await nextTick()
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()

    expect(updateKey).toHaveBeenCalledWith(1, expect.objectContaining({
      group_id: 10,
      group_routes: [{ group_id: 10, priority: 0 }],
      expected_route_version: 7,
    }))
    expect(showError).toHaveBeenCalledWith('keys.routeConfigConflict')
    expect(listKeys).toHaveBeenCalledTimes(2)
  })

})
