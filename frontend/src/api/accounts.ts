/**
 * User-owned account management endpoints.
 * These routes are scoped to the current authenticated user.
 */

import { apiClient } from './client'
import type {
  GrokAuthUrlRequest,
  GrokAuthUrlResponse,
  GrokExchangeCodeRequest,
  GrokTokenInfo,
} from '@/api/admin/grok'
import type {
  Account,
  AccountExternalPlacementTarget,
  AccountStatsRange,
  AccountUsageInfo,
  AccountUsageStatsResponse,
  AdminDataPayload,
  CreateAccountRequest,
  OpenAIQuotaResetResult,
  OpenAIQuotaUsage,
  PaginatedResponse,
  UpdateAccountRequest,
  UserAccountQuotaPoolDashboard,
  WindowStats
} from '@/types'

const USER_ACCOUNT_BULK_OPERATION_TIMEOUT_MS = 120000
const GOOGLE_OAUTH_EXCHANGE_TIMEOUT_MS = 120000

export type UserContentModerationMode = 'observe' | 'pre_block'
export type UserContentModerationProvider = 'openai' | 'zhipu'

export interface UserContentModerationConfig {
  id?: number
  owner_user_id: number
  account_id: number
  enabled: boolean
  mode: UserContentModerationMode
  provider: UserContentModerationProvider
  base_url: string
  model: string
  api_key_configured: boolean
  api_key_masked?: string
  sample_rate: number
  block_message: string
  created_at?: string
  updated_at?: string
}

export interface UpdateUserContentModerationConfigRequest {
  enabled?: boolean
  mode?: UserContentModerationMode
  provider?: UserContentModerationProvider
  api_key?: string
  clear_api_key?: boolean
  sample_rate?: number
  block_message?: string
}

export interface UserContentModerationLog {
  id: number
  request_id: string
  account_id: number
  endpoint: string
  provider: string
  model: string
  mode: UserContentModerationMode
  action: string
  flagged: boolean
  highest_category: string
  sampled: boolean
  error?: string
  created_at: string
}

export interface UserContentModerationTestResult {
  flagged: boolean
  highest_category: string
}

export interface UserAccountListFilters {
  platform?: string
  type?: string
  status?: string
  group_id?: number | string
  search?: string
  sort_by?: string
  sort_order?: 'asc' | 'desc'
}

export async function list(
  page: number = 1,
  pageSize: number = 20,
  filters?: UserAccountListFilters,
  options?: {
    signal?: AbortSignal
  }
): Promise<PaginatedResponse<Account>> {
  const { data } = await apiClient.get<PaginatedResponse<Account>>('/accounts', {
    params: {
      page,
      page_size: pageSize,
      ...filters
    },
    signal: options?.signal
  })
  return data
}

export async function getById(id: number): Promise<Account> {
  const { data } = await apiClient.get<Account>(`/accounts/${id}`)
  return data
}

export async function getQuotaDashboard(options?: {
  signal?: AbortSignal
}): Promise<UserAccountQuotaPoolDashboard> {
  const { data } = await apiClient.get<UserAccountQuotaPoolDashboard>('/accounts/quota-dashboard', {
    signal: options?.signal
  })
  return data
}

export async function create(accountData: CreateAccountRequest): Promise<Account> {
  const { data } = await apiClient.post<Account>('/accounts', accountData)
  return data
}

export async function importAccount(accountData: CreateAccountRequest): Promise<Account> {
  const { data } = await apiClient.post<Account>('/accounts/import', accountData)
  return data
}

export interface ImportCredentialContentsRequest {
  contents: string[]
  platform?: Account['platform']
  openai_auth_mode?: 'agent_identity'
  account_level?: Account['account_level']
  proxy_id?: number
  share_mode?: 'private' | 'public'
  concurrency?: number
  load_factor?: number | null
  priority?: number
  group_ids?: number[]
  expires_at?: number | null
  auto_pause_on_expired?: boolean
}

export interface ImportCredentialError {
  index: number
  kind?: string
  name?: string
  message: string
}

export interface ImportCredentialContentsResponse {
  total: number
  created: number
  updated?: number
  failed: number
  errors: ImportCredentialError[]
}

export async function importCredentialContents(
  request: ImportCredentialContentsRequest
): Promise<ImportCredentialContentsResponse> {
  const { data } = await apiClient.post<ImportCredentialContentsResponse>(
    '/accounts/import-credentials',
    request
  )
  return data
}

