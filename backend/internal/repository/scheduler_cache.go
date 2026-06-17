package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	schedulerBucketSetKey       = "sched:buckets"
	schedulerOutboxWatermarkKey = "sched:outbox:watermark"
	schedulerAccountPrefix      = "sched:acc:"
	schedulerAccountMetaPrefix  = "sched:meta:"
	schedulerActivePrefix       = "sched:active:"
	schedulerReadyPrefix        = "sched:ready:"
	schedulerVersionPrefix      = "sched:ver:"
	schedulerSnapshotPrefix     = "sched:"
	schedulerLockPrefix         = "sched:lock:"
	// Legacy candidate-index keys are kept only for expiring indexes written by
	// earlier builds. Candidate reads now sample the active snapshot directly.
	schedulerCandidateIndexPrefix  = "schedidx:v1:"
	schedulerCandidateActivePrefix = "schedidx:active:"
	schedulerCandidateReadyPrefix  = "schedidx:ready:"
	schedulerCandidateMetaPrefix   = "schedidx:meta:"

	defaultSchedulerSnapshotMGetChunkSize  = 128
	defaultSchedulerSnapshotWriteChunkSize = 256
	defaultSchedulerSnapshotLocalCacheTTL  = 5 * time.Second
	defaultSchedulerCandidateLimit         = 256
	maxSchedulerCandidateLimit             = 1024
	minSchedulerCandidateShardSize         = 5000

	// snapshotGraceTTLSeconds 旧快照过期的宽限期（秒）。
	// 替代立即 DEL，让正在读取旧版本的 reader 有足够时间完成 ZRANGE。
	snapshotGraceTTLSeconds = 60
)

var (
	// activateSnapshotScript 原子 CAS 切换快照版本。
	// 仅当新版本号 >= 当前激活版本时才切换，防止并发写入导致版本回滚。
	// 旧快照使用 EXPIRE 设置宽限期而非立即 DEL，避免与 reader 竞态。
	//
	// KEYS[1] = activeKey     (sched:active:{bucket})
	// KEYS[2] = readyKey      (sched:ready:{bucket})
	// KEYS[3] = bucketSetKey  (sched:buckets)
	// KEYS[4] = snapshotKey   (新写入的快照 key)
	// ARGV[1] = 新版本号字符串
	// ARGV[2] = bucket 字符串 (用于 SADD)
	// ARGV[3] = 快照 key 前缀 (用于构造旧快照 key)
	// ARGV[4] = 宽限期 TTL 秒数
	//
	// 返回 1 = 已激活, 0 = 版本过旧未激活
	activateSnapshotScript = redis.NewScript(`
local currentActive = redis.call('GET', KEYS[1])
local newVersion = tonumber(ARGV[1])

if currentActive ~= false then
	local curVersion = tonumber(currentActive)
	if curVersion and newVersion < curVersion then
		redis.call('DEL', KEYS[4])
		return 0
	end
end

redis.call('SET', KEYS[1], ARGV[1])
redis.call('SET', KEYS[2], '1')
redis.call('SADD', KEYS[3], ARGV[2])

if currentActive ~= false and currentActive ~= ARGV[1] then
	redis.call('EXPIRE', ARGV[3] .. currentActive, tonumber(ARGV[4]))
end

return 1
`)
	clearEmptySnapshotScript = redis.NewScript(`
local currentActive = redis.call('GET', KEYS[1])
local newVersion = tonumber(ARGV[1])

if currentActive ~= false then
	local curVersion = tonumber(currentActive)
	if curVersion and newVersion < curVersion then
		return 0
	end
	redis.call('EXPIRE', ARGV[3] .. currentActive, tonumber(ARGV[4]))
end

redis.call('DEL', KEYS[1])
redis.call('DEL', KEYS[2])
redis.call('SREM', KEYS[3], ARGV[2])

return 1
`)
)

type schedulerCache struct {
	rdb            *redis.Client
	mgetChunkSize  int
	writeChunkSize int
	indexedBuckets map[string]struct{}
	localMu        sync.RWMutex
	localSnapshots map[string]schedulerLocalSnapshot
	localBuckets   map[int64]map[string]struct{}
	localTTL       time.Duration
}

