<template>
  <div class="mt-6 border-t border-gray-200/80 pt-5 dark:border-dark-700/80">
    <div
      class="mb-3 flex justify-between gap-3 text-[10px] font-bold uppercase tracking-[0.14em] text-gray-400"
    >
      <span>{{ t('monitorCommon.history60pts', { n: length }) }}</span>
      <span class="font-mono tabular-nums text-gray-500 dark:text-gray-400">{{ t('monitorCommon.nextUpdateIn', { n: countdownSeconds }) }}</span>
    </div>

    <div
      v-if="maintenance"
      class="flex h-9 w-full items-center justify-center rounded-lg border border-dashed border-gray-300 text-[10px] font-semibold uppercase tracking-widest text-gray-400 dark:border-dark-600"
    >
      {{ t('monitorCommon.maintenancePaused') }}
    </div>
    <div v-else class="flex h-9 w-full items-end gap-[3px] overflow-hidden rounded-lg bg-gray-50 px-1.5 py-1 dark:bg-dark-900/45">
      <div
        v-for="(bar, idx) in displayBars"
        :key="idx"
        class="min-w-0 flex-1 rounded-[3px] opacity-90 transition-[height,opacity] duration-300 group-hover:opacity-100"
        :class="bar.colorClass"
        :style="{ height: bar.heightPct + '%' }"
        :title="bar.title"
      ></div>
    </div>

    <div
      class="mt-2 flex justify-between font-mono text-[9px] font-medium uppercase tracking-[0.16em] text-gray-400"
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

const props = withDefaults(defineProps<{
  buckets?: MonitorTimelinePoint[]
  countdownSeconds: number
  length?: number
  maintenance?: boolean
}>(), {
  buckets: () => [],
  length: 60,
  maintenance: false,
})

const { t } = useI18n()
const { statusLabel, formatLatency, formatRelativeTime } = useChannelMonitorFormat()

interface Bar {
  colorClass: string
  heightPct: number
  title: string
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
    const firstToken = formatLatency(point.first_token_ms)
    const relative = formatRelativeTime(point.checked_at)
    const label = statusLabel(point.status)
    bars.push({
      colorClass,
      heightPct,
      title: point.first_token_ms != null
        ? `${relative} · ${label} · ${t('monitorCommon.firstToken')} ${firstToken}ms`
        : `${relative} · ${label}`,
    })
  }

  return bars
})
</script>
