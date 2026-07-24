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
	AllowedModels       []string
	CodexCLIOnly        bool
	Codex5hLimitPercent float64
	Codex7dLimitPercent float64
}

const accountExternalPlacementDrainLease = 2 * time.Minute

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

	var accountName, platform, accountLevel, accountStatus string
	var accountSchedulable bool
	var accountConcurrency, accountPriority int
	if err := tx.QueryRowContext(ctx, `
		SELECT name, platform, account_level, status, schedulable, concurrency, priority
		FROM accounts
		WHERE id = $1
			AND owner_user_id = $2
			AND deleted_at IS NULL
		FOR UPDATE
	`, accountID, ownerUserID).Scan(
		&accountName,
		&platform,
		&accountLevel,
		&accountStatus,
		&accountSchedulable,
		&accountConcurrency,
		&accountPriority,
	); errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareRoomOwnerMismatch
	} else if err != nil {
		return nil, err
	}
	platform = strings.ToLower(strings.TrimSpace(platform))
	accountLevel = service.NormalizeAccountLevel(accountLevel)
	if accountLevel == service.AccountLevelUnknown {
		return nil, service.ErrAccountShareRoomUnknownLevel
	}
	if accountStatus != service.StatusActive || !accountSchedulable {
		return nil, service.ErrAccountShareAccountUnavailable
	}
	if listing.Platform != "" && !strings.EqualFold(strings.TrimSpace(listing.Platform), platform) {
		return nil, service.ErrAccountShareRoomPlatformMismatch
	}
	if listing.AccountLevel != "" && service.NormalizeAccountLevel(listing.AccountLevel) != accountLevel {
		return nil, service.ErrAccountShareRoomLevelMismatch
	}
	roomName := strings.TrimSpace(listing.RoomName)
	if roomName == "" {
		return nil, service.ErrAccountShareModeInvalidName
	}
	allowedModelsJSON, err := json.Marshal(listing.AllowedModels)
	if err != nil {
		return nil, err
	}
	idempotentListingID, err := getIdempotentRoomCreationInTx(ctx, tx, ownerUserID, accountID, idempotencyKey, roomName, listing, string(allowedModelsJSON))
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

	current, err := getAccountExternalPlacementInTx(ctx, tx, accountID, ownerUserID, true)
	if err != nil {
		return nil, err
	}
	if current != nil && current.Target == service.AccountExternalPlacementRoom {
		return nil, service.ErrAccountExternalPlacementConflict
	}
	if current != nil && current.State != "draining" {
		return nil, service.ErrAccountExternalPlacementBusy
	}
	previousVersion, err := currentAccountExternalPlacementVersionInTx(ctx, tx, accountID, current)
	if err != nil {
		return nil, err
	}
	version := previousVersion + 1
	privateGroupID, err := accountOwnerPrivateGroupIDInTx(ctx, tx, ownerUserID, platform)
	if err != nil {
		return nil, err
	}
	if err := validateAccountShareModeGroupInTx(ctx, tx, modeGroupID, platform); err != nil {
		return nil, err
	}

	account := &service.Account{
		ID:           accountID,
		Name:         accountName,
		Platform:     platform,
		AccountLevel: accountLevel,
		OwnerUserID:  &ownerUserID,
	}
	accountIdentityID, err := ensureAccountShareAccountIdentityInTx(ctx, tx, account)
	if err != nil {
		return nil, err
	}
	var listingID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO account_share_listings (
			account_id, owner_user_id, room_name, platform, account_level,
			status, seat_limit, rate_multiplier, allowed_models,
			per_user_concurrency, hourly_rate, hourly_fee_waiver_minimum,
			min_balance_required, codex_cli_only, codex_5h_limit_percent,
			codex_7d_limit_percent, account_identity_id, created_at, updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9::jsonb,
			$10, $11, $12,
			$13, $14, $15,
			$16, $17, NOW(), NOW()
		)
		RETURNING id
	`,
		accountID,
		ownerUserID,
		roomName,
		platform,
		accountLevel,
		service.AccountShareListingStatusActive,
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

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM account_external_placements
		WHERE account_id = $1
	`, accountID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO account_external_placements (
			account_id, owner_user_id, platform, account_level,
			placement_type, listing_id, state, priority, version, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, 'room', $5, 'active', $6, $7, NOW(), NOW())
	`, accountID, ownerUserID, platform, accountLevel, listingID, accountPriority, version); err != nil {
		return nil, translateAccountShareRoomPersistenceError(err)
	}
	if err := syncRoomAccountConfigInTx(ctx, tx, accountID, platform, &lockedAccountShareRoom{
		ID:                  listingID,
		OwnerUserID:         ownerUserID,
		Platform:            platform,
		AccountLevel:        accountLevel,
		Status:              service.AccountShareListingStatusActive,
		AllowedModels:       append([]string(nil), listing.AllowedModels...),
		CodexCLIOnly:        listing.CodexCLIOnly,
		Codex5hLimitPercent: listing.Codex5hLimitPercent,
		Codex7dLimitPercent: listing.Codex7dLimitPercent,
	}); err != nil {
		return nil, err
	}
	if err := replaceAccountGroupsInTx(ctx, tx, accountID, []int64{privateGroupID, modeGroupID}); err != nil {
		return nil, err
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
		return nil, err
	}
	if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account_share_room", "[SchedulerOutbox] enqueue room account change failed: account=%d err=%v", accountID, err)
	}
	if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountGroupsChanged, &accountID, nil, buildSchedulerGroupPayload([]int64{privateGroupID, modeGroupID})); err != nil {
		logger.LegacyPrintf("repository.account_share_room", "[SchedulerOutbox] enqueue room account group change failed: account=%d err=%v", accountID, err)
	}
	previousPlacement := placementToService(current)
	if previousPlacement == nil {
		previousPlacement = privateAccountExternalPlacement(previousVersion)
	}
	conversionResult := &service.ConvertAccountExternalPlacementResult{
		AccountID: accountID,
		Previous:  previousPlacement,
		Current: &service.AccountExternalPlacement{
			Target:   service.AccountExternalPlacementRoom,
			RoomID:   &listingID,
			RoomName: roomName,
			State:    "active",
			Version:  version,
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
	`, ownerUserID, accountID, idempotencyKey, listingID, version, string(resultJSON)); err != nil {
		return nil, translateAccountExternalPlacementConversionError(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return r.GetListingByID(ctx, listingID, ownerUserID)
}

func (r *accountShareModeRepository) ListRoomAccounts(ctx context.Context, listingID, viewerUserID int64) ([]service.AccountShareRoomAccount, error) {
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
	if ownerUserID != viewerUserID {
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
			placement.priority,
			placement.state,
			a.last_used_at
		FROM account_external_placements placement
		JOIN accounts a ON a.id = placement.account_id
		WHERE placement.listing_id = $1
			AND placement.placement_type = 'room'
			AND a.deleted_at IS NULL
		ORDER BY placement.priority ASC, a.id ASC
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
	if target == service.AccountExternalPlacementRoom && accountLevel == service.AccountLevelUnknown {
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
		if input.RoomID == nil || *input.RoomID <= 0 {
			return nil, service.ErrAccountExternalPlacementInvalid
		}
		roomIDs = append(roomIDs, *input.RoomID)
	} else if input.RoomID != nil {
		return nil, service.ErrAccountExternalPlacementInvalid
	}
	rooms, err := lockAccountShareRoomsInTx(ctx, tx, roomIDs)
	if err != nil {
		return nil, err
	}
	var targetRoom *lockedAccountShareRoom
	if target == service.AccountExternalPlacementRoom {
		room := rooms[*input.RoomID]
		if room == nil {
			return nil, service.ErrAccountShareListingNotFound
		}
		targetRoom = room
		if room.OwnerUserID != input.OwnerUserID {
			return nil, service.ErrAccountShareRoomOwnerMismatch
		}
		if room.Platform != platform {
			return nil, service.ErrAccountShareRoomPlatformMismatch
		}
		if service.NormalizeAccountLevel(room.AccountLevel) != accountLevel {
			return nil, service.ErrAccountShareRoomLevelMismatch
		}
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
		if err := writeAccountExternalPlacementTargetInTx(ctx, tx, input, target, platform, accountLevel, accountPriority, version, targetRoom); err != nil {
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

func (r *accountShareModeRepository) RebindMembershipToHealthyRoomAccount(ctx context.Context, membershipID, currentAccountID int64, now time.Time) (bool, error) {
	if membershipID <= 0 || currentAccountID <= 0 {
		return false, nil
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
	var listingID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT listing_id
		FROM account_share_memberships
		WHERE id = $1
			AND account_id = $2
			AND status IN ($3, $4)
			AND deleted_at IS NULL
		FOR UPDATE
	`, membershipID, currentAccountID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued).Scan(&listingID); errors.Is(err, sql.ErrNoRows) {
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
	if _, err := tx.ExecContext(ctx, `
		UPDATE account_share_memberships
		SET account_id = $1,
			updated_at = NOW()
		WHERE id = $2
			AND account_id = $3
			AND status IN ($4, $5)
			AND deleted_at IS NULL
	`, replacementID, membershipID, currentAccountID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued); err != nil {
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
		SELECT id, owner_user_id, platform, account_level, status,
			allowed_models, codex_cli_only, codex_5h_limit_percent, codex_7d_limit_percent
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

func getIdempotentRoomCreationInTx(
	ctx context.Context,
	tx *sql.Tx,
	ownerUserID, accountID int64,
	idempotencyKey, roomName string,
	listing *service.AccountShareListing,
	allowedModelsJSON string,
) (int64, error) {
	var storedAccountID int64
	var storedTarget string
	var storedListingID, storedPublicGroupID sql.NullInt64
	var payloadMatches bool
	err := tx.QueryRowContext(ctx, `
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
	replacementID, err := healthyRoomAccountIDInTx(ctx, tx, listingID, accountID, time.Now().UTC())
	if errors.Is(err, sql.ErrNoRows) {
		now := time.Now().UTC()
		if _, updateErr := tx.ExecContext(ctx, `
			UPDATE account_share_listings
			SET status = $1,
				account_id = NULL,
				updated_at = NOW()
			WHERE id = $2
				AND deleted_at IS NULL
		`, service.AccountShareListingStatusPaused, listingID); updateErr != nil {
			return nil, updateErr
		}
		rows, queryErr := tx.QueryContext(ctx, `
			SELECT id, status
			FROM account_share_memberships
			WHERE listing_id = $1
				AND account_id = $2
				AND status IN ($3, $4)
				AND deleted_at IS NULL
			ORDER BY consumer_user_id ASC, id ASC
		`, listingID, accountID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued)
		if queryErr != nil {
			return nil, queryErr
		}
		type membershipState struct {
			ID     int64
			Status string
		}
		memberships := make([]membershipState, 0)
		for rows.Next() {
			var membership membershipState
			if scanErr := rows.Scan(&membership.ID, &membership.Status); scanErr != nil {
				_ = rows.Close()
				return nil, scanErr
			}
			memberships = append(memberships, membership)
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			_ = rows.Close()
			return nil, rowsErr
		}
		if closeErr := rows.Close(); closeErr != nil {
			return nil, closeErr
		}
		result := &service.AccountShareSeatBillingResult{}
		queuedIDs := make([]int64, 0)
		for _, item := range memberships {
			if item.Status == service.AccountShareMembershipStatusQueued {
				queuedIDs = append(queuedIDs, item.ID)
				continue
			}
			membership, lockErr := r.lockSeatBillingMembershipInTx(ctx, tx, item.ID, 0)
			if errors.Is(lockErr, sql.ErrNoRows) {
				continue
			}
			if lockErr != nil {
				return nil, lockErr
			}
			itemResult, endErr := r.endSeatBillingMembershipInTx(
				ctx,
				tx,
				membership,
				now,
				service.AccountShareMembershipEndReasonUnavailable,
			)
			if endErr != nil {
				return nil, endErr
			}
			if itemResult != nil {
				result.DebitUserIDs = append(result.DebitUserIDs, itemResult.DebitUserIDs...)
				result.CreditUserIDs = append(result.CreditUserIDs, itemResult.CreditUserIDs...)
				result.EndedConsumerUserIDs = append(result.EndedConsumerUserIDs, itemResult.EndedConsumerUserIDs...)
			}
		}
		if len(queuedIDs) > 0 {
			queuedRows, updateErr := tx.QueryContext(ctx, `
				UPDATE account_share_memberships
				SET status = $1,
					ended_at = $2,
					ended_reason = $3,
					paid_until = $2,
					billed_until = $2,
					waiver_window_started_at = $2,
					waiver_window_usage_amount = 0,
					waiver_window_request_count = 0,
					waiver_window_last_request_at = NULL,
					dispatch_cooldown_until = NULL,
					updated_at = NOW()
				WHERE id = ANY($4)
					AND status = $5
					AND deleted_at IS NULL
				RETURNING consumer_user_id
			`,
				service.AccountShareMembershipStatusEnded,
				now,
				service.AccountShareMembershipEndReasonUnavailable,
				pq.Array(queuedIDs),
				service.AccountShareMembershipStatusQueued,
			)
			if updateErr != nil {
				return nil, updateErr
			}
			for queuedRows.Next() {
				var consumerUserID int64
				if scanErr := queuedRows.Scan(&consumerUserID); scanErr != nil {
					_ = queuedRows.Close()
					return nil, scanErr
				}
				result.EndedConsumerUserIDs = append(result.EndedConsumerUserIDs, consumerUserID)
			}
			if rowsErr := queuedRows.Err(); rowsErr != nil {
				_ = queuedRows.Close()
				return nil, rowsErr
			}
			if closeErr := queuedRows.Close(); closeErr != nil {
				return nil, closeErr
			}
		}
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE account_share_listings
		SET account_id = $1,
			updated_at = NOW()
		WHERE id = $2
			AND deleted_at IS NULL
	`, replacementID, listingID); err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE account_share_memberships
		SET account_id = $1,
			updated_at = NOW()
		WHERE listing_id = $2
			AND account_id = $3
			AND status IN ($4, $5)
			AND deleted_at IS NULL
	`, replacementID, listingID, accountID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued)
	return nil, err
}

func healthyRoomAccountIDInTx(ctx context.Context, tx *sql.Tx, listingID, excludeAccountID int64, now time.Time) (int64, error) {
	var accountID int64
	err := tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT a.id
		FROM account_external_placements placement
		JOIN accounts a ON a.id = placement.account_id
		WHERE placement.listing_id = $1
			AND placement.placement_type = 'room'
			AND placement.state = 'active'
			AND ($2 <= 0 OR placement.account_id <> $2)
			AND a.deleted_at IS NULL
			AND NOT %s
		ORDER BY placement.priority ASC, a.last_used_at ASC NULLS FIRST, a.id ASC
		LIMIT 1
	`, accountShareAccountUnavailableConditionSQL("$3")), listingID, excludeAccountID, now.UTC()).Scan(&accountID)
	return accountID, err
}

func syncRoomAccountConfigInTx(ctx context.Context, tx *sql.Tx, accountID int64, platform string, room *lockedAccountShareRoom) error {
	if tx == nil || accountID <= 0 || room == nil || room.ID <= 0 {
		return service.ErrAccountExternalPlacementInvalid
	}
	modelMappingJSON, err := json.Marshal(service.AccountShareModeAllowedModelsMapping(room.AllowedModels))
	if err != nil {
		return err
	}
	platform = strings.ToLower(strings.TrimSpace(platform))
	var result sql.Result
	switch platform {
	case service.PlatformAnthropic:
		result, err = tx.ExecContext(ctx, `
			UPDATE accounts
			SET credentials = jsonb_set(COALESCE(credentials, '{}'::jsonb), '{model_mapping}', $1::jsonb, true),
				extra = COALESCE(extra, '{}'::jsonb) || jsonb_build_object(
					'anthropic_5h_limit_percent', $2,
					'anthropic_7d_limit_percent', $3
				),
				updated_at = NOW()
			WHERE id = $4
				AND owner_user_id = $5
				AND deleted_at IS NULL
		`, string(modelMappingJSON), room.Codex5hLimitPercent, room.Codex7dLimitPercent, accountID, room.OwnerUserID)
	default:
		result, err = tx.ExecContext(ctx, `
			UPDATE accounts
			SET credentials = jsonb_set(COALESCE(credentials, '{}'::jsonb), '{model_mapping}', $1::jsonb, true),
				extra = COALESCE(extra, '{}'::jsonb) || jsonb_build_object(
					'codex_cli_only', $2,
					'codex_5h_limit_percent', $3,
					'codex_7d_limit_percent', $4
				),
				updated_at = NOW()
			WHERE id = $5
				AND owner_user_id = $6
				AND deleted_at IS NULL
		`, string(modelMappingJSON), room.CodexCLIOnly, room.Codex5hLimitPercent, room.Codex7dLimitPercent, accountID, room.OwnerUserID)
	}
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return service.ErrAccountShareRoomOwnerMismatch
	}
	return nil
}

func writeAccountExternalPlacementTargetInTx(ctx context.Context, tx *sql.Tx, input service.ConvertAccountExternalPlacementInput, target, platform, accountLevel string, priority int, version int64, targetRoom *lockedAccountShareRoom) error {
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
		if targetRoom == nil {
			return service.ErrAccountShareListingNotFound
		}
		listingID = targetRoom.ID
		publicGroupID = nil
		if err := syncRoomAccountConfigInTx(ctx, tx, input.AccountID, platform, targetRoom); err != nil {
			return err
		}
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
	if target == service.AccountExternalPlacementRoom {
		if _, err := tx.ExecContext(ctx, `
			UPDATE account_share_listings
			SET status = CASE
					WHEN status = $1
						AND NOT EXISTS (
							SELECT 1
							FROM account_external_placements other
							WHERE other.listing_id = account_share_listings.id
								AND other.account_id <> $2
								AND other.placement_type = 'room'
						)
					THEN $3
					ELSE status
				END,
				updated_at = NOW()
			WHERE id = $4
				AND deleted_at IS NULL
		`, service.AccountShareListingStatusPaused, input.AccountID, service.AccountShareListingStatusActive, targetRoom.ID); err != nil {
			return err
		}
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
	switch pqErr.Constraint {
	case "uq_account_share_rooms_owner_name_live":
		return service.ErrAccountShareModeDuplicateName
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
