/**
 * 订阅套餐有效期单位的换算与展示。
 *
 * 管理端表单只提供复数取值（days / weeks / months，见 PlanEditDialog 的
 * validityUnitOptions），而后端 psComputeValidityDays 早先只认单数，
 * 导致「配置 1 个月」实际只发 1 天订阅；更麻烦的是前端展示同样只匹配单数，
 * 于是页面也显示「1天」，两边一起错，问题长期不可见。
 *
 * 这里与后端 psComputeValidityDays 保持同一套换算语义，作为两个展示点的唯一实现。
 *
 * 注意：后端没有 year 分支（落到 default 原样返回天数），所以此处同样把未知单位
 * 按天处理，不单独渲染「年」——否则展示会与实际发放的订阅时长不一致。
 */

export type ValidityUnit = 'day' | 'week' | 'month'

/** 归一化单复数写法；未知单位一律按天，与后端 default 分支一致。 */
export function normalizeValidityUnit(unit?: string | null): ValidityUnit {
  switch ((unit || '').trim().toLowerCase()) {
    case 'week':
    case 'weeks':
      return 'week'
    case 'month':
    case 'months':
      return 'month'
    default:
      return 'day'
  }
}

/** 与后端 psComputeValidityDays 一一对应的天数换算。 */
export function validityEffectiveDays(days: number, unit?: string | null): number {
  switch (normalizeValidityUnit(unit)) {
    case 'week':
      return days * 7
    case 'month':
      return days * 30
    default:
      return days
  }
}

type Translate = (key: string) => string

interface ValidityPlanLike {
  validity_days?: number | null
  validity_unit?: string | null
}

/** 价格后缀，渲染成 “/ 月”“/ 30天” 这种形式。 */
export function formatPlanValiditySuffix(
  plan: ValidityPlanLike | null | undefined,
  t: Translate,
): string {
  if (!plan) return ''
  const days = plan.validity_days ?? 0
  const unit = normalizeValidityUnit(plan.validity_unit)
  if (unit === 'month') {
    return days === 1 ? t('payment.perMonth') : `${days}${t('payment.months')}`
  }
  return `${validityEffectiveDays(days, unit)}${t('payment.days')}`
}
