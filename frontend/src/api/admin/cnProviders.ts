import { apiClient } from '../client'
import type { CNProviderObservedLimit, CNProviderQuotaWindow } from '@/types'

export interface CNQuotaTier {
  window: CNProviderQuotaWindow
  used_percent: number
  reset_at?: string
}

export interface CNProviderQuotaProbeResult {
  provider: string
  success: boolean
  credential_valid: boolean | null
  tiers?: CNQuotaTier[]
  source?: string
  automatic_query_supported?: boolean
  observed_limits?: CNProviderObservedLimit[]
  plan_level?: string
  status_code?: number
  fetched_at: number
  persisted: boolean
  error?: string
}

export interface CNProviderBalanceEntry { currency: string; balance: number }

export interface CNProviderBalanceResult {
  provider: string
  success: boolean
  balance: number
  currency?: string
  balances?: CNProviderBalanceEntry[]
  available: boolean
  status_code?: number
  fetched_at: number
  persisted: boolean
  error?: string
}
export type CNProviderBalanceProbeResult = CNProviderBalanceResult

export async function queryUserQuota(id: number): Promise<CNProviderQuotaProbeResult> {
  const { data } = await apiClient.get<CNProviderQuotaProbeResult>(`/accounts/${id}/cn-quota`)
  return data
}

export async function queryUserBalance(id: number): Promise<CNProviderBalanceResult> {
  const { data } = await apiClient.get<CNProviderBalanceResult>(`/accounts/${id}/cn-balance`)
  return data
}

export async function queryQuota(id: number): Promise<CNProviderQuotaProbeResult> {
  const { data } = await apiClient.get<CNProviderQuotaProbeResult>(`/admin/cn-providers/accounts/${id}/quota`)
  return data
}

export async function queryBalance(id: number): Promise<CNProviderBalanceResult> {
  const { data } = await apiClient.get<CNProviderBalanceResult>(`/admin/cn-providers/accounts/${id}/balance`)
  return data
}

export default { queryQuota, queryBalance }
