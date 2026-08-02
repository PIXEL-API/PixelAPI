/**
 * 请求安全相关的通用助手。
 *
 * 这些函数原先内联在 AccountShareView.vue 中；房间生命周期弹窗拆分为独立组件后
 * 由父子双方共用，故提取到此处，避免出现两份行为可能漂移的副本。
 */

/**
 * 判断错误是否来自被主动取消/中止的请求。
 * axios 取消用 ERR_CANCELED / CanceledError，原生 AbortController 用 AbortError。
 */
export function isCanceledRequest(error: unknown): boolean {
  if (typeof error !== 'object' || error === null) return false
  const maybeCanceled = error as { code?: string; name?: string }
  return maybeCanceled.code === 'ERR_CANCELED' || maybeCanceled.name === 'CanceledError' || maybeCanceled.name === 'AbortError'
}

/** 把可能为空或非法的时间字符串规范为 Date，无法解析时返回 null。 */
export function normalizeDateInput(value?: string | null): Date | null {
  if (!value) return null
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? null : date
}

/**
 * 生成幂等键用的安全随机 ID。
 * 不静默降级到 Math.random——幂等键碰撞会造成重复的写操作。
 */
export function createSecureRequestID(): string {
  const requestID = globalThis.crypto?.randomUUID?.()
  if (!requestID) {
    throw new Error('当前浏览器无法生成安全的幂等键，请升级浏览器后重试。')
  }
  return requestID
}
