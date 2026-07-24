import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ExternalPlacementDialog from '../ExternalPlacementDialog.vue'
import type { Account } from '@/types'

const { convertPlacementMock, listListingsMock } = vi.hoisted(() => ({
  convertPlacementMock: vi.fn(),
  listListingsMock: vi.fn()
}))

vi.mock('@/api/accountShare', () => ({
  accountShareAPI: {
    convertAccountExternalPlacement: convertPlacementMock,
    listListings: listListingsMock
  }
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (_key: string, arg?: string | Record<string, unknown>, fallback?: string) => (
        typeof arg === 'string' ? arg : fallback ?? _key
      )
    })
  }
})

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: {
    show: { type: Boolean, default: false }
  },
  emits: ['close'],
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

function buildAccount(overrides: Partial<Account> = {}): Account {
  return {
    id: 17,
    name: '号主账号',
    platform: 'openai',
    account_level: 'plus',
    type: 'oauth',
    proxy_id: 5,
    concurrency: 2,
    priority: 10,
    rate_multiplier: 1,
    status: 'active',
    error_message: null,
    error_since: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: false,
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    created_at: '2026-07-24T00:00:00Z',
    updated_at: '2026-07-24T00:00:00Z',
    ...overrides
  }
}

function mountDialog(account = buildAccount()) {
  return mount(ExternalPlacementDialog, {
    props: {
      show: true,
      account,
      ownerUserId: 88
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Icon: true
      }
    }
  })
}

describe('ExternalPlacementDialog', () => {
  beforeEach(() => {
    convertPlacementMock.mockReset()
    listListingsMock.mockReset()
    listListingsMock.mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 100,
      pages: 0
    })
    vi.spyOn(globalThis.crypto, 'randomUUID').mockReturnValue(
      '11111111-1111-4111-8111-111111111111'
    )
  })

  it('复用同一幂等键重试失败但意图未改变的转换', async () => {
    convertPlacementMock
      .mockRejectedValueOnce({ message: '网络暂时不可用' })
      .mockResolvedValueOnce({
        account_id: 17,
        previous: null,
        current: {
          target: 'public_pool',
          state: 'active',
          version: 1
        },
        unchanged: false
      })

    const wrapper = mountDialog()
    await wrapper.get('[data-testid="placement-target-public_pool"]').setValue()

    const submit = wrapper.get('[data-testid="convert-external-placement"]')
    await submit.trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="placement-submit-error"]').text()).toContain('网络暂时不可用')
    expect(wrapper.emitted('converted')).toBeUndefined()

    await submit.trigger('click')
    await flushPromises()

    expect(convertPlacementMock).toHaveBeenCalledTimes(2)
    const firstPayload = convertPlacementMock.mock.calls[0][1]
    const secondPayload = convertPlacementMock.mock.calls[1][1]
    expect(firstPayload).toEqual({
      target: 'public_pool',
      idempotency_key: 'account-placement-17-11111111-1111-4111-8111-111111111111'
    })
    expect(secondPayload.idempotency_key).toBe(firstPayload.idempotency_key)
    expect(wrapper.emitted('converted')).toHaveLength(1)
  })

  it('提交期间拒绝重复点击', async () => {
    let resolveRequest: ((value: unknown) => void) | undefined
    convertPlacementMock.mockImplementation(() => new Promise((resolve) => {
      resolveRequest = resolve
    }))

    const wrapper = mountDialog()
    await wrapper.get('[data-testid="placement-target-public_pool"]').setValue()
    const submit = wrapper.get('[data-testid="convert-external-placement"]')

    await submit.trigger('click')
    await submit.trigger('click')

    expect(convertPlacementMock).toHaveBeenCalledTimes(1)

    resolveRequest?.({
      account_id: 17,
      previous: null,
      current: {
        target: 'public_pool',
        state: 'active',
        version: 1
      },
      unchanged: false
    })
    await flushPromises()
  })

  it('只保留同号主、同平台、同等级房间并携带 room_id', async () => {
    listListingsMock.mockResolvedValue({
      items: [
        {
          id: 101,
          room_name: '可加入房间',
          owner_user_id: 88,
          platform: 'openai',
          account_level: 'plus',
          status: 'active'
        },
        {
          id: 102,
          room_name: '其他号主房间',
          owner_user_id: 99,
          platform: 'openai',
          account_level: 'plus',
          status: 'active'
        },
        {
          id: 103,
          room_name: '其他等级房间',
          owner_user_id: 88,
          platform: 'openai',
          account_level: 'team',
          status: 'active'
        }
      ],
      total: 3,
      page: 1,
      page_size: 100,
      pages: 1
    })
    convertPlacementMock.mockResolvedValue({
      account_id: 17,
      previous: null,
      current: {
        target: 'room',
        room_id: 101,
        room_name: '可加入房间',
        state: 'active',
        version: 1
      },
      unchanged: false
    })

    const wrapper = mountDialog()
    await wrapper.get('[data-testid="placement-target-room"]').setValue()
    await flushPromises()

    expect(listListingsMock).toHaveBeenCalledWith(1, 100, {
      tab: 'mine',
      owner_user_id: 88,
      platform: 'openai',
      account_level: 'plus'
    })
    expect(wrapper.find('[data-testid="compatible-room-101"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="compatible-room-102"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="compatible-room-103"]').exists()).toBe(false)

    await wrapper.get('[data-testid="compatible-room-101"]').setValue()
    await wrapper.get('[data-testid="convert-external-placement"]').trigger('click')
    await flushPromises()

    expect(convertPlacementMock).toHaveBeenCalledWith(17, {
      target: 'room',
      room_id: 101,
      idempotency_key: 'account-placement-17-11111111-1111-4111-8111-111111111111'
    })
  })

  it('兼容迁移前的公共共享和账号房间字段', async () => {
    const publicWrapper = mountDialog(buildAccount({
      share_mode: 'public',
      external_placement: null
    }))
    expect(
      (publicWrapper.get('[data-testid="placement-target-public_pool"]').element as HTMLInputElement).checked
    ).toBe(true)
    publicWrapper.unmount()

    listListingsMock.mockResolvedValue({
      items: [{
        id: 301,
        room_name: '旧房间',
        owner_user_id: 88,
        platform: 'openai',
        account_level: 'plus',
        status: 'active'
      }],
      total: 1,
      page: 1,
      page_size: 100,
      pages: 1
    })
    const roomWrapper = mountDialog(buildAccount({
      account_share_mode_listing_id: 301,
      external_placement: null
    }))
    await flushPromises()

    expect(
      (roomWrapper.get('[data-testid="placement-target-room"]').element as HTMLInputElement).checked
    ).toBe(true)
    expect(
      (roomWrapper.get('[data-testid="compatible-room-301"]').element as HTMLInputElement).checked
    ).toBe(true)
    roomWrapper.unmount()
  })
})
