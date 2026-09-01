<template>
  <div class="relative mt-5 pt-3 border-t border-gray-100 dark:border-dark-700/60">
    <div
      class="flex justify-between text-[10px] font-semibold uppercase tracking-[0.1em] text-gray-400 mb-2"
    >
      <span>{{ t('monitorCommon.history60pts', { n: length }) }}</span>
      <span class="tabular-nums">{{ t('monitorCommon.nextUpdateIn', { n: countdownSeconds }) }}</span>
    </div>

    <div
      v-if="maintenance"
      class="flex h-5 w-full items-center justify-center rounded border border-dashed border-gray-300 dark:border-dark-600 text-[10px] uppercase tracking-widest text-gray-400"
    >
      {{ t('monitorCommon.maintenancePaused') }}
    </div>
    <div v-else class="relative flex items-end gap-1 h-8 w-full">
      <HelpTooltip v-for="(bar, idx) in displayBars" :key="idx" class="!ml-0 h-8 min-w-0 flex-1 items-end" width-class="w-56">
        <template #trigger>
          <div class="h-8 w-full rounded-md transition-opacity group-hover:opacity-90" :class="bar.colorClass" :style="{ height: bar.heightPct + '%' }" :title="bar.title"></div>
        </template>
        <template v-if="bar.point">
          <div class="mb-1 flex items-center justify-between gap-2 font-semibold">
            <span>{{ formatCheckedAt(bar.point.checked_at) }}</span>
            <span>{{ statusLabel(bar.point.status) }}</span>
          </div>
          <div class="grid grid-cols-3 gap-2 text-[11px] text-gray-300">
            <span>{{ t('monitorCommon.availabilityPrefix') }} {{ probeAvailability(bar.point) }}%</span>
            <span>{{ t('monitorCommon.firstToken') }} {{ formatFirstTokenSeconds(bar.point.first_token_ms) }}s</span>
            <span>{{ t('monitorCommon.cacheHitRate') }} {{ cacheHitRate == null ? '-' : `${cacheHitRate.toFixed(1)}%` }}</span>
          </div>
        </template>
      </HelpTooltip>
    </div>

    <div
      class="mt-1 flex justify-between text-[9px] uppercase tracking-[0.1em] text-gray-400"
    >
      <span>{{ t('monitorCommon.past') }}</span>
      <span>{{ t('monitorCommon.now') }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MonitorTimelinePoint } from '@/api/channelMonitor'
import { useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'
import { firstTokenSeverity, LATENCY_BAR_CLASSES } from '@/utils/latencyHealth'
import HelpTooltip from '@/components/common/HelpTooltip.vue'

const props = withDefaults(defineProps<{
  buckets?: MonitorTimelinePoint[]
  countdownSeconds: number
  cacheHitRate?: number | null
  length?: number
  maintenance?: boolean
}>(), {
  buckets: () => [],
  length: 18,
  maintenance: false,
  cacheHitRate: null,
})

const { t } = useI18n()
const { statusLabel, formatRelativeTime } = useChannelMonitorFormat()

interface Bar {
  colorClass: string
  heightPct: number
  title: string
  point?: MonitorTimelinePoint
}

function probeAvailability(point: MonitorTimelinePoint): string {
  return point.status === 'operational' || point.status === 'degraded' ? '100.0' : '0.0'
}

function formatCheckedAt(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(undefined, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(date)
}

function formatFirstTokenSeconds(ms: number | null | undefined): string {
  if (ms == null) return t('monitorCommon.latencyEmpty')
  return (ms / 1000).toFixed(1)
}
// 4 级高度 + 颜色双重编码：高=好+绿，短=坏+红，灰=未测试。
// 长绿(正常) > 中黄(降级) > 短红(失败/系统错误) > 很短灰(未测试)。
const STATUS_HEIGHT: Record<string, number> = {
  operational: 100,
  degraded: 65,
  failed: 35,
  error: 35,
  empty: 15,
}

const STATUS_COLOR: Record<string, string> = {
  operational: 'bg-emerald-500',
  degraded: 'bg-amber-500',
  failed: 'bg-red-500',
  error: 'bg-red-500',
  empty: 'bg-gray-300 dark:bg-dark-600',
}

const displayBars = computed<Bar[]>(() => {
  // Real points come newest-first; convert to oldest-first so the rightmost
  // bar represents "now". Pad the left with empty placeholders to keep the
  // bar count stable at `length`.
  const real = [...(props.buckets ?? [])]
    .slice(0, props.length)
    .reverse()

  const padCount = Math.max(0, props.length - real.length)
  const bars: Bar[] = []

  for (let i = 0; i < padCount; i += 1) {
    bars.push({
      colorClass: STATUS_COLOR.empty,
      heightPct: STATUS_HEIGHT.empty,
      title: '',
    })
  }

  for (const point of real) {
    const status = point.status as keyof typeof STATUS_HEIGHT
    // Responses probes carry TTFT; use the same latency health bands as usage
    // records. Legacy/non-streaming points retain their status color.
    const colorClass = point.first_token_ms != null
      ? LATENCY_BAR_CLASSES[firstTokenSeverity(point.first_token_ms)]
      : STATUS_COLOR[status] ?? STATUS_COLOR.empty
    const heightPct = STATUS_HEIGHT[status] ?? STATUS_HEIGHT.empty
    const firstToken = formatFirstTokenSeconds(point.first_token_ms)
    const relative = formatRelativeTime(point.checked_at)
    const label = statusLabel(point.status)
    bars.push({
      colorClass,
      heightPct,
      point,
      title: point.first_token_ms != null
        ? `${relative} · ${label} · ${t('monitorCommon.firstToken')} ${firstToken}s`
        : `${relative} · ${label}`,
    })
  }

  return bars
})
</script>
