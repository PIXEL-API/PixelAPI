package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

var (
	ErrUsageLogNotFound = infraerrors.NotFound("USAGE_LOG_NOT_FOUND", "usage log not found")
)

// CreateUsageLogRequest 创建使用日志请求
type CreateUsageLogRequest struct {
	UserID                int64   `json:"user_id"`
	APIKeyID              int64   `json:"api_key_id"`
	AccountID             int64   `json:"account_id"`
	RequestID             string  `json:"request_id"`
	Model                 string  `json:"model"`
	InputTokens           int     `json:"input_tokens"`
	OutputTokens          int     `json:"output_tokens"`
	CacheCreationTokens   int     `json:"cache_creation_tokens"`
	CacheReadTokens       int     `json:"cache_read_tokens"`
	CacheCreation5mTokens int     `json:"cache_creation_5m_tokens"`
	CacheCreation1hTokens int     `json:"cache_creation_1h_tokens"`
	InputCost             float64 `json:"input_cost"`
	OutputCost            float64 `json:"output_cost"`
	CacheCreationCost     float64 `json:"cache_creation_cost"`
	CacheReadCost         float64 `json:"cache_read_cost"`
	TotalCost             float64 `json:"total_cost"`
	ActualCost            float64 `json:"actual_cost"`
	RateMultiplier        float64 `json:"rate_multiplier"`
	Stream                bool    `json:"stream"`
	DurationMs            *int    `json:"duration_ms"`
}

// UsageStats 使用统计
type UsageStats struct {
	TotalRequests            int64   `json:"total_requests"`
	TotalInputTokens         int64   `json:"total_input_tokens"`
	TotalOutputTokens        int64   `json:"total_output_tokens"`
	TotalCacheTokens         int64   `json:"total_cache_tokens"`
	TotalCacheCreationTokens int64   `json:"total_cache_creation_tokens"`
	TotalCacheReadTokens     int64   `json:"total_cache_read_tokens"`
	TotalTokens              int64   `json:"total_tokens"`
	TotalCost                float64 `json:"total_cost"`
	TotalActualCost          float64 `json:"total_actual_cost"`
	AverageDurationMs        float64 `json:"average_duration_ms"`
}

// UsageService 使用统计服务
type UsageService struct {
	usageRepo            UsageLogRepository
	userRepo             UserRepository
	entClient            *dbent.Client
	authCacheInvalidator APIKeyAuthCacheInvalidator
}

// NewUsageService 创建使用统计服务实例
func NewUsageService(usageRepo UsageLogRepository, userRepo UserRepository, entClient *dbent.Client, authCacheInvalidator APIKeyAuthCacheInvalidator) *UsageService {
	return &UsageService{
		usageRepo:            usageRepo,
		userRepo:             userRepo,
		entClient:            entClient,
		authCacheInvalidator: authCacheInvalidator,
	}
}

// Create 创建使用日志
func (s *UsageService) Create(ctx context.Context, req CreateUsageLogRequest) (*UsageLog, error) {
	// 使用数据库事务保证「使用日志插入」与「扣费」的原子性，避免重复扣费或漏扣风险。
	tx, err := s.entClient.Tx(ctx)
	if err != nil && !errors.Is(err, dbent.ErrTxStarted) {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}

	txCtx := ctx
	if err == nil {
		defer func() { _ = tx.Rollback() }()
		txCtx = dbent.NewTxContext(ctx, tx)
	}

	// 验证用户存在
	_, err = s.userRepo.GetByID(txCtx, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}

	// 创建使用日志
	usageLog := &UsageLog{
		UserID:                req.UserID,
		APIKeyID:              req.APIKeyID,
		AccountID:             req.AccountID,
		RequestID:             req.RequestID,
		Model:                 req.Model,
		InputTokens:           req.InputTokens,
		OutputTokens:          req.OutputTokens,
		CacheCreationTokens:   req.CacheCreationTokens,
		CacheReadTokens:       req.CacheReadTokens,
		CacheCreation5mTokens: req.CacheCreation5mTokens,
		CacheCreation1hTokens: req.CacheCreation1hTokens,
		InputCost:             req.InputCost,
		OutputCost:            req.OutputCost,
		CacheCreationCost:     req.CacheCreationCost,
		CacheReadCost:         req.CacheReadCost,
		TotalCost:             req.TotalCost,
		ActualCost:            req.ActualCost,
		RateMultiplier:        req.RateMultiplier,
		Stream:                req.Stream,
		DurationMs:            req.DurationMs,
	}

	inserted, err := s.usageRepo.Create(txCtx, usageLog)
	if err != nil {
		return nil, fmt.Errorf("create usage log: %w", err)
	}

	// 扣除用户余额
	balanceUpdated := false
	if inserted && req.ActualCost > 0 {
		if err := s.userRepo.UpdateBalance(txCtx, req.UserID, -req.ActualCost); err != nil {
			return nil, fmt.Errorf("update user balance: %w", err)
		}
		balanceUpdated = true
	}

	if tx != nil {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit transaction: %w", err)
		}
	}

	s.invalidateUsageCaches(ctx, req.UserID, balanceUpdated)

	return usageLog, nil
}

