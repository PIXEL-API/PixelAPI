package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
)

type accountShareModeRepository struct {
	db      *sql.DB
	rollout config.AccountShareRolloutConfig
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

func (s *accountShareListingRevisionSnapshot) termsSnapshot() *service.AccountShareListingTermsSnapshot {
	if s == nil {
		return nil
	}
	return &service.AccountShareListingTermsSnapshot{
		ListingRevisionID:       s.ID,
		RowVersion:              s.RowVersion,
		SchemaVersion:           s.SchemaVersion,
		RoomName:                s.RoomName,
		Status:                  s.Status,
		SeatLimit:               s.SeatLimit,
		RateMultiplier:          s.RateMultiplier,
		AllowedModels:           append([]string(nil), s.AllowedModels...),
		PerUserConcurrency:      s.PerUserConcurrency,
		HourlyRate:              s.HourlyRate,
		HourlyFeeWaiverMinimum:  s.HourlyFeeWaiverMinimum,
		MinBalanceRequired:      s.MinBalanceRequired,
		CodexCLIOnly:            s.CodexCLIOnly,
		Codex5hLimitPercent:     s.Codex5hLimitPercent,
		Codex7dLimitPercent:     s.Codex7dLimitPercent,
		Anthropic5hLimitPercent: s.Codex5hLimitPercent,
		Anthropic7dLimitPercent: s.Codex7dLimitPercent,
	}
}

func NewAccountShareModeRepository(
	_ *dbent.Client,
	sqlDB *sql.DB,
	cfg *config.Config,
) service.AccountShareModeRepository {
	rollout := config.AccountShareRolloutConfig{QuotaMode: config.AccountShareQuotaModeShadow}
	if cfg != nil {
		rollout = cfg.AccountShareRollout
	}
	return &accountShareModeRepository{
		db:      sqlDB,
		rollout: rollout,
	}
}

func (r *accountShareModeRepository) deferredQueueBindingEnabled() bool {
	// 灰度已收敛：排队成员延迟绑定是唯一形态（迁移 248 已把存量 queued 的
	// account_id 置 NULL），不再受配置开关控制。
	return r != nil
}

func (r *accountShareModeRepository) reviewRoomSubjectWritesEnabled() bool {
	return r != nil && r.rollout.ReviewRoomSubjectWritesEnabled
}

func (r *accountShareModeRepository) quotaEnforcementEnabled() bool {
	return r != nil &&
		(r.rollout.QuotaMode == "" || r.rollout.QuotaMode == config.AccountShareQuotaModeEnforce)
}

func (r *accountShareModeRepository) listingSuspensionStatus() string {
	// 灰度已收敛：lifecycle 合约是唯一形态，暂停一律用 suspended。
	return service.AccountShareListingStatusSuspended
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
				AND status IN ($3, $4, $5)
				AND deleted_at IS NULL
		)
	`,
		consumerUserID,
		apiKeyID,
		service.AccountShareMembershipStatusActive,
		service.AccountShareMembershipStatusQueued,
		service.AccountShareMembershipStatusEnding,
	).Scan(&exists)
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
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "@") {
		return ""
	}
	return service.MaskEmailIdentity(email)
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
			codex_5h_limit_percent, codex_7d_limit_percent, account_identity_id, created_at, updated_at
		)
		VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8::jsonb,
			$9, $10, $11, $12, $13,
			$14, $15, $16, NOW(), NOW()
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
		Total:    total,
		Page:     page,
		PageSize: limit,
		Pages:    pages,
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
	listing.EditingByUserID = nil
	listing.EditingByUsername = ""
	listing.EditingExpiresAt = nil
	listing.EditingMine = false
	listing.EditSessionID = ""
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

type accountShareMembershipHistorySnapshot struct {
	MembershipID       int64
	ListingID          int64
	ListingRevisionID  *int64
	ListingVersion     *int64
	RoomName           string
	OwnerUserID        int64
	OwnerUsername      string
	Platform           string
	AccountLevel       string
	APIKeyName         string
	Terms              *service.AccountShareListingTermsSnapshot
	AccountID          int64
	AccountName        string
	AccountConcurrency int
	SnapshotQuality    string
}

func (r *accountShareModeRepository) loadAccountShareMembershipHistorySnapshots(
	ctx context.Context,
	consumerUserID int64,
	membershipIDs []int64,
) (map[int64]accountShareMembershipHistorySnapshot, error) {
	if consumerUserID <= 0 || len(membershipIDs) == 0 {
		return map[int64]accountShareMembershipHistorySnapshot{}, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			m.id,
			m.listing_id,
			m.listing_revision_id,
			m.listing_version_snapshot,
			COALESCE(
				NULLIF(m.room_name_snapshot, ''),
				NULLIF(revision.room_name, ''),
				''
			),
			COALESCE(m.owner_user_id_snapshot, revision.owner_user_id, 0),
			COALESCE(
				NULLIF(m.owner_username_snapshot, ''),
				NULLIF(revision.owner_display_name_snapshot, ''),
				''
			),
			COALESCE(
				NULLIF(m.platform_snapshot, ''),
				NULLIF(history_binding.platform_snapshot, ''),
				NULLIF(revision.platform, ''),
				''
			),
			COALESCE(
				NULLIF(m.account_level_snapshot, ''),
				NULLIF(history_binding.account_level_snapshot, ''),
				NULLIF(revision.account_level, ''),
				''
			),
			COALESCE(NULLIF(m.api_key_name_snapshot, ''), ''),
			m.terms_snapshot,
			COALESCE(history_binding.account_id_snapshot, m.account_id, 0),
			COALESCE(NULLIF(history_binding.account_name_snapshot, ''), ''),
			COALESCE(history_binding.configured_concurrency_snapshot, 0),
			COALESCE(NULLIF(m.snapshot_quality, ''), NULLIF(revision.snapshot_quality, ''), '')
		FROM account_share_memberships m
		LEFT JOIN account_share_listing_revisions revision
			ON revision.id = m.listing_revision_id
			AND revision.listing_id = m.listing_id
		LEFT JOIN account_share_listings l ON l.id = m.listing_id
		LEFT JOIN LATERAL (
			SELECT
				binding.account_id_snapshot,
				binding.account_name_snapshot,
				binding.platform_snapshot,
				binding.account_level_snapshot,
				binding.configured_concurrency_snapshot
			FROM account_share_membership_account_bindings binding
			WHERE binding.membership_id = m.id
				AND binding.listing_id = m.listing_id
			ORDER BY binding.routing_generation DESC, binding.id DESC
			LIMIT 1
		) history_binding ON TRUE
		WHERE m.id = ANY($1::bigint[])
			AND m.consumer_user_id = $2
			AND m.deleted_at IS NULL
	`, pq.Array(membershipIDs), consumerUserID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	snapshots := make(map[int64]accountShareMembershipHistorySnapshot, len(membershipIDs))
	for rows.Next() {
		var snapshot accountShareMembershipHistorySnapshot
		var listingRevisionID, listingVersion sql.NullInt64
		var termsRaw []byte
		if err := rows.Scan(
			&snapshot.MembershipID,
			&snapshot.ListingID,
			&listingRevisionID,
			&listingVersion,
			&snapshot.RoomName,
			&snapshot.OwnerUserID,
			&snapshot.OwnerUsername,
			&snapshot.Platform,
			&snapshot.AccountLevel,
			&snapshot.APIKeyName,
			&termsRaw,
			&snapshot.AccountID,
			&snapshot.AccountName,
			&snapshot.AccountConcurrency,
			&snapshot.SnapshotQuality,
		); err != nil {
			return nil, err
		}
		snapshot.ListingRevisionID = sqlNullInt64Ptr(listingRevisionID)
		snapshot.ListingVersion = sqlNullInt64Ptr(listingVersion)
		snapshot.RoomName = strings.TrimSpace(snapshot.RoomName)
		snapshot.OwnerUsername = strings.TrimSpace(snapshot.OwnerUsername)
		snapshot.Platform = strings.ToLower(strings.TrimSpace(snapshot.Platform))
		snapshot.AccountLevel = service.NormalizeAccountLevel(snapshot.AccountLevel)
		snapshot.APIKeyName = strings.TrimSpace(snapshot.APIKeyName)
		snapshot.AccountName = strings.TrimSpace(snapshot.AccountName)
		snapshot.SnapshotQuality = normalizeAccountShareSnapshotQuality(snapshot.SnapshotQuality)
		if err := validateAccountShareSnapshotQuality(snapshot.MembershipID, snapshot.SnapshotQuality); err != nil {
			return nil, err
		}
		terms, err := decodeAccountShareMembershipTermsSnapshot(
			snapshot.MembershipID,
			snapshot.ListingRevisionID,
			snapshot.ListingVersion,
			termsRaw,
		)
		if err != nil {
			return nil, err
		}
		snapshot.Terms = terms
		snapshots[snapshot.MembershipID] = snapshot
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return snapshots, nil
}

func (r *accountShareModeRepository) applyAccountShareHistorySnapshots(
	ctx context.Context,
	consumerUserID int64,
	listings []service.AccountShareListing,
) error {
	membershipIDs := make([]int64, 0, len(listings))
	seen := make(map[int64]struct{}, len(listings))
	for i := range listings {
		if listings[i].LastUsedMembershipID == nil || *listings[i].LastUsedMembershipID <= 0 {
			return fmt.Errorf("account share history listing %d has no ended membership identity", listings[i].ID)
		}
		membershipID := *listings[i].LastUsedMembershipID
		if _, exists := seen[membershipID]; exists {
			continue
		}
		seen[membershipID] = struct{}{}
		membershipIDs = append(membershipIDs, membershipID)
	}
	if len(membershipIDs) == 0 {
		return nil
	}
	snapshots, err := r.loadAccountShareMembershipHistorySnapshots(ctx, consumerUserID, membershipIDs)
	if err != nil {
		return err
	}
	for i := range listings {
		membershipID := *listings[i].LastUsedMembershipID
		snapshot, ok := snapshots[membershipID]
		if !ok || snapshot.ListingID != listings[i].ID {
			return fmt.Errorf(
				"account share history listing %d membership %d snapshot is unavailable",
				listings[i].ID,
				membershipID,
			)
		}
		listings[i].HistorySnapshotQuality = snapshot.SnapshotQuality
		if snapshot.SnapshotQuality == service.AccountShareSnapshotQualityUnknown {
			clearUntrustedAccountShareHistoryProjection(&listings[i])
		}
		if snapshot.ListingRevisionID != nil {
			revisionID := *snapshot.ListingRevisionID
			listings[i].CurrentRevisionID = &revisionID
		}
		if snapshot.ListingVersion != nil {
			listings[i].RowVersion = *snapshot.ListingVersion
		}
		if snapshot.RoomName != "" {
			listings[i].RoomName = snapshot.RoomName
		}
		if snapshot.OwnerUserID > 0 {
			listings[i].OwnerUserID = snapshot.OwnerUserID
		}
		if snapshot.OwnerUsername != "" {
			listings[i].OwnerUsername = snapshot.OwnerUsername
		}
		if snapshot.Platform != "" {
			listings[i].Platform = snapshot.Platform
		}
		if snapshot.AccountLevel != "" {
			listings[i].AccountLevel = snapshot.AccountLevel
		}
		if snapshot.AccountID > 0 {
			listings[i].AccountID = snapshot.AccountID
		}
		if snapshot.AccountName != "" {
			listings[i].AccountName = snapshot.AccountName
		}
		if snapshot.AccountConcurrency > 0 {
			listings[i].AccountConcurrency = snapshot.AccountConcurrency
		}
		if snapshot.Terms != nil {
			terms := snapshot.Terms
			listings[i].RoomName = terms.RoomName
			listings[i].Status = terms.Status
			listings[i].SeatLimit = terms.SeatLimit
			listings[i].RateMultiplier = terms.RateMultiplier
			listings[i].AllowedModels = append([]string(nil), terms.AllowedModels...)
			listings[i].PerUserConcurrency = terms.PerUserConcurrency
			listings[i].HourlyRate = terms.HourlyRate
			listings[i].HourlyFeeWaiverMinimum = terms.HourlyFeeWaiverMinimum
			listings[i].MinBalanceRequired = terms.MinBalanceRequired
			listings[i].CodexCLIOnly = terms.CodexCLIOnly
			listings[i].Codex5hLimitPercent = terms.Codex5hLimitPercent
			listings[i].Codex7dLimitPercent = terms.Codex7dLimitPercent
			listings[i].Anthropic5hLimitPercent = terms.Anthropic5hLimitPercent
			listings[i].Anthropic7dLimitPercent = terms.Anthropic7dLimitPercent
		}
	}
	return nil
}

// clearUntrustedAccountShareHistoryProjection removes values inherited from the
// mutable listing projection before applying any immutable membership fields
// that survived from a pre-snapshot record. This prevents final/current room
// state from being presented as the consumer's historical terms.
func clearUntrustedAccountShareHistoryProjection(listing *service.AccountShareListing) {
	if listing == nil {
		return
	}
	listing.RowVersion = 0
	listing.CurrentRevisionID = nil
	listing.RoomName = ""
	listing.Platform = ""
	listing.OwnerUserID = 0
	listing.OwnerUsername = ""
	listing.AccountID = 0
	listing.AccountName = ""
	listing.AccountIdentityID = nil
	listing.Status = ""
	listing.SeatLimit = 0
	listing.RatingCount = 0
	listing.RatingScoreSum = 0
	listing.RatingAvg = 0
	listing.RateMultiplier = 0
	listing.AllowedModels = []string{}
	listing.PerUserConcurrency = 0
	listing.AccountConcurrency = 0
	listing.HourlyRate = 0
	listing.HourlyFeeWaiverMinimum = 0
	listing.MinBalanceRequired = 0
	listing.CodexCLIOnly = false
	listing.Codex5hLimitPercent = 0
	listing.Codex7dLimitPercent = 0
	listing.Anthropic5hLimitPercent = 0
	listing.Anthropic7dLimitPercent = 0
	listing.AccountLevel = ""
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
	membership, err := r.resolveMySpendMembership(ctx, query.ListingID, query.ConsumerID, query.MembershipID)
	if err != nil {
		return nil, err
	}
	if membership == nil {
		return nil, service.ErrAccountShareListingNotFound
	}
	listing, err := r.getMySpendListing(ctx, query.ListingID, query.ConsumerID, membership)
	if err != nil {
		return nil, err
	}
	startTime := query.StartTime
	endTime := query.EndTime
	filterMembershipID := int64(0)
	if query.Range == service.AccountShareSpendRangeCurrentMembership {
		filterMembershipID = membership.ID
		startTime = membership.JoinedAt
		if membership.EndedAt != nil && membership.EndedAt.Before(endTime) {
			endTime = *membership.EndedAt
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

func (r *accountShareModeRepository) getMySpendListing(
	ctx context.Context,
	listingID int64,
	consumerUserID int64,
	membership *service.AccountShareMySpendMembership,
) (*service.AccountShareMySpendListing, error) {
	if membership == nil || membership.ID <= 0 {
		return nil, service.ErrAccountShareListingNotFound
	}
	snapshots, err := r.loadAccountShareMembershipHistorySnapshots(
		ctx,
		consumerUserID,
		[]int64{membership.ID},
	)
	if err != nil {
		return nil, err
	}
	snapshot, ok := snapshots[membership.ID]
	if !ok || snapshot.ListingID != listingID || snapshot.OwnerUserID <= 0 || snapshot.Platform == "" {
		return nil, service.ErrAccountShareListingNotFound
	}
	if snapshot.APIKeyName != "" {
		membership.APIKeyName = snapshot.APIKeyName
	}
	return &service.AccountShareMySpendListing{
		ID:            snapshot.ListingID,
		AccountID:     snapshot.AccountID,
		AccountName:   snapshot.AccountName,
		Platform:      snapshot.Platform,
		OwnerUserID:   snapshot.OwnerUserID,
		OwnerUsername: snapshot.OwnerUsername,
	}, nil
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
			COALESCE(NULLIF(m.api_key_name_snapshot, ''), NULLIF(ak.name, ''), '') AS api_key_name,
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
	whereSQL, args := accountShareMySpendSettlementWhere(listingID, consumerID, membershipID, summary.StartTime, summary.EndTime)
	query := fmt.Sprintf(`
		SELECT
			COUNT(entry.id)::bigint,
			COALESCE(SUM(ul.input_tokens), 0)::bigint,
			COALESCE(SUM(ul.output_tokens), 0)::bigint,
			COALESCE(SUM(ul.cache_creation_tokens), 0)::bigint,
			COALESCE(SUM(ul.cache_read_tokens), 0)::bigint,
			COALESCE(SUM(entry.base_charge), 0)::double precision,
			MAX(entry.created_at)
		FROM account_share_mode_settlement_entries entry
		LEFT JOIN usage_logs ul ON ul.id = entry.usage_log_id
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
	whereSQL, args := accountShareMySpendLedgerWhere(
		listingID,
		consumerID,
		membershipID,
		summary.StartTime,
		summary.EndTime,
	)
	query := fmt.Sprintf(`
		SELECT
			COALESCE(SUM(ubl.amount) FILTER (WHERE ubl.direction = 'debit' AND ubl.reason = $2), 0)::double precision,
			COALESCE(SUM(ubl.amount) FILTER (WHERE ubl.direction = 'credit' AND ubl.reason = $3), 0)::double precision,
			COALESCE(SUM(ubl.amount) FILTER (WHERE ubl.direction = 'credit' AND ubl.reason = $4), 0)::double precision
		FROM user_balance_ledger ubl
		WHERE %s
	`, whereSQL)
	return r.db.QueryRowContext(ctx, query, args...).Scan(
		&summary.HourlyCharge,
		&summary.HourlyRefund,
		&summary.HourlyWaiverRefund,
	)
}

func accountShareMySpendLedgerWhere(listingID, consumerID, membershipID int64, startTime, endTime time.Time) (string, []any) {
	where := []string{
		"ubl.user_id = $1",
		"ubl.reason IN ($2, $3, $4)",
	}
	args := []any{
		consumerID,
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
	} else {
		where = append(
			where,
			fmt.Sprintf("ubl.created_at >= $%d", next),
			fmt.Sprintf("ubl.created_at < $%d", next+1),
		)
		args = append(args, startTime, endTime)
	}
	return strings.Join(where, " AND "), args
}

func (r *accountShareModeRepository) listMySpendModelBreakdown(ctx context.Context, listingID, consumerID, membershipID int64, startTime, endTime time.Time) ([]service.AccountShareMySpendModelBreakdown, error) {
	whereSQL, args := accountShareMySpendSettlementWhere(listingID, consumerID, membershipID, startTime, endTime)
	query := fmt.Sprintf(`
		SELECT
			COALESCE(NULLIF(ul.model, ''), 'unknown') AS model,
			COUNT(entry.id)::bigint,
			COALESCE(SUM(ul.input_tokens), 0)::bigint,
			COALESCE(SUM(ul.output_tokens), 0)::bigint,
			COALESCE(SUM(ul.cache_creation_tokens), 0)::bigint,
			COALESCE(SUM(ul.cache_read_tokens), 0)::bigint,
			COALESCE(SUM(entry.base_charge), 0)::double precision
		FROM account_share_mode_settlement_entries entry
		LEFT JOIN usage_logs ul ON ul.id = entry.usage_log_id
		WHERE %s
		GROUP BY COALESCE(NULLIF(ul.model, ''), 'unknown')
		ORDER BY COALESCE(SUM(entry.base_charge), 0) DESC, COUNT(entry.id) DESC, model ASC
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

func accountShareMySpendSettlementWhere(listingID, consumerID, membershipID int64, startTime, endTime time.Time) (string, []any) {
	args := []any{listingID, consumerID}
	where := []string{
		"entry.listing_id = $1",
		"entry.consumer_user_id = $2",
		"entry.settlement_type = 'usage_request'",
	}
	if membershipID > 0 {
		args = append(args, membershipID)
		where = append(where, fmt.Sprintf("entry.membership_id = $%d", len(args)))
	} else {
		args = append(args, startTime, endTime)
		where = append(
			where,
			"entry.created_at >= $3",
			"entry.created_at < $4",
		)
	}
	return strings.Join(where, " AND "), args
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
	var activeEditSession sql.NullString
	var editingByUserID sql.NullInt64
	var editingExpiresAt sql.NullTime
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
			l.edit_session_id,
			l.editing_by_user_id,
			l.editing_expires_at,
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
		&activeEditSession,
		&editingByUserID,
		&editingExpiresAt,
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

	now := time.Now().UTC()
	activeEdit := activeEditSession.Valid && editingExpiresAt.Valid && editingExpiresAt.Time.After(now)
	editLockMine := activeEdit && editingByUserID.Valid && editingByUserID.Int64 == actorUserID
	sessionProvided := strings.TrimSpace(input.EditSessionID) != ""
	// 编辑锁只用于「不同用户之间」互斥：别人持锁一律拒绝。
	// 自己持锁时不再强制要求带上 session id —— 免锁的消费者安全更新恒不带 session，
	// 旧写法会让房主自己十分钟前留下的残留锁把这条路整个打死（连只改房间名都保存不了）。
	// 同一房间的并发写由 expected_version 乐观锁兜底。
	if activeEdit && !editLockMine {
		return nil, service.ErrAccountShareListingEditing
	}
	// 锁已过期时不在这里拦：让它落到下面的 editSessionHeld 判定，合约变更会拿到
	// ACCOUNT_SHARE_EDIT_SESSION_INVALID（可自愈：关窗重进编辑），纯改名则照常放行。
	// 在这里拦会把「自己的会话续期失败后过期」误报成「别人正在编辑」，用户等谁都等不到。
	if activeEdit && sessionProvided && activeEditSession.String != input.EditSessionID {
		return nil, service.ErrAccountShareListingEditing
	}
	// 走加锁路径（合约变更且不是消费者安全更新）时必须真正握着自己的有效编辑会话，
	// 不能靠「库里恰好有一把残留锁」蒙混过关。
	editSessionHeld := editLockMine && sessionProvided && activeEditSession.String == input.EditSessionID
	var currentAllowedModels []string
	if err := json.Unmarshal(currentAllowedModelsRaw, &currentAllowedModels); err != nil {
		return nil, err
	}
	contractUpdate := accountShareListingConfigUpdateRequiresEditSession(input)
	consumerSafeUpdate := false
	if contractUpdate && strings.TrimSpace(input.EditSessionID) == "" && !input.ForceActiveEdit {
		// 免锁的「消费者安全更新」同样受房间生命周期状态约束。
		// 状态门禁原本只写在下面 !consumerSafeUpdate 的分支里，免锁路径整个绕过它 ——
		// 在这条路径此前不可达时无害，一旦放通就意味着 suspended（风控挂起）、draining
		// 的房间也能被房主改合约字段并 bump row_version，等于风控挂起不再冻结配置。
		if !accountShareOwnerEditableStatus(currentStatus) {
			return nil, service.ErrAccountShareUpdateRequiresPaused
		}
		consumerSafeUpdate, err = accountShareListingUpdateProtectsConsumers(
			ctx,
			tx,
			listingID,
			input,
			accountShareListingConsumerTerms{
				rateMultiplier:     currentRateMultiplier,
				allowedModels:      currentAllowedModels,
				perUserConcurrency: currentPerUserConcurrency,
				hourlyRate:         currentHourlyRate,
				feeWaiverMinimum:   currentHourlyFeeWaiverMinimum,
				minBalanceRequired: currentMinBalanceRequired,
			},
		)
		if err != nil {
			return nil, err
		}
	}
	if contractUpdate && !consumerSafeUpdate {
		if !editSessionHeld {
			return nil, service.ErrAccountShareEditSessionInvalid
		}
		if actorIsAdmin && input.ForceActiveEdit {
			if !accountShareAdminForceEditableStatus(currentStatus) {
				return nil, service.ErrAccountShareRoomOperationConflict.WithMetadata(map[string]string{
					"blocker": "lifecycle_status",
					"status":  currentStatus,
				})
			}
		} else {
			if !accountShareOwnerEditableStatus(currentStatus) {
				return nil, service.ErrAccountShareUpdateRequiresPaused
			}
			blockers, err := accountShareListingEditBlockersInTx(ctx, tx, listingID)
			if err != nil {
				return nil, err
			}
			if blockers.Any() {
				return nil, service.ErrAccountShareListingInUse.WithMetadata(blockers.Metadata())
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
	if len(changedFields) == 0 {
		return nil, service.ErrAccountShareRoomNoChanges
	}
	if contractUpdate && !consumerSafeUpdate {
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
					WHERE membership.status IN ('active', 'queued', 'ending')
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
			if account.IsModelSupported(model) {
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
		input.Anthropic7dLimitPercent != nil
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
	var listingStatus string
	var activeSession sql.NullString
	var editingByUserID sql.NullInt64
	var editingExpiresAt sql.NullTime
	var pendingOperationID sql.NullString
	ownerPredicate := ""
	selectArgs := []any{listingID}
	if !actorIsAdmin {
		selectArgs = append(selectArgs, actorUserID)
		ownerPredicate = fmt.Sprintf("AND l.owner_user_id = $%d", len(selectArgs))
	}
	selectQuery := fmt.Sprintf(`
		SELECT l.owner_user_id, l.status, l.edit_session_id, l.editing_by_user_id, l.editing_expires_at,
			l.pending_operation_id
		FROM account_share_listings l
		%s
		WHERE l.id = $1
			%s
			AND l.deleted_at IS NULL
		FOR UPDATE OF l
	`, accountShareRoomRepresentativeJoinSQL("NOW()"), ownerPredicate)
	if err := tx.QueryRowContext(ctx, selectQuery, selectArgs...).Scan(
		&ownerUserID,
		&listingStatus,
		&activeSession,
		&editingByUserID,
		&editingExpiresAt,
		&pendingOperationID,
	); errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	} else if err != nil {
		return nil, err
	}
	if pendingOperationID.Valid {
		return nil, service.ErrAccountShareRoomOperationConflict.WithMetadata(map[string]string{
			"operation_id": pendingOperationID.String,
		})
	}

	now := time.Now().UTC()
	if activeSession.Valid && editingExpiresAt.Valid && editingExpiresAt.Time.After(now) &&
		(activeSession.String != sessionID || !editingByUserID.Valid || editingByUserID.Int64 != actorUserID) {
		return nil, service.ErrAccountShareListingEditing
	}

	if actorIsAdmin && input.Force {
		if !accountShareAdminForceEditableStatus(listingStatus) {
			return nil, service.ErrAccountShareRoomOperationConflict.WithMetadata(map[string]string{
				"blocker": "lifecycle_status",
				"status":  listingStatus,
			})
		}
	} else {
		if !accountShareOwnerEditableStatus(listingStatus) {
			return nil, service.ErrAccountShareUpdateRequiresPaused
		}
		blockers, err := accountShareListingEditBlockersInTx(ctx, tx, listingID)
		if err != nil {
			return nil, err
		}
		if blockers.Any() {
			return nil, service.ErrAccountShareListingInUse.WithMetadata(blockers.Metadata())
		}
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

func loadAccountShareMembershipTraceSnapshotInTx(ctx context.Context, tx *sql.Tx, membership *service.AccountShareMembership) error {
	if tx == nil || membership == nil || membership.ID <= 0 {
		return nil
	}
	var listingRevisionID, listingVersionSnapshot, ownerUserIDSnapshot sql.NullInt64
	var roomName, ownerUsername, platform, accountLevel, apiKeyName, snapshotQuality, endingReason, settlementStatus sql.NullString
	var endingRequestedAt sql.NullTime
	var termsSnapshotRaw []byte
	err := tx.QueryRowContext(ctx, `
		SELECT
			listing_revision_id, listing_version_snapshot, room_name_snapshot,
			owner_user_id_snapshot, owner_username_snapshot, platform_snapshot,
			account_level_snapshot, api_key_name_snapshot, terms_snapshot,
			snapshot_quality, ending_requested_at, ending_reason, settlement_status
		FROM account_share_memberships
		WHERE id = $1
			AND deleted_at IS NULL
	`, membership.ID).Scan(
		&listingRevisionID,
		&listingVersionSnapshot,
		&roomName,
		&ownerUserIDSnapshot,
		&ownerUsername,
		&platform,
		&accountLevel,
		&apiKeyName,
		&termsSnapshotRaw,
		&snapshotQuality,
		&endingRequestedAt,
		&endingReason,
		&settlementStatus,
	)
	if err != nil {
		return err
	}
	membership.ListingRevisionID = sqlNullInt64Ptr(listingRevisionID)
	membership.ListingVersionSnapshot = sqlNullInt64Ptr(listingVersionSnapshot)
	membership.RoomNameSnapshot = strings.TrimSpace(roomName.String)
	membership.OwnerUserIDSnapshot = sqlNullInt64Ptr(ownerUserIDSnapshot)
	membership.OwnerUsernameSnapshot = strings.TrimSpace(ownerUsername.String)
	membership.PlatformSnapshot = strings.ToLower(strings.TrimSpace(platform.String))
	membership.AccountLevelSnapshot = service.NormalizeAccountLevel(accountLevel.String)
	membership.APIKeyNameSnapshot = strings.TrimSpace(apiKeyName.String)
	membership.SnapshotQuality = strings.TrimSpace(snapshotQuality.String)
	membership.EndingReason = strings.TrimSpace(endingReason.String)
	membership.SettlementStatus = strings.TrimSpace(settlementStatus.String)
	membership.EndingRequestedAt = sqlNullTimePtr(endingRequestedAt)
	if len(termsSnapshotRaw) > 0 {
		var terms service.AccountShareListingTermsSnapshot
		if err := json.Unmarshal(termsSnapshotRaw, &terms); err != nil {
			return err
		}
		normalizeAccountShareListingTermsAliases(&terms)
		membership.TermsSnapshot = &terms
	}
	return nil
}

func normalizeAccountShareListingTermsAliases(terms *service.AccountShareListingTermsSnapshot) {
	if terms == nil {
		return
	}
	// Older immutable membership snapshots predate the explicit Anthropic
	// aliases. Both providers share the same persisted quota threshold
	// columns, so hydrate the aliases without changing the contract value.
	if terms.Anthropic5hLimitPercent <= 0 {
		terms.Anthropic5hLimitPercent = terms.Codex5hLimitPercent
	}
	if terms.Anthropic7dLimitPercent <= 0 {
		terms.Anthropic7dLimitPercent = terms.Codex7dLimitPercent
	}
}

func loadAndValidateAccountShareMembershipTermsSnapshotInTx(
	ctx context.Context,
	tx *sql.Tx,
	membership *service.AccountShareMembership,
) error {
	if tx == nil || membership == nil || membership.ID <= 0 || membership.ListingID <= 0 ||
		(membership.Status != service.AccountShareMembershipStatusActive &&
			membership.Status != service.AccountShareMembershipStatusQueued) {
		return fmt.Errorf(
			"%w: invalid membership terms snapshot input",
			service.ErrAccountShareBillingBindingUnavailable,
		)
	}
	if err := loadAccountShareMembershipTraceSnapshotInTx(ctx, tx, membership); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf(
				"%w: membership %d immutable snapshot is missing",
				service.ErrAccountShareBillingBindingUnavailable,
				membership.ID,
			)
		}
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &syntaxErr) || errors.As(err, &typeErr) {
			return fmt.Errorf(
				"%w: membership %d immutable terms snapshot is malformed: %v",
				service.ErrAccountShareBillingBindingUnavailable,
				membership.ID,
				err,
			)
		}
		return err
	}

	terms := membership.TermsSnapshot
	if membership.ListingRevisionID == nil || *membership.ListingRevisionID <= 0 ||
		membership.ListingVersionSnapshot == nil || *membership.ListingVersionSnapshot <= 0 ||
		terms == nil ||
		terms.ListingRevisionID != *membership.ListingRevisionID ||
		terms.RowVersion != *membership.ListingVersionSnapshot ||
		terms.SchemaVersion <= 0 {
		return fmt.Errorf(
			"%w: membership %d immutable terms snapshot does not match its listing revision",
			service.ErrAccountShareBillingBindingUnavailable,
			membership.ID,
		)
	}
	return nil
}

func validateAccountShareMembershipTermsRevisionInTx(
	ctx context.Context,
	tx *sql.Tx,
	membership *service.AccountShareMembership,
) error {
	if tx == nil || membership == nil || membership.TermsSnapshot == nil ||
		membership.ListingRevisionID == nil || membership.ListingVersionSnapshot == nil {
		return fmt.Errorf(
			"%w: membership immutable terms revision is unavailable",
			service.ErrAccountShareBillingBindingUnavailable,
		)
	}
	terms := membership.TermsSnapshot
	revision, err := loadAccountShareListingRevisionSnapshotInTx(
		ctx,
		tx,
		membership.ListingID,
		*membership.ListingRevisionID,
	)
	if errors.Is(err, service.ErrAccountShareListingNotFound) {
		return fmt.Errorf(
			"%w: membership %d immutable listing revision is missing",
			service.ErrAccountShareBillingBindingUnavailable,
			membership.ID,
		)
	}
	if err != nil {
		return err
	}
	if !accountShareMembershipTermsMatchRevision(terms, revision) {
		return fmt.Errorf(
			"%w: membership %d immutable terms do not match the listing revision",
			service.ErrAccountShareBillingBindingUnavailable,
			membership.ID,
		)
	}
	return nil
}

func accountShareMembershipTermsMatchRevision(
	terms *service.AccountShareListingTermsSnapshot,
	revision *accountShareListingRevisionSnapshot,
) bool {
	if terms == nil || revision == nil {
		return false
	}
	return terms.ListingRevisionID == revision.ID &&
		terms.RowVersion == revision.RowVersion &&
		terms.SchemaVersion == revision.SchemaVersion &&
		terms.RoomName == revision.RoomName &&
		terms.Status == revision.Status &&
		terms.SeatLimit == revision.SeatLimit &&
		terms.RateMultiplier == revision.RateMultiplier &&
		equalNormalizedAccountShareModels(terms.AllowedModels, revision.AllowedModels) &&
		terms.PerUserConcurrency == revision.PerUserConcurrency &&
		terms.HourlyRate == revision.HourlyRate &&
		terms.HourlyFeeWaiverMinimum == revision.HourlyFeeWaiverMinimum &&
		terms.MinBalanceRequired == revision.MinBalanceRequired &&
		terms.CodexCLIOnly == revision.CodexCLIOnly &&
		terms.Codex5hLimitPercent == revision.Codex5hLimitPercent &&
		terms.Codex7dLimitPercent == revision.Codex7dLimitPercent &&
		terms.Anthropic5hLimitPercent == revision.Codex5hLimitPercent &&
		terms.Anthropic7dLimitPercent == revision.Codex7dLimitPercent
}

func validateAccountShareMembershipOpenRuntimeBindingInTx(
	ctx context.Context,
	tx *sql.Tx,
	membership *service.AccountShareMembership,
) error {
	if tx == nil || membership == nil || membership.ID <= 0 ||
		membership.ListingID <= 0 || membership.AccountID <= 0 ||
		membership.Status != service.AccountShareMembershipStatusActive ||
		membership.TermsSnapshot == nil || membership.ListingRevisionID == nil {
		return fmt.Errorf(
			"%w: invalid active membership runtime binding input",
			service.ErrAccountShareBillingBindingUnavailable,
		)
	}

	terms := membership.TermsSnapshot
	var bindingRevisionID, termsRevisionNumber int64
	err := tx.QueryRowContext(ctx, `
		SELECT
			binding.listing_revision_id,
			binding.terms_revision_number
		FROM account_share_memberships current_membership
		JOIN account_share_membership_account_bindings binding
			ON binding.membership_id = current_membership.id
			AND binding.listing_id = current_membership.listing_id
			AND binding.account_id = current_membership.account_id
			AND binding.account_id_snapshot = current_membership.account_id
			AND binding.listing_revision_id = current_membership.listing_revision_id
			AND binding.unbound_at IS NULL
		JOIN account_share_listing_revisions revision
			ON revision.listing_id = binding.listing_id
			AND revision.id = binding.listing_revision_id
			AND revision.revision_number = binding.terms_revision_number
		WHERE current_membership.id = $1
			AND current_membership.listing_id = $2
			AND current_membership.account_id = $3
			AND current_membership.listing_revision_id = $4
			AND current_membership.status = $5
			AND current_membership.deleted_at IS NULL
	`,
		membership.ID,
		membership.ListingID,
		membership.AccountID,
		*membership.ListingRevisionID,
		service.AccountShareMembershipStatusActive,
	).Scan(&bindingRevisionID, &termsRevisionNumber)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf(
			"%w: membership %d has no matching open immutable binding",
			service.ErrAccountShareBillingBindingUnavailable,
			membership.ID,
		)
	}
	if err != nil {
		return err
	}
	if bindingRevisionID != terms.ListingRevisionID ||
		termsRevisionNumber != terms.RowVersion {
		return fmt.Errorf(
			"%w: membership %d binding revision does not match its immutable terms snapshot",
			service.ErrAccountShareBillingBindingUnavailable,
			membership.ID,
		)
	}
	return nil
}

func loadAndValidateAccountShareMembershipRuntimeSnapshotInTx(
	ctx context.Context,
	tx *sql.Tx,
	membership *service.AccountShareMembership,
) error {
	if err := loadAndValidateAccountShareMembershipTermsSnapshotInTx(ctx, tx, membership); err != nil {
		return err
	}
	if err := validateAccountShareMembershipTermsRevisionInTx(ctx, tx, membership); err != nil {
		return err
	}
	return validateAccountShareMembershipOpenRuntimeBindingInTx(ctx, tx, membership)
}

func applyAccountShareMembershipRuntimeTerms(
	membership *service.AccountShareMembership,
	listing *service.AccountShareListing,
) error {
	if membership == nil || listing == nil || membership.TermsSnapshot == nil ||
		membership.ListingRevisionID == nil ||
		membership.TermsSnapshot.ListingRevisionID != *membership.ListingRevisionID {
		return fmt.Errorf(
			"%w: immutable membership terms cannot be applied to the runtime listing",
			service.ErrAccountShareBillingBindingUnavailable,
		)
	}
	terms := membership.TermsSnapshot
	listing.RateMultiplier = terms.RateMultiplier
	listing.AllowedModels = append([]string(nil), terms.AllowedModels...)
	listing.PerUserConcurrency = terms.PerUserConcurrency
	listing.HourlyRate = terms.HourlyRate
	listing.HourlyFeeWaiverMinimum = terms.HourlyFeeWaiverMinimum
	listing.MinBalanceRequired = terms.MinBalanceRequired
	listing.CodexCLIOnly = terms.CodexCLIOnly
	listing.Codex5hLimitPercent = terms.Codex5hLimitPercent
	listing.Codex7dLimitPercent = terms.Codex7dLimitPercent
	listing.Anthropic5hLimitPercent = terms.Anthropic5hLimitPercent
	listing.Anthropic7dLimitPercent = terms.Anthropic7dLimitPercent
	return nil
}

func ensureAccountShareMembershipBindingAssignmentInTx(
	ctx context.Context,
	tx *sql.Tx,
	listingID int64,
	accountID int64,
) error {
	snapshot := accountShareRoomAssignmentSnapshot{}
	var projectionCreatedAt time.Time
	err := tx.QueryRowContext(ctx, `
		SELECT
			room_account.listing_id,
			room_account.account_id,
			room_account.owner_user_id,
			account.name,
			account.platform,
			account.account_level,
			account.concurrency,
			room_account.created_at
		FROM account_share_room_accounts room_account
		JOIN accounts account
			ON account.id = room_account.account_id
			AND account.owner_user_id = room_account.owner_user_id
			AND account.deleted_at IS NULL
		WHERE room_account.listing_id = $1
			AND room_account.account_id = $2
			AND room_account.state = 'active'
		FOR UPDATE OF room_account, account
	`, listingID, accountID).Scan(
		&snapshot.ListingID,
		&snapshot.AccountID,
		&snapshot.OwnerUserID,
		&snapshot.AccountName,
		&snapshot.Platform,
		&snapshot.AccountLevel,
		&snapshot.ConfiguredConcurrency,
		&projectionCreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf(
			"account share membership cannot bind account %d: active room projection is missing",
			accountID,
		)
	}
	if err != nil {
		return err
	}
	if projectionCreatedAt.IsZero() {
		return fmt.Errorf(
			"account share room account %d in listing %d has no trustworthy projection timestamp",
			accountID,
			listingID,
		)
	}
	snapshot.Platform = strings.ToLower(strings.TrimSpace(snapshot.Platform))
	snapshot.AccountLevel = service.NormalizeAccountLevel(snapshot.AccountLevel)

	assignments, err := lockAccountShareRoomOpenAssignmentsInTx(ctx, tx, []int64{accountID})
	if err != nil {
		return err
	}
	if assignment, hasAssignment := assignments[accountID]; hasAssignment {
		if assignment.ListingID != listingID {
			return service.ErrAccountShareRoomAccountConflict
		}
		return nil
	}
	_, err = insertBackfilledAccountShareRoomAssignmentInTx(
		ctx,
		tx,
		snapshot,
		projectionCreatedAt,
	)
	return err
}

func (r *accountShareModeRepository) createAccountShareMembershipBindingInTx(
	ctx context.Context,
	tx *sql.Tx,
	membershipID int64,
	listingID int64,
	accountID int64,
	listingRevisionID int64,
	boundByUserID int64,
	boundByRole string,
	bindReason string,
	boundAt time.Time,
) (int64, int64, error) {
	boundByRole = strings.TrimSpace(boundByRole)
	bindReason = strings.TrimSpace(bindReason)
	if r == nil || tx == nil ||
		membershipID <= 0 || listingID <= 0 || accountID <= 0 || listingRevisionID <= 0 ||
		!accountShareBindingActorRoleValid(boundByRole) || bindReason == "" || boundAt.IsZero() {
		return 0, 0, fmt.Errorf("invalid account-share membership binding input")
	}
	if err := ensureAccountShareMembershipBindingAssignmentInTx(
		ctx,
		tx,
		listingID,
		accountID,
	); err != nil {
		return 0, 0, err
	}

	var bindingID, routingGeneration int64
	err := tx.QueryRowContext(ctx, `
		WITH binding_source AS MATERIALIZED (
			SELECT
				assignment.id AS room_account_assignment_id,
				assignment.account_name_snapshot,
				assignment.platform_snapshot,
				assignment.account_level_snapshot,
				assignment.configured_concurrency_snapshot,
				assignment.snapshot_quality,
				revision.revision_number AS terms_revision_number
			FROM account_share_memberships membership
			JOIN account_share_room_accounts room_account
				ON room_account.listing_id = membership.listing_id
				AND room_account.account_id = membership.account_id
				AND room_account.state = 'active'
			JOIN account_share_room_account_assignments assignment
				ON assignment.listing_id = room_account.listing_id
				AND assignment.account_id_snapshot = room_account.account_id
				AND assignment.account_id = room_account.account_id
				AND assignment.detached_at IS NULL
			JOIN accounts bound_account
				ON bound_account.id = room_account.account_id
				AND bound_account.deleted_at IS NULL
			JOIN account_share_listing_revisions revision
				ON revision.listing_id = membership.listing_id
				AND revision.id = membership.listing_revision_id
			WHERE membership.id = $1
				AND membership.listing_id = $2
				AND membership.account_id = $3
				AND membership.listing_revision_id = $4
				AND membership.status IN ('active', 'ending')
				AND membership.deleted_at IS NULL
			FOR UPDATE OF assignment
		),
		next_generation AS MATERIALIZED (
			SELECT COALESCE(MAX(binding.routing_generation), 0) + 1 AS routing_generation
			FROM account_share_membership_account_bindings binding
			WHERE binding.membership_id = $1
		)
		INSERT INTO account_share_membership_account_bindings (
			membership_id, listing_id, account_id, account_id_snapshot,
			room_account_assignment_id, listing_revision_id, terms_revision_number,
			account_name_snapshot, platform_snapshot, account_level_snapshot,
			configured_concurrency_snapshot, routing_generation,
			bound_at, bound_by_user_id, bound_by_role, bind_reason,
			snapshot_quality, created_at
		)
		SELECT
			$1, $2, $3, $3,
			source.room_account_assignment_id, $4, source.terms_revision_number,
			source.account_name_snapshot, source.platform_snapshot, source.account_level_snapshot,
			source.configured_concurrency_snapshot, generation.routing_generation,
			$5, $6, $7, $8,
			source.snapshot_quality, $5
		FROM binding_source source
		CROSS JOIN next_generation generation
		RETURNING id, routing_generation
	`,
		membershipID,
		listingID,
		accountID,
		listingRevisionID,
		boundAt.UTC(),
		nullablePositiveInt64(boundByUserID),
		boundByRole,
		bindReason,
	).Scan(&bindingID, &routingGeneration)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, fmt.Errorf(
			"account share membership %d cannot bind account %d: active assignment or revision snapshot is missing",
			membershipID,
			accountID,
		)
	}
	if err != nil {
		return 0, 0, err
	}
	if bindingID <= 0 || routingGeneration <= 0 {
		return 0, 0, fmt.Errorf(
			"account share membership %d produced invalid binding id=%d generation=%d",
			membershipID,
			bindingID,
			routingGeneration,
		)
	}
	return bindingID, routingGeneration, nil
}

func (r *accountShareModeRepository) closeAccountShareMembershipBindingInTx(
	ctx context.Context,
	tx *sql.Tx,
	membershipID int64,
	unboundByUserID int64,
	unboundByRole string,
	unbindReason string,
	unboundAt time.Time,
) (bool, error) {
	unboundByRole = strings.TrimSpace(unboundByRole)
	unbindReason = strings.TrimSpace(unbindReason)
	if r == nil || tx == nil || membershipID <= 0 ||
		!accountShareBindingActorRoleValid(unboundByRole) || unbindReason == "" || unboundAt.IsZero() {
		return false, fmt.Errorf("invalid account-share membership unbinding input")
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE account_share_membership_account_bindings
		SET unbound_at = $1,
			unbound_by_user_id = $2,
			unbound_by_role = $3,
			unbind_reason = $4
		WHERE membership_id = $5
			AND unbound_at IS NULL
	`,
		unboundAt.UTC(),
		nullablePositiveInt64(unboundByUserID),
		unboundByRole,
		unbindReason,
		membershipID,
	)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected > 1 {
		return false, fmt.Errorf(
			"close account share membership %d binding affected %d rows",
			membershipID,
			affected,
		)
	}
	return affected == 1, nil
}

func accountShareBindingActorRoleValid(role string) bool {
	switch strings.TrimSpace(role) {
	case "owner", "consumer", "admin", "system":
		return true
	default:
		return false
	}
}

func (r *accountShareModeRepository) JoinListing(ctx context.Context, input service.AccountShareJoinRepositoryInput) (*service.AccountShareMembership, error) {
	consumerUserID := input.ConsumerUserID
	apiKeyID := input.APIKeyID
	listingID := input.ListingID
	idleTimeoutMinutes := input.IdleTimeoutMinutes
	if consumerUserID <= 0 || apiKeyID <= 0 || listingID <= 0 || idleTimeoutMinutes <= 0 {
		return nil, service.ErrAccountShareJoinIntentInvalid
	}
	if input.ExpectedVersion <= 0 ||
		input.ExpectedRevisionID <= 0 ||
		input.AcceptedTerms == nil ||
		input.IntentIssuedAt.IsZero() ||
		strings.TrimSpace(input.IntentNonce) == "" {
		return nil, service.ErrAccountShareJoinIntentInvalid
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

	var accountID, ownerUserID int64
	var status string
	var seatLimit int
	var hourlyRate, hourlyFeeWaiverMinimum, minBalanceRequired float64
	var apiKeyName string
	var editSession sql.NullString
	var editingExpiresAt sql.NullTime
	err = tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT a.id, l.owner_user_id, l.status, l.seat_limit, l.hourly_rate, l.hourly_fee_waiver_minimum, l.min_balance_required,
			l.edit_session_id, l.editing_expires_at
		FROM account_share_listings l
		%s
		WHERE l.id = $1
			AND l.deleted_at IS NULL
		FOR UPDATE OF l
	`, accountShareRoomRepresentativeJoinSQL("NOW()")), listingID).Scan(
		&accountID,
		&ownerUserID,
		&status,
		&seatLimit,
		&hourlyRate,
		&hourlyFeeWaiverMinimum,
		&minBalanceRequired,
		&editSession,
		&editingExpiresAt,
	)
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
	revisionID, listingVersion, err := ensureAccountShareListingRevisionInTx(ctx, tx, listingID)
	if err != nil {
		return nil, err
	}
	revision, err := loadAccountShareListingRevisionSnapshotInTx(ctx, tx, listingID, revisionID)
	if err != nil {
		return nil, err
	}
	if input.ExpectedVersion > 0 && listingVersion != input.ExpectedVersion {
		return nil, service.ErrAccountShareJoinTermsChanged.WithMetadata(map[string]string{
			"expected_version": strconv.FormatInt(input.ExpectedVersion, 10),
			"actual_version":   strconv.FormatInt(listingVersion, 10),
		})
	}
	if input.ExpectedRevisionID > 0 && revisionID != input.ExpectedRevisionID {
		return nil, service.ErrAccountShareJoinTermsChanged.WithMetadata(map[string]string{
			"expected_revision_id": strconv.FormatInt(input.ExpectedRevisionID, 10),
			"actual_revision_id":   strconv.FormatInt(revisionID, 10),
		})
	}
	if input.AcceptedTerms != nil && !accountShareMembershipTermsMatchRevision(input.AcceptedTerms, revision) {
		return nil, service.ErrAccountShareJoinTermsChanged
	}
	apiKeyName, err = lockAccountShareJoinAPIKeyInTx(ctx, tx, apiKeyID, consumerUserID)
	if err != nil {
		return nil, err
	}
	termsSnapshot := revision.termsSnapshot()
	termsSnapshotJSON, err := json.Marshal(termsSnapshot)
	if err != nil {
		return nil, err
	}
	seatLimit = revision.SeatLimit
	hourlyRate = revision.HourlyRate
	hourlyFeeWaiverMinimum = revision.HourlyFeeWaiverMinimum
	minBalanceRequired = revision.MinBalanceRequired
	now := time.Now().UTC()
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
	if _, err := endStaleQueuedMembershipsForConsumerInTx(
		ctx,
		tx,
		consumerUserID,
		now,
		r.deferredQueueBindingEnabled(),
	); err != nil {
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
			AND m.listing_id = $2
			AND m.status IN ($3, $4, $5)
			AND m.deleted_at IS NULL
		ORDER BY CASE WHEN m.status = $5 THEN 0 ELSE 1 END, m.id ASC
		LIMIT 1
	`,
		consumerUserID,
		listingID,
		service.AccountShareMembershipStatusActive,
		service.AccountShareMembershipStatusQueued,
		service.AccountShareMembershipStatusEnding,
	))
	if err == nil {
		if existing.Status == service.AccountShareMembershipStatusEnding {
			return nil, service.ErrAccountShareMembershipEnding.WithMetadata(map[string]string{
				"membership_id": strconv.FormatInt(existing.ID, 10),
				"listing_id":    strconv.FormatInt(listingID, 10),
			})
		}
		if existing.APIKeyID != apiKeyID {
			return nil, service.ErrAccountShareAlreadyUsing.WithMetadata(map[string]string{
				"membership_id": strconv.FormatInt(existing.ID, 10),
				"listing_id":    strconv.FormatInt(listingID, 10),
			})
		}
		if err := loadAccountShareMembershipTraceSnapshotInTx(ctx, tx, existing); err != nil {
			return nil, err
		}
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if !input.IntentIssuedAt.IsZero() {
		var consumed bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM account_share_memberships
				WHERE consumer_user_id = $1
					AND api_key_id = $2
					AND listing_id = $3
					AND created_at >= $4
					AND status IN ($5, $6)
			)
		`,
			consumerUserID,
			apiKeyID,
			listingID,
			input.IntentIssuedAt,
			service.AccountShareMembershipStatusEnding,
			service.AccountShareMembershipStatusEnded,
		).Scan(&consumed); err != nil {
			return nil, err
		}
		if consumed {
			return nil, service.ErrAccountShareJoinIntentConsumed
		}
	}

	var apiKeyQueueCount, maxQueueRank, consumerQueueCount, roomQueueCount int
	var hasLiveMembership bool
	if err := tx.QueryRowContext(ctx, `
		SELECT
			(
				SELECT COUNT(*)::int
				FROM account_share_memberships
				WHERE consumer_user_id = $1
					AND api_key_id = $2
					AND status = $3
					AND deleted_at IS NULL
					AND (queue_expires_at IS NULL OR queue_expires_at > $6)
			),
			COALESCE((
				SELECT MAX(queue_rank)
				FROM account_share_memberships
				WHERE api_key_id = $2
					AND status IN ($3, $4)
					AND deleted_at IS NULL
			), 0)::int,
			EXISTS (
				SELECT 1
				FROM account_share_memberships
				WHERE consumer_user_id = $1
					AND api_key_id = $2
					AND status IN ($4, $5)
					AND deleted_at IS NULL
			),
			(
				SELECT COUNT(*)::int
				FROM account_share_memberships
				WHERE consumer_user_id = $1
					AND status = $3
					AND deleted_at IS NULL
					AND (queue_expires_at IS NULL OR queue_expires_at > $6)
			),
			(
				SELECT COUNT(*)::int
				FROM account_share_memberships
				WHERE listing_id = $7
					AND status = $3
					AND deleted_at IS NULL
					AND (queue_expires_at IS NULL OR queue_expires_at > $6)
			)
	`,
		consumerUserID,
		apiKeyID,
		service.AccountShareMembershipStatusQueued,
		service.AccountShareMembershipStatusActive,
		service.AccountShareMembershipStatusEnding,
		now,
		listingID,
	).Scan(
		&apiKeyQueueCount,
		&maxQueueRank,
		&hasLiveMembership,
		&consumerQueueCount,
		&roomQueueCount,
	); err != nil {
		return nil, err
	}
	queueRank := maxQueueRank + 1
	activateNow := false
	if !hasLiveMembership && apiKeyQueueCount == 0 {
		if ownerSelfUse {
			activateNow = true
		} else {
			activeSeats, err := liveAccountShareSeatCountInTx(ctx, tx, listingID)
			if err != nil {
				return nil, err
			}
			activateNow = activeSeats < seatLimit
		}
	}
	if activateNow {
		unavailable, err := r.accountShareAccountUnavailableInTx(ctx, tx, accountID, now)
		if err != nil {
			return nil, err
		}
		if unavailable {
			return nil, service.ErrAccountShareAccountUnavailable
		}
	}
	if !activateNow {
		if err := accountShareJoinQueueCapacityError(
			apiKeyQueueCount,
			consumerQueueCount,
			roomQueueCount,
			seatLimit,
		); err != nil {
			return nil, err
		}
	}
	if !activateNow && !input.AcceptQueue {
		return nil, service.ErrAccountShareQueueConfirmationRequired
	}
	if activateNow && !ownerSelfUse && prepayAmount > 0 && userBalance < minBalanceRequired+prepayAmount {
		return nil, service.ErrAccountShareModePrepayInsufficient
	}

	membership := &service.AccountShareMembership{}
	var endedAt, lastRequestAt sql.NullTime
	var paidUntilScan, billedUntilScan, dispatchFailedAt, dispatchCooldownUntil sql.NullTime
	var waiverWindowStartedAt, waiverWindowLastRequestAt sql.NullTime
	var endedReason sql.NullString
	var membershipAccountID sql.NullInt64
	var paidUntilValue any
	var billedUntilValue any
	var waiverWindowStartedAtValue any
	var membershipAccountIDValue any
	membershipStatus := service.AccountShareMembershipStatusQueued
	if activateNow {
		membershipStatus = service.AccountShareMembershipStatusActive
	}
	if activateNow || !r.deferredQueueBindingEnabled() {
		membershipAccountIDValue = accountID
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
			waiver_window_request_count, waiver_window_last_request_at, dispatch_failed_at, dispatch_cooldown_until,
			queue_expires_at,
			listing_revision_id, listing_version_snapshot, room_name_snapshot, owner_user_id_snapshot,
			owner_username_snapshot, platform_snapshot, account_level_snapshot, api_key_name_snapshot,
			terms_snapshot, snapshot_quality, created_at, updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5::varchar(20), $6, $7, $8, $9, $10, NULL, NULL, $11, $12, $13, 0, 0, NULL, NULL, NULL,
			CASE WHEN $5::varchar(20) = 'queued'::varchar(20) THEN NOW() + make_interval(hours => $24) ELSE NULL END,
			$14, $15, $16, $17, $18, $19, $20, $21, $22::jsonb, $23, NOW(), NOW()
		)
		RETURNING id, listing_id, account_id, consumer_user_id, api_key_id, status, queue_rank,
			hourly_rate_snapshot, hourly_fee_waiver_minimum_snapshot, idle_timeout_minutes, joined_at, last_request_at, ended_at,
			ended_reason, paid_until, billed_until, waiver_window_started_at, waiver_window_usage_amount,
			waiver_window_request_count, waiver_window_last_request_at, dispatch_failed_at, dispatch_cooldown_until, created_at, updated_at
	`,
		listingID,
		membershipAccountIDValue,
		consumerUserID,
		apiKeyID,
		membershipStatus,
		queueRank,
		hourlyRate,
		hourlyFeeWaiverMinimum,
		idleTimeoutMinutes,
		now,
		paidUntilValue,
		billedUntilValue,
		waiverWindowStartedAtValue,
		revisionID,
		listingVersion,
		revision.RoomName,
		ownerUserID,
		revision.OwnerDisplayName,
		revision.Platform,
		revision.AccountLevel,
		strings.TrimSpace(apiKeyName),
		string(termsSnapshotJSON),
		service.AccountShareSnapshotQualityExact,
		service.AccountShareModeQueueExpiryDuration.Hours(),
	).Scan(
		&membership.ID,
		&membership.ListingID,
		&membershipAccountID,
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
	if membershipAccountID.Valid {
		membership.AccountID = membershipAccountID.Int64
	} else if membership.Status != service.AccountShareMembershipStatusQueued {
		return nil, fmt.Errorf("account share membership %d in status %q has no account binding", membership.ID, membership.Status)
	}
	membership.OwnerUserID = ownerUserID
	membership.ListingRevisionID = &revisionID
	membership.ListingVersionSnapshot = &listingVersion
	membership.RoomNameSnapshot = revision.RoomName
	membership.OwnerUserIDSnapshot = &ownerUserID
	membership.OwnerUsernameSnapshot = revision.OwnerDisplayName
	membership.PlatformSnapshot = revision.Platform
	membership.AccountLevelSnapshot = revision.AccountLevel
	membership.APIKeyNameSnapshot = strings.TrimSpace(apiKeyName)
	membership.TermsSnapshot = termsSnapshot
	membership.SnapshotQuality = service.AccountShareSnapshotQualityExact
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
	if activateNow {
		boundByRole := "consumer"
		if ownerSelfUse {
			boundByRole = "owner"
		}
		if _, _, err := r.createAccountShareMembershipBindingInTx(
			ctx,
			tx,
			membership.ID,
			listingID,
			accountID,
			revisionID,
			consumerUserID,
			boundByRole,
			"join_activation",
			now,
		); err != nil {
			return nil, err
		}
	}
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

func (r *accountShareModeRepository) GetMembershipForEnd(
	ctx context.Context,
	consumerUserID int64,
	membershipID int64,
) (*service.AccountShareMembership, error) {
	if r == nil || r.db == nil || consumerUserID <= 0 || membershipID <= 0 {
		return nil, service.ErrAccountShareMembershipNotFound
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

	if _, _, err := lockAccountShareEndListingInTx(ctx, tx, membershipID, consumerUserID); err != nil {
		return nil, err
	}
	membership, err := lockAccountShareEndMembershipInTx(ctx, tx, membershipID, consumerUserID)
	if err != nil {
		return nil, err
	}
	if err := loadAccountShareMembershipEndStateInTx(ctx, tx, membership); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return membership, nil
}

func (r *accountShareModeRepository) BeginMembershipEnd(
	ctx context.Context,
	input service.BeginAccountShareMembershipEndInput,
) (*service.AccountShareMembership, *service.AccountShareSeatBillingResult, error) {
	// 单阶段结束：按成员当前状态收口，不再要求调用方携带状态快照。
	operationID := strings.TrimSpace(input.OperationID)
	if r == nil || r.db == nil ||
		input.ConsumerUserID <= 0 ||
		input.MembershipID <= 0 ||
		operationID == "" {
		return nil, nil, service.ErrAccountShareEndStateConflict
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	listingID, listingVersion, err := lockAccountShareEndListingInTx(ctx, tx, input.MembershipID, input.ConsumerUserID)
	if err != nil {
		return nil, nil, err
	}
	membership, err := lockAccountShareEndMembershipInTx(ctx, tx, input.MembershipID, input.ConsumerUserID)
	if err != nil {
		return nil, nil, err
	}
	if err := loadAccountShareMembershipEndStateInTx(ctx, tx, membership); err != nil {
		return nil, nil, err
	}

	if membership.Status == service.AccountShareMembershipStatusEnding ||
		membership.Status == service.AccountShareMembershipStatusEnded {
		// Another confirmed request may already have moved this membership
		// forward with a different operation ID. Ownership is locked and
		// verified above, so return the durable current state instead of
		// turning a successful concurrent end into a business error.
		if err := tx.Commit(); err != nil {
			return nil, nil, err
		}
		tx = nil
		return membership, nil, nil
	}
	if membership.Status != service.AccountShareMembershipStatusActive &&
		membership.Status != service.AccountShareMembershipStatusQueued {
		return nil, nil, service.ErrAccountShareEndStateConflict
	}

	now := time.Now().UTC()
	if membership.Status == service.AccountShareMembershipStatusQueued {
		// 排队成员未入座、无费用，直接终结。降级重排队残留的
		// billed_until/paid_until/绑定形态不构成阻塞（资金在降级时已结清），
		// 兜底关闭可能残留的 open binding 即可。
		if _, err := r.closeAccountShareMembershipBindingInTx(
			ctx, tx, membership.ID, input.ConsumerUserID, "consumer", "membership_ended", now,
		); err != nil {
			return nil, nil, err
		}
		resultPayload, err := json.Marshal(map[string]any{
			"membership_id":     membership.ID,
			"status":            service.AccountShareMembershipStatusEnded,
			"settlement_status": "not_required",
		})
		if err != nil {
			return nil, nil, err
		}
		if err := insertAccountShareEndOperationInTx(
			ctx,
			tx,
			operationID,
			listingID,
			membership.ID,
			input.ConsumerUserID,
			listingVersion,
			"succeeded",
			resultPayload,
			now,
		); err != nil {
			return nil, nil, err
		}
		membership, err = scanAccountShareMembership(tx.QueryRowContext(ctx, `
			UPDATE account_share_memberships m
			SET status = $1,
				account_id = CASE WHEN $7::boolean THEN NULL ELSE m.account_id END,
				ended_at = $2,
				ended_reason = $3::text,
				paid_until = NULL,
				billed_until = NULL,
				queue_expires_at = NULL,
				ending_requested_at = $2,
				ending_reason = $8::text,
				ending_operation_id = $4::uuid,
				settlement_status = 'not_required',
				waiver_window_started_at = NULL,
				waiver_window_usage_amount = 0,
				waiver_window_request_count = 0,
				waiver_window_last_request_at = NULL,
				dispatch_failed_at = NULL,
				dispatch_cooldown_until = NULL,
				updated_at = NOW()
			FROM account_share_listings l
			WHERE m.id = $5
				AND m.status = $6
				AND m.deleted_at IS NULL
				AND l.id = m.listing_id
			RETURNING
				m.id, m.listing_id, m.account_id, l.owner_user_id, m.consumer_user_id, m.api_key_id,
				m.status, m.queue_rank, m.hourly_rate_snapshot, m.hourly_fee_waiver_minimum_snapshot, m.idle_timeout_minutes,
				m.joined_at, m.last_request_at, m.ended_at, m.ended_reason, m.paid_until, m.billed_until,
				m.waiver_window_started_at, m.waiver_window_usage_amount, m.waiver_window_request_count, m.waiver_window_last_request_at,
				m.dispatch_failed_at, m.dispatch_cooldown_until, m.created_at, m.updated_at
		`,
			service.AccountShareMembershipStatusEnded,
			now,
			service.AccountShareMembershipEndReasonManual,
			operationID,
			membership.ID,
			service.AccountShareMembershipStatusQueued,
			r.deferredQueueBindingEnabled(),
			service.AccountShareMembershipEndReasonManual,
		))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, service.ErrAccountShareEndStateConflict
		}
		if err != nil {
			return nil, nil, err
		}
		membership.EndingRequestedAt = &now
		membership.EndingReason = service.AccountShareMembershipEndReasonManual
		membership.EndingOperationID = operationID
		membership.SettlementStatus = "not_required"
		if err := tx.Commit(); err != nil {
			return nil, nil, err
		}
		tx = nil
		return membership, &service.AccountShareSeatBillingResult{
			Processed:            1,
			EndedConsumerUserIDs: []int64{membership.ConsumerUserID},
		}, nil
	}

	if membership.AccountID <= 0 {
		return nil, nil, service.ErrAccountShareEndStateConflict
	}
	if err := insertAccountShareEndOperationInTx(
		ctx,
		tx,
		operationID,
		listingID,
		membership.ID,
		input.ConsumerUserID,
		listingVersion,
		"pending",
		nil,
		now,
	); err != nil {
		return nil, nil, err
	}
	membership, err = scanAccountShareMembership(tx.QueryRowContext(ctx, `
		UPDATE account_share_memberships m
		SET status = $1,
			ending_requested_at = $2,
			ending_reason = $3,
			ending_operation_id = $4::uuid,
			settlement_status = 'pending',
			updated_at = NOW()
		FROM account_share_listings l
		WHERE m.id = $5
			AND m.status = $6
			AND m.deleted_at IS NULL
			AND l.id = m.listing_id
		RETURNING
			m.id, m.listing_id, m.account_id, l.owner_user_id, m.consumer_user_id, m.api_key_id,
			m.status, m.queue_rank, m.hourly_rate_snapshot, m.hourly_fee_waiver_minimum_snapshot, m.idle_timeout_minutes,
			m.joined_at, m.last_request_at, m.ended_at, m.ended_reason, m.paid_until, m.billed_until,
			m.waiver_window_started_at, m.waiver_window_usage_amount, m.waiver_window_request_count, m.waiver_window_last_request_at,
			m.dispatch_failed_at, m.dispatch_cooldown_until, m.created_at, m.updated_at
	`,
		service.AccountShareMembershipStatusEnding,
		now,
		service.AccountShareMembershipEndReasonManual,
		operationID,
		membership.ID,
		service.AccountShareMembershipStatusActive,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, service.ErrAccountShareEndStateConflict
	}
	if err != nil {
		return nil, nil, err
	}
	membership.EndingRequestedAt = &now
	membership.EndingReason = service.AccountShareMembershipEndReasonManual
	membership.EndingOperationID = operationID
	membership.SettlementStatus = "pending"
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	tx = nil
	return membership, nil, nil
}

func (r *accountShareModeRepository) FinalizeMembershipEnd(
	ctx context.Context,
	membershipID int64,
	operationID string,
) (*service.AccountShareMembership, *service.AccountShareSeatBillingResult, bool, error) {
	operationID = strings.TrimSpace(operationID)
	if r == nil || r.db == nil || membershipID <= 0 || operationID == "" {
		return nil, nil, false, service.ErrAccountShareEndStateConflict
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, false, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	_, listingVersion, err := lockAccountShareEndListingInTx(ctx, tx, membershipID, 0)
	if err != nil {
		return nil, nil, false, err
	}
	membership, err := lockAccountShareEndMembershipInTx(ctx, tx, membershipID, 0)
	if err != nil {
		return nil, nil, false, err
	}
	if err := loadAccountShareMembershipEndStateInTx(ctx, tx, membership); err != nil {
		return nil, nil, false, err
	}
	if membership.EndingOperationID != operationID {
		return nil, nil, false, service.ErrAccountShareEndStateConflict
	}
	if membership.Status == service.AccountShareMembershipStatusEnded {
		if err := tx.Commit(); err != nil {
			return nil, nil, false, err
		}
		tx = nil
		return membership, nil, true, nil
	}
	if membership.Status != service.AccountShareMembershipStatusEnding ||
		membership.EndingRequestedAt == nil ||
		membership.SettlementStatus == "" {
		return nil, nil, false, service.ErrAccountShareEndStateConflict
	}
	if err := lockAccountShareEndOperationInTx(ctx, tx, operationID, membership.ID); err != nil {
		return nil, nil, false, err
	}

	openBindings, pendingIntents, err := lockAccountShareEndRuntimeRowsInTx(ctx, tx, membership.ID)
	if err != nil {
		return nil, nil, false, err
	}
	if openBindings > 1 {
		return nil, nil, false, fmt.Errorf("membership %d has %d open account-share bindings", membership.ID, openBindings)
	}
	if pendingIntents > 0 {
		blockerJSON, marshalErr := json.Marshal(map[string]any{
			"code":                 "pending_billing_intents",
			"pending_intent_count": pendingIntents,
		})
		if marshalErr != nil {
			return nil, nil, false, marshalErr
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE account_share_room_operations
			SET blocker = $1::jsonb,
				state_token = state_token + 1,
				updated_at = NOW()
			WHERE id = $2::uuid
				AND action = 'end_membership'
				AND membership_id = $3
				AND status IN ('pending', 'running', 'needs_attention')
				AND blocker IS DISTINCT FROM $1::jsonb
		`, string(blockerJSON), operationID, membership.ID); err != nil {
			return nil, nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return nil, nil, false, err
		}
		tx = nil
		return membership, nil, false, nil
	}

	if err := lockAccountShareEndBillingUsersInTx(ctx, tx, membership); err != nil {
		return nil, nil, false, err
	}
	endedAt := membership.EndingRequestedAt.UTC()
	settledUntil, _, creditUserIDs, err := r.settleSeatChargeInTx(ctx, tx, membership, endedAt, true, endedAt)
	if err != nil {
		return nil, nil, false, err
	}
	if err := r.refundUnusedSeatPrepayInTx(ctx, tx, membership, endedAt); err != nil {
		return nil, nil, false, err
	}
	if settledUntil == nil {
		settledUntil = &endedAt
	}
	if _, err := r.closeAccountShareMembershipBindingInTx(
		ctx,
		tx,
		membership.ID,
		membership.ConsumerUserID,
		"consumer",
		"membership_ended",
		endedAt,
	); err != nil {
		return nil, nil, false, err
	}
	membership, err = scanAccountShareMembership(tx.QueryRowContext(ctx, `
		UPDATE account_share_memberships m
		SET status = $1,
			ended_at = $2,
			ended_reason = $3,
			paid_until = $4,
			billed_until = $4,
			queue_expires_at = NULL,
			settlement_status = 'settled',
			waiver_window_started_at = $4,
			waiver_window_usage_amount = 0,
			waiver_window_request_count = 0,
			waiver_window_last_request_at = NULL,
			dispatch_cooldown_until = NULL,
			updated_at = NOW()
		FROM account_share_listings l
		WHERE m.id = $5
			AND m.status = $6
			AND m.ending_operation_id = $7::uuid
			AND m.deleted_at IS NULL
			AND l.id = m.listing_id
		RETURNING
			m.id, m.listing_id, m.account_id, l.owner_user_id, m.consumer_user_id, m.api_key_id,
			m.status, m.queue_rank, m.hourly_rate_snapshot, m.hourly_fee_waiver_minimum_snapshot, m.idle_timeout_minutes,
			m.joined_at, m.last_request_at, m.ended_at, m.ended_reason, m.paid_until, m.billed_until,
			m.waiver_window_started_at, m.waiver_window_usage_amount, m.waiver_window_request_count, m.waiver_window_last_request_at,
			m.dispatch_failed_at, m.dispatch_cooldown_until, m.created_at, m.updated_at
	`,
		service.AccountShareMembershipStatusEnded,
		endedAt,
		service.AccountShareMembershipEndReasonManual,
		*settledUntil,
		membership.ID,
		service.AccountShareMembershipStatusEnding,
		operationID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, false, service.ErrAccountShareEndStateConflict
	}
	if err != nil {
		return nil, nil, false, err
	}
	membership.EndingRequestedAt = &endedAt
	membership.EndingReason = service.AccountShareMembershipEndReasonManual
	membership.EndingOperationID = operationID
	membership.SettlementStatus = "settled"
	resultPayload, err := json.Marshal(map[string]any{
		"membership_id":     membership.ID,
		"status":            membership.Status,
		"settlement_status": membership.SettlementStatus,
		"ended_at":          endedAt.Format(time.RFC3339Nano),
	})
	if err != nil {
		return nil, nil, false, err
	}
	if err := completeAccountShareRoomOperationInTx(ctx, tx, operationID, listingVersion, resultPayload); err != nil {
		return nil, nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, false, err
	}
	tx = nil
	billing := accountShareMembershipBillingResult(membership, creditUserIDs)
	billing.Processed = 1
	return membership, billing, true, nil
}

func (r *accountShareModeRepository) ListEndingMembershipCandidates(
	ctx context.Context,
	limit int,
) ([]service.AccountShareEndingMembershipCandidate, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrServiceUnavailable
	}
	if limit <= 0 {
		limit = service.AccountShareModeSeatBillingBatchSize
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT m.id, m.ending_operation_id::text, m.ending_requested_at, m.last_request_at
		FROM account_share_memberships m
		JOIN account_share_room_operations operation
			ON operation.id = m.ending_operation_id
			AND operation.action = 'end_membership'
			AND operation.membership_id = m.id
			AND operation.status IN ('pending', 'running', 'needs_attention')
		WHERE m.status = $1
			AND m.ending_operation_id IS NOT NULL
			AND m.deleted_at IS NULL
		ORDER BY m.ending_requested_at ASC, m.id ASC
		LIMIT $2
	`, service.AccountShareMembershipStatusEnding, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	candidates := make([]service.AccountShareEndingMembershipCandidate, 0, limit)
	for rows.Next() {
		var candidate service.AccountShareEndingMembershipCandidate
		var endingRequestedAt time.Time
		var lastRequestAt sql.NullTime
		if err := rows.Scan(&candidate.MembershipID, &candidate.OperationID, &endingRequestedAt, &lastRequestAt); err != nil {
			return nil, err
		}
		candidate.EndingRequestedAt = endingRequestedAt.UTC()
		if lastRequestAt.Valid {
			candidate.LastRequestAt = lastRequestAt.Time.UTC()
		}
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}

func lockAccountShareEndListingInTx(
	ctx context.Context,
	tx *sql.Tx,
	membershipID int64,
	consumerUserID int64,
) (int64, int64, error) {
	if tx == nil || membershipID <= 0 {
		return 0, 0, service.ErrAccountShareMembershipNotFound
	}
	query := `
		SELECT listing_id
		FROM account_share_memberships
		WHERE id = $1
			AND deleted_at IS NULL
	`
	args := []any{membershipID}
	if consumerUserID > 0 {
		query += " AND consumer_user_id = $2"
		args = append(args, consumerUserID)
	}
	var listingID int64
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&listingID); errors.Is(err, sql.ErrNoRows) {
		return 0, 0, service.ErrAccountShareMembershipNotFound
	} else if err != nil {
		return 0, 0, err
	}
	var rowVersion int64
	if err := tx.QueryRowContext(ctx, `
		SELECT row_version
		FROM account_share_listings
		WHERE id = $1
		FOR UPDATE
	`, listingID).Scan(&rowVersion); errors.Is(err, sql.ErrNoRows) {
		return 0, 0, service.ErrAccountShareMembershipNotFound
	} else if err != nil {
		return 0, 0, err
	}
	return listingID, rowVersion, nil
}

func lockAccountShareEndMembershipInTx(
	ctx context.Context,
	tx *sql.Tx,
	membershipID int64,
	consumerUserID int64,
) (*service.AccountShareMembership, error) {
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
			AND m.deleted_at IS NULL
		FOR UPDATE OF m
	`
	args := []any{membershipID}
	if consumerUserID > 0 {
		query = strings.Replace(query, "AND m.deleted_at IS NULL", "AND m.consumer_user_id = $2\n\t\t\tAND m.deleted_at IS NULL", 1)
		args = append(args, consumerUserID)
	}
	membership, err := scanAccountShareMembership(tx.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareMembershipNotFound
	}
	if err != nil {
		return nil, err
	}
	return membership, nil
}

func loadAccountShareMembershipEndStateInTx(
	ctx context.Context,
	tx *sql.Tx,
	membership *service.AccountShareMembership,
) error {
	if tx == nil || membership == nil || membership.ID <= 0 {
		return service.ErrAccountShareMembershipNotFound
	}
	var endingRequestedAt sql.NullTime
	var endingReason, settlementStatus, endingOperationID sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT
			ending_requested_at,
			ending_reason,
			settlement_status,
			ending_operation_id::text
		FROM account_share_memberships
		WHERE id = $1
			AND deleted_at IS NULL
	`, membership.ID).Scan(
		&endingRequestedAt,
		&endingReason,
		&settlementStatus,
		&endingOperationID,
	); err != nil {
		return err
	}
	membership.EndingRequestedAt = sqlNullTimePtr(endingRequestedAt)
	membership.EndingReason = strings.TrimSpace(endingReason.String)
	membership.SettlementStatus = strings.TrimSpace(settlementStatus.String)
	membership.EndingOperationID = strings.TrimSpace(endingOperationID.String)
	return nil
}

func insertAccountShareEndOperationInTx(
	ctx context.Context,
	tx *sql.Tx,
	operationID string,
	listingID int64,
	membershipID int64,
	consumerUserID int64,
	listingVersion int64,
	status string,
	resultPayload []byte,
	now time.Time,
) error {
	if len(resultPayload) == 0 {
		resultPayload = []byte(`{}`)
	}
	var completedAt any
	if status == "succeeded" {
		completedAt = now.UTC()
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO account_share_room_operations (
			id, listing_id, membership_id, action,
			actor_user_id, actor_role, source, request_id,
			expected_version, start_version, final_version,
			status, blocker, result, completed_at, created_at, updated_at
		)
		VALUES (
			$1::uuid, $2, $3, 'end_membership',
			$4, 'consumer', 'consumer_request', $1,
			$5::bigint, $5::bigint,
			CASE
				WHEN $6::varchar(20) = 'succeeded'::varchar(20) THEN $5::bigint
				ELSE NULL::bigint
			END,
			$6::varchar(20), '{}'::jsonb, $7::jsonb, $8::timestamptz, $9::timestamptz, $9::timestamptz
		)
	`, operationID, listingID, membershipID, consumerUserID, listingVersion, status, string(resultPayload), completedAt, now.UTC())
	if err != nil {
		return translateAccountShareLifecyclePersistenceError(err)
	}
	return nil
}

func lockAccountShareEndOperationInTx(
	ctx context.Context,
	tx *sql.Tx,
	operationID string,
	membershipID int64,
) error {
	var status string
	err := tx.QueryRowContext(ctx, `
		SELECT status
		FROM account_share_room_operations
		WHERE id = $1::uuid
			AND action = 'end_membership'
			AND membership_id = $2
		FOR UPDATE
	`, operationID, membershipID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrAccountShareEndStateConflict
	}
	if err != nil {
		return err
	}
	switch status {
	case "pending", "running", "needs_attention":
		return nil
	default:
		return service.ErrAccountShareEndStateConflict
	}
}

func lockAccountShareEndRuntimeRowsInTx(
	ctx context.Context,
	tx *sql.Tx,
	membershipID int64,
) (int, int, error) {
	bindingRows, err := tx.QueryContext(ctx, `
		SELECT id
		FROM account_share_membership_account_bindings
		WHERE membership_id = $1
			AND unbound_at IS NULL
		ORDER BY id ASC
		FOR UPDATE
	`, membershipID)
	if err != nil {
		return 0, 0, err
	}
	openBindings := 0
	for bindingRows.Next() {
		var id int64
		if err := bindingRows.Scan(&id); err != nil {
			_ = bindingRows.Close()
			return 0, 0, err
		}
		openBindings++
	}
	if err := bindingRows.Err(); err != nil {
		_ = bindingRows.Close()
		return 0, 0, err
	}
	if err := bindingRows.Close(); err != nil {
		return 0, 0, err
	}

	// billing intent 体系已删除：同步结算不存在"未结算 intent"，不再阻塞结束流程
	return openBindings, 0, nil
}

func lockAccountShareEndBillingUsersInTx(
	ctx context.Context,
	tx *sql.Tx,
	membership *service.AccountShareMembership,
) error {
	if tx == nil || membership == nil || membership.ConsumerUserID <= 0 || membership.OwnerUserID <= 0 {
		return service.ErrUserNotFound
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id
		FROM users
		WHERE deleted_at IS NULL
			AND (
				id = ANY($1::bigint[])
				OR id = (
					SELECT affiliate.inviter_id
					FROM user_affiliates affiliate
					WHERE affiliate.user_id = $2
						AND affiliate.inviter_id IS NOT NULL
						AND affiliate.inviter_id <> affiliate.user_id
					LIMIT 1
				)
			)
		ORDER BY id ASC
		FOR UPDATE
	`, pq.Array([]int64{membership.ConsumerUserID, membership.OwnerUserID}), membership.ConsumerUserID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	locked := make(map[int64]struct{}, 3)
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return err
		}
		locked[userID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, ok := locked[membership.ConsumerUserID]; !ok {
		return service.ErrUserNotFound
	}
	if _, ok := locked[membership.OwnerUserID]; !ok {
		return service.ErrUserNotFound
	}
	return nil
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
		FOR UPDATE OF l
	`, membershipID, consumerUserID).Scan(&lockedListingID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, err
	}

	var listingID, ownerUserID int64
	var currentAccountID, legacyAccountIdentityID sql.NullInt64
	var lastRequestAt, listingDeletedAt sql.NullTime
	var membershipStatus string
	err = tx.QueryRowContext(ctx, `
		SELECT
			m.listing_id,
			COALESCE(history_binding.account_id, m.account_id),
			l.account_identity_id,
			l.deleted_at,
			COALESCE(m.owner_user_id_snapshot, revision.owner_user_id, l.owner_user_id, 0),
			m.last_request_at,
			m.status
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
		LEFT JOIN account_share_listing_revisions revision
			ON revision.id = m.listing_revision_id
			AND revision.listing_id = m.listing_id
		LEFT JOIN LATERAL (
			SELECT
				binding.account_id
			FROM account_share_membership_account_bindings binding
			WHERE binding.membership_id = m.id
				AND binding.listing_id = m.listing_id
			ORDER BY binding.routing_generation DESC, binding.id DESC
			LIMIT 1
		) history_binding ON TRUE
		WHERE m.id = $1
			AND m.consumer_user_id = $2
			AND m.deleted_at IS NULL
		FOR UPDATE OF m
	`, membershipID, consumerUserID).Scan(
		&listingID,
		&currentAccountID,
		&legacyAccountIdentityID,
		&listingDeletedAt,
		&ownerUserID,
		&lastRequestAt,
		&membershipStatus,
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
	if ownerUserID <= 0 {
		return nil, service.ErrUserNotFound
	}

	var reviewAccountIdentityID any
	if !r.reviewRoomSubjectWritesEnabled() {
		identityID := legacyAccountIdentityID.Int64
		if identityID <= 0 {
			if listingDeletedAt.Valid {
				return nil, service.ErrAccountShareReviewIdentityMissing
			}
			if !currentAccountID.Valid || currentAccountID.Int64 <= 0 {
				return nil, service.ErrAccountShareReviewIdentityMissing
			}
			var currentAccountName, currentAccountPlatform string
			var credentialsRaw, extraRaw []byte
			err := tx.QueryRowContext(ctx, `
				SELECT
					COALESCE(name, ''),
					COALESCE(platform, ''),
					credentials,
					extra
				FROM accounts
				WHERE id = $1
			`, currentAccountID.Int64).Scan(
				&currentAccountName,
				&currentAccountPlatform,
				&credentialsRaw,
				&extraRaw,
			)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, service.ErrAccountShareReviewIdentityMissing
			}
			if err != nil {
				return nil, err
			}
			credentials, err := unmarshalAccountShareJSONMap(credentialsRaw)
			if err != nil {
				return nil, err
			}
			extra, err := unmarshalAccountShareJSONMap(extraRaw)
			if err != nil {
				return nil, err
			}
			account := &service.Account{
				ID:          currentAccountID.Int64,
				Name:        currentAccountName,
				Platform:    currentAccountPlatform,
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
		reviewAccountIdentityID = identityID
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
	`,
		reviewAccountIdentityID,
		listingID,
		nullableInt64(sqlNullInt64Ptr(currentAccountID)),
		membershipID,
		ownerUserID,
		consumerUserID,
		input.Score,
		comment,
		commentStatus,
		moderationRequestedAt,
		moderationNextRetryAt,
	).Scan(&reviewID)
	if err != nil {
		if isAccountShareReviewUniqueViolation(err) {
			return nil, service.ErrAccountShareReviewAlreadyExists.WithCause(err)
		}
		return nil, err
	}
	if err := refreshAccountShareListingRatingsInTx(ctx, tx, listingID); err != nil {
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

func (r *accountShareModeRepository) ListListingReviews(
	ctx context.Context,
	viewerUserID int64,
	viewerIsAdmin bool,
	listingID int64,
	params pagination.PaginationParams,
) ([]service.AccountShareReview, *pagination.PaginationResult, error) {
	page := params.Page
	if page < 1 {
		page = 1
	}
	limit := params.Limit()
	offset := (page - 1) * limit

	var resolvedListingID int64
	var total int64
	if err := r.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT
			l.id,
			COUNT(r.id)
		FROM account_share_listings l
		LEFT JOIN account_share_reviews r
			ON r.listing_id = l.id
			AND r.comment_status = $2
			AND r.comment <> ''
			AND r.deleted_at IS NULL
		WHERE l.id = $1
			AND (
				(l.deleted_at IS NULL AND l.status = 'active')
				OR $3::boolean
				OR l.owner_user_id = $4
				OR %s
			)
		GROUP BY l.id
	`, accountShareReviewBoundViewerMembershipExistsSQL("l.id", "$4")),
		listingID,
		service.AccountShareReviewCommentStatusApproved,
		viewerIsAdmin,
		viewerUserID,
	).Scan(&resolvedListingID, &total); errors.Is(err, sql.ErrNoRows) {
		return nil, nil, service.ErrAccountShareListingNotFound
	} else if err != nil {
		return nil, nil, err
	}
	if total == 0 {
		return []service.AccountShareReview{}, accountShareReviewPagination(total, page, limit), nil
	}
	rows, err := r.db.QueryContext(ctx, accountShareReviewSelectSQL()+`
		WHERE r.listing_id = $1
			AND r.comment_status = $2
			AND r.comment <> ''
			AND r.deleted_at IS NULL
		ORDER BY r.created_at DESC, r.id DESC
		LIMIT $3 OFFSET $4
	`, resolvedListingID, service.AccountShareReviewCommentStatusApproved, limit, offset)
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

func (r *accountShareModeRepository) CanViewListingReviewDetails(
	ctx context.Context,
	viewerUserID int64,
	viewerIsAdmin bool,
	listingID int64,
) (bool, error) {
	if r == nil || r.db == nil {
		return false, service.ErrServiceUnavailable
	}
	if listingID <= 0 || (!viewerIsAdmin && viewerUserID <= 0) {
		return false, service.ErrAccountShareListingNotFound
	}
	var allowed bool
	err := r.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT EXISTS (
			SELECT 1
			FROM account_share_listings listing
			WHERE listing.id = $1
				AND (
					$2::boolean
					OR listing.owner_user_id = $3
					OR %s
				)
		)
	`, accountShareReviewBoundViewerMembershipExistsSQL("listing.id", "$3")),
		listingID,
		viewerIsAdmin,
		viewerUserID,
	).Scan(&allowed)
	if err != nil {
		return false, err
	}
	return allowed, nil
}

func accountShareReviewBoundViewerMembershipExistsSQL(listingIDExpr, viewerUserIDExpr string) string {
	return fmt.Sprintf(`EXISTS (
		SELECT 1
		FROM account_share_memberships viewer_membership
		WHERE viewer_membership.listing_id = %s
			AND viewer_membership.consumer_user_id = %s
			AND viewer_membership.deleted_at IS NULL
			AND EXISTS (
				SELECT 1
				FROM account_share_membership_account_bindings viewer_binding
				WHERE viewer_binding.membership_id = viewer_membership.id
					AND viewer_binding.listing_id = viewer_membership.listing_id
			)
	)`, listingIDExpr, viewerUserIDExpr)
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

func (r *accountShareModeRepository) BeginReviewModerationAttempt(
	ctx context.Context,
	reviewID int64,
	maxAttempts int,
) (bool, error) {
	if reviewID <= 0 {
		return false, nil
	}
	if maxAttempts <= 0 {
		maxAttempts = service.AccountShareReviewModerationMaxAttempts
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE account_share_reviews
		SET moderation_attempts = moderation_attempts + 1,
			moderation_requested_at = NOW(),
			updated_at = NOW()
		WHERE id = $1
			AND deleted_at IS NULL
			AND comment <> ''
			AND comment_status IN ($2, $3)
			AND moderation_attempts < $4
	`, reviewID, service.AccountShareReviewCommentStatusPending, service.AccountShareReviewCommentStatusFailed, maxAttempts)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
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

func (r *accountShareModeRepository) ListAPIKeyBindingMemberships(ctx context.Context, consumerUserID int64, apiKeyID int64) ([]service.AccountShareMembership, error) {
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
			AND m.status IN ($3, $4, $5)
			AND m.deleted_at IS NULL
		ORDER BY m.queue_rank ASC, m.id ASC
	`,
		consumerUserID,
		apiKeyID,
		service.AccountShareMembershipStatusActive,
		service.AccountShareMembershipStatusQueued,
		service.AccountShareMembershipStatusEnding,
	)
	if err != nil {
		return nil, err
	}

	memberships := make([]service.AccountShareMembership, 0, service.AccountShareModeQueueMaxItems+1)
	endingIndexes := make(map[int64]int)
	endingIDs := make([]int64, 0, 1)
	for rows.Next() {
		membership, scanErr := scanAccountShareMembership(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		memberships = append(memberships, *membership)
		if membership.Status == service.AccountShareMembershipStatusEnding {
			endingIndexes[membership.ID] = len(memberships) - 1
			endingIDs = append(endingIDs, membership.ID)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(endingIDs) == 0 {
		return memberships, nil
	}

	endStateRows, err := r.db.QueryContext(ctx, `
		SELECT
			membership.id,
			membership.ending_requested_at,
			membership.ending_reason,
			membership.settlement_status,
			membership.ending_operation_id::text,
			COALESCE(operation.status, '')
		FROM account_share_memberships membership
		LEFT JOIN account_share_room_operations operation
			ON operation.id = membership.ending_operation_id
			AND operation.action = 'end_membership'
			AND operation.membership_id = membership.id
		WHERE membership.id = ANY($1)
			AND membership.consumer_user_id = $2
			AND membership.api_key_id = $3
			AND membership.deleted_at IS NULL
	`, pq.Array(endingIDs), consumerUserID, apiKeyID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = endStateRows.Close() }()

	loadedEndingStates := make(map[int64]struct{}, len(endingIDs))
	for endStateRows.Next() {
		var (
			membershipID     int64
			endingRequested  sql.NullTime
			endingReason     sql.NullString
			settlementStatus sql.NullString
			endingOperation  sql.NullString
			operationStatus  string
		)
		if err := endStateRows.Scan(
			&membershipID,
			&endingRequested,
			&endingReason,
			&settlementStatus,
			&endingOperation,
			&operationStatus,
		); err != nil {
			return nil, err
		}
		index, ok := endingIndexes[membershipID]
		if !ok {
			return nil, fmt.Errorf("unexpected account-share ending state for membership %d", membershipID)
		}
		memberships[index].EndingRequestedAt = sqlNullTimePtr(endingRequested)
		memberships[index].EndingReason = strings.TrimSpace(endingReason.String)
		memberships[index].SettlementStatus = strings.TrimSpace(settlementStatus.String)
		memberships[index].EndingOperationID = strings.TrimSpace(endingOperation.String)
		memberships[index].EndingOperationStatus = strings.TrimSpace(operationStatus)
		loadedEndingStates[membershipID] = struct{}{}
	}
	if err := endStateRows.Err(); err != nil {
		return nil, err
	}
	for _, membershipID := range endingIDs {
		if _, ok := loadedEndingStates[membershipID]; !ok {
			return nil, fmt.Errorf("account-share ending state unavailable for membership %d", membershipID)
		}
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
	// The partial unique index uq_account_share_memberships_queue_rank spans
	// (api_key_id, queue_rank) over live rows, so the final 1..N ranks must be
	// staged through a temporary range that cannot collide with any live row.
	// Enqueue assigns MAX(queue_rank)+1, which climbs unbounded across
	// join/leave churn — the old "100+index" offset was only safe while every
	// live rank stayed below 100, and reorder is a client action, so a large
	// enough rank makes 100+index collide with a not-yet-rewritten batch row
	// and trips the unique index. Negative temp ranks are disjoint from all
	// valid (>=1) ranks and therefore always safe.
	for index, id := range membershipIDs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE account_share_memberships
			SET queue_rank = $1,
				updated_at = NOW()
			WHERE id = $2
		`, -(index + 1), id); err != nil {
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
	// 空闲超时自动退出也必须关闭 binding（与 FinalizeMembershipEnd 对齐），
	// 否则残留孤儿 binding 阻塞账号/房间删除。
	if _, err := r.closeAccountShareMembershipBindingInTx(
		ctx,
		tx,
		membership.ID,
		membership.ConsumerUserID,
		"consumer",
		"membership_idle_timeout",
		endedAt,
	); err != nil {
		return nil, nil, err
	}
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
	`, accountShareMembershipPermanentlyUnavailableConditionSQL("$2::timestamptz"))
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
	result, unavailableErr := r.processUnavailableMembershipIDs(ctx, ids, result, now)
	if result == nil {
		result = &service.AccountShareSeatBillingResult{Processed: len(ids)}
	}
	// 排队过期清扫使用独立预算，且不因不可用成员处理出错而被饿死——
	// 单条毒数据不再中断整条清理链（这是旧版排空长期卡死的帮凶之一）。
	endedCount, endedUserIDs, staleErr := r.endStaleQueuedMemberships(ctx, now, limit)
	if staleErr == nil {
		result.Processed += endedCount
		result.EndedConsumerUserIDs = append(result.EndedConsumerUserIDs, endedUserIDs...)
	}
	return result, errors.Join(unavailableErr, staleErr)
}

// CleanupOrphanMembershipBindings 兜底清理历史遗留的孤儿 binding：membership 已 ended
// （或已删除）但 binding 仍 unbound_at 为 NULL 的行。这类行由早期 idle/预扣耗尽/账号
// 不可用结束路径遗漏产生，会被账号删除守卫判为不可解析的阻塞项（account_repo.go:2567），
// 导致账号/房间永远删不掉。正常结束路径（FinalizeMembershipEnd/EndIdleMembership/
// endSeatBillingMembershipInTx）现已全部关闭 binding，本方法只处理存量脏数据。
func (r *accountShareModeRepository) CleanupOrphanMembershipBindings(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit <= 0 {
		limit = service.AccountShareModeSeatBillingBatchSize
	}
	if limit > 1000 {
		limit = 1000
	}
	now = now.UTC()
	result, err := r.db.ExecContext(ctx, `
		UPDATE account_share_membership_account_bindings binding
		SET unbound_at = $1,
			unbound_by_user_id = NULL,
			unbound_by_role = 'system',
			unbind_reason = 'orphan_cleanup'
		WHERE binding.id IN (
			SELECT binding.id
			FROM account_share_membership_account_bindings binding
			JOIN account_share_memberships membership
				ON membership.id = binding.membership_id
			WHERE binding.unbound_at IS NULL
				AND (membership.deleted_at IS NOT NULL OR membership.status = $2)
			ORDER BY binding.id ASC
			LIMIT $3
			FOR UPDATE OF binding
		)
	`, now, service.AccountShareMembershipStatusEnded, limit)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(affected), nil
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
	`, accountShareMembershipSuspendableUnavailableConditionSQL("$2::timestamptz")), service.AccountShareMembershipStatusActive, now, limit)
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
	if err := r.lockRecoverableUnavailableMembershipResourcesInTx(ctx, tx, membershipID); errors.Is(err, sql.ErrNoRows) || errors.Is(err, service.ErrAccountShareListingNotFound) {
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
	recoverable, err := r.accountShareMembershipSuspendableUnavailableInTx(ctx, tx, membership.ListingID, membership.AccountID, unavailableAt)
	if err != nil {
		return nil, nil, err
	}
	if !recoverable {
		return nil, nil, nil
	}
	replacementAvailable, err := r.accountShareMembershipHealthyReplacementAvailableInTx(
		ctx,
		tx,
		membership.ListingID,
		membership.AccountID,
		unavailableAt,
	)
	if err != nil {
		return nil, nil, err
	}
	if replacementAvailable {
		// 服务层会在下一次解析时执行正式重绑；这里必须保留 active/binding，
		// 避免“检测后新增健康账号”的并发窗口仍把 membership 排队。
		return nil, nil, nil
	}
	membership, creditUserIDs, err := r.suspendActiveMembershipInTx(
		ctx,
		tx,
		membership,
		unavailableAt,
		unavailableAt.Add(service.AccountShareModeDispatchCooldown),
	)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	tx = nil
	return membership, accountShareMembershipBillingResult(membership, creditUserIDs), nil
}

func (r *accountShareModeRepository) accountShareMembershipHealthyReplacementAvailableInTx(ctx context.Context, tx *sql.Tx, listingID, currentAccountID int64, now time.Time) (bool, error) {
	if tx == nil || listingID <= 0 || currentAccountID <= 0 {
		return false, nil
	}
	query := fmt.Sprintf(`
		SELECT EXISTS (
			SELECT 1
			FROM account_share_room_accounts room_account
			JOIN accounts a ON a.id = room_account.account_id
			WHERE room_account.listing_id = $1
				AND room_account.account_id <> $2
				AND room_account.state = 'active'
				AND a.deleted_at IS NULL
				AND NOT %s
		)
	`, accountShareAccountUnavailableConditionSQL("$3::timestamptz"))
	var available bool
	if err := tx.QueryRowContext(ctx, query, listingID, currentAccountID, now.UTC()).Scan(&available); err != nil {
		return false, err
	}
	return available, nil
}

// lockRecoverableUnavailableMembershipResourcesInTx serializes recoverable suspension
// with room rebind, listing relists and account recovery. Listing discovery is read-only;
// every mutable fact is rechecked after locking the canonical room rebind scope. The caller
// locks the membership afterwards, preserving listing -> room projection/accounts ->
// membership order without trusting a pre-lock account_id snapshot.
func (r *accountShareModeRepository) lockRecoverableUnavailableMembershipResourcesInTx(ctx context.Context, tx *sql.Tx, membershipID int64) error {
	var listingID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT listing_id
		FROM account_share_memberships
		WHERE id = $1
			AND status = $2
			AND deleted_at IS NULL
	`, membershipID, service.AccountShareMembershipStatusActive).Scan(&listingID); err != nil {
		return err
	}
	_, err := lockAccountShareMembershipRebindScopeInTx(ctx, tx, listingID)
	return err
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
	`, accountShareAccountPermanentlyUnavailableConditionSQL("$3::timestamptz"))
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
	rows, err := r.db.QueryContext(ctx, `
		WITH candidates AS (
			SELECT
				m.id,
				(m.queue_expires_at <= $2) AS queue_expired
			FROM account_share_memberships m
			JOIN account_share_listings l ON l.id = m.listing_id
			WHERE m.status = $1
				AND m.deleted_at IS NULL
				AND (
					m.queue_expires_at <= $2
					OR
					l.deleted_at IS NOT NULL
					OR l.status IN ($3, $4, 'draining')
				)
			ORDER BY COALESCE(m.queue_expires_at, m.joined_at) ASC, m.id ASC
			LIMIT $5
			FOR UPDATE OF m SKIP LOCKED
		)
		UPDATE account_share_memberships m
		SET status = $6,
			account_id = CASE WHEN $9::boolean THEN NULL ELSE m.account_id END,
			ended_at = $2,
			ended_reason = CASE
				WHEN c.queue_expired THEN $7
				ELSE $8
			END,
			paid_until = NULL,
			billed_until = NULL,
			waiver_window_started_at = NULL,
			waiver_window_usage_amount = 0,
			waiver_window_request_count = 0,
			waiver_window_last_request_at = NULL,
			dispatch_failed_at = NULL,
			dispatch_cooldown_until = NULL,
			settlement_status = 'not_required',
			updated_at = NOW()
		FROM candidates c
		WHERE m.id = c.id
		RETURNING m.consumer_user_id
	`,
		service.AccountShareMembershipStatusQueued,
		endedAt,
		service.AccountShareListingStatusDisabled,
		service.AccountShareListingStatusSuspended,
		limit,
		service.AccountShareMembershipStatusEnded,
		service.AccountShareMembershipEndReasonQueueExpired,
		service.AccountShareMembershipEndReasonUnavailable,
		r.deferredQueueBindingEnabled(),
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
			WHERE l.status = $1
				AND l.deleted_at IS NULL
				AND NOT EXISTS (
					SELECT 1
					FROM account_share_room_accounts room_account
					JOIN accounts a ON a.id = room_account.account_id
					WHERE room_account.listing_id = l.id
						AND room_account.state = 'active'
						AND NOT %s
				)
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
	`, accountShareAccountPermanentlyUnavailableConditionSQL("$4::timestamptz"))
	rows, err := r.db.QueryContext(
		ctx,
		query,
		service.AccountShareListingStatusActive,
		r.listingSuspensionStatus(),
		limit,
		now,
	)
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
	unavailable, err := r.accountShareMembershipPermanentlyUnavailableInTx(ctx, tx, membership.ListingID, membership.AccountID, endedAt)
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
	`, accountShareMembershipRecoverablyUnavailableConditionSQL("$2::timestamptz")), service.AccountShareMembershipStatusActive, now, limit)
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

func seatWaiverCompensationReadyBefore(now time.Time) time.Time {
	delay := service.AccountShareModeSeatWaiverCompensationDelay
	if delay <= 0 {
		delay = service.AccountShareModeSeatWaiverSettlementGrace
	}
	return now.UTC().Add(-delay)
}

// ProcessSeatWaiverBacklogCompensations 处理从未评估过的 seat_charge 积压
// (waiver_evaluated_at IS NULL,主要是迁移 203 回炉的历史行)。
// ORDER BY 必须以 waiver_evaluated_at 打头:IS NULL 不参与 planner 的 pathkey
// 消除,不显式写进排序头部就拿不到 202 部分索引的有序扫描,LIMIT 无法截断。
// 匹配集内该列全为 NULL,结果顺序语义与 (period_ended_at, id) 相同。
func (r *accountShareModeRepository) ProcessSeatWaiverBacklogCompensations(ctx context.Context, now time.Time, limit int, cursorPeriodEndedAt time.Time, cursorID int64) (*service.AccountShareSeatWaiverBatch, error) {
	if limit <= 0 {
		limit = service.AccountShareModeSeatWaiverCompensationBatchSize
	}
	readyBefore := seatWaiverCompensationReadyBefore(now)

	args := []any{accountShareSeatSettlementTypeCharge, accountShareSeatSettlementTypeWaiverRefund, readyBefore}
	// 游标只在非零时拼入:写成 "$n IS NULL OR ..." 会把 row-compare 挤出 Index Cond。
	cursorClause := ""
	if !cursorPeriodEndedAt.IsZero() {
		args = append(args, cursorPeriodEndedAt.UTC(), cursorID)
		cursorClause = "AND (sc.period_ended_at, sc.id) > ($4, $5)"
	}
	args = append(args, limit)
	query := fmt.Sprintf(`
		SELECT sc.id, sc.period_ended_at
		FROM account_share_mode_settlement_entries sc
		JOIN account_share_memberships m ON m.id = sc.membership_id
		WHERE sc.settlement_type = $1
			AND sc.hourly_charge > 0
			AND sc.period_started_at IS NOT NULL
			AND sc.period_ended_at IS NOT NULL
			AND sc.waiver_evaluated_at IS NULL
			AND sc.period_ended_at > sc.period_started_at
			AND sc.period_ended_at <= $3
			%s
			AND COALESCE(NULLIF(sc.waiver_minimum_snapshot, 0), m.hourly_fee_waiver_minimum_snapshot) > 0
			AND NOT EXISTS (
				SELECT 1
				FROM account_share_mode_settlement_entries wr
				WHERE wr.membership_id = sc.membership_id
					AND wr.settlement_type = $2
					AND wr.period_started_at = sc.period_started_at
					AND wr.period_ended_at = sc.period_ended_at
			)
		ORDER BY sc.waiver_evaluated_at ASC, sc.period_ended_at ASC, sc.id ASC
		LIMIT $%d
	`, cursorClause, len(args))
	return r.runSeatWaiverCompensationBatch(ctx, query, args, readyBefore, limit)
}

// ProcessSeatWaiverLateUsageCompensations 反查迟到 usage 触发的重评:
// 已评估行中,存在与其计费窗口重叠、且晚于评估时间落账的 usage_request 条目。
// usageSince 约束迟到条目的 created_at(迟到落账必然新近);windowSince 是由
// 不变量 waiver_evaluated_at >= period_ended_at(三条写入路径均保证)推导出的
// 语义超集双下界,让两列都进入 202 索引的 Index Cond。
func (r *accountShareModeRepository) ProcessSeatWaiverLateUsageCompensations(ctx context.Context, now time.Time, limit int, usageSince, windowSince time.Time, cursorPeriodEndedAt time.Time, cursorID int64) (*service.AccountShareSeatWaiverBatch, error) {
	if limit <= 0 {
		limit = service.AccountShareModeSeatWaiverCompensationBatchSize
	}
	readyBefore := seatWaiverCompensationReadyBefore(now)

	args := []any{
		accountShareSeatSettlementTypeCharge,
		accountShareSeatSettlementTypeWaiverRefund,
		accountShareSeatSettlementTypeUsage,
		readyBefore,
		windowSince.UTC(),
		usageSince.UTC(),
	}
	cursorClause := ""
	if !cursorPeriodEndedAt.IsZero() {
		args = append(args, cursorPeriodEndedAt.UTC(), cursorID)
		cursorClause = "AND (sc.period_ended_at, sc.id) > ($7, $8)"
	}
	args = append(args, limit)
	query := fmt.Sprintf(`
		SELECT sc.id, sc.period_ended_at
		FROM account_share_mode_settlement_entries sc
		JOIN account_share_memberships m ON m.id = sc.membership_id
		WHERE sc.settlement_type = $1
			AND sc.hourly_charge > 0
			AND sc.period_started_at IS NOT NULL
			AND sc.period_ended_at IS NOT NULL
			AND sc.period_ended_at > sc.period_started_at
			AND sc.period_ended_at <= $4
			AND sc.period_ended_at >= $5
			AND sc.waiver_evaluated_at IS NOT NULL
			AND sc.waiver_evaluated_at >= $5
			%s
			AND COALESCE(NULLIF(sc.waiver_minimum_snapshot, 0), m.hourly_fee_waiver_minimum_snapshot) > 0
			AND EXISTS (
				SELECT 1
				FROM account_share_mode_settlement_entries e
				LEFT JOIN usage_logs ul ON ul.id = e.usage_log_id
				WHERE e.membership_id = sc.membership_id
					AND e.settlement_type = $3
					AND e.created_at >= $6
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
			AND NOT EXISTS (
				SELECT 1
				FROM account_share_mode_settlement_entries wr
				WHERE wr.membership_id = sc.membership_id
					AND wr.settlement_type = $2
					AND wr.period_started_at = sc.period_started_at
					AND wr.period_ended_at = sc.period_ended_at
			)
		ORDER BY sc.period_ended_at ASC, sc.id ASC
		LIMIT $%d
	`, cursorClause, len(args))
	return r.runSeatWaiverCompensationBatch(ctx, query, args, readyBefore, limit)
}

func (r *accountShareModeRepository) runSeatWaiverCompensationBatch(ctx context.Context, query string, args []any, readyBefore time.Time, limit int) (*service.AccountShareSeatWaiverBatch, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	ids := make([]int64, 0, limit)
	batch := &service.AccountShareSeatWaiverBatch{}
	for rows.Next() {
		var id int64
		var periodEndedAt time.Time
		if err := rows.Scan(&id, &periodEndedAt); err != nil {
			return nil, err
		}
		ids = append(ids, id)
		batch.CursorPeriodEndedAt = periodEndedAt
		batch.CursorID = id
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	batch.Matched = len(ids)

	result := &service.AccountShareSeatBillingResult{Processed: len(ids)}
	batch.Billing = result
	for _, id := range ids {
		item, err := r.processSeatWaiverCompensation(ctx, id, readyBefore)
		if err != nil {
			return batch, err
		}
		if item == nil {
			continue
		}
		result.DebitUserIDs = append(result.DebitUserIDs, item.DebitUserIDs...)
		result.CreditUserIDs = append(result.CreditUserIDs, item.CreditUserIDs...)
	}
	return batch, nil
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
	unavailable, err := r.accountShareMembershipPermanentlyUnavailableInTx(ctx, tx, membership.ListingID, membership.AccountID, now)
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
			// 预扣耗尽自动终结也必须关闭 binding（与 FinalizeMembershipEnd 对齐），
			// 否则残留孤儿 binding 阻塞账号/房间删除。
			if _, err := r.closeAccountShareMembershipBindingInTx(
				ctx,
				tx,
				membership.ID,
				membership.ConsumerUserID,
				"consumer",
				"membership_ended",
				*settledUntil,
			); err != nil {
				return nil, err
			}
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
			INSERT INTO user_affiliate_ledger (user_id, action, amount, source_user_id, created_at, updated_at)
			VALUES ($1, $2, $3::numeric, $4, NOW(), NOW())
		`, inviterUserID, affiliateLedgerActionShareReverse, charge.Split.InviteCredit.StringFixed(10), membership.ConsumerUserID); err != nil {
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
	// 结束成员关系必须同时关闭其 account-share binding（与 FinalizeMembershipEnd 对齐），
	// 否则残留 unbound_at 为 NULL 的孤儿 binding，会被账号删除守卫判为不可解析的阻塞项
	// （account_repo.go:2567），导致账号/房间永远删不掉。
	if _, err := r.closeAccountShareMembershipBindingInTx(
		ctx,
		tx,
		membership.ID,
		membership.ConsumerUserID,
		"consumer",
		"membership_ended",
		endedAt,
	); err != nil {
		return nil, err
	}
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
	`, accountShareAccountUnavailableConditionSQL("$2::timestamptz"))
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
	`, accountShareAccountPermanentlyUnavailableConditionSQL("$2::timestamptz"))
	var unavailable bool
	if err := tx.QueryRowContext(ctx, query, accountID, now.UTC()).Scan(&unavailable); err != nil {
		return false, err
	}
	if unavailable {
		logger.LegacyPrintf("repository.account_share_mode", "account share permanently unavailable matched: account_id=%d now=%s details=%s", accountID, now.UTC().Format(time.RFC3339), r.accountShareAccountUnavailableDetailsInTx(ctx, tx, accountID, now))
	}
	return unavailable, nil
}

func (r *accountShareModeRepository) accountShareMembershipPermanentlyUnavailableInTx(ctx context.Context, tx *sql.Tx, listingID, accountID int64, now time.Time) (bool, error) {
	if listingID <= 0 || accountID <= 0 {
		return false, nil
	}
	query := fmt.Sprintf(`
		SELECT NOT EXISTS (
			SELECT 1
			FROM account_share_listings l
			JOIN account_share_room_accounts room_account
				ON room_account.listing_id = l.id
				AND room_account.account_id = $2
				AND room_account.state IN ('active', 'draining')
			JOIN accounts a ON a.id = room_account.account_id
			WHERE l.id = $1
				AND l.deleted_at IS NULL
				AND NOT %s
		)
	`, accountShareMembershipPermanentlyUnavailableConditionSQL("$3::timestamptz"))
	var unavailable bool
	if err := tx.QueryRowContext(ctx, query, listingID, accountID, now.UTC()).Scan(&unavailable); err != nil {
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
			JOIN account_share_room_accounts room_account
				ON room_account.listing_id = l.id
				AND room_account.account_id = $2
				AND room_account.state IN ('active', 'draining')
			JOIN accounts a ON a.id = room_account.account_id
			WHERE l.id = $1
				AND %s
		)
	`, accountShareMembershipRecoverablyUnavailableConditionSQL("$3::timestamptz"))
	var unavailable bool
	if err := tx.QueryRowContext(ctx, query, listingID, accountID, now.UTC()).Scan(&unavailable); err != nil {
		return false, err
	}
	if unavailable {
		logger.LegacyPrintf("repository.account_share_mode", "account share membership recoverably unavailable matched: membership_listing_id=%d account_id=%d now=%s", listingID, accountID, now.UTC().Format(time.RFC3339))
	}
	return unavailable, nil
}

func (r *accountShareModeRepository) accountShareMembershipSuspendableUnavailableInTx(ctx context.Context, tx *sql.Tx, listingID, accountID int64, now time.Time) (bool, error) {
	if listingID <= 0 || accountID <= 0 {
		return false, nil
	}
	query := fmt.Sprintf(`
		SELECT EXISTS (
			SELECT 1
			FROM account_share_listings l
			JOIN account_share_room_accounts room_account
				ON room_account.listing_id = l.id
				AND room_account.account_id = $2
				AND room_account.state IN ('active', 'draining')
			JOIN accounts a ON a.id = room_account.account_id
			WHERE l.id = $1
				AND %s
		)
	`, accountShareMembershipSuspendableUnavailableConditionSQL("$3::timestamptz"))
	var unavailable bool
	if err := tx.QueryRowContext(ctx, query, listingID, accountID, now.UTC()).Scan(&unavailable); err != nil {
		return false, err
	}
	if unavailable {
		logger.LegacyPrintf("repository.account_share_mode", "account share membership suspendable unavailable matched: membership_listing_id=%d account_id=%d now=%s", listingID, accountID, now.UTC().Format(time.RFC3339))
	}
	return unavailable, nil
}

func (r *accountShareModeRepository) accountShareAccountUnavailableDetailsInTx(ctx context.Context, tx *sql.Tx, accountID int64, now time.Time) string {
	query := fmt.Sprintf(`
		SELECT
			a.status,
			a.schedulable,
			(a.auto_pause_on_expired = TRUE AND a.expires_at IS NOT NULL AND a.expires_at <= $2::timestamptz) AS expired,
			(a.overload_until IS NOT NULL AND a.overload_until > $2::timestamptz) AS overload,
			(a.rate_limit_reset_at IS NOT NULL AND a.rate_limit_reset_at > $2::timestamptz) AS rate_limited,
			(a.temp_unschedulable_until IS NOT NULL AND a.temp_unschedulable_until > $2::timestamptz) AS temp_unschedulable,
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
	`, accountShareCodexQuotaProtectedSQL("codex_5h_used_percent", "codex_5h_reset_at", "codex_5h_limit_percent", "$2::timestamptz"),
		accountShareCodexQuotaProtectedSQL("codex_7d_used_percent", "codex_7d_reset_at", "codex_7d_limit_percent", "$2::timestamptz"))
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
	split.OwnerCredit, split.InviteCredit, split.PlatformCredit = splitAccountShareCredits(
		totalCharge,
		split.OwnerRatio,
		split.InviteRatio,
	)
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
	membership, listing, err := r.queryActiveMembership(ctx, `
		m.consumer_user_id = $1
		AND m.api_key_id = $2
		AND a.platform = (
			SELECT mg.platform
			FROM account_share_mode_groups mg
			WHERE mg.group_id = $3
		)
	`, userID, apiKeyID, groupID)
	if err == nil {
		return membership, listing, nil
	}
	if !errors.Is(err, service.ErrAccountShareListingNotFound) {
		return nil, nil, err
	}
	// 无 active membership 时探测是否有「退出结算中」(ending) 的同平台 membership。
	// 若有，说明用户刚结束使用、结算尚未完成——此时路由到「未绑定账号」会误导用户
	// 去重新授权/解绑。返回专用的 ACCOUNT_SHARE_MEMBERSHIP_ENDING，让 handler 给出
	// 「正在退出结算，请稍候」的中文提示。
	ending, err := r.membershipEndingPendingForRequest(ctx, userID, apiKeyID, groupID)
	if err != nil {
		return nil, nil, err
	}
	if ending {
		return nil, nil, service.ErrAccountShareMembershipEnding
	}
	return nil, nil, service.ErrAccountShareListingNotFound
}

// membershipEndingPendingForRequest 判断该 (userID, apiKeyID) 在指定平台分组上
// 是否有未完成结算的 ending membership。
func (r *accountShareModeRepository) membershipEndingPendingForRequest(ctx context.Context, userID, apiKeyID, groupID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM account_share_memberships m
			JOIN account_share_listings l ON l.id = m.listing_id
				AND l.deleted_at IS NULL
			JOIN accounts a ON a.id = m.account_id
			WHERE m.consumer_user_id = $1
				AND m.api_key_id = $2
				AND m.status = $3
				AND m.deleted_at IS NULL
				AND a.platform = (
					SELECT mg.platform
					FROM account_share_mode_groups mg
					WHERE mg.group_id = $4
				)
		)
	`, userID, apiKeyID, service.AccountShareMembershipStatusEnding, groupID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
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
	if _, err := endStaleQueuedMembershipsForAPIKeyInTx(
		ctx,
		tx,
		userID,
		apiKeyID,
		now,
		r.deferredQueueBindingEnabled(),
	); err != nil {
		return nil, nil, err
	}
	lockedListingIDs, err := r.lockQueuedMembershipListingsForRequestInTx(ctx, tx, userID, apiKeyID, groupID, now)
	if err != nil {
		return nil, nil, err
	}
	if len(lockedListingIDs) == 0 {
		return nil, nil, accountShareMembershipRequestStateErrorInTx(ctx, tx, userID, apiKeyID, groupID, now)
	}

	var membershipID, listingID, accountID, ownerUserID, listingRevisionID int64
	var queueRank, idleTimeoutMinutes int
	err = tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT
			m.id, m.listing_id, a.id, l.owner_user_id, m.listing_revision_id, m.queue_rank, m.idle_timeout_minutes
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
			AND l.deleted_at IS NULL
		%s
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
			AND m.queue_expires_at > $5
			AND l.id = ANY($7::bigint[])
			AND %s
		ORDER BY CASE WHEN m.queue_rank > $6 THEN 0 ELSE 1 END,
			m.queue_rank ASC,
			m.id ASC
		LIMIT 1
		FOR UPDATE OF m
	`, accountShareRoomRepresentativeJoinSQL("$5"), accountShareQueuedActivationConditionSQL("$5", "$1")),
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
		&listingRevisionID,
		&queueRank,
		&idleTimeoutMinutes,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, accountShareMembershipRequestStateErrorInTx(ctx, tx, userID, apiKeyID, groupID, now)
	}
	if err != nil {
		return nil, nil, err
	}

	queuedMembership := &service.AccountShareMembership{
		ID:        membershipID,
		ListingID: listingID,
		Status:    service.AccountShareMembershipStatusQueued,
	}
	if err := loadAndValidateAccountShareMembershipTermsSnapshotInTx(ctx, tx, queuedMembership); err != nil {
		return nil, nil, err
	}
	if err := validateAccountShareMembershipTermsRevisionInTx(ctx, tx, queuedMembership); err != nil {
		return nil, nil, err
	}
	if queuedMembership.ListingRevisionID == nil || *queuedMembership.ListingRevisionID != listingRevisionID {
		return nil, nil, fmt.Errorf(
			"%w: queued membership %d selected revision does not match its immutable terms",
			service.ErrAccountShareBillingBindingUnavailable,
			membershipID,
		)
	}
	terms := queuedMembership.TermsSnapshot
	hourlyRate := terms.HourlyRate
	hourlyFeeWaiverMinimum := terms.HourlyFeeWaiverMinimum
	minBalanceRequired := terms.MinBalanceRequired
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
			account_id = $2,
			hourly_rate_snapshot = $3,
			hourly_fee_waiver_minimum_snapshot = $4,
			idle_timeout_minutes = $5,
			joined_at = $6,
			last_request_at = NULL,
			ended_at = NULL,
			ended_reason = NULL,
			paid_until = $7,
			billed_until = $8,
			waiver_window_started_at = $8,
			waiver_window_usage_amount = 0,
			waiver_window_request_count = 0,
			waiver_window_last_request_at = NULL,
			dispatch_failed_at = NULL,
			dispatch_cooldown_until = NULL,
			queue_expires_at = NULL,
			updated_at = NOW()
		FROM account_share_listings l
		WHERE m.id = $9
			AND m.status = $10
			AND m.deleted_at IS NULL
			AND l.id = m.listing_id
			AND l.deleted_at IS NULL
			AND l.status = $11
			AND (l.editing_expires_at IS NULL OR l.editing_expires_at <= $6)
			AND (
				l.owner_user_id = m.consumer_user_id
				OR l.seat_limit > (
					SELECT COUNT(*)::int
					FROM account_share_memberships m_occupied
					WHERE m_occupied.listing_id = l.id
						AND m_occupied.status IN ($12, $13)
						AND m_occupied.deleted_at IS NULL
						AND m_occupied.consumer_user_id <> l.owner_user_id
				)
			)
		RETURNING
			m.id, m.listing_id, m.account_id, l.owner_user_id, m.consumer_user_id, m.api_key_id,
			m.status, m.queue_rank, m.hourly_rate_snapshot, m.hourly_fee_waiver_minimum_snapshot, m.idle_timeout_minutes,
			m.joined_at, m.last_request_at, m.ended_at, m.ended_reason, m.paid_until, m.billed_until,
			m.waiver_window_started_at, m.waiver_window_usage_amount, m.waiver_window_request_count, m.waiver_window_last_request_at,
			m.dispatch_failed_at, m.dispatch_cooldown_until, m.created_at, m.updated_at
	`,
		service.AccountShareMembershipStatusActive,
		accountID,
		hourlyRate,
		hourlyFeeWaiverMinimum,
		idleTimeoutMinutes,
		now,
		paidUntilValue,
		billedUntilValue,
		membershipID,
		service.AccountShareMembershipStatusQueued,
		service.AccountShareListingStatusActive,
		service.AccountShareMembershipStatusActive,
		service.AccountShareMembershipStatusEnding,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, accountShareMembershipRequestStateErrorInTx(ctx, tx, userID, apiKeyID, groupID, now)
	}
	if err != nil {
		return nil, nil, translateAccountShareMembershipConflict(err)
	}
	membership.QueueRank = queueRank
	boundByRole := "consumer"
	if ownerSelfUse {
		boundByRole = "owner"
	}
	if _, _, err := r.createAccountShareMembershipBindingInTx(
		ctx,
		tx,
		membership.ID,
		listingID,
		accountID,
		listingRevisionID,
		userID,
		boundByRole,
		"queue_activation",
		now,
	); err != nil {
		return nil, nil, err
	}
	if err := loadAndValidateAccountShareMembershipRuntimeSnapshotInTx(ctx, tx, membership); err != nil {
		return nil, nil, err
	}
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
	listing, err := r.getListingByMembershipAccount(ctx, membership)
	if err != nil {
		return nil, nil, err
	}
	if err := applyAccountShareMembershipRuntimeTerms(membership, listing); err != nil {
		return nil, nil, err
	}
	return membership, listing, nil
}

func accountShareMembershipRequestStateErrorInTx(ctx context.Context, tx *sql.Tx, userID, apiKeyID, groupID int64, now time.Time) error {
	if tx == nil {
		return service.ErrAccountShareListingNotFound
	}
	now = now.UTC()
	// queryActiveMembership 会在付费席位到期、尚未完成续扣时暂时隐藏 active
	// membership。账号正处于短 429 等可恢复状态时，续扣会按设计暂停，因此这里
	// 必须先识别仍然存在的 active 关系；否则后续既找不到 queued，也找不到 ended，
	// 最终会把“恢复中”再次误报成“未绑定”。
	var activeMembershipExists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM account_share_memberships m
			JOIN account_share_listings l ON l.id = m.listing_id
			WHERE m.consumer_user_id = $1
				AND m.api_key_id = $2
				AND m.status = $3
				AND m.deleted_at IS NULL
				AND l.platform = (
					SELECT mg.platform
					FROM account_share_mode_groups mg
					WHERE mg.group_id = $4
				)
		)
	`, userID, apiKeyID, service.AccountShareMembershipStatusActive, groupID).Scan(&activeMembershipExists); err != nil {
		return err
	}
	if activeMembershipExists {
		return service.NewAccountShareModeRecoveringError(service.AccountShareModeDefaultRecoveryRetryAfter)
	}

	var dispatchCooldownUntil sql.NullTime
	err := tx.QueryRowContext(ctx, `
		SELECT m.dispatch_cooldown_until
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
		WHERE m.consumer_user_id = $1
			AND m.api_key_id = $2
			AND m.status = $3
			AND m.deleted_at IS NULL
			AND m.queue_expires_at > $5
			AND l.platform = (
				SELECT mg.platform
				FROM account_share_mode_groups mg
				WHERE mg.group_id = $4
			)
		ORDER BY m.queue_rank ASC, m.id ASC
		LIMIT 1
	`, userID, apiKeyID, service.AccountShareMembershipStatusQueued, groupID, now).Scan(&dispatchCooldownUntil)
	if err == nil {
		retryAfterSeconds := service.AccountShareModeDefaultRecoveryRetryAfter
		if dispatchCooldownUntil.Valid && dispatchCooldownUntil.Time.After(now) {
			retryAfterSeconds = int(math.Ceil(dispatchCooldownUntil.Time.Sub(now).Seconds()))
			if retryAfterSeconds <= 0 {
				retryAfterSeconds = 1
			}
		}
		return service.NewAccountShareModeRecoveringError(retryAfterSeconds)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	var endedReason sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT m.ended_reason
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
		WHERE m.consumer_user_id = $1
			AND m.api_key_id = $2
			AND m.status = $3
			AND m.deleted_at IS NULL
			AND l.platform = (
				SELECT mg.platform
				FROM account_share_mode_groups mg
				WHERE mg.group_id = $4
			)
		ORDER BY COALESCE(m.ended_at, m.updated_at) DESC, m.id DESC
		LIMIT 1
	`, userID, apiKeyID, service.AccountShareMembershipStatusEnded, groupID).Scan(&endedReason)
	if err == nil && endedReason.Valid && endedReason.String == service.AccountShareMembershipEndReasonIdleTimeout {
		return service.ErrAccountShareMembershipIdleTimeout
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return service.ErrAccountShareListingNotFound
}

func (r *accountShareModeRepository) lockQueuedMembershipListingsForRequestInTx(ctx context.Context, tx *sql.Tx, userID, apiKeyID, groupID int64, now time.Time) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT l.id
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
			AND l.deleted_at IS NULL
		WHERE m.consumer_user_id = $1
			AND m.api_key_id = $2
			AND m.status = $3
			AND m.deleted_at IS NULL
			AND m.queue_expires_at > $5
			AND l.platform = (
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
	if _, err := r.closeAccountShareMembershipBindingInTx(
		ctx,
		tx,
		membership.ID,
		0,
		"system",
		"membership_requeued",
		failedAt,
	); err != nil {
		return nil, nil, err
	}
	membership, err = scanAccountShareMembership(tx.QueryRowContext(ctx, `
		UPDATE account_share_memberships m
		SET status = $1::varchar(20),
			account_id = CASE WHEN $7::boolean THEN NULL ELSE m.account_id END,
			paid_until = NULL,
			billed_until = $2::timestamptz,
			waiver_window_started_at = $2::timestamptz,
			waiver_window_usage_amount = 0,
			waiver_window_request_count = 0,
			waiver_window_last_request_at = NULL,
			dispatch_failed_at = $3::timestamptz,
			dispatch_cooldown_until = $4::timestamptz,
			queue_expires_at = $3::timestamptz + make_interval(hours => $8),
			updated_at = NOW()
		FROM account_share_listings l
		WHERE m.id = $5::bigint
			AND l.id = m.listing_id
			AND m.status = $6::varchar(20)
			AND m.deleted_at IS NULL
		RETURNING
			m.id, m.listing_id, m.account_id, l.owner_user_id, m.consumer_user_id, m.api_key_id,
			m.status, m.queue_rank, m.hourly_rate_snapshot, m.hourly_fee_waiver_minimum_snapshot, m.idle_timeout_minutes,
			m.joined_at, m.last_request_at, m.ended_at, m.ended_reason, m.paid_until, m.billed_until,
			m.waiver_window_started_at, m.waiver_window_usage_amount, m.waiver_window_request_count, m.waiver_window_last_request_at,
			m.dispatch_failed_at, m.dispatch_cooldown_until, m.created_at, m.updated_at
	`,
		service.AccountShareMembershipStatusQueued,
		*settledUntil,
		failedAt,
		cooldownUntil,
		membership.ID,
		service.AccountShareMembershipStatusActive,
		r.deferredQueueBindingEnabled(),
		service.AccountShareModeQueueExpiryDuration.Hours(),
	))
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
		return "COALESCE(room_stats.total_concurrency, a.concurrency, 0)"
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

func accountShareAccountUnavailableBlockerSQL(nowExpr string) string {
	return accountShareAccountUnavailableBlockerWithTransientRateLimitGraceSQL(nowExpr, 0)
}

func accountShareAccountUnavailableBlockerWithTransientRateLimitGraceSQL(nowExpr string, grace time.Duration) string {
	rateLimitConditionSQL := fmt.Sprintf(
		"a.rate_limit_reset_at IS NOT NULL AND a.rate_limit_reset_at > %s",
		nowExpr,
	)
	if grace > 0 {
		graceSeconds := int64(math.Ceil(grace.Seconds()))
		rateLimitConditionSQL += fmt.Sprintf(` AND NOT (
			a.rate_limited_at IS NOT NULL
			AND a.rate_limit_reset_at > a.rate_limited_at
			AND a.rate_limit_reset_at - a.rate_limited_at <= INTERVAL '%d seconds'
		)`, graceSeconds)
	}
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
	opencodeProtectedSQL := fmt.Sprintf(`(
		a.platform = '%s'
		AND a.type = '%s'
		AND (
			%s
			OR %s
			OR %s
		)
	)`,
		service.PlatformOpencode,
		service.AccountTypeAPIKey,
		accountShareCodexQuotaProtectedSQL("opencode_5h_used_percent", "opencode_5h_reset_at", "opencode_5h_limit_percent", nowExpr),
		accountShareCodexQuotaProtectedSQL("opencode_7d_used_percent", "opencode_7d_reset_at", "opencode_7d_limit_percent", nowExpr),
		accountShareCodexQuotaProtectedSQL("opencode_30d_used_percent", "opencode_30d_reset_at", "opencode_30d_limit_percent", nowExpr),
	)
	return fmt.Sprintf(`(CASE
		WHEN a.status <> '%s' THEN 'status_not_active'
		WHEN a.schedulable = FALSE THEN 'scheduling_disabled'
		WHEN a.concurrency <= 0 THEN 'non_positive_concurrency'
		WHEN a.auto_pause_on_expired = TRUE AND a.expires_at IS NOT NULL AND a.expires_at <= %s THEN 'expired'
		WHEN a.overload_until IS NOT NULL AND a.overload_until > %s THEN 'overloaded'
		WHEN %s THEN 'rate_limited'
		WHEN a.temp_unschedulable_until IS NOT NULL AND a.temp_unschedulable_until > %s THEN 'temporarily_unschedulable'
		WHEN %s THEN 'codex_quota_protected'
		WHEN %s THEN 'anthropic_quota_protected'
		WHEN %s THEN 'opencode_quota_protected'
		ELSE NULL
	END)`,
		service.StatusActive,
		nowExpr,
		nowExpr,
		rateLimitConditionSQL,
		nowExpr,
		codexProtectedSQL,
		anthropicProtectedSQL,
		opencodeProtectedSQL,
	)
}

func accountShareAccountUnavailableConditionSQL(nowExpr string) string {
	return fmt.Sprintf(`(%s IS NOT NULL)`, accountShareAccountUnavailableBlockerSQL(nowExpr))
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
				AND m_available.status IN ('%[4]s', '%[5]s')
				AND m_available.deleted_at IS NULL
				AND m_available.consumer_user_id <> l.owner_user_id
		)
	)`,
		service.AccountShareListingStatusActive,
		nowExpr,
		accountShareAccountUnavailableConditionSQL(nowExpr),
		service.AccountShareMembershipStatusActive,
		service.AccountShareMembershipStatusEnding,
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
					AND m_available.status IN ('%[5]s', '%[6]s')
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
		service.AccountShareMembershipStatusEnding,
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

func accountShareRoomRepresentativeJoinSQL(nowExpr string) string {
	return accountShareRoomRepresentativeJoinSQLWithType("JOIN LATERAL", nowExpr)
}

func accountShareRoomOptionalRepresentativeJoinSQL(nowExpr string) string {
	return accountShareRoomRepresentativeJoinSQLWithType("LEFT JOIN LATERAL", nowExpr)
}

func accountShareRoomRepresentativeJoinSQLWithType(joinType, nowExpr string) string {
	return fmt.Sprintf(`
		%s (
			SELECT a.*
			FROM account_share_room_accounts room_account
			JOIN accounts a ON a.id = room_account.account_id
			WHERE room_account.listing_id = l.id
				AND room_account.state = 'active'
				AND a.deleted_at IS NULL
			ORDER BY
				CASE WHEN %s THEN 1 ELSE 0 END,
				room_account.priority ASC,
				a.last_used_at ASC NULLS FIRST,
				a.id ASC
			LIMIT 1
		) a ON TRUE
	`, joinType, accountShareAccountUnavailableConditionSQL(nowExpr))
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
		OR l.status IN ('%s', '%s')
		OR %s
	)`,
		service.AccountShareListingStatusDisabled,
		service.AccountShareListingStatusSuspended,
		accountShareAccountPermanentlyUnavailableConditionSQL(nowExpr),
	)
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

// accountShareMembershipSuspendableUnavailableConditionSQL 仅用于把 active membership
// 持久化重排队。短 429 仍会被 billing predicate 识别并阻止续扣，但不会关闭长期 binding。
func accountShareMembershipSuspendableUnavailableConditionSQL(nowExpr string) string {
	accountUnavailableSQL := fmt.Sprintf(
		`(%s IS NOT NULL)`,
		accountShareAccountUnavailableBlockerWithTransientRateLimitGraceSQL(
			nowExpr,
			service.AccountShareModeTransientRateLimitGrace,
		),
	)
	return fmt.Sprintf(`(
		NOT %s
		AND (
			l.status = '%s'
			OR %s
		)
	)`,
		accountShareMembershipPermanentlyUnavailableConditionSQL(nowExpr),
		service.AccountShareListingStatusPaused,
		accountUnavailableSQL,
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
		WHEN COALESCE(NULLIF(a.account_level, ''), l.account_level) IN (%s) THEN COALESCE(NULLIF(a.account_level, ''), l.account_level)
		%s
		ELSE 'unknown'
	END`, strings.Join(accountLevelLiterals, ", "), strings.Join(whens, "\n\t\t"))
}

func accountShareSQLLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func accountShareListingSelectSQL() string {
	return accountShareListingSelectSQLWithAccountJoin(accountShareRoomOptionalRepresentativeJoinSQL("NOW()"))
}

func accountShareListingSelectSQLFromPage() string {
	return accountShareListingSelectSQLWithSourceAndCurrentMembershipJoin(
		"paged_listings",
		accountShareRoomOptionalRepresentativeJoinSQL("NOW()"),
		accountShareViewerCurrentMembershipJoinSQL(),
	)
}

func accountShareViewerCurrentMembershipCTESQL() string {
	return fmt.Sprintf(`viewer_current_membership AS MATERIALIZED (
		SELECT
			m.id,
			m.listing_id,
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
		WHERE m.consumer_user_id = $1
			AND m.status IN ('%s', '%s')
			AND m.deleted_at IS NULL
			AND (
				m.status = '%s'
				OR (
					(m.hourly_rate_snapshot <= 0 OR m.paid_until IS NULL OR m.paid_until > NOW())
					AND (m.idle_timeout_minutes <= 0 OR COALESCE(m.last_request_at, m.joined_at) + (m.idle_timeout_minutes * INTERVAL '1 minute') > NOW())
				)
			)
	)`,
		service.AccountShareMembershipStatusActive,
		service.AccountShareMembershipStatusEnding,
		service.AccountShareMembershipStatusEnding,
	)
}

