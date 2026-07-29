import { apiClient } from './client'
import type {
  AccountExternalPlacement,
  AccountExternalPlacementTarget,
  AccountLevel,
  AccountStatus,
  PaginatedResponse,
  Proxy,
  UsageProgress
} from '@/types'

export type AccountShareRoomLifecycleStatus =
  | 'active'
  | 'paused'
  | 'validating'
  | 'draining'
  | 'suspended'

export type AccountShareListingStatus = AccountShareRoomLifecycleStatus | 'disabled'
export type AccountShareListingFilterStatus = 'active' | 'paused' | 'suspended' | 'all' | ''
export type AccountShareListingTab = 'using' | 'history' | 'all' | 'mine' | 'archive'
export type AccountShareListingSortBy = 'account_concurrency' | 'per_user_concurrency' | 'min_balance_required' | 'hourly_rate' | 'hourly_fee_waiver' | 'rate_multiplier' | 'remaining_seats' | 'rating' | 'updated_at'
export type AccountShareListingSortOrder = 'asc' | 'desc'
export type AccountShareListingSortKey = `${AccountShareListingSortBy}:${AccountShareListingSortOrder}`
export type AccountShareListingFeatureTag = 'hourly_fee_waiver' | 'image_generation' | 'no_hourly_fee' | 'codex_cli_only' | 'non_codex_cli_only' | 'available'
export type AccountShareMySpendRange = 'current_membership' | 'today' | '7d'

export interface AccountShareModeGroup {
  group_id: number
  platform: 'openai' | 'anthropic' | string
}

export interface AccountShareQuotaValue {
  limit: number
  used: number
  remaining: number
}

export interface AccountShareCapabilityBlocker {
  code: string
  message: string
  limit: number
  used: number
}

export interface AccountShareCapabilities {
  lifecycle_enabled?: boolean
  can_create_room: boolean
  live_rooms: AccountShareQuotaValue
  room_creates_24_hours: AccountShareQuotaValue
  owner_room_accounts: AccountShareQuotaValue
  max_accounts_per_room: number
  seat_limit_minimum: number
  seat_limit_maximum: number
  capability_blockers: AccountShareCapabilityBlocker[]
}

export type AccountShareRoomHealthState = 'healthy' | 'degraded' | 'unavailable'
export type AccountShareRoomLifecycleAction = 'drain' | 'activate' | 'suspend' | 'delete'
export type AccountShareRoomOperationAction = 'drain_room' | 'delete_room' | 'end_membership'
export type AccountShareRoomOperationStatus =
  | 'pending'
  | 'running'
  | 'needs_attention'
  | 'succeeded'
  | 'failed'
  | 'cancelled'

export interface AccountShareRoomBlockers {
  active_membership_count: number
  queued_membership_count: number
  ending_membership_count: number
  in_flight_request_count: number
  pending_billing_intent_count: number
  synchronous_billing_pending_count: number
  valid_edit_session: boolean
  conflicting_operation: boolean
  conflicting_operation_id?: string
  runtime_dependency_unavailable: boolean
}

export interface AccountShareRoomManagementState {
  listing_id: number
  room_name: string
  row_version: number
  lifecycle_status: AccountShareRoomLifecycleStatus
  health_state: AccountShareRoomHealthState
  status_reason_code?: string
  status_reason?: string
  seat_limit: number
  active_seats: number
  ending_seats: number
  admission_remaining_seats: number
  queued_membership_count: number
  room_account_count: number
  configured_total_concurrency: number
  eligible_total_concurrency: number
  in_flight_concurrency: number
  pending_billing_intent_count: number
  allowed_actions: AccountShareRoomLifecycleAction[]
  blockers: AccountShareRoomBlockers
  pending_operation_id?: string
  deleted_at?: string
}

export interface AccountShareRoomLifecycleCommandRequest {
  expected_version: number
  reason?: string
  confirmed: boolean
}

export interface AccountShareRoomDeleteIntentRequest {
  expected_version: number
  reason?: string
}

export interface AccountShareRoomDeleteIntent {
  listing_id: number
  room_name: string
  row_version: number
  can_delete: boolean
  account_count: number
  blockers: AccountShareRoomBlockers
  token?: string
  expires_at?: string
  history_notice: string
}

