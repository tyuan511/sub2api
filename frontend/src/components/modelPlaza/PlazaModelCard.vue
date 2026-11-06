<template>
  <button
    type="button"
    class="model-card group flex h-full w-full flex-col rounded-2xl border border-[var(--fv-line)] bg-[var(--fv-surface)] p-4 text-left shadow-[var(--fv-shadow)] backdrop-blur-[var(--fv-blur)] transition-all duration-200 hover:-translate-y-0.5 hover:border-[var(--fv-accent-outline)] hover:shadow-[var(--fv-shadow-hover)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--fv-accent-outline)]"
    @click="emit('open')"
  >
    <!-- 头部：平台图标 + 模型名 + 最低倍率徽章 -->
    <div class="flex items-start gap-3">
      <span
        class="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl"
        :style="{ color: accent, background: `color-mix(in srgb, ${accent} 10%, transparent)` }"
      >
        <PlatformIcon :platform="item.platform as GroupPlatform" size="md" />
      </span>
      <div class="min-w-0 flex-1">
        <div class="truncate text-sm font-semibold text-[var(--fv-text)]" :title="item.name">{{ item.name }}</div>
        <div class="mt-0.5 text-xs text-[var(--fv-muted)]">{{ platformLabel(item.platform) }}</div>
      </div>
      <span
        class="shrink-0 rounded-full bg-[var(--fv-accent-wash)] px-2 py-0.5 font-mono text-xs font-bold text-[var(--fv-accent)]"
        :title="t('modelPlaza.card.lowestRate')"
      >
        {{ formatRate(item.lowestRate) }}x
      </span>
    </div>

    <!-- 价格区：token 模式展示输入/输出最低价；按次/按图展示单次价格 -->
    <div class="mt-4 grid flex-1 grid-cols-2 gap-3">
      <template v-if="isToken">
        <div>
          <div class="text-[11px] uppercase tracking-wide text-[var(--fv-muted)]">{{ t('modelPlaza.table.input') }}</div>
          <div class="mt-1 font-mono text-sm font-semibold tabular-nums text-[var(--fv-text)]">
            {{ formatPerMillion(item.minInput) }}
          </div>
        </div>
        <div>
          <div class="text-[11px] uppercase tracking-wide text-[var(--fv-muted)]">{{ t('modelPlaza.table.output') }}</div>
          <div class="mt-1 font-mono text-sm font-semibold tabular-nums text-[var(--fv-text)]">
            {{ formatPerMillion(item.minOutput) }}
          </div>
        </div>
      </template>
      <div v-else>
        <div class="text-[11px] uppercase tracking-wide text-[var(--fv-muted)]">{{ billingModeLabel }}</div>
        <div class="mt-1 font-mono text-sm font-semibold tabular-nums text-[var(--fv-text)]">
          {{ formatRequest(item.minRequestPrice) }}
          <span class="font-normal text-[var(--fv-muted)]">{{ perUnitSuffix }}</span>
        </div>
      </div>
    </div>

    <!-- 底部：线路数 + 查看入口 -->
    <div class="mt-3 flex items-center justify-between border-t border-[var(--fv-line-soft)] pt-3 text-xs">
      <span class="text-[var(--fv-muted)]">{{ t('modelPlaza.card.routes', { n: item.offers.length }) }}</span>
      <span class="inline-flex items-center gap-1 font-medium text-[var(--fv-accent)] transition-transform group-hover:translate-x-0.5">
        {{ t('modelPlaza.card.viewGroups') }}
        <Icon name="arrowRight" size="xs" />
      </span>
    </div>
  </button>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import type { GroupPlatform } from '@/types'
import { platformAccentColor, platformLabel } from '@/utils/platformColors'
import { formatScaled } from '@/utils/pricing'
import {
  BILLING_MODE_IMAGE,
  BILLING_MODE_TOKEN
} from '@/constants/channel'
import type { AggregatedModel } from './aggregate'

const props = defineProps<{
  item: AggregatedModel
}>()

const emit = defineEmits<{ open: [] }>()

const { t } = useI18n()

const PER_MILLION = 1_000_000

const accent = computed(() => platformAccentColor(props.item.platform))

const isToken = computed(() => props.item.billingMode === BILLING_MODE_TOKEN)

const billingModeLabel = computed(() =>
  props.item.billingMode === BILLING_MODE_IMAGE
    ? t('modelPlaza.table.perImage')
    : t('modelPlaza.table.perRequest')
)

const perUnitSuffix = computed(() =>
  props.item.billingMode === BILLING_MODE_IMAGE
    ? t('modelPlaza.table.perUnitImage')
    : t('modelPlaza.table.perUnitRequest')
)

/** 价格已按最低倍率折算，统一保底 2 位小数。 */
function formatPerMillion(value: number | null): string {
  if (value == null) return '-'
  return formatScaled(value, PER_MILLION, 2)
}

function formatRequest(value: number | null): string {
  if (value == null) return '-'
  return formatScaled(value, 1, 2)
}

/** 倍率徽章去掉浮点噪声：0.5 → "0.5"，1 → "1"。 */
function formatRate(rate: number): string {
  return String(Math.round(rate * 1000) / 1000)
}
</script>
