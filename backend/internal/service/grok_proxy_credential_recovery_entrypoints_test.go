//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type grokRecoveryEntrypointStub struct {
	recoverCalls  []int64
	scheduleCalls []int64
	recoverErr    error
	onRecover     func(int64)
}

func (s *grokRecoveryEntrypointStub) RecoverGrokProxyCredentialFailure(
	_ context.Context,
	accountID int64,
) (*SuccessfulTestRecoveryResult, error) {
	s.recoverCalls = append(s.recoverCalls, accountID)
	if s.recoverErr != nil {
		return nil, s.recoverErr
	}
	if s.onRecover != nil {
		s.onRecover(accountID)
	}
	return &SuccessfulTestRecoveryResult{ClearedError: true, RestoredSchedulable: true}, nil
}

func (s *grokRecoveryEntrypointStub) ScheduleGrokProxyCredentialRecovery(proxyID int64) {
	s.scheduleCalls = append(s.scheduleCalls, proxyID)
}

type grokRecoveryAdminAccountRepoStub struct {
	AccountRepository
	account         *Account
	clearErrorCalls int
}

func (r *grokRecoveryAdminAccountRepoStub) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}

func (r *grokRecoveryAdminAccountRepoStub) ClearError(context.Context, int64) error {
	r.clearErrorCalls++
	return nil
}

type grokRecoveryProxyRepoStub struct {
	ProxyRepository
	proxy       *Proxy
	updateCalls int
}

func (r *grokRecoveryProxyRepoStub) GetByID(context.Context, int64) (*Proxy, error) {
	return r.proxy, nil
}

func (r *grokRecoveryProxyRepoStub) Update(context.Context, *Proxy) error {
	r.updateCalls++
	return nil
}

type grokRecoveryOwnedAccountRepoStub struct {
	AccountRepository
	account     *Account
	updateCalls int
}

func (r *grokRecoveryOwnedAccountRepoStub) GetByID(_ context.Context, id int64) (*Account, error) {
	if r.account == nil || r.account.ID != id {
		return nil, ErrAccountNotFound
	}
	return r.account, nil
}

func (r *grokRecoveryOwnedAccountRepoStub) Update(_ context.Context, account *Account) error {
	r.updateCalls++
	r.account = account
	return nil
}

type grokRecoveryOwnedProxyRepoStub struct {
	proxy *Proxy
}

func (r *grokRecoveryOwnedProxyRepoStub) GetVisibleByID(context.Context, ProxyScope, int64) (*Proxy, error) {
	return r.proxy, nil
}

func (r *grokRecoveryOwnedProxyRepoStub) CountAccountsByProxyID(context.Context, int64) (int64, error) {
	return 1, nil
}

func TestAdminClearAccountErrorUsesVerifiedGrokProxyRecovery(t *testing.T) {
	version := time.Date(2026, 8, 14, 5, 0, 0, 0, time.UTC)
	account := newGrokProxyFailureAccount(51, 951, version)
	repo := &grokRecoveryAdminAccountRepoStub{account: account}
	recovery := &grokRecoveryEntrypointStub{onRecover: func(accountID int64) {
		require.Equal(t, account.ID, accountID)
		account.Status = StatusActive
		account.Schedulable = true
		account.ErrorMessage = ""
	}}
	svc := &adminServiceImpl{accountRepo: repo, grokProxyRecovery: recovery}

	updated, err := svc.ClearAccountError(context.Background(), account.ID)

	require.NoError(t, err)
	require.Same(t, account, updated)
	require.Equal(t, []int64{account.ID}, recovery.recoverCalls)
	require.Zero(t, repo.clearErrorCalls, "精确 Grok proxy-invalid 状态不能绕过验证直接清错")
	require.Equal(t, StatusActive, updated.Status)
	require.True(t, updated.Schedulable)
}

