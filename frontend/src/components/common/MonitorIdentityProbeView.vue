<template>
  <HelpTooltip v-if="result" width-class="w-72">
    <template #trigger>
      <span
        class="inline-flex h-5 w-5 cursor-help items-center justify-center rounded-full text-amber-500 transition-colors hover:bg-amber-50 hover:text-amber-600 dark:text-amber-400 dark:hover:bg-amber-500/15 dark:hover:text-amber-300"
        :aria-label="t('monitorCommon.identityProbe.title')"
        @click.stop
      >
        <Icon name="shield" size="sm" />
      </span>
    </template>
    <div class="space-y-2 text-left">
      <div class="flex flex-wrap items-center gap-2 pr-2">
        <span class="font-semibold">{{ t('monitorCommon.identityProbe.title') }}</span>
        <span class="rounded-full px-1.5 py-0.5 font-medium" :class="identityClass">{{ identityLabel }}</span>
      </div>
      <div class="space-y-1 text-gray-300">
        <div v-if="result.claimed_model">
          {{ t('monitorCommon.identityProbe.claimed', { model: result.claimed_model }) }}
        </div>
        <div v-if="result.confidence != null">
          {{ t('monitorCommon.identityProbe.confidence', { value: formatConfidence(result.confidence) }) }}
        </div>
        <div v-if="probeError" class="break-words text-red-300">{{ probeError }}</div>
        <div v-if="result.checked_at" class="text-gray-400">
          {{ t('monitorCommon.identityProbe.checkedAt', { time: formatCheckedAt(result.checked_at) }) }}
        </div>
      </div>
    </div>
  </HelpTooltip>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { BazaarLinkProbeResult } from '@/api/admin/channelMonitor'
import type { UserMonitorProbeResult } from '@/api/channelMonitor'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{
  result?: BazaarLinkProbeResult | UserMonitorProbeResult | null
}>()
const { t, locale } = useI18n()

const probeError = computed(() => {
  const value = props.result && 'error' in props.result ? props.result.error : undefined
  return typeof value === 'string' && value.trim() ? value : ''
})

const identityLabel = computed(() => {
  if (props.result?.status === 'timed_out') return t('monitorCommon.identityProbe.status.timed_out')
  if (props.result?.status === 'queued' || props.result?.status === 'running') {
    return t('monitorCommon.identityProbe.status.pending')
  }
  if (props.result?.status === 'failed') return t('monitorCommon.identityProbe.status.failed')
  const status = props.result?.identity_status
  if (status === 'confirmed') return t('monitorCommon.identityProbe.status.confirmed')
  if (status === 'mismatch') return t('monitorCommon.identityProbe.status.mismatch')
  if (status === 'insufficient_data') return t('monitorCommon.identityProbe.status.insufficient_data')
  if (!status) return t('monitorCommon.identityProbe.unknown')
  return status
})

const identityClass = computed(() => {
  if (props.result?.status === 'failed' || props.result?.status === 'timed_out') {
    return 'bg-red-100 text-red-700 dark:bg-red-500/15 dark:text-red-300'
  }
  if (props.result?.status === 'queued' || props.result?.status === 'running') {
    return 'bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300'
  }
  switch (props.result?.identity_status) {
    case 'confirmed':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300'
    case 'mismatch':
      return 'bg-red-100 text-red-700 dark:bg-red-500/15 dark:text-red-300'
    default:
      return 'bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300'
  }
})

function formatConfidence(value: number): string {
  return `${Math.round(value * 100)}%`
}

function formatCheckedAt(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(locale.value || undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}
</script>
