//go:build unit

package service

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type tokenRefreshAccountRepo struct {
	mockAccountRepoForGemini
	updateCalls            int
	fullUpdateCalls        int
	updateCredentialsCalls int
	setErrorCalls          int
	clearTempCalls         int
	setTempUnschedCalls    int
	lastTempUnschedReason  string
	grokCredentialCASMatch bool
	grokErrorCalls         int
	grokTempCalls          int
	lastGrokSnapshot       GrokCredentialMutationSnapshot
	lastGrokErrorMessage   string
	lastAccount            *Account
	updateErr              error
}

func (r *tokenRefreshAccountRepo) SetGrokCredentialErrorIfMatch(
	_ context.Context,
	_ int64,
	snapshot GrokCredentialMutationSnapshot,
	errorMessage string,
) (bool, error) {
	r.grokErrorCalls++
	r.lastGrokSnapshot = snapshot
	r.lastGrokErrorMessage = errorMessage
	return r.grokCredentialCASMatch, nil
}

func (r *tokenRefreshAccountRepo) SetGrokCredentialTempUnschedulableIfMatch(
	_ context.Context,
	_ int64,
	snapshot GrokCredentialMutationSnapshot,
	_ time.Time,
	_ string,
) (bool, error) {
	r.grokTempCalls++
	r.lastGrokSnapshot = snapshot
	return r.grokCredentialCASMatch, nil
}

func (r *tokenRefreshAccountRepo) Update(ctx context.Context, account *Account) error {
	r.updateCalls++
	r.fullUpdateCalls++
	r.lastAccount = account
	return r.updateErr
}

func (r *tokenRefreshAccountRepo) UpdateCredentials(ctx context.Context, id int64, credentials map[string]any) error {
	r.updateCalls++
	r.updateCredentialsCalls++
	if r.updateErr != nil {
		return r.updateErr
	}
	cloned := cloneCredentials(credentials)
	if r.accountsByID != nil {
		if acc, ok := r.accountsByID[id]; ok && acc != nil {
			acc.Credentials = cloned
			r.lastAccount = acc
			return nil
		}
	}
	r.lastAccount = &Account{ID: id, Credentials: cloned}
	return nil
}

func (r *tokenRefreshAccountRepo) UpdateGrokOAuthCredentialsIfUnchanged(
	ctx context.Context,
	id int64,
	expectedCredentials map[string]any,
	expectedProxyID *int64,
	credentials map[string]any,
) (bool, error) {
	account := r.accountsByID[id]
	if account == nil || account.Platform != PlatformGrok || account.Type != AccountTypeOAuth ||
		!reflect.DeepEqual(account.Credentials, expectedCredentials) ||
		!reflect.DeepEqual(account.ProxyID, expectedProxyID) {
		return false, nil
	}
	if err := r.UpdateCredentials(ctx, id, credentials); err != nil {
		return false, err
	}
	return true, nil
}

func (r *tokenRefreshAccountRepo) SetError(ctx context.Context, id int64, errorMsg string) error {
	r.setErrorCalls++
	return nil
}

func (r *tokenRefreshAccountRepo) ClearTempUnschedulable(ctx context.Context, id int64) error {
	r.clearTempCalls++
	return nil
}

func (r *tokenRefreshAccountRepo) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	r.setTempUnschedCalls++
	r.lastTempUnschedReason = reason
	return nil
}

type tokenCacheInvalidatorStub struct {
	calls int
	err   error
}

func (s *tokenCacheInvalidatorStub) InvalidateToken(ctx context.Context, account *Account) error {
	s.calls++
	return s.err
}

type tempUnschedCacheStub struct {
	deleteCalls int
	setCalls    int
	lastState   *TempUnschedState
}

type tokenRefreshSchedulerCacheStub struct {
	SchedulerCache
	setAccountCalls       int
	needsReauthAtCacheSet bool
}

func (s *tokenRefreshSchedulerCacheStub) SetAccount(_ context.Context, account *Account) error {
	s.setAccountCalls++
	s.needsReauthAtCacheSet = accountGrokNeedsReauth(account)
	return nil
}

func (s *tempUnschedCacheStub) SetTempUnsched(ctx context.Context, accountID int64, state *TempUnschedState) error {
	s.setCalls++
	s.lastState = state
	return nil
}

func (s *tempUnschedCacheStub) GetTempUnsched(ctx context.Context, accountID int64) (*TempUnschedState, error) {
	return nil, nil
}

func (s *tempUnschedCacheStub) DeleteTempUnsched(ctx context.Context, accountID int64) error {
	s.deleteCalls++
	return nil
}

type tokenRefresherStub struct {
	credentials map[string]any
	err         error
}

func (r *tokenRefresherStub) CanRefresh(account *Account) bool {
	return true
}

