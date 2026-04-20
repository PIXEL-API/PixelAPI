import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ProfileIdentityBindingsSection from '@/components/user/profile/ProfileIdentityBindingsSection.vue'
import type { User } from '@/types'

const routeState = vi.hoisted(() => ({
  fullPath: '/profile',
}))

const locationState = vi.hoisted(() => ({
  current: { href: 'http://localhost/profile' } as { href: string },
}))

vi.mock('vue-router', () => ({
  useRoute: () => routeState,
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string>) => {
        if (key === 'profile.authBindings.title') return 'Connected sign-in methods'
        if (key === 'profile.authBindings.description') return 'Manage bound providers'
        if (key === 'profile.authBindings.status.bound') return 'Bound'
        if (key === 'profile.authBindings.status.notBound') return 'Not bound'
        if (key === 'profile.authBindings.providers.email') return 'Email'
        if (key === 'profile.authBindings.providers.linuxdo') return 'LinuxDo'
        if (key === 'profile.authBindings.providers.wechat') return 'WeChat'
        if (key === 'profile.authBindings.providers.oidc') return params?.providerName || 'OIDC'
        if (key === 'profile.authBindings.bindAction') return `Bind ${params?.providerName || ''}`.trim()
        return key
      },
    }),
  }
})

function createUser(overrides: Partial<User> = {}): User {
  return {
    id: 7,
    username: 'alice',
    email: 'alice@example.com',
    role: 'user',
    balance: 10,
    concurrency: 2,
    status: 'active',
    allowed_groups: null,
    balance_notify_enabled: true,
    balance_notify_threshold: null,
    balance_notify_extra_emails: [],
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-20T00:00:00Z',
    ...overrides,
  }
}

describe('ProfileIdentityBindingsSection', () => {
  beforeEach(() => {
    routeState.fullPath = '/profile'
    locationState.current = { href: 'http://localhost/profile' }
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: locationState.current,
    })
    Object.defineProperty(window.navigator, 'userAgent', {
      configurable: true,
      value: 'Mozilla/5.0',
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders provider binding states and provider-specific bind actions', () => {
    const wrapper = mount(ProfileIdentityBindingsSection, {
      props: {
        user: createUser({
          auth_bindings: {
            email: { bound: true },
            linuxdo: { bound: true },
            oidc: { bound: false },
            wechat: false,
          },
        }),
        linuxdoEnabled: true,
        oidcEnabled: true,
        oidcProviderName: 'ExampleID',
        wechatEnabled: true,
      },
    })

    expect(wrapper.get('[data-testid="profile-binding-email-status"]').text()).toBe('Bound')
    expect(wrapper.get('[data-testid="profile-binding-linuxdo-status"]').text()).toBe('Bound')
    expect(wrapper.get('[data-testid="profile-binding-oidc-status"]').text()).toBe('Not bound')
    expect(wrapper.get('[data-testid="profile-binding-oidc-action"]').text()).toBe(
      'Bind ExampleID'
    )
    expect(wrapper.get('[data-testid="profile-binding-wechat-action"]').text()).toBe('Bind WeChat')
  })

  it('starts the WeChat bind flow for the current profile page', async () => {
    const wrapper = mount(ProfileIdentityBindingsSection, {
      props: {
        user: createUser(),
        linuxdoEnabled: false,
        oidcEnabled: false,
        wechatEnabled: true,
      },
    })

    await wrapper.get('[data-testid="profile-binding-wechat-action"]').trigger('click')

    expect(locationState.current.href).toContain('/api/v1/auth/oauth/wechat/start?')
    expect(locationState.current.href).toContain('mode=open')
    expect(locationState.current.href).toContain('intent=bind_current_user')
    expect(locationState.current.href).toContain('redirect=%2Fprofile')
  })
})
