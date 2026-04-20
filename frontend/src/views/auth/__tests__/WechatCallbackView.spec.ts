import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import WechatCallbackView from '@/views/auth/WechatCallbackView.vue'

const {
  exchangePendingOAuthCompletionMock,
  completeWeChatOAuthRegistrationMock,
  replaceMock,
  setTokenMock,
  showSuccessMock,
  showErrorMock,
  routeState,
} = vi.hoisted(() => ({
  exchangePendingOAuthCompletionMock: vi.fn(),
  completeWeChatOAuthRegistrationMock: vi.fn(),
  replaceMock: vi.fn(),
  setTokenMock: vi.fn(),
  showSuccessMock: vi.fn(),
  showErrorMock: vi.fn(),
  routeState: {
    query: {} as Record<string, unknown>,
  },
}))

vi.mock('vue-router', () => ({
  useRoute: () => routeState,
  useRouter: () => ({
    replace: replaceMock,
  }),
}))

vi.mock('vue-i18n', () => ({
  createI18n: () => ({
    global: {
      t: (key: string) => key,
    },
  }),
  useI18n: () => ({
    t: (key: string, params?: Record<string, string>) => {
      if (key === 'auth.oidc.callbackTitle') {
        return `Signing you in with ${params?.providerName ?? ''}`.trim()
      }
      if (key === 'auth.oidc.callbackProcessing') {
        return `Completing login with ${params?.providerName ?? ''}`.trim()
      }
      if (key === 'auth.oidc.invitationRequired') {
        return `${params?.providerName ?? ''} invitation required`.trim()
      }
      if (key === 'auth.oidc.completeRegistration') {
        return 'Complete registration'
      }
      if (key === 'auth.oidc.completing') {
        return 'Completing'
      }
      if (key === 'auth.oidc.backToLogin') {
        return 'Back to login'
      }
      if (key === 'auth.invitationCodePlaceholder') {
        return 'Invitation code'
      }
      if (key === 'auth.loginSuccess') {
        return 'Login success'
      }
      if (key === 'auth.loginFailed') {
        return 'Login failed'
      }
      if (key === 'auth.oidc.callbackHint') {
        return 'Callback hint'
      }
      if (key === 'auth.oidc.callbackMissingToken') {
        return 'Missing login token'
      }
      if (key === 'auth.oidc.completeRegistrationFailed') {
        return 'Complete registration failed'
      }
      return key
    },
  }),
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => ({
    setToken: setTokenMock,
  }),
  useAppStore: () => ({
    showSuccess: showSuccessMock,
    showError: showErrorMock,
  }),
}))

vi.mock('@/api/auth', async () => {
  const actual = await vi.importActual<typeof import('@/api/auth')>('@/api/auth')
  return {
    ...actual,
    exchangePendingOAuthCompletion: (...args: any[]) => exchangePendingOAuthCompletionMock(...args),
    completeWeChatOAuthRegistration: (...args: any[]) => completeWeChatOAuthRegistrationMock(...args),
  }
})