func (r *tokenRefresherStub) NeedsRefresh(account *Account, refreshWindowDuration time.Duration) bool {
	return true
}

func (r *tokenRefresherStub) Refresh(ctx context.Context, account *Account) (map[string]any, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.credentials, nil
}

func (r *tokenRefresherStub) CacheKey(account *Account) string {
	return "test:stub:" + account.Platform
}

type blockingOAuthRefreshCandidateRepo struct {
	tokenRefreshAccountRepo
	listStarted chan struct{}
	startOnce   sync.Once
}

func (r *blockingOAuthRefreshCandidateRepo) ListOAuthRefreshCandidates(ctx context.Context, _ time.Duration) ([]Account, error) {
	r.startOnce.Do(func() { close(r.listStarted) })
	<-ctx.Done()
	return nil, ctx.Err()
}

type staticOAuthRefreshCandidateRepo struct {
	tokenRefreshAccountRepo
	candidates []Account
}

func (r *staticOAuthRefreshCandidateRepo) ListOAuthRefreshCandidates(context.Context, time.Duration) ([]Account, error) {
	return r.candidates, nil
}

// observedDoneContext 让测试能精确知道刷新协程已进入退避 select，
// 避免依赖 sleep 猜测协程调度时序。
type observedDoneContext struct {
	context.Context
	done         chan struct{}
	doneObserved chan struct{}
	observeOnce  sync.Once
	cancelOnce   sync.Once
}

func newObservedDoneContext() *observedDoneContext {
	return &observedDoneContext{
		Context:      context.Background(),
		done:         make(chan struct{}),
		doneObserved: make(chan struct{}),
	}
}

func (c *observedDoneContext) Done() <-chan struct{} {
	c.observeOnce.Do(func() { close(c.doneObserved) })
	return c.done
}

func (c *observedDoneContext) Err() error {
	select {
	case <-c.done:
		return context.Canceled
	default:
		return nil
	}
}

func (c *observedDoneContext) cancel() {
	c.cancelOnce.Do(func() { close(c.done) })
}

func requireTokenRefreshSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-signal:
	case <-timer.C:
		t.Fatalf("timed out waiting for %s", description)
	}
}

func requireTokenRefreshStop(t *testing.T, service *TokenRefreshService) {
	t.Helper()
	stopped := make(chan struct{})
	go func() {
		service.Stop()
		close(stopped)
	}()
	requireTokenRefreshSignal(t, stopped, "TokenRefreshService.Stop")
}

func TestTokenRefreshService_StopCancelsBlockingCandidateList(t *testing.T) {
	repo := &blockingOAuthRefreshCandidateRepo{listStarted: make(chan struct{})}
	cfg := &config.Config{
		TokenRefresh: config.TokenRefreshConfig{
			Enabled:                  true,
			CheckIntervalMinutes:     5,
			MaxRetries:               3,
			RetryBackoffSeconds:      2,
			RefreshBeforeExpiryHours: 0.5,
		},
	}
	service := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, cfg, nil)
	service.Start()
	requireTokenRefreshSignal(t, repo.listStarted, "OAuth refresh candidate query to start")

	requireTokenRefreshStop(t, service)
	require.Zero(t, repo.setErrorCalls)
	require.Zero(t, repo.setTempUnschedCalls)
}

func TestTokenRefreshService_StopInterruptsRetryBackoffWithoutAccountPenalty(t *testing.T) {
	repo := &staticOAuthRefreshCandidateRepo{
		candidates: []Account{{
			ID:       1001,
			Platform: PlatformGemini,
			Type:     AccountTypeOAuth,
		}},
	}
	cfg := &config.Config{
		TokenRefresh: config.TokenRefreshConfig{
			Enabled:                  true,
			CheckIntervalMinutes:     5,
			MaxRetries:               3,
			RetryBackoffSeconds:      60,
			RefreshBeforeExpiryHours: 0.5,
		},
	}
	service := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, cfg, nil)
	refresher := &tokenRefresherStub{err: errors.New("transient token endpoint failure")}
	service.refreshers = []TokenRefresher{refresher}
	service.executors = []OAuthRefreshExecutor{refresher}

	// 用可观测 context 替换构造器的 context，只用于确认已真正进入 60s 退避等待。
	service.cancelRun()
	runCtx := newObservedDoneContext()
	service.runCtx = runCtx
	service.cancelRun = runCtx.cancel

	service.Start()
	requireTokenRefreshSignal(t, runCtx.doneObserved, "retry backoff wait to start")
	requireTokenRefreshStop(t, service)

	require.Zero(t, repo.setErrorCalls, "shutdown cancellation must not mark the account as error")
	require.Zero(t, repo.setTempUnschedCalls, "shutdown cancellation must not temporarily unschedule the account")
}

