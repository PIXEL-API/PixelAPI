import { apiClient } from '@/api/client'

export async function getPlatformModels(platform: string): Promise<string[]> {
  const { data } = await apiClient.get<string[]>('/admin/models', {
    params: { platform }
  })
  return data
}

export const modelsAPI = {
  getPlatformModels
}

export default modelsAPI
