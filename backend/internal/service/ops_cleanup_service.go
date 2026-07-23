package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
)

const (
	opsCleanupJobName            = "ops_cleanup"
	opsCleanupArchiveTriggeredBy = "ops_cleanup_auto"

	opsCleanupLeaderLockKeyDefault = "ops:cleanup:leader"
	opsCleanupLeaderLockTTLDefault = 30 * time.Minute
	opsCleanupLeaderLockTTLGrace   = 5 * time.Minute
	opsCleanupReconcileInterval    = time.Minute
)

var opsCleanupCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

var opsCleanupReleaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

// OpsCleanupService periodically deletes old ops data to prevent unbounded DB growth.
//
// - Scheduling: 5-field cron spec (minute hour dom month dow).
// - Multi-instance: best-effort Redis leader lock so only one node runs cleanup.
// - Safety: deletes in batches to avoid long transactions.
//
// 附带：在 runCleanupOnce 末尾调用 ChannelMonitorService.RunDailyMaintenance，
// 统一共享 cron schedule + leader lock + heartbeat，避免再引一套调度。
type OpsCleanupService struct {
	opsRepo           OpsRepository
	settingRepo       SettingRepository
	db                *sql.DB
	redisClient       *redis.Client
	cfg               *config.Config
	channelMonitorSvc *ChannelMonitorService
	archiveCreator    opsCleanupArchiveCreator
	taskExecutor      *ClusterTaskExecutor

	instanceID string

	cronMu      sync.Mutex
	cron        *cron.Cron
	cronEntryID cron.EntryID
	stopped     bool

	scheduleStateInitialized bool
	appliedSchedule          string
	appliedEnabled           bool

	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc
	reconcileWG     sync.WaitGroup

	startOnce sync.Once
	stopOnce  sync.Once

	warnNoRedisOnce sync.Once
}

type opsCleanupArchiveCreator interface {
	CreateDataArchive(ctx context.Context, input DataArchiveInput) (*BackupRecord, error)
}

func NewOpsCleanupService(
	opsRepo OpsRepository,
	settingRepo SettingRepository,
	db *sql.DB,
	redisClient *redis.Client,
	cfg *config.Config,
	channelMonitorSvc *ChannelMonitorService,
	archiveCreator opsCleanupArchiveCreator,
) *OpsCleanupService {
	return &OpsCleanupService{
		opsRepo:           opsRepo,
		settingRepo:       settingRepo,
		db:                db,
		redisClient:       redisClient,
		cfg:               cfg,
		channelMonitorSvc: channelMonitorSvc,
		archiveCreator:    archiveCreator,
		instanceID:        uuid.NewString(),
	}
}

func (s *OpsCleanupService) Start() {
	if s == nil {
		return
	}
	if s.cfg == nil {
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] not started (missing config)")
		return
	}
	if !s.cfg.Ops.Enabled {
		return
	}
	if s.opsRepo == nil || s.db == nil {
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] not started (missing deps)")
		return
	}
	if err := s.cfg.Ops.Cleanup.Validate(); err != nil {
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] not started (invalid config): %v", err)
		return
	}
	loc, err := loadOpsCleanupLocation(s.cfg.Timezone)
	if err != nil {
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] not started (invalid config): %v", err)
		return
	}

	s.startOnce.Do(func() {
		c := cron.New(cron.WithParser(opsCleanupCronParser), cron.WithLocation(loc))
		lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
		s.cronMu.Lock()
		s.cron = c
		s.stopped = false
		s.lifecycleCtx = lifecycleCtx
		s.lifecycleCancel = lifecycleCancel
		s.cronMu.Unlock()
		c.Start()

		ctx, cancel := context.WithTimeout(lifecycleCtx, 5*time.Second)
		loadErr := s.ReconcileDataRetentionSettings(ctx)
		cancel()
		if loadErr != nil {
			logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] started without a cleanup entry (load dynamic settings failed): %v", loadErr)
		}

		s.reconcileWG.Add(1)
		go s.runSettingsReconcileLoop(lifecycleCtx)
	})
}

