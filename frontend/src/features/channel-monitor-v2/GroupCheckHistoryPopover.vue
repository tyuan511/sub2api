<template>
  <button ref="trigger" type="button" class="btn btn-secondary !px-3 !py-1.5 text-xs" :aria-expanded="open" aria-haspopup="dialog" @click="toggle">
    {{ t('channelMonitorV2.groupCheck.history') }}
  </button>
  <Teleport to="body">
    <section v-if="open" ref="panel" role="dialog" :aria-label="t('channelMonitorV2.groupCheck.history')" class="fixed z-[9999] flex max-h-[min(32rem,calc(100dvh-1rem))] w-[min(24rem,calc(100vw-1rem))] flex-col overflow-hidden rounded-xl border border-gray-200 bg-white shadow-xl dark:border-dark-700 dark:bg-dark-800" :style="position">
      <header class="flex items-center justify-between gap-2 border-b border-gray-100 p-3 dark:border-dark-700">
        <h4 class="font-semibold text-gray-900 dark:text-white">{{ t('channelMonitorV2.groupCheck.history') }}</h4>
        <div class="flex gap-2">
          <button type="button" class="btn btn-secondary !px-2 !py-1 text-xs" @click="load">{{ t('channelMonitorV2.groupCheck.refresh') }}</button>
          <button type="button" class="btn btn-secondary !px-2 !py-1 text-xs" :aria-label="t('channelMonitorV2.groupCheck.close')" @click="close(true)">×</button>
        </div>
      </header>
      <div class="space-y-3 overflow-y-auto p-3">
        <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
        <p v-if="loading && !visibleRecords.length" class="text-sm text-gray-500">{{ t('channelMonitorV2.groupCheck.loading') }}</p>
        <p v-else-if="!visibleRecords.length && !error" class="text-sm text-gray-500">{{ t('channelMonitorV2.groupCheck.empty') }}</p>
        <GroupCheckRecord v-for="record in visibleRecords" :key="record.id" :record="record" />
      </div>
    </section>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getGroupChecks, type GroupCheckRecord as CheckRecord } from '@/api/channelMonitorV2'
import { extractApiErrorMessage } from '@/utils/apiError'
import GroupCheckRecord from './GroupCheckRecord.vue'

const props = defineProps<{ groupId: number }>()
const { t } = useI18n()
const trigger = ref<HTMLElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const open = ref(false)
const records = ref<CheckRecord[]>([])
const loading = ref(false)
const error = ref('')
const position = ref({ top: '8px', left: '8px' })
const POLL_INTERVAL_MS = 5000
const BILLING_RETRY_WINDOW_MS = 60_000
const visibleRecords = computed(() => records.value.filter(record => record.is_mine || record.status === 'succeeded' || record.status === 'failed'))
let sequence = 0
let timer: ReturnType<typeof setInterval> | undefined
const billingRetryUntil = new Map<number, number>()

function updatePosition() {
  if (!trigger.value || !panel.value) return
  const rect = trigger.value.getBoundingClientRect()
  const width = panel.value.offsetWidth
  const height = panel.value.offsetHeight
  const margin = 8
  const top = rect.bottom + margin + height <= window.innerHeight - margin ? rect.bottom + margin : Math.max(margin, rect.top - height - margin)
  const left = Math.max(margin, Math.min(rect.left + rect.width / 2 - width / 2, window.innerWidth - margin - width))
  position.value = { top: `${Math.round(top)}px`, left: `${Math.round(left)}px` }
}

function shouldPollRecords() {
  const now = Date.now()
  return records.value.some(record => {
    if (record.status === 'pending' || record.status === 'running') return true
    if (!record.is_mine || record.actual_cost != null) return false
    const retryUntil = billingRetryUntil.get(record.id)
    return retryUntil != null && now < retryUntil
  })
}
function schedulePolling() {
  clearInterval(timer)
  timer = shouldPollRecords() ? setInterval(() => void load(), POLL_INTERVAL_MS) : undefined
}
async function load() {
  if (!open.value) return
  const id = ++sequence
  const groupId = props.groupId
  loading.value = true
  try {
    const result = await getGroupChecks(groupId)
    if (id !== sequence || groupId !== props.groupId) return
    records.value = result
    for (const record of result) {
      if (record.is_mine && ['succeeded', 'failed'].includes(record.status) && record.actual_cost == null) {
        if (!billingRetryUntil.has(record.id)) billingRetryUntil.set(record.id, Date.now() + BILLING_RETRY_WINDOW_MS)
      } else if (record.actual_cost != null || record.status === 'pending' || record.status === 'running') {
        billingRetryUntil.delete(record.id)
      }
    }
    error.value = ''
  } catch (err) {
    if (id === sequence) error.value = extractApiErrorMessage(err, t('channelMonitorV2.groupCheck.loadFailed'))
  } finally {
    if (id === sequence) {
      loading.value = false
      await nextTick()
      if (id !== sequence || !open.value || groupId !== props.groupId) return
      updatePosition()
      schedulePolling()
    }
  }
}
function outside(event: PointerEvent) {
  const target = event.target as Node
  if (!trigger.value?.contains(target) && !panel.value?.contains(target)) close(false)
}
function keydown(event: KeyboardEvent) {
  if (event.key === 'Escape') { event.preventDefault(); close(true) }
}
function close(restoreFocus = false) {
  open.value = false
  sequence++
  clearInterval(timer)
  document.removeEventListener('pointerdown', outside)
  document.removeEventListener('keydown', keydown)
  window.removeEventListener('scroll', updatePosition, true)
  window.removeEventListener('resize', updatePosition)
  if (restoreFocus) trigger.value?.focus()
}
async function toggle() {
  if (open.value) { close(); return }
  open.value = true
  records.value = []
  error.value = ''
  billingRetryUntil.clear()
  document.addEventListener('pointerdown', outside)
  document.addEventListener('keydown', keydown)
  window.addEventListener('scroll', updatePosition, true)
  window.addEventListener('resize', updatePosition)
  await nextTick()
  if (!open.value) return
  updatePosition()
  void load()
}
onBeforeUnmount(() => close())
</script>