export async function exportData(options?: {
  ids?: number[]
  filters?: UserAccountListFilters
}): Promise<AdminDataPayload> {
  const params: Record<string, string> = {}
  if (options?.ids && options.ids.length > 0) {
    params.ids = options.ids.join(',')
  } else if (options?.filters) {
    const { platform, type, status, group_id, search, sort_by, sort_order } = options.filters
    if (platform) params.platform = platform
    if (type) params.type = type
    if (status) params.status = status
    if (group_id !== undefined && group_id !== '') params.group_id = String(group_id)
    if (search) params.search = search
    if (sort_by) params.sort_by = sort_by
    if (sort_order) params.sort_order = sort_order
  }
  const { data } = await apiClient.get<AdminDataPayload>('/accounts/data', { params })
  return data
}

export async function update(id: number, updates: UpdateAccountRequest): Promise<Account> {
  const { data } = await apiClient.put<Account>(`/accounts/${id}`, updates)
  return data
}

export async function revalidatePublicShare(id: number): Promise<Account> {
  const { data } = await apiClient.post<Account>(
    `/accounts/${id}/revalidate-public-share`,
    undefined,
    { timeout: 75000 }
  )
  return data
}

export async function deleteAccount(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(`/accounts/${id}`)
  return data
}

export interface UserBulkAccountResult {
  account_id: number
  success: boolean
  error?: string
}

export interface UserBulkAccountOperationResponse {
  async?: boolean
  task?: AccountBatchTask
  success: number
  failed: number
  success_ids?: number[]
  failed_ids?: number[]
  results: UserBulkAccountResult[]
}

export interface ConvertBatchExternalPlacementRequest {
  account_ids: number[]
  target: AccountExternalPlacementTarget
  room_id?: number
  idempotency_key: string
}

export type AccountBatchTaskStatus = 'pending' | 'running' | 'succeeded' | 'failed' | 'canceled'

export interface AccountBatchTaskItem {
  id: number
  task_id: number
  account_id: number
  status: AccountBatchTaskStatus
  error_message?: string
  result?: Record<string, unknown>
}

export interface AccountBatchTask {
  id: number
  scope: 'admin' | 'user'
  operation: string
  status: AccountBatchTaskStatus
  total: number
  processed: number
  success: number
  failed: number
  created_by: number
  owner_user_id?: number
  error_message?: string
  items?: AccountBatchTaskItem[]
}

export async function toggleStatus(id: number, status: 'active' | 'disabled'): Promise<Account> {
  return update(id, { status })
}

export async function bulkUpdate(
  accountIds: number[],
  updates: Partial<UpdateAccountRequest>
): Promise<UserBulkAccountOperationResponse> {
  const { data } = await apiClient.post<UserBulkAccountOperationResponse>('/accounts/bulk-update', {
    account_ids: accountIds,
    ...updates
  }, {
    timeout: USER_ACCOUNT_BULK_OPERATION_TIMEOUT_MS
  })
  return data
}

export async function bulkDelete(accountIds: number[]): Promise<UserBulkAccountOperationResponse> {
  const { data } = await apiClient.post<UserBulkAccountOperationResponse>('/accounts/bulk-delete', {
    account_ids: accountIds
  }, {
    timeout: USER_ACCOUNT_BULK_OPERATION_TIMEOUT_MS
  })
  return data
}

export async function convertExternalPlacementBatch(
  request: ConvertBatchExternalPlacementRequest
): Promise<UserBulkAccountOperationResponse> {
  const { data } = await apiClient.post<UserBulkAccountOperationResponse>(
    '/accounts/external-placement:convert-batch',
    request,
    { timeout: USER_ACCOUNT_BULK_OPERATION_TIMEOUT_MS }
  )
  return data
}

export async function createBatchRefreshTask(accountIds: number[]): Promise<AccountBatchTask> {
  const { data } = await apiClient.post<AccountBatchTask>('/accounts/batch-refresh/async', {
    account_ids: accountIds
  })
  return data
}

export async function createBatchTestConnectionTask(accountIds: number[]): Promise<AccountBatchTask> {
  const { data } = await apiClient.post<AccountBatchTask>('/accounts/batch-test/async', {
    account_ids: accountIds
  })
  return data
}

export async function createBatchRevalidatePublicShareTask(accountIds: number[]): Promise<AccountBatchTask> {
  const { data } = await apiClient.post<AccountBatchTask>('/accounts/batch-revalidate-public-share/async', {
    account_ids: accountIds
  })
  return data
}

export async function getBatchTask(taskId: number): Promise<AccountBatchTask> {
  const { data } = await apiClient.get<AccountBatchTask>(`/accounts/batch-tasks/${taskId}`)
  return data
}

