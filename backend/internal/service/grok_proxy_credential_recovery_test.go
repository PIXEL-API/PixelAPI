//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type grokProxyRecoveryRepoStub struct {
	AccountRepository
	mu                    sync.Mutex
	accounts              map[int64]*Account
	candidates            []Account
	currentProxyUpdatedAt time.Time
	recoverCalls          []int64
	listCalls             int
	listActive            int
	listMaxActive         int
	listStarted           chan struct{}
	listRelease           chan struct{}
}

func (r *grokProxyRecoveryRepoStub) GetByID(_ context.Context, id int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account := r.accounts[id]
	if account == nil {
		return nil, ErrAccountNotFound
	}
	copy := *account
	copy.Credentials = MergeCredentials(nil, account.Credentials)
	return &copy, nil
}

func (r *grokProxyRecoveryRepoStub) ListGrokProxyCredentialRecoveryCandidates(context.Context, int64) ([]Account, error) {
	r.mu.Lock()
	r.listCalls++
	r.listActive++
	if r.listActive > r.listMaxActive {
		r.listMaxActive = r.listActive
	}
	candidates := append([]Account(nil), r.candidates...)
	started := r.listStarted
	release := r.listRelease
	r.mu.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if release != nil {
		<-release
	}
	r.mu.Lock()
	r.listActive--
	r.mu.Unlock()
	return candidates, nil
}

func (r *grokProxyRecoveryRepoStub) RecoverGrokProxyCredentialFailureIfMatch(
	_ context.Context,
	accountID int64,
	snapshot GrokCredentialMutationSnapshot,
) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recoverCalls = append(r.recoverCalls, accountID)
	account := r.accounts[accountID]
	if account == nil || !isGrokProxyCredentialFailureAccount(account) || snapshot.ProxyUpdatedAt == nil ||
		!snapshot.ProxyUpdatedAt.Equal(r.currentProxyUpdatedAt) || snapshot.CredentialsJSON != grokCredentialMutationSnapshot(account).CredentialsJSON {
		return false, nil
	}
	account.Status = StatusActive
	account.Schedulable = true
	account.ErrorMessage = ""
	account.TempUnschedulableUntil = nil
	account.TempUnschedulableReason = ""
	return true, nil
}

type grokProxyRecoveryVerifierStub struct {
	mu       sync.Mutex
	version  time.Time
	failures map[int64]error
	callIDs  []int64
}

func (v *grokProxyRecoveryVerifierStub) VerifyGrokProxyCredentialRecovery(
	_ context.Context,
	account *Account,
) (*Account, GrokCredentialMutationSnapshot, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.callIDs = append(v.callIDs, account.ID)
	if err := v.failures[account.ID]; err != nil {
		return nil, GrokCredentialMutationSnapshot{}, err
	}
	copy := *account
	snapshot := grokCredentialMutationSnapshot(&copy)
	updatedAt := v.version
	snapshot.ProxyUpdatedAt = &updatedAt
	return &copy, snapshot, nil
}

type grokSchedulingBlockCleanerStub struct {
	mu      sync.Mutex
	cleared []int64
}

func (c *grokSchedulingBlockCleanerStub) ClearAccountSchedulingBlock(accountID int64) {
	c.mu.Lock()
	c.cleared = append(c.cleared, accountID)
	c.mu.Unlock()
}

func newGrokProxyFailureAccount(id, proxyID int64, proxyUpdatedAt time.Time) *Account {
	return &Account{
		ID:           id,
		Platform:     PlatformGrok,
		Type:         AccountTypeOAuth,
		Status:       StatusError,
		Schedulable:  false,
		ErrorMessage: string(GrokCredentialReasonProxyInvalid),
		ProxyID:      &proxyID,
		Proxy: &Proxy{
			ID:        proxyID,
			Status:    StatusActive,
			UpdatedAt: proxyUpdatedAt,
		},
		Credentials: map[string]any{
			"access_token":  "access",
			"refresh_token": "refresh",
		},
	}
}

