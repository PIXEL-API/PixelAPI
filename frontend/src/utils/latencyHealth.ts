export type LatencySeverity = 'good' | 'warn' | 'slow' | 'critical'

export const FIRST_TOKEN_THRESHOLDS_MS = {
  warn: 10_000,
  slow: 30_000,
  critical: 60_000
} as const

export const DURATION_THRESHOLDS_MS = {
  warn: 60_000,
  slow: 180_000,
  critical: 300_000
} as const

interface Thresholds {
  warn: number
  slow: number
  critical: number
}

const classify = (ms: number, thresholds: Thresholds): LatencySeverity => {
  if (ms >= thresholds.critical) return 'critical'
  if (ms >= thresholds.slow) return 'slow'
  if (ms >= thresholds.warn) return 'warn'
  return 'good'
}

export const firstTokenSeverity = (ms: number): LatencySeverity =>
  classify(ms, FIRST_TOKEN_THRESHOLDS_MS)

export const durationSeverity = (ms: number): LatencySeverity =>
  classify(ms, DURATION_THRESHOLDS_MS)

export const LATENCY_TEXT_CLASSES: Record<LatencySeverity, string> = {
  good: 'text-emerald-700 dark:text-emerald-400',
  warn: 'text-amber-700 dark:text-amber-400',
  slow: 'text-orange-700 dark:text-orange-400',
  critical: 'text-red-600 dark:text-red-400'
}

export const LATENCY_SEVERITY_LABEL_KEYS: Record<LatencySeverity, string> = {
  good: 'monitorCommon.status.operational',
  warn: 'common.warning',
  slow: 'monitorCommon.status.degraded',
  critical: 'common.critical'
}

export const LATENCY_SEVERITY_SYMBOLS: Record<LatencySeverity, string> = {
  good: '✓',
  warn: '!',
  slow: '…',
  critical: '×'
}

export const LATENCY_BAR_CLASSES: Record<LatencySeverity, string> = {
  good: 'bg-emerald-500',
  warn: 'bg-amber-400',
  slow: 'bg-orange-500',
  critical: 'bg-red-500'
}

export const LATENCY_BAR_FROM_CLASSES: Record<LatencySeverity, string> = {
  good: 'from-emerald-500',
  warn: 'from-amber-400',
  slow: 'from-orange-500',
  critical: 'from-red-500'
}

export const LATENCY_BAR_TO_CLASSES: Record<LatencySeverity, string> = {
  good: 'to-emerald-500',
  warn: 'to-amber-400',
  slow: 'to-orange-500',
  critical: 'to-red-500'
}

export const formatUsageDuration = (milliseconds: number | null | undefined): string => {
  if (milliseconds == null || !Number.isFinite(milliseconds) || milliseconds < 0) return '-'
  if (milliseconds < 1_000) return `${Math.round(milliseconds)}ms`
  if (milliseconds < 60_000) return `${(milliseconds / 1_000).toFixed(2)}s`

  const totalSeconds = Math.floor(milliseconds / 1_000)
  if (totalSeconds < 3_600) {
    return `${Math.floor(totalSeconds / 60)}m ${totalSeconds % 60}s`
  }

  return `${Math.floor(totalSeconds / 3_600)}h ${Math.floor((totalSeconds % 3_600) / 60)}m`
}