func (s *UsageService) invalidateUsageCaches(ctx context.Context, userID int64, balanceUpdated bool) {
	if !balanceUpdated || s.authCacheInvalidator == nil {
		return
	}
	s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
}

// GetByID 根据ID获取使用日志
func (s *UsageService) GetByID(ctx context.Context, id int64) (*UsageLog, error) {
	log, err := s.usageRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get usage log: %w", err)
	}
	return log, nil
}

// ListByUser 获取用户的使用日志列表
func (s *UsageService) ListByUser(ctx context.Context, userID int64, params pagination.PaginationParams) ([]UsageLog, *pagination.PaginationResult, error) {
	logs, pagination, err := s.usageRepo.ListByUser(ctx, userID, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list usage logs: %w", err)
	}
	return logs, pagination, nil
}

// ListByAPIKey 获取API Key的使用日志列表
func (s *UsageService) ListByAPIKey(ctx context.Context, apiKeyID int64, params pagination.PaginationParams) ([]UsageLog, *pagination.PaginationResult, error) {
	logs, pagination, err := s.usageRepo.ListByAPIKey(ctx, apiKeyID, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list usage logs: %w", err)
	}
	return logs, pagination, nil
}

// ListByAccount 获取账号的使用日志列表
func (s *UsageService) ListByAccount(ctx context.Context, accountID int64, params pagination.PaginationParams) ([]UsageLog, *pagination.PaginationResult, error) {
	logs, pagination, err := s.usageRepo.ListByAccount(ctx, accountID, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list usage logs: %w", err)
	}
	return logs, pagination, nil
}

// GetStatsByUser 获取用户的使用统计
func (s *UsageService) GetStatsByUser(ctx context.Context, userID int64, startTime, endTime time.Time) (*UsageStats, error) {
	stats, err := s.usageRepo.GetUserStatsAggregated(ctx, userID, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("get user stats: %w", err)
	}

	return &UsageStats{
		TotalRequests:            stats.TotalRequests,
		TotalInputTokens:         stats.TotalInputTokens,
		TotalOutputTokens:        stats.TotalOutputTokens,
		TotalCacheTokens:         stats.TotalCacheTokens,
		TotalCacheCreationTokens: stats.TotalCacheCreationTokens,
		TotalCacheReadTokens:     stats.TotalCacheReadTokens,
		TotalTokens:              stats.TotalTokens,
		TotalCost:                stats.TotalCost,
		TotalActualCost:          stats.TotalActualCost,
		AverageDurationMs:        stats.AverageDurationMs,
	}, nil
}

// GetStatsByAPIKey 获取API Key的使用统计
func (s *UsageService) GetStatsByAPIKey(ctx context.Context, apiKeyID int64, startTime, endTime time.Time) (*UsageStats, error) {
	stats, err := s.usageRepo.GetAPIKeyStatsAggregated(ctx, apiKeyID, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("get api key stats: %w", err)
	}

	return &UsageStats{
		TotalRequests:            stats.TotalRequests,
		TotalInputTokens:         stats.TotalInputTokens,
		TotalOutputTokens:        stats.TotalOutputTokens,
		TotalCacheTokens:         stats.TotalCacheTokens,
		TotalCacheCreationTokens: stats.TotalCacheCreationTokens,
		TotalCacheReadTokens:     stats.TotalCacheReadTokens,
		TotalTokens:              stats.TotalTokens,
		TotalCost:                stats.TotalCost,
		TotalActualCost:          stats.TotalActualCost,
		AverageDurationMs:        stats.AverageDurationMs,
	}, nil
}

