import { defineStore } from 'pinia'
import { ref } from 'vue'
import { adminAPI } from '@/api'
import type { CustomMenuItem, OpenAIAccountLevelConfig } from '@/types'
import { DEFAULT_OPENAI_ACCOUNT_LEVELS, normalizeOpenAIAccountLevelConfigs } from '@/utils/openaiAccountLevels'

export const useAdminSettingsStore = defineStore('adminSettings', () => {
  const loaded = ref(false)
  const loading = ref(false)

  // 在途请求与代际计数，用于把并发调用合并成一次网络请求。
  //
  // 为什么不能只靠 loading 布尔：旧实现里 `if (loading.value) return` 返回的是一个
  // 立即 resolve 的空 promise，并发调用方拿到的是"假完成"信号——会在数据还没到达时
  // 就基于旧值做分支（OpsDashboard 就依赖 await 后立刻读 opsMonitoringEnabled）。
  // 这里改为返回同一个在途 promise，调用方 await 到的就是真正的完成。
  let activePromise: Promise<void> | null = null
  let requestGeneration = 0

  const readCachedBool = (key: string, defaultValue: boolean): boolean => {
    try {
      const raw = localStorage.getItem(key)
      if (raw === 'true') return true
      if (raw === 'false') return false
    } catch {
      // ignore localStorage failures
    }
    return defaultValue
  }

  const writeCachedBool = (key: string, value: boolean) => {
    try {
      localStorage.setItem(key, value ? 'true' : 'false')
    } catch {
      // ignore localStorage failures
    }
  }

  const readCachedString = (key: string, defaultValue: string): string => {
    try {
      const raw = localStorage.getItem(key)
      if (typeof raw === 'string' && raw.length > 0) return raw
    } catch {
      // ignore localStorage failures
    }
    return defaultValue
  }

  const writeCachedString = (key: string, value: string) => {
    try {
      localStorage.setItem(key, value)
    } catch {
      // ignore localStorage failures
    }
  }

  // Default open, but honor cached value to reduce UI flicker on first paint.
  const opsMonitoringEnabled = ref(readCachedBool('ops_monitoring_enabled_cached', true))
  const opsRealtimeMonitoringEnabled = ref(readCachedBool('ops_realtime_monitoring_enabled_cached', true))
  const opsQueryModeDefault = ref(readCachedString('ops_query_mode_default_cached', 'auto'))
  const paymentEnabled = ref(readCachedBool('payment_enabled_cached', false))
  const customMenuItems = ref<CustomMenuItem[]>([])
  const openAIAccountLevels = ref<OpenAIAccountLevelConfig[]>(normalizeOpenAIAccountLevelConfigs(DEFAULT_OPENAI_ACCOUNT_LEVELS))
  // 提升到 store 是为了消除重复请求：账号弹窗此前为读这一个布尔值而各自直连
  // /admin/settings（211KB），且它们在账号页是常驻挂载的，用户没打开弹窗就已经打了两发。
  const accountQuotaNotifyEnabled = ref(false)

  /**
   * 加载管理端设置。并发调用会合并为同一次网络请求。
   *
   * @param force - 强制重新拉取。**force 永远不会复用在途请求**：
   *   保存设置后调用 fetch(true) 时，若复用一个在 PUT 之前发出的在途请求，
   *   会读回保存前的旧值，表现为"保存成功但界面没变、刷新才好"。
   */
  async function fetch(force = false): Promise<void> {
    if (loaded.value && !force) return
    // 非 force 时合并到在途请求；force 必须另起一发，见上方说明。
    if (!force && activePromise) return activePromise

    const generation = ++requestGeneration
    loading.value = true

    const run = async () => {
      try {
        const [settings, paymentConfigResp] = await Promise.all([
          adminAPI.settings.getSettings(),
          adminAPI.payment.getConfig()
        ])

        // 代际校验：只有最新一次发起的请求可以落盘。并发 force 与旧请求同时在途时，
        // 保证"最后发起的胜出"，而不是"最后返回的胜出"。
        if (generation !== requestGeneration) return

        opsMonitoringEnabled.value = settings.ops_monitoring_enabled ?? true
        writeCachedBool('ops_monitoring_enabled_cached', opsMonitoringEnabled.value)

        opsRealtimeMonitoringEnabled.value = settings.ops_realtime_monitoring_enabled ?? true
        writeCachedBool('ops_realtime_monitoring_enabled_cached', opsRealtimeMonitoringEnabled.value)

        opsQueryModeDefault.value = settings.ops_query_mode_default || 'auto'
        writeCachedString('ops_query_mode_default_cached', opsQueryModeDefault.value)

        customMenuItems.value = Array.isArray(settings.custom_menu_items) ? settings.custom_menu_items : []
        openAIAccountLevels.value = normalizeOpenAIAccountLevelConfigs(settings.openai_account_levels)
        accountQuotaNotifyEnabled.value = settings.account_quota_notify_enabled === true

        paymentEnabled.value = paymentConfigResp.data?.enabled ?? false
        writeCachedBool('payment_enabled_cached', paymentEnabled.value)

        loaded.value = true
      } catch (err) {
        if (generation !== requestGeneration) return
        // Keep cached/default value: do not "flip" the UI based on a transient fetch failure.
        loaded.value = true
        console.error('[adminSettings] Failed to fetch settings:', err)
      } finally {
        if (generation === requestGeneration) {
          loading.value = false
          activePromise = null
        }
      }
    }

    const promise = run()
    // 只有非 force 的调用方可以被后续调用合并进来；force 请求不对外共享，
    // 避免它被当作"某个更早请求的结果"复用。
    if (!force) activePromise = promise
    return promise
  }

  // ── Web Search 模拟：全局是否可用 ──
  //
  // 独立于 fetch() 的 action：它是独立端点、独立后端 handler，合并进 fetch() 会让
  // 侧边栏的设置加载额外背上一次往返。这里要的是"同 store、不同 action、各自去重"。
  const webSearchGlobalEnabled = ref(false)
  const webSearchLoaded = ref(false)
  let webSearchPromise: Promise<void> | null = null
  let webSearchGeneration = 0

  /**
   * 确保 Web Search 全局开关已加载；并发调用合并为一次请求。
   * @param force - 强制重新拉取，不复用在途请求（语义同 fetch）。
   */
  async function ensureWebSearchEmulation(force = false): Promise<void> {
    if (webSearchLoaded.value && !force) return
    if (!force && webSearchPromise) return webSearchPromise

    const generation = ++webSearchGeneration
    const run = async () => {
      try {
        const cfg = await adminAPI.settings.getWebSearchEmulationConfig()
        if (generation !== webSearchGeneration) return
        webSearchGlobalEnabled.value = cfg?.enabled === true && (cfg?.providers?.length ?? 0) > 0
        webSearchLoaded.value = true
      } catch {
        if (generation !== webSearchGeneration) return
        webSearchGlobalEnabled.value = false
        webSearchLoaded.value = true
      } finally {
        if (generation === webSearchGeneration) webSearchPromise = null
      }
    }
    const promise = run()
    if (!force) webSearchPromise = promise
    return promise
  }

  /**
   * 使 Web Search 缓存失效。
   * 设置页保存全局开关后必须调用，否则同一 SPA 会话内回到账号页，
   * 弹窗读到的仍是旧状态。
   */
  function invalidateWebSearchEmulation() {
    webSearchLoaded.value = false
    webSearchPromise = null
    webSearchGeneration++
  }

  function setOpsMonitoringEnabledLocal(value: boolean) {
    opsMonitoringEnabled.value = value
    writeCachedBool('ops_monitoring_enabled_cached', value)
    loaded.value = true
  }

  function setOpsRealtimeMonitoringEnabledLocal(value: boolean) {
    opsRealtimeMonitoringEnabled.value = value
    writeCachedBool('ops_realtime_monitoring_enabled_cached', value)
    loaded.value = true
  }

  function setPaymentEnabledLocal(value: boolean) {
    paymentEnabled.value = value
    writeCachedBool('payment_enabled_cached', value)
    loaded.value = true
  }

  function setOpsQueryModeDefaultLocal(value: string) {
    opsQueryModeDefault.value = value || 'auto'
    writeCachedString('ops_query_mode_default_cached', opsQueryModeDefault.value)
    loaded.value = true
  }

  // Keep UI consistent if we learn that ops is disabled via feature-gated 404s.
  // (event is dispatched from the axios interceptor)
  let eventHandlerCleanup: (() => void) | null = null

  function initializeEventListeners() {
    if (eventHandlerCleanup) return

    try {
      const handler = () => {
        setOpsMonitoringEnabledLocal(false)
      }
      window.addEventListener('ops-monitoring-disabled', handler)
      eventHandlerCleanup = () => {
        window.removeEventListener('ops-monitoring-disabled', handler)
      }
    } catch {
      // ignore window access failures (SSR)
    }
  }

  if (typeof window !== 'undefined') {
    initializeEventListeners()
  }

  return {
    loaded,
    loading,
    opsMonitoringEnabled,
    opsRealtimeMonitoringEnabled,
    opsQueryModeDefault,
    paymentEnabled,
    customMenuItems,
    openAIAccountLevels,
    accountQuotaNotifyEnabled,
    webSearchGlobalEnabled,
    fetch,
    ensureWebSearchEmulation,
    invalidateWebSearchEmulation,
    setOpsMonitoringEnabledLocal,
    setOpsRealtimeMonitoringEnabledLocal,
    setPaymentEnabledLocal,
    setOpsQueryModeDefaultLocal
  }
})
