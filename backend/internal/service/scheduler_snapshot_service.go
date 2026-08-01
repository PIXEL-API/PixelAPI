package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"

	"golang.org/x/sync/singleflight"
)

var (
	ErrSchedulerCacheNotReady           = errors.New("scheduler cache not ready")
	ErrSchedulerFallbackLimited         = errors.New("scheduler db fallback limited")
	ErrSchedulerGroupLifecycleLeaseBusy = errors.New("scheduler group lifecycle lease busy")
	ErrSchedulerBucketRebuildBusy       = errors.New("scheduler bucket rebuild busy")
)

const (
	outboxEventTimeout                    = 2 * time.Minute
	schedulerOutboxCleanupBatch           = 5000
	schedulerGroupLifecycleTimeout        = 30 * time.Second
	schedulerGroupLifecycleLeaseTTL       = 60 * time.Second
	schedulerGroupLifecycleReleaseTimeout = 2 * time.Second
	outboxRebuildRetryBaseDelay           = 5 * time.Second
	outboxRebuildRetryMaxDelay            = 5 * time.Minute
	outboxMaxIDErrorLogSampleInterval     = time.Minute
)

// Dynamic (admin-configurable) scheduler candidate sampling settings. When
// enabled, every scheduling bucket larger than the threshold serves a capped
// candidate sample instead of the full snapshot, bounding per-request memory.
// These are read from the settings store with a short-lived cache so the hot
// scheduling path does not hit the DB on every request.
const (
	SettingKeySchedulerCandidateSamplingEnabled   = "scheduler_candidate_sampling_enabled"
	SettingKeySchedulerCandidateSamplingLimit     = "scheduler_candidate_sampling_limit"
	SettingKeySchedulerCandidateSamplingThreshold = "scheduler_candidate_sampling_threshold"
)

const (
	schedulerCandidateSamplingSettingCacheTTL  = 5 * time.Second
	schedulerCandidateSamplingSettingDBTimeout = 2 * time.Second
	schedulerCandidateSamplingSettingSFKey     = "scheduler_candidate_sampling"

	defaultSchedulerCandidateSamplingLimit     = 256
	maxSchedulerCandidateSamplingLimit         = 1024
	defaultSchedulerCandidateSamplingThreshold = 5000
)

type cachedSchedulerCandidateSamplingSetting struct {
	enabled   bool
	limit     int
	threshold int
	expiresAt int64
}

var schedulerCandidateSamplingSettingCache atomic.Value // *cachedSchedulerCandidateSamplingSetting
var schedulerCandidateSamplingSettingSF singleflight.Group

// batchSeenKey tracks which (groupID, platform) bucket sets have already been
// rebuilt within a single pollOutbox call, to avoid redundant work when multiple
// account_changed events share the same groups.
type batchSeenKey struct {
	groupID   int64
	platform  string
	lifecycle bool
}

type schedulerBucketWriteTask struct {
	bucket SchedulerBucket
	token  SchedulerBucketWriteToken
}

type schedulerAccountQueryKey struct {
	groupID  int64
	platform string
}

type schedulerAccountQueryCache struct {
	remaining map[schedulerAccountQueryKey]int
	accounts  map[schedulerAccountQueryKey][]Account
}

func newSchedulerAccountQueryCache(taskSets ...[]schedulerBucketWriteTask) *schedulerAccountQueryCache {
	queries := &schedulerAccountQueryCache{
		remaining: make(map[schedulerAccountQueryKey]int),
		accounts:  make(map[schedulerAccountQueryKey][]Account),
	}
	for _, tasks := range taskSets {
		for _, task := range tasks {
			if key, ok := schedulerAccountQueryKeyForBucket(task.bucket); ok {
				queries.remaining[key]++
			}
		}
	}
	return queries
}

func schedulerAccountQueryKeyForBucket(bucket SchedulerBucket) (schedulerAccountQueryKey, bool) {
	if bucket.Mode != SchedulerModeSingle && bucket.Mode != SchedulerModeForced {
		return schedulerAccountQueryKey{}, false
	}
	return schedulerAccountQueryKey{groupID: bucket.GroupID, platform: bucket.Platform}, true
}

func (c *schedulerAccountQueryCache) release(bucket SchedulerBucket) {
	if c == nil {
		return
	}
	key, ok := schedulerAccountQueryKeyForBucket(bucket)
	if !ok {
		return
	}
	remaining := c.remaining[key] - 1
	if remaining <= 0 {
		delete(c.remaining, key)
		delete(c.accounts, key)
		return
	}
	c.remaining[key] = remaining
}

type schedulerGroupLifecyclePlan struct {
	active bool
	tasks  []schedulerBucketWriteTask
}

type schedulerActiveGroupIDLister interface {
	ListActiveIDs(ctx context.Context) ([]int64, error)
}

func schedulerSnapshotPlatforms() [5]string {
	return [5]string{PlatformAnthropic, PlatformGemini, PlatformOpenAI, PlatformAntigravity, PlatformGrok}
}

type SchedulerSnapshotService struct {
	cache         SchedulerCache
	outboxRepo    SchedulerOutboxRepository
	accountRepo   AccountRepository
	groupRepo     GroupRepository
	settingRepo   SettingRepository
	cfg           *config.Config
	stopCh        chan struct{}
	stopOnce      sync.Once
	wg            sync.WaitGroup
	fallbackLimit *fallbackLimiter
	lagMu         sync.Mutex
	lagFailures   int

	outboxRebuildLatched         bool
	outboxRebuildRunning         bool
	outboxRebuildFailures        int
	outboxRebuildRetryAt         time.Time
	outboxRebuildRetryReason     string
	outboxLagWarningActive       bool
	outboxMaxIDErrorLastLoggedAt time.Time

	fullRebuildRunMu     sync.Mutex
	fullRebuildStateMu   sync.Mutex
	fullRebuildRequested uint64
	fullRebuildCompleted uint64
	fullRebuildLastErr   error

	// 按 bucket 的重建去抖状态：间隔内重复触发的 bucket 转入 pendingRebuild，
	// 由 Start() 的 drain goroutine 周期性兜底。仅作用于 outbox 增量重建入口
	// （rebuildBuckets），分组生命周期与全量重建不经过去抖。
	rebuildMu      sync.Mutex
	lastRebuildAt  map[SchedulerBucket]time.Time
	pendingRebuild map[SchedulerBucket]struct{}
}

func NewSchedulerSnapshotService(
	cache SchedulerCache,
	outboxRepo SchedulerOutboxRepository,
	accountRepo AccountRepository,
	groupRepo GroupRepository,
	cfg *config.Config,
) *SchedulerSnapshotService {
	maxQPS := 0
	if cfg != nil {
		maxQPS = cfg.Gateway.Scheduling.DbFallbackMaxQPS
	}
	return &SchedulerSnapshotService{
		cache:          cache,
		outboxRepo:     outboxRepo,
		accountRepo:    accountRepo,
		groupRepo:      groupRepo,
		cfg:            cfg,
		stopCh:         make(chan struct{}),
		fallbackLimit:  newFallbackLimiter(maxQPS),
		lastRebuildAt:  make(map[SchedulerBucket]time.Time),
		pendingRebuild: make(map[SchedulerBucket]struct{}),
	}
}

func (s *SchedulerSnapshotService) lifecycleCache() (SchedulerLifecycleCache, error) {
	if s == nil || s.cache == nil {
		return nil, ErrSchedulerCacheNotReady
	}
	cache, ok := s.cache.(SchedulerLifecycleCache)
	if !ok {
		return nil, ErrSchedulerLifecycleCacheRequired
	}
	return cache, nil
}

// SetSettingRepository injects the settings store used to read the dynamic
// candidate-sampling settings. Optional: when nil, sampling defaults apply.
func (s *SchedulerSnapshotService) SetSettingRepository(repo SettingRepository) {
	if s == nil {
		return
	}
	s.settingRepo = repo
}

