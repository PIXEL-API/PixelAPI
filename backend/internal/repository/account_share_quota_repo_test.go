package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestResolveAccountShareQuotaUsesActiveOwnerOverride(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	})
	repo := &accountShareModeRepository{db: db}
	at := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	expiry := at.Add(24 * time.Hour)

	mock.ExpectQuery("FROM account_share_quota_policies AS policy").
		WithArgs(service.AccountShareQuotaScopeGlobal, nil, at).
		WillReturnRows(accountShareQuotaPolicyRows().AddRow(
			int64(1), service.AccountShareQuotaScopeGlobal, nil, int64(2),
			service.AccountShareQuotaPolicyStatusActive,
			service.AccountShareQuotaPolicyKindDefault,
			5, 5, 20, 100,
			at.Add(-time.Hour), nil, "global", nil, int64(0), at.Add(-time.Hour),
		))
	mock.ExpectQuery("FROM account_share_quota_policies AS policy").
		WithArgs(service.AccountShareQuotaScopeOwner, int64(42), at).
		WillReturnRows(accountShareQuotaPolicyRows().AddRow(
			int64(9), service.AccountShareQuotaScopeOwner, int64(42), int64(3),
			service.AccountShareQuotaPolicyStatusActive,
			service.AccountShareQuotaPolicyKindManual,
			9, 10, 30, 200,
			at.Add(-time.Minute), expiry, "temporary capacity", int64(7), int64(7), at.Add(-time.Minute),
		))

	got, err := repo.ResolveAccountShareQuota(context.Background(), 42, at)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "owner_override", got.Source)
	require.Equal(t, int64(9), got.PolicyID)
	require.Equal(t, int64(3), got.PolicyVersion)
	require.Equal(t, 30, got.Limits.MaxAccountsPerRoom)
	require.False(t, got.GrowthBlocked)
	require.NotNil(t, got.OverrideExpiresAt)
	require.Equal(t, expiry, *got.OverrideExpiresAt)
}

func TestResolveAccountShareQuotaDoesNotReactivateExpiredOverride(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	})
	repo := &accountShareModeRepository{db: db}
	at := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery("FROM account_share_quota_policies AS policy").
		WithArgs(service.AccountShareQuotaScopeGlobal, nil, at).
		WillReturnRows(accountShareQuotaPolicyRows().AddRow(
			int64(1), service.AccountShareQuotaScopeGlobal, nil, int64(2),
			service.AccountShareQuotaPolicyStatusActive,
			service.AccountShareQuotaPolicyKindDefault,
			5, 5, 20, 100,
			at.Add(-time.Hour), nil, "global", nil, int64(0), at.Add(-time.Hour),
		))
	mock.ExpectQuery("FROM account_share_quota_policies AS policy").
		WithArgs(service.AccountShareQuotaScopeOwner, int64(42), at).
		WillReturnRows(accountShareQuotaPolicyRows().AddRow(
			int64(9), service.AccountShareQuotaScopeOwner, int64(42), int64(4),
			service.AccountShareQuotaPolicyStatusActive,
			service.AccountShareQuotaPolicyKindManual,
			50, 50, 50, 500,
			at.Add(-48*time.Hour), at.Add(-time.Hour), "expired", int64(7), int64(7), at.Add(-48*time.Hour),
		))

	got, err := repo.ResolveAccountShareQuota(context.Background(), 42, at)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, service.AccountShareQuotaScopeGlobal, got.Source)
	require.Equal(t, int64(1), got.PolicyID)
	require.Equal(t, 5, got.Limits.MaxLiveRooms)
}

