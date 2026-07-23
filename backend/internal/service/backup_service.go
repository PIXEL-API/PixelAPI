package service

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

const (
	settingKeyBackupS3Config = "backup_s3_config"
	settingKeyBackupSchedule = "backup_schedule"
	settingKeyBackupRecords  = "backup_records"
	settingKeyUsageRetention = "usage_cleanup_auto_retention"
	backupEncryptedSecretV1  = "enc:v1:"

	// 在 OPS 04:00 清理前的低峰时段执行；标准 cron 表达式不会在服务启动时立即触发。
	backupExpirationCleanupCronExpr  = "30 3 * * *"
	backupExpirationCleanupBatchSize = 50
	backupExpirationCleanupAttempts  = 3
	backupExpirationCleanupRetryWait = 10 * time.Minute
	backupCompensationTimeout        = 30 * time.Second
)

var (
	ErrBackupS3NotConfigured            = infraerrors.BadRequest("BACKUP_S3_NOT_CONFIGURED", "backup S3 storage is not configured")
	ErrBackupNotFound                   = infraerrors.NotFound("BACKUP_NOT_FOUND", "backup record not found")
	ErrBackupInProgress                 = infraerrors.Conflict("BACKUP_IN_PROGRESS", "a backup is already in progress")
	ErrRestoreInProgress                = infraerrors.Conflict("RESTORE_IN_PROGRESS", "a restore is already in progress")
	ErrBackupRecordsCorrupt             = infraerrors.InternalServer("BACKUP_RECORDS_CORRUPT", "backup records data is corrupted")
	ErrBackupS3ConfigCorrupt            = infraerrors.InternalServer("BACKUP_S3_CONFIG_CORRUPT", "backup S3 config data is corrupted")
	ErrSecretEncryptionKeyNotConfigured = infraerrors.BadRequest(
		"SECRET_ENCRYPTION_KEY_NOT_CONFIGURED",
		"cannot store the S3 secret access key: configure a fixed TOTP_ENCRYPTION_KEY before saving durable credentials",
	)
)

// ─── 接口定义 ───

// DBDumper abstracts database dump/restore operations
type DBDumper interface {
	Dump(ctx context.Context) (io.ReadCloser, error)
	Restore(ctx context.Context, data io.Reader) error
}

// BackupObjectStore abstracts object storage for backup files
type BackupObjectStore interface {
	Upload(ctx context.Context, key string, body io.Reader, contentType string) (sizeBytes int64, err error)
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	PresignURL(ctx context.Context, key string, expiry time.Duration) (string, error)
	HeadBucket(ctx context.Context) error
}

// BackupObjectStoreFactory creates an object store from S3 config
type BackupObjectStoreFactory func(ctx context.Context, cfg *BackupS3Config) (BackupObjectStore, error)

// ─── 数据模型 ───

// BackupS3Config S3 兼容存储配置（支持 Cloudflare R2）
type BackupS3Config struct {
	Endpoint        string `json:"endpoint"` // e.g. https://<account_id>.r2.cloudflarestorage.com
	Region          string `json:"region"`   // R2 用 "auto"
	Bucket          string `json:"bucket"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key,omitempty"` //nolint:revive // field name follows AWS convention
	Prefix          string `json:"prefix"`                      // S3 key 前缀，如 "backups/"
	ForcePathStyle  bool   `json:"force_path_style"`
}

// IsConfigured 检查必要字段是否已配置
func (c *BackupS3Config) IsConfigured() bool {
	return c.Bucket != "" && c.AccessKeyID != "" && c.SecretAccessKey != ""
}

// BackupScheduleConfig 定时备份配置
type BackupScheduleConfig struct {
	Enabled     bool   `json:"enabled"`
	CronExpr    string `json:"cron_expr"`    // cron 表达式，如 "0 2 * * *" 每天凌晨2点
	RetainDays  int    `json:"retain_days"`  // 备份文件过期天数，默认14，0=不自动清理
	RetainCount int    `json:"retain_count"` // 最多保留份数，0=不限制
}

// UsageRetentionConfig controls automatic raw usage_logs archive and cleanup.
type UsageRetentionConfig struct {
	Enabled          bool `json:"enabled"`
	RetainDays       int  `json:"retain_days"`
	RunIntervalHours int  `json:"run_interval_hours"`
	WindowDays       int  `json:"window_days"`
	BackupExpireDays int  `json:"backup_expire_days"`
}

// BackupRecord 备份记录
type BackupRecord struct {
	ID            string `json:"id"`
	Status        string `json:"status"`      // pending, running, completed, failed
	BackupType    string `json:"backup_type"` // postgres
	FileName      string `json:"file_name"`
	S3Key         string `json:"s3_key"`
	SizeBytes     int64  `json:"size_bytes"`
	TriggeredBy   string `json:"triggered_by"` // manual, scheduled
	ErrorMsg      string `json:"error_message,omitempty"`
	StartedAt     string `json:"started_at"`
	FinishedAt    string `json:"finished_at,omitempty"`
	ExpiresAt     string `json:"expires_at,omitempty"`     // 过期时间
	Progress      string `json:"progress,omitempty"`       // "dumping", "uploading", ""
	RestoreStatus string `json:"restore_status,omitempty"` // "", "running", "completed", "failed"
	RestoreError  string `json:"restore_error,omitempty"`
	RestoredAt    string `json:"restored_at,omitempty"`
}

type UsageLogsArchiveInput struct {
	Stream     io.ReadCloser
	StartTime  time.Time
	EndTime    time.Time
	ExpireDays int
}

type DataArchiveInput struct {
	Stream      io.ReadCloser
	FileName    string
	BackupType  string
	TriggeredBy string
	ExpireDays  int
}

// BackupService 数据库备份恢复服务
type BackupService struct {
	settingRepo  SettingRepository
	dbCfg        *config.DatabaseConfig
	usageCleanup config.UsageCleanupConfig
	encryptor    SecretEncryptor
	// false 表示当前密钥由进程启动时临时生成，不能用于持久化可恢复的密文。
	encryptionKeyConfigured bool
	storeFactory            BackupObjectStoreFactory
	dumper                  DBDumper
	taskExecutor            *ClusterTaskExecutor

	opMu      sync.Mutex // 保护 backingUp/restoring 标志
	backingUp bool
	restoring bool

	storeMu sync.Mutex // 保护 store/s3Cfg 缓存
	store   BackupObjectStore
	s3Cfg   *BackupS3Config

	recordsMu   sync.Mutex // 保护 records 的 load/save 操作
	lifecycleMu sync.Mutex // 串行化新操作注册与 Stop/wg.Wait

	cronMu      sync.Mutex
	cronSched   *cron.Cron
	cronEntryID cron.EntryID
	// cronCleanupEntryID 独立于 PostgreSQL 全库备份 schedule，始终按固定周期运行。
	cronCleanupEntryID cron.EntryID

	wg           sync.WaitGroup     // 追踪活跃的备份/恢复 goroutine
	shuttingDown atomic.Bool        // 阻止新备份启动
	bgCtx        context.Context    // 所有后台操作的 parent context
	bgCancel     context.CancelFunc // 取消所有活跃后台操作
}

func NewBackupService(
	settingRepo SettingRepository,
	cfg *config.Config,
	encryptor SecretEncryptor,
	storeFactory BackupObjectStoreFactory,
	dumper DBDumper,
) *BackupService {
	bgCtx, bgCancel := context.WithCancel(context.Background())
	dbCfg := &config.DatabaseConfig{}
	var usageCleanup config.UsageCleanupConfig
	var encryptionKeyConfigured bool
	if cfg != nil {
		dbCfg = &cfg.Database
		usageCleanup = cfg.UsageCleanup
		encryptionKeyConfigured = cfg.Totp.EncryptionKeyConfigured
	}
	return &BackupService{
		settingRepo:             settingRepo,
		dbCfg:                   dbCfg,
		usageCleanup:            usageCleanup,
		encryptor:               encryptor,
		encryptionKeyConfigured: encryptionKeyConfigured,
		storeFactory:            storeFactory,
		dumper:                  dumper,
		bgCtx:                   bgCtx,
		bgCancel:                bgCancel,
	}
}

// Start 启动定时备份调度器并清理孤立记录
func (s *BackupService) Start() {
	s.cronSched = cron.New()
	s.cronSched.Start()
	if err := s.applyExpirationCleanupSchedule(); err != nil {
		logger.LegacyPrintf("service.backup", "[Backup] 注册归档过期清理任务失败: %v", err)
	} else {
		logger.LegacyPrintf("service.backup", "[Backup] 归档过期清理任务已启用: %s", backupExpirationCleanupCronExpr)
	}

	// 清理重启后孤立的 running 记录
	s.recoverStaleRecords()

	// 加载已有的定时配置
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	schedule, err := s.GetSchedule(ctx)
	if err != nil {
		logger.LegacyPrintf("service.backup", "[Backup] 加载定时备份配置失败: %v", err)
		return
	}
	if schedule.Enabled && schedule.CronExpr != "" {
		if err := s.applyCronSchedule(schedule); err != nil {
			logger.LegacyPrintf("service.backup", "[Backup] 应用定时备份配置失败: %v", err)
		}
	}
}