type schedulerLocalSnapshot struct {
	activeVersion string
	expiresAt     time.Time
	accounts      []*service.Account
}

func NewSchedulerCache(rdb *redis.Client) service.SchedulerCache {
	return newSchedulerCacheWithChunkSizes(rdb, defaultSchedulerSnapshotMGetChunkSize, defaultSchedulerSnapshotWriteChunkSize)
}

func newSchedulerCacheWithChunkSizes(rdb *redis.Client, mgetChunkSize, writeChunkSize int) service.SchedulerCache {
	return newSchedulerCacheWithOptions(rdb, mgetChunkSize, writeChunkSize, nil)
}

func newSchedulerCacheWithOptions(rdb *redis.Client, mgetChunkSize, writeChunkSize int, indexedBuckets []string) service.SchedulerCache {
	if mgetChunkSize <= 0 {
		mgetChunkSize = defaultSchedulerSnapshotMGetChunkSize
	}
	if writeChunkSize <= 0 {
		writeChunkSize = defaultSchedulerSnapshotWriteChunkSize
	}
	indexed := make(map[string]struct{}, len(indexedBuckets))
	for _, raw := range indexedBuckets {
		if bucket, ok := service.ParseSchedulerBucket(raw); ok {
			indexed[bucket.String()] = struct{}{}
		}
	}
	return &schedulerCache{
		rdb:            rdb,
		mgetChunkSize:  mgetChunkSize,
		writeChunkSize: writeChunkSize,
		indexedBuckets: indexed,
		localSnapshots: make(map[string]schedulerLocalSnapshot),
		localBuckets:   make(map[int64]map[string]struct{}),
		localTTL:       defaultSchedulerSnapshotLocalCacheTTL,
	}
}

func (c *schedulerCache) GetSnapshot(ctx context.Context, bucket service.SchedulerBucket) ([]*service.Account, bool, error) {
	readyKey := schedulerBucketKey(schedulerReadyPrefix, bucket)
	readyVal, err := c.rdb.Get(ctx, readyKey).Result()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if readyVal != "1" {
		return nil, false, nil
	}

	activeKey := schedulerBucketKey(schedulerActivePrefix, bucket)
	activeVal, err := c.rdb.Get(ctx, activeKey).Result()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	cacheKey := bucket.String()
	if accounts, ok := c.getLocalSnapshot(cacheKey, activeVal); ok {
		return accounts, true, nil
	}

	snapshotKey := schedulerSnapshotKey(bucket, activeVal)
	ids, err := c.rdb.ZRange(ctx, snapshotKey, 0, -1).Result()
	if err != nil {
		return nil, false, err
	}
	if len(ids) == 0 {
		// 空快照视为缓存未命中，触发数据库回退查询
		// 这解决了新分组创建后立即绑定账号时的竞态条件问题
		return nil, false, nil
	}

	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, schedulerAccountMetaKey(id))
	}
	values, err := c.mgetChunked(ctx, keys)
	if err != nil {
		return nil, false, err
	}

	accounts := make([]*service.Account, 0, len(values))
	for _, val := range values {
		if val == nil {
			return nil, false, nil
		}
		account, err := decodeCachedAccount(val)
		if err != nil {
			return nil, false, err
		}
		accounts = append(accounts, account)
	}

	c.setLocalSnapshot(cacheKey, activeVal, accounts)
	return accounts, true, nil
}

