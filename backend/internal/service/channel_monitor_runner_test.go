//go:build unit

package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stubMonitorSvc 实现 monitorRunnerSvc，用于隔离 runner 与真实 service/repo。
type stubMonitorSvc struct {
	enabled    []*ChannelMonitor
	runCount   atomic.Int64
	runCalled  chan int64 // 每次 RunCheck 触发时 push 一次（缓冲足够大避免阻塞）
	runGuard   chan *ClusterLeaseGuard
	runDone    chan struct{}
	runErr     error
	listErr    error
	runHoldFor time.Duration // RunCheck 内额外阻塞的时长，用来测试 Stop 等待行为
	mu         sync.RWMutex
}

func (s *stubMonitorSvc) ListEnabledMonitors(_ context.Context) ([]*ChannelMonitor, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.listErr != nil {
		return nil, s.listErr
	}
	return append([]*ChannelMonitor(nil), s.enabled...), nil
}

func (s *stubMonitorSvc) runScheduledCheck(
	ctx context.Context,
	id int64,
	guard *ClusterLeaseGuard,
) ([]*CheckResult, error) {
	if s.runDone != nil {
		defer func() {
			select {
			case s.runDone <- struct{}{}:
			default:
			}
		}()
	}
	s.runCount.Add(1)
	if s.runCalled != nil {
		select {
		case s.runCalled <- id:
		default:
		}
	}
	if s.runGuard != nil {
		select {
		case s.runGuard <- guard:
		default:
		}
	}
	if s.runHoldFor > 0 {
		select {
		case <-time.After(s.runHoldFor):
		case <-ctx.Done():
		}
	}
	return nil, s.runErr
}

func (s *stubMonitorSvc) setEnabled(enabled []*ChannelMonitor) {
	s.mu.Lock()
	s.enabled = enabled
	s.mu.Unlock()
}

type stubClusterMonitorTaskExecutor struct {
	ownsLease bool
	guard     *ClusterLeaseGuard
	runCalls  atomic.Int64
}

func (s *stubClusterMonitorTaskExecutor) Run(
	ctx context.Context,
	_ string,
	task func(context.Context, *ClusterLeaseGuard) error,
) (bool, error) {
	s.runCalls.Add(1)
	if !s.ownsLease {
		return false, nil
	}
	return true, task(ctx, s.guard)
}

func newRunnerForTest(svc monitorRunnerSvc) *ChannelMonitorRunner {
	return newChannelMonitorRunner(svc, nil)
}

// 等待 condition 在 timeout 内变 true，否则 t.Fatalf。轮询 5ms 一次。
func waitFor(t *testing.T, timeout time.Duration, msg string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !cond() {
		t.Fatalf("waitFor timed out: %s", msg)
	}
}

func runnerTaskCount(r *ChannelMonitorRunner) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.tasks)
}

func runnerTaskPtr(r *ChannelMonitorRunner, id int64) *scheduledMonitor {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tasks[id]
}

