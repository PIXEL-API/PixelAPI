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
  listMembershipQueue,
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
  beginListingEdit,
  releaseListingEdit,
  exchangeOpenAICode,
  exchangeAnthropicCode,
  submitReview,
  listProxies,
  createRoom,
  recommendListings,
  getRecommendationUsageProfile,
  listOwnerReviews,
  listAccounts,
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
  listMembershipQueue: vi.fn(),
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
  beginListingEdit: vi.fn(),
  releaseListingEdit: vi.fn(),
  exchangeOpenAICode: vi.fn(),
  exchangeAnthropicCode: vi.fn(),
  submitReview: vi.fn(),
  listProxies: vi.fn(),
  createRoom: vi.fn(),
  recommendListings: vi.fn(),
  getRecommendationUsageProfile: vi.fn(),
  listOwnerReviews: vi.fn(),
  listAccounts: vi.fn(),
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
      listMembershipQueue,
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
      beginListingEdit,
      releaseListingEdit,
      exchangeOpenAICode,
      exchangeAnthropicCode,
      submitReview,
      listProxies,
      createRoom,
      recommendListings,
      getRecommendationUsageProfile,
      listOwnerReviews,
    },
  }
})

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
  props: ['show'],
  template: '<section v-if="show"><slot /><slot name="footer" /></section>',
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
    editing_mine: false,
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
    accept_queue: false,
    queue_may_be_required: false,
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
    queued_membership_count: 0,
    ending_membership_count: 0,
    in_flight_request_count: 0,
    pending_billing_intent_count: 0,
    synchronous_billing_pending_count: 0,
    valid_edit_session: false,
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
    queued_membership_count: 0,
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
  }
}