// candidateSamplingSettings returns the current (enabled, limit, threshold) for
// scheduler candidate sampling, cached for a few seconds and coalesced so the
// hot scheduling path never hits the DB per request. Falls back to defaults
// (disabled, cfg/256 limit, 5000 threshold) when the setting store is absent.
func (s *SchedulerSnapshotService) candidateSamplingSettings(ctx context.Context) (bool, int, int) {
	if cached, ok := schedulerCandidateSamplingSettingCache.Load().(*cachedSchedulerCandidateSamplingSetting); ok && cached != nil {
		if time.Now().UnixNano() < cached.expiresAt {
			return cached.enabled, cached.limit, cached.threshold
		}
	}

	result, _, _ := schedulerCandidateSamplingSettingSF.Do(schedulerCandidateSamplingSettingSFKey, func() (any, error) {
		if cached, ok := schedulerCandidateSamplingSettingCache.Load().(*cachedSchedulerCandidateSamplingSetting); ok && cached != nil {
			if time.Now().UnixNano() < cached.expiresAt {
				return cached, nil
			}
		}

		enabled := false
		limit := s.candidateIndexLimit()
		threshold := defaultSchedulerCandidateSamplingThreshold

		if s != nil && s.settingRepo != nil {
			dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), schedulerCandidateSamplingSettingDBTimeout)
			defer cancel()
			values, err := s.settingRepo.GetMultiple(dbCtx, []string{
				SettingKeySchedulerCandidateSamplingEnabled,
				SettingKeySchedulerCandidateSamplingLimit,
				SettingKeySchedulerCandidateSamplingThreshold,
			})
			if err == nil {
				enabled = strings.EqualFold(strings.TrimSpace(values[SettingKeySchedulerCandidateSamplingEnabled]), "true")
				if n, e := strconv.Atoi(strings.TrimSpace(values[SettingKeySchedulerCandidateSamplingLimit])); e == nil && n > 0 {
					limit = normalizeSchedulerCandidateSamplingLimit(n)
				}
				if n, e := strconv.Atoi(strings.TrimSpace(values[SettingKeySchedulerCandidateSamplingThreshold])); e == nil && n > 0 {
					threshold = n
				}
			}
		}

		cached := &cachedSchedulerCandidateSamplingSetting{
			enabled:   enabled,
			limit:     limit,
			threshold: threshold,
			expiresAt: time.Now().Add(schedulerCandidateSamplingSettingCacheTTL).UnixNano(),
		}
		schedulerCandidateSamplingSettingCache.Store(cached)
		return cached, nil
	})

	if cached, ok := result.(*cachedSchedulerCandidateSamplingSetting); ok && cached != nil {
		return cached.enabled, cached.limit, cached.threshold
	}
	return false, defaultSchedulerCandidateSamplingLimit, defaultSchedulerCandidateSamplingThreshold
}

// normalizeSchedulerCandidateSamplingLimit clamps the candidate limit to the
// supported range [1, 1024].
func normalizeSchedulerCandidateSamplingLimit(limit int) int {
	if limit <= 0 {
		return defaultSchedulerCandidateSamplingLimit
	}
	if limit > maxSchedulerCandidateSamplingLimit {
		return maxSchedulerCandidateSamplingLimit
	}
	return limit
}

// normalizeSchedulerCandidateSamplingThreshold falls back to the default bucket
// threshold when a non-positive value is supplied.
func normalizeSchedulerCandidateSamplingThreshold(threshold int) int {
	if threshold <= 0 {
		return defaultSchedulerCandidateSamplingThreshold
	}
	return threshold
}

func (s *SchedulerSnapshotService) Start() {
	if s == nil || s.cache == nil {
		return
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.runInitialRebuild()
	}()

	interval := s.outboxPollInterval()
	if s.outboxRepo != nil && interval > 0 {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.runOutboxWorker(interval)
		}()
	}

	fullInterval := s.fullRebuildInterval()
	if fullInterval > 0 {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.runFullRebuildWorker(fullInterval)
		}()
	}

	if debounceInterval := s.rebuildDebounceInterval(); debounceInterval > 0 {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.runRebuildDebounceWorker(debounceInterval)
		}()
	}
}

func (s *SchedulerSnapshotService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *SchedulerSnapshotService) ListSchedulableAccounts(ctx context.Context, groupID *int64, platform string, hasForcePlatform bool) ([]Account, bool, error) {
	useMixed := (platform == PlatformAnthropic || platform == PlatformGemini) && !hasForcePlatform
	mode := s.resolveMode(platform, hasForcePlatform)
	bucket := s.bucketFor(groupID, platform, mode)
	var writeToken SchedulerBucketWriteToken
	var lifecycleCache SchedulerLifecycleCache
	canPublish := false

	if s.cache != nil {
		if candidateCache, ok := s.cache.(SchedulerCandidateCache); ok {
			if !IsSchedulerCandidateIndexBypassed(ctx) {
				samplingEnabled, candidateLimit, candidateThreshold := s.candidateSamplingSettings(ctx)
				cached, hit, err := candidateCache.GetCandidateSnapshot(ctx, bucket, candidateLimit, candidateThreshold, samplingEnabled)
				if err != nil {
					logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] candidate cache read failed: bucket=%s err=%v", bucket.String(), err)
				} else if hit {
					return filterSchedulableAccounts(derefAccounts(cached)), useMixed, nil
				}
			}
		}
		cached, hit, err := s.cache.GetSnapshot(ctx, bucket)
		if err != nil {
			logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] cache read failed: bucket=%s err=%v", bucket.String(), err)
		} else if hit {
			return filterSchedulableAccounts(derefAccounts(cached)), useMixed, nil
		}
		lifecycleCache, err = s.lifecycleCache()
		if err != nil {
			return nil, useMixed, err
		} else if token, tokenErr := lifecycleCache.CaptureBucketWriteToken(ctx, bucket); tokenErr != nil {
			if errors.Is(tokenErr, ErrSchedulerBucketRetired) || errors.Is(tokenErr, ErrSchedulerBucketWriteFenced) {
				slog.Debug("[Scheduler] cache publish fenced", "bucket", bucket.String())
			} else {
				logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] cache publish token failed: bucket=%s err=%v", bucket.String(), tokenErr)
			}
		} else {
			writeToken = token
			canPublish = true
		}
	}

	if err := s.guardFallback(ctx); err != nil {
		return nil, useMixed, err
	}

	fallbackCtx, cancel := s.withFallbackTimeout(ctx)
	defer cancel()

	accounts, err := s.loadAccountsFromDB(fallbackCtx, bucket, useMixed)
	if err != nil {
		return nil, useMixed, err
	}

	if lifecycleCache != nil && canPublish {
		if err := lifecycleCache.SetSnapshotFenced(fallbackCtx, bucket, writeToken, accounts); err != nil {
			if errors.Is(err, ErrSchedulerBucketRetired) || errors.Is(err, ErrSchedulerBucketWriteFenced) {
				slog.Debug("[Scheduler] cache publish fenced", "bucket", bucket.String())
			} else {
				logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] cache write failed: bucket=%s err=%v", bucket.String(), err)
			}
		}
	}

	return filterSchedulableAccounts(accounts), useMixed, nil
}

func (s *SchedulerSnapshotService) GetAccount(ctx context.Context, accountID int64) (*Account, error) {
	if accountID <= 0 {
		return nil, nil
	}
	if s.cache != nil {
		account, err := s.cache.GetAccount(ctx, accountID)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				// 客户端断连导致的取消是常态，不值得进 ops 错误索引
				//（曾以 error 级别写入每小时上万条）。
				slog.Debug("[Scheduler] account cache read canceled", "account_id", accountID)
			} else {
				logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] account cache read failed: id=%d err=%v", accountID, err)
			}
		} else if account != nil {
			return account, nil
		}
	}
	if s.accountRepo == nil {
		return nil, nil
	}

	if err := s.guardFallback(ctx); err != nil {
		return nil, err
	}
	fallbackCtx, cancel := s.withFallbackTimeout(ctx)
	defer cancel()
	return s.accountRepo.GetByID(fallbackCtx, accountID)
}

// GetGroupByID 获取分组信息（供调度器使用）
func (s *SchedulerSnapshotService) GetGroupByID(ctx context.Context, groupID int64) (*Group, error) {
	if s.groupRepo == nil {
		return nil, nil
	}
	return s.groupRepo.GetByID(ctx, groupID)
}

