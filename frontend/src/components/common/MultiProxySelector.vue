<template>
  <div class="flex flex-col gap-2">
    <div class="relative" ref="containerRef">
      <button
        type="button"
        @click="toggle"
        :disabled="disabled"
        :class="[
          'select-trigger',
          isOpen && 'select-trigger-open',
          disabled && 'select-trigger-disabled'
        ]"
      >
        <span class="select-value">{{ triggerLabel }}</span>
        <span class="select-icon">
          <Icon
            name="chevronDown"
            size="md"
            :class="['transition-transform duration-200', isOpen && 'rotate-180']"
          />
        </span>
      </button>

      <Transition name="select-dropdown">
        <div v-if="isOpen" class="select-dropdown">
          <div class="select-header">
            <div class="select-search">
              <Icon name="search" size="sm" class="text-gray-400" />
              <input
                ref="searchInputRef"
                v-model="searchQuery"
                type="text"
                :placeholder="t('admin.proxies.searchProxies')"
                class="select-search-input"
                @click.stop
              />
            </div>
            <button
              v-if="modelValue.length > 0"
              type="button"
              class="clear-btn"
              @click.stop="clearAll"
            >
              {{ t('admin.accounts.proxyPool.clear') }}
            </button>
          </div>

          <div class="select-options">
            <div
              v-for="proxy in filteredProxies"
              :key="proxy.id"
              @click="toggleProxy(proxy.id)"
              :class="['select-option', isSelected(proxy.id) && 'select-option-selected']"
            >
              <div class="min-w-0 flex-1">
                <div class="flex items-center gap-2">
                  <span class="truncate font-medium">{{ proxy.name }}</span>
                  <span
                    v-if="proxy.account_count !== undefined"
                    class="inline-flex flex-shrink-0 items-center rounded bg-gray-100 px-1.5 py-0.5 text-xs text-gray-600 dark:bg-dark-600 dark:text-gray-400"
                  >
                    {{ proxy.account_count }}
                  </span>
                  <span
                    v-if="proxy.status && proxy.status !== 'active'"
                    class="inline-flex flex-shrink-0 items-center rounded bg-red-100 px-1.5 py-0.5 text-xs text-red-700 dark:bg-red-900/30 dark:text-red-400"
                  >
                    {{ proxy.status }}
                  </span>
                </div>
                <div class="truncate text-xs text-gray-500 dark:text-gray-400">
                  {{ proxy.protocol }}://{{ proxy.host }}:{{ proxy.port }}
                </div>
              </div>
              <Icon
                v-if="isSelected(proxy.id)"
                name="check"
                size="sm"
                class="flex-shrink-0 text-primary-500"
              />
            </div>

            <div v-if="filteredProxies.length === 0" class="select-empty">
              {{ t('common.noOptionsFound') }}
            </div>
          </div>
        </div>
      </Transition>
    </div>

    <!-- Selected proxies with per-proxy concurrency -->
    <div v-if="modelValue.length > 0" class="proxy-pool">
      <div
        v-for="(binding, index) in modelValue"
        :key="binding.proxy_id"
        class="proxy-pool-row"
      >
        <span class="proxy-pool-index">{{ index + 1 }}</span>
        <div class="min-w-0 flex-1">
          <div class="truncate text-sm font-medium text-gray-900 dark:text-gray-100">
            {{ proxyLabel(binding.proxy_id) }}
            <span
              v-if="isPool && index === 0"
              class="ml-1 rounded bg-primary-50 px-1.5 py-0.5 text-xs text-primary-600 dark:bg-primary-900/20 dark:text-primary-300"
            >
              {{ t('admin.accounts.proxyPool.primary') }}
            </span>
          </div>
          <div class="truncate text-xs text-gray-500 dark:text-gray-400">
            {{ proxyEndpoint(binding.proxy_id) }}
          </div>
        </div>
        <label v-if="isPool" class="proxy-pool-concurrency">
          <span class="text-xs text-gray-500 dark:text-gray-400">{{
            t('admin.accounts.concurrency')
          }}</span>
          <input
            :value="binding.concurrency"
            type="number"
            min="1"
            :disabled="disabled"
            class="input h-8 w-20 py-1 text-sm"
            @input="updateConcurrency(binding.proxy_id, $event)"
          />
        </label>
        <button
          type="button"
          class="remove-btn"
          :disabled="disabled"
          :title="t('admin.accounts.proxyPool.remove')"
          @click="removeProxy(binding.proxy_id)"
        >
          <Icon name="x" size="sm" />
        </button>
      </div>
      <p v-if="isPool" class="proxy-pool-total">
        {{ t('admin.accounts.proxyPool.total', { count: modelValue.length, concurrency: totalConcurrency }) }}
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { AccountProxyBinding, Proxy } from '@/types'

