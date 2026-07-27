package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestTranslateAccountPersistenceErrorForExternalPlacementIdentity(t *testing.T) {
	tests := []struct {
		name       string
		constraint string
		want       error
	}{
		{
			name:       "room level change",
			constraint: "account_external_placement_room_level_change_chk",
			want:       service.ErrOwnedAccountPlacementConversionRequired,
		},
		{
			name:       "public pool level change",
			constraint: "account_external_placement_level_change_chk",
			want:       service.ErrOwnedAccountPlacementConversionRequired,
		},
		{
			name:       "owner or platform change",
			constraint: "account_external_placement_identity_change_chk",
			want:       service.ErrOwnedAccountPlacementConversionRequired,
		},
		{
			name:       "placement identity mismatch",
			constraint: "account_external_placements_account_identity_chk",
			want:       service.ErrAccountExternalPlacementConflict,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := translateAccountPersistenceError(&pq.Error{
				Code:       "23514",
				Constraint: test.constraint,
			}, service.ErrAccountNotFound)
			if !errors.Is(err, test.want) {
				t.Fatalf("expected %v, got %v", test.want, err)
			}
		})
	}
}

func TestTranslateAccountShareRoomPersistenceErrorForOpenAssignmentConflict(t *testing.T) {
	err := translateAccountShareRoomPersistenceError(&pq.Error{
		Code:       "23505",
		Constraint: "uq_account_share_room_assignments_open_account",
	})
	if !errors.Is(err, service.ErrAccountShareRoomAccountConflict) {
		t.Fatalf("error = %v, want %v", err, service.ErrAccountShareRoomAccountConflict)
	}
}