// GetStatsByAccount 获取账号的使用统计
func (s *UsageService) GetStatsByAccount(ctx context.Context, accountID int64, startTime, endTime time.Time) (*UsageStats, error) {
	stats, err := s.usageRepo.GetAccountStatsAggregated(ctx, accountID, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("get account stats: %w", err)
	}

	return &UsageStats{
		TotalRequests:            stats.TotalRequests,
		TotalInputTokens:         stats.TotalInputTokens,
		TotalOutputTokens:        stats.TotalOutputTokens,
		TotalCacheTokens:         stats.TotalCacheTokens,
		TotalCacheCreationTokens: stats.TotalCacheCreationTokens,
		TotalCacheReadTokens:     stats.TotalCacheReadTokens,
		TotalTokens:              stats.TotalTokens,
		TotalCost:                stats.TotalCost,
		TotalActualCost:          stats.TotalActualCost,
		AverageDurationMs:        stats.AverageDurationMs,
	}, nil
}

// GetStatsByModel 获取模型的使用统计
func (s *UsageService) GetStatsByModel(ctx context.Context, modelName string, startTime, endTime time.Time) (*UsageStats, error) {
	stats, err := s.usageRepo.GetModelStatsAggregated(ctx, modelName, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("get model stats: %w", err)
	}

	return &UsageStats{
		TotalRequests:            stats.TotalRequests,
		TotalInputTokens:         stats.TotalInputTokens,
		TotalOutputTokens:        stats.TotalOutputTokens,
		TotalCacheTokens:         stats.TotalCacheTokens,
		TotalCacheCreationTokens: stats.TotalCacheCreationTokens,
		TotalCacheReadTokens:     stats.TotalCacheReadTokens,
		TotalTokens:              stats.TotalTokens,
		TotalCost:                stats.TotalCost,
		TotalActualCost:          stats.TotalActualCost,
		AverageDurationMs:        stats.AverageDurationMs,
	}, nil
}

// GetDailyStats 获取每日使用统计（最近N天）
func (s *UsageService) GetDailyStats(ctx context.Context, userID int64, days int) ([]map[string]any, error) {
	endTime := time.Now()
	startTime := endTime.AddDate(0, 0, -days)

	stats, err := s.usageRepo.GetDailyStatsAggregated(ctx, userID, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("get daily stats: %w", err)
	}

	return stats, nil
}

// Delete 删除使用日志（管理员功能，谨慎使用）
func (s *UsageService) Delete(ctx context.Context, id int64) error {
	if err := s.usageRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete usage log: %w", err)
	}
	return nil
}

// GetUserDashboardStats returns per-user dashboard summary stats.
func (s *UsageService) GetUserDashboardStats(ctx context.Context, userID int64) (*usagestats.UserDashboardStats, error) {
	stats, err := s.usageRepo.GetUserDashboardStats(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user dashboard stats: %w", err)
	}
	return stats, nil
}

// GetAPIKeyDashboardStats returns dashboard summary stats filtered by API Key.
func (s *UsageService) GetAPIKeyDashboardStats(ctx context.Context, apiKeyID int64) (*usagestats.UserDashboardStats, error) {
	stats, err := s.usageRepo.GetAPIKeyDashboardStats(ctx, apiKeyID)
	if err != nil {
		return nil, fmt.Errorf("get api key dashboard stats: %w", err)
	}
	return stats, nil
}

// GetUserUsageTrendByUserID returns per-user usage trend.
func (s *UsageService) GetUserUsageTrendByUserID(ctx context.Context, userID int64, startTime, endTime time.Time, granularity string) ([]usagestats.TrendDataPoint, error) {
	trend, err := s.usageRepo.GetUserUsageTrendByUserID(ctx, userID, startTime, endTime, granularity)
	if err != nil {
		return nil, fmt.Errorf("get user usage trend: %w", err)
	}
	return trend, nil
}

// GetUserModelStats returns per-user model usage stats.
func (s *UsageService) GetUserModelStats(ctx context.Context, userID int64, startTime, endTime time.Time) ([]usagestats.ModelStat, error) {
	stats, err := s.usageRepo.GetUserModelStats(ctx, userID, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("get user model stats: %w", err)
	}
	return stats, nil
}

// GetUserAccountSharingDashboard returns owned-account consumption and public-sharing settlement stats.
func (s *UsageService) GetUserAccountSharingDashboard(ctx context.Context, userID int64, startTime, endTime time.Time, granularity string) (*usagestats.AccountSharingDashboardStats, error) {
	stats, err := s.usageRepo.GetUserAccountSharingDashboard(ctx, userID, startTime, endTime, granularity)
	if err != nil {
		return nil, fmt.Errorf("get user account sharing dashboard: %w", err)
	}
	return stats, nil
}