func (c *schedulerCache) SetSnapshot(ctx context.Context, bucket service.SchedulerBucket, accounts []service.Account) error {
	// Phase 1: 分配新版本号并写入快照数据。
	// INCR 保证每个调用方获得唯一递增版本号。
	// 写入的 snapshotKey 是新的版本化 key，reader 尚不知晓，因此无竞态。
	versionKey := schedulerBucketKey(schedulerVersionPrefix, bucket)
	version, err := c.rdb.Incr(ctx, versionKey).Result()
	if err != nil {
		return err
	}

	versionStr := strconv.FormatInt(version, 10)
	snapshotKey := schedulerSnapshotKey(bucket, versionStr)

	if err := c.writeAccounts(ctx, accounts); err != nil {
		return err
	}

	if len(accounts) == 0 {
		return c.clearEmptySnapshot(ctx, bucket, versionStr)
	}

	// 使用序号作为 score，保持数据库返回的排序语义。
	members := make([]redis.Z, 0, len(accounts))
	for idx, account := range accounts {
		members = append(members, redis.Z{
			Score:  float64(idx),
			Member: strconv.FormatInt(account.ID, 10),
		})
	}
	pipe := c.rdb.Pipeline()
	for start := 0; start < len(members); start += c.writeChunkSize {
		end := start + c.writeChunkSize
		if end > len(members) {
			end = len(members)
		}
		pipe.ZAdd(ctx, snapshotKey, members[start:end]...)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}

	// Phase 2: 原子 CAS 激活版本。
	// Lua 脚本保证：仅当新版本 >= 当前激活版本时才切换 active 指针，
	// 防止并发写入导致版本回滚。
	// 旧快照使用 EXPIRE 宽限期而非立即 DEL，避免 reader 竞态。
	activeKey := schedulerBucketKey(schedulerActivePrefix, bucket)
	readyKey := schedulerBucketKey(schedulerReadyPrefix, bucket)
	snapshotKeyPrefix := fmt.Sprintf("%s%d:%s:%s:v", schedulerSnapshotPrefix, bucket.GroupID, bucket.Platform, bucket.Mode)

	keys := []string{activeKey, readyKey, schedulerBucketSetKey, snapshotKey}
	args := []any{versionStr, bucket.String(), snapshotKeyPrefix, snapshotGraceTTLSeconds}

	activated, err := activateSnapshotScript.Run(ctx, c.rdb, keys, args...).Int()
	if err != nil {
		return err
	}
	if activated == 0 {
		return nil
	}

	_ = c.expireLegacyCandidateIndex(ctx, bucket)
	c.invalidateLocalSnapshot(bucket.String())
	return nil
}

func (c *schedulerCache) clearEmptySnapshot(ctx context.Context, bucket service.SchedulerBucket, versionStr string) error {
	activeKey := schedulerBucketKey(schedulerActivePrefix, bucket)
	readyKey := schedulerBucketKey(schedulerReadyPrefix, bucket)
	snapshotKeyPrefix := fmt.Sprintf("%s%d:%s:%s:v", schedulerSnapshotPrefix, bucket.GroupID, bucket.Platform, bucket.Mode)

	keys := []string{activeKey, readyKey, schedulerBucketSetKey}
	args := []any{versionStr, bucket.String(), snapshotKeyPrefix, snapshotGraceTTLSeconds}

	activated, err := clearEmptySnapshotScript.Run(ctx, c.rdb, keys, args...).Int()
	if err == nil && activated == 1 {
		_ = c.expireLegacyCandidateIndex(ctx, bucket)
		c.invalidateLocalSnapshot(bucket.String())
	}
	return err
}

func (c *schedulerCache) GetCandidateSnapshot(ctx context.Context, bucket service.SchedulerBucket, limit int) ([]*service.Account, bool, error) {
	if c == nil || !c.isCandidateIndexEnabled(bucket) {
		return nil, false, nil
	}
	if limit <= 0 {
		limit = defaultSchedulerCandidateLimit
	}
	if limit > maxSchedulerCandidateLimit {
		limit = maxSchedulerCandidateLimit
	}

	readyVal, err := c.rdb.Get(ctx, schedulerBucketKey(schedulerReadyPrefix, bucket)).Result()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if readyVal != "1" {
		return nil, false, nil
	}

	version, err := c.rdb.Get(ctx, schedulerBucketKey(schedulerActivePrefix, bucket)).Result()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	snapshotKey := schedulerSnapshotKey(bucket, version)
	size, err := c.rdb.ZCard(ctx, snapshotKey).Result()
	if err != nil {
		return nil, false, err
	}
	if size <= minSchedulerCandidateShardSize {
		return nil, false, nil
	}

	ids, err := c.readCandidateIDsFromZSet(ctx, snapshotKey, int(size), limit)
	if err != nil {
		return nil, false, err
	}
	if len(ids) == 0 {
		return nil, false, nil
	}

	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, schedulerAccountMetaKey(id))
	}
	values, err := c.mgetChunked(ctx, keys)
	if err != nil {
		return nil, false, err
	}

	accounts := make([]*service.Account, 0, len(values))
	for _, val := range values {
		if val == nil {
			continue
		}
		account, err := decodeCachedAccount(val)
		if err != nil {
			return nil, false, err
		}
		accounts = append(accounts, account)
	}
	if len(accounts) == 0 {
		return nil, false, nil
	}
	return accounts, true, nil
}

