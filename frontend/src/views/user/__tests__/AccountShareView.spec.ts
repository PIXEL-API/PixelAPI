import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { AccountShareListing } from '@/api/accountShare'
import type { ApiKey } from '@/types'
import AccountShareView from '../AccountShareView.vue'

const {
  listListings,
  listMembershipQueue,
  listModeGroups,
  listProxies,
  listKeys,
  showSuccess,
  showWarning,
} = vi.hoisted(() => ({
  listListings: vi.fn(),
  listMembershipQueue: vi.fn(),
  listModeGroups: vi.fn(),
  listProxies: vi.fn(),
  listKeys: vi.fn(),
  showSuccess: vi.fn(),
  showWarning: vi.fn(),
}))

vi.mock('@/api/accountShare', () => ({
  accountShareAPI: {
    listListings,
    listMembershipQueue,
    listModeGroups,
    listProxies,
  },
}))

vi.mock('@/api', () => ({
  accountsAPI: {
    getById: vi.fn(),
    getStats: vi.fn(),
    recoverState: vi.fn(),
    refreshCredentials: vi.fn(),
  },
  adminAPI: {
    accounts: {
      test: vi.fn(),
    },
  },
  keysAPI: {
    list: listKeys,
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    cachedPublicSettings: undefined,
    showSuccess,
    showWarning,
  }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    isAdmin: false,
    user: { id: 9, balance: 100 },
  }),
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ query: {} }),
  useRouter: () => ({ push: vi.fn() }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard: vi.fn().mockResolvedValue(true),
  }),
}))

const AppLayoutStub = { template: '<main><slot /></main>' }

function listing(overrides: Partial<AccountShareListing> = {}): AccountShareListing {
  const now = '2026-07-11T01:00:00Z'
  return {
    id: 501,
    account_id: 601,
    platform: 'openai',
    owner_user_id: 700,
    owner_username: 'owner',
    account_name: '异步快照账号',
    status: 'active',
    seat_limit: 3,
    active_seats: 1,
    rating_count: 0,
    rating_score_sum: 0,
    rating_avg: 0,
    rate_multiplier: 1,
    allowed_models: ['gpt-5.5'],
    per_user_concurrency: 1,
    account_concurrency: 10,
    hourly_rate: 0.2,
    hourly_fee_waiver_minimum: 0,
    min_balance_required: 1,
    codex_cli_only: false,
    codex_5h_limit_percent: 100,
    codex_7d_limit_percent: 100,
    account_status: 'active',
    account_schedulable: true,
    editing_mine: false,
    created_at: now,
    updated_at: now,
    ...overrides,
  }
}

function apiKey(id: number, groupID: number, name: string): ApiKey {
  const now = '2026-07-11T01:00:00Z'
  return {
    id,
    user_id: 9,
    key: `sk-${id}`,
    name,
    group_id: groupID,
    status: 'active',
    ip_whitelist: [],
    ip_blacklist: [],
    last_used_at: null,
    quota: 0,
    quota_used: 0,
    expires_at: null,
    created_at: now,
    updated_at: now,
    rate_limit_5h: 0,
    rate_limit_1d: 0,
    rate_limit_7d: 0,
    usage_5h: 0,
    usage_1d: 0,
    usage_7d: 0,
    window_5h_start: null,
    window_1d_start: null,
    window_7d_start: null,
    reset_5h_at: null,
    reset_1d_at: null,
    reset_7d_at: null,
  }
}

function paginated(items: unknown[], page = 1, pages = 1) {
  return {
    items,
    total: items.length,
    page,
    page_size: 10,
    pages,
  }
}

function mountView() {
  return mount(AccountShareView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        BaseDialog: true,
        ConfirmDialog: true,
        Icon: true,
        AccountStatsModal: true,
        AccountTestModal: true,
        ModelWhitelistSelector: true,
        OAuthAuthorizationFlow: true,
        ProxySelector: true,
        ReAuthAccountModal: true,
        UsageProgressBar: true,
        Pagination: true,
        Teleport: true,
      },
    },
  })
}

describe('AccountShareView async snapshots and mode keys', () => {
  beforeEach(() => {
    localStorage.clear()
    listListings.mockReset()
    listMembershipQueue.mockReset()
    listModeGroups.mockReset()
    listProxies.mockReset()
    listKeys.mockReset()
    showSuccess.mockReset()
    showWarning.mockReset()

    listListings.mockResolvedValue(paginated([]))
    listModeGroups.mockResolvedValue([
      { group_id: 101, platform: 'openai' },
      { group_id: 202, platform: 'anthropic' },
    ])
    listProxies.mockResolvedValue([])
    listMembershipQueue.mockResolvedValue([])
    listKeys.mockResolvedValue(paginated([]))
  })

  it('renders the main listing before queue snapshots finish and enables reordering only after the snapshot arrives', async () => {
    let resolveQueue!: (memberships: unknown[]) => void
    const queuePromise = new Promise<unknown[]>(resolve => {
      resolveQueue = resolve
    })
    const queuedListing = listing({
      queue_membership_id: 902,
      queue_api_key_id: 77,
      queue_api_key_name: '预约 Key',
      queue_rank: 2,
      queue_status: 'queued',
      queue_idle_timeout_minutes: 30,
    })
    listListings.mockImplementation((_page: number, pageSize: number) => {
      return Promise.resolve(pageSize === 10 ? paginated([queuedListing]) : paginated([]))
    })
    listMembershipQueue.mockReturnValue(queuePromise)

    const wrapper = mountView()
    await flushPromises()
    await nextTick()

    expect(wrapper.text()).toContain('异步快照账号')
    expect(wrapper.text()).toContain('预约队列')
    expect(listMembershipQueue).toHaveBeenCalledWith(77, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    const moveUp = wrapper.findAll('button').find(button => button.text().includes('上移'))
    expect(moveUp).toBeDefined()
    expect(moveUp?.attributes('disabled')).toBeDefined()

    resolveQueue([
      { id: 901, queue_rank: 1 },
      { id: 902, queue_rank: 2 },
    ])
    await flushPromises()
    await nextTick()

    expect(moveUp?.attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('loads every reported API Key page before publishing usable mode keys', async () => {
    listKeys.mockImplementation((page: number, _pageSize: number, filters: { group_id: number }) => {
      if (filters.group_id === 101 && page === 1) {
        return Promise.resolve(paginated([apiKey(1001, 101, '第一页 Key')], 1, 2))
      }
      if (filters.group_id === 101 && page === 2) {
        return Promise.resolve(paginated([apiKey(1002, 101, '第二页 Key')], 2, 2))
      }
      return Promise.resolve(paginated([], page, 1))
    })

    const wrapper = mountView()
    await flushPromises()
    await nextTick()

    expect(listKeys).toHaveBeenCalledWith(1, 100, { group_id: 101, status: 'active' })
    expect(listKeys).toHaveBeenCalledWith(2, 100, { group_id: 101, status: 'active' })
    const setupState = (wrapper.vm as any).$?.setupState
    expect(setupState.modeApiKeysByPlatform.openai.map((key: ApiKey) => key.id)).toEqual([1001, 1002])
    wrapper.unmount()
  })
})
