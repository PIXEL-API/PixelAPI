import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))

vi.mock('@/api/client', () => ({
  apiClient: { post }
}))

import { revertProxyFallback } from '@/api/admin/accounts'

describe('admin proxy lifecycle API', () => {
  beforeEach(() => {
    post.mockReset().mockResolvedValue({ data: { message: 'ok' } })
  })

  it('calls the account fallback revert endpoint without inventing a payload', async () => {
    await expect(revertProxyFallback(42)).resolves.toEqual({ message: 'ok' })
    expect(post).toHaveBeenCalledWith('/admin/accounts/42/revert-proxy-fallback')
  })
})