function mountView(options: { renderDialogs?: boolean; attachTo?: HTMLElement } = {}) {
  return mount(AccountShareView, {
    ...(options.attachTo ? { attachTo: options.attachTo } : {}),
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        BaseDialog: options.renderDialogs ? BaseDialogWithSlotsStub : true,
        ConfirmDialog: true,
        Icon: true,
        AccountStatsModal: true,
        AccountTestModal: true,
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

describe('AccountShareView async snapshots and mode keys', () => {
  beforeEach(() => {
    localStorage.clear()
    listListings.mockReset()
    listMembershipHistory.mockReset()
    getMySpendSummary.mockReset()
    listMembershipQueue.mockReset()
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
    beginListingEdit.mockReset()
    releaseListingEdit.mockReset()
    exchangeOpenAICode.mockReset()
    exchangeAnthropicCode.mockReset()
    submitReview.mockReset()
    listProxies.mockReset()
    createRoom.mockReset()
    recommendListings.mockReset()
    getRecommendationUsageProfile.mockReset()
    listOwnerReviews.mockReset()
    listAccounts.mockReset()
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
    listAccounts.mockResolvedValue(paginated([]))
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
    beginListingEdit.mockResolvedValue(listing({
      row_version: 7,
      status: 'paused',
      active_seats: 0,
      editing_mine: true,
      edit_session_id: 'edit-session',
    }))
    releaseListingEdit.mockResolvedValue(listing({
      status: 'paused',
      active_seats: 0,
    }))
    exchangeOpenAICode.mockResolvedValue({})
    exchangeAnthropicCode.mockResolvedValue({})
    submitReview.mockResolvedValue(undefined)
    listMembershipQueue.mockResolvedValue([])
    getAPIKeyBindingStatus.mockResolvedValue({
      api_key_id: 1001,
      active_count: 0,
      queued_count: 0,
      ending_count: 0,
      blocking_count: 0,
      memberships: [],
    })
    getListing.mockImplementation((id: number) => Promise.resolve(listing({ id })))
    listKeys.mockResolvedValue(paginated([]))
    fetchPublicSettings.mockResolvedValue(publicSettings)
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
    const setupState = (wrapper.vm as any).$?.setupState

    expect(getAPIKeyBindingStatus).toHaveBeenCalledWith(1001)
    expect(setupState.keyResolutionAllClear).toBe(false)
    expect(wrapper.text()).not.toContain('关联已全部解除')
    expect(wrapper.text()).toContain('退出/结算中')
    expect(wrapper.get('[data-testid="membership-ending-state"]').text()).toContain('结算待处理')
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

  it('shows owner result totals and loads the remaining owner rooms on demand', async () => {
    listListings.mockImplementation((page: number, pageSize: number, filters?: { owner_user_id?: number }) => {
      if (!filters?.owner_user_id) return Promise.resolve(paginated([], 1, 1, 0, pageSize))
      if (page === 1) {
        return Promise.resolve(paginated(
          [listing({ id: 711, owner_user_id: 700, room_name: '第一页房间' })],
          1,
          2,
          2,
          pageSize
        ))
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

    expect(wrapper.text()).toContain('已显示 1/2')
    const loadMore = wrapper.findAll('button').find(button => button.text().includes('继续加载账号'))
    expect(loadMore).toBeDefined()
    await loadMore?.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('第一页房间')
    expect(wrapper.text()).toContain('第二页房间')
    expect(wrapper.text()).toContain('已显示 2/2')
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

  it('removes a cached mode API Key when it expires while the page remains open', async () => {
    vi.useFakeTimers()
    let wrapper: ReturnType<typeof mountView> | undefined
    try {
      const consumerListing = listing({ id: 515, room_name: '等待 Key 过期的房间' })
      listListings.mockResolvedValue(paginated([consumerListing]))
      wrapper = mountView()
      await flushPromises()

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
    const joinButton = wrapper.findAll('button').find(button => button.text() === '加入使用')
    await joinButton?.trigger('click')
    await flushPromises()

    expect(createJoinIntent).toHaveBeenCalledWith(consumerListing.id, {
      api_key_id: 1001,
      idle_timeout_minutes: 10,
      accept_queue: false,
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
      accept_queue: false,
    })
    wrapper.unmount()
  })

  it('disables an expired join confirmation when the shared clock advances', async () => {
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

      expect(submitButton.attributes('disabled')).toBeDefined()
      expect(joinListing).not.toHaveBeenCalled()
    } finally {
      wrapper?.unmount()
      vi.clearAllTimers()
      vi.useRealTimers()
    }
  })

  it('requires explicit queue consent, reissues the intent, and blocks every close path while submitting', async () => {
    const consumerListing = listing({ room_name: '需要预约的房间' })
    const initialIntent = joinIntent(consumerListing, {
      queue_may_be_required: true,
    })
    const queueAcceptedIntent = {
      ...initialIntent,
      token: 'queue-accepted-token',
      accept_queue: true,
    }
    listListings.mockResolvedValue(paginated([consumerListing]))
    listKeys.mockImplementation((_page: number, _pageSize: number, filters: { group_id: number }) =>
      Promise.resolve(filters.group_id === 101
        ? paginated([apiKey(1001, 101, '预约 Key')])
        : paginated([]))
    )
    createJoinIntent
      .mockResolvedValueOnce(initialIntent)
      .mockResolvedValueOnce(queueAcceptedIntent)
    let resolveJoin!: (membership: Record<string, unknown>) => void
    joinListing.mockReturnValue(new Promise(resolve => {
      resolveJoin = resolve
    }))

    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === '加入使用')?.trigger('click')
    await flushPromises()

    const submitButton = wrapper.get('[data-testid="join-confirm-submit"]')
    expect(submitButton.attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="join-accept-queue"]').setValue(true)
    await flushPromises()

    expect(createJoinIntent).toHaveBeenNthCalledWith(2, consumerListing.id, {
      api_key_id: 1001,
      idle_timeout_minutes: 10,
      accept_queue: true,
    })
    expect(submitButton.attributes('disabled')).toBeUndefined()

    await submitButton.trigger('click')
    await nextTick()
    const setupState = (wrapper.vm as any).$?.setupState
    setupState.closeJoinConfirmation()
    await nextTick()
    expect(wrapper.find('[data-testid="join-confirmation"]').exists()).toBe(true)
    expect(joinListing).toHaveBeenCalledWith(consumerListing.id, expect.objectContaining({
      intent_token: 'queue-accepted-token',
      accept_queue: true,
    }))

    resolveJoin({
      id: 802,
      listing_id: consumerListing.id,
      account_id: consumerListing.account_id,
      consumer_user_id: 9,
      api_key_id: 1001,
      status: 'queued',
      queue_rank: 1,
      idle_timeout_minutes: 10,
      joined_at: '2026-07-11T01:00:00Z',
      created_at: '2026-07-11T01:00:00Z',
      updated_at: '2026-07-11T01:00:00Z',
    })
    await flushPromises()
    wrapper.unmount()
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
    expect(setupState.createSourceMode).toBe('existing')
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
    expect(wrapper.text()).toContain('我的共享房间')
    expect(wrapper.text()).toContain('可调度账号 2/3')
    expect(wrapper.text()).toContain('席位 1/3')
    expect(wrapper.text()).not.toContain('消费者 1/3')
    expect(wrapper.text()).toContain('可用并发')
    expect(wrapper.text()).not.toContain('实时容量')

    const roomCountButton = wrapper.findAll('button').find(button => button.text().includes('管理账号'))
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
    { status: 'paused' as const, label: '已暂停' },
    { status: 'validating' as const, label: '恢复校验中' },
    { status: 'draining' as const, label: '安全排空中' },
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

  it('submits an active empty room owner edit with optimistic versioning and no administrator override fields', async () => {
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
      row_version: activeRoom.row_version,
      lifecycle_status: 'active',
      active_seats: 0,
      allowed_actions: ['drain', 'delete'],
    }))
    beginListingEdit.mockResolvedValue({
      ...activeRoom,
      editing_mine: true,
      edit_session_id: 'owner-edit-session',
    })
    updateListing.mockResolvedValue({
      ...activeRoom,
      row_version: 13,
    })

    const wrapper = mountView({ renderDialogs: true })
    await flushPromises()
    const setupState = (wrapper.vm as any).$?.setupState
    setupState.requestOpenConfigEdit(activeRoom)
    await flushPromises()
    expect(beginListingEdit).toHaveBeenCalledWith(902, {
      session_id: undefined,
    }, expect.stringMatching(/^account-share-edit-begin-902-/))

    setupState.editReason = '根据近期使用情况调整房间参数'
    await setupState.saveConfigEdit()
    await flushPromises()

    expect(updateListing).toHaveBeenCalledTimes(1)
    const payload = updateListing.mock.calls[0][1]
    expect(payload).toEqual(expect.objectContaining({
      expected_version: 12,
      edit_session_id: 'owner-edit-session',
      seat_limit: 3,
      reason: '根据近期使用情况调整房间参数',
    }))
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

    expect(beginListingEdit).not.toHaveBeenCalled()
    expect(setupState.showConfigEditDialog).toBe(true)
    expect(setupState.editConsumerProtected).toBe(true)
    expect(setupState.editSessionID).toBe('')
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
    beginListingEdit.mockResolvedValue({
      ...activeRoom,
      editing_mine: true,
      edit_session_id: 'admin-force-session',
    })
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
    expect(beginListingEdit).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="force-edit-reason"]').setValue('紧急修正错误价格')
    expect(forceButton.attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="force-edit-confirmed"]').setValue(true)
    expect(forceButton.attributes('disabled')).toBeUndefined()
    await forceButton.trigger('click')
    await flushPromises()

    expect(beginListingEdit).toHaveBeenCalledWith(903, {
      session_id: undefined,
      force: true,
    }, expect.stringMatching(/^account-share-edit-begin-903-/))
    await setupState.saveConfigEdit()
    await flushPromises()

    expect(updateListing).toHaveBeenCalledWith(903, expect.objectContaining({
      expected_version: 15,
      edit_session_id: 'admin-force-session',
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
    beginListingEdit.mockResolvedValue({
      ...pausedRoom,
      editing_mine: true,
      edit_session_id: 'conflicted-session',
    })
    releaseListingEdit.mockResolvedValue({
      ...pausedRoom,
      editing_mine: false,
      edit_session_id: undefined,
    })
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

    expect(releaseListingEdit).toHaveBeenCalledWith(
      904,
      'conflicted-session',
      expect.stringMatching(/^account-share-edit-release-904-/)
    )
    expect(listListings).toHaveBeenCalled()
    expect(beginListingEdit).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('ignores a failed renewal from an older edit session after a newer session is active', async () => {
    let rejectOlderRenewal!: (reason: unknown) => void
    beginListingEdit.mockReturnValueOnce(new Promise((_resolve, reject) => {
      rejectOlderRenewal = reject
    }))

    const wrapper = mountView()
    await flushPromises()
    const setupState = (wrapper.vm as any).$?.setupState
    setupState.editingConfigListing = listing({
      id: 930,
      owner_user_id: 9,
      status: 'paused',
      editing_mine: true,
      edit_session_id: 'older-session',
    })
    setupState.editSessionID = 'older-session'
    const olderRenewal = setupState.renewConfigEditSession()
    await nextTick()

    setupState.resetConfigEditState()
    setupState.editingConfigListing = listing({
      id: 931,
      owner_user_id: 9,
      status: 'paused',
      editing_mine: true,
      edit_session_id: 'newer-session',
    })
    setupState.editSessionID = 'newer-session'
    setupState.showConfigEditDialog = true

    rejectOlderRenewal(new Error('旧续期失败'))
    await olderRenewal
    await flushPromises()

    expect(setupState.editingConfigListing.id).toBe(931)
    expect(setupState.editSessionID).toBe('newer-session')
    expect(setupState.showConfigEditDialog).toBe(true)
    expect(setupState.editErrorMessage).toBe('')
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
    await wrapper.findAll('button').find(button => button.text().includes('消费记录'))?.trigger('click')
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
    await wrapper.findAll('button').find(button => button.text().includes('消费记录'))?.trigger('click')
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
    await wrapper.findAll('button').find(button => button.text().includes('消费记录'))?.trigger('click')
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
    await wrapper.findAll('button').find(button => button.text().includes('消费记录'))?.trigger('click')
    await nextTick()
    await wrapper.findAll('button').find(button => button.text().trim() === '全部')?.trigger('click')
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
    await wrapper.findAll('button').find(button => button.text().includes('消费记录'))?.trigger('click')
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
    await wrapper.findAll('button').find(button => button.text().includes('已删除房间'))?.trigger('click')
    await flushPromises()

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
    await wrapper.findAll('button').find(button => button.text().includes('已删除房间'))?.trigger('click')
    await flushPromises()

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
    await wrapper.findAll('button').find(button => button.text().includes('已删除房间'))?.trigger('click')
    await flushPromises()

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
    await wrapper.findAll('button').find(button => button.text().includes('已删除房间'))?.trigger('click')
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
    const roomCountButton = wrapper.findAll('button').find(button =>
      button.text().includes('管理账号')
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
      expect(wrapper.get('[data-testid="membership-ending-state"]').text()).toContain('退出/结算中')
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

      expect(wrapper.get('[data-testid="membership-ending-state"]').text()).toContain('退出/结算中')
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

  it('removes a queued reservation directly from the visible action button', async () => {
    const queuedListing = listing({
      id: 910,
      current_membership_id: undefined,
      queue_membership_id: 63368,
      queue_api_key_id: 23185,
      queue_api_key_name: 'gpt',
      queue_rank: 1,
      queue_status: 'queued',
    })
    listListings.mockResolvedValue(paginated([queuedListing]))
    endMembership.mockResolvedValue(membership({
      id: 63368,
      listing_id: queuedListing.id,
      api_key_id: 23185,
      status: 'ended',
      ended_reason: 'manual',
    }))

    const wrapper = mountView()
    await flushPromises()
    const removeButton = wrapper.get('button.membership-end-button')

    await removeButton.trigger('click')
    await flushPromises()

    expect(endMembership).toHaveBeenCalledWith(63368)
    expect(wrapper.text()).not.toContain('确认将该账号')
    wrapper.unmount()
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

    const button = wrapper.findAll('button').find(item => item.text().includes('退出结算处理中'))
    expect(button).toBeDefined()
    expect(button?.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('结算完成后才能重新加入或排队')
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
      button.text().includes('我的账号')
    )
    await ownerFilterButton?.trigger('click')
    await flushPromises()
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
    await wrapper.findAll('button').find(button => button.text().includes('我的账号'))?.trigger('click')
    await flushPromises()
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
    await wrapper.findAll('button').find(button => button.text().includes('我的账号'))?.trigger('click')
    await flushPromises()
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
    expect(wrapper.get('[data-testid="room-lifecycle-deleted"]').text()).toContain('房间已软删除')
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
      await wrapper.findAll('button').find(button => button.text().includes('我的账号'))?.trigger('click')
      await flushPromises()
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
    expect(guideText).toContain('成员上限由房主设置')
    expect(guideText).toContain('最低 1 人、最高 30 人')
    expect(guideText).toContain('房主自用不占消费者名额')
    expect(guideText).toContain('删除房间')
    expect(guideText).toContain('软删除')
    expect(guideText).toContain('删除时保存的精确房间条款快照')
    expect(guideText).toContain('管理员最高处理权限不向房主开放')
    expect(guideText).toContain('每次加入/使用记录按 membership 独立留档')

    wrapper.unmount()
  })
})
