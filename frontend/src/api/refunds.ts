/**
 * 用户侧余额退款 API。
 *
 * 口径：可退款金额 =（账户余额 − 已到账推广返利）×(1 − 手续费率)，保留两位小数。
 * 提交时上传收款二维码，管理员审核为「已完成」或「已拒绝 + 原因」。
 */
import { apiClient } from './client'
import type { PaginatedResponse } from '@/types'

export type RefundRequestStatus = 'pending' | 'completed' | 'rejected'

/** 起退点兜底值，与后端 RefundMinAmountDefault 保持一致（$20）。 */
export const DEFAULT_REFUND_MIN_AMOUNT = 20

export const REFUND_STATUS_VALUES: RefundRequestStatus[] = ['pending', 'completed', 'rejected']

/** 报价：金额四件套 + 是否还能提交 */
export interface RefundQuote {
  user_id: number
  balance: number
  rebate_amount: number
  fee_rate_percent: number
  fee_amount: number
  refundable_amount: number
  refundable_base: number
  /** 当前待处理申请已经冻结的金额；提交后余额不可消费，拒绝时解冻退回。 */
  frozen_amount: number
  /** 起退点：可退金额低于它不给提交。 */
  min_refund_amount: number
  below_minimum: boolean
  can_submit: boolean
  pending_request?: RefundRequest | null
}

export interface RefundRequest {
  id: number
  balance: number
  /** 提交时从余额转入冻结的那笔钱（= 当时的整笔余额）。 */
  frozen_amount: number
  rebate_amount: number
  fee_rate_percent: number
  fee_amount: number
  refundable_amount: number
  status: RefundRequestStatus
  user_note: string
  reject_reason?: string
  /** 管理员完成退款时填写的可选备注（打款流水号等），对用户可见。 */
  admin_note?: string
  has_qr_code: boolean
  created_at: string
  reviewed_at?: string
}

export async function getRefundQuote(): Promise<RefundQuote> {
  const { data } = await apiClient.get<RefundQuote>('/user/refunds/quote')
  return data
}

export async function createRefundRequest(payload: {
  qr_image: string
  note?: string
}): Promise<RefundRequest> {
  const { data } = await apiClient.post<RefundRequest>('/user/refund-requests', payload)
  return data
}

export async function listMyRefundRequests(page = 1, pageSize = 20): Promise<PaginatedResponse<RefundRequest>> {
  const { data } = await apiClient.get<PaginatedResponse<RefundRequest>>('/user/refund-requests', {
    params: { page, page_size: pageSize }
  })
  return data
}

/** 收款二维码需要鉴权，按 blob 取回后由调用方生成 object URL。 */
export async function getRefundQRBlob(id: number): Promise<Blob> {
  const { data } = await apiClient.get<Blob>(`/user/refund-requests/${id}/qr`, { responseType: 'blob' })
  return data
}

export const refundsAPI = {
  getRefundQuote,
  createRefundRequest,
  listMyRefundRequests,
  getRefundQRBlob
}
