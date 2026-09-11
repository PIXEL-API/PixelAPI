import type { CNProviderObservedLimit, CNProviderQuotaWindow } from '@/types'

export function applyInterceptWarmup(
  credentials: Record<string, unknown>,
  enabled: boolean,
  mode: 'create' | 'edit'
): void {
  if (enabled) {
    credentials.intercept_warmup_requests = true
  } else if (mode === 'edit') {
    delete credentials.intercept_warmup_requests
  }
}

export const HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY = 'header_override_enabled'
export const HEADER_OVERRIDES_CREDENTIAL_KEY = 'header_overrides'

export interface HeaderOverrideRow {
  name: string
  value: string
}

export function isHeaderOverrideCapable(platform: string, type: string): boolean {
  if (platform === 'anthropic' || platform === 'openai') {
    return type === 'apikey'
  }
  return platform === 'grok' && (type === 'apikey' || type === 'oauth')
}

export function cnQuotaCellVisible(platform: string, accountMode: string): boolean {
  return (platform === 'kimi' || platform === 'zhipu' || platform === 'minimax' || platform === 'qwen') && accountMode === 'coding'
}

export function cnBalanceCellVisible(platform: string, accountMode: string): boolean {
  return (platform === 'kimi' || platform === 'deepseek') && accountMode !== 'coding'
}

export type CnAccountMode = 'payg' | 'coding'
export type CnProviderPlatform = 'kimi' | 'zhipu' | 'deepseek' | 'minimax' | 'qwen'
export type CnApiProtocol = 'adaptive' | 'chat_completions' | 'anthropic' | 'responses'
export type QwenCodingQuotaWindow = CNProviderQuotaWindow
export type QwenCodingLimitObservation = CNProviderObservedLimit

const QWEN_CODING_QUOTA_WINDOWS: QwenCodingQuotaWindow[] = ['5h', 'weekly', 'monthly']

/** Read upstream limit observations persisted in an account extra snapshot. */
export function readQwenCodingLimitObservations(
  extra: Record<string, unknown> | null | undefined
): QwenCodingLimitObservation[] {
  if (!extra) return []
  return QWEN_CODING_QUOTA_WINDOWS.flatMap((window) => {
    const value = extra[`qwen_coding_${window}_limit`]
    if (!value || typeof value !== 'object' || Array.isArray(value)) return []
    const observation = value as Record<string, unknown>
    if (observation.window !== window || typeof observation.observed_at !== 'string') return []
    if (Number.isNaN(Date.parse(observation.observed_at))) return []
    if (observation.retry_at !== undefined && typeof observation.retry_at !== 'string') return []
    return [{
      window,
      observed_at: observation.observed_at,
      ...(typeof observation.retry_at === 'string' ? { retry_at: observation.retry_at } : {})
    }]
  })
}

export function cnSupportsNativeResponses(platform: string): boolean { return platform === 'kimi' || platform === 'deepseek' || platform === 'minimax' }
export function defaultCNBaseUrl(platform: string, mode: CnAccountMode, protocol: CnApiProtocol): string {
  if (protocol === 'anthropic') {
    if (platform === 'kimi') return mode === 'coding' ? 'https://api.kimi.com/coding' : 'https://api.moonshot.cn/anthropic'
    if (platform === 'zhipu') return 'https://open.bigmodel.cn/api/anthropic'
    if (platform === 'deepseek') return 'https://api.deepseek.com/anthropic'
    if (platform === 'minimax') return 'https://api.minimaxi.com/anthropic'
    if (platform === 'qwen') return mode === 'coding' ? 'https://coding.dashscope.aliyuncs.com/apps/anthropic' : 'https://dashscope.aliyuncs.com/apps/anthropic'
  }
  if (platform === 'kimi') return mode === 'coding' ? 'https://api.kimi.com/coding/v1' : 'https://api.moonshot.cn/v1'
  if (platform === 'zhipu') return mode === 'coding' ? 'https://open.bigmodel.cn/api/coding/paas/v4' : 'https://open.bigmodel.cn/api/paas/v4'
  if (platform === 'deepseek') return 'https://api.deepseek.com'
  if (platform === 'minimax') return 'https://api.minimaxi.com/v1'
  if (platform === 'qwen') return mode === 'coding' ? 'https://coding.dashscope.aliyuncs.com/v1' : 'https://dashscope.aliyuncs.com/compatible-mode/v1'
  return ''
}
export function defaultCNAdaptiveBaseUrls(platform: CnProviderPlatform, mode: CnAccountMode): Record<'chat_completions' | 'anthropic' | 'responses', string> {
  return {
    chat_completions: defaultCNBaseUrl(platform, mode, 'chat_completions'),
    anthropic: defaultCNBaseUrl(platform, mode, 'anthropic'),
    responses: cnSupportsNativeResponses(platform) ? defaultCNBaseUrl(platform, mode, 'responses') : ''
  }
}

const HEADER_OVERRIDE_BLOCKED_NAMES = new Set([
  'host',
  'content-length',
  'content-type',
  'transfer-encoding',
  'connection',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'proxy-connection',
  'te',
  'trailer',
  'upgrade',
  'authorization',
  'x-api-key',
  'x-goog-api-key',
  'cookie',
  'accept-encoding',
  'sec-websocket-key',
  'sec-websocket-version',
  'sec-websocket-extensions',
  'sec-websocket-protocol',
  'sec-websocket-accept',
  'session_id',
  'conversation_id',
  'x-codex-turn-state',
  'x-codex-turn-metadata',
  'chatgpt-account-id',
  'x-claude-code-session-id',
  'x-client-request-id',
  'x-grok-conv-id'
])

