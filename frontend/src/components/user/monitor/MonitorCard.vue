<template>
  <button
    type="button"
    class="group relative isolate overflow-visible text-left p-5 rounded-3xl min-h-[280px] w-full bg-white border border-gray-200/80 shadow-sm dark:bg-dark-800/80 dark:border-dark-700/70 hover:-translate-y-0.5 hover:shadow-card-hover dark:hover:border-primary-500/30 hover:border-primary-200/80 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-400/60 focus-visible:ring-offset-2 focus-visible:ring-offset-gray-50 dark:focus-visible:ring-offset-dark-950 active:scale-[0.995] motion-reduce:transform-none motion-reduce:transition-none transition-all duration-300 ease-out flex flex-col"
    @click="emit('click')"
  >
    <!-- Header: icon + name/model + status chip -->
    <div class="flex items-start gap-3">
      <span
        class="w-10 h-10 rounded-2xl ring-1 ring-black/5 dark:ring-white/10 grid place-items-center flex-shrink-0 transition-transform duration-300 motion-reduce:transition-none group-hover:scale-105"
        :class="[providerGradient(item.provider), providerTintClass]"
      >
        <ProviderIcon :provider="item.provider" :size="20" />
      </span>
      <div class="flex-1 min-w-0">
        <div class="text-[15px] font-bold truncate text-gray-900 dark:text-gray-100">
          {{ item.name }}
        </div>
        <div class="mt-0.5 flex items-center gap-1.5 min-w-0">
          <span
            class="inline-flex items-center rounded-md px-1.5 py-0.5 text-[10px] font-medium flex-shrink-0"
            :class="providerBadgeClass(item.provider)"
          >
            {{ providerLabel(item.provider) }}
          </span>
          <!-- 纯配额模式主模型是占位符 "quota"，展示层替换为本地化「配额」标签 -->
          <span class="font-mono text-xs truncate text-gray-500 dark:text-gray-400">
            {{ formatMonitorModel(item.primary_model) }}
          </span>
        </div>
      </div>
      <span
        class="inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-full text-xs font-semibold flex-shrink-0 ring-1 ring-inset ring-black/5 dark:ring-white/10"
        :class="statusBadgeClass(item.primary_status)"
      >
        <span
          class="h-1.5 w-1.5 rounded-full bg-current opacity-80"
          aria-hidden="true"
        ></span>
        {{ statusLabel(item.primary_status) }}
      </span>
    </div>

    <!-- Metrics -->
    <div class="mt-5 grid grid-cols-3 gap-2.5">
      <div class="rounded-2xl border border-gray-200/80 bg-gray-50/80 p-3 dark:border-dark-700/70 dark:bg-dark-900/45">
        <span class="block text-[10px] font-semibold uppercase tracking-[0.08em] text-gray-400">{{ t('monitorCommon.availabilityPrefix') }}</span>
        <strong class="mt-2 block text-xl font-bold tabular-nums text-emerald-600 dark:text-emerald-400">{{ availabilityValue == null ? '-' : `${availabilityValue.toFixed(1)}%` }}</strong>
      </div>
      <div class="rounded-2xl border border-gray-200/80 bg-gray-50/80 p-3 dark:border-dark-700/70 dark:bg-dark-900/45">
        <span class="block text-[10px] font-semibold uppercase tracking-[0.08em] text-gray-400">{{ t('monitorCommon.firstToken') }}</span>
        <strong class="mt-2 block text-xl font-bold tabular-nums" :class="primaryMetricClass">{{ formatFirstTokenSeconds(item.primary_first_token_ms) }}<small class="ml-0.5 text-xs font-normal text-gray-400">s</small></strong>
      </div>
      <div class="rounded-2xl border border-gray-200/80 bg-gray-50/80 p-3 dark:border-dark-700/70 dark:bg-dark-900/45">
        <span class="block text-[10px] font-semibold uppercase tracking-[0.08em] text-gray-400">{{ t('monitorCommon.cacheHitRate') }}</span>
        <strong class="mt-2 block text-xl font-bold tabular-nums text-emerald-600 dark:text-emerald-400">{{ cacheHitRate == null ? '-' : `${cacheHitRate.toFixed(1)}%` }}</strong>
      </div>
    </div>

    <!-- 配额模式：最新用量/余额快照（服务端已按系统开关剥离，此处 flag 为纵深防御） -->
    <MonitorQuotaView v-if="quotaVisible" :snapshot="item.latest_quota" class="mt-2" />

    <!-- Divider -->
    <div class="mt-4 border-t border-gray-100 dark:border-dark-700/60"></div>

    <!-- Availability row -->
    <!-- Timeline -->
    <MonitorTimeline
      :buckets="item.timeline"
      :countdown-seconds="countdownSeconds"
      :cache-hit-rate="cacheHitRate"
    />
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
import { firstTokenSeverity, LATENCY_TEXT_CLASSES } from '@/utils/latencyHealth'
import ProviderIcon from './ProviderIcon.vue'
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

const primaryMetricClass = computed(() =>
  props.item.primary_first_token_ms == null
    ? 'text-gray-900 dark:text-gray-100'
    : LATENCY_TEXT_CLASSES[firstTokenSeverity(props.item.primary_first_token_ms)],
)

function formatFirstTokenSeconds(ms: number | null | undefined): string {
  if (ms == null) return t('monitorCommon.latencyEmpty')
  return (ms / 1000).toFixed(1)
}
</script>