// TestSchedule_AddsTaskAndFiresOnce 验证 Schedule 后立即触发一次首检测，并把任务记入 tasks 表。
func TestSchedule_AddsTaskAndFiresOnce(t *testing.T) {
	svc := &stubMonitorSvc{runCalled: make(chan int64, 4)}
	r := newRunnerForTest(svc)
	r.Start() // svc.enabled 为空，Start 立即完成

	r.Schedule(&ChannelMonitor{ID: 1, Name: "m1", Enabled: true, IntervalSeconds: 60})

	if got := runnerTaskCount(r); got != 1 {
		t.Fatalf("expected 1 scheduled task, got %d", got)
	}

	select {
	case id := <-svc.runCalled:
		if id != 1 {
			t.Fatalf("expected first fire for id=1, got %d", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected immediate first fire within 2s")
	}

	r.Stop()
}

// TestSchedule_ReplaceCancelsOldTask 验证对同一 id 二次 Schedule 会替换旧 task 实例。
// （旧 goroutine 通过 ctx 取消退出；这里以 task 指针不同 + Stop 不超时作为证据。）
func TestSchedule_ReplaceCancelsOldTask(t *testing.T) {
	svc := &stubMonitorSvc{runCalled: make(chan int64, 8)}
	r := newRunnerForTest(svc)
	r.Start()

	m := &ChannelMonitor{ID: 7, Name: "m7", Enabled: true, IntervalSeconds: 60}
	r.Schedule(m)
	first := runnerTaskPtr(r, 7)
	if first == nil {
		t.Fatal("first schedule did not register task")
	}

	r.Schedule(m)
	second := runnerTaskPtr(r, 7)
	if second == nil {
		t.Fatal("second schedule did not register task")
	}
	if first == second {
		t.Fatal("re-Schedule should create a new scheduledMonitor instance")
	}

	stoppedWithin(t, r, 3*time.Second)
}

// TestUnschedule_RemovesTask 验证 Unschedule 删除 task 并使对应 goroutine 退出。
func TestUnschedule_RemovesTask(t *testing.T) {
	svc := &stubMonitorSvc{runCalled: make(chan int64, 4)}
	r := newRunnerForTest(svc)
	r.Start()

	r.Schedule(&ChannelMonitor{ID: 3, Enabled: true, IntervalSeconds: 60})
	waitFor(t, time.Second, "task registered", func() bool { return runnerTaskCount(r) == 1 })

	r.Unschedule(3)
	if got := runnerTaskCount(r); got != 0 {
		t.Fatalf("expected tasks empty after Unschedule, got %d", got)
	}

	stoppedWithin(t, r, 3*time.Second)
}

// TestSchedule_DisabledRedirectsToUnschedule 验证 Enabled=false 等同于 Unschedule。
func TestSchedule_DisabledRedirectsToUnschedule(t *testing.T) {
	svc := &stubMonitorSvc{runCalled: make(chan int64, 4)}
	r := newRunnerForTest(svc)
	r.Start()

	r.Schedule(&ChannelMonitor{ID: 9, Enabled: true, IntervalSeconds: 60})
	waitFor(t, time.Second, "task registered", func() bool { return runnerTaskCount(r) == 1 })

	r.Schedule(&ChannelMonitor{ID: 9, Enabled: false, IntervalSeconds: 60})
	if got := runnerTaskCount(r); got != 0 {
		t.Fatalf("expected tasks empty after disabled re-Schedule, got %d", got)
	}

	stoppedWithin(t, r, 3*time.Second)
}

// TestSchedule_InvalidIntervalSkipped 验证 IntervalSeconds<=0 不会注册任务（防御性检查）。
func TestSchedule_InvalidIntervalSkipped(t *testing.T) {
	svc := &stubMonitorSvc{}
	r := newRunnerForTest(svc)
	r.Start()

	r.Schedule(&ChannelMonitor{ID: 1, Enabled: true, IntervalSeconds: 0})
	if got := runnerTaskCount(r); got != 0 {
		t.Fatalf("expected no task for invalid interval, got %d", got)
	}
	r.Stop()
}

// TestSchedule_BeforeStartIsNoOp 验证 Start 之前调用 Schedule 不会注册任务。
func TestSchedule_BeforeStartIsNoOp(t *testing.T) {
	svc := &stubMonitorSvc{}
	r := newRunnerForTest(svc)
	// 故意不调用 Start

	r.Schedule(&ChannelMonitor{ID: 1, Enabled: true, IntervalSeconds: 60})
	if got := runnerTaskCount(r); got != 0 {
		t.Fatalf("expected no task before Start, got %d", got)
	}
	r.Stop()
}

// TestStart_LoadsAllEnabledMonitors 验证 Start 会为 ListEnabledMonitors 返回的每条记录建立任务。
func TestStart_LoadsAllEnabledMonitors(t *testing.T) {
	svc := &stubMonitorSvc{
		enabled: []*ChannelMonitor{
			{ID: 1, Enabled: true, IntervalSeconds: 60},
			{ID: 2, Enabled: true, IntervalSeconds: 60},
			{ID: 3, Enabled: true, IntervalSeconds: 60},
		},
	}
	r := newRunnerForTest(svc)
	r.Start()
	waitFor(t, 2*time.Second, "all 3 tasks scheduled", func() bool { return runnerTaskCount(r) == 3 })

	stoppedWithin(t, r, 3*time.Second)
}

// TestStop_DrainsAllGoroutines 验证 Stop 会等待所有调度 goroutine 退出（无游离）。
func TestStop_DrainsAllGoroutines(t *testing.T) {
	svc := &stubMonitorSvc{}
	r := newRunnerForTest(svc)
	r.Start()

	for id := int64(1); id <= 5; id++ {
		r.Schedule(&ChannelMonitor{ID: id, Enabled: true, IntervalSeconds: 60})
	}
	waitFor(t, 2*time.Second, "5 tasks scheduled", func() bool { return runnerTaskCount(r) == 5 })

	stoppedWithin(t, r, 3*time.Second)
}

// TestStop_WaitsForInFlightCheck 验证 Stop 会取消正在执行的 RunCheck，
// 并等待 worker 实际退出后才返回（pool.StopAndWait）。
func TestStop_WaitsForInFlightCheck(t *testing.T) {
	svc := &stubMonitorSvc{
		runCalled:  make(chan int64, 1),
		runDone:    make(chan struct{}, 1),
		runHoldFor: 200 * time.Millisecond,
	}
	r := newRunnerForTest(svc)
	r.Start()
	r.Schedule(&ChannelMonitor{ID: 1, Enabled: true, IntervalSeconds: 60})

	select {
	case <-svc.runCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("first fire never happened")
	}

	stoppedWithin(t, r, 3*time.Second)
	select {
	case <-svc.runDone:
	default:
		t.Fatal("Stop returned before the in-flight worker exited")
	}
}

// TestInFlight_PoolFullReleasesSlot 直接驱动 fire 路径，模拟 pool.TrySubmit 失败时 inFlight 必须释放。
// 用一个小型 stub pool 替换 r.pool 不便（pond.Pool 是接口但 mock 麻烦），
// 改为：占满 inFlight 后直接 fire，验证不会在 inFlight 空槽时永久卡住。
func TestInFlight_AcquireReleaseSymmetric(t *testing.T) {
	svc := &stubMonitorSvc{}
	r := newRunnerForTest(svc)

	if !r.tryAcquireInFlight(42) {
		t.Fatal("first acquire should succeed")
	}
	if r.tryAcquireInFlight(42) {
		t.Fatal("second acquire (no release) must fail")
	}
	r.releaseInFlight(42)
	if !r.tryAcquireInFlight(42) {
		t.Fatal("acquire after release should succeed")
	}
	r.releaseInFlight(42)
}

func TestClusterScheduler_FollowerDoesNotRunChecks(t *testing.T) {
	svc := &stubMonitorSvc{
		enabled:   []*ChannelMonitor{{ID: 1, Enabled: true, IntervalSeconds: 60}},
		runCalled: make(chan int64, 1),
	}
	executor := &stubClusterMonitorTaskExecutor{ownsLease: false}
	r := newChannelMonitorRunnerWithCluster(svc, nil, executor, true, nil)
	r.clusterLeaseRetryInterval = 10 * time.Millisecond
	r.Start()

	waitFor(t, time.Second, "follower attempted scheduler lease", func() bool {
		return executor.runCalls.Load() >= 2
	})
	r.Schedule(&ChannelMonitor{ID: 2, Enabled: true, IntervalSeconds: 60})
	if got := runnerTaskCount(r); got != 0 {
		t.Fatalf("follower must ignore startup and CRUD schedules, got %d tasks", got)
	}
	select {
	case id := <-svc.runCalled:
		t.Fatalf("follower unexpectedly ran monitor %d", id)
	default:
	}

	stoppedWithin(t, r, 3*time.Second)
}

func TestClusterScheduler_LeaderReconcilesDatabaseChanges(t *testing.T) {
	guard := &ClusterLeaseGuard{}
	svc := &stubMonitorSvc{
		enabled:   []*ChannelMonitor{{ID: 1, Enabled: true, IntervalSeconds: 60}},
		runCalled: make(chan int64, 4),
		runGuard:  make(chan *ClusterLeaseGuard, 4),
	}
	executor := &stubClusterMonitorTaskExecutor{ownsLease: true, guard: guard}
	r := newChannelMonitorRunnerWithCluster(svc, nil, executor, true, nil)
	r.clusterReconcileInterval = 20 * time.Millisecond
	r.clusterDrainCheckInterval = 10 * time.Millisecond
	r.Start()

	select {
	case id := <-svc.runCalled:
		if id != 1 {
			t.Fatalf("expected initial monitor 1, got %d", id)
		}
	case <-time.After(time.Second):
		t.Fatal("leader did not run initial monitor")
	}
	select {
	case got := <-svc.runGuard:
		if got != guard {
			t.Fatal("scheduled check did not receive leader lease guard")
		}
	case <-time.After(time.Second):
		t.Fatal("scheduled check did not expose lease guard")
	}
	time.Sleep(3 * r.clusterReconcileInterval)
	if got := svc.runCount.Load(); got != 1 {
		t.Fatalf("unchanged reconciliation must not reset/fire monitor, got %d runs", got)
	}

	svc.setEnabled([]*ChannelMonitor{{ID: 2, Enabled: true, IntervalSeconds: 60}})
	waitFor(t, time.Second, "leader reconciled monitor replacement", func() bool {
		return runnerTaskCount(r) == 1 && runnerTaskPtr(r, 2) != nil
	})
	select {
	case id := <-svc.runCalled:
		if id != 2 {
			t.Fatalf("expected reconciled monitor 2, got %d", id)
		}
	case <-time.After(time.Second):
		t.Fatal("reconciled monitor did not fire")
	}

	stoppedWithin(t, r, 3*time.Second)
}

func TestClusterScheduler_DrainingRelinquishesLeader(t *testing.T) {
	var draining atomic.Bool
	svc := &stubMonitorSvc{
		enabled:   []*ChannelMonitor{{ID: 1, Enabled: true, IntervalSeconds: 60}},
		runCalled: make(chan int64, 2),
	}
	executor := &stubClusterMonitorTaskExecutor{ownsLease: true, guard: &ClusterLeaseGuard{}}
	r := newChannelMonitorRunnerWithCluster(svc, nil, executor, true, draining.Load)
	r.clusterLeaseRetryInterval = 10 * time.Millisecond
	r.clusterDrainCheckInterval = 10 * time.Millisecond
	r.Start()

	select {
	case <-svc.runCalled:
	case <-time.After(time.Second):
		t.Fatal("leader did not run initial monitor")
	}
	draining.Store(true)
	waitFor(t, time.Second, "draining leader cleared scheduled tasks", func() bool {
		return runnerTaskCount(r) == 0
	})

	stoppedWithin(t, r, 3*time.Second)
}

// stoppedWithin 在 timeout 内并行调用 Stop，超时则 Fatal。验证 Stop 不会阻塞。
func stoppedWithin(t *testing.T, r *ChannelMonitorRunner, timeout time.Duration) {
	t.Helper()
	done := make(chan struct{})
	var once sync.Once
	go func() {
		r.Stop()
		once.Do(func() { close(done) })
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("Stop did not return within %s — leaked goroutine?", timeout)
	}
}
