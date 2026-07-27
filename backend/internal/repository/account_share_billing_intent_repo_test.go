package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

const accountShareBillingIntentCreateLockQueryPattern = `(?s)WITH locked_listing AS MATERIALIZED.*FROM account_share_listings listing.*FOR UPDATE OF listing.*locked_membership AS MATERIALIZED.*JOIN locked_listing listing.*FOR UPDATE OF membership.*SELECT binding\.id.*JOIN locked_membership membership.*FOR UPDATE OF binding`

func TestAccountShareBillingIntentRepositoryCreatePrepared(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &accountShareBillingIntentRepository{db: db}
	input := billingIntentRepositoryTestInput()
	prepared, err := service.PrepareAccountShareBillingIntent(input)
	require.NoError(t, err)

	mock.ExpectBegin()
	mock.ExpectQuery(accountShareBillingIntentCreateLockQueryPattern).
		WithArgs(
			prepared.ListingID,
			prepared.MembershipID,
			prepared.APIKeyID,
			prepared.ConsumerUserID,
			prepared.AccountID,
			prepared.ListingRevisionID,
			prepared.BindingID,
			prepared.TermsRevisionNumber,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(prepared.BindingID))
	mock.ExpectQuery(`(?s)INSERT INTO account_share_request_billing_intents.*ON CONFLICT \(dispatch_id\) DO NOTHING.*RETURNING`).
		WithArgs(
			prepared.RequestID,
			prepared.ClientRequestID,
			prepared.DispatchID,
			prepared.AttemptNo,
			prepared.APIKeyID,
			prepared.MembershipID,
			prepared.ListingID,
			prepared.AccountID,
			prepared.BindingID,
			prepared.ListingRevisionID,
			prepared.TermsRevisionNumber,
			prepared.ActorUserID,
			prepared.ActorRole,
			prepared.ConsumerUserID,
			prepared.OwnerUserID,
			prepared.Command.RequestedModel,
			prepared.Command.RoutedModel,
			prepared.Command.RateMultiplier,
			prepared.Command.OwnerShareRatio,
			prepared.Command.InviteShareRatio,
			prepared.Command.PlatformShareRatio,
			prepared.Command.SchemaVersion,
			string(prepared.CommandJSON),
			prepared.CommandHash,
			prepared.RequestFingerprint,
		).
		WillReturnRows(billingIntentStateRows(service.AccountShareBillingIntentStatusCreated, 1, 0, 0, "", nil))
	mock.ExpectCommit()

	state, created, err := repo.CreatePrepared(context.Background(), input)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, service.AccountShareBillingIntentStatusCreated, state.Status)
	require.Equal(t, int64(1), state.StateToken)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountShareBillingIntentRepositoryCreatePreparedRejectsLifecycleFence(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &accountShareBillingIntentRepository{db: db}
	input := billingIntentRepositoryTestInput()
	prepared, err := service.PrepareAccountShareBillingIntent(input)
	require.NoError(t, err)

	mock.ExpectBegin()
	mock.ExpectQuery(accountShareBillingIntentCreateLockQueryPattern).
		WithArgs(
			prepared.ListingID,
			prepared.MembershipID,
			prepared.APIKeyID,
			prepared.ConsumerUserID,
			prepared.AccountID,
			prepared.ListingRevisionID,
			prepared.BindingID,
			prepared.TermsRevisionNumber,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()

	state, created, err := repo.CreatePrepared(context.Background(), input)
	require.Nil(t, state)
	require.False(t, created)
	require.ErrorIs(t, err, service.ErrAccountShareBillingBindingUnavailable)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountShareBillingIntentRepositoryCreatePreparedConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &accountShareBillingIntentRepository{db: db}
	input := billingIntentRepositoryTestInput()
	prepared, err := service.PrepareAccountShareBillingIntent(input)
	require.NoError(t, err)

	mock.ExpectBegin()
	mock.ExpectQuery(accountShareBillingIntentCreateLockQueryPattern).
		WithArgs(
			prepared.ListingID,
			prepared.MembershipID,
			prepared.APIKeyID,
			prepared.ConsumerUserID,
			prepared.AccountID,
			prepared.ListingRevisionID,
			prepared.BindingID,
			prepared.TermsRevisionNumber,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(prepared.BindingID))
	mock.ExpectQuery(`(?s)INSERT INTO account_share_request_billing_intents.*ON CONFLICT`).
		WillReturnRows(sqlmock.NewRows(billingIntentStateColumnNames()))
	mock.ExpectQuery(`(?s)SELECT.*request_fingerprint.*FROM account_share_request_billing_intents`).
		WithArgs(prepared.DispatchID).
		WillReturnRows(billingIntentStateRowsWithFingerprint(
			service.AccountShareBillingIntentStatusCreated,
			1,
			"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		))
	mock.ExpectRollback()

	state, created, err := repo.CreatePrepared(context.Background(), input)
	require.Nil(t, state)
	require.False(t, created)
	require.ErrorIs(t, err, service.ErrAccountShareBillingIntentConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountShareBillingIntentRepositoryMarkReadyUsesStateTokenCAS(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &accountShareBillingIntentRepository{db: db}
	input := billingIntentRepositoryReadyInput()
	prepared, err := service.PrepareAccountShareBillingIntentReady(input)
	require.NoError(t, err)

	mock.ExpectQuery(`(?s)UPDATE account_share_request_billing_intents.*state_token = state_token \+ 1.*AND state_token = \$2.*AND status = 'in_flight'.*RETURNING`).
		WithArgs(
			input.ID,
			input.ExpectedStateToken,
			service.AccountShareBillingUsageSchemaV2,
			string(prepared.UsageJSON),
			prepared.UsageHash,
			string(prepared.ResponseSummaryJSON),
		).
		WillReturnRows(billingIntentStateRows(service.AccountShareBillingIntentStatusReady, 3, 0, 0, "", nil))

	state, err := repo.MarkReady(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, service.AccountShareBillingIntentStatusReady, state.Status)
	require.Equal(t, int64(3), state.StateToken)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountShareBillingIntentRepositoryClaimReadyUsesSkipLockedAndFencingTokens(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &accountShareBillingIntentRepository{db: db}
	createPrepared, err := service.PrepareAccountShareBillingIntent(billingIntentRepositoryTestInput())
	require.NoError(t, err)
	readyPrepared, err := service.PrepareAccountShareBillingIntentReady(billingIntentRepositoryReadyInput())
	require.NoError(t, err)
	now := time.Date(2026, 7, 27, 2, 0, 0, 0, time.UTC)
	leaseExpiresAt := now.Add(30 * time.Second)

	mock.ExpectQuery(`(?s)FOR UPDATE SKIP LOCKED.*state_token = intent\.state_token \+ 1.*lease_token = intent\.lease_token \+ 1.*RETURNING`).
		WithArgs(5, "worker-a", int64(30000)).
		WillReturnRows(sqlmock.NewRows(billingIntentWorkColumnNames()).AddRow(
			int64(100),
			createPrepared.RequestID,
			createPrepared.ClientRequestID,
			createPrepared.DispatchID,
			createPrepared.AttemptNo,
			createPrepared.APIKeyID,
			createPrepared.MembershipID,
			service.AccountShareBillingIntentStatusProcessing,
			int64(4),
			1,
			int64(1),
			"worker-a",
			leaseExpiresAt,
			now,
			now,
			createPrepared.ListingID,
			createPrepared.AccountID,
			createPrepared.BindingID,
			createPrepared.ListingRevisionID,
			createPrepared.TermsRevisionNumber,
			createPrepared.ActorUserID,
			createPrepared.ActorRole,
			createPrepared.ConsumerUserID,
			createPrepared.OwnerUserID,
			string(createPrepared.CommandJSON),
			createPrepared.CommandHash,
			createPrepared.RequestFingerprint,
			string(readyPrepared.UsageJSON),
			readyPrepared.UsageHash,
			string(readyPrepared.ResponseSummaryJSON),
		))

	items, err := repo.ClaimReady(context.Background(), service.ClaimAccountShareBillingIntentsInput{
		WorkerID:      "worker-a",
		Limit:         5,
		LeaseDuration: 30 * time.Second,
	})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, int64(1), items[0].LeaseToken)
	require.Equal(t, "gpt-5.6", items[0].Command.RoutedModel)
	require.Equal(t, int64(50), items[0].Usage.OutputTokens)
	require.Equal(t, 200, items[0].ResponseSummary.HTTPStatus)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountShareBillingIntentRepositoryMarkSettledRejectsStaleWorker(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &accountShareBillingIntentRepository{db: db}
	input := service.MarkAccountShareBillingIntentSettledInput{
		AccountShareBillingIntentLeaseTransition: service.AccountShareBillingIntentLeaseTransition{
			ID:                 100,
			ExpectedStateToken: 4,
			LeaseToken:         2,
			WorkerID:           "old-worker",
		},
	}

	mock.ExpectQuery(`(?s)UPDATE account_share_request_billing_intents.*AND lease_token = \$3.*AND lease_owner = \$4.*AND lease_expires_at > clock_timestamp\(\).*RETURNING`).
		WithArgs(input.ID, input.ExpectedStateToken, input.LeaseToken, input.WorkerID, nil).
		WillReturnRows(sqlmock.NewRows(billingIntentStateColumnNames()))
	mock.ExpectQuery(`(?s)SELECT EXISTS.*account_share_request_billing_intents`).
		WithArgs(input.ID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	state, err := repo.MarkSettled(context.Background(), input)
	require.Nil(t, state)
	require.ErrorIs(t, err, service.ErrAccountShareBillingIntentLeaseLost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountShareBillingIntentRepositoryCountPendingIncludesAttentionStates(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &accountShareBillingIntentRepository{db: db}

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\)::bigint.*membership_id = \$1.*status NOT IN \('settled', 'cancelled'\)`).
		WithArgs(int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(3)))

	count, err := repo.CountPendingByMembership(context.Background(), 11)
	require.NoError(t, err)
	require.Equal(t, int64(3), count)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountShareBillingIntentRepositoryListStaleForAttention(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &accountShareBillingIntentRepository{db: db}
	staleBefore := time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC)
	now := staleBefore.Add(-time.Hour)

	mock.ExpectQuery(`(?s)'forward_runtime_lease_expired'.*WHERE status = 'in_flight'\s+AND updated_at <= \$1\s+ORDER BY`).
		WithArgs(staleBefore, 10).
		WillReturnRows(sqlmock.NewRows(billingIntentAttentionColumnNames()).AddRow(
			int64(100),
			"req-stale",
			"client-req-stale",
			"6ea3aa0c-5f11-4af3-a0f8-c227d77eaf20",
			2,
			int64(10),
			int64(11),
			service.AccountShareBillingIntentStatusInFlight,
			int64(4),
			1,
			int64(1),
			"",
			nil,
			now,
			now,
			"forward_runtime_lease_expired",
			"",
			"",
			now,
			nil,
			nil,
		))

	items, err := repo.ListStaleForAttention(context.Background(), staleBefore, 10)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "forward_runtime_lease_expired", items[0].ReasonCode)
	require.Nil(t, items[0].LeaseExpiresAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountShareBillingIntentRepositoryEscalateStaleRequiresCutoffAndStateToken(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &accountShareBillingIntentRepository{db: db}
	staleBefore := time.Date(2026, 7, 27, 1, 0, 0, 0, time.UTC)
	input := service.EscalateAccountShareBillingIntentInput{
		AccountShareBillingIntentTransition: service.AccountShareBillingIntentTransition{
			ID:                 100,
			ExpectedStateToken: 4,
		},
		ReasonCode:    "runtime_lease_expired_without_usage",
		ReasonMessage: "runtime lease expired",
		StaleBefore:   staleBefore,
	}

	mock.ExpectQuery(`(?s)UPDATE account_share_request_billing_intents.*SET status = 'needs_attention'.*WHERE id = \$1\s+AND state_token = \$2\s+AND status = 'in_flight'\s+AND updated_at <= \$5`).
		WithArgs(
			input.ID,
			input.ExpectedStateToken,
			input.ReasonCode,
			input.ReasonMessage,
			staleBefore,
		).
		WillReturnRows(billingIntentStateRows(
			service.AccountShareBillingIntentStatusNeedsAttention,
			5,
			0,
			0,
			"",
			nil,
		))

	state, err := repo.EscalateStaleToNeedsAttention(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, service.AccountShareBillingIntentStatusNeedsAttention, state.Status)
	require.Equal(t, int64(5), state.StateToken)
	require.NoError(t, mock.ExpectationsWereMet())
}

func billingIntentRepositoryTestInput() service.CreateAccountShareBillingIntentInput {
	groupID := int64(90)
	policyID := int64(91)
	return service.CreateAccountShareBillingIntentInput{
		RequestID:           "req-account-share-repo-1",
		ClientRequestID:     "client-account-share-repo-1",
		DispatchID:          "3f2f4875-c1af-4b68-80a2-0df8f1fa5fcb",
		AttemptNo:           1,
		APIKeyID:            10,
		MembershipID:        11,
		ListingID:           12,
		AccountID:           13,
		BindingID:           14,
		ListingRevisionID:   15,
		TermsRevisionNumber: 2,
		ActorUserID:         16,
		ActorRole:           "consumer",
		ConsumerUserID:      16,
		OwnerUserID:         17,
		Command: service.AccountShareBillingCommand{
			SchemaVersion:         service.AccountShareBillingCommandSchemaV3,
			RequestPayloadHash:    strings.Repeat("b", 64),
			GroupID:               &groupID,
			AccountType:           "openai",
			RequestedModel:        "gpt-5.6",
			RoutedModel:           "gpt-5.6",
			InboundEndpoint:       "/v1/responses",
			UpstreamEndpoint:      "/v1/responses",
			RequestType:           "stream",
			ServiceTier:           "priority",
			ReasoningEffort:       "high",
			BillingType:           int16(service.BillingTypeBalance),
			PreferPointsBilling:   true,
			RateMultiplier:        "1",
			RateMultiplierSource:  service.RateMultiplierSourceAccountShare,
			AccountRateMultiplier: "1",
			HourlyRate:            "0.5",
			OwnerShareRatio:       "0.7",
			InviteShareRatio:      "0.1",
			PlatformShareRatio:    "0.2",
			PolicyID:              &policyID,
			PolicyVersion:         3,
			ModelMappingChain:     "gpt-5.6",
			SettlementEnabled:     true,
			ShareModeSnapshot:     service.AccountShareModePrivate,
			ShareStatusSnapshot:   service.AccountShareStatusApproved,
			SharePlatformSnapshot: service.PlatformOpenAI,
		},
	}
}

func billingIntentRepositoryReadyInput() service.MarkAccountShareBillingIntentReadyInput {
	return service.MarkAccountShareBillingIntentReadyInput{
		AccountShareBillingIntentTransition: service.AccountShareBillingIntentTransition{
			ID:                 100,
			ExpectedStateToken: 2,
		},
		Usage: service.AccountShareBillingUsagePayloadV2{
			SchemaVersion:          service.AccountShareBillingUsageSchemaV2,
			UsageOccurredAt:        time.Date(2026, 7, 27, 1, 2, 3, 0, time.UTC),
			Model:                  "gpt-5.6",
			InputTokens:            100,
			OutputTokens:           50,
			DurationMilliseconds:   1000,
			FirstTokenMilliseconds: billingIntentInt64Pointer(100),
			AppliedRateMultiplier:  "1",
			InputCost:              "0.15",
			OutputCost:             "0.05",
			TotalCost:              "0.2",
			ActualCost:             "0.2",
			BalanceCost:            "0.2",
			BaseCharge:             "0.2",
			TotalCharge:            "0.2",
		},
		ResponseSummary: service.AccountShareBillingResponseSummaryV1{
			SchemaVersion:     service.AccountShareBillingResponseSchemaV1,
			HTTPStatus:        200,
			ProviderRequestID: "upstream-1",
			FinishReason:      "stop",
			Streamed:          true,
		},
	}
}

func billingIntentInt64Pointer(value int64) *int64 {
	return &value
}

func billingIntentStateColumnNames() []string {
	return []string{
		"id",
		"request_id",
		"client_request_id",
		"dispatch_id",
		"attempt_no",
		"api_key_id_snapshot",
		"membership_id",
		"status",
		"state_token",
		"attempt_count",
		"lease_token",
		"lease_owner",
		"lease_expires_at",
		"created_at",
		"updated_at",
	}
}

func billingIntentStateRows(status string, stateToken int64, attemptCount int, leaseToken int64, leaseOwner string, leaseExpiresAt any) *sqlmock.Rows {
	now := time.Date(2026, 7, 27, 2, 0, 0, 0, time.UTC)
	return sqlmock.NewRows(billingIntentStateColumnNames()).AddRow(
		int64(100),
		"req-account-share-repo-1",
		"client-account-share-repo-1",
		"3f2f4875-c1af-4b68-80a2-0df8f1fa5fcb",
		1,
		int64(10),
		int64(11),
		status,
		stateToken,
		attemptCount,
		leaseToken,
		leaseOwner,
		leaseExpiresAt,
		now,
		now,
	)
}

func billingIntentStateRowsWithFingerprint(status string, stateToken int64, fingerprint string) *sqlmock.Rows {
	columns := append(billingIntentStateColumnNames(), "request_fingerprint")
	now := time.Date(2026, 7, 27, 2, 0, 0, 0, time.UTC)
	return sqlmock.NewRows(columns).AddRow(
		int64(100),
		"req-account-share-repo-1",
		"client-account-share-repo-1",
		"3f2f4875-c1af-4b68-80a2-0df8f1fa5fcb",
		1,
		int64(10),
		int64(11),
		status,
		stateToken,
		0,
		int64(0),
		"",
		nil,
		now,
		now,
		fingerprint,
	)
}

func billingIntentWorkColumnNames() []string {
	return append(billingIntentStateColumnNames(),
		"listing_id",
		"account_id_snapshot",
		"binding_id",
		"listing_revision_id",
		"terms_revision_number",
		"actor_user_id_snapshot",
		"actor_role",
		"consumer_user_id_snapshot",
		"owner_user_id_snapshot",
		"command_payload",
		"command_hash",
		"request_fingerprint",
		"usage_payload",
		"usage_payload_hash",
		"response_summary",
	)
}

func billingIntentAttentionColumnNames() []string {
	return append(billingIntentStateColumnNames(),
		"reason_code",
		"last_error_code",
		"last_error_message",
		"forward_started_at",
		"completed_at",
		"next_attempt_at",
	)
}
