import { apiClient } from '../client'

export type UpstreamType = 'sub2api' | 'newapi' | 'other'

export interface Upstream {
  id: number
  name: string
  type: UpstreamType
  website_url: string
  base_url: string
  user_id: number
  balance_threshold: number
  balance_multiplier: number
  quota_per_unit: number
  check_interval_seconds: number
  enabled: boolean
  balance: number | null
  balance_checked_at: string | null
  last_check_at: string | null
  last_error: string
  has_balance_api_key: boolean
  has_access_token: boolean
  created_at: string
  updated_at: string
}

export interface UpstreamInput {
  name: string
  type: UpstreamType
  website_url: string
  base_url: string
  user_id?: number
  balance_api_key?: string
  access_token?: string
  balance_threshold?: number
  balance_multiplier?: number
  quota_per_unit?: number
  check_interval_seconds?: number
  enabled?: boolean
}

export default {
  async list(): Promise<Upstream[]> {
    const { data } = await apiClient.get<Upstream[]>('/admin/upstreams')
    return data
  },
  async create(input: UpstreamInput): Promise<Upstream> {
    const { data } = await apiClient.post<Upstream>('/admin/upstreams', input)
    return data
  },
  async update(id: number, input: UpstreamInput): Promise<Upstream> {
    const { data } = await apiClient.put<Upstream>(`/admin/upstreams/${id}`, input)
    return data
  },
  async remove(id: number): Promise<void> {
    await apiClient.delete(`/admin/upstreams/${id}`)
  },
  async refresh(id: number): Promise<Upstream> {
    const { data } = await apiClient.post<Upstream>(`/admin/upstreams/${id}/refresh`)
    return data
  }
}
