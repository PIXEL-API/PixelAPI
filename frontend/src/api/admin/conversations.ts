/**
 * Admin conversations API endpoints
 */

import { apiClient } from '../client'
import type {
  AddConversationMessageRequest,
  AdminConversationListFilters,
  BasePaginationResponse,
  Conversation,
  ConversationMessage,
  ConversationStatus,
  CreateAdminConversationRequest
} from '@/types'

function normalizeFilters(filters?: AdminConversationListFilters): Record<string, unknown> {
  return {
    ...filters,
    user_id: filters?.user_id || undefined,
    assigned_admin_id: filters?.assigned_admin_id || undefined,
    unread_only: filters?.unread_only ? 1 : undefined
  }
}

export async function list(
  page: number = 1,
  pageSize: number = 20,
  filters?: AdminConversationListFilters,
  options?: {
    signal?: AbortSignal
  }
): Promise<BasePaginationResponse<Conversation>> {
  const { data } = await apiClient.get<BasePaginationResponse<Conversation>>('/admin/conversations', {
    params: { page, page_size: pageSize, ...normalizeFilters(filters) },
    signal: options?.signal
  })
  return data
}

export async function create(request: CreateAdminConversationRequest): Promise<Conversation> {
  const { data } = await apiClient.post<Conversation>('/admin/conversations', request)
  return data
}

export async function getById(id: number): Promise<Conversation> {
  const { data } = await apiClient.get<Conversation>(`/admin/conversations/${id}`)
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
    `/admin/conversations/${id}/messages`,
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
  const { data } = await apiClient.post<Conversation>(`/admin/conversations/${id}/messages`, request)
  return data
}

export async function markRead(id: number, readUntilMessageId?: number): Promise<Conversation> {
  const { data } = await apiClient.post<Conversation>(
    `/admin/conversations/${id}/read`,
    readUntilMessageId ? { read_until_message_id: readUntilMessageId } : undefined
  )
  return data
}

export async function updateStatus(id: number, status: ConversationStatus): Promise<Conversation> {
  const { data } = await apiClient.put<Conversation>(`/admin/conversations/${id}/status`, { status })
  return data
}

export async function updateAssignee(
  id: number,
  assignedAdminId: number | null
): Promise<Conversation> {
  const { data } = await apiClient.put<Conversation>(`/admin/conversations/${id}/assignee`, {
    assigned_admin_id: assignedAdminId
  })
  return data
}

export async function unreadCount(): Promise<{ count: number }> {
  const { data } = await apiClient.get<{ count: number }>('/admin/conversations/unread-count')
  return data
}

const conversationsAPI = {
  list,
  create,
  getById,
  listMessages,
  addMessage,
  markRead,
  updateStatus,
  updateAssignee,
  unreadCount
}

export default conversationsAPI
