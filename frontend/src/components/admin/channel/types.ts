import type { BillingMode, ChannelModelPricing, PricingInterval } from '@/api/admin/channels'

export interface IntervalFormEntry {
  min_tokens: number
  max_tokens: number | null
  tier_label: string
  input_price: number | string | null
  output_price: number | string | null
  cache_write_price: number | string | null
  cache_read_price: number | string | null
  per_request_price: number | string | null
  sort_order: number
}

export interface PricingFormEntry {
  models: string[]
  billing_mode: BillingMode
  long_context_pricing_enabled: boolean | null
  long_context_input_token_threshold: number | string | null
  input_price: number | string | null
  output_price: number | string | null
  cache_write_price: number | string | null
  cache_read_price: number | string | null
  image_input_price: number | string | null
  image_cache_read_price: number | string | null
  image_output_price: number | string | null
  per_request_price: number | string | null
  intervals: IntervalFormEntry[]
}

export const MAX_LONG_CONTEXT_INPUT_TOKEN_THRESHOLD = 2_147_483_647

export type LongContextPricingValidationError = 'interval_conflict' | 'threshold_required'

interface CreatePricingFormEntryOptions {
  longContextPricingEnabled?: boolean | null
}

export function createPricingFormEntry(options: CreatePricingFormEntryOptions = {}): PricingFormEntry {
  return {
    models: [],
    billing_mode: 'token',
    long_context_pricing_enabled: options.longContextPricingEnabled ?? null,
    long_context_input_token_threshold: null,
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_read_price: null,
    image_input_price: null,
    image_cache_read_price: null,
    image_output_price: null,
    per_request_price: null,
    intervals: [],
  }
}

// 价格转换：后端存 per-token，前端显示 per-MTok ($/1M tokens)
const MTOK = 1_000_000

export function toNullableNumber(val: number | string | null | undefined): number | null {
  if (val === null || val === undefined || val === '') return null
  const num = Number(val)
  return isNaN(num) ? null : num
}

export function toPositiveInteger(val: number | string | null | undefined): number | null {
  const num = toNullableNumber(val)
  return num !== null && Number.isInteger(num) && num > 0 && num <= MAX_LONG_CONTEXT_INPUT_TOKEN_THRESHOLD
    ? num
    : null
}

export function apiLongContextPricingToForm(
  pricing: Pick<ChannelModelPricing, 'long_context_pricing_enabled' | 'long_context_input_token_threshold'>,
): Pick<PricingFormEntry, 'long_context_pricing_enabled' | 'long_context_input_token_threshold'> {
  return {
    long_context_pricing_enabled: pricing.long_context_pricing_enabled ?? null,
    long_context_input_token_threshold: pricing.long_context_input_token_threshold ?? null,
  }
}

export function formLongContextPricingToAPI(
  entry: Pick<PricingFormEntry, 'long_context_pricing_enabled' | 'long_context_input_token_threshold'>,
): Pick<ChannelModelPricing, 'long_context_pricing_enabled' | 'long_context_input_token_threshold'> {
  const enabled = entry.long_context_pricing_enabled ?? null
  return {
    long_context_pricing_enabled: enabled,
    long_context_input_token_threshold: enabled === true
      ? toPositiveInteger(entry.long_context_input_token_threshold)
      : null,
  }
}

export function validateLongContextPricing(entry: PricingFormEntry): LongContextPricingValidationError | null {
  if (entry.long_context_pricing_enabled !== true) return null
  if (entry.intervals.length > 0) return 'interval_conflict'
  if (toPositiveInteger(entry.long_context_input_token_threshold) === null) return 'threshold_required'
  return null
}

/** 前端显示值($/MTok) → 后端存储值(per-token) */
export function mTokToPerToken(val: number | string | null | undefined): number | null {
  const num = toNullableNumber(val)
  return num === null ? null : parseFloat((num / MTOK).toPrecision(10))
}

/** 后端存储值(per-token) → 前端显示值($/MTok) */
export function perTokenToMTok(val: number | null | undefined): number | null {
  if (val === null || val === undefined) return null
  // toPrecision(10) 消除 IEEE 754 浮点乘法精度误差，如 5e-8 * 1e6 = 0.04999...96 → 0.05
  return parseFloat((val * MTOK).toPrecision(10))
}

export function apiIntervalsToForm(intervals: PricingInterval[]): IntervalFormEntry[] {
  return (intervals || []).map(iv => ({
    min_tokens: iv.min_tokens,
    max_tokens: iv.max_tokens,
    tier_label: iv.tier_label || '',
    input_price: perTokenToMTok(iv.input_price),
    output_price: perTokenToMTok(iv.output_price),
    cache_write_price: perTokenToMTok(iv.cache_write_price),
    cache_read_price: perTokenToMTok(iv.cache_read_price),
    per_request_price: iv.per_request_price,
    sort_order: iv.sort_order
  }))
}

export function formIntervalsToAPI(intervals: IntervalFormEntry[]): PricingInterval[] {
  return (intervals || []).map(iv => ({
    min_tokens: iv.min_tokens,
    max_tokens: iv.max_tokens,
    tier_label: iv.tier_label,
    input_price: mTokToPerToken(iv.input_price),
    output_price: mTokToPerToken(iv.output_price),
    cache_write_price: mTokToPerToken(iv.cache_write_price),
    cache_read_price: mTokToPerToken(iv.cache_read_price),
    per_request_price: toNullableNumber(iv.per_request_price),
    sort_order: iv.sort_order
  }))
}