func TestTokenRefreshService_RefreshWithRetry_InvalidatesCache(t *testing.T) {
	repo := &tokenRefreshAccountRepo{}
	invalidator := &tokenCacheInvalidatorStub{}
	cfg := &config.Config{
		TokenRefresh: config.TokenRefreshConfig{
			MaxRetries:          1,
			RetryBackoffSeconds: 0,
		},
	}
	service := NewTokenRefreshService(repo, nil, nil, nil, nil, invalidator, nil, cfg, nil)
	account := &Account{
		ID:       5,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}
	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"access_token": "new-token",
		},
	}

	err := service.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCalls)
	require.Equal(t, 1, repo.updateCredentialsCalls)
	require.Equal(t, 0, repo.fullUpdateCalls)
	require.Equal(t, 1, invalidator.calls)
	require.Equal(t, "new-token", account.GetCredential("access_token"))
}

func TestTokenRefreshService_RefreshWithRetry_InvalidatorErrorIgnored(t *testing.T) {
	repo := &tokenRefreshAccountRepo{}
	invalidator := &tokenCacheInvalidatorStub{err: errors.New("invalidate failed")}
	cfg := &config.Config{
		TokenRefresh: config.TokenRefreshConfig{
			MaxRetries:          1,
			RetryBackoffSeconds: 0,
		},
	}
	service := NewTokenRefreshService(repo, nil, nil, nil, nil, invalidator, nil, cfg, nil)
	account := &Account{
		ID:       6,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}
	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"access_token": "token",
		},
	}

	err := service.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCalls)
	require.Equal(t, 1, invalidator.calls)
}

func TestTokenRefreshService_RefreshWithRetry_NilInvalidator(t *testing.T) {
	repo := &tokenRefreshAccountRepo{}
	cfg := &config.Config{
		TokenRefresh: config.TokenRefreshConfig{
			MaxRetries:          1,
			RetryBackoffSeconds: 0,
		},
	}
	service := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, cfg, nil)
	account := &Account{
		ID:       7,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}
	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"access_token": "token",
		},
	}

	err := service.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCalls)
}

func TestTokenRefreshService_RefreshWithRetry_ClearsGrokReauthBeforeSchedulerCache(t *testing.T) {
	repo := &tokenRefreshAccountRepo{}
	schedulerCache := &tokenRefreshSchedulerCacheStub{}
	cfg := &config.Config{
		TokenRefresh: config.TokenRefreshConfig{
			MaxRetries:          1,
			RetryBackoffSeconds: 0,
		},
	}
	service := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, schedulerCache, cfg, nil)
	account := &Account{
		ID:       71,
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"grok_needs_reauth":        true,
			"grok_needs_reauth_reason": "spending_limit",
		},
	}
	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"access_token": "refreshed-grok-token",
		},
	}

	err := service.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, schedulerCache.setAccountCalls)
	require.False(t, schedulerCache.needsReauthAtCacheSet, "scheduler cache must not serialize stale reauth state")
	require.False(t, accountGrokNeedsReauth(account))
}

// TestTokenRefreshService_RefreshWithRetry_Antigravity 测试 Antigravity 平台的缓存失效
func TestTokenRefreshService_RefreshWithRetry_Antigravity(t *testing.T) {
	repo := &tokenRefreshAccountRepo{}
	invalidator := &tokenCacheInvalidatorStub{}
	cfg := &config.Config{
		TokenRefresh: config.TokenRefreshConfig{
			MaxRetries:          1,
			RetryBackoffSeconds: 0,
		},
	}
	service := NewTokenRefreshService(repo, nil, nil, nil, nil, invalidator, nil, cfg, nil)
	account := &Account{
		ID:       8,
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
	}
	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"access_token": "ag-token",
		},
	}

	err := service.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCalls)
	require.Equal(t, 1, invalidator.calls) // Antigravity 也应触发缓存失效
}

// TestTokenRefreshService_RefreshWithRetry_NonOAuthAccount 测试非 OAuth 账号不触发缓存失效
func TestTokenRefreshService_RefreshWithRetry_NonOAuthAccount(t *testing.T) {
	repo := &tokenRefreshAccountRepo{}
	invalidator := &tokenCacheInvalidatorStub{}
	cfg := &config.Config{
		TokenRefresh: config.TokenRefreshConfig{
			MaxRetries:          1,
			RetryBackoffSeconds: 0,
		},
	}
	service := NewTokenRefreshService(repo, nil, nil, nil, nil, invalidator, nil, cfg, nil)
	account := &Account{
		ID:       9,
		Platform: PlatformGemini,
		Type:     AccountTypeAPIKey, // 非 OAuth
	}
	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"access_token": "token",
		},
	}

	err := service.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCalls)
	require.Equal(t, 0, invalidator.calls) // 非 OAuth 不触发缓存失效
}