func accountShareViewerCurrentMembershipJoinSQL() string {
	return `
		LEFT JOIN viewer_current_membership cm ON cm.listing_id = l.id`
}

func accountShareViewerCurrentMembershipFullLateralSQL() string {
	return fmt.Sprintf(`
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
				AND m.status IN ('%s', '%s')
				AND m.deleted_at IS NULL
				AND (
					m.status = '%s'
					OR (
						(m.hourly_rate_snapshot <= 0 OR m.paid_until IS NULL OR m.paid_until > NOW())
						AND (m.idle_timeout_minutes <= 0 OR COALESCE(m.last_request_at, m.joined_at) + (m.idle_timeout_minutes * INTERVAL '1 minute') > NOW())
					)
				)
			ORDER BY m.joined_at DESC
			LIMIT 1
		) cm ON TRUE`,
		service.AccountShareMembershipStatusActive,
		service.AccountShareMembershipStatusEnding,
		service.AccountShareMembershipStatusEnding,
	)
}

// accountShareListingSelectionJoinSQL 只为筛选或排序实际引用的别名拼接
// join。完整 god-view 的展示关联不经过这里，避免为了 count/page 输出列
// 引入无关的代表账号、用户或聚合扫描。
func accountShareListingSelectionJoinSQL(dependenciesSQL, currentMembershipJoinSQL string) string {
	var b strings.Builder
	if strings.Contains(dependenciesSQL, "a.") {
		_, _ = b.WriteString(accountShareRoomOptionalRepresentativeJoinSQL("NOW()"))
	}
	if strings.Contains(dependenciesSQL, "u.") {
		_, _ = b.WriteString(`
		LEFT JOIN users u ON u.id = l.owner_user_id`)
	}
	if strings.Contains(dependenciesSQL, "cm.") {
		_, _ = b.WriteString(currentMembershipJoinSQL)
	}
	if strings.Contains(dependenciesSQL, "qm.") {
		_, _ = b.WriteString(fmt.Sprintf(`
		LEFT JOIN LATERAL (
			SELECT m.id, m.queue_rank
			FROM account_share_memberships m
			WHERE m.listing_id = l.id
				AND m.consumer_user_id = $1
				AND m.status IN ('%s', '%s', '%s')
				AND m.deleted_at IS NULL
			ORDER BY
				CASE m.status
					WHEN '%s' THEN 0
					WHEN '%s' THEN 1
					ELSE 2
				END,
				m.queue_rank ASC,
				m.id DESC
			LIMIT 1
		) qm ON TRUE`,
			service.AccountShareMembershipStatusActive,
			service.AccountShareMembershipStatusQueued,
			service.AccountShareMembershipStatusEnding,
			service.AccountShareMembershipStatusActive,
			service.AccountShareMembershipStatusEnding,
		))
	}
	if strings.Contains(dependenciesSQL, "hm.") {
		_, _ = b.WriteString(fmt.Sprintf(`
		LEFT JOIN LATERAL (
			SELECT m.id, COALESCE(m.ended_at, m.updated_at) AS ended_at
			FROM account_share_memberships m
			WHERE m.listing_id = l.id
				AND m.consumer_user_id = $1
				AND m.status = '%s'
				AND m.deleted_at IS NULL
			ORDER BY COALESCE(m.ended_at, m.updated_at) DESC
			LIMIT 1
		) hm ON TRUE`,
			service.AccountShareMembershipStatusEnded,
		))
	}
	if strings.Contains(dependenciesSQL, "room_stats.") {
		_, _ = b.WriteString(fmt.Sprintf(`
		LEFT JOIN LATERAL (
			SELECT COALESCE(SUM(a.concurrency) FILTER (WHERE NOT %s), 0)::int AS total_concurrency
			FROM account_share_room_accounts room_account
			JOIN accounts a ON a.id = room_account.account_id
			WHERE room_account.listing_id = l.id
				AND room_account.state = 'active'
				AND a.deleted_at IS NULL
		) room_stats ON TRUE`, accountShareAccountUnavailableConditionSQL("NOW()")))
	}
	if strings.Contains(dependenciesSQL, "ac.") {
		_, _ = b.WriteString(fmt.Sprintf(`
		LEFT JOIN LATERAL (
			SELECT COUNT(*)::int AS active_seats
			FROM account_share_memberships m
			WHERE m.listing_id = l.id
				AND m.status IN ('%s', '%s')
				AND m.deleted_at IS NULL
				AND m.consumer_user_id <> l.owner_user_id
		) ac ON TRUE`,
			service.AccountShareMembershipStatusActive,
			service.AccountShareMembershipStatusEnding,
		))
	}
	return b.String()
}

