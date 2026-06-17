package repository

import (
	"context"
	"regexp"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func TestUserRepositoryApplyBalanceLedgerDeltaWritesLedger(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	drv := entsql.OpenDB(dialect.Postgres, db)
	client := dbent.NewClient(dbent.Driver(drv))
	defer func() { _ = client.Close() }()

	const userID int64 = 7
	refID := int64(42)
	repo := newUserRepositoryWithSQL(client, db)

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT\s+balance\s+FROM users\s+WHERE id = \$1 AND deleted_at IS NULL\s+FOR UPDATE`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(10.0))
	mock.ExpectExec(regexp.QuoteMeta(`
		UPDATE users
		SET balance = $1::numeric,
			total_recharged = CASE
				WHEN $2::boolean AND $3::numeric > 0 THEN COALESCE(total_recharged, 0) + $3::numeric
				ELSE total_recharged
			END,
			updated_at = NOW()
		WHERE id = $4 AND deleted_at IS NULL
	`)).
		WithArgs("15.5000000000", true, "5.5000000000", userID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO user_balance_ledger (
			user_id, direction, amount, reason, ref_type, ref_id, balance_after, metadata
		) VALUES (
			$1, $2, $3::numeric, $4, $5, $6, $7::numeric, $8::jsonb
		)
		ON CONFLICT DO NOTHING
	`)).
		WithArgs(userID, "credit", "5.5000000000", service.UserBalanceLedgerReasonRedeemCode, "redeem_code", refID, "15.5000000000", `{"code":"ABC"}`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := repo.ApplyBalanceLedgerDelta(context.Background(), service.UserBalanceLedgerDeltaInput{
		UserID:              userID,
		Delta:               5.5,
		Reason:              service.UserBalanceLedgerReasonRedeemCode,
		RefType:             "redeem_code",
		RefID:               &refID,
		Metadata:            map[string]any{"code": "ABC"},
		TrackTotalRecharged: true,
	})

	require.NoError(t, err)
	require.Equal(t, 10.0, result.BalanceBefore)
	require.Equal(t, 15.5, result.BalanceAfter)
	require.Equal(t, 5.5, result.Delta)
	require.NoError(t, mock.ExpectationsWereMet())
}