// recoverStaleRecords 启动时将孤立的 running 记录标记为 failed
func (s *BackupService) recoverStaleRecords() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	records, err := s.loadRecords(ctx)
	if err != nil {
		logger.LegacyPrintf("service.backup", "[Backup] 加载孤立备份记录失败: %v", err)
		return
	}
	for i := range records {
		if records[i].Status == "running" {
			records[i].Status = "failed"
			records[i].ErrorMsg = "interrupted by server restart"
			records[i].Progress = ""
			records[i].FinishedAt = time.Now().Format(time.RFC3339)
			if err := s.saveRecord(ctx, &records[i]); err != nil {
				logger.LegacyPrintf("service.backup", "[Backup] 标记孤立备份记录失败: id=%s err=%v", records[i].ID, err)
			} else {
				logger.LegacyPrintf("service.backup", "[Backup] recovered stale running record: %s", records[i].ID)
			}
		}
		if records[i].RestoreStatus == "running" {
			records[i].RestoreStatus = "failed"
			records[i].RestoreError = "interrupted by server restart"
			if err := s.saveRecord(ctx, &records[i]); err != nil {
				logger.LegacyPrintf("service.backup", "[Backup] 标记孤立恢复记录失败: id=%s err=%v", records[i].ID, err)
			} else {
				logger.LegacyPrintf("service.backup", "[Backup] recovered stale restoring record: %s", records[i].ID)
			}
		}
	}
}

// Stop 停止定时备份并等待活跃操作完成
func (s *BackupService) Stop() {
	s.lifecycleMu.Lock()
	s.shuttingDown.Store(true)
	s.lifecycleMu.Unlock()

	s.cronMu.Lock()
	if s.cronSched != nil {
		s.cronSched.Stop()
	}
	s.cronEntryID = 0
	s.cronCleanupEntryID = 0
	s.cronMu.Unlock()
	if s.bgCancel != nil {
		s.bgCancel()
	}

	// 后台 context 已取消；给流式上传/恢复一个有界退出窗口。
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		logger.LegacyPrintf("service.backup", "[Backup] all active operations finished")
	case <-time.After(30 * time.Second):
		logger.LegacyPrintf("service.backup", "[Backup] active operation shutdown timed out after 30s")
	}
}

// ─── S3 配置管理 ───

// EncryptionKeyConfigured reports whether durable secrets can survive a restart.
func (s *BackupService) EncryptionKeyConfigured() bool {
	return s != nil && s.encryptionKeyConfigured
}

func (s *BackupService) GetS3Config(ctx context.Context) (*BackupS3Config, error) {
	cfg, err := s.loadS3Config(ctx)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return &BackupS3Config{}, nil
	}
	// 脱敏返回
	cfg.SecretAccessKey = ""
	return cfg, nil
}

func (s *BackupService) UpdateS3Config(ctx context.Context, cfg BackupS3Config) (*BackupS3Config, error) {
	if !s.tryBeginRun() {
		return nil, infraerrors.ServiceUnavailable("SERVER_SHUTTING_DOWN", "server is shutting down")
	}
	defer s.endRun()
	if err := s.beginBackupCleanup(); err != nil {
		return nil, err
	}
	defer s.finishBackupCleanup()

	old, err := s.loadS3Config(ctx)
	if err != nil {
		return nil, err
	}
	locationChanged := old == nil && backupStorageLocationConfigured(cfg)
	if old != nil {
		locationChanged = backupStorageLocationChanged(*old, cfg)
	}
	if locationChanged {
		records, err := s.loadRecords(ctx)
		if err != nil {
			return nil, fmt.Errorf("load backup records before changing storage location: %w", err)
		}
		for _, record := range records {
			if strings.TrimSpace(record.S3Key) != "" {
				return nil, infraerrors.Conflict(
					"BACKUP_STORAGE_LOCATION_IN_USE",
					"cannot change backup endpoint, region, bucket, prefix, or path style while backup records still reference the current storage",
				)
			}
		}
	}

	// 如果没提供 secret，保留原有明文值；落库前始终重新加密，避免只修改
	// 其他字段时把 loadS3Config 解密后的密钥以明文写回。
	if cfg.SecretAccessKey == "" {
		if old != nil {
			cfg.SecretAccessKey = old.SecretAccessKey
		}
	}
	if cfg.SecretAccessKey != "" {
		if !s.encryptionKeyConfigured {
			return nil, ErrSecretEncryptionKeyNotConfigured
		}
		encrypted, err := s.encryptor.Encrypt(cfg.SecretAccessKey)
		if err != nil {
			return nil, fmt.Errorf("encrypt secret: %w", err)
		}
		cfg.SecretAccessKey = backupEncryptedSecretV1 + encrypted
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal s3 config: %w", err)
	}
	if err := s.settingRepo.Set(ctx, settingKeyBackupS3Config, string(data)); err != nil {
		return nil, fmt.Errorf("save s3 config: %w", err)
	}

	// 清除缓存的 S3 客户端
	s.storeMu.Lock()
	s.store = nil
	s.s3Cfg = nil
	s.storeMu.Unlock()

	cfg.SecretAccessKey = ""
	return &cfg, nil
}

func backupStorageLocationChanged(oldCfg, newCfg BackupS3Config) bool {
	normalizeEndpoint := func(value string) string {
		return strings.TrimRight(strings.TrimSpace(value), "/")
	}
	normalizePrefix := func(value string) string {
		value = strings.TrimRight(value, "/")
		if value == "" {
			return "backups"
		}
		return value
	}
	return normalizeEndpoint(oldCfg.Endpoint) != normalizeEndpoint(newCfg.Endpoint) ||
		strings.TrimSpace(oldCfg.Region) != strings.TrimSpace(newCfg.Region) ||
		strings.TrimSpace(oldCfg.Bucket) != strings.TrimSpace(newCfg.Bucket) ||
		normalizePrefix(oldCfg.Prefix) != normalizePrefix(newCfg.Prefix) ||
		oldCfg.ForcePathStyle != newCfg.ForcePathStyle
}

func backupStorageLocationConfigured(cfg BackupS3Config) bool {
	return strings.TrimSpace(cfg.Endpoint) != "" ||
		strings.TrimSpace(cfg.Region) != "" ||
		strings.TrimSpace(cfg.Bucket) != "" ||
		strings.TrimSpace(cfg.Prefix) != "" ||
		cfg.ForcePathStyle
}

func (s *BackupService) TestS3Connection(ctx context.Context, cfg BackupS3Config) error {
	// 如果没提供 secret，用已保存的
	if cfg.SecretAccessKey == "" {
		old, err := s.loadS3Config(ctx)
		if err != nil {
			return err
		}
		if old != nil {
			cfg.SecretAccessKey = old.SecretAccessKey
		}
	}

	if cfg.Bucket == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return fmt.Errorf("incomplete S3 config: bucket, access_key_id, secret_access_key are required")
	}

	store, err := s.storeFactory(ctx, &cfg)
	if err != nil {
		return err
	}
	return store.HeadBucket(ctx)
}

// ─── 定时备份管理 ───

func (s *BackupService) GetSchedule(ctx context.Context) (*BackupScheduleConfig, error) {
	raw, err := s.settingRepo.GetValue(ctx, settingKeyBackupSchedule)
	if err != nil || raw == "" {
		return &BackupScheduleConfig{}, nil
	}
	var cfg BackupScheduleConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return &BackupScheduleConfig{}, nil
	}
	return &cfg, nil
}

func (s *BackupService) UpdateSchedule(ctx context.Context, cfg BackupScheduleConfig) (*BackupScheduleConfig, error) {
	if cfg.Enabled && cfg.CronExpr == "" {
		return nil, infraerrors.BadRequest("INVALID_CRON", "cron expression is required when schedule is enabled")
	}
	// 验证 cron 表达式
	if cfg.CronExpr != "" {
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		if _, err := parser.Parse(cfg.CronExpr); err != nil {
			return nil, infraerrors.BadRequest("INVALID_CRON", fmt.Sprintf("invalid cron expression: %v", err))
		}
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal schedule config: %w", err)
	}
	if err := s.settingRepo.Set(ctx, settingKeyBackupSchedule, string(data)); err != nil {
		return nil, fmt.Errorf("save schedule config: %w", err)
	}

	// 应用或停止定时任务
	if cfg.Enabled {
		if err := s.applyCronSchedule(&cfg); err != nil {
			return nil, err
		}
	} else {
		s.removeCronSchedule()
	}

	return &cfg, nil
}

