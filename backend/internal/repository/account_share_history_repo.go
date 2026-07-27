package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.AccountShareHistoryRepository = (*accountShareModeRepository)(nil)

// ListMembershipHistory returns one immutable record per ended membership.
// The query is deliberately rooted at memberships rather than the live listing
// projection so repeated stays in the same room are not collapsed and deleted
// rooms or detached accounts remain readable.
func (r *accountShareModeRepository) ListMembershipHistory(
	ctx context.Context,
	consumerUserID int64,
	params pagination.PaginationParams,
) ([]service.AccountShareMembershipHistoryEntry, *pagination.PaginationResult, error) {
	if r == nil || r.db == nil {
		return nil, nil, service.ErrServiceUnavailable
	}
	if consumerUserID <= 0 {
		return nil, nil, service.ErrUserNotFound
	}
	page := params.Page
	if page < 1 {
		page = 1
	}
	limit := params.Limit()
	offset := (page - 1) * limit

	var total int64
	if err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM account_share_memberships membership
		WHERE membership.consumer_user_id = $1
			AND membership.status = $2
			AND membership.deleted_at IS NULL
	`, consumerUserID, service.AccountShareMembershipStatusEnded).Scan(&total); err != nil {
		return nil, nil, err
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT
			membership.id,
			membership.listing_id,
			membership.listing_revision_id,
			membership.listing_version_snapshot,
			COALESCE(
				NULLIF(membership.room_name_snapshot, ''),
				NULLIF(revision.room_name, ''),
				''
			),
			(listing.deleted_at IS NOT NULL),
			listing.deleted_at,
			COALESCE(membership.owner_user_id_snapshot, revision.owner_user_id, 0),
			COALESCE(
				NULLIF(membership.owner_username_snapshot, ''),
				NULLIF(revision.owner_display_name_snapshot, ''),
				''
			),
			COALESCE(
				NULLIF(membership.platform_snapshot, ''),
				NULLIF(history_binding.platform_snapshot, ''),
				NULLIF(revision.platform, ''),
				''
			),
			COALESCE(
				NULLIF(membership.account_level_snapshot, ''),
				NULLIF(history_binding.account_level_snapshot, ''),
				NULLIF(revision.account_level, ''),
				''
			),
			COALESCE(history_binding.account_id_snapshot, membership.account_id, 0),
			COALESCE(NULLIF(history_binding.account_name_snapshot, ''), ''),
			COALESCE(history_binding.configured_concurrency_snapshot, 0),
			membership.api_key_id,
			COALESCE(NULLIF(membership.api_key_name_snapshot, ''), ''),
			membership.status,
			membership.joined_at,
			membership.last_request_at,
			membership.ended_at,
			COALESCE(membership.ended_reason, ''),
			membership.paid_until,
			membership.billed_until,
			membership.hourly_rate_snapshot::double precision,
			membership.hourly_fee_waiver_minimum_snapshot::double precision,
			membership.idle_timeout_minutes,
			COALESCE(spend.usage_request_count, 0),
			COALESCE(spend.usage_request_cost, 0),
			membership.terms_snapshot,
			COALESCE(NULLIF(membership.snapshot_quality, ''), NULLIF(revision.snapshot_quality, ''), ''),
			review.id,
			review.score,
			COALESCE(review.comment, ''),
			COALESCE(review.comment_status, ''),
			COALESCE(review.comment_reject_reason, ''),
			review.created_at
		FROM account_share_memberships membership
		LEFT JOIN account_share_listing_revisions revision
			ON revision.id = membership.listing_revision_id
			AND revision.listing_id = membership.listing_id
		LEFT JOIN account_share_listings listing ON listing.id = membership.listing_id
		LEFT JOIN LATERAL (
			SELECT
				binding.account_id,
				binding.account_id_snapshot,
				binding.account_name_snapshot,
				binding.platform_snapshot,
				binding.account_level_snapshot,
				binding.configured_concurrency_snapshot
			FROM account_share_membership_account_bindings binding
			WHERE binding.membership_id = membership.id
				AND binding.listing_id = membership.listing_id
			ORDER BY binding.routing_generation DESC, binding.id DESC
			LIMIT 1
		) history_binding ON TRUE
		LEFT JOIN LATERAL (
			SELECT
				COUNT(*)::bigint AS usage_request_count,
				COALESCE(
					SUM((intent.usage_payload ->> 'actual_cost')::numeric),
					0
				)::double precision AS usage_request_cost
			FROM account_share_request_billing_intents intent
			WHERE intent.membership_id = membership.id
				AND intent.listing_id = membership.listing_id
				AND intent.consumer_user_id_snapshot = membership.consumer_user_id
				AND intent.status = $5
				AND intent.usage_payload IS NOT NULL
		) spend ON TRUE
		LEFT JOIN account_share_reviews review
			ON review.membership_id = membership.id
			AND review.consumer_user_id = membership.consumer_user_id
			AND review.deleted_at IS NULL
		WHERE membership.consumer_user_id = $1
			AND membership.status = $2
			AND membership.deleted_at IS NULL
		ORDER BY COALESCE(membership.ended_at, membership.updated_at, membership.joined_at) DESC, membership.id DESC
		LIMIT $3 OFFSET $4
	`, consumerUserID, service.AccountShareMembershipStatusEnded, limit, offset, service.AccountShareBillingIntentStatusSettled)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	entries := make([]service.AccountShareMembershipHistoryEntry, 0, limit)
	for rows.Next() {
		entry, err := scanAccountShareMembershipHistoryEntry(rows)
		if err != nil {
			return nil, nil, err
		}
		entries = append(entries, *entry)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return entries, accountShareReviewPagination(total, page, limit), nil
}