func accountShareListingSelectSQLWithAccountJoin(accountJoinSQL string) string {
	return accountShareListingSelectSQLWithSourceAndCurrentMembershipJoin(
		"account_share_listings",
		accountJoinSQL,
		accountShareViewerCurrentMembershipFullLateralSQL(),
	)
}

func accountShareListingSelectSQLWithSourceAndCurrentMembershipJoin(listingSource, accountJoinSQL, currentMembershipJoinSQL string) string {
	return fmt.Sprintf(`
		SELECT
			l.id,
			l.row_version,
			l.current_revision_id,
			(l.deleted_at IS NOT NULL),
			COALESCE(a.id, 0),
			l.room_name,
			COALESCE(room_stats.account_count, 0),
			COALESCE(room_stats.healthy_account_count, 0),
			l.owner_user_id,
			COALESCE(u.username, ''),
			COALESCE(a.name, ''),
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
			COALESCE(room_stats.total_concurrency, a.concurrency, 0),
			COALESCE(a.concurrency, 0),
			COALESCE(a.auto_pause_on_expired, FALSE),
			l.hourly_rate,
			l.hourly_fee_waiver_minimum,
			l.min_balance_required,
			l.codex_cli_only,
			l.codex_5h_limit_percent,
			l.codex_7d_limit_percent,
			COALESCE(NULLIF(a.platform, ''), l.platform),
			COALESCE(a.type, ''),
			COALESCE(NULLIF(a.account_level, ''), l.account_level),
			CASE WHEN a.id IS NULL OR a.deleted_at IS NOT NULL THEN '%s' ELSE a.status END,
			COALESCE(a.schedulable AND a.deleted_at IS NULL, FALSE),
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
			qm.ending_operation_id,
			qm.ending_operation_status,
			qm.settlement_status,
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
		FROM %s l
		%s
		LEFT JOIN users u ON u.id = l.owner_user_id
		LEFT JOIN users eu ON eu.id = l.editing_by_user_id AND l.editing_expires_at > NOW()
		LEFT JOIN LATERAL (
			SELECT
				COUNT(*)::int AS account_count,
				COUNT(*) FILTER (WHERE NOT %s)::int AS healthy_account_count,
				COALESCE(SUM(a.concurrency) FILTER (WHERE NOT %s), 0)::int AS total_concurrency
			FROM account_share_room_accounts room_account
			JOIN accounts a ON a.id = room_account.account_id
			WHERE room_account.listing_id = l.id
				AND room_account.state = 'active'
				AND a.deleted_at IS NULL
		) room_stats ON TRUE
		LEFT JOIN LATERAL (
			SELECT COUNT(*)::int AS active_seats
			FROM account_share_memberships m
			WHERE m.listing_id = l.id
				AND m.status IN ('%s', '%s')
				AND m.deleted_at IS NULL
				AND m.consumer_user_id <> l.owner_user_id
		) ac ON TRUE
		%s
		LEFT JOIN LATERAL (
			SELECT
				m.id,
				m.api_key_id,
				COALESCE(ak.name, '') AS api_key_name,
				m.queue_rank,
				m.status,
				COALESCE(m.ending_operation_id::text, '') AS ending_operation_id,
				COALESCE(operation.status, '') AS ending_operation_status,
				COALESCE(m.settlement_status, '') AS settlement_status,
				m.idle_timeout_minutes,
				m.dispatch_cooldown_until
			FROM account_share_memberships m
			LEFT JOIN api_keys ak ON ak.id = m.api_key_id
			LEFT JOIN account_share_room_operations operation
				ON operation.id = m.ending_operation_id
				AND operation.action = 'end_membership'
				AND operation.membership_id = m.id
			WHERE m.listing_id = l.id
				AND m.consumer_user_id = $1
				AND m.status IN ('%s', '%s', '%s')
				AND m.deleted_at IS NULL
			ORDER BY
				CASE m.status
					WHEN '%s' THEN 0
					WHEN '%s' THEN 1
					ELSE 2
				END,
				m.queue_rank ASC,
				m.id DESC
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
	`,
		service.StatusDisabled,
		listingSource,
		accountJoinSQL,
		accountShareAccountUnavailableConditionSQL("NOW()"),
		accountShareAccountUnavailableConditionSQL("NOW()"),
		service.AccountShareMembershipStatusActive,
		service.AccountShareMembershipStatusEnding,
		currentMembershipJoinSQL,
		service.AccountShareMembershipStatusActive,
		service.AccountShareMembershipStatusQueued,
		service.AccountShareMembershipStatusEnding,
		service.AccountShareMembershipStatusActive,
		service.AccountShareMembershipStatusEnding,
		service.AccountShareMembershipStatusEnded,
	)
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
			COALESCE(r.account_identity_id, 0),
			COALESCE(r.listing_id, history_membership.listing_id, 0),
			COALESCE(
				r.account_id,
				history_binding.account_id_snapshot,
				history_membership.account_id,
				0
			),
			r.membership_id,
			r.owner_user_id,
			COALESCE(
				NULLIF(history_membership.owner_username_snapshot, ''),
				NULLIF(history_revision.owner_display_name_snapshot, ''),
				''
			),
			r.consumer_user_id,
			COALESCE(cu.username, ''),
			COALESCE(
				NULLIF(history_binding.account_name_snapshot, ''),
				''
			),
			COALESCE(
				NULLIF(history_membership.platform_snapshot, ''),
				NULLIF(history_binding.platform_snapshot, ''),
				NULLIF(history_revision.platform, ''),
				NULLIF(i.platform, ''),
				''
			),
			r.score,
			r.comment,
			r.comment_status,
			r.comment_reject_reason,
			r.created_at,
			r.updated_at
		FROM account_share_reviews r
		LEFT JOIN account_share_account_identities i ON i.id = r.account_identity_id
		LEFT JOIN account_share_memberships history_membership
			ON history_membership.id = r.membership_id
		LEFT JOIN account_share_listing_revisions history_revision
			ON history_revision.id = history_membership.listing_revision_id
			AND history_revision.listing_id = history_membership.listing_id
		LEFT JOIN LATERAL (
			SELECT
				binding.account_id,
				binding.account_id_snapshot,
				binding.account_name_snapshot,
				binding.platform_snapshot
			FROM account_share_membership_account_bindings binding
			WHERE binding.membership_id = r.membership_id
				AND binding.listing_id = history_membership.listing_id
			ORDER BY binding.routing_generation DESC, binding.id DESC
			LIMIT 1
		) history_binding ON TRUE
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

func refreshAccountShareListingRatingsInTx(ctx context.Context, tx *sql.Tx, listingID int64) error {
	if tx == nil || listingID <= 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE account_share_listings l
		SET rating_count = COALESCE((
				SELECT COUNT(*)::int
				FROM account_share_reviews r
				WHERE r.listing_id = $1
					AND r.deleted_at IS NULL
			), 0),
			rating_score_sum = COALESCE((
				SELECT SUM(r.score)::int
				FROM account_share_reviews r
				WHERE r.listing_id = $1
					AND r.deleted_at IS NULL
			), 0),
			rating_avg = COALESCE((
				SELECT ROUND(AVG(r.score)::numeric, 2)
				FROM account_share_reviews r
				WHERE r.listing_id = $1
					AND r.deleted_at IS NULL
			), 0)
		WHERE l.id = $1
	`, listingID)
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
	var accountID sql.NullInt64
	err := scanner.Scan(
		&membership.ID,
		&membership.ListingID,
		&accountID,
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
	if accountID.Valid {
		membership.AccountID = accountID.Int64
	} else if membership.Status == service.AccountShareMembershipStatusActive ||
		membership.Status == service.AccountShareMembershipStatusEnding {
		return nil, fmt.Errorf(
			"account share membership %d in status %q has no account binding",
			membership.ID,
			membership.Status,
		)
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
	var currentRevisionID, proxyID, accountIdentityID, currentMembershipID, currentConsumerUserID, currentAPIKeyID, currentIdleTimeoutMinutes, queueMembershipID, queueAPIKeyID, queueRank, queueIdleTimeoutMinutes, lastUsedMembershipID, editingByUserID sql.NullInt64
	var currentJoinedAt, currentPaidUntil, currentBilledUntil, currentLastRequestAt, currentWaiverWindowStartedAt, currentWaiverWindowLastRequestAt, queueDispatchCooldownUntil, lastUsedAt, editingExpiresAt sql.NullTime
	var accountPlatform, accountType, accountLevel, accountStatus string
	var accountSchedulable bool
	var accountExpiresAt, accountLastUsedAt, rateLimitedAt, rateLimitResetAt, overloadUntil, tempUnschedulableUntil, sessionWindowStart, sessionWindowEnd sql.NullTime
	var tempUnschedulableReason, sessionWindowStatus, subscriptionExpiresAtRaw, currentAPIKeyName, queueAPIKeyName, queueStatus, queueEndingOperationID, queueEndingOperationStatus, queueSettlementStatus sql.NullString
	var editingByUsername, editSessionID string
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
	listing.CurrentRevisionID = sqlNullInt64Ptr(currentRevisionID)
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
	listing.EditingByUserID = sqlNullInt64Ptr(editingByUserID)
	listing.EditingByUsername = editingByUsername
	listing.EditingExpiresAt = sqlNullTimePtr(editingExpiresAt)
	listing.EditSessionID = editSessionID
	listing.AccountSampleScope = service.AccountShareAccountSampleScopeRepresentative
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

func lockAccountShareJoinAPIKeyInTx(
	ctx context.Context,
	tx *sql.Tx,
	apiKeyID int64,
	consumerUserID int64,
) (string, error) {
	if tx == nil || apiKeyID <= 0 || consumerUserID <= 0 {
		return "", service.ErrAPIKeyNotFound
	}
	var apiKeyName string
	err := tx.QueryRowContext(ctx, `
		SELECT name
		FROM api_keys
		WHERE id = $1
			AND user_id = $2
			AND deleted_at IS NULL
		FOR UPDATE
	`, apiKeyID, consumerUserID).Scan(&apiKeyName)
	if errors.Is(err, sql.ErrNoRows) {
		return "", service.ErrAPIKeyNotFound
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(apiKeyName), nil
}

func liveAccountShareSeatCountInTx(ctx context.Context, tx *sql.Tx, listingID int64) (int, error) {
	var activeSeats int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)::int
		FROM account_share_memberships m
		JOIN account_share_listings l ON l.id = m.listing_id
			AND l.deleted_at IS NULL
		WHERE m.listing_id = $1
			AND m.status IN ($2, $3)
			AND m.deleted_at IS NULL
			AND m.consumer_user_id <> l.owner_user_id
	`,
		listingID,
		service.AccountShareMembershipStatusActive,
		service.AccountShareMembershipStatusEnding,
	).Scan(&activeSeats); err != nil {
		return 0, err
	}
	return activeSeats, nil
}

