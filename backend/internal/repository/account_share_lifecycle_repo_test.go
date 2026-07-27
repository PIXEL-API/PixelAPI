package repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func TestAccountShareModeRepositoryGetRoomManagementStatePermissionsAndScan(t *testing.T) {
	const (
		listingID  = int64(7)
		ownerID    = int64(42)
		adminID    = int64(9)
		outsiderID = int64(77)
	)
	deletedAt := time.Date(2026, time.July, 27, 8, 30, 0, 0, time.FixedZone("UTC+8", 8*60*60))

	tests := []struct {
		name          string
		viewerUserID  int64
		viewerIsAdmin bool
		returnRow     bool
		wantErr       error
	}{
		{
			name:         "owner can inspect own room",
			viewerUserID: ownerID,
			returnRow:    true,
		},
		{
			name:          "admin can inspect another owner's room",
			viewerUserID:  adminID,
			viewerIsAdmin: true,
			returnRow:     true,
		},
		{
			name:         "another owner cannot inspect the room",
			viewerUserID: outsiderID,
			wantErr:      service.ErrAccountShareListingNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock := newAccountShareLifecycleSQLMock(t)
			rows := sqlmock.NewRows(lifecycleManagementStateColumns())
			if tt.returnRow {
				rows.AddRow(
					listingID,
					"历史房间",
					ownerID,
					int64(9),
					service.AccountShareListingStatusActive,
					service.AccountShareRoomHealthDegraded,
					"partial_capacity",
					"one account is temporarily unavailable",
					15,
					3,
					2,
					10,
					1,
					2,
					12,
					8,
					4,
					5,
					4,
					3,
					true,
					true,
					"operation-7",
					"{701,702}",
					"{11,12}",
					deletedAt,
				)
			}
			mock.ExpectQuery("WITH membership_stats AS").
				WithArgs(listingID, tt.viewerIsAdmin, tt.viewerUserID).
				WillReturnRows(rows)

			state, err := repo.GetRoomManagementState(
				context.Background(),
				tt.viewerUserID,
				tt.viewerIsAdmin,
				listingID,
			)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("GetRoomManagementState error = %v, want %v", err, tt.wantErr)
				}
				if state != nil {
					t.Fatalf("GetRoomManagementState state = %#v, want nil", state)
				}
				return
			}
			if err != nil {
				t.Fatalf("GetRoomManagementState failed: %v", err)
			}
			if state.ListingID != listingID ||
				state.RoomName != "历史房间" ||
				state.OwnerUserID != ownerID ||
				state.RowVersion != 9 ||
				state.LifecycleStatus != service.AccountShareListingStatusActive ||
				state.HealthState != service.AccountShareRoomHealthDegraded ||
				state.StatusReasonCode != "partial_capacity" ||
				state.StatusReason != "one account is temporarily unavailable" {
				t.Fatalf("unexpected management identity/lifecycle fields: %#v", state)
			}
			if state.SeatLimit != 15 ||
				state.ActiveSeats != 3 ||
				state.EndingSeats != 2 ||
				state.AdmissionRemainingSeats != 10 ||
				state.QueuedMembershipCount != 1 ||
				state.RoomAccountCount != 2 ||
				state.ConfiguredTotalConcurrency != 12 ||
				state.EligibleTotalConcurrency != 8 ||
				state.PendingBillingIntentCount != 4 {
				t.Fatalf("unexpected management capacity fields: %#v", state)
			}
			if state.Blockers.ActiveMembershipCount != 4 ||
				state.Blockers.QueuedMembershipCount != 1 ||
				state.Blockers.EndingMembershipCount != 3 ||
				state.Blockers.PendingBillingIntentCount != 4 ||
				state.Blockers.SynchronousBillingPendingCount != 5 ||
				!state.Blockers.ValidEditSession ||
				!state.Blockers.ConflictingOperation ||
				state.Blockers.ConflictingOperationID != "operation-7" ||
				state.PendingOperationID != "operation-7" {
				t.Fatalf("unexpected management blockers: %#v", state.Blockers)
			}
			if !reflect.DeepEqual(state.RuntimeMembershipIDs, []int64{701, 702}) {
				t.Fatalf("RuntimeMembershipIDs = %v, want [701 702]", state.RuntimeMembershipIDs)
			}
			if !reflect.DeepEqual(state.RuntimeAccountIDs, []int64{11, 12}) {
				t.Fatalf("RuntimeAccountIDs = %v, want [11 12]", state.RuntimeAccountIDs)
			}
			if state.DeletedAt == nil || !state.DeletedAt.Equal(deletedAt.UTC()) {
				t.Fatalf("DeletedAt = %v, want %v", state.DeletedAt, deletedAt.UTC())
			}
		})
	}
}