// GetAPIKeyModelStats returns per-model usage stats for a specific API Key.
func (s *UsageService) GetAPIKeyModelStats(ctx context.Context, apiKeyID int64, startTime, endTime time.Time) ([]usagestats.ModelStat, error) {
	stats, err := s.usageRepo.GetModelStatsWithFilters(ctx, startTime, endTime, 0, apiKeyID, 0, 0, nil, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("get api key model stats: %w", err)
	}
	return stats, nil
}

// GetBatchAPIKeyUsageStats returns today/total actual_cost for given api keys.
func (s *UsageService) GetBatchAPIKeyUsageStats(ctx context.Context, apiKeyIDs []int64, startTime, endTime time.Time) (map[int64]*usagestats.BatchAPIKeyUsageStats, error) {
	stats, err := s.usageRepo.GetBatchAPIKeyUsageStats(ctx, apiKeyIDs, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("get batch api key usage stats: %w", err)
	}
	return stats, nil
}

// ListWithFilters lists usage logs with admin filters.
func (s *UsageService) ListWithFilters(ctx context.Context, params pagination.PaginationParams, filters usagestats.UsageLogFilters) ([]UsageLog, *pagination.PaginationResult, error) {
	logs, result, err := s.usageRepo.ListWithFilters(ctx, params, filters)
	if err != nil {
		return nil, nil, fmt.Errorf("list usage logs with filters: %w", err)
	}
	return logs, result, nil
}

func (s *UsageService) ListBalanceLedger(ctx context.Context, params pagination.PaginationParams, filters UserBalanceLedgerFilters) ([]UserBalanceLedgerEntry, *pagination.PaginationResult, error) {
	if s == nil || s.entClient == nil {
		return nil, nil, infraerrors.InternalServer("USAGE_BALANCE_LEDGER_UNAVAILABLE", "usage balance ledger is unavailable")
	}
	if filters.RequireUserID && filters.UserID <= 0 {
		return nil, nil, infraerrors.BadRequest("USER_ID_INVALID", "user_id must be positive")
	}

	direction := strings.ToLower(strings.TrimSpace(filters.Direction))
	if direction != "" && direction != "debit" && direction != "credit" {
		return nil, nil, infraerrors.BadRequest("BALANCE_LEDGER_DIRECTION_INVALID", "direction must be debit or credit")
	}
	reason := strings.TrimSpace(filters.Reason)
	refType := strings.TrimSpace(filters.RefType)

	whereParts := make([]string, 0, 7)
	args := make([]any, 0, 7)
	addArg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if filters.UserID > 0 {
		whereParts = append(whereParts, "l.user_id = "+addArg(filters.UserID))
	}
	if direction != "" {
		whereParts = append(whereParts, "l.direction = "+addArg(direction))
	}
	if reason != "" {
		whereParts = append(whereParts, "l.reason = "+addArg(reason))
	}
	if refType != "" {
		whereParts = append(whereParts, "l.ref_type = "+addArg(refType))
	}
	if filters.RefID != nil {
		whereParts = append(whereParts, "l.ref_id = "+addArg(*filters.RefID))
	}
	if filters.StartTime != nil {
		whereParts = append(whereParts, "l.created_at >= "+addArg(*filters.StartTime))
	}
	if filters.EndTime != nil {
		whereParts = append(whereParts, "l.created_at < "+addArg(*filters.EndTime))
	}
	if len(whereParts) == 0 {
		whereParts = append(whereParts, "TRUE")
	}

	whereSQL := strings.Join(whereParts, " AND ")
	if filters.ExactTotal {
		return s.listBalanceLedgerWithExactTotal(ctx, params, whereSQL, args)
	}

	return s.listBalanceLedgerWithFastPagination(ctx, params, whereSQL, args)
}

func (s *UsageService) listBalanceLedgerWithExactTotal(ctx context.Context, params pagination.PaginationParams, whereSQL string, args []any) ([]UserBalanceLedgerEntry, *pagination.PaginationResult, error) {
	var total int64
	countRows, err := s.entClient.QueryContext(ctx, "SELECT COUNT(*) FROM user_balance_ledger l WHERE "+whereSQL, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("count balance ledger: %w", err)
	}
	defer func() { _ = countRows.Close() }()
	if !countRows.Next() {
		return nil, nil, fmt.Errorf("count balance ledger: no rows")
	}
	if err := countRows.Scan(&total); err != nil {
		return nil, nil, fmt.Errorf("count balance ledger: %w", err)
	}
	if err := countRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("count balance ledger: %w", err)
	}
	entries, err := s.listBalanceLedgerByQuery(ctx, params, whereSQL, args, params.Limit(), params.Offset())
	if err != nil {
		return nil, nil, err
	}
	return entries, usagePaginationResultFromTotal(total, params), nil
}

