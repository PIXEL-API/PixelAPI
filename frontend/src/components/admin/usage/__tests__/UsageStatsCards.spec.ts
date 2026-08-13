import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import UsageStatsCards from '../UsageStatsCards.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

describe('UsageStatsCards cache hit rate', () => {
  it('includes cache creation tokens in the shared denominator', () => {
    const wrapper = mount(UsageStatsCards, {
      props: {
        stats: {
          total_requests: 1,
          total_input_tokens: 200,
          total_output_tokens: 100,
          total_cache_tokens: 800,
          total_cache_creation_tokens: 300,
          total_cache_read_tokens: 500,
          total_tokens: 1100,
          total_cost: 0,
          total_actual_cost: 0,
          total_request_actual_cost: 0,
          total_hourly_cost: 0,
          total_account_cost: 0,
          average_duration_ms: 0,
        },
      },
      global: { stubs: { Icon: true } },
    })

    expect(wrapper.text()).toContain('usage.cacheHitRate: 50.0%')
  })
})
