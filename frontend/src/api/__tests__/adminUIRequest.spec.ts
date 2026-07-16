import { describe, expect, it } from 'vitest'
import {
  isUserTimingAPIPath,
  shouldMarkAdminUIRequest,
  shouldMarkUserUIRequest,
} from '@/api/adminUIRequest'

describe('Server-Timing UI request scope', () => {
  it.each([
    '/auth/me',
    '/api/v1/user/profile',
    '/keys/1',
    '/accounts/1/usage',
    '/account-oauth/openai/auth-url',
    '/account-share/listings',
    '/groups/available',
    '/channels/available',
    '/usage/dashboard/stats',
    '/announcements',
    '/conversations/unread-count',
    '/redeem/history',
    '/subscriptions/active',
    '/channel-monitors/1/status',
    '/activities/winners',
    '/shop/draw-progress',
    '/shop/orders/1',
    '/payment/orders/my',
    '/oidc/authorize/complete',
  ])('marks authenticated user API %s', (path) => {
    expect(isUserTimingAPIPath(path)).toBe(true)
    expect(shouldMarkUserUIRequest(path)).toBe(true)
  })

  it.each([
    '/auth/login',
    '/settings/public',
    '/public/usage/today',
    '/shop/categories',
    '/shop/products/1',
    '/payment/public/orders/verify',
    '/payment/webhook/stripe',
    '/admin/users',
    '/groups',
    '/channels',
    'https://untrusted.example/keys',
  ])('does not mark public, admin, or external API %s as a user request', (path) => {
    expect(isUserTimingAPIPath(path)).toBe(false)
    expect(shouldMarkUserUIRequest(path)).toBe(false)
  })

  it('marks admin API paths and requests made by an admin page', () => {
    expect(shouldMarkAdminUIRequest('/api/v1/admin/users', '/')).toBe(true)
    expect(shouldMarkAdminUIRequest('/groups/available', '/admin/dashboard')).toBe(true)
    expect(shouldMarkAdminUIRequest('/groups/available', '/dashboard')).toBe(false)
  })

  it('does not mark external absolute URLs as admin API paths', () => {
    expect(shouldMarkAdminUIRequest('https://untrusted.example/api/v1/admin/users', '/')).toBe(false)
    expect(
      shouldMarkAdminUIRequest('https://untrusted.example/api/v1/admin/users', '/admin/dashboard')
    ).toBe(false)
  })
})