// UpdateAccountInCache 立即更新 Redis 中单个账号的数据（用于模型限流后立即生效）
func (s *SchedulerSnapshotService) UpdateAccountInCache(ctx context.Context, account *Account) error {
	if s.cache == nil || account == nil {
		return nil
	}
	return s.cache.SetAccount(ctx, account)
}

func (s *SchedulerSnapshotService) runInitialRebuild() {
	if s.cache == nil {
		return
	}
	_ = s.coalesceFullRebuild(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := s.rebuildFullSnapshot(ctx, "startup"); err != nil {
			logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] rebuild startup failed: %v", err)
			return err
		}
		return nil
	})
}

func (s *SchedulerSnapshotService) runOutboxWorker(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	s.pollOutbox()
	for {
		select {
		case <-ticker.C:
			s.pollOutbox()
		case <-s.stopCh:
			return
		}
	}
}

func (s *SchedulerSnapshotService) runFullRebuildWorker(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := s.triggerFullRebuild("interval"); err != nil {
				logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] full rebuild failed: %v", err)
			}
		case <-s.stopCh:
			return
		}
	}
}

func (s *SchedulerSnapshotService) pollOutbox() {
	if s.outboxRepo == nil || s.cache == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	watermark, err := s.cache.GetOutboxWatermark(ctx)
	if err != nil {
		logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] outbox watermark read failed: %v", err)
		return
	}

	events, err := s.outboxRepo.ListAfterAndReleaseDedup(ctx, watermark, 200)
	if err != nil {
		logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] outbox poll failed: %v", err)
		return
	}
	if len(events) == 0 {
		s.clearOutboxDegradedEpisode()
		return
	}

	seen := make(map[batchSeenKey]struct{})
	for _, event := range events {
		eventCtx, cancel := context.WithTimeout(context.Background(), outboxEventTimeout)
		err := s.handleOutboxEvent(eventCtx, event, seen)
		cancel()
		if err != nil {
			logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] outbox handle failed: id=%d type=%s err=%v", event.ID, event.EventType, err)
			return
		}
	}

	lastID := events[len(events)-1].ID
	var wmErr error
	for i := range 3 {
		wmCtx, wmCancel := context.WithTimeout(context.Background(), 5*time.Second)
		wmErr = s.cache.SetOutboxWatermark(wmCtx, lastID)
		wmCancel()
		if wmErr == nil {
			break
		}
		if i < 2 {
			time.Sleep(200 * time.Millisecond)
		}
	}
	if wmErr != nil {
		logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] outbox watermark write failed: %v", wmErr)
		return
	}
	s.cleanupConsumedOutbox(lastID)

	lagCtx, lagCancel := context.WithTimeout(context.Background(), 5*time.Second)
	s.checkOutboxLag(lagCtx, lastID)
	lagCancel()
}

func (s *SchedulerSnapshotService) cleanupConsumedOutbox(watermark int64) {
	if s == nil || s.outboxRepo == nil || watermark <= 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	lease, acquired, err := s.outboxRepo.TryAcquireCleanupLock(ctx)
	if err != nil {
		logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] outbox cleanup lock failed: %v", err)
		return
	}
	if !acquired {
		return
	}
	defer lease.Release()

	for {
		deleted, err := s.outboxRepo.DeleteConsumedUpTo(ctx, watermark, schedulerOutboxCleanupBatch)
		if err != nil {
			logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] outbox cleanup failed: watermark=%d err=%v", watermark, err)
			return
		}
		if deleted == 0 || deleted < schedulerOutboxCleanupBatch {
			return
		}
	}
}

func (s *SchedulerSnapshotService) handleOutboxEvent(ctx context.Context, event SchedulerOutboxEvent, seen map[batchSeenKey]struct{}) error {
	switch event.EventType {
	case SchedulerOutboxEventAccountLastUsed:
		return s.handleLastUsedEvent(ctx, event.Payload)
	case SchedulerOutboxEventAccountBulkChanged:
		return s.handleBulkAccountEvent(ctx, event.Payload, seen)
	case SchedulerOutboxEventAccountGroupsChanged:
		return s.handleAccountEvent(ctx, event.AccountID, event.Payload, seen)
	case SchedulerOutboxEventAccountChanged:
		return s.handleAccountEvent(ctx, event.AccountID, event.Payload, seen)
	case SchedulerOutboxEventGroupChanged:
		return s.handleGroupEvent(ctx, event.GroupID, seen)
	case SchedulerOutboxEventFullRebuild:
		return s.triggerFullRebuild("outbox")
	default:
		return nil
	}
}

func (s *SchedulerSnapshotService) handleLastUsedEvent(ctx context.Context, payload map[string]any) error {
	if s.cache == nil || payload == nil {
		return nil
	}
	raw, ok := payload["last_used"].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	updates := make(map[int64]time.Time, len(raw))
	for key, value := range raw {
		id, err := strconv.ParseInt(key, 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		sec, ok := toInt64(value)
		if !ok || sec <= 0 {
			continue
		}
		updates[id] = time.Unix(sec, 0)
	}
	if len(updates) == 0 {
		return nil
	}
	return s.cache.UpdateLastUsed(ctx, updates)
}

func (s *SchedulerSnapshotService) handleBulkAccountEvent(ctx context.Context, payload map[string]any, seen map[batchSeenKey]struct{}) error {
	if payload == nil {
		return nil
	}
	if s.accountRepo == nil {
		return nil
	}

	rawIDs := parseInt64Slice(payload["account_ids"])
	if len(rawIDs) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(rawIDs))
	seenIDs := make(map[int64]struct{}, len(rawIDs))
	for _, id := range rawIDs {
		if id <= 0 {
			continue
		}
		if _, exists := seenIDs[id]; exists {
			continue
		}
		seenIDs[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}

	preloadGroupIDs := parseInt64Slice(payload["group_ids"])
	accounts, err := s.accountRepo.GetByIDs(ctx, ids)
	if err != nil {
		return err
	}

	found := make(map[int64]struct{}, len(accounts))
	rebuildGroupSet := make(map[int64]struct{}, len(preloadGroupIDs))
	for _, gid := range preloadGroupIDs {
		if gid > 0 {
			rebuildGroupSet[gid] = struct{}{}
		}
	}

	for _, account := range accounts {
		if account == nil || account.ID <= 0 {
			continue
		}
		found[account.ID] = struct{}{}
		if s.cache != nil {
			if err := s.cache.SetAccount(ctx, account); err != nil {
				return err
			}
		}
		for _, gid := range account.GroupIDs {
			if gid > 0 {
				rebuildGroupSet[gid] = struct{}{}
			}
		}
	}

	if s.cache != nil {
		for _, id := range ids {
			if _, ok := found[id]; ok {
				continue
			}
			if err := s.cache.DeleteAccount(ctx, id); err != nil {
				return err
			}
		}
	}

	rebuildGroupIDs := make([]int64, 0, len(rebuildGroupSet))
	for gid := range rebuildGroupSet {
		rebuildGroupIDs = append(rebuildGroupIDs, gid)
	}
	return s.rebuildByGroupIDs(ctx, rebuildGroupIDs, "account_bulk_change", seen)
}

func (s *SchedulerSnapshotService) handleAccountEvent(ctx context.Context, accountID *int64, payload map[string]any, seen map[batchSeenKey]struct{}) error {
	if accountID == nil || *accountID <= 0 {
		return nil
	}
	if s.accountRepo == nil {
		return nil
	}

	var groupIDs []int64
	if payload != nil {
		groupIDs = parseInt64Slice(payload["group_ids"])
	}

	account, err := s.accountRepo.GetByID(ctx, *accountID)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			if s.cache != nil {
				if err := s.cache.DeleteAccount(ctx, *accountID); err != nil {
					return err
				}
			}
			return s.rebuildByGroupIDs(ctx, groupIDs, "account_miss", seen)
		}
		return err
	}
	if s.cache != nil {
		if err := s.cache.SetAccount(ctx, account); err != nil {
			return err
		}
	}
	if len(groupIDs) == 0 {
		groupIDs = account.GroupIDs
	}
	return s.rebuildByAccount(ctx, account, groupIDs, "account_change", seen)
}