const { t } = useI18n()

interface Props {
  modelValue: AccountProxyBinding[]
  proxies: Proxy[]
  disabled?: boolean
  defaultConcurrency?: number
}

const props = withDefaults(defineProps<Props>(), {
  disabled: false,
  defaultConcurrency: 3
})

const emit = defineEmits<{
  'update:modelValue': [value: AccountProxyBinding[]]
}>()

const isOpen = ref(false)
const searchQuery = ref('')
const containerRef = ref<HTMLElement | null>(null)
const searchInputRef = ref<HTMLInputElement | null>(null)

const proxyById = computed(() => {
  const map = new Map<number, Proxy>()
  for (const proxy of props.proxies) map.set(proxy.id, proxy)
  return map
})

// 只有 ≥2 个代理才构成代理池；单个代理沿用账号自身的并发数，不显示每代理并发。
const isPool = computed(() => props.modelValue.length >= 2)

const totalConcurrency = computed(() =>
  props.modelValue.reduce((sum, item) => sum + Math.max(1, item.concurrency || 1), 0)
)

const triggerLabel = computed(() => {
  if (props.modelValue.length === 0) return t('admin.accounts.noProxy')
  if (props.modelValue.length === 1) return proxyLabel(props.modelValue[0].proxy_id)
  return t('admin.accounts.proxyPool.selected', { count: props.modelValue.length })
})

const filteredProxies = computed(() => {
  if (!searchQuery.value) return props.proxies
  const query = searchQuery.value.toLowerCase()
  return props.proxies.filter(
    (proxy) => proxy.name.toLowerCase().includes(query) || proxy.host.toLowerCase().includes(query)
  )
})

function proxyLabel(proxyId: number): string {
  return proxyById.value.get(proxyId)?.name || `#${proxyId}`
}

function proxyEndpoint(proxyId: number): string {
  const proxy = proxyById.value.get(proxyId)
  if (!proxy) return ''
  return `${proxy.protocol}://${proxy.host}:${proxy.port}`
}

function isSelected(proxyId: number): boolean {
  return props.modelValue.some((item) => item.proxy_id === proxyId)
}

function toggle() {
  if (props.disabled) return
  isOpen.value = !isOpen.value
  if (isOpen.value) {
    nextTick(() => searchInputRef.value?.focus())
  }
}

function toggleProxy(proxyId: number) {
  if (props.disabled) return
  if (isSelected(proxyId)) {
    removeProxy(proxyId)
    return
  }
  // 新加入的代理默认沿用账号当前并发（由父组件通过 defaultConcurrency 传入），
  // 避免把原来的账号并发静默改成固定默认值。
  emit('update:modelValue', [
    ...props.modelValue,
    { proxy_id: proxyId, concurrency: Math.max(1, Math.floor(props.defaultConcurrency) || 1) }
  ])
}

function removeProxy(proxyId: number) {
  emit(
    'update:modelValue',
    props.modelValue.filter((item) => item.proxy_id !== proxyId)
  )
}

function clearAll() {
  emit('update:modelValue', [])
}

function updateConcurrency(proxyId: number, event: Event) {
  const raw = Number((event.target as HTMLInputElement).value)
  const concurrency = Number.isFinite(raw) && raw >= 1 ? Math.floor(raw) : 1
  emit(
    'update:modelValue',
    props.modelValue.map((item) => (item.proxy_id === proxyId ? { ...item, concurrency } : item))
  )
}

const handleClickOutside = (event: MouseEvent) => {
  if (containerRef.value && !containerRef.value.contains(event.target as Node)) {
    isOpen.value = false
    searchQuery.value = ''
  }
}