func TestAppendGrandfatherQuotaLocksOwnerAndDerivesCurrentBaseline(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	})
	repo := &accountShareModeRepository{db: db}
	now := time.Now().UTC()
	expiry := now.Add(30 * 24 * time.Hour)

	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs("account_share_quota_policy:global").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs("account_share_owner_quota:42").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT EXISTS \\(SELECT 1 FROM users WHERE id = \\$1\\)").
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("FROM account_share_quota_policies AS policy").
		WithArgs(service.AccountShareQuotaScopeOwner, int64(42)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("FROM account_share_quota_policies AS policy").
		WithArgs(service.AccountShareQuotaScopeGlobal, nil, sqlmock.AnyArg()).
		WillReturnRows(accountShareQuotaPolicyRows().AddRow(
			int64(1), service.AccountShareQuotaScopeGlobal, nil, int64(1),
			service.AccountShareQuotaPolicyStatusActive,
			service.AccountShareQuotaPolicyKindDefault,
			5, 5, 20, 100,
			now.Add(-time.Hour), nil, "global", nil, int64(0), now.Add(-time.Hour),
		))
	mock.ExpectQuery("FROM account_share_quota_policies AS policy").
		WithArgs(service.AccountShareQuotaScopeOwner, int64(42), sqlmock.AnyArg()).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT\\s+\\(\\s+SELECT COUNT\\(\\*\\)::int").
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"live_rooms",
			"room_creates_24_hours",
			"owner_room_accounts",
			"largest_room_accounts",
		}).AddRow(7, 6, 120, 25))
	mock.ExpectQuery("INSERT INTO account_share_quota_policies AS policy").
		WillReturnRows(accountShareQuotaPolicyRows().AddRow(
			int64(10), service.AccountShareQuotaScopeOwner, int64(42), int64(1),
			service.AccountShareQuotaPolicyStatusActive,
			service.AccountShareQuotaPolicyKindGrandfather,
			7, 6, 25, 120,
			now, expiry, "legacy baseline", int64(9), int64(9), now,
		))
	mock.ExpectCommit()

	got, err := repo.AppendAccountShareQuotaPolicyRevision(
		context.Background(),
		service.AppendAccountShareQuotaPolicyInput{
			ScopeType:         service.AccountShareQuotaScopeOwner,
			OwnerUserID:       ptrInt64ForRepositoryQuotaTest(42),
			ExpectedVersion:   0,
			Status:            service.AccountShareQuotaPolicyStatusActive,
			OverrideKind:      service.AccountShareQuotaPolicyKindGrandfather,
			EffectiveAt:       now,
			ExpiresAt:         &expiry,
			Reason:            "legacy baseline",
			ActorUserID:       9,
			DeriveGrandfather: true,
		},
	)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, 7, got.Limits.MaxLiveRooms)
	require.Equal(t, 25, got.Limits.MaxAccountsPerRoom)
	require.Equal(t, 120, got.Limits.MaxRoomAccountsPerOwner)
}

func TestAppendGrandfatherQuotaRejectsOwnerWithinEffectiveQuota(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	})
	repo := &accountShareModeRepository{db: db}
	now := time.Now().UTC()
	expiry := now.Add(30 * 24 * time.Hour)

	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs("account_share_quota_policy:global").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs("account_share_owner_quota:42").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT EXISTS \\(SELECT 1 FROM users WHERE id = \\$1\\)").
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("FROM account_share_quota_policies AS policy").
		WithArgs(service.AccountShareQuotaScopeOwner, int64(42)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("FROM account_share_quota_policies AS policy").
		WithArgs(service.AccountShareQuotaScopeGlobal, nil, sqlmock.AnyArg()).
		WillReturnRows(accountShareQuotaPolicyRows().AddRow(
			int64(1), service.AccountShareQuotaScopeGlobal, nil, int64(1),
			service.AccountShareQuotaPolicyStatusActive,
			service.AccountShareQuotaPolicyKindDefault,
			5, 5, 20, 100,
			now.Add(-time.Hour), nil, "global", nil, int64(0), now.Add(-time.Hour),
		))
	mock.ExpectQuery("FROM account_share_quota_policies AS policy").
		WithArgs(service.AccountShareQuotaScopeOwner, int64(42), sqlmock.AnyArg()).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT\\s+\\(\\s+SELECT COUNT\\(\\*\\)::int").
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"live_rooms",
			"room_creates_24_hours",
			"owner_room_accounts",
			"largest_room_accounts",
		}).AddRow(5, 4, 100, 20))
	mock.ExpectRollback()

	_, err = repo.AppendAccountShareQuotaPolicyRevision(
		context.Background(),
		service.AppendAccountShareQuotaPolicyInput{
			ScopeType:         service.AccountShareQuotaScopeOwner,
			OwnerUserID:       ptrInt64ForRepositoryQuotaTest(42),
			ExpectedVersion:   0,
			Status:            service.AccountShareQuotaPolicyStatusActive,
			OverrideKind:      service.AccountShareQuotaPolicyKindGrandfather,
			EffectiveAt:       now,
			ExpiresAt:         &expiry,
			Reason:            "legacy baseline",
			ActorUserID:       9,
			DeriveGrandfather: true,
		},
	)
	require.ErrorIs(t, err, service.ErrAccountShareQuotaNotCandidate)
}