func scanAccountShareMembershipHistoryEntry(
	scanner accountShareMembershipScanner,
) (*service.AccountShareMembershipHistoryEntry, error) {
	var entry service.AccountShareMembershipHistoryEntry
	var listingRevisionID, listingVersion sql.NullInt64
	var roomDeletedAt, lastRequestAt, endedAt, paidUntil, billedUntil sql.NullTime
	var termsRaw []byte
	var reviewID, reviewScore sql.NullInt64
	var reviewComment, reviewStatus, reviewRejectReason string
	var reviewCreatedAt sql.NullTime
	if err := scanner.Scan(
		&entry.MembershipID,
		&entry.ListingID,
		&listingRevisionID,
		&listingVersion,
		&entry.RoomName,
		&entry.RoomDeleted,
		&roomDeletedAt,
		&entry.OwnerUserID,
		&entry.OwnerUsername,
		&entry.Platform,
		&entry.AccountLevel,
		&entry.AccountID,
		&entry.AccountName,
		&entry.ConfiguredConcurrencySnapshot,
		&entry.APIKeyID,
		&entry.APIKeyName,
		&entry.Status,
		&entry.JoinedAt,
		&lastRequestAt,
		&endedAt,
		&entry.EndedReason,
		&paidUntil,
		&billedUntil,
		&entry.HourlyRateSnapshot,
		&entry.HourlyFeeWaiverMinimum,
		&entry.IdleTimeoutMinutes,
		&entry.UsageRequestCount,
		&entry.UsageRequestCost,
		&termsRaw,
		&entry.SnapshotQuality,
		&reviewID,
		&reviewScore,
		&reviewComment,
		&reviewStatus,
		&reviewRejectReason,
		&reviewCreatedAt,
	); err != nil {
		return nil, err
	}

	entry.ListingRevisionID = sqlNullInt64Ptr(listingRevisionID)
	entry.ListingVersionSnapshot = sqlNullInt64Ptr(listingVersion)
	entry.RoomDeletedAt = sqlNullTimePtr(roomDeletedAt)
	entry.LastRequestAt = sqlNullTimePtr(lastRequestAt)
	entry.EndedAt = sqlNullTimePtr(endedAt)
	entry.PaidUntil = sqlNullTimePtr(paidUntil)
	entry.BilledUntil = sqlNullTimePtr(billedUntil)
	entry.RoomName = strings.TrimSpace(entry.RoomName)
	entry.OwnerUsername = strings.TrimSpace(entry.OwnerUsername)
	entry.Platform = strings.ToLower(strings.TrimSpace(entry.Platform))
	entry.AccountLevel = service.NormalizeAccountLevel(entry.AccountLevel)
	entry.AccountName = strings.TrimSpace(entry.AccountName)
	entry.APIKeyName = strings.TrimSpace(entry.APIKeyName)
	entry.SnapshotQuality = normalizeAccountShareSnapshotQuality(entry.SnapshotQuality)
	if err := validateAccountShareSnapshotQuality(entry.MembershipID, entry.SnapshotQuality); err != nil {
		return nil, err
	}
	terms, err := decodeAccountShareMembershipTermsSnapshot(
		entry.MembershipID,
		entry.ListingRevisionID,
		entry.ListingVersionSnapshot,
		termsRaw,
	)
	if err != nil {
		return nil, err
	}
	entry.TermsSnapshot = terms
	if terms != nil && strings.TrimSpace(terms.RoomName) != "" {
		entry.RoomName = strings.TrimSpace(terms.RoomName)
	}
	if reviewID.Valid {
		entry.Review = &service.AccountShareMembershipHistoryReview{
			ID:                  reviewID.Int64,
			Score:               int(reviewScore.Int64),
			Comment:             strings.TrimSpace(reviewComment),
			CommentStatus:       strings.TrimSpace(reviewStatus),
			CommentRejectReason: strings.TrimSpace(reviewRejectReason),
			CreatedAt:           sqlNullTimePtr(reviewCreatedAt),
		}
	}
	return &entry, nil
}

func validateAccountShareSnapshotQuality(membershipID int64, quality string) error {
	switch strings.TrimSpace(quality) {
	case service.AccountShareSnapshotQualityExact,
		service.AccountShareSnapshotQualityBackfilledCurrent,
		service.AccountShareSnapshotQualityUnknown:
		return nil
	default:
		return fmt.Errorf(
			"account share membership %d history snapshot has unsupported quality %q",
			membershipID,
			quality,
		)
	}
}

func normalizeAccountShareSnapshotQuality(quality string) string {
	normalized := strings.TrimSpace(quality)
	if normalized == "" {
		return service.AccountShareSnapshotQualityUnknown
	}
	return normalized
}

func decodeAccountShareMembershipTermsSnapshot(
	membershipID int64,
	listingRevisionID *int64,
	listingVersion *int64,
	raw []byte,
) (*service.AccountShareListingTermsSnapshot, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var terms service.AccountShareListingTermsSnapshot
	if err := json.Unmarshal(raw, &terms); err != nil {
		return nil, fmt.Errorf(
			"decode account share membership %d history terms snapshot: %w",
			membershipID,
			err,
		)
	}
	normalizeAccountShareListingTermsAliases(&terms)
	if listingRevisionID == nil ||
		listingVersion == nil ||
		terms.ListingRevisionID != *listingRevisionID ||
		terms.RowVersion != *listingVersion ||
		terms.SchemaVersion <= 0 {
		return nil, fmt.Errorf(
			"account share membership %d history terms snapshot does not match its listing revision",
			membershipID,
		)
	}
	return &terms, nil
}
