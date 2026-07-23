package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
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
	accountShareSeatSettlementTypeUsage        = "usage_request"
	accountShareSeatSettlementTypeCharge       = "seat_charge"
	accountShareSeatSettlementTypeRefund       = "seat_refund"
	accountShareSeatSettlementTypeWaiverRefund = "seat_waiver_refund"
	accountShareSeatPrepayReason               = "account_share_mode_seat_prepay"
	accountShareSeatRefundReason               = "account_share_mode_seat_refund"
	accountShareSeatWaiverRefundReason         = "account_share_mode_seat_waiver_refund"
	accountShareSeatInviteWaiverRefundReason   = "account_share_mode_invite_waiver_refund"
	accountShareSeatIncomeReason               = "account_share_mode_income"
	accountShareModeSettlementRefType          = "account_share_mode_settlement"
	accountShareSeatPrepayRefType              = "account_share_mode_seat_prepay_ref"
)

func NewAccountShareModeRepository(_ *dbent.Client, sqlDB *sql.DB) service.AccountShareModeRepository {
	return &accountShareModeRepository{db: sqlDB}
}

func NewAccountShareModeAPIKeyBindingChecker(_ *dbent.Client, sqlDB *sql.DB) service.AccountShareAPIKeyBindingChecker {
	return &accountShareModeRepository{db: sqlDB}
}

func (r *accountShareModeRepository) HasActiveOrQueuedMembershipForAPIKey(ctx context.Context, consumerUserID, apiKeyID int64) (bool, error) {
	if consumerUserID <= 0 || apiKeyID <= 0 {
		return false, nil
	}

	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM account_share_memberships
			WHERE consumer_user_id = $1
				AND api_key_id = $2
				AND status IN ($3, $4)
				AND deleted_at IS NULL
		)
	`, consumerUserID, apiKeyID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func accountShareSeatPrepayRefID(membershipID int64, paidUntil time.Time) int64 {
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%d:%d", membershipID, paidUntil.UTC().UnixNano())
	refID := int64(h.Sum64() & 0x7fffffffffffffff)
	if refID == 0 {
		return 1
	}
	return refID
}

func ensureAccountShareAccountIdentityInTx(ctx context.Context, tx *sql.Tx, account *service.Account) (*int64, error) {
	if tx == nil || account == nil || account.ID <= 0 {
		return nil, nil
	}
	email := accountShareAccountIdentityEmail(account)
	if email == "" {
		return nil, nil
	}
	platform := strings.ToLower(strings.TrimSpace(account.Platform))
	if platform == "" {
		return nil, nil
	}
	var identityID int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO account_share_account_identities (
			platform, identity_type, identity_value, identity_hint,
			first_account_id, last_account_id, created_at, updated_at
		)
		VALUES ($1, 'email', $2, $3, $4, $4, NOW(), NOW())
		ON CONFLICT (platform, identity_type, identity_value) WHERE deleted_at IS NULL
		DO UPDATE SET
			identity_hint = EXCLUDED.identity_hint,
			last_account_id = EXCLUDED.last_account_id,
			updated_at = NOW()
		RETURNING id
	`, platform, email, accountShareIdentityHint(email), account.ID).Scan(&identityID)
	if err != nil {
		return nil, err
	}
	return &identityID, nil
}

func accountShareAccountIdentityEmail(account *service.Account) string {
	if account == nil {
		return ""
	}
	for _, value := range []string{
		accountShareStringFromMap(account.Credentials, "email"),
		accountShareStringFromMap(account.Credentials, "email_address"),
		accountShareStringFromMap(account.Extra, "email"),
		accountShareStringFromMap(account.Extra, "email_address"),
	} {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			return value
		}
	}
	return ""
}

func accountShareStringFromMap(values map[string]any, key string) string {
	if len(values) == 0 {
		return ""
	}
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

func accountShareIdentityHint(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return ""
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return ""
	}
	local := parts[0]
	domain := parts[1]
	if local == "" || domain == "" {
		return ""
	}
	if len(local) == 1 {
		return local + "***@" + domain
	}
	return local[:1] + "***" + local[len(local)-1:] + "@" + domain
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

	accountIdentityID, err := ensureAccountShareAccountIdentityInTx(ctx, tx, account)
	if err != nil {
		return nil, err
	}
	if accountIdentityID != nil {
		listing.AccountIdentityID = accountIdentityID
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
			codex_5h_limit_percent, codex_7d_limit_percent, account_identity_id, created_at, updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6::jsonb,
			$7, $8, $9, $10, $11,
			$12, $13, $14, NOW(), NOW()
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
		nullableInt64(listing.AccountIdentityID),
	).Scan(&listingID)
	if err != nil {
		return nil, err
	}
	if listing.AccountIdentityID != nil {
		if err := refreshAccountShareListingRatingsInTx(ctx, tx, *listing.AccountIdentityID); err != nil {
			return nil, err
		}
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
		whereParts = append(whereParts, "qm.id IS NOT NULL")
		applyStatusFilter(false)
	case service.AccountShareModeListingTabHistory:
		whereParts = append(whereParts, "hm.id IS NOT NULL", "qm.id IS NULL")
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
	if filters.Platform != "" {
		whereParts = append(whereParts, "a.platform = "+addArg(filters.Platform))
	}
	if filters.OwnerUserID > 0 {
		whereParts = append(whereParts, "l.owner_user_id = "+addArg(filters.OwnerUserID))
	}
	if filters.AvailableOnly {
		whereParts = append(whereParts, accountShareListingAvailableConditionSQL("NOW()"))
	}
	if len(filters.SeatLimits) > 0 {
		whereParts = append(whereParts, "l.seat_limit = ANY("+addArg(pq.Array(filters.SeatLimits))+")")
	} else if filters.SeatLimit >= service.AccountShareModeMinSeats && filters.SeatLimit <= service.AccountShareModeMaxSeats {
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
			whereParts = append(whereParts, accountShareListingAvailableConditionSQL("NOW()"))
		}
	}
	whereSQL := strings.Join(whereParts, " AND ")

	approximatePagination := filters.SkipTotal || accountShareListingUsesApproximatePagination(filters)
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
					AND m.status IN ('%s', '%s')
					AND m.deleted_at IS NULL
				ORDER BY m.queue_rank ASC, m.id ASC
				LIMIT 1
			) qm ON TRUE
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
		`, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued, service.AccountShareMembershipStatusEnded, whereSQL)
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
		ORDER BY %s
		LIMIT $%d OFFSET $%d
	`, accountShareListingSelectSQL(), whereSQL, accountShareListingOrderSQL(filters), len(args)-1, len(args))
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

type accountShareWaiverProgressMembership struct {
	ID                       int64
	JoinedAt                 time.Time
	LastRequestAt            *time.Time
	HourlyRate               float64
	WaiverMinimum            float64
	WaiverWindowStartedAt    *time.Time
	WaiverWindowUsageAmount  decimal.Decimal
	WaiverWindowRequestCount int64
	WaiverWindowLastRequest  *time.Time
}

func accountShareWaiverWindowStartAt(joinedAt time.Time, at time.Time) time.Time {
	joinedAt = joinedAt.UTC()
	at = at.UTC()
	windowMax := service.AccountShareModeSeatWaiverWindowMax
	if windowMax <= 0 {
		windowMax = time.Hour
	}
	if at.Before(joinedAt) || !at.After(joinedAt) {
		return joinedAt
	}
	elapsed := at.Sub(joinedAt)
	windows := elapsed / windowMax
	return joinedAt.Add(windows * windowMax).UTC()
}

func accountShareWaiverWindowEnd(windowStart time.Time) time.Time {
	windowMax := service.AccountShareModeSeatWaiverWindowMax
	if windowMax <= 0 {
		windowMax = time.Hour
	}
	return windowStart.Add(windowMax).UTC()
}

func buildAccountShareWaiverProgress(membership accountShareWaiverProgressMembership, usage accountShareModeUsageStat, now time.Time) *service.AccountShareWaiverProgress {
	windowStart := accountShareWaiverWindowStartAt(membership.JoinedAt, now)
	windowEnd := accountShareWaiverWindowEnd(windowStart)
	effectiveEnd := now.UTC()
	if windowEnd.Before(effectiveEnd) {
		effectiveEnd = windowEnd
	}
	if effectiveEnd.Before(windowStart) {
		effectiveEnd = windowStart
	}
	elapsedMs := effectiveEnd.Sub(windowStart).Milliseconds()
	if elapsedMs < 0 {
		elapsedMs = 0
	}
	remainingSeconds := int64(0)
	if windowEnd.After(now) {
		remainingSeconds = int64(windowEnd.Sub(now).Seconds())
	}

	minimum := decimalFromFloat(membership.WaiverMinimum)
	required := minimum.Mul(decimal.NewFromInt(elapsedMs)).Div(decimal.NewFromInt(3600000)).Round(10)
	usageAmount := usage.Total.Round(10)
	remainingAmount := required.Sub(usageAmount)
	if remainingAmount.IsNegative() {
		remainingAmount = decimal.Zero
	}
	progressPercent := 0.0
	if required.GreaterThan(decimal.Zero) {
		progressPercent, _ = usageAmount.Mul(decimal.NewFromInt(100)).Div(required).Float64()
		if progressPercent > 100 {
			progressPercent = 100
		}
	}
	status := service.AccountShareWaiverProgressStatusInProgress
	if required.GreaterThan(decimal.Zero) && usageAmount.GreaterThanOrEqual(required) {
		status = service.AccountShareWaiverProgressStatusMet
	}
	lastRequestAt := usage.LastRequestAt
	if lastRequestAt == nil {
		lastRequestAt = membership.LastRequestAt
	}
	if lastRequestAt != nil && (lastRequestAt.Before(windowStart) || !lastRequestAt.Before(windowEnd)) {
		lastRequestAt = nil
	}
	requiredFloat, _ := required.Float64()
	usageFloat, _ := usageAmount.Float64()
	remainingFloat, _ := remainingAmount.Float64()
	return &service.AccountShareWaiverProgress{
		Enabled:                  true,
		Status:                   status,
		WindowStart:              windowStart,
		WindowEnd:                windowEnd,
		Now:                      now.UTC(),
		ElapsedSeconds:           elapsedMs / 1000,
		RemainingSeconds:         remainingSeconds,
		RequiredAmount:           requiredFloat,
		UsageAmount:              usageFloat,
		RemainingAmount:          remainingFloat,
		ProgressPercent:          progressPercent,
		HourlyRate:               membership.HourlyRate,
		WaiverMinimum:            membership.WaiverMinimum,
		EstimatedHourlyFeeRefund: service.AccountShareHourlyCharge(membership.HourlyRate, int(elapsedMs)),
		RequestCount:             usage.RequestCount,
		LastRequestAt:            lastRequestAt,
	}
}

func (r *accountShareModeRepository) GetMySpendSummary(ctx context.Context, query service.AccountShareMySpendQuery) (*service.AccountShareMySpendSummary, error) {
	if query.ListingID <= 0 || query.ConsumerID <= 0 {
		return nil, service.ErrAccountShareListingNotFound
	}
	listing, err := r.getMySpendListing(ctx, query.ListingID)
	if err != nil {
		return nil, err
	}
	membership, err := r.resolveMySpendMembership(ctx, query.ListingID, query.ConsumerID, query.MembershipID)
	if err != nil {
		return nil, err
	}
	startTime := query.StartTime
	endTime := query.EndTime
	filterMembershipID := int64(0)
	if query.Range == service.AccountShareSpendRangeCurrentMembership {
		if membership == nil {
			startTime = endTime
		} else {
			filterMembershipID = membership.ID
			startTime = membership.JoinedAt
			if membership.EndedAt != nil && membership.EndedAt.Before(endTime) {
				endTime = *membership.EndedAt
			}
		}
	}
	summary := &service.AccountShareMySpendSummary{
		Range:          query.Range,
		StartTime:      startTime,
		EndTime:        endTime,
		Listing:        *listing,
		Membership:     membership,
		ModelBreakdown: []service.AccountShareMySpendModelBreakdown{},
	}
	if startTime.IsZero() || endTime.IsZero() || !endTime.After(startTime) {
		return summary, nil
	}
	if err := r.fillMySpendTotals(ctx, summary, query.ListingID, query.ConsumerID, filterMembershipID); err != nil {
		return nil, err
	}
	models, err := r.listMySpendModelBreakdown(ctx, query.ListingID, query.ConsumerID, filterMembershipID, startTime, endTime)
	if err != nil {
		return nil, err
	}
	summary.ModelBreakdown = models
	return summary, nil
}

func (r *accountShareModeRepository) getMySpendListing(ctx context.Context, listingID int64) (*service.AccountShareMySpendListing, error) {
	var listing service.AccountShareMySpendListing
	err := r.db.QueryRowContext(ctx, `
		SELECT
			l.id,
			l.account_id,
			COALESCE(a.name, ''),
			a.platform,
			l.owner_user_id,
			COALESCE(u.username, '')
		FROM account_share_listings l
		JOIN accounts a ON a.id = l.account_id AND a.deleted_at IS NULL
		LEFT JOIN users u ON u.id = l.owner_user_id
		WHERE l.id = $1
			AND l.deleted_at IS NULL
	`, listingID).Scan(
		&listing.ID,
		&listing.AccountID,
		&listing.AccountName,
		&listing.Platform,
		&listing.OwnerUserID,
		&listing.OwnerUsername,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, err
	}
	return &listing, nil
}

func (r *accountShareModeRepository) resolveMySpendMembership(ctx context.Context, listingID, consumerID int64, membershipID *int64) (*service.AccountShareMySpendMembership, error) {
	args := []any{listingID, consumerID}
	membershipPredicate := ""
	if membershipID != nil {
		if *membershipID <= 0 {
			return nil, service.ErrAccountShareListingNotFound
		}
		args = append(args, *membershipID)
		membershipPredicate = fmt.Sprintf("AND m.id = $%d", len(args))
	}
	query := fmt.Sprintf(`
		SELECT
			m.id,
			m.api_key_id,
			COALESCE(ak.name, '') AS api_key_name,
			m.status,
			m.queue_rank,
			m.joined_at,
			m.last_request_at,
			m.ended_at,
			m.ended_reason,
			m.paid_until,
			m.billed_until,
			m.hourly_rate_snapshot,
			m.hourly_fee_waiver_minimum_snapshot,
			m.idle_timeout_minutes
		FROM account_share_memberships m
		LEFT JOIN api_keys ak ON ak.id = m.api_key_id
		WHERE m.listing_id = $1
			AND m.consumer_user_id = $2
			AND m.deleted_at IS NULL
			%s
		ORDER BY
			CASE m.status
				WHEN '%s' THEN 0
				WHEN '%s' THEN 1
				WHEN '%s' THEN 2
				ELSE 3
			END,
			COALESCE(m.ended_at, m.updated_at, m.joined_at) DESC,
			m.id DESC
		LIMIT 1
	`, membershipPredicate, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued, service.AccountShareMembershipStatusEnded)
	var membership service.AccountShareMySpendMembership
	var lastRequestAt, endedAt, paidUntil, billedUntil sql.NullTime
	var endedReason sql.NullString
	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&membership.ID,
		&membership.APIKeyID,
		&membership.APIKeyName,
		&membership.Status,
		&membership.QueueRank,
		&membership.JoinedAt,
		&lastRequestAt,
		&endedAt,
		&endedReason,
		&paidUntil,
		&billedUntil,
		&membership.HourlyRate,
		&membership.WaiverMinimum,
		&membership.IdleTimeoutMinutes,
	)
	if errors.Is(err, sql.ErrNoRows) {
		if membershipID != nil {
			return nil, service.ErrAccountShareListingNotFound
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	membership.LastRequestAt = sqlNullTimePtr(lastRequestAt)
	membership.EndedAt = sqlNullTimePtr(endedAt)
	membership.PaidUntil = sqlNullTimePtr(paidUntil)
	membership.BilledUntil = sqlNullTimePtr(billedUntil)
	if endedReason.Valid {
		membership.EndedReason = endedReason.String
	}
	return &membership, nil
}

func (r *accountShareModeRepository) fillMySpendTotals(ctx context.Context, summary *service.AccountShareMySpendSummary, listingID, consumerID, membershipID int64) error {
	whereSQL, args := accountShareMySpendWhere(listingID, consumerID, membershipID, summary.StartTime, summary.EndTime)
	query := fmt.Sprintf(`
		SELECT
			COUNT(e.id) FILTER (WHERE e.settlement_type = 'usage_request')::bigint,
			COALESCE(SUM(COALESCE(ul.input_tokens, 0)) FILTER (WHERE e.settlement_type = 'usage_request'), 0)::bigint,
			COALESCE(SUM(COALESCE(ul.output_tokens, 0)) FILTER (WHERE e.settlement_type = 'usage_request'), 0)::bigint,
			COALESCE(SUM(COALESCE(ul.cache_creation_tokens, 0)) FILTER (WHERE e.settlement_type = 'usage_request'), 0)::bigint,
			COALESCE(SUM(COALESCE(ul.cache_read_tokens, 0)) FILTER (WHERE e.settlement_type = 'usage_request'), 0)::bigint,
			COALESCE(SUM(e.total_charge) FILTER (WHERE e.settlement_type = 'usage_request'), 0)::double precision,
			MAX(e.created_at)
		FROM account_share_mode_settlement_entries e
		LEFT JOIN usage_logs ul ON ul.id = e.usage_log_id
		WHERE %s
	`, whereSQL)
	var lastActivityAt sql.NullTime
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&summary.RequestCount,
		&summary.InputTokens,
		&summary.OutputTokens,
		&summary.CacheCreationTokens,
		&summary.CacheReadTokens,
		&summary.RequestCost,
		&lastActivityAt,
	); err != nil {
		return err
	}
	if err := r.fillMySpendHourlyLedgerTotals(ctx, summary, listingID, consumerID, membershipID); err != nil {
		return err
	}
	summary.TotalTokens = summary.InputTokens + summary.OutputTokens + summary.CacheCreationTokens + summary.CacheReadTokens
	summary.HourlyNetCost = summary.HourlyCharge - summary.HourlyRefund - summary.HourlyWaiverRefund
	if summary.HourlyNetCost < 0 {
		summary.HourlyNetCost = 0
	}
	summary.TotalCost = summary.RequestCost + summary.HourlyNetCost
	summary.LastActivityAt = sqlNullTimePtr(lastActivityAt)
	return nil
}