export async function getUsage(
  id: number,
  source: 'local' | 'passive' | 'active',
  options: { signal?: AbortSignal } = {}
): Promise<AccountUsageInfo> {
  const { data } = await apiClient.get<AccountUsageInfo>(`/accounts/${id}/usage`, {
    params: source ? { source } : undefined,
    signal: options.signal
  })
  return data
}

export async function queryOpenAIQuota(id: number): Promise<OpenAIQuotaUsage> {
  const { data } = await apiClient.get<OpenAIQuotaUsage>(`/accounts/${id}/openai-quota`)
  return data
}

export async function resetOpenAIQuota(id: number): Promise<OpenAIQuotaResetResult> {
  const { data } = await apiClient.post<OpenAIQuotaResetResult>(`/accounts/${id}/openai-quota/reset`)
  return data
}

export async function getStats(
  id: number,
  range: AccountStatsRange
): Promise<AccountUsageStatsResponse> {
  const { data } = await apiClient.get<AccountUsageStatsResponse>(`/accounts/${id}/stats`, {
    params: {
      start_date: range.startDate,
      end_date: range.endDate
    }
  })
  return data
}

export async function getTodayStats(id: number): Promise<WindowStats> {
  const { data } = await apiClient.get<WindowStats>(`/accounts/${id}/today-stats`)
  return data
}

export async function testAccount(
  id: number,
  modelId?: string,
  prompt?: string,
  mode?: string
): Promise<{
  status: 'success' | 'error'
  message: string
  response?: string
  latency?: number
}> {
  const { data } = await apiClient.post<{
    status: 'success' | 'error'
    message: string
    response?: string
    latency?: number
  }>(`/accounts/${id}/test`, {
    model_id: modelId,
    prompt,
    mode
  })
  return data
}

export interface RefreshCredentialsResponse {
  account: Account
  warning?: string
  message?: string
}

function normalizeRefreshCredentialsResponse(data: Account | RefreshCredentialsResponse): RefreshCredentialsResponse {
  if (data && typeof data === 'object' && 'account' in data) {
    return data as RefreshCredentialsResponse
  }
  return { account: data as Account }
}

export async function refreshCredentials(id: number): Promise<RefreshCredentialsResponse> {
  const { data } = await apiClient.post<Account | RefreshCredentialsResponse>(`/accounts/${id}/refresh`)
  return normalizeRefreshCredentialsResponse(data)
}

export async function refreshCredentialsAccount(id: number): Promise<Account> {
  const data = await refreshCredentials(id)
  return data.account
}

export async function recoverState(id: number): Promise<Account> {
  const { data } = await apiClient.post<Account>(`/accounts/${id}/recover-state`)
  return data
}

export async function setPrivacy(id: number): Promise<Account> {
  const { data } = await apiClient.post<Account>(`/accounts/${id}/set-privacy`)
  return data
}

export async function getModerationConfig(id: number): Promise<UserContentModerationConfig> {
  const { data } = await apiClient.get<UserContentModerationConfig>(`/accounts/${id}/moderation/config`)
  return data
}

export async function updateModerationConfig(
  id: number,
  payload: UpdateUserContentModerationConfigRequest
): Promise<UserContentModerationConfig> {
  const { data } = await apiClient.put<UserContentModerationConfig>(
    `/accounts/${id}/moderation/config`,
    payload
  )
  return data
}

export async function testModeration(
  id: number,
  prompt: string
): Promise<UserContentModerationTestResult> {
  const { data } = await apiClient.post<UserContentModerationTestResult>(
    `/accounts/${id}/moderation/test`,
    { prompt }
  )
  return data
}

export async function listModerationLogs(
  id: number,
  page = 1,
  pageSize = 20,
  result?: string
): Promise<PaginatedResponse<UserContentModerationLog>> {
  const { data } = await apiClient.get<PaginatedResponse<UserContentModerationLog>>(
    `/accounts/${id}/moderation/logs`,
    {
      params: {
        page,
        page_size: pageSize,
        ...(result ? { result } : {})
      }
    }
  )
  return data
}

export interface UserBatchTodayStatsResponse {
  stats: Record<string, WindowStats>
}

export async function getBatchTodayStats(accountIds: number[]): Promise<UserBatchTodayStatsResponse> {
  const { data } = await apiClient.post<UserBatchTodayStatsResponse>('/accounts/today-stats/batch', {
    account_ids: accountIds
  })
  return data
}

export interface UserOAuthAuthUrlResponse {
  auth_url: string
  session_id: string
  state?: string
}