export interface AccountShareRoomDeleteRequest {
  expected_version: number
  room_name: string
  token: string
  reason?: string
  confirmed: true
}

export interface AccountShareRoomOperation {
  id: string
  listing_id: number
  membership_id?: number
  action: AccountShareRoomOperationAction
  status: AccountShareRoomOperationStatus
  expected_version?: number
  start_version?: number
  final_version?: number
  blocker: Record<string, unknown>
  result: Record<string, unknown>
  error_code?: string
  error_message?: string
  created_at: string
  started_at?: string
  completed_at?: string
  updated_at: string
}

export interface AccountShareRequestOptions {
  signal?: AbortSignal
}

export interface BoundedPaginationOptions extends AccountShareRequestOptions {
  concurrency?: number
  isCurrent?: () => boolean
}

function throwCanceledPaginatedLoad(): never {
  const error = new Error('Paginated request canceled')
  error.name = 'AbortError'
  throw error
}

export async function loadAllPaginatedItems<T>(
  loadPage: (page: number) => Promise<PaginatedResponse<T>>,
  options: BoundedPaginationOptions = {}
): Promise<T[]> {
  const concurrency = Math.max(1, Math.min(5, Math.trunc(options.concurrency ?? 3)))
  const assertCurrent = (): void => {
    if (options.signal?.aborted || (options.isCurrent && !options.isCurrent())) {
      throwCanceledPaginatedLoad()
    }
  }

  assertCurrent()
  const firstPage = await loadPage(1)
  assertCurrent()
  const totalPages = Math.max(1, Math.trunc(Number(firstPage.pages) || 1))
  const itemsByPage = new Map<number, T[]>([[1, firstPage.items || []]])
  let nextPage = 2
  let failed = false

  const loadWorker = async (): Promise<void> => {
    while (!failed) {
      assertCurrent()
      const page = nextPage
      nextPage += 1
      if (page > totalPages) return
      try {
        const result = await loadPage(page)
        assertCurrent()
        itemsByPage.set(page, result.items || [])
      } catch (error) {
        failed = true
        throw error
      }
    }
  }

  const workerCount = Math.min(concurrency, Math.max(0, totalPages - 1))
  await Promise.all(Array.from({ length: workerCount }, () => loadWorker()))
  assertCurrent()

  const items: T[] = []
  for (let page = 1; page <= totalPages; page += 1) {
    items.push(...(itemsByPage.get(page) || []))
  }
  return items
}

export interface AccountShareWaiverProgress {
  enabled: boolean
  status: 'in_progress' | 'met' | string
  window_start: string
  window_end: string
  now: string
  elapsed_seconds: number
  remaining_seconds: number
  required_amount: number
  usage_amount: number
  remaining_amount: number
  progress_percent: number
  hourly_rate: number
  waiver_minimum: number
  estimated_hourly_fee_refund: number
  request_count: number
  last_request_at?: string
}