func (s *SchedulerSnapshotService) handleGroupEvent(ctx context.Context, groupID *int64, seen map[batchSeenKey]struct{}) error {
	if groupID == nil || *groupID <= 0 || s.isRunModeSimple() {
		return nil
	}
	if seen != nil {
		if _, ok := seen[batchSeenKey{groupID: *groupID, lifecycle: true}]; ok {
			return nil
		}
	}
	return s.reconcileGroupLifecycle(ctx, *groupID, seen)
}

func (s *SchedulerSnapshotService) reconcileGroupLifecycle(ctx context.Context, groupID int64, seen map[batchSeenKey]struct{}) error {
	plan, err := s.prepareGroupLifecycle(ctx, groupID, nil)
	if err != nil {
		return err
	}
	if plan.active {
		queries := newSchedulerAccountQueryCache(plan.tasks)
		for _, task := range plan.tasks {
			if err := s.rebuildBucketWithTokenPolicyAndQueryCache(ctx, task, "group_change", true, queries); err != nil {
				return err
			}
		}
	}
	markGroupLifecycleSeen(seen, groupID)
	return nil
}

func (s *SchedulerSnapshotService) prepareGroupLifecycle(ctx context.Context, groupID int64, knownHistorical []SchedulerBucket) (plan schedulerGroupLifecyclePlan, retErr error) {
	if groupID <= 0 || s.isRunModeSimple() {
		return schedulerGroupLifecyclePlan{}, nil
	}
	if s.groupRepo == nil {
		return schedulerGroupLifecyclePlan{}, ErrSchedulerCacheNotReady
	}
	cache, err := s.lifecycleCache()
	if err != nil {
		return schedulerGroupLifecyclePlan{}, err
	}

	lifecycleCtx, cancel := context.WithTimeout(ctx, schedulerGroupLifecycleTimeout)
	defer cancel()
	lease, acquired, err := cache.TryAcquireGroupLifecycleLease(lifecycleCtx, groupID, schedulerGroupLifecycleLeaseTTL)
	if err != nil {
		return schedulerGroupLifecyclePlan{}, err
	}
	if !acquired {
		return schedulerGroupLifecyclePlan{}, fmt.Errorf("%w: group=%d", ErrSchedulerGroupLifecycleLeaseBusy, groupID)
	}
	leaseHeld := true
	defer func() {
		if leaseHeld {
			retErr = errors.Join(retErr, s.releaseGroupLifecycleLease(cache, lease))
		}
	}()

	group, err := s.groupRepo.GetByIDLite(lifecycleCtx, groupID)
	missing := errors.Is(err, ErrGroupNotFound)
	if err != nil && !missing {
		return schedulerGroupLifecyclePlan{}, err
	}
	if err == nil && (group == nil || group.ID != groupID || !group.Hydrated) {
		return schedulerGroupLifecyclePlan{}, fmt.Errorf("untrusted scheduler group lifecycle state: group=%d", groupID)
	}

	plan = schedulerGroupLifecyclePlan{active: !missing && group.IsActive()}
	if plan.active {
		buckets := schedulerBucketsForGroup(groupID)
		plan.tasks = make([]schedulerBucketWriteTask, 0, len(buckets))
		for _, bucket := range buckets {
			token, err := cache.ReopenBucket(lifecycleCtx, bucket)
			if err != nil {
				return schedulerGroupLifecyclePlan{}, err
			}
			plan.tasks = append(plan.tasks, schedulerBucketWriteTask{bucket: bucket, token: token})
		}
	} else {
		registered := knownHistorical
		if registered == nil {
			registered, err = s.cache.ListBuckets(lifecycleCtx)
			if err != nil {
				return schedulerGroupLifecyclePlan{}, err
			}
		}
		buckets := schedulerBucketsForGroup(groupID)
		for _, bucket := range registered {
			if bucket.GroupID == groupID {
				buckets = append(buckets, bucket)
			}
		}
		for _, bucket := range dedupeBuckets(buckets) {
			if err := cache.RetireBucket(lifecycleCtx, bucket); err != nil {
				return schedulerGroupLifecyclePlan{}, err
			}
		}
	}

	releaseErr := s.releaseGroupLifecycleLease(cache, lease)
	leaseHeld = false
	if releaseErr != nil {
		return schedulerGroupLifecyclePlan{}, releaseErr
	}
	return plan, nil
}

func (s *SchedulerSnapshotService) releaseGroupLifecycleLease(cache SchedulerLifecycleCache, lease SchedulerGroupLifecycleLease) error {
	releaseCtx, cancel := context.WithTimeout(context.Background(), schedulerGroupLifecycleReleaseTimeout)
	defer cancel()
	return cache.ReleaseGroupLifecycleLease(releaseCtx, lease)
}

func markGroupLifecycleSeen(seen map[batchSeenKey]struct{}, groupID int64) {
	if seen == nil {
		return
	}
	seen[batchSeenKey{groupID: groupID, lifecycle: true}] = struct{}{}
	for _, platform := range schedulerSnapshotPlatforms() {
		seen[batchSeenKey{groupID: groupID, platform: platform}] = struct{}{}
	}
}

func (s *SchedulerSnapshotService) rebuildByAccount(ctx context.Context, account *Account, groupIDs []int64, reason string, seen map[batchSeenKey]struct{}) error {
	if account == nil {
		return nil
	}
	groupIDs = s.normalizeGroupIDs(groupIDs)
	if len(groupIDs) == 0 {
		return nil
	}

	buckets := s.bucketsForPlatform(account.Platform, groupIDs, seen)
	if account.Platform == PlatformAntigravity && account.IsMixedSchedulingEnabled() {
		buckets = append(buckets, s.bucketsForPlatform(PlatformAnthropic, groupIDs, seen)...)
		buckets = append(buckets, s.bucketsForPlatform(PlatformGemini, groupIDs, seen)...)
	}
	return s.rebuildBuckets(ctx, buckets, reason)
}

func schedulerBucketsForGroup(groupID int64) []SchedulerBucket {
	if groupID <= 0 {
		return nil
	}
	return schedulerCanonicalBuckets(groupID)
}

func schedulerCanonicalBuckets(groupID int64) []SchedulerBucket {
	buckets := make([]SchedulerBucket, 0, 12)
	for _, platform := range schedulerSnapshotPlatforms() {
		buckets = append(buckets,
			SchedulerBucket{GroupID: groupID, Platform: platform, Mode: SchedulerModeSingle},
			SchedulerBucket{GroupID: groupID, Platform: platform, Mode: SchedulerModeForced},
		)
		if platform == PlatformAnthropic || platform == PlatformGemini {
			buckets = append(buckets, SchedulerBucket{GroupID: groupID, Platform: platform, Mode: SchedulerModeMixed})
		}
	}
	return buckets
}

func (s *SchedulerSnapshotService) rebuildByGroupIDs(ctx context.Context, groupIDs []int64, reason string, seen map[batchSeenKey]struct{}) error {
	groupIDs = s.normalizeGroupIDs(groupIDs)
	if len(groupIDs) == 0 {
		return nil
	}
	buckets := make([]SchedulerBucket, 0, len(groupIDs)*12)
	for _, platform := range schedulerSnapshotPlatforms() {
		buckets = append(buckets, s.bucketsForPlatform(platform, groupIDs, seen)...)
	}
	return s.rebuildBuckets(ctx, buckets, reason)
}

func (s *SchedulerSnapshotService) bucketsForPlatform(platform string, groupIDs []int64, seen map[batchSeenKey]struct{}) []SchedulerBucket {
	if platform == "" {
		return nil
	}
	buckets := make([]SchedulerBucket, 0, len(groupIDs)*3)
	for _, gid := range groupIDs {
		if seen != nil {
			key := batchSeenKey{groupID: gid, platform: platform}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
		}
		buckets = append(buckets,
			SchedulerBucket{GroupID: gid, Platform: platform, Mode: SchedulerModeSingle},
			SchedulerBucket{GroupID: gid, Platform: platform, Mode: SchedulerModeForced},
		)
		if platform == PlatformAnthropic || platform == PlatformGemini {
			buckets = append(buckets, SchedulerBucket{GroupID: gid, Platform: platform, Mode: SchedulerModeMixed})
		}
	}
	return buckets
}