func TestApplyGrandfatherCandidateLocksGlobalThenOwnerAndReturnsCompactPolicySummary(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	})
	repo := &accountShareModeRepository{db: db}
	now := time.Now().UTC()
	expiry := now.Add(30 * 24 * time.Hour)
	usage := service.AccountShareQuotaUsage{
		LiveRooms:           7,
		RoomCreates24Hours:  6,
		OwnerRoomAccounts:   120,
		LargestRoomAccounts: 25,
	}
	resolved := service.AccountShareResolvedQuota{
		Limits: service.AccountShareQuotaLimits{
			MaxLiveRooms:            5,
			MaxRoomCreates24Hours:   5,
			MaxAccountsPerRoom:      20,
			MaxRoomAccountsPerOwner: 100,
		},
		Source:        service.AccountShareQuotaScopeGlobal,
		PolicyID:      1,
		PolicyVersion: 1,
		OverrideKind:  service.AccountShareQuotaPolicyKindDefault,
	}
	fingerprint := service.BuildAccountShareGrandfatherCandidateFingerprint(42, 0, usage, resolved)

	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs("account_share_quota_policy:global").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs("account_share_owner_quota:42").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT EXISTS \\(SELECT 1 FROM users WHERE id = \\$1\\)").
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("FROM account_share_quota_policies AS policy").
		WithArgs(service.AccountShareQuotaScopeOwner, int64(42)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("FROM account_share_quota_policies AS policy").
		WithArgs(service.AccountShareQuotaScopeGlobal, nil, sqlmock.AnyArg()).
		WillReturnRows(accountShareQuotaPolicyRows().AddRow(
			int64(1), service.AccountShareQuotaScopeGlobal, nil, int64(1),
			service.AccountShareQuotaPolicyStatusActive,
			service.AccountShareQuotaPolicyKindDefault,
			5, 5, 20, 100,
			now.Add(-time.Hour), nil, "global", nil, int64(0), now.Add(-time.Hour),
		))
	mock.ExpectQuery("FROM account_share_quota_policies AS policy").
		WithArgs(service.AccountShareQuotaScopeOwner, int64(42), sqlmock.AnyArg()).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT\\s+\\(\\s+SELECT COUNT\\(\\*\\)::int").
		WithArgs(int64(42)).
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
	mock.ExpectQuery("INSERT INTO account_share_quota_policies").
		WillReturnRows(accountShareQuotaPolicyRows().AddRow(
			int64(10), service.AccountShareQuotaScopeOwner, int64(42), int64(1),
			service.AccountShareQuotaPolicyStatusActive,
			service.AccountShareQuotaPolicyKindGrandfather,
			7, 6, 25, 120,
			now, expiry, "legacy baseline", int64(9), int64(9), now,
		))
	mock.ExpectCommit()

	result, err := repo.ApplyAccountShareGrandfatherCandidate(
		context.Background(),
		service.ApplyAccountShareGrandfatherCandidateInput{
			Item: service.AccountShareGrandfatherCandidateItem{
				OwnerUserID:        42,
				ExpectedVersion:    0,
				PreviewUsage:       usage,
				PreviewFingerprint: fingerprint,
			},
			ExpiresAt:   expiry,
			Reason:      "legacy baseline",
			ActorUserID: 9,
		},
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "applied", result.Status)
	require.Equal(t, int64(10), result.PolicyID)
	require.Equal(t, int64(1), result.PolicyVersion)
	require.Equal(t, expiry, *result.ExpiresAt)
	require.Empty(t, result.ResultCode)
}

func TestAppendOwnerQuotaRejectsStaleExpectedVersionBeforeInsert(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	})
	repo := &accountShareModeRepository{db: db}
	now := time.Now().UTC()
	expiry := now.Add(24 * time.Hour)

	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs("account_share_owner_quota:42").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT EXISTS \\(SELECT 1 FROM users WHERE id = \\$1\\)").
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("FROM account_share_quota_policies AS policy").
		WithArgs(service.AccountShareQuotaScopeOwner, int64(42)).
		WillReturnRows(accountShareQuotaPolicyRows().AddRow(
			int64(8), service.AccountShareQuotaScopeOwner, int64(42), int64(2),
			service.AccountShareQuotaPolicyStatusActive,
			service.AccountShareQuotaPolicyKindManual,
			8, 8, 25, 150,
			now.Add(-time.Hour), expiry, "current", int64(7), int64(7), now.Add(-time.Hour),
		))
	mock.ExpectRollback()

	_, err = repo.AppendAccountShareQuotaPolicyRevision(
		context.Background(),
		service.AppendAccountShareQuotaPolicyInput{
			ScopeType:       service.AccountShareQuotaScopeOwner,
			OwnerUserID:     ptrInt64ForRepositoryQuotaTest(42),
			ExpectedVersion: 1,
			Status:          service.AccountShareQuotaPolicyStatusActive,
			OverrideKind:    service.AccountShareQuotaPolicyKindManual,
			Limits:          service.DefaultAccountShareQuotaLimits(),
			EffectiveAt:     now,
			ExpiresAt:       &expiry,
			Reason:          "stale update",
			ActorUserID:     9,
		},
	)
	require.ErrorIs(t, err, service.ErrAccountShareQuotaVersionConflict)
}

