package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSettleUsageBillingQuotaReservation(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	tx, err := db.Begin()
	require.NoError(t, err)

	mock.ExpectExec(`UPDATE quota_reservations\s+SET actual_cost = \$2,\s+status = 'settled',\s+settled_at = NOW\(\),\s+updated_at = NOW\(\)\s+WHERE reservation_id = \$1\s+AND status = 'reserved'`).
		WithArgs("qres_1", 1.75).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	err = settleUsageBillingQuotaReservation(context.Background(), tx, "qres_1", 1.75)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSettleUsageBillingQuotaReservationNotFound(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	tx, err := db.Begin()
	require.NoError(t, err)

	mock.ExpectExec(`UPDATE quota_reservations\s+SET actual_cost = \$2,\s+status = 'settled',\s+settled_at = NOW\(\),\s+updated_at = NOW\(\)\s+WHERE reservation_id = \$1\s+AND status = 'reserved'`).
		WithArgs("qres_missing", 0.5).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err = settleUsageBillingQuotaReservation(context.Background(), tx, "qres_missing", 0.5)
	require.ErrorIs(t, err, service.ErrQuotaReservationNotFound)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}
