import { apiClient } from './client'
import type { ActivityCampaign, ActivityProgress, ActivityWinner, ActivityWinnerPublicPage } from '@/types/activity'

export const activityAPI = {
  async listWelfareActivities(): Promise<ActivityCampaign[]> {
    const { data } = await apiClient.get<ActivityCampaign[]>('/activities')
    return data || []
  },

  async listMyWinners(): Promise<ActivityWinner[]> {
    const { data } = await apiClient.get<ActivityWinner[]>('/activities/winners')
    return data || []
  },

  async listPublicWinners(id: number, page = 1, pageSize = 50): Promise<ActivityWinnerPublicPage> {
    const { data } = await apiClient.get<ActivityWinnerPublicPage>(`/activities/${id}/public-winners`, {
      params: { page, page_size: pageSize },
    })
    return data
  },

  async joinDraw(id: number): Promise<ActivityProgress> {
    const { data } = await apiClient.post<ActivityProgress>(`/activities/${id}/join`)
    return data
  },

  async submitWinnerClaim(id: number, claimInfo: Record<string, unknown>): Promise<ActivityWinner> {
    const { data } = await apiClient.post<ActivityWinner>(`/activities/winners/${id}/claim`, {
      claim_info: claimInfo,
    })
    return data
  },
}

export default activityAPI