// GetUsageRetention returns the raw usage_logs auto archive and cleanup config.
func (s *BackupService) GetUsageRetention(ctx context.Context) (*UsageRetentionConfig, error) {
	defaultCfg := s.defaultUsageRetentionConfig()
	raw, err := s.settingRepo.GetValue(ctx, settingKeyUsageRetention)
	if err != nil || strings.TrimSpace(raw) == "" {
		return &defaultCfg, nil
	}
	var cfg UsageRetentionConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, infraerrors.InternalServer("USAGE_RETENTION_CONFIG_CORRUPT", "usage retention config data is corrupted")
	}
	if err := validateUsageRetentionConfig(cfg, s.usageRetentionMaxWindowDays()); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// UpdateUsageRetention saves the raw usage_logs auto archive and cleanup config.
func (s *BackupService) UpdateUsageRetention(ctx context.Context, cfg UsageRetentionConfig) (*UsageRetentionConfig, error) {
	if err := validateUsageRetentionConfig(cfg, s.usageRetentionMaxWindowDays()); err != nil {
		return nil, err
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal usage retention config: %w", err)
	}
	if err := s.settingRepo.Set(ctx, settingKeyUsageRetention, string(data)); err != nil {
		return nil, fmt.Errorf("save usage retention config: %w", err)
	}
	return &cfg, nil
}

func (s *BackupService) defaultUsageRetentionConfig() UsageRetentionConfig {
	cfg := UsageRetentionConfig{
		Enabled:          false,
		RetainDays:       3,
		RunIntervalHours: 24,
		WindowDays:       1,
		BackupExpireDays: 14,
	}
	if s == nil {
		return cfg
	}
	auto := s.usageCleanup.AutoRetention
	cfg.Enabled = auto.Enabled
	if auto.RetainDays > 0 {
		cfg.RetainDays = auto.RetainDays
	}
	if auto.RunIntervalHours > 0 {
		cfg.RunIntervalHours = auto.RunIntervalHours
	}
	if auto.WindowDays > 0 {
		cfg.WindowDays = auto.WindowDays
	}
	if auto.BackupExpireDays >= 0 {
		cfg.BackupExpireDays = auto.BackupExpireDays
	}
	return cfg
}

func (s *BackupService) usageRetentionMaxWindowDays() int {
	if s == nil || s.usageCleanup.MaxRangeDays <= 0 {
		return 31
	}
	return s.usageCleanup.MaxRangeDays
}

func validateUsageRetentionConfig(cfg UsageRetentionConfig, maxWindowDays int) error {
	if cfg.RetainDays <= 0 {
		return infraerrors.BadRequest("INVALID_USAGE_RETENTION_RETAIN_DAYS", "retain_days must be positive")
	}
	if cfg.RunIntervalHours <= 0 {
		return infraerrors.BadRequest("INVALID_USAGE_RETENTION_RUN_INTERVAL", "run_interval_hours must be positive")
	}
	if cfg.WindowDays <= 0 {
		return infraerrors.BadRequest("INVALID_USAGE_RETENTION_WINDOW_DAYS", "window_days must be positive")
	}
	if cfg.BackupExpireDays < 0 {
		return infraerrors.BadRequest("INVALID_USAGE_RETENTION_BACKUP_EXPIRE_DAYS", "backup_expire_days must be non-negative")
	}
	if maxWindowDays <= 0 {
		maxWindowDays = 31
	}
	if cfg.WindowDays > maxWindowDays {
		return infraerrors.BadRequest("INVALID_USAGE_RETENTION_WINDOW_DAYS", fmt.Sprintf("window_days must be less than or equal to %d", maxWindowDays))
	}
	return nil
}

func (s *BackupService) applyCronSchedule(cfg *BackupScheduleConfig) error {
	s.cronMu.Lock()
	defer s.cronMu.Unlock()

	if s.cronSched == nil {
		return fmt.Errorf("cron scheduler not initialized")
	}

	// 移除旧任务
	if s.cronEntryID != 0 {
		s.cronSched.Remove(s.cronEntryID)
		s.cronEntryID = 0
	}

	entryID, err := s.cronSched.AddFunc(cfg.CronExpr, func() {
		s.runScheduledBackup()
	})
	if err != nil {
		return infraerrors.BadRequest("INVALID_CRON", fmt.Sprintf("failed to schedule: %v", err))
	}
	s.cronEntryID = entryID
	logger.LegacyPrintf("service.backup", "[Backup] 定时备份已启用: %s", cfg.CronExpr)
	return nil
}

func (s *BackupService) removeCronSchedule() {
	s.cronMu.Lock()
	defer s.cronMu.Unlock()
	if s.cronSched != nil && s.cronEntryID != 0 {
		s.cronSched.Remove(s.cronEntryID)
		s.cronEntryID = 0
		logger.LegacyPrintf("service.backup", "[Backup] 定时备份已停用")
	}
}

func (s *BackupService) runScheduledBackup() {
	ctx, cancel := context.WithTimeout(s.bgCtx, 30*time.Minute)
	defer cancel()
	_, err := s.taskExecutor.Run(ctx, "scheduled_database_backup", func(taskCtx context.Context, guard *ClusterLeaseGuard) error {
		s.runScheduledBackupLeased(taskCtx, guard)
		return nil
	})
	if err != nil {
		logger.LegacyPrintf("service.backup", "[Backup] 定时备份租约执行失败: %v", err)
	}
}

func (s *BackupService) runScheduledBackupLeased(ctx context.Context, guard *ClusterLeaseGuard) {
	if !s.tryBeginRun() {
		return
	}
	defer s.endRun()

	if err := guard.Check(ctx); err != nil {
		logger.LegacyPrintf("service.backup", "[Backup] 定时备份租约已失效: %v", err)
		return
	}

	// 读取定时备份配置中的过期天数
	schedule, _ := s.GetSchedule(ctx)
	expireDays := 14 // 默认14天过期
	if schedule != nil && schedule.RetainDays > 0 {
		expireDays = schedule.RetainDays
	}

	logger.LegacyPrintf("service.backup", "[Backup] 开始执行定时备份, 过期天数: %d", expireDays)
	record, err := s.CreateBackup(ctx, "scheduled", expireDays)
	if err != nil {
		if errors.Is(err, ErrBackupInProgress) {
			logger.LegacyPrintf("service.backup", "[Backup] 定时备份跳过: 已有备份正在进行中")
		} else {
			logger.LegacyPrintf("service.backup", "[Backup] 定时备份失败: %v", err)
		}
		return
	}
	logger.LegacyPrintf("service.backup", "[Backup] 定时备份完成: id=%s size=%d", record.ID, record.SizeBytes)

	// 定时备份的份数/天数策略只适用于 PostgreSQL 全库备份。
	if schedule == nil {
		return
	}
	if err := guard.Check(ctx); err != nil {
		logger.LegacyPrintf("service.backup", "[Backup] 清理前租约已失效: %v", err)
		return
	}
	if err := s.cleanupOldBackups(ctx, schedule); err != nil {
		logger.LegacyPrintf("service.backup", "[Backup] 清理过期备份失败: %v", err)
	}
}

func (s *BackupService) applyExpirationCleanupSchedule() error {
	s.cronMu.Lock()
	defer s.cronMu.Unlock()

	if s.cronSched == nil {
		return fmt.Errorf("cron scheduler not initialized")
	}
	if s.cronCleanupEntryID != 0 {
		s.cronSched.Remove(s.cronCleanupEntryID)
		s.cronCleanupEntryID = 0
	}
	entryID, err := s.cronSched.AddFunc(backupExpirationCleanupCronExpr, s.runScheduledExpirationCleanup)
	if err != nil {
		return fmt.Errorf("schedule backup expiration cleanup: %w", err)
	}
	s.cronCleanupEntryID = entryID
	return nil
}

func (s *BackupService) runScheduledExpirationCleanup() {
	ctx, cancel := context.WithTimeout(s.bgCtx, 30*time.Minute)
	defer cancel()
	_, err := s.taskExecutor.Run(ctx, "backup_expiration_cleanup", func(taskCtx context.Context, guard *ClusterLeaseGuard) error {
		if err := guard.Check(taskCtx); err != nil {
			return err
		}
		s.runScheduledExpirationCleanupLeased(taskCtx)
		return nil
	})
	if err != nil {
		logger.LegacyPrintf("service.backup", "[Backup] 归档过期清理租约执行失败: %v", err)
	}
}

