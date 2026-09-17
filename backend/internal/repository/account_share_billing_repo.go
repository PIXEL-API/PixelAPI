package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

func accountShareSeatPrepayRefID(membershipID int64, paidUntil time.Time) int64 {
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%d:%d", membershipID, paidUntil.UTC().UnixNano())
	refID := int64(h.Sum64() & 0x7fffffffffffffff)
	if refID == 0 {
		return 1
	}
	return refID
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
	result, unavailableErr := r.processUnavailableMembershipIDs(ctx, ids, result, now, service.AccountShareBackoffScopeUnavailable)
	if result == nil {
		result = &service.AccountShareSeatBillingResult{Processed: len(ids)}
	}
	return result, unavailableErr
}

// CleanupOrphanMembershipBindings 兜底清理历史遗留的孤儿 binding：
//  1. membership 已 ended（或已删除）但 binding 仍 unbound_at 为 NULL 的行——
//     这类行由早期 idle/预扣耗尽/账号不可用结束路径遗漏产生，会被账号删除
//     守卫判为不可解析的阻塞项（account_repo.go:2567），导致账号/房间永远
//     删不掉。
//  2. ending 已超过宽限时间、但仍有多条 open binding 的 membership——
//     FinalizeMembershipEnd 遇到 openBindings>1 会每轮返回同一错误形成永久
//     卡死；同一 membership 出现多条 open binding 本身就是提交后不变的
//     脏状态。清理时保留最新一条 open binding 交给 finalize 正常收口，
//     只关闭多余的历史行。宽限（默认 45min）覆盖在途 slot TTL，避免误伤
//     仍在正常流转的绑定。
// 正常退出统一由 FinalizeMembershipEnd 关闭 binding，本方法只处理存量脏数据。

