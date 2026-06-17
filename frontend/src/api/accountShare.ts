import { apiClient } from './client'
import type { AccountLevel, AccountStatus, PaginatedResponse, Proxy, UsageProgress } from '@/types'

export type AccountShareListingStatus = 'active' | 'paused' | 'disabled'
export type AccountShareListingTab = 'using' | 'history' | 'all' | 'mine'

export interface AccountShareListing {
  id: number
  account_id: number
  owner_user_id: number
  owner_username?: string
  account_name?: string
  proxy_id?: number
  proxy?: Proxy
  status: AccountShareListingStatus
  seat_limit: number
  active_seats: number
  rate_multiplier: number
  allowed_models: string[]
  per_user_concurrency: number
  account_concurrency: number
  hourly_rate: number
  hourly_fee_waiver_minimum: number
  min_balance_required: number
  codex_cli_only: boolean
  codex_5h_limit_percent: number
  codex_7d_limit_percent: number
  account_level?: AccountLevel
  account_plan_type?: string
  account_status?: AccountStatus | string
  account_schedulable?: boolean
  current_concurrency?: number
  account_expires_at?: string
  subscription_expires_at?: string
  account_last_used_at?: string
  rate_limited_at?: string
  rate_limit_reset_at?: string
  overload_until?: string
  temp_unschedulable_until?: string
  temp_unschedulable_reason?: string
  codex_quota_protection_reason?: string
  codex_quota_protection_reset_at?: string
  codex_5h_usage?: UsageProgress | null
  codex_7d_usage?: UsageProgress | null
  codex_usage_updated_at?: string
  current_membership_id?: number
  current_api_key_id?: number
  current_joined_at?: string
  current_paid_until?: string
  current_billed_until?: string
  current_idle_timeout_minutes?: number
  current_last_request_at?: string
  current_idle_expires_at?: string
  last_used_membership_id?: number
  last_used_at?: string
  editing_by_user_id?: number
  editing_by_username?: string
  editing_expires_at?: string
  editing_mine: boolean
  edit_session_id?: string
  created_at: string
  updated_at: string
}

export interface AccountShareMembership {
  id: number
  listing_id: number
  account_id: number
  consumer_user_id: number
  owner_user_id?: number
  api_key_id: number
  status: 'active' | 'ended'
  hourly_rate_snapshot?: number
  hourly_fee_waiver_minimum_snapshot?: number
  idle_timeout_minutes: number
  joined_at: string
  last_request_at?: string
  ended_at?: string
  ended_reason?: string
  paid_until?: string
  billed_until?: string
  created_at: string
  updated_at: string
}

export interface AccountShareEndMembershipIntent {
  membership_id: number
  token: string
  expires_at: string
}

export interface AccountShareAuthURLResponse {
  auth_url: string
  session_id: string
}

export interface CreateAccountShareOpenAIRequest {
  session_id: string
  code: string
  state: string
  redirect_uri?: string
  proxy_id: number
  name?: string
  notes?: string
  concurrency: number
  seat_limit: number
  rate_multiplier: number
  allowed_models: string[]
  per_user_concurrency: number
  hourly_rate: number
  hourly_fee_waiver_minimum?: number
  min_balance_required?: number
  codex_cli_only?: boolean
  codex_5h_limit_percent?: number
  codex_7d_limit_percent?: number
}

export interface UpdateAccountShareListingRequest {
  name?: string
  proxy_id?: number
  status?: AccountShareListingStatus
  seat_limit?: number
  rate_multiplier?: number
  allowed_models?: string[]
  per_user_concurrency?: number
  hourly_rate?: number
  hourly_fee_waiver_minimum?: number
  min_balance_required?: number
  codex_cli_only?: boolean
  codex_5h_limit_percent?: number
  codex_7d_limit_percent?: number
  concurrency?: number
  edit_session_id?: string
  force_active_edit?: boolean
}

export interface AccountShareListingEditSessionRequest {
  session_id?: string
  force?: boolean
}

export interface AccountShareListingFilters {
  tab?: AccountShareListingTab
  seat_limit?: number
  search?: string
  status?: AccountShareListingStatus | 'all' | ''
  available_only?: boolean
  per_user_concurrency_min?: number
  per_user_concurrency_max?: number
  min_balance_required_min?: number
  min_balance_required_max?: number
  hourly_rate_min?: number
  hourly_rate_max?: number
  hourly_fee_waiver_min?: number
  hourly_fee_waiver_max?: number
  models?: string[]
  account_level?: AccountLevel | 'all' | ''
}