func (c *schedulerCache) GetAccount(ctx context.Context, accountID int64) (*service.Account, error) {
	key := schedulerAccountKey(strconv.FormatInt(accountID, 10))
	val, err := c.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeCachedAccount(val)
}

func (c *schedulerCache) SetAccount(ctx context.Context, account *service.Account) error {
	if account == nil || account.ID <= 0 {
		return nil
	}
	if err := c.writeAccounts(ctx, []service.Account{*account}); err != nil {
		return err
	}
	c.invalidateLocalSnapshotsForAccountChange(account)
	return nil
}

func (c *schedulerCache) DeleteAccount(ctx context.Context, accountID int64) error {
	if accountID <= 0 {
		return nil
	}
	id := strconv.FormatInt(accountID, 10)
	if err := c.rdb.Del(ctx, schedulerAccountKey(id), schedulerAccountMetaKey(id)).Err(); err != nil {
		return err
	}
	c.invalidateLocalSnapshotsForAccount(accountID)
	return nil
}

func (c *schedulerCache) UpdateLastUsed(ctx context.Context, updates map[int64]time.Time) error {
	if len(updates) == 0 {
		return nil
	}

	keys := make([]string, 0, len(updates))
	ids := make([]int64, 0, len(updates))
	for id := range updates {
		keys = append(keys, schedulerAccountKey(strconv.FormatInt(id, 10)))
		ids = append(ids, id)
	}

	values, err := c.mgetChunked(ctx, keys)
	if err != nil {
		return err
	}

	pipe := c.rdb.Pipeline()
	for i, val := range values {
		if val == nil {
			continue
		}
		account, err := decodeCachedAccount(val)
		if err != nil {
			return err
		}
		account.LastUsedAt = ptrTime(updates[ids[i]])
		updated, err := json.Marshal(account)
		if err != nil {
			return err
		}
		metaPayload, err := json.Marshal(buildSchedulerMetadataAccount(*account))
		if err != nil {
			return err
		}
		pipe.Set(ctx, keys[i], updated, 0)
		pipe.Set(ctx, schedulerAccountMetaKey(strconv.FormatInt(ids[i], 10)), metaPayload, 0)
	}
	_, err = pipe.Exec(ctx)
	if err == nil {
		c.updateLocalSnapshotLastUsed(updates)
	}
	return err
}

func (c *schedulerCache) getLocalSnapshot(cacheKey, activeVersion string) ([]*service.Account, bool) {
	if c == nil || c.localTTL <= 0 {
		return nil, false
	}
	now := time.Now()
	c.localMu.RLock()
	entry, ok := c.localSnapshots[cacheKey]
	if !ok || entry.activeVersion != activeVersion || now.After(entry.expiresAt) {
		c.localMu.RUnlock()
		return nil, false
	}
	accounts := cloneAccountPointerSlice(entry.accounts)
	c.localMu.RUnlock()
	return accounts, true
}

func (c *schedulerCache) setLocalSnapshot(cacheKey, activeVersion string, accounts []*service.Account) {
	if c == nil || c.localTTL <= 0 {
		return
	}
	c.localMu.Lock()
	c.removeLocalSnapshotLocked(cacheKey)
	c.localSnapshots[cacheKey] = schedulerLocalSnapshot{
		activeVersion: activeVersion,
		expiresAt:     time.Now().Add(c.localTTL),
		accounts:      cloneAccountPointerSlice(accounts),
	}
	c.indexLocalSnapshotLocked(cacheKey, accounts)
	c.localMu.Unlock()
}

