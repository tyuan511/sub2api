<template>
  <AppLayout>
    <div class="space-y-4">
      <!-- Filters -->
      <div class="card p-4">
        <div class="flex flex-wrap items-center gap-3">
          <div class="flex-1 sm:max-w-64">
            <input
              v-model="search"
              type="text"
              class="input"
              :placeholder="t('admin.refunds.searchPlaceholder')"
              data-testid="refund-search"
              @input="debounceLoad"
            />
          </div>
          <Select
            v-model="statusFilter"
            :options="statusOptions"
            class="w-36"
            data-testid="refund-status-filter"
            @change="reloadFromFirstPage"
          />
          <button class="btn btn-secondary" :disabled="loading" :title="t('common.refresh')" @click="loadRequests">
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>
      </div>

      <!-- Table -->
      <DataTable :columns="columns" :data="requests" :loading="loading">
        <template #cell-user="{ row }">
          <div class="space-y-0.5">
            <div class="text-sm font-medium text-gray-900 dark:text-white">{{ row.user_email || '-' }}</div>
            <div class="text-xs text-gray-500 dark:text-dark-400">
              #{{ row.user_id }}<span v-if="row.user_name"> · {{ row.user_name }}</span>
            </div>
          </div>
        </template>
        <template #cell-balance="{ row }">
          <span class="font-mono text-sm text-gray-700 dark:text-gray-300">{{ money(row.balance) }}</span>
        </template>
        <template #cell-rebate_amount="{ row }">
          <span class="font-mono text-sm text-gray-500 dark:text-dark-400">{{ money(row.rebate_amount) }}</span>
        </template>
        <template #cell-fee_amount="{ row }">
          <span class="font-mono text-sm text-gray-500 dark:text-dark-400">
            {{ money(row.fee_amount) }}
            <span class="text-xs text-gray-400 dark:text-dark-500">({{ feeLabel(row.fee_rate_percent) }})</span>
          </span>
        </template>
        <template #cell-refundable_amount="{ row }">
          <span class="font-mono text-sm font-semibold text-primary-700 dark:text-primary-300">
            {{ money(row.refundable_amount) }}
          </span>
        </template>
        <template #cell-status="{ row }">
          <span :class="['badge whitespace-nowrap', statusBadgeClass(row.status)]">
            {{ statusLabel(row.status) }}
          </span>
        </template>
        <template #cell-created_at="{ row }">
          <span class="text-sm text-gray-700 dark:text-gray-300">{{ formatDateTime(row.created_at) }}</span>
        </template>
        <template #cell-actions="{ row }">
          <div class="flex items-center gap-1">
            <button
              class="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-gray-600 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-dark-600"
              data-testid="refund-view"
              @click="openDetail(row)"
            >
              <Icon name="eye" size="sm" />
              {{ t('common.view') }}
            </button>
            <button
              v-if="row.status === 'pending'"
              class="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-green-600 hover:bg-green-50 dark:text-green-400 dark:hover:bg-green-900/20"
              data-testid="refund-complete"
              @click="openDetail(row)"
            >
              <Icon name="check" size="sm" />
              {{ t('admin.refunds.complete') }}
            </button>
          </div>
        </template>
      </DataTable>

      <Pagination
        v-if="pagination.total > 0"
        :page="pagination.page"
        :total="pagination.total"
        :page-size="pagination.page_size"
        @update:page="handlePageChange"
        @update:pageSize="handlePageSizeChange"
      />
    </div>

    <!-- 详情：金额拆解 + 收款二维码 + 审核动作 -->
    <BaseDialog
      :show="detailOpen"
      :title="t('admin.refunds.detailTitle')"
      width="wide"
      @close="closeDetail"
    >
      <div v-if="selected" class="space-y-5">
        <div class="grid gap-3 sm:grid-cols-2">
          <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-800">
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.refunds.applicant') }}</p>
            <p class="mt-1 truncate text-sm font-medium text-gray-900 dark:text-white">{{ selected.user_email || '-' }}</p>
            <p class="text-xs text-gray-500 dark:text-dark-400">#{{ selected.user_id }}{{ selected.user_name ? ' · ' + selected.user_name : '' }}</p>
          </div>
          <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-800">
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.refunds.submittedAt') }}</p>
            <p class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ formatDateTime(selected.created_at) }}</p>
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ statusLabel(selected.status) }}</p>
          </div>
        </div>

        <div class="overflow-hidden rounded-2xl border border-gray-100 dark:border-dark-700">
          <DetailRow :label="t('refund.balance')" :value="money(selected.balance)" />
          <DetailRow :label="t('refund.rebate')" :value="`-${money(selected.rebate_amount)}`" />
          <DetailRow :label="t('refund.fee', { rate: feeLabel(selected.fee_rate_percent) })" :value="`-${money(selected.fee_amount)}`" />
          <DetailRow
            :label="t('refund.refundable')"
            :value="money(selected.refundable_amount)"
            highlight
          />
          <DetailRow
            v-if="selected.frozen_amount > 0"
            :label="t('admin.refunds.frozen')"
            :value="money(selected.frozen_amount)"
          />
        </div>

        <p
          v-if="selected.status === 'pending'"
          class="text-xs leading-relaxed text-amber-700 dark:text-amber-400"
        >
          {{ t('admin.refunds.frozenNote') }}
        </p>

        <!-- 余额来源构成：钱在余额里是流动的，系统不做拦截，摊开给管理员判断 -->
        <div v-if="composition">
          <p class="input-label">{{ t('admin.refunds.compositionTitle') }}</p>
          <div class="overflow-hidden rounded-2xl border border-gray-100 dark:border-dark-700">
            <div
              v-for="line in compositionLines"
              :key="line.key"
              class="flex items-center justify-between border-t border-gray-100 px-4 py-2.5 first:border-t-0 dark:border-dark-700"
            >
              <span class="text-sm text-gray-500 dark:text-dark-400">{{ line.label }}</span>
              <span :class="['font-mono text-sm', line.cash ? 'font-semibold text-gray-900 dark:text-white' : 'text-gray-600 dark:text-gray-300']">
                {{ money(line.value) }}
              </span>
            </div>
          </div>
          <p
            :class="['mt-2 text-xs leading-relaxed', paidOnlineIsZero ? 'text-amber-700 dark:text-amber-400' : 'text-gray-500 dark:text-dark-400']"
            data-testid="refund-composition-note"
          >
            {{ paidOnlineIsZero ? t('admin.refunds.compositionNoCash') : t('admin.refunds.compositionHint') }}
          </p>
        </div>

        <div>
          <p class="input-label">{{ t('admin.refunds.qrCode') }}</p>
          <div v-if="qrLoading" class="flex h-56 items-center justify-center rounded-2xl border border-gray-100 dark:border-dark-700">
            <div class="h-6 w-6 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"></div>
          </div>
          <div
            v-else-if="qrUrl"
            class="flex items-center justify-center rounded-2xl border border-gray-100 bg-white p-4 dark:border-dark-700"
          >
            <img :src="qrUrl" :alt="t('admin.refunds.qrCode')" class="max-h-72 w-auto object-contain" />
          </div>
          <p v-else class="text-sm text-gray-500 dark:text-dark-400">{{ t('admin.refunds.qrUnavailable') }}</p>
        </div>

        <div v-if="selected.user_note" class="rounded-xl bg-gray-50 p-4 dark:bg-dark-800">
          <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('refund.noteLabel') }}</p>
          <p class="mt-1 whitespace-pre-wrap break-words text-sm text-gray-800 dark:text-gray-200">{{ selected.user_note }}</p>
        </div>

        <div v-if="selected.reject_reason" class="rounded-xl border border-red-200 bg-red-50 p-4 dark:border-red-900/50 dark:bg-red-950/30">
          <p class="text-xs text-red-600 dark:text-red-400">{{ t('admin.refunds.rejectReason') }}</p>
          <p class="mt-1 whitespace-pre-wrap break-words text-sm text-red-800 dark:text-red-300">{{ selected.reject_reason }}</p>
        </div>

        <div v-if="selected.admin_note" class="rounded-xl border border-green-200 bg-green-50 p-4 dark:border-green-900/50 dark:bg-green-950/30">
          <p class="text-xs text-green-700 dark:text-green-400">{{ t('admin.refunds.adminNote') }}</p>
          <p class="mt-1 whitespace-pre-wrap break-words text-sm text-green-900 dark:text-green-200">{{ selected.admin_note }}</p>
        </div>

        <div v-if="selected.status === 'pending'" class="space-y-3 rounded-2xl border border-gray-100 p-4 dark:border-dark-700">
          <div>
            <label class="input-label">{{ t('admin.refunds.adminNoteInput') }}</label>
            <input
              v-model="adminNote"
              type="text"
              class="input mt-1 w-full"
              :maxlength="500"
              :placeholder="t('admin.refunds.adminNotePlaceholder')"
              data-testid="refund-admin-note-input"
            />
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
              {{ t('admin.refunds.adminNoteHint') }}
            </p>
          </div>

          <label class="input-label">{{ t('admin.refunds.rejectReasonInput') }}</label>
          <textarea
            v-model="rejectReason"
            rows="2"
            class="input w-full"
            :maxlength="1000"
            :placeholder="t('admin.refunds.rejectReasonPlaceholder')"
          />
          <div class="flex flex-wrap justify-end gap-2">
            <button
              class="btn btn-secondary"
              :disabled="submitting"
              data-testid="refund-reject"
              @click="handleReject"
            >
              {{ t('admin.refunds.reject') }}
            </button>
            <button
              class="btn btn-primary"
              :disabled="submitting"
              data-testid="refund-complete-confirm"
              @click="confirmOpen = true"
            >
              {{ t('admin.refunds.complete') }}
            </button>
          </div>
        </div>
      </div>
    </BaseDialog>

    <ConfirmDialog
      :show="confirmOpen"
      :title="t('admin.refunds.complete')"
      :message="selected ? t('admin.refunds.completeConfirm', {
        email: selected.user_email || `#${selected.user_id}`,
        amount: money(selected.refundable_amount)
      }) : ''"
      :danger="true"
      :loading="submitting"
      :confirm-text="t('admin.refunds.complete')"
      @confirm="handleComplete"
      @cancel="confirmOpen = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, h, onBeforeUnmount, onMounted, reactive, ref, watch, defineComponent } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import { adminRefundsAPI, type AdminRefundDetail, type AdminRefundEntry, type RefundBalanceComposition } from '@/api/admin/refunds'
