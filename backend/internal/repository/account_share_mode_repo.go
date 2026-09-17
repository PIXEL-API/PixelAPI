package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type accountShareModeRepository struct {
	db      *sql.DB
	rollout config.AccountShareRolloutConfig
	// itemBackoff 为后台批处理管线（席位计费、unavailable 结束、waiver
	// 补偿）提供单行失败的进程内退避；scope 前缀区分不同管线中相同的
	// membership/settlement id。请求路径不使用它，保证瞬时故障可随
	// 用户请求立即自愈。
	itemBackoff *service.AccountShareItemBackoff
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
		db:          sqlDB,
		rollout:     rollout,
		itemBackoff: service.NewAccountShareItemBackoff(),
	}
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
			l.join_password_hash,
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
			l.created_at,
			l.updated_at
		FROM %s l
		%s
		LEFT JOIN users u ON u.id = l.owner_user_id
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
			return service.ErrAccountShareAPIKeyAlreadyBound.WithCause(err)
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
