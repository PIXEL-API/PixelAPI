import { getModelsByPlatform } from '@/composables/useModelWhitelist'
import { OPENAI_WS_MODE_OFF } from '@/utils/openaiWsMode'
import type { AccountPlatform, OpenAICompactMode } from '@/types'

export const PERSONAL_ACCOUNT_MIN_CONCURRENCY = 3
export const PERSONAL_ACCOUNT_MAX_CONCURRENCY = 50
export const PERSONAL_ACCOUNT_DEFAULT_CONCURRENCY = PERSONAL_ACCOUNT_MIN_CONCURRENCY
export const PERSONAL_ACCOUNT_MIN_LOAD_FACTOR = 1
export const PERSONAL_ACCOUNT_MAX_LOAD_FACTOR = 10000
export const PERSONAL_ACCOUNT_DEFAULT_LOAD_FACTOR = 10
export const PERSONAL_ACCOUNT_DEFAULT_PRIORITY = 1
export const PERSONAL_ACCOUNT_DEFAULT_AUTO_PAUSE_ON_EXPIRED = true
export const PERSONAL_ACCOUNT_IMPORT_LIMIT = 100

export const PERSONAL_ACCOUNT_DEFAULT_OPENAI_COMPACT_MODE: OpenAICompactMode = 'force_on'
export const PERSONAL_ACCOUNT_DEFAULT_OPENAI_WS_MODE = OPENAI_WS_MODE_OFF

export function normalizePersonalAccountConcurrency(value: unknown): number {
  const numericValue = Number(value)
  if (!Number.isFinite(numericValue)) {
    return PERSONAL_ACCOUNT_DEFAULT_CONCURRENCY
  }
  return Math.min(
    PERSONAL_ACCOUNT_MAX_CONCURRENCY,
    Math.max(PERSONAL_ACCOUNT_MIN_CONCURRENCY, Math.trunc(numericValue))
  )
}

export function normalizePersonalAccountLoadFactor(value: unknown): number {
  const numericValue = Number(value)
  if (!Number.isFinite(numericValue)) {
    return PERSONAL_ACCOUNT_DEFAULT_LOAD_FACTOR
  }
  return Math.min(
    PERSONAL_ACCOUNT_MAX_LOAD_FACTOR,
    Math.max(PERSONAL_ACCOUNT_MIN_LOAD_FACTOR, Math.trunc(numericValue))
  )
}

export function buildPersonalAccountModelMapping(platform: AccountPlatform | string): Record<string, string> {
  const mapping: Record<string, string> = {}
  for (const model of getModelsByPlatform(platform)) {
    if (!model.includes('*')) {
      mapping[model] = model
    }
  }
  return mapping
}

export function applyPersonalAccountTemplate(
  platform: AccountPlatform | string,
  credentials: Record<string, unknown>,
  extra?: Record<string, unknown>
): { credentials: Record<string, unknown>; extra?: Record<string, unknown> } {
  const nextCredentials: Record<string, unknown> = {
    ...credentials,
    model_mapping: buildPersonalAccountModelMapping(platform)
  }

  const nextExtra: Record<string, unknown> = { ...(extra || {}) }
  if (platform === 'openai') {
    nextExtra.openai_oauth_responses_websockets_v2_mode = PERSONAL_ACCOUNT_DEFAULT_OPENAI_WS_MODE
    nextExtra.openai_oauth_responses_websockets_v2_enabled = false
    nextExtra.openai_passthrough = false
    nextExtra.openai_oauth_passthrough = false
    nextExtra.codex_cli_only = false
    nextExtra.openai_compact_mode = PERSONAL_ACCOUNT_DEFAULT_OPENAI_COMPACT_MODE
    delete nextCredentials.compact_model_mapping
  }

  return {
    credentials: nextCredentials,
    extra: Object.keys(nextExtra).length > 0 ? nextExtra : undefined
  }
}
