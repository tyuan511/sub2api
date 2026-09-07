<template>
  <AppLayout>
    <div class="space-y-6 pb-12">
      <!-- Ops-style elevated shell: title toolbar + filters (mirrors OpsDashboardHeader) -->
      <section
        class="card !rounded-3xl !border-0 p-0 shadow-sm ring-1 ring-gray-900/5 dark:!bg-dark-800 dark:ring-dark-700"
      >
        <header class="page-header mb-0 flex flex-wrap items-start justify-between gap-4 border-b border-gray-100 px-5 py-4 dark:border-dark-700 sm:px-6">
          <div class="min-w-0">
            <h1 class="page-title flex items-center gap-2 text-xl font-black text-gray-900 dark:text-white">
              <span class="inline-flex h-8 w-8 items-center justify-center rounded-xl bg-emerald-50 text-emerald-500 dark:bg-emerald-900/30 dark:text-emerald-400">
                <Icon name="chart" size="sm" />
              </span>
              {{ t('channelStatus.title') }}
            </h1>
            <div class="page-description mt-1.5 flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
              <span class="relative flex h-2 w-2 shrink-0">
                <span
                  class="relative inline-flex h-2 w-2 rounded-full"
                  :class="loading || refreshing ? 'bg-gray-400' : 'bg-green-500'"
                ></span>
              </span>
              <span v-if="refreshing" class="inline-flex items-center gap-1 text-primary-600 dark:text-primary-300">
                <LoadingSpinner size="sm" />
                {{ t('channelMonitorV2.updating') }}
              </span>
              <span v-else-if="snapshot?.coverage.data_through">
                {{ t('channelMonitorV2.updatedTo', { time: formatTime(snapshot.coverage.data_through) }) }}
              </span>
              <span v-else class="text-gray-400">{{ t('common.loading') }}</span>
              <span
                v-if="snapshot && !snapshot.coverage.coverage_complete && !bootstrapActive"
                class="badge badge-warning"
              >
                {{ t('channelMonitorV2.partialCoverage') }}
              </span>
              <span
                v-if="bootstrapActive"
                class="badge badge-primary inline-flex items-center gap-1"
              >
                <LoadingSpinner size="sm" />
                {{ t('channelMonitorV2.bootstrap.progress', { percent: bootstrapPercent }) }}
              </span>
            </div>
          </div>
          <button
            class="btn btn-secondary btn-icon flex h-8 w-8 items-center justify-center rounded-lg bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-dark-700 dark:text-gray-400 dark:hover:bg-dark-600"
            type="button"
            :title="t('common.refresh')"
            :disabled="loading"
            @click="reload(false)"
          >
            <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
          </button>
        </header>

        <!-- First-upgrade silent backfill: show until 30d product window is covered -->
        <div
          v-if="bootstrapActive"
          class="border-b border-blue-100 bg-blue-50/90 px-5 py-3 dark:border-blue-900/40 dark:bg-blue-950/40 sm:px-6"
          role="status"
          aria-live="polite"
        >
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div class="min-w-0 flex-1">
              <p class="text-sm font-semibold text-blue-900 dark:text-blue-100">
                {{ t('channelMonitorV2.bootstrap.title') }}
              </p>
              <p class="mt-0.5 text-xs text-blue-800/80 dark:text-blue-200/80">
                {{ t('channelMonitorV2.bootstrap.description') }}
              </p>
            </div>
            <span class="shrink-0 text-xs font-medium tabular-nums text-blue-700 dark:text-blue-300">
              {{ t('channelMonitorV2.bootstrap.progress', { percent: bootstrapPercent }) }}
            </span>
          </div>
          <div
            class="mt-2.5 h-1.5 overflow-hidden rounded-full bg-blue-200/80 dark:bg-blue-900/60"
            role="progressbar"
            :aria-valuenow="bootstrapPercent"
            aria-valuemin="0"
            aria-valuemax="100"
            :aria-label="t('channelMonitorV2.bootstrap.working')"
          >
            <div
              class="h-full rounded-full bg-blue-500 transition-[width] duration-500 ease-out dark:bg-blue-400"
              :style="{ width: `${bootstrapPercent}%` }"
            />
          </div>
        </div>

        <!-- Range + V2 contract + overall rates (screenshot layout) -->
        <div class="monitor-toolbar flex flex-nowrap items-center gap-2 overflow-x-auto px-4 py-3 sm:px-5">
          <div
            class="tabs inline-flex shrink-0"
            role="group"
            :aria-label="t('channelMonitorV2.timeRange')"
          >
            <button
              v-for="option in ranges"
              :key="option.value"
              type="button"
              class="tab !px-2 !py-1 text-xs sm:!px-2.5"
              :class="filter.range === option.value ? 'tab-active' : ''"
              @click="setRange(option.value)"
            >
              {{ option.label }}
            </button>
          </div>

          <span class="mx-0.5 hidden h-5 w-px shrink-0 bg-gray-200 dark:bg-dark-700 sm:block" aria-hidden="true"></span>
          <span class="hidden shrink-0 text-xs text-gray-500 dark:text-gray-400 sm:inline">
            {{ t('channelMonitorV2.passiveUsage') }}
          </span>
          <span
            v-if="snapshot"
            class="ml-auto shrink-0 text-xs tabular-nums text-gray-500 dark:text-gray-400"
          >
            {{
              t('channelMonitorV2.overallRates', {
                availability: formatPercent(1 - snapshot.metrics.error_rate),
                cache: formatPercent(snapshot.metrics.cache_rate),
              })
            }}
          </span>
        </div>
      </section>

      <div class="relative min-h-[320px]">
        <RelayPulseMatrix
          v-if="matrix"
          :rows="matrixRows"
          :coverage="matrix.coverage"
          :health-mode="healthMode"
          :show-throughput="showThroughput"
          :rates-by-group-id="ratesByGroupId"
          :countdown-seconds="countdownSeconds"
        />
        <div
          v-else-if="loading"
          class="card flex min-h-[320px] items-center justify-center !rounded-3xl !border-0 text-sm text-gray-400 shadow-sm ring-1 ring-gray-900/5 dark:ring-dark-700"
        >
          <span class="animate-pulse">{{ t('common.loading') }}</span>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import RelayPulseMatrix from '@/features/channel-monitor-v2/RelayPulseMatrix.vue'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { isChannelMonitorThroughputHidden } from '@/utils/featureFlags'
