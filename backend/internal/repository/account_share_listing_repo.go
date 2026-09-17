package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
)

type accountShareListingRevisionSnapshot struct {
	ID                     int64
	ListingID              int64
	RowVersion             int64
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
	AllowedModels          []string
	PerUserConcurrency     int
	HourlyRate             float64
	HourlyFeeWaiverMinimum float64
	MinBalanceRequired     float64
	CodexCLIOnly           bool
	Codex5hLimitPercent    float64
	Codex7dLimitPercent    float64
}

func accountShareRevisionActorRole(actorUserID int64, actorIsAdmin bool) string {
	if actorUserID <= 0 {
		return "system"
	}
	if actorIsAdmin {
		return "admin"
	}
	return "owner"
}

func createAccountShareListingRevisionInTx(
	ctx context.Context,
	tx *sql.Tx,
	listingID int64,
	actorUserID int64,
	actorIsAdmin bool,
	source string,
	reason string,
	forceApplied bool,
	eventType string,
	eventPayload map[string]any,
	operationIDs ...string,
) (int64, int64, error) {
	if tx == nil || listingID <= 0 {
		return 0, 0, service.ErrAccountShareListingNotFound
	}
	var snapshot accountShareListingRevisionSnapshot
	var allowedModelsRaw []byte
	var platform, accountLevel sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT
			l.id, l.row_version, COALESCE(l.room_name, ''), l.platform, l.account_level,
			l.owner_user_id, COALESCE(u.username, ''), l.status,
			l.seat_limit, l.rate_multiplier, l.allowed_models, l.per_user_concurrency,
			l.hourly_rate, l.hourly_fee_waiver_minimum, l.min_balance_required,
			l.codex_cli_only, l.codex_5h_limit_percent, l.codex_7d_limit_percent
		FROM account_share_listings l
		LEFT JOIN users u ON u.id = l.owner_user_id
		WHERE l.id = $1
			AND l.deleted_at IS NULL
		FOR UPDATE OF l
	`, listingID).Scan(
		&snapshot.ListingID,
		&snapshot.RowVersion,
		&snapshot.RoomName,
		&platform,
		&accountLevel,
		&snapshot.OwnerUserID,
		&snapshot.OwnerDisplayName,
		&snapshot.Status,
		&snapshot.SeatLimit,
		&snapshot.RateMultiplier,
		&allowedModelsRaw,
		&snapshot.PerUserConcurrency,
		&snapshot.HourlyRate,
		&snapshot.HourlyFeeWaiverMinimum,
		&snapshot.MinBalanceRequired,
		&snapshot.CodexCLIOnly,
		&snapshot.Codex5hLimitPercent,
		&snapshot.Codex7dLimitPercent,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return 0, 0, err
	}
	snapshot.Platform = strings.ToLower(strings.TrimSpace(platform.String))
	snapshot.AccountLevel = service.NormalizeAccountLevel(accountLevel.String)
	snapshot.SchemaVersion = 1
	snapshot.SnapshotQuality = service.AccountShareSnapshotQualityExact
	snapshot.OwnerDisplayName = strings.TrimSpace(snapshot.OwnerDisplayName)
	if err := json.Unmarshal(allowedModelsRaw, &snapshot.AllowedModels); err != nil {
		return 0, 0, err
	}
	allowedModelsJSON, err := json.Marshal(snapshot.AllowedModels)
	if err != nil {
		return 0, 0, err
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = "update"
	}
	reason = strings.TrimSpace(reason)
	actorRole := accountShareRevisionActorRole(actorUserID, actorIsAdmin)
	var actor any
	if actorUserID > 0 {
		actor = actorUserID
	}
	var operationID any
	if len(operationIDs) > 0 {
		if normalized := strings.TrimSpace(operationIDs[0]); normalized != "" {
			operationID = normalized
		}
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO account_share_listing_revisions (
			listing_id, revision_number, schema_version, snapshot_quality,
			room_name, platform, account_level, owner_user_id, owner_display_name_snapshot, status,
			seat_limit, rate_multiplier, allowed_models, per_user_concurrency,
			hourly_rate, hourly_fee_waiver_minimum, min_balance_required,
			codex_cli_only, codex_5h_limit_percent, codex_7d_limit_percent,
			created_by_user_id, created_by_role, source, change_reason, operation_id, force_applied, created_at
		)
		VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8, $9, $10,
			$11, $12, $13::jsonb, $14,
			$15, $16, $17,
			$18, $19, $20,
			$21, $22, $23, $24, $25::uuid, $26, NOW()
		)
		RETURNING id
	`,
		snapshot.ListingID,
		snapshot.RowVersion,
		snapshot.SchemaVersion,
		snapshot.SnapshotQuality,
		snapshot.RoomName,
		nullableEmptyString(snapshot.Platform),
		nullableEmptyString(snapshot.AccountLevel),
		snapshot.OwnerUserID,
		snapshot.OwnerDisplayName,
		snapshot.Status,
		snapshot.SeatLimit,
		snapshot.RateMultiplier,
		string(allowedModelsJSON),
		snapshot.PerUserConcurrency,
		snapshot.HourlyRate,
		snapshot.HourlyFeeWaiverMinimum,
		snapshot.MinBalanceRequired,
		snapshot.CodexCLIOnly,
		snapshot.Codex5hLimitPercent,
		snapshot.Codex7dLimitPercent,
		actor,
		actorRole,
		source,
		nullableEmptyString(reason),
		operationID,
		forceApplied,
	).Scan(&snapshot.ID)
	if err != nil {
		return 0, 0, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE account_share_listings
		SET current_revision_id = $1
		WHERE id = $2
			AND row_version = $3
			AND deleted_at IS NULL
	`, snapshot.ID, snapshot.ListingID, snapshot.RowVersion)
	if err != nil {
		return 0, 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, 0, err
	}
	if affected != 1 {
		return 0, 0, fmt.Errorf(
			"account share listing %d revision pointer update affected %d rows for version %d",
			snapshot.ListingID,
			affected,
			snapshot.RowVersion,
		)
	}
	if eventType == "" {
		eventType = "listing.updated"
	}
	if eventPayload == nil {
		eventPayload = map[string]any{}
	}
	eventPayload["row_version"] = snapshot.RowVersion
	eventPayload["source"] = source
	eventPayload["force_applied"] = forceApplied
	eventPayloadJSON, err := json.Marshal(eventPayload)
	if err != nil {
		return 0, 0, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO account_share_room_events (
			listing_id, revision_id, event_type, actor_user_id, actor_role,
			reason, payload, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, NOW())
	`,
		snapshot.ListingID,
		snapshot.ID,
		eventType,
		actor,
		actorRole,
		nullableEmptyString(reason),
		string(eventPayloadJSON),
	); err != nil {
		return 0, 0, err
	}
	return snapshot.ID, snapshot.RowVersion, nil
}

func ensureAccountShareListingRevisionInTx(ctx context.Context, tx *sql.Tx, listingID int64) (int64, int64, error) {
	var currentRevisionID, currentRevisionNumber sql.NullInt64
	var rowVersion int64
	err := tx.QueryRowContext(ctx, `
		SELECT l.current_revision_id, l.row_version, revision.revision_number
		FROM account_share_listings l
		LEFT JOIN account_share_listing_revisions revision
			ON revision.id = l.current_revision_id
			AND revision.listing_id = l.id
		WHERE l.id = $1
			AND l.deleted_at IS NULL
		FOR UPDATE OF l
	`, listingID).Scan(&currentRevisionID, &rowVersion, &currentRevisionNumber)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return 0, 0, err
	}
	if currentRevisionID.Valid && currentRevisionID.Int64 > 0 {
		if !currentRevisionNumber.Valid || currentRevisionNumber.Int64 != rowVersion {
			return 0, 0, fmt.Errorf(
				"account share listing %d revision pointer mismatch: row_version=%d revision_number=%d revision_valid=%t",
				listingID,
				rowVersion,
				currentRevisionNumber.Int64,
				currentRevisionNumber.Valid,
			)
		}
		return currentRevisionID.Int64, rowVersion, nil
	}
	return createAccountShareListingRevisionInTx(
		ctx,
		tx,
		listingID,
		0,
		false,
		"legacy_join_materialization",
		"",
		false,
		"listing.revision_materialized",
		nil,
	)
}

