<template>
  <AppLayout>
    <div class="flex min-h-[calc(100vh-8rem)] flex-col gap-4 xl:h-[calc(100vh-8rem)] xl:min-h-0 xl:overflow-hidden">
      <section class="card p-4">
        <div class="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
          <div class="flex min-w-0 flex-1 flex-wrap items-center gap-3">
            <div class="relative w-full sm:w-72">
              <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
              <input
                v-model="searchQuery"
                type="search"
                class="input pl-9"
                :placeholder="t('admin.conversations.searchPlaceholder')"
                @input="handleSearchInput"
              />
            </div>
            <Select v-model="filters.status" :options="statusFilterOptions" class="w-full sm:w-40" @change="resetAndLoad" />
            <Select v-model="filters.type" :options="typeFilterOptions" class="w-full sm:w-40" @change="resetAndLoad" />
            <Select v-model="filters.priority" :options="priorityFilterOptions" class="w-full sm:w-40" @change="resetAndLoad" />
            <Select v-model="filters.unreadOnly" :options="unreadFilterOptions" class="w-full sm:w-36" @change="resetAndLoad" />
            <input
              v-model.number="filters.userId"
              type="number"
              min="1"
              class="input w-full sm:w-32"
              :placeholder="t('admin.conversations.userId')"
              @change="resetAndLoad"
            />
            <input
              v-model.number="filters.assignedAdminId"
              type="number"
              min="1"
              class="input w-full sm:w-36"
              :placeholder="t('admin.conversations.assignedAdminId')"
              @change="resetAndLoad"
            />
          </div>
          <div class="flex flex-wrap items-center justify-end gap-2">
            <span class="badge badge-primary">{{ t('admin.conversations.unreadCount', { count: unreadCount }) }}</span>
            <button type="button" class="btn btn-secondary px-3" :disabled="loading" :title="t('common.refresh')" @click="refreshAll">
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <button type="button" class="btn btn-primary" @click="openCreateDialog">
              <Icon name="plus" size="md" class="mr-1" />
              {{ t('admin.conversations.createConversation') }}
            </button>
          </div>
        </div>
      </section>

      <div class="grid min-h-0 flex-1 grid-cols-1 gap-4 xl:grid-cols-[minmax(19rem,23rem)_minmax(0,1fr)] 2xl:grid-cols-[minmax(20rem,24rem)_minmax(0,1fr)]">
        <section class="card flex min-h-[32rem] flex-col overflow-hidden xl:min-h-0">
          <div class="flex h-12 flex-shrink-0 items-center justify-between border-b border-gray-100 px-4 dark:border-dark-700">
            <div class="min-w-0">
              <div class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.conversations.title') }}</div>
              <div class="text-xs text-gray-500 dark:text-dark-400">{{ pagination.total }}</div>
            </div>
            <Icon name="chat" size="sm" class="text-gray-400 dark:text-dark-400" />
          </div>

          <div class="min-h-0 flex-1 overflow-y-auto">
            <button
              v-for="conversation in conversations"
              :key="conversation.id"
              type="button"
              class="group flex w-full gap-3 border-b border-gray-100 px-4 py-3 text-left transition-colors hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-700/60"
              :class="selectedConversation?.id === conversation.id ? 'bg-primary-50/80 dark:bg-primary-900/20' : 'bg-white/70 dark:bg-transparent'"
              @click="selectConversation(conversation)"
            >
              <span
                class="mt-1.5 h-2.5 w-2.5 flex-shrink-0 rounded-full"
                :class="conversation.admin_unread ? 'bg-amber-500' : 'bg-gray-300 dark:bg-dark-500'"
              ></span>
              <span class="min-w-0 flex-1">
                <span class="flex items-start justify-between gap-3">
                  <span class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ conversation.subject }}</span>
                  <span class="flex-shrink-0 text-xs text-gray-400 dark:text-dark-400">{{ formatRelativeTime(conversation.last_message_at) }}</span>
                </span>
                <span class="mt-1 block truncate text-xs text-gray-500 dark:text-dark-300">
                  {{ conversationUserLabel(conversation) }}
                </span>
                <span class="mt-2 flex flex-wrap items-center gap-1.5">
                    <span class="badge" :class="statusBadgeClass(conversation.status)">{{ statusLabel(conversation.status) }}</span>
                  <span class="badge" :class="priorityBadgeClass(conversation.priority)">{{ priorityLabel(conversation.priority) }}</span>
                  <span class="badge badge-gray">{{ typeLabel(conversation.type) }}</span>
                </span>
                <span class="mt-2 block truncate text-sm text-gray-500 dark:text-dark-300">
                  {{ conversation.last_message_excerpt || t('conversations.noMessages') }}
                </span>
              </span>
            </button>

            <div v-if="!loading && conversations.length === 0" class="px-6 py-12">
              <EmptyState
                :title="t('admin.conversations.empty')"
                :description="t('admin.conversations.emptyDescription')"
                :action-text="t('admin.conversations.createConversation')"
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
              <div class="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
                <div class="min-w-0">
                  <div class="flex flex-wrap items-center gap-2">
                    <h2 class="truncate text-lg font-semibold text-gray-900 dark:text-white">{{ selectedConversation.subject }}</h2>
                    <span v-if="selectedConversation.admin_unread" class="badge badge-warning">{{ t('conversations.unread') }}</span>
                  </div>
                  <div class="mt-2 flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-dark-300">
                    <span>#{{ selectedConversation.id }}</span>
                    <span>{{ conversationUserLabel(selectedConversation) }}</span>
                    <span>{{ t('conversations.createdAt', { time: formatDateTime(selectedConversation.created_at) }) }}</span>
                  </div>
                </div>
                <div class="grid w-full grid-cols-1 gap-2 sm:grid-cols-2 xl:w-auto xl:min-w-[28rem]">
                  <Select v-model="statusForm.status" :options="statusOptions" @change="updateSelectedStatus" />
                  <div class="flex gap-2">
                    <input
                      v-model.number="assigneeForm.assignedAdminId"
                      type="number"
                      min="1"
                      class="input"
                      :placeholder="t('admin.conversations.assignedAdminId')"
                    />
                    <button type="button" class="btn btn-secondary shrink-0" :disabled="assigning" @click="assignSelected">
                      {{ t('admin.conversations.assign') }}
                    </button>
                    <button type="button" class="btn btn-secondary shrink-0" :disabled="assigning" :title="t('admin.conversations.unassign')" @click="unassignSelected">
                      <Icon name="x" size="sm" />
                    </button>
                  </div>
                </div>
              </div>
              <div class="mt-3 flex flex-wrap gap-2 border-t border-gray-100 pt-3 dark:border-dark-700">
                <span class="badge" :class="statusBadgeClass(selectedConversation.status)">{{ statusLabel(selectedConversation.status) }}</span>
                <span class="badge" :class="priorityBadgeClass(selectedConversation.priority)">{{ priorityLabel(selectedConversation.priority) }}</span>
                <span class="badge badge-gray">{{ typeLabel(selectedConversation.type) }}</span>
                <span class="badge badge-gray">{{ t('admin.conversations.assigneeValue', { id: selectedConversation.assigned_admin_id || '-' }) }}</span>
              </div>
            </div>

            <div ref="messagePaneRef" class="min-h-[18rem] flex-1 space-y-3 overflow-y-auto bg-gray-50/70 p-4 dark:bg-dark-900/40" @scroll="handleMessagePaneScroll">
              <div v-if="messagesLoading" class="space-y-3">
                <div v-for="index in 4" :key="index" class="h-20 animate-pulse rounded-xl bg-white dark:bg-dark-700"></div>
              </div>
              <div v-if="!messagesLoading && (hasOlderMessages || loadingOlderMessages)" class="flex justify-center">
                <button type="button" class="btn btn-secondary btn-sm" :disabled="loadingOlderMessages" @click="loadOlderMessages">
                  <Icon name="arrowUp" size="sm" class="mr-1" />
                  {{ loadingOlderMessages ? t('common.loading') : t('conversations.loadEarlier') }}
                </button>
              </div>
              <div
                v-for="message in messages"
                :key="message.id"
                class="flex"
                :class="message.sender_type === 'admin' ? 'justify-end' : 'justify-start'"
              >
                <article
                  class="min-w-[14rem] max-w-[min(48rem,88%)] rounded-xl border px-4 py-3 shadow-sm"
                  :class="message.sender_type === 'admin'
                    ? 'border-primary-100 bg-primary-600 text-white dark:border-primary-800'
                    : message.sender_type === 'system'
                      ? 'border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-900/50 dark:bg-amber-900/20 dark:text-amber-100'
                      : 'border-gray-200 bg-white text-gray-800 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-100'"
                >
                  <div class="mb-1 flex items-center justify-between gap-3 text-xs" :class="message.sender_type === 'admin' ? 'text-primary-100' : 'text-gray-500 dark:text-dark-300'">
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
                :disabled="selectedConversation.status === 'closed' || sending"
                :placeholder="selectedConversation.status === 'closed' ? t('conversations.closedPlaceholder') : t('admin.conversations.replyPlaceholder')"
              ></textarea>
              <div class="mt-3 flex flex-wrap items-center justify-between gap-2">
                <button type="button" class="btn btn-secondary" :disabled="markingRead" @click="markSelectedRead">
                  <Icon name="checkCircle" size="sm" class="mr-1" />
                  {{ t('conversations.markRead') }}
                </button>
                <button type="submit" class="btn btn-primary" :disabled="!replyContent.trim() || selectedConversation.status === 'closed' || sending">
                  <Icon name="arrowRight" size="md" class="mr-1" />
                  {{ sending ? t('common.saving') : t('conversations.send') }}
                </button>
              </div>
            </form>
          </div>

          <div v-else class="flex min-h-[32rem] flex-1 items-center justify-center p-6">
            <EmptyState
              :title="t('admin.conversations.noSelection')"
              :description="t('admin.conversations.noSelectionDescription')"
              :action-text="t('admin.conversations.createConversation')"
              @action="openCreateDialog"
            />
          </div>
        </section>
      </div>
    </div>

    <BaseDialog
      :show="showCreateDialog"
      :title="t('admin.conversations.createConversation')"
      width="wide"
      @close="showCreateDialog = false"
    >
      <form id="admin-conversation-create-form" class="space-y-4" @submit.prevent="createConversation">
        <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <div>
            <label class="input-label">{{ t('admin.conversations.userId') }}</label>
            <input v-model.number="createForm.userId" type="number" min="1" class="input" required />
          </div>
          <div>
            <label class="input-label">{{ t('conversations.priority') }}</label>
            <Select v-model="createForm.priority" :options="priorityOptions" />
          </div>
        </div>
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
            <label class="input-label">{{ t('admin.conversations.source') }}</label>
            <input v-model.trim="createForm.source" type="text" class="input" />
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
          <button type="submit" form="admin-conversation-create-form" class="btn btn-primary" :disabled="creating">
            {{ creating ? t('common.saving') : t('common.create') }}
          </button>
        </div>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
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
const unreadCount = computed(() => conversationNotificationStore.adminUnreadCount)
const loading = ref(false)
const messagesLoading = ref(false)
const loadingOlderMessages = ref(false)
const hasOlderMessages = ref(false)
const sending = ref(false)
const creating = ref(false)
const assigning = ref(false)
const markingRead = ref(false)
const searchQuery = ref('')
const replyContent = ref('')
const showCreateDialog = ref(false)
const messagePaneRef = ref<HTMLElement | null>(null)
let searchTimer: ReturnType<typeof setTimeout> | undefined
let refreshTimer: ReturnType<typeof setInterval> | undefined
let conversationRequestSeq = 0
let messageRequestSeq = 0

