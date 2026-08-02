package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 审核调用同步挡在网关请求前面，最坏附加延迟必须有界：
// 整轮（含重试、退避、智谱分块）共用一个预算，不被分块或退避二次放大。
func TestContentModerationCallBudgetBoundsTotalLatency(t *testing.T) {
	t.Parallel()

	if got := contentModerationCallBudget(3000, 1); got != 3*time.Second {
		t.Fatalf("单次尝试预算 = %v, want 3s", got)
	}
	// 2 次尝试：2×3s + 一次 100ms 退避。
	if got := contentModerationCallBudget(3000, 2); got != 6*time.Second+100*time.Millisecond {
		t.Fatalf("两次尝试预算 = %v, want 6.1s", got)
	}
	if got := contentModerationCallBudget(0, 0); got != time.Duration(defaultContentModerationTimeoutMS)*time.Millisecond {
		t.Fatalf("非法入参应回落到默认超时，实际 = %v", got)
	}
}

func TestContentModerationCallStopsAtTotalBudget(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int64
	// release 用于在断言结束后放行挂起的 handler：仅靠客户端超时，服务端的
	// 请求 context 未必会及时取消，server.Close() 会一直等待未完成的连接。
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)

	cfg := &ContentModerationConfig{
		Provider:   ContentModerationProviderOpenAI,
		BaseURL:    server.URL,
		Model:      defaultContentModerationModel,
		APIKeys:    []string{"key"},
		TimeoutMS:  150,
		RetryCount: 2,
	}
	cfg.normalize()
	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)

	start := time.Now()
	if _, err := svc.callModeration(context.Background(), cfg, ContentModerationInput{Text: "hello"}); err == nil {
		t.Fatal("上游一直挂起时应当返回错误（由调用方 fail-open 放行）")
	}
	elapsed := time.Since(start)

	budget := contentModerationCallBudget(cfg.TimeoutMS, cfg.RetryCount+1)
	if elapsed > budget+time.Second {
		t.Fatalf("整轮耗时 %v 超出预算 %v 过多", elapsed, budget)
	}
	if got := attempts.Load(); got > int64(cfg.RetryCount+1) {
		t.Fatalf("尝试次数 = %d, 不应超过 %d", got, cfg.RetryCount+1)
	}
}

// 智谱分块并发发起：整批耗时应接近一个超时，而不是块数 × 超时。
func TestContentModerationZhipuChunksRunConcurrently(t *testing.T) {
	t.Parallel()

	const chunkDelay = 200 * time.Millisecond
	var inFlight atomic.Int64
	var maxInFlight atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := inFlight.Add(1)
		for {
			observed := maxInFlight.Load()
			if current <= observed || maxInFlight.CompareAndSwap(observed, current) {
				break
			}
		}
		time.Sleep(chunkDelay)
		inFlight.Add(-1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result_list": []map[string]any{{
				"content_type": "text",
				"risk_level":   "PASS",
				"risk_type":    []string{},
			}},
		})
	}))
	defer server.Close()

	cfg := &ContentModerationConfig{
		Provider:   ContentModerationProviderZhipu,
		BaseURL:    server.URL,
		Model:      defaultZhipuContentModerationModel,
		APIKeys:    []string{"zhipu-key"},
		TimeoutMS:  defaultContentModerationTimeoutMS,
		RetryCount: 0,
	}
	cfg.normalize()
	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)

	// 6 块。
	text := strings.Repeat("测", maxZhipuModerationInputRunes*6)
	start := time.Now()
	if _, err := svc.callModeration(context.Background(), cfg, ContentModerationInput{Text: text}); err != nil {
		t.Fatalf("callModeration 返回错误: %v", err)
	}
	elapsed := time.Since(start)

	if maxInFlight.Load() < 2 {
		t.Fatalf("分块仍是串行发起，最大并发 = %d", maxInFlight.Load())
	}
	// 串行需要 6×200ms=1.2s；并发应远低于此。
	if elapsed >= 6*chunkDelay {
		t.Fatalf("整批分块耗时 %v，与串行无异", elapsed)
	}
}

