import type { PaginatedResponse } from '@/types'
import { apiClient } from '../client'

export interface AccountShareQuotaLimits {
  max_live_rooms: number
  max_room_creates_24_hours: number
  max_accounts_per_room: number
  max_room_accounts_per_owner: number
}

export type AccountShareQuotaScope = 'global' | 'owner'
export type AccountShareResolvedQuotaSource = 'global' | 'owner_override'
export type AccountShareQuotaPolicyStatus = 'active' | 'revoked' | string
export type AccountShareQuotaOverrideKind = 'default' | 'manual' | 'grandfather' | string

export interface AccountShareQuotaPolicy {
  id: number
  scope_type: AccountShareQuotaScope
  owner_user_id?: number
  version: number
  status: AccountShareQuotaPolicyStatus
  override_kind: AccountShareQuotaOverrideKind
  limits: AccountShareQuotaLimits
  effective_at: string
  expires_at?: string
  reason: string
  actor_user_id?: number
  actor_user_id_snapshot: number
  created_at: string
}

export interface AccountShareResolvedQuota {
  limits: AccountShareQuotaLimits
  source: AccountShareResolvedQuotaSource
  policy_id: number
  policy_version: number
  override_kind: AccountShareQuotaOverrideKind
  override_expires_at?: string
  growth_blocked: boolean
}

export interface AccountShareQuotaUsage {
  live_rooms: number
  room_creates_24_hours: number
  owner_room_accounts: number
  largest_room_accounts: number
}

export interface AccountShareQuotaAdminState {
  global_policy: AccountShareQuotaPolicy
  owner_policy?: AccountShareQuotaPolicy
  effective_quota: AccountShareResolvedQuota
  usage: AccountShareQuotaUsage
}

export interface AccountShareGrandfatherCandidate {
  owner_user_id: number
  usage: AccountShareQuotaUsage
  exceeded_dimensions: string[]
  effective_quota: AccountShareResolvedQuota
  latest_owner_version: number
  suggested_limits: AccountShareQuotaLimits
  preview_fingerprint: string
  as_of: string
}

export interface AccountShareGrandfatherCandidateItem {
  owner_user_id: number
  expected_version: number
  preview_usage: AccountShareQuotaUsage
  preview_fingerprint: string
}

export interface BatchGrandfatherAccountShareQuotaRequest {
  items: AccountShareGrandfatherCandidateItem[]
  expires_at: string
  reason: string
  confirmed: true
}

export type AccountShareGrandfatherBatchItemStatus =
  | 'applied'
  | 'skipped'
  | 'conflict'
  | 'failed'

export interface AccountShareGrandfatherBatchItemResult {
  owner_user_id: number
  status: AccountShareGrandfatherBatchItemStatus
  result_code?: string
  message?: string
  policy_id?: number
  policy_version?: number
  expires_at?: string
}

interface AccountShareQuotaMutationBase {
  expected_version: number
  reason: string
  confirmed: true
}

export interface UpdateAccountShareGlobalQuotaRequest extends AccountShareQuotaMutationBase {
  limits: AccountShareQuotaLimits
  effective_at?: string
}

export interface UpsertAccountShareOwnerQuotaRequest extends AccountShareQuotaMutationBase {
  limits: AccountShareQuotaLimits
  effective_at?: string
  expires_at: string
}

export interface GrandfatherAccountShareOwnerQuotaRequest extends AccountShareQuotaMutationBase {
  effective_at?: string
  expires_at: string
}

export type RevokeAccountShareOwnerQuotaRequest = AccountShareQuotaMutationBase

export interface AccountShareQuotaRequestOptions {
  signal?: AbortSignal
}

function idempotencyHeaders(idempotencyKey: string): Record<string, string> {
  return {
    'Idempotency-Key': idempotencyKey
  }
}

export async function getGlobalAccountShareQuota(
  options: AccountShareQuotaRequestOptions = {}
): Promise<AccountShareQuotaPolicy> {
  const { data } = await apiClient.get<AccountShareQuotaPolicy>(
    '/admin/account-share/quotas/global',
    { signal: options.signal }
  )
  return data
}