import type { RefundRequestStatus } from '@/api/refunds'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const requests = ref<AdminRefundEntry[]>([])
const search = ref('')
const statusFilter = ref<RefundRequestStatus | ''>('pending')
const pagination = reactive({ page: 1, page_size: 20, total: 0 })

const detailOpen = ref(false)
const selected = ref<AdminRefundEntry | null>(null)
const qrUrl = ref('')
const qrLoading = ref(false)
const rejectReason = ref('')
const adminNote = ref('')
const submitting = ref(false)
const confirmOpen = ref(false)
let debounceTimer: ReturnType<typeof setTimeout> | null = null

// 详情里的金额行：可退款金额高亮，其余保持灰阶，和管理员看到的账单口径一致。
const DetailRow = defineComponent({
  props: {
    label: { type: String, required: true },
    value: { type: String, required: true },
    highlight: { type: Boolean, default: false }
  },
  setup(props) {
    return () =>
      h('div', {
        class: [
          'flex items-center justify-between px-4 py-3 border-t border-gray-100 dark:border-dark-700 first:border-t-0',
          props.highlight ? 'bg-primary-50 dark:bg-primary-950/30' : ''
        ]
      }, [
        h('span', {
          class: props.highlight
            ? 'text-sm font-medium text-gray-700 dark:text-dark-200'
            : 'text-sm text-gray-500 dark:text-dark-400'
        }, props.label),
        h('span', {
          class: props.highlight
            ? 'font-mono text-lg font-semibold text-primary-700 dark:text-primary-300'
            : 'font-mono text-sm text-gray-700 dark:text-gray-300'
        }, props.value)
      ])
  }
})

