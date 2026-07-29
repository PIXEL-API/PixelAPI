import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'

export type AccountShareBillingIntentAdminStatus =
  | 'created'
  | 'in_flight'
  | 'ready'
  | 'processing'
  | 'settled'
  | 'cancelled'
  | 'failed'
  | 'needs_attention'
  | string

export interface AccountShareBillingIntentAdminRecord {
  id: number
  request_id: string
  dispatch_id: string
  attempt_no: number
  api_key_id: number
  membership_id: number
  listing_id: number
  account_id: number
  status: AccountShareBillingIntentAdminStatus
  state_token: number
  last_error_code?: string
  last_error_message?: string
  forward_started_at?: string
  completed_at?: string
  created_at: string
  updated_at: string
}

export interface AccountShareBillingAdminRequestOptions {
  signal?: AbortSignal
}

export async function listBillingIntentsNeedingAttention(
  page = 1,
  pageSize = 20,
  options: AccountShareBillingAdminRequestOptions = {}
): Promise<PaginatedResponse<AccountShareBillingIntentAdminRecord>> {
  const { data } = await apiClient.get<PaginatedResponse<AccountShareBillingIntentAdminRecord>>(
    '/admin/account-share/billing-intents/needs-attention',
    {
      params: {
        page,
        page_size: pageSize
      },
      signal: options.signal
    }
  )
  return data
}

export async function getBillingIntentForAdmin(
  intentID: number,
  options: AccountShareBillingAdminRequestOptions = {}
): Promise<AccountShareBillingIntentAdminRecord> {
  const { data } = await apiClient.get<AccountShareBillingIntentAdminRecord>(
    `/admin/account-share/billing-intents/${intentID}`,
    {
      signal: options.signal
    }
  )
  return data
}

export const accountShareBillingAdminAPI = {
  listNeedsAttention: listBillingIntentsNeedingAttention,
  getDetail: getBillingIntentForAdmin
}

export default accountShareBillingAdminAPI