const handleEscape = (event: KeyboardEvent) => {
  if (event.key === 'Escape' && isOpen.value) {
    isOpen.value = false
    searchQuery.value = ''
  }
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  document.addEventListener('keydown', handleEscape)
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
  document.removeEventListener('keydown', handleEscape)
})
</script>

<style scoped>
.select-trigger {
  @apply flex w-full items-center justify-between gap-2;
  @apply rounded-xl px-4 py-2.5 text-sm;
  @apply bg-white dark:bg-dark-800;
  @apply border border-gray-200 dark:border-dark-600;
  @apply text-gray-900 dark:text-gray-100;
  @apply transition-all duration-200;
  @apply focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/30;
  @apply hover:border-gray-300 dark:hover:border-dark-500;
  @apply cursor-pointer;
}

.select-trigger-open {
  @apply border-primary-500 ring-2 ring-primary-500/30;
}

.select-trigger-disabled {
  @apply cursor-not-allowed bg-gray-100 opacity-60 dark:bg-dark-900;
}

.select-value {
  @apply flex-1 truncate text-left;
}

.select-icon {
  @apply flex-shrink-0 text-gray-400 dark:text-dark-400;
}

.select-dropdown {
  @apply absolute z-[100] mt-2 w-full;
  @apply bg-white dark:bg-dark-800;
  @apply rounded-xl;
  @apply border border-gray-200 dark:border-dark-700;
  @apply shadow-lg shadow-black/10 dark:shadow-black/30;
  @apply overflow-hidden;
}

.select-header {
  @apply flex items-center gap-2 px-3 py-2;
  @apply border-b border-gray-100 dark:border-dark-700;
}

.select-search {
  @apply flex flex-1 items-center gap-2;
}

.select-search-input {
  @apply flex-1 bg-transparent text-sm;
  @apply text-gray-900 dark:text-gray-100;
  @apply placeholder:text-gray-400 dark:placeholder:text-dark-400;
  @apply focus:outline-none;
}

.clear-btn {
  @apply flex-shrink-0 rounded-lg px-2 py-1 text-xs;
  @apply text-gray-500 hover:text-red-600 dark:hover:text-red-400;
  @apply hover:bg-red-50 dark:hover:bg-red-900/20;
  @apply transition-colors;
}

.select-options {
  @apply max-h-60 overflow-y-auto py-1;
}

.select-option {
  @apply flex items-center justify-between gap-2;
  @apply px-4 py-2.5 text-sm;
  @apply text-gray-700 dark:text-gray-300;
  @apply cursor-pointer transition-colors duration-150;
  @apply hover:bg-gray-50 dark:hover:bg-dark-700;
}

.select-option-selected {
  @apply bg-primary-50 dark:bg-primary-900/20;
  @apply text-primary-700 dark:text-primary-300;
}

.select-empty {
  @apply px-4 py-8 text-center text-sm;
  @apply text-gray-500 dark:text-dark-400;
}

.proxy-pool {
  @apply flex flex-col gap-2 rounded-xl border border-gray-200 p-2 dark:border-dark-600;
}

.proxy-pool-row {
  @apply flex items-center gap-3 rounded-lg px-2 py-1.5;
  @apply bg-gray-50 dark:bg-dark-700/50;
}

.proxy-pool-index {
  @apply flex h-5 w-5 flex-shrink-0 items-center justify-center rounded-full;
  @apply bg-gray-200 text-xs text-gray-600 dark:bg-dark-600 dark:text-gray-300;
}

.proxy-pool-concurrency {
  @apply flex flex-shrink-0 items-center gap-1.5;
}

.proxy-pool-total {
  @apply px-2 text-xs text-gray-500 dark:text-gray-400;
}

.remove-btn {
  @apply flex-shrink-0 rounded p-1;
  @apply text-gray-400 hover:text-red-600 dark:hover:text-red-400;
  @apply hover:bg-red-50 dark:hover:bg-red-900/20;
  @apply transition-colors disabled:cursor-not-allowed disabled:opacity-50;
}

.select-dropdown-enter-active,
.select-dropdown-leave-active {
  transition: all 0.2s ease;
}

.select-dropdown-enter-from,
.select-dropdown-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>
