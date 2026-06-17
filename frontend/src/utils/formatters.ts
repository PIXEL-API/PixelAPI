/**
 * 格式化缓存 token 数量（1K/1M 缩写）
 */
export function formatCacheTokens(tokens: number): string {
  if (tokens >= 1000000) return `${(tokens / 1000000).toFixed(1)}M`
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}K`
  return tokens.toLocaleString()
}

/**
 * 格式化输入侧缓存命中率：缓存读取 Token /（实际输入 Token + 缓存读取 Token）
 */
export function formatCacheHitRate(inputTokens?: number | null, cacheReadTokens?: number | null): string {
  const input = Math.max(0, Number(inputTokens) || 0)
  const cacheRead = Math.max(0, Number(cacheReadTokens) || 0)
  const totalInputSide = input + cacheRead
  if (totalInputSide <= 0) return '0%'

  const percentage = (cacheRead / totalInputSide) * 100
  if (!Number.isFinite(percentage) || percentage <= 0) return '0%'
  if (percentage < 0.1) return '<0.1%'
  return `${percentage.toFixed(percentage >= 10 ? 1 : 2)}%`
}

/**
 * 自适应精度格式化倍率（确保小数值如 0.001 不被截断）
 */
export function formatMultiplier(val: number): string {
  if (val >= 0.01) return val.toFixed(2)
  if (val >= 0.001) return val.toFixed(3)
  if (val >= 0.0001) return val.toFixed(4)
  return val.toPrecision(2)
}
