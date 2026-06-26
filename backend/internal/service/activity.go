package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	ActivityTypeConsumptionLottery = "consumption_lottery"

	ActivityStatusDraft  = "draft"
	ActivityStatusActive = "active"
	ActivityStatusPaused = "paused"
	ActivityStatusEnded  = "ended"

	ActivityMetricAPICostAmount   = "api_cost_amount"
	ActivityMetricAPIRequestCount = "api_request_count"

	ActivityPeriodFixedRange  = "fixed_range"
	ActivityPeriodToday       = "today"
	ActivityPeriodRollingDays = "rolling_days"
	ActivityPeriodCampaign    = "campaign"

	ActivityTicketModeFixed        = "fixed"
	ActivityTicketModeProportional = "proportional"
	ActivityTicketModeTiered       = "tiered"
	ActivityTierModeHighest        = "highest"
	ActivityTierModeCumulative     = "cumulative"

	ActivityPrizeBalance           = "balance"
	ActivityPrizePoints            = "points"
	ActivityPrizeLoadFactorCredits = "load_factor_credits"
	ActivityPrizeManual            = "manual"

	ActivityWinnerStatusPendingClaim    = "pending_claim"
	ActivityWinnerStatusPendingDelivery = "pending_delivery"
	ActivityWinnerStatusDelivered       = "delivered"
	ActivityWinnerStatusRejected        = "rejected"
	ActivityWinnerStatusExpired         = "expired"

	ActivityClaimStatusNotRequired = "not_required"
	ActivityClaimStatusPending     = "pending"
	ActivityClaimStatusSubmitted   = "submitted"

	ActivityPublicParticipantCountOff   = "off"
	ActivityPublicParticipantCountFuzzy = "fuzzy"
	ActivityPublicParticipantCountExact = "exact"

	ActivityRewardLedgerReason = "activity_lottery_reward"
	ActivityRewardRefType      = "activity_winner"
)

var (
	ErrActivityNotFound          = infraerrors.NotFound("ACTIVITY_NOT_FOUND", "activity campaign not found")
	ErrActivityInvalidInput      = infraerrors.BadRequest("ACTIVITY_INVALID_INPUT", "invalid activity input")
	ErrActivityDrawNotReady      = infraerrors.Conflict("ACTIVITY_DRAW_NOT_READY", "activity draw time has not arrived")
	ErrActivityDrawAlreadyExists = infraerrors.Conflict("ACTIVITY_DRAW_ALREADY_EXISTS", "activity draw already exists")
	ErrActivityWinnerNotFound    = infraerrors.NotFound("ACTIVITY_WINNER_NOT_FOUND", "activity winner not found")
	ErrActivityClaimUnavailable  = infraerrors.Conflict("ACTIVITY_CLAIM_UNAVAILABLE", "winner claim is not available")
	ErrActivityJoinUnavailable   = infraerrors.Conflict("ACTIVITY_JOIN_UNAVAILABLE", "activity draw join is not available")
	ErrActivityNotQualified      = infraerrors.BadRequest("ACTIVITY_NOT_QUALIFIED", "activity qualification has not been reached")
)

type ActivityService struct {
	entClient           *dbent.Client
	encryptor           SecretEncryptor
	billingCacheService *BillingCacheService
}

func NewActivityService(entClient *dbent.Client, encryptor SecretEncryptor, billingCacheService *BillingCacheService) *ActivityService {
	return &ActivityService{
		entClient:           entClient,
		encryptor:           encryptor,
		billingCacheService: billingCacheService,
	}
}

type ActivityCampaignDTO struct {
	ID               int64                   `json:"id"`
	Type             string                  `json:"type"`
	Name             string                  `json:"name"`
	Description      *string                 `json:"description,omitempty"`
	CoverURL         *string                 `json:"cover_url,omitempty"`
	Status           string                  `json:"status"`
	StartsAt         time.Time               `json:"starts_at"`
	EndsAt           time.Time               `json:"ends_at"`
	DrawAt           *time.Time              `json:"draw_at,omitempty"`
	Timezone         string                  `json:"timezone"`
	PublicEnabled    bool                    `json:"public_enabled"`
	SortOrder        int                     `json:"sort_order"`
	RuleConfig       ActivityRuleConfig      `json:"rule_config"`
	DisplayConfig    map[string]any          `json:"display_config"`
	Prizes           []ActivityPrizeDTO      `json:"prizes,omitempty"`
	UserProgress     *ActivityProgressDTO    `json:"user_progress,omitempty"`
	PublicStats      *ActivityPublicStatsDTO `json:"public_stats,omitempty"`
	YesterdayWinners []ActivityWinnerPublic  `json:"yesterday_winners,omitempty"`
	RecentWinners    []ActivityWinnerPublic  `json:"recent_winners,omitempty"`
	CreatedAt        time.Time               `json:"created_at"`
	UpdatedAt        time.Time               `json:"updated_at"`
}

type ActivityRuleConfig struct {
	Metric            string             `json:"metric"`
	PeriodType        string             `json:"period_type"`
	PeriodStartAt     *time.Time         `json:"period_start_at,omitempty"`
	PeriodEndAt       *time.Time         `json:"period_end_at,omitempty"`
	RollingDays       int                `json:"rolling_days,omitempty"`
	Threshold         float64            `json:"threshold"`
	TicketMode        string             `json:"ticket_mode"`
	FixedTickets      int                `json:"fixed_tickets,omitempty"`
	UnitAmount        float64            `json:"unit_amount,omitempty"`
	TicketsPerUnit    int                `json:"tickets_per_unit,omitempty"`
	MaxTicketsPerUser int                `json:"max_tickets_per_user,omitempty"`
	TierMode          string             `json:"tier_mode,omitempty"`
	Tiers             []ActivityRuleTier `json:"tiers,omitempty"`
}

type ActivityRuleTier struct {
	Threshold float64 `json:"threshold"`
	Tickets   int     `json:"tickets"`
}

type ActivityPrizeDTO struct {
	ID                int64                `json:"id"`
	CampaignID        int64                `json:"campaign_id"`
	Name              string               `json:"name"`
	Description       *string              `json:"description,omitempty"`
	PrizeType         string               `json:"prize_type"`
	Amount            float64              `json:"amount"`
	Quantity          int                  `json:"quantity"`
	Weight            float64              `json:"weight"`
	RequiresClaimInfo bool                 `json:"requires_claim_info"`
	ClaimFields       []ActivityClaimField `json:"claim_fields"`
	Enabled           bool                 `json:"enabled"`
	SortOrder         int                  `json:"sort_order"`
	CreatedAt         time.Time            `json:"created_at"`
	UpdatedAt         time.Time            `json:"updated_at"`
}

type ActivityClaimField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
	Type     string `json:"type,omitempty"`
}

type ActivityProgressDTO struct {
	MetricType    string     `json:"metric_type"`
	MetricValue   float64    `json:"metric_value"`
	TicketCount   int        `json:"ticket_count"`
	NextThreshold *float64   `json:"next_threshold,omitempty"`
	NextTickets   *int       `json:"next_tickets,omitempty"`
	PeriodStartAt time.Time  `json:"period_start_at"`
	PeriodEndAt   time.Time  `json:"period_end_at"`
	DrawAt        *time.Time `json:"draw_at,omitempty"`
	Joined        bool       `json:"joined"`
	JoinedTickets int        `json:"joined_tickets"`
	JoinedAt      *time.Time `json:"joined_at,omitempty"`
}

type ActivityWinnerPublic struct {
	ID           int64     `json:"id"`
	CampaignID   int64     `json:"campaign_id"`
	CampaignName string    `json:"campaign_name"`
	PrizeName    string    `json:"prize_name"`
	PrizeType    string    `json:"prize_type"`
	PrizeAmount  float64   `json:"prize_amount"`
	MaskedUser   string    `json:"masked_user"`
	CreatedAt    time.Time `json:"created_at"`
}

type ActivityWinnerDTO struct {
	ID               int64                `json:"id"`
	CampaignID       int64                `json:"campaign_id"`
	CampaignName     string               `json:"campaign_name,omitempty"`
	DrawID           int64                `json:"draw_id"`
	PrizeID          *int64               `json:"prize_id,omitempty"`
	UserID           int64                `json:"user_id"`
	UserEmail        string               `json:"user_email,omitempty"`
	UserUsername     string               `json:"user_username,omitempty"`
	PrizeName        string               `json:"prize_name"`
	PrizeType        string               `json:"prize_type"`
	PrizeAmount      float64              `json:"prize_amount"`
	ClaimFields      []ActivityClaimField `json:"claim_fields,omitempty"`
	TicketCount      int                  `json:"ticket_count"`
	MaskedUser       string               `json:"masked_user"`
	Status           string               `json:"status"`
	ClaimStatus      string               `json:"claim_status"`
	ClaimInfo        map[string]any       `json:"claim_info,omitempty"`
	ClaimSubmittedAt *time.Time           `json:"claim_submitted_at,omitempty"`
	DeliveredAt      *time.Time           `json:"delivered_at,omitempty"`
	AdminNote        *string              `json:"admin_note,omitempty"`
	CreatedAt        time.Time            `json:"created_at"`
	UpdatedAt        time.Time            `json:"updated_at"`
}

type ActivityDrawResult struct {
	DrawID       int64               `json:"draw_id"`
	CampaignID   int64               `json:"campaign_id"`
	CampaignName string              `json:"campaign_name,omitempty"`
	TotalUsers   int                 `json:"total_users"`
	TotalTickets int                 `json:"total_tickets"`
	WinnerCount  int                 `json:"winner_count"`
	Winners      []ActivityWinnerDTO `json:"winners"`
}

type ActivityAutoDrawResult struct {
	Processed int                  `json:"processed"`
	Draws     []ActivityDrawResult `json:"draws"`
}

type ActivityPublicStatsDTO struct {
	ParticipantCountMode   string `json:"participant_count_mode"`
	ParticipantCount       *int64 `json:"participant_count,omitempty"`
	ParticipantCountBucket string `json:"participant_count_bucket,omitempty"`
}

type ActivityDrawSummaryDTO struct {
	ID              int64     `json:"id"`
	DrawAt          time.Time `json:"draw_at"`
	SnapshotStartAt time.Time `json:"snapshot_start_at"`
	SnapshotEndAt   time.Time `json:"snapshot_end_at"`
	Status          string    `json:"status"`
	TotalUsers      int64     `json:"total_users"`
	TotalTickets    int64     `json:"total_tickets"`
	WinnerCount     int64     `json:"winner_count"`
	ExecutedAt      time.Time `json:"executed_at"`
}

