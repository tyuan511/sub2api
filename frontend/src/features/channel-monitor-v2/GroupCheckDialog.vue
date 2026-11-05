<template>
  <BaseDialog :show="show" :title="t('channelMonitorV2.groupCheck.title', { group: groupName })" width="wide" @close="emit('close')">
    <div class="space-y-5">
      <form class="space-y-3" @submit.prevent="start">
        <label class="block text-sm font-medium text-gray-700 dark:text-gray-200">
          {{ t('channelMonitorV2.groupCheck.model') }}
          <input v-model.trim="model" class="input mt-1 w-full" :placeholder="t('channelMonitorV2.groupCheck.modelPlaceholder')" required maxlength="200" />
        </label>
        <fieldset>
          <legend class="mb-1 text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('channelMonitorV2.groupCheck.type') }}</legend>
          <div class="flex flex-wrap gap-4 text-sm">
            <label class="flex items-center gap-2"><input v-model="type" type="radio" value="connectivity" />{{ t('channelMonitorV2.groupCheck.connectivity') }}</label>
            <label class="flex items-center gap-2"><input v-model="type" type="radio" value="pelican" />{{ t('channelMonitorV2.groupCheck.pelican') }}</label>
          </div>
        </fieldset>
        <div class="flex flex-wrap items-center justify-between gap-3">
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('channelMonitorV2.groupCheck.billing') }}</p>
          <button class="btn btn-primary" type="submit" :disabled="starting || !model.trim()">{{ starting ? t('channelMonitorV2.groupCheck.running') : t('channelMonitorV2.groupCheck.start') }}</button>
        </div>
        <p v-if="startError" class="text-sm text-red-600 dark:text-red-400" role="alert">{{ startError }}</p>
      </form>
      <GroupCheckRecord v-if="current" :record="current" />
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { getGroupChecks, startGroupCheck, type GroupCheckRecord as CheckRecord, type GroupCheckType } from '@/api/channelMonitorV2'
import { extractApiErrorMessage } from '@/utils/apiError'
import GroupCheckRecord from './GroupCheckRecord.vue'

const props = defineProps<{ show: boolean; groupId: number; groupName: string; initialModel?: string }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const model = ref('')
const type = ref<GroupCheckType>('connectivity')
const current = ref<CheckRecord | null>(null)
const starting = ref(false)
const startError = ref('')
const POLL_INTERVAL_MS = 5000
const BILLING_RETRY_WINDOW_MS = 60_000
let generation = 0
let timer: ReturnType<typeof setTimeout> | undefined

async function poll(id: number, groupId: number, token: number, billingRetryUntil?: number) {
  let retryUntil = billingRetryUntil
  try {
    const records = await getGroupChecks(groupId)
    if (token !== generation) return
    const record = records.find(item => item.id === id && item.is_mine)
    if (record) {
      current.value = record
      if (shouldAwaitBilling(record) && retryUntil == null) retryUntil = Date.now() + BILLING_RETRY_WINDOW_MS
    }
  } catch {
    // Keep the last known result and retry while it is running or awaiting billing.
  }
  if (token !== generation || !current.value) return
  const isRunning = ['pending', 'running'].includes(current.value.status)
  const isAwaitingBilling = current.value.is_mine
    && ['succeeded', 'failed'].includes(current.value.status)
    && current.value.actual_cost == null
    && retryUntil != null
    && Date.now() < retryUntil
  if (isRunning || isAwaitingBilling) {
    timer = setTimeout(() => void poll(id, groupId, token, retryUntil), POLL_INTERVAL_MS)
  }
}
function shouldAwaitBilling(record: CheckRecord) {
  return record.is_mine && ['succeeded', 'failed'].includes(record.status) && record.actual_cost == null
}
async function start() {
  if (starting.value || !model.value.trim()) return
  const token = ++generation
  const groupId = props.groupId
  clearTimeout(timer)
  current.value = null
  starting.value = true
  startError.value = ''
  try {
    const result = await startGroupCheck(groupId, type.value, model.value.trim())
    if (token !== generation) return
    current.value = result
    if (['pending', 'running'].includes(result.status)) {
      void poll(result.id, groupId, token)
    } else if (shouldAwaitBilling(result)) {
      void poll(result.id, groupId, token, Date.now() + BILLING_RETRY_WINDOW_MS)
    }
  } catch (error) {
    if (token === generation) startError.value = extractApiErrorMessage(error, t('channelMonitorV2.groupCheck.startFailed'))
  } finally {
    if (token === generation) starting.value = false
  }
}
watch(() => [props.show, props.groupId] as const, () => {
  generation++
  clearTimeout(timer)
  current.value = null
  starting.value = false
  startError.value = ''
  model.value = props.initialModel || ''
}, { immediate: true })
onBeforeUnmount(() => { generation++; clearTimeout(timer) })
</script>
