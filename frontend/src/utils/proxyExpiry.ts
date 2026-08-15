import { formatDateTimeLocalInput, parseDateTimeLocalInput } from './format'

export const DEFAULT_PROXY_EXPIRY_WARN_DAYS = 7
export const PROXY_EXPIRY_DANGER_DAYS = 3
const DAY_MS = 86_400_000

export type ProxyExpiryState = 'never' | 'valid' | 'expiring' | 'expired'

export interface ProxyExpiryInfo {
  state: ProxyExpiryState
  days: number | null
}

function nowMilliseconds(now: Date | number): number {
  const value = typeof now === 'number' ? now : now.getTime()
  if (!Number.isFinite(value)) throw new RangeError('now must be a valid date or timestamp')
  return value
}

function expiryMilliseconds(expiresAt: string): number {
  const value = new Date(expiresAt).getTime()
  if (!Number.isFinite(value)) throw new RangeError('expiresAt must be a valid ISO timestamp')
  return value
}

function validateWarnDays(warnDays: number): number {
  if (!Number.isInteger(warnDays) || warnDays < 0) {
    throw new RangeError('warnDays must be a non-negative integer')
  }
  return warnDays
}

export function daysUntilProxyExpiry(
  expiresAt: string,
  now: Date | number = Date.now()
): number {
  return Math.ceil((expiryMilliseconds(expiresAt) - nowMilliseconds(now)) / DAY_MS)
}

export function getProxyExpiryInfo(
  expiresAt: string | null,
  status?: string,
  warnDays = DEFAULT_PROXY_EXPIRY_WARN_DAYS,
  now: Date | number = Date.now()
): ProxyExpiryInfo {
  const checkedWarnDays = validateWarnDays(warnDays)
  if (!expiresAt) return { state: status === 'expired' ? 'expired' : 'never', days: null }

  const expiryMs = expiryMilliseconds(expiresAt)
  const currentMs = nowMilliseconds(now)
  const days = Math.ceil((expiryMs - currentMs) / DAY_MS)
  if (status === 'expired' || expiryMs <= currentMs) return { state: 'expired', days }
  if (days <= checkedWarnDays) return { state: 'expiring', days }
  return { state: 'valid', days }
}

export function proxyExpiryBadgeClass(
  expiresAt: string | null,
  status?: string,
  warnDays = DEFAULT_PROXY_EXPIRY_WARN_DAYS,
  now: Date | number = Date.now()
): string {
  const info = getProxyExpiryInfo(expiresAt, status, warnDays, now)
  if (info.state === 'expired') return 'badge badge-danger'
  if (info.state === 'expiring') {
    return (info.days ?? Infinity) <= Math.min(PROXY_EXPIRY_DANGER_DAYS, warnDays)
      ? 'badge badge-danger'
      : 'badge badge-warning'
  }
  return 'text-gray-500 dark:text-gray-400'
}

export function proxyExpiryLabelKey(
  expiresAt: string | null,
  status?: string,
  warnDays = DEFAULT_PROXY_EXPIRY_WARN_DAYS,
  now: Date | number = Date.now()
): { key: string; params?: { days: number } } {
  const info = getProxyExpiryInfo(expiresAt, status, warnDays, now)
  if (info.state === 'never') return { key: 'admin.proxies.neverExpires' }
  if (info.state === 'expired') {
    const overdueDays = Math.max(0, Math.abs(info.days ?? 0))
    return overdueDays > 0
      ? { key: 'admin.proxies.overdueDays', params: { days: overdueDays } }
      : { key: 'admin.proxies.expired' }
  }
  if (info.state === 'expiring') {
    return { key: 'admin.proxies.expiringInDays', params: { days: info.days ?? 0 } }
  }
  return { key: 'admin.proxies.remainingDays', params: { days: info.days ?? 0 } }
}

export function toDateTimeLocalValue(expiresAt: string | null): string {
  if (!expiresAt) return ''
  return formatDateTimeLocalInput(expiryMilliseconds(expiresAt) / 1000)
}

export function dateTimeLocalToUnixSeconds(value: string): number {
  const timestamp = parseDateTimeLocalInput(value)
  if (timestamp === null) {
    throw new RangeError('value must be a valid datetime-local value')
  }
  return timestamp
}
