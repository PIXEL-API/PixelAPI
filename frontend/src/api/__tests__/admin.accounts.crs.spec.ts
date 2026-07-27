import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { post } }))

import { syncFromCrs } from '@/api/admin/accounts'

describe('admin CRS sync API', () => {
  beforeEach(() => {
    post.mockReset()
    post.mockResolvedValue({
      data: {
        created: 0,
        updated: 1,
        skipped: 0,
        failed: 0,
        items: [],
      },
    })
  })

  it('forwards the signed preview snapshot, force guard snapshot and idempotency key', async () => {
    const request = {
      base_url: 'https://crs.example.com',
      username: 'admin',
      password: 'secret',
      preview_token: 'signed-preview-token',
      force_active_edit: true,
      confirmed: true,
      reason: 'rotate credentials',
      expected_versions: { 71: 6 },
    }

    await syncFromCrs(request, 'crs-sync-operation')

    expect(post).toHaveBeenCalledWith('/admin/accounts/sync/crs', request, {
      headers: {
        'Idempotency-Key': 'crs-sync-operation',
      },
    })
  })
})
