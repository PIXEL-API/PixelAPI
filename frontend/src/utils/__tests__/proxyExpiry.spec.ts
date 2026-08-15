import { describe, expect, it } from 'vitest'
import {
  dateTimeLocalToUnixSeconds,
  daysUntilProxyExpiry,
  getProxyExpiryInfo,
  proxyExpiryBadgeClass,
  proxyExpiryLabelKey,
  toDateTimeLocalValue
} from '../proxyExpiry'

const NOW = new Date('2026-08-14T00:00:00.000Z')
const inDays = (days: number) => new Date(NOW.getTime() + days * 86_400_000).toISOString()

describe('proxy expiry presentation', () => {
  it('uses the proxy-specific warning threshold instead of a global fixed window', () => {
    expect(getProxyExpiryInfo(inDays(10), 'active', 14, NOW)).toEqual({ state: 'expiring', days: 10 })
    expect(getProxyExpiryInfo(inDays(10), 'active', 7, NOW)).toEqual({ state: 'valid', days: 10 })
    expect(proxyExpiryBadgeClass(inDays(10), 'active', 14, NOW)).toBe('badge badge-warning')
  })

  it('treats wall-clock expiry as expired even before a worker updates status', () => {
    expect(getProxyExpiryInfo(inDays(-2), 'active', 7, NOW)).toEqual({ state: 'expired', days: -2 })
    expect(proxyExpiryLabelKey(inDays(-2), 'active', 7, NOW)).toEqual({
      key: 'admin.proxies.overdueDays',
      params: { days: 2 }
    })
  })

  it('preserves explicit zero as a disabled advance-warning window', () => {
    expect(getProxyExpiryInfo(inDays(1), 'active', 0, NOW).state).toBe('valid')
    expect(getProxyExpiryInfo(NOW.toISOString(), 'active', 0, NOW).state).toBe('expired')
  })

  it('rounds remaining partial days upward and rejects invalid contracts', () => {
    expect(daysUntilProxyExpiry(new Date(NOW.getTime() + 1).toISOString(), NOW)).toBe(1)
    expect(() => getProxyExpiryInfo(inDays(1), 'active', -1, NOW)).toThrow(RangeError)
    expect(() => daysUntilProxyExpiry('not-a-date', NOW)).toThrow(RangeError)
  })
})

describe('proxy expiry form conversion', () => {
  it('round-trips a local minute value through the Unix-seconds API contract', () => {
    const localValue = '2026-12-31T23:45'
    const unixSeconds = dateTimeLocalToUnixSeconds(localValue)
    expect(toDateTimeLocalValue(new Date(unixSeconds * 1000).toISOString())).toBe(localValue)
  })

  it('rejects an empty or invalid datetime-local value instead of silently clearing it', () => {
    expect(() => dateTimeLocalToUnixSeconds('')).toThrow(RangeError)
    expect(() => dateTimeLocalToUnixSeconds('invalid')).toThrow(RangeError)
  })
})
