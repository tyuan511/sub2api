<template>
  <BaseDialog
    :show="show"
    :title="t('refund.dialogTitle')"
    width="normal"
    @close="handleClose"
  >
    <div v-if="loading" class="flex justify-center py-10">
      <div class="h-6 w-6 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"></div>
    </div>

    <div v-else-if="display" class="space-y-5">
      <!-- 金额拆解：先剔掉推广返利，再收手续费，最后一行才是能退到手的钱。
           有待处理申请时展示那笔申请的快照，避免用冻结后的 0 余额算出一张空单。 -->
      <div class="overflow-hidden rounded-2xl border border-gray-100 dark:border-dark-700">
        <div class="flex items-center justify-between px-4 py-3">
          <span class="text-sm text-gray-500 dark:text-dark-400">{{ t('refund.balance') }}</span>
          <span class="font-mono text-sm font-medium text-gray-900 dark:text-white">{{ money(display.balance) }}</span>
        </div>
        <div class="flex items-center justify-between border-t border-gray-100 px-4 py-3 dark:border-dark-700">
          <span class="text-sm text-gray-500 dark:text-dark-400">{{ t('refund.rebate') }}</span>
          <span class="font-mono text-sm text-gray-500 dark:text-dark-400">-{{ money(display.rebate) }}</span>
        </div>
        <div class="flex items-center justify-between border-t border-gray-100 px-4 py-3 dark:border-dark-700">
          <span class="text-sm text-gray-500 dark:text-dark-400">
            {{ t('refund.fee', { rate: formatFeeRate(display.feeRate) }) }}
          </span>
          <span class="font-mono text-sm text-gray-500 dark:text-dark-400">-{{ money(display.fee) }}</span>
        </div>
        <div class="flex items-center justify-between border-t border-gray-100 bg-primary-50 px-4 py-4 dark:border-dark-700 dark:bg-primary-950/30">
          <span class="text-sm font-medium text-gray-700 dark:text-dark-200">{{ t('refund.refundable') }}</span>
          <span class="font-mono text-lg font-semibold text-primary-700 dark:text-primary-300">
            {{ money(display.refundable) }}
          </span>
        </div>
      </div>

      <p class="text-xs leading-relaxed text-gray-500 dark:text-dark-400">
        {{ t('refund.rebateHint') }}
      </p>

      <!-- 待处理：整笔余额已冻结，审核期间不可消费 -->
      <div
        v-if="pending"
        class="rounded-2xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800 dark:border-amber-900/50 dark:bg-amber-950/30 dark:text-amber-300"
      >
        <p class="font-medium">{{ t('refund.pendingTitle') }}</p>
        <p class="mt-1">
          {{ t('refund.pendingDesc', {
            amount: money(pending.refundable_amount),
            date: formatDateTime(pending.created_at)
          }) }}
        </p>
        <p class="mt-1 text-amber-700/90 dark:text-amber-400/90">
          {{ t('refund.frozenHint', { amount: money(pending.frozen_amount) }) }}
        </p>
      </div>

      <div
        v-else-if="belowMinimum"
        class="rounded-2xl border border-gray-200 bg-gray-50 p-4 text-sm text-gray-600 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-300"
        data-testid="refund-below-min"
      >
        {{ t('refund.belowMinimum', { amount: money(minRefundAmount) }) }}
      </div>

      <template v-else>
        <div>
          <label class="input-label">{{ t('refund.qrLabel') }}</label>
          <ImageUpload
            v-model="qrImage"
            mode="image"
            :upload-label="t('refund.qrUpload')"
            :remove-label="t('refund.qrRemove')"
            :hint="t('refund.qrHint')"
            :max-size="maxQRBytes"
          />
        </div>

        <div>
          <label class="input-label">{{ t('refund.noteLabel') }}</label>
          <textarea
            v-model="note"
            rows="2"
            class="input mt-1 w-full"
            :maxlength="500"
            :placeholder="t('refund.notePlaceholder')"
          />
        </div>
      </template>

      <!-- 我的退款记录：状态、金额、拒绝原因都在这里回显 -->
      <div>
        <p class="input-label">{{ t('refund.recordsTitle') }}</p>
        <div v-if="recordsLoading" class="py-4 text-sm text-gray-500 dark:text-dark-400">
          {{ t('common.loading') }}
        </div>
        <div
          v-else-if="!records.length"
          class="rounded-2xl border border-dashed border-gray-200 px-4 py-6 text-center text-sm text-gray-400 dark:border-dark-600 dark:text-dark-500"
        >
          {{ t('refund.recordsEmpty') }}
        </div>
        <ul v-else class="divide-y divide-gray-100 overflow-hidden rounded-2xl border border-gray-100 dark:divide-dark-700 dark:border-dark-700">
          <li
            v-for="record in records"
            :key="record.id"
            class="px-4 py-3"
            data-testid="refund-record-row"
          >
            <div class="flex items-center justify-between gap-3">
              <div class="min-w-0">
                <p class="font-mono text-sm font-medium text-gray-900 dark:text-white">
                  {{ money(record.refundable_amount) }}
                </p>
                <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">
                  {{ formatDateTime(record.created_at) }}
                  <span v-if="record.frozen_amount > 0"> · {{ t('refund.frozenShort', { amount: money(record.frozen_amount) }) }}</span>
                </p>
              </div>
              <span :class="['badge whitespace-nowrap', statusBadgeClass(record.status)]">
                {{ t(`refund.status.${record.status}`) }}
              </span>
            </div>
            <p
              v-if="record.reject_reason"
              class="mt-2 whitespace-pre-wrap break-words rounded-lg bg-red-50 px-3 py-2 text-xs text-red-700 dark:bg-red-950/30 dark:text-red-300"
            >
              {{ t('refund.rejectReason') }}{{ record.reject_reason }}
            </p>
            <p
              v-if="record.admin_note"
              class="mt-2 whitespace-pre-wrap break-words rounded-lg bg-green-50 px-3 py-2 text-xs text-green-700 dark:bg-green-950/30 dark:text-green-300"
            >
              {{ t('refund.adminNote') }}{{ record.admin_note }}
            </p>
          </li>
        </ul>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button class="btn btn-secondary" @click="handleClose">{{ t('common.close') }}</button>
        <button
          v-if="quote && !pending && !belowMinimum"
          class="btn btn-primary"
          :disabled="loading || submitting || !canSubmit"
          data-testid="refund-submit"
          @click="handleSubmit"
        >
          {{ submitting ? t('common.processing') : t('refund.submit') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ImageUpload from '@/components/common/ImageUpload.vue'
import {
  createRefundRequest,
  getRefundQuote,
  listMyRefundRequests,
  type RefundQuote,
  type RefundRequest
} from '@/api/refunds'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{
  show: boolean
}>()

const emit = defineEmits<{
  close: []
  success: []
}>()

const { t } = useI18n()
const appStore = useAppStore()

// 与后端 RefundQRMaxBytes 对齐，避免用户填完才收到超限报错。
const maxQRBytes = 1024 * 1024

const loading = ref(false)
const recordsLoading = ref(false)
const submitting = ref(false)
const quote = ref<RefundQuote | null>(null)
const records = ref<RefundRequest[]>([])
const qrImage = ref('')
const note = ref('')

const pending = computed(() => quote.value?.pending_request ?? null)
const belowMinimum = computed(() => quote.value?.below_minimum ?? false)
const minRefundAmount = computed(() => quote.value?.min_refund_amount ?? 0)

// 有待处理申请时用那笔申请的快照，否则用实时报价：
// 冻结后余额已经是 0，拿实时余额算明细只会得到一张空单。
const display = computed(() => {
  if (pending.value) {
    const record = pending.value
    return {
      balance: record.balance,
      rebate: record.rebate_amount,
      feeRate: record.fee_rate_percent,
      fee: record.fee_amount,
      refundable: record.refundable_amount
    }
  }
  if (!quote.value) return null
  return {
    balance: quote.value.balance,
    rebate: quote.value.rebate_amount,
    feeRate: quote.value.fee_rate_percent,
    fee: quote.value.fee_amount,
    refundable: quote.value.refundable_amount
  }
})

const canSubmit = computed(() =>
  Boolean(quote.value?.can_submit) && qrImage.value.trim().length > 0 && !submitting.value
)

// 余额在面板里按美元记账，展示口径与个人资料页一致：$ + 两位小数。
function money(value: number | null | undefined): string {
  return `$${(value ?? 0).toFixed(2)}`
}

function formatFeeRate(rate: number): string {
  return `${Number(rate.toFixed(2))}%`
}

function statusBadgeClass(status: string): string {
  if (status === 'completed') return 'badge-success'
  if (status === 'rejected') return 'badge-danger'
  return 'badge-warning'
}

async function loadQuote(): Promise<void> {
  loading.value = true
  try {
    quote.value = await getRefundQuote()
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('refund.loadFailed')))
  } finally {
    loading.value = false
  }
}

