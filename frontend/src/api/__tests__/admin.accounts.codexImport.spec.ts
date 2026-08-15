import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { post } }))

import { importCodexSession } from '@/api/admin/accounts'

describe('admin accounts Codex session import', () => {
  beforeEach(() => {
    post.mockReset()
  })

  it('uses the upstream-compatible endpoint and a 120 second timeout', async () => {
    const result = {
      total: 1,
      created: 1,
      updated: 0,
      skipped: 0,
      failed: 0,
      items: [{ index: 1, action: 'created' as const, account_id: 42 }],
      warnings: [],
      errors: []
    }
    post.mockResolvedValueOnce({ data: result })

    const payload = {
      content: '{"tokens":{"access_token":"header.payload.signature"}}',
      update_existing: true
    }

    await expect(importCodexSession(payload)).resolves.toEqual(result)
    expect(post).toHaveBeenCalledWith(
      '/admin/accounts/import/codex-session',
      payload,
      { timeout: 120000 }
    )
  })
})