// ── 模型模式冲突检测 ──────────────────────────────────────

interface ModelPattern {
  pattern: string
  prefix: string  // lowercase, 通配符去掉尾部 *
  wildcard: boolean
}

function toModelPattern(model: string): ModelPattern {
  const lower = model.toLowerCase()
  const wildcard = lower.endsWith('*')
  return {
    pattern: model,
    prefix: wildcard ? lower.slice(0, -1) : lower,
    wildcard,
  }
}

function patternsConflict(a: ModelPattern, b: ModelPattern): boolean {
  if (!a.wildcard && !b.wildcard) return a.prefix === b.prefix
  if (a.wildcard && !b.wildcard) return b.prefix.startsWith(a.prefix)
  if (!a.wildcard && b.wildcard) return a.prefix.startsWith(b.prefix)
  // 双通配符：任一前缀是另一前缀的前缀即冲突
  return a.prefix.startsWith(b.prefix) || b.prefix.startsWith(a.prefix)
}

/** 检测模型模式列表中的冲突，返回冲突的两个模式名；无冲突返回 null */
export function findModelConflict(models: string[]): [string, string] | null {
  const patterns = models.map(toModelPattern)
  for (let i = 0; i < patterns.length; i++) {
    for (let j = i + 1; j < patterns.length; j++) {
      if (patternsConflict(patterns[i], patterns[j])) {
        return [patterns[i].pattern, patterns[j].pattern]
      }
    }
  }
  return null
}

// ── 区间校验 ──────────────────────────────────────────────

/** 校验区间列表的合法性，返回错误消息；通过则返回 null */
export function validateIntervals(intervals: IntervalFormEntry[]): string | null {
  if (!intervals || intervals.length === 0) return null

  // 按 min_tokens 排序（不修改原数组）
  const sorted = [...intervals].sort((a, b) => a.min_tokens - b.min_tokens)

  for (let i = 0; i < sorted.length; i++) {
    const err = validateSingleInterval(sorted[i], i)
    if (err) return err
  }
  return checkIntervalOverlap(sorted)
}

function validateSingleInterval(iv: IntervalFormEntry, idx: number): string | null {
  if (iv.min_tokens < 0) {
    return `区间 #${idx + 1}: 最小 token 数 (${iv.min_tokens}) 不能为负数`
  }
  if (iv.max_tokens != null) {
    if (iv.max_tokens <= 0) {
      return `区间 #${idx + 1}: 最大 token 数 (${iv.max_tokens}) 必须大于 0`
    }
    if (iv.max_tokens <= iv.min_tokens) {
      return `区间 #${idx + 1}: 最大 token 数 (${iv.max_tokens}) 必须大于最小 token 数 (${iv.min_tokens})`
    }
  }
  return validateIntervalPrices(iv, idx)
}

function validateIntervalPrices(iv: IntervalFormEntry, idx: number): string | null {
  const prices: [string, number | string | null][] = [
    ['输入价格', iv.input_price],
    ['输出价格', iv.output_price],
    ['缓存写入价格', iv.cache_write_price],
    ['缓存读取价格', iv.cache_read_price],
    ['单次价格', iv.per_request_price],
  ]
  for (const [name, val] of prices) {
    if (val != null && val !== '' && Number(val) < 0) {
      return `区间 #${idx + 1}: ${name}不能为负数`
    }
  }
  return null
}

function checkIntervalOverlap(sorted: IntervalFormEntry[]): string | null {
  for (let i = 0; i < sorted.length; i++) {
    // 无上限区间必须是最后一个
    if (sorted[i].max_tokens == null && i < sorted.length - 1) {
      return `区间 #${i + 1}: 无上限区间（最大 token 数为空）只能是最后一个`
    }
    if (i === 0) continue
    const prev = sorted[i - 1]
    // (min, max] 语义：前一个区间上界 > 当前区间下界则重叠
    if (prev.max_tokens == null || prev.max_tokens > sorted[i].min_tokens) {
      const prevMax = prev.max_tokens == null ? '∞' : String(prev.max_tokens)
      return `区间 #${i} 和 #${i + 1} 重叠：前一个区间上界 (${prevMax}) 大于当前区间下界 (${sorted[i].min_tokens})`
    }
  }
  return null
}

/** 平台对应的模型 tag 样式（背景+文字） */
export function getPlatformTagClass(platform: string): string {
  switch (platform) {
    case 'anthropic': return 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400'
    case 'openai': return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400'
    case 'gemini': return 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400'
    case 'antigravity': return 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400'
    case 'grok': return 'bg-zinc-100 text-zinc-800 dark:bg-zinc-900/30 dark:text-zinc-200'
    case 'opencode': return 'bg-cyan-100 text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-400'
    default: return 'bg-gray-100 text-gray-700 dark:bg-gray-900/30 dark:text-gray-400'
  }
}

/** 平台对应的模型文字色（仅 text-*，用于 input/text 场景）— 与 getPlatformTagClass 同色系 */
export function getPlatformTextClass(platform: string): string {
  switch (platform) {
    case 'anthropic': return 'text-orange-700 dark:text-orange-400'
    case 'openai': return 'text-emerald-700 dark:text-emerald-400'
    case 'gemini': return 'text-blue-700 dark:text-blue-400'
    case 'antigravity': return 'text-purple-700 dark:text-purple-400'
    case 'grok': return 'text-zinc-800 dark:text-zinc-200'
    case 'opencode': return 'text-cyan-700 dark:text-cyan-400'
    default: return ''
  }
}