func loadAccountShareListingRevisionSnapshotInTx(ctx context.Context, tx *sql.Tx, listingID, revisionID int64) (*accountShareListingRevisionSnapshot, error) {
	snapshot := &accountShareListingRevisionSnapshot{}
	var allowedModelsRaw []byte
	var platform, accountLevel sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT
			id, listing_id, revision_number, schema_version, snapshot_quality,
			room_name, platform, account_level, owner_user_id, owner_display_name_snapshot, status,
			seat_limit, rate_multiplier, allowed_models, per_user_concurrency,
			hourly_rate, hourly_fee_waiver_minimum, min_balance_required,
			codex_cli_only, codex_5h_limit_percent, codex_7d_limit_percent
		FROM account_share_listing_revisions
		WHERE id = $1
			AND listing_id = $2
	`, revisionID, listingID).Scan(
		&snapshot.ID,
		&snapshot.ListingID,
		&snapshot.RowVersion,
		&snapshot.SchemaVersion,
		&snapshot.SnapshotQuality,
		&snapshot.RoomName,
		&platform,
		&accountLevel,
		&snapshot.OwnerUserID,
		&snapshot.OwnerDisplayName,
		&snapshot.Status,
		&snapshot.SeatLimit,
		&snapshot.RateMultiplier,
		&allowedModelsRaw,
		&snapshot.PerUserConcurrency,
		&snapshot.HourlyRate,
		&snapshot.HourlyFeeWaiverMinimum,
		&snapshot.MinBalanceRequired,
		&snapshot.CodexCLIOnly,
		&snapshot.Codex5hLimitPercent,
		&snapshot.Codex7dLimitPercent,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, err
	}
	snapshot.Platform = strings.ToLower(strings.TrimSpace(platform.String))
	snapshot.AccountLevel = service.NormalizeAccountLevel(accountLevel.String)
	snapshot.SnapshotQuality = strings.TrimSpace(snapshot.SnapshotQuality)
	snapshot.OwnerDisplayName = strings.TrimSpace(snapshot.OwnerDisplayName)
	if err := json.Unmarshal(allowedModelsRaw, &snapshot.AllowedModels); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (r *accountShareModeRepository) EnsureListingRevisionTerms(
	ctx context.Context,
	listingID int64,
) (*service.AccountShareListingTermsSnapshot, error) {
	if r == nil || r.db == nil || listingID <= 0 {
		return nil, service.ErrAccountShareListingNotFound
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

	revisionID, _, err := ensureAccountShareListingRevisionInTx(ctx, tx, listingID)
	if err != nil {
		return nil, err
	}
	revision, err := loadAccountShareListingRevisionSnapshotInTx(ctx, tx, listingID, revisionID)
	if err != nil {
		return nil, err
	}
	terms := revision.termsSnapshot()
	if terms == nil {
		return nil, fmt.Errorf("account share listing %d revision terms are unavailable", listingID)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return terms, nil
}

func (r *accountShareModeRepository) EnsureModeGroup(ctx context.Context, platform string) (*service.Group, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform == "" {
		platform = service.PlatformOpenAI
	}
	if group, err := r.GetModeGroup(ctx, platform); err == nil {
		return group, nil
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

	groupName := accountShareModeGroupName(platform)
	var groupID int64
	err = tx.QueryRowContext(ctx, `
		SELECT id
		FROM groups
		WHERE name = $1 AND deleted_at IS NULL
		ORDER BY id ASC
		LIMIT 1
	`, groupName).Scan(&groupID)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `
			INSERT INTO groups (
				name, description, rate_multiplier, is_exclusive, status, owner_user_id,
				scope, platform, required_account_level, subscription_type, default_validity_days,
				allow_image_generation, image_rate_independent, image_rate_multiplier,
				claude_code_only, model_routing, model_routing_enabled, mcp_xml_inject,
				supported_model_scopes, sort_order, allow_messages_dispatch, require_oauth_only,
				require_privacy_set, default_mapped_model, messages_dispatch_model_config,
				rpm_limit, created_at, updated_at
			)
			VALUES (
				$1, $2, 1.0, FALSE, $3, NULL,
				$4, $5, '', $6, 30,
				FALSE, FALSE, 1.0,
				FALSE, '{}'::jsonb, FALSE, TRUE,
				'[]'::jsonb, -900, TRUE, TRUE,
				FALSE, '', '{}'::jsonb,
				0, NOW(), NOW()
			)
			RETURNING id
		`,
			groupName,
			"统一账号共享模式分组；倍率由消费者绑定的共享账号动态决定。",
			service.StatusActive,
			service.GroupScopePublic,
			platform,
			service.SubscriptionTypeStandard,
		).Scan(&groupID)
	}
	if err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO account_share_mode_groups (platform, group_id, created_at, updated_at)
		VALUES ($1, $2, NOW(), NOW())
		ON CONFLICT (platform) DO UPDATE
		SET group_id = EXCLUDED.group_id,
			updated_at = NOW()
	`, platform, groupID); err != nil {
		return nil, err
	}
	if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventGroupChanged, nil, &groupID, nil); err != nil {
		logger.LegacyPrintf("repository.account_share_mode", "[SchedulerOutbox] enqueue mode group ensure failed: group=%d err=%v", groupID, err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return r.scanGroupByID(ctx, groupID)
}

func (r *accountShareModeRepository) GetModeGroup(ctx context.Context, platform string) (*service.Group, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	var groupID int64
	err := r.db.QueryRowContext(ctx, `
		SELECT g.id
		FROM account_share_mode_groups mg
		JOIN groups g ON g.id = mg.group_id AND g.deleted_at IS NULL
		WHERE mg.platform = $1
	`, platform).Scan(&groupID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareModeGroupUnavailable
	}
	if err != nil {
		return nil, err
	}
	return r.scanGroupByID(ctx, groupID)
}

func (r *accountShareModeRepository) IsModeGroup(ctx context.Context, groupID int64) (bool, error) {
	if groupID <= 0 {
		return false, nil
	}
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM account_share_mode_groups mg
			JOIN groups g ON g.id = mg.group_id AND g.deleted_at IS NULL
			WHERE mg.group_id = $1
		)
	`, groupID).Scan(&exists)
	return exists, err
}

func (r *accountShareModeRepository) EnsureListingNameAvailable(ctx context.Context, ownerUserID int64, accountName string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	if err := ensureAccountShareListingNameAvailable(ctx, tx, ownerUserID, accountName); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	tx = nil
	return nil
}

func (r *accountShareModeRepository) CreatePlatformListing(ctx context.Context, account *service.Account, listing *service.AccountShareListing, modeGroupID int64) (*service.AccountShareListing, error) {
	if account == nil || listing == nil || modeGroupID <= 0 {
		return nil, service.ErrAccountNilInput
	}
	if service.NormalizeAccountShareMode(account.ShareMode) == service.AccountShareModePublic {
		return nil, service.ErrAccountShareModePublicPoolAccount
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

	credentialsJSON, err := json.Marshal(normalizeJSONMap(account.Credentials))
	if err != nil {
		return nil, err
	}
	extraJSON, err := json.Marshal(normalizeJSONMap(account.Extra))
	if err != nil {
		return nil, err
	}
	accountRateMultiplier := 1.0
	if account.RateMultiplier != nil {
		accountRateMultiplier = *account.RateMultiplier
	}
	ownerUserID := derefInt64(account.OwnerUserID)
	if ownerUserID <= 0 {
		return nil, service.ErrAccountShareRoomOwnerMismatch
	}
	if err := lockAccountShareOwnerQuotaInTx(ctx, tx, ownerUserID); err != nil {
		return nil, err
	}
	if err := r.enforceAccountShareRoomCreationQuotaInTx(ctx, tx, ownerUserID); err != nil {
		return nil, err
	}
	if err := ensureAccountShareListingNameAvailable(ctx, tx, ownerUserID, account.Name); err != nil {
		return nil, err
	}
	privateGroupID, err := accountOwnerPrivateGroupIDInTx(ctx, tx, ownerUserID, strings.ToLower(strings.TrimSpace(account.Platform)))
	if err != nil {
		return nil, err
	}
	if err := validateAccountShareModeGroupInTx(ctx, tx, modeGroupID, strings.ToLower(strings.TrimSpace(account.Platform))); err != nil {
		return nil, err
	}
	if account.ProxyID != nil {
		if err := ensureAccountShareProxyCapacityInTx(ctx, tx, ownerUserID, *account.ProxyID, 0); err != nil {
			return nil, err
		}
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO accounts (
			name, notes, platform, account_level, type, credentials, extra,
			owner_user_id, share_mode, share_status, proxy_id, concurrency,
			load_factor, load_factor_paid_ceiling, priority, rate_multiplier,
			status, error_message, expires_at, auto_pause_on_expired, schedulable,
			created_at, updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6::jsonb, $7::jsonb,
			$8, $9, $10, $11, $12,
			$13, $14, $15, $16,
			$17, $18, $19, $20, $21,
			NOW(), NOW()
		)
		RETURNING id, created_at, updated_at
	`,
		account.Name,
		nullableString(account.Notes),
		account.Platform,
		service.NormalizeAccountLevel(account.AccountLevel),
		account.Type,
		string(credentialsJSON),
		string(extraJSON),
		nullableInt64(account.OwnerUserID),
		service.NormalizeAccountShareMode(account.ShareMode),
		service.NormalizeAccountShareStatus(account.ShareStatus),
		nullableInt64(account.ProxyID),
		account.Concurrency,
		nullableInt(account.LoadFactor),
		normalizeLoadFactorPaidCeiling(account.LoadFactorPaidCeiling),
		account.Priority,
		accountRateMultiplier,
		account.Status,
		nullableEmptyString(account.ErrorMessage),
		nullableTimePtr(account.ExpiresAt),
		account.AutoPauseOnExpired,
		account.Schedulable,
	).Scan(&account.ID, &account.CreatedAt, &account.UpdatedAt)
	if err != nil {
		return nil, translateAccountPersistenceError(err, service.ErrAccountNotFound)
	}

	groupIDs := []int64{privateGroupID, modeGroupID}
	if err := replaceAccountGroupsInTx(ctx, tx, account.ID, groupIDs); err != nil {
		return nil, err
	}
	account.GroupIDs = append([]int64(nil), groupIDs...)

	accountIdentityID, err := ensureAccountShareAccountIdentityInTx(ctx, tx, account)
	if err != nil {
		return nil, err
	}
	if accountIdentityID != nil {
		listing.AccountIdentityID = accountIdentityID
	}

	listing.AccountID = account.ID
	listing.OwnerUserID = ownerUserID
	listing.RoomName = strings.TrimSpace(account.Name)
	listing.Platform = strings.ToLower(strings.TrimSpace(account.Platform))
	listing.AccountLevel = service.NormalizeAccountLevel(account.AccountLevel)
	if listing.Status == "" {
		listing.Status = service.AccountShareListingStatusValidating
	}
	if listing.AccountConcurrency <= 0 {
		listing.AccountConcurrency = account.Concurrency
	}
	allowedModelsJSON, err := json.Marshal(listing.AllowedModels)
	if err != nil {
		return nil, err
	}
	var listingID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO account_share_listings (
			owner_user_id, room_name, platform, account_level,
			status, seat_limit, rate_multiplier, allowed_models,
			per_user_concurrency, hourly_rate, hourly_fee_waiver_minimum, min_balance_required, codex_cli_only,
			codex_5h_limit_percent, codex_7d_limit_percent, account_identity_id, join_password_hash, created_at, updated_at
		)
		VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8::jsonb,
			$9, $10, $11, $12, $13,
			$14, $15, $16, $17, NOW(), NOW()
		)
		RETURNING id
	`,
		listing.OwnerUserID,
		listing.RoomName,
		listing.Platform,
		listing.AccountLevel,
		listing.Status,
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
		nullableInt64(listing.AccountIdentityID),
		nullableEmptyString(listing.JoinPasswordHash),
	).Scan(&listingID)
	if err != nil {
		return nil, err
	}
	revisionID, rowVersion, err := createAccountShareListingRevisionInTx(
		ctx,
		tx,
		listingID,
		ownerUserID,
		false,
		"create_platform_listing",
		"",
		false,
		"listing.created",
		map[string]any{"mode_group_id": modeGroupID},
	)
	if err != nil {
		return nil, err
	}
	listing.RowVersion = rowVersion
	listing.CurrentRevisionID = &revisionID
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO account_external_placements (
			account_id, owner_user_id, platform, account_level,
			placement_type, state, priority, version, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, 'room', 'active', $5, 1, NOW(), NOW())
	`, account.ID, ownerUserID, listing.Platform, listing.AccountLevel, account.Priority); err != nil {
		return nil, translateAccountShareRoomPersistenceError(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO account_share_room_accounts (
			listing_id, account_id, owner_user_id, platform, account_level,
			state, priority, version, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, 'active', $6, 1, NOW(), NOW())
	`, listingID, account.ID, ownerUserID, listing.Platform, listing.AccountLevel, account.Priority); err != nil {
		return nil, translateAccountShareRoomPersistenceError(err)
	}

	if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &account.ID, nil, buildSchedulerGroupPayload(groupIDs)); err != nil {
		logger.LegacyPrintf("repository.account_share_mode", "[SchedulerOutbox] enqueue shared account create failed: account=%d err=%v", account.ID, err)
	}
	if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountGroupsChanged, &account.ID, nil, buildSchedulerGroupPayload(groupIDs)); err != nil {
		logger.LegacyPrintf("repository.account_share_mode", "[SchedulerOutbox] enqueue shared account group failed: account=%d group=%d err=%v", account.ID, modeGroupID, err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return r.GetListingByID(ctx, listingID, listing.OwnerUserID)
}

func (r *accountShareModeRepository) GetListingByID(ctx context.Context, listingID int64, viewerUserID int64) (*service.AccountShareListing, error) {
	return r.queryOneListing(ctx, viewerUserID, "l.id = $2", listingID)
}

func (r *accountShareModeRepository) GetVisibleListingByID(
	ctx context.Context,
	listingID int64,
	viewerUserID int64,
	viewerIsAdmin bool,
) (*service.AccountShareListing, error) {
	query := fmt.Sprintf(`
		%s
		WHERE l.deleted_at IS NULL
			AND a.deleted_at IS NULL
			AND l.id = $2
			AND (
				$3::boolean
				OR l.status = '%s'
				OR l.owner_user_id = $1
				OR EXISTS (
					SELECT 1
					FROM account_share_memberships visible_membership
					WHERE visible_membership.listing_id = l.id
						AND visible_membership.consumer_user_id = $1
						AND visible_membership.status IN ('%s', '%s', '%s', '%s')
						AND visible_membership.deleted_at IS NULL
				)
			)
	`,
		accountShareListingSelectSQL(),
		service.AccountShareListingStatusActive,
		service.AccountShareMembershipStatusActive,
		service.AccountShareMembershipStatusQueued,
		service.AccountShareMembershipStatusEnding,
		service.AccountShareMembershipStatusEnded,
	)
	listing, err := scanAccountShareListing(r.db.QueryRowContext(
		ctx,
		query,
		viewerUserID,
		listingID,
		viewerIsAdmin,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, err
	}
	return listing, nil
}

func (r *accountShareModeRepository) GetListingByAccountID(ctx context.Context, accountID int64) (*service.AccountShareListing, error) {
	return r.queryOneListing(ctx, 0, `EXISTS (
		SELECT 1
		FROM account_share_room_accounts room_account
		WHERE room_account.listing_id = l.id
			AND room_account.account_id = $2
			AND room_account.state IN ('active', 'draining')
	)`, accountID)
}

func (r *accountShareModeRepository) ListListings(ctx context.Context, viewerUserID int64, filters service.AccountShareListingFilters, params pagination.PaginationParams) ([]service.AccountShareListing, *pagination.PaginationResult, error) {
	page := params.Page
	if page < 1 {
		page = 1
	}
	limit := params.Limit()
	offset := (page - 1) * limit

	historyView := filters.Tab == service.AccountShareModeListingTabHistory
	archiveView := filters.Tab == service.AccountShareModeListingTabArchive
	whereParts := make([]string, 0, 16)
	if !historyView && !archiveView {
		whereParts = append(whereParts, "l.deleted_at IS NULL")
	}
	args := []any{viewerUserID}
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	applyStatusFilter := func(defaultActive bool) {
		switch filters.Status {
		case "all":
			return
		case service.AccountShareListingStatusActive, service.AccountShareListingStatusPaused:
			whereParts = append(whereParts, "l.status = "+addArg(filters.Status))
		case service.AccountShareListingStatusDisabled, service.AccountShareListingStatusSuspended:
			whereParts = append(
				whereParts,
				"l.status IN ("+
					addArg(service.AccountShareListingStatusDisabled)+","+
					addArg(service.AccountShareListingStatusSuspended)+")",
			)
		default:
			if defaultActive {
				whereParts = append(whereParts, "l.status = '"+service.AccountShareListingStatusActive+"'")
			}
		}
	}
	switch filters.Tab {
	case service.AccountShareModeListingTabUsing:
		whereParts = append(whereParts, "qm.id IS NOT NULL")
		applyStatusFilter(false)
	case service.AccountShareModeListingTabHistory:
		whereParts = append(whereParts, "hm.id IS NOT NULL", "qm.id IS NULL")
		if filters.Status != "" {
			applyStatusFilter(false)
		}
	case service.AccountShareModeListingTabMine:
		if !filters.ViewerIsAdmin {
			whereParts = append(whereParts, "l.owner_user_id = $1")
		}
		applyStatusFilter(false)
	case service.AccountShareModeListingTabArchive:
		whereParts = append(whereParts, "l.deleted_at IS NOT NULL")
		if !filters.ViewerIsAdmin {
			whereParts = append(whereParts, "l.owner_user_id = $1")
		}
		applyStatusFilter(false)
	default:
		if filters.ViewerIsAdmin {
			applyStatusFilter(true)
		} else {
			whereParts = append(whereParts, "l.status = '"+service.AccountShareListingStatusActive+"'")
		}
	}
	if filters.Platform != "" {
		whereParts = append(whereParts, "l.platform = "+addArg(filters.Platform))
	}
	if filters.OwnerUserID > 0 {
		whereParts = append(whereParts, "l.owner_user_id = "+addArg(filters.OwnerUserID))
	}
	if filters.AvailableOnly && !historyView && !archiveView {
		whereParts = append(whereParts, accountShareListingAvailableConditionSQL("NOW()"))
	}
	if len(filters.SeatLimits) > 0 {
		whereParts = append(whereParts, "l.seat_limit = ANY("+addArg(pq.Array(filters.SeatLimits))+")")
	} else if filters.SeatLimit >= service.AccountShareModeMinSeats && filters.SeatLimit <= service.AccountShareModeMaxSeats {
		whereParts = append(whereParts, "l.seat_limit = "+addArg(filters.SeatLimit))
	}
	if filters.Search != "" {
		placeholder := addArg("%" + filters.Search + "%")
		if archiveView {
			whereParts = append(whereParts, fmt.Sprintf(`(
				l.id::text ILIKE %[1]s
				OR l.owner_user_id::text ILIKE %[1]s
				OR EXISTS (
					SELECT 1
					FROM account_share_listing_revisions deleted_revision
					WHERE deleted_revision.id = l.deleted_revision_id
						AND deleted_revision.listing_id = l.id
						AND deleted_revision.revision_number > 0
						AND deleted_revision.schema_version > 0
						AND deleted_revision.snapshot_quality IN ('%[2]s', '%[3]s')
						AND jsonb_typeof(deleted_revision.allowed_models) = 'array'
						AND NOT EXISTS (
							SELECT 1
							FROM jsonb_array_elements(
								CASE
									WHEN jsonb_typeof(deleted_revision.allowed_models) = 'array'
									THEN deleted_revision.allowed_models
									ELSE '[]'::jsonb
								END
							) AS allowed_model(value)
							WHERE jsonb_typeof(allowed_model.value) <> 'string'
						)
						AND (
							deleted_revision.room_name ILIKE %[1]s
							OR deleted_revision.owner_display_name_snapshot ILIKE %[1]s
							OR EXISTS (
								SELECT 1
								FROM jsonb_array_elements_text(
									CASE
										WHEN jsonb_typeof(deleted_revision.allowed_models) = 'array'
										THEN deleted_revision.allowed_models
										ELSE '[]'::jsonb
									END
								) AS model(value)
								WHERE model.value ILIKE %[1]s
							)
						)
				)
			)`,
				placeholder,
				service.AccountShareSnapshotQualityExact,
				service.AccountShareSnapshotQualityBackfilledCurrent,
			))
		} else {
			whereParts = append(whereParts, fmt.Sprintf(`(
				l.room_name ILIKE %[1]s
				OR a.name ILIKE %[1]s
				OR COALESCE(u.username, '') ILIKE %[1]s
				OR l.id::text ILIKE %[1]s
				OR l.owner_user_id::text ILIKE %[1]s
				OR EXISTS (
					SELECT 1
					FROM jsonb_array_elements_text(l.allowed_models) AS model(value)
					WHERE model.value ILIKE %[1]s
				)
			)`, placeholder))
		}
	}
	if len(filters.Models) > 0 {
		whereParts = append(whereParts, fmt.Sprintf(`EXISTS (
			SELECT 1
			FROM jsonb_array_elements_text(l.allowed_models) AS model(value)
			WHERE lower(model.value) = ANY(%s)
		)`, addArg(pq.Array(lowerAccountShareModels(filters.Models)))))
	}
	if filters.AccountLevel != "" {
		whereParts = append(whereParts, fmt.Sprintf("%s = %s", accountShareEffectiveAccountLevelSQL(filters.AccountLevels), addArg(filters.AccountLevel)))
	}
	for _, feature := range filters.FeatureTags {
		switch feature {
		case service.AccountShareListingFeatureHourlyFeeWaiver:
			whereParts = append(whereParts, "l.hourly_fee_waiver_minimum > 0")
		case service.AccountShareListingFeatureImageGeneration:
			whereParts = append(whereParts, accountShareListingSupportsImageGenerationSQL())
		case service.AccountShareListingFeatureNoHourlyFee:
			whereParts = append(whereParts, "l.hourly_rate = 0")
		case service.AccountShareListingFeatureCodexCLIOnly:
			whereParts = append(whereParts, "l.codex_cli_only = TRUE")
		case service.AccountShareListingFeatureNonCodexCLIOnly:
			whereParts = append(whereParts, "l.codex_cli_only = FALSE")
		case service.AccountShareListingFeatureAvailable:
			if !historyView && !archiveView {
				whereParts = append(whereParts, accountShareListingAvailableConditionSQL("NOW()"))
			}
		}
	}
	whereSQL := strings.Join(whereParts, " AND ")

	approximatePagination := filters.SkipTotal || accountShareListingUsesApproximatePagination(filters)
	var total int64
	if !approximatePagination {
		countQuery := fmt.Sprintf(`
			SELECT COUNT(*)
			FROM account_share_listings l
			%s
			WHERE $1::bigint > 0
				AND %s
		`,
			accountShareListingSelectionJoinSQL(whereSQL, accountShareViewerCurrentMembershipFullLateralSQL()),
			whereSQL,
		)
		// args 的第一个位置固定为 viewerUserID，后续动态筛选从 $2 开始。
		// 即使 count 查询裁掉了所有依赖 viewer 的 join，也必须显式保留并标注
		// $1 的类型，否则 PostgreSQL 面对仅含 $2 等后续占位符的查询时无法推断
		// $1 类型，并返回 "could not determine data type of parameter $1"。
		if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
			return nil, nil, err
		}
	}

	queryLimit := limit
	if approximatePagination {
		queryLimit = limit + 1
	}
	args = append(args, queryLimit, offset)
	// 两阶段分页：viewer_current_membership 只求值一次，page 物化后再进入完整
	// god-view，防止 PostgreSQL 将外层 LATERAL 提前到页内 ID 半连接之前执行。
	// 外层必须复用同一 ORDER BY 表达式，否则页内乱序；单条语句同一快照下，
	// 两阶段排序值保持一致。
	orderSQL := accountShareListingOrderSQL(filters)
	query := fmt.Sprintf(`
		WITH %s,
		page AS MATERIALIZED (
			SELECT l.id
			FROM account_share_listings l
			%s
			WHERE %s
			ORDER BY %s
			LIMIT $%d OFFSET $%d
		),
		paged_listings AS MATERIALIZED (
			SELECT l.*
			FROM page
			JOIN account_share_listings l ON l.id = page.id
		)
		%s
		WHERE l.id IN (SELECT id FROM page)
		ORDER BY %s
	`, accountShareViewerCurrentMembershipCTESQL(), accountShareListingSelectionJoinSQL(whereSQL+" "+orderSQL, accountShareViewerCurrentMembershipJoinSQL()), whereSQL, orderSQL, len(args)-1, len(args), accountShareListingSelectSQLFromPage(), orderSQL)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	listings := make([]service.AccountShareListing, 0, limit)
	for rows.Next() {
		listing, err := scanAccountShareListing(rows)
		if err != nil {
			return nil, nil, err
		}
		listings = append(listings, *listing)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if historyView {
		if err := r.applyAccountShareHistorySnapshots(ctx, viewerUserID, listings); err != nil {
			return nil, nil, err
		}
	}
	if archiveView {
		if err := r.applyAccountShareArchiveSnapshots(ctx, listings); err != nil {
			return nil, nil, err
		}
	}
	if historyView || archiveView {
		for i := range listings {
			sanitizeAccountShareHistoricalListing(&listings[i], historyView)
		}
	}

	if approximatePagination {
		hasMore := len(listings) > limit
		if hasMore {
			listings = listings[:limit]
		}
		total = int64(offset + len(listings))
		if hasMore {
			total = int64(offset + limit + 1)
		}
	}
	pages := 0
	if total > 0 {
		pages = int((total + int64(limit) - 1) / int64(limit))
	}
	return listings, &pagination.PaginationResult{
		Approximate: approximatePagination,
		HasMore:     int64(offset+len(listings)) < total,
		Total:       total,
		Page:        page,
		PageSize:    limit,
		Pages:       pages,
	}, nil
}

func (r *accountShareModeRepository) ListRoomRuntimeAccounts(
	ctx context.Context,
	listingIDs []int64,
	now time.Time,
) (map[int64][]service.AccountWithConcurrency, error) {
	normalizedIDs := normalizeAccountShareListingIDs(listingIDs)
	if len(normalizedIDs) == 0 {
		return map[int64][]service.AccountWithConcurrency{}, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			room_account.listing_id,
			a.id,
			a.concurrency
		FROM account_share_room_accounts room_account
		JOIN accounts a ON a.id = room_account.account_id
		WHERE room_account.listing_id = ANY($1)
			AND room_account.state = 'active'
			AND a.deleted_at IS NULL
			AND NOT %s
		ORDER BY room_account.listing_id ASC, room_account.priority ASC, a.id ASC
	`, accountShareAccountUnavailableConditionSQL("$2::timestamptz")), pq.Array(normalizedIDs), now.UTC())
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	accountsByListing := make(map[int64][]service.AccountWithConcurrency, len(normalizedIDs))
	for rows.Next() {
		var listingID int64
		var account service.AccountWithConcurrency
		if err := rows.Scan(&listingID, &account.ID, &account.MaxConcurrency); err != nil {
			return nil, err
		}
		accountsByListing[listingID] = append(accountsByListing[listingID], account)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return accountsByListing, nil
}