const HEADER_NAME_PATTERN = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/
const HEADER_OVERRIDE_MAX_ENTRIES = 64
const HEADER_OVERRIDE_MAX_NAME_LENGTH = 200
const HEADER_OVERRIDE_MAX_VALUE_LENGTH = 8192
// eslint-disable-next-line no-control-regex
const HEADER_VALUE_INVALID_PATTERN = /[\x00-\x08\x0a-\x1f\x7f]/
const HEADER_TEXT_ENCODER = new TextEncoder()

export function validateHeaderOverrideRows(
  rows: HeaderOverrideRow[]
): 'invalidName' | 'blockedName' | 'duplicateName' | 'invalidValue' | 'tooManyEntries' | null {
  const seen = new Set<string>()
  for (const row of rows) {
    const name = row.name.trim()
    const value = row.value.trim()
    if (!name) {
      if (value) return 'invalidName'
      continue
    }
    if (!HEADER_NAME_PATTERN.test(name) || name.length > HEADER_OVERRIDE_MAX_NAME_LENGTH) {
      return 'invalidName'
    }
    const normalizedName = name.toLowerCase()
    if (HEADER_OVERRIDE_BLOCKED_NAMES.has(normalizedName)) return 'blockedName'
    if (seen.has(normalizedName)) return 'duplicateName'
    if (
      HEADER_VALUE_INVALID_PATTERN.test(value) ||
      HEADER_TEXT_ENCODER.encode(value).length > HEADER_OVERRIDE_MAX_VALUE_LENGTH
    ) {
      return 'invalidValue'
    }
    seen.add(normalizedName)
  }
  return seen.size > HEADER_OVERRIDE_MAX_ENTRIES ? 'tooManyEntries' : null
}

export function buildHeaderOverridesObject(rows: HeaderOverrideRow[]): Record<string, string> {
  const result: Record<string, string> = {}
  for (const row of rows) {
    const name = row.name.trim().toLowerCase()
    if (name) result[name] = row.value.trim()
  }
  return result
}

export function splitHeaderOverridesObject(record: unknown): HeaderOverrideRow[] {
  if (!record || typeof record !== 'object' || Array.isArray(record)) return []
  return Object.entries(record as Record<string, unknown>)
    .filter(([, value]) => typeof value === 'string')
    .map(([name, value]) => ({ name, value: value as string }))
    .sort((left, right) => left.name.localeCompare(right.name))
}

export function parseHeaderOverridesJson(text: string): HeaderOverrideRow[] | null {
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    return null
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return null
  const rows: HeaderOverrideRow[] = []
  for (const [rawName, rawValue] of Object.entries(parsed as Record<string, unknown>)) {
    const name = rawName.trim()
    if (!name) continue
    if (
      typeof rawValue !== 'string' &&
      typeof rawValue !== 'number' &&
      typeof rawValue !== 'boolean'
    ) {
      return null
    }
    rows.push({ name, value: String(rawValue).trim() })
  }
  return rows.sort((left, right) => left.name.localeCompare(right.name))
}

export function serializeHeaderOverrideRows(rows: HeaderOverrideRow[]): string {
  const record: Record<string, string> = {}
  for (const row of rows) {
    const name = row.name.trim()
    if (name) record[name] = row.value.trim()
  }
  return JSON.stringify(record, null, 2)
}

const GROK_DEFAULT_GATEWAY_HOST = 'cli-chat-proxy.grok.com'

export function isCustomGrokBaseUrl(value: unknown): boolean {
  if (typeof value !== 'string' || !value.trim()) return false
  try {
    return new URL(value.trim()).hostname.toLowerCase() !== GROK_DEFAULT_GATEWAY_HOST
  } catch {
    return false
  }
}

export interface GrokBaseUrlPreset {
  labelKey?: 'cli' | 'official'
  label?: string
  url: string
}

export const GROK_BASE_URL_PRESETS: GrokBaseUrlPreset[] = [
  { labelKey: 'cli', url: 'https://cli-chat-proxy.grok.com/v1' },
  { labelKey: 'official', url: 'https://api.x.ai/v1' },
  { label: 'us-east-1', url: 'https://us-east-1.api.x.ai/v1' },
  { label: 'us-west-2', url: 'https://us-west-2.api.x.ai/v1' },
  { label: 'eu-west-1', url: 'https://eu-west-1.api.x.ai/v1' }
]

export function applyHeaderOverride(
  credentials: Record<string, unknown>,
  enabled: boolean,
  rows: HeaderOverrideRow[],
  mode: 'create' | 'edit'
): void {
  if (enabled) {
    credentials[HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY] = true
    credentials[HEADER_OVERRIDES_CREDENTIAL_KEY] = buildHeaderOverridesObject(rows)
  } else if (mode === 'edit') {
    delete credentials[HEADER_OVERRIDE_ENABLED_CREDENTIAL_KEY]
    delete credentials[HEADER_OVERRIDES_CREDENTIAL_KEY]
  }
}

/** Read a manually overridden OpenAI plan_type, rejecting non-string credential data. */
export function readPlanType(
  credentials: Record<string, unknown> | null | undefined
): string {
  const value = credentials?.plan_type
  return typeof value === 'string' ? value.trim() : ''
}

/**
 * Apply a manual OpenAI plan_type override while preserving all other credentials.
 * An empty value removes the override so later OAuth refreshes can detect the tier again.
 */
export function applyPlanType(
  credentials: Record<string, unknown>,
  planType: string
): void {
  const normalizedPlanType = planType.trim()
  if (normalizedPlanType) {
    credentials.plan_type = normalizedPlanType
    return
  }
  delete credentials.plan_type
}