func TestListRoomAccountsRejectsNonOwnerUser(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	mock.ExpectQuery("SELECT owner_user_id").
		WithArgs(int64(700)).
		WillReturnRows(sqlmock.NewRows([]string{"owner_user_id"}).AddRow(int64(42)))

	_, err = repo.ListRoomAccounts(context.Background(), 700, 99, false)

	if !errors.Is(err, service.ErrInsufficientPerms) {
		t.Fatalf("error = %v, want %v", err, service.ErrInsufficientPerms)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestListRoomAccountsAllowsAdministrator(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	lastUsedAt := time.Date(2026, 7, 24, 15, 0, 0, 0, time.UTC)

	mock.ExpectQuery("SELECT owner_user_id").
		WithArgs(int64(700)).
		WillReturnRows(sqlmock.NewRows([]string{"owner_user_id"}).AddRow(int64(42)))
	mock.ExpectQuery("SELECT\\s+a\\.id").
		WithArgs(int64(700), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "platform", "account_level", "status", "schedulable",
			"concurrency", "priority", "state", "last_used_at",
		}).AddRow(
			int64(10),
			"room-account",
			service.PlatformOpenAI,
			service.AccountLevelPlus,
			service.StatusActive,
			true,
			20,
			1,
			"active",
			lastUsedAt,
		))

	accounts, err := repo.ListRoomAccounts(context.Background(), 700, 99, true)

	if err != nil {
		t.Fatalf("ListRoomAccounts: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("accounts length = %d, want 1", len(accounts))
	}
	if accounts[0].AccountID != 10 || accounts[0].AccountName != "room-account" {
		t.Fatalf("unexpected account: %#v", accounts[0])
	}
	if accounts[0].LastUsedAt == nil || !accounts[0].LastUsedAt.Equal(lastUsedAt) {
		t.Fatalf("last_used_at = %v, want %v", accounts[0].LastUsedAt, lastUsedAt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAttachRoomAccountsAtomicLocksSortedIDsAndKeepsPausedRoomPaused(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	projectionCreatedAt := time.Date(2026, 7, 20, 9, 30, 0, 0, time.UTC)
	accountIDs := []int64{10, 11}

	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs("account_share_owner_quota:42").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id, owner_user_id, platform, account_level, status, allowed_models").
		WithArgs(int64(700)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "owner_user_id", "platform", "account_level", "status", "allowed_models"}).
			AddRow(int64(700), int64(42), service.PlatformOpenAI, service.AccountLevelPlus, service.AccountShareListingStatusPaused, `["gpt-5.5"]`))
	mock.ExpectQuery("SELECT\\s+id, name, platform, account_level, concurrency, priority").
		WithArgs(pq.Array(accountIDs), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "platform", "account_level", "concurrency", "priority",
			"status", "schedulable", "type", "credentials", "extra",
		}).
			AddRow(int64(10), "room-account-10", service.PlatformOpenAI, service.AccountLevelPlus, 20, 3, service.StatusActive, true, service.AccountTypeOAuth, `{}`, `{}`).
			AddRow(int64(11), "room-account-11", service.PlatformOpenAI, service.AccountLevelPlus, 30, 4, service.StatusActive, true, service.AccountTypeOAuth, `{}`, `{}`))
	mock.ExpectQuery("SELECT account_id\\s+FROM account_external_placements").
		WithArgs(pq.Array(accountIDs), int64(42), service.PlatformOpenAI).
		WillReturnRows(sqlmock.NewRows([]string{"account_id"}).
			AddRow(int64(10)).
			AddRow(int64(11)))
	mock.ExpectQuery("SELECT account_id, listing_id, state, created_at").
		WithArgs(pq.Array(accountIDs)).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "listing_id", "state", "created_at"}).
			AddRow(int64(10), int64(700), "active", projectionCreatedAt))
	mock.ExpectQuery("SELECT id, listing_id, account_id_snapshot").
		WithArgs(pq.Array(accountIDs)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "listing_id", "account_id_snapshot"}).
			AddRow(int64(900), int64(700), int64(10)))
	expectDefaultAccountShareQuotaPolicy(mock, int64(42))
	expectAccountShareQuotaUsage(mock, 42, service.AccountShareQuotaUsage{
		LiveRooms:           1,
		RoomCreates24Hours:  1,
		OwnerRoomAccounts:   3,
		LargestRoomAccounts: 1,
	})
	mock.ExpectQuery("FROM account_share_room_accounts room_account\\s+WHERE room_account\\.listing_id = \\$1").
		WithArgs(int64(700)).
		WillReturnRows(sqlmock.NewRows([]string{"room_accounts"}).AddRow(1))
	mock.ExpectExec("INSERT INTO account_share_room_accounts").
		WithArgs(
			int64(700),
			int64(11),
			int64(42),
			service.PlatformOpenAI,
			service.AccountLevelPlus,
			4,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO account_share_room_account_assignments").
		WithArgs(
			int64(700),
			int64(11),
			int64(42),
			"room-account-11",
			service.PlatformOpenAI,
			service.AccountLevelPlus,
			30,
			int64(42),
			"owner",
			"owner_attach",
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE account_share_listings\\s+SET updated_at = NOW\\(\\)").
		WithArgs(int64(700), int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repo.AttachRoomAccountsAtomic(context.Background(), service.BatchAccountShareRoomAccountsInput{
		ListingID:      700,
		AccountIDs:     []int64{11, 10, 11},
		OwnerUserID:    42,
		IdempotencyKey: "attach-batch",
	}); err != nil {
		t.Fatalf("AttachRoomAccountsAtomic: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAttachRoomAccountsAtomicRollsBackEarlierWritesWhenLaterAssignmentFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	accountIDs := []int64{10, 11}
	historyErr := errors.New("second assignment insert failed")

	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs("account_share_owner_quota:42").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id, owner_user_id, platform, account_level, status, allowed_models").
		WithArgs(int64(700)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "owner_user_id", "platform", "account_level", "status", "allowed_models"}).
			AddRow(int64(700), int64(42), service.PlatformOpenAI, service.AccountLevelPlus, service.AccountShareListingStatusActive, `["gpt-5.5"]`))
	mock.ExpectQuery("SELECT\\s+id, name, platform, account_level, concurrency, priority").
		WithArgs(pq.Array(accountIDs), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "platform", "account_level", "concurrency", "priority",
			"status", "schedulable", "type", "credentials", "extra",
		}).
			AddRow(int64(10), "room-account-10", service.PlatformOpenAI, service.AccountLevelPlus, 20, 3, service.StatusActive, true, service.AccountTypeOAuth, `{}`, `{}`).
			AddRow(int64(11), "room-account-11", service.PlatformOpenAI, service.AccountLevelPlus, 30, 4, service.StatusActive, true, service.AccountTypeOAuth, `{}`, `{}`))
	mock.ExpectQuery("SELECT account_id\\s+FROM account_external_placements").
		WithArgs(pq.Array(accountIDs), int64(42), service.PlatformOpenAI).
		WillReturnRows(sqlmock.NewRows([]string{"account_id"}).
			AddRow(int64(10)).
			AddRow(int64(11)))
	mock.ExpectQuery("SELECT account_id, listing_id, state, created_at").
		WithArgs(pq.Array(accountIDs)).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "listing_id", "state", "created_at"}))
	mock.ExpectQuery("SELECT id, listing_id, account_id_snapshot").
		WithArgs(pq.Array(accountIDs)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "listing_id", "account_id_snapshot"}))
	expectDefaultAccountShareQuotaPolicy(mock, int64(42))
	expectAccountShareQuotaUsage(mock, 42, service.AccountShareQuotaUsage{
		LiveRooms:           1,
		RoomCreates24Hours:  1,
		OwnerRoomAccounts:   3,
		LargestRoomAccounts: 1,
	})
	mock.ExpectQuery("FROM account_share_room_accounts room_account\\s+WHERE room_account\\.listing_id = \\$1").
		WithArgs(int64(700)).
		WillReturnRows(sqlmock.NewRows([]string{"room_accounts"}).AddRow(1))
	mock.ExpectExec("INSERT INTO account_share_room_accounts").
		WithArgs(
			int64(700),
			int64(10),
			int64(42),
			service.PlatformOpenAI,
			service.AccountLevelPlus,
			3,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO account_share_room_account_assignments").
		WithArgs(
			int64(700),
			int64(10),
			int64(42),
			"room-account-10",
			service.PlatformOpenAI,
			service.AccountLevelPlus,
			20,
			int64(42),
			"owner",
			"owner_attach",
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO account_share_room_accounts").
		WithArgs(
			int64(700),
			int64(11),
			int64(42),
			service.PlatformOpenAI,
			service.AccountLevelPlus,
			4,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO account_share_room_account_assignments").
		WithArgs(
			int64(700),
			int64(11),
			int64(42),
			"room-account-11",
			service.PlatformOpenAI,
			service.AccountLevelPlus,
			30,
			int64(42),
			"owner",
			"owner_attach",
		).
		WillReturnError(historyErr)
	mock.ExpectRollback()

	err = repo.AttachRoomAccountsAtomic(context.Background(), service.BatchAccountShareRoomAccountsInput{
		ListingID:      700,
		AccountIDs:     []int64{11, 10},
		OwnerUserID:    42,
		IdempotencyKey: "attach-rollback",
	})
	if !errors.Is(err, historyErr) {
		t.Fatalf("error = %v, want %v", err, historyErr)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAttachRoomAccountsAtomicRejectsUnavailableOrModelIncompatibleAccounts(t *testing.T) {
	tests := []struct {
		name          string
		status        string
		schedulable   bool
		concurrency   int
		credentials   string
		expectedError error
	}{
		{
			name:          "inactive account",
			status:        service.StatusError,
			schedulable:   true,
			concurrency:   20,
			credentials:   `{}`,
			expectedError: service.ErrAccountShareAccountUnavailable,
		},
		{
			name:          "unschedulable account",
			status:        service.StatusActive,
			schedulable:   false,
			concurrency:   20,
			credentials:   `{}`,
			expectedError: service.ErrAccountShareAccountUnavailable,
		},
		{
			name:          "zero concurrency",
			status:        service.StatusActive,
			schedulable:   true,
			concurrency:   0,
			credentials:   `{}`,
			expectedError: service.ErrAccountShareAccountUnavailable,
		},
		{
			name:          "room model is not supported",
			status:        service.StatusActive,
			schedulable:   true,
			concurrency:   20,
			credentials:   `{"model_mapping":{"gpt-5.4":"gpt-5.4"}}`,
			expectedError: service.ErrAccountShareModeUnsupportedModel,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			repo := &accountShareModeRepository{db: db}
			accountIDs := []int64{10}

			mock.ExpectBegin()
			mock.ExpectExec("SELECT pg_advisory_xact_lock").
				WithArgs("account_share_owner_quota:42").
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery("SELECT id, owner_user_id, platform, account_level, status, allowed_models").
				WithArgs(int64(700)).
				WillReturnRows(sqlmock.NewRows([]string{"id", "owner_user_id", "platform", "account_level", "status", "allowed_models"}).
					AddRow(int64(700), int64(42), service.PlatformOpenAI, service.AccountLevelPlus, service.AccountShareListingStatusActive, `["gpt-5.5"]`))
			mock.ExpectQuery("SELECT\\s+id, name, platform, account_level, concurrency, priority").
				WithArgs(pq.Array(accountIDs), int64(42)).
				WillReturnRows(sqlmock.NewRows([]string{
					"id", "name", "platform", "account_level", "concurrency", "priority",
					"status", "schedulable", "type", "credentials", "extra",
				}).AddRow(
					int64(10),
					"room-account-10",
					service.PlatformOpenAI,
					service.AccountLevelPlus,
					test.concurrency,
					3,
					test.status,
					test.schedulable,
					service.AccountTypeOAuth,
					test.credentials,
					`{}`,
				))
			mock.ExpectRollback()

			err = repo.AttachRoomAccountsAtomic(context.Background(), service.BatchAccountShareRoomAccountsInput{
				ListingID:      700,
				AccountIDs:     accountIDs,
				OwnerUserID:    42,
				IdempotencyKey: "attach-validation",
			})

			require.ErrorIs(t, err, test.expectedError)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestAttachRoomAccountsAtomicRejectsDrainingRoom(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs("account_share_owner_quota:42").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id, owner_user_id, platform, account_level, status, allowed_models").
		WithArgs(int64(700)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "owner_user_id", "platform", "account_level", "status", "allowed_models"}).
			AddRow(int64(700), int64(42), service.PlatformOpenAI, service.AccountLevelPlus, service.AccountShareListingStatusDraining, `["gpt-5.5"]`))
	mock.ExpectRollback()

	err = repo.AttachRoomAccountsAtomic(context.Background(), service.BatchAccountShareRoomAccountsInput{
		ListingID:      700,
		AccountIDs:     []int64{10},
		OwnerUserID:    42,
		IdempotencyKey: "attach-draining",
	})

	require.ErrorIs(t, err, service.ErrAccountShareRoomOperationConflict)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestInsertAccountShareRoomProjectionAndAssignmentForRoomCreation(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	snapshot := roomAssignmentTestSnapshot()

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	mock.ExpectExec(`
		INSERT INTO account_share_room_accounts (
			listing_id, account_id, owner_user_id, platform, account_level,
			state, priority, version, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, 'active', $6, 1, NOW(), NOW())
	`).
		WithArgs(
			snapshot.ListingID,
			snapshot.AccountID,
			snapshot.OwnerUserID,
			snapshot.Platform,
			snapshot.AccountLevel,
			3,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`
		INSERT INTO account_share_room_account_assignments (
			listing_id, account_id, account_id_snapshot,
			owner_user_id, owner_user_id_snapshot,
			account_name_snapshot, platform_snapshot, account_level_snapshot,
			configured_concurrency_snapshot, attached_at,
			attached_by_user_id, attached_by_role, attach_reason,
			snapshot_quality, created_at
		)
		VALUES (
			$1, $2, $2,
			$3, $3,
			$4, $5, $6,
			$7, NOW(),
			$8, $9, $10,
			'exact', NOW()
		)
	`).
		WithArgs(
			snapshot.ListingID,
			snapshot.AccountID,
			snapshot.OwnerUserID,
			snapshot.AccountName,
			snapshot.Platform,
			snapshot.AccountLevel,
			snapshot.ConfiguredConcurrency,
			snapshot.OwnerUserID,
			"owner",
			"room_created",
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = insertAccountShareRoomProjectionAndAssignmentInTx(
		context.Background(),
		tx,
		snapshot,
		3,
		snapshot.OwnerUserID,
		"owner",
		"room_created",
	)
	if err != nil {
		t.Fatalf("insertAccountShareRoomProjectionAndAssignmentInTx: %v", err)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestInsertAccountShareRoomProjectionAndAssignmentRollsBackOnHistoryFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	snapshot := roomAssignmentTestSnapshot()
	historyErr := errors.New("assignment insert failed")

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	mock.ExpectExec("INSERT INTO account_share_room_accounts").
		WithArgs(
			snapshot.ListingID,
			snapshot.AccountID,
			snapshot.OwnerUserID,
			snapshot.Platform,
			snapshot.AccountLevel,
			3,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO account_share_room_account_assignments").
		WithArgs(
			snapshot.ListingID,
			snapshot.AccountID,
			snapshot.OwnerUserID,
			snapshot.AccountName,
			snapshot.Platform,
			snapshot.AccountLevel,
			snapshot.ConfiguredConcurrency,
			snapshot.OwnerUserID,
			"owner",
			"owner_attach",
		).
		WillReturnError(historyErr)

	err = insertAccountShareRoomProjectionAndAssignmentInTx(
		context.Background(),
		tx,
		snapshot,
		3,
		snapshot.OwnerUserID,
		"owner",
		"owner_attach",
	)
	if !errors.Is(err, historyErr) {
		t.Fatalf("error = %v, want %v", err, historyErr)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestInsertBackfilledAccountShareRoomAssignmentUsesProjectionTimestamp(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	snapshot := roomAssignmentTestSnapshot()
	projectionCreatedAt := time.Date(2026, 7, 20, 9, 30, 0, 0, time.UTC)

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	mock.ExpectQuery(`
		INSERT INTO account_share_room_account_assignments (
			listing_id, account_id, account_id_snapshot,
			owner_user_id, owner_user_id_snapshot,
			account_name_snapshot, platform_snapshot, account_level_snapshot,
			configured_concurrency_snapshot, attached_at,
			attached_by_user_id, attached_by_role, attach_reason,
			snapshot_quality, created_at
		)
		VALUES (
			$1, $2, $2,
			$3, $3,
			$4, $5, $6,
			$7, $8,
			NULL, 'system', 'legacy_projection_backfill',
			'backfilled_current', NOW()
		)
		RETURNING id
	`).
		WithArgs(
			snapshot.ListingID,
			snapshot.AccountID,
			snapshot.OwnerUserID,
			snapshot.AccountName,
			snapshot.Platform,
			snapshot.AccountLevel,
			snapshot.ConfiguredConcurrency,
			projectionCreatedAt,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(900)))

	assignmentID, err := insertBackfilledAccountShareRoomAssignmentInTx(
		context.Background(),
		tx,
		snapshot,
		projectionCreatedAt,
	)
	if err != nil {
		t.Fatalf("insertBackfilledAccountShareRoomAssignmentInTx: %v", err)
	}
	if assignmentID != 900 {
		t.Fatalf("assignmentID = %d, want 900", assignmentID)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAttachRoomAccountsAtomicBackfillsLegacyProjectionOnIdempotentTouch(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	snapshot := roomAssignmentTestSnapshot()
	projectionCreatedAt := time.Date(2026, 7, 20, 9, 30, 0, 0, time.UTC)
	accountIDs := []int64{snapshot.AccountID}

	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs("account_share_owner_quota:42").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id, owner_user_id, platform, account_level, status, allowed_models").
		WithArgs(snapshot.ListingID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "owner_user_id", "platform", "account_level", "status", "allowed_models"}).
			AddRow(snapshot.ListingID, snapshot.OwnerUserID, snapshot.Platform, snapshot.AccountLevel, service.AccountShareListingStatusActive, `["gpt-5.5"]`))
	mock.ExpectQuery("SELECT\\s+id, name, platform, account_level, concurrency, priority").
		WithArgs(pq.Array(accountIDs), snapshot.OwnerUserID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "platform", "account_level", "concurrency", "priority",
			"status", "schedulable", "type", "credentials", "extra",
		}).AddRow(
			snapshot.AccountID,
			snapshot.AccountName,
			snapshot.Platform,
			snapshot.AccountLevel,
			snapshot.ConfiguredConcurrency,
			3,
			service.StatusActive,
			true,
			service.AccountTypeOAuth,
			`{}`,
			`{}`,
		))
	mock.ExpectQuery("SELECT account_id\\s+FROM account_external_placements").
		WithArgs(pq.Array(accountIDs), snapshot.OwnerUserID, snapshot.Platform).
		WillReturnRows(sqlmock.NewRows([]string{"account_id"}).AddRow(snapshot.AccountID))
	mock.ExpectQuery("SELECT account_id, listing_id, state, created_at").
		WithArgs(pq.Array(accountIDs)).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "listing_id", "state", "created_at"}).
			AddRow(snapshot.AccountID, snapshot.ListingID, "active", projectionCreatedAt))
	mock.ExpectQuery("SELECT id, listing_id, account_id_snapshot").
		WithArgs(pq.Array(accountIDs)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "listing_id", "account_id_snapshot"}))
	mock.ExpectQuery("INSERT INTO account_share_room_account_assignments").
		WithArgs(
			snapshot.ListingID,
			snapshot.AccountID,
			snapshot.OwnerUserID,
			snapshot.AccountName,
			snapshot.Platform,
			snapshot.AccountLevel,
			snapshot.ConfiguredConcurrency,
			projectionCreatedAt,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(900)))
	mock.ExpectCommit()

	err = repo.AttachRoomAccountsAtomic(context.Background(), service.BatchAccountShareRoomAccountsInput{
		ListingID:      snapshot.ListingID,
		AccountIDs:     accountIDs,
		OwnerUserID:    snapshot.OwnerUserID,
		IdempotencyKey: "attach-backfill",
	})
	if err != nil {
		t.Fatalf("AttachRoomAccountsAtomic: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDetachRoomAccountsAtomicBackfillsAndClosesHistoryBeforeProjectionDelete(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	snapshot := roomAssignmentTestSnapshot()
	secondSnapshot := snapshot
	secondSnapshot.AccountID = 11
	secondSnapshot.AccountName = "room-account-11"
	secondSnapshot.ConfiguredConcurrency = 30
	projectionCreatedAt := time.Date(2026, 7, 20, 9, 30, 0, 0, time.UTC)
	accountIDs := []int64{snapshot.AccountID, secondSnapshot.AccountID}

	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs("account_share_owner_quota:42").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id, owner_user_id, platform, account_level, status, allowed_models").
		WithArgs(snapshot.ListingID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "owner_user_id", "platform", "account_level", "status", "allowed_models"}).
			AddRow(snapshot.ListingID, snapshot.OwnerUserID, snapshot.Platform, snapshot.AccountLevel, service.AccountShareListingStatusActive, `["gpt-5.5"]`))
	mock.ExpectQuery("SELECT\\s+id, name, platform, account_level, concurrency, priority").
		WithArgs(pq.Array(accountIDs), snapshot.OwnerUserID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "platform", "account_level", "concurrency", "priority",
			"status", "schedulable", "type", "credentials", "extra",
		}).AddRow(
			snapshot.AccountID,
			snapshot.AccountName,
			snapshot.Platform,
			snapshot.AccountLevel,
			snapshot.ConfiguredConcurrency,
			3,
			service.StatusActive,
			true,
			service.AccountTypeOAuth,
			`{}`,
			`{}`,
		).AddRow(
			secondSnapshot.AccountID,
			secondSnapshot.AccountName,
			secondSnapshot.Platform,
			secondSnapshot.AccountLevel,
			secondSnapshot.ConfiguredConcurrency,
			4,
			service.StatusActive,
			true,
			service.AccountTypeOAuth,
			`{}`,
			`{}`,
		))
	mock.ExpectQuery("SELECT account_id, listing_id, state, created_at").
		WithArgs(snapshot.ListingID, snapshot.OwnerUserID, pq.Array(accountIDs)).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "listing_id", "state", "created_at"}).
			AddRow(snapshot.AccountID, snapshot.ListingID, "active", projectionCreatedAt).
			AddRow(secondSnapshot.AccountID, snapshot.ListingID, "active", projectionCreatedAt))
	mock.ExpectQuery("SELECT id, listing_id, account_id_snapshot").
		WithArgs(pq.Array(accountIDs)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "listing_id", "account_id_snapshot"}).
			AddRow(int64(900), snapshot.ListingID, snapshot.AccountID))
	mock.ExpectQuery("INSERT INTO account_share_room_account_assignments").
		WithArgs(
			secondSnapshot.ListingID,
			secondSnapshot.AccountID,
			secondSnapshot.OwnerUserID,
			secondSnapshot.AccountName,
			secondSnapshot.Platform,
			secondSnapshot.AccountLevel,
			secondSnapshot.ConfiguredConcurrency,
			projectionCreatedAt,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(901)))
	rebindScopeAccountIDs := []int64{snapshot.AccountID, secondSnapshot.AccountID, 12}
	expectAccountShareRoomRebindScope(
		mock,
		snapshot.ListingID,
		snapshot.OwnerUserID,
		snapshot.Platform,
		snapshot.AccountLevel,
		rebindScopeAccountIDs,
	)
	mock.ExpectQuery("SELECT a\\.id\\s+FROM account_share_room_accounts").
		WithArgs(snapshot.ListingID, pq.Array(accountIDs), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(12)))
	mock.ExpectQuery("SELECT id, listing_id, account_id, listing_revision_id").
		WithArgs(
			snapshot.ListingID,
			pq.Array(accountIDs),
			service.AccountShareMembershipStatusActive,
		).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipRebindColumns()))
	mock.ExpectExec("UPDATE account_share_room_accounts").
		WithArgs(snapshot.ListingID, snapshot.OwnerUserID, pq.Array(accountIDs)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec("UPDATE account_share_room_account_assignments").
		WithArgs(
			snapshot.OwnerUserID,
			"owner",
			"owner_detach",
			int64(900),
			snapshot.ListingID,
			snapshot.AccountID,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE account_share_room_account_assignments").
		WithArgs(
			secondSnapshot.OwnerUserID,
			"owner",
			"owner_detach",
			int64(901),
			secondSnapshot.ListingID,
			secondSnapshot.AccountID,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM account_share_room_accounts").
		WithArgs(snapshot.ListingID, snapshot.OwnerUserID, pq.Array(accountIDs)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	billing, err := repo.DetachRoomAccountsAtomic(context.Background(), service.BatchAccountShareRoomAccountsInput{
		ListingID:      snapshot.ListingID,
		AccountIDs:     []int64{secondSnapshot.AccountID, snapshot.AccountID},
		OwnerUserID:    snapshot.OwnerUserID,
		IdempotencyKey: "detach-batch",
	})
	if err != nil {
		t.Fatalf("DetachRoomAccountsAtomic: %v", err)
	}
	if billing != nil {
		t.Fatalf("billing = %#v, want nil after replacement rebind", billing)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDetachRoomAccountsAtomicRollsBackClosedHistoryWhenProjectionDeleteFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	snapshot := roomAssignmentTestSnapshot()
	projectionCreatedAt := time.Date(2026, 7, 20, 9, 30, 0, 0, time.UTC)
	deleteErr := errors.New("projection delete failed")
	accountIDs := []int64{snapshot.AccountID}

	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs("account_share_owner_quota:42").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id, owner_user_id, platform, account_level, status, allowed_models").
		WithArgs(snapshot.ListingID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "owner_user_id", "platform", "account_level", "status", "allowed_models"}).
			AddRow(snapshot.ListingID, snapshot.OwnerUserID, snapshot.Platform, snapshot.AccountLevel, service.AccountShareListingStatusActive, `["gpt-5.5"]`))
	mock.ExpectQuery("SELECT\\s+id, name, platform, account_level, concurrency, priority").
		WithArgs(pq.Array(accountIDs), snapshot.OwnerUserID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "platform", "account_level", "concurrency", "priority",
			"status", "schedulable", "type", "credentials", "extra",
		}).AddRow(
			snapshot.AccountID,
			snapshot.AccountName,
			snapshot.Platform,
			snapshot.AccountLevel,
			snapshot.ConfiguredConcurrency,
			3,
			service.StatusActive,
			true,
			service.AccountTypeOAuth,
			`{}`,
			`{}`,
		))
	mock.ExpectQuery("SELECT account_id, listing_id, state, created_at").
		WithArgs(snapshot.ListingID, snapshot.OwnerUserID, pq.Array(accountIDs)).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "listing_id", "state", "created_at"}).
			AddRow(snapshot.AccountID, snapshot.ListingID, "active", projectionCreatedAt))
	mock.ExpectQuery("SELECT id, listing_id, account_id_snapshot").
		WithArgs(pq.Array(accountIDs)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "listing_id", "account_id_snapshot"}).
			AddRow(int64(900), snapshot.ListingID, snapshot.AccountID))
	rebindScopeAccountIDs := []int64{snapshot.AccountID, 11}
	expectAccountShareRoomRebindScope(
		mock,
		snapshot.ListingID,
		snapshot.OwnerUserID,
		snapshot.Platform,
		snapshot.AccountLevel,
		rebindScopeAccountIDs,
	)
	mock.ExpectQuery("SELECT a\\.id\\s+FROM account_share_room_accounts").
		WithArgs(snapshot.ListingID, pq.Array(accountIDs), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(11)))
	mock.ExpectQuery("SELECT id, listing_id, account_id, listing_revision_id").
		WithArgs(
			snapshot.ListingID,
			pq.Array(accountIDs),
			service.AccountShareMembershipStatusActive,
		).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipRebindColumns()))
	mock.ExpectExec("UPDATE account_share_room_accounts").
		WithArgs(snapshot.ListingID, snapshot.OwnerUserID, pq.Array(accountIDs)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE account_share_room_account_assignments").
		WithArgs(
			snapshot.OwnerUserID,
			"owner",
			"owner_detach",
			int64(900),
			snapshot.ListingID,
			snapshot.AccountID,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM account_share_room_accounts").
		WithArgs(snapshot.ListingID, snapshot.OwnerUserID, pq.Array(accountIDs)).
		WillReturnError(deleteErr)
	mock.ExpectRollback()

	_, err = repo.DetachRoomAccountsAtomic(context.Background(), service.BatchAccountShareRoomAccountsInput{
		ListingID:      snapshot.ListingID,
		AccountIDs:     accountIDs,
		OwnerUserID:    snapshot.OwnerUserID,
		IdempotencyKey: "detach-rollback",
	})
	if !errors.Is(err, deleteErr) {
		t.Fatalf("error = %v, want %v", err, deleteErr)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCloseAccountShareRoomAssignmentOnlyWritesClosureMetadata(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	snapshot := roomAssignmentTestSnapshot()

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	mock.ExpectExec(`
		UPDATE account_share_room_account_assignments
		SET detached_at = NOW(),
			detached_by_user_id = $1,
			detached_by_role = $2,
			detach_reason = $3
		WHERE id = $4
			AND listing_id = $5
			AND account_id_snapshot = $6
			AND detached_at IS NULL
	`).
		WithArgs(
			snapshot.OwnerUserID,
			"owner",
			"owner_detach",
			int64(900),
			snapshot.ListingID,
			snapshot.AccountID,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = closeAccountShareRoomAssignmentInTx(
		context.Background(),
		tx,
		900,
		snapshot,
		snapshot.OwnerUserID,
		"owner",
		"owner_detach",
	)
	if err != nil {
		t.Fatalf("closeAccountShareRoomAssignmentInTx: %v", err)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func roomAssignmentTestSnapshot() accountShareRoomAssignmentSnapshot {
	return accountShareRoomAssignmentSnapshot{
		ListingID:             700,
		AccountID:             10,
		OwnerUserID:           42,
		AccountName:           "room-account",
		Platform:              service.PlatformOpenAI,
		AccountLevel:          service.AccountLevelPlus,
		ConfiguredConcurrency: 20,
	}
}

func expectDefaultAccountShareQuotaPolicy(mock sqlmock.Sqlmock, ownerUserID int64) {
	policyColumns := []string{
		"id",
		"scope_type",
		"owner_user_id",
		"version",
		"status",
		"override_kind",
		"max_live_rooms",
		"max_room_creates_24_hours",
		"max_accounts_per_room",
		"max_room_accounts_per_owner",
		"effective_at",
		"expires_at",
		"reason",
		"actor_user_id",
		"actor_user_id_snapshot",
		"created_at",
	}
	now := time.Now().UTC()
	mock.ExpectQuery("FROM account_share_quota_policies AS policy").
		WithArgs(service.AccountShareQuotaScopeGlobal, nil, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows(policyColumns).AddRow(
			int64(1),
			service.AccountShareQuotaScopeGlobal,
			nil,
			int64(1),
			service.AccountShareQuotaPolicyStatusActive,
			service.AccountShareQuotaPolicyKindDefault,
			service.AccountShareDefaultMaxLiveRooms,
			service.AccountShareDefaultMaxRoomCreatesPer24Hours,
			service.AccountShareDefaultMaxAccountsPerRoom,
			service.AccountShareDefaultMaxRoomAccountsPerOwner,
			now.Add(-time.Hour),
			nil,
			"initial defaults",
			nil,
			int64(0),
			now.Add(-time.Hour),
		))
	mock.ExpectQuery("FROM account_share_quota_policies AS policy").
		WithArgs(service.AccountShareQuotaScopeOwner, ownerUserID, sqlmock.AnyArg()).
		WillReturnError(sql.ErrNoRows)
}

func expectAccountShareQuotaUsage(
	mock sqlmock.Sqlmock,
	ownerUserID int64,
	usage service.AccountShareQuotaUsage,
) {
	mock.ExpectQuery("SELECT\\s+\\(\\s+SELECT COUNT\\(\\*\\)::int\\s+FROM account_share_listings listing").
		WithArgs(ownerUserID).
		WillReturnRows(sqlmock.NewRows([]string{
			"live_rooms",
			"room_creates_24_hours",
			"owner_room_accounts",
			"largest_room_accounts",
		}).AddRow(
			usage.LiveRooms,
			usage.RoomCreates24Hours,
			usage.OwnerRoomAccounts,
			usage.LargestRoomAccounts,
		))
}

func TestEnforceAccountShareRoomCreationQuotaRejectsLiveRoomLimit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	expectDefaultAccountShareQuotaPolicy(mock, int64(42))
	expectAccountShareQuotaUsage(mock, 42, service.AccountShareQuotaUsage{
		LiveRooms:          service.AccountShareDefaultMaxLiveRooms,
		RoomCreates24Hours: 1,
	})

	err = enforceAccountShareRoomCreationQuotaInTx(context.Background(), tx, 42)
	if !errors.Is(err, service.ErrAccountShareRoomLimitExceeded) {
		t.Fatalf("error = %v, want %v", err, service.ErrAccountShareRoomLimitExceeded)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestEnforceAccountShareRoomAccountQuotaRejectsPerRoomLimit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	expectDefaultAccountShareQuotaPolicy(mock, int64(42))
	expectAccountShareQuotaUsage(mock, 42, service.AccountShareQuotaUsage{
		OwnerRoomAccounts:   40,
		LargestRoomAccounts: service.AccountShareDefaultMaxAccountsPerRoom,
	})
	mock.ExpectQuery("FROM account_share_room_accounts room_account\\s+WHERE room_account\\.listing_id = \\$1").
		WithArgs(int64(700)).
		WillReturnRows(sqlmock.NewRows([]string{"room_accounts"}).
			AddRow(service.AccountShareDefaultMaxAccountsPerRoom))

	err = enforceAccountShareRoomAccountQuotaForAdditionalInTx(context.Background(), tx, 42, 700, 1)
	if !errors.Is(err, service.ErrAccountShareRoomAccountLimitExceeded) {
		t.Fatalf("error = %v, want %v", err, service.ErrAccountShareRoomAccountLimitExceeded)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestEnforceAccountShareRoomAccountQuotaRejectsBatchBeyondRemainingCapacity(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	expectDefaultAccountShareQuotaPolicy(mock, int64(42))
	expectAccountShareQuotaUsage(mock, 42, service.AccountShareQuotaUsage{
		OwnerRoomAccounts:   40,
		LargestRoomAccounts: service.AccountShareDefaultMaxAccountsPerRoom - 1,
	})
	mock.ExpectQuery("FROM account_share_room_accounts room_account\\s+WHERE room_account\\.listing_id = \\$1").
		WithArgs(int64(700)).
		WillReturnRows(sqlmock.NewRows([]string{"room_accounts"}).
			AddRow(service.AccountShareDefaultMaxAccountsPerRoom - 1))

	err = enforceAccountShareRoomAccountQuotaForAdditionalInTx(
		context.Background(),
		tx,
		42,
		700,
		2,
	)
	if !errors.Is(err, service.ErrAccountShareRoomAccountLimitExceeded) {
		t.Fatalf("error = %v, want %v", err, service.ErrAccountShareRoomAccountLimitExceeded)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestEnforceAccountShareRoomGrowthRejectsGrandfatherPolicyBeforeCounting(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	expiry := now.Add(24 * time.Hour)
	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	mock.ExpectQuery("FROM account_share_quota_policies AS policy").
		WithArgs(service.AccountShareQuotaScopeGlobal, nil, sqlmock.AnyArg()).
		WillReturnRows(accountShareQuotaPolicyRows().AddRow(
			int64(1),
			service.AccountShareQuotaScopeGlobal,
			nil,
			int64(1),
			service.AccountShareQuotaPolicyStatusActive,
			service.AccountShareQuotaPolicyKindDefault,
			5,
			5,
			20,
			100,
			now.Add(-time.Hour),
			nil,
			"global",
			nil,
			int64(0),
			now.Add(-time.Hour),
		))
	mock.ExpectQuery("FROM account_share_quota_policies AS policy").
		WithArgs(service.AccountShareQuotaScopeOwner, int64(42), sqlmock.AnyArg()).
		WillReturnRows(accountShareQuotaPolicyRows().AddRow(
			int64(8),
			service.AccountShareQuotaScopeOwner,
			int64(42),
			int64(1),
			service.AccountShareQuotaPolicyStatusActive,
			service.AccountShareQuotaPolicyKindGrandfather,
			8,
			8,
			25,
			150,
			now.Add(-time.Minute),
			expiry,
			"legacy baseline",
			int64(7),
			int64(7),
			now.Add(-time.Minute),
		))

	err = enforceAccountShareRoomAccountQuotaForAdditionalInTx(
		context.Background(),
		tx,
		42,
		700,
		1,
	)
	if !errors.Is(err, service.ErrAccountShareQuotaGrandfatherGrowthBlocked) {
		t.Fatalf(
			"error = %v, want %v",
			err,
			service.ErrAccountShareQuotaGrandfatherGrowthBlocked,
		)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestEnforceAccountShareRoomGrowthRejectsHistoricalOverageWithoutGrandfather(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	expectDefaultAccountShareQuotaPolicy(mock, int64(42))
	expectAccountShareQuotaUsage(mock, 42, service.AccountShareQuotaUsage{
		LiveRooms: service.AccountShareDefaultMaxLiveRooms + 1,
	})

	err = enforceAccountShareRoomAccountQuotaForAdditionalInTx(context.Background(), tx, 42, 700, 1)
	if !errors.Is(err, service.ErrAccountShareQuotaHistoricalGrowthBlocked) {
		t.Fatalf("error = %v, want %v", err, service.ErrAccountShareQuotaHistoricalGrowthBlocked)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPrepareAccountForRoomCreationConvertsPrivateAccountAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	mock.ExpectQuery("SELECT placement\\.placement_type").
		WithArgs(int64(10), int64(42)).
		WillReturnError(sql.ErrNoRows)
	expectPrepareAccountForRoomCreationMutation(mock, 10, 42, 81, 91, 1)

	previous, version, err := repo.prepareAccountForRoomCreationInTx(
		context.Background(),
		tx,
		42,
		10,
		91,
		service.PlatformOpenAI,
		service.AccountLevelPlus,
		3,
	)
	if err != nil {
		t.Fatalf("prepareAccountForRoomCreationInTx: %v", err)
	}
	if previous == nil || previous.Target != service.AccountExternalPlacementPrivate || previous.Version != 0 {
		t.Fatalf("previous = %#v, want private placement version 0", previous)
	}
	if version != 1 {
		t.Fatalf("version = %d, want 1", version)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPrepareAccountForRoomCreationConvertsDrainedPublicAccountAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	updatedAt := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	mock.ExpectQuery("SELECT placement\\.placement_type").
		WithArgs(int64(10), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"placement_type", "listing_id", "room_name", "public_group_id",
			"state", "version", "updated_at",
		}).AddRow(
			service.AccountExternalPlacementPublicPool,
			nil,
			"",
			int64(71),
			"draining",
			int64(7),
			updatedAt,
		))
	expectPrepareAccountForRoomCreationMutation(mock, 10, 42, 81, 91, 8)

	previous, version, err := repo.prepareAccountForRoomCreationInTx(
		context.Background(),
		tx,
		42,
		10,
		91,
		service.PlatformOpenAI,
		service.AccountLevelPlus,
		3,
	)
	if err != nil {
		t.Fatalf("prepareAccountForRoomCreationInTx: %v", err)
	}
	if previous == nil ||
		previous.Target != service.AccountExternalPlacementPublicPool ||
		previous.State != "draining" ||
		previous.Version != 7 {
		t.Fatalf("previous = %#v, want draining public placement version 7", previous)
	}
	if version != 8 {
		t.Fatalf("version = %d, want 8", version)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPrepareAccountForRoomCreationRejectsPublicAccountBeforeDrain(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	updatedAt := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	mock.ExpectQuery("SELECT placement\\.placement_type").
		WithArgs(int64(10), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"placement_type", "listing_id", "room_name", "public_group_id",
			"state", "version", "updated_at",
		}).AddRow(
			service.AccountExternalPlacementPublicPool,
			nil,
			"",
			int64(71),
			"active",
			int64(7),
			updatedAt,
		))

	_, _, err = repo.prepareAccountForRoomCreationInTx(
		context.Background(),
		tx,
		42,
		10,
		91,
		service.PlatformOpenAI,
		service.AccountLevelPlus,
		3,
	)
	if !errors.Is(err, service.ErrAccountExternalPlacementBusy) {
		t.Fatalf("error = %v, want %v", err, service.ErrAccountExternalPlacementBusy)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPrepareAccountForRoomCreationAllowsUnboundRoomMode(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	updatedAt := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	mock.ExpectQuery("SELECT placement\\.placement_type").
		WithArgs(int64(10), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"placement_type", "listing_id", "room_name", "public_group_id",
			"state", "version", "updated_at",
		}).AddRow(
			service.AccountExternalPlacementRoom,
			nil,
			"",
			nil,
			"active",
			int64(7),
			updatedAt,
		))
	mock.ExpectQuery("SELECT account_id, listing_id, state, created_at").
		WithArgs(pq.Array([]int64{10})).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "listing_id", "state", "created_at"}))
	expectPrepareAccountForRoomCreationMutation(mock, 10, 42, 81, 91, 8)

	previous, version, err := repo.prepareAccountForRoomCreationInTx(
		context.Background(),
		tx,
		42,
		10,
		91,
		service.PlatformOpenAI,
		service.AccountLevelPlus,
		3,
	)
	if err != nil {
		t.Fatalf("prepareAccountForRoomCreationInTx: %v", err)
	}
	if previous == nil ||
		previous.Target != service.AccountExternalPlacementRoom ||
		previous.RoomID != nil ||
		previous.State != "active" ||
		previous.Version != 7 {
		t.Fatalf("previous = %#v, want unbound active room placement version 7", previous)
	}
	if version != 8 {
		t.Fatalf("version = %d, want 8", version)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestPrepareAccountForRoomCreationRejectsAccountAlreadyAttachedToRoom(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	updatedAt := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	mock.ExpectQuery("SELECT placement\\.placement_type").
		WithArgs(int64(10), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"placement_type", "listing_id", "room_name", "public_group_id",
			"state", "version", "updated_at",
		}).AddRow(
			service.AccountExternalPlacementRoom,
			nil,
			"",
			nil,
			"active",
			int64(7),
			updatedAt,
		))
	mock.ExpectQuery("SELECT account_id, listing_id, state, created_at").
		WithArgs(pq.Array([]int64{10})).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "listing_id", "state", "created_at"}).
			AddRow(int64(10), int64(700), "active", updatedAt))

	_, _, err = repo.prepareAccountForRoomCreationInTx(
		context.Background(),
		tx,
		42,
		10,
		91,
		service.PlatformOpenAI,
		service.AccountLevelPlus,
		3,
	)
	if !errors.Is(err, service.ErrAccountExternalPlacementConflict) {
		t.Fatalf("error = %v, want %v", err, service.ErrAccountExternalPlacementConflict)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestGetIdempotentRoomCreationReturnsOriginalListing(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	listing := roomCreationIdempotencyTestListing()

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	expectIdempotentRoomCreationQuery(mock, listing, true)

	listingID, err := getIdempotentRoomCreationInTx(
		context.Background(),
		tx,
		42,
		10,
		"create-room",
		"稳定房间",
		listing,
		`["gpt-5"]`,
	)
	if err != nil {
		t.Fatalf("getIdempotentRoomCreationInTx: %v", err)
	}
	if listingID != 700 {
		t.Fatalf("listingID = %d, want 700", listingID)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestGetIdempotentRoomCreationRejectsPayloadMismatch(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	listing := roomCreationIdempotencyTestListing()

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	expectIdempotentRoomCreationQuery(mock, listing, false)

	_, err = getIdempotentRoomCreationInTx(
		context.Background(),
		tx,
		42,
		10,
		"create-room",
		"稳定房间",
		listing,
		`["gpt-5"]`,
	)
	if !errors.Is(err, service.ErrAccountExternalPlacementIdempotency) {
		t.Fatalf("error = %v, want %v", err, service.ErrAccountExternalPlacementIdempotency)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func roomCreationIdempotencyTestListing() *service.AccountShareListing {
	return &service.AccountShareListing{
		SeatLimit:              3,
		RateMultiplier:         1.2,
		AllowedModels:          []string{"gpt-5"},
		PerUserConcurrency:     2,
		HourlyRate:             0.5,
		HourlyFeeWaiverMinimum: 1.5,
		MinBalanceRequired:     10,
		CodexCLIOnly:           true,
		Codex5hLimitPercent:    80,
		Codex7dLimitPercent:    70,
	}
}

func expectIdempotentRoomCreationQuery(mock sqlmock.Sqlmock, listing *service.AccountShareListing, payloadMatches bool) {
	mock.ExpectQuery("SELECT\\s+conversion\\.account_id").
		WithArgs(
			int64(42),
			"create-room",
			"稳定房间",
			listing.SeatLimit,
			listing.RateMultiplier,
			`["gpt-5"]`,
			listing.PerUserConcurrency,
			listing.HourlyRate,
			listing.HourlyFeeWaiverMinimum,
			listing.MinBalanceRequired,
			listing.CodexCLIOnly,
			listing.Codex5hLimitPercent,
			listing.Codex7dLimitPercent,
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"account_id",
			"target_type",
			"target_listing_id",
			"target_public_group_id",
			"payload_matches",
		}).AddRow(
			int64(10),
			service.AccountExternalPlacementRoom,
			int64(700),
			nil,
			payloadMatches,
		))
}

func expectPrepareAccountForRoomCreationMutation(
	mock sqlmock.Sqlmock,
	accountID, ownerUserID, privateGroupID, modeGroupID, version int64,
) {
	mock.ExpectQuery("SELECT id\\s+FROM groups").
		WithArgs(ownerUserID, service.PlatformOpenAI, service.GroupScopeUserPrivate).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(privateGroupID))
	mock.ExpectQuery("SELECT GREATEST").
		WithArgs(accountID).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(version - 1))
	mock.ExpectExec("DELETE FROM account_groups").
		WithArgs(accountID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO account_groups").
		WithArgs(accountID, privateGroupID).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO account_groups").
		WithArgs(accountID, modeGroupID).
		WillReturnResult(sqlmock.NewResult(2, 1))
	mock.ExpectExec("UPDATE accounts").
		WithArgs(
			service.AccountShareModePrivate,
			service.AccountShareStatusApproved,
			accountID,
			ownerUserID,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO account_external_placements").
		WithArgs(
			accountID,
			ownerUserID,
			service.PlatformOpenAI,
			service.AccountLevelPlus,
			service.AccountExternalPlacementRoom,
			nil,
			nil,
			3,
			version,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO scheduler_outbox").
		WithArgs(
			service.SchedulerOutboxEventAccountChanged,
			sqlmock.AnyArg(),
			nil,
			nil,
			sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO scheduler_outbox").
		WithArgs(
			service.SchedulerOutboxEventAccountGroupsChanged,
			sqlmock.AnyArg(),
			nil,
			sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(2, 1))
}

func TestConvertExternalPlacementRejectsSpecificRoomTarget(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	roomID := int64(700)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT platform, account_level, priority").
		WithArgs(int64(10), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"platform", "account_level", "priority"}).
			AddRow(service.PlatformOpenAI, service.AccountLevelPlus, 1))
	mock.ExpectQuery("SELECT placement\\.placement_type").
		WithArgs(int64(10), int64(42)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT account_id, target_type, target_listing_id").
		WithArgs(int64(42), "convert-room").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err = repo.ConvertExternalPlacement(context.Background(), service.ConvertAccountExternalPlacementInput{
		AccountID:      10,
		OwnerUserID:    42,
		Target:         service.AccountExternalPlacementRoom,
		RoomID:         &roomID,
		IdempotencyKey: "convert-room",
	})
	if !errors.Is(err, service.ErrAccountExternalPlacementInvalid) {
		t.Fatalf("error = %v, want %v", err, service.ErrAccountExternalPlacementInvalid)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestConvertExternalPlacementCompatibleRoomReachesGroupBinding(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	stopErr := errors.New("stop after room compatibility validation")

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT platform, account_level, priority").
		WithArgs(int64(10), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"platform", "account_level", "priority"}).
			AddRow(service.PlatformOpenAI, service.AccountLevelPlus, 1))
	mock.ExpectQuery("SELECT placement\\.placement_type").
		WithArgs(int64(10), int64(42)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT account_id, target_type, target_listing_id").
		WithArgs(int64(42), "compatible-room").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT id\\s+FROM groups").
		WithArgs(int64(42), service.PlatformOpenAI, service.GroupScopeUserPrivate).
		WillReturnError(stopErr)
	mock.ExpectRollback()

	_, err = repo.ConvertExternalPlacement(context.Background(), service.ConvertAccountExternalPlacementInput{
		AccountID:      10,
		OwnerUserID:    42,
		Target:         service.AccountExternalPlacementRoom,
		IdempotencyKey: "compatible-room",
	})
	if !errors.Is(err, stopErr) {
		t.Fatalf("error = %v, want compatibility sentinel", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestConvertExternalPlacementIdempotentRetry(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	roomID := int64(700)
	stored := &service.ConvertAccountExternalPlacementResult{
		AccountID: 10,
		Previous:  privateAccountExternalPlacement(0),
		Current: &service.AccountExternalPlacement{
			Target:  service.AccountExternalPlacementRoom,
			RoomID:  &roomID,
			State:   "active",
			Version: 1,
		},
	}
	storedJSON, err := json.Marshal(stored)
	if err != nil {
		t.Fatalf("marshal stored result: %v", err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT platform, account_level, priority").
		WithArgs(int64(10), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"platform", "account_level", "priority"}).
			AddRow(service.PlatformOpenAI, service.AccountLevelPlus, 1))
	mock.ExpectQuery("SELECT placement\\.placement_type").
		WithArgs(int64(10), int64(42)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT account_id, target_type, target_listing_id").
		WithArgs(int64(42), "same-request").
		WillReturnRows(sqlmock.NewRows([]string{
			"account_id", "target_type", "target_listing_id", "target_public_group_id", "result",
		}).AddRow(int64(10), service.AccountExternalPlacementRoom, roomID, nil, storedJSON))
	mock.ExpectCommit()

	result, err := repo.ConvertExternalPlacement(context.Background(), service.ConvertAccountExternalPlacementInput{
		AccountID:      10,
		OwnerUserID:    42,
		Target:         service.AccountExternalPlacementRoom,
		RoomID:         &roomID,
		IdempotencyKey: "same-request",
	})
	if err != nil {
		t.Fatalf("ConvertExternalPlacement: %v", err)
	}
	if result == nil || result.Current == nil || result.Current.RoomID == nil || *result.Current.RoomID != roomID || result.Current.Version != 1 {
		t.Fatalf("unexpected idempotent result: %#v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestConvertExternalPlacementIdempotencyConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	roomID := int64(700)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT platform, account_level, priority").
		WithArgs(int64(10), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"platform", "account_level", "priority"}).
			AddRow(service.PlatformOpenAI, service.AccountLevelPlus, 1))
	mock.ExpectQuery("SELECT placement\\.placement_type").
		WithArgs(int64(10), int64(42)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT account_id, target_type, target_listing_id").
		WithArgs(int64(42), "reused-request").
		WillReturnRows(sqlmock.NewRows([]string{
			"account_id", "target_type", "target_listing_id", "target_public_group_id", "result",
		}).AddRow(int64(10), service.AccountExternalPlacementPublicPool, nil, int64(90), []byte(`{}`)))
	mock.ExpectRollback()

	_, err = repo.ConvertExternalPlacement(context.Background(), service.ConvertAccountExternalPlacementInput{
		AccountID:      10,
		OwnerUserID:    42,
		Target:         service.AccountExternalPlacementRoom,
		RoomID:         &roomID,
		IdempotencyKey: "reused-request",
	})
	if !errors.Is(err, service.ErrAccountExternalPlacementIdempotency) {
		t.Fatalf("error = %v, want idempotency conflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestGetOpenMembershipRuntimeBindingReturnsValidatedSnapshot(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	mock.ExpectQuery("SELECT\\s+binding\\.id,\\s+binding\\.membership_id").
		WithArgs(int64(500), int64(10), service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"membership_id",
			"listing_id",
			"account_id_snapshot",
			"listing_revision_id",
			"terms_revision_number",
			"routing_generation",
		}).AddRow(
			int64(600),
			int64(500),
			int64(700),
			int64(10),
			int64(900),
			int64(3),
			int64(2),
		))

	binding, err := repo.GetOpenMembershipRuntimeBinding(context.Background(), 500, 10)
	if err != nil {
		t.Fatalf("GetOpenMembershipRuntimeBinding: %v", err)
	}
	if binding == nil ||
		binding.BindingID != 600 ||
		binding.MembershipID != 500 ||
		binding.ListingID != 700 ||
		binding.AccountID != 10 ||
		binding.ListingRevisionID != 900 ||
		binding.TermsRevisionNumber != 3 ||
		binding.RoutingGeneration != 2 {
		t.Fatalf("unexpected binding: %#v", binding)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestGetOpenMembershipRuntimeBindingRejectsStaleProjection(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	mock.ExpectQuery("SELECT\\s+binding\\.id,\\s+binding\\.membership_id").
		WithArgs(int64(500), int64(10), service.AccountShareMembershipStatusActive).
		WillReturnError(sql.ErrNoRows)

	binding, err := repo.GetOpenMembershipRuntimeBinding(context.Background(), 500, 10)
	if binding != nil {
		t.Fatalf("binding = %#v, want nil", binding)
	}
	if !errors.Is(err, service.ErrAccountShareBillingBindingUnavailable) {
		t.Fatalf("error = %v, want binding unavailable", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestRebindMembershipToHealthyRoomAccountMaterializesLegacyBindingAndRotatesGeneration(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	now := time.Date(2026, 7, 24, 2, 30, 0, 0, time.UTC)
	membershipID := int64(500)
	listingID := int64(700)
	currentAccountID := int64(10)
	replacementAccountID := int64(11)
	listingRevisionID := int64(900)

	mock.ExpectQuery("SELECT listing_id\\s+FROM account_share_memberships").
		WithArgs(membershipID, currentAccountID, service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"listing_id"}).AddRow(listingID))
	mock.ExpectBegin()
	expectAccountShareRoomRebindScope(
		mock,
		listingID,
		42,
		service.PlatformOpenAI,
		service.AccountLevelPlus,
		[]int64{currentAccountID, replacementAccountID},
	)
	mock.ExpectQuery("SELECT a\\.id\\s+FROM account_share_room_accounts").
		WithArgs(listingID, currentAccountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(replacementAccountID))
	mock.ExpectQuery("SELECT id, listing_id, account_id, listing_revision_id").
		WithArgs(listingID, currentAccountID, service.AccountShareMembershipStatusActive, membershipID).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipRebindColumns()).
			AddRow(membershipID, listingID, currentAccountID, listingRevisionID))
	mock.ExpectQuery("SELECT id, membership_id, listing_id, account_id_snapshot, listing_revision_id").
		WithArgs(pq.Array([]int64{membershipID})).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipOpenBindingColumns()))
	mock.ExpectQuery("SELECT id, membership_id, status\\s+FROM account_share_request_billing_intents").
		WithArgs(pq.Array([]int64{membershipID})).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipPendingIntentColumns()))
	expectAccountShareMembershipBindingInsert(
		mock,
		membershipID,
		listingID,
		currentAccountID,
		listingRevisionID,
		now,
		accountShareBindingReasonLegacyProjectionMaterialized,
		600,
		1,
		nil,
	)
	mock.ExpectExec("UPDATE account_share_membership_account_bindings").
		WithArgs(
			now,
			nil,
			"system",
			accountShareBindingReasonAccountRebind,
			membershipID,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE account_share_memberships\\s+SET account_id").
		WithArgs(
			replacementAccountID,
			now,
			membershipID,
			listingID,
			currentAccountID,
			listingRevisionID,
			service.AccountShareMembershipStatusActive,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	expectAccountShareMembershipBindingInsert(
		mock,
		membershipID,
		listingID,
		replacementAccountID,
		listingRevisionID,
		now,
		accountShareBindingReasonAccountRebind,
		601,
		2,
		nil,
	)
	mock.ExpectCommit()

	rebound, err := repo.RebindMembershipToHealthyRoomAccount(
		context.Background(),
		membershipID,
		currentAccountID,
		now,
	)
	if err != nil {
		t.Fatalf("RebindMembershipToHealthyRoomAccount: %v", err)
	}
	if !rebound {
		t.Fatal("expected membership to be rebound")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestRebindMembershipToHealthyRoomAccountRejectsPendingBillingIntent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	now := time.Date(2026, 7, 24, 2, 31, 0, 0, time.UTC)
	membershipID := int64(500)
	listingID := int64(700)
	currentAccountID := int64(10)
	replacementAccountID := int64(11)
	listingRevisionID := int64(900)

	mock.ExpectQuery("SELECT listing_id\\s+FROM account_share_memberships").
		WithArgs(membershipID, currentAccountID, service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"listing_id"}).AddRow(listingID))
	mock.ExpectBegin()
	expectAccountShareRoomRebindScope(
		mock,
		listingID,
		42,
		service.PlatformOpenAI,
		service.AccountLevelPlus,
		[]int64{currentAccountID, replacementAccountID},
	)
	mock.ExpectQuery("SELECT a\\.id\\s+FROM account_share_room_accounts").
		WithArgs(listingID, currentAccountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(replacementAccountID))
	mock.ExpectQuery("SELECT id, listing_id, account_id, listing_revision_id").
		WithArgs(listingID, currentAccountID, service.AccountShareMembershipStatusActive, membershipID).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipRebindColumns()).
			AddRow(membershipID, listingID, currentAccountID, listingRevisionID))
	mock.ExpectQuery("SELECT id, membership_id, listing_id, account_id_snapshot, listing_revision_id").
		WithArgs(pq.Array([]int64{membershipID})).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipOpenBindingColumns()).
			AddRow(int64(600), membershipID, listingID, currentAccountID, listingRevisionID))
	mock.ExpectQuery("SELECT id, membership_id, status\\s+FROM account_share_request_billing_intents").
		WithArgs(pq.Array([]int64{membershipID})).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipPendingIntentColumns()).
			AddRow(int64(800), membershipID, "in_flight"))
	mock.ExpectRollback()

	rebound, err := repo.RebindMembershipToHealthyRoomAccount(
		context.Background(),
		membershipID,
		currentAccountID,
		now,
	)
	if rebound {
		t.Fatal("pending billing intent must keep the original binding")
	}
	if !errors.Is(err, service.ErrAccountShareRoomOperationConflict) {
		t.Fatalf("error = %v, want operation conflict", err)
	}
	appErr := infraerrors.FromError(err)
	if appErr.Metadata["blocker"] != "pending_billing_intent" ||
		appErr.Metadata["membership_id"] != "500" ||
		appErr.Metadata["billing_intent_id"] != "800" ||
		appErr.Metadata["intent_status"] != "in_flight" {
		t.Fatalf("unexpected conflict metadata: %#v", appErr.Metadata)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestRebindMembershipToHealthyRoomAccountIgnoresQueuedMembership(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	mock.ExpectQuery("SELECT listing_id\\s+FROM account_share_memberships").
		WithArgs(int64(500), int64(10), service.AccountShareMembershipStatusActive).
		WillReturnError(sql.ErrNoRows)

	rebound, err := repo.RebindMembershipToHealthyRoomAccount(
		context.Background(),
		500,
		10,
		time.Date(2026, 7, 24, 2, 32, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("RebindMembershipToHealthyRoomAccount: %v", err)
	}
	if rebound {
		t.Fatal("queued membership must not be rebound")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestRebindMembershipToHealthyRoomAccountRollsBackWhenNewBindingFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	now := time.Date(2026, 7, 24, 2, 33, 0, 0, time.UTC)
	membershipID := int64(500)
	listingID := int64(700)
	currentAccountID := int64(10)
	replacementAccountID := int64(11)
	listingRevisionID := int64(900)
	insertErr := errors.New("new binding insert failed")

	mock.ExpectQuery("SELECT listing_id\\s+FROM account_share_memberships").
		WithArgs(membershipID, currentAccountID, service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"listing_id"}).AddRow(listingID))
	mock.ExpectBegin()
	expectAccountShareRoomRebindScope(
		mock,
		listingID,
		42,
		service.PlatformOpenAI,
		service.AccountLevelPlus,
		[]int64{currentAccountID, replacementAccountID},
	)
	mock.ExpectQuery("SELECT a\\.id\\s+FROM account_share_room_accounts").
		WithArgs(listingID, currentAccountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(replacementAccountID))
	mock.ExpectQuery("SELECT id, listing_id, account_id, listing_revision_id").
		WithArgs(listingID, currentAccountID, service.AccountShareMembershipStatusActive, membershipID).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipRebindColumns()).
			AddRow(membershipID, listingID, currentAccountID, listingRevisionID))
	mock.ExpectQuery("SELECT id, membership_id, listing_id, account_id_snapshot, listing_revision_id").
		WithArgs(pq.Array([]int64{membershipID})).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipOpenBindingColumns()).
			AddRow(int64(600), membershipID, listingID, currentAccountID, listingRevisionID))
	mock.ExpectQuery("SELECT id, membership_id, status\\s+FROM account_share_request_billing_intents").
		WithArgs(pq.Array([]int64{membershipID})).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipPendingIntentColumns()))
	mock.ExpectExec("UPDATE account_share_membership_account_bindings").
		WithArgs(
			now,
			nil,
			"system",
			accountShareBindingReasonAccountRebind,
			membershipID,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE account_share_memberships\\s+SET account_id").
		WithArgs(
			replacementAccountID,
			now,
			membershipID,
			listingID,
			currentAccountID,
			listingRevisionID,
			service.AccountShareMembershipStatusActive,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	expectAccountShareMembershipBindingInsert(
		mock,
		membershipID,
		listingID,
		replacementAccountID,
		listingRevisionID,
		now,
		accountShareBindingReasonAccountRebind,
		0,
		0,
		insertErr,
	)
	mock.ExpectRollback()

	rebound, err := repo.RebindMembershipToHealthyRoomAccount(
		context.Background(),
		membershipID,
		currentAccountID,
		now,
	)
	if rebound {
		t.Fatal("failed binding rotation must not report a rebind")
	}
	if !errors.Is(err, insertErr) {
		t.Fatalf("error = %v, want %v", err, insertErr)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestRebindRoomMembershipSetUsesStableReplacementOutsideRemovalSet(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	listingID := int64(700)
	sourceAccountIDs := []int64{10, 11}
	replacementAccountID := int64(12)
	listingRevisionID := int64(900)
	firstMembershipID := int64(500)
	secondMembershipID := int64(501)

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	expectAccountShareRoomRebindScope(
		mock,
		listingID,
		42,
		service.PlatformOpenAI,
		service.AccountLevelPlus,
		[]int64{10, 11, 12},
	)
	mock.ExpectQuery("SELECT a\\.id\\s+FROM account_share_room_accounts").
		WithArgs(listingID, pq.Array(sourceAccountIDs), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(replacementAccountID))
	mock.ExpectQuery("SELECT id, listing_id, account_id, listing_revision_id").
		WithArgs(listingID, pq.Array(sourceAccountIDs), service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipRebindColumns()).
			AddRow(firstMembershipID, listingID, sourceAccountIDs[0], listingRevisionID).
			AddRow(secondMembershipID, listingID, sourceAccountIDs[1], listingRevisionID))
	membershipIDs := []int64{firstMembershipID, secondMembershipID}
	mock.ExpectQuery("SELECT id, membership_id, listing_id, account_id_snapshot, listing_revision_id").
		WithArgs(pq.Array(membershipIDs)).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipOpenBindingColumns()).
			AddRow(int64(600), firstMembershipID, listingID, sourceAccountIDs[0], listingRevisionID).
			AddRow(int64(601), secondMembershipID, listingID, sourceAccountIDs[1], listingRevisionID))
	mock.ExpectQuery("SELECT id, membership_id, status\\s+FROM account_share_request_billing_intents").
		WithArgs(pq.Array(membershipIDs)).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipPendingIntentColumns()))

	for index, membershipID := range membershipIDs {
		mock.ExpectExec("UPDATE account_share_membership_account_bindings").
			WithArgs(
				sqlmock.AnyArg(),
				nil,
				"system",
				accountShareBindingReasonAccountRebind,
				membershipID,
			).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("UPDATE account_share_memberships\\s+SET account_id").
			WithArgs(
				replacementAccountID,
				sqlmock.AnyArg(),
				membershipID,
				listingID,
				sourceAccountIDs[index],
				listingRevisionID,
				service.AccountShareMembershipStatusActive,
			).
			WillReturnResult(sqlmock.NewResult(0, 1))
		expectAccountShareMembershipBindingInsert(
			mock,
			membershipID,
			listingID,
			replacementAccountID,
			listingRevisionID,
			sqlmock.AnyArg(),
			accountShareBindingReasonAccountRebind,
			int64(602+index),
			2,
			nil,
		)
	}
	mock.ExpectRollback()

	result, err := repo.rebindRoomMembershipsBeforePlacementRemovalSetInTx(
		context.Background(),
		tx,
		listingID,
		sourceAccountIDs,
	)
	if err != nil {
		t.Fatalf("rebindRoomMembershipsBeforePlacementRemovalSetInTx: %v", err)
	}
	if result != nil {
		t.Fatalf("result = %#v, want nil after direct rebind", result)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestRebindRoomMembershipsRejectsLastAccountRemovalWithActiveMembership(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	listingID := int64(700)
	accountID := int64(10)
	membershipID := int64(500)
	listingRevisionID := int64(900)

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	expectAccountShareRoomRebindScope(
		mock,
		listingID,
		42,
		service.PlatformOpenAI,
		service.AccountLevelPlus,
		[]int64{accountID},
	)
	mock.ExpectQuery("SELECT a\\.id\\s+FROM account_share_room_accounts").
		WithArgs(listingID, pq.Array([]int64{accountID}), sqlmock.AnyArg()).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT id, listing_id, account_id, listing_revision_id").
		WithArgs(listingID, pq.Array([]int64{accountID}), service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipRebindColumns()).
			AddRow(membershipID, listingID, accountID, listingRevisionID))
	mock.ExpectQuery("SELECT id, membership_id, listing_id, account_id_snapshot, listing_revision_id").
		WithArgs(pq.Array([]int64{membershipID})).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipOpenBindingColumns()).
			AddRow(int64(600), membershipID, listingID, accountID, listingRevisionID))
	mock.ExpectQuery("SELECT id, membership_id, status\\s+FROM account_share_request_billing_intents").
		WithArgs(pq.Array([]int64{membershipID})).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipPendingIntentColumns()))
	mock.ExpectRollback()

	result, err := repo.rebindRoomMembershipsBeforePlacementRemovalInTx(
		context.Background(),
		tx,
		listingID,
		accountID,
	)
	if result != nil {
		t.Fatalf("result = %#v, want nil blocker", result)
	}
	if !errors.Is(err, service.ErrAccountShareRoomOperationConflict) {
		t.Fatalf("error = %v, want operation conflict", err)
	}
	appErr := infraerrors.FromError(err)
	if appErr.Metadata["blocker"] != "no_healthy_replacement_account" ||
		appErr.Metadata["listing_id"] != "700" ||
		appErr.Metadata["account_id"] != "10" ||
		appErr.Metadata["membership_id"] != "500" {
		t.Fatalf("unexpected conflict metadata: %#v", appErr.Metadata)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestRebindRoomMembershipsIgnoresQueuedMembershipAndPausesEmptyRoom(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	listingID := int64(700)
	accountID := int64(10)
	ownerUserID := int64(42)
	revisionID := int64(701)
	nextVersion := int64(2)

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	expectAccountShareRoomRebindScope(
		mock,
		listingID,
		ownerUserID,
		service.PlatformOpenAI,
		service.AccountLevelPlus,
		[]int64{accountID},
	)
	mock.ExpectQuery("SELECT a\\.id\\s+FROM account_share_room_accounts").
		WithArgs(listingID, pq.Array([]int64{accountID}), sqlmock.AnyArg()).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT id, listing_id, account_id, listing_revision_id").
		WithArgs(listingID, pq.Array([]int64{accountID}), service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipRebindColumns()))
	mock.ExpectExec("UPDATE account_share_listings").
		WithArgs(
			service.AccountShareListingStatusPaused,
			sqlmock.AnyArg(),
			accountShareRoomStatusReasonNoAccounts,
			accountShareRoomStatusMessageNoAccounts,
			listingID,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT\\s+l\\.id, l\\.row_version").
		WithArgs(listingID).
		WillReturnRows(accountShareRevisionSnapshotRows(
			listingID,
			nextVersion,
			"room",
			ownerUserID,
			"owner",
			func(row *accountShareRevisionSourceRowData) {
				row.AccountLevel = service.AccountLevelPlus
				row.Status = service.AccountShareListingStatusPaused
			},
		))
	mock.ExpectQuery("INSERT INTO account_share_listing_revisions").
		WithArgs(
			listingID,
			nextVersion,
			1,
			service.AccountShareSnapshotQualityExact,
			"room",
			service.PlatformOpenAI,
			service.AccountLevelPlus,
			ownerUserID,
			"owner",
			service.AccountShareListingStatusPaused,
			4,
			0.2,
			`["gpt-5.5"]`,
			5,
			0.15,
			0.0,
			1.0,
			false,
			99.0,
			99.0,
			nil,
			"system",
			"account_placement_removal",
			accountShareRoomStatusMessageNoAccounts,
			nil,
			false,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(revisionID))
	mock.ExpectExec("UPDATE account_share_listings\\s+SET current_revision_id").
		WithArgs(revisionID, listingID, nextVersion).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO account_share_room_events").
		WithArgs(
			listingID,
			revisionID,
			"listing.auto_paused",
			nil,
			"system",
			accountShareRoomStatusMessageNoAccounts,
			`{"force_applied":false,"removed_account_ids":[10],"row_version":2,"source":"account_placement_removal","status_reason_code":"no_room_accounts"}`,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	result, err := repo.rebindRoomMembershipsBeforePlacementRemovalInTx(
		context.Background(),
		tx,
		listingID,
		accountID,
	)
	if err != nil {
		t.Fatalf("rebindRoomMembershipsBeforePlacementRemovalInTx: %v", err)
	}
	if result == nil {
		t.Fatal("expected an empty seat billing result")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func expectAccountShareRoomRebindScope(
	mock sqlmock.Sqlmock,
	listingID int64,
	ownerUserID int64,
	platform string,
	accountLevel string,
	accountIDs []int64,
) {
	mock.ExpectQuery("SELECT id, owner_user_id, platform, account_level, status, allowed_models").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "owner_user_id", "platform", "account_level", "status", "allowed_models"}).
			AddRow(listingID, ownerUserID, platform, accountLevel, service.AccountShareListingStatusActive, `["gpt-5.5"]`))
	roomAccountRows := sqlmock.NewRows([]string{"account_id"})
	accountRows := sqlmock.NewRows([]string{"id"})
	for _, accountID := range accountIDs {
		roomAccountRows.AddRow(accountID)
		accountRows.AddRow(accountID)
	}
	mock.ExpectQuery("SELECT account_id\\s+FROM account_share_room_accounts").
		WithArgs(listingID).
		WillReturnRows(roomAccountRows)
	if len(accountIDs) > 0 {
		mock.ExpectQuery("SELECT id\\s+FROM accounts").
			WithArgs(pq.Array(accountIDs)).
			WillReturnRows(accountRows)
	}
}

func expectAccountShareMembershipBindingInsert(
	mock sqlmock.Sqlmock,
	membershipID int64,
	listingID int64,
	accountID int64,
	listingRevisionID int64,
	now any,
	reason string,
	bindingID int64,
	generation int64,
	queryErr error,
) {
	mock.ExpectQuery("SELECT\\s+room_account.listing_id,\\s+room_account.account_id").
		WithArgs(listingID, accountID).
		WillReturnRows(sqlmock.NewRows([]string{
			"listing_id", "account_id", "owner_user_id", "name",
			"platform", "account_level", "concurrency", "created_at",
		}).AddRow(
			listingID,
			accountID,
			int64(42),
			"room-account",
			service.PlatformOpenAI,
			service.AccountLevelPlus,
			20,
			time.Now().UTC(),
		))
	mock.ExpectQuery("SELECT id, listing_id, account_id_snapshot").
		WithArgs(pq.Array([]int64{accountID})).
		WillReturnRows(sqlmock.NewRows([]string{"id", "listing_id", "account_id_snapshot"}).
			AddRow(accountID+100000, listingID, accountID))
	expectation := mock.ExpectQuery("WITH binding_source AS MATERIALIZED").
		WithArgs(
			membershipID,
			listingID,
			accountID,
			listingRevisionID,
			now,
			nil,
			"system",
			reason,
		)
	if queryErr != nil {
		expectation.WillReturnError(queryErr)
		return
	}
	expectation.WillReturnRows(sqlmock.NewRows([]string{"id", "routing_generation"}).
		AddRow(bindingID, generation))
}

func accountShareMembershipRebindColumns() []string {
	return []string{"id", "listing_id", "account_id", "listing_revision_id"}
}

func accountShareMembershipOpenBindingColumns() []string {
	return []string{"id", "membership_id", "listing_id", "account_id_snapshot", "listing_revision_id"}
}

func accountShareMembershipPendingIntentColumns() []string {
	return []string{"id", "membership_id", "status"}
}