func TestAdminUpdateProxySchedulesRecoveryOnlyForRelevantActualChanges(t *testing.T) {
	tests := []struct {
		name              string
		input             UpdateProxyInput
		expectedSchedules int
	}{
		{name: "name_only", input: UpdateProxyInput{Name: "renamed"}},
		{name: "same_host", input: UpdateProxyInput{Host: "old.example.com"}},
		{name: "host_changed", input: UpdateProxyInput{Host: "new.example.com"}, expectedSchedules: 1},
		{name: "status_changed", input: UpdateProxyInput{Status: StatusDisabled}, expectedSchedules: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := &Proxy{
				ID:       952,
				Name:     "old-name",
				Protocol: "http",
				Host:     "old.example.com",
				Port:     8080,
				Status:   StatusActive,
				Platform: PlatformGrok,
			}
			proxyRepo := &grokRecoveryProxyRepoStub{proxy: proxy}
			recovery := &grokRecoveryEntrypointStub{}
			svc := &adminServiceImpl{proxyRepo: proxyRepo, grokProxyRecovery: recovery}

			updated, err := svc.UpdateProxy(context.Background(), proxy.ID, &tt.input)

			require.NoError(t, err)
			require.Same(t, proxy, updated)
			require.Equal(t, 1, proxyRepo.updateCalls)
			require.Len(t, recovery.scheduleCalls, tt.expectedSchedules)
			if tt.expectedSchedules > 0 {
				require.Equal(t, proxy.ID, recovery.scheduleCalls[0])
			}
		})
	}
}

func TestOwnedGrokReauthorizationRecoversOnlyAfterVerifierSuccess(t *testing.T) {
	const ownerUserID int64 = 601
	version := time.Date(2026, 8, 14, 6, 0, 0, 0, time.UTC)
	proxyID := int64(953)
	newCredentials := map[string]any{
		"access_token":  "new-access",
		"refresh_token": "new-refresh",
	}

	t.Run("success", func(t *testing.T) {
		account := newGrokProxyFailureAccount(52, proxyID, version)
		account.OwnerUserID = pointerTo(ownerUserID)
		repo := &grokRecoveryOwnedAccountRepoStub{account: account}
		proxyRepo := &grokRecoveryOwnedProxyRepoStub{proxy: &Proxy{
			ID: proxyID, Status: StatusActive, Platform: PlatformGrok,
		}}
		recovery := &grokRecoveryEntrypointStub{onRecover: func(int64) {
			account.Status = StatusActive
			account.Schedulable = true
			account.ErrorMessage = ""
		}}
		svc := NewAccountService(repo, nil, nil, nil, proxyRepo)
		svc.SetGrokProxyCredentialRecovery(recovery)

		updated, err := svc.UpdateOwned(
			context.Background(),
			ownerUserID,
			account.ID,
			UpdateAccountRequest{Credentials: &newCredentials},
		)

		require.NoError(t, err)
		require.Equal(t, 1, repo.updateCalls)
		require.Equal(t, []int64{account.ID}, recovery.recoverCalls)
		require.Equal(t, "new-refresh", updated.GetCredential("refresh_token"))
		require.Equal(t, StatusActive, updated.Status)
		require.True(t, updated.Schedulable)
	})

	t.Run("verification_failure_keeps_error_state", func(t *testing.T) {
		account := newGrokProxyFailureAccount(53, proxyID, version)
		account.OwnerUserID = pointerTo(ownerUserID)
		repo := &grokRecoveryOwnedAccountRepoStub{account: account}
		proxyRepo := &grokRecoveryOwnedProxyRepoStub{proxy: &Proxy{
			ID: proxyID, Status: StatusActive, Platform: PlatformGrok,
		}}
		recovery := &grokRecoveryEntrypointStub{recoverErr: errors.New("invalid_grant")}
		svc := NewAccountService(repo, nil, nil, nil, proxyRepo)
		svc.SetGrokProxyCredentialRecovery(recovery)

		updated, err := svc.UpdateOwned(
			context.Background(),
			ownerUserID,
			account.ID,
			UpdateAccountRequest{Credentials: &newCredentials},
		)

		require.Nil(t, updated)
		require.ErrorContains(t, err, "invalid_grant")
		require.Equal(t, 1, repo.updateCalls, "新凭证先持久化，恢复仍必须依赖后续强制验证")
		require.Equal(t, "new-refresh", account.GetCredential("refresh_token"))
		require.Equal(t, StatusError, account.Status)
		require.False(t, account.Schedulable)
		require.Equal(t, string(GrokCredentialReasonProxyInvalid), account.ErrorMessage)
	})
}

func pointerTo[T any](value T) *T {
	return &value
}
