package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type lockedAccountExternalPlacement struct {
	Target        string
	RoomID        *int64
	RoomName      string
	PublicGroupID *int64
	State         string
	Version       int64
	UpdatedAt     time.Time
}

type lockedAccountShareRoom struct {
	ID                  int64
	OwnerUserID         int64
	Platform            string
	AccountLevel        string
	Status              string
	PendingOperationID  sql.NullString
	AllowedModels       []string
	CodexCLIOnly        bool
	Codex5hLimitPercent float64
	Codex7dLimitPercent float64
}

type accountShareRoomAssignmentSnapshot struct {
	ListingID             int64
	AccountID             int64
	OwnerUserID           int64
	AccountName           string
	Platform              string
	AccountLevel          string
	ConfiguredConcurrency int
}

type accountShareRoomAccountCandidate struct {
	Snapshot           accountShareRoomAssignmentSnapshot
	Priority           int
	Status             string
	UnavailableBlocker string
	AccountType        string
	Credentials        map[string]any
	Extra              map[string]any
}

type accountShareRoomAccountProjection struct {
	ListingID int64
	State     string
	CreatedAt time.Time
}

type accountShareRoomOpenAssignment struct {
	ID        int64
	ListingID int64
}

type accountShareMembershipRebindState struct {
	ID                int64
	ListingID         int64
	AccountID         int64
	ListingRevisionID int64
}

type accountShareMembershipOpenBinding struct {
	ID                int64
	MembershipID      int64
	ListingID         int64
	AccountIDSnapshot int64
	ListingRevisionID int64
}

const (
	accountExternalPlacementDrainLease = 2 * time.Minute

	accountShareBindingReasonAccountRebind                = "account_rebind"
	accountShareBindingReasonLegacyProjectionMaterialized = "legacy_projection_materialized"
	accountShareRoomStatusReasonNoAccounts                = "no_room_accounts"
	accountShareRoomStatusMessageNoAccounts               = "房间已无可用账号，已自动暂停"
)

var _ service.AccountShareRuntimeBindingRepository = (*accountShareModeRepository)(nil)

type accountShareRoomQueryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (r *accountShareModeRepository) FindRoomCreationByIdempotency(
	ctx context.Context,
	ownerUserID, accountID int64,
	idempotencyKey string,
	listing *service.AccountShareListing,
) (*service.AccountShareListing, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if r == nil || r.db == nil || ownerUserID <= 0 || accountID <= 0 || listing == nil || idempotencyKey == "" {
		return nil, service.ErrAccountNilInput
	}
	allowedModelsJSON, err := json.Marshal(listing.AllowedModels)
	if err != nil {
		return nil, err
	}
	listingID, err := getIdempotentRoomCreation(
		ctx,
		r.db,
		ownerUserID,
		accountID,
		idempotencyKey,
		strings.TrimSpace(listing.RoomName),
		listing,
		string(allowedModelsJSON),
	)
	if err != nil || listingID <= 0 {
		return nil, err
	}
	return r.GetListingByID(ctx, listingID, ownerUserID)
}

func (r *accountShareModeRepository) CreateRoomFromOwnedAccount(ctx context.Context, ownerUserID, accountID, modeGroupID int64, idempotencyKey string, listing *service.AccountShareListing) (*service.AccountShareListing, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if ownerUserID <= 0 || accountID <= 0 || modeGroupID <= 0 || listing == nil || idempotencyKey == "" || len(idempotencyKey) > 128 {
		return nil, service.ErrAccountNilInput
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	if err := lockAccountShareOwnerQuotaInTx(ctx, tx, ownerUserID); err != nil {
		return nil, err
	}

	var accountName, platform, accountLevel, accountStatus string
	var accountSchedulable bool
	var accountConcurrency, accountPriority int
	var accountCredentialsRaw, accountExtraRaw []byte
	if err := tx.QueryRowContext(ctx, `
		SELECT
			name,
			platform,
			account_level,
			status,
			NOT `+accountShareAccountUnavailableConditionSQL("NOW()")+` AS schedulable,
			concurrency,
			priority,
			credentials,
			extra
		FROM accounts a
		WHERE a.id = $1
			AND a.owner_user_id = $2
			AND a.deleted_at IS NULL
		FOR UPDATE
	`, accountID, ownerUserID).Scan(
		&accountName,
		&platform,
		&accountLevel,
		&accountStatus,
		&accountSchedulable,
		&accountConcurrency,
		&accountPriority,
		&accountCredentialsRaw,
		&accountExtraRaw,
	); errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareRoomOwnerMismatch
	} else if err != nil {
		return nil, err
	}
	platform = strings.ToLower(strings.TrimSpace(platform))
	roomName := strings.TrimSpace(listing.RoomName)
	if roomName == "" {
		return nil, service.ErrAccountShareModeInvalidName
	}
	allowedModelsJSON, err := json.Marshal(listing.AllowedModels)
	if err != nil {
		return nil, err
	}
	idempotentListingID, err := getIdempotentRoomCreation(
		ctx,
		tx,
		ownerUserID,
		accountID,
		idempotencyKey,
		roomName,
		listing,
		string(allowedModelsJSON),
	)
	if err != nil {
		return nil, err
	}
	if idempotentListingID > 0 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		tx = nil
		return r.GetListingByID(ctx, idempotentListingID, ownerUserID)
	}
	accountLevel = service.NormalizeAccountLevel(accountLevel)
	if accountLevel == service.AccountLevelUnknown && platform != service.PlatformOpencode {
		return nil, service.ErrAccountShareRoomUnknownLevel
	}
	if accountStatus != service.StatusActive || !accountSchedulable {
		return nil, service.ErrAccountShareAccountUnavailable
	}
	accountCredentials, err := unmarshalAccountShareJSONMap(accountCredentialsRaw)
	if err != nil {
		return nil, err
	}
	accountExtra, err := unmarshalAccountShareJSONMap(accountExtraRaw)
	if err != nil {
		return nil, err
	}
	if listing.Platform != "" && !strings.EqualFold(strings.TrimSpace(listing.Platform), platform) {
		return nil, service.ErrAccountShareRoomPlatformMismatch
	}
	if listing.AccountLevel != "" && service.NormalizeAccountLevel(listing.AccountLevel) != accountLevel {
		return nil, service.ErrAccountShareRoomLevelMismatch
	}
	if err := r.enforceAccountShareRoomCreationQuotaInTx(ctx, tx, ownerUserID); err != nil {
		return nil, err
	}
	var duplicateRoom bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM account_share_listings
			WHERE owner_user_id = $1
				AND LOWER(BTRIM(room_name)) = LOWER(BTRIM($2))
				AND deleted_at IS NULL
		)
	`, ownerUserID, roomName).Scan(&duplicateRoom); err != nil {
		return nil, err
	}
	if duplicateRoom {
		return nil, service.ErrAccountShareModeDuplicateName
	}

	if err := validateAccountShareModeGroupInTx(ctx, tx, modeGroupID, platform); err != nil {
		return nil, err
	}
	previousPlacement, placementVersion, err := r.prepareAccountForRoomCreationInTx(
		ctx,
		tx,
		ownerUserID,
		accountID,
		modeGroupID,
		platform,
		accountLevel,
		accountPriority,
	)
	if err != nil {
		return nil, err
	}

	account := &service.Account{
		ID:           accountID,
		Name:         accountName,
		Platform:     platform,
		AccountLevel: accountLevel,
		OwnerUserID:  &ownerUserID,
		Credentials:  accountCredentials,
		Extra:        accountExtra,
	}
	accountIdentityID, err := ensureAccountShareAccountIdentityInTx(ctx, tx, account)
	if err != nil {
		return nil, err
	}
	listingStatus := strings.ToLower(strings.TrimSpace(listing.Status))
	if listingStatus == "" {
		listingStatus = service.AccountShareListingStatusValidating
	}
	var listingID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO account_share_listings (
			owner_user_id, room_name, platform, account_level,
			status, seat_limit, rate_multiplier, allowed_models,
			per_user_concurrency, hourly_rate, hourly_fee_waiver_minimum,
			min_balance_required, codex_cli_only, codex_5h_limit_percent,
			codex_7d_limit_percent, account_identity_id, created_at, updated_at
		)
		VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8::jsonb,
			$9, $10, $11,
			$12, $13, $14,
			$15, $16, NOW(), NOW()
		)
		RETURNING id
	`,
		ownerUserID,
		roomName,
		platform,
		accountLevel,
		listingStatus,
		listing.SeatLimit,
		listing.RateMultiplier,
		string(allowedModelsJSON),
		listing.PerUserConcurrency,
		listing.HourlyRate,
		listing.HourlyFeeWaiverMinimum,
		listing.MinBalanceRequired,
		listing.CodexCLIOnly,
		listing.Codex5hLimitPercent,
		listing.Codex7dLimitPercent,
		nullableInt64(accountIdentityID),
	).Scan(&listingID)
	if err != nil {
		return nil, translateAccountShareRoomPersistenceError(err)
	}
	if _, _, err := createAccountShareListingRevisionInTx(
		ctx,
		tx,
		listingID,
		ownerUserID,
		false,
		"create_room",
		"",
		false,
		"listing.created",
		map[string]any{"mode_group_id": modeGroupID},
	); err != nil {
		return nil, err
	}

	if err := insertAccountShareRoomProjectionAndAssignmentInTx(
		ctx,
		tx,
		accountShareRoomAssignmentSnapshot{
			ListingID:             listingID,
			AccountID:             accountID,
			OwnerUserID:           ownerUserID,
			AccountName:           accountName,
			Platform:              platform,
			AccountLevel:          accountLevel,
			ConfiguredConcurrency: accountConcurrency,
		},
		accountPriority,
		ownerUserID,
		"owner",
		"room_created",
	); err != nil {
		return nil, err
	}
	conversionResult := &service.ConvertAccountExternalPlacementResult{
		AccountID: accountID,
		Previous:  previousPlacement,
		Current: &service.AccountExternalPlacement{
			Target:   service.AccountExternalPlacementRoom,
			RoomID:   &listingID,
			RoomName: roomName,
			State:    "active",
			Version:  placementVersion,
		},
	}
	resultJSON, err := json.Marshal(conversionResult)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO account_external_placement_conversions (
			owner_user_id, account_id, idempotency_key, target_type,
			target_listing_id, target_public_group_id, placement_version,
			result, created_at
		)
		VALUES ($1, $2, $3, 'room', $4, NULL, $5, $6::jsonb, NOW())
	`, ownerUserID, accountID, idempotencyKey, listingID, placementVersion, string(resultJSON)); err != nil {
		return nil, translateAccountExternalPlacementConversionError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return r.GetListingByID(ctx, listingID, ownerUserID)
}

func (r *accountShareModeRepository) prepareAccountForRoomCreationInTx(
	ctx context.Context,
	tx *sql.Tx,
	ownerUserID, accountID, modeGroupID int64,
	platform, accountLevel string,
	accountPriority int,
) (*service.AccountExternalPlacement, int64, error) {
	current, err := getAccountExternalPlacementInTx(ctx, tx, accountID, ownerUserID, true)
	if err != nil {
		return nil, 0, err
	}
	if current != nil {
		switch current.Target {
		case service.AccountExternalPlacementRoom:
			if current.State != "active" {
				return nil, 0, service.ErrAccountExternalPlacementBusy
			}
			if current.RoomID != nil && *current.RoomID > 0 {
				return nil, 0, service.ErrAccountExternalPlacementConflict
			}
			projections, projectionErr := lockAccountShareRoomAccountProjectionsInTx(
				ctx,
				tx,
				[]int64{accountID},
			)
			if projectionErr != nil {
				return nil, 0, projectionErr
			}
			if _, attached := projections[accountID]; attached {
				return nil, 0, service.ErrAccountExternalPlacementConflict
			}
		case service.AccountExternalPlacementPublicPool:
			if current.State != "draining" {
				return nil, 0, service.ErrAccountExternalPlacementBusy
			}
		default:
			return nil, 0, service.ErrAccountExternalPlacementBusy
		}
	}

	privateGroupID, err := accountOwnerPrivateGroupIDInTx(ctx, tx, ownerUserID, platform)
	if err != nil {
		return nil, 0, err
	}
	previousVersion, err := currentAccountExternalPlacementVersionInTx(ctx, tx, accountID, current)
	if err != nil {
		return nil, 0, err
	}
	previousPlacement := placementToService(current)
	if previousPlacement == nil {
		previousPlacement = privateAccountExternalPlacement(previousVersion)
	}
	version := previousVersion + 1
	groupIDs := []int64{privateGroupID, modeGroupID}
	if err := replaceAccountGroupsInTx(ctx, tx, accountID, groupIDs); err != nil {
		return nil, 0, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE accounts
		SET share_mode = $1,
			share_status = $2,
			updated_at = NOW()
		WHERE id = $3
			AND owner_user_id = $4
			AND deleted_at IS NULL
	`, service.AccountShareModePrivate, service.AccountShareStatusApproved, accountID, ownerUserID); err != nil {
		return nil, 0, err
	}
	if err := writeAccountExternalPlacementTargetInTx(
		ctx,
		tx,
		service.ConvertAccountExternalPlacementInput{
			AccountID:   accountID,
			OwnerUserID: ownerUserID,
			Target:      service.AccountExternalPlacementRoom,
		},
		service.AccountExternalPlacementRoom,
		platform,
		accountLevel,
		accountPriority,
		version,
	); err != nil {
		return nil, 0, err
	}
	if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account_share_room", "[SchedulerOutbox] enqueue room creation account change failed: account=%d err=%v", accountID, err)
	}
	if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountGroupsChanged, &accountID, nil, buildSchedulerGroupPayload(groupIDs)); err != nil {
		logger.LegacyPrintf("repository.account_share_room", "[SchedulerOutbox] enqueue room creation group change failed: account=%d err=%v", accountID, err)
	}
	return previousPlacement, version, nil
}

