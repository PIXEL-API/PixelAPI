import { defineStore } from 'pinia'
import { onScopeDispose, ref } from 'vue'
import { conversationsAPI } from '@/api'
import { adminAPI } from '@/api/admin'

type ConversationUnreadScope = 'admin' | 'user'
type PollingTimer = ReturnType<typeof setTimeout>

const POLL_INTERVAL_MS = 15000
const CONVERSATION_UNREAD_SCOPES: readonly ConversationUnreadScope[] = ['admin', 'user']

export const useConversationNotificationStore = defineStore('conversationNotifications', () => {
  const adminUnreadCount = ref(0)
  const userUnreadCount = ref(0)
  const loading = ref(false)

  const pollingScopes = new Set<ConversationUnreadScope>()
  const pollingTimers: Partial<Record<ConversationUnreadScope, PollingTimer>> = {}
  const inFlightRequests: Partial<Record<ConversationUnreadScope, Promise<number>>> = {}
  const inFlightRequestTokens: Partial<Record<ConversationUnreadScope, object>> = {}
  const lastFetchedAt: Record<ConversationUnreadScope, number | null> = {
    admin: null,
    user: null
  }
  const requestGenerations: Record<ConversationUnreadScope, number> = {
    admin: 0,
    user: 0
  }
  let visibilityListenerRegistered = false

  function currentUnreadCount(scope: ConversationUnreadScope): number {
    return scope === 'admin' ? adminUnreadCount.value : userUnreadCount.value
  }

  function updateLoading(): void {
    loading.value = CONVERSATION_UNREAD_SCOPES.some((scope) => Boolean(inFlightRequests[scope]))
  }

  function isVisible(): boolean {
    return typeof document === 'undefined' || document.visibilityState === 'visible'
  }

  function freshnessRemaining(scope: ConversationUnreadScope): number {
    const fetchedAt = lastFetchedAt[scope]
    if (fetchedAt === null) return 0
    return Math.min(
      POLL_INTERVAL_MS,
      Math.max(0, POLL_INTERVAL_MS - (Date.now() - fetchedAt))
    )
  }

  /**
   * Fetch an unread count. Direct callers force a refresh by default; passive
   * polling opts into freshness checks while still sharing any in-flight call.
   */
  function fetchUnreadCount(scope: ConversationUnreadScope, force = true): Promise<number> {
    const inFlight = inFlightRequests[scope]
    if (inFlight) return inFlight

    if (!force && freshnessRemaining(scope) > 0) {
      return Promise.resolve(currentUnreadCount(scope))
    }

    const requestGeneration = requestGenerations[scope]
    const requestToken = {}
    inFlightRequestTokens[scope] = requestToken
    loading.value = true
    let apiRequest: Promise<{ count: number }>
    try {
      apiRequest = scope === 'admin'
        ? adminAPI.conversations.unreadCount()
        : conversationsAPI.unreadCount()
    } catch (error) {
      apiRequest = Promise.reject(error)
    }

    const requestPromise = apiRequest
      .then((result) => {
        if (requestGenerations[scope] === requestGeneration) {
          setUnreadCount(scope, result.count)
          lastFetchedAt[scope] = Date.now()
          if (pollingScopes.has(scope) && isVisible()) {
            schedulePolling(scope, POLL_INTERVAL_MS)
          }
        }
        return result.count
      })
      .catch((error) => {
        console.error('Failed to fetch conversation unread count:', error)
        return currentUnreadCount(scope)
      })
      .finally(() => {
        if (inFlightRequestTokens[scope] === requestToken) {
          delete inFlightRequestTokens[scope]
          delete inFlightRequests[scope]
        }
        updateLoading()
      })

    inFlightRequests[scope] = requestPromise
    return requestPromise
  }

  function setUnreadCount(scope: ConversationUnreadScope, count: number): void {
    const normalized = Math.max(0, Number.isFinite(count) ? count : 0)
    if (scope === 'admin') {
      adminUnreadCount.value = normalized
      return
    }
    userUnreadCount.value = normalized
  }

  function clearPollingTimer(scope: ConversationUnreadScope): void {
    const timer = pollingTimers[scope]
    if (!timer) return
    clearTimeout(timer)
    delete pollingTimers[scope]
  }

  function schedulePolling(scope: ConversationUnreadScope, delay = freshnessRemaining(scope)): void {
    clearPollingTimer(scope)
    if (!pollingScopes.has(scope) || !isVisible()) return

    if (delay <= 0) {
      void runScheduledPoll(scope)
      return
    }

    pollingTimers[scope] = setTimeout(() => {
      delete pollingTimers[scope]
      void runScheduledPoll(scope)
    }, delay)
  }

  async function runScheduledPoll(scope: ConversationUnreadScope): Promise<void> {
    if (!pollingScopes.has(scope) || !isVisible()) return
    try {
      await fetchUnreadCount(scope, false)
    } finally {
      schedulePolling(scope, POLL_INTERVAL_MS)
    }
  }

  function handleVisibilityChange(): void {
    if (!isVisible()) {
      for (const scope of pollingScopes) {
        clearPollingTimer(scope)
      }
      return
    }

    for (const scope of pollingScopes) {
      schedulePolling(scope)
    }
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

  function startPolling(scope: ConversationUnreadScope): void {
    if (pollingScopes.has(scope)) return
    pollingScopes.add(scope)
    registerVisibilityListener()
    schedulePolling(scope)
  }

  function stopPolling(scope?: ConversationUnreadScope): void {
    if (scope) {
      pollingScopes.delete(scope)
      clearPollingTimer(scope)
    } else {
      pollingScopes.clear()
      for (const timerScope of CONVERSATION_UNREAD_SCOPES) {
        clearPollingTimer(timerScope)
      }
    }

    if (pollingScopes.size === 0) unregisterVisibilityListener()
  }

  function reset(): void {
    stopPolling()
    for (const scope of CONVERSATION_UNREAD_SCOPES) {
      requestGenerations[scope]++
      delete inFlightRequestTokens[scope]
      delete inFlightRequests[scope]
      lastFetchedAt[scope] = null
    }
    adminUnreadCount.value = 0
    userUnreadCount.value = 0
    updateLoading()
  }

  onScopeDispose(reset)

  return {
    adminUnreadCount,
    userUnreadCount,
    loading,
    fetchUnreadCount,
    setUnreadCount,
    startPolling,
    stopPolling,
    reset
  }
})
