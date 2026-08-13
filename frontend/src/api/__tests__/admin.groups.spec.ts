import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({
  get: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: { get }
}))

import { getUsageSummary } from '@/api/admin/groups'

describe('admin groups usage summary API', () => {
  beforeEach(() => {
    get.mockReset()
  })

  it('requests only the visible group IDs with the browser timezone', async () => {
    const summaries = [
      { group_id: 17, today_cost: 1.25, total_cost: 8.75 }
    ]
    get.mockResolvedValue({ data: summaries })

    await expect(
      getUsageSummary([17, 23], 'Asia/Shanghai')
    ).resolves.toEqual(summaries)

    expect(get).toHaveBeenCalledWith('/admin/groups/usage-summary', {
      params: {
        timezone: 'Asia/Shanghai',
        group_ids: '17,23'
      }
    })
  })

  it('omits optional query parameters when callers provide no filters', async () => {
    get.mockResolvedValue({ data: [] })

    await expect(getUsageSummary()).resolves.toEqual([])

    expect(get).toHaveBeenCalledWith('/admin/groups/usage-summary', {
      params: {}
    })
  })
})
