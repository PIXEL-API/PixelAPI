/**
 * Axios HTTP Client Configuration
 * Base client with interceptors for authentication, token refresh, and error handling
 */

import axios, { AxiosInstance, AxiosError, InternalAxiosRequestConfig, AxiosResponse } from 'axios'
import type { ApiResponse } from '@/types'
import { getLocale } from '@/i18n'
import {
  ADMIN_UI_REQUEST_HEADER,
  USER_UI_REQUEST_HEADER,
  shouldMarkAdminUIRequest,
  shouldMarkUserUIRequest,
} from './adminUIRequest'

// ==================== Axios Instance Configuration ====================

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || '/api/v1'
const AUTH_TOKEN_KEY = 'auth_token'
const REFRESH_TOKEN_KEY = 'refresh_token'
const AUTH_USER_KEY = 'auth_user'
const TOKEN_EXPIRES_AT_KEY = 'token_expires_at'
const AUTH_EXPIRED_KEY = 'auth_expired'
const TOKEN_REFRESH_TIMEOUT_MS = 30_000
const TOKEN_REFRESH_LOCK_NAME = 'sub2api-auth-token-refresh'

export const apiClient: AxiosInstance = axios.create({
  baseURL: API_BASE_URL,
  withCredentials: true,
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json'
  }
})

// ==================== Token Refresh State ====================

export interface AuthTokenPair {
  access_token: string
  refresh_token: string
  expires_in: number
}

interface AuthStorageSnapshot {
  accessToken: string | null
  refreshToken: string
  userIdentity: string | null
}

interface StructuredAuthError {
  status: number
  code?: unknown
  reason?: unknown
  error?: unknown
  message: string
  metadata?: unknown
}

export class TokenRefreshError extends Error {
  readonly status: number
  readonly code?: unknown
  readonly cause?: unknown

  constructor(message: string, status = 0, code?: unknown, cause?: unknown) {
    super(message)
    this.name = 'TokenRefreshError'
    this.status = status
    this.code = code
    this.cause = cause
  }
}

let tokenRefreshPromise: Promise<AuthTokenPair> | null = null
let tokenRefreshToken: string | null = null

function getPersistedUserIdentity(): string | null {
  const rawUser = localStorage.getItem(AUTH_USER_KEY)
  if (!rawUser) return null
  try {
    const parsed = JSON.parse(rawUser) as { id?: unknown; email?: unknown }
    if (typeof parsed.id === 'number' || typeof parsed.id === 'string') {
      return `id:${String(parsed.id)}`
    }
    if (typeof parsed.email === 'string' && parsed.email.trim()) {
      return `email:${parsed.email.trim().toLowerCase()}`
    }
  } catch {
    return null
  }
  return null
}

function getAuthStorageSnapshot(refreshTokenOverride?: string): AuthStorageSnapshot {
  const persistedRefreshToken = localStorage.getItem(REFRESH_TOKEN_KEY)?.trim() || ''
  const override = refreshTokenOverride?.trim() || ''
  if (override && override !== persistedRefreshToken) {
    throw new TokenRefreshError('Refresh token is no longer current', 409, 'STALE_REFRESH_TOKEN')
  }

  const refreshToken = override || persistedRefreshToken
  if (!refreshToken) {
    throw new TokenRefreshError('No refresh token available', 401, 'NO_REFRESH_TOKEN')
  }
  return {
    accessToken: localStorage.getItem(AUTH_TOKEN_KEY),
    refreshToken,
    userIdentity: getPersistedUserIdentity()
  }
}

function isAuthSnapshotCurrent(snapshot: AuthStorageSnapshot): boolean {
  return (
    localStorage.getItem(AUTH_TOKEN_KEY) === snapshot.accessToken &&
    localStorage.getItem(REFRESH_TOKEN_KEY) === snapshot.refreshToken
  )
}

