import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import WechatCallbackView from '@/views/auth/WechatCallbackView.vue'

const {
  exchangePendingOAuthCompletionMock,
  completeWeChatOAuthRegistrationMock,
  login2FAMock,
  apiClientPostMock,
  prepareOAuthBindAccessTokenCookieMock,
  getAuthTokenMock,
  replaceMock,
  setTokenMock,
  showSuccessMock,
  showErrorMock,
  routeState,
  locationState,
} = vi.hoisted(() => ({
  exchangePendingOAuthCompletionMock: vi.fn(),
  completeWeChatOAuthRegistrationMock: vi.fn(),
  login2FAMock: vi.fn(),
  apiClientPostMock: vi.fn(),
  prepareOAuthBindAccessTokenCookieMock: vi.fn(),
  getAuthTokenMock: vi.fn(),
  replaceMock: vi.fn(),
  setTokenMock: vi.fn(),
  showSuccessMock: vi.fn(),
  showErrorMock: vi.fn(),
  routeState: {
    query: {} as Record<string, unknown>,
  },
  locationState: {
    current: {
      href: 'http://localhost/auth/wechat/callback',
      hash: '',
      search: '',
      pathname: '/auth/wechat/callback'
    } as { href: string; hash: string; search: string; pathname: string },
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

vi.mock('@/api/client', () => ({
  apiClient: {
    post: (...args: any[]) => apiClientPostMock(...args),
  },
}))

vi.mock('@/api/auth', async () => {
  const actual = await vi.importActual<typeof import('@/api/auth')>('@/api/auth')
  return {
    ...actual,
    exchangePendingOAuthCompletion: (...args: any[]) => exchangePendingOAuthCompletionMock(...args),
    completeWeChatOAuthRegistration: (...args: any[]) => completeWeChatOAuthRegistrationMock(...args),
    login2FA: (...args: any[]) => login2FAMock(...args),
    prepareOAuthBindAccessTokenCookie: (...args: any[]) => prepareOAuthBindAccessTokenCookieMock(...args),
    getAuthToken: (...args: any[]) => getAuthTokenMock(...args),
  }
})

describe('WechatCallbackView', () => {
  beforeEach(() => {
    exchangePendingOAuthCompletionMock.mockReset()
    completeWeChatOAuthRegistrationMock.mockReset()
    login2FAMock.mockReset()
    apiClientPostMock.mockReset()
    replaceMock.mockReset()
    setTokenMock.mockReset()
    showSuccessMock.mockReset()
    showErrorMock.mockReset()
    prepareOAuthBindAccessTokenCookieMock.mockReset()
    getAuthTokenMock.mockReset()
    routeState.query = {}
    localStorage.clear()
    locationState.current = {
      href: 'http://localhost/auth/wechat/callback',
      hash: '',
      search: '',
      pathname: '/auth/wechat/callback'
    }
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: locationState.current,
    })
    Object.defineProperty(window.navigator, 'userAgent', {
      configurable: true,
      value: 'Mozilla/5.0',
    })
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

  it('offers existing-account email collection during invitation flow', async () => {
    exchangePendingOAuthCompletionMock.mockResolvedValue({
      error: 'invitation_required',
      redirect: '/usage',
    })
    getAuthTokenMock.mockReturnValue(null)

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

    const emailInput = wrapper.get('[data-testid="existing-account-email"]')
    await emailInput.setValue('user@example.com')
    await wrapper.get('[data-testid="existing-account-submit"]').trigger('click')

    expect(replaceMock).toHaveBeenCalledTimes(1)
    expect(replaceMock.mock.calls[0]?.[0]).toContain('/login?')
    expect(replaceMock.mock.calls[0]?.[0]).toContain('wechat_bind_existing%3D1')
    expect(replaceMock.mock.calls[0]?.[0]).toContain('email=user%40example.com')
  })

  it('collects email for pending oauth account creation and submits adoption decisions', async () => {
    exchangePendingOAuthCompletionMock.mockResolvedValue({
      error: 'email_required',
      redirect: '/welcome',
      adoption_required: true,
      suggested_display_name: 'WeChat Nick',
      suggested_avatar_url: 'https://cdn.example/wechat.png',
    })
    apiClientPostMock.mockResolvedValue({
      data: {
        access_token: 'new-access-token',
        refresh_token: 'new-refresh-token',
        expires_in: 3600,
        token_type: 'Bearer',
      },
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

    const checkboxes = wrapper.findAll('input[type="checkbox"]')
    expect(checkboxes).toHaveLength(2)
    await checkboxes[1].setValue(false)
    await wrapper.get('[data-testid="wechat-create-account-email"]').setValue('  new@example.com  ')
    await wrapper.get('[data-testid="wechat-create-account-submit"]').trigger('click')
    await flushPromises()

    expect(apiClientPostMock).toHaveBeenCalledWith('/auth/oauth/pending/create-account', {
      email: 'new@example.com',
      adopt_display_name: true,
      adopt_avatar: false,
    })
    expect(setTokenMock).toHaveBeenCalledWith('new-access-token')
    expect(replaceMock).toHaveBeenCalledWith('/welcome')
  })

  it('shows bind-login form for existing account binding and submits credentials with adoption decisions', async () => {
    exchangePendingOAuthCompletionMock.mockResolvedValue({
      step: 'bind_login_required',
      redirect: '/profile/security',
      email: 'existing@example.com',
      adoption_required: true,
      suggested_display_name: 'WeChat Nick',
      suggested_avatar_url: 'https://cdn.example/wechat.png',
    })
    apiClientPostMock.mockResolvedValue({
      data: {
        access_token: 'bind-access-token',
        refresh_token: 'bind-refresh-token',
        expires_in: 3600,
        token_type: 'Bearer',
      },
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

    const checkboxes = wrapper.findAll('input[type="checkbox"]')
    expect(checkboxes).toHaveLength(2)
    await checkboxes[0].setValue(false)
    await wrapper.get('[data-testid="wechat-bind-login-email"]').setValue('existing@example.com')
    await wrapper.get('[data-testid="wechat-bind-login-password"]').setValue('secret-password')
    await wrapper.get('[data-testid="wechat-bind-login-submit"]').trigger('click')
    await flushPromises()

    expect(apiClientPostMock).toHaveBeenCalledWith('/auth/oauth/pending/bind-login', {
      email: 'existing@example.com',
      password: 'secret-password',
      adopt_display_name: false,
      adopt_avatar: true,
    })
    expect(setTokenMock).toHaveBeenCalledWith('bind-access-token')
    expect(replaceMock).toHaveBeenCalledWith('/profile/security')
  })

  it('allows switching from server-driven bind-login to create-account mode', async () => {
    exchangePendingOAuthCompletionMock.mockResolvedValue({
      step: 'bind_login_required',
      redirect: '/welcome',
      email: 'existing@example.com',
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

    await wrapper.get('button.btn-secondary').trigger('click')
    await flushPromises()

    const createAccountEmail = wrapper.get('[data-testid="wechat-create-account-email"]')
    expect((createAccountEmail.element as HTMLInputElement).value).toBe('existing@example.com')
  })

  it('reuses query email for bind-login when backend does not echo it back', async () => {
    routeState.query = {
      email: 'resume@example.com',
    }
    exchangePendingOAuthCompletionMock.mockResolvedValue({
      step: 'bind_login_required',
      redirect: '/profile',
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

    const bindEmail = wrapper.get('[data-testid="wechat-bind-login-email"]')
    expect((bindEmail.element as HTMLInputElement).value).toBe('resume@example.com')
  })

  it('keeps rendering pending bind-login UI when adoption confirmation leads to another pending step', async () => {
    exchangePendingOAuthCompletionMock
      .mockResolvedValueOnce({
        redirect: '/profile',
        adoption_required: true,
        suggested_display_name: 'WeChat Nick',
        suggested_avatar_url: 'https://cdn.example/wechat.png',
      })
      .mockResolvedValueOnce({
        step: 'bind_login_required',
        redirect: '/profile',
        email: 'existing@example.com',
        adoption_required: true,
        suggested_display_name: 'WeChat Nick',
        suggested_avatar_url: 'https://cdn.example/wechat.png',
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

    expect(showSuccessMock).not.toHaveBeenCalled()
    expect(replaceMock).not.toHaveBeenCalled()
    expect((wrapper.get('[data-testid="wechat-bind-login-email"]').element as HTMLInputElement).value).toBe(
      'existing@example.com'
    )
  })

  it('handles bind-login 2FA challenge before redirecting', async () => {
    exchangePendingOAuthCompletionMock.mockResolvedValue({
      error: 'adopt_existing_user_by_email',
      redirect: '/profile',
      email: 'existing@example.com',
      adoption_required: true,
      suggested_display_name: 'WeChat Nick',
      suggested_avatar_url: 'https://cdn.example/wechat.png',
    })
    apiClientPostMock.mockResolvedValue({
      data: {
        requires_2fa: true,
        temp_token: 'temp-123',
        user_email_masked: 'o***g@example.com',
      },
    })
    login2FAMock.mockResolvedValue({
      access_token: '2fa-access-token',
      refresh_token: '2fa-refresh-token',
      expires_in: 3600,
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

    await wrapper.get('[data-testid="wechat-bind-login-password"]').setValue('secret-password')
    await wrapper.get('[data-testid="wechat-bind-login-submit"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('o***g@example.com')
    expect(login2FAMock).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="wechat-bind-login-totp"]').setValue('123456')
    await wrapper.get('[data-testid="wechat-bind-login-totp-submit"]').trigger('click')
    await flushPromises()

    expect(login2FAMock).toHaveBeenCalledWith({
      temp_token: 'temp-123',
      totp_code: '123456',
    })
    expect(setTokenMock).toHaveBeenCalledWith('2fa-access-token')
    expect(replaceMock).toHaveBeenCalledWith('/profile')
    expect(localStorage.getItem('refresh_token')).toBe('2fa-refresh-token')
  })

  it('restarts the current-user bind flow after returning from login', async () => {
    routeState.query = {
      wechat_bind_existing: '1',
      redirect: '/profile'
    }
    getAuthTokenMock.mockReturnValue('existing-auth-token')

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

    expect(exchangePendingOAuthCompletionMock).not.toHaveBeenCalled()
    expect(prepareOAuthBindAccessTokenCookieMock).toHaveBeenCalledTimes(1)
    expect(locationState.current.href).toContain('/api/v1/auth/oauth/wechat/start?')
    expect(locationState.current.href).toContain('intent=bind_current_user')
    expect(locationState.current.href).toContain('redirect=%2Fprofile')
  })
})