// 同一用户在冷却窗口内只放行一封违规告知邮件，避免用户自行刷信。
func TestContentModerationViolationEmailThrottle(t *testing.T) {
	t.Parallel()

	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	if !svc.allowViolationEmail(42) {
		t.Fatal("首封违规邮件应放行")
	}
	for i := 0; i < 20; i++ {
		if svc.allowViolationEmail(42) {
			t.Fatal("冷却窗口内不应重复放行")
		}
	}
	if !svc.allowViolationEmail(43) {
		t.Fatal("限频必须按用户隔离，不能影响其他用户")
	}

	// 冷却期满后恢复放行。
	svc.emailThrottleMu.Lock()
	svc.emailThrottle[42] = time.Now().Add(-contentModerationViolationEmailCooldown - time.Second)
	svc.emailThrottleMu.Unlock()
	if !svc.allowViolationEmail(42) {
		t.Fatal("冷却期满后应重新放行")
	}
}

func TestContentModerationViolationEmailThrottleIsConcurrencySafe(t *testing.T) {
	t.Parallel()

	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if svc.allowViolationEmail(7) {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != 1 {
		t.Fatalf("并发下放行了 %d 封，应恰好 1 封", got)
	}
}

// 描述"持续错误状态"的告警（审核服务不可用、未配置 Key、Redis 故障）会随每个请求
// 各打一条，必须按 key 限频，否则一次上游故障就是一场日志风暴。
func TestContentModerationWarnThrottle(t *testing.T) {
	t.Parallel()

	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	allowed := func(key string) bool {
		before := len(svc.warnThrottle)
		svc.warnThrottled(key, "test.message")
		svc.warnThrottleMu.Lock()
		last, ok := svc.warnThrottle[key]
		svc.warnThrottleMu.Unlock()
		return ok && (before == 0 || time.Since(last) < time.Second)
	}

	if !allowed("audit_api_failed") {
		t.Fatal("首条告警应放行")
	}
	svc.warnThrottleMu.Lock()
	first := svc.warnThrottle["audit_api_failed"]
	svc.warnThrottleMu.Unlock()

	for i := 0; i < 100; i++ {
		svc.warnThrottled("audit_api_failed", "test.message")
	}
	svc.warnThrottleMu.Lock()
	afterBurst := svc.warnThrottle["audit_api_failed"]
	svc.warnThrottleMu.Unlock()
	if !afterBurst.Equal(first) {
		t.Fatal("冷却窗口内的重复告警不应刷新时间戳，说明未被抑制")
	}

	// 不同 key 相互独立。
	svc.warnThrottled("dynamic_sampling_failed", "test.message")
	svc.warnThrottleMu.Lock()
	_, otherKey := svc.warnThrottle["dynamic_sampling_failed"]
	svc.warnThrottleMu.Unlock()
	if !otherKey {
		t.Fatal("限频必须按 key 隔离")
	}

	// 冷却期满后恢复。注意不要拿 renewed 和 afterBurst 直接比大小：
	// Windows 上 time.Now 的粒度约 15ms，整个用例可能落在同一个时钟 tick 内。
	// 与注入的陈旧时间戳比较才是稳定的判据。
	stale := time.Now().Add(-contentModerationWarnLogInterval - time.Second)
	svc.warnThrottleMu.Lock()
	svc.warnThrottle["audit_api_failed"] = stale
	svc.warnThrottleMu.Unlock()
	svc.warnThrottled("audit_api_failed", "test.message")
	svc.warnThrottleMu.Lock()
	renewed := svc.warnThrottle["audit_api_failed"]
	svc.warnThrottleMu.Unlock()
	if !renewed.After(stale) {
		t.Fatal("冷却期满后应重新放行并刷新时间戳")
	}
}

func TestContentModerationWarnThrottleIsConcurrencySafe(t *testing.T) {
	t.Parallel()

	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			svc.warnThrottled("shared_key", "test.message", "i", i)
		}(i)
	}
	wg.Wait()
	svc.warnThrottleMu.Lock()
	entries := len(svc.warnThrottle)
	svc.warnThrottleMu.Unlock()
	if entries != 1 {
		t.Fatalf("并发下应只留一个 key，实际 %d", entries)
	}
}