function readExternallyRotatedTokenPair(snapshot: AuthStorageSnapshot): AuthTokenPair | null {
  const accessToken = localStorage.getItem(AUTH_TOKEN_KEY)?.trim() || ''
  const refreshToken = localStorage.getItem(REFRESH_TOKEN_KEY)?.trim() || ''
  const expiresAtRaw = localStorage.getItem(TOKEN_EXPIRES_AT_KEY)
  const expiresAt = expiresAtRaw ? Number.parseInt(expiresAtRaw, 10) : Number.NaN
  const currentIdentity = getPersistedUserIdentity()

  if (
    !snapshot.userIdentity ||
    currentIdentity !== snapshot.userIdentity ||
    !accessToken ||
    !refreshToken ||
    refreshToken === snapshot.refreshToken ||
    !Number.isFinite(expiresAt) ||
    expiresAt <= Date.now()
  ) {
    return null
  }

  return {
    access_token: accessToken,
    refresh_token: refreshToken,
    expires_in: Math.max(1, Math.ceil((expiresAt - Date.now()) / 1000))
  }
}

function parseRefreshResponse(response: AxiosResponse): AuthTokenPair {
  const body = response.data as ApiResponse<AuthTokenPair> | AuthTokenPair
  const data =
    body && typeof body === 'object' && 'code' in body
      ? body.code === 0
        ? body.data
        : null
      : body

  if (
    !data ||
    typeof data.access_token !== 'string' ||
    !data.access_token.trim() ||
    typeof data.refresh_token !== 'string' ||
    !data.refresh_token.trim() ||
    typeof data.expires_in !== 'number' ||
    !Number.isFinite(data.expires_in) ||
    data.expires_in <= 0
  ) {
    throw new TokenRefreshError('Token refresh returned an invalid token pair', 502, 'INVALID_REFRESH_RESPONSE')
  }

  return data
}

function normalizeRefreshError(error: unknown): TokenRefreshError {
  if (error instanceof TokenRefreshError) return error
  if (axios.isAxiosError<ApiResponse<unknown>>(error)) {
    const status = error.response?.status ?? 0
    const data = error.response?.data
    return new TokenRefreshError(
      data?.message || error.message || 'Token refresh failed',
      status,
      data?.code,
      error
    )
  }
  if (error && typeof error === 'object') {
    const candidate = error as {
      message?: string
      code?: unknown
      response?: { status?: number; data?: { code?: unknown; message?: string } }
    }
    return new TokenRefreshError(
      candidate.response?.data?.message || candidate.message || 'Token refresh failed',
      candidate.response?.status ?? 0,
      candidate.response?.data?.code ?? candidate.code,
      error
    )
  }
  return new TokenRefreshError(
    error instanceof Error ? error.message : 'Token refresh failed',
    0,
    undefined,
    error
  )
}

export function isTerminalTokenRefreshError(error: unknown): boolean {
  const status = error instanceof TokenRefreshError ? error.status : 0
  return status === 400 || status === 401 || status === 403
}

function clearPersistedAuth(): void {
  localStorage.removeItem(AUTH_TOKEN_KEY)
  localStorage.removeItem(REFRESH_TOKEN_KEY)
  localStorage.removeItem(AUTH_USER_KEY)
  localStorage.removeItem(TOKEN_EXPIRES_AT_KEY)
}

function expireAuthSession(): void {
  clearPersistedAuth()
  sessionStorage.setItem(AUTH_EXPIRED_KEY, '1')
  try {
    window.dispatchEvent(new CustomEvent('auth-session-expired'))
  } catch {
    // The persisted state remains the source of truth when events are unavailable.
  }
  if (!window.location.pathname.includes('/login')) {
    window.location.href = '/login'
  }
}

function requestRefreshedTokenPair(snapshot: AuthStorageSnapshot): Promise<AuthTokenPair> {
  return axios
    .post<ApiResponse<AuthTokenPair>>(
      `${API_BASE_URL}/auth/refresh`,
      { refresh_token: snapshot.refreshToken },
      {
        headers: { 'Content-Type': 'application/json' },
        timeout: TOKEN_REFRESH_TIMEOUT_MS,
        withCredentials: true
      }
    )
    .then((response) => {
      const pair = parseRefreshResponse(response)
      if (!isAuthSnapshotCurrent(snapshot)) {
        throw new TokenRefreshError('Token refresh response is stale', 409, 'STALE_REFRESH_RESPONSE')
      }

      localStorage.setItem(AUTH_TOKEN_KEY, pair.access_token)
      localStorage.setItem(REFRESH_TOKEN_KEY, pair.refresh_token)
      localStorage.setItem(TOKEN_EXPIRES_AT_KEY, String(Date.now() + pair.expires_in * 1000))
      try {
        window.dispatchEvent(new CustomEvent('auth-token-refreshed', { detail: pair }))
      } catch {
        // Store actions also re-read persisted tokens after awaiting this promise.
      }
      return pair
    })
    .catch((error: unknown) => {
      const normalized = normalizeRefreshError(error)
      if (isTerminalTokenRefreshError(normalized) && isAuthSnapshotCurrent(snapshot)) {
        expireAuthSession()
      }
      throw normalized
    })
}

