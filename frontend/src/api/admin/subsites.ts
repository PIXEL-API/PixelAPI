import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'

export interface Subsite {
  id: number
  subsite_id: string
  name: string
  public_url: string
  region: string
  capabilities: string[]
  status: string
  max_qps: number
  max_concurrency: number
  version: string
  last_heartbeat_at?: string
  health_score: number
  last_seen_ip: string
  metadata?: Record<string, unknown>
  created_at: string
  updated_at: string
}

export interface AccountLease {
  id: number
  lease_id: string
  subsite_id: string
  account_id: number
  group_id?: number | null
  group_name?: string | null
  account_name?: string | null
  platform: string
  status: string
  max_concurrency: number
  max_requests: number
  max_tokens: number
  used_requests: number
  used_tokens: number
  assigned_at: string
  expires_at: string
  renewed_at?: string
  released_at?: string
  created_at: string
  updated_at: string
}

export interface CreateSubsiteRequest {
  subsite_id?: string
  name: string
  public_url: string
  region?: string
  capabilities?: string[]
  max_qps?: number
  max_concurrency?: number
  version?: string
}

export interface CreateSubsiteResult {
  subsite: Subsite
  secret: string
}

export interface ResetSubsiteSecretResult {
  subsite: Subsite
  secret: string
}

export interface UpdateSubsiteRequest {
  name?: string
  public_url?: string
  region?: string
  capabilities?: string[]
  max_qps?: number
  max_concurrency?: number
  version?: string
  metadata?: Record<string, unknown>
}

export interface CreateLeaseRequest {
  account_id: number
  group_id: number
  max_concurrency?: number
  max_requests?: number
  max_tokens?: number
  ttl_seconds?: number
}

export interface RenewLeaseRequest {
  ttl_seconds: number
}

export interface UpdateLeaseRequest {
  max_concurrency?: number
  max_requests?: number
  max_tokens?: number
}

export async function list(
  page = 1,
  pageSize = 20,
  filters?: { status?: string; search?: string }
): Promise<PaginatedResponse<Subsite>> {
  const { data } = await apiClient.get<PaginatedResponse<Subsite>>('/admin/subsites', {
    params: {
      page,
      page_size: pageSize,
      ...filters
    }
  })
  return data
}

export async function create(payload: CreateSubsiteRequest): Promise<CreateSubsiteResult> {
  const { data } = await apiClient.post<CreateSubsiteResult>('/admin/subsites', payload)
  return data
}

export async function update(subsiteID: string, payload: UpdateSubsiteRequest): Promise<Subsite> {
  const { data } = await apiClient.patch<Subsite>(`/admin/subsites/${subsiteID}`, payload)
  return data
}

export async function activate(subsiteID: string): Promise<{ status: string }> {
  const { data } = await apiClient.post<{ status: string }>(`/admin/subsites/${subsiteID}/activate`)
  return data
}

export async function pause(subsiteID: string): Promise<{ status: string }> {
  const { data } = await apiClient.post<{ status: string }>(`/admin/subsites/${subsiteID}/pause`)
  return data
}

export async function resume(subsiteID: string): Promise<{ status: string }> {
  const { data } = await apiClient.post<{ status: string }>(`/admin/subsites/${subsiteID}/resume`)
  return data
}

export async function resetSecret(subsiteID: string): Promise<ResetSubsiteSecretResult> {
  const { data } = await apiClient.post<ResetSubsiteSecretResult>(`/admin/subsites/${subsiteID}/reset-secret`)
  return data
}

export async function listLeases(
  subsiteID: string,
  page = 1,
  pageSize = 20
): Promise<PaginatedResponse<AccountLease>> {
  const { data } = await apiClient.get<PaginatedResponse<AccountLease>>(`/admin/subsites/${subsiteID}/leases`, {
    params: {
      page,
      page_size: pageSize
    }
  })
  return data
}

export async function listLeaseActiveAccountIds(subsiteID: string): Promise<number[]> {
  const { data } = await apiClient.get<{ account_ids: number[] }>(`/admin/subsites/${subsiteID}/leases/active-account-ids`)
  return data.account_ids || []
}

export async function createLease(subsiteID: string, payload: CreateLeaseRequest): Promise<AccountLease> {
  const { data } = await apiClient.post<AccountLease>(`/admin/subsites/${subsiteID}/leases`, payload)
  return data
}

export async function drainLease(subsiteID: string, leaseID: string): Promise<AccountLease> {
  const { data } = await apiClient.post<AccountLease>(`/admin/subsites/${subsiteID}/leases/${leaseID}/drain`)
  return data
}

export async function releaseLease(subsiteID: string, leaseID: string): Promise<AccountLease> {
  const { data } = await apiClient.post<AccountLease>(`/admin/subsites/${subsiteID}/leases/${leaseID}/release`)
  return data
}

export async function renewLease(subsiteID: string, leaseID: string, payload: RenewLeaseRequest): Promise<AccountLease> {
  const { data } = await apiClient.post<AccountLease>(`/admin/subsites/${subsiteID}/leases/${leaseID}/renew`, payload)
  return data
}

export async function updateLease(subsiteID: string, leaseID: string, payload: UpdateLeaseRequest): Promise<AccountLease> {
  const { data } = await apiClient.patch<AccountLease>(`/admin/subsites/${subsiteID}/leases/${leaseID}`, payload)
  return data
}

export async function deleteLease(subsiteID: string, leaseID: string): Promise<void> {
  await apiClient.delete(`/admin/subsites/${subsiteID}/leases/${leaseID}`)
}

export default {
  list,
  create,
  update,
  activate,
  pause,
  resume,
  resetSecret,
  listLeases,
  listLeaseActiveAccountIds,
  createLease,
  drainLease,
  releaseLease,
  renewLease,
  updateLease,
  deleteLease
}
