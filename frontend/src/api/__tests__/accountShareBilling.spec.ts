import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({
  get: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: { get }
}))

import {
  getBillingIntentForAdmin,
  listBillingIntentsNeedingAttention
} from '@/api/admin/accountShareBilling'

describe('account share billing admin API', () => {
  beforeEach(() => {
    get.mockReset()
    get.mockResolvedValue({ data: {} })
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
})