describe('WechatCallbackView', () => {
  beforeEach(() => {
    exchangePendingOAuthCompletionMock.mockReset()
    completeWeChatOAuthRegistrationMock.mockReset()
    replaceMock.mockReset()
    setTokenMock.mockReset()
    showSuccessMock.mockReset()
    showErrorMock.mockReset()
    routeState.query = {}
    localStorage.clear()
  })

  it('does not send adoption decisions during the initial exchange', async () => {
    exchangePendingOAuthCompletionMock.mockResolvedValue({
      access_token: 'access-token',
      refresh_token: 'refresh-token',
      expires_in: 3600,
      redirect: '/dashboard',
      adoption_required: true,
    })
    setTokenMock.mockResolvedValue({})

    mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()

    expect(exchangePendingOAuthCompletionMock).toHaveBeenCalledWith()
    expect(exchangePendingOAuthCompletionMock).toHaveBeenCalledTimes(1)
  })

  it('waits for explicit adoption confirmation before finishing a non-invitation login', async () => {
    exchangePendingOAuthCompletionMock
      .mockResolvedValueOnce({
        redirect: '/dashboard',
        adoption_required: true,
        suggested_display_name: 'WeChat Nick',
        suggested_avatar_url: 'https://cdn.example/wechat.png',
      })
      .mockResolvedValueOnce({
        access_token: 'wechat-access-token',
        refresh_token: 'wechat-refresh-token',
        expires_in: 3600,
        token_type: 'Bearer',
        redirect: '/dashboard',
      })
    setTokenMock.mockResolvedValue({})

    const wrapper = mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('WeChat Nick')
    expect(setTokenMock).not.toHaveBeenCalled()
    expect(replaceMock).not.toHaveBeenCalled()

    const checkboxes = wrapper.findAll('input[type="checkbox"]')
    expect(checkboxes).toHaveLength(2)
    await checkboxes[1].setValue(false)

    const buttons = wrapper.findAll('button')
    expect(buttons).toHaveLength(1)
    await buttons[0].trigger('click')
    await flushPromises()

    expect(exchangePendingOAuthCompletionMock).toHaveBeenNthCalledWith(1)
    expect(exchangePendingOAuthCompletionMock).toHaveBeenNthCalledWith(2, {
      adoptDisplayName: true,
      adoptAvatar: false,
    })
    expect(setTokenMock).toHaveBeenCalledWith('wechat-access-token')
    expect(replaceMock).toHaveBeenCalledWith('/dashboard')
    expect(localStorage.getItem('refresh_token')).toBe('wechat-refresh-token')
  })

  it('supports bind completion after adoption confirmation', async () => {
    exchangePendingOAuthCompletionMock
      .mockResolvedValueOnce({
        redirect: '/dashboard',
        adoption_required: true,
        suggested_display_name: 'WeChat Nick',
        suggested_avatar_url: 'https://cdn.example/wechat.png',
      })
      .mockResolvedValueOnce({
        redirect: '/profile/connections',
      })

    const wrapper = mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()

    await wrapper.findAll('button')[0].trigger('click')
    await flushPromises()

    expect(exchangePendingOAuthCompletionMock).toHaveBeenNthCalledWith(2, {
      adoptDisplayName: true,
      adoptAvatar: true,
    })
    expect(setTokenMock).not.toHaveBeenCalled()
    expect(showSuccessMock).toHaveBeenCalledWith('profile.authBindings.bindSuccess')
    expect(replaceMock).toHaveBeenCalledWith('/profile/connections')
  })

  it('renders adoption choices for invitation flow and submits the selected values', async () => {
    exchangePendingOAuthCompletionMock.mockResolvedValue({
      error: 'invitation_required',
      redirect: '/subscriptions',
      adoption_required: true,
      suggested_display_name: 'WeChat Nick',
      suggested_avatar_url: 'https://cdn.example/wechat.png',
    })
    completeWeChatOAuthRegistrationMock.mockResolvedValue({
      access_token: 'wechat-invite-token',
      refresh_token: 'wechat-invite-refresh',
      expires_in: 600,
      token_type: 'Bearer',
    })

    const wrapper = mount(WechatCallbackView, {
      global: {
        stubs: {
          AuthLayout: { template: '<div><slot /></div>' },
          Icon: true,
          RouterLink: { template: '<a><slot /></a>' },
          transition: false,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('WeChat Nick')
    const checkboxes = wrapper.findAll('input[type="checkbox"]')
    expect(checkboxes).toHaveLength(2)
    await checkboxes[0].setValue(false)
    await wrapper.get('input[type="text"]').setValue(' INVITE-CODE ')
    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(completeWeChatOAuthRegistrationMock).toHaveBeenCalledWith('INVITE-CODE', {
      adoptDisplayName: false,
      adoptAvatar: true,
    })
    expect(setTokenMock).toHaveBeenCalledWith('wechat-invite-token')
    expect(replaceMock).toHaveBeenCalledWith('/subscriptions')
  })
})