func (s *BackupService) runScheduledExpirationCleanupLeased(ctx context.Context) {
	if !s.tryBeginRun() {
		return
	}
	defer s.endRun()

	if err := s.cleanupExpiredBackupsWithRetry(ctx, backupExpirationCleanupAttempts, backupExpirationCleanupRetryWait); err != nil {
		switch {
		case errors.Is(err, ErrBackupInProgress):
			logger.LegacyPrintf("service.backup", "[Backup] 归档过期清理跳过: 备份或归档正在运行")
		case errors.Is(err, ErrRestoreInProgress):
			logger.LegacyPrintf("service.backup", "[Backup] 归档过期清理跳过: 恢复正在运行")
		default:
			logger.LegacyPrintf("service.backup", "[Backup] 归档过期清理失败: %v", err)
		}
		return
	}
	logger.LegacyPrintf("service.backup", "[Backup] 归档过期清理执行完成")
}

func (s *BackupService) cleanupExpiredBackupsWithRetry(ctx context.Context, attempts int, retryWait time.Duration) error {
	if attempts <= 0 {
		return fmt.Errorf("expiration cleanup attempts must be positive")
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		err := s.cleanupExpiredBackups(ctx)
		if err == nil || (!errors.Is(err, ErrBackupInProgress) && !errors.Is(err, ErrRestoreInProgress)) {
			return err
		}
		if attempt == attempts {
			return fmt.Errorf("expiration cleanup remained busy after %d attempts: %w", attempts, err)
		}
		logger.LegacyPrintf(
			"service.backup",
			"[Backup] 归档过期清理等待重试: attempt=%d/%d wait=%s err=%v",
			attempt,
			attempts,
			retryWait,
			err,
		)
		timer := time.NewTimer(retryWait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

// tryBeginRun 将所有同步、异步和定时操作的 wg.Add 与 Stop/wg.Wait 串行化。
func (s *BackupService) tryBeginRun() bool {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.shuttingDown.Load() {
		return false
	}
	s.wg.Add(1)
	return true
}

func (s *BackupService) endRun() {
	s.wg.Done()
}

// ─── 备份/恢复核心 ───

// CreateBackup 创建全量数据库备份并上传到 S3（流式处理）
// expireDays: 备份过期天数，0=永不过期，默认14天
func (s *BackupService) CreateBackup(ctx context.Context, triggeredBy string, expireDays int) (*BackupRecord, error) {
	if !s.tryBeginRun() {
		return nil, infraerrors.ServiceUnavailable("SERVER_SHUTTING_DOWN", "server is shutting down")
	}
	defer s.endRun()

	s.opMu.Lock()
	if s.backingUp {
		s.opMu.Unlock()
		return nil, ErrBackupInProgress
	}
	if s.restoring {
		s.opMu.Unlock()
		return nil, ErrRestoreInProgress
	}
	s.backingUp = true
	s.opMu.Unlock()
	defer func() {
		s.opMu.Lock()
		s.backingUp = false
		s.opMu.Unlock()
	}()

	s3Cfg, err := s.loadS3Config(ctx)
	if err != nil {
		return nil, err
	}
	if s3Cfg == nil || !s3Cfg.IsConfigured() {
		return nil, ErrBackupS3NotConfigured
	}

	objectStore, err := s.getOrCreateStore(ctx, s3Cfg)
	if err != nil {
		return nil, fmt.Errorf("init object store: %w", err)
	}

	now := time.Now()
	backupID := uuid.New().String()[:8]
	fileName := fmt.Sprintf("%s_%s.sql.gz", s.dbCfg.DBName, now.Format("20060102_150405"))
	s3Key := s.buildS3Key(s3Cfg, fileName)

	var expiresAt string
	if expireDays > 0 {
		expiresAt = now.AddDate(0, 0, expireDays).Format(time.RFC3339)
	}

	record := &BackupRecord{
		ID:          backupID,
		Status:      "running",
		BackupType:  "postgres",
		FileName:    fileName,
		S3Key:       s3Key,
		TriggeredBy: triggeredBy,
		StartedAt:   now.Format(time.RFC3339),
		ExpiresAt:   expiresAt,
	}

	// 流式执行: pg_dump -> gzip -> S3 upload
	dumpReader, err := s.dumper.Dump(ctx)
	if err != nil {
		record.Status = "failed"
		record.ErrorMsg = fmt.Sprintf("pg_dump failed: %v", err)
		record.FinishedAt = time.Now().Format(time.RFC3339)
		if saveErr := s.saveRecord(ctx, record); saveErr != nil {
			logger.LegacyPrintf("service.backup", "[Backup] 保存失败备份记录失败: %v", saveErr)
		}
		return record, fmt.Errorf("pg_dump: %w", err)
	}

	// 使用 io.Pipe 将 gzip 压缩数据流式传递给 S3 上传
	pr, pw := io.Pipe()
	gzipDone := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				pw.CloseWithError(fmt.Errorf("gzip goroutine panic: %v", r)) //nolint:errcheck
				gzipDone <- fmt.Errorf("gzip goroutine panic: %v", r)
			}
		}()
		gzWriter := gzip.NewWriter(pw)
		var gzErr error
		_, gzErr = io.Copy(gzWriter, dumpReader)
		if closeErr := gzWriter.Close(); closeErr != nil && gzErr == nil {
			gzErr = closeErr
		}
		if closeErr := dumpReader.Close(); closeErr != nil && gzErr == nil {
			gzErr = closeErr
		}
		if gzErr != nil {
			_ = pw.CloseWithError(gzErr)
		} else {
			_ = pw.Close()
		}
		gzipDone <- gzErr
	}()

	contentType := "application/gzip"
	sizeBytes, err := objectStore.Upload(ctx, s3Key, pr, contentType)
	if err != nil {
		_ = pr.CloseWithError(err) // 确保 gzip goroutine 不会悬挂
		gzErr := <-gzipDone        // 安全等待 gzip goroutine 完成
		record.Status = "failed"
		errMsg := fmt.Sprintf("S3 upload failed: %v", err)
		if gzErr != nil {
			errMsg = fmt.Sprintf("gzip/dump failed: %v", gzErr)
		}
		record.ErrorMsg = errMsg
		record.FinishedAt = time.Now().Format(time.RFC3339)
		if saveErr := s.saveRecord(ctx, record); saveErr != nil {
			logger.LegacyPrintf("service.backup", "[Backup] 保存上传失败备份记录失败: %v", saveErr)
		}
		return record, fmt.Errorf("backup upload: %w", err)
	}
	if gzErr := <-gzipDone; gzErr != nil {
		record.Status = "failed"
		record.ErrorMsg = fmt.Sprintf("gzip/dump failed: %v", gzErr)
		record.FinishedAt = time.Now().Format(time.RFC3339)
		archiveErr := fmt.Errorf("backup gzip/dump: %w", gzErr)
		return record, compensateUploadedObject(ctx, objectStore, record.S3Key, archiveErr)
	}

	record.SizeBytes = sizeBytes
	record.Status = "completed"
	record.FinishedAt = time.Now().Format(time.RFC3339)
	if err := s.saveCompletedRecordOrCompensate(ctx, objectStore, record); err != nil {
		return record, err
	}

	return record, nil
}

func (s *BackupService) CreateUsageLogsArchive(ctx context.Context, input UsageLogsArchiveInput) (*BackupRecord, error) {
	if input.Stream == nil {
		return nil, infraerrors.BadRequest("USAGE_LOG_ARCHIVE_EMPTY_STREAM", "usage log archive stream is required")
	}
	if !input.EndTime.After(input.StartTime) {
		_ = input.Stream.Close()
		return nil, infraerrors.BadRequest("USAGE_LOG_ARCHIVE_INVALID_RANGE", "usage log archive range is invalid")
	}
	{
		fileName := fmt.Sprintf("usage_logs_%s_%s.ndjson.gz", input.StartTime.UTC().Format("20060102_150405"), input.EndTime.UTC().Format("20060102_150405"))
		return s.CreateDataArchive(ctx, DataArchiveInput{
			Stream:      input.Stream,
			FileName:    fileName,
			BackupType:  "usage_logs_archive",
			TriggeredBy: "usage_cleanup_auto",
			ExpireDays:  input.ExpireDays,
		})
	}
}

// StartBackup 异步创建备份，立即返回 running 状态的记录

