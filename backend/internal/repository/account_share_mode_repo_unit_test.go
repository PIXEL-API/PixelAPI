package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
)

func TestAccountShareIdentityHintIsUnicodeSafe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		email string
		want  string
	}{
		{name: "ascii", email: "Alice@Example.COM", want: "a***e@example.com"},
		{name: "single ascii rune", email: "A@Example.COM", want: "a***@example.com"},
		{name: "single chinese rune", email: "中@例子.公司", want: "中***@例子.公司"},
		{name: "multiple chinese runes", email: "中文@例子.公司", want: "中***文@例子.公司"},
		{name: "missing local part", email: "@example.com", want: ""},
		{name: "multiple separators", email: "a@b@example.com", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := accountShareIdentityHint(tt.email)
			if got != tt.want {
				t.Fatalf("accountShareIdentityHint(%q) = %q, want %q", tt.email, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("accountShareIdentityHint(%q) returned invalid UTF-8: %q", tt.email, got)
			}
		})
	}
}

func TestAccountShareRoomRepresentativeJoinUsesIndexedPlacementCandidates(t *testing.T) {
	query := accountShareRoomRepresentativeJoinSQL("NOW()")
	normalized := strings.ToLower(strings.Join(strings.Fields(query), " "))

	required := []string{
		"from account_share_room_accounts room_account",
		"join accounts a on a.id = room_account.account_id",
		"where room_account.listing_id = l.id",
		"and room_account.state = 'active'",
		"room_account.priority asc",
	}
	for _, fragment := range required {
		if !strings.Contains(normalized, fragment) {
			t.Fatalf("representative account query must contain %q:\n%s", fragment, query)
		}
	}
	if strings.Contains(normalized, "account_external_placements") {
		t.Fatalf("representative account query must not read platform-mode placements:\n%s", query)
	}
}

func TestEnsureAccountShareMembershipBindingAssignmentBackfillsLegacyProjection(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	listingID := int64(700)
	accountID := int64(10)
	ownerUserID := int64(42)
	projectionCreatedAt := time.Date(2026, 7, 20, 9, 30, 0, 0, time.UTC)

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	mock.ExpectQuery("SELECT\\s+room_account.listing_id,\\s+room_account.account_id").
		WithArgs(listingID, accountID).
		WillReturnRows(sqlmock.NewRows([]string{
			"listing_id", "account_id", "owner_user_id", "name",
			"platform", "account_level", "concurrency", "created_at",
		}).AddRow(
			listingID,
			accountID,
			ownerUserID,
			"legacy-room-account",
			service.PlatformOpenAI,
			service.AccountLevelPlus,
			20,
			projectionCreatedAt,
		))
	mock.ExpectQuery("SELECT id, listing_id, account_id_snapshot").
		WithArgs(pq.Array([]int64{accountID})).
		WillReturnRows(sqlmock.NewRows([]string{"id", "listing_id", "account_id_snapshot"}))
	mock.ExpectQuery("INSERT INTO account_share_room_account_assignments").
		WithArgs(
			listingID,
			accountID,
			ownerUserID,
			"legacy-room-account",
			service.PlatformOpenAI,
			service.AccountLevelPlus,
			20,
			projectionCreatedAt,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(900)))

	if err := ensureAccountShareMembershipBindingAssignmentInTx(
		context.Background(),
		tx,
		listingID,
		accountID,
	); err != nil {
		t.Fatalf("ensure binding assignment: %v", err)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareListingSelectSQLPreservesViewerMembershipLifecycle(t *testing.T) {
	normalized := strings.ToLower(strings.Join(strings.Fields(accountShareListingSelectSQL()), " "))
	currentStart := strings.Index(normalized, "select m.id, m.consumer_user_id")
	queueStart := strings.Index(normalized, "select m.id, m.api_key_id, coalesce(ak.name, '') as api_key_name, m.queue_rank")
	historyStart := strings.Index(normalized, "select m.id, coalesce(m.ended_at, m.updated_at) as ended_at")
	if currentStart < 0 || queueStart <= currentStart || historyStart <= queueStart {
		t.Fatalf("membership lifecycle projections are missing or out of order:\n%s", normalized)
	}

	currentProjection := normalized[currentStart:queueStart]
	for _, fragment := range []string{
		"m.consumer_user_id = $1",
		"m.status in ('active', 'ending')",
		"and ( m.status = 'ending' or ( (m.hourly_rate_snapshot <= 0 or m.paid_until is null or m.paid_until > now()) and (m.idle_timeout_minutes <= 0",
	} {
		if !strings.Contains(currentProjection, fragment) {
			t.Fatalf("current membership projection must contain %q:\n%s", fragment, currentProjection)
		}
	}

	queueProjection := normalized[queueStart:historyStart]
	for _, fragment := range []string{
		"m.consumer_user_id = $1",
		"m.status in ('active', 'queued', 'ending')",
	} {
		if !strings.Contains(queueProjection, fragment) {
			t.Fatalf("queue membership projection must contain %q:\n%s", fragment, queueProjection)
		}
	}

	historyProjection := normalized[historyStart:]
	for _, fragment := range []string{
		"m.consumer_user_id = $1",
		"m.status = 'ended'",
	} {
		if !strings.Contains(historyProjection, fragment) {
			t.Fatalf("history membership projection must contain %q:\n%s", fragment, historyProjection)
		}
	}
}

func TestAccountShareModeRepositoryListListingsSearchUsesViewOwnedProjection(t *testing.T) {
	queryErr := errors.New("stop after search query validation")
	checkCurrentProjectionSearch := func(normalized string) error {
		for _, fragment := range []string{
			"l.room_name ilike $2",
			"a.name ilike $2",
			"coalesce(u.username, '') ilike $2",
			"l.id::text ilike $2",
			"l.owner_user_id::text ilike $2",
			"jsonb_array_elements_text(l.allowed_models)",
		} {
			if !strings.Contains(normalized, fragment) {
				return fmt.Errorf("current projection search query missing %q", fragment)
			}
		}
		if strings.Contains(normalized, "account_share_listing_revisions deleted_revision") {
			return errors.New("current projection search must not depend on deleted revision snapshots")
		}
		return nil
	}
	tests := []struct {
		name    string
		filters service.AccountShareListingFilters
		check   func(string) error
	}{
		{
			name: "archive uses only trusted deleted revision text",
			filters: service.AccountShareListingFilters{
				Tab:       service.AccountShareModeListingTabArchive,
				Search:    "needle",
				SkipTotal: true,
			},
			check: func(normalized string) error {
				for _, fragment := range []string{
					"l.id::text ilike $2",
					"l.owner_user_id::text ilike $2",
					"from account_share_listing_revisions deleted_revision",
					"deleted_revision.id = l.deleted_revision_id",
					"deleted_revision.listing_id = l.id",
					"deleted_revision.revision_number > 0",
					"deleted_revision.schema_version > 0",
					"deleted_revision.snapshot_quality in ('exact', 'backfilled_current')",
					"jsonb_typeof(deleted_revision.allowed_models) = 'array'",
					"and not exists ( select 1 from jsonb_array_elements( case when jsonb_typeof(deleted_revision.allowed_models) = 'array'",
					") as allowed_model(value) where jsonb_typeof(allowed_model.value) <> 'string' )",
					"deleted_revision.room_name ilike $2",
					"deleted_revision.owner_display_name_snapshot ilike $2",
					"jsonb_array_elements_text( case when jsonb_typeof(deleted_revision.allowed_models) = 'array'",
					"else '[]'::jsonb end ) as model(value)",
				} {
					if !strings.Contains(normalized, fragment) {
						return fmt.Errorf("archive search query missing %q", fragment)
					}
				}
				for _, fragment := range []string{
					"l.room_name ilike $2",
					"a.name ilike $2",
					"coalesce(u.username, '') ilike $2",
					"jsonb_array_elements_text(l.allowed_models)",
				} {
					if strings.Contains(normalized, fragment) {
						return fmt.Errorf("archive search query used mutable field %q", fragment)
					}
				}
				return nil
			},
		},
		{
			name: "ordinary listing keeps current projection search",
			filters: service.AccountShareListingFilters{
				Search:    "needle",
				SkipTotal: true,
			},
			check: checkCurrentProjectionSearch,
		},
		{
			name: "history keeps current projection search",
			filters: service.AccountShareListingFilters{
				Tab:       service.AccountShareModeListingTabHistory,
				Search:    "needle",
				SkipTotal: true,
			},
			check: checkCurrentProjectionSearch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queryMatcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
				if expectedSQL != "listing search" {
					return nil
				}
				normalized := strings.ToLower(strings.Join(strings.Fields(actualSQL), " "))
				return tt.check(normalized)
			})
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(queryMatcher))
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			defer func() { _ = db.Close() }()
			repo := &accountShareModeRepository{db: db}

			mock.ExpectQuery("listing search").
				WithArgs(int64(42), "%needle%", 21, 0).
				WillReturnError(queryErr)

			_, _, err = repo.ListListings(
				context.Background(),
				42,
				tt.filters,
				pagination.PaginationParams{Page: 1, PageSize: 20},
			)
			if !errors.Is(err, queryErr) {
				t.Fatalf("ListListings error = %v, want query sentinel", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet expectations: %v", err)
			}
		})
	}
}

func TestScanAccountShareListingProjectsMembershipLifecycleStates(t *testing.T) {
	const viewerUserID int64 = 5926
	tests := []struct {
		name                string
		configure           func(*accountShareListingRowData)
		wantCurrentID       int64
		wantQueueID         int64
		wantQueueStatus     string
		wantLastUsedID      int64
		wantCurrentPresence bool
		wantQueuePresence   bool
		wantHistoryPresence bool
	}{
		{
			name: "active",
			configure: func(row *accountShareListingRowData) {
				row.CurrentMembershipID = int64(101)
				row.CurrentConsumerUserID = viewerUserID
				row.QueueMembershipID = int64(101)
				row.QueueStatus = service.AccountShareMembershipStatusActive
			},
			wantCurrentID:       101,
			wantQueueID:         101,
			wantQueueStatus:     service.AccountShareMembershipStatusActive,
			wantCurrentPresence: true,
			wantQueuePresence:   true,
		},
		{
			name: "queued",
			configure: func(row *accountShareListingRowData) {
				row.QueueMembershipID = int64(102)
				row.QueueStatus = service.AccountShareMembershipStatusQueued
			},
			wantQueueID:       102,
			wantQueueStatus:   service.AccountShareMembershipStatusQueued,
			wantQueuePresence: true,
		},
		{
			name: "ending",
			configure: func(row *accountShareListingRowData) {
				row.CurrentMembershipID = int64(103)
				row.CurrentConsumerUserID = viewerUserID
				row.QueueMembershipID = int64(103)
				row.QueueStatus = service.AccountShareMembershipStatusEnding
			},
			wantCurrentID:       103,
			wantQueueID:         103,
			wantQueueStatus:     service.AccountShareMembershipStatusEnding,
			wantCurrentPresence: true,
			wantQueuePresence:   true,
		},
		{
			name: "ended",
			configure: func(row *accountShareListingRowData) {
				row.LastUsedMembershipID = int64(104)
				row.LastUsedAt = time.Date(2026, 7, 27, 9, 0, 0, 0, time.UTC)
			},
			wantLastUsedID:      104,
			wantHistoryPresence: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			defer func() { _ = db.Close() }()

			mock.ExpectQuery("SELECT lifecycle_projection").
				WillReturnRows(accountShareListingRows(7, 8, 9, "", time.Time{}, tt.configure))
			listing, err := scanAccountShareListing(
				db.QueryRowContext(context.Background(), "SELECT lifecycle_projection"),
			)
			if err != nil {
				t.Fatalf("scanAccountShareListing: %v", err)
			}

			if got := listing.CurrentMembershipID != nil; got != tt.wantCurrentPresence {
				t.Fatalf("current membership presence = %v, want %v: %#v", got, tt.wantCurrentPresence, listing)
			}
			if tt.wantCurrentPresence && *listing.CurrentMembershipID != tt.wantCurrentID {
				t.Fatalf("current membership id = %d, want %d", *listing.CurrentMembershipID, tt.wantCurrentID)
			}
			if got := listing.QueueMembershipID != nil; got != tt.wantQueuePresence {
				t.Fatalf("queue membership presence = %v, want %v: %#v", got, tt.wantQueuePresence, listing)
			}
			if tt.wantQueuePresence {
				if *listing.QueueMembershipID != tt.wantQueueID {
					t.Fatalf("queue membership id = %d, want %d", *listing.QueueMembershipID, tt.wantQueueID)
				}
				if listing.QueueStatus != tt.wantQueueStatus {
					t.Fatalf("queue status = %q, want %q", listing.QueueStatus, tt.wantQueueStatus)
				}
			}
			if got := listing.LastUsedMembershipID != nil; got != tt.wantHistoryPresence {
				t.Fatalf("history membership presence = %v, want %v: %#v", got, tt.wantHistoryPresence, listing)
			}
			if tt.wantHistoryPresence && *listing.LastUsedMembershipID != tt.wantLastUsedID {
				t.Fatalf("last used membership id = %d, want %d", *listing.LastUsedMembershipID, tt.wantLastUsedID)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet expectations: %v", err)
			}
		})
	}
}

