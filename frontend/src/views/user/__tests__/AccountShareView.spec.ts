import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { AccountShareListing } from '@/api/accountShare'
import type { Account, ApiKey } from '@/types'
import AccountShareView from '../AccountShareView.vue'

const {
  listListings,
  listMembershipQueue,
  listModeGroups,
  listProxies,
  createRoom,
  listAccounts,
  listKeys,
  fetchPublicSettings,
  publicSettings,
  showSuccess,
  showWarning,
} = vi.hoisted(() => ({
  listListings: vi.fn(),
  listMembershipQueue: vi.fn(),
  listModeGroups: vi.fn(),
  listProxies: vi.fn(),
  createRoom: vi.fn(),
  listAccounts: vi.fn(),
  listKeys: vi.fn(),
  fetchPublicSettings: vi.fn(),
  publicSettings: {
    user_private_group_commission_rate: 0.0075,
  },
  showSuccess: vi.fn(),
  showWarning: vi.fn(),
}))

vi.mock('@/api/accountShare', () => ({
  accountShareAPI: {
    listListings,
    listMembershipQueue,
    listModeGroups,
    listProxies,
    createRoom,
  },
}))

vi.mock('@/api', () => ({
  accountsAPI: {
    list: listAccounts,
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
    cachedPublicSettings: publicSettings,
    fetchPublicSettings,
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
const BaseDialogWithSlotsStub = {
  props: ['show'],
  template: '<section v-if="show"><slot /><slot name="footer" /></section>',
}

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

function account(overrides: Partial<Account> = {}): Account {
  const now = '2026-07-11T01:00:00Z'
  return {
    id: 801,
    name: '自有账号',
    platform: 'openai',
    account_level: 'plus',
    type: 'oauth',
    proxy_id: 11,
    concurrency: 10,
    priority: 50,
    status: 'active',
    error_message: null,
    error_since: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: false,
    created_at: now,
    updated_at: now,
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    ...overrides,
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

function mountView(options: { renderDialogs?: boolean } = {}) {
  return mount(AccountShareView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        BaseDialog: options.renderDialogs ? BaseDialogWithSlotsStub : true,
        ConfirmDialog: true,
        Icon: true,
        AccountStatsModal: true,
        AccountTestModal: true,
        ModelWhitelistSelector: true,
        OAuthAuthorizationFlow: true,
        ProxySelector: true,
        ReAuthAccountModal: true,
        RoomAccountsDialog: {
          props: ['show', 'listing'],
          template: '<div v-if="show" data-testid="room-accounts-dialog">{{ listing?.room_name }}</div>',
        },
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
    createRoom.mockReset()
    listAccounts.mockReset()
    listKeys.mockReset()
    fetchPublicSettings.mockReset()
    showSuccess.mockReset()
    showWarning.mockReset()
    publicSettings.user_private_group_commission_rate = 0.0075

    listListings.mockResolvedValue(paginated([]))
    listModeGroups.mockResolvedValue([
      { group_id: 101, platform: 'openai' },
      { group_id: 202, platform: 'anthropic' },
    ])
    listProxies.mockResolvedValue([])
    listAccounts.mockResolvedValue(paginated([]))
    createRoom.mockResolvedValue(listing({ owner_user_id: 9, room_name: '新房间' }))
    listMembershipQueue.mockResolvedValue([])
    listKeys.mockResolvedValue(paginated([]))
    fetchPublicSettings.mockResolvedValue(publicSettings)
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

  it('uses the public self-use rate and the recommendation effective rate instead of a hardcoded multiplier', async () => {
    const ownListing = listing({ owner_user_id: 9 })
    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()

    const setupState = (wrapper.vm as any).$?.setupState
    setupState.pendingJoinConfirmation = {
      listing: ownListing,
      apiKeyID: 1001,
      idleTimeoutMinutes: 10,
    }
    await nextTick()

    expect(wrapper.text()).toContain('全局自用倍率 0.0075x')
    expect(wrapper.text()).not.toContain('0.005x')

    const summary = setupState.recommendationOwnerSelfUseSummary({
      listing: ownListing,
      estimate: {
        effective_rate_multiplier: 0.0085,
      },
    })
    expect(summary).toContain('0.0085x')
    wrapper.unmount()
  })

  it('defaults to existing accounts and filters out incompatible room members', async () => {
    listAccounts.mockResolvedValue(paginated([
      account({ id: 1, name: '可用私有账号', external_placement: { target: 'private', state: 'active', version: 1 } }),
      account({ id: 2, name: '公共号池账号', external_placement: { target: 'public_pool', state: 'active', version: 2 } }),
      account({ id: 3, name: '其他房间账号', external_placement: { target: 'room', room_id: 99, state: 'active', version: 3 } }),
      account({ id: 4, name: '未知等级账号', account_level: 'unknown' }),
      account({ id: 5, name: '不可调度账号', schedulable: false }),
      account({ id: 6, name: '其他平台账号', platform: 'anthropic' }),
    ]))

    const wrapper = mountView()
    await flushPromises()
    const createButton = wrapper.findAll('button').find(button => button.text().includes('创建房间'))
    await createButton?.trigger('click')
    await flushPromises()

    const setupState = (wrapper.vm as any).$?.setupState
    expect(setupState.createSourceMode).toBe('existing')
    expect(setupState.eligibleOwnedAccounts.map((item: Account) => item.id)).toEqual([2, 1])
    expect(wrapper.text()).toContain('公共号池账号')
    expect(wrapper.text()).toContain('可用私有账号')
    expect(wrapper.text()).not.toContain('其他房间账号')
    expect(wrapper.text()).not.toContain('未知等级账号')
    wrapper.unmount()
  })

  it('creates a room from a public-pool account without OAuth fields and reuses the idempotency key after failure', async () => {
    const publicAccount = account({
      id: 22,
      name: '公共号池主账号',
      external_placement: { target: 'public_pool', state: 'active', version: 4 },
    })
    listAccounts.mockResolvedValue(paginated([publicAccount]))
    createRoom
      .mockRejectedValueOnce(new Error('temporary failure'))
      .mockResolvedValueOnce(listing({
        id: 700,
        account_id: 22,
        owner_user_id: 9,
        room_name: 'OpenAI账号房间',
        account_count: 1,
        healthy_account_count: 1,
      }))

    const wrapper = mountView()
    await flushPromises()
    const createPanelButton = wrapper.findAll('button').find(button => button.text().includes('创建房间'))
    await createPanelButton?.trigger('click')
    await flushPromises()

    const submitButton = () => wrapper.findAll('button').find(button =>
      button.text().includes('使用已有账号创建房间')
    )
    await submitButton()?.trigger('click')
    await flushPromises()
    await submitButton()?.trigger('click')
    await flushPromises()

    expect(createRoom).toHaveBeenCalledTimes(2)
    const firstPayload = createRoom.mock.calls[0]?.[0]
    const secondPayload = createRoom.mock.calls[1]?.[0]
    expect(firstPayload).toMatchObject({
      account_id: 22,
      room_name: 'OpenAI账号房间',
      seat_limit: 2,
      per_user_concurrency: 5,
    })
    expect(firstPayload.idempotency_key).toMatch(/^account-share-room-22-/)
    expect(secondPayload.idempotency_key).toBe(firstPayload.idempotency_key)
    expect(firstPayload).not.toHaveProperty('proxy_id')
    expect(firstPayload).not.toHaveProperty('session_id')
    expect(firstPayload).not.toHaveProperty('code')
    expect(firstPayload).not.toHaveProperty('credentials')
    wrapper.unmount()
  })

  it('renders room metadata and opens the room member dialog for the owner', async () => {
    const ownRoom = listing({
      id: 900,
      owner_user_id: 9,
      room_name: '我的多账号房间',
      account_count: 3,
      healthy_account_count: 2,
    })
    listListings.mockImplementation((_page: number, pageSize: number) =>
      Promise.resolve(pageSize === 10 ? paginated([ownRoom]) : paginated([ownRoom]))
    )

    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('我的多账号房间')
    expect(wrapper.text()).toContain('健康账号 2/3')

    const roomCountButton = wrapper.findAll('button').find(button => button.text().includes('健康账号 2/3'))
    await roomCountButton?.trigger('click')
    await nextTick()

    expect(wrapper.get('[data-testid="room-accounts-dialog"]').text()).toContain('我的多账号房间')
    wrapper.unmount()
  })
})