func (s *SchedulerSnapshotService) rebuildBuckets(ctx context.Context, buckets []SchedulerBucket, reason string) error {
	if interval := s.rebuildDebounceInterval(); interval > 0 {
		buckets = s.debounceRebuildBuckets(dedupeBuckets(buckets), interval)
		if len(buckets) == 0 {
			return nil
		}
	}
	tasks, firstErr := s.prepareBucketWriteTasks(ctx, buckets)
	queries := newSchedulerAccountQueryCache(tasks)
	if err := s.rebuildPreparedBucketTasks(ctx, tasks, reason, false, queries); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// debounceRebuildBuckets 返回本次需要立即重建的 bucket；间隔内已重建过的 bucket
// 转入 pendingRebuild 等待 drain goroutine 兜底，调用方视其为已受理（返回 nil），
// outbox watermark 照常推进。
func (s *SchedulerSnapshotService) debounceRebuildBuckets(buckets []SchedulerBucket, interval time.Duration) []SchedulerBucket {
	now := time.Now()
	due := make([]SchedulerBucket, 0, len(buckets))
	s.rebuildMu.Lock()
	if s.lastRebuildAt == nil {
		s.lastRebuildAt = make(map[SchedulerBucket]time.Time)
	}
	if s.pendingRebuild == nil {
		s.pendingRebuild = make(map[SchedulerBucket]struct{})
	}
	for _, bucket := range buckets {
		if last, ok := s.lastRebuildAt[bucket]; ok && now.Sub(last) < interval {
			s.pendingRebuild[bucket] = struct{}{}
			continue
		}
		s.lastRebuildAt[bucket] = now
		due = append(due, bucket)
	}
	s.rebuildMu.Unlock()
	return due
}

func (s *SchedulerSnapshotService) runRebuildDebounceWorker(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.drainPendingRebuilds(interval)
		case <-s.stopCh:
			return
		}
	}
}

func (s *SchedulerSnapshotService) drainPendingRebuilds(interval time.Duration) {
	now := time.Now()
	s.rebuildMu.Lock()
	// 过期的 lastRebuildAt 条目不再抑制任何请求，顺手回收避免 map 无界增长。
	for bucket, last := range s.lastRebuildAt {
		if now.Sub(last) >= interval {
			delete(s.lastRebuildAt, bucket)
		}
	}
	if len(s.pendingRebuild) == 0 {
		s.rebuildMu.Unlock()
		return
	}
	buckets := make([]SchedulerBucket, 0, len(s.pendingRebuild))
	for bucket := range s.pendingRebuild {
		buckets = append(buckets, bucket)
		s.lastRebuildAt[bucket] = now
	}
	s.pendingRebuild = make(map[SchedulerBucket]struct{})
	s.rebuildMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), outboxEventTimeout)
	defer cancel()
	tasks, firstErr := s.prepareBucketWriteTasks(ctx, buckets)
	queries := newSchedulerAccountQueryCache(tasks)
	if err := s.rebuildPreparedBucketTasks(ctx, tasks, "rebuild_debounce", false, queries); err != nil && firstErr == nil {
		firstErr = err
	}
	if firstErr != nil {
		// 重建幂等：全部重新入队，下个 tick 整批重试。
		s.rebuildMu.Lock()
		if s.pendingRebuild == nil {
			s.pendingRebuild = make(map[SchedulerBucket]struct{})
		}
		for _, bucket := range buckets {
			s.pendingRebuild[bucket] = struct{}{}
		}
		s.rebuildMu.Unlock()
		logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] debounce drain rebuild failed: buckets=%d err=%v", len(buckets), firstErr)
	}
}

func (s *SchedulerSnapshotService) prepareBucketWriteTasks(ctx context.Context, buckets []SchedulerBucket) ([]schedulerBucketWriteTask, error) {
	cache, err := s.lifecycleCache()
	if err != nil {
		return nil, err
	}
	tasks := make([]schedulerBucketWriteTask, 0, len(buckets))
	var firstErr error
	for _, bucket := range dedupeBuckets(buckets) {
		token, captureErr := cache.CaptureBucketWriteToken(ctx, bucket)
		if captureErr != nil {
			if errors.Is(captureErr, ErrSchedulerBucketRetired) || errors.Is(captureErr, ErrSchedulerBucketWriteFenced) {
				continue
			}
			if firstErr == nil {
				firstErr = captureErr
			}
			continue
		}
		tasks = append(tasks, schedulerBucketWriteTask{bucket: bucket, token: token})
	}
	return tasks, firstErr
}

func (s *SchedulerSnapshotService) rebuildPreparedBucketTasks(ctx context.Context, tasks []schedulerBucketWriteTask, reason string, strict bool, queries *schedulerAccountQueryCache) error {
	var firstErr error
	for _, task := range tasks {
		if err := s.rebuildBucketWithTokenPolicyAndQueryCache(ctx, task, reason, strict, queries); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *SchedulerSnapshotService) rebuildBucketWithTokenPolicyAndQueryCache(ctx context.Context, task schedulerBucketWriteTask, reason string, strict bool, queries *schedulerAccountQueryCache) error {
	if queries != nil {
		defer queries.release(task.bucket)
	}
	cache, err := s.lifecycleCache()
	if err != nil {
		return err
	}
	bucket := task.bucket
	ok, err := s.cache.TryLockBucket(ctx, bucket, 30*time.Second)
	if err != nil {
		return err
	}
	if !ok {
		if strict {
			return fmt.Errorf("%w: bucket=%s", ErrSchedulerBucketRebuildBusy, bucket.String())
		}
		return nil
	}
	defer func() {
		_ = s.cache.UnlockBucket(ctx, bucket)
	}()

	rebuildCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	accounts, err := s.loadAccountsForRebuild(rebuildCtx, bucket, queries)
	if err != nil {
		logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] rebuild failed: bucket=%s reason=%s err=%v", bucket.String(), reason, err)
		return err
	}
	if err := cache.SetSnapshotFenced(rebuildCtx, bucket, task.token, accounts); err != nil {
		if errors.Is(err, ErrSchedulerBucketRetired) || errors.Is(err, ErrSchedulerBucketWriteFenced) {
			slog.Debug("[Scheduler] rebuild fenced", "bucket", bucket.String(), "reason", reason)
			if strict {
				return err
			}
			return nil
		}
		logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] rebuild cache failed: bucket=%s reason=%s err=%v", bucket.String(), reason, err)
		return err
	}
	slog.Debug("[Scheduler] rebuild ok", "bucket", bucket.String(), "reason", reason, "size", len(accounts))
	return nil
}

func (s *SchedulerSnapshotService) triggerFullRebuild(reason string) error {
	if s.cache == nil {
		return ErrSchedulerCacheNotReady
	}
	return s.coalesceFullRebuild(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		return s.rebuildFullSnapshot(ctx, reason)
	})
}

