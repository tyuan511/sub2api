<template>
  <div class="space-y-3">
    <div ref="containerRef" class="relative">
      <button
        ref="triggerRef"
        type="button"
        class="input flex cursor-pointer items-center justify-between gap-2 text-left"
        :class="isOpen && 'border-primary-500 ring-2 ring-primary-500/30'"
        :aria-expanded="isOpen"
        aria-haspopup="listbox"
        aria-controls="route-group-options"
        data-test="route-group-trigger"
        @click="toggleDropdown"
        @keydown="handleTriggerKeydown"
      >
        <span
          class="min-w-0 flex-1 truncate"
          :class="!modelValue.length && 'text-gray-400 dark:text-dark-400'"
        >
          {{ triggerLabel }}
        </span>
        <Icon
          name="chevronDown"
          size="md"
          :class="['shrink-0 text-gray-400 transition-transform duration-200 dark:text-dark-400', isOpen && 'rotate-180']"
        />
      </button>

      <Transition name="route-dropdown">
        <div
          v-if="isOpen"
          id="route-group-options"
          class="absolute inset-x-0 top-full z-[70] mt-1.5 overflow-hidden rounded-xl border border-gray-200 bg-white shadow-lg shadow-black/10 dark:border-dark-600 dark:bg-dark-800 dark:shadow-black/30"
          role="listbox"
          aria-multiselectable="true"
          data-test="route-group-options"
        >
          <div class="flex items-center gap-2 border-b border-gray-100 px-3 py-2 dark:border-dark-700">
            <Icon name="search" size="sm" class="shrink-0 text-gray-400" />
            <input
              ref="searchInputRef"
              v-model="searchQuery"
              type="text"
              class="min-w-0 flex-1 bg-transparent text-sm text-gray-900 placeholder:text-gray-400 focus:outline-none dark:text-gray-100 dark:placeholder:text-dark-400"
              :placeholder="t('keys.searchGroup')"
              :aria-activedescendant="activeOptionID"
              @keydown="handleSearchKeydown"
            />
          </div>

          <div class="max-h-72 overflow-y-auto py-1">
            <button
              v-for="group in filteredGroups"
              :key="group.id"
              type="button"
              class="flex w-full items-center gap-3 px-3 py-2 text-left transition-colors"
              :class="[optionClass(group), highlightedGroup?.id === group.id && 'ring-2 ring-inset ring-primary-400']"
              :disabled="!canToggle(group)"
              :id="`route-group-option-${group.id}`"
              role="option"
              :aria-selected="isSelected(group.id)"
              tabindex="-1"
              :data-test="`route-group-option-${group.id}`"
              @click="toggleGroup(group)"
              @mouseenter="highlightGroup(group.id)"
            >
              <span
                class="flex h-5 w-5 flex-none items-center justify-center rounded border"
                :class="isSelected(group.id)
                  ? 'border-primary-500 bg-primary-500 text-white'
                  : 'border-gray-300 dark:border-dark-500'"
              >
                <Icon v-if="isSelected(group.id)" name="check" size="xs" :stroke-width="2.5" />
              </span>
              <span class="min-w-0 flex-1">
                <span class="flex flex-wrap items-center gap-1.5">
                  <span class="truncate text-sm font-medium">{{ group.name }}</span>
                  <span class="rounded bg-gray-100 px-1.5 py-0.5 text-[11px] text-gray-600 dark:bg-dark-600 dark:text-gray-300">
                    {{ group.platform }}
                  </span>
                  <span
                    v-if="group.status !== 'active'"
                    class="rounded bg-amber-100 px-1.5 py-0.5 text-[11px] text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
                  >
                    {{ t('keys.routeGroupUnavailable') }}
                  </span>
                </span>
                <span v-if="disabledReason(group)" class="mt-0.5 block text-xs text-amber-600 dark:text-amber-400">
                  {{ disabledReason(group) }}
                </span>
                <span v-else class="mt-0.5 block text-xs text-gray-500 dark:text-gray-400">
                  {{ t('keys.routeGroupRate', { rate: effectiveRate(group).toFixed(2) }) }}
                </span>
              </span>
            </button>

            <p v-if="filteredGroups.length === 0" class="px-3 py-5 text-center text-sm text-gray-400">
              {{ t('keys.noGroupFound') }}
            </p>
          </div>
        </div>
      </Transition>
    </div>

    <div v-if="modelValue.length" class="space-y-2" data-test="selected-route-groups">
      <div
        v-for="(groupId, index) in modelValue"
        :key="groupId"
        class="flex flex-wrap items-center gap-2 rounded-lg border border-gray-200 bg-gray-50 px-3 py-2 sm:flex-nowrap dark:border-dark-600 dark:bg-dark-800"
        :data-test="`selected-route-group-${groupId}`"
      >
        <span class="flex h-6 w-6 flex-none items-center justify-center rounded-full bg-primary-100 text-xs font-semibold text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
          {{ index + 1 }}
        </span>
        <div class="min-w-0 flex-1">
          <div class="truncate text-sm font-medium text-gray-900 dark:text-gray-100">
            {{ groupById.get(groupId)?.name || `#${groupId}` }}
          </div>
          <div class="text-xs text-gray-500 dark:text-gray-400">
            {{ index === 0 ? t('keys.routePrimaryGroup') : t('keys.routeFallbackGroup') }}
            <template v-if="groupById.get(groupId)">
              · {{ groupById.get(groupId)?.platform }}
              · {{ t('keys.routeGroupRate', { rate: effectiveRate(groupById.get(groupId)!).toFixed(2) }) }}
            </template>
          </div>
          <div
            v-if="routeDetail(groupId)"
            class="mt-1 flex flex-wrap gap-x-2 gap-y-1 text-[11px] text-gray-500 dark:text-gray-400"
            :data-test="`route-group-detail-${groupId}`"
          >
            <span v-if="routeDetail(groupId)?.current_rank">
              {{ t('keys.routeCurrentRank', { rank: routeDetail(groupId)?.current_rank }) }}
            </span>
            <span v-if="routeDetail(groupId)?.normalized_effective_rate !== undefined">
              {{ t('keys.routeEffectiveRate', { rate: routeDetail(groupId)!.normalized_effective_rate!.toFixed(2) }) }}
            </span>
            <span v-if="routeDetail(groupId)?.success_rate !== undefined">
              {{ t('keys.routeSuccessRate', { rate: formatPercent(routeDetail(groupId)!.success_rate!) }) }}
            </span>
            <span v-if="routeDetail(groupId)?.ttft_ms">
              {{ t('keys.routeTTFT', { value: Math.round(routeDetail(groupId)!.ttft_ms!) }) }}
            </span>
            <span v-if="routeDetail(groupId)?.duration_ms">
              {{ t('keys.routeDuration', { value: Math.round(routeDetail(groupId)!.duration_ms!) }) }}
            </span>
            <span v-if="routeDetail(groupId)?.cache_hit_rate !== undefined">
              {{ t('keys.routeCacheHit', { rate: formatPercent(routeDetail(groupId)!.cache_hit_rate!) }) }}
            </span>
            <span v-if="routeDetail(groupId)?.predicted_share !== undefined">
              {{ t('keys.routePredictedShare', { rate: formatPercent(routeDetail(groupId)!.predicted_share!) }) }}
            </span>
            <span v-if="routeDetail(groupId)?.price_confidence">
              {{ t(`keys.routeConfidence.${routeDetail(groupId)!.price_confidence}`) }}
            </span>
          </div>
        </div>
        <button
          type="button"
          class="rounded p-1 text-gray-400 hover:bg-white hover:text-gray-700 disabled:cursor-not-allowed disabled:opacity-30 dark:hover:bg-dark-700 dark:hover:text-gray-200"
          :disabled="index === 0"
          :title="t('keys.moveGroupUp')"
          :aria-label="t('keys.moveGroupUp')"
          :data-test="`move-route-up-${groupId}`"
          @click="move(index, -1)"
        >
          <Icon name="chevronUp" size="sm" />
        </button>
        <button
          type="button"
          class="rounded p-1 text-gray-400 hover:bg-white hover:text-gray-700 disabled:cursor-not-allowed disabled:opacity-30 dark:hover:bg-dark-700 dark:hover:text-gray-200"
          :disabled="index === modelValue.length - 1"
          :title="t('keys.moveGroupDown')"
          :aria-label="t('keys.moveGroupDown')"
          :data-test="`move-route-down-${groupId}`"
          @click="move(index, 1)"
        >
          <Icon name="chevronDown" size="sm" />
        </button>
        <button
          type="button"
          class="rounded p-1 text-gray-400 hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-300"
          :title="t('keys.removeGroup')"
          :aria-label="t('keys.removeGroup')"
          :data-test="`remove-route-group-${groupId}`"
          @click="remove(groupId)"
        >
          <Icon name="x" size="sm" />
        </button>
      </div>
    </div>

    <p class="text-xs text-gray-500 dark:text-gray-400">
      {{ t('keys.routeGroupHint', { max: maxGroups }) }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { ApiKeyGroupRoute, Group } from '@/types'

const props = withDefaults(defineProps<{
  modelValue: number[]
  groups: Group[]
  userGroupRates?: Record<number, number>
  routeDetails?: ApiKeyGroupRoute[]
  maxGroups?: number
}>(), {
  userGroupRates: () => ({}),
  routeDetails: () => [],
  maxGroups: 8
})

const emit = defineEmits<{
  'update:modelValue': [value: number[]]
}>()

const { t } = useI18n()
const isOpen = ref(false)
const searchQuery = ref('')
const containerRef = ref<HTMLElement | null>(null)
const triggerRef = ref<HTMLButtonElement | null>(null)
const searchInputRef = ref<HTMLInputElement | null>(null)
const highlightedIndex = ref(0)

const groupById = computed(() => {
  const result = new Map<number, Group>()
  for (const group of props.groups) result.set(group.id, group)
  return result
})

const routeDetailById = computed(() => new Map(props.routeDetails.map((route) => [route.group_id, route])))

const firstGroup = computed(() => groupById.value.get(props.modelValue[0]))

const triggerLabel = computed(() => {
  if (!props.modelValue.length) return t('keys.selectRouteGroups')
  return t('keys.routeGroupsSelected', { count: props.modelValue.length, max: props.maxGroups })
})

const filteredGroups = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  if (!query) return props.groups
  return props.groups.filter((group) =>
    group.name.toLowerCase().includes(query) ||
    group.platform.toLowerCase().includes(query) ||
    (group.description || '').toLowerCase().includes(query)
  )
})
const highlightedGroup = computed(() => filteredGroups.value[highlightedIndex.value])
const activeOptionID = computed(() => highlightedGroup.value ? `route-group-option-${highlightedGroup.value.id}` : undefined)

