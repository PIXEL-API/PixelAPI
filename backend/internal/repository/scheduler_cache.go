package repository

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
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
	schedulerEpochPrefix        = "sched:epoch:"
	schedulerRetiredPrefix      = "sched:retired:"
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

	// schedulerEmptySnapshotSentinel 空快照哨兵成员。空 bucket 写入该成员而非
	// 跳过 ZSET，读侧据此把"真空 bucket"识别为缓存命中（返回空账号列表），
	// 避免每个请求都回退数据库。哨兵不是合法账号 ID，读侧一律过滤。
	// 旧二进制读到哨兵成员会因 meta MGet 为 nil 而按 miss 回退 DB，与旧行为一致。
	schedulerEmptySnapshotSentinel = "__empty__"

	schedulerGroupLifecycleLockPrefix      = "sched:group:lifecycle-lock:"
	schedulerGroupLifecycleOwnerTokenBytes = 16
)

var (
	captureBucketWriteTokenScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[2]) == 1 then
    return -1
end

local currentEpoch = redis.call('GET', KEYS[1])
if currentEpoch == false then
    redis.call('SET', KEYS[1], '1')
    return 1
end

local parsedEpoch = tonumber(currentEpoch)
if parsedEpoch == nil or parsedEpoch < 1 then
    return -2
end
return parsedEpoch
`)

	allocateSnapshotVersionScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[2]) == 1 then
    return -1
end

local currentEpoch = tonumber(redis.call('GET', KEYS[1]))
local expectedEpoch = tonumber(ARGV[1])
if currentEpoch == nil or expectedEpoch == nil or currentEpoch ~= expectedEpoch then
    return -2
end

return redis.call('INCR', KEYS[3])
`)

	retireBucketScript = redis.NewScript(`
local retired = redis.call('GET', KEYS[2])
local currentEpoch = tonumber(redis.call('GET', KEYS[1])) or 0

if retired == false then
    currentEpoch = currentEpoch + 1
    if currentEpoch < 1 then
        currentEpoch = 1
    end
    redis.call('SET', KEYS[1], tostring(currentEpoch))
    redis.call('SET', KEYS[2], tostring(currentEpoch))
elseif currentEpoch < 1 then
    currentEpoch = tonumber(retired) or 1
    redis.call('SET', KEYS[1], tostring(currentEpoch))
end

redis.call('SREM', KEYS[3], ARGV[1])
local currentActive = redis.call('GET', KEYS[5])
if currentActive ~= false then
    redis.call('EXPIRE', ARGV[2] .. currentActive, tonumber(ARGV[3]))
end
redis.call('DEL', KEYS[4], KEYS[5])
return currentEpoch
`)

	reopenBucketScript = redis.NewScript(`
local currentEpochRaw = redis.call('GET', KEYS[1])
local currentEpoch = tonumber(currentEpochRaw)
local retiredEpochRaw = redis.call('GET', KEYS[2])

if retiredEpochRaw == false then
    if currentEpochRaw == false then
        redis.call('SET', KEYS[1], '1')
        return 1
    end
    if currentEpoch == nil or currentEpoch < 1 then
        return -2
    end
    return currentEpoch
end

local retiredEpoch = tonumber(retiredEpochRaw)
if retiredEpoch == nil or retiredEpoch < 1 then
    return -2
end
if currentEpoch == nil or currentEpoch < retiredEpoch then
    currentEpoch = retiredEpoch
end

redis.call('SET', KEYS[1], tostring(currentEpoch))
redis.call('DEL', KEYS[2])
redis.call('SREM', KEYS[3], ARGV[1])
local currentActive = redis.call('GET', KEYS[5])
if currentActive ~= false then
    redis.call('EXPIRE', ARGV[2] .. currentActive, tonumber(ARGV[3]))
end
redis.call('DEL', KEYS[4], KEYS[5])
return currentEpoch
`)

	releaseGroupLifecycleLeaseScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
    return redis.call('DEL', KEYS[1])
