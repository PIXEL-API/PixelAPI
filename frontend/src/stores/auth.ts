/**
 * Authentication Store
 * Manages user authentication state, login/logout, token refresh, and token persistence
 */

import { defineStore } from 'pinia'
import { ref, computed, readonly, onScopeDispose } from 'vue'
import { authAPI, isTotp2FARequired, type LoginResponse } from '@/api'
import { assertCompleteAuthResponse } from '@/api/auth'
import type { User, LoginRequest, RegisterRequest, AuthResponse } from '@/types'

const AUTH_TOKEN_KEY = 'auth_token'
const AUTH_USER_KEY = 'auth_user'
const REFRESH_TOKEN_KEY = 'refresh_token'
const TOKEN_EXPIRES_AT_KEY = 'token_expires_at' // 存储过期时间戳而非有效期
const PENDING_AUTH_SESSION_KEY = 'pending_auth_session'
const AUTO_REFRESH_INTERVAL = 60 * 1000 // 60 seconds for user data refresh
const TOKEN_REFRESH_BUFFER = 120 * 1000 // 120 seconds before expiry to refresh token

type PendingAuthTokenField = 'pending_auth_token' | 'pending_oauth_token'

interface PendingAuthSessionSummary {
  token: string
  token_field: PendingAuthTokenField
  provider: string
  redirect?: string
  adoption_required?: boolean
  suggested_display_name?: string
  suggested_avatar_url?: string
}

interface AuthSessionSnapshot {
  accessToken: string
  refreshToken: string | null
  localTokenRefreshGeneration: number
}

function normalizePendingAuthTokenField(value: unknown): PendingAuthTokenField {
  return value === 'pending_oauth_token' ? 'pending_oauth_token' : 'pending_auth_token'
}

function getPersistedPendingAuthSession(): PendingAuthSessionSummary | null {
  const raw = localStorage.getItem(PENDING_AUTH_SESSION_KEY)
  if (!raw) {
    return null
  }

  try {
    const parsed = JSON.parse(raw) as Partial<PendingAuthSessionSummary> | null
    const provider = typeof parsed?.provider === 'string' ? parsed.provider.trim() : ''
    if (!provider) {
      localStorage.removeItem(PENDING_AUTH_SESSION_KEY)
      return null
    }
    return {
      token: typeof parsed?.token === 'string' ? parsed.token : '',
      token_field: normalizePendingAuthTokenField(parsed?.token_field),
      provider,
      redirect: typeof parsed?.redirect === 'string' ? parsed.redirect : undefined,
      adoption_required: typeof parsed?.adoption_required === 'boolean' ? parsed.adoption_required : undefined,
      suggested_display_name: typeof parsed?.suggested_display_name === 'string' ? parsed.suggested_display_name : undefined,
      suggested_avatar_url: typeof parsed?.suggested_avatar_url === 'string' ? parsed.suggested_avatar_url : undefined
    }
  } catch {
    localStorage.removeItem(PENDING_AUTH_SESSION_KEY)
    return null
  }
}

function persistPendingAuthSession(session: PendingAuthSessionSummary): void {
  localStorage.setItem(PENDING_AUTH_SESSION_KEY, JSON.stringify(session))
}

function clearPendingAuthSessionStorage(): void {
  localStorage.removeItem(PENDING_AUTH_SESSION_KEY)
}

