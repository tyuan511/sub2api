<template>
  <div class="flex items-center gap-1">
    <button
      @click="$emit('run', row)"
      :disabled="running"
      class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700 dark:hover:text-primary-400"
    >
      <Icon name="refresh" size="sm" :class="running ? 'animate-spin' : ''" />
      <span class="text-xs">{{ t('admin.channelMonitor.runNow') }}</span>
    </button>
    <button
      v-if="canBazaarLinkProbe"
      data-testid="monitor-bazaarlink-probe"
      :title="t('admin.channelMonitor.bazaarLinkProbe.tooltip')"
      @click="$emit('bazaarlink-probe', row)"
      :disabled="probing"
      class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-violet-500 transition-colors hover:bg-violet-50 hover:text-violet-700 disabled:cursor-not-allowed disabled:opacity-50 dark:hover:bg-violet-900/20 dark:hover:text-violet-300"
    >
      <Icon name="shield" size="sm" :class="probing ? 'animate-pulse' : ''" />
      <span class="text-xs">{{ probing ? t('admin.channelMonitor.bazaarLinkProbe.running') : t('admin.channelMonitor.bazaarLinkProbe.action') }}</span>
    </button>
    <button
      data-testid="monitor-duplicate"
      :title="duplicateTitle"
      :disabled="duplicating || Boolean(row.api_key_decrypt_failed)"
      @click="$emit('duplicate', row)"
      class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 disabled:cursor-not-allowed disabled:opacity-50 dark:hover:bg-dark-700 dark:hover:text-primary-400"
    >
      <Icon name="copy" size="sm" />
      <span class="text-xs">
        {{ duplicating ? t('admin.channelMonitor.duplicating') : t('admin.channelMonitor.duplicate') }}
      </span>
    </button>
    <button
      @click="$emit('edit', row)"
      class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700 dark:hover:text-primary-400"
    >
      <Icon name="edit" size="sm" />
      <span class="text-xs">{{ t('common.edit') }}</span>
    </button>
    <button
      @click="$emit('delete', row)"
      class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
    >
      <Icon name="trash" size="sm" />
      <span class="text-xs">{{ t('common.delete') }}</span>
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ChannelMonitor } from '@/api/admin/channelMonitor'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'

const props = defineProps<{
  row: ChannelMonitor
  running: boolean
  duplicating: boolean
  probing: boolean
}>()

defineEmits<{
  (e: 'run', row: ChannelMonitor): void
  (e: 'bazaarlink-probe', row: ChannelMonitor): void
  (e: 'duplicate', row: ChannelMonitor): void
  (e: 'edit', row: ChannelMonitor): void
  (e: 'delete', row: ChannelMonitor): void
}>()

const { t } = useI18n()
const appStore = useAppStore()
const duplicateTitle = computed(() => {
  if (props.row.api_key_decrypt_failed) return t('admin.channelMonitor.duplicateKeyUnavailable')
  if (props.duplicating) return t('admin.channelMonitor.duplicating')
  return t('admin.channelMonitor.duplicate')
})
const canBazaarLinkProbe = computed(() =>
  appStore.cachedPublicSettings?.channel_monitor_bazaarlink_probe_enabled !== false &&
  !props.row.api_key_decrypt_failed &&
  !!props.row.endpoint?.trim() &&
  !!props.row.primary_model?.trim() &&
  props.row.primary_model.toLowerCase() !== 'quota' &&
  ['openai', 'anthropic', 'grok', 'kimi', 'zhipu', 'deepseek'].includes(props.row.provider),
)
</script>