// CreateDataArchive gzip-compresses a newline-delimited export stream and uploads it as a backup record.
func (s *BackupService) CreateDataArchive(ctx context.Context, input DataArchiveInput) (*BackupRecord, error) {
	if input.Stream == nil {
		return nil, infraerrors.BadRequest("DATA_ARCHIVE_EMPTY_STREAM", "data archive stream is required")
	}
	defer func() { _ = input.Stream.Close() }()
	input.FileName = strings.TrimSpace(input.FileName)
	input.BackupType = strings.TrimSpace(input.BackupType)
	input.TriggeredBy = strings.TrimSpace(input.TriggeredBy)
	if input.FileName == "" {
		return nil, infraerrors.BadRequest("DATA_ARCHIVE_EMPTY_FILE_NAME", "data archive file name is required")
	}
	if input.BackupType == "" {
		return nil, infraerrors.BadRequest("DATA_ARCHIVE_EMPTY_TYPE", "data archive backup type is required")
	}
	if input.TriggeredBy == "" {
		input.TriggeredBy = "system"
	}
	if !s.tryBeginRun() {
		return nil, infraerrors.ServiceUnavailable("SERVER_SHUTTING_DOWN", "server is shutting down")
	}
	defer s.endRun()

	s.opMu.Lock()
	if s.backingUp {
		s.opMu.Unlock()
		return nil, ErrBackupInProgress
	}
	if s.restoring {
		s.opMu.Unlock()
		return nil, ErrRestoreInProgress
	}
	s.backingUp = true
	s.opMu.Unlock()
	defer func() {
		s.opMu.Lock()
		s.backingUp = false
		s.opMu.Unlock()
	}()

	s3Cfg, err := s.loadS3Config(ctx)
	if err != nil {
		return nil, err
	}
	if s3Cfg == nil || !s3Cfg.IsConfigured() {
		return nil, ErrBackupS3NotConfigured
	}
	objectStore, err := s.getOrCreateStore(ctx, s3Cfg)
	if err != nil {
		return nil, fmt.Errorf("init object store: %w", err)
	}

	now := time.Now()
	record := &BackupRecord{
		ID:          uuid.New().String()[:8],
		Status:      "running",
		BackupType:  input.BackupType,
		FileName:    input.FileName,
		S3Key:       s.buildS3Key(s3Cfg, input.FileName),
		TriggeredBy: input.TriggeredBy,
		StartedAt:   now.Format(time.RFC3339),
	}
	if input.ExpireDays > 0 {
		record.ExpiresAt = now.AddDate(0, 0, input.ExpireDays).Format(time.RFC3339)
	}

	pr, pw := io.Pipe()
	gzipDone := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("%s archive gzip panic: %v", input.BackupType, r)
				_ = pw.CloseWithError(err)
				gzipDone <- err
			}
		}()
		gzWriter := gzip.NewWriter(pw)
		var gzErr error
		_, gzErr = io.Copy(gzWriter, input.Stream)
		if closeErr := gzWriter.Close(); closeErr != nil && gzErr == nil {
			gzErr = closeErr
		}
		if gzErr != nil {
			_ = pw.CloseWithError(gzErr)
		} else {
			_ = pw.Close()
		}
		gzipDone <- gzErr
	}()

	sizeBytes, err := objectStore.Upload(ctx, record.S3Key, pr, "application/gzip")
	if err != nil {
		_ = pr.CloseWithError(err)
		gzErr := <-gzipDone
		record.Status = "failed"
		record.ErrorMsg = fmt.Sprintf("S3 upload failed: %v", err)
		if gzErr != nil {
			record.ErrorMsg = fmt.Sprintf("gzip/archive failed: %v", gzErr)
		}
		record.FinishedAt = time.Now().Format(time.RFC3339)
		if saveErr := s.saveRecord(ctx, record); saveErr != nil {
			logger.LegacyPrintf("service.backup", "[Backup] 保存归档上传失败状态失败: %v", saveErr)
		}
		return record, fmt.Errorf("%s archive upload: %w", input.BackupType, err)
	}
	if gzErr := <-gzipDone; gzErr != nil {
		record.Status = "failed"
		record.ErrorMsg = fmt.Sprintf("gzip/archive failed: %v", gzErr)
		record.FinishedAt = time.Now().Format(time.RFC3339)
		archiveErr := fmt.Errorf("%s archive gzip: %w", input.BackupType, gzErr)
		return record, compensateUploadedObject(ctx, objectStore, record.S3Key, archiveErr)
	}
	record.SizeBytes = sizeBytes
	record.Status = "completed"
	record.FinishedAt = time.Now().Format(time.RFC3339)
	if err := s.saveCompletedRecordOrCompensate(ctx, objectStore, record); err != nil {
		return record, err
	}
	return record, nil
}

func (s *BackupService) StartBackup(ctx context.Context, triggeredBy string, expireDays int) (*BackupRecord, error) {
	if !s.tryBeginRun() {
		return nil, infraerrors.ServiceUnavailable("SERVER_SHUTTING_DOWN", "server is shutting down")
	}
	runOwned := true
	defer func() {
		if runOwned {
			s.endRun()
		}
	}()

	s.opMu.Lock()
	if s.backingUp {
		s.opMu.Unlock()
		return nil, ErrBackupInProgress
	}
	if s.restoring {
		s.opMu.Unlock()
		return nil, ErrRestoreInProgress
	}
	s.backingUp = true
	s.opMu.Unlock()

	// 初始化阶段出错时自动重置标志
	launched := false
	defer func() {
		if !launched {
			s.opMu.Lock()
			s.backingUp = false
			s.opMu.Unlock()
		}
	}()

	// 在返回前加载 S3 配置和创建 store，避免 goroutine 中配置被修改
	s3Cfg, err := s.loadS3Config(ctx)
	if err != nil {
		return nil, err
	}
	if s3Cfg == nil || !s3Cfg.IsConfigured() {
		return nil, ErrBackupS3NotConfigured
	}

	objectStore, err := s.getOrCreateStore(ctx, s3Cfg)
	if err != nil {
		return nil, fmt.Errorf("init object store: %w", err)
	}

	now := time.Now()
	backupID := uuid.New().String()[:8]
	fileName := fmt.Sprintf("%s_%s.sql.gz", s.dbCfg.DBName, now.Format("20060102_150405"))
	s3Key := s.buildS3Key(s3Cfg, fileName)

	var expiresAt string
	if expireDays > 0 {
		expiresAt = now.AddDate(0, 0, expireDays).Format(time.RFC3339)
	}

	record := &BackupRecord{
		ID:          backupID,
		Status:      "running",
		BackupType:  "postgres",
		FileName:    fileName,
		S3Key:       s3Key,
		TriggeredBy: triggeredBy,
		StartedAt:   now.Format(time.RFC3339),
		ExpiresAt:   expiresAt,
		Progress:    "pending",
	}

	if err := s.saveRecord(ctx, record); err != nil {
		return nil, fmt.Errorf("save initial record: %w", err)
	}

	launched = true
	// 在启动 goroutine 前完成拷贝，避免数据竞争
	result := *record

	go func() {
		defer s.endRun()
		defer func() {
			s.opMu.Lock()
			s.backingUp = false
			s.opMu.Unlock()
		}()
		defer func() {
			if r := recover(); r != nil {
				logger.LegacyPrintf("service.backup", "[Backup] panic recovered: %v", r)
				record.Status = "failed"
				record.ErrorMsg = fmt.Sprintf("internal panic: %v", r)
				record.Progress = ""
				record.FinishedAt = time.Now().Format(time.RFC3339)
				if saveErr := s.saveRecord(context.Background(), record); saveErr != nil {
					logger.LegacyPrintf("service.backup", "[Backup] 保存 panic 失败状态失败: %v", saveErr)
				}
			}
		}()
		s.executeBackup(record, objectStore)
	}()
	runOwned = false

	return &result, nil
}