func (s *OpsCleanupService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		s.cronMu.Lock()
		c := s.cron
		cancel := s.lifecycleCancel
		s.cron = nil
		s.cronEntryID = 0
		s.stopped = true
		s.cronMu.Unlock()
		if cancel != nil {
			cancel()
		}
		if c != nil {
			ctx := c.Stop()
			select {
			case <-ctx.Done():
			case <-time.After(3 * time.Second):
				logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] cron stop timed out")
			}
		}
		reconcileDone := make(chan struct{})
		go func() {
			s.reconcileWG.Wait()
			close(reconcileDone)
		}()
		select {
		case <-reconcileDone:
		case <-time.After(3 * time.Second):
			logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] settings reconcile stop timed out")
		}
	})
}

func (s *OpsCleanupService) runSettingsReconcileLoop(ctx context.Context) {
	defer s.reconcileWG.Done()
	ticker := time.NewTicker(opsCleanupReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcileCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := s.ReconcileDataRetentionSettings(reconcileCtx)
			cancel()
			if err != nil && !errors.Is(err, context.Canceled) {
				logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] reconcile dynamic settings failed: %v", err)
			}
		}
	}
}

func (s *OpsCleanupService) runScheduled(triggerSchedule string) {
	if s == nil || s.db == nil || s.opsRepo == nil || s.cfg == nil {
		return
	}
	parentCtx := s.lifecycleCtx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	settingsCtx, settingsCancel := context.WithTimeout(parentCtx, 5*time.Second)
	cleanupCfg, err := s.loadEffectiveCleanupConfig(settingsCtx)
	settingsCancel()
	if err != nil {
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] cleanup rejected (invalid dynamic settings): %v", err)
		return
	}
	if !cleanupCfg.Enabled {
		return
	}
	if err := s.applyCleanupSchedule(cleanupCfg); err != nil {
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] cleanup rejected (schedule reconcile failed): %v", err)
		return
	}
	if !opsCleanupTriggerMatches(triggerSchedule, cleanupCfg.Schedule) {
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] skipped stale cron callback (effective schedule=%q)", cleanupCfg.Schedule)
		return
	}
	loc, err := loadOpsCleanupLocation(s.cfg.Timezone)
	if err != nil {
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] cleanup rejected (invalid timezone): %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(parentCtx, time.Duration(cleanupCfg.RunTimeoutSeconds)*time.Second)
	defer cancel()

	if s.cfg.Cluster.Enabled {
		if s.taskExecutor == nil {
			logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] cluster task executor is not configured")
			return
		}
		_, taskErr := s.taskExecutor.Run(ctx, opsCleanupJobName, func(taskCtx context.Context, guard *ClusterLeaseGuard) error {
			return s.runCleanupOwned(taskCtx, cleanupCfg, loc, guard)
		})
		if taskErr != nil {
			logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] cleanup failed: %v", taskErr)
		}
		return
	}

	release, ok := s.tryAcquireLeaderLock(ctx)
	if !ok {
		return
	}
	if release != nil {
		defer release()
	}

	if err := s.runCleanupOwned(ctx, cleanupCfg, loc, &ClusterLeaseGuard{}); err != nil {
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] cleanup failed: %v", err)
	}
}

func (s *OpsCleanupService) runCleanupOwned(
	ctx context.Context,
	cleanupCfg config.OpsCleanupConfig,
	loc *time.Location,
	guard *ClusterLeaseGuard,
) error {
	startedAt := time.Now().UTC()
	runAt := startedAt

	counts, err := s.runCleanupOnceWithConfigGuarded(ctx, cleanupCfg, loc, guard)
	if err != nil {
		if guardErr := guard.Check(ctx); guardErr != nil {
			return guardErr
		}
		s.recordHeartbeatError(runAt, time.Since(startedAt), err)
		return err
	}
	if err := guard.Check(ctx); err != nil {
		return err
	}
	s.recordHeartbeatSuccess(runAt, time.Since(startedAt), counts)
	logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] cleanup complete: %s", counts)
	return nil
}

type opsCleanupDeletedCounts struct {
	errorLogs     int64
	retryAttempts int64
	alertEvents   int64
	systemLogs    int64
	logAudits     int64
	systemMetrics int64
	hourlyPreagg  int64
	dailyPreagg   int64
}

type opsCleanupWindow struct {
	Start time.Time
	End   time.Time
}