func TestGrokProxyCredentialRecoveryRejectsStaleProxyVersion(t *testing.T) {
	oldVersion := time.Date(2026, 8, 14, 2, 0, 0, 0, time.UTC)
	currentVersion := oldVersion.Add(time.Minute)
	account := newGrokProxyFailureAccount(1, 91, currentVersion)
	repo := &grokProxyRecoveryRepoStub{
		accounts:              map[int64]*Account{account.ID: account},
		currentProxyUpdatedAt: currentVersion,
	}
	verifier := &grokProxyRecoveryVerifierStub{version: oldVersion, failures: map[int64]error{}}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	svc.SetGrokProxyCredentialRecoveryVerifier(verifier)

	result, err := svc.RecoverGrokProxyCredentialFailure(context.Background(), account.ID)

	require.Nil(t, result)
	require.ErrorIs(t, err, ErrGrokProxyCredentialRecoveryConflict)
	require.Equal(t, StatusError, account.Status)
	require.False(t, account.Schedulable)
	require.Equal(t, []int64{account.ID}, repo.recoverCalls)
}

func TestGrokProxyCredentialRecoveryVerificationFailureDoesNotRecover(t *testing.T) {
	version := time.Date(2026, 8, 14, 2, 0, 0, 0, time.UTC)
	account := newGrokProxyFailureAccount(2, 92, version)
	repo := &grokProxyRecoveryRepoStub{
		accounts:              map[int64]*Account{account.ID: account},
		currentProxyUpdatedAt: version,
	}
	verifier := &grokProxyRecoveryVerifierStub{
		version:  version,
		failures: map[int64]error{account.ID: errors.New("invalid_grant")},
	}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	svc.SetGrokProxyCredentialRecoveryVerifier(verifier)

	result, err := svc.RecoverGrokProxyCredentialFailure(context.Background(), account.ID)

	require.Nil(t, result)
	require.ErrorContains(t, err, "invalid_grant")
	require.Empty(t, repo.recoverCalls)
	require.Equal(t, StatusError, account.Status)
	require.False(t, account.Schedulable)
}

func TestGrokProxyCredentialRecoverySuccessRestoresSchedulingAndClearsRuntimeBlock(t *testing.T) {
	version := time.Date(2026, 8, 14, 2, 0, 0, 0, time.UTC)
	account := newGrokProxyFailureAccount(3, 93, version)
	repo := &grokProxyRecoveryRepoStub{
		accounts:              map[int64]*Account{account.ID: account},
		currentProxyUpdatedAt: version,
	}
	verifier := &grokProxyRecoveryVerifierStub{version: version, failures: map[int64]error{}}
	cleaner := &grokSchedulingBlockCleanerStub{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	svc.SetGrokProxyCredentialRecoveryVerifier(verifier)
	svc.SetGrokSchedulingBlockCleaner(cleaner)

	result, err := svc.RecoverAccountAfterSuccessfulTest(context.Background(), account.ID)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.ClearedError)
	require.True(t, result.RestoredSchedulable)
	require.Equal(t, StatusActive, account.Status)
	require.True(t, account.Schedulable)
	require.Empty(t, account.ErrorMessage)
	require.Equal(t, []int64{account.ID}, cleaner.cleared)
}

func TestGrokProxyCredentialRecoveryReportsTempCacheCleanupFailure(t *testing.T) {
	version := time.Date(2026, 8, 14, 2, 0, 0, 0, time.UTC)
	account := newGrokProxyFailureAccount(31, 931, version)
	repo := &grokProxyRecoveryRepoStub{
		accounts:              map[int64]*Account{account.ID: account},
		currentProxyUpdatedAt: version,
	}
	verifier := &grokProxyRecoveryVerifierStub{version: version, failures: map[int64]error{}}
	cache := &tempUnschedCacheRecorder{deleteErr: errors.New("redis unavailable")}
	cleaner := &grokSchedulingBlockCleanerStub{}
	svc := NewRateLimitService(repo, nil, nil, nil, cache)
	svc.SetGrokProxyCredentialRecoveryVerifier(verifier)
	svc.SetGrokSchedulingBlockCleaner(cleaner)

	result, err := svc.RecoverGrokProxyCredentialFailure(context.Background(), account.ID)

	require.ErrorContains(t, err, "temporary scheduling cache")
	require.NotNil(t, result)
	require.True(t, result.ClearedError, "数据库原子恢复已经完成")
	require.True(t, result.RestoredSchedulable)
	require.True(t, result.RuntimeCleanupIncomplete, "Redis 清理失败不得报告为完全恢复")
	require.Equal(t, StatusActive, account.Status)
	require.True(t, account.Schedulable)
	require.Equal(t, []int64{account.ID}, cleaner.cleared, "Redis 失败不应阻止当前实例 runtime block 清理")
}

