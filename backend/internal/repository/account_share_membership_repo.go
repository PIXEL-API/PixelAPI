package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

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
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		reason = service.AccountShareMembershipEndReasonManual
	}
	if reason != service.AccountShareMembershipEndReasonManual && reason != service.AccountShareMembershipEndReasonUnavailable {
		return nil, nil, service.ErrAccountShareEndStateConflict
	}
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
	if membership.Status != service.AccountShareMembershipStatusActive {
		return nil, nil, service.ErrAccountShareEndStateConflict
	}

	now := time.Now().UTC()

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
		reason,
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
	membership.EndingReason = reason
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

	openBindings, err := lockAccountShareEndRuntimeRowsInTx(ctx, tx, membership.ID)
	if err != nil {
		return nil, nil, false, err
	}
	if openBindings > 1 {
		return nil, nil, false, fmt.Errorf("membership %d has %d open account-share bindings", membership.ID, openBindings)
	}

	if err := lockAccountShareEndBillingUsersInTx(ctx, tx, membership); err != nil {
		return nil, nil, false, err
	}
	endedAt := membership.EndingRequestedAt.UTC()
	endingReason := strings.TrimSpace(membership.EndingReason)
	if endingReason == "" {
		return nil, nil, false, service.ErrAccountShareEndStateConflict
	}
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
		endingReason,
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
	membership.EndingReason = endingReason
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
	afterID int64,
	limit int,
) ([]service.AccountShareEndingMembershipCandidate, error) {
	if r == nil || r.db == nil {
		return nil, service.ErrServiceUnavailable
	}
	if limit <= 0 {
		limit = service.AccountShareModeSeatBillingBatchSize
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT m.id, m.ending_operation_id::text, m.ending_requested_at
		FROM account_share_memberships m
		JOIN account_share_room_operations operation
			ON operation.id = m.ending_operation_id
			AND operation.action = 'end_membership'
			AND operation.membership_id = m.id
			AND operation.status IN ('pending', 'running', 'needs_attention')
		WHERE m.status = $1
			AND m.ending_operation_id IS NOT NULL
			AND m.deleted_at IS NULL
		AND m.id > $3
		ORDER BY m.id ASC
		LIMIT $2
	`, service.AccountShareMembershipStatusEnding, limit, afterID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	candidates := make([]service.AccountShareEndingMembershipCandidate, 0, limit)
	for rows.Next() {
		var candidate service.AccountShareEndingMembershipCandidate
		var endingRequestedAt time.Time
		if err := rows.Scan(&candidate.MembershipID, &candidate.OperationID, &endingRequestedAt); err != nil {
			return nil, err
		}
		candidate.EndingRequestedAt = endingRequestedAt.UTC()
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
			$4,
			CASE WHEN $4::bigint IS NULL THEN 'system' ELSE 'consumer' END,
			CASE WHEN $4::bigint IS NULL THEN 'automatic_exit' ELSE 'consumer_request' END,
			$1,
			$5::bigint, $5::bigint,
			CASE
				WHEN $6::varchar(20) = 'succeeded'::varchar(20) THEN $5::bigint
				ELSE NULL::bigint
			END,
			$6::varchar(20), '{}'::jsonb, $7::jsonb, $8::timestamptz, $9::timestamptz, $9::timestamptz
		)
	`, operationID, listingID, membershipID, nullablePositiveInt64(consumerUserID), listingVersion, status, string(resultPayload), completedAt, now.UTC())
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
) (int, error) {
	bindingRows, err := tx.QueryContext(ctx, `
		SELECT id
		FROM account_share_membership_account_bindings
		WHERE membership_id = $1
			AND unbound_at IS NULL
		ORDER BY id ASC
		FOR UPDATE
	`, membershipID)
	if err != nil {
		return 0, err
	}
	openBindings := 0
	for bindingRows.Next() {
		var id int64
		if err := bindingRows.Scan(&id); err != nil {
			_ = bindingRows.Close()
			return 0, err
		}
		openBindings++
	}
	if err := bindingRows.Err(); err != nil {
		_ = bindingRows.Close()
		return 0, err
	}
	if err := bindingRows.Close(); err != nil {
		return 0, err
	}

	return openBindings, nil
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
			AND m.status = $4
			AND m.deleted_at IS NULL
			AND l.id = m.listing_id
		RETURNING
			m.id, m.listing_id, m.account_id, l.owner_user_id, m.consumer_user_id, m.api_key_id,
			m.status, m.queue_rank, m.hourly_rate_snapshot, m.hourly_fee_waiver_minimum_snapshot, m.idle_timeout_minutes,
			m.joined_at, m.last_request_at, m.ended_at, m.ended_reason, m.paid_until, m.billed_until,
			m.waiver_window_started_at, m.waiver_window_usage_amount, m.waiver_window_request_count, m.waiver_window_last_request_at,
			m.dispatch_failed_at, m.dispatch_cooldown_until, m.created_at, m.updated_at
	`, idleTimeoutMinutes, membershipID, consumerUserID, service.AccountShareMembershipStatusActive))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAccountShareListingNotFound
	}
	if err != nil {
		return nil, err
	}
	return membership, nil
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
			AND m.status IN ($3, $4)
			AND m.deleted_at IS NULL
		ORDER BY m.queue_rank ASC, m.id ASC
	`,
		consumerUserID,
		apiKeyID,
		service.AccountShareMembershipStatusActive,
		service.AccountShareMembershipStatusEnding,
	)
	if err != nil {
		return nil, err
	}

	memberships := make([]service.AccountShareMembership, 0, 1)
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

	if _, _, err := lockAccountShareEndListingInTx(ctx, tx, membershipID, 0); err != nil {
		return nil, nil, err
	}
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
	membership, err = r.beginMembershipEndInTx(ctx, tx, membership, deadline, service.AccountShareMembershipEndReasonIdleTimeout)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	tx = nil
	return membership, &service.AccountShareSeatBillingResult{EndedConsumerUserIDs: []int64{membership.ConsumerUserID}}, nil
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