type opsCleanupWindowExporter func(context.Context, opsCleanupWindow) (io.ReadCloser, error)

func (c opsCleanupDeletedCounts) String() string {
	return fmt.Sprintf(
		"error_logs=%d retry_attempts=%d alert_events=%d system_logs=%d log_audits=%d system_metrics=%d hourly_preagg=%d daily_preagg=%d",
		c.errorLogs,
		c.retryAttempts,
		c.alertEvents,
		c.systemLogs,
		c.logAudits,
		c.systemMetrics,
		c.hourlyPreagg,
		c.dailyPreagg,
	)
}

// opsCleanupPlan 把"保留天数"翻译成清理截止时间；0 明确表示禁用该目标。
func opsCleanupPlan(now time.Time, days int) (cutoff time.Time, ok bool) {
	if days <= 0 {
		return time.Time{}, false
	}
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return dayStart.AddDate(0, 0, -days), true
}

func loadOpsCleanupLocation(value string) (*time.Location, error) {
	name := strings.TrimSpace(value)
	if name == "" {
		return nil, fmt.Errorf("ops cleanup timezone is required")
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("load ops cleanup timezone %q: %w", name, err)
	}
	return loc, nil
}

func opsCleanupTriggerMatches(triggerSchedule, effectiveSchedule string) bool {
	return strings.TrimSpace(triggerSchedule) == strings.TrimSpace(effectiveSchedule)
}

func mergeOpsCleanupDataRetention(base config.OpsCleanupConfig, retention OpsDataRetentionSettings) (config.OpsCleanupConfig, error) {
	if err := validateOpsDataRetentionSettings(retention); err != nil {
		return config.OpsCleanupConfig{}, err
	}
	base.Enabled = base.Enabled && retention.CleanupEnabled
	base.Schedule = strings.TrimSpace(retention.CleanupSchedule)
	base.ErrorLogRetentionDays = retention.ErrorLogRetentionDays
	base.MinuteMetricsRetentionDays = retention.MinuteMetricsRetentionDays
	base.HourlyMetricsRetentionDays = retention.HourlyMetricsRetentionDays
	if err := base.Validate(); err != nil {
		return config.OpsCleanupConfig{}, err
	}
	return base, nil
}

func (s *OpsCleanupService) loadEffectiveCleanupConfig(ctx context.Context) (config.OpsCleanupConfig, error) {
	if s == nil || s.cfg == nil {
		return config.OpsCleanupConfig{}, fmt.Errorf("ops cleanup service is not configured")
	}
	base := s.cfg.Ops.Cleanup
	if err := base.Validate(); err != nil {
		return config.OpsCleanupConfig{}, err
	}
	if !base.Enabled || s.settingRepo == nil {
		return base, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyOpsAdvancedSettings)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return base, nil
		}
		return config.OpsCleanupConfig{}, fmt.Errorf("load ops advanced settings: %w", err)
	}
	advanced := defaultOpsAdvancedSettings()
	if err := json.Unmarshal([]byte(raw), advanced); err != nil {
		return config.OpsCleanupConfig{}, fmt.Errorf("decode ops advanced settings: %w", err)
	}
	if err := validateOpsDataRetentionSettings(advanced.DataRetention); err != nil {
		return config.OpsCleanupConfig{}, fmt.Errorf("validate ops advanced data retention: %w", err)
	}
	normalizeOpsAdvancedSettings(advanced)
	return mergeOpsCleanupDataRetention(base, advanced.DataRetention)
}

// ReconcileDataRetentionSettings treats the persisted settings row as the only source of truth.
// It is safe to call concurrently and is run periodically by every application instance.
func (s *OpsCleanupService) ReconcileDataRetentionSettings(ctx context.Context) error {
	if s == nil || s.cfg == nil {
		return fmt.Errorf("ops cleanup service is not configured")
	}
	if !s.cfg.Ops.Enabled {
		return nil
	}
	cleanupCfg, err := s.loadEffectiveCleanupConfig(ctx)
	if err != nil {
		return err
	}
	return s.applyCleanupSchedule(cleanupCfg)
}