// TestTokenRefreshService_RefreshWithRetry_OtherPlatformOAuth 测试所有 OAuth 平台都触发缓存失效
func TestTokenRefreshService_RefreshWithRetry_OtherPlatformOAuth(t *testing.T) {
	repo := &tokenRefreshAccountRepo{}
	invalidator := &tokenCacheInvalidatorStub{}
	cfg := &config.Config{
		TokenRefresh: config.TokenRefreshConfig{
			MaxRetries:          1,
			RetryBackoffSeconds: 0,
		},
	}
	service := NewTokenRefreshService(repo, nil, nil, nil, nil, invalidator, nil, cfg, nil)
	account := &Account{
		ID:       10,
		Platform: PlatformOpenAI, // OpenAI OAuth 账户
		Type:     AccountTypeOAuth,
	}
	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"access_token": "token",
		},
	}

	err := service.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCalls)
	require.Equal(t, 1, repo.updateCredentialsCalls)
	require.Equal(t, 1, invalidator.calls) // 所有 OAuth 账户刷新后触发缓存失效
}

func TestTokenRefreshService_RefreshWithRetry_UsesCredentialsUpdater(t *testing.T) {
	repo := &tokenRefreshAccountRepo{}
	cfg := &config.Config{
		TokenRefresh: config.TokenRefreshConfig{
			MaxRetries:          1,
			RetryBackoffSeconds: 0,
		},
	}
	service := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, cfg, nil)
	resetAt := time.Now().Add(30 * time.Minute)
	account := &Account{
		ID:               17,
		Platform:         PlatformOpenAI,
		Type:             AccountTypeOAuth,
		RateLimitResetAt: &resetAt,
		Credentials: map[string]any{
			"access_token": "old-token",
		},
	}
	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"access_token": "new-token",
		},
	}

	err := service.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCredentialsCalls)
	require.Equal(t, 0, repo.fullUpdateCalls)
	require.NotNil(t, account.RateLimitResetAt)
	require.WithinDuration(t, resetAt, *account.RateLimitResetAt, time.Second)
}

// TestTokenRefreshService_RefreshWithRetry_UpdateFailed 测试更新失败的情况
func TestTokenRefreshService_RefreshWithRetry_UpdateFailed(t *testing.T) {
	repo := &tokenRefreshAccountRepo{updateErr: errors.New("update failed")}
	invalidator := &tokenCacheInvalidatorStub{}
	cfg := &config.Config{
		TokenRefresh: config.TokenRefreshConfig{
			MaxRetries:          1,
			RetryBackoffSeconds: 0,
		},
	}
	service := NewTokenRefreshService(repo, nil, nil, nil, nil, invalidator, nil, cfg, nil)
	account := &Account{
		ID:       11,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}
	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"access_token": "token",
		},
	}

	err := service.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to save credentials")
	require.Equal(t, 1, repo.updateCalls)
	require.Equal(t, 0, invalidator.calls) // 更新失败时不应触发缓存失效
}

// TestTokenRefreshService_RefreshWithRetry_RefreshFailed 测试可重试错误耗尽不标记 error
func TestTokenRefreshService_RefreshWithRetry_RefreshFailed(t *testing.T) {
	repo := &tokenRefreshAccountRepo{}
	invalidator := &tokenCacheInvalidatorStub{}
	cfg := &config.Config{
		TokenRefresh: config.TokenRefreshConfig{
			MaxRetries:          2,
			RetryBackoffSeconds: 0,
		},
	}
	service := NewTokenRefreshService(repo, nil, nil, nil, nil, invalidator, nil, cfg, nil)
	account := &Account{
		ID:       12,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}
	refresher := &tokenRefresherStub{
		err: errors.New("refresh failed"),
	}

	err := service.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)
	require.Error(t, err)
	require.Equal(t, 0, repo.updateCalls)   // 刷新失败不应更新
	require.Equal(t, 0, invalidator.calls)  // 刷新失败不应触发缓存失效
	require.Equal(t, 0, repo.setErrorCalls) // 可重试错误耗尽不标记 error，下个周期继续重试
}

// TestTokenRefreshService_RefreshWithRetry_AntigravityRefreshFailed 测试 Antigravity 刷新失败不设置错误状态
func TestTokenRefreshService_RefreshWithRetry_AntigravityRefreshFailed(t *testing.T) {
	repo := &tokenRefreshAccountRepo{}
	invalidator := &tokenCacheInvalidatorStub{}
	cfg := &config.Config{
		TokenRefresh: config.TokenRefreshConfig{
			MaxRetries:          1,
			RetryBackoffSeconds: 0,
		},
	}
	service := NewTokenRefreshService(repo, nil, nil, nil, nil, invalidator, nil, cfg, nil)
	account := &Account{
		ID:       13,
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
	}
	refresher := &tokenRefresherStub{
		err: errors.New("network error"), // 可重试错误
	}

	err := service.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)
	require.Error(t, err)
	require.Equal(t, 0, repo.updateCalls)
	require.Equal(t, 0, invalidator.calls)
	require.Equal(t, 0, repo.setErrorCalls) // Antigravity 可重试错误不设置错误状态
}