import * as api from '@/api/channelMonitorV2'
import { userGroupsAPI } from '@/api/groups'
import type {
  MonitorFilter,
  MonitorMatrixGroupBy,
  MonitorMatrixResponse,
  MonitorRange,
  MonitorSnapshot,
} from '@/api/channelMonitorV2'
import { formatMonitorPercent } from '@/features/channel-monitor-v2/monitorFormat'

type HealthMode = 'overall' | 'success' | 'ttft' | 'cache'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const appStore = useAppStore()
const { t, locale } = useI18n()
const isAdmin = computed(() => authStore.isAdmin)
const showThroughput = computed(() => isAdmin.value || !isChannelMonitorThroughputHidden())

const ranges = computed(() => [
  { value: '90m' as MonitorRange, label: t('channelMonitorV2.ranges.90m') },
  { value: '24h' as MonitorRange, label: t('channelMonitorV2.ranges.24h') },
  { value: '7d' as MonitorRange, label: t('channelMonitorV2.ranges.7d') },
  { value: '30d' as MonitorRange, label: t('channelMonitorV2.ranges.30d') },
])

const filter = ref<MonitorFilter>({
  range: parseRange(route.query.range),
  platforms: csv(route.query.platform),
  groupIds: csv(route.query.group).map(Number).filter(Boolean),
  models: csv(route.query.model),
})
const matrixGroupBy = ref<MonitorMatrixGroupBy>('platform_group')
const healthMode = ref<HealthMode>('overall')
const snapshot = ref<MonitorSnapshot | null>(null)
const matrix = ref<MonitorMatrixResponse | null>(null)
const ratesByGroupId = ref<Record<number, number>>({})
const loading = ref(false)
const refreshing = ref(false)
const countdownSeconds = ref(0)
let controller: AbortController | null = null
let sequence = 0
let autoRefreshTimer: number | null = null
let countdownTimer: number | null = null