func (s *OpsCleanupService) applyCleanupSchedule(cleanupCfg config.OpsCleanupConfig) error {
	if err := cleanupCfg.Validate(); err != nil {
		return err
	}
	schedule := strings.TrimSpace(cleanupCfg.Schedule)
	if cleanupCfg.Enabled {
		if _, err := opsCleanupCronParser.Parse(schedule); err != nil {
			return fmt.Errorf("invalid ops cleanup schedule %q: %w", schedule, err)
		}
	}

	s.cronMu.Lock()
	defer s.cronMu.Unlock()
	if s.stopped {
		return fmt.Errorf("ops cleanup scheduler is stopped")
	}
	if s.cron == nil {
		return fmt.Errorf("ops cleanup scheduler is not initialized")
	}
	if s.scheduleStateInitialized && s.appliedEnabled == cleanupCfg.Enabled && s.appliedSchedule == schedule {
		return nil
	}

	var newEntryID cron.EntryID
	if cleanupCfg.Enabled {
		triggerSchedule := schedule
		entryID, err := s.cron.AddFunc(schedule, func() { s.runScheduled(triggerSchedule) })
		if err != nil {
			return fmt.Errorf("add ops cleanup schedule %q: %w", schedule, err)
		}
		newEntryID = entryID
	}
	oldEntryID := s.cronEntryID
	s.cronEntryID = newEntryID
	s.scheduleStateInitialized = true
	s.appliedEnabled = cleanupCfg.Enabled
	s.appliedSchedule = schedule
	if oldEntryID != 0 {
		s.cron.Remove(oldEntryID)
	}
	if cleanupCfg.Enabled {
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] schedule applied (schedule=%q)", schedule)
	} else {
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] schedule disabled by advanced settings")
	}
	return nil
}

func (s *OpsCleanupService) runCleanupOnce(ctx context.Context) (opsCleanupDeletedCounts, error) {
	out := opsCleanupDeletedCounts{}
	if s == nil || s.db == nil || s.cfg == nil {
		return out, fmt.Errorf("ops cleanup service is not configured")
	}
	cleanupCfg, err := s.loadEffectiveCleanupConfig(ctx)
	if err != nil {
		return out, err
	}
	if !cleanupCfg.Enabled {
		return out, nil
	}
	loc, err := loadOpsCleanupLocation(s.cfg.Timezone)
	if err != nil {
		return out, err
	}
	return s.runCleanupOnceWithConfig(ctx, cleanupCfg, loc)
}

func (s *OpsCleanupService) runCleanupOnceWithConfig(
	ctx context.Context,
	cleanupCfg config.OpsCleanupConfig,
	loc *time.Location,
) (opsCleanupDeletedCounts, error) {
	return s.runCleanupOnceWithConfigGuarded(ctx, cleanupCfg, loc, &ClusterLeaseGuard{})
}