// TestTokenRefreshService_RefreshWithRetry_AntigravityNonRetryableError 测试 Antigravity 不可重试错误
func TestTokenRefreshService_RefreshWithRetry_AntigravityNonRetryableError(t *testing.T) {
	repo := &tokenRefreshAccountRepo{}
	invalidator := &tokenCacheInvalidatorStub{}
	cfg := &config.Config{
		TokenRefresh: config.TokenRefreshConfig{
			MaxRetries:          3,
			RetryBackoffSeconds: 0,
		},
	}
	service := NewTokenRefreshService(repo, nil, nil, nil, nil, invalidator, nil, cfg, nil)
	account := &Account{
		ID:       14,
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
	}
	refresher := &tokenRefresherStub{
		err: errors.New("invalid_grant: token revoked"), // 不可重试错误
	}

	err := service.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)
	require.Error(t, err)
	require.Equal(t, 0, repo.updateCalls)
	require.Equal(t, 0, invalidator.calls)
	require.Equal(t, 1, repo.setErrorCalls) // 不可重试错误应设置错误状态
}

// TestTokenRefreshService_RefreshWithRetry_ClearsTempUnschedulable 测试刷新成功后清除临时不可调度（DB + Redis）
func TestTokenRefreshService_RefreshWithRetry_ClearsTempUnschedulable(t *testing.T) {
	repo := &tokenRefreshAccountRepo{}
	invalidator := &tokenCacheInvalidatorStub{}
	tempCache := &tempUnschedCacheStub{}
	cfg := &config.Config{
		TokenRefresh: config.TokenRefreshConfig{
			MaxRetries:          1,
			RetryBackoffSeconds: 0,
		},
	}
	service := NewTokenRefreshService(repo, nil, nil, nil, nil, invalidator, nil, cfg, tempCache)
	until := time.Now().Add(10 * time.Minute)
	account := &Account{
		ID:                     15,
		Platform:               PlatformGemini,
		Type:                   AccountTypeOAuth,
		TempUnschedulableUntil: &until,
	}
	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"access_token": "new-token",
		},
	}

	err := service.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCalls)
	require.Equal(t, 1, repo.clearTempCalls)   // DB 清除
	require.Equal(t, 1, tempCache.deleteCalls) // Redis 缓存也应清除
}

// TestTokenRefreshService_RefreshWithRetry_NonRetryableErrorAllPlatforms 测试所有平台不可重试错误都 SetError
func TestTokenRefreshService_RefreshWithRetry_NonRetryableErrorAllPlatforms(t *testing.T) {
	tests := []struct {
		name     string
		platform string
	}{
		{name: "gemini", platform: PlatformGemini},
		{name: "anthropic", platform: PlatformAnthropic},
		{name: "openai", platform: PlatformOpenAI},
		{name: "antigravity", platform: PlatformAntigravity},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &tokenRefreshAccountRepo{}
			invalidator := &tokenCacheInvalidatorStub{}
			cfg := &config.Config{
				TokenRefresh: config.TokenRefreshConfig{
					MaxRetries:          3,
					RetryBackoffSeconds: 0,
				},
			}
			service := NewTokenRefreshService(repo, nil, nil, nil, nil, invalidator, nil, cfg, nil)
			account := &Account{
				ID:       16,
				Platform: tt.platform,
				Type:     AccountTypeOAuth,
			}
			refresher := &tokenRefresherStub{
				err: errors.New("invalid_grant: token revoked"),
			}

			err := service.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)
			require.Error(t, err)
			require.Equal(t, 1, repo.setErrorCalls) // 所有平台不可重试错误都应 SetError
		})
	}
}

func TestTokenRefreshService_RefreshWithRetry_NoRefreshTokenDoesNotTempUnschedule(t *testing.T) {
	repo := &tokenRefreshAccountRepo{}
	cfg := &config.Config{
		TokenRefresh: config.TokenRefreshConfig{
			MaxRetries:          2,
			RetryBackoffSeconds: 0,
		},
	}
	service := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, cfg, nil)
	account := &Account{
		ID:       18,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	}
	refresher := &tokenRefresherStub{
		err: errors.New("no refresh token available"),
	}

	err := service.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)
	require.Error(t, err)
	require.Equal(t, 0, repo.updateCalls)
	require.Equal(t, 0, repo.setTempUnschedCalls, "missing refresh token should not mark the account temp unschedulable")
	require.Equal(t, 1, repo.setErrorCalls, "missing refresh token should be treated as a non-retryable credential state")
}

