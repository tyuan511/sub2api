<template>
  <div class="space-y-2 rounded-xl border border-gray-200 p-4 dark:border-dark-600">
    <label :for="id" class="input-label">{{ t('admin.upstreams.selectUpstream') }}</label>
    <select :id="id" :value="selectedId ?? ''" class="input" :disabled="loading" @change="selectUpstream">
      <option value="">{{ t('admin.upstreams.manualMode') }}</option>
      <option v-for="upstream in upstreams" :key="upstream.id" :value="upstream.id">{{ upstream.name }} · {{ upstream.type }}</option>
    </select>
    <p v-if="failed" class="input-hint text-red-600">{{ t('admin.upstreams.loadFailed') }}</p>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { Upstream } from '@/api/admin/upstreams'

// Only the endpoint is linked. The account's API key remains manually entered.
defineProps<{ selectedId: number | null }>()
const emit = defineEmits<{ (event: 'select', upstream: Upstream | null): void }>()
const { t } = useI18n()
const id = `upstream-picker-${Math.random().toString(36).slice(2)}`
const upstreams = ref<Upstream[]>([])
const loading = ref(false)
const failed = ref(false)
let mounted = true
function selectUpstream(event: Event) {
  const value = (event.target as HTMLSelectElement).value
  emit('select', value === '' ? null : upstreams.value.find(u => u.id === Number(value)) || null)
}
onMounted(async () => {
  loading.value = true
  try { const result = await adminAPI.upstreams.list(); if (mounted) upstreams.value = result.filter(u => u.enabled) }
  catch { if (mounted) failed.value = true }
  finally { if (mounted) loading.value = false }
})
onUnmounted(() => { mounted = false })
</script>