func (s *SchedulerSnapshotService) rebuildFullSnapshot(ctx context.Context, reason string) error {
	if _, err := s.lifecycleCache(); err != nil {
		return err
	}
	registered, err := s.cache.ListBuckets(ctx)
	if err != nil {
		return err
	}
	registered = dedupeBuckets(registered)

	if s.isRunModeSimple() {
		canonical := schedulerCanonicalBuckets(0)
		captured, err := s.captureFullRebuildCanonicalTasks(ctx, canonical)
		if err != nil {
			return err
		}
		ordinary := appendBucketsExcept(nil, registered, canonical)
		return s.prepareAndRebuildFullSnapshot(ctx, captured, nil, ordinary, reason)
	}

	activeGroupIDs, err := s.listActiveSchedulerGroupIDs(ctx)
	if err != nil {
		return err
	}
	activeGroups := make(map[int64]struct{}, len(activeGroupIDs))
	for _, groupID := range activeGroupIDs {
		activeGroups[groupID] = struct{}{}
	}

	registeredByGroup := make(map[int64][]SchedulerBucket)
	for _, bucket := range registered {
		registeredByGroup[bucket.GroupID] = append(registeredByGroup[bucket.GroupID], bucket)
	}

	groupZeroCanonical := schedulerCanonicalBuckets(0)
	capturedTasks, err := s.captureFullRebuildCanonicalTasks(ctx, groupZeroCanonical)
	if err != nil {
		return err
	}
	ordinaryBuckets := appendBucketsExcept(nil, registeredByGroup[0], groupZeroCanonical)
	for groupID, buckets := range registeredByGroup {
		if groupID < 0 {
			ordinaryBuckets = append(ordinaryBuckets, buckets...)
		}
	}

	reopenedTasks := make([]schedulerBucketWriteTask, 0)
	for _, groupID := range activeGroupIDs {
		canonical := schedulerBucketsForGroup(groupID)
		canonicalTasks, captureErr := s.captureFullRebuildCanonicalTasks(ctx, canonical)
		if captureErr == nil {
			capturedTasks = append(capturedTasks, canonicalTasks...)
			ordinaryBuckets = appendBucketsExcept(ordinaryBuckets, registeredByGroup[groupID], canonical)
			continue
		}
		if !errors.Is(captureErr, ErrSchedulerBucketRetired) && !errors.Is(captureErr, ErrSchedulerBucketWriteFenced) {
			return captureErr
		}
		knownHistorical := registeredByGroup[groupID]
		if knownHistorical == nil {
			knownHistorical = []SchedulerBucket{}
		}
		plan, err := s.prepareGroupLifecycle(ctx, groupID, knownHistorical)
		if err != nil {
			return err
		}
		if plan.active {
			reopenedTasks = append(reopenedTasks, plan.tasks...)
			ordinaryBuckets = appendBucketsExcept(ordinaryBuckets, registeredByGroup[groupID], canonical)
		}
	}

	staleGroupIDs := make([]int64, 0)
	for groupID := range registeredByGroup {
		if groupID <= 0 {
			continue
		}
		if _, active := activeGroups[groupID]; !active {
			staleGroupIDs = append(staleGroupIDs, groupID)
		}
	}
	sort.Slice(staleGroupIDs, func(i, j int) bool { return staleGroupIDs[i] < staleGroupIDs[j] })
	for _, groupID := range staleGroupIDs {
		plan, err := s.prepareGroupLifecycle(ctx, groupID, registeredByGroup[groupID])
		if err != nil {
			return err
		}
		if plan.active {
			reopenedTasks = append(reopenedTasks, plan.tasks...)
			ordinaryBuckets = appendBucketsExcept(ordinaryBuckets, registeredByGroup[groupID], schedulerBucketsForGroup(groupID))
		}
	}
	return s.prepareAndRebuildFullSnapshot(ctx, capturedTasks, reopenedTasks, ordinaryBuckets, reason)
}

func (s *SchedulerSnapshotService) listActiveSchedulerGroupIDs(ctx context.Context) ([]int64, error) {
	if s.groupRepo == nil {
		return nil, ErrSchedulerCacheNotReady
	}
	var groupIDs []int64
	if lister, ok := s.groupRepo.(schedulerActiveGroupIDLister); ok {
		ids, err := lister.ListActiveIDs(ctx)
		if err != nil {
			return nil, err
		}
		groupIDs = ids
	} else {
		groups, err := s.groupRepo.ListActive(ctx)
		if err != nil {
			return nil, err
		}
		groupIDs = make([]int64, 0, len(groups))
		for _, group := range groups {
			groupIDs = append(groupIDs, group.ID)
		}
	}
	seen := make(map[int64]struct{}, len(groupIDs))
	normalized := make([]int64, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		if groupID <= 0 {
			continue
		}
		if _, ok := seen[groupID]; ok {
			continue
		}
		seen[groupID] = struct{}{}
		normalized = append(normalized, groupID)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i] < normalized[j] })
	return normalized, nil
}

func (s *SchedulerSnapshotService) prepareAndRebuildFullSnapshot(ctx context.Context, captured, reopened []schedulerBucketWriteTask, ordinaryBuckets []SchedulerBucket, reason string) error {
	preparedBuckets := make(map[SchedulerBucket]struct{}, len(captured)+len(reopened))
	for _, task := range captured {
		preparedBuckets[task.bucket] = struct{}{}
	}
	for _, task := range reopened {
		preparedBuckets[task.bucket] = struct{}{}
	}
	ordinaryBuckets = dedupeBuckets(ordinaryBuckets)
	toCapture := make([]SchedulerBucket, 0, len(ordinaryBuckets))
	for _, bucket := range ordinaryBuckets {
		if _, ok := preparedBuckets[bucket]; !ok {
			toCapture = append(toCapture, bucket)
		}
	}
	ordinary, firstErr := s.prepareBucketWriteTasks(ctx, toCapture)
	if firstErr != nil {
		return firstErr
	}
	captured = append(captured, ordinary...)
	queries := newSchedulerAccountQueryCache(reopened, captured)
	if err := s.rebuildPreparedBucketTasks(ctx, reopened, reason, true, queries); err != nil {
		firstErr = err
	}
	if err := s.rebuildPreparedBucketTasks(ctx, captured, reason, false, queries); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func (s *SchedulerSnapshotService) captureFullRebuildCanonicalTasks(ctx context.Context, buckets []SchedulerBucket) ([]schedulerBucketWriteTask, error) {
	cache, err := s.lifecycleCache()
	if err != nil {
		return nil, err
	}
	tasks := make([]schedulerBucketWriteTask, 0, len(buckets))
	for _, bucket := range buckets {
		token, err := cache.CaptureBucketWriteToken(ctx, bucket)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, schedulerBucketWriteTask{bucket: bucket, token: token})
	}
	return tasks, nil
}

func appendBucketsExcept(dst, buckets, excluded []SchedulerBucket) []SchedulerBucket {
	excludedKeys := make(map[SchedulerBucket]struct{}, len(excluded))
	for _, bucket := range excluded {
		excludedKeys[bucket] = struct{}{}
	}
	for _, bucket := range buckets {
		if _, ok := excludedKeys[bucket]; !ok {
			dst = append(dst, bucket)
		}
	}
	return dst
}

func (s *SchedulerSnapshotService) coalesceFullRebuild(run func() error) error {
	s.fullRebuildStateMu.Lock()
	s.fullRebuildRequested++
	requestID := s.fullRebuildRequested
	s.fullRebuildStateMu.Unlock()

	s.fullRebuildRunMu.Lock()
	defer s.fullRebuildRunMu.Unlock()

	s.fullRebuildStateMu.Lock()
	if s.fullRebuildCompleted >= requestID {
		err := s.fullRebuildLastErr
		s.fullRebuildStateMu.Unlock()
		return err
	}
	coveredThrough := s.fullRebuildRequested
	s.fullRebuildStateMu.Unlock()

	err := run()
	s.fullRebuildStateMu.Lock()
	s.fullRebuildCompleted = coveredThrough
	s.fullRebuildLastErr = err
	s.fullRebuildStateMu.Unlock()
	return err
}

