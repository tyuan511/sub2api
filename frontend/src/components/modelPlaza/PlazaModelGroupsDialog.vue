<template>
  <BaseDialog
    :show="show && !!item"
    :title="item?.name ?? ''"
    width="wide"
    @close="emit('close')"
  >
    <p class="mb-4 flex items-center gap-2 text-xs text-[var(--fv-muted)]">
      <span>{{ t('modelPlaza.dialog.groups') }}</span>
      <span>·</span>
      <span>{{ t('modelPlaza.table.unitPerMillion') }}</span>
    </p>

    <div class="space-y-4">
      <div
        v-for="offer in item?.offers ?? []"
        :key="offer.group.id"
        class="rounded-xl border border-[var(--fv-line-soft)] bg-[var(--fv-surface-2)]/40 p-4"
      >
        <!-- 分组头：名称/平台/倍率徽章 -->
        <div class="flex flex-wrap items-center gap-2">
          <GroupBadge
            :name="offer.group.name"
            :platform="offer.group.platform as GroupPlatform"
            :subscription-type="(offer.group.subscription_type || 'standard') as SubscriptionType"
            :rate-multiplier="offer.group.rate_multiplier"
            :user-rate-multiplier="offer.group.user_rate_multiplier ?? null"
            :peak-rate-enabled="offer.group.peak_rate_enabled"
            :peak-start="offer.group.peak_start"
            :peak-end="offer.group.peak_end"
            :peak-rate-multiplier="offer.group.peak_rate_multiplier"
            always-show-rate
          />
          <span
            v-if="offer.group.is_exclusive"
            class="inline-flex items-center gap-1 rounded-md bg-purple-50 px-2 py-0.5 text-xs font-medium text-purple-600 dark:bg-purple-900/20 dark:text-purple-400"
          >
            <Icon name="shield" size="xs" class="h-3 w-3" />
            {{ t('modelPlaza.badges.exclusive') }}
          </span>
        </div>
        <p v-if="offer.group.description" class="mt-2 text-xs leading-5 text-[var(--fv-muted)]">
          {{ offer.group.description }}
        </p>

        <!-- 分时倍率时段 -->
        <div v-if="timePeriods(offer.model).length" class="mt-2.5 flex flex-wrap items-center gap-1.5">
          <span
            v-for="(period, idx) in timePeriods(offer.model)"
            :key="idx"
            class="inline-flex items-center whitespace-nowrap rounded-md bg-gray-100 px-2 py-0.5 font-mono text-[11px] text-gray-600 dark:bg-dark-700/70 dark:text-dark-200"
            :title="timePricingHint(offer)"
          >
            <span v-if="offer.model.time_pricing?.weekdays_only" class="mr-1 font-sans">
              {{ t('modelPlaza.table.timePricingWeekdays') }}
            </span>
            {{ formatTimeWindow(period) }}
            <span class="ml-1 font-bold text-primary-600 dark:text-primary-400">
              ×{{ formatRate(offer.effectiveRate * period.multiplier) }}
            </span>
          </span>
        </div>

        <!-- 价格明细：token 模式 -->
        <template v-if="billingMode(offer.model) === BILLING_MODE_TOKEN">
          <div v-if="tokenIntervals(offer.model).length" class="mt-3 overflow-x-auto">
            <table class="w-full min-w-[420px] text-left text-xs tabular-nums">
              <thead>
                <tr class="text-[11px] uppercase tracking-wide text-[var(--fv-muted)]">
                  <th class="py-1.5 pr-3 font-medium">{{ t('modelPlaza.table.tierLabel') }}</th>
                  <th class="py-1.5 px-3 font-medium">{{ t('modelPlaza.table.input') }}</th>
                  <th class="py-1.5 px-3 font-medium">{{ t('modelPlaza.table.output') }}</th>
                  <th class="py-1.5 pl-3 font-medium">{{ t('modelPlaza.table.cache') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="(iv, idx) in tokenIntervals(offer.model)"
                  :key="idx"
                  class="border-t border-[var(--fv-line-soft)]"
                >
                  <td class="py-2 pr-3 font-medium text-[var(--fv-text-soft)]" :title="tierHint(offer.model)">
                    {{ tierLabel(iv) }}
                  </td>
                  <td class="px-3 py-2 font-mono text-[var(--fv-text)]" :title="tierHint(offer.model)">
                    <span v-if="listTokenPrice(iv.input_price, offer)" class="mr-1 text-[var(--fv-muted)] line-through">{{ listTokenPrice(iv.input_price, offer) }}</span>
                    {{ paidPerMillion(iv.input_price, offer.effectiveRate) }}
                  </td>
                  <td class="px-3 py-2 font-mono text-[var(--fv-text)]">
                    <span v-if="listTokenPrice(iv.output_price, offer)" class="mr-1 text-[var(--fv-muted)] line-through">{{ listTokenPrice(iv.output_price, offer) }}</span>
                    {{ paidPerMillion(iv.output_price, offer.effectiveRate) }}
                  </td>
                  <td class="py-2 pl-3 font-mono text-[var(--fv-text-soft)]">
                    <template v-if="iv.cache_write_price != null || iv.cache_read_price != null">
                      {{ t('modelPlaza.table.cacheWriteShort') }} {{ paidPerMillion(iv.cache_write_price, offer.effectiveRate) }}
                      ·
                      {{ t('modelPlaza.table.cacheReadShort') }} {{ paidPerMillion(iv.cache_read_price, offer.effectiveRate) }}
                    </template>
                    <template v-else>-</template>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <dl v-else class="mt-3 grid grid-cols-2 gap-x-6 gap-y-2 text-xs sm:grid-cols-4">
            <div>
              <dt class="text-[var(--fv-muted)]">{{ t('modelPlaza.table.input') }}</dt>
              <dd class="mt-0.5 font-mono font-semibold tabular-nums text-[var(--fv-text)]">
                <span v-if="listTokenPrice(offer.model.pricing?.input_price, offer)" class="mr-1 font-normal text-[var(--fv-muted)] line-through">{{ listTokenPrice(offer.model.pricing?.input_price, offer) }}</span>
                {{ paidPerMillion(offer.model.pricing?.input_price, offer.effectiveRate) }}
              </dd>
            </div>
            <div>
              <dt class="text-[var(--fv-muted)]">{{ t('modelPlaza.table.output') }}</dt>
              <dd class="mt-0.5 font-mono font-semibold tabular-nums text-[var(--fv-text)]">
                <span v-if="listTokenPrice(offer.model.pricing?.output_price, offer)" class="mr-1 font-normal text-[var(--fv-muted)] line-through">{{ listTokenPrice(offer.model.pricing?.output_price, offer) }}</span>
                {{ paidPerMillion(offer.model.pricing?.output_price, offer.effectiveRate) }}
              </dd>
            </div>
            <div>
              <dt class="text-[var(--fv-muted)]">{{ t('modelPlaza.table.cacheWrite') }}</dt>
              <dd class="mt-0.5 font-mono tabular-nums text-[var(--fv-text-soft)]">
                <span v-if="listTokenPrice(offer.model.pricing?.cache_write_price, offer)" class="mr-1 font-normal text-[var(--fv-muted)] line-through">{{ listTokenPrice(offer.model.pricing?.cache_write_price, offer) }}</span>
                {{ paidPerMillion(offer.model.pricing?.cache_write_price, offer.effectiveRate) }}
              </dd>
            </div>
            <div>
              <dt class="text-[var(--fv-muted)]">{{ t('modelPlaza.table.cacheRead') }}</dt>
              <dd class="mt-0.5 font-mono tabular-nums text-[var(--fv-text-soft)]">
                <span v-if="listTokenPrice(offer.model.pricing?.cache_read_price, offer)" class="mr-1 font-normal text-[var(--fv-muted)] line-through">{{ listTokenPrice(offer.model.pricing?.cache_read_price, offer) }}</span>
                {{ paidPerMillion(offer.model.pricing?.cache_read_price, offer.effectiveRate) }}
              </dd>
            </div>
          </dl>
        </template>

        <!-- 按次 / 按图计费 -->
        <template v-else>
          <div class="mt-3 text-xs text-[var(--fv-text-soft)]">
            <template v-if="requestIntervals(offer.model).length">
              <span
                v-for="(iv, idx) in requestIntervals(offer.model)"
                :key="idx"
                class="mb-1.5 mr-1.5 inline-flex items-center gap-1 rounded-md bg-gray-100 px-2 py-1 font-mono text-[var(--fv-text)] dark:bg-dark-700/60 dark:text-dark-100"
              >
                <span class="font-sans text-[var(--fv-muted)]">{{ tierLabel(iv) }}</span>
                <span v-if="listRequestPrice(iv.per_request_price, offer)" class="line-through">{{ listRequestPrice(iv.per_request_price, offer) }}</span>
                {{ paidRequest(iv.per_request_price, offer) }}
                <span class="font-sans text-[var(--fv-muted)]">{{ perUnitSuffix(offer.model) }}</span>
              </span>
            </template>
            <template v-else-if="offer.model.pricing?.per_request_price != null">
              <span class="font-mono font-semibold text-[var(--fv-text)]">
                <span v-if="listRequestPrice(offer.model.pricing.per_request_price, offer)" class="mr-1 font-normal text-[var(--fv-muted)] line-through">{{ listRequestPrice(offer.model.pricing.per_request_price, offer) }}</span>
                {{ paidRequest(offer.model.pricing.per_request_price, offer) }}
              </span>
              <span class="ml-1 text-[var(--fv-muted)]">{{ perUnitSuffix(offer.model) }}</span>
            </template>
            <span v-else class="text-[var(--fv-muted)]">{{ t('modelPlaza.detail.noPricing') }}</span>
          </div>
        </template>

        <!-- 思考等级倍率 -->
        <div v-if="reasoningEffortMultipliers(offer.model).length" class="mt-3 flex flex-wrap items-center gap-1.5">
          <span
            v-for="[effort, multiplier] in reasoningEffortMultipliers(offer.model)"
            :key="effort"
            class="rounded-md bg-amber-50 px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
            :title="t('modelPlaza.table.reasoningMultiplierHint', { effort, multiplier })"
          >
            {{ t('modelPlaza.table.reasoningMultiplierBadge', { effort, multiplier }) }}
          </span>
        </div>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import Icon from '@/components/icons/Icon.vue'
import type { GroupPlatform, SubscriptionType } from '@/types'
import { formatScaled, resolveIntervalPrices } from '@/utils/pricing'
import {
  BILLING_MODE_TOKEN,
  BILLING_MODE_IMAGE,
  REASONING_EFFORT_LEVELS,
  type BillingMode
} from '@/constants/channel'
import type { UserPricingInterval } from '@/api/channels'
import type { PlazaModel, PlazaTimePricingPeriod } from '@/api/modelPlaza'
import type { AggregatedModel, ModelOffer } from './aggregate'

defineProps<{
  item: AggregatedModel | null
  show: boolean
}>()

const emit = defineEmits<{ close: [] }>()

const { t } = useI18n()

const PER_MILLION = 1_000_000

function billingMode(model: PlazaModel): BillingMode {
  return (model.pricing?.billing_mode || BILLING_MODE_TOKEN) as BillingMode
}

function reasoningEffortMultipliers(model: PlazaModel): [string, number][] {
  const multipliers = model.pricing?.reasoning_effort_multipliers
  return REASONING_EFFORT_LEVELS.flatMap((effort) => {
    const multiplier = multipliers?.[effort]
    return typeof multiplier === 'number' && Number.isFinite(multiplier) && multiplier > 0
      ? [[effort, multiplier] as [string, number]]
      : []
  })
}

function paidPerMillion(value: number | null | undefined, rate: number): string {
  if (value == null) return '-'
  return formatScaled(value * rate, PER_MILLION, 2)
}

/** 倍率徽章去掉浮点噪声：0.5 → "0.5"，1 → "1"。 */
function formatRate(rate: number): string {
  return String(Math.round(rate * 1000) / 1000)
}

function paidRequest(value: number | null | undefined, offer: ModelOffer): string {
  if (value == null) return '-'
  return formatScaled(value * offer.effectiveRate, 1, 2)
}

/** 倍率 ≠ 1 时返回未乘倍率的原价供划线展示。 */
function listTokenPrice(value: number | null | undefined, offer: ModelOffer): string | null {
  if (value == null || offer.effectiveRate === 1) return null
  return formatScaled(value, PER_MILLION, 2)
}

function listRequestPrice(value: number | null | undefined, offer: ModelOffer): string | null {
  if (value == null || offer.effectiveRate === 1) return null
  return formatScaled(value, 1, 2)
}

function perUnitSuffix(model: PlazaModel): string {
  return billingMode(model) === BILLING_MODE_IMAGE
    ? t('modelPlaza.table.perUnitImage')
    : t('modelPlaza.table.perUnitRequest')
}

function timePeriods(model: PlazaModel): PlazaTimePricingPeriod[] {
  return model.time_pricing?.periods ?? []
}

function timePricingHint(offer: ModelOffer): string {
  const key = offer.model.time_pricing?.weekdays_only
    ? 'modelPlaza.table.timePricingRowHintWeekdays'
    : 'modelPlaza.table.timePricingRowHint'
  return t(key, { timezone: offer.model.time_pricing?.timezone })
}

function formatTimeWindow(p: PlazaTimePricingPeriod): string {
  const clock = (v: string) => v.replace(/^(\d{2}:\d{2}):00$/, '$1')
  return `${clock(p.start_time)}–${clock(p.end_time)}`
}

function tokenIntervals(model: PlazaModel): UserPricingInterval[] {
  return sortByContext(model.pricing?.intervals ?? []).map((iv) => resolveIntervalPrices(iv, model.pricing!))
}

function sortByContext(intervals: UserPricingInterval[]): UserPricingInterval[] {
  return [...intervals].sort((a, b) => a.min_tokens - b.min_tokens)
}

function tierHint(model: PlazaModel): string {
  return model.long_context_basis === 'marginal'
    ? t('modelPlaza.table.tierHintMarginal')
    : t('modelPlaza.table.tierHint')
}

function requestIntervals(model: PlazaModel): UserPricingInterval[] {
  return (model.pricing?.intervals ?? []).filter((iv) => iv.per_request_price != null)
}

function tierLabel(iv: UserPricingInterval): string {
  if (iv.tier_label) return iv.tier_label
  const { min_tokens: min, max_tokens: max } = iv
  return max == null ? `>${formatTokenCount(min)}` : `≤${formatTokenCount(max)}`
}

function formatTokenCount(n: number): string {
  if (n >= 1_000_000) return `${trimZero(n / 1_000_000)}M`
  if (n >= 1_000) return `${trimZero(n / 1_000)}K`
  return String(n)
}

function trimZero(n: number): string {
  return String(Math.round(n * 100) / 100)
}
</script>