func (s *OpsCleanupService) runCleanupOnceWithConfigGuarded(
	ctx context.Context,
	cleanupCfg config.OpsCleanupConfig,
	loc *time.Location,
	guard *ClusterLeaseGuard,
) (opsCleanupDeletedCounts, error) {
	out := opsCleanupDeletedCounts{}
	if s == nil || s.db == nil || loc == nil {
		return out, fmt.Errorf("ops cleanup service is not configured")
	}
	if err := cleanupCfg.Validate(); err != nil {
		return out, err
	}
	if !cleanupCfg.Enabled {
		return out, nil
	}
	now := time.Now().In(loc)

	runDelete := func(cutoff time.Time, table, timeCol string, castDate bool) (int64, error) {
		deleteCtx, cancel := context.WithTimeout(ctx, time.Duration(cleanupCfg.DeleteTimeoutSeconds)*time.Second)
		defer cancel()
		return deleteOldRowsByIDGuarded(deleteCtx, s.db, table, timeCol, cutoff, cleanupCfg.DeleteBatchSize, castDate, guard)
	}
	var cleanupErr error
	recordError := func(scope string, err error) {
		if err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("%s: %w", scope, err))
		}
	}

	// Archive-backed log tables run first so auxiliary-table deletion cannot consume
	// the run budget before both durable archives have advanced.
	if cutoff, ok := opsCleanupPlan(now, cleanupCfg.ErrorLogRetentionDays); ok {
		n, err := s.cleanupOpsLogWindowsWithConfigGuarded(ctx, cleanupCfg, loc, "ops_error_logs", "created_at", cutoff, s.exportOpsErrorLogWindow, guard)
		if err == nil {
			out.errorLogs = n
		}
		recordError("cleanup ops_error_logs", err)

		n, err = s.cleanupOpsLogWindowsWithConfigGuarded(ctx, cleanupCfg, loc, "ops_system_logs", "created_at", cutoff, s.exportOpsSystemLogWindow, guard)
		if err == nil {
			out.systemLogs = n
		}
		recordError("cleanup ops_system_logs", err)

		n, err = runDelete(cutoff, "ops_retry_attempts", "created_at", false)
		if err == nil {
			out.retryAttempts = n
		}
		recordError("cleanup ops_retry_attempts", err)

		n, err = runDelete(cutoff, "ops_alert_events", "created_at", false)
		if err == nil {
			out.alertEvents = n
		}
		recordError("cleanup ops_alert_events", err)

		n, err = runDelete(cutoff, "ops_system_log_cleanup_audits", "created_at", false)
		if err == nil {
			out.logAudits = n
		}
		recordError("cleanup ops_system_log_cleanup_audits", err)
	}

	// Minute-level metrics snapshots.
	if cutoff, ok := opsCleanupPlan(now, cleanupCfg.MinuteMetricsRetentionDays); ok {
		n, err := runDelete(cutoff, "ops_system_metrics", "created_at", false)
		if err == nil {
			out.systemMetrics = n
		}
		recordError("cleanup ops_system_metrics", err)
	}

	// Pre-aggregation tables (hourly/daily).
	if cutoff, ok := opsCleanupPlan(now, cleanupCfg.HourlyMetricsRetentionDays); ok {
		n, err := runDelete(cutoff, "ops_metrics_hourly", "bucket_start", false)
		if err == nil {
			out.hourlyPreagg = n
		}
		recordError("cleanup ops_metrics_hourly", err)

		n, err = runDelete(cutoff, "ops_metrics_daily", "bucket_date", true)
		if err == nil {
			out.dailyPreagg = n
		}
		recordError("cleanup ops_metrics_daily", err)
	}

	// Channel monitor 每日维护（聚合昨日明细 + 软删过期明细/聚合）。
	// 失败只记日志，不影响 ops 清理的成功状态（与 ops 各步骤风格一致）；
	// 维护本身已经把每步错误打到 slog，heartbeat result 不再分项记录。
	if s.channelMonitorSvc != nil {
		if err := guard.Check(ctx); err != nil {
			return out, errors.Join(cleanupErr, err)
		}
		if err := s.channelMonitorSvc.RunDailyMaintenance(ctx); err != nil {
			logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] channel monitor maintenance failed: %v", err)
		}
	}

	return out, cleanupErr
}

func (s *OpsCleanupService) exportOpsErrorLogWindow(ctx context.Context, window opsCleanupWindow) (io.ReadCloser, error) {
	start, end := window.Start.UTC(), window.End.UTC()
	return s.opsRepo.ExportErrorLogs(ctx, &OpsErrorLogCleanupFilter{StartTime: &start, EndTime: &end})
}

func (s *OpsCleanupService) exportOpsSystemLogWindow(ctx context.Context, window opsCleanupWindow) (io.ReadCloser, error) {
	start, end := window.Start.UTC(), window.End.UTC()
	return s.opsRepo.ExportSystemLogs(ctx, &OpsSystemLogCleanupFilter{StartTime: &start, EndTime: &end})
}

func (s *OpsCleanupService) cleanupOpsLogWindows(
	ctx context.Context,
	table string,
	timeColumn string,
	cutoff time.Time,
	exportWindow opsCleanupWindowExporter,
) (int64, error) {
	if s == nil || s.db == nil || s.cfg == nil {
		return 0, fmt.Errorf("ops cleanup window dependencies are not configured")
	}
	cleanupCfg, err := s.loadEffectiveCleanupConfig(ctx)
	if err != nil {
		return 0, err
	}
	if !cleanupCfg.Enabled {
		return 0, nil
	}
	loc, err := loadOpsCleanupLocation(s.cfg.Timezone)
	if err != nil {
		return 0, err
	}
	return s.cleanupOpsLogWindowsWithConfig(ctx, cleanupCfg, loc, table, timeColumn, cutoff, exportWindow)
}

