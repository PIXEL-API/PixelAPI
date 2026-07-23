import { describe, expect, it } from 'vitest'
import {
  calculateExpirationPresetDate,
  resolveExpirationPresetBase
} from '@/utils/apiKeyExpiration'

describe('apiKeyExpiration', () => {
  const openedAt = new Date('2026-07-22T08:00:00.000Z')

  it('extends a future edit expiration from the original expiration', () => {
    const base = resolveExpirationPresetBase('2026-08-01T08:00:00.000Z', openedAt)

    expect(calculateExpirationPresetDate(base, 7)).toEqual(
      new Date('2026-08-08T08:00:00.000Z')
    )
  })

  it('extends an expired edit expiration from the modal open time', () => {
    const base = resolveExpirationPresetBase('2026-07-01T08:00:00.000Z', openedAt)

    expect(calculateExpirationPresetDate(base, 7)).toEqual(
      new Date('2026-07-29T08:00:00.000Z')
    )
  })

  it('calculates a create expiration from the current time', () => {
    expect(calculateExpirationPresetDate(openedAt, 30)).toEqual(
      new Date('2026-08-21T08:00:00.000Z')
    )
  })

  it('does not accumulate when switching between edit presets', () => {
    const base = resolveExpirationPresetBase('2026-08-01T08:00:00.000Z', openedAt)
    const sevenDayExpiration = calculateExpirationPresetDate(base, 7)
    const thirtyDayExpiration = calculateExpirationPresetDate(base, 30)

    expect(sevenDayExpiration).toEqual(new Date('2026-08-08T08:00:00.000Z'))
    expect(thirtyDayExpiration).toEqual(
      new Date('2026-08-31T08:00:00.000Z')
    )
    expect(base).toEqual(new Date('2026-08-01T08:00:00.000Z'))
  })
})
