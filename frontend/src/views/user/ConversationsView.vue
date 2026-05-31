<template>
  <AppLayout>
    <div class="flex min-h-[calc(100vh-8rem)] flex-col gap-4 xl:h-[calc(100vh-8rem)] xl:min-h-0 xl:overflow-hidden">
      <section class="card p-4">
        <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
          <div class="flex min-w-0 flex-1 flex-wrap items-center gap-3">
            <div class="relative w-full sm:w-72">
              <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
              <input
                v-model="searchQuery"
                type="search"
                class="input pl-9"
                :placeholder="t('conversations.searchPlaceholder')"
                @input="handleSearchInput"
              />
            </div>
            <Select v-model="filters.status" :options="statusFilterOptions" class="w-full sm:w-40" @change="resetAndLoad" />
            <Select v-model="filters.unreadOnly" :options="unreadFilterOptions" class="w-full sm:w-36" @change="resetAndLoad" />
          </div>
          <div class="flex flex-wrap items-center justify-end gap-2">
            <button type="button" class="btn btn-secondary px-3" :disabled="loading" :title="t('common.refresh')" @click="() => loadConversations()">
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
          </div>
        </div>
      </section>

      <div class="grid min-h-0 flex-1 grid-cols-1 gap-4 xl:grid-cols-[minmax(19rem,23rem)_minmax(0,1fr)]">
        <section class="card flex min-h-[32rem] flex-col overflow-hidden xl:min-h-0">
          <div class="flex h-12 flex-shrink-0 items-center justify-between border-b border-gray-100 px-4 dark:border-dark-700">
            <div class="min-w-0">
              <div class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ t('conversations.title') }}</div>
              <div class="text-xs text-gray-500 dark:text-dark-400">{{ pagination.total }}</div>
            </div>
            <Icon name="chat" size="sm" class="text-gray-400 dark:text-dark-400" />
          </div>

          <div class="min-h-0 flex-1 overflow-y-auto">
            <button
              v-for="conversation in conversations"
              :key="conversation.id"
              type="button"
              class="flex w-full gap-3 border-b border-gray-100 px-4 py-3 text-left transition-colors hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-700/60"
              :class="selectedConversation?.id === conversation.id ? 'bg-primary-50/80 dark:bg-primary-900/20' : 'bg-white/70 dark:bg-transparent'"
              @click="selectConversation(conversation)"
            >
              <span
                class="mt-1.5 h-2.5 w-2.5 flex-shrink-0 rounded-full"
                :class="conversation.user_unread ? 'bg-primary-500' : 'bg-gray-300 dark:bg-dark-500'"
              ></span>
              <span class="min-w-0 flex-1">
                <span class="flex items-start justify-between gap-3">
                  <span class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ conversation.subject }}</span>
                  <span class="flex-shrink-0 text-xs text-gray-400 dark:text-dark-400">{{ formatRelativeTime(conversation.last_message_at) }}</span>
                </span>
                <span class="mt-1 flex flex-wrap items-center gap-1.5">
                  <span class="badge" :class="statusBadgeClass(conversation.status)">{{ statusLabel(conversation.status) }}</span>
                  <span class="badge" :class="priorityBadgeClass(conversation.priority)">{{ priorityLabel(conversation.priority) }}</span>
                  <span class="text-xs text-gray-400 dark:text-dark-400">#{{ conversation.id }}</span>
                </span>
                <span class="mt-2 block truncate text-sm text-gray-500 dark:text-dark-300">
                  {{ conversation.last_message_excerpt || t('conversations.noMessages') }}
                </span>
              </span>
            </button>

            <div v-if="!loading && conversations.length === 0" class="px-6 py-12">
              <EmptyState
                :title="t('conversations.empty')"
                :description="t('conversations.emptyDescription')"
                :action-text="t('conversations.startTicket')"
                @action="openCreateDialog"
              />
            </div>
            <div v-if="loading && conversations.length === 0" class="space-y-3 p-4">
              <div v-for="index in 6" :key="index" class="h-20 animate-pulse rounded-xl bg-gray-100 dark:bg-dark-700"></div>
            </div>
          </div>

          <div v-if="pagination.total > pagination.page_size" class="flex-shrink-0 border-t border-gray-100 p-3 dark:border-dark-700">
            <Pagination
              :page="pagination.page"
              :total="pagination.total"
              :page-size="pagination.page_size"
              @update:page="handlePageChange"
              @update:pageSize="handlePageSizeChange"
            />
          </div>
        </section>

        <section class="card flex min-h-[32rem] flex-col overflow-hidden xl:min-h-0">
          <div v-if="selectedConversation" class="flex min-h-0 flex-1 flex-col">
            <div class="flex-shrink-0 border-b border-gray-100 px-5 py-4 dark:border-dark-700">
              <div class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                <div class="min-w-0">
                  <div class="flex flex-wrap items-center gap-2">
                    <h2 class="truncate text-lg font-semibold text-gray-900 dark:text-white">{{ selectedConversation.subject }}</h2>
                    <span v-if="selectedConversation.user_unread" class="badge badge-primary">{{ t('conversations.unread') }}</span>
                  </div>
                  <div class="mt-2 flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-dark-300">
                    <span>#{{ selectedConversation.id }}</span>
                    <span>{{ typeLabel(selectedConversation.type) }}</span>
                    <span>{{ t('conversations.createdAt', { time: formatDateTime(selectedConversation.created_at) }) }}</span>
                  </div>
                </div>
                <div class="flex flex-wrap items-center gap-2">
                  <button
                    type="button"
                    class="btn btn-secondary btn-sm"
                    :disabled="markingRead"
                    @click="markSelectedRead"
                  >
                    <Icon name="checkCircle" size="sm" class="mr-1" />
                    {{ t('conversations.markRead') }}
                  </button>
                  <button
                    type="button"
                    class="btn btn-secondary btn-sm"
                    :disabled="selectedConversation.status === 'closed' || closing"
                    @click="closeSelectedConversation"
                  >
                    <Icon name="xCircle" size="sm" class="mr-1" />
                    {{ t('conversations.close') }}
                  </button>
                </div>
              </div>
            </div>

            <div ref="messagePaneRef" class="min-h-[18rem] flex-1 space-y-3 overflow-y-auto bg-gray-50/70 p-4 dark:bg-dark-900/40">
              <div v-if="messagesLoading" class="space-y-3">
                <div v-for="index in 4" :key="index" class="h-20 animate-pulse rounded-xl bg-white dark:bg-dark-700"></div>
              </div>
              <div
                v-for="message in messages"
                :key="message.id"
                class="flex"
                :class="message.sender_type === 'user' ? 'justify-end' : 'justify-start'"
              >
                <article
                  class="min-w-[14rem] max-w-[min(48rem,88%)] rounded-xl border px-4 py-3 shadow-sm"
                  :class="message.sender_type === 'user'
                    ? 'border-primary-100 bg-primary-600 text-white dark:border-primary-800'
                    : message.sender_type === 'system'
                      ? 'border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-900/50 dark:bg-amber-900/20 dark:text-amber-100'
                      : 'border-gray-200 bg-white text-gray-800 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-100'"
                >
                  <div class="mb-1 flex items-center justify-between gap-3 text-xs" :class="message.sender_type === 'user' ? 'text-primary-100' : 'text-gray-500 dark:text-dark-300'">
                    <span class="font-medium">{{ senderLabel(message.sender_type) }}</span>
                    <time>{{ formatDateTime(message.created_at) }}</time>
                  </div>
                  <p class="whitespace-pre-wrap break-words text-sm leading-6">{{ message.content }}</p>
                </article>
              </div>
              <div v-if="!messagesLoading && messages.length === 0" class="flex min-h-[18rem] items-center justify-center text-sm text-gray-500 dark:text-dark-300">
                {{ t('conversations.noMessages') }}
              </div>
            </div>

            <form class="flex-shrink-0 border-t border-gray-100 bg-white p-4 dark:border-dark-700 dark:bg-dark-800" @submit.prevent="sendReply">
              <textarea
                v-model="replyContent"
                rows="3"
                class="input resize-none"
                :disabled="sending"
                :placeholder="selectedConversation.status === 'closed' ? t('conversations.reopenPlaceholder') : t('conversations.replyPlaceholder')"
              ></textarea>
              <div class="mt-3 flex items-center justify-end">
                <button type="submit" class="btn btn-primary" :disabled="!replyContent.trim() || sending">
                  <Icon name="arrowRight" size="md" class="mr-1" />
                  {{ sending ? t('common.saving') : t('conversations.send') }}
                </button>
              </div>
            </form>
          </div>

          <div v-else class="flex min-h-[32rem] flex-1 items-center justify-center p-6">
            <EmptyState
              :title="t('conversations.noSelection')"
              :description="t('conversations.noSelectionDescription')"
              :action-text="t('conversations.startTicket')"
              @action="openCreateDialog"
            />
          </div>
        </section>
      </div>
    </div>

    <BaseDialog
      :show="showCreateDialog"
      :title="t('conversations.startTicket')"
      width="wide"
      @close="showCreateDialog = false"
    >
      <form id="conversation-create-form" class="space-y-4" @submit.prevent="createConversation">
        <div>
          <label class="input-label">{{ t('conversations.subject') }}</label>
          <input v-model.trim="createForm.subject" type="text" class="input" maxlength="160" required />
        </div>
        <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <div>
            <label class="input-label">{{ t('conversations.type') }}</label>
            <Select v-model="createForm.type" :options="typeOptions" />
          </div>
          <div>
            <label class="input-label">{{ t('conversations.priority') }}</label>
            <Select v-model="createForm.priority" :options="priorityOptions" />
          </div>
        </div>
        <div>
          <label class="input-label">{{ t('conversations.content') }}</label>
          <textarea v-model.trim="createForm.content" rows="6" class="input" required></textarea>
        </div>
      </form>

      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="showCreateDialog = false">{{ t('common.cancel') }}</button>
          <button type="submit" form="conversation-create-form" class="btn btn-primary" :disabled="creating">
            {{ creating ? t('common.saving') : t('conversations.startTicket') }}
          </button>
        </div>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { conversationsAPI } from '@/api'
