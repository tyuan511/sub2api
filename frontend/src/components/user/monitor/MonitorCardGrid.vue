<template>
  <div class="pb-10 pt-2">
    <div
      v-if="loading && items.length === 0"
      class="grid grid-cols-1 gap-6 lg:grid-cols-2 2xl:grid-cols-3"
    >
      <div
        v-for="i in 6"
        :key="i"
        class="relative min-h-[390px] overflow-hidden rounded-[28px] border border-gray-200/80 bg-white/90 p-6 shadow-[0_18px_50px_rgba(30,64,175,0.08)] dark:border-dark-700 dark:bg-dark-800/80"
      >
        <div class="animate-pulse">
          <div class="flex items-start gap-4">
            <div class="h-12 w-12 rounded-2xl bg-gray-200 dark:bg-dark-700"></div>
            <div class="flex-1 space-y-2.5 pt-1">
              <div class="h-5 w-2/3 rounded bg-gray-200 dark:bg-dark-700"></div>
              <div class="h-3 w-1/2 rounded bg-gray-200 dark:bg-dark-700"></div>
            </div>
            <div class="h-7 w-20 rounded-full bg-gray-200 dark:bg-dark-700"></div>
          </div>
          <div class="mt-6 grid grid-cols-2 gap-3">
            <div class="h-24 rounded-2xl bg-gray-100 dark:bg-dark-900/50"></div>
            <div class="h-24 rounded-2xl bg-gray-100 dark:bg-dark-900/50"></div>
          </div>
          <div class="mt-6 grid grid-cols-2 gap-3">
            <div class="h-28 rounded-2xl bg-gray-100 dark:bg-dark-900/50"></div>
            <div class="h-28 rounded-2xl bg-gray-100 dark:bg-dark-900/50"></div>
          </div>
          <div class="mt-6 h-9 w-full rounded-lg bg-gray-100 dark:bg-dark-900/50"></div>
        </div>
      </div>
    </div>

    <EmptyState
      v-else-if="items.length === 0"
      :title="t('channelStatus.empty.title')"
      :description="t('channelStatus.empty.description')"
    />

    <div
      v-else
      class="grid grid-cols-1 gap-6 lg:grid-cols-2 2xl:grid-cols-3"
    >
      <MonitorCard
        v-for="(item, index) in items"
        :key="item.id"
        class="monitor-card-reveal"
        :style="{ animationDelay: `${Math.min(index, 8) * 45}ms` }"
        :item="item"
        :window="window"
        :availability-value="resolveAvailability(item)"
        :cache-hit-rate="resolveCacheHitRate(item)"
        :countdown-seconds="countdownSeconds"
        @click="emit('cardClick', item)"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { UserMonitorView, UserMonitorDetail } from '@/api/channelMonitor'
import EmptyState from '@/components/common/EmptyState.vue'
import MonitorCard from './MonitorCard.vue'

const props = defineProps<{
  items: UserMonitorView[]
  window: '7d' | '15d' | '30d'
  countdownSeconds: number
  loading: boolean
  detailCache: Record<number, UserMonitorDetail>
}>()

const emit = defineEmits<{
  (e: 'cardClick', item: UserMonitorView): void
}>()

const { t } = useI18n()

function resolveAvailability(item: UserMonitorView): number | null {
  if (props.window === '7d') {
    return item.availability_7d ?? null
  }
  const detail = props.detailCache[item.id]
  if (!detail) return null
  const primary = detail.models.find(m => m.model === item.primary_model)
  if (!primary) return null
  return props.window === '15d' ? primary.availability_15d ?? null : primary.availability_30d ?? null
}

function resolveCacheHitRate(item: UserMonitorView): number | null {
  if (props.window === '7d') return item.cache_hit_rate_7d ?? null
  if (props.window === '15d') return item.cache_hit_rate_15d ?? null
  return item.cache_hit_rate_30d ?? null
}
</script>

<style scoped>
.monitor-card-reveal {
  animation: monitor-card-in 480ms cubic-bezier(0.22, 1, 0.36, 1) both;
}

@keyframes monitor-card-in {
  from {
    opacity: 0;
    transform: translateY(16px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

@media (prefers-reduced-motion: reduce) {
  .monitor-card-reveal {
    animation: none;
  }
}
</style>
