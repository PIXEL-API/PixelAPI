export const ADMIN_UI_REQUEST_HEADER = 'X-Admin-UI-Request'
export const USER_UI_REQUEST_HEADER = 'X-User-UI-Request'

function isAdminPath(path: string): boolean {
  return (
    path === '/admin' ||
    path.startsWith('/admin/') ||
    path === '/api/v1/admin' ||
    path.startsWith('/api/v1/admin/')
  )
}

function requestPath(rawURL: string): string {
  const value = rawURL.trim()
  if (!value) return ''
  try {
    const origin = typeof window !== 'undefined' ? window.location.origin : 'http://localhost'
    const parsed = new URL(value, origin)
    if (/^[a-z][a-z\d+.-]*:/i.test(value) && parsed.origin !== origin) return ''
    return parsed.pathname
  } catch {
    return value.split(/[?#]/, 1)[0]
  }
}

function normalizeAPIPath(path: string): string {
  const raw = requestPath(path)
  if (!raw) return ''
  if (raw === '/api/v1' || raw.startsWith('/api/v1/')) {
    return raw.slice('/api/v1'.length) || '/'
  }
  return raw.startsWith('/') ? raw : `/${raw}`
}

/** Mirrors the backend's explicit authenticated user Web API allowlist. */
export function isUserTimingAPIPath(requestURL: string): boolean {
  const path = normalizeAPIPath(requestURL)
  if (!path) return false

  if (
    path === '/auth/me' ||
    path === '/auth/revoke-all-sessions' ||
    path === '/auth/oauth/bind-token' ||
    path === '/oidc/authorize/complete'
  ) {
    return true
  }
  if (path === '/user' || path.startsWith('/user/')) return true
  if (path === '/keys' || path.startsWith('/keys/')) return true
  if (path === '/accounts' || path.startsWith('/accounts/')) return true
  if (path === '/account-oauth' || path.startsWith('/account-oauth/')) return true
  if (path === '/account-share' || path.startsWith('/account-share/')) return true
  if (path === '/groups/available' || path === '/groups/rates') return true
  if (path === '/channels/available') return true
  if (path === '/usage' || path.startsWith('/usage/')) return true
  if (path === '/announcements' || path.startsWith('/announcements/')) return true
  if (path === '/conversations' || path.startsWith('/conversations/')) return true
  if (path === '/redeem' || path.startsWith('/redeem/')) return true
  if (path === '/subscriptions' || path.startsWith('/subscriptions/')) return true
  if (path === '/channel-monitors' || path.startsWith('/channel-monitors/')) return true
  if (path === '/activities' || path.startsWith('/activities/')) return true
  if (
    path === '/shop/draw-progress' ||
    path === '/shop/orders' ||
    path.startsWith('/shop/orders/')
  ) {
    return true
  }
  if (path.startsWith('/payment/')) {
    if (
      path === '/payment/public' ||
      path.startsWith('/payment/public/') ||
      path === '/payment/webhook' ||
      path.startsWith('/payment/webhook/')
    ) {
      return false
    }
    return true
  }
  return false
}

export function shouldMarkAdminUIRequest(requestURL: string, pagePath?: string): boolean {
  const path = requestPath(requestURL)
  if (!path) return false
  const currentPath =
    pagePath ?? (typeof window !== 'undefined' ? window.location.pathname : '')
  return isAdminPath(path) || isAdminPath(currentPath)
}

export function shouldMarkUserUIRequest(requestURL: string): boolean {
  return isUserTimingAPIPath(requestURL)
}
