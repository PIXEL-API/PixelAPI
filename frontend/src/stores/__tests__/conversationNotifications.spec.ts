import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useConversationNotificationStore } from '@/stores/conversationNotifications'

const mockUserUnreadCount = vi.fn()
const mockAdminUnreadCount = vi.fn()
let documentVisibilityState: DocumentVisibilityState = 'visible'

vi.mock('@/api', () => ({
  conversationsAPI: {
    unreadCount: (...args: unknown[]) => mockUserUnreadCount(...args)
  }
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    conversations: {
      unreadCount: (...args: unknown[]) => mockAdminUnreadCount(...args)
    }
  }
}))

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

describe('useConversationNotificationStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.useFakeTimers()
    vi.clearAllMocks()
    documentVisibilityState = 'visible'
    vi.spyOn(document, 'visibilityState', 'get').mockImplementation(() => documentVisibilityState)
  })

  afterEach(() => {
    useConversationNotificationStore().$dispose()
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  it('合并同一 scope 的并发请求', async () => {
    const pending = createDeferred<{ count: number }>()
    mockUserUnreadCount.mockReturnValue(pending.promise)
    const store = useConversationNotificationStore()

    const first = store.fetchUnreadCount('user')
    const second = store.fetchUnreadCount('user')

    expect(mockUserUnreadCount).toHaveBeenCalledTimes(1)
    expect(store.loading).toBe(true)

    pending.resolve({ count: 3 })
    await expect(first).resolves.toBe(3)
    await expect(second).resolves.toBe(3)
    expect(store.userUnreadCount).toBe(3)
    expect(store.loading).toBe(false)
  })

  it('admin 与 user scope 独立请求', async () => {
    const userPending = createDeferred<{ count: number }>()
    const adminPending = createDeferred<{ count: number }>()
    mockUserUnreadCount.mockReturnValue(userPending.promise)
    mockAdminUnreadCount.mockReturnValue(adminPending.promise)
    const store = useConversationNotificationStore()

    const userRequest = store.fetchUnreadCount('user')
    const adminRequest = store.fetchUnreadCount('admin')

    expect(mockUserUnreadCount).toHaveBeenCalledTimes(1)
    expect(mockAdminUnreadCount).toHaveBeenCalledTimes(1)

    userPending.resolve({ count: 2 })
    adminPending.resolve({ count: 5 })
    await Promise.all([userRequest, adminRequest])

    expect(store.userUnreadCount).toBe(2)
    expect(store.adminUnreadCount).toBe(5)
  })

  it('直接业务调用在 freshness 窗口内仍强制刷新', async () => {
    mockUserUnreadCount
      .mockResolvedValueOnce({ count: 1 })
      .mockResolvedValueOnce({ count: 2 })
    const store = useConversationNotificationStore()

    await store.fetchUnreadCount('user')
    await store.fetchUnreadCount('user')

    expect(mockUserUnreadCount).toHaveBeenCalledTimes(2)
    expect(store.userUnreadCount).toBe(2)
  })

  it('重启轮询时复用 freshness 并保持 15 秒周期', async () => {
    mockUserUnreadCount.mockResolvedValue({ count: 1 })
    const store = useConversationNotificationStore()

    store.startPolling('user')
    await vi.advanceTimersByTimeAsync(0)
    expect(mockUserUnreadCount).toHaveBeenCalledTimes(1)

    store.stopPolling('user')
    await vi.advanceTimersByTimeAsync(5_000)
    store.startPolling('user')

    await vi.advanceTimersByTimeAsync(10_000 - 1)
    expect(mockUserUnreadCount).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(1)
    expect(mockUserUnreadCount).toHaveBeenCalledTimes(2)
  })

  it('隐藏时暂停轮询，恢复可见后仅刷新过期 scope 一次', async () => {
    mockUserUnreadCount.mockResolvedValue({ count: 1 })
    const store = useConversationNotificationStore()

    store.startPolling('user')
    await vi.advanceTimersByTimeAsync(0)
    expect(mockUserUnreadCount).toHaveBeenCalledTimes(1)

    setDocumentVisibility('hidden')
    await vi.advanceTimersByTimeAsync(15_000 - 1)
    setDocumentVisibility('visible')
    expect(mockUserUnreadCount).toHaveBeenCalledTimes(1)

    setDocumentVisibility('hidden')
    await vi.advanceTimersByTimeAsync(1)
    setDocumentVisibility('visible')
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(0)

    expect(mockUserUnreadCount).toHaveBeenCalledTimes(2)
  })

  it('reset 后旧响应不能覆盖新状态', async () => {
    const staleRequest = createDeferred<{ count: number }>()
    const currentRequest = createDeferred<{ count: number }>()
    mockUserUnreadCount
      .mockReturnValueOnce(staleRequest.promise)
      .mockReturnValueOnce(currentRequest.promise)
    const store = useConversationNotificationStore()

    const staleResult = store.fetchUnreadCount('user')
    store.reset()
    const currentResult = store.fetchUnreadCount('user')

    staleRequest.resolve({ count: 99 })
    await staleResult
    expect(store.userUnreadCount).toBe(0)

    currentRequest.resolve({ count: 4 })
    await currentResult
    expect(store.userUnreadCount).toBe(4)
  })

  it('会话 reset 后不会复用旧请求或 freshness', async () => {
    const staleRequest = createDeferred<{ count: number }>()
    mockUserUnreadCount
      .mockReturnValueOnce(staleRequest.promise)
      .mockResolvedValueOnce({ count: 4 })
    const store = useConversationNotificationStore()

    store.startPolling('user')
    await vi.advanceTimersByTimeAsync(0)
    expect(mockUserUnreadCount).toHaveBeenCalledTimes(1)

    store.reset()
    store.startPolling('user')
    await vi.advanceTimersByTimeAsync(0)
    expect(mockUserUnreadCount).toHaveBeenCalledTimes(2)
    expect(store.userUnreadCount).toBe(4)

    staleRequest.resolve({ count: 99 })
    await Promise.resolve()
    await Promise.resolve()
    expect(store.userUnreadCount).toBe(4)
  })
})