export interface AccountShareListing {
  id: number
  row_version?: number
  current_revision_id?: number
  deleted?: boolean
  history_snapshot_quality?: 'exact' | 'backfilled_current' | 'unknown'
  account_id?: number
  room_name?: string
  account_count?: number
  healthy_account_count?: number
  accounts?: AccountShareRoomAccount[]
  account_sample_scope?: 'representative'
  quota_summary?: AccountShareRoomQuotaSummary
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
  runtime_load_known?: boolean
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
  current_api_key_name?: string
  current_joined_at?: string
  current_paid_until?: string
  current_billed_until?: string
  current_idle_timeout_minutes?: number
  current_last_request_at?: string
  current_idle_expires_at?: string
  current_waiver_progress?: AccountShareWaiverProgress | null
  queue_membership_id?: number
  queue_api_key_id?: number
  queue_api_key_name?: string
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

export interface AccountShareRoomQuotaWindow {
  known_count: number
  min_utilization: number | null
  max_utilization: number | null
  average_utilization: number | null
  max_utilization_resets_at?: string | null
  partial: boolean
}

export interface AccountShareRoomQuotaSummary {
  scope: 'room'
  attached_count: number
  eligible_count: number
  window_5h?: AccountShareRoomQuotaWindow
  window_7d?: AccountShareRoomQuotaWindow
}

export interface AccountShareListingTermsSnapshot {
  listing_revision_id: number
  row_version: number
  schema_version: number
  room_name: string
  status: string
  seat_limit: number
  rate_multiplier: number
  allowed_models: string[]
  per_user_concurrency: number
  hourly_rate: number
  hourly_fee_waiver_minimum: number
  min_balance_required: number
  codex_cli_only: boolean
  codex_5h_limit_percent: number
  codex_7d_limit_percent: number
  anthropic_5h_limit_percent?: number
  anthropic_7d_limit_percent?: number
}

export interface AccountShareMembershipHistoryReview {
  id: number
  score: number
  comment?: string
  comment_status: 'none' | 'pending' | 'approved' | 'rejected' | 'failed' | string
  comment_reject_reason?: string
  created_at?: string
}

export interface AccountShareMembershipHistoryEntry {
  membership_id: number
  listing_id: number
  listing_revision_id?: number
  listing_version_snapshot?: number
  room_name: string
  room_deleted: boolean
  room_deleted_at?: string
  owner_user_id: number
  owner_username?: string
  platform: string
  account_level?: string
  account_id?: number
  account_name?: string
  configured_concurrency_snapshot?: number
  api_key_id: number
  api_key_name?: string
  status: string
  joined_at: string
  last_request_at?: string
  ended_at?: string
  ended_reason?: string
  paid_until?: string
  billed_until?: string
  hourly_rate_snapshot: number
  hourly_fee_waiver_minimum_snapshot: number
  idle_timeout_minutes: number
  usage_request_count: number
  usage_request_cost: number
  terms_snapshot?: AccountShareListingTermsSnapshot
  snapshot_quality: 'exact' | 'backfilled_current' | 'unknown' | string
  review?: AccountShareMembershipHistoryReview
}

export interface AccountShareRoomAccount {
  account_id: number
  account_name: string
  platform: string
  account_level: AccountLevel
  status: AccountStatus | string
  schedulable: boolean
  current_concurrency: number
  priority: number
  placement_state?: string
  last_used_at?: string
}

export interface AccountShareRoomAccountsBatchRequest {
  account_ids: number[]
  idempotency_key: string
}

export interface AccountShareRoomAccountsBatchResult {
  account_id: number
  success: boolean
  error?: string
}

export interface AccountShareRoomAccountsBatchResponse {
  success: number
  failed: number
  success_ids?: number[]
  failed_ids?: number[]
  results: AccountShareRoomAccountsBatchResult[]
}

export interface ConvertAccountExternalPlacementRequest {
  target: AccountExternalPlacementTarget
  idempotency_key: string
}

export interface ConvertAccountExternalPlacementResponse {
  account_id: number
  previous: AccountExternalPlacement | null
  current: AccountExternalPlacement | null
  unchanged: boolean
}

export interface CreateAccountShareRoomRequest {
  account_id: number
  room_name: string
  idempotency_key: string
  seat_limit: number
  rate_multiplier: number
  allowed_models: string[]
  per_user_concurrency: number
  hourly_rate: number
  hourly_fee_waiver_minimum: number
  min_balance_required: number
  codex_cli_only: boolean
  codex_5h_limit_percent: number
  codex_7d_limit_percent: number
  anthropic_5h_limit_percent: number
  anthropic_7d_limit_percent: number
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
  /** Informational total cache reads; history cannot split text and image cache. */
  cache_read_tokens_per_request: number
  image_input_tokens_per_request: number
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

export interface AccountShareRecommendationScoreBreakdown {
  cost_saving_score: number
  stability_score: number
  availability_score: number
  risk_control_score: number
  overall_score: number
}

export interface AccountShareRecommendationCandidate {
  rank: number
  listing: AccountShareListing
  estimate: AccountShareRecommendationEstimate
  score: number
  score_breakdown: AccountShareRecommendationScoreBreakdown
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
  status: 'active' | 'queued' | 'ending' | 'ended'
  queue_rank: number
  hourly_rate_snapshot?: number
  hourly_fee_waiver_minimum_snapshot?: number
  idle_timeout_minutes: number
  joined_at: string
  last_request_at?: string
  ending_requested_at?: string
  ending_reason?: string
  ending_operation_id?: string
  ending_operation_status?: string
  settlement_status?: string
  ended_at?: string
  ended_reason?: string
  paid_until?: string
  billed_until?: string
  dispatch_failed_at?: string
  dispatch_cooldown_until?: string
  created_at: string
  updated_at: string
}

export interface AccountShareAPIKeyBindingStatus {
  api_key_id: number
  active_count: number
  queued_count: number
  ending_count: number
  blocking_count: number
  memberships: AccountShareMembership[]
}

export interface AccountShareJoinIntent {
  listing_id: number
  api_key_id: number
  token: string
  expires_at: string
  expected_version: number
  expected_revision_id: number
  accept_queue: boolean
  queue_may_be_required: boolean
  terms: AccountShareListingTermsSnapshot
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
  api_key_name?: string
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
  operation_id: string
  token: string
  expires_at: string
}

export interface AccountShareReview {
  id: number
  account_identity_id?: number
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
  expected_version: number
  name?: string
  proxy_id?: number
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
  reason?: string
  confirmed?: boolean
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
  status?: AccountShareListingFilterStatus
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

export interface UpdateAccountShareProxyRequest extends Omit<CreateAccountShareProxyRequest, 'password'> {
  password?: string
}

export interface JoinAccountShareListingRequest {
  api_key_id: number
  idle_timeout_minutes: number
  intent_token: string
  expected_version: number
  expected_revision_id: number
  accept_queue: boolean
}

export interface CreateAccountShareJoinIntentRequest {
  api_key_id: number
  idle_timeout_minutes: number
  accept_queue: boolean
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

export async function exchangeOpenAICode(
  payload: CreateAccountShareOpenAIRequest,
  idempotencyKey: string
): Promise<AccountShareListing> {
  const { data } = await apiClient.post<AccountShareListing>(
    '/account-share/openai/exchange-code',
    payload,
    idempotencyRequestConfig(idempotencyKey)
  )
  return data
}

export async function generateAnthropicAuthURL(payload: {
  proxy_id: number
}): Promise<AccountShareAuthURLResponse> {
  const { data } = await apiClient.post<AccountShareAuthURLResponse>('/account-share/anthropic/auth-url', payload)
  return data
}

export async function exchangeAnthropicCode(
  payload: CreateAccountShareAnthropicRequest,
  idempotencyKey: string
): Promise<AccountShareListing> {
  const { data } = await apiClient.post<AccountShareListing>(
    '/account-share/anthropic/exchange-code',
    payload,
    idempotencyRequestConfig(idempotencyKey)
  )
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

export async function listMembershipHistory(
  page = 1,
  pageSize = 20,
  options: AccountShareRequestOptions = {}
): Promise<PaginatedResponse<AccountShareMembershipHistoryEntry>> {
  const { data } = await apiClient.get<PaginatedResponse<AccountShareMembershipHistoryEntry>>(
    '/account-share/history/memberships',
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

export async function recommendListings(
  payload: AccountShareRecommendationRequest,
  options: AccountShareRequestOptions = {}
): Promise<AccountShareRecommendationResult> {
  const { data } = await apiClient.post<AccountShareRecommendationResult>(
    '/account-share/recommendations',
    payload,
    { signal: options.signal }
  )
  return data
}

export async function getRecommendationUsageProfile(
  payload: AccountShareRecommendationUsageProfileRequest,
  options: AccountShareRequestOptions = {}
): Promise<AccountShareRecommendationUsageProfile> {
  const { data } = await apiClient.get<AccountShareRecommendationUsageProfile>('/account-share/recommendations/usage-profile', {
    params: {
      platform: payload.platform,
      model: payload.model,
      days: payload.days
    },
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

export async function updateProxy(id: number, payload: UpdateAccountShareProxyRequest): Promise<Proxy> {
  const { data } = await apiClient.put<Proxy>(`/account-share/proxies/${id}`, payload)
  return data
}

export async function deleteProxy(id: number): Promise<void> {
  await apiClient.delete(`/account-share/proxies/${id}`)
}

export async function getListing(id: number): Promise<AccountShareListing> {
  const { data } = await apiClient.get<AccountShareListing>(`/account-share/listings/${id}`)
  return data
}

export async function listModeGroups(): Promise<AccountShareModeGroup[]> {
  const { data } = await apiClient.get<AccountShareModeGroup[]>('/account-share/mode-groups')
  return data
}

export async function getCapabilities(): Promise<AccountShareCapabilities> {
  const { data } = await apiClient.get<AccountShareCapabilities>('/account-share/me/capabilities')
  return data
}

export async function getRoomManagementState(
  id: number,
  options: AccountShareRequestOptions = {}
): Promise<AccountShareRoomManagementState> {
  const { data } = await apiClient.get<AccountShareRoomManagementState>(
    `/account-share/listings/${id}/management-state`,
    { signal: options.signal }
  )
  return data
}

function idempotencyRequestConfig(idempotencyKey: string): {
  headers: { 'Idempotency-Key': string }
} {
  return {
    headers: {
      'Idempotency-Key': idempotencyKey
    }
  }
}

export async function drainRoom(
  id: number,
  payload: AccountShareRoomLifecycleCommandRequest,
  idempotencyKey: string
): Promise<AccountShareRoomManagementState> {
  const { data } = await apiClient.post<AccountShareRoomManagementState>(
    `/account-share/listings/${id}/drain`,
    payload,
    idempotencyRequestConfig(idempotencyKey)
  )
  return data
}

export async function activateRoom(
  id: number,
  payload: AccountShareRoomLifecycleCommandRequest,
  idempotencyKey: string
): Promise<AccountShareRoomManagementState> {
  const { data } = await apiClient.post<AccountShareRoomManagementState>(
    `/account-share/listings/${id}/activate`,
    payload,
    idempotencyRequestConfig(idempotencyKey)
  )
  return data
}

export async function suspendRoom(
  id: number,
  payload: AccountShareRoomLifecycleCommandRequest,
  idempotencyKey: string
): Promise<AccountShareRoomManagementState> {
  const { data } = await apiClient.post<AccountShareRoomManagementState>(
    `/account-share/listings/${id}/suspend`,
    payload,
    idempotencyRequestConfig(idempotencyKey)
  )
  return data
}

export async function createRoomDeleteIntent(
  id: number,
  payload: AccountShareRoomDeleteIntentRequest
): Promise<AccountShareRoomDeleteIntent> {
  const { data } = await apiClient.post<AccountShareRoomDeleteIntent>(
    `/account-share/listings/${id}/delete-intent`,
    payload
  )
  return data
}

export async function deleteRoom(
  id: number,
  payload: AccountShareRoomDeleteRequest,
  idempotencyKey: string
): Promise<AccountShareRoomOperation> {
  const { data } = await apiClient.delete<AccountShareRoomOperation>(
    `/account-share/listings/${id}`,
    {
      data: payload,
      ...idempotencyRequestConfig(idempotencyKey)
    }
  )
  return data
}

export async function getRoomOperation(
  operationID: string,
  options: AccountShareRequestOptions = {}
): Promise<AccountShareRoomOperation> {
  const { data } = await apiClient.get<AccountShareRoomOperation>(
    `/account-share/room-operations/${encodeURIComponent(operationID)}`,
    { signal: options.signal }
  )
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

export async function updateListing(
  id: number,
  payload: UpdateAccountShareListingRequest,
  idempotencyKey: string
): Promise<AccountShareListing> {
  const { data } = await apiClient.patch<AccountShareListing>(
    `/account-share/listings/${id}`,
    payload,
    idempotencyRequestConfig(idempotencyKey)
  )
  return data
}

export async function beginListingEdit(
  id: number,
  payload: AccountShareListingEditSessionRequest,
  idempotencyKey: string,
  options: AccountShareRequestOptions = {}
): Promise<AccountShareListing> {
  const { data } = await apiClient.post<AccountShareListing>(
    `/account-share/listings/${id}/edit-session`,
    payload,
    {
      ...idempotencyRequestConfig(idempotencyKey),
      signal: options.signal
    }
  )
  return data
}

export async function releaseListingEdit(
  id: number,
  sessionID: string,
  idempotencyKey: string
): Promise<AccountShareListing> {
  const { data } = await apiClient.post<AccountShareListing>(
    `/account-share/listings/${id}/edit-session/release`,
    { session_id: sessionID },
    idempotencyRequestConfig(idempotencyKey)
  )
  return data
}

export async function createJoinIntent(
  id: number,
  payload: CreateAccountShareJoinIntentRequest
): Promise<AccountShareJoinIntent> {
  const { data } = await apiClient.post<AccountShareJoinIntent>(
    `/account-share/listings/${id}/join-intent`,
    payload
  )
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

export async function listMembershipQueue(
  apiKeyID: number,
  options: { signal?: AbortSignal } = {}
): Promise<AccountShareMembership[]> {
  const { data } = await apiClient.get<AccountShareMembership[]>(`/account-share/queue/${apiKeyID}`, {
    signal: options.signal
  })
  return data
}

export async function getAPIKeyBindingStatus(
  apiKeyID: number,
  options: AccountShareRequestOptions = {}
): Promise<AccountShareAPIKeyBindingStatus> {
  const { data } = await apiClient.get<AccountShareAPIKeyBindingStatus>(
    `/account-share/api-key-bindings/${apiKeyID}/status`,
    { signal: options.signal }
  )
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

export async function submitReview(
  id: number,
  payload: SubmitAccountShareReviewRequest,
  idempotencyKey: string
): Promise<AccountShareReview> {
  const { data } = await apiClient.post<AccountShareReview>(
    `/account-share/memberships/${id}/review`,
    payload,
    idempotencyRequestConfig(idempotencyKey)
  )
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
  pageSize = 20,
  options: AccountShareRequestOptions = {}
): Promise<PaginatedResponse<AccountShareReview>> {
  const { data } = await apiClient.get<PaginatedResponse<AccountShareReview>>(`/account-share/owners/${ownerUserID}/reviews`, {
    params: {
      page,
      page_size: pageSize
    },
    signal: options.signal
  })
  return data
}

export async function convertAccountExternalPlacement(
  accountID: number,
  payload: ConvertAccountExternalPlacementRequest
): Promise<ConvertAccountExternalPlacementResponse> {
  const { data } = await apiClient.post<ConvertAccountExternalPlacementResponse>(
    `/accounts/${accountID}/external-placement:convert`,
    payload
  )
  return data
}

export async function createRoom(
  payload: CreateAccountShareRoomRequest
): Promise<AccountShareListing> {
  const { data } = await apiClient.post<AccountShareListing>('/account-share/rooms', payload)
  return data
}

export async function listRoomAccounts(listingID: number): Promise<AccountShareRoomAccount[]> {
  const { data } = await apiClient.get<AccountShareRoomAccount[]>(
    `/account-share/listings/${listingID}/accounts`
  )
  return data
}

export async function attachRoomAccounts(
  listingID: number,
  payload: AccountShareRoomAccountsBatchRequest
): Promise<AccountShareRoomAccountsBatchResponse> {
  const { data } = await apiClient.post<AccountShareRoomAccountsBatchResponse>(
    `/account-share/listings/${listingID}/accounts/attach-batch`,
    payload
  )
  return data
}

export async function detachRoomAccounts(
  listingID: number,
  payload: AccountShareRoomAccountsBatchRequest
): Promise<AccountShareRoomAccountsBatchResponse> {
  const { data } = await apiClient.post<AccountShareRoomAccountsBatchResponse>(
    `/account-share/listings/${listingID}/accounts/detach-batch`,
    payload
  )
  return data
}

export const accountShareAPI = {
  listModeGroups,
  getCapabilities,
  getRoomManagementState,
  drainRoom,
  activateRoom,
  suspendRoom,
  createRoomDeleteIntent,
  deleteRoom,
  getRoomOperation,
  generateOpenAIAuthURL,
  exchangeOpenAICode,
  generateAnthropicAuthURL,
  exchangeAnthropicCode,
  listProxies,
  createProxy,
  updateProxy,
  deleteProxy,
  listListings,
  listMembershipHistory,
  recommendListings,
  getRecommendationUsageProfile,
  getListing,
  getMySpendSummary,
  updateListing,
  beginListingEdit,
  releaseListingEdit,
  createJoinIntent,
  joinListing,
  updateMembershipIdleTimeout,
  listMembershipQueue,
  getAPIKeyBindingStatus,
  reorderMembershipQueue,
  createEndMembershipIntent,
  endMembership,
  submitReview,
  listListingReviews,
  listOwnerReviews,
  convertAccountExternalPlacement,
  createRoom,
  listRoomAccounts,
  attachRoomAccounts,
  detachRoomAccounts
}

export default accountShareAPI
