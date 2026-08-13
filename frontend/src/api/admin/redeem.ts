/**
 * Admin Redeem Codes API endpoints
 * Handles redeem code generation and management for administrators
 */

import { apiClient } from '../client'
import type {
  RedeemCode,
  GenerateRedeemCodesRequest,
  RedeemCodeType,
  PaginatedResponse
} from '@/types'

export interface RedeemCodeQueryFilters {
  type?: RedeemCodeType
  status?: 'used' | 'expired' | 'unused'
  category?: string
  uncategorized?: boolean
  search?: string
  sort_by?: string
  sort_order?: 'asc' | 'desc'
}

/**
 * List all redeem codes with pagination
 * @param page - Page number (default: 1)
 * @param pageSize - Items per page (default: 20)
 * @param filters - Optional filters
 * @returns Paginated list of redeem codes
 */
export async function list(
  page: number = 1,
  pageSize: number = 20,
  filters?: RedeemCodeQueryFilters,
  options?: {
    signal?: AbortSignal
  }
): Promise<PaginatedResponse<RedeemCode>> {
  const { data } = await apiClient.get<PaginatedResponse<RedeemCode>>('/admin/redeem-codes', {
    params: {
      page,
      page_size: pageSize,
      ...filters
    },
    signal: options?.signal
  })
  return data
}

/**
 * Get redeem code by ID
 * @param id - Redeem code ID
 * @returns Redeem code details
 */
export async function getById(id: number): Promise<RedeemCode> {
  const { data } = await apiClient.get<RedeemCode>(`/admin/redeem-codes/${id}`)
  return data
}

/**
 * Generate new redeem codes
 * @param payload - Generation settings, including count, type, category, and value
 * @returns Array of generated redeem codes
 */
export async function generate(payload: GenerateRedeemCodesRequest): Promise<RedeemCode[]> {
  const { data } = await apiClient.post<RedeemCode[]>('/admin/redeem-codes/generate', payload)
  return data
}

/**
 * List distinct non-empty categories used by redeem codes.
 */
export async function listCategories(): Promise<string[]> {
  const { data } = await apiClient.get<{ categories: string[] }>(
    '/admin/redeem-codes/categories'
  )
  return data.categories
}

/**
 * Delete redeem code
 * @param id - Redeem code ID
 * @returns Success confirmation
 */
export async function deleteCode(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(`/admin/redeem-codes/${id}`)
  return data
}

/**
 * Batch delete redeem codes
 * @param ids - Array of redeem code IDs
 * @returns Success confirmation
 */
export async function batchDelete(ids: number[]): Promise<{
  deleted: number
  message: string
}> {
  const { data } = await apiClient.post<{
    deleted: number
    message: string
  }>('/admin/redeem-codes/batch-delete', { ids })
  return data
}

/**
 * Expire redeem code
 * @param id - Redeem code ID
 * @returns Updated redeem code
 */
export async function expire(id: number): Promise<RedeemCode> {
  const { data } = await apiClient.post<RedeemCode>(`/admin/redeem-codes/${id}/expire`)
  return data
}

/**
 * Export redeem codes to CSV
 * @param filters - Optional filters
 * @returns CSV data as blob
 */
export async function exportCodes(filters?: RedeemCodeQueryFilters): Promise<Blob> {
  const response = await apiClient.get('/admin/redeem-codes/export', {
    params: filters,
    responseType: 'blob'
  })
  return response.data
}

export const redeemAPI = {
  list,
  getById,
  generate,
  listCategories,
  delete: deleteCode,
  batchDelete,
  expire,
  exportCodes
}

export default redeemAPI