const REFRESH_INTERVAL_MS = 5000
const MESSAGE_PAGE_SIZE = 50
const MESSAGE_BOTTOM_THRESHOLD_PX = 96
const MESSAGE_TOP_THRESHOLD_PX = 64

const pagination = reactive({
  page: 1,
  page_size: 20,
  total: 0,
  pages: 1
})

const filters = reactive<{
  status: ConversationStatus | ''
  type: ConversationType | ''
  priority: ConversationPriority | ''
  unreadOnly: boolean
  userId: number | null
  assignedAdminId: number | null
}>({
  status: '',
  type: '',
  priority: '',
  unreadOnly: false,
  userId: null,
  assignedAdminId: null
})

const statusForm = reactive<{ status: ConversationStatus }>({ status: 'open' })
const assigneeForm = reactive<{ assignedAdminId: number | null }>({ assignedAdminId: null })

const createForm = reactive<{
  userId: number | null
  subject: string
  content: string
  priority: ConversationPriority
  type: ConversationType
  source: string
}>({
  userId: null,
  subject: '',
  content: '',
  priority: 'normal',
  type: 'support',
  source: ''
})

const statusFilterOptions = computed<SelectOption[]>(() => [
  { value: '', label: t('conversations.allStatus') },
  ...statusOptions.value
])