func (c *schedulerCache) invalidateLocalSnapshot(cacheKey string) {
	if c == nil {
		return
	}
	c.localMu.Lock()
	c.removeLocalSnapshotLocked(cacheKey)
	c.localMu.Unlock()
}

func (c *schedulerCache) clearLocalSnapshots() {
	if c == nil {
		return
	}
	c.localMu.Lock()
	c.localSnapshots = make(map[string]schedulerLocalSnapshot)
	c.localBuckets = make(map[int64]map[string]struct{})
	c.localMu.Unlock()
}

func (c *schedulerCache) updateLocalSnapshotLastUsed(updates map[int64]time.Time) {
	if c == nil || len(updates) == 0 {
		return
	}
	c.localMu.Lock()
	affected := make(map[string]struct{})
	for id := range updates {
		for cacheKey := range c.localBuckets[id] {
			affected[cacheKey] = struct{}{}
		}
	}
	for cacheKey := range affected {
		entry, ok := c.localSnapshots[cacheKey]
		if !ok {
			continue
		}
		accounts := cloneAccountPointers(entry.accounts)
		changed := false
		for _, account := range accounts {
			if account == nil {
				continue
			}
			if usedAt, ok := updates[account.ID]; ok {
				account.LastUsedAt = ptrTime(usedAt)
				changed = true
			}
		}
		if changed {
			entry.accounts = accounts
			c.localSnapshots[cacheKey] = entry
		}
	}
	c.localMu.Unlock()
}

func (c *schedulerCache) invalidateLocalSnapshotsForAccount(accountID int64) {
	if c == nil || accountID <= 0 {
		return
	}
	c.localMu.Lock()
	for cacheKey := range c.localBuckets[accountID] {
		c.removeLocalSnapshotLocked(cacheKey)
	}
	delete(c.localBuckets, accountID)
	c.localMu.Unlock()
}

func (c *schedulerCache) invalidateLocalSnapshotsForAccountChange(account *service.Account) {
	if c == nil || account == nil || account.ID <= 0 {
		return
	}
	c.localMu.Lock()
	for cacheKey := range c.localBuckets[account.ID] {
		c.removeLocalSnapshotLocked(cacheKey)
	}
	for _, cacheKey := range candidateBucketKeysForAccount(account) {
		c.removeLocalSnapshotLocked(cacheKey)
	}
	c.localMu.Unlock()
}

func (c *schedulerCache) removeLocalSnapshotLocked(cacheKey string) {
	entry, ok := c.localSnapshots[cacheKey]
	if !ok {
		return
	}
	for _, account := range entry.accounts {
		if account == nil {
			continue
		}
		buckets := c.localBuckets[account.ID]
		delete(buckets, cacheKey)
		if len(buckets) == 0 {
			delete(c.localBuckets, account.ID)
		}
	}
	delete(c.localSnapshots, cacheKey)
}

func (c *schedulerCache) indexLocalSnapshotLocked(cacheKey string, accounts []*service.Account) {
	for _, account := range accounts {
		if account == nil || account.ID <= 0 {
			continue
		}
		buckets := c.localBuckets[account.ID]
		if buckets == nil {
			buckets = make(map[string]struct{})
			c.localBuckets[account.ID] = buckets
		}
		buckets[cacheKey] = struct{}{}
	}
}

func candidateBucketKeysForAccount(account *service.Account) []string {
	groupIDs := make([]int64, 0, len(account.GroupIDs)+1)
	groupIDs = append(groupIDs, 0)
	seenGroups := map[int64]struct{}{0: {}}
	for _, groupID := range account.GroupIDs {
		if groupID <= 0 {
			continue
		}
		if _, ok := seenGroups[groupID]; ok {
			continue
		}
		seenGroups[groupID] = struct{}{}
		groupIDs = append(groupIDs, groupID)
	}

	platforms := []string{account.Platform}
	if account.Platform == service.PlatformAntigravity {
		platforms = append(platforms, service.PlatformAnthropic, service.PlatformGemini)
	}
	modes := []string{service.SchedulerModeSingle, service.SchedulerModeForced, service.SchedulerModeMixed}

	out := make([]string, 0, len(groupIDs)*len(platforms)*len(modes))
	for _, groupID := range groupIDs {
		for _, platform := range platforms {
			if platform == "" {
				continue
			}
			for _, mode := range modes {
				out = append(out, service.SchedulerBucket{GroupID: groupID, Platform: platform, Mode: mode}.String())
			}
		}
	}
	return out
}