func TestAccountShareModeRepositoryRoomLifecycleOwnerDrainCommitsRevision(t *testing.T) {
	const (
		listingID  = int64(7)
		ownerID    = int64(42)
		accountID  = int64(99)
		oldVersion = int64(3)
		newVersion = int64(4)
		revisionID = int64(704)
	)
	reason := "owner maintenance"
	repo, mock := newAccountShareLifecycleSQLMock(t)
	operationID := &lifecycleCapturedStringArgument{}

	mock.ExpectBegin()
	expectLifecycleListingLock(
		mock,
		listingID,
		ownerID,
		false,
		lifecycleLockedListingRows(
			listingID,
			ownerID,
			"lifecycle-room",
			service.AccountShareListingStatusActive,
			oldVersion,
		),
	)
	mock.ExpectQuery("SELECT id\\s+FROM account_share_memberships\\s+WHERE listing_id = \\$1\\s+AND status = 'queued'").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec("INSERT INTO account_share_room_operations").
		WithArgs(
			operationID,
			listingID,
			accountShareRoomOperationActionDrain,
			ownerID,
			"owner",
			oldVersion,
			newVersion,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("(?s)UPDATE account_share_listings\\s+SET status = \\$1::varchar\\(20\\).*CASE WHEN \\$1::varchar\\(20\\) = 'draining'::varchar\\(20\\)").
		WithArgs(
			service.AccountShareListingStatusDraining,
			"owner_draining",
			reason,
			operationID,
			listingID,
			oldVersion,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	expectLifecycleRevisionSuccess(
		mock,
		listingID,
		newVersion,
		revisionID,
		ownerID,
		ownerID,
		false,
		"lifecycle-room",
		service.AccountShareListingStatusDraining,
		"drain_room",
		reason,
		"listing.drain_requested",
		operationID,
	)
	mock.ExpectCommit()
	mock.ExpectQuery("SELECT\\s+l\\.id").
		WithArgs(ownerID, listingID).
		WillReturnRows(accountShareListingRows(
			listingID,
			accountID,
			ownerID,
			"",
			time.Time{},
			func(row *accountShareListingRowData) {
				row.RowVersion = newVersion
				row.CurrentRevisionID = revisionID
				row.RoomName = "lifecycle-room"
				row.Status = service.AccountShareListingStatusDraining
			},
		))

	listing, err := repo.TransitionRoomLifecycle(
		context.Background(),
		ownerID,
		false,
		listingID,
		service.AccountShareRoomActionDrain,
		service.AccountShareRoomLifecycleCommandInput{
			ExpectedVersion: oldVersion,
			Reason:          reason,
		},
	)
	if err != nil {
		t.Fatalf("TransitionRoomLifecycle failed: %v", err)
	}
	if operationID.value == "" {
		t.Fatal("drain operation id was not persisted")
	}
	if listing.RowVersion != newVersion ||
		listing.CurrentRevisionID == nil ||
		*listing.CurrentRevisionID != revisionID ||
		listing.Status != service.AccountShareListingStatusDraining {
		t.Fatalf("unexpected drained listing: %#v", listing)
	}
}

func TestEndQueuedMembershipsForRoomDrainUsesSupportedLifecycleReason(t *testing.T) {
	const (
		listingID    = int64(7)
		membershipID = int64(51)
		actorUserID  = int64(42)
	)
	repo, mock := newAccountShareLifecycleSQLMock(t)

	mock.ExpectBegin()
	tx, err := repo.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	mock.ExpectQuery("SELECT id\\s+FROM account_share_memberships\\s+WHERE listing_id = \\$1\\s+AND status = 'queued'").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(membershipID))
	mock.ExpectExec("UPDATE account_share_membership_account_bindings").
		WithArgs(actorUserID, "owner", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE account_share_memberships\\s+SET status = 'ended'").
		WithArgs(
			sqlmock.AnyArg(),
			service.AccountShareMembershipEndReasonRoomDraining,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := endQueuedMembershipsForRoomDrainInTx(
		context.Background(),
		tx,
		listingID,
		actorUserID,
		"owner",
	); err != nil {
		t.Fatalf("endQueuedMembershipsForRoomDrainInTx: %v", err)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
}

func TestAccountShareModeRepositoryRoomLifecycleRollsBackAfterRevisionFailure(t *testing.T) {
	const (
		listingID  = int64(7)
		ownerID    = int64(42)
		oldVersion = int64(3)
		newVersion = int64(4)
	)
	repo, mock := newAccountShareLifecycleSQLMock(t)
	operationID := &lifecycleCapturedStringArgument{}
	sentinel := errors.New("revision persistence failed")

	mock.ExpectBegin()
	expectLifecycleListingLock(
		mock,
		listingID,
		ownerID,
		false,
		lifecycleLockedListingRows(
			listingID,
			ownerID,
			"lifecycle-room",
			service.AccountShareListingStatusActive,
			oldVersion,
		),
	)
	mock.ExpectQuery("SELECT id\\s+FROM account_share_memberships\\s+WHERE listing_id = \\$1\\s+AND status = 'queued'").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec("INSERT INTO account_share_room_operations").
		WithArgs(
			operationID,
			listingID,
			accountShareRoomOperationActionDrain,
			ownerID,
			"owner",
			oldVersion,
			newVersion,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("(?s)UPDATE account_share_listings\\s+SET status = \\$1::varchar\\(20\\).*CASE WHEN \\$1::varchar\\(20\\) = 'draining'::varchar\\(20\\)").
		WithArgs(
			service.AccountShareListingStatusDraining,
			"owner_draining",
			"atomic rollback",
			operationID,
			listingID,
			oldVersion,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT\\s+l\\.id, l\\.row_version").
		WithArgs(listingID).
		WillReturnError(sentinel)
	mock.ExpectRollback()

	listing, err := repo.TransitionRoomLifecycle(
		context.Background(),
		ownerID,
		false,
		listingID,
		service.AccountShareRoomActionDrain,
		service.AccountShareRoomLifecycleCommandInput{
			ExpectedVersion: oldVersion,
			Reason:          "atomic rollback",
		},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("TransitionRoomLifecycle error = %v, want %v", err, sentinel)
	}
	if listing != nil {
		t.Fatalf("TransitionRoomLifecycle listing = %#v, want nil", listing)
	}
}

func TestAccountShareModeRepositoryRoomDeletionSoftDeleteIdempotentReplay(t *testing.T) {
	const (
		listingID = int64(7)
		ownerID   = int64(42)
	)
	operationID := "11111111-1111-4111-8111-111111111111"
	requestID := "delete-request-7"
	now := time.Date(2026, time.July, 27, 1, 2, 3, 0, time.UTC)
	repo, mock := newAccountShareLifecycleSQLMock(t)

	mock.ExpectBegin()
	expectLifecycleListingLock(
		mock,
		listingID,
		ownerID,
		false,
		lifecycleLockedListingRows(
			listingID,
			ownerID,
			"lifecycle-room",
			service.AccountShareListingStatusDraining,
			6,
			func(row *lifecycleLockedListingRowData) {
				row.PendingOperationID = operationID
				row.DeleteRequestID = requestID
			},
		),
	)
	mock.ExpectQuery("SELECT\\s+id::text, listing_id, membership_id").
		WithArgs(operationID).
		WillReturnRows(lifecycleOperationRows(
			operationID,
			listingID,
			ownerID,
			"owner",
			accountShareRoomOperationActionDelete,
			accountShareRoomOperationStatusPending,
			now,
			func(row *lifecycleOperationRowData) {
				row.ExpectedVersion = int64(5)
				row.StartVersion = int64(6)
			},
		))
	mock.ExpectCommit()

	operation, err := repo.SoftDeleteRoom(
		context.Background(),
		ownerID,
		false,
		listingID,
		service.AccountShareRoomDeleteInput{
			ExpectedVersion: 5,
			RequestID:       requestID,
		},
	)
	if err != nil {
		t.Fatalf("SoftDeleteRoom replay failed: %v", err)
	}
	if operation.ID != operationID ||
		operation.ListingID != listingID ||
		operation.Action != accountShareRoomOperationActionDelete ||
		operation.Status != accountShareRoomOperationStatusPending {
		t.Fatalf("unexpected replayed operation: %#v", operation)
	}
}

func TestAccountShareModeRepositoryRoomDeletionSoftDeleteBlockedRollsBack(t *testing.T) {
	const (
		listingID = int64(7)
		ownerID   = int64(42)
	)
	repo, mock := newAccountShareLifecycleSQLMock(t)

	mock.ExpectBegin()
	expectLifecycleListingLock(
		mock,
		listingID,
		ownerID,
		false,
		lifecycleLockedListingRows(
			listingID,
			ownerID,
			"lifecycle-room",
			service.AccountShareListingStatusActive,
			5,
		),
	)
	expectLifecycleDatabaseBlockers(mock, listingID, 1, 0, 0, 0, 0)
	mock.ExpectRollback()

	operation, err := repo.SoftDeleteRoom(
		context.Background(),
		ownerID,
		false,
		listingID,
		service.AccountShareRoomDeleteInput{
			ExpectedVersion: 5,
			RequestID:       "delete-request-blocked",
			Reason:          "cleanup",
		},
	)
	if !errors.Is(err, service.ErrAccountShareRoomDeleteBlocked) {
		t.Fatalf("SoftDeleteRoom error = %v, want delete blocked", err)
	}
	if operation != nil {
		t.Fatalf("SoftDeleteRoom operation = %#v, want nil", operation)
	}
}

func TestEnsureAccountShareDeletionReviewIdentityMaterializesLegacyRoomIdentity(t *testing.T) {
	const (
		listingID  = int64(7)
		ownerID    = int64(42)
		accountID  = int64(88)
		identityID = int64(701)
	)
	repo, mock := newAccountShareLifecycleSQLMock(t)
	mock.ExpectBegin()
	tx, err := repo.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}

	mock.ExpectQuery("SELECT EXISTS \\(\\s+SELECT 1\\s+FROM account_share_memberships membership").
		WithArgs(listingID, ownerID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("WITH candidate_accounts AS").
		WithArgs(listingID, ownerID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"name",
			"platform",
			"credentials",
			"extra",
		}).AddRow(
			accountID,
			"legacy-account",
			service.PlatformOpenAI,
			[]byte(`{"email":"owner@example.com"}`),
			[]byte(`{}`),
		))
	mock.ExpectQuery("INSERT INTO account_share_account_identities").
		WithArgs(
			service.PlatformOpenAI,
			"owner@example.com",
			"o***r@example.com",
			accountID,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(identityID))
	mock.ExpectExec("UPDATE account_share_listings\\s+SET account_identity_id = \\$1").
		WithArgs(identityID, listingID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	listing := &lockedAccountShareLifecycleListing{
		ID:          listingID,
		OwnerUserID: ownerID,
	}
	if err := ensureAccountShareDeletionReviewIdentityInTx(
		context.Background(),
		tx,
		listing,
	); err != nil {
		t.Fatalf("ensureAccountShareDeletionReviewIdentityInTx failed: %v", err)
	}
	if !listing.AccountIdentityID.Valid || listing.AccountIdentityID.Int64 != identityID {
		t.Fatalf("AccountIdentityID = %#v, want %d", listing.AccountIdentityID, identityID)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
}

func TestEnsureAccountShareDeletionReviewIdentityBlocksUnrecoverableLegacyRoom(t *testing.T) {
	const (
		listingID = int64(7)
		ownerID   = int64(42)
	)
	repo, mock := newAccountShareLifecycleSQLMock(t)
	mock.ExpectBegin()
	tx, err := repo.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx failed: %v", err)
	}

	mock.ExpectQuery("SELECT EXISTS \\(\\s+SELECT 1\\s+FROM account_share_memberships membership").
		WithArgs(listingID, ownerID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("WITH candidate_accounts AS").
		WithArgs(listingID, ownerID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"name",
			"platform",
			"credentials",
			"extra",
		}))

	err = ensureAccountShareDeletionReviewIdentityInTx(
		context.Background(),
		tx,
		&lockedAccountShareLifecycleListing{
			ID:          listingID,
			OwnerUserID: ownerID,
		},
	)
	if !errors.Is(err, service.ErrAccountShareRoomReviewIdentityMissing) {
		t.Fatalf("ensure error = %v, want review identity missing", err)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
}

func TestAccountShareModeRepositoryRoomDeletionSoftDeleteTxACommitsClaimOnly(t *testing.T) {
	const (
		listingID  = int64(7)
		ownerID    = int64(42)
		oldVersion = int64(5)
		newVersion = int64(6)
		revisionID = int64(706)
	)
	requestID := "delete-request-tx-a"
	reason := "owner requested cleanup"
	now := time.Date(2026, time.July, 27, 1, 2, 3, 0, time.UTC)
	repo, mock := newAccountShareLifecycleSQLMock(t)
	operationID := &lifecycleCapturedStringArgument{}

	mock.ExpectBegin()
	expectLifecycleListingLock(
		mock,
		listingID,
		ownerID,
		false,
		lifecycleLockedListingRows(
			listingID,
			ownerID,
			"lifecycle-room",
			service.AccountShareListingStatusActive,
			oldVersion,
		),
	)
	expectLifecycleDatabaseBlockers(mock, listingID, 0, 0, 0, 0, 0)
	mock.ExpectExec("INSERT INTO account_share_room_operations").
		WithArgs(
			operationID,
			listingID,
			ownerID,
			"owner",
			requestID,
			oldVersion,
			newVersion,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE account_share_listings\\s+SET status = 'draining'").
		WithArgs(
			reason,
			operationID,
			ownerID,
			requestID,
			listingID,
			oldVersion,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	expectLifecycleRevisionSuccess(
		mock,
		listingID,
		newVersion,
		revisionID,
		ownerID,
		ownerID,
		false,
		"lifecycle-room",
		service.AccountShareListingStatusDraining,
		"delete_request",
		reason,
		"listing.delete_requested",
		operationID,
	)
	mock.ExpectQuery("SELECT\\s+id::text, listing_id, membership_id").
		WithArgs(operationID).
		WillReturnRows(lifecycleOperationRows(
			"22222222-2222-4222-8222-222222222222",
			listingID,
			ownerID,
			"owner",
			accountShareRoomOperationActionDelete,
			accountShareRoomOperationStatusPending,
			now,
			func(row *lifecycleOperationRowData) {
				row.ExpectedVersion = oldVersion
				row.StartVersion = newVersion
			},
		))
	mock.ExpectCommit()

	operation, err := repo.SoftDeleteRoom(
		context.Background(),
		ownerID,
		false,
		listingID,
		service.AccountShareRoomDeleteInput{
			ExpectedVersion: oldVersion,
			RequestID:       requestID,
			Reason:          reason,
		},
	)
	if err != nil {
		t.Fatalf("SoftDeleteRoom Tx A failed: %v", err)
	}
	if operationID.value == "" {
		t.Fatal("delete operation id was not persisted")
	}
	if operation.Action != accountShareRoomOperationActionDelete ||
		operation.Status != accountShareRoomOperationStatusPending ||
		operation.ExpectedVersion == nil ||
		*operation.ExpectedVersion != oldVersion ||
		operation.StartVersion == nil ||
		*operation.StartVersion != newVersion {
		t.Fatalf("unexpected Tx A operation: %#v", operation)
	}
}

func TestAccountShareModeRepositoryRoomDeletionFinalizeLiveMembershipBlocked(t *testing.T) {
	const listingID = int64(7)
	operationID := "33333333-3333-4333-8333-333333333333"
	accountIDs := []int64{11}
	repo, mock := newAccountShareLifecycleSQLMock(t)

	mock.ExpectBegin()
	expectLifecycleListingLock(
		mock,
		listingID,
		0,
		true,
		lifecycleLockedListingRows(
			listingID,
			42,
			"lifecycle-room",
			service.AccountShareListingStatusDraining,
			6,
			func(row *lifecycleLockedListingRowData) {
				row.PendingOperationID = operationID
			},
		),
	)
	mock.ExpectQuery("SELECT account_id\\s+FROM account_share_room_accounts").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{"account_id"}).AddRow(accountIDs[0]))
	mock.ExpectQuery("SELECT id\\s+FROM accounts").
		WithArgs(pq.Array(accountIDs)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(accountIDs[0]))
	mock.ExpectQuery("SELECT id\\s+FROM account_share_memberships\\s+WHERE listing_id = \\$1\\s+AND status IN").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(901)))
	expectLifecycleDatabaseBlockers(mock, listingID, 1, 0, 0, 0, 0)
	mock.ExpectRollback()

	operation, err := repo.FinalizeRoomDeletion(context.Background(), listingID, operationID)
	if !errors.Is(err, service.ErrAccountShareRoomDeleteBlocked) {
		t.Fatalf("FinalizeRoomDeletion error = %v, want delete blocked", err)
	}
	if operation != nil {
		t.Fatalf("FinalizeRoomDeletion operation = %#v, want nil", operation)
	}
}

func TestAccountShareModeRepositoryRoomDeletionFinalizeClosesProjectionInOrder(t *testing.T) {
	const (
		listingID    = int64(7)
		ownerID      = int64(42)
		oldVersion   = int64(6)
		finalVersion = int64(7)
		revisionID   = int64(707)
	)
	operationID := "44444444-4444-4444-8444-444444444444"
	accountIDs := []int64{11, 12}
	bindingIDs := []int64{101, 102}
	assignmentIDs := []int64{201}
	reason := "owner requested cleanup"
	now := time.Date(2026, time.July, 27, 1, 2, 3, 0, time.UTC)
	repo, mock := newAccountShareLifecycleSQLMock(t)

	mock.ExpectBegin()
	expectLifecycleListingLock(
		mock,
		listingID,
		0,
		true,
		lifecycleLockedListingRows(
			listingID,
			ownerID,
			"lifecycle-room",
			service.AccountShareListingStatusDraining,
			oldVersion,
			func(row *lifecycleLockedListingRowData) {
				row.PendingOperationID = operationID
				row.DeleteReason = reason
				row.DeletedByUserID = ownerID
			},
		),
	)
	mock.ExpectQuery("SELECT account_id\\s+FROM account_share_room_accounts").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{"account_id"}).
			AddRow(accountIDs[0]).
			AddRow(accountIDs[1]))
	mock.ExpectQuery("SELECT id\\s+FROM accounts").
		WithArgs(pq.Array(accountIDs)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).
			AddRow(accountIDs[0]).
			AddRow(accountIDs[1]))
	mock.ExpectQuery("SELECT id\\s+FROM account_share_memberships\\s+WHERE listing_id = \\$1\\s+AND status IN").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT id\\s+FROM account_share_membership_account_bindings").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).
			AddRow(bindingIDs[0]).
			AddRow(bindingIDs[1]))
	mock.ExpectQuery("SELECT id\\s+FROM account_share_request_billing_intents").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	expectLifecycleDatabaseBlockers(mock, listingID, 0, 0, 0, 0, 0)
	mock.ExpectQuery("SELECT\\s+id::text, listing_id, membership_id").
		WithArgs(operationID).
		WillReturnRows(lifecycleOperationRows(
			operationID,
			listingID,
			ownerID,
			"owner",
			accountShareRoomOperationActionDelete,
			accountShareRoomOperationStatusPending,
			now,
			func(row *lifecycleOperationRowData) {
				row.ExpectedVersion = int64(5)
				row.StartVersion = oldVersion
			},
		))
	mock.ExpectExec("UPDATE account_share_membership_account_bindings\\s+SET unbound_at").
		WithArgs(sqlmock.AnyArg(), ownerID, "owner", pq.Array(bindingIDs)).
		WillReturnResult(sqlmock.NewResult(0, int64(len(bindingIDs))))
	mock.ExpectQuery("SELECT id\\s+FROM account_share_room_account_assignments").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(assignmentIDs[0]))
	mock.ExpectExec("UPDATE account_share_room_account_assignments\\s+SET detached_at").
		WithArgs(sqlmock.AnyArg(), ownerID, "owner", pq.Array(assignmentIDs)).
		WillReturnResult(sqlmock.NewResult(0, int64(len(assignmentIDs))))
	mock.ExpectExec("DELETE FROM account_share_room_accounts").
		WithArgs(listingID).
		WillReturnResult(sqlmock.NewResult(0, int64(len(accountIDs))))
	mock.ExpectExec("UPDATE account_share_listings\\s+SET row_version = row_version \\+ 1").
		WithArgs(sqlmock.AnyArg(), listingID, operationID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	expectLifecycleRevisionSuccess(
		mock,
		listingID,
		finalVersion,
		revisionID,
		ownerID,
		ownerID,
		false,
		"lifecycle-room",
		service.AccountShareListingStatusDraining,
		"delete_finalize",
		reason,
		"listing.delete_completed",
		operationID,
	)
	mock.ExpectExec("UPDATE account_share_listings\\s+SET deleted_at").
		WithArgs(
			sqlmock.AnyArg(),
			revisionID,
			sqlmock.AnyArg(),
			listingID,
			finalVersion,
			operationID,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	for _, accountID := range accountIDs {
		mock.ExpectExec("INSERT INTO scheduler_outbox").
			WithArgs(
				service.SchedulerOutboxEventAccountChanged,
				accountID,
				nil,
				nil,
				sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectExec("UPDATE account_share_room_operations\\s+SET status = 'succeeded'").
		WithArgs(finalVersion, sqlmock.AnyArg(), operationID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT\\s+id::text, listing_id, membership_id").
		WithArgs(operationID).
		WillReturnRows(lifecycleOperationRows(
			operationID,
			listingID,
			ownerID,
			"owner",
			accountShareRoomOperationActionDelete,
			accountShareRoomOperationStatusSucceeded,
			now,
			func(row *lifecycleOperationRowData) {
				row.ExpectedVersion = int64(5)
				row.StartVersion = oldVersion
				row.FinalVersion = finalVersion
				row.CompletedAt = now.Add(time.Second)
			},
		))
	mock.ExpectCommit()

	operation, err := repo.FinalizeRoomDeletion(context.Background(), listingID, operationID)
	if err != nil {
		t.Fatalf("FinalizeRoomDeletion failed: %v", err)
	}
	if operation.ID != operationID ||
		operation.Status != accountShareRoomOperationStatusSucceeded ||
		operation.FinalVersion == nil ||
		*operation.FinalVersion != finalVersion {
		t.Fatalf("unexpected finalized operation: %#v", operation)
	}
}

type lifecycleCapturedStringArgument struct {
	value string
}

func (argument *lifecycleCapturedStringArgument) Match(value driver.Value) bool {
	text, ok := value.(string)
	if !ok || text == "" {
		return false
	}
	if argument.value == "" {
		argument.value = text
		return true
	}
	return argument.value == text
}

type lifecycleLockedListingRowData struct {
	PendingOperationID any
	DeleteRequestID    any
	DeletedAt          any
	AccountIdentityID  any
	EditSessionID      any
	EditingExpiresAt   any
	DeleteReason       any
	DeletedByUserID    any
}

func lifecycleLockedListingRows(
	listingID int64,
	ownerUserID int64,
	roomName string,
	status string,
	rowVersion int64,
	configure ...func(*lifecycleLockedListingRowData),
) *sqlmock.Rows {
	row := &lifecycleLockedListingRowData{AccountIdentityID: int64(901)}
	for _, apply := range configure {
		if apply != nil {
			apply(row)
		}
	}
	return sqlmock.NewRows([]string{
		"id",
		"owner_user_id",
		"account_identity_id",
		"room_name",
		"status",
		"row_version",
		"pending_operation_id",
		"delete_request_id",
		"deleted_at",
		"edit_session_id",
		"editing_expires_at",
		"delete_reason",
		"deleted_by_user_id",
	}).AddRow(
		listingID,
		ownerUserID,
		row.AccountIdentityID,
		roomName,
		status,
		rowVersion,
		row.PendingOperationID,
		row.DeleteRequestID,
		row.DeletedAt,
		row.EditSessionID,
		row.EditingExpiresAt,
		row.DeleteReason,
		row.DeletedByUserID,
	)
}

type lifecycleOperationRowData struct {
	MembershipID    any
	ActorUserID     any
	ExpectedVersion any
	StartVersion    any
	FinalVersion    any
	Blocker         []byte
	Result          []byte
	ErrorCode       string
	ErrorMessage    string
	StartedAt       any
	CompletedAt     any
}

func lifecycleOperationRows(
	operationID string,
	listingID int64,
	actorUserID int64,
	actorRole string,
	action string,
	status string,
	now time.Time,
	configure ...func(*lifecycleOperationRowData),
) *sqlmock.Rows {
	row := &lifecycleOperationRowData{
		ActorUserID: actorUserID,
		Blocker:     []byte(`{}`),
		Result:      []byte(`{}`),
	}
	for _, apply := range configure {
		if apply != nil {
			apply(row)
		}
	}
	return sqlmock.NewRows([]string{
		"id",
		"listing_id",
		"membership_id",
		"actor_user_id",
		"actor_role",
		"action",
		"status",
		"expected_version",
		"start_version",
		"final_version",
		"blocker",
		"result",
		"error_code",
		"error_message",
		"created_at",
		"started_at",
		"completed_at",
		"updated_at",
	}).AddRow(
		operationID,
		listingID,
		row.MembershipID,
		row.ActorUserID,
		actorRole,
		action,
		status,
		row.ExpectedVersion,
		row.StartVersion,
		row.FinalVersion,
		row.Blocker,
		row.Result,
		row.ErrorCode,
		row.ErrorMessage,
		now,
		row.StartedAt,
		row.CompletedAt,
		now,
	)
}

func lifecycleManagementStateColumns() []string {
	return []string{
		"id",
		"room_name",
		"owner_user_id",
		"row_version",
		"status",
		"health_state",
		"status_reason_code",
		"status_reason",
		"seat_limit",
		"active_count",
		"ending_count",
		"remaining_count",
		"queued_count",
		"account_count",
		"configured_total_concurrency",
		"eligible_total_concurrency",
		"pending_billing_intent_count",
		"synchronous_billing_pending_count",
		"blocking_active_membership_count",
		"blocking_ending_membership_count",
		"valid_edit_session",
		"conflicting_operation",
		"conflicting_operation_id",
		"runtime_membership_ids",
		"runtime_account_ids",
		"deleted_at",
	}
}

func TestAccountShareModeRepositoryListValidatingRoomIDsFiltersStaleUnclaimedLiveRooms(t *testing.T) {
	repository, mock := newAccountShareLifecycleSQLMock(t)
	staleBefore := time.Date(
		2026,
		time.July,
		27,
		8,
		30,
		0,
		0,
		time.FixedZone("UTC+8", 8*60*60),
	)
	mock.ExpectQuery(
		"SELECT id\\s+"+
			"FROM account_share_listings\\s+"+
			"WHERE status = 'validating'\\s+"+
			"AND pending_operation_id IS NULL\\s+"+
			"AND deleted_at IS NULL\\s+"+
			"AND updated_at <= \\$1\\s+"+
			"ORDER BY updated_at ASC, id ASC\\s+"+
			"LIMIT \\$2",
	).
		WithArgs(staleBefore.UTC(), 5).
		WillReturnRows(
			sqlmock.NewRows([]string{"id"}).
				AddRow(int64(9)).
				AddRow(int64(11)),
		)

	listingIDs, err := repository.ListValidatingRoomIDs(
		context.Background(),
		staleBefore,
		5,
	)

	if err != nil {
		t.Fatalf("ListValidatingRoomIDs failed: %v", err)
	}
	if !reflect.DeepEqual(listingIDs, []int64{9, 11}) {
		t.Fatalf("listing ids = %v, want [9 11]", listingIDs)
	}
}

func newAccountShareLifecycleSQLMock(t *testing.T) (*accountShareModeRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet sqlmock expectations: %v", err)
		}
	})
	return &accountShareModeRepository{db: db}, mock
}

func expectLifecycleListingLock(
	mock sqlmock.Sqlmock,
	listingID int64,
	actorUserID int64,
	actorIsAdmin bool,
	rows *sqlmock.Rows,
) {
	mock.ExpectQuery("SELECT\\s+id,\\s+owner_user_id,\\s+account_identity_id,\\s+COALESCE\\(room_name, ''\\)").
		WithArgs(listingID, actorIsAdmin, actorUserID).
		WillReturnRows(rows)
}

func expectLifecycleDatabaseBlockers(
	mock sqlmock.Sqlmock,
	listingID int64,
	active int,
	queued int,
	ending int,
	synchronousBilling int,
	pendingBillingIntents int,
) {
	mock.ExpectQuery("SELECT\\s+COUNT\\(\\*\\) FILTER \\(WHERE status = 'active'\\)::int").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{
			"active_count",
			"queued_count",
			"ending_count",
			"synchronous_billing_pending_count",
		}).AddRow(active, queued, ending, synchronousBilling))
	mock.ExpectQuery("SELECT COUNT\\(\\*\\)::int\\s+FROM account_share_request_billing_intents").
		WithArgs(listingID).
		WillReturnRows(sqlmock.NewRows([]string{"pending_count"}).AddRow(pendingBillingIntents))
}

