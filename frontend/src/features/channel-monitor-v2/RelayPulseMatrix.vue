<template>
  <section>
    <div class="matrix-header sr-only">
      {{ t('channelMonitorV2.metrics.cacheRate') }}
      {{ t('channelMonitorV2.metrics.availability') }}
      {{ t('channelMonitorV2.metrics.ttft') }}
    </div>
    <div v-if="rows.length" class="space-y-8">
      <div v-for="group in platformGroups" :key="group.platform">
        <div class="mb-4 flex items-center gap-3 px-1">
          <span
            class="grid h-9 w-9 flex-none place-items-center rounded-xl ring-1 ring-black/5 dark:ring-white/10"
            :class="[providerGradient(group.provider), providerTint(group.provider)]"
          >
            <ProviderIcon :provider="group.provider" :size="18" />
          </span>
          <h3 class="text-lg font-extrabold tracking-tight text-gray-900 dark:text-white">
            {{ group.label }}
          </h3>
          <span class="rounded-full border border-gray-200 bg-white px-2 py-0.5 text-xs font-semibold text-gray-500 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-300">
            {{ group.rows.length }}
          </span>
        </div>
        <div class="relay-card-grid">
          <article
            v-for="entry in group.rows"
            :key="rowKey(entry.row)"
            class="relay-card-item card !rounded-3xl !border-0 p-5 shadow-sm ring-1 ring-gray-900/5 dark:!bg-dark-800 dark:ring-dark-700"
          >
            <header class="flex items-start justify-between gap-2">
              <div class="flex min-w-0 items-center gap-3">
                <span
                  class="grid h-10 w-10 flex-none place-items-center rounded-xl ring-1 ring-black/5 dark:ring-white/10"
                  :class="[providerGradient(group.provider), providerTint(group.provider)]"
                >
                  <ProviderIcon :provider="group.provider" :size="20" />
                </span>
                <div class="min-w-0">
                  <h4 class="truncate text-base font-semibold tracking-tight text-gray-900 dark:text-white">
                    {{ cardTitle(entry.row) }}
                  </h4>
                  <div class="mt-1 flex min-w-0 items-center gap-1.5 whitespace-nowrap">
                    <span
                      class="inline-flex flex-shrink-0 items-center rounded-md px-1.5 py-0.5 text-[10px] font-medium"
                      :class="providerBadgeClass(group.provider)"
                    >
                      {{ group.label }}
                    </span>
                    <span
                      v-if="rateLabel(entry.row)"
                      class="user-rate font-mono text-[11px] text-gray-500 dark:text-gray-400"
                    >
                      {{ rateLabel(entry.row) }}
                    </span>
                  </div>
                </div>
              </div>
              <span :class="['relay-health-pill bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300', cellClass(entry.row.health, entry.row.metrics.request_count)]">
                {{ healthLabel(entry.row.health, entry.row.metrics.request_count) }}
              </span>
            </header>

            <div class="mt-5 grid grid-cols-3 gap-2">
              <div class="relay-stat rounded-2xl bg-gray-50 px-2.5 py-2.5 dark:bg-dark-900/70">
                <span>{{ t('channelMonitorV2.metrics.cacheRate') }}</span>
                <strong>{{ formatPercent(entry.row.metrics.cache_rate) }}</strong>
              </div>
              <div class="relay-stat rounded-2xl bg-gray-50 px-2.5 py-2.5 dark:bg-dark-900/70">
                <span>{{ t('channelMonitorV2.metrics.availability') }}</span>
                <strong :class="metricTone(entry.row.health.error_rate)">{{ availability(entry.row.metrics) }}</strong>
              </div>
              <div class="relay-stat rounded-2xl bg-gray-50 px-2.5 py-2.5 dark:bg-dark-900/70">
                <span>{{ t('channelMonitorV2.metrics.ttft') }}</span>
                <strong :class="metricTone(entry.row.health.ttft)">{{ formatMs(entry.row.metrics.ttft.p50_ms) }}</strong>
              </div>
            </div>

            <div class="mt-5 border-t border-gray-100 pt-4 dark:border-dark-700/70">
              <div class="mb-2.5 flex items-center justify-between gap-3 text-[10px] font-medium uppercase tracking-[0.12em] text-gray-400">
                <span>{{ t('monitorCommon.history60pts', { n: PULSE_RECORD_COUNT }) }}</span>
                <span class="font-mono tabular-nums text-gray-500 dark:text-gray-400">
                  {{ t('monitorCommon.nextUpdateIn', { n: countdownSeconds }) }}
                </span>
              </div>
              <div class="pulse-track relay-pulse-track" role="img" :aria-label="t('monitorCommon.history60pts', { n: PULSE_RECORD_COUNT })">
                <span
                  v-for="(slot, index) in entry.slots"
                  :key="slot.start"
                  class="pulse-cell relay-pulse-cell relative"
                  :class="[
                    slot.bucket ? cellClass(slot.bucket.health, slot.bucket.metrics.request_count) : 'health-unknown',
                    slot.bucket ? 'has-data' : 'is-empty',
                  ]"
                  :style="{ height: `${barHeight(slot)}%`, '--bar-delay': `${index * 28}ms` }"
                  :title="slot.bucket ? bucketTooltip(slot.bucket) : formatBucketTime(slot.start)"
                  tabindex="0"
                  role="img"
                  :aria-label="slot.bucket ? bucketTooltip(slot.bucket) : formatBucketTime(slot.start)"
                  @mouseenter="showTooltip($event, slot)"
                  @mousemove="moveTooltip($event)"
                  @mouseleave="hideTooltip"
                  @focus="showTooltip($event, slot)"
                  @blur="hideTooltip"
                >
                  <span class="pulse-tooltip" role="tooltip">
                    <span class="pulse-tooltip-line">{{ slot.bucket ? bucketTooltip(slot.bucket) : t('channelMonitorV2.matrix.noTraffic') }}</span>
                  </span>
                </span>
              </div>
              <div class="mt-1.5 flex justify-between font-mono text-[9px] font-medium uppercase tracking-[0.14em] text-gray-400">
                <span>{{ t('monitorCommon.past') }}</span>
                <span>{{ t('monitorCommon.now') }}</span>
              </div>
            </div>
          </article>
        </div>
      </div>
    </div>
    <div v-else class="flex min-h-[200px] items-center justify-center text-sm text-gray-400">
      {{ t('channelMonitorV2.matrix.emptyTitle') }}
    </div>

    <Teleport to="body">
      <div
        v-if="floatingTooltip.visible"
        class="matrix-floating-tooltip"
        :style="{ left: `${floatingTooltip.x}px`, top: `${floatingTooltip.y}px` }"
        role="tooltip"
      >
        {{ floatingTooltip.text }}
      </div>
    </Teleport>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { computed, reactive } from 'vue'