func cloneAccountPointerSlice(accounts []*service.Account) []*service.Account {
	if len(accounts) == 0 {
		return []*service.Account{}
	}
	out := make([]*service.Account, len(accounts))
	copy(out, accounts)
	return out
}

func cloneAccountPointers(accounts []*service.Account) []*service.Account {
	if len(accounts) == 0 {
		return []*service.Account{}
	}
	out := make([]*service.Account, 0, len(accounts))
	for _, account := range accounts {
		if account == nil {
			out = append(out, nil)
			continue
		}
		cloned := *account
		out = append(out, &cloned)
	}
	return out
}

func (c *schedulerCache) TryLockBucket(ctx context.Context, bucket service.SchedulerBucket, ttl time.Duration) (bool, error) {
	key := schedulerBucketKey(schedulerLockPrefix, bucket)
	return c.rdb.SetNX(ctx, key, time.Now().UnixNano(), ttl).Result()
}

func (c *schedulerCache) UnlockBucket(ctx context.Context, bucket service.SchedulerBucket) error {
	key := schedulerBucketKey(schedulerLockPrefix, bucket)
	return c.rdb.Del(ctx, key).Err()
}

func (c *schedulerCache) ListBuckets(ctx context.Context) ([]service.SchedulerBucket, error) {
	raw, err := c.rdb.SMembers(ctx, schedulerBucketSetKey).Result()
	if err != nil {
		return nil, err
	}
	out := make([]service.SchedulerBucket, 0, len(raw))
	for _, entry := range raw {
		bucket, ok := service.ParseSchedulerBucket(entry)
		if !ok {
			continue
		}
		out = append(out, bucket)
	}
	return out, nil
}

func (c *schedulerCache) GetOutboxWatermark(ctx context.Context) (int64, error) {
	val, err := c.rdb.Get(ctx, schedulerOutboxWatermarkKey).Result()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	id, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (c *schedulerCache) SetOutboxWatermark(ctx context.Context, id int64) error {
	return c.rdb.Set(ctx, schedulerOutboxWatermarkKey, strconv.FormatInt(id, 10), 0).Err()
}

func (c *schedulerCache) isCandidateIndexEnabled(bucket service.SchedulerBucket) bool {
	if c == nil || len(c.indexedBuckets) == 0 {
		return false
	}
	_, ok := c.indexedBuckets[bucket.String()]
	return ok
}

func (c *schedulerCache) readCandidateIDsFromZSet(ctx context.Context, key string, size int, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	if size <= limit {
		return c.rdb.ZRange(ctx, key, 0, int64(limit-1)).Result()
	}
	start := rand.Intn(size - limit + 1)
	stop := start + limit - 1
	return c.rdb.ZRange(ctx, key, int64(start), int64(stop)).Result()
}

func (c *schedulerCache) expireLegacyCandidateIndex(ctx context.Context, bucket service.SchedulerBucket) error {
	if c == nil || !c.isCandidateIndexEnabled(bucket) {
		return nil
	}

	activeKey := schedulerBucketKey(schedulerCandidateActivePrefix, bucket)
	readyKey := schedulerBucketKey(schedulerCandidateReadyPrefix, bucket)
	version, err := c.rdb.Get(ctx, activeKey).Result()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return err
	}
	if version == "" {
		return nil
	}

	shards := 1
	metaKey := schedulerCandidateMetaKey(bucket, version)
	if raw, err := c.rdb.HGet(ctx, metaKey, "shards").Result(); err == nil {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 {
			shards = parsed
		}
	} else if err != redis.Nil {
		return err
	}

	pipe := c.rdb.Pipeline()
	pipe.Del(ctx, activeKey, readyKey)
	pipe.Expire(ctx, metaKey, snapshotGraceTTLSeconds*time.Second)
	if shards <= 1 {
		pipe.Expire(ctx, schedulerCandidateIndexBaseKey(bucket, version), snapshotGraceTTLSeconds*time.Second)
	} else {
		for shard := 0; shard < shards; shard++ {
			pipe.Expire(ctx, schedulerCandidateShardKey(bucket, version, shard), snapshotGraceTTLSeconds*time.Second)
		}
	}
	_, err = pipe.Exec(ctx)
	return err
}

