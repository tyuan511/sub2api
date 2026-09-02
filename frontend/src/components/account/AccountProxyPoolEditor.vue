<template>
  <div class="space-y-2" data-testid="account-proxy-pool-editor">
    <div
      v-for="(row, index) in localRows"
      :key="index"
      class="flex items-center gap-2"
    >
      <div class="min-w-0 flex-1">
        <ProxySelector
          v-model="row.proxy_id"
          :proxies="proxies"
          @update:model-value="publish"
        />
      </div>
      <div class="w-32 shrink-0">
        <input
          v-model.number="row.concurrency"
          type="number"
          min="0"
          class="input"
          :placeholder="t('admin.accounts.proxyPool.concurrencyPlaceholder')"
          :aria-label="t('admin.accounts.proxyPool.concurrency')"
          @input="row.concurrency = Math.max(0, row.concurrency || 0); publish()"
        />
      </div>
      <button
        type="button"
        class="rounded-md p-2 text-gray-500 hover:bg-gray-100 hover:text-red-600 dark:hover:bg-dark-600"
        :aria-label="t('admin.accounts.proxyPool.remove')"
        @click="removeRow(index)"
      >
        <Icon name="trash" size="sm" />
      </button>
    </div>

    <div v-if="localRows.length === 0" class="text-xs text-gray-500 dark:text-gray-400">
      {{ t('admin.accounts.proxyPool.empty') }}
    </div>
    <button
      type="button"
      class="inline-flex items-center gap-1 rounded-md border border-dashed border-gray-300 px-3 py-1.5 text-sm text-gray-600 hover:border-primary-400 hover:text-primary-600 dark:border-dark-500 dark:text-gray-300"
      @click="addRow"
    >
      <Icon name="plus" size="sm" />
      {{ t('admin.accounts.proxyPool.add') }}
    </button>
    <p class="text-xs text-gray-500 dark:text-gray-400">
      {{ t('admin.accounts.proxyPool.hint') }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Proxy, AccountProxyConfig } from '@/types'
import Icon from '@/components/icons/Icon.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'

type ProxyPoolRow = Pick<AccountProxyConfig, 'proxy_id' | 'concurrency'>

const props = defineProps<{
  proxies: Proxy[]
  modelValue: ProxyPoolRow[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: ProxyPoolRow[]]
}>()

const { t } = useI18n()
const localRows = ref<ProxyPoolRow[]>([])

const cloneRows = (rows: ProxyPoolRow[] | undefined) =>
  (rows || []).map((row) => ({
    proxy_id: row.proxy_id ?? null,
    concurrency: Number.isFinite(row.concurrency) ? Math.max(0, row.concurrency) : 3
  }))

watch(
  () => props.modelValue,
  (value) => {
    localRows.value = cloneRows(value)
  },
  { immediate: true, deep: true }
)

const publish = () => emit('update:modelValue', cloneRows(localRows.value))

const addRow = () => {
  localRows.value.push({ proxy_id: null, concurrency: 3 })
  publish()
}

const removeRow = (index: number) => {
  localRows.value.splice(index, 1)
  publish()
}

</script>
