package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
)

type accountShareModeRepository struct {
	db *sql.DB
}

const (
	accountShareSeatSettlementTypeCharge       = "seat_charge"
	accountShareSeatSettlementTypeRefund       = "seat_refund"
	accountShareSeatSettlementTypeWaiverRefund = "seat_waiver_refund"
	accountShareSeatPrepayReason               = "account_share_mode_seat_prepay"
	accountShareSeatRefundReason               = "account_share_mode_seat_refund"
	accountShareSeatWaiverRefundReason         = "account_share_mode_seat_waiver_refund"
	accountShareSeatIncomeReason               = "account_share_mode_income"
	accountShareModeSettlementRefType          = "account_share_mode_settlement"
)

func NewAccountShareModeRepository(_ *dbent.Client, sqlDB *sql.DB) service.AccountShareModeRepository {
	return &accountShareModeRepository{db: sqlDB}
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

func (r *accountShareModeRepository) CreateOpenAIListing(ctx context.Context, account *service.Account, listing *service.AccountShareListing, modeGroupID int64) (*service.AccountShareListing, error) {
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
	if err := ensureAccountShareListingNameAvailable(ctx, tx, ownerUserID, account.Name); err != nil {
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

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO account_groups (account_id, group_id, priority, created_at)
		VALUES ($1, $2, 1, NOW())
		ON CONFLICT (account_id, group_id) DO NOTHING
	`, account.ID, modeGroupID); err != nil {
		return nil, err
	}

	listing.AccountID = account.ID
	listing.OwnerUserID = ownerUserID
	if listing.Status == "" {
		listing.Status = service.AccountShareListingStatusActive
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
			account_id, owner_user_id, status, seat_limit, rate_multiplier, allowed_models,
			per_user_concurrency, hourly_rate, hourly_fee_waiver_minimum, min_balance_required, codex_cli_only,
			codex_5h_limit_percent, codex_7d_limit_percent, created_at, updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6::jsonb,
			$7, $8, $9, $10, $11,
			$12, $13, NOW(), NOW()
		)
		RETURNING id
	`,
		listing.AccountID,
		listing.OwnerUserID,
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
	).Scan(&listingID)
	if err != nil {
		return nil, err
	}

	if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &account.ID, nil, buildSchedulerGroupPayload([]int64{modeGroupID})); err != nil {
		logger.LegacyPrintf("repository.account_share_mode", "[SchedulerOutbox] enqueue shared account create failed: account=%d err=%v", account.ID, err)
	}
	if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountGroupsChanged, &account.ID, nil, buildSchedulerGroupPayload([]int64{modeGroupID})); err != nil {
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

func (r *accountShareModeRepository) GetListingByAccountID(ctx context.Context, accountID int64) (*service.AccountShareListing, error) {
	return r.queryOneListing(ctx, 0, "l.account_id = $2", accountID)
}

func (r *accountShareModeRepository) ListListings(ctx context.Context, viewerUserID int64, filters service.AccountShareListingFilters, params pagination.PaginationParams) ([]service.AccountShareListing, *pagination.PaginationResult, error) {
	page := params.Page
	if page < 1 {
		page = 1
	}
	limit := params.Limit()
	offset := (page - 1) * limit

	whereParts := []string{"l.deleted_at IS NULL", "a.deleted_at IS NULL"}
	args := []any{viewerUserID}
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	applyStatusFilter := func(defaultActive bool) {
		switch filters.Status {
		case "all":
			return
		case service.AccountShareListingStatusActive, service.AccountShareListingStatusPaused, service.AccountShareListingStatusDisabled:
			whereParts = append(whereParts, "l.status = "+addArg(filters.Status))
		default:
			if defaultActive {
				whereParts = append(whereParts, "l.status = '"+service.AccountShareListingStatusActive+"'")
			}
		}
	}
	switch filters.Tab {
	case service.AccountShareModeListingTabUsing:
		whereParts = append(whereParts, "cm.id IS NOT NULL")
		applyStatusFilter(false)
	case service.AccountShareModeListingTabHistory:
		whereParts = append(whereParts, "hm.id IS NOT NULL", "cm.id IS NULL")
		if filters.Status == "" {
			whereParts = append(whereParts, "l.status <> '"+service.AccountShareListingStatusDisabled+"'")
		} else {
			applyStatusFilter(false)
		}
	case service.AccountShareModeListingTabMine:
		if !filters.ViewerIsAdmin {
			whereParts = append(whereParts, "l.owner_user_id = $1")
		}
		applyStatusFilter(false)
	default:
		applyStatusFilter(true)
	}
	if filters.AvailableOnly {
		whereParts = append(whereParts, accountShareListingAvailableConditionSQL("NOW()"))
	}
	if filters.SeatLimit >= service.AccountShareModeMinSeats && filters.SeatLimit <= service.AccountShareModeMaxSeats {
		whereParts = append(whereParts, "l.seat_limit = "+addArg(filters.SeatLimit))
	}
	if filters.Search != "" {
		placeholder := addArg("%" + filters.Search + "%")
		whereParts = append(whereParts, fmt.Sprintf(`(
			a.name ILIKE %[1]s
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
	if filters.PerUserConcurrencyMin != nil {
		whereParts = append(whereParts, "l.per_user_concurrency >= "+addArg(*filters.PerUserConcurrencyMin))
	}
	if filters.PerUserConcurrencyMax != nil {
		whereParts = append(whereParts, "l.per_user_concurrency <= "+addArg(*filters.PerUserConcurrencyMax))
	}
	if filters.MinBalanceRequiredMin != nil {
		whereParts = append(whereParts, "l.min_balance_required >= "+addArg(*filters.MinBalanceRequiredMin))
	}
	if filters.MinBalanceRequiredMax != nil {
		whereParts = append(whereParts, "l.min_balance_required <= "+addArg(*filters.MinBalanceRequiredMax))
	}
	if filters.HourlyRateMin != nil {
		whereParts = append(whereParts, "l.hourly_rate >= "+addArg(*filters.HourlyRateMin))
	}
	if filters.HourlyRateMax != nil {
		whereParts = append(whereParts, "l.hourly_rate <= "+addArg(*filters.HourlyRateMax))
	}
	if filters.HourlyFeeWaiverMin != nil {
		whereParts = append(whereParts, "l.hourly_fee_waiver_minimum >= "+addArg(*filters.HourlyFeeWaiverMin))
	}
	if filters.HourlyFeeWaiverMax != nil {
		whereParts = append(whereParts, "l.hourly_fee_waiver_minimum <= "+addArg(*filters.HourlyFeeWaiverMax))
	}
	if len(filters.Models) > 0 {
		whereParts = append(whereParts, fmt.Sprintf(`EXISTS (
			SELECT 1
			FROM jsonb_array_elements_text(l.allowed_models) AS model(value)
			WHERE lower(model.value) = ANY(%s)
		)`, addArg(pq.Array(lowerAccountShareModels(filters.Models)))))
	}
	if filters.AccountLevel != "" {
		whereParts = append(whereParts, fmt.Sprintf("%s = %s", accountShareEffectiveAccountLevelSQL(), addArg(filters.AccountLevel)))
	}
	whereSQL := strings.Join(whereParts, " AND ")

	approximatePagination := accountShareListingUsesApproximatePagination(filters)
	var total int64
	if !approximatePagination {
		countQuery := fmt.Sprintf(`
			SELECT COUNT(*)
			FROM account_share_listings l
			JOIN accounts a ON a.id = l.account_id
			LEFT JOIN users u ON u.id = l.owner_user_id
			LEFT JOIN LATERAL (
				SELECT m.id
				FROM account_share_memberships m
				WHERE m.listing_id = l.id
					AND m.consumer_user_id = $1
					AND m.status = '%s'
					AND m.deleted_at IS NULL
					AND (m.hourly_rate_snapshot <= 0 OR m.paid_until IS NULL OR m.paid_until > NOW())
				ORDER BY m.joined_at DESC
				LIMIT 1
			) cm ON TRUE
			LEFT JOIN LATERAL (
				SELECT m.id
				FROM account_share_memberships m
				WHERE m.listing_id = l.id
					AND m.consumer_user_id = $1
					AND m.status = '%s'
					AND m.deleted_at IS NULL
				ORDER BY COALESCE(m.ended_at, m.updated_at) DESC
				LIMIT 1
			) hm ON TRUE
			WHERE %s
		`, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusEnded, whereSQL)
		if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
			return nil, nil, err
		}
	}

	queryLimit := limit
	if approximatePagination {
		queryLimit = limit + 1
	}
	args = append(args, queryLimit, offset)
	query := fmt.Sprintf(`
		%s
		WHERE %s
		ORDER BY
			CASE WHEN cm.id IS NOT NULL THEN 0 ELSE 1 END,
			COALESCE(cm.joined_at, hm.ended_at, l.updated_at) DESC,
			l.id DESC
		LIMIT $%d OFFSET $%d
	`, accountShareListingSelectSQL(), whereSQL, len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

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
		Total:    total,
		Page:     page,
		PageSize: limit,
		Pages:    pages,
	}, nil
}

func (r *accountShareModeRepository) UpdateListing(ctx context.Context, actorUserID int64, actorIsAdmin bool, listingID int64, input service.UpdateAccountShareListingInput) (*service.AccountShareListing, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	var accountID, ownerUserID int64
	var currentSeatLimit, currentPerUserConcurrency, currentAccountConcurrency int
	var currentProxyID sql.NullInt64
	var activeEditSession sql.NullString
	var editingByUserID sql.NullInt64
	var editingExpiresAt sql.NullTime
	ownerPredicate := ""
	selectArgs := []any{listingID}
	if !actorIsAdmin {
		selectArgs = append(selectArgs, actorUserID)
		ownerPredicate = fmt.Sprintf("AND l.owner_user_id = $%d", len(selectArgs))
	}
	selectQuery := fmt.Sprintf(`
		SELECT l.account_id, l.owner_user_id, l.seat_limit, l.per_user_concurrency, a.concurrency,
			a.proxy_id, l.edit_session_id, l.editing_by_user_id, l.editing_expires_at
		FROM account_share_listings l
		JOIN accounts a ON a.id = l.account_id AND a.deleted_at IS NULL
		WHERE l.id = $1
			%s
			AND l.deleted_at IS NULL
		FOR UPDATE OF l
	`, ownerPredicate)
	if err := tx.QueryRowContext(ctx, selectQuery, selectArgs...).Scan(&accountID, &ownerUserID, &currentSeatLimit, &currentPerUserConcurrency, &currentAccountConcurrency, &currentProxyID, &activeEditSession, &editingByUserID, &editingExpiresAt); errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	} else if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	activeEdit := activeEditSession.Valid && editingExpiresAt.Valid && editingExpiresAt.Time.After(now)
	if activeEdit && (strings.TrimSpace(input.EditSessionID) == "" || activeEditSession.String != input.EditSessionID || !editingByUserID.Valid || editingByUserID.Int64 != actorUserID) {
		return nil, service.ErrAccountShareListingEditing
	}
	configUpdate := accountShareListingConfigUpdateRequiresEditSession(input)
	if configUpdate {
		if !activeEdit {
			return nil, service.ErrAccountShareEditSessionInvalid
		}
		activeSeats, err := activeAccountShareSeatCountInTx(ctx, tx, listingID)
		if err != nil {
			return nil, err
		}
		if activeSeats > 0 && !(actorIsAdmin && input.ForceActiveEdit) {
			return nil, service.ErrAccountShareListingInUse
		}
		if input.ProxyID != nil {
			if err := ensureAccountShareProxyVisibleInTx(ctx, tx, ownerUserID, *input.ProxyID); err != nil {
				return nil, err
			}
			if !currentProxyID.Valid || currentProxyID.Int64 != *input.ProxyID {
				if err := ensureAccountShareProxyCapacityInTx(ctx, tx, ownerUserID, *input.ProxyID, accountID); err != nil {
					return nil, err
				}
			}
		}
		if input.Name != nil {
			if err := ensureAccountShareListingNameAvailableForUpdate(ctx, tx, ownerUserID, accountID, *input.Name); err != nil {
				return nil, err
			}
		}
	}

	nextSeatLimit := currentSeatLimit
	nextPerUserConcurrency := currentPerUserConcurrency
	nextAccountConcurrency := currentAccountConcurrency
	if input.SeatLimit != nil {
		nextSeatLimit = *input.SeatLimit
	}
	if input.PerUserConcurrency != nil {
		nextPerUserConcurrency = *input.PerUserConcurrency
	}
	if input.Concurrency != nil {
		nextAccountConcurrency = *input.Concurrency
	}
	if nextAccountConcurrency < nextSeatLimit*nextPerUserConcurrency {
		return nil, service.ErrAccountShareModeInsufficientConcurrency
	}

	setParts := []string{"updated_at = NOW()"}
	updateArgs := []any{}
	addArg := func(value any) string {
		updateArgs = append(updateArgs, value)
		return fmt.Sprintf("$%d", len(updateArgs))
	}
	if input.Status != nil {
		status := strings.ToLower(strings.TrimSpace(*input.Status))
		switch status {
		case service.AccountShareListingStatusActive, service.AccountShareListingStatusPaused, service.AccountShareListingStatusDisabled:
			setParts = append(setParts, "status = "+addArg(status))
		default:
			return nil, service.ErrAccountShareListingNotActive
		}
	}
	if input.SeatLimit != nil {
		setParts = append(setParts, "seat_limit = "+addArg(*input.SeatLimit))
	}
	if input.RateMultiplier != nil {
		setParts = append(setParts, "rate_multiplier = "+addArg(*input.RateMultiplier))
	}
	if input.AllowedModels != nil {
		modelsJSON, err := json.Marshal(*input.AllowedModels)
		if err != nil {
			return nil, err
		}
		setParts = append(setParts, "allowed_models = "+addArg(string(modelsJSON))+"::jsonb")
	}
	if input.PerUserConcurrency != nil {
		setParts = append(setParts, "per_user_concurrency = "+addArg(*input.PerUserConcurrency))
	}
	if input.HourlyRate != nil {
		setParts = append(setParts, "hourly_rate = "+addArg(*input.HourlyRate))
	}
	if input.HourlyFeeWaiverMinimum != nil {
		setParts = append(setParts, "hourly_fee_waiver_minimum = "+addArg(*input.HourlyFeeWaiverMinimum))
	}
	if input.MinBalanceRequired != nil {
		setParts = append(setParts, "min_balance_required = "+addArg(*input.MinBalanceRequired))
	}
	if input.CodexCLIOnly != nil {
		setParts = append(setParts, "codex_cli_only = "+addArg(*input.CodexCLIOnly))
	}
	if input.Codex5hLimitPercent != nil {
		setParts = append(setParts, "codex_5h_limit_percent = "+addArg(*input.Codex5hLimitPercent))
	}
	if input.Codex7dLimitPercent != nil {
		setParts = append(setParts, "codex_7d_limit_percent = "+addArg(*input.Codex7dLimitPercent))
	}
	if configUpdate {
		setParts = append(setParts,
			"edit_session_id = NULL",
			"editing_by_user_id = NULL",
			"editing_started_at = NULL",
			"editing_expires_at = NULL",
		)
	}

	listingArg := addArg(listingID)
	ownerUpdatePredicate := ""
	if !actorIsAdmin {
		ownerUpdatePredicate = "AND owner_user_id = " + addArg(actorUserID)
	}
	query := fmt.Sprintf(`
		UPDATE account_share_listings
		SET %s
		WHERE id = %s
			%s
			AND deleted_at IS NULL
	`, strings.Join(setParts, ", "), listingArg, ownerUpdatePredicate)
	if _, err := tx.ExecContext(ctx, query, updateArgs...); err != nil {
		return nil, err
	}

	accountSetParts := []string{"updated_at = NOW()"}
	accountArgs := []any{}
	addAccountArg := func(value any) string {
		accountArgs = append(accountArgs, value)
		return fmt.Sprintf("$%d", len(accountArgs))
	}
	accountChanged := false
	if input.Name != nil {
		accountSetParts = append(accountSetParts, "name = "+addAccountArg(*input.Name))
		accountChanged = true
	}
	if input.ProxyID != nil {
		accountSetParts = append(accountSetParts, "proxy_id = "+addAccountArg(*input.ProxyID))
		accountChanged = true
	}
	if input.AllowedModels != nil {
		modelMappingJSON, err := json.Marshal(service.AccountShareModeAllowedModelsMapping(*input.AllowedModels))
		if err != nil {
			return nil, err
		}
		accountSetParts = append(accountSetParts, "credentials = jsonb_set(COALESCE(credentials, '{}'::jsonb), '{model_mapping}', "+addAccountArg(string(modelMappingJSON))+"::jsonb, true)")
		accountChanged = true
	}

	if input.Concurrency != nil {
		accountSetParts = append(accountSetParts, "concurrency = "+addAccountArg(*input.Concurrency))
		accountChanged = true
	}

	extraExpr := "COALESCE(extra, '{}'::jsonb)"
	extraChanged := false
	addExtraSet := func(key string, value any) error {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		extraExpr = fmt.Sprintf("jsonb_set(%s, '{%s}', %s::jsonb, true)", extraExpr, key, addAccountArg(string(raw)))
		extraChanged = true
		return nil
	}
	if input.CodexCLIOnly != nil {
		if err := addExtraSet("codex_cli_only", *input.CodexCLIOnly); err != nil {
			return nil, err
		}
	}
	if input.Codex5hLimitPercent != nil {
		if err := addExtraSet("codex_5h_limit_percent", *input.Codex5hLimitPercent); err != nil {
			return nil, err
		}
	}
	if input.Codex7dLimitPercent != nil {
		if err := addExtraSet("codex_7d_limit_percent", *input.Codex7dLimitPercent); err != nil {
			return nil, err
		}
	}
	if extraChanged {
		accountSetParts = append(accountSetParts, "extra = "+extraExpr)
		accountChanged = true
	}

	if accountChanged {
		accountArgs = append(accountArgs, accountID, ownerUserID)
		accountIDArg := fmt.Sprintf("$%d", len(accountArgs)-1)
		ownerIDArg := fmt.Sprintf("$%d", len(accountArgs))
		accountQuery := fmt.Sprintf(`
			UPDATE accounts
			SET %s
			WHERE id = %s
				AND owner_user_id = %s
				AND deleted_at IS NULL
		`, strings.Join(accountSetParts, ", "), accountIDArg, ownerIDArg)
		if _, err := tx.ExecContext(ctx, accountQuery, accountArgs...); err != nil {
			return nil, err
		}
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
			logger.LegacyPrintf("repository.account_share_mode", "[SchedulerOutbox] enqueue account share listing update failed: account=%d err=%v", accountID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return r.GetListingByID(ctx, listingID, ownerUserID)
}

func (r *accountShareModeRepository) BeginListingEdit(ctx context.Context, actorUserID int64, actorIsAdmin bool, listingID int64, input service.BeginAccountShareListingEditInput) (*service.AccountShareListing, error) {
	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" || input.Expires.IsZero() {
		return nil, service.ErrAccountShareEditSessionRequired
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
	var activeSession sql.NullString
	var editingByUserID sql.NullInt64
	var editingExpiresAt sql.NullTime
	ownerPredicate := ""
	selectArgs := []any{listingID}
	if !actorIsAdmin {
		selectArgs = append(selectArgs, actorUserID)
		ownerPredicate = fmt.Sprintf("AND l.owner_user_id = $%d", len(selectArgs))
	}
	selectQuery := fmt.Sprintf(`
		SELECT l.owner_user_id, l.edit_session_id, l.editing_by_user_id, l.editing_expires_at
		FROM account_share_listings l
		JOIN accounts a ON a.id = l.account_id AND a.deleted_at IS NULL
		WHERE l.id = $1
			%s
			AND l.deleted_at IS NULL
		FOR UPDATE OF l
	`, ownerPredicate)
	if err := tx.QueryRowContext(ctx, selectQuery, selectArgs...).Scan(&ownerUserID, &activeSession, &editingByUserID, &editingExpiresAt); errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	} else if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if activeSession.Valid && editingExpiresAt.Valid && editingExpiresAt.Time.After(now) &&
		(activeSession.String != sessionID || !editingByUserID.Valid || editingByUserID.Int64 != actorUserID) {
		return nil, service.ErrAccountShareListingEditing
	}

	activeSeats, err := activeAccountShareSeatCountInTx(ctx, tx, listingID)
	if err != nil {
		return nil, err
	}
	if activeSeats > 0 && !(actorIsAdmin && input.Force) {
		return nil, service.ErrAccountShareListingInUse
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE account_share_listings
		SET edit_session_id = $1::varchar,
			editing_by_user_id = $2::bigint,
			editing_started_at = CASE
				WHEN edit_session_id = $1::varchar AND editing_by_user_id = $2::bigint THEN COALESCE(editing_started_at, NOW())
				ELSE NOW()
			END,
			editing_expires_at = $3::timestamptz,
			updated_at = NOW()
		WHERE id = $4::bigint
			AND deleted_at IS NULL
	`, sessionID, actorUserID, input.Expires, listingID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return r.GetListingByID(ctx, listingID, actorUserID)
}

func (r *accountShareModeRepository) ReleaseListingEdit(ctx context.Context, actorUserID int64, actorIsAdmin bool, listingID int64, sessionID string) (*service.AccountShareListing, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, service.ErrAccountShareEditSessionRequired
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

	ownerPredicate := ""
	args := []any{listingID, sessionID}
	if !actorIsAdmin {
		args = append(args, actorUserID)
		ownerPredicate = "AND owner_user_id = $3"
	}
	query := fmt.Sprintf(`
		UPDATE account_share_listings
		SET edit_session_id = NULL,
			editing_by_user_id = NULL,
			editing_started_at = NULL,
			editing_expires_at = NULL,
			updated_at = NOW()
		WHERE id = $1
			AND edit_session_id = $2
			%s
			AND deleted_at IS NULL
	`, ownerPredicate)
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, service.ErrAccountShareEditSessionInvalid
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return r.GetListingByID(ctx, listingID, actorUserID)
}

func accountShareListingConfigUpdateRequiresEditSession(input service.UpdateAccountShareListingInput) bool {
	if input.AllowedModels != nil &&
		input.Name == nil &&
		input.ProxyID == nil &&
		input.Status == nil &&
		input.SeatLimit == nil &&
		input.RateMultiplier == nil &&
		input.PerUserConcurrency == nil &&
		input.HourlyRate == nil &&
		input.HourlyFeeWaiverMinimum == nil &&
		input.MinBalanceRequired == nil &&
		input.CodexCLIOnly == nil &&
		input.Codex5hLimitPercent == nil &&
		input.Codex7dLimitPercent == nil &&
		input.Concurrency == nil &&
		!input.ForceActiveEdit {
		return false
	}
	return input.Name != nil ||
		input.ProxyID != nil ||
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
		input.Concurrency != nil
}

func (r *accountShareModeRepository) JoinListing(ctx context.Context, consumerUserID int64, apiKeyID int64, listingID int64, idleTimeoutMinutes int) (*service.AccountShareMembership, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	var accountID, ownerUserID int64
	var status string
	var seatLimit int
	var hourlyRate, hourlyFeeWaiverMinimum, minBalanceRequired float64
	var editSession sql.NullString
	var editingExpiresAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT l.account_id, l.owner_user_id, l.status, l.seat_limit, l.hourly_rate, l.hourly_fee_waiver_minimum, l.min_balance_required,
			l.edit_session_id, l.editing_expires_at
		FROM account_share_listings l
		JOIN accounts a ON a.id = l.account_id AND a.deleted_at IS NULL
		WHERE l.id = $1
			AND l.deleted_at IS NULL
		FOR UPDATE OF l
	`, listingID).Scan(&accountID, &ownerUserID, &status, &seatLimit, &hourlyRate, &hourlyFeeWaiverMinimum, &minBalanceRequired, &editSession, &editingExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, err
	}
	if editSession.Valid && editingExpiresAt.Valid && editingExpiresAt.Time.After(time.Now().UTC()) {
		return nil, service.ErrAccountShareListingEditing
	}
	if ownerUserID == consumerUserID {
		return nil, service.ErrAccountShareOwnerCannotJoin
	}
	if status != service.AccountShareListingStatusActive {
		return nil, service.ErrAccountShareListingNotActive
	}
	now := time.Now().UTC()
	unavailable, err := r.accountShareAccountUnavailableInTx(ctx, tx, accountID, now)
	if err != nil {
		return nil, err
	}
	if unavailable {
		return nil, service.ErrAccountShareAccountUnavailable
	}
	prepayDuration := service.AccountShareModeSeatPrepayDuration
	prepayAmount := accountShareSeatCharge(hourlyRate, prepayDuration)
	paidUntil := now.Add(prepayDuration)
	var userBalance float64
	if err := tx.QueryRowContext(ctx, `
		SELECT balance
		FROM users
		WHERE id = $1
			AND deleted_at IS NULL
		FOR UPDATE
	`, consumerUserID).Scan(&userBalance); errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUserNotFound
	} else if err != nil {
		return nil, err
	}
	if userBalance < minBalanceRequired {
		return nil, service.ErrAccountShareBalanceBelowMinimum
	}
	if prepayAmount > 0 && userBalance < minBalanceRequired+prepayAmount {
		return nil, service.ErrAccountShareModePrepayInsufficient
	}

	var activeSeats int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM account_share_memberships
		WHERE listing_id = $1
			AND status = $2
			AND deleted_at IS NULL
			AND (hourly_rate_snapshot <= 0 OR paid_until IS NULL OR paid_until > NOW())
			AND (idle_timeout_minutes <= 0 OR COALESCE(last_request_at, joined_at) + (idle_timeout_minutes * INTERVAL '1 minute') > NOW())
	`, listingID, service.AccountShareMembershipStatusActive).Scan(&activeSeats); err != nil {
		return nil, err
	}
	if activeSeats >= seatLimit {
		return nil, service.ErrAccountShareListingFull
	}

	if exists, err := existsInTx(ctx, tx, `
		SELECT 1
		FROM account_share_memberships
		WHERE consumer_user_id = $1
			AND status = $2
			AND deleted_at IS NULL
		LIMIT 1
	`, consumerUserID, service.AccountShareMembershipStatusActive); err != nil {
		return nil, err
	} else if exists {
		return nil, service.ErrAccountShareAlreadyUsing
	}
	if exists, err := existsInTx(ctx, tx, `
		SELECT 1
		FROM account_share_memberships
		WHERE api_key_id = $1
			AND status = $2
			AND deleted_at IS NULL
		LIMIT 1
	`, apiKeyID, service.AccountShareMembershipStatusActive); err != nil {
		return nil, err
	} else if exists {
		return nil, service.ErrAccountShareAPIKeyAlreadyBound
	}

	membership := &service.AccountShareMembership{}
	var endedAt, lastRequestAt sql.NullTime
	var paidUntilScan, billedUntilScan sql.NullTime
	var endedReason sql.NullString
	var paidUntilValue any
	var billedUntilValue any = now
	if prepayAmount > 0 {
		paidUntilValue = paidUntil
	} else {
		paidUntilValue = nil
		billedUntilValue = nil
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO account_share_memberships (
			listing_id, account_id, consumer_user_id, api_key_id, status,
			hourly_rate_snapshot, hourly_fee_waiver_minimum_snapshot, idle_timeout_minutes, joined_at, last_request_at,
			ended_reason, paid_until, billed_until, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULL, NULL, $10, $11, NOW(), NOW())
		RETURNING id, listing_id, account_id, consumer_user_id, api_key_id, status,
			hourly_rate_snapshot, hourly_fee_waiver_minimum_snapshot, idle_timeout_minutes, joined_at, last_request_at, ended_at,
			ended_reason, paid_until, billed_until, created_at, updated_at
	`, listingID, accountID, consumerUserID, apiKeyID, service.AccountShareMembershipStatusActive, hourlyRate, hourlyFeeWaiverMinimum, idleTimeoutMinutes, now, paidUntilValue, billedUntilValue).Scan(
		&membership.ID,
		&membership.ListingID,
		&membership.AccountID,
		&membership.ConsumerUserID,
		&membership.APIKeyID,
		&membership.Status,
		&membership.HourlyRateSnapshot,
		&membership.HourlyFeeWaiverMinimumSnapshot,
		&membership.IdleTimeoutMinutes,
		&membership.JoinedAt,
		&lastRequestAt,
		&endedAt,
		&endedReason,
		&paidUntilScan,
		&billedUntilScan,
		&membership.CreatedAt,
		&membership.UpdatedAt,
	)
	if err != nil {
		return nil, translateAccountShareMembershipConflict(err)
	}
	if endedAt.Valid {
		membership.EndedAt = &endedAt.Time
	}
	if lastRequestAt.Valid {
		membership.LastRequestAt = &lastRequestAt.Time
	}
	if endedReason.Valid {
		membership.EndedReason = endedReason.String
	}
	if paidUntilScan.Valid {
		membership.PaidUntil = &paidUntilScan.Time
	}
	if billedUntilScan.Valid {
		membership.BilledUntil = &billedUntilScan.Time
	}
	membership.OwnerUserID = ownerUserID
	if prepayAmount > 0 {
		newBalance := userBalance - prepayAmount
		if _, err := tx.ExecContext(ctx, `
			UPDATE users
			SET balance = $1::numeric,
				updated_at = NOW()
			WHERE id = $2
				AND deleted_at IS NULL
		`, decimalFromSignedFloat(newBalance).StringFixed(10), consumerUserID); err != nil {
			return nil, err
		}
		if err := insertUserBalanceLedger(ctx, tx, userBalanceLedgerInput{
			UserID:       consumerUserID,
			Direction:    "debit",
			Amount:       decimalFromFloat(prepayAmount),
			Reason:       accountShareSeatPrepayReason,
			RefType:      "account_share_membership",
			RefID:        membership.ID,
			BalanceAfter: decimalFromSignedFloat(newBalance),
			Metadata: map[string]any{
				"listing_id":    listingID,
				"account_id":    accountID,
				"hourly_rate":   hourlyRate,
				"duration_ms":   int(prepayDuration.Milliseconds()),
				"paid_until":    paidUntil.Format(time.RFC3339),
				"prepay_stage":  "join",
				"seat_billing":  true,
				"consumer_user": consumerUserID,
			},
		}); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return membership, nil
}

func (r *accountShareModeRepository) EndMembership(ctx context.Context, consumerUserID int64, membershipID int64) (*service.AccountShareMembership, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	membership := &service.AccountShareMembership{}
	var endedAt, lastRequestAt, paidUntil, billedUntil sql.NullTime
	var endedReason sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT
			m.id, m.listing_id, m.account_id, l.owner_user_id, m.consumer_user_id, m.api_key_id,
			m.status, m.hourly_rate_snapshot, m.hourly_fee_waiver_minimum_snapshot, m.idle_timeout_minutes, m.joined_at, m.last_request_at,
			m.ended_at, m.ended_reason, m.paid_until, m.billed_until, m.created_at, m.updated_at
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
		WHERE m.id = $1
			AND m.consumer_user_id = $2
			AND m.status = $3
			AND m.deleted_at IS NULL
		FOR UPDATE OF m
	`,
		membershipID,
		consumerUserID,
		service.AccountShareMembershipStatusActive,
	).Scan(
		&membership.ID,
		&membership.ListingID,
		&membership.AccountID,
		&membership.OwnerUserID,
		&membership.ConsumerUserID,
		&membership.APIKeyID,
		&membership.Status,
		&membership.HourlyRateSnapshot,
		&membership.HourlyFeeWaiverMinimumSnapshot,
		&membership.IdleTimeoutMinutes,
		&membership.JoinedAt,
		&lastRequestAt,
		&endedAt,
		&endedReason,
		&paidUntil,
		&billedUntil,
		&membership.CreatedAt,
		&membership.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, err
	}
	if paidUntil.Valid {
		membership.PaidUntil = &paidUntil.Time
	}
	if billedUntil.Valid {
		membership.BilledUntil = &billedUntil.Time
	}
	if lastRequestAt.Valid {
		membership.LastRequestAt = &lastRequestAt.Time
	}
	if endedReason.Valid {
		membership.EndedReason = endedReason.String
	}

	now := time.Now().UTC()
	settledUntil, _, _, err := r.settleSeatChargeInTx(ctx, tx, membership, now)
	if err != nil {
		return nil, err
	}
	if err := r.refundUnusedSeatPrepayInTx(ctx, tx, membership, now); err != nil {
		return nil, err
	}
	if settledUntil == nil {
		settledUntil = &now
	}
	endedAtValue := now
	err = tx.QueryRowContext(ctx, `
		UPDATE account_share_memberships
		SET status = $1,
			ended_at = $2,
			ended_reason = $3,
			paid_until = $4,
			billed_until = $4,
			updated_at = NOW()
		WHERE id = $5
		RETURNING status, ended_at, ended_reason, paid_until, billed_until, updated_at
	`,
		service.AccountShareMembershipStatusEnded,
		endedAtValue,
		service.AccountShareMembershipEndReasonManual,
		*settledUntil,
		membership.ID,
	).Scan(&membership.Status, &endedAt, &endedReason, &paidUntil, &billedUntil, &membership.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if endedAt.Valid {
		membership.EndedAt = &endedAt.Time
	}
	if endedReason.Valid {
		membership.EndedReason = endedReason.String
	}
	if paidUntil.Valid {
		membership.PaidUntil = &paidUntil.Time
	}
	if billedUntil.Valid {
		membership.BilledUntil = &billedUntil.Time
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return membership, nil
}

func (r *accountShareModeRepository) UpdateMembershipIdleTimeout(ctx context.Context, consumerUserID int64, membershipID int64, idleTimeoutMinutes int) (*service.AccountShareMembership, error) {
	membership := &service.AccountShareMembership{}
	var endedAt, lastRequestAt, paidUntil, billedUntil sql.NullTime
	var endedReason sql.NullString
	err := r.db.QueryRowContext(ctx, `
		UPDATE account_share_memberships m
		SET idle_timeout_minutes = $1,
			updated_at = NOW()
		FROM account_share_listings l
		WHERE m.id = $2
			AND m.consumer_user_id = $3
			AND m.status = $4
			AND m.deleted_at IS NULL
			AND l.id = m.listing_id
		RETURNING
			m.id, m.listing_id, m.account_id, l.owner_user_id, m.consumer_user_id, m.api_key_id,
			m.status, m.hourly_rate_snapshot, m.hourly_fee_waiver_minimum_snapshot, m.idle_timeout_minutes, m.joined_at, m.last_request_at,
			m.ended_at, m.ended_reason, m.paid_until, m.billed_until, m.created_at, m.updated_at
	`, idleTimeoutMinutes, membershipID, consumerUserID, service.AccountShareMembershipStatusActive).Scan(
		&membership.ID,
		&membership.ListingID,
		&membership.AccountID,
		&membership.OwnerUserID,
		&membership.ConsumerUserID,
		&membership.APIKeyID,
		&membership.Status,
		&membership.HourlyRateSnapshot,
		&membership.HourlyFeeWaiverMinimumSnapshot,
		&membership.IdleTimeoutMinutes,
		&membership.JoinedAt,
		&lastRequestAt,
		&endedAt,
		&endedReason,
		&paidUntil,
		&billedUntil,
		&membership.CreatedAt,
		&membership.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, err
	}
	applyAccountShareMembershipNullableFields(membership, lastRequestAt, endedAt, endedReason, paidUntil, billedUntil)
	return membership, nil
}

func (r *accountShareModeRepository) TouchMembershipLastRequest(ctx context.Context, membershipID int64, at time.Time) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE account_share_memberships
		SET last_request_at = $1,
			updated_at = NOW()
		WHERE id = $2
			AND status = $3
			AND deleted_at IS NULL
	`, at.UTC(), membershipID, service.AccountShareMembershipStatusActive)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAccountShareListingNotFound
	}
	return nil
}

func (r *accountShareModeRepository) ListIdleMembershipCandidates(ctx context.Context, now time.Time, filter service.AccountShareIdleMembershipFilter, limit int) ([]service.AccountShareIdleMembershipCandidate, error) {
	if limit <= 0 {
		limit = service.AccountShareModeSeatBillingBatchSize
	}
	args := []any{service.AccountShareMembershipStatusActive, now.UTC()}
	where := []string{
		"status = $1",
		"deleted_at IS NULL",
		"idle_timeout_minutes > 0",
		"COALESCE(last_request_at, joined_at) + (idle_timeout_minutes * INTERVAL '1 minute') <= $2",
	}
	next := 3
	if filter.ConsumerUserID > 0 {
		where = append(where, fmt.Sprintf("consumer_user_id = $%d", next))
		args = append(args, filter.ConsumerUserID)
		next++
	}
	if filter.APIKeyID > 0 {
		where = append(where, fmt.Sprintf("api_key_id = $%d", next))
		args = append(args, filter.APIKeyID)
		next++
	}
	if filter.ListingID > 0 {
		where = append(where, fmt.Sprintf("listing_id = $%d", next))
		args = append(args, filter.ListingID)
		next++
	}
	args = append(args, limit)
	query := fmt.Sprintf(`
		SELECT id,
			COALESCE(last_request_at, joined_at) + (idle_timeout_minutes * INTERVAL '1 minute') AS idle_deadline
		FROM account_share_memberships
		WHERE %s
		ORDER BY idle_deadline ASC, id ASC
		LIMIT $%d
	`, strings.Join(where, " AND "), next)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	candidates := make([]service.AccountShareIdleMembershipCandidate, 0, limit)
	for rows.Next() {
		var candidate service.AccountShareIdleMembershipCandidate
		if err := rows.Scan(&candidate.MembershipID, &candidate.Deadline); err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return candidates, nil
}

func (r *accountShareModeRepository) EndIdleMembership(ctx context.Context, membershipID int64, endedAt time.Time) (*service.AccountShareMembership, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	membership, err := r.lockSeatBillingMembershipInTx(ctx, tx, membershipID, 0)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, err
	}
	deadline, ok := accountShareMembershipIdleDeadline(membership)
	if !ok || deadline.After(endedAt.UTC()) {
		return nil, service.ErrAccountShareListingNotFound
	}
	settledUntil, _, _, err := r.settleSeatChargeInTx(ctx, tx, membership, deadline)
	if err != nil {
		return nil, err
	}
	if err := r.refundUnusedSeatPrepayInTx(ctx, tx, membership, deadline); err != nil {
		return nil, err
	}
	if settledUntil == nil {
		settledUntil = &deadline
	}
	var endedAtNull, paidUntilNull, billedUntilNull sql.NullTime
	var endedReasonNull sql.NullString
	err = tx.QueryRowContext(ctx, `
		UPDATE account_share_memberships
		SET status = $1,
			ended_at = $2,
			ended_reason = $3,
			paid_until = $4,
			billed_until = $4,
			updated_at = NOW()
		WHERE id = $5
			AND status = $6
			AND deleted_at IS NULL
		RETURNING status, ended_at, ended_reason, paid_until, billed_until, updated_at
	`,
		service.AccountShareMembershipStatusEnded,
		deadline,
		service.AccountShareMembershipEndReasonIdleTimeout,
		*settledUntil,
		membership.ID,
		service.AccountShareMembershipStatusActive,
	).Scan(&membership.Status, &endedAtNull, &endedReasonNull, &paidUntilNull, &billedUntilNull, &membership.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, err
	}
	applyAccountShareMembershipNullableFields(membership, sql.NullTime{}, endedAtNull, endedReasonNull, paidUntilNull, billedUntilNull)
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return membership, nil
}

func (r *accountShareModeRepository) ProcessUnavailableMemberships(ctx context.Context, now time.Time, limit int) (*service.AccountShareSeatBillingResult, error) {
	if limit <= 0 {
		limit = service.AccountShareModeSeatBillingBatchSize
	}
	now = now.UTC()
	query := fmt.Sprintf(`
		SELECT m.id
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
			AND l.deleted_at IS NULL
		LEFT JOIN accounts a ON a.id = m.account_id
		WHERE m.status = $1
			AND m.deleted_at IS NULL
			AND %s
		ORDER BY m.joined_at ASC, m.id ASC
		LIMIT $3
	`, accountShareAccountUnavailableOrMissingConditionSQL("$2"))
	rows, err := r.db.QueryContext(ctx, query, service.AccountShareMembershipStatusActive, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := &service.AccountShareSeatBillingResult{Processed: len(ids)}
	return r.processUnavailableMembershipIDs(ctx, ids, result, now)
}

func (r *accountShareModeRepository) EndUnavailableAccountMemberships(ctx context.Context, accountID int64, endedAt time.Time, limit int) (*service.AccountShareSeatBillingResult, error) {
	if accountID <= 0 {
		return &service.AccountShareSeatBillingResult{}, nil
	}
	if limit <= 0 {
		limit = service.AccountShareModeSeatBillingBatchSize
	}
	endedAt = endedAt.UTC()
	query := fmt.Sprintf(`
		SELECT m.id
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
			AND l.deleted_at IS NULL
		LEFT JOIN accounts a ON a.id = m.account_id
		WHERE m.status = $1
			AND m.account_id = $2
			AND m.deleted_at IS NULL
			AND %s
		ORDER BY m.joined_at ASC, m.id ASC
		LIMIT $4
	`, accountShareAccountUnavailableOrMissingConditionSQL("$3"))
	rows, err := r.db.QueryContext(ctx, query, service.AccountShareMembershipStatusActive, accountID, endedAt, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := &service.AccountShareSeatBillingResult{Processed: len(ids)}
	return r.processUnavailableMembershipIDs(ctx, ids, result, endedAt)
}

func (r *accountShareModeRepository) DisablePermanentlyUnavailableListings(ctx context.Context, now time.Time, limit int) (*service.AccountShareListingMaintenanceResult, error) {
	if limit <= 0 {
		limit = service.AccountShareModeSeatBillingBatchSize
	}
	now = now.UTC()
	query := fmt.Sprintf(`
		WITH candidates AS (
			SELECT l.id
			FROM account_share_listings l
			LEFT JOIN accounts a ON a.id = l.account_id
			WHERE l.status = $1
				AND l.deleted_at IS NULL
				AND %s
			ORDER BY l.updated_at ASC, l.id ASC
			LIMIT $3
		)
		UPDATE account_share_listings l
		SET status = $2,
			edit_session_id = NULL,
			editing_by_user_id = NULL,
			editing_started_at = NULL,
			editing_expires_at = NULL,
			updated_at = NOW()
		FROM candidates c
		WHERE l.id = c.id
		RETURNING l.id
	`, accountShareAccountPermanentlyUnavailableConditionSQL("$4"))
	rows, err := r.db.QueryContext(ctx, query, service.AccountShareListingStatusActive, service.AccountShareListingStatusDisabled, limit, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	processed := 0
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		processed++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if processed > 0 {
		logger.LegacyPrintf("repository.account_share_mode", "disabled permanently unavailable account share listings: count=%d", processed)
	}
	return &service.AccountShareListingMaintenanceResult{Processed: processed}, nil
}

func (r *accountShareModeRepository) processUnavailableMembershipIDs(ctx context.Context, ids []int64, result *service.AccountShareSeatBillingResult, endedAt time.Time) (*service.AccountShareSeatBillingResult, error) {
	if result == nil {
		result = &service.AccountShareSeatBillingResult{}
	}
	for _, id := range ids {
		item, err := r.endUnavailableMembership(ctx, id, endedAt)
		if err != nil {
			return result, err
		}
		if item == nil {
			continue
		}
		result.DebitUserIDs = append(result.DebitUserIDs, item.DebitUserIDs...)
		result.CreditUserIDs = append(result.CreditUserIDs, item.CreditUserIDs...)
		result.EndedConsumerUserIDs = append(result.EndedConsumerUserIDs, item.EndedConsumerUserIDs...)
	}
	return result, nil
}

func (r *accountShareModeRepository) endUnavailableMembership(ctx context.Context, membershipID int64, endedAt time.Time) (*service.AccountShareSeatBillingResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	membership, err := r.lockSeatBillingMembershipInTx(ctx, tx, membershipID, 0)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	unavailable, err := r.accountShareAccountUnavailableInTx(ctx, tx, membership.AccountID, endedAt)
	if err != nil {
		return nil, err
	}
	if !unavailable {
		return nil, nil
	}
	result, err := r.endSeatBillingMembershipInTx(ctx, tx, membership, endedAt, service.AccountShareMembershipEndReasonUnavailable)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func (r *accountShareModeRepository) ProcessSeatBilling(ctx context.Context, now time.Time, limit int) (*service.AccountShareSeatBillingResult, error) {
	if limit <= 0 {
		limit = service.AccountShareModeSeatBillingBatchSize
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id
		FROM account_share_memberships
		WHERE status = $1
			AND deleted_at IS NULL
			AND hourly_rate_snapshot > 0
			AND paid_until IS NOT NULL
			AND paid_until <= $2
			AND (idle_timeout_minutes <= 0 OR COALESCE(last_request_at, joined_at) + (idle_timeout_minutes * INTERVAL '1 minute') > $2)
		ORDER BY paid_until ASC, id ASC
		LIMIT $3
	`, service.AccountShareMembershipStatusActive, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := &service.AccountShareSeatBillingResult{Processed: len(ids)}
	return r.processSeatBillingIDs(ctx, ids, result, now)
}

func (r *accountShareModeRepository) ProcessSeatBillingForJoin(ctx context.Context, now time.Time, consumerUserID, apiKeyID, listingID int64) (*service.AccountShareSeatBillingResult, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id
		FROM account_share_memberships
		WHERE status = $1
			AND deleted_at IS NULL
			AND hourly_rate_snapshot > 0
			AND paid_until IS NOT NULL
			AND paid_until <= $2
			AND (idle_timeout_minutes <= 0 OR COALESCE(last_request_at, joined_at) + (idle_timeout_minutes * INTERVAL '1 minute') > $2)
			AND (
				consumer_user_id = $3
				OR api_key_id = $4
				OR listing_id = $5
			)
		ORDER BY paid_until ASC, id ASC
		LIMIT $6
	`, service.AccountShareMembershipStatusActive, now, consumerUserID, apiKeyID, listingID, service.AccountShareModeSeatBillingBatchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]int64, 0, service.AccountShareModeSeatBillingBatchSize)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := &service.AccountShareSeatBillingResult{Processed: len(ids)}
	return r.processSeatBillingIDs(ctx, ids, result, now)
}

func (r *accountShareModeRepository) ProcessSeatBillingForRequest(ctx context.Context, now time.Time, consumerUserID, apiKeyID int64) (*service.AccountShareSeatBillingResult, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id
		FROM account_share_memberships
		WHERE status = $1
			AND deleted_at IS NULL
			AND hourly_rate_snapshot > 0
			AND paid_until IS NOT NULL
			AND paid_until <= $2
			AND (idle_timeout_minutes <= 0 OR COALESCE(last_request_at, joined_at) + (idle_timeout_minutes * INTERVAL '1 minute') > $2)
			AND consumer_user_id = $3
			AND api_key_id = $4
		ORDER BY paid_until ASC, id ASC
		LIMIT $5
	`, service.AccountShareMembershipStatusActive, now, consumerUserID, apiKeyID, service.AccountShareModeSeatBillingBatchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]int64, 0, service.AccountShareModeSeatBillingBatchSize)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := &service.AccountShareSeatBillingResult{Processed: len(ids)}
	return r.processSeatBillingIDs(ctx, ids, result, now)
}

func (r *accountShareModeRepository) processSeatBillingIDs(ctx context.Context, ids []int64, result *service.AccountShareSeatBillingResult, now time.Time) (*service.AccountShareSeatBillingResult, error) {
	if result == nil {
		result = &service.AccountShareSeatBillingResult{}
	}
	for _, id := range ids {
		item, err := r.processSeatBillingMembership(ctx, id, now)
		if err != nil {
			return result, err
		}
		if item == nil {
			continue
		}
		result.DebitUserIDs = append(result.DebitUserIDs, item.DebitUserIDs...)
		result.CreditUserIDs = append(result.CreditUserIDs, item.CreditUserIDs...)
		result.EndedConsumerUserIDs = append(result.EndedConsumerUserIDs, item.EndedConsumerUserIDs...)
	}
	return result, nil
}

func (r *accountShareModeRepository) processSeatBillingMembership(ctx context.Context, membershipID int64, now time.Time) (*service.AccountShareSeatBillingResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	membership, err := r.lockSeatBillingMembershipInTx(ctx, tx, membershipID, 0)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if membership.Status != service.AccountShareMembershipStatusActive || membership.PaidUntil == nil || membership.HourlyRateSnapshot <= 0 || membership.PaidUntil.After(now) {
		return nil, nil
	}
	unavailable, err := r.accountShareAccountUnavailableInTx(ctx, tx, membership.AccountID, now)
	if err != nil {
		return nil, err
	}
	if unavailable {
		result, err := r.endSeatBillingMembershipInTx(ctx, tx, membership, now, service.AccountShareMembershipEndReasonUnavailable)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		tx = nil
		return result, nil
	}

	settledUntil, settlementID, creditUserIDs, err := r.settleSeatChargeInTx(ctx, tx, membership, *membership.PaidUntil)
	if err != nil {
		return nil, err
	}
	if settledUntil == nil {
		settledUntil = membership.PaidUntil
	}

	nextDuration := service.AccountShareModeSeatPrepayDuration
	prepayAmount := accountShareSeatCharge(membership.HourlyRateSnapshot, nextDuration)
	var userBalance float64
	if err := tx.QueryRowContext(ctx, `
		SELECT balance
		FROM users
		WHERE id = $1
			AND deleted_at IS NULL
		FOR UPDATE
	`, membership.ConsumerUserID).Scan(&userBalance); errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUserNotFound
	} else if err != nil {
		return nil, err
	}

	result := &service.AccountShareSeatBillingResult{CreditUserIDs: creditUserIDs}
	if prepayAmount <= 0 || userBalance < prepayAmount {
		err = tx.QueryRowContext(ctx, `
			UPDATE account_share_memberships
			SET status = $1,
				ended_at = $2,
				ended_reason = $3,
				billed_until = $2,
				paid_until = $2,
				updated_at = NOW()
			WHERE id = $4
			RETURNING updated_at
		`, service.AccountShareMembershipStatusEnded, *settledUntil, service.AccountShareMembershipEndReasonPrepay, membership.ID).Scan(&membership.UpdatedAt)
		if err != nil {
			return nil, err
		}
		result.EndedConsumerUserIDs = append(result.EndedConsumerUserIDs, membership.ConsumerUserID)
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		tx = nil
		return result, nil
	}

	newPaidUntil := membership.PaidUntil.Add(nextDuration)
	newBalance := userBalance - prepayAmount
	if _, err := tx.ExecContext(ctx, `
		UPDATE users
		SET balance = $1::numeric,
			updated_at = NOW()
		WHERE id = $2
			AND deleted_at IS NULL
	`, decimalFromSignedFloat(newBalance).StringFixed(10), membership.ConsumerUserID); err != nil {
		return nil, err
	}
	if err := insertUserBalanceLedger(ctx, tx, userBalanceLedgerInput{
		UserID:       membership.ConsumerUserID,
		Direction:    "debit",
		Amount:       decimalFromFloat(prepayAmount),
		Reason:       accountShareSeatPrepayReason,
		RefType:      accountShareModeSettlementRefType,
		RefID:        nullablePositiveInt64(settlementID),
		BalanceAfter: decimalFromSignedFloat(newBalance),
		Metadata: map[string]any{
			"listing_id":    membership.ListingID,
			"account_id":    membership.AccountID,
			"hourly_rate":   membership.HourlyRateSnapshot,
			"membership_id": membership.ID,
			"settlement_id": settlementID,
			"duration_ms":   int(nextDuration.Milliseconds()),
			"paid_until":    newPaidUntil.Format(time.RFC3339),
			"prepay_stage":  "renew",
			"seat_billing":  true,
		},
	}); err != nil {
		return nil, err
	}
	err = tx.QueryRowContext(ctx, `
		UPDATE account_share_memberships
		SET paid_until = $1,
			billed_until = $2,
			updated_at = NOW()
		WHERE id = $3
		RETURNING updated_at
	`, newPaidUntil, *settledUntil, membership.ID).Scan(&membership.UpdatedAt)
	if err != nil {
		return nil, err
	}
	result.DebitUserIDs = append(result.DebitUserIDs, membership.ConsumerUserID)
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func (r *accountShareModeRepository) endSeatBillingMembershipInTx(ctx context.Context, tx *sql.Tx, membership *service.AccountShareMembership, endedAt time.Time, reason string) (*service.AccountShareSeatBillingResult, error) {
	if membership == nil || membership.ID <= 0 {
		return nil, nil
	}
	endedAt = endedAt.UTC()
	settledUntil, _, creditUserIDs, err := r.settleSeatChargeInTx(ctx, tx, membership, endedAt)
	if err != nil {
		return nil, err
	}
	if err := r.refundUnusedSeatPrepayInTx(ctx, tx, membership, endedAt); err != nil {
		return nil, err
	}
	if settledUntil == nil {
		settledUntil = &endedAt
	}
	var endedAtNull, paidUntilNull, billedUntilNull sql.NullTime
	var endedReasonNull sql.NullString
	err = tx.QueryRowContext(ctx, `
		UPDATE account_share_memberships
		SET status = $1,
			ended_at = $2,
			ended_reason = $3,
			paid_until = $4,
			billed_until = $4,
			updated_at = NOW()
		WHERE id = $5
			AND status = $6
			AND deleted_at IS NULL
		RETURNING status, ended_at, ended_reason, paid_until, billed_until, updated_at
	`,
		service.AccountShareMembershipStatusEnded,
		endedAt,
		reason,
		*settledUntil,
		membership.ID,
		service.AccountShareMembershipStatusActive,
	).Scan(&membership.Status, &endedAtNull, &endedReasonNull, &paidUntilNull, &billedUntilNull, &membership.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	applyAccountShareMembershipNullableFields(membership, sql.NullTime{}, endedAtNull, endedReasonNull, paidUntilNull, billedUntilNull)
	return &service.AccountShareSeatBillingResult{
		DebitUserIDs:         []int64{membership.ConsumerUserID},
		CreditUserIDs:        creditUserIDs,
		EndedConsumerUserIDs: []int64{membership.ConsumerUserID},
	}, nil
}

func (r *accountShareModeRepository) accountShareAccountUnavailableInTx(ctx context.Context, tx *sql.Tx, accountID int64, now time.Time) (bool, error) {
	if accountID <= 0 {
		return false, nil
	}
	query := fmt.Sprintf(`
		SELECT EXISTS (
			SELECT 1
			FROM accounts a
			WHERE a.id = $1
				AND (
					a.deleted_at IS NOT NULL
					OR %s
				)
		) OR NOT EXISTS (
			SELECT 1
			FROM accounts a
			WHERE a.id = $1
		)
	`, accountShareAccountUnavailableConditionSQL("$2"))
	var unavailable bool
	if err := tx.QueryRowContext(ctx, query, accountID, now.UTC()).Scan(&unavailable); err != nil {
		return false, err
	}
	if unavailable {
		logger.LegacyPrintf("repository.account_share_mode", "account share unavailable matched: account_id=%d now=%s details=%s", accountID, now.UTC().Format(time.RFC3339), r.accountShareAccountUnavailableDetailsInTx(ctx, tx, accountID, now))
	}
	return unavailable, nil
}

func (r *accountShareModeRepository) accountShareAccountUnavailableDetailsInTx(ctx context.Context, tx *sql.Tx, accountID int64, now time.Time) string {
	query := fmt.Sprintf(`
		SELECT
			a.status,
			a.schedulable,
			(a.auto_pause_on_expired = TRUE AND a.expires_at IS NOT NULL AND a.expires_at <= $2) AS expired,
			(a.overload_until IS NOT NULL AND a.overload_until > $2) AS overload,
			(a.rate_limit_reset_at IS NOT NULL AND a.rate_limit_reset_at > $2) AS rate_limited,
			(a.temp_unschedulable_until IS NOT NULL AND a.temp_unschedulable_until > $2) AS temp_unschedulable,
			%s AS codex_5h_protected,
			%s AS codex_7d_protected,
			COALESCE(a.extra->>'codex_5h_used_percent', '') AS codex_5h_used_percent,
			COALESCE(a.extra->>'codex_7d_used_percent', '') AS codex_7d_used_percent,
			COALESCE(a.extra->>'codex_5h_limit_percent', '') AS codex_5h_limit_percent,
			COALESCE(a.extra->>'codex_7d_limit_percent', '') AS codex_7d_limit_percent,
			COALESCE(a.extra->>'codex_5h_reset_at', '') AS codex_5h_reset_at,
			COALESCE(a.extra->>'codex_7d_reset_at', '') AS codex_7d_reset_at
		FROM accounts a
		WHERE a.id = $1
	`, accountShareCodexQuotaProtectedSQL("codex_5h_used_percent", "codex_5h_reset_at", "codex_5h_limit_percent", "$2"),
		accountShareCodexQuotaProtectedSQL("codex_7d_used_percent", "codex_7d_reset_at", "codex_7d_limit_percent", "$2"))
	var status, used5h, used7d, limit5h, limit7d, reset5h, reset7d string
	var schedulable, expired, overload, rateLimited, tempUnschedulable, codex5hProtected, codex7dProtected bool
	if err := tx.QueryRowContext(ctx, query, accountID, now.UTC()).Scan(
		&status,
		&schedulable,
		&expired,
		&overload,
		&rateLimited,
		&tempUnschedulable,
		&codex5hProtected,
		&codex7dProtected,
		&used5h,
		&used7d,
		&limit5h,
		&limit7d,
		&reset5h,
		&reset7d,
	); err != nil {
		return fmt.Sprintf("detail_query_error=%v", err)
	}
	return fmt.Sprintf("status=%s schedulable=%t expired=%t overload=%t rate_limited=%t temp_unschedulable=%t codex_5h_protected=%t codex_7d_protected=%t codex_5h_used=%s codex_7d_used=%s codex_5h_limit=%s codex_7d_limit=%s codex_5h_reset_at=%s codex_7d_reset_at=%s",
		status,
		schedulable,
		expired,
		overload,
		rateLimited,
		tempUnschedulable,
		codex5hProtected,
		codex7dProtected,
		used5h,
		used7d,
		limit5h,
		limit7d,
		reset5h,
		reset7d,
	)
}

func (r *accountShareModeRepository) lockSeatBillingMembershipInTx(ctx context.Context, tx *sql.Tx, membershipID int64, consumerUserID int64) (*service.AccountShareMembership, error) {
	query := `
		SELECT
			m.id, m.listing_id, m.account_id, l.owner_user_id, m.consumer_user_id, m.api_key_id,
			m.status, m.hourly_rate_snapshot, m.hourly_fee_waiver_minimum_snapshot, m.idle_timeout_minutes, m.joined_at, m.last_request_at,
			m.ended_at, m.ended_reason, m.paid_until, m.billed_until, m.created_at, m.updated_at
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
		WHERE m.id = $1
			AND m.status = $2
			AND m.deleted_at IS NULL
	`
	args := []any{membershipID, service.AccountShareMembershipStatusActive}
	if consumerUserID > 0 {
		query += " AND m.consumer_user_id = $3"
		args = append(args, consumerUserID)
	}
	query += " FOR UPDATE OF m"

	membership := &service.AccountShareMembership{}
	var endedAt, lastRequestAt, paidUntil, billedUntil sql.NullTime
	var endedReason sql.NullString
	err := tx.QueryRowContext(ctx, query, args...).Scan(
		&membership.ID,
		&membership.ListingID,
		&membership.AccountID,
		&membership.OwnerUserID,
		&membership.ConsumerUserID,
		&membership.APIKeyID,
		&membership.Status,
		&membership.HourlyRateSnapshot,
		&membership.HourlyFeeWaiverMinimumSnapshot,
		&membership.IdleTimeoutMinutes,
		&membership.JoinedAt,
		&lastRequestAt,
		&endedAt,
		&endedReason,
		&paidUntil,
		&billedUntil,
		&membership.CreatedAt,
		&membership.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if endedAt.Valid {
		membership.EndedAt = &endedAt.Time
	}
	if lastRequestAt.Valid {
		membership.LastRequestAt = &lastRequestAt.Time
	}
	if endedReason.Valid {
		membership.EndedReason = endedReason.String
	}
	if paidUntil.Valid {
		membership.PaidUntil = &paidUntil.Time
	}
	if billedUntil.Valid {
		membership.BilledUntil = &billedUntil.Time
	}
	return membership, nil
}

func (r *accountShareModeRepository) settleSeatChargeInTx(ctx context.Context, tx *sql.Tx, membership *service.AccountShareMembership, at time.Time) (*time.Time, int64, []int64, error) {
	if membership == nil || membership.HourlyRateSnapshot <= 0 || membership.PaidUntil == nil {
		return nil, 0, nil, nil
	}
	start := membership.JoinedAt
	if membership.BilledUntil != nil {
		start = *membership.BilledUntil
	}
	end := at.UTC()
	if membership.PaidUntil.Before(end) {
		end = *membership.PaidUntil
	}
	if !end.After(start) {
		return &start, 0, nil, nil
	}
	duration := end.Sub(start)
	charge := accountShareSeatCharge(membership.HourlyRateSnapshot, duration)
	if charge <= 0 {
		return &end, 0, nil, nil
	}
	waiver, err := r.resolveSeatChargeWaiverInTx(ctx, tx, membership, start, end, charge)
	if err != nil {
		return nil, 0, nil, err
	}
	if waiver.Eligible {
		settlementID, err := r.refundSeatChargeWaiverInTx(ctx, tx, membership, start, end, charge, waiver)
		if err != nil {
			return nil, 0, nil, err
		}
		return &end, settlementID, []int64{membership.ConsumerUserID}, nil
	}
	policy, err := r.resolveAccountShareModePolicyInTx(ctx, tx, service.PlatformOpenAI)
	if err != nil {
		return nil, 0, nil, err
	}
	ownerRatio, platformRatio := accountShareModeSettlementRatios(policy.OwnerShareRatio, policy.PlatformShareRatio)
	totalCharge := decimalFromFloat(charge)
	ownerCredit := totalCharge.Mul(ownerRatio).Round(10)
	if ownerCredit.GreaterThan(totalCharge) {
		ownerCredit = totalCharge
	}
	platformCredit := totalCharge.Mul(platformRatio).Round(10)
	settlementID, err := r.insertSeatSettlementInTx(ctx, tx, membership, accountShareSeatSettlementTypeCharge, start, end, charge, 0, ownerCredit, platformCredit)
	if err != nil {
		return nil, 0, nil, err
	}
	creditUserIDs := make([]int64, 0, 1)
	if ownerCredit.GreaterThan(decimal.Zero) {
		newBalance, err := creditUsageBillingBalance(ctx, tx, membership.OwnerUserID, ownerCredit)
		if err != nil {
			return nil, 0, nil, err
		}
		if err := insertUserBalanceLedger(ctx, tx, userBalanceLedgerInput{
			UserID:       membership.OwnerUserID,
			Direction:    "credit",
			Amount:       ownerCredit,
			Reason:       accountShareSeatIncomeReason,
			RefType:      accountShareModeSettlementRefType,
			RefID:        nullablePositiveInt64(settlementID),
			BalanceAfter: decimalFromSignedFloat(newBalance),
			Metadata: map[string]any{
				"listing_id":       membership.ListingID,
				"account_id":       membership.AccountID,
				"membership_id":    membership.ID,
				"settlement_id":    settlementID,
				"consumer_user_id": membership.ConsumerUserID,
				"total_charge":     totalCharge.StringFixed(10),
				"owner_ratio":      ownerRatio.StringFixed(8),
				"settlement_type":  accountShareSeatSettlementTypeCharge,
				"period_started":   start.Format(time.RFC3339),
				"period_ended":     end.Format(time.RFC3339),
			},
		}); err != nil {
			return nil, 0, nil, err
		}
		creditUserIDs = append(creditUserIDs, membership.OwnerUserID)
	}
	return &end, settlementID, creditUserIDs, nil
}

type accountShareSeatChargeWaiver struct {
	Eligible bool
	Minimum  decimal.Decimal
	Required decimal.Decimal
	Usage    decimal.Decimal
}

func (r *accountShareModeRepository) resolveSeatChargeWaiverInTx(ctx context.Context, tx *sql.Tx, membership *service.AccountShareMembership, periodStart, periodEnd time.Time, charge float64) (accountShareSeatChargeWaiver, error) {
	waiver := accountShareSeatChargeWaiver{}
	if membership == nil || membership.HourlyFeeWaiverMinimumSnapshot <= 0 || charge <= 0 || !periodEnd.After(periodStart) {
		return waiver, nil
	}
	minimum := decimalFromFloat(membership.HourlyFeeWaiverMinimumSnapshot)
	if minimum.LessThanOrEqual(decimal.Zero) {
		return waiver, nil
	}
	durationMs := periodEnd.Sub(periodStart).Milliseconds()
	if durationMs <= 0 {
		return waiver, nil
	}
	required := minimum.Mul(decimal.NewFromInt(durationMs)).Div(decimal.NewFromInt(3600000)).Round(10)
	if required.LessThanOrEqual(decimal.Zero) {
		return waiver, nil
	}
	usage, err := r.accountShareModeUsageChargeInTx(ctx, tx, membership.ID, periodStart, periodEnd)
	if err != nil {
		return waiver, err
	}
	waiver.Minimum = minimum
	waiver.Required = required
	waiver.Usage = usage
	waiver.Eligible = usage.GreaterThanOrEqual(required)
	return waiver, nil
}

func (r *accountShareModeRepository) accountShareModeUsageChargeInTx(ctx context.Context, tx *sql.Tx, membershipID int64, periodStart, periodEnd time.Time) (decimal.Decimal, error) {
	if membershipID <= 0 || !periodEnd.After(periodStart) {
		return decimal.Zero, nil
	}
	var totalRaw string
	err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(e.total_charge), 0)::text
		FROM account_share_mode_settlement_entries e
		LEFT JOIN usage_logs ul ON ul.id = e.usage_log_id
		WHERE e.membership_id = $1
			AND e.settlement_type = 'usage_request'
			AND COALESCE(ul.created_at, e.created_at) >= $2
			AND COALESCE(ul.created_at, e.created_at) < $3
	`, membershipID, periodStart, periodEnd).Scan(&totalRaw)
	if err != nil {
		return decimal.Zero, err
	}
	total, err := decimal.NewFromString(strings.TrimSpace(totalRaw))
	if err != nil {
		return decimal.Zero, err
	}
	if total.IsNegative() {
		return decimal.Zero, nil
	}
	return total, nil
}

func (r *accountShareModeRepository) refundSeatChargeWaiverInTx(ctx context.Context, tx *sql.Tx, membership *service.AccountShareMembership, periodStart, periodEnd time.Time, charge float64, waiver accountShareSeatChargeWaiver) (int64, error) {
	if membership == nil || charge <= 0 || !periodEnd.After(periodStart) {
		return 0, nil
	}
	refund := decimalFromFloat(charge)
	if refund.LessThanOrEqual(decimal.Zero) {
		return 0, nil
	}
	settlementID, err := r.insertSeatWaiverSettlementInTx(ctx, tx, membership, periodStart, periodEnd, refund, waiver)
	if err != nil {
		return 0, err
	}
	newBalance, err := creditUsageBillingBalance(ctx, tx, membership.ConsumerUserID, refund)
	if err != nil {
		return 0, err
	}
	if err := insertUserBalanceLedger(ctx, tx, userBalanceLedgerInput{
		UserID:       membership.ConsumerUserID,
		Direction:    "credit",
		Amount:       refund,
		Reason:       accountShareSeatWaiverRefundReason,
		RefType:      accountShareModeSettlementRefType,
		RefID:        nullablePositiveInt64(settlementID),
		BalanceAfter: decimalFromSignedFloat(newBalance),
		Metadata: map[string]any{
			"listing_id":      membership.ListingID,
			"account_id":      membership.AccountID,
			"membership_id":   membership.ID,
			"settlement_id":   settlementID,
			"hourly_rate":     membership.HourlyRateSnapshot,
			"duration_ms":     int(periodEnd.Sub(periodStart).Milliseconds()),
			"period_started":  periodStart.Format(time.RFC3339),
			"period_ended":    periodEnd.Format(time.RFC3339),
			"refund_amount":   refund.StringFixed(10),
			"waiver_minimum":  waiver.Minimum.StringFixed(8),
			"waiver_required": waiver.Required.StringFixed(10),
			"waiver_usage":    waiver.Usage.StringFixed(10),
			"settlement_type": accountShareSeatSettlementTypeWaiverRefund,
		},
	}); err != nil {
		return 0, err
	}
	return settlementID, nil
}

func (r *accountShareModeRepository) refundUnusedSeatPrepayInTx(ctx context.Context, tx *sql.Tx, membership *service.AccountShareMembership, endedAt time.Time) error {
	if membership == nil || membership.HourlyRateSnapshot <= 0 || membership.PaidUntil == nil || !membership.PaidUntil.After(endedAt) {
		return nil
	}
	duration := membership.PaidUntil.Sub(endedAt)
	refund := accountShareSeatCharge(membership.HourlyRateSnapshot, duration)
	if refund <= 0 {
		return nil
	}
	newBalance, err := creditUsageBillingBalance(ctx, tx, membership.ConsumerUserID, decimalFromFloat(refund))
	if err != nil {
		return err
	}
	if err := insertUserBalanceLedger(ctx, tx, userBalanceLedgerInput{
		UserID:       membership.ConsumerUserID,
		Direction:    "credit",
		Amount:       decimalFromFloat(refund),
		Reason:       accountShareSeatRefundReason,
		RefType:      "account_share_membership",
		RefID:        membership.ID,
		BalanceAfter: decimalFromSignedFloat(newBalance),
		Metadata: map[string]any{
			"listing_id":      membership.ListingID,
			"account_id":      membership.AccountID,
			"hourly_rate":     membership.HourlyRateSnapshot,
			"duration_ms":     int(duration.Milliseconds()),
			"refund_until":    membership.PaidUntil.Format(time.RFC3339),
			"settlement_type": accountShareSeatSettlementTypeRefund,
			"seat_billing":    true,
		},
	}); err != nil {
		return err
	}
	_, err = r.insertSeatSettlementInTx(ctx, tx, membership, accountShareSeatSettlementTypeRefund, endedAt, *membership.PaidUntil, 0, refund, decimal.Zero, decimal.Zero)
	return err
}

func (r *accountShareModeRepository) insertSeatSettlementInTx(ctx context.Context, tx *sql.Tx, membership *service.AccountShareMembership, settlementType string, periodStart, periodEnd time.Time, charge float64, refund float64, ownerCredit, platformCredit decimal.Decimal) (int64, error) {
	if membership == nil {
		return 0, nil
	}
	durationMs := int(periodEnd.Sub(periodStart).Milliseconds())
	if durationMs < 0 {
		durationMs = 0
	}
	var settlementID int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO account_share_mode_settlement_entries (
			usage_log_id,
			membership_id,
			listing_id,
			account_id,
			owner_user_id,
			consumer_user_id,
			api_key_id,
			base_charge,
			hourly_charge,
			total_charge,
			owner_credit,
			platform_credit,
			rate_multiplier_snapshot,
			hourly_rate_snapshot,
			owner_share_ratio_snapshot,
			platform_share_ratio_snapshot,
			duration_ms,
			settlement_type,
			period_started_at,
			period_ended_at,
			refund_amount,
			created_at
		)
		VALUES (
			NULL, $1, $2, $3, $4, $5, $6,
			0, $7::numeric, $7::numeric, $8::numeric, $9::numeric,
			1, $10::numeric, $11::numeric, $12::numeric, $13,
			$14, $15, $16, $17::numeric, NOW()
		)
		RETURNING id
	`,
		membership.ID,
		membership.ListingID,
		membership.AccountID,
		membership.OwnerUserID,
		membership.ConsumerUserID,
		membership.APIKeyID,
		decimalFromFloat(charge).StringFixed(10),
		ownerCredit.StringFixed(10),
		platformCredit.StringFixed(10),
		decimalFromFloat(membership.HourlyRateSnapshot).StringFixed(8),
		ratioFromCredits(ownerCredit, decimalFromFloat(charge)).StringFixed(8),
		ratioFromCredits(platformCredit, decimalFromFloat(charge)).StringFixed(8),
		durationMs,
		settlementType,
		periodStart,
		periodEnd,
		decimalFromFloat(refund).StringFixed(10),
	).Scan(&settlementID)
	return settlementID, err
}

func (r *accountShareModeRepository) insertSeatWaiverSettlementInTx(ctx context.Context, tx *sql.Tx, membership *service.AccountShareMembership, periodStart, periodEnd time.Time, refund decimal.Decimal, waiver accountShareSeatChargeWaiver) (int64, error) {
	if membership == nil || refund.LessThanOrEqual(decimal.Zero) {
		return 0, nil
	}
	durationMs := int(periodEnd.Sub(periodStart).Milliseconds())
	if durationMs < 0 {
		durationMs = 0
	}
	var settlementID int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO account_share_mode_settlement_entries (
			usage_log_id,
			membership_id,
			listing_id,
			account_id,
			owner_user_id,
			consumer_user_id,
			api_key_id,
			base_charge,
			hourly_charge,
			total_charge,
			owner_credit,
			platform_credit,
			rate_multiplier_snapshot,
			hourly_rate_snapshot,
			owner_share_ratio_snapshot,
			platform_share_ratio_snapshot,
			duration_ms,
			settlement_type,
			period_started_at,
			period_ended_at,
			refund_amount,
			waiver_minimum_snapshot,
			waiver_required_amount,
			waiver_usage_amount,
			created_at
		)
		VALUES (
			NULL, $1, $2, $3, $4, $5, $6,
			0, 0, 0, 0, 0,
			1, $7::numeric, 0, 0, $8,
			$9, $10, $11, $12::numeric,
			$13::numeric, $14::numeric, $15::numeric,
			NOW()
		)
		RETURNING id
	`,
		membership.ID,
		membership.ListingID,
		membership.AccountID,
		membership.OwnerUserID,
		membership.ConsumerUserID,
		membership.APIKeyID,
		decimalFromFloat(membership.HourlyRateSnapshot).StringFixed(8),
		durationMs,
		accountShareSeatSettlementTypeWaiverRefund,
		periodStart,
		periodEnd,
		refund.StringFixed(10),
		waiver.Minimum.StringFixed(8),
		waiver.Required.StringFixed(10),
		waiver.Usage.StringFixed(10),
	).Scan(&settlementID)
	return settlementID, err
}

func (r *accountShareModeRepository) resolveAccountShareModePolicyInTx(ctx context.Context, tx *sql.Tx, platform string) (*service.AccountShareModePolicy, error) {
	policy := &service.AccountShareModePolicy{
		Platform:           platform,
		OwnerShareRatio:    service.AccountShareModeDefaultOwnerShareRatio,
		PlatformShareRatio: service.AccountShareModeDefaultPlatformShareRatio,
		Enabled:            true,
		Version:            1,
	}
	var enabled bool
	err := tx.QueryRowContext(ctx, `
		SELECT owner_share_ratio, platform_share_ratio, enabled, version
		FROM account_share_mode_policies
		WHERE platform = $1
	`, platform).Scan(&policy.OwnerShareRatio, &policy.PlatformShareRatio, &enabled, &policy.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return policy, nil
	}
	if err != nil {
		return nil, err
	}
	policy.Enabled = enabled
	if !enabled {
		policy.OwnerShareRatio = 0
		policy.PlatformShareRatio = 1
	}
	return policy, nil
}

func accountShareSeatCharge(hourlyRate float64, duration time.Duration) float64 {
	if hourlyRate <= 0 || duration <= 0 {
		return 0
	}
	return hourlyRate * float64(duration.Milliseconds()) / 3600000.0
}

func ratioFromCredits(part, total decimal.Decimal) decimal.Decimal {
	if total.LessThanOrEqual(decimal.Zero) || part.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero
	}
	return part.Div(total).Round(8)
}

func (r *accountShareModeRepository) GetActiveMembershipForAPIKey(ctx context.Context, apiKeyID int64) (*service.AccountShareMembership, *service.AccountShareListing, error) {
	return r.queryActiveMembership(ctx, `
		m.api_key_id = $1
	`, apiKeyID)
}

func (r *accountShareModeRepository) GetActiveMembershipForRequest(ctx context.Context, userID, apiKeyID, groupID int64) (*service.AccountShareMembership, *service.AccountShareListing, error) {
	// The active membership is the source of truth for account-share mode routing.
	// account_groups is scheduler metadata and can be rewritten by generic owned-account repair flows.
	_ = groupID
	return r.queryActiveMembership(ctx, `
		m.consumer_user_id = $1
		AND m.api_key_id = $2
	`, userID, apiKeyID)
}

func (r *accountShareModeRepository) ResolvePolicy(ctx context.Context, platform string) (*service.AccountShareModePolicy, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform == "" {
		platform = service.PlatformOpenAI
	}
	policy := &service.AccountShareModePolicy{}
	err := r.db.QueryRowContext(ctx, `
		SELECT id, platform, platform_share_ratio, owner_share_ratio, enabled, version
		FROM account_share_mode_policies
		WHERE platform = $1
			AND deleted_at IS NULL
	`, platform).Scan(
		&policy.ID,
		&policy.Platform,
		&policy.PlatformShareRatio,
		&policy.OwnerShareRatio,
		&policy.Enabled,
		&policy.Version,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return &service.AccountShareModePolicy{
			Platform:           platform,
			PlatformShareRatio: service.AccountShareModeDefaultPlatformShareRatio,
			OwnerShareRatio:    service.AccountShareModeDefaultOwnerShareRatio,
			Enabled:            true,
			Version:            1,
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return policy, nil
}

func (r *accountShareModeRepository) UpsertPolicy(ctx context.Context, input service.UpdateAccountShareModePolicyInput) (*service.AccountShareModePolicy, error) {
	platform := strings.ToLower(strings.TrimSpace(input.Platform))
	if platform == "" {
		platform = service.PlatformOpenAI
	}
	platformRatio := service.AccountShareModeDefaultPlatformShareRatio
	if input.PlatformShareRatio != nil {
		platformRatio = *input.PlatformShareRatio
	}
	ownerRatio := service.AccountShareModeDefaultOwnerShareRatio
	if input.OwnerShareRatio != nil {
		ownerRatio = *input.OwnerShareRatio
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	policy := &service.AccountShareModePolicy{}
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO account_share_mode_policies (
			platform,
			platform_share_ratio,
			owner_share_ratio,
			enabled,
			version,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, 1, NOW(), NOW())
		ON CONFLICT (platform) DO UPDATE
		SET platform_share_ratio = EXCLUDED.platform_share_ratio,
			owner_share_ratio = EXCLUDED.owner_share_ratio,
			enabled = EXCLUDED.enabled,
			version = account_share_mode_policies.version + 1,
			deleted_at = NULL,
			updated_at = NOW()
		RETURNING id, platform, platform_share_ratio, owner_share_ratio, enabled, version
	`, platform, platformRatio, ownerRatio, enabled).Scan(
		&policy.ID,
		&policy.Platform,
		&policy.PlatformShareRatio,
		&policy.OwnerShareRatio,
		&policy.Enabled,
		&policy.Version,
	)
	if err != nil {
		return nil, err
	}
	return policy, nil
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

func (r *accountShareModeRepository) queryActiveMembership(ctx context.Context, predicate string, args ...any) (*service.AccountShareMembership, *service.AccountShareListing, error) {
	query := fmt.Sprintf(`
		SELECT
			m.id, m.listing_id, m.account_id, l.owner_user_id, m.consumer_user_id, m.api_key_id, m.status,
			m.hourly_rate_snapshot, m.hourly_fee_waiver_minimum_snapshot, m.idle_timeout_minutes, m.joined_at, m.last_request_at, m.ended_at,
			m.ended_reason, m.paid_until, m.billed_until, m.created_at, m.updated_at
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
			AND l.deleted_at IS NULL
			AND l.status = '%s'
		JOIN accounts a ON a.id = m.account_id
			AND a.deleted_at IS NULL
		WHERE m.status = '%s'
			AND m.deleted_at IS NULL
			AND (m.hourly_rate_snapshot <= 0 OR m.paid_until IS NULL OR m.paid_until > NOW())
			AND %s
		ORDER BY m.joined_at DESC
		LIMIT 1
	`, service.AccountShareListingStatusActive, service.AccountShareMembershipStatusActive, predicate)
	membership := &service.AccountShareMembership{}
	var endedAt, lastRequestAt, paidUntil, billedUntil sql.NullTime
	var endedReason sql.NullString
	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&membership.ID,
		&membership.ListingID,
		&membership.AccountID,
		&membership.OwnerUserID,
		&membership.ConsumerUserID,
		&membership.APIKeyID,
		&membership.Status,
		&membership.HourlyRateSnapshot,
		&membership.HourlyFeeWaiverMinimumSnapshot,
		&membership.IdleTimeoutMinutes,
		&membership.JoinedAt,
		&lastRequestAt,
		&endedAt,
		&endedReason,
		&paidUntil,
		&billedUntil,
		&membership.CreatedAt,
		&membership.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if endedAt.Valid {
		membership.EndedAt = &endedAt.Time
	}
	if lastRequestAt.Valid {
		membership.LastRequestAt = &lastRequestAt.Time
	}
	if endedReason.Valid {
		membership.EndedReason = endedReason.String
	}
	if paidUntil.Valid {
		membership.PaidUntil = &paidUntil.Time
	}
	if billedUntil.Valid {
		membership.BilledUntil = &billedUntil.Time
	}
	listing, err := r.GetListingByID(ctx, membership.ListingID, membership.ConsumerUserID)
	if err != nil {
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
		strings.TrimSpace(filters.Search) != "" ||
		strings.TrimSpace(filters.Status) != "" ||
		filters.PerUserConcurrencyMin != nil ||
		filters.PerUserConcurrencyMax != nil ||
		filters.MinBalanceRequiredMin != nil ||
		filters.MinBalanceRequiredMax != nil ||
		filters.HourlyRateMin != nil ||
		filters.HourlyRateMax != nil ||
		filters.HourlyFeeWaiverMin != nil ||
		filters.HourlyFeeWaiverMax != nil ||
		len(filters.Models) > 0 ||
		strings.TrimSpace(filters.AccountLevel) != ""
}

func applyAccountShareMembershipNullableFields(membership *service.AccountShareMembership, lastRequestAt, endedAt sql.NullTime, endedReason sql.NullString, paidUntil, billedUntil sql.NullTime) {
	if membership == nil {
		return
	}
	if lastRequestAt.Valid {
		membership.LastRequestAt = &lastRequestAt.Time
	}
	if endedAt.Valid {
		membership.EndedAt = &endedAt.Time
	}
	if endedReason.Valid {
		membership.EndedReason = endedReason.String
	}
	if paidUntil.Valid {
		membership.PaidUntil = &paidUntil.Time
	}
	if billedUntil.Valid {
		membership.BilledUntil = &billedUntil.Time
	}
}

func accountShareMembershipIdleDeadline(membership *service.AccountShareMembership) (time.Time, bool) {
	if membership == nil || membership.IdleTimeoutMinutes <= 0 {
		return time.Time{}, false
	}
	base := membership.JoinedAt
	if membership.LastRequestAt != nil {
		base = *membership.LastRequestAt
	}
	return base.UTC().Add(time.Duration(membership.IdleTimeoutMinutes) * time.Minute), true
}

func accountShareAccountUnavailableConditionSQL(nowExpr string) string {
	codexProtectedSQL := fmt.Sprintf(`(
		a.platform = '%s'
		AND a.type = '%s'
		AND (
			%s
			OR %s
		)
	)`,
		service.PlatformOpenAI,
		service.AccountTypeOAuth,
		accountShareCodexQuotaProtectedSQL("codex_5h_used_percent", "codex_5h_reset_at", "codex_5h_limit_percent", nowExpr),
		accountShareCodexQuotaProtectedSQL("codex_7d_used_percent", "codex_7d_reset_at", "codex_7d_limit_percent", nowExpr),
	)
	return fmt.Sprintf(`(
		a.status <> '%s'
		OR a.schedulable = FALSE
		OR (a.auto_pause_on_expired = TRUE AND a.expires_at IS NOT NULL AND a.expires_at <= %s)
		OR (a.overload_until IS NOT NULL AND a.overload_until > %s)
		OR (a.rate_limit_reset_at IS NOT NULL AND a.rate_limit_reset_at > %s)
		OR (a.temp_unschedulable_until IS NOT NULL AND a.temp_unschedulable_until > %s)
		OR %s
	)`,
		service.StatusActive,
		nowExpr,
		nowExpr,
		nowExpr,
		nowExpr,
		codexProtectedSQL,
	)
}

func accountShareListingAvailableConditionSQL(nowExpr string) string {
	return fmt.Sprintf(`(
		l.status = '%[1]s'
		AND NOT %[3]s
		AND (l.editing_expires_at IS NULL OR l.editing_expires_at <= %[2]s)
		AND l.seat_limit > (
			SELECT COUNT(*)::int
			FROM account_share_memberships m_available
			WHERE m_available.listing_id = l.id
				AND m_available.status = '%[4]s'
				AND m_available.deleted_at IS NULL
				AND (m_available.hourly_rate_snapshot <= 0 OR m_available.paid_until IS NULL OR m_available.paid_until > %[2]s)
				AND (m_available.idle_timeout_minutes <= 0 OR COALESCE(m_available.last_request_at, m_available.joined_at) + (m_available.idle_timeout_minutes * INTERVAL '1 minute') > %[2]s)
		)
	)`,
		service.AccountShareListingStatusActive,
		nowExpr,
		accountShareAccountUnavailableConditionSQL(nowExpr),
		service.AccountShareMembershipStatusActive,
	)
}

func accountShareAccountUnavailableOrMissingConditionSQL(nowExpr string) string {
	return fmt.Sprintf(`(
		a.id IS NULL
		OR a.deleted_at IS NOT NULL
		OR %s
	)`, accountShareAccountUnavailableConditionSQL(nowExpr))
}

func accountShareAccountPermanentlyUnavailableConditionSQL(nowExpr string) string {
	return fmt.Sprintf(`(
		a.id IS NULL
		OR a.deleted_at IS NOT NULL
		OR a.status <> '%s'
		OR a.schedulable = FALSE
		OR (a.auto_pause_on_expired = TRUE AND a.expires_at IS NOT NULL AND a.expires_at <= %s)
	)`, service.StatusActive, nowExpr)
}

func accountShareCodexQuotaProtectedSQL(usedKey, resetKey, limitKey, nowExpr string) string {
	used := fmt.Sprintf("COALESCE((%s), 0)", accountShareExtraNumberSQL(usedKey))
	limitRaw := accountShareExtraNumberSQL(limitKey)
	minLimit := strconv.FormatFloat(service.CodexQuotaMinLimitPercent, 'f', 1, 64)
	maxLimit := strconv.FormatFloat(service.CodexQuotaMaxLimitPercent, 'f', 1, 64)
	defaultLimit := strconv.FormatFloat(service.CodexQuotaDefaultLimitPercent, 'f', 1, 64)
	limit := fmt.Sprintf(`CASE WHEN (%s) >= %s AND (%s) <= %s THEN (%s) ELSE %s END`,
		limitRaw,
		minLimit,
		limitRaw,
		maxLimit,
		limitRaw,
		defaultLimit,
	)
	resetAt := accountShareExtraTimeSQL(resetKey)
	return fmt.Sprintf(`COALESCE(((%s) >= (%s) AND (%s) > %s), FALSE)`, used, limit, resetAt, nowExpr)
}

func accountShareExtraNumberSQL(key string) string {
	return fmt.Sprintf(`CASE
		WHEN (COALESCE(a.extra, '{}'::jsonb)->>'%[1]s') ~ '^-?[0-9]+(\.[0-9]+)?$'
		THEN (COALESCE(a.extra, '{}'::jsonb)->>'%[1]s')::numeric
		ELSE NULL
	END`, key)
}

func accountShareExtraTimeSQL(key string) string {
	value := fmt.Sprintf(`(COALESCE(a.extra, '{}'::jsonb)->>'%s')`, key)
	return fmt.Sprintf(`CASE
		WHEN %[1]s ~ '^[0-9]{10,}$' THEN to_timestamp(%[1]s::double precision)
		WHEN %[1]s ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}[Tt ]' THEN %[1]s::timestamptz
		ELSE NULL
	END`, value)
}

func accountSharePlanTokenSQL() string {
	return `regexp_replace(lower(COALESCE(
		NULLIF(a.credentials->>'plan_type', ''),
		NULLIF(a.credentials->>'chatgpt_plan_type', ''),
		NULLIF(a.credentials->>'subscription_plan', ''),
		NULLIF(a.extra->>'plan_type', ''),
		NULLIF(a.extra->>'chatgpt_plan_type', ''),
		NULLIF(a.extra->>'subscription_plan', ''),
		''
	)), '[[:space:]_-]+', '', 'g')`
}

func accountShareEffectiveAccountLevelSQL() string {
	token := accountSharePlanTokenSQL()
	return fmt.Sprintf(`CASE
		WHEN a.account_level IN ('free', 'plus', 'pro', 'team') THEN a.account_level
		WHEN %[1]s IN ('free', 'chatgptfree') THEN 'free'
		WHEN %[1]s = 'plus' OR %[1]s = 'chatgptplus' OR %[1]s LIKE 'plus%%' THEN 'plus'
		WHEN %[1]s = 'team' OR %[1]s = 'chatgptteam' OR %[1]s LIKE 'team%%' THEN 'team'
		WHEN %[1]s = 'pro' OR %[1]s = 'chatgptpro' OR %[1]s LIKE 'pro%%' OR %[1]s LIKE 'chatgptpro%%' THEN 'pro'
		ELSE 'unknown'
	END`, token)
}

func accountShareListingSelectSQL() string {
	return fmt.Sprintf(`
		SELECT
			l.id,
			l.account_id,
			l.owner_user_id,
			COALESCE(u.username, ''),
			a.name,
			a.proxy_id,
			l.status,
			l.seat_limit,
			COALESCE(ac.active_seats, 0),
			l.rate_multiplier,
			l.allowed_models,
			l.per_user_concurrency,
			a.concurrency,
			l.hourly_rate,
			l.hourly_fee_waiver_minimum,
			l.min_balance_required,
			l.codex_cli_only,
			l.codex_5h_limit_percent,
			l.codex_7d_limit_percent,
			a.platform,
			a.type,
			a.account_level,
			a.status,
			a.schedulable,
			a.expires_at,
			a.last_used_at,
			a.rate_limited_at,
			a.rate_limit_reset_at,
			a.overload_until,
			a.temp_unschedulable_until,
			a.temp_unschedulable_reason,
			a.credentials,
			a.extra,
			COALESCE(NULLIF(a.credentials->>'subscription_expires_at', ''), NULLIF(a.extra->>'subscription_expires_at', '')),
			cm.id,
			cm.api_key_id,
			cm.joined_at,
			cm.paid_until,
			cm.billed_until,
			cm.idle_timeout_minutes,
			cm.last_request_at,
			hm.id,
			hm.ended_at,
			CASE WHEN l.editing_expires_at > NOW() THEN l.editing_by_user_id ELSE NULL END,
			CASE WHEN l.editing_expires_at > NOW() THEN COALESCE(eu.username, '') ELSE '' END,
			CASE WHEN l.editing_expires_at > NOW() THEN l.editing_expires_at ELSE NULL END,
			CASE WHEN l.editing_expires_at > NOW() AND l.editing_by_user_id = $1 THEN TRUE ELSE FALSE END,
			CASE WHEN l.editing_expires_at > NOW() AND l.editing_by_user_id = $1 THEN COALESCE(l.edit_session_id, '') ELSE '' END,
			l.created_at,
			l.updated_at
		FROM account_share_listings l
		JOIN accounts a ON a.id = l.account_id
		LEFT JOIN users u ON u.id = l.owner_user_id
		LEFT JOIN users eu ON eu.id = l.editing_by_user_id AND l.editing_expires_at > NOW()
		LEFT JOIN LATERAL (
			SELECT COUNT(*)::int AS active_seats
			FROM account_share_memberships m
			WHERE m.listing_id = l.id
				AND m.status = '%s'
				AND m.deleted_at IS NULL
				AND (m.hourly_rate_snapshot <= 0 OR m.paid_until IS NULL OR m.paid_until > NOW())
				AND (m.idle_timeout_minutes <= 0 OR COALESCE(m.last_request_at, m.joined_at) + (m.idle_timeout_minutes * INTERVAL '1 minute') > NOW())
		) ac ON TRUE
		LEFT JOIN LATERAL (
			SELECT m.id, m.api_key_id, m.joined_at, m.paid_until, m.billed_until, m.idle_timeout_minutes, m.last_request_at
			FROM account_share_memberships m
			WHERE m.listing_id = l.id
				AND m.consumer_user_id = $1
				AND m.status = '%s'
				AND m.deleted_at IS NULL
				AND (m.hourly_rate_snapshot <= 0 OR m.paid_until IS NULL OR m.paid_until > NOW())
				AND (m.idle_timeout_minutes <= 0 OR COALESCE(m.last_request_at, m.joined_at) + (m.idle_timeout_minutes * INTERVAL '1 minute') > NOW())
			ORDER BY m.joined_at DESC
			LIMIT 1
		) cm ON TRUE
		LEFT JOIN LATERAL (
			SELECT m.id, COALESCE(m.ended_at, m.updated_at) AS ended_at
			FROM account_share_memberships m
			WHERE m.listing_id = l.id
				AND m.consumer_user_id = $1
				AND m.status = '%s'
				AND m.deleted_at IS NULL
			ORDER BY COALESCE(m.ended_at, m.updated_at) DESC
			LIMIT 1
		) hm ON TRUE
	`, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusEnded)
}

type accountShareListingScanner interface {
	Scan(dest ...any) error
}

func scanAccountShareListing(scanner accountShareListingScanner) (*service.AccountShareListing, error) {
	listing := &service.AccountShareListing{}
	var allowedModelsRaw []byte
	var proxyID, currentMembershipID, currentAPIKeyID, currentIdleTimeoutMinutes, lastUsedMembershipID, editingByUserID sql.NullInt64
	var currentJoinedAt, currentPaidUntil, currentBilledUntil, currentLastRequestAt, lastUsedAt, editingExpiresAt sql.NullTime
	var accountPlatform, accountType, accountLevel, accountStatus string
	var accountSchedulable bool
	var accountExpiresAt, accountLastUsedAt, rateLimitedAt, rateLimitResetAt, overloadUntil, tempUnschedulableUntil sql.NullTime
	var tempUnschedulableReason, subscriptionExpiresAtRaw sql.NullString
	var editingByUsername, editSessionID string
	var credentialsRaw, extraRaw []byte
	err := scanner.Scan(
		&listing.ID,
		&listing.AccountID,
		&listing.OwnerUserID,
		&listing.OwnerUsername,
		&listing.AccountName,
		&proxyID,
		&listing.Status,
		&listing.SeatLimit,
		&listing.ActiveSeats,
		&listing.RateMultiplier,
		&allowedModelsRaw,
		&listing.PerUserConcurrency,
		&listing.AccountConcurrency,
		&listing.HourlyRate,
		&listing.HourlyFeeWaiverMinimum,
		&listing.MinBalanceRequired,
		&listing.CodexCLIOnly,
		&listing.Codex5hLimitPercent,
		&listing.Codex7dLimitPercent,
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
		&credentialsRaw,
		&extraRaw,
		&subscriptionExpiresAtRaw,
		&currentMembershipID,
		&currentAPIKeyID,
		&currentJoinedAt,
		&currentPaidUntil,
		&currentBilledUntil,
		&currentIdleTimeoutMinutes,
		&currentLastRequestAt,
		&lastUsedMembershipID,
		&lastUsedAt,
		&editingByUserID,
		&editingByUsername,
		&editingExpiresAt,
		&listing.EditingMine,
		&editSessionID,
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
		Schedulable:             accountSchedulable,
	}
	now := time.Now()
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
	if currentMembershipID.Valid {
		listing.CurrentMembershipID = &currentMembershipID.Int64
	}
	if currentAPIKeyID.Valid {
		listing.CurrentAPIKeyID = &currentAPIKeyID.Int64
	}
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
	if lastUsedMembershipID.Valid {
		listing.LastUsedMembershipID = &lastUsedMembershipID.Int64
	}
	if lastUsedAt.Valid {
		listing.LastUsedAt = &lastUsedAt.Time
	}
	listing.EditingByUserID = sqlNullInt64Ptr(editingByUserID)
	listing.EditingByUsername = editingByUsername
	listing.EditingExpiresAt = sqlNullTimePtr(editingExpiresAt)
	listing.EditSessionID = editSessionID
	return listing, nil
}

func unmarshalAccountShareJSONMap(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	if result == nil {
		return map[string]any{}, nil
	}
	return result, nil
}

func sqlNullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}

func sqlNullInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	v := value.Int64
	return &v
}

func parseAccountShareTime(raw string) *time.Time {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return &parsed
		}
	}
	if unixSeconds, err := strconv.ParseInt(value, 10, 64); err == nil && unixSeconds > 0 {
		parsed := time.Unix(unixSeconds, 0).UTC()
		return &parsed
	}
	return nil
}

