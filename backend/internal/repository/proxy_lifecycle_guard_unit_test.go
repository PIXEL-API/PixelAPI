//go:build unit

package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func newProxyLifecycleGuardSQLMock(t *testing.T) (*proxyRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	driver := entsql.OpenDB(dialect.Postgres, db)
	client := dbent.NewClient(dbent.Driver(driver))
	t.Cleanup(func() { _ = client.Close() })
	return newProxyRepositoryWithSQL(client, db), mock
}

func TestCountProxyOwnerAssignmentConflictsIncludesPlatformAccounts(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*FROM accounts.*proxy_id = \$1.*deleted_at IS NULL.*\(owner_user_id IS NULL OR owner_user_id <> \$2\)`).
		WithArgs(int64(20), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(2)))

	conflicts, err := countProxyOwnerAssignmentConflicts(context.Background(), db, 20, 42)

	require.NoError(t, err)
	require.Equal(t, int64(2), conflicts)
	require.NoError(t, mock.ExpectationsWereMet())
}

func proxyUpdateGuardCandidate(proxyID int64, maxAccounts int) *service.Proxy {
	return &service.Proxy{
		ID:             proxyID,
		Name:           "guarded proxy",
		Protocol:       "http",
		Host:           "127.0.0.1",
		Port:           8080,
		Status:         service.StatusActive,
		MaxAccounts:    maxAccounts,
		FallbackMode:   service.FallbackModeNone,
		ExpiryWarnDays: 7,
		UpdatedAt:      time.Date(2026, time.August, 31, 11, 0, 0, 0, time.UTC),
	}
}

func expectProxyMutationTargetsLock(
	mock sqlmock.Sqlmock,
	ids []int64,
	targets ...lockedProxyMutationTarget,
) {
	rows := sqlmock.NewRows([]string{"id", "owner_user_id", "updated_at"})
	for _, target := range targets {
		rows.AddRow(target.id, target.ownerUserID, target.updatedAt)
	}
	mock.ExpectQuery(`(?s)SELECT id, owner_user_id, updated_at.*FROM proxies.*id = ANY\(\$1\).*deleted_at IS NULL.*ORDER BY id ASC.*FOR UPDATE`).
		WithArgs(pq.Array(ids)).
		WillReturnRows(rows)
}

func expectProxyUpdateGuardLockAndCount(
	mock sqlmock.Sqlmock,
	candidate *service.Proxy,
	currentAccounts int64,
) {
	expectProxyMutationTargetsLock(mock, []int64{candidate.ID}, lockedProxyMutationTarget{
		id:          candidate.ID,
		ownerUserID: candidate.OwnerUserID,
		updatedAt:   candidate.UpdatedAt,
	})
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*FROM accounts.*proxy_id = \$1.*deleted_at IS NULL`).
		WithArgs(candidate.ID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(currentAccounts))
}

func expectProxyUpdateGuardSave(mock sqlmock.Sqlmock, candidate *service.Proxy) {
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	mock.ExpectExec(`(?s)UPDATE "proxies" SET .*WHERE "id" = \$[0-9]+ AND \("proxies"\."updated_at" = \$[0-9]+ AND "proxies"\."deleted_at" IS NULL\)`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)SELECT .*FROM "proxies".*WHERE .*"id" = \$1`).
		WithArgs(candidate.ID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "created_at", "updated_at", "deleted_at", "name", "protocol", "host", "port",
			"username", "password", "owner_user_id", "platform", "required_account_level", "status",
			"max_accounts", "expires_at", "fallback_mode", "backup_proxy_id", "expiry_warn_days",
		}).AddRow(
			candidate.ID, now, now, nil, candidate.Name, candidate.Protocol, candidate.Host, candidate.Port,
			nil, nil, candidate.OwnerUserID, candidate.Platform, candidate.RequiredAccountLevel, candidate.Status,
			candidate.MaxAccounts, nil, candidate.FallbackMode, candidate.BackupProxyID, candidate.ExpiryWarnDays,
		))
}

func proxyCreateGuardCandidate(backupID int64) *service.Proxy {
	return &service.Proxy{
		Name:           "guarded create proxy",
		Protocol:       "http",
		Host:           "127.0.0.1",
		Port:           8080,
		Status:         service.StatusActive,
		MaxAccounts:    5,
		FallbackMode:   service.FallbackModeProxy,
		BackupProxyID:  &backupID,
		ExpiryWarnDays: 7,
	}
}

func expectProxyCreateSave(mock sqlmock.Sqlmock, createdID int64) {
	mock.ExpectQuery(`(?s)INSERT INTO "proxies".*RETURNING "id"`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(createdID))
}

func expectLiveProxyBackupReferenceCount(mock sqlmock.Sqlmock, proxyID, count int64) {
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*FROM proxies.*backup_proxy_id = \$1.*deleted_at IS NULL`).
		WithArgs(proxyID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
}

