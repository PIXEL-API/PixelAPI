import type { AccountShareRoomOperation } from '@/api/accountShare'

export function accountShareOperationWaitReason(
  operation: Pick<AccountShareRoomOperation, 'blocker' | 'error_message'>
): string {
  if (operation.error_message) return operation.error_message
  const blocker = operation.blocker || {}
  switch (blocker.code) {
    case 'in_flight_requests': {
      const count = Number(blocker.in_flight_request_count)
      return Number.isFinite(count) && count > 0
        ? `仍有 ${count} 个请求尚未释放，释放后继续结算。`
        : '正在等待请求释放，然后继续结算。'
    }
    case 'runtime_dependency_unavailable':
      return '暂时无法核对运行中请求，后台会继续重试。'
    case 'settlement_error':
      return '本轮结算未完成，后台会继续重试。'
    default:
      return ''
  }
}

export function accountShareOperationWaitDuration(createdAt: string | undefined, nowMs: number): string {
  const start = Date.parse(createdAt || '')
  if (!Number.isFinite(start)) return '等待时长暂不可用'
  const seconds = Math.max(0, Math.floor((nowMs - start) / 1000))
  if (seconds < 60) return `已等待 ${seconds} 秒`
  if (seconds < 3600) return `已等待 ${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`
  return `已等待 ${Math.floor(seconds / 3600)} 小时 ${Math.floor(seconds % 3600 / 60)} 分`
}