func accountShareJoinQueueCapacityError(
	apiKeyQueueCount int,
	consumerQueueCount int,
	roomQueueCount int,
	seatLimit int,
) error {
	if apiKeyQueueCount >= service.AccountShareModeQueueMaxItems {
		return service.ErrAccountShareQueueFull.WithMetadata(map[string]string{
			"scope": "api_key",
			"limit": strconv.Itoa(service.AccountShareModeQueueMaxItems),
			"used":  strconv.Itoa(apiKeyQueueCount),
		})
	}
	if consumerQueueCount >= service.AccountShareModeQueueMaxItems {
		return service.ErrAccountShareQueueFull.WithMetadata(map[string]string{
			"scope": "consumer",
			"limit": strconv.Itoa(service.AccountShareModeQueueMaxItems),
			"used":  strconv.Itoa(consumerQueueCount),
		})
	}
	roomQueueLimit := service.AccountShareRoomQueueLimit(seatLimit)
	if roomQueueCount >= roomQueueLimit {
		return service.ErrAccountShareRoomQueueLimitExceeded.WithMetadata(map[string]string{
			"scope": "room",
			"limit": strconv.Itoa(roomQueueLimit),
			"used":  strconv.Itoa(roomQueueCount),
		})
	}
	return nil
}

func endStaleQueuedMembershipsForAPIKeyInTx(
	ctx context.Context,
	tx *sql.Tx,
	consumerUserID, apiKeyID int64,
	endedAt time.Time,
	deferredQueueBindingEnabled ...bool,
) (int64, error) {
	if consumerUserID <= 0 || apiKeyID <= 0 {
		return 0, nil
	}
	return endStaleQueuedMembershipsInTx(
		ctx,
		tx,
		consumerUserID,
		&apiKeyID,
		endedAt,
		deferredQueueBindingEnabled...,
	)
}