// TestIsNonRetryableRefreshError 测试不可重试错误判断
func TestIsNonRetryableRefreshError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{name: "nil_error", err: nil, expected: false},
		{name: "network_error", err: errors.New("network timeout"), expected: false},
		{name: "invalid_grant", err: errors.New("invalid_grant"), expected: true},
		{name: "invalid_client", err: errors.New("invalid_client"), expected: true},
		{name: "unauthorized_client", err: errors.New("unauthorized_client"), expected: true},
		{name: "access_denied", err: errors.New("access_denied"), expected: true},
		{name: "no_refresh_token", err: errors.New("no refresh token available"), expected: true},
		{name: "try_signing_in_again", err: errors.New("Please try signing in again"), expected: true},
		{name: "try_signing_in_again_with_context", err: errors.New("token endpoint returned 401: Please try signing in again"), expected: true},
		{name: "invalid_grant_with_desc", err: errors.New("Error: invalid_grant - token revoked"), expected: true},
		{name: "case_insensitive", err: errors.New("INVALID_GRANT"), expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsNonRetryableRefreshError(tt.err)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestTokenRefreshServiceGrokProxyAuthenticationFailureStopsRetryAndUsesCAS(t *testing.T) {
	proxyID := int64(91)
	account := &Account{
		ID:       1901,
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		ProxyID:  &proxyID,
		Credentials: map[string]any{
			"access_token":  "old-access",
			"refresh_token": "old-refresh",
		},
	}
	repo := &tokenRefreshAccountRepo{grokCredentialCASMatch: true}
	cfg := &config.Config{TokenRefresh: config.TokenRefreshConfig{
		MaxRetries:          3,
		RetryBackoffSeconds: 0,
	}}
	service := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, cfg, nil)
	refresher := &countingTokenRefresherStub{err: errors.New(
		`Post "https://auth.x.ai/oauth2/token": Proxy Authentication Required`,
	)}

	err := service.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)

	require.Error(t, err)
	require.Equal(t, 1, refresher.calls, "deterministic proxy authentication failure must not be retried")
	require.Equal(t, 1, repo.grokErrorCalls)
	require.Zero(t, repo.grokTempCalls)
	require.Zero(t, repo.setErrorCalls, "Grok failures must not bypass credential+proxy CAS")
	require.Equal(t, grokCredentialMutationSnapshot(account), repo.lastGrokSnapshot)
	class := classifyGrokCredentialFailure(account, err)
	require.Equal(t, GrokCredentialReasonProxyInvalid, class.reason)
}

func TestTokenRefreshServiceGrokRetryExhaustionUsesObservedProxySnapshotFromError(t *testing.T) {
	proxyID := int64(92)
	staleUpdatedAt := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	actualUpdatedAt := staleUpdatedAt.Add(time.Minute)
	account := &Account{
		ID:          1902,
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		ProxyID:     &proxyID,
		Proxy:       &Proxy{ID: proxyID, UpdatedAt: staleUpdatedAt},
		Credentials: map[string]any{
			"access_token":  "old-access",
			"refresh_token": "old-refresh",
		},
	}
	observedSnapshot := grokCredentialMutationSnapshot(account)
	observedSnapshot.ProxyUpdatedAt = &actualUpdatedAt
	retryableErr := withGrokCredentialFailureMutationSnapshot(errors.New("temporary upstream timeout"), observedSnapshot)
	repo := &tokenRefreshAccountRepo{grokCredentialCASMatch: true}
	cfg := &config.Config{TokenRefresh: config.TokenRefreshConfig{
		MaxRetries:          1,
		RetryBackoffSeconds: 0,
	}}
	service := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, cfg, nil)
	refresher := &countingTokenRefresherStub{err: retryableErr}

	err := service.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)

	require.Error(t, err)
	require.Equal(t, 1, repo.grokTempCalls)
	require.Zero(t, repo.grokErrorCalls)
	require.NotNil(t, repo.lastGrokSnapshot.ProxyUpdatedAt)
	require.Equal(t, actualUpdatedAt, *repo.lastGrokSnapshot.ProxyUpdatedAt)
	require.NotEqual(t, staleUpdatedAt, *repo.lastGrokSnapshot.ProxyUpdatedAt)
}

