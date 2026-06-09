package service

import (
	"archive/zip"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	revenueShareSettlementExportStatusPending   = "pending"
	revenueShareSettlementExportStatusRunning   = "running"
	revenueShareSettlementExportStatusCompleted = "completed"
	revenueShareSettlementExportStatusFailed    = "failed"
	revenueShareSettlementExportStatusCanceled  = "canceled"

	revenueShareSettlementExportBatchSize       = 2000
	revenueShareSettlementExportRowsPerFile     = 100000
	revenueShareSettlementExportMaxRows         = 1000000
	revenueShareSettlementExportRetention       = 72 * time.Hour
	revenueShareSettlementExportStaleAfter      = 2 * time.Hour
	revenueShareSettlementExportCleanupBatchMax = 50
)

const revenueShareSettlementSelectColumns = `
	ase.id,
	ase.usage_log_id,
	ase.request_id,
	ase.api_key_id,
	COALESCE(NULLIF(ak.name, ''), '') AS api_key_name,
	ase.consumer_user_id,
	COALESCE(NULLIF(cu.email, ''), 'unknown') AS consumer_email,
	COALESCE(NULLIF(cu.username, ''), '') AS consumer_username,
	ase.owner_user_id,
	COALESCE(NULLIF(ou.email, ''), 'unknown') AS owner_email,
	COALESCE(NULLIF(ou.username, ''), '') AS owner_username,
	ase.inviter_user_id,
	COALESCE(NULLIF(iu.email, ''), '') AS inviter_email,
	COALESCE(NULLIF(iu.username, ''), '') AS inviter_username,
	ase.account_id,
	COALESCE(NULLIF(a.name, ''), CONCAT('Account #', ase.account_id::text)) AS account_name,
	COALESCE(NULLIF(a.platform, ''), '') AS account_platform,
	ase.group_id,
	COALESCE(NULLIF(g.name, ''), '') AS group_name,
	ase.policy_id,
	ase.policy_version,
	ase.share_mode_snapshot,
	ase.share_status_snapshot,
	ase.consumer_charge::double precision,
	ase.account_cost::double precision,
	ase.owner_share_ratio::double precision,
	ase.owner_credit::double precision,
	ase.invite_bound_at_snapshot,
	ase.invite_expires_at_snapshot,
	ase.invite_share_ratio::double precision,
	ase.invite_credit::double precision,
	ase.platform_share_ratio::double precision,
	ase.platform_fee::double precision,
	(ase.platform_fee - ase.account_cost)::double precision AS platform_net_profit,
	ase.status,
	ase.created_at
`

var (
	revenueShareSettlementExportRunner int32
	errRevenueShareSettlementCanceled  = errors.New("revenue share settlement export canceled")
)

type RevenueShareSettlementExportParams struct {
	StartTime time.Time
	EndTime   time.Time
	Search    string
	Status    string
	Timezone  string
}

type revenueShareSettlementExportFilters struct {
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Search    string `json:"search,omitempty"`
	Status    string `json:"status,omitempty"`
	Timezone  string `json:"timezone,omitempty"`
}

