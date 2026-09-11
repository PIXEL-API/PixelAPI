import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { Account } from '@/types'

const { get, queryQuota } = vi.hoisted(() => ({ get: vi.fn(), queryQuota: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { get } }))
vi.mock('@/api/admin', () => ({ adminAPI: { cnProviders: { queryQuota } } }))

import CNProviderQuotaCell from '../CNProviderQuotaCell.vue'

function makeAccount(overrides: Partial<Account> = {}): Account {
  return {
    id: 1,
    name: 'Qwen Coding',
    platform: 'qwen',
    account_level: 'unknown',
    type: 'apikey',
    credentials: { account_mode: 'coding' },
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    status: 'active',
    error_message: null,
    error_since: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: true,
    created_at: '2026-09-11T00:00:00Z',
    updated_at: '2026-09-11T00:00:00Z',
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    ...overrides
  }
}

describe('CNProviderQuotaCell', () => {
  beforeEach(() => {
    get.mockReset()
    queryQuota.mockReset()
  })

  it('renders Qwen observations without inventing a percentage or invalid credential state', () => {
    const wrapper = mount(CNProviderQuotaCell, {
      props: {
        account: makeAccount({
          extra: {
            qwen_coding_5h_limit: {
              window: '5h',
              observed_at: '2026-09-11T01:00:00Z',
              retry_at: '2026-09-11T02:00:00Z'
            }
          }
        })
      }
    })

    expect(wrapper.text()).toContain('最近记录')
    expect(wrapper.text()).toContain('可重试')
    expect(wrapper.text()).toContain('刷新状态')
    expect(wrapper.get('a').attributes('href')).toBe('https://bailian.console.aliyun.com/cn-beijing/?tab=plan')
    expect(wrapper.text()).not.toMatch(/100%|凭证无效|耗尽/)
  })

  it('shows a refresh error while retaining the previous observation', async () => {
    get.mockRejectedValue(new Error('network unavailable'))
    const wrapper = mount(CNProviderQuotaCell, {
      props: {
        account: makeAccount({
          extra: {
            qwen_coding_5h_limit: { window: '5h', observed_at: '2026-09-11T01:00:00Z' }
          }
        }),
        scope: 'user'
      }
    })

    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('本次刷新失败')
    expect(wrapper.text()).toContain('最近记录')
  })

  it('keeps user quota probing on the account-scoped route', async () => {
    get.mockResolvedValue({
      data: {
        provider: 'qwen',
        success: true,
        credential_valid: null,
        source: 'upstream_response',
        automatic_query_supported: false,
        observed_limits: [],
        fetched_at: 1,
        persisted: false
      }
    })
    const wrapper = mount(CNProviderQuotaCell, {
      props: { account: makeAccount(), scope: 'user' }
    })

    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(get).toHaveBeenCalledWith('/accounts/1/cn-quota')
  })
})
