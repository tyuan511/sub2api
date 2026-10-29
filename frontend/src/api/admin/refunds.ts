/**
 * 管理端余额退款申请 API：查看申请、标记已完成、拒绝并填写原因。
 */
import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'
import type { RefundRequest, RefundRequestStatus } from '../refunds'

export interface AdminRefundEntry extends RefundRequest {
  user_id: number
  user_email: string
  user_name: string
  qr_content_type: string
  qr_size: number
  reviewed_by?: number | null
}

/** 余额来源构成：管理员判断"这钱是不是真付过的"的决策依据，系统不做拦截。 */
export interface RefundBalanceComposition {
  /** 线上支付净额（剔除赠送额度与已退金额） */
  paid_online: number
  /** 卡密兑换入账累计 */
  redeem_codes: number
  /** 管理员手动加减净额（不含退款冻结/解冻） */
  admin_adjusted: number
  /** 优惠码赠送累计 */
  promo_bonus: number
  /** 推广返利转入累计 */
  rebate_transferred: number
  /** 可归因入账合计 */
  attributed_total: number
  current_balance: number
  frozen_balance: number
  /** 判定"无法归因"所用的账户规模：审核时取该申请的余额快照 */
  reference_balance: number
  /** 账户规模超出可归因入账的部分：首登赠送、直接改库等无流水可查的钱 */
  unattributed: number
}

export interface AdminRefundDetail extends AdminRefundEntry {
  composition?: RefundBalanceComposition | null
}

export interface ListAdminRefundsParams {
  page?: number
  page_size?: number
  status?: RefundRequestStatus | ''
  search?: string
  user_id?: number
}

export async function listRefundRequests(
  params: ListAdminRefundsParams = {}
): Promise<PaginatedResponse<AdminRefundEntry>> {
  const { data } = await apiClient.get<PaginatedResponse<AdminRefundEntry>>('/admin/refund-requests', { params })
  return data
}

export async function getRefundRequest(id: number): Promise<AdminRefundDetail> {
  const { data } = await apiClient.get<AdminRefundDetail>(`/admin/refund-requests/${id}`)
  return data
}

/**
 * 标记已完成：后端会结清提交时冻结的余额并留下一笔余额流水。
 * note 可选（打款流水号、渠道单号等），与拒绝原因一样会展示给用户。
 */
export async function completeRefundRequest(id: number, note = ''): Promise<AdminRefundEntry> {
  const { data } = await apiClient.post<AdminRefundEntry>(`/admin/refund-requests/${id}/complete`, { note })
  return data
}

export async function rejectRefundRequest(id: number, reason: string): Promise<AdminRefundEntry> {
  const { data } = await apiClient.post<AdminRefundEntry>(`/admin/refund-requests/${id}/reject`, { reason })
  return data
}

export async function getRefundQRBlob(id: number): Promise<Blob> {
  const { data } = await apiClient.get<Blob>(`/admin/refund-requests/${id}/qr`, { responseType: 'blob' })
  return data
}

export const adminRefundsAPI = {
  listRefundRequests,
  getRefundRequest,
  completeRefundRequest,
  rejectRefundRequest,
  getRefundQRBlob
}
