package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
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

func TestConvertExternalPlacementRejectsIncompatibleRoom(t *testing.T) {
	tests := []struct {
		name         string
		accountLevel string
		roomOwner    int64
		roomPlatform string
		roomLevel    string
		wantErr      error
	}{
		{
			name:         "unknown account level",
			accountLevel: service.AccountLevelUnknown,
			roomOwner:    42,
			roomPlatform: service.PlatformOpenAI,
			roomLevel:    service.AccountLevelPlus,
			wantErr:      service.ErrAccountShareRoomUnknownLevel,
		},
		{
			name:         "different owner",
			accountLevel: service.AccountLevelPlus,
			roomOwner:    99,
			roomPlatform: service.PlatformOpenAI,
			roomLevel:    service.AccountLevelPlus,
			wantErr:      service.ErrAccountShareRoomOwnerMismatch,
		},
		{
			name:         "different platform",
			accountLevel: service.AccountLevelPlus,
			roomOwner:    42,
			roomPlatform: service.PlatformAnthropic,
			roomLevel:    service.AccountLevelPlus,
			wantErr:      service.ErrAccountShareRoomPlatformMismatch,
		},
		{
			name:         "different account level",
			accountLevel: service.AccountLevelPlus,
			roomOwner:    42,
			roomPlatform: service.PlatformOpenAI,
			roomLevel:    service.AccountLevelTeam,
			wantErr:      service.ErrAccountShareRoomLevelMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
					AddRow(service.PlatformOpenAI, tt.accountLevel, 1))
			if tt.accountLevel != service.AccountLevelUnknown {
				mock.ExpectQuery("SELECT placement\\.placement_type").
					WithArgs(int64(10), int64(42)).
					WillReturnError(sql.ErrNoRows)
				mock.ExpectQuery("SELECT account_id, target_type, target_listing_id").
					WithArgs(int64(42), "convert-room").
					WillReturnError(sql.ErrNoRows)
				mock.ExpectQuery("SELECT id, owner_user_id, platform, account_level, status").
					WithArgs(sqlmock.AnyArg()).
					WillReturnRows(sqlmock.NewRows([]string{
						"id", "owner_user_id", "platform", "account_level", "status",
						"allowed_models", "codex_cli_only", "codex_5h_limit_percent", "codex_7d_limit_percent",
					}).AddRow(
						roomID,
						tt.roomOwner,
						tt.roomPlatform,
						tt.roomLevel,
						service.AccountShareListingStatusActive,
						[]byte(`["gpt-5.4"]`),
						false,
						100,
						100,
					))
			}
			mock.ExpectRollback()

			_, err = repo.ConvertExternalPlacement(context.Background(), service.ConvertAccountExternalPlacementInput{
				AccountID:      10,
				OwnerUserID:    42,
				Target:         service.AccountExternalPlacementRoom,
				RoomID:         &roomID,
				IdempotencyKey: "convert-room",
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet expectations: %v", err)
			}
		})
	}
}

func TestConvertExternalPlacementCompatibleRoomReachesGroupBinding(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	roomID := int64(700)
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
	mock.ExpectQuery("SELECT id, owner_user_id, platform, account_level, status").
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "owner_user_id", "platform", "account_level", "status",
			"allowed_models", "codex_cli_only", "codex_5h_limit_percent", "codex_7d_limit_percent",
		}).AddRow(
			roomID,
			int64(42),
			service.PlatformOpenAI,
			service.AccountLevelPlus,
			service.AccountShareListingStatusActive,
			[]byte(`["gpt-5.4"]`),
			false,
			100,
			100,
		))
	mock.ExpectQuery("SELECT id\\s+FROM groups").
		WithArgs(int64(42), service.PlatformOpenAI, service.GroupScopeUserPrivate).
		WillReturnError(stopErr)
	mock.ExpectRollback()

	_, err = repo.ConvertExternalPlacement(context.Background(), service.ConvertAccountExternalPlacementInput{
		AccountID:      10,
		OwnerUserID:    42,
		Target:         service.AccountExternalPlacementRoom,
		RoomID:         &roomID,
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

func TestRebindMembershipToHealthyRoomAccountStaysInsideListing(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	now := time.Date(2026, 7, 24, 2, 30, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT listing_id\\s+FROM account_share_memberships").
		WithArgs(int64(500), int64(10), service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued).
		WillReturnRows(sqlmock.NewRows([]string{"listing_id"}).AddRow(int64(700)))
	mock.ExpectQuery("SELECT a\\.id\\s+FROM account_external_placements").
		WithArgs(int64(700), int64(10), now).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(11)))
	mock.ExpectExec("UPDATE account_share_memberships").
		WithArgs(int64(11), int64(500), int64(10), service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	rebound, err := repo.RebindMembershipToHealthyRoomAccount(context.Background(), 500, 10, now)
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

func TestRebindRoomMembershipsPausesRoomWhenLastAccountLeaves(t *testing.T) {
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

	mock.ExpectQuery("SELECT a\\.id\\s+FROM account_external_placements").
		WithArgs(int64(700), int64(10), sqlmock.AnyArg()).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("UPDATE account_share_listings").
		WithArgs(service.AccountShareListingStatusPaused, int64(700)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT id, status\\s+FROM account_share_memberships").
		WithArgs(int64(700), int64(10), service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status"}))

	result, err := repo.rebindRoomMembershipsBeforePlacementRemovalInTx(context.Background(), tx, 700, 10)
	if err != nil {
		t.Fatalf("rebindRoomMembershipsBeforePlacementRemovalInTx: %v", err)
	}
	if result == nil {
		t.Fatal("expected an empty seat billing result")
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