/** First-upgrade backfill toward 90m/24h/7d/30d; banner hides when backend omits bootstrap. */
const bootstrapActive = computed(() => Boolean(snapshot.value?.coverage?.bootstrap?.active))
const bootstrapPercent = computed(() => {
  const raw = snapshot.value?.coverage?.bootstrap?.progress_percent
  if (typeof raw !== 'number' || Number.isNaN(raw)) return 0
  return Math.min(100, Math.max(0, Math.round(raw)))
})
const matrixRows = computed(() => {
  const items = matrix.value?.items || []
  return items.filter((row) => row.group_id != null && Number(row.group_id) > 0)
})

function csv(value: unknown) {
  return typeof value === 'string' ? value.split(',').filter(Boolean) : []
}
function parseRange(value: unknown): MonitorRange {
  return ['90m', '24h', '7d', '30d'].includes(String(value)) ? (value as MonitorRange) : '90m'
}
function syncQuery() {
  void router.replace({
    query: {
      range: filter.value.range,
    },
  })
}

async function loadGroupRates() {
  try {
    const [groups, userRates] = await Promise.all([
      userGroupsAPI.getAvailable(),
      userGroupsAPI.getUserGroupRates(),
    ])
    const next: Record<number, number> = {}
    for (const group of groups) {
      const custom = userRates[group.id]
      next[group.id] = custom != null ? custom : group.rate_multiplier
    }
    ratesByGroupId.value = next
  } catch {
    ratesByGroupId.value = {}
  }
}

async function loadMetrics(signal?: AbortSignal, id = sequence) {
  const [nextSnapshot, nextMatrix] = await Promise.all([
    api.getSnapshot(filter.value, isAdmin.value, signal),
    api.getMatrix(filter.value, matrixGroupBy.value, isAdmin.value, signal),
  ])
  if (id !== sequence) return
  snapshot.value = nextSnapshot
  matrix.value = nextMatrix
  scheduleAutoRefresh()
}

async function reload(silent = true) {
  controller?.abort()
  const request = new AbortController()
  controller = request
  const id = ++sequence
  refreshing.value = true
  if (!silent) loading.value = true
  try {
    await loadMetrics(request.signal, id)
  } catch (error) {
    if ((error as { name?: string }).name !== 'CanceledError') {
      appStore.showError(extractApiErrorMessage(error, t('channelMonitorV2.loadFailed')))
    }
  } finally {
    if (id === sequence) {
      loading.value = false
      refreshing.value = false
    }
  }
}

function setRange(value: MonitorRange) {
  filter.value.range = value
}

function refreshIntervalSeconds() {
  if (bootstrapActive.value) return 10
  return snapshot.value?.config?.refresh_interval_seconds || 60
}

function scheduleAutoRefresh() {
  if (autoRefreshTimer) {
    window.clearInterval(autoRefreshTimer)
    autoRefreshTimer = null
  }
  if (countdownTimer) {
    window.clearInterval(countdownTimer)
    countdownTimer = null
  }
  const seconds = Math.max(bootstrapActive.value ? 10 : 60, refreshIntervalSeconds())
  countdownSeconds.value = seconds
  countdownTimer = window.setInterval(() => {
    if (countdownSeconds.value > 0) countdownSeconds.value -= 1
  }, 1000)
  autoRefreshTimer = window.setInterval(() => {
    if (!loading.value && !refreshing.value) {
      void reload(true)
    }
  }, seconds * 1000)
}

function formatPercent(value: number) {
  return formatMonitorPercent(value)
}
function formatTime(value: string) {
  return new Intl.DateTimeFormat(locale.value || undefined, {
    year: 'numeric',
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(new Date(value))
}

watch(
  () => filter.value.range,
  () => {
    syncQuery()
    void reload(true)
  },
)
onMounted(() => {
  void loadGroupRates()
  void reload(false)
})
onBeforeUnmount(() => {
  controller?.abort()
  if (autoRefreshTimer) window.clearInterval(autoRefreshTimer)
  if (countdownTimer) window.clearInterval(countdownTimer)
})
</script>
