import { apiClient } from '../client'
import type { SupportListParams, SupportListResult, SupportTicket } from '../support'

export interface TelegramConfig {
  enabled: boolean
  bot_username: string
  token_set: boolean
  webhook_set: boolean
  webhook_base_url: string
}

export interface TelegramBinding {
  bound: boolean
  enabled: boolean
  telegram_username?: string
  notify_new_ticket: boolean
  notify_user_reply: boolean
  bound_at?: string
  last_success_at?: string
  last_error?: string
}

export interface SupportUserSearchItem {
  user_id: number
  user_email: string
  user_name: string
  ticket_id?: number
  last_message_at?: string
  last_message_preview?: string
}

export async function listAdminSupport(params: SupportListParams = {}): Promise<SupportListResult> {
  const { data } = await apiClient.get<SupportListResult>('/admin/support', { params })
  return data
}

export async function getAdminSupportTicket(id: number): Promise<SupportTicket> {
  const { data } = await apiClient.get<SupportTicket>(`/admin/support/${id}`)
  return data
}

export async function searchAdminSupportUsers(search: string): Promise<SupportUserSearchItem[]> {
  const { data } = await apiClient.get<SupportUserSearchItem[]>('/admin/support/users', { params: { search, limit: 20 } })
  return data
}

export async function startAdminSupportConversation(userID: number, content: string, files?: File[]): Promise<SupportTicket> {
  const form = new FormData()
  form.set('user_id', String(userID))
  form.set('content', content)
  form.set('client_request_id', crypto.randomUUID())
  for (const file of files || []) form.append('attachments', file)
  const { data } = await apiClient.post<SupportTicket>('/admin/support/conversations', form, { headers: { 'Content-Type': undefined } })
  return data
}

export async function replyAdminSupportTicket(id: number, content: string, files?: File[]): Promise<SupportTicket> {
  const form = new FormData()
  form.set('content', content)
  form.set('client_request_id', crypto.randomUUID())
  for (const file of files || []) form.append('attachments', file)
  const { data } = await apiClient.post<SupportTicket>(`/admin/support/${id}/replies`, form, { headers: { 'Content-Type': undefined } })
  return data
}

export async function getAdminSupportUnreadCount(): Promise<number> {
  const { data } = await apiClient.get<{ count: number }>('/admin/support/unread-count')
  return data.count
}

export async function getTelegramConfig(): Promise<TelegramConfig> {
  const { data } = await apiClient.get<TelegramConfig>('/admin/support/telegram/config')
  return data
}

export async function saveTelegramConfig(input: { enabled: boolean; bot_token: string; webhook_base_url: string }): Promise<TelegramConfig> {
  const { data } = await apiClient.put<TelegramConfig>('/admin/support/telegram/config', input)
  return data
}

export async function getTelegramBinding(): Promise<TelegramBinding> {
  const { data } = await apiClient.get<TelegramBinding>('/admin/support/telegram/binding')
  return data
}

export async function createTelegramBindLink(): Promise<{ url: string; expires_at: string }> {
  const { data } = await apiClient.post<{ url: string; expires_at: string }>('/admin/support/telegram/binding/link')
  return data
}

export async function updateTelegramBinding(input: Omit<TelegramBinding, 'bound' | 'telegram_username' | 'bound_at' | 'last_success_at' | 'last_error'>): Promise<TelegramBinding> {
  const { data } = await apiClient.put<TelegramBinding>('/admin/support/telegram/binding', input)
  return data
}

export async function deleteTelegramBinding(): Promise<void> { await apiClient.delete('/admin/support/telegram/binding') }
export async function testTelegramBinding(): Promise<void> { await apiClient.post('/admin/support/telegram/binding/test') }