import { useAppStore, useConversationNotificationStore } from '@/stores'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime, formatRelativeTime } from '@/utils/format'
import type {
  Conversation,
  ConversationMessage,
  ConversationPriority,
  ConversationStatus,
  ConversationType,
  SelectOption
} from '@/types'

import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'

const { t } = useI18n()
const appStore = useAppStore()
const conversationNotificationStore = useConversationNotificationStore()

const conversations = ref<Conversation[]>([])
const selectedConversation = ref<Conversation | null>(null)
const messages = ref<ConversationMessage[]>([])
const loading = ref(false)
const messagesLoading = ref(false)
const sending = ref(false)
const creating = ref(false)
const closing = ref(false)
const markingRead = ref(false)
const searchQuery = ref('')
const replyContent = ref('')
const showCreateDialog = ref(false)
const messagePaneRef = ref<HTMLElement | null>(null)
let searchTimer: ReturnType<typeof setTimeout> | undefined
let refreshTimer: ReturnType<typeof setInterval> | undefined
let conversationRequestSeq = 0
let messageRequestSeq = 0

const REFRESH_INTERVAL_MS = 10000
const MESSAGE_BOTTOM_THRESHOLD_PX = 80

const pagination = reactive({
  page: 1,
  page_size: 20,
  total: 0,
  pages: 1
})

