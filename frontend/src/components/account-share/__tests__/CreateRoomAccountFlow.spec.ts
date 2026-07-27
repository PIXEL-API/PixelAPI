import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AccountShareListing } from '@/api/accountShare'
import type { Account } from '@/types'
import CreateRoomAccountFlow from '../CreateRoomAccountFlow.vue'

const {
  attachRoomAccounts,
  convertAccountExternalPlacement,
  listAccounts,
} = vi.hoisted(() => ({
  attachRoomAccounts: vi.fn(),
  convertAccountExternalPlacement: vi.fn(),
  listAccounts: vi.fn(),
}))

vi.mock('@/api/accountShare', () => ({
  accountShareAPI: {
    attachRoomAccounts,
    convertAccountExternalPlacement,
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
    <section v-if="show" data-testid="base-dialog" :data-close-disabled="String(closeDisabled)">
      <button type="button" data-testid="base-dialog-force-close" @click="$emit('close')">close</button>
      <slot />
      <slot name="footer" />
    </section>
  `,
}

const CreateAccountModalStub = {
  name: 'CreateAccountModal',
  props: [
    'show',
    'title',
    'initialPlatform',
    'lockPlatform',
    'initialAccountLevel',
    'lockAccountLevel',
  ],
  emits: ['created', 'close'],
  template: '<section v-if="show" data-testid="create-account-modal-stub"></section>',
}

function room(): AccountShareListing {
  return {
    id: 71,
    row_version: 3,
    current_revision_id: 5,
    account_id: 700,
    room_name: '集中创建房间',
    account_name: '集中创建房间',
    platform: 'openai',
    account_level: 'plus',
    owner_user_id: 9,
    status: 'active',
    seat_limit: 15,
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
    editing_mine: false,
    created_at: '2026-07-24T00:00:00Z',
    updated_at: '2026-07-24T00:00:00Z',
  }
}

function account(id = 91): Account {
  return {
    id,
    name: `新账号 ${id}`,
    platform: 'openai',
    account_level: 'plus',
    type: 'oauth',
    proxy_id: null,
    owner_user_id: 9,
    share_mode: 'private',
    concurrency: 10,
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
  }
}

function accountPage(items: Account[]) {
  return {
    items,
    total: items.length,
    page: 1,
    page_size: 100,
    pages: 1,
  }
}

function mountFlow() {
  return mount(CreateRoomAccountFlow, {
    props: {
      show: true,
      listing: room(),
      proxies: [],
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        CreateAccountModal: CreateAccountModalStub,
        Icon: true,
      },
    },
  })
}

async function emitCreated(wrapper: ReturnType<typeof mountFlow>, payload?: Account[]): Promise<void> {
  wrapper.getComponent({ name: 'CreateAccountModal' }).vm.$emit('created', payload)
  await flushPromises()
}

describe('CreateRoomAccountFlow', () => {
  beforeEach(() => {
    attachRoomAccounts.mockReset()
    convertAccountExternalPlacement.mockReset()
    listAccounts.mockReset()
    listAccounts.mockResolvedValue(accountPage([]))
    convertAccountExternalPlacement.mockResolvedValue({
      account_id: 91,
      previous: null,
      current: { target: 'room', room_id: 71, state: 'active', version: 1 },
      unchanged: false,
    })
    attachRoomAccounts.mockResolvedValue({
      success: 1,
      failed: 0,
      success_ids: [91],
      failed_ids: [],
      results: [{ account_id: 91, success: true }],
    })
    vi.spyOn(globalThis.crypto, 'randomUUID')
      .mockReturnValueOnce('11111111-1111-4111-8111-111111111111')
      .mockReturnValueOnce('22222222-2222-4222-8222-222222222222')
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('asks before abandoning an unfinished account creation form', async () => {
    const wrapper = mountFlow()
    await flushPromises()

    wrapper.getComponent({ name: 'CreateAccountModal' }).vm.$emit('close')
    await nextTick()

    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.text()).toContain('当前账号信息尚未提交')
    expect(wrapper.text()).toContain('OAuth 授权进度不会保留')

    await wrapper.get('[data-testid="continue-create-room-account"]').trigger('click')
    expect(wrapper.find('[data-testid="discard-create-room-account"]').exists()).toBe(false)

    wrapper.getComponent({ name: 'CreateAccountModal' }).vm.$emit('close')
    await nextTick()
    await wrapper.get('[data-testid="discard-create-room-account"]').trigger('click')

    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('creates, converts, and attaches one compatible account as a single guided flow', async () => {
    const createdAccount = account()
    const wrapper = mountFlow()
    await flushPromises()

    expect(wrapper.find('[data-testid="create-account-modal-stub"]').exists()).toBe(true)
    await emitCreated(wrapper, [createdAccount])

    expect(convertAccountExternalPlacement).toHaveBeenCalledWith(91, {
      target: 'room',
      idempotency_key: 'room-account-convert-71-91-11111111-1111-4111-8111-111111111111',
    })
    expect(attachRoomAccounts).toHaveBeenCalledWith(71, {
      account_ids: [91],
      idempotency_key: 'room-account-attach-71-91-22222222-2222-4222-8222-222222222222',
    })
    expect(wrapper.find('[data-testid="create-room-account-completed"]').exists()).toBe(true)
    expect(wrapper.emitted('completed')).toEqual([[{ accountID: 91 }]])
  })

  it('does not attach after conversion failure and reuses the conversion key on retry', async () => {
    convertAccountExternalPlacement
      .mockRejectedValueOnce(new Error('转换失败'))
      .mockResolvedValueOnce({ account_id: 91 })
    const wrapper = mountFlow()
    await flushPromises()
    await emitCreated(wrapper, [account()])

    expect(wrapper.find('[data-testid="create-room-account-error"]').exists()).toBe(true)
    expect(attachRoomAccounts).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="retry-room-account-conversion"]').trigger('click')
    await flushPromises()

    expect(convertAccountExternalPlacement).toHaveBeenCalledTimes(2)
    expect(convertAccountExternalPlacement.mock.calls[0][1].idempotency_key)
      .toBe(convertAccountExternalPlacement.mock.calls[1][1].idempotency_key)
    expect(attachRoomAccounts).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('completed')).toEqual([[{ accountID: 91 }]])
  })

  it('retries only attach and reuses its idempotency key after an uncertain failure', async () => {
    attachRoomAccounts
      .mockRejectedValueOnce(new Error('网络状态未知'))
      .mockResolvedValueOnce({
        success: 1,
        failed: 0,
        results: [{ account_id: 91, success: true }],
      })
    const wrapper = mountFlow()
    await flushPromises()
    await emitCreated(wrapper, [account()])

    expect(wrapper.find('[data-testid="retry-room-account-attach"]').exists()).toBe(true)
    await wrapper.get('[data-testid="retry-room-account-attach"]').trigger('click')
    await flushPromises()

    expect(convertAccountExternalPlacement).toHaveBeenCalledTimes(1)
    expect(attachRoomAccounts).toHaveBeenCalledTimes(2)
    expect(attachRoomAccounts.mock.calls[0][1].idempotency_key)
      .toBe(attachRoomAccounts.mock.calls[1][1].idempotency_key)
    expect(wrapper.emitted('completed')).toEqual([[{ accountID: 91 }]])
  })

  it('identifies the created account by the before-and-after snapshot when no payload is emitted', async () => {
    const existing = account(80)
    const created = account(91)
    listAccounts
      .mockResolvedValueOnce(accountPage([existing]))
      .mockResolvedValueOnce(accountPage([existing, created]))
    const wrapper = mountFlow()
    await flushPromises()
    await emitCreated(wrapper)

    expect(convertAccountExternalPlacement).toHaveBeenCalledWith(
      created.id,
      expect.objectContaining({
        target: 'room',
        idempotency_key: expect.stringMatching(/^room-account-convert-71-91-/),
      })
    )
    expect(wrapper.emitted('completed')).toEqual([[{ accountID: created.id }]])
  })

  it('ignores every close and duplicate-created path while conversion is running', async () => {
    let resolveConversion!: (value: Record<string, unknown>) => void
    convertAccountExternalPlacement.mockReturnValue(new Promise(resolve => {
      resolveConversion = resolve
    }))
    const wrapper = mountFlow()
    await flushPromises()
    wrapper.getComponent({ name: 'CreateAccountModal' }).vm.$emit('created', [account()])
    await nextTick()

    expect(wrapper.get('[data-testid="base-dialog"]').attributes('data-close-disabled')).toBe('true')
    await wrapper.get('[data-testid="base-dialog-force-close"]').trigger('click')
    const setupState = (wrapper.vm as any).$?.setupState
    await setupState.handleAccountCreated([account()])
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(convertAccountExternalPlacement).toHaveBeenCalledTimes(1)

    resolveConversion({ account_id: 91 })
    await flushPromises()
    expect(wrapper.emitted('completed')).toEqual([[{ accountID: 91 }]])
  })

  it('ignores a stale conversion result after the parent switches to another room', async () => {
    let resolveConversion!: (value: Record<string, unknown>) => void
    convertAccountExternalPlacement.mockReturnValue(new Promise(resolve => {
      resolveConversion = resolve
    }))
    const wrapper = mountFlow()
    await flushPromises()
    wrapper.getComponent({ name: 'CreateAccountModal' }).vm.$emit('created', [account()])
    await nextTick()

    await wrapper.setProps({
      listing: {
        ...room(),
        id: 72,
        room_name: '另一个房间',
      },
    })
    await flushPromises()
    resolveConversion({ account_id: 91 })
    await flushPromises()

    expect(convertAccountExternalPlacement).toHaveBeenCalledTimes(1)
    expect(attachRoomAccounts).not.toHaveBeenCalled()
    expect(wrapper.emitted('completed')).toBeUndefined()
  })
})
