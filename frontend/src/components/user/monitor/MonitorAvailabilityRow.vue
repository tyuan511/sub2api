<template>
  <div class="mt-3 grid grid-cols-3 divide-x divide-gray-200/80 dark:divide-dark-700/80">
    <div class="min-w-0 pr-2">
      <div class="text-[10px] font-medium leading-tight text-gray-500 dark:text-gray-400">
        {{ windowLabel }}
      </div>
      <div class="mt-1 flex items-baseline gap-0.5">
        <span
          class="text-xl font-semibold tabular-nums leading-none"
          :style="colorStyle"
        >
          {{ displayValue }}
        </span>
        <span
          class="text-base font-semibold leading-none"
          :style="colorStyle"
        >%</span>
      </div>
    </div>

    <div class="min-w-0 px-2">
      <div class="text-[10px] font-medium leading-tight text-gray-500 dark:text-gray-400">
        {{ t('monitorCommon.firstToken') }}
      </div>
      <div class="mt-1 flex items-baseline gap-0.5">
        <span
          class="text-xl font-semibold tabular-nums leading-none"
          :class="firstTokenColorClass"
        >
          {{ firstTokenDisplayValue }}
        </span>
        <span
          class="text-xs font-medium leading-none"
          :class="firstTokenColorClass"
        >ms</span>
      </div>
    </div>

    <div class="min-w-0 pl-2">
      <div class="text-[10px] font-medium leading-tight text-gray-500 dark:text-gray-400">
        {{ t('monitorCommon.cacheHitRate') }}
      </div>
      <div class="mt-1 flex items-baseline gap-0.5">
        <span
          class="text-xl font-semibold tabular-nums leading-none"
          :style="cacheColorStyle"
        >
          {{ cacheDisplayValue }}
        </span>
        <span
          class="text-base font-semibold leading-none"
          :style="cacheColorStyle"
        >%</span>
      </div>
    </div>
  </div>
  <div
    v-if="samplesLabel"
    class="mt-1 text-[11px] text-gray-400 text-right"
  >
    {{ samplesLabel }}
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { hslForPct } from '@/composables/useChannelMonitorFormat'
import { firstTokenSeverity, LATENCY_TEXT_CLASSES } from '@/utils/latencyHealth'

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
    return 'text-gray-900 dark:text-gray-100'
  }
  return LATENCY_TEXT_CLASSES[firstTokenSeverity(props.firstTokenMs)]
})
</script>
