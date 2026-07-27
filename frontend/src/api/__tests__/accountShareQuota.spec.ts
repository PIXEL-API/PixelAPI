import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, put, post } = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn(),
  post: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, put, post }
}))

import {
  batchGrandfatherAccountShareQuotas,
  getGlobalAccountShareQuota,
  getOwnerAccountShareQuota,
  grandfatherOwnerAccountShareQuota,
  listAccountShareGrandfatherCandidates,
  listAccountShareQuotaAudit,
  revokeOwnerAccountShareQuota,
  updateGlobalAccountShareQuota,
  upsertOwnerAccountShareQuota
} from '@/api/admin/accountShareQuota'

describe('account share quota admin API', () => {
  beforeEach(() => {
    get.mockReset()
    put.mockReset()
    post.mockReset()
    get.mockResolvedValue({ data: {} })
    put.mockResolvedValue({ data: {} })
    post.mockResolvedValue({ data: {} })
  })

  it('reads global, owner, and owner-scoped audit state with cancellation support', async () => {
    const controller = new AbortController()

    await getGlobalAccountShareQuota({ signal: controller.signal })
    await getOwnerAccountShareQuota(77, { signal: controller.signal })
    await listAccountShareQuotaAudit('owner', 2, 12, 77, { signal: controller.signal })

    expect(get).toHaveBeenNthCalledWith(
      1,
      '/admin/account-share/quotas/global',
      { signal: controller.signal }
    )
    expect(get).toHaveBeenNthCalledWith(
      2,
      '/admin/account-share/quotas/owners/77',
      { signal: controller.signal }
    )
    expect(get).toHaveBeenNthCalledWith(
      3,
      '/admin/account-share/quotas/audit',
      {
        params: {
          scope_type: 'owner',
          page: 2,
          page_size: 12,
          owner_id: 77
        },
        signal: controller.signal
      }
    )
  })

  it('sends every quota mutation with the caller supplied idempotency key', async () => {
    const limits = {
      max_live_rooms: 5,
      max_room_creates_24_hours: 8,
      max_accounts_per_room: 20,
      max_room_accounts_per_owner: 100
    }
    const base = {
      expected_version: 3,
      reason: '容量调整',
      confirmed: true as const
    }
    const expiresAt = '2026-08-31T00:00:00.000Z'

    await updateGlobalAccountShareQuota({ ...base, limits }, 'global-key')
    await upsertOwnerAccountShareQuota(77, { ...base, limits, expires_at: expiresAt }, 'owner-key')
    await grandfatherOwnerAccountShareQuota(77, { ...base, expires_at: expiresAt }, 'grandfather-key')
    await revokeOwnerAccountShareQuota(77, base, 'revoke-key')

    expect(put).toHaveBeenNthCalledWith(
      1,
      '/admin/account-share/quotas/global',
      { ...base, limits },
      { headers: { 'Idempotency-Key': 'global-key' } }
    )
    expect(put).toHaveBeenNthCalledWith(
      2,
      '/admin/account-share/quotas/owners/77',
      { ...base, limits, expires_at: expiresAt },
      { headers: { 'Idempotency-Key': 'owner-key' } }
    )
    expect(post).toHaveBeenNthCalledWith(
      1,
      '/admin/account-share/quotas/owners/77/grandfather',
      { ...base, expires_at: expiresAt },
      { headers: { 'Idempotency-Key': 'grandfather-key' } }
    )
    expect(post).toHaveBeenNthCalledWith(
      2,
      '/admin/account-share/quotas/owners/77/revoke',
      base,
      { headers: { 'Idempotency-Key': 'revoke-key' } }
    )
  })

  it('lists server-generated grandfather candidates and submits their untouched preview contract', async () => {
    const controller = new AbortController()
    const items = [{
      owner_user_id: 77,
      expected_version: 3,
      preview_usage: {
        live_rooms: 6,
        room_creates_24_hours: 5,
        owner_room_accounts: 100,
        largest_room_accounts: 20
      },
      preview_fingerprint: 'candidate-77'
    }]
    const payload = {
      items,
      expires_at: '2026-08-31T00:00:00.000Z',
      reason: '历史超限冻结',
      confirmed: true as const
    }
    const results = [{
      owner_user_id: 77,
      status: 'applied' as const,
      policy_id: 8,
      policy_version: 4,
      expires_at: payload.expires_at
    }]
    post.mockResolvedValueOnce({ data: results })

    await listAccountShareGrandfatherCandidates(3, 12, { signal: controller.signal })
    await expect(batchGrandfatherAccountShareQuotas(payload, 'batch-key')).resolves.toEqual(results)

    expect(get).toHaveBeenCalledWith(
      '/admin/account-share/quotas/grandfather-candidates',
      {
        params: {
          page: 3,
          page_size: 12
        },
        signal: controller.signal
      }
    )
    expect(post).toHaveBeenCalledWith(
      '/admin/account-share/quotas/grandfather/batch',
      payload,
      { headers: { 'Idempotency-Key': 'batch-key' } }
    )
  })
})
