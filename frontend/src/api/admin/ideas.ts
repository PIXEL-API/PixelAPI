import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'
import type { IdeaPost, IdeaTag, IdeaReport } from '@/types/ideas'

export async function adminListIdeas(params: {
  page?: number;
  page_size?: number;
  sort?: string;
  keyword?: string;
} = {}) {
  const { data } = await apiClient.get<PaginatedResponse<IdeaPost>>('/admin/ideas', { params })
  return data
}

export async function adminGetIdea(id: number) {
  const { data } = await apiClient.get<IdeaPost>(`/admin/ideas/${id}`)
  return data
}

export async function adminApproveIdea(id: number) {
  const { data } = await apiClient.post<IdeaPost>(`/admin/ideas/${id}/approve`)
  return data
}

export async function adminRejectIdea(id: number, reason: string) {
  const { data } = await apiClient.post<IdeaPost>(`/admin/ideas/${id}/reject`, { reason })
  return data
}

export async function adminHideIdea(id: number) {
  const { data } = await apiClient.post<IdeaPost>(`/admin/ideas/${id}/hide`)
  return data
}

export async function adminRestoreIdea(id: number) {
  const { data } = await apiClient.post<IdeaPost>(`/admin/ideas/${id}/restore`)
  return data
}

export async function adminRetryModeration(id: number) {
  const { data } = await apiClient.post<IdeaPost>(`/admin/ideas/${id}/retry-moderation`)
  return data
}

export async function adminListReports(params: { page?: number; page_size?: number; status?: string } = {}) {
  const { data } = await apiClient.get<PaginatedResponse<IdeaReport>>('/admin/ideas/reports', { params })
  return data
}

export async function adminResolveReport(id: number, resolution: string) {
  const { data } = await apiClient.post(`/admin/ideas/reports/${id}/resolve`, { resolution })
  return data
}

export async function adminListTags() {
  const { data } = await apiClient.get<IdeaTag[]>('/admin/ideas/tags')
  return data
}

export async function adminCreateTag(name: string) {
  const { data } = await apiClient.post<IdeaTag>('/admin/ideas/tags', { name })
  return data
}

export async function adminUpdateTag(id: number, payload: { name?: string; status?: string }) {
  const { data } = await apiClient.patch<IdeaTag>(`/admin/ideas/tags/${id}`, payload)
  return data
}

export async function adminMergeTags(sourceId: number, targetId: number) {
  const { data } = await apiClient.post(`/admin/ideas/tags/${sourceId}/merge`, { target_tag_id: targetId })
  return data
}
