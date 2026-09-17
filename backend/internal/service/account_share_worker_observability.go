package service

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"
)

// 账号广场后台 worker 的可观测性与批处理退避。
//
// 包含两块：
//  1. job heartbeat：每个周期性 worker 每轮向 ops 的 job_heartbeats 写入
//     最近运行/成功/失败时间，让停摆（lease 死锁、依赖故障、panic 后未重启）
//     在 Ops 仪表盘上可见，而不是只落在进程日志里。
//  2. item backoff：批处理管线（席位计费、unavailable 结束、idle 结束、
//     waiver 补偿、可恢复 suspend）对单行失败做进程内指数退避，避免一条
//     确定性坏行按相同排序每轮都被重新选中并拖垮整条管线。
//     退避状态是可丢失的运行态，进程重启后清零，失败行会立即重试。

const (
	accountShareItemBackoffBase       = time.Minute
	accountShareItemBackoffMax        = 30 * time.Minute
	accountShareItemBackoffMaxEntries = 4096
	accountShareItemBackoffMaxShift   = 5 // 1m<<5 = 32m，再被 Max 截断

	// 批处理管线 scope，用于区分不同管线里相同的 membership/settlement id。
	// 导出是因为 repository 包内的批处理入口也使用同一套退避语义。
	AccountShareBackoffScopeSeatBilling = "seat_billing"
	AccountShareBackoffScopeUnavailable = "unavailable_end"
	AccountShareBackoffScopeWaiver      = "waiver_compensation"
	AccountShareBackoffScopeIdle        = "idle_end"
	AccountShareBackoffScopeRecoverable = "recoverable_suspend"
)

// accountShareJobHeartbeatSink 是账号广场 worker 依赖的 ops 心跳窄接口，
// 由 OpsRepository 满足。保持窄接口以便测试装配时无需构造完整 ops 仓储。
type accountShareJobHeartbeatSink interface {
	UpsertJobHeartbeat(ctx context.Context, input *OpsUpsertJobHeartbeatInput) error
}

// SetJobHeartbeatSink 注入 ops 心跳记录器；未注入时 worker 照常运行，
// 仅失去仪表盘可观测性。
func (s *AccountShareModeService) SetJobHeartbeatSink(sink accountShareJobHeartbeatSink) {
	if s == nil {
		return
	}
	s.jobHeartbeatSink = sink
}

