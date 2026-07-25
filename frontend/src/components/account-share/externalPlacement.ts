import type {
  Account,
  AccountExternalPlacementTarget
} from '@/types'

export function resolveAccountExternalPlacementTarget(
  account: Account | null | undefined
): AccountExternalPlacementTarget {
  const target = account?.external_placement?.target
  if (target) return target
  if (
    Number(account?.account_share_mode_listing_id || 0) > 0
    || account?.extra?.account_share_mode === true
    || account?.extra?.account_share_mode === 'true'
  ) {
    return 'room'
  }
  return account?.share_mode === 'public' ? 'public_pool' : 'private'
}