func TestGrokProxyCredentialRecoveryDoesNotRecoverRevokedOrManuallyDisabledAccount(t *testing.T) {
	version := time.Date(2026, 8, 14, 2, 0, 0, 0, time.UTC)
	revoked := newGrokProxyFailureAccount(4, 94, version)
	revoked.ErrorMessage = string(GrokCredentialReasonRevoked)
	manual := newGrokProxyFailureAccount(5, 94, version)
	manual.Status = StatusDisabled
	repo := &grokProxyRecoveryRepoStub{
		accounts: map[int64]*Account{revoked.ID: revoked, manual.ID: manual},
	}
	verifier := &grokProxyRecoveryVerifierStub{version: version, failures: map[int64]error{}}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	svc.SetGrokProxyCredentialRecoveryVerifier(verifier)

	for _, accountID := range []int64{revoked.ID, manual.ID} {
		result, err := svc.RecoverGrokProxyCredentialFailure(context.Background(), accountID)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.False(t, result.ClearedError)
		require.False(t, result.RestoredSchedulable)
	}
	require.Empty(t, verifier.callIDs)
	require.Empty(t, repo.recoverCalls)
}

func TestGrokProxyCredentialRecoverySharedProxyOnlyRecoversVerifiedAccounts(t *testing.T) {
	version := time.Date(2026, 8, 14, 2, 0, 0, 0, time.UTC)
	proxyID := int64(95)
	first := newGrokProxyFailureAccount(6, proxyID, version)
	second := newGrokProxyFailureAccount(7, proxyID, version)
	third := newGrokProxyFailureAccount(8, proxyID, version)
	repo := &grokProxyRecoveryRepoStub{
		accounts: map[int64]*Account{
			first.ID:  first,
			second.ID: second,
			third.ID:  third,
		},
		candidates:            []Account{*first, *second, *third},
		currentProxyUpdatedAt: version,
	}
	verifier := &grokProxyRecoveryVerifierStub{
		version:  version,
		failures: map[int64]error{second.ID: errors.New("invalid_grant")},
	}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	svc.SetGrokProxyCredentialRecoveryVerifier(verifier)

	result, err := svc.RecoverGrokProxyCredentialFailuresByProxy(context.Background(), proxyID)

	require.NoError(t, err)
	require.Equal(t, 3, result.Candidates)
	require.Equal(t, 2, result.Recovered)
	require.Equal(t, 1, result.Failed)
	require.Equal(t, StatusActive, first.Status)
	require.Equal(t, StatusError, second.Status)
	require.Equal(t, StatusActive, third.Status)
}

func TestScheduleGrokProxyCredentialRecoveryCoalescesSameProxyInFlight(t *testing.T) {
	proxyID := int64(96)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	repo := &grokProxyRecoveryRepoStub{
		accounts:    map[int64]*Account{},
		listStarted: started,
		listRelease: release,
	}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)

	svc.ScheduleGrokProxyCredentialRecovery(proxyID)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("首个代理恢复批次未启动")
	}
	svc.ScheduleGrokProxyCredentialRecovery(proxyID)

	repo.mu.Lock()
	listCallsWhileBlocked := repo.listCalls
	repo.mu.Unlock()
	require.Equal(t, 1, listCallsWhileBlocked, "同一 proxyID 的并发调度必须合并")
	close(release)
	require.Eventually(t, func() bool {
		svc.grokProxyRecoveryMu.Lock()
		defer svc.grokProxyRecoveryMu.Unlock()
		_, exists := svc.grokProxyRecoveryInFlight[proxyID]
		return !exists
	}, time.Second, 10*time.Millisecond)

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, 2, repo.listCalls, "并发触发应合并为当前批次后的单次追赶，不得丢失更新")
	require.Equal(t, 1, repo.listMaxActive, "同一 proxyID 不得并发启动多个恢复批次")
}