const composition = computed<RefundBalanceComposition | null>(
  () => (selected.value as AdminRefundDetail | null)?.composition ?? null
)

// 线上实付为 0 但账户里有钱：这些钱都来自发放，退现金前值得停一下。
// 判据必须用申请时的规模（reference_balance）：提交后整笔余额已转入冻结，
// 实时余额恒为 0，用它会让我最需要亮灯的场合恰好不亮。
const paidOnlineIsZero = computed(() => {
  const c = composition.value
  if (!c) return false
  return c.paid_online <= 0 && (c.reference_balance ?? c.current_balance + c.frozen_balance) > 0
})

const compositionLines = computed(() => {
  const c = composition.value
  if (!c) return []
  return [
    { key: 'paid', label: t('admin.refunds.composition.paidOnline'), value: c.paid_online, cash: true },
    { key: 'redeem', label: t('admin.refunds.composition.redeemCodes'), value: c.redeem_codes, cash: false },
    { key: 'admin', label: t('admin.refunds.composition.adminAdjusted'), value: c.admin_adjusted, cash: false },
    { key: 'promo', label: t('admin.refunds.composition.promoBonus'), value: c.promo_bonus, cash: false },
    { key: 'rebate', label: t('admin.refunds.composition.rebateTransferred'), value: c.rebate_transferred, cash: false },
    { key: 'unattributed', label: t('admin.refunds.composition.unattributed'), value: c.unattributed, cash: false },
    { key: 'attributed', label: t('admin.refunds.composition.attributedTotal'), value: c.attributed_total, cash: false },
    // 用申请时的快照而不是实时余额：冻结之后实时余额恒为 0，看了等于没看。
    { key: 'reference', label: t('admin.refunds.composition.referenceBalance'), value: c.reference_balance, cash: false }
  ]
})

