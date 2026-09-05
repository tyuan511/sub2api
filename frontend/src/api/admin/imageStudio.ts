import { apiClient } from '../client'
import type { ImageStorageConfig } from './backup'
export interface ImageStorageProfile {
  id: number; name: string; config: ImageStorageConfig; secret_configured: boolean; file_count: number
}
export interface ImageStorageHistory { active_id: number; enabled: boolean; profiles: ImageStorageProfile[] }
export const imageStorageHistoryAPI = {
  async get(signal?: AbortSignal) { return (await apiClient.get<ImageStorageHistory>('/admin/backups/image-storage/history', { signal })).data },
  async migrate(from_id: number, to_id: number, signal?: AbortSignal) {
    return (await apiClient.post<{ moved: number; remaining: number }>('/admin/backups/image-storage/migrate', { from_id, to_id }, { timeout: 120000, signal })).data
  },
}