async function refreshWithCrossTabLock(snapshot: AuthStorageSnapshot): Promise<AuthTokenPair> {
  const execute = async (): Promise<AuthTokenPair> => {
    if (!isAuthSnapshotCurrent(snapshot)) {
      const externallyRotated = readExternallyRotatedTokenPair(snapshot)
      if (externallyRotated) return externallyRotated
      throw new TokenRefreshError('Refresh token is no longer current', 409, 'STALE_REFRESH_TOKEN')
    }
    return requestRefreshedTokenPair(snapshot)
  }

  if (typeof navigator === 'undefined' || !navigator.locks?.request) {
    return execute()
  }
  return navigator.locks.request(TOKEN_REFRESH_LOCK_NAME, { mode: 'exclusive' }, execute)
}

/**
 * Shared refresh coordinator used by both proactive refresh and 401 retry handling.
 * The compare-and-swap checks prevent a late response from overwriting logout,
 * a newer login, or a refresh completed in another browser tab.
 */
export function refreshAuthTokenPair(refreshTokenOverride?: string): Promise<AuthTokenPair> {
  const snapshot = getAuthStorageSnapshot(refreshTokenOverride)
  if (tokenRefreshPromise && tokenRefreshToken === snapshot.refreshToken) {
    return tokenRefreshPromise
  }

  const request = refreshWithCrossTabLock(snapshot)
    .finally(() => {
      if (tokenRefreshPromise === request) {
        tokenRefreshPromise = null
        tokenRefreshToken = null
      }
    })

  tokenRefreshToken = snapshot.refreshToken
  tokenRefreshPromise = request
  return request
}

// ==================== Request Interceptor ====================

// Get user's timezone
const getUserTimezone = (): string => {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone
  } catch {
    return 'UTC'
  }
}

apiClient.interceptors.request.use(
  (config: InternalAxiosRequestConfig) => {
    // Attach token from localStorage
    const token = localStorage.getItem(AUTH_TOKEN_KEY)
    if (token && config.headers) {
      config.headers.Authorization = `Bearer ${token}`
    }

    // Attach locale for backend translations
    if (config.headers) {
      config.headers['Accept-Language'] = getLocale()
    }

    // Attach timezone for all GET requests (backend may use it for default date ranges)
	if (config.method === 'get') {
      if (!config.params) {
        config.params = {}
      }
      config.params.timezone = getUserTimezone()
	}

    if (config.headers) {
      const requestURL = String(config.url || '')
      if (shouldMarkAdminUIRequest(requestURL)) {
        config.headers[ADMIN_UI_REQUEST_HEADER] = '1'
      }
      if (shouldMarkUserUIRequest(requestURL)) {
        config.headers[USER_UI_REQUEST_HEADER] = '1'
      }
    }

	return config
  },
  (error: AxiosError) => {
    return Promise.reject(error)
  }
)

// ==================== Response Interceptor ====================

