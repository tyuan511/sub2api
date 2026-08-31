import { apiClient } from './client'

export interface SupportAttachment {
  id: number
  message_id: number
  original_name: string
  content_type: string
  size: number
  width: number
  height: number
  download_url: string
}

export interface SupportMessage {
  id: number
  sender_id: number
  sender_role: 'user' | 'admin'
  content: string
  attachments: SupportAttachment[]
  created_at: string
}

export interface SupportTicket {
  id: number
  user_id: number
  user_email?: string
  user_name?: string
  last_message_at: string
  last_message_preview?: string
  unread_count: number
  messages?: SupportMessage[]
}

export interface SupportListResult {
  items: SupportTicket[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface SupportListParams {
  page?: number
  page_size?: number
  search?: string
  user_id?: number
}

function formData(input: { content: string; files?: File[] }) {
  const data = new FormData()
  data.set('content', input.content)
  data.set('client_request_id', crypto.randomUUID())
  for (const file of input.files || []) data.append('attachments', file)
  return data
}

export async function listSupport(params: SupportListParams = {}): Promise<SupportListResult> {
  const { data } = await apiClient.get<SupportListResult>('/support', { params })
  return data
}

export async function getSupportTicket(id: number): Promise<SupportTicket> {
  const { data } = await apiClient.get<SupportTicket>(`/support/${id}`)
  return data
}

export async function createSupportTicket(input: { content: string; files?: File[] }): Promise<SupportTicket> {
  const { data } = await apiClient.post<SupportTicket>('/support', formData(input), { headers: { 'Content-Type': undefined } })
  return data
}

export async function replySupportTicket(id: number, content: string, files?: File[]): Promise<SupportTicket> {
  const { data } = await apiClient.post<SupportTicket>(`/support/${id}/replies`, formData({ content, files }), { headers: { 'Content-Type': undefined } })
  return data
}

export async function getSupportUnreadCount(): Promise<number> {
  const { data } = await apiClient.get<{ count: number }>('/support/unread-count')
  return data.count
}

export async function getSupportAttachmentBlob(id: number, admin = false): Promise<Blob> {
  const prefix = admin ? '/admin/support' : '/support'
  const { data } = await apiClient.get<Blob>(`${prefix}/attachments/${id}`, { responseType: 'blob' })
  return data
}