func schedulerCandidateIndexBaseKey(bucket service.SchedulerBucket, version string) string {
	return fmt.Sprintf("%s%d:%s:%s:v%s", schedulerCandidateIndexPrefix, bucket.GroupID, bucket.Platform, bucket.Mode, version)
}

func schedulerCandidateShardKey(bucket service.SchedulerBucket, version string, shard int) string {
	return fmt.Sprintf("%s:s%d", schedulerCandidateIndexBaseKey(bucket, version), shard)
}

func schedulerCandidateMetaKey(bucket service.SchedulerBucket, version string) string {
	return fmt.Sprintf("%s%d:%s:%s:v%s", schedulerCandidateMetaPrefix, bucket.GroupID, bucket.Platform, bucket.Mode, version)
}

func schedulerBucketKey(prefix string, bucket service.SchedulerBucket) string {
	return fmt.Sprintf("%s%d:%s:%s", prefix, bucket.GroupID, bucket.Platform, bucket.Mode)
}

func schedulerSnapshotKey(bucket service.SchedulerBucket, version string) string {
	return fmt.Sprintf("%s%d:%s:%s:v%s", schedulerSnapshotPrefix, bucket.GroupID, bucket.Platform, bucket.Mode, version)
}

func schedulerAccountKey(id string) string {
	return schedulerAccountPrefix + id
}

func schedulerAccountMetaKey(id string) string {
	return schedulerAccountMetaPrefix + id
}

func ptrTime(t time.Time) *time.Time {
	return &t
}

func decodeCachedAccount(val any) (*service.Account, error) {
	var payload []byte
	switch raw := val.(type) {
	case string:
		payload = []byte(raw)
	case []byte:
		payload = raw
	default:
		return nil, fmt.Errorf("unexpected account cache type: %T", val)
	}
	var account service.Account
	if err := json.Unmarshal(payload, &account); err != nil {
		return nil, err
	}
	return &account, nil
}