type ActivityCampaignStatsDTO struct {
	CampaignID            int64                   `json:"campaign_id"`
	CampaignName          string                  `json:"campaign_name"`
	Status                string                  `json:"status"`
	PeriodStartAt         time.Time               `json:"period_start_at"`
	PeriodEndAt           time.Time               `json:"period_end_at"`
	DrawAt                *time.Time              `json:"draw_at,omitempty"`
	JoinedUserCount       int64                   `json:"joined_user_count"`
	JoinedTicketCount     int64                   `json:"joined_ticket_count"`
	JoinedMetricTotal     float64                 `json:"joined_metric_total"`
	AverageTicketsPerUser float64                 `json:"average_tickets_per_user"`
	AverageMetricValue    float64                 `json:"average_metric_value"`
	MaxTicketCount        int                     `json:"max_ticket_count"`
	MaxMetricValue        float64                 `json:"max_metric_value"`
	FirstJoinedAt         *time.Time              `json:"first_joined_at,omitempty"`
	LastJoinedAt          *time.Time              `json:"last_joined_at,omitempty"`
	EnabledPrizeCount     int                     `json:"enabled_prize_count"`
	PrizeTotalQuantity    int                     `json:"prize_total_quantity"`
	WinnerCount           int64                   `json:"winner_count"`
	PendingClaimCount     int64                   `json:"pending_claim_count"`
	PendingDeliveryCount  int64                   `json:"pending_delivery_count"`
	DeliveredCount        int64                   `json:"delivered_count"`
	RejectedCount         int64                   `json:"rejected_count"`
	ExpiredCount          int64                   `json:"expired_count"`
	ClaimSubmittedCount   int64                   `json:"claim_submitted_count"`
	PendingActionCount    int64                   `json:"pending_action_count"`
	Drawn                 bool                    `json:"drawn"`
	CanRunDraw            bool                    `json:"can_run_draw"`
	DrawBlockReason       string                  `json:"draw_block_reason,omitempty"`
	NoParticipantWarning  bool                    `json:"no_participant_warning"`
	LatestDraw            *ActivityDrawSummaryDTO `json:"latest_draw,omitempty"`
}

type ActivityListParams struct {
	Page     int
	PageSize int
	Status   string
	Keyword  string
}

type ActivityCampaignUpsertInput struct {
	Type          string                `json:"type"`
	Name          string                `json:"name"`
	Description   *string               `json:"description"`
	CoverURL      *string               `json:"cover_url"`
	Status        string                `json:"status"`
	StartsAt      time.Time             `json:"starts_at"`
	EndsAt        time.Time             `json:"ends_at"`
	DrawAt        *time.Time            `json:"draw_at"`
	Timezone      string                `json:"timezone"`
	PublicEnabled bool                  `json:"public_enabled"`
	SortOrder     int                   `json:"sort_order"`
	RuleConfig    ActivityRuleConfig    `json:"rule_config"`
	DisplayConfig map[string]any        `json:"display_config"`
	Prizes        []ActivityPrizeUpsert `json:"prizes"`
}

type ActivityPrizeUpsert struct {
	ID                int64                `json:"id,omitempty"`
	Name              string               `json:"name"`
	Description       *string              `json:"description"`
	PrizeType         string               `json:"prize_type"`
	Amount            float64              `json:"amount"`
	Quantity          int                  `json:"quantity"`
	Weight            float64              `json:"weight"`
	RequiresClaimInfo bool                 `json:"requires_claim_info"`
	ClaimFields       []ActivityClaimField `json:"claim_fields"`
	Enabled           bool                 `json:"enabled"`
	SortOrder         int                  `json:"sort_order"`
}

type activityCampaignRow struct {
	dto ActivityCampaignDTO
}

type activityPeriod struct {
	start time.Time
	end   time.Time
}

type activityEligibleUser struct {
	userID      int64
	email       string
	username    string
	metricValue float64
	tickets     int
}

type activitySQLClient interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (s *ActivityService) AdminListCampaigns(ctx context.Context, params ActivityListParams) ([]ActivityCampaignDTO, int64, error) {
	if s == nil || s.entClient == nil {
		return nil, 0, infraerrors.ServiceUnavailable("ACTIVITY_SERVICE_UNAVAILABLE", "activity service unavailable")
	}
	page, pageSize := normalizeActivityPagination(params.Page, params.PageSize)
	where := []string{"1=1"}
	args := []any{}
	if status := strings.TrimSpace(params.Status); status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(params.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		where = append(where, fmt.Sprintf("(name ILIKE $%d OR COALESCE(description, '') ILIKE $%d)", len(args), len(args)))
	}
	whereSQL := strings.Join(where, " AND ")
	var total int64
	if err := s.querySingle(ctx, "SELECT COUNT(*) FROM activity_campaigns WHERE "+whereSQL, args, &total); err != nil {
		return nil, 0, fmt.Errorf("count activity campaigns: %w", err)
	}
	args = append(args, pageSize, (page-1)*pageSize)
	query := fmt.Sprintf(`
		SELECT %s
		FROM activity_campaigns
		WHERE %s
		ORDER BY sort_order ASC, created_at DESC, id DESC
		LIMIT $%d OFFSET $%d
	`, activityCampaignSelectColumns, whereSQL, len(args)-1, len(args))
	items, err := s.queryCampaigns(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	if err := s.attachPrizes(ctx, items); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *ActivityService) AdminGetCampaign(ctx context.Context, id int64) (*ActivityCampaignDTO, error) {
	item, err := s.getCampaign(ctx, id)
	if err != nil {
		return nil, err
	}
	items := []ActivityCampaignDTO{*item}
	if err := s.attachPrizes(ctx, items); err != nil {
		return nil, err
	}
	return &items[0], nil
}

func (s *ActivityService) AdminGetCampaignStats(ctx context.Context, id int64) (*ActivityCampaignStatsDTO, error) {
	campaign, err := s.AdminGetCampaign(ctx, id)
	if err != nil {
		return nil, err
	}
	ref := time.Now()
	if campaign.DrawAt != nil {
		ref = *campaign.DrawAt
	}
	period, err := activityRulePeriod(campaign, ref)
	if err != nil {
		return nil, err
	}
	stats := &ActivityCampaignStatsDTO{
		CampaignID:    campaign.ID,
		CampaignName:  campaign.Name,
		Status:        campaign.Status,
		PeriodStartAt: period.start,
		PeriodEndAt:   period.end,
		DrawAt:        campaign.DrawAt,
	}
	for _, prize := range campaign.Prizes {
		if !prize.Enabled {
			continue
		}
		stats.EnabledPrizeCount++
		stats.PrizeTotalQuantity += prize.Quantity
	}
	if campaign.DrawAt != nil {
		if err := s.attachActivityParticipationStats(ctx, stats, campaign.ID, *campaign.DrawAt); err != nil {
			return nil, err
		}
		drawn, err := s.activityDrawExists(ctx, campaign.ID, *campaign.DrawAt)
		if err != nil {
			return nil, err
		}
		stats.Drawn = drawn
	}
	latestDraw, err := s.latestActivityDrawSummary(ctx, campaign.ID)
	if err != nil {
		return nil, err
	}
	stats.LatestDraw = latestDraw
	if err := s.attachActivityWinnerStats(ctx, stats, campaign.ID); err != nil {
		return nil, err
	}
	stats.PendingActionCount = stats.PendingClaimCount + stats.PendingDeliveryCount
	stats.CanRunDraw, stats.DrawBlockReason = activityDrawReadiness(campaign, stats.Drawn, time.Now())
	stats.NoParticipantWarning = stats.CanRunDraw && stats.JoinedUserCount == 0
	return stats, nil
}

func (s *ActivityService) AdminCreateCampaign(ctx context.Context, input ActivityCampaignUpsertInput, operatorUserID int64) (*ActivityCampaignDTO, error) {
	normalized, err := normalizeActivityCampaignInput(input)
	if err != nil {
		return nil, err
	}
	ruleJSON, displayJSON, err := marshalActivityConfig(normalized.RuleConfig, normalized.DisplayConfig)
	if err != nil {
		return nil, err
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin activity create tx: %w", err)
	}
	defer rollbackEntTx(tx)
	client, err := activityTxClient(tx)
	if err != nil {
		return nil, err
	}
	var id int64
	rows, err := client.QueryContext(ctx, `
		INSERT INTO activity_campaigns (
			type, name, description, cover_url, status, starts_at, ends_at, draw_at,
			timezone, public_enabled, sort_order, rule_config, display_config,
			created_by_user_id, updated_by_user_id
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12::jsonb, $13::jsonb,
			$14, $14
		)
		RETURNING id
	`, normalized.Type, normalized.Name, nullableStringArg(normalized.Description), nullableStringArg(normalized.CoverURL),
		normalized.Status, normalized.StartsAt, normalized.EndsAt, nullableTimeArg(normalized.DrawAt),
		normalized.Timezone, normalized.PublicEnabled, normalized.SortOrder, string(ruleJSON), string(displayJSON), operatorUserID)
	if err != nil {
		return nil, fmt.Errorf("create activity campaign: %w", err)
	}
	if rows.Next() {
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan activity campaign id: %w", err)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close activity create rows: %w", err)
	}
	if id <= 0 {
		return nil, fmt.Errorf("create activity campaign returned no id")
	}
	if err := replaceActivityPrizes(ctx, client, id, normalized.Prizes); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit activity create tx: %w", err)
	}
	return s.AdminGetCampaign(ctx, id)
}

func (s *ActivityService) AdminUpdateCampaign(ctx context.Context, id int64, input ActivityCampaignUpsertInput, operatorUserID int64) (*ActivityCampaignDTO, error) {
	if id <= 0 {
		return nil, ErrActivityNotFound
	}
	if _, err := s.getCampaign(ctx, id); err != nil {
		return nil, err
	}
	normalized, err := normalizeActivityCampaignInput(input)
	if err != nil {
		return nil, err
	}
	ruleJSON, displayJSON, err := marshalActivityConfig(normalized.RuleConfig, normalized.DisplayConfig)
	if err != nil {
		return nil, err
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin activity update tx: %w", err)
	}
	defer rollbackEntTx(tx)
	client, err := activityTxClient(tx)
	if err != nil {
		return nil, err
	}
	if _, err := client.ExecContext(ctx, `
		UPDATE activity_campaigns
		SET type = $1,
			name = $2,
			description = $3,
			cover_url = $4,
			status = $5,
			starts_at = $6,
			ends_at = $7,
			draw_at = $8,
			timezone = $9,
			public_enabled = $10,
			sort_order = $11,
			rule_config = $12::jsonb,
			display_config = $13::jsonb,
			updated_by_user_id = $14,
			updated_at = NOW()
		WHERE id = $15
	`, normalized.Type, normalized.Name, nullableStringArg(normalized.Description), nullableStringArg(normalized.CoverURL),
		normalized.Status, normalized.StartsAt, normalized.EndsAt, nullableTimeArg(normalized.DrawAt),
		normalized.Timezone, normalized.PublicEnabled, normalized.SortOrder, string(ruleJSON), string(displayJSON), operatorUserID, id); err != nil {
		return nil, fmt.Errorf("update activity campaign: %w", err)
	}
	if err := replaceActivityPrizes(ctx, client, id, normalized.Prizes); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit activity update tx: %w", err)
	}
	return s.AdminGetCampaign(ctx, id)
}

