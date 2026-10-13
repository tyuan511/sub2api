<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <input v-model="search" class="input w-full sm:w-64" :placeholder="t('admin.upstreams.search')" :aria-label="t('admin.upstreams.search')" />
          <select v-model="typeFilter" class="input w-full sm:w-40" :aria-label="t('admin.upstreams.type')">
            <option value="">{{ t('admin.upstreams.allTypes') }}</option>
            <option v-for="type in types" :key="type" :value="type">{{ typeLabel(type) }}</option>
          </select>
          <div class="flex flex-1 justify-end gap-2">
            <button class="btn btn-secondary" :disabled="loading" @click="load(true)">{{ t('common.refresh') }}</button>
            <button class="btn btn-primary" @click="openEditor()">{{ t('admin.upstreams.add') }}</button>
          </div>
        </div>
        <p class="mt-3 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.upstreams.pollingHint') }}</p>
        <p v-if="loadError" role="alert" class="mt-2 text-sm text-red-600">{{ loadError }}</p>
      </template>
      <template #table>
        <DataTable :columns="columns" :data="filtered" :loading="loading">
          <template #cell-name="{ row }">
            <div class="font-medium text-gray-900 dark:text-white">{{ row.name }}</div>
            <a v-if="row.website_url" :href="row.website_url" target="_blank" rel="noopener noreferrer" class="text-xs text-primary-600 hover:underline">{{ t('admin.upstreams.website') }}</a>
          </template>
          <template #cell-type="{ row }"><span class="badge badge-gray">{{ typeLabel(row.type) }}</span></template>
          <template #cell-base_url="{ row }"><span class="break-all font-mono text-xs">{{ row.base_url }}</span></template>
          <template #cell-balance="{ row }">
            <div :class="row.balance !== null && row.balance < row.balance_threshold ? 'font-semibold text-red-600 dark:text-red-400' : 'font-medium text-gray-900 dark:text-white'">
              {{ row.balance === null ? '—' : `¥${row.balance.toFixed(2)}` }}
            </div>
            <div v-if="row.type !== 'other'" class="text-xs text-gray-500">{{ t('admin.upstreams.thresholdValue', { value: row.balance_threshold }) }}</div>
          </template>
          <template #cell-status="{ row }">
            <span v-if="row.type === 'other'" class="text-gray-500">{{ t('admin.upstreams.manualOnly') }}</span>
            <span v-else-if="!row.enabled" class="badge badge-gray">{{ t('admin.upstreams.paused') }}</span>
            <span v-else-if="row.last_error" class="text-red-600 dark:text-red-400">{{ t('admin.upstreams.queryFailed') }}</span>
            <span v-else-if="row.balance === null" class="text-gray-500">{{ t('admin.upstreams.pending') }}</span>
            <span v-else-if="row.balance < row.balance_threshold" class="text-red-600 dark:text-red-400">{{ t('admin.upstreams.lowBalance') }}</span>
            <span v-else class="text-green-600 dark:text-green-400">{{ t('admin.upstreams.normal') }}</span>
            <div v-if="row.balance_checked_at" class="mt-1 text-xs text-gray-500">{{ dateTime(row.balance_checked_at) }}</div>
            <p v-if="row.last_error" class="mt-1 w-48 whitespace-normal break-words text-xs text-red-600 dark:text-red-400">{{ row.last_error }}</p>
          </template>
          <template #cell-actions="{ row }">
            <div class="flex gap-2 whitespace-nowrap">
              <button v-if="row.type !== 'other'" class="btn btn-secondary btn-sm" :disabled="!row.enabled || refreshing.has(row.id)" @click="refreshBalance(row)">{{ t('admin.upstreams.refreshBalance') }}</button>
              <button class="btn btn-secondary btn-sm" @click="openEditor(row)">{{ t('common.edit') }}</button>
              <button class="btn btn-secondary btn-sm text-red-600" @click="deleting = row">{{ t('common.delete') }}</button>
            </div>
          </template>
        </DataTable>
      </template>
    </TablePageLayout>

    <BaseDialog :show="editorOpen" :title="t(editing ? 'admin.upstreams.edit' : 'admin.upstreams.add')" width="wide" :close-on-escape="!saving" :close-on-click-outside="!saving" :show-close-button="!saving" @close="closeEditor">
      <form id="upstream-form" class="space-y-4" @submit.prevent="save">
        <div class="grid gap-4 sm:grid-cols-2">
          <div><label for="upstream-name" class="input-label">{{ t('common.name') }}</label><input id="upstream-name" v-model="form.name" class="input" required maxlength="100" /></div>
          <div><label for="upstream-type" class="input-label">{{ t('admin.upstreams.type') }}</label><select id="upstream-type" v-model="form.type" class="input"><option v-for="type in types" :key="type" :value="type">{{ typeLabel(type) }}</option></select></div>
        </div>
        <div><label for="upstream-website" class="input-label">{{ t(form.type === 'newapi' ? 'admin.upstreams.managementUrl' : 'admin.upstreams.websiteUrl') }}</label><input id="upstream-website" v-model="form.website_url" class="input" type="url" placeholder="https://example.com" /><p v-if="form.type === 'newapi'" class="input-hint">{{ t('admin.upstreams.managementUrlHint') }}</p></div>
        <div><label for="upstream-base-url" class="input-label">Base URL</label><input id="upstream-base-url" v-model="form.base_url" class="input" type="url" required placeholder="https://api.example.com" /></div>
        <template v-if="form.type === 'sub2api'">
          <div><label for="upstream-balance-key" class="input-label">{{ t('admin.upstreams.balanceKey') }}</label><input id="upstream-balance-key" v-model="balanceKey" class="input" type="password" autocomplete="new-password" :required="!editing?.has_balance_api_key || editing.type !== form.type" :placeholder="secretPlaceholder" /><p class="input-hint">{{ t('admin.upstreams.balanceKeyHint') }}</p></div>
        </template>
        <template v-if="form.type === 'newapi'">
          <div><label for="upstream-user-id" class="input-label">{{ t('admin.upstreams.userId') }}</label><input id="upstream-user-id" v-model.number="form.user_id" class="input" type="number" min="1" step="1" required /></div>
          <div><label for="upstream-access-token" class="input-label">{{ t('admin.upstreams.accessToken') }}</label><input id="upstream-access-token" v-model="accessToken" class="input" type="password" autocomplete="new-password" :required="!editing?.has_access_token || editing.type !== form.type" :placeholder="secretPlaceholder" /><p class="input-hint">{{ t('admin.upstreams.accessTokenHint') }}</p></div>
        </template>
        <div v-if="form.type !== 'other'" class="space-y-4 rounded-xl bg-gray-50 p-4 dark:bg-dark-700">
          <div class="grid gap-4 sm:grid-cols-2">
            <div><label for="upstream-threshold" class="input-label">{{ t('admin.upstreams.threshold') }}</label><input id="upstream-threshold" v-model.number="form.balance_threshold" class="input" type="number" min="0" max="10000000000" step="0.01" required /></div>
            <div><label for="upstream-interval" class="input-label">{{ t('admin.upstreams.interval') }}</label><input id="upstream-interval" v-model.number="form.check_interval_seconds" class="input" type="number" min="60" max="86400" step="1" required /></div>
            <div><label for="upstream-multiplier" class="input-label">{{ t('admin.upstreams.multiplier') }}</label><input id="upstream-multiplier" v-model.number="form.balance_multiplier" class="input" type="number" min="0.00000001" step="any" required /></div>
            <div v-if="form.type === 'newapi'"><label for="upstream-quota-unit" class="input-label">{{ t('admin.upstreams.quotaUnit') }}</label><input id="upstream-quota-unit" v-model.number="form.quota_per_unit" class="input" type="number" min="0.00000001" step="any" required /></div>
          </div>
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.upstreams.conversionHint') }}</p>
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.upstreams.alertHint') }}</p>
        </div>
        <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300"><input v-model="form.enabled" type="checkbox" class="rounded" />{{ t('admin.upstreams.enabled') }}</label>
      </form>
      <template #footer><div class="flex justify-end gap-3"><button class="btn btn-secondary" :disabled="saving" @click="closeEditor">{{ t('common.cancel') }}</button><button form="upstream-form" type="submit" class="btn btn-primary" :disabled="saving">{{ t(saving ? 'common.submitting' : 'common.save') }}</button></div></template>
    </BaseDialog>
    <ConfirmDialog :show="!!deleting" :title="t('admin.upstreams.delete')" :message="t('admin.upstreams.deleteConfirm', { name: deleting?.name })" :confirm-text="t('common.delete')" :danger="true" :loading="deleteLoading" @confirm="remove" @cancel="deleting = null" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { Upstream, UpstreamInput, UpstreamType } from '@/api/admin/upstreams'
