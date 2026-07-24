import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { AccountShareListing, AccountShareRoomAccount } from '@/api/accountShare'
import RoomAccountsDialog from '../RoomAccountsDialog.vue'

const { listRoomAccounts } = vi.hoisted(() => ({
  listRoomAccounts: vi.fn(),
}))

vi.mock('@/api/accountShare', () => ({
  accountShareAPI: {
    listRoomAccounts,
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
  props: ['show', 'title'],
  template: '<section v-if="show"><h2>{{ title }}</h2><slot /><slot name="footer" /></section>',
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
      },
    },
  })
}

describe('RoomAccountsDialog', () => {
  beforeEach(() => {
    listRoomAccounts.mockReset()
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
})
