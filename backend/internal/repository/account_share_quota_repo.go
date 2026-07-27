package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.AccountShareQuotaAdminRepository = (*accountShareModeRepository)(nil)

const accountShareQuotaPolicyColumns = `
	policy.id,
	policy.scope_type,
	policy.owner_user_id,
	policy.version,
	policy.status,
	policy.override_kind,
	policy.max_live_rooms,
	policy.max_room_creates_24_hours,
	policy.max_accounts_per_room,
	policy.max_room_accounts_per_owner,
	policy.effective_at,
	policy.expires_at,
	policy.reason,
	policy.actor_user_id,
	policy.actor_user_id_snapshot,
	policy.created_at`

type accountShareQuotaQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (r *accountShareModeRepository) ResolveAccountShareQuota(
	ctx context.Context,
	ownerUserID int64,
	at time.Time,
) (*service.AccountShareResolvedQuota, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrAccountShareQuotaConfigurationUnavailable
	}
	return resolveAccountShareQuotaWithQueryer(ctx, r.db, ownerUserID, at)
}

func (r *accountShareModeRepository) GetLatestAccountShareQuotaPolicy(
	ctx context.Context,
	scopeType string,
	ownerUserID *int64,
) (*service.AccountShareQuotaPolicy, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrAccountShareQuotaConfigurationUnavailable
	}
	policy, err := getLatestAccountShareQuotaPolicyWithQueryer(
		ctx,
		r.db,
		scopeType,
		ownerUserID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		if scopeType == service.AccountShareQuotaScopeGlobal {
			return nil, service.ErrAccountShareQuotaConfigurationUnavailable
		}
		return nil, nil
	}
	return policy, err
}

func (r *accountShareModeRepository) GetAccountShareQuotaAdminState(
	ctx context.Context,
	ownerUserID int64,
	at time.Time,
) (*service.AccountShareQuotaAdminState, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrAccountShareQuotaConfigurationUnavailable
	}
	if ownerUserID <= 0 {
		return nil, service.ErrAccountShareQuotaInvalid
	}
	globalPolicy, err := r.GetLatestAccountShareQuotaPolicy(
		ctx,
		service.AccountShareQuotaScopeGlobal,
		nil,
	)
	if err != nil {
		return nil, err
	}
	ownerPolicy, err := r.GetLatestAccountShareQuotaPolicy(
		ctx,
		service.AccountShareQuotaScopeOwner,
		&ownerUserID,
	)
	if err != nil {
		return nil, err
	}
	effective, err := r.ResolveAccountShareQuota(ctx, ownerUserID, at)
	if err != nil {
		return nil, err
	}
	usage, err := r.GetAccountShareQuotaUsage(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}
	if globalPolicy == nil || effective == nil || usage == nil {
		return nil, service.ErrAccountShareQuotaConfigurationUnavailable
	}
	effective.GrowthBlocked = service.IsAccountShareQuotaGrowthBlocked(effective, *usage)
	return &service.AccountShareQuotaAdminState{
		GlobalPolicy:   *globalPolicy,
		OwnerPolicy:    ownerPolicy,
		EffectiveQuota: *effective,
		Usage:          *usage,
	}, nil
}

