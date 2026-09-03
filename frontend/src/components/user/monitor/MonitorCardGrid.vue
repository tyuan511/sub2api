<template>
  <div class="pb-8 pt-1">
    <div
      v-if="loading && items.length === 0"
      class="grid grid-cols-1 gap-5 lg:grid-cols-2 2xl:grid-cols-3"
    >
      <div
        v-for="i in 6"
        :key="i"
        class="min-h-[300px] overflow-hidden rounded-2xl border border-gray-200 bg-white p-5 shadow-card dark:border-dark-700 dark:bg-dark-800"
      >
        <div class="animate-pulse">
          <div class="flex items-start gap-3">
            <div class="h-10 w-10 rounded-xl bg-gray-200 dark:bg-dark-700"></div>
            <div class="flex-1 space-y-2 pt-1">
              <div class="h-5 w-2/3 rounded bg-gray-200 dark:bg-dark-700"></div>
              <div class="h-3 w-1/2 rounded bg-gray-200 dark:bg-dark-700"></div>
            </div>
            <div class="h-6 w-16 rounded-full bg-gray-200 dark:bg-dark-700"></div>
          </div>
          <div class="mt-5 grid grid-cols-3 gap-3 border-y border-gray-100 py-5 dark:border-dark-700/70">
            <div class="h-14 rounded bg-gray-100 dark:bg-dark-900/50"></div>
            <div class="h-14 rounded bg-gray-100 dark:bg-dark-900/50"></div>
            <div class="h-14 rounded bg-gray-100 dark:bg-dark-900/50"></div>
          </div>
          <div class="mt-5 h-8 w-full rounded-md bg-gray-100 dark:bg-dark-900/50"></div>
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
      class="grid grid-cols-1 gap-5 lg:grid-cols-2 2xl:grid-cols-3"
    >
      <MonitorCard
        v-for="item in items"
        :key="item.id"
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
  window: '3d' | '7d' | '15d' | '30d'
  countdownSeconds: number
  loading: boolean
  detailCache: Record<number, UserMonitorDetail>
}>()

const emit = defineEmits<{
  (e: 'cardClick', item: UserMonitorView): void
}>()

const { t } = useI18n()

function resolveAvailability(item: UserMonitorView): number | null {
  if (props.window === '3d') {
    return item.availability_3d ?? null
  }
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
  if (props.window === '3d') return item.cache_hit_rate_3d ?? null
  if (props.window === '7d') return item.cache_hit_rate_7d ?? null
  if (props.window === '15d') return item.cache_hit_rate_15d ?? null
  return item.cache_hit_rate_30d ?? null
}
</script>
