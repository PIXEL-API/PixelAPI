package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestHydrateAccountMutationBindingsFromPrelockedCopiesRoomVersion(t *testing.T) {
	revisionID := int64(91)
	prelocked := []accountMutationRoomBinding{{
		accountID:       7,
		listingID:       11,
		rowVersion:      4,
		revisionID:      &revisionID,
		lifecycleStatus: service.AccountShareListingStatusPaused,
		blockers: service.AccountShareRoomBlockers{
			PendingBillingIntentCount: 2,
		},
		openBindingCount: 3,
	}}
	current := []accountMutationRoomBinding{{
		accountID: 7,
		listingID: 11,
	}}

	if err := hydrateAccountMutationBindingsFromPrelocked(prelocked, current); err != nil {
		t.Fatalf("hydrateAccountMutationBindingsFromPrelocked: %v", err)
	}
	if current[0].rowVersion != 4 {
		t.Fatalf("row version = %d, want 4", current[0].rowVersion)
	}
	if current[0].revisionID == nil || *current[0].revisionID != revisionID {
		t.Fatalf("revision = %v, want %d", current[0].revisionID, revisionID)
	}
	require.Equal(t, service.AccountShareListingStatusPaused, current[0].lifecycleStatus)
	require.Equal(t, 2, current[0].blockers.PendingBillingIntentCount)
	require.Equal(t, 3, current[0].openBindingCount)
}

func TestHydrateAccountMutationBindingsFromPrelockedRejectsNewRoomBinding(t *testing.T) {
	prelocked := []accountMutationRoomBinding{{
		accountID:  7,
		listingID:  11,
		rowVersion: 4,
	}}
	current := []accountMutationRoomBinding{{
		accountID: 7,
		listingID: 12,
	}}

	err := hydrateAccountMutationBindingsFromPrelocked(prelocked, current)
	if !errors.Is(err, service.ErrAccountMutationStale) {
		t.Fatalf("expected new room binding to fail fast as stale, got %v", err)
	}
}

func TestLockAndHydrateAccountMutationRoomsLoadsPersistentSafetyState(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`(?s)SELECT\s+id,\s+row_version,\s+current_revision_id,\s+status,.*FROM account_share_listings.*FOR UPDATE`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"row_version",
			"current_revision_id",
			"status",
			"valid_edit_session",
			"conflicting_operation",
			"pending_operation_id",
		}).AddRow(int64(11), int64(4), int64(91), service.AccountShareListingStatusPaused, false, false, ""))
	mock.ExpectQuery(`(?s)WITH membership_blockers AS.*billing_blockers AS.*binding_blockers AS.*ORDER BY listing.id`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{
			"listing_id",
			"active_count",
			"queued_count",
			"ending_count",
			"settlement_count",
			"pending_count",
			"open_count",
		}).AddRow(int64(11), 0, 0, 0, 0, 0, 0))

	bindings := []accountMutationRoomBinding{{accountID: 7, listingID: 11}}
	err = lockAndHydrateAccountMutationRooms(context.Background(), db, bindings)

	require.NoError(t, err)
	require.Equal(t, int64(4), bindings[0].rowVersion)
	require.NotNil(t, bindings[0].revisionID)
	require.Equal(t, int64(91), *bindings[0].revisionID)
	require.Equal(t, service.AccountShareListingStatusPaused, bindings[0].lifecycleStatus)
	require.False(t, bindings[0].blockers.Any())
	require.Zero(t, bindings[0].openBindingCount)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLockAndHydrateAccountMutationRoomsFailsClosedWhenBlockerQueryFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`(?s)SELECT\s+id,\s+row_version,\s+current_revision_id,\s+status,.*FROM account_share_listings.*FOR UPDATE`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{
			"id",
			"row_version",
			"current_revision_id",
			"status",
			"valid_edit_session",
			"conflicting_operation",
			"pending_operation_id",
		}).AddRow(int64(11), int64(4), nil, service.AccountShareListingStatusPaused, false, false, ""))
	mock.ExpectQuery(`(?s)WITH membership_blockers AS.*billing_blockers AS.*binding_blockers AS`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnError(errors.New("blocker query unavailable"))

	err = lockAndHydrateAccountMutationRooms(
		context.Background(),
		db,
		[]accountMutationRoomBinding{{accountID: 7, listingID: 11}},
	)

	require.ErrorIs(t, err, service.ErrAccountMutationGuardUnavailable)
	appErr := infraerrors.FromError(err)
	require.Equal(t, "room_blockers", appErr.Metadata["stage"])
	require.NoError(t, mock.ExpectationsWereMet())
}

