package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestQuotaReservationRepositoryCreateRejectsLeaseGroupMismatch(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	repo := &quotaReservationRepository{db: db}
	groupID := int64(7)
	reservation := &service.QuotaReservation{
		ReservationID:      "qres_1",
		RequestID:          "subreq_1",
		SubsiteID:          "site_1",
		LeaseID:            "lease_1",
		AccountID:          100,
		APIKeyID:           200,
		UserID:             300,
		GroupID:            &groupID,
		Platform:           service.PlatformOpenAI,
		RequestedModel:     "gpt-5.4",
		MappedModel:        "gpt-5.4",
		EstimatedCost:      0.01,
		ReservedRequests:   1,
		ReservedTokens:     128,
		ActiveRequestUnits: 1,
		BillingType:        service.BillingTypeBalance,
		Status:             service.QuotaReservationStatusReserved,
		RequestFingerprint: "fp_1",
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT al\.max_concurrency,\s+COALESCE\(a\.concurrency, 0\) AS account_concurrency,\s+al\.max_requests,\s+al\.max_tokens,\s+al\.used_requests,\s+al\.used_tokens\s+FROM account_leases al\s+JOIN accounts a ON a\.id = al\.account_id`).
		WithArgs(reservation.LeaseID, reservation.SubsiteID, reservation.AccountID, reservation.GroupID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	err = repo.Create(context.Background(), reservation)
	require.ErrorIs(t, err, service.ErrAccountLeaseNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestQuotaReservationRepositoryCreate_UsesAccountConcurrencyWhenLeaseConcurrencyZero(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	repo := &quotaReservationRepository{db: db}
	groupID := int64(7)
	reservation := &service.QuotaReservation{
		ReservationID:      "qres_1",
		RequestID:          "subreq_1",
		SubsiteID:          "site_1",
		LeaseID:            "lease_1",
		AccountID:          100,
		APIKeyID:           200,
		UserID:             300,
		GroupID:            &groupID,
		Platform:           service.PlatformOpenAI,
		RequestedModel:     "gpt-5.4",
		MappedModel:        "gpt-5.4",
		EstimatedCost:      0.01,
		ReservedRequests:   1,
		ReservedTokens:     128,
		ActiveRequestUnits: 1,
		BillingType:        service.BillingTypeBalance,
		Status:             service.QuotaReservationStatusReserved,
		RequestFingerprint: "fp_1",
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT al\.max_concurrency,\s+COALESCE\(a\.concurrency, 0\) AS account_concurrency,\s+al\.max_requests,\s+al\.max_tokens,\s+al\.used_requests,\s+al\.used_tokens\s+FROM account_leases al\s+JOIN accounts a ON a\.id = al\.account_id`).
		WithArgs(reservation.LeaseID, reservation.SubsiteID, reservation.AccountID, reservation.GroupID).
		WillReturnRows(sqlmock.NewRows([]string{
			"max_concurrency", "account_concurrency", "max_requests", "max_tokens", "used_requests", "used_tokens",
		}).AddRow(0, 2, 0, 0, 0, 0))
	mock.ExpectQuery(`SELECT COALESCE\(SUM\(reserved_requests\), 0\),\s+COALESCE\(SUM\(reserved_tokens\), 0\),\s+COALESCE\(SUM\(active_request_units\), 0\)\s+FROM quota_reservations`).
		WithArgs(reservation.LeaseID).
		WillReturnRows(sqlmock.NewRows([]string{"reserved_requests", "reserved_tokens", "active_request_units"}).AddRow(0, 0, 2))
	mock.ExpectRollback()

	err = repo.Create(context.Background(), reservation)
	require.ErrorIs(t, err, service.ErrSubsiteLeaseCapacityExceeded)
	require.NoError(t, mock.ExpectationsWereMet())
}