func TestScanAccountShareListingProjectsRepresentativeAccountEligibility(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	expiresAt := time.Date(2026, 7, 27, 9, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT representative_eligibility").
		WillReturnRows(accountShareListingRows(7, 8, 9, "", time.Time{}, func(row *accountShareListingRowData) {
			row.RepresentativeAccountConcurrency = 0
			row.RepresentativeAccountAutoPauseOnExpired = true
			row.AccountExpiresAt = expiresAt
		}))

	listing, err := scanAccountShareListing(
		db.QueryRowContext(context.Background(), "SELECT representative_eligibility"),
	)
	if err != nil {
		t.Fatalf("scanAccountShareListing: %v", err)
	}
	if listing.RepresentativeAccountConcurrency != 0 {
		t.Fatalf("representative concurrency = %d, want 0", listing.RepresentativeAccountConcurrency)
	}
	if !listing.RepresentativeAccountAutoPauseOnExpired {
		t.Fatal("representative auto-pause-on-expired was not projected")
	}
	if listing.AccountExpiresAt == nil || !listing.AccountExpiresAt.Equal(expiresAt) {
		t.Fatalf("representative expires_at = %v, want %v", listing.AccountExpiresAt, expiresAt)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareAccountUnavailableConditionSQLIncludesConfiguredConcurrencyAndAutomaticExpiry(t *testing.T) {
	normalized := strings.ToLower(strings.Join(
		strings.Fields(accountShareAccountUnavailableConditionSQL("$1")),
		" ",
	))
	for _, required := range []string{
		"a.concurrency <= 0",
		"a.auto_pause_on_expired = true",
		"a.expires_at is not null",
		"a.expires_at <= $1",
	} {
		if !strings.Contains(normalized, required) {
			t.Fatalf("account unavailable SQL missing %q: %s", required, normalized)
		}
	}
}

func TestAccountShareModeRepositoryListListingsRestoresEndingMembershipAfterRefresh(t *testing.T) {
	queryMatcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		normalized := strings.ToLower(strings.Join(strings.Fields(actualSQL), " "))
		switch expectedSQL {
		case "ending membership count":
			for _, fragment := range []string{
				"m.consumer_user_id = $1",
				"m.status in ('active', 'queued', 'ending')",
				"qm.id is not null",
			} {
				if !strings.Contains(normalized, fragment) {
					return fmt.Errorf("ending membership count query missing %q: %s", fragment, normalized)
				}
			}
		case "ending membership listing":
			for _, fragment := range []string{
				"m.consumer_user_id = $1",
				"m.status in ('active', 'ending')",
				"m.status in ('active', 'queued', 'ending')",
				"m.status = 'ending' or ( (m.hourly_rate_snapshot <= 0",
				"qm.id is not null",
			} {
				if !strings.Contains(normalized, fragment) {
					return fmt.Errorf("ending membership listing query missing %q: %s", fragment, normalized)
				}
			}
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(queryMatcher))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	const (
		viewerUserID int64 = 5926
		membershipID int64 = 18012
		apiKeyID     int64 = 15007
	)
	const operationID = "ca292d86-824f-4ac0-b10a-b9436b8f2669"
	joinedAt := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	expiredAt := joinedAt.Add(30 * time.Minute)

	mock.ExpectQuery("ending membership count").
		WithArgs(viewerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectQuery("ending membership listing").
		WithArgs(viewerUserID, 20, 0).
		WillReturnRows(accountShareListingRows(
			510,
			405606,
			7001,
			"",
			time.Time{},
			func(row *accountShareListingRowData) {
				row.CurrentMembershipID = membershipID
				row.CurrentConsumerUserID = viewerUserID
				row.CurrentAPIKeyID = apiKeyID
				row.CurrentAPIKeyName = "coding-key"
				row.CurrentJoinedAt = joinedAt
				row.CurrentPaidUntil = expiredAt
				row.CurrentIdleTimeoutMinutes = 15
				row.QueueMembershipID = membershipID
				row.QueueAPIKeyID = apiKeyID
				row.QueueAPIKeyName = "coding-key"
				row.QueueStatus = service.AccountShareMembershipStatusEnding
				row.QueueEndingOperationID = operationID
				row.QueueEndingOperationStatus = "running"
				row.QueueSettlementStatus = "pending"
			},
		))

	listings, result, err := repo.ListListings(
		context.Background(),
		viewerUserID,
		service.AccountShareListingFilters{Tab: service.AccountShareModeListingTabUsing},
		pagination.PaginationParams{Page: 1, PageSize: 20},
	)
	if err != nil {
		t.Fatalf("ListListings using tab: %v", err)
	}
	if result == nil || result.Total != 1 || len(listings) != 1 {
		t.Fatalf("ending membership list result = %#v, listings=%d", result, len(listings))
	}
	listing := listings[0]
	if listing.CurrentMembershipID == nil || *listing.CurrentMembershipID != membershipID {
		t.Fatalf("current membership was not restored: %#v", listing)
	}
	if listing.QueueMembershipID == nil || *listing.QueueMembershipID != membershipID {
		t.Fatalf("ending lifecycle membership was not restored: %#v", listing)
	}
	if listing.QueueStatus != service.AccountShareMembershipStatusEnding {
		t.Fatalf("queue status = %q, want %q", listing.QueueStatus, service.AccountShareMembershipStatusEnding)
	}
	if listing.QueueEndingOperationID != operationID ||
		listing.QueueEndingOperationStatus != "running" ||
		listing.QueueSettlementStatus != "pending" {
		t.Fatalf("ending operation projection is incomplete: %#v", listing)
	}
	if listing.CurrentAPIKeyID == nil || *listing.CurrentAPIKeyID != apiKeyID ||
		listing.QueueAPIKeyID == nil || *listing.QueueAPIKeyID != apiKeyID {
		t.Fatalf("ending membership API key projection is incomplete: %#v", listing)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

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
		WithArgs(
			int64(7),
			int64(42),
			service.AccountShareMembershipStatusActive,
			service.AccountShareMembershipStatusQueued,
			service.AccountShareMembershipStatusEnding,
		).
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

func TestAccountShareModeRepositoryListAPIKeyBindingMembershipsIncludesEndingState(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := &accountShareModeRepository{db: db}
	consumerUserID := int64(7)
	apiKeyID := int64(42)
	now := time.Date(2026, 7, 28, 8, 0, 0, 0, time.UTC)
	endingRequestedAt := now.Add(time.Minute)
	operationID := "00000000-0000-4000-8000-000000000003"

	mock.ExpectQuery(`(?s)SELECT\s+m\.id.*AND m\.status IN \(\$3, \$4, \$5\).*ORDER BY m\.queue_rank ASC, m\.id ASC`).
		WithArgs(
			consumerUserID,
			apiKeyID,
			service.AccountShareMembershipStatusActive,
			service.AccountShareMembershipStatusQueued,
			service.AccountShareMembershipStatusEnding,
		).
		WillReturnRows(
			sqlmock.NewRows(accountShareMembershipColumns()).
				AddRow(accountShareEndMembershipRow(
					1,
					11,
					int64(101),
					20,
					consumerUserID,
					apiKeyID,
					service.AccountShareMembershipStatusActive,
					now,
					now,
				)...).
				AddRow(accountShareEndMembershipRow(
					2,
					12,
					nil,
					21,
					consumerUserID,
					apiKeyID,
					service.AccountShareMembershipStatusQueued,
					now,
					now,
				)...).
				AddRow(accountShareEndMembershipRow(
					3,
					13,
					int64(103),
					22,
					consumerUserID,
					apiKeyID,
					service.AccountShareMembershipStatusEnding,
					now,
					now,
				)...),
		)
	mock.ExpectQuery(`(?s)SELECT\s+membership\.id,\s+membership\.ending_requested_at.*LEFT JOIN account_share_room_operations operation`).
		WithArgs(sqlmock.AnyArg(), consumerUserID, apiKeyID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"ending_requested_at",
			"ending_reason",
			"settlement_status",
			"ending_operation_id",
			"ending_operation_status",
		}).AddRow(
			int64(3),
			endingRequestedAt,
			service.AccountShareMembershipEndReasonManual,
			"pending",
			operationID,
			"needs_attention",
		))

	memberships, err := repo.ListAPIKeyBindingMemberships(
		context.Background(),
		consumerUserID,
		apiKeyID,
	)
	if err != nil {
		t.Fatalf("ListAPIKeyBindingMemberships: %v", err)
	}
	if len(memberships) != 3 {
		t.Fatalf("memberships = %d, want 3", len(memberships))
	}
	ending := memberships[2]
	if ending.Status != service.AccountShareMembershipStatusEnding ||
		ending.EndingRequestedAt == nil ||
		!ending.EndingRequestedAt.Equal(endingRequestedAt) ||
		ending.EndingReason != service.AccountShareMembershipEndReasonManual ||
		ending.SettlementStatus != "pending" ||
		ending.EndingOperationID != operationID ||
		ending.EndingOperationStatus != "needs_attention" {
		t.Fatalf("unexpected ending membership: %#v", ending)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestLockAccountShareJoinAPIKeyInTxRejectsMissingOrForeignKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	mock.ExpectQuery("SELECT\\s+name\\s+FROM api_keys").
		WithArgs(int64(42), int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"name"}))

	_, err = lockAccountShareJoinAPIKeyInTx(context.Background(), tx, 42, 7)
	if !errors.Is(err, service.ErrAPIKeyNotFound) {
		t.Fatalf("expected missing or foreign API key to fail closed, got %v", err)
	}

	mock.ExpectRollback()
	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		t.Fatalf("Rollback: %v", rollbackErr)
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
	expectedVersion := int64(1)

	mock.ExpectBegin()
	mock.ExpectQuery(accountShareUpdateListingLockQueryPattern).
		WithArgs(int64(7), int64(42)).
		WillReturnRows(accountShareUpdateListingLockRows())
	mock.ExpectRollback()

	name := "renamed-room"
	_, err = repo.UpdateListing(context.Background(), 42, false, 7, service.UpdateAccountShareListingInput{
		Name:            &name,
		ExpectedVersion: &expectedVersion,
		Reason:          "rename room",
	})
	if !errors.Is(err, service.ErrAccountShareListingNotFound) {
		t.Fatalf("expected not found for non-owner listing, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryUpdateListingRequiresExpectedVersion(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	name := "renamed-room"
	_, err = repo.UpdateListing(context.Background(), 42, false, 7, service.UpdateAccountShareListingInput{Name: &name})
	if !errors.Is(err, service.ErrAccountShareExpectedVersionRequired) {
		t.Fatalf("expected missing expected_version rejection, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryUpdateListingRequiresReasonForEveryConfigUpdate(t *testing.T) {
	tests := []struct {
		name         string
		force        bool
		actorIsAdmin bool
		wantErr      error
	}{
		{
			name:    "owner update",
			wantErr: service.ErrAccountShareUpdateReasonRequired,
		},
		{
			name:         "admin force update",
			force:        true,
			actorIsAdmin: true,
			wantErr:      service.ErrAccountShareForceReasonRequired,
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
			expectedVersion := int64(1)
			name := "renamed-room"

			_, err = repo.UpdateListing(context.Background(), 42, tt.actorIsAdmin, 7, service.UpdateAccountShareListingInput{
				Name:            &name,
				ExpectedVersion: &expectedVersion,
				ForceActiveEdit: tt.force,
				Reason:          " \t ",
				Confirmed:       tt.force,
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("UpdateListing error = %v, want %v", err, tt.wantErr)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("reason validation must fail before database access: %v", err)
			}
		})
	}
}

func TestAccountShareModeRepositoryUpdateListingRejectsNonAdminForceBeforeReasonValidation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	expectedVersion := int64(1)
	name := "renamed-room"

	_, err = repo.UpdateListing(context.Background(), 42, false, 7, service.UpdateAccountShareListingInput{
		Name:            &name,
		ExpectedVersion: &expectedVersion,
		ForceActiveEdit: true,
		Reason:          "",
		Confirmed:       true,
	})
	if !errors.Is(err, service.ErrAccountShareForceAdminRequired) {
		t.Fatalf("UpdateListing error = %v, want %v", err, service.ErrAccountShareForceAdminRequired)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("authorization validation must fail before database access: %v", err)
	}
}

func TestAccountShareModeRepositoryUpdateListingRejectsStaleVersion(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	expectedVersion := int64(1)
	actualVersion := int64(2)
	name := "renamed-room"

	mock.ExpectBegin()
	mock.ExpectQuery(accountShareUpdateListingLockQueryPattern).
		WithArgs(int64(7), int64(42)).
		WillReturnRows(accountShareUpdateListingLockRows(func(row *accountShareUpdateListingLockRowData) {
			row.OwnerUserID = 42
			row.RowVersion = actualVersion
		}))
	mock.ExpectRollback()

	_, err = repo.UpdateListing(context.Background(), 42, false, 7, service.UpdateAccountShareListingInput{
		Name:            &name,
		ExpectedVersion: &expectedVersion,
		Reason:          "rename room",
	})
	if !errors.Is(err, service.ErrAccountShareVersionConflict) {
		t.Fatalf("expected stale version conflict, got %v", err)
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
	expectedVersion := int64(1)
	name := "renamed-room"

	mock.ExpectBegin()
	mock.ExpectQuery(accountShareUpdateListingLockQueryPattern).
		WithArgs(int64(7)).
		WillReturnRows(accountShareUpdateListingLockRows(func(row *accountShareUpdateListingLockRowData) {
			row.OwnerUserID = 50
			row.RowVersion = expectedVersion
		}))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs("account_share_room_name:50:renamed-room").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT l\\.id\\s+FROM account_share_listings l").
		WithArgs(int64(50), name, int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec("UPDATE account_share_listings").
		WithArgs(name, int64(7), expectedVersion).
		WillReturnError(updateErr)
	mock.ExpectRollback()

	_, err = repo.UpdateListing(context.Background(), 42, true, 7, service.UpdateAccountShareListingInput{
		Name:            &name,
		ExpectedVersion: &expectedVersion,
		Reason:          "admin rename room",
	})
	if !errors.Is(err, updateErr) {
		t.Fatalf("expected update error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryUpdateListingWritesRevisionAndAuditEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	listingID := int64(7)
	ownerUserID := int64(42)
	expectedVersion := int64(1)
	nextVersion := int64(2)
	revisionID := int64(701)
	name := "renamed-room"
	reason := "clarify room name"

	mock.ExpectBegin()
	mock.ExpectQuery(accountShareUpdateListingLockQueryPattern).
		WithArgs(listingID, ownerUserID).
		WillReturnRows(accountShareUpdateListingLockRows(func(row *accountShareUpdateListingLockRowData) {
			row.OwnerUserID = ownerUserID
			row.RowVersion = expectedVersion
		}))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").
		WithArgs("account_share_room_name:42:renamed-room").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT l\\.id\\s+FROM account_share_listings l").
		WithArgs(ownerUserID, name, listingID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec("UPDATE account_share_listings").
		WithArgs(name, listingID, ownerUserID, expectedVersion).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT\\s+l\\.id, l\\.row_version").
		WithArgs(listingID).
		WillReturnRows(accountShareRevisionSnapshotRows(
			listingID,
			nextVersion,
			name,
			ownerUserID,
			"owner",
		))
	mock.ExpectQuery("INSERT INTO account_share_listing_revisions").
		WithArgs(
			listingID,
			nextVersion,
			1,
			service.AccountShareSnapshotQualityExact,
			name,
			service.PlatformOpenAI,
			"pro",
			ownerUserID,
			"owner",
			service.AccountShareListingStatusActive,
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
			ownerUserID,
			"owner",
			"update_listing",
			reason,
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
			"listing.updated",
			ownerUserID,
			"owner",
			reason,
			`{"changed_fields":["room_name"],"force_applied":false,"row_version":2,"source":"update_listing"}`,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery("SELECT\\s+l\\.id").
		WithArgs(ownerUserID, listingID).
		WillReturnRows(accountShareListingRows(listingID, 99, ownerUserID, "", time.Time{}, func(row *accountShareListingRowData) {
			row.RowVersion = nextVersion
			row.CurrentRevisionID = revisionID
			row.RoomName = name
		}))

	listing, err := repo.UpdateListing(context.Background(), ownerUserID, false, listingID, service.UpdateAccountShareListingInput{
		Name:            &name,
		ExpectedVersion: &expectedVersion,
		Reason:          "  " + reason + "  ",
	})
	if err != nil {
		t.Fatalf("UpdateListing failed: %v", err)
	}
	if listing.RowVersion != nextVersion || listing.CurrentRevisionID == nil || *listing.CurrentRevisionID != revisionID {
		t.Fatalf("unexpected revision state: row_version=%d current_revision_id=%v", listing.RowVersion, listing.CurrentRevisionID)
	}
	if listing.RoomName != name {
		t.Fatalf("room name = %q, want %q", listing.RoomName, name)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryAdminForceUpdateWritesReasonedRevision(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	listingID := int64(7)
	ownerUserID := int64(42)
	adminUserID := int64(9)
	expectedVersion := int64(1)
	nextVersion := int64(2)
	revisionID := int64(702)
	seatLimit := 5
	editSessionID := "admin-edit"
	editExpiresAt := time.Now().UTC().Add(10 * time.Minute)
	reason := "emergency capacity correction"

	mock.ExpectBegin()
	mock.ExpectQuery(accountShareUpdateListingLockQueryPattern).
		WithArgs(listingID).
		WillReturnRows(accountShareUpdateListingLockRows(func(row *accountShareUpdateListingLockRowData) {
			row.OwnerUserID = ownerUserID
			row.RowVersion = expectedVersion
			row.EditSessionID = editSessionID
			row.EditingByUserID = adminUserID
			row.EditingExpiresAt = editExpiresAt
		}))
	mock.ExpectExec("UPDATE account_share_listings").
		WithArgs(seatLimit, listingID, expectedVersion).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT\\s+l\\.id, l\\.row_version").
		WithArgs(listingID).
		WillReturnRows(accountShareRevisionSnapshotRows(
			listingID,
			nextVersion,
			"shared-room",
			ownerUserID,
			"owner",
			func(row *accountShareRevisionSourceRowData) {
				row.SeatLimit = seatLimit
			},
		))
	mock.ExpectQuery("INSERT INTO account_share_listing_revisions").
		WithArgs(
			listingID,
			nextVersion,
			1,
			service.AccountShareSnapshotQualityExact,
			"shared-room",
			service.PlatformOpenAI,
			"pro",
			ownerUserID,
			"owner",
			service.AccountShareListingStatusActive,
			seatLimit,
			0.2,
			`["gpt-5.5"]`,
			5,
			0.15,
			0.0,
			1.0,
			false,
			99.0,
			99.0,
			adminUserID,
			"admin",
			"update_listing",
			reason,
			nil,
			true,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(revisionID))
	mock.ExpectExec("UPDATE account_share_listings\\s+SET current_revision_id").
		WithArgs(revisionID, listingID, nextVersion).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO account_share_room_events").
		WithArgs(
			listingID,
			revisionID,
			"listing.updated",
			adminUserID,
			"admin",
			reason,
			`{"changed_fields":["seat_limit"],"force_applied":true,"row_version":2,"source":"update_listing"}`,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery("SELECT\\s+l\\.id").
		WithArgs(ownerUserID, listingID).
		WillReturnRows(accountShareListingRows(listingID, 99, ownerUserID, "", time.Time{}, func(row *accountShareListingRowData) {
			row.RowVersion = nextVersion
			row.CurrentRevisionID = revisionID
		}))

	listing, err := repo.UpdateListing(context.Background(), adminUserID, true, listingID, service.UpdateAccountShareListingInput{
		SeatLimit:       &seatLimit,
		EditSessionID:   editSessionID,
		ForceActiveEdit: true,
		ExpectedVersion: &expectedVersion,
		Reason:          reason,
		Confirmed:       true,
	})
	if err != nil {
		t.Fatalf("admin force UpdateListing failed: %v", err)
	}
	if listing.RowVersion != nextVersion || listing.CurrentRevisionID == nil || *listing.CurrentRevisionID != revisionID {
		t.Fatalf("unexpected revision state: row_version=%d current_revision_id=%v", listing.RowVersion, listing.CurrentRevisionID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryUpdateListingDoesNotSyncAllowedModelsToRoomAccounts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}
	revisionErr := errors.New("stop before revision materialization")
	models := []string{"gpt-5.5", "gpt-5.4"}
	expectedVersion := int64(1)
	editSessionID := "edit-session"
	editExpiresAt := time.Now().UTC().Add(10 * time.Minute)

	mock.ExpectBegin()
	mock.ExpectQuery(accountShareUpdateListingLockQueryPattern).
		WithArgs(int64(7), int64(42)).
		WillReturnRows(accountShareUpdateListingLockRows(func(row *accountShareUpdateListingLockRowData) {
			row.OwnerUserID = 42
			row.Status = service.AccountShareListingStatusPaused
			row.RowVersion = expectedVersion
			row.EditSessionID = editSessionID
			row.EditingByUserID = int64(42)
			row.EditingExpiresAt = editExpiresAt
		}))
	expectAccountShareEditDatabaseBlockers(mock, int64(7), 0, 0, 0, 0)
	mock.ExpectQuery("SELECT account_id\\s+FROM account_share_room_accounts").
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"account_id"}).AddRow(int64(10)))
	mock.ExpectQuery(`(?s)SELECT\s+a\.id, a\.name, a\.platform, a\.account_level, a\.concurrency, a\.priority,.*a\.auto_pause_on_expired.*AS schedulable`).
		WithArgs(pq.Array([]int64{10}), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "platform", "account_level", "concurrency", "priority",
			"status", "schedulable", "type", "credentials", "extra",
		}).AddRow(
			int64(10),
			"room-account",
			service.PlatformOpenAI,
			service.AccountLevelPro,
			5,
			1,
			service.StatusActive,
			true,
			service.AccountTypeOAuth,
			`{"model_mapping":{"gpt-5.5":"gpt-5.5","gpt-5.4":"gpt-5.4"}}`,
			`{}`,
		))
	mock.ExpectExec("UPDATE account_share_listings").
		WithArgs(`["gpt-5.5","gpt-5.4"]`, int64(7), int64(42), expectedVersion).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT\\s+l\\.id, l\\.row_version").
		WithArgs(int64(7)).
		WillReturnError(revisionErr)
	mock.ExpectRollback()

	_, err = repo.UpdateListing(context.Background(), 42, false, 7, service.UpdateAccountShareListingInput{
		AllowedModels:   &models,
		EditSessionID:   editSessionID,
		ExpectedVersion: &expectedVersion,
		Reason:          "update supported models",
	})
	if !errors.Is(err, revisionErr) {
		t.Fatalf("expected revision sentinel error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryUpdateListingRejectsModelUnsupportedByCurrentRoomAccount(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}
	models := []string{"gpt-5.5", "gpt-5.4"}
	expectedVersion := int64(1)
	editSessionID := "edit-session"
	editExpiresAt := time.Now().UTC().Add(10 * time.Minute)

	mock.ExpectBegin()
	mock.ExpectQuery(accountShareUpdateListingLockQueryPattern).
		WithArgs(int64(7), int64(42)).
		WillReturnRows(accountShareUpdateListingLockRows(func(row *accountShareUpdateListingLockRowData) {
			row.OwnerUserID = 42
			row.Status = service.AccountShareListingStatusPaused
			row.RowVersion = expectedVersion
			row.EditSessionID = editSessionID
			row.EditingByUserID = int64(42)
			row.EditingExpiresAt = editExpiresAt
		}))
	expectAccountShareEditDatabaseBlockers(mock, int64(7), 0, 0, 0, 0)
	mock.ExpectQuery("SELECT account_id\\s+FROM account_share_room_accounts").
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"account_id"}).AddRow(int64(10)))
	mock.ExpectQuery(`(?s)SELECT\s+a\.id, a\.name, a\.platform, a\.account_level, a\.concurrency, a\.priority,.*a\.auto_pause_on_expired.*AS schedulable`).
		WithArgs(pq.Array([]int64{10}), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "platform", "account_level", "concurrency", "priority",
			"status", "schedulable", "type", "credentials", "extra",
		}).AddRow(
			int64(10),
			"room-account",
			service.PlatformOpenAI,
			service.AccountLevelPro,
			5,
			1,
			service.StatusActive,
			true,
			service.AccountTypeOAuth,
			`{"model_mapping":{"gpt-5.5":"gpt-5.5"}}`,
			`{}`,
		))
	mock.ExpectRollback()

	_, err = repo.UpdateListing(context.Background(), 42, false, 7, service.UpdateAccountShareListingInput{
		AllowedModels:   &models,
		EditSessionID:   editSessionID,
		ExpectedVersion: &expectedVersion,
		Reason:          "update supported models",
	})

	if !errors.Is(err, service.ErrAccountShareModeUnsupportedModel) {
		t.Fatalf("expected unsupported model rejection, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryUpdateActiveEmptyListingDoesNotDependOnRoomAccountConcurrency(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}
	revisionErr := errors.New("stop after independent seat and concurrency update")
	editSessionID := "edit-session"
	editExpiresAt := time.Now().UTC().Add(10 * time.Minute)
	seatLimit := service.AccountShareModeMaxSeats
	perUserConcurrency := service.AccountShareModeMaxPerUserConcurrency
	expectedVersion := int64(1)

	mock.ExpectBegin()
	mock.ExpectQuery(accountShareUpdateListingLockQueryPattern).
		WithArgs(int64(7), int64(42)).
		WillReturnRows(accountShareUpdateListingLockRows(func(row *accountShareUpdateListingLockRowData) {
			row.OwnerUserID = 42
			row.Status = service.AccountShareListingStatusActive
			row.RowVersion = expectedVersion
			row.EditSessionID = editSessionID
			row.EditingByUserID = int64(42)
			row.EditingExpiresAt = editExpiresAt
		}))
	expectAccountShareEditDatabaseBlockers(mock, int64(7), 0, 0, 0, 0)
	mock.ExpectExec("UPDATE account_share_listings").
		WithArgs(seatLimit, perUserConcurrency, int64(7), int64(42), expectedVersion).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT\\s+l\\.id, l\\.row_version").
		WithArgs(int64(7)).
		WillReturnError(revisionErr)
	mock.ExpectRollback()

	_, err = repo.UpdateListing(context.Background(), 42, false, 7, service.UpdateAccountShareListingInput{
		SeatLimit:          &seatLimit,
		PerUserConcurrency: &perUserConcurrency,
		EditSessionID:      editSessionID,
		ExpectedVersion:    &expectedVersion,
		Reason:             "adjust room capacity",
	})
	if !errors.Is(err, revisionErr) {
		t.Fatalf("expected update to reach commit without reading room account concurrency, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryUpdateListingRejectsFinancialBlockers(t *testing.T) {
	tests := []struct {
		name                    string
		synchronousBillingCount int
	}{
		{
			name:                    "membership settlement pending",
			synchronousBillingCount: 1,
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
			expectedVersion := int64(1)
			seatLimit := 5
			expiresAt := time.Now().UTC().Add(10 * time.Minute)

			mock.ExpectBegin()
			mock.ExpectQuery(accountShareUpdateListingLockQueryPattern).
				WithArgs(int64(7), int64(42)).
				WillReturnRows(accountShareUpdateListingLockRows(func(row *accountShareUpdateListingLockRowData) {
					row.OwnerUserID = 42
					row.Status = service.AccountShareListingStatusActive
					row.RowVersion = expectedVersion
					row.EditSessionID = "edit-session"
					row.EditingByUserID = int64(42)
					row.EditingExpiresAt = expiresAt
				}))
			expectAccountShareEditDatabaseBlockers(
				mock,
				int64(7),
				0,
				0,
				0,
				tt.synchronousBillingCount,
			)
			mock.ExpectRollback()

			_, err = repo.UpdateListing(context.Background(), 42, false, 7, service.UpdateAccountShareListingInput{
				SeatLimit:       &seatLimit,
				EditSessionID:   "edit-session",
				ExpectedVersion: &expectedVersion,
				Reason:          "adjust room capacity",
			})
			if !errors.Is(err, service.ErrAccountShareListingInUse) {
				t.Fatalf("expected financial blocker rejection, got %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet expectations: %v", err)
			}
		})
	}
}

func TestAccountShareModeRepositoryUpdateListingRejectsPendingOperationEvenForAdminForce(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	expectedVersion := int64(1)
	seatLimit := 5
	expiresAt := time.Now().UTC().Add(10 * time.Minute)

	mock.ExpectBegin()
	mock.ExpectQuery(accountShareUpdateListingLockQueryPattern).
		WithArgs(int64(7)).
		WillReturnRows(accountShareUpdateListingLockRows(func(row *accountShareUpdateListingLockRowData) {
			row.OwnerUserID = 42
			row.Status = service.AccountShareListingStatusActive
			row.RowVersion = expectedVersion
			row.EditSessionID = "admin-edit"
			row.EditingByUserID = int64(9)
			row.EditingExpiresAt = expiresAt
			row.PendingOperationID = "11111111-1111-4111-8111-111111111111"
		}))
	mock.ExpectRollback()

	_, err = repo.UpdateListing(context.Background(), 9, true, 7, service.UpdateAccountShareListingInput{
		SeatLimit:       &seatLimit,
		EditSessionID:   "admin-edit",
		ForceActiveEdit: true,
		ExpectedVersion: &expectedVersion,
		Reason:          "emergency correction",
		Confirmed:       true,
	})
	if !errors.Is(err, service.ErrAccountShareRoomOperationConflict) {
		t.Fatalf("expected pending operation conflict, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryUpdateListingRejectsAdminForceDuringValidating(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	expectedVersion := int64(1)
	seatLimit := 5
	expiresAt := time.Now().UTC().Add(10 * time.Minute)

	mock.ExpectBegin()
	mock.ExpectQuery(accountShareUpdateListingLockQueryPattern).
		WithArgs(int64(7)).
		WillReturnRows(accountShareUpdateListingLockRows(func(row *accountShareUpdateListingLockRowData) {
			row.OwnerUserID = 42
			row.Status = service.AccountShareListingStatusValidating
			row.RowVersion = expectedVersion
			row.EditSessionID = "admin-edit"
			row.EditingByUserID = int64(9)
			row.EditingExpiresAt = expiresAt
		}))
	mock.ExpectRollback()

	_, err = repo.UpdateListing(context.Background(), 9, true, 7, service.UpdateAccountShareListingInput{
		SeatLimit:       &seatLimit,
		EditSessionID:   "admin-edit",
		ForceActiveEdit: true,
		ExpectedVersion: &expectedVersion,
		Reason:          "emergency correction",
		Confirmed:       true,
	})
	if !errors.Is(err, service.ErrAccountShareRoomOperationConflict) {
		t.Fatalf("expected lifecycle status conflict, got %v", err)
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
	mock.ExpectQuery("SELECT l\\.owner_user_id, l\\.status, l\\.edit_session_id, l\\.editing_by_user_id, l\\.editing_expires_at").
		WithArgs(int64(7), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"owner_user_id", "status", "edit_session_id", "editing_by_user_id", "editing_expires_at", "pending_operation_id"}).
			AddRow(int64(42), service.AccountShareListingStatusActive, nil, nil, nil, nil))
	expectAccountShareEditDatabaseBlockers(mock, int64(7), 1, 0, 0, 0)
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

func TestAccountShareModeRepositoryBeginListingEditRejectsPendingOperationEvenForAdminForce(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT l\\.owner_user_id, l\\.status, l\\.edit_session_id, l\\.editing_by_user_id, l\\.editing_expires_at").
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{
			"owner_user_id",
			"status",
			"edit_session_id",
			"editing_by_user_id",
			"editing_expires_at",
			"pending_operation_id",
		}).AddRow(
			int64(42),
			service.AccountShareListingStatusActive,
			nil,
			nil,
			nil,
			"11111111-1111-4111-8111-111111111111",
		))
	mock.ExpectRollback()

	_, err = repo.BeginListingEdit(context.Background(), 9, true, 7, service.BeginAccountShareListingEditInput{
		SessionID: "admin-edit",
		Force:     true,
		Expires:   time.Now().UTC().Add(10 * time.Minute),
	})
	if !errors.Is(err, service.ErrAccountShareRoomOperationConflict) {
		t.Fatalf("expected pending operation conflict, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryBeginListingEditRejectsAdminForceDuringDraining(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT l\\.owner_user_id, l\\.status, l\\.edit_session_id, l\\.editing_by_user_id, l\\.editing_expires_at").
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{
			"owner_user_id",
			"status",
			"edit_session_id",
			"editing_by_user_id",
			"editing_expires_at",
			"pending_operation_id",
		}).AddRow(
			int64(42),
			service.AccountShareListingStatusDraining,
			nil,
			nil,
			nil,
			nil,
		))
	mock.ExpectRollback()

	_, err = repo.BeginListingEdit(context.Background(), 9, true, 7, service.BeginAccountShareListingEditInput{
		SessionID: "admin-edit",
		Force:     true,
		Expires:   time.Now().UTC().Add(10 * time.Minute),
	})
	if !errors.Is(err, service.ErrAccountShareRoomOperationConflict) {
		t.Fatalf("expected draining lifecycle conflict, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryBeginListingEditAllowsOwnerForActiveEmptyRoom(t *testing.T) {
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
	mock.ExpectQuery("SELECT l\\.owner_user_id, l\\.status, l\\.edit_session_id, l\\.editing_by_user_id, l\\.editing_expires_at").
		WithArgs(int64(7), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"owner_user_id", "status", "edit_session_id", "editing_by_user_id", "editing_expires_at", "pending_operation_id"}).
			AddRow(int64(42), service.AccountShareListingStatusActive, nil, nil, nil, nil))
	expectAccountShareEditDatabaseBlockers(mock, int64(7), 0, 0, 0, 0)
	mock.ExpectExec("SET edit_session_id = \\$1::varchar").
		WithArgs("edit-session", int64(42), expires, int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery("SELECT\\s+l\\.id").
		WithArgs(int64(42), int64(7)).
		WillReturnRows(accountShareListingRows(7, 99, 42, "edit-session", expires, func(row *accountShareListingRowData) {
			row.Status = service.AccountShareListingStatusActive
		}))

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

func TestAccountShareOwnerEditableStatusAllowsOnlyActiveOrPaused(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   bool
	}{
		{name: "active", status: service.AccountShareListingStatusActive, want: true},
		{name: "paused", status: service.AccountShareListingStatusPaused, want: true},
		{name: "normalized active", status: " ACTIVE ", want: true},
		{name: "validating", status: service.AccountShareListingStatusValidating, want: false},
		{name: "draining", status: service.AccountShareListingStatusDraining, want: false},
		{name: "disabled", status: service.AccountShareListingStatusDisabled, want: false},
		{name: "suspended", status: service.AccountShareListingStatusSuspended, want: false},
		{name: "empty", status: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := accountShareOwnerEditableStatus(tt.status); got != tt.want {
				t.Fatalf("accountShareOwnerEditableStatus(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
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
	mock.ExpectQuery("SELECT a\\.id, l\\.owner_user_id, l\\.status, l\\.seat_limit").
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

	_, err = repo.JoinListing(context.Background(), service.AccountShareJoinRepositoryInput{
		ConsumerUserID:     42,
		APIKeyID:           12,
		ListingID:          7,
		IdleTimeoutMinutes: 1,
		AcceptQueue:        true,
		ExpectedVersion:    1,
		ExpectedRevisionID: 70,
		AcceptedTerms:      accountShareAcceptedJoinTerms(70, 1, "editing-room"),
		IntentIssuedAt:     time.Now().UTC(),
		IntentNonce:        "editing-intent",
	})
	if !errors.Is(err, service.ErrAccountShareListingEditing) {
		t.Fatalf("expected editing listing rejection, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryJoinListingRequiresCompleteServerIntent(t *testing.T) {
	repo := &accountShareModeRepository{}

	_, err := repo.JoinListing(context.Background(), service.AccountShareJoinRepositoryInput{
		ConsumerUserID:     42,
		APIKeyID:           12,
		ListingID:          7,
		IdleTimeoutMinutes: 10,
		AcceptQueue:        true,
	})

	if !errors.Is(err, service.ErrAccountShareJoinIntentInvalid) {
		t.Fatalf("expected incomplete intent rejection, got %v", err)
	}
}

func TestAccountShareModeRepositoryJoinListingRejectsStaleConfirmedRevision(t *testing.T) {
	tests := []struct {
		name               string
		expectedVersion    int64
		expectedRevisionID int64
	}{
		{name: "row version changed", expectedVersion: 2, expectedRevisionID: 70},
		{name: "revision changed", expectedVersion: 3, expectedRevisionID: 71},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			defer func() {
				_ = db.Close()
			}()
			repo := &accountShareModeRepository{db: db}
			listingID := int64(7)
			revisionID := int64(70)
			rowVersion := int64(3)

			mock.ExpectBegin()
			mock.ExpectQuery("SELECT a\\.id, l\\.owner_user_id, l\\.status, l\\.seat_limit").
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
				}).AddRow(int64(99), int64(50), service.AccountShareListingStatusActive, 4, 0.15, 0, 1, nil, nil))
			mock.ExpectQuery("SELECT l\\.current_revision_id, l\\.row_version, revision\\.revision_number").
				WithArgs(listingID).
				WillReturnRows(sqlmock.NewRows([]string{"current_revision_id", "row_version", "revision_number"}).AddRow(revisionID, rowVersion, rowVersion))
			mock.ExpectQuery("SELECT\\s+id, listing_id, revision_number, schema_version, snapshot_quality").
				WithArgs(revisionID, listingID).
				WillReturnRows(accountShareStoredRevisionRows(revisionID, listingID, rowVersion, "immutable-room", 50, "owner"))
			mock.ExpectRollback()

			_, err = repo.JoinListing(context.Background(), service.AccountShareJoinRepositoryInput{
				ConsumerUserID:     42,
				APIKeyID:           12,
				ListingID:          listingID,
				IdleTimeoutMinutes: 10,
				ExpectedVersion:    tt.expectedVersion,
				ExpectedRevisionID: tt.expectedRevisionID,
				AcceptQueue:        true,
				AcceptedTerms:      accountShareAcceptedJoinTerms(tt.expectedRevisionID, tt.expectedVersion, "immutable-room"),
				IntentIssuedAt:     time.Now().UTC(),
				IntentNonce:        "stale-confirmation",
			})
			if !errors.Is(err, service.ErrAccountShareJoinTermsChanged) {
				t.Fatalf("expected stale terms rejection, got %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet expectations: %v", err)
			}
		})
	}
}

func TestEnsureAccountShareListingRevisionMaterializesLegacyBaselineAsSystem(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	listingID := int64(7)
	rowVersion := int64(1)
	revisionID := int64(70)
	ownerUserID := int64(42)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT l\\.current_revision_id, l\\.row_version, revision\\.revision_number").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{"current_revision_id", "row_version", "revision_number"}).AddRow(nil, rowVersion, nil))
	mock.ExpectQuery("SELECT\\s+l\\.id, l\\.row_version").
		WithArgs(listingID).
		WillReturnRows(accountShareRevisionSnapshotRows(listingID, rowVersion, "legacy-room", ownerUserID, "owner"))
	mock.ExpectQuery("INSERT INTO account_share_listing_revisions").
		WithArgs(
			listingID,
			rowVersion,
			1,
			service.AccountShareSnapshotQualityExact,
			"legacy-room",
			service.PlatformOpenAI,
			"pro",
			ownerUserID,
			"owner",
			service.AccountShareListingStatusActive,
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
			"legacy_join_materialization",
			nil,
			nil,
			false,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(revisionID))
	mock.ExpectExec("UPDATE account_share_listings\\s+SET current_revision_id").
		WithArgs(revisionID, listingID, rowVersion).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO account_share_room_events").
		WithArgs(
			listingID,
			revisionID,
			"listing.revision_materialized",
			nil,
			"system",
			nil,
			`{"force_applied":false,"row_version":1,"source":"legacy_join_materialization"}`,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	gotRevisionID, gotVersion, err := ensureAccountShareListingRevisionInTx(context.Background(), tx, listingID)
	if err != nil {
		t.Fatalf("ensureAccountShareListingRevisionInTx: %v", err)
	}
	if gotRevisionID != revisionID || gotVersion != rowVersion {
		t.Fatalf("revision=(%d,%d), want (%d,%d)", gotRevisionID, gotVersion, revisionID, rowVersion)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestEnsureAccountShareListingRevisionRejectsPointerVersionMismatch(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	listingID := int64(7)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT l\\.current_revision_id, l\\.row_version, revision\\.revision_number").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{"current_revision_id", "row_version", "revision_number"}).AddRow(int64(70), int64(2), int64(1)))
	mock.ExpectRollback()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	_, _, err = ensureAccountShareListingRevisionInTx(context.Background(), tx, listingID)
	if err == nil || !strings.Contains(err.Error(), "revision pointer mismatch") {
		t.Fatalf("expected revision pointer mismatch, got %v", err)
	}
	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		t.Fatalf("rollback: %v", rollbackErr)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryEnsureListingRevisionTermsReturnsImmutableThresholdSnapshot(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}
	listingID := int64(7)
	revisionID := int64(70)
	rowVersion := int64(3)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT l\\.current_revision_id, l\\.row_version, revision\\.revision_number").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{"current_revision_id", "row_version", "revision_number"}).AddRow(revisionID, rowVersion, rowVersion))
	mock.ExpectQuery("SELECT\\s+id, listing_id, revision_number, schema_version, snapshot_quality").
		WithArgs(revisionID, listingID).
		WillReturnRows(accountShareStoredRevisionRows(revisionID, listingID, rowVersion, "immutable-room", 42, "owner", func(row *accountShareStoredRevisionRowData) {
			row.Platform = service.PlatformAnthropic
			row.Codex5hLimitPercent = 88
			row.Codex7dLimitPercent = 77
		}))
	mock.ExpectCommit()

	terms, err := repo.EnsureListingRevisionTerms(context.Background(), listingID)
	if err != nil {
		t.Fatalf("EnsureListingRevisionTerms: %v", err)
	}
	if terms == nil {
		t.Fatal("expected immutable terms")
	}
	if terms.ListingRevisionID != revisionID || terms.RowVersion != rowVersion {
		t.Fatalf("unexpected revision identity: %+v", terms)
	}
	if terms.Anthropic5hLimitPercent != 88 || terms.Anthropic7dLimitPercent != 77 {
		t.Fatalf("anthropic thresholds were not preserved: %+v", terms)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareMembershipTermsMatchRevisionIncludesAnthropicThresholds(t *testing.T) {
	revision := &accountShareListingRevisionSnapshot{
		ID:                     70,
		RowVersion:             3,
		SchemaVersion:          1,
		RoomName:               "immutable-room",
		Status:                 service.AccountShareListingStatusActive,
		SeatLimit:              4,
		RateMultiplier:         0.2,
		AllowedModels:          []string{"claude-sonnet-4-5"},
		PerUserConcurrency:     2,
		HourlyRate:             0.15,
		HourlyFeeWaiverMinimum: 0.05,
		MinBalanceRequired:     1,
		Codex5hLimitPercent:    88,
		Codex7dLimitPercent:    77,
	}
	terms := revision.termsSnapshot()
	if !accountShareMembershipTermsMatchRevision(terms, revision) {
		t.Fatal("expected exact immutable terms to match revision")
	}
	terms.Anthropic5hLimitPercent--
	if accountShareMembershipTermsMatchRevision(terms, revision) {
		t.Fatal("expected anthropic threshold drift to invalidate immutable terms")
	}
}

func TestLoadAccountShareMembershipTraceSnapshotPreservesImmutableTerms(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()

	membershipID := int64(700)
	revisionID := int64(70)
	listingVersion := int64(3)
	ownerUserID := int64(42)
	endingRequestedAt := time.Date(2026, 7, 27, 5, 0, 0, 0, time.UTC)
	termsJSON := []byte(`{
		"listing_revision_id":70,
		"row_version":3,
		"schema_version":1,
		"room_name":"archived-room",
		"status":"active",
		"seat_limit":4,
		"rate_multiplier":0.2,
		"allowed_models":["gpt-5.5"],
		"per_user_concurrency":5,
		"hourly_rate":0.15,
		"hourly_fee_waiver_minimum":0,
		"min_balance_required":1,
		"codex_cli_only":false,
		"codex_5h_limit_percent":99,
		"codex_7d_limit_percent":99
	}`)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT\\s+listing_revision_id, listing_version_snapshot, room_name_snapshot").
		WithArgs(membershipID).
		WillReturnRows(sqlmock.NewRows([]string{
			"listing_revision_id",
			"listing_version_snapshot",
			"room_name_snapshot",
			"owner_user_id_snapshot",
			"owner_username_snapshot",
			"platform_snapshot",
			"account_level_snapshot",
			"api_key_name_snapshot",
			"terms_snapshot",
			"snapshot_quality",
			"ending_requested_at",
			"ending_reason",
			"settlement_status",
		}).AddRow(
			revisionID,
			listingVersion,
			"archived-room",
			ownerUserID,
			"owner",
			service.PlatformOpenAI,
			"pro",
			"consumer-key",
			termsJSON,
			service.AccountShareSnapshotQualityExact,
			endingRequestedAt,
			"user_requested",
			"pending",
		))
	mock.ExpectCommit()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	membership := &service.AccountShareMembership{ID: membershipID}
	if err := loadAccountShareMembershipTraceSnapshotInTx(context.Background(), tx, membership); err != nil {
		t.Fatalf("loadAccountShareMembershipTraceSnapshotInTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if membership.ListingRevisionID == nil || *membership.ListingRevisionID != revisionID {
		t.Fatalf("listing revision id = %v, want %d", membership.ListingRevisionID, revisionID)
	}
	if membership.TermsSnapshot == nil || membership.TermsSnapshot.SchemaVersion != 1 || membership.TermsSnapshot.RowVersion != listingVersion {
		t.Fatalf("unexpected terms snapshot: %+v", membership.TermsSnapshot)
	}
	if membership.TermsSnapshot.Anthropic5hLimitPercent != 99 || membership.TermsSnapshot.Anthropic7dLimitPercent != 99 {
		t.Fatalf("legacy quota aliases were not hydrated: %+v", membership.TermsSnapshot)
	}
	if membership.EndingRequestedAt == nil || !membership.EndingRequestedAt.Equal(endingRequestedAt) {
		t.Fatalf("ending requested at = %v, want %v", membership.EndingRequestedAt, endingRequestedAt)
	}
	if membership.SnapshotQuality != service.AccountShareSnapshotQualityExact || membership.SettlementStatus != "pending" {
		t.Fatalf("unexpected trace state: quality=%q settlement=%q", membership.SnapshotQuality, membership.SettlementStatus)
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
	revisionID := int64(70)
	listingVersion := int64(1)
	idleTimeoutMinutes := 10
	now := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT a\\.id, l\\.owner_user_id, l\\.status, l\\.seat_limit").
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
	mock.ExpectQuery("SELECT l\\.current_revision_id, l\\.row_version, revision\\.revision_number").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{"current_revision_id", "row_version", "revision_number"}).AddRow(revisionID, listingVersion, listingVersion))
	mock.ExpectQuery("SELECT\\s+id, listing_id, revision_number, schema_version, snapshot_quality").
		WithArgs(revisionID, listingID).
		WillReturnRows(accountShareStoredRevisionRows(revisionID, listingID, listingVersion, "owner-room", ownerUserID, "owner", func(row *accountShareStoredRevisionRowData) {
			row.SeatLimit = 2
			row.HourlyRate = 1.5
			row.HourlyFeeWaiverMinimum = 0.5
			row.MinBalanceRequired = 100
		}))
	mock.ExpectQuery("SELECT\\s+name\\s+FROM api_keys").
		WithArgs(apiKeyID, consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"api_key_name"}).AddRow("owner-key"))
	mock.ExpectQuery("SELECT balance").
		WithArgs(consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(0.01))
	expectEndStaleQueuedMembershipsForConsumer(mock, consumerUserID, 0)
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(consumerUserID, listingID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued, service.AccountShareMembershipStatusEnding).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(
			consumerUserID,
			apiKeyID,
			listingID,
			sqlmock.AnyArg(),
			service.AccountShareMembershipStatusEnding,
			service.AccountShareMembershipStatusEnded,
		).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	expectAccountShareJoinQueueState(mock, consumerUserID, apiKeyID, listingID, 0, 0, false, 0, 0)
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(accountID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery("(?s)INSERT INTO account_share_memberships.*\\$5::varchar\\(20\\).*CASE WHEN \\$5::varchar\\(20\\) = 'queued'::varchar\\(20\\)").
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
			revisionID,
			listingVersion,
			"owner-room",
			ownerUserID,
			"owner",
			service.PlatformOpenAI,
			"pro",
			"owner-key",
			sqlmock.AnyArg(),
			service.AccountShareSnapshotQualityExact,
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
	expectAccountShareMembershipBinding(
		mock,
		membershipID,
		listingID,
		accountID,
		revisionID,
		consumerUserID,
		"owner",
		"join_activation",
		1,
	)
	mock.ExpectCommit()

	membership, err := repo.JoinListing(context.Background(), service.AccountShareJoinRepositoryInput{
		ConsumerUserID:     consumerUserID,
		APIKeyID:           apiKeyID,
		ListingID:          listingID,
		IdleTimeoutMinutes: idleTimeoutMinutes,
		AcceptQueue:        true,
		ExpectedVersion:    listingVersion,
		ExpectedRevisionID: revisionID,
		AcceptedTerms: accountShareAcceptedJoinTerms(revisionID, listingVersion, "owner-room", func(terms *service.AccountShareListingTermsSnapshot) {
			terms.SeatLimit = 2
			terms.HourlyRate = 1.5
			terms.HourlyFeeWaiverMinimum = 0.5
			terms.MinBalanceRequired = 100
		}),
		IntentIssuedAt: now.Add(-time.Minute),
		IntentNonce:    "owner-join-intent",
	})
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
	if membership.ListingRevisionID == nil || *membership.ListingRevisionID != revisionID {
		t.Fatalf("listing revision id = %v, want %d", membership.ListingRevisionID, revisionID)
	}
	if membership.TermsSnapshot == nil || membership.TermsSnapshot.HourlyRate != 1.5 {
		t.Fatalf("terms snapshot must preserve pre-owner-waiver terms: %+v", membership.TermsSnapshot)
	}
	if membership.SnapshotQuality != service.AccountShareSnapshotQualityExact {
		t.Fatalf("snapshot quality = %q, want exact", membership.SnapshotQuality)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryJoinListingQueuesBehindExistingActiveMembership(t *testing.T) {
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
	revisionID := int64(80)
	listingVersion := int64(3)
	idleTimeoutMinutes := 10
	now := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT a\\.id, l\\.owner_user_id, l\\.status, l\\.seat_limit").
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
	mock.ExpectQuery("SELECT l\\.current_revision_id, l\\.row_version, revision\\.revision_number").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{"current_revision_id", "row_version", "revision_number"}).AddRow(revisionID, listingVersion, listingVersion))
	mock.ExpectQuery("SELECT\\s+id, listing_id, revision_number, schema_version, snapshot_quality").
		WithArgs(revisionID, listingID).
		WillReturnRows(accountShareStoredRevisionRows(revisionID, listingID, listingVersion, "queued-room", ownerUserID, "room-owner", func(row *accountShareStoredRevisionRowData) {
			row.SeatLimit = 1
			row.HourlyRate = 0.6
			row.HourlyFeeWaiverMinimum = 0.1
		}))
	mock.ExpectQuery("SELECT\\s+name\\s+FROM api_keys").
		WithArgs(apiKeyID, consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"api_key_name"}).AddRow("consumer-key"))
	mock.ExpectQuery("SELECT balance").
		WithArgs(consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(1.005))
	expectEndStaleQueuedMembershipsForConsumer(mock, consumerUserID, 0)
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(consumerUserID, listingID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued, service.AccountShareMembershipStatusEnding).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(
			consumerUserID,
			apiKeyID,
			listingID,
			sqlmock.AnyArg(),
			service.AccountShareMembershipStatusEnding,
			service.AccountShareMembershipStatusEnded,
		).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	expectAccountShareJoinQueueState(mock, consumerUserID, apiKeyID, listingID, 0, 1, true, 0, 0)
	mock.ExpectQuery("INSERT INTO account_share_memberships").
		WithArgs(
			listingID,
			nil,
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
			revisionID,
			listingVersion,
			"queued-room",
			ownerUserID,
			"room-owner",
			service.PlatformOpenAI,
			"pro",
			"consumer-key",
			sqlmock.AnyArg(),
			service.AccountShareSnapshotQualityExact,
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
			nil,
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

	membership, err := repo.JoinListing(context.Background(), service.AccountShareJoinRepositoryInput{
		ConsumerUserID:     consumerUserID,
		APIKeyID:           apiKeyID,
		ListingID:          listingID,
		IdleTimeoutMinutes: idleTimeoutMinutes,
		AcceptQueue:        true,
		ExpectedVersion:    listingVersion,
		ExpectedRevisionID: revisionID,
		AcceptedTerms: accountShareAcceptedJoinTerms(revisionID, listingVersion, "queued-room", func(terms *service.AccountShareListingTermsSnapshot) {
			terms.SeatLimit = 1
			terms.HourlyRate = 0.6
			terms.HourlyFeeWaiverMinimum = 0.1
		}),
		IntentIssuedAt: now.Add(-time.Minute),
		IntentNonce:    "queued-join-intent",
	})
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
	if membership.AccountID != 0 {
		t.Fatalf("queued membership account id = %d, want no pre-bound account", membership.AccountID)
	}
	if membership.ListingVersionSnapshot == nil || *membership.ListingVersionSnapshot != listingVersion {
		t.Fatalf("listing version snapshot = %v, want %d", membership.ListingVersionSnapshot, listingVersion)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryJoinListingRequiresExplicitQueueAcceptance(t *testing.T) {
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
	revisionID := int64(80)
	listingVersion := int64(3)
	intentIssuedAt := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT a\\.id, l\\.owner_user_id, l\\.status, l\\.seat_limit").
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
	mock.ExpectQuery("SELECT l\\.current_revision_id, l\\.row_version, revision\\.revision_number").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{"current_revision_id", "row_version", "revision_number"}).AddRow(revisionID, listingVersion, listingVersion))
	mock.ExpectQuery("SELECT\\s+id, listing_id, revision_number, schema_version, snapshot_quality").
		WithArgs(revisionID, listingID).
		WillReturnRows(accountShareStoredRevisionRows(revisionID, listingID, listingVersion, "queued-room", ownerUserID, "room-owner", func(row *accountShareStoredRevisionRowData) {
			row.SeatLimit = 1
			row.HourlyRate = 0.6
			row.HourlyFeeWaiverMinimum = 0.1
		}))
	mock.ExpectQuery("SELECT\\s+name\\s+FROM api_keys").
		WithArgs(apiKeyID, consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"api_key_name"}).AddRow("consumer-key"))
	mock.ExpectQuery("SELECT balance").
		WithArgs(consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(10.0))
	expectEndStaleQueuedMembershipsForConsumer(mock, consumerUserID, 0)
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(consumerUserID, listingID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued, service.AccountShareMembershipStatusEnding).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(
			consumerUserID,
			apiKeyID,
			listingID,
			intentIssuedAt,
			service.AccountShareMembershipStatusEnding,
			service.AccountShareMembershipStatusEnded,
		).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	expectAccountShareJoinQueueState(mock, consumerUserID, apiKeyID, listingID, 1, 1, false, 1, 0)
	mock.ExpectRollback()

	_, err = repo.JoinListing(context.Background(), service.AccountShareJoinRepositoryInput{
		ConsumerUserID:     consumerUserID,
		APIKeyID:           apiKeyID,
		ListingID:          listingID,
		IdleTimeoutMinutes: 10,
		ExpectedVersion:    listingVersion,
		ExpectedRevisionID: revisionID,
		AcceptQueue:        false,
		AcceptedTerms: accountShareAcceptedJoinTerms(revisionID, listingVersion, "queued-room", func(terms *service.AccountShareListingTermsSnapshot) {
			terms.SeatLimit = 1
			terms.HourlyRate = 0.6
			terms.HourlyFeeWaiverMinimum = 0.1
		}),
		IntentIssuedAt: intentIssuedAt,
		IntentNonce:    "queue-declined",
	})
	if !errors.Is(err, service.ErrAccountShareQueueConfirmationRequired) {
		t.Fatalf("expected queue confirmation rejection, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryJoinListingRejectsConsumedIntent(t *testing.T) {
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
	revisionID := int64(80)
	listingVersion := int64(3)
	intentIssuedAt := time.Now().UTC()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT a\\.id, l\\.owner_user_id, l\\.status, l\\.seat_limit").
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
	mock.ExpectQuery("SELECT l\\.current_revision_id, l\\.row_version, revision\\.revision_number").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{"current_revision_id", "row_version", "revision_number"}).AddRow(revisionID, listingVersion, listingVersion))
	mock.ExpectQuery("SELECT\\s+id, listing_id, revision_number, schema_version, snapshot_quality").
		WithArgs(revisionID, listingID).
		WillReturnRows(accountShareStoredRevisionRows(revisionID, listingID, listingVersion, "queued-room", ownerUserID, "room-owner", func(row *accountShareStoredRevisionRowData) {
			row.SeatLimit = 1
			row.HourlyRate = 0.6
			row.HourlyFeeWaiverMinimum = 0.1
		}))
	mock.ExpectQuery("SELECT\\s+name\\s+FROM api_keys").
		WithArgs(apiKeyID, consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"api_key_name"}).AddRow("consumer-key"))
	mock.ExpectQuery("SELECT balance").
		WithArgs(consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(10.0))
	expectEndStaleQueuedMembershipsForConsumer(mock, consumerUserID, 0)
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(consumerUserID, listingID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued, service.AccountShareMembershipStatusEnding).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(
			consumerUserID,
			apiKeyID,
			listingID,
			intentIssuedAt,
			service.AccountShareMembershipStatusEnding,
			service.AccountShareMembershipStatusEnded,
		).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectRollback()

	_, err = repo.JoinListing(context.Background(), service.AccountShareJoinRepositoryInput{
		ConsumerUserID:     consumerUserID,
		APIKeyID:           apiKeyID,
		ListingID:          listingID,
		IdleTimeoutMinutes: 10,
		ExpectedVersion:    listingVersion,
		ExpectedRevisionID: revisionID,
		AcceptQueue:        true,
		AcceptedTerms: accountShareAcceptedJoinTerms(revisionID, listingVersion, "queued-room", func(terms *service.AccountShareListingTermsSnapshot) {
			terms.SeatLimit = 1
			terms.HourlyRate = 0.6
			terms.HourlyFeeWaiverMinimum = 0.1
		}),
		IntentIssuedAt: intentIssuedAt,
		IntentNonce:    "already-consumed",
	})
	if !errors.Is(err, service.ErrAccountShareJoinIntentConsumed) {
		t.Fatalf("expected consumed intent rejection, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryJoinListingRetriesReturnExistingReservation(t *testing.T) {
	tests := []struct {
		name             string
		status           string
		accountID        any
		existingAPIKeyID int64
		wantErr          error
	}{
		{name: "active", status: service.AccountShareMembershipStatusActive, accountID: int64(100)},
		{name: "queued", status: service.AccountShareMembershipStatusQueued, accountID: nil},
		{name: "ending", status: service.AccountShareMembershipStatusEnding, accountID: int64(100), wantErr: service.ErrAccountShareMembershipEnding},
		{name: "ending with another key", status: service.AccountShareMembershipStatusEnding, accountID: int64(100), existingAPIKeyID: 77, wantErr: service.ErrAccountShareMembershipEnding},
		{name: "active with another key", status: service.AccountShareMembershipStatusActive, accountID: int64(100), existingAPIKeyID: 77, wantErr: service.ErrAccountShareAlreadyUsing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			defer func() {
				_ = db.Close()
			}()
			repo := &accountShareModeRepository{db: db}
			listingID := int64(8)
			representativeAccountID := int64(100)
			ownerUserID := int64(50)
			consumerUserID := int64(42)
			apiKeyID := int64(12)
			existingAPIKeyID := tt.existingAPIKeyID
			if existingAPIKeyID <= 0 {
				existingAPIKeyID = apiKeyID
			}
			membershipID := int64(701)
			revisionID := int64(80)
			listingVersion := int64(3)
			now := time.Now().UTC()

			mock.ExpectBegin()
			mock.ExpectQuery("SELECT a\\.id, l\\.owner_user_id, l\\.status, l\\.seat_limit").
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
				}).AddRow(representativeAccountID, ownerUserID, service.AccountShareListingStatusActive, 4, 0.15, 0, 1, nil, nil))
			mock.ExpectQuery("SELECT l\\.current_revision_id, l\\.row_version, revision\\.revision_number").
				WithArgs(listingID).
				WillReturnRows(sqlmock.NewRows([]string{"current_revision_id", "row_version", "revision_number"}).AddRow(revisionID, listingVersion, listingVersion))
			mock.ExpectQuery("SELECT\\s+id, listing_id, revision_number, schema_version, snapshot_quality").
				WithArgs(revisionID, listingID).
				WillReturnRows(accountShareStoredRevisionRows(revisionID, listingID, listingVersion, "retry-room", ownerUserID, "room-owner"))
			mock.ExpectQuery("SELECT\\s+name\\s+FROM api_keys").
				WithArgs(apiKeyID, consumerUserID).
				WillReturnRows(sqlmock.NewRows([]string{"api_key_name"}).AddRow("consumer-key"))
			mock.ExpectQuery("SELECT balance").
				WithArgs(consumerUserID).
				WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(10.0))
			expectEndStaleQueuedMembershipsForConsumer(mock, consumerUserID, 0)
			mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
				WithArgs(consumerUserID, listingID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued, service.AccountShareMembershipStatusEnding).
				WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
					accountShareEndMembershipRow(
						membershipID,
						listingID,
						tt.accountID,
						ownerUserID,
						consumerUserID,
						existingAPIKeyID,
						tt.status,
						now,
						now,
					)...,
				))
			if tt.wantErr == nil {
				expectAccountShareMembershipRuntimeSnapshot(
					mock,
					membershipID,
					revisionID,
					listingVersion,
					accountShareRuntimeTermsJSON(revisionID, listingVersion, 0.2),
				)
			}
			mock.ExpectRollback()

			membership, err := repo.JoinListing(context.Background(), service.AccountShareJoinRepositoryInput{
				ConsumerUserID:     consumerUserID,
				APIKeyID:           apiKeyID,
				ListingID:          listingID,
				IdleTimeoutMinutes: 10,
				ExpectedVersion:    listingVersion,
				ExpectedRevisionID: revisionID,
				AcceptQueue:        true,
				AcceptedTerms:      accountShareAcceptedJoinTerms(revisionID, listingVersion, "retry-room"),
				IntentIssuedAt:     now.Add(-time.Minute),
				IntentNonce:        "retry-same-intent",
			})
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("JoinListing error = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("JoinListing retry: %v", err)
			}
			if tt.wantErr == nil && (membership == nil || membership.ID != membershipID || membership.Status != tt.status) {
				t.Fatalf("unexpected idempotent membership: %+v", membership)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet expectations: %v", err)
			}
		})
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
	revisionID := int64(90)
	listingVersion := int64(2)
	idleTimeoutMinutes := 10
	now := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT a\\.id, l\\.owner_user_id, l\\.status, l\\.seat_limit").
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
	mock.ExpectQuery("SELECT l\\.current_revision_id, l\\.row_version, revision\\.revision_number").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{"current_revision_id", "row_version", "revision_number"}).AddRow(revisionID, listingVersion, listingVersion))
	mock.ExpectQuery("SELECT\\s+id, listing_id, revision_number, schema_version, snapshot_quality").
		WithArgs(revisionID, listingID).
		WillReturnRows(accountShareStoredRevisionRows(revisionID, listingID, listingVersion, "active-room", ownerUserID, "room-owner", func(row *accountShareStoredRevisionRowData) {
			row.SeatLimit = 2
			row.HourlyRate = 0
			row.HourlyFeeWaiverMinimum = 0
		}))
	mock.ExpectQuery("SELECT\\s+name\\s+FROM api_keys").
		WithArgs(apiKeyID, consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"api_key_name"}).AddRow("consumer-key"))
	mock.ExpectQuery("SELECT balance").
		WithArgs(consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(10.0))
	expectEndStaleQueuedMembershipsForConsumer(mock, consumerUserID, 1)
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(consumerUserID, listingID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued, service.AccountShareMembershipStatusEnding).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(
			consumerUserID,
			apiKeyID,
			listingID,
			sqlmock.AnyArg(),
			service.AccountShareMembershipStatusEnding,
			service.AccountShareMembershipStatusEnded,
		).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	expectAccountShareJoinQueueState(
		mock,
		consumerUserID,
		apiKeyID,
		listingID,
		0,
		0,
		false,
		service.AccountShareModeQueueMaxItems,
		0,
	)
	mock.ExpectQuery("SELECT COUNT\\(\\*\\)::int").
		WithArgs(
			listingID,
			service.AccountShareMembershipStatusActive,
			service.AccountShareMembershipStatusEnding,
		).
		WillReturnRows(sqlmock.NewRows([]string{"active_seats"}).AddRow(0))
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs(accountID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
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
			revisionID,
			listingVersion,
			"active-room",
			ownerUserID,
			"room-owner",
			service.PlatformOpenAI,
			"pro",
			"consumer-key",
			sqlmock.AnyArg(),
			service.AccountShareSnapshotQualityExact,
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
	expectAccountShareMembershipBinding(
		mock,
		membershipID,
		listingID,
		accountID,
		revisionID,
		consumerUserID,
		"consumer",
		"join_activation",
		1,
	)
	mock.ExpectCommit()

	membership, err := repo.JoinListing(context.Background(), service.AccountShareJoinRepositoryInput{
		ConsumerUserID:     consumerUserID,
		APIKeyID:           apiKeyID,
		ListingID:          listingID,
		IdleTimeoutMinutes: idleTimeoutMinutes,
		AcceptQueue:        true,
		ExpectedVersion:    listingVersion,
		ExpectedRevisionID: revisionID,
		AcceptedTerms: accountShareAcceptedJoinTerms(revisionID, listingVersion, "active-room", func(terms *service.AccountShareListingTermsSnapshot) {
			terms.SeatLimit = 2
			terms.HourlyRate = 0
			terms.HourlyFeeWaiverMinimum = 0
		}),
		IntentIssuedAt: now.Add(-time.Minute),
		IntentNonce:    "active-join-intent",
	})
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

func TestAccountShareJoinQueueCapacityErrorMetadata(t *testing.T) {
	tests := []struct {
		name               string
		apiKeyQueueCount   int
		consumerQueueCount int
		roomQueueCount     int
		seatLimit          int
		wantErr            error
		wantScope          string
		wantLimit          string
		wantUsed           string
	}{
		{
			name:             "api key cap",
			apiKeyQueueCount: service.AccountShareModeQueueMaxItems,
			seatLimit:        1,
			wantErr:          service.ErrAccountShareQueueFull,
			wantScope:        "api_key",
			wantLimit:        strconv.Itoa(service.AccountShareModeQueueMaxItems),
			wantUsed:         strconv.Itoa(service.AccountShareModeQueueMaxItems),
		},
		{
			name:               "consumer cap",
			consumerQueueCount: service.AccountShareModeQueueMaxItems,
			seatLimit:          1,
			wantErr:            service.ErrAccountShareQueueFull,
			wantScope:          "consumer",
			wantLimit:          strconv.Itoa(service.AccountShareModeQueueMaxItems),
			wantUsed:           strconv.Itoa(service.AccountShareModeQueueMaxItems),
		},
		{
			name:           "room cap",
			roomQueueCount: service.AccountShareRoomQueueLimit(1),
			seatLimit:      1,
			wantErr:        service.ErrAccountShareRoomQueueLimitExceeded,
			wantScope:      "room",
			wantLimit:      strconv.Itoa(service.AccountShareRoomQueueLimit(1)),
			wantUsed:       strconv.Itoa(service.AccountShareRoomQueueLimit(1)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := accountShareJoinQueueCapacityError(
				tt.apiKeyQueueCount,
				tt.consumerQueueCount,
				tt.roomQueueCount,
				tt.seatLimit,
			)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			appErr := infraerrors.FromError(err)
			if appErr.Metadata["scope"] != tt.wantScope ||
				appErr.Metadata["limit"] != tt.wantLimit ||
				appErr.Metadata["used"] != tt.wantUsed {
				t.Fatalf("metadata = %#v, want scope=%q limit=%q used=%q", appErr.Metadata, tt.wantScope, tt.wantLimit, tt.wantUsed)
			}
		})
	}

	if err := accountShareJoinQueueCapacityError(4, 4, 19, 1); err != nil {
		t.Fatalf("below all queue caps returned error: %v", err)
	}
}

func TestTranslateAccountShareMembershipConflictCoversLifecycleIndexes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		constraint string
		want       error
	}{
		{"uq_account_share_memberships_live_consumer", service.ErrAccountShareAlreadyUsing},
		{"uq_as_memberships_live_consumer_rebuild_guard", service.ErrAccountShareAlreadyUsing},
		{"uq_account_share_memberships_live_api_key", service.ErrAccountShareAPIKeyAlreadyBound},
		{"uq_as_memberships_live_api_key_rebuild_guard", service.ErrAccountShareAPIKeyAlreadyBound},
		{"uq_account_share_memberships_live_listing_consumer", service.ErrAccountShareMembershipEnding},
		{"uq_as_memberships_live_listing_consumer_rebuild_guard", service.ErrAccountShareMembershipEnding},
	}
	for _, tt := range tests {
		t.Run(tt.constraint, func(t *testing.T) {
			err := translateAccountShareMembershipConflict(&pq.Error{
				Code:       "23505",
				Constraint: tt.constraint,
			})
			if !errors.Is(err, tt.want) {
				t.Fatalf("translated error = %v, want %v", err, tt.want)
			}
		})
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
	mock.ExpectQuery("SELECT NOT EXISTS").
		WithArgs(listingID, accountID, now).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery("SELECT EXISTS.*\\$3::timestamptz").
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
	mock.ExpectQuery("SELECT NOT EXISTS").
		WithArgs(listingID, accountID, now).
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
	mock.ExpectQuery("SELECT NOT EXISTS").
		WithArgs(listingID, accountID, now).
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
	mock.ExpectQuery("SELECT NOT EXISTS").
		WithArgs(listingID, accountID, now).
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
	mock.ExpectQuery("SELECT NOT EXISTS").
		WithArgs(listingID, accountID, now).
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

	mock.ExpectQuery("SELECT sc\\.id, sc\\.period_ended_at").
		WithArgs(accountShareSeatSettlementTypeCharge, accountShareSeatSettlementTypeWaiverRefund, readyBefore, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "period_ended_at"}).AddRow(settlementID, windowEnd))
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

	batch, err := repo.ProcessSeatWaiverBacklogCompensations(context.Background(), now, 1, time.Time{}, 0)
	if err != nil {
		t.Fatalf("ProcessSeatWaiverBacklogCompensations failed: %v", err)
	}
	if batch == nil || batch.Billing == nil {
		t.Fatal("expected compensation batch")
	}
	if got := strings.Trim(strings.Join(int64sToStrings(batch.Billing.CreditUserIDs), ","), ","); got != "4866" {
		t.Fatalf("credit users = %q, want 4866", got)
	}
	if got := strings.Trim(strings.Join(int64sToStrings(batch.Billing.DebitUserIDs), ","), ","); got != "7001" {
		t.Fatalf("debit users = %q, want 7001", got)
	}
	if batch.Matched != 1 {
		t.Fatalf("matched = %d, want 1", batch.Matched)
	}
	if !batch.CursorPeriodEndedAt.Equal(windowEnd) || batch.CursorID != settlementID {
		t.Fatalf("cursor = (%v, %d), want (%v, %d)", batch.CursorPeriodEndedAt, batch.CursorID, windowEnd, settlementID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryProcessSeatWaiverCompensationsUsesWindowEndReadiness(t *testing.T) {
	matcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		normalized := strings.ToLower(strings.Join(strings.Fields(actualSQL), " "))
		switch expectedSQL {
		case "seat waiver backlog candidate query":
			if !strings.Contains(normalized, "sc.period_ended_at <= $3") {
				return errors.New("backlog candidate query must wait until the charged window has ended")
			}
			if !strings.Contains(normalized, "sc.waiver_evaluated_at is null") {
				return errors.New("backlog candidate query must target unevaluated rows only")
			}
			if strings.Contains(normalized, "(sc.period_ended_at, sc.id) >") {
				return errors.New("backlog candidate query must omit the cursor clause when cursor is zero")
			}
			if strings.Contains(normalized, "sc.created_at <=") {
				return errors.New("candidate query must not use settlement creation time as readiness")
			}
		case "seat waiver late usage candidate query":
			if !strings.Contains(normalized, "sc.period_ended_at <= $4") {
				return errors.New("late usage candidate query must wait until the charged window has ended")
			}
			if !strings.Contains(normalized, "sc.period_ended_at >= $5") || !strings.Contains(normalized, "sc.waiver_evaluated_at >= $5") {
				return errors.New("late usage candidate query must carry the window lower bounds")
			}
			if !strings.Contains(normalized, "e.created_at >= $6") {
				return errors.New("late usage candidate query must bound late entries by created_at")
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

	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	readyBefore := now.Add(-service.AccountShareModeSeatWaiverCompensationDelay)
	usageSince := now.Add(-service.AccountShareModeSeatWaiverLateUsageLookback)
	windowSince := usageSince.Add(-service.AccountShareModeSeatWaiverLateUsageSlack)

	mock.ExpectQuery("seat waiver backlog candidate query").
		WithArgs(accountShareSeatSettlementTypeCharge, accountShareSeatSettlementTypeWaiverRefund, readyBefore, service.AccountShareModeSeatWaiverCompensationBatchSize).
		WillReturnRows(sqlmock.NewRows([]string{"id", "period_ended_at"}))
	mock.ExpectQuery("seat waiver late usage candidate query").
		WithArgs(
			accountShareSeatSettlementTypeCharge,
			accountShareSeatSettlementTypeWaiverRefund,
			accountShareSeatSettlementTypeUsage,
			readyBefore,
			windowSince,
			usageSince,
			service.AccountShareModeSeatWaiverCompensationBatchSize,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "period_ended_at"}))

	backlog, err := repo.ProcessSeatWaiverBacklogCompensations(context.Background(), now, 0, time.Time{}, 0)
	if err != nil {
		t.Fatalf("ProcessSeatWaiverBacklogCompensations failed: %v", err)
	}
	if backlog == nil || backlog.Matched != 0 {
		t.Fatalf("backlog matched = %#v, want 0", backlog)
	}
	late, err := repo.ProcessSeatWaiverLateUsageCompensations(context.Background(), now, 0, usageSince, windowSince, time.Time{}, 0)
	if err != nil {
		t.Fatalf("ProcessSeatWaiverLateUsageCompensations failed: %v", err)
	}
	if late == nil || late.Matched != 0 {
		t.Fatalf("late usage matched = %#v, want 0", late)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositorySeatWaiverCursorClauseOnlyWhenSet(t *testing.T) {
	matcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		normalized := strings.ToLower(strings.Join(strings.Fields(actualSQL), " "))
		switch expectedSQL {
		case "backlog with cursor":
			if !strings.Contains(normalized, "(sc.period_ended_at, sc.id) > ($4, $5)") {
				return errors.New("backlog query with cursor must carry the row-compare keyset clause")
			}
			if !strings.Contains(normalized, "limit $6") {
				return errors.New("backlog query with cursor must renumber the limit placeholder")
			}
		case "late usage with cursor":
			if !strings.Contains(normalized, "(sc.period_ended_at, sc.id) > ($7, $8)") {
				return errors.New("late usage query with cursor must carry the row-compare keyset clause")
			}
			if !strings.Contains(normalized, "limit $9") {
				return errors.New("late usage query with cursor must renumber the limit placeholder")
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

	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	readyBefore := now.Add(-service.AccountShareModeSeatWaiverCompensationDelay)
	usageSince := now.Add(-service.AccountShareModeSeatWaiverLateUsageLookback)
	windowSince := usageSince.Add(-service.AccountShareModeSeatWaiverLateUsageSlack)
	cursorEndedAt := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	cursorID := int64(9911)

	mock.ExpectQuery("backlog with cursor").
		WithArgs(accountShareSeatSettlementTypeCharge, accountShareSeatSettlementTypeWaiverRefund, readyBefore, cursorEndedAt, cursorID, 25).
		WillReturnRows(sqlmock.NewRows([]string{"id", "period_ended_at"}))
	mock.ExpectQuery("late usage with cursor").
		WithArgs(
			accountShareSeatSettlementTypeCharge,
			accountShareSeatSettlementTypeWaiverRefund,
			accountShareSeatSettlementTypeUsage,
			readyBefore,
			windowSince,
			usageSince,
			cursorEndedAt,
			cursorID,
			25,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "period_ended_at"}))

	if _, err := repo.ProcessSeatWaiverBacklogCompensations(context.Background(), now, 25, cursorEndedAt, cursorID); err != nil {
		t.Fatalf("ProcessSeatWaiverBacklogCompensations failed: %v", err)
	}
	if _, err := repo.ProcessSeatWaiverLateUsageCompensations(context.Background(), now, 25, usageSince, windowSince, cursorEndedAt, cursorID); err != nil {
		t.Fatalf("ProcessSeatWaiverLateUsageCompensations failed: %v", err)
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
	mock.ExpectQuery("SELECT NOT EXISTS").
		WithArgs(listingID, accountID, now).
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
				!strings.Contains(normalized, "l.status in ('disabled', 'suspended')") ||
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
			if !strings.Contains(normalized, "m.status = $1") ||
				!strings.Contains(normalized, "m.queue_expires_at <= $2") ||
				!strings.Contains(normalized, "l.status in ($3, $4, 'draining')") {
				return errors.New("stale queued cleanup must target expired queues and suspended/draining listings")
			}
			if strings.Contains(normalized, "join accounts") ||
				strings.Contains(normalized, "a.status") ||
				!strings.Contains(normalized, "then null else m.account_id end") ||
				!strings.Contains(normalized, "when c.queue_expired then $7") {
				return errors.New("queued cleanup must not depend on a pre-bound account and must preserve the expiry reason")
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
			now,
			service.AccountShareListingStatusDisabled,
			service.AccountShareListingStatusSuspended,
			service.AccountShareModeSeatBillingBatchSize,
			service.AccountShareMembershipStatusEnded,
			service.AccountShareMembershipEndReasonQueueExpired,
			service.AccountShareMembershipEndReasonUnavailable,
			true,
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

func TestAccountShareModeRepositoryBeginMembershipEndQueuedEndsAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	membershipID := int64(25101)
	listingID := int64(521)
	ownerUserID := int64(1001)
	consumerUserID := int64(18467)
	apiKeyID := int64(27485)
	listingVersion := int64(8)
	operationID := "f434216c-73b0-4fe0-a8cb-0e53d3328317"
	joinedAt := time.Date(2026, 7, 27, 4, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 7, 27, 4, 5, 0, 123000000, time.UTC)

	mock.ExpectBegin()
	expectAccountShareEndListingLock(mock, membershipID, consumerUserID, listingID, listingVersion)
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(membershipID, consumerUserID).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			accountShareEndMembershipRow(
				membershipID, listingID, nil, ownerUserID, consumerUserID, apiKeyID,
				service.AccountShareMembershipStatusQueued, joinedAt, updatedAt,
			)...,
		))
	expectAccountShareEndState(mock, membershipID, nil, nil, nil, nil)
	mock.ExpectExec("(?s)UPDATE account_share_membership_account_bindings\\s+SET unbound_at").
		WithArgs(sqlmock.AnyArg(), consumerUserID, "consumer", "membership_ended", membershipID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("(?s)INSERT INTO account_share_room_operations.*\\$5::bigint.*\\$6::varchar\\(20\\).*\\$8::timestamptz").
		WithArgs(
			operationID,
			listingID,
			membershipID,
			consumerUserID,
			listingVersion,
			"succeeded",
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("(?s)UPDATE account_share_memberships m\\s+SET status.*ended_reason = \\$3::text.*ending_reason = \\$8::text").
		WithArgs(
			service.AccountShareMembershipStatusEnded,
			sqlmock.AnyArg(),
			service.AccountShareMembershipEndReasonManual,
			operationID,
			membershipID,
			service.AccountShareMembershipStatusQueued,
			true,
			service.AccountShareMembershipEndReasonManual,
		).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			accountShareEndMembershipEndedRow(
				membershipID, listingID, nil, ownerUserID, consumerUserID, apiKeyID,
				joinedAt, updatedAt.Add(time.Second),
			)...,
		))
	mock.ExpectCommit()

	membership, billing, err := repo.BeginMembershipEnd(context.Background(), service.BeginAccountShareMembershipEndInput{
		ConsumerUserID:           consumerUserID,
		MembershipID:             membershipID,
		ExpectedMembershipStatus: service.AccountShareMembershipStatusQueued,
		OperationID:              operationID,
	})
	if err != nil {
		t.Fatalf("BeginMembershipEnd failed: %v", err)
	}
	if membership == nil || membership.Status != service.AccountShareMembershipStatusEnded {
		t.Fatalf("unexpected membership: %#v", membership)
	}
	if membership.AccountID != 0 || membership.SettlementStatus != "not_required" || membership.EndingOperationID != operationID {
		t.Fatalf("queued end contract was not preserved: %#v", membership)
	}
	if billing == nil || billing.Processed != 1 || len(billing.EndedConsumerUserIDs) != 1 {
		t.Fatalf("unexpected queued end billing result: %#v", billing)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryBeginMembershipEndActiveCreatesDurableFence(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	membershipID := int64(25102)
	listingID := int64(522)
	accountID := int64(449297)
	ownerUserID := int64(1001)
	consumerUserID := int64(18467)
	apiKeyID := int64(27485)
	listingVersion := int64(9)
	operationID := "8400ef23-9509-45be-a84f-86a67ea23436"
	joinedAt := time.Date(2026, 7, 27, 4, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 7, 27, 4, 10, 0, 321000000, time.UTC)

	mock.ExpectBegin()
	expectAccountShareEndListingLock(mock, membershipID, consumerUserID, listingID, listingVersion)
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(membershipID, consumerUserID).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			accountShareEndMembershipRow(
				membershipID, listingID, accountID, ownerUserID, consumerUserID, apiKeyID,
				service.AccountShareMembershipStatusActive, joinedAt, updatedAt,
			)...,
		))
	expectAccountShareEndState(mock, membershipID, nil, nil, nil, nil)
	mock.ExpectExec("INSERT INTO account_share_room_operations").
		WithArgs(
			operationID,
			listingID,
			membershipID,
			consumerUserID,
			listingVersion,
			"pending",
			sqlmock.AnyArg(),
			nil,
			sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("UPDATE account_share_memberships m\\s+SET status").
		WithArgs(
			service.AccountShareMembershipStatusEnding,
			sqlmock.AnyArg(),
			service.AccountShareMembershipEndReasonManual,
			operationID,
			membershipID,
			service.AccountShareMembershipStatusActive,
		).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			accountShareEndMembershipRow(
				membershipID, listingID, accountID, ownerUserID, consumerUserID, apiKeyID,
				service.AccountShareMembershipStatusEnding, joinedAt, updatedAt.Add(time.Second),
			)...,
		))
	mock.ExpectCommit()

	membership, billing, err := repo.BeginMembershipEnd(context.Background(), service.BeginAccountShareMembershipEndInput{
		ConsumerUserID:           consumerUserID,
		MembershipID:             membershipID,
		ExpectedMembershipStatus: service.AccountShareMembershipStatusActive,
		OperationID:              operationID,
	})
	if err != nil {
		t.Fatalf("BeginMembershipEnd failed: %v", err)
	}
	if billing != nil {
		t.Fatalf("active transition must not settle synchronously: %#v", billing)
	}
	if membership == nil ||
		membership.Status != service.AccountShareMembershipStatusEnding ||
		membership.SettlementStatus != "pending" ||
		membership.EndingOperationID != operationID ||
		membership.EndingRequestedAt == nil {
		t.Fatalf("durable ending fence was not returned: %#v", membership)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryBeginMembershipEndOperationFailureRollsBack(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	membershipID := int64(25104)
	listingID := int64(524)
	consumerUserID := int64(18467)
	updatedAt := time.Date(2026, 7, 27, 4, 20, 0, 0, time.UTC)
	operationID := "e145965b-832c-46aa-8e99-5f5f2d7371e9"
	writeErr := errors.New("operation insert failed")

	mock.ExpectBegin()
	expectAccountShareEndListingLock(mock, membershipID, consumerUserID, listingID, 11)
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(membershipID, consumerUserID).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			accountShareEndMembershipRow(
				membershipID, listingID, int64(90), int64(1001), consumerUserID, int64(91),
				service.AccountShareMembershipStatusActive, updatedAt.Add(-time.Hour), updatedAt,
			)...,
		))
	expectAccountShareEndState(mock, membershipID, nil, nil, nil, nil)
	mock.ExpectExec("INSERT INTO account_share_room_operations").
		WithArgs(
			operationID,
			listingID,
			membershipID,
			consumerUserID,
			int64(11),
			"pending",
			sqlmock.AnyArg(),
			nil,
			sqlmock.AnyArg(),
		).
		WillReturnError(writeErr)
	mock.ExpectRollback()

	_, _, err = repo.BeginMembershipEnd(context.Background(), service.BeginAccountShareMembershipEndInput{
		ConsumerUserID:           consumerUserID,
		MembershipID:             membershipID,
		ExpectedMembershipStatus: service.AccountShareMembershipStatusActive,
		OperationID:              operationID,
	})
	if !errors.Is(err, writeErr) {
		t.Fatalf("expected operation error, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestLockAccountShareEndRuntimeRowsCountsOpenBindingsWithoutIntentBlockers(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()

	membershipID := int64(251051)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id\\s+FROM account_share_membership_account_bindings").
		WithArgs(membershipID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(4001)))
	mock.ExpectRollback()

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}
	openBindings, pendingIntents, err := lockAccountShareEndRuntimeRowsInTx(
		context.Background(),
		tx,
		membershipID,
	)
	if err != nil {
		t.Fatalf("lockAccountShareEndRuntimeRowsInTx failed: %v", err)
	}
	if openBindings != 1 {
		t.Fatalf("expected one open binding, got %d", openBindings)
	}
	if pendingIntents != 0 {
		t.Fatalf("synchronous billing must never report pending intent blockers, got %d", pendingIntents)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryFinalizeMembershipEndClosesBindingAndOperation(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	membershipID := int64(25106)
	listingID := int64(526)
	accountID := int64(90)
	ownerUserID := int64(1001)
	consumerUserID := int64(18467)
	apiKeyID := int64(91)
	listingVersion := int64(13)
	operationID := "8e500d54-5aa4-4f63-a14e-3bdac9f32e49"
	endingRequestedAt := time.Date(2026, 7, 27, 4, 30, 0, 0, time.UTC)
	updatedAt := endingRequestedAt.Add(time.Second)

	mock.ExpectBegin()
	expectAccountShareEndListingLock(mock, membershipID, 0, listingID, listingVersion)
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id").
		WithArgs(membershipID).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			accountShareEndMembershipRow(
				membershipID, listingID, accountID, ownerUserID, consumerUserID, apiKeyID,
				service.AccountShareMembershipStatusEnding, endingRequestedAt.Add(-time.Hour), updatedAt,
			)...,
		))
	expectAccountShareEndState(mock, membershipID, endingRequestedAt, "manual", "pending", operationID)
	mock.ExpectQuery("SELECT status\\s+FROM account_share_room_operations").
		WithArgs(operationID, membershipID).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("pending"))
	mock.ExpectQuery("SELECT id\\s+FROM account_share_membership_account_bindings").
		WithArgs(membershipID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(4002)))
	mock.ExpectQuery("SELECT id\\s+FROM users").
		WithArgs(pq.Array([]int64{consumerUserID, ownerUserID}), consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).
			AddRow(ownerUserID).
			AddRow(consumerUserID))
	mock.ExpectExec("UPDATE account_share_membership_account_bindings").
		WithArgs(endingRequestedAt, consumerUserID, "consumer", "membership_ended", membershipID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("UPDATE account_share_memberships m\\s+SET status").
		WithArgs(
			service.AccountShareMembershipStatusEnded,
			endingRequestedAt,
			service.AccountShareMembershipEndReasonManual,
			endingRequestedAt,
			membershipID,
			service.AccountShareMembershipStatusEnding,
			operationID,
		).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			accountShareEndMembershipEndedRow(
				membershipID, listingID, accountID, ownerUserID, consumerUserID, apiKeyID,
				endingRequestedAt.Add(-time.Hour), updatedAt.Add(time.Second),
			)...,
		))
	mock.ExpectExec("UPDATE account_share_room_operations\\s+SET status = 'succeeded'").
		WithArgs(listingVersion, sqlmock.AnyArg(), operationID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	membership, billing, finalized, err := repo.FinalizeMembershipEnd(context.Background(), membershipID, operationID)
	if err != nil {
		t.Fatalf("FinalizeMembershipEnd failed: %v", err)
	}
	if !finalized || membership == nil || membership.Status != service.AccountShareMembershipStatusEnded {
		t.Fatalf("membership was not finalized: finalized=%t membership=%#v", finalized, membership)
	}
	if membership.SettlementStatus != "settled" || membership.EndingOperationID != operationID {
		t.Fatalf("final state was not preserved: %#v", membership)
	}
	if billing == nil || billing.Processed != 1 {
		t.Fatalf("unexpected billing result: %#v", billing)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryGetMembershipForEndReturnsAlreadyEndedSnapshot(t *testing.T) {
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
	mock.ExpectQuery("SELECT listing_id").
		WithArgs(membershipID, consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"listing_id"}).AddRow(listingID))
	mock.ExpectQuery("SELECT row_version").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{"row_version"}).AddRow(int64(8)))
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
	mock.ExpectQuery("SELECT\\s+ending_requested_at").
		WithArgs(membershipID).
		WillReturnRows(sqlmock.NewRows([]string{
			"ending_requested_at",
			"ending_reason",
			"settlement_status",
			"ending_operation_id",
		}).AddRow(nil, nil, "settled", nil))
	mock.ExpectCommit()

	membership, err := repo.GetMembershipForEnd(context.Background(), consumerUserID, membershipID)
	if err != nil {
		t.Fatalf("GetMembershipForEnd failed: %v", err)
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
		WithArgs(service.AccountShareListingStatusActive, service.AccountShareListingStatusSuspended, 50, now).
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

func accountShareArchiveRevisionRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"listing_id",
		"revision_number",
		"schema_version",
		"snapshot_quality",
		"room_name",
		"platform",
		"account_level",
		"owner_user_id",
		"owner_display_name_snapshot",
		"status",
		"seat_limit",
		"rate_multiplier",
		"allowed_models",
		"per_user_concurrency",
		"hourly_rate",
		"hourly_fee_waiver_minimum",
		"min_balance_required",
		"codex_cli_only",
		"codex_5h_limit_percent",
		"codex_7d_limit_percent",
	})
}