const statusOptions = computed(() => [
  { value: '', label: t('admin.refunds.statusAll') },
  { value: 'pending', label: t('admin.refunds.status.pending') },
  { value: 'completed', label: t('admin.refunds.status.completed') },
  { value: 'rejected', label: t('admin.refunds.status.rejected') }
])

const columns = computed<Column[]>(() => [
  { key: 'user', label: t('admin.refunds.applicant') },
  { key: 'balance', label: t('refund.balance') },
  { key: 'rebate_amount', label: t('refund.rebate') },
  { key: 'fee_amount', label: t('refund.feeShort') },
  { key: 'refundable_amount', label: t('refund.refundable') },
  { key: 'status', label: t('admin.refunds.statusLabel') },
  { key: 'created_at', label: t('admin.refunds.submittedAt') },
  { key: 'actions', label: t('common.actions') }
])

// 余额按美元记账，与用户侧展示保持同一个 $ 口径。
function money(value: number | null | undefined): string {
  return `$${(value ?? 0).toFixed(2)}`
}

function feeLabel(rate: number): string {
  return `${Number(rate.toFixed(2))}%`
}

function statusLabel(status: string): string {
  return t(`admin.refunds.status.${status}`)
}

function statusBadgeClass(status: string): string {
  if (status === 'completed') return 'badge-success'
  if (status === 'rejected') return 'badge-danger'
  return 'badge-warning'
}

function revokeQR(): void {
  if (qrUrl.value) {
    URL.revokeObjectURL(qrUrl.value)
    qrUrl.value = ''
  }
}

async function loadRequests(): Promise<void> {
  loading.value = true
  try {
    const resp = await adminRefundsAPI.listRefundRequests({
      page: pagination.page,
      page_size: pagination.page_size,
      status: statusFilter.value || undefined,
      search: search.value.trim() || undefined
    })
    requests.value = resp.items ?? []
    pagination.total = resp.total
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.refunds.loadFailed')))
  } finally {
    loading.value = false
  }
}

function debounceLoad(): void {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    reloadFromFirstPage()
  }, 300)
}

function reloadFromFirstPage(): void {
  pagination.page = 1
  void loadRequests()
}

function handlePageChange(page: number): void {
  pagination.page = page
  void loadRequests()
}

function handlePageSizeChange(size: number): void {
  pagination.page_size = size
  pagination.page = 1
  void loadRequests()
}

async function openDetail(row: AdminRefundEntry): Promise<void> {
  selected.value = row
  rejectReason.value = ''
  adminNote.value = ''
  revokeQR()
  detailOpen.value = true
  qrLoading.value = true

  // 二维码与余额构成并行取：构成失败只少一块参考信息，不挡审核。
  const blobTask = adminRefundsAPI.getRefundQRBlob(row.id)
    .then((blob) => {
      qrUrl.value = URL.createObjectURL(blob)
    })
    .catch((error: unknown) => {
      appStore.showError(extractApiErrorMessage(error, t('admin.refunds.qrLoadFailed')))
    })

  try {
    const detail = await adminRefundsAPI.getRefundRequest(row.id)
    selected.value = detail
  } catch {
    // 详情拉不到时退回列表行数据，弹窗仍然可用，只是没有构成。
  } finally {
    await blobTask
    qrLoading.value = false
  }
}

function closeDetail(): void {
  detailOpen.value = false
  confirmOpen.value = false
  revokeQR()
  selected.value = null
}

async function handleComplete(): Promise<void> {
  if (!selected.value) return
  submitting.value = true
  try {
    await adminRefundsAPI.completeRefundRequest(selected.value.id, adminNote.value.trim())
    appStore.showSuccess(t('admin.refunds.completedToast'))
    closeDetail()
    void loadRequests()
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.refunds.completeFailed')))
  } finally {
    submitting.value = false
  }
}

async function handleReject(): Promise<void> {
  if (!selected.value) return
  const reason = rejectReason.value.trim()
  if (!reason) {
    appStore.showError(t('admin.refunds.rejectReasonRequired'))
    return
  }
  submitting.value = true
  try {
    await adminRefundsAPI.rejectRefundRequest(selected.value.id, reason)
    appStore.showSuccess(t('admin.refunds.rejectedToast'))
    closeDetail()
    void loadRequests()
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.refunds.rejectFailed')))
  } finally {
    submitting.value = false
  }
}

// 切走用户时兜底释放 blob URL，避免内存泄漏。
watch(detailOpen, (open) => {
  if (!open) revokeQR()
})

onMounted(() => {
  void loadRequests()
})

onBeforeUnmount(() => {
  if (debounceTimer) clearTimeout(debounceTimer)
  revokeQR()
})
</script>