export const useAuthStore = defineStore('auth', () => {
  // ==================== State ====================

  const user = ref<User | null>(null)
  const token = ref<string | null>(null)
  const refreshTokenValue = ref<string | null>(null)
  const tokenExpiresAt = ref<number | null>(null) // 过期时间戳（毫秒）
  const runMode = ref<'standard' | 'simple'>('standard')
  const pendingAuthSession = ref<PendingAuthSessionSummary | null>(null)
  let refreshIntervalId: ReturnType<typeof setTimeout> | null = null
  let tokenRefreshTimeoutId: ReturnType<typeof setTimeout> | null = null
  let refreshUserPromise: Promise<User> | null = null
  let refreshUserGeneration = 0
  let tokenRefreshGeneration = 0
  let localTokenRefreshGeneration = 0
  let lastUserRefreshAt: number | null = null
  let autoRefreshEnabled = false
  let visibilityListenerRegistered = false
  let authEventListenersRegistered = false

  // ==================== Computed ====================

  const isAuthenticated = computed(() => {
    return !!token.value && !!user.value
  })

  const isAdmin = computed(() => {
    return user.value?.role === 'admin'
  })

  const isSimpleMode = computed(() => runMode.value === 'simple')
  const hasPendingAuthSession = computed(() => pendingAuthSession.value !== null)

  // ==================== Actions ====================

  /**
   * Initialize auth state from localStorage
   * Call this on app startup to restore session
   * Also starts auto-refresh and immediately fetches latest user data
   */
  function checkAuth(): void {
    const savedToken = localStorage.getItem(AUTH_TOKEN_KEY)
    const savedUser = localStorage.getItem(AUTH_USER_KEY)
    const savedRefreshToken = localStorage.getItem(REFRESH_TOKEN_KEY)
    const savedExpiresAt = localStorage.getItem(TOKEN_EXPIRES_AT_KEY)
    const parsedExpiresAt = savedExpiresAt ? Number.parseInt(savedExpiresAt, 10) : Number.NaN
    pendingAuthSession.value = getPersistedPendingAuthSession()

    if (
      !savedToken ||
      !savedUser ||
      !savedRefreshToken ||
      !Number.isFinite(parsedExpiresAt) ||
      parsedExpiresAt <= 0
    ) {
      if (savedToken || savedUser || savedRefreshToken || savedExpiresAt) {
        clearAuth({ preservePendingAuthSession: true })
      }
      return
    }

    try {
      restoreRefreshSession(savedToken)
      token.value = savedToken
      user.value = JSON.parse(savedUser)
      refreshTokenValue.value = savedRefreshToken
      tokenExpiresAt.value = parsedExpiresAt

      // Start proactive token refresh first so an already-expired access token does not
      // launch an independent 401-driven refresh before the shared refresh starts.
      scheduleTokenRefreshAt(tokenExpiresAt.value)

      // Immediately refresh user data from backend (async, don't block)
      refreshUser().catch((error) => {
        console.error('Failed to refresh user on init:', error)
      })

      // Start auto-refresh interval for user data
      startAutoRefresh()
    } catch (error) {
      console.error('Failed to parse saved user data:', error)
      clearAuth({ preservePendingAuthSession: true })
    }
  }

  /**
   * Start auto-refresh interval for user data
   * Refreshes user data every 60 seconds
   */
  function isVisible(): boolean {
    return typeof document === 'undefined' || document.visibilityState === 'visible'
  }

  function freshnessRemaining(): number {
    if (lastUserRefreshAt === null) return 0
    return Math.min(
      AUTO_REFRESH_INTERVAL,
      Math.max(0, AUTO_REFRESH_INTERVAL - (Date.now() - lastUserRefreshAt))
    )
  }

  function clearAutoRefreshTimer(): void {
    if (!refreshIntervalId) return
    clearTimeout(refreshIntervalId)
    refreshIntervalId = null
  }

  function scheduleAutoRefresh(delay = freshnessRemaining()): void {
    clearAutoRefreshTimer()
    if (!autoRefreshEnabled || !token.value || !isVisible()) return

    if (delay <= 0) {
      void runAutoRefresh()
      return
    }

    refreshIntervalId = setTimeout(() => {
      refreshIntervalId = null
      void runAutoRefresh()
    }, delay)
  }

  async function runAutoRefresh(): Promise<void> {
    if (!autoRefreshEnabled || !token.value || !isVisible()) return
    try {
      await refreshUser(false)
    } catch (error) {
      console.error('Auto-refresh user failed:', error)
    } finally {
      scheduleAutoRefresh(AUTO_REFRESH_INTERVAL)
    }
  }

  function handleVisibilityChange(): void {
    if (!isVisible()) {
      clearAutoRefreshTimer()
      return
    }
    scheduleAutoRefresh()
  }

  function registerVisibilityListener(): void {
    if (visibilityListenerRegistered || typeof document === 'undefined') return
    document.addEventListener('visibilitychange', handleVisibilityChange)
    visibilityListenerRegistered = true
  }

  function unregisterVisibilityListener(): void {
    if (!visibilityListenerRegistered || typeof document === 'undefined') return
    document.removeEventListener('visibilitychange', handleVisibilityChange)
    visibilityListenerRegistered = false
  }

  function startAutoRefresh(): void {
    autoRefreshEnabled = true
    registerVisibilityListener()
    scheduleAutoRefresh()
  }

  /**
   * Stop auto-refresh interval
   */
  function stopAutoRefresh(): void {
    autoRefreshEnabled = false
    clearAutoRefreshTimer()
    unregisterVisibilityListener()
  }

  /**
   * Schedule proactive token refresh before expiry (based on expiry timestamp)
   * @param expiresAtMs - Token expiry timestamp in milliseconds
   */
  function scheduleTokenRefreshAt(expiresAtMs: number): void {
    // Clear any existing timeout
    if (tokenRefreshTimeoutId) {
      clearTimeout(tokenRefreshTimeoutId)
      tokenRefreshTimeoutId = null
    }

    // Calculate remaining time until refresh (buffer time before expiry)
    const now = Date.now()
    const refreshInMs = Math.max(0, expiresAtMs - now - TOKEN_REFRESH_BUFFER)

    if (refreshInMs <= 0) {
      // Token is about to expire or already expired, refresh immediately
      performTokenRefresh()
      return
    }

    tokenRefreshTimeoutId = setTimeout(() => {
      performTokenRefresh()
    }, refreshInMs)
  }

  /**
   * Schedule proactive token refresh before expiry (based on expires_in seconds)
   * @param expiresInSeconds - Token expiry time in seconds from now
   */
  function scheduleTokenRefresh(expiresInSeconds: number): void {
    const expiresAtMs = Date.now() + expiresInSeconds * 1000
    tokenExpiresAt.value = expiresAtMs
    localStorage.setItem(TOKEN_EXPIRES_AT_KEY, String(expiresAtMs))
    scheduleTokenRefreshAt(expiresAtMs)
  }

  /**
   * Perform the actual token refresh
   */
  async function performTokenRefresh(): Promise<void> {
    const persistedRefreshToken = localStorage.getItem(REFRESH_TOKEN_KEY)
    const currentRefreshToken = persistedRefreshToken || refreshTokenValue.value
    if (!currentRefreshToken) {
      return
    }
    refreshTokenValue.value = currentRefreshToken
    const requestGeneration = tokenRefreshGeneration

    try {
      const response = await authAPI.refreshToken(currentRefreshToken)

      if (requestGeneration !== tokenRefreshGeneration) {
        return
      }

      syncTokenPairFromStorage()

      // Schedule next refresh (this also updates tokenExpiresAt and localStorage)
      scheduleTokenRefresh(response.expires_in)
    } catch (error) {
      if (requestGeneration === tokenRefreshGeneration) {
        console.error('Token refresh failed:', error)
      }
      // Don't clear auth here - the interceptor will handle 401 errors
    }
  }

  /**
   * Stop token refresh timeout
   */
  function stopTokenRefresh(): void {
    tokenRefreshGeneration++
    if (tokenRefreshTimeoutId) {
      clearTimeout(tokenRefreshTimeoutId)
      tokenRefreshTimeoutId = null
    }
  }

  /**
   * User login
   * @param credentials - Login credentials (email and password)
   * @returns Promise resolving to the login response (may require 2FA)
   * @throws Error if login fails
   */
  async function login(credentials: LoginRequest): Promise<LoginResponse> {
    try {
      const response = await authAPI.login(credentials)

      // If 2FA is required, return the response without setting auth state
      if (isTotp2FARequired(response)) {
        return response
      }

      // Set auth state from the response
      setAuthFromResponse(response)

      return response
    } catch (error) {
      // Clear any partial state on error
      clearAuth({ preservePendingAuthSession: pendingAuthSession.value !== null })
      throw error
    }
  }

  /**
   * Complete login with 2FA code
   * @param tempToken - Temporary token from initial login
   * @param totpCode - 6-digit TOTP code
   * @returns Promise resolving to the authenticated user
   * @throws Error if 2FA verification fails
   */
  async function login2FA(
    tempToken: string,
    totpCode: string,
    loginAgreementRevision?: string
  ): Promise<User> {
    try {
      const response = await authAPI.login2FA({
        temp_token: tempToken,
        totp_code: totpCode,
        login_agreement_revision: loginAgreementRevision
      })
      setAuthFromResponse(response)
      return user.value!
    } catch (error) {
      clearAuth({ preservePendingAuthSession: pendingAuthSession.value !== null })
      throw error
    }
  }

  /**
   * Set auth state from an AuthResponse
   * Internal helper function
   */
  function setAuthFromResponse(response: AuthResponse): void {
    assertCompleteAuthResponse(response)
    stopTokenRefresh()
    invalidateUserRefresh()

    token.value = response.access_token
    refreshTokenValue.value = response.refresh_token
    tokenExpiresAt.value = Date.now() + response.expires_in * 1000

    // Extract run_mode if present
    if (response.user.run_mode) {
      runMode.value = response.user.run_mode
    }
    const { run_mode: _run_mode, ...userData } = response.user
    user.value = userData
    lastUserRefreshAt = Date.now()

    // Persist to localStorage
    localStorage.setItem(AUTH_TOKEN_KEY, response.access_token)
    localStorage.setItem(REFRESH_TOKEN_KEY, response.refresh_token)
    localStorage.setItem(TOKEN_EXPIRES_AT_KEY, String(tokenExpiresAt.value))
    localStorage.setItem(AUTH_USER_KEY, JSON.stringify(userData))
    clearPendingAuthSession()

    // Start auto-refresh interval for user data
    startAutoRefresh()

    // Start proactive token refresh if we have refresh token and expiry info
    // scheduleTokenRefresh will also store the expiry timestamp
    scheduleTokenRefresh(response.expires_in)
  }

  function syncTokenPairFromStorage(): void {
    const persistedAccessToken = localStorage.getItem(AUTH_TOKEN_KEY)
    const persistedRefreshToken = localStorage.getItem(REFRESH_TOKEN_KEY)
    const persistedExpiresAt = localStorage.getItem(TOKEN_EXPIRES_AT_KEY)
    const nextExpiresAt = persistedExpiresAt ? Number.parseInt(persistedExpiresAt, 10) : null

    if (!persistedAccessToken || !persistedRefreshToken || !nextExpiresAt || !Number.isFinite(nextExpiresAt)) {
      return
    }
    token.value = persistedAccessToken
    refreshTokenValue.value = persistedRefreshToken
    tokenExpiresAt.value = nextExpiresAt
  }

  /**
   * User registration
   * @param userData - Registration data (username, email, password)
   * @returns Promise resolving to the newly registered and authenticated user
   * @throws Error if registration fails
   */
  async function register(userData: RegisterRequest): Promise<User> {
    try {
      const response = await authAPI.register(userData)

      // Use the common helper to set auth state
      setAuthFromResponse(response)

      return user.value!
    } catch (error) {
      // Clear any partial state on error
      clearAuth({ preservePendingAuthSession: pendingAuthSession.value !== null })
      throw error
    }
  }

  /**
   * 直接设置 token（用于 OAuth/SSO 回调），并加载当前用户信息。
   * 会自动读取 localStorage 中已设置的 refresh_token 和 token_expires_in
   * @param newToken - 后端签发的 JWT access token
   */
  async function setToken(newToken: string): Promise<User> {
    // Clear any previous state first (avoid mixing sessions)
    // Note: Don't clear localStorage here as OAuth callback may have set refresh_token
    stopAutoRefresh()
    stopTokenRefresh()
    invalidateUserRefresh()
    token.value = null
    user.value = null
    refreshTokenValue.value = null
    tokenExpiresAt.value = null

    const savedRefreshToken = localStorage.getItem(REFRESH_TOKEN_KEY)?.trim() || ''
    const savedExpiresAt = localStorage.getItem(TOKEN_EXPIRES_AT_KEY)?.trim() || ''
    const parsedExpiresAt = savedExpiresAt ? Number.parseInt(savedExpiresAt, 10) : Number.NaN
    if (!newToken.trim() || !savedRefreshToken || !Number.isFinite(parsedExpiresAt) || parsedExpiresAt <= 0) {
      clearAuth({ preservePendingAuthSession: pendingAuthSession.value !== null })
      throw new Error('Authentication response is missing a complete token pair')
    }

    // OAuth account switching persists the token pair before /auth/me completes.
    // Remove the previous profile first so other tabs cannot combine a new token
    // with the old account while the replacement profile is still loading.
    localStorage.removeItem(AUTH_USER_KEY)
    token.value = newToken
    localStorage.setItem(AUTH_TOKEN_KEY, newToken)

    // Read refresh token and expires_at from localStorage if set by OAuth callback
    refreshTokenValue.value = savedRefreshToken
    tokenExpiresAt.value = parsedExpiresAt

    try {
      const userData = await refreshUser()
      startAutoRefresh()

      // Start proactive token refresh if we have refresh token and expiry info
      // Note: use !== null to handle case when tokenExpiresAt.value is 0 (expired)
      if (savedRefreshToken && tokenExpiresAt.value !== null) {
        scheduleTokenRefreshAt(tokenExpiresAt.value)
      }

      clearPendingAuthSession()
      return userData
    } catch (error) {
      clearAuth({ preservePendingAuthSession: pendingAuthSession.value !== null })
      throw error
    }
  }

  function setPendingAuthSession(session: PendingAuthSessionSummary | null): void {
    pendingAuthSession.value = session

    if (session) {
      persistPendingAuthSession(session)
      return
    }

    clearPendingAuthSessionStorage()
  }

  function clearPendingAuthSession(): void {
    setPendingAuthSession(null)
  }

  /**
   * User logout
   * Clears all authentication state and persisted data
   */
  async function logout(): Promise<void> {
    stopAutoRefresh()
    stopTokenRefresh()
    invalidateUserRefresh()

    // Call API logout (revokes refresh token on server)
    try {
      await authAPI.logout()
    } finally {
      // Clear state even when network revocation fails.
      clearAuth()
    }
  }

  /**
   * Refresh current user data
   * Fetches latest user info from the server
   * @returns Promise resolving to the updated user
   * @throws Error if not authenticated or request fails
   */
  function refreshUser(force = true): Promise<User> {
    if (!token.value) {
      return Promise.reject(new Error('Not authenticated'))
    }

    if (refreshUserPromise) return refreshUserPromise

    if (!force && lastUserRefreshAt !== null && freshnessRemaining() > 0 && user.value) {
      return Promise.resolve(user.value)
    }

    const requestGeneration = refreshUserGeneration
    const sessionSnapshot: AuthSessionSnapshot = {
      accessToken: token.value,
      refreshToken: localStorage.getItem(REFRESH_TOKEN_KEY),
      localTokenRefreshGeneration
    }
    const requestPromise = authAPI.getCurrentUser()
      .then((response) => {
        const { run_mode: nextRunMode, ...userData } = response.data
        if (
          requestGeneration === refreshUserGeneration &&
          isAuthSessionSnapshotCurrent(sessionSnapshot)
        ) {
          if (nextRunMode) runMode.value = nextRunMode
          user.value = userData
          lastUserRefreshAt = Date.now()
          localStorage.setItem(AUTH_USER_KEY, JSON.stringify(userData))

          if (autoRefreshEnabled && isVisible()) {
            scheduleAutoRefresh(AUTO_REFRESH_INTERVAL)
          }
        }
        return userData
      })
      .catch((error) => {
        if (
          requestGeneration === refreshUserGeneration &&
          isAuthSessionSnapshotCurrent(sessionSnapshot) &&
          (error as { status?: number }).status === 401
        ) {
          clearAuth({ preservePendingAuthSession: pendingAuthSession.value !== null })
        }
        throw error
      })
      .finally(() => {
        if (refreshUserPromise === requestPromise) {
          refreshUserPromise = null
        }
      })

    refreshUserPromise = requestPromise
    return requestPromise
  }

  function isAuthSessionSnapshotCurrent(snapshot: AuthSessionSnapshot): boolean {
    const persistedAccessToken = localStorage.getItem(AUTH_TOKEN_KEY)
    const persistedRefreshToken = localStorage.getItem(REFRESH_TOKEN_KEY)
    if (token.value !== persistedAccessToken || refreshTokenValue.value !== persistedRefreshToken) {
      return false
    }
    if (
      persistedAccessToken === snapshot.accessToken &&
      persistedRefreshToken === snapshot.refreshToken
    ) {
      return true
    }
    return localTokenRefreshGeneration > snapshot.localTokenRefreshGeneration
  }

  function invalidateUserRefresh(): void {
    refreshUserGeneration++
    refreshUserPromise = null
    lastUserRefreshAt = null
  }

  function restoreRefreshSession(savedToken: string): void {
    if (token.value !== savedToken) {
      invalidateUserRefresh()
    }
  }

  function disposeStoreResources(): void {
    stopAutoRefresh()
    stopTokenRefresh()
    invalidateUserRefresh()
    unregisterAuthEventListeners()
  }

  function handleTokenRefreshed(): void {
    const previousAccessToken = token.value
    const previousRefreshToken = refreshTokenValue.value
    syncTokenPairFromStorage()
    if (
      token.value !== previousAccessToken ||
      refreshTokenValue.value !== previousRefreshToken
    ) {
      localTokenRefreshGeneration++
    }
    if (refreshTokenValue.value && tokenExpiresAt.value !== null) {
      scheduleTokenRefreshAt(tokenExpiresAt.value)
    }
  }

  function handleAuthSessionExpired(): void {
    clearAuth({ preservePendingAuthSession: pendingAuthSession.value !== null })
  }

  function handleAuthStorageChange(event: StorageEvent): void {
    if (![AUTH_TOKEN_KEY, REFRESH_TOKEN_KEY, TOKEN_EXPIRES_AT_KEY, AUTH_USER_KEY].includes(event.key || '')) return

    const persistedToken = localStorage.getItem(AUTH_TOKEN_KEY)
    const persistedRefreshToken = localStorage.getItem(REFRESH_TOKEN_KEY)
    if (!persistedToken || !persistedRefreshToken) {
      clearAuth({ preservePendingAuthSession: pendingAuthSession.value !== null })
      return
    }

    const sessionChanged =
      persistedToken !== token.value || persistedRefreshToken !== refreshTokenValue.value
    if (sessionChanged) {
      stopTokenRefresh()
      invalidateUserRefresh()
      user.value = null
    }

    syncTokenPairFromStorage()
    const persistedUser = localStorage.getItem(AUTH_USER_KEY)
    if (persistedUser) {
      try {
        user.value = JSON.parse(persistedUser) as User
      } catch {
        clearAuth({ preservePendingAuthSession: pendingAuthSession.value !== null })
        return
      }
    } else if (sessionChanged || event.key === AUTH_USER_KEY) {
      user.value = null
    }
    if (tokenExpiresAt.value !== null) {
      scheduleTokenRefreshAt(tokenExpiresAt.value)
    }
  }

  function registerAuthEventListeners(): void {
    if (authEventListenersRegistered || typeof window === 'undefined') return
    window.addEventListener('auth-token-refreshed', handleTokenRefreshed)
    window.addEventListener('auth-session-expired', handleAuthSessionExpired)
    window.addEventListener('storage', handleAuthStorageChange)
    authEventListenersRegistered = true
  }

  function unregisterAuthEventListeners(): void {
    if (!authEventListenersRegistered || typeof window === 'undefined') return
    window.removeEventListener('auth-token-refreshed', handleTokenRefreshed)
    window.removeEventListener('auth-session-expired', handleAuthSessionExpired)
    window.removeEventListener('storage', handleAuthStorageChange)
    authEventListenersRegistered = false
  }

  registerAuthEventListeners()

  onScopeDispose(disposeStoreResources)

  /**
   * Clear all authentication state
   * Internal helper function
   */
  function clearAuth(options?: { preservePendingAuthSession?: boolean }): void {
    // Stop auto-refresh
    stopAutoRefresh()
    // Stop token refresh
    stopTokenRefresh()
    invalidateUserRefresh()

    token.value = null
    refreshTokenValue.value = null
    tokenExpiresAt.value = null
    user.value = null
    localStorage.removeItem(AUTH_TOKEN_KEY)
    localStorage.removeItem(AUTH_USER_KEY)
    localStorage.removeItem(REFRESH_TOKEN_KEY)
    localStorage.removeItem(TOKEN_EXPIRES_AT_KEY)

    if (options?.preservePendingAuthSession) {
      pendingAuthSession.value = getPersistedPendingAuthSession()
      return
    }

    pendingAuthSession.value = null
    clearPendingAuthSessionStorage()
  }

  // ==================== Return Store API ====================

  return {
    // State
    user,
    token,
    runMode: readonly(runMode),
    pendingAuthSession: readonly(pendingAuthSession),

    // Computed
    isAuthenticated,
    isAdmin,
    isSimpleMode,
    hasPendingAuthSession,

    // Actions
    login,
    login2FA,
    register,
    setToken,
    logout,
    checkAuth,
    refreshUser,
    setPendingAuthSession,
    clearPendingAuthSession
  }
})