func (r *accountShareModeRepository) ListRoomAccounts(ctx context.Context, listingID, viewerUserID int64, viewerIsAdmin bool) ([]service.AccountShareRoomAccount, error) {
	if listingID <= 0 || viewerUserID <= 0 {
		return nil, service.ErrAccountShareListingNotFound
	}
	var ownerUserID int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT owner_user_id
		FROM account_share_listings
		WHERE id = $1
			AND deleted_at IS NULL
	`, listingID).Scan(&ownerUserID); errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	} else if err != nil {
		return nil, err
	}
	if ownerUserID != viewerUserID && !viewerIsAdmin {
		return nil, service.ErrInsufficientPerms
	}
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			a.id,
			a.name,
			a.platform,
			a.account_level,
			a.status,
			NOT %s AS schedulable,
			a.concurrency,
			room_account.priority,
			room_account.state,
			a.last_used_at
		FROM account_share_room_accounts room_account
		JOIN accounts a ON a.id = room_account.account_id
		WHERE room_account.listing_id = $1
			AND a.deleted_at IS NULL
		ORDER BY room_account.priority ASC, a.id ASC
	`, accountShareAccountUnavailableConditionSQL("$2")), listingID, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.AccountShareRoomAccount, 0)
	for rows.Next() {
		var item service.AccountShareRoomAccount
		var lastUsedAt sql.NullTime
		if err := rows.Scan(
			&item.AccountID,
			&item.AccountName,
			&item.Platform,
			&item.AccountLevel,
			&item.Status,
			&item.Schedulable,
			&item.CurrentConcurrency,
			&item.Priority,
			&item.PlacementState,
			&lastUsedAt,
		); err != nil {
			return nil, err
		}
		item.AccountLevel = service.NormalizeAccountLevel(item.AccountLevel)
		item.LastUsedAt = sqlNullTimePtr(lastUsedAt)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *accountShareModeRepository) AttachRoomAccountsAtomic(
	ctx context.Context,
	input service.BatchAccountShareRoomAccountsInput,
) (*service.BulkUpdateAccountsResult, error) {
	accountIDs, err := normalizeAccountShareRoomBatchInput(input)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if err := lockAccountShareOwnerQuotaInTx(ctx, tx, input.OwnerUserID); err != nil {
		return nil, err
	}
	room, err := lockAccountShareRoomIdentityInTx(
		ctx,
		tx,
		input.ListingID,
	)
	if err != nil {
		return nil, err
	}
	if room.OwnerUserID != input.OwnerUserID {
		return nil, service.ErrAccountShareRoomOwnerMismatch
	}
	if room.Status != service.AccountShareListingStatusActive &&
		room.Status != service.AccountShareListingStatusPaused {
		return nil, service.ErrAccountShareRoomOperationConflict.WithMetadata(map[string]string{
			"blocker": "room_not_attachable",
			"status":  room.Status,
		})
	}

	candidates, err := lockAccountShareRoomAccountCandidatesInTx(
		ctx,
		tx,
		input.OwnerUserID,
		input.ListingID,
		accountIDs,
		false,
	)
	if err != nil {
		return nil, err
	}
	if len(candidates) != len(accountIDs) {
		// Missing, deleted, or foreign accounts stay request-level failures to avoid
		// exposing whether an arbitrary account ID exists.
		return nil, service.ErrAccountShareRoomOwnerMismatch
	}
	roomPlatform := strings.ToLower(strings.TrimSpace(room.Platform))
	roomAccountLevel := service.NormalizeAccountLevel(room.AccountLevel)
	if roomAccountLevel == service.AccountLevelUnknown && roomPlatform != service.PlatformOpencode {
		return nil, service.ErrAccountShareRoomUnknownLevel
	}

	failedByID := make(map[int64]service.BulkUpdateAccountResult, len(accountIDs))
	recordFailure := func(accountID int64, cause error, metadata map[string]string) {
		if _, exists := failedByID[accountID]; exists {
			return
		}
		failedByID[accountID] = accountShareRoomBatchFailure(accountID, cause, metadata)
	}
	candidatesByID := make(map[int64]accountShareRoomAccountCandidate, len(candidates))
	for _, candidate := range candidates {
		accountID := candidate.Snapshot.AccountID
		candidatesByID[accountID] = candidate
		if candidate.Snapshot.Platform != roomPlatform {
			recordFailure(accountID, service.ErrAccountShareRoomPlatformMismatch, map[string]string{
				"account_platform": candidate.Snapshot.Platform,
				"room_platform":    roomPlatform,
			})
			continue
		}
		if candidate.Snapshot.AccountLevel == service.AccountLevelUnknown && roomPlatform != service.PlatformOpencode {
			recordFailure(accountID, service.ErrAccountShareRoomUnknownLevel, nil)
			continue
		}
		if candidate.Snapshot.AccountLevel != roomAccountLevel {
			recordFailure(accountID, service.ErrAccountShareRoomLevelMismatch, map[string]string{
				"account_level": candidate.Snapshot.AccountLevel,
				"room_level":    roomAccountLevel,
			})
			continue
		}
		if candidate.UnavailableBlocker != "" {
			recordFailure(accountID, service.ErrAccountShareAccountUnavailable, map[string]string{
				"blocker": candidate.UnavailableBlocker,
			})
			continue
		}
		account := &service.Account{
			ID:           accountID,
			Platform:     candidate.Snapshot.Platform,
			AccountLevel: candidate.Snapshot.AccountLevel,
			Type:         candidate.AccountType,
			Credentials:  candidate.Credentials,
			Extra:        candidate.Extra,
		}
		for _, model := range room.AllowedModels {
			if account.IsModelSupported(model) {
				continue
			}
			recordFailure(accountID, service.ErrAccountShareModeUnsupportedModel, map[string]string{"model": model})
			break
		}
	}

	eligibleAccountIDs := accountShareRoomEligibleAccountIDs(accountIDs, failedByID)
	if len(eligibleAccountIDs) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return accountShareRoomBatchResult(accountIDs, failedByID, nil), nil
	}
	roomModeEligibleIDs, err := lockRoomModeAccountIDsInTx(
		ctx,
		tx,
		input.OwnerUserID,
		roomPlatform,
		eligibleAccountIDs,
	)
	if err != nil {
		return nil, err
	}
	roomModeAccountIDs := make(map[int64]struct{}, len(roomModeEligibleIDs))
	for _, accountID := range roomModeEligibleIDs {
		roomModeAccountIDs[accountID] = struct{}{}
	}
	for _, accountID := range accountIDs {
		if _, alreadyFailed := failedByID[accountID]; alreadyFailed {
			continue
		}
		if _, eligible := roomModeAccountIDs[accountID]; !eligible {
			recordFailure(accountID, service.ErrAccountShareRoomModeRequired, nil)
		}
	}
	eligibleAccountIDs = accountShareRoomEligibleAccountIDs(accountIDs, failedByID)
	if len(eligibleAccountIDs) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return accountShareRoomBatchResult(accountIDs, failedByID, nil), nil
	}
	projections, err := lockAccountShareRoomAccountProjectionsInTx(ctx, tx, eligibleAccountIDs)
	if err != nil {
		return nil, err
	}
	openAssignments, err := lockAccountShareRoomOpenAssignmentsInTx(ctx, tx, eligibleAccountIDs)
	if err != nil {
		return nil, err
	}

	additionalAccounts := 0
	for _, accountID := range eligibleAccountIDs {
		projection, hasProjection := projections[accountID]
		assignment, hasAssignment := openAssignments[accountID]
		if hasProjection {
			if projection.ListingID != input.ListingID || projection.State != "active" {
				recordFailure(accountID, service.ErrAccountShareRoomAccountConflict, nil)
				continue
			}
			if projection.CreatedAt.IsZero() {
				return nil, fmt.Errorf(
					"account share room account %d in listing %d has no trustworthy projection timestamp",
					accountID,
					input.ListingID,
				)
			}
			if hasAssignment && assignment.ListingID != input.ListingID {
				recordFailure(accountID, service.ErrAccountShareRoomAccountConflict, nil)
			}
			continue
		}
		if hasAssignment {
			recordFailure(accountID, service.ErrAccountShareRoomAccountConflict, nil)
			continue
		}
		additionalAccounts++
	}
	eligibleAccountIDs = accountShareRoomEligibleAccountIDs(accountIDs, failedByID)
	if additionalAccounts > 0 {
		if err := r.enforceAccountShareRoomAccountQuotaForAdditionalInTx(
			ctx,
			tx,
			input.OwnerUserID,
			input.ListingID,
			additionalAccounts,
		); err != nil {
			return nil, err
		}
	}

	for _, accountID := range eligibleAccountIDs {
		candidate, exists := candidatesByID[accountID]
		if !exists {
			return nil, fmt.Errorf("locked account share room candidate %d disappeared", accountID)
		}
		projection, hasProjection := projections[accountID]
		assignment, hasAssignment := openAssignments[accountID]
		if hasProjection {
			if !hasAssignment {
				if _, err := insertBackfilledAccountShareRoomAssignmentInTx(
					ctx,
					tx,
					candidate.Snapshot,
					projection.CreatedAt,
				); err != nil {
					return nil, err
				}
			} else if assignment.ListingID != input.ListingID {
				return nil, service.ErrAccountShareRoomAccountConflict
			}
			continue
		}
		if err := insertAccountShareRoomProjectionAndAssignmentInTx(
			ctx,
			tx,
			candidate.Snapshot,
			candidate.Priority,
			input.OwnerUserID,
			"owner",
			"owner_attach",
		); err != nil {
			return nil, err
		}
	}
	if additionalAccounts > 0 {
		result, err := tx.ExecContext(ctx, `
			UPDATE account_share_listings
			SET updated_at = NOW()
			WHERE id = $1
				AND owner_user_id = $2
				AND deleted_at IS NULL
		`, input.ListingID, input.OwnerUserID)
		if err != nil {
			return nil, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if affected != 1 {
			return nil, fmt.Errorf(
				"update account share room %d after atomic attach affected %d rows",
				input.ListingID,
				affected,
			)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return accountShareRoomBatchResult(accountIDs, failedByID, eligibleAccountIDs), nil
}

func accountShareRoomBatchFailure(accountID int64, cause error, metadata map[string]string) service.BulkUpdateAccountResult {
	appErr := infraerrors.FromError(cause)
	mergedMetadata := make(map[string]string, len(appErr.Metadata)+len(metadata))
	for key, value := range appErr.Metadata {
		mergedMetadata[key] = value
	}
	for key, value := range metadata {
		mergedMetadata[key] = value
	}
	if len(mergedMetadata) == 0 {
		mergedMetadata = nil
	}
	return service.BulkUpdateAccountResult{
		AccountID: accountID,
		Success:   false,
		Error:     appErr.Message,
		Reason:    appErr.Reason,
		Message:   appErr.Message,
		Metadata:  mergedMetadata,
	}
}

func accountShareRoomEligibleAccountIDs(
	accountIDs []int64,
	failedByID map[int64]service.BulkUpdateAccountResult,
) []int64 {
	eligible := make([]int64, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		if _, failed := failedByID[accountID]; !failed {
			eligible = append(eligible, accountID)
		}
	}
	return eligible
}

func accountShareRoomBatchResult(
	accountIDs []int64,
	failedByID map[int64]service.BulkUpdateAccountResult,
	successIDs []int64,
) *service.BulkUpdateAccountsResult {
	successSet := make(map[int64]struct{}, len(successIDs))
	for _, accountID := range successIDs {
		successSet[accountID] = struct{}{}
	}
	result := &service.BulkUpdateAccountsResult{
		SuccessIDs: make([]int64, 0, len(successIDs)),
		FailedIDs:  make([]int64, 0, len(failedByID)),
		Results:    make([]service.BulkUpdateAccountResult, 0, len(accountIDs)),
	}
	for _, accountID := range accountIDs {
		if failure, failed := failedByID[accountID]; failed {
			result.Failed++
			result.FailedIDs = append(result.FailedIDs, accountID)
			result.Results = append(result.Results, failure)
			continue
		}
		if _, succeeded := successSet[accountID]; succeeded {
			result.Success++
			result.SuccessIDs = append(result.SuccessIDs, accountID)
			result.Results = append(result.Results, service.BulkUpdateAccountResult{
				AccountID: accountID,
				Success:   true,
			})
		}
	}
	return result
}

func normalizeAccountShareRoomBatchInput(input service.BatchAccountShareRoomAccountsInput) ([]int64, error) {
	if input.ListingID <= 0 || input.OwnerUserID <= 0 {
		return nil, service.ErrAccountExternalPlacementInvalid
	}
	idempotencyKey, err := service.NormalizeIdempotencyKey(input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if idempotencyKey == "" {
		return nil, service.ErrIdempotencyKeyRequired
	}
	accountIDs := uniqueSortedPositiveInt64s(input.AccountIDs)
	if len(accountIDs) == 0 || len(accountIDs) > service.AccountShareRoomBatchMaxAccounts {
		return nil, service.ErrAccountExternalPlacementInvalid
	}
	return accountIDs, nil
}

func lockAccountShareRoomIdentityInTx(
	ctx context.Context,
	tx *sql.Tx,
	listingID int64,
) (*lockedAccountShareRoom, error) {
	if tx == nil || listingID <= 0 {
		return nil, service.ErrAccountShareListingNotFound
	}
	room := &lockedAccountShareRoom{}
	var allowedModelsRaw []byte
	err := tx.QueryRowContext(ctx, `
		SELECT id, owner_user_id, platform, account_level, status, allowed_models,
			pending_operation_id::text
		FROM account_share_listings
		WHERE id = $1
			AND deleted_at IS NULL
		FOR UPDATE
	`, listingID).Scan(
		&room.ID,
		&room.OwnerUserID,
		&room.Platform,
		&room.AccountLevel,
		&room.Status,
		&allowedModelsRaw,
		&room.PendingOperationID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(allowedModelsRaw, &room.AllowedModels); err != nil {
		return nil, err
	}
	room.Platform = strings.ToLower(strings.TrimSpace(room.Platform))
	room.AccountLevel = service.NormalizeAccountLevel(room.AccountLevel)
	room.Status = strings.ToLower(strings.TrimSpace(room.Status))
	return room, nil
}

func lockAccountShareRoomAccountCandidatesInTx(
	ctx context.Context,
	tx *sql.Tx,
	ownerUserID, listingID int64,
	accountIDs []int64,
	includeDeleted bool,
) ([]accountShareRoomAccountCandidate, error) {
	if tx == nil || ownerUserID <= 0 || listingID <= 0 || len(accountIDs) == 0 {
		return nil, service.ErrAccountExternalPlacementInvalid
	}
	deletedFilter := "AND a.deleted_at IS NULL"
	if includeDeleted {
		deletedFilter = ""
	}
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			a.id, a.name, a.platform, a.account_level, a.concurrency, a.priority,
			a.status, COALESCE(%s, '') AS unavailable_blocker,
			a.type, a.credentials, a.extra
		FROM accounts a
		WHERE a.id = ANY($1)
			AND a.owner_user_id = $2
			%s
		ORDER BY a.id ASC
		FOR UPDATE
	`, accountShareAccountUnavailableBlockerSQL("NOW()"), deletedFilter), pq.Array(accountIDs), ownerUserID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	candidates := make([]accountShareRoomAccountCandidate, 0, len(accountIDs))
	for rows.Next() {
		var candidate accountShareRoomAccountCandidate
		var credentialsRaw, extraRaw []byte
		candidate.Snapshot.ListingID = listingID
		candidate.Snapshot.OwnerUserID = ownerUserID
		if err := rows.Scan(
			&candidate.Snapshot.AccountID,
			&candidate.Snapshot.AccountName,
			&candidate.Snapshot.Platform,
			&candidate.Snapshot.AccountLevel,
			&candidate.Snapshot.ConfiguredConcurrency,
			&candidate.Priority,
			&candidate.Status,
			&candidate.UnavailableBlocker,
			&candidate.AccountType,
			&credentialsRaw,
			&extraRaw,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(credentialsRaw, &candidate.Credentials); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(extraRaw, &candidate.Extra); err != nil {
			return nil, err
		}
		candidate.Snapshot.Platform = strings.ToLower(strings.TrimSpace(candidate.Snapshot.Platform))
		candidate.Snapshot.AccountLevel = service.NormalizeAccountLevel(candidate.Snapshot.AccountLevel)
		candidate.Status = strings.ToLower(strings.TrimSpace(candidate.Status))
		candidate.AccountType = strings.ToLower(strings.TrimSpace(candidate.AccountType))
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return candidates, nil
}

func lockRoomModeAccountIDsInTx(
	ctx context.Context,
	tx *sql.Tx,
	ownerUserID int64,
	platform string,
	accountIDs []int64,
) ([]int64, error) {
	if tx == nil || ownerUserID <= 0 || len(accountIDs) == 0 {
		return nil, service.ErrAccountExternalPlacementInvalid
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT account_id
		FROM account_external_placements
		WHERE account_id = ANY($1)
			AND owner_user_id = $2
			AND platform = $3
			AND placement_type = 'room'
			AND state = 'active'
		ORDER BY account_id ASC
		FOR UPDATE
	`, pq.Array(accountIDs), ownerUserID, platform)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	eligibleAccountIDs := make([]int64, 0, len(accountIDs))
	for rows.Next() {
		var accountID int64
		if err := rows.Scan(&accountID); err != nil {
			return nil, err
		}
		eligibleAccountIDs = append(eligibleAccountIDs, accountID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return eligibleAccountIDs, nil
}

func lockAccountShareRoomAccountProjectionsInTx(
	ctx context.Context,
	tx *sql.Tx,
	accountIDs []int64,
) (map[int64]accountShareRoomAccountProjection, error) {
	if tx == nil || len(accountIDs) == 0 {
		return nil, service.ErrAccountExternalPlacementInvalid
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT account_id, listing_id, state, created_at
		FROM account_share_room_accounts
		WHERE account_id = ANY($1)
		ORDER BY account_id ASC
		FOR UPDATE
	`, pq.Array(accountIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	projections := make(map[int64]accountShareRoomAccountProjection, len(accountIDs))
	for rows.Next() {
		var accountID int64
		var projection accountShareRoomAccountProjection
		if err := rows.Scan(
			&accountID,
			&projection.ListingID,
			&projection.State,
			&projection.CreatedAt,
		); err != nil {
			return nil, err
		}
		projections[accountID] = projection
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return projections, nil
}

func lockAccountShareRoomAccountProjectionsForListingInTx(
	ctx context.Context,
	tx *sql.Tx,
	listingID, ownerUserID int64,
	accountIDs []int64,
) (map[int64]accountShareRoomAccountProjection, error) {
	if tx == nil || listingID <= 0 || ownerUserID <= 0 || len(accountIDs) == 0 {
		return nil, service.ErrAccountExternalPlacementInvalid
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT account_id, listing_id, state, created_at
		FROM account_share_room_accounts
		WHERE listing_id = $1
			AND owner_user_id = $2
			AND account_id = ANY($3)
		ORDER BY account_id ASC
		FOR UPDATE
	`, listingID, ownerUserID, pq.Array(accountIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	projections := make(map[int64]accountShareRoomAccountProjection, len(accountIDs))
	for rows.Next() {
		var accountID int64
		var projection accountShareRoomAccountProjection
		if err := rows.Scan(
			&accountID,
			&projection.ListingID,
			&projection.State,
			&projection.CreatedAt,
		); err != nil {
			return nil, err
		}
		projections[accountID] = projection
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return projections, nil
}

func lockAccountShareRoomOpenAssignmentsInTx(
	ctx context.Context,
	tx *sql.Tx,
	accountIDs []int64,
) (map[int64]accountShareRoomOpenAssignment, error) {
	if tx == nil || len(accountIDs) == 0 {
		return nil, service.ErrAccountExternalPlacementInvalid
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, listing_id, account_id_snapshot
		FROM account_share_room_account_assignments
		WHERE account_id_snapshot = ANY($1)
			AND detached_at IS NULL
		ORDER BY account_id_snapshot ASC
		FOR UPDATE
	`, pq.Array(accountIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	assignments := make(map[int64]accountShareRoomOpenAssignment, len(accountIDs))
	for rows.Next() {
		var accountID int64
		var assignment accountShareRoomOpenAssignment
		if err := rows.Scan(&assignment.ID, &assignment.ListingID, &accountID); err != nil {
			return nil, err
		}
		assignments[accountID] = assignment
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return assignments, nil
}

func insertBackfilledAccountShareRoomAssignmentInTx(
	ctx context.Context,
	tx *sql.Tx,
	snapshot accountShareRoomAssignmentSnapshot,
	projectionCreatedAt time.Time,
) (int64, error) {
	if err := validateAccountShareRoomAssignmentSnapshot(tx, snapshot); err != nil {
		return 0, err
	}
	if projectionCreatedAt.IsZero() {
		return 0, fmt.Errorf(
			"account share room account %d in listing %d has no trustworthy projection timestamp",
			snapshot.AccountID,
			snapshot.ListingID,
		)
	}
	var assignmentID int64
	err := tx.QueryRowContext(ctx, `
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
	`,
		snapshot.ListingID,
		snapshot.AccountID,
		snapshot.OwnerUserID,
		snapshot.AccountName,
		snapshot.Platform,
		snapshot.AccountLevel,
		snapshot.ConfiguredConcurrency,
		projectionCreatedAt.UTC(),
	).Scan(&assignmentID)
	if err != nil {
		return 0, translateAccountShareRoomPersistenceError(err)
	}
	return assignmentID, nil
}

func insertAccountShareRoomProjectionAndAssignmentInTx(
	ctx context.Context,
	tx *sql.Tx,
	snapshot accountShareRoomAssignmentSnapshot,
	priority int,
	actorUserID int64,
	actorRole string,
	reason string,
) error {
	if err := validateAccountShareRoomAssignmentSnapshot(tx, snapshot); err != nil {
		return err
	}
	if actorUserID <= 0 || strings.TrimSpace(actorRole) == "" {
		return service.ErrAccountExternalPlacementInvalid
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO account_share_room_accounts (
			listing_id, account_id, owner_user_id, platform, account_level,
			state, priority, version, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, 'active', $6, 1, NOW(), NOW())
	`,
		snapshot.ListingID,
		snapshot.AccountID,
		snapshot.OwnerUserID,
		snapshot.Platform,
		snapshot.AccountLevel,
		priority,
	); err != nil {
		return translateAccountShareRoomPersistenceError(err)
	}
	if _, err := tx.ExecContext(ctx, `
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
	`,
		snapshot.ListingID,
		snapshot.AccountID,
		snapshot.OwnerUserID,
		snapshot.AccountName,
		snapshot.Platform,
		snapshot.AccountLevel,
		snapshot.ConfiguredConcurrency,
		actorUserID,
		actorRole,
		strings.TrimSpace(reason),
	); err != nil {
		return translateAccountShareRoomPersistenceError(err)
	}
	return nil
}

func closeAccountShareRoomAssignmentInTx(
	ctx context.Context,
	tx *sql.Tx,
	assignmentID int64,
	snapshot accountShareRoomAssignmentSnapshot,
	actorUserID int64,
	actorRole string,
	reason string,
) error {
	if err := validateAccountShareRoomAssignmentSnapshot(tx, snapshot); err != nil {
		return err
	}
	if assignmentID <= 0 || actorUserID <= 0 || strings.TrimSpace(actorRole) == "" {
		return service.ErrAccountExternalPlacementInvalid
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE account_share_room_account_assignments
		SET detached_at = NOW(),
			detached_by_user_id = $1,
			detached_by_role = $2,
			detach_reason = $3
		WHERE id = $4
			AND listing_id = $5
			AND account_id_snapshot = $6
			AND detached_at IS NULL
	`,
		actorUserID,
		actorRole,
		strings.TrimSpace(reason),
		assignmentID,
		snapshot.ListingID,
		snapshot.AccountID,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf(
			"close account share room assignment %d affected %d rows",
			assignmentID,
			affected,
		)
	}
	return nil
}

func validateAccountShareRoomAssignmentSnapshot(
	tx *sql.Tx,
	snapshot accountShareRoomAssignmentSnapshot,
) error {
	if tx == nil ||
		snapshot.ListingID <= 0 ||
		snapshot.AccountID <= 0 ||
		snapshot.OwnerUserID <= 0 ||
		snapshot.ConfiguredConcurrency <= 0 {
		return service.ErrAccountExternalPlacementInvalid
	}
	return nil
}

func lockAccountShareOwnerQuotaInTx(ctx context.Context, tx *sql.Tx, ownerUserID int64) error {
	if tx == nil || ownerUserID <= 0 {
		return service.ErrAccountShareRoomOwnerMismatch
	}
	_, err := tx.ExecContext(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtext($1)::bigint)",
		fmt.Sprintf("account_share_owner_quota:%d", ownerUserID),
	)
	return err
}

func enforceAccountShareRoomCreationQuotaInTx(ctx context.Context, tx *sql.Tx, ownerUserID int64) error {
	if tx == nil || ownerUserID <= 0 {
		return service.ErrAccountShareRoomOwnerMismatch
	}
	quota, err := resolveAccountShareQuotaWithQueryer(ctx, tx, ownerUserID, time.Now().UTC())
	if err != nil {
		return err
	}
	if quota == nil || !quota.Limits.Valid() {
		return service.ErrAccountShareQuotaConfigurationUnavailable
	}
	if quota.GrowthBlocked {
		return service.ErrAccountShareQuotaGrandfatherGrowthBlocked
	}
	limits := quota.Limits
	usage, err := getAccountShareQuotaUsageWithQueryer(ctx, tx, ownerUserID)
	if err != nil {
		return err
	}
	if usage == nil {
		return service.ErrAccountShareQuotaConfigurationUnavailable
	}
	if len(service.AccountShareQuotaExceededDimensions(limits, *usage)) > 0 {
		return service.ErrAccountShareQuotaHistoricalGrowthBlocked
	}
	if usage.LiveRooms >= limits.MaxLiveRooms {
		return service.ErrAccountShareRoomLimitExceeded
	}
	if usage.RoomCreates24Hours >= limits.MaxRoomCreates24Hours {
		return service.ErrAccountShareRoomCreateRateExceeded
	}
	if usage.OwnerRoomAccounts >= limits.MaxRoomAccountsPerOwner {
		return service.ErrAccountShareOwnerRoomAccountLimitExceeded
	}
	return nil
}

func (r *accountShareModeRepository) enforceAccountShareRoomCreationQuotaInTx(
	ctx context.Context,
	tx *sql.Tx,
	ownerUserID int64,
) error {
	err := enforceAccountShareRoomCreationQuotaInTx(ctx, tx, ownerUserID)
	if err == nil || r.quotaEnforcementEnabled() || !isAccountShareQuotaLimitError(err) {
		return err
	}
	logger.LegacyPrintf(
		"repository.account_share_room",
		"[AccountShareQuotaShadow] owner=%d operation=create_room blocker=%v",
		ownerUserID,
		err,
	)
	return nil
}

func enforceAccountShareRoomAccountQuotaForAdditionalInTx(
	ctx context.Context,
	tx *sql.Tx,
	ownerUserID, listingID int64,
	additionalAccounts int,
) error {
	if tx == nil || ownerUserID <= 0 || listingID <= 0 || additionalAccounts <= 0 {
		return service.ErrAccountExternalPlacementInvalid
	}
	quota, err := resolveAccountShareQuotaWithQueryer(ctx, tx, ownerUserID, time.Now().UTC())
	if err != nil {
		return err
	}
	if quota == nil || !quota.Limits.Valid() {
		return service.ErrAccountShareQuotaConfigurationUnavailable
	}
	if quota.GrowthBlocked {
		return service.ErrAccountShareQuotaGrandfatherGrowthBlocked
	}
	limits := quota.Limits
	usage, err := getAccountShareQuotaUsageWithQueryer(ctx, tx, ownerUserID)
	if err != nil {
		return err
	}
	if usage == nil {
		return service.ErrAccountShareQuotaConfigurationUnavailable
	}
	if len(service.AccountShareQuotaExceededDimensions(limits, *usage)) > 0 {
		return service.ErrAccountShareQuotaHistoricalGrowthBlocked
	}
	var roomAccounts int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)::int
		FROM account_share_room_accounts room_account
		WHERE room_account.listing_id = $1
			AND room_account.state IN ('active', 'draining')
	`, listingID).Scan(&roomAccounts); err != nil {
		return err
	}
	if roomAccounts+additionalAccounts > limits.MaxAccountsPerRoom {
		return service.ErrAccountShareRoomAccountLimitExceeded
	}
	if usage.OwnerRoomAccounts+additionalAccounts > limits.MaxRoomAccountsPerOwner {
		return service.ErrAccountShareOwnerRoomAccountLimitExceeded
	}
	return nil
}

func (r *accountShareModeRepository) enforceAccountShareRoomAccountQuotaForAdditionalInTx(
	ctx context.Context,
	tx *sql.Tx,
	ownerUserID, listingID int64,
	additionalAccounts int,
) error {
	err := enforceAccountShareRoomAccountQuotaForAdditionalInTx(
		ctx,
		tx,
		ownerUserID,
		listingID,
		additionalAccounts,
	)
	if err == nil || r.quotaEnforcementEnabled() || !isAccountShareQuotaLimitError(err) {
		return err
	}
	logger.LegacyPrintf(
		"repository.account_share_room",
		"[AccountShareQuotaShadow] owner=%d listing=%d operation=attach_accounts additional=%d blocker=%v",
		ownerUserID,
		listingID,
		additionalAccounts,
		err,
	)
	return nil
}

func isAccountShareQuotaLimitError(err error) bool {
	return errors.Is(err, service.ErrAccountShareQuotaGrandfatherGrowthBlocked) ||
		errors.Is(err, service.ErrAccountShareQuotaHistoricalGrowthBlocked) ||
		errors.Is(err, service.ErrAccountShareRoomLimitExceeded) ||
		errors.Is(err, service.ErrAccountShareRoomCreateRateExceeded) ||
		errors.Is(err, service.ErrAccountShareRoomAccountLimitExceeded) ||
		errors.Is(err, service.ErrAccountShareOwnerRoomAccountLimitExceeded)
}

func (r *accountShareModeRepository) DetachRoomAccountsAtomic(
	ctx context.Context,
	input service.BatchAccountShareRoomAccountsInput,
) (*service.AccountShareSeatBillingResult, error) {
	accountIDs, err := normalizeAccountShareRoomBatchInput(input)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if err := lockAccountShareOwnerQuotaInTx(ctx, tx, input.OwnerUserID); err != nil {
		return nil, err
	}
	room, err := lockAccountShareRoomIdentityInTx(ctx, tx, input.ListingID)
	if err != nil {
		return nil, err
	}
	if room.OwnerUserID != input.OwnerUserID {
		return nil, service.ErrAccountShareRoomOwnerMismatch
	}
	candidates, err := lockAccountShareRoomAccountCandidatesInTx(
		ctx,
		tx,
		input.OwnerUserID,
		input.ListingID,
		accountIDs,
		true,
	)
	if err != nil {
		return nil, err
	}
	candidatesByID := make(map[int64]accountShareRoomAccountCandidate, len(candidates))
	for _, candidate := range candidates {
		candidatesByID[candidate.Snapshot.AccountID] = candidate
	}
	projections, err := lockAccountShareRoomAccountProjectionsForListingInTx(
		ctx,
		tx,
		input.ListingID,
		input.OwnerUserID,
		accountIDs,
	)
	if err != nil {
		return nil, err
	}
	if len(projections) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	projectionAccountIDs := make([]int64, 0, len(projections))
	for _, accountID := range accountIDs {
		if _, ok := projections[accountID]; ok {
			projectionAccountIDs = append(projectionAccountIDs, accountID)
		}
	}
	openAssignments, err := lockAccountShareRoomOpenAssignmentsInTx(
		ctx,
		tx,
		projectionAccountIDs,
	)
	if err != nil {
		return nil, err
	}
	for _, accountID := range projectionAccountIDs {
		candidate, ok := candidatesByID[accountID]
		if !ok {
			return nil, fmt.Errorf(
				"account share room %d projection references missing owner account %d",
				input.ListingID,
				accountID,
			)
		}
		if err := validateAccountShareRoomAssignmentSnapshot(tx, candidate.Snapshot); err != nil {
			return nil, err
		}
		projection := projections[accountID]
		if projection.CreatedAt.IsZero() {
			return nil, fmt.Errorf(
				"account share room account %d in listing %d has no trustworthy projection timestamp",
				accountID,
				input.ListingID,
			)
		}
		if assignment, ok := openAssignments[accountID]; ok && assignment.ListingID != input.ListingID {
			return nil, service.ErrAccountShareRoomAccountConflict
		}
	}

	assignmentIDs := make(map[int64]int64, len(projectionAccountIDs))
	for _, accountID := range projectionAccountIDs {
		if assignment, ok := openAssignments[accountID]; ok {
			assignmentIDs[accountID] = assignment.ID
			continue
		}
		assignmentID, err := insertBackfilledAccountShareRoomAssignmentInTx(
			ctx,
			tx,
			candidatesByID[accountID].Snapshot,
			projections[accountID].CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		assignmentIDs[accountID] = assignmentID
	}
	billing, err := r.rebindRoomMembershipsBeforePlacementRemovalSetInTx(
		ctx,
		tx,
		input.ListingID,
		projectionAccountIDs,
	)
	if err != nil {
		return nil, err
	}
	drainResult, err := tx.ExecContext(ctx, `
		UPDATE account_share_room_accounts
		SET state = 'draining',
			version = version + 1,
			updated_at = NOW()
		WHERE listing_id = $1
			AND owner_user_id = $2
			AND account_id = ANY($3)
			AND state = 'active'
	`, input.ListingID, input.OwnerUserID, pq.Array(projectionAccountIDs))
	if err != nil {
		return nil, err
	}
	drained, err := drainResult.RowsAffected()
	if err != nil {
		return nil, err
	}
	if drained != int64(len(projectionAccountIDs)) {
		return nil, fmt.Errorf(
			"mark %d accounts draining in account share room %d affected %d rows",
			len(projectionAccountIDs),
			input.ListingID,
			drained,
		)
	}
	for _, accountID := range projectionAccountIDs {
		if err := closeAccountShareRoomAssignmentInTx(
			ctx,
			tx,
			assignmentIDs[accountID],
			candidatesByID[accountID].Snapshot,
			input.OwnerUserID,
			"owner",
			"owner_detach",
		); err != nil {
			return nil, err
		}
	}
	deleteResult, err := tx.ExecContext(ctx, `
		DELETE FROM account_share_room_accounts
		WHERE listing_id = $1
			AND owner_user_id = $2
			AND account_id = ANY($3)
	`, input.ListingID, input.OwnerUserID, pq.Array(projectionAccountIDs))
	if err != nil {
		return nil, err
	}
	deleted, err := deleteResult.RowsAffected()
	if err != nil {
		return nil, err
	}
	if deleted != int64(len(projectionAccountIDs)) {
		return nil, fmt.Errorf(
			"delete %d account projections from account share room %d affected %d rows",
			len(projectionAccountIDs),
			input.ListingID,
			deleted,
		)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return billing, nil
}

func (r *accountShareModeRepository) HasRoomAccount(ctx context.Context, ownerUserID, accountID int64) (bool, error) {
	if ownerUserID <= 0 || accountID <= 0 {
		return false, service.ErrAccountExternalPlacementInvalid
	}
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM account_share_room_accounts
			WHERE account_id = $1
				AND owner_user_id = $2
				AND state IN ('active', 'draining')
		)
	`, accountID, ownerUserID).Scan(&exists)
	return exists, err
}

func (r *accountShareModeRepository) GetExternalPlacement(ctx context.Context, ownerUserID, accountID int64) (*service.AccountExternalPlacement, error) {
	if ownerUserID <= 0 || accountID <= 0 {
		return nil, service.ErrAccountNotFound
	}
	var exists bool
	if err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM accounts
			WHERE id = $1
				AND owner_user_id = $2
				AND deleted_at IS NULL
		)
	`, accountID, ownerUserID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, service.ErrAccountNotFound
	}
	placement, err := getAccountExternalPlacement(ctx, r.db, accountID, ownerUserID)
	if err != nil {
		return nil, err
	}
	if placement == nil {
		var version int64
		if err := r.db.QueryRowContext(ctx, `
			SELECT COALESCE(MAX(placement_version), 0)
			FROM account_external_placement_conversions
			WHERE account_id = $1
		`, accountID).Scan(&version); err != nil {
			return nil, err
		}
		return privateAccountExternalPlacement(version), nil
	}
	return placement, nil
}

func (r *accountShareModeRepository) BeginExternalPlacementDrain(ctx context.Context, ownerUserID, accountID int64) (bool, error) {
	if ownerUserID <= 0 || accountID <= 0 {
		return false, service.ErrAccountExternalPlacementInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var lockedAccountID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM accounts
		WHERE id = $1
			AND owner_user_id = $2
			AND deleted_at IS NULL
		FOR UPDATE
	`, accountID, ownerUserID).Scan(&lockedAccountID); errors.Is(err, sql.ErrNoRows) {
		return false, service.ErrAccountShareRoomOwnerMismatch
	} else if err != nil {
		return false, err
	}
	current, err := getAccountExternalPlacementInTx(ctx, tx, accountID, ownerUserID, true)
	if err != nil {
		return false, err
	}
	if current == nil {
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}
	now := time.Now().UTC()
	if current.State == "draining" && current.UpdatedAt.After(now.Add(-accountExternalPlacementDrainLease)) {
		return false, service.ErrAccountExternalPlacementBusy
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE account_external_placements
		SET state = 'draining',
			updated_at = $1
		WHERE account_id = $2
			AND owner_user_id = $3
	`, now, accountID, ownerUserID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected != 1 {
		return false, service.ErrAccountExternalPlacementConflict
	}
	if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account_share_room", "[SchedulerOutbox] enqueue placement drain failed: account=%d err=%v", accountID, err)
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *accountShareModeRepository) RestoreExternalPlacementAfterDrain(ctx context.Context, ownerUserID, accountID int64) error {
	if ownerUserID <= 0 || accountID <= 0 {
		return service.ErrAccountExternalPlacementInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE account_external_placements
		SET state = 'active',
			updated_at = NOW()
		WHERE account_id = $1
			AND owner_user_id = $2
			AND state = 'draining'
	`, accountID, ownerUserID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
			logger.LegacyPrintf("repository.account_share_room", "[SchedulerOutbox] enqueue placement drain restore failed: account=%d err=%v", accountID, err)
		}
	}
	return tx.Commit()
}

func (r *accountShareModeRepository) ConvertExternalPlacement(ctx context.Context, input service.ConvertAccountExternalPlacementInput) (*service.ConvertAccountExternalPlacementResult, error) {
	target := strings.ToLower(strings.TrimSpace(input.Target))
	if input.AccountID <= 0 || input.OwnerUserID <= 0 || strings.TrimSpace(input.IdempotencyKey) == "" {
		return nil, service.ErrAccountExternalPlacementInvalid
	}
	switch target {
	case service.AccountExternalPlacementPrivate, service.AccountExternalPlacementPublicPool, service.AccountExternalPlacementRoom:
	default:
		return nil, service.ErrAccountExternalPlacementInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	var platform, accountLevel string
	var accountPriority int
	if err := tx.QueryRowContext(ctx, `
		SELECT platform, account_level, priority
		FROM accounts
		WHERE id = $1
			AND owner_user_id = $2
			AND deleted_at IS NULL
		FOR UPDATE
	`, input.AccountID, input.OwnerUserID).Scan(&platform, &accountLevel, &accountPriority); errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareRoomOwnerMismatch
	} else if err != nil {
		return nil, err
	}
	platform = strings.ToLower(strings.TrimSpace(platform))
	accountLevel = service.NormalizeAccountLevel(accountLevel)
	if target == service.AccountExternalPlacementRoom && accountLevel == service.AccountLevelUnknown && platform != service.PlatformOpencode {
		return nil, service.ErrAccountShareRoomUnknownLevel
	}

	current, err := getAccountExternalPlacementInTx(ctx, tx, input.AccountID, input.OwnerUserID, true)
	if err != nil {
		return nil, err
	}
	existingResult, err := getIdempotentExternalPlacementConversionInTx(ctx, tx, input, target)
	if err != nil {
		return nil, err
	}
	if existingResult != nil {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		tx = nil
		return existingResult, nil
	}
	targetMatchesCurrent := accountExternalPlacementTargetMatches(current, target, input.RoomID, input.PublicGroupID)
	unchanged := targetMatchesCurrent &&
		(current == nil || current.State == "active")
	if current != nil && !unchanged && current.State != "draining" {
		return nil, service.ErrAccountExternalPlacementBusy
	}

	roomIDs := make([]int64, 0, 2)
	if current != nil && current.Target == service.AccountExternalPlacementRoom && current.RoomID != nil {
		roomIDs = append(roomIDs, *current.RoomID)
	}
	if target == service.AccountExternalPlacementRoom {
		if input.RoomID != nil {
			return nil, service.ErrAccountExternalPlacementInvalid
		}
	} else if input.RoomID != nil {
		return nil, service.ErrAccountExternalPlacementInvalid
	}
	if _, err := lockAccountShareRoomsInTx(ctx, tx, roomIDs); err != nil {
		return nil, err
	}

	privateGroupID, err := accountOwnerPrivateGroupIDInTx(ctx, tx, input.OwnerUserID, platform)
	if err != nil {
		return nil, err
	}
	expectedGroupIDs := []int64{privateGroupID}
	switch target {
	case service.AccountExternalPlacementPublicPool:
		if input.PublicGroupID == nil || *input.PublicGroupID <= 0 {
			return nil, service.ErrAccountExternalPlacementInvalid
		}
		if err := validatePublicPlacementGroupInTx(ctx, tx, *input.PublicGroupID, platform); err != nil {
			return nil, err
		}
		expectedGroupIDs = append(expectedGroupIDs, *input.PublicGroupID)
	case service.AccountExternalPlacementRoom:
		var modeGroupID int64
		if err := tx.QueryRowContext(ctx, `
			SELECT group_id
			FROM account_share_mode_groups
			WHERE platform = $1
		`, platform).Scan(&modeGroupID); errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrAccountShareModeGroupUnavailable
		} else if err != nil {
			return nil, err
		}
		expectedGroupIDs = append(expectedGroupIDs, modeGroupID)
	}
	if !samePositiveInt64Set(input.GroupIDs, expectedGroupIDs) {
		return nil, service.ErrAccountExternalPlacementInvalid.WithMetadata(map[string]string{"field": "group_ids"})
	}

	previousVersion, err := currentAccountExternalPlacementVersionInTx(ctx, tx, input.AccountID, current)
	if err != nil {
		return nil, err
	}
	previous := placementToService(current)
	if previous == nil {
		previous = privateAccountExternalPlacement(previousVersion)
	}
	version, err := nextAccountExternalPlacementVersionInTx(ctx, tx, input.AccountID, current, unchanged)
	if err != nil {
		return nil, err
	}
	var seatBillingResult *service.AccountShareSeatBillingResult
	if !unchanged {
		if !targetMatchesCurrent && current != nil && current.Target == service.AccountExternalPlacementRoom && current.RoomID != nil {
			seatBillingResult, err = r.rebindRoomMembershipsBeforePlacementRemovalInTx(ctx, tx, *current.RoomID, input.AccountID)
			if err != nil {
				return nil, err
			}
		}
		if err := replaceAccountGroupsInTx(ctx, tx, input.AccountID, expectedGroupIDs); err != nil {
			return nil, err
		}
		shareMode := service.AccountShareModePrivate
		if target == service.AccountExternalPlacementPublicPool {
			shareMode = service.AccountShareModePublic
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE accounts
			SET share_mode = $1,
				share_status = $2,
				updated_at = NOW()
			WHERE id = $3
				AND owner_user_id = $4
				AND deleted_at IS NULL
		`, shareMode, service.AccountShareStatusApproved, input.AccountID, input.OwnerUserID); err != nil {
			return nil, err
		}
		if err := writeAccountExternalPlacementTargetInTx(ctx, tx, input, target, platform, accountLevel, accountPriority, version); err != nil {
			return nil, err
		}
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &input.AccountID, nil, nil); err != nil {
			logger.LegacyPrintf("repository.account_share_room", "[SchedulerOutbox] enqueue placement account change failed: account=%d err=%v", input.AccountID, err)
		}
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountGroupsChanged, &input.AccountID, nil, buildSchedulerGroupPayload(expectedGroupIDs)); err != nil {
			logger.LegacyPrintf("repository.account_share_room", "[SchedulerOutbox] enqueue placement group change failed: account=%d err=%v", input.AccountID, err)
		}
	}

	currentPlacement := privateAccountExternalPlacement(version)
	if target != service.AccountExternalPlacementPrivate {
		lockedCurrent, loadErr := getAccountExternalPlacementInTx(ctx, tx, input.AccountID, input.OwnerUserID, false)
		err = loadErr
		if err != nil {
			return nil, err
		}
		if lockedCurrent == nil {
			return nil, service.ErrAccountExternalPlacementConflict
		}
		currentPlacement = placementToService(lockedCurrent)
	}
	result := &service.ConvertAccountExternalPlacementResult{
		AccountID:         input.AccountID,
		Previous:          previous,
		Current:           currentPlacement,
		Unchanged:         unchanged,
		SeatBillingResult: seatBillingResult,
	}
	if result.Current == nil {
		result.Current = privateAccountExternalPlacement(version)
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO account_external_placement_conversions (
			owner_user_id, account_id, idempotency_key, target_type,
			target_listing_id, target_public_group_id, placement_version,
			result, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, NOW())
	`, input.OwnerUserID, input.AccountID, strings.TrimSpace(input.IdempotencyKey), target, nullableInt64(input.RoomID), nullableInt64(input.PublicGroupID), version, string(resultJSON)); err != nil {
		return nil, translateAccountExternalPlacementConversionError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func (r *accountShareModeRepository) GetOpenMembershipRuntimeBinding(
	ctx context.Context,
	membershipID int64,
	accountID int64,
) (*service.AccountShareMembershipRuntimeBinding, error) {
	if r == nil || r.db == nil || membershipID <= 0 || accountID <= 0 {
		return nil, service.ErrAccountShareBillingBindingUnavailable
	}
	binding := &service.AccountShareMembershipRuntimeBinding{}
	err := r.db.QueryRowContext(ctx, `
		SELECT
			binding.id,
			binding.membership_id,
			binding.listing_id,
			binding.account_id_snapshot,
			binding.listing_revision_id,
			binding.terms_revision_number,
			binding.routing_generation
		FROM account_share_memberships membership
		JOIN account_share_membership_account_bindings binding
			ON binding.membership_id = membership.id
			AND binding.listing_id = membership.listing_id
			AND binding.account_id = membership.account_id
			AND binding.account_id_snapshot = membership.account_id
			AND binding.listing_revision_id = membership.listing_revision_id
			AND binding.unbound_at IS NULL
		WHERE membership.id = $1
			AND membership.account_id = $2
			AND membership.status = $3
			AND membership.deleted_at IS NULL
	`, membershipID, accountID, service.AccountShareMembershipStatusActive).Scan(
		&binding.BindingID,
		&binding.MembershipID,
		&binding.ListingID,
		&binding.AccountID,
		&binding.ListingRevisionID,
		&binding.TermsRevisionNumber,
		&binding.RoutingGeneration,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareBillingBindingUnavailable
	}
	if err != nil {
		return nil, err
	}
	if binding.BindingID <= 0 ||
		binding.MembershipID != membershipID ||
		binding.ListingID <= 0 ||
		binding.AccountID != accountID ||
		binding.ListingRevisionID <= 0 ||
		binding.TermsRevisionNumber <= 0 ||
		binding.RoutingGeneration <= 0 {
		return nil, fmt.Errorf(
			"invalid account-share runtime binding for membership %d account %d",
			membershipID,
			accountID,
		)
	}
	return binding, nil
}

func (r *accountShareModeRepository) RebindMembershipToHealthyRoomAccount(ctx context.Context, membershipID, currentAccountID int64, now time.Time) (bool, error) {
	if r == nil || r.db == nil || membershipID <= 0 || currentAccountID <= 0 {
		return false, nil
	}
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}

	// Listing discovery is intentionally read-only. Every mutable fact is
	// rechecked under the canonical listing -> room accounts/accounts ->
	// membership -> binding -> billing intent lock order below.
	var listingID int64
	err := r.db.QueryRowContext(ctx, `
		SELECT listing_id
		FROM account_share_memberships
		WHERE id = $1
			AND account_id = $2
			AND status = $3
			AND deleted_at IS NULL
	`, membershipID, currentAccountID, service.AccountShareMembershipStatusActive).Scan(&listingID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err := lockAccountShareMembershipRebindScopeInTx(ctx, tx, listingID); errors.Is(err, service.ErrAccountShareListingNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}

	replacementID, err := healthyRoomAccountIDInTx(ctx, tx, listingID, currentAccountID, now.UTC())
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	memberships, err := lockAccountShareMembershipsForRebindInTx(
		ctx,
		tx,
		listingID,
		currentAccountID,
		membershipID,
	)
	if err != nil {
		return false, err
	}
	if len(memberships) != 1 {
		return false, nil
	}
	if err := r.rebindLockedAccountShareMembershipsInTx(ctx, tx, memberships, replacementID, now); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	tx = nil
	return true, nil
}

func accountOwnerPrivateGroupIDInTx(ctx context.Context, tx *sql.Tx, ownerUserID int64, platform string) (int64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id
		FROM groups
		WHERE owner_user_id = $1
			AND platform = $2
			AND scope = $3
			AND status = 'active'
			AND deleted_at IS NULL
			AND COALESCE(subscription_type, '') <> 'none'
		ORDER BY id
		LIMIT 2
		FOR SHARE
	`, ownerUserID, platform, service.GroupScopeUserPrivate)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(ids) != 1 {
		return 0, service.ErrAccountSharePrivateGroupUnavailable
	}
	return ids[0], nil
}

func validateAccountShareModeGroupInTx(ctx context.Context, tx *sql.Tx, groupID int64, platform string) error {
	var exists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM account_share_mode_groups mg
			JOIN groups g ON g.id = mg.group_id
			WHERE mg.group_id = $1
				AND mg.platform = $2
				AND g.status = 'active'
				AND g.deleted_at IS NULL
		)
	`, groupID, platform).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return service.ErrAccountShareModeGroupUnavailable
	}
	return nil
}

func validatePublicPlacementGroupInTx(ctx context.Context, tx *sql.Tx, groupID int64, platform string) error {
	var exists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM groups g
			WHERE g.id = $1
				AND g.platform = $2
				AND g.scope = 'public'
				AND g.owner_user_id IS NULL
				AND g.status = 'active'
				AND g.deleted_at IS NULL
				AND NOT EXISTS (
					SELECT 1
					FROM account_share_mode_groups mg
					WHERE mg.group_id = g.id
				)
		)
	`, groupID, platform).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return service.ErrOwnedAccountPublicPoolUnavailable
	}
	return nil
}

func getAccountExternalPlacement(ctx context.Context, db *sql.DB, accountID, ownerUserID int64) (*service.AccountExternalPlacement, error) {
	var placement service.AccountExternalPlacement
	var roomID, publicGroupID sql.NullInt64
	err := db.QueryRowContext(ctx, `
		SELECT placement.placement_type, placement.listing_id, COALESCE(l.room_name, ''),
			placement.public_group_id, placement.state, placement.version
		FROM account_external_placements placement
		LEFT JOIN account_share_listings l ON l.id = placement.listing_id
		WHERE placement.account_id = $1
			AND placement.owner_user_id = $2
	`, accountID, ownerUserID).Scan(
		&placement.Target,
		&roomID,
		&placement.RoomName,
		&publicGroupID,
		&placement.State,
		&placement.Version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	placement.RoomID = sqlNullInt64Ptr(roomID)
	placement.PublicGroupID = sqlNullInt64Ptr(publicGroupID)
	return &placement, nil
}

func getAccountExternalPlacementInTx(ctx context.Context, tx *sql.Tx, accountID, ownerUserID int64, lock bool) (*lockedAccountExternalPlacement, error) {
	lockClause := ""
	if lock {
		lockClause = " FOR UPDATE OF placement"
	}
	var placement lockedAccountExternalPlacement
	var roomID, publicGroupID sql.NullInt64
	err := tx.QueryRowContext(ctx, `
		SELECT placement.placement_type, placement.listing_id, COALESCE(l.room_name, ''),
			placement.public_group_id, placement.state, placement.version, placement.updated_at
		FROM account_external_placements placement
		LEFT JOIN account_share_listings l ON l.id = placement.listing_id
		WHERE placement.account_id = $1
			AND placement.owner_user_id = $2
	`+lockClause, accountID, ownerUserID).Scan(
		&placement.Target,
		&roomID,
		&placement.RoomName,
		&publicGroupID,
		&placement.State,
		&placement.Version,
		&placement.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	placement.RoomID = sqlNullInt64Ptr(roomID)
	placement.PublicGroupID = sqlNullInt64Ptr(publicGroupID)
	return &placement, nil
}

func lockAccountShareRoomsInTx(ctx context.Context, tx *sql.Tx, roomIDs []int64) (map[int64]*lockedAccountShareRoom, error) {
	roomIDs = uniqueSortedPositiveInt64s(roomIDs)
	out := make(map[int64]*lockedAccountShareRoom, len(roomIDs))
	if len(roomIDs) == 0 {
		return out, nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, owner_user_id, platform, account_level, status, allowed_models,
			pending_operation_id::text,
			codex_cli_only, codex_5h_limit_percent, codex_7d_limit_percent
		FROM account_share_listings
		WHERE id = ANY($1)
			AND deleted_at IS NULL
		ORDER BY id
		FOR UPDATE
	`, pq.Array(roomIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		room := &lockedAccountShareRoom{}
		var allowedModelsRaw []byte
		if err := rows.Scan(
			&room.ID,
			&room.OwnerUserID,
			&room.Platform,
			&room.AccountLevel,
			&room.Status,
			&allowedModelsRaw,
			&room.PendingOperationID,
			&room.CodexCLIOnly,
			&room.Codex5hLimitPercent,
			&room.Codex7dLimitPercent,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(allowedModelsRaw, &room.AllowedModels); err != nil {
			return nil, err
		}
		room.Platform = strings.ToLower(strings.TrimSpace(room.Platform))
		out[room.ID] = room
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func getIdempotentExternalPlacementConversionInTx(ctx context.Context, tx *sql.Tx, input service.ConvertAccountExternalPlacementInput, target string) (*service.ConvertAccountExternalPlacementResult, error) {
	var accountID int64
	var storedTarget string
	var storedRoomID, storedPublicGroupID sql.NullInt64
	var resultRaw []byte
	err := tx.QueryRowContext(ctx, `
		SELECT account_id, target_type, target_listing_id, target_public_group_id, result
		FROM account_external_placement_conversions
		WHERE owner_user_id = $1
			AND idempotency_key = $2
	`, input.OwnerUserID, strings.TrimSpace(input.IdempotencyKey)).Scan(
		&accountID,
		&storedTarget,
		&storedRoomID,
		&storedPublicGroupID,
		&resultRaw,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if accountID != input.AccountID ||
		storedTarget != target ||
		!nullableInt64Equals(storedRoomID, input.RoomID) ||
		!nullableInt64Equals(storedPublicGroupID, input.PublicGroupID) {
		return nil, service.ErrAccountExternalPlacementIdempotency
	}
	var result service.ConvertAccountExternalPlacementResult
	if err := json.Unmarshal(resultRaw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func getIdempotentRoomCreation(
	ctx context.Context,
	queryer accountShareRoomQueryRower,
	ownerUserID, accountID int64,
	idempotencyKey, roomName string,
	listing *service.AccountShareListing,
	allowedModelsJSON string,
) (int64, error) {
	var storedAccountID int64
	var storedTarget string
	var storedListingID, storedPublicGroupID sql.NullInt64
	var payloadMatches bool
	err := queryer.QueryRowContext(ctx, `
		SELECT
			conversion.account_id,
			conversion.target_type,
			conversion.target_listing_id,
			conversion.target_public_group_id,
			EXISTS (
				SELECT 1
				FROM account_share_listings listing
				WHERE listing.id = conversion.target_listing_id
					AND listing.owner_user_id = $1
					AND listing.deleted_at IS NULL
					AND BTRIM(listing.room_name) = BTRIM($3)
					AND listing.seat_limit = $4
					AND listing.rate_multiplier = $5
					AND listing.allowed_models = $6::jsonb
					AND listing.per_user_concurrency = $7
					AND listing.hourly_rate = $8
					AND listing.hourly_fee_waiver_minimum = $9
					AND listing.min_balance_required = $10
					AND listing.codex_cli_only = $11
					AND listing.codex_5h_limit_percent = $12
					AND listing.codex_7d_limit_percent = $13
			)
		FROM account_external_placement_conversions conversion
		WHERE conversion.owner_user_id = $1
			AND conversion.idempotency_key = $2
	`, ownerUserID, idempotencyKey, roomName,
		listing.SeatLimit,
		listing.RateMultiplier,
		allowedModelsJSON,
		listing.PerUserConcurrency,
		listing.HourlyRate,
		listing.HourlyFeeWaiverMinimum,
		listing.MinBalanceRequired,
		listing.CodexCLIOnly,
		listing.Codex5hLimitPercent,
		listing.Codex7dLimitPercent,
	).Scan(
		&storedAccountID,
		&storedTarget,
		&storedListingID,
		&storedPublicGroupID,
		&payloadMatches,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if storedAccountID != accountID ||
		storedTarget != service.AccountExternalPlacementRoom ||
		!storedListingID.Valid ||
		storedListingID.Int64 <= 0 ||
		storedPublicGroupID.Valid ||
		!payloadMatches {
		return 0, service.ErrAccountExternalPlacementIdempotency
	}
	return storedListingID.Int64, nil
}

func (r *accountShareModeRepository) rebindRoomMembershipsBeforePlacementRemovalInTx(ctx context.Context, tx *sql.Tx, listingID, accountID int64) (*service.AccountShareSeatBillingResult, error) {
	return r.rebindRoomMembershipsBeforePlacementRemovalSetInTx(
		ctx,
		tx,
		listingID,
		[]int64{accountID},
	)
}

func (r *accountShareModeRepository) rebindRoomMembershipsBeforePlacementRemovalSetInTx(
	ctx context.Context,
	tx *sql.Tx,
	listingID int64,
	accountIDs []int64,
) (*service.AccountShareSeatBillingResult, error) {
	accountIDs = uniqueSortedPositiveInt64s(accountIDs)
	if r == nil || tx == nil || listingID <= 0 || len(accountIDs) == 0 {
		return nil, service.ErrAccountShareRoomOperationConflict
	}
	now := time.Now().UTC()
	room, err := lockAccountShareMembershipRebindScopeInTx(ctx, tx, listingID)
	if err != nil {
		return nil, err
	}
	preserveLifecycle, err := accountShareRoomPlacementRemovalLifecycle(room)
	if err != nil {
		return nil, err
	}

	replacementID, replacementErr := healthyRoomAccountIDExcludingInTx(
		ctx,
		tx,
		listingID,
		accountIDs,
		now,
	)
	if replacementErr != nil && !errors.Is(replacementErr, sql.ErrNoRows) {
		return nil, replacementErr
	}

	memberships, err := lockAccountShareMembershipsForAccountSetRebindInTx(
		ctx,
		tx,
		listingID,
		accountIDs,
	)
	if err != nil {
		return nil, err
	}
	if len(memberships) == 0 {
		if replacementErr == nil {
			return nil, nil
		}
		if preserveLifecycle {
			return &service.AccountShareSeatBillingResult{}, nil
		}
		result, err := tx.ExecContext(ctx, `
			UPDATE account_share_listings
			SET status = $1,
				row_version = row_version + 1,
				paused_at = $2,
				status_reason_code = $3,
				status_reason = $4,
				updated_at = $2
			WHERE id = $5
				AND deleted_at IS NULL
		`,
			service.AccountShareListingStatusPaused,
			now,
			accountShareRoomStatusReasonNoAccounts,
			accountShareRoomStatusMessageNoAccounts,
			listingID,
		)
		if err != nil {
			return nil, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if affected != 1 {
			return nil, fmt.Errorf(
				"pause account share room %d without replacement affected %d rows",
				listingID,
				affected,
			)
		}
		if _, _, err := createAccountShareListingRevisionInTx(
			ctx,
			tx,
			listingID,
			0,
			false,
			"account_placement_removal",
			accountShareRoomStatusMessageNoAccounts,
			false,
			"listing.auto_paused",
			map[string]any{
				"removed_account_ids": accountIDs,
				"status_reason_code":  accountShareRoomStatusReasonNoAccounts,
			},
		); err != nil {
			return nil, err
		}
		return &service.AccountShareSeatBillingResult{}, nil
	}

	if err := r.rebindLockedAccountShareMembershipsInTx(
		ctx,
		tx,
		memberships,
		replacementID,
		now,
	); err != nil {
		return nil, err
	}
	return nil, nil
}

func accountShareRoomPlacementRemovalLifecycle(room *lockedAccountShareRoom) (bool, error) {
	if room == nil {
		return false, service.ErrAccountShareRoomOperationConflict
	}
	pendingOperationID := strings.TrimSpace(room.PendingOperationID.String)
	hasPendingOperation := room.PendingOperationID.Valid && pendingOperationID != ""
	switch room.Status {
	case service.AccountShareListingStatusActive,
		service.AccountShareListingStatusPaused:
		if !hasPendingOperation {
			return false, nil
		}
	case service.AccountShareListingStatusDraining:
		if hasPendingOperation {
			return true, nil
		}
	}

	metadata := map[string]string{
		"blocker":          "room_lifecycle_conflict",
		"lifecycle_status": room.Status,
	}
	if pendingOperationID != "" {
		metadata["operation_id"] = pendingOperationID
	}
	return false, service.ErrAccountShareRoomOperationConflict.WithMetadata(metadata)
}

func lockAccountShareMembershipRebindScopeInTx(
	ctx context.Context,
	tx *sql.Tx,
	listingID int64,
) (*lockedAccountShareRoom, error) {
	if tx == nil || listingID <= 0 {
		return nil, service.ErrAccountShareRoomOperationConflict
	}
	room, err := lockAccountShareRoomIdentityInTx(ctx, tx, listingID)
	if err != nil {
		return nil, err
	}
	accountIDs, err := lockAccountShareRoomProjectionInTx(ctx, tx, listingID)
	if err != nil {
		return nil, err
	}
	if err := lockAccountShareAccountsInTx(ctx, tx, accountIDs); err != nil {
		return nil, err
	}
	return room, nil
}

func lockAccountShareMembershipsForRebindInTx(
	ctx context.Context,
	tx *sql.Tx,
	listingID int64,
	currentAccountID int64,
	membershipID int64,
) ([]accountShareMembershipRebindState, error) {
	if tx == nil || listingID <= 0 || currentAccountID <= 0 || membershipID < 0 {
		return nil, service.ErrAccountShareRoomOperationConflict
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, listing_id, account_id, listing_revision_id
		FROM account_share_memberships
		WHERE listing_id = $1
			AND account_id = $2
			AND status = $3
			AND deleted_at IS NULL
			AND ($4::bigint = 0 OR id = $4)
		ORDER BY id ASC
		FOR UPDATE
	`,
		listingID,
		currentAccountID,
		service.AccountShareMembershipStatusActive,
		membershipID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	memberships := make([]accountShareMembershipRebindState, 0)
	for rows.Next() {
		var membership accountShareMembershipRebindState
		if err := rows.Scan(
			&membership.ID,
			&membership.ListingID,
			&membership.AccountID,
			&membership.ListingRevisionID,
		); err != nil {
			return nil, err
		}
		memberships = append(memberships, membership)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return memberships, nil
}

func lockAccountShareMembershipsForAccountSetRebindInTx(
	ctx context.Context,
	tx *sql.Tx,
	listingID int64,
	currentAccountIDs []int64,
) ([]accountShareMembershipRebindState, error) {
	currentAccountIDs = uniqueSortedPositiveInt64s(currentAccountIDs)
	if tx == nil || listingID <= 0 || len(currentAccountIDs) == 0 {
		return nil, service.ErrAccountShareRoomOperationConflict
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, listing_id, account_id, listing_revision_id
		FROM account_share_memberships
		WHERE listing_id = $1
			AND account_id = ANY($2::bigint[])
			AND status = $3
			AND deleted_at IS NULL
		ORDER BY id ASC
		FOR UPDATE
	`,
		listingID,
		pq.Array(currentAccountIDs),
		service.AccountShareMembershipStatusActive,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	memberships := make([]accountShareMembershipRebindState, 0)
	for rows.Next() {
		var membership accountShareMembershipRebindState
		if err := rows.Scan(
			&membership.ID,
			&membership.ListingID,
			&membership.AccountID,
			&membership.ListingRevisionID,
		); err != nil {
			return nil, err
		}
		memberships = append(memberships, membership)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return memberships, nil
}

func lockAccountShareMembershipOpenBindingsForRebindInTx(
	ctx context.Context,
	tx *sql.Tx,
	membershipIDs []int64,
) (map[int64]accountShareMembershipOpenBinding, error) {
	membershipIDs = uniqueSortedPositiveInt64s(membershipIDs)
	bindings := make(map[int64]accountShareMembershipOpenBinding, len(membershipIDs))
	if tx == nil || len(membershipIDs) == 0 {
		return bindings, nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, membership_id, listing_id, account_id_snapshot, listing_revision_id
		FROM account_share_membership_account_bindings
		WHERE membership_id = ANY($1::bigint[])
			AND unbound_at IS NULL
		ORDER BY membership_id ASC, id ASC
		FOR UPDATE
	`, pq.Array(membershipIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var binding accountShareMembershipOpenBinding
		if err := rows.Scan(
			&binding.ID,
			&binding.MembershipID,
			&binding.ListingID,
			&binding.AccountIDSnapshot,
			&binding.ListingRevisionID,
		); err != nil {
			return nil, err
		}
		if previous, exists := bindings[binding.MembershipID]; exists {
			return nil, fmt.Errorf(
				"account share membership %d has multiple open bindings %d and %d",
				binding.MembershipID,
				previous.ID,
				binding.ID,
			)
		}
		bindings[binding.MembershipID] = binding
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return bindings, nil
}

func (r *accountShareModeRepository) rebindLockedAccountShareMembershipsInTx(
	ctx context.Context,
	tx *sql.Tx,
	memberships []accountShareMembershipRebindState,
	replacementAccountID int64,
	now time.Time,
) error {
	if r == nil || tx == nil || len(memberships) == 0 || now.IsZero() {
		return service.ErrAccountShareRoomOperationConflict
	}
	membershipIDs := make([]int64, 0, len(memberships))
	for _, membership := range memberships {
		membershipIDs = append(membershipIDs, membership.ID)
	}
	openBindings, err := lockAccountShareMembershipOpenBindingsForRebindInTx(ctx, tx, membershipIDs)
	if err != nil {
		return err
	}
	if replacementAccountID <= 0 {
		membership := memberships[0]
		return service.ErrAccountShareRoomOperationConflict.WithMetadata(map[string]string{
			"blocker":          "no_healthy_replacement_account",
			"listing_id":       fmt.Sprintf("%d", membership.ListingID),
			"account_id":       fmt.Sprintf("%d", membership.AccountID),
			"membership_id":    fmt.Sprintf("%d", membership.ID),
			"membership_count": fmt.Sprintf("%d", len(memberships)),
		})
	}

	now = now.UTC()
	for _, membership := range memberships {
		if replacementAccountID == membership.AccountID {
			return service.ErrAccountShareRoomOperationConflict.WithMetadata(map[string]string{
				"blocker":       "replacement_matches_current_account",
				"membership_id": fmt.Sprintf("%d", membership.ID),
				"account_id":    fmt.Sprintf("%d", membership.AccountID),
			})
		}
		if binding, exists := openBindings[membership.ID]; exists {
			if binding.ListingID != membership.ListingID ||
				binding.AccountIDSnapshot != membership.AccountID ||
				binding.ListingRevisionID != membership.ListingRevisionID {
				return service.ErrAccountShareRoomOperationConflict.WithMetadata(map[string]string{
					"blocker":       "binding_projection_mismatch",
					"membership_id": fmt.Sprintf("%d", membership.ID),
					"binding_id":    fmt.Sprintf("%d", binding.ID),
				})
			}
		} else {
			if _, _, err := r.createAccountShareMembershipBindingInTx(
				ctx,
				tx,
				membership.ID,
				membership.ListingID,
				membership.AccountID,
				membership.ListingRevisionID,
				0,
				"system",
				accountShareBindingReasonLegacyProjectionMaterialized,
				now,
			); err != nil {
				return err
			}
		}

		closed, err := r.closeAccountShareMembershipBindingInTx(
			ctx,
			tx,
			membership.ID,
			0,
			"system",
			accountShareBindingReasonAccountRebind,
			now,
		)
		if err != nil {
			return err
		}
		if !closed {
			return service.ErrAccountShareRoomOperationConflict.WithMetadata(map[string]string{
				"blocker":       "open_binding_missing",
				"membership_id": fmt.Sprintf("%d", membership.ID),
			})
		}

		result, err := tx.ExecContext(ctx, `
			UPDATE account_share_memberships
			SET account_id = $1,
				updated_at = $2
			WHERE id = $3
				AND listing_id = $4
				AND account_id = $5
				AND listing_revision_id = $6
				AND status = $7
				AND deleted_at IS NULL
		`,
			replacementAccountID,
			now,
			membership.ID,
			membership.ListingID,
			membership.AccountID,
			membership.ListingRevisionID,
			service.AccountShareMembershipStatusActive,
		)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return service.ErrAccountShareRoomOperationConflict.WithMetadata(map[string]string{
				"blocker":       "membership_projection_changed",
				"membership_id": fmt.Sprintf("%d", membership.ID),
			})
		}

		if _, _, err := r.createAccountShareMembershipBindingInTx(
			ctx,
			tx,
			membership.ID,
			membership.ListingID,
			replacementAccountID,
			membership.ListingRevisionID,
			0,
			"system",
			accountShareBindingReasonAccountRebind,
			now,
		); err != nil {
			return err
		}
	}
	return nil
}

func healthyRoomAccountIDExcludingInTx(
	ctx context.Context,
	tx *sql.Tx,
	listingID int64,
	excludeAccountIDs []int64,
	now time.Time,
) (int64, error) {
	excludeAccountIDs = uniqueSortedPositiveInt64s(excludeAccountIDs)
	if tx == nil || listingID <= 0 || len(excludeAccountIDs) == 0 {
		return 0, sql.ErrNoRows
	}
	var accountID int64
	err := tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT a.id
		FROM account_share_room_accounts room_account
		JOIN accounts a ON a.id = room_account.account_id
		WHERE room_account.listing_id = $1
			AND room_account.state = 'active'
			AND NOT (room_account.account_id = ANY($2::bigint[]))
			AND a.deleted_at IS NULL
			AND NOT %s
		ORDER BY room_account.priority ASC, a.last_used_at ASC NULLS FIRST, a.id ASC
		LIMIT 1
	`, accountShareAccountUnavailableConditionSQL("$3")), listingID, pq.Array(excludeAccountIDs), now.UTC()).Scan(&accountID)
	return accountID, err
}

func healthyRoomAccountIDInTx(ctx context.Context, tx *sql.Tx, listingID, excludeAccountID int64, now time.Time) (int64, error) {
	var accountID int64
	err := tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT a.id
		FROM account_share_room_accounts room_account
		JOIN accounts a ON a.id = room_account.account_id
		WHERE room_account.listing_id = $1
			AND room_account.state = 'active'
			AND ($2 <= 0 OR room_account.account_id <> $2)
			AND a.deleted_at IS NULL
			AND NOT %s
		ORDER BY room_account.priority ASC, a.last_used_at ASC NULLS FIRST, a.id ASC
		LIMIT 1
	`, accountShareAccountUnavailableConditionSQL("$3")), listingID, excludeAccountID, now.UTC()).Scan(&accountID)
	return accountID, err
}

func writeAccountExternalPlacementTargetInTx(ctx context.Context, tx *sql.Tx, input service.ConvertAccountExternalPlacementInput, target, platform, accountLevel string, priority int, version int64) error {
	if target == service.AccountExternalPlacementPrivate {
		_, err := tx.ExecContext(ctx, `
			DELETE FROM account_external_placements
			WHERE account_id = $1
		`, input.AccountID)
		return err
	}
	placementType := target
	var listingID, publicGroupID any
	if target == service.AccountExternalPlacementRoom {
		listingID = nil
		publicGroupID = nil
	} else {
		listingID = nil
		publicGroupID = nullableInt64(input.PublicGroupID)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO account_external_placements (
			account_id, owner_user_id, platform, account_level, placement_type,
			listing_id, public_group_id, state, priority, version, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'active', $8, $9, NOW(), NOW())
		ON CONFLICT (account_id) DO UPDATE
		SET owner_user_id = EXCLUDED.owner_user_id,
			platform = EXCLUDED.platform,
			account_level = EXCLUDED.account_level,
			placement_type = EXCLUDED.placement_type,
			listing_id = EXCLUDED.listing_id,
			public_group_id = EXCLUDED.public_group_id,
			state = EXCLUDED.state,
			priority = EXCLUDED.priority,
			version = EXCLUDED.version,
			updated_at = NOW()
	`, input.AccountID, input.OwnerUserID, platform, accountLevel, placementType, listingID, publicGroupID, priority, version); err != nil {
		return translateAccountShareRoomPersistenceError(err)
	}
	return nil
}

func replaceAccountGroupsInTx(ctx context.Context, tx *sql.Tx, accountID int64, groupIDs []int64) error {
	groupIDs = uniqueSortedPositiveInt64s(groupIDs)
	if len(groupIDs) == 0 {
		return service.ErrAccountExternalPlacementInvalid
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM account_groups
		WHERE account_id = $1
	`, accountID); err != nil {
		return err
	}
	for _, groupID := range groupIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO account_groups (account_id, group_id, priority, created_at)
			VALUES ($1, $2, 1, NOW())
		`, accountID, groupID); err != nil {
			return err
		}
	}
	return nil
}

func nextAccountExternalPlacementVersionInTx(ctx context.Context, tx *sql.Tx, accountID int64, current *lockedAccountExternalPlacement, unchanged bool) (int64, error) {
	latest, err := currentAccountExternalPlacementVersionInTx(ctx, tx, accountID, current)
	if err != nil {
		return 0, err
	}
	if unchanged {
		return latest, nil
	}
	return latest + 1, nil
}

func currentAccountExternalPlacementVersionInTx(ctx context.Context, tx *sql.Tx, accountID int64, _ *lockedAccountExternalPlacement) (int64, error) {
	var latest int64
	if err := tx.QueryRowContext(ctx, `
		SELECT GREATEST(
			COALESCE((SELECT MAX(placement_version) FROM account_external_placement_conversions WHERE account_id = $1), 0),
			COALESCE((SELECT version FROM account_external_placements WHERE account_id = $1), 0)
		)
	`, accountID).Scan(&latest); err != nil {
		return 0, err
	}
	return latest, nil
}

func accountExternalPlacementTargetMatches(current *lockedAccountExternalPlacement, target string, roomID, publicGroupID *int64) bool {
	if current == nil {
		return target == service.AccountExternalPlacementPrivate
	}
	if current.Target != target {
		return false
	}
	switch target {
	case service.AccountExternalPlacementRoom:
		return int64PtrEquals(current.RoomID, roomID)
	case service.AccountExternalPlacementPublicPool:
		return int64PtrEquals(current.PublicGroupID, publicGroupID)
	default:
		return false
	}
}

func placementToService(placement any) *service.AccountExternalPlacement {
	switch value := placement.(type) {
	case *lockedAccountExternalPlacement:
		if value == nil {
			return nil
		}
		return &service.AccountExternalPlacement{
			Target:        value.Target,
			RoomID:        value.RoomID,
			RoomName:      value.RoomName,
			PublicGroupID: value.PublicGroupID,
			State:         value.State,
			Version:       value.Version,
		}
	case *service.AccountExternalPlacement:
		return value
	default:
		return nil
	}
}

func privateAccountExternalPlacement(version int64) *service.AccountExternalPlacement {
	return &service.AccountExternalPlacement{
		Target:  service.AccountExternalPlacementPrivate,
		State:   "active",
		Version: version,
	}
}

func samePositiveInt64Set(left, right []int64) bool {
	left = uniqueSortedPositiveInt64s(left)
	right = uniqueSortedPositiveInt64s(right)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func uniqueSortedPositiveInt64s(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	out := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func nullableInt64Equals(value sql.NullInt64, expected *int64) bool {
	if expected == nil {
		return !value.Valid
	}
	return value.Valid && value.Int64 == *expected
}

func int64PtrEquals(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func translateAccountShareRoomPersistenceError(err error) error {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		return err
	}
	if pqErr.Code == "22001" {
		return service.ErrAccountShareModeInvalidName
	}
	switch pqErr.Constraint {
	case "uq_account_share_rooms_owner_name_live":
		return service.ErrAccountShareModeDuplicateName
	case "account_share_room_accounts_pkey",
		"account_share_room_accounts_account_id_key",
		"uq_account_share_room_accounts_account",
		"uq_account_share_room_assignments_open_account":
		return service.ErrAccountShareRoomAccountConflict
	case "account_share_room_accounts_listing_fk":
		return service.ErrAccountShareListingNotFound
	case "account_share_room_accounts_account_fk",
		"account_share_room_accounts_account_identity_fk":
		return service.ErrAccountShareRoomOwnerMismatch
	case "account_share_room_accounts_room_identity_fk":
		return service.ErrAccountShareRoomLevelMismatch
	case "account_external_placements_pkey":
		return service.ErrAccountExternalPlacementConflict
	case "account_external_placements_account_fk":
		return service.ErrAccountShareRoomOwnerMismatch
	case "account_external_placements_room_fk":
		return service.ErrAccountShareRoomLevelMismatch
	default:
		return err
	}
}

func translateAccountExternalPlacementConversionError(err error) error {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Constraint == "account_external_placement_conversions_idempotency_uniq" {
		return service.ErrAccountExternalPlacementIdempotency
	}
	return err
}
