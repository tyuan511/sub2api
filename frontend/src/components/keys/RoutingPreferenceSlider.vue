<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  modelValue: number
  min: number
  max: number
  step: number
  defaultValue: number
  label: string
  valueLabel: string
  description: string
  leftLabel: string
  rightLabel: string
  resetLabel: string
  ticks?: number
}>(), { ticks: 6 })
const emit = defineEmits<{ 'update:modelValue': [value: number] }>()
const progress = computed(() => Math.max(0, Math.min(100,
  (props.modelValue - props.min) / (props.max - props.min) * 100)))
// Match the 20px native thumb's travel while retaining a larger hit area.
const position = (percent: number) => `calc(${percent}% + ${10 - .2 * percent}px)`
const update = (event: Event) => {
  const value = Number((event.target as HTMLInputElement).value)
  emit('update:modelValue', Math.max(props.min, Math.min(props.max,
    props.min + Math.round((value - props.min) / props.step) * props.step)))
}
</script>

<template>
  <section class="routing-slider rounded-2xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800">
    <div class="grid grid-cols-[2rem_minmax(0,1fr)_2rem] items-start gap-2">
      <svg class="mt-1 h-5 w-5 text-gray-400 dark:text-gray-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true">
        <path d="m13 2-9 12h7l-1 8 10-13h-7l1-7Z" stroke-linecap="round" stroke-linejoin="round" />
      </svg>
      <div class="min-w-0 text-center">
        <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ label }}</p>
        <p class="mt-1 text-lg font-semibold tabular-nums tracking-tight text-primary-500 dark:text-primary-400" data-test="slider-value">{{ valueLabel }}</p>
        <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ description }}</p>
      </div>
      <button
        type="button" :aria-label="resetLabel" :title="resetLabel"
        class="flex h-8 w-8 items-center justify-center rounded-full text-gray-400 transition hover:bg-gray-100 hover:text-gray-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:hover:bg-dark-700 dark:hover:text-gray-200"
        @click="emit('update:modelValue', defaultValue)"
      >
        <svg class="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" aria-hidden="true">
          <path d="M3 10a9 9 0 1 1 2 8M3 4v6h6" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </button>
    </div>
    <div class="relative mt-4 h-5" data-test="slider-track">
      <input
        type="range" class="slider-input peer absolute inset-x-0 -top-2.5 z-10 m-0 h-10 w-full cursor-pointer opacity-0"
        :min="min" :max="max" :step="step" :value="modelValue"
        :aria-label="label" :aria-valuetext="`${valueLabel}. ${description}`"
        @input="update"
      />
      <div class="pointer-events-none absolute inset-0 overflow-hidden rounded-full border border-gray-200 bg-gray-100 peer-focus-visible:ring-2 peer-focus-visible:ring-primary-500 peer-focus-visible:ring-offset-4 dark:border-dark-600 dark:bg-dark-700 dark:peer-focus-visible:ring-offset-dark-800" aria-hidden="true">
        <div class="absolute inset-y-0 left-0 rounded-l-full bg-primary-500" :style="{ width: position(progress) }" />
        <span
          v-for="tick in ticks" :key="tick"
          class="absolute top-1/2 h-1 w-1 -translate-x-1/2 -translate-y-1/2 rounded-full"
          :class="(tick - 1) / (ticks - 1) * 100 <= progress ? 'bg-white/45' : 'bg-gray-400/55 dark:bg-gray-500'"
          :style="{ left: position((tick - 1) / (ticks - 1) * 100) }"
        />
      </div>
      <div class="pointer-events-none absolute top-1/2 h-5 w-5 -translate-x-1/2 -translate-y-1/2 rounded-full border border-gray-200 bg-white shadow-sm dark:border-gray-300" :style="{ left: position(progress) }" data-test="slider-thumb" aria-hidden="true" />
    </div>
    <div class="mt-2 flex items-center justify-between gap-3 text-xs text-gray-500 dark:text-gray-400">
      <span>{{ leftLabel }}</span><span class="text-right">{{ rightLabel }}</span>
    </div>
    <slot />
  </section>
</template>

<style scoped>
.routing-slider { border-radius: 20px; }
.slider-input { appearance: none; -webkit-appearance: none; background: transparent; }
.slider-input::-webkit-slider-thumb { appearance: none; -webkit-appearance: none; width: 20px; height: 20px; }
.slider-input::-moz-range-thumb { width: 20px; height: 20px; border: 0; }
</style>
