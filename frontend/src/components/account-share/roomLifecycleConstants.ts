/**
 * 房间生命周期相关的共享常量。
 *
 * 这两项被两处独立功能共用：RoomLifecycleDialog 的房间操作轮询，以及
 * AccountShareView 中「成员退出结算」的轮询与错误提示。房间弹窗拆分为独立组件后
 * 必须保持单一来源，否则两边的终态判定或错误文案会各自漂移。
 */

/** 房间异步操作的终态集合：处于这些状态时应停止轮询。 */
export const ROOM_LIFECYCLE_TERMINAL_OPERATION_STATUSES = new Set([
  'succeeded',
  'failed',
  'cancelled'
])

/** 房间生命周期错误码到用户可读文案的映射。 */
export const ROOM_LIFECYCLE_ERROR_MESSAGES: Record<string, string> = {
  ACCOUNT_SHARE_ROOM_VERSION_CONFLICT: '房间状态刚刚发生变化，请刷新后重新确认本次操作。',
  ACCOUNT_SHARE_ROOM_OPERATION_CONFLICT: '房间已有一个生命周期操作正在执行，请等待它结束后再试。',
  ACCOUNT_SHARE_ROOM_INVALID_TRANSITION: '当前房间状态不允许执行该操作，请刷新后查看最新状态。',
  ACCOUNT_SHARE_ROOM_DELETE_BLOCKED: '房间仍有使用、请求或结算阻塞项，暂时不能删除。',
  ACCOUNT_SHARE_ROOM_REVIEW_IDENTITY_MISSING: '房间存在可评价的历史记录，但账号邮箱身份尚未固化。请先刷新或重新授权房间账号后再删除。',
  ACCOUNT_SHARE_ROOM_DELETION_TOKEN_INVALID: '删除确认已失效或房间状态已变化，请重新检查删除条件。',
  ACCOUNT_SHARE_ROOM_DELETED: '房间已经删除，无需重复操作。',
  ACCOUNT_SHARE_LISTING_EDITING: '房间配置仍在编辑中，请先关闭编辑窗口或等待编辑会话失效。',
  ACCOUNT_SHARE_ROOM_VALIDATION_FAILED: '房间恢复校验未通过，请检查房间账号状态后重试。',
  ACCOUNT_SHARE_RUNTIME_DEPENDENCY_UNAVAILABLE: '运行时状态暂时不可用，为保护历史与结算安全，当前操作已停止。'
}
