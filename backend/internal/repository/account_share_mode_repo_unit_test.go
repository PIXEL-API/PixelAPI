package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

func TestAccountShareModeRepositoryHasActiveOrQueuedMembershipForAPIKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(int64(7), int64(42), service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	exists, err := repo.HasActiveOrQueuedMembershipForAPIKey(context.Background(), 7, 42)
	if err != nil {
		t.Fatalf("HasActiveOrQueuedMembershipForAPIKey: %v", err)
	}
	if !exists {
		t.Fatalf("expected binding to exist")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryUpdateListingRequiresOwnerForUser(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT l\\.account_id, l\\.owner_user_id, l\\.seat_limit, l\\.per_user_concurrency, a\\.concurrency").
		WithArgs(int64(7), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "owner_user_id", "seat_limit", "per_user_concurrency", "concurrency", "proxy_id", "edit_session_id", "editing_by_user_id", "editing_expires_at"}))
	mock.ExpectRollback()

	status := service.AccountShareListingStatusPaused
	_, err = repo.UpdateListing(context.Background(), 42, false, 7, service.UpdateAccountShareListingInput{Status: &status})
	if !errors.Is(err, service.ErrAccountShareListingNotFound) {
		t.Fatalf("expected not found for non-owner listing, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryUpdateListingAllowsAdminWithoutOwnerFilter(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}
	updateErr := errors.New("stop after update")

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT l\\.account_id, l\\.owner_user_id, l\\.seat_limit, l\\.per_user_concurrency, a\\.concurrency").
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "owner_user_id", "seat_limit", "per_user_concurrency", "concurrency", "proxy_id", "edit_session_id", "editing_by_user_id", "editing_expires_at"}).
			AddRow(int64(99), int64(50), 2, 5, 20, nil, nil, nil, nil))
	mock.ExpectExec("UPDATE account_share_listings").
		WithArgs(service.AccountShareListingStatusPaused, int64(7)).
		WillReturnError(updateErr)
	mock.ExpectRollback()

	status := service.AccountShareListingStatusPaused
	_, err = repo.UpdateListing(context.Background(), 42, true, 7, service.UpdateAccountShareListingInput{Status: &status})
	if !errors.Is(err, updateErr) {
		t.Fatalf("expected update error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryUpdateListingSyncsAllowedModelsToAccount(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}
	commitErr := errors.New("stop after account sync")
	models := []string{"gpt-5.5", "gpt-5.4"}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT l\\.account_id, l\\.owner_user_id, l\\.seat_limit, l\\.per_user_concurrency, a\\.concurrency").
		WithArgs(int64(7), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "owner_user_id", "seat_limit", "per_user_concurrency", "concurrency", "proxy_id", "edit_session_id", "editing_by_user_id", "editing_expires_at"}).
			AddRow(int64(99), int64(42), 2, 5, 20, nil, nil, nil, nil))
	mock.ExpectExec("UPDATE account_share_listings").
		WithArgs(`["gpt-5.5","gpt-5.4"]`, int64(7), int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE accounts").
		WithArgs(`{"gpt-5.4":"gpt-5.4","gpt-5.5":"gpt-5.5"}`, int64(99), int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO scheduler_outbox").
		WithArgs(service.SchedulerOutboxEventAccountChanged, sqlmock.AnyArg(), nil, nil, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit().WillReturnError(commitErr)

	_, err = repo.UpdateListing(context.Background(), 42, false, 7, service.UpdateAccountShareListingInput{AllowedModels: &models})
	if !errors.Is(err, commitErr) {
		t.Fatalf("expected commit sentinel error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryBeginListingEditRejectsActiveSeatsForOwner(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT l\\.owner_user_id, l\\.edit_session_id, l\\.editing_by_user_id, l\\.editing_expires_at").
		WithArgs(int64(7), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"owner_user_id", "edit_session_id", "editing_by_user_id", "editing_expires_at"}).
			AddRow(int64(42), nil, nil, nil))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\)::int").
		WithArgs(int64(7), service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectRollback()

	_, err = repo.BeginListingEdit(context.Background(), 42, false, 7, service.BeginAccountShareListingEditInput{
		SessionID: "edit-session",
		Expires:   time.Now().UTC().Add(10 * time.Minute),
	})
	if !errors.Is(err, service.ErrAccountShareListingInUse) {
		t.Fatalf("expected active seat edit rejection, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryBeginListingEditAllowsOwnerWithoutActiveSeats(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	now := time.Now().UTC()
	expires := now.Add(10 * time.Minute)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT l\\.owner_user_id, l\\.edit_session_id, l\\.editing_by_user_id, l\\.editing_expires_at").
		WithArgs(int64(7), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"owner_user_id", "edit_session_id", "editing_by_user_id", "editing_expires_at"}).
			AddRow(int64(42), nil, nil, nil))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\)::int").
		WithArgs(int64(7), service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec("SET edit_session_id = \\$1::varchar").
		WithArgs("edit-session", int64(42), expires, int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery("SELECT\\s+l\\.id").
		WithArgs(int64(42), int64(7)).
		WillReturnRows(accountShareListingRows(7, 99, 42, "edit-session", expires))

	listing, err := repo.BeginListingEdit(context.Background(), 42, false, 7, service.BeginAccountShareListingEditInput{
		SessionID: "edit-session",
		Expires:   expires,
	})
	if err != nil {
		t.Fatalf("expected begin edit to succeed, got %v", err)
	}
	if listing.EditSessionID != "edit-session" || !listing.EditingMine {
		t.Fatalf("unexpected edit session fields: session=%q mine=%v", listing.EditSessionID, listing.EditingMine)
	}
	if listing.ActiveSeats != 0 {
		t.Fatalf("expected no active seats, got %d", listing.ActiveSeats)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryJoinListingRejectsActiveEditSession(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT l\\.account_id, l\\.owner_user_id, l\\.status, l\\.seat_limit").
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{
			"account_id",
			"owner_user_id",
			"status",
			"seat_limit",
			"hourly_rate",
			"hourly_fee_waiver_minimum",
			"min_balance_required",
			"edit_session_id",
			"editing_expires_at",
		}).AddRow(int64(99), int64(50), service.AccountShareListingStatusActive, 2, 0.2, 0, 1, "edit-session", time.Now().UTC().Add(10*time.Minute)))
	mock.ExpectRollback()

	_, err = repo.JoinListing(context.Background(), 42, 12, 7, 0)
	if !errors.Is(err, service.ErrAccountShareListingEditing) {
		t.Fatalf("expected editing listing rejection, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryJoinListingOwnerSelfUseHasNoSeatPrepay(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	listingID := int64(7)
	accountID := int64(99)
	ownerUserID := int64(42)
	consumerUserID := ownerUserID
	apiKeyID := int64(12)
	membershipID := int64(700)
	idleTimeoutMinutes := 10
	now := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT l\\.account_id, l\\.owner_user_id, l\\.status, l\\.seat_limit").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{
			"account_id",
			"owner_user_id",
			"status",
			"seat_limit",
			"hourly_rate",
			"hourly_fee_waiver_minimum",
			"min_balance_required",
			"edit_session_id",
			"editing_expires_at",
		}).AddRow(accountID, ownerUserID, service.AccountShareListingStatusActive, 2, 1.5, 0.5, 100, nil, nil))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(accountID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery("SELECT balance").
		WithArgs(consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(0.01))
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(consumerUserID, apiKeyID, listingID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()))
	mock.ExpectExec("UPDATE account_share_memberships m").
		WithArgs(
			service.AccountShareMembershipStatusEnded,
			sqlmock.AnyArg(),
			service.AccountShareMembershipEndReasonUnavailable,
			consumerUserID,
			apiKeyID,
			service.AccountShareMembershipStatusQueued,
			service.AccountShareListingStatusDisabled,
		).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\)::int").
		WithArgs(consumerUserID, apiKeyID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued).
		WillReturnRows(sqlmock.NewRows([]string{"count", "max", "active"}).AddRow(0, 0, false))
	mock.ExpectQuery("INSERT INTO account_share_memberships").
		WithArgs(
			listingID,
			accountID,
			consumerUserID,
			apiKeyID,
			service.AccountShareMembershipStatusActive,
			1,
			0.0,
			0.0,
			idleTimeoutMinutes,
			sqlmock.AnyArg(),
			nil,
			nil,
			nil,
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"listing_id",
			"account_id",
			"consumer_user_id",
			"api_key_id",
			"status",
			"queue_rank",
			"hourly_rate_snapshot",
			"hourly_fee_waiver_minimum_snapshot",
			"idle_timeout_minutes",
			"joined_at",
			"last_request_at",
			"ended_at",
			"ended_reason",
			"paid_until",
			"billed_until",
			"waiver_window_started_at",
			"waiver_window_usage_amount",
			"waiver_window_request_count",
			"waiver_window_last_request_at",
			"dispatch_failed_at",
			"dispatch_cooldown_until",
			"created_at",
			"updated_at",
		}).AddRow(
			membershipID,
			listingID,
			accountID,
			consumerUserID,
			apiKeyID,
			service.AccountShareMembershipStatusActive,
			1,
			0.0,
			0.0,
			idleTimeoutMinutes,
			now,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			0,
			int64(0),
			nil,
			nil,
			nil,
			now,
			now,
		))
	mock.ExpectCommit()

	membership, err := repo.JoinListing(context.Background(), consumerUserID, apiKeyID, listingID, idleTimeoutMinutes)
	if err != nil {
		t.Fatalf("JoinListing owner self-use failed: %v", err)
	}
	if membership.OwnerUserID != ownerUserID {
		t.Fatalf("owner user id = %d, want %d", membership.OwnerUserID, ownerUserID)
	}
	if membership.HourlyRateSnapshot != 0 {
		t.Fatalf("hourly rate snapshot = %v, want 0", membership.HourlyRateSnapshot)
	}
	if membership.HourlyFeeWaiverMinimumSnapshot != 0 {
		t.Fatalf("hourly waiver snapshot = %v, want 0", membership.HourlyFeeWaiverMinimumSnapshot)
	}
	if membership.PaidUntil != nil {
		t.Fatalf("paid until = %v, want nil", membership.PaidUntil)
	}
	if membership.BilledUntil != nil {
		t.Fatalf("billed until = %v, want nil", membership.BilledUntil)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryJoinListingQueuesBehindExistingReservation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	listingID := int64(8)
	accountID := int64(100)
	ownerUserID := int64(50)
	consumerUserID := int64(42)
	apiKeyID := int64(12)
	membershipID := int64(701)
	idleTimeoutMinutes := 10
	now := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT l\\.account_id, l\\.owner_user_id, l\\.status, l\\.seat_limit").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{
			"account_id",
			"owner_user_id",
			"status",
			"seat_limit",
			"hourly_rate",
			"hourly_fee_waiver_minimum",
			"min_balance_required",
			"edit_session_id",
			"editing_expires_at",
		}).AddRow(accountID, ownerUserID, service.AccountShareListingStatusActive, 1, 0.6, 0.1, 1, nil, nil))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(accountID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery("SELECT balance").
		WithArgs(consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(1.005))
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(consumerUserID, apiKeyID, listingID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()))
	mock.ExpectExec("UPDATE account_share_memberships m").
		WithArgs(
			service.AccountShareMembershipStatusEnded,
			sqlmock.AnyArg(),
			service.AccountShareMembershipEndReasonUnavailable,
			consumerUserID,
			apiKeyID,
			service.AccountShareMembershipStatusQueued,
			service.AccountShareListingStatusDisabled,
		).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\)::int").
		WithArgs(consumerUserID, apiKeyID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued).
		WillReturnRows(sqlmock.NewRows([]string{"count", "max", "active"}).AddRow(1, 1, false))
	mock.ExpectQuery("INSERT INTO account_share_memberships").
		WithArgs(
			listingID,
			accountID,
			consumerUserID,
			apiKeyID,
			service.AccountShareMembershipStatusQueued,
			2,
			0.6,
			0.1,
			idleTimeoutMinutes,
			sqlmock.AnyArg(),
			nil,
			nil,
			nil,
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"listing_id",
			"account_id",
			"consumer_user_id",
			"api_key_id",
			"status",
			"queue_rank",
			"hourly_rate_snapshot",
			"hourly_fee_waiver_minimum_snapshot",
			"idle_timeout_minutes",
			"joined_at",
			"last_request_at",
			"ended_at",
			"ended_reason",
			"paid_until",
			"billed_until",
			"waiver_window_started_at",
			"waiver_window_usage_amount",
			"waiver_window_request_count",
			"waiver_window_last_request_at",
			"dispatch_failed_at",
			"dispatch_cooldown_until",
			"created_at",
			"updated_at",
		}).AddRow(
			membershipID,
			listingID,
			accountID,
			consumerUserID,
			apiKeyID,
			service.AccountShareMembershipStatusQueued,
			2,
			0.6,
			0.1,
			idleTimeoutMinutes,
			now,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			0,
			int64(0),
			nil,
			nil,
			nil,
			now,
			now,
		))
	mock.ExpectCommit()

	membership, err := repo.JoinListing(context.Background(), consumerUserID, apiKeyID, listingID, idleTimeoutMinutes)
	if err != nil {
		t.Fatalf("JoinListing queued reservation failed: %v", err)
	}
	if membership.Status != service.AccountShareMembershipStatusQueued {
		t.Fatalf("membership status = %q, want %q", membership.Status, service.AccountShareMembershipStatusQueued)
	}
	if membership.QueueRank != 2 {
		t.Fatalf("queue rank = %d, want 2", membership.QueueRank)
	}
	if membership.PaidUntil != nil {
		t.Fatalf("paid until = %v, want nil for queued reservation", membership.PaidUntil)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryJoinListingActivatesAfterStaleQueuedCleanup(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	listingID := int64(9)
	accountID := int64(101)
	ownerUserID := int64(50)
	consumerUserID := int64(42)
	apiKeyID := int64(12)
	membershipID := int64(702)
	idleTimeoutMinutes := 10
	now := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT l\\.account_id, l\\.owner_user_id, l\\.status, l\\.seat_limit").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{
			"account_id",
			"owner_user_id",
			"status",
			"seat_limit",
			"hourly_rate",
			"hourly_fee_waiver_minimum",
			"min_balance_required",
			"edit_session_id",
			"editing_expires_at",
		}).AddRow(accountID, ownerUserID, service.AccountShareListingStatusActive, 2, 0.0, 0.0, 1, nil, nil))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(accountID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery("SELECT balance").
		WithArgs(consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(10.0))
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(consumerUserID, apiKeyID, listingID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()))
	mock.ExpectExec("UPDATE account_share_memberships m").
		WithArgs(
			service.AccountShareMembershipStatusEnded,
			sqlmock.AnyArg(),
			service.AccountShareMembershipEndReasonUnavailable,
			consumerUserID,
			apiKeyID,
			service.AccountShareMembershipStatusQueued,
			service.AccountShareListingStatusDisabled,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\)::int").
		WithArgs(consumerUserID, apiKeyID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued).
		WillReturnRows(sqlmock.NewRows([]string{"count", "max", "active"}).AddRow(0, 0, false))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\)::int").
		WithArgs(listingID, service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"active_seats"}).AddRow(0))
	mock.ExpectQuery("INSERT INTO account_share_memberships").
		WithArgs(
			listingID,
			accountID,
			consumerUserID,
			apiKeyID,
			service.AccountShareMembershipStatusActive,
			1,
			0.0,
			0.0,
			idleTimeoutMinutes,
			sqlmock.AnyArg(),
			nil,
			nil,
			nil,
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"listing_id",
			"account_id",
			"consumer_user_id",
			"api_key_id",
			"status",
			"queue_rank",
			"hourly_rate_snapshot",
			"hourly_fee_waiver_minimum_snapshot",
			"idle_timeout_minutes",
			"joined_at",
			"last_request_at",
			"ended_at",
			"ended_reason",
			"paid_until",
			"billed_until",
			"waiver_window_started_at",
			"waiver_window_usage_amount",
			"waiver_window_request_count",
			"waiver_window_last_request_at",
			"dispatch_failed_at",
			"dispatch_cooldown_until",
			"created_at",
			"updated_at",
		}).AddRow(
			membershipID,
			listingID,
			accountID,
			consumerUserID,
			apiKeyID,
			service.AccountShareMembershipStatusActive,
			1,
			0.0,
			0.0,
			idleTimeoutMinutes,
			now,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			0,
			int64(0),
			nil,
			nil,
			nil,
			now,
			now,
		))
	mock.ExpectCommit()

	membership, err := repo.JoinListing(context.Background(), consumerUserID, apiKeyID, listingID, idleTimeoutMinutes)
	if err != nil {
		t.Fatalf("JoinListing after stale cleanup failed: %v", err)
	}
	if membership.Status != service.AccountShareMembershipStatusActive {
		t.Fatalf("membership status = %q, want %q", membership.Status, service.AccountShareMembershipStatusActive)
	}
	if membership.QueueRank != 1 {
		t.Fatalf("queue rank = %d, want 1", membership.QueueRank)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareCodexQuotaProtectedSQLParenthesizesCaseExpressions(t *testing.T) {
	sql := accountShareCodexQuotaProtectedSQL("codex_5h_used_percent", "codex_5h_reset_at", "codex_5h_limit_percent", "$2")
	required := []string{
		"COALESCE((CASE",
		") >= (CASE",
		"CASE WHEN (CASE",
		"AND (CASE",
		">= 1.0",
		"<= 100.0",
		"ELSE 100.0",
	}
	for _, fragment := range required {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("generated SQL missing %q: %s", fragment, sql)
		}
	}
	if strings.Contains(sql, "END >= CASE") {
		t.Fatalf("generated SQL must not compare unparenthesized CASE expressions: %s", sql)
	}
	if strings.Contains(sql, "<= 1.0") || strings.Contains(sql, "ELSE 1.0") {
		t.Fatalf("generated SQL must not collapse max/default quota limits to the minimum: %s", sql)
	}
}

