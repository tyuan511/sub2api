<template>
  <div class="mt-3 grid grid-cols-2 gap-3">
    <div>
      <div class="text-[11px] uppercase tracking-widest text-gray-400">
        {{ windowLabel }}
      </div>
      <div class="mt-1 flex items-baseline gap-0.5">
        <span
          class="text-3xl font-bold tabular-nums leading-none"
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
    <div class="border-l border-gray-100 dark:border-dark-700/60 pl-3">
      <div class="text-[11px] uppercase tracking-widest text-gray-400">
        {{ t('monitorCommon.cacheHitRate') }}
      </div>
      <div class="mt-1 flex items-baseline gap-0.5">
        <span
          class="text-3xl font-bold tabular-nums leading-none"
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

const props = defineProps<{
  windowLabel: string
  value: number | null
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
</script>
