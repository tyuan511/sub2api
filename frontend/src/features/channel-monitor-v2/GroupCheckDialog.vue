<template>
  <BaseDialog :show="show" :title="t('channelMonitorV2.groupCheck.title', { group: groupName })" width="wide" @close="emit('close')">
    <div class="space-y-5">
      <div class="flex gap-1 border-b border-gray-200 dark:border-dark-700" role="tablist" :aria-label="t('channelMonitorV2.groupCheck.tabsAria')">
        <button v-for="item in tabs" :key="item.key" type="button" role="tab" :aria-selected="tab === item.key" class="border-b-2 px-3 py-2 text-sm font-medium" :class="tab === item.key ? 'border-primary-500 text-primary-600' : 'border-transparent text-gray-500'" @click="tab = item.key">
          {{ item.label }}<span v-if="item.key === 'history'" class="ml-1">({{ historyRecords.length }})</span>
        </button>
      </div>

      <form v-if="tab === 'run'" class="space-y-3" @submit.prevent="start">
        <label class="block text-sm font-medium text-gray-700 dark:text-gray-200">
          {{ t('channelMonitorV2.groupCheck.model') }}
          <select v-model="model" class="input mt-1 w-full" required :disabled="starting || Boolean(activeRecord)">
            <option v-for="option in modelOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
          </select>
        </label>
        <fieldset>
          <legend class="mb-1 text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('channelMonitorV2.groupCheck.type') }}</legend>
          <div class="flex flex-wrap gap-4 text-sm">
            <label class="flex items-center gap-2"><input v-model="type" type="radio" value="connectivity" :disabled="starting || Boolean(activeRecord)" />{{ t('channelMonitorV2.groupCheck.connectivity') }}</label>
            <label class="flex items-center gap-2"><input v-model="type" type="radio" value="pelican" :disabled="starting || Boolean(activeRecord)" />{{ t('channelMonitorV2.groupCheck.pelican') }}</label>
          </div>
        </fieldset>
        <div class="flex flex-wrap items-center justify-between gap-3">
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('channelMonitorV2.groupCheck.billing') }}</p>
          <button class="btn btn-primary" type="submit" :disabled="starting || Boolean(activeRecord) || !model">{{ activeRecord ? t('channelMonitorV2.groupCheck.running') : starting ? t('channelMonitorV2.groupCheck.starting') : t('channelMonitorV2.groupCheck.start') }}</button>
        </div>
        <p v-if="activeRecord" class="text-sm text-blue-600 dark:text-blue-300">{{ t('channelMonitorV2.groupCheck.inProgress', { elapsed: elapsedLabel(activeRecord) }) }}</p>
        <p v-if="platformError" class="text-sm text-red-600 dark:text-red-400" role="alert">{{ t('channelMonitorV2.groupCheck.platformUnavailable') }}</p>
        <p v-if="startError" class="text-sm text-red-600 dark:text-red-400" role="alert">{{ startError }}</p>
        <GroupCheckRecord v-if="current" :record="current" />
      </form>

      <div v-else class="space-y-3">
        <div class="flex justify-end"><button type="button" class="btn btn-secondary !px-2 !py-1 text-xs" :disabled="loading" @click="refresh">{{ t('channelMonitorV2.groupCheck.refresh') }}</button></div>
        <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
        <p v-if="loading && !historyRecords.length" class="text-sm text-gray-500">{{ t('channelMonitorV2.groupCheck.loading') }}</p>
        <p v-else-if="!historyRecords.length && !error" class="text-sm text-gray-500">{{ t('channelMonitorV2.groupCheck.empty') }}</p>
        <GroupCheckRecord v-for="record in historyRecords" :key="record.id" :record="record" />
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { getGroupChecks, groupCheckModelOptions, startGroupCheck, type GroupCheckRecord as CheckRecord, type GroupCheckType } from '@/api/channelMonitorV2'
import { userGroupsAPI } from '@/api/groups'
import { extractApiErrorMessage } from '@/utils/apiError'
import GroupCheckRecord from './GroupCheckRecord.vue'