// executeBackup 后台执行备份（独立于 HTTP context）
func (s *BackupService) executeBackup(record *BackupRecord, objectStore BackupObjectStore) {
	ctx, cancel := context.WithTimeout(s.bgCtx, 30*time.Minute)
	defer cancel()

	// 阶段1: pg_dump
	record.Progress = "dumping"
	if saveErr := s.saveRecord(ctx, record); saveErr != nil {
		logger.LegacyPrintf("service.backup", "[Backup] 保存备份进度 dumping 失败: %v", saveErr)
	}

	dumpReader, err := s.dumper.Dump(ctx)
	if err != nil {
		record.Status = "failed"
		record.ErrorMsg = fmt.Sprintf("pg_dump failed: %v", err)
		record.Progress = ""
		record.FinishedAt = time.Now().Format(time.RFC3339)
		if saveErr := s.saveRecord(context.Background(), record); saveErr != nil {
			logger.LegacyPrintf("service.backup", "[Backup] 保存异步 pg_dump 失败状态失败: %v", saveErr)
		}
		return
	}

	// 阶段2: gzip + upload
	record.Progress = "uploading"
	if saveErr := s.saveRecord(ctx, record); saveErr != nil {
		logger.LegacyPrintf("service.backup", "[Backup] 保存备份进度 uploading 失败: %v", saveErr)
	}

	pr, pw := io.Pipe()
	gzipDone := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				pw.CloseWithError(fmt.Errorf("gzip goroutine panic: %v", r)) //nolint:errcheck
				gzipDone <- fmt.Errorf("gzip goroutine panic: %v", r)
			}
		}()
		gzWriter := gzip.NewWriter(pw)
		var gzErr error
		_, gzErr = io.Copy(gzWriter, dumpReader)
		if closeErr := gzWriter.Close(); closeErr != nil && gzErr == nil {
			gzErr = closeErr
		}
		if closeErr := dumpReader.Close(); closeErr != nil && gzErr == nil {
			gzErr = closeErr
		}
		if gzErr != nil {
			_ = pw.CloseWithError(gzErr)
		} else {
			_ = pw.Close()
		}
		gzipDone <- gzErr
	}()

	contentType := "application/gzip"
	sizeBytes, err := objectStore.Upload(ctx, record.S3Key, pr, contentType)
	if err != nil {
		_ = pr.CloseWithError(err) // 确保 gzip goroutine 不会悬挂
		gzErr := <-gzipDone        // 安全等待 gzip goroutine 完成
		record.Status = "failed"
		errMsg := fmt.Sprintf("S3 upload failed: %v", err)
		if gzErr != nil {
			errMsg = fmt.Sprintf("gzip/dump failed: %v", gzErr)
		}
		record.ErrorMsg = errMsg
		record.Progress = ""
		record.FinishedAt = time.Now().Format(time.RFC3339)
		if saveErr := s.saveRecord(context.Background(), record); saveErr != nil {
			logger.LegacyPrintf("service.backup", "[Backup] 保存异步上传失败状态失败: %v", saveErr)
		}
		return
	}
	if gzErr := <-gzipDone; gzErr != nil {
		record.Status = "failed"
		record.ErrorMsg = fmt.Sprintf("gzip/dump failed: %v", gzErr)
		record.Progress = ""
		record.FinishedAt = time.Now().Format(time.RFC3339)
		cleanupErr := compensateUploadedObject(ctx, objectStore, record.S3Key, fmt.Errorf("backup gzip/dump: %w", gzErr))
		record.ErrorMsg = cleanupErr.Error()
		if saveErr := s.saveRecord(context.Background(), record); saveErr != nil {
			logger.LegacyPrintf("service.backup", "[Backup] 保存异步 gzip 失败状态失败: %v", saveErr)
		}
		return
	}

	record.SizeBytes = sizeBytes
	record.Status = "completed"
	record.Progress = ""
	record.FinishedAt = time.Now().Format(time.RFC3339)
	if err := s.saveCompletedRecordOrCompensate(context.Background(), objectStore, record); err != nil {
		logger.LegacyPrintf("service.backup", "[Backup] 保存备份记录失败，已将备份标记为失败并尝试补偿: %v", err)
		// 初始 running 记录已经存在；若存储故障短暂恢复，尽力持久化失败状态。
		if saveErr := s.saveRecord(context.Background(), record); saveErr != nil {
			logger.LegacyPrintf("service.backup", "[Backup] 保存补偿失败状态失败: %v", saveErr)
		}
	}
}

// RestoreBackup 从 S3 下载备份并流式恢复到数据库
func (s *BackupService) RestoreBackup(ctx context.Context, backupID string) error {
	if !s.tryBeginRun() {
		return infraerrors.ServiceUnavailable("SERVER_SHUTTING_DOWN", "server is shutting down")
	}
	defer s.endRun()

	s.opMu.Lock()
	if s.backingUp {
		s.opMu.Unlock()
		return ErrBackupInProgress
	}
	if s.restoring {
		s.opMu.Unlock()
		return ErrRestoreInProgress
	}
	s.restoring = true
	s.opMu.Unlock()
	defer func() {
		s.opMu.Lock()
		s.restoring = false
		s.opMu.Unlock()
	}()

	record, err := s.GetBackupRecord(ctx, backupID)
	if err != nil {
		return err
	}
	if record.Status != "completed" {
		return infraerrors.BadRequest("BACKUP_NOT_COMPLETED", "can only restore from a completed backup")
	}
	if record.BackupType != "" && record.BackupType != "postgres" {
		return infraerrors.BadRequest("BACKUP_TYPE_NOT_RESTORABLE", "only full postgres backups can be restored automatically")
	}

	s3Cfg, err := s.loadS3Config(ctx)
	if err != nil {
		return err
	}
	objectStore, err := s.getOrCreateStore(ctx, s3Cfg)
	if err != nil {
		return fmt.Errorf("init object store: %w", err)
	}

	// 从 S3 流式下载
	body, err := objectStore.Download(ctx, record.S3Key)
	if err != nil {
		return fmt.Errorf("S3 download failed: %w", err)
	}
	defer func() { _ = body.Close() }()

	// 流式解压 gzip -> psql（不将全部数据加载到内存）
	gzReader, err := gzip.NewReader(body)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer func() { _ = gzReader.Close() }()

	// 流式恢复
	if err := s.dumper.Restore(ctx, gzReader); err != nil {
		return fmt.Errorf("pg restore: %w", err)
	}

	return nil
}

// StartRestore 异步恢复备份，立即返回
func (s *BackupService) StartRestore(ctx context.Context, backupID string) (*BackupRecord, error) {
	if !s.tryBeginRun() {
		return nil, infraerrors.ServiceUnavailable("SERVER_SHUTTING_DOWN", "server is shutting down")
	}
	runOwned := true
	defer func() {
		if runOwned {
			s.endRun()
		}
	}()

	s.opMu.Lock()
	if s.backingUp {
		s.opMu.Unlock()
		return nil, ErrBackupInProgress
	}
	if s.restoring {
		s.opMu.Unlock()
		return nil, ErrRestoreInProgress
	}
	s.restoring = true
	s.opMu.Unlock()

	// 初始化阶段出错时自动重置标志
	launched := false
	defer func() {
		if !launched {
			s.opMu.Lock()
			s.restoring = false
			s.opMu.Unlock()
		}
	}()

	record, err := s.GetBackupRecord(ctx, backupID)
	if err != nil {
		return nil, err
	}
	if record.Status != "completed" {
		return nil, infraerrors.BadRequest("BACKUP_NOT_COMPLETED", "can only restore from a completed backup")
	}
	if record.BackupType != "" && record.BackupType != "postgres" {
		return nil, infraerrors.BadRequest("BACKUP_TYPE_NOT_RESTORABLE", "only full postgres backups can be restored automatically")
	}

	s3Cfg, err := s.loadS3Config(ctx)
	if err != nil {
		return nil, err
	}
	objectStore, err := s.getOrCreateStore(ctx, s3Cfg)
	if err != nil {
		return nil, fmt.Errorf("init object store: %w", err)
	}

	record.RestoreStatus = "running"
	if err := s.saveRecord(ctx, record); err != nil {
		return nil, fmt.Errorf("save restore status: %w", err)
	}

	launched = true
	result := *record

	go func() {
		defer s.endRun()
		defer func() {
			s.opMu.Lock()
			s.restoring = false
			s.opMu.Unlock()
		}()
		defer func() {
			if r := recover(); r != nil {
				logger.LegacyPrintf("service.backup", "[Backup] restore panic recovered: %v", r)
				record.RestoreStatus = "failed"
				record.RestoreError = fmt.Sprintf("internal panic: %v", r)
				if saveErr := s.saveRecord(context.Background(), record); saveErr != nil {
					logger.LegacyPrintf("service.backup", "[Backup] 保存恢复 panic 失败状态失败: %v", saveErr)
				}
			}
		}()
		s.executeRestore(record, objectStore)
	}()
	runOwned = false

	return &result, nil
}

