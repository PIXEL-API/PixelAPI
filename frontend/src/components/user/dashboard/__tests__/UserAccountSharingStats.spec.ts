import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import type { AccountSharingDashboardStats } from '@/api/usage'
import UserAccountSharingStats from '@/components/user/dashboard/UserAccountSharingStats.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

function buildStats(accountCount: number): AccountSharingDashboardStats {
  return {
    summary: {
      owned_accounts: accountCount,
      private_accounts: 0,
      public_pending_accounts: 0,
      public_approved_accounts: accountCount,
      public_suspended_accounts: 0,
      self_requests: 0,
      self_tokens: 0,
      self_actual_cost: 0,
      self_account_cost: 0,
      external_requests: 0,
      external_consumer_charge: 0,
      external_account_cost: 0,
      external_owner_credit: 0,
      external_platform_fee: 0,
      total_account_cost: 0,
      balance_net_change: 0
    },
    accounts: Array.from({ length: accountCount }, (_, index) => ({
      account_id: index + 1,
      name: `account-${index + 1}`,
      platform: 'openai',
      share_mode: 'public',
      share_status: 'approved',
      self_requests: 0,
      self_tokens: 0,
      self_actual_cost: 0,
      self_account_cost: 0,
      external_requests: 0,
      external_consumer_charge: 0,
      external_account_cost: 0,
      external_owner_credit: 0,
      external_platform_fee: 0
    })),
    trend: [],
    start_date: '2026-07-15',
    end_date: '2026-07-21',
    granularity: 'day'
  }
}

describe('UserAccountSharingStats', () => {
  it('paginates owned accounts instead of discarding entries after the first ten', async () => {
    const stats = buildStats(11)
    const wrapper = mount(UserAccountSharingStats, {
      props: {
        stats,
        loading: false
      },
      global: {
        stubs: {
          Icon: true,
          LoadingSpinner: true
        }
      }
    })

    expect(wrapper.text()).toContain('account-1')
    expect(wrapper.text()).toContain('account-10')
    expect(wrapper.text()).not.toContain('account-11')
    expect(wrapper.findAll('tbody tr')).toHaveLength(10)

    await wrapper.get('button[aria-label="pagination.next"]').trigger('click')

    expect(wrapper.text()).toContain('account-11')
    expect(wrapper.text()).not.toContain('account-10')
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)

    await wrapper.setProps({ stats: buildStats(5) })
    await nextTick()

    expect(wrapper.text()).toContain('account-1')
    expect(wrapper.text()).not.toContain('dashboard.noOwnedAccountStats')
    expect(wrapper.find('button[aria-label="pagination.next"]').exists()).toBe(false)
  })
})