const statusOptions = computed<SelectOption[]>(() => [
  { value: 'open', label: statusLabel('open') },
  { value: 'pending_user', label: statusLabel('pending_user') },
  { value: 'pending_admin', label: statusLabel('pending_admin') },
  { value: 'resolved', label: statusLabel('resolved') },
  { value: 'closed', label: statusLabel('closed') }
])

const priorityFilterOptions = computed<SelectOption[]>(() => [
  { value: '', label: t('admin.conversations.allPriority') },
  ...priorityOptions.value
])

const priorityOptions = computed<SelectOption[]>(() => [
  { value: 'low', label: priorityLabel('low') },
  { value: 'normal', label: priorityLabel('normal') },
  { value: 'high', label: priorityLabel('high') },
  { value: 'urgent', label: priorityLabel('urgent') }
])

const typeFilterOptions = computed<SelectOption[]>(() => [
  { value: '', label: t('admin.conversations.allType') },
  ...typeOptions.value
])

const typeOptions = computed<SelectOption[]>(() => [
  { value: 'support', label: typeLabel('support') },
  { value: 'notice', label: typeLabel('notice') },
  { value: 'billing', label: typeLabel('billing') },
  { value: 'subscription', label: typeLabel('subscription') },
  { value: 'account', label: typeLabel('account') },
  { value: 'security', label: typeLabel('security') }
])

