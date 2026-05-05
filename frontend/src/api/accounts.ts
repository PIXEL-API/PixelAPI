/**
 * User-owned account management endpoints.
 * These routes are scoped to the current authenticated user.
 */

import { apiClient } from './client'
import type { Account, AccountUsageInfo, CreateAccountRequest, PaginatedResponse, UpdateAccountRequest, UserAccountQuotaPoolDashboard, WindowStats } from '@/types'

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

export async function update(id: number, updates: UpdateAccountRequest): Promise<Account> {
  const { data } = await apiClient.put<Account>(`/accounts/${id}`, updates)
  return data
}

export async function deleteAccount(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(`/accounts/${id}`)
  return data
}

export async function toggleStatus(id: number, status: 'active' | 'disabled'): Promise<Account> {
  return update(id, { status })
}

export async function bulkUpdate(
  accountIds: number[],
  updates: Partial<UpdateAccountRequest>
): Promise<{
  success: number
  failed: number
  success_ids?: number[]
  failed_ids?: number[]
  results: Array<{ account_id: number; success: boolean; error?: string }>
}> {
  const { data } = await apiClient.post<{
    success: number
    failed: number
    success_ids?: number[]
    failed_ids?: number[]
    results: Array<{ account_id: number; success: boolean; error?: string }>
  }>('/accounts/bulk-update', {
    account_ids: accountIds,
    ...updates
  })
  return data
}

export async function getUsage(id: number, source?: 'passive' | 'active'): Promise<AccountUsageInfo> {
  const { data } = await apiClient.get<AccountUsageInfo>(`/accounts/${id}/usage`, {
    params: source ? { source } : undefined
  })
  return data
}

export async function getTodayStats(id: number): Promise<WindowStats> {
  const { data } = await apiClient.get<WindowStats>(`/accounts/${id}/today-stats`)
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
}

export interface UserOAuthExchangeCodePayload {
  session_id: string
  code: string
  state?: string
  proxy_id?: number
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
    compactPayload(payload)
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
    compactPayload(payload)
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
  update,
  delete: deleteAccount,
  toggleStatus,
  bulkUpdate,
  getUsage,
  getTodayStats,
  getBatchTodayStats,
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
  refreshAntigravityToken
}

export default accountsAPI
