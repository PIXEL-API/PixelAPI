import { describe, expect, it } from 'vitest'

import {
  buildGroupUsageSummaryParams,
  createGroupUsageSummaryMap,
  formatGroupUsageCost
} from '@/api/admin/groupUsageSummary'

describe('group usage summary helpers', () => {
  it('builds an explicit current-page query without empty parameters', () => {
    expect(buildGroupUsageSummaryParams([7, 11], 'Asia/Shanghai')).toEqual({
      timezone: 'Asia/Shanghai',
      group_ids: '7,11'
    })
    expect(buildGroupUsageSummaryParams([], 'UTC')).toEqual({ timezone: 'UTC' })
    expect(buildGroupUsageSummaryParams([7])).toEqual({ group_ids: '7' })
    expect(buildGroupUsageSummaryParams([])).toEqual({})
  })

  it('maps backend summaries by group ID without changing monetary values', () => {
    expect(
      createGroupUsageSummaryMap([
        { group_id: 7, today_cost: 1.25, total_cost: 10.5 },
        { group_id: 11, today_cost: 0, total_cost: 200.75 }
      ])
    ).toEqual(
      new Map([
        [7, { today_cost: 1.25, total_cost: 10.5 }],
        [11, { today_cost: 0, total_cost: 200.75 }]
      ])
    )
  })

  it('formats group costs with scale-appropriate precision', () => {
    expect(formatGroupUsageCost(1234.56)).toBe('1235')
    expect(formatGroupUsageCost(123.45)).toBe('123.5')
    expect(formatGroupUsageCost(12.345)).toBe('12.35')
  })
})