end
return 0
`)

	// activateSnapshotScript 原子 CAS 切换快照版本。
	// 仅当新版本号 >= 当前激活版本时才切换，防止并发写入导致版本回滚。
	// 旧快照使用 EXPIRE 设置宽限期而非立即 DEL，避免与 reader 竞态。
	//
	// KEYS[1] = activeKey     (sched:active:{bucket})
	// KEYS[2] = readyKey      (sched:ready:{bucket})
	// KEYS[3] = bucketSetKey  (sched:buckets)
	// KEYS[4] = snapshotKey   (新写入的快照 key)
	// KEYS[5] = epochKey
	// KEYS[6] = retiredKey
	// ARGV[1] = 新版本号字符串
	// ARGV[2] = bucket 字符串 (用于 SADD)
	// ARGV[3] = 快照 key 前缀 (用于构造旧快照 key)
	// ARGV[4] = 宽限期 TTL 秒数
	// ARGV[5] = writer epoch
	//
	// 返回 1 = 已激活, 0 = 版本过旧未激活
	activateSnapshotScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[6]) == 1 then
	redis.call('DEL', KEYS[4])
	return -1
end

local currentEpoch = tonumber(redis.call('GET', KEYS[5]))
local expectedEpoch = tonumber(ARGV[5])
if currentEpoch == nil or expectedEpoch == nil or currentEpoch ~= expectedEpoch then
	redis.call('DEL', KEYS[4])
	return -2
end

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
	ids, hasSentinel := filterEmptySnapshotSentinel(ids)
	if len(ids) == 0 {
		if hasSentinel {
			// 带哨兵的空快照是确定性的"真空 bucket"，按命中返回空列表
			accounts := []*service.Account{}
			c.setLocalSnapshot(cacheKey, activeVal, accounts)
			return accounts, true, nil
		}
		// 无哨兵的空快照视为缓存未命中，触发数据库回退查询
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

func (c *schedulerCache) CaptureBucketWriteToken(ctx context.Context, bucket service.SchedulerBucket) (service.SchedulerBucketWriteToken, error) {
	result, err := captureBucketWriteTokenScript.Run(ctx, c.rdb, []string{
		schedulerBucketKey(schedulerEpochPrefix, bucket),
		schedulerBucketKey(schedulerRetiredPrefix, bucket),
	}).Int64()
	if err != nil {
		return service.SchedulerBucketWriteToken{}, err
	}
	if err := schedulerBucketWriteResultError(result, bucket); err != nil {
		return service.SchedulerBucketWriteToken{}, err
	}
	return service.SchedulerBucketWriteToken{Bucket: bucket, Epoch: result}, nil
}

func (c *schedulerCache) RetireBucket(ctx context.Context, bucket service.SchedulerBucket) error {
	snapshotKeyPrefix := fmt.Sprintf("%s%d:%s:%s:v", schedulerSnapshotPrefix, bucket.GroupID, bucket.Platform, bucket.Mode)
	result, err := retireBucketScript.Run(ctx, c.rdb, []string{
		schedulerBucketKey(schedulerEpochPrefix, bucket),
		schedulerBucketKey(schedulerRetiredPrefix, bucket),
		schedulerBucketSetKey,
		schedulerBucketKey(schedulerReadyPrefix, bucket),
		schedulerBucketKey(schedulerActivePrefix, bucket),
	}, bucket.String(), snapshotKeyPrefix, snapshotGraceTTLSeconds).Int64()
	if err != nil {
		return err
	}
	if result < 1 {
		return fmt.Errorf("retire scheduler bucket %s returned invalid epoch %d", bucket.String(), result)
	}
	c.invalidateLocalSnapshot(bucket.String())
	return nil
}

func (c *schedulerCache) ReopenBucket(ctx context.Context, bucket service.SchedulerBucket) (service.SchedulerBucketWriteToken, error) {
	snapshotKeyPrefix := fmt.Sprintf("%s%d:%s:%s:v", schedulerSnapshotPrefix, bucket.GroupID, bucket.Platform, bucket.Mode)
	result, err := reopenBucketScript.Run(ctx, c.rdb, []string{
		schedulerBucketKey(schedulerEpochPrefix, bucket),
		schedulerBucketKey(schedulerRetiredPrefix, bucket),
		schedulerBucketSetKey,
		schedulerBucketKey(schedulerReadyPrefix, bucket),
		schedulerBucketKey(schedulerActivePrefix, bucket),
	}, bucket.String(), snapshotKeyPrefix, snapshotGraceTTLSeconds).Int64()
	if err != nil {
		return service.SchedulerBucketWriteToken{}, err
	}
	if err := schedulerBucketWriteResultError(result, bucket); err != nil {
		return service.SchedulerBucketWriteToken{}, err
	}
	c.invalidateLocalSnapshot(bucket.String())
	return service.SchedulerBucketWriteToken{Bucket: bucket, Epoch: result}, nil
}

func (c *schedulerCache) TryAcquireGroupLifecycleLease(ctx context.Context, groupID int64, ttl time.Duration) (service.SchedulerGroupLifecycleLease, bool, error) {
	if groupID <= 0 || ttl <= 0 {
		return service.SchedulerGroupLifecycleLease{}, false, service.ErrSchedulerGroupLifecycleLeaseInvalid
	}
	ownerToken, err := newSchedulerGroupLifecycleOwnerToken()
	if err != nil {
		return service.SchedulerGroupLifecycleLease{}, false, err
	}
	acquired, err := c.rdb.SetNX(ctx, schedulerGroupLifecycleLockKey(groupID), ownerToken, ttl).Result()
	if err != nil || !acquired {
		return service.SchedulerGroupLifecycleLease{}, acquired, err
	}
	return service.SchedulerGroupLifecycleLease{GroupID: groupID, OwnerToken: ownerToken}, true, nil
}

func (c *schedulerCache) ReleaseGroupLifecycleLease(ctx context.Context, lease service.SchedulerGroupLifecycleLease) error {
	if !lease.ValidFor(lease.GroupID) {
		return service.ErrSchedulerGroupLifecycleLeaseInvalid
	}
	result, err := releaseGroupLifecycleLeaseScript.Run(ctx, c.rdb, []string{schedulerGroupLifecycleLockKey(lease.GroupID)}, lease.OwnerToken).Int64()
	if err != nil {
		return err
	}
	if result != 1 {
		return fmt.Errorf("%w: group=%d", service.ErrSchedulerGroupLifecycleLeaseLost, lease.GroupID)
	}
	return nil
}

func newSchedulerGroupLifecycleOwnerToken() (string, error) {
	raw := make([]byte, schedulerGroupLifecycleOwnerTokenBytes)
	if _, err := cryptorand.Read(raw); err != nil {
		return "", fmt.Errorf("generate scheduler lifecycle owner token: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// SetSnapshot preserves the legacy interface while still using fenced writes.
func (c *schedulerCache) SetSnapshot(ctx context.Context, bucket service.SchedulerBucket, accounts []service.Account) error {
	token, err := c.CaptureBucketWriteToken(ctx, bucket)
	if err != nil {
		return err
	}
	return c.SetSnapshotFenced(ctx, bucket, token, accounts)
}

func (c *schedulerCache) SetSnapshotFenced(ctx context.Context, bucket service.SchedulerBucket, token service.SchedulerBucketWriteToken, accounts []service.Account) error {
	if !token.ValidFor(bucket) {
		return fmt.Errorf("%w: bucket=%s", service.ErrSchedulerBucketWriteFenced, bucket.String())
	}
	version, err := c.allocateSnapshotVersion(ctx, bucket, token)
	if err != nil {
		return err
	}
	if err := c.writeSnapshotVersion(ctx, bucket, version, accounts); err != nil {
		return err
	}
	return c.activateSnapshotVersion(ctx, bucket, token, version)
}

func (c *schedulerCache) allocateSnapshotVersion(ctx context.Context, bucket service.SchedulerBucket, token service.SchedulerBucketWriteToken) (string, error) {
	result, err := allocateSnapshotVersionScript.Run(ctx, c.rdb, []string{
		schedulerBucketKey(schedulerEpochPrefix, bucket),
		schedulerBucketKey(schedulerRetiredPrefix, bucket),
		schedulerBucketKey(schedulerVersionPrefix, bucket),
	}, token.Epoch).Int64()
	if err != nil {
		return "", err
	}
	if err := schedulerBucketWriteResultError(result, bucket); err != nil {
		return "", err
	}
	return strconv.FormatInt(result, 10), nil
}

func (c *schedulerCache) writeSnapshotVersion(ctx context.Context, bucket service.SchedulerBucket, version string, accounts []service.Account) error {
	cacheableAccounts, err := c.writeAccounts(ctx, accounts)
	if err != nil {
		return err
	}
	if len(cacheableAccounts) == 0 {
		// 空集也要落一个哨兵成员，否则激活后的空版本 ZRange 为空、被读侧当作 miss
		return c.rdb.ZAdd(ctx, schedulerSnapshotKey(bucket, version), redis.Z{
			Score:  0,
			Member: schedulerEmptySnapshotSentinel,
		}).Err()
	}
	members := make([]redis.Z, 0, len(cacheableAccounts))
	for idx, account := range cacheableAccounts {
		members = append(members, redis.Z{Score: float64(idx), Member: strconv.FormatInt(account.ID, 10)})
	}
	pipe := c.rdb.Pipeline()
	for start := 0; start < len(members); start += c.writeChunkSize {
		end := min(start+c.writeChunkSize, len(members))
		pipe.ZAdd(ctx, schedulerSnapshotKey(bucket, version), members[start:end]...)
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (c *schedulerCache) activateSnapshotVersion(ctx context.Context, bucket service.SchedulerBucket, token service.SchedulerBucketWriteToken, version string) error {
	snapshotKey := schedulerSnapshotKey(bucket, version)
	snapshotKeyPrefix := fmt.Sprintf("%s%d:%s:%s:v", schedulerSnapshotPrefix, bucket.GroupID, bucket.Platform, bucket.Mode)
	result, err := activateSnapshotScript.Run(ctx, c.rdb, []string{
		schedulerBucketKey(schedulerActivePrefix, bucket),
		schedulerBucketKey(schedulerReadyPrefix, bucket),
		schedulerBucketSetKey,
		snapshotKey,
		schedulerBucketKey(schedulerEpochPrefix, bucket),
		schedulerBucketKey(schedulerRetiredPrefix, bucket),
	}, version, bucket.String(), snapshotKeyPrefix, snapshotGraceTTLSeconds, token.Epoch).Int64()
	if err != nil {
		return err
	}
	if err := schedulerBucketWriteResultError(result, bucket); err != nil {
		return err
	}
	if result == 1 {
		_ = c.expireLegacyCandidateIndex(ctx, bucket)
		c.invalidateLocalSnapshot(bucket.String())
	}
	return nil
}

// filterEmptySnapshotSentinel 从快照成员中剔除空快照哨兵，返回剩余成员与是否含哨兵。
func filterEmptySnapshotSentinel(ids []string) ([]string, bool) {
	hasSentinel := false
	for _, id := range ids {
		if id == schedulerEmptySnapshotSentinel {
			hasSentinel = true
			break
		}
	}
	if !hasSentinel {
		return ids, false
	}
	filtered := make([]string, 0, len(ids)-1)
	for _, id := range ids {
		if id == schedulerEmptySnapshotSentinel {
			continue
		}
		filtered = append(filtered, id)
	}
	return filtered, true
}

func schedulerBucketWriteResultError(result int64, bucket service.SchedulerBucket) error {
	switch result {
	case -1:
		return fmt.Errorf("%w: bucket=%s", service.ErrSchedulerBucketRetired, bucket.String())
	case -2:
		return fmt.Errorf("%w: bucket=%s", service.ErrSchedulerBucketWriteFenced, bucket.String())
	default:
		return nil
	}
}

func (c *schedulerCache) GetCandidateSnapshot(ctx context.Context, bucket service.SchedulerBucket, limit, threshold int, globalEnabled bool) ([]*service.Account, bool, error) {
	if c == nil || (!globalEnabled && !c.isCandidateIndexEnabled(bucket)) {
		return nil, false, nil
	}
	if limit <= 0 {
		limit = defaultSchedulerCandidateLimit
	}
	if limit > maxSchedulerCandidateLimit {
		limit = maxSchedulerCandidateLimit
	}
	minSize := int64(threshold)
	if minSize <= 0 {
		minSize = minSchedulerCandidateShardSize
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
	if size <= minSize {
		return nil, false, nil
	}

	ids, err := c.readCandidateIDsFromZSet(ctx, snapshotKey, int(size), limit)
	if err != nil {
		return nil, false, err
	}
	ids, _ = filterEmptySnapshotSentinel(ids)
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
	cacheable, err := c.writeAccounts(ctx, []service.Account{*account})
	if err != nil {
		return err
	}
	if len(cacheable) == 0 {
		return c.DeleteAccount(ctx, account.ID)
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
	invalidAccountIDs := make([]int64, 0)
	for i, val := range values {
		if val == nil {
			continue
		}
		account, err := decodeCachedAccount(val)
		if err != nil {
			return err
		}
		account.LastUsedAt = ptrTime(updates[ids[i]])
		updated, metaPayload, err := marshalSchedulerCacheAccount(*account)
		if err != nil {
			slog.Warn("scheduler cache removes account with unencodable payload",
				"account_id", ids[i],
				"error", err,
			)
			pipe.Del(ctx, keys[i], schedulerAccountMetaKey(strconv.FormatInt(ids[i], 10)))
			invalidAccountIDs = append(invalidAccountIDs, ids[i])
			continue
		}
		pipe.Set(ctx, keys[i], updated, 0)
		pipe.Set(ctx, schedulerAccountMetaKey(strconv.FormatInt(ids[i], 10)), metaPayload, 0)
	}
	_, err = pipe.Exec(ctx)
	if err == nil {
		c.updateLocalSnapshotLastUsed(updates)
		for _, accountID := range invalidAccountIDs {
			c.invalidateLocalSnapshotsForAccount(accountID)
		}
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

func schedulerGroupLifecycleLockKey(groupID int64) string {
	return schedulerGroupLifecycleLockPrefix + strconv.FormatInt(groupID, 10)
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

func (c *schedulerCache) writeAccounts(ctx context.Context, accounts []service.Account) ([]service.Account, error) {
	if len(accounts) == 0 {
		return nil, nil
	}

	pipe := c.rdb.Pipeline()
	cacheableAccounts := make([]service.Account, 0, len(accounts))
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
		fullPayload, metaPayload, err := marshalSchedulerCacheAccount(account)
		if err != nil {
			slog.Warn("scheduler cache skips account with unencodable payload",
				"account_id", account.ID,
				"error", err,
			)
			continue
		}

		id := strconv.FormatInt(account.ID, 10)
		pipe.Set(ctx, schedulerAccountKey(id), fullPayload, 0)
		pipe.Set(ctx, schedulerAccountMetaKey(id), metaPayload, 0)
		cacheableAccounts = append(cacheableAccounts, account)
		pending++
		if pending >= c.writeChunkSize {
			if err := flush(); err != nil {
				return nil, err
			}
		}
	}

	if err := flush(); err != nil {
		return nil, err
	}
	return cacheableAccounts, nil
}

func marshalSchedulerCacheAccount(account service.Account) ([]byte, []byte, error) {
	fullPayload, err := json.Marshal(account)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal account: %w", err)
	}
	metaPayload, err := json.Marshal(buildSchedulerMetadataAccount(account))
	if err != nil {
		return nil, nil, fmt.Errorf("marshal account metadata: %w", err)
	}
	return fullPayload, metaPayload, nil
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
		OwnerUserID:             account.OwnerUserID,
		ShareMode:               account.ShareMode,
		ShareStatus:             account.ShareStatus,
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
		service.GrokMediaEligibleExtraKey,
		"grok_billing_snapshot",
		// 配额元数据：Account.IsQuotaExceededAt 在选号时会读这些键。
		// 若在快照里剥掉，从 Redis 命中路径读回的账号 Extra 无配额字段，
		// IsQuotaExceededAt 恒为 false，配额已耗尽的账号仍会被选中（资损）。
		// *_reset_mode / quota_reset_timezone 也必须带上：
		// GetQuotaDailyResetMode / GetQuotaWeeklyResetMode / 固定周期过期判定会读。
		"quota_limit",
		"quota_used",
		"quota_daily_limit",
		"quota_daily_used",
		"quota_daily_start",
		"quota_daily_reset_mode",
		"quota_daily_reset_hour",
		"quota_weekly_limit",
		"quota_weekly_used",
		"quota_weekly_start",
		"quota_weekly_reset_mode",
		"quota_weekly_reset_day",
		"quota_weekly_reset_hour",
		"quota_reset_timezone",
		// 剥掉后 openAIQuotaHeadroomSnapshotStale 恒为 true，
		// headroom 调度权重在热路径上被永久钉死为中性值。
		"codex_usage_updated_at",
		// isModelRateLimitedWithContext 依赖该键判定账号是否被按模型限流。
		"model_rate_limits",
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