func (r *accountShareModeRepository) CleanupOrphanMembershipBindings(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit <= 0 {
		limit = service.AccountShareModeSeatBillingBatchSize
	}
	if limit > 1000 {
		limit = 1000
	}
	now = now.UTC()
	cleaned := 0
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
	cleaned += int(affected)

	// ending 超时 membership 的多余 open binding：保留每个 membership 最新
	// 一条 open binding（id 最大者，对应最近一次绑定），关闭更早的残留行。
	endingBefore := now.Add(-service.AccountShareModeEndingOrphanBindingGrace)
	result, err = r.db.ExecContext(ctx, `
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
				AND membership.deleted_at IS NULL
				AND membership.status = $2
				AND membership.ending_requested_at IS NOT NULL
				AND membership.ending_requested_at <= $3
				AND binding.id < (
					SELECT MAX(newer.id)
					FROM account_share_membership_account_bindings newer
					WHERE newer.membership_id = binding.membership_id
						AND newer.unbound_at IS NULL
				)
			ORDER BY binding.id ASC
			LIMIT $4
			FOR UPDATE OF binding
		)
	`, now, service.AccountShareMembershipStatusEnding, endingBefore, limit)
	if err != nil {
		return cleaned, err
	}
	affected, err = result.RowsAffected()
	if err != nil {
		return cleaned, err
	}
	cleaned += int(affected)
	return cleaned, nil
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

func (r *accountShareModeRepository) BeginUnavailableMembershipEnd(ctx context.Context, membershipID int64, unavailableAt time.Time) (*service.AccountShareMembership, *service.AccountShareSeatBillingResult, error) {
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
		// 避免检测后新增健康账号的并发窗口仍结束 membership。
		return nil, nil, nil
	}
	membership, err = r.beginMembershipEndInTx(ctx, tx, membership, unavailableAt, service.AccountShareMembershipEndReasonUnavailable)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	tx = nil
	return membership, &service.AccountShareSeatBillingResult{EndedConsumerUserIDs: []int64{membership.ConsumerUserID}}, nil
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
	return r.processUnavailableMembershipIDs(ctx, ids, result, endedAt, service.AccountShareBackoffScopeUnavailable)
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

// processUnavailableMembershipIDs 逐条隔离失败：单行错误记入
// result.ItemFailures 并进入退避（scope 非空时），不再中断整轮。

func (r *accountShareModeRepository) processUnavailableMembershipIDs(ctx context.Context, ids []int64, result *service.AccountShareSeatBillingResult, endedAt time.Time, backoffScope string) (*service.AccountShareSeatBillingResult, error) {
	if result == nil {
		result = &service.AccountShareSeatBillingResult{}
	}
	for _, id := range ids {
		if backoffScope != "" && !r.itemBackoff.Allow(backoffScope, id, endedAt) {
			result.SkippedBackoff++
			continue
		}
		item, err := r.endUnavailableMembership(ctx, id, endedAt)
		if err != nil {
			if backoffScope != "" {
				delay := r.itemBackoff.OnFailure(backoffScope, id, endedAt)
				logger.LegacyPrintf("repository.account_share_mode", "end unavailable membership %d failed (retry in %s): %v", id, delay, err)
			}
			result.ItemFailures = append(result.ItemFailures, service.AccountShareItemFailure{ItemID: id, Err: err})
			continue
		}
		if backoffScope != "" {
			r.itemBackoff.OnSuccess(backoffScope, id)
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

	if _, _, err := lockAccountShareEndListingInTx(ctx, tx, membershipID, 0); err != nil {
		return nil, err
	}
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
	return r.processSeatBillingIDs(ctx, ids, result, now, service.AccountShareBackoffScopeSeatBilling)
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

	// 逐条隔离失败：单行错误记入 ItemFailures 并进入退避，游标照常推进，
	// 不再让一条坏 settlement 卡死整个补偿管线。
	result := &service.AccountShareSeatBillingResult{Processed: len(ids)}
	batch.Billing = result
	for _, id := range ids {
		if !r.itemBackoff.Allow(service.AccountShareBackoffScopeWaiver, id, readyBefore) {
			result.SkippedBackoff++
			continue
		}
		item, err := r.processSeatWaiverCompensation(ctx, id, readyBefore)
		if err != nil {
			delay := r.itemBackoff.OnFailure(service.AccountShareBackoffScopeWaiver, id, readyBefore)
			logger.LegacyPrintf("repository.account_share_mode", "process seat waiver compensation %d failed (retry in %s): %v", id, delay, err)
			result.ItemFailures = append(result.ItemFailures, service.AccountShareItemFailure{ItemID: id, Err: err})
			continue
		}
		r.itemBackoff.OnSuccess(service.AccountShareBackoffScopeWaiver, id)
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
	result, err = r.processSeatBillingIDs(ctx, ids, result, now, "")
	return firstAccountShareItemFailure(result, err)
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
	result, err = r.processSeatBillingIDs(ctx, ids, result, now, "")
	return firstAccountShareItemFailure(result, err)
}

// firstAccountShareItemFailure 在请求路径上保持"有行失败即返回错误"的对外
// 契约：批内已逐条隔离处理完毕，向调用方透出首个行级错误。

func firstAccountShareItemFailure(result *service.AccountShareSeatBillingResult, err error) (*service.AccountShareSeatBillingResult, error) {
	if err != nil || result == nil || len(result.ItemFailures) == 0 {
		return result, err
	}
	return result, result.ItemFailures[0].Err
}

// processSeatBillingIDs 逐条隔离失败：单行错误记入 result.ItemFailures 并
// 继续处理后续行，避免一条确定性坏行按 paid_until 排序反复阻塞整轮。
// backoffScope 非空时（后台 worker）失败行进入进程内退避；为空（请求路径
// ForJoin/ForRequest）时不退避，由调用方决定如何把 ItemFailures 透出。

func (r *accountShareModeRepository) processSeatBillingIDs(ctx context.Context, ids []int64, result *service.AccountShareSeatBillingResult, now time.Time, backoffScope string) (*service.AccountShareSeatBillingResult, error) {
	if result == nil {
		result = &service.AccountShareSeatBillingResult{}
	}
	for _, id := range ids {
		if backoffScope != "" && !r.itemBackoff.Allow(backoffScope, id, now) {
			result.SkippedBackoff++
			continue
		}
		item, err := r.processSeatBillingMembership(ctx, id, now)
		if err != nil {
			if backoffScope != "" {
				delay := r.itemBackoff.OnFailure(backoffScope, id, now)
				logger.LegacyPrintf("repository.account_share_mode", "process seat billing membership %d failed (retry in %s): %v", id, delay, err)
			}
			result.ItemFailures = append(result.ItemFailures, service.AccountShareItemFailure{ItemID: id, Err: err})
			continue
		}
		if backoffScope != "" {
			r.itemBackoff.OnSuccess(backoffScope, id)
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

	if _, _, err := lockAccountShareEndListingInTx(ctx, tx, membershipID, 0); err != nil {
		return nil, err
	}
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

	if err := lockAccountShareEndBillingUsersInTx(ctx, tx, membership); err != nil {
		return nil, err
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
	var userBalanceText string
	if err := tx.QueryRowContext(ctx, `
		SELECT balance::text
		FROM users
		WHERE id = $1
			AND deleted_at IS NULL
		FOR UPDATE
	`, membership.ConsumerUserID).Scan(&userBalanceText); errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUserNotFound
	} else if err != nil {
		return nil, err
	}
	userBalance, err := decimal.NewFromString(userBalanceText)
	if err != nil {
		return nil, fmt.Errorf("parse consumer %d balance: %w", membership.ConsumerUserID, err)
	}

	result := &service.AccountShareSeatBillingResult{CreditUserIDs: creditUserIDs}
	canRenewSeat := prepayAmount > 0 && !userBalance.LessThan(decimalFromFloat(prepayAmount))
	if !canRenewSeat {
		// Only already matured windows may have settled above. Keep unfinished
		// waiver windows for the finalizer after all request leases have drained.
		if settledUntil != nil {
			if _, err := tx.ExecContext(ctx, `
			UPDATE account_share_memberships
			SET billed_until = $2,
				waiver_window_started_at = $2,
				waiver_window_usage_amount = 0,
				waiver_window_request_count = 0,
				waiver_window_last_request_at = NULL
			WHERE id = $1 AND status = 'active'
		`, membership.ID, *settledUntil); err != nil {
				return nil, err
			}
		}
		if _, err := r.beginMembershipEndInTx(ctx, tx, membership, *membership.PaidUntil, service.AccountShareMembershipEndReasonPrepay); err != nil {
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
	newBalance := userBalance.Sub(decimalFromFloat(prepayAmount))
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
	`, newBalance.StringFixed(10), membership.ConsumerUserID); err != nil {
		return nil, err
	}
	if err := insertUserBalanceLedger(ctx, tx, userBalanceLedgerInput{
		UserID:          membership.ConsumerUserID,
		Direction:       "debit",
		Amount:          decimalFromFloat(prepayAmount),
		Reason:          accountShareSeatPrepayReason,
		RefType:         refType,
		RefID:           refID,
		BalanceAfter:    newBalance,
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
		var newBalanceText string
		err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1::numeric,
			updated_at = NOW()
		WHERE id = $2
		RETURNING balance::text
		`, charge.Split.OwnerCredit.StringFixed(10), membership.OwnerUserID).Scan(&newBalanceText)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrUserNotFound
		}
		if err != nil {
			return nil, err
		}
		newBalance, err := decimal.NewFromString(newBalanceText)
		if err != nil {
			return nil, fmt.Errorf("parse owner %d balance: %w", membership.OwnerUserID, err)
		}
		if err := insertUserBalanceLedger(ctx, tx, userBalanceLedgerInput{
			UserID:          membership.OwnerUserID,
			Direction:       "debit",
			Amount:          charge.Split.OwnerCredit,
			Reason:          accountShareSeatWaiverRefundReason,
			RefType:         accountShareModeSettlementRefType,
			RefID:           nullablePositiveInt64(refundSettlementID),
			BalanceAfter:    newBalance,
			RequireInserted: true,
			Metadata:        accountShareSeatWaiverReversalMetadata(membership, charge, refundSettlementID, waiver),
		}); err != nil {
			return nil, err
		}
		debitUserIDs = append(debitUserIDs, membership.OwnerUserID)
	}
	if charge.Split.Invite.InviterUserID > 0 && charge.Split.InviteCredit.GreaterThan(decimal.Zero) {
		inviterUserID := charge.Split.Invite.InviterUserID
		var newBalanceText string
		err := tx.QueryRowContext(ctx, `
			UPDATE users
			SET balance = balance - $1::numeric,
				updated_at = NOW()
			WHERE id = $2
			RETURNING balance::text
		`, charge.Split.InviteCredit.StringFixed(10), inviterUserID).Scan(&newBalanceText)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrUserNotFound
		}
		if err != nil {
			return nil, err
		}
		newBalance, err := decimal.NewFromString(newBalanceText)
		if err != nil {
			return nil, fmt.Errorf("parse inviter %d balance: %w", inviterUserID, err)
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
			BalanceAfter:    newBalance,
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
	if _, err := r.beginMembershipEndInTx(ctx, tx, membership, endedAt.UTC(), reason); err != nil {
		return nil, err
	}
	return &service.AccountShareSeatBillingResult{EndedConsumerUserIDs: []int64{membership.ConsumerUserID}}, nil
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
			BalanceAfter:    newBalance,
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
		BalanceAfter:    newBalance,
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
		BalanceAfter:    newBalance,
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