type RevenueShareSettlementExportTask struct {
	ID            int64      `json:"id"`
	CreatedBy     int64      `json:"created_by"`
	Status        string     `json:"status"`
	TotalRows     int64      `json:"total_rows"`
	ExportedRows  int64      `json:"exported_rows"`
	FileCount     int        `json:"file_count"`
	FileName      string     `json:"file_name,omitempty"`
	FileSizeBytes int64      `json:"file_size_bytes"`
	ErrorMessage  string     `json:"error_message,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	CanceledAt    *time.Time `json:"canceled_at,omitempty"`

	Filters  revenueShareSettlementExportFilters `json:"filters"`
	filePath string
}

type RevenueShareSettlementExportDownload struct {
	Path        string
	FileName    string
	ContentType string
	Size        int64
}

func (s *RevenueService) CreateShareSettlementExport(ctx context.Context, params RevenueShareSettlementExportParams, createdBy int64) (*RevenueShareSettlementExportTask, error) {
	if s == nil || s.entClient == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "REVENUE_SERVICE_UNAVAILABLE", "revenue service is unavailable")
	}
	if createdBy <= 0 {
		return nil, infraerrors.New(http.StatusUnauthorized, "REVENUE_EXPORT_UNAUTHORIZED", "operator is required")
	}
	normalized, status, err := normalizeRevenueShareSettlementExportParams(params)
	if err != nil {
		return nil, err
	}
	_ = s.failStaleShareSettlementExports(ctx)
	_ = s.cleanupExpiredShareSettlementExports(ctx)

	activeID, err := s.findActiveShareSettlementExport(ctx, createdBy)
	if err != nil {
		return nil, err
	}
	if activeID > 0 {
		return nil, infraerrors.New(http.StatusConflict, "REVENUE_EXPORT_ALREADY_RUNNING", "a share settlement export is already pending or running")
	}

	where, args := buildRevenueShareSettlementWhere(RevenueShareSettlementQueryParams{
		StartTime: normalized.StartTime,
		EndTime:   normalized.EndTime,
		Search:    normalized.Search,
	}, status)
	var totalRows int64
	if err := s.querySingle(ctx, revenueShareSettlementCountQuery(normalized.Search, where), args, &totalRows); err != nil {
		return nil, fmt.Errorf("count revenue share settlements for export: %w", err)
	}
	if totalRows > revenueShareSettlementExportMaxRows {
		return nil, infraerrors.BadRequest("REVENUE_EXPORT_TOO_LARGE", "share settlement export exceeds maximum row count")
	}

	filters := revenueShareSettlementExportFilters{
		StartTime: normalized.StartTime.UTC().Format(time.RFC3339Nano),
		EndTime:   normalized.EndTime.UTC().Format(time.RFC3339Nano),
		Search:    normalized.Search,
		Status:    status,
		Timezone:  normalizeRevenueTimezoneForExport(normalized.Timezone),
	}
	filterBytes, err := json.Marshal(filters)
	if err != nil {
		return nil, fmt.Errorf("marshal revenue export filters: %w", err)
	}

	query := `
		INSERT INTO revenue_share_settlement_export_tasks (
			created_by,
			status,
			filters,
			total_rows,
			exported_rows,
			file_count,
			expires_at
		)
		VALUES ($1, $2, $3::jsonb, $4, 0, 0, $5)
		RETURNING
			id, created_by, status, filters, total_rows, exported_rows, file_count,
			COALESCE(file_path, ''), COALESCE(file_name, ''), file_size_bytes, COALESCE(error_message, ''),
			created_at, started_at, completed_at, expires_at, canceled_at
	`
	task, err := s.queryShareSettlementExportTask(ctx, query, createdBy, revenueShareSettlementExportStatusPending, string(filterBytes), totalRows, time.Now().Add(revenueShareSettlementExportRetention))
	if err != nil {
		return nil, fmt.Errorf("create revenue share settlement export task: %w", err)
	}
	s.scheduleShareSettlementExportQueue()
	return task, nil
}

func (s *RevenueService) GetShareSettlementExportTask(ctx context.Context, taskID, createdBy int64) (*RevenueShareSettlementExportTask, error) {
	if s == nil || s.entClient == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "REVENUE_SERVICE_UNAVAILABLE", "revenue service is unavailable")
	}
	if taskID <= 0 {
		return nil, infraerrors.BadRequest("REVENUE_EXPORT_TASK_INVALID", "invalid export task id")
	}
	task, err := s.queryShareSettlementExportTask(ctx, `
		SELECT
			id, created_by, status, filters, total_rows, exported_rows, file_count,
			COALESCE(file_path, ''), COALESCE(file_name, ''), file_size_bytes, COALESCE(error_message, ''),
			created_at, started_at, completed_at, expires_at, canceled_at
		FROM revenue_share_settlement_export_tasks
		WHERE id = $1 AND created_by = $2
	`, taskID, createdBy)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, infraerrors.New(http.StatusNotFound, "REVENUE_EXPORT_TASK_NOT_FOUND", "export task not found")
		}
		return nil, fmt.Errorf("get revenue share settlement export task: %w", err)
	}
	return task, nil
}

func (s *RevenueService) CancelShareSettlementExport(ctx context.Context, taskID, createdBy int64) (*RevenueShareSettlementExportTask, error) {
	if s == nil || s.entClient == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "REVENUE_SERVICE_UNAVAILABLE", "revenue service is unavailable")
	}
	if taskID <= 0 {
		return nil, infraerrors.BadRequest("REVENUE_EXPORT_TASK_INVALID", "invalid export task id")
	}
	task, err := s.queryShareSettlementExportTask(ctx, `
		UPDATE revenue_share_settlement_export_tasks
		SET status = $3,
			canceled_at = NOW(),
			completed_at = COALESCE(completed_at, NOW()),
			updated_at = NOW()
		WHERE id = $1
			AND created_by = $2
			AND status IN ($4, $5)
		RETURNING
			id, created_by, status, filters, total_rows, exported_rows, file_count,
			COALESCE(file_path, ''), COALESCE(file_name, ''), file_size_bytes, COALESCE(error_message, ''),
			created_at, started_at, completed_at, expires_at, canceled_at
	`, taskID, createdBy, revenueShareSettlementExportStatusCanceled, revenueShareSettlementExportStatusPending, revenueShareSettlementExportStatusRunning)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, infraerrors.New(http.StatusConflict, "REVENUE_EXPORT_CANCEL_CONFLICT", "export task cannot be canceled")
		}
		return nil, fmt.Errorf("cancel revenue share settlement export task: %w", err)
	}
	return task, nil
}

func (s *RevenueService) GetShareSettlementExportDownload(ctx context.Context, taskID, createdBy int64) (*RevenueShareSettlementExportDownload, error) {
	task, err := s.GetShareSettlementExportTask(ctx, taskID, createdBy)
	if err != nil {
		return nil, err
	}
	if task.Status != revenueShareSettlementExportStatusCompleted {
		return nil, infraerrors.New(http.StatusConflict, "REVENUE_EXPORT_NOT_READY", "export task is not completed")
	}
	if task.ExpiresAt != nil && time.Now().After(*task.ExpiresAt) {
		return nil, infraerrors.New(http.StatusGone, "REVENUE_EXPORT_EXPIRED", "export file has expired")
	}
	if strings.TrimSpace(task.filePath) == "" || strings.TrimSpace(task.FileName) == "" {
		return nil, infraerrors.New(http.StatusGone, "REVENUE_EXPORT_FILE_MISSING", "export file is missing")
	}
	root, err := revenueShareSettlementExportRoot()
	if err != nil {
		return nil, err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve revenue export root: %w", err)
	}
	absPath, err := filepath.Abs(task.filePath)
	if err != nil {
		return nil, fmt.Errorf("resolve revenue export path: %w", err)
	}
	if absPath != absRoot && !strings.HasPrefix(absPath, absRoot+string(os.PathSeparator)) {
		return nil, infraerrors.New(http.StatusInternalServerError, "REVENUE_EXPORT_PATH_INVALID", "export file path is invalid")
	}
	stat, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, infraerrors.New(http.StatusGone, "REVENUE_EXPORT_FILE_MISSING", "export file is missing")
		}
		return nil, fmt.Errorf("stat revenue export file: %w", err)
	}
	contentType := "application/gzip"
	if strings.HasSuffix(task.FileName, ".zip") {
		contentType = "application/zip"
	}
	return &RevenueShareSettlementExportDownload{
		Path:        absPath,
		FileName:    task.FileName,
		ContentType: contentType,
		Size:        stat.Size(),
	}, nil
}

func normalizeRevenueShareSettlementExportParams(params RevenueShareSettlementExportParams) (RevenueShareSettlementExportParams, string, error) {
	if params.StartTime.IsZero() || params.EndTime.IsZero() {
		return params, "", infraerrors.BadRequest("REVENUE_TIME_RANGE_REQUIRED", "start_time and end_time are required")
	}
	if !params.EndTime.After(params.StartTime) {
		return params, "", infraerrors.BadRequest("REVENUE_TIME_RANGE_INVALID", "end_time must be after start_time")
	}
	if params.EndTime.Sub(params.StartTime) > maxRevenueLiveRange {
		return params, "", infraerrors.BadRequest("REVENUE_TIME_RANGE_TOO_LARGE", "date range exceeds 3 days")
	}
	status, err := normalizeRevenueSettlementStatus(params.Status)
	if err != nil {
		return params, "", infraerrors.BadRequest("REVENUE_SHARE_STATUS_INVALID", "invalid share settlement status")
	}
	params.Search = strings.TrimSpace(params.Search)
	params.Timezone = normalizeRevenueTimezoneForExport(params.Timezone)
	return params, status, nil
}

func normalizeRevenueTimezoneForExport(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return revenueSnapshotBusinessTimezone
	}
	return strings.TrimSpace(raw)
}

func revenueShareSettlementCountQuery(search string, where string) string {
	countJoins := ""
	if strings.TrimSpace(search) != "" {
		countJoins = `
		LEFT JOIN users cu ON cu.id = ase.consumer_user_id
		LEFT JOIN users ou ON ou.id = ase.owner_user_id
		LEFT JOIN accounts a ON a.id = ase.account_id
		LEFT JOIN api_keys ak ON ak.id = ase.api_key_id
		`
	}
	return `
		SELECT COUNT(*)
		FROM account_share_settlement_entries ase
		` + countJoins + `
		WHERE ` + where
}

func scanRevenueShareSettlementItems(rows *sql.Rows, capacity int) ([]RevenueShareSettlementItem, error) {
	items := make([]RevenueShareSettlementItem, 0, capacity)
	for rows.Next() {
		var (
			item       RevenueShareSettlementItem
			usageLogID sql.NullInt64
			groupID    sql.NullInt64
			policyID   sql.NullInt64
			inviterID  sql.NullInt64
			boundAt    sql.NullTime
			expiresAt  sql.NullTime
		)
		if err := rows.Scan(
			&item.ID,
			&usageLogID,
			&item.RequestID,
			&item.APIKeyID,
			&item.APIKeyName,
			&item.ConsumerUserID,
			&item.ConsumerEmail,
			&item.ConsumerUsername,
			&item.OwnerUserID,
			&item.OwnerEmail,
			&item.OwnerUsername,
			&inviterID,
			&item.InviterEmail,
			&item.InviterUsername,
			&item.AccountID,
			&item.AccountName,
			&item.AccountPlatform,
			&groupID,
			&item.GroupName,
			&policyID,
			&item.PolicyVersion,
			&item.ShareModeSnapshot,
			&item.ShareStatusSnapshot,
			&item.ConsumerCharge,
			&item.AccountCost,
			&item.OwnerShareRatio,
			&item.OwnerCredit,
			&boundAt,
			&expiresAt,
			&item.InviteShareRatio,
			&item.InviteCredit,
			&item.PlatformShareRatio,
			&item.PlatformFee,
			&item.PlatformNetProfit,
			&item.Status,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan revenue share settlement: %w", err)
		}
		if usageLogID.Valid {
			v := usageLogID.Int64
			item.UsageLogID = &v
		}
		if groupID.Valid {
			v := groupID.Int64
			item.GroupID = &v
		}
		if policyID.Valid {
			v := policyID.Int64
			item.PolicyID = &v
		}
		if inviterID.Valid {
			v := inviterID.Int64
			item.InviterUserID = &v
		}
		if boundAt.Valid {
			v := boundAt.Time
			item.InviteBoundAt = &v
		}
		if expiresAt.Valid {
			v := expiresAt.Time
			item.InviteExpiresAt = &v
		}
		item.ConsumerCharge = roundRevenue(item.ConsumerCharge)
		item.AccountCost = roundRevenue(item.AccountCost)
		item.OwnerShareRatio = roundRevenue(item.OwnerShareRatio)
		item.OwnerCredit = roundRevenue(item.OwnerCredit)
		item.InviteShareRatio = roundRevenue(item.InviteShareRatio)
		item.InviteCredit = roundRevenue(item.InviteCredit)
		item.PlatformShareRatio = roundRevenue(item.PlatformShareRatio)
		item.PlatformFee = roundRevenue(item.PlatformFee)
		item.PlatformNetProfit = roundRevenue(item.PlatformNetProfit)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate revenue share settlements: %w", err)
	}
	return items, nil
}

func (s *RevenueService) queryShareSettlementExportTask(ctx context.Context, query string, args ...any) (*RevenueShareSettlementExportTask, error) {
	rows, err := s.entClient.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, sql.ErrNoRows
	}
	task, err := scanRevenueShareSettlementExportTask(rows)
	if err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return task, nil
}

func scanRevenueShareSettlementExportTask(rows *sql.Rows) (*RevenueShareSettlementExportTask, error) {
	var (
		task          RevenueShareSettlementExportTask
		filtersRaw    []byte
		filePath      string
		startedAt     sql.NullTime
		completedAt   sql.NullTime
		expiresAt     sql.NullTime
		canceledAt    sql.NullTime
		errorMessage  string
		fileName      string
		fileSizeBytes int64
	)
	if err := rows.Scan(
		&task.ID,
		&task.CreatedBy,
		&task.Status,
		&filtersRaw,
		&task.TotalRows,
		&task.ExportedRows,
		&task.FileCount,
		&filePath,
		&fileName,
		&fileSizeBytes,
		&errorMessage,
		&task.CreatedAt,
		&startedAt,
		&completedAt,
		&expiresAt,
		&canceledAt,
	); err != nil {
		return nil, err
	}
	if len(filtersRaw) > 0 {
		if err := json.Unmarshal(filtersRaw, &task.Filters); err != nil {
			return nil, fmt.Errorf("unmarshal revenue export filters: %w", err)
		}
	}
	task.filePath = filePath
	task.FileName = fileName
	task.FileSizeBytes = fileSizeBytes
	task.ErrorMessage = errorMessage
	if startedAt.Valid {
		v := startedAt.Time
		task.StartedAt = &v
	}
	if completedAt.Valid {
		v := completedAt.Time
		task.CompletedAt = &v
	}
	if expiresAt.Valid {
		v := expiresAt.Time
		task.ExpiresAt = &v
	}
	if canceledAt.Valid {
		v := canceledAt.Time
		task.CanceledAt = &v
	}
	return &task, nil
}

func (s *RevenueService) findActiveShareSettlementExport(ctx context.Context, createdBy int64) (int64, error) {
	var id int64
	err := s.querySingle(ctx, `
		SELECT COALESCE((
			SELECT id
			FROM revenue_share_settlement_export_tasks
			WHERE created_by = $1 AND status IN ($2, $3)
			ORDER BY created_at ASC
			LIMIT 1
		), 0)
	`, []any{createdBy, revenueShareSettlementExportStatusPending, revenueShareSettlementExportStatusRunning}, &id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (s *RevenueService) scheduleShareSettlementExportQueue() {
	if !atomic.CompareAndSwapInt32(&revenueShareSettlementExportRunner, 0, 1) {
		return
	}
	go func() {
		defer atomic.StoreInt32(&revenueShareSettlementExportRunner, 0)
		for {
			task, ok, err := s.claimNextShareSettlementExport(context.Background())
			if err != nil || !ok {
				return
			}
			s.runShareSettlementExportTask(context.Background(), task)
		}
	}()
}

func (s *RevenueService) claimNextShareSettlementExport(ctx context.Context) (*RevenueShareSettlementExportTask, bool, error) {
	task, err := s.queryShareSettlementExportTask(ctx, `
		UPDATE revenue_share_settlement_export_tasks
		SET status = $1,
			started_at = NOW(),
			updated_at = NOW()
		WHERE id = (
			SELECT id
			FROM revenue_share_settlement_export_tasks
			WHERE status = $2
			ORDER BY created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING
			id, created_by, status, filters, total_rows, exported_rows, file_count,
			COALESCE(file_path, ''), COALESCE(file_name, ''), file_size_bytes, COALESCE(error_message, ''),
			created_at, started_at, completed_at, expires_at, canceled_at
	`, revenueShareSettlementExportStatusRunning, revenueShareSettlementExportStatusPending)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return task, true, nil
}

func (s *RevenueService) runShareSettlementExportTask(ctx context.Context, task *RevenueShareSettlementExportTask) {
	filePath, fileName, fileCount, exportedRows, err := s.generateShareSettlementExportFile(ctx, task)
	if err != nil {
		_ = os.Remove(filePath)
		if errors.Is(err, errRevenueShareSettlementCanceled) {
			_ = s.markShareSettlementExportCanceled(ctx, task.ID, exportedRows)
			return
		}
		_ = s.markShareSettlementExportFailed(ctx, task.ID, exportedRows, err)
		return
	}
	stat, statErr := os.Stat(filePath)
	if statErr != nil {
		_ = s.markShareSettlementExportFailed(ctx, task.ID, exportedRows, statErr)
		return
	}
	_ = s.markShareSettlementExportCompleted(ctx, task.ID, exportedRows, fileCount, filePath, fileName, stat.Size())
}

func (s *RevenueService) generateShareSettlementExportFile(ctx context.Context, task *RevenueShareSettlementExportTask) (string, string, int, int64, error) {
	params, err := task.Filters.toParams()
	if err != nil {
		return "", "", 0, 0, err
	}
	root, err := revenueShareSettlementExportRoot()
	if err != nil {
		return "", "", 0, 0, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", "", 0, 0, fmt.Errorf("create revenue export dir: %w", err)
	}
	loc := loadRevenueLocation(params.Timezone)
	baseName := fmt.Sprintf("share-settlements-%s-to-%s-%d", exportDateLabel(params.StartTime, loc), exportDateLabel(params.EndTime.Add(-time.Nanosecond), loc), task.ID)
	tmpPath := filepath.Join(root, baseName+".tmp")

	if task.TotalRows <= revenueShareSettlementExportRowsPerFile {
		fileName := baseName + ".csv.gz"
		finalPath := filepath.Join(root, fileName)
		exportedRows, err := s.writeShareSettlementExportGzip(ctx, tmpPath, params, task.ID)
		if err != nil {
			_ = os.Remove(tmpPath)
			return tmpPath, fileName, 1, exportedRows, err
		}
		if err := os.Rename(tmpPath, finalPath); err != nil {
			_ = os.Remove(tmpPath)
			return tmpPath, fileName, 1, exportedRows, fmt.Errorf("finalize revenue export file: %w", err)
		}
		return finalPath, fileName, 1, exportedRows, nil
	}

	fileName := baseName + ".zip"
	finalPath := filepath.Join(root, fileName)
	fileCount, exportedRows, err := s.writeShareSettlementExportZip(ctx, tmpPath, baseName, params, task.ID)
	if err != nil {
		_ = os.Remove(tmpPath)
		return tmpPath, fileName, fileCount, exportedRows, err
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return tmpPath, fileName, fileCount, exportedRows, fmt.Errorf("finalize revenue export archive: %w", err)
	}
	return finalPath, fileName, fileCount, exportedRows, nil
}

func (f revenueShareSettlementExportFilters) toParams() (RevenueShareSettlementExportParams, error) {
	start, err := time.Parse(time.RFC3339Nano, f.StartTime)
	if err != nil {
		return RevenueShareSettlementExportParams{}, fmt.Errorf("parse export start time: %w", err)
	}
	end, err := time.Parse(time.RFC3339Nano, f.EndTime)
	if err != nil {
		return RevenueShareSettlementExportParams{}, fmt.Errorf("parse export end time: %w", err)
	}
	return RevenueShareSettlementExportParams{
		StartTime: start,
		EndTime:   end,
		Search:    f.Search,
		Status:    f.Status,
		Timezone:  f.Timezone,
	}, nil
}

func (s *RevenueService) writeShareSettlementExportGzip(ctx context.Context, path string, params RevenueShareSettlementExportParams, taskID int64) (int64, error) {
	file, err := os.Create(path)
	if err != nil {
		return 0, fmt.Errorf("create revenue export gzip: %w", err)
	}
	defer func() { _ = file.Close() }()
	gz := gzip.NewWriter(file)
	defer func() { _ = gz.Close() }()
	if err := copyWithBOM(gz); err != nil {
		return 0, err
	}
	writer := csv.NewWriter(gz)
	if err := writeRevenueShareSettlementCSVHeader(writer); err != nil {
		return 0, err
	}
	exportedRows, err := s.writeShareSettlementExportRows(ctx, writer, params, taskID)
	writer.Flush()
	if flushErr := writer.Error(); flushErr != nil && err == nil {
		err = flushErr
	}
	if closeErr := gz.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	return exportedRows, err
}

func (s *RevenueService) writeShareSettlementExportZip(ctx context.Context, path, baseName string, params RevenueShareSettlementExportParams, taskID int64) (int, int64, error) {
	file, err := os.Create(path)
	if err != nil {
		return 0, 0, fmt.Errorf("create revenue export zip: %w", err)
	}
	defer func() { _ = file.Close() }()
	zipWriter := zip.NewWriter(file)
	defer func() { _ = zipWriter.Close() }()

	var exportedRows int64
	fileCount := 0
	var writer *csv.Writer
	var currentPartRows int
	openPart := func() error {
		if writer != nil {
			writer.Flush()
			if err := writer.Error(); err != nil {
				return err
			}
		}
		fileCount++
		currentPartRows = 0
		part, err := zipWriter.Create(fmt.Sprintf("%s-part-%03d.csv", baseName, fileCount))
		if err != nil {
			return err
		}
		if _, err := part.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
			return err
		}
		writer = csv.NewWriter(part)
		return writeRevenueShareSettlementCSVHeader(writer)
	}

	loc := loadRevenueLocation(params.Timezone)
	var (
		cursorCreatedAt *time.Time
		cursorID        int64
	)
	for {
		if err := s.ensureShareSettlementExportActive(ctx, taskID); err != nil {
			return fileCount, exportedRows, err
		}
		items, err := s.queryShareSettlementExportBatch(ctx, params, cursorCreatedAt, cursorID, revenueShareSettlementExportBatchSize)
		if err != nil {
			return fileCount, exportedRows, err
		}
		if len(items) == 0 {
			break
		}
		for i := range items {
			if writer == nil || currentPartRows >= revenueShareSettlementExportRowsPerFile {
				if err := openPart(); err != nil {
					return fileCount, exportedRows, err
				}
			}
			if err := writeRevenueShareSettlementCSVRow(writer, &items[i], loc); err != nil {
				return fileCount, exportedRows, err
			}
			exportedRows++
			currentPartRows++
		}
		last := items[len(items)-1]
		cursorCreatedAt = &last.CreatedAt
		cursorID = last.ID
		if err := s.updateShareSettlementExportProgress(ctx, taskID, exportedRows); err != nil {
			return fileCount, exportedRows, err
		}
	}
	if writer != nil {
		writer.Flush()
		if flushErr := writer.Error(); flushErr != nil && err == nil {
			err = flushErr
		}
	}
	if closeErr := zipWriter.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	return fileCount, exportedRows, err
}

func (s *RevenueService) writeShareSettlementExportRows(ctx context.Context, writer *csv.Writer, params RevenueShareSettlementExportParams, taskID int64) (int64, error) {
	loc := loadRevenueLocation(params.Timezone)
	var (
		cursorCreatedAt *time.Time
		cursorID        int64
		exportedRows    int64
	)
	for {
		if err := s.ensureShareSettlementExportActive(ctx, taskID); err != nil {
			return exportedRows, err
		}
		items, err := s.queryShareSettlementExportBatch(ctx, params, cursorCreatedAt, cursorID, revenueShareSettlementExportBatchSize)
		if err != nil {
			return exportedRows, err
		}
		if len(items) == 0 {
			return exportedRows, nil
		}
		for i := range items {
			if err := writeRevenueShareSettlementCSVRow(writer, &items[i], loc); err != nil {
				return exportedRows, err
			}
			exportedRows++
		}
		last := items[len(items)-1]
		cursorCreatedAt = &last.CreatedAt
		cursorID = last.ID
		if err := s.updateShareSettlementExportProgress(ctx, taskID, exportedRows); err != nil {
			return exportedRows, err
		}
	}
}

func (s *RevenueService) queryShareSettlementExportBatch(ctx context.Context, params RevenueShareSettlementExportParams, cursorCreatedAt *time.Time, cursorID int64, limit int) ([]RevenueShareSettlementItem, error) {
	normalized, status, err := normalizeRevenueShareSettlementExportParams(params)
	if err != nil {
		return nil, err
	}
	where, args := buildRevenueShareSettlementWhere(RevenueShareSettlementQueryParams{
		StartTime: normalized.StartTime,
		EndTime:   normalized.EndTime,
		Search:    normalized.Search,
	}, status)
	if cursorCreatedAt != nil {
		args = append(args, *cursorCreatedAt, cursorID)
		createdAtArg := len(args) - 1
		idArg := len(args)
		where += fmt.Sprintf(" AND (ase.created_at < $%d OR (ase.created_at = $%d AND ase.id < $%d))", createdAtArg, createdAtArg, idArg)
	}
	args = append(args, limit)
	limitArg := len(args)
	query := fmt.Sprintf(`
		SELECT %s
		FROM account_share_settlement_entries ase
		LEFT JOIN users cu ON cu.id = ase.consumer_user_id
		LEFT JOIN users ou ON ou.id = ase.owner_user_id
		LEFT JOIN users iu ON iu.id = ase.inviter_user_id
		LEFT JOIN accounts a ON a.id = ase.account_id
		LEFT JOIN api_keys ak ON ak.id = ase.api_key_id
		LEFT JOIN groups g ON g.id = ase.group_id
		WHERE %s
		ORDER BY ase.created_at DESC, ase.id DESC
		LIMIT $%d
	`, revenueShareSettlementSelectColumns, where, limitArg)
	rows, err := s.entClient.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query revenue share settlement export batch: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanRevenueShareSettlementItems(rows, limit)
}

func writeRevenueShareSettlementCSVHeader(writer *csv.Writer) error {
	if err := writer.Write([]string{
		"时间",
		"请求ID",
		"密钥ID",
		"密钥名称",
		"消费用户ID",
		"消费用户邮箱",
		"消费用户名",
		"账号主ID",
		"账号主邮箱",
		"账号主用户名",
		"邀请人ID",
		"邀请人邮箱",
		"邀请人用户名",
		"账号ID",
		"账号名称",
		"账号平台",
		"分组ID",
		"分组名称",
		"策略ID",
		"策略版本",
		"分成模式快照",
		"分成状态快照",
		"他人消费",
		"账号成本",
		"账号主比例",
		"账号主收入",
		"邀请绑定时间",
		"邀请过期时间",
		"邀请比例",
		"邀请收益",
		"平台比例",
		"平台留存",
		"平台净收益",
		"状态",
	}); err != nil {
		return fmt.Errorf("write revenue export csv header: %w", err)
	}
	return nil
}

func writeRevenueShareSettlementCSVRow(writer *csv.Writer, item *RevenueShareSettlementItem, loc *time.Location) error {
	groupID := ""
	if item.GroupID != nil {
		groupID = strconv.FormatInt(*item.GroupID, 10)
	}
	policyID := ""
	if item.PolicyID != nil {
		policyID = strconv.FormatInt(*item.PolicyID, 10)
	}
	inviterID := ""
	if item.InviterUserID != nil {
		inviterID = strconv.FormatInt(*item.InviterUserID, 10)
	}
	return writer.Write([]string{
		formatRevenueExportTime(item.CreatedAt, loc),
		item.RequestID,
		strconv.FormatInt(item.APIKeyID, 10),
		item.APIKeyName,
		strconv.FormatInt(item.ConsumerUserID, 10),
		item.ConsumerEmail,
		item.ConsumerUsername,
		strconv.FormatInt(item.OwnerUserID, 10),
		item.OwnerEmail,
		item.OwnerUsername,
		inviterID,
		item.InviterEmail,
		item.InviterUsername,
		strconv.FormatInt(item.AccountID, 10),
		item.AccountName,
		item.AccountPlatform,
		groupID,
		item.GroupName,
		policyID,
		strconv.Itoa(item.PolicyVersion),
		item.ShareModeSnapshot,
		item.ShareStatusSnapshot,
		formatRevenueExportAmount(item.ConsumerCharge),
		formatRevenueExportAmount(item.AccountCost),
		formatRevenueExportPercent(item.OwnerShareRatio),
		formatRevenueExportAmount(item.OwnerCredit),
		formatRevenueExportOptionalTime(item.InviteBoundAt, loc),
		formatRevenueExportOptionalTime(item.InviteExpiresAt, loc),
		formatRevenueExportPercent(item.InviteShareRatio),
		formatRevenueExportAmount(item.InviteCredit),
		formatRevenueExportPercent(item.PlatformShareRatio),
		formatRevenueExportAmount(item.PlatformFee),
		formatRevenueExportAmount(item.PlatformNetProfit),
		item.Status,
	})
}

func formatRevenueExportTime(value time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	return value.In(loc).Format("2006-01-02 15:04:05")
}

func formatRevenueExportOptionalTime(value *time.Time, loc *time.Location) string {
	if value == nil {
		return ""
	}
	return formatRevenueExportTime(*value, loc)
}

func formatRevenueExportAmount(value float64) string {
	return strconv.FormatFloat(roundRevenue(value), 'f', 6, 64)
}

func formatRevenueExportPercent(value float64) string {
	return strconv.FormatFloat(roundRevenue(value*100), 'f', 2, 64) + "%"
}

func (s *RevenueService) ensureShareSettlementExportActive(ctx context.Context, taskID int64) error {
	var status string
	if err := s.querySingle(ctx, `
		SELECT status
		FROM revenue_share_settlement_export_tasks
		WHERE id = $1
	`, []any{taskID}, &status); err != nil {
		return err
	}
	if status == revenueShareSettlementExportStatusCanceled {
		return errRevenueShareSettlementCanceled
	}
	if status != revenueShareSettlementExportStatusRunning {
		return fmt.Errorf("unexpected revenue export status: %s", status)
	}
	return nil
}

func (s *RevenueService) updateShareSettlementExportProgress(ctx context.Context, taskID int64, exportedRows int64) error {
	_, err := s.entClient.ExecContext(ctx, `
		UPDATE revenue_share_settlement_export_tasks
		SET exported_rows = $2,
			updated_at = NOW()
		WHERE id = $1 AND status = $3
	`, taskID, exportedRows, revenueShareSettlementExportStatusRunning)
	return err
}

func (s *RevenueService) markShareSettlementExportCompleted(ctx context.Context, taskID int64, exportedRows int64, fileCount int, filePath, fileName string, fileSize int64) error {
	_, err := s.entClient.ExecContext(ctx, `
		UPDATE revenue_share_settlement_export_tasks
		SET status = $2,
			exported_rows = $3,
			file_count = $4,
			file_path = $5,
			file_name = $6,
			file_size_bytes = $7,
			completed_at = NOW(),
			updated_at = NOW()
		WHERE id = $1 AND status = $8
	`, taskID, revenueShareSettlementExportStatusCompleted, exportedRows, fileCount, filePath, fileName, fileSize, revenueShareSettlementExportStatusRunning)
	return err
}

func (s *RevenueService) markShareSettlementExportFailed(ctx context.Context, taskID int64, exportedRows int64, cause error) error {
	_, err := s.entClient.ExecContext(ctx, `
		UPDATE revenue_share_settlement_export_tasks
		SET status = $2,
			exported_rows = $3,
			error_message = $4,
			completed_at = NOW(),
			updated_at = NOW()
		WHERE id = $1 AND status = $5
	`, taskID, revenueShareSettlementExportStatusFailed, exportedRows, truncateRevenueExportError(cause), revenueShareSettlementExportStatusRunning)
	return err
}

func (s *RevenueService) markShareSettlementExportCanceled(ctx context.Context, taskID int64, exportedRows int64) error {
	_, err := s.entClient.ExecContext(ctx, `
		UPDATE revenue_share_settlement_export_tasks
		SET status = $2,
			exported_rows = $3,
			canceled_at = COALESCE(canceled_at, NOW()),
			completed_at = COALESCE(completed_at, NOW()),
			updated_at = NOW()
		WHERE id = $1 AND status IN ($2, $4)
	`, taskID, revenueShareSettlementExportStatusCanceled, exportedRows, revenueShareSettlementExportStatusRunning)
	return err
}

func (s *RevenueService) failStaleShareSettlementExports(ctx context.Context) error {
	_, err := s.entClient.ExecContext(ctx, `
		UPDATE revenue_share_settlement_export_tasks
		SET status = $1,
			error_message = $2,
			completed_at = NOW(),
			updated_at = NOW()
		WHERE status = $3
			AND updated_at < $4
	`, revenueShareSettlementExportStatusFailed, "export worker interrupted", revenueShareSettlementExportStatusRunning, time.Now().Add(-revenueShareSettlementExportStaleAfter))
	return err
}

func (s *RevenueService) cleanupExpiredShareSettlementExports(ctx context.Context) error {
	rows, err := s.entClient.QueryContext(ctx, `
		SELECT id, COALESCE(file_path, '')
		FROM revenue_share_settlement_export_tasks
		WHERE expires_at < NOW()
			AND COALESCE(file_path, '') <> ''
		ORDER BY expires_at ASC
		LIMIT $1
	`, revenueShareSettlementExportCleanupBatchMax)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			return err
		}
		if strings.TrimSpace(path) != "" {
			_ = os.Remove(path)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := s.entClient.ExecContext(ctx, `
			UPDATE revenue_share_settlement_export_tasks
			SET file_path = NULL,
				file_size_bytes = 0,
				updated_at = NOW()
			WHERE id = $1
		`, id); err != nil {
			return err
		}
	}
	return nil
}

func revenueShareSettlementExportRoot() (string, error) {
	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir == "" {
		dataDir = "data"
	}
	if strings.Contains(dataDir, "\x00") {
		return "", infraerrors.New(http.StatusInternalServerError, "REVENUE_EXPORT_DIR_INVALID", "export directory is invalid")
	}
	return filepath.Clean(filepath.Join(dataDir, "exports", "revenue")), nil
}

func exportDateLabel(t time.Time, loc *time.Location) string {
	if loc != nil {
		t = t.In(loc)
	}
	return t.Format("20060102")
}

func truncateRevenueExportError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if len(msg) > 1000 {
		return msg[:1000]
	}
	return msg
}

func copyWithBOM(w io.Writer) error {
	_, err := w.Write([]byte{0xEF, 0xBB, 0xBF})
	return err
}