export interface UserOAuthProxyPayload {
  proxy_id?: number
  account_level?: Account['account_level']
}

export interface UserOAuthExchangeCodePayload {
  session_id: string
  code: string
  state?: string
  proxy_id?: number
  account_level?: Account['account_level']
  redirect_uri?: string
  oauth_type?: 'code_assist' | 'google_one' | 'ai_studio'
  tier_id?: string
}

export interface UserGeminiAuthUrlPayload extends UserOAuthProxyPayload {
  project_id?: string
  oauth_type?: 'code_assist' | 'google_one' | 'ai_studio'
  tier_id?: string
}

export interface UserGeminiOAuthCapabilities {
  ai_studio_oauth_enabled: boolean
  required_redirect_uris: string[]
}

function compactPayload<T extends object>(payload?: T): Partial<T> {
  if (!payload) return {}
  return Object.fromEntries(
    Object.entries(payload as Record<string, unknown>).filter(([, value]) => value !== undefined && value !== null && value !== '')
  ) as Partial<T>
}

export async function generateAnthropicOAuthUrl(
  payload?: UserOAuthProxyPayload
): Promise<UserOAuthAuthUrlResponse> {
  const { data } = await apiClient.post<UserOAuthAuthUrlResponse>(
    '/account-oauth/anthropic/auth-url',
    compactPayload(payload)
  )
  return data
}

export async function exchangeAnthropicOAuthCode(
  payload: UserOAuthExchangeCodePayload
): Promise<Record<string, unknown>> {
  const { data } = await apiClient.post<Record<string, unknown>>(
    '/account-oauth/anthropic/exchange-code',
    compactPayload(payload)
  )
  return data
}

export async function generateAnthropicSetupTokenUrl(
  payload?: UserOAuthProxyPayload
): Promise<UserOAuthAuthUrlResponse> {
  const { data } = await apiClient.post<UserOAuthAuthUrlResponse>(
    '/account-oauth/anthropic/setup-token/auth-url',
    compactPayload(payload)
  )
  return data
}

export async function exchangeAnthropicSetupTokenCode(
  payload: UserOAuthExchangeCodePayload
): Promise<Record<string, unknown>> {
  const { data } = await apiClient.post<Record<string, unknown>>(
    '/account-oauth/anthropic/setup-token/exchange-code',
    compactPayload(payload)
  )
  return data
}

export async function anthropicCookieAuth(
  payload: { code: string; proxy_id?: number }
): Promise<Record<string, unknown>> {
  const { data } = await apiClient.post<Record<string, unknown>>(
    '/account-oauth/anthropic/cookie-auth',
    compactPayload(payload)
  )
  return data
}

export async function anthropicSetupTokenCookieAuth(
  payload: { code: string; proxy_id?: number }
): Promise<Record<string, unknown>> {
  const { data } = await apiClient.post<Record<string, unknown>>(
    '/account-oauth/anthropic/setup-token-cookie-auth',
    compactPayload(payload)
  )
  return data
}

export async function generateOpenAIOAuthUrl(
  payload?: UserOAuthProxyPayload & { redirect_uri?: string }
): Promise<UserOAuthAuthUrlResponse> {
  const { data } = await apiClient.post<UserOAuthAuthUrlResponse>(
    '/account-oauth/openai/auth-url',
    compactPayload(payload)
  )
  return data
}

export async function exchangeOpenAIOAuthCode(
  payload: UserOAuthExchangeCodePayload
): Promise<Record<string, unknown>> {
  const { data } = await apiClient.post<Record<string, unknown>>(
    '/account-oauth/openai/exchange-code',
    compactPayload(payload)
  )
  return data
}

export async function refreshOpenAIToken(
  refreshToken: string,
  proxyId?: number | null,
  clientId?: string
): Promise<Record<string, unknown>> {
  const payload: { refresh_token: string; proxy_id?: number; client_id?: string } = {
    refresh_token: refreshToken
  }
  if (proxyId) payload.proxy_id = proxyId
  if (clientId) payload.client_id = clientId
  const { data } = await apiClient.post<Record<string, unknown>>(
    '/account-oauth/openai/refresh-token',
    compactPayload(payload)
  )
  return data
}

export async function getGeminiOAuthCapabilities(): Promise<UserGeminiOAuthCapabilities> {
  const { data } = await apiClient.get<UserGeminiOAuthCapabilities>(
    '/account-oauth/gemini/capabilities'
  )
  return data
}