import type {
  MonitorCoverage,
  MonitorHealth,
  MonitorMatrixBucket,
  MonitorMatrixRow,
  MonitorMetric,
} from '@/api/channelMonitorV2'
import type { Provider } from '@/api/admin/channelMonitor'
import ProviderIcon from '@/components/user/monitor/ProviderIcon.vue'
import {
  providerGradient,
  useChannelMonitorFormat,
} from '@/composables/useChannelMonitorFormat'
import { formatMultiplier } from '@/utils/formatters'
import {
  PROVIDER_ANTHROPIC,
  PROVIDER_GEMINI,
  PROVIDER_GROK,
  PROVIDER_OPENAI,
} from '@/constants/channelMonitor'
import {
  formatMonitorMs,
  formatMonitorPercent,
  formatMonitorSuccessRateFromError,
  healthModeScore,
  healthScoreClass,
} from '@/features/channel-monitor-v2/monitorFormat'

const PULSE_RECORD_COUNT = 18

type HealthMode = 'overall' | 'success' | 'ttft' | 'cache'
const { t } = useI18n()
const { providerLabel, providerBadgeClass } = useChannelMonitorFormat()

const PLATFORM_ORDER = [
  PROVIDER_OPENAI,
  PROVIDER_ANTHROPIC,
  PROVIDER_GROK,
  PROVIDER_GEMINI,
] as const

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