func TestGrokOAuthReconcileStructuralBlockPersistsStableCredentialReason(t *testing.T) {
	account := &Account{
		ID:          1903,
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"access_token": "access-without-refresh"},
	}
	repo := &tokenRefreshAccountRepo{
		mockAccountRepoForGemini: mockAccountRepoForGemini{
			accountsByID: map[int64]*Account{account.ID: account},
		},
		grokCredentialCASMatch: true,
	}
	service := &TokenRefreshService{accountRepo: repo}
	item := &GrokOAuthReconcileItem{}

	outcome := service.applyGrokOAuthReconcileStructuralBlock(
		context.Background(),
		repo,
		account,
		time.Hour,
		item,
	)

	require.Equal(t, GrokOAuthReconcileOutcomePartial, outcome)
	require.Equal(t, GrokOAuthReconcileReasonMissingRefreshToken, item.Reason)
	require.Equal(t, string(GrokCredentialReasonMissing), repo.lastGrokErrorMessage)
	require.NotContains(t, repo.lastGrokErrorMessage, "reconciliation")
}

type countingTokenRefresherStub struct {
	calls int
	err   error
}

func (r *countingTokenRefresherStub) CanRefresh(*Account) bool { return true }
func (r *countingTokenRefresherStub) NeedsRefresh(*Account, time.Duration) bool {
	return true
}
func (r *countingTokenRefresherStub) Refresh(context.Context, *Account) (map[string]any, error) {
	r.calls++
	return nil, r.err
}
func (r *countingTokenRefresherStub) CacheKey(account *Account) string {
	return "test:counting:" + account.Platform
}

// ========== Path A (refreshAPI) 测试用例 ==========

// mockTokenCacheForRefreshAPI 用于 Path A 测试的 GeminiTokenCache mock
type mockTokenCacheForRefreshAPI struct {
	lockResult   bool
	lockErr      error
	releaseCalls int
}

func (m *mockTokenCacheForRefreshAPI) GetAccessToken(_ context.Context, _ string) (string, error) {
	return "", errors.New("not cached")
}

func (m *mockTokenCacheForRefreshAPI) SetAccessToken(_ context.Context, _ string, _ string, _ time.Duration) error {
	return nil
}

func (m *mockTokenCacheForRefreshAPI) DeleteAccessToken(_ context.Context, _ string) error {
	return nil
}

func (m *mockTokenCacheForRefreshAPI) AcquireRefreshLock(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return m.lockResult, m.lockErr
}

func (m *mockTokenCacheForRefreshAPI) ReleaseRefreshLock(_ context.Context, _ string) error {
	m.releaseCalls++
	return nil
}

// buildPathAService 构建注入了 refreshAPI 的 service（Path A 测试辅助）
func buildPathAService(repo *tokenRefreshAccountRepo, cache GeminiTokenCache, invalidator TokenCacheInvalidator) (*TokenRefreshService, *tokenRefresherStub) {
	cfg := &config.Config{
		TokenRefresh: config.TokenRefreshConfig{
			MaxRetries:          1,
			RetryBackoffSeconds: 0,
		},
	}
	service := NewTokenRefreshService(repo, nil, nil, nil, nil, invalidator, nil, cfg, nil)
	refreshAPI := NewOAuthRefreshAPI(repo, cache)
	service.SetRefreshAPI(refreshAPI)

	refresher := &tokenRefresherStub{
		credentials: map[string]any{
			"access_token": "refreshed-token",
		},
	}
	return service, refresher
}

// TestPathA_Success 统一 API 路径正常成功：刷新 + DB 更新 + postRefreshActions
func TestPathA_Success(t *testing.T) {
	account := &Account{
		ID:       100,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}
	repo := &tokenRefreshAccountRepo{}
	repo.accountsByID = map[int64]*Account{account.ID: account}
	invalidator := &tokenCacheInvalidatorStub{}
	cache := &mockTokenCacheForRefreshAPI{lockResult: true}

	service, refresher := buildPathAService(repo, cache, invalidator)

	err := service.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCalls)   // DB 更新被调用
	require.Equal(t, 1, invalidator.calls)  // 缓存失效被调用
	require.Equal(t, 1, cache.releaseCalls) // 锁被释放
}

// TestPathA_LockHeld 锁被其他 worker 持有 → 返回 errRefreshSkipped
func TestPathA_LockHeld(t *testing.T) {
	account := &Account{
		ID:       101,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}
	repo := &tokenRefreshAccountRepo{}
	invalidator := &tokenCacheInvalidatorStub{}
	cache := &mockTokenCacheForRefreshAPI{lockResult: false} // 锁获取失败（被占）

	service, refresher := buildPathAService(repo, cache, invalidator)

	err := service.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)
	require.ErrorIs(t, err, errRefreshSkipped)
	require.Equal(t, 0, repo.updateCalls)  // 不应更新 DB
	require.Equal(t, 0, invalidator.calls) // 不应触发缓存失效
}