func (c *schedulerCache) writeAccounts(ctx context.Context, accounts []service.Account) error {
	if len(accounts) == 0 {
		return nil
	}

	pipe := c.rdb.Pipeline()
	pending := 0
	flush := func() error {
		if pending == 0 {
			return nil
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
		pipe = c.rdb.Pipeline()
		pending = 0
		return nil
	}

	for _, account := range accounts {
		fullPayload, err := json.Marshal(account)
		if err != nil {
			return err
		}
		metaPayload, err := json.Marshal(buildSchedulerMetadataAccount(account))
		if err != nil {
			return err
		}

		id := strconv.FormatInt(account.ID, 10)
		pipe.Set(ctx, schedulerAccountKey(id), fullPayload, 0)
		pipe.Set(ctx, schedulerAccountMetaKey(id), metaPayload, 0)
		pending++
		if pending >= c.writeChunkSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}

	return flush()
}

func (c *schedulerCache) mgetChunked(ctx context.Context, keys []string) ([]any, error) {
	if len(keys) == 0 {
		return []any{}, nil
	}

	out := make([]any, 0, len(keys))
	chunkSize := c.mgetChunkSize
	if chunkSize <= 0 {
		chunkSize = defaultSchedulerSnapshotMGetChunkSize
	}
	for start := 0; start < len(keys); start += chunkSize {
		end := start + chunkSize
		if end > len(keys) {
			end = len(keys)
		}
		part, err := c.rdb.MGet(ctx, keys[start:end]...).Result()
		if err != nil {
			return nil, err
		}
		out = append(out, part...)
	}
	return out, nil
}

func buildSchedulerMetadataAccount(account service.Account) service.Account {
	return service.Account{
		ID:                      account.ID,
		Name:                    account.Name,
		Platform:                account.Platform,
		Type:                    account.Type,
		Concurrency:             account.Concurrency,
		LoadFactor:              account.LoadFactor,
		Priority:                account.Priority,
		RateMultiplier:          account.RateMultiplier,
		Status:                  account.Status,
		LastUsedAt:              account.LastUsedAt,
		ExpiresAt:               account.ExpiresAt,
		AutoPauseOnExpired:      account.AutoPauseOnExpired,
		Schedulable:             account.Schedulable,
		RateLimitedAt:           account.RateLimitedAt,
		RateLimitResetAt:        account.RateLimitResetAt,
		OverloadUntil:           account.OverloadUntil,
		TempUnschedulableUntil:  account.TempUnschedulableUntil,
		TempUnschedulableReason: account.TempUnschedulableReason,
		SessionWindowStart:      account.SessionWindowStart,
		SessionWindowEnd:        account.SessionWindowEnd,
		SessionWindowStatus:     account.SessionWindowStatus,
		AccountGroups:           filterSchedulerAccountGroups(account.AccountGroups),
		GroupIDs:                filterSchedulerGroupIDs(account.GroupIDs, account.AccountGroups),
		Credentials:             filterSchedulerCredentials(account.Credentials),
		Extra:                   filterSchedulerExtra(account.Extra),
	}
}

func filterSchedulerAccountGroups(accountGroups []service.AccountGroup) []service.AccountGroup {
	if len(accountGroups) == 0 {
		return nil
	}

	filtered := make([]service.AccountGroup, 0, len(accountGroups))
	for _, ag := range accountGroups {
		if ag.GroupID <= 0 {
			continue
		}
		filtered = append(filtered, service.AccountGroup{
			AccountID: ag.AccountID,
			GroupID:   ag.GroupID,
			Priority:  ag.Priority,
			CreatedAt: ag.CreatedAt,
		})
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func filterSchedulerGroupIDs(groupIDs []int64, accountGroups []service.AccountGroup) []int64 {
	if len(groupIDs) == 0 && len(accountGroups) == 0 {
		return nil
	}

	seen := make(map[int64]struct{}, len(groupIDs)+len(accountGroups))
	filtered := make([]int64, 0, len(groupIDs)+len(accountGroups))
	for _, id := range groupIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		filtered = append(filtered, id)
	}
	for _, ag := range accountGroups {
		if ag.GroupID <= 0 {
			continue
		}
		if _, ok := seen[ag.GroupID]; ok {
			continue
		}
		seen[ag.GroupID] = struct{}{}
		filtered = append(filtered, ag.GroupID)
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func filterSchedulerCredentials(credentials map[string]any) map[string]any {
	if len(credentials) == 0 {
		return nil
	}
	keys := []string{"model_mapping", "api_key", "project_id", "oauth_type"}
	filtered := make(map[string]any)
	for _, key := range keys {
		if value, ok := credentials[key]; ok && value != nil {
			filtered[key] = value
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func filterSchedulerExtra(extra map[string]any) map[string]any {
	if len(extra) == 0 {
		return nil
	}
	keys := []string{
		"mixed_scheduling",
		"window_cost_limit",
		"window_cost_sticky_reserve",
		"max_sessions",
		"session_idle_timeout_minutes",
		"openai_oauth_responses_websockets_v2_enabled",
		"openai_oauth_responses_websockets_v2_mode",
		"openai_apikey_responses_websockets_v2_enabled",
		"openai_apikey_responses_websockets_v2_mode",
		"responses_websockets_v2_enabled",
		"openai_ws_enabled",
		"openai_ws_force_http",
		"openai_responses_mode",
		"openai_responses_supported",
		"codex_5h_used_percent",
		"codex_5h_reset_at",
		"codex_5h_reset_after_seconds",
		"codex_5h_limit_percent",
		"codex_7d_used_percent",
		"codex_7d_reset_at",
		"codex_7d_reset_after_seconds",
		"codex_7d_limit_percent",
	}
	filtered := make(map[string]any)
	for _, key := range keys {
		if value, ok := extra[key]; ok && value != nil {
			filtered[key] = value
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}
