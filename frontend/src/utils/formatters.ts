/**
 * 格式化缓存 token 数量（1K/1M 缩写）
 */
export function formatCacheTokens(tokens: number): string {
  if (tokens >= 1000000) return `${(tokens / 1000000).toFixed(1)}M`
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}K`
  return tokens.toLocaleString()
}

/**
 * 计算输入侧缓存命中率百分比。
 * usage_logs 的实际输入、缓存创建和缓存读取是三个互斥 Token 桶。
 */
export function calculateCacheHitRate(
  inputTokens?: number | null,
  cacheCreationTokens?: number | null,
  cacheReadTokens?: number | null
): number {
  const input = Math.max(0, Number(inputTokens) || 0)
  const cacheCreation = Math.max(0, Number(cacheCreationTokens) || 0)
  const cacheRead = Math.max(0, Number(cacheReadTokens) || 0)
  const totalInputSide = input + cacheCreation + cacheRead
  if (totalInputSide <= 0) return 0

  const percentage = cacheRead / totalInputSide * 100
  return Number.isFinite(percentage) && percentage > 0 ? percentage : 0
}

/**
 * 格式化输入侧缓存命中率。
 */
export function formatCacheHitRate(
  inputTokens?: number | null,
  cacheCreationTokens?: number | null,
  cacheReadTokens?: number | null
): string {
  const percentage = calculateCacheHitRate(inputTokens, cacheCreationTokens, cacheReadTokens)
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