// ---------------------------------------------------------------------------
// 广场公共池投放（无 listing）的守卫。
//
// 房间账号通过 account_share_room_accounts 天然进得了守卫；公共池账号没有任何
// listing，此前完全不在守卫覆盖范围内，只能靠 service 层一道粗糙的前置检查一刀切
// 拒绝。现在它们走同一套判定，差别只在角色：房主自助照旧放行，管理员改别人的号
// 需要刻意确认并留审计。
// ---------------------------------------------------------------------------

func accountMutationGuardPublicPoolPlacements(accountID int64) []accountMutationPlacementBinding {
	return []accountMutationPlacementBinding{{
		accountID:     accountID,
		placementType: service.AccountExternalPlacementPublicPool,
		version:       3,
	}}
}

func TestAuthorizePublicPoolPlacementAllowsOwnerSelfService(t *testing.T) {
	targets := accountMutationGuardSensitiveTargets(7)
	request := service.AccountMutationGuardRequest{
		ActorUserID: 42,
		Intent:      service.AccountMutationIntentOwner,
	}

	// 房主改自己的号是正常自助行为：改完系统会把公共池账号自动打回 pending 重验，
	// 这条链路本身就是安全的。额外设卡会让用户连自己的号都动不了。
	require.NoError(t, authorizeAccountMutation(request, targets, nil, accountMutationGuardPublicPoolPlacements(7)))
}

func TestAuthorizePublicPoolPlacementAllowsSystemTokenRefresh(t *testing.T) {
	targets := accountMutationGuardSensitiveTargets(7)
	request := service.AccountMutationGuardRequest{
		Intent: service.AccountMutationIntentSystemTokenRefresh,
	}

	require.NoError(t, authorizeAccountMutation(request, targets, nil, accountMutationGuardPublicPoolPlacements(7)))
}

func TestAuthorizePublicPoolPlacementRequiresAdminForceConfirmAndReason(t *testing.T) {
	targets := accountMutationGuardSensitiveTargets(7)
	placements := accountMutationGuardPublicPoolPlacements(7)
	valid := service.AccountMutationGuardRequest{
		ActorUserID:     99,
		ActorIsAdmin:    true,
		Intent:          service.AccountMutationIntentAdmin,
		ForceActiveEdit: true,
		Confirmed:       true,
		Reason:          "上游账号被封，更换凭证",
	}

	require.NoError(t, authorizeAccountMutation(valid, targets, nil, placements))

	tests := []struct {
		name    string
		mutate  func(*service.AccountMutationGuardRequest)
		missing string
	}{
		{"missing force", func(r *service.AccountMutationGuardRequest) { r.ForceActiveEdit = false }, "force_active_edit"},
		{"missing confirmation", func(r *service.AccountMutationGuardRequest) { r.Confirmed = false }, "confirmed"},
		{"blank reason", func(r *service.AccountMutationGuardRequest) { r.Reason = "   " }, "reason"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			test.mutate(&request)

			err := authorizeAccountMutation(request, targets, nil, placements)

			require.ErrorIs(t, err, service.ErrAccountMutationForceRequired)
			appErr := infraerrors.FromError(err)
			require.Equal(t, test.missing, appErr.Metadata["missing"])
			require.Equal(t, "7", appErr.Metadata["account_ids"])
			require.Equal(t, service.AccountExternalPlacementPublicPool, appErr.Metadata["placement_target"])
			require.Equal(t, "credentials", appErr.Metadata["changed_fields"])
		})
	}
}

// 公共池投放不做 expected_version 校验：守卫已经用 ExpectedUpdatedAt 对账号行做了
// 乐观并发控制，而任何一次投放转换都会 UPDATE accounts 推进 updated_at。
// 再要求一个投放版本号只会让管理端多背一个无用参数。
func TestAuthorizePublicPoolPlacementDoesNotRequireExpectedVersion(t *testing.T) {
	targets := accountMutationGuardSensitiveTargets(7)
	request := service.AccountMutationGuardRequest{
		ActorUserID:     99,
		ActorIsAdmin:    true,
		Intent:          service.AccountMutationIntentAdmin,
		ForceActiveEdit: true,
		Confirmed:       true,
		Reason:          "risk review",
	}

	require.NoError(t, authorizeAccountMutation(request, targets, nil, accountMutationGuardPublicPoolPlacements(7)))
}

