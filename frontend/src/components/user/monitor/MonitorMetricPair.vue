<template>
  <div class="mt-5 grid grid-cols-2 gap-2.5">
    <div
      class="rounded-2xl p-3.5 bg-gradient-to-br from-gray-50/95 to-white/70 dark:from-dark-900/55 dark:to-dark-800/35 border border-gray-200/70 dark:border-dark-700/60 shadow-sm transition-all duration-300 motion-reduce:transition-none group-hover:-translate-y-0.5 group-hover:border-gray-300/80 dark:group-hover:border-dark-600"
    >
      <div
        class="flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-[0.12em] text-gray-400 dark:text-gray-500"
      >
        <Icon :name="primaryIcon" size="xs" />
        <span>{{ primaryLabel }}</span>
      </div>
      <div class="mt-1.5 text-xl font-bold tracking-tight font-mono tabular-nums" :class="primaryValueClass || 'text-gray-900 dark:text-gray-100'">
        {{ primaryValue }}<span class="text-xs font-normal text-gray-400 ml-0.5">{{ primaryUnit }}</span>
      </div>
    </div>
    <div
      class="rounded-2xl p-3.5 bg-gradient-to-br from-gray-50/95 to-white/70 dark:from-dark-900/55 dark:to-dark-800/35 border border-gray-200/70 dark:border-dark-700/60 shadow-sm transition-all duration-300 motion-reduce:transition-none group-hover:-translate-y-0.5 group-hover:border-gray-300/80 dark:group-hover:border-dark-600"
    >
      <div
        class="flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-[0.12em] text-gray-400 dark:text-gray-500"
      >
        <Icon :name="secondaryIcon" size="xs" />
        <span>{{ secondaryLabel }}</span>
      </div>
      <div class="mt-1.5 text-xl font-bold tracking-tight font-mono tabular-nums text-gray-900 dark:text-gray-100">
        {{ secondaryValue }}<span class="text-xs font-normal text-gray-400 ml-0.5">{{ secondaryUnit }}</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'

defineProps<{
  primaryLabel: string
  primaryValue: string
  primaryUnit: string
  primaryIcon: 'bolt' | 'globe' | 'clock' | 'link' | 'trendingUp'
  primaryValueClass?: string
  secondaryLabel: string
  secondaryValue: string
  secondaryUnit: string
  secondaryIcon: 'bolt' | 'globe' | 'clock' | 'link' | 'trendingUp'
}>()
</script>