// 模式分组判定每请求都会走到，必须命中缓存而不是每次落库。
func TestContentModerationModeGroupLookupIsCached(t *testing.T) {
	t.Parallel()

	resolver := &countingModeGroupResolver{modeGroupID: 20}
	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	svc.SetAccountShareModeResolver(resolver)

	groupID := int64(20)
	cfg := defaultContentModerationConfig()
	cfg.normalize()
	input := ContentModerationCheckInput{UserID: 1, APIKeyID: 2, GroupID: &groupID}

	for i := 0; i < 10; i++ {
		svc.resolveScope(context.Background(), cfg, input)
	}
	if got := resolver.calls.Load(); got != 1 {
		t.Fatalf("IsModeGroup 被调用 %d 次，缓存未生效", got)
	}

	// TTL 过期后应重新回源。
	svc.modeGroupCacheMu.Lock()
	svc.modeGroupCache[groupID] = contentModerationModeGroupCacheEntry{value: true, expiresAt: time.Now().Add(-time.Second)}
	svc.modeGroupCacheMu.Unlock()
	svc.resolveScope(context.Background(), cfg, input)
	if got := resolver.calls.Load(); got != 2 {
		t.Fatalf("TTL 过期后应回源一次，实际调用 %d 次", got)
	}
}

// 查询失败会被 IsModeGroup 折叠成 false。若把这个 false 缓存下来，一次客户端断连或
// 数据库抖动就能让该分组在整个 TTL 内被判为非模式分组——请求方可以主动中断一次请求
// 来制造这个窗口，期间审计范围与日志归属都是错的。失败必须只影响当前请求。
func TestContentModerationModeGroupLookupFailureIsNotCached(t *testing.T) {
	t.Parallel()

	groupID := int64(20)
	resolver := &countingModeGroupResolver{modeGroupID: groupID, err: context.Canceled}
	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	svc.SetAccountShareModeResolver(resolver)

	cfg := defaultContentModerationConfig()
	cfg.normalize()
	input := ContentModerationCheckInput{UserID: 1, APIKeyID: 2, GroupID: &groupID}

	// 失败的那次查询不得写入缓存。
	svc.resolveScope(context.Background(), cfg, input)
	svc.modeGroupCacheMu.Lock()
	_, cached := svc.modeGroupCache[groupID]
	svc.modeGroupCacheMu.Unlock()
	if cached {
		t.Fatal("查询失败的结果被缓存，故障会在整个 TTL 内持续放大")
	}

	// 恢复后下一个请求必须立刻拿到正确判定，而不是等 TTL 过期。
	resolver.err = nil
	_, scope := svc.resolveScope(context.Background(), cfg, input)
	if scope.ScopeType != contentModerationScopeTypeAccountShareMode {
		t.Fatalf("恢复后应立即判定为账号广场模式分组，实际 scope=%q", scope.ScopeType)
	}
	if got := resolver.calls.Load(); got != 2 {
		t.Fatalf("失败一次 + 恢复一次应各查一次，实际 %d 次", got)
	}
}

type countingModeGroupResolver struct {
	modeGroupID int64
	calls       atomic.Int64
	err         error
}

func (r *countingModeGroupResolver) IsModeGroup(ctx context.Context, groupID int64) bool {
	ok, err := r.IsModeGroupChecked(ctx, groupID)
	return err == nil && ok
}

func (r *countingModeGroupResolver) IsModeGroupChecked(_ context.Context, groupID int64) (bool, error) {
	r.calls.Add(1)
	if r.err != nil {
		return false, r.err
	}
	return r.modeGroupID == groupID, nil
}

func (r *countingModeGroupResolver) ResolveActiveBindingForRequest(context.Context, int64, int64, int64) (*AccountShareMembership, *AccountShareListing, error) {
	return &AccountShareMembership{ID: 1, ConsumerUserID: 1}, &AccountShareListing{ID: 1, AccountID: 1, OwnerUserID: 1}, nil
}