func TestProxyRepositoryCreateLocksBackupAndPublishesAfterCommit(t *testing.T) {
	repo, mock := newProxyLifecycleGuardSQLMock(t)
	backupID := int64(9)
	candidate := proxyCreateGuardCandidate(backupID)
	version := time.Date(2026, time.August, 31, 10, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	expectProxyMutationTargetsLock(mock, []int64{backupID}, lockedProxyMutationTarget{
		id:        backupID,
		updatedAt: version,
	})
	expectProxyCreateSave(mock, 77)
	mock.ExpectCommit()

	err := repo.Create(context.Background(), candidate)

	require.NoError(t, err)
	require.Equal(t, int64(77), candidate.ID)
	require.False(t, candidate.CreatedAt.IsZero())
	require.False(t, candidate.UpdatedAt.IsZero())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyRepositoryCreateCommitFailureDoesNotPublishPhantomState(t *testing.T) {
	repo, mock := newProxyLifecycleGuardSQLMock(t)
	backupID := int64(9)
	candidate := proxyCreateGuardCandidate(backupID)
	before := *candidate
	commitErr := errors.New("commit failed")

	mock.ExpectBegin()
	expectProxyMutationTargetsLock(mock, []int64{backupID}, lockedProxyMutationTarget{
		id:        backupID,
		updatedAt: time.Date(2026, time.August, 31, 10, 0, 0, 0, time.UTC),
	})
	expectProxyCreateSave(mock, 78)
	mock.ExpectCommit().WillReturnError(commitErr)

	err := repo.Create(context.Background(), candidate)

	require.ErrorIs(t, err, commitErr)
	require.Equal(t, before, *candidate, "commit failure must not publish generated proxy fields")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyRepositoryCreateRejectsMissingLockedBackup(t *testing.T) {
	repo, mock := newProxyLifecycleGuardSQLMock(t)
	backupID := int64(404)
	candidate := proxyCreateGuardCandidate(backupID)

	mock.ExpectBegin()
	expectProxyMutationTargetsLock(mock, []int64{backupID})
	mock.ExpectRollback()

	err := repo.Create(context.Background(), candidate)

	require.ErrorIs(t, err, service.ErrProxyBackupInvalid)
	require.Zero(t, candidate.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyRepositoryCreateRejectsLockedBackupOwnerConflict(t *testing.T) {
	repo, mock := newProxyLifecycleGuardSQLMock(t)
	backupID := int64(9)
	backupOwnerID := int64(42)
	candidate := proxyCreateGuardCandidate(backupID)

	mock.ExpectBegin()
	expectProxyMutationTargetsLock(mock, []int64{backupID}, lockedProxyMutationTarget{
		id:          backupID,
		ownerUserID: &backupOwnerID,
		updatedAt:   time.Date(2026, time.August, 31, 10, 0, 0, 0, time.UTC),
	})
	mock.ExpectRollback()

	err := repo.Create(context.Background(), candidate)

	require.ErrorIs(t, err, service.ErrProxyBackupOwnerConflict)
	require.Zero(t, candidate.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyRepositoryCreateReusesOuterTransactionWithoutOwningCommit(t *testing.T) {
	repo, mock := newProxyLifecycleGuardSQLMock(t)
	backupID := int64(9)
	staged := proxyCreateGuardCandidate(backupID)

	mock.ExpectBegin()
	outerTx, err := repo.client.Tx(context.Background())
	require.NoError(t, err)
	outerCtx := dbent.NewTxContext(context.Background(), outerTx)
	expectProxyMutationTargetsLock(mock, []int64{backupID}, lockedProxyMutationTarget{
		id:        backupID,
		updatedAt: time.Date(2026, time.August, 31, 10, 0, 0, 0, time.UTC),
	})
	expectProxyCreateSave(mock, 79)

	require.NoError(t, repo.Create(outerCtx, staged))
	require.Equal(t, int64(79), staged.ID, "outer UoW needs the staged ID for dependent writes")
	mock.ExpectRollback()
	require.NoError(t, outerTx.Rollback(), "repository must leave outer transaction ownership to the caller")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyRepositoryUpdateRejectsLimitBelowLockedCurrentCount(t *testing.T) {
	repo, mock := newProxyLifecycleGuardSQLMock(t)
	candidate := proxyUpdateGuardCandidate(21, 5)

	mock.ExpectBegin()
	expectProxyUpdateGuardLockAndCount(mock, candidate, 6)
	mock.ExpectRollback()

	err := repo.Update(context.Background(), candidate)

	require.Error(t, err)
	require.Equal(t, "PROXY_ACCOUNT_LIMIT_BELOW_CURRENT", infraerrors.Reason(err))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyRepositoryUpdateAllowsUnlimitedAndExactCurrentLimit(t *testing.T) {
	tests := []struct {
		name            string
		maxAccounts     int
		currentAccounts int64
	}{
		{name: "zero remains unlimited", maxAccounts: 0, currentAccounts: 99},
		{name: "exact current count is allowed", maxAccounts: 5, currentAccounts: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock := newProxyLifecycleGuardSQLMock(t)
			candidate := proxyUpdateGuardCandidate(22, tt.maxAccounts)

			mock.ExpectBegin()
			expectProxyUpdateGuardLockAndCount(mock, candidate, tt.currentAccounts)
			expectProxyUpdateGuardSave(mock, candidate)
			mock.ExpectCommit()

			err := repo.Update(context.Background(), candidate)

			require.NoError(t, err)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestProxyRepositoryUpdateWithOwnerAssignmentChecksCapacityThenOwner(t *testing.T) {
	repo, mock := newProxyLifecycleGuardSQLMock(t)
	candidate := proxyUpdateGuardCandidate(23, 5)
	ownerUserID := int64(42)
	candidate.OwnerUserID = &ownerUserID

	mock.ExpectBegin()
	expectProxyUpdateGuardLockAndCount(mock, candidate, 5)
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*FROM accounts.*proxy_id = \$1.*deleted_at IS NULL.*owner_user_id IS NULL OR owner_user_id <> \$2`).
		WithArgs(candidate.ID, ownerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	expectProxyUpdateGuardSave(mock, candidate)
	mock.ExpectCommit()

	err := repo.UpdateWithOwnerAssignment(context.Background(), candidate)

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyRepositoryUpdateReusesOuterTransactionWithoutOwningCommit(t *testing.T) {
	repo, mock := newProxyLifecycleGuardSQLMock(t)
	candidate := proxyUpdateGuardCandidate(24, 3)

	mock.ExpectBegin()
	tx, err := repo.client.Tx(context.Background())
	require.NoError(t, err)
	txCtx := dbent.NewTxContext(context.Background(), tx)

	expectProxyUpdateGuardLockAndCount(mock, candidate, 3)
	expectProxyUpdateGuardSave(mock, candidate)
	mock.ExpectCommit()

	require.NoError(t, repo.Update(txCtx, candidate))
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLockProxyMutationTargetsUsesAscendingUniqueIDs(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	version := time.Date(2026, time.August, 31, 11, 0, 0, 0, time.UTC)

	expectProxyMutationTargetsLock(
		mock,
		[]int64{10, 30},
		lockedProxyMutationTarget{id: 10, updatedAt: version},
		lockedProxyMutationTarget{id: 30, updatedAt: version},
	)

	locked, err := lockProxyMutationTargets(context.Background(), db, []int64{30, 10, 30})

	require.NoError(t, err)
	require.Len(t, locked, 2)
	require.Contains(t, locked, int64(10))
	require.Contains(t, locked, int64(30))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyRepositoryUpdateRejectsStaleSnapshotBeforeCountingOrSaving(t *testing.T) {
	repo, mock := newProxyLifecycleGuardSQLMock(t)
	candidate := proxyUpdateGuardCandidate(25, 5)

	mock.ExpectBegin()
	expectProxyMutationTargetsLock(mock, []int64{candidate.ID}, lockedProxyMutationTarget{
		id:        candidate.ID,
		updatedAt: candidate.UpdatedAt.Add(time.Second),
	})
	mock.ExpectRollback()

	err := repo.Update(context.Background(), candidate)

	require.ErrorIs(t, err, service.ErrProxyMutationStale)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyRepositoryUpdateMapsOptimisticPredicateMissToStale(t *testing.T) {
	repo, mock := newProxyLifecycleGuardSQLMock(t)
	candidate := proxyUpdateGuardCandidate(27, 5)

	mock.ExpectBegin()
	expectProxyUpdateGuardLockAndCount(mock, candidate, 0)
	mock.ExpectExec(`(?s)UPDATE "proxies" SET .*WHERE "id" = \$[0-9]+ AND \("proxies"\."updated_at" = \$[0-9]+ AND "proxies"\."deleted_at" IS NULL\)`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(`(?s)SELECT EXISTS .*FROM "proxies".*"id" = \$1.*"updated_at" = \$2.*"deleted_at" IS NULL`).
		WithArgs(candidate.ID, candidate.UpdatedAt).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	err := repo.Update(context.Background(), candidate)

	require.ErrorIs(t, err, service.ErrProxyMutationStale)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyRepositoryUpdateLocksAndValidatesDesiredBackup(t *testing.T) {
	repo, mock := newProxyLifecycleGuardSQLMock(t)
	candidate := proxyUpdateGuardCandidate(30, 5)
	backupID := int64(10)
	candidate.FallbackMode = service.FallbackModeProxy
	candidate.BackupProxyID = &backupID

	mock.ExpectBegin()
	expectProxyMutationTargetsLock(
		mock,
		[]int64{backupID, candidate.ID},
		lockedProxyMutationTarget{id: backupID, updatedAt: candidate.UpdatedAt},
		lockedProxyMutationTarget{id: candidate.ID, updatedAt: candidate.UpdatedAt},
	)
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*FROM accounts.*proxy_id = \$1.*deleted_at IS NULL`).
		WithArgs(candidate.ID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	expectProxyUpdateGuardSave(mock, candidate)
	mock.ExpectCommit()

	err := repo.Update(context.Background(), candidate)

	require.NoError(t, err)
	require.Equal(t, backupID, *candidate.BackupProxyID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyRepositoryUpdateRejectsMissingLockedBackup(t *testing.T) {
	repo, mock := newProxyLifecycleGuardSQLMock(t)
	candidate := proxyUpdateGuardCandidate(30, 5)
	backupID := int64(10)
	candidate.FallbackMode = service.FallbackModeProxy
	candidate.BackupProxyID = &backupID

	mock.ExpectBegin()
	expectProxyMutationTargetsLock(
		mock,
		[]int64{backupID, candidate.ID},
		lockedProxyMutationTarget{id: candidate.ID, updatedAt: candidate.UpdatedAt},
	)
	mock.ExpectRollback()

	err := repo.Update(context.Background(), candidate)

	require.ErrorIs(t, err, service.ErrProxyBackupInvalid)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyRepositoryUpdateRejectsLockedBackupOwnerConflict(t *testing.T) {
	repo, mock := newProxyLifecycleGuardSQLMock(t)
	candidate := proxyUpdateGuardCandidate(30, 5)
	backupID := int64(10)
	backupOwnerID := int64(42)
	candidate.FallbackMode = service.FallbackModeProxy
	candidate.BackupProxyID = &backupID

	mock.ExpectBegin()
	expectProxyMutationTargetsLock(
		mock,
		[]int64{backupID, candidate.ID},
		lockedProxyMutationTarget{id: backupID, ownerUserID: &backupOwnerID, updatedAt: candidate.UpdatedAt},
		lockedProxyMutationTarget{id: candidate.ID, updatedAt: candidate.UpdatedAt},
	)
	mock.ExpectRollback()

	err := repo.Update(context.Background(), candidate)

	require.ErrorIs(t, err, service.ErrProxyBackupOwnerConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyRepositoryUpdateCannotBypassOwnerAssignmentGuard(t *testing.T) {
	repo, mock := newProxyLifecycleGuardSQLMock(t)
	candidate := proxyUpdateGuardCandidate(26, 5)
	ownerUserID := int64(42)
	candidate.OwnerUserID = &ownerUserID

	mock.ExpectBegin()
	expectProxyMutationTargetsLock(mock, []int64{candidate.ID}, lockedProxyMutationTarget{
		id:        candidate.ID,
		updatedAt: candidate.UpdatedAt,
	})
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*FROM accounts.*proxy_id = \$1.*deleted_at IS NULL`).
		WithArgs(candidate.ID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*FROM accounts.*proxy_id = \$1.*deleted_at IS NULL.*owner_user_id IS NULL OR owner_user_id <> \$2`).
		WithArgs(candidate.ID, ownerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectRollback()

	err := repo.Update(context.Background(), candidate)

	require.ErrorIs(t, err, service.ErrProxyOwnerConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyRepositoryDeleteIfUnusedLocksCountsAndSoftDeletesAtomically(t *testing.T) {
	repo, mock := newProxyLifecycleGuardSQLMock(t)
	proxyID := int64(17)

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT id.*FROM proxies.*id = \$1 AND deleted_at IS NULL.*FOR UPDATE`).
		WithArgs(proxyID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(proxyID))
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*FROM accounts.*proxy_id = \$1 AND deleted_at IS NULL`).
		WithArgs(proxyID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	expectLiveProxyBackupReferenceCount(mock, proxyID, 0)
	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE proxies
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`)).
		WithArgs(proxyID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repo.DeleteIfUnused(context.Background(), proxyID)

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyRepositoryDeleteCannotBypassAtomicGuard(t *testing.T) {
	repo, mock := newProxyLifecycleGuardSQLMock(t)
	proxyID := int64(19)

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT id.*FROM proxies.*id = \$1 AND deleted_at IS NULL.*FOR UPDATE`).
		WithArgs(proxyID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(proxyID))
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*FROM accounts.*proxy_id = \$1 AND deleted_at IS NULL`).
		WithArgs(proxyID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectRollback()

	err := repo.Delete(context.Background(), proxyID)

	require.ErrorIs(t, err, service.ErrProxyInUse)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyRepositoryDeleteIfUnusedRejectsBoundProxyAndRollsBack(t *testing.T) {
	repo, mock := newProxyLifecycleGuardSQLMock(t)
	proxyID := int64(18)

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT id.*FROM proxies.*id = \$1 AND deleted_at IS NULL.*FOR UPDATE`).
		WithArgs(proxyID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(proxyID))
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*FROM accounts.*proxy_id = \$1 AND deleted_at IS NULL`).
		WithArgs(proxyID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectRollback()

	err := repo.DeleteIfUnused(context.Background(), proxyID)

	require.ErrorIs(t, err, service.ErrProxyInUse)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyRepositoryDeleteIfUnusedRejectsLiveBackupReference(t *testing.T) {
	repo, mock := newProxyLifecycleGuardSQLMock(t)
	proxyID := int64(20)

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT id.*FROM proxies.*id = \$1 AND deleted_at IS NULL.*FOR UPDATE`).
		WithArgs(proxyID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(proxyID))
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*FROM accounts.*proxy_id = \$1 AND deleted_at IS NULL`).
		WithArgs(proxyID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	expectLiveProxyBackupReferenceCount(mock, proxyID, 1)
	mock.ExpectRollback()

	err := repo.DeleteIfUnused(context.Background(), proxyID)

	require.ErrorIs(t, err, service.ErrProxyBackupInUse)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyRepositoryDeleteIfUnusedKeepsMissingDeleteIdempotent(t *testing.T) {
	repo, mock := newProxyLifecycleGuardSQLMock(t)
	proxyID := int64(404)

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT id.*FROM proxies.*id = \$1 AND deleted_at IS NULL.*FOR UPDATE`).
		WithArgs(proxyID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()

	err := repo.DeleteIfUnused(context.Background(), proxyID)

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyRepositoryDeleteIfUnusedReusesOuterTransaction(t *testing.T) {
	repo, mock := newProxyLifecycleGuardSQLMock(t)
	proxyID := int64(23)

	mock.ExpectBegin()
	outerTx, err := repo.client.Tx(context.Background())
	require.NoError(t, err)
	outerCtx := dbent.NewTxContext(context.Background(), outerTx)
	mock.ExpectQuery(`(?s)SELECT id.*FROM proxies.*id = \$1 AND deleted_at IS NULL.*FOR UPDATE`).
		WithArgs(proxyID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(proxyID))
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*FROM accounts.*proxy_id = \$1.*deleted_at IS NULL`).
		WithArgs(proxyID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))
	expectLiveProxyBackupReferenceCount(mock, proxyID, 0)
	mock.ExpectExec(`(?s)UPDATE proxies.*SET deleted_at = NOW\(\), updated_at = NOW\(\).*WHERE id = \$1 AND deleted_at IS NULL`).
		WithArgs(proxyID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = repo.DeleteIfUnused(outerCtx, proxyID)

	require.NoError(t, err)
	mock.ExpectRollback()
	require.NoError(t, outerTx.Rollback(), "repository must leave outer transaction ownership to the caller")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildProxyExpiryLockPlanSortsCompleteFallbackChain(t *testing.T) {
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	sourceID := int64(30)
	middleID := int64(20)
	targetID := int64(10)
	proxies := map[int64]service.Proxy{
		sourceID: {
			ID: sourceID, Status: service.StatusActive, ExpiresAt: &past,
			FallbackMode: service.FallbackModeProxy, BackupProxyID: &middleID,
		},
		middleID: {
			ID: middleID, Status: service.StatusExpired, ExpiresAt: &past,
			FallbackMode: service.FallbackModeProxy, BackupProxyID: &targetID,
		},
		targetID: {ID: targetID, Status: service.StatusActive, ExpiresAt: &future},
	}

	plan := buildProxyExpiryLockPlan(proxies[sourceID], proxies, now)

	require.Equal(t, []int64{sourceID, middleID, targetID}, plan.path)
	require.Equal(t, []int64{targetID, middleID, sourceID}, plan.proxyIDs)
	require.True(t, plan.change)
	require.NotNil(t, plan.targetID)
	require.Equal(t, targetID, *plan.targetID)
}

func TestBuildProxyExpiryLockPlanDetectsLockedChainDrift(t *testing.T) {
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	firstTargetID := int64(10)
	secondTargetID := int64(11)
	source := service.Proxy{
		ID: 30, Status: service.StatusActive, ExpiresAt: &past,
		FallbackMode: service.FallbackModeProxy, BackupProxyID: &firstTargetID,
	}
	preview := map[int64]service.Proxy{
		source.ID:     source,
		firstTargetID: {ID: firstTargetID, Status: service.StatusActive, ExpiresAt: &future},
	}
	currentSource := source
	currentSource.BackupProxyID = &secondTargetID
	current := map[int64]service.Proxy{
		currentSource.ID: currentSource,
		secondTargetID:   {ID: secondTargetID, Status: service.StatusActive, ExpiresAt: &future},
	}

	require.False(t, buildProxyExpiryLockPlan(source, preview, now).equal(
		buildProxyExpiryLockPlan(currentSource, current, now),
	))
}

func TestBuildProxyExpiryLockPlanDeduplicatesCycleInAscendingLockSet(t *testing.T) {
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	sourceID := int64(30)
	middleID := int64(10)
	proxies := map[int64]service.Proxy{
		sourceID: {
			ID: sourceID, Status: service.StatusActive, ExpiresAt: &past,
			FallbackMode: service.FallbackModeProxy, BackupProxyID: &middleID,
		},
		middleID: {
			ID: middleID, Status: service.StatusExpired, ExpiresAt: &past,
			FallbackMode: service.FallbackModeProxy, BackupProxyID: &sourceID,
		},
	}

	plan := buildProxyExpiryLockPlan(proxies[sourceID], proxies, now)

	require.Equal(t, []int64{sourceID, middleID, sourceID}, plan.path)
	require.Equal(t, []int64{middleID, sourceID}, plan.proxyIDs)
	require.False(t, plan.change, "cycle must fail closed without rerouting")
	require.Nil(t, plan.targetID)
}

func TestLockProxyExpiryPlanUsesAscendingSkipLockedQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)SELECT id, owner_user_id, platform, required_account_level, status,.*FROM proxies.*id = ANY\(\$1\).*ORDER BY id ASC.*FOR UPDATE SKIP LOCKED`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "owner_user_id", "platform", "required_account_level", "status",
			"max_accounts", "expires_at", "fallback_mode", "backup_proxy_id",
		}).
			AddRow(int64(10), nil, "", "", service.StatusActive, 0, now.Add(time.Hour), service.FallbackModeNone, nil).
			AddRow(int64(30), nil, "", "", service.StatusActive, 0, now.Add(-time.Hour), service.FallbackModeProxy, int64(10)))

	locked, err := lockProxyExpiryPlan(context.Background(), db, []int64{10, 30})

	require.NoError(t, err)
	require.True(t, proxyExpirySnapshotContainsIDs(locked, []int64{10, 30}))
	require.NoError(t, mock.ExpectationsWereMet())
}