func expectLifecycleRevisionSuccess(
	mock sqlmock.Sqlmock,
	listingID int64,
	rowVersion int64,
	revisionID int64,
	ownerUserID int64,
	actorUserID int64,
	actorIsAdmin bool,
	roomName string,
	status string,
	source string,
	reason string,
	eventType string,
	operationID any,
) {
	mock.ExpectQuery("SELECT\\s+l\\.id, l\\.row_version").
		WithArgs(listingID).
		WillReturnRows(accountShareRevisionSnapshotRows(
			listingID,
			rowVersion,
			roomName,
			ownerUserID,
			"owner",
			func(row *accountShareRevisionSourceRowData) {
				row.Status = status
			},
		))
	mock.ExpectQuery("INSERT INTO account_share_listing_revisions").
		WithArgs(
			listingID,
			rowVersion,
			1,
			service.AccountShareSnapshotQualityExact,
			roomName,
			service.PlatformOpenAI,
			"pro",
			ownerUserID,
			"owner",
			status,
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
			nullablePositiveInt64(actorUserID),
			accountShareRevisionActorRole(actorUserID, actorIsAdmin),
			source,
			nullableEmptyString(reason),
			operationID,
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
			eventType,
			nullablePositiveInt64(actorUserID),
			accountShareRevisionActorRole(actorUserID, actorIsAdmin),
			nullableEmptyString(reason),
			sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
}
