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
