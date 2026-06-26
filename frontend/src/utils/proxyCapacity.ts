import type { Proxy } from '@/types'

export function normalizeProxyAccountCount(proxy: Proxy | null | undefined): number {
  const value = Number(proxy?.account_count ?? 0)
  if (!Number.isFinite(value) || value <= 0) return 0
  return Math.floor(value)
}

export function normalizeProxyMaxAccounts(proxy: Proxy | null | undefined): number {
  const value = Number(proxy?.max_accounts ?? 0)
  if (!Number.isFinite(value) || value <= 0) return 0
  return Math.floor(value)
}

export function isProxyAccountFull(proxy: Proxy | null | undefined): boolean {
  const maxAccounts = normalizeProxyMaxAccounts(proxy)
  return maxAccounts > 0 && normalizeProxyAccountCount(proxy) >= maxAccounts
}