const props = withDefaults(
  defineProps<{
    rows: MonitorMatrixRow[]
    coverage: MonitorCoverage
    healthMode: HealthMode
    showThroughput?: boolean
    ratesByGroupId?: Record<number, number>
    countdownSeconds?: number
  }>(),
  { showThroughput: true, countdownSeconds: 0 },
)

type AlignedSlot = { start: string; bucket?: MonitorMatrixBucket }
type AlignedRow = { row: MonitorMatrixRow; slots: AlignedSlot[] }

const floatingTooltip = reactive({
  visible: false,
  x: 0,
  y: 0,
  text: '',
})

const allBucketStarts = computed(() => {
  const step = Math.max(60, props.coverage.bucket_seconds) * 1000
  const requestedStart = new Date(props.coverage.requested_start).getTime()
  const requestedEndRaw = props.coverage.requested_end
    ? new Date(props.coverage.requested_end).getTime()
    : NaN
  const dataThrough = new Date(props.coverage.data_through).getTime()
  const end = Number.isFinite(requestedEndRaw) && requestedEndRaw > requestedStart
    ? requestedEndRaw
    : dataThrough
  if (![requestedStart, end].every(Number.isFinite) || requestedStart >= end) return []
  const starts: string[] = []
  for (let cursor = Math.floor(requestedStart / step) * step; cursor < end; cursor += step) {
    starts.push(new Date(cursor).toISOString())
  }
  return starts
})

/** Fixed last-N window. Pad the left with empty slots when history is short. */
const bucketStarts = computed(() => {
  const all = allBucketStarts.value
  if (all.length >= PULSE_RECORD_COUNT) return all.slice(-PULSE_RECORD_COUNT)
  const step = Math.max(60, props.coverage.bucket_seconds) * 1000
  const first = all.length ? new Date(all[0]).getTime() : Date.now()
  const pad: string[] = []
  for (let i = PULSE_RECORD_COUNT - all.length; i > 0; i -= 1) {
    pad.push(new Date(first - i * step).toISOString())
  }
  return [...pad, ...all]
})

const bucketStartIndex = computed(() => {
  const map = new Map<string, number>()
  bucketStarts.value.forEach((start, index) => map.set(start, index))
  return map
})

const alignedRows = computed<AlignedRow[]>(() => {
  const starts = bucketStarts.value
  const indexByStart = bucketStartIndex.value
  return props.rows.map((row) => {
    const slots: AlignedSlot[] = starts.map((start) => ({ start }))
    for (const bucket of row.buckets || []) {
      const key = new Date(bucket.bucket_start).toISOString()
      const index = indexByStart.get(key)
      if (index != null) slots[index] = { start: starts[index], bucket }
    }
    return { row, slots }
  })
})

const platformGroups = computed(() => {
  const groups = new Map<string, AlignedRow[]>()
  for (const entry of alignedRows.value) {
    const platform = entry.row.platform || t('channelMonitorV2.matrix.dimension')
    const rows = groups.get(platform) || []
    rows.push(entry)
    groups.set(platform, rows)
  }
  return Array.from(groups, ([platform, rows]) => {
    const provider = normalizeProvider(platform)
    return {
      platform,
      provider,
      label: providerLabel(provider) === provider ? platform : providerLabel(provider),
      rows,
    }
  }).sort((a, b) => {
    const ai = PLATFORM_ORDER.indexOf(a.provider as (typeof PLATFORM_ORDER)[number])
    const bi = PLATFORM_ORDER.indexOf(b.provider as (typeof PLATFORM_ORDER)[number])
    const aRank = ai === -1 ? PLATFORM_ORDER.length : ai
    const bRank = bi === -1 ? PLATFORM_ORDER.length : bi
    if (aRank !== bRank) return aRank - bRank
    return a.label.localeCompare(b.label)
  })
})

function normalizeProvider(platform: string): Provider {
  return platform.trim().toLowerCase() as Provider
}

function providerTint(provider: string): string {
  return PROVIDER_TINT[provider] ?? 'text-gray-500 dark:text-gray-300'
}

function rateLabel(row: MonitorMatrixRow): string {
  const id = row.group_id
  if (id == null) return ''
  const rate = props.ratesByGroupId?.[id]
  if (rate == null || Number.isNaN(Number(rate))) return ''
  return t('channelMonitorV2.userRate', { value: formatMultiplier(Number(rate)) })
}

