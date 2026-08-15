//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newGrokProxyRecoverySQLMock(t *testing.T) (*accountRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return &accountRepository{sql: db}, mock
}

func grokProxyRecoverySnapshot(proxyID int64, proxyUpdatedAt time.Time) service.GrokCredentialMutationSnapshot {
	return service.GrokCredentialMutationSnapshot{
		CredentialsJSON: `{"access_token":"access","refresh_token":"refresh"}`,
		ProxyID:         &proxyID,
		ProxyUpdatedAt:  &proxyUpdatedAt,
	}
}

func TestSetGrokCredentialErrorIfMatchRejectsStaleProxyVersionCAS(t *testing.T) {
	repo, mock := newGrokProxyRecoverySQLMock(t)
	accountID := int64(41)
	proxyID := int64(91)
	observedVersion := time.Date(2026, 8, 14, 1, 2, 3, 0, time.UTC)
	snapshot := grokProxyRecoverySnapshot(proxyID, observedVersion)

	mock.ExpectExec(`(?s)UPDATE accounts AS a.*a\.credentials = \$8::jsonb.*a\.proxy_id = \$9.*p\.updated_at = \$10.*INSERT INTO scheduler_outbox.*SELECT \$11`).
		WithArgs(
			service.StatusError,
			string(service.GrokCredentialReasonProxyInvalid),
			accountID,
			service.StatusActive,
			service.PlatformGrok,
			service.AccountTypeOAuth,
			service.AccountTypeAPIKey,
			snapshot.CredentialsJSON,
			proxyID,
			observedVersion,
			service.SchedulerOutboxEventAccountChanged,
		).
		WillReturnResult(sqlmock.NewResult(0, 0))

	applied, err := repo.SetGrokCredentialErrorIfMatch(
		context.Background(),
		accountID,
		snapshot,
		string(service.GrokCredentialReasonProxyInvalid),
	)

	require.NoError(t, err)
	require.False(t, applied, "代理 updated_at 已变化时必须 CAS miss")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSetGrokCredentialTempUnschedulableIfMatchAcceptsCurrentProxyVersion(t *testing.T) {
	repo, mock := newGrokProxyRecoverySQLMock(t)
	accountID := int64(42)
	proxyID := int64(92)
	observedVersion := time.Date(2026, 8, 14, 2, 3, 4, 0, time.UTC)
	until := observedVersion.Add(10 * time.Minute)
	snapshot := grokProxyRecoverySnapshot(proxyID, observedVersion)

	mock.ExpectExec(`(?s)UPDATE accounts AS a.*temp_unschedulable_until = CASE.*a\.credentials = \$7::jsonb.*a\.proxy_id = \$8.*p\.updated_at = \$9.*INSERT INTO scheduler_outbox.*SELECT \$10`).
		WithArgs(
			until,
			string(service.GrokCredentialReasonProxyInvalid),
			accountID,
			service.StatusActive,
			service.PlatformGrok,
			service.AccountTypeOAuth,
			snapshot.CredentialsJSON,
			proxyID,
			observedVersion,
			service.SchedulerOutboxEventAccountChanged,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	applied, err := repo.SetGrokCredentialTempUnschedulableIfMatch(
		context.Background(),
		accountID,
		snapshot,
		until,
		string(service.GrokCredentialReasonProxyInvalid),
	)

	require.NoError(t, err)
	require.True(t, applied)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRecoverGrokProxyCredentialFailureIfMatchAtomicallyRestoresExactFailure(t *testing.T) {
	repo, mock := newGrokProxyRecoverySQLMock(t)
	accountID := int64(43)
	proxyID := int64(93)
	observedVersion := time.Date(2026, 8, 14, 3, 4, 5, 0, time.UTC)
	snapshot := grokProxyRecoverySnapshot(proxyID, observedVersion)

	mock.ExpectExec(`(?s)UPDATE accounts AS a.*SET status = \$1,.*error_message = '',.*error_since = NULL,.*schedulable = TRUE,.*temp_unschedulable_until = NULL,.*temp_unschedulable_reason = NULL.*a\.platform = \$3.*a\.type = \$4.*a\.status = \$5.*a\.schedulable IS FALSE.*a\.error_message = \$6.*a\.credentials = \$7::jsonb.*a\.proxy_id = \$8.*p\.updated_at = \$9.*INSERT INTO scheduler_outbox.*SELECT \$10`).
		WithArgs(
			service.StatusActive,
			accountID,
			service.PlatformGrok,
			service.AccountTypeOAuth,
			service.StatusError,
			string(service.GrokCredentialReasonProxyInvalid),
			snapshot.CredentialsJSON,
			proxyID,
			observedVersion,
			service.SchedulerOutboxEventAccountChanged,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	applied, err := repo.RecoverGrokProxyCredentialFailureIfMatch(context.Background(), accountID, snapshot)

	require.NoError(t, err)
	require.True(t, applied)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRecoverGrokProxyCredentialFailureIfMatchRejectsStateOrProxyMismatch(t *testing.T) {
	repo, mock := newGrokProxyRecoverySQLMock(t)
	accountID := int64(44)
	proxyID := int64(94)
	observedVersion := time.Date(2026, 8, 14, 4, 5, 6, 0, time.UTC)
	snapshot := grokProxyRecoverySnapshot(proxyID, observedVersion)

	mock.ExpectExec(`(?s)UPDATE accounts AS a.*a\.type = \$4.*a\.status = \$5.*a\.error_message = \$6.*a\.credentials = \$7::jsonb.*a\.proxy_id = \$8.*p\.updated_at = \$9`).
		WithArgs(
			service.StatusActive,
			accountID,
			service.PlatformGrok,
			service.AccountTypeOAuth,
			service.StatusError,
			string(service.GrokCredentialReasonProxyInvalid),
			snapshot.CredentialsJSON,
			proxyID,
			observedVersion,
			service.SchedulerOutboxEventAccountChanged,
		).
		WillReturnResult(sqlmock.NewResult(0, 0))

	applied, err := repo.RecoverGrokProxyCredentialFailureIfMatch(context.Background(), accountID, snapshot)

	require.NoError(t, err)
	require.False(t, applied, "非精确 proxy-invalid OAuth error 或代理版本变化时不得恢复")
	require.NoError(t, mock.ExpectationsWereMet())
}
