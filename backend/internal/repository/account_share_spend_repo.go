package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

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
