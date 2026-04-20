import { beforeEach, describe, expect, it, vi } from 'vitest'

const post = vi.fn()

vi.mock('@/api/client', () => ({
  apiClient: {
    post
  }
}))

describe('oauth adoption auth api', () => {
  beforeEach(() => {
    post.mockReset()
    post.mockResolvedValue({ data: {} })
    localStorage.clear()
    document.cookie = 'oauth_bind_access_token=; Max-Age=0; path=/'
  })

  it('posts adoption decisions when exchanging pending oauth completion', async () => {
    const { exchangePendingOAuthCompletion } = await import('@/api/auth')

    await exchangePendingOAuthCompletion({
      adoptDisplayName: false,
      adoptAvatar: true
    })

    expect(post).toHaveBeenCalledWith('/auth/oauth/pending/exchange', {
      adopt_display_name: false,
      adopt_avatar: true
    })
  })

  it('posts linuxdo invitation completion with adoption decisions', async () => {
    const { completeLinuxDoOAuthRegistration } = await import('@/api/auth')

    await completeLinuxDoOAuthRegistration('invite-code', {
      adoptDisplayName: true,
      adoptAvatar: false
    })

    expect(post).toHaveBeenCalledWith('/auth/oauth/linuxdo/complete-registration', {
      invitation_code: 'invite-code',
      adopt_display_name: true,
      adopt_avatar: false
    })
  })

  it('posts oidc invitation completion with adoption decisions', async () => {
    const { completeOIDCOAuthRegistration } = await import('@/api/auth')

    await completeOIDCOAuthRegistration('invite-code', {
      adoptDisplayName: false,
      adoptAvatar: true
    })

    expect(post).toHaveBeenCalledWith('/auth/oauth/oidc/complete-registration', {
      invitation_code: 'invite-code',
      adopt_display_name: false,
      adopt_avatar: true
    })
  })

  it('posts wechat invitation completion with adoption decisions', async () => {
    const { completeWeChatOAuthRegistration } = await import('@/api/auth')

    await completeWeChatOAuthRegistration('invite-code', {
      adoptDisplayName: true,
      adoptAvatar: true
    })

    expect(post).toHaveBeenCalledWith('/auth/oauth/wechat/complete-registration', {
      invitation_code: 'invite-code',
      adopt_display_name: true,
      adopt_avatar: true
    })
  })

  it('classifies oauth completion results as login or bind', async () => {
    const { getOAuthCompletionKind } = await import('@/api/auth')

    expect(getOAuthCompletionKind({ access_token: 'access-token' })).toBe('login')
    expect(getOAuthCompletionKind({ redirect: '/profile' })).toBe('bind')
  })

  it('prepares an oauth bind access token cookie before redirect binding', async () => {
    localStorage.setItem('auth_token', 'access-token-value')
    const setCookie = vi.fn()
    Object.defineProperty(document, 'cookie', {
      configurable: true,
      get: () => '',
      set: setCookie
    })

    const { prepareOAuthBindAccessTokenCookie } = await import('@/api/auth')

    prepareOAuthBindAccessTokenCookie()

    expect(setCookie).toHaveBeenCalledTimes(1)
    expect(setCookie.mock.calls[0]?.[0]).toContain('oauth_bind_access_token=access-token-value')
  })
})
