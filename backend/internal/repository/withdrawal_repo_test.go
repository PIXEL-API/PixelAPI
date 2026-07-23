package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestWithdrawalListAdminIncludesLastWithdrawalAt(t *testing.T) {
	repo, mock := newWithdrawalRateLimitRepository(t)
	currentTime := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	lastWithdrawalTime := currentTime.Add(-6 * time.Hour)

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM user_withdrawal_requests`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(`(?s)SELECT .*previous\.created_at.*\(previous\.created_at, previous\.id\) < \(user_withdrawal_requests\.created_at, user_withdrawal_requests\.id\).*ORDER BY previous\.created_at DESC, previous\.id DESC.*LIMIT 1.*ORDER BY created_at DESC, id DESC.*LIMIT \$1 OFFSET \$2`).
		WithArgs(20, 0).
		WillReturnRows(sqlmock.NewRows(withdrawalAdminResultColumns()).
			AddRow(
				int64(12), int64(5), "repeat@example.com", 100.0, 0.0, 100.0, 500.0, 400.0,
				"alipay", "oss", "receipt/repeat.png", "https://example.com/repeat.png",
				"image/png", 1024, "repeat-sha256", currentTime.Add(-time.Hour),
				service.WithdrawalStatusPending, nil, nil, nil, nil, currentTime, currentTime,
				lastWithdrawalTime,
			).
			AddRow(
				int64(11), int64(6), "first@example.com", 80.0, 0.1, 80.1, 300.0, 219.9,
				"wechat", "oss", "receipt/first.png", "https://example.com/first.png",
				"image/png", 2048, "first-sha256", currentTime.Add(-2*time.Hour),
				service.WithdrawalStatusPending, nil, nil, nil, nil, currentTime.Add(-time.Minute), currentTime,
				nil,
			))

	items, total, err := repo.ListAdmin(context.Background(), service.WithdrawalListParams{
		Page:     1,
		PageSize: 20,
	})

	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, items, 2)
	require.NotNil(t, items[0].LastWithdrawalAt)
	require.Equal(t, lastWithdrawalTime, *items[0].LastWithdrawalAt)
	require.Nil(t, items[1].LastWithdrawalAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

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

func withdrawalAdminResultColumns() []string {
	return []string{
		"id",
		"user_id",
		"user_email",
		"amount",
		"fee_amount",
		"total_deducted",
		"balance_before",
		"balance_after",
		"payment_method",
		"receipt_code_storage_provider",
		"receipt_code_storage_key",
		"receipt_code_url",
		"receipt_code_content_type",
		"receipt_code_byte_size",
		"receipt_code_sha256",
		"receipt_code_updated_at",
		"status",
		"user_cancel_reason",
		"admin_note",
		"processed_by_user_id",
		"processed_at",
		"created_at",
		"updated_at",
		"last_withdrawal_at",
	}
}
