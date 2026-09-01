<template>
  <BaseDialog
    :show="show"
    :title="t('admin.channelMonitor.runResultTitle')"
    width="normal"
    @close="$emit('close')"
  >
    <div class="space-y-2">
      <div
        v-for="r in results"
        :key="r.model"
        class="grid grid-cols-1 items-center gap-x-4 gap-y-2 rounded-lg border border-gray-200 px-3 py-2 text-sm dark:border-dark-600 sm:grid-cols-[minmax(0,1fr)_auto]"
      >
        <div class="min-w-0">
          <span class="block break-words font-medium text-gray-900 dark:text-white">{{ formatMonitorModel(r.model) }}</span>
          <span v-if="r.message" class="text-xs text-gray-500 dark:text-gray-400">{{ r.message }}</span>
          <MonitorQuotaView :snapshot="r.quota" class="mt-1" />
        </div>
        <div class="flex min-w-0 flex-wrap items-center justify-start gap-x-4 gap-y-1 sm:justify-end">
          <span
            class="inline-flex shrink-0 items-center whitespace-nowrap rounded-full px-2 py-0.5 text-[11px]"
            :class="statusBadgeClass(r.status)"
          >
            {{ statusLabel(r.status) }}
          </span>
          <span
            v-if="r.first_token_ms != null"
            class="shrink-0 whitespace-nowrap text-xs font-medium tabular-nums"
            :class="LATENCY_TEXT_CLASSES[firstTokenSeverity(r.first_token_ms)]"
          >{{ t('monitorCommon.firstToken') }} {{ formatLatency(r.first_token_ms) }} ms</span>
          <span v-if="r.tokens_per_second != null" class="shrink-0 whitespace-nowrap text-xs text-gray-500 dark:text-gray-400">
            {{ t('monitorCommon.tokenSpeed') }} {{ formatTokensPerSecond(r.tokens_per_second) }} Token/s
          </span>
        </div>
      </div>
    </div>
    <template #footer>
      <div class="flex justify-end">
        <button @click="$emit('close')" class="btn btn-primary">
          {{ t('common.close') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { CheckResult } from '@/api/admin/channelMonitor'
import BaseDialog from '@/components/common/BaseDialog.vue'
import MonitorQuotaView from '@/components/common/MonitorQuotaView.vue'
import { useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'
import { firstTokenSeverity, LATENCY_TEXT_CLASSES } from '@/utils/latencyHealth'

defineProps<{
  show: boolean
  results: CheckResult[]
}>()

defineEmits<{
  (e: 'close'): void
}>()

const { t } = useI18n()
const { statusLabel, statusBadgeClass, formatLatency, formatTokensPerSecond, formatMonitorModel } = useChannelMonitorFormat()
</script>
