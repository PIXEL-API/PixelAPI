import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useAuthStore } from '@/stores/auth'

// Mock authAPI
const mockLogin = vi.fn()
const mockLogin2FA = vi.fn()
const mockLogout = vi.fn()
const mockGetCurrentUser = vi.fn()
const mockRegister = vi.fn()
const mockRefreshToken = vi.fn()
let documentVisibilityState: DocumentVisibilityState = 'visible'

function createDeferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

function setDocumentVisibility(state: DocumentVisibilityState): void {
  documentVisibilityState = state
  document.dispatchEvent(new Event('visibilitychange'))
}

vi.mock('@/api', () => ({
  authAPI: {
    login: (...args: any[]) => mockLogin(...args),
    login2FA: (...args: any[]) => mockLogin2FA(...args),
    logout: (...args: any[]) => mockLogout(...args),
    getCurrentUser: (...args: any[]) => mockGetCurrentUser(...args),
    register: (...args: any[]) => mockRegister(...args),
    refreshToken: (...args: any[]) => mockRefreshToken(...args),
  },
  isTotp2FARequired: (response: any) => response?.requires_2fa === true,
}))

const fakeUser = {
  id: 1,
  username: 'testuser',
  email: 'test@example.com',
  role: 'user' as const,
  balance: 100,
  concurrency: 5,
  status: 'active' as const,
  allowed_groups: null,
  created_at: '2024-01-01',
  updated_at: '2024-01-01',
}

const fakeAdminUser = {
  ...fakeUser,
  id: 2,
  username: 'admin',
  email: 'admin@example.com',
  role: 'admin' as const,
}

const fakeAuthResponse = {
  access_token: 'test-token-123',
  refresh_token: 'refresh-token-456',
  expires_in: 3600,
  token_type: 'Bearer',
  user: { ...fakeUser },
}