// executeRestore 后台执行恢复
func (s *BackupService) executeRestore(record *BackupRecord, objectStore BackupObjectStore) {
	ctx, cancel := context.WithTimeout(s.bgCtx, 30*time.Minute)
	defer cancel()

	body, err := objectStore.Download(ctx, record.S3Key)
	if err != nil {
		record.RestoreStatus = "failed"
		record.RestoreError = fmt.Sprintf("S3 download failed: %v", err)
		if saveErr := s.saveRecord(context.Background(), record); saveErr != nil {
			logger.LegacyPrintf("service.backup", "[Backup] 保存恢复下载失败状态失败: %v", saveErr)
		}
		return
	}
	defer func() { _ = body.Close() }()

	gzReader, err := gzip.NewReader(body)
	if err != nil {
		record.RestoreStatus = "failed"
		record.RestoreError = fmt.Sprintf("gzip reader: %v", err)
		if saveErr := s.saveRecord(context.Background(), record); saveErr != nil {
			logger.LegacyPrintf("service.backup", "[Backup] 保存恢复 gzip 失败状态失败: %v", saveErr)
		}
		return
	}
	defer func() { _ = gzReader.Close() }()

	if err := s.dumper.Restore(ctx, gzReader); err != nil {
		record.RestoreStatus = "failed"
		record.RestoreError = fmt.Sprintf("pg restore: %v", err)
		if saveErr := s.saveRecord(context.Background(), record); saveErr != nil {
			logger.LegacyPrintf("service.backup", "[Backup] 保存恢复失败状态失败: %v", saveErr)
		}
		return
	}

	record.RestoreStatus = "completed"
	record.RestoredAt = time.Now().Format(time.RFC3339)
	if err := s.saveRecord(context.Background(), record); err != nil {
		logger.LegacyPrintf("service.backup", "[Backup] 保存恢复记录失败: %v", err)
	}
}

// ─── 备份记录管理 ───

func (s *BackupService) ListBackups(ctx context.Context) ([]BackupRecord, error) {
	records, err := s.loadRecords(ctx)
	if err != nil {
		return nil, err
	}
	// 倒序返回（最新在前）
	sort.Slice(records, func(i, j int) bool {
		return records[i].StartedAt > records[j].StartedAt
	})
	return records, nil
}

func (s *BackupService) GetBackupRecord(ctx context.Context, backupID string) (*BackupRecord, error) {
	records, err := s.loadRecords(ctx)
	if err != nil {
		return nil, err
	}
	for i := range records {
		if records[i].ID == backupID {
			return &records[i], nil
		}
	}
	return nil, ErrBackupNotFound
}

func (s *BackupService) DeleteBackup(ctx context.Context, backupID string) error {
	if !s.tryBeginRun() {
		return infraerrors.ServiceUnavailable("SERVER_SHUTTING_DOWN", "server is shutting down")
	}
	defer s.endRun()

	if err := s.beginBackupCleanup(); err != nil {
		return err
	}
	defer s.finishBackupCleanup()

	s.recordsMu.Lock()
	defer s.recordsMu.Unlock()

	records, err := s.loadRecordsLocked(ctx)
	if err != nil {
		return err
	}

	var found *BackupRecord
	var remaining []BackupRecord
	for i := range records {
		if records[i].ID == backupID {
			found = &records[i]
		} else {
			remaining = append(remaining, records[i])
		}
	}
	if found == nil {
		return ErrBackupNotFound
	}

	// 有对象键时必须先确认 S3 删除成功，防止对象仍存在但记录已经丢失。
	if strings.TrimSpace(found.S3Key) != "" {
		if err := s.deleteS3Object(ctx, found.S3Key); err != nil {
			return fmt.Errorf("delete backup object for record %s: %w", found.ID, err)
		}
	}

	return s.saveRecordsLocked(ctx, remaining)
}

// GetBackupDownloadURL 获取备份文件预签名下载 URL
func (s *BackupService) GetBackupDownloadURL(ctx context.Context, backupID string) (string, error) {
	record, err := s.GetBackupRecord(ctx, backupID)
	if err != nil {
		return "", err
	}
	if record.Status != "completed" {
		return "", infraerrors.BadRequest("BACKUP_NOT_COMPLETED", "backup is not completed")
	}

	s3Cfg, err := s.loadS3Config(ctx)
	if err != nil {
		return "", err
	}
	objectStore, err := s.getOrCreateStore(ctx, s3Cfg)
	if err != nil {
		return "", err
	}

	url, err := objectStore.PresignURL(ctx, record.S3Key, 1*time.Hour)
	if err != nil {
		return "", fmt.Errorf("presign url: %w", err)
	}
	return url, nil
}

// ─── 内部方法 ───

func (s *BackupService) loadS3Config(ctx context.Context) (*BackupS3Config, error) {
	raw, err := s.settingRepo.GetValue(ctx, settingKeyBackupS3Config)
	if errors.Is(err, ErrSettingNotFound) {
		return nil, nil //nolint:nilnil // no config is a valid state
	}
	if err != nil {
		return nil, fmt.Errorf("load S3 config setting: %w", err)
	}
	if strings.TrimSpace(raw) == "" {
		return nil, ErrBackupS3ConfigCorrupt
	}
	var cfg BackupS3Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, ErrBackupS3ConfigCorrupt
	}
	// 新格式带显式版本标记：解密失败说明密钥变化或数据损坏，必须快速失败，
	// 不能把密文继续当成 SecretAccessKey 传给对象存储。
	if cfg.SecretAccessKey != "" {
		if strings.HasPrefix(cfg.SecretAccessKey, backupEncryptedSecretV1) {
			ciphertext := strings.TrimPrefix(cfg.SecretAccessKey, backupEncryptedSecretV1)
			decrypted, err := s.encryptor.Decrypt(ciphertext)
			if err != nil {
				return nil, fmt.Errorf("%w: decrypt S3 secret access key: %v", ErrBackupS3ConfigCorrupt, err)
			}
			cfg.SecretAccessKey = decrypted
		} else if decrypted, err := s.encryptor.Decrypt(cfg.SecretAccessKey); err == nil {
			// 兼容旧版无标记密文；下一次保存配置时会迁移到 v1 标记格式。
			cfg.SecretAccessKey = decrypted
		} else {
			// 兼容早期直接保存的明文。只有无版本标记的数据允许走此分支。
			logger.LegacyPrintf("service.backup", "[Backup] 检测到旧版未标记的 S3 SecretAccessKey，将在下次保存时迁移")
		}
	}
	return &cfg, nil
}

func (s *BackupService) getOrCreateStore(ctx context.Context, cfg *BackupS3Config) (BackupObjectStore, error) {
	s.storeMu.Lock()
	defer s.storeMu.Unlock()

	if s.store != nil && s.s3Cfg != nil {
		return s.store, nil
	}

	if cfg == nil {
		return nil, ErrBackupS3NotConfigured
	}

	store, err := s.storeFactory(ctx, cfg)
	if err != nil {
		return nil, err
	}
	s.store = store
	s.s3Cfg = cfg
	return store, nil
}

func (s *BackupService) buildS3Key(cfg *BackupS3Config, fileName string) string {
	prefix := strings.TrimRight(cfg.Prefix, "/")
	if prefix == "" {
		prefix = "backups"
	}
	return fmt.Sprintf("%s/%s/%s", prefix, time.Now().Format("2006/01/02"), fileName)
}

// loadRecords 加载备份记录，区分"无数据"和"数据损坏"
func (s *BackupService) loadRecords(ctx context.Context) ([]BackupRecord, error) {
	s.recordsMu.Lock()
	defer s.recordsMu.Unlock()
	return s.loadRecordsLocked(ctx)
}

// loadRecordsLocked 在已持有 recordsMu 锁的情况下加载记录
func (s *BackupService) loadRecordsLocked(ctx context.Context) ([]BackupRecord, error) {
	raw, err := s.settingRepo.GetValue(ctx, settingKeyBackupRecords)
	if errors.Is(err, ErrSettingNotFound) {
		return nil, nil //nolint:nilnil // no records is a valid state
	}
	if err != nil {
		return nil, fmt.Errorf("load backup records setting: %w", err)
	}
	if strings.TrimSpace(raw) == "" {
		return nil, ErrBackupRecordsCorrupt
	}
	var records []BackupRecord
	if err := json.Unmarshal([]byte(raw), &records); err != nil {
		return nil, ErrBackupRecordsCorrupt
	}
	return records, nil
}

// saveRecordsLocked 在已持有 recordsMu 锁的情况下保存记录
func (s *BackupService) saveRecordsLocked(ctx context.Context, records []BackupRecord) error {
	data, err := json.Marshal(records)
	if err != nil {
		return err
	}
	return s.settingRepo.Set(ctx, settingKeyBackupRecords, string(data))
}

// saveRecord 保存单条记录（带互斥锁保护）
func (s *BackupService) saveRecord(ctx context.Context, record *BackupRecord) error {
	s.recordsMu.Lock()
	defer s.recordsMu.Unlock()

	records, err := s.loadRecordsLocked(ctx)
	if err != nil {
		return err
	}

	// 更新已有记录或追加
	found := false
	for i := range records {
		if records[i].ID == record.ID {
			records[i] = *record
			found = true
			break
		}
	}
	if !found {
		records = append(records, *record)
	}
	return s.saveRecordsLocked(ctx, records)
}

