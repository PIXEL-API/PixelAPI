import { apiClient } from './client'
import type { PaginatedResponse } from '@/types'
import type {
  IdeaPost,
  IdeaTag,
  IdeaReward,
  IdeaAsset,
} from '@/types/ideas'

export interface IdeaListParams {
  page?: number;
  page_size?: number;
  sort?: string;
  tag?: string;
  keyword?: string;
}

export interface IdeaPostPayload {
  title: string;
  summary: string;
  body: string;
  tags: string[];
}

export async function listIdeas(params: IdeaListParams = {}) {
  const { data } = await apiClient.get<PaginatedResponse<IdeaPost>>('/ideas', { params })
  return data
}

export async function listMyIdeas(params: { page?: number; page_size?: number; sort?: string } = {}) {
  const { data } = await apiClient.get<PaginatedResponse<IdeaPost>>('/ideas/mine', { params })
  return data
}

export async function getIdea(id: number) {
  const { data } = await apiClient.get<IdeaPost>(`/ideas/${id}`)
  return data
}

export async function createIdea(payload: IdeaPostPayload) {
  const { data } = await apiClient.post<IdeaPost>('/ideas', payload)
  return data
}

export async function updateIdea(id: number, payload: IdeaPostPayload) {
  const { data } = await apiClient.patch<IdeaPost>(`/ideas/${id}`, payload)
  return data
}

export async function publishIdea(id: number) {
  const { data } = await apiClient.post<IdeaPost>(`/ideas/${id}/publish`)
  return data
}

export async function deleteIdea(id: number) {
  const { data } = await apiClient.delete(`/ideas/${id}`)
  return data
}

export async function listIdeaTags() {
  const { data } = await apiClient.get<IdeaTag[]>('/ideas/tags')
  return data
}

export async function likeIdea(id: number) {
  const { data } = await apiClient.post<{ count: number }>(`/ideas/${id}/like`)
  return data
}

export async function unlikeIdea(id: number) {
  const { data } = await apiClient.delete<{ count: number }>(`/ideas/${id}/like`)
  return data
}

export async function favoriteIdea(id: number) {
  const { data } = await apiClient.post<{ count: number }>(`/ideas/${id}/favorite`)
  return data
}

export async function unfavoriteIdea(id: number) {
  const { data } = await apiClient.delete<{ count: number }>(`/ideas/${id}/favorite`)
  return data
}

export async function recordIdeaView(id: number) {
  await apiClient.post(`/ideas/${id}/view`)
}

export async function rewardIdea(
  id: number,
  payload: { asset_type: 'balance' | 'points'; amount: number },
  idempotencyKey: string,
) {
  const { data } = await apiClient.post<IdeaReward>(`/ideas/${id}/rewards`, payload, {
    headers: { 'Idempotency-Key': idempotencyKey },
  })
  return data
}

export async function reportIdea(id: number, payload: { reason: string; detail: string }) {
  const { data } = await apiClient.post(`/ideas/${id}/reports`, payload)
  return data
}

export async function listIdeaAssets(id: number) {
  const { data } = await apiClient.get<IdeaAsset[]>(`/ideas/${id}/assets`)
  return data
}

export async function uploadIdeaAsset(id: number, file: File) {
  const form = new FormData()
  form.append('file', file)
  const { data } = await apiClient.post<IdeaAsset>(`/ideas/${id}/assets`, form, {
    headers: { 'Content-Type': 'multipart/form-data' },
  })
  return data
}

export async function getIdeaAssetURL(id: number, assetId: number) {
  const { data } = await apiClient.get<{ url: string }>(`/ideas/${id}/assets/${assetId}/url`)
  return data.url
}