export async function generateGeminiOAuthUrl(
  payload?: UserGeminiAuthUrlPayload
): Promise<UserOAuthAuthUrlResponse> {
  const { data } = await apiClient.post<UserOAuthAuthUrlResponse>(
    '/account-oauth/gemini/auth-url',
    compactPayload(payload)
  )
  return data
}

export async function exchangeGeminiOAuthCode(
  payload: UserOAuthExchangeCodePayload
): Promise<Record<string, unknown>> {
  const { data } = await apiClient.post<Record<string, unknown>>(
    '/account-oauth/gemini/exchange-code',
    compactPayload(payload),
    { timeout: GOOGLE_OAUTH_EXCHANGE_TIMEOUT_MS }
  )
  return data
}

export async function generateAntigravityOAuthUrl(
  payload?: UserOAuthProxyPayload
): Promise<UserOAuthAuthUrlResponse> {
  const { data } = await apiClient.post<UserOAuthAuthUrlResponse>(
    '/account-oauth/antigravity/auth-url',
    compactPayload(payload)
  )
  return data
}

export async function exchangeAntigravityOAuthCode(
  payload: UserOAuthExchangeCodePayload
): Promise<Record<string, unknown>> {
  const { data } = await apiClient.post<Record<string, unknown>>(
    '/account-oauth/antigravity/exchange-code',
    compactPayload(payload),
    { timeout: GOOGLE_OAUTH_EXCHANGE_TIMEOUT_MS }
  )
  return data
}

export async function generateGrokOAuthUrl(
  payload?: GrokAuthUrlRequest
): Promise<GrokAuthUrlResponse> {
  const { data } = await apiClient.post<GrokAuthUrlResponse>(
    '/account-oauth/grok/auth-url',
    compactPayload(payload)
  )
  return data
}

export async function exchangeGrokOAuthCode(
  payload: GrokExchangeCodeRequest
): Promise<GrokTokenInfo> {
  const { data } = await apiClient.post<GrokTokenInfo>(
    '/account-oauth/grok/exchange-code',
    compactPayload(payload)
  )
  return data
}

export async function refreshGrokToken(
  refreshToken: string,
  proxyId?: number | null
): Promise<GrokTokenInfo> {
  const payload: { refresh_token: string; proxy_id?: number } = { refresh_token: refreshToken }
  if (proxyId) payload.proxy_id = proxyId
  const { data } = await apiClient.post<GrokTokenInfo>(
    '/account-oauth/grok/refresh-token',
    compactPayload(payload)
  )
  return data
}

export async function refreshAntigravityToken(
  refreshToken: string,
  proxyId?: number | null
): Promise<Record<string, unknown>> {
  const payload: { refresh_token: string; proxy_id?: number } = { refresh_token: refreshToken }
  if (proxyId) payload.proxy_id = proxyId
  const { data } = await apiClient.post<Record<string, unknown>>(
    '/account-oauth/antigravity/refresh-token',
    compactPayload(payload),
    { timeout: GOOGLE_OAUTH_EXCHANGE_TIMEOUT_MS }
  )
  return data
}

export const accountsAPI = {
  list,
  getById,
  getQuotaDashboard,
  create,
  importAccount,
  importCredentialContents,
  exportData,
  update,
  revalidatePublicShare,
  delete: deleteAccount,
  toggleStatus,
  bulkUpdate,
  bulkDelete,
  convertExternalPlacementBatch,
  createBatchRefreshTask,
  createBatchTestConnectionTask,
  createBatchRevalidatePublicShareTask,
  getBatchTask,
  getUsage,
  queryOpenAIQuota,
  resetOpenAIQuota,
  getStats,
  getTodayStats,
  getBatchTodayStats,
  testAccount,
  refreshCredentials,
  recoverState,
  setPrivacy,
  getModerationConfig,
  updateModerationConfig,
  testModeration,
  listModerationLogs,
  generateAnthropicOAuthUrl,
  exchangeAnthropicOAuthCode,
  generateAnthropicSetupTokenUrl,
  exchangeAnthropicSetupTokenCode,
  anthropicCookieAuth,
  anthropicSetupTokenCookieAuth,
  generateOpenAIOAuthUrl,
  exchangeOpenAIOAuthCode,
  refreshOpenAIToken,
  getGeminiOAuthCapabilities,
  generateGeminiOAuthUrl,
  exchangeGeminiOAuthCode,
  generateAntigravityOAuthUrl,
  exchangeAntigravityOAuthCode,
  generateGrokOAuthUrl,
  exchangeGrokOAuthCode,
  refreshGrokToken,
  refreshAntigravityToken
}

export default accountsAPI
