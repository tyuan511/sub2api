<template>
  <section class="rounded-2xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800">
    <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('keys.smartPreferenceLabel') }}</p>
    <div class="mt-2 grid grid-cols-2 gap-1.5">
      <button
        v-for="preset in presets"
        :key="preset.bps"
        type="button"
        class="rounded-lg border px-2.5 py-1.5 text-left transition-colors"
        :class="selectedBps === preset.bps
          ? 'border-primary-500 bg-primary-50 text-primary-800 ring-1 ring-primary-500 dark:bg-primary-900/20 dark:text-primary-200'
          : 'border-gray-200 text-gray-700 hover:border-gray-300 dark:border-dark-600 dark:text-gray-300'"
        :data-test="`smart-balance-preset-${preset.bps}`"
        @click="emit('update:modelValue', preset.bps)"
      >
        <span class="block text-xs font-medium">{{ preset.label }}</span>
        <span class="mt-0.5 block text-[11px] leading-4 opacity-70">{{ preset.hint }}</span>
      </button>
    </div>
    <div class="mt-3 flex w-full flex-col items-center" data-test="routing-score-radar">
      <svg viewBox="0 0 380 195" class="w-72 max-w-full" role="img" :aria-label="selectedPreset.desc">
        <polygon
          v-for="ring in rings"
          :key="ring"
          :points="polygonPoints(ring)"
          class="fill-none stroke-gray-200 dark:stroke-dark-600"
          stroke-width="1"
        />
        <line
          v-for="(_, index) in axes"
          :key="`axis-${index}`"
          :x1="cx"
          :y1="cy"
          :x2="point(index, 1).x"
          :y2="point(index, 1).y"
          class="stroke-gray-200 dark:stroke-dark-600"
          stroke-width="1"
        />
        <polygon
          :points="valuePoints"
          class="fill-primary-500/20 stroke-primary-500"
          stroke-width="2"
          stroke-linejoin="round"
          data-test="routing-score-radar-shape"
        />
        <circle
          v-for="(vertex, index) in valueVertices"
          :key="`dot-${index}`"
          :cx="vertex.x"
          :cy="vertex.y"
          r="3.5"
          class="fill-primary-500"
        />
        <text
          v-for="(axis, index) in axes"
          :key="axis.key"
          :x="labelPoint(index).x"
          :y="labelPoint(index).y"
          :text-anchor="labelAnchor(index)"
          dominant-baseline="middle"
          class="fill-gray-500 dark:fill-gray-400"
          font-size="12"
        >
          {{ axis.label }} {{ axis.value }}%
        </text>
      </svg>
      <p class="mt-1 max-w-sm text-center text-xs leading-5 text-gray-500 dark:text-gray-400">
        {{ selectedPreset.desc }}
      </p>
    </div>
  </section>
</template>

<script lang="ts">
export const SMART_BALANCE_PRESETS = [0, 2500, 5000, 10000] as const

export function snapSmartBalanceBps(bps: number): number {
  if (bps === 1250) return 0
  if (bps === 8750) return 10000
  return SMART_BALANCE_PRESETS.reduce((best, preset) =>
    Math.abs(preset - bps) < Math.abs(best - bps) ? preset : best)
}
</script>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  modelValue: number
}>()

const emit = defineEmits<{
  'update:modelValue': [value: number]
}>()

const { t } = useI18n()
const cx = 190
const cy = 108
const radius = 74
const rings = [0.25, 0.5, 0.75, 1]

const selectedBps = computed(() => snapSmartBalanceBps(props.modelValue))

const presets = computed(() => [
  { bps: 0, label: t('keys.presetLowest'), hint: t('keys.presetLowestHint'), desc: t('keys.presetLowestDesc') },
  { bps: 2500, label: t('keys.presetThrifty'), hint: t('keys.presetThriftyHint'), desc: t('keys.presetThriftyDesc') },
  { bps: 5000, label: t('keys.presetBalanced'), hint: t('keys.presetBalancedHint'), desc: t('keys.presetBalancedDesc') },
  { bps: 10000, label: t('keys.presetRock'), hint: t('keys.presetRockHint'), desc: t('keys.presetRockDesc') }
])

const selectedPreset = computed(() => presets.value.find((preset) => preset.bps === selectedBps.value) ?? presets.value[2])

const weights = computed(() => {
  const stability = selectedBps.value / 100
  return {
    price: Number((100 - stability).toFixed(2)),
    success: Number((stability * 0.8).toFixed(2)),
    ttft: Number((stability * 0.2).toFixed(2))
  }
})

const axes = computed(() => [
  { key: 'price', label: t('keys.weightPrice'), value: weights.value.price, ratio: weights.value.price / 100 },
  { key: 'success', label: t('keys.weightReliability'), value: weights.value.success, ratio: weights.value.success / 100 },
  { key: 'ttft', label: t('keys.weightTTFT'), value: weights.value.ttft, ratio: weights.value.ttft / 100 }
])

function point(index: number, ratio: number) {
  const angle = -Math.PI / 2 + (index * 2 * Math.PI) / 3
  return {
    x: cx + Math.cos(angle) * radius * ratio,
    y: cy + Math.sin(angle) * radius * ratio
  }
}

function polygonPoints(ratio: number) {
  return [0, 1, 2].map((index) => {
    const vertex = point(index, ratio)
    return `${vertex.x.toFixed(2)},${vertex.y.toFixed(2)}`
  }).join(' ')
}

const valueVertices = computed(() => axes.value.map((axis, index) => point(index, axis.ratio)))
const valuePoints = computed(() => valueVertices.value.map((vertex) => `${vertex.x.toFixed(2)},${vertex.y.toFixed(2)}`).join(' '))

function labelPoint(index: number) {
  const vertex = point(index, 1.16)
  if (index === 1) return { x: vertex.x + 8, y: vertex.y + 4 }
  if (index === 2) return { x: vertex.x - 8, y: vertex.y + 4 }
  return { x: vertex.x, y: vertex.y - 12 }
}

function labelAnchor(index: number) {
  if (index === 1) return 'start'
  if (index === 2) return 'end'
  return 'middle'
}
</script>
