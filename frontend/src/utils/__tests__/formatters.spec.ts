import { describe, expect, it } from 'vitest'

import { calculateCacheHitRate, formatCacheHitRate } from '../formatters'

describe('cache hit rate formatters', () => {
  it('uses all three mutually exclusive input-side token buckets', () => {
    expect(calculateCacheHitRate(200, 300, 500)).toBe(50)
    expect(formatCacheHitRate(200, 300, 500)).toBe('50.0%')
    expect(calculateCacheHitRate(500, 0, 1500)).toBe(75)
  })

  it('clamps invalid or negative buckets to zero', () => {
    expect(calculateCacheHitRate(100, -50, 100)).toBe(50)
    expect(calculateCacheHitRate(0, 0, 0)).toBe(0)
    expect(formatCacheHitRate(undefined, undefined, undefined)).toBe('0%')
  })

  it('preserves the existing display precision', () => {
    expect(formatCacheHitRate(9999, 0, 1)).toBe('<0.1%')
    expect(formatCacheHitRate(900, 0, 100)).toBe('10.0%')
  })
})
