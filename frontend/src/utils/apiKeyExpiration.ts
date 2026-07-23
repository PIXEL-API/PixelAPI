function assertValidDate(date: Date, fieldName: string): void {
  if (Number.isNaN(date.getTime())) {
    throw new RangeError(`${fieldName} must be a valid date`)
  }
}

export function resolveExpirationPresetBase(
  currentExpiration: string | null | undefined,
  openedAt: Date
): Date {
  assertValidDate(openedAt, 'openedAt')

  if (!currentExpiration) {
    return new Date(openedAt.getTime())
  }

  const expiration = new Date(currentExpiration)
  assertValidDate(expiration, 'currentExpiration')

  return expiration.getTime() > openedAt.getTime()
    ? expiration
    : new Date(openedAt.getTime())
}

export function calculateExpirationPresetDate(base: Date, days: number): Date {
  assertValidDate(base, 'base')
  if (!Number.isInteger(days) || days <= 0) {
    throw new RangeError('days must be a positive integer')
  }

  const expiration = new Date(base.getTime())
  expiration.setDate(expiration.getDate() + days)
  return expiration
}
