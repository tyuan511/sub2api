<template>
  <button
    type="button"
    class="monitor-card group relative isolate flex min-h-[390px] w-full flex-col overflow-hidden rounded-[28px] border border-gray-200/90 bg-white/90 text-left shadow-[0_18px_50px_rgba(30,64,175,0.08)] backdrop-blur-xl transition-[transform,box-shadow,border-color] duration-300 ease-out hover:-translate-y-1.5 hover:border-primary-300/90 hover:shadow-[0_24px_65px_rgba(30,64,175,0.15)] focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-400/70 focus-visible:ring-offset-2 focus-visible:ring-offset-gray-50 active:scale-[0.995] motion-reduce:transform-none motion-reduce:transition-none dark:border-dark-600/80 dark:bg-dark-800/80 dark:shadow-[0_20px_50px_rgba(0,0,0,0.22)] dark:hover:border-primary-500/50"
    @click="emit('click')"
  >
    <span
      aria-hidden="true"
      class="pointer-events-none absolute inset-x-0 top-0 h-1 bg-gradient-to-r from-primary-500 via-sky-400 to-emerald-400 opacity-75 transition-opacity duration-300 group-hover:opacity-100"
    ></span>
    <span
      aria-hidden="true"
      class="pointer-events-none absolute -right-24 -top-24 h-64 w-64 rounded-full bg-primary-400/10 blur-3xl opacity-60 transition-all duration-500 group-hover:scale-110 group-hover:opacity-100 dark:bg-primary-500/10"
    ></span>

    <!-- Header: icon + name/model + status chip -->
    <div class="relative flex items-start gap-4 px-6 pt-7">
      <span
        class="grid h-12 w-12 flex-shrink-0 place-items-center rounded-2xl ring-1 ring-black/5 shadow-sm transition-transform duration-300 motion-reduce:transition-none group-hover:scale-105 dark:ring-white/10"
        :class="[providerGradient(item.provider), providerTintClass]"
      >
        <ProviderIcon :provider="item.provider" :size="24" />
      </span>
      <div class="flex-1 min-w-0">
        <div class="truncate text-lg font-bold tracking-tight text-gray-950 dark:text-white">
          {{ item.name }}
        </div>
        <div class="mt-1.5 flex min-w-0 items-center gap-2">
          <span
            class="inline-flex flex-shrink-0 items-center rounded-md px-2 py-1 text-[10px] font-bold uppercase tracking-[0.08em]"
            :class="providerBadgeClass(item.provider)"
          >
            {{ providerLabel(item.provider) }}
          </span>
          <!-- 纯配额模式主模型是占位符 "quota"，展示层替换为本地化「配额」标签 -->
          <span class="truncate font-mono text-xs text-gray-500 dark:text-gray-400">
            {{ formatMonitorModel(item.primary_model) }}
          </span>
        </div>
      </div>
      <span
        class="inline-flex flex-shrink-0 items-center gap-2 rounded-full px-3 py-1.5 text-[11px] font-bold tracking-[0.08em] ring-1 ring-inset ring-black/5 dark:ring-white/10"
        :class="statusBadgeClass(item.primary_status)"
      >
        <span
          class="h-2 w-2 rounded-full bg-current opacity-80 shadow-[0_0_0_4px_currentColor] [--tw-shadow-color:currentColor]"
          aria-hidden="true"
        ></span>
        {{ statusLabel(item.primary_status) }}
      </span>
    </div>

    <!-- Availability + TTFT + cache hit rate -->
    <div class="relative px-6">
      <MonitorAvailabilityRow
        :window-label="availabilityLabel"
        :value="availabilityValue"
        :first-token-ms="item.primary_first_token_ms"
        :cache-hit-rate="cacheHitRate"
        :samples-label="extraModelsCountLabel"
      />
    </div>

    <!-- 配额模式：最新用量/余额快照（服务端已按系统开关剥离，此处 flag 为纵深防御） -->
    <div v-if="quotaVisible" class="relative px-6">
      <MonitorQuotaView
        :snapshot="item.latest_quota"
        class="mt-5 rounded-2xl border border-gray-200/80 bg-gray-50/80 p-4 dark:border-dark-700 dark:bg-dark-900/45"
      />
    </div>

    <!-- Timeline -->
    <div class="relative mt-auto px-6 pb-6">
      <MonitorTimeline
        :buckets="item.timeline"
        :countdown-seconds="countdownSeconds"
      />
    </div>
  </button>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UserMonitorView } from '@/api/channelMonitor'
import {
  useChannelMonitorFormat,
  providerGradient,
} from '@/composables/useChannelMonitorFormat'
import { isChannelMonitorQuotaVisible } from '@/utils/featureFlags'
import ProviderIcon from './ProviderIcon.vue'
import MonitorAvailabilityRow from './MonitorAvailabilityRow.vue'
import MonitorTimeline from './MonitorTimeline.vue'
import MonitorQuotaView from '@/components/common/MonitorQuotaView.vue'

// 图标配色与 utils/platformColors.ts 的平台色对齐（新 4 家）。
const PROVIDER_TINT: Record<string, string> = {
  openai: 'text-emerald-600 dark:text-emerald-300',
  anthropic: 'text-orange-600 dark:text-orange-300',
  gemini: 'text-sky-600 dark:text-sky-300',
  grok: 'text-zinc-700 dark:text-zinc-200',
  antigravity: 'text-purple-600 dark:text-purple-300',
  kimi: 'text-pink-600 dark:text-pink-300',
  zhipu: 'text-indigo-600 dark:text-indigo-300',
  deepseek: 'text-teal-600 dark:text-teal-300',
}

const props = defineProps<{
  item: UserMonitorView
  window: '7d' | '15d' | '30d'
  availabilityValue: number | null
  cacheHitRate: number | null
  countdownSeconds: number
}>()

const emit = defineEmits<{
  (e: 'click'): void
}>()

const { t } = useI18n()
const {
  statusLabel,
  statusBadgeClass,
  providerLabel,
  providerBadgeClass,
  formatMonitorModel,
} = useChannelMonitorFormat()

const providerTintClass = computed(() =>
  PROVIDER_TINT[props.item.provider] ?? 'text-gray-500 dark:text-gray-300'
)

const quotaVisible = computed(
  () => isChannelMonitorQuotaVisible() && !!props.item.latest_quota
)

const availabilityLabel = computed(() => {
  const win = t(`channelStatus.windowTab.${props.window}`)
  return `${t('monitorCommon.availabilityPrefix')} · ${win}`
})

const extraModelsCountLabel = computed(() => {
  const count = props.item.extra_models?.length ?? 0
  if (count === 0) return undefined
  return t('monitorCommon.extraModelsCount', { n: count })
})
</script>
