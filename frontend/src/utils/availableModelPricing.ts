import type {
  UserAvailableGroup,
  UserPricingInterval,
  UserPricingTimeRange,
  UserSupportedModel,
  UserSupportedModelPricing,
} from '@/api/channels'
import {
  BILLING_MODE_IMAGE,
  BILLING_MODE_PER_REQUEST,
  BILLING_MODE_TOKEN,
} from '@/constants/channel'
import { formatScaled } from '@/utils/pricing'

export const TOKEN_PRICE_SCALE = 1_000_000

type Translate = (key: string) => string

export interface AvailablePricingItem {
  key: string
  label: string
  value: string
}

function pricingKey(prefix: string, key: string): string {
  return `${prefix}.${key}`
}

export function availablePriceUnit(scale: number, translate: Translate, prefix: string): string {
  return translate(pricingKey(prefix, scale === TOKEN_PRICE_SCALE ? 'unitPerMillion' : 'unitPerRequest'))
}

export function formatAvailablePrice(
  value: number | null,
  scale: number,
  translate: Translate,
  prefix: string,
): string {
  if (value == null) return '-'
  return `${formatScaled(value, scale)} ${availablePriceUnit(scale, translate, prefix)}`
}

export function billingModeLabel(
  pricing: UserSupportedModelPricing | null,
  translate: Translate,
  prefix: string,
): string {
  switch (pricing?.billing_mode) {
    case BILLING_MODE_TOKEN:
      return translate(pricingKey(prefix, 'billingModeToken'))
    case BILLING_MODE_PER_REQUEST:
      return translate(pricingKey(prefix, 'billingModePerRequest'))
    case BILLING_MODE_IMAGE:
      return translate(pricingKey(prefix, 'billingModeImage'))
    default:
      return '-'
  }
}

export function availablePricingItems(
  model: UserSupportedModel,
  translate: Translate,
  prefix: string,
  multiplier = 1,
): AvailablePricingItem[] {
  const pricing = model.pricing
  if (!pricing) return []

  const items: AvailablePricingItem[] = [
    {
      key: 'billingMode',
      label: translate(pricingKey(prefix, 'billingMode')),
      value: billingModeLabel(pricing, translate, prefix),
    },
  ]

  const addPrice = (key: string, labelKey: string, value: number | null, scale: number) => {
    if (value == null) return
    items.push({
      key,
      label: translate(pricingKey(prefix, labelKey)),
      value: formatAvailablePrice(value * multiplier, scale, translate, prefix),
    })
  }

  if (pricing.billing_mode === BILLING_MODE_TOKEN) {
    addPrice('input', 'inputPrice', pricing.input_price, TOKEN_PRICE_SCALE)
    addPrice('output', 'outputPrice', pricing.output_price, TOKEN_PRICE_SCALE)
    addPrice('cacheWrite', 'cacheWritePrice', pricing.cache_write_price, TOKEN_PRICE_SCALE)
    addPrice('cacheRead', 'cacheReadPrice', pricing.cache_read_price, TOKEN_PRICE_SCALE)
    addPrice('imageInput', 'imageInputPrice', pricing.image_input_price, TOKEN_PRICE_SCALE)
    addPrice('imageCacheRead', 'imageCacheReadPrice', pricing.image_cache_read_price, TOKEN_PRICE_SCALE)
    addPrice('imageOutput', 'imageOutputPrice', pricing.image_output_price, TOKEN_PRICE_SCALE)
  } else if (pricing.billing_mode === BILLING_MODE_PER_REQUEST) {
    addPrice('perRequest', 'perRequestPrice', pricing.per_request_price, 1)
  } else if (pricing.billing_mode === BILLING_MODE_IMAGE) {
    addPrice('imageOutput', 'imageOutputPrice', pricing.image_output_price, 1)
  }

  return items
}