const props = defineProps<{ show: boolean; groupId: number; groupName: string; initialModel?: string }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const tab = ref<'run' | 'history'>('run')
const model = ref('')
const type = ref<GroupCheckType>('connectivity')
const current = ref<CheckRecord | null>(null)
const records = ref<CheckRecord[]>([])
const starting = ref(false)
const loading = ref(false)
const startError = ref('')
const error = ref('')
const now = ref(Date.now())
const platform = ref('')
const platformError = ref(false)
const modelOptions = computed(() => groupCheckModelOptions(platform.value))
const activeRecord = computed(() => records.value.find(record => record.is_mine && ['pending', 'running'].includes(record.status)) || (current.value?.is_mine && ['pending', 'running'].includes(current.value.status) ? current.value : null))
const historyRecords = computed(() => records.value.filter(record => record.is_mine || record.status === 'succeeded' || record.status === 'failed'))
const tabs = computed(() => [{ key: 'run' as const, label: t('channelMonitorV2.groupCheck.runTab') }, { key: 'history' as const, label: t('channelMonitorV2.groupCheck.history') }])
const POLL_INTERVAL_MS = 5000
const BILLING_RETRY_WINDOW_MS = 60_000
let generation = 0
let timer: ReturnType<typeof setTimeout> | undefined
let clock: ReturnType<typeof setInterval> | undefined
const billingRetryUntil = new Map<number, number>()

function clearPolling() { clearTimeout(timer); timer = undefined }
function shouldAwaitBilling(record: CheckRecord) { return record.is_mine && ['succeeded', 'failed'].includes(record.status) && record.actual_cost == null }
function defaultModel() {
  return modelOptions.value.some(option => option.value === props.initialModel) ? props.initialModel! : modelOptions.value[0]?.value || ''
}
function elapsedLabel(record: CheckRecord) {
  const ms = record.duration_ms != null ? record.duration_ms : Math.max(0, now.value - Date.parse(record.created_at))
  return `${(ms / 1000).toFixed(1)}s`
}
function schedulePoll(token: number) {
  clearPolling()
  const record = activeRecord.value || current.value
  const waitingHistoryBilling = records.value.some(item => shouldAwaitBilling(item) && (billingRetryUntil.get(item.id) || 0) > Date.now())
  if (token !== generation) return
  const waitingBilling = record != null && shouldAwaitBilling(record) && (billingRetryUntil.get(record.id) || 0) > Date.now()
  if ((record && ['pending', 'running'].includes(record.status)) || waitingBilling || waitingHistoryBilling) timer = setTimeout(() => void load(token), POLL_INTERVAL_MS)
}
async function load(token = generation) {
  if (!props.show || token !== generation) return
  loading.value = true
  try {
    const result = await getGroupChecks(props.groupId)
    if (token !== generation) return
    records.value = result
    const refreshedCurrent = current.value ? result.find(record => record.id === current.value?.id) : undefined
    if (refreshedCurrent) current.value = refreshedCurrent
    const running = result.find(record => record.is_mine && ['pending', 'running'].includes(record.status))
    if (running) current.value = running
    for (const record of result) {
      if (shouldAwaitBilling(record) && !billingRetryUntil.has(record.id)) billingRetryUntil.set(record.id, Date.now() + BILLING_RETRY_WINDOW_MS)
      if (record.actual_cost != null) billingRetryUntil.delete(record.id)
    }
    error.value = ''
  } catch (err) {
    if (token === generation) error.value = extractApiErrorMessage(err, t('channelMonitorV2.groupCheck.loadFailed'))
  } finally {
    if (token === generation) { loading.value = false; schedulePoll(token) }
  }
}
async function refresh() { await load() }
async function start() {
  if (starting.value || activeRecord.value || !model.value) return
  const token = ++generation
  clearPolling(); loading.value = false; starting.value = true; startError.value = ''
  try {
    const result = await startGroupCheck(props.groupId, type.value, model.value)
    if (token !== generation) return
    current.value = result
    records.value = [result, ...records.value.filter(item => item.id !== result.id)]
    tab.value = 'run'
    if (['pending', 'running'].includes(result.status)) void load(token)
    else if (shouldAwaitBilling(result)) { billingRetryUntil.set(result.id, Date.now() + BILLING_RETRY_WINDOW_MS); void load(token) }
  } catch (err) {
    if (token === generation) startError.value = extractApiErrorMessage(err, t('channelMonitorV2.groupCheck.startFailed'))
  } finally { if (token === generation) starting.value = false }
}
watch(() => [props.show, props.groupId, props.initialModel] as const, ([show]) => {
  generation++; clearPolling(); clearInterval(clock); clock = undefined; billingRetryUntil.clear(); current.value = null; records.value = []; startError.value = ''; error.value = ''; platform.value = ''; platformError.value = false; tab.value = 'run'; model.value = ''
  if (show) {
    const token = generation
    void userGroupsAPI.getAvailable().then(groups => {
      if (token !== generation) return
      platform.value = groups.find(group => group.id === props.groupId)?.platform || ''
      model.value = defaultModel()
      platformError.value = !model.value
    }).catch(() => { if (token === generation) platformError.value = true })
    void load(); clock = setInterval(() => { now.value = Date.now() }, 1000)
  }
}, { immediate: true })
onBeforeUnmount(() => { generation++; clearPolling(); clearInterval(clock) })
</script>