func TestAccountShareModeRepositoryListArchiveRestoresDeletedRevisionSnapshot(t *testing.T) {
	queryMatcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		normalized := strings.ToLower(strings.Join(strings.Fields(actualSQL), " "))
		switch expectedSQL {
		case "archive listings":
			for _, fragment := range []string{
				"from account_share_listings l",
				"l.deleted_at is not null",
				"l.owner_user_id = $1",
			} {
				if !strings.Contains(normalized, fragment) {
					return fmt.Errorf("archive listing query missing %q", fragment)
				}
			}
		case "archive deleted revisions":
			for _, fragment := range []string{
				"from account_share_listings listing",
				"join account_share_listing_revisions revision",
				"revision.id = listing.deleted_revision_id",
				"revision.listing_id = listing.id",
				"listing.id = any($1::bigint[])",
				"listing.deleted_at is not null",
			} {
				if !strings.Contains(normalized, fragment) {
					return fmt.Errorf("archive revision query missing %q", fragment)
				}
			}
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(queryMatcher))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	const (
		viewerUserID int64 = 9
		listingID    int64 = 7
		revisionID   int64 = 701
	)
	mock.ExpectQuery("archive listings").
		WithArgs(viewerUserID, 21, 0).
		WillReturnRows(accountShareListingRows(
			listingID,
			88,
			viewerUserID,
			"mutable-edit-session",
			time.Now().UTC().Add(time.Hour),
			func(row *accountShareListingRowData) {
				row.RowVersion = 99
				row.CurrentRevisionID = int64(999)
				row.Deleted = true
				row.RoomName = "mutable-final-room"
				row.Status = service.AccountShareListingStatusDraining
				row.RateMultiplier = 9.9
				row.HourlyRate = 8.8
				row.HourlyFeeWaiverMinimum = 7.7
			},
		))
	mock.ExpectQuery("archive deleted revisions").
		WithArgs(pq.Array([]int64{listingID})).
		WillReturnRows(accountShareArchiveRevisionRows().AddRow(
			revisionID,
			listingID,
			int64(4),
			1,
			service.AccountShareSnapshotQualityExact,
			"immutable-deleted-room",
			service.PlatformAnthropic,
			"team",
			viewerUserID,
			"immutable-owner",
			service.AccountShareListingStatusPaused,
			12,
			0.45,
			[]byte(`["claude-sonnet-4-5","claude-opus-4-1"]`),
			7,
			0.33,
			0.22,
			6.5,
			true,
			84.0,
			73.0,
		))

	listings, result, err := repo.ListListings(
		context.Background(),
		viewerUserID,
		service.AccountShareListingFilters{
			Tab:       service.AccountShareModeListingTabArchive,
			SkipTotal: true,
		},
		pagination.PaginationParams{Page: 1, PageSize: 20},
	)
	if err != nil {
		t.Fatalf("ListListings archive failed: %v", err)
	}
	if len(listings) != 1 {
		t.Fatalf("archive listings len = %d, want 1", len(listings))
	}
	listing := listings[0]
	if !listing.Deleted ||
		listing.RowVersion != 4 ||
		listing.CurrentRevisionID == nil ||
		*listing.CurrentRevisionID != revisionID ||
		listing.HistorySnapshotQuality != service.AccountShareSnapshotQualityExact ||
		listing.RoomName != "immutable-deleted-room" ||
		listing.Platform != service.PlatformAnthropic ||
		listing.AccountLevel != "team" ||
		listing.OwnerUserID != viewerUserID ||
		listing.OwnerUsername != "immutable-owner" ||
		listing.Status != service.AccountShareListingStatusPaused ||
		listing.SeatLimit != 12 ||
		math.Abs(listing.RateMultiplier-0.45) > 1e-9 ||
		!reflect.DeepEqual(listing.AllowedModels, []string{"claude-sonnet-4-5", "claude-opus-4-1"}) ||
		listing.PerUserConcurrency != 7 ||
		math.Abs(listing.HourlyRate-0.33) > 1e-9 ||
		math.Abs(listing.HourlyFeeWaiverMinimum-0.22) > 1e-9 ||
		math.Abs(listing.MinBalanceRequired-6.5) > 1e-9 ||
		!listing.CodexCLIOnly ||
		listing.Codex5hLimitPercent != 84 ||
		listing.Codex7dLimitPercent != 73 ||
		listing.Anthropic5hLimitPercent != 84 ||
		listing.Anthropic7dLimitPercent != 73 {
		t.Fatalf("archive listing did not use immutable deleted revision: %#v", listing)
	}
	if listing.AccountID != 0 ||
		listing.AccountName != "" ||
		listing.AccountConcurrency != 0 ||
		listing.AccountIdentityID != nil ||
		listing.AccountCount != 0 ||
		listing.HealthyAccountCount != 0 ||
		listing.ActiveSeats != 0 ||
		listing.EditingByUserID != nil ||
		listing.EditingByUsername != "" ||
		listing.EditingExpiresAt != nil ||
		listing.EditSessionID != "" {
		t.Fatalf("archive listing leaked current account or edit projection: %#v", listing)
	}
	if listing.RoomName == "mutable-final-room" ||
		listing.OwnerUsername == "owner" ||
		listing.RowVersion == 99 ||
		math.Abs(listing.RateMultiplier-9.9) < 1e-9 {
		t.Fatalf("archive listing reused mutable listing values: %#v", listing)
	}
	if result == nil || result.Total != 1 || result.Page != 1 || result.PageSize != 20 {
		t.Fatalf("unexpected archive pagination: %#v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestApplyAccountShareArchiveSnapshotsFailsClosedPerListingAndUsesOneBatch(t *testing.T) {
	queryMatcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if expectedSQL != "archive snapshot batch" {
			return nil
		}
		normalized := strings.ToLower(strings.Join(strings.Fields(actualSQL), " "))
		for _, fragment := range []string{
			"revision.id = listing.deleted_revision_id",
			"revision.listing_id = listing.id",
			"listing.id = any($1::bigint[])",
		} {
			if !strings.Contains(normalized, fragment) {
				return fmt.Errorf("archive snapshot batch query missing %q", fragment)
			}
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(queryMatcher))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	listingIDs := []int64{7, 8, 9, 10}
	mock.ExpectQuery("archive snapshot batch").
		WithArgs(pq.Array(listingIDs)).
		WillReturnRows(accountShareArchiveRevisionRows().
			AddRow(
				int64(701),
				listingIDs[0],
				int64(4),
				1,
				"forged",
				"untrusted-quality-room",
				service.PlatformOpenAI,
				"pro",
				int64(91),
				"untrusted-quality-owner",
				service.AccountShareListingStatusPaused,
				4,
				0.4,
				[]byte(`["gpt-5.5"]`),
				5,
				0.2,
				0.1,
				1.0,
				false,
				90.0,
				80.0,
			).
			AddRow(
				int64(703),
				listingIDs[2],
				int64(6),
				1,
				service.AccountShareSnapshotQualityBackfilledCurrent,
				"backfilled-room",
				service.PlatformOpenAI,
				"team",
				int64(93),
				"backfilled-owner",
				service.AccountShareListingStatusDisabled,
				11,
				0.6,
				[]byte(`["gpt-5.4"]`),
				6,
				0.3,
				0.2,
				2.0,
				true,
				88.0,
				77.0,
			).
			AddRow(
				int64(704),
				listingIDs[3],
				int64(7),
				1,
				service.AccountShareSnapshotQualityExact,
				"malformed-content-room",
				service.PlatformOpenAI,
				"pro",
				int64(94),
				"malformed-content-owner",
				service.AccountShareListingStatusPaused,
				3,
				0.2,
				[]byte(`{"not":"an-array"}`),
				3,
				0.1,
				0.0,
				1.0,
				false,
				99.0,
				99.0,
			))

	listings := make([]service.AccountShareListing, 0, len(listingIDs))
	for _, listingID := range listingIDs {
		currentRevisionID := listingID + 1000
		accountIdentityID := listingID + 2000
		listings = append(listings, service.AccountShareListing{
			ID:                      listingID,
			RowVersion:              99,
			CurrentRevisionID:       &currentRevisionID,
			Deleted:                 true,
			AccountID:               listingID + 3000,
			RoomName:                "mutable-current-room",
			Platform:                service.PlatformOpenAI,
			OwnerUserID:             listingID + 4000,
			OwnerUsername:           "mutable-current-owner",
			AccountName:             "mutable-current-account",
			Status:                  service.AccountShareListingStatusDraining,
			SeatLimit:               15,
			AccountIdentityID:       &accountIdentityID,
			RatingCount:             10,
			RatingScoreSum:          90,
			RatingAvg:               9,
			RateMultiplier:          9.9,
			AllowedModels:           []string{"mutable-current-model"},
			PerUserConcurrency:      15,
			AccountConcurrency:      30,
			HourlyRate:              8.8,
			HourlyFeeWaiverMinimum:  7.7,
			MinBalanceRequired:      6.6,
			CodexCLIOnly:            true,
			Codex5hLimitPercent:     50,
			Codex7dLimitPercent:     40,
			Anthropic5hLimitPercent: 30,
			Anthropic7dLimitPercent: 20,
			AccountLevel:            service.AccountLevelPro,
			HistorySnapshotQuality:  service.AccountShareSnapshotQualityExact,
		})
	}

	if err := repo.applyAccountShareArchiveSnapshots(context.Background(), listings); err != nil {
		t.Fatalf("applyAccountShareArchiveSnapshots failed: %v", err)
	}
	for _, index := range []int{0, 1, 3} {
		listing := listings[index]
		if listing.HistorySnapshotQuality != service.AccountShareSnapshotQualityUnknown ||
			listing.RowVersion != 0 ||
			listing.CurrentRevisionID != nil ||
			listing.RoomName != "" ||
			listing.Platform != "" ||
			listing.OwnerUserID != 0 ||
			listing.OwnerUsername != "" ||
			listing.AccountID != 0 ||
			listing.AccountName != "" ||
			listing.AccountIdentityID != nil ||
			listing.Status != "" ||
			listing.SeatLimit != 0 ||
			listing.RatingCount != 0 ||
			listing.RatingScoreSum != 0 ||
			listing.RatingAvg != 0 ||
			listing.RateMultiplier != 0 ||
			len(listing.AllowedModels) != 0 ||
			listing.PerUserConcurrency != 0 ||
			listing.AccountConcurrency != 0 ||
			listing.HourlyRate != 0 ||
			listing.HourlyFeeWaiverMinimum != 0 ||
			listing.MinBalanceRequired != 0 ||
			listing.CodexCLIOnly ||
			listing.Codex5hLimitPercent != 0 ||
			listing.Codex7dLimitPercent != 0 ||
			listing.Anthropic5hLimitPercent != 0 ||
			listing.Anthropic7dLimitPercent != 0 ||
			listing.AccountLevel != "" {
			t.Fatalf("untrusted archive listing %d leaked mutable projection: %#v", listing.ID, listing)
		}
	}

	backfilled := listings[2]
	if backfilled.HistorySnapshotQuality != service.AccountShareSnapshotQualityBackfilledCurrent ||
		backfilled.RowVersion != 6 ||
		backfilled.CurrentRevisionID == nil ||
		*backfilled.CurrentRevisionID != 703 ||
		backfilled.RoomName != "backfilled-room" ||
		backfilled.OwnerUserID != 93 ||
		backfilled.OwnerUsername != "backfilled-owner" ||
		backfilled.Status != service.AccountShareListingStatusDisabled ||
		!reflect.DeepEqual(backfilled.AllowedModels, []string{"gpt-5.4"}) {
		t.Fatalf("backfilled archive snapshot was not restored: %#v", backfilled)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func accountShareMembershipHistorySnapshotRows(
	membershipID int64,
	listingID int64,
	revisionID driver.Value,
	listingVersion driver.Value,
	roomName string,
	ownerUserID int64,
	ownerUsername string,
	platform string,
	accountLevel string,
	apiKeyName string,
	termsSnapshot driver.Value,
	accountID int64,
	accountName string,
	accountConcurrency int,
	snapshotQuality string,
) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"membership_id",
		"listing_id",
		"listing_revision_id",
		"listing_version_snapshot",
		"room_name",
		"owner_user_id",
		"owner_username",
		"platform",
		"account_level",
		"api_key_name",
		"terms_snapshot",
		"account_id",
		"account_name",
		"account_concurrency",
		"snapshot_quality",
	}).AddRow(
		membershipID,
		listingID,
		revisionID,
		listingVersion,
		roomName,
		ownerUserID,
		ownerUsername,
		platform,
		accountLevel,
		apiKeyName,
		termsSnapshot,
		accountID,
		accountName,
		accountConcurrency,
		snapshotQuality,
	)
}

func TestAccountShareModeRepositoryListHistoryKeepsDeletedRoomAndUnboundAccountSnapshot(t *testing.T) {
	queryMatcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		normalized := strings.ToLower(strings.Join(strings.Fields(actualSQL), " "))
		switch expectedSQL {
		case "deleted room history list":
			if !strings.Contains(normalized, "where hm.id is not null and qm.id is null") {
				return errors.New("history list must be owned by the viewer's ended membership")
			}
			if strings.Contains(normalized, "where l.deleted_at is null and a.deleted_at is null and hm.id is not null") {
				return errors.New("history list must not discard a soft-deleted room")
			}
			if !strings.Contains(normalized, "left join lateral ( select a.* from account_share_room_accounts") {
				return errors.New("history list must tolerate a missing current representative account")
			}
		case "deleted room history snapshot":
			for _, fragment := range []string{
				"left join account_share_listing_revisions revision",
				"from account_share_membership_account_bindings binding",
				"m.consumer_user_id = $2",
			} {
				if !strings.Contains(normalized, fragment) {
					return fmt.Errorf("history snapshot query missing %q", fragment)
				}
			}
			if strings.Contains(normalized, "binding.unbound_at is null") ||
				strings.Contains(normalized, "l.deleted_at is null") {
				return errors.New("history snapshot must retain closed bindings and deleted listings")
			}
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(queryMatcher))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	viewerUserID := int64(42)
	listingID := int64(7)
	membershipID := int64(91)
	revisionID := int64(701)
	listingVersion := int64(3)
	accountID := int64(88)
	lastUsedAt := time.Date(2026, 7, 26, 11, 0, 0, 0, time.UTC)

	mock.ExpectQuery("deleted room history list").
		WithArgs(viewerUserID, 21, 0).
		WillReturnRows(accountShareListingRows(
			listingID,
			0,
			9,
			"",
			time.Time{},
			func(row *accountShareListingRowData) {
				row.RowVersion = 9
				row.Deleted = true
				row.RoomName = "deleted-live-projection"
				row.Status = service.AccountShareListingStatusDraining
				row.LastUsedMembershipID = membershipID
				row.LastUsedAt = lastUsedAt
			},
		))
	mock.ExpectQuery("deleted room history snapshot").
		WithArgs(pq.Array([]int64{membershipID}), viewerUserID).
		WillReturnRows(accountShareMembershipHistorySnapshotRows(
			membershipID,
			listingID,
			revisionID,
			listingVersion,
			"immutable-room",
			9,
			"owner-snapshot",
			service.PlatformOpenAI,
			"pro",
			"archived-key",
			accountShareRuntimeTermsJSON(revisionID, listingVersion, 0.35),
			accountID,
			"detached-account-snapshot",
			15,
			service.AccountShareSnapshotQualityExact,
		))

	listings, result, err := repo.ListListings(
		context.Background(),
		viewerUserID,
		service.AccountShareListingFilters{
			Tab:       service.AccountShareModeListingTabHistory,
			SkipTotal: true,
		},
		pagination.PaginationParams{Page: 1, PageSize: 20},
	)
	if err != nil {
		t.Fatalf("ListListings history failed: %v", err)
	}
	if len(listings) != 1 {
		t.Fatalf("history listings len = %d, want 1", len(listings))
	}
	listing := listings[0]
	if !listing.Deleted {
		t.Fatalf("deleted history listing lost deleted marker: %#v", listing)
	}
	if listing.RoomName != "immutable-room" ||
		listing.AccountID != accountID ||
		listing.AccountName != "detached-account-snapshot" ||
		listing.AccountConcurrency != 15 ||
		listing.RowVersion != listingVersion ||
		listing.CurrentRevisionID == nil ||
		*listing.CurrentRevisionID != revisionID ||
		listing.HistorySnapshotQuality != service.AccountShareSnapshotQualityExact ||
		math.Abs(listing.RateMultiplier-0.35) > 1e-9 ||
		listing.Anthropic5hLimitPercent != 91 ||
		listing.Anthropic7dLimitPercent != 92 {
		t.Fatalf("history listing did not use immutable membership snapshot: %#v", listing)
	}
	if listing.AccountCount != 0 ||
		listing.HealthyAccountCount != 0 ||
		listing.ActiveSeats != 0 ||
		listing.AccountStatus != "" ||
		listing.AccountSchedulable ||
		listing.CurrentConcurrency != 0 ||
		listing.EditingByUserID != nil ||
		listing.EditingExpiresAt != nil ||
		listing.EditSessionID != "" {
		t.Fatalf("history listing leaked current runtime or edit state: %#v", listing)
	}
	if result == nil || result.Total != 1 || result.Page != 1 || result.PageSize != 20 {
		t.Fatalf("unexpected history pagination: %#v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestApplyAccountShareHistorySnapshotsMarksLegacyRowsUnknownAndClearsCurrentProjection(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	viewerUserID := int64(42)
	listingID := int64(7)
	membershipID := int64(91)
	lastUsedAt := time.Date(2026, 7, 26, 11, 0, 0, 0, time.UTC)
	mock.ExpectQuery("FROM account_share_memberships m").
		WithArgs(pq.Array([]int64{membershipID}), viewerUserID).
		WillReturnRows(accountShareMembershipHistorySnapshotRows(
			membershipID,
			listingID,
			nil,
			nil,
			"",
			0,
			"",
			"",
			"",
			"",
			nil,
			0,
			"",
			0,
			"",
		))

	revisionID := int64(701)
	accountIdentityID := int64(88)
	listings := []service.AccountShareListing{{
		ID:                      listingID,
		RowVersion:              9,
		CurrentRevisionID:       &revisionID,
		Deleted:                 true,
		AccountID:               88,
		RoomName:                "mutable-final-room",
		Platform:                service.PlatformOpenAI,
		OwnerUserID:             9,
		OwnerUsername:           "mutable-owner",
		AccountName:             "mutable-account",
		Status:                  service.AccountShareListingStatusDraining,
		SeatLimit:               15,
		AccountIdentityID:       &accountIdentityID,
		RatingCount:             10,
		RatingScoreSum:          90,
		RatingAvg:               9,
		RateMultiplier:          0.35,
		AllowedModels:           []string{"gpt-5.5"},
		PerUserConcurrency:      3,
		AccountConcurrency:      20,
		HourlyRate:              1.5,
		HourlyFeeWaiverMinimum:  2,
		MinBalanceRequired:      10,
		CodexCLIOnly:            true,
		Codex5hLimitPercent:     80,
		Codex7dLimitPercent:     70,
		Anthropic5hLimitPercent: 60,
		Anthropic7dLimitPercent: 50,
		AccountLevel:            service.AccountLevelPro,
		LastUsedMembershipID:    &membershipID,
		LastUsedAt:              &lastUsedAt,
	}}

	if err := repo.applyAccountShareHistorySnapshots(context.Background(), viewerUserID, listings); err != nil {
		t.Fatalf("applyAccountShareHistorySnapshots failed: %v", err)
	}
	listing := listings[0]
	if listing.HistorySnapshotQuality != service.AccountShareSnapshotQualityUnknown {
		t.Fatalf("history snapshot quality = %q, want unknown", listing.HistorySnapshotQuality)
	}
	if !listing.Deleted || listing.LastUsedMembershipID == nil ||
		*listing.LastUsedMembershipID != membershipID || listing.LastUsedAt == nil ||
		!listing.LastUsedAt.Equal(lastUsedAt) {
		t.Fatalf("legacy identity fields were not preserved: %#v", listing)
	}
	if listing.RowVersion != 0 ||
		listing.CurrentRevisionID != nil ||
		listing.RoomName != "" ||
		listing.Platform != "" ||
		listing.OwnerUserID != 0 ||
		listing.OwnerUsername != "" ||
		listing.AccountID != 0 ||
		listing.AccountName != "" ||
		listing.AccountIdentityID != nil ||
		listing.Status != "" ||
		listing.SeatLimit != 0 ||
		listing.RatingCount != 0 ||
		listing.RatingScoreSum != 0 ||
		listing.RatingAvg != 0 ||
		listing.RateMultiplier != 0 ||
		len(listing.AllowedModels) != 0 ||
		listing.PerUserConcurrency != 0 ||
		listing.AccountConcurrency != 0 ||
		listing.HourlyRate != 0 ||
		listing.HourlyFeeWaiverMinimum != 0 ||
		listing.MinBalanceRequired != 0 ||
		listing.CodexCLIOnly ||
		listing.Codex5hLimitPercent != 0 ||
		listing.Codex7dLimitPercent != 0 ||
		listing.Anthropic5hLimitPercent != 0 ||
		listing.Anthropic7dLimitPercent != 0 ||
		listing.AccountLevel != service.AccountLevelUnknown {
		t.Fatalf("legacy history leaked mutable listing projection: %#v", listing)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryListMembershipHistoryKeepsEveryStayAndOnlySnapshots(t *testing.T) {
	queryMatcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if expectedSQL != "membership history records" {
			return nil
		}
		normalized := strings.ToLower(strings.Join(strings.Fields(actualSQL), " "))
		for _, required := range []string{
			"from account_share_memberships membership",
			"from account_share_membership_account_bindings binding",
			"from account_share_mode_settlement_entries entry",
			"membership.consumer_user_id = $1",
			"membership.status = $2",
			"sum(entry.base_charge)",
			"entry.settlement_type = 'usage_request'",
			"history_binding.configured_concurrency_snapshot",
			"order by coalesce(membership.ended_at, membership.updated_at, membership.joined_at) desc",
		} {
			if !strings.Contains(normalized, required) {
				return fmt.Errorf("membership history query missing %q", required)
			}
		}
		for _, forbidden := range []string{
			"left join accounts",
			"left join api_keys",
			"left join users",
			"listing.room_name",
			"listing.platform",
			"listing.account_level",
			"credentials",
			"proxy",
			"health_state",
			"in_flight",
		} {
			if strings.Contains(normalized, forbidden) {
				return fmt.Errorf("membership history query must not read current field %q", forbidden)
			}
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(queryMatcher))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	consumerUserID := int64(42)
	listingID := int64(7)
	deletedAt := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	firstJoinedAt := deletedAt.Add(-4 * time.Hour)
	firstLastRequestAt := firstJoinedAt.Add(20 * time.Minute)
	firstEndedAt := firstJoinedAt.Add(time.Hour)
	secondJoinedAt := deletedAt.Add(-2 * time.Hour)
	secondLastRequestAt := secondJoinedAt.Add(15 * time.Minute)
	secondEndedAt := secondJoinedAt.Add(time.Hour)
	reviewCreatedAt := secondEndedAt.Add(time.Minute)
	columns := []string{
		"membership_id",
		"listing_id",
		"listing_revision_id",
		"listing_version_snapshot",
		"room_name",
		"room_deleted",
		"room_deleted_at",
		"owner_user_id",
		"owner_username",
		"platform",
		"account_level",
		"account_id",
		"account_name",
		"account_concurrency",
		"api_key_id",
		"api_key_name",
		"status",
		"joined_at",
		"last_request_at",
		"ended_at",
		"ended_reason",
		"paid_until",
		"billed_until",
		"hourly_rate_snapshot",
		"hourly_fee_waiver_minimum_snapshot",
		"idle_timeout_minutes",
		"usage_request_count",
		"usage_request_cost",
		"terms_snapshot",
		"snapshot_quality",
		"review_id",
		"review_score",
		"review_comment",
		"review_comment_status",
		"review_comment_reject_reason",
		"review_created_at",
	}
	rows := sqlmock.NewRows(columns).
		AddRow(
			int64(91),
			listingID,
			int64(701),
			int64(3),
			"membership-room-1",
			true,
			deletedAt,
			int64(9),
			"owner-snapshot",
			service.PlatformOpenAI,
			"pro",
			int64(88),
			"account-snapshot-1",
			15,
			int64(501),
			"key-snapshot-1",
			service.AccountShareMembershipStatusEnded,
			firstJoinedAt,
			firstLastRequestAt,
			firstEndedAt,
			"user_ended",
			nil,
			firstEndedAt,
			0.2,
			0.1,
			30,
			int64(3),
			1.25,
			accountShareRuntimeTermsJSON(701, 3, 0.35),
			service.AccountShareSnapshotQualityExact,
			nil,
			nil,
			"",
			"",
			"",
			nil,
		).
		AddRow(
			int64(92),
			listingID,
			int64(702),
			int64(4),
			"membership-room-2",
			true,
			deletedAt,
			int64(9),
			"owner-snapshot",
			service.PlatformOpenAI,
			"team",
			int64(89),
			"account-snapshot-2",
			12,
			int64(502),
			"key-snapshot-2",
			service.AccountShareMembershipStatusEnded,
			secondJoinedAt,
			secondLastRequestAt,
			secondEndedAt,
			"idle_timeout",
			nil,
			secondEndedAt,
			0.3,
			0.1,
			45,
			int64(5),
			2.75,
			accountShareRuntimeTermsJSON(702, 4, 0.45),
			"",
			int64(900),
			9,
			"稳定",
			service.AccountShareReviewCommentStatusApproved,
			"",
			reviewCreatedAt,
		)

	mock.ExpectQuery("SELECT COUNT").
		WithArgs(consumerUserID, service.AccountShareMembershipStatusEnded).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(2)))
	mock.ExpectQuery("membership history records").
		WithArgs(
			consumerUserID,
			service.AccountShareMembershipStatusEnded,
			2,
			0,
		).
		WillReturnRows(rows)

	entries, result, err := repo.ListMembershipHistory(
		context.Background(),
		consumerUserID,
		pagination.PaginationParams{Page: 1, PageSize: 2},
	)
	if err != nil {
		t.Fatalf("ListMembershipHistory failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("history entries len = %d, want 2", len(entries))
	}
	if entries[0].ListingID != listingID ||
		entries[1].ListingID != listingID ||
		entries[0].MembershipID == entries[1].MembershipID {
		t.Fatalf("same-room stays were collapsed: %#v", entries)
	}
	if !entries[0].RoomDeleted ||
		entries[0].RoomDeletedAt == nil ||
		entries[0].AccountName != "account-snapshot-1" ||
		entries[0].ConfiguredConcurrencySnapshot != 15 ||
		entries[0].APIKeyName != "key-snapshot-1" ||
		entries[0].UsageRequestCount != 3 ||
		entries[0].SnapshotQuality != service.AccountShareSnapshotQualityExact {
		t.Fatalf("first immutable history snapshot mismatch: %#v", entries[0])
	}
	if entries[1].Review == nil ||
		entries[1].Review.ID != 900 ||
		entries[1].Review.Score != 9 ||
		entries[1].Review.Comment != "稳定" ||
		entries[1].SnapshotQuality != service.AccountShareSnapshotQualityUnknown {
		t.Fatalf("second history review mismatch: %#v", entries[1])
	}
	if result == nil || result.Total != 2 || result.Page != 1 || result.PageSize != 2 {
		t.Fatalf("unexpected pagination: %#v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryListAllStillExcludesDeletedRooms(t *testing.T) {
	queryMatcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if expectedSQL != "live listing visibility" {
			return nil
		}
		normalized := strings.ToLower(strings.Join(strings.Fields(actualSQL), " "))
		if !strings.Contains(
			normalized,
			"where l.deleted_at is null and a.deleted_at is null and l.status = 'active'",
		) {
			return errors.New("ordinary account plaza list must continue excluding deleted rooms")
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(queryMatcher))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	mock.ExpectQuery("live listing visibility").
		WithArgs(int64(42), 21, 0).
		WillReturnRows(accountShareListingRows(7, 8, 9, "", time.Time{}))

	listings, _, err := repo.ListListings(
		context.Background(),
		42,
		service.AccountShareListingFilters{SkipTotal: true},
		pagination.PaginationParams{Page: 1, PageSize: 20},
	)
	if err != nil {
		t.Fatalf("ListListings all failed: %v", err)
	}
	if len(listings) != 1 {
		t.Fatalf("live listings len = %d, want 1", len(listings))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryListingVisibilityMatrix(t *testing.T) {
	tests := []struct {
		name      string
		filters   service.AccountShareListingFilters
		required  []string
		forbidden []string
	}{
		{
			name: "public all cannot bypass active status",
			filters: service.AccountShareListingFilters{
				Tab:       service.AccountShareModeListingTabAll,
				Status:    "all",
				SkipTotal: true,
			},
			required: []string{"l.status = 'active'"},
		},
		{
			name: "admin all keeps operational visibility",
			filters: service.AccountShareListingFilters{
				Tab:           service.AccountShareModeListingTabAll,
				Status:        "all",
				ViewerIsAdmin: true,
				SkipTotal:     true,
			},
			forbidden: []string{"l.status = 'active'"},
		},
		{
			name: "owner mine keeps non-public rooms",
			filters: service.AccountShareListingFilters{
				Tab:       service.AccountShareModeListingTabMine,
				Status:    "all",
				SkipTotal: true,
			},
			required:  []string{"l.owner_user_id = $1"},
			forbidden: []string{"l.status = 'active'"},
		},
		{
			name: "effective member using keeps non-public rooms",
			filters: service.AccountShareListingFilters{
				Tab:       service.AccountShareModeListingTabUsing,
				Status:    "all",
				SkipTotal: true,
			},
			required:  []string{"qm.id is not null"},
			forbidden: []string{"l.status = 'active'"},
		},
		{
			name: "history keeps ended membership rooms",
			filters: service.AccountShareListingFilters{
				Tab:       service.AccountShareModeListingTabHistory,
				Status:    "all",
				SkipTotal: true,
			},
			required:  []string{"hm.id is not null", "qm.id is null"},
			forbidden: []string{"l.status = 'active'"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
				if expectedSQL != "listing visibility matrix" {
					return nil
				}
				normalized := strings.ToLower(strings.Join(strings.Fields(actualSQL), " "))
				for _, fragment := range tt.required {
					if !strings.Contains(normalized, fragment) {
						return fmt.Errorf("listing query missing required visibility predicate %q", fragment)
					}
				}
				for _, fragment := range tt.forbidden {
					if strings.Contains(normalized, fragment) {
						return fmt.Errorf("listing query contains forbidden visibility predicate %q", fragment)
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
			querySentinel := errors.New("stop after visibility query")

			mock.ExpectQuery("listing visibility matrix").
				WithArgs(int64(42), 21, 0).
				WillReturnError(querySentinel)

			_, _, err = repo.ListListings(
				context.Background(),
				42,
				tt.filters,
				pagination.PaginationParams{Page: 1, PageSize: 20},
			)
			if !errors.Is(err, querySentinel) {
				t.Fatalf("ListListings error = %v, want query sentinel", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet expectations: %v", err)
			}
		})
	}
}

func TestAccountShareModeRepositoryGetVisibleListingPermissionMatrix(t *testing.T) {
	matcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if expectedSQL != "visible listing detail" {
			return nil
		}
		normalized := strings.ToLower(strings.Join(strings.Fields(actualSQL), " "))
		for _, fragment := range []string{
			"$3::boolean",
			"l.status = 'active'",
			"l.owner_user_id = $1",
			"from account_share_memberships visible_membership",
			"visible_membership.consumer_user_id = $1",
			"visible_membership.status in ('active', 'queued', 'ending', 'ended')",
			"visible_membership.deleted_at is null",
		} {
			if !strings.Contains(normalized, fragment) {
				return fmt.Errorf("visible detail query missing %q", fragment)
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

	mock.ExpectQuery("visible listing detail").
		WithArgs(int64(42), int64(7), false).
		WillReturnRows(accountShareListingRows(7, 70, 700, "", time.Time{}))

	listing, err := repo.GetVisibleListingByID(context.Background(), 7, 42, false)
	if err != nil {
		t.Fatalf("GetVisibleListingByID failed: %v", err)
	}
	if listing == nil || listing.ID != 7 {
		t.Fatalf("unexpected listing: %#v", listing)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryListRoomRuntimeAccountsBatchesActiveAccounts(t *testing.T) {
	matcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if expectedSQL != "room runtime accounts" {
			return nil
		}
		normalized := strings.ToLower(strings.Join(strings.Fields(actualSQL), " "))
		for _, fragment := range []string{
			"from account_share_room_accounts room_account",
			"join accounts a on a.id = room_account.account_id",
			"room_account.listing_id = any($1)",
			"room_account.state = 'active'",
			"a.deleted_at is null",
		} {
			if !strings.Contains(normalized, fragment) {
				return fmt.Errorf("runtime account query missing %q", fragment)
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
	now := time.Date(2026, 7, 28, 8, 0, 0, 0, time.UTC)

	mock.ExpectQuery("room runtime accounts").
		WithArgs(pq.Array([]int64{7, 8}), now).
		WillReturnRows(sqlmock.NewRows([]string{"listing_id", "account_id", "concurrency"}).
			AddRow(int64(7), int64(70), 3).
			AddRow(int64(7), int64(71), 4).
			AddRow(int64(8), int64(80), 5))

	accountsByListing, err := repo.ListRoomRuntimeAccounts(context.Background(), []int64{7, 8, 7, 0}, now)
	if err != nil {
		t.Fatalf("ListRoomRuntimeAccounts failed: %v", err)
	}
	if !reflect.DeepEqual(accountsByListing, map[int64][]service.AccountWithConcurrency{
		7: {
			{ID: 70, MaxConcurrency: 3},
			{ID: 71, MaxConcurrency: 4},
		},
		8: {
			{ID: 80, MaxConcurrency: 5},
		},
	}) {
		t.Fatalf("unexpected runtime accounts: %#v", accountsByListing)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryArchiveScopesOwnerAndDoesNotRequireRepresentativeAccount(t *testing.T) {
	queryMatcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		normalized := strings.ToLower(strings.Join(strings.Fields(actualSQL), " "))
		switch expectedSQL {
		case "owner archive":
			if !strings.Contains(
				normalized,
				"where l.deleted_at is not null and l.owner_user_id = $1",
			) {
				return errors.New("owner archive must contain owner scope")
			}
		case "admin archive":
			if !strings.Contains(normalized, "where l.deleted_at is not null order by") {
				return errors.New("admin archive must include all deleted rooms")
			}
		case "archive snapshot":
			if !strings.Contains(normalized, "revision.id = listing.deleted_revision_id") ||
				!strings.Contains(normalized, "revision.listing_id = listing.id") {
				return errors.New("archive snapshot must match the deleted revision to its listing")
			}
			return nil
		}
		if !strings.Contains(
			normalized,
			"left join lateral ( select a.* from account_share_room_accounts",
		) {
			return errors.New("archive must tolerate a missing representative account")
		}
		if strings.Contains(normalized, "l.status = 'active'") {
			return errors.New("archive must not apply the live-room status filter")
		}
		return nil
	})

	for _, tt := range []struct {
		name          string
		viewerIsAdmin bool
		expectedQuery string
	}{
		{name: "owner sees own archive", expectedQuery: "owner archive"},
		{name: "admin sees all archives", viewerIsAdmin: true, expectedQuery: "admin archive"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(queryMatcher))
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			defer func() { _ = db.Close() }()
			repo := &accountShareModeRepository{db: db}
			mock.ExpectQuery(tt.expectedQuery).
				WithArgs(int64(42), 21, 0).
				WillReturnRows(accountShareListingRows(
					7,
					0,
					42,
					"",
					time.Time{},
					func(row *accountShareListingRowData) {
						row.Deleted = true
						row.Status = service.AccountShareListingStatusDisabled
						row.RoomName = "已删除房间"
					},
				))
			mock.ExpectQuery("archive snapshot").
				WithArgs(pq.Array([]int64{7})).
				WillReturnRows(accountShareArchiveRevisionRows())

			listings, result, err := repo.ListListings(
				context.Background(),
				42,
				service.AccountShareListingFilters{
					Tab:           service.AccountShareModeListingTabArchive,
					SkipTotal:     true,
					ViewerIsAdmin: tt.viewerIsAdmin,
				},
				pagination.PaginationParams{Page: 1, PageSize: 20},
			)
			if err != nil {
				t.Fatalf("ListListings archive failed: %v", err)
			}
			if len(listings) != 1 ||
				!listings[0].Deleted ||
				listings[0].AccountID != 0 ||
				listings[0].RoomName != "" ||
				listings[0].HistorySnapshotQuality != service.AccountShareSnapshotQualityUnknown {
				t.Fatalf("unexpected archive listing: %#v", listings)
			}
			if result == nil || result.Total != 1 {
				t.Fatalf("unexpected archive pagination: %#v", result)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet expectations: %v", err)
			}
		})
	}
}

func TestAccountShareModeRepositoryGetMySpendRejectsUnrelatedConsumerBeforeHistoryLookup(t *testing.T) {
	queryMatcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if expectedSQL != "unrelated consumer membership" {
			return nil
		}
		normalized := strings.ToLower(strings.Join(strings.Fields(actualSQL), " "))
		if !strings.Contains(normalized, "m.listing_id = $1") ||
			!strings.Contains(normalized, "m.consumer_user_id = $2") {
			return errors.New("spend authorization must be anchored to listing and consumer membership")
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(queryMatcher))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	mock.ExpectQuery("unrelated consumer membership").
		WithArgs(int64(7), int64(404)).
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
		}))

	_, err = repo.GetMySpendSummary(context.Background(), service.AccountShareMySpendQuery{
		ListingID:  7,
		ConsumerID: 404,
		Range:      service.AccountShareSpendRangeCurrentMembership,
		EndTime:    time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, service.ErrAccountShareListingNotFound) {
		t.Fatalf("unrelated consumer error = %v, want listing not found", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryGetMySpendSummaryAggregatesCurrentMembership(t *testing.T) {
	queryMatcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		switch expectedSQL {
		case "my spend membership":
			if !strings.Contains(actualSQL, "FROM account_share_memberships") || !strings.Contains(actualSQL, "m.consumer_user_id = $2") {
				return errors.New("expected consumer membership lookup")
			}
		case "my spend history snapshot":
			if !strings.Contains(actualSQL, "FROM account_share_memberships") ||
				!strings.Contains(actualSQL, "LEFT JOIN account_share_listing_revisions") ||
				!strings.Contains(actualSQL, "FROM account_share_membership_account_bindings") ||
				!strings.Contains(actualSQL, "m.consumer_user_id = $2") {
				return errors.New("expected membership-owned immutable history snapshot lookup")
			}
			if strings.Contains(actualSQL, "l.deleted_at IS NULL") || strings.Contains(actualSQL, "history_binding.unbound_at IS NULL") {
				return errors.New("history snapshot must survive room deletion and account unbinding")
			}
		case "my spend totals":
			if !strings.Contains(actualSQL, "account_share_mode_settlement_entries") ||
				!strings.Contains(actualSQL, "entry.membership_id = $3") ||
				!strings.Contains(actualSQL, "LEFT JOIN usage_logs") {
				return errors.New("expected totals query to aggregate settlement entries with membership filter")
			}
			if strings.Contains(actualSQL, "entry.created_at >=") ||
				strings.Contains(actualSQL, "entry.created_at <") {
				return errors.New("membership totals must include late settlement after membership end")
			}
		case "my spend hourly ledger totals":
			if !strings.Contains(actualSQL, "FROM user_balance_ledger") || !strings.Contains(actualSQL, "metadata->>'membership_id'") {
				return errors.New("expected hourly ledger totals query to filter balance ledger by membership metadata")
			}
			if strings.Contains(actualSQL, "ubl.created_at") {
				return errors.New("membership ledger totals must include late refund after membership end")
			}
		case "my spend models":
			if !strings.Contains(actualSQL, "GROUP BY") ||
				!strings.Contains(actualSQL, "entry.membership_id = $3") ||
				!strings.Contains(actualSQL, "ul.model") {
				return errors.New("expected model query grouped from settlement entries joined to usage logs")
			}
			if strings.Contains(actualSQL, "entry.created_at >=") ||
				strings.Contains(actualSQL, "entry.created_at <") {
				return errors.New("membership model totals must include late settlement after membership end")
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
	revisionID := int64(701)
	listingVersion := int64(3)
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
	mock.ExpectQuery("my spend history snapshot").
		WithArgs(pq.Array([]int64{11}), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"membership_id",
			"listing_id",
			"listing_revision_id",
			"listing_version_snapshot",
			"room_name",
			"owner_user_id",
			"owner_username",
			"platform",
			"account_level",
			"api_key_name",
			"terms_snapshot",
			"account_id",
			"account_name",
			"account_concurrency",
			"snapshot_quality",
		}).AddRow(
			int64(11),
			int64(7),
			revisionID,
			listingVersion,
			"immutable-room",
			int64(9),
			"owner-snapshot",
			service.PlatformOpenAI,
			"pro",
			"archived-key",
			accountShareRuntimeTermsJSON(revisionID, listingVersion, 0.35),
			int64(8),
			"shared-account-snapshot",
			15,
			service.AccountShareSnapshotQualityExact,
		))
	mock.ExpectQuery("my spend totals").
		WithArgs(int64(7), int64(42), int64(11)).
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
		WithArgs(int64(7), int64(42), int64(11)).
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
	if summary.Membership.APIKeyName != "archived-key" {
		t.Fatalf("api key name = %q, want archived-key snapshot", summary.Membership.APIKeyName)
	}
	if summary.Listing.AccountID != 8 ||
		summary.Listing.AccountName != "shared-account-snapshot" ||
		summary.Listing.OwnerUsername != "owner-snapshot" {
		t.Fatalf("unexpected spend history listing snapshot: %#v", summary.Listing)
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

func TestAccountShareMySpendWhereUsesMembershipAsCompleteSettlementBoundary(t *testing.T) {
	startTime := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	endTime := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)

	settlementWhere, settlementArgs := accountShareMySpendSettlementWhere(7, 42, 11, startTime, endTime)
	if !strings.Contains(settlementWhere, "entry.membership_id = $3") ||
		strings.Contains(settlementWhere, "entry.created_at") {
		t.Fatalf("membership settlement scope must not truncate late settlements: %s", settlementWhere)
	}
	if !reflect.DeepEqual(settlementArgs, []any{int64(7), int64(42), int64(11)}) {
		t.Fatalf("unexpected membership settlement args: %#v", settlementArgs)
	}

	ledgerWhere, ledgerArgs := accountShareMySpendLedgerWhere(7, 42, 11, startTime, endTime)
	if !strings.Contains(ledgerWhere, "(ubl.metadata->>'membership_id')::bigint = $6") ||
		strings.Contains(ledgerWhere, "ubl.created_at") {
		t.Fatalf("membership ledger scope must not truncate late refunds: %s", ledgerWhere)
	}
	if !reflect.DeepEqual(ledgerArgs, []any{
		int64(42),
		accountShareSeatPrepayReason,
		accountShareSeatRefundReason,
		accountShareSeatWaiverRefundReason,
		int64(7),
		int64(11),
	}) {
		t.Fatalf("unexpected membership ledger args: %#v", ledgerArgs)
	}
}

func TestAccountShareMySpendWhereKeepsTimeBoundaryForCalendarRanges(t *testing.T) {
	startTime := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	endTime := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)

	settlementWhere, settlementArgs := accountShareMySpendSettlementWhere(7, 42, 0, startTime, endTime)
	if strings.Contains(settlementWhere, "entry.membership_id") ||
		!strings.Contains(settlementWhere, "entry.created_at >= $3") ||
		!strings.Contains(settlementWhere, "entry.created_at < $4") {
		t.Fatalf("calendar settlement scope must keep the requested time range: %s", settlementWhere)
	}
	if !reflect.DeepEqual(settlementArgs, []any{int64(7), int64(42), startTime, endTime}) {
		t.Fatalf("unexpected calendar settlement args: %#v", settlementArgs)
	}

	ledgerWhere, ledgerArgs := accountShareMySpendLedgerWhere(7, 42, 0, startTime, endTime)
	if strings.Contains(ledgerWhere, "membership_id") ||
		!strings.Contains(ledgerWhere, "ubl.created_at >= $6") ||
		!strings.Contains(ledgerWhere, "ubl.created_at < $7") {
		t.Fatalf("calendar ledger scope must keep the requested time range: %s", ledgerWhere)
	}
	if !reflect.DeepEqual(ledgerArgs, []any{
		int64(42),
		accountShareSeatPrepayReason,
		accountShareSeatRefundReason,
		accountShareSeatWaiverRefundReason,
		int64(7),
		startTime,
		endTime,
	}) {
		t.Fatalf("unexpected calendar ledger args: %#v", ledgerArgs)
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
	identityID := int64(86)
	lastRequestAt := time.Date(2026, 7, 11, 1, 5, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT\\s+l\\.id.*FOR UPDATE OF l$").
		WithArgs(membershipID, consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(listingID))
	mock.ExpectQuery("SELECT\\s+m\\.listing_id.*FOR UPDATE OF m$").
		WithArgs(membershipID, consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{
			"listing_id", "current_account_id", "account_identity_id", "listing_deleted_at", "owner_user_id", "last_request_at", "status",
		}).AddRow(
			listingID, accountID, identityID, nil, ownerUserID, lastRequestAt, service.AccountShareMembershipStatusActive,
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

func TestAccountShareModeRepositorySubmitReviewAllowsDeletedUsedMembership(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	membershipID := int64(181)
	listingID := int64(182)
	accountID := int64(183)
	identityID := int64(184)
	ownerUserID := int64(185)
	consumerUserID := int64(186)
	reviewID := int64(187)
	lastRequestAt := time.Date(2026, 7, 11, 1, 5, 0, 0, time.UTC)
	deletedAt := lastRequestAt.Add(time.Hour)
	createdAt := deletedAt.Add(time.Minute)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT\\s+l\\.id.*FOR UPDATE OF l$").
		WithArgs(membershipID, consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(listingID))
	mock.ExpectQuery("SELECT\\s+m\\.listing_id.*FOR UPDATE OF m$").
		WithArgs(membershipID, consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{
			"listing_id", "current_account_id", "account_identity_id", "listing_deleted_at", "owner_user_id", "last_request_at", "status",
		}).AddRow(
			listingID,
			accountID,
			identityID,
			deletedAt,
			ownerUserID,
			lastRequestAt,
			service.AccountShareMembershipStatusEnded,
		))
	mock.ExpectQuery("INSERT INTO account_share_reviews").
		WithArgs(
			identityID,
			listingID,
			accountID,
			membershipID,
			ownerUserID,
			consumerUserID,
			9,
			"",
			service.AccountShareReviewCommentStatusNone,
			nil,
			nil,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(reviewID))
	mock.ExpectExec("UPDATE account_share_listings l").
		WithArgs(listingID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT\\s+r\\.id,\\s+COALESCE\\(r\\.account_identity_id, 0\\)").
		WithArgs(reviewID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"account_identity_id",
			"listing_id",
			"account_id",
			"membership_id",
			"owner_user_id",
			"owner_username",
			"consumer_user_id",
			"consumer_username",
			"account_name",
			"platform",
			"score",
			"comment",
			"comment_status",
			"comment_reject_reason",
			"created_at",
			"updated_at",
		}).AddRow(
			reviewID,
			identityID,
			listingID,
			accountID,
			membershipID,
			ownerUserID,
			"owner-snapshot",
			consumerUserID,
			"consumer",
			"account-snapshot",
			service.PlatformOpenAI,
			9,
			"",
			service.AccountShareReviewCommentStatusNone,
			"",
			createdAt,
			createdAt,
		))
	mock.ExpectCommit()

	review, err := repo.SubmitReview(
		context.Background(),
		consumerUserID,
		membershipID,
		service.SubmitAccountShareReviewInput{Score: 9},
	)
	if err != nil {
		t.Fatalf("SubmitReview failed: %v", err)
	}
	if review == nil ||
		review.ID != reviewID ||
		review.MembershipID != membershipID ||
		review.AccountName != "account-snapshot" ||
		review.OwnerUsername != "owner-snapshot" {
		t.Fatalf("unexpected deleted-room review: %#v", review)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositorySubmitReviewSubjectWriteRollout(t *testing.T) {
	insertErr := errors.New("stop after review insert")
	lastRequestAt := time.Date(2026, 7, 11, 2, 5, 0, 0, time.UTC)

	tests := []struct {
		name                     string
		roomSubjectWritesEnabled bool
		legacyIdentityID         any
		listingDeletedAt         any
		resolveMissingIdentity   bool
		resolvedIdentityID       int64
		expectedInsertIdentity   any
		expectedErr              error
		expectInsert             bool
	}{
		{
			name:                   "default writes legacy identity",
			legacyIdentityID:       int64(304),
			expectedInsertIdentity: int64(304),
			expectedErr:            insertErr,
			expectInsert:           true,
		},
		{
			name:                     "enabled writes room subject without identity",
			roomSubjectWritesEnabled: true,
			legacyIdentityID:         int64(304),
			expectedInsertIdentity:   nil,
			expectedErr:              insertErr,
			expectInsert:             true,
		},
		{
			name:                   "default resolves and backfills missing legacy identity",
			legacyIdentityID:       nil,
			resolveMissingIdentity: true,
			resolvedIdentityID:     int64(307),
			expectedInsertIdentity: int64(307),
			expectedErr:            insertErr,
			expectInsert:           true,
		},
		{
			name:             "default rejects deleted room when legacy identity is missing",
			legacyIdentityID: nil,
			listingDeletedAt: lastRequestAt.Add(time.Hour),
			expectedErr:      service.ErrAccountShareReviewIdentityMissing,
		},
		{
			name:                     "enabled writes room subject for deleted room without identity",
			roomSubjectWritesEnabled: true,
			legacyIdentityID:         nil,
			listingDeletedAt:         lastRequestAt.Add(time.Hour),
			expectedInsertIdentity:   nil,
			expectedErr:              insertErr,
			expectInsert:             true,
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
			repo.rollout.ReviewRoomSubjectWritesEnabled = tt.roomSubjectWritesEnabled
			membershipID := int64(301)
			listingID := int64(302)
			accountID := int64(303)
			ownerUserID := int64(305)
			consumerUserID := int64(306)

			mock.ExpectBegin()
			mock.ExpectQuery("SELECT\\s+l\\.id.*FOR UPDATE OF l$").
				WithArgs(membershipID, consumerUserID).
				WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(listingID))
			mock.ExpectQuery("SELECT\\s+m\\.listing_id.*FOR UPDATE OF m$").
				WithArgs(membershipID, consumerUserID).
				WillReturnRows(sqlmock.NewRows([]string{
					"listing_id",
					"current_account_id",
					"account_identity_id",
					"listing_deleted_at",
					"owner_user_id",
					"last_request_at",
					"status",
				}).AddRow(
					listingID,
					accountID,
					tt.legacyIdentityID,
					tt.listingDeletedAt,
					ownerUserID,
					lastRequestAt,
					service.AccountShareMembershipStatusEnded,
				))
			if tt.resolveMissingIdentity {
				mock.ExpectQuery("SELECT\\s+COALESCE\\(name, ''\\)").
					WithArgs(accountID).
					WillReturnRows(sqlmock.NewRows([]string{
						"name",
						"platform",
						"credentials",
						"extra",
					}).AddRow(
						"legacy-account",
						service.PlatformOpenAI,
						[]byte(`{"email":"legacy@example.com"}`),
						[]byte(`{}`),
					))
				mock.ExpectQuery("INSERT INTO account_share_account_identities").
					WithArgs(
						service.PlatformOpenAI,
						"legacy@example.com",
						"l***y@example.com",
						accountID,
					).
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(tt.resolvedIdentityID))
				mock.ExpectExec("UPDATE account_share_listings").
					WithArgs(tt.resolvedIdentityID, listingID).
					WillReturnResult(sqlmock.NewResult(0, 1))
			}
			if tt.expectInsert {
				mock.ExpectQuery("INSERT INTO account_share_reviews").
					WithArgs(
						tt.expectedInsertIdentity,
						listingID,
						accountID,
						membershipID,
						ownerUserID,
						consumerUserID,
						7,
						"",
						service.AccountShareReviewCommentStatusNone,
						nil,
						nil,
					).
					WillReturnError(insertErr)
			}
			mock.ExpectRollback()

			_, err = repo.SubmitReview(
				context.Background(),
				consumerUserID,
				membershipID,
				service.SubmitAccountShareReviewInput{Score: 7},
			)
			if !errors.Is(err, tt.expectedErr) {
				t.Fatalf("SubmitReview error = %v, want %v", err, tt.expectedErr)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet expectations: %v", err)
			}
		})
	}
}

func TestAccountShareModeRepositorySubmitReviewDeletedRoomKeepsSelfReviewGuard(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	membershipID := int64(191)
	listingID := int64(192)
	accountID := int64(193)
	identityID := int64(194)
	consumerUserID := int64(195)
	lastRequestAt := time.Date(2026, 7, 11, 1, 5, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT\\s+l\\.id.*FOR UPDATE OF l$").
		WithArgs(membershipID, consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(listingID))
	mock.ExpectQuery("SELECT\\s+m\\.listing_id.*FOR UPDATE OF m$").
		WithArgs(membershipID, consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{
			"listing_id", "current_account_id", "account_identity_id", "listing_deleted_at", "owner_user_id", "last_request_at", "status",
		}).AddRow(
			listingID,
			accountID,
			identityID,
			lastRequestAt.Add(time.Hour),
			consumerUserID,
			lastRequestAt,
			service.AccountShareMembershipStatusEnded,
		))
	mock.ExpectRollback()

	_, err = repo.SubmitReview(
		context.Background(),
		consumerUserID,
		membershipID,
		service.SubmitAccountShareReviewInput{Score: 8},
	)
	if !errors.Is(err, service.ErrAccountShareReviewSelfUse) {
		t.Fatalf("SubmitReview error = %v, want self-review rejection", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositorySubmitReviewDeletedRoomKeepsDuplicateGuard(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	membershipID := int64(201)
	listingID := int64(202)
	accountID := int64(203)
	identityID := int64(204)
	ownerUserID := int64(205)
	consumerUserID := int64(206)
	lastRequestAt := time.Date(2026, 7, 11, 1, 5, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT\\s+l\\.id.*FOR UPDATE OF l$").
		WithArgs(membershipID, consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(listingID))
	mock.ExpectQuery("SELECT\\s+m\\.listing_id.*FOR UPDATE OF m$").
		WithArgs(membershipID, consumerUserID).
		WillReturnRows(sqlmock.NewRows([]string{
			"listing_id", "current_account_id", "account_identity_id", "listing_deleted_at", "owner_user_id", "last_request_at", "status",
		}).AddRow(
			listingID,
			accountID,
			identityID,
			lastRequestAt.Add(time.Hour),
			ownerUserID,
			lastRequestAt,
			service.AccountShareMembershipStatusEnded,
		))
	mock.ExpectQuery("INSERT INTO account_share_reviews").
		WithArgs(
			identityID,
			listingID,
			accountID,
			membershipID,
			ownerUserID,
			consumerUserID,
			8,
			"",
			service.AccountShareReviewCommentStatusNone,
			nil,
			nil,
		).
		WillReturnError(&pq.Error{
			Code:       "23505",
			Constraint: "uq_account_share_reviews_membership_live",
		})
	mock.ExpectRollback()

	_, err = repo.SubmitReview(
		context.Background(),
		consumerUserID,
		membershipID,
		service.SubmitAccountShareReviewInput{Score: 8},
	)
	if !errors.Is(err, service.ErrAccountShareReviewAlreadyExists) {
		t.Fatalf("SubmitReview error = %v, want duplicate-review rejection", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareReviewSelectSQLUsesImmutableHistorySnapshots(t *testing.T) {
	normalized := strings.ToLower(strings.Join(strings.Fields(accountShareReviewSelectSQL()), " "))
	for _, required := range []string{
		"left join account_share_memberships history_membership",
		"left join account_share_listing_revisions history_revision",
		"from account_share_membership_account_bindings binding",
		"history_membership.owner_username_snapshot",
		"history_binding.account_name_snapshot",
		"history_membership.platform_snapshot",
	} {
		if !strings.Contains(normalized, required) {
			t.Fatalf("review projection must contain %q: %s", required, normalized)
		}
	}
	for _, forbidden := range []string{
		"left join accounts",
		"left join users ou",
		"credentials",
		"proxy",
		"health_state",
		"in_flight",
	} {
		if strings.Contains(normalized, forbidden) {
			t.Fatalf("review history projection must not read %q: %s", forbidden, normalized)
		}
	}
}

func TestAccountShareModeRepositoryListDeletedListingReviewsAccessBoundary(t *testing.T) {
	queryMatcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if expectedSQL != "listing review access" {
			return nil
		}
		normalized := strings.ToLower(strings.Join(strings.Fields(actualSQL), " "))
		for _, required := range []string{
			"left join account_share_reviews r on r.listing_id = l.id",
			"(l.deleted_at is null and l.status = 'active') or $3::boolean",
			"or l.owner_user_id = $4",
			"from account_share_memberships viewer_membership",
			"viewer_membership.consumer_user_id = $4",
			"from account_share_membership_account_bindings viewer_binding",
			"viewer_binding.membership_id = viewer_membership.id",
			"viewer_binding.listing_id = viewer_membership.listing_id",
		} {
			if !strings.Contains(normalized, required) {
				return fmt.Errorf("deleted listing review access query missing %q", required)
			}
		}
		if strings.Contains(normalized, "r.account_identity_id = l.account_identity_id") {
			return errors.New("listing reviews must not aggregate by account identity")
		}
		return nil
	})

	for _, tt := range []struct {
		name          string
		viewerUserID  int64
		viewerIsAdmin bool
		allowed       bool
	}{
		{name: "owner", viewerUserID: 301, allowed: true},
		{name: "bound history consumer", viewerUserID: 302, allowed: true},
		{name: "admin", viewerUserID: 303, viewerIsAdmin: true, allowed: true},
		{name: "queued-only consumer", viewerUserID: 305, allowed: false},
		{name: "unrelated user", viewerUserID: 304, allowed: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(queryMatcher))
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			defer func() { _ = db.Close() }()
			repo := &accountShareModeRepository{db: db}
			expectation := mock.ExpectQuery("listing review access").
				WithArgs(
					int64(300),
					service.AccountShareReviewCommentStatusApproved,
					tt.viewerIsAdmin,
					tt.viewerUserID,
				)
			if tt.allowed {
				expectation.WillReturnRows(
					sqlmock.NewRows([]string{"listing_id", "review_count"}).
						AddRow(int64(300), int64(0)),
				)
			} else {
				expectation.WillReturnRows(
					sqlmock.NewRows([]string{"listing_id", "review_count"}),
				)
			}

			reviews, result, err := repo.ListListingReviews(
				context.Background(),
				tt.viewerUserID,
				tt.viewerIsAdmin,
				300,
				pagination.PaginationParams{Page: 1, PageSize: 20},
			)
			if !tt.allowed {
				if !errors.Is(err, service.ErrAccountShareListingNotFound) {
					t.Fatalf("ListListingReviews error = %v, want not found", err)
				}
			} else {
				if err != nil {
					t.Fatalf("ListListingReviews failed: %v", err)
				}
				if len(reviews) != 0 || result == nil || result.Total != 0 {
					t.Fatalf("unexpected empty review result: reviews=%#v result=%#v", reviews, result)
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet expectations: %v", err)
			}
		})
	}
}

func TestAccountShareModeRepositoryReviewDetailAuthorizationRequiresHistoricalBinding(t *testing.T) {
	queryMatcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if expectedSQL != "review detail authorization" {
			return nil
		}
		normalized := strings.ToLower(strings.Join(strings.Fields(actualSQL), " "))
		for _, required := range []string{
			"listing.owner_user_id = $3",
			"from account_share_memberships viewer_membership",
			"viewer_membership.consumer_user_id = $3",
			"from account_share_membership_account_bindings viewer_binding",
			"viewer_binding.membership_id = viewer_membership.id",
			"viewer_binding.listing_id = viewer_membership.listing_id",
		} {
			if !strings.Contains(normalized, required) {
				return fmt.Errorf("review detail authorization query missing %q", required)
			}
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(queryMatcher))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	mock.ExpectQuery("review detail authorization").
		WithArgs(int64(300), false, int64(305)).
		WillReturnRows(sqlmock.NewRows([]string{"allowed"}).AddRow(false))

	allowed, err := repo.CanViewListingReviewDetails(context.Background(), 305, false, 300)
	if err != nil {
		t.Fatalf("CanViewListingReviewDetails failed: %v", err)
	}
	if allowed {
		t.Fatal("queued-only viewer without a historical binding must not receive full review DTOs")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryListListingReviewsDetailsStayWithinListing(t *testing.T) {
	queryMatcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if expectedSQL != "listing review detail" {
			return nil
		}
		normalized := strings.ToLower(strings.Join(strings.Fields(actualSQL), " "))
		if !strings.Contains(normalized, "where r.listing_id = $1") {
			return errors.New("listing review detail query must filter by listing_id")
		}
		if strings.Contains(normalized, "where r.account_identity_id = $1") {
			return errors.New("listing review detail query must not aggregate sibling rooms by account identity")
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(queryMatcher))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}
	listingID := int64(300)
	viewerUserID := int64(301)

	mock.ExpectQuery("listing review access").
		WithArgs(
			listingID,
			service.AccountShareReviewCommentStatusApproved,
			false,
			viewerUserID,
		).
		WillReturnRows(
			sqlmock.NewRows([]string{"listing_id", "review_count"}).
				AddRow(listingID, int64(1)),
		)
	mock.ExpectQuery("listing review detail").
		WithArgs(
			listingID,
			service.AccountShareReviewCommentStatusApproved,
			20,
			0,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	reviews, result, err := repo.ListListingReviews(
		context.Background(),
		viewerUserID,
		false,
		listingID,
		pagination.PaginationParams{Page: 1, PageSize: 20},
	)

	if err != nil {
		t.Fatalf("ListListingReviews failed: %v", err)
	}
	if len(reviews) != 0 || result == nil || result.Total != 1 {
		t.Fatalf("unexpected listing-scoped result: reviews=%#v result=%#v", reviews, result)
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
		if strings.Contains(normalized, "moderation_attempts = r_claim.moderation_attempts + 1") {
			return errors.New("claiming a review must not consume a moderation attempt")
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

func TestAccountShareModeRepositoryBeginsModerationAttemptAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	mock.ExpectExec("UPDATE account_share_reviews").
		WithArgs(
			int64(91),
			service.AccountShareReviewCommentStatusPending,
			service.AccountShareReviewCommentStatusFailed,
			service.AccountShareReviewModerationMaxAttempts,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	begun, err := repo.BeginReviewModerationAttempt(
		context.Background(),
		91,
		service.AccountShareReviewModerationMaxAttempts,
	)
	if err != nil {
		t.Fatalf("BeginReviewModerationAttempt failed: %v", err)
	}
	if !begun {
		t.Fatal("expected moderation attempt to begin")
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
	mock.ExpectQuery("SELECT NOT EXISTS").
		WithArgs(listingID, accountID, now).
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
	mock.ExpectExec("UPDATE account_share_membership_account_bindings").
		WithArgs(now, nil, "system", "membership_requeued", membershipID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("(?s)UPDATE account_share_memberships m.*dispatch_failed_at = \\$3::timestamptz.*queue_expires_at = \\$3::timestamptz \\+ INTERVAL '2 hours'").
		WithArgs(service.AccountShareMembershipStatusQueued, now, now, now, membershipID, service.AccountShareMembershipStatusActive, true).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			membershipID, listingID, nil, ownerUserID, consumerUserID, apiKeyID,
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
	if membership.AccountID != 0 {
		t.Fatalf("requeued membership account id = %d, want no pre-bound account", membership.AccountID)
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
				!strings.Contains(normalized, "m_available.status in ('active', 'ending')") ||
				!strings.Contains(normalized, "for update of m") ||
				strings.Contains(normalized, "for update of m, l") ||
				strings.Contains(normalized, "l.hourly_rate") ||
				strings.Contains(normalized, "l.hourly_fee_waiver_minimum") ||
				strings.Contains(normalized, "l.min_balance_required") {
				return errors.New("activation must lock only the membership and must not read mutable listing billing terms")
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
	expectEndStaleQueuedMembershipsForAPIKey(mock, userID, apiKeyID, 0)
	mock.ExpectQuery("lock queued listing candidates").
		WithArgs(userID, apiKeyID, service.AccountShareMembershipStatusQueued, groupID, now, service.AccountShareModeQueueMaxItems).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(501)).AddRow(int64(502)))
	mock.ExpectQuery("activate queued membership").
		WithArgs(userID, apiKeyID, service.AccountShareMembershipStatusQueued, groupID, now, 0, "{501,502}").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "listing_id", "account_id", "owner_user_id", "listing_revision_id", "queue_rank", "idle_timeout_minutes",
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

func TestAccountShareModeRepositoryAvailableAndQueuedCapacityCountEndingSeats(t *testing.T) {
	tests := []struct {
		name string
		sql  string
	}{
		{
			name: "available listing",
			sql:  accountShareListingAvailableConditionSQL("NOW()"),
		},
		{
			name: "queued activation",
			sql:  accountShareQueuedActivationConditionSQL("$5", "$1"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized := strings.ToLower(strings.Join(strings.Fields(tt.sql), " "))
			if !strings.Contains(normalized, "m_available.status in ('active', 'ending')") {
				t.Fatalf("seat capacity must count both active and ending memberships:\n%s", tt.sql)
			}
			if !strings.Contains(normalized, "m_available.consumer_user_id <> l.owner_user_id") {
				t.Fatalf("owner self-use must remain excluded from consumer seat capacity:\n%s", tt.sql)
			}
		})
	}
}

func TestAccountShareModeRepositoryActivatesQueuedMembershipWithNewBindingGeneration(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	now := time.Date(2026, 7, 11, 1, 5, 0, 0, time.UTC)
	paidUntil := now.Add(service.AccountShareModeSeatPrepayDuration)
	userID := int64(101)
	apiKeyID := int64(202)
	groupID := int64(303)
	listingID := int64(501)
	accountID := int64(601)
	ownerUserID := int64(701)
	membershipID := int64(801)
	revisionID := int64(901)
	revisionNumber := int64(4)
	queueRank := 2
	idleTimeoutMinutes := 10
	termsRateMultiplier := 0.35
	expectedPrepayRefID := accountShareSeatPrepayRefID(membershipID, paidUntil)

	mock.ExpectBegin()
	expectEndStaleQueuedMembershipsForAPIKey(mock, userID, apiKeyID, 0)
	mock.ExpectQuery("SELECT\\s+l\\.id").
		WithArgs(userID, apiKeyID, service.AccountShareMembershipStatusQueued, groupID, now, service.AccountShareModeQueueMaxItems).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(listingID))
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id, a\\.id").
		WithArgs(userID, apiKeyID, service.AccountShareMembershipStatusQueued, groupID, now, 0, "{501}").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "listing_id", "account_id", "owner_user_id", "listing_revision_id",
			"queue_rank", "idle_timeout_minutes",
		}).AddRow(
			membershipID, listingID, accountID, ownerUserID, revisionID,
			queueRank, idleTimeoutMinutes,
		))
	expectAccountShareMembershipRuntimeSnapshot(
		mock,
		membershipID,
		revisionID,
		revisionNumber,
		accountShareRuntimeTermsJSON(revisionID, revisionNumber, termsRateMultiplier),
	)
	expectAccountShareMembershipTermsRevision(
		mock,
		listingID,
		revisionID,
		revisionNumber,
		termsRateMultiplier,
	)
	mock.ExpectQuery("SELECT balance").
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(10.0))
	mock.ExpectQuery("UPDATE account_share_memberships m.*m\\.status = \\$10.*m\\.deleted_at IS NULL.*l\\.status = \\$11.*l\\.owner_user_id = m\\.consumer_user_id.*m_occupied\\.status IN \\(\\$12, \\$13\\)").
		WithArgs(
			service.AccountShareMembershipStatusActive,
			accountID,
			0.6,
			0.1,
			idleTimeoutMinutes,
			now,
			paidUntil,
			now,
			membershipID,
			service.AccountShareMembershipStatusQueued,
			service.AccountShareListingStatusActive,
			service.AccountShareMembershipStatusActive,
			service.AccountShareMembershipStatusEnding,
		).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			membershipID, listingID, accountID, ownerUserID, userID, apiKeyID,
			service.AccountShareMembershipStatusActive, queueRank, 0.6, 0.1, idleTimeoutMinutes,
			now, nil, nil, nil, paidUntil, now, now, 0, int64(0), nil,
			nil, nil, now.Add(-time.Hour), now,
		))
	expectAccountShareMembershipBinding(
		mock,
		membershipID,
		listingID,
		accountID,
		revisionID,
		userID,
		"consumer",
		"queue_activation",
		2,
	)
	expectAccountShareMembershipRuntimeSnapshot(
		mock,
		membershipID,
		revisionID,
		revisionNumber,
		accountShareRuntimeTermsJSON(revisionID, revisionNumber, termsRateMultiplier),
	)
	expectAccountShareMembershipTermsRevision(
		mock,
		listingID,
		revisionID,
		revisionNumber,
		termsRateMultiplier,
	)
	expectAccountShareMembershipRuntimeBinding(
		mock,
		membershipID,
		listingID,
		accountID,
		revisionID,
		revisionNumber,
	)
	mock.ExpectExec("UPDATE users").
		WithArgs("9.9900000000", userID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO user_balance_ledger").
		WithArgs(
			userID,
			"debit",
			"0.0100000000",
			accountShareSeatPrepayReason,
			accountShareSeatPrepayRefType,
			expectedPrepayRefID,
			"9.9900000000",
			sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery("SELECT\\s+l\\.id").
		WithArgs(userID, listingID, accountID).
		WillReturnRows(accountShareListingRows(
			listingID,
			accountID,
			ownerUserID,
			"",
			time.Time{},
			func(row *accountShareListingRowData) {
				row.RateMultiplier = 0.9
			},
		))

	membership, listing, err := repo.ActivateNextQueuedMembershipForRequest(
		context.Background(),
		userID,
		apiKeyID,
		groupID,
		0,
		now,
	)
	if err != nil {
		t.Fatalf("ActivateNextQueuedMembershipForRequest failed: %v", err)
	}
	if membership == nil || membership.Status != service.AccountShareMembershipStatusActive || membership.AccountID != accountID {
		t.Fatalf("unexpected activated membership: %#v", membership)
	}
	if membership.TermsSnapshot == nil ||
		membership.TermsSnapshot.ListingRevisionID != revisionID ||
		membership.TermsSnapshot.RowVersion != revisionNumber ||
		membership.TermsSnapshot.RateMultiplier != termsRateMultiplier {
		t.Fatalf("activated membership runtime terms snapshot = %+v", membership.TermsSnapshot)
	}
	if listing == nil ||
		listing.ID != listingID ||
		listing.RateMultiplier != termsRateMultiplier ||
		listing.HourlyRate != 0.6 ||
		listing.HourlyFeeWaiverMinimum != 0.1 ||
		listing.MinBalanceRequired != 1 ||
		listing.PerUserConcurrency != 5 ||
		len(listing.AllowedModels) != 1 ||
		listing.AllowedModels[0] != "gpt-5.5" {
		t.Fatalf("unexpected activated listing: %#v", listing)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryQueuedActivationFinalSeatGuardRollsBack(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	now := time.Date(2026, 7, 11, 1, 5, 30, 0, time.UTC)
	paidUntil := now.Add(service.AccountShareModeSeatPrepayDuration)
	userID := int64(101)
	apiKeyID := int64(202)
	groupID := int64(303)
	listingID := int64(501)
	accountID := int64(601)
	ownerUserID := int64(701)
	membershipID := int64(801)
	revisionID := int64(901)
	revisionNumber := int64(4)
	queueRank := 2
	idleTimeoutMinutes := 10
	termsRateMultiplier := 0.35

	mock.ExpectBegin()
	expectEndStaleQueuedMembershipsForAPIKey(mock, userID, apiKeyID, 0)
	mock.ExpectQuery("SELECT\\s+l\\.id").
		WithArgs(
			userID,
			apiKeyID,
			service.AccountShareMembershipStatusQueued,
			groupID,
			now,
			service.AccountShareModeQueueMaxItems,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(listingID))
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id, a\\.id").
		WithArgs(
			userID,
			apiKeyID,
			service.AccountShareMembershipStatusQueued,
			groupID,
			now,
			0,
			"{501}",
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"listing_id",
			"account_id",
			"owner_user_id",
			"listing_revision_id",
			"queue_rank",
			"idle_timeout_minutes",
		}).AddRow(
			membershipID,
			listingID,
			accountID,
			ownerUserID,
			revisionID,
			queueRank,
			idleTimeoutMinutes,
		))
	expectAccountShareMembershipRuntimeSnapshot(
		mock,
		membershipID,
		revisionID,
		revisionNumber,
		accountShareRuntimeTermsJSON(revisionID, revisionNumber, termsRateMultiplier),
	)
	expectAccountShareMembershipTermsRevision(
		mock,
		listingID,
		revisionID,
		revisionNumber,
		termsRateMultiplier,
	)
	mock.ExpectQuery("SELECT balance").
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(10.0))
	mock.ExpectQuery("UPDATE account_share_memberships m.*m\\.status = \\$10.*m\\.deleted_at IS NULL.*l\\.status = \\$11.*m_occupied\\.status IN \\(\\$12, \\$13\\)").
		WithArgs(
			service.AccountShareMembershipStatusActive,
			accountID,
			0.6,
			0.1,
			idleTimeoutMinutes,
			now,
			paidUntil,
			now,
			membershipID,
			service.AccountShareMembershipStatusQueued,
			service.AccountShareListingStatusActive,
			service.AccountShareMembershipStatusActive,
			service.AccountShareMembershipStatusEnding,
		).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()))
	mock.ExpectRollback()

	membership, listing, err := repo.ActivateNextQueuedMembershipForRequest(
		context.Background(),
		userID,
		apiKeyID,
		groupID,
		0,
		now,
	)
	if !errors.Is(err, service.ErrAccountShareListingNotFound) {
		t.Fatalf("expected final seat guard rejection, got membership=%#v listing=%#v err=%v", membership, listing, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryQueuedActivationRejectsMissingOrMalformedImmutableTerms(t *testing.T) {
	tests := []struct {
		name          string
		termsSnapshot any
	}{
		{
			name:          "missing terms",
			termsSnapshot: nil,
		},
		{
			name:          "malformed terms",
			termsSnapshot: []byte(`{"listing_revision_id":`),
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

			now := time.Date(2026, 7, 11, 1, 6, 0, 0, time.UTC)
			userID := int64(101)
			apiKeyID := int64(202)
			groupID := int64(303)
			listingID := int64(501)
			accountID := int64(601)
			ownerUserID := int64(701)
			membershipID := int64(801)
			revisionID := int64(901)
			revisionNumber := int64(4)

			mock.ExpectBegin()
			expectEndStaleQueuedMembershipsForAPIKey(mock, userID, apiKeyID, 0)
			mock.ExpectQuery("SELECT\\s+l\\.id").
				WithArgs(
					userID,
					apiKeyID,
					service.AccountShareMembershipStatusQueued,
					groupID,
					now,
					service.AccountShareModeQueueMaxItems,
				).
				WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(listingID))
			mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id, a\\.id").
				WithArgs(
					userID,
					apiKeyID,
					service.AccountShareMembershipStatusQueued,
					groupID,
					now,
					0,
					"{501}",
				).
				WillReturnRows(sqlmock.NewRows([]string{
					"id",
					"listing_id",
					"account_id",
					"owner_user_id",
					"listing_revision_id",
					"queue_rank",
					"idle_timeout_minutes",
				}).AddRow(
					membershipID,
					listingID,
					accountID,
					ownerUserID,
					revisionID,
					2,
					10,
				))
			expectAccountShareMembershipRuntimeSnapshot(
				mock,
				membershipID,
				revisionID,
				revisionNumber,
				tt.termsSnapshot,
			)
			mock.ExpectRollback()

			_, _, err = repo.ActivateNextQueuedMembershipForRequest(
				context.Background(),
				userID,
				apiKeyID,
				groupID,
				0,
				now,
			)
			if !errors.Is(err, service.ErrAccountShareBillingBindingUnavailable) {
				t.Fatalf("expected immutable terms rejection, got %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet expectations: %v", err)
			}
		})
	}
}

func int64sToStrings(values []int64) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strconv.FormatInt(value, 10))
	}
	return out
}

func expectEndStaleQueuedMembershipsForAPIKey(
	mock sqlmock.Sqlmock,
	consumerUserID int64,
	apiKeyID int64,
	affected int64,
) {
	mock.ExpectExec("UPDATE account_share_memberships m").
		WithArgs(
			service.AccountShareMembershipStatusEnded,
			sqlmock.AnyArg(),
			service.AccountShareMembershipEndReasonQueueExpired,
			service.AccountShareMembershipEndReasonUnavailable,
			consumerUserID,
			apiKeyID,
			service.AccountShareMembershipStatusQueued,
			service.AccountShareListingStatusDisabled,
			service.AccountShareListingStatusSuspended,
			true,
		).
		WillReturnResult(sqlmock.NewResult(0, affected))
}

func expectEndStaleQueuedMembershipsForConsumer(
	mock sqlmock.Sqlmock,
	consumerUserID int64,
	affected int64,
) {
	mock.ExpectExec("UPDATE account_share_memberships m").
		WithArgs(
			service.AccountShareMembershipStatusEnded,
			sqlmock.AnyArg(),
			service.AccountShareMembershipEndReasonQueueExpired,
			service.AccountShareMembershipEndReasonUnavailable,
			consumerUserID,
			nil,
			service.AccountShareMembershipStatusQueued,
			service.AccountShareListingStatusDisabled,
			service.AccountShareListingStatusSuspended,
			true,
		).
		WillReturnResult(sqlmock.NewResult(0, affected))
}

func expectAccountShareJoinQueueState(
	mock sqlmock.Sqlmock,
	consumerUserID int64,
	apiKeyID int64,
	listingID int64,
	apiKeyQueueCount int,
	maxQueueRank int,
	hasLiveMembership bool,
	consumerQueueCount int,
	roomQueueCount int,
) {
	mock.ExpectQuery("(?s)SELECT\\s+\\(\\s*SELECT COUNT\\(\\*\\)::int.*?SELECT MAX\\(queue_rank\\)\\s+FROM account_share_memberships\\s+WHERE api_key_id = \\$2\\s+AND status IN \\(\\$3, \\$4\\)\\s+AND deleted_at IS NULL\\s+\\), 0").
		WithArgs(
			consumerUserID,
			apiKeyID,
			service.AccountShareMembershipStatusQueued,
			service.AccountShareMembershipStatusActive,
			service.AccountShareMembershipStatusEnding,
			sqlmock.AnyArg(),
			listingID,
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"api_key_queue_count",
			"max_queue_rank",
			"has_live_membership",
			"consumer_queue_count",
			"room_queue_count",
		}).AddRow(
			apiKeyQueueCount,
			maxQueueRank,
			hasLiveMembership,
			consumerQueueCount,
			roomQueueCount,
		))
}

func expectAccountShareMembershipBinding(
	mock sqlmock.Sqlmock,
	membershipID int64,
	listingID int64,
	accountID int64,
	listingRevisionID int64,
	boundByUserID int64,
	boundByRole string,
	bindReason string,
	routingGeneration int64,
) {
	accountIDs := []int64{accountID}
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
		WithArgs(pq.Array(accountIDs)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "listing_id", "account_id_snapshot"}).
			AddRow(accountID+100000, listingID, accountID))
	mock.ExpectQuery("WITH binding_source AS MATERIALIZED").
		WithArgs(
			membershipID,
			listingID,
			accountID,
			listingRevisionID,
			sqlmock.AnyArg(),
			boundByUserID,
			boundByRole,
			bindReason,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "routing_generation"}).
			AddRow(membershipID+100000, routingGeneration))
}

func accountShareRuntimeTermsJSON(listingRevisionID, rowVersion int64, rateMultiplier float64) []byte {
	return []byte(fmt.Sprintf(
		`{"listing_revision_id":%d,"row_version":%d,"schema_version":1,"room_name":"immutable-room","status":"active","seat_limit":4,"rate_multiplier":%.8f,"allowed_models":["gpt-5.5"],"per_user_concurrency":5,"hourly_rate":0.6,"hourly_fee_waiver_minimum":0.1,"min_balance_required":1,"codex_5h_limit_percent":91,"codex_7d_limit_percent":92}`,
		listingRevisionID,
		rowVersion,
		rateMultiplier,
	))
}

func expectAccountShareMembershipRuntimeSnapshot(
	mock sqlmock.Sqlmock,
	membershipID int64,
	listingRevisionID int64,
	listingVersion int64,
	termsSnapshot any,
) {
	mock.ExpectQuery("SELECT\\s+listing_revision_id, listing_version_snapshot, room_name_snapshot").
		WithArgs(membershipID).
		WillReturnRows(sqlmock.NewRows([]string{
			"listing_revision_id",
			"listing_version_snapshot",
			"room_name_snapshot",
			"owner_user_id_snapshot",
			"owner_username_snapshot",
			"platform_snapshot",
			"account_level_snapshot",
			"api_key_name_snapshot",
			"terms_snapshot",
			"snapshot_quality",
			"ending_requested_at",
			"ending_reason",
			"settlement_status",
		}).AddRow(
			listingRevisionID,
			listingVersion,
			"immutable-room",
			int64(701),
			"owner",
			service.PlatformOpenAI,
			"pro",
			"consumer-key",
			termsSnapshot,
			service.AccountShareSnapshotQualityExact,
			nil,
			nil,
			nil,
		))
}

func expectAccountShareMembershipTermsRevision(
	mock sqlmock.Sqlmock,
	listingID int64,
	listingRevisionID int64,
	revisionNumber int64,
	rateMultiplier float64,
) {
	mock.ExpectQuery("SELECT\\s+id, listing_id, revision_number, schema_version, snapshot_quality").
		WithArgs(listingRevisionID, listingID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"listing_id",
			"revision_number",
			"schema_version",
			"snapshot_quality",
			"room_name",
			"platform",
			"account_level",
			"owner_user_id",
			"owner_display_name_snapshot",
			"status",
			"seat_limit",
			"rate_multiplier",
			"allowed_models",
			"per_user_concurrency",
			"hourly_rate",
			"hourly_fee_waiver_minimum",
			"min_balance_required",
			"codex_cli_only",
			"codex_5h_limit_percent",
			"codex_7d_limit_percent",
		}).AddRow(
			listingRevisionID,
			listingID,
			revisionNumber,
			1,
			service.AccountShareSnapshotQualityExact,
			"immutable-room",
			service.PlatformOpenAI,
			"pro",
			int64(701),
			"owner",
			service.AccountShareListingStatusActive,
			4,
			rateMultiplier,
			[]byte(`["gpt-5.5"]`),
			5,
			0.6,
			0.1,
			1.0,
			false,
			91.0,
			92.0,
		))
}

func expectAccountShareMembershipRuntimeBinding(
	mock sqlmock.Sqlmock,
	membershipID int64,
	listingID int64,
	accountID int64,
	listingRevisionID int64,
	termsRevisionNumber int64,
) {
	mock.ExpectQuery("SELECT\\s+binding\\.listing_revision_id,\\s+binding\\.terms_revision_number").
		WithArgs(
			membershipID,
			listingID,
			accountID,
			listingRevisionID,
			service.AccountShareMembershipStatusActive,
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"listing_revision_id",
			"terms_revision_number",
		}).AddRow(listingRevisionID, termsRevisionNumber))
}

// Reordering must stage the final 1..N ranks through a temporary range that is
// disjoint from every live queue_rank, because uq_account_share_memberships_queue_rank
// is unique over (api_key_id, queue_rank) for live rows. The previous
// "100+index" offset collided once any live rank reached >=100 (ranks climb
// unbounded via MAX(queue_rank)+1 across join/leave churn). This test seeds
// live ranks at 100 and 101 — the exact case that tripped the unique index —
// and asserts the temp pass writes negative ranks before settling to 1..N.
func TestReorderMembershipQueueStagesThroughCollisionFreeRanks(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() { _ = db.Close() }()
	repo := &accountShareModeRepository{db: db}

	const (
		consumerUserID = int64(42)
		apiKeyID       = int64(7)
		ownerUserID    = int64(9)
		listingID      = int64(700)
		firstID        = int64(501)
		secondID       = int64(502)
	)
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	// Current live memberships, ordered by queue_rank. Ranks are high (100, 101)
	// to reproduce the collision the old offset scheme suffered from.
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id, m\\.account_id").
		WithArgs(
			consumerUserID,
			apiKeyID,
			service.AccountShareMembershipStatusActive,
			service.AccountShareMembershipStatusQueued,
		).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).
			AddRow(
				firstID, listingID, int64(0), ownerUserID, consumerUserID, apiKeyID,
				service.AccountShareMembershipStatusQueued, 100, "0", "0", 0,
				now, nil, nil, "", nil, nil,
				nil, "0", 0, nil,
				nil, nil, now, now,
			).
			AddRow(
				secondID, listingID, int64(0), ownerUserID, consumerUserID, apiKeyID,
				service.AccountShareMembershipStatusQueued, 101, "0", "0", 0,
				now, nil, nil, "", nil, nil,
				nil, "0", 0, nil,
				nil, nil, now, now,
			))

	// Requested order: put secondID first. Temp pass must use negative ranks.
	mock.ExpectExec("UPDATE account_share_memberships\\s+SET queue_rank = \\$1").
		WithArgs(-1, secondID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE account_share_memberships\\s+SET queue_rank = \\$1").
		WithArgs(-2, firstID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	// Final pass assigns 1..N.
	mock.ExpectExec("UPDATE account_share_memberships\\s+SET queue_rank = \\$1").
		WithArgs(1, secondID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE account_share_memberships\\s+SET queue_rank = \\$1").
		WithArgs(2, firstID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	out, err := repo.ReorderMembershipQueue(
		context.Background(),
		consumerUserID,
		apiKeyID,
		[]int64{secondID, firstID},
	)
	if err != nil {
		t.Fatalf("ReorderMembershipQueue: %v", err)
	}
	if len(out) != 2 || out[0].ID != secondID || out[1].ID != firstID {
		t.Fatalf("unexpected reorder result: %#v", out)
	}
	if out[0].QueueRank != 1 || out[1].QueueRank != 2 {
		t.Fatalf("unexpected final ranks: %d, %d", out[0].QueueRank, out[1].QueueRank)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
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

func expectAccountShareEndListingLock(
	mock sqlmock.Sqlmock,
	membershipID int64,
	consumerUserID int64,
	listingID int64,
	listingVersion int64,
) {
	listingQuery := mock.ExpectQuery("SELECT listing_id")
	if consumerUserID > 0 {
		listingQuery.WithArgs(membershipID, consumerUserID)
	} else {
		listingQuery.WithArgs(membershipID)
	}
	listingQuery.WillReturnRows(sqlmock.NewRows([]string{"listing_id"}).AddRow(listingID))
	mock.ExpectQuery("SELECT row_version").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{"row_version"}).AddRow(listingVersion))
}

func expectAccountShareEndState(
	mock sqlmock.Sqlmock,
	membershipID int64,
	endingRequestedAt any,
	endingReason any,
	settlementStatus any,
	operationID any,
) {
	mock.ExpectQuery("SELECT\\s+ending_requested_at").
		WithArgs(membershipID).
		WillReturnRows(sqlmock.NewRows([]string{
			"ending_requested_at",
			"ending_reason",
			"settlement_status",
			"ending_operation_id",
		}).AddRow(endingRequestedAt, endingReason, settlementStatus, operationID))
}

func accountShareEndMembershipRow(
	membershipID int64,
	listingID int64,
	accountID any,
	ownerUserID int64,
	consumerUserID int64,
	apiKeyID int64,
	status string,
	joinedAt time.Time,
	updatedAt time.Time,
) []driver.Value {
	return []driver.Value{
		membershipID,
		listingID,
		accountID,
		ownerUserID,
		consumerUserID,
		apiKeyID,
		status,
		0,
		0.0,
		0.0,
		10,
		joinedAt,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		0.0,
		int64(0),
		nil,
		nil,
		nil,
		joinedAt,
		updatedAt,
	}
}

func accountShareEndMembershipEndedRow(
	membershipID int64,
	listingID int64,
	accountID any,
	ownerUserID int64,
	consumerUserID int64,
	apiKeyID int64,
	joinedAt time.Time,
	updatedAt time.Time,
) []driver.Value {
	values := accountShareEndMembershipRow(
		membershipID,
		listingID,
		accountID,
		ownerUserID,
		consumerUserID,
		apiKeyID,
		service.AccountShareMembershipStatusEnded,
		joinedAt,
		updatedAt,
	)
	values[13] = updatedAt
	values[14] = service.AccountShareMembershipEndReasonManual
	values[15] = updatedAt
	values[16] = updatedAt
	return values
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

	mock.ExpectBegin()
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
	mock.ExpectRollback()

	_, _, err = repo.GetActiveMembershipForRequest(context.Background(), 20, 30, 50)
	if !errors.Is(err, service.ErrAccountShareListingNotFound) {
		t.Fatalf("expected not found from empty binding query, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryGetActiveMembershipLoadsImmutableRuntimeSnapshot(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	membershipID := int64(700)
	listingID := int64(70)
	accountID := int64(99)
	ownerUserID := int64(42)
	consumerUserID := int64(20)
	apiKeyID := int64(30)
	groupID := int64(50)
	revisionID := int64(7001)
	revisionNumber := int64(3)
	joinedAt := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)
	immutableRateMultiplier := 0.35
	currentListingRateMultiplier := 0.9

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id, m\\.account_id").
		WithArgs(consumerUserID, apiKeyID, groupID).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			accountShareEndMembershipRow(
				membershipID,
				listingID,
				accountID,
				ownerUserID,
				consumerUserID,
				apiKeyID,
				service.AccountShareMembershipStatusActive,
				joinedAt,
				joinedAt,
			)...,
		))
	expectAccountShareMembershipRuntimeSnapshot(
		mock,
		membershipID,
		revisionID,
		revisionNumber,
		accountShareRuntimeTermsJSON(revisionID, revisionNumber, immutableRateMultiplier),
	)
	expectAccountShareMembershipTermsRevision(
		mock,
		listingID,
		revisionID,
		revisionNumber,
		immutableRateMultiplier,
	)
	expectAccountShareMembershipRuntimeBinding(
		mock,
		membershipID,
		listingID,
		accountID,
		revisionID,
		revisionNumber,
	)
	mock.ExpectCommit()
	mock.ExpectQuery("SELECT\\s+l\\.id").
		WithArgs(consumerUserID, listingID, accountID).
		WillReturnRows(accountShareListingRows(
			listingID,
			accountID,
			ownerUserID,
			"",
			time.Time{},
			func(row *accountShareListingRowData) {
				row.RateMultiplier = currentListingRateMultiplier
			},
		))

	membership, listing, err := repo.GetActiveMembershipForRequest(
		context.Background(),
		consumerUserID,
		apiKeyID,
		groupID,
	)
	if err != nil {
		t.Fatalf("GetActiveMembershipForRequest: %v", err)
	}
	if membership == nil || membership.ListingRevisionID == nil ||
		*membership.ListingRevisionID != revisionID ||
		membership.TermsSnapshot == nil ||
		membership.TermsSnapshot.RateMultiplier != immutableRateMultiplier {
		t.Fatalf("active membership immutable snapshot = %+v", membership)
	}
	if listing == nil ||
		listing.RateMultiplier != immutableRateMultiplier ||
		listing.HourlyRate != 0.6 ||
		listing.MinBalanceRequired != 1 ||
		listing.Codex5hLimitPercent != 91 ||
		listing.Codex7dLimitPercent != 92 {
		t.Fatalf("runtime listing did not apply immutable membership terms: %+v", listing)
	}
	if listing.RateMultiplier == currentListingRateMultiplier {
		t.Fatalf(
			"runtime listing leaked current mutable terms: membership=%v listing=%v",
			membership.TermsSnapshot.RateMultiplier,
			listing.RateMultiplier,
		)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestAccountShareModeRepositoryGetActiveMembershipRejectsInvalidRuntimeSnapshot(t *testing.T) {
	tests := []struct {
		name                   string
		termsSnapshot          any
		expectRevisionMismatch bool
	}{
		{
			name:          "missing terms snapshot",
			termsSnapshot: nil,
		},
		{
			name: "terms revision mismatch",
			termsSnapshot: accountShareRuntimeTermsJSON(
				7002,
				3,
				0.35,
			),
		},
		{
			name:                   "terms content mismatch",
			termsSnapshot:          accountShareRuntimeTermsJSON(7001, 3, 0.36),
			expectRevisionMismatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			defer func() {
				_ = db.Close()
			}()
			repo := &accountShareModeRepository{db: db}

			membershipID := int64(700)
			listingID := int64(70)
			accountID := int64(99)
			ownerUserID := int64(42)
			consumerUserID := int64(20)
			apiKeyID := int64(30)
			groupID := int64(50)
			revisionID := int64(7001)
			revisionNumber := int64(3)
			joinedAt := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)

			mock.ExpectBegin()
			mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id, m\\.account_id").
				WithArgs(consumerUserID, apiKeyID, groupID).
				WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
					accountShareEndMembershipRow(
						membershipID,
						listingID,
						accountID,
						ownerUserID,
						consumerUserID,
						apiKeyID,
						service.AccountShareMembershipStatusActive,
						joinedAt,
						joinedAt,
					)...,
				))
			expectAccountShareMembershipRuntimeSnapshot(
				mock,
				membershipID,
				revisionID,
				revisionNumber,
				tt.termsSnapshot,
			)
			if tt.expectRevisionMismatch {
				expectAccountShareMembershipTermsRevision(
					mock,
					listingID,
					revisionID,
					revisionNumber,
					0.35,
				)
			}
			mock.ExpectRollback()

			_, _, err = repo.GetActiveMembershipForRequest(
				context.Background(),
				consumerUserID,
				apiKeyID,
				groupID,
			)
			if !errors.Is(err, service.ErrAccountShareBillingBindingUnavailable) {
				t.Fatalf("expected unavailable immutable runtime binding, got %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet expectations: %v", err)
			}
		})
	}
}

func TestAccountShareModeRepositoryGetActiveMembershipRejectsMissingOpenRuntimeBinding(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer func() {
		_ = db.Close()
	}()
	repo := &accountShareModeRepository{db: db}

	membershipID := int64(700)
	listingID := int64(70)
	accountID := int64(99)
	ownerUserID := int64(42)
	consumerUserID := int64(20)
	apiKeyID := int64(30)
	groupID := int64(50)
	revisionID := int64(7001)
	revisionNumber := int64(3)
	joinedAt := time.Date(2026, 7, 27, 8, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT\\s+m\\.id, m\\.listing_id, m\\.account_id").
		WithArgs(consumerUserID, apiKeyID, groupID).
		WillReturnRows(sqlmock.NewRows(accountShareMembershipColumns()).AddRow(
			accountShareEndMembershipRow(
				membershipID,
				listingID,
				accountID,
				ownerUserID,
				consumerUserID,
				apiKeyID,
				service.AccountShareMembershipStatusActive,
				joinedAt,
				joinedAt,
			)...,
		))
	expectAccountShareMembershipRuntimeSnapshot(
		mock,
		membershipID,
		revisionID,
		revisionNumber,
		accountShareRuntimeTermsJSON(revisionID, revisionNumber, 0.35),
	)
	expectAccountShareMembershipTermsRevision(
		mock,
		listingID,
		revisionID,
		revisionNumber,
		0.35,
	)
	mock.ExpectQuery("SELECT\\s+binding\\.listing_revision_id,\\s+binding\\.terms_revision_number").
		WithArgs(
			membershipID,
			listingID,
			accountID,
			revisionID,
			service.AccountShareMembershipStatusActive,
		).
		WillReturnRows(sqlmock.NewRows([]string{
			"listing_revision_id",
			"terms_revision_number",
		}))
	mock.ExpectRollback()

	_, _, err = repo.GetActiveMembershipForRequest(
		context.Background(),
		consumerUserID,
		apiKeyID,
		groupID,
	)
	if !errors.Is(err, service.ErrAccountShareBillingBindingUnavailable) {
		t.Fatalf("expected missing open binding to fail closed, got %v", err)
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

const accountShareUpdateListingLockQueryPattern = "SELECT\\s+l\\.owner_user_id,\\s+COALESCE\\(l\\.room_name"

func expectAccountShareEditDatabaseBlockers(
	mock sqlmock.Sqlmock,
	listingID int64,
	activeCount int,
	queuedCount int,
	endingCount int,
	synchronousBillingPendingCount int,
) {
	// 编辑准入用的是 accountShareListingEditBlockersInTx：与生命周期口径同源，但 JOIN 上
	// listings 并排除房主自己的席位（房主自用不该把自己锁死在改不了配置的状态）。
	mock.ExpectQuery(`(?s)SELECT\s+COUNT\(\*\) FILTER \(WHERE membership\.status = 'active'\)::int.*settlement_status IN \('pending', 'processing', 'failed'\).*FROM account_share_memberships membership.*membership\.consumer_user_id <> listing\.owner_user_id`).
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{
			"active_count",
			"queued_count",
			"ending_count",
			"synchronous_billing_pending_count",
		}).AddRow(
			activeCount,
			queuedCount,
			endingCount,
			synchronousBillingPendingCount,
		))
}

type accountShareUpdateListingLockRowData struct {
	OwnerUserID            int64
	RoomName               string
	Status                 string
	RowVersion             int64
	SeatLimit              int
	RateMultiplier         float64
	AllowedModels          string
	PerUserConcurrency     int
	HourlyRate             float64
	HourlyFeeWaiverMinimum float64
	MinBalanceRequired     float64
	CodexCLIOnly           bool
	Codex5hLimitPercent    float64
	Codex7dLimitPercent    float64
	EditSessionID          any
	EditingByUserID        any
	EditingExpiresAt       any
	PendingOperationID     any
}

func accountShareUpdateListingLockRows(
	configure ...func(*accountShareUpdateListingLockRowData),
) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{
		"owner_user_id",
		"room_name",
		"status",
		"row_version",
		"seat_limit",
		"rate_multiplier",
		"allowed_models",
		"per_user_concurrency",
		"hourly_rate",
		"hourly_fee_waiver_minimum",
		"min_balance_required",
		"codex_cli_only",
		"codex_5h_limit_percent",
		"codex_7d_limit_percent",
		"edit_session_id",
		"editing_by_user_id",
		"editing_expires_at",
		"pending_operation_id",
	})
	if len(configure) == 0 {
		return rows
	}
	row := accountShareUpdateListingLockRowData{
		OwnerUserID:         42,
		RoomName:            "shared-room",
		Status:              service.AccountShareListingStatusActive,
		RowVersion:          1,
		SeatLimit:           4,
		RateMultiplier:      0.2,
		AllowedModels:       `["gpt-5.5"]`,
		PerUserConcurrency:  5,
		HourlyRate:          0.15,
		MinBalanceRequired:  1.0,
		Codex5hLimitPercent: 99,
		Codex7dLimitPercent: 99,
	}
	for _, apply := range configure {
		if apply != nil {
			apply(&row)
		}
	}
	return rows.AddRow(
		row.OwnerUserID,
		row.RoomName,
		row.Status,
		row.RowVersion,
		row.SeatLimit,
		row.RateMultiplier,
		row.AllowedModels,
		row.PerUserConcurrency,
		row.HourlyRate,
		row.HourlyFeeWaiverMinimum,
		row.MinBalanceRequired,
		row.CodexCLIOnly,
		row.Codex5hLimitPercent,
		row.Codex7dLimitPercent,
		row.EditSessionID,
		row.EditingByUserID,
		row.EditingExpiresAt,
		row.PendingOperationID,
	)
}

type accountShareRevisionSourceRowData struct {
	ListingID              int64
	RowVersion             int64
	RoomName               string
	Platform               string
	AccountLevel           string
	OwnerUserID            int64
	OwnerDisplayName       string
	Status                 string
	SeatLimit              int
	RateMultiplier         float64
	AllowedModels          []byte
	PerUserConcurrency     int
	HourlyRate             float64
	HourlyFeeWaiverMinimum float64
	MinBalanceRequired     float64
	CodexCLIOnly           bool
	Codex5hLimitPercent    float64
	Codex7dLimitPercent    float64
}

func accountShareRevisionSnapshotRows(
	listingID,
	rowVersion int64,
	roomName string,
	ownerUserID int64,
	ownerDisplayName string,
	configure ...func(*accountShareRevisionSourceRowData),
) *sqlmock.Rows {
	row := &accountShareRevisionSourceRowData{
		ListingID:           listingID,
		RowVersion:          rowVersion,
		RoomName:            roomName,
		Platform:            service.PlatformOpenAI,
		AccountLevel:        "pro",
		OwnerUserID:         ownerUserID,
		OwnerDisplayName:    ownerDisplayName,
		Status:              service.AccountShareListingStatusActive,
		SeatLimit:           4,
		RateMultiplier:      0.2,
		AllowedModels:       []byte(`["gpt-5.5"]`),
		PerUserConcurrency:  5,
		HourlyRate:          0.15,
		MinBalanceRequired:  1,
		Codex5hLimitPercent: 99,
		Codex7dLimitPercent: 99,
	}
	for _, apply := range configure {
		if apply != nil {
			apply(row)
		}
	}
	return sqlmock.NewRows([]string{
		"id",
		"row_version",
		"room_name",
		"platform",
		"account_level",
		"owner_user_id",
		"owner_display_name",
		"status",
		"seat_limit",
		"rate_multiplier",
		"allowed_models",
		"per_user_concurrency",
		"hourly_rate",
		"hourly_fee_waiver_minimum",
		"min_balance_required",
		"codex_cli_only",
		"codex_5h_limit_percent",
		"codex_7d_limit_percent",
	}).AddRow(
		row.ListingID,
		row.RowVersion,
		row.RoomName,
		row.Platform,
		row.AccountLevel,
		row.OwnerUserID,
		row.OwnerDisplayName,
		row.Status,
		row.SeatLimit,
		row.RateMultiplier,
		row.AllowedModels,
		row.PerUserConcurrency,
		row.HourlyRate,
		row.HourlyFeeWaiverMinimum,
		row.MinBalanceRequired,
		row.CodexCLIOnly,
		row.Codex5hLimitPercent,
		row.Codex7dLimitPercent,
	)
}

type accountShareStoredRevisionRowData struct {
	RevisionID             int64
	ListingID              int64
	RevisionNumber         int64
	SchemaVersion          int
	SnapshotQuality        string
	RoomName               string
	Platform               string
	AccountLevel           string
	OwnerUserID            int64
	OwnerDisplayName       string
	Status                 string
	SeatLimit              int
	RateMultiplier         float64
	AllowedModels          []byte
	PerUserConcurrency     int
	HourlyRate             float64
	HourlyFeeWaiverMinimum float64
	MinBalanceRequired     float64
	CodexCLIOnly           bool
	Codex5hLimitPercent    float64
	Codex7dLimitPercent    float64
}

func accountShareStoredRevisionRows(
	revisionID,
	listingID,
	revisionNumber int64,
	roomName string,
	ownerUserID int64,
	ownerDisplayName string,
	configure ...func(*accountShareStoredRevisionRowData),
) *sqlmock.Rows {
	row := &accountShareStoredRevisionRowData{
		RevisionID:          revisionID,
		ListingID:           listingID,
		RevisionNumber:      revisionNumber,
		SchemaVersion:       1,
		SnapshotQuality:     service.AccountShareSnapshotQualityExact,
		RoomName:            roomName,
		Platform:            service.PlatformOpenAI,
		AccountLevel:        "pro",
		OwnerUserID:         ownerUserID,
		OwnerDisplayName:    ownerDisplayName,
		Status:              service.AccountShareListingStatusActive,
		SeatLimit:           4,
		RateMultiplier:      0.2,
		AllowedModels:       []byte(`["gpt-5.5"]`),
		PerUserConcurrency:  5,
		HourlyRate:          0.15,
		MinBalanceRequired:  1,
		Codex5hLimitPercent: 99,
		Codex7dLimitPercent: 99,
	}
	for _, apply := range configure {
		if apply != nil {
			apply(row)
		}
	}
	return sqlmock.NewRows([]string{
		"id",
		"listing_id",
		"revision_number",
		"schema_version",
		"snapshot_quality",
		"room_name",
		"platform",
		"account_level",
		"owner_user_id",
		"owner_display_name_snapshot",
		"status",
		"seat_limit",
		"rate_multiplier",
		"allowed_models",
		"per_user_concurrency",
		"hourly_rate",
		"hourly_fee_waiver_minimum",
		"min_balance_required",
		"codex_cli_only",
		"codex_5h_limit_percent",
		"codex_7d_limit_percent",
	}).AddRow(
		row.RevisionID,
		row.ListingID,
		row.RevisionNumber,
		row.SchemaVersion,
		row.SnapshotQuality,
		row.RoomName,
		row.Platform,
		row.AccountLevel,
		row.OwnerUserID,
		row.OwnerDisplayName,
		row.Status,
		row.SeatLimit,
		row.RateMultiplier,
		row.AllowedModels,
		row.PerUserConcurrency,
		row.HourlyRate,
		row.HourlyFeeWaiverMinimum,
		row.MinBalanceRequired,
		row.CodexCLIOnly,
		row.Codex5hLimitPercent,
		row.Codex7dLimitPercent,
	)
}

func accountShareAcceptedJoinTerms(
	revisionID,
	rowVersion int64,
	roomName string,
	configure ...func(*service.AccountShareListingTermsSnapshot),
) *service.AccountShareListingTermsSnapshot {
	terms := &service.AccountShareListingTermsSnapshot{
		ListingRevisionID:       revisionID,
		RowVersion:              rowVersion,
		SchemaVersion:           1,
		RoomName:                roomName,
		Status:                  service.AccountShareListingStatusActive,
		SeatLimit:               4,
		RateMultiplier:          0.2,
		AllowedModels:           []string{"gpt-5.5"},
		PerUserConcurrency:      5,
		HourlyRate:              0.15,
		MinBalanceRequired:      1,
		Codex5hLimitPercent:     99,
		Codex7dLimitPercent:     99,
		Anthropic5hLimitPercent: 99,
		Anthropic7dLimitPercent: 99,
	}
	for _, apply := range configure {
		if apply != nil {
			apply(terms)
		}
	}
	return terms
}

type accountShareListingRowData struct {
	ListingID                               int64
	RowVersion                              int64
	CurrentRevisionID                       any
	Deleted                                 bool
	AccountID                               int64
	OwnerUserID                             int64
	RoomName                                string
	Status                                  string
	EditSessionID                           string
	EditingExpiresAt                        time.Time
	RateMultiplier                          float64
	RepresentativeAccountConcurrency        int
	RepresentativeAccountAutoPauseOnExpired bool
	AccountExpiresAt                        any
	HourlyRate                              float64
	HourlyFeeWaiverMinimum                  float64
	CurrentMembershipID                     any
	CurrentConsumerUserID                   any
	CurrentAPIKeyID                         any
	CurrentAPIKeyName                       any
	CurrentJoinedAt                         any
	CurrentPaidUntil                        any
	CurrentBilledUntil                      any
	CurrentIdleTimeoutMinutes               any
	CurrentLastRequestAt                    any
	CurrentWaiverWindowStartedAt            any
	CurrentWaiverWindowUsageAmount          any
	CurrentWaiverWindowRequestCount         any
	CurrentWaiverWindowLastRequestAt        any
	QueueMembershipID                       any
	QueueAPIKeyID                           any
	QueueAPIKeyName                         any
	QueueRank                               any
	QueueStatus                             any
	QueueEndingOperationID                  any
	QueueEndingOperationStatus              any
	QueueSettlementStatus                   any
	QueueIdleTimeoutMinutes                 any
	QueueDispatchCooldownUntil              any
	LastUsedMembershipID                    any
	LastUsedAt                              any
}

func accountShareListingRows(listingID, accountID, ownerUserID int64, editSessionID string, editingExpiresAt time.Time, configure ...func(*accountShareListingRowData)) *sqlmock.Rows {
	now := time.Now().UTC()
	row := &accountShareListingRowData{
		ListingID:                        listingID,
		RowVersion:                       1,
		AccountID:                        accountID,
		OwnerUserID:                      ownerUserID,
		RoomName:                         "shared-room",
		Status:                           service.AccountShareListingStatusActive,
		EditSessionID:                    editSessionID,
		EditingExpiresAt:                 editingExpiresAt,
		RateMultiplier:                   0.2,
		RepresentativeAccountConcurrency: 20,
		HourlyRate:                       0.15,
		HourlyFeeWaiverMinimum:           0,
	}
	for _, apply := range configure {
		if apply != nil {
			apply(row)
		}
	}
	columns := []string{
		"id",
		"row_version",
		"current_revision_id",
		"deleted",
		"account_id",
		"room_name",
		"account_count",
		"healthy_account_count",
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
		"representative_account_concurrency",
		"representative_account_auto_pause_on_expired",
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
		"queue_ending_operation_id",
		"queue_ending_operation_status",
		"queue_settlement_status",
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
		row.RowVersion,
		row.CurrentRevisionID,
		row.Deleted,
		row.AccountID,
		row.RoomName,
		1,
		1,
		row.OwnerUserID,
		"owner",
		"shared-account",
		nil,
		row.Status,
		4,
		0,
		nil,
		0,
		0,
		0.0,
		row.RateMultiplier,
		[]byte(`["gpt-5.5"]`),
		5,
		20,
		row.RepresentativeAccountConcurrency,
		row.RepresentativeAccountAutoPauseOnExpired,
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
		row.AccountExpiresAt,
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
		row.QueueMembershipID,
		row.QueueAPIKeyID,
		row.QueueAPIKeyName,
		row.QueueRank,
		row.QueueStatus,
		row.QueueEndingOperationID,
		row.QueueEndingOperationStatus,
		row.QueueSettlementStatus,
		row.QueueIdleTimeoutMinutes,
		row.QueueDispatchCooldownUntil,
		row.LastUsedMembershipID,
		row.LastUsedAt,
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

// 回归：风控 suspend 之后房间配置必须冻结。
// 免锁的「消费者安全更新」分支此前从 HTTP 入口不可达（service 层有条件完全相同的前置判定
// 堵着），该分支整个绕过了房间生命周期状态门禁；前置判定删掉后分支被激活，suspended、
// draining 的房间就会因为「这次改动对消费者无害」被放行改合约字段并 bump row_version，
// 等于风控挂起不再冻结配置。第二个子用例同时证明这条免锁分支在 paused 下确实可达，
// 免得第一个子用例因为别的原因（比如压根没进这个分支）假绿。
func TestAccountShareModeRepositoryUpdateListingRejectsConsumerSafeUpdateWhenSuspended(t *testing.T) {
	listingID := int64(7)
	ownerUserID := int64(42)

	t.Run("suspended room freezes consumer safe update", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() {
			_ = db.Close()
		}()
		repo := &accountShareModeRepository{db: db}

		expectedVersion := int64(1)
		loweredHourlyRate := 0.05

		mock.ExpectBegin()
		mock.ExpectQuery(accountShareUpdateListingLockQueryPattern).
			WithArgs(listingID, ownerUserID).
			WillReturnRows(accountShareUpdateListingLockRows(func(row *accountShareUpdateListingLockRowData) {
				row.OwnerUserID = ownerUserID
				row.RowVersion = expectedVersion
				row.Status = service.AccountShareListingStatusSuspended
			}))
		// 状态门禁必须在消费者安全判定之前拦下：锁行之后不允许再有任何查询或 UPDATE。
		mock.ExpectRollback()

		_, err = repo.UpdateListing(context.Background(), ownerUserID, false, listingID, service.UpdateAccountShareListingInput{
			HourlyRate:      &loweredHourlyRate,
			ExpectedVersion: &expectedVersion,
			Reason:          "lower price for consumers",
		})
		if !errors.Is(err, service.ErrAccountShareUpdateRequiresPaused) {
			t.Fatalf("UpdateListing error = %v, want %v", err, service.ErrAccountShareUpdateRequiresPaused)
		}
		// sqlmock 是有序匹配：若代码真的执行了 UPDATE，上面的 errors.Is 会先失败；
		// 这里再兜一层，确认锁行之后确实一条语句都没跑。
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("suspended room must be rejected before any further statement: %v", err)
		}
	})

	t.Run("paused room still reaches update for the same input", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() {
			_ = db.Close()
		}()
		repo := &accountShareModeRepository{db: db}

		expectedVersion := int64(1)
		loweredHourlyRate := 0.05
		updateErr := errors.New("stop after update")

		mock.ExpectBegin()
		mock.ExpectQuery(accountShareUpdateListingLockQueryPattern).
			WithArgs(listingID, ownerUserID).
			WillReturnRows(accountShareUpdateListingLockRows(func(row *accountShareUpdateListingLockRowData) {
				row.OwnerUserID = ownerUserID
				row.RowVersion = expectedVersion
				row.Status = service.AccountShareListingStatusPaused
			}))
		// 只降 hourly_rate 时消费者安全判定不需要查席位/并发，直接判定为安全，
		// 于是免锁放行、跳过编辑会话与编辑阻塞项检查，直达 UPDATE。
		mock.ExpectExec("UPDATE account_share_listings").
			WithArgs(loweredHourlyRate, listingID, ownerUserID, expectedVersion).
			WillReturnError(updateErr)
		mock.ExpectRollback()

		_, err = repo.UpdateListing(context.Background(), ownerUserID, false, listingID, service.UpdateAccountShareListingInput{
			HourlyRate:      &loweredHourlyRate,
			ExpectedVersion: &expectedVersion,
			Reason:          "lower price for consumers",
		})
		if !errors.Is(err, updateErr) {
			t.Fatalf("UpdateListing error = %v, want %v", err, updateErr)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet expectations: %v", err)
		}
	})
}

// 回归：自己的编辑锁过期后不能被误报成「别人正在编辑」。
// 旧写法把「库里有一把（已过期的）锁」也算作占用，房主的会话续期一失败就再也保存不了
// 任何东西——连不需要编辑会话的纯改房间名都被 ACCOUNT_SHARE_LISTING_EDITING 打死，
// 而且等谁都等不到（锁是自己的，没有第二个人会来释放它）。
// 现在过期锁不在编辑锁判定里拦，落到 editSessionHeld：纯改名照常放行，合约变更拿到
// 可自愈的 ACCOUNT_SHARE_EDIT_SESSION_INVALID（关窗重进编辑即可）。
func TestAccountShareModeRepositoryUpdateListingAllowsRenameWithExpiredOwnEditLock(t *testing.T) {
	listingID := int64(7)
	ownerUserID := int64(42)
	staleEditSessionID := "expired-edit-session"

	t.Run("rename only is saved", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() {
			_ = db.Close()
		}()
		repo := &accountShareModeRepository{db: db}

		expectedVersion := int64(1)
		nextVersion := int64(2)
		revisionID := int64(703)
		name := "renamed-room"
		reason := "rename after edit session expired"

		mock.ExpectBegin()
		mock.ExpectQuery(accountShareUpdateListingLockQueryPattern).
			WithArgs(listingID, ownerUserID).
			WillReturnRows(accountShareUpdateListingLockRows(func(row *accountShareUpdateListingLockRowData) {
				row.OwnerUserID = ownerUserID
				row.RowVersion = expectedVersion
				// 锁是房主自己的，但已经过期：activeEdit=false，不该被当成占用。
				row.EditSessionID = staleEditSessionID
				row.EditingByUserID = ownerUserID
				row.EditingExpiresAt = time.Now().UTC().Add(-10 * time.Minute)
			}))
		mock.ExpectExec("SELECT pg_advisory_xact_lock").
			WithArgs("account_share_room_name:42:renamed-room").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery("SELECT l\\.id\\s+FROM account_share_listings l").
			WithArgs(ownerUserID, name, listingID).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))
		// 非合约字段：contractUpdate=false，所以 UPDATE 不会顺手清掉编辑锁字段。
		mock.ExpectExec("UPDATE account_share_listings").
			WithArgs(name, listingID, ownerUserID, expectedVersion).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery("SELECT\\s+l\\.id, l\\.row_version").
			WithArgs(listingID).
			WillReturnRows(accountShareRevisionSnapshotRows(
				listingID,
				nextVersion,
				name,
				ownerUserID,
				"owner",
			))
		mock.ExpectQuery("INSERT INTO account_share_listing_revisions").
			WithArgs(
				listingID,
				nextVersion,
				1,
				service.AccountShareSnapshotQualityExact,
				name,
				service.PlatformOpenAI,
				"pro",
				ownerUserID,
				"owner",
				service.AccountShareListingStatusActive,
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
				ownerUserID,
				"owner",
				"update_listing",
				reason,
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
				"listing.updated",
				ownerUserID,
				"owner",
				reason,
				`{"changed_fields":["room_name"],"force_applied":false,"row_version":2,"source":"update_listing"}`,
			).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
		mock.ExpectQuery("SELECT\\s+l\\.id").
			WithArgs(ownerUserID, listingID).
			WillReturnRows(accountShareListingRows(listingID, 99, ownerUserID, "", time.Time{}, func(row *accountShareListingRowData) {
				row.RowVersion = nextVersion
				row.CurrentRevisionID = revisionID
				row.RoomName = name
			}))

		listing, err := repo.UpdateListing(context.Background(), ownerUserID, false, listingID, service.UpdateAccountShareListingInput{
			Name:            &name,
			EditSessionID:   staleEditSessionID,
			ExpectedVersion: &expectedVersion,
			Reason:          reason,
		})
		if errors.Is(err, service.ErrAccountShareListingEditing) {
			t.Fatalf("expired own edit lock must not be reported as someone else editing: %v", err)
		}
		if err != nil {
			t.Fatalf("UpdateListing failed: %v", err)
		}
		if listing.RoomName != name || listing.RowVersion != nextVersion {
			t.Fatalf("unexpected listing state: room_name=%q row_version=%d", listing.RoomName, listing.RowVersion)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet expectations: %v", err)
		}
	})

	t.Run("contract update reports a self healing edit session error", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer func() {
			_ = db.Close()
		}()
		repo := &accountShareModeRepository{db: db}

		expectedVersion := int64(1)
		seatLimit := 6

		mock.ExpectBegin()
		mock.ExpectQuery(accountShareUpdateListingLockQueryPattern).
			WithArgs(listingID, ownerUserID).
			WillReturnRows(accountShareUpdateListingLockRows(func(row *accountShareUpdateListingLockRowData) {
				row.OwnerUserID = ownerUserID
				row.RowVersion = expectedVersion
				row.Status = service.AccountShareListingStatusPaused
				row.EditSessionID = staleEditSessionID
				row.EditingByUserID = ownerUserID
				row.EditingExpiresAt = time.Now().UTC().Add(-10 * time.Minute)
			}))
		mock.ExpectRollback()

		_, err = repo.UpdateListing(context.Background(), ownerUserID, false, listingID, service.UpdateAccountShareListingInput{
			SeatLimit:       &seatLimit,
			EditSessionID:   staleEditSessionID,
			ExpectedVersion: &expectedVersion,
			Reason:          "raise seats after edit session expired",
		})
		if errors.Is(err, service.ErrAccountShareListingEditing) {
			t.Fatalf("expired own edit lock must not be reported as someone else editing: %v", err)
		}
		if !errors.Is(err, service.ErrAccountShareEditSessionInvalid) {
			t.Fatalf("UpdateListing error = %v, want %v", err, service.ErrAccountShareEditSessionInvalid)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet expectations: %v", err)
		}
	})
}