func (s *OpsCleanupService) cleanupOpsLogWindowsWithConfig(
	ctx context.Context,
	cleanupCfg config.OpsCleanupConfig,
	loc *time.Location,
	table string,
	timeColumn string,
	cutoff time.Time,
	exportWindow opsCleanupWindowExporter,
) (int64, error) {
	return s.cleanupOpsLogWindowsWithConfigGuarded(ctx, cleanupCfg, loc, table, timeColumn, cutoff, exportWindow, &ClusterLeaseGuard{})
}

func (s *OpsCleanupService) cleanupOpsLogWindowsWithConfigGuarded(
	ctx context.Context,
	cleanupCfg config.OpsCleanupConfig,
	loc *time.Location,
	table string,
	timeColumn string,
	cutoff time.Time,
	exportWindow opsCleanupWindowExporter,
	guard *ClusterLeaseGuard,
) (int64, error) {
	if s == nil || s.db == nil || exportWindow == nil || loc == nil {
		return 0, fmt.Errorf("ops cleanup window dependencies are not configured")
	}
	if s.archiveCreator == nil {
		return 0, fmt.Errorf("ops cleanup archive creator is not configured")
	}
	if err := cleanupCfg.Validate(); err != nil {
		return 0, err
	}
	var deletedTotal int64
	for range cleanupCfg.MaxCatchupWindowsPerRun {
		oldest, err := findOldestOpsCleanupTime(ctx, s.db, table, timeColumn, cutoff)
		if err != nil {
			return deletedTotal, err
		}
		if oldest == nil {
			return deletedTotal, nil
		}
		window, err := buildOpsCleanupWindow(*oldest, cutoff, cleanupCfg.ArchiveWindowDays, loc)
		if err != nil {
			return deletedTotal, err
		}

		archiveCtx, archiveCancel := context.WithTimeout(ctx, time.Duration(cleanupCfg.ArchiveTimeoutSeconds)*time.Second)
		stream, err := exportWindow(archiveCtx, window)
		if err == nil {
			err = guard.Check(archiveCtx)
		}
		if err == nil {
			err = s.createOpsCleanupArchive(archiveCtx, table, window, stream)
		} else if stream != nil {
			_ = stream.Close()
		}
		archiveCancel()
		if err != nil {
			return deletedTotal, fmt.Errorf("archive %s window [%s,%s): %w", table, window.Start.Format(time.RFC3339), window.End.Format(time.RFC3339), err)
		}

		deleteCtx, deleteCancel := context.WithTimeout(ctx, time.Duration(cleanupCfg.DeleteTimeoutSeconds)*time.Second)
		deleted, err := deleteRowsByIDWindowGuarded(deleteCtx, s.db, table, timeColumn, window, cleanupCfg.DeleteBatchSize, guard)
		deleteCancel()
		deletedTotal += deleted
		if err != nil {
			return deletedTotal, fmt.Errorf("delete %s window [%s,%s): %w", table, window.Start.Format(time.RFC3339), window.End.Format(time.RFC3339), err)
		}
	}
	return deletedTotal, nil
}

func (s *OpsCleanupService) createOpsCleanupArchive(ctx context.Context, table string, window opsCleanupWindow, stream io.ReadCloser) error {
	if stream == nil {
		return fmt.Errorf("%s archive stream is nil", table)
	}
	defer func() { _ = stream.Close() }()
	record, err := s.archiveCreator.CreateDataArchive(ctx, DataArchiveInput{
		Stream:      stream,
		FileName:    opsCleanupArchiveFileName(table, window, time.Now().UTC()),
		BackupType:  table + "_archive",
		TriggeredBy: opsCleanupArchiveTriggeredBy,
		ExpireDays:  s.opsArchiveExpireDays(),
	})
	if err != nil {
		return fmt.Errorf("archive %s before cleanup: %w", table, err)
	}
	if record == nil || record.Status != "completed" {
		return fmt.Errorf("archive %s before cleanup incomplete", table)
	}
	logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] archived %s before cleanup: backup=%s file=%s", table, record.ID, record.FileName)
	return nil
}

