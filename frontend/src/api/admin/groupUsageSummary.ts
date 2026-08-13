export interface GroupUsageSummary {
  group_id: number
  today_cost: number
  total_cost: number
}

export function buildGroupUsageSummaryParams(
  groupIds: number[],
  timezone?: string
): { timezone?: string; group_ids?: string } {
  return {
    ...(timezone ? { timezone } : {}),
    ...(groupIds.length > 0 ? { group_ids: groupIds.join(',') } : {})
  }
}

export function createGroupUsageSummaryMap(
  summaries: GroupUsageSummary[]
): Map<number, Omit<GroupUsageSummary, 'group_id'>> {
  return new Map(
    summaries.map(({ group_id, today_cost, total_cost }) => [
      group_id,
      { today_cost, total_cost }
    ])
  )
}

export function formatGroupUsageCost(cost: number): string {
  if (cost >= 1000) return cost.toFixed(0)
  if (cost >= 100) return cost.toFixed(1)
  return cost.toFixed(2)
}
