import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type {
  AccountShareJoinIntent,
  AccountShareListing,
  AccountShareMembership,
  AccountShareMembershipHistoryEntry,
  AccountShareRoomBlockers,
  AccountShareRoomManagementState,
  AccountShareRoomOperation
} from '@/api/accountShare'
import type { Account, ApiKey } from '@/types'
import AccountShareView from '../AccountShareView.vue'

const {
  listListings,
  listMembershipHistory,
  getMySpendSummary,
  getAPIKeyBindingStatus,
  getListing,
  listModeGroups,
  getCapabilities,
  getRoomManagementState,
  drainRoom,
  activateRoom,
  suspendRoom,
  createRoomDeleteIntent,
  deleteRoom,
  getRoomOperation,
  endMembership,
  updateMembershipIdleTimeout,
  createJoinIntent,
  joinListing,
  updateListing,
  submitReview,
  listProxies,
  createRoom,
  recommendListings,
  getRecommendationUsageProfile,
  listOwnerReviews,
  listListingReviews,
  listAccounts,
  getModelOptions,
  listKeys,
  fetchPublicSettings,
  publicSettings,
  showSuccess,
  showWarning,
  authState,
  routeQuery,
} = vi.hoisted(() => ({
  listListings: vi.fn(),
  listMembershipHistory: vi.fn(),
  getMySpendSummary: vi.fn(),
  getAPIKeyBindingStatus: vi.fn(),
  getListing: vi.fn(),
  listModeGroups: vi.fn(),
  getCapabilities: vi.fn(),
  getRoomManagementState: vi.fn(),
  drainRoom: vi.fn(),
  activateRoom: vi.fn(),
  suspendRoom: vi.fn(),
  createRoomDeleteIntent: vi.fn(),
  deleteRoom: vi.fn(),
  getRoomOperation: vi.fn(),
  endMembership: vi.fn(),
  updateMembershipIdleTimeout: vi.fn(),
  createJoinIntent: vi.fn(),
  joinListing: vi.fn(),
  updateListing: vi.fn(),
  submitReview: vi.fn(),
  listProxies: vi.fn(),
  createRoom: vi.fn(),
  recommendListings: vi.fn(),
  getRecommendationUsageProfile: vi.fn(),
  listOwnerReviews: vi.fn(),
  listListingReviews: vi.fn(),
  listAccounts: vi.fn(),
  getModelOptions: vi.fn(),
  listKeys: vi.fn(),
  fetchPublicSettings: vi.fn(),
  publicSettings: {
    user_private_group_commission_rate: 0.0075,
  },
  showSuccess: vi.fn(),
  showWarning: vi.fn(),
  showError: vi.fn(),
  authState: {
    isAdmin: false,
    user: { id: 9, balance: 100 },
  },
  routeQuery: {} as Record<string, string>,
}))

vi.mock('@/api/accountShare', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/accountShare')>()
  return {
    ...actual,
    accountShareAPI: {
      listListings,
      listMembershipHistory,
      getMySpendSummary,
      getAPIKeyBindingStatus,
      getListing,
      listModeGroups,
      getCapabilities,
      getRoomManagementState,
      drainRoom,
      activateRoom,
      suspendRoom,
      createRoomDeleteIntent,
      deleteRoom,
      getRoomOperation,
          endMembership,
      updateMembershipIdleTimeout,
      createJoinIntent,
      joinListing,
      updateListing,
      submitReview,
      listProxies,
      createRoom,
      recommendListings,
      getRecommendationUsageProfile,
      listOwnerReviews,
      listListingReviews,
    },
  }
})

vi.mock('@/api', () => ({
  accountsAPI: {
    list: listAccounts,
    getModelOptions,
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
  useAuthStore: () => authState,
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ query: routeQuery }),
  useRouter: () => ({ push: vi.fn() }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard: vi.fn().mockResolvedValue(true),
  }),
}))

const AppLayoutStub = { template: '<main><slot /></main>' }
const BaseDialogWithSlotsStub = {
  props: ['show', 'title'],
  emits: ['close'],
  template: '<section v-if="show"><h2 v-if="title" data-testid="dialog-title">{{ title }}</h2><button type="button" aria-label="关闭弹窗" @click="$emit(\'close\')"></button><slot /><slot name="footer" /></section>',
}

function listing(overrides: Partial<AccountShareListing> = {}): AccountShareListing {
  const now = '2026-07-11T01:00:00Z'
  return {
    id: 501,
    row_version: 7,
    current_revision_id: 17,
    account_id: 601,
    platform: 'openai',
    owner_user_id: 700,
    owner_username: 'owner',
    room_name: '异步快照账号',
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
    created_at: now,
    updated_at: now,
    ...overrides,
  }
}

function joinIntent(
  source: AccountShareListing,
  overrides: Partial<AccountShareJoinIntent> = {}
): AccountShareJoinIntent {
  const expectedVersion = Number(source.row_version || 7)
  const expectedRevisionID = Number(source.current_revision_id || 0)
  return {
    listing_id: source.id,
    api_key_id: 1001,
    token: 'signed-join-intent',
    expires_at: '2099-07-11T01:02:00Z',
    expected_version: expectedVersion,
    expected_revision_id: expectedRevisionID,
    terms: {
      listing_revision_id: expectedRevisionID,
      row_version: expectedVersion,
      schema_version: 1,
      room_name: source.room_name || source.account_name || `房间 #${source.id}`,
      status: source.status,
      seat_limit: source.seat_limit,
      rate_multiplier: source.rate_multiplier,
      allowed_models: [...source.allowed_models],
      per_user_concurrency: source.per_user_concurrency,
      hourly_rate: source.hourly_rate,
      hourly_fee_waiver_minimum: source.hourly_fee_waiver_minimum,
      min_balance_required: source.min_balance_required,
      codex_cli_only: source.codex_cli_only,
      codex_5h_limit_percent: source.codex_5h_limit_percent,
      codex_7d_limit_percent: source.codex_7d_limit_percent,
      anthropic_5h_limit_percent: source.anthropic_5h_limit_percent,
      anthropic_7d_limit_percent: source.anthropic_7d_limit_percent,
    },
    ...overrides,
  }
}

function roomBlockers(
  overrides: Partial<AccountShareRoomBlockers> = {}
): AccountShareRoomBlockers {
  return {
    active_membership_count: 0,
    ending_membership_count: 0,
    in_flight_request_count: 0,
    pending_billing_intent_count: 0,
    synchronous_billing_pending_count: 0,
    conflicting_operation: false,
    runtime_dependency_unavailable: false,
    ...overrides,
  }
}

function roomManagementState(
  overrides: Partial<AccountShareRoomManagementState> = {}
): AccountShareRoomManagementState {
  return {
    listing_id: 900,
    room_name: '我的共享房间',
    row_version: 7,
    lifecycle_status: 'active',
    health_state: 'healthy',
    seat_limit: 3,
    active_seats: 1,
    ending_seats: 0,
    admission_remaining_seats: 2,
    room_account_count: 2,
    configured_total_concurrency: 20,
    eligible_total_concurrency: 20,
    in_flight_concurrency: 0,
    pending_billing_intent_count: 0,
    allowed_actions: ['drain'],
    blockers: roomBlockers(),
    ...overrides,
  }
}

