import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ProfileWithdrawalCard from '@/components/user/profile/ProfileWithdrawalCard.vue'

const {
  getReceiptCodeMock,
  listWithdrawalsMock,
  authState,
  showErrorMock,
  showSuccessMock
} = vi.hoisted(() => ({
  getReceiptCodeMock: vi.fn(),
  listWithdrawalsMock: vi.fn(),
  authState: {
    user: { balance: 59.07 },
    refreshUser: vi.fn()
  },
  showErrorMock: vi.fn(),
  showSuccessMock: vi.fn()
}))

vi.mock('@/api', () => ({
  userAPI: {
    getReceiptCode: getReceiptCodeMock,
    listWithdrawals: listWithdrawalsMock,
    uploadReceiptCode: vi.fn(),
    deleteReceiptCode: vi.fn(),
    submitWithdrawal: vi.fn(),
    cancelWithdrawal: vi.fn()
  }
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authState
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: showErrorMock,
    showSuccess: showSuccessMock
  })
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

describe('ProfileWithdrawalCard', () => {
  beforeEach(() => {
    getReceiptCodeMock.mockReset()
    listWithdrawalsMock.mockReset()
    showErrorMock.mockReset()
    showSuccessMock.mockReset()
    authState.user = { balance: 59.07 }
    authState.refreshUser.mockReset()

    getReceiptCodeMock.mockResolvedValue(null)
    listWithdrawalsMock.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 5, pages: 1 })
  })

  it('keeps the withdrawal card mounted when amount is typed', async () => {
    const wrapper = mount(ProfileWithdrawalCard, {
      global: {
        stubs: {
          Icon: true
        }
      }
    })
    await flushPromises()

    const amountInput = wrapper.get('input[inputmode="decimal"]')
    await amountInput.setValue('1.23')

    expect(wrapper.text()).toContain('余额提现与收款码')
    expect(wrapper.text()).toContain('$1.23')
    expect(showErrorMock).not.toHaveBeenCalled()
  })

  it('shows the configured frequency limit and rejection reason directly', async () => {
    listWithdrawalsMock.mockResolvedValue({
      items: [{
        id: 42,
        amount: 12,
        total_deducted: 12,
        status: 'REJECTED',
        rejection_reason: '收款码无法识别，请重新上传',
        created_at: '2026-07-10T08:00:00Z'
      }],
      total: 1,
      page: 1,
      page_size: 5,
      pages: 1
    })

    const wrapper = mount(ProfileWithdrawalCard, {
      props: {
        rateLimitWindowDays: 7,
        rateLimitMax: 3,
        rateLimitExemptAmount: 500
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('每 7 天最多提交 3 次提现申请')
    expect(wrapper.text()).toContain('单笔超过 ¥500.00 时不受次数限制')
    expect(wrapper.text()).toContain('驳回原因')
    expect(wrapper.text()).toContain('收款码无法识别，请重新上传')
  })
})