export async function updateGlobalAccountShareQuota(
  payload: UpdateAccountShareGlobalQuotaRequest,
  idempotencyKey: string
): Promise<AccountShareQuotaPolicy> {
  const { data } = await apiClient.put<AccountShareQuotaPolicy>(
    '/admin/account-share/quotas/global',
    payload,
    { headers: idempotencyHeaders(idempotencyKey) }
  )
  return data
}

export async function getOwnerAccountShareQuota(
  ownerUserID: number,
  options: AccountShareQuotaRequestOptions = {}
): Promise<AccountShareQuotaAdminState> {
  const { data } = await apiClient.get<AccountShareQuotaAdminState>(
    `/admin/account-share/quotas/owners/${ownerUserID}`,
    { signal: options.signal }
  )
  return data
}

export async function upsertOwnerAccountShareQuota(
  ownerUserID: number,
  payload: UpsertAccountShareOwnerQuotaRequest,
  idempotencyKey: string
): Promise<AccountShareQuotaPolicy> {
  const { data } = await apiClient.put<AccountShareQuotaPolicy>(
    `/admin/account-share/quotas/owners/${ownerUserID}`,
    payload,
    { headers: idempotencyHeaders(idempotencyKey) }
  )
  return data
}

export async function grandfatherOwnerAccountShareQuota(
  ownerUserID: number,
  payload: GrandfatherAccountShareOwnerQuotaRequest,
  idempotencyKey: string
): Promise<AccountShareQuotaPolicy> {
  const { data } = await apiClient.post<AccountShareQuotaPolicy>(
    `/admin/account-share/quotas/owners/${ownerUserID}/grandfather`,
    payload,
    { headers: idempotencyHeaders(idempotencyKey) }
  )
  return data
}

export async function revokeOwnerAccountShareQuota(
  ownerUserID: number,
  payload: RevokeAccountShareOwnerQuotaRequest,
  idempotencyKey: string
): Promise<AccountShareQuotaPolicy> {
  const { data } = await apiClient.post<AccountShareQuotaPolicy>(
    `/admin/account-share/quotas/owners/${ownerUserID}/revoke`,
    payload,
    { headers: idempotencyHeaders(idempotencyKey) }
  )
  return data
}

export async function listAccountShareQuotaAudit(
  scopeType: AccountShareQuotaScope,
  page = 1,
  pageSize = 20,
  ownerUserID?: number,
  options: AccountShareQuotaRequestOptions = {}
): Promise<PaginatedResponse<AccountShareQuotaPolicy>> {
  const { data } = await apiClient.get<PaginatedResponse<AccountShareQuotaPolicy>>(
    '/admin/account-share/quotas/audit',
    {
      params: {
        scope_type: scopeType,
        page,
        page_size: pageSize,
        ...(scopeType === 'owner' && ownerUserID ? { owner_id: ownerUserID } : {})
      },
      signal: options.signal
    }
  )
  return data
}

export async function listAccountShareGrandfatherCandidates(
  page = 1,
  pageSize = 20,
  options: AccountShareQuotaRequestOptions = {}
): Promise<PaginatedResponse<AccountShareGrandfatherCandidate>> {
  const { data } = await apiClient.get<PaginatedResponse<AccountShareGrandfatherCandidate>>(
    '/admin/account-share/quotas/grandfather-candidates',
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

export async function batchGrandfatherAccountShareQuotas(
  payload: BatchGrandfatherAccountShareQuotaRequest,
  idempotencyKey: string
): Promise<AccountShareGrandfatherBatchItemResult[]> {
  const { data } = await apiClient.post<AccountShareGrandfatherBatchItemResult[]>(
    '/admin/account-share/quotas/grandfather/batch',
    payload,
    { headers: idempotencyHeaders(idempotencyKey) }
  )
  return data
}

export const accountShareQuotaAdminAPI = {
  getGlobal: getGlobalAccountShareQuota,
  updateGlobal: updateGlobalAccountShareQuota,
  getOwner: getOwnerAccountShareQuota,
  upsertOwner: upsertOwnerAccountShareQuota,
  grandfatherOwner: grandfatherOwnerAccountShareQuota,
  revokeOwner: revokeOwnerAccountShareQuota,
  listAudit: listAccountShareQuotaAudit,
  listGrandfatherCandidates: listAccountShareGrandfatherCandidates,
  batchGrandfather: batchGrandfatherAccountShareQuotas
}

export default accountShareQuotaAdminAPI