func (r *accountShareModeRepository) AppendAccountShareQuotaPolicyRevision(
	ctx context.Context,
	input service.AppendAccountShareQuotaPolicyInput,
) (*service.AccountShareQuotaPolicy, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrAccountShareQuotaConfigurationUnavailable
	}
	if err := validateAccountShareQuotaPolicyAppendInput(input); err != nil {
		return nil, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if input.ScopeType == service.AccountShareQuotaScopeOwner {
		if input.DeriveGrandfather {
			if err := lockAccountShareGlobalQuotaInTx(ctx, tx); err != nil {
				return nil, err
			}
		}
		if err := lockAccountShareOwnerQuotaInTx(ctx, tx, *input.OwnerUserID); err != nil {
			return nil, err
		}
		var ownerExists bool
		if err := tx.QueryRowContext(
			ctx,
			"SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)",
			*input.OwnerUserID,
		).Scan(&ownerExists); err != nil {
			return nil, err
		}
		if !ownerExists {
			return nil, service.ErrUserNotFound
		}
	} else {
		if err := lockAccountShareGlobalQuotaInTx(ctx, tx); err != nil {
			return nil, err
		}
	}

	latest, latestErr := getLatestAccountShareQuotaPolicyWithQueryer(
		ctx,
		tx,
		input.ScopeType,
		input.OwnerUserID,
	)
	if latestErr != nil && !errors.Is(latestErr, sql.ErrNoRows) {
		return nil, latestErr
	}
	latestVersion := int64(0)
	if latest != nil {
		latestVersion = latest.Version
	}
	if input.ScopeType == service.AccountShareQuotaScopeGlobal && latest == nil {
		return nil, service.ErrAccountShareQuotaConfigurationUnavailable
	}
	if input.ExpectedVersion != latestVersion {
		return nil, service.ErrAccountShareQuotaVersionConflict.WithMetadata(map[string]string{
			"expected_version": fmt.Sprintf("%d", input.ExpectedVersion),
			"current_version":  fmt.Sprintf("%d", latestVersion),
		})
	}

	limits := input.Limits
	if input.DeriveGrandfather {
		effectiveQuota, err := resolveAccountShareQuotaWithQueryer(
			ctx,
			tx,
			*input.OwnerUserID,
			time.Now().UTC(),
		)
		if err != nil {
			return nil, err
		}
		usage, err := getAccountShareQuotaUsageWithQueryer(
			ctx,
			tx,
			*input.OwnerUserID,
		)
		if err != nil {
			return nil, err
		}
		if effectiveQuota == nil {
			return nil, service.ErrAccountShareQuotaConfigurationUnavailable
		}
		switch classifyAccountShareGrandfatherEligibility(effectiveQuota, *usage) {
		case accountShareGrandfatherAlreadyActive:
			return nil, service.ErrAccountShareQuotaGrandfatherAlreadyActive
		case accountShareGrandfatherNotCandidate:
			return nil, service.ErrAccountShareQuotaNotCandidate
		}
		limits = grandfatherAccountShareQuotaLimits(effectiveQuota.Limits, *usage)
	}
	if !limits.Valid() {
		return nil, service.ErrAccountShareQuotaInvalid
	}

	row := tx.QueryRowContext(ctx, `
		INSERT INTO account_share_quota_policies AS policy (
			scope_type,
			owner_user_id,
			version,
			status,
			override_kind,
			max_live_rooms,
			max_room_creates_24_hours,
			max_accounts_per_room,
			max_room_accounts_per_owner,
			effective_at,
			expires_at,
			reason,
			actor_user_id,
			actor_user_id_snapshot
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $13
		)
		RETURNING `+accountShareQuotaPolicyColumns+`
	`,
		input.ScopeType,
		nullablePtrInt64(input.OwnerUserID),
		latestVersion+1,
		input.Status,
		input.OverrideKind,
		limits.MaxLiveRooms,
		limits.MaxRoomCreates24Hours,
		limits.MaxAccountsPerRoom,
		limits.MaxRoomAccountsPerOwner,
		input.EffectiveAt.UTC(),
		accountShareQuotaNullableTime(input.ExpiresAt),
		strings.TrimSpace(input.Reason),
		input.ActorUserID,
	)
	policy, err := scanAccountShareQuotaPolicy(row)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return policy, nil
}

