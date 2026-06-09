import { defineStore } from 'pinia'
import { ref } from 'vue'
import { conversationsAPI } from '@/api'
import { adminAPI } from '@/api/admin'

type ConversationUnreadScope = 'admin' | 'user'

const POLL_INTERVAL_MS = 15000

export const useConversationNotificationStore = defineStore('conversationNotifications', () => {
  const adminUnreadCount = ref(0)
  const userUnreadCount = ref(0)
  const loading = ref(false)

  const pollingTimers: Partial<Record<ConversationUnreadScope, ReturnType<typeof setInterval>>> = {}

  async function fetchUnreadCount(scope: ConversationUnreadScope): Promise<number> {
    loading.value = true
    try {
      const result = scope === 'admin'
        ? await adminAPI.conversations.unreadCount()
        : await conversationsAPI.unreadCount()
      setUnreadCount(scope, result.count)
      return result.count
    } catch (error) {
      console.error('Failed to fetch conversation unread count:', error)
      return scope === 'admin' ? adminUnreadCount.value : userUnreadCount.value
    } finally {
      loading.value = false
    }
  }

  function setUnreadCount(scope: ConversationUnreadScope, count: number): void {
    const normalized = Math.max(0, Number.isFinite(count) ? count : 0)
    if (scope === 'admin') {
      adminUnreadCount.value = normalized
      return
    }
    userUnreadCount.value = normalized
  }

  function startPolling(scope: ConversationUnreadScope): void {
    if (pollingTimers[scope]) return
    void fetchUnreadCount(scope)
    pollingTimers[scope] = setInterval(() => {
      void fetchUnreadCount(scope)
    }, POLL_INTERVAL_MS)
  }

  function stopPolling(scope?: ConversationUnreadScope): void {
    if (scope) {
      if (pollingTimers[scope]) {
        clearInterval(pollingTimers[scope])
        delete pollingTimers[scope]
      }
      return
    }
    for (const timerScope of ['admin', 'user'] as const) {
      if (pollingTimers[timerScope]) {
        clearInterval(pollingTimers[timerScope])
        delete pollingTimers[timerScope]
      }
    }
  }

  function reset(): void {
    stopPolling()
    adminUnreadCount.value = 0
    userUnreadCount.value = 0
    loading.value = false
  }

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
