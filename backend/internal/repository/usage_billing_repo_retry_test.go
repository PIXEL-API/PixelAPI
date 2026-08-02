package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestSplitAccountShareCreditsCapsRoundedInviteAtRemainingBalance(t *testing.T) {
	total := decimal.RequireFromString("0.0000000001")
	ratio := decimal.RequireFromString("0.5")

	owner, invite, platform := splitAccountShareCredits(total, ratio, ratio)

	require.True(t, owner.Equal(total))
	require.True(t, invite.IsZero())
	require.True(t, platform.IsZero())
	require.True(t, owner.Add(invite).Add(platform).Equal(total))
}

func TestUsageBillingRepositoryApplyRetriesDeadlockWithFreshTransaction(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageBillingRepository{db: db}
	cmd := newUsageBillingRetryTestCommand()

	mock.ExpectBegin()
	expectUsageBillingClaim(mock, cmd)
	mock.ExpectQuery(`SELECT request_fingerprint\s+FROM usage_billing_dedup_archive`).
		WithArgs(cmd.RequestID, cmd.APIKeyID).
		WillReturnError(&pq.Error{Code: "40P01"})
	mock.ExpectRollback()

	mock.ExpectBegin()
	expectUsageBillingClaimAndArchiveMiss(mock, cmd)
	mock.ExpectCommit()

	result, err := repo.Apply(context.Background(), cmd)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Applied)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageBillingRepositoryApplyRetriesDeadlockFromCommit(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageBillingRepository{db: db}
	cmd := newUsageBillingRetryTestCommand()

	mock.ExpectBegin()
	expectUsageBillingClaimAndArchiveMiss(mock, cmd)
	mock.ExpectCommit().WillReturnError(&pq.Error{Code: "40P01"})

	mock.ExpectBegin()
	expectUsageBillingClaimAndArchiveMiss(mock, cmd)
	mock.ExpectCommit()

	result, err := repo.Apply(context.Background(), cmd)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Applied)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageBillingRepositoryApplyStopsAfterDeadlockRetryLimit(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageBillingRepository{db: db}
	cmd := newUsageBillingRetryTestCommand()
	lastErr := &pq.Error{Code: "40P01", Message: "retry limit"}

	for attempt := 1; attempt <= usageBillingMaxAttempts; attempt++ {
		mock.ExpectBegin()
		expectUsageBillingClaim(mock, cmd)
		deadlockErr := &pq.Error{Code: "40P01"}
		if attempt == usageBillingMaxAttempts {
			deadlockErr = lastErr
		}
		mock.ExpectQuery(`SELECT request_fingerprint\s+FROM usage_billing_dedup_archive`).
			WithArgs(cmd.RequestID, cmd.APIKeyID).
			WillReturnError(deadlockErr)
		mock.ExpectRollback()
	}

	result, err := repo.Apply(context.Background(), cmd)
	require.Nil(t, result)
	require.Same(t, lastErr, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageBillingRepositoryApplyDoesNotRetryOtherErrors(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageBillingRepository{db: db}
	cmd := newUsageBillingRetryTestCommand()
	wantErr := errors.New("query failed")

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO usage_billing_dedup`).
		WithArgs(cmd.RequestID, cmd.APIKeyID, sqlmock.AnyArg()).
		WillReturnError(wantErr)
	mock.ExpectRollback()

	result, err := repo.Apply(context.Background(), cmd)
	require.Nil(t, result)
	require.ErrorIs(t, err, wantErr)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageBillingRepositoryReplayReturnsExistingUsageLogID(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageBillingRepository{db: db}
	cmd := newUsageBillingRetryTestCommand()
	cmd.UsageLog = &service.UsageLog{}
	cmd.Normalize()

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO usage_billing_dedup`).
		WithArgs(cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT request_fingerprint\s+FROM usage_billing_dedup`).
		WithArgs(cmd.RequestID, cmd.APIKeyID).
		WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}).AddRow(cmd.RequestFingerprint))
	mock.ExpectQuery(`SELECT id\s+FROM usage_logs`).
		WithArgs(cmd.RequestID, cmd.APIKeyID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(91)))
	mock.ExpectRollback()

	result, err := repo.Apply(context.Background(), cmd)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Applied)
	require.NotNil(t, result.UsageLogID)
	require.Equal(t, int64(91), *result.UsageLogID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageBillingRepositoryReplayRestoresInviteCreditCacheTargets(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageBillingRepository{db: db}
	ownerUserID := int64(41)
	cmd := newUsageBillingRetryTestCommand()
	cmd.ShareOwnerUserID = &ownerUserID
	cmd.Normalize()

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO usage_billing_dedup`).
		WithArgs(cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT request_fingerprint\s+FROM usage_billing_dedup`).
		WithArgs(cmd.RequestID, cmd.APIKeyID).
		WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}).AddRow(cmd.RequestFingerprint))
	mock.ExpectQuery(`(?s)SELECT credited_invites\.inviter_user_id\s+FROM.*account_share_mode_settlement_entries.*UNION ALL.*account_share_settlement_entries`).
		WithArgs(cmd.RequestID, cmd.APIKeyID).
		WillReturnRows(sqlmock.NewRows([]string{"inviter_user_id"}).
			AddRow(int64(52)).
			AddRow(int64(63)).
			AddRow(int64(52)))
	mock.ExpectRollback()

	result, err := repo.Apply(context.Background(), cmd)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Applied)
	require.Equal(t, []int64{52, 63}, result.BalanceCreditUserIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageBillingRepositoryReplayFailsWhenCreditTargetsCannotBeRestored(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageBillingRepository{db: db}
	cmd := newUsageBillingRetryTestCommand()
	cmd.AccountShareModeSettlement = &service.AccountShareModeBillingSnapshot{MembershipID: 23}
	cmd.Normalize()
	queryErr := errors.New("settlement lookup failed")

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO usage_billing_dedup`).
		WithArgs(cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT request_fingerprint\s+FROM usage_billing_dedup`).
		WithArgs(cmd.RequestID, cmd.APIKeyID).
		WillReturnRows(sqlmock.NewRows([]string{"request_fingerprint"}).AddRow(cmd.RequestFingerprint))
	mock.ExpectQuery(`(?s)SELECT credited_invites\.inviter_user_id\s+FROM.*account_share_mode_settlement_entries.*UNION ALL.*account_share_settlement_entries`).
		WithArgs(cmd.RequestID, cmd.APIKeyID).
		WillReturnError(queryErr)
	mock.ExpectRollback()

	result, err := repo.Apply(context.Background(), cmd)
	require.Nil(t, result)
	require.ErrorIs(t, err, queryErr)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestIsUsageBillingDeadlock(t *testing.T) {
	var typedNil *pq.Error
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "deadlock", err: &pq.Error{Code: "40P01"}, want: true},
		{name: "wrapped deadlock", err: fmt.Errorf("wrapped: %w", &pq.Error{Code: "40P01"}), want: true},
		{name: "serialization failure", err: &pq.Error{Code: "40001"}, want: false},
		{name: "ordinary error", err: errors.New("deadlock detected"), want: false},
		{name: "typed nil", err: typedNil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isUsageBillingDeadlock(tt.err))
		})
	}
}

func TestWaitUsageBillingRetryHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := waitUsageBillingRetry(ctx, time.Second)
	require.ErrorIs(t, err, context.Canceled)
}

func TestAccountShareModeUsageRequestPeriodFallsBackToUsageOccurredAt(t *testing.T) {
	occurredAt := time.Date(2026, 7, 11, 8, 30, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	snapshot := &service.AccountShareModeBillingSnapshot{DurationMs: 1500}
	cmd := &service.UsageBillingCommand{UsageOccurredAt: occurredAt}

	startedAt, endedAt := accountShareModeUsageRequestPeriod(cmd, snapshot)
	require.Equal(t, occurredAt.UTC(), endedAt)
	require.Equal(t, occurredAt.UTC().Add(-1500*time.Millisecond), startedAt)
}

func TestLockAccountShareModeMembershipBeforeWalletUsesDeclaredMembershipAlias(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)

	snapshot := &service.AccountShareModeBillingSnapshot{
		MembershipID:   11,
		ListingID:      12,
		AccountID:      13,
		OwnerUserID:    14,
		ConsumerUserID: 15,
		APIKeyID:       16,
	}
	mock.ExpectQuery(`FROM account_share_memberships m\s+JOIN account_share_listings l ON l\.id = m\.listing_id`).
		WithArgs(snapshot.MembershipID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"listing_id",
			"account_id",
			"owner_user_id",
			"consumer_user_id",
			"api_key_id",
		}).AddRow(
			snapshot.MembershipID,
			snapshot.ListingID,
			snapshot.AccountID,
			snapshot.OwnerUserID,
			snapshot.ConsumerUserID,
			snapshot.APIKeyID,
		))
	mock.ExpectRollback()

	err = lockAccountShareModeMembershipBeforeWallet(context.Background(), tx, &service.UsageBillingCommand{
		AccountShareModeSettlement: snapshot,
	})
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDeductUsageBillingWalletUsesNoKeyUpdateLock(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)

	mock.ExpectQuery(`SELECT balance, points_balance\s+FROM users\s+WHERE id = \$1 AND deleted_at IS NULL\s+FOR NO KEY UPDATE`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "points_balance"}).AddRow(10.0, 5.0))
	mock.ExpectExec(`UPDATE users\s+SET points_balance = \$1::numeric,\s+balance = \$2::numeric`).
		WithArgs("0.0000000000", "8.0000000000", int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	newPoints, newBalance, pointsDeducted, balanceDeducted, sufficient, err := deductUsageBillingWallet(
		context.Background(),
		tx,
		42,
		7,
		true,
	)
	require.NoError(t, err)
	require.InDelta(t, 0, newPoints, 1e-9)
	require.InDelta(t, 8, newBalance, 1e-9)
	require.InDelta(t, 5, pointsDeducted, 1e-9)
	require.InDelta(t, 2, balanceDeducted, 1e-9)
	// 余额 10 足以覆盖积分截断后剩下的 2，不构成透支。
	require.True(t, sufficient)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func newUsageBillingRetryTestCommand() *service.UsageBillingCommand {
	return &service.UsageBillingCommand{
		RequestID: "req-deadlock-retry",
		APIKeyID:  17,
	}
}

func expectUsageBillingClaim(mock sqlmock.Sqlmock, cmd *service.UsageBillingCommand) {
	mock.ExpectQuery(`INSERT INTO usage_billing_dedup`).
		WithArgs(cmd.RequestID, cmd.APIKeyID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
}

func expectUsageBillingClaimAndArchiveMiss(mock sqlmock.Sqlmock, cmd *service.UsageBillingCommand) {
	expectUsageBillingClaim(mock, cmd)
	mock.ExpectQuery(`SELECT request_fingerprint\s+FROM usage_billing_dedup_archive`).
		WithArgs(cmd.RequestID, cmd.APIKeyID).
		WillReturnError(sql.ErrNoRows)
}

// TestDeductUsageBillingBalanceReportsOverdraft 余额不足时必须回报 sufficient=false。
//
// 扣款本身仍然发生（账已经用掉了，钱必须记上），但守卫的意义在于并发：
// 原先无条件 UPDATE 让多个并发请求可以把余额一路扣成负数且无人知晓。
func TestDeductUsageBillingBalanceReportsOverdraft(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)

	// 带 balance >= $1 守卫的 UPDATE 不命中 → 余额不足。
	mock.ExpectQuery(`UPDATE users\s+SET balance = balance - \$1,\s+updated_at = NOW\(\)\s+WHERE id = \$2 AND deleted_at IS NULL AND balance >= \$1`).
		WithArgs(7.0, int64(42)).
		WillReturnError(sql.ErrNoRows)
	// 回落到无条件扣款，余额被扣成负数。
	mock.ExpectQuery(`UPDATE users\s+SET balance = balance - \$1,\s+updated_at = NOW\(\)\s+WHERE id = \$2 AND deleted_at IS NULL\s+RETURNING balance`).
		WithArgs(7.0, int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(-2.0))
	mock.ExpectRollback()

	newBalance, sufficient, err := deductUsageBillingBalance(context.Background(), tx, 42, 7)
	require.NoError(t, err)
	require.False(t, sufficient)
	require.InDelta(t, -2, newBalance, 1e-9)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestDeductUsageBillingWalletReportsOverdraftOnPointsPath
// 双钱包路径下，积分截断后剩余部分超过余额同样要回报透支。
func TestDeductUsageBillingWalletReportsOverdraftOnPointsPath(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)

	// 余额 1、积分 5，本次扣 7：积分出 5，余额需出 2 但只有 1 → 透支。
	mock.ExpectQuery(`SELECT balance, points_balance\s+FROM users\s+WHERE id = \$1 AND deleted_at IS NULL\s+FOR NO KEY UPDATE`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "points_balance"}).AddRow(1.0, 5.0))
	mock.ExpectExec(`UPDATE users\s+SET points_balance = \$1::numeric,\s+balance = \$2::numeric`).
		WithArgs("0.0000000000", "-1.0000000000", int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	_, newBalance, pointsDeducted, balanceDeducted, sufficient, err := deductUsageBillingWallet(
		context.Background(), tx, 42, 7, true,
	)
	require.NoError(t, err)
	require.False(t, sufficient)
	require.InDelta(t, -1, newBalance, 1e-9)
	require.InDelta(t, 5, pointsDeducted, 1e-9)
	require.InDelta(t, 2, balanceDeducted, 1e-9)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}