const unreadFilterOptions = computed<SelectOption[]>(() => [
  { value: false, label: t('conversations.allMessages') },
  { value: true, label: t('conversations.unreadOnly') }
])

function statusLabel(status: ConversationStatus): string {
  return t(`admin.conversations.statusLabels.${status}`)
}

function priorityLabel(priority: ConversationPriority): string {
  return t(`conversations.priorityLabels.${priority}`)
}

function typeLabel(type: ConversationType): string {
  return t(`conversations.typeLabels.${type}`)
}

function senderLabel(sender: string): string {
  return t(`admin.conversations.senderLabels.${sender}`)
}

function conversationUserLabel(conversation: Conversation): string {
  const userId = t('admin.conversations.userIdValue', { id: conversation.user_id })
  const identity = conversation.user_email || conversation.user_name || ''
  return identity ? `${userId} · ${identity}` : userId
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

function syncSelectionForms(conversation: Conversation): void {
  statusForm.status = conversation.status
  assigneeForm.assignedAdminId = conversation.assigned_admin_id ?? null
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
    syncSelectionForms(next)
  }
}

async function loadConversations(options: { silent?: boolean } = {}): Promise<void> {
  const requestSeq = ++conversationRequestSeq
  if (!options.silent) loading.value = true
  try {
    const result = await adminAPI.conversations.list(pagination.page, pagination.page_size, {
      status: filters.status,
      type: filters.type,
      priority: filters.priority,
      search: searchQuery.value.trim(),
      unread_only: filters.unreadOnly,
      user_id: filters.userId || undefined,
      assigned_admin_id: filters.assignedAdminId || undefined,
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
      hasOlderMessages.value = false
    }
  } catch (error) {
    if (!options.silent) {
      appStore.showError(extractApiErrorMessage(error, t('admin.conversations.loadFailed')))
    } else {
      console.error('Failed to refresh admin conversations:', error)
    }
  } finally {
    if (!options.silent) loading.value = false
  }
}

async function loadUnreadCount(): Promise<void> {
  await conversationNotificationStore.fetchUnreadCount('admin')
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

function mergeMessages(current: ConversationMessage[], incoming: ConversationMessage[]): ConversationMessage[] {
  const byId = new Map<number, ConversationMessage>()
  for (const message of current) {
    byId.set(message.id, message)
  }
  for (const message of incoming) {
    byId.set(message.id, message)
  }
  return Array.from(byId.values()).sort((left, right) => left.id - right.id)
}

async function loadMessages(conversation: Conversation, options: { silent?: boolean, refreshUnread?: boolean, replace?: boolean } = {}): Promise<void> {
  const requestSeq = ++messageRequestSeq
  if (!options.silent) messagesLoading.value = true
  try {
    const result = await adminAPI.conversations.listMessages(conversation.id, 1, MESSAGE_PAGE_SIZE, { latest: true })
    if (requestSeq !== messageRequestSeq || selectedConversation.value?.id !== conversation.id) return
    const previousLastMessageId = messages.value.at(-1)?.id
    const latestMessageId = result.items.at(-1)?.id
    const shouldScrollToBottom = !options.silent || (
      latestMessageId !== previousLastMessageId && isMessagePaneNearBottom()
    )
    messages.value = options.replace ? result.items : mergeMessages(messages.value, result.items)
    hasOlderMessages.value = messages.value.length < result.total
    const readConversation = await adminAPI.conversations.markRead(conversation.id)
    if (requestSeq !== messageRequestSeq || selectedConversation.value?.id !== conversation.id) return
    updateConversationInList(readConversation)
    if (options.refreshUnread !== false) await loadUnreadCount()
    if (shouldScrollToBottom) {
      await nextTick()
      scrollMessagePaneToBottom()
    }
  } catch (error) {
    if (!options.silent) {
      appStore.showError(extractApiErrorMessage(error, t('admin.conversations.loadMessagesFailed')))
    } else {
      console.error('Failed to refresh admin conversation messages:', error)
    }
  } finally {
    if (!options.silent) messagesLoading.value = false
  }
}

async function loadOlderMessages(): Promise<void> {
  if (!selectedConversation.value || loadingOlderMessages.value || !hasOlderMessages.value) return
  const oldestMessageId = messages.value[0]?.id
  if (!oldestMessageId) return

  const conversationID = selectedConversation.value.id
  const pane = messagePaneRef.value
  const previousScrollHeight = pane?.scrollHeight ?? 0
  const previousScrollTop = pane?.scrollTop ?? 0
  loadingOlderMessages.value = true
  try {
    const result = await adminAPI.conversations.listMessages(conversationID, 1, MESSAGE_PAGE_SIZE, {
      beforeId: oldestMessageId
    })
    if (selectedConversation.value?.id !== conversationID) return
    messages.value = mergeMessages(messages.value, result.items)
    hasOlderMessages.value = result.total > result.items.length
    await nextTick()
    if (pane) {
      pane.scrollTop = pane.scrollHeight - previousScrollHeight + previousScrollTop
    }
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.conversations.loadMessagesFailed')))
  } finally {
    loadingOlderMessages.value = false
  }
}

function handleMessagePaneScroll(): void {
  const pane = messagePaneRef.value
  if (!pane || pane.scrollTop > MESSAGE_TOP_THRESHOLD_PX) return
  void loadOlderMessages()
}

async function selectConversation(conversation: Conversation): Promise<void> {
  selectedConversation.value = conversation
  syncSelectionForms(conversation)
  messages.value = []
  hasOlderMessages.value = false
  replyContent.value = ''
  await loadMessages(conversation, { replace: true })
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

async function refreshAll(): Promise<void> {
  await Promise.all([loadConversations(), loadUnreadCount()])
}

async function refreshLiveData(): Promise<void> {
  await Promise.all([
    loadConversations({ silent: true }),
    selectedConversation.value
      ? loadMessages(selectedConversation.value, { silent: true, refreshUnread: false })
      : Promise.resolve()
  ])
  await loadUnreadCount()
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

function openCreateDialog(): void {
  createForm.userId = null
  createForm.subject = ''
  createForm.content = ''
  createForm.priority = 'normal'
  createForm.type = 'support'
  createForm.source = ''
  showCreateDialog.value = true
}

async function createConversation(): Promise<void> {
  if (!createForm.userId || !createForm.subject.trim() || !createForm.content.trim()) return
  creating.value = true
  try {
    const conversation = await adminAPI.conversations.create({
      user_id: createForm.userId,
      subject: createForm.subject.trim(),
      content: createForm.content.trim(),
      kind: 'ticket',
      priority: createForm.priority,
      type: createForm.type,
      source: createForm.source.trim() || undefined,
      content_format: 'plain'
    })
    showCreateDialog.value = false
    updateConversationInList(conversation)
    selectedConversation.value = conversation
    syncSelectionForms(conversation)
    messages.value = []
    hasOlderMessages.value = false
    await loadMessages(conversation, { refreshUnread: false, replace: true })
    await loadUnreadCount()
    appStore.showSuccess(t('admin.conversations.createSuccess'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.conversations.createFailed')))
  } finally {
    creating.value = false
  }
}

async function sendReply(): Promise<void> {
  if (!selectedConversation.value || !replyContent.value.trim()) return
  sending.value = true
  try {
    const conversation = await adminAPI.conversations.addMessage(selectedConversation.value.id, {
      content: replyContent.value.trim()
    })
    replyContent.value = ''
    updateConversationInList(conversation)
    await Promise.all([
      loadMessages(conversation, { refreshUnread: false }),
      loadConversations({ silent: true })
    ])
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
    const conversation = await adminAPI.conversations.markRead(selectedConversation.value.id)
    updateConversationInList(conversation)
    await loadUnreadCount()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('conversations.markReadFailed')))
  } finally {
    markingRead.value = false
  }
}

async function updateSelectedStatus(): Promise<void> {
  if (
    !selectedConversation.value ||
    selectedConversation.value.status === statusForm.status
  ) return
  try {
    const conversation = await adminAPI.conversations.updateStatus(selectedConversation.value.id, statusForm.status)
    updateConversationInList(conversation)
    appStore.showSuccess(t('admin.conversations.statusUpdated'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.conversations.statusUpdateFailed')))
    if (selectedConversation.value) syncSelectionForms(selectedConversation.value)
  }
}

async function assignSelected(): Promise<void> {
  if (!selectedConversation.value || !assigneeForm.assignedAdminId) return
  assigning.value = true
  try {
    const conversation = await adminAPI.conversations.updateAssignee(
      selectedConversation.value.id,
      assigneeForm.assignedAdminId
    )
    updateConversationInList(conversation)
    appStore.showSuccess(t('admin.conversations.assigneeUpdated'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.conversations.assigneeUpdateFailed')))
  } finally {
    assigning.value = false
  }
}

async function unassignSelected(): Promise<void> {
  if (!selectedConversation.value) return
  assigning.value = true
  try {
    const conversation = await adminAPI.conversations.updateAssignee(selectedConversation.value.id, null)
    updateConversationInList(conversation)
    appStore.showSuccess(t('admin.conversations.assigneeUpdated'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.conversations.assigneeUpdateFailed')))
  } finally {
    assigning.value = false
  }
}

onMounted(() => {
  void refreshAll()
  conversationNotificationStore.startPolling('admin')
  startLiveRefresh()
})

onBeforeUnmount(() => {
  stopLiveRefresh()
})
</script>
