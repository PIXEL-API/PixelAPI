import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post } = vi.hoisted(() => ({
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    post,
  },
}))

import { create } from '@/api/keys'

describe('api keys create api', () => {
  beforeEach(() => {
    post.mockReset().mockResolvedValue({ data: {} })
  })

  it('sends an exact expires_at without degrading it to whole days', async () => {
    const expiresAt = '2099-03-03T21:06:00.000Z'

    await create(
      'Precise expiration key',
      9,
      undefined,
      [],
      [],
      0,
      undefined,
      undefined,
      undefined,
      expiresAt
    )

    expect(post).toHaveBeenCalledWith('/keys', {
      name: 'Precise expiration key',
      group_id: 9,
      expires_at: expiresAt,
    })
  })

  it('keeps expires_in_days serialization for legacy callers', async () => {
    await create('Legacy expiration key', 9, undefined, [], [], 0, 30)

    expect(post).toHaveBeenCalledWith('/keys', {
      name: 'Legacy expiration key',
      group_id: 9,
      expires_in_days: 30,
    })
  })
})