// ListRoomAccountModelInfos 返回每个房间内账号的模型映射键集合，
// 用于计算房间可配置模型交集（supported_models）。

func (r *accountShareModeRepository) ListRoomAccountModelInfos(
	ctx context.Context,
	listingIDs []int64,
) (map[int64][]service.AccountShareRoomModelInfo, error) {
	normalizedIDs := normalizeAccountShareListingIDs(listingIDs)
	if len(normalizedIDs) == 0 {
		return map[int64][]service.AccountShareRoomModelInfo{}, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			room_account.listing_id,
			a.id,
			a.platform,
			a.credentials,
			a.owner_user_id
		FROM account_share_room_accounts room_account
		JOIN accounts a ON a.id = room_account.account_id
		WHERE room_account.listing_id = ANY($1)
			AND room_account.state = 'active'
			AND a.deleted_at IS NULL
		ORDER BY room_account.listing_id ASC, a.id ASC
	`, pq.Array(normalizedIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	infosByListing := make(map[int64][]service.AccountShareRoomModelInfo, len(normalizedIDs))
	for rows.Next() {
		var listingID, accountID int64
		var platform string
		var credentialsRaw []byte
		var ownerUserID sql.NullInt64
		if err := rows.Scan(&listingID, &accountID, &platform, &credentialsRaw, &ownerUserID); err != nil {
			return nil, err
		}
		account := &service.Account{
			ID:       accountID,
			Platform: strings.ToLower(strings.TrimSpace(platform)),
		}
		if ownerUserID.Valid {
			account.OwnerUserID = &ownerUserID.Int64
		}
		if len(credentialsRaw) > 0 {
			var credentials map[string]any
			if err := json.Unmarshal(credentialsRaw, &credentials); err != nil {
				return nil, err
			}
			account.Credentials = credentials
		}
		info := service.AccountShareRoomModelInfo{AccountID: accountID}
		if mapping := account.GetModelMapping(); len(mapping) > 0 {
			info.Models = make([]string, 0, len(mapping))
			for model := range mapping {
				info.Models = append(info.Models, model)
			}
			sort.Strings(info.Models)
		}
		infosByListing[listingID] = append(infosByListing[listingID], info)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return infosByListing, nil
}

func (r *accountShareModeRepository) ListRoomQuotaSnapshots(
	ctx context.Context,
	listingIDs []int64,
	now time.Time,
) (map[int64][]service.AccountShareRoomQuotaSnapshot, error) {
	normalizedIDs := normalizeAccountShareListingIDs(listingIDs)
	if len(normalizedIDs) == 0 {
		return map[int64][]service.AccountShareRoomQuotaSnapshot{}, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT
			room_account.listing_id,
			LOWER(BTRIM(a.platform)),
			a.type,
			a.extra,
			a.session_window_end
		FROM account_share_room_accounts room_account
		JOIN accounts a ON a.id = room_account.account_id
		WHERE room_account.listing_id = ANY($1)
			AND room_account.state = 'active'
			AND a.deleted_at IS NULL
		ORDER BY room_account.listing_id ASC, room_account.priority ASC, a.id ASC
	`, pq.Array(normalizedIDs))
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	snapshotsByListing := make(map[int64][]service.AccountShareRoomQuotaSnapshot, len(normalizedIDs))
	for rows.Next() {
		var (
			listingID        int64
			platform         string
			accountType      string
			extraRaw         []byte
			sessionWindowEnd sql.NullTime
		)
		if err := rows.Scan(&listingID, &platform, &accountType, &extraRaw, &sessionWindowEnd); err != nil {
			return nil, err
		}
		extra, err := unmarshalAccountShareJSONMap(extraRaw)
		if err != nil {
			return nil, err
		}
		account := &service.Account{
			Platform: strings.ToLower(strings.TrimSpace(platform)),
			Type:     strings.TrimSpace(accountType),
			Extra:    extra,
		}
		if sessionWindowEnd.Valid {
			value := sessionWindowEnd.Time.UTC()
			account.SessionWindowEnd = &value
		}
		snapshot := service.AccountShareRoomQuotaSnapshot{ListingID: listingID}
		switch account.Platform {
		case service.PlatformOpenAI:
			snapshot.Window5h = account.CodexUsageProgress(service.CodexQuotaWindow5h, now)
			snapshot.Window7d = account.CodexUsageProgress(service.CodexQuotaWindow7d, now)
		case service.PlatformAnthropic:
			snapshot.Window5h = account.AnthropicUsageProgress(service.AnthropicQuotaWindow5h, now)
			snapshot.Window7d = account.AnthropicUsageProgress(service.AnthropicQuotaWindow7d, now)
		case service.PlatformOpencode:
			snapshot.Window5h = account.OpencodeUsageProgress(service.OpencodeQuotaWindow5h, now)
			snapshot.Window7d = account.OpencodeUsageProgress(service.OpencodeQuotaWindow7d, now)
		}
		snapshotsByListing[listingID] = append(snapshotsByListing[listingID], snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return snapshotsByListing, nil
}

func normalizeAccountShareListingIDs(listingIDs []int64) []int64 {
	normalizedIDs := make([]int64, 0, len(listingIDs))
	seen := make(map[int64]struct{}, len(listingIDs))
	for _, listingID := range listingIDs {
		if listingID <= 0 {
			continue
		}
		if _, exists := seen[listingID]; exists {
			continue
		}
		seen[listingID] = struct{}{}
		normalizedIDs = append(normalizedIDs, listingID)
	}
	return normalizedIDs
}

// sanitizeAccountShareHistoricalListing removes fields that are derived from
// the current account, Redis runtime state, or an active edit session. A
// consumer history row may keep only the immutable account identity snapshot
// applied by applyAccountShareHistorySnapshots. Owner archive rows do not have
// a membership-owned account snapshot, so their representative account fields
// are cleared as well.

func sanitizeAccountShareHistoricalListing(listing *service.AccountShareListing, preserveAccountSnapshot bool) {
	if listing == nil {
		return
	}
	listing.AccountCount = 0
	listing.HealthyAccountCount = 0
	listing.QuotaSummary = nil
	listing.Accounts = nil
	listing.ActiveSeats = 0
	listing.ProxyID = nil
	listing.Proxy = nil
	listing.AccountPlanType = ""
	listing.AccountStatus = ""
	listing.AccountSchedulable = false
	listing.CurrentConcurrency = 0
	listing.AccountExpiresAt = nil
	listing.SubscriptionExpiresAt = nil
	listing.AccountLastUsedAt = nil
	listing.RateLimitedAt = nil
	listing.RateLimitResetAt = nil
	listing.OverloadUntil = nil
	listing.TempUnschedulableUntil = nil
	listing.TempUnschedulableReason = ""
	listing.CodexQuotaProtectionReason = nil
	listing.CodexQuotaProtectionResetAt = nil
	listing.Codex5hUsage = nil
	listing.Codex7dUsage = nil
	listing.CodexUsageUpdatedAt = nil
	listing.AnthropicQuotaProtectionReason = nil
	listing.AnthropicQuotaProtectionResetAt = nil
	listing.Anthropic5hUsage = nil
	listing.Anthropic7dUsage = nil
	listing.AnthropicUsageUpdatedAt = nil
	listing.OpencodeQuotaProtectionReason = nil
	listing.OpencodeQuotaProtectionResetAt = nil
	listing.Opencode5hUsage = nil
	listing.Opencode7dUsage = nil
	listing.Opencode30dUsage = nil
	listing.OpencodeUsageUpdatedAt = nil
	listing.CurrentMembershipID = nil
	listing.CurrentAPIKeyID = nil
	listing.CurrentAPIKeyName = ""
	listing.CurrentJoinedAt = nil
	listing.CurrentPaidUntil = nil
	listing.CurrentBilledUntil = nil
	listing.CurrentIdleTimeoutMinutes = nil
	listing.CurrentLastRequestAt = nil
	listing.CurrentIdleExpiresAt = nil
	listing.CurrentWaiverProgress = nil
	listing.QueueMembershipID = nil
	listing.QueueAPIKeyID = nil
	listing.QueueAPIKeyName = ""
	listing.QueueRank = nil
	listing.QueueStatus = ""
	listing.QueueIdleTimeoutMinutes = nil
	listing.QueueDispatchCooldownUntil = nil
	if !preserveAccountSnapshot {
		listing.AccountID = 0
		listing.AccountName = ""
		listing.AccountConcurrency = 0
	}
}

func (r *accountShareModeRepository) applyAccountShareArchiveSnapshots(
	ctx context.Context,
	listings []service.AccountShareListing,
) error {
	if len(listings) == 0 {
		return nil
	}

	listingIDs := make([]int64, 0, len(listings))
	indexesByListingID := make(map[int64][]int, len(listings))
	for i := range listings {
		listings[i].HistorySnapshotQuality = service.AccountShareSnapshotQualityUnknown
		clearUntrustedAccountShareHistoryProjection(&listings[i])
		if listings[i].ID <= 0 {
			continue
		}
		if _, exists := indexesByListingID[listings[i].ID]; !exists {
			listingIDs = append(listingIDs, listings[i].ID)
		}
		indexesByListingID[listings[i].ID] = append(indexesByListingID[listings[i].ID], i)
	}
	if len(listingIDs) == 0 {
		return nil
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT
			revision.id,
			revision.listing_id,
			revision.revision_number,
			revision.schema_version,
			revision.snapshot_quality,
			revision.room_name,
			revision.platform,
			revision.account_level,
			revision.owner_user_id,
			revision.owner_display_name_snapshot,
			revision.status,
			revision.seat_limit,
			revision.rate_multiplier,
			revision.allowed_models,
			revision.per_user_concurrency,
			revision.hourly_rate,
			revision.hourly_fee_waiver_minimum,
			revision.min_balance_required,
			revision.codex_cli_only,
			revision.codex_5h_limit_percent,
			revision.codex_7d_limit_percent
		FROM account_share_listings listing
		JOIN account_share_listing_revisions revision
			ON revision.id = listing.deleted_revision_id
			AND revision.listing_id = listing.id
		WHERE listing.id = ANY($1::bigint[])
			AND listing.deleted_at IS NOT NULL
	`, pq.Array(listingIDs))
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var snapshot accountShareListingRevisionSnapshot
		var allowedModelsRaw []byte
		var platform, accountLevel sql.NullString
		if err := rows.Scan(
			&snapshot.ID,
			&snapshot.ListingID,
			&snapshot.RowVersion,
			&snapshot.SchemaVersion,
			&snapshot.SnapshotQuality,
			&snapshot.RoomName,
			&platform,
			&accountLevel,
			&snapshot.OwnerUserID,
			&snapshot.OwnerDisplayName,
			&snapshot.Status,
			&snapshot.SeatLimit,
			&snapshot.RateMultiplier,
			&allowedModelsRaw,
			&snapshot.PerUserConcurrency,
			&snapshot.HourlyRate,
			&snapshot.HourlyFeeWaiverMinimum,
			&snapshot.MinBalanceRequired,
			&snapshot.CodexCLIOnly,
			&snapshot.Codex5hLimitPercent,
			&snapshot.Codex7dLimitPercent,
		); err != nil {
			return err
		}

		indexes, requested := indexesByListingID[snapshot.ListingID]
		if !requested ||
			snapshot.ID <= 0 ||
			snapshot.RowVersion <= 0 ||
			snapshot.SchemaVersion <= 0 {
			continue
		}
		snapshot.SnapshotQuality = normalizeAccountShareSnapshotQuality(snapshot.SnapshotQuality)
		switch snapshot.SnapshotQuality {
		case service.AccountShareSnapshotQualityExact,
			service.AccountShareSnapshotQualityBackfilledCurrent:
		default:
			continue
		}
		if err := json.Unmarshal(allowedModelsRaw, &snapshot.AllowedModels); err != nil ||
			snapshot.AllowedModels == nil {
			continue
		}
		snapshot.RoomName = strings.TrimSpace(snapshot.RoomName)
		snapshot.Platform = strings.ToLower(strings.TrimSpace(platform.String))
		snapshot.AccountLevel = service.NormalizeAccountLevel(accountLevel.String)
		snapshot.OwnerDisplayName = strings.TrimSpace(snapshot.OwnerDisplayName)

		for _, index := range indexes {
			revisionID := snapshot.ID
			listing := &listings[index]
			listing.RowVersion = snapshot.RowVersion
			listing.CurrentRevisionID = &revisionID
			listing.RoomName = snapshot.RoomName
			listing.Platform = snapshot.Platform
			listing.AccountLevel = snapshot.AccountLevel
			listing.OwnerUserID = snapshot.OwnerUserID
			listing.OwnerUsername = snapshot.OwnerDisplayName
			listing.Status = snapshot.Status
			listing.SeatLimit = snapshot.SeatLimit
			listing.RateMultiplier = snapshot.RateMultiplier
			listing.AllowedModels = append([]string(nil), snapshot.AllowedModels...)
			listing.PerUserConcurrency = snapshot.PerUserConcurrency
			listing.HourlyRate = snapshot.HourlyRate
			listing.HourlyFeeWaiverMinimum = snapshot.HourlyFeeWaiverMinimum
			listing.MinBalanceRequired = snapshot.MinBalanceRequired
			listing.CodexCLIOnly = snapshot.CodexCLIOnly
			listing.Codex5hLimitPercent = snapshot.Codex5hLimitPercent
			listing.Codex7dLimitPercent = snapshot.Codex7dLimitPercent
			listing.Anthropic5hLimitPercent = snapshot.Codex5hLimitPercent
			listing.Anthropic7dLimitPercent = snapshot.Codex7dLimitPercent
			listing.HistorySnapshotQuality = snapshot.SnapshotQuality
		}
	}
	return rows.Err()
}

func (r *accountShareModeRepository) UpdateListing(ctx context.Context, actorUserID int64, actorIsAdmin bool, listingID int64, input service.UpdateAccountShareListingInput) (*service.AccountShareListing, error) {
	if input.ExpectedVersion == nil || *input.ExpectedVersion <= 0 {
		return nil, service.ErrAccountShareExpectedVersionRequired.WithMetadata(map[string]string{"field": "expected_version"})
	}
	if input.Status != nil {
		return nil, service.ErrAccountShareRoomLifecycleCommandRequired
	}
	if input.ProxyID != nil || input.Concurrency != nil {
		return nil, service.ErrAccountShareRoomAccountConfigUnsupported
	}
	if (input.Codex5hLimitPercent != nil && input.Anthropic5hLimitPercent != nil) ||
		(input.Codex7dLimitPercent != nil && input.Anthropic7dLimitPercent != nil) {
		return nil, service.ErrAccountShareRoomConflictingFields
	}
	if !repositoryHasAccountShareListingUpdate(input) {
		return nil, service.ErrAccountShareRoomNoChanges
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.ForceActiveEdit {
		if !actorIsAdmin {
			return nil, service.ErrAccountShareForceAdminRequired
		}
		if input.Reason == "" {
			return nil, service.ErrAccountShareForceReasonRequired.WithMetadata(map[string]string{"field": "reason"})
		}
		if !input.Confirmed {
			return nil, service.ErrAccountShareForceConfirmationRequired.WithMetadata(map[string]string{"field": "confirmed"})
		}
	} else if input.Reason == "" {
		return nil, service.ErrAccountShareUpdateReasonRequired.WithMetadata(map[string]string{"field": "reason"})
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

	var ownerUserID int64
	var currentName string
	var currentStatus string
	var currentRowVersion int64
	var currentSeatLimit int
	var currentRateMultiplier float64
	var currentAllowedModelsRaw []byte
	var currentPerUserConcurrency int
	var currentHourlyRate float64
	var currentHourlyFeeWaiverMinimum float64
	var currentMinBalanceRequired float64
	var currentCodexCLIOnly bool
	var currentCodex5hLimitPercent float64
	var currentCodex7dLimitPercent float64
	var pendingOperationID sql.NullString
	ownerPredicate := ""
	selectArgs := []any{listingID}
	if !actorIsAdmin {
		selectArgs = append(selectArgs, actorUserID)
		ownerPredicate = fmt.Sprintf("AND l.owner_user_id = $%d", len(selectArgs))
	}
	selectQuery := fmt.Sprintf(`
		SELECT
			l.owner_user_id,
			COALESCE(l.room_name, ''),
			l.status,
			l.row_version,
			l.seat_limit,
			l.rate_multiplier,
			l.allowed_models,
			l.per_user_concurrency,
			l.hourly_rate,
			l.hourly_fee_waiver_minimum,
			l.min_balance_required,
			l.codex_cli_only,
			l.codex_5h_limit_percent,
			l.codex_7d_limit_percent,
			l.pending_operation_id
		FROM account_share_listings l
		WHERE l.id = $1
			%s
			AND l.deleted_at IS NULL
		FOR UPDATE OF l
	`, ownerPredicate)
	if err := tx.QueryRowContext(ctx, selectQuery, selectArgs...).Scan(
		&ownerUserID,
		&currentName,
		&currentStatus,
		&currentRowVersion,
		&currentSeatLimit,
		&currentRateMultiplier,
		&currentAllowedModelsRaw,
		&currentPerUserConcurrency,
		&currentHourlyRate,
		&currentHourlyFeeWaiverMinimum,
		&currentMinBalanceRequired,
		&currentCodexCLIOnly,
		&currentCodex5hLimitPercent,
		&currentCodex7dLimitPercent,
		&pendingOperationID,
	); errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	} else if err != nil {
		return nil, err
	}
	if currentRowVersion != *input.ExpectedVersion {
		return nil, accountShareVersionConflict(*input.ExpectedVersion, currentRowVersion)
	}
	if pendingOperationID.Valid {
		return nil, service.ErrAccountShareRoomOperationConflict.WithMetadata(map[string]string{
			"operation_id": pendingOperationID.String,
		})
	}

	var currentAllowedModels []string
	if err := json.Unmarshal(currentAllowedModelsRaw, &currentAllowedModels); err != nil {
		return nil, err
	}
	contractUpdate := accountShareListingConfigUpdateRequiresEmptyRoom(input)
	if contractUpdate {
		if actorIsAdmin && input.ForceActiveEdit {
			if !accountShareAdminForceEditableStatus(currentStatus) {
				return nil, service.ErrAccountShareRoomOperationConflict.WithMetadata(map[string]string{"blocker": "lifecycle_status", "status": currentStatus})
			}
		} else {
			if !accountShareOwnerEditableStatus(currentStatus) {
				return nil, service.ErrAccountShareUpdateRequiresPaused
			}
			blockers, err := accountShareListingEditBlockersInTx(ctx, tx, listingID)
			if err != nil {
				return nil, err
			}
			if blockers.EndingMembershipCount > 0 || blockers.SynchronousBillingPendingCount > 0 {
				return nil, service.ErrAccountShareListingInUse.WithMetadata(blockers.Metadata())
			}
			if blockers.ActiveMembershipCount > 0 {
				consumerSafe, err := accountShareListingUpdateProtectsConsumers(ctx, tx, listingID, input, accountShareListingConsumerTerms{
					rateMultiplier: currentRateMultiplier, allowedModels: currentAllowedModels,
					perUserConcurrency: currentPerUserConcurrency, hourlyRate: currentHourlyRate,
					feeWaiverMinimum: currentHourlyFeeWaiverMinimum, minBalanceRequired: currentMinBalanceRequired,
				})
				if err != nil {
					return nil, err
				}
				if !consumerSafe {
					return nil, service.ErrAccountShareListingInUse.WithMetadata(blockers.Metadata())
				}
			}
		}
	}
	if input.Name != nil {
		if err := ensureAccountShareRoomNameAvailableForUpdate(ctx, tx, ownerUserID, listingID, *input.Name); err != nil {
			return nil, err
		}
	}
	allowedModelsChanged := input.AllowedModels != nil &&
		!equalNormalizedAccountShareModels(*input.AllowedModels, currentAllowedModels)
	if allowedModelsChanged {
		if err := validateAccountShareRoomAllowedModelsInTx(
			ctx,
			tx,
			ownerUserID,
			listingID,
			*input.AllowedModels,
		); err != nil {
			return nil, err
		}
	}

	setParts := []string{"updated_at = NOW()", "row_version = row_version + 1"}
	updateArgs := []any{}
	changedFields := make([]string, 0, 14)
	addArg := func(value any) string {
		updateArgs = append(updateArgs, value)
		return fmt.Sprintf("$%d", len(updateArgs))
	}
	if input.Name != nil && strings.TrimSpace(*input.Name) != currentName {
		setParts = append(setParts, "room_name = "+addArg(strings.TrimSpace(*input.Name)))
		changedFields = append(changedFields, "room_name")
	}
	if input.SeatLimit != nil && *input.SeatLimit != currentSeatLimit {
		setParts = append(setParts, "seat_limit = "+addArg(*input.SeatLimit))
		changedFields = append(changedFields, "seat_limit")
	}
	if input.RateMultiplier != nil && *input.RateMultiplier != currentRateMultiplier {
		setParts = append(setParts, "rate_multiplier = "+addArg(*input.RateMultiplier))
		changedFields = append(changedFields, "rate_multiplier")
	}
	if allowedModelsChanged {
		modelsJSON, err := json.Marshal(*input.AllowedModels)
		if err != nil {
			return nil, err
		}
		setParts = append(setParts, "allowed_models = "+addArg(string(modelsJSON))+"::jsonb")
		changedFields = append(changedFields, "allowed_models")
	}
	if input.PerUserConcurrency != nil && *input.PerUserConcurrency != currentPerUserConcurrency {
		setParts = append(setParts, "per_user_concurrency = "+addArg(*input.PerUserConcurrency))
		changedFields = append(changedFields, "per_user_concurrency")
	}
	if input.HourlyRate != nil && *input.HourlyRate != currentHourlyRate {
		setParts = append(setParts, "hourly_rate = "+addArg(*input.HourlyRate))
		changedFields = append(changedFields, "hourly_rate")
	}
	if input.HourlyFeeWaiverMinimum != nil && *input.HourlyFeeWaiverMinimum != currentHourlyFeeWaiverMinimum {
		setParts = append(setParts, "hourly_fee_waiver_minimum = "+addArg(*input.HourlyFeeWaiverMinimum))
		changedFields = append(changedFields, "hourly_fee_waiver_minimum")
	}
	if input.MinBalanceRequired != nil && *input.MinBalanceRequired != currentMinBalanceRequired {
		setParts = append(setParts, "min_balance_required = "+addArg(*input.MinBalanceRequired))
		changedFields = append(changedFields, "min_balance_required")
	}
	if input.CodexCLIOnly != nil && *input.CodexCLIOnly != currentCodexCLIOnly {
		setParts = append(setParts, "codex_cli_only = "+addArg(*input.CodexCLIOnly))
		changedFields = append(changedFields, "codex_cli_only")
	}
	if input.Codex5hLimitPercent != nil && *input.Codex5hLimitPercent != currentCodex5hLimitPercent {
		setParts = append(setParts, "codex_5h_limit_percent = "+addArg(*input.Codex5hLimitPercent))
		changedFields = append(changedFields, "codex_5h_limit_percent")
	}
	if input.Codex7dLimitPercent != nil && *input.Codex7dLimitPercent != currentCodex7dLimitPercent {
		setParts = append(setParts, "codex_7d_limit_percent = "+addArg(*input.Codex7dLimitPercent))
		changedFields = append(changedFields, "codex_7d_limit_percent")
	}
	if input.Anthropic5hLimitPercent != nil && *input.Anthropic5hLimitPercent != currentCodex5hLimitPercent {
		setParts = append(setParts, "codex_5h_limit_percent = "+addArg(*input.Anthropic5hLimitPercent))
		changedFields = append(changedFields, "anthropic_5h_limit_percent")
	}
	if input.Anthropic7dLimitPercent != nil && *input.Anthropic7dLimitPercent != currentCodex7dLimitPercent {
		setParts = append(setParts, "codex_7d_limit_percent = "+addArg(*input.Anthropic7dLimitPercent))
		changedFields = append(changedFields, "anthropic_7d_limit_percent")
	}
	if input.JoinPassword != nil {
		// service 层已把明文替换成 bcrypt 哈希;空串表示清除密码。
		if trimmed := strings.TrimSpace(*input.JoinPassword); trimmed == "" {
			setParts = append(setParts, "join_password_hash = NULL")
		} else {
			setParts = append(setParts, "join_password_hash = "+addArg(*input.JoinPassword))
		}
		changedFields = append(changedFields, "join_password")
	}
	if len(changedFields) == 0 {
		return nil, service.ErrAccountShareRoomNoChanges
	}

	listingArg := addArg(listingID)
	ownerUpdatePredicate := ""
	if !actorIsAdmin {
		ownerUpdatePredicate = "AND owner_user_id = " + addArg(actorUserID)
	}
	expectedVersionArg := addArg(*input.ExpectedVersion)
	query := fmt.Sprintf(`
		UPDATE account_share_listings
		SET %s
		WHERE id = %s
			%s
			AND row_version = %s
			AND deleted_at IS NULL
	`, strings.Join(setParts, ", "), listingArg, ownerUpdatePredicate, expectedVersionArg)
	result, err := tx.ExecContext(ctx, query, updateArgs...)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		var actualVersion int64
		if err := tx.QueryRowContext(ctx, `
			SELECT row_version
			FROM account_share_listings
			WHERE id = $1
				AND deleted_at IS NULL
		`, listingID).Scan(&actualVersion); errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrAccountShareListingNotFound
		} else if err != nil {
			return nil, err
		}
		return nil, accountShareVersionConflict(*input.ExpectedVersion, actualVersion)
	}
	if _, _, err := createAccountShareListingRevisionInTx(
		ctx,
		tx,
		listingID,
		actorUserID,
		actorIsAdmin,
		"update_listing",
		input.Reason,
		input.ForceActiveEdit,
		"listing.updated",
		map[string]any{"changed_fields": changedFields},
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return r.GetListingByID(ctx, listingID, ownerUserID)
}

type accountShareListingConsumerTerms struct {
	rateMultiplier     float64
	allowedModels      []string
	perUserConcurrency int
	hourlyRate         float64
	feeWaiverMinimum   float64
	minBalanceRequired float64
}

func accountShareListingUpdateProtectsConsumers(
	ctx context.Context,
	tx *sql.Tx,
	listingID int64,
	input service.UpdateAccountShareListingInput,
	current accountShareListingConsumerTerms,
) (bool, error) {
	if input.CodexCLIOnly != nil ||
		input.Codex5hLimitPercent != nil ||
		input.Codex7dLimitPercent != nil ||
		input.Anthropic5hLimitPercent != nil ||
		input.Anthropic7dLimitPercent != nil {
		return false, nil
	}
	reject := func(field, reason string) (bool, error) {
		return false, service.ErrAccountShareConsumerProtectionViolation.WithMetadata(map[string]string{
			"field":  field,
			"reason": reason,
		})
	}
	if input.RateMultiplier != nil && *input.RateMultiplier > current.rateMultiplier {
		return reject("rate_multiplier", "cannot_increase")
	}
	if input.HourlyRate != nil && *input.HourlyRate > current.hourlyRate {
		return reject("hourly_rate", "cannot_increase")
	}
	if input.HourlyFeeWaiverMinimum != nil && *input.HourlyFeeWaiverMinimum > current.feeWaiverMinimum {
		return reject("hourly_fee_waiver_minimum", "cannot_increase")
	}
	if input.MinBalanceRequired != nil && *input.MinBalanceRequired > current.minBalanceRequired {
		return reject("min_balance_required", "cannot_increase")
	}
	if input.PerUserConcurrency != nil && *input.PerUserConcurrency < current.perUserConcurrency {
		return reject("per_user_concurrency", "cannot_decrease")
	}
	if input.AllowedModels != nil && !accountShareModelsContainAll(*input.AllowedModels, current.allowedModels) {
		return reject("allowed_models", "cannot_remove_existing_models")
	}

	var protectedSeats, configuredConcurrency int
	if input.SeatLimit != nil || input.PerUserConcurrency != nil {
		if err := tx.QueryRowContext(ctx, `
			SELECT
				COUNT(*) FILTER (
					WHERE membership.status IN ('active', 'ending')
						AND membership.deleted_at IS NULL
						AND membership.consumer_user_id <> listing.owner_user_id
				)::int,
				COALESCE((
					SELECT SUM(account.concurrency)::int
					FROM account_share_room_accounts room_account
					JOIN accounts account ON account.id = room_account.account_id
					WHERE room_account.listing_id = $1
						AND room_account.state IN ('active', 'draining')
						AND account.deleted_at IS NULL
				), 0)::int
			FROM account_share_listings listing
			LEFT JOIN account_share_memberships membership ON membership.listing_id = listing.id
			WHERE listing.id = $1
			GROUP BY listing.id
		`, listingID).Scan(&protectedSeats, &configuredConcurrency); err != nil {
			return false, err
		}
	}
	if input.SeatLimit != nil && *input.SeatLimit < protectedSeats {
		return reject("seat_limit", "below_protected_seats")
	}
	if input.PerUserConcurrency != nil && *input.PerUserConcurrency > configuredConcurrency {
		return reject("per_user_concurrency", "above_room_total_concurrency")
	}
	return true, nil
}

func accountShareModelsContainAll(candidate, required []string) bool {
	available := make(map[string]struct{}, len(candidate))
	for _, model := range candidate {
		model = strings.ToLower(strings.TrimSpace(model))
		if model != "" {
			available[model] = struct{}{}
		}
	}
	for _, model := range required {
		model = strings.ToLower(strings.TrimSpace(model))
		if model == "" {
			continue
		}
		if _, ok := available[model]; !ok {
			return false
		}
	}
	return true
}

func validateAccountShareRoomAllowedModelsInTx(
	ctx context.Context,
	tx *sql.Tx,
	ownerUserID int64,
	listingID int64,
	allowedModels []string,
) error {
	accountIDs, err := lockAccountShareRoomProjectionInTx(ctx, tx, listingID)
	if err != nil {
		return err
	}
	if len(accountIDs) == 0 {
		return nil
	}
	candidates, err := lockAccountShareRoomAccountCandidatesInTx(
		ctx,
		tx,
		ownerUserID,
		listingID,
		accountIDs,
		false,
	)
	if err != nil {
		return err
	}
	if len(candidates) != len(accountIDs) {
		return service.ErrAccountShareAccountUnavailable.WithMetadata(map[string]string{
			"reason": "room contains a missing, deleted, or foreign account",
		})
	}
	for _, candidate := range candidates {
		account := &service.Account{
			ID:           candidate.Snapshot.AccountID,
			Platform:     candidate.Snapshot.Platform,
			AccountLevel: candidate.Snapshot.AccountLevel,
			Type:         candidate.AccountType,
			Credentials:  candidate.Credentials,
			Extra:        candidate.Extra,
		}
		for _, model := range allowedModels {
			if account.IsModelSupportedByMapping(model) {
				continue
			}
			return service.ErrAccountShareModeUnsupportedModel.WithMetadata(map[string]string{
				"account_id": strconv.FormatInt(account.ID, 10),
				"model":      strings.TrimSpace(model),
			})
		}
	}
	return nil
}

func repositoryHasAccountShareListingUpdate(input service.UpdateAccountShareListingInput) bool {
	return input.Name != nil ||
		input.SeatLimit != nil ||
		input.RateMultiplier != nil ||
		input.AllowedModels != nil ||
		input.PerUserConcurrency != nil ||
		input.HourlyRate != nil ||
		input.HourlyFeeWaiverMinimum != nil ||
		input.MinBalanceRequired != nil ||
		input.CodexCLIOnly != nil ||
		input.Codex5hLimitPercent != nil ||
		input.Codex7dLimitPercent != nil ||
		input.Anthropic5hLimitPercent != nil ||
		input.Anthropic7dLimitPercent != nil ||
		input.JoinPassword != nil
}

func equalNormalizedAccountShareModels(left, right []string) bool {
	left = serviceNormalizeAccountShareModelSet(left)
	right = serviceNormalizeAccountShareModelSet(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func serviceNormalizeAccountShareModelSet(models []string) []string {
	normalized := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		model = strings.ToLower(strings.TrimSpace(model))
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		normalized = append(normalized, model)
	}
	sort.Strings(normalized)
	return normalized
}

func accountShareVersionConflict(expectedVersion, actualVersion int64) error {
	return service.ErrAccountShareVersionConflict.WithMetadata(map[string]string{
		"expected_version": strconv.FormatInt(expectedVersion, 10),
		"actual_version":   strconv.FormatInt(actualVersion, 10),
	})
}

func accountShareAdminForceEditableStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case service.AccountShareListingStatusActive,
		service.AccountShareListingStatusPaused,
		service.AccountShareListingStatusDisabled,
		service.AccountShareListingStatusSuspended:
		return true
	default:
		return false
	}
}

func accountShareOwnerEditableStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case service.AccountShareListingStatusActive,
		service.AccountShareListingStatusPaused:
		return true
	default:
		return false
	}
}

func accountShareListingConfigUpdateRequiresEmptyRoom(input service.UpdateAccountShareListingInput) bool {
	return input.SeatLimit != nil ||
		input.RateMultiplier != nil ||
		input.AllowedModels != nil ||
		input.PerUserConcurrency != nil ||
		input.HourlyRate != nil ||
		input.HourlyFeeWaiverMinimum != nil ||
		input.MinBalanceRequired != nil ||
		input.CodexCLIOnly != nil ||
		input.Codex5hLimitPercent != nil ||
		input.Codex7dLimitPercent != nil ||
		input.Anthropic5hLimitPercent != nil ||
		input.Anthropic7dLimitPercent != nil
}

func (r *accountShareModeRepository) ResolvePolicy(ctx context.Context) (*service.AccountSharePolicy, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrServiceUnavailable
	}
	return resolveEnabledGlobalAccountSharePolicy(ctx, r.db)
}

func (r *accountShareModeRepository) queryOneListing(ctx context.Context, viewerUserID int64, predicate string, value any) (*service.AccountShareListing, error) {
	query := fmt.Sprintf(`
		%s
		WHERE l.deleted_at IS NULL
			AND a.deleted_at IS NULL
			AND %s
	`, accountShareListingSelectSQL(), predicate)
	row := r.db.QueryRowContext(ctx, query, viewerUserID, value)
	listing, err := scanAccountShareListing(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, err
	}
	return listing, nil
}

func (r *accountShareModeRepository) getListingByMembershipAccount(ctx context.Context, membership *service.AccountShareMembership) (*service.AccountShareListing, error) {
	if membership == nil || membership.ListingID <= 0 || membership.AccountID <= 0 {
		return nil, service.ErrAccountShareListingNotFound
	}
	query := fmt.Sprintf(`
		%s
		WHERE l.deleted_at IS NULL
			AND l.id = $2
			AND a.id = $3
	`, accountShareListingSelectSQLWithAccountJoin("JOIN accounts a ON a.id = $3"))
	listing, err := scanAccountShareListing(r.db.QueryRowContext(
		ctx,
		query,
		membership.ConsumerUserID,
		membership.ListingID,
		membership.AccountID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, err
	}
	return listing, nil
}

func (r *accountShareModeRepository) queryActiveMembership(ctx context.Context, predicate string, args ...any) (*service.AccountShareMembership, *service.AccountShareListing, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	query := fmt.Sprintf(`
		SELECT
			m.id, m.listing_id, m.account_id, l.owner_user_id, m.consumer_user_id, m.api_key_id, m.status,
			m.queue_rank, m.hourly_rate_snapshot, m.hourly_fee_waiver_minimum_snapshot, m.idle_timeout_minutes,
			m.joined_at, m.last_request_at, m.ended_at, m.ended_reason, m.paid_until, m.billed_until,
			m.waiver_window_started_at, m.waiver_window_usage_amount, m.waiver_window_request_count, m.waiver_window_last_request_at,
			m.dispatch_failed_at, m.dispatch_cooldown_until, m.created_at, m.updated_at
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
			AND l.deleted_at IS NULL
			AND l.status IN ('%s', '%s')
		JOIN accounts a ON a.id = m.account_id
		WHERE m.status = '%s'
			AND m.deleted_at IS NULL
			AND (m.hourly_rate_snapshot <= 0 OR m.paid_until IS NULL OR m.paid_until > NOW())
			AND %s
		ORDER BY m.joined_at DESC
		LIMIT 1
	`,
		service.AccountShareListingStatusActive,
		service.AccountShareListingStatusDraining,
		service.AccountShareMembershipStatusActive,
		predicate,
	)
	membership, err := scanAccountShareMembership(tx.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if err := loadAndValidateAccountShareMembershipRuntimeSnapshotInTx(ctx, tx, membership); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	tx = nil
	listing, err := r.getListingByMembershipAccount(ctx, membership)
	if err != nil {
		return nil, nil, err
	}
	if err := applyAccountShareMembershipRuntimeTerms(membership, listing); err != nil {
		return nil, nil, err
	}
	return membership, listing, nil
}

func lowerAccountShareModels(models []string) []string {
	out := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.ToLower(strings.TrimSpace(model))
		if model != "" {
			out = append(out, model)
		}
	}
	return out
}

func accountShareListingUsesApproximatePagination(filters service.AccountShareListingFilters) bool {
	return filters.SeatLimit > 0 ||
		len(filters.SeatLimits) > 0 ||
		strings.TrimSpace(filters.Search) != "" ||
		strings.TrimSpace(filters.Status) != "" ||
		filters.OwnerUserID > 0 ||
		len(filters.Models) > 0 ||
		strings.TrimSpace(filters.AccountLevel) != "" ||
		len(filters.FeatureTags) > 0
}

type accountShareListingScanner interface {
	Scan(dest ...any) error
}

func scanAccountShareListing(scanner accountShareListingScanner) (*service.AccountShareListing, error) {
	listing := &service.AccountShareListing{}
	var allowedModelsRaw []byte
	var currentRevisionID, proxyID, accountIdentityID, currentMembershipID, currentConsumerUserID, currentAPIKeyID, currentIdleTimeoutMinutes, queueMembershipID, queueAPIKeyID, queueRank, queueIdleTimeoutMinutes, lastUsedMembershipID sql.NullInt64
	var currentJoinedAt, currentPaidUntil, currentBilledUntil, currentLastRequestAt, currentWaiverWindowStartedAt, currentWaiverWindowLastRequestAt, queueDispatchCooldownUntil, lastUsedAt sql.NullTime
	var accountPlatform, accountType, accountLevel, accountStatus string
	var accountSchedulable bool
	var accountExpiresAt, accountLastUsedAt, rateLimitedAt, rateLimitResetAt, overloadUntil, tempUnschedulableUntil, sessionWindowStart, sessionWindowEnd sql.NullTime
	var tempUnschedulableReason, sessionWindowStatus, subscriptionExpiresAtRaw, currentAPIKeyName, queueAPIKeyName, queueStatus, queueEndingOperationID, queueEndingOperationStatus, queueSettlementStatus, joinPasswordHash sql.NullString
	var credentialsRaw, extraRaw []byte
	var currentWaiverWindowUsageAmount sql.NullString
	var currentWaiverWindowRequestCount sql.NullInt64
	err := scanner.Scan(
		&listing.ID,
		&listing.RowVersion,
		&currentRevisionID,
		&listing.Deleted,
		&listing.AccountID,
		&listing.RoomName,
		&listing.AccountCount,
		&listing.HealthyAccountCount,
		&listing.OwnerUserID,
		&listing.OwnerUsername,
		&listing.AccountName,
		&proxyID,
		&listing.Status,
		&listing.SeatLimit,
		&listing.ActiveSeats,
		&accountIdentityID,
		&listing.RatingCount,
		&listing.RatingScoreSum,
		&listing.RatingAvg,
		&listing.RateMultiplier,
		&allowedModelsRaw,
		&listing.PerUserConcurrency,
		&listing.AccountConcurrency,
		&listing.RepresentativeAccountConcurrency,
		&listing.RepresentativeAccountAutoPauseOnExpired,
		&listing.HourlyRate,
		&listing.HourlyFeeWaiverMinimum,
		&listing.MinBalanceRequired,
		&listing.CodexCLIOnly,
		&listing.Codex5hLimitPercent,
		&listing.Codex7dLimitPercent,
		&joinPasswordHash,
		&accountPlatform,
		&accountType,
		&accountLevel,
		&accountStatus,
		&accountSchedulable,
		&accountExpiresAt,
		&accountLastUsedAt,
		&rateLimitedAt,
		&rateLimitResetAt,
		&overloadUntil,
		&tempUnschedulableUntil,
		&tempUnschedulableReason,
		&sessionWindowStart,
		&sessionWindowEnd,
		&sessionWindowStatus,
		&credentialsRaw,
		&extraRaw,
		&subscriptionExpiresAtRaw,
		&currentMembershipID,
		&currentConsumerUserID,
		&currentAPIKeyID,
		&currentAPIKeyName,
		&currentJoinedAt,
		&currentPaidUntil,
		&currentBilledUntil,
		&currentIdleTimeoutMinutes,
		&currentLastRequestAt,
		&currentWaiverWindowStartedAt,
		&currentWaiverWindowUsageAmount,
		&currentWaiverWindowRequestCount,
		&currentWaiverWindowLastRequestAt,
		&queueMembershipID,
		&queueAPIKeyID,
		&queueAPIKeyName,
		&queueRank,
		&queueStatus,
		&queueEndingOperationID,
		&queueEndingOperationStatus,
		&queueSettlementStatus,
		&queueIdleTimeoutMinutes,
		&queueDispatchCooldownUntil,
		&lastUsedMembershipID,
		&lastUsedAt,
		&listing.CreatedAt,
		&listing.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(allowedModelsRaw) > 0 {
		if err := json.Unmarshal(allowedModelsRaw, &listing.AllowedModels); err != nil {
			return nil, err
		}
	}
	listing.ProxyID = sqlNullInt64Ptr(proxyID)
	listing.CurrentRevisionID = sqlNullInt64Ptr(currentRevisionID)
	listing.AccountIdentityID = sqlNullInt64Ptr(accountIdentityID)
	if joinPasswordHash.Valid {
		listing.JoinPasswordHash = joinPasswordHash.String
	}
	listing.HasJoinPassword = strings.TrimSpace(listing.JoinPasswordHash) != ""
	credentials, err := unmarshalAccountShareJSONMap(credentialsRaw)
	if err != nil {
		return nil, err
	}
	extra, err := unmarshalAccountShareJSONMap(extraRaw)
	if err != nil {
		return nil, err
	}
	account := &service.Account{
		ID:                      listing.AccountID,
		Platform:                accountPlatform,
		AccountLevel:            accountLevel,
		Type:                    accountType,
		Credentials:             credentials,
		Extra:                   extra,
		Status:                  accountStatus,
		ExpiresAt:               sqlNullTimePtr(accountExpiresAt),
		LastUsedAt:              sqlNullTimePtr(accountLastUsedAt),
		RateLimitedAt:           sqlNullTimePtr(rateLimitedAt),
		RateLimitResetAt:        sqlNullTimePtr(rateLimitResetAt),
		OverloadUntil:           sqlNullTimePtr(overloadUntil),
		TempUnschedulableUntil:  sqlNullTimePtr(tempUnschedulableUntil),
		TempUnschedulableReason: tempUnschedulableReason.String,
		SessionWindowStart:      sqlNullTimePtr(sessionWindowStart),
		SessionWindowEnd:        sqlNullTimePtr(sessionWindowEnd),
		SessionWindowStatus:     sessionWindowStatus.String,
		Schedulable:             accountSchedulable,
	}
	now := time.Now()
	listing.Platform = strings.ToLower(strings.TrimSpace(account.Platform))
	listing.AccountLevel = service.NormalizeOpenAIAccountLevel(account.Platform, account.AccountLevel, account.Credentials, account.Extra)
	listing.AccountPlanType = service.OpenAIAccountPlanType(account.Credentials, account.Extra)
	listing.AccountStatus = account.Status
	listing.AccountSchedulable = account.Schedulable
	listing.AccountExpiresAt = account.ExpiresAt
	listing.SubscriptionExpiresAt = parseAccountShareTime(subscriptionExpiresAtRaw.String)
	listing.AccountLastUsedAt = account.LastUsedAt
	listing.RateLimitedAt = account.RateLimitedAt
	listing.RateLimitResetAt = account.RateLimitResetAt
	listing.OverloadUntil = account.OverloadUntil
	listing.TempUnschedulableUntil = account.TempUnschedulableUntil
	listing.TempUnschedulableReason = account.TempUnschedulableReason
	if reason := account.CodexQuotaProtectionReasonAt(now); reason != "" {
		listing.CodexQuotaProtectionReason = &reason
		listing.CodexQuotaProtectionResetAt = account.CodexQuotaProtectionResetAt(now)
	}
	listing.Codex5hUsage = account.CodexUsageProgress(service.CodexQuotaWindow5h, now)
	listing.Codex7dUsage = account.CodexUsageProgress(service.CodexQuotaWindow7d, now)
	listing.CodexUsageUpdatedAt = account.CodexUsageUpdatedAt()
	listing.Anthropic5hLimitPercent = listing.Codex5hLimitPercent
	listing.Anthropic7dLimitPercent = listing.Codex7dLimitPercent
	if reason := account.AnthropicQuotaProtectionReasonAt(now); reason != "" {
		listing.AnthropicQuotaProtectionReason = &reason
		listing.AnthropicQuotaProtectionResetAt = account.AnthropicQuotaProtectionResetAt(now)
	}
	listing.Anthropic5hUsage = account.AnthropicUsageProgress(service.AnthropicQuotaWindow5h, now)
	listing.Anthropic7dUsage = account.AnthropicUsageProgress(service.AnthropicQuotaWindow7d, now)
	listing.AnthropicUsageUpdatedAt = account.AnthropicUsageUpdatedAt()
	if reason := account.OpencodeQuotaProtectionReasonAt(now); reason != "" {
		listing.OpencodeQuotaProtectionReason = &reason
		listing.OpencodeQuotaProtectionResetAt = account.OpencodeQuotaProtectionResetAt(now)
	}
	listing.Opencode5hUsage = account.OpencodeUsageProgress(service.OpencodeQuotaWindow5h, now)
	listing.Opencode7dUsage = account.OpencodeUsageProgress(service.OpencodeQuotaWindow7d, now)
	listing.Opencode30dUsage = account.OpencodeUsageProgress(service.OpencodeQuotaWindow30d, now)
	listing.OpencodeUsageUpdatedAt = account.OpencodeUsageUpdatedAt()
	if currentMembershipID.Valid {
		listing.CurrentMembershipID = &currentMembershipID.Int64
	}
	if currentAPIKeyID.Valid {
		listing.CurrentAPIKeyID = &currentAPIKeyID.Int64
	}
	listing.CurrentAPIKeyName = strings.TrimSpace(currentAPIKeyName.String)
	if currentJoinedAt.Valid {
		listing.CurrentJoinedAt = &currentJoinedAt.Time
	}
	if currentPaidUntil.Valid {
		listing.CurrentPaidUntil = &currentPaidUntil.Time
	}
	if currentBilledUntil.Valid {
		listing.CurrentBilledUntil = &currentBilledUntil.Time
	}
	if currentIdleTimeoutMinutes.Valid {
		minutes := int(currentIdleTimeoutMinutes.Int64)
		listing.CurrentIdleTimeoutMinutes = &minutes
		if minutes > 0 {
			base := listing.CurrentJoinedAt
			if currentLastRequestAt.Valid {
				listing.CurrentLastRequestAt = &currentLastRequestAt.Time
				base = &currentLastRequestAt.Time
			}
			if base != nil {
				deadline := base.Add(time.Duration(minutes) * time.Minute)
				listing.CurrentIdleExpiresAt = &deadline
			}
		}
	}
	if currentLastRequestAt.Valid && listing.CurrentLastRequestAt == nil {
		listing.CurrentLastRequestAt = &currentLastRequestAt.Time
	}
	isOwnerSelfUse := currentConsumerUserID.Valid && listing.OwnerUserID > 0 && currentConsumerUserID.Int64 == listing.OwnerUserID
	if !isOwnerSelfUse && listing.CurrentMembershipID != nil && listing.HourlyRate > 0 && listing.HourlyFeeWaiverMinimum > 0 && listing.CurrentJoinedAt != nil {
		usageAmount := decimal.Zero
		if currentWaiverWindowUsageAmount.Valid {
			parsed, err := decimal.NewFromString(strings.TrimSpace(currentWaiverWindowUsageAmount.String))
			if err != nil {
				return nil, err
			}
			if parsed.GreaterThan(decimal.Zero) {
				usageAmount = parsed.Round(10)
			}
		}
		membership := accountShareWaiverProgressMembership{
			ID:                       *listing.CurrentMembershipID,
			JoinedAt:                 *listing.CurrentJoinedAt,
			LastRequestAt:            listing.CurrentLastRequestAt,
			HourlyRate:               listing.HourlyRate,
			WaiverMinimum:            listing.HourlyFeeWaiverMinimum,
			WaiverWindowStartedAt:    sqlNullTimePtr(currentWaiverWindowStartedAt),
			WaiverWindowUsageAmount:  usageAmount,
			WaiverWindowRequestCount: currentWaiverWindowRequestCount.Int64,
			WaiverWindowLastRequest:  sqlNullTimePtr(currentWaiverWindowLastRequestAt),
		}
		windowStart := accountShareWaiverWindowStartAt(membership.JoinedAt, now.UTC())
		usage := accountShareModeUsageStat{}
		if membership.WaiverWindowStartedAt != nil && membership.WaiverWindowStartedAt.UTC().Equal(windowStart) {
			usage = accountShareModeUsageStat{
				Total:         membership.WaiverWindowUsageAmount,
				RequestCount:  membership.WaiverWindowRequestCount,
				LastRequestAt: membership.WaiverWindowLastRequest,
			}
		}
		listing.CurrentWaiverProgress = buildAccountShareWaiverProgress(membership, usage, now.UTC())
	}
	if queueMembershipID.Valid {
		listing.QueueMembershipID = &queueMembershipID.Int64
	}
	if queueAPIKeyID.Valid {
		listing.QueueAPIKeyID = &queueAPIKeyID.Int64
	}
	listing.QueueAPIKeyName = strings.TrimSpace(queueAPIKeyName.String)
	if queueRank.Valid {
		rank := int(queueRank.Int64)
		listing.QueueRank = &rank
	}
	if queueStatus.Valid {
		listing.QueueStatus = queueStatus.String
	}
	listing.QueueEndingOperationID = strings.TrimSpace(queueEndingOperationID.String)
	listing.QueueEndingOperationStatus = strings.TrimSpace(queueEndingOperationStatus.String)
	listing.QueueSettlementStatus = strings.TrimSpace(queueSettlementStatus.String)
	if queueIdleTimeoutMinutes.Valid {
		minutes := int(queueIdleTimeoutMinutes.Int64)
		listing.QueueIdleTimeoutMinutes = &minutes
	}
	if queueDispatchCooldownUntil.Valid {
		listing.QueueDispatchCooldownUntil = &queueDispatchCooldownUntil.Time
	}
	if lastUsedMembershipID.Valid {
		listing.LastUsedMembershipID = &lastUsedMembershipID.Int64
	}
	if lastUsedAt.Valid {
		listing.LastUsedAt = &lastUsedAt.Time
	}
	listing.AccountSampleScope = service.AccountShareAccountSampleScopeRepresentative
	return listing, nil
}

func accountShareModeGroupName(platform string) string {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case service.PlatformOpenAI, "":
		return "OpenAI账号模式"
	default:
		return strings.ToUpper(platform[:1]) + platform[1:] + "账号模式"
	}
}

func ensureAccountShareListingNameAvailable(ctx context.Context, tx *sql.Tx, ownerUserID int64, accountName string) error {
	return ensureAccountShareListingNameAvailableForUpdate(ctx, tx, ownerUserID, 0, accountName)
}

func ensureAccountShareListingNameAvailableForUpdate(ctx context.Context, tx *sql.Tx, ownerUserID int64, excludeAccountID int64, accountName string) error {
	accountName = strings.TrimSpace(accountName)
	if ownerUserID <= 0 || accountName == "" {
		return nil
	}
	lockKey := fmt.Sprintf("account_share_listing_name:%d:%s", ownerUserID, strings.ToLower(accountName))
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtext($1)::bigint)", lockKey); err != nil {
		return err
	}

	var duplicateID int64
	err := tx.QueryRowContext(ctx, `
		SELECT l.id
		FROM account_share_listings l
		WHERE l.owner_user_id = $1
			AND LOWER(BTRIM(l.room_name)) = LOWER(BTRIM($2))
			AND (
				$3::bigint <= 0
				OR NOT EXISTS (
					SELECT 1
					FROM account_share_room_accounts room_account
					WHERE room_account.listing_id = l.id
						AND room_account.account_id = $3
				)
			)
			AND l.deleted_at IS NULL
		LIMIT 1
	`, ownerUserID, accountName, excludeAccountID).Scan(&duplicateID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return service.ErrAccountShareModeDuplicateName
}

func ensureAccountShareRoomNameAvailableForUpdate(ctx context.Context, tx *sql.Tx, ownerUserID, excludeListingID int64, roomName string) error {
	roomName = strings.TrimSpace(roomName)
	if ownerUserID <= 0 || roomName == "" {
		return nil
	}
	lockKey := fmt.Sprintf("account_share_room_name:%d:%s", ownerUserID, strings.ToLower(roomName))
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtext($1)::bigint)", lockKey); err != nil {
		return err
	}

	var duplicateID int64
	err := tx.QueryRowContext(ctx, `
		SELECT l.id
		FROM account_share_listings l
		WHERE l.owner_user_id = $1
			AND LOWER(BTRIM(l.room_name)) = LOWER(BTRIM($2))
			AND ($3::bigint <= 0 OR l.id <> $3::bigint)
			AND l.deleted_at IS NULL
		LIMIT 1
	`, ownerUserID, roomName, excludeListingID).Scan(&duplicateID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return service.ErrAccountShareModeDuplicateName
}

func ensureAccountShareProxyVisibleInTx(ctx context.Context, tx *sql.Tx, ownerUserID, proxyID int64) error {
	if ownerUserID <= 0 {
		return service.ErrUserNotFound
	}
	if proxyID <= 0 {
		return service.ErrAccountShareModeProxyRequired
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM proxies
			WHERE id = $1
				AND status = $2
				AND deleted_at IS NULL
				AND (owner_user_id IS NULL OR owner_user_id = $3)
		)
	`, proxyID, service.StatusActive, ownerUserID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return service.ErrProxyNotFound
	}
	return nil
}

func ensureAccountShareProxyCapacityInTx(ctx context.Context, tx *sql.Tx, ownerUserID, proxyID, excludeAccountID int64) error {
	if ownerUserID <= 0 {
		return service.ErrUserNotFound
	}
	if proxyID <= 0 {
		return service.ErrAccountShareModeProxyRequired
	}

	var maxAccounts int
	if err := tx.QueryRowContext(ctx, `
		SELECT max_accounts
		FROM proxies
		WHERE id = $1
			AND status = $2
			AND deleted_at IS NULL
			AND (owner_user_id IS NULL OR owner_user_id = $3)
		FOR UPDATE
	`, proxyID, service.StatusActive, ownerUserID).Scan(&maxAccounts); errors.Is(err, sql.ErrNoRows) {
		return service.ErrProxyNotFound
	} else if err != nil {
		return err
	}
	if maxAccounts <= 0 {
		return nil
	}

	var current int64
	args := []any{proxyID}
	query := `
		SELECT COUNT(*)
		FROM accounts
		WHERE proxy_id = $1
			AND deleted_at IS NULL
	`
	if excludeAccountID > 0 {
		args = append(args, excludeAccountID)
		query += " AND id <> $2"
	}
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&current); err != nil {
		return err
	}
	if current+1 > int64(maxAccounts) {
		return service.ProxyAccountLimitExceededError(proxyID, current, int64(maxAccounts), 1)
	}
	return nil
}