describe('useAuthStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
    vi.useFakeTimers()
    vi.clearAllMocks()
    documentVisibilityState = 'visible'
    vi.spyOn(document, 'visibilityState', 'get').mockImplementation(() => documentVisibilityState)
  })

  afterEach(() => {
    useAuthStore().$dispose()
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  // --- login ---

  describe('login', () => {
    it('成功登录后设置 token 和 user', async () => {
      mockLogin.mockResolvedValue(fakeAuthResponse)
      const store = useAuthStore()

      await store.login({ email: 'test@example.com', password: '123456' })

      expect(store.token).toBe('test-token-123')
      expect(store.user).toEqual(fakeUser)
      expect(store.isAuthenticated).toBe(true)
      expect(localStorage.getItem('auth_token')).toBe('test-token-123')
      expect(localStorage.getItem('auth_user')).toBe(JSON.stringify(fakeUser))
    })

    it('登录失败时清除状态并抛出错误', async () => {
      mockLogin.mockRejectedValue(new Error('Invalid credentials'))
      const store = useAuthStore()

      await expect(store.login({ email: 'test@example.com', password: 'wrong' })).rejects.toThrow(
        'Invalid credentials'
      )

      expect(store.token).toBeNull()
      expect(store.user).toBeNull()
      expect(store.isAuthenticated).toBe(false)
    })

    it('需要 2FA 时返回响应但不设置认证状态', async () => {
      const twoFAResponse = { requires_2fa: true, temp_token: 'temp-123' }
      mockLogin.mockResolvedValue(twoFAResponse)
      const store = useAuthStore()

      const result = await store.login({ email: 'test@example.com', password: '123456' })

      expect(result).toEqual(twoFAResponse)
      expect(store.token).toBeNull()
      expect(store.isAuthenticated).toBe(false)
    })

    it('缺少完整 token pair 时快速失败并清除旧认证数据', async () => {
      localStorage.setItem('refresh_token', 'old-refresh-token')
      localStorage.setItem('token_expires_at', String(Date.now() + 3600_000))
      mockLogin.mockResolvedValue({
        access_token: 'access-only-token',
        token_type: 'Bearer',
        user: { ...fakeUser },
      })
      const store = useAuthStore()

      await expect(
        store.login({ email: 'test@example.com', password: '123456' })
      ).rejects.toThrow('complete token pair')

      expect(store.isAuthenticated).toBe(false)
      expect(localStorage.getItem('auth_token')).toBeNull()
      expect(localStorage.getItem('refresh_token')).toBeNull()
      expect(localStorage.getItem('token_expires_at')).toBeNull()
    })
  })

  // --- login2FA ---

  describe('login2FA', () => {
    it('2FA 验证成功后设置认证状态', async () => {
      mockLogin2FA.mockResolvedValue(fakeAuthResponse)
      const store = useAuthStore()

      const user = await store.login2FA('temp-123', '654321')

      expect(store.token).toBe('test-token-123')
      expect(store.user).toEqual(fakeUser)
      expect(user).toEqual(fakeUser)
      expect(mockLogin2FA).toHaveBeenCalledWith({
        temp_token: 'temp-123',
        totp_code: '654321',
      })
    })

    it('2FA 验证失败时清除状态并抛出错误', async () => {
      mockLogin2FA.mockRejectedValue(new Error('Invalid TOTP'))
      const store = useAuthStore()

      await expect(store.login2FA('temp-123', '000000')).rejects.toThrow('Invalid TOTP')
      expect(store.token).toBeNull()
      expect(store.isAuthenticated).toBe(false)
    })
  })

  // --- logout ---

  describe('logout', () => {
    it('注销后清除所有状态和 localStorage', async () => {
      mockLogin.mockResolvedValue(fakeAuthResponse)
      mockLogout.mockResolvedValue(undefined)
      const store = useAuthStore()

      // 先登录
      await store.login({ email: 'test@example.com', password: '123456' })
      expect(store.isAuthenticated).toBe(true)

      // 注销
      await store.logout()

      expect(store.token).toBeNull()
      expect(store.user).toBeNull()
      expect(store.isAuthenticated).toBe(false)
      expect(localStorage.getItem('auth_token')).toBeNull()
      expect(localStorage.getItem('auth_user')).toBeNull()
      expect(localStorage.getItem('refresh_token')).toBeNull()
      expect(localStorage.getItem('token_expires_at')).toBeNull()
    })
  })

  // --- checkAuth ---

  describe('checkAuth', () => {
    it('从 localStorage 恢复持久化状态', () => {
      localStorage.setItem('auth_token', 'saved-token')
      localStorage.setItem('auth_user', JSON.stringify(fakeUser))
      localStorage.setItem('refresh_token', 'saved-refresh')
      localStorage.setItem('token_expires_at', String(Date.now() + 3600_000))

      // Mock refreshUser (getCurrentUser) 防止后台刷新报错
      mockGetCurrentUser.mockResolvedValue({ data: fakeUser })

      const store = useAuthStore()
      store.checkAuth()

      expect(store.token).toBe('saved-token')
      expect(store.user).toEqual(fakeUser)
      expect(store.isAuthenticated).toBe(true)
    })

    it('localStorage 无数据时保持未认证状态', () => {
      const store = useAuthStore()
      store.checkAuth()

      expect(store.token).toBeNull()
      expect(store.user).toBeNull()
      expect(store.isAuthenticated).toBe(false)
    })

    it('localStorage 中用户数据损坏时清除状态', () => {
      localStorage.setItem('auth_token', 'saved-token')
      localStorage.setItem('auth_user', 'invalid-json{{{')
      localStorage.setItem('refresh_token', 'saved-refresh')
      localStorage.setItem('token_expires_at', String(Date.now() + 3600_000))

      const store = useAuthStore()
      store.checkAuth()

      expect(store.token).toBeNull()
      expect(store.user).toBeNull()
      expect(localStorage.getItem('auth_token')).toBeNull()
    })

    it('恢复 refresh token 和过期时间', () => {
      const futureTs = String(Date.now() + 3600_000)
      localStorage.setItem('auth_token', 'saved-token')
      localStorage.setItem('auth_user', JSON.stringify(fakeUser))
      localStorage.setItem('refresh_token', 'saved-refresh')
      localStorage.setItem('token_expires_at', futureTs)

      mockGetCurrentUser.mockResolvedValue({ data: fakeUser })

      const store = useAuthStore()
      store.checkAuth()

      expect(store.isAuthenticated).toBe(true)
    })

    it('缺少 refresh token 的历史半会话不会恢复为已登录', () => {
      localStorage.setItem('auth_token', 'saved-token')
      localStorage.setItem('auth_user', JSON.stringify(fakeUser))
      localStorage.setItem('token_expires_at', String(Date.now() + 3600_000))
      const store = useAuthStore()

      store.checkAuth()

      expect(store.isAuthenticated).toBe(false)
      expect(localStorage.getItem('auth_token')).toBeNull()
      expect(localStorage.getItem('auth_user')).toBeNull()
      expect(localStorage.getItem('token_expires_at')).toBeNull()
    })

    it('恢复持久化 pending auth session', () => {
      localStorage.setItem(
        'pending_auth_session',
        JSON.stringify({
          token: 'pending-token',
          token_field: 'pending_auth_token',
          provider: 'wechat',
          redirect: '/profile',
        })
      )

      const store = useAuthStore()
      store.checkAuth()

      expect(store.hasPendingAuthSession).toBe(true)
      expect(store.pendingAuthSession).toEqual({
        token: 'pending-token',
        token_field: 'pending_auth_token',
        provider: 'wechat',
        redirect: '/profile',
      })
    })
  })

  describe('pending auth session', () => {
    it('persists and clears pending auth session state', () => {
      const store = useAuthStore()

      store.setPendingAuthSession({
        token: 'pending-token',
        token_field: 'pending_auth_token',
        provider: 'wechat',
        redirect: '/profile',
      })

      expect(store.hasPendingAuthSession).toBe(true)
      expect(JSON.parse(localStorage.getItem('pending_auth_session') || 'null')).toEqual({
        token: 'pending-token',
        token_field: 'pending_auth_token',
        provider: 'wechat',
        redirect: '/profile',
      })

      store.clearPendingAuthSession()

      expect(store.hasPendingAuthSession).toBe(false)
      expect(localStorage.getItem('pending_auth_session')).toBeNull()
    })

    it('restores a persisted pending oauth session without requiring a token value', () => {
      const firstStore = useAuthStore()

      firstStore.setPendingAuthSession({
        token: '',
        token_field: 'pending_oauth_token',
        provider: 'oidc',
        redirect: '/welcome',
        adoption_required: true,
        suggested_display_name: 'OIDC Nick'
      })

      setActivePinia(createPinia())
      const restoredStore = useAuthStore()
      restoredStore.checkAuth()

      expect(restoredStore.isAuthenticated).toBe(false)
      expect(restoredStore.hasPendingAuthSession).toBe(true)
      expect(restoredStore.pendingAuthSession).toEqual({
        token: '',
        token_field: 'pending_oauth_token',
        provider: 'oidc',
        redirect: '/welcome',
        adoption_required: true,
        suggested_display_name: 'OIDC Nick',
        suggested_avatar_url: undefined
      })
    })

    it('preserves pending auth session when registration fails', async () => {
      const store = useAuthStore()
      store.setPendingAuthSession({
        token: 'pending-token',
        token_field: 'pending_auth_token',
        provider: 'oidc',
        redirect: '/register',
      })
      mockRegister.mockRejectedValue(new Error('Register failed'))

      await expect(
        store.register({ email: 'user@example.com', password: 'secret-123' })
      ).rejects.toThrow('Register failed')

      expect(store.hasPendingAuthSession).toBe(true)
      expect(store.pendingAuthSession).toEqual({
        token: 'pending-token',
        token_field: 'pending_auth_token',
        provider: 'oidc',
        redirect: '/register',
      })
    })
  })

  describe('OAuth token adoption', () => {
    it('首次加载用户期间发生同会话 token 轮换仍能完成登录', async () => {
      const pendingUser = createDeferred<{ data: typeof fakeUser }>()
      mockGetCurrentUser.mockReturnValue(pendingUser.promise)
      localStorage.setItem('refresh_token', 'oauth-initial-refresh-token')
      localStorage.setItem('token_expires_at', String(Date.now() + 3_600_000))
      const store = useAuthStore()

      const adoption = store.setToken('oauth-initial-access-token')

      localStorage.setItem('auth_token', 'oauth-rotated-access-token')
      localStorage.setItem('refresh_token', 'oauth-rotated-refresh-token')
      localStorage.setItem('token_expires_at', String(Date.now() + 7_200_000))
      window.dispatchEvent(new CustomEvent('auth-token-refreshed'))

      const oauthUser = { ...fakeUser, id: 77, username: 'oauth-user' }
      pendingUser.resolve({ data: oauthUser })

      await expect(adoption).resolves.toEqual(oauthUser)
      expect(store.token).toBe('oauth-rotated-access-token')
      expect(store.user).toEqual(oauthUser)
      expect(localStorage.getItem('auth_user')).toBe(JSON.stringify(oauthUser))
    })
  })

  // --- isAdmin ---

  describe('isAdmin', () => {
    it('管理员用户返回 true', async () => {
      const adminResponse = { ...fakeAuthResponse, user: { ...fakeAdminUser } }
      mockLogin.mockResolvedValue(adminResponse)
      const store = useAuthStore()

      await store.login({ email: 'admin@example.com', password: '123456' })

      expect(store.isAdmin).toBe(true)
    })

    it('普通用户返回 false', async () => {
      mockLogin.mockResolvedValue(fakeAuthResponse)
      const store = useAuthStore()

      await store.login({ email: 'test@example.com', password: '123456' })

      expect(store.isAdmin).toBe(false)
    })

    it('未登录时返回 false', () => {
      const store = useAuthStore()
      expect(store.isAdmin).toBe(false)
    })
  })

  // --- refreshUser ---

  describe('refreshUser', () => {
    it('刷新用户数据并更新 localStorage', async () => {
      mockLogin.mockResolvedValue(fakeAuthResponse)
      const store = useAuthStore()
      await store.login({ email: 'test@example.com', password: '123456' })

      const updatedUser = { ...fakeUser, username: 'updated-name' }
      mockGetCurrentUser.mockResolvedValue({ data: updatedUser })

      const result = await store.refreshUser()

      expect(result).toEqual(updatedUser)
      expect(store.user).toEqual(updatedUser)
      expect(JSON.parse(localStorage.getItem('auth_user')!)).toEqual(updatedUser)
    })

    it('未认证时抛出错误', async () => {
      const store = useAuthStore()
      await expect(store.refreshUser()).rejects.toThrow('Not authenticated')
    })

    it('合并同一认证会话中的并发刷新', async () => {
      mockLogin.mockResolvedValue(fakeAuthResponse)
      const store = useAuthStore()
      await store.login({ email: 'test@example.com', password: '123456' })

      const pendingRefresh = createDeferred<{ data: typeof fakeUser }>()
      mockGetCurrentUser.mockReturnValue(pendingRefresh.promise)

      const firstRefresh = store.refreshUser()
      const secondRefresh = store.refreshUser()

      expect(mockGetCurrentUser).toHaveBeenCalledTimes(1)

      const updatedUser = { ...fakeUser, username: 'deduplicated' }
      pendingRefresh.resolve({ data: updatedUser })

      await expect(firstRefresh).resolves.toEqual(updatedUser)
      await expect(secondRefresh).resolves.toEqual(updatedUser)
      expect(store.user).toEqual(updatedUser)
    })

    it('注销后忽略旧刷新响应', async () => {
      mockLogin.mockResolvedValue(fakeAuthResponse)
      mockLogout.mockResolvedValue(undefined)
      const store = useAuthStore()
      await store.login({ email: 'test@example.com', password: '123456' })

      const pendingRefresh = createDeferred<{ data: typeof fakeUser }>()
      mockGetCurrentUser.mockReturnValue(pendingRefresh.promise)
      const refresh = store.refreshUser()

      await store.logout()
      pendingRefresh.resolve({ data: { ...fakeUser, username: 'stale-user' } })
      await refresh

      expect(store.user).toBeNull()
      expect(localStorage.getItem('auth_user')).toBeNull()
    })

    it('跨标签会话替换后忽略旧用户刷新响应', async () => {
      mockLogin.mockResolvedValue(fakeAuthResponse)
      const store = useAuthStore()
      await store.login({ email: 'test@example.com', password: '123456' })

      const pendingRefresh = createDeferred<{ data: typeof fakeUser }>()
      mockGetCurrentUser.mockReturnValue(pendingRefresh.promise)
      const refresh = store.refreshUser()

      const nextUser = { ...fakeUser, id: 99, username: 'other-tab-user' }
      localStorage.setItem('auth_token', 'other-tab-token')
      localStorage.setItem('refresh_token', 'other-tab-refresh-token')
      localStorage.setItem('token_expires_at', String(Date.now() + 3_600_000))
      localStorage.setItem('auth_user', JSON.stringify(nextUser))
      window.dispatchEvent(new StorageEvent('storage', {
        key: 'auth_token',
        oldValue: fakeAuthResponse.access_token,
        newValue: 'other-tab-token'
      }))

      pendingRefresh.resolve({ data: { ...fakeUser, username: 'stale-user' } })
      await refresh

      expect(store.token).toBe('other-tab-token')
      expect(store.user).toEqual(nextUser)
      expect(localStorage.getItem('auth_user')).toBe(JSON.stringify(nextUser))
    })

    it('跨标签存储已换号但 storage 事件延迟时也不会让旧响应污染新会话', async () => {
      mockLogin.mockResolvedValue(fakeAuthResponse)
      const store = useAuthStore()
      await store.login({ email: 'test@example.com', password: '123456' })

      const pendingRefresh = createDeferred<{ data: typeof fakeUser }>()
      mockGetCurrentUser.mockReturnValue(pendingRefresh.promise)
      const refresh = store.refreshUser()

      const nextUser = { ...fakeUser, id: 99, username: 'other-tab-user' }
      localStorage.setItem('auth_token', 'other-tab-token')
      localStorage.setItem('refresh_token', 'other-tab-refresh-token')
      localStorage.setItem('token_expires_at', String(Date.now() + 3_600_000))
      localStorage.setItem('auth_user', JSON.stringify(nextUser))

      pendingRefresh.resolve({ data: { ...fakeUser, username: 'stale-user' } })
      await refresh

      expect(store.user).toEqual(fakeUser)
      expect(localStorage.getItem('auth_user')).toBe(JSON.stringify(nextUser))

      window.dispatchEvent(new StorageEvent('storage', {
        key: 'auth_token',
        oldValue: fakeAuthResponse.access_token,
        newValue: 'other-tab-token'
      }))

      expect(store.token).toBe('other-tab-token')
      expect(store.user).toEqual(nextUser)
    })

    it('OAuth 跨标签换号等待用户资料时不会把新 token 与旧用户组合', async () => {
      mockLogin.mockResolvedValue(fakeAuthResponse)
      const store = useAuthStore()
      await store.login({ email: 'test@example.com', password: '123456' })

      localStorage.setItem('refresh_token', 'oauth-next-refresh-token')
      localStorage.setItem('token_expires_at', String(Date.now() + 3_600_000))
      localStorage.removeItem('auth_user')
      localStorage.setItem('auth_token', 'oauth-next-access-token')
      window.dispatchEvent(new StorageEvent('storage', {
        key: 'auth_token',
        oldValue: fakeAuthResponse.access_token,
        newValue: 'oauth-next-access-token'
      }))

      expect(store.token).toBe('oauth-next-access-token')
      expect(store.user).toBeNull()
      expect(store.isAuthenticated).toBe(false)

      const nextUser = { ...fakeUser, id: 99, username: 'oauth-next-user' }
      localStorage.setItem('auth_user', JSON.stringify(nextUser))
      window.dispatchEvent(new StorageEvent('storage', {
        key: 'auth_user',
        oldValue: null,
        newValue: JSON.stringify(nextUser)
      }))

      expect(store.user).toEqual(nextUser)
      expect(store.isAuthenticated).toBe(true)
    })
  })

  describe('auto refresh visibility', () => {
    it('保持 60 秒刷新周期', async () => {
      mockLogin.mockResolvedValue(fakeAuthResponse)
      mockGetCurrentUser.mockResolvedValue({ data: fakeUser })
      const store = useAuthStore()
      await store.login({ email: 'test@example.com', password: '123456' })

      await vi.advanceTimersByTimeAsync(60_000 - 1)
      expect(mockGetCurrentUser).not.toHaveBeenCalled()

      await vi.advanceTimersByTimeAsync(1)
      expect(mockGetCurrentUser).toHaveBeenCalledTimes(1)
    })

    it('隐藏时暂停轮询，恢复可见后仅刷新过期数据一次', async () => {
      mockLogin.mockResolvedValue(fakeAuthResponse)
      mockGetCurrentUser.mockResolvedValue({ data: fakeUser })
      const store = useAuthStore()
      await store.login({ email: 'test@example.com', password: '123456' })

      setDocumentVisibility('hidden')
      await vi.advanceTimersByTimeAsync(60_000 - 1)
      setDocumentVisibility('visible')
      expect(mockGetCurrentUser).not.toHaveBeenCalled()

      setDocumentVisibility('hidden')
      await vi.advanceTimersByTimeAsync(1)
      setDocumentVisibility('visible')
      document.dispatchEvent(new Event('visibilitychange'))
      await vi.advanceTimersByTimeAsync(0)

      expect(mockGetCurrentUser).toHaveBeenCalledTimes(1)
    })
  })

  describe('token refresh session isolation', () => {
    it('注销并重新登录后忽略旧 token 刷新响应', async () => {
      const expiringResponse = { ...fakeAuthResponse, expires_in: 120 }
      mockLogin.mockResolvedValueOnce(expiringResponse)
      mockLogout.mockResolvedValue(undefined)
      const staleRefresh = createDeferred<{
        access_token: string
        refresh_token: string
        expires_in: number
      }>()
      mockRefreshToken.mockReturnValue(staleRefresh.promise)
      const store = useAuthStore()

      await store.login({ email: 'test@example.com', password: '123456' })
      await vi.advanceTimersByTimeAsync(0)
      expect(mockRefreshToken).toHaveBeenCalledTimes(1)
      expect(mockRefreshToken).toHaveBeenCalledWith('refresh-token-456')

      await store.logout()
      const nextAuthResponse = {
        ...fakeAuthResponse,
        access_token: 'next-token',
        refresh_token: 'next-refresh-token',
        user: { ...fakeUser, id: 99, username: 'next-user' }
      }
      mockLogin.mockResolvedValueOnce(nextAuthResponse)
      await store.login({ email: 'next@example.com', password: '123456' })

      staleRefresh.resolve({
        access_token: 'stale-token',
        refresh_token: 'stale-refresh-token',
        expires_in: 3600
      })
      await Promise.resolve()
      await Promise.resolve()

      expect(store.token).toBe('next-token')
      expect(store.user?.id).toBe(99)
      expect(localStorage.getItem('auth_token')).toBe('next-token')
      expect(localStorage.getItem('refresh_token')).toBe('next-refresh-token')
    })

    it('主动刷新复用已持久化的新 refresh token', async () => {
      const expiringResponse = { ...fakeAuthResponse, expires_in: 121 }
      mockLogin.mockResolvedValue(expiringResponse)
      mockRefreshToken.mockResolvedValue({
        access_token: 'renewed-token',
        refresh_token: 'renewed-refresh-token',
        expires_in: 3600,
      })
      const store = useAuthStore()

      await store.login({ email: 'test@example.com', password: '123456' })
      expect(mockRefreshToken).not.toHaveBeenCalled()
      localStorage.setItem('auth_token', 'interceptor-renewed-token')
      localStorage.setItem('refresh_token', 'interceptor-renewed-refresh-token')
      localStorage.setItem('token_expires_at', String(Date.now() + 120_000))
      window.dispatchEvent(new CustomEvent('auth-token-refreshed'))
      await vi.advanceTimersByTimeAsync(0)

      expect(mockRefreshToken).toHaveBeenCalledWith('interceptor-renewed-refresh-token')
      expect(store.token).toBe('interceptor-renewed-token')
    })
  })

  // --- isSimpleMode ---

  describe('isSimpleMode', () => {
    it('run_mode 为 simple 时返回 true', async () => {
      const simpleResponse = {
        ...fakeAuthResponse,
        user: { ...fakeUser, run_mode: 'simple' as const },
      }
      mockLogin.mockResolvedValue(simpleResponse)
      const store = useAuthStore()

      await store.login({ email: 'test@example.com', password: '123456' })

      expect(store.isSimpleMode).toBe(true)
    })

    it('默认为 standard 模式', () => {
      const store = useAuthStore()
      expect(store.isSimpleMode).toBe(false)
    })
  })
})