func TestAccountShareModeRepositorySeatBillingUsesSettlementRefForLedgers(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	now := time.Date(2026, 6, 13, 11, 30, 0, 0, time.UTC)
	joinedAt := now.Add(-2 * time.Minute)
	billedUntil := now.Add(-1 * time.Minute)
	paidUntil := now
	membershipID := int64(70)
	settlementID := int64(7001)
	ownerUserID := int64(2284)
	consumerUserID := int64(4866)
	accountID := int64(417583)
	listingID := int64(10)
	apiKeyID := int64(20150)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(membershipID, service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			membershipID,
			listingID,
			accountID,
			ownerUserID,
			consumerUserID,
			apiKeyID,
			service.AccountShareMembershipStatusActive,
			1,
			0.2,
			0,
			0,
			joinedAt,
			nil,
			nil,
			nil,
			paidUntil,
			billedUntil,
			billedUntil,
			0,
			int64(0),
			nil,
			nil,
			nil,
			joinedAt,
			joinedAt,
		))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(accountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(listingID, accountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	expectAccountShareBillingUserLock(mock, consumerUserID)
	mock.ExpectQuery("SELECT id, scope_type, scope_id, platform, owner_share_ratio::text, invite_share_ratio::text, version, enabled").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "scope_type", "scope_id", "platform", "owner_share_ratio", "invite_share_ratio",
			"version", "enabled", "effective_at", "created_by_admin_id", "created_at", "updated_at", "deleted_at",
		}).AddRow(1, service.AccountSharePolicyScopeGlobal, nil, nil, "0.9", "0", 1, true, joinedAt, 1, joinedAt, joinedAt, nil))
	mock.ExpectQuery("INSERT INTO account_share_mode_settlement_entries").
		WithArgs(
			membershipID,
			listingID,
			accountID,
			ownerUserID,
			consumerUserID,
			apiKeyID,
			"0.0033333333",
			"0.0030000000",
			"0.0003333333",
			"0.20000000",
			int64(1),
			1,
			"0.90000000",
			nil,
			nil,
			nil,
			"0.00000000",
			"0.0000000000",
			"0.10000000",
			60000,
			accountShareSeatSettlementTypeCharge,
			billedUntil,
			paidUntil,
			"0.0000000000",
			"0.00000000",
			"0.0000000000",
			"0.0000000000",
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(settlementID))
	mock.ExpectQuery("UPDATE users").
		WithArgs("0.0030000000", ownerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(100.003))
	mock.ExpectExec("INSERT INTO user_balance_ledger").
		WithArgs(ownerUserID, "credit", "0.0030000000", accountShareSeatIncomeReason, accountShareModeSettlementRefType, settlementID, "100.0030000000", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT balance").
		WithArgs(consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(10.0))
	mock.ExpectExec("UPDATE users").
		WithArgs("9.9966666667", consumerUserID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO user_balance_ledger").
		WithArgs(consumerUserID, "debit", "0.0033333333", accountShareSeatPrepayReason, accountShareModeSettlementRefType, settlementID, "9.9966666667", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("UPDATE account_share_memberships").
		WithArgs(paidUntil.Add(time.Minute), paidUntil, membershipID).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))
	mock.ExpectCommit()

	result, err := repo.processSeatBillingMembership(context.Background(), membershipID, now)
	if err != nil {
		t.Fatalf("processSeatBillingMembership failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected billing result")
	}
	if got := strings.Trim(strings.Join(int64sToStrings(result.DebitUserIDs), ","), ","); got != "4866" {
		t.Fatalf("debit users = %q", got)
	}
	if got := strings.Trim(strings.Join(int64sToStrings(result.CreditUserIDs), ","), ","); got != "2284" {
		t.Fatalf("credit users = %q", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositorySeatBillingUsesUniquePrepayRefBeforeWaiverWindowSettles(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	now := time.Date(2026, 6, 24, 3, 49, 57, 0, time.UTC)
	joinedAt := now.Add(-2 * time.Minute)
	billedUntil := joinedAt
	paidUntil := now
	newPaidUntil := paidUntil.Add(time.Minute)
	membershipID := int64(70)
	ownerUserID := int64(2284)
	consumerUserID := int64(4866)
	accountID := int64(417583)
	listingID := int64(10)
	apiKeyID := int64(20150)
	expectedPrepayRefID := accountShareSeatPrepayRefID(membershipID, newPaidUntil)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(membershipID, service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			membershipID,
			listingID,
			accountID,
			ownerUserID,
			consumerUserID,
			apiKeyID,
			service.AccountShareMembershipStatusActive,
			1,
			0.2,
			0.12,
			0,
			joinedAt,
			nil,
			nil,
			nil,
			paidUntil,
			billedUntil,
			billedUntil,
			0,
			int64(0),
			nil,
			nil,
			nil,
			joinedAt,
			joinedAt,
		))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(accountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(listingID, accountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	expectAccountShareBillingUserLock(mock, consumerUserID)
	mock.ExpectQuery("SELECT balance").
		WithArgs(consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(10.0))
	mock.ExpectExec("UPDATE users").
		WithArgs("9.9966666667", consumerUserID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO user_balance_ledger").
		WithArgs(consumerUserID, "debit", "0.0033333333", accountShareSeatPrepayReason, accountShareSeatPrepayRefType, expectedPrepayRefID, "9.9966666667", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("UPDATE account_share_memberships").
		WithArgs(newPaidUntil, nil, membershipID).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))
	mock.ExpectCommit()

	result, err := repo.processSeatBillingMembership(context.Background(), membershipID, now)
	if err != nil {
		t.Fatalf("processSeatBillingMembership failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected billing result")
	}
	if got := strings.Trim(strings.Join(int64sToStrings(result.DebitUserIDs), ","), ","); got != "4866" {
		t.Fatalf("debit users = %q", got)
	}
	if got := strings.Trim(strings.Join(int64sToStrings(result.CreditUserIDs), ","), ","); got != "" {
		t.Fatalf("credit users = %q", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositorySeatBillingRollsBackWhenPrepayLedgerIsSkipped(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	now := time.Date(2026, 6, 24, 3, 49, 57, 0, time.UTC)
	joinedAt := now.Add(-2 * time.Minute)
	billedUntil := joinedAt
	paidUntil := now
	newPaidUntil := paidUntil.Add(time.Minute)
	membershipID := int64(70)
	ownerUserID := int64(2284)
	consumerUserID := int64(4866)
	accountID := int64(417583)
	listingID := int64(10)
	apiKeyID := int64(20150)
	expectedPrepayRefID := accountShareSeatPrepayRefID(membershipID, newPaidUntil)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(membershipID, service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			membershipID,
			listingID,
			accountID,
			ownerUserID,
			consumerUserID,
			apiKeyID,
			service.AccountShareMembershipStatusActive,
			1,
			0.2,
			0.12,
			0,
			joinedAt,
			nil,
			nil,
			nil,
			paidUntil,
			billedUntil,
			billedUntil,
			0.13,
			int64(2),
			paidUntil.Add(-time.Second),
			nil,
			nil,
			joinedAt,
			joinedAt,
		))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(accountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(listingID, accountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	expectAccountShareBillingUserLock(mock, consumerUserID)
	mock.ExpectQuery("SELECT balance").
		WithArgs(consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(10.0))
	mock.ExpectExec("UPDATE users").
		WithArgs("9.9966666667", consumerUserID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO user_balance_ledger").
		WithArgs(consumerUserID, "debit", "0.0033333333", accountShareSeatPrepayReason, accountShareSeatPrepayRefType, expectedPrepayRefID, "9.9966666667", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	result, err := repo.processSeatBillingMembership(context.Background(), membershipID, now)
	if err == nil {
		t.Fatal("expected processSeatBillingMembership to fail when prepay ledger is skipped")
	}
	if !strings.Contains(err.Error(), "user balance ledger insert skipped") {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("result = %#v, want nil", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryRefundUnusedSeatPrepayUsesSettlementRef(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	endedAt := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	paidUntil := endedAt.Add(30 * time.Minute)
	membership := &service.AccountShareMembership{
		ID:                 18012,
		ListingID:          510,
		AccountID:          405606,
		OwnerUserID:        7001,
		ConsumerUserID:     5926,
		APIKeyID:           15007,
		HourlyRateSnapshot: 0.2,
		PaidUntil:          &paidUntil,
	}
	settlementID := int64(991234)

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO account_share_mode_settlement_entries").
		WithArgs(
			membership.ID,
			membership.ListingID,
			membership.AccountID,
			membership.OwnerUserID,
			membership.ConsumerUserID,
			membership.APIKeyID,
			"0.0000000000",
			"0.0000000000",
			"0.0000000000",
			"0.20000000",
			nil,
			0,
			"0.00000000",
			nil,
			nil,
			nil,
			"0.00000000",
			"0.0000000000",
			"0.00000000",
			1800000,
			accountShareSeatSettlementTypeRefund,
			endedAt,
			paidUntil,
			"0.1000000000",
			"0.00000000",
			"0.0000000000",
			"0.0000000000",
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(settlementID))
	mock.ExpectQuery("UPDATE users").
		WithArgs("0.1000000000", membership.ConsumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(12.1))
	mock.ExpectExec("INSERT INTO user_balance_ledger").
		WithArgs(membership.ConsumerUserID, "credit", "0.1000000000", accountShareSeatRefundReason, accountShareModeSettlementRefType, settlementID, "12.1000000000", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := repo.refundUnusedSeatPrepayInTx(context.Background(), tx, membership, endedAt); err != nil {
		_ = tx.Rollback()
		t.Fatalf("refundUnusedSeatPrepayInTx failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryListListingsReadsWaiverProgressFromMainQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	membershipID := int64(18012)
	viewerUserID := int64(5926)
	ownerUserID := int64(7001)
	joinedAt := time.Now().UTC().Add(-30 * time.Minute)
	lastRequestAt := joinedAt.Add(20 * time.Minute)
	mock.ExpectQuery("SELECT\\s+l\\.id").
		WithArgs(viewerUserID, 21, 0).
		WillReturnRows(accountShareListingRows(510, 405606, ownerUserID, "", time.Time{}, func(row *accountShareListingRowData) {
			row.HourlyRate = 0.2
			row.HourlyFeeWaiverMinimum = 0.12
			row.CurrentMembershipID = membershipID
			row.CurrentConsumerUserID = viewerUserID
			row.CurrentAPIKeyID = 15007
			row.CurrentAPIKeyName = "coding-key"
			row.CurrentJoinedAt = joinedAt
			row.CurrentLastRequestAt = lastRequestAt
			row.CurrentWaiverWindowStartedAt = joinedAt
			row.CurrentWaiverWindowUsageAmount = "0.0800000000"
			row.CurrentWaiverWindowRequestCount = int64(3)
			row.CurrentWaiverWindowLastRequestAt = lastRequestAt
		}))

	listings, _, err := repo.ListListings(context.Background(), viewerUserID, service.AccountShareListingFilters{SkipTotal: true}, pagination.PaginationParams{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListListings failed: %v", err)
	}
	if len(listings) != 1 {
		t.Fatalf("listings length = %d, want 1", len(listings))
	}
	progress := listings[0].CurrentWaiverProgress
	if listings[0].CurrentAPIKeyName != "coding-key" {
		t.Fatalf("current api key name = %q, want coding-key", listings[0].CurrentAPIKeyName)
	}
	if progress == nil {
		t.Fatal("expected waiver progress")
	}
	if !progress.Enabled {
		t.Fatal("expected waiver progress enabled")
	}
	if progress.Status != service.AccountShareWaiverProgressStatusMet {
		t.Fatalf("status = %q, want %q", progress.Status, service.AccountShareWaiverProgressStatusMet)
	}
	if progress.UsageAmount != 0.08 {
		t.Fatalf("usage amount = %v, want 0.08", progress.UsageAmount)
	}
	if progress.RequiredAmount <= 0 || progress.RequiredAmount > 0.12 {
		t.Fatalf("required amount = %v, want within (0, 0.12]", progress.RequiredAmount)
	}
	if progress.ProgressPercent <= 0 || progress.ProgressPercent > 100 {
		t.Fatalf("progress percent = %v, want within (0, 100]", progress.ProgressPercent)
	}
	if progress.RequestCount != 3 {
		t.Fatalf("request count = %d, want 3", progress.RequestCount)
	}
	if progress.LastRequestAt == nil || !progress.LastRequestAt.Equal(lastRequestAt) {
		t.Fatalf("last request at = %v, want %v", progress.LastRequestAt, lastRequestAt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryListListingsSkipsOwnerSelfUseWaiverProgress(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	viewerUserID := int64(7001)
	joinedAt := time.Now().UTC().Add(-30 * time.Minute)
	mock.ExpectQuery("SELECT\\s+l\\.id").
		WithArgs(viewerUserID, 21, 0).
		WillReturnRows(accountShareListingRows(510, 405606, viewerUserID, "", time.Time{}, func(row *accountShareListingRowData) {
			row.HourlyRate = 0.2
			row.HourlyFeeWaiverMinimum = 0.12
			row.CurrentMembershipID = 18012
			row.CurrentConsumerUserID = viewerUserID
			row.CurrentAPIKeyID = 15007
			row.CurrentJoinedAt = joinedAt
			row.CurrentWaiverWindowStartedAt = joinedAt
			row.CurrentWaiverWindowUsageAmount = "0.0800000000"
			row.CurrentWaiverWindowRequestCount = int64(3)
			row.CurrentWaiverWindowLastRequestAt = joinedAt.Add(20 * time.Minute)
		}))

	listings, _, err := repo.ListListings(context.Background(), viewerUserID, service.AccountShareListingFilters{SkipTotal: true}, pagination.PaginationParams{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListListings failed: %v", err)
	}
	if len(listings) != 1 {
		t.Fatalf("listings length = %d, want 1", len(listings))
	}
	if listings[0].CurrentWaiverProgress != nil {
		t.Fatalf("expected owner self-use progress to be skipped, got %+v", listings[0].CurrentWaiverProgress)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeSettlementUpdatesWaiverProgressCacheAfterInsert(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	usageLogID := int64(99001)
	membershipID := int64(18012)
	windowStart := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	occurredAt := windowStart.Add(30 * time.Second)
	snapshot := &service.AccountShareModeBillingSnapshot{
		MembershipID:       membershipID,
		ListingID:          510,
		AccountID:          405606,
		OwnerUserID:        7001,
		ConsumerUserID:     5926,
		APIKeyID:           15007,
		BaseCharge:         0.02,
		HourlyCharge:       0.04,
		TotalCharge:        0.06,
		RateMultiplier:     1,
		HourlyRate:         0.2,
		OwnerShareRatio:    0,
		PlatformShareRatio: 1,
		DurationMs:         60000,
	}
	cmd := &service.UsageBillingCommand{
		RequestID:                  "req-waiver-cache",
		APIKeyID:                   snapshot.APIKeyID,
		AccountShareModeSettlement: snapshot,
		UsageLog:                   &service.UsageLog{CreatedAt: occurredAt},
	}
	periodStartedAt, periodEndedAt := accountShareModeUsageRequestPeriod(cmd, snapshot)

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO account_share_mode_settlement_entries").
		WithArgs(
			nullablePositiveInt64(usageLogID),
			snapshot.MembershipID,
			snapshot.ListingID,
			snapshot.AccountID,
			snapshot.OwnerUserID,
			snapshot.ConsumerUserID,
			snapshot.APIKeyID,
			"0.0200000000",
			"0.0400000000",
			"0.0600000000",
			"0.0000000000",
			"0.0600000000",
			"1.0000",
			"0.20000000",
			nil,
			0,
			"0.00000000",
			nil,
			nil,
			nil,
			"0.00000000",
			"0.0000000000",
			"1.00000000",
			snapshot.DurationMs,
			periodStartedAt,
			periodEndedAt,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(700100)))
	mock.ExpectQuery("SELECT joined_at").
		WithArgs(membershipID, service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"joined_at"}).AddRow(windowStart))
	mock.ExpectExec("UPDATE account_share_memberships").
		WithArgs(membershipID, windowStart, "0.0300000000", periodEndedAt, service.AccountShareMembershipStatusActive).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	result := &service.UsageBillingApplyResult{}
	if err := applyAccountShareModeSettlement(context.Background(), tx, cmd, usageLogID, result); err != nil {
		_ = tx.Rollback()
		t.Fatalf("applyAccountShareModeSettlement failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if len(result.BalanceCreditUserIDs) != 0 {
		t.Fatalf("credit user ids = %v, want none", result.BalanceCreditUserIDs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeSettlementAdvancesWaiverProgressCacheByFixedJoinedWindow(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	usageLogID := int64(99003)
	membershipID := int64(18012)
	joinedAt := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	secondWindowStart := joinedAt.Add(time.Hour)
	occurredAt := secondWindowStart.Add(2 * time.Minute)
	snapshot := &service.AccountShareModeBillingSnapshot{
		MembershipID:       membershipID,
		ListingID:          510,
		AccountID:          405606,
		OwnerUserID:        7001,
		ConsumerUserID:     5926,
		APIKeyID:           15007,
		BaseCharge:         0.08,
		TotalCharge:        0.08,
		RateMultiplier:     1,
		HourlyRate:         0.2,
		OwnerShareRatio:    0,
		PlatformShareRatio: 1,
		DurationMs:         60000,
	}
	cmd := &service.UsageBillingCommand{
		RequestID:                  "req-waiver-cache-next-window",
		APIKeyID:                   snapshot.APIKeyID,
		AccountShareModeSettlement: snapshot,
		UsageLog:                   &service.UsageLog{CreatedAt: occurredAt},
	}
	periodStartedAt, periodEndedAt := accountShareModeUsageRequestPeriod(cmd, snapshot)

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO account_share_mode_settlement_entries").
		WithArgs(
			nullablePositiveInt64(usageLogID),
			snapshot.MembershipID,
			snapshot.ListingID,
			snapshot.AccountID,
			snapshot.OwnerUserID,
			snapshot.ConsumerUserID,
			snapshot.APIKeyID,
			"0.0800000000",
			"0.0000000000",
			"0.0800000000",
			"0.0000000000",
			"0.0800000000",
			"1.0000",
			"0.20000000",
			nil,
			0,
			"0.00000000",
			nil,
			nil,
			nil,
			"0.00000000",
			"0.0000000000",
			"1.00000000",
			snapshot.DurationMs,
			periodStartedAt,
			periodEndedAt,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(700101)))
	mock.ExpectQuery("SELECT joined_at").
		WithArgs(membershipID, service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"joined_at"}).AddRow(joinedAt))
	mock.ExpectExec("UPDATE account_share_memberships").
		WithArgs(membershipID, secondWindowStart, "0.0800000000", periodEndedAt, service.AccountShareMembershipStatusActive).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	result := &service.UsageBillingApplyResult{}
	if err := applyAccountShareModeSettlement(context.Background(), tx, cmd, usageLogID, result); err != nil {
		_ = tx.Rollback()
		t.Fatalf("applyAccountShareModeSettlement failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeWindowOverlapChargeSplitsCrossWindowRequest(t *testing.T) {
	totalCharge := decimal.RequireFromString("0.3000000000")
	windowStart := time.Date(2026, 7, 1, 4, 51, 5, 0, time.UTC)
	windowEnd := windowStart.Add(time.Hour)
	requestStart := windowEnd.Add(-10 * time.Second)
	requestEnd := windowEnd.Add(5 * time.Minute)

	usageInPreviousWindow := accountShareModeWindowOverlapCharge(totalCharge, requestStart, requestEnd, windowStart, windowEnd)
	if got, want := usageInPreviousWindow.StringFixed(10), "0.0096774194"; got != want {
		t.Fatalf("previous window usage = %s, want %s", got, want)
	}

	nextWindowUsage := accountShareModeWindowOverlapCharge(totalCharge, requestStart, requestEnd, windowEnd, windowEnd.Add(time.Hour))
	if got, want := nextWindowUsage.StringFixed(10), "0.2903225806"; got != want {
		t.Fatalf("next window usage = %s, want %s", got, want)
	}
}

func TestAccountShareModeSettlementSkipsWaiverProgressCacheOnConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	usageLogID := int64(99002)
	occurredAt := time.Date(2026, 6, 30, 12, 15, 0, 0, time.UTC)
	snapshot := &service.AccountShareModeBillingSnapshot{
		MembershipID:       18012,
		ListingID:          510,
		AccountID:          405606,
		OwnerUserID:        7001,
		ConsumerUserID:     5926,
		APIKeyID:           15007,
		BaseCharge:         0.02,
		HourlyCharge:       0.04,
		TotalCharge:        0.06,
		RateMultiplier:     1,
		HourlyRate:         0.2,
		OwnerShareRatio:    0,
		PlatformShareRatio: 1,
		DurationMs:         60000,
	}
	cmd := &service.UsageBillingCommand{
		RequestID:                  "req-waiver-cache-conflict",
		APIKeyID:                   snapshot.APIKeyID,
		AccountShareModeSettlement: snapshot,
		UsageLog:                   &service.UsageLog{CreatedAt: occurredAt},
	}
	periodStartedAt, periodEndedAt := accountShareModeUsageRequestPeriod(cmd, snapshot)

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO account_share_mode_settlement_entries").
		WithArgs(
			nullablePositiveInt64(usageLogID),
			snapshot.MembershipID,
			snapshot.ListingID,
			snapshot.AccountID,
			snapshot.OwnerUserID,
			snapshot.ConsumerUserID,
			snapshot.APIKeyID,
			"0.0200000000",
			"0.0400000000",
			"0.0600000000",
			"0.0000000000",
			"0.0600000000",
			"1.0000",
			"0.20000000",
			nil,
			0,
			"0.00000000",
			nil,
			nil,
			nil,
			"0.00000000",
			"0.0000000000",
			"1.00000000",
			snapshot.DurationMs,
			periodStartedAt,
			periodEndedAt,
		).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectCommit()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	result := &service.UsageBillingApplyResult{}
	if err := applyAccountShareModeSettlement(context.Background(), tx, cmd, usageLogID, result); err != nil {
		_ = tx.Rollback()
		t.Fatalf("applyAccountShareModeSettlement failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositorySeatBillingDefersWaiverWindowDuringGrace(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	paidUntil := time.Date(2026, 6, 13, 11, 30, 0, 0, time.UTC)
	now := paidUntil.Add(service.AccountShareModeSeatWaiverSettlementGrace - time.Second)
	joinedAt := paidUntil.Add(-time.Hour)
	billedUntil := joinedAt
	newPaidUntil := paidUntil.Add(time.Minute)
	membershipID := int64(70)
	ownerUserID := int64(2284)
	consumerUserID := int64(4866)
	accountID := int64(417583)
	listingID := int64(10)
	apiKeyID := int64(20150)
	expectedPrepayRefID := accountShareSeatPrepayRefID(membershipID, newPaidUntil)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(membershipID, service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			membershipID,
			listingID,
			accountID,
			ownerUserID,
			consumerUserID,
			apiKeyID,
			service.AccountShareMembershipStatusActive,
			1,
			0.2,
			0.12,
			0,
			joinedAt,
			nil,
			nil,
			nil,
			paidUntil,
			billedUntil,
			billedUntil,
			0,
			int64(0),
			nil,
			nil,
			nil,
			joinedAt,
			joinedAt,
		))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(accountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(listingID, accountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	expectAccountShareBillingUserLock(mock, consumerUserID)
	mock.ExpectQuery("SELECT balance").
		WithArgs(consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(10.0))
	mock.ExpectExec("UPDATE users").
		WithArgs("9.9966666667", consumerUserID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO user_balance_ledger").
		WithArgs(consumerUserID, "debit", "0.0033333333", accountShareSeatPrepayReason, accountShareSeatPrepayRefType, expectedPrepayRefID, "9.9966666667", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("UPDATE account_share_memberships").
		WithArgs(newPaidUntil, nil, membershipID).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))
	mock.ExpectCommit()

	result, err := repo.processSeatBillingMembership(context.Background(), membershipID, now)
	if err != nil {
		t.Fatalf("processSeatBillingMembership failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected billing result")
	}
	if got := strings.Trim(strings.Join(int64sToStrings(result.DebitUserIDs), ","), ","); got != "4866" {
		t.Fatalf("debit users = %q", got)
	}
	if got := strings.Trim(strings.Join(int64sToStrings(result.CreditUserIDs), ","), ","); got != "" {
		t.Fatalf("credit users = %q", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositorySeatBillingRefundsSeatChargeWhenWaiverMinimumMet(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	paidUntil := time.Date(2026, 6, 13, 11, 30, 0, 0, time.UTC)
	now := paidUntil.Add(service.AccountShareModeSeatWaiverSettlementGrace)
	joinedAt := paidUntil.Add(-time.Hour)
	billedUntil := joinedAt
	membershipID := int64(70)
	settlementID := int64(7002)
	ownerUserID := int64(2284)
	consumerUserID := int64(4866)
	accountID := int64(417583)
	listingID := int64(10)
	apiKeyID := int64(20150)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(membershipID, service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			membershipID,
			listingID,
			accountID,
			ownerUserID,
			consumerUserID,
			apiKeyID,
			service.AccountShareMembershipStatusActive,
			1,
			0.2,
			0.12,
			0,
			joinedAt,
			nil,
			nil,
			nil,
			paidUntil,
			billedUntil,
			billedUntil,
			0.13,
			int64(2),
			paidUntil.Add(-time.Second),
			nil,
			nil,
			joinedAt,
			joinedAt,
		))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(accountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(listingID, accountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	expectAccountShareBillingUserLock(mock, consumerUserID)
	mock.ExpectQuery("WITH usage_rows").
		WithArgs(membershipID, billedUntil, paidUntil).
		WillReturnRows(sqlmock.NewRows([]string{"usage"}).AddRow("0.1300000000"))
	mock.ExpectQuery("INSERT INTO account_share_mode_settlement_entries").
		WithArgs(
			membershipID,
			listingID,
			accountID,
			ownerUserID,
			consumerUserID,
			apiKeyID,
			"0.0000000000",
			"0.0000000000",
			"0.20000000",
			nil,
			0,
			"0.00000000",
			nil,
			nil,
			nil,
			"0.00000000",
			"0.0000000000",
			"0.00000000",
			3600000,
			accountShareSeatSettlementTypeWaiverRefund,
			billedUntil,
			paidUntil,
			"0.2000000000",
			"0.12000000",
			"0.1200000000",
			"0.1300000000",
			nil,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(settlementID))
	mock.ExpectQuery("UPDATE users").
		WithArgs("0.2000000000", consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(10.2))
	mock.ExpectExec("INSERT INTO user_balance_ledger").
		WithArgs(consumerUserID, "credit", "0.2000000000", accountShareSeatWaiverRefundReason, accountShareModeSettlementRefType, settlementID, "10.2000000000", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT balance").
		WithArgs(consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(10.2))
	mock.ExpectExec("UPDATE users").
		WithArgs("10.1966666667", consumerUserID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO user_balance_ledger").
		WithArgs(consumerUserID, "debit", "0.0033333333", accountShareSeatPrepayReason, accountShareModeSettlementRefType, settlementID, "10.1966666667", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("UPDATE account_share_memberships").
		WithArgs(paidUntil.Add(time.Minute), paidUntil, membershipID).
		WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(now))
	mock.ExpectCommit()

	result, err := repo.processSeatBillingMembership(context.Background(), membershipID, now)
	if err != nil {
		t.Fatalf("processSeatBillingMembership failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected billing result")
	}
	if got := strings.Trim(strings.Join(int64sToStrings(result.DebitUserIDs), ","), ","); got != "4866" {
		t.Fatalf("debit users = %q", got)
	}
	if got := strings.Trim(strings.Join(int64sToStrings(result.CreditUserIDs), ","), ","); got != "4866" {
		t.Fatalf("credit users = %q", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositorySeatBillingRefundsPartialFinalWaiverWindowFromUsageEntries(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	joinedAt := time.Date(2026, 7, 1, 4, 51, 5, 36_145_000, time.UTC)
	windowStart := joinedAt.Add(2 * time.Hour)
	endedAt := windowStart.Add(10 * time.Minute)
	staleWaiverWindow := joinedAt
	membership := &service.AccountShareMembership{
		ID:                             20107,
		ListingID:                      452,
		AccountID:                      448111,
		OwnerUserID:                    7001,
		ConsumerUserID:                 8545,
		APIKeyID:                       9302,
		HourlyRateSnapshot:             0.4,
		HourlyFeeWaiverMinimumSnapshot: 0.4,
		JoinedAt:                       joinedAt,
		PaidUntil:                      &endedAt,
		BilledUntil:                    &windowStart,
		WaiverWindowStartedAt:          &staleWaiverWindow,
		WaiverWindowUsageAmount:        0,
	}
	settlementID := int64(991234)

	mock.ExpectBegin()
	expectAccountShareBillingUserLock(mock, membership.ConsumerUserID)
	mock.ExpectQuery("WITH usage_rows").
		WithArgs(membership.ID, windowStart, endedAt).
		WillReturnRows(sqlmock.NewRows([]string{"usage"}).AddRow("0.1936050504"))
	mock.ExpectQuery("INSERT INTO account_share_mode_settlement_entries").
		WithArgs(
			membership.ID,
			membership.ListingID,
			membership.AccountID,
			membership.OwnerUserID,
			membership.ConsumerUserID,
			membership.APIKeyID,
			"0.0000000000",
			"0.0000000000",
			"0.40000000",
			nil,
			0,
			"0.00000000",
			nil,
			nil,
			nil,
			"0.00000000",
			"0.0000000000",
			"0.00000000",
			600000,
			accountShareSeatSettlementTypeWaiverRefund,
			windowStart,
			endedAt,
			"0.0666666667",
			"0.40000000",
			"0.0666666667",
			"0.1936050504",
			nil,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(settlementID))
	mock.ExpectQuery("UPDATE users").
		WithArgs("0.0666666667", membership.ConsumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(1.9313793267))
	mock.ExpectExec("INSERT INTO user_balance_ledger").
		WithArgs(membership.ConsumerUserID, "credit", "0.0666666667", accountShareSeatWaiverRefundReason, accountShareModeSettlementRefType, settlementID, "1.9313793267", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	settledUntil, gotSettlementID, creditUserIDs, err := repo.settleSeatChargeInTx(context.Background(), tx, membership, endedAt, true, endedAt)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("settleSeatChargeInTx failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if settledUntil == nil || !settledUntil.Equal(endedAt) {
		t.Fatalf("settled until = %v, want %v", settledUntil, endedAt)
	}
	if gotSettlementID != settlementID {
		t.Fatalf("settlement id = %d, want %d", gotSettlementID, settlementID)
	}
	if got := strings.Trim(strings.Join(int64sToStrings(creditUserIDs), ","), ","); got != "8545" {
		t.Fatalf("credit users = %q, want 8545", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryProcessSeatWaiverCompensationRefundsLateEligibleWindow(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	settlementID := int64(8181)
	refundSettlementID := int64(8282)
	membershipID := int64(22564)
	listingID := int64(510)
	accountID := int64(449840)
	ownerUserID := int64(7001)
	consumerUserID := int64(4866)
	apiKeyID := int64(24514)
	windowStart := time.Date(2026, 7, 2, 9, 28, 11, 357850000, time.UTC)
	windowEnd := time.Date(2026, 7, 2, 9, 30, 25, 404639000, time.UTC)
	joinedAt := windowStart
	readyBefore := windowEnd.Add(service.AccountShareModeSeatWaiverCompensationDelay)
	charge := decimal.RequireFromString("0.0700018000")
	ownerCredit := decimal.RequireFromString("0.0630016200")

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT\\s+sc\\.id,").
		WithArgs(settlementID, accountShareSeatSettlementTypeCharge, readyBefore.UTC(), accountShareSeatSettlementTypeWaiverRefund).
		WillReturnRows(accountShareSeatChargeCompensationRows(
			settlementID,
			membershipID,
			listingID,
			accountID,
			ownerUserID,
			consumerUserID,
			apiKeyID,
			charge,
			ownerCredit,
			charge.Sub(ownerCredit),
			joinedAt,
			windowStart,
			windowEnd,
		))
	expectAccountShareBillingUserLock(mock, consumerUserID)
	mock.ExpectQuery("WITH usage_rows").
		WithArgs(membershipID, windowStart, windowEnd).
		WillReturnRows(sqlmock.NewRows([]string{"usage"}).AddRow("0.0834274000"))
	mock.ExpectExec("UPDATE account_share_mode_settlement_entries").
		WithArgs(settlementID, "1.88000000", "0.0700018000", "0.0834274000", accountShareSeatSettlementTypeCharge).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("INSERT INTO account_share_mode_settlement_entries").
		WithArgs(
			membershipID,
			listingID,
			accountID,
			ownerUserID,
			consumerUserID,
			apiKeyID,
			ownerCredit.StringFixed(10),
			charge.Sub(ownerCredit).StringFixed(10),
			"1.88000000",
			nil,
			0,
			"0.90000000",
			nil,
			nil,
			nil,
			"0.00000000",
			"0.0000000000",
			"0.10000000",
			int(windowEnd.Sub(windowStart).Milliseconds()),
			accountShareSeatSettlementTypeWaiverRefund,
			windowStart,
			windowEnd,
			charge.StringFixed(10),
			"1.88000000",
			"0.0700018000",
			"0.0834274000",
			settlementID,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(refundSettlementID))
	mock.ExpectQuery("UPDATE users").
		WithArgs(charge.StringFixed(10), consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(10.0700018))
	mock.ExpectExec("INSERT INTO user_balance_ledger").
		WithArgs(consumerUserID, "credit", charge.StringFixed(10), accountShareSeatWaiverRefundReason, accountShareModeSettlementRefType, refundSettlementID, "10.0700018000", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("UPDATE users").
		WithArgs(ownerCredit.StringFixed(10), ownerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(19.93699838))
	mock.ExpectExec("INSERT INTO user_balance_ledger").
		WithArgs(ownerUserID, "debit", ownerCredit.StringFixed(10), accountShareSeatWaiverRefundReason, accountShareModeSettlementRefType, refundSettlementID, "19.9369983800", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := repo.processSeatWaiverCompensation(context.Background(), settlementID, readyBefore)
	if err != nil {
		t.Fatalf("processSeatWaiverCompensation failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected compensation result")
	}
	if got := strings.Trim(strings.Join(int64sToStrings(result.CreditUserIDs), ","), ","); got != "4866" {
		t.Fatalf("credit users = %q, want 4866", got)
	}
	if got := strings.Trim(strings.Join(int64sToStrings(result.DebitUserIDs), ","), ","); got != "7001" {
		t.Fatalf("debit users = %q, want 7001", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryProcessSeatWaiverCompensationSkipsOwnerReversalWhenRefundAlreadyExists(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	settlementID := int64(8181)
	membershipID := int64(22564)
	listingID := int64(510)
	accountID := int64(449840)
	ownerUserID := int64(7001)
	consumerUserID := int64(4866)
	apiKeyID := int64(24514)
	windowStart := time.Date(2026, 7, 2, 9, 28, 11, 357850000, time.UTC)
	windowEnd := time.Date(2026, 7, 2, 9, 30, 25, 404639000, time.UTC)
	readyBefore := windowEnd.Add(service.AccountShareModeSeatWaiverCompensationDelay)
	charge := decimal.RequireFromString("0.0700018000")
	ownerCredit := decimal.RequireFromString("0.0630016200")

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT\\s+sc\\.id,").
		WithArgs(settlementID, accountShareSeatSettlementTypeCharge, readyBefore.UTC(), accountShareSeatSettlementTypeWaiverRefund).
		WillReturnRows(accountShareSeatChargeCompensationRows(
			settlementID,
			membershipID,
			listingID,
			accountID,
			ownerUserID,
			consumerUserID,
			apiKeyID,
			charge,
			ownerCredit,
			charge.Sub(ownerCredit),
			windowStart,
			windowStart,
			windowEnd,
		))
	expectAccountShareBillingUserLock(mock, consumerUserID)
	mock.ExpectQuery("WITH usage_rows").
		WithArgs(membershipID, windowStart, windowEnd).
		WillReturnRows(sqlmock.NewRows([]string{"usage"}).AddRow("0.0834274000"))
	mock.ExpectExec("UPDATE account_share_mode_settlement_entries").
		WithArgs(settlementID, "1.88000000", "0.0700018000", "0.0834274000", accountShareSeatSettlementTypeCharge).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("INSERT INTO account_share_mode_settlement_entries").
		WithArgs(
			membershipID,
			listingID,
			accountID,
			ownerUserID,
			consumerUserID,
			apiKeyID,
			ownerCredit.StringFixed(10),
			charge.Sub(ownerCredit).StringFixed(10),
			"1.88000000",
			nil,
			0,
			"0.90000000",
			nil,
			nil,
			nil,
			"0.00000000",
			"0.0000000000",
			"0.10000000",
			int(windowEnd.Sub(windowStart).Milliseconds()),
			accountShareSeatSettlementTypeWaiverRefund,
			windowStart,
			windowEnd,
			charge.StringFixed(10),
			"1.88000000",
			"0.0700018000",
			"0.0834274000",
			settlementID,
		).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectCommit()

	result, err := repo.processSeatWaiverCompensation(context.Background(), settlementID, readyBefore)
	if err != nil {
		t.Fatalf("processSeatWaiverCompensation failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected compensation result")
	}
	if len(result.CreditUserIDs) != 0 {
		t.Fatalf("credit users = %v, want empty", result.CreditUserIDs)
	}
	if len(result.DebitUserIDs) != 0 {
		t.Fatalf("debit users = %v, want empty", result.DebitUserIDs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryProcessSeatWaiverCompensationsAggregatesDebits(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	readyBefore := now.Add(-service.AccountShareModeSeatWaiverCompensationDelay)
	settlementID := int64(8181)
	refundSettlementID := int64(8282)
	membershipID := int64(22564)
	listingID := int64(510)
	accountID := int64(449840)
	ownerUserID := int64(7001)
	consumerUserID := int64(4866)
	apiKeyID := int64(24514)
	windowStart := time.Date(2026, 7, 2, 9, 28, 11, 357850000, time.UTC)
	windowEnd := time.Date(2026, 7, 2, 9, 30, 25, 404639000, time.UTC)
	charge := decimal.RequireFromString("0.0700018000")
	ownerCredit := decimal.RequireFromString("0.0630016200")

	mock.ExpectQuery("SELECT sc\\.id").
		WithArgs(accountShareSeatSettlementTypeCharge, accountShareSeatSettlementTypeWaiverRefund, accountShareSeatSettlementTypeUsage, readyBefore, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(settlementID))
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT\\s+sc\\.id,").
		WithArgs(settlementID, accountShareSeatSettlementTypeCharge, readyBefore.UTC(), accountShareSeatSettlementTypeWaiverRefund).
		WillReturnRows(accountShareSeatChargeCompensationRows(
			settlementID,
			membershipID,
			listingID,
			accountID,
			ownerUserID,
			consumerUserID,
			apiKeyID,
			charge,
			ownerCredit,
			charge.Sub(ownerCredit),
			windowStart,
			windowStart,
			windowEnd,
		))
	expectAccountShareBillingUserLock(mock, consumerUserID)
	mock.ExpectQuery("WITH usage_rows").
		WithArgs(membershipID, windowStart, windowEnd).
		WillReturnRows(sqlmock.NewRows([]string{"usage"}).AddRow("0.0834274000"))
	mock.ExpectExec("UPDATE account_share_mode_settlement_entries").
		WithArgs(settlementID, "1.88000000", "0.0700018000", "0.0834274000", accountShareSeatSettlementTypeCharge).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("INSERT INTO account_share_mode_settlement_entries").
		WithArgs(
			membershipID,
			listingID,
			accountID,
			ownerUserID,
			consumerUserID,
			apiKeyID,
			ownerCredit.StringFixed(10),
			charge.Sub(ownerCredit).StringFixed(10),
			"1.88000000",
			nil,
			0,
			"0.90000000",
			nil,
			nil,
			nil,
			"0.00000000",
			"0.0000000000",
			"0.10000000",
			int(windowEnd.Sub(windowStart).Milliseconds()),
			accountShareSeatSettlementTypeWaiverRefund,
			windowStart,
			windowEnd,
			charge.StringFixed(10),
			"1.88000000",
			"0.0700018000",
			"0.0834274000",
			settlementID,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(refundSettlementID))
	mock.ExpectQuery("UPDATE users").
		WithArgs(charge.StringFixed(10), consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(10.0700018))
	mock.ExpectExec("INSERT INTO user_balance_ledger").
		WithArgs(consumerUserID, "credit", charge.StringFixed(10), accountShareSeatWaiverRefundReason, accountShareModeSettlementRefType, refundSettlementID, "10.0700018000", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("UPDATE users").
		WithArgs(ownerCredit.StringFixed(10), ownerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(19.93699838))
	mock.ExpectExec("INSERT INTO user_balance_ledger").
		WithArgs(ownerUserID, "debit", ownerCredit.StringFixed(10), accountShareSeatWaiverRefundReason, accountShareModeSettlementRefType, refundSettlementID, "19.9369983800", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := repo.ProcessSeatWaiverCompensations(context.Background(), now, 1)
	if err != nil {
		t.Fatalf("ProcessSeatWaiverCompensations failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected compensation result")
	}
	if got := strings.Trim(strings.Join(int64sToStrings(result.CreditUserIDs), ","), ","); got != "4866" {
		t.Fatalf("credit users = %q, want 4866", got)
	}
	if got := strings.Trim(strings.Join(int64sToStrings(result.DebitUserIDs), ","), ","); got != "7001" {
		t.Fatalf("debit users = %q, want 7001", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryProcessSeatWaiverCompensationsUsesWindowEndReadiness(t *testing.T) {
	matcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if expectedSQL != "seat waiver compensation candidate query" {
			return nil
		}
		normalized := strings.ToLower(strings.Join(strings.Fields(actualSQL), " "))
		if !strings.Contains(normalized, "sc.period_ended_at <= $4") {
			return errors.New("waiver compensation candidate query must wait until the charged window has ended")
		}
		if strings.Contains(normalized, "sc.created_at <= $4") {
			return errors.New("waiver compensation candidate query must not use settlement creation time as readiness")
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	readyBefore := now.Add(-service.AccountShareModeSeatWaiverCompensationDelay)
	mock.ExpectQuery("seat waiver compensation candidate query").
		WithArgs(accountShareSeatSettlementTypeCharge, accountShareSeatSettlementTypeWaiverRefund, accountShareSeatSettlementTypeUsage, readyBefore, service.AccountShareModeSeatWaiverCompensationBatchSize).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	result, err := repo.ProcessSeatWaiverCompensations(context.Background(), now, 0)
	if err != nil {
		t.Fatalf("ProcessSeatWaiverCompensations failed: %v", err)
	}
	if result == nil || result.Processed != 0 {
		t.Fatalf("processed = %#v, want 0", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositorySeatBillingEndsUnavailableAccount(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	now := time.Date(2026, 6, 13, 11, 30, 0, 0, time.UTC)
	joinedAt := now.Add(-time.Minute)
	membershipID := int64(70)
	ownerUserID := int64(2284)
	consumerUserID := int64(4866)
	accountID := int64(417583)
	listingID := int64(10)
	apiKeyID := int64(20150)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(membershipID, service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			membershipID,
			listingID,
			accountID,
			ownerUserID,
			consumerUserID,
			apiKeyID,
			service.AccountShareMembershipStatusActive,
			1,
			0.2,
			0,
			0,
			joinedAt,
			nil,
			nil,
			nil,
			now,
			now,
			now,
			0,
			int64(0),
			nil,
			nil,
			nil,
			joinedAt,
			joinedAt,
		))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(accountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("SELECT\\s+a\\.status,").
		WithArgs(accountID, now).
		WillReturnRows(sqlmock.NewRows([]string{
			"status",
			"schedulable",
			"expired",
			"overload",
			"rate_limited",
			"temp_unschedulable",
			"codex_5h_protected",
			"codex_7d_protected",
			"codex_5h_used_percent",
			"codex_7d_used_percent",
			"codex_5h_limit_percent",
			"codex_7d_limit_percent",
			"codex_5h_reset_at",
			"codex_7d_reset_at",
		}).AddRow(
			service.StatusDisabled,
			true,
			false,
			false,
			false,
			false,
			false,
			false,
			"",
			"",
			"",
			"",
			"",
			"",
		))
	mock.ExpectQuery("UPDATE account_share_memberships").
		WithArgs(
			service.AccountShareMembershipStatusEnded,
			now,
			service.AccountShareMembershipEndReasonUnavailable,
			now,
			membershipID,
			service.AccountShareMembershipStatusActive,
		).
		WillReturnRows(sqlmock.NewRows([]string{"status", "ended_at", "ended_reason", "paid_until", "billed_until", "updated_at"}).
			AddRow(service.AccountShareMembershipStatusEnded, now, service.AccountShareMembershipEndReasonUnavailable, now, now, now))
	mock.ExpectCommit()

	result, err := repo.processSeatBillingMembership(context.Background(), membershipID, now)
	if err != nil {
		t.Fatalf("processSeatBillingMembership failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected billing result")
	}
	if got := strings.Trim(strings.Join(int64sToStrings(result.EndedConsumerUserIDs), ","), ","); got != "4866" {
		t.Fatalf("ended users = %q", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryProcessUnavailableMembershipsIncludesDeletedAccounts(t *testing.T) {
	matcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		normalized := strings.ToLower(actualSQL)
		switch expectedSQL {
		case "process unavailable memberships":
			if !strings.Contains(normalized, "left join account_share_listings l on l.id = m.listing_id") ||
				!strings.Contains(normalized, "left join accounts a on a.id = m.account_id") {
				return errors.New("unavailable membership scan must include deleted or missing accounts")
			}
			if !strings.Contains(normalized, "l.deleted_at is not null") ||
				!strings.Contains(normalized, "l.status = 'disabled'") ||
				!strings.Contains(normalized, "a.deleted_at is not null") {
				return errors.New("unavailable membership scan must treat terminal listings and soft-deleted accounts as unavailable")
			}
			for _, forbidden := range []string{
				"a.status <> 'active'",
				"a.schedulable = false",
				"rate_limit_reset_at",
				"temp_unschedulable_until",
				"overload_until",
			} {
				if strings.Contains(normalized, forbidden) {
					return errors.New("unavailable membership scan must not end recoverable account state: " + forbidden)
				}
			}
			if !strings.Contains(normalized, "a.status in ('disabled', 'inactive')") {
				return errors.New("unavailable membership scan must include explicitly disabled account states")
			}
		case "process stale queued memberships":
			if !strings.Contains(normalized, "m.status = $1") || !strings.Contains(normalized, "l.status = $2") {
				return errors.New("stale queued cleanup must target queued memberships on disabled listings")
			}
			if !strings.Contains(normalized, "a.status in ('disabled', 'inactive')") ||
				strings.Contains(normalized, "a.status <> 'active'") ||
				strings.Contains(normalized, "a.schedulable = false") ||
				strings.Contains(normalized, "rate_limit_reset_at") {
				return errors.New("stale queued cleanup must use permanent, non-recoverable account-unavailable conditions")
			}
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	now := time.Date(2026, 6, 14, 8, 30, 0, 0, time.UTC)
	mock.ExpectQuery("process unavailable memberships").
		WithArgs(service.AccountShareMembershipStatusActive, now, service.AccountShareModeSeatBillingBatchSize).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("process stale queued memberships").
		WithArgs(
			service.AccountShareMembershipStatusQueued,
			service.AccountShareListingStatusDisabled,
			now,
			service.AccountShareModeSeatBillingBatchSize,
			service.AccountShareMembershipStatusEnded,
			service.AccountShareMembershipEndReasonUnavailable,
		).
		WillReturnRows(sqlmock.NewRows([]string{"consumer_user_id"}))

	result, err := repo.ProcessUnavailableMemberships(context.Background(), now, service.AccountShareModeSeatBillingBatchSize)
	if err != nil {
		t.Fatalf("ProcessUnavailableMemberships failed: %v", err)
	}
	if result == nil || result.Processed != 0 {
		t.Fatalf("processed = %#v, want 0", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryEndMembershipReturnsAlreadyEndedMembership(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	now := time.Date(2026, 7, 5, 4, 18, 0, 0, time.UTC)
	endedAt := now.Add(-10 * time.Minute)
	membershipID := int64(25119)
	listingID := int64(521)
	accountID := int64(449297)
	ownerUserID := int64(1001)
	consumerUserID := int64(18467)
	apiKeyID := int64(27485)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(membershipID, consumerUserID).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			membershipID,
			listingID,
			accountID,
			ownerUserID,
			consumerUserID,
			apiKeyID,
			service.AccountShareMembershipStatusEnded,
			1,
			0.1,
			0.0,
			10,
			now.Add(-2*time.Hour),
			now.Add(-20*time.Minute),
			endedAt,
			service.AccountShareMembershipEndReasonIdleTimeout,
			endedAt,
			endedAt,
			endedAt,
			0.0,
			int64(0),
			nil,
			nil,
			nil,
			now.Add(-2*time.Hour),
			endedAt,
		))
	mock.ExpectCommit()

	membership, _, err := repo.EndMembership(context.Background(), consumerUserID, membershipID)
	if err != nil {
		t.Fatalf("EndMembership failed: %v", err)
	}
	if membership == nil || membership.ID != membershipID {
		t.Fatalf("unexpected membership: %#v", membership)
	}
	if membership.Status != service.AccountShareMembershipStatusEnded {
		t.Fatalf("status = %q, want ended", membership.Status)
	}
	if membership.EndedAt == nil || !membership.EndedAt.Equal(endedAt) {
		t.Fatalf("ended_at = %v, want %v", membership.EndedAt, endedAt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryDisablePermanentlyUnavailableListingsUsesPermanentConditionsOnly(t *testing.T) {
	matcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if expectedSQL != "disable permanent unavailable listings" {
			return nil
		}
		normalized := strings.ToLower(actualSQL)
		for _, forbidden := range []string{
			"a.status <> 'active'",
			"a.schedulable = false",
			"overload_until",
			"rate_limit_reset_at",
			"temp_unschedulable_until",
			"codex_5h",
			"codex_7d",
		} {
			if strings.Contains(normalized, forbidden) {
				return errors.New("permanent listing disable must not use transient availability condition: " + forbidden)
			}
		}
		for _, required := range []string{
			"update account_share_listings",
			"a.deleted_at is not null",
			"a.status in ('disabled', 'inactive')",
			"a.auto_pause_on_expired = true",
		} {
			if !strings.Contains(normalized, required) {
				return errors.New("permanent listing disable query missing condition: " + required)
			}
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	now := time.Date(2026, 6, 14, 8, 35, 0, 0, time.UTC)
	mock.ExpectQuery("disable permanent unavailable listings").
		WithArgs(service.AccountShareListingStatusActive, service.AccountShareListingStatusDisabled, 50, now).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(10)).AddRow(int64(11)))

	result, err := repo.DisablePermanentlyUnavailableListings(context.Background(), now, 50)
	if err != nil {
		t.Fatalf("DisablePermanentlyUnavailableListings failed: %v", err)
	}
	if result == nil || result.Processed != 2 {
		t.Fatalf("processed = %#v, want 2", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareListingUsesApproximatePagination(t *testing.T) {
	if accountShareListingUsesApproximatePagination(service.AccountShareListingFilters{}) {
		t.Fatal("default listing filters should keep exact pagination")
	}
	if accountShareListingUsesApproximatePagination(service.AccountShareListingFilters{
		SortBy:    service.AccountShareListingSortHourlyRate,
		SortOrder: service.AccountShareListingSortOrderAsc,
	}) {
		t.Fatal("sorting alone should keep exact pagination")
	}

	cases := []service.AccountShareListingFilters{
		{SeatLimit: 2},
		{SeatLimits: []int{2, 3}},
		{Search: "gpt"},
		{Status: service.AccountShareListingStatusActive},
		{Models: []string{"gpt-5.5"}},
		{AccountLevel: "pro"},
		{FeatureTags: []string{service.AccountShareListingFeatureImageGeneration}},
	}
	for _, filters := range cases {
		if !accountShareListingUsesApproximatePagination(filters) {
			t.Fatalf("expected approximate pagination for filters %#v", filters)
		}
	}
}

func TestAccountShareModeRepositoryListListingsFiltersNonCodexCLIOnly(t *testing.T) {
	queryMatcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if expectedSQL != "list listings with non codex cli only filter" {
			return nil
		}
		if !strings.Contains(actualSQL, "l.codex_cli_only = FALSE") {
			return errors.New("expected non_codex_cli_only filter to require l.codex_cli_only = FALSE")
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(queryMatcher))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	mock.ExpectQuery("list listings with non codex cli only filter").
		WithArgs(int64(42), 21, 0).
		WillReturnRows(accountShareListingRows(7, 8, 9, "", time.Time{}))

	listings, result, err := repo.ListListings(context.Background(), 42, service.AccountShareListingFilters{
		FeatureTags: []string{service.AccountShareListingFeatureNonCodexCLIOnly},
	}, pagination.PaginationParams{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListListings failed: %v", err)
	}
	if len(listings) != 1 {
		t.Fatalf("listings length = %d, want 1", len(listings))
	}
	if result == nil || result.Total != 1 || result.Page != 1 || result.PageSize != 20 {
		t.Fatalf("unexpected pagination result: %#v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryGetMySpendSummaryAggregatesCurrentMembership(t *testing.T) {
	queryMatcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		switch expectedSQL {
		case "my spend listing":
			if !strings.Contains(actualSQL, "FROM account_share_listings") {
				return errors.New("expected listing lookup")
			}
		case "my spend membership":
			if !strings.Contains(actualSQL, "FROM account_share_memberships") || !strings.Contains(actualSQL, "m.consumer_user_id = $2") {
				return errors.New("expected consumer membership lookup")
			}
		case "my spend totals":
			if !strings.Contains(actualSQL, "account_share_mode_settlement_entries") || !strings.Contains(actualSQL, "e.membership_id = $5") {
				return errors.New("expected totals query to include settlement entries and membership filter")
			}
		case "my spend hourly ledger totals":
			if !strings.Contains(actualSQL, "FROM user_balance_ledger") || !strings.Contains(actualSQL, "metadata->>'membership_id'") {
				return errors.New("expected hourly ledger totals query to filter balance ledger by membership metadata")
			}
		case "my spend models":
			if !strings.Contains(actualSQL, "GROUP BY") || !strings.Contains(actualSQL, "e.membership_id = $5") {
				return errors.New("expected model query grouped with membership filter")
			}
		default:
			return nil
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(queryMatcher))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}
	joinedAt := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	lastActivityAt := time.Date(2026, 6, 26, 11, 30, 0, 0, time.UTC)

	mock.ExpectQuery("my spend listing").
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "account_id", "account_name", "platform", "owner_user_id", "owner_username"}).
			AddRow(int64(7), int64(8), "shared-account", service.PlatformOpenAI, int64(9), "owner"))
	mock.ExpectQuery("my spend membership").
		WithArgs(int64(7), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"api_key_id",
			"api_key_name",
			"status",
			"queue_rank",
			"joined_at",
			"last_request_at",
			"ended_at",
			"ended_reason",
			"paid_until",
			"billed_until",
			"hourly_rate_snapshot",
			"hourly_fee_waiver_minimum_snapshot",
			"idle_timeout_minutes",
		}).AddRow(
			int64(11),
			int64(12),
			"primary-key",
			service.AccountShareMembershipStatusActive,
			0,
			joinedAt,
			lastActivityAt,
			nil,
			nil,
			nil,
			nil,
			0.5,
			2.0,
			10,
		))
	mock.ExpectQuery("my spend totals").
		WithArgs(int64(7), int64(42), joinedAt, now, int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{
			"request_count",
			"input_tokens",
			"output_tokens",
			"cache_creation_tokens",
			"cache_read_tokens",
			"request_cost",
			"last_activity_at",
		}).AddRow(int64(3), int64(100), int64(40), int64(10), int64(5), 1.2, lastActivityAt))
	mock.ExpectQuery("my spend hourly ledger totals").
		WithArgs(
			int64(42),
			joinedAt,
			now,
			accountShareSeatPrepayReason,
			accountShareSeatRefundReason,
			accountShareSeatWaiverRefundReason,
			int64(7),
			int64(11),
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"hourly_charge",
			"hourly_refund",
			"hourly_waiver_refund",
		}).AddRow(0.8, 0.1, 0.2))
	mock.ExpectQuery("my spend models").
		WithArgs(int64(7), int64(42), joinedAt, now, int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{
			"model",
			"request_count",
			"input_tokens",
			"output_tokens",
			"cache_creation_tokens",
			"cache_read_tokens",
			"request_cost",
		}).
			AddRow("gpt-5.5", int64(2), int64(80), int64(30), int64(10), int64(5), 0.9).
			AddRow("gpt-5.4", int64(1), int64(20), int64(10), int64(0), int64(0), 0.3))

	summary, err := repo.GetMySpendSummary(context.Background(), service.AccountShareMySpendQuery{
		ListingID:  7,
		ConsumerID: 42,
		Range:      service.AccountShareSpendRangeCurrentMembership,
		EndTime:    now,
	})
	if err != nil {
		t.Fatalf("GetMySpendSummary failed: %v", err)
	}
	if summary.Membership == nil || summary.Membership.ID != 11 {
		t.Fatalf("unexpected membership: %#v", summary.Membership)
	}
	if summary.Membership.APIKeyName != "primary-key" {
		t.Fatalf("api key name = %q, want primary-key", summary.Membership.APIKeyName)
	}
	if summary.RequestCount != 3 || summary.TotalTokens != 155 {
		t.Fatalf("unexpected request totals: %#v", summary)
	}
	if math.Abs(summary.HourlyNetCost-0.5) > 1e-9 {
		t.Fatalf("hourly net cost = %v, want 0.5", summary.HourlyNetCost)
	}
	if math.Abs(summary.TotalCost-1.7) > 1e-9 {
		t.Fatalf("total cost = %v, want 1.7", summary.TotalCost)
	}
	if len(summary.ModelBreakdown) != 2 || summary.ModelBreakdown[0].Model != "gpt-5.5" {
		t.Fatalf("unexpected model breakdown: %#v", summary.ModelBreakdown)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareListingOrderSQLMultipleCriteria(t *testing.T) {
	got := accountShareListingOrderSQL(service.AccountShareListingFilters{
		Sorts: []service.AccountShareListingSortCriterion{
			{SortBy: service.AccountShareListingSortPerUserConcurrency, SortOrder: service.AccountShareListingSortOrderAsc},
			{SortBy: service.AccountShareListingSortMinBalanceRequired, SortOrder: service.AccountShareListingSortOrderDesc},
			{SortBy: service.AccountShareListingSortUpdatedAt, SortOrder: service.AccountShareListingSortOrderAsc},
		},
	})
	want := "l.per_user_concurrency ASC, l.min_balance_required DESC, l.updated_at ASC, l.id ASC"
	if got != want {
		t.Fatalf("unexpected order SQL\nwant: %s\n got: %s", want, got)
	}
}

func TestAccountShareModeRepositorySubmitReviewLocksListingBeforeMembership(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	membershipID := int64(81)
	listingID := int64(82)
	accountID := int64(83)
	ownerUserID := int64(84)
	consumerUserID := int64(85)
	lastRequestAt := time.Date(2026, 7, 11, 1, 5, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT\\s+l\\.id.*FOR UPDATE OF l$").
		WithArgs(membershipID, consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(listingID))
	mock.ExpectQuery("SELECT\\s+m\\.listing_id.*FOR UPDATE OF m$").
		WithArgs(membershipID, consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{
			"listing_id", "account_id", "account_identity_id", "owner_user_id", "last_request_at",
			"status", "name", "platform", "credentials", "extra",
		}).AddRow(
			listingID, accountID, nil, ownerUserID, lastRequestAt,
			service.AccountShareMembershipStatusActive, "shared-account", service.PlatformOpenAI, `{}`, `{}`,
		))
	mock.ExpectRollback()

	_, err = repo.SubmitReview(context.Background(), consumerUserID, membershipID, service.SubmitAccountShareReviewInput{Score: 5})
	if !errors.Is(err, service.ErrAccountShareReviewNoUsage) {
		t.Fatalf("expected no-usage rejection for active membership, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryClaimPendingReviewModerationsUsesTopLevelCTE(t *testing.T) {
	matcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if expectedSQL != "claim review moderation query" {
			return nil
		}
		normalized := strings.ToLower(strings.Join(strings.Fields(actualSQL), " "))
		if !strings.HasPrefix(normalized, "with picked as") {
			return errors.New("claim query must start with top-level picked CTE")
		}
		if !strings.Contains(normalized, "claimed as ( update account_share_reviews r_claim") {
			return errors.New("claim query must use a top-level data-modifying claimed CTE")
		}
		if strings.Contains(normalized, "join ( with picked") {
			return errors.New("postgres does not allow the data-modifying CTE inside a join subquery")
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}
	now := time.Date(2026, 6, 24, 4, 40, 0, 0, time.UTC)

	mock.ExpectQuery("claim review moderation query").
		WithArgs(now, service.AccountShareReviewCommentStatusPending, service.AccountShareReviewCommentStatusFailed, service.AccountShareReviewModerationMaxAttempts, 7).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	reviews, err := repo.ClaimPendingReviewModerations(context.Background(), now, 7)
	if err != nil {
		t.Fatalf("ClaimPendingReviewModerations failed: %v", err)
	}
	if len(reviews) != 0 {
		t.Fatalf("reviews len = %d, want 0", len(reviews))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryListsOnlyRecoverableUnavailableMemberships(t *testing.T) {
	matcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if expectedSQL != "recoverable unavailable memberships" {
			return nil
		}
		normalized := strings.ToLower(strings.Join(strings.Fields(actualSQL), " "))
		for _, required := range []string{
			"join account_share_listings l on l.id = m.listing_id",
			"left join accounts a on a.id = m.account_id",
			"l.status = 'paused'",
			"a.status <> 'active'",
			"a.schedulable = false",
			"a.status in ('disabled', 'inactive')",
			"order by coalesce(m.last_request_at, m.joined_at) asc, m.id asc",
		} {
			if !strings.Contains(normalized, required) {
				return fmt.Errorf("recoverable scan missing %q", required)
			}
		}
		if !strings.Contains(normalized, "not (") {
			return errors.New("recoverable scan must explicitly exclude permanent states")
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	now := time.Date(2026, 7, 11, 1, 0, 0, 0, time.UTC)

	mock.ExpectQuery("recoverable unavailable memberships").
		WithArgs(service.AccountShareMembershipStatusActive, now, 2).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(41)).AddRow(int64(42)))

	ids, err := repo.ListRecoverableUnavailableMembershipIDs(context.Background(), now, 2)
	if err != nil {
		t.Fatalf("ListRecoverableUnavailableMembershipIDs failed: %v", err)
	}
	if len(ids) != 2 || ids[0] != 41 || ids[1] != 42 {
		t.Fatalf("unexpected membership ids: %#v", ids)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositorySeatBillingExcludesRecoverableUnavailableMemberships(t *testing.T) {
	matcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if expectedSQL != "seat billing candidates" {
			return nil
		}
		normalized := strings.ToLower(strings.Join(strings.Fields(actualSQL), " "))
		if !strings.Contains(normalized, "join account_share_listings l on l.id = m.listing_id") ||
			!strings.Contains(normalized, "left join accounts a on a.id = m.account_id") ||
			!strings.Contains(normalized, "and not (") ||
			!strings.Contains(normalized, "l.status = 'paused'") ||
			!strings.Contains(normalized, "a.schedulable = false") {
			return errors.New("seat billing candidates must exclude recoverable unavailable memberships")
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	now := time.Date(2026, 7, 11, 1, 1, 0, 0, time.UTC)

	mock.ExpectQuery("seat billing candidates").
		WithArgs(service.AccountShareMembershipStatusActive, now, 5).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	result, err := repo.ProcessSeatBilling(context.Background(), now, 5)
	if err != nil {
		t.Fatalf("ProcessSeatBilling failed: %v", err)
	}
	if result == nil || result.Processed != 0 {
		t.Fatalf("unexpected billing result: %#v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryRecoverableUnavailableDoesNotRenewSeat(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	now := time.Date(2026, 7, 11, 1, 2, 0, 0, time.UTC)
	joinedAt := now.Add(-2 * time.Minute)
	billedUntil := now.Add(-time.Minute)
	membershipID := int64(70)
	listingID := int64(510)
	accountID := int64(405606)
	ownerUserID := int64(7001)
	consumerUserID := int64(5926)
	apiKeyID := int64(15007)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(membershipID, service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			membershipID, listingID, accountID, ownerUserID, consumerUserID, apiKeyID,
			service.AccountShareMembershipStatusActive, 1, 0.2, 0.0, 0,
			joinedAt, nil, nil, nil, now, billedUntil, billedUntil, 0, int64(0), nil,
			nil, nil, joinedAt, joinedAt,
		))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(accountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(listingID, accountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectRollback()

	result, err := repo.processSeatBillingMembership(context.Background(), membershipID, now)
	if err != nil {
		t.Fatalf("processSeatBillingMembership failed: %v", err)
	}
	if result != nil {
		t.Fatalf("recoverable unavailable membership must not renew, got %#v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryRecoverableSuspensionSkipsRecentlyActiveMembership(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	now := time.Date(2026, 7, 11, 1, 2, 30, 0, time.UTC)
	joinedAt := now.Add(-time.Minute)
	paidUntil := now.Add(time.Minute)
	membershipID := int64(71)
	listingID := int64(511)
	accountID := int64(405607)
	ownerUserID := int64(7002)
	consumerUserID := int64(5927)
	apiKeyID := int64(15008)

	mock.ExpectBegin()
	expectRecoverableSuspensionResourceLocks(mock, membershipID, listingID, accountID)
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(membershipID, service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			membershipID, listingID, accountID, ownerUserID, consumerUserID, apiKeyID,
			service.AccountShareMembershipStatusActive, 1, 0.2, 0.0, 0,
			joinedAt, now, nil, nil, paidUntil, now, now, 0, int64(0), nil,
			nil, nil, joinedAt, now,
		))
	mock.ExpectRollback()

	membership, _, err := repo.SuspendRecoverableUnavailableMembership(context.Background(), membershipID, now)
	if err != nil {
		t.Fatalf("SuspendRecoverableUnavailableMembership failed: %v", err)
	}
	if membership != nil {
		t.Fatalf("recently active membership must stay active, got %#v", membership)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositorySuspendsRecoverableUnavailableAndRefundsPrepay(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	now := time.Date(2026, 7, 11, 1, 3, 0, 0, time.UTC)
	joinedAt := now.Add(-time.Minute)
	paidUntil := now.Add(30 * time.Minute)
	membershipID := int64(18012)
	listingID := int64(510)
	accountID := int64(405606)
	ownerUserID := int64(7001)
	consumerUserID := int64(5926)
	apiKeyID := int64(15007)
	settlementID := int64(991234)

	mock.ExpectBegin()
	expectRecoverableSuspensionResourceLocks(mock, membershipID, listingID, accountID)
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(membershipID, service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			membershipID, listingID, accountID, ownerUserID, consumerUserID, apiKeyID,
			service.AccountShareMembershipStatusActive, 1, 0.2, 0.0, 0,
			joinedAt, nil, nil, nil, paidUntil, now, now, 0, int64(0), nil,
			nil, nil, joinedAt, joinedAt,
		))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(listingID, accountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("INSERT INTO account_share_mode_settlement_entries").
		WithArgs(
			membershipID, listingID, accountID, ownerUserID, consumerUserID, apiKeyID,
			"0.0000000000", "0.0000000000", "0.0000000000", "0.20000000",
			nil, 0, "0.00000000", nil, nil, nil, "0.00000000", "0.0000000000", "0.00000000",
			1800000, accountShareSeatSettlementTypeRefund,
			now, paidUntil, "0.1000000000", "0.00000000", "0.0000000000", "0.0000000000",
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(settlementID))
	mock.ExpectQuery("UPDATE users").
		WithArgs("0.1000000000", consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(12.1))
	mock.ExpectExec("INSERT INTO user_balance_ledger").
		WithArgs(consumerUserID, "credit", "0.1000000000", accountShareSeatRefundReason, accountShareModeSettlementRefType, settlementID, "12.1000000000", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("UPDATE account_share_memberships m").
		WithArgs(service.AccountShareMembershipStatusQueued, now, now, now, membershipID, service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			membershipID, listingID, accountID, ownerUserID, consumerUserID, apiKeyID,
			service.AccountShareMembershipStatusQueued, 1, 0.2, 0.0, 0,
			joinedAt, nil, nil, nil, nil, now, now, 0, int64(0), nil,
			now, now, joinedAt, now,
		))
	mock.ExpectCommit()

	membership, _, err := repo.SuspendRecoverableUnavailableMembership(context.Background(), membershipID, now)
	if err != nil {
		t.Fatalf("SuspendRecoverableUnavailableMembership failed: %v", err)
	}
	if membership == nil || membership.Status != service.AccountShareMembershipStatusQueued {
		t.Fatalf("unexpected suspended membership: %#v", membership)
	}
	if membership.PaidUntil != nil || membership.BilledUntil == nil || !membership.BilledUntil.Equal(now) {
		t.Fatalf("unexpected billing timestamps after suspension: %#v", membership)
	}
	if membership.DispatchCooldownUntil == nil || !membership.DispatchCooldownUntil.Equal(now) {
		t.Fatalf("recoverable suspension must be immediately eligible after recovery: %#v", membership.DispatchCooldownUntil)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryRecoverableSuspensionRechecksAvailabilityAfterResourceLocks(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	now := time.Date(2026, 7, 11, 1, 3, 30, 0, time.UTC)
	joinedAt := now.Add(-time.Minute)
	paidUntil := now.Add(time.Minute)
	membershipID := int64(72)
	listingID := int64(512)
	accountID := int64(405608)
	ownerUserID := int64(7003)
	consumerUserID := int64(5928)
	apiKeyID := int64(15009)

	mock.ExpectBegin()
	expectRecoverableSuspensionResourceLocks(mock, membershipID, listingID, accountID)
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(membershipID, service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			membershipID, listingID, accountID, ownerUserID, consumerUserID, apiKeyID,
			service.AccountShareMembershipStatusActive, 1, 0.2, 0.0, 0,
			joinedAt, nil, nil, nil, paidUntil, now, now, 0, int64(0), nil,
			nil, nil, joinedAt, joinedAt,
		))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(listingID, accountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	membership, _, err := repo.SuspendRecoverableUnavailableMembership(context.Background(), membershipID, now)
	if err != nil {
		t.Fatalf("SuspendRecoverableUnavailableMembership failed: %v", err)
	}
	if membership != nil {
		t.Fatalf("recovered listing/account must keep membership active, got %#v", membership)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func expectRecoverableSuspensionResourceLocks(mock sqlmock.Sqlmock, membershipID, listingID, accountID int64) {
	mock.ExpectQuery("SELECT\\s+m\\.listing_id, m\\.account_id.*FOR UPDATE OF l").
		WithArgs(membershipID, service.AccountShareMembershipStatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"listing_id", "account_id"}).AddRow(listingID, accountID))
	mock.ExpectQuery("SELECT\\s+id\\s+FROM accounts.*FOR UPDATE").
		WithArgs(accountID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(accountID))
}

func TestAccountShareModeRepositoryActivationLocksCandidateListingsBeforeCapacityCheck(t *testing.T) {
	matcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		normalized := strings.ToLower(strings.Join(strings.Fields(actualSQL), " "))
		switch expectedSQL {
		case "lock queued listing candidates":
			if !strings.Contains(normalized, "select l.id") ||
				!strings.Contains(normalized, "order by l.id asc") ||
				!strings.Contains(normalized, "limit $6 for update of l") {
				return errors.New("queued activation must lock every candidate listing in deterministic id order")
			}
		case "activate queued membership":
			if !strings.Contains(normalized, "l.id = any($7::bigint[])") ||
				!strings.Contains(normalized, "m_available.status = 'active'") ||
				!strings.Contains(normalized, "for update of m") ||
				strings.Contains(normalized, "for update of m, l") {
				return errors.New("activation must use the locked listing set, recount active seats, then lock only the membership")
			}
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	now := time.Date(2026, 7, 11, 1, 4, 0, 0, time.UTC)
	userID := int64(101)
	apiKeyID := int64(202)
	groupID := int64(303)

	mock.ExpectBegin()
	mock.ExpectQuery("lock queued listing candidates").
		WithArgs(userID, apiKeyID, service.AccountShareMembershipStatusQueued, groupID, now, service.AccountShareModeQueueMaxItems).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(501)).AddRow(int64(502)))
	mock.ExpectQuery("activate queued membership").
		WithArgs(userID, apiKeyID, service.AccountShareMembershipStatusQueued, groupID, now, 0, "{501,502}").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "listing_id", "account_id", "owner_user_id", "queue_rank", "idle_timeout_minutes",
			"hourly_rate", "hourly_fee_waiver_minimum", "min_balance_required",
		}))
	mock.ExpectRollback()

	_, _, err = repo.ActivateNextQueuedMembershipForRequest(context.Background(), userID, apiKeyID, groupID, 0, now)
	if !errors.Is(err, service.ErrAccountShareListingNotFound) {
		t.Fatalf("expected no available candidate after locked-set recount, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func int64sToStrings(values []int64) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strconv.FormatInt(value, 10))
	}
	return out
}

func accountShareMembershipColumns() []string {
	return []string{
		"id",
		"listing_id",
		"account_id",
		"owner_user_id",
		"consumer_user_id",
		"api_key_id",
		"status",
		"queue_rank",
		"hourly_rate_snapshot",
		"hourly_fee_waiver_minimum_snapshot",
		"idle_timeout_minutes",
		"joined_at",
		"last_request_at",
		"ended_at",
		"ended_reason",
		"paid_until",
		"billed_until",
		"waiver_window_started_at",
		"waiver_window_usage_amount",
		"waiver_window_request_count",
		"waiver_window_last_request_at",
		"dispatch_failed_at",
		"dispatch_cooldown_until",
		"created_at",
		"updated_at",
	}
}

func TestAccountShareModeRepositoryGetActiveMembershipForRequestUsesMembershipOnly(t *testing.T) {
	matcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if expectedSQL != "active request membership query" {
			return nil
		}
		normalized := strings.ToLower(actualSQL)
		if strings.Contains(normalized, "account_groups") {
			return errors.New("request binding query must not depend on account_groups")
		}
		if !strings.Contains(normalized, "m.consumer_user_id = $1") || !strings.Contains(normalized, "m.api_key_id = $2") {
			return errors.New("request binding query must match consumer and api key")
		}
		if !strings.Contains(normalized, "account_share_mode_groups") || !strings.Contains(normalized, "mg.group_id = $3") {
			return errors.New("request binding query must match request mode group platform")
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	mock.ExpectQuery("active request membership query").
		WithArgs(int64(20), int64(30), int64(50)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"listing_id",
			"account_id",
			"owner_user_id",
			"consumer_user_id",
			"api_key_id",
			"status",
			"hourly_rate_snapshot",
			"hourly_fee_waiver_minimum_snapshot",
			"joined_at",
			"ended_at",
			"paid_until",
			"billed_until",
			"created_at",
			"updated_at",
		}))

	_, _, err = repo.GetActiveMembershipForRequest(context.Background(), 20, 30, 50)
	if !errors.Is(err, service.ErrAccountShareListingNotFound) {
		t.Fatalf("expected not found from empty binding query, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRatioKeepsExplicitZero(t *testing.T) {
	got := normalizeAccountShareModeRatio(0)
	if !got.Equal(decimal.Zero) {
		t.Fatalf("expected explicit zero ratio to stay zero, got %s", got)
	}
}

func TestAccountShareModeSettlementRatiosClampPlatformOverflow(t *testing.T) {
	owner, invite, platform := accountShareModeSettlementRatios(0.8, 0.5)
	if !owner.Equal(decimal.NewFromFloat(0.8)) {
		t.Fatalf("owner ratio = %s, want 0.8", owner)
	}
	if !invite.Equal(decimal.NewFromFloat(0.2)) {
		t.Fatalf("invite ratio = %s, want 0.2", invite)
	}
	if !platform.Equal(decimal.Zero) {
		t.Fatalf("platform ratio = %s, want 0", platform)
	}
}

func expectAccountShareBillingUserLock(mock sqlmock.Sqlmock, userID int64) {
	mock.ExpectQuery("SELECT\\s+id\\s+FROM users.*FOR UPDATE").
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(userID))
}

func accountShareSeatChargeCompensationRows(
	settlementID,
	membershipID,
	listingID,
	accountID,
	ownerUserID,
	consumerUserID,
	apiKeyID int64,
	charge,
	ownerCredit,
	platformCredit decimal.Decimal,
	joinedAt,
	windowStart,
	windowEnd time.Time,
) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"membership_id",
		"listing_id",
		"account_id",
		"owner_user_id",
		"consumer_user_id",
		"api_key_id",
		"hourly_charge",
		"owner_credit",
		"invite_credit",
		"platform_credit",
		"hourly_rate_snapshot",
		"policy_id",
		"policy_version",
		"owner_share_ratio_snapshot",
		"inviter_user_id",
		"invite_bound_at_snapshot",
		"invite_expires_at_snapshot",
		"invite_share_ratio_snapshot",
		"platform_share_ratio_snapshot",
		"waiver_minimum",
		"status",
		"queue_rank",
		"idle_timeout_minutes",
		"joined_at",
		"period_started_at",
		"period_ended_at",
		"created_at",
		"updated_at",
	}).AddRow(
		settlementID,
		membershipID,
		listingID,
		accountID,
		ownerUserID,
		consumerUserID,
		apiKeyID,
		charge.StringFixed(10),
		ownerCredit.StringFixed(10),
		"0.0000000000",
		platformCredit.StringFixed(10),
		"1.88000000",
		nil,
		0,
		"0.90000000",
		nil,
		nil,
		nil,
		"0.00000000",
		"0.10000000",
		"1.88000000",
		service.AccountShareMembershipStatusEnded,
		1,
		0,
		joinedAt,
		windowStart,
		windowEnd,
		windowEnd,
		windowEnd,
	)
}

type accountShareListingRowData struct {
	ListingID                        int64
	AccountID                        int64
	OwnerUserID                      int64
	EditSessionID                    string
	EditingExpiresAt                 time.Time
	HourlyRate                       float64
	HourlyFeeWaiverMinimum           float64
	CurrentMembershipID              any
	CurrentConsumerUserID            any
	CurrentAPIKeyID                  any
	CurrentAPIKeyName                any
	CurrentJoinedAt                  any
	CurrentPaidUntil                 any
	CurrentBilledUntil               any
	CurrentIdleTimeoutMinutes        any
	CurrentLastRequestAt             any
	CurrentWaiverWindowStartedAt     any
	CurrentWaiverWindowUsageAmount   any
	CurrentWaiverWindowRequestCount  any
	CurrentWaiverWindowLastRequestAt any
	QueueAPIKeyName                  any
}

func accountShareListingRows(listingID, accountID, ownerUserID int64, editSessionID string, editingExpiresAt time.Time, configure ...func(*accountShareListingRowData)) *sqlmock.Rows {
	now := time.Now().UTC()
	row := &accountShareListingRowData{
		ListingID:              listingID,
		AccountID:              accountID,
		OwnerUserID:            ownerUserID,
		EditSessionID:          editSessionID,
		EditingExpiresAt:       editingExpiresAt,
		HourlyRate:             0.15,
		HourlyFeeWaiverMinimum: 0,
	}
	for _, apply := range configure {
		if apply != nil {
			apply(row)
		}
	}
	columns := []string{
		"id",
		"account_id",
		"owner_user_id",
		"owner_username",
		"account_name",
		"proxy_id",
		"status",
		"seat_limit",
		"active_seats",
		"account_identity_id",
		"rating_count",
		"rating_score_sum",
		"rating_avg",
		"rate_multiplier",
		"allowed_models",
		"per_user_concurrency",
		"account_concurrency",
		"hourly_rate",
		"hourly_fee_waiver_minimum",
		"min_balance_required",
		"codex_cli_only",
		"codex_5h_limit_percent",
		"codex_7d_limit_percent",
		"platform",
		"type",
		"account_level",
		"account_status",
		"schedulable",
		"expires_at",
		"last_used_at",
		"rate_limited_at",
		"rate_limit_reset_at",
		"overload_until",
		"temp_unschedulable_until",
		"temp_unschedulable_reason",
		"session_window_start",
		"session_window_end",
		"session_window_status",
		"credentials",
		"extra",
		"subscription_expires_at",
		"current_membership_id",
		"current_consumer_user_id",
		"current_api_key_id",
		"current_api_key_name",
		"current_joined_at",
		"current_paid_until",
		"current_billed_until",
		"current_idle_timeout_minutes",
		"current_last_request_at",
		"current_waiver_window_started_at",
		"current_waiver_window_usage_amount",
		"current_waiver_window_request_count",
		"current_waiver_window_last_request_at",
		"queue_membership_id",
		"queue_api_key_id",
		"queue_api_key_name",
		"queue_rank",
		"queue_status",
		"queue_idle_timeout_minutes",
		"queue_dispatch_cooldown_until",
		"last_used_membership_id",
		"last_used_at",
		"editing_by_user_id",
		"editing_by_username",
		"editing_expires_at",
		"editing_mine",
		"edit_session_id",
		"created_at",
		"updated_at",
	}
	values := []driver.Value{
		row.ListingID,
		row.AccountID,
		row.OwnerUserID,
		"owner",
		"shared-account",
		nil,
		service.AccountShareListingStatusActive,
		4,
		0,
		nil,
		0,
		0,
		0.0,
		0.2,
		[]byte(`["gpt-5.5"]`),
		5,
		20,
		row.HourlyRate,
		row.HourlyFeeWaiverMinimum,
		1.0,
		false,
		99.0,
		99.0,
		service.PlatformOpenAI,
		service.AccountTypeOAuth,
		"pro",
		service.StatusActive,
		true,
		nil, // expires_at
		nil, // last_used_at
		nil, // rate_limited_at
		nil, // rate_limit_reset_at
		nil, // overload_until
		nil, // temp_unschedulable_until
		nil, // temp_unschedulable_reason
		nil, // session_window_start
		nil, // session_window_end
		nil, // session_window_status
		[]byte(`{}`),
		[]byte(`{}`),
		nil, // subscription_expires_at
		row.CurrentMembershipID,
		row.CurrentConsumerUserID,
		row.CurrentAPIKeyID,
		row.CurrentAPIKeyName,
		row.CurrentJoinedAt,
		row.CurrentPaidUntil,
		row.CurrentBilledUntil,
		row.CurrentIdleTimeoutMinutes,
		row.CurrentLastRequestAt,
		row.CurrentWaiverWindowStartedAt,
		row.CurrentWaiverWindowUsageAmount,
		row.CurrentWaiverWindowRequestCount,
		row.CurrentWaiverWindowLastRequestAt,
		nil, // queue_membership_id
		nil, // queue_api_key_id
		row.QueueAPIKeyName,
		nil, // queue_rank
		nil, // queue_status
		nil, // queue_idle_timeout_minutes
		nil, // queue_dispatch_cooldown_until
		nil, // last_used_membership_id
		nil, // last_used_at
		row.OwnerUserID,
		"owner",
		row.EditingExpiresAt,
		true,
		row.EditSessionID,
		now,
		now,
	}
	return sqlmock.NewRows(columns).AddRow(values...)
}
