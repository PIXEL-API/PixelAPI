import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))

vi.mock('@/api/client', () => ({
  apiClient: { get },
}))

import { listMyIdeas } from '@/api/ideas'

describe('ideas api', () => {
  beforeEach(() => {
    get.mockReset().mockResolvedValue({ data: { items: [], total: 0 } })
  })

  it('passes the typed status filter to the mine endpoint', async () => {
    await listMyIdeas({ page: 2, page_size: 20, status: 'pending_revision' })

    expect(get).toHaveBeenCalledWith('/ideas/mine', {
      params: { page: 2, page_size: 20, status: 'pending_revision' },
    })
  })
})