const filters = reactive<{
  status: ConversationStatus | ''
  unreadOnly: boolean
}>({
  status: '',
  unreadOnly: false
})

const createForm = reactive<{
  subject: string
  content: string
  priority: ConversationPriority
  type: ConversationType
}>({
  subject: '',
  content: '',
  priority: 'normal',
  type: 'support'
})

const statusFilterOptions = computed<SelectOption[]>(() => [
  { value: '', label: t('conversations.allStatus') },
  { value: 'open', label: statusLabel('open') },
  { value: 'pending_user', label: statusLabel('pending_user') },
  { value: 'pending_admin', label: statusLabel('pending_admin') },
  { value: 'resolved', label: statusLabel('resolved') },
  { value: 'closed', label: statusLabel('closed') }
])

const unreadFilterOptions = computed<SelectOption[]>(() => [
  { value: false, label: t('conversations.allMessages') },
  { value: true, label: t('conversations.unreadOnly') }
])

const priorityOptions = computed<SelectOption[]>(() => [
  { value: 'low', label: priorityLabel('low') },
  { value: 'normal', label: priorityLabel('normal') },
  { value: 'high', label: priorityLabel('high') },
  { value: 'urgent', label: priorityLabel('urgent') }
])

const typeOptions = computed<SelectOption[]>(() => [
  { value: 'support', label: typeLabel('support') },
  { value: 'notice', label: typeLabel('notice') },
  { value: 'billing', label: typeLabel('billing') },
  { value: 'subscription', label: typeLabel('subscription') },
  { value: 'account', label: typeLabel('account') },
  { value: 'security', label: typeLabel('security') }
])

