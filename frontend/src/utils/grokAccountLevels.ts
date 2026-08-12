import type { AccountLevel } from '@/types'

export type GrokAccountLevel = 'free' | 'heavy'

export const GROK_ACCOUNT_LEVEL_OPTIONS: ReadonlyArray<{
  value: GrokAccountLevel
  label: string
}> = [
  { value: 'free', label: 'Free' },
  { value: 'heavy', label: 'Heavy' }
]

export function isGrokAccountLevel(level: AccountLevel | string | null | undefined): level is GrokAccountLevel {
  return level === 'free' || level === 'heavy'
}

export function grokAccountLevelLabel(level: AccountLevel | string | null | undefined): string {
  return GROK_ACCOUNT_LEVEL_OPTIONS.find(option => option.value === level)?.label || String(level || '')
}
