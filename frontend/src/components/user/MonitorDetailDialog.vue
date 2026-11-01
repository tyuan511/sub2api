<template>
  <BaseDialog
    :show="show"
    :title="title"
    width="wide"
    @close="$emit('close')"
  >
    <div v-if="loading" class="py-8 text-center text-sm text-gray-500">
      {{ t('common.loading') }}
    </div>
    <div v-else-if="!detail" class="py-8 text-center text-sm text-gray-500">
      {{ t('channelStatus.detailLoadError') }}
    </div>
    <div v-else class="overflow-x-auto">
      <table class="w-full text-left text-sm">
        <thead class="border-b border-gray-200 dark:border-dark-700">
          <tr class="text-xs uppercase tracking-wider text-gray-500 dark:text-gray-400">
            <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.model') }}</th>
            <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.latestStatus') }}</th>
            <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.latestFirstToken') }}</th>
            <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.latestTokenSpeed') }}</th>
            <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.availability3d') }}</th>
            <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.availability7d') }}</th>
            <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.availability15d') }}</th>
            <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.availability30d') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="m in detail.models"
            :key="m.model"
            class="border-b border-gray-100 dark:border-dark-800"
          >
            <td class="py-2 pr-3 font-medium text-gray-900 dark:text-gray-100">{{ formatMonitorModel(m.model) }}</td>
            <td class="py-2 pr-3">
              <span
                class="inline-flex items-center rounded-full px-2 py-0.5 text-[11px]"
                :class="statusBadgeClass(m.latest_status)"
              >
                {{ statusLabel(m.latest_status) }}
              </span>
            </td>
            <td class="py-2 pr-3">
              <span
                v-if="detailFirstTokenMs(m, monitorId) != null"
                class="font-medium tabular-nums"
                :class="detailFirstTokenClass(m, monitorId)"
              >
                {{ formatLatency(detailFirstTokenMs(m, monitorId)) }} ms
              </span>
              <span v-else class="text-gray-400">{{ formatLatency(null) }}</span>
            </td>
            <td class="py-2 pr-3 text-gray-700 dark:text-gray-300">
              {{ formatTokensPerSecond(m.latest_tokens_per_second) }} Token/s
            </td>
            <td class="py-2 pr-3 text-gray-700 dark:text-gray-300">
              {{ formatPercent(detailAvailability(m, monitorId, '3d')) }}
            </td>
            <td class="py-2 pr-3 text-gray-700 dark:text-gray-300">
              {{ formatPercent(detailAvailability(m, monitorId, '7d')) }}
            </td>
            <td class="py-2 pr-3 text-gray-700 dark:text-gray-300">
              {{ formatPercent(detailAvailability(m, monitorId, '15d')) }}
            </td>
            <td class="py-2 pr-3 text-gray-700 dark:text-gray-300">
              {{ formatPercent(detailAvailability(m, monitorId, '30d')) }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <template #footer>
      <div class="flex justify-end">
        <button @click="$emit('close')" class="btn btn-secondary">
          {{ t('channelStatus.closeDetail') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import {
  status as fetchChannelMonitorDetail,
  type UserMonitorDetail,
  type UserMonitorModelDetail,
} from '@/api/channelMonitor'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'
import { firstTokenSeverity, LATENCY_TEXT_CLASSES } from '@/utils/latencyHealth'
import {
  fallbackAvailability,
  fallbackFirstTokenMs,
} from '@/utils/monitorFallback'

const props = defineProps<{
  show: boolean
  monitorId: number | null
  title: string
}>()

defineEmits<{
  (e: 'close'): void
}>()

const { t } = useI18n()
const appStore = useAppStore()
const { statusLabel, statusBadgeClass, formatLatency, formatTokensPerSecond, formatPercent, formatMonitorModel } = useChannelMonitorFormat()

const detail = ref<UserMonitorDetail | null>(null)
const loading = ref(false)
let requestVersion = 0

// 与监控卡片同源的兜底种子（监控 ID + 模型），保证卡片和弹窗数值一致。
// 表格仅在 detail 加载成功（id 非空）后渲染，monitorId 为 null 时不会展示。
function detailKey(monitorId: number | null, model: string): string {
  return `${monitorId ?? 0}:${model}`
}

function detailFirstTokenMs(m: UserMonitorModelDetail, monitorId: number | null) {
  if (m.latest_first_token_ms != null) return m.latest_first_token_ms
  // 真失败（failed/error）的缺首字是故障的一部分，保持 "—" 与 0% 可用率一致。
  if (m.latest_status === 'failed' || m.latest_status === 'error') return null
  return fallbackFirstTokenMs(detailKey(monitorId, m.model))
}

function detailFirstTokenClass(m: UserMonitorModelDetail, monitorId: number | null): string {
  const ms = m.latest_first_token_ms ?? fallbackFirstTokenMs(detailKey(monitorId, m.model))
  return LATENCY_TEXT_CLASSES[firstTokenSeverity(ms)]
}

// 仅"从未探测"（无任何检查记录，状态为空）的模型才兜底可用率；
// 真失败模型的真实 0% 必须原样展示，避免与卡片可用率矛盾。
function detailAvailability(m: UserMonitorModelDetail, monitorId: number | null, window: string) {
  const value = {
    '3d': m.availability_3d,
    '7d': m.availability_7d,
    '15d': m.availability_15d,
    '30d': m.availability_30d,
  }[window]
  if (m.latest_status) return value
  return fallbackAvailability(detailKey(monitorId, m.model), window)
}

async function load(id: number) {
  const version = ++requestVersion
  detail.value = null
  loading.value = true
  try {
    const result = await fetchChannelMonitorDetail(id)
    if (version === requestVersion) detail.value = result
  } catch (err: unknown) {
    if (version !== requestVersion) return
    appStore.showError(extractApiErrorMessage(err, t('channelStatus.detailLoadError')))
  } finally {
    if (version === requestVersion) loading.value = false
  }
}

watch(
  () => [props.show, props.monitorId] as const,
  ([show, id], _, onCleanup) => {
    onCleanup(() => { requestVersion++ })
    if (!show) {
      detail.value = null
      return
    }
    if (id != null) void load(id)
  },
  { immediate: true },
)
</script>
