import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import axios from 'axios'
import type { AxiosInstance } from 'axios'

// 需要在导入 client 之前设置 mock
vi.mock('@/i18n', () => ({
  getLocale: () => 'zh-CN',
}))

describe('API Client', () => {
  let apiClient: AxiosInstance

  beforeEach(async () => {
    localStorage.clear()
    sessionStorage.clear()
    // 每次测试重新导入以获取干净的模块状态
    vi.resetModules()
    const mod = await import('@/api/client')
    apiClient = mod.apiClient
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  // --- 请求拦截器 ---

  describe('请求拦截器', () => {
    it('自动附加 Authorization 头', async () => {
      localStorage.setItem('auth_token', 'my-jwt-token')

      // 拦截实际请求
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/test')

      const config = adapter.mock.calls[0][0]
      expect(config.headers.get('Authorization')).toBe('Bearer my-jwt-token')
    })

    it('无 token 时不附加 Authorization 头', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/test')

      const config = adapter.mock.calls[0][0]
      expect(config.headers.get('Authorization')).toBeFalsy()
    })

    it('GET 请求自动附加 timezone 参数', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/test')

      const config = adapter.mock.calls[0][0]
      expect(config.params).toHaveProperty('timezone')
    })

    it('POST 请求不附加 timezone 参数', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.post('/test', { foo: 'bar' })

      const config = adapter.mock.calls[0][0]
      expect(config.params?.timezone).toBeUndefined()
    })

    it('请求默认带 withCredentials 以支持跨域 cookie', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.post('/auth/oauth/bind-token')

      const config = adapter.mock.calls[0][0]
      expect(config.withCredentials).toBe(true)
    })

    it('仅为管理端和用户端 allowlist API 添加 Server-Timing scope 头', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: {} },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await apiClient.get('/admin/users')
      await apiClient.get('/accounts')
      await apiClient.post('/payment/public/orders/verify')

      expect(adapter.mock.calls[0][0].headers.get('X-Admin-UI-Request')).toBe('1')
      expect(adapter.mock.calls[0][0].headers.get('X-User-UI-Request')).toBeFalsy()
      expect(adapter.mock.calls[1][0].headers.get('X-User-UI-Request')).toBe('1')
      expect(adapter.mock.calls[2][0].headers.get('X-Admin-UI-Request')).toBeFalsy()
      expect(adapter.mock.calls[2][0].headers.get('X-User-UI-Request')).toBeFalsy()
    })
  })

  // --- 响应拦截器 ---

  describe('响应拦截器', () => {
    it('code=0 时解包 data 字段', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 0, data: { name: 'test' }, message: 'ok' },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      const response = await apiClient.get('/test')
      expect(response.data).toEqual({ name: 'test' })
    })

    it('code!=0 时拒绝并返回结构化错误', async () => {
      const adapter = vi.fn().mockResolvedValue({
        status: 200,
        data: { code: 1001, message: '参数错误', data: null },
        headers: {},
        config: {},
        statusText: 'OK',
      })
      apiClient.defaults.adapter = adapter

      await expect(apiClient.get('/test')).rejects.toEqual(
        expect.objectContaining({
          code: 1001,
          message: '参数错误',
        })
      )
    })
  })

  // --- 401 Token 刷新 ---

  describe('401 Token 刷新', () => {
    it('无 refresh_token 时 401 清除 localStorage', async () => {
      localStorage.setItem('auth_token', 'expired-token')
      // 不设置 refresh_token

      // Mock window.location
      const originalLocation = window.location
      Object.defineProperty(window, 'location', {
        value: { ...originalLocation, pathname: '/dashboard', href: '/dashboard' },
        writable: true,
      })

      const adapter = vi.fn().mockRejectedValue({
        response: {
          status: 401,
          data: { code: 'TOKEN_EXPIRED', message: 'Token expired' },
        },
        config: {
          url: '/test',
          headers: { Authorization: 'Bearer expired-token' },
        },
        code: 'ERR_BAD_REQUEST',
      })
      apiClient.defaults.adapter = adapter

      await expect(apiClient.get('/test')).rejects.toBeDefined()

      expect(localStorage.getItem('auth_token')).toBeNull()

      // 恢复 location
      Object.defineProperty(window, 'location', {
        value: originalLocation,
        writable: true,
      })
    })

    it('并发 401 只发起一次 refresh 并重放所有请求', async () => {
      localStorage.setItem('auth_token', 'expired-token')
      localStorage.setItem('refresh_token', 'refresh-token')
      localStorage.setItem('auth_user', JSON.stringify({ id: 1 }))

      const postSpy = vi.spyOn(axios, 'post').mockResolvedValue({
        data: {
          code: 0,
          data: {
            access_token: 'renewed-token',
            refresh_token: 'renewed-refresh-token',
            expires_in: 3600,
          },
        },
      })
      const adapter = vi.fn(async (config) => {
        if (config.headers.get('Authorization') === 'Bearer renewed-token') {
          return { status: 200, data: { code: 0, data: { ok: true } }, headers: {}, config, statusText: 'OK' }
        }
        throw {
          response: { status: 401, data: { code: 'TOKEN_EXPIRED', message: 'expired' } },
          config,
          code: 'ERR_BAD_REQUEST',
        }
      })
      apiClient.defaults.adapter = adapter

      const results = await Promise.all([apiClient.get('/one'), apiClient.get('/two')])

      expect(results.map((result) => result.data)).toEqual([{ ok: true }, { ok: true }])
      expect(postSpy).toHaveBeenCalledTimes(1)
      expect(postSpy.mock.calls[0][2]).toEqual(expect.objectContaining({ timeout: 30_000 }))
      expect(localStorage.getItem('refresh_token')).toBe('renewed-refresh-token')
    })

    it('主动刷新与 401 刷新共享同一个 singleflight', async () => {
      localStorage.setItem('auth_token', 'expired-token')
      localStorage.setItem('refresh_token', 'refresh-token')
      const postSpy = vi.spyOn(axios, 'post').mockResolvedValue({
        data: {
          code: 0,
          data: {
            access_token: 'renewed-token',
            refresh_token: 'renewed-refresh-token',
            expires_in: 3600,
          },
        },
      })
      const { refreshAuthTokenPair } = await import('@/api/client')
      const { refreshToken } = await import('@/api/auth')

      const [coordinatorResult, authApiResult] = await Promise.all([
        refreshAuthTokenPair('refresh-token'),
        refreshToken('refresh-token'),
      ])

      expect(postSpy).toHaveBeenCalledTimes(1)
      expect(coordinatorResult.access_token).toBe('renewed-token')
      expect(authApiResult.access_token).toBe('renewed-token')
      expect(authApiResult.token_type).toBe('Bearer')
    })

    it('跨标签刷新锁等待期间复用同用户已轮换的新 token pair', async () => {
      localStorage.setItem('auth_token', 'old-token')
      localStorage.setItem('refresh_token', 'old-refresh-token')
      localStorage.setItem('auth_user', JSON.stringify({ id: 7, email: 'same@example.com' }))
      localStorage.setItem('token_expires_at', String(Date.now() + 60_000))

      const postSpy = vi.spyOn(axios, 'post')
      const originalLocksDescriptor = Object.getOwnPropertyDescriptor(navigator, 'locks')
      const lockRequest = vi.fn(async (
        _name: string,
        _options: LockOptions,
        callback: () => Promise<unknown>
      ) => {
        localStorage.setItem('auth_token', 'rotated-token')
        localStorage.setItem('refresh_token', 'rotated-refresh-token')
        localStorage.setItem('token_expires_at', String(Date.now() + 3_600_000))
        return callback()
      })
      Object.defineProperty(navigator, 'locks', {
        configurable: true,
        value: { request: lockRequest }
      })

      try {
        const { refreshAuthTokenPair } = await import('@/api/client')
        const result = await refreshAuthTokenPair('old-refresh-token')

        expect(lockRequest).toHaveBeenCalledTimes(1)
        expect(postSpy).not.toHaveBeenCalled()
        expect(result).toEqual(expect.objectContaining({
          access_token: 'rotated-token',
          refresh_token: 'rotated-refresh-token'
        }))
        expect(result.expires_in).toBeGreaterThan(0)
      } finally {
        if (originalLocksDescriptor) {
          Object.defineProperty(navigator, 'locks', originalLocksDescriptor)
        } else {
          Reflect.deleteProperty(navigator, 'locks')
        }
      }
    })

    it.each([
      ['网络失败', { code: 'ERR_NETWORK', message: 'network unavailable' }],
      ['限流', { response: { status: 429, data: { code: 429, message: 'too many requests' } }, message: '429' }],
      ['服务端失败', { response: { status: 503, data: { code: 503, message: 'unavailable' } }, message: '503' }],
    ])('%s时保留登录态', async (_name, refreshError) => {
      localStorage.setItem('auth_token', 'expired-token')
      localStorage.setItem('refresh_token', 'refresh-token')
      localStorage.setItem('auth_user', JSON.stringify({ id: 1 }))
      vi.spyOn(axios, 'post').mockRejectedValue(refreshError)
      apiClient.defaults.adapter = vi.fn().mockRejectedValue({
        response: { status: 401, data: { code: 'TOKEN_EXPIRED', message: 'expired' } },
        config: { url: '/test', headers: { Authorization: 'Bearer expired-token' } },
        code: 'ERR_BAD_REQUEST',
      })

      await expect(apiClient.get('/test')).rejects.toBeDefined()

      expect(localStorage.getItem('auth_token')).toBe('expired-token')
      expect(localStorage.getItem('refresh_token')).toBe('refresh-token')
      expect(localStorage.getItem('auth_user')).not.toBeNull()
      expect(sessionStorage.getItem('auth_expired')).toBeNull()
    })

    it('refresh 确定失效时清除登录态', async () => {
      const originalLocation = window.location
      Object.defineProperty(window, 'location', {
        value: { ...originalLocation, pathname: '/dashboard', href: '/dashboard' },
        writable: true,
      })
      localStorage.setItem('auth_token', 'expired-token')
      localStorage.setItem('refresh_token', 'refresh-token')
      localStorage.setItem('auth_user', JSON.stringify({ id: 1 }))
      vi.spyOn(axios, 'post').mockRejectedValue({
        response: { status: 401, data: { code: 'REFRESH_TOKEN_INVALID', message: 'invalid' } },
        message: 'invalid',
      })
      apiClient.defaults.adapter = vi.fn().mockRejectedValue({
        response: { status: 401, data: { code: 'TOKEN_EXPIRED', message: 'expired' } },
        config: { url: '/test', headers: { Authorization: 'Bearer expired-token' } },
        code: 'ERR_BAD_REQUEST',
      })

      await expect(apiClient.get('/test')).rejects.toBeDefined()

      expect(localStorage.getItem('auth_token')).toBeNull()
      expect(localStorage.getItem('refresh_token')).toBeNull()
      expect(localStorage.getItem('auth_user')).toBeNull()
      expect(sessionStorage.getItem('auth_expired')).toBe('1')

      Object.defineProperty(window, 'location', {
        value: originalLocation,
        writable: true,
      })
    })

    it('logout 的 401 不触发 refresh 或提前清除本地 token', async () => {
      localStorage.setItem('auth_token', 'current-token')
      localStorage.setItem('refresh_token', 'current-refresh-token')
      const postSpy = vi.spyOn(axios, 'post')
      apiClient.defaults.adapter = vi.fn().mockRejectedValue({
        response: { status: 401, data: { code: 'TOKEN_EXPIRED', message: 'expired' } },
        config: { url: '/auth/logout', headers: { Authorization: 'Bearer current-token' } },
        code: 'ERR_BAD_REQUEST',
      })

      await expect(apiClient.post('/auth/logout')).rejects.toBeDefined()

      expect(postSpy).not.toHaveBeenCalled()
      expect(localStorage.getItem('auth_token')).toBe('current-token')
      expect(localStorage.getItem('refresh_token')).toBe('current-refresh-token')
    })

    it('旧 refresh 响应不得覆盖 logout 或新登录', async () => {
      localStorage.setItem('auth_token', 'old-token')
      localStorage.setItem('refresh_token', 'old-refresh-token')
      let resolveRefresh!: (value: unknown) => void
      const refreshPromise = new Promise((resolve) => {
        resolveRefresh = resolve
      })
      vi.spyOn(axios, 'post').mockReturnValue(refreshPromise as never)
      const { refreshAuthTokenPair } = await import('@/api/client')
      const pending = refreshAuthTokenPair('old-refresh-token')

      localStorage.setItem('auth_token', 'new-login-token')
      localStorage.setItem('refresh_token', 'new-login-refresh-token')
      resolveRefresh({
        data: {
          code: 0,
          data: {
            access_token: 'stale-token',
            refresh_token: 'stale-refresh-token',
            expires_in: 3600,
          },
        },
      })

      await expect(pending).rejects.toMatchObject({ code: 'STALE_REFRESH_RESPONSE' })
      expect(localStorage.getItem('auth_token')).toBe('new-login-token')
      expect(localStorage.getItem('refresh_token')).toBe('new-login-refresh-token')
    })

    it('旧 refresh 的终态错误不得清除已更新的会话', async () => {
      localStorage.setItem('auth_token', 'old-token')
      localStorage.setItem('refresh_token', 'old-refresh-token')
      let rejectRefresh!: (reason: unknown) => void
      const refreshPromise = new Promise((_resolve, reject) => {
        rejectRefresh = reject
      })
      vi.spyOn(axios, 'post').mockReturnValue(refreshPromise as never)
      const { refreshAuthTokenPair } = await import('@/api/client')
      const pending = refreshAuthTokenPair('old-refresh-token')

      localStorage.setItem('auth_token', 'new-login-token')
      localStorage.setItem('refresh_token', 'new-login-refresh-token')
      rejectRefresh({
        response: { status: 401, data: { code: 'REFRESH_TOKEN_INVALID', message: 'invalid' } },
        message: 'invalid',
      })

      await expect(pending).rejects.toMatchObject({ status: 401 })
      expect(localStorage.getItem('auth_token')).toBe('new-login-token')
      expect(localStorage.getItem('refresh_token')).toBe('new-login-refresh-token')
      expect(sessionStorage.getItem('auth_expired')).toBeNull()
    })
  })

  // --- 网络错误 ---

  describe('网络错误', () => {
    it('网络错误返回 status 0 的错误', async () => {
      const adapter = vi.fn().mockRejectedValue({
        code: 'ERR_NETWORK',
        message: 'Network Error',
        config: { url: '/test' },
        // 没有 response
      })
      apiClient.defaults.adapter = adapter

      await expect(apiClient.get('/test')).rejects.toEqual(
        expect.objectContaining({
          status: 0,
          message: 'Network error. Please check your connection.',
        })
      )
    })

    it('请求超时返回明确的超时错误', async () => {
      const adapter = vi.fn().mockRejectedValue({
        code: 'ECONNABORTED',
        message: 'timeout of 30000ms exceeded',
        config: { url: '/test' },
      })
      apiClient.defaults.adapter = adapter

      await expect(apiClient.get('/test')).rejects.toEqual(
        expect.objectContaining({
          status: 0,
          code: 'ECONNABORTED',
          message: 'Request timed out. Please try again later.',
        })
      )
    })
  })

  // --- 请求取消 ---

  describe('请求取消', () => {
    it('取消的请求保持原始取消错误', async () => {
      const source = axios.CancelToken.source()

      const adapter = vi.fn().mockRejectedValue(
        new axios.Cancel('Operation canceled')
      )
      apiClient.defaults.adapter = adapter

      await expect(
        apiClient.get('/test', { cancelToken: source.token })
      ).rejects.toBeDefined()
    })
  })
})