async function loadRecords(): Promise<void> {
  recordsLoading.value = true
  try {
    const resp = await listMyRefundRequests(1, 10)
    records.value = resp.items ?? []
  } catch {
    // 记录列表失败不打断主流程：报价与提交本身已经报错提示。
    records.value = []
  } finally {
    recordsLoading.value = false
  }
}

async function handleSubmit(): Promise<void> {
  if (!canSubmit.value) return
  submitting.value = true
  try {
    const created = await createRefundRequest({ qr_image: qrImage.value, note: note.value.trim() })
    appStore.showSuccess(t('refund.submitted', { amount: money(created.refundable_amount) }))
    qrImage.value = ''
    note.value = ''
    emit('success')
    emit('close')
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('refund.submitFailed')))
    // 可能是并发下已存在待处理申请，重新拉一次保证界面与后端一致。
    await Promise.all([loadQuote(), loadRecords()])
  } finally {
    submitting.value = false
  }
}

function handleClose(): void {
  emit('close')
}

// 每次打开都重新报价：余额、返利和费率都可能已经变化。
watch(
  () => props.show,
  (visible) => {
    if (!visible) return
    quote.value = null
    records.value = []
    qrImage.value = ''
    note.value = ''
    void loadQuote()
    void loadRecords()
  },
  { immediate: true }
)
</script>
