/**
 * Admin API Keys API endpoints
 * Handles API key management for administrators
 */

import { apiClient } from '../client'
import type { ApiKey } from '@/types'

/**
 * Update an API key's group binding
 * @param id - API Key ID
 * @param groupId - Group ID (0 to unbind, positive to bind, null/undefined to skip)
 * @returns Updated API key
 */
export async function updateApiKeyGroup(id: number, groupId: number | null): Promise<ApiKey> {
  const { data } = await apiClient.put<ApiKey>(`/admin/api-keys/${id}`, {
    group_id: groupId === null ? 0 : groupId
  })
  return data
}

export const apiKeysAPI = {
  updateApiKeyGroup
}

export default apiKeysAPI