func (r *accountShareModeRepository) GetMembershipRequestState(ctx context.Context, userID, apiKeyID, groupID int64, now time.Time) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	return accountShareMembershipRequestStateErrorInTx(ctx, tx, userID, apiKeyID, groupID, now)
}

func accountShareMembershipRequestStateErrorInTx(ctx context.Context, tx *sql.Tx, userID, apiKeyID, groupID int64, now time.Time) error {
	if tx == nil {
		return service.ErrAccountShareListingNotFound
	}
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

	var endedReason sql.NullString
	err := tx.QueryRowContext(ctx, `
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

func (r *accountShareModeRepository) beginMembershipEndInTx(ctx context.Context, tx *sql.Tx, membership *service.AccountShareMembership, endingAt time.Time, reason string) (*service.AccountShareMembership, error) {
	if membership == nil || membership.Status != service.AccountShareMembershipStatusActive {
		return nil, service.ErrAccountShareEndStateConflict
	}
	endingAt = endingAt.UTC()
	if endingAt.Before(membership.JoinedAt) {
		endingAt = membership.JoinedAt.UTC()
	}
	var listingVersion int64
	if err := tx.QueryRowContext(ctx, "SELECT row_version FROM account_share_listings WHERE id = $1", membership.ListingID).Scan(&listingVersion); err != nil {
		return nil, err
	}
	operationID := uuid.NewString()
	if err := insertAccountShareEndOperationInTx(ctx, tx, operationID, membership.ListingID, membership.ID, 0, listingVersion, "pending", nil, endingAt); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE account_share_memberships
		SET status = 'ending', ending_requested_at = $2, ending_reason = $3,
			ending_operation_id = $4::uuid, settlement_status = 'pending',
			dispatch_failed_at = CASE WHEN $3 = 'account_unavailable' THEN $2 ELSE dispatch_failed_at END,
			dispatch_cooldown_until = NULL, updated_at = NOW()
		WHERE id = $1 AND status = 'active' AND deleted_at IS NULL
	`, membership.ID, endingAt, reason, operationID)
	if err != nil {
		return nil, err
	}
	if count, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if count != 1 {
		return nil, service.ErrAccountShareEndStateConflict
	}
	membership.Status = service.AccountShareMembershipStatusEnding
	membership.EndingRequestedAt = &endingAt
	membership.EndingReason = reason
	membership.EndingOperationID = operationID
	membership.SettlementStatus = "pending"
	return membership, nil
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

type accountShareMembershipScanner interface {
	Scan(dest ...any) error
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