func (s *BackupService) saveCompletedRecordOrCompensate(
	ctx context.Context,
	objectStore BackupObjectStore,
	record *BackupRecord,
) error {
	if err := s.saveRecord(ctx, record); err != nil {
		saveErr := fmt.Errorf("save completed %s backup record: %w", record.BackupType, err)
		record.Status = "failed"
		record.ErrorMsg = saveErr.Error()
		return compensateUploadedObject(ctx, objectStore, record.S3Key, saveErr)
	}
	return nil
}

func compensateUploadedObject(
	ctx context.Context,
	objectStore BackupObjectStore,
	key string,
	cause error,
) error {
	compensationCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), backupCompensationTimeout)
	defer cancel()
	if err := objectStore.Delete(compensationCtx, key); err != nil {
		return errors.Join(cause, fmt.Errorf("delete uploaded object %s after failure: %w", key, err))
	}
	return cause
}

func (s *BackupService) cleanupExpiredBackups(ctx context.Context) error {
	if err := s.beginBackupCleanup(); err != nil {
		return err
	}
	defer s.finishBackupCleanup()

	return s.cleanupExpiredBackupRecords(ctx, time.Now())
}

// cleanupExpiredBackupRecords 仅按 ExpiresAt 清理已授权的数据归档类型。
// 调用方必须已经占用备份操作槽位，避免与备份、归档及恢复并发。
func (s *BackupService) cleanupExpiredBackupRecords(ctx context.Context, now time.Time) error {
	s.recordsMu.Lock()
	defer s.recordsMu.Unlock()

	records, err := s.loadRecordsLocked(ctx)
	if err != nil {
		return err
	}

	toDelete := make(map[string]struct{})
	var cleanupErr error
	for _, record := range records {
		eligible, typeErr := isExpirationManagedArchive(record)
		if typeErr != nil {
			cleanupErr = errors.Join(cleanupErr, typeErr)
			continue
		}
		if !eligible {
			continue
		}
		expiresAtValue := strings.TrimSpace(record.ExpiresAt)
		if expiresAtValue == "" {
			continue
		}
		expiresAt, err := time.Parse(time.RFC3339, expiresAtValue)
		if err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("parse expires_at for backup record %s: %w", record.ID, err))
			continue
		}
		if now.Before(expiresAt) {
			continue
		}
		if record.Status == "running" || record.RestoreStatus == "running" {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("backup record %s is expired but still active", record.ID))
			continue
		}
		toDelete[record.ID] = struct{}{}
		if len(toDelete) >= backupExpirationCleanupBatchSize {
			break
		}
	}

	deleted, remaining, deleteErr := s.deleteBackupRecordObjectsLocked(ctx, records, toDelete)
	cleanupErr = errors.Join(cleanupErr, deleteErr)
	if deleted == 0 {
		return cleanupErr
	}
	if err := s.saveRecordsLocked(ctx, remaining); err != nil {
		return errors.Join(cleanupErr, fmt.Errorf("save records after expiration cleanup: %w", err))
	}
	logger.LegacyPrintf("service.backup", "[Backup] 自动清理了 %d 个已过期归档", deleted)
	return cleanupErr
}

func isExpirationManagedArchive(record BackupRecord) (bool, error) {
	switch strings.TrimSpace(record.BackupType) {
	case "usage_logs_archive", "ops_system_logs_archive", "ops_error_logs_archive":
		return true, nil
	case "", "postgres":
		return false, nil
	default:
		if strings.TrimSpace(record.ExpiresAt) == "" {
			return false, nil
		}
		return false, fmt.Errorf("backup record %s has unsupported expiration-managed type %q", record.ID, record.BackupType)
	}
}

func (s *BackupService) cleanupOldBackups(ctx context.Context, schedule *BackupScheduleConfig) error {
	if schedule == nil {
		return nil
	}
	if err := s.beginBackupCleanup(); err != nil {
		return err
	}
	defer s.finishBackupCleanup()

	s.recordsMu.Lock()
	defer s.recordsMu.Unlock()

	records, err := s.loadRecordsLocked(ctx)
	if err != nil {
		return err
	}

	if schedule.RetainCount <= 0 && schedule.RetainDays <= 0 {
		return nil
	}

	// 份数和保留天数只针对已完成的 PostgreSQL 全库备份计算，归档记录不参与排序。
	type postgresBackupWithTime struct {
		record    BackupRecord
		startedAt time.Time
	}
	postgresRecords := make([]postgresBackupWithTime, 0, len(records))
	var cleanupErr error
	for _, record := range records {
		if !isPostgresBackupRecord(record) || record.Status != "completed" {
			continue
		}
		startedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(record.StartedAt))
		if err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("parse started_at for postgres backup record %s: %w", record.ID, err))
			continue
		}
		postgresRecords = append(postgresRecords, postgresBackupWithTime{record: record, startedAt: startedAt})
	}
	// RetainCount 依赖完整且可靠的时间顺序；存在损坏记录时直接失败，禁止猜测排序后误删。
	if cleanupErr != nil {
		return cleanupErr
	}
	sort.Slice(postgresRecords, func(i, j int) bool {
		return postgresRecords[i].startedAt.After(postgresRecords[j].startedAt)
	})

	toDelete := make(map[string]struct{})
	cutoff := time.Time{}
	if schedule.RetainDays > 0 {
		cutoff = time.Now().AddDate(0, 0, -schedule.RetainDays)
	}
	for i, candidate := range postgresRecords {
		// 无论份数/天数策略如何，始终保留最新一份成功的全库备份。
		if i == 0 {
			continue
		}
		shouldDelete := false

		// 按保留份数清理
		if schedule.RetainCount > 0 && i >= schedule.RetainCount {
			shouldDelete = true
		}

		// 按保留天数清理
		if schedule.RetainDays > 0 && candidate.startedAt.Before(cutoff) {
			shouldDelete = true
		}

		if shouldDelete {
			toDelete[candidate.record.ID] = struct{}{}
		}
	}

	deleted, remaining, deleteErr := s.deleteBackupRecordObjectsLocked(ctx, records, toDelete)
	cleanupErr = errors.Join(cleanupErr, deleteErr)
	if deleted == 0 {
		return cleanupErr
	}
	if err := s.saveRecordsLocked(ctx, remaining); err != nil {
		return errors.Join(cleanupErr, fmt.Errorf("save records after postgres backup cleanup: %w", err))
	}
	logger.LegacyPrintf("service.backup", "[Backup] 自动清理了 %d 个 PostgreSQL 全库备份", deleted)
	return cleanupErr
}

func (s *BackupService) beginBackupCleanup() error {
	if s.shuttingDown.Load() {
		return infraerrors.ServiceUnavailable("SERVER_SHUTTING_DOWN", "server is shutting down")
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.backingUp {
		return ErrBackupInProgress
	}
	if s.restoring {
		return ErrRestoreInProgress
	}
	s.backingUp = true
	return nil
}

func (s *BackupService) finishBackupCleanup() {
	s.opMu.Lock()
	s.backingUp = false
	s.opMu.Unlock()
}

func isPostgresBackupRecord(record BackupRecord) bool {
	backupType := strings.TrimSpace(record.BackupType)
	return backupType == "" || backupType == "postgres"
}

// deleteBackupRecordObjectsLocked 删除候选记录对应的对象。
// 只有对象删除成功的记录才会从 remaining 中移除；失败项保留并聚合返回错误。
func (s *BackupService) deleteBackupRecordObjectsLocked(
	ctx context.Context,
	records []BackupRecord,
	toDelete map[string]struct{},
) (int, []BackupRecord, error) {
	remaining := make([]BackupRecord, 0, len(records))
	deleted := 0
	var cleanupErr error
	for _, record := range records {
		if _, ok := toDelete[record.ID]; !ok {
			remaining = append(remaining, record)
			continue
		}
		if strings.TrimSpace(record.S3Key) == "" {
			remaining = append(remaining, record)
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("backup record %s cannot be deleted: S3 key is empty", record.ID))
			continue
		}
		if err := s.deleteS3Object(ctx, record.S3Key); err != nil {
			remaining = append(remaining, record)
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete S3 object for backup record %s: %w", record.ID, err))
			continue
		}
		deleted++
	}
	return deleted, remaining, cleanupErr
}

func (s *BackupService) deleteS3Object(ctx context.Context, key string) error {
	s3Cfg, err := s.loadS3Config(ctx)
	if err != nil {
		return fmt.Errorf("load S3 config: %w", err)
	}
	if s3Cfg == nil || !s3Cfg.IsConfigured() {
		return ErrBackupS3NotConfigured
	}
	objectStore, err := s.getOrCreateStore(ctx, s3Cfg)
	if err != nil {
		return err
	}
	return objectStore.Delete(ctx, key)
}