import { useAppStore } from '@/stores/app'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import type { Column } from '@/components/common/types'

const { t } = useI18n()
const app = useAppStore()
const types: UpstreamType[] = ['sub2api', 'newapi', 'other']
const items = ref<Upstream[]>([])
const search = ref('')
const typeFilter = ref('')
const loading = ref(false)
const loadError = ref('')
const editorOpen = ref(false)
const editing = ref<Upstream | null>(null)
const saving = ref(false)
const deleting = ref<Upstream | null>(null)
const deleteLoading = ref(false)
const refreshing = reactive(new Set<number>())
const balanceKey = ref('')
const accessToken = ref('')
const defaults = () => ({ name: '', type: 'sub2api' as UpstreamType, website_url: '', base_url: '', user_id: 0, balance_threshold: 10, balance_multiplier: 1, quota_per_unit: 500000, check_interval_seconds: 300, enabled: true })
const form = reactive(defaults())
const secretPlaceholder = computed(() => editing.value?.type === form.type ? t('admin.upstreams.keepSecret') : '')
const filtered = computed(() => items.value.filter(u => (!typeFilter.value || u.type === typeFilter.value) && `${u.name} ${u.base_url}`.toLowerCase().includes(search.value.toLowerCase())))
const columns = computed<Column[]>(() => [
  { key: 'name', label: t('common.name') }, { key: 'type', label: t('admin.upstreams.type') },
  { key: 'base_url', label: 'Base URL' }, { key: 'balance', label: t('admin.upstreams.balance') },
  { key: 'status', label: t('common.status'), class: 'min-w-[13rem]' }, { key: 'actions', label: t('common.actions') }
])
const typeLabel = (type: string) => type === 'other' ? t('admin.upstreams.other') : type
const dateTime = (date: string) => new Date(date).toLocaleString()
function errorMessage(error: unknown) {
  return (error as { response?: { data?: { message?: string } } })?.response?.data?.message || t('admin.upstreams.failed')
}
let listInFlight = false
let disposed = false
async function load(showLoading = false) {
  if (listInFlight) return
  listInFlight = true
  if (showLoading) loading.value = true
  try { const result = await adminAPI.upstreams.list(); if (!disposed) { items.value = result; loadError.value = '' } }
  catch (error) { if (!disposed) loadError.value = errorMessage(error) }
  finally { loading.value = false; listInFlight = false }
}
function openEditor(row?: Upstream) {
  editing.value = row || null
  Object.assign(form, defaults(), row ? Object.fromEntries(Object.keys(defaults()).map(key => [key, row[key as keyof Upstream]])) : {})
  balanceKey.value = ''; accessToken.value = ''; editorOpen.value = true
}
function closeEditor() { if (saving.value) return; editorOpen.value = false; balanceKey.value = ''; accessToken.value = '' }
watch(() => form.type, () => { balanceKey.value = ''; accessToken.value = '' })
async function save() {
  saving.value = true
  const input: UpstreamInput = { ...form, name: form.name.trim(), base_url: form.base_url.trim(), website_url: form.website_url.trim() }
  if (balanceKey.value.trim()) input.balance_api_key = balanceKey.value.trim()
  if (accessToken.value.trim()) input.access_token = accessToken.value.trim()
  try {
    const row = editing.value ? await adminAPI.upstreams.update(editing.value.id, input) : await adminAPI.upstreams.create(input)
    items.value = [row, ...items.value.filter(u => u.id !== row.id)]
    app.showSuccess(t('common.saved')); saving.value = false; closeEditor()
    if (row.enabled && row.type !== 'other') await refreshBalance(row)
  } catch (error) { app.showError(errorMessage(error)) }
  finally { saving.value = false }
}
async function refreshBalance(row: Upstream) {
  if (refreshing.has(row.id)) return
  refreshing.add(row.id)
  try {
    const updated = await adminAPI.upstreams.refresh(row.id)
    items.value = items.value.map(u => u.id === row.id ? updated : u)
    if (updated.last_error) app.showError(updated.last_error)
  } catch (error) { app.showError(errorMessage(error)) }
  finally { refreshing.delete(row.id) }
}
async function remove() {
  if (!deleting.value || deleteLoading.value) return
  deleteLoading.value = true
  try { await adminAPI.upstreams.remove(deleting.value.id); items.value = items.value.filter(u => u.id !== deleting.value?.id); deleting.value = null; app.showSuccess(t('common.deleted')) }
  catch (error) { app.showError(errorMessage(error)) }
  finally { deleteLoading.value = false }
}
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => { void load(true); timer = setInterval(() => { if (!document.hidden) void load() }, 30000) })
onUnmounted(() => { disposed = true; if (timer) clearInterval(timer) })
</script>
