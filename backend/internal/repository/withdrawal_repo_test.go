package repository

import (
	"context"
	"errors"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestWithdrawalSubmitRateLimitIncludesThresholdAmount(t *testing.T) {
	repo, mock := newWithdrawalRateLimitRepository(t)
	expectWithdrawalSubmitPreamble(mock, 1)
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*amount <= \$3::numeric`).
		WithArgs(int64(1), 7, 500.0).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	mock.ExpectRollback()

	result, err := repo.Submit(context.Background(), service.WithdrawalSubmitInput{
		UserID:        1,
		Amount:        500,
		PaymentMethod: "alipay",
		RateLimit: service.WithdrawalRateLimitConfig{
			WindowDays:   7,
			MaxRequests:  3,
			ExemptAmount: 500,
		},
	})

	require.Nil(t, result)
	require.Equal(t, "WITHDRAWAL_RATE_LIMIT_EXCEEDED", infraerrors.Reason(err))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestWithdrawalSubmitSkipsRateLimitAboveThreshold(t *testing.T) {
	repo, mock := newWithdrawalRateLimitRepository(t)
	expectWithdrawalSubmitPreamble(mock, 1)
	nextQueryErr := errors.New("reached post-rate-limit query")
	mock.ExpectQuery(`(?s)SELECT EXISTS \(.*WHERE user_id = \$1.*\)`).
		WithArgs(int64(1)).
		WillReturnError(nextQueryErr)
	mock.ExpectRollback()

	result, err := repo.Submit(context.Background(), service.WithdrawalSubmitInput{
		UserID:        1,
		Amount:        500.01,
		PaymentMethod: "alipay",
		RateLimit: service.WithdrawalRateLimitConfig{
			WindowDays:   7,
			MaxRequests:  3,
			ExemptAmount: 500,
		},
	})

	require.Nil(t, result)
	require.ErrorIs(t, err, nextQueryErr)
	require.NoError(t, mock.ExpectationsWereMet())
}

func newWithdrawalRateLimitRepository(t *testing.T) (*withdrawalRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
	})
	return &withdrawalRepository{db: db}, mock
}

func expectWithdrawalSubmitPreamble(mock sqlmock.Sqlmock, userID int64) {
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT email, balance::double precision.*FOR UPDATE`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"email", "balance"}).AddRow("user@example.com", 1000.0))
	mock.ExpectQuery(`(?s)SELECT EXISTS \(.*status = \$2.*\)`).
		WithArgs(userID, service.WithdrawalStatusPending).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
}
