import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, post }
}))

import {
  getBillingIntentForAdmin,
  listBillingIntentsNeedingAttention,
  waiveBillingIntentForAdmin
} from '@/api/admin/accountShareBilling'

describe('account share billing admin API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    get.mockResolvedValue({ data: {} })
    post.mockResolvedValue({ data: {} })
  })

  it('reads the paginated attention list and a redacted detail with cancellation support', async () => {
    const controller = new AbortController()

    await listBillingIntentsNeedingAttention(2, 12, { signal: controller.signal })
    await getBillingIntentForAdmin(41, { signal: controller.signal })

    expect(get).toHaveBeenNthCalledWith(
      1,
      '/admin/account-share/billing-intents/needs-attention',
      {
        params: { page: 2, page_size: 12 },
        signal: controller.signal
      }
    )
    expect(get).toHaveBeenNthCalledWith(
      2,
      '/admin/account-share/billing-intents/41',
      { signal: controller.signal }
    )
  })

  it('submits an explicit waiver against the selected state token', async () => {
    const payload = {
      expected_state_token: 7,
      reason: '人工确认无法恢复',
      confirmed: true as const
    }

    await waiveBillingIntentForAdmin(41, payload)

    expect(post).toHaveBeenCalledWith(
      '/admin/account-share/billing-intents/41/waive',
      payload
    )
  })
})
