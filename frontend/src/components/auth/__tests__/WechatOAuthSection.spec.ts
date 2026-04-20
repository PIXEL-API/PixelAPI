import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import WechatOAuthSection from '@/components/auth/WechatOAuthSection.vue'

const routeState = vi.hoisted(() => ({
  query: {} as Record<string, unknown>,
}))

const locationState = vi.hoisted(() => ({
  current: { href: 'http://localhost/login' } as { href: string },
}))

vi.mock('vue-router', () => ({
  useRoute: () => routeState,
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, string>) => {
      if (key === 'auth.oidc.signIn') {
        return `Continue with ${params?.providerName ?? ''}`.trim()
      }
      if (key === 'auth.oauthOrContinue') {
        return 'or continue'
      }
      return key
    },
  }),
}))

describe('WechatOAuthSection', () => {
  beforeEach(() => {
    routeState.query = { redirect: '/billing?plan=pro' }
    locationState.current = { href: 'http://localhost/login' }
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

  it('starts the open WeChat OAuth flow with the current redirect target', async () => {
    const wrapper = mount(WechatOAuthSection)

    expect(wrapper.text()).toContain('WeChat')

    await wrapper.get('button').trigger('click')

    expect(locationState.current.href).toContain(
      '/api/v1/auth/oauth/wechat/start?mode=open&redirect=%2Fbilling%3Fplan%3Dpro'
    )
  })

  it('uses mp mode inside the WeChat browser', async () => {
    Object.defineProperty(window.navigator, 'userAgent', {
      configurable: true,
      value: 'Mozilla/5.0 MicroMessenger',
    })
    const wrapper = mount(WechatOAuthSection)

    await wrapper.get('button').trigger('click')

    expect(locationState.current.href).toContain(
      '/api/v1/auth/oauth/wechat/start?mode=mp&redirect=%2Fbilling%3Fplan%3Dpro'
    )
  })
})
