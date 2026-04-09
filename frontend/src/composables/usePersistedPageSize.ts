import { getConfiguredTableDefaultPageSize, normalizeTablePageSize } from '@/utils/tablePreferences'

const STORAGE_KEY = 'table-page-size'
const SOURCE_KEY = 'table-page-size-source'

/**
 * 从 localStorage 读取/写入 pageSize
 * 全局共享一个 key，所有表格统一偏好
 */
export function getPersistedPageSize(fallback = getConfiguredTableDefaultPageSize()): number {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored) {
      return normalizeTablePageSize(stored)
    }
  } catch {
    // localStorage 不可用（隐私模式等）
  }
  return normalizeTablePageSize(fallback)
}

export function setPersistedPageSize(size: number): void {
  try {
    localStorage.setItem(STORAGE_KEY, String(normalizeTablePageSize(size)))
    localStorage.setItem(SOURCE_KEY, 'user')
  } catch {
    // 静默失败
  }
}

export function syncPersistedPageSizeWithSystemDefault(defaultSize = getConfiguredTableDefaultPageSize()): void {
  try {
    const normalizedDefault = normalizeTablePageSize(defaultSize)
    const stored = localStorage.getItem(STORAGE_KEY)
    const source = localStorage.getItem(SOURCE_KEY)
    const normalizedStored = stored ? normalizeTablePageSize(stored) : null

    if ((source === 'user' || (source === null && stored !== null)) && stored) {
      localStorage.setItem(STORAGE_KEY, String(normalizedStored ?? normalizedDefault))
      localStorage.setItem(SOURCE_KEY, 'user')
      return
    }

    localStorage.setItem(STORAGE_KEY, String(normalizedDefault))
    localStorage.setItem(SOURCE_KEY, 'system')
  } catch {
    // 静默失败
  }
}