// TestPathA_AlreadyRefreshed 二次检查发现已被其他路径刷新 → 返回 errRefreshSkipped
func TestPathA_AlreadyRefreshed(t *testing.T) {
	// NeedsRefresh 返回 false → RefreshIfNeeded 返回 {Refreshed: false}
	account := &Account{
		ID:       102,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}
	repo := &tokenRefreshAccountRepo{}
	repo.accountsByID = map[int64]*Account{account.ID: account}
	invalidator := &tokenCacheInvalidatorStub{}
	cache := &mockTokenCacheForRefreshAPI{lockResult: true}

	service, _ := buildPathAService(repo, cache, invalidator)

	// 使用一个 NeedsRefresh 返回 false 的 stub
	noRefreshNeeded := &tokenRefresherStub{
		credentials: map[string]any{"access_token": "token"},
	}
	// 覆盖 NeedsRefresh 行为 — 我们需要一个新的 stub 类型
	alwaysFreshStub := &alwaysFreshRefresherStub{}

	err := service.refreshWithRetry(context.Background(), account, noRefreshNeeded, alwaysFreshStub, time.Hour)
	require.ErrorIs(t, err, errRefreshSkipped)
	require.Equal(t, 0, repo.updateCalls)
	require.Equal(t, 0, invalidator.calls)
}

// alwaysFreshRefresherStub 二次检查时认为不需要刷新（模拟已被其他路径刷新）
type alwaysFreshRefresherStub struct{}

func (r *alwaysFreshRefresherStub) CanRefresh(_ *Account) bool                    { return true }
func (r *alwaysFreshRefresherStub) NeedsRefresh(_ *Account, _ time.Duration) bool { return false }
func (r *alwaysFreshRefresherStub) Refresh(_ context.Context, _ *Account) (map[string]any, error) {
	return nil, errors.New("should not be called")
}
func (r *alwaysFreshRefresherStub) CacheKey(account *Account) string {
	return "test:fresh:" + account.Platform
}

// TestPathA_NonRetryableError 统一 API 路径返回不可重试错误 → SetError
func TestPathA_NonRetryableError(t *testing.T) {
	account := &Account{
		ID:       103,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}
	repo := &tokenRefreshAccountRepo{}
	repo.accountsByID = map[int64]*Account{account.ID: account}
	invalidator := &tokenCacheInvalidatorStub{}
	cache := &mockTokenCacheForRefreshAPI{lockResult: true}

	service, _ := buildPathAService(repo, cache, invalidator)

	refresher := &tokenRefresherStub{
		err: errors.New("invalid_grant: token revoked"),
	}

	err := service.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)
	require.Error(t, err)
	require.Equal(t, 1, repo.setErrorCalls) // 应标记 error 状态
	require.Equal(t, 0, repo.updateCalls)   // 不应更新 credentials
	require.Equal(t, 0, invalidator.calls)  // 不应触发缓存失效
}

// TestPathA_RetryableErrorExhausted 统一 API 路径可重试错误耗尽 → 不标记 error
func TestPathA_RetryableErrorExhausted(t *testing.T) {
	account := &Account{
		ID:       104,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}
	repo := &tokenRefreshAccountRepo{}
	repo.accountsByID = map[int64]*Account{account.ID: account}
	invalidator := &tokenCacheInvalidatorStub{}
	cache := &mockTokenCacheForRefreshAPI{lockResult: true}

	cfg := &config.Config{
		TokenRefresh: config.TokenRefreshConfig{
			MaxRetries:          2,
			RetryBackoffSeconds: 0,
		},
	}
	service := NewTokenRefreshService(repo, nil, nil, nil, nil, invalidator, nil, cfg, nil)
	refreshAPI := NewOAuthRefreshAPI(repo, cache)
	service.SetRefreshAPI(refreshAPI)

	refresher := &tokenRefresherStub{
		err: errors.New("network timeout"),
	}

	err := service.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)
	require.Error(t, err)
	require.Equal(t, 0, repo.setErrorCalls) // 可重试错误不标记 error
	require.Equal(t, 0, repo.updateCalls)   // 刷新失败不应更新
	require.Equal(t, 0, invalidator.calls)  // 不应触发缓存失效
}

// TestPathA_DBUpdateFailed 统一 API 路径 DB 更新失败 → 返回 error，不执行 postRefreshActions
func TestPathA_DBUpdateFailed(t *testing.T) {
	account := &Account{
		ID:       105,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}
	repo := &tokenRefreshAccountRepo{updateErr: errors.New("db connection lost")}
	repo.accountsByID = map[int64]*Account{account.ID: account}
	invalidator := &tokenCacheInvalidatorStub{}
	cache := &mockTokenCacheForRefreshAPI{lockResult: true}

	service, refresher := buildPathAService(repo, cache, invalidator)

	err := service.refreshWithRetry(context.Background(), account, refresher, refresher, time.Hour)
	require.Error(t, err)
	require.Contains(t, err.Error(), "DB update failed")
	require.Equal(t, 1, repo.updateCalls)  // DB 更新被尝试
	require.Equal(t, 0, invalidator.calls) // DB 失败时不应触发缓存失效
}
