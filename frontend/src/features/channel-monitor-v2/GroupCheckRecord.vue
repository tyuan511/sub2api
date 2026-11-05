<template>
  <article class="rounded-xl bg-gray-50 p-3 dark:bg-dark-900/70">
    <div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
      <strong class="break-all text-gray-900 dark:text-white">{{ record.model }}</strong>
      <span class="text-gray-500">{{ t(`channelMonitorV2.groupCheck.${record.kind}`) }}</span>
      <span class="text-gray-500">{{ t(record.is_mine ? 'channelMonitorV2.groupCheck.mine' : 'channelMonitorV2.groupCheck.others') }}</span>
      <span :class="record.status === 'failed' ? 'text-red-600 dark:text-red-400' : 'text-emerald-600 dark:text-emerald-400'">{{ statusLabel }}</span>
    </div>
    <div class="mt-1 flex flex-wrap gap-x-3 text-xs text-gray-500 dark:text-gray-400">
      <time :datetime="record.created_at">{{ formatTime(record.created_at) }}</time>
      <span v-if="record.duration_ms != null">{{ t('channelMonitorV2.groupCheck.duration', { value: `${(record.duration_ms / 1000).toFixed(1)}s` }) }}</span>
      <span v-if="record.is_mine && record.actual_cost != null">{{ t('channelMonitorV2.groupCheck.cost', { value: `$${record.actual_cost.toFixed(6)}` }) }}</span>
    </div>
    <p v-if="record.error_message && record.is_mine" class="mt-2 break-words text-xs text-red-600 dark:text-red-400">{{ record.error_message }}</p>
    <div v-if="record.svg" class="mt-3 overflow-hidden rounded-lg bg-white p-2">
      <img v-if="preview" :src="preview" :alt="t('channelMonitorV2.groupCheck.pelican')" class="mx-auto max-h-80 max-w-full" />
      <p v-else class="text-xs text-gray-500">{{ t('channelMonitorV2.groupCheck.invalidSvg') }}</p>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { GroupCheckRecord } from '@/api/channelMonitorV2'
import { groupCheckSvgUrl } from './groupCheckSvg'

const props = defineProps<{ record: GroupCheckRecord }>()
const { t } = useI18n()
const preview = computed(() => groupCheckSvgUrl(props.record.svg))
const statusLabel = computed(() => t(`channelMonitorV2.groupCheck.${props.record.status === 'pending' || props.record.status === 'running' ? 'running' : props.record.status === 'succeeded' ? 'success' : props.record.status}`))
function formatTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}
</script>