func (r *accountShareModeRepository) scanGroupByID(ctx context.Context, groupID int64) (*service.Group, error) {
	group := &service.Group{}
	var ownerUserID sql.NullInt64
	var description, requiredAccountLevel, subscriptionType, defaultMappedModel sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT
			id, name, description, platform, rate_multiplier, is_exclusive, status,
			owner_user_id, scope, subscription_type, required_account_level,
			default_validity_days, allow_image_generation, image_rate_independent,
			image_rate_multiplier, claude_code_only, sort_order, allow_messages_dispatch,
			require_oauth_only, require_privacy_set, default_mapped_model, rpm_limit,
			created_at, updated_at
		FROM groups
		WHERE id = $1
			AND deleted_at IS NULL
	`, groupID).Scan(
		&group.ID,
		&group.Name,
		&description,
		&group.Platform,
		&group.RateMultiplier,
		&group.IsExclusive,
		&group.Status,
		&ownerUserID,
		&group.Scope,
		&subscriptionType,
		&requiredAccountLevel,
		&group.DefaultValidityDays,
		&group.AllowImageGeneration,
		&group.ImageRateIndependent,
		&group.ImageRateMultiplier,
		&group.ClaudeCodeOnly,
		&group.SortOrder,
		&group.AllowMessagesDispatch,
		&group.RequireOAuthOnly,
		&group.RequirePrivacySet,
		&defaultMappedModel,
		&group.RPMLimit,
		&group.CreatedAt,
		&group.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareModeGroupUnavailable
	}
	if err != nil {
		return nil, err
	}
	group.Description = description.String
	if ownerUserID.Valid {
		group.OwnerUserID = &ownerUserID.Int64
	}
	group.Scope = service.NormalizeGroupScope(group.Scope)
	group.SubscriptionType = subscriptionType.String
	group.RequiredAccountLevel = service.NormalizeRequiredAccountLevel(requiredAccountLevel.String)
	group.DefaultMappedModel = defaultMappedModel.String
	group.Hydrated = true
	return group, nil
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
		SELECT a.id
		FROM account_share_listings l
		JOIN accounts a ON a.id = l.account_id AND a.deleted_at IS NULL
		WHERE l.owner_user_id = $1
			AND LOWER(a.name) = LOWER($2)
			AND ($3::bigint <= 0 OR a.id <> $3::bigint)
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

func activeAccountShareSeatCountInTx(ctx context.Context, tx *sql.Tx, listingID int64) (int, error) {
	var activeSeats int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)::int
		FROM account_share_memberships
		WHERE listing_id = $1
			AND status = $2
			AND deleted_at IS NULL
			AND (hourly_rate_snapshot <= 0 OR paid_until IS NULL OR paid_until > NOW())
			AND (idle_timeout_minutes <= 0 OR COALESCE(last_request_at, joined_at) + (idle_timeout_minutes * INTERVAL '1 minute') > NOW())
	`, listingID, service.AccountShareMembershipStatusActive).Scan(&activeSeats); err != nil {
		return 0, err
	}
	return activeSeats, nil
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

func existsInTx(ctx context.Context, tx *sql.Tx, query string, args ...any) (bool, error) {
	var value int
	err := tx.QueryRowContext(ctx, query, args...).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func translateAccountShareMembershipConflict(err error) error {
	if err == nil {
		return nil
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		switch pqErr.Constraint {
		case "uq_account_share_memberships_active_consumer":
			return service.ErrAccountShareAlreadyUsing.WithCause(err)
		case "uq_account_share_memberships_active_api_key":
			return service.ErrAccountShareAPIKeyAlreadyBound.WithCause(err)
		default:
			return service.ErrAccountShareAlreadyUsing.WithCause(err)
		}
	}
	return err
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableEmptyString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTimePtr(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}

func derefInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
