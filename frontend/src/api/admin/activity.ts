import { apiClient } from '../client'
import type {
  ActivityCampaign,
  ActivityCampaignPage,
  ActivityCampaignPayload,
  ActivityCampaignStats,
  ActivityDrawResult,
  ActivityWinner,
  ActivityWinnerPage,
} from '@/types/activity'

export const adminActivityAPI = {
  async listCampaigns(params?: {
    page?: number
    page_size?: number
    status?: string
    keyword?: string
  }): Promise<ActivityCampaignPage> {
    const { data } = await apiClient.get<ActivityCampaignPage>('/admin/activities', { params })
    return data
  },

  async getCampaign(id: number): Promise<ActivityCampaign> {
    const { data } = await apiClient.get<ActivityCampaign>(`/admin/activities/${id}`)
    return data
  },

  async getCampaignStats(id: number): Promise<ActivityCampaignStats> {
    const { data } = await apiClient.get<ActivityCampaignStats>(`/admin/activities/${id}/stats`)
    return data
  },

  async createCampaign(payload: ActivityCampaignPayload): Promise<ActivityCampaign> {
    const { data } = await apiClient.post<ActivityCampaign>('/admin/activities', payload)
    return data
  },

  async updateCampaign(id: number, payload: ActivityCampaignPayload): Promise<ActivityCampaign> {
    const { data } = await apiClient.put<ActivityCampaign>(`/admin/activities/${id}`, payload)
    return data
  },

  async endCampaign(id: number): Promise<void> {
    await apiClient.delete(`/admin/activities/${id}`)
  },

  async runDraw(id: number): Promise<ActivityDrawResult> {
    const { data } = await apiClient.post<ActivityDrawResult>(`/admin/activities/${id}/draw`)
    return data
  },

  async listWinners(params?: {
    page?: number
    page_size?: number
    campaign_id?: number
  }): Promise<ActivityWinnerPage> {
    const { data } = await apiClient.get<ActivityWinnerPage>('/admin/activity-winners', { params })
    return data
  },

  async markWinnerDelivered(id: number, note = ''): Promise<ActivityWinner> {
    const { data } = await apiClient.post<ActivityWinner>(`/admin/activity-winners/${id}/deliver`, { note })
    return data
  },

  async rejectWinner(id: number, note = ''): Promise<ActivityWinner> {
    const { data } = await apiClient.post<ActivityWinner>(`/admin/activity-winners/${id}/reject`, { note })
    return data
  },
}

export default adminActivityAPI