func (r *accountShareModeRepository) fillMySpendHourlyLedgerTotals(ctx context.Context, summary *service.AccountShareMySpendSummary, listingID, consumerID, membershipID int64) error {
	if summary == nil {
		return nil
	}
	where := []string{
		"ubl.user_id = $1",
		"ubl.created_at >= $2",
		"ubl.created_at < $3",
		"ubl.reason IN ($4, $5, $6)",
	}
	args := []any{
		consumerID,
		summary.StartTime,
		summary.EndTime,
		accountShareSeatPrepayReason,
		accountShareSeatRefundReason,
		accountShareSeatWaiverRefundReason,
	}
	next := len(args) + 1
	where = append(where, fmt.Sprintf("(ubl.metadata->>'listing_id')::bigint = $%d", next))
	args = append(args, listingID)
	next++
	if membershipID > 0 {
		where = append(where, fmt.Sprintf("(ubl.metadata->>'membership_id')::bigint = $%d", next))
		args = append(args, membershipID)
		next++
	}
	query := fmt.Sprintf(`
		SELECT
			COALESCE(SUM(ubl.amount) FILTER (WHERE ubl.direction = 'debit' AND ubl.reason = $4), 0)::double precision,
			COALESCE(SUM(ubl.amount) FILTER (WHERE ubl.direction = 'credit' AND ubl.reason = $5), 0)::double precision,
			COALESCE(SUM(ubl.amount) FILTER (WHERE ubl.direction = 'credit' AND ubl.reason = $6), 0)::double precision
		FROM user_balance_ledger ubl
		WHERE %s
	`, strings.Join(where, " AND "))
	return r.db.QueryRowContext(ctx, query, args...).Scan(
		&summary.HourlyCharge,
		&summary.HourlyRefund,
		&summary.HourlyWaiverRefund,
	)
}