func (s *OpsCleanupService) opsArchiveExpireDays() int {
	if s == nil || s.cfg == nil {
		return 0
	}
	return s.cfg.Ops.Cleanup.ArchiveExpireDays
}

func opsCleanupArchiveFileName(table string, window opsCleanupWindow, attemptAt time.Time) string {
	return fmt.Sprintf(
		"%s_%s_%s_%s.ndjson.gz",
		table,
		window.Start.UTC().Format("20060102_150405"),
		window.End.UTC().Format("20060102_150405"),
		attemptAt.UTC().Format("20060102_150405.000000000"),
	)
}

func findOldestOpsCleanupTime(ctx context.Context, db *sql.DB, table, timeColumn string, cutoff time.Time) (*time.Time, error) {
	if db == nil {
		return nil, fmt.Errorf("ops cleanup database is not configured")
	}
	var oldest sql.NullTime
	query := fmt.Sprintf("SELECT MIN(%s) FROM %s WHERE %s < $1", timeColumn, table, timeColumn)
	if err := db.QueryRowContext(ctx, query, cutoff.UTC()).Scan(&oldest); err != nil {
		return nil, err
	}
	if !oldest.Valid {
		return nil, nil
	}
	value := oldest.Time.UTC()
	return &value, nil
}

func buildOpsCleanupWindow(oldest, cutoff time.Time, windowDays int, loc *time.Location) (opsCleanupWindow, error) {
	if windowDays <= 0 || loc == nil {
		return opsCleanupWindow{}, fmt.Errorf("invalid ops cleanup window configuration")
	}
	localOldest := oldest.In(loc)
	start := time.Date(localOldest.Year(), localOldest.Month(), localOldest.Day(), 0, 0, 0, 0, loc).UTC()
	end := start.In(loc).AddDate(0, 0, windowDays).UTC()
	if end.After(cutoff.UTC()) {
		end = cutoff.UTC()
	}
	if !end.After(start) {
		return opsCleanupWindow{}, fmt.Errorf("invalid ops cleanup window [%s,%s)", start.Format(time.RFC3339), end.Format(time.RFC3339))
	}
	return opsCleanupWindow{Start: start, End: end}, nil
}

func deleteOldRowsByID(
	ctx context.Context,
	db *sql.DB,
	table string,
	timeColumn string,
	cutoff time.Time,
	batchSize int,
	castCutoffToDate bool,
) (int64, error) {
	return deleteOldRowsByIDGuarded(ctx, db, table, timeColumn, cutoff, batchSize, castCutoffToDate, &ClusterLeaseGuard{})
}

func deleteOldRowsByIDGuarded(
	ctx context.Context,
	db *sql.DB,
	table string,
	timeColumn string,
	cutoff time.Time,
	batchSize int,
	castCutoffToDate bool,
	guard *ClusterLeaseGuard,
) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("ops cleanup database is not configured")
	}
	if batchSize <= 0 {
		return 0, fmt.Errorf("ops cleanup delete batch size must be positive")
	}

	where := fmt.Sprintf("%s < $1", timeColumn)
	if castCutoffToDate {
		where = fmt.Sprintf("%s < $1::date", timeColumn)
	}

	q := fmt.Sprintf(`
WITH batch AS (
  SELECT id FROM %s
  WHERE %s
  ORDER BY id
  LIMIT $2
)
DELETE FROM %s
WHERE id IN (SELECT id FROM batch)
`, table, where, table)

	var total int64
	for {
		if err := guard.Check(ctx); err != nil {
			return total, err
		}
		res, err := db.ExecContext(ctx, q, cutoff, batchSize)
		if err != nil {
			return total, err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += affected
		if affected < int64(batchSize) {
			break
		}
	}
	return total, nil
}

func deleteRowsByIDWindow(
	ctx context.Context,
	db *sql.DB,
	table string,
	timeColumn string,
	window opsCleanupWindow,
	batchSize int,
) (int64, error) {
	return deleteRowsByIDWindowGuarded(ctx, db, table, timeColumn, window, batchSize, &ClusterLeaseGuard{})
}