function cellClass(health: MonitorHealth, requestCount: number): string {
  return healthScoreClass(health, props.healthMode, requestCount)
}

function healthLabel(health: MonitorHealth, requestCount: number): string {
  const state = cellClass(health, requestCount)
  const label = (key: string) => t(key).replace(/\s*\(.+$/, '')
  if (state.includes('score')) {
    const score = healthModeScore(health, props.healthMode)
    if (score != null && score >= 80) return label('channelMonitorV2.matrix.healthyLegend')
    if (score != null && score >= 50) return label('channelMonitorV2.matrix.warningLegend')
    return label('channelMonitorV2.matrix.criticalLegend')
  }
  if (state === 'health-healthy') return label('channelMonitorV2.matrix.healthyLegend')
  if (state === 'health-warning') return label('channelMonitorV2.matrix.warningLegend')
  if (state === 'health-critical') return label('channelMonitorV2.matrix.criticalLegend')
  return label('channelMonitorV2.matrix.unknownLegend')
}

function rowKey(row: MonitorMatrixRow): string {
  return [row.platform, row.group_id || 0, row.model || ''].join(':')
}

function cardTitle(row: MonitorMatrixRow): string {
  if (row.group_name) return row.group_name
  if (row.model === '__other__') return t('channelMonitorV2.otherModels')
  if (row.model) return row.model
  return row.platform
}

function availability(metrics: MonitorMetric): string {
  const noCount = metrics.request_count <= 0
  const noTP = (metrics.rpm || 0) <= 0 && (metrics.tpm || 0) <= 0
  if (noCount && noTP && props.showThroughput) return '-'
  return formatMonitorSuccessRateFromError(metrics.error_rate)
}

function metricTone(state: string | undefined): string {
  if (state === 'healthy') return 'text-emerald-600 dark:text-emerald-300'
  if (state === 'warning') return 'text-amber-600 dark:text-amber-300'
  if (state === 'critical') return 'text-red-600 dark:text-red-300'
  return ''
}

function barHeight(slot: AlignedSlot): number {
  if (!slot.bucket) return 18
  const score = healthModeScore(slot.bucket.health, props.healthMode)
  if (score != null && !Number.isNaN(score)) {
    return Math.max(18, Math.round(score))
  }
  const coarse = slot.bucket.health.overall
  if (coarse === 'healthy') return 100
  if (coarse === 'warning') return 40
  if (coarse === 'critical') return 32
  if (slot.bucket.metrics.request_count > 0) return 40
  return 18
}

function bucketTooltip(bucket: MonitorMatrixBucket): string {
  const metrics = bucket.metrics
  return [
    formatBucketTime(bucket.bucket_start),
    t('channelMonitorV2.metrics.availabilityValue', { value: availability(metrics) }),
    t('channelMonitorV2.metrics.cacheRateValue', { value: formatPercent(metrics.cache_rate) }),
    t('channelMonitorV2.metrics.ttftValue', { value: formatMs(metrics.ttft.p50_ms) }),
  ].join(' · ')
}

function showTooltip(event: MouseEvent | FocusEvent, slot: AlignedSlot) {
  floatingTooltip.text = slot.bucket
    ? bucketTooltip(slot.bucket)
    : t('channelMonitorV2.matrix.noTraffic')
  floatingTooltip.visible = true
  positionTooltip(event)
}

function moveTooltip(event: MouseEvent) {
  if (!floatingTooltip.visible) return
  positionTooltip(event)
}

function hideTooltip() {
  floatingTooltip.visible = false
}

function positionTooltip(event: MouseEvent | FocusEvent) {
  if ('clientX' in event) {
    floatingTooltip.x = Math.min(window.innerWidth - 12, Math.max(12, event.clientX))
    floatingTooltip.y = Math.min(window.innerHeight - 12, Math.max(12, event.clientY)) - 12
    return
  }
  const target = event.target as HTMLElement | null
  const rect = target?.getBoundingClientRect()
  if (!rect) return
  floatingTooltip.x = rect.left + rect.width / 2
  floatingTooltip.y = rect.top - 10
}

function formatPercent(value: number) {
  return formatMonitorPercent(value)
}

function formatMs(value: number | null) {
  return formatMonitorMs(value)
}

function formatBucketTime(value: string) {
  const date = new Date(value)
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  const hour = String(date.getHours()).padStart(2, '0')
  const minute = String(date.getMinutes()).padStart(2, '0')
  return `${month}/${day} ${hour}:${minute}`
}
</script>

<style scoped>
.relay-card-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 1.25rem;
}
@media (min-width: 640px) {
  .relay-card-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (min-width: 1024px) {
  .relay-card-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}
@media (min-width: 1536px) {
  .relay-card-grid {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}
.relay-health-pill {
  border-radius: 999px;
  padding: 0.12rem 0.42rem;
  font-size: 0.625rem;
  font-weight: 600;
  line-height: 1.2;
  flex: none;
  white-space: nowrap;
}
.relay-health-pill.health-warning,
.relay-health-pill.health-score4,
.relay-health-pill.health-score5,
.relay-health-pill.health-score6,
.relay-health-pill.health-score7 {
  @apply bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300;
}
.relay-health-pill.health-critical,
.relay-health-pill.health-score0,
.relay-health-pill.health-score1,
.relay-health-pill.health-score2,
.relay-health-pill.health-score3 {
  @apply bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300;
}
.relay-stat span {
  display: block;
  font-size: 0.65rem;
  color: #94a3b8;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.relay-stat strong {
  display: block;
  margin-top: 0.35rem;
  font-size: 1.15rem;
  font-variant-numeric: tabular-nums;
  font-weight: 700;
  @apply text-gray-900 dark:text-gray-50;
}

.relay-pulse-track {
  display: flex;
  align-items: flex-end;
  height: 24px;
  gap: 3px;
  overflow: visible;
}
.relay-pulse-cell {
  min-width: 0;
  flex: 1;
  border-radius: 3px;
  transform-origin: bottom;
  animation: relay-bar-enter 0.58s cubic-bezier(0.22, 1.2, 0.36, 1) both;
  animation-delay: var(--bar-delay, 0ms);
  @apply bg-gray-300 dark:bg-slate-600;
}
@keyframes relay-bar-enter {
  from {
    transform: scaleY(0.12);
    opacity: 0.25;
  }
  to {
    transform: scaleY(1);
    opacity: 1;
  }
}
@media (prefers-reduced-motion: reduce) {
  .relay-pulse-cell {
    animation: none;
  }
}
.relay-pulse-cell.has-data { cursor: help; }
.relay-pulse-cell.is-empty { opacity: 0.7; }
.relay-pulse-cell.health-score10,
.relay-pulse-cell.health-score9,
.relay-pulse-cell.health-score8,
.relay-pulse-cell.health-healthy { background: #10b981; }
.relay-pulse-cell.health-score7,
.relay-pulse-cell.health-score6,
.relay-pulse-cell.health-score5,
.relay-pulse-cell.health-warning { background: #f59e0b; }
.relay-pulse-cell.health-score4,
.relay-pulse-cell.health-score3,
.relay-pulse-cell.health-score2,
.relay-pulse-cell.health-score1,
.relay-pulse-cell.health-score0,
.relay-pulse-cell.health-critical { background: #ef4444; }
.relay-pulse-cell.health-unknown {
  @apply bg-gray-300 dark:bg-slate-600;
}
.relay-pulse-cell:hover,
.relay-pulse-cell:focus-visible {
  filter: brightness(0.92);
  outline: 2px solid rgb(16 185 129 / 0.4);
  outline-offset: 1px;
}

.pulse-tooltip {
  display: none;
}
.matrix-floating-tooltip {
  pointer-events: none;
  position: fixed;
  z-index: 9999;
  max-width: min(18rem, calc(100vw - 1.5rem));
  transform: translate(-50%, -100%);
  border-radius: 0.5rem;
  background: rgb(17 24 39);
  color: rgb(243 244 246);
  padding: 0.4rem 0.65rem;
  font-size: 12px;
  line-height: 1.45;
  box-shadow: 0 12px 28px -10px rgb(0 0 0 / 0.45);
}
</style>