func (s *SchedulerSnapshotService) checkOutboxLag(ctx context.Context, watermark int64) {
	if s.cfg == nil || s.outboxRepo == nil {
		return
	}
	now := time.Now()
	oldestCreatedAt, ok, err := s.outboxRepo.FirstCreatedAtAfter(ctx, watermark)
	if err != nil {
		logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] outbox pending event read failed: %v", err)
		return
	}
	var lag time.Duration
	if ok && !oldestCreatedAt.IsZero() {
		lag = now.Sub(oldestCreatedAt)
	}
	lagSeconds := int(lag.Seconds())
	lagWarning := ok && !oldestCreatedAt.IsZero() &&
		s.cfg.Gateway.Scheduling.OutboxLagWarnSeconds > 0 &&
		lagSeconds >= s.cfg.Gateway.Scheduling.OutboxLagWarnSeconds
	lagDegraded := ok && !oldestCreatedAt.IsZero() &&
		s.cfg.Gateway.Scheduling.OutboxLagRebuildSeconds > 0 &&
		lagSeconds >= s.cfg.Gateway.Scheduling.OutboxLagRebuildSeconds

	backlogThreshold := s.cfg.Gateway.Scheduling.OutboxBacklogRebuildRows
	backlogKnown := true
	var backlog int64
	if backlogThreshold > 0 {
		maxID, maxErr := s.outboxRepo.MaxID(ctx)
		if maxErr != nil {
			backlogKnown = false
			if s.shouldLogOutboxMaxIDError(now) {
				logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] outbox max id read failed: %v", maxErr)
			}
		} else {
			backlog = maxID - watermark
		}
	}
	backlogDegraded := backlogKnown && backlogThreshold > 0 && backlog >= int64(backlogThreshold)

	logLagWarning := s.shouldLogOutboxLagWarning(lagWarning)
	s.lagMu.Lock()
	fullyRecovered := !lagDegraded && backlogKnown && !backlogDegraded
	if fullyRecovered {
		s.lagFailures = 0
		s.outboxRebuildLatched = false
		s.outboxRebuildFailures = 0
		s.outboxRebuildRetryAt = time.Time{}
		s.outboxRebuildRetryReason = ""
	}
	if s.outboxRebuildRetryReason != "" {
		retryReasonActive := (s.outboxRebuildRetryReason == "outbox_lag" && lagDegraded) ||
			(s.outboxRebuildRetryReason == "outbox_backlog" && (!backlogKnown || backlogDegraded))
		if !retryReasonActive {
			s.outboxRebuildFailures = 0
			s.outboxRebuildRetryAt = time.Time{}
			s.outboxRebuildRetryReason = ""
		}
	}
	lagRetryPending := s.outboxRebuildRetryReason == "outbox_lag" && !s.outboxRebuildRetryAt.IsZero()
	if lagDegraded {
		if !s.outboxRebuildLatched && !s.outboxRebuildRunning && !lagRetryPending {
			s.lagFailures++
		}
	} else {
		s.lagFailures = 0
	}
	failures := s.lagFailures
	lagReady := lagDegraded && failures >= s.cfg.Gateway.Scheduling.OutboxLagRebuildFailures
	retryDue := s.outboxRebuildRetryReason != "" && !s.outboxRebuildRetryAt.IsZero() && !now.Before(s.outboxRebuildRetryAt)

	reason := ""
	lagCanPreemptRetry := lagReady && s.outboxRebuildRetryReason != "outbox_lag"
	if !s.outboxRebuildLatched && !s.outboxRebuildRunning &&
		(s.outboxRebuildRetryAt.IsZero() || retryDue || lagCanPreemptRetry) {
		switch {
		case lagReady || (retryDue && s.outboxRebuildRetryReason == "outbox_lag" && lagDegraded):
			if s.outboxRebuildRetryReason != "" && s.outboxRebuildRetryReason != "outbox_lag" {
				s.outboxRebuildFailures = 0
				s.outboxRebuildRetryAt = time.Time{}
				s.outboxRebuildRetryReason = ""
			}
			reason = "outbox_lag"
			s.lagFailures = 0
		case backlogDegraded && (s.outboxRebuildRetryReason == "" ||
			(retryDue && s.outboxRebuildRetryReason == "outbox_backlog")):
			reason = "outbox_backlog"
		}
		if reason != "" {
			s.outboxRebuildRunning = true
		}
	}
	s.lagMu.Unlock()

	if logLagWarning {
		logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] outbox lag warning: %ds", lagSeconds)
	}
	if reason == "" {
		return
	}

	logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] %s rebuild triggered: lag=%s backlog=%d failures=%d", reason, lag, backlog, failures)
	rebuildErr := s.triggerFullRebuild(reason)

	s.lagMu.Lock()
	s.outboxRebuildRunning = false
	if rebuildErr == nil {
		s.outboxRebuildLatched = true
		s.outboxRebuildFailures = 0
		s.outboxRebuildRetryAt = time.Time{}
		s.outboxRebuildRetryReason = ""
	} else {
		s.outboxRebuildLatched = false
		s.outboxRebuildFailures++
		s.outboxRebuildRetryAt = time.Now().Add(outboxRebuildRetryDelay(s.outboxRebuildFailures))
		s.outboxRebuildRetryReason = reason
	}
	s.lagMu.Unlock()
	if rebuildErr != nil {
		logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] %s rebuild failed: %v", reason, rebuildErr)
	}
}

func outboxRebuildRetryDelay(failures int) time.Duration {
	delay := outboxRebuildRetryBaseDelay
	for i := 1; i < failures && delay < outboxRebuildRetryMaxDelay; i++ {
		delay *= 2
		if delay >= outboxRebuildRetryMaxDelay {
			return outboxRebuildRetryMaxDelay
		}
	}
	return delay
}

func (s *SchedulerSnapshotService) clearOutboxDegradedEpisode() {
	if s == nil {
		return
	}
	s.lagMu.Lock()
	if s.lagFailures != 0 || s.outboxRebuildLatched || s.outboxRebuildRunning ||
		s.outboxRebuildFailures != 0 || !s.outboxRebuildRetryAt.IsZero() ||
		s.outboxRebuildRetryReason != "" || s.outboxLagWarningActive {
		s.lagFailures = 0
		s.outboxRebuildLatched = false
		s.outboxRebuildFailures = 0
		s.outboxRebuildRetryAt = time.Time{}
		s.outboxRebuildRetryReason = ""
		s.outboxLagWarningActive = false
	}
	s.lagMu.Unlock()
}

func (s *SchedulerSnapshotService) shouldLogOutboxMaxIDError(now time.Time) bool {
	s.lagMu.Lock()
	defer s.lagMu.Unlock()
	if !s.outboxMaxIDErrorLastLoggedAt.IsZero() && now.Sub(s.outboxMaxIDErrorLastLoggedAt) < outboxMaxIDErrorLogSampleInterval {
		return false
	}
	s.outboxMaxIDErrorLastLoggedAt = now
	return true
}

func (s *SchedulerSnapshotService) shouldLogOutboxLagWarning(active bool) bool {
	s.lagMu.Lock()
	defer s.lagMu.Unlock()
	shouldLog := active && !s.outboxLagWarningActive
	s.outboxLagWarningActive = active
	return shouldLog
}

func (s *SchedulerSnapshotService) loadAccountsFromDB(ctx context.Context, bucket SchedulerBucket, useMixed bool) ([]Account, error) {
	if s.accountRepo == nil {
		return nil, ErrSchedulerCacheNotReady
	}
	groupID := bucket.GroupID
	if s.isRunModeSimple() {
		groupID = 0
	}

	if useMixed {
		platforms := []string{bucket.Platform, PlatformAntigravity}
		var accounts []Account
		var err error
		if groupID > 0 {
			accounts, err = s.accountRepo.ListSchedulableByGroupIDAndPlatforms(ctx, groupID, platforms)
		} else if s.isRunModeSimple() {
			accounts, err = s.accountRepo.ListSchedulableByPlatforms(ctx, platforms)
		} else {
			accounts, err = s.accountRepo.ListSchedulableUngroupedByPlatforms(ctx, platforms)
		}
		if err != nil {
			return nil, err
		}
		out := accounts[:0]
		for _, acc := range accounts {
			if !acc.IsSchedulableWithoutCodexQuotaProtection() {
				continue
			}
			if acc.Platform == PlatformAntigravity && !acc.IsMixedSchedulingEnabled() {
				continue
			}
			out = append(out, acc)
		}
		return out, nil
	}

	filterSchedulable := func(accounts []Account, err error) ([]Account, error) {
		if err != nil {
			return nil, err
		}
		return filterSchedulableAccountsForSnapshot(accounts), nil
	}
	if groupID > 0 {
		return filterSchedulable(s.accountRepo.ListSchedulableByGroupIDAndPlatform(ctx, groupID, bucket.Platform))
	}
	if s.isRunModeSimple() {
		return filterSchedulable(s.accountRepo.ListSchedulableByPlatform(ctx, bucket.Platform))
	}
	return filterSchedulable(s.accountRepo.ListSchedulableUngroupedByPlatform(ctx, bucket.Platform))
}

