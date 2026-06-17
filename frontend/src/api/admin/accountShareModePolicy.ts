import { apiClient } from '../client'

export interface AccountShareModePolicy {
  id?: number
  platform: string
  platform_share_ratio: number
  owner_share_ratio: number
  enabled: boolean
  version: number
}

export interface UpdateAccountShareModePolicyRequest {
  platform?: string
  platform_share_ratio?: number
  owner_share_ratio?: number
  enabled?: boolean
}

export const accountShareModePolicyAPI = {
  async get(platform = 'openai'): Promise<AccountShareModePolicy> {
    const { data } = await apiClient.get<AccountShareModePolicy>('/admin/account-share-mode-policy', {
      params: { platform }
    })
    return data
  },

  async update(payload: UpdateAccountShareModePolicyRequest): Promise<AccountShareModePolicy> {
    const { data } = await apiClient.put<AccountShareModePolicy>('/admin/account-share-mode-policy', payload)
    return data
  }
}

export default accountShareModePolicyAPI