// recordShareJobHeartbeat 记录一轮 worker 运行结果。心跳写失败只记日志，
// 不反哺业务错误；使用独立的短超时 context，避免继承已被取消的 worker ctx。
func (s *AccountShareModeService) recordShareJobHeartbeat(jobName string, startedAt time.Time, result string, runErr error) {
	if s == nil || s.jobHeartbeatSink == nil || jobName == "" {
		return
	}
	finishedAt := time.Now().UTC()
	durationMs := finishedAt.Sub(startedAt).Milliseconds()
	if durationMs < 0 {
		durationMs = 0
	}
	runAt := startedAt.UTC()
	input := &OpsUpsertJobHeartbeatInput{
		JobName:        jobName,
		LastRunAt:      &runAt,
		LastDurationMs: &durationMs,
	}
	if runErr != nil {
		errText := runErr.Error()
		if len(errText) > 512 {
			errText = errText[:512]
		}
		input.LastErrorAt = &finishedAt
		input.LastError = &errText
		errResult := "error"
		if result != "" {
			errResult = result
		}
		input.LastResult = &errResult
	} else {
		if result == "" {
			result = "ok"
		}
		input.LastSuccessAt = &finishedAt
		input.LastResult = &result
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.jobHeartbeatSink.UpsertJobHeartbeat(ctx, input); err != nil {
		log.Printf("account_share_mode: record job heartbeat failed: job=%s err=%v", jobName, err)
	}
}

// accountShareWorkerRound 聚合一轮 worker 运行的扫描/失败计数与步骤错误，
// 用于 heartbeat 的 result 摘要。
type accountShareWorkerRound struct {
	scanned    int
	failed     int
	stepErrors []string
}

func (r *accountShareWorkerRound) observe(result *AccountShareSeatBillingResult) {
	if r == nil || result == nil {
		return
	}
	r.scanned += result.Processed
	r.failed += len(result.ItemFailures)
}

func (r *accountShareWorkerRound) add(scanned, failed int) {
	if r == nil {
		return
	}
	r.scanned += scanned
	r.failed += failed
}

// noteStepError 记录子步骤级错误（不中断本轮其余步骤，但进入心跳摘要）。
func (r *accountShareWorkerRound) noteStepError(step string, err error) {
	if r == nil || err == nil {
		return
	}
	r.stepErrors = append(r.stepErrors, fmt.Sprintf("%s: %v", step, err))
}

func (r *accountShareWorkerRound) summary() string {
	if r == nil {
		return "ok"
	}
	switch {
	case len(r.stepErrors) > 0:
		return fmt.Sprintf("partial scanned=%d item_failures=%d step_errors=%d first_step_error=%q",
			r.scanned, r.failed, len(r.stepErrors), truncateAccountShareLogText(r.stepErrors[0], 200))
	case r.failed > 0:
		return fmt.Sprintf("partial scanned=%d item_failures=%d", r.scanned, r.failed)
	default:
		return fmt.Sprintf("ok scanned=%d", r.scanned)
	}
}

func truncateAccountShareLogText(text string, max int) string {
	if len(text) <= max {
		return text
	}
	return text[:max]
}

type accountShareBackoffEntry struct {
	failures int
	until    time.Time
}

// AccountShareItemBackoff 是账号广场批处理管线的进程内存退避表。
// 单行连续失败按 1m<<failures 指数推迟重试（上限 30m）；成功后清除。
// 只对后台 worker 批处理生效；请求路径（ForJoin/ForRequest）不退避，
// 保证瞬时故障仍能随用户请求自愈。
type AccountShareItemBackoff struct {
	mu      sync.Mutex
	entries map[string]accountShareBackoffEntry
}

func NewAccountShareItemBackoff() *AccountShareItemBackoff {
	return &AccountShareItemBackoff{entries: make(map[string]accountShareBackoffEntry)}
}

func accountShareBackoffKey(scope string, id int64) string {
	return scope + ":" + strconv.FormatInt(id, 10)
}

// Allow 报告条目本轮是否可被处理；nil 接收者放行一切（未装配时无退避）。
func (b *AccountShareItemBackoff) Allow(scope string, id int64, now time.Time) bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.evictLocked(now)
	entry, ok := b.entries[accountShareBackoffKey(scope, id)]
	return !ok || !now.Before(entry.until)
}

func (b *AccountShareItemBackoff) OnSuccess(scope string, id int64) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.entries, accountShareBackoffKey(scope, id))
}

// OnFailure 记录一次失败并返回本轮生效的退避时长（便于日志）。
func (b *AccountShareItemBackoff) OnFailure(scope string, id int64, now time.Time) time.Duration {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.evictLocked(now)
	key := accountShareBackoffKey(scope, id)
	entry := b.entries[key]
	entry.failures++
	shift := entry.failures - 1
	if shift > accountShareItemBackoffMaxShift {
		shift = accountShareItemBackoffMaxShift
	}
	delay := accountShareItemBackoffBase << shift
	if delay > accountShareItemBackoffMax {
		delay = accountShareItemBackoffMax
	}
	entry.until = now.Add(delay)
	b.entries[key] = entry
	return delay
}

func (b *AccountShareItemBackoff) evictLocked(now time.Time) {
	if len(b.entries) <= accountShareItemBackoffMaxEntries {
		return
	}
	for key, entry := range b.entries {
		if !now.Before(entry.until) {
			delete(b.entries, key)
		}
	}
	// 仍超限（全是活跃退避）时直接重建：退避状态丢失只意味着提前重试，
	// 不会漏处理或错处理。
	if len(b.entries) > accountShareItemBackoffMaxEntries {
		b.entries = make(map[string]accountShareBackoffEntry, 1024)
	}
}