function statusLabel(status: ConversationStatus): string {
  return t(`conversations.statusLabels.${status}`)
}

function priorityLabel(priority: ConversationPriority): string {
  return t(`conversations.priorityLabels.${priority}`)
}

function typeLabel(type: ConversationType): string {
  return t(`conversations.typeLabels.${type}`)
}

function senderLabel(sender: string): string {
  return t(`conversations.senderLabels.${sender}`)
}

function statusBadgeClass(status: ConversationStatus): string {
  switch (status) {
    case 'pending_user':
      return 'badge-warning'
    case 'pending_admin':
      return 'badge-primary'
    case 'resolved':
      return 'badge-success'
    case 'closed':
      return 'badge-gray'
    default:
      return 'badge-primary'
  }
}

function priorityBadgeClass(priority: ConversationPriority): string {
  switch (priority) {
    case 'urgent':
      return 'badge-danger'
    case 'high':
      return 'badge-warning'
    case 'low':
      return 'badge-gray'
    default:
      return 'badge-primary'
  }
}

function updateConversationInList(next: Conversation): void {
  const index = conversations.value.findIndex((item) => item.id === next.id)
  if (index >= 0) {
    conversations.value.splice(index, 1, next)
  } else {
    conversations.value.unshift(next)
  }
  if (selectedConversation.value?.id === next.id) {
    selectedConversation.value = next
  }
}

async function loadConversations(options: { silent?: boolean } = {}): Promise<void> {
  const requestSeq = ++conversationRequestSeq
  if (!options.silent) loading.value = true
  try {
    const result = await conversationsAPI.list(pagination.page, pagination.page_size, {
      status: filters.status,
      search: searchQuery.value.trim(),
      unread_only: filters.unreadOnly,
      sort_by: 'last_message_at',
      sort_order: 'desc'
    })
    if (requestSeq !== conversationRequestSeq) return
    conversations.value = result.items
    pagination.total = result.total
    pagination.page = result.page
    pagination.page_size = result.page_size
    pagination.pages = result.pages
    if (selectedConversation.value && !result.items.some((item) => item.id === selectedConversation.value?.id)) {
      selectedConversation.value = null
      messages.value = []
    }
    if (!selectedConversation.value && result.items.length > 0 && !options.silent) {
      await selectConversation(result.items[0])
    }
  } catch (error) {
    if (!options.silent) {
      appStore.showError(extractApiErrorMessage(error, t('conversations.loadFailed')))
    } else {
      console.error('Failed to refresh conversations:', error)
    }
  } finally {
    if (!options.silent) loading.value = false
  }
}

function isMessagePaneNearBottom(): boolean {
  const pane = messagePaneRef.value
  if (!pane) return true
  return pane.scrollHeight - pane.scrollTop - pane.clientHeight <= MESSAGE_BOTTOM_THRESHOLD_PX
}

function scrollMessagePaneToBottom(): void {
  const pane = messagePaneRef.value
  if (!pane) return
  pane.scrollTo({ top: pane.scrollHeight })
}

async function loadUnreadCount(): Promise<void> {
  await conversationNotificationStore.fetchUnreadCount('user')
}

