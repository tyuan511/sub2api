<template>
  <div class="mt-5 grid grid-cols-1 gap-4 py-4 sm:grid-cols-3 sm:divide-x sm:divide-gray-100 dark:sm:divide-dark-700/70">
    <div class="min-w-0 sm:pr-3">
      <div class="flex min-w-0 items-center gap-1.5 text-[10px] font-medium uppercase tracking-[0.12em] text-gray-500 dark:text-gray-400">
        <span class="h-1.5 w-1.5 flex-shrink-0 rounded-full bg-emerald-500"></span>
        <span class="truncate" :title="windowLabel">{{ windowLabel }}</span>
      </div>
      <div class="mt-2 flex min-w-0 items-baseline gap-1">
        <span
          class="min-w-0 truncate whitespace-nowrap font-mono text-[clamp(1.15rem,1.7vw,1.5rem)] font-semibold tabular-nums leading-none tracking-tight"
          :style="colorStyle"
        >
          {{ displayValue }}
        </span>
        <span
          class="flex-shrink-0 text-xs font-medium leading-none"
          :style="colorStyle"
        >%</span>
      </div>
      <div class="mt-3 h-1 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
        <span
          class="block h-full rounded-full transition-[width,background-color] duration-500"
          :style="availabilityBarStyle"
        ></span>
      </div>
    </div>

    <div class="min-w-0 sm:px-3">
      <div class="flex min-w-0 items-center gap-1.5 text-[10px] font-medium uppercase tracking-[0.12em] text-gray-500 dark:text-gray-400">
        <span class="h-1.5 w-1.5 flex-shrink-0 rounded-full bg-primary-500"></span>
        {{ t('monitorCommon.firstToken') }}
      </div>
      <div class="mt-2 flex min-w-0 items-baseline gap-1">
        <span
          class="min-w-0 truncate whitespace-nowrap font-mono text-[clamp(1.1rem,1.7vw,1.4rem)] font-semibold tabular-nums leading-none tracking-tight"
          :class="firstTokenColorClass"
        >
          {{ firstTokenDisplayValue }}
        </span>
        <span class="flex-shrink-0 text-xs font-medium leading-none text-gray-400">ms</span>
      </div>
      <div class="mt-3 h-1 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
        <span class="block h-full w-full rounded-full" :class="firstTokenBarClass"></span>
      </div>
    </div>

    <div class="min-w-0 sm:pl-3">
      <div class="flex min-w-0 items-center gap-1.5 text-[10px] font-medium uppercase tracking-[0.12em] text-gray-500 dark:text-gray-400">
        <span class="h-1.5 w-1.5 flex-shrink-0 rounded-full bg-sky-500"></span>
        {{ t('monitorCommon.cacheHitRate') }}
      </div>
      <div class="mt-2 flex min-w-0 items-baseline gap-1">
        <span
          class="min-w-0 truncate whitespace-nowrap font-mono text-[clamp(1.15rem,1.7vw,1.5rem)] font-semibold tabular-nums leading-none tracking-tight"
          :style="cacheColorStyle"
        >
          {{ cacheDisplayValue }}
        </span>
        <span
          class="flex-shrink-0 text-xs font-medium leading-none"
          :style="cacheColorStyle"
        >%</span>
      </div>
      <div class="mt-3 h-1 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
        <span
          class="block h-full rounded-full transition-[width,background-color] duration-500"
          :style="cacheBarStyle"
        ></span>
      </div>
    </div>
  </div>
  <div
    v-if="samplesLabel"
    class="mt-2 text-right text-[10px] text-gray-400"
  >
    {{ samplesLabel }}
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { hslForPct } from '@/composables/useChannelMonitorFormat'
import { firstTokenSeverity, LATENCY_BAR_CLASSES, LATENCY_TEXT_CLASSES } from '@/utils/latencyHealth'

const props = defineProps<{
  windowLabel: string
  value: number | null
  firstTokenMs?: number | null
  cacheHitRate: number | null
  samplesLabel?: string
}>()

const { t } = useI18n()

const displayValue = computed(() => {
  if (props.value === null || Number.isNaN(props.value)) return t('monitorCommon.latencyEmpty')
  return props.value.toFixed(2)
})

const colorStyle = computed(() => {
  const colour = hslForPct(props.value)
  return colour ? { color: colour } : { color: 'rgb(156 163 175)' }
})

const cacheDisplayValue = computed(() => {
  if (props.cacheHitRate === null || Number.isNaN(props.cacheHitRate)) return t('monitorCommon.metricEmpty')
  return props.cacheHitRate.toFixed(2)
})

const cacheColorStyle = computed(() => {
  const colour = hslForPct(props.cacheHitRate)
  return colour ? { color: colour } : { color: 'rgb(156 163 175)' }
})

const availabilityBarStyle = computed(() => ({
  width: percentageWidth(props.value),
  backgroundColor: colorStyle.value.color,
}))

const cacheBarStyle = computed(() => ({
  width: percentageWidth(props.cacheHitRate),
  backgroundColor: cacheColorStyle.value.color,
}))

const firstTokenDisplayValue = computed(() => {
  if (props.firstTokenMs == null || Number.isNaN(props.firstTokenMs)) {
    return t('monitorCommon.latencyEmpty')
  }
  return String(Math.round(props.firstTokenMs))
})

const firstTokenColorClass = computed(() => {
  if (props.firstTokenMs == null || Number.isNaN(props.firstTokenMs)) {
    return 'text-gray-400'
  }
  return LATENCY_TEXT_CLASSES[firstTokenSeverity(props.firstTokenMs)]
})

const firstTokenBarClass = computed(() => {
  if (props.firstTokenMs == null || Number.isNaN(props.firstTokenMs)) {
    return 'bg-gray-300 dark:bg-dark-600'
  }
  return LATENCY_BAR_CLASSES[firstTokenSeverity(props.firstTokenMs)]
})

function percentageWidth(value: number | null): string {
  if (value === null || Number.isNaN(value)) return '0%'
  return `${Math.max(0, Math.min(100, value))}%`
}
</script>