func (s *UsageService) listBalanceLedgerWithFastPagination(ctx context.Context, params pagination.PaginationParams, whereSQL string, args []any) ([]UserBalanceLedgerEntry, *pagination.PaginationResult, error) {
	limit := params.Limit()
	offset := params.Offset()
	limitWithProbe := limit + 1

	entries, err := s.listBalanceLedgerByQuery(ctx, params, whereSQL, args, limitWithProbe, offset)
	if err != nil {
		return nil, nil, err
	}

	hasMore := false
	if len(entries) > limit {
		hasMore = true
		entries = entries[:limit]
	}

	total := int64(offset) + int64(len(entries))
	if hasMore {
		total = int64(offset) + int64(limit) + 1
	}

	return entries, usagePaginationResultFromTotal(total, params), nil
}

func (s *UsageService) listBalanceLedgerByQuery(
	ctx context.Context,
	params pagination.PaginationParams,
	whereSQL string,
	args []any,
	limit int,
	offset int,
) ([]UserBalanceLedgerEntry, error) {
	sortOrder := params.NormalizedSortOrder(pagination.SortOrderDesc)
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, limit, offset)
	rows, err := s.entClient.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			l.id,
			l.user_id,
			l.direction,
			l.amount::text,
			l.reason,
			l.ref_type,
			l.ref_id,
			l.balance_after::text,
			l.metadata,
			l.created_at,
			u.email,
			u.username,
			u.status
		FROM user_balance_ledger l
		LEFT JOIN users u ON u.id = l.user_id
		WHERE %s
		ORDER BY l.created_at %s, l.id %s
		LIMIT $%d OFFSET $%d
		`, whereSQL, strings.ToUpper(sortOrder), strings.ToUpper(sortOrder), len(queryArgs)-1, len(queryArgs)), queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("list balance ledger: %w", err)
	}
	defer func() { _ = rows.Close() }()

	entries := make([]UserBalanceLedgerEntry, 0, limit)
	for rows.Next() {
		var entry UserBalanceLedgerEntry
		var refID sql.NullInt64
		var rawMetadata []byte
		var userEmail sql.NullString
		var username sql.NullString
		var userStatus sql.NullString
		if err := rows.Scan(
			&entry.ID,
			&entry.UserID,
			&entry.Direction,
			&entry.Amount,
			&entry.Reason,
			&entry.RefType,
			&refID,
			&entry.BalanceAfter,
			&rawMetadata,
			&entry.CreatedAt,
			&userEmail,
			&username,
			&userStatus,
		); err != nil {
			return nil, fmt.Errorf("scan balance ledger: %w", err)
		}
		if refID.Valid {
			value := refID.Int64
			entry.RefID = &value
		}
		entry.Metadata = map[string]any{}
		if len(rawMetadata) > 0 {
			if err := json.Unmarshal(rawMetadata, &entry.Metadata); err != nil {
				return nil, fmt.Errorf("decode balance ledger metadata: %w", err)
			}
		}
		if userEmail.Valid || username.Valid || userStatus.Valid {
			entry.User = &User{ID: entry.UserID}
			if userEmail.Valid {
				entry.User.Email = userEmail.String
			}
			if username.Valid {
				entry.User.Username = username.String
			}
			if userStatus.Valid {
				entry.User.Status = userStatus.String
			}
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate balance ledger: %w", err)
	}
	return entries, nil
}

func usagePaginationResultFromTotal(total int64, params pagination.PaginationParams) *pagination.PaginationResult {
	limit := params.Limit()
	pages := int(total) / limit
	if int(total)%limit > 0 {
		pages++
	}
	if pages < 1 {
		pages = 1
	}
	if params.Page < 1 {
		params.Page = 1
	}
	return &pagination.PaginationResult{
		Total:    total,
		Page:     params.Page,
		PageSize: limit,
		Pages:    pages,
	}
}

// GetGlobalStats returns global usage stats for a time range.
func (s *UsageService) GetGlobalStats(ctx context.Context, startTime, endTime time.Time) (*usagestats.UsageStats, error) {
	stats, err := s.usageRepo.GetGlobalStats(ctx, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("get global usage stats: %w", err)
	}
	return stats, nil
}

// GetStatsWithFilters returns usage stats with optional filters.
func (s *UsageService) GetStatsWithFilters(ctx context.Context, filters usagestats.UsageLogFilters) (*usagestats.UsageStats, error) {
	stats, err := s.usageRepo.GetStatsWithFilters(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("get usage stats with filters: %w", err)
	}
	return stats, nil
}
