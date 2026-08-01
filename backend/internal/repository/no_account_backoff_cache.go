package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

// "无可用账号"退避限流 Redis 实现。
//
// 设计说明：
//   - key 形式：noacct:{u<uid>:g<gid>}:cnt（窗口计数）、noacct:{u<uid>:g<gid>}:block（退避标记）。
//     大括号为 Redis Cluster hash tag，保证同一 (user, group) 的两个 key 落在同一 slot，
//     Lua 脚本才能原子操作。groupID 为 nil 时记 0。
//   - RecordFailure 用 Lua 原子完成 INCR+PEXPIRE（含 TTL 丢失修复）+阈值判定；
//     跨过阈值时写 block 键并删除计数键，避免退避结束后旧计数立刻再次触发。
//   - 所有 Redis 调用统一 150ms 超时，出错 fail-open（视为未限流），不阻断正常请求。
const noAccountBackoffOpTimeout = 150 * time.Millisecond

// noAccountBackoffRecordScript 原子记录失败并判定是否进入退避。
// KEYS[1]=cnt KEYS[2]=block ARGV[1]=窗口ms ARGV[2]=阈值 ARGV[3]=退避ms
// 返回 {count, blocked(0/1), blockTTLms}
var noAccountBackoffRecordScript = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
local ttl = redis.call('PTTL', KEYS[1])
if n == 1 or ttl == -1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
if n >= tonumber(ARGV[2]) then
  redis.call('SET', KEYS[2], '1', 'PX', ARGV[3])
  redis.call('DEL', KEYS[1])
  return {n, 1, tonumber(ARGV[3])}
end
return {n, 0, 0}
`)

type noAccountBackoffCacheImpl struct {
	rdb *redis.Client
	cfg config.NoAccountBackoffConfig
}

// NewNoAccountBackoffCache 创建"无可用账号"退避限流器。
func NewNoAccountBackoffCache(rdb *redis.Client, cfg *config.Config) service.NoAccountBackoffLimiter {
	var backoffCfg config.NoAccountBackoffConfig
	if cfg != nil {
		backoffCfg = cfg.RateLimit.NoAccountBackoff
	}
	// 防御非法配置：参数缺失或被改坏时退回默认值，避免 0 窗口/0 阈值导致误封。
	if backoffCfg.WindowSeconds <= 0 {
		backoffCfg.WindowSeconds = 60
	}
	if backoffCfg.Threshold <= 0 {
		backoffCfg.Threshold = 30
	}
	if backoffCfg.BackoffSeconds <= 0 {
		backoffCfg.BackoffSeconds = 60
	}
	return &noAccountBackoffCacheImpl{rdb: rdb, cfg: backoffCfg}
}

// noAccountBackoffKeys 生成 (user, group) 对应的计数键与退避标记键。
func noAccountBackoffKeys(userID int64, groupID *int64) (cntKey, blockKey string) {
	gid := int64(0)
	if groupID != nil {
		gid = *groupID
	}
	tag := fmt.Sprintf("{u%d:g%d}", userID, gid)
	return "noacct:" + tag + ":cnt", "noacct:" + tag + ":block"
}

// noAccountBackoffCeilSeconds 将剩余 TTL 换算为向上取整的秒数（至少 1，用于 Retry-After）。
func noAccountBackoffCeilSeconds(d time.Duration) int {
	secs := int((d + time.Second - 1) / time.Second)
	if secs < 1 {
		secs = 1
	}
	return secs
}

// noAccountBackoffInt64 解析 Lua 返回值中的整数（go-redis 对 Lua number 统一返回 int64）。
func noAccountBackoffInt64(v any) int64 {
	if n, ok := v.(int64); ok {
		return n
	}
	return 0
}

// CheckBlocked 查询退避标记剩余 TTL；>0 视为处于退避期。
func (c *noAccountBackoffCacheImpl) CheckBlocked(ctx context.Context, userID int64, groupID *int64) (bool, int) {
	_, blockKey := noAccountBackoffKeys(userID, groupID)
	opCtx, cancel := context.WithTimeout(ctx, noAccountBackoffOpTimeout)
	defer cancel()
	ttl, err := c.rdb.PTTL(opCtx, blockKey).Result()
	if err != nil {
		logger.LegacyPrintf("repository.no_account_backoff", "[WARN] check blocked failed (fail-open): user=%d err=%v", userID, err)
		return false, 0
	}
	// -2=键不存在，-1=无过期（异常残留，宁可放行也不永久封禁）
	if ttl <= 0 {
		return false, 0
	}
	return true, noAccountBackoffCeilSeconds(ttl)
}

// RecordFailure 记录一次失败；跨过阈值的那次调用返回 blocked=true。
func (c *noAccountBackoffCacheImpl) RecordFailure(ctx context.Context, userID int64, groupID *int64) (bool, int) {
	cntKey, blockKey := noAccountBackoffKeys(userID, groupID)
	opCtx, cancel := context.WithTimeout(ctx, noAccountBackoffOpTimeout)
	defer cancel()
	values, err := noAccountBackoffRecordScript.Run(
		opCtx, c.rdb,
		[]string{cntKey, blockKey},
		c.cfg.WindowSeconds*1000, c.cfg.Threshold, c.cfg.BackoffSeconds*1000,
	).Slice()
	if err != nil {
		logger.LegacyPrintf("repository.no_account_backoff", "[WARN] record failure failed (fail-open): user=%d err=%v", userID, err)
		return false, 0
	}
	if len(values) < 3 {
		logger.LegacyPrintf("repository.no_account_backoff", "[WARN] record failure script returned %d values (fail-open): user=%d", len(values), userID)
		return false, 0
	}
	if noAccountBackoffInt64(values[1]) != 1 {
		return false, 0
	}
	return true, noAccountBackoffCeilSeconds(time.Duration(noAccountBackoffInt64(values[2])) * time.Millisecond)
}