export interface CreateAccountShareProxyRequest {
  name?: string
  protocol: Proxy['protocol']
  host: string
  port: number
  username?: string
  password?: string
}

export interface JoinAccountShareListingRequest {
  api_key_id: number
  idle_timeout_minutes?: number
}

export async function generateOpenAIAuthURL(payload: {
  proxy_id: number
  redirect_uri?: string
}): Promise<AccountShareAuthURLResponse> {
  const { data } = await apiClient.post<AccountShareAuthURLResponse>('/account-share/openai/auth-url', payload)
  return data
}

export async function exchangeOpenAICode(payload: CreateAccountShareOpenAIRequest): Promise<AccountShareListing> {
  const { data } = await apiClient.post<AccountShareListing>('/account-share/openai/exchange-code', payload)
  return data
}

export async function listListings(
  page = 1,
  pageSize = 20,
  filters?: AccountShareListingFilters,
  options: { signal?: AbortSignal } = {}
): Promise<PaginatedResponse<AccountShareListing>> {
  const params: Record<string, unknown> = {
    page,
    page_size: pageSize
  }
  if (filters) {
    for (const [key, value] of Object.entries(filters)) {
      if (value === undefined || value === null || value === '') continue
      if (Array.isArray(value)) {
        if (value.length > 0) params[key] = value.join(',')
        continue
      }
      params[key] = value
    }
  }
  const { data } = await apiClient.get<PaginatedResponse<AccountShareListing>>('/account-share/listings', {
    params,
    signal: options.signal
  })
  return data
}

export async function listProxies(): Promise<Proxy[]> {
  const { data } = await apiClient.get<Proxy[]>('/account-share/proxies')
  return data
}

export async function createProxy(payload: CreateAccountShareProxyRequest): Promise<Proxy> {
  const { data } = await apiClient.post<Proxy>('/account-share/proxies', payload)
  return data
}

export async function getListing(id: number): Promise<AccountShareListing> {
  const { data } = await apiClient.get<AccountShareListing>(`/account-share/listings/${id}`)
  return data
}

export async function updateListing(id: number, payload: UpdateAccountShareListingRequest): Promise<AccountShareListing> {
  const { data } = await apiClient.patch<AccountShareListing>(`/account-share/listings/${id}`, payload)
  return data
}

export async function beginListingEdit(id: number, payload: AccountShareListingEditSessionRequest = {}): Promise<AccountShareListing> {
  const { data } = await apiClient.post<AccountShareListing>(`/account-share/listings/${id}/edit-session`, payload)
  return data
}

export async function releaseListingEdit(id: number, sessionID: string): Promise<AccountShareListing> {
  const { data } = await apiClient.post<AccountShareListing>(`/account-share/listings/${id}/edit-session/release`, { session_id: sessionID })
  return data
}

export async function joinListing(id: number, payload: JoinAccountShareListingRequest): Promise<AccountShareMembership> {
  const { data } = await apiClient.post<AccountShareMembership>(`/account-share/listings/${id}/join`, payload)
  return data
}

export async function updateMembershipIdleTimeout(id: number, idleTimeoutMinutes: number): Promise<AccountShareMembership> {
  const { data } = await apiClient.patch<AccountShareMembership>(`/account-share/memberships/${id}/idle-timeout`, {
    idle_timeout_minutes: idleTimeoutMinutes
  })
  return data
}

export async function createEndMembershipIntent(id: number): Promise<AccountShareEndMembershipIntent> {
  const { data } = await apiClient.post<AccountShareEndMembershipIntent>(`/account-share/memberships/${id}/end-intent`)
  return data
}

export async function endMembership(id: number, token: string): Promise<AccountShareMembership> {
  const { data } = await apiClient.post<AccountShareMembership>(`/account-share/memberships/${id}/end`, { token })
  return data
}

export const accountShareAPI = {
  generateOpenAIAuthURL,
  exchangeOpenAICode,
  listProxies,
  createProxy,
  listListings,
  getListing,
  updateListing,
  beginListingEdit,
  releaseListingEdit,
  joinListing,
  updateMembershipIdleTimeout,
  createEndMembershipIntent,
  endMembership
}

export default accountShareAPI