function effectiveRate(group: Group): number {
  return props.userGroupRates[group.id] ?? group.rate_multiplier
}

function routeDetail(groupId: number): ApiKeyGroupRoute | undefined {
  return routeDetailById.value.get(groupId)
}

function formatPercent(value: number): string {
  return `${(value * 100).toFixed(1)}%`
}

function isSelected(groupId: number): boolean {
  return props.modelValue.includes(groupId)
}

function disabledReason(group: Group): string {
  if (isSelected(group.id)) return ''
  if (group.status !== 'active') return t('keys.routeGroupUnavailableReason')
  if (props.modelValue.length >= props.maxGroups) return t('keys.routeGroupLimitReached', { max: props.maxGroups })
  if (!firstGroup.value) return ''
  if (group.platform !== firstGroup.value.platform) return t('keys.routeGroupPlatformMismatch')
  if (group.subscription_type !== firstGroup.value.subscription_type) return t('keys.routeGroupBillingMismatch')
  return ''
}

function canToggle(group: Group): boolean {
  return isSelected(group.id) || !disabledReason(group)
}

function optionClass(group: Group): string {
  if (!canToggle(group)) return 'cursor-not-allowed bg-gray-50 text-gray-400 dark:bg-dark-800/60 dark:text-dark-400'
  if (isSelected(group.id)) return 'bg-primary-50 text-primary-800 hover:bg-primary-100 dark:bg-primary-900/20 dark:text-primary-200'
  return 'text-gray-700 hover:bg-gray-50 dark:text-gray-200 dark:hover:bg-dark-700'
}

