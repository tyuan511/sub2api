<template>
  <button
    type="button"
    class="monitor-card group flex min-h-[300px] w-full flex-col overflow-hidden rounded-2xl border border-gray-200 bg-white text-left shadow-card transition-[transform,box-shadow,border-color] duration-200 ease-out hover:-translate-y-0.5 hover:border-primary-300 hover:shadow-card-hover focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-400/70 focus-visible:ring-offset-2 focus-visible:ring-offset-gray-50 active:scale-[0.995] motion-reduce:transform-none motion-reduce:transition-none dark:border-dark-700 dark:bg-dark-800 dark:hover:border-primary-500/50"
    @click="emit('click')"
  >
    <!-- Header: icon + name/model + status chip -->
    <div class="flex items-start gap-3 px-5 pt-5">
      <span
        class="grid h-10 w-10 flex-shrink-0 place-items-center rounded-xl ring-1 ring-black/5 transition-transform duration-200 motion-reduce:transition-none group-hover:scale-105 dark:ring-white/10"
        :class="[providerGradient(item.provider), providerTintClass]"
      >
        <ProviderIcon :provider="item.provider" :size="20" />
      </span>
      <div class="min-w-0 flex-1">
        <div class="truncate text-base font-semibold tracking-tight text-gray-900 dark:text-gray-100">
          {{ item.name }}
        </div>
        <div class="mt-1 flex min-w-0 items-center gap-1.5">
          <span
            class="inline-flex flex-shrink-0 items-center rounded-md px-1.5 py-0.5 text-[10px] font-medium"
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
        class="inline-flex max-w-[38%] flex-shrink-0 items-center gap-1.5 truncate rounded-full px-2.5 py-1 text-[11px] font-medium ring-1 ring-inset ring-black/5 dark:ring-white/10"
        :class="statusBadgeClass(item.primary_status)"
      >
        <span
          class="h-1.5 w-1.5 flex-shrink-0 rounded-full bg-current opacity-80"
          aria-hidden="true"
        ></span>
        <span class="truncate">{{ statusLabel(item.primary_status) }}</span>
      </span>
    </div>

    <!-- Availability + TTFT + cache hit rate -->
    <div class="px-5">
      <MonitorAvailabilityRow
        :window-label="availabilityLabel"
        :value="availabilityValue"
        :first-token-ms="item.primary_first_token_ms"
        :cache-hit-rate="cacheHitRate"
        :samples-label="extraModelsCountLabel"
      />
    </div>

    <!-- 配额模式：最新用量/余额快照（服务端已按系统开关剥离，此处 flag 为纵深防御） -->
    <div v-if="quotaVisible" class="px-5">
      <MonitorQuotaView
        :snapshot="item.latest_quota"
        class="mt-4 border-t border-gray-100 pt-4 dark:border-dark-700/70"
      />
    </div>

    <!-- Timeline -->
    <div class="px-5 pb-5">
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