// 只改模型映射的账号不算敏感变更，公共池守卫不该要求填理由。
func TestAuthorizePublicPoolPlacementSkipsNonForceableTargets(t *testing.T) {
	diff := service.AccountMutationDiff{
		Sensitive:             true,
		ChangedFields:         []string{"credentials"},
		SensitiveFields:       []string{"credentials"},
		CredentialChangedKeys: []string{"model_mapping"},
	}
	targets := map[int64]*accountMutationLockedTarget{
		7: {diff: diff, impact: service.ClassifyAccountPlacementImpact(diff)},
	}
	request := service.AccountMutationGuardRequest{
		ActorUserID:  99,
		ActorIsAdmin: true,
		Intent:       service.AccountMutationIntentAdmin,
	}

	require.NoError(t, authorizeAccountMutation(request, targets, nil, accountMutationGuardPublicPoolPlacements(7)))
}

func TestAuthorizeAccountMutationOwnerAllowsOnlyPausedDrainedRooms(t *testing.T) {
	targets := accountMutationGuardSensitiveTargets(7)
	request := service.AccountMutationGuardRequest{
		ActorUserID: 42,
		Intent:      service.AccountMutationIntentOwner,
	}
	bindings := []accountMutationRoomBinding{{
		accountID:       7,
		listingID:       11,
		rowVersion:      4,
		lifecycleStatus: service.AccountShareListingStatusPaused,
	}}

	require.NoError(t, authorizeAccountMutation(request, targets, bindings, nil))
}

func TestAuthorizeAccountMutationOwnerRejectsNonPausedRoomLifecycle(t *testing.T) {
	targets := accountMutationGuardSensitiveTargets(7)
	request := service.AccountMutationGuardRequest{
		ActorUserID: 42,
		Intent:      service.AccountMutationIntentOwner,
	}
	statuses := []string{
		service.AccountShareListingStatusValidating,
		service.AccountShareListingStatusActive,
		service.AccountShareListingStatusDraining,
		service.AccountShareListingStatusSuspended,
	}
	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			err := authorizeAccountMutation(request, targets, []accountMutationRoomBinding{{
				accountID:       7,
				listingID:       11,
				rowVersion:      4,
				lifecycleStatus: status,
			}}, nil)

			require.ErrorIs(t, err, service.ErrAccountMutationBlocked)
			appErr := infraerrors.FromError(err)
			require.Equal(t, status, appErr.Metadata["lifecycle_status"])
			require.Equal(t, "11", appErr.Metadata["listing_id"])
		})
	}
}

func TestAuthorizeAccountMutationOwnerRequiresEveryAssociatedRoomToBeSafe(t *testing.T) {
	targets := accountMutationGuardSensitiveTargets(7)
	request := service.AccountMutationGuardRequest{
		ActorUserID: 42,
		Intent:      service.AccountMutationIntentOwner,
	}
	err := authorizeAccountMutation(request, targets, []accountMutationRoomBinding{
		{
			accountID:       7,
			listingID:       11,
			rowVersion:      4,
			lifecycleStatus: service.AccountShareListingStatusPaused,
		},
		{
			accountID:       7,
			listingID:       12,
			rowVersion:      8,
			lifecycleStatus: service.AccountShareListingStatusDraining,
		},
	}, nil)

	require.ErrorIs(t, err, service.ErrAccountMutationBlocked)
	appErr := infraerrors.FromError(err)
	require.Equal(t, "11,12", appErr.Metadata["listing_ids"])
	require.Equal(t, "12", appErr.Metadata["listing_id"])
	require.Equal(t, service.AccountShareListingStatusDraining, appErr.Metadata["lifecycle_status"])
}