func endStaleQueuedMembershipsForConsumerInTx(
	ctx context.Context,
	tx *sql.Tx,
	consumerUserID int64,
	endedAt time.Time,
	deferredQueueBindingEnabled ...bool,
) (int64, error) {
	if consumerUserID <= 0 {
		return 0, nil
	}
	return endStaleQueuedMembershipsInTx(
		ctx,
		tx,
		consumerUserID,
		nil,
		endedAt,
		deferredQueueBindingEnabled...,
	)
}

func endStaleQueuedMembershipsInTx(
	ctx context.Context,
	tx *sql.Tx,
	consumerUserID int64,
	apiKeyID *int64,
	endedAt time.Time,
	deferredQueueBindingEnabled ...bool,
) (int64, error) {
	if tx == nil || consumerUserID <= 0 {
		return 0, nil
	}
	endedAt = endedAt.UTC()
	clearAccountID := len(deferredQueueBindingEnabled) > 0 && deferredQueueBindingEnabled[0]
	result, err := tx.ExecContext(ctx, `
		UPDATE account_share_memberships m
		SET status = $1,
			account_id = CASE WHEN $10::boolean THEN NULL ELSE m.account_id END,
			ended_at = $2,
			ended_reason = CASE
				WHEN m.queue_expires_at <= $2 THEN $3
				ELSE $4
			END,
			paid_until = NULL,
			billed_until = NULL,
			waiver_window_started_at = NULL,
			waiver_window_usage_amount = 0,
			waiver_window_request_count = 0,
			waiver_window_last_request_at = NULL,
			dispatch_failed_at = NULL,
			dispatch_cooldown_until = NULL,
			settlement_status = 'not_required',
			updated_at = NOW()
		WHERE m.consumer_user_id = $5
			AND ($6::bigint IS NULL OR m.api_key_id = $6)
			AND m.status = $7
			AND m.deleted_at IS NULL
			AND (
				m.queue_expires_at <= $2
				OR EXISTS (
					SELECT 1
					FROM account_share_listings l
					WHERE l.id = m.listing_id
						AND (
							l.deleted_at IS NOT NULL
							OR l.status IN ($8, $9, 'draining')
						)
				)
			)
	`,
		service.AccountShareMembershipStatusEnded,
		endedAt,
		service.AccountShareMembershipEndReasonQueueExpired,
		service.AccountShareMembershipEndReasonUnavailable,
		consumerUserID,
		apiKeyID,
		service.AccountShareMembershipStatusQueued,
		service.AccountShareListingStatusDisabled,
		service.AccountShareListingStatusSuspended,
		clearAccountID,
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
		case "uq_account_share_memberships_live_consumer",
			"uq_as_memberships_live_consumer_rebuild_guard":
			return service.ErrAccountShareAlreadyUsing.WithCause(err)
		case "uq_account_share_memberships_live_api_key",
			"uq_as_memberships_live_api_key_rebuild_guard":
			return service.ErrAccountShareAPIKeyAlreadyBound.WithCause(err)
		case "uq_account_share_memberships_live_listing_consumer",
			"uq_as_memberships_live_listing_consumer_rebuild_guard":
			return service.ErrAccountShareMembershipEnding.WithCause(err)
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
