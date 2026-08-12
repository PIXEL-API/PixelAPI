import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, del } = vi.hoisted(() => ({
  get: vi.fn(),
  del: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    delete: del,
  },
}))

import {
  clearCyberPolicyRestriction,
  getCyberPolicyRestriction,
  type ClearCyberPolicyRestrictionResponse,
  type CyberPolicyRestriction,
} from '@/api/admin/riskControl'

type Assert<T extends true> = T
type IsExact<T, U> = (
  (<G>() => G extends T ? 1 : 2) extends (<G>() => G extends U ? 1 : 2)
    ? ((<G>() => G extends U ? 1 : 2) extends (<G>() => G extends T ? 1 : 2) ? true : false)
    : false
)

type ExpectedRestriction = {
  user_id: number
  group_id: number
  blocked: boolean
  scope: 'user_group_day' | ''
  blocked_until: string | null
  retry_after_seconds: number
}

type ExpectedClearResponse = {
  user_id: number
  group_id: number
  removed: boolean
}

const restrictionContractExact: Assert<IsExact<CyberPolicyRestriction, ExpectedRestriction>> = true
const clearContractExact: Assert<IsExact<ClearCyberPolicyRestrictionResponse, ExpectedClearResponse>> = true

describe('admin risk-control Cyber restriction API', () => {
  beforeEach(() => {
    get.mockReset()
    del.mockReset()
  })

  it('queries the exact user and routed group restriction', async () => {
    const response: CyberPolicyRestriction = {
      user_id: 445,
      group_id: 1198,
      blocked: true,
      scope: 'user_group_day',
      blocked_until: '2026-08-12T00:00:00+08:00',
      retry_after_seconds: 3600,
    }
    get.mockResolvedValue({ data: response })

    await expect(getCyberPolicyRestriction(445, 1198)).resolves.toEqual(response)
    expect(get).toHaveBeenCalledWith('/admin/risk-control/cyber-restrictions/users/445/groups/1198')
  })

  it('clears only the Cyber restriction and never calls the account-unban endpoint', async () => {
    const response: ClearCyberPolicyRestrictionResponse = {
      user_id: 445,
      group_id: 1198,
      removed: true,
    }
    del.mockResolvedValue({ data: response })

    await expect(clearCyberPolicyRestriction(445, 1198)).resolves.toEqual(response)
    expect(del).toHaveBeenCalledWith('/admin/risk-control/cyber-restrictions/users/445/groups/1198')
    expect(del.mock.calls.flat().join(' ')).not.toContain('/users/445/unban')
  })

  it('keeps request and response contracts aligned with the backend', () => {
    expect(restrictionContractExact).toBe(true)
    expect(clearContractExact).toBe(true)
  })
})
