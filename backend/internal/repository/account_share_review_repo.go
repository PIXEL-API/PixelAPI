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
	"github.com/lib/pq"
)

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