function roomOperation(
  overrides: Partial<AccountShareRoomOperation> = {}
): AccountShareRoomOperation {
  const now = '2026-07-11T01:00:00Z'
  return {
    id: 'operation-900',
    listing_id: 900,
    action: 'drain_room',
    status: 'pending',
    blocker: {},
    result: {},
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

function membership(overrides: Partial<AccountShareMembership> = {}): AccountShareMembership {
  const now = '2026-07-11T01:00:00Z'
  return {
    id: 801,
    listing_id: 501,
    account_id: 601,
    consumer_user_id: 9,
    api_key_id: 1001,
    status: 'ended',
    queue_rank: 0,
    idle_timeout_minutes: 10,
    joined_at: now,
    last_request_at: now,
    ended_at: now,
    created_at: now,
    updated_at: now,
    ...overrides,
  }
}

function membershipHistoryEntry(
  overrides: Partial<AccountShareMembershipHistoryEntry> = {}
): AccountShareMembershipHistoryEntry {
  return {
    membership_id: 8801,
    listing_id: 501,
    listing_revision_id: 17,
    listing_version_snapshot: 7,
    room_name: '历史房间',
    room_deleted: false,
    owner_user_id: 700,
    owner_username: 'owner',
    platform: 'openai',
    account_level: 'plus',
    account_id: 601,
    account_name: '历史账号',
    configured_concurrency_snapshot: 10,
    api_key_id: 1001,
    api_key_name: '历史 Key',
    status: 'ended',
    joined_at: '2026-07-10T01:00:00Z',
    last_request_at: '2026-07-10T01:30:00Z',
    ended_at: '2026-07-10T02:00:00Z',
    ended_reason: 'manual',
    hourly_rate_snapshot: 0.2,
    hourly_fee_waiver_minimum_snapshot: 1,
    idle_timeout_minutes: 10,
    usage_request_count: 3,
    usage_request_cost: 0.45,
    snapshot_quality: 'exact',
    terms_snapshot: {
      listing_revision_id: 17,
      row_version: 7,
      schema_version: 1,
      room_name: '历史房间',
      status: 'active',
      seat_limit: 3,
      rate_multiplier: 1,
      allowed_models: ['gpt-5.5'],
      per_user_concurrency: 1,
      hourly_rate: 0.2,
      hourly_fee_waiver_minimum: 1,
      min_balance_required: 1,
      codex_cli_only: false,
      codex_5h_limit_percent: 100,
      codex_7d_limit_percent: 100,
    },
    ...overrides,
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

function paginated(items: unknown[], page = 1, pages = 1, total = items.length, pageSize = 10) {
  return {
    items,
    total,
    page,
    page_size: pageSize,
    pages,
    total_exact: true,
    has_more: page < pages,
  }
}

function mountView(options: { renderDialogs?: boolean; attachTo?: HTMLElement } = {}) {
  return mount(AccountShareView, {
    ...(options.attachTo ? { attachTo: options.attachTo } : {}),
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        BaseDialog: BaseDialogWithSlotsStub,
        ConfirmDialog: true,
        Icon: true,
        AccountStatsModal: true,
        AccountTestModal: true,
        CreateAccountModal: {
          name: 'CreateAccountModal',
          props: { show: Boolean, initialPlatform: String, lockPlatform: Boolean, accountScope: String },
          template: '<section v-if="show" data-testid="shared-account-creator"></section>',
        },
        ModelWhitelistSelector: true,
        Select: true,
        OAuthAuthorizationFlow: {
          name: 'OAuthAuthorizationFlow',
          data: () => ({ authCode: '', oauthState: '' }),
          methods: {
            reset() {},
          },
          template: '<div data-testid="oauth-flow-stub"></div>',
        },
        ProxySelector: true,
        ReAuthAccountModal: true,
        CreateRoomDialog: {
          props: ['show', 'busy'],
          emits: ['close', 'reset'],
          template: '<section v-if="show" data-testid="create-room-dialog"><slot /></section>',
        },
        RoomAccountsDialog: {
          props: ['show', 'listing'],
          emits: ['changed'],
          template: `
            <div v-if="show" data-testid="room-accounts-dialog">
              {{ listing?.room_name }}
              <button
                data-testid="room-accounts-changed"
                @click="$emit('changed', { operation: 'add', success: 1, failed: 0 })"
              >
                changed
              </button>
            </div>
          `,
        },
        AccountShareQuotaAdminDialog: true,
        UsageProgressBar: true,
        Pagination: true,
        Teleport: true,
      },
    },
  })
}

async function openRoomDetails(
  wrapper: ReturnType<typeof mountView>,
  tab: 'overview' | 'models' | 'reviews' | 'usage' = 'overview',
  index = 0
) {
  await wrapper.findAll('.listing-card')[index].trigger('click')
  await flushPromises()
  if (tab !== 'overview') {
    await wrapper.get(`.room-detail-tabs [data-tab="${tab}"]`).trigger('click')
    await flushPromises()
  }
  return wrapper.get('[data-testid="room-details-drawer"]')
}

async function mountPendingJoin(expiresAt = '2099-07-11T01:02:00Z') {
  const room = listing({ seat_limit: 1, active_seats: 0 })
  listListings.mockResolvedValue(paginated([room]))
  const wrapper = mountView({ renderDialogs: true })
  await flushPromises()
  const state = (wrapper.vm as any).$.setupState
  state.pendingJoinConfirmation = {
    listingID: room.id,
    ownerSelfUse: false,
    platform: 'openai',
    apiKeyID: 1001,
    apiKeyLabel: '消费 Key',
    idleTimeoutMinutes: 10,
    intent: joinIntent(room, { expires_at: expiresAt }),
  }
  await nextTick()
  return { wrapper, state, room }
}

describe('AccountShareView async snapshots and mode keys', () => {
  beforeEach(() => {
    localStorage.clear()
    listListings.mockReset()
    listMembershipHistory.mockReset()
    getMySpendSummary.mockReset()
    getAPIKeyBindingStatus.mockReset()
    getListing.mockReset()
    listModeGroups.mockReset()
    getCapabilities.mockReset()
    getRoomManagementState.mockReset()
    drainRoom.mockReset()
    activateRoom.mockReset()
    suspendRoom.mockReset()
    createRoomDeleteIntent.mockReset()
    deleteRoom.mockReset()
    getRoomOperation.mockReset()
    endMembership.mockReset()
    updateMembershipIdleTimeout.mockReset()
    createJoinIntent.mockReset()
    joinListing.mockReset()
    updateListing.mockReset()
    submitReview.mockReset()
    listProxies.mockReset()
    createRoom.mockReset()
    recommendListings.mockReset()
    getRecommendationUsageProfile.mockReset()
    listOwnerReviews.mockReset()
    listListingReviews.mockReset()
    listAccounts.mockReset()
    getModelOptions.mockReset()
    listKeys.mockReset()
    fetchPublicSettings.mockReset()
    showSuccess.mockReset()
    showWarning.mockReset()
    authState.isAdmin = false
    authState.user = { id: 9, balance: 100 }
    Object.keys(routeQuery).forEach(key => delete routeQuery[key])
    publicSettings.user_private_group_commission_rate = 0.0075

    listListings.mockResolvedValue(paginated([]))
    listMembershipHistory.mockResolvedValue(paginated([]))
    getMySpendSummary.mockResolvedValue({
      range: 'current_membership',
      start_time: '2026-07-10T01:00:00Z',
      end_time: '2026-07-10T02:00:00Z',
      listing: {
        id: 501,
        account_id: 601,
        account_name: '历史账号',
        platform: 'openai',
        owner_user_id: 700,
        owner_username: 'owner',
      },
      membership: {
        id: 8801,
        api_key_id: 1001,
        api_key_name: '历史 Key',
        status: 'ended',
        queue_rank: 0,
        joined_at: '2026-07-10T01:00:00Z',
        ended_at: '2026-07-10T02:00:00Z',
        hourly_rate: 0.2,
        waiver_minimum: 1,
        idle_timeout_minutes: 10,
      },
      request_count: 0,
      input_tokens: 0,
      output_tokens: 0,
      cache_creation_tokens: 0,
      cache_read_tokens: 0,
      total_tokens: 0,
      request_cost: 0,
      hourly_charge: 0,
      hourly_refund: 0,
      hourly_waiver_refund: 0,
      hourly_net_cost: 0,
      total_cost: 0,
      model_breakdown: [],
    })
    listModeGroups.mockResolvedValue([
      { group_id: 101, platform: 'openai' },
      { group_id: 202, platform: 'anthropic' },
      { group_id: 303, platform: 'opencode' },
    ])
    getCapabilities.mockResolvedValue({
      can_create_room: true,
      live_rooms: { limit: 5, used: 0, remaining: 5 },
      room_creates_24_hours: { limit: 5, used: 0, remaining: 5 },
      owner_room_accounts: { limit: 100, used: 0, remaining: 100 },
      max_accounts_per_room: 20,
      seat_limit_minimum: 1,
      seat_limit_maximum: 30,
      capability_blockers: [],
    })
    listProxies.mockResolvedValue([])
    listOwnerReviews.mockResolvedValue(paginated([]))
    listListingReviews.mockResolvedValue(paginated([]))
    listAccounts.mockResolvedValue(paginated([]))
    getModelOptions.mockResolvedValue({ models: ['gpt-5.5'] })
    createRoom.mockResolvedValue(listing({ owner_user_id: 9, room_name: '新房间' }))
    getRoomManagementState.mockResolvedValue(roomManagementState())
    drainRoom.mockResolvedValue(roomManagementState({
      row_version: 8,
      lifecycle_status: 'paused',
      active_seats: 0,
      allowed_actions: ['activate', 'delete'],
    }))
    activateRoom.mockResolvedValue(roomManagementState({
      row_version: 8,
      lifecycle_status: 'active',
      allowed_actions: ['drain'],
    }))
    suspendRoom.mockResolvedValue(roomManagementState({
      row_version: 8,
      lifecycle_status: 'suspended',
      allowed_actions: [],
    }))
    createRoomDeleteIntent.mockResolvedValue({
      listing_id: 900,
      room_name: '我的共享房间',
      row_version: 7,
      can_delete: true,
      account_count: 2,
      blockers: roomBlockers(),
      token: 'delete-token',
      expires_at: '2099-07-11T01:02:00Z',
      history_notice: '删除后历史消费、结算和评价会继续保留。',
    })
    deleteRoom.mockResolvedValue(roomOperation({
      action: 'delete_room',
      status: 'succeeded',
      completed_at: '2026-07-11T01:00:01Z',
    }))
    getRoomOperation.mockResolvedValue(roomOperation())
    endMembership.mockResolvedValue(membership())
    updateMembershipIdleTimeout.mockResolvedValue(membership({ status: 'active' }))
    createJoinIntent.mockImplementation((id: number) => {
      const source = listing({ id })
      return Promise.resolve(joinIntent(source))
    })
    joinListing.mockResolvedValue({
      id: 801,
      listing_id: 501,
      account_id: 601,
      consumer_user_id: 9,
      api_key_id: 1001,
      status: 'active',
      queue_rank: 0,
      idle_timeout_minutes: 10,
      joined_at: '2026-07-11T01:00:00Z',
      created_at: '2026-07-11T01:00:00Z',
      updated_at: '2026-07-11T01:00:00Z',
    })
    updateListing.mockImplementation((_id: number, payload: Record<string, unknown>) =>
      Promise.resolve(listing({ row_version: Number(payload.expected_version || 0) + 1 }))
    )
    submitReview.mockResolvedValue(undefined)
    getAPIKeyBindingStatus.mockResolvedValue({
      api_key_id: 1001,
      active_count: 0,
      queued_count: 0,
      ending_count: 0,
      blocking_count: 0,
      memberships: [],
    })
    getListing.mockImplementation(async (id: number) => {
      for (const result of [...listListings.mock.results].reverse()) {
        if (result.type !== 'return') continue
        const response = await result.value
        const source = response?.items?.find((item: AccountShareListing) => item.id === id)
        if (source) return { ...source }
      }
      return listing({ id })
    })
    listKeys.mockResolvedValue(paginated([]))
    fetchPublicSettings.mockResolvedValue(publicSettings)
  })

  it('keeps cards compact and opens fresh complete details through the keyboard', async () => {
    const room = listing({ allowed_models: ['gpt-old'] })
    listListings.mockResolvedValue(paginated([room]))
    getListing.mockResolvedValue(listing({
      room_name: '最新房间条款',
      rate_multiplier: 1.75,
      allowed_models: ['gpt-current', 'gpt-reasoning', 'gpt-image'],
    }))
    const wrapper = mountView()
    await flushPromises()

    const card = wrapper.get('.listing-card')
    expect(card.attributes('role')).toBe('button')
    expect(card.attributes('tabindex')).toBe('0')
    expect(card.find('input, select, select-stub, .listing-action-row').exists()).toBe(false)
    expect(wrapper.find('[data-testid="room-details-drawer"]').exists()).toBe(false)
    expect(getListing).not.toHaveBeenCalled()
    expect(listListingReviews).not.toHaveBeenCalled()

    await card.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(getListing).toHaveBeenCalledWith(501, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    const drawer = wrapper.get('[data-testid="room-details-drawer"]')
    expect(wrapper.get('[data-testid="dialog-title"]').text()).toContain('最新房间条款')
    expect(drawer.get('.listing-price-primary strong').text()).toBe('1.75×')
    await drawer.get('[data-tab="models"]').trigger('click')
    expect(drawer.findAll('.room-detail-model-list button').map(button => button.text()))
      .toEqual(['gpt-current', 'gpt-reasoning', 'gpt-image'])
    expect(drawer.find('.listing-health-panel').exists()).toBe(false)
    expect(listListingReviews).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('loads public room reviews only on demand and keeps a retryable error distinct from an empty list', async () => {
    listListings.mockResolvedValue(paginated([listing({ rating_count: 7, rating_avg: 9.2 })]))
    listListingReviews.mockRejectedValueOnce({ message: '评论服务暂不可用' }).mockResolvedValue({
      items: [{
        id: 77, score: 9, comment: '<img src=x onerror=alert(1)> 原始留言',
        created_at: '2026-07-11T01:00:00Z', consumer_user_id: 9876,
        consumer_username: '不可公开用户', account_name: '不可公开账号',
      }],
      page: 1, pages: 1, page_size: 10, total: 1,
    })
    const wrapper = mountView()
    await flushPromises()
    await openRoomDetails(wrapper, 'models')
    expect(listListingReviews).not.toHaveBeenCalled()

    await wrapper.get('.room-detail-tabs [data-tab="reviews"]').trigger('click')
    await flushPromises()
    const reviews = wrapper.get('[data-testid="room-reviews-panel"]')
    expect(listListingReviews).toHaveBeenCalledWith(501, 1, 10, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(reviews.get('[role="alert"]').text()).toContain('评论服务暂不可用')
    expect(reviews.find('[data-testid="room-reviews-empty"]').exists()).toBe(false)
    await reviews.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(reviews.text()).toContain('7 次评分')
    expect(reviews.text()).toContain('1 条公开文字评论')
    expect(reviews.get('[data-testid="room-review"]').text()).toContain('匿名用户')
    expect(reviews.get('.room-review-comment').text()).toBe('<img src=x onerror=alert(1)> 原始留言')
    expect(reviews.find('img').exists()).toBe(false)
    expect(reviews.text()).not.toContain('不可公开')
    expect(listOwnerReviews).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('withholds join controls while room details are loading or failed and restores them after retry', async () => {
    const room = listing()
    listListings.mockResolvedValue(paginated([room]))
    let rejectDetails!: (reason: unknown) => void
    getListing.mockImplementationOnce(() => new Promise((_resolve, reject) => { rejectDetails = reject }))
      .mockResolvedValue(room)
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('.listing-card').trigger('click')
    expect(wrapper.get('[data-testid="room-details-drawer"]').text()).toContain('正在读取房间详情')
    expect(wrapper.find('.listing-action-row').exists()).toBe(false)

    rejectDetails({ message: '房间状态暂不可读取' })
    await flushPromises()
    const error = wrapper.get('.room-detail-error')
    expect(error.text()).toContain('房间状态暂不可读取')
    expect(wrapper.find('.listing-action-row').exists()).toBe(false)
    expect(createJoinIntent).not.toHaveBeenCalled()
    await error.get('button').trigger('click')
    await flushPromises()
    expect(getListing).toHaveBeenCalledTimes(2)
    expect(wrapper.find('.room-detail-error').exists()).toBe(false)
    expect(wrapper.find('.listing-action-row').exists()).toBe(true)
    wrapper.unmount()
  })

  it('aborts closed detail requests and ignores late responses after a different room opens', async () => {
    const rooms = [listing({ id: 501, room_name: '第一个房间' }), listing({ id: 502, room_name: '第二个房间' })]
    listListings.mockResolvedValue(paginated(rooms))
    const pending: Array<{ signal: AbortSignal; resolve: (value: AccountShareListing) => void }> = []
    getListing.mockImplementation((_id: number, options: { signal: AbortSignal }) => new Promise(resolve => {
      pending.push({ signal: options.signal, resolve })
    }))
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('.listing-card')[0].trigger('click')
    await wrapper.get('[aria-label="关闭弹窗"]').trigger('click')
    expect(pending[0].signal.aborted).toBe(true)
    expect(wrapper.find('[data-testid="room-details-drawer"]').exists()).toBe(false)

    await wrapper.findAll('.listing-card')[1].trigger('click')
    pending[0].resolve(listing({ id: 501, room_name: '不可覆盖的旧响应' }))
    await flushPromises()
    expect(wrapper.get('[data-testid="dialog-title"]').text()).toContain('第二个房间')
    expect(wrapper.get('[data-testid="room-details-drawer"]').text()).toContain('正在读取房间详情')
    expect(wrapper.text()).not.toContain('不可覆盖的旧响应')
    pending[1].resolve(listing({ id: 502, room_name: '第二个房间最新详情' }))
    await flushPromises()
    expect(wrapper.get('[data-testid="dialog-title"]').text()).toContain('第二个房间最新详情')
    await wrapper.get('.room-detail-tabs [data-tab="models"]').trigger('click')
    await wrapper.get('[aria-label="关闭弹窗"]').trigger('click')
    await wrapper.findAll('.listing-card')[0].trigger('click')
    expect(wrapper.get('.room-detail-tabs [data-tab="overview"]').attributes('aria-selected')).toBe('true')
    wrapper.unmount()
    expect(pending[2].signal.aborted).toBe(true)
    pending[2].resolve(rooms[0])
    await flushPromises()
  })

  it('clears obsolete membership fields when fresh room details no longer include a binding', async () => {
    listListings.mockResolvedValue(paginated([listing({
      current_membership_id: 903, current_api_key_id: 1001, current_api_key_name: '已经解除的 Key',
      current_joined_at: '2026-07-11T01:30:00Z', current_idle_timeout_minutes: 30,
    })]))
    getListing.mockResolvedValue(listing())
    listKeys.mockImplementation((_page: number, _size: number, filters: { group_id: number }) =>
      Promise.resolve(paginated(filters.group_id === 101 ? [apiKey(1001, 101, '可用 Key')] : []))
    )
    const wrapper = mountView()
    await flushPromises()
    await openRoomDetails(wrapper, 'usage')
    expect(wrapper.find('.account-share-membership-panel').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('已经解除的 Key')
    const joinButton = wrapper.findAll('button').find(button => button.text() === '加入使用')
    expect(joinButton).toBeDefined()
    expect(joinButton?.attributes('disabled')).toBeUndefined()
    const state = (wrapper.vm as any).$.setupState
    expect(state.listings[0].current_membership_id).toBeUndefined()
    expect(state.listings[0].current_api_key_id).toBeUndefined()
    wrapper.unmount()
  })

  it('preserves the requested Key membership when fresh detail fields describe a different Key', async () => {
    routeQuery.mode = 'resolve-key-binding'
    routeQuery.api_key_id = '1001'
    routeQuery.api_key_name = '指定 Key'
    getAPIKeyBindingStatus.mockResolvedValue({
      api_key_id: 1001, active_count: 1, ending_count: 0, blocking_count: 1,
      memberships: [membership({ id: 801, listing_id: 501, api_key_id: 1001, status: 'active' })],
    })
    getListing.mockResolvedValue(listing({
      current_membership_id: 990, current_api_key_id: 2002, current_api_key_name: '其他 Key',
    }))
    const wrapper = mountView()
    await flushPromises()
    await openRoomDetails(wrapper, 'usage')
    const panel = wrapper.get('.account-share-membership-panel')
    expect(panel.text()).toContain('指定 Key')
    expect(panel.text()).not.toContain('其他 Key')
    const state = (wrapper.vm as any).$.setupState
    expect(state.detailListing.current_membership_id).toBe(801)
    expect(state.detailListing.current_api_key_id).toBe(1001)
    expect(state.keyResolutionAllClear).toBe(false)
    wrapper.unmount()
  })

  it('keeps key resolution blocked and renders the ending membership from unified status', async () => {
    routeQuery.mode = 'resolve-key-binding'
    routeQuery.api_key_id = '1001'
    routeQuery.api_key_name = '结算中的 Key'
    getAPIKeyBindingStatus.mockResolvedValue({
      api_key_id: 1001,
      active_count: 0,
      queued_count: 0,
      ending_count: 1,
      blocking_count: 1,
      memberships: [membership({
        listing_id: 501,
        api_key_id: 1001,
        status: 'ending',
        ending_operation_id: undefined,
        ending_operation_status: 'needs_attention',
        settlement_status: 'pending',
      })],
    })

    const wrapper = mountView()
    await flushPromises()
    await openRoomDetails(wrapper, 'usage')
    const setupState = (wrapper.vm as any).$?.setupState

    expect(getAPIKeyBindingStatus).toHaveBeenCalledWith(1001)
    expect(setupState.keyResolutionAllClear).toBe(false)
    expect(wrapper.text()).not.toContain('关联已全部解除')
    expect(wrapper.text()).toContain('退出/结算中')
    expect(wrapper.get('[data-testid="membership-ending-state"]').text()).toContain('后台本轮处理遇到阻塞，正在继续重试')
    wrapper.unmount()
  })

  it('keeps polling unified status for an ending membership without an operation id and stops after unmount', async () => {
    vi.useFakeTimers()
    routeQuery.mode = 'resolve-key-binding'
    routeQuery.api_key_id = '1001'
    const endingStatus = {
      api_key_id: 1001,
      active_count: 0,
      queued_count: 0,
      ending_count: 1,
      blocking_count: 1,
      memberships: [membership({
        listing_id: 501,
        api_key_id: 1001,
        status: 'ending' as const,
        ending_operation_id: undefined,
        ending_operation_status: undefined,
        settlement_status: 'pending',
      })],
    }
    getAPIKeyBindingStatus.mockResolvedValue(endingStatus)
    const wrapper = mountView()
    try {
      await flushPromises()
      const initialCalls = getAPIKeyBindingStatus.mock.calls.length

      await vi.advanceTimersByTimeAsync(8_000)
      await flushPromises()

      expect(getAPIKeyBindingStatus.mock.calls.length).toBeGreaterThan(initialCalls)
      wrapper.unmount()
      const callsAfterUnmount = getAPIKeyBindingStatus.mock.calls.length

      await vi.advanceTimersByTimeAsync(8_000)
      await flushPromises()
      expect(getAPIKeyBindingStatus).toHaveBeenCalledTimes(callsAfterUnmount)
    } finally {
      if (wrapper.exists()) wrapper.unmount()
      vi.clearAllTimers()
      vi.useRealTimers()
    }
  })

  it('only blocks duplicate room names owned by the same user', async () => {
    const otherOwnerRoom = listing({
      id: 601,
      owner_user_id: 700,
      room_name: '共享名称',
    })
    const ownRoom = listing({
      id: 602,
      owner_user_id: 9,
      room_name: '我的重名房间',
    })
    listListings.mockResolvedValue(paginated([otherOwnerRoom, ownRoom]))

    const wrapper = mountView()
    await flushPromises()

    const setupState = (wrapper.vm as any).$?.setupState
    expect(setupState.validateAccountName('共享名称', undefined, 9)).toBe('')
    expect(setupState.validateAccountName('我的重名房间', undefined, 9)).toBe('房间名称已存在，请换一个名称')
    wrapper.unmount()
  })

  it('discards an older recommendation when its request inputs change', async () => {
    let resolveRecommendation!: (value: unknown) => void
    recommendListings.mockReturnValueOnce(new Promise(resolve => {
      resolveRecommendation = resolve
    }))
    listKeys.mockImplementation((_page: number, _pageSize: number, filters: { group_id: number }) => (
      Promise.resolve(paginated(
        filters.group_id === 101
          ? [apiKey(1001, 101, 'Key A'), apiKey(1002, 101, 'Key B')]
          : []
      ))
    ))

    const wrapper = mountView()
    await flushPromises()
    const setupState = (wrapper.vm as any).$?.setupState
    setupState.openRecommendationDialog()
    setupState.recommendationForm.api_key_id = 1001
    const pending = setupState.runRecommendation()
    await nextTick()
    setupState.recommendationForm.api_key_id = 1002
    await nextTick()

    resolveRecommendation({
      input: {
        platform: 'openai',
        model: 'gpt-5.5',
        api_key_id: 1001,
        request_count: 1,
        active_hours: 1,
        input_tokens: 1,
        output_tokens: 1,
        cache_creation_tokens: 0,
        cache_read_tokens: 0,
        image_input_tokens: 0,
        image_cache_read_tokens: 0,
        image_output_tokens: 0,
        limit: 10,
      },
      candidate_count: 0,
      items: [],
    })
    await pending
    await flushPromises()

    expect(setupState.recommendationResult).toBeNull()
    expect(setupState.recommendationLoading).toBe(false)
    wrapper.unmount()
  })

  it('keeps the three-day usage profile scoped to user and platform and ignores a stale dialog response', async () => {
    listListings.mockResolvedValue(paginated([listing()]))
    let resolveProfile!: (value: unknown) => void
    getRecommendationUsageProfile.mockReturnValueOnce(new Promise(resolve => {
      resolveProfile = resolve
    }))

    const wrapper = mountView()
    await flushPromises()
    const setupState = (wrapper.vm as any).$?.setupState
    setupState.openRecommendationDialog()
    const originalRequestCount = setupState.recommendationForm.request_count
    const pending = setupState.applyRecentUsageProfile()
    await nextTick()
    setupState.closeRecommendationDialog()

    resolveProfile({
      platform: 'openai',
      model: 'gpt-5.5',
      days: 3,
      start_time: '2026-07-01T00:00:00Z',
      end_time: '2026-07-04T00:00:00Z',
      has_history: true,
      model_matched: true,
      used_model_fallback: false,
      capped: false,
      total_requests: 30,
      active_hour_buckets: 3,
      request_count: 10,
      active_hours: 1,
      input_tokens_per_request: 100,
      output_tokens_per_request: 50,
      cache_creation_tokens_per_request: 0,
      cache_read_tokens_per_request: 0,
      image_output_tokens_per_request: 0,
    })
    await pending
    await flushPromises()

    expect(getRecommendationUsageProfile).toHaveBeenCalledWith(
      {
        platform: 'openai',
        model: 'gpt-5.5',
        days: 3,
      },
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    expect(setupState.recommendationForm.request_count).toBe(originalRequestCount)
    expect(setupState.recommendationUsageProfileMessage).toBe('')
    expect(setupState.recommendationUsageProfileLoading).toBe(false)
    wrapper.unmount()
  })

  it('does not allow a late owner response to replace the currently open owner', async () => {
    let resolveOwnerA!: (value: unknown) => void
    let resolveOwnerB!: (value: unknown) => void
    listListings.mockImplementation((_page: number, pageSize: number, filters?: { owner_user_id?: number }) => {
      if (filters?.owner_user_id === 700) {
        return new Promise(resolve => { resolveOwnerA = resolve })
      }
      if (filters?.owner_user_id === 701) {
        return new Promise(resolve => { resolveOwnerB = resolve })
      }
      return Promise.resolve(paginated([], 1, 1, 0, pageSize))
    })

    const wrapper = mountView()
    await flushPromises()
    const setupState = (wrapper.vm as any).$?.setupState
    const openingA = setupState.openOwnerDialog(listing({ owner_user_id: 700, owner_username: 'owner-a' }))
    await nextTick()
    const openingB = setupState.openOwnerDialog(listing({ owner_user_id: 701, owner_username: 'owner-b' }))
    await nextTick()

    resolveOwnerB(paginated([listing({ id: 702, owner_user_id: 701, room_name: 'B 的房间' })]))
    await openingB
    resolveOwnerA(paginated([listing({ id: 703, owner_user_id: 700, room_name: 'A 的房间' })]))
    await openingA
    await flushPromises()

    expect(setupState.ownerDialog.ownerUserID).toBe(701)
    expect(setupState.ownerDialog.listings.map((item: AccountShareListing) => item.room_name)).toEqual(['B 的房间'])
    wrapper.unmount()
  })

  it('shows lower-bound owner totals and follows has_more until an exact final page', async () => {
    listListings.mockImplementation((page: number, pageSize: number, filters?: { owner_user_id?: number }) => {
      if (!filters?.owner_user_id) return Promise.resolve(paginated([], 1, 1, 0, pageSize))
      if (page === 1) {
        return Promise.resolve(Object.assign(paginated(
          [listing({ id: 711, owner_user_id: 700, room_name: '第一页房间' })],
          1,
          2,
          2,
          pageSize
        ), { total_exact: false, has_more: true }))
      }
      return Promise.resolve(paginated(
        [listing({ id: 712, owner_user_id: 700, room_name: '第二页房间' })],
        2,
        2,
        2,
        pageSize
      ))
    })

    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    const setupState = (wrapper.vm as any).$?.setupState
    await setupState.openOwnerDialog(listing({ owner_user_id: 700, owner_username: 'owner' }))
    await nextTick()

    expect(wrapper.text()).toContain('已显示 1 条 · 至少 2 条')
    const loadMore = wrapper.findAll('button').find(button => button.text().includes('继续加载账号'))
    expect(loadMore).toBeDefined()
    await loadMore?.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('第一页房间')
    expect(wrapper.text()).toContain('第二页房间')
    expect(wrapper.text()).toContain('已显示 2 条 · 2 条')
    wrapper.unmount()
  })

  it('keeps owner rooms usable when reviews fail, searches by exact owner id, and anonymizes public reviews', async () => {
    const ownerRoom = listing({
      id: 721,
      owner_user_id: 700,
      owner_username: '目标号主',
      room_name: '目标号主房间',
    })
    listListings.mockImplementation((_page: number, pageSize: number, filters?: { owner_user_id?: number }) => {
      if (filters?.owner_user_id === 700) {
        return Promise.resolve(paginated([ownerRoom], 1, 1, 1, pageSize))
      }
      return Promise.resolve(paginated([], 1, 1, 0, pageSize))
    })
    listOwnerReviews
      .mockRejectedValueOnce(new Error('评论服务不可用'))
      .mockResolvedValueOnce(paginated([{
        id: 81,
        owner_user_id: 700,
        consumer_user_id: 912,
        consumer_username: '不应公开的消费者',
        account_identity_id: 991,
        account_id: 992,
        account_name: '不应公开的账号',
        platform: 'openai',
        score: 9,
        comment: '公开评论正文',
        comment_status: 'approved',
        created_at: '2026-07-11T01:00:00Z',
        updated_at: '2026-07-11T01:00:00Z',
      }]))

    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    const setupState = (wrapper.vm as any).$?.setupState
    await setupState.openOwnerDialog(ownerRoom)
    await flushPromises()

    expect(setupState.ownerDialog.listings.map((item: AccountShareListing) => item.id)).toEqual([721])
    expect(setupState.ownerDialog.listingsError).toBe('')
    expect(setupState.ownerDialog.reviewsError).toBe('评论服务不可用')

    await setupState.loadOwnerReviews()
    setupState.ownerDialog.tab = 'reviews'
    await nextTick()
    expect(wrapper.text()).toContain('公开评论正文')
    expect(wrapper.text()).toContain('来自 匿名用户')
    expect(wrapper.text()).not.toContain('不应公开的消费者')
    expect(wrapper.text()).not.toContain('不应公开的账号')

    const listingCallsBeforeSearch = listListings.mock.calls.length
    setupState.searchQuery = '会造成模糊匹配的旧关键词'
    setupState.searchOwnerFromDialog()
    await flushPromises()

    const exactOwnerCall = listListings.mock.calls.slice(listingCallsBeforeSearch).find(call =>
      call[2]?.owner_user_id === 700
    )
    expect(exactOwnerCall?.[2]).toMatchObject({ owner_user_id: 700 })
    expect(exactOwnerCall?.[2]).not.toHaveProperty('search')
    expect(setupState.searchQuery).toBe('')
    wrapper.unmount()
  })

  it('uses disclosure semantics for advanced filters instead of an incomplete menu pattern', async () => {
    const wrapper = mountView()
    await flushPromises()

    const statusTrigger = wrapper.get('button[aria-controls="account-share-status-filter"]')
    expect(statusTrigger.attributes('aria-haspopup')).toBeUndefined()
    await statusTrigger.trigger('click')
    expect(wrapper.find('[role="menu"]').exists()).toBe(false)
    expect(wrapper.get('#account-share-status-filter').attributes('role')).toBe('group')
    expect(wrapper.get('#account-share-status-filter button').attributes('aria-pressed')).toBeDefined()
    wrapper.unmount()
  })

  it('restores keyboard focus to the filter trigger after Escape closes its disclosure', async () => {
    const host = document.createElement('div')
    document.body.appendChild(host)
    const wrapper = mountView({ attachTo: host })
    try {
      await flushPromises()
      const statusTrigger = wrapper.get('button[aria-controls="account-share-status-filter"]')
      await statusTrigger.trigger('click')
      const popover = wrapper.get('#account-share-status-filter')
      ;(popover.element as HTMLElement).focus()
      await popover.trigger('keydown', { key: 'Escape' })
      await nextTick()

      expect(wrapper.find('#account-share-status-filter').exists()).toBe(false)
      expect(document.activeElement).toBe(statusTrigger.element)
    } finally {
      wrapper.unmount()
      host.remove()
    }
  })

  it('fills authoritative image usage while preserving manually split cache fields', async () => {
    listListings.mockResolvedValue(paginated([listing()]))
    listKeys.mockImplementation((_page: number, _pageSize: number, filters: { group_id: number }) =>
      Promise.resolve(paginated(
        filters.group_id === 101 ? [apiKey(1001, 101, '推荐 Key')] : []
      ))
    )
    getRecommendationUsageProfile.mockResolvedValue({
      platform: 'openai',
      model: 'gpt-5.5',
      days: 3,
      start_time: '2026-07-01T00:00:00Z',
      end_time: '2026-07-04T00:00:00Z',
      has_history: true,
      model_matched: true,
      used_model_fallback: false,
      capped: false,
      total_requests: 30,
      active_hour_buckets: 3,
      request_count: 10,
      active_hours: 2,
      input_tokens_per_request: 100,
      output_tokens_per_request: 50,
      cache_creation_tokens_per_request: 20,
      cache_read_tokens_per_request: 30,
      image_input_tokens_per_request: 220,
      image_output_tokens_per_request: 440,
    })
    recommendListings.mockResolvedValue({
      input: {
        platform: 'openai',
        model: 'gpt-5.5',
        api_key_id: 1001,
        request_count: 10,
        active_hours: 2,
        input_tokens: 1000,
        output_tokens: 500,
        cache_creation_tokens: 200,
        cache_read_tokens: 300,
        image_input_tokens: 2200,
        image_cache_read_tokens: 330,
        image_output_tokens: 4400,
        limit: 10,
      },
      candidate_count: 0,
      items: [],
    })

    const wrapper = mountView()
    await flushPromises()
    const setupState = (wrapper.vm as any).$?.setupState
    setupState.openRecommendationDialog()
    setupState.recommendationForm.cache_read_tokens_per_request = 44
    setupState.recommendationForm.image_cache_read_tokens_per_request = 33
    await setupState.applyRecentUsageProfile()
    await flushPromises()

    expect(getRecommendationUsageProfile).toHaveBeenCalledWith(
      { platform: 'openai', model: 'gpt-5.5', days: 3 },
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    expect(setupState.recommendationForm).toMatchObject({
      input_tokens_per_request: 100,
      output_tokens_per_request: 50,
      cache_creation_tokens_per_request: 20,
      cache_read_tokens_per_request: 44,
      image_input_tokens_per_request: 220,
      image_output_tokens_per_request: 440,
      image_cache_read_tokens_per_request: 33,
    })
    expect(setupState.recommendationUsageProfileMessage).toContain('历史总Cache读取 30（未自动填入）')
    expect(setupState.recommendationUsageProfileMessage).toContain('文本/图片Cache读取因无法可靠拆分')

    await setupState.runRecommendation()
    await flushPromises()
    expect(recommendListings).toHaveBeenCalledWith(
      expect.objectContaining({
        api_key_id: 1001,
        input_tokens_per_request: 100,
        output_tokens_per_request: 50,
        cache_creation_tokens_per_request: 20,
        cache_read_tokens_per_request: 44,
        image_input_tokens_per_request: 220,
        image_output_tokens_per_request: 440,
        image_cache_read_tokens_per_request: 33,
      }),
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
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

  it('removes a cached mode API Key when it expires while the page remains open', async () => {
    vi.useFakeTimers()
    let wrapper: ReturnType<typeof mountView> | undefined
    try {
      const consumerListing = listing({ id: 515, room_name: '等待 Key 过期的房间' })
      listListings.mockResolvedValue(paginated([consumerListing]))
      wrapper = mountView()
      await flushPromises()
      await openRoomDetails(wrapper, 'overview')

      const setupState = (wrapper.vm as any).$?.setupState
      const expiringKey = apiKey(1001, 101, '即将过期 Key')
      expiringKey.expires_at = new Date(Date.now() + 5_000).toISOString()
      setupState.modeGroupIDsByPlatform.openai = 101
      setupState.modeApiKeysByPlatform.openai = [expiringKey]
      setupState.modeKeysLoadedByPlatform.openai = true
      setupState.selectedKeyByListing[consumerListing.id] = expiringKey.id
      await nextTick()

      expect(setupState.modeApiKeysForListing(consumerListing).map((key: ApiKey) => key.id)).toEqual([1001])
      expect(wrapper.text()).toContain('即将过期 Key')

      vi.advanceTimersByTime(30_000)
      await nextTick()

      expect(setupState.modeApiKeysForListing(consumerListing)).toEqual([])
      expect(setupState.selectedKeyByListing[consumerListing.id]).toBe(0)
      expect(wrapper.text()).not.toContain('即将过期 Key')
      await wrapper.findAll('button').find(button => button.text() === '加入使用')?.trigger('click')
      expect(createJoinIntent).not.toHaveBeenCalled()
    } finally {
      wrapper?.unmount()
      vi.clearAllTimers()
      vi.useRealTimers()
    }
  })

  it('renders the membership panel for a current membership without a queue membership', async () => {
    const currentListing = listing({
      current_membership_id: 903,
      current_api_key_id: 78,
      current_api_key_name: '当前 Key',
      current_idle_timeout_minutes: 30,
      current_joined_at: '2026-07-11T01:30:00Z',
    })
    listListings.mockResolvedValue(paginated([currentListing]))

    const wrapper = mountView()
    await flushPromises()
    await openRoomDetails(wrapper, 'usage')
    await nextTick()

    const membershipPanel = wrapper.get('.account-share-membership-panel')
    expect(membershipPanel.text()).toContain('正在使用')
    expect(membershipPanel.text()).toContain('当前 Key')
    wrapper.unmount()
  })

  it('uses the public self-use rate and the recommendation effective rate instead of a hardcoded multiplier', async () => {
    const ownListing = listing({ owner_user_id: 9 })
    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()

    const setupState = (wrapper.vm as any).$?.setupState
    setupState.pendingJoinConfirmation = {
      listingID: ownListing.id,
      ownerSelfUse: true,
      platform: 'openai',
      apiKeyID: 1001,
      apiKeyLabel: '自用 Key',
      idleTimeoutMinutes: 10,
      intent: joinIntent(ownListing),
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

  it('requests a signed intent before opening confirmation and submits the exact server snapshot', async () => {
    const consumerListing = listing({
      room_name: '列表中的旧房间名',
      allowed_models: ['gpt-old'],
    })
    const freshIntent = joinIntent(consumerListing, {
      terms: {
        ...joinIntent(consumerListing).terms,
        room_name: '服务端最新房间名',
        rate_multiplier: 1.25,
        allowed_models: ['gpt-current'],
      },
    })
    listListings.mockResolvedValue(paginated([consumerListing]))
    listKeys.mockImplementation((_page: number, _pageSize: number, filters: { group_id: number }) =>
      Promise.resolve(filters.group_id === 101
        ? paginated([apiKey(1001, 101, '消费 Key')])
        : paginated([]))
    )
    createJoinIntent.mockResolvedValue(freshIntent)

    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    await openRoomDetails(wrapper, 'overview')
    const joinButton = wrapper.findAll('button').find(button => button.text() === '加入使用')
    await joinButton?.trigger('click')
    await flushPromises()

    expect(createJoinIntent).toHaveBeenCalledWith(consumerListing.id, {
      api_key_id: 1001,
      idle_timeout_minutes: 10,
    })
    expect(joinListing).not.toHaveBeenCalled()
    const confirmation = wrapper.get('[data-testid="join-confirmation"]')
    expect(confirmation.text()).toContain('服务端最新房间名')
    expect(confirmation.text()).toContain('1.25x')
    expect(confirmation.text()).toContain('gpt-current')
    expect(confirmation.text()).not.toContain('gpt-old')
    expect(confirmation.text()).not.toContain('配置并发')

    await wrapper.get('[data-testid="join-confirm-submit"]').trigger('click')
    await flushPromises()

    expect(joinListing).toHaveBeenCalledWith(consumerListing.id, {
      api_key_id: 1001,
      idle_timeout_minutes: 10,
      intent_token: 'signed-join-intent',
      expected_version: 7,
      expected_revision_id: 17,
    })
    wrapper.unmount()
  })

  it('checks binding before expiring a confirmation when the shared clock advances', async () => {
    vi.useFakeTimers()
    let wrapper: ReturnType<typeof mountView> | undefined
    try {
      const consumerListing = listing({ room_name: '即将过期的房间' })
      wrapper = mountView({ renderDialogs: true })
      await flushPromises()

      const setupState = (wrapper.vm as any).$?.setupState
      setupState.pendingJoinConfirmation = {
        listingID: consumerListing.id,
        ownerSelfUse: false,
        platform: 'openai',
        apiKeyID: 1001,
        apiKeyLabel: '消费 Key',
        idleTimeoutMinutes: 10,
        intent: joinIntent(consumerListing, {
          expires_at: new Date(Date.now() + 5_000).toISOString(),
        }),
      }
      await nextTick()

      const submitButton = wrapper.get('[data-testid="join-confirm-submit"]')
      expect(submitButton.attributes('disabled')).toBeUndefined()

      vi.advanceTimersByTime(30_000)
      await nextTick()

      expect(submitButton.attributes('disabled')).toBeUndefined()
      expect(submitButton.text()).toContain('核对绑定状态')
      await submitButton.trigger('click')
      await flushPromises()
      expect(getAPIKeyBindingStatus).toHaveBeenCalledWith(1001)
      expect(setupState.pendingJoinConfirmation).toBeNull()
      expect(wrapper.text()).toContain('未发现该 Key 正在使用此房间')
      expect(joinListing).not.toHaveBeenCalled()
    } finally {
      wrapper?.unmount()
      vi.clearAllTimers()
      vi.useRealTimers()
    }
  })

  it('closes stale confirmation, refreshes listings, and requires a new confirmation when terms change', async () => {
    const consumerListing = listing({ room_name: '会变化的房间' })
    listListings.mockResolvedValue(paginated([consumerListing]))
    listKeys.mockImplementation((_page: number, _pageSize: number, filters: { group_id: number }) =>
      Promise.resolve(filters.group_id === 101
        ? paginated([apiKey(1001, 101, '消费 Key')])
        : paginated([]))
    )
    createJoinIntent.mockResolvedValue(joinIntent(consumerListing))
    joinListing.mockRejectedValue({ reason: 'ACCOUNT_SHARE_JOIN_TERMS_CHANGED' })

    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    await openRoomDetails(wrapper, 'overview')
    await wrapper.findAll('button').find(button => button.text() === '加入使用')?.trigger('click')
    await flushPromises()
    const listingCallsBeforeSubmit = listListings.mock.calls.length

    await wrapper.get('[data-testid="join-confirm-submit"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="join-confirmation"]').exists()).toBe(false)
    expect(listListings.mock.calls.length).toBeGreaterThan(listingCallsBeforeSubmit)
    expect(wrapper.text()).toContain('旧确认已关闭')
    expect(wrapper.text()).toContain('重新点击加入并确认最新条款')
    wrapper.unmount()
  })

  it('allows only one join-intent request at a time across different rooms', async () => {
    const firstListing = listing({ id: 511, room_name: '第一个房间' })
    const secondListing = listing({ id: 512, room_name: '第二个房间' })
    listListings.mockResolvedValue(paginated([firstListing, secondListing]))
    listKeys.mockImplementation((_page: number, _pageSize: number, filters: { group_id: number }) =>
      Promise.resolve(filters.group_id === 101
        ? paginated([apiKey(1001, 101, '消费 Key')])
        : paginated([]))
    )
    let resolveIntent!: (intent: AccountShareJoinIntent) => void
    createJoinIntent.mockReturnValue(new Promise(resolve => {
      resolveIntent = resolve
    }))

    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    const setupState = (wrapper.vm as any).$?.setupState
    const firstRequest = setupState.joinUse(firstListing)
    await nextTick()
    await setupState.joinUse(secondListing)

    expect(createJoinIntent).toHaveBeenCalledTimes(1)
    expect(createJoinIntent).toHaveBeenCalledWith(firstListing.id, expect.any(Object))

    resolveIntent(joinIntent(firstListing))
    await firstRequest
    await flushPromises()
    wrapper.unmount()
  })

  it('defaults to existing accounts, accepts an implicit private placement, and filters out incompatible room members', async () => {
    listAccounts.mockResolvedValue(paginated([
      account({ id: 1, name: '可用隐式私有账号', external_placement: null }),
      account({ id: 2, name: '公共号池账号', external_placement: { target: 'public_pool', state: 'active', version: 2 } }),
      account({ id: 3, name: '其他房间账号', external_placement: { target: 'room', room_id: 99, state: 'active', version: 3 } }),
      account({ id: 7, name: '未绑定平台模式账号', external_placement: { target: 'room', state: 'active', version: 4 } }),
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
    expect(wrapper.find('[data-testid="create-room-new-account"]').exists()).toBe(true)
    expect(setupState.eligibleOwnedAccounts.map((item: Account) => item.id)).toHaveLength(3)
    expect(setupState.eligibleOwnedAccounts.map((item: Account) => item.id))
      .toEqual(expect.arrayContaining([1, 2, 7]))
    expect(wrapper.text()).toContain('公共号池账号')
    expect(wrapper.text()).toContain('可用隐式私有账号')
    expect(wrapper.text()).toContain('未绑定平台模式账号')
    expect(wrapper.text()).not.toContain('其他房间账号')
    expect(wrapper.text()).not.toContain('未知等级账号')
    expect(wrapper.text()).toContain('成员上限（1～30）')
    expect(wrapper.text()).toContain('由房主设置，与账号数量/账号并发无推导关系；房主自用不占消费者名额')
    const seatLimitInput = wrapper.get('[data-testid="create-room-seat-limit"]')
    expect(seatLimitInput.attributes('type')).toBe('number')
    expect(seatLimitInput.attributes('min')).toBe('1')
    expect(seatLimitInput.attributes('max')).toBe('30')
    expect((seatLimitInput.element as HTMLInputElement).value).toBe('5')
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
        room_name: 'OpenAI房间',
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
      room_name: 'OpenAI房间',
      seat_limit: 5,
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

  it.each([
    [
      'ACCOUNT_SHARE_QUOTA_HISTORICAL_GROWTH_BLOCKED',
      '当前用量超过新配额，历史保留状态下只能收缩，不能继续创建或增加房间账号',
    ],
    [
      'ACCOUNT_SHARE_QUOTA_GRANDFATHER_GROWTH_BLOCKED',
      '当前处于历史保留配额，只能减少现有用量；请先整理房间或联系管理员调整配额',
    ],
  ])('maps the room growth blocker %s to a user-facing message', async (reason, message) => {
    listAccounts.mockResolvedValue(paginated([
      account({
        id: 23,
        name: '配额校验账号',
        external_placement: { target: 'private', state: 'active', version: 1 },
      }),
    ]))
    createRoom.mockRejectedValue({ reason })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('创建房间'))?.trigger('click')
    await flushPromises()
    const setupState = (wrapper.vm as any).$?.setupState
    setupState.selectedOwnedAccountID = 23
    await setupState.createRoomFromOwnedAccount()
    await flushPromises()

    expect(createRoom).toHaveBeenCalledTimes(1)
    expect(setupState.createErrorMessage).toBe(message)
    wrapper.unmount()
  })

  it('renders room metadata and opens the room member dialog for the owner', async () => {
    const ownRoom = listing({
      id: 900,
      owner_user_id: 9,
      room_name: '我的共享房间',
      account_count: 3,
      healthy_account_count: 2,
    })
    listListings.mockImplementation((_page: number, pageSize: number) =>
      Promise.resolve(pageSize === 10 ? paginated([ownRoom]) : paginated([ownRoom]))
    )

    const wrapper = mountView()
    await flushPromises()
    await openRoomDetails(wrapper, 'overview')
    expect(wrapper.text()).toContain('我的共享房间')
    expect(wrapper.text()).toContain('可调度账号 2/3')
    expect(wrapper.get('.room-detail-facts').text()).toContain('成员席位1 / 3')
    expect(wrapper.text()).not.toContain('消费者 1/3')
    expect(wrapper.text()).toContain('可用并发')
    expect(wrapper.text()).not.toContain('实时容量')

    await wrapper.get('.room-detail-tabs [data-tab="usage"]').trigger('click')
    const roomCountButton = wrapper.findAll('button').find(button => button.text().includes('查看房间账号'))
    await roomCountButton?.trigger('click')
    await nextTick()

    expect(wrapper.get('[data-testid="room-accounts-dialog"]').text()).toContain('我的共享房间')
    wrapper.unmount()
  })

  it('renders one combined availability bar without leaking ranges or a public representative account', async () => {
    const publicRoom = listing({
      id: 903,
      owner_user_id: 700,
      room_name: undefined,
      account_name: '不应公开的代表账号',
      account_sample_scope: 'representative',
      account_count: 3,
      healthy_account_count: 1,
      account_concurrency: 8,
      current_concurrency: 0,
      runtime_load_known: false,
      accounts: [{
        account_id: 601,
        account_name: '不应公开的成员账号',
        platform: 'openai',
        account_level: 'plus',
        status: 'active',
        schedulable: true,
        current_concurrency: 2,
        priority: 50,
        placement_state: 'active',
      }],
      quota_summary: {
        scope: 'room',
        attached_count: 3,
        eligible_count: 2,
        window_5h: {
          known_count: 2,
          min_utilization: 10,
          max_utilization: 120,
          average_utilization: 65,
          partial: true,
        },
        window_7d: {
          known_count: 3,
          min_utilization: 40,
          max_utilization: 40,
          average_utilization: 40,
          partial: false,
        },
      },
    })
    listListings.mockResolvedValue(paginated([publicRoom]))

    const wrapper = mountView()
    await flushPromises()
    await openRoomDetails(wrapper, 'overview')

    expect(wrapper.text()).toContain('房间 #903')
    expect(wrapper.text()).toContain('可调度账号 2/3')
    expect(wrapper.get('[data-testid="room-quota-summary"]').text()).toContain('5H 综合已用65%')
    expect(wrapper.get('[data-testid="room-quota-summary"]').text()).toContain('7D 综合已用40%')
    const progressBars = wrapper.findAll('[role="progressbar"]')
    expect(progressBars).toHaveLength(2)
    expect(progressBars[0]?.attributes('aria-valuenow')).toBe('65')
    expect(progressBars[0]?.get('span').attributes('style')).toContain('width: 65%')
    expect(progressBars[1]?.attributes('aria-valuenow')).toBe('40')
    expect(progressBars[1]?.get('span').attributes('style')).toContain('width: 40%')
    expect(wrapper.text()).not.toContain('用量范围')
    expect(wrapper.text()).not.toContain('10%–120%')
    expect(wrapper.text()).not.toContain('部分快照')
    expect(wrapper.text()).toContain('运行时未知')
    expect(wrapper.text()).not.toContain('不应公开的代表账号')
    expect(wrapper.text()).not.toContain('不应公开的成员账号')
    wrapper.unmount()
  })

  it.each([
    { status: 'paused' as const, label: '已下架' },
    { status: 'validating' as const, label: '恢复校验中' },
    { status: 'draining' as const, label: '下架处理中' },
    { status: 'suspended' as const, label: '管理员暂停' },
    { status: 'disabled' as const, label: '已下架' },
  ])('prioritizes the $label lifecycle over healthy account availability', async ({ status, label }) => {
    listListings.mockResolvedValue(paginated([
      listing({
        status,
        account_count: 2,
        healthy_account_count: 2,
      }),
    ]))

    const wrapper = mountView()
    await flushPromises()
    await openRoomDetails(wrapper, 'overview')

    const [aggregateTile, concurrencyTile] = wrapper.findAll('.listing-runtime-tile')
    expect(aggregateTile).toBeDefined()
    expect(concurrencyTile).toBeDefined()
    expect(aggregateTile.text()).toContain(label)
    expect(aggregateTile.text()).not.toContain('全部挂载账号当前具备路由资格')
    expect(aggregateTile.text()).not.toContain('可调度账号')
    expect(aggregateTile.text()).not.toContain('可用')
    expect(concurrencyTile.text()).toContain('并发状态')
    expect(concurrencyTile.text()).toContain('当前不可新加入')
    wrapper.unmount()
  })

  it('uses the same member-limit contract in the edit dialog', async () => {
    const ownRoom = listing({
      id: 902,
      owner_user_id: 9,
      room_name: '待编辑房间',
      account_count: 1,
      healthy_account_count: 1,
    })
    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()

    const setupState = (wrapper.vm as any).$?.setupState
    setupState.editingConfigListing = ownRoom
    setupState.editForm.seat_limit = 3
    setupState.showConfigEditDialog = true
    await nextTick()

    expect(wrapper.text()).toContain('成员上限（1～30）')
    expect(wrapper.text()).toContain('由房主设置，与账号数量/账号并发无推导关系；房主自用不占消费者名额')
    const seatLimitInput = wrapper.get('[data-testid="edit-room-seat-limit"]')
    expect(seatLimitInput.attributes('type')).toBe('number')
    expect(seatLimitInput.attributes('min')).toBe('1')
    expect(seatLimitInput.attributes('max')).toBe('30')
    wrapper.unmount()
  })

  it('saves the configuration snapshot version even when runtime state has advanced, without administrator override fields', async () => {
    const activeRoom = listing({
      id: 902,
      owner_user_id: 9,
      room_name: '房主可编辑房间',
      status: 'active',
      active_seats: 0,
      row_version: 12,
      current_revision_id: 20,
    })
    listListings.mockResolvedValue(paginated([activeRoom]))
    getRoomManagementState.mockResolvedValue(roomManagementState({
      listing_id: activeRoom.id,
      room_name: activeRoom.room_name,
      row_version: 99,
      lifecycle_status: 'active',
      active_seats: 0,
      allowed_actions: ['drain', 'delete'],
    }))
    updateListing.mockResolvedValue({
      ...activeRoom,
      row_version: 13,
    })

    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    const setupState = (wrapper.vm as any).$?.setupState
    setupState.requestOpenConfigEdit(activeRoom)
    await flushPromises()

    setupState.editReason = '根据近期使用情况调整房间参数'
    await setupState.saveConfigEdit()
    await flushPromises()

    expect(updateListing).toHaveBeenCalledTimes(1)
    const payload = updateListing.mock.calls[0][1]
    expect(payload).toEqual(expect.objectContaining({
      expected_version: 12,
      seat_limit: 3,
      reason: '根据近期使用情况调整房间参数',
    }))
    expect(payload).not.toHaveProperty('edit_session_id')
    expect(payload).not.toHaveProperty('force_active_edit')
    expect(payload).not.toHaveProperty('confirmed')
    wrapper.unmount()
  })

  it('opens consumer-protected editing without taking an exclusive edit lock', async () => {
    const activeRoom = listing({
      id: 906,
      owner_user_id: 9,
      room_name: '仍有成员的房间',
      status: 'active',
      active_seats: 1,
      row_version: 14,
    })
    listListings.mockResolvedValue(paginated([activeRoom]))
    getRoomManagementState.mockResolvedValue(roomManagementState({
      listing_id: activeRoom.id,
      room_name: activeRoom.room_name,
      row_version: activeRoom.row_version,
      lifecycle_status: 'active',
      active_seats: 1,
      blockers: roomBlockers({ active_membership_count: 1 }),
    }))

    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    const setupState = (wrapper.vm as any).$?.setupState
    await setupState.requestOpenConfigEdit(activeRoom)
    await flushPromises()
    expect(setupState.showConfigEditDialog).toBe(true)
    expect(setupState.editConsumerProtected).toBe(true)
    expect(wrapper.text()).toContain('基础配置')
    wrapper.unmount()
  })

  it('requires an administrator reason and confirmation before sending a complete force-edit request', async () => {
    authState.isAdmin = true
    const activeRoom = listing({
      id: 903,
      owner_user_id: 700,
      room_name: '运行中的管理员房间',
      status: 'active',
      active_seats: 1,
      row_version: 15,
      current_revision_id: 23,
    })
    listListings.mockResolvedValue(paginated([activeRoom]))
    getRoomManagementState.mockResolvedValue(roomManagementState({
      listing_id: activeRoom.id,
      room_name: activeRoom.room_name,
      row_version: activeRoom.row_version,
      lifecycle_status: 'active',
      active_seats: 1,
      blockers: roomBlockers({ active_membership_count: 1 }),
    }))
    updateListing.mockResolvedValue({
      ...activeRoom,
      row_version: 16,
    })

    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    const setupState = (wrapper.vm as any).$?.setupState
    await setupState.requestOpenConfigEdit(activeRoom)
    await nextTick()

    const forceButton = wrapper.get('[data-testid="confirm-force-edit"]')
    expect(forceButton.attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="force-edit-reason"]').setValue('紧急修正错误价格')
    expect(forceButton.attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="force-edit-confirmed"]').setValue(true)
    expect(forceButton.attributes('disabled')).toBeUndefined()
    await forceButton.trigger('click')
    await flushPromises()
    await setupState.saveConfigEdit()
    await flushPromises()

    expect(updateListing).toHaveBeenCalledWith(903, expect.objectContaining({
      expected_version: 15,
      force_active_edit: true,
      reason: '紧急修正错误价格',
      confirmed: true,
    }), expect.stringMatching(/^account-share-listing-update-903-/))
    wrapper.unmount()
  })

  it('shows a recoverable version-conflict state and reopens editing from refreshed data', async () => {
    const pausedRoom = listing({
      id: 904,
      owner_user_id: 9,
      room_name: '发生冲突的房间',
      status: 'paused',
      active_seats: 0,
      row_version: 21,
    })
    listListings.mockResolvedValue(paginated([pausedRoom]))
    getRoomManagementState.mockResolvedValue(roomManagementState({
      listing_id: pausedRoom.id,
      room_name: pausedRoom.room_name,
      row_version: pausedRoom.row_version,
      lifecycle_status: 'paused',
      active_seats: 0,
      allowed_actions: ['activate', 'delete'],
    }))
    updateListing.mockRejectedValue({ reason: 'ACCOUNT_SHARE_ROOM_VERSION_CONFLICT' })

    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    const setupState = (wrapper.vm as any).$?.setupState
    setupState.requestOpenConfigEdit(pausedRoom)
    await flushPromises()
    setupState.editReason = '验证版本冲突处理'
    await setupState.saveConfigEdit()
    await flushPromises()

    expect(wrapper.text()).toContain('房间配置已被更新，请刷新后重新编辑')
    await wrapper.get('[data-testid="reload-conflicted-room-config"]').trigger('click')
    await flushPromises()
    expect(listListings).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('renders repeated stays in the same room as independent membership history records', async () => {
    listMembershipHistory.mockResolvedValue(paginated([
      membershipHistoryEntry({
        membership_id: 8802,
        listing_id: 905,
        joined_at: '2026-07-12T01:00:00Z',
        ended_at: '2026-07-12T02:00:00Z',
      }),
      membershipHistoryEntry({
        membership_id: 8801,
        listing_id: 905,
        joined_at: '2026-07-10T01:00:00Z',
        ended_at: '2026-07-10T02:00:00Z',
      }),
    ]))

    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === '我的使用')?.trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === '历史记录')?.trigger('click')
    await flushPromises()

    expect(listMembershipHistory).toHaveBeenCalledWith(
      1,
      10,
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    expect(listListings.mock.calls.some(call => call[2]?.tab === 'history')).toBe(false)
    expect(wrapper.findAll('[data-testid="membership-history-card"]')).toHaveLength(2)
    expect(wrapper.text()).toContain('记录 #8801')
    expect(wrapper.text()).toContain('记录 #8802')
    wrapper.unmount()
  })

  it('uses membership history in my spend and submits the exact selected membership', async () => {
    listListings.mockImplementation((_page: number, _pageSize: number, filters?: { tab?: string }) =>
      Promise.resolve(filters?.tab === 'using' ? paginated([], 1, 1, 0, 12) : paginated([]))
    )
    listMembershipHistory.mockResolvedValue(paginated([
      membershipHistoryEntry({
        membership_id: 8802,
        listing_id: 905,
        room_name: '重复入住的房间',
        joined_at: '2026-07-12T01:00:00Z',
        ended_at: '2026-07-12T02:00:00Z',
      }),
      membershipHistoryEntry({
        membership_id: 8801,
        listing_id: 905,
        room_name: '重复入住的房间',
        joined_at: '2026-07-10T01:00:00Z',
        ended_at: '2026-07-10T02:00:00Z',
      }),
    ], 1, 1, 2, 12))

    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('我的消费'))?.trigger('click')
    await flushPromises()

    expect(listMembershipHistory).toHaveBeenCalledWith(
      1,
      12,
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    expect(listListings.mock.calls.some(call => call[2]?.tab === 'history')).toBe(false)
    const historyOptions = wrapper.findAll('.my-spend-account-option')
    expect(historyOptions).toHaveLength(2)
    expect(historyOptions.map(option => option.text())).toEqual(expect.arrayContaining([
      expect.stringContaining('记录 #8801'),
      expect.stringContaining('记录 #8802'),
    ]))

    await historyOptions.find(option => option.text().includes('记录 #8801'))?.trigger('click')
    await flushPromises()

    expect(getMySpendSummary).toHaveBeenLastCalledWith(
      905,
      {
        range: 'current_membership',
        membership_id: 8801,
        timezone: expect.any(String),
      },
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    const rangeButtons = wrapper.findAll('.my-spend-range-tabs button')
    expect(rangeButtons.find(button => button.text() === '本次使用')?.attributes('disabled')).toBeUndefined()
    expect(rangeButtons.find(button => button.text() === '今天')?.attributes('disabled')).toBeDefined()
    expect(rangeButtons.find(button => button.text() === '近7天')?.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('不会合并同一房间的其他使用记录')
    wrapper.unmount()
  })

  it('paginates my spend history without scanning every history page on open', async () => {
    listListings.mockImplementation((_page: number, _pageSize: number, filters?: { tab?: string }) =>
      Promise.resolve(filters?.tab === 'using' ? paginated([], 1, 1, 0, 12) : paginated([]))
    )
    listMembershipHistory.mockImplementation((page: number) => Promise.resolve(
      page === 1
        ? paginated([membershipHistoryEntry({ membership_id: 8801 })], 1, 2, 13, 12)
        : paginated([membershipHistoryEntry({ membership_id: 8813 })], 2, 2, 13, 12)
    ))

    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('我的消费'))?.trigger('click')
    await flushPromises()

    expect(listMembershipHistory).toHaveBeenCalledTimes(1)
    expect(listMembershipHistory).toHaveBeenLastCalledWith(
      1,
      12,
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )

    const setupState = (wrapper.vm as any).$?.setupState
    await setupState.handleMySpendAccountPageChange(2)
    await flushPromises()

    expect(listMembershipHistory).toHaveBeenCalledTimes(2)
    expect(listMembershipHistory).toHaveBeenLastCalledWith(
      2,
      12,
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    expect(wrapper.text()).toContain('记录 #8813')
    wrapper.unmount()
  })

  it('aborts an older spend summary before refreshing account options', async () => {
    let resolveOlderSummary!: (value: unknown) => void
    getMySpendSummary.mockReturnValue(new Promise(resolve => {
      resolveOlderSummary = resolve
    }))
    listListings.mockImplementation((_page: number, _pageSize: number, filters?: { tab?: string }) =>
      Promise.resolve(filters?.tab === 'using' ? paginated([], 1, 1, 0, 12) : paginated([]))
    )
    listMembershipHistory
      .mockResolvedValueOnce(paginated([
        membershipHistoryEntry({ membership_id: 8801, listing_id: 905 }),
      ], 1, 1, 1, 12))
      .mockResolvedValueOnce(paginated([], 1, 1, 0, 12))

    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('我的消费'))?.trigger('click')
    await flushPromises()

    const summarySignal = getMySpendSummary.mock.calls[0]?.[2]?.signal as AbortSignal
    expect(summarySignal.aborted).toBe(false)
    const accountPicker = wrapper.get('.my-spend-account-picker')
    await accountPicker.findAll('button').find(button => button.text().includes('刷新账号'))?.trigger('click')
    await flushPromises()

    expect(summarySignal.aborted).toBe(true)
    resolveOlderSummary({ total_cost: 999 })
    await flushPromises()

    const setupState = (wrapper.vm as any).$?.setupState
    expect(setupState.mySpendSelectedOption).toBeNull()
    expect(setupState.mySpendSummary).toBeNull()
    expect(wrapper.text()).not.toContain('999')
    wrapper.unmount()
  })

  it('renders Anthropic history terms with Claude thresholds instead of Codex fields', async () => {
    listMembershipHistory.mockResolvedValue(paginated([
      membershipHistoryEntry({
        membership_id: 8898,
        platform: 'anthropic',
        terms_snapshot: {
          ...membershipHistoryEntry().terms_snapshot!,
          anthropic_5h_limit_percent: 35,
          anthropic_7d_limit_percent: 55,
        },
      }),
    ]))

    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === '我的使用')?.trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === '历史记录')?.trigger('click')
    await flushPromises()

    const panel = wrapper.get('[data-testid="membership-history-panel"]')
    expect(panel.text()).toContain('Claude 5小时 / 7天阈值')
    expect(panel.text()).toContain('35% / 55%')
    expect(panel.text()).not.toContain('Codex CLI')
    expect(panel.text()).not.toContain('Codex 5小时 / 7天阈值')
    wrapper.unmount()
  })

  it('shows a retryable membership history error and recovers without touching listing pagination', async () => {
    listMembershipHistory
      .mockRejectedValueOnce(new Error('history unavailable'))
      .mockResolvedValueOnce(paginated([membershipHistoryEntry({ membership_id: 8897 })], 2, 2))

    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === '我的使用')?.trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === '历史记录')?.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('history unavailable')
    const setupState = (wrapper.vm as any).$?.setupState
    setupState.membershipHistoryPagination.page = 2
    await wrapper.findAll('button').find(button => button.text().includes('重新加载'))?.trigger('click')
    await flushPromises()

    expect(listMembershipHistory).toHaveBeenLastCalledWith(
      2,
      10,
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    expect(wrapper.text()).toContain('记录 #8897')
    expect(setupState.pagination.page).toBe(1)
    wrapper.unmount()
  })

  it('does not let a late membership history response overwrite a newer listing view', async () => {
    let resolveHistory!: (value: ReturnType<typeof paginated>) => void
    listMembershipHistory.mockReturnValue(new Promise(resolve => {
      resolveHistory = resolve
    }))
    listListings.mockResolvedValue(paginated([
      listing({ id: 512, room_name: '当前账号列表', account_name: '不应公开的底层账号名' }),
    ]))

    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === '我的使用')?.trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === '历史记录')?.trigger('click')
    await nextTick()
    await wrapper.findAll('button').find(button => button.text().trim() === '找房间')?.trigger('click')
    await flushPromises()

    resolveHistory(paginated([
      membershipHistoryEntry({ membership_id: 8896, room_name: '迟到的历史响应' }),
    ]))
    await flushPromises()

    const setupState = (wrapper.vm as any).$?.setupState
    expect(wrapper.text()).toContain('当前账号列表')
    expect(wrapper.text()).not.toContain('迟到的历史响应')
    expect(setupState.membershipHistoryEntries).toEqual([])
    wrapper.unmount()
  })

  it('submits a review against the selected historical membership and refreshes history', async () => {
    listMembershipHistory.mockResolvedValue(paginated([
      membershipHistoryEntry({
        membership_id: 8899,
        listing_id: 905,
        room_name: '可评价历史房间',
        usage_request_count: 2,
      }),
    ]))

    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === '我的使用')?.trigger('click')
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === '历史记录')?.trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="membership-history-review"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('可评价历史房间')
    const scoreButton = wrapper.findAll('button').find(button => button.text().trim() === '8')
    expect(scoreButton).toBeDefined()
    await scoreButton?.trigger('click')
    await wrapper.findAll('button').find(button => button.text().includes('提交评分'))?.trigger('click')
    await flushPromises()

    expect(submitReview).toHaveBeenCalledWith(
      8899,
      { score: 8, comment: undefined },
      expect.stringMatching(/^account-share-review-8899-/)
    )
    expect(listMembershipHistory.mock.calls.length).toBeGreaterThan(1)
    wrapper.unmount()
  })

  it('loads a deleted room through the archive tab and keeps the snapshot immutable', async () => {
    const deletedRoom = listing({
      id: 905,
      owner_user_id: 9,
      room_name: '已经删除的房间',
      status: 'active',
      deleted: true,
      history_snapshot_quality: 'exact',
      active_seats: 7,
      healthy_account_count: 4,
      account_count: 4,
      current_concurrency: 6,
      account_concurrency: 99,
      current_membership_id: 9905,
      current_api_key_name: '不应展示的实时 Key',
    })
    listListings.mockImplementation((_page: number, _pageSize: number, filters?: { tab?: string }) =>
      Promise.resolve(filters?.tab === 'archive' ? paginated([deletedRoom]) : paginated([]))
    )

    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === '我的房间')?.trigger('click')
    await flushPromises()
    await wrapper.get('#owner-room-state').setValue('archive')
    await flushPromises()
    await openRoomDetails(wrapper)

    expect(listListings).toHaveBeenLastCalledWith(
      1,
      10,
      expect.objectContaining({ tab: 'archive' }),
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    expect(wrapper.text()).toContain('已删除')
    expect(wrapper.text()).toContain('删除时保存的精确房间条款快照')
    const archiveCard = wrapper.get('[data-testid="archive-listing-card"]')
    expect(archiveCard.text()).not.toContain('健康账号')
    expect(archiveCard.text()).not.toContain('账号状态')
    expect(archiveCard.text()).not.toContain('运行时请求能力')
    expect(archiveCard.text()).not.toContain('5小时可用量')
    expect(archiveCard.text()).not.toContain('不应展示的实时 Key')
    expect(archiveCard.text()).not.toContain('结束使用')
    expect(archiveCard.find('button').exists()).toBe(false)
    expect(wrapper.find('[data-testid="room-lifecycle-entry"]').exists()).toBe(false)
    expect(wrapper.findAll('button').some(button => button.text().includes('加入使用'))).toBe(false)
    expect(wrapper.findAll('button').some(button => button.text().includes('编辑配置'))).toBe(false)
    expect(wrapper.findAll('button').some(button => button.text().includes('查看房间账号'))).toBe(false)
    await wrapper.get('.room-detail-tabs [data-tab="reviews"]').trigger('click')
    expect(wrapper.text()).toContain('历史快照未保存当时的评论')
    expect(getListing).not.toHaveBeenCalled()
    expect(listListingReviews).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it.each([
    { label: 'explicit unknown', id: 906, quality: 'unknown' as const },
    { label: 'unmarked', id: 908, quality: undefined },
  ])('renders an $label legacy history record as a compact honest card without projected room details', async ({ id, quality }) => {
    const unknownHistory = listing({
      id,
      deleted: true,
      history_snapshot_quality: quality,
      last_used_at: '2026-06-20T08:30:00Z',
      owner_username: '绝不展示的号主',
      account_name: '绝不展示的账号',
      rate_multiplier: 9.9,
      account_concurrency: 99,
      per_user_concurrency: 88,
      allowed_models: ['gpt-secret-history'],
    })
    listListings.mockImplementation((_page: number, _pageSize: number, filters?: { tab?: string }) =>
      Promise.resolve(filters?.tab === 'archive' ? paginated([unknownHistory]) : paginated([]))
    )

    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === '我的房间')?.trigger('click')
    await flushPromises()
    await wrapper.get('#owner-room-state').setValue('archive')
    await flushPromises()
    await openRoomDetails(wrapper)

    const card = wrapper.get('[data-testid="unknown-history-card"]')
    expect(card.text()).toContain(`房间 ID：#${id}`)
    expect(card.text()).toContain('已删除')
    expect(card.text()).toContain('最后使用')
    expect(card.text()).toContain('迁移前的房间详情与使用条款不可恢复')
    expect(card.text()).not.toContain('绝不展示的号主')
    expect(card.text()).not.toContain('绝不展示的账号')
    expect(card.text()).not.toContain('gpt-secret-history')
    expect(card.find('button').exists()).toBe(false)
    expect(wrapper.find('[data-testid="room-lifecycle-entry"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('labels a backfilled legacy history record as non-exact', async () => {
    const backfilledHistory = listing({
      id: 907,
      deleted: true,
      history_snapshot_quality: 'backfilled_current',
      last_used_at: '2026-06-21T08:30:00Z',
    })
    listListings.mockImplementation((_page: number, _pageSize: number, filters?: { tab?: string }) =>
      Promise.resolve(filters?.tab === 'archive' ? paginated([backfilledHistory]) : paginated([]))
    )

    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === '我的房间')?.trigger('click')
    await flushPromises()
    await wrapper.get('#owner-room-state').setValue('archive')
    await flushPromises()
    await openRoomDetails(wrapper)

    expect(wrapper.get('[data-testid="backfilled-history-notice"]').text())
      .toContain('不是删除当时保存的精确快照')
    expect(wrapper.text()).toContain('由删除前最终房间信息回填')
    wrapper.unmount()
  })

  it('does not carry live filters into archive requests and restores the archive preference', async () => {
    localStorage.setItem('account-share-listing-preferences:user:9', JSON.stringify({
      platform: 'openai',
      tab: 'archive',
      search: '保留快照',
      pageSize: 10,
      status: 'available',
      accountLevel: 'plus',
      sortKeys: ['remaining_seats:desc'],
      seatLimits: [15],
      featureTags: ['available'],
      models: ['gpt-live-only'],
    }))
    const archivedRoom = listing({
      id: 909,
      deleted: true,
      room_name: '保留快照',
      history_snapshot_quality: 'exact',
    })
    listListings.mockImplementation((_page: number, _pageSize: number, filters?: { tab?: string }) =>
      Promise.resolve(filters?.tab === 'archive' ? paginated([archivedRoom]) : paginated([]))
    )

    const wrapper = mountView()
    await flushPromises()
    await openRoomDetails(wrapper, 'overview')

    const archiveCall = listListings.mock.calls.find(call => call[2]?.tab === 'archive')
    expect(archiveCall).toBeDefined()
    expect(archiveCall?.[2]).toEqual({
      tab: 'archive',
      platform: 'openai',
      search: '保留快照',
    })
    expect(wrapper.find('[data-testid="archive-readonly-notice"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="archive-listing-card"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('本页可用席位')
    wrapper.unmount()
  })

  it('does not carry a hidden owner filter into an archive transition', async () => {
    const wrapper = mountView()
    await flushPromises()
    const setupState = (wrapper.vm as any).$?.setupState
    setupState.selectedOwnerID = 700
    setupState.selectedOwnerDisplayName = '其他号主'
    await nextTick()

    const callsBeforeArchive = listListings.mock.calls.length
    await wrapper.findAll('button').find(button => button.text() === '我的房间')?.trigger('click')
    await flushPromises()
    await wrapper.get('#owner-room-state').setValue('archive')
    await flushPromises()

    const archiveCalls = listListings.mock.calls
      .slice(callsBeforeArchive)
      .filter(call => call[2]?.tab === 'archive')
    expect(archiveCalls).toHaveLength(1)
    expect(archiveCalls[0]?.[2]).not.toHaveProperty('owner_user_id')
    expect(setupState.hasResultFilters).toBe(false)
    expect(wrapper.text()).toContain('暂无已删除房间')
    wrapper.unmount()
  })

  it('refreshes room counts and rebinds the open dialog after room accounts change', async () => {
    const ownRoom = listing({
      id: 901,
      owner_user_id: 9,
      room_name: '待更新房间',
      account_count: 1,
      healthy_account_count: 1,
    })
    const refreshedRoom = {
      ...ownRoom,
      account_count: 2,
      healthy_account_count: 2,
      updated_at: '2026-07-11T02:00:00Z',
    }
    let mainListRequestCount = 0
    listListings.mockImplementation((_page: number, pageSize: number) => {
      if (pageSize !== 10) return Promise.resolve(paginated([refreshedRoom]))
      mainListRequestCount += 1
      return Promise.resolve(paginated([
        mainListRequestCount === 1 ? ownRoom : refreshedRoom,
      ]))
    })

    const wrapper = mountView()
    await flushPromises()
    await openRoomDetails(wrapper, 'usage')
    const roomCountButton = wrapper.findAll('button').find(button =>
      button.text().includes('查看房间账号')
    )
    await roomCountButton?.trigger('click')
    await wrapper.get('[data-testid="room-accounts-changed"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('可调度账号 2/2')
    expect(wrapper.get('[data-testid="room-accounts-dialog"]').text()).toContain('待更新房间')
    expect(showSuccess).toHaveBeenCalledWith('已有 1 个账号成功加入房间')
    wrapper.unmount()
  })

  it('shows owner quotas and blocks the create dialog when a server capability is exhausted', async () => {
    getCapabilities.mockResolvedValue({
      can_create_room: false,
      live_rooms: { limit: 5, used: 5, remaining: 0 },
      room_creates_24_hours: { limit: 5, used: 3, remaining: 2 },
      owner_room_accounts: { limit: 100, used: 12, remaining: 88 },
      max_accounts_per_room: 20,
      seat_limit_minimum: 1,
      seat_limit_maximum: 30,
      capability_blockers: [{
        code: 'ACCOUNT_SHARE_ROOM_LIMIT_EXCEEDED',
        message: '未删除房间数量已达到上限',
        limit: 5,
        used: 5,
      }],
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('5/5')
    expect(wrapper.text()).toContain('未删除房间数量已达到配额上限')
    const createButton = wrapper.findAll('button').find(button => button.text().includes('创建房间'))
    expect(createButton?.attributes('disabled')).toBeDefined()
    expect(createButton?.attributes('title')).toBe('未删除房间数量已达到配额上限')
    expect(wrapper.find('[data-testid="create-room-dialog"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('keeps an accepted exit in ending state and opens review only after the operation reports ended', async () => {
    vi.useFakeTimers()
    const activeListing = listing({
      current_membership_id: 801,
      current_api_key_id: 1001,
      current_api_key_name: '退出测试 Key',
      current_idle_timeout_minutes: 10,
      current_last_request_at: '2026-07-11T01:00:00Z',
    })
    listListings.mockImplementation((_page: number, pageSize: number) =>
      Promise.resolve(pageSize === 10 ? paginated([activeListing]) : paginated([activeListing]))
    )
    endMembership.mockResolvedValue(membership({
      status: 'ending',
      ended_at: undefined,
      last_request_at: '2026-07-11T01:00:00Z',
      // 单阶段结束：operation id 由后端生成并随响应返回
      ending_operation_id: 'operation-end-801',
      settlement_status: 'pending',
    }))
    getRoomOperation.mockResolvedValue(roomOperation({
      id: 'operation-end-801',
      listing_id: activeListing.id,
      action: 'end_membership',
      status: 'succeeded',
      result: {
        status: 'ended',
        ended_at: '2026-07-11T01:00:05Z',
        settlement_status: 'settled',
      },
      completed_at: '2026-07-11T01:00:05Z',
    }))

    const wrapper = mountView({ renderDialogs: true })
    try {
      await flushPromises()
      await openRoomDetails(wrapper, 'usage')
      const setupState = (wrapper.vm as any).$?.setupState
      setupState.pendingEndUse = {
        membershipID: 801,
        apiKeyID: 1001,
        apiKeyName: '退出测试 Key',
        status: 'active',
        listing: activeListing,
      }

      await setupState.confirmEndUse()
      await flushPromises()

      expect(endMembership).toHaveBeenCalledWith(801)
      expect(setupState.pendingReview).toBeNull()
      expect(wrapper.get('[data-testid="membership-ending-state"]').text()).toContain('退出请求已受理')
      expect(wrapper.text()).toContain('正在退出并结算')
      expect(getRoomOperation).not.toHaveBeenCalled()

      await vi.advanceTimersByTimeAsync(8_000)
      await flushPromises()

      expect(getRoomOperation).toHaveBeenCalledWith(
        'operation-end-801',
        expect.objectContaining({ signal: expect.any(AbortSignal) })
      )
      expect(setupState.pendingReview).toMatchObject({
        membershipID: 801,
        roomName: '异步快照账号',
        ownerName: 'owner',
      })
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('restores an accepted exit operation from the listing projection after refresh', async () => {
    vi.useFakeTimers()
    const endingListing = listing({
      id: 909,
      current_membership_id: undefined,
      queue_membership_id: 18012,
      queue_api_key_id: 15007,
      queue_api_key_name: '恢复测试 Key',
      queue_status: 'ending',
      queue_ending_operation_id: 'operation-restored-18012',
      queue_ending_operation_status: 'running',
      queue_settlement_status: 'pending',
    })
    listListings.mockResolvedValue(paginated([endingListing]))
    getRoomOperation.mockResolvedValue(roomOperation({
      id: 'operation-restored-18012',
      listing_id: endingListing.id,
      action: 'end_membership',
      status: 'running',
    }))

    const wrapper = mountView()
    try {
      await flushPromises()
      await openRoomDetails(wrapper, 'usage')

      expect(wrapper.get('[data-testid="membership-ending-state"]').text()).toContain('退出请求已受理')
      expect(wrapper.text()).toContain('正在退出并结算')
      expect(wrapper.text()).not.toContain('缺少进度标识')

      await vi.advanceTimersByTimeAsync(8_000)
      await flushPromises()

      expect(getRoomOperation).toHaveBeenCalledWith(
        'operation-restored-18012',
        expect.objectContaining({ signal: expect.any(AbortSignal) })
      )
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('disables rejoin while the listing projection is ending even without a membership id', async () => {
    listListings.mockResolvedValue(paginated([
      listing({
        id: 908,
        queue_status: 'ending',
        current_membership_id: undefined,
        queue_membership_id: undefined,
      }),
    ]))

    const wrapper = mountView()
    await flushPromises()
    await openRoomDetails(wrapper, 'overview')

    const button = wrapper.findAll('button').find(item => item.text().includes('退出结算处理中'))
    expect(button).toBeDefined()
    expect(button?.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('结算完成后才能重新加入。')
    expect(createJoinIntent).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('refreshes a visible validating room after eight seconds', async () => {
    vi.useFakeTimers()
    let mainRequestCount = 0
    listListings.mockImplementation((_page: number, pageSize: number) => {
      if (pageSize !== 10) return Promise.resolve(paginated([]))
      mainRequestCount += 1
      return Promise.resolve(paginated([
        listing({ status: mainRequestCount === 1 ? 'validating' : 'active' }),
      ]))
    })

    const wrapper = mountView()
    try {
      await flushPromises()
      expect(mainRequestCount).toBe(1)

      await vi.advanceTimersByTimeAsync(8_000)
      await flushPromises()

      expect(mainRequestCount).toBe(2)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('does not let an aborted older listing response overwrite a newer response', async () => {
    let resolveOlder!: (value: ReturnType<typeof paginated>) => void
    let resolveNewer!: (value: ReturnType<typeof paginated>) => void
    let mainRequestCount = 0
    listListings.mockImplementation((_page: number, pageSize: number) => {
      if (pageSize !== 10) return Promise.resolve(paginated([]))
      mainRequestCount += 1
      return new Promise(resolve => {
        if (mainRequestCount === 1) resolveOlder = resolve
        else resolveNewer = resolve
      })
    })

    const wrapper = mountView()
    const setupState = (wrapper.vm as any).$?.setupState
    const newerLoad = setupState.loadListings()
    resolveNewer(paginated([listing({ id: 502, room_name: '最新房间', account_name: '最新底层账号' })]))
    await newerLoad
    await nextTick()
    expect(wrapper.text()).toContain('最新房间')

    resolveOlder(paginated([listing({ id: 501, room_name: '旧响应房间', account_name: '旧响应底层账号' })]))
    await flushPromises()
    expect(wrapper.text()).toContain('最新房间')
    expect(wrapper.text()).not.toContain('旧响应房间')
    wrapper.unmount()
  })

  it('opens owner lifecycle management, follows allowed actions, and prevents duplicate drain submits', async () => {
    const ownRoom = listing({
      id: 900,
      owner_user_id: 9,
      room_name: '我的共享房间',
    })
    listListings.mockResolvedValue(paginated([ownRoom]))
    getRoomManagementState.mockResolvedValue(roomManagementState({
      allowed_actions: ['drain'],
    }))
    let resolveDrain!: (value: AccountShareRoomManagementState) => void
    drainRoom.mockReturnValue(new Promise<AccountShareRoomManagementState>(resolve => {
      resolveDrain = resolve
    }))

    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    const ownerFilterButton = wrapper.findAll('button').find(button =>
      button.text() === '我的房间'
    )
    await ownerFilterButton?.trigger('click')
    await flushPromises()
    await openRoomDetails(wrapper, 'usage')
    await wrapper.get('[data-testid="room-lifecycle-entry"]').trigger('click')
    await flushPromises()

    expect(getRoomManagementState).toHaveBeenCalledWith(
      900,
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    expect(wrapper.find('[data-testid="room-lifecycle-action-drain"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="room-lifecycle-action-activate"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="room-lifecycle-action-suspend"]').exists()).toBe(false)

    await wrapper.get('[data-testid="room-lifecycle-action-drain"]').trigger('click')
    expect(wrapper.get('[data-testid="room-lifecycle-submit"]').classes()).toEqual(
      expect.arrayContaining(['btn', 'btn-primary', 'min-h-11'])
    )
    await wrapper.get('[data-testid="room-lifecycle-submit"]').trigger('click')
    await wrapper.get('[data-testid="room-lifecycle-submit"]').trigger('click')
    await nextTick()

    expect(drainRoom).toHaveBeenCalledTimes(1)
    const closeButton = wrapper.findAll('button').find(button => button.text() === '关闭')
    await closeButton?.trigger('click')
    expect(wrapper.find('[data-testid="room-lifecycle-dialog"]').exists()).toBe(true)

    resolveDrain(roomManagementState({
      row_version: 8,
      lifecycle_status: 'paused',
      active_seats: 0,
      allowed_actions: ['activate', 'delete'],
    }))
    await flushPromises()

    expect(drainRoom).toHaveBeenCalledWith(
      900,
      {
        expected_version: 7,
        confirmed: true,
      },
      expect.stringMatching(/^account-share-room-900-drain-/)
    )
    wrapper.unmount()
  })

  it('checks delete intent and renders concrete blockers before allowing deletion', async () => {
    const ownRoom = listing({
      id: 900,
      owner_user_id: 9,
      room_name: '我的共享房间',
    })
    listListings.mockResolvedValue(paginated([ownRoom]))
    createRoomDeleteIntent.mockResolvedValue({
      listing_id: 900,
      room_name: '我的共享房间',
      row_version: 7,
      can_delete: false,
      account_count: 2,
      blockers: roomBlockers({
        active_membership_count: 2,
        in_flight_request_count: 1,
        runtime_dependency_unavailable: true,
      }),
      history_notice: '删除后历史消费、结算和评价会继续保留。',
    })

    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === '我的房间')?.trigger('click')
    await flushPromises()
    await openRoomDetails(wrapper, 'usage')
    await wrapper.get('[data-testid="room-lifecycle-entry"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="room-lifecycle-action-delete"]').trigger('click')
    await flushPromises()

    expect(createRoomDeleteIntent).toHaveBeenCalledWith(900, {
      expected_version: 7,
    })
    const blockers = wrapper.get('[data-testid="room-delete-blockers"]').text()
    expect(blockers).toContain('正在使用的成员')
    expect(blockers).toContain('2')
    expect(blockers).toContain('进行中的请求')
    expect(blockers).toContain('运行时状态')
    expect(wrapper.text()).toContain('删除后历史消费、结算和评价会继续保留。')
    expect(wrapper.find('[data-testid="room-lifecycle-submit"]').exists()).toBe(false)
    expect(deleteRoom).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('requires the exact room name and submits the signed soft-delete token', async () => {
    const ownRoom = listing({
      id: 900,
      owner_user_id: 9,
      room_name: '我的共享房间',
    })
    listListings.mockResolvedValue(paginated([ownRoom]))
    getRoomManagementState.mockResolvedValue(roomManagementState({
      lifecycle_status: 'paused',
      active_seats: 0,
      allowed_actions: ['activate', 'delete'],
    }))

    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === '我的房间')?.trigger('click')
    await flushPromises()
    await openRoomDetails(wrapper, 'usage')
    await wrapper.get('[data-testid="room-lifecycle-entry"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="room-lifecycle-action-delete"]').trigger('click')
    await flushPromises()

    const submitButton = wrapper.get('[data-testid="room-lifecycle-submit"]')
    expect(submitButton.classes()).toEqual(
      expect.arrayContaining(['btn', 'btn-danger', 'min-h-11'])
    )
    const backButton = wrapper.findAll('button').find(button => button.text() === '返回')
    expect(backButton?.classes()).toEqual(
      expect.arrayContaining(['btn', 'btn-secondary', 'min-h-11'])
    )
    expect(submitButton.attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="room-delete-name-input"]').setValue('我的共享房间')
    await nextTick()
    expect(submitButton.attributes('disabled')).toBeUndefined()
    await submitButton.trigger('click')
    await flushPromises()

    expect(deleteRoom).toHaveBeenCalledWith(
      900,
      {
        expected_version: 7,
        room_name: '我的共享房间',
        token: 'delete-token',
        confirmed: true,
      },
      expect.stringMatching(/^account-share-room-900-delete-/)
    )
    expect(wrapper.get('[data-testid="room-lifecycle-deleted"]').text()).toContain('房间已删除')
    expect(wrapper.text()).toContain('历史消费、结算和评价记录仍会保留')
    wrapper.unmount()
  })

  it('stops pending operation polling after the lifecycle dialog closes', async () => {
    vi.useFakeTimers()
    const ownRoom = listing({
      id: 900,
      owner_user_id: 9,
      room_name: '我的共享房间',
    })
    listListings.mockResolvedValue(paginated([ownRoom]))
    getRoomManagementState.mockResolvedValue(roomManagementState({
      lifecycle_status: 'draining',
      allowed_actions: [],
      pending_operation_id: 'operation-900',
      blockers: roomBlockers({
        conflicting_operation: true,
        conflicting_operation_id: 'operation-900',
      }),
    }))
    getRoomOperation.mockResolvedValue(roomOperation({
      id: 'operation-900',
      status: 'pending',
    }))

    const wrapper = mountView({ renderDialogs: true })
    try {
      await flushPromises()
      await wrapper.findAll('button').find(button => button.text() === '我的房间')?.trigger('click')
      await flushPromises()
      await openRoomDetails(wrapper, 'usage')
      await wrapper.get('[data-testid="room-lifecycle-entry"]').trigger('click')
      await flushPromises()

      expect(getRoomOperation).toHaveBeenCalledTimes(1)
      const closeButton = wrapper.findAll('button').find(button => button.text() === '关闭')
      await closeButton?.trigger('click')
      await nextTick()
      expect(wrapper.find('[data-testid="room-lifecycle-dialog"]').exists()).toBe(false)

      await vi.advanceTimersByTimeAsync(1600)
      await flushPromises()
      expect(getRoomOperation).toHaveBeenCalledTimes(1)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('shows the complete room lifecycle, seat, permission and archive rules in the usage guide', async () => {
    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()

    const usageGuideButton = wrapper.findAll('button').find(button => button.text().includes('使用说明'))
    expect(usageGuideButton).toBeDefined()
    await usageGuideButton?.trigger('click')
    await nextTick()

    const guideText = wrapper.text()
    expect(guideText).toContain('每个 Key 同时只能使用一个房间')
    expect(guideText).toContain('每个低消核销窗口最长 1 小时')
    expect(guideText).toContain('不跨窗口累计抵扣')
    expect(guideText).toContain('我的使用 → 历史记录')
    expect(guideText).toContain('均匀分布到各小时窗口')
    wrapper.unmount()
  })

  it('buildListingFilters: available 只在 tab=all 翻译成 available_only，管理视图不全量过滤', async () => {
    const wrapper = mountView()
    await flushPromises()
    const setupState = (wrapper.vm as any).$?.setupState
    // 默认 status=available（广场默认只看可用）
    expect(setupState.listingFilters.status).toBe('available')
    // tab=all：available → status=active + available_only=true
    const allFilters = setupState.buildListingFilters('all')
    expect(allFilters.status).toBe('active')
    expect(allFilters.available_only).toBe(true)
    // tab=mine：available 不被翻译，管理视图全量
    const mineFilters = setupState.buildListingFilters('mine')
    expect(mineFilters.available_only).toBeUndefined()
    expect(mineFilters.status).toBeUndefined()
    wrapper.unmount()
  })

  it('满员房间明确阻止加入且不提供排队', async () => {
    const fullRoom = listing({
      id: 601,
      room_name: '已满员房间',
      seat_limit: 2,
      active_seats: 2,
      account_count: 1,
      healthy_account_count: 1,
      min_balance_required: 0,
    })
    const unavailableRoom = listing({
      id: 602,
      room_name: '无可路由账号房间',
      seat_limit: 3,
      active_seats: 1,
      account_count: 1,
      quota_summary: { eligible_count: 0, attached_count: 1 },
      min_balance_required: 0,
    })
    listListings.mockResolvedValue(paginated([fullRoom, unavailableRoom]))
    const wrapper = mountView()
    await flushPromises()
    const setupState = (wrapper.vm as any).$?.setupState
    // 满员只提示选择其他房间，不能创建隐藏预约。
    expect(setupState.listingJoinUnavailableReason(fullRoom)).toContain('房间已满')
    // 无可路由账号 = 不可加入（quota_summary.eligible_count 明确为 0）
    expect(setupState.listingJoinUnavailableReason(unavailableRoom)).toContain('没有可路由账号')
    wrapper.unmount()
  })

  it('余额不足的房间对非号主置灰，号主自用不受余额限制', async () => {
    const highBalanceRoom = listing({
      id: 701,
      room_name: '高余额门槛房间',
      seat_limit: 3,
      active_seats: 1,
      account_count: 1,
      healthy_account_count: 1,
      min_balance_required: 999999,
    })
    const ownRoom = {
      ...highBalanceRoom,
      id: 702,
      owner_user_id: 9, // 当前用户是号主
      room_name: '号主自己的高门槛房间',
    }
    listListings.mockResolvedValue(paginated([highBalanceRoom, ownRoom]))
    const wrapper = mountView()
    await flushPromises()
    const setupState = (wrapper.vm as any).$?.setupState
    // 非号主：余额不足 → 不可加入
    expect(setupState.listingJoinUnavailableReason(highBalanceRoom)).toContain('最低余额')
    // 号主自用：不受余额限制
    expect(setupState.listingJoinUnavailableReason(ownRoom)).toBe('')
    wrapper.unmount()
  })

  it('管理员浏览广场默认不启用可用性过滤（保持全量）', async () => {
    const originalUser = authState.user
    authState.user = { ...(originalUser as object), role: 'admin' } as never
    try {
      const wrapper = mountView()
      await flushPromises()
      const setupState = (wrapper.vm as any).$?.setupState
      // admin 默认 status 为空（非 available）
      expect(setupState.listingFilters.status).toBe('')
      // tab=all 下 admin 不翻译 available_only
      const allFilters = setupState.buildListingFilters('all')
      expect(allFilters.available_only).toBeUndefined()
      wrapper.unmount()
    } finally {
      authState.user = originalUser
    }
  })

  it('老用户 localStorage 的旧 status="" 会迁移为当前默认可用账号', async () => {
    localStorage.setItem('account-share-listing-preferences:user:9', JSON.stringify({
      platform: 'openai',
      tab: 'all',
      search: '',
      pageSize: 10,
      status: '', // 旧版本默认
      accountLevel: 'all',
      sortKeys: [],
      seatLimits: [],
      featureTags: [],
      models: [],
    }))
    const wrapper = mountView()
    await flushPromises()
    const setupState = (wrapper.vm as any).$?.setupState
    // 普通用户（非 admin）：旧 '' 迁移为 available
    expect(setupState.listingFilters.status).toBe('available')
    // 迁移写回 localStorage
    const stored = JSON.parse(localStorage.getItem('account-share-listing-preferences:user:9') as string)
    expect(stored.status).toBe('available')
    wrapper.unmount()
  })
  it('keeps three primary views and places history and deleted rooms underneath them', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.findAll('.filter-actions > button').map(button => button.text())).toEqual(['找房间', '我的使用', '我的房间'])
    await wrapper.findAll('.filter-actions > button')[1].trigger('click')
    await flushPromises()
    expect(wrapper.find('[aria-label="我的使用视图"]').exists()).toBe(true)
    await wrapper.findAll('.filter-actions > button')[2].trigger('click')
    await flushPromises()
    expect(wrapper.find('#owner-room-state').exists()).toBe(true)
    wrapper.unmount()
  })

  it('clears a platform-specific model filter when changing platform', async () => {
    const wrapper = mountView()
    await flushPromises()
    const state = (wrapper.vm as any).$.setupState
    state.listingFilters.models = ['gpt-5.5']
    state.setListingPlatform('anthropic')
    await flushPromises()
    expect(state.listingFilters.models).toEqual([])
    expect(listListings.mock.calls.at(-1)?.[2]).toMatchObject({ platform: 'anthropic' })
    expect(listListings.mock.calls.at(-1)?.[2]).not.toHaveProperty('models')
    wrapper.unmount()
  })

  it('refreshes active membership usage without a focus event and stops after unmount', async () => {
    vi.useFakeTimers()
    const active = listing({
      current_membership_id: 901,
      current_api_key_id: 1001,
      current_waiver_progress: {
        enabled: true, status: 'in_progress', window_start: '2026-07-11T01:00:00Z',
        window_end: '2026-07-11T02:00:00Z', now: '2026-07-11T01:30:00Z',
        elapsed_seconds: 1800, remaining_seconds: 1800, required_amount: 1,
        usage_amount: 0.25, remaining_amount: 0.75, progress_percent: 25,
        hourly_rate: 0.2, waiver_minimum: 2, estimated_hourly_fee_refund: 0.1, request_count: 1,
      },
    })
    let refreshCount = 0
    listListings.mockImplementation((_page: number, size: number) => {
      if (size !== 10) return Promise.resolve(paginated([]))
      refreshCount += 1
      return Promise.resolve(paginated([refreshCount === 1 ? active : {
        ...active,
        current_waiver_progress: {
          ...active.current_waiver_progress!,
          window_start: '2026-07-11T02:00:00Z',
          window_end: '2026-07-11T03:00:00Z',
          now: '2026-07-11T02:30:00Z',
          usage_amount: 0.8,
          request_count: 4,
        },
      }]))
    })
    const wrapper = mountView()
    try {
      await flushPromises()
      const initial = listListings.mock.calls.filter(call => call[1] === 10).length
      await openRoomDetails(wrapper, 'usage')
      expect(wrapper.get('.waiver-progress-track').attributes('aria-valuenow')).toBe('25')
      await vi.advanceTimersByTimeAsync(8_000)
      await flushPromises()
      expect(listListings.mock.calls.filter(call => call[1] === 10).length).toBe(initial + 1)
      const state = (wrapper.vm as any).$.setupState
      expect(state.listings[0].current_waiver_progress.window_start).toBe('2026-07-11T02:00:00Z')
      expect(wrapper.get('.waiver-progress-track').attributes('aria-valuenow')).toBe('80')
      expect(wrapper.get('.room-detail-tabs [data-tab="usage"]').attributes('aria-selected')).toBe('true')
      wrapper.unmount()
      const finished = listListings.mock.calls.length
      await vi.advanceTimersByTimeAsync(16_000)
      expect(listListings).toHaveBeenCalledTimes(finished)
    } finally {
      vi.useRealTimers()
    }
  })

  it('renders inexact pagination as a lower bound without an invented last page', async () => {
    listListings.mockImplementation((_page: number, size: number) => Promise.resolve({
      ...paginated(size === 10 ? [listing()] : [], 1, 2, 11),
      total_exact: false,
      has_more: true,
    }))
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('至少 11')
    const pager = wrapper.get('[data-testid="listing-cursor-pagination"]')
    expect(pager.text()).toContain('下一页')
    expect(pager.text()).not.toContain('共 2 页')
    expect(wrapper.find('pagination-stub').exists()).toBe(false)
    wrapper.unmount()
  })

  it('does not show queue controls or score-based recommendations', async () => {
    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    const state = (wrapper.vm as any).$.setupState
    state.openRecommendationDialog()
    await nextTick()
    expect(wrapper.find('[data-testid="join-accept-queue"]').exists()).toBe(false)
    expect(wrapper.find('.recommendation-score-panel').exists()).toBe(false)
    expect(wrapper.text()).toContain('均匀分布在各小时窗口')
    expect(wrapper.text()).not.toContain('综合匹配度')
    wrapper.unmount()
  })

  it('orders fee estimates by cost and displays the service settlement assumption', async () => {
    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    const state = (wrapper.vm as any).$.setupState
    state.openRecommendationDialog()
    const candidate = (id: number, cost: number) => ({
      rank: id,
      listing: listing({ id, room_name: `费用房间 ${id}` }),
      estimate: {
        assumption: '按每小时均匀使用估算，实际按独立小时窗口结算。',
        billing_mode: 'token', base_request_cost: cost, request_cost: cost,
        per_request_cost: cost / 10, hourly_gross_cost: 0.4, hourly_waived_cost: 0.4,
        hourly_net_cost: 0, waiver_required_amount: 1, waiver_usage_amount: cost,
        waiver_eligible: true, total_cost: cost, upfront_required: 1,
        effective_rate_multiplier: 1, effective_hourly_rate: 0.2, owner_self_use: false,
      },
    })
    state.recommendationResult = {
      input: { active_hours: 2, request_count: 10, platform: 'openai', model: 'gpt-5.5' },
      candidate_count: 2,
      items: [candidate(51, 8), candidate(52, 2)],
    }
    await nextTick()
    expect(state.recommendationCandidates.map((item: { listing: AccountShareListing }) => item.listing.id)).toEqual([52, 51])
    expect(wrapper.text()).toContain('按每小时均匀使用估算，实际按独立小时窗口结算。')
    expect(wrapper.text()).not.toContain('综合匹配度')
    wrapper.unmount()
  })

  it('uses lower-bound pagination in the usage picker and follows has_more instead of an estimated page count', async () => {
    listListings.mockImplementation((page: number, size: number, filters?: { tab?: string }) => Promise.resolve(
      filters?.tab === 'using'
        ? {
            ...paginated([listing({ id: 801 + page, current_membership_id: 900 + page, current_api_key_id: 1001 })], page, 1, 2, size),
            total_exact: page === 2,
            has_more: page === 1,
          }
        : paginated([])
    ))
    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    const state = (wrapper.vm as any).$.setupState
    state.openMySpendDialog()
    await flushPromises()
    expect(wrapper.text()).toContain('当前使用 至少 2')
    const pager = wrapper.get('[data-testid="my-spend-cursor-pagination"]')
    expect(pager.text()).not.toContain('共 1 页')
    await pager.findAll('button')[1].trigger('click')
    await flushPromises()
    expect(listListings).toHaveBeenCalledWith(2, expect.any(Number), { tab: 'using' }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(state.mySpendUsingPagination.totalExact).toBe(true)
    expect(state.mySpendUsingPagination.hasMore).toBe(false)
    wrapper.unmount()
  })

  it('mounts the shared account creator while closed and opens it with the selected room platform', async () => {
    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    const creator = wrapper.getComponent({ name: 'CreateAccountModal' })
    expect(creator.props('show')).toBe(false)
    const state = (wrapper.vm as any).$.setupState
    state.createPlatform = 'openai'
    state.openStandaloneAccountCreator()
    await flushPromises()
    expect(wrapper.getComponent({ name: 'CreateAccountModal' }).vm).toBe(creator.vm)
    expect(creator.props()).toMatchObject({
      show: true, initialPlatform: 'openai', lockPlatform: true, accountScope: 'user',
    })
    state.showCreateAccount = false
    await nextTick()
    state.createPlatform = 'anthropic'
    state.openStandaloneAccountCreator()
    await flushPromises()
    expect(creator.props('initialPlatform')).toBe('anthropic')
    wrapper.unmount()
  })

  it.each([
    { name: 'network status zero', error: { status: 0, message: 'network unavailable' } },
    { name: 'timeout', error: { status: 0, code: 'ECONNABORTED' } },
    { name: 'unstructured failure', error: new Error('response lost') },
    { name: 'server failure', error: { status: 503, message: 'response unavailable' } },
  ])('keeps the original join token after $name and retries the same request', async ({ error }) => {
    const { wrapper, state } = await mountPendingJoin()
    joinListing.mockRejectedValueOnce(error).mockResolvedValueOnce(membership({ status: 'active' }))
    await wrapper.get('[data-testid="join-confirm-submit"]').trigger('click')
    await flushPromises()

    expect(state.pendingJoinConfirmation.intent.token).toBe('signed-join-intent')
    expect(wrapper.get('[data-testid="join-intent-error"]').text()).toContain('加入结果待确认')
    expect(getAPIKeyBindingStatus).toHaveBeenCalledWith(1001)
    expect(wrapper.get('[data-testid="join-confirm-submit"]').attributes('disabled')).toBeUndefined()

    await wrapper.get('[data-testid="join-confirm-submit"]').trigger('click')
    await flushPromises()
    expect(joinListing).toHaveBeenCalledTimes(2)
    expect(joinListing.mock.calls[1]).toEqual(joinListing.mock.calls[0])
    expect(createJoinIntent).not.toHaveBeenCalled()
    expect(state.pendingJoinConfirmation).toBeNull()
    wrapper.unmount()
  })

  it('confirms a successful join by key even when taking the last seat hides the room from available results', async () => {
    const { wrapper, state, room } = await mountPendingJoin()
    joinListing.mockRejectedValue({ status: 504 })
    getAPIKeyBindingStatus.mockResolvedValue({
      api_key_id: 1001, active_count: 1, ending_count: 0, blocking_count: 1,
      memberships: [membership({ api_key_id: 1001, listing_id: room.id, status: 'active' })],
    })
    listListings.mockResolvedValue(paginated([]))
    await wrapper.get('[data-testid="join-confirm-submit"]').trigger('click')
    await flushPromises()
    expect(getAPIKeyBindingStatus).toHaveBeenCalledWith(1001)
    expect(state.pendingJoinConfirmation).toBeNull()
    expect(state.listings).toEqual([])
    expect(showSuccess).toHaveBeenCalledWith('已确认当前 Key 正在使用该房间，可在“我的使用”查看。')
    expect(createJoinIntent).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('requires both the requested key and room to match before accepting binding recovery', async () => {
    const { wrapper, state, room } = await mountPendingJoin()
    joinListing.mockRejectedValue({ status: 0 })
    getAPIKeyBindingStatus.mockResolvedValue({
      api_key_id: 1001, active_count: 2, ending_count: 0, blocking_count: 2,
      memberships: [
        membership({ api_key_id: 1002, listing_id: room.id, status: 'active' }),
        membership({ api_key_id: 1001, listing_id: room.id + 1, status: 'active' }),
      ],
    })
    await wrapper.get('[data-testid="join-confirm-submit"]').trigger('click')
    await flushPromises()
    expect(state.pendingJoinConfirmation.intent.token).toBe('signed-join-intent')
    expect(showSuccess).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('recovers an already active binding before discarding an expired join token', async () => {
    const { wrapper, state, room } = await mountPendingJoin(new Date(Date.now() - 1000).toISOString())
    getAPIKeyBindingStatus.mockResolvedValue({
      api_key_id: 1001, active_count: 1, ending_count: 0, blocking_count: 1,
      memberships: [membership({ api_key_id: 1001, listing_id: room.id, status: 'active' })],
    })
    await wrapper.get('[data-testid="join-confirm-submit"]').trigger('click')
    await flushPromises()
    expect(state.pendingJoinConfirmation).toBeNull()
    expect(joinListing).not.toHaveBeenCalled()
    expect(createJoinIntent).not.toHaveBeenCalled()
    expect(showSuccess).toHaveBeenCalledWith('已确认当前 Key 正在使用该房间，可在“我的使用”查看。')
    wrapper.unmount()
  })

  it('keeps expired confirmation available for another binding check when status lookup fails', async () => {
    const { wrapper, state } = await mountPendingJoin(new Date(Date.now() - 1000).toISOString())
    getAPIKeyBindingStatus.mockRejectedValue({ status: 503, message: 'binding lookup unavailable' })
    await wrapper.get('[data-testid="join-confirm-submit"]').trigger('click')
    await flushPromises()
    expect(state.pendingJoinConfirmation.intent.token).toBe('signed-join-intent')
    expect(wrapper.get('[data-testid="join-intent-error"]').text()).toContain('binding lookup unavailable')
    expect(wrapper.get('[data-testid="join-confirm-submit"]').attributes('disabled')).toBeUndefined()
    expect(joinListing).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('closes confirmation on an explicit business rejection without treating it as an unknown result', async () => {
    const { wrapper, state } = await mountPendingJoin()
    joinListing.mockRejectedValue({ status: 402, reason: 'ACCOUNT_SHARE_BALANCE_BELOW_MINIMUM' })
    await wrapper.get('[data-testid="join-confirm-submit"]').trigger('click')
    await flushPromises()
    expect(state.pendingJoinConfirmation).toBeNull()
    expect(getAPIKeyBindingStatus).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('余额低于该账号最低要求')
    wrapper.unmount()
  })

  it.each(['ACCOUNT_SHARE_JOIN_INTENT_CONSUMED', 'ACCOUNT_SHARE_MEMBERSHIP_ENDING'])(
    'refreshes current usage and closes retry controls when %s is returned',
    async reason => {
      const { wrapper, state } = await mountPendingJoin()
      const callsBeforeSubmit = listListings.mock.calls.length
      joinListing.mockRejectedValue({ status: 409, reason })
      await wrapper.get('[data-testid="join-confirm-submit"]').trigger('click')
      await flushPromises()
      expect(state.pendingJoinConfirmation).toBeNull()
      expect(getAPIKeyBindingStatus).toHaveBeenCalledWith(1001)
      expect(listListings.mock.calls.length).toBeGreaterThan(callsBeforeSubmit)
      expect(wrapper.text()).toContain('我的使用')
      expect(createJoinIntent).not.toHaveBeenCalled()
      expect(joinListing).toHaveBeenCalledTimes(1)
      wrapper.unmount()
    }
  )


  it.each([
    ['in_flight_requests', 'pending', '', '仍有 2 个请求尚未释放'],
    ['runtime_dependency_unavailable', 'needs_attention', '运行时核对暂时不可用', '运行时核对暂时不可用'],
    ['settlement_error', 'needs_attention', '', '本轮结算未完成'],
  ])('shows the actual %s blocker, wait duration and successful queries while continuing to poll', async (code, status, errorMessage, expectedReason) => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-11T01:01:00Z'))
    const endingListing = listing({
      queue_membership_id: 801,
      queue_api_key_id: 1001,
      queue_status: 'ending',
      queue_ending_operation_id: 'operation-end-801',
      queue_ending_operation_status: status,
      current_paid_until: '2026-07-11T02:00:00Z',
    })
    listListings.mockResolvedValue(paginated([endingListing]))
    getRoomOperation.mockResolvedValue(roomOperation({
      id: 'operation-end-801',
      listing_id: 501,
      action: 'end_membership',
      status: status as AccountShareRoomOperation['status'],
      blocker: { code, in_flight_request_count: 2, checked_at: '2026-07-11T01:01:00Z' },
      error_message: errorMessage,
    }))
    const wrapper = mountView()
    try {
      await flushPromises()
      await openRoomDetails(wrapper, 'usage')
      await vi.advanceTimersByTimeAsync(8_000)
      await flushPromises()
      expect(wrapper.get('[data-testid="membership-ending-state"]').text()).toContain(expectedReason)
      expect(wrapper.text().split(expectedReason)).toHaveLength(2)
      expect(wrapper.text()).not.toContain('下次预付')
      expect(wrapper.text()).not.toContain('退出/结算中')
      expect(wrapper.get('[data-testid="membership-ending-observation"]').text()).toContain('已等待 1 分')
      expect(wrapper.get('[data-testid="membership-ending-observation"]').text()).toContain('最近成功查询')
      await vi.advanceTimersByTimeAsync(8_000)
      expect(getRoomOperation).toHaveBeenCalledTimes(2)
      expect(getAPIKeyBindingStatus).not.toHaveBeenCalled()
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('keeps an invisible membership pending after one operation error and confirms release by Key after repeated errors', async () => {
    vi.useFakeTimers()
    const endingListing = listing({
      queue_membership_id: 801,
      queue_api_key_id: 1001,
      queue_status: 'ending',
      queue_ending_operation_id: 'operation-end-801',
    })
    listListings.mockResolvedValue(paginated([endingListing]))
    getRoomOperation.mockRejectedValue({ status: 404, message: '操作进度暂时不可读取' })
    const wrapper = mountView({ renderDialogs: true })
    try {
      await flushPromises()
      await openRoomDetails(wrapper, 'usage')
      const setupState = (wrapper.vm as any).$?.setupState
      await vi.advanceTimersByTimeAsync(8_000)
      await flushPromises()
      expect(wrapper.get('[data-testid="membership-ending-state"]').text()).toContain('进度查询失败')
      expect(wrapper.get('[data-testid="membership-ending-observation"]').text()).toContain('最近查询失败')
      expect(setupState.pendingMembershipEnds[501]).toBeDefined()
      expect(getAPIKeyBindingStatus).not.toHaveBeenCalled()

      listListings.mockResolvedValue(paginated([]))
      await setupState.loadListings(true)
      expect(setupState.pendingMembershipEnds[501]).toBeDefined()
      await vi.advanceTimersByTimeAsync(8_000)
      await flushPromises()
      expect(getRoomOperation).toHaveBeenCalledTimes(2)
      expect(getAPIKeyBindingStatus).toHaveBeenCalledWith(1001, expect.objectContaining({ signal: expect.any(AbortSignal) }))
      expect(setupState.pendingMembershipEnds[501]).toBeUndefined()
      expect(setupState.pendingReview).toBeNull()
      expect(showSuccess).toHaveBeenCalledWith('已通过 Key 绑定状态确认使用已解除，可在历史记录查看结算结果。')
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('merges missing-operation checks for the same Key and retains active or ending bindings until an authoritative release', async () => {
    vi.useFakeTimers()
    const endings = [501, 502].map((id, index) => listing({
      id, queue_membership_id: 801 + index, queue_api_key_id: 1001, queue_status: 'ending',
    }))
    listListings.mockResolvedValue(paginated(endings))
    getAPIKeyBindingStatus.mockResolvedValue({
      api_key_id: 1001, active_count: 1, ending_count: 1, blocking_count: 2,
      memberships: [
        membership({ id: 801, listing_id: 501, status: 'active' }),
        membership({ id: 802, listing_id: 502, status: 'ending' }),
      ],
    })
    const wrapper = mountView()
    try {
      await flushPromises()
      await openRoomDetails(wrapper, 'usage')
      const setupState = (wrapper.vm as any).$?.setupState
      await vi.advanceTimersByTimeAsync(8_000)
      await flushPromises()
      expect(getAPIKeyBindingStatus).toHaveBeenCalledTimes(1)
      expect(Object.keys(setupState.pendingMembershipEnds)).toHaveLength(2)
      expect(wrapper.text()).toContain('正在通过 Key 绑定状态核对')
      await vi.advanceTimersByTimeAsync(24_000)
      expect(getAPIKeyBindingStatus).toHaveBeenCalledTimes(1)

      getAPIKeyBindingStatus.mockResolvedValue({ api_key_id: 1001, active_count: 0, ending_count: 0, blocking_count: 0, memberships: [] })
      listListings.mockResolvedValue(paginated([]))
      await vi.advanceTimersByTimeAsync(8_000)
      await flushPromises()
      expect(getAPIKeyBindingStatus).toHaveBeenCalledTimes(2)
      expect(Object.keys(setupState.pendingMembershipEnds)).toHaveLength(0)
      expect(getRoomOperation).not.toHaveBeenCalled()
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it.each([
    ['a failed binding query', () => Promise.reject({ status: 503, message: '绑定查询暂不可用' })],
    ['a mismatched Key response', () => Promise.resolve({ api_key_id: 2002, active_count: 0, ending_count: 0, blocking_count: 0, memberships: [] })],
  ])('retains an ending membership after %s and continues retrying without a request flood', async (_label, bindingRequest) => {
    vi.useFakeTimers()
    listListings.mockResolvedValue(paginated([listing({
      queue_membership_id: 801, queue_api_key_id: 1001, queue_status: 'ending',
    })]))
    getAPIKeyBindingStatus.mockImplementation(bindingRequest)
    const wrapper = mountView()
    try {
      await flushPromises()
      await openRoomDetails(wrapper, 'usage')
      const setupState = (wrapper.vm as any).$?.setupState
      await vi.advanceTimersByTimeAsync(8_000)
      await flushPromises()
      expect(setupState.pendingMembershipEnds[501]).toBeDefined()
      expect(wrapper.get('[data-testid="membership-ending-state"]').text()).toContain('Key 绑定核对失败')
      await vi.advanceTimersByTimeAsync(24_000)
      expect(getAPIKeyBindingStatus).toHaveBeenCalledTimes(1)
      await vi.advanceTimersByTimeAsync(8_000)
      expect(getAPIKeyBindingStatus).toHaveBeenCalledTimes(2)
      expect(setupState.pendingMembershipEnds[501]).toBeDefined()
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('checks the authoritative binding for an incomplete operation result instead of claiming settlement completed', async () => {
    vi.useFakeTimers()
    listListings.mockResolvedValue(paginated([listing({
      queue_membership_id: 801, queue_api_key_id: 1001, queue_status: 'ending',
      queue_ending_operation_id: 'operation-end-801',
    })]))
    getRoomOperation.mockResolvedValue(roomOperation({
      id: 'operation-end-801', listing_id: 501, action: 'end_membership', status: 'succeeded', result: {},
    }))
    getAPIKeyBindingStatus.mockResolvedValue({
      api_key_id: 1001, active_count: 0, ending_count: 1, blocking_count: 1,
      memberships: [membership({ status: 'ending', ending_operation_id: 'operation-end-801' })],
    })
    const wrapper = mountView({ renderDialogs: true })
    try {
      await flushPromises()
      await openRoomDetails(wrapper, 'usage')
      await vi.advanceTimersByTimeAsync(8_000)
      await flushPromises()
      const setupState = (wrapper.vm as any).$?.setupState
      expect(getAPIKeyBindingStatus).toHaveBeenCalledTimes(1)
      expect(setupState.pendingMembershipEnds[501]).toBeDefined()
      expect(setupState.pendingReview).toBeNull()
      expect(wrapper.text()).toContain('完成信息不完整')
      expect(showSuccess).not.toHaveBeenCalledWith('退出与结算已完成')
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('retries a failed room operation query automatically and clears the transient error after recovery', async () => {
    vi.useFakeTimers()
    listListings.mockResolvedValue(paginated([listing({ id: 900, owner_user_id: 9 })]))
    getRoomManagementState.mockResolvedValue(roomManagementState({
      lifecycle_status: 'draining', allowed_actions: [], pending_operation_id: 'operation-900',
    }))
    getRoomOperation.mockRejectedValueOnce({ status: 503, message: '进度接口暂不可用' })
      .mockResolvedValue(roomOperation({ status: 'needs_attention', blocker: { code: 'settlement_error' } }))
    const wrapper = mountView({ renderDialogs: true })
    try {
      await flushPromises()
      await wrapper.findAll('button').find(button => button.text() === '我的房间')?.trigger('click')
      await flushPromises()
      await openRoomDetails(wrapper, 'usage')
      await wrapper.get('[data-testid="room-lifecycle-entry"]').trigger('click')
      await flushPromises()
      expect(wrapper.get('[data-testid="room-operation-query-observation"]').text()).toContain('最近查询失败')
      expect(wrapper.text()).toContain('进度接口暂不可用')
      expect(wrapper.get('[data-testid="room-operation-query-observation"]').text()).toContain('系统会继续重试')
      await vi.advanceTimersByTimeAsync(1_500)
      await flushPromises()
      expect(getRoomOperation).toHaveBeenCalledTimes(2)
      expect(wrapper.get('[data-testid="room-operation-query-observation"]').text()).toContain('最近查询成功')
      expect(wrapper.text()).not.toContain('进度接口暂不可用')
      expect(wrapper.get('[data-testid="room-lifecycle-operation"]').text()).toContain('本轮结算未完成')
      await vi.advanceTimersByTimeAsync(1_500)
      expect(getRoomOperation).toHaveBeenCalledTimes(3)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('shows the room query deadline persistently and allows a new round of queries', async () => {
    vi.useFakeTimers()
    listListings.mockResolvedValue(paginated([listing({ id: 900, owner_user_id: 9 })]))
    getRoomManagementState.mockResolvedValue(roomManagementState({
      lifecycle_status: 'draining', allowed_actions: [], pending_operation_id: 'operation-900',
    }))
    getRoomOperation.mockRejectedValue({ status: 503, message: '进度接口暂不可用' })
    const wrapper = mountView({ renderDialogs: true })
    try {
      await flushPromises()
      await wrapper.findAll('button').find(button => button.text() === '我的房间')?.trigger('click')
      await flushPromises()
      await openRoomDetails(wrapper, 'usage')
      await wrapper.get('[data-testid="room-lifecycle-entry"]').trigger('click')
      await flushPromises()
      vi.setSystemTime(Date.now() + 600_000)
      await vi.advanceTimersByTimeAsync(1_500)
      await flushPromises()
      expect(wrapper.get('[data-testid="room-operation-query-stopped"]').text()).toContain('10 分钟自动查询期限')
      const stoppedCalls = getRoomOperation.mock.calls.length
      await vi.advanceTimersByTimeAsync(3_000)
      expect(getRoomOperation).toHaveBeenCalledTimes(stoppedCalls)
      await wrapper.findAll('button').find(button => button.text() === '继续查询')?.trigger('click')
      await flushPromises()
      expect(getRoomOperation).toHaveBeenCalledTimes(stoppedCalls + 1)
      expect(wrapper.find('[data-testid="room-operation-query-stopped"]').exists()).toBe(false)
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

})
