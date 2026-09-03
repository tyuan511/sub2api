<template>
  <div class="mt-6 grid grid-cols-1 gap-3 sm:grid-cols-3">
    <div class="relative overflow-hidden rounded-2xl border border-gray-200/80 bg-gray-50/75 p-4 dark:border-dark-700/80 dark:bg-dark-900/45">
      <span aria-hidden="true" class="absolute -bottom-8 -right-8 h-20 w-20 rounded-full bg-emerald-400/10 blur-xl"></span>
      <div class="relative flex items-center justify-between gap-2 text-[10px] font-bold uppercase tracking-[0.14em] text-gray-500 dark:text-gray-400">
        {{ windowLabel }}
        <span class="h-1.5 w-1.5 rounded-full bg-emerald-500"></span>
      </div>
      <div class="relative mt-3 flex items-end gap-1">
        <span
          class="font-mono text-4xl font-bold tabular-nums leading-none tracking-tight"
          :style="colorStyle"
        >
          {{ displayValue }}
        </span>
        <span
          class="pb-0.5 text-sm font-bold leading-none"
          :style="colorStyle"
        >%</span>
      </div>
      <div class="relative mt-4 h-1.5 overflow-hidden rounded-full bg-gray-200/80 dark:bg-dark-700">
        <span
          class="block h-full rounded-full bg-gradient-to-r from-amber-400 via-lime-400 to-emerald-500 transition-[width] duration-500"
          :style="{ width: percentageWidth(value) }"
        ></span>
      </div>
    </div>

    <div class="relative overflow-hidden rounded-2xl border border-gray-200/80 bg-gray-50/75 p-4 dark:border-dark-700/80 dark:bg-dark-900/45">
      <span aria-hidden="true" class="absolute -bottom-8 -right-8 h-20 w-20 rounded-full bg-primary-400/10 blur-xl"></span>
      <div class="relative flex items-center justify-between gap-2 text-[10px] font-bold uppercase tracking-[0.14em] text-gray-500 dark:text-gray-400">
        {{ t('monitorCommon.firstToken') }}
        <span class="h-1.5 w-1.5 rounded-full bg-primary-500"></span>
      </div>
      <div class="relative mt-3 flex items-end gap-1">
        <span
          class="font-mono text-3xl font-bold tabular-nums leading-none tracking-tight"
          :class="firstTokenColorClass"
        >
          {{ firstTokenDisplayValue }}
        </span>
        <span class="pb-0.5 text-xs font-bold leading-none text-gray-400">ms</span>
      </div>
      <div class="relative mt-4 h-1.5 overflow-hidden rounded-full bg-gray-200/80 dark:bg-dark-700">
        <span class="block h-full w-full rounded-full" :class="firstTokenBarClass"></span>
      </div>
    </div>

    <div class="relative overflow-hidden rounded-2xl border border-gray-200/80 bg-gray-50/75 p-4 dark:border-dark-700/80 dark:bg-dark-900/45">
      <span aria-hidden="true" class="absolute -bottom-8 -right-8 h-20 w-20 rounded-full bg-sky-400/10 blur-xl"></span>
      <div class="relative flex items-center justify-between gap-2 text-[10px] font-bold uppercase tracking-[0.14em] text-gray-500 dark:text-gray-400">
        {{ t('monitorCommon.cacheHitRate') }}
        <span class="h-1.5 w-1.5 rounded-full bg-sky-500"></span>
      </div>
      <div class="relative mt-3 flex items-end gap-1">
        <span
          class="font-mono text-4xl font-bold tabular-nums leading-none tracking-tight"
          :style="cacheColorStyle"
        >
          {{ cacheDisplayValue }}
        </span>
        <span
          class="pb-0.5 text-sm font-bold leading-none"
          :style="cacheColorStyle"
        >%</span>
      </div>
      <div class="relative mt-4 h-1.5 overflow-hidden rounded-full bg-gray-200/80 dark:bg-dark-700">
        <span
          class="block h-full rounded-full bg-gradient-to-r from-sky-400 to-primary-500 transition-[width] duration-500"
          :style="{ width: percentageWidth(cacheHitRate) }"
        ></span>
      </div>
    </div>
  </div>
  <div
    v-if="samplesLabel"
    class="mt-2 text-right font-mono text-[10px] uppercase tracking-[0.12em] text-gray-400"
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