func (s *SchedulerSnapshotService) loadAccountsForRebuild(ctx context.Context, bucket SchedulerBucket, queries *schedulerAccountQueryCache) ([]Account, error) {
	key, cacheable := schedulerAccountQueryKeyForBucket(bucket)
	if queries == nil || !cacheable {
		return s.loadAccountsFromDB(ctx, bucket, bucket.Mode == SchedulerModeMixed)
	}
	if accounts, ok := queries.accounts[key]; ok {
		return accounts, nil
	}
	if queries.remaining[key] <= 1 {
		return s.loadAccountsFromDB(ctx, bucket, false)
	}
	accounts, err := s.loadAccountsFromDB(ctx, bucket, false)
	if err != nil {
		return nil, err
	}
	queries.accounts[key] = accounts
	return accounts, nil
}

func filterSchedulableAccounts(accounts []Account) []Account {
	filtered := make([]Account, 0, len(accounts))
	for _, acc := range accounts {
		if acc.IsSchedulable() {
			filtered = append(filtered, acc)
		}
	}
	return filtered
}

func filterSchedulableAccountsForSnapshot(accounts []Account) []Account {
	filtered := make([]Account, 0, len(accounts))
	for _, acc := range accounts {
		if acc.IsSchedulableWithoutCodexQuotaProtection() {
			filtered = append(filtered, acc)
		}
	}
	return filtered
}

func (s *SchedulerSnapshotService) bucketFor(groupID *int64, platform string, mode string) SchedulerBucket {
	return SchedulerBucket{
		GroupID:  s.normalizeGroupID(groupID),
		Platform: platform,
		Mode:     mode,
	}
}

func (s *SchedulerSnapshotService) normalizeGroupID(groupID *int64) int64 {
	if s.isRunModeSimple() {
		return 0
	}
	if groupID == nil || *groupID <= 0 {
		return 0
	}
	return *groupID
}

func (s *SchedulerSnapshotService) normalizeGroupIDs(groupIDs []int64) []int64 {
	if s.isRunModeSimple() {
		return []int64{0}
	}
	if len(groupIDs) == 0 {
		return []int64{0}
	}
	seen := make(map[int64]struct{}, len(groupIDs))
	out := make([]int64, 0, len(groupIDs))
	for _, id := range groupIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return []int64{0}
	}
	return out
}

func (s *SchedulerSnapshotService) resolveMode(platform string, hasForcePlatform bool) string {
	if hasForcePlatform {
		return SchedulerModeForced
	}
	if platform == PlatformAnthropic || platform == PlatformGemini {
		return SchedulerModeMixed
	}
	return SchedulerModeSingle
}

func (s *SchedulerSnapshotService) guardFallback(ctx context.Context) error {
	if s.cfg == nil || s.cfg.Gateway.Scheduling.DbFallbackEnabled {
		if s.fallbackLimit == nil || s.fallbackLimit.Allow() {
			return nil
		}
		return ErrSchedulerFallbackLimited
	}
	return ErrSchedulerCacheNotReady
}

func (s *SchedulerSnapshotService) withFallbackTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if s.cfg == nil || s.cfg.Gateway.Scheduling.DbFallbackTimeoutSeconds <= 0 {
		return context.WithCancel(ctx)
	}
	timeout := time.Duration(s.cfg.Gateway.Scheduling.DbFallbackTimeoutSeconds) * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return context.WithCancel(ctx)
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	return context.WithTimeout(ctx, timeout)
}

func (s *SchedulerSnapshotService) isRunModeSimple() bool {
	return s.cfg != nil && s.cfg.RunMode == config.RunModeSimple
}

func (s *SchedulerSnapshotService) outboxPollInterval() time.Duration {
	if s.cfg == nil {
		return time.Second
	}
	sec := s.cfg.Gateway.Scheduling.OutboxPollIntervalSeconds
	if sec <= 0 {
		return time.Second
	}
	return time.Duration(sec) * time.Second
}

// rebuildDebounceInterval 返回增量重建去抖间隔；0 表示关闭（直通旧逻辑，可作回滚开关）。
func (s *SchedulerSnapshotService) rebuildDebounceInterval() time.Duration {
	if s == nil || s.cfg == nil {
		return 0
	}
	sec := s.cfg.Gateway.Scheduling.RebuildDebounceSeconds
	if sec <= 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}

func (s *SchedulerSnapshotService) fullRebuildInterval() time.Duration {
	if s.cfg == nil {
		return 0
	}
	sec := s.cfg.Gateway.Scheduling.FullRebuildIntervalSeconds
	if sec <= 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}

func (s *SchedulerSnapshotService) candidateIndexLimit() int {
	if s == nil || s.cfg == nil || s.cfg.Gateway.Scheduling.IndexedCandidateLimit <= 0 {
		return 256
	}
	return s.cfg.Gateway.Scheduling.IndexedCandidateLimit
}

func (s *SchedulerSnapshotService) defaultBuckets(ctx context.Context) ([]SchedulerBucket, error) {
	buckets := make([]SchedulerBucket, 0)
	for _, platform := range schedulerSnapshotPlatforms() {
		buckets = append(buckets, SchedulerBucket{GroupID: 0, Platform: platform, Mode: SchedulerModeSingle})
		buckets = append(buckets, SchedulerBucket{GroupID: 0, Platform: platform, Mode: SchedulerModeForced})
		if platform == PlatformAnthropic || platform == PlatformGemini {
			buckets = append(buckets, SchedulerBucket{GroupID: 0, Platform: platform, Mode: SchedulerModeMixed})
		}
	}

	if s.isRunModeSimple() || s.groupRepo == nil {
		return dedupeBuckets(buckets), nil
	}

	groups, err := s.groupRepo.ListActive(ctx)
	if err != nil {
		return dedupeBuckets(buckets), nil
	}
	for _, group := range groups {
		if group.Platform == "" {
			continue
		}
		buckets = append(buckets, SchedulerBucket{GroupID: group.ID, Platform: group.Platform, Mode: SchedulerModeSingle})
		buckets = append(buckets, SchedulerBucket{GroupID: group.ID, Platform: group.Platform, Mode: SchedulerModeForced})
		if group.Platform == PlatformAnthropic || group.Platform == PlatformGemini {
			buckets = append(buckets, SchedulerBucket{GroupID: group.ID, Platform: group.Platform, Mode: SchedulerModeMixed})
		}
	}
	return dedupeBuckets(buckets), nil
}

func dedupeBuckets(in []SchedulerBucket) []SchedulerBucket {
	seen := make(map[string]struct{}, len(in))
	out := make([]SchedulerBucket, 0, len(in))
	for _, bucket := range in {
		key := bucket.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, bucket)
	}
	return out
}

func derefAccounts(accounts []*Account) []Account {
	if len(accounts) == 0 {
		return []Account{}
	}
	out := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if account == nil {
			continue
		}
		out = append(out, *account)
	}
	return out
}

func parseInt64Slice(value any) []int64 {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]int64, 0, len(raw))
	for _, item := range raw {
		if v, ok := toInt64(item); ok && v > 0 {
			out = append(out, v)
		}
	}
	return out
}

func toInt64(value any) (int64, bool) {
	switch v := value.(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	case json.Number:
		parsed, err := strconv.ParseInt(v.String(), 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

type fallbackLimiter struct {
	maxQPS int
	mu     sync.Mutex
	window time.Time
	count  int
}

func newFallbackLimiter(maxQPS int) *fallbackLimiter {
	if maxQPS <= 0 {
		return nil
	}
	return &fallbackLimiter{
		maxQPS: maxQPS,
		window: time.Now(),
	}
}

func (l *fallbackLimiter) Allow() bool {
	if l == nil || l.maxQPS <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	if now.Sub(l.window) >= time.Second {
		l.window = now
		l.count = 0
	}
	if l.count >= l.maxQPS {
		return false
	}
	l.count++
	return true
}