func (s *ActivityService) AdminEndCampaign(ctx context.Context, id, operatorUserID int64) error {
	if id <= 0 {
		return ErrActivityNotFound
	}
	res, err := s.entClient.ExecContext(ctx, `
		UPDATE activity_campaigns
		SET status = 'ended',
			updated_by_user_id = $2,
			updated_at = NOW()
		WHERE id = $1
	`, id, operatorUserID)
	if err != nil {
		return fmt.Errorf("end activity campaign: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrActivityNotFound
	}
	return nil
}

func (s *ActivityService) UserListWelfareActivities(ctx context.Context, userID int64) ([]ActivityCampaignDTO, error) {
	if userID <= 0 {
		return nil, infraerrors.Unauthorized("UNAUTHORIZED", "authentication required")
	}
	now := time.Now()
	items, err := s.queryCampaigns(ctx, `
		SELECT `+activityCampaignSelectColumns+`
		FROM activity_campaigns
		WHERE public_enabled = TRUE
			AND status IN ('active', 'ended')
			AND starts_at <= $1
		ORDER BY CASE WHEN ends_at >= $1 THEN 0 ELSE 1 END ASC,
			sort_order ASC, starts_at DESC, id DESC
		LIMIT 100
	`, now)
	if err != nil {
		return nil, err
	}
	if err := s.attachPrizes(ctx, items); err != nil {
		return nil, err
	}
	for i := range items {
		progressRef := activityProgressReferenceTime(&items[i], now)
		progress, err := s.computeUserProgress(ctx, &items[i], userID, progressRef)
		if err != nil {
			return nil, err
		}
		items[i].UserProgress = progress
		winners, err := s.ListYesterdayWinners(ctx, items[i].ID, items[i].Timezone, 8)
		if err != nil {
			return nil, err
		}
		items[i].YesterdayWinners = winners
		recentWinners, err := s.ListRecentWinners(ctx, items[i].ID, 12)
		if err != nil {
			return nil, err
		}
		items[i].RecentWinners = recentWinners
		if err := s.attachActivityPublicStats(ctx, &items[i]); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *ActivityService) UserJoinDraw(ctx context.Context, userID, campaignID int64) (*ActivityProgressDTO, error) {
	if userID <= 0 {
		return nil, infraerrors.Unauthorized("UNAUTHORIZED", "authentication required")
	}
	campaign, err := s.getCampaign(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if !campaign.PublicEnabled || campaign.Status != ActivityStatusActive || now.Before(campaign.StartsAt) || now.After(campaign.EndsAt) {
		return nil, ErrActivityJoinUnavailable
	}
	if campaign.DrawAt == nil || !now.Before(*campaign.DrawAt) {
		return nil, ErrActivityJoinUnavailable
	}
	var drawCount int64
	if err := s.querySingle(ctx, `
		SELECT COUNT(*)
		FROM activity_draws
		WHERE campaign_id = $1 AND draw_at = $2
	`, []any{campaign.ID, *campaign.DrawAt}, &drawCount); err != nil {
		return nil, fmt.Errorf("check activity draw existence: %w", err)
	}
	if drawCount > 0 {
		return nil, ErrActivityDrawAlreadyExists
	}
	progress, err := s.computeUserProgress(ctx, campaign, userID, now)
	if err != nil {
		return nil, err
	}
	if progress.TicketCount <= 0 {
		return nil, ErrActivityNotQualified
	}
	rows, err := s.entClient.QueryContext(ctx, `
		INSERT INTO activity_entries (
			campaign_id, user_id, draw_at, snapshot_start_at, snapshot_end_at,
			metric_type, metric_value, ticket_count
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8
		)
		ON CONFLICT (campaign_id, user_id, draw_at) DO UPDATE
		SET snapshot_start_at = EXCLUDED.snapshot_start_at,
			snapshot_end_at = EXCLUDED.snapshot_end_at,
			metric_type = EXCLUDED.metric_type,
			metric_value = GREATEST(activity_entries.metric_value, EXCLUDED.metric_value),
			ticket_count = GREATEST(activity_entries.ticket_count, EXCLUDED.ticket_count),
			updated_at = NOW()
		WHERE activity_entries.draw_id IS NULL
		RETURNING ticket_count, created_at
	`, campaign.ID, userID, *campaign.DrawAt, progress.PeriodStartAt, progress.PeriodEndAt,
		progress.MetricType, progress.MetricValue, progress.TicketCount)
	if err != nil {
		return nil, fmt.Errorf("join activity draw: %w", err)
	}
	var joinedTickets int
	var joinedAt time.Time
	if rows.Next() {
		if err := rows.Scan(&joinedTickets, &joinedAt); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan joined activity entry: %w", err)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close joined activity rows: %w", err)
	}
	if joinedTickets <= 0 {
		return nil, ErrActivityDrawAlreadyExists
	}
	progress.Joined = true
	progress.JoinedTickets = joinedTickets
	progress.JoinedAt = &joinedAt
	return progress, nil
}

func (s *ActivityService) ListYesterdayWinners(ctx context.Context, campaignID int64, timezone string, limit int) ([]ActivityWinnerPublic, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	loc := mustActivityLocation(timezone)
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	start := today.AddDate(0, 0, -1).UTC()
	end := today.UTC()
	args := []any{start, end}
	where := "w.created_at >= $1 AND w.created_at < $2 AND w.status IN ('pending_claim', 'pending_delivery', 'delivered')"
	if campaignID > 0 {
		args = append(args, campaignID)
		where += fmt.Sprintf(" AND w.campaign_id = $%d", len(args))
	}
	args = append(args, limit)
	query := fmt.Sprintf(`
		SELECT w.id, w.campaign_id, c.name, w.prize_name, w.prize_type,
			w.prize_amount::double precision, w.masked_user, w.created_at
		FROM activity_winners w
		INNER JOIN activity_campaigns c ON c.id = w.campaign_id
		WHERE %s
		ORDER BY w.created_at DESC, w.id DESC
		LIMIT $%d
	`, where, len(args))
	rows, err := s.entClient.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query yesterday activity winners: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []ActivityWinnerPublic{}
	for rows.Next() {
		var item ActivityWinnerPublic
		if err := rows.Scan(&item.ID, &item.CampaignID, &item.CampaignName, &item.PrizeName, &item.PrizeType, &item.PrizeAmount, &item.MaskedUser, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan yesterday activity winner: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate yesterday activity winners: %w", err)
	}
	return out, nil
}

func (s *ActivityService) ListRecentWinners(ctx context.Context, campaignID int64, limit int) ([]ActivityWinnerPublic, error) {
	if limit <= 0 || limit > 50 {
		limit = 12
	}
	args := []any{}
	where := "w.status IN ('pending_claim', 'pending_delivery', 'delivered')"
	if campaignID > 0 {
		args = append(args, campaignID)
		where += fmt.Sprintf(" AND w.campaign_id = $%d", len(args))
	}
	args = append(args, limit)
	query := fmt.Sprintf(`
		SELECT w.id, w.campaign_id, c.name, w.prize_name, w.prize_type,
			w.prize_amount::double precision, w.masked_user, w.created_at
		FROM activity_winners w
		INNER JOIN activity_campaigns c ON c.id = w.campaign_id
		WHERE %s
		ORDER BY w.created_at DESC, w.id DESC
		LIMIT $%d
	`, where, len(args))
	rows, err := s.entClient.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query recent activity winners: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []ActivityWinnerPublic{}
	for rows.Next() {
		var item ActivityWinnerPublic
		if err := rows.Scan(&item.ID, &item.CampaignID, &item.CampaignName, &item.PrizeName, &item.PrizeType, &item.PrizeAmount, &item.MaskedUser, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan recent activity winner: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent activity winners: %w", err)
	}
	return out, nil
}

func (s *ActivityService) UserListWinners(ctx context.Context, userID int64) ([]ActivityWinnerDTO, error) {
	if userID <= 0 {
		return nil, infraerrors.Unauthorized("UNAUTHORIZED", "authentication required")
	}
	return s.queryWinners(ctx, `
		SELECT `+activityWinnerSelectColumns+`
		FROM activity_winners w
		INNER JOIN activity_campaigns c ON c.id = w.campaign_id
		LEFT JOIN users u ON u.id = w.user_id
		WHERE w.user_id = $1
		ORDER BY w.created_at DESC, w.id DESC
		LIMIT 100
	`, true, userID)
}

func (s *ActivityService) UserSubmitWinnerClaim(ctx context.Context, userID, winnerID int64, claimInfo map[string]any) (*ActivityWinnerDTO, error) {
	if userID <= 0 {
		return nil, infraerrors.Unauthorized("UNAUTHORIZED", "authentication required")
	}
	if winnerID <= 0 {
		return nil, ErrActivityWinnerNotFound
	}
	winners, err := s.queryWinners(ctx, `
		SELECT `+activityWinnerSelectColumns+`
		FROM activity_winners w
		INNER JOIN activity_campaigns c ON c.id = w.campaign_id
		LEFT JOIN users u ON u.id = w.user_id
		WHERE w.id = $1 AND w.user_id = $2
	`, true, winnerID, userID)
	if err != nil {
		return nil, err
	}
	if len(winners) == 0 {
		return nil, ErrActivityWinnerNotFound
	}
	winner := winners[0]
	if winner.Status != ActivityWinnerStatusPendingClaim || winner.ClaimStatus == ActivityClaimStatusNotRequired {
		return nil, ErrActivityClaimUnavailable
	}
	if err := validateActivityClaimInfo(winner.ClaimFields, claimInfo); err != nil {
		return nil, err
	}
	encrypted, err := s.encryptClaimInfo(claimInfo)
	if err != nil {
		return nil, err
	}
	if _, err := s.entClient.ExecContext(ctx, `
		UPDATE activity_winners
		SET claim_info_encrypted = $1,
			claim_status = 'submitted',
			status = 'pending_delivery',
			claim_submitted_at = NOW(),
			updated_at = NOW()
		WHERE id = $2 AND user_id = $3
	`, encrypted, winnerID, userID); err != nil {
		return nil, fmt.Errorf("submit activity claim info: %w", err)
	}
	updated, err := s.queryWinners(ctx, `
		SELECT `+activityWinnerSelectColumns+`
		FROM activity_winners w
		INNER JOIN activity_campaigns c ON c.id = w.campaign_id
		LEFT JOIN users u ON u.id = w.user_id
		WHERE w.id = $1
	`, true, winnerID)
	if err != nil {
		return nil, err
	}
	if len(updated) == 0 {
		return nil, ErrActivityWinnerNotFound
	}
	return &updated[0], nil
}

func (s *ActivityService) AdminListWinners(ctx context.Context, campaignID int64, page, pageSize int) ([]ActivityWinnerDTO, int64, error) {
	page, pageSize = normalizeActivityPagination(page, pageSize)
	args := []any{}
	where := "1=1"
	if campaignID > 0 {
		args = append(args, campaignID)
		where = "w.campaign_id = $1"
	}
	var total int64
	if err := s.querySingle(ctx, "SELECT COUNT(*) FROM activity_winners w WHERE "+where, args, &total); err != nil {
		return nil, 0, fmt.Errorf("count activity winners: %w", err)
	}
	args = append(args, pageSize, (page-1)*pageSize)
	query := fmt.Sprintf(`
		SELECT %s
		FROM activity_winners w
		INNER JOIN activity_campaigns c ON c.id = w.campaign_id
		LEFT JOIN users u ON u.id = w.user_id
		WHERE %s
		ORDER BY w.created_at DESC, w.id DESC
		LIMIT $%d OFFSET $%d
	`, activityWinnerSelectColumns, where, len(args)-1, len(args))
	items, err := s.queryWinners(ctx, query, true, args...)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *ActivityService) RunDueDraws(ctx context.Context, now time.Time, limit int) (*ActivityAutoDrawResult, error) {
	if s == nil || s.entClient == nil {
		return nil, infraerrors.ServiceUnavailable("ACTIVITY_SERVICE_UNAVAILABLE", "activity service unavailable")
	}
	if now.IsZero() {
		now = time.Now()
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.entClient.QueryContext(ctx, `
		SELECT c.id
		FROM activity_campaigns c
		WHERE c.status = 'active'
			AND c.draw_at IS NOT NULL
			AND c.draw_at <= $1
			AND NOT EXISTS (
				SELECT 1
				FROM activity_draws d
				WHERE d.campaign_id = c.id
					AND d.draw_at = c.draw_at
			)
		ORDER BY c.draw_at ASC, c.id ASC
		LIMIT $2
	`, now.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("query due activity draws: %w", err)
	}
	defer func() { _ = rows.Close() }()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan due activity draw: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due activity draws: %w", err)
	}
	out := &ActivityAutoDrawResult{Draws: []ActivityDrawResult{}}
	for _, id := range ids {
		result, err := s.runDraw(ctx, id, 0, false)
		if err != nil {
			if errors.Is(err, ErrActivityDrawAlreadyExists) {
				continue
			}
			return out, err
		}
		out.Processed++
		out.Draws = append(out.Draws, *result)
	}
	return out, nil
}

func (s *ActivityService) attachActivityParticipationStats(ctx context.Context, stats *ActivityCampaignStatsDTO, campaignID int64, drawAt time.Time) error {
	var firstJoinedAt, lastJoinedAt sql.NullTime
	var avgTickets float64
	if err := s.querySingle(ctx, `
		SELECT COUNT(DISTINCT user_id),
			COALESCE(SUM(ticket_count), 0)::bigint,
			COALESCE(SUM(metric_value), 0)::double precision,
			COALESCE(AVG(ticket_count), 0)::double precision,
			COALESCE(AVG(metric_value), 0)::double precision,
			COALESCE(MAX(ticket_count), 0),
			COALESCE(MAX(metric_value), 0)::double precision,
			MIN(created_at),
			MAX(created_at)
		FROM activity_entries
		WHERE campaign_id = $1
			AND draw_at = $2
			AND ticket_count > 0
	`, []any{campaignID, drawAt}, &stats.JoinedUserCount, &stats.JoinedTicketCount, &stats.JoinedMetricTotal,
		&avgTickets, &stats.AverageMetricValue, &stats.MaxTicketCount, &stats.MaxMetricValue, &firstJoinedAt, &lastJoinedAt); err != nil {
		return fmt.Errorf("query activity participation stats: %w", err)
	}
	stats.JoinedMetricTotal = normalizeActivityAmount(stats.JoinedMetricTotal)
	stats.AverageTicketsPerUser = normalizeActivityAmount(avgTickets)
	stats.AverageMetricValue = normalizeActivityAmount(stats.AverageMetricValue)
	stats.MaxMetricValue = normalizeActivityAmount(stats.MaxMetricValue)
	if firstJoinedAt.Valid {
		stats.FirstJoinedAt = &firstJoinedAt.Time
	}
	if lastJoinedAt.Valid {
		stats.LastJoinedAt = &lastJoinedAt.Time
	}
	return nil
}

func (s *ActivityService) activityDrawExists(ctx context.Context, campaignID int64, drawAt time.Time) (bool, error) {
	var count int64
	if err := s.querySingle(ctx, `
		SELECT COUNT(*)
		FROM activity_draws
		WHERE campaign_id = $1 AND draw_at = $2
	`, []any{campaignID, drawAt}, &count); err != nil {
		return false, fmt.Errorf("query activity draw existence: %w", err)
	}
	return count > 0, nil
}

func (s *ActivityService) latestActivityDrawSummary(ctx context.Context, campaignID int64) (*ActivityDrawSummaryDTO, error) {
	rows, err := s.entClient.QueryContext(ctx, `
		SELECT id, draw_at, snapshot_start_at, snapshot_end_at, status,
			total_users::bigint, total_tickets::bigint, winner_count::bigint, executed_at
		FROM activity_draws
		WHERE campaign_id = $1
		ORDER BY executed_at DESC, id DESC
		LIMIT 1
	`, campaignID)
	if err != nil {
		return nil, fmt.Errorf("query latest activity draw: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return nil, nil
	}
	var item ActivityDrawSummaryDTO
	if err := rows.Scan(&item.ID, &item.DrawAt, &item.SnapshotStartAt, &item.SnapshotEndAt, &item.Status,
		&item.TotalUsers, &item.TotalTickets, &item.WinnerCount, &item.ExecutedAt); err != nil {
		return nil, fmt.Errorf("scan latest activity draw: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate latest activity draw: %w", err)
	}
	return &item, nil
}

func (s *ActivityService) attachActivityWinnerStats(ctx context.Context, stats *ActivityCampaignStatsDTO, campaignID int64) error {
	rows, err := s.entClient.QueryContext(ctx, `
		SELECT status, claim_status, COUNT(*)::bigint
		FROM activity_winners
		WHERE campaign_id = $1
		GROUP BY status, claim_status
	`, campaignID)
	if err != nil {
		return fmt.Errorf("query activity winner stats: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var status, claimStatus string
		var count int64
		if err := rows.Scan(&status, &claimStatus, &count); err != nil {
			return fmt.Errorf("scan activity winner stats: %w", err)
		}
		stats.WinnerCount += count
		switch status {
		case ActivityWinnerStatusPendingClaim:
			stats.PendingClaimCount += count
		case ActivityWinnerStatusPendingDelivery:
			stats.PendingDeliveryCount += count
		case ActivityWinnerStatusDelivered:
			stats.DeliveredCount += count
		case ActivityWinnerStatusRejected:
			stats.RejectedCount += count
		case ActivityWinnerStatusExpired:
			stats.ExpiredCount += count
		}
		if claimStatus == ActivityClaimStatusSubmitted {
			stats.ClaimSubmittedCount += count
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate activity winner stats: %w", err)
	}
	return nil
}

func (s *ActivityService) AdminRunDraw(ctx context.Context, campaignID, operatorUserID int64) (*ActivityDrawResult, error) {
	return s.runDraw(ctx, campaignID, operatorUserID, true)
}

func (s *ActivityService) runDraw(ctx context.Context, campaignID, operatorUserID int64, requireReadyTime bool) (*ActivityDrawResult, error) {
	campaign, err := s.AdminGetCampaign(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	if campaign.Status != ActivityStatusActive {
		return nil, infraerrors.Conflict("ACTIVITY_NOT_ACTIVE", "activity campaign is not active")
	}
	if campaign.DrawAt == nil {
		return nil, infraerrors.Conflict("ACTIVITY_DRAW_TIME_REQUIRED", "activity draw time is required")
	}
	now := time.Now()
	if requireReadyTime && now.Before(*campaign.DrawAt) {
		return nil, ErrActivityDrawNotReady
	}
	period, err := activityRulePeriod(campaign, *campaign.DrawAt)
	if err != nil {
		return nil, err
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin activity draw tx: %w", err)
	}
	defer rollbackEntTx(tx)
	client, err := activityTxClient(tx)
	if err != nil {
		return nil, err
	}
	eligible, totalTickets, err := snapshotJoinedUsersForDrawInTx(ctx, client, campaign)
	if err != nil {
		return nil, err
	}
	drawID, err := createActivityDrawInTx(ctx, client, campaign, period, len(eligible), totalTickets, operatorUserID)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			return nil, ErrActivityDrawAlreadyExists
		}
		return nil, err
	}
	if err := markActivityEntriesDrawnInTx(ctx, client, drawID, campaign); err != nil {
		return nil, err
	}
	winners, err := s.drawAndCreateWinnersInTx(ctx, client, drawID, campaign, eligible)
	if err != nil {
		return nil, err
	}
	if _, err := client.ExecContext(ctx, `
		UPDATE activity_draws
		SET winner_count = $1
		WHERE id = $2
	`, len(winners), drawID); err != nil {
		return nil, fmt.Errorf("update activity draw winner count: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit activity draw tx: %w", err)
	}
	return &ActivityDrawResult{
		DrawID:       drawID,
		CampaignID:   campaign.ID,
		CampaignName: campaign.Name,
		TotalUsers:   len(eligible),
		TotalTickets: totalTickets,
		WinnerCount:  len(winners),
		Winners:      winners,
	}, nil
}

func (s *ActivityService) AdminMarkWinnerDelivered(ctx context.Context, winnerID int64, note string) (*ActivityWinnerDTO, error) {
	winner, err := s.getWinnerForAdmin(ctx, winnerID)
	if err != nil {
		return nil, err
	}
	if winner.Status == ActivityWinnerStatusPendingClaim {
		return nil, infraerrors.Conflict("ACTIVITY_WINNER_CLAIM_REQUIRED", "winner claim info has not been submitted")
	}
	if winner.Status == ActivityWinnerStatusDelivered {
		return winner, nil
	}
	if _, err := s.entClient.ExecContext(ctx, `
		UPDATE activity_winners
		SET status = 'delivered',
			delivered_at = COALESCE(delivered_at, NOW()),
			admin_note = $2,
			updated_at = NOW()
		WHERE id = $1
	`, winnerID, strings.TrimSpace(note)); err != nil {
		return nil, fmt.Errorf("mark activity winner delivered: %w", err)
	}
	return s.getWinnerForAdmin(ctx, winnerID)
}

func (s *ActivityService) AdminRejectWinner(ctx context.Context, winnerID int64, note string) (*ActivityWinnerDTO, error) {
	if winnerID <= 0 {
		return nil, ErrActivityWinnerNotFound
	}
	if _, err := s.entClient.ExecContext(ctx, `
		UPDATE activity_winners
		SET status = 'rejected',
			admin_note = $2,
			updated_at = NOW()
		WHERE id = $1
	`, winnerID, strings.TrimSpace(note)); err != nil {
		return nil, fmt.Errorf("reject activity winner: %w", err)
	}
	return s.getWinnerForAdmin(ctx, winnerID)
}

func (s *ActivityService) getWinnerForAdmin(ctx context.Context, winnerID int64) (*ActivityWinnerDTO, error) {
	items, err := s.queryWinners(ctx, `
		SELECT `+activityWinnerSelectColumns+`
		FROM activity_winners w
		INNER JOIN activity_campaigns c ON c.id = w.campaign_id
		LEFT JOIN users u ON u.id = w.user_id
		WHERE w.id = $1
	`, true, winnerID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrActivityWinnerNotFound
	}
	return &items[0], nil
}

func (s *ActivityService) computeUserProgress(ctx context.Context, campaign *ActivityCampaignDTO, userID int64, ref time.Time) (*ActivityProgressDTO, error) {
	period, err := activityRulePeriod(campaign, ref)
	if err != nil {
		return nil, err
	}
	metricValue, err := s.queryUserActivityMetric(ctx, userID, campaign.RuleConfig.Metric, period)
	if err != nil {
		return nil, err
	}
	tickets := activityTicketCount(campaign.RuleConfig, metricValue)
	nextThreshold, nextTickets := activityNextThreshold(campaign.RuleConfig, metricValue)
	progress := &ActivityProgressDTO{
		MetricType:    campaign.RuleConfig.Metric,
		MetricValue:   normalizeActivityAmount(metricValue),
		TicketCount:   tickets,
		NextThreshold: nextThreshold,
		NextTickets:   nextTickets,
		PeriodStartAt: period.start,
		PeriodEndAt:   period.end,
		DrawAt:        campaign.DrawAt,
	}
	if campaign.DrawAt != nil {
		if err := s.attachUserActivityEntry(ctx, progress, campaign.ID, userID, *campaign.DrawAt); err != nil {
			return nil, err
		}
	}
	return progress, nil
}

func (s *ActivityService) attachUserActivityEntry(ctx context.Context, progress *ActivityProgressDTO, campaignID, userID int64, drawAt time.Time) error {
	var joinedTickets int
	var joinedAt time.Time
	err := s.querySingle(ctx, `
		SELECT ticket_count, created_at
		FROM activity_entries
		WHERE campaign_id = $1 AND user_id = $2 AND draw_at = $3
	`, []any{campaignID, userID, drawAt}, &joinedTickets, &joinedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("query user activity entry: %w", err)
	}
	progress.Joined = true
	progress.JoinedTickets = joinedTickets
	progress.JoinedAt = &joinedAt
	return nil
}

func (s *ActivityService) attachActivityPublicStats(ctx context.Context, campaign *ActivityCampaignDTO) error {
	if campaign == nil || campaign.DrawAt == nil {
		return nil
	}
	mode := activityPublicParticipantMode(campaign.DisplayConfig)
	if mode == ActivityPublicParticipantCountOff {
		return nil
	}
	var count int64
	if err := s.querySingle(ctx, `
		SELECT COUNT(DISTINCT user_id)
		FROM activity_entries
		WHERE campaign_id = $1
			AND draw_at = $2
			AND ticket_count > 0
	`, []any{campaign.ID, *campaign.DrawAt}, &count); err != nil {
		return fmt.Errorf("query public activity participant count: %w", err)
	}
	publicStats := &ActivityPublicStatsDTO{ParticipantCountMode: mode}
	if mode == ActivityPublicParticipantCountExact {
		publicStats.ParticipantCount = &count
	} else {
		publicStats.ParticipantCountBucket = activityParticipantCountBucket(count)
	}
	campaign.PublicStats = publicStats
	return nil
}

func (s *ActivityService) queryUserActivityMetric(ctx context.Context, userID int64, metric string, period activityPeriod) (float64, error) {
	if metric == ActivityMetricAPIRequestCount {
		var count float64
		if err := s.querySingle(ctx, `
			SELECT COUNT(*)::double precision
			FROM usage_logs
			WHERE user_id = $1
				AND created_at >= $2
				AND created_at < $3
		`, []any{userID, period.start, period.end}, &count); err != nil {
			return 0, fmt.Errorf("query activity request count: %w", err)
		}
		return count, nil
	}
	var amount float64
	if err := s.querySingle(ctx, `
		SELECT COALESCE(SUM(actual_cost), 0)::double precision
		FROM usage_logs
		WHERE user_id = $1
			AND created_at >= $2
			AND created_at < $3
	`, []any{userID, period.start, period.end}, &amount); err != nil {
		return 0, fmt.Errorf("query activity cost amount: %w", err)
	}
	return amount, nil
}

func snapshotJoinedUsersForDrawInTx(ctx context.Context, client activitySQLClient, campaign *ActivityCampaignDTO) ([]activityEligibleUser, int, error) {
	if campaign == nil || campaign.DrawAt == nil {
		return nil, 0, ErrActivityNotFound
	}
	rows, err := client.QueryContext(ctx, `
		SELECT e.user_id, COALESCE(u.email, ''), COALESCE(u.username, ''),
			e.metric_value::double precision, e.ticket_count
		FROM activity_entries e
		INNER JOIN users u ON u.id = e.user_id
		WHERE e.campaign_id = $1
			AND e.draw_at = $2
			AND e.ticket_count > 0
			AND e.draw_id IS NULL
			AND u.deleted_at IS NULL
			AND u.status = 'active'
		ORDER BY e.ticket_count DESC, e.user_id ASC
		FOR UPDATE OF e
	`, campaign.ID, *campaign.DrawAt)
	if err != nil {
		return nil, 0, fmt.Errorf("query joined activity users: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []activityEligibleUser{}
	totalTickets := 0
	for rows.Next() {
		var item activityEligibleUser
		if err := rows.Scan(&item.userID, &item.email, &item.username, &item.metricValue, &item.tickets); err != nil {
			return nil, 0, fmt.Errorf("scan joined activity user: %w", err)
		}
		if item.tickets <= 0 {
			continue
		}
		out = append(out, item)
		totalTickets += item.tickets
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate joined activity users: %w", err)
	}
	return out, totalTickets, nil
}

func (s *ActivityService) drawAndCreateWinnersInTx(ctx context.Context, client activitySQLClient, drawID int64, campaign *ActivityCampaignDTO, eligible []activityEligibleUser) ([]ActivityWinnerDTO, error) {
	remaining := append([]activityEligibleUser(nil), eligible...)
	winners := []ActivityWinnerDTO{}
	for _, prize := range campaign.Prizes {
		if !prize.Enabled {
			continue
		}
		for i := 0; i < prize.Quantity && len(remaining) > 0; i++ {
			idx, err := pickActivityWinnerIndex(remaining)
			if err != nil {
				return nil, err
			}
			user := remaining[idx]
			remaining = append(remaining[:idx], remaining[idx+1:]...)
			winner, err := s.createWinnerInTx(ctx, client, drawID, campaign, prize, user)
			if err != nil {
				return nil, err
			}
			winners = append(winners, *winner)
		}
	}
	return winners, nil
}

func (s *ActivityService) createWinnerInTx(ctx context.Context, client activitySQLClient, drawID int64, campaign *ActivityCampaignDTO, prize ActivityPrizeDTO, user activityEligibleUser) (*ActivityWinnerDTO, error) {
	status := ActivityWinnerStatusPendingDelivery
	claimStatus := ActivityClaimStatusNotRequired
	if prize.RequiresClaimInfo {
		status = ActivityWinnerStatusPendingClaim
		claimStatus = ActivityClaimStatusPending
	}
	if prize.PrizeType == ActivityPrizeManual && !prize.RequiresClaimInfo {
		status = ActivityWinnerStatusPendingDelivery
	}
	var prizeID any
	if prize.ID > 0 {
		prizeID = prize.ID
	}
	masked := maskActivityUser(user.email, user.username, user.userID)
	var winnerID int64
	rows, err := client.QueryContext(ctx, `
		INSERT INTO activity_winners (
			campaign_id, draw_id, prize_id, user_id, prize_name, prize_type,
			prize_amount, ticket_count, masked_user, status, claim_status
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11
		)
		RETURNING id
	`, campaign.ID, drawID, prizeID, user.userID, prize.Name, prize.PrizeType, prize.Amount, user.tickets, masked, status, claimStatus)
	if err != nil {
		return nil, fmt.Errorf("create activity winner: %w", err)
	}
	if rows.Next() {
		if err := rows.Scan(&winnerID); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan activity winner id: %w", err)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close activity winner rows: %w", err)
	}
	if winnerID <= 0 {
		return nil, fmt.Errorf("create activity winner returned no id")
	}
	deliveredAt := (*time.Time)(nil)
	if status == ActivityWinnerStatusPendingDelivery && prize.PrizeType != ActivityPrizeManual {
		if err := s.deliverWinnerRewardInTx(ctx, client, winnerID, user.userID, prize); err != nil {
			return nil, err
		}
		now := time.Now()
		deliveredAt = &now
		if _, err := client.ExecContext(ctx, `
			UPDATE activity_winners
			SET status = 'delivered',
				delivered_at = $2,
				updated_at = NOW()
			WHERE id = $1
		`, winnerID, *deliveredAt); err != nil {
			return nil, fmt.Errorf("mark auto delivered activity winner: %w", err)
		}
		status = ActivityWinnerStatusDelivered
	}
	return &ActivityWinnerDTO{
		ID:           winnerID,
		CampaignID:   campaign.ID,
		CampaignName: campaign.Name,
		DrawID:       drawID,
		PrizeID:      &prize.ID,
		UserID:       user.userID,
		UserEmail:    user.email,
		UserUsername: user.username,
		PrizeName:    prize.Name,
		PrizeType:    prize.PrizeType,
		PrizeAmount:  prize.Amount,
		TicketCount:  user.tickets,
		MaskedUser:   masked,
		Status:       status,
		ClaimStatus:  claimStatus,
		DeliveredAt:  deliveredAt,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}, nil
}

func (s *ActivityService) deliverWinnerRewardInTx(ctx context.Context, client activitySQLClient, winnerID, userID int64, prize ActivityPrizeDTO) error {
	if prize.PrizeType == ActivityPrizeManual {
		return nil
	}
	if prize.Amount <= 0 {
		return infraerrors.BadRequest("ACTIVITY_PRIZE_AMOUNT_INVALID", "automatic prize amount must be greater than 0")
	}
	metadata, _ := json.Marshal(map[string]any{
		"activity_campaign_prize": prize.Name,
		"activity_prize_type":     prize.PrizeType,
	})
	switch prize.PrizeType {
	case ActivityPrizeBalance:
		var before float64
		if err := querySingleWithClient(ctx, client, `
			SELECT balance::double precision
			FROM users
			WHERE id = $1 AND deleted_at IS NULL
			FOR UPDATE
		`, []any{userID}, &before); err != nil {
			return fmt.Errorf("lock activity winner balance: %w", err)
		}
		after := normalizeActivityAmount(before + prize.Amount)
		if _, err := client.ExecContext(ctx, `
			UPDATE users SET balance = $1, updated_at = NOW() WHERE id = $2
		`, after, userID); err != nil {
			return fmt.Errorf("credit activity winner balance: %w", err)
		}
		if _, err := client.ExecContext(ctx, `
			INSERT INTO user_balance_ledger (
				user_id, direction, amount, reason, ref_type, ref_id, balance_after, metadata
			) VALUES (
				$1, 'credit', $2, $3, $4, $5, $6, $7::jsonb
			)
		`, userID, prize.Amount, ActivityRewardLedgerReason, ActivityRewardRefType, winnerID, after, string(metadata)); err != nil {
			return fmt.Errorf("create activity balance ledger: %w", err)
		}
	case ActivityPrizePoints:
		var before float64
		if err := querySingleWithClient(ctx, client, `
			SELECT points_balance::double precision
			FROM users
			WHERE id = $1 AND deleted_at IS NULL
			FOR UPDATE
		`, []any{userID}, &before); err != nil {
			return fmt.Errorf("lock activity winner points: %w", err)
		}
		after := normalizeActivityAmount(before + prize.Amount)
		if _, err := client.ExecContext(ctx, `
			UPDATE users SET points_balance = $1, updated_at = NOW() WHERE id = $2
		`, after, userID); err != nil {
			return fmt.Errorf("credit activity winner points: %w", err)
		}
		if _, err := client.ExecContext(ctx, `
			INSERT INTO points_ledger (
				user_id, direction, amount, reason, ref_type, ref_id,
				balance_before, balance_after, metadata
			) VALUES (
				$1, 'credit', $2, $3, $4, $5,
				$6, $7, $8::jsonb
			)
		`, userID, prize.Amount, ActivityRewardLedgerReason, ActivityRewardRefType, winnerID, before, after, string(metadata)); err != nil {
			return fmt.Errorf("create activity points ledger: %w", err)
		}
	case ActivityPrizeLoadFactorCredits:
		amount := int(math.Round(prize.Amount))
		if amount <= 0 {
			return infraerrors.BadRequest("ACTIVITY_PRIZE_AMOUNT_INVALID", "load factor credits prize amount must be greater than 0")
		}
		var before int
		if err := querySingleWithClient(ctx, client, `
			SELECT load_factor_credits_balance
			FROM users
			WHERE id = $1 AND deleted_at IS NULL
			FOR UPDATE
		`, []any{userID}, &before); err != nil {
			return fmt.Errorf("lock activity winner load factor credits: %w", err)
		}
		after := before + amount
		if _, err := client.ExecContext(ctx, `
			UPDATE users SET load_factor_credits_balance = $1, updated_at = NOW() WHERE id = $2
		`, after, userID); err != nil {
			return fmt.Errorf("credit activity winner load factor credits: %w", err)
		}
		if _, err := client.ExecContext(ctx, `
			INSERT INTO user_load_factor_ledger (
				user_id, account_id, direction, amount, reason, ref_type, ref_id,
				balance_before, balance_after, operator_user_id, metadata
			) VALUES (
				$1, NULL, 'credit', $2, $3, $4, $5,
				$6, $7, NULL, $8::jsonb
			)
		`, userID, amount, ActivityRewardLedgerReason, ActivityRewardRefType, winnerID, before, after, string(metadata)); err != nil {
			return fmt.Errorf("create activity load factor ledger: %w", err)
		}
	default:
		return infraerrors.BadRequest("ACTIVITY_PRIZE_TYPE_INVALID", "invalid activity prize type")
	}
	if s.billingCacheService != nil {
		_ = s.billingCacheService.InvalidateUserBalance(ctx, userID)
	}
	return nil
}

const activityCampaignSelectColumns = `
	id, type, name, description, cover_url, status, starts_at, ends_at, draw_at,
	timezone, public_enabled, sort_order, rule_config::text, display_config::text,
	created_at, updated_at
`

func (s *ActivityService) getCampaign(ctx context.Context, id int64) (*ActivityCampaignDTO, error) {
	if id <= 0 {
		return nil, ErrActivityNotFound
	}
	items, err := s.queryCampaigns(ctx, `
		SELECT `+activityCampaignSelectColumns+`
		FROM activity_campaigns
		WHERE id = $1
	`, id)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrActivityNotFound
	}
	return &items[0], nil
}

func (s *ActivityService) queryCampaigns(ctx context.Context, query string, args ...any) ([]ActivityCampaignDTO, error) {
	rows, err := s.entClient.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query activity campaigns: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []ActivityCampaignDTO{}
	for rows.Next() {
		item, err := scanActivityCampaign(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate activity campaigns: %w", err)
	}
	return out, nil
}

func scanActivityCampaign(rows *sql.Rows) (ActivityCampaignDTO, error) {
	var item ActivityCampaignDTO
	var description, coverURL sql.NullString
	var drawAt sql.NullTime
	var ruleRaw, displayRaw string
	if err := rows.Scan(
		&item.ID,
		&item.Type,
		&item.Name,
		&description,
		&coverURL,
		&item.Status,
		&item.StartsAt,
		&item.EndsAt,
		&drawAt,
		&item.Timezone,
		&item.PublicEnabled,
		&item.SortOrder,
		&ruleRaw,
		&displayRaw,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return item, fmt.Errorf("scan activity campaign: %w", err)
	}
	item.Description = nullableStringPtr(description)
	item.CoverURL = nullableStringPtr(coverURL)
	if drawAt.Valid {
		item.DrawAt = &drawAt.Time
	}
	if err := json.Unmarshal([]byte(ruleRaw), &item.RuleConfig); err != nil {
		return item, fmt.Errorf("parse activity rule config: %w", err)
	}
	item.RuleConfig = normalizeActivityRuleConfig(item.RuleConfig)
	if err := json.Unmarshal([]byte(displayRaw), &item.DisplayConfig); err != nil {
		return item, fmt.Errorf("parse activity display config: %w", err)
	}
	if item.DisplayConfig == nil {
		item.DisplayConfig = map[string]any{}
	}
	return item, nil
}

func (s *ActivityService) attachPrizes(ctx context.Context, campaigns []ActivityCampaignDTO) error {
	if len(campaigns) == 0 {
		return nil
	}
	ids := make([]any, 0, len(campaigns))
	placeholders := make([]string, 0, len(campaigns))
	indexByID := make(map[int64]int, len(campaigns))
	for i := range campaigns {
		ids = append(ids, campaigns[i].ID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
		indexByID[campaigns[i].ID] = i
	}
	rows, err := s.entClient.QueryContext(ctx, `
		SELECT id, campaign_id, name, description, prize_type, amount::double precision,
			quantity, weight::double precision, requires_claim_info, claim_fields::text,
			enabled, sort_order, created_at, updated_at
		FROM activity_prizes
		WHERE campaign_id IN (`+strings.Join(placeholders, ",")+`)
		ORDER BY sort_order ASC, id ASC
	`, ids...)
	if err != nil {
		return fmt.Errorf("query activity prizes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		prize, err := scanActivityPrize(rows)
		if err != nil {
			return err
		}
		if idx, ok := indexByID[prize.CampaignID]; ok {
			campaigns[idx].Prizes = append(campaigns[idx].Prizes, prize)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate activity prizes: %w", err)
	}
	return nil
}

func scanActivityPrize(rows *sql.Rows) (ActivityPrizeDTO, error) {
	var item ActivityPrizeDTO
	var description sql.NullString
	var claimFieldsRaw string
	if err := rows.Scan(
		&item.ID,
		&item.CampaignID,
		&item.Name,
		&description,
		&item.PrizeType,
		&item.Amount,
		&item.Quantity,
		&item.Weight,
		&item.RequiresClaimInfo,
		&claimFieldsRaw,
		&item.Enabled,
		&item.SortOrder,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return item, fmt.Errorf("scan activity prize: %w", err)
	}
	item.Description = nullableStringPtr(description)
	if err := json.Unmarshal([]byte(claimFieldsRaw), &item.ClaimFields); err != nil {
		return item, fmt.Errorf("parse activity prize claim fields: %w", err)
	}
	return item, nil
}

func replaceActivityPrizes(ctx context.Context, client activitySQLClient, campaignID int64, prizes []ActivityPrizeUpsert) error {
	if _, err := client.ExecContext(ctx, "DELETE FROM activity_prizes WHERE campaign_id = $1", campaignID); err != nil {
		return fmt.Errorf("delete activity prizes: %w", err)
	}
	for _, prize := range prizes {
		normalized, err := normalizeActivityPrizeInput(prize)
		if err != nil {
			return err
		}
		fieldsJSON, err := json.Marshal(normalized.ClaimFields)
		if err != nil {
			return fmt.Errorf("marshal activity prize claim fields: %w", err)
		}
		if _, err := client.ExecContext(ctx, `
			INSERT INTO activity_prizes (
				campaign_id, name, description, prize_type, amount, quantity, weight,
				requires_claim_info, claim_fields, enabled, sort_order
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7,
				$8, $9::jsonb, $10, $11
			)
		`, campaignID, normalized.Name, nullableStringArg(normalized.Description), normalized.PrizeType, normalized.Amount,
			normalized.Quantity, normalized.Weight, normalized.RequiresClaimInfo, string(fieldsJSON), normalized.Enabled, normalized.SortOrder); err != nil {
			return fmt.Errorf("insert activity prize: %w", err)
		}
	}
	return nil
}

func normalizeActivityCampaignInput(input ActivityCampaignUpsertInput) (ActivityCampaignUpsertInput, error) {
	input.Type = strings.TrimSpace(input.Type)
	if input.Type == "" {
		input.Type = ActivityTypeConsumptionLottery
	}
	if input.Type != ActivityTypeConsumptionLottery {
		return input, infraerrors.BadRequest("ACTIVITY_TYPE_INVALID", "invalid activity type")
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return input, infraerrors.BadRequest("ACTIVITY_NAME_REQUIRED", "activity name is required")
	}
	input.Status = strings.TrimSpace(input.Status)
	if input.Status == "" {
		input.Status = ActivityStatusDraft
	}
	if !isValidActivityStatus(input.Status) {
		return input, infraerrors.BadRequest("ACTIVITY_STATUS_INVALID", "invalid activity status")
	}
	if input.StartsAt.IsZero() || input.EndsAt.IsZero() || !input.EndsAt.After(input.StartsAt) {
		return input, infraerrors.BadRequest("ACTIVITY_WINDOW_INVALID", "activity time window is invalid")
	}
	if input.DrawAt != nil && input.DrawAt.Before(input.StartsAt) {
		return input, infraerrors.BadRequest("ACTIVITY_DRAW_TIME_INVALID", "draw time must not be before activity start")
	}
	input.Timezone = strings.TrimSpace(input.Timezone)
	if input.Timezone == "" {
		input.Timezone = "Asia/Shanghai"
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil {
		return input, infraerrors.BadRequest("ACTIVITY_TIMEZONE_INVALID", "invalid activity timezone")
	}
	input.RuleConfig = normalizeActivityRuleConfig(input.RuleConfig)
	if err := validateActivityRuleConfig(input.RuleConfig, input.StartsAt, input.EndsAt); err != nil {
		return input, err
	}
	if input.DisplayConfig == nil {
		input.DisplayConfig = map[string]any{}
	}
	if len(input.Prizes) == 0 {
		return input, infraerrors.BadRequest("ACTIVITY_PRIZE_REQUIRED", "at least one activity prize is required")
	}
	for i := range input.Prizes {
		normalized, err := normalizeActivityPrizeInput(input.Prizes[i])
		if err != nil {
			return input, err
		}
		input.Prizes[i] = normalized
	}
	return input, nil
}

func normalizeActivityRuleConfig(rule ActivityRuleConfig) ActivityRuleConfig {
	rule.Metric = strings.TrimSpace(rule.Metric)
	if rule.Metric == "" {
		rule.Metric = ActivityMetricAPICostAmount
	}
	rule.PeriodType = strings.TrimSpace(rule.PeriodType)
	if rule.PeriodType == "" {
		rule.PeriodType = ActivityPeriodCampaign
	}
	rule.TicketMode = strings.TrimSpace(rule.TicketMode)
	if rule.TicketMode == "" {
		rule.TicketMode = ActivityTicketModeFixed
	}
	if rule.FixedTickets <= 0 {
		rule.FixedTickets = 1
	}
	if rule.TicketsPerUnit <= 0 {
		rule.TicketsPerUnit = 1
	}
	if rule.TierMode == "" {
		rule.TierMode = ActivityTierModeHighest
	}
	sort.Slice(rule.Tiers, func(i, j int) bool {
		return rule.Tiers[i].Threshold < rule.Tiers[j].Threshold
	})
	return rule
}

func validateActivityRuleConfig(rule ActivityRuleConfig, campaignStart, campaignEnd time.Time) error {
	switch rule.Metric {
	case ActivityMetricAPICostAmount, ActivityMetricAPIRequestCount:
	default:
		return infraerrors.BadRequest("ACTIVITY_METRIC_INVALID", "invalid activity metric")
	}
	switch rule.PeriodType {
	case ActivityPeriodCampaign:
		if campaignStart.IsZero() || campaignEnd.IsZero() || !campaignEnd.After(campaignStart) {
			return infraerrors.BadRequest("ACTIVITY_PERIOD_INVALID", "campaign period is invalid")
		}
	case ActivityPeriodToday:
	case ActivityPeriodRollingDays:
		if rule.RollingDays <= 0 || rule.RollingDays > 365 {
			return infraerrors.BadRequest("ACTIVITY_ROLLING_DAYS_INVALID", "rolling days must be between 1 and 365")
		}
	case ActivityPeriodFixedRange:
		if rule.PeriodStartAt == nil || rule.PeriodEndAt == nil || !rule.PeriodEndAt.After(*rule.PeriodStartAt) {
			return infraerrors.BadRequest("ACTIVITY_PERIOD_INVALID", "fixed activity period is invalid")
		}
	default:
		return infraerrors.BadRequest("ACTIVITY_PERIOD_TYPE_INVALID", "invalid activity period type")
	}
	if rule.Threshold < 0 || rule.MaxTicketsPerUser < 0 {
		return infraerrors.BadRequest("ACTIVITY_RULE_INVALID", "activity thresholds must be non-negative")
	}
	switch rule.TicketMode {
	case ActivityTicketModeFixed:
		if rule.FixedTickets <= 0 {
			return infraerrors.BadRequest("ACTIVITY_TICKETS_INVALID", "fixed tickets must be greater than 0")
		}
	case ActivityTicketModeProportional:
		if rule.UnitAmount <= 0 || rule.TicketsPerUnit <= 0 {
			return infraerrors.BadRequest("ACTIVITY_TICKETS_INVALID", "unit amount and tickets per unit must be greater than 0")
		}
	case ActivityTicketModeTiered:
		if len(rule.Tiers) == 0 {
			return infraerrors.BadRequest("ACTIVITY_TIERS_REQUIRED", "tiered activity rule requires tiers")
		}
		if rule.TierMode != ActivityTierModeHighest && rule.TierMode != ActivityTierModeCumulative {
			return infraerrors.BadRequest("ACTIVITY_TIER_MODE_INVALID", "invalid activity tier mode")
		}
		for _, tier := range rule.Tiers {
			if tier.Threshold < 0 || tier.Tickets <= 0 {
				return infraerrors.BadRequest("ACTIVITY_TIERS_INVALID", "activity tiers must use non-negative thresholds and positive tickets")
			}
		}
	default:
		return infraerrors.BadRequest("ACTIVITY_TICKET_MODE_INVALID", "invalid activity ticket mode")
	}
	return nil
}

func normalizeActivityPrizeInput(input ActivityPrizeUpsert) (ActivityPrizeUpsert, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return input, infraerrors.BadRequest("ACTIVITY_PRIZE_NAME_REQUIRED", "activity prize name is required")
	}
	input.PrizeType = strings.TrimSpace(input.PrizeType)
	if !isValidActivityPrizeType(input.PrizeType) {
		return input, infraerrors.BadRequest("ACTIVITY_PRIZE_TYPE_INVALID", "invalid activity prize type")
	}
	if input.PrizeType != ActivityPrizeManual && input.Amount <= 0 {
		return input, infraerrors.BadRequest("ACTIVITY_PRIZE_AMOUNT_INVALID", "automatic activity prize amount must be greater than 0")
	}
	if input.PrizeType == ActivityPrizeManual && input.Amount < 0 {
		return input, infraerrors.BadRequest("ACTIVITY_PRIZE_AMOUNT_INVALID", "activity prize amount must be non-negative")
	}
	if input.Quantity <= 0 {
		input.Quantity = 1
	}
	if input.Weight <= 0 {
		input.Weight = 1
	}
	for i := range input.ClaimFields {
		input.ClaimFields[i].Key = strings.TrimSpace(input.ClaimFields[i].Key)
		input.ClaimFields[i].Label = strings.TrimSpace(input.ClaimFields[i].Label)
		input.ClaimFields[i].Type = strings.TrimSpace(input.ClaimFields[i].Type)
		if input.ClaimFields[i].Key == "" || input.ClaimFields[i].Label == "" {
			return input, infraerrors.BadRequest("ACTIVITY_CLAIM_FIELD_INVALID", "claim field key and label are required")
		}
	}
	return input, nil
}

func validateActivityClaimInfo(fields []ActivityClaimField, info map[string]any) error {
	if info == nil {
		info = map[string]any{}
	}
	for _, field := range fields {
		if !field.Required {
			continue
		}
		value, ok := info[field.Key]
		if !ok || value == nil {
			return infraerrors.BadRequest("ACTIVITY_CLAIM_INFO_REQUIRED", fmt.Sprintf("claim field %s is required", field.Key))
		}
		if str, ok := value.(string); ok && strings.TrimSpace(str) == "" {
			return infraerrors.BadRequest("ACTIVITY_CLAIM_INFO_REQUIRED", fmt.Sprintf("claim field %s is required", field.Key))
		}
	}
	return nil
}

func marshalActivityConfig(rule ActivityRuleConfig, display map[string]any) ([]byte, []byte, error) {
	ruleJSON, err := json.Marshal(rule)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal activity rule config: %w", err)
	}
	if display == nil {
		display = map[string]any{}
	}
	displayJSON, err := json.Marshal(display)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal activity display config: %w", err)
	}
	return ruleJSON, displayJSON, nil
}

func activityProgressReferenceTime(campaign *ActivityCampaignDTO, now time.Time) time.Time {
	if campaign == nil {
		return now
	}
	if campaign.DrawAt != nil && now.After(*campaign.DrawAt) {
		return *campaign.DrawAt
	}
	if now.After(campaign.EndsAt) {
		return campaign.EndsAt
	}
	return now
}

func activityRulePeriod(campaign *ActivityCampaignDTO, ref time.Time) (activityPeriod, error) {
	if campaign == nil {
		return activityPeriod{}, ErrActivityNotFound
	}
	rule := campaign.RuleConfig
	loc := mustActivityLocation(campaign.Timezone)
	if ref.IsZero() {
		ref = time.Now()
	}
	localRef := ref.In(loc)
	switch rule.PeriodType {
	case ActivityPeriodFixedRange:
		if rule.PeriodStartAt == nil || rule.PeriodEndAt == nil || !rule.PeriodEndAt.After(*rule.PeriodStartAt) {
			return activityPeriod{}, infraerrors.BadRequest("ACTIVITY_PERIOD_INVALID", "fixed activity period is invalid")
		}
		return activityPeriod{start: rule.PeriodStartAt.UTC(), end: rule.PeriodEndAt.UTC()}, nil
	case ActivityPeriodToday:
		start := time.Date(localRef.Year(), localRef.Month(), localRef.Day(), 0, 0, 0, 0, loc)
		return activityPeriod{start: start.UTC(), end: start.AddDate(0, 0, 1).UTC()}, nil
	case ActivityPeriodRollingDays:
		days := rule.RollingDays
		if days <= 0 {
			return activityPeriod{}, infraerrors.BadRequest("ACTIVITY_ROLLING_DAYS_INVALID", "rolling days must be greater than 0")
		}
		end := localRef
		start := end.AddDate(0, 0, -days)
		return activityPeriod{start: start.UTC(), end: end.UTC()}, nil
	case ActivityPeriodCampaign:
		return activityPeriod{start: campaign.StartsAt.UTC(), end: campaign.EndsAt.UTC()}, nil
	default:
		return activityPeriod{}, infraerrors.BadRequest("ACTIVITY_PERIOD_TYPE_INVALID", "invalid activity period type")
	}
}

func activityTicketCount(rule ActivityRuleConfig, metricValue float64) int {
	if metricValue < rule.Threshold {
		return 0
	}
	tickets := 0
	switch rule.TicketMode {
	case ActivityTicketModeFixed:
		tickets = rule.FixedTickets
	case ActivityTicketModeProportional:
		tickets = int(math.Floor(metricValue/rule.UnitAmount)) * rule.TicketsPerUnit
	case ActivityTicketModeTiered:
		for _, tier := range rule.Tiers {
			if metricValue < tier.Threshold {
				continue
			}
			if rule.TierMode == ActivityTierModeCumulative {
				tickets += tier.Tickets
			} else {
				tickets = tier.Tickets
			}
		}
	}
	if rule.MaxTicketsPerUser > 0 && tickets > rule.MaxTicketsPerUser {
		tickets = rule.MaxTicketsPerUser
	}
	if tickets < 0 {
		return 0
	}
	return tickets
}

func activityNextThreshold(rule ActivityRuleConfig, metricValue float64) (*float64, *int) {
	if rule.TicketMode == ActivityTicketModeTiered {
		for _, tier := range rule.Tiers {
			if metricValue < tier.Threshold {
				threshold := tier.Threshold
				tickets := tier.Tickets
				return &threshold, &tickets
			}
		}
		return nil, nil
	}
	if metricValue < rule.Threshold {
		threshold := rule.Threshold
		tickets := rule.FixedTickets
		if rule.TicketMode == ActivityTicketModeProportional {
			tickets = rule.TicketsPerUnit
		}
		return &threshold, &tickets
	}
	if rule.TicketMode == ActivityTicketModeProportional && rule.UnitAmount > 0 {
		next := math.Floor(metricValue/rule.UnitAmount+1) * rule.UnitAmount
		tickets := activityTicketCount(rule, next)
		if rule.MaxTicketsPerUser > 0 && activityTicketCount(rule, metricValue) >= rule.MaxTicketsPerUser {
			return nil, nil
		}
		return &next, &tickets
	}
	return nil, nil
}

func createActivityDrawInTx(ctx context.Context, client activitySQLClient, campaign *ActivityCampaignDTO, period activityPeriod, totalUsers, totalTickets int, operatorUserID int64) (int64, error) {
	seed, err := randomActivitySeed()
	if err != nil {
		return 0, err
	}
	var operatorArg any
	if operatorUserID > 0 {
		operatorArg = operatorUserID
	}
	var drawID int64
	rows, err := client.QueryContext(ctx, `
		INSERT INTO activity_draws (
			campaign_id, draw_at, snapshot_start_at, snapshot_end_at,
			status, total_users, total_tickets, winner_count, seed, executed_by_user_id
		) VALUES (
			$1, $2, $3, $4,
			'completed', $5, $6, 0, $7, $8
		)
		RETURNING id
	`, campaign.ID, *campaign.DrawAt, period.start, period.end, totalUsers, totalTickets, seed, operatorArg)
	if err != nil {
		return 0, fmt.Errorf("create activity draw: %w", err)
	}
	if rows.Next() {
		if err := rows.Scan(&drawID); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("scan activity draw id: %w", err)
		}
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close activity draw rows: %w", err)
	}
	if drawID <= 0 {
		return 0, fmt.Errorf("create activity draw returned no id")
	}
	return drawID, nil
}

func markActivityEntriesDrawnInTx(ctx context.Context, client activitySQLClient, drawID int64, campaign *ActivityCampaignDTO) error {
	if campaign == nil || campaign.DrawAt == nil {
		return ErrActivityNotFound
	}
	if _, err := client.ExecContext(ctx, `
		UPDATE activity_entries e
		SET draw_id = $1,
			updated_at = NOW()
		FROM users u
		WHERE e.user_id = u.id
			AND e.campaign_id = $2
			AND e.draw_at = $3
			AND e.ticket_count > 0
			AND e.draw_id IS NULL
			AND u.deleted_at IS NULL
			AND u.status = 'active'
	`, drawID, campaign.ID, *campaign.DrawAt); err != nil {
		return fmt.Errorf("mark activity entries drawn: %w", err)
	}
	return nil
}

func pickActivityWinnerIndex(items []activityEligibleUser) (int, error) {
	total := 0
	for _, item := range items {
		if item.tickets > 0 {
			total += item.tickets
		}
	}
	if total <= 0 {
		return -1, errors.New("activity winner pool has no tickets")
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(total)))
	if err != nil {
		return -1, fmt.Errorf("generate activity draw random: %w", err)
	}
	target := int(n.Int64())
	acc := 0
	for i, item := range items {
		acc += item.tickets
		if target < acc {
			return i, nil
		}
	}
	return len(items) - 1, nil
}

func randomActivitySeed() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate activity draw seed: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

const activityWinnerSelectColumns = `
	w.id, w.campaign_id, c.name, w.draw_id, w.prize_id, w.user_id,
	COALESCE(u.email, ''), COALESCE(u.username, ''),
	w.prize_name, w.prize_type, w.prize_amount::double precision,
	COALESCE((SELECT p.claim_fields::text FROM activity_prizes p WHERE p.id = w.prize_id), '[]'),
	w.ticket_count, w.masked_user, w.status, w.claim_status,
	w.claim_info_encrypted, w.claim_submitted_at, w.delivered_at, w.admin_note,
	w.created_at, w.updated_at
`

func (s *ActivityService) queryWinners(ctx context.Context, query string, decryptClaim bool, args ...any) ([]ActivityWinnerDTO, error) {
	rows, err := s.entClient.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query activity winners: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []ActivityWinnerDTO{}
	for rows.Next() {
		item, encrypted, err := scanActivityWinner(rows)
		if err != nil {
			return nil, err
		}
		if decryptClaim && encrypted.Valid && strings.TrimSpace(encrypted.String) != "" {
			info, err := s.decryptClaimInfo(encrypted.String)
			if err != nil {
				return nil, err
			}
			item.ClaimInfo = info
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate activity winners: %w", err)
	}
	return out, nil
}

func scanActivityWinner(rows *sql.Rows) (ActivityWinnerDTO, sql.NullString, error) {
	var item ActivityWinnerDTO
	var prizeID sql.NullInt64
	var encrypted sql.NullString
	var claimFieldsRaw string
	var claimSubmittedAt, deliveredAt sql.NullTime
	var adminNote sql.NullString
	if err := rows.Scan(
		&item.ID,
		&item.CampaignID,
		&item.CampaignName,
		&item.DrawID,
		&prizeID,
		&item.UserID,
		&item.UserEmail,
		&item.UserUsername,
		&item.PrizeName,
		&item.PrizeType,
		&item.PrizeAmount,
		&claimFieldsRaw,
		&item.TicketCount,
		&item.MaskedUser,
		&item.Status,
		&item.ClaimStatus,
		&encrypted,
		&claimSubmittedAt,
		&deliveredAt,
		&adminNote,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return item, encrypted, fmt.Errorf("scan activity winner: %w", err)
	}
	if prizeID.Valid {
		item.PrizeID = &prizeID.Int64
	}
	if err := json.Unmarshal([]byte(claimFieldsRaw), &item.ClaimFields); err != nil {
		return item, encrypted, fmt.Errorf("decode activity winner claim fields: %w", err)
	}
	if claimSubmittedAt.Valid {
		item.ClaimSubmittedAt = &claimSubmittedAt.Time
	}
	if deliveredAt.Valid {
		item.DeliveredAt = &deliveredAt.Time
	}
	item.AdminNote = nullableStringPtr(adminNote)
	return item, encrypted, nil
}

func (s *ActivityService) encryptClaimInfo(info map[string]any) (string, error) {
	if info == nil {
		info = map[string]any{}
	}
	raw, err := json.Marshal(info)
	if err != nil {
		return "", fmt.Errorf("marshal activity claim info: %w", err)
	}
	if s.encryptor == nil {
		return "", infraerrors.ServiceUnavailable("ACTIVITY_CLAIM_ENCRYPTOR_UNAVAILABLE", "claim info encryptor is unavailable")
	}
	encrypted, err := s.encryptor.Encrypt(string(raw))
	if err != nil {
		return "", fmt.Errorf("encrypt activity claim info: %w", err)
	}
	return encrypted, nil
}

func (s *ActivityService) decryptClaimInfo(encrypted string) (map[string]any, error) {
	if s.encryptor == nil {
		return nil, infraerrors.ServiceUnavailable("ACTIVITY_CLAIM_ENCRYPTOR_UNAVAILABLE", "claim info encryptor is unavailable")
	}
	raw, err := s.encryptor.Decrypt(encrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt activity claim info: %w", err)
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		return nil, fmt.Errorf("parse activity claim info: %w", err)
	}
	if info == nil {
		info = map[string]any{}
	}
	return info, nil
}

func (s *ActivityService) querySingle(ctx context.Context, query string, args []any, dest ...any) error {
	return querySingleWithClient(ctx, s.entClient, query, args, dest...)
}

func querySingleWithClient(ctx context.Context, client activitySQLClient, query string, args []any, dest ...any) error {
	rows, err := client.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return sql.ErrNoRows
	}
	if err := rows.Scan(dest...); err != nil {
		return err
	}
	return rows.Err()
}

func normalizeActivityPagination(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func isValidActivityStatus(status string) bool {
	switch status {
	case ActivityStatusDraft, ActivityStatusActive, ActivityStatusPaused, ActivityStatusEnded:
		return true
	default:
		return false
	}
}

func isValidActivityPrizeType(prizeType string) bool {
	switch prizeType {
	case ActivityPrizeBalance, ActivityPrizePoints, ActivityPrizeLoadFactorCredits, ActivityPrizeManual:
		return true
	default:
		return false
	}
}

func activityTxClient(tx *dbent.Tx) (activitySQLClient, error) {
	if tx == nil {
		return nil, errors.New("activity transaction is nil")
	}
	client := tx.Client()
	if client == nil {
		return nil, errors.New("activity transaction client is nil")
	}
	var sqlClient activitySQLClient = client
	return sqlClient, nil
}

func rollbackEntTx(tx *dbent.Tx) {
	if tx != nil {
		_ = tx.Rollback()
	}
}

func nullableStringArg(ptr *string) any {
	if ptr == nil {
		return nil
	}
	value := strings.TrimSpace(*ptr)
	if value == "" {
		return nil
	}
	return value
}

func nullableTimeArg(ptr *time.Time) any {
	if ptr == nil || ptr.IsZero() {
		return nil
	}
	return ptr.UTC()
}

func nullableStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	v := value.String
	return &v
}

func mustActivityLocation(name string) *time.Location {
	loc, err := time.LoadLocation(strings.TrimSpace(name))
	if err == nil {
		return loc
	}
	loc, err = time.LoadLocation("Asia/Shanghai")
	if err == nil {
		return loc
	}
	return time.FixedZone("Asia/Shanghai", 8*60*60)
}

func normalizeActivityAmount(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return math.Round(value*100) / 100
}

func activityPublicParticipantMode(display map[string]any) string {
	if display == nil {
		return ActivityPublicParticipantCountOff
	}
	raw, ok := display["public_participant_count"].(string)
	if !ok {
		return ActivityPublicParticipantCountOff
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ActivityPublicParticipantCountFuzzy:
		return ActivityPublicParticipantCountFuzzy
	case ActivityPublicParticipantCountExact:
		return ActivityPublicParticipantCountExact
	default:
		return ActivityPublicParticipantCountOff
	}
}

func activityParticipantCountBucket(count int64) string {
	switch {
	case count <= 0:
		return ""
	case count < 10:
		return "1+"
	case count < 50:
		return fmt.Sprintf("%d+", (count/10)*10)
	case count < 100:
		return "50+"
	case count < 1000:
		return fmt.Sprintf("%d+", (count/100)*100)
	default:
		return "1000+"
	}
}

func activityDrawReadiness(campaign *ActivityCampaignDTO, drawn bool, now time.Time) (bool, string) {
	if campaign == nil {
		return false, "not_found"
	}
	if campaign.Status != ActivityStatusActive {
		return false, "not_active"
	}
	if campaign.DrawAt == nil {
		return false, "draw_time_missing"
	}
	if now.Before(*campaign.DrawAt) {
		return false, "waiting_draw_time"
	}
	if drawn {
		return false, "already_drawn"
	}
	return true, ""
}

func maskActivityUser(email, username string, userID int64) string {
	email = strings.TrimSpace(email)
	if email != "" {
		parts := strings.SplitN(email, "@", 2)
		prefix := parts[0]
		if len(prefix) <= 2 {
			prefix = prefix[:1] + "***"
		} else {
			prefix = prefix[:2] + "***"
		}
		if len(parts) == 2 {
			return prefix + "@" + parts[1]
		}
		return prefix
	}
	username = strings.TrimSpace(username)
	if username != "" {
		runes := []rune(username)
		if len(runes) <= 2 {
			return string(runes[:1]) + "***"
		}
		return string(runes[:2]) + "***"
	}
	return fmt.Sprintf("用户 %d***", userID)
}
