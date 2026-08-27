export interface AccountMutationVersionChallenge {
  expectedVersions?: Record<string, number>
  missingRequiredVersions: boolean
}

const parsePositiveSafeInteger = (value: unknown): number | null => {
  const parsed = typeof value === 'number' ? value : Number(value)
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : null
}

const parseListingIDs = (metadata: Record<string, unknown>): {
  listingIDs: string[]
  invalid: boolean
} => {
  const raw = metadata.listing_ids
  if (raw === undefined || raw === null || raw === '') {
    return { listingIDs: [], invalid: false }
  }
  if (typeof raw !== 'string') {
    return { listingIDs: [], invalid: true }
  }

  const tokens = raw
    .split(',')
    .map(value => value.trim())
    .filter(Boolean)
  const parsed = tokens.map(parsePositiveSafeInteger)
  return {
    listingIDs: parsed.filter((value): value is number => value !== null).map(String),
    invalid: tokens.length === 0 || parsed.some(value => value === null)
  }
}

const parseExpectedVersions = (raw: unknown): Record<string, number> | undefined => {
  let parsed = raw
  if (typeof raw === 'string') {
    try {
      parsed = JSON.parse(raw)
    } catch {
      return undefined
    }
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return undefined

  const expectedVersions: Record<string, number> = {}
  for (const [rawListingID, rawVersion] of Object.entries(parsed)) {
    const listingID = parsePositiveSafeInteger(rawListingID)
    const version = parsePositiveSafeInteger(rawVersion)
    if (listingID === null || version === null) continue
    expectedVersions[String(listingID)] = version
  }

  return Object.keys(expectedVersions).length > 0 ? expectedVersions : undefined
}

/**
 * Reads the optimistic-lock snapshot returned by ACCOUNT_MUTATION_FORCE_REQUIRED.
 * Room challenges must contain a positive version for every affected listing;
 * public-pool-only challenges intentionally do not carry listing versions.
 */
export const extractAccountMutationVersionChallenge = (
  rawMetadata: unknown
): AccountMutationVersionChallenge => {
  if (!rawMetadata || typeof rawMetadata !== 'object' || Array.isArray(rawMetadata)) {
    return { missingRequiredVersions: false }
  }

  const metadata = rawMetadata as Record<string, unknown>
  const { listingIDs, invalid: invalidListingIDs } = parseListingIDs(metadata)
  const expectedVersions = parseExpectedVersions(metadata.expected_versions)
  const missingRequiredVersions = invalidListingIDs || listingIDs.some(
    listingID => expectedVersions?.[listingID] === undefined
  )

  return {
    expectedVersions,
    missingRequiredVersions
  }
}

export const isConfirmedAccountMutationPayload = (payload: Record<string, unknown> | null) =>
  payload?.force_active_edit === true && payload?.confirmed === true