func TestListAccountShareGrandfatherCandidatesIncludesOwnerWithoutPolicy(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	})
	repo := &accountShareModeRepository{db: db}
	at := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery("FROM account_share_quota_policies AS policy").
		WithArgs(service.AccountShareQuotaScopeGlobal, nil, at).
		WillReturnRows(accountShareQuotaPolicyRows().AddRow(
			int64(1), service.AccountShareQuotaScopeGlobal, nil, int64(1),
			service.AccountShareQuotaPolicyStatusActive,
			service.AccountShareQuotaPolicyKindDefault,
			5, 5, 20, 100,
			at.Add(-time.Hour), nil, "global", nil, int64(0), at.Add(-time.Hour),
		))
	mock.ExpectQuery(
		`WHERE \(\s*current_policy\.id IS NULL\s*OR NOT \(`,
	).
		WithArgs(at, 5, 5, 20, 100, 0, 20).
		WillReturnRows(sqlmock.NewRows([]string{
			"owner_user_id",
			"live_rooms",
			"room_creates_24_hours",
			"owner_room_accounts",
			"largest_room_accounts",
			"latest_owner_version",
			"policy_id",
			"policy_version",
			"policy_status",
			"policy_kind",
			"max_live_rooms",
			"max_room_creates_24_hours",
			"max_accounts_per_room",
			"max_room_accounts_per_owner",
			"expires_at",
			"total",
		}).AddRow(
			int64(42), 6, 5, 100, 20, int64(0),
			nil, nil, nil, nil, nil, nil, nil, nil, nil,
			int64(1),
		))

	items, total, err := repo.ListAccountShareGrandfatherCandidates(
		context.Background(),
		at,
		pagination.PaginationParams{Page: 1, PageSize: 20},
	)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, items, 1)
	require.Equal(t, int64(42), items[0].OwnerUserID)
	require.Equal(t, int64(0), items[0].LatestOwnerVersion)
	require.Equal(t, service.AccountShareQuotaScopeGlobal, items[0].EffectiveQuota.Source)
	require.Equal(t, []string{"max_live_rooms"}, items[0].ExceededDimensions)
}

func TestListAccountShareGrandfatherCandidatesPreservesTotalOnEmptyPage(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	})
	repo := &accountShareModeRepository{db: db}
	at := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery("FROM account_share_quota_policies AS policy").
		WithArgs(service.AccountShareQuotaScopeGlobal, nil, at).
		WillReturnRows(accountShareQuotaPolicyRows().AddRow(
			int64(1), service.AccountShareQuotaScopeGlobal, nil, int64(1),
			service.AccountShareQuotaPolicyStatusActive,
			service.AccountShareQuotaPolicyKindDefault,
			5, 5, 20, 100,
			at.Add(-time.Hour), nil, "global", nil, int64(0), at.Add(-time.Hour),
		))
	mock.ExpectQuery(`FROM \(SELECT COUNT\(\*\)::bigint AS total FROM candidates\) totals`).
		WithArgs(at, 5, 5, 20, 100, 20, 20).
		WillReturnRows(sqlmock.NewRows([]string{
			"owner_user_id",
			"live_rooms",
			"room_creates_24_hours",
			"owner_room_accounts",
			"largest_room_accounts",
			"latest_owner_version",
			"policy_id",
			"policy_version",
			"policy_status",
			"policy_kind",
			"max_live_rooms",
			"max_room_creates_24_hours",
			"max_accounts_per_room",
			"max_room_accounts_per_owner",
			"expires_at",
			"total",
		}).AddRow(
			nil, nil, nil, nil, nil, nil,
			nil, nil, nil, nil, nil, nil, nil, nil, nil,
			int64(7),
		))

	items, total, err := repo.ListAccountShareGrandfatherCandidates(
		context.Background(),
		at,
		pagination.PaginationParams{Page: 2, PageSize: 20},
	)
	require.NoError(t, err)
	require.Empty(t, items)
	require.Equal(t, int64(7), total)
}

func accountShareQuotaPolicyRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
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
	})
}

func ptrInt64ForRepositoryQuotaTest(value int64) *int64 {
	return &value
}