func (r *accountShareModeRepository) listMySpendModelBreakdown(ctx context.Context, listingID, consumerID, membershipID int64, startTime, endTime time.Time) ([]service.AccountShareMySpendModelBreakdown, error) {
	whereSQL, args := accountShareMySpendWhere(listingID, consumerID, membershipID, startTime, endTime)
	query := fmt.Sprintf(`
		SELECT
			COALESCE(NULLIF(ul.model, ''), 'unknown') AS model,
			COUNT(ul.id)::bigint,
			COALESCE(SUM(COALESCE(ul.input_tokens, 0)), 0)::bigint,
			COALESCE(SUM(COALESCE(ul.output_tokens, 0)), 0)::bigint,
			COALESCE(SUM(COALESCE(ul.cache_creation_tokens, 0)), 0)::bigint,
			COALESCE(SUM(COALESCE(ul.cache_read_tokens, 0)), 0)::bigint,
			COALESCE(SUM(e.total_charge), 0)::double precision
		FROM account_share_mode_settlement_entries e
		JOIN usage_logs ul ON ul.id = e.usage_log_id
		WHERE %s
			AND e.settlement_type = 'usage_request'
		GROUP BY COALESCE(NULLIF(ul.model, ''), 'unknown')
		ORDER BY COALESCE(SUM(e.total_charge), 0) DESC, COUNT(ul.id) DESC, model ASC
	`, whereSQL)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()
	items := make([]service.AccountShareMySpendModelBreakdown, 0)
	for rows.Next() {
		var item service.AccountShareMySpendModelBreakdown
		if err := rows.Scan(
			&item.Model,
			&item.RequestCount,
			&item.InputTokens,
			&item.OutputTokens,
			&item.CacheCreationTokens,
			&item.CacheReadTokens,
			&item.RequestCost,
		); err != nil {
			return nil, err
		}
		item.TotalTokens = item.InputTokens + item.OutputTokens + item.CacheCreationTokens + item.CacheReadTokens
		if item.RequestCount > 0 {
			item.AverageRequestCost = item.RequestCost / float64(item.RequestCount)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func accountShareMySpendWhere(listingID, consumerID, membershipID int64, startTime, endTime time.Time) (string, []any) {
	args := []any{listingID, consumerID, startTime, endTime}
	where := []string{
		"e.listing_id = $1",
		"e.consumer_user_id = $2",
		"e.created_at >= $3",
		"e.created_at < $4",
	}
	if membershipID > 0 {
		args = append(args, membershipID)
		where = append(where, fmt.Sprintf("e.membership_id = $%d", len(args)))
	}
	return strings.Join(where, " AND "), args
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
		if activeSeats > 0 && (!actorIsAdmin || !input.ForceActiveEdit) {
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
	if input.Anthropic5hLimitPercent != nil {
		setParts = append(setParts, "codex_5h_limit_percent = "+addArg(*input.Anthropic5hLimitPercent))
	}
	if input.Anthropic7dLimitPercent != nil {
		setParts = append(setParts, "codex_7d_limit_percent = "+addArg(*input.Anthropic7dLimitPercent))
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
	if input.Anthropic5hLimitPercent != nil {
		if err := addExtraSet("anthropic_5h_limit_percent", *input.Anthropic5hLimitPercent); err != nil {
			return nil, err
		}
	}
	if input.Anthropic7dLimitPercent != nil {
		if err := addExtraSet("anthropic_7d_limit_percent", *input.Anthropic7dLimitPercent); err != nil {
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
	if activeSeats > 0 && (!actorIsAdmin || !input.Force) {
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
		input.Anthropic5hLimitPercent == nil &&
		input.Anthropic7dLimitPercent == nil &&
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
		input.Anthropic5hLimitPercent != nil ||
		input.Anthropic7dLimitPercent != nil ||
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
	ownerSelfUse := ownerUserID == consumerUserID
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
	if ownerSelfUse {
		hourlyRate = 0
		hourlyFeeWaiverMinimum = 0
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
	if !ownerSelfUse && userBalance < minBalanceRequired {
		return nil, service.ErrAccountShareBalanceBelowMinimum
	}

	existing, err := scanAccountShareMembership(tx.QueryRowContext(ctx, `
		SELECT
			m.id, m.listing_id, m.account_id, l.owner_user_id, m.consumer_user_id, m.api_key_id,
			m.status, m.queue_rank, m.hourly_rate_snapshot, m.hourly_fee_waiver_minimum_snapshot, m.idle_timeout_minutes,
			m.joined_at, m.last_request_at, m.ended_at, m.ended_reason, m.paid_until, m.billed_until,
			m.waiver_window_started_at, m.waiver_window_usage_amount, m.waiver_window_request_count, m.waiver_window_last_request_at,
			m.dispatch_failed_at, m.dispatch_cooldown_until, m.created_at, m.updated_at
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
		WHERE m.consumer_user_id = $1
			AND m.api_key_id = $2
			AND m.listing_id = $3
			AND m.status IN ($4, $5)
			AND m.deleted_at IS NULL
		LIMIT 1
	`, consumerUserID, apiKeyID, listingID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued))
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if _, err := endStaleQueuedMembershipsForAPIKeyInTx(ctx, tx, consumerUserID, apiKeyID, now); err != nil {
		return nil, err
	}

	var queueCount, maxQueueRank int
	var hasActive bool
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)::int,
			COALESCE(MAX(queue_rank), 0)::int,
			COALESCE(BOOL_OR(status = $3), FALSE)
		FROM account_share_memberships
		WHERE consumer_user_id = $1
			AND api_key_id = $2
			AND status IN ($3, $4)
			AND deleted_at IS NULL
	`, consumerUserID, apiKeyID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued).Scan(&queueCount, &maxQueueRank, &hasActive); err != nil {
		return nil, err
	}
	if queueCount >= service.AccountShareModeQueueMaxItems {
		return nil, service.ErrAccountShareQueueFull
	}
	queueRank := maxQueueRank + 1
	activateNow := false
	if !hasActive && queueCount == 0 {
		if ownerSelfUse {
			activateNow = true
		} else {
			activeSeats, err := activeAccountShareSeatCountInTx(ctx, tx, listingID)
			if err != nil {
				return nil, err
			}
			activateNow = activeSeats < seatLimit
		}
	}
	if activateNow && !ownerSelfUse && prepayAmount > 0 && userBalance < minBalanceRequired+prepayAmount {
		return nil, service.ErrAccountShareModePrepayInsufficient
	}

	membership := &service.AccountShareMembership{}
	var endedAt, lastRequestAt sql.NullTime
	var paidUntilScan, billedUntilScan, dispatchFailedAt, dispatchCooldownUntil sql.NullTime
	var waiverWindowStartedAt, waiverWindowLastRequestAt sql.NullTime
	var endedReason sql.NullString
	var paidUntilValue any
	var billedUntilValue any
	var waiverWindowStartedAtValue any
	membershipStatus := service.AccountShareMembershipStatusQueued
	if activateNow {
		membershipStatus = service.AccountShareMembershipStatusActive
	}
	if activateNow && prepayAmount > 0 {
		paidUntilValue = paidUntil
		billedUntilValue = now
		waiverWindowStartedAtValue = now
	} else {
		paidUntilValue = nil
		billedUntilValue = nil
		waiverWindowStartedAtValue = nil
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO account_share_memberships (
			listing_id, account_id, consumer_user_id, api_key_id, status,
			queue_rank, hourly_rate_snapshot, hourly_fee_waiver_minimum_snapshot, idle_timeout_minutes, joined_at, last_request_at,
			ended_reason, paid_until, billed_until, waiver_window_started_at, waiver_window_usage_amount,
			waiver_window_request_count, waiver_window_last_request_at, dispatch_failed_at, dispatch_cooldown_until, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULL, NULL, $11, $12, $13, 0, 0, NULL, NULL, NULL, NOW(), NOW())
		RETURNING id, listing_id, account_id, consumer_user_id, api_key_id, status, queue_rank,
			hourly_rate_snapshot, hourly_fee_waiver_minimum_snapshot, idle_timeout_minutes, joined_at, last_request_at, ended_at,
			ended_reason, paid_until, billed_until, waiver_window_started_at, waiver_window_usage_amount,
			waiver_window_request_count, waiver_window_last_request_at, dispatch_failed_at, dispatch_cooldown_until, created_at, updated_at
	`, listingID, accountID, consumerUserID, apiKeyID, membershipStatus, queueRank, hourlyRate, hourlyFeeWaiverMinimum, idleTimeoutMinutes, now, paidUntilValue, billedUntilValue, waiverWindowStartedAtValue).Scan(
		&membership.ID,
		&membership.ListingID,
		&membership.AccountID,
		&membership.ConsumerUserID,
		&membership.APIKeyID,
		&membership.Status,
		&membership.QueueRank,
		&membership.HourlyRateSnapshot,
		&membership.HourlyFeeWaiverMinimumSnapshot,
		&membership.IdleTimeoutMinutes,
		&membership.JoinedAt,
		&lastRequestAt,
		&endedAt,
		&endedReason,
		&paidUntilScan,
		&billedUntilScan,
		&waiverWindowStartedAt,
		&membership.WaiverWindowUsageAmount,
		&membership.WaiverWindowRequestCount,
		&waiverWindowLastRequestAt,
		&dispatchFailedAt,
		&dispatchCooldownUntil,
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
	if waiverWindowStartedAt.Valid {
		membership.WaiverWindowStartedAt = &waiverWindowStartedAt.Time
	}
	if waiverWindowLastRequestAt.Valid {
		membership.WaiverWindowLastRequestAt = &waiverWindowLastRequestAt.Time
	}
	if dispatchFailedAt.Valid {
		membership.DispatchFailedAt = &dispatchFailedAt.Time
	}
	if dispatchCooldownUntil.Valid {
		membership.DispatchCooldownUntil = &dispatchCooldownUntil.Time
	}
	membership.OwnerUserID = ownerUserID
	if activateNow && prepayAmount > 0 {
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
			UserID:          consumerUserID,
			Direction:       "debit",
			Amount:          decimalFromFloat(prepayAmount),
			Reason:          accountShareSeatPrepayReason,
			RefType:         accountShareSeatPrepayRefType,
			RefID:           accountShareSeatPrepayRefID(membership.ID, paidUntil),
			BalanceAfter:    decimalFromSignedFloat(newBalance),
			RequireInserted: true,
			Metadata: map[string]any{
				"listing_id":    listingID,
				"account_id":    accountID,
				"membership_id": membership.ID,
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

func (r *accountShareModeRepository) EndMembership(ctx context.Context, consumerUserID int64, membershipID int64) (*service.AccountShareMembership, *service.AccountShareSeatBillingResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	membership, err := scanAccountShareMembership(tx.QueryRowContext(ctx, `
		SELECT
			m.id, m.listing_id, m.account_id, l.owner_user_id, m.consumer_user_id, m.api_key_id,
			m.status, m.queue_rank, m.hourly_rate_snapshot, m.hourly_fee_waiver_minimum_snapshot, m.idle_timeout_minutes,
			m.joined_at, m.last_request_at, m.ended_at, m.ended_reason, m.paid_until, m.billed_until,
			m.waiver_window_started_at, m.waiver_window_usage_amount, m.waiver_window_request_count, m.waiver_window_last_request_at,
			m.dispatch_failed_at, m.dispatch_cooldown_until, m.created_at, m.updated_at
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
		WHERE m.id = $1
			AND m.consumer_user_id = $2
			AND m.deleted_at IS NULL
		FOR UPDATE OF m
	`,
		membershipID,
		consumerUserID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if membership.Status == service.AccountShareMembershipStatusEnded {
		if err := tx.Commit(); err != nil {
			return nil, nil, err
		}
		tx = nil
		return membership, accountShareMembershipBillingResult(membership, nil), nil
	}
	if membership.Status != service.AccountShareMembershipStatusActive && membership.Status != service.AccountShareMembershipStatusQueued {
		return nil, nil, service.ErrAccountShareListingNotFound
	}

	now := time.Now().UTC()
	var settledUntil *time.Time
	var creditUserIDs []int64
	if membership.Status == service.AccountShareMembershipStatusActive {
		settledUntil, _, creditUserIDs, err = r.settleSeatChargeInTx(ctx, tx, membership, now, true, now)
		if err != nil {
			return nil, nil, err
		}
		if err := r.refundUnusedSeatPrepayInTx(ctx, tx, membership, now); err != nil {
			return nil, nil, err
		}
	}
	if settledUntil == nil {
		settledUntil = &now
	}
	endedAtValue := now
	var endedAt, paidUntil, billedUntil, dispatchFailedAt, dispatchCooldownUntil sql.NullTime
	var endedReason sql.NullString
	err = tx.QueryRowContext(ctx, `
		UPDATE account_share_memberships
		SET status = $1,
			ended_at = $2,
			ended_reason = $3,
			paid_until = $4,
			billed_until = $5,
			waiver_window_started_at = $5,
			waiver_window_usage_amount = 0,
			waiver_window_request_count = 0,
			waiver_window_last_request_at = NULL,
			dispatch_cooldown_until = NULL,
			updated_at = NOW()
		WHERE id = $6
		RETURNING status, ended_at, ended_reason, paid_until, billed_until, dispatch_failed_at, dispatch_cooldown_until, updated_at
	`,
		service.AccountShareMembershipStatusEnded,
		endedAtValue,
		service.AccountShareMembershipEndReasonManual,
		*settledUntil,
		*settledUntil,
		membership.ID,
	).Scan(&membership.Status, &endedAt, &endedReason, &paidUntil, &billedUntil, &dispatchFailedAt, &dispatchCooldownUntil, &membership.UpdatedAt)
	if err != nil {
		return nil, nil, err
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
	if dispatchFailedAt.Valid {
		membership.DispatchFailedAt = &dispatchFailedAt.Time
	} else {
		membership.DispatchFailedAt = nil
	}
	if dispatchCooldownUntil.Valid {
		membership.DispatchCooldownUntil = &dispatchCooldownUntil.Time
	} else {
		membership.DispatchCooldownUntil = nil
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	tx = nil
	return membership, accountShareMembershipBillingResult(membership, creditUserIDs), nil
}

func (r *accountShareModeRepository) UpdateMembershipIdleTimeout(ctx context.Context, consumerUserID int64, membershipID int64, idleTimeoutMinutes int) (*service.AccountShareMembership, error) {
	membership, err := scanAccountShareMembership(r.db.QueryRowContext(ctx, `
		UPDATE account_share_memberships m
		SET idle_timeout_minutes = $1,
			updated_at = NOW()
		FROM account_share_listings l
		WHERE m.id = $2
			AND m.consumer_user_id = $3
			AND m.status IN ($4, $5)
			AND m.deleted_at IS NULL
			AND l.id = m.listing_id
		RETURNING
			m.id, m.listing_id, m.account_id, l.owner_user_id, m.consumer_user_id, m.api_key_id,
			m.status, m.queue_rank, m.hourly_rate_snapshot, m.hourly_fee_waiver_minimum_snapshot, m.idle_timeout_minutes,
			m.joined_at, m.last_request_at, m.ended_at, m.ended_reason, m.paid_until, m.billed_until,
			m.waiver_window_started_at, m.waiver_window_usage_amount, m.waiver_window_request_count, m.waiver_window_last_request_at,
			m.dispatch_failed_at, m.dispatch_cooldown_until, m.created_at, m.updated_at
	`, idleTimeoutMinutes, membershipID, consumerUserID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, err
	}
	return membership, nil
}

func (r *accountShareModeRepository) SubmitReview(ctx context.Context, consumerUserID int64, membershipID int64, input service.SubmitAccountShareReviewInput) (*service.AccountShareReview, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	var lockedListingID int64
	err = tx.QueryRowContext(ctx, `
		SELECT l.id
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
		WHERE m.id = $1
			AND m.consumer_user_id = $2
			AND m.deleted_at IS NULL
			AND l.deleted_at IS NULL
		FOR UPDATE OF l
	`, membershipID, consumerUserID).Scan(&lockedListingID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, err
	}

	var listingID, accountID, ownerUserID int64
	var accountIdentityID sql.NullInt64
	var lastRequestAt sql.NullTime
	var membershipStatus, accountName, platform string
	var credentialsRaw, extraRaw []byte
	err = tx.QueryRowContext(ctx, `
		SELECT
			m.listing_id,
			m.account_id,
			l.account_identity_id,
			l.owner_user_id,
			m.last_request_at,
			m.status,
			a.name,
			a.platform,
			a.credentials,
			a.extra
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
		JOIN accounts a ON a.id = m.account_id
		WHERE m.id = $1
			AND m.consumer_user_id = $2
			AND m.deleted_at IS NULL
			AND l.deleted_at IS NULL
		FOR UPDATE OF m
	`, membershipID, consumerUserID).Scan(
		&listingID,
		&accountID,
		&accountIdentityID,
		&ownerUserID,
		&lastRequestAt,
		&membershipStatus,
		&accountName,
		&platform,
		&credentialsRaw,
		&extraRaw,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, err
	}
	if ownerUserID == consumerUserID {
		return nil, service.ErrAccountShareReviewSelfUse
	}
	if membershipStatus != service.AccountShareMembershipStatusEnded || !lastRequestAt.Valid {
		return nil, service.ErrAccountShareReviewNoUsage
	}

	identityID := accountIdentityID.Int64
	if identityID <= 0 {
		credentials, err := unmarshalAccountShareJSONMap(credentialsRaw)
		if err != nil {
			return nil, err
		}
		extra, err := unmarshalAccountShareJSONMap(extraRaw)
		if err != nil {
			return nil, err
		}
		account := &service.Account{
			ID:          accountID,
			Name:        accountName,
			Platform:    platform,
			Credentials: credentials,
			Extra:       extra,
		}
		resolvedIdentityID, err := ensureAccountShareAccountIdentityInTx(ctx, tx, account)
		if err != nil {
			return nil, err
		}
		if resolvedIdentityID == nil || *resolvedIdentityID <= 0 {
			return nil, service.ErrAccountShareReviewIdentityMissing
		}
		identityID = *resolvedIdentityID
		if _, err := tx.ExecContext(ctx, `
			UPDATE account_share_listings
			SET account_identity_id = $1
			WHERE id = $2
				AND account_identity_id IS NULL
		`, identityID, listingID); err != nil {
			return nil, err
		}
	}

	comment := strings.TrimSpace(input.Comment)
	commentStatus := service.AccountShareReviewCommentStatusNone
	var moderationRequestedAt any
	var moderationNextRetryAt any
	if comment != "" {
		commentStatus = service.AccountShareReviewCommentStatusPending
		now := time.Now().UTC()
		moderationRequestedAt = now
		moderationNextRetryAt = now
	}

	var reviewID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO account_share_reviews (
			account_identity_id, listing_id, account_id, membership_id,
			owner_user_id, consumer_user_id, score, comment, comment_status,
			moderation_requested_at, moderation_next_retry_at, created_at, updated_at
		)
		VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8, $9,
			$10, $11, NOW(), NOW()
		)
		RETURNING id
	`, identityID, listingID, accountID, membershipID, ownerUserID, consumerUserID, input.Score, comment, commentStatus, moderationRequestedAt, moderationNextRetryAt).Scan(&reviewID)
	if err != nil {
		if isAccountShareReviewUniqueViolation(err) {
			return nil, service.ErrAccountShareReviewAlreadyExists.WithCause(err)
		}
		return nil, err
	}
	if err := refreshAccountShareListingRatingsInTx(ctx, tx, identityID); err != nil {
		return nil, err
	}
	review, err := getAccountShareReviewByIDTx(ctx, tx, reviewID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return review, nil
}

func (r *accountShareModeRepository) ListListingReviews(ctx context.Context, viewerUserID int64, listingID int64, params pagination.PaginationParams) ([]service.AccountShareReview, *pagination.PaginationResult, error) {
	page := params.Page
	if page < 1 {
		page = 1
	}
	limit := params.Limit()
	offset := (page - 1) * limit

	var total int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM account_share_reviews r
		JOIN account_share_listings l ON l.id = $1
		WHERE r.account_identity_id = l.account_identity_id
			AND r.comment_status = $2
			AND r.comment <> ''
			AND r.deleted_at IS NULL
			AND l.deleted_at IS NULL
	`, listingID, service.AccountShareReviewCommentStatusApproved).Scan(&total); err != nil {
		return nil, nil, err
	}
	rows, err := r.db.QueryContext(ctx, accountShareReviewSelectSQL()+`
		JOIN account_share_listings target_l ON target_l.id = $1
		WHERE r.account_identity_id = target_l.account_identity_id
			AND r.comment_status = $2
			AND r.comment <> ''
			AND r.deleted_at IS NULL
			AND target_l.deleted_at IS NULL
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT $3 OFFSET $4
	`, listingID, service.AccountShareReviewCommentStatusApproved, limit, offset)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		_ = rows.Close()
	}()
	reviews, err := scanAccountShareReviews(rows)
	if err != nil {
		return nil, nil, err
	}
	return reviews, accountShareReviewPagination(total, page, limit), nil
}

func (r *accountShareModeRepository) ListOwnerReviews(ctx context.Context, viewerUserID int64, ownerUserID int64, params pagination.PaginationParams) ([]service.AccountShareReview, *pagination.PaginationResult, error) {
	page := params.Page
	if page < 1 {
		page = 1
	}
	limit := params.Limit()
	offset := (page - 1) * limit

	var total int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM account_share_reviews r
		WHERE r.owner_user_id = $1
			AND r.comment_status = $2
			AND r.comment <> ''
			AND r.deleted_at IS NULL
	`, ownerUserID, service.AccountShareReviewCommentStatusApproved).Scan(&total); err != nil {
		return nil, nil, err
	}
	rows, err := r.db.QueryContext(ctx, accountShareReviewSelectSQL()+`
		WHERE r.owner_user_id = $1
			AND r.comment_status = $2
			AND r.comment <> ''
			AND r.deleted_at IS NULL
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT $3 OFFSET $4
	`, ownerUserID, service.AccountShareReviewCommentStatusApproved, limit, offset)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		_ = rows.Close()
	}()
	reviews, err := scanAccountShareReviews(rows)
	if err != nil {
		return nil, nil, err
	}
	return reviews, accountShareReviewPagination(total, page, limit), nil
}

func (r *accountShareModeRepository) ClaimPendingReviewModerations(ctx context.Context, now time.Time, limit int) ([]service.AccountShareReview, error) {
	if limit <= 0 {
		limit = service.AccountShareReviewModerationBatchSize
	}
	rows, err := r.db.QueryContext(ctx, `
		WITH picked AS (
			SELECT id
			FROM account_share_reviews
			WHERE deleted_at IS NULL
				AND comment <> ''
				AND comment_status IN ($2, $3)
				AND moderation_attempts < $4
				AND (moderation_next_retry_at IS NULL OR moderation_next_retry_at <= $1)
			ORDER BY COALESCE(moderation_next_retry_at, created_at), id
			LIMIT $5
			FOR UPDATE SKIP LOCKED
		), claimed AS (
			UPDATE account_share_reviews r_claim
			SET comment_status = $2,
				moderation_attempts = r_claim.moderation_attempts + 1,
				moderation_requested_at = $1,
				moderation_next_retry_at = NULL,
				updated_at = NOW()
			FROM picked
			WHERE r_claim.id = picked.id
			RETURNING r_claim.id
		)
		`+accountShareReviewSelectSQL()+`
		JOIN claimed ON claimed.id = r.id
		ORDER BY r.created_at ASC, r.id ASC
	`, now, service.AccountShareReviewCommentStatusPending, service.AccountShareReviewCommentStatusFailed, service.AccountShareReviewModerationMaxAttempts, limit)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()
	return scanAccountShareReviews(rows)
}

func (r *accountShareModeRepository) CompleteReviewModeration(ctx context.Context, reviewID int64, result service.AccountShareReviewModerationResult) error {
	status := service.AccountShareReviewCommentStatusApproved
	reason := ""
	if !result.Passed {
		status = service.AccountShareReviewCommentStatusRejected
		reason = strings.TrimSpace(result.RejectReason)
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE account_share_reviews
		SET comment_status = $2,
			comment_reject_reason = $3,
			moderation_last_error = '',
			moderated_at = NOW(),
			moderation_model_snapshot = $4,
			moderation_url_snapshot = $5,
			updated_at = NOW()
		WHERE id = $1
			AND deleted_at IS NULL
			AND comment <> ''
	`, reviewID, status, reason, strings.TrimSpace(result.ModelSnapshot), strings.TrimSpace(result.URLSnapshot))
	return err
}

func (r *accountShareModeRepository) FailReviewModeration(ctx context.Context, reviewID int64, reason string, nextRetryAt time.Time, maxAttempts int) error {
	if maxAttempts <= 0 {
		maxAttempts = service.AccountShareReviewModerationMaxAttempts
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE account_share_reviews
		SET comment_status = $2,
			moderation_last_error = $3,
			moderation_next_retry_at = CASE
				WHEN moderation_attempts >= $5 THEN NULL
				ELSE $4
			END,
			updated_at = NOW()
		WHERE id = $1
			AND deleted_at IS NULL
			AND comment <> ''
	`, reviewID, service.AccountShareReviewCommentStatusFailed, strings.TrimSpace(reason), nextRetryAt, maxAttempts)
	return err
}

func (r *accountShareModeRepository) ListMembershipQueue(ctx context.Context, consumerUserID int64, apiKeyID int64) ([]service.AccountShareMembership, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			m.id, m.listing_id, m.account_id, l.owner_user_id, m.consumer_user_id, m.api_key_id,
			m.status, m.queue_rank, m.hourly_rate_snapshot, m.hourly_fee_waiver_minimum_snapshot, m.idle_timeout_minutes,
			m.joined_at, m.last_request_at, m.ended_at, m.ended_reason, m.paid_until, m.billed_until,
			m.waiver_window_started_at, m.waiver_window_usage_amount, m.waiver_window_request_count, m.waiver_window_last_request_at,
			m.dispatch_failed_at, m.dispatch_cooldown_until, m.created_at, m.updated_at
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
		WHERE m.consumer_user_id = $1
			AND m.api_key_id = $2
			AND m.status IN ($3, $4)
			AND m.deleted_at IS NULL
		ORDER BY m.queue_rank ASC, m.id ASC
	`, consumerUserID, apiKeyID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	memberships := make([]service.AccountShareMembership, 0, service.AccountShareModeQueueMaxItems)
	for rows.Next() {
		membership, err := scanAccountShareMembership(rows)
		if err != nil {
			return nil, err
		}
		memberships = append(memberships, *membership)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return memberships, nil
}

func (r *accountShareModeRepository) ReorderMembershipQueue(ctx context.Context, consumerUserID int64, apiKeyID int64, membershipIDs []int64) ([]service.AccountShareMembership, error) {
	if len(membershipIDs) == 0 || len(membershipIDs) > service.AccountShareModeQueueMaxItems {
		return nil, service.ErrAccountShareQueueInvalid
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

	rows, err := tx.QueryContext(ctx, `
		SELECT
			m.id, m.listing_id, m.account_id, l.owner_user_id, m.consumer_user_id, m.api_key_id,
			m.status, m.queue_rank, m.hourly_rate_snapshot, m.hourly_fee_waiver_minimum_snapshot, m.idle_timeout_minutes,
			m.joined_at, m.last_request_at, m.ended_at, m.ended_reason, m.paid_until, m.billed_until,
			m.waiver_window_started_at, m.waiver_window_usage_amount, m.waiver_window_request_count, m.waiver_window_last_request_at,
			m.dispatch_failed_at, m.dispatch_cooldown_until, m.created_at, m.updated_at
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
		WHERE m.consumer_user_id = $1
			AND m.api_key_id = $2
			AND m.status IN ($3, $4)
			AND m.deleted_at IS NULL
		ORDER BY m.queue_rank ASC, m.id ASC
		FOR UPDATE OF m
	`, consumerUserID, apiKeyID, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued)
	if err != nil {
		return nil, err
	}
	current := make(map[int64]*service.AccountShareMembership)
	for rows.Next() {
		membership, err := scanAccountShareMembership(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		current[membership.ID] = membership
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(current) != len(membershipIDs) {
		return nil, service.ErrAccountShareQueueInvalid
	}
	for _, id := range membershipIDs {
		if _, ok := current[id]; !ok {
			return nil, service.ErrAccountShareQueueInvalid
		}
	}
	for index, id := range membershipIDs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE account_share_memberships
			SET queue_rank = $1,
				updated_at = NOW()
			WHERE id = $2
		`, 100+index, id); err != nil {
			return nil, err
		}
	}
	for index, id := range membershipIDs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE account_share_memberships
			SET queue_rank = $1,
				updated_at = NOW()
			WHERE id = $2
		`, index+1, id); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil

	out := make([]service.AccountShareMembership, 0, len(membershipIDs))
	for _, id := range membershipIDs {
		item := *current[id]
		item.QueueRank = len(out) + 1
		out = append(out, item)
	}
	return out, nil
}

func (r *accountShareModeRepository) TouchMembershipLastRequest(ctx context.Context, membershipID int64, at time.Time) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE account_share_memberships
		SET last_request_at = CASE
				WHEN last_request_at IS NULL OR last_request_at < $1 THEN $1
				ELSE last_request_at
			END,
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
	defer func() {
		_ = rows.Close()
	}()

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

func (r *accountShareModeRepository) EndIdleMembership(ctx context.Context, membershipID int64, endedAt time.Time) (*service.AccountShareMembership, *service.AccountShareSeatBillingResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	membership, err := r.lockSeatBillingMembershipInTx(ctx, tx, membershipID, 0)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	deadline, ok := accountShareMembershipIdleDeadline(membership)
	if !ok || deadline.After(endedAt.UTC()) {
		return nil, nil, service.ErrAccountShareListingNotFound
	}
	settledUntil, _, creditUserIDs, err := r.settleSeatChargeInTx(ctx, tx, membership, deadline, true, endedAt)
	if err != nil {
		return nil, nil, err
	}
	if err := r.refundUnusedSeatPrepayInTx(ctx, tx, membership, deadline); err != nil {
		return nil, nil, err
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
			waiver_window_started_at = $4,
			waiver_window_usage_amount = 0,
			waiver_window_request_count = 0,
			waiver_window_last_request_at = NULL,
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
		return nil, nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	applyAccountShareMembershipNullableFields(membership, sql.NullTime{}, endedAtNull, endedReasonNull, paidUntilNull, billedUntilNull)
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	tx = nil
	return membership, accountShareMembershipBillingResult(membership, creditUserIDs), nil
}

func (r *accountShareModeRepository) ProcessUnavailableMemberships(ctx context.Context, now time.Time, limit int) (*service.AccountShareSeatBillingResult, error) {
	if limit <= 0 {
		limit = service.AccountShareModeSeatBillingBatchSize
	}
	now = now.UTC()
	query := fmt.Sprintf(`
		SELECT m.id
		FROM account_share_memberships m
		LEFT JOIN account_share_listings l ON l.id = m.listing_id
		LEFT JOIN accounts a ON a.id = m.account_id
		WHERE m.status = $1
			AND m.deleted_at IS NULL
			AND %s
		ORDER BY m.joined_at ASC, m.id ASC
		LIMIT $3
	`, accountShareMembershipPermanentlyUnavailableConditionSQL("$2"))
	rows, err := r.db.QueryContext(ctx, query, service.AccountShareMembershipStatusActive, now, limit)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

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
	result, err = r.processUnavailableMembershipIDs(ctx, ids, result, now)
	if err != nil {
		return result, err
	}
	remaining := limit - len(ids)
	if remaining <= 0 {
		return result, nil
	}
	endedCount, endedUserIDs, err := r.endStaleQueuedMemberships(ctx, now, remaining)
	if err != nil {
		return result, err
	}
	result.Processed += endedCount
	result.EndedConsumerUserIDs = append(result.EndedConsumerUserIDs, endedUserIDs...)
	return result, nil
}

func (r *accountShareModeRepository) ListRecoverableUnavailableMembershipIDs(ctx context.Context, now time.Time, limit int) ([]int64, error) {
	if limit <= 0 {
		limit = service.AccountShareModeSeatBillingBatchSize
	}
	now = now.UTC()
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT m.id
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
		LEFT JOIN accounts a ON a.id = m.account_id
		WHERE m.status = $1
			AND m.deleted_at IS NULL
			AND %s
		ORDER BY COALESCE(m.last_request_at, m.joined_at) ASC, m.id ASC
		LIMIT $3
	`, accountShareMembershipRecoverablyUnavailableConditionSQL("$2")), service.AccountShareMembershipStatusActive, now, limit)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	membershipIDs := make([]int64, 0, limit)
	for rows.Next() {
		var membershipID int64
		if err := rows.Scan(&membershipID); err != nil {
			return nil, err
		}
		membershipIDs = append(membershipIDs, membershipID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return membershipIDs, nil
}

func (r *accountShareModeRepository) SuspendRecoverableUnavailableMembership(ctx context.Context, membershipID int64, unavailableAt time.Time) (*service.AccountShareMembership, *service.AccountShareSeatBillingResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	unavailableAt = unavailableAt.UTC()
	if err := r.lockRecoverableUnavailableMembershipResourcesInTx(ctx, tx, membershipID); errors.Is(err, sql.ErrNoRows) {
		return nil, nil, service.ErrAccountShareListingNotFound
	} else if err != nil {
		return nil, nil, err
	}
	membership, err := r.lockSeatBillingMembershipInTx(ctx, tx, membershipID, 0)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	if accountShareMembershipRecentlyActive(membership, unavailableAt) {
		return nil, nil, nil
	}
	recoverable, err := r.accountShareMembershipRecoverablyUnavailableInTx(ctx, tx, membership.ListingID, membership.AccountID, unavailableAt)
	if err != nil {
		return nil, nil, err
	}
	if !recoverable {
		return nil, nil, nil
	}
	membership, creditUserIDs, err := r.suspendActiveMembershipInTx(ctx, tx, membership, unavailableAt, unavailableAt)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	tx = nil
	return membership, accountShareMembershipBillingResult(membership, creditUserIDs), nil
}

// lockRecoverableUnavailableMembershipResourcesInTx serializes recoverable suspension
// with listing relists and account recovery. The lock order must remain
// listing -> account -> membership to match UpdateListing and queued activation.
func (r *accountShareModeRepository) lockRecoverableUnavailableMembershipResourcesInTx(ctx context.Context, tx *sql.Tx, membershipID int64) error {
	var listingID, accountID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT m.listing_id, m.account_id
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
		WHERE m.id = $1
			AND m.status = $2
			AND m.deleted_at IS NULL
		FOR UPDATE OF l
	`, membershipID, service.AccountShareMembershipStatusActive).Scan(&listingID, &accountID); err != nil {
		return err
	}

	var lockedAccountID int64
	return tx.QueryRowContext(ctx, `
		SELECT id
		FROM accounts
		WHERE id = $1
		FOR UPDATE
	`, accountID).Scan(&lockedAccountID)
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
	`, accountShareAccountPermanentlyUnavailableConditionSQL("$3"))
	rows, err := r.db.QueryContext(ctx, query, service.AccountShareMembershipStatusActive, accountID, endedAt, limit)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

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

func (r *accountShareModeRepository) endStaleQueuedMemberships(ctx context.Context, endedAt time.Time, limit int) (int, []int64, error) {
	if limit <= 0 {
		return 0, nil, nil
	}
	endedAt = endedAt.UTC()
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		WITH candidates AS (
			SELECT m.id
			FROM account_share_memberships m
			JOIN account_share_listings l ON l.id = m.listing_id
			LEFT JOIN accounts a ON a.id = m.account_id
			WHERE m.status = $1
				AND m.deleted_at IS NULL
				AND (
					l.deleted_at IS NOT NULL
					OR l.status = $2
					OR %s
				)
			ORDER BY m.joined_at ASC, m.id ASC
			LIMIT $4
			FOR UPDATE OF m SKIP LOCKED
		)
		UPDATE account_share_memberships m
		SET status = $5,
			ended_at = $3,
			ended_reason = $6,
			paid_until = $3,
			billed_until = $3,
			waiver_window_started_at = $3,
			waiver_window_usage_amount = 0,
			waiver_window_request_count = 0,
			waiver_window_last_request_at = NULL,
			dispatch_cooldown_until = NULL,
			updated_at = NOW()
		FROM candidates c
		WHERE m.id = c.id
		RETURNING m.consumer_user_id
	`, accountShareAccountPermanentlyUnavailableConditionSQL("$3")),
		service.AccountShareMembershipStatusQueued,
		service.AccountShareListingStatusDisabled,
		endedAt,
		limit,
		service.AccountShareMembershipStatusEnded,
		service.AccountShareMembershipEndReasonUnavailable,
	)
	if err != nil {
		return 0, nil, err
	}
	defer func() {
		_ = rows.Close()
	}()
	consumerIDs := make([]int64, 0, limit)
	for rows.Next() {
		var consumerID int64
		if err := rows.Scan(&consumerID); err != nil {
			return len(consumerIDs), consumerIDs, err
		}
		consumerIDs = append(consumerIDs, consumerID)
	}
	if err := rows.Err(); err != nil {
		return len(consumerIDs), consumerIDs, err
	}
	return len(consumerIDs), consumerIDs, nil
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
	defer func() {
		_ = rows.Close()
	}()

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
	unavailable, err := r.accountShareMembershipPermanentlyUnavailableInTx(ctx, tx, membership.AccountID, endedAt)
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
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT m.id
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
		LEFT JOIN accounts a ON a.id = m.account_id
		WHERE m.status = $1
			AND m.deleted_at IS NULL
			AND m.hourly_rate_snapshot > 0
			AND m.paid_until IS NOT NULL
			AND m.paid_until <= $2
			AND (m.idle_timeout_minutes <= 0 OR COALESCE(m.last_request_at, m.joined_at) + (m.idle_timeout_minutes * INTERVAL '1 minute') > $2)
			AND NOT %s
		ORDER BY m.paid_until ASC, m.id ASC
		LIMIT $3
	`, accountShareMembershipRecoverablyUnavailableConditionSQL("$2")), service.AccountShareMembershipStatusActive, now, limit)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

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

func (r *accountShareModeRepository) ProcessSeatWaiverCompensations(ctx context.Context, now time.Time, limit int) (*service.AccountShareSeatBillingResult, error) {
	if limit <= 0 {
		limit = service.AccountShareModeSeatWaiverCompensationBatchSize
	}
	now = now.UTC()
	delay := service.AccountShareModeSeatWaiverCompensationDelay
	if delay <= 0 {
		delay = service.AccountShareModeSeatWaiverSettlementGrace
	}
	readyBefore := now.Add(-delay)
	rows, err := r.db.QueryContext(ctx, `
		SELECT sc.id
		FROM account_share_mode_settlement_entries sc
		JOIN account_share_memberships m ON m.id = sc.membership_id
		WHERE sc.settlement_type = $1
			AND sc.hourly_charge > 0
			AND sc.period_started_at IS NOT NULL
			AND sc.period_ended_at IS NOT NULL
			AND sc.period_ended_at > sc.period_started_at
			AND sc.period_ended_at <= $4
			AND COALESCE(NULLIF(sc.waiver_minimum_snapshot, 0), m.hourly_fee_waiver_minimum_snapshot) > 0
			AND (
				sc.waiver_evaluated_at IS NULL
				OR EXISTS (
					SELECT 1
					FROM account_share_mode_settlement_entries e
					LEFT JOIN usage_logs ul ON ul.id = e.usage_log_id
					WHERE e.membership_id = sc.membership_id
						AND e.settlement_type = $3
						AND COALESCE(e.period_ended_at, COALESCE(ul.created_at, e.created_at)) >= sc.period_started_at
						AND COALESCE(
							e.period_started_at,
							COALESCE(ul.created_at, e.created_at) - (GREATEST(e.duration_ms, 0) * INTERVAL '1 millisecond')
						) < sc.period_ended_at
						AND (
							e.created_at > sc.waiver_evaluated_at
							OR COALESCE(ul.created_at, e.created_at) > sc.waiver_evaluated_at
						)
				)
			)
			AND NOT EXISTS (
				SELECT 1
				FROM account_share_mode_settlement_entries wr
				WHERE wr.membership_id = sc.membership_id
					AND wr.settlement_type = $2
					AND wr.period_started_at = sc.period_started_at
					AND wr.period_ended_at = sc.period_ended_at
			)
		ORDER BY sc.period_ended_at ASC, sc.id ASC
		LIMIT $5
	`, accountShareSeatSettlementTypeCharge, accountShareSeatSettlementTypeWaiverRefund, accountShareSeatSettlementTypeUsage, readyBefore, limit)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

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
	for _, id := range ids {
		item, err := r.processSeatWaiverCompensation(ctx, id, readyBefore)
		if err != nil {
			return result, err
		}
		if item == nil {
			continue
		}
		result.DebitUserIDs = append(result.DebitUserIDs, item.DebitUserIDs...)
		result.CreditUserIDs = append(result.CreditUserIDs, item.CreditUserIDs...)
	}
	return result, nil
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
	defer func() {
		_ = rows.Close()
	}()

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
	defer func() {
		_ = rows.Close()
	}()

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
	unavailable, err := r.accountShareMembershipPermanentlyUnavailableInTx(ctx, tx, membership.AccountID, now)
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
	recoverable, err := r.accountShareMembershipRecoverablyUnavailableInTx(ctx, tx, membership.ListingID, membership.AccountID, now)
	if err != nil {
		return nil, err
	}
	if recoverable {
		return nil, nil
	}

	settledUntil, settlementID, creditUserIDs, err := r.settleSeatChargeInTx(ctx, tx, membership, *membership.PaidUntil, false, now)
	if err != nil {
		return nil, err
	}
	if settledUntil != nil {
		settled := settledUntil.UTC()
		membership.BilledUntil = &settled
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
	canRenewSeat := prepayAmount > 0 && userBalance >= prepayAmount
	if !canRenewSeat {
		forcedUntil, forcedSettlementID, forcedCreditUserIDs, err := r.settleSeatChargeInTx(ctx, tx, membership, *membership.PaidUntil, true, now)
		if err != nil {
			return nil, err
		}
		if forcedUntil != nil {
			settledUntil = forcedUntil
			settled := forcedUntil.UTC()
			membership.BilledUntil = &settled
		}
		if forcedSettlementID > 0 {
			settlementID = forcedSettlementID
		}
		if len(forcedCreditUserIDs) > 0 {
			result.CreditUserIDs = append(result.CreditUserIDs, forcedCreditUserIDs...)
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
		}
		canRenewSeat = prepayAmount > 0 && userBalance >= prepayAmount
		if !canRenewSeat {
			if settledUntil == nil {
				settledUntil = membership.PaidUntil
			}
			err = tx.QueryRowContext(ctx, `
				UPDATE account_share_memberships
				SET status = $1,
					ended_at = $2,
					ended_reason = $3,
					billed_until = $2,
					paid_until = $2,
					waiver_window_started_at = $2,
					waiver_window_usage_amount = 0,
					waiver_window_request_count = 0,
					waiver_window_last_request_at = NULL,
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
	}

	newPaidUntil := membership.PaidUntil.Add(nextDuration)
	newBalance := userBalance - prepayAmount
	refType := accountShareModeSettlementRefType
	refID := nullablePositiveInt64(settlementID)
	if settlementID <= 0 {
		refType = accountShareSeatPrepayRefType
		refID = accountShareSeatPrepayRefID(membership.ID, newPaidUntil)
	}
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
		UserID:          membership.ConsumerUserID,
		Direction:       "debit",
		Amount:          decimalFromFloat(prepayAmount),
		Reason:          accountShareSeatPrepayReason,
		RefType:         refType,
		RefID:           refID,
		BalanceAfter:    decimalFromSignedFloat(newBalance),
		RequireInserted: true,
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
			billed_until = COALESCE($2::timestamptz, billed_until),
			waiver_window_started_at = CASE WHEN $2::timestamptz IS NULL THEN waiver_window_started_at ELSE $2::timestamptz END,
			waiver_window_usage_amount = CASE WHEN $2::timestamptz IS NULL THEN waiver_window_usage_amount ELSE 0 END,
			waiver_window_request_count = CASE WHEN $2::timestamptz IS NULL THEN waiver_window_request_count ELSE 0 END,
			waiver_window_last_request_at = CASE WHEN $2::timestamptz IS NULL THEN waiver_window_last_request_at ELSE NULL END,
			updated_at = NOW()
		WHERE id = $3
		RETURNING updated_at
	`, newPaidUntil, nullableTimePtr(settledUntil), membership.ID).Scan(&membership.UpdatedAt)
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

func (r *accountShareModeRepository) processSeatWaiverCompensation(ctx context.Context, seatChargeSettlementID int64, readyBefore time.Time) (*service.AccountShareSeatBillingResult, error) {
	if seatChargeSettlementID <= 0 {
		return nil, nil
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

	membership, charge, err := r.lockSeatChargeCompensationWindowInTx(ctx, tx, seatChargeSettlementID, readyBefore)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := lockAccountShareBillingUserInTx(ctx, tx, membership.ConsumerUserID); err != nil {
		return nil, err
	}
	chargeFloat, _ := charge.HourlyCharge.Float64()
	waiver, err := r.resolveSeatChargeWaiverInTx(ctx, tx, membership, charge.PeriodStart, charge.PeriodEnd, chargeFloat)
	if err != nil {
		return nil, err
	}
	if err := r.updateSeatChargeWaiverEvaluationInTx(ctx, tx, charge.SettlementID, waiver); err != nil {
		return nil, err
	}
	result := &service.AccountShareSeatBillingResult{}
	if waiver.Eligible {
		settlementID, err := r.refundSeatChargeWaiverAmountInTx(ctx, tx, membership, charge.PeriodStart, charge.PeriodEnd, charge.HourlyCharge, charge.Split, charge.SettlementID, waiver, map[string]any{
			"compensation":               true,
			"compensated_seat_charge_id": charge.SettlementID,
			"compensation_reason":        "late_usage_request_settlement",
		})
		if err != nil {
			return nil, err
		}
		if settlementID > 0 {
			debitUserIDs, err := r.reverseSeatChargeRevenueCreditsInTx(ctx, tx, membership, charge, settlementID, waiver)
			if err != nil {
				return nil, err
			}
			result.CreditUserIDs = append(result.CreditUserIDs, membership.ConsumerUserID)
			result.DebitUserIDs = append(result.DebitUserIDs, debitUserIDs...)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

type accountShareSeatChargeCompensationWindow struct {
	SettlementID int64
	PeriodStart  time.Time
	PeriodEnd    time.Time
	HourlyCharge decimal.Decimal
	Split        accountShareModeRevenueSplit
}

func (r *accountShareModeRepository) lockSeatChargeCompensationWindowInTx(ctx context.Context, tx *sql.Tx, settlementID int64, readyBefore time.Time) (*service.AccountShareMembership, accountShareSeatChargeCompensationWindow, error) {
	var charge accountShareSeatChargeCompensationWindow
	membership := &service.AccountShareMembership{}
	var policyID, inviterUserID sql.NullInt64
	var waiverMinimumText, hourlyChargeText, hourlyRateText string
	var ownerRatioText, inviteRatioText, platformRatioText string
	var ownerCreditText, inviteCreditText, platformCreditText string
	err := tx.QueryRowContext(ctx, `
		SELECT
			sc.id,
			sc.membership_id,
			sc.listing_id,
			sc.account_id,
			sc.owner_user_id,
			sc.consumer_user_id,
			sc.api_key_id,
			sc.hourly_charge::text,
			sc.owner_credit::text,
			sc.invite_credit::text,
			sc.platform_credit::text,
			sc.hourly_rate_snapshot::text,
			sc.policy_id,
			sc.policy_version,
			sc.owner_share_ratio_snapshot::text,
			sc.inviter_user_id,
			sc.invite_bound_at_snapshot,
			sc.invite_expires_at_snapshot,
			sc.invite_share_ratio_snapshot::text,
			sc.platform_share_ratio_snapshot::text,
			COALESCE(NULLIF(sc.waiver_minimum_snapshot, 0), m.hourly_fee_waiver_minimum_snapshot)::text,
			m.status,
			m.queue_rank,
			m.idle_timeout_minutes,
			m.joined_at,
			sc.period_started_at,
			sc.period_ended_at,
			m.created_at,
			m.updated_at
		FROM account_share_mode_settlement_entries sc
		JOIN account_share_memberships m ON m.id = sc.membership_id
		WHERE sc.id = $1
			AND sc.settlement_type = $2
			AND sc.hourly_charge > 0
			AND sc.period_started_at IS NOT NULL
			AND sc.period_ended_at IS NOT NULL
			AND sc.period_ended_at > sc.period_started_at
			AND sc.period_ended_at <= $3
			AND COALESCE(NULLIF(sc.waiver_minimum_snapshot, 0), m.hourly_fee_waiver_minimum_snapshot) > 0
			AND NOT EXISTS (
				SELECT 1
				FROM account_share_mode_settlement_entries wr
				WHERE wr.membership_id = sc.membership_id
					AND wr.settlement_type = $4
					AND wr.period_started_at = sc.period_started_at
					AND wr.period_ended_at = sc.period_ended_at
			)
		FOR UPDATE OF sc
	`, settlementID, accountShareSeatSettlementTypeCharge, readyBefore.UTC(), accountShareSeatSettlementTypeWaiverRefund).Scan(
		&charge.SettlementID,
		&membership.ID,
		&membership.ListingID,
		&membership.AccountID,
		&membership.OwnerUserID,
		&membership.ConsumerUserID,
		&membership.APIKeyID,
		&hourlyChargeText,
		&ownerCreditText,
		&inviteCreditText,
		&platformCreditText,
		&hourlyRateText,
		&policyID,
		&charge.Split.PolicyVersion,
		&ownerRatioText,
		&inviterUserID,
		&charge.Split.Invite.BoundAt,
		&charge.Split.Invite.ExpiresAt,
		&inviteRatioText,
		&platformRatioText,
		&waiverMinimumText,
		&membership.Status,
		&membership.QueueRank,
		&membership.IdleTimeoutMinutes,
		&membership.JoinedAt,
		&charge.PeriodStart,
		&charge.PeriodEnd,
		&membership.CreatedAt,
		&membership.UpdatedAt,
	)
	if err != nil {
		return nil, charge, err
	}
	charge.HourlyCharge, err = decimal.NewFromString(strings.TrimSpace(hourlyChargeText))
	if err != nil {
		return nil, charge, err
	}
	charge.Split.OwnerCredit, err = decimal.NewFromString(strings.TrimSpace(ownerCreditText))
	if err != nil {
		return nil, charge, err
	}
	charge.Split.InviteCredit, err = decimal.NewFromString(strings.TrimSpace(inviteCreditText))
	if err != nil {
		return nil, charge, err
	}
	charge.Split.PlatformCredit, err = decimal.NewFromString(strings.TrimSpace(platformCreditText))
	if err != nil {
		return nil, charge, err
	}
	charge.Split.OwnerRatio, err = decimal.NewFromString(strings.TrimSpace(ownerRatioText))
	if err != nil {
		return nil, charge, err
	}
	charge.Split.InviteRatio, err = decimal.NewFromString(strings.TrimSpace(inviteRatioText))
	if err != nil {
		return nil, charge, err
	}
	charge.Split.PlatformRatio, err = decimal.NewFromString(strings.TrimSpace(platformRatioText))
	if err != nil {
		return nil, charge, err
	}
	if policyID.Valid {
		charge.Split.PolicyID = &policyID.Int64
	}
	if inviterUserID.Valid {
		charge.Split.Invite.InviterUserID = inviterUserID.Int64
	}
	hourlyRate, err := decimal.NewFromString(strings.TrimSpace(hourlyRateText))
	if err != nil {
		return nil, charge, err
	}
	waiverMinimum, err := decimal.NewFromString(strings.TrimSpace(waiverMinimumText))
	if err != nil {
		return nil, charge, err
	}
	membership.HourlyRateSnapshot, _ = hourlyRate.Float64()
	membership.HourlyFeeWaiverMinimumSnapshot, _ = waiverMinimum.Float64()
	periodEnd := charge.PeriodEnd.UTC()
	membership.PaidUntil = &periodEnd
	return membership, charge, nil
}

func (r *accountShareModeRepository) updateSeatChargeWaiverEvaluationInTx(ctx context.Context, tx *sql.Tx, settlementID int64, waiver accountShareSeatChargeWaiver) error {
	if settlementID <= 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE account_share_mode_settlement_entries
		SET waiver_minimum_snapshot = $2::numeric,
			waiver_required_amount = $3::numeric,
			waiver_usage_amount = $4::numeric,
			waiver_evaluated_at = NOW()
		WHERE id = $1
			AND settlement_type = $5
	`, settlementID, waiver.Minimum.StringFixed(8), waiver.Required.StringFixed(10), waiver.Usage.StringFixed(10), accountShareSeatSettlementTypeCharge)
	return err
}

func (r *accountShareModeRepository) reverseSeatChargeRevenueCreditsInTx(ctx context.Context, tx *sql.Tx, membership *service.AccountShareMembership, charge accountShareSeatChargeCompensationWindow, refundSettlementID int64, waiver accountShareSeatChargeWaiver) ([]int64, error) {
	if membership == nil {
		return nil, nil
	}
	debitUserIDs := make([]int64, 0, 2)
	if charge.Split.OwnerCredit.GreaterThan(decimal.Zero) {
		var newBalance float64
		err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1::numeric,
			updated_at = NOW()
		WHERE id = $2
			AND deleted_at IS NULL
		RETURNING balance
		`, charge.Split.OwnerCredit.StringFixed(10), membership.OwnerUserID).Scan(&newBalance)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrUserNotFound
		}
		if err != nil {
			return nil, err
		}
		if err := insertUserBalanceLedger(ctx, tx, userBalanceLedgerInput{
			UserID:          membership.OwnerUserID,
			Direction:       "debit",
			Amount:          charge.Split.OwnerCredit,
			Reason:          accountShareSeatWaiverRefundReason,
			RefType:         accountShareModeSettlementRefType,
			RefID:           nullablePositiveInt64(refundSettlementID),
			BalanceAfter:    decimalFromSignedFloat(newBalance),
			RequireInserted: true,
			Metadata:        accountShareSeatWaiverReversalMetadata(membership, charge, refundSettlementID, waiver),
		}); err != nil {
			return nil, err
		}
		debitUserIDs = append(debitUserIDs, membership.OwnerUserID)
	}
	if charge.Split.Invite.InviterUserID > 0 && charge.Split.InviteCredit.GreaterThan(decimal.Zero) {
		inviterUserID := charge.Split.Invite.InviterUserID
		var newBalance float64
		err := tx.QueryRowContext(ctx, `
			UPDATE users
			SET balance = balance - $1::numeric,
				updated_at = NOW()
			WHERE id = $2
				AND deleted_at IS NULL
			RETURNING balance
		`, charge.Split.InviteCredit.StringFixed(10), inviterUserID).Scan(&newBalance)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrUserNotFound
		}
		if err != nil {
			return nil, err
		}
		metadata := accountShareSeatWaiverReversalMetadata(membership, charge, refundSettlementID, waiver)
		metadata["invite_credit_reversed"] = charge.Split.InviteCredit.StringFixed(10)
		if err := insertUserBalanceLedger(ctx, tx, userBalanceLedgerInput{
			UserID:          inviterUserID,
			Direction:       "debit",
			Amount:          charge.Split.InviteCredit,
			Reason:          accountShareSeatInviteWaiverRefundReason,
			RefType:         accountShareModeSettlementRefType,
			RefID:           nullablePositiveInt64(refundSettlementID),
			BalanceAfter:    decimalFromSignedFloat(newBalance),
			RequireInserted: true,
			Metadata:        metadata,
		}); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE user_affiliates
			SET aff_history_quota = aff_history_quota - $1::numeric,
				updated_at = NOW()
			WHERE user_id = $2
		`, charge.Split.InviteCredit.StringFixed(10), inviterUserID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_affiliate_ledger (user_id, action, amount, source_user_id, created_at, updated_at)
			VALUES ($1, 'reverse', $2::numeric, $3, NOW(), NOW())
		`, inviterUserID, charge.Split.InviteCredit.StringFixed(10), membership.ConsumerUserID); err != nil {
			return nil, err
		}
		debitUserIDs = appendUniqueInt64(debitUserIDs, inviterUserID)
	}
	return debitUserIDs, nil
}

func accountShareSeatWaiverReversalMetadata(membership *service.AccountShareMembership, charge accountShareSeatChargeCompensationWindow, refundSettlementID int64, waiver accountShareSeatChargeWaiver) map[string]any {
	return map[string]any{
		"listing_id":                 membership.ListingID,
		"account_id":                 membership.AccountID,
		"membership_id":              membership.ID,
		"settlement_id":              refundSettlementID,
		"compensated_seat_charge_id": charge.SettlementID,
		"consumer_user_id":           membership.ConsumerUserID,
		"owner_credit_reversed":      charge.Split.OwnerCredit.StringFixed(10),
		"invite_credit_reversed":     charge.Split.InviteCredit.StringFixed(10),
		"platform_credit_reversed":   charge.Split.PlatformCredit.StringFixed(10),
		"waiver_minimum":             waiver.Minimum.StringFixed(8),
		"waiver_required":            waiver.Required.StringFixed(10),
		"waiver_usage":               waiver.Usage.StringFixed(10),
		"settlement_type":            accountShareSeatSettlementTypeWaiverRefund,
		"period_started":             charge.PeriodStart.Format(time.RFC3339),
		"period_ended":               charge.PeriodEnd.Format(time.RFC3339),
		"compensation":               true,
	}
}

func (r *accountShareModeRepository) endSeatBillingMembershipInTx(ctx context.Context, tx *sql.Tx, membership *service.AccountShareMembership, endedAt time.Time, reason string) (*service.AccountShareSeatBillingResult, error) {
	if membership == nil || membership.ID <= 0 {
		return nil, nil
	}
	endedAt = endedAt.UTC()
	settledUntil, _, creditUserIDs, err := r.settleSeatChargeInTx(ctx, tx, membership, endedAt, true, endedAt)
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
			waiver_window_started_at = $4,
			waiver_window_usage_amount = 0,
			waiver_window_request_count = 0,
			waiver_window_last_request_at = NULL,
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

func (r *accountShareModeRepository) accountShareAccountPermanentlyUnavailableInTx(ctx context.Context, tx *sql.Tx, accountID int64, now time.Time) (bool, error) {
	if accountID <= 0 {
		return false, nil
	}
	query := fmt.Sprintf(`
		SELECT EXISTS (
			SELECT 1
			FROM accounts a
			WHERE a.id = $1
				AND %s
		) OR NOT EXISTS (
			SELECT 1
			FROM accounts a
			WHERE a.id = $1
		)
	`, accountShareAccountPermanentlyUnavailableConditionSQL("$2"))
	var unavailable bool
	if err := tx.QueryRowContext(ctx, query, accountID, now.UTC()).Scan(&unavailable); err != nil {
		return false, err
	}
	if unavailable {
		logger.LegacyPrintf("repository.account_share_mode", "account share permanently unavailable matched: account_id=%d now=%s details=%s", accountID, now.UTC().Format(time.RFC3339), r.accountShareAccountUnavailableDetailsInTx(ctx, tx, accountID, now))
	}
	return unavailable, nil
}

func (r *accountShareModeRepository) accountShareMembershipPermanentlyUnavailableInTx(ctx context.Context, tx *sql.Tx, accountID int64, now time.Time) (bool, error) {
	if accountID <= 0 {
		return false, nil
	}
	query := fmt.Sprintf(`
		SELECT EXISTS (
			SELECT 1
			FROM account_share_listings l
			LEFT JOIN accounts a ON a.id = l.account_id
			WHERE l.account_id = $1
				AND %s
		) OR NOT EXISTS (
			SELECT 1
			FROM accounts a
			WHERE a.id = $1
		)
	`, accountShareMembershipPermanentlyUnavailableConditionSQL("$2"))
	var unavailable bool
	if err := tx.QueryRowContext(ctx, query, accountID, now.UTC()).Scan(&unavailable); err != nil {
		return false, err
	}
	if unavailable {
		logger.LegacyPrintf("repository.account_share_mode", "account share membership permanently unavailable matched: account_id=%d now=%s details=%s", accountID, now.UTC().Format(time.RFC3339), r.accountShareAccountUnavailableDetailsInTx(ctx, tx, accountID, now))
	}
	return unavailable, nil
}

func (r *accountShareModeRepository) accountShareMembershipRecoverablyUnavailableInTx(ctx context.Context, tx *sql.Tx, listingID, accountID int64, now time.Time) (bool, error) {
	if listingID <= 0 || accountID <= 0 {
		return false, nil
	}
	query := fmt.Sprintf(`
		SELECT EXISTS (
			SELECT 1
			FROM account_share_listings l
			LEFT JOIN accounts a ON a.id = l.account_id
			WHERE l.id = $1
				AND l.account_id = $2
				AND %s
		)
	`, accountShareMembershipRecoverablyUnavailableConditionSQL("$3"))
	var unavailable bool
	if err := tx.QueryRowContext(ctx, query, listingID, accountID, now.UTC()).Scan(&unavailable); err != nil {
		return false, err
	}
	if unavailable {
		logger.LegacyPrintf("repository.account_share_mode", "account share membership recoverably unavailable matched: membership_listing_id=%d account_id=%d now=%s", listingID, accountID, now.UTC().Format(time.RFC3339))
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
			m.status, m.queue_rank, m.hourly_rate_snapshot, m.hourly_fee_waiver_minimum_snapshot, m.idle_timeout_minutes,
			m.joined_at, m.last_request_at, m.ended_at, m.ended_reason, m.paid_until, m.billed_until,
			m.waiver_window_started_at, m.waiver_window_usage_amount, m.waiver_window_request_count, m.waiver_window_last_request_at,
			m.dispatch_failed_at, m.dispatch_cooldown_until, m.created_at, m.updated_at
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

	membership, err := scanAccountShareMembership(tx.QueryRowContext(ctx, query, args...))
	if err != nil {
		return nil, err
	}
	return membership, nil
}

func (r *accountShareModeRepository) settleSeatChargeInTx(ctx context.Context, tx *sql.Tx, membership *service.AccountShareMembership, at time.Time, forceClose bool, settleAt time.Time) (*time.Time, int64, []int64, error) {
	if membership == nil || membership.HourlyRateSnapshot <= 0 || membership.PaidUntil == nil {
		return nil, 0, nil, nil
	}
	start := membership.JoinedAt
	if membership.BilledUntil != nil {
		start = *membership.BilledUntil
	}
	start = start.UTC()
	targetEnd := at.UTC()
	if membership.PaidUntil.Before(targetEnd) {
		targetEnd = membership.PaidUntil.UTC()
	}
	settleAt = settleAt.UTC()
	if settleAt.IsZero() {
		settleAt = time.Now().UTC()
	}
	if !targetEnd.After(start) {
		return &start, 0, nil, nil
	}
	if err := lockAccountShareBillingUserInTx(ctx, tx, membership.ConsumerUserID); err != nil {
		return nil, 0, nil, err
	}

	if membership.HourlyFeeWaiverMinimumSnapshot <= 0 {
		settlementID, creditUserIDs, err := r.settleSeatChargeWindowInTx(ctx, tx, membership, start, targetEnd)
		if err != nil {
			return nil, 0, nil, err
		}
		return &targetEnd, settlementID, creditUserIDs, nil
	}

	windowMax := service.AccountShareModeSeatWaiverWindowMax
	if windowMax <= 0 {
		windowMax = time.Hour
	}
	cursor := start
	var settledUntil *time.Time
	var lastSettlementID int64
	creditUserIDs := make([]int64, 0, 2)
	for cursor.Before(targetEnd) {
		windowEnd := cursor.Add(windowMax)
		end := targetEnd
		if windowEnd.Before(end) {
			end = windowEnd
		}
		if !forceClose && end.Before(windowEnd) {
			break
		}
		if !forceClose && !accountShareSeatWaiverWindowReadyAt(settleAt, end) {
			break
		}
		settlementID, windowCreditUserIDs, err := r.settleSeatChargeWindowInTx(ctx, tx, membership, cursor, end)
		if err != nil {
			return nil, 0, nil, err
		}
		if settlementID > 0 {
			lastSettlementID = settlementID
		}
		creditUserIDs = append(creditUserIDs, windowCreditUserIDs...)
		settled := end.UTC()
		settledUntil = &settled
		cursor = end
	}
	if settledUntil == nil {
		return nil, 0, nil, nil
	}
	return settledUntil, lastSettlementID, creditUserIDs, nil
}

func lockAccountShareBillingUserInTx(ctx context.Context, tx *sql.Tx, userID int64) error {
	if tx == nil || userID <= 0 {
		return service.ErrUserNotFound
	}
	var lockedUserID int64
	err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM users
		WHERE id = $1
			AND deleted_at IS NULL
		FOR UPDATE
	`, userID).Scan(&lockedUserID)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrUserNotFound
	}
	return err
}

func (r *accountShareModeRepository) settleSeatChargeWindowInTx(ctx context.Context, tx *sql.Tx, membership *service.AccountShareMembership, start, end time.Time) (int64, []int64, error) {
	if membership == nil || !end.After(start) {
		return 0, nil, nil
	}
	duration := end.Sub(start)
	charge := accountShareSeatCharge(membership.HourlyRateSnapshot, duration)
	if charge <= 0 {
		return 0, nil, nil
	}
	waiver, err := r.resolveSeatChargeWaiverInTx(ctx, tx, membership, start, end, charge)
	if err != nil {
		return 0, nil, err
	}
	if waiver.Eligible {
		settlementID, err := r.refundSeatChargeWaiverInTx(ctx, tx, membership, start, end, charge, waiver)
		if err != nil {
			return 0, nil, err
		}
		return settlementID, []int64{membership.ConsumerUserID}, nil
	}
	totalCharge := decimalFromFloat(charge)
	split, err := resolveAccountShareModeRevenueSplitInTx(ctx, tx, membership.ConsumerUserID, totalCharge, end)
	if err != nil {
		return 0, nil, err
	}
	settlementID, err := r.insertSeatSettlementInTx(ctx, tx, membership, accountShareSeatSettlementTypeCharge, start, end, charge, 0, split, &waiver)
	if err != nil {
		return 0, nil, err
	}
	creditUserIDs := make([]int64, 0, 2)
	if split.OwnerCredit.GreaterThan(decimal.Zero) {
		newBalance, err := creditUsageBillingBalance(ctx, tx, membership.OwnerUserID, split.OwnerCredit)
		if err != nil {
			return 0, nil, err
		}
		if err := insertUserBalanceLedger(ctx, tx, userBalanceLedgerInput{
			UserID:          membership.OwnerUserID,
			Direction:       "credit",
			Amount:          split.OwnerCredit,
			Reason:          accountShareSeatIncomeReason,
			RefType:         accountShareModeSettlementRefType,
			RefID:           nullablePositiveInt64(settlementID),
			BalanceAfter:    decimalFromSignedFloat(newBalance),
			RequireInserted: true,
			Metadata: map[string]any{
				"listing_id":       membership.ListingID,
				"account_id":       membership.AccountID,
				"membership_id":    membership.ID,
				"settlement_id":    settlementID,
				"consumer_user_id": membership.ConsumerUserID,
				"total_charge":     totalCharge.StringFixed(10),
				"owner_ratio":      split.OwnerRatio.StringFixed(8),
				"invite_ratio":     split.InviteRatio.StringFixed(8),
				"platform_ratio":   split.PlatformRatio.StringFixed(8),
				"settlement_type":  accountShareSeatSettlementTypeCharge,
				"period_started":   start.Format(time.RFC3339),
				"period_ended":     end.Format(time.RFC3339),
			},
		}); err != nil {
			return 0, nil, err
		}
		creditUserIDs = append(creditUserIDs, membership.OwnerUserID)
	}
	if split.Invite.InviterUserID > 0 && split.InviteCredit.GreaterThan(decimal.Zero) {
		if err := creditAccountShareModeInviteBalance(ctx, tx, membership, settlementID, split.Invite.InviterUserID, split.InviteCredit); err != nil {
			return 0, nil, err
		}
		creditUserIDs = appendUniqueInt64(creditUserIDs, split.Invite.InviterUserID)
	}
	return settlementID, creditUserIDs, nil
}

func creditAccountShareModeInviteBalance(ctx context.Context, tx *sql.Tx, membership *service.AccountShareMembership, settlementID, inviterUserID int64, amount decimal.Decimal) error {
	if membership == nil || settlementID <= 0 {
		return nil
	}
	return creditInviteShareBalanceEntry(ctx, tx, inviteShareBalanceCreditInput{
		InviterUserID:  inviterUserID,
		ConsumerUserID: membership.ConsumerUserID,
		Amount:         amount,
		RefType:        accountShareModeSettlementRefType,
		RefID:          nullablePositiveInt64(settlementID),
		Metadata: map[string]any{
			"api_key_id":       membership.APIKeyID,
			"account_id":       membership.AccountID,
			"listing_id":       membership.ListingID,
			"membership_id":    membership.ID,
			"settlement_id":    settlementID,
			"consumer_user_id": membership.ConsumerUserID,
			"settlement_type":  accountShareSeatSettlementTypeCharge,
		},
	})
}

func appendUniqueInt64(values []int64, value int64) []int64 {
	if value <= 0 {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func accountShareMembershipBillingResult(membership *service.AccountShareMembership, creditUserIDs []int64) *service.AccountShareSeatBillingResult {
	result := &service.AccountShareSeatBillingResult{}
	if membership == nil {
		return result
	}
	result.DebitUserIDs = appendUniqueInt64(result.DebitUserIDs, membership.ConsumerUserID)
	result.CreditUserIDs = appendUniqueInt64(result.CreditUserIDs, membership.OwnerUserID)
	for _, userID := range creditUserIDs {
		result.CreditUserIDs = appendUniqueInt64(result.CreditUserIDs, userID)
	}
	result.EndedConsumerUserIDs = appendUniqueInt64(result.EndedConsumerUserIDs, membership.ConsumerUserID)
	return result
}

type accountShareSeatChargeWaiver struct {
	Eligible bool
	Minimum  decimal.Decimal
	Required decimal.Decimal
	Usage    decimal.Decimal
}

type accountShareModeRevenueSplit struct {
	PolicyID       *int64
	PolicyVersion  int
	OwnerRatio     decimal.Decimal
	Invite         accountInviteSnapshot
	InviteRatio    decimal.Decimal
	PlatformRatio  decimal.Decimal
	OwnerCredit    decimal.Decimal
	InviteCredit   decimal.Decimal
	PlatformCredit decimal.Decimal
}

func resolveAccountShareModeRevenueSplitInTx(ctx context.Context, tx *sql.Tx, consumerUserID int64, totalCharge decimal.Decimal, occurredAt time.Time) (accountShareModeRevenueSplit, error) {
	split := accountShareModeRevenueSplit{PlatformRatio: decimal.NewFromInt(1)}
	if tx == nil || consumerUserID <= 0 || totalCharge.LessThanOrEqual(decimal.Zero) {
		return split, nil
	}
	policy, err := resolveEnabledGlobalAccountSharePolicy(ctx, tx)
	if err != nil {
		return split, err
	}
	configuredInviteRatio := decimal.Zero
	if policy != nil {
		policyID := policy.ID
		split.PolicyID = &policyID
		split.PolicyVersion = policy.Version
		split.OwnerRatio, configuredInviteRatio, _ = accountShareModeSettlementRatios(policy.OwnerShareRatio, policy.InviteShareRatio)
	}
	split.Invite, err = resolveEligibleAccountShareInvite(ctx, tx, consumerUserID, configuredInviteRatio, occurredAt)
	if err != nil {
		return accountShareModeRevenueSplit{}, err
	}
	if split.Invite.InviterUserID > 0 {
		split.InviteRatio = configuredInviteRatio
	}
	split.PlatformRatio = decimal.NewFromInt(1).Sub(split.OwnerRatio).Sub(split.InviteRatio)
	if split.PlatformRatio.IsNegative() {
		return accountShareModeRevenueSplit{}, fmt.Errorf("account share mode settlement ratios exceed 1")
	}
	split.OwnerCredit = totalCharge.Mul(split.OwnerRatio).Round(10)
	if split.OwnerCredit.GreaterThan(totalCharge) {
		split.OwnerCredit = totalCharge
	}
	remaining := totalCharge.Sub(split.OwnerCredit)
	split.InviteCredit = totalCharge.Mul(split.InviteRatio).Round(10)
	if split.InviteCredit.GreaterThan(remaining) {
		split.InviteCredit = remaining
	}
	split.PlatformCredit = remaining.Sub(split.InviteCredit)
	return split, nil
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
	usage, err := r.accountShareWaiverWindowUsageInTx(ctx, tx, membership, periodStart, periodEnd)
	if err != nil {
		return waiver, err
	}
	waiver.Minimum = minimum
	waiver.Required = required
	waiver.Usage = usage
	waiver.Eligible = usage.GreaterThanOrEqual(required)
	return waiver, nil
}

type accountShareModeUsageStat struct {
	Total         decimal.Decimal
	RequestCount  int64
	LastRequestAt *time.Time
}

func (r *accountShareModeRepository) accountShareWaiverWindowUsageInTx(ctx context.Context, tx *sql.Tx, membership *service.AccountShareMembership, windowStart, windowEnd time.Time) (decimal.Decimal, error) {
	if tx == nil || membership == nil || membership.ID <= 0 || !windowEnd.After(windowStart) {
		return decimal.Zero, nil
	}
	windowStart = windowStart.UTC()
	windowEnd = windowEnd.UTC()
	var usageText string
	err := tx.QueryRowContext(ctx, `
		WITH usage_rows AS (
			SELECT
				e.total_charge,
				COALESCE(
					e.period_started_at,
					COALESCE(ul.created_at, e.created_at) - (GREATEST(e.duration_ms, 0) * INTERVAL '1 millisecond')
				) AS request_started_at,
				COALESCE(e.period_ended_at, COALESCE(ul.created_at, e.created_at)) AS request_ended_at
			FROM account_share_mode_settlement_entries e
			LEFT JOIN usage_logs ul ON ul.id = e.usage_log_id
			WHERE e.membership_id = $1
				AND e.settlement_type = 'usage_request'
				AND COALESCE(e.period_ended_at, COALESCE(ul.created_at, e.created_at)) >= $2
				AND COALESCE(
					e.period_started_at,
					COALESCE(ul.created_at, e.created_at) - (GREATEST(e.duration_ms, 0) * INTERVAL '1 millisecond')
				) < $3
		)
		SELECT COALESCE(SUM(
			CASE
				WHEN request_ended_at > request_started_at
					AND LEAST(request_ended_at, $3::timestamptz) > GREATEST(request_started_at, $2::timestamptz)
				THEN total_charge
					* EXTRACT(EPOCH FROM (LEAST(request_ended_at, $3::timestamptz) - GREATEST(request_started_at, $2::timestamptz)))::numeric
					/ NULLIF(EXTRACT(EPOCH FROM (request_ended_at - request_started_at))::numeric, 0)
				WHEN request_ended_at = request_started_at
					AND request_ended_at >= $2::timestamptz
					AND request_ended_at < $3::timestamptz
				THEN total_charge
				ELSE 0
			END
		), 0)::text
		FROM usage_rows
	`, membership.ID, windowStart, windowEnd).Scan(&usageText)
	if err != nil {
		return decimal.Zero, err
	}
	usage, err := decimal.NewFromString(strings.TrimSpace(usageText))
	if err != nil {
		return decimal.Zero, err
	}
	if usage.IsNegative() {
		return decimal.Zero, nil
	}
	return usage.Round(10), nil
}

func (r *accountShareModeRepository) refundSeatChargeWaiverInTx(ctx context.Context, tx *sql.Tx, membership *service.AccountShareMembership, periodStart, periodEnd time.Time, charge float64, waiver accountShareSeatChargeWaiver) (int64, error) {
	if membership == nil || charge <= 0 || !periodEnd.After(periodStart) {
		return 0, nil
	}
	refund := decimalFromFloat(charge)
	return r.refundSeatChargeWaiverAmountInTx(ctx, tx, membership, periodStart, periodEnd, refund, accountShareModeRevenueSplit{}, 0, waiver, nil)
}

func (r *accountShareModeRepository) refundSeatChargeWaiverAmountInTx(ctx context.Context, tx *sql.Tx, membership *service.AccountShareMembership, periodStart, periodEnd time.Time, refund decimal.Decimal, reversal accountShareModeRevenueSplit, reversalOfSettlementID int64, waiver accountShareSeatChargeWaiver, extraMetadata map[string]any) (int64, error) {
	if membership == nil || !periodEnd.After(periodStart) {
		return 0, nil
	}
	if refund.LessThanOrEqual(decimal.Zero) {
		return 0, nil
	}
	settlementID, err := r.insertSeatWaiverSettlementInTx(ctx, tx, membership, periodStart, periodEnd, refund, reversal, reversalOfSettlementID, waiver)
	if err != nil {
		return 0, err
	}
	if settlementID <= 0 {
		return 0, nil
	}
	newBalance, err := creditUsageBillingBalance(ctx, tx, membership.ConsumerUserID, refund)
	if err != nil {
		return 0, err
	}
	metadata := map[string]any{
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
	}
	for key, value := range extraMetadata {
		metadata[key] = value
	}
	if err := insertUserBalanceLedger(ctx, tx, userBalanceLedgerInput{
		UserID:          membership.ConsumerUserID,
		Direction:       "credit",
		Amount:          refund,
		Reason:          accountShareSeatWaiverRefundReason,
		RefType:         accountShareModeSettlementRefType,
		RefID:           nullablePositiveInt64(settlementID),
		BalanceAfter:    decimalFromSignedFloat(newBalance),
		RequireInserted: true,
		Metadata:        metadata,
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
	settlementID, err := r.insertSeatSettlementInTx(ctx, tx, membership, accountShareSeatSettlementTypeRefund, endedAt, *membership.PaidUntil, 0, refund, accountShareModeRevenueSplit{}, nil)
	if err != nil {
		return err
	}
	newBalance, err := creditUsageBillingBalance(ctx, tx, membership.ConsumerUserID, decimalFromFloat(refund))
	if err != nil {
		return err
	}
	if err := insertUserBalanceLedger(ctx, tx, userBalanceLedgerInput{
		UserID:          membership.ConsumerUserID,
		Direction:       "credit",
		Amount:          decimalFromFloat(refund),
		Reason:          accountShareSeatRefundReason,
		RefType:         accountShareModeSettlementRefType,
		RefID:           settlementID,
		BalanceAfter:    decimalFromSignedFloat(newBalance),
		RequireInserted: true,
		Metadata: map[string]any{
			"listing_id":      membership.ListingID,
			"account_id":      membership.AccountID,
			"membership_id":   membership.ID,
			"settlement_id":   settlementID,
			"hourly_rate":     membership.HourlyRateSnapshot,
			"duration_ms":     int(duration.Milliseconds()),
			"refund_until":    membership.PaidUntil.Format(time.RFC3339),
			"settlement_type": accountShareSeatSettlementTypeRefund,
			"seat_billing":    true,
		},
	}); err != nil {
		return err
	}
	return nil
}

func (r *accountShareModeRepository) insertSeatSettlementInTx(ctx context.Context, tx *sql.Tx, membership *service.AccountShareMembership, settlementType string, periodStart, periodEnd time.Time, charge float64, refund float64, split accountShareModeRevenueSplit, waiver *accountShareSeatChargeWaiver) (int64, error) {
	if membership == nil {
		return 0, nil
	}
	durationMs := int(periodEnd.Sub(periodStart).Milliseconds())
	if durationMs < 0 {
		durationMs = 0
	}
	waiverMinimum := decimal.Zero
	waiverRequired := decimal.Zero
	waiverUsage := decimal.Zero
	if waiver != nil {
		waiverMinimum = waiver.Minimum
		waiverRequired = waiver.Required
		waiverUsage = waiver.Usage
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
			policy_id,
			policy_version,
			owner_share_ratio_snapshot,
			inviter_user_id,
			invite_bound_at_snapshot,
			invite_expires_at_snapshot,
			invite_share_ratio_snapshot,
			invite_credit,
			platform_share_ratio_snapshot,
			duration_ms,
			settlement_type,
			period_started_at,
			period_ended_at,
			refund_amount,
			waiver_minimum_snapshot,
			waiver_required_amount,
			waiver_usage_amount,
			waiver_evaluated_at,
			created_at
		)
		VALUES (
			NULL, $1, $2, $3, $4, $5, $6,
			0, $7::numeric, $7::numeric, $8::numeric, $9::numeric,
			1, $10::numeric, $11, $12, $13::numeric,
			$14, $15, $16, $17::numeric, $18::numeric, $19::numeric,
			$20, $21::varchar, $22, $23, $24::numeric, $25::numeric, $26::numeric, $27::numeric,
			CASE WHEN $21::varchar = 'seat_charge' THEN NOW() ELSE NULL END,
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
		decimalFromFloat(charge).StringFixed(10),
		split.OwnerCredit.StringFixed(10),
		split.PlatformCredit.StringFixed(10),
		decimalFromFloat(membership.HourlyRateSnapshot).StringFixed(8),
		nullablePtrInt64(split.PolicyID),
		split.PolicyVersion,
		split.OwnerRatio.StringFixed(8),
		nullablePositiveInt64(split.Invite.InviterUserID),
		nullableTime(split.Invite.BoundAt),
		nullableTime(split.Invite.ExpiresAt),
		split.InviteRatio.StringFixed(8),
		split.InviteCredit.StringFixed(10),
		split.PlatformRatio.StringFixed(8),
		durationMs,
		settlementType,
		periodStart,
		periodEnd,
		decimalFromFloat(refund).StringFixed(10),
		waiverMinimum.StringFixed(8),
		waiverRequired.StringFixed(10),
		waiverUsage.StringFixed(10),
	).Scan(&settlementID)
	return settlementID, err
}

func (r *accountShareModeRepository) insertSeatWaiverSettlementInTx(ctx context.Context, tx *sql.Tx, membership *service.AccountShareMembership, periodStart, periodEnd time.Time, refund decimal.Decimal, reversal accountShareModeRevenueSplit, reversalOfSettlementID int64, waiver accountShareSeatChargeWaiver) (int64, error) {
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
			policy_id,
			policy_version,
			owner_share_ratio_snapshot,
			inviter_user_id,
			invite_bound_at_snapshot,
			invite_expires_at_snapshot,
			invite_share_ratio_snapshot,
			invite_credit,
			platform_share_ratio_snapshot,
			duration_ms,
			settlement_type,
			period_started_at,
			period_ended_at,
			refund_amount,
			waiver_minimum_snapshot,
			waiver_required_amount,
			waiver_usage_amount,
			reversal_of_settlement_id,
			created_at
		)
		VALUES (
			NULL, $1, $2, $3, $4, $5, $6,
			0, 0, 0, $7::numeric, $8::numeric,
			1, $9::numeric, $10, $11, $12::numeric,
			$13, $14, $15, $16::numeric, $17::numeric, $18::numeric,
			$19, $20, $21, $22, $23::numeric,
			$24::numeric, $25::numeric, $26::numeric, $27,
			NOW()
		)
		ON CONFLICT (membership_id, period_started_at, period_ended_at)
			WHERE settlement_type = 'seat_waiver_refund'
			DO NOTHING
		RETURNING id
	`,
		membership.ID,
		membership.ListingID,
		membership.AccountID,
		membership.OwnerUserID,
		membership.ConsumerUserID,
		membership.APIKeyID,
		reversal.OwnerCredit.StringFixed(10),
		reversal.PlatformCredit.StringFixed(10),
		decimalFromFloat(membership.HourlyRateSnapshot).StringFixed(8),
		nullablePtrInt64(reversal.PolicyID),
		reversal.PolicyVersion,
		reversal.OwnerRatio.StringFixed(8),
		nullablePositiveInt64(reversal.Invite.InviterUserID),
		nullableTime(reversal.Invite.BoundAt),
		nullableTime(reversal.Invite.ExpiresAt),
		reversal.InviteRatio.StringFixed(8),
		reversal.InviteCredit.StringFixed(10),
		reversal.PlatformRatio.StringFixed(8),
		durationMs,
		accountShareSeatSettlementTypeWaiverRefund,
		periodStart,
		periodEnd,
		refund.StringFixed(10),
		waiver.Minimum.StringFixed(8),
		waiver.Required.StringFixed(10),
		waiver.Usage.StringFixed(10),
		nullablePositiveInt64(reversalOfSettlementID),
	).Scan(&settlementID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return settlementID, err
}

func accountShareSeatCharge(hourlyRate float64, duration time.Duration) float64 {
	if hourlyRate <= 0 || duration <= 0 {
		return 0
	}
	return hourlyRate * float64(duration.Milliseconds()) / 3600000.0
}

func accountShareSeatWaiverWindowReadyAt(settleAt time.Time, windowEnd time.Time) bool {
	grace := service.AccountShareModeSeatWaiverSettlementGrace
	if grace <= 0 {
		return true
	}
	return !settleAt.UTC().Before(windowEnd.UTC().Add(grace))
}

func (r *accountShareModeRepository) GetActiveMembershipForAPIKey(ctx context.Context, apiKeyID int64) (*service.AccountShareMembership, *service.AccountShareListing, error) {
	return r.queryActiveMembership(ctx, `
		m.api_key_id = $1
	`, apiKeyID)
}

func (r *accountShareModeRepository) GetActiveMembershipForRequest(ctx context.Context, userID, apiKeyID, groupID int64) (*service.AccountShareMembership, *service.AccountShareListing, error) {
	// The active membership is the source of truth for account-share mode routing.
	// account_groups is scheduler metadata and can be rewritten by generic owned-account repair flows.
	return r.queryActiveMembership(ctx, `
		m.consumer_user_id = $1
		AND m.api_key_id = $2
		AND a.platform = (
			SELECT mg.platform
			FROM account_share_mode_groups mg
			WHERE mg.group_id = $3
		)
	`, userID, apiKeyID, groupID)
}

func (r *accountShareModeRepository) ActivateNextQueuedMembershipForRequest(ctx context.Context, userID, apiKeyID, groupID int64, afterRank int, now time.Time) (*service.AccountShareMembership, *service.AccountShareListing, error) {
	now = now.UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	lockedListingIDs, err := r.lockQueuedMembershipListingsForRequestInTx(ctx, tx, userID, apiKeyID, groupID, now)
	if err != nil {
		return nil, nil, err
	}
	if len(lockedListingIDs) == 0 {
		return nil, nil, service.ErrAccountShareListingNotFound
	}

	var membershipID, listingID, accountID, ownerUserID int64
	var queueRank, idleTimeoutMinutes int
	var hourlyRate, hourlyFeeWaiverMinimum, minBalanceRequired float64
	err = tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT
			m.id, m.listing_id, m.account_id, l.owner_user_id, m.queue_rank, m.idle_timeout_minutes,
			l.hourly_rate, l.hourly_fee_waiver_minimum, l.min_balance_required
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
			AND l.deleted_at IS NULL
		JOIN accounts a ON a.id = m.account_id
			AND a.deleted_at IS NULL
		WHERE m.consumer_user_id = $1
			AND m.api_key_id = $2
			AND m.status = $3
			AND m.deleted_at IS NULL
			AND a.platform = (
				SELECT mg.platform
				FROM account_share_mode_groups mg
				WHERE mg.group_id = $4
			)
			AND (m.dispatch_cooldown_until IS NULL OR m.dispatch_cooldown_until <= $5)
			AND l.id = ANY($7::bigint[])
			AND %s
		ORDER BY CASE WHEN m.queue_rank > $6 THEN 0 ELSE 1 END,
			m.queue_rank ASC,
			m.id ASC
		LIMIT 1
		FOR UPDATE OF m
	`, accountShareQueuedActivationConditionSQL("$5", "$1")),
		userID,
		apiKeyID,
		service.AccountShareMembershipStatusQueued,
		groupID,
		now,
		afterRank,
		pq.Array(lockedListingIDs),
	).Scan(
		&membershipID,
		&listingID,
		&accountID,
		&ownerUserID,
		&queueRank,
		&idleTimeoutMinutes,
		&hourlyRate,
		&hourlyFeeWaiverMinimum,
		&minBalanceRequired,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, nil, err
	}

	ownerSelfUse := ownerUserID == userID
	if ownerSelfUse {
		hourlyRate = 0
		hourlyFeeWaiverMinimum = 0
	}
	var userBalance float64
	if err := tx.QueryRowContext(ctx, `
		SELECT balance
		FROM users
		WHERE id = $1
			AND deleted_at IS NULL
		FOR UPDATE
	`, userID).Scan(&userBalance); errors.Is(err, sql.ErrNoRows) {
		return nil, nil, service.ErrUserNotFound
	} else if err != nil {
		return nil, nil, err
	}
	prepayDuration := service.AccountShareModeSeatPrepayDuration
	prepayAmount := accountShareSeatCharge(hourlyRate, prepayDuration)
	paidUntil := now.Add(prepayDuration)
	if !ownerSelfUse && userBalance < minBalanceRequired {
		return nil, nil, service.ErrAccountShareBalanceBelowMinimum
	}
	if !ownerSelfUse && prepayAmount > 0 && userBalance < minBalanceRequired+prepayAmount {
		return nil, nil, service.ErrAccountShareModePrepayInsufficient
	}
	var paidUntilValue any
	var billedUntilValue any
	if prepayAmount > 0 {
		paidUntilValue = paidUntil
		billedUntilValue = now
	}

	membership, err := scanAccountShareMembership(tx.QueryRowContext(ctx, `
		UPDATE account_share_memberships m
		SET status = $1,
			hourly_rate_snapshot = $2,
			hourly_fee_waiver_minimum_snapshot = $3,
			idle_timeout_minutes = $4,
			joined_at = $5,
			last_request_at = NULL,
			ended_at = NULL,
			ended_reason = NULL,
			paid_until = $6,
			billed_until = $7,
			waiver_window_started_at = $7,
			waiver_window_usage_amount = 0,
			waiver_window_request_count = 0,
			waiver_window_last_request_at = NULL,
			dispatch_failed_at = NULL,
			dispatch_cooldown_until = NULL,
			updated_at = NOW()
		FROM account_share_listings l
		WHERE m.id = $8
			AND l.id = m.listing_id
		RETURNING
			m.id, m.listing_id, m.account_id, l.owner_user_id, m.consumer_user_id, m.api_key_id,
			m.status, m.queue_rank, m.hourly_rate_snapshot, m.hourly_fee_waiver_minimum_snapshot, m.idle_timeout_minutes,
			m.joined_at, m.last_request_at, m.ended_at, m.ended_reason, m.paid_until, m.billed_until,
			m.waiver_window_started_at, m.waiver_window_usage_amount, m.waiver_window_request_count, m.waiver_window_last_request_at,
			m.dispatch_failed_at, m.dispatch_cooldown_until, m.created_at, m.updated_at
	`,
		service.AccountShareMembershipStatusActive,
		hourlyRate,
		hourlyFeeWaiverMinimum,
		idleTimeoutMinutes,
		now,
		paidUntilValue,
		billedUntilValue,
		membershipID,
	))
	if err != nil {
		return nil, nil, translateAccountShareMembershipConflict(err)
	}
	membership.QueueRank = queueRank
	if prepayAmount > 0 {
		newBalance := userBalance - prepayAmount
		if _, err := tx.ExecContext(ctx, `
			UPDATE users
			SET balance = $1::numeric,
				updated_at = NOW()
			WHERE id = $2
				AND deleted_at IS NULL
		`, decimalFromSignedFloat(newBalance).StringFixed(10), userID); err != nil {
			return nil, nil, err
		}
		if err := insertUserBalanceLedger(ctx, tx, userBalanceLedgerInput{
			UserID:          userID,
			Direction:       "debit",
			Amount:          decimalFromFloat(prepayAmount),
			Reason:          accountShareSeatPrepayReason,
			RefType:         accountShareSeatPrepayRefType,
			RefID:           accountShareSeatPrepayRefID(membership.ID, paidUntil),
			BalanceAfter:    decimalFromSignedFloat(newBalance),
			RequireInserted: true,
			Metadata: map[string]any{
				"listing_id":    listingID,
				"account_id":    accountID,
				"membership_id": membership.ID,
				"hourly_rate":   hourlyRate,
				"duration_ms":   int(prepayDuration.Milliseconds()),
				"paid_until":    paidUntil.Format(time.RFC3339),
				"prepay_stage":  "queue_activation",
				"seat_billing":  true,
				"consumer_user": userID,
			},
		}); err != nil {
			return nil, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	tx = nil
	listing, err := r.GetListingByID(ctx, membership.ListingID, membership.ConsumerUserID)
	if err != nil {
		return nil, nil, err
	}
	return membership, listing, nil
}

func (r *accountShareModeRepository) lockQueuedMembershipListingsForRequestInTx(ctx context.Context, tx *sql.Tx, userID, apiKeyID, groupID int64, now time.Time) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT l.id
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
			AND l.deleted_at IS NULL
		JOIN accounts a ON a.id = m.account_id
			AND a.deleted_at IS NULL
		WHERE m.consumer_user_id = $1
			AND m.api_key_id = $2
			AND m.status = $3
			AND m.deleted_at IS NULL
			AND a.platform = (
				SELECT mg.platform
				FROM account_share_mode_groups mg
				WHERE mg.group_id = $4
			)
			AND (m.dispatch_cooldown_until IS NULL OR m.dispatch_cooldown_until <= $5)
		ORDER BY l.id ASC
		LIMIT $6
		FOR UPDATE OF l
	`,
		userID,
		apiKeyID,
		service.AccountShareMembershipStatusQueued,
		groupID,
		now.UTC(),
		service.AccountShareModeQueueMaxItems,
	)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	listingIDs := make([]int64, 0, service.AccountShareModeQueueMaxItems)
	for rows.Next() {
		var listingID int64
		if err := rows.Scan(&listingID); err != nil {
			return nil, err
		}
		listingIDs = append(listingIDs, listingID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return listingIDs, nil
}

func (r *accountShareModeRepository) SuspendMembershipForDispatchFailure(ctx context.Context, membershipID int64, failedAt time.Time, cooldownUntil time.Time) (*service.AccountShareMembership, *service.AccountShareSeatBillingResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	failedAt = failedAt.UTC()
	cooldownUntil = cooldownUntil.UTC()
	membership, err := r.lockSeatBillingMembershipInTx(ctx, tx, membershipID, 0)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	// Slot acquisition and its heartbeat update last_request_at only after the
	// membership is actually in use. Keep this check inside the membership row
	// lock so a concurrent dispatch failure cannot queue an active stream.
	if accountShareMembershipRecentlyActive(membership, failedAt) {
		return nil, nil, nil
	}
	membership, creditUserIDs, err := r.suspendActiveMembershipInTx(ctx, tx, membership, failedAt, cooldownUntil)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	tx = nil
	return membership, accountShareMembershipBillingResult(membership, creditUserIDs), nil
}

func (r *accountShareModeRepository) suspendActiveMembershipInTx(ctx context.Context, tx *sql.Tx, membership *service.AccountShareMembership, failedAt time.Time, cooldownUntil time.Time) (*service.AccountShareMembership, []int64, error) {
	if membership == nil || membership.ID <= 0 {
		return nil, nil, service.ErrAccountShareListingNotFound
	}
	settledUntil, _, creditUserIDs, err := r.settleSeatChargeInTx(ctx, tx, membership, failedAt, true, failedAt)
	if err != nil {
		return nil, nil, err
	}
	if err := r.refundUnusedSeatPrepayInTx(ctx, tx, membership, failedAt); err != nil {
		return nil, nil, err
	}
	if settledUntil == nil {
		settledUntil = &failedAt
	}
	membership, err = scanAccountShareMembership(tx.QueryRowContext(ctx, `
		UPDATE account_share_memberships m
		SET status = $1,
			paid_until = NULL,
			billed_until = $2,
			waiver_window_started_at = $2,
			waiver_window_usage_amount = 0,
			waiver_window_request_count = 0,
			waiver_window_last_request_at = NULL,
			dispatch_failed_at = $3,
			dispatch_cooldown_until = $4,
			updated_at = NOW()
		FROM account_share_listings l
		WHERE m.id = $5
			AND l.id = m.listing_id
			AND m.status = $6
			AND m.deleted_at IS NULL
		RETURNING
			m.id, m.listing_id, m.account_id, l.owner_user_id, m.consumer_user_id, m.api_key_id,
			m.status, m.queue_rank, m.hourly_rate_snapshot, m.hourly_fee_waiver_minimum_snapshot, m.idle_timeout_minutes,
			m.joined_at, m.last_request_at, m.ended_at, m.ended_reason, m.paid_until, m.billed_until,
			m.waiver_window_started_at, m.waiver_window_usage_amount, m.waiver_window_request_count, m.waiver_window_last_request_at,
			m.dispatch_failed_at, m.dispatch_cooldown_until, m.created_at, m.updated_at
	`, service.AccountShareMembershipStatusQueued, *settledUntil, failedAt, cooldownUntil, membership.ID, service.AccountShareMembershipStatusActive))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	return membership, creditUserIDs, nil
}

func accountShareMembershipRecentlyActive(membership *service.AccountShareMembership, now time.Time) bool {
	if membership == nil || membership.LastRequestAt == nil {
		return false
	}
	guardWindow := service.AccountShareModeLastRequestTouchInterval
	if guardWindow <= 0 {
		guardWindow = 30 * time.Second
	}
	return !membership.LastRequestAt.UTC().Before(now.UTC().Add(-guardWindow))
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

func (r *accountShareModeRepository) queryActiveMembership(ctx context.Context, predicate string, args ...any) (*service.AccountShareMembership, *service.AccountShareListing, error) {
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
	membership, err := scanAccountShareMembership(r.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, nil, err
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
		len(filters.SeatLimits) > 0 ||
		strings.TrimSpace(filters.Search) != "" ||
		strings.TrimSpace(filters.Status) != "" ||
		filters.OwnerUserID > 0 ||
		len(filters.Models) > 0 ||
		strings.TrimSpace(filters.AccountLevel) != "" ||
		len(filters.FeatureTags) > 0
}

func accountShareListingOrderSQL(filters service.AccountShareListingFilters) string {
	sorts := filters.Sorts
	if len(sorts) == 0 && strings.TrimSpace(filters.SortBy) != "" {
		sorts = []service.AccountShareListingSortCriterion{{SortBy: filters.SortBy, SortOrder: filters.SortOrder}}
	}
	if len(sorts) == 0 {
		return `CASE WHEN qm.id IS NOT NULL THEN 0 ELSE 1 END,
			qm.queue_rank ASC NULLS LAST,
			COALESCE(cm.joined_at, hm.ended_at, l.updated_at) DESC,
			l.id DESC`
	}
	orderParts := make([]string, 0, len(sorts)+1)
	lastDirection := "ASC"
	for _, sort := range sorts {
		expr := accountShareListingSortExpressionSQL(sort.SortBy)
		if expr == "" {
			continue
		}
		direction := "ASC"
		if sort.SortOrder == service.AccountShareListingSortOrderDesc {
			direction = "DESC"
		}
		lastDirection = direction
		orderParts = append(orderParts, fmt.Sprintf("%s %s", expr, direction))
	}
	if len(orderParts) == 0 {
		return `CASE WHEN qm.id IS NOT NULL THEN 0 ELSE 1 END,
			qm.queue_rank ASC NULLS LAST,
			COALESCE(cm.joined_at, hm.ended_at, l.updated_at) DESC,
			l.id DESC`
	}
	orderParts = append(orderParts, fmt.Sprintf("l.id %s", lastDirection))
	return strings.Join(orderParts, ", ")
}

func accountShareListingSortExpressionSQL(sortBy string) string {
	switch sortBy {
	case service.AccountShareListingSortAccountConcurrency:
		return "a.concurrency"
	case service.AccountShareListingSortPerUserConcurrency:
		return "l.per_user_concurrency"
	case service.AccountShareListingSortMinBalanceRequired:
		return "l.min_balance_required"
	case service.AccountShareListingSortHourlyRate:
		return "l.hourly_rate"
	case service.AccountShareListingSortHourlyFeeWaiver:
		return "l.hourly_fee_waiver_minimum"
	case service.AccountShareListingSortRateMultiplier:
		return "l.rate_multiplier"
	case service.AccountShareListingSortRemainingSeats:
		return "(l.seat_limit - COALESCE(ac.active_seats, 0))"
	case service.AccountShareListingSortRating:
		return "(CASE WHEN l.rating_count > 0 THEN l.rating_avg ELSE -1 END)"
	case service.AccountShareListingSortUpdatedAt:
		return "l.updated_at"
	default:
		return ""
	}
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
	anthropicProtectedSQL := fmt.Sprintf(`(
		a.platform = '%s'
		AND a.type IN ('%s', '%s')
		AND (
			%s
			OR %s
		)
	)`,
		service.PlatformAnthropic,
		service.AccountTypeOAuth,
		service.AccountTypeSetupToken,
		accountShareAnthropicQuotaProtectedSQL(
			"session_window_utilization",
			"anthropic_5h_limit_percent",
			fmt.Sprintf("COALESCE(a.session_window_end, %s, %s)", accountShareExtraTimeSQL("anthropic_5h_reset_at"), accountShareExtraTimeSQL("session_window_reset_at")),
			nowExpr,
		),
		accountShareAnthropicQuotaProtectedSQL(
			"passive_usage_7d_utilization",
			"anthropic_7d_limit_percent",
			fmt.Sprintf("COALESCE(%s, %s)", accountShareExtraTimeSQL("anthropic_7d_reset_at"), accountShareExtraTimeSQL("passive_usage_7d_reset")),
			nowExpr,
		),
	)
	return fmt.Sprintf(`(
		a.status <> '%s'
		OR a.schedulable = FALSE
		OR (a.auto_pause_on_expired = TRUE AND a.expires_at IS NOT NULL AND a.expires_at <= %s)
		OR (a.overload_until IS NOT NULL AND a.overload_until > %s)
		OR (a.rate_limit_reset_at IS NOT NULL AND a.rate_limit_reset_at > %s)
		OR (a.temp_unschedulable_until IS NOT NULL AND a.temp_unschedulable_until > %s)
		OR %s
		OR %s
	)`,
		service.StatusActive,
		nowExpr,
		nowExpr,
		nowExpr,
		nowExpr,
		codexProtectedSQL,
		anthropicProtectedSQL,
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
				AND m_available.consumer_user_id <> l.owner_user_id
		)
	)`,
		service.AccountShareListingStatusActive,
		nowExpr,
		accountShareAccountUnavailableConditionSQL(nowExpr),
		service.AccountShareMembershipStatusActive,
	)
}

func accountShareQueuedActivationConditionSQL(nowExpr string, consumerUserIDExpr string) string {
	return fmt.Sprintf(`(
		l.status = '%[1]s'
		AND NOT %[4]s
		AND (l.editing_expires_at IS NULL OR l.editing_expires_at <= %[2]s)
		AND (
			l.owner_user_id = %[3]s
			OR l.seat_limit > (
				SELECT COUNT(*)::int
				FROM account_share_memberships m_available
				WHERE m_available.listing_id = l.id
					AND m_available.status = '%[5]s'
					AND m_available.deleted_at IS NULL
					AND m_available.consumer_user_id <> l.owner_user_id
			)
		)
	)`,
		service.AccountShareListingStatusActive,
		nowExpr,
		consumerUserIDExpr,
		accountShareAccountUnavailableConditionSQL(nowExpr),
		service.AccountShareMembershipStatusActive,
	)
}

func accountShareListingSupportsImageGenerationSQL() string {
	return `EXISTS (
		SELECT 1
		FROM jsonb_array_elements_text(l.allowed_models) AS image_model(value)
		WHERE lower(image_model.value) ~ '(^|[/_:])(gpt-image(-|$)|dall-e(-|$)|dalle(-|$))'
	)`
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
		OR a.status IN ('%s', 'inactive')
		OR (a.auto_pause_on_expired = TRUE AND a.expires_at IS NOT NULL AND a.expires_at <= %s)
	)`, service.StatusDisabled, nowExpr)
}

func accountShareMembershipPermanentlyUnavailableConditionSQL(nowExpr string) string {
	return fmt.Sprintf(`(
		l.id IS NULL
		OR l.deleted_at IS NOT NULL
		OR l.status = '%s'
		OR %s
	)`, service.AccountShareListingStatusDisabled, accountShareAccountPermanentlyUnavailableConditionSQL(nowExpr))
}

func accountShareMembershipRecoverablyUnavailableConditionSQL(nowExpr string) string {
	return fmt.Sprintf(`(
		NOT %s
		AND (
			l.status = '%s'
			OR %s
		)
	)`,
		accountShareMembershipPermanentlyUnavailableConditionSQL(nowExpr),
		service.AccountShareListingStatusPaused,
		accountShareAccountUnavailableConditionSQL(nowExpr),
	)
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

func accountShareAnthropicQuotaProtectedSQL(utilizationKey, limitKey, resetExpr, nowExpr string) string {
	utilization := fmt.Sprintf("COALESCE((%s), 0)", accountShareAnthropicUtilizationPercentSQL(utilizationKey))
	limitRaw := accountShareExtraNumberSQL(limitKey)
	minLimit := strconv.FormatFloat(service.AnthropicQuotaMinLimitPercent, 'f', 1, 64)
	maxLimit := strconv.FormatFloat(service.AnthropicQuotaMaxLimitPercent, 'f', 1, 64)
	defaultLimit := strconv.FormatFloat(service.AnthropicQuotaDefaultLimitPercent, 'f', 1, 64)
	limit := fmt.Sprintf(`CASE WHEN (%s) >= %s AND (%s) <= %s THEN (%s) ELSE %s END`,
		limitRaw,
		minLimit,
		limitRaw,
		maxLimit,
		limitRaw,
		defaultLimit,
	)
	return fmt.Sprintf(`COALESCE(((%s) >= (%s) AND (%s) > %s), FALSE)`, utilization, limit, resetExpr, nowExpr)
}

func accountShareAnthropicUtilizationPercentSQL(key string) string {
	raw := accountShareExtraNumberSQL(key)
	return fmt.Sprintf(`CASE
		WHEN (%[1]s) IS NULL THEN NULL
		WHEN (%[1]s) < 0 THEN 0
		WHEN (%[1]s) <= 1.5 THEN (%[1]s) * 100
		ELSE (%[1]s)
	END`, raw)
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

func accountShareEffectiveAccountLevelSQL(configs []service.OpenAIAccountLevelConfig) string {
	token := accountSharePlanTokenSQL()
	levels := service.OpenAIAccountLevelConfigSelectable(configs)
	if len(levels) == 0 {
		levels = service.DefaultOpenAIAccountLevelConfigs()
	}
	accountLevelLiterals := make([]string, 0, len(levels))
	whens := make([]string, 0, len(levels))
	for _, cfg := range levels {
		key := service.NormalizeAccountLevelKey(cfg.Key)
		if key == "" || key == service.AccountLevelUnknown {
			continue
		}
		accountLevelLiterals = append(accountLevelLiterals, accountShareSQLLiteral(key))
		conditions := make([]string, 0, len(cfg.Aliases)+1)
		for _, alias := range service.NormalizeOpenAIAccountLevelConfigs([]service.OpenAIAccountLevelConfig{cfg})[0].Aliases {
			if strings.HasSuffix(alias, "*") {
				prefix := strings.TrimSuffix(alias, "*")
				if prefix != "" {
					conditions = append(conditions, fmt.Sprintf("%s LIKE %s", token, accountShareSQLLiteral(prefix+"%")))
				}
				continue
			}
			conditions = append(conditions, fmt.Sprintf("%s = %s", token, accountShareSQLLiteral(alias)))
		}
		if len(conditions) > 0 {
			whens = append(whens, fmt.Sprintf("WHEN %s THEN %s", strings.Join(conditions, " OR "), accountShareSQLLiteral(key)))
		}
	}
	if len(accountLevelLiterals) == 0 {
		accountLevelLiterals = []string{accountShareSQLLiteral(service.AccountLevelUnknown)}
	}
	return fmt.Sprintf(`CASE
		WHEN a.account_level IN (%s) THEN a.account_level
		%s
		ELSE 'unknown'
	END`, strings.Join(accountLevelLiterals, ", "), strings.Join(whens, "\n\t\t"))
}

func accountShareSQLLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
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
			l.account_identity_id,
			l.rating_count,
			l.rating_score_sum,
			l.rating_avg,
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
			a.session_window_start,
			a.session_window_end,
			a.session_window_status,
			a.credentials,
			a.extra,
			COALESCE(NULLIF(a.credentials->>'subscription_expires_at', ''), NULLIF(a.extra->>'subscription_expires_at', '')),
			cm.id,
			cm.consumer_user_id,
			cm.api_key_id,
			cm.api_key_name,
			cm.joined_at,
			cm.paid_until,
			cm.billed_until,
			cm.idle_timeout_minutes,
			cm.last_request_at,
			cm.waiver_window_started_at,
			cm.waiver_window_usage_amount::text,
			cm.waiver_window_request_count,
			cm.waiver_window_last_request_at,
			qm.id,
			qm.api_key_id,
			qm.api_key_name,
			qm.queue_rank,
			qm.status,
			qm.idle_timeout_minutes,
			qm.dispatch_cooldown_until,
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
				AND m.consumer_user_id <> l.owner_user_id
		) ac ON TRUE
		LEFT JOIN LATERAL (
			SELECT
				m.id,
				m.consumer_user_id,
				m.api_key_id,
				COALESCE(ak.name, '') AS api_key_name,
				m.joined_at,
				m.paid_until,
				m.billed_until,
				m.idle_timeout_minutes,
				m.last_request_at,
				m.waiver_window_started_at,
				m.waiver_window_usage_amount,
				m.waiver_window_request_count,
				m.waiver_window_last_request_at
			FROM account_share_memberships m
			LEFT JOIN api_keys ak ON ak.id = m.api_key_id
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
			SELECT m.id, m.api_key_id, COALESCE(ak.name, '') AS api_key_name, m.queue_rank, m.status, m.idle_timeout_minutes, m.dispatch_cooldown_until
			FROM account_share_memberships m
			LEFT JOIN api_keys ak ON ak.id = m.api_key_id
			WHERE m.listing_id = l.id
				AND m.consumer_user_id = $1
				AND m.status IN ('%s', '%s')
				AND m.deleted_at IS NULL
			ORDER BY m.queue_rank ASC, m.id ASC
			LIMIT 1
		) qm ON TRUE
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
	`, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusActive, service.AccountShareMembershipStatusQueued, service.AccountShareMembershipStatusEnded)
}

type accountShareListingScanner interface {
	Scan(dest ...any) error
}

type accountShareMembershipScanner interface {
	Scan(dest ...any) error
}

type accountShareReviewScanner interface {
	Scan(dest ...any) error
}

func accountShareReviewSelectSQL() string {
	return `
		SELECT
			r.id,
			r.account_identity_id,
			COALESCE(r.listing_id, 0),
			COALESCE(r.account_id, 0),
			r.membership_id,
			r.owner_user_id,
			COALESCE(ou.username, ''),
			r.consumer_user_id,
			COALESCE(cu.username, ''),
			COALESCE(a.name, ''),
			COALESCE(i.platform, ''),
			r.score,
			r.comment,
			r.comment_status,
			r.comment_reject_reason,
			r.created_at,
			r.updated_at
		FROM account_share_reviews r
		JOIN account_share_account_identities i ON i.id = r.account_identity_id
		LEFT JOIN account_share_listings l ON l.id = r.listing_id
		LEFT JOIN accounts a ON a.id = COALESCE(r.account_id, l.account_id)
		LEFT JOIN users ou ON ou.id = r.owner_user_id
		LEFT JOIN users cu ON cu.id = r.consumer_user_id
	`
}

func getAccountShareReviewByIDTx(ctx context.Context, tx *sql.Tx, reviewID int64) (*service.AccountShareReview, error) {
	return scanAccountShareReview(tx.QueryRowContext(ctx, accountShareReviewSelectSQL()+`
		WHERE r.id = $1
			AND r.deleted_at IS NULL
	`, reviewID))
}

func scanAccountShareReview(scanner accountShareReviewScanner) (*service.AccountShareReview, error) {
	review := &service.AccountShareReview{}
	err := scanner.Scan(
		&review.ID,
		&review.AccountIdentityID,
		&review.ListingID,
		&review.AccountID,
		&review.MembershipID,
		&review.OwnerUserID,
		&review.OwnerUsername,
		&review.ConsumerUserID,
		&review.ConsumerUsername,
		&review.AccountName,
		&review.Platform,
		&review.Score,
		&review.Comment,
		&review.CommentStatus,
		&review.CommentRejectReason,
		&review.CreatedAt,
		&review.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return review, nil
}

func scanAccountShareReviews(rows *sql.Rows) ([]service.AccountShareReview, error) {
	reviews := make([]service.AccountShareReview, 0)
	for rows.Next() {
		review, err := scanAccountShareReview(rows)
		if err != nil {
			return nil, err
		}
		reviews = append(reviews, *review)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return reviews, nil
}

func accountShareReviewPagination(total int64, page, limit int) *pagination.PaginationResult {
	pages := 0
	if total > 0 {
		pages = int((total + int64(limit) - 1) / int64(limit))
	}
	return &pagination.PaginationResult{
		Total:    total,
		Page:     page,
		PageSize: limit,
		Pages:    pages,
	}
}

func refreshAccountShareListingRatingsInTx(ctx context.Context, tx *sql.Tx, accountIdentityID int64) error {
	if tx == nil || accountIdentityID <= 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE account_share_listings l
		SET rating_count = COALESCE((
				SELECT COUNT(*)::int
				FROM account_share_reviews r
				WHERE r.account_identity_id = $1
					AND r.deleted_at IS NULL
			), 0),
			rating_score_sum = COALESCE((
				SELECT SUM(r.score)::int
				FROM account_share_reviews r
				WHERE r.account_identity_id = $1
					AND r.deleted_at IS NULL
			), 0),
			rating_avg = COALESCE((
				SELECT ROUND(AVG(r.score)::numeric, 2)
				FROM account_share_reviews r
				WHERE r.account_identity_id = $1
					AND r.deleted_at IS NULL
			), 0)
		WHERE l.account_identity_id = $1
			AND l.deleted_at IS NULL
	`, accountIdentityID)
	return err
}

func isAccountShareReviewUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) &&
		pqErr.Code == "23505" &&
		pqErr.Constraint == "uq_account_share_reviews_membership_live"
}

func scanAccountShareMembership(scanner accountShareMembershipScanner) (*service.AccountShareMembership, error) {
	membership := &service.AccountShareMembership{}
	var endedAt, lastRequestAt, paidUntil, billedUntil, waiverWindowStartedAt, waiverWindowLastRequestAt, dispatchFailedAt, dispatchCooldownUntil sql.NullTime
	var endedReason sql.NullString
	err := scanner.Scan(
		&membership.ID,
		&membership.ListingID,
		&membership.AccountID,
		&membership.OwnerUserID,
		&membership.ConsumerUserID,
		&membership.APIKeyID,
		&membership.Status,
		&membership.QueueRank,
		&membership.HourlyRateSnapshot,
		&membership.HourlyFeeWaiverMinimumSnapshot,
		&membership.IdleTimeoutMinutes,
		&membership.JoinedAt,
		&lastRequestAt,
		&endedAt,
		&endedReason,
		&paidUntil,
		&billedUntil,
		&waiverWindowStartedAt,
		&membership.WaiverWindowUsageAmount,
		&membership.WaiverWindowRequestCount,
		&waiverWindowLastRequestAt,
		&dispatchFailedAt,
		&dispatchCooldownUntil,
		&membership.CreatedAt,
		&membership.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	applyAccountShareMembershipNullableFields(membership, lastRequestAt, endedAt, endedReason, paidUntil, billedUntil)
	if waiverWindowStartedAt.Valid {
		membership.WaiverWindowStartedAt = &waiverWindowStartedAt.Time
	}
	if waiverWindowLastRequestAt.Valid {
		membership.WaiverWindowLastRequestAt = &waiverWindowLastRequestAt.Time
	}
	if dispatchFailedAt.Valid {
		membership.DispatchFailedAt = &dispatchFailedAt.Time
	}
	if dispatchCooldownUntil.Valid {
		membership.DispatchCooldownUntil = &dispatchCooldownUntil.Time
	}
	return membership, nil
}

func scanAccountShareListing(scanner accountShareListingScanner) (*service.AccountShareListing, error) {
	listing := &service.AccountShareListing{}
	var allowedModelsRaw []byte
	var proxyID, accountIdentityID, currentMembershipID, currentConsumerUserID, currentAPIKeyID, currentIdleTimeoutMinutes, queueMembershipID, queueAPIKeyID, queueRank, queueIdleTimeoutMinutes, lastUsedMembershipID, editingByUserID sql.NullInt64
	var currentJoinedAt, currentPaidUntil, currentBilledUntil, currentLastRequestAt, currentWaiverWindowStartedAt, currentWaiverWindowLastRequestAt, queueDispatchCooldownUntil, lastUsedAt, editingExpiresAt sql.NullTime
	var accountPlatform, accountType, accountLevel, accountStatus string
	var accountSchedulable bool
	var accountExpiresAt, accountLastUsedAt, rateLimitedAt, rateLimitResetAt, overloadUntil, tempUnschedulableUntil, sessionWindowStart, sessionWindowEnd sql.NullTime
	var tempUnschedulableReason, sessionWindowStatus, subscriptionExpiresAtRaw, currentAPIKeyName, queueAPIKeyName, queueStatus sql.NullString
	var editingByUsername, editSessionID string
	var credentialsRaw, extraRaw []byte
	var currentWaiverWindowUsageAmount sql.NullString
	var currentWaiverWindowRequestCount sql.NullInt64
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
		&accountIdentityID,
		&listing.RatingCount,
		&listing.RatingScoreSum,
		&listing.RatingAvg,
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
		&queueIdleTimeoutMinutes,
		&queueDispatchCooldownUntil,
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
	listing.AccountIdentityID = sqlNullInt64Ptr(accountIdentityID)
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
			id, name, description, platform, rate_multiplier, new_user_rate_enabled,
			new_user_rate_multiplier, new_user_rate_window_seconds, new_user_rate_quota_usd, is_exclusive, status,
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
		&group.NewUserRateEnabled,
		&group.NewUserRateMultiplier,
		&group.NewUserRateWindowSeconds,
		&group.NewUserRateQuotaUSD,
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
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
			AND l.deleted_at IS NULL
		WHERE m.listing_id = $1
			AND m.status = $2
			AND m.deleted_at IS NULL
			AND m.consumer_user_id <> l.owner_user_id
	`, listingID, service.AccountShareMembershipStatusActive).Scan(&activeSeats); err != nil {
		return 0, err
	}
	return activeSeats, nil
}

func endStaleQueuedMembershipsForAPIKeyInTx(ctx context.Context, tx *sql.Tx, consumerUserID, apiKeyID int64, endedAt time.Time) (int64, error) {
	if consumerUserID <= 0 || apiKeyID <= 0 {
		return 0, nil
	}
	endedAt = endedAt.UTC()
	result, err := tx.ExecContext(ctx, fmt.Sprintf(`
		UPDATE account_share_memberships m
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
		WHERE m.consumer_user_id = $4
			AND m.api_key_id = $5
			AND m.status = $6
			AND m.deleted_at IS NULL
			AND EXISTS (
				SELECT 1
				FROM account_share_listings l
				LEFT JOIN accounts a ON a.id = m.account_id
				WHERE l.id = m.listing_id
					AND (
						l.deleted_at IS NOT NULL
						OR l.status = $7
						OR %s
					)
			)
	`, accountShareAccountPermanentlyUnavailableConditionSQL("$2")),
		service.AccountShareMembershipStatusEnded,
		endedAt,
		service.AccountShareMembershipEndReasonUnavailable,
		consumerUserID,
		apiKeyID,
		service.AccountShareMembershipStatusQueued,
		service.AccountShareListingStatusDisabled,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
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
		case "uq_account_share_memberships_queue_rank":
			return service.ErrAccountShareQueueInvalid.WithCause(err)
		case "uq_account_share_memberships_active_or_queued_listing_consumer":
			return service.ErrAccountShareAlreadyUsing.WithCause(err)
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