apiClient.interceptors.response.use(
  (response: AxiosResponse) => {
    // Unwrap standard API response format { code, message, data }
    const apiResponse = response.data as ApiResponse<unknown>
    if (apiResponse && typeof apiResponse === 'object' && 'code' in apiResponse) {
      if (apiResponse.code === 0) {
        // Success - return the data portion
        response.data = apiResponse.data
      } else {
        // API error
        const resp = apiResponse as unknown as Record<string, unknown>
        return Promise.reject({
          status: response.status,
          code: apiResponse.code,
          message: apiResponse.message || 'Unknown error',
          reason: resp.reason,
          metadata: resp.metadata,
        })
      }
    }
    return response
  },
  async (error: AxiosError<ApiResponse<unknown>>) => {
    // Request cancellation: keep the original axios cancellation error so callers can ignore it.
    // Otherwise we'd misclassify it as a generic "network error".
    if (error.code === 'ERR_CANCELED' || axios.isCancel(error)) {
      return Promise.reject(error)
    }

    const originalRequest = error.config as InternalAxiosRequestConfig & { _retry?: boolean }

    // Handle common errors
    if (error.response) {
      const { status, data } = error.response
      const url = String(error.config?.url || '')

      // Validate `data` shape to avoid HTML error pages breaking our error handling.
      const apiData = (typeof data === 'object' && data !== null ? data : {}) as Record<string, any>

      // Ops monitoring disabled: treat as feature-flagged 404, and proactively redirect away
      // from ops pages to avoid broken UI states.
      if (status === 404 && apiData.message === 'Ops monitoring is disabled') {
        try {
          localStorage.setItem('ops_monitoring_enabled_cached', 'false')
        } catch {
          // ignore localStorage failures
        }
        try {
          window.dispatchEvent(new CustomEvent('ops-monitoring-disabled'))
        } catch {
          // ignore event failures
        }

        if (window.location.pathname.startsWith('/admin/ops')) {
          window.location.href = '/admin/settings'
        }

        return Promise.reject({
          status,
          code: 'OPS_DISABLED',
          message: apiData.message || error.message,
          url
        })
      }

      // 401: Try to refresh the token if we have a refresh token
      // This handles TOKEN_EXPIRED, INVALID_TOKEN, TOKEN_REVOKED, etc.
      if (status === 401 && !originalRequest._retry) {
        const refreshToken = localStorage.getItem(REFRESH_TOKEN_KEY)
        const isCredentialAuthEndpoint =
          url.includes('/auth/login') || url.includes('/auth/register') || url.includes('/auth/refresh')
        const isSessionManagementEndpoint = url.includes('/auth/logout')
        const skipsRefresh = isCredentialAuthEndpoint || isSessionManagementEndpoint

        // If we have a refresh token and this is not an auth endpoint, try to refresh
        if (refreshToken && !skipsRefresh) {
          originalRequest._retry = true
          try {
            const refreshed = await refreshAuthTokenPair(refreshToken)
            if (originalRequest.headers) {
              originalRequest.headers.Authorization = `Bearer ${refreshed.access_token}`
            }
            return apiClient(originalRequest)
          } catch (refreshError: unknown) {
            if (isTerminalTokenRefreshError(refreshError)) {
              if (localStorage.getItem(REFRESH_TOKEN_KEY) === refreshToken) {
                expireAuthSession()
              }
              return Promise.reject<never>({
                status: 401,
                code: 'TOKEN_REFRESH_FAILED',
                message: 'Session expired. Please log in again.'
              } satisfies StructuredAuthError)
            }

            const normalized = normalizeRefreshError(refreshError)
            return Promise.reject<never>({
              status: normalized.status,
              code: normalized.code || 'TOKEN_REFRESH_TEMPORARY_FAILURE',
              message: normalized.message
            } satisfies StructuredAuthError)
          }
        }

        if (isSessionManagementEndpoint) {
          return Promise.reject({
            status,
            code: apiData.code,
            reason: apiData.reason,
            error: apiData.error,
            message: apiData.message || apiData.detail || error.message,
            metadata: apiData.metadata,
          })
        }

        // No refresh token or credential auth endpoint - clear auth and redirect
        const hasToken = !!localStorage.getItem(AUTH_TOKEN_KEY)
        const headers = error.config?.headers as Record<string, unknown> | undefined
        const authHeader = headers?.Authorization ?? headers?.authorization
        const sentAuth =
          typeof authHeader === 'string'
            ? authHeader.trim() !== ''
            : Array.isArray(authHeader)
              ? authHeader.length > 0
              : !!authHeader

        clearPersistedAuth()
        if ((hasToken || sentAuth) && !isCredentialAuthEndpoint) {
          sessionStorage.setItem(AUTH_EXPIRED_KEY, '1')
        }
        // Only redirect if not already on login page
        if (!window.location.pathname.includes('/login')) {
          window.location.href = '/login'
        }
      }

      // Return structured error
      return Promise.reject({
        status,
        code: apiData.code,
        reason: apiData.reason,
        error: apiData.error,
        message: apiData.message || apiData.detail || error.message,
        metadata: apiData.metadata,
      })
    }

    if (error.code === 'ECONNABORTED' || error.code === 'ETIMEDOUT') {
      return Promise.reject({
        status: 0,
        code: error.code,
        message: 'Request timed out. Please try again later.'
      })
    }

    // Network error
    return Promise.reject({
      status: 0,
      message: 'Network error. Please check your connection.'
    })
  }
)

export default apiClient
