/**
 * User conversations API endpoints
 */

import { apiClient } from './client'
import type {
  AddConversationMessageRequest,
  BasePaginationResponse,
  Conversation,
  ConversationListFilters,
  ConversationMessage,
  CreateConversationRequest
} from '@/types'

function normalizeFilters(filters?: ConversationListFilters): Record<string, unknown> {
  return {
    ...filters,
    unread_only: filters?.unread_only ? 1 : undefined
  }
}

export async function list(
  page: number = 1,
  pageSize: number = 20,
  filters?: ConversationListFilters,
  options?: {
    signal?: AbortSignal
  }
): Promise<BasePaginationResponse<Conversation>> {
  const { data } = await apiClient.get<BasePaginationResponse<Conversation>>('/conversations', {
    params: { page, page_size: pageSize, ...normalizeFilters(filters) },
    signal: options?.signal
  })
  return data
}

export async function create(request: CreateConversationRequest): Promise<Conversation> {
  const { data } = await apiClient.post<Conversation>('/conversations', request)
  return data
}

export async function getById(id: number): Promise<Conversation> {
  const { data } = await apiClient.get<Conversation>(`/conversations/${id}`)
  return data
}

export async function listMessages(
  id: number,
  page: number = 1,
  pageSize: number = 100,
  options?: {
    signal?: AbortSignal
  }
): Promise<BasePaginationResponse<ConversationMessage>> {
  const { data } = await apiClient.get<BasePaginationResponse<ConversationMessage>>(
    `/conversations/${id}/messages`,
    {
      params: { page, page_size: pageSize },
      signal: options?.signal
    }
  )
  return data
}

export async function addMessage(
  id: number,
  request: AddConversationMessageRequest
): Promise<Conversation> {
  const { data } = await apiClient.post<Conversation>(`/conversations/${id}/messages`, request)
  return data
}

export async function markRead(id: number, readUntilMessageId?: number): Promise<Conversation> {
  const { data } = await apiClient.post<Conversation>(
    `/conversations/${id}/read`,
    readUntilMessageId ? { read_until_message_id: readUntilMessageId } : undefined
  )
  return data
}

export async function close(id: number): Promise<Conversation> {
  const { data } = await apiClient.post<Conversation>(`/conversations/${id}/close`)
  return data
}

export async function unreadCount(): Promise<{ count: number }> {
  const { data } = await apiClient.get<{ count: number }>('/conversations/unread-count')
  return data
}

const conversationsAPI = {
  list,
  create,
  getById,
  listMessages,
  addMessage,
  markRead,
  close,
  unreadCount
}

export default conversationsAPI
