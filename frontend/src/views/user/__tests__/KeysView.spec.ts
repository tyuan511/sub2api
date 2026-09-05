import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'

import type { ApiKey, Group } from '@/types'
import KeysView from '../KeysView.vue'

const {
  listKeys,
  getRoutingCapabilities,
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
  getRoutingCapabilities: vi.fn(),
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
    getRoutingCapabilities,
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
  smart_preference: null,
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
  props: ['columns', 'data'],
  emits: ['sort'],
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
        BaseDialog: renderDialogs ? BaseDialogStub : true,
        ConfirmDialog: true,
        EmptyState: true,
        Select: SelectStub,
        SearchInput: SearchInputStub,
        Icon: IconStub,
        UseKeyModal: true,
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
    getRoutingCapabilities.mockReset().mockResolvedValue({ multi_group_routing_enabled: true })
    createKeyWithRequest.mockReset()
    updateKey.mockReset()
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

  it('keeps filters and selected page size when sorting by current concurrency', async () => {
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

    listKeys.mockClear()

    await wrapper.get('[data-test="sort-current-concurrency"]').trigger('click')
    await flushPromises()

    expect(listKeys).toHaveBeenLastCalledWith(
      1,
      50,
      {
        search: 'target',
        status: 'active',
        group_id: 42,
        sort_by: 'current_concurrency',
        sort_order: 'asc',
      },
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
  })

  it('keeps non-beta users on the legacy single-group form and request contract', async () => {
    getRoutingCapabilities.mockResolvedValue({ multi_group_routing_enabled: false })
    getAvailableGroups.mockResolvedValue([createGroup(10), createGroup(20)])
    const wrapper = await mountView(true)
    await getButtonByText(wrapper, 'Create API Key').trigger('click')
    expect(wrapper.find('[data-test="route-group-trigger"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="routing-success-slider"]').exists()).toBe(false)
    wrapper.getComponent('[data-test="legacy-group-select"]').vm.$emit('update:modelValue', 10)
    await nextTick()
    await wrapper.get('[data-tour="key-form-name"]').setValue('legacy')
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    const payload = createKeyWithRequest.mock.calls.at(-1)?.[0]
    expect(payload.group_id).toBe(10)
    for (const field of ['group_routes', 'schedule_mode', 'smart_preference', 'smart_balance_bps', 'routing_min_success_rate']) expect(payload[field]).toBeUndefined()
  })

  it('preserves dormant routing configuration when a removed user edits only the key name', async () => {
    getRoutingCapabilities.mockResolvedValue({ multi_group_routing_enabled: false })
    const group = createGroup(10), second = createGroup(20)
    getAvailableGroups.mockResolvedValue([group, second])
    listKeys.mockResolvedValue({ items: [{ ...createApiKey(), group_id: 10, group,
      group_routes: [{ group_id: 10, priority: 0, enabled: true, group }, { group_id: 20, priority: 1, enabled: true, group: second }],
      schedule_mode: 'smart', smart_preference: 'price', smart_balance_bps: 3000, routing_min_success_rate: 95 }],
      total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = await mountView(true)
    expect(wrapper.get('[data-test="api-key-groups-1"]').text()).not.toContain('keys.scheduleSmart')
    await wrapper.get('[data-test="edit-api-key-1"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="route-group-trigger"]').exists()).toBe(false)
    await wrapper.get('[data-tour="key-form-name"]').setValue('renamed')
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    const payload = updateKey.mock.calls.at(-1)?.[1]
    expect(payload.name).toBe('renamed')
    for (const field of ['group_id', 'group_routes', 'schedule_mode', 'smart_preference', 'smart_balance_bps', 'routing_min_success_rate']) expect(payload[field]).toBeUndefined()
  })

  it('fails closed to the legacy form when capability lookup fails', async () => {
    getRoutingCapabilities.mockRejectedValue(new Error('offline'))
    const wrapper = await mountView(true)
    await getButtonByText(wrapper, 'Create API Key').trigger('click')
    expect(wrapper.find('[data-test="legacy-group-select"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="route-group-trigger"]').exists()).toBe(false)
  })

  it('creates an ordered smart route set with an explicit preference', async () => {
    getAvailableGroups.mockResolvedValue([createGroup(10, 1.2), createGroup(20, 0.8)])
    const wrapper = await mountView(true)

    await getButtonByText(wrapper, 'Create API Key').trigger('click')
    await nextTick()
    await wrapper.get('[data-tour="key-form-name"]').setValue('multi-route-key')
    await wrapper.get('[data-test="route-group-trigger"]').trigger('click')
    await wrapper.get('[data-test="route-group-option-10"]').trigger('click')
    await wrapper.get('[data-test="route-group-option-20"]').trigger('click')
    await wrapper.get('[data-test="move-route-up-20"]').trigger('click')
    await wrapper.get('[data-test="schedule-mode-smart"]').trigger('click')
    expect(wrapper.get('[data-test="smart-balance-slider"] input').attributes('step')).toBe('500')
    await wrapper.get('[data-test="smart-balance-slider"] input').setValue(3000)
    await wrapper.get('[data-test="routing-success-slider"] input').setValue(85)
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()

    expect(createKeyWithRequest).toHaveBeenCalledWith(expect.objectContaining({
      name: 'multi-route-key',
      group_id: 20,
      group_routes: [
        { group_id: 20, priority: 0 },
        { group_id: 10, priority: 1 },
      ],
      schedule_mode: 'smart',
      smart_preference: 'price',
      smart_balance_bps: 3000,
      routing_min_success_rate: 85,
    }))
  })

  it('defaults new keys to 80%, resets to 80%, and restores the default on reopening', async () => {
    getAvailableGroups.mockResolvedValue([createGroup(10), createGroup(20)])
    const wrapper = await mountView(true)
    await getButtonByText(wrapper, 'Create API Key').trigger('click')
    await nextTick()
    const selectTwoGroups = async () => {
      await wrapper.get('[data-test="route-group-trigger"]').trigger('click')
      await wrapper.get('[data-test="route-group-option-10"]').trigger('click')
      await wrapper.get('[data-test="route-group-option-20"]').trigger('click')
    }
    await selectTwoGroups()
    const slider = () => wrapper.get('[data-test="routing-success-slider"] input')
    expect((slider().element as HTMLInputElement).value).toBe('80')
    await slider().setValue(95)
    await wrapper.get('[data-test="routing-success-slider"] button').trigger('click')
    expect((slider().element as HTMLInputElement).value).toBe('80')
    await wrapper.get('[data-tour="key-form-name"]').setValue('default-threshold-key')
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    expect(createKeyWithRequest).toHaveBeenCalledWith(expect.objectContaining({ routing_min_success_rate: 80 }))
    await getButtonByText(wrapper, 'Create API Key').trigger('click')
    await nextTick()
    await selectTwoGroups()
    expect((slider().element as HTMLInputElement).value).toBe('80')
  })

  it('shows controls only for multiple groups and does not submit hidden preferences', async () => {
    getAvailableGroups.mockResolvedValue([createGroup(10), createGroup(20)])
    const wrapper = await mountView(true)
    await getButtonByText(wrapper, 'Create API Key').trigger('click')
    const expectHidden = () => {
      expect(wrapper.find('[data-test="schedule-mode-smart"]').exists()).toBe(false)
      expect(wrapper.find('[data-test="routing-success-slider"]').exists()).toBe(false)
      expect(wrapper.find('[data-test="smart-balance-slider"]').exists()).toBe(false)
    }
    expectHidden()
    await wrapper.get('[data-test="route-group-trigger"]').trigger('click')
    await wrapper.get('[data-test="route-group-option-10"]').trigger('click')
    expectHidden()
    await wrapper.get('[data-test="route-group-option-20"]').trigger('click')
    expect(wrapper.find('[data-test="routing-success-slider"]').exists()).toBe(true)
    await wrapper.get('[data-test="schedule-mode-smart"]').trigger('click')
    await wrapper.get('[data-test="smart-balance-slider"] input').setValue(7350)
    await wrapper.get('[data-test="routing-success-slider"] input').setValue(95)
    await wrapper.get('[data-test="remove-route-group-20"]').trigger('click')
    expectHidden()
    await wrapper.get('[data-tour="key-form-name"]').setValue('fixed-group')
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    const payload = createKeyWithRequest.mock.calls.at(-1)?.[0]
    expect(payload).toMatchObject({ group_routes: [{ group_id: 10, priority: 0 }], schedule_mode: 'sequential', smart_preference: null })
    expect(payload.smart_balance_bps).toBeUndefined()
    expect(payload.routing_min_success_rate).toBeUndefined()
  })

  it.each([0, 1, 2])('shows the smart badge only for multiple enabled groups (count=%s)', async (count) => {
    const groups = [createGroup(10), createGroup(20)].slice(0, count)
    listKeys.mockResolvedValue({ items: [{ ...createApiKey(),
      group_id: groups[0]?.id ?? null, group: groups[0] ?? null,
      group_routes: groups.map((group, priority) => ({ group_id: group.id, priority, enabled: true, group })),
      schedule_mode: 'smart' }], total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = await mountView()
    const groupCell = wrapper.get('[data-test="api-key-groups-1"]')
    expect(groupCell.text().includes('keys.scheduleSmart')).toBe(count > 1)
    expect(groupCell.text().includes('keys.noGroup')).toBe(count === 0)
  })

  it('hides old single-group smart settings without overwriting its stored threshold', async () => {
    const group = createGroup(10)
    getAvailableGroups.mockResolvedValue([group])
    listKeys.mockResolvedValue({ items: [{ ...createApiKey(), group_id: 10, group,
      group_routes: [{ group_id: 10, priority: 0, enabled: true, group }],
      schedule_mode: 'smart', smart_preference: 'price', smart_balance_bps: 3000, routing_min_success_rate: 95 }],
      total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = await mountView(true)
    await wrapper.get('[data-test="edit-api-key-1"]').trigger('click')
    expect(wrapper.find('[data-test="routing-success-slider"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="schedule-mode-smart"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="smart-balance-slider"]').exists()).toBe(false)
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    const payload = updateKey.mock.calls.at(-1)?.[1]
    expect(payload).toMatchObject({ schedule_mode: 'sequential', smart_preference: null })
    expect(payload.routing_min_success_rate).toBeUndefined()
  })

  it.each([
    { preference: 'price', stored: null, expected: 1250, threshold: 50 },
    { preference: 'speed', stored: null, expected: 8750, threshold: 50 },
    { preference: 'price', stored: 0, expected: 0, threshold: 95 },
    { preference: 'speed', stored: 7350, expected: 7350, threshold: 85 },
  ])('restores exact controls and compatible legacy preference $preference/$stored', async ({ preference, stored, expected, threshold }) => {
    const group = createGroup(10)
    const second = createGroup(20)
    listKeys.mockResolvedValue({
      items: [{ ...createApiKey(), group_id: 10, group,
        group_routes: [{ group_id: 10, priority: 0, enabled: true, group }, { group_id: 20, priority: 1, enabled: true, group: second }],
        schedule_mode: 'smart', smart_preference: preference, smart_balance_bps: stored,
        routing_min_success_rate: threshold, route_version: 8 }],
      total: 1, page: 1, page_size: 20, pages: 1,
    })
    getAvailableGroups.mockResolvedValue([group, second])
    const wrapper = await mountView(true)
    await wrapper.get('[data-test="edit-api-key-1"]').trigger('click')
    await nextTick()
    expect((wrapper.get('[data-test="smart-balance-slider"] input').element as HTMLInputElement).value).toBe(String(expected))
    expect((wrapper.get('[data-test="routing-success-slider"] input').element as HTMLInputElement).value).toBe(String(threshold))
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    expect(updateKey).toHaveBeenCalledWith(1, expect.objectContaining({
      smart_balance_bps: expected, routing_min_success_rate: threshold, expected_route_version: 8,
    }))
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
    updateKey.mockRejectedValue({ response: { status: 409 } })
    const wrapper = await mountView(true)

    await wrapper.get('[data-test="edit-api-key-1"]').trigger('click')
    await nextTick()
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()

    expect(updateKey).toHaveBeenCalledWith(1, expect.objectContaining({
      group_id: 10,
      group_routes: [{ group_id: 10, priority: 0 }],
      schedule_mode: 'sequential',
      smart_preference: null,
      expected_route_version: 7,
    }))
    expect(showError).toHaveBeenCalledWith('keys.routeConfigConflict')
    expect(listKeys).toHaveBeenCalledTimes(2)
  })
})
