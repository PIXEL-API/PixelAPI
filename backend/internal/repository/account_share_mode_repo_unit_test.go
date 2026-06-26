package repository

import (
	"context"
	"errors"
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
	mock.ExpectQuery("SELECT COUNT\\(\\*\\)::int").
		WithArgs(consumerUserID, apiKeyID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued).
		WillReturnRows(sqlmock.NewRows([]string{"count", "max", "active"}).AddRow(0, 0, false))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\)").
		WithArgs(listingID, service.AccountShareMembershipStatusActive, ownerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
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
			nil,
			nil,
			joinedAt,
			joinedAt,
		))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(accountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery("SELECT owner_share_ratio, platform_share_ratio, enabled, version").
		WithArgs(service.AccountShareModePolicyPlatformUnified).
		WillReturnRows(sqlmock.NewRows([]string{"owner_share_ratio", "platform_share_ratio", "enabled", "version"}).
			AddRow(0.9, 0.1, true, 1))
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
			"0.90000001",
			"0.09999999",
			60000,
			accountShareSeatSettlementTypeCharge,
			billedUntil,
			paidUntil,
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
			nil,
			nil,
			joinedAt,
			joinedAt,
		))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(accountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
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
			nil,
			nil,
			joinedAt,
			joinedAt,
		))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(accountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
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
			nil,
			nil,
			joinedAt,
			joinedAt,
		))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(accountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
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
			nil,
			nil,
			joinedAt,
			joinedAt,
		))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(accountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery("WITH usage_rows AS").
		WithArgs(membershipID, billedUntil, paidUntil).
		WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow("0.1300000000"))
	mock.ExpectQuery("INSERT INTO account_share_mode_settlement_entries").
		WithArgs(
			membershipID,
			listingID,
			accountID,
			ownerUserID,
			consumerUserID,
			apiKeyID,
			"0.20000000",
			3600000,
			accountShareSeatSettlementTypeWaiverRefund,
			billedUntil,
			paidUntil,
			"0.2000000000",
			"0.12000000",
			"0.1200000000",
			"0.1300000000",
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
			service.StatusError,
			false,
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
		if expectedSQL != "process unavailable memberships" {
			return nil
		}
		normalized := strings.ToLower(actualSQL)
		if !strings.Contains(normalized, "left join accounts a on a.id = m.account_id") {
			return errors.New("unavailable membership scan must include deleted or missing accounts")
		}
		if !strings.Contains(normalized, "a.deleted_at is not null") {
			return errors.New("unavailable membership scan must treat soft-deleted accounts as unavailable")
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

func TestAccountShareModeRepositoryDisablePermanentlyUnavailableListingsUsesPermanentConditionsOnly(t *testing.T) {
	matcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if expectedSQL != "disable permanent unavailable listings" {
			return nil
		}
		normalized := strings.ToLower(actualSQL)
		for _, forbidden := range []string{"overload_until", "rate_limit_reset_at", "temp_unschedulable_until", "codex_5h", "codex_7d"} {
			if strings.Contains(normalized, forbidden) {
				return errors.New("permanent listing disable must not use transient availability condition: " + forbidden)
			}
		}
		for _, required := range []string{
			"update account_share_listings",
			"a.deleted_at is not null",
			"a.status <> 'active'",
			"a.schedulable = false",
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
			if !strings.Contains(actualSQL, "seat_waiver_refund") || !strings.Contains(actualSQL, "e.membership_id = $5") {
				return errors.New("expected totals query to include waiver refund and membership filter")
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
			"hourly_charge",
			"hourly_refund",
			"hourly_waiver_refund",
			"last_activity_at",
		}).AddRow(int64(3), int64(100), int64(40), int64(10), int64(5), 1.2, 0.8, 0.1, 0.2, lastActivityAt))
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
	got := normalizeAccountShareModeRatio(0, service.AccountShareModeDefaultOwnerShareRatio)
	if !got.Equal(decimal.Zero) {
		t.Fatalf("expected explicit zero ratio to stay zero, got %s", got)
	}
}

func TestAccountShareModeSettlementRatiosClampPlatformOverflow(t *testing.T) {
	owner, platform := accountShareModeSettlementRatios(0.8, 0.5)
	if !owner.Equal(decimal.NewFromFloat(0.8)) {
		t.Fatalf("owner ratio = %s, want 0.8", owner)
	}
	if !platform.Equal(decimal.NewFromFloat(0.2)) {
		t.Fatalf("platform ratio = %s, want 0.2", platform)
	}
}

func accountShareListingRows(listingID, accountID, ownerUserID int64, editSessionID string, editingExpiresAt time.Time) *sqlmock.Rows {
	now := time.Now().UTC()
	return sqlmock.NewRows([]string{
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
		"current_api_key_id",
		"current_joined_at",
		"current_paid_until",
		"current_billed_until",
		"current_idle_timeout_minutes",
		"current_last_request_at",
		"queue_membership_id",
		"queue_api_key_id",
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
	}).AddRow(
		listingID,
		accountID,
		ownerUserID,
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
		0.15,
		0.0,
		1.0,
		false,
		99.0,
		99.0,
		service.PlatformOpenAI,
		service.AccountTypeOAuth,
		"pro",
		service.StatusActive,
		true,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		[]byte(`{}`),
		[]byte(`{}`),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		ownerUserID,
		"owner",
		editingExpiresAt,
		true,
		editSessionID,
		now,
		now,
	)
}