func TestAuthorizeAccountMutationOwnerRejectsPausedRoomPersistentBlockers(t *testing.T) {
	targets := accountMutationGuardSensitiveTargets(7)
	request := service.AccountMutationGuardRequest{
		ActorUserID: 42,
		Intent:      service.AccountMutationIntentOwner,
	}
	tests := []struct {
		name             string
		blockers         service.AccountShareRoomBlockers
		openBindingCount int
		metadataKey      string
	}{
		{
			name:        "active membership",
			blockers:    service.AccountShareRoomBlockers{ActiveMembershipCount: 1},
			metadataKey: "active_membership_count",
		},
		{
			name:        "queued membership",
			blockers:    service.AccountShareRoomBlockers{QueuedMembershipCount: 1},
			metadataKey: "queued_membership_count",
		},
		{
			name:        "ending membership",
			blockers:    service.AccountShareRoomBlockers{EndingMembershipCount: 1},
			metadataKey: "ending_membership_count",
		},
		{
			name:        "synchronous settlement",
			blockers:    service.AccountShareRoomBlockers{SynchronousBillingPendingCount: 1},
			metadataKey: "synchronous_billing_pending_count",
		},
		{
			name:        "dispatch billing intent",
			blockers:    service.AccountShareRoomBlockers{PendingBillingIntentCount: 1},
			metadataKey: "pending_billing_intent_count",
		},
		{
			name:             "open dispatch binding",
			openBindingCount: 1,
			metadataKey:      "open_binding_count",
		},
		{
			name:        "valid edit session",
			blockers:    service.AccountShareRoomBlockers{ValidEditSession: true},
			metadataKey: "valid_edit_session",
		},
		{
			name: "conflicting operation",
			blockers: service.AccountShareRoomBlockers{
				ConflictingOperation:   true,
				ConflictingOperationID: "4c80deef-5a1b-4faf-9d39-b3b2ef5463e0",
			},
			metadataKey: "conflicting_operation",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := authorizeAccountMutation(request, targets, []accountMutationRoomBinding{{
				accountID:        7,
				listingID:        11,
				rowVersion:       4,
				lifecycleStatus:  service.AccountShareListingStatusPaused,
				blockers:         test.blockers,
				openBindingCount: test.openBindingCount,
			}}, nil)

			require.ErrorIs(t, err, service.ErrAccountMutationBlocked)
			appErr := infraerrors.FromError(err)
			require.NotEqual(t, "", appErr.Metadata[test.metadataKey])
			require.Equal(t, "11", appErr.Metadata["listing_id"])
		})
	}
}

func TestAuthorizeAccountMutationAdminForceContractIsUnchanged(t *testing.T) {
	targets := accountMutationGuardSensitiveTargets(7)
	version := int64(4)
	bindings := []accountMutationRoomBinding{{
		accountID:       7,
		listingID:       11,
		rowVersion:      version,
		lifecycleStatus: service.AccountShareListingStatusActive,
		blockers: service.AccountShareRoomBlockers{
			ActiveMembershipCount:     1,
			PendingBillingIntentCount: 1,
		},
		openBindingCount: 1,
	}}
	valid := service.AccountMutationGuardRequest{
		ActorUserID:            99,
		ActorIsAdmin:           true,
		Intent:                 service.AccountMutationIntentAdmin,
		ForceActiveEdit:        true,
		Confirmed:              true,
		Reason:                 "risk review",
		ExpectedListingVersion: &version,
	}

	require.NoError(t, authorizeAccountMutation(valid, targets, bindings, nil))

	tests := []struct {
		name   string
		mutate func(*service.AccountMutationGuardRequest)
	}{
		{
			name: "force required",
			mutate: func(request *service.AccountMutationGuardRequest) {
				request.ForceActiveEdit = false
			},
		},
		{
			name: "confirmation required",
			mutate: func(request *service.AccountMutationGuardRequest) {
				request.Confirmed = false
			},
		},
		{
			name: "reason required",
			mutate: func(request *service.AccountMutationGuardRequest) {
				request.Reason = " "
			},
		},
		{
			name: "expected version required",
			mutate: func(request *service.AccountMutationGuardRequest) {
				request.ExpectedListingVersion = nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			test.mutate(&request)
			require.ErrorIs(t, authorizeAccountMutation(request, targets, bindings, nil), service.ErrAccountMutationForceRequired)
		})
	}

	staleVersion := version - 1
	stale := valid
	stale.ExpectedListingVersion = &staleVersion
	require.ErrorIs(t, authorizeAccountMutation(stale, targets, bindings, nil), service.ErrAccountMutationVersionConflict)
}

func accountMutationGuardSensitiveTargets(accountID int64) map[int64]*accountMutationLockedTarget {
	diff := service.AccountMutationDiff{
		Sensitive:             true,
		ChangedFields:         []string{"credentials"},
		SensitiveFields:       []string{"credentials"},
		CredentialChangedKeys: []string{"access_token"},
	}
	return map[int64]*accountMutationLockedTarget{
		accountID: {
			diff:   diff,
			impact: service.ClassifyAccountPlacementImpact(diff),
		},
	}
}