function toggleDropdown() {
  isOpen.value = !isOpen.value
  if (isOpen.value) {
    highlightedIndex.value = firstEnabledIndex(0, 1)
    nextTick(() => searchInputRef.value?.focus())
  }
}

function firstEnabledIndex(start: number, direction: 1 | -1): number {
  const groups = filteredGroups.value
  if (!groups.length) return 0
  for (let offset = 0; offset < groups.length; offset += 1) {
    const index = (start + direction * offset + groups.length) % groups.length
    if (canToggle(groups[index])) return index
  }
  return Math.min(Math.max(start, 0), groups.length - 1)
}

function moveHighlight(direction: 1 | -1) {
  if (!filteredGroups.value.length) return
  highlightedIndex.value = firstEnabledIndex(highlightedIndex.value + direction, direction)
}

function handleTriggerKeydown(event: KeyboardEvent) {
  if (!['ArrowDown', 'ArrowUp', 'Enter', ' '].includes(event.key)) return
  event.preventDefault()
  if (!isOpen.value) toggleDropdown()
  if (event.key === 'ArrowUp') moveHighlight(-1)
}

function handleSearchKeydown(event: KeyboardEvent) {
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    moveHighlight(event.key === 'ArrowDown' ? 1 : -1)
    return
  }
  if (event.key === 'Enter' && highlightedGroup.value) {
    event.preventDefault()
    toggleGroup(highlightedGroup.value)
    return
  }
  if (event.key === 'Escape') {
    event.preventDefault()
    isOpen.value = false
    nextTick(() => triggerRef.value?.focus())
  }
}

function highlightGroup(groupId: number) {
  const index = filteredGroups.value.findIndex(group => group.id === groupId)
  if (index >= 0) highlightedIndex.value = index
}

function toggleGroup(group: Group) {
  if (!canToggle(group)) return
  if (isSelected(group.id)) {
    remove(group.id)
    return
  }
  emit('update:modelValue', [...props.modelValue, group.id])
}

function remove(groupId: number) {
  emit('update:modelValue', props.modelValue.filter((id) => id !== groupId))
}

function move(index: number, delta: -1 | 1) {
  const target = index + delta
  if (target < 0 || target >= props.modelValue.length) return
  const next = [...props.modelValue]
  ;[next[index], next[target]] = [next[target], next[index]]
  emit('update:modelValue', next)
}

function handleOutsideClick(event: MouseEvent) {
  if (!containerRef.value?.contains(event.target as Node)) isOpen.value = false
}

watch(filteredGroups, () => {
  highlightedIndex.value = firstEnabledIndex(0, 1)
})

onMounted(() => document.addEventListener('mousedown', handleOutsideClick))
onUnmounted(() => document.removeEventListener('mousedown', handleOutsideClick))
</script>

<style scoped>
.route-dropdown-enter-active,
.route-dropdown-leave-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
}

.route-dropdown-enter-from,
.route-dropdown-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}
</style>
