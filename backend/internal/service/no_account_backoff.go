package service

import "context"

// NoAccountBackoffLimiter 对"无可用账号"快速失败做 per-(user, group) 退避限流。
//
// 背景：分组内账号全部不可调度时，自动化客户端会以极高频率原样重试，
// 每次请求都空转一遍选号/诊断链路（多次 DB 查询）。该接口在网关入口做硬闸：
// 窗口内失败次数达到阈值后进入退避期，期间请求直接 429，不再进入选号。
//
// 实现必须 fail-open：Redis 异常时一律视为未被限流，不得影响正常请求。
type NoAccountBackoffLimiter interface {
	// CheckBlocked 查询 (user, group) 是否处于退避期。groupID 为 nil 时按 0 归并。
	// blocked=true 时 retryAfterSeconds 为剩余退避秒数（向上取整，至少 1）。
	CheckBlocked(ctx context.Context, userID int64, groupID *int64) (blocked bool, retryAfterSeconds int)

	// RecordFailure 记录一次"无可用账号"失败。窗口内累计达到阈值的那一次调用
	// 返回 blocked=true（阈值跃迁），retryAfterSeconds 为本次退避时长。
	RecordFailure(ctx context.Context, userID int64, groupID *int64) (blocked bool, retryAfterSeconds int)
}
