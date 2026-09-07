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
      <div class="flex items-baseline gap-1.5 pr-2">
        <span class="font-semibold">{{ t('monitorCommon.identityProbe.title') }}</span>
        <span class="text-[11px] font-medium leading-none" :class="identityClass" data-testid="identity-status">
          {{ identityLabel }}
        </span>
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
        <a
          v-if="reportUrl"
          :href="reportUrl"
          target="_blank"
          rel="noopener noreferrer"
          class="inline-flex text-amber-300 underline-offset-2 hover:underline"
          @click.stop
        >
          {{ t('monitorCommon.identityProbe.viewReport') }}
        </a>
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

const reportUrl = computed(() => {
  const result = props.result
  if (!result) return ''
  if ('report_url' in result && typeof result.report_url === 'string' && result.report_url.trim()) {
    return result.report_url.trim()
  }
  if ('run_id' in result && typeof result.run_id === 'string' && result.run_id.trim()) {
    return `https://bazaarlink.ai/probe?runId=${encodeURIComponent(result.run_id.trim())}`
  }
  return ''
})

type IdentityKind = 'timed_out' | 'pending' | 'failed' | 'confirmed' | 'mismatch' | 'insufficient_data' | 'unknown'

function normalizeIdentityStatus(status?: string): string {
  switch ((status || '').trim().toLowerCase()) {
    case 'confirmed':
    case 'match':
    case 'matched':
      return 'confirmed'
    case 'mismatch':
    case 'no_match':
      return 'mismatch'
    case 'insufficient_data':
      return 'insufficient_data'
    default:
      return (status || '').trim()
  }
}

const identityKind = computed<IdentityKind | string>(() => {
  if (props.result?.status === 'timed_out') return 'timed_out'
  if (props.result?.status === 'queued' || props.result?.status === 'running') return 'pending'
  if (props.result?.status === 'failed') return 'failed'
  const status = normalizeIdentityStatus(props.result?.identity_status)
  if (status === 'confirmed' || status === 'mismatch' || status === 'insufficient_data') return status
  if (!status) return 'unknown'
  return status
})

const identityLabel = computed(() => {
  const kind = identityKind.value
  switch (kind) {
    case 'timed_out':
      return t('monitorCommon.identityProbe.status.timed_out')
    case 'pending':
      return t('monitorCommon.identityProbe.status.pending')
    case 'failed':
      return t('monitorCommon.identityProbe.status.failed')
    case 'confirmed':
      return t('monitorCommon.identityProbe.status.confirmed')
    case 'mismatch':
      return t('monitorCommon.identityProbe.status.mismatch')
    case 'insufficient_data':
      return t('monitorCommon.identityProbe.status.insufficient_data')
    case 'unknown':
      return t('monitorCommon.identityProbe.unknown')
    default:
      return kind
  }
})

const identityClass = computed(() => {
  switch (identityKind.value) {
    case 'confirmed':
      return 'text-emerald-400'
    case 'mismatch':
    case 'failed':
    case 'timed_out':
      return 'text-rose-400'
    case 'pending':
    case 'insufficient_data':
      return 'text-amber-400'
    default:
      return 'text-gray-400'
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