func (r *accountShareModeRepository) ListAccountShareGrandfatherCandidates(
	ctx context.Context,
	at time.Time,
	params pagination.PaginationParams,
) ([]service.AccountShareGrandfatherCandidate, int64, error) {
	if r == nil || r.db == nil {
		return nil, 0, service.ErrAccountShareQuotaConfigurationUnavailable
	}
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	global, err := getEffectiveAccountShareQuotaPolicyWithQueryer(
		ctx, r.db, service.AccountShareQuotaScopeGlobal, nil, at,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, service.ErrAccountShareQuotaConfigurationUnavailable
	}
	if err != nil {
		return nil, 0, err
	}
	if global.Status != service.AccountShareQuotaPolicyStatusActive ||
		global.OverrideKind != service.AccountShareQuotaPolicyKindDefault || !global.Limits.Valid() {
		return nil, 0, service.ErrAccountShareQuotaConfigurationUnavailable
	}

	rows, err := r.db.QueryContext(ctx, `
		WITH room_account_counts AS (
			SELECT listing_id, COUNT(*) FILTER (WHERE state IN ('active', 'draining'))::int AS account_count
			FROM account_share_room_accounts
			GROUP BY listing_id
		), owner_usage AS (
			SELECT
				listing.owner_user_id,
				COUNT(DISTINCT listing.id) FILTER (WHERE listing.deleted_at IS NULL)::int AS live_rooms,
				COUNT(DISTINCT listing.id) FILTER (WHERE listing.created_at >= NOW() - INTERVAL '24 hours')::int AS room_creates_24_hours,
				COALESCE(SUM(COALESCE(room_accounts.account_count, 0)) FILTER (WHERE listing.deleted_at IS NULL), 0)::int AS owner_room_accounts,
				COALESCE(MAX(COALESCE(room_accounts.account_count, 0)) FILTER (WHERE listing.deleted_at IS NULL), 0)::int AS largest_room_accounts
			FROM account_share_listings listing
			LEFT JOIN room_account_counts room_accounts ON room_accounts.listing_id = listing.id
			GROUP BY listing.owner_user_id
		), candidates AS (
			SELECT
				usage.owner_user_id,
				usage.live_rooms,
				usage.room_creates_24_hours,
				usage.owner_room_accounts,
				usage.largest_room_accounts,
				COALESCE(latest.version, 0)::bigint AS latest_owner_version,
				current_policy.id AS policy_id,
				current_policy.version AS policy_version,
				current_policy.status AS policy_status,
				current_policy.override_kind AS policy_kind,
				current_policy.max_live_rooms,
				current_policy.max_room_creates_24_hours,
				current_policy.max_accounts_per_room,
				current_policy.max_room_accounts_per_owner,
				current_policy.expires_at
			FROM owner_usage usage
			LEFT JOIN LATERAL (
				SELECT version
				FROM account_share_quota_policies
				WHERE scope_type = 'owner' AND owner_user_id = usage.owner_user_id
				ORDER BY version DESC, id DESC
				LIMIT 1
			) latest ON TRUE
			LEFT JOIN LATERAL (
				SELECT id, version, status, override_kind, max_live_rooms,
					max_room_creates_24_hours, max_accounts_per_room,
					max_room_accounts_per_owner, expires_at
				FROM account_share_quota_policies
				WHERE scope_type = 'owner'
					AND owner_user_id = usage.owner_user_id
					AND effective_at <= $1
				ORDER BY version DESC, id DESC
				LIMIT 1
			) current_policy ON TRUE
			WHERE (
				current_policy.id IS NULL
				OR NOT (
					current_policy.status = 'active'
					AND current_policy.override_kind = 'grandfather'
					AND current_policy.expires_at > $1
				)
			)
			AND (
				usage.live_rooms > CASE WHEN current_policy.status = 'active' AND current_policy.expires_at > $1 THEN current_policy.max_live_rooms ELSE $2 END
				OR usage.room_creates_24_hours > CASE WHEN current_policy.status = 'active' AND current_policy.expires_at > $1 THEN current_policy.max_room_creates_24_hours ELSE $3 END
				OR usage.largest_room_accounts > CASE WHEN current_policy.status = 'active' AND current_policy.expires_at > $1 THEN current_policy.max_accounts_per_room ELSE $4 END
				OR usage.owner_room_accounts > CASE WHEN current_policy.status = 'active' AND current_policy.expires_at > $1 THEN current_policy.max_room_accounts_per_owner ELSE $5 END
			)
		)
		SELECT
			candidate.owner_user_id,
			candidate.live_rooms,
			candidate.room_creates_24_hours,
			candidate.owner_room_accounts,
			candidate.largest_room_accounts,
			candidate.latest_owner_version,
			candidate.policy_id,
			candidate.policy_version,
			candidate.policy_status,
			candidate.policy_kind,
			candidate.max_live_rooms,
			candidate.max_room_creates_24_hours,
			candidate.max_accounts_per_room,
			candidate.max_room_accounts_per_owner,
			candidate.expires_at,
			totals.total
		FROM (SELECT COUNT(*)::bigint AS total FROM candidates) totals
		LEFT JOIN LATERAL (
			SELECT *
			FROM candidates
			ORDER BY owner_user_id ASC
			OFFSET $6
			LIMIT $7
		) candidate ON TRUE
		ORDER BY candidate.owner_user_id ASC
	`,
		at,
		global.Limits.MaxLiveRooms,
		global.Limits.MaxRoomCreates24Hours,
		global.Limits.MaxAccountsPerRoom,
		global.Limits.MaxRoomAccountsPerOwner,
		params.Offset(),
		params.Limit(),
	)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.AccountShareGrandfatherCandidate, 0, params.Limit())
	var total int64
	for rows.Next() {
		var (
			candidate        service.AccountShareGrandfatherCandidate
			ownerUserID      sql.NullInt64
			liveRooms        sql.NullInt64
			roomCreates      sql.NullInt64
			ownerAccounts    sql.NullInt64
			largestAccounts  sql.NullInt64
			latestVersion    sql.NullInt64
			policyID         sql.NullInt64
			policyVersion    sql.NullInt64
			policyStatus     sql.NullString
			policyKind       sql.NullString
			maxLiveRooms     sql.NullInt64
			maxCreates       sql.NullInt64
			maxPerRoom       sql.NullInt64
			maxOwnerAccounts sql.NullInt64
			expiresAt        sql.NullTime
		)
		if err := rows.Scan(
			&ownerUserID,
			&liveRooms,
			&roomCreates,
			&ownerAccounts,
			&largestAccounts,
			&latestVersion,
			&policyID,
			&policyVersion,
			&policyStatus,
			&policyKind,
			&maxLiveRooms,
			&maxCreates,
			&maxPerRoom,
			&maxOwnerAccounts,
			&expiresAt,
			&total,
		); err != nil {
			return nil, 0, err
		}
		if !ownerUserID.Valid {
			continue
		}
		if !liveRooms.Valid || !roomCreates.Valid || !ownerAccounts.Valid ||
			!largestAccounts.Valid || !latestVersion.Valid {
			return nil, 0, service.ErrAccountShareQuotaConfigurationUnavailable
		}
		candidate.OwnerUserID = ownerUserID.Int64
		candidate.Usage = service.AccountShareQuotaUsage{
			LiveRooms:           int(liveRooms.Int64),
			RoomCreates24Hours:  int(roomCreates.Int64),
			OwnerRoomAccounts:   int(ownerAccounts.Int64),
			LargestRoomAccounts: int(largestAccounts.Int64),
		}
		candidate.LatestOwnerVersion = latestVersion.Int64
		candidate.EffectiveQuota = service.AccountShareResolvedQuota{
			Limits: global.Limits, Source: service.AccountShareQuotaScopeGlobal,
			PolicyID: global.ID, PolicyVersion: global.Version, OverrideKind: global.OverrideKind,
		}
		if policyID.Valid && policyStatus.String == service.AccountShareQuotaPolicyStatusActive &&
			expiresAt.Valid && expiresAt.Time.After(at) {
			candidate.EffectiveQuota = service.AccountShareResolvedQuota{
				Limits: service.AccountShareQuotaLimits{
					MaxLiveRooms:            int(maxLiveRooms.Int64),
					MaxRoomCreates24Hours:   int(maxCreates.Int64),
					MaxAccountsPerRoom:      int(maxPerRoom.Int64),
					MaxRoomAccountsPerOwner: int(maxOwnerAccounts.Int64),
				},
				Source: "owner_override", PolicyID: policyID.Int64,
				PolicyVersion: policyVersion.Int64, OverrideKind: policyKind.String,
				OverrideExpiresAt: &expiresAt.Time,
			}
		}
		candidate.ExceededDimensions = service.AccountShareQuotaExceededDimensions(
			candidate.EffectiveQuota.Limits, candidate.Usage,
		)
		candidate.SuggestedLimits = grandfatherAccountShareQuotaLimits(
			candidate.EffectiveQuota.Limits, candidate.Usage,
		)
		candidate.AsOf = at
		candidate.PreviewFingerprint = service.BuildAccountShareGrandfatherCandidateFingerprint(
			candidate.OwnerUserID, candidate.LatestOwnerVersion, candidate.Usage, candidate.EffectiveQuota,
		)
		items = append(items, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *accountShareModeRepository) ApplyAccountShareGrandfatherCandidate(
	ctx context.Context,
	input service.ApplyAccountShareGrandfatherCandidateInput,
) (*service.AccountShareGrandfatherBatchItemResult, error) {
	result := &service.AccountShareGrandfatherBatchItemResult{OwnerUserID: input.Item.OwnerUserID}
	if r == nil || r.db == nil {
		return nil, service.ErrAccountShareQuotaConfigurationUnavailable
	}
	if input.Item.OwnerUserID <= 0 || input.Item.ExpectedVersion < 0 ||
		!input.Item.PreviewUsage.Valid() || strings.TrimSpace(input.Item.PreviewFingerprint) == "" ||
		input.ActorUserID <= 0 || input.ExpiresAt.IsZero() || !input.ExpiresAt.After(time.Now().UTC()) {
		return nil, service.ErrAccountShareQuotaInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockAccountShareGlobalQuotaInTx(ctx, tx); err != nil {
		return nil, err
	}
	if err := lockAccountShareOwnerQuotaInTx(ctx, tx, input.Item.OwnerUserID); err != nil {
		return nil, err
	}
	var ownerExists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)", input.Item.OwnerUserID).Scan(&ownerExists); err != nil {
		return nil, err
	}
	if !ownerExists {
		result.Status, result.ResultCode, result.Message = "skipped", "OWNER_NOT_FOUND", "owner no longer exists"
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return result, nil
	}
	latest, err := getLatestAccountShareQuotaPolicyWithQueryer(ctx, tx, service.AccountShareQuotaScopeOwner, &input.Item.OwnerUserID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	latestVersion := int64(0)
	if latest != nil {
		latestVersion = latest.Version
	}
	if latestVersion != input.Item.ExpectedVersion {
		result.Status, result.ResultCode, result.Message = "conflict", "ACCOUNT_SHARE_QUOTA_VERSION_CONFLICT", "owner quota policy changed; refresh the candidate preview"
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return result, nil
	}
	at := time.Now().UTC()
	quota, err := resolveAccountShareQuotaWithQueryer(ctx, tx, input.Item.OwnerUserID, at)
	if err != nil {
		return nil, err
	}
	usage, err := getAccountShareQuotaUsageWithQueryer(ctx, tx, input.Item.OwnerUserID)
	if err != nil {
		return nil, err
	}
	if quota == nil || usage == nil {
		return nil, service.ErrAccountShareQuotaConfigurationUnavailable
	}
	switch classifyAccountShareGrandfatherEligibility(quota, *usage) {
	case accountShareGrandfatherAlreadyActive:
		result.Status, result.ResultCode, result.Message = "skipped", service.ErrAccountShareQuotaGrandfatherAlreadyActive.Reason, service.ErrAccountShareQuotaGrandfatherAlreadyActive.Message
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return result, nil
	case accountShareGrandfatherNotCandidate:
		result.Status, result.ResultCode, result.Message = "skipped", service.ErrAccountShareQuotaNotCandidate.Reason, service.ErrAccountShareQuotaNotCandidate.Message
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return result, nil
	}
	fingerprint := service.BuildAccountShareGrandfatherCandidateFingerprint(input.Item.OwnerUserID, latestVersion, *usage, *quota)
	if !sameAccountShareQuotaUsage(input.Item.PreviewUsage, *usage) || input.Item.PreviewFingerprint != fingerprint {
		result.Status, result.ResultCode, result.Message = "conflict", "ACCOUNT_SHARE_QUOTA_CANDIDATE_STALE", "candidate usage or effective quota changed; refresh the preview"
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return result, nil
	}
	limits := grandfatherAccountShareQuotaLimits(quota.Limits, *usage)
	row := tx.QueryRowContext(ctx, `
		INSERT INTO account_share_quota_policies (
			scope_type, owner_user_id, version, status, override_kind,
			max_live_rooms, max_room_creates_24_hours, max_accounts_per_room,
			max_room_accounts_per_owner, effective_at, expires_at, reason,
			actor_user_id, actor_user_id_snapshot
		) VALUES (
			'owner', $1, $2, 'active', 'grandfather',
			$3, $4, $5, $6, $7, $8, $9, $10, $10
		)
		RETURNING `+accountShareQuotaPolicyColumns,
		input.Item.OwnerUserID, latestVersion+1,
		limits.MaxLiveRooms, limits.MaxRoomCreates24Hours, limits.MaxAccountsPerRoom, limits.MaxRoomAccountsPerOwner,
		at, input.ExpiresAt.UTC(), strings.TrimSpace(input.Reason), input.ActorUserID,
	)
	policy, err := scanAccountShareQuotaPolicy(row)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	result.Status = "applied"
	result.PolicyID = policy.ID
	result.PolicyVersion = policy.Version
	result.ExpiresAt = policy.ExpiresAt
	return result, nil
}

func (r *accountShareModeRepository) ListAccountShareQuotaPolicyRevisions(
	ctx context.Context,
	scopeType string,
	ownerUserID *int64,
	params pagination.PaginationParams,
) ([]service.AccountShareQuotaPolicy, int64, error) {
	if r == nil || r.db == nil {
		return nil, 0, service.ErrAccountShareQuotaConfigurationUnavailable
	}
	if scopeType != service.AccountShareQuotaScopeGlobal &&
		scopeType != service.AccountShareQuotaScopeOwner {
		return nil, 0, service.ErrAccountShareQuotaInvalid
	}
	if scopeType == service.AccountShareQuotaScopeOwner &&
		(ownerUserID == nil || *ownerUserID <= 0) {
		return nil, 0, service.ErrAccountShareQuotaInvalid
	}
	var total int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*)::bigint
		FROM account_share_quota_policies
		WHERE scope_type = $1
			AND owner_user_id IS NOT DISTINCT FROM $2::bigint
	`, scopeType, nullablePtrInt64(ownerUserID)).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+accountShareQuotaPolicyColumns+`
		FROM account_share_quota_policies AS policy
		WHERE policy.scope_type = $1
			AND policy.owner_user_id IS NOT DISTINCT FROM $2::bigint
		ORDER BY policy.version DESC, policy.id DESC
		OFFSET $3
		LIMIT $4
	`, scopeType, nullablePtrInt64(ownerUserID), params.Offset(), params.Limit())
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.AccountShareQuotaPolicy, 0, params.Limit())
	for rows.Next() {
		item, scanErr := scanAccountShareQuotaPolicy(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func resolveAccountShareQuotaWithQueryer(
	ctx context.Context,
	queryer accountShareQuotaQueryer,
	ownerUserID int64,
	at time.Time,
) (*service.AccountShareResolvedQuota, error) {
	if queryer == nil || ownerUserID <= 0 {
		return nil, service.ErrAccountShareQuotaInvalid
	}
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	globalPolicy, err := getEffectiveAccountShareQuotaPolicyWithQueryer(
		ctx,
		queryer,
		service.AccountShareQuotaScopeGlobal,
		nil,
		at,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareQuotaConfigurationUnavailable
	}
	if err != nil {
		return nil, err
	}
	if globalPolicy.Status != service.AccountShareQuotaPolicyStatusActive ||
		globalPolicy.OverrideKind != service.AccountShareQuotaPolicyKindDefault ||
		!globalPolicy.Limits.Valid() {
		return nil, service.ErrAccountShareQuotaConfigurationUnavailable
	}

	resolved := &service.AccountShareResolvedQuota{
		Limits:        globalPolicy.Limits,
		Source:        service.AccountShareQuotaScopeGlobal,
		PolicyID:      globalPolicy.ID,
		PolicyVersion: globalPolicy.Version,
		OverrideKind:  globalPolicy.OverrideKind,
	}
	ownerPolicy, ownerErr := getEffectiveAccountShareQuotaPolicyWithQueryer(
		ctx,
		queryer,
		service.AccountShareQuotaScopeOwner,
		&ownerUserID,
		at,
	)
	if errors.Is(ownerErr, sql.ErrNoRows) {
		return resolved, nil
	}
	if ownerErr != nil {
		return nil, ownerErr
	}
	if ownerPolicy.Status != service.AccountShareQuotaPolicyStatusActive ||
		ownerPolicy.ExpiresAt == nil ||
		!ownerPolicy.ExpiresAt.After(at) {
		return resolved, nil
	}
	if ownerPolicy.OverrideKind != service.AccountShareQuotaPolicyKindManual &&
		ownerPolicy.OverrideKind != service.AccountShareQuotaPolicyKindGrandfather {
		return nil, service.ErrAccountShareQuotaConfigurationUnavailable
	}
	if !ownerPolicy.Limits.Valid() {
		return nil, service.ErrAccountShareQuotaConfigurationUnavailable
	}
	resolved.Limits = ownerPolicy.Limits
	resolved.Source = "owner_override"
	resolved.PolicyID = ownerPolicy.ID
	resolved.PolicyVersion = ownerPolicy.Version
	resolved.OverrideKind = ownerPolicy.OverrideKind
	resolved.OverrideExpiresAt = ownerPolicy.ExpiresAt
	resolved.GrowthBlocked = ownerPolicy.OverrideKind == service.AccountShareQuotaPolicyKindGrandfather
	return resolved, nil
}

func getEffectiveAccountShareQuotaPolicyWithQueryer(
	ctx context.Context,
	queryer accountShareQuotaQueryer,
	scopeType string,
	ownerUserID *int64,
	at time.Time,
) (*service.AccountShareQuotaPolicy, error) {
	return scanAccountShareQuotaPolicy(queryer.QueryRowContext(ctx, `
		SELECT `+accountShareQuotaPolicyColumns+`
		FROM account_share_quota_policies AS policy
		WHERE policy.scope_type = $1
			AND policy.owner_user_id IS NOT DISTINCT FROM $2::bigint
			AND policy.effective_at <= $3
		ORDER BY policy.version DESC, policy.id DESC
		LIMIT 1
	`, scopeType, nullablePtrInt64(ownerUserID), at.UTC()))
}

func getLatestAccountShareQuotaPolicyWithQueryer(
	ctx context.Context,
	queryer accountShareQuotaQueryer,
	scopeType string,
	ownerUserID *int64,
) (*service.AccountShareQuotaPolicy, error) {
	return scanAccountShareQuotaPolicy(queryer.QueryRowContext(ctx, `
		SELECT `+accountShareQuotaPolicyColumns+`
		FROM account_share_quota_policies AS policy
		WHERE policy.scope_type = $1
			AND policy.owner_user_id IS NOT DISTINCT FROM $2::bigint
		ORDER BY policy.version DESC, policy.id DESC
		LIMIT 1
	`, scopeType, nullablePtrInt64(ownerUserID)))
}

func scanAccountShareQuotaPolicy(scanner sqlScanner) (*service.AccountShareQuotaPolicy, error) {
	var (
		policy      service.AccountShareQuotaPolicy
		ownerUserID sql.NullInt64
		expiresAt   sql.NullTime
		actorUserID sql.NullInt64
	)
	if err := scanner.Scan(
		&policy.ID,
		&policy.ScopeType,
		&ownerUserID,
		&policy.Version,
		&policy.Status,
		&policy.OverrideKind,
		&policy.Limits.MaxLiveRooms,
		&policy.Limits.MaxRoomCreates24Hours,
		&policy.Limits.MaxAccountsPerRoom,
		&policy.Limits.MaxRoomAccountsPerOwner,
		&policy.EffectiveAt,
		&expiresAt,
		&policy.Reason,
		&actorUserID,
		&policy.ActorUserIDSnapshot,
		&policy.CreatedAt,
	); err != nil {
		return nil, err
	}
	if ownerUserID.Valid {
		policy.OwnerUserID = &ownerUserID.Int64
	}
	if expiresAt.Valid {
		t := expiresAt.Time
		policy.ExpiresAt = &t
	}
	if actorUserID.Valid {
		policy.ActorUserID = &actorUserID.Int64
	}
	return &policy, nil
}

func validateAccountShareQuotaPolicyAppendInput(
	input service.AppendAccountShareQuotaPolicyInput,
) error {
	if input.ActorUserID <= 0 ||
		input.ExpectedVersion < 0 ||
		input.EffectiveAt.IsZero() ||
		strings.TrimSpace(input.Reason) == "" {
		return service.ErrAccountShareQuotaInvalid
	}
	switch input.ScopeType {
	case service.AccountShareQuotaScopeGlobal:
		if input.OwnerUserID != nil ||
			input.Status != service.AccountShareQuotaPolicyStatusActive ||
			input.OverrideKind != service.AccountShareQuotaPolicyKindDefault ||
			input.ExpiresAt != nil ||
			input.DeriveGrandfather ||
			!input.Limits.Valid() {
			return service.ErrAccountShareQuotaInvalid
		}
	case service.AccountShareQuotaScopeOwner:
		if input.OwnerUserID == nil || *input.OwnerUserID <= 0 {
			return service.ErrAccountShareQuotaInvalid
		}
		if input.OverrideKind != service.AccountShareQuotaPolicyKindManual &&
			input.OverrideKind != service.AccountShareQuotaPolicyKindGrandfather {
			return service.ErrAccountShareQuotaInvalid
		}
		switch input.Status {
		case service.AccountShareQuotaPolicyStatusActive:
			if input.ExpiresAt == nil || !input.ExpiresAt.After(input.EffectiveAt) {
				return service.ErrAccountShareQuotaInvalid
			}
			if input.OverrideKind == service.AccountShareQuotaPolicyKindGrandfather {
				if !input.DeriveGrandfather {
					return service.ErrAccountShareQuotaInvalid
				}
			} else if input.DeriveGrandfather || !input.Limits.Valid() {
				return service.ErrAccountShareQuotaInvalid
			}
		case service.AccountShareQuotaPolicyStatusRevoked:
			if input.ExpiresAt != nil || input.DeriveGrandfather || !input.Limits.Valid() {
				return service.ErrAccountShareQuotaInvalid
			}
		default:
			return service.ErrAccountShareQuotaInvalid
		}
	default:
		return service.ErrAccountShareQuotaInvalid
	}
	return nil
}

type accountShareGrandfatherEligibility uint8

const (
	accountShareGrandfatherEligible accountShareGrandfatherEligibility = iota
	accountShareGrandfatherAlreadyActive
	accountShareGrandfatherNotCandidate
)

func classifyAccountShareGrandfatherEligibility(
	quota *service.AccountShareResolvedQuota,
	usage service.AccountShareQuotaUsage,
) accountShareGrandfatherEligibility {
	if quota != nil &&
		quota.GrowthBlocked &&
		quota.OverrideKind == service.AccountShareQuotaPolicyKindGrandfather {
		return accountShareGrandfatherAlreadyActive
	}
	if quota == nil || len(service.AccountShareQuotaExceededDimensions(quota.Limits, usage)) == 0 {
		return accountShareGrandfatherNotCandidate
	}
	return accountShareGrandfatherEligible
}

func lockAccountShareGlobalQuotaInTx(ctx context.Context, tx *sql.Tx) error {
	if tx == nil {
		return service.ErrAccountShareQuotaConfigurationUnavailable
	}
	_, err := tx.ExecContext(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtext($1)::bigint)",
		"account_share_quota_policy:global",
	)
	return err
}

func grandfatherAccountShareQuotaLimits(
	global service.AccountShareQuotaLimits,
	usage service.AccountShareQuotaUsage,
) service.AccountShareQuotaLimits {
	limits := global
	limits.MaxLiveRooms = maxInt(limits.MaxLiveRooms, usage.LiveRooms)
	limits.MaxRoomCreates24Hours = maxInt(
		limits.MaxRoomCreates24Hours,
		usage.RoomCreates24Hours,
	)
	limits.MaxAccountsPerRoom = maxInt(
		limits.MaxAccountsPerRoom,
		usage.LargestRoomAccounts,
	)
	limits.MaxRoomAccountsPerOwner = maxInt(
		limits.MaxRoomAccountsPerOwner,
		usage.OwnerRoomAccounts,
	)
	limits.MaxRoomAccountsPerOwner = maxInt(
		limits.MaxRoomAccountsPerOwner,
		limits.MaxAccountsPerRoom,
	)
	return limits
}

func sameAccountShareQuotaUsage(left, right service.AccountShareQuotaUsage) bool {
	return left.LiveRooms == right.LiveRooms &&
		left.RoomCreates24Hours == right.RoomCreates24Hours &&
		left.OwnerRoomAccounts == right.OwnerRoomAccounts &&
		left.LargestRoomAccounts == right.LargestRoomAccounts
}

func accountShareQuotaNullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