export function availableModelPriceSummary(
  model: UserSupportedModel,
  translate: Translate,
  prefix: string,
  noPricingLabel: string,
  multiplier = 1,
): string {
  const pricing = model.pricing
  if (!pricing) return noPricingLabel

  if (pricing.billing_mode === BILLING_MODE_TOKEN) {
    const inputPrice = pricing.input_price == null ? null : pricing.input_price * multiplier
    const outputPrice = pricing.output_price == null ? null : pricing.output_price * multiplier
    return `${translate(pricingKey(prefix, 'inputPrice'))} ${formatScaled(inputPrice, TOKEN_PRICE_SCALE)} · ${translate(pricingKey(prefix, 'outputPrice'))} ${formatScaled(outputPrice, TOKEN_PRICE_SCALE)}`
  }
  if (pricing.billing_mode === BILLING_MODE_PER_REQUEST) {
    const price = pricing.per_request_price == null ? null : pricing.per_request_price * multiplier
    return `${translate(pricingKey(prefix, 'perRequestPrice'))} ${formatScaled(price, 1)}`
  }
  if (pricing.billing_mode === BILLING_MODE_IMAGE) {
    const price = pricing.image_output_price == null ? null : pricing.image_output_price * multiplier
    return `${translate(pricingKey(prefix, 'imageOutputPrice'))} ${formatScaled(price, 1)}`
  }
  return billingModeLabel(pricing, translate, prefix)
}

export function availableIntervalLabel(interval: UserPricingInterval): string {
  const maximum = interval.max_tokens == null ? '∞' : String(interval.max_tokens)
  return interval.tier_label || `(${interval.min_tokens}, ${maximum}]`
}

export function intervalHasPrice(interval: UserPricingInterval): boolean {
  return (
    interval.input_price != null ||
    interval.output_price != null ||
    interval.cache_write_price != null ||
    interval.cache_read_price != null ||
    interval.per_request_price != null
  )
}

export function timeRangeHasPrice(range: UserPricingTimeRange): boolean {
  return (
    range.input_price != null ||
    range.output_price != null ||
    range.cache_write_price != null ||
    range.cache_read_price != null ||
    range.image_input_price != null ||
    range.image_cache_read_price != null ||
    range.image_output_price != null ||
    range.per_request_price != null
  )
}

function formatMinute(minute: number): string {
  const h = Math.floor(minute / 60)
  const m = minute % 60
  return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`
}

export function availableTimeRangeLabel(range: UserPricingTimeRange): string {
  return `${formatMinute(range.start_minute)} - ${formatMinute(range.end_minute)}`
}

export function effectiveGroupRate(
  group: UserAvailableGroup,
  userGroupRates: Record<number, number>,
): number {
  return userGroupRates[group.id] ?? group.rate_multiplier
}

export function effectiveGroupPriceSummary(
  model: UserSupportedModel,
  group: UserAvailableGroup,
  userGroupRates: Record<number, number>,
  translate: Translate,
  prefix: string,
  noPricingLabel: string,
): string {
  const pricing = model.pricing
  if (!pricing) return noPricingLabel
  if (pricing.intervals?.some(intervalHasPrice)) {
    return translate('availableChannels.groupRates.intervalMultiplierHint')
  }

  const rate = effectiveGroupRate(group, userGroupRates)
  const effectivePrice = (value: number | null, scale: number) =>
    formatAvailablePrice(value == null ? null : value * rate, scale, translate, prefix)

  if (pricing.billing_mode === BILLING_MODE_TOKEN) {
    const entries = [
      ['inputPrice', pricing.input_price],
      ['outputPrice', pricing.output_price],
      ['cacheWritePrice', pricing.cache_write_price],
      ['cacheReadPrice', pricing.cache_read_price],
      ['imageInputPrice', pricing.image_input_price],
      ['imageCacheReadPrice', pricing.image_cache_read_price],
      ['imageOutputPrice', pricing.image_output_price],
    ] as const

    const summary = entries
      .filter(([, value]) => value != null)
      .map(
        ([labelKey, value]) =>
          `${translate(pricingKey(prefix, labelKey))} ${effectivePrice(value, TOKEN_PRICE_SCALE)}`,
      )
      .join(' · ')
    return summary || '-'
  }
  if (pricing.billing_mode === BILLING_MODE_PER_REQUEST) {
    return `${translate(pricingKey(prefix, 'perRequestPrice'))} ${effectivePrice(pricing.per_request_price, 1)}`
  }
  if (pricing.billing_mode === BILLING_MODE_IMAGE) {
    return `${translate(pricingKey(prefix, 'imageOutputPrice'))} ${effectivePrice(pricing.image_output_price, 1)}`
  }
  return '-'
}
