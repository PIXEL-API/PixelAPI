import { apiClient } from './client'
import type { AccountLevel, AccountStatus, PaginatedResponse, Proxy, UsageProgress } from '@/types'

export type AccountShareListingStatus = 'active' | 'paused' | 'disabled'
export type AccountShareListingTab = 'using' | 'history' | 'all' | 'mine'
export type AccountShareListingSortBy = 'account_concurrency' | 'per_user_concurrency' | 'min_balance_required' | 'hourly_rate' | 'hourly_fee_waiver' | 'rate_multiplier' | 'remaining_seats' | 'rating' | 'updated_at'
export type AccountShareListingSortOrder = 'asc' | 'desc'
export type AccountShareListingSortKey = `${AccountShareListingSortBy}:${AccountShareListingSortOrder}`
export type AccountShareListingFeatureTag = 'hourly_fee_waiver' | 'image_generation' | 'no_hourly_fee' | 'codex_cli_only' | 'non_codex_cli_only' | 'available'
export type AccountShareMySpendRange = 'current_membership' | 'today' | '7d'

export interface AccountShareListing {
  id: number
  account_id: number
  platform: 'openai' | 'anthropic' | string
  owner_user_id: number
  owner_username?: string
  account_name?: string
  proxy_id?: number
  proxy?: Proxy
  status: AccountShareListingStatus
  seat_limit: number
  active_seats: number
  account_identity_id?: number
  rating_count: number
  rating_score_sum: number
  rating_avg: number
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
  anthropic_5h_limit_percent?: number
  anthropic_7d_limit_percent?: number
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
  anthropic_quota_protection_reason?: string
  anthropic_quota_protection_reset_at?: string
  anthropic_5h_usage?: UsageProgress | null
  anthropic_7d_usage?: UsageProgress | null
  anthropic_usage_updated_at?: string
  current_membership_id?: number
  current_api_key_id?: number
  current_joined_at?: string
  current_paid_until?: string
  current_billed_until?: string
  current_idle_timeout_minutes?: number
  current_last_request_at?: string
  current_idle_expires_at?: string
  queue_membership_id?: number
  queue_api_key_id?: number
  queue_rank?: number
  queue_status?: 'active' | 'queued' | string
  queue_idle_timeout_minutes?: number
  queue_dispatch_cooldown_until?: string
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

export interface AccountShareRecommendationRequest {
  platform: 'openai' | 'anthropic' | string
  model: string
  api_key_id: number
  request_count: number
  active_hours: number
  input_tokens_per_request: number
  output_tokens_per_request: number
  cache_creation_tokens_per_request?: number
  cache_read_tokens_per_request?: number
  image_input_tokens_per_request?: number
  image_cache_read_tokens_per_request?: number
  image_output_tokens_per_request?: number
  size_tier?: string
  service_tier?: string
  limit?: number
}

export interface AccountShareRecommendationUsageProfileRequest {
  platform: 'openai' | 'anthropic' | string
  model?: string
  days?: number
}

export interface AccountShareRecommendationUsageProfile {
  platform: string
  model?: string
  days: number
  start_time: string
  end_time: string
  has_history: boolean
  model_matched: boolean
  used_model_fallback: boolean
  capped: boolean
  total_requests: number
  active_hour_buckets: number
  request_count: number
  active_hours: number
  input_tokens_per_request: number
  output_tokens_per_request: number
  cache_creation_tokens_per_request: number
  cache_read_tokens_per_request: number
  image_output_tokens_per_request: number
}

export interface AccountShareRecommendationUsage {
  platform: string
  model: string
  api_key_id?: number
  request_count: number
  active_hours: number
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  image_input_tokens: number
  image_cache_read_tokens: number
  image_output_tokens: number
  size_tier?: string
  service_tier?: string
  limit: number
}

export interface AccountShareRecommendationEstimate {
  billing_mode: 'token' | 'per_request' | 'image' | string
  base_request_cost: number
  request_cost: number
  per_request_cost: number
  hourly_gross_cost: number
  hourly_waived_cost: number
  hourly_net_cost: number
  waiver_required_amount: number
  waiver_usage_amount: number
  waiver_eligible: boolean
  total_cost: number
  upfront_required: number
  effective_rate_multiplier: number
  effective_hourly_rate: number
  owner_self_use: boolean
}

export interface AccountShareRecommendationCandidate {
  rank: number
  listing: AccountShareListing
  estimate: AccountShareRecommendationEstimate
  score: number
  tags: string[]
  reasons: string[]
  warnings?: string[]
}

export interface AccountShareRecommendationResult {
  input: AccountShareRecommendationUsage
  candidate_count: number
  items: AccountShareRecommendationCandidate[]
  recommended?: AccountShareRecommendationCandidate
}

export interface AccountShareMembership {
  id: number
  listing_id: number
  account_id: number
  consumer_user_id: number
  owner_user_id?: number
  api_key_id: number
  status: 'active' | 'queued' | 'ended'
  queue_rank: number
  hourly_rate_snapshot?: number
  hourly_fee_waiver_minimum_snapshot?: number
  idle_timeout_minutes: number
  joined_at: string
  last_request_at?: string
  ended_at?: string
  ended_reason?: string
  paid_until?: string
  billed_until?: string
  dispatch_failed_at?: string
  dispatch_cooldown_until?: string
  created_at: string
  updated_at: string
}

export interface AccountShareMySpendParams {
  range?: AccountShareMySpendRange
  membership_id?: number
  timezone?: string
}

export interface AccountShareMySpendListing {
  id: number
  account_id: number
  account_name?: string
  platform: 'openai' | 'anthropic' | string
  owner_user_id: number
  owner_username?: string
}

export interface AccountShareMySpendMembership {
  id: number
  api_key_id: number
  status: 'active' | 'queued' | 'ended' | string
  queue_rank: number
  joined_at: string
  last_request_at?: string
  ended_at?: string
  ended_reason?: string
  paid_until?: string
  billed_until?: string
  hourly_rate: number
  waiver_minimum: number
  idle_timeout_minutes: number
}

export interface AccountShareMySpendModelBreakdown {
  model: string
  request_count: number
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  total_tokens: number
  request_cost: number
  average_request_cost: number
}

export interface AccountShareMySpendSummary {
  range: AccountShareMySpendRange
  start_time: string
  end_time: string
  listing: AccountShareMySpendListing
  membership?: AccountShareMySpendMembership
  request_count: number
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  total_tokens: number
  request_cost: number
  hourly_charge: number
  hourly_refund: number
  hourly_waiver_refund: number
  hourly_net_cost: number
  total_cost: number
  last_activity_at?: string
  model_breakdown: AccountShareMySpendModelBreakdown[]
}

export interface AccountShareEndMembershipIntent {
  membership_id: number
  token: string
  expires_at: string
}

export interface AccountShareReview {
  id: number
  account_identity_id: number
  listing_id?: number
  account_id?: number
  membership_id?: number
  owner_user_id: number
  owner_username?: string
  consumer_user_id?: number
  consumer_username?: string
  account_name?: string
  platform?: string
  score: number
  comment?: string
  comment_status: 'none' | 'pending' | 'approved' | 'rejected' | 'failed' | string
  comment_reject_reason?: string
  created_at: string
  updated_at: string
}

export interface SubmitAccountShareReviewRequest {
  score: number
  comment?: string
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

export interface CreateAccountShareAnthropicRequest {
  session_id: string
  code: string
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
  anthropic_5h_limit_percent?: number
  anthropic_7d_limit_percent?: number
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
  anthropic_5h_limit_percent?: number
  anthropic_7d_limit_percent?: number
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
  platform?: 'openai' | 'anthropic'
  seat_limit?: number
  seat_limits?: number[]
  search?: string
  status?: AccountShareListingStatus | 'all' | ''
  available_only?: boolean
  models?: string[]
  account_level?: AccountLevel | 'all' | ''
  owner_user_id?: number
  feature_tags?: AccountShareListingFeatureTag[]
  sort_by?: AccountShareListingSortBy
  sort_order?: AccountShareListingSortOrder
  sorts?: AccountShareListingSortKey[]
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

export interface ReorderAccountShareQueueRequest {
  api_key_id: number
  membership_ids: number[]
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

export async function generateAnthropicAuthURL(payload: {
  proxy_id: number
}): Promise<AccountShareAuthURLResponse> {
  const { data } = await apiClient.post<AccountShareAuthURLResponse>('/account-share/anthropic/auth-url', payload)
  return data
}

export async function exchangeAnthropicCode(payload: CreateAccountShareAnthropicRequest): Promise<AccountShareListing> {
  const { data } = await apiClient.post<AccountShareListing>('/account-share/anthropic/exchange-code', payload)
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

export async function recommendListings(payload: AccountShareRecommendationRequest): Promise<AccountShareRecommendationResult> {
  const { data } = await apiClient.post<AccountShareRecommendationResult>('/account-share/recommendations', payload)
  return data
}

export async function getRecommendationUsageProfile(payload: AccountShareRecommendationUsageProfileRequest): Promise<AccountShareRecommendationUsageProfile> {
  const { data } = await apiClient.get<AccountShareRecommendationUsageProfile>('/account-share/recommendations/usage-profile', {
    params: {
      platform: payload.platform,
      model: payload.model,
      days: payload.days
    }
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

export async function getMySpendSummary(
  id: number,
  payload: AccountShareMySpendParams = {},
  options: { signal?: AbortSignal } = {}
): Promise<AccountShareMySpendSummary> {
  const params: Record<string, unknown> = {}
  if (payload.range) params.range = payload.range
  if (payload.membership_id && payload.membership_id > 0) params.membership_id = payload.membership_id
  if (payload.timezone) params.timezone = payload.timezone
  const { data } = await apiClient.get<AccountShareMySpendSummary>(`/account-share/listings/${id}/my-spend`, {
    params,
    signal: options.signal
  })
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

export async function listMembershipQueue(apiKeyID: number): Promise<AccountShareMembership[]> {
  const { data } = await apiClient.get<AccountShareMembership[]>(`/account-share/queue/${apiKeyID}`)
  return data
}

export async function reorderMembershipQueue(payload: ReorderAccountShareQueueRequest): Promise<AccountShareMembership[]> {
  const { data } = await apiClient.patch<AccountShareMembership[]>('/account-share/queue', payload)
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

export async function submitReview(id: number, payload: SubmitAccountShareReviewRequest): Promise<AccountShareReview> {
  const { data } = await apiClient.post<AccountShareReview>(`/account-share/memberships/${id}/review`, payload)
  return data
}

export async function listListingReviews(
  listingID: number,
  page = 1,
  pageSize = 20
): Promise<PaginatedResponse<AccountShareReview>> {
  const { data } = await apiClient.get<PaginatedResponse<AccountShareReview>>(`/account-share/listings/${listingID}/reviews`, {
    params: {
      page,
      page_size: pageSize
    }
  })
  return data
}

export async function listOwnerReviews(
  ownerUserID: number,
  page = 1,
  pageSize = 20
): Promise<PaginatedResponse<AccountShareReview>> {
  const { data } = await apiClient.get<PaginatedResponse<AccountShareReview>>(`/account-share/owners/${ownerUserID}/reviews`, {
    params: {
      page,
      page_size: pageSize
    }
  })
  return data
}

export const accountShareAPI = {
  generateOpenAIAuthURL,
  exchangeOpenAICode,
  generateAnthropicAuthURL,
  exchangeAnthropicCode,
  listProxies,
  createProxy,
  listListings,
  recommendListings,
  getRecommendationUsageProfile,
  getListing,
  getMySpendSummary,
  updateListing,
  beginListingEdit,
  releaseListingEdit,
  joinListing,
  updateMembershipIdleTimeout,
  listMembershipQueue,
  reorderMembershipQueue,
  createEndMembershipIntent,
  endMembership,
  submitReview,
  listListingReviews,
  listOwnerReviews
}

export default accountShareAPI
