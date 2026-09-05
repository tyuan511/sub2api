<template>
  <section class="card" data-test="routing-rollout-card">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <div class="flex items-center justify-between gap-3">
        <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.settings.routingRollout.title') }}</h2>
        <span class="rounded-full bg-primary-50 px-2.5 py-1 text-xs font-medium text-primary-700 dark:bg-primary-900/20 dark:text-primary-300">{{ t('admin.settings.routingRollout.count', { count: selectedIds.length }) }}</span>
      </div>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.routingRollout.description') }}</p>
    </div>
    <div class="space-y-4 p-6">
      <p v-if="loading" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
      <div v-else-if="loadFailed" class="flex items-center justify-between gap-3 text-sm text-red-600">
        <span>{{ t('admin.settings.routingRollout.loadFailed') }}</span>
        <button type="button" class="btn btn-secondary" @click="load">{{ t('common.retry') }}</button>
      </div>
      <template v-else>
        <label for="routing-rollout-search" class="input-label">{{ t('admin.settings.routingRollout.searchLabel') }}</label>
        <input id="routing-rollout-search" v-model="search" type="search" class="input" :disabled="saving"
          :placeholder="t('admin.settings.routingRollout.searchPlaceholder')" data-test="rollout-search" />
        <div class="max-h-52 overflow-y-auto rounded-xl border border-gray-200 dark:border-dark-600" aria-live="polite">
          <p v-if="searching" class="px-4 py-3 text-sm text-gray-500">{{ t('common.loading') }}</p>
          <p v-else-if="searchFailed" class="px-4 py-3 text-sm text-red-600">{{ t('admin.settings.routingRollout.searchFailed') }}</p>
          <p v-else-if="!results.length" class="px-4 py-3 text-sm text-gray-500">{{ t('admin.settings.routingRollout.noUsers') }}</p>
          <button v-for="user in results" v-else :key="user.id" type="button" :disabled="saving"
            :aria-pressed="selectedIds.includes(user.id)" :data-test="`rollout-user-${user.id}`" @click="toggleUser(user.id)"
            class="flex w-full items-center justify-between gap-3 border-b border-gray-100 px-4 py-3 text-left last:border-0 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-700"
            :class="selectedIds.includes(user.id) ? 'bg-primary-50/60 dark:bg-primary-900/10' : ''">
            <span class="min-w-0"><span class="block truncate text-sm font-medium text-gray-900 dark:text-white">{{ user.username || user.email }}</span>
              <span class="block truncate text-xs text-gray-500">#{{ user.id }} · {{ user.email }}</span></span>
            <span class="shrink-0 text-sm text-primary-600">{{ selectedIds.includes(user.id) ? '✓' : '+' }}</span>
          </button>
        </div>
        <div class="flex items-center justify-between gap-2">
          <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.settings.routingRollout.selected') }}</span>
          <button v-if="selectedIds.length" type="button" class="text-xs text-gray-500 hover:text-red-600" :disabled="saving" data-test="rollout-clear" @click="selectedIds = []">{{ t('admin.settings.routingRollout.clear') }}</button>
        </div>
        <div v-if="selectedIds.length" class="flex max-h-40 flex-wrap gap-2 overflow-y-auto">
          <button v-for="id in selectedIds" :key="id" type="button" :disabled="saving" :data-test="`rollout-remove-${id}`"
            :aria-label="t('admin.settings.routingRollout.remove', { id })" @click="toggleUser(id)"
            class="rounded-full border border-primary-200 bg-primary-50 px-3 py-1 text-xs font-medium text-primary-700 dark:border-primary-800 dark:bg-primary-900/20 dark:text-primary-300">#{{ id }} <span aria-hidden="true" class="ml-1">×</span></button>
        </div>
        <p v-else class="rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-300" data-test="rollout-empty">{{ t('admin.settings.routingRollout.empty') }}</p>
        <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('admin.settings.routingRollout.withdrawalHint') }}</p>
        <div class="flex justify-end">
          <button type="button" class="btn btn-primary" :disabled="saving" data-test="rollout-save" @click="save">{{ saving ? t('common.saving') : t('common.save') }}</button>
        </div>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { AdminUser } from '@/types'

const { t } = useI18n()
const app = useAppStore()
const selectedIds = ref<number[]>([])
const results = ref<AdminUser[]>([])
const search = ref('')
const loading = ref(true)
const loadFailed = ref(false)
const saving = ref(false)
const searching = ref(false)
const searchFailed = ref(false)
let timer: ReturnType<typeof setTimeout> | undefined
let searchVersion = 0

async function searchUsers(version: number) {
  searching.value = true
  searchFailed.value = false
  const query = search.value.trim()
  try {
    let users: AdminUser[]
    if (/^\d+$/.test(query)) {
      const id = Number(query)
      users = Number.isSafeInteger(id) && id > 0 ? [await adminAPI.users.getById(id)] : []
    } else {
      users = (await adminAPI.users.list(1, 20, { search: query, include_subscriptions: false })).items
    }
    if (version === searchVersion) results.value = users
  } catch (error: unknown) {
    if (version === searchVersion) {
      results.value = []
      searchFailed.value = (error as { response?: { status?: number } }).response?.status !== 404
    }
  } finally {
    if (version === searchVersion) searching.value = false
  }
}

watch(search, () => {
  const version = ++searchVersion
  clearTimeout(timer)
  results.value = []
  searching.value = true
  timer = setTimeout(() => void searchUsers(version), 250)
})

function toggleUser(id: number) {
  if (selectedIds.value.includes(id)) selectedIds.value = selectedIds.value.filter(value => value !== id)
  else if (selectedIds.value.length < 1000) selectedIds.value = [...selectedIds.value, id].sort((a, b) => a - b)
  else app.showError(t('admin.settings.routingRollout.limit'))
}

async function load() {
  loading.value = true
  loadFailed.value = false
  try {
    const settings = await adminAPI.settings.getAPIKeyRoutingRollout()
    selectedIds.value = settings.user_ids
    void searchUsers(++searchVersion)
  } catch { loadFailed.value = true }
  finally { loading.value = false }
}

async function save() {
  saving.value = true
  try {
    const result = await adminAPI.settings.updateAPIKeyRoutingRollout({ user_ids: selectedIds.value })
    selectedIds.value = result.user_ids
    app.showSuccess(t('admin.settings.routingRollout.saved'))
  } catch { app.showError(t('admin.settings.routingRollout.saveFailed')) }
  finally { saving.value = false }
}

onMounted(load)
onUnmounted(() => { clearTimeout(timer); searchVersion++ })
</script>