func deleteRowsByIDWindowGuarded(
	ctx context.Context,
	db *sql.DB,
	table string,
	timeColumn string,
	window opsCleanupWindow,
	batchSize int,
	guard *ClusterLeaseGuard,
) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("ops cleanup database is not configured")
	}
	if batchSize <= 0 {
		return 0, fmt.Errorf("ops cleanup delete batch size must be positive")
	}
	if !window.End.After(window.Start) {
		return 0, fmt.Errorf("invalid ops cleanup delete window")
	}
	query := fmt.Sprintf(`
WITH batch AS (
  SELECT id FROM %s
  WHERE %s >= $1 AND %s < $2
  ORDER BY id
  LIMIT $3
)
DELETE FROM %s
WHERE id IN (SELECT id FROM batch)
`, table, timeColumn, timeColumn, table)
	var total int64
	for {
		if err := guard.Check(ctx); err != nil {
			return total, err
		}
		result, err := db.ExecContext(ctx, query, window.Start.UTC(), window.End.UTC(), batchSize)
		if err != nil {
			return total, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return total, err
		}
		total += affected
		if affected < int64(batchSize) {
			return total, nil
		}
	}
}

func (s *OpsCleanupService) tryAcquireLeaderLock(ctx context.Context) (func(), bool) {
	if s == nil {
		return nil, false
	}
	// In simple run mode, assume single instance.
	if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		return nil, true
	}

	key := opsCleanupLeaderLockKeyDefault
	ttl := configuredOpsCleanupLeaderLockTTL(s.cfg)

	// Prefer Redis leader lock when available, but avoid stampeding the DB when Redis is flaky by
	// falling back to a DB advisory lock.
	if s.redisClient != nil {
		ok, err := s.redisClient.SetNX(ctx, key, s.instanceID, ttl).Result()
		if err == nil {
			if !ok {
				return nil, false
			}
			return func() {
				releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if _, releaseErr := opsCleanupReleaseScript.Run(releaseCtx, s.redisClient, []string{key}, s.instanceID).Result(); releaseErr != nil {
					logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] leader lock release failed: %v", releaseErr)
				}
			}, true
		}
		// Redis error: fall back to DB advisory lock.
		s.warnNoRedisOnce.Do(func() {
			logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] leader lock SetNX failed; falling back to DB advisory lock: %v", err)
		})
	} else {
		s.warnNoRedisOnce.Do(func() {
			logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] redis not configured; using DB advisory lock")
		})
	}

	release, ok := tryAcquireDBAdvisoryLock(ctx, s.db, hashAdvisoryLockID(key))
	if !ok {
		return nil, false
	}
	return release, true
}

func configuredOpsCleanupLeaderLockTTL(cfg *config.Config) time.Duration {
	ttl := opsCleanupLeaderLockTTLDefault
	if cfg == nil || cfg.Ops.Cleanup.RunTimeoutSeconds <= 0 {
		return ttl
	}
	runTTL := time.Duration(cfg.Ops.Cleanup.RunTimeoutSeconds)*time.Second + opsCleanupLeaderLockTTLGrace
	if runTTL > ttl {
		return runTTL
	}
	return ttl
}

func (s *OpsCleanupService) recordHeartbeatSuccess(runAt time.Time, duration time.Duration, counts opsCleanupDeletedCounts) {
	if s == nil || s.opsRepo == nil {
		return
	}
	now := time.Now().UTC()
	durMs := duration.Milliseconds()
	result := truncateString(counts.String(), 2048)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.opsRepo.UpsertJobHeartbeat(ctx, &OpsUpsertJobHeartbeatInput{
		JobName:        opsCleanupJobName,
		LastRunAt:      &runAt,
		LastSuccessAt:  &now,
		LastDurationMs: &durMs,
		LastResult:     &result,
	})
}

func (s *OpsCleanupService) recordHeartbeatError(runAt time.Time, duration time.Duration, err error) {
	if s == nil || s.opsRepo == nil || err == nil {
		return
	}
	now := time.Now().UTC()
	durMs := duration.Milliseconds()
	msg := truncateString(err.Error(), 2048)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.opsRepo.UpsertJobHeartbeat(ctx, &OpsUpsertJobHeartbeatInput{
		JobName:        opsCleanupJobName,
		LastRunAt:      &runAt,
		LastErrorAt:    &now,
		LastError:      &msg,
		LastDurationMs: &durMs,
	})
}
