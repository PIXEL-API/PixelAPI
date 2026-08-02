import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useAdminSettingsStore } from '@/stores/adminSettings'

const mockGetSettings = vi.fn()
const mockGetPaymentConfig = vi.fn()
const mockGetWebSearchEmulationConfig = vi.fn()

vi.mock('@/api', () => ({
  adminAPI: {
    settings: {
      getSettings: (...args: any[]) => mockGetSettings(...args),
      getWebSearchEmulationConfig: (...args: any[]) => mockGetWebSearchEmulationConfig(...args),
    },
    payment: {
      getConfig: (...args: any[]) => mockGetPaymentConfig(...args),
    },
  },
}))

/** 构造一个可由测试手动 resolve 的 promise，用来精确制造"在途"状态。 */
function deferred<T>() {
  let resolve!: (v: T) => void
  let reject!: (e: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

const baseSettings = (overrides: Record<string, unknown> = {}) => ({
  ops_monitoring_enabled: true,
  ops_realtime_monitoring_enabled: true,
  ops_query_mode_default: 'auto',
  custom_menu_items: [],
  openai_account_levels: [],
  account_quota_notify_enabled: false,
  ...overrides,
})

describe('adminSettings store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
    mockGetSettings.mockReset()
    mockGetPaymentConfig.mockReset()
    mockGetWebSearchEmulationConfig.mockReset()
    mockGetPaymentConfig.mockResolvedValue({ data: { enabled: false } })
  })

  describe('fetch 去重', () => {
    it('并发调用只发一次请求，且都等到数据真正到达', async () => {
      // 这是本次改造的核心：/admin/settings 是 211KB 的响应，
      // 打开 /admin/accounts 时曾有 3 个调用点各打一发。
      const d = deferred<any>()
      mockGetSettings.mockReturnValue(d.promise)

      const store = useAdminSettingsStore()
      const a = store.fetch()
      const b = store.fetch()
      const c = store.fetch()

      expect(mockGetSettings).toHaveBeenCalledTimes(1)

      d.resolve(baseSettings({ account_quota_notify_enabled: true }))
      await Promise.all([a, b, c])

      // 旧实现里后两个调用会拿到一个立即 resolve 的空 promise，
      // await 返回时数据还没到——这里断言它们等到了真实结果。
      expect(store.accountQuotaNotifyEnabled).toBe(true)
      expect(store.loaded).toBe(true)
      expect(mockGetSettings).toHaveBeenCalledTimes(1)
    })

    it('已加载后再调用不产生请求', async () => {
      mockGetSettings.mockResolvedValue(baseSettings())
      const store = useAdminSettingsStore()

      await store.fetch()
      await store.fetch()
      await store.fetch()

      expect(mockGetSettings).toHaveBeenCalledTimes(1)
    })

    it('force 不复用在途请求，避免读回保存前的旧值', async () => {
      // 场景：管理端保存设置。若 force 复用了一个在 PUT 之前发出的在途请求，
      // 就会把保存前的旧值写回 store，表现为"保存成功但界面没变、刷新才好"。
      const stale = deferred<any>()
      mockGetSettings.mockReturnValueOnce(stale.promise)

      const store = useAdminSettingsStore()
      const inflight = store.fetch()
      expect(mockGetSettings).toHaveBeenCalledTimes(1)

      // 保存后的强刷：必须另起一发请求
      mockGetSettings.mockResolvedValueOnce(baseSettings({ account_quota_notify_enabled: true }))
      const forced = store.fetch(true)
      expect(mockGetSettings).toHaveBeenCalledTimes(2)

      // 旧请求带着过期数据返回，不得覆盖 force 的结果
      stale.resolve(baseSettings({ account_quota_notify_enabled: false }))
      await Promise.all([inflight, forced])

      expect(store.accountQuotaNotifyEnabled).toBe(true)
    })

    it('后发起的 force 胜出，而不是后返回的胜出', async () => {
      const slow = deferred<any>()
      const fast = deferred<any>()
      mockGetSettings.mockReturnValueOnce(slow.promise).mockReturnValueOnce(fast.promise)

      const store = useAdminSettingsStore()
      const first = store.fetch(true)
      const second = store.fetch(true)

      // 第二发先返回，第一发后返回——最终应保留第二发（最后发起）的结果
      fast.resolve(baseSettings({ ops_query_mode_default: 'second' }))
      await second
      slow.resolve(baseSettings({ ops_query_mode_default: 'first' }))
      await first

      expect(store.opsQueryModeDefault).toBe('second')
    })

    it('请求失败时保留既有值，不因瞬时失败翻转 UI', async () => {
      mockGetSettings.mockRejectedValue(new Error('network down'))
      const store = useAdminSettingsStore()
      const before = store.opsMonitoringEnabled

      await store.fetch()

      expect(store.opsMonitoringEnabled).toBe(before)
      expect(store.loaded).toBe(true)
    })

    it('失败后 force 可以重新发起请求', async () => {
      mockGetSettings.mockRejectedValueOnce(new Error('boom'))
      const store = useAdminSettingsStore()
      await store.fetch()
      expect(mockGetSettings).toHaveBeenCalledTimes(1)

      mockGetSettings.mockResolvedValueOnce(baseSettings({ account_quota_notify_enabled: true }))
      await store.fetch(true)

      expect(mockGetSettings).toHaveBeenCalledTimes(2)
      expect(store.accountQuotaNotifyEnabled).toBe(true)
    })
  })

  describe('ensureWebSearchEmulation', () => {
    it('并发调用合并为一次请求', async () => {
      const d = deferred<any>()
      mockGetWebSearchEmulationConfig.mockReturnValue(d.promise)
      const store = useAdminSettingsStore()

      const calls = [
        store.ensureWebSearchEmulation(),
        store.ensureWebSearchEmulation(),
        store.ensureWebSearchEmulation(),
      ]
      expect(mockGetWebSearchEmulationConfig).toHaveBeenCalledTimes(1)

      d.resolve({ enabled: true, providers: [{ id: 'p1' }] })
      await Promise.all(calls)

      expect(store.webSearchGlobalEnabled).toBe(true)
    })

    it('已加载后不再请求，失效后可重新加载', async () => {
      mockGetWebSearchEmulationConfig.mockResolvedValue({ enabled: true, providers: [{ id: 'p1' }] })
      const store = useAdminSettingsStore()

      await store.ensureWebSearchEmulation()
      await store.ensureWebSearchEmulation()
      expect(mockGetWebSearchEmulationConfig).toHaveBeenCalledTimes(1)
      expect(store.webSearchGlobalEnabled).toBe(true)

      // 设置页保存全局开关后必须失效，否则同一会话内回账号页读到旧状态。
      mockGetWebSearchEmulationConfig.mockResolvedValue({ enabled: false, providers: [] })
      store.invalidateWebSearchEmulation()
      await store.ensureWebSearchEmulation()

      expect(mockGetWebSearchEmulationConfig).toHaveBeenCalledTimes(2)
      expect(store.webSearchGlobalEnabled).toBe(false)
    })

    it('enabled 为 true 但没有 provider 时视为不可用', async () => {
      mockGetWebSearchEmulationConfig.mockResolvedValue({ enabled: true, providers: [] })
      const store = useAdminSettingsStore()

      await store.ensureWebSearchEmulation()

      expect(store.webSearchGlobalEnabled).toBe(false)
    })

    it('请求失败时降级为不可用而不是抛出', async () => {
      mockGetWebSearchEmulationConfig.mockRejectedValue(new Error('boom'))
      const store = useAdminSettingsStore()

      await expect(store.ensureWebSearchEmulation()).resolves.toBeUndefined()
      expect(store.webSearchGlobalEnabled).toBe(false)
    })
  })
})