async function loadMessages(conversation: Conversation, options: { silent?: boolean, refreshUnread?: boolean } = {}): Promise<void> {
  const requestSeq = ++messageRequestSeq
  if (!options.silent) messagesLoading.value = true
  try {
    const result = await conversationsAPI.listMessages(conversation.id, 1, 100)
    if (requestSeq !== messageRequestSeq || selectedConversation.value?.id !== conversation.id) return
    const shouldScrollToBottom = !options.silent || (
      result.items.length !== messages.value.length && isMessagePaneNearBottom()
    )
    messages.value = result.items
    const lastVisibleMessageId = result.items.at(-1)?.id
    const readConversation = await conversationsAPI.markRead(conversation.id, lastVisibleMessageId)
    if (requestSeq !== messageRequestSeq || selectedConversation.value?.id !== conversation.id) return
    updateConversationInList(readConversation)
    if (options.refreshUnread !== false) await loadUnreadCount()
    if (shouldScrollToBottom) {
      await nextTick()
      scrollMessagePaneToBottom()
    }
  } catch (error) {
    if (!options.silent) {
      appStore.showError(extractApiErrorMessage(error, t('conversations.loadMessagesFailed')))
    } else {
      console.error('Failed to refresh conversation messages:', error)
    }
  } finally {
    if (!options.silent) messagesLoading.value = false
  }
}

async function selectConversation(conversation: Conversation): Promise<void> {
  selectedConversation.value = conversation
  replyContent.value = ''
  await loadMessages(conversation)
}

async function refreshLiveData(): Promise<void> {
  await Promise.all([
    loadConversations({ silent: true }),
    selectedConversation.value
      ? loadMessages(selectedConversation.value, { silent: true })
      : Promise.resolve()
  ])
}

function startLiveRefresh(): void {
  stopLiveRefresh()
  refreshTimer = setInterval(() => {
    void refreshLiveData()
  }, REFRESH_INTERVAL_MS)
}

function stopLiveRefresh(): void {
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = undefined
  }
}

function handleSearchInput(): void {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    resetAndLoad()
  }, 300)
}

function resetAndLoad(): void {
  pagination.page = 1
  void loadConversations()
}

function handlePageChange(page: number): void {
  pagination.page = page
  void loadConversations()
}

function handlePageSizeChange(pageSize: number): void {
  pagination.page = 1
  pagination.page_size = pageSize
  void loadConversations()
}

function openCreateDialog(): void {
  createForm.subject = t('conversations.defaultSubject')
  createForm.content = ''
  createForm.priority = 'normal'
  createForm.type = 'support'
  showCreateDialog.value = true
}

async function createConversation(): Promise<void> {
  if (!createForm.subject.trim() || !createForm.content.trim()) return
  creating.value = true
  try {
    const conversation = await conversationsAPI.create({
      subject: createForm.subject.trim(),
      content: createForm.content.trim(),
      priority: createForm.priority,
      type: createForm.type
    })
    showCreateDialog.value = false
    updateConversationInList(conversation)
    selectedConversation.value = conversation
    await loadMessages(conversation, { refreshUnread: false })
    await loadUnreadCount()
    appStore.showSuccess(t('conversations.createSuccess'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('conversations.createFailed')))
  } finally {
    creating.value = false
  }
}

async function sendReply(): Promise<void> {
  if (!selectedConversation.value || !replyContent.value.trim()) return
  sending.value = true
  try {
    const conversation = await conversationsAPI.addMessage(selectedConversation.value.id, {
      content: replyContent.value.trim()
    })
    replyContent.value = ''
    updateConversationInList(conversation)
    await loadMessages(conversation, { refreshUnread: false })
    await loadUnreadCount()
    appStore.showSuccess(t('conversations.sent'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('conversations.sendFailed')))
  } finally {
    sending.value = false
  }
}

async function markSelectedRead(): Promise<void> {
  if (!selectedConversation.value) return
  markingRead.value = true
  try {
    const lastVisibleMessageId = messages.value.at(-1)?.id
    const conversation = await conversationsAPI.markRead(selectedConversation.value.id, lastVisibleMessageId)
    updateConversationInList(conversation)
    await loadUnreadCount()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('conversations.markReadFailed')))
  } finally {
    markingRead.value = false
  }
}

async function closeSelectedConversation(): Promise<void> {
  if (!selectedConversation.value) return
  closing.value = true
  try {
    const conversation = await conversationsAPI.close(selectedConversation.value.id)
    updateConversationInList(conversation)
    appStore.showSuccess(t('conversations.closed'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('conversations.closeFailed')))
  } finally {
    closing.value = false
  }
}

onMounted(() => {
  void loadConversations()
  startLiveRefresh()
})

onBeforeUnmount(() => {
  stopLiveRefresh()
})
</script>
