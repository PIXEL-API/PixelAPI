import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { get } }))

import { queryUserBalance, queryUserQuota } from '@/api/admin/cnProviders'

describe('CN provider user quota endpoints', () => {
  beforeEach(() => get.mockReset())

  it('uses the account-scoped quota route', async () => {
    const result = { provider: 'qwen', success: true, credential_valid: null, fetched_at: 1, persisted: false, observed_limits: [] }
    get.mockResolvedValue({ data: result })

    await expect(queryUserQuota(42)).resolves.toEqual(result)
    expect(get).toHaveBeenCalledWith('/accounts/42/cn-quota')
  })

  it('uses the account-scoped balance route', async () => {
    const result = { provider: 'kimi', success: true, balance: 3, available: true, fetched_at: 1, persisted: false }
    get.mockResolvedValue({ data: result })

    await expect(queryUserBalance(42)).resolves.toEqual(result)
    expect(get).toHaveBeenCalledWith('/accounts/42/cn-balance')
  })
})
