package repository

import (
	"context"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountShareBillingAdminRepositoryListsOnlyNeedsAttention(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &accountShareBillingIntentRepository{db: db}

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\)::bigint\s+FROM account_share_request_billing_intents\s+WHERE status = 'needs_attention'`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectQuery(`(?s)FROM account_share_request_billing_intents AS intent\s+WHERE intent\.status = 'needs_attention'\s+ORDER BY intent\.updated_at DESC, intent\.id DESC\s+OFFSET \$1\s+LIMIT \$2`).
		WithArgs(0, 20).
		WillReturnRows(billingIntentAdminRows(
			service.AccountShareBillingIntentStatusNeedsAttention,
			4,
		))

	items, total, err := repo.ListNeedsAttentionForAdmin(context.Background(), 0, 20)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	require.Equal(t, service.AccountShareBillingIntentStatusNeedsAttention, items[0].Status)
	require.Equal(t, int64(4), items[0].StateToken)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountShareBillingAdminRepositoryGetsNonSensitiveDetailByID(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &accountShareBillingIntentRepository{db: db}

	mock.ExpectQuery(`(?s)FROM account_share_request_billing_intents AS intent\s+WHERE intent\.id = \$1`).
		WithArgs(int64(100)).
		WillReturnRows(billingIntentAdminRows(
			service.AccountShareBillingIntentStatusNeedsAttention,
			4,
		))

	record, err := repo.GetForAdmin(context.Background(), 100)
	require.NoError(t, err)
	require.Equal(t, int64(100), record.ID)
	require.Equal(t, int64(12), record.ListingID)
	require.Equal(t, int64(11), record.MembershipID)
	require.Equal(t, int64(4), record.StateToken)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountShareBillingAdminRepositoryWaiveUsesAuditAndCASWithoutBillingWrites(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &accountShareBillingIntentRepository{db: db}
	input := service.WaiveAccountShareBillingIntentRepositoryInput{
		IntentID:           100,
		ExpectedStateToken: 4,
		ActorUserID:        42,
		Reason:             "人工确认无法恢复，执行免单",
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)FROM account_share_request_billing_intents AS intent\s+WHERE intent\.id = \$1\s+FOR UPDATE`).
		WithArgs(input.IntentID).
		WillReturnRows(billingIntentAdminRows(
			service.AccountShareBillingIntentStatusNeedsAttention,
			input.ExpectedStateToken,
		))
	mock.ExpectQuery(`(?s)INSERT INTO account_share_billing_intent_admin_waivers AS waiver.*SELECT.*intent\.status,\s+'cancelled',\s+intent\.state_token,\s+intent\.state_token \+ 1.*WHERE intent\.id = \$1\s+AND intent\.status = 'needs_attention'\s+AND intent\.state_token = \$4.*RETURNING`).
		WithArgs(input.IntentID, input.ActorUserID, input.Reason, input.ExpectedStateToken).
		WillReturnRows(billingIntentAdminWaiverRows(input))
	mock.ExpectQuery(`(?s)UPDATE account_share_request_billing_intents AS intent\s+SET status = 'cancelled',\s+state_token = intent\.state_token \+ 1,\s+admin_waiver_audit_id = \$2.*WHERE intent\.id = \$1\s+AND intent\.status = 'needs_attention'\s+AND intent\.state_token = \$3.*RETURNING`).
		WithArgs(input.IntentID, int64(200), input.ExpectedStateToken).
		WillReturnRows(billingIntentAdminRows(
			service.AccountShareBillingIntentStatusCancelled,
			input.ExpectedStateToken+1,
		))
	mock.ExpectCommit()

	result, err := repo.WaiveNeedsAttention(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, service.AccountShareBillingIntentStatusCancelled, result.Intent.Status)
	require.Equal(t, int64(5), result.Intent.StateToken)
	require.Equal(t, int64(200), result.Waiver.ID)
	require.Equal(t, input.Reason, result.Waiver.Reason)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountShareBillingAdminRepositoryIdenticalReplayIsIdempotent(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &accountShareBillingIntentRepository{db: db}
	input := service.WaiveAccountShareBillingIntentRepositoryInput{
		IntentID:           100,
		ExpectedStateToken: 4,
		ActorUserID:        42,
		Reason:             "人工确认无法恢复，执行免单",
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)FROM account_share_request_billing_intents AS intent\s+WHERE intent\.id = \$1\s+FOR UPDATE`).
		WithArgs(input.IntentID).
		WillReturnRows(billingIntentAdminRows(
			service.AccountShareBillingIntentStatusCancelled,
			input.ExpectedStateToken+1,
		))
	mock.ExpectQuery(`(?s)FROM account_share_billing_intent_admin_waivers AS waiver\s+WHERE waiver\.intent_id = \$1`).
		WithArgs(input.IntentID).
		WillReturnRows(billingIntentAdminWaiverRows(input))
	mock.ExpectCommit()

	result, err := repo.WaiveNeedsAttention(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, int64(200), result.Waiver.ID)
	require.Equal(t, int64(5), result.Intent.StateToken)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountShareBillingAdminRepositoryRejectsStaleStateTokenBeforeAuditInsert(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &accountShareBillingIntentRepository{db: db}
	input := service.WaiveAccountShareBillingIntentRepositoryInput{
		IntentID:           100,
		ExpectedStateToken: 4,
		ActorUserID:        42,
		Reason:             "人工确认无法恢复，执行免单",
	}

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)FROM account_share_request_billing_intents AS intent\s+WHERE intent\.id = \$1\s+FOR UPDATE`).
		WithArgs(input.IntentID).
		WillReturnRows(billingIntentAdminRows(
			service.AccountShareBillingIntentStatusNeedsAttention,
			input.ExpectedStateToken+1,
		))
	mock.ExpectRollback()

	result, err := repo.WaiveNeedsAttention(context.Background(), input)
	require.Nil(t, result)
	require.ErrorIs(t, err, service.ErrAccountShareBillingIntentStateConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountShareBillingAdminRepositoryMismatchedReplayConflicts(t *testing.T) {
	storedInput := service.WaiveAccountShareBillingIntentRepositoryInput{
		IntentID:           100,
		ExpectedStateToken: 4,
		ActorUserID:        42,
		Reason:             "原始原因",
	}
	tests := []struct {
		name  string
		input service.WaiveAccountShareBillingIntentRepositoryInput
	}{
		{
			name: "different reason",
			input: service.WaiveAccountShareBillingIntentRepositoryInput{
				IntentID:           100,
				ExpectedStateToken: 4,
				ActorUserID:        42,
				Reason:             "不同原因",
			},
		},
		{
			name: "different actor",
			input: service.WaiveAccountShareBillingIntentRepositoryInput{
				IntentID:           100,
				ExpectedStateToken: 4,
				ActorUserID:        43,
				Reason:             "原始原因",
			},
		},
		{
			name: "different token",
			input: service.WaiveAccountShareBillingIntentRepositoryInput{
				IntentID:           100,
				ExpectedStateToken: 5,
				ActorUserID:        42,
				Reason:             "原始原因",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			repo := &accountShareBillingIntentRepository{db: db}

			mock.ExpectBegin()
			mock.ExpectQuery(`(?s)FROM account_share_request_billing_intents AS intent\s+WHERE intent\.id = \$1\s+FOR UPDATE`).
				WithArgs(tt.input.IntentID).
				WillReturnRows(billingIntentAdminRows(
					service.AccountShareBillingIntentStatusCancelled,
					storedInput.ExpectedStateToken+1,
				))
			mock.ExpectQuery(`(?s)FROM account_share_billing_intent_admin_waivers AS waiver\s+WHERE waiver\.intent_id = \$1`).
				WithArgs(tt.input.IntentID).
				WillReturnRows(billingIntentAdminWaiverRows(storedInput))
			mock.ExpectRollback()

			result, err := repo.WaiveNeedsAttention(context.Background(), tt.input)
			require.Nil(t, result)
			require.ErrorIs(t, err, service.ErrAccountShareBillingIntentStateConflict)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func billingIntentAdminColumnNames() []string {
	return []string{
		"id",
		"request_id",
		"dispatch_id",
		"attempt_no",
		"api_key_id_snapshot",
		"membership_id",
		"listing_id",
		"account_id_snapshot",
		"status",
		"state_token",
		"last_error_code",
		"last_error_message",
		"forward_started_at",
		"completed_at",
		"created_at",
		"updated_at",
	}
}

func billingIntentAdminRows(status string, stateToken int64) *sqlmock.Rows {
	now := time.Date(2026, 7, 27, 6, 0, 0, 0, time.UTC)
	return sqlmock.NewRows(billingIntentAdminColumnNames()).AddRow(
		int64(100),
		"request-100",
		"6ea3aa0c-5f11-4af3-a0f8-c227d77eaf20",
		1,
		int64(10),
		int64(11),
		int64(12),
		int64(13),
		status,
		stateToken,
		"runtime_lease_expired_without_usage",
		"runtime lease expired",
		now.Add(-time.Hour),
		nil,
		now.Add(-2*time.Hour),
		now,
	)
}

func billingIntentAdminWaiverColumnNames() []string {
	return []string{
		"id",
		"intent_id",
		"listing_id",
		"membership_id",
		"actor_user_id_snapshot",
		"reason",
		"action",
		"previous_status",
		"resulting_status",
		"previous_state_token",
		"resulting_state_token",
		"created_at",
	}
}

func billingIntentAdminWaiverRows(
	input service.WaiveAccountShareBillingIntentRepositoryInput,
) *sqlmock.Rows {
	return sqlmock.NewRows(billingIntentAdminWaiverColumnNames()).AddRow(
		int64(200),
		input.IntentID,
		int64(12),
		int64(11),
		input.ActorUserID,
		input.Reason,
		"waive",
		service.AccountShareBillingIntentStatusNeedsAttention,
		service.AccountShareBillingIntentStatusCancelled,
		input.ExpectedStateToken,
		input.ExpectedStateToken+1,
		time.Date(2026, 7, 27, 6, 1, 0, 0, time.UTC),
	)
}
