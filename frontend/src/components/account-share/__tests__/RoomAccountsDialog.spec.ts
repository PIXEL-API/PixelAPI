import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AccountShareListing, AccountShareRoomAccount } from '@/api/accountShare'
import type { Account } from '@/types'
import RoomAccountsDialog from '../RoomAccountsDialog.vue'

const { attachRoomAccounts, detachRoomAccounts, listAccounts, listRoomAccounts } = vi.hoisted(() => ({
  attachRoomAccounts: vi.fn(),
  detachRoomAccounts: vi.fn(),
  listAccounts: vi.fn(),
  listRoomAccounts: vi.fn(),
}))

vi.mock('@/api/accountShare', () => ({
  accountShareAPI: {
    listRoomAccounts,
    attachRoomAccounts,
    detachRoomAccounts,
  },
}))

vi.mock('@/api/accounts', () => ({
  accountsAPI: {
    list: listAccounts,
  },
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

const BaseDialogStub = {
  name: 'BaseDialog',
  props: ['show', 'title', 'closeDisabled'],
  emits: ['close'],
  template: `
    <section
      v-if="show"
      data-testid="base-dialog"
      :data-close-disabled="String(closeDisabled)"
    >
      <h2>{{ title }}</h2>
      <button type="button" data-testid="base-dialog-close" @click="$emit('close')">
        force close
      </button>
      <slot />
      <slot name="footer" />
    </section>
  `,
}

function listing(id: number, roomName: string): AccountShareListing {
  return {
    id,
    account_id: id * 10,
    room_name: roomName,
    account_name: `${roomName}主账号`,
    platform: 'openai',
    account_level: 'plus',
    owner_user_id: 9,
    status: 'active',
    seat_limit: 3,
    active_seats: 0,
    rating_count: 0,
    rating_score_sum: 0,
    rating_avg: 0,
    rate_multiplier: 1,
    allowed_models: ['gpt-5.5'],
    per_user_concurrency: 2,
    account_concurrency: 10,
    hourly_rate: 0,
    hourly_fee_waiver_minimum: 0,
    min_balance_required: 0,
    codex_cli_only: true,
    codex_5h_limit_percent: 100,
    codex_7d_limit_percent: 100,
    account_status: 'active',
    account_schedulable: true,
    editing_mine: false,
    created_at: '2026-07-24T00:00:00Z',
    updated_at: '2026-07-24T00:00:00Z',
  }
}

function roomAccount(
  accountID: number,
  name: string,
  overrides: Partial<AccountShareRoomAccount> = {}
): AccountShareRoomAccount {
  return {
    account_id: accountID,
    account_name: name,
    platform: 'openai',
    account_level: 'plus',
    status: 'active',
    schedulable: true,
    current_concurrency: 1,
    priority: 50,
    placement_state: 'active',
    last_used_at: '2026-07-24T01:00:00Z',
    ...overrides,
  }
}

function account(
  accountID: number,
  name: string,
  overrides: Partial<Account> = {}
): Account {
  return {
    id: accountID,
    name,
    platform: 'openai',
    account_level: 'plus',
    type: 'oauth',
    proxy_id: null,
    owner_user_id: 9,
    share_mode: 'private',
    external_placement: { target: 'room', state: 'active', version: 1 },
    concurrency: 10,
    current_concurrency: 0,
    priority: 50,
    status: 'active',
    schedulable: true,
    error_message: null,
    error_since: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: false,
    created_at: '2026-07-24T00:00:00Z',
    updated_at: '2026-07-24T00:00:00Z',
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

function paginatedAccounts(items: Account[], page = 1, pages = 1) {
  return {
    items,
    total: items.length,
    page,
    page_size: 100,
    pages,
  }
}

function mountDialog(room = listing(1, '测试房间')) {
  return mount(RoomAccountsDialog, {
    props: {
      show: true,
      listing: room,
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Icon: true,
        CreateRoomAccountFlow: {
          name: 'CreateRoomAccountFlow',
          props: ['show', 'listing', 'proxies'],
          emits: ['close', 'completed'],
          template: `
            <section v-if="show" data-testid="create-room-account-flow">
              <button type="button" data-testid="complete-room-account-flow" @click="$emit('completed')">
                completed
              </button>
            </section>
          `,
        },
      },
    },
  })
}

describe('RoomAccountsDialog', () => {
  beforeEach(() => {
    attachRoomAccounts.mockReset()
    detachRoomAccounts.mockReset()
    listAccounts.mockReset()
    listRoomAccounts.mockReset()
    listAccounts.mockResolvedValue(paginatedAccounts([]))
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads room members and distinguishes healthy accounts', async () => {
    listRoomAccounts.mockResolvedValueOnce([
      roomAccount(11, '健康账号'),
      roomAccount(12, '暂停账号', { schedulable: false }),
    ])

    const wrapper = mountDialog()
    await flushPromises()

    expect(listRoomAccounts).toHaveBeenCalledWith(1)
    expect(wrapper.text()).toContain('健康账号')
    expect(wrapper.text()).toContain('暂停账号')
    expect(wrapper.text()).toContain('1/2')
    expect(wrapper.text()).toContain('accountShare.roomAccounts.healthy')
    expect(wrapper.text()).toContain('accountShare.roomAccounts.unavailable')
  })

  it('shows the API error without inventing fallback member data', async () => {
    listRoomAccounts.mockRejectedValueOnce(new Error('成员加载失败'))

    const wrapper = mountDialog()
    await flushPromises()

    expect(wrapper.text()).toContain('成员加载失败')
    expect(wrapper.text()).not.toContain('测试房间主账号')
  })

  it('discards a stale request when switching rooms', async () => {
    let resolveFirst!: (accounts: AccountShareRoomAccount[]) => void
    let resolveSecond!: (accounts: AccountShareRoomAccount[]) => void
    listRoomAccounts
      .mockReturnValueOnce(new Promise(resolve => {
        resolveFirst = resolve
      }))
      .mockReturnValueOnce(new Promise(resolve => {
        resolveSecond = resolve
      }))

    const wrapper = mountDialog(listing(1, '旧房间'))
    await flushPromises()
    await wrapper.setProps({ listing: listing(2, '新房间') })
    await flushPromises()

    resolveSecond([roomAccount(22, '新房间账号')])
    await flushPromises()
    expect(wrapper.text()).toContain('新房间账号')

    resolveFirst([roomAccount(11, '旧房间账号')])
    await flushPromises()
    expect(wrapper.text()).toContain('新房间账号')
    expect(wrapper.text()).not.toContain('旧房间账号')
  })

  it('loads every candidate page and only enables matching platform-mode accounts', async () => {
    listRoomAccounts.mockResolvedValueOnce([roomAccount(11, '房间内账号')])
    listAccounts
      .mockResolvedValueOnce(paginatedAccounts([
        account(11, '房间内账号', {
          external_placement: { target: 'room', state: 'active', version: 2 },
        }),
        account(12, '兼容平台模式账号'),
        account(13, '等级不符', { account_level: 'team' }),
      ], 1, 2))
      .mockResolvedValueOnce(paginatedAccounts([
        account(14, '未知等级', { account_level: 'unknown' }),
        account(15, '仅本人账号', {
          external_placement: { target: 'private', state: 'active', version: 3 },
        }),
        account(16, '号主信息缺失', { owner_user_id: null }),
      ], 2, 2))

    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('[data-testid="room-accounts-add-tab"]').trigger('click')

    expect(listAccounts).toHaveBeenNthCalledWith(1, 1, 100, { platform: 'openai' })
    expect(listAccounts).toHaveBeenNthCalledWith(2, 2, 100, { platform: 'openai' })
    expect(wrapper.text()).toContain('兼容平台模式账号')
    expect(wrapper.text()).toContain('等级不符')
    expect(wrapper.text()).toContain('未知等级')
    expect(wrapper.text()).toContain('仅本人账号')
    expect(wrapper.text()).toContain('号主信息缺失')
    expect(wrapper.text()).not.toContain('房间内账号')

    const candidateCheckboxes = wrapper.findAll('input[type="checkbox"]')
    expect(candidateCheckboxes).toHaveLength(5)
    expect(candidateCheckboxes.filter(
      (checkbox) => !(checkbox.element as HTMLInputElement).disabled
    )).toHaveLength(1)
  })

  it('opens the concentrated account creator and refreshes both room views after completion', async () => {
    listRoomAccounts.mockResolvedValue([])
    listAccounts.mockResolvedValue(paginatedAccounts([]))

    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('[data-testid="room-accounts-add-tab"]').trigger('click')
    await wrapper.get('[data-testid="create-compatible-room-account"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="create-room-account-flow"]').exists()).toBe(true)
    await wrapper.get('[data-testid="base-dialog-close"]').trigger('click')
    expect(wrapper.emitted('close')).toBeUndefined()

    const roomCallsBeforeCompletion = listRoomAccounts.mock.calls.length
    const candidateCallsBeforeCompletion = listAccounts.mock.calls.length
    await wrapper.get('[data-testid="complete-room-account-flow"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="create-room-account-flow"]').exists()).toBe(false)
    expect(listRoomAccounts.mock.calls.length).toBeGreaterThan(roomCallsBeforeCompletion)
    expect(listAccounts.mock.calls.length).toBeGreaterThan(candidateCallsBeforeCompletion)
    expect(wrapper.emitted('changed')).toEqual([[
      { operation: 'add', success: 1, failed: 0 },
    ]])
    expect(wrapper.get('[data-testid="room-accounts-members-tab"]').attributes('aria-selected')).toBe('true')
  })

  it('adds selected compatible accounts with a secure batch idempotency key', async () => {
    listRoomAccounts
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([roomAccount(12, '待加入账号')])
    listAccounts.mockResolvedValue(paginatedAccounts([account(12, '待加入账号')]))
    attachRoomAccounts.mockResolvedValueOnce({
      success: 1,
      failed: 0,
      success_ids: [12],
      failed_ids: [],
      results: [{ account_id: 12, success: true }],
    })
    vi.spyOn(globalThis.crypto, 'randomUUID').mockReturnValue(
      '11111111-1111-4111-8111-111111111111'
    )

    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('[data-testid="room-accounts-add-tab"]').trigger('click')
    await wrapper.get('input[type="checkbox"]').setValue(true)
    await wrapper.get('[data-testid="add-selected-room-accounts"]').trigger('click')
    await flushPromises()

    expect(attachRoomAccounts).toHaveBeenCalledWith(1, {
      account_ids: [12],
      idempotency_key: 'room-add-1-11111111-1111-4111-8111-111111111111',
    })
    expect(wrapper.emitted('changed')).toEqual([[
      { operation: 'add', success: 1, failed: 0 },
    ]])
    expect(wrapper.get('[data-testid="room-accounts-operation-summary"]').text())
      .toContain('accountShare.roomAccounts.addSuccess')
  })

  it('reuses the same idempotency key when retrying an uncertain network failure', async () => {
    listRoomAccounts
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([roomAccount(18, '重试账号')])
    listAccounts.mockResolvedValue(paginatedAccounts([account(18, '重试账号')]))
    attachRoomAccounts
      .mockRejectedValueOnce(new Error('网络中断'))
      .mockResolvedValueOnce({
        success: 1,
        failed: 0,
        success_ids: [18],
        failed_ids: [],
        results: [{ account_id: 18, success: true }],
      })
    const randomUUID = vi.spyOn(globalThis.crypto, 'randomUUID').mockReturnValue(
      '44444444-4444-4444-8444-444444444444'
    )

    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('[data-testid="room-accounts-add-tab"]').trigger('click')
    await wrapper.get('input[type="checkbox"]').setValue(true)
    await wrapper.get('[data-testid="add-selected-room-accounts"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="add-selected-room-accounts"]').trigger('click')
    await flushPromises()

    expect(randomUUID).toHaveBeenCalledTimes(1)
    expect(attachRoomAccounts).toHaveBeenCalledTimes(2)
    expect(attachRoomAccounts.mock.calls[0][1].idempotency_key)
      .toBe(attachRoomAccounts.mock.calls[1][1].idempotency_key)
  })

  it('blocks every close path and duplicate submission while an operation is running', async () => {
    let resolveAttach!: (result: {
      success: number
      failed: number
      success_ids: number[]
      failed_ids: number[]
      results: Array<{ account_id: number; success: boolean; error?: string }>
    }) => void
    listRoomAccounts.mockResolvedValueOnce([])
    listAccounts.mockResolvedValue(paginatedAccounts([account(19, '操作中账号')]))
    attachRoomAccounts.mockReturnValueOnce(new Promise(resolve => {
      resolveAttach = resolve
    }))
    vi.spyOn(globalThis.crypto, 'randomUUID').mockReturnValue(
      '55555555-5555-4555-8555-555555555555'
    )

    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('[data-testid="room-accounts-add-tab"]').trigger('click')
    await wrapper.get('input[type="checkbox"]').setValue(true)

    const submitButton = wrapper.get('[data-testid="add-selected-room-accounts"]')
    await submitButton.trigger('click')

    expect(wrapper.get('[data-testid="base-dialog"]').attributes('data-close-disabled'))
      .toBe('true')
    expect(wrapper.get('[data-testid="close-room-accounts-dialog"]').attributes('disabled'))
      .toBeDefined()

    await wrapper.get('[data-testid="base-dialog-close"]').trigger('click')
    await submitButton.trigger('click')

    expect(wrapper.emitted('close')).toBeUndefined()
    expect(attachRoomAccounts).toHaveBeenCalledTimes(1)

    resolveAttach({
      success: 0,
      failed: 1,
      success_ids: [],
      failed_ids: [19],
      results: [{ account_id: 19, success: false, error: '账号仍有运行中请求' }],
    })
    await flushPromises()

    expect(wrapper.get('[data-testid="base-dialog"]').attributes('data-close-disabled'))
      .toBe('false')
    await wrapper.get('[data-testid="base-dialog-close"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('detaches selected members without changing their account mode', async () => {
    listRoomAccounts
      .mockResolvedValueOnce([roomAccount(21, '待退出账号')])
      .mockResolvedValueOnce([])
    listAccounts.mockResolvedValue(paginatedAccounts([account(21, '待退出账号')]))
    detachRoomAccounts.mockResolvedValueOnce({
      success: 1,
      failed: 0,
      success_ids: [21],
      failed_ids: [],
      results: [{ account_id: 21, success: true }],
    })
    vi.spyOn(globalThis.crypto, 'randomUUID').mockReturnValue(
      '22222222-2222-4222-8222-222222222222'
    )

    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('input[type="checkbox"]').setValue(true)
    await wrapper.get('[data-testid="remove-selected-room-accounts"]').trigger('click')
    await flushPromises()

    expect(detachRoomAccounts).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="room-account-remove-confirmation"]').text())
      .toContain('这会移出房间的最后一个账号')
    await wrapper.get('[data-testid="confirm-remove-room-accounts"]').trigger('click')
    await flushPromises()

    expect(detachRoomAccounts).toHaveBeenCalledWith(1, {
      account_ids: [21],
      idempotency_key: 'room-remove-1-22222222-2222-4222-8222-222222222222',
    })
    expect(wrapper.emitted('changed')).toEqual([[
      { operation: 'remove', success: 1, failed: 0 },
    ]])
    expect(wrapper.text()).toContain('accountShare.roomAccounts.removeHint')
  })

  it('reports item-level failures after a partial removal and refreshes real state', async () => {
    listRoomAccounts
      .mockResolvedValueOnce([
        roomAccount(31, '成功账号'),
        roomAccount(32, '忙碌账号'),
      ])
      .mockResolvedValueOnce([roomAccount(32, '忙碌账号')])
    detachRoomAccounts.mockResolvedValueOnce({
      success: 1,
      failed: 1,
      success_ids: [31],
      failed_ids: [32],
      results: [
        { account_id: 31, success: true },
        { account_id: 32, success: false, error: '账号仍有运行中请求' },
      ],
    })
    vi.spyOn(globalThis.crypto, 'randomUUID').mockReturnValue(
      '33333333-3333-4333-8333-333333333333'
    )

    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('[data-testid="select-all-room-members"]').trigger('click')
    await wrapper.get('[data-testid="remove-selected-room-accounts"]').trigger('click')
    expect(detachRoomAccounts).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="confirm-remove-room-accounts"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="room-accounts-operation-summary"]').text())
      .toContain('accountShare.roomAccounts.removePartial')
    expect(wrapper.text()).toContain('忙碌账号')
    expect(wrapper.text()).toContain('账号仍有运行中请求')
    expect(listRoomAccounts).toHaveBeenCalledTimes(2)
  })
})
