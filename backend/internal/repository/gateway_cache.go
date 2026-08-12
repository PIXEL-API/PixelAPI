package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const stickySessionPrefix = "sticky_session:"

type gatewayCache struct {
	rdb *redis.Client
}

func NewGatewayCache(rdb *redis.Client) service.GatewayCache {
	return &gatewayCache{rdb: rdb}
}

// buildSessionKey 构建 session key，包含 groupID 实现分组隔离
// 格式: sticky_session:{groupID}:{sessionHash}
func buildSessionKey(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%s%d:%s", stickySessionPrefix, groupID, sessionHash)
}

func (c *gatewayCache) GetSessionAccountID(ctx context.Context, groupID int64, sessionHash string) (int64, error) {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Get(ctx, key).Int64()
}

func (c *gatewayCache) SetSessionAccountID(ctx context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Set(ctx, key, accountID, ttl).Err()
}

func (c *gatewayCache) RefreshSessionTTL(ctx context.Context, groupID int64, sessionHash string, ttl time.Duration) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Expire(ctx, key, ttl).Err()
}

// DeleteSessionAccountID 删除粘性会话与账号的绑定关系。
// 当检测到绑定的账号不可用（如状态错误、禁用、不可调度等）时调用，
// 以便下次请求能够重新选择可用账号。
//
// DeleteSessionAccountID removes the sticky session binding for the given session.
// Called when the bound account becomes unavailable (e.g., error status, disabled,
// or unschedulable), allowing subsequent requests to select a new available account.
func (c *gatewayCache) DeleteSessionAccountID(ctx context.Context, groupID int64, sessionHash string) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Del(ctx, key).Err()
}

var _ service.CyberPolicyIsolationStore = (*gatewayCache)(nil)

const cyberPolicyIsolationPrefix = "cyber_policy_isolation:"

const (
	cyberPolicyScopeCodeNone int64 = iota
	cyberPolicyScopeCodeUserGroupDay
)

// cyberPolicyRecordHitScript performs upstream-attempt deduplication, daily
// counting, and the user/group day restriction atomically. All keys share the
// same Redis Cluster hash tag.
//
// KEYS[1] daily count, KEYS[2] seen attempt, KEYS[3] user/group day block.
// ARGV[1] local day reset epoch ms.
// Returns {hit sequence, scope code, blocked-until epoch ms, duplicate (0/1)}.
var cyberPolicyRecordHitScript = redis.NewScript(`
local seen = redis.call('HMGET', KEYS[2], 'count', 'scope', 'until')
if seen[1] ~= false then
  return {tonumber(seen[1]) or 0, tonumber(seen[2]) or 0, tonumber(seen[3]) or 0, 1}
end

local count = redis.call('INCR', KEYS[1])
redis.call('PEXPIREAT', KEYS[1], ARGV[1])

local scope = 1
local blocked_until = tonumber(ARGV[1])
redis.call('SET', KEYS[3], tostring(blocked_until))
redis.call('PEXPIREAT', KEYS[3], blocked_until)

redis.call('HSET', KEYS[2], 'count', count, 'scope', scope, 'until', blocked_until)
redis.call('PEXPIREAT', KEYS[2], ARGV[1])
return {count, scope, blocked_until, 0}
`)

// cyberPolicyCheckBlockScript checks the natural-day user/group block.
// Returns {scope code, remaining TTL ms, blocked-until epoch ms}.
var cyberPolicyCheckBlockScript = redis.NewScript(`
local ttl = redis.call('PTTL', KEYS[1])
if ttl > 0 then return {1, ttl, tonumber(redis.call('GET', KEYS[1])) or 0} end
return {0, 0, 0}
`)

// cyberPolicyClearBlockScript clears only the current day's block and hit
// count. Seen-attempt keys deliberately remain to make administrative release
// idempotent for an already handled upstream attempt.
var cyberPolicyClearBlockScript = redis.NewScript(`
local existed = redis.call('EXISTS', KEYS[1])
redis.call('DEL', KEYS[1], KEYS[2])
return existed
`)

type cyberPolicyIsolationKeys struct {
	count string
	seen  string
	day   string
}

func cyberPolicyKeyDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func buildCyberPolicyIsolationKeys(
	userID, effectiveGroupID int64,
	businessDate, upstreamAttemptID string,
) cyberPolicyIsolationKeys {
	tag := fmt.Sprintf("{u%d:g%d}", userID, effectiveGroupID)
	base := cyberPolicyIsolationPrefix + tag
	seenPart := "none"
	if upstreamAttemptID != "" {
		seenPart = cyberPolicyKeyDigest(upstreamAttemptID)
	}
	return cyberPolicyIsolationKeys{
		count: base + ":count:" + businessDate,
		seen:  base + ":seen:" + seenPart,
		day:   base + ":day:" + businessDate,
	}
}

func cyberPolicyBusinessWindow(now time.Time) (businessDate string, resetAt time.Time) {
	localNow := now.In(timezone.Location())
	return localNow.Format("20060102"), timezone.StartOfDay(localNow).AddDate(0, 0, 1)
}

func cyberPolicyScopeFromCode(code int64) service.CyberPolicyBlockScope {
	switch code {
	case cyberPolicyScopeCodeUserGroupDay:
		return service.CyberPolicyBlockScopeUserGroupDay
	default:
		return service.CyberPolicyBlockScopeNone
	}
}

func cyberPolicyScriptInt64(value any) (int64, error) {
	switch v := value.(type) {
	case int64:
		return v, nil
	case string:
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse integer %q: %w", v, err)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("unexpected script integer type %T", value)
	}
}

func (c *gatewayCache) RecordHit(
	ctx context.Context,
	userID, effectiveGroupID int64,
	upstreamAttemptID string,
) (service.CyberPolicyHitDecision, error) {
	if c == nil || c.rdb == nil {
		return service.CyberPolicyHitDecision{}, errors.New("cyber policy isolation redis is unavailable")
	}
	if userID <= 0 || effectiveGroupID <= 0 {
		return service.CyberPolicyHitDecision{}, errors.New("cyber policy isolation requires positive user and group IDs")
	}
	upstreamAttemptID = strings.TrimSpace(upstreamAttemptID)
	if upstreamAttemptID == "" {
		return service.CyberPolicyHitDecision{}, errors.New("cyber policy isolation requires upstream attempt ID")
	}

	redisNow, err := c.rdb.Time(ctx).Result()
	if err != nil {
		return service.CyberPolicyHitDecision{}, fmt.Errorf("get Redis time for cyber policy hit: %w", err)
	}
	businessDate, resetAt := cyberPolicyBusinessWindow(redisNow)
	keys := buildCyberPolicyIsolationKeys(userID, effectiveGroupID, businessDate, upstreamAttemptID)

	values, err := cyberPolicyRecordHitScript.Run(
		ctx,
		c.rdb,
		[]string{keys.count, keys.seen, keys.day},
		resetAt.UnixMilli(),
	).Slice()
	if err != nil {
		return service.CyberPolicyHitDecision{}, fmt.Errorf("record cyber policy hit: %w", err)
	}
	if len(values) != 4 {
		return service.CyberPolicyHitDecision{}, fmt.Errorf("record cyber policy hit returned %d values", len(values))
	}
	hitSequence, err := cyberPolicyScriptInt64(values[0])
	if err != nil {
		return service.CyberPolicyHitDecision{}, fmt.Errorf("parse cyber policy hit sequence: %w", err)
	}
	scopeCode, err := cyberPolicyScriptInt64(values[1])
	if err != nil {
		return service.CyberPolicyHitDecision{}, fmt.Errorf("parse cyber policy action: %w", err)
	}
	blockedUntilMillis, err := cyberPolicyScriptInt64(values[2])
	if err != nil {
		return service.CyberPolicyHitDecision{}, fmt.Errorf("parse cyber policy blocked until: %w", err)
	}
	duplicateCode, err := cyberPolicyScriptInt64(values[3])
	if err != nil {
		return service.CyberPolicyHitDecision{}, fmt.Errorf("parse cyber policy duplicate marker: %w", err)
	}

	decision := service.CyberPolicyHitDecision{
		HitSequence: hitSequence,
		Action:      cyberPolicyScopeFromCode(scopeCode),
		Duplicate:   duplicateCode == 1,
	}
	if blockedUntilMillis > 0 {
		decision.BlockedUntil = time.UnixMilli(blockedUntilMillis).In(timezone.Location())
	}
	return decision, nil
}

func (c *gatewayCache) CheckBlock(
	ctx context.Context,
	userID, effectiveGroupID int64,
) (service.CyberPolicyBlockState, error) {
	if c == nil || c.rdb == nil {
		return service.CyberPolicyBlockState{}, errors.New("cyber policy isolation redis is unavailable")
	}
	if userID <= 0 || effectiveGroupID <= 0 {
		return service.CyberPolicyBlockState{}, errors.New("cyber policy isolation requires positive user and group IDs")
	}

	redisNow, err := c.rdb.Time(ctx).Result()
	if err != nil {
		return service.CyberPolicyBlockState{}, fmt.Errorf("get Redis time for cyber policy check: %w", err)
	}
	businessDate, _ := cyberPolicyBusinessWindow(redisNow)
	keys := buildCyberPolicyIsolationKeys(userID, effectiveGroupID, businessDate, "")

	values, err := cyberPolicyCheckBlockScript.Run(
		ctx,
		c.rdb,
		[]string{keys.day},
	).Slice()
	if err != nil {
		return service.CyberPolicyBlockState{}, fmt.Errorf("check cyber policy block: %w", err)
	}
	if len(values) != 3 {
		return service.CyberPolicyBlockState{}, fmt.Errorf("check cyber policy block returned %d values", len(values))
	}
	scopeCode, err := cyberPolicyScriptInt64(values[0])
	if err != nil {
		return service.CyberPolicyBlockState{}, fmt.Errorf("parse cyber policy block scope: %w", err)
	}
	ttlMillis, err := cyberPolicyScriptInt64(values[1])
	if err != nil {
		return service.CyberPolicyBlockState{}, fmt.Errorf("parse cyber policy block TTL: %w", err)
	}
	if scopeCode == cyberPolicyScopeCodeNone || ttlMillis <= 0 {
		return service.CyberPolicyBlockState{}, nil
	}
	blockedUntilMillis, err := cyberPolicyScriptInt64(values[2])
	if err != nil {
		return service.CyberPolicyBlockState{}, fmt.Errorf("parse cyber policy block deadline: %w", err)
	}
	retryAfter := time.Duration(ttlMillis) * time.Millisecond
	blockedUntil := redisNow.Add(retryAfter).In(timezone.Location())
	if blockedUntilMillis > 0 {
		blockedUntil = time.UnixMilli(blockedUntilMillis).In(timezone.Location())
	}
	return service.CyberPolicyBlockState{
		Blocked:      true,
		Scope:        cyberPolicyScopeFromCode(scopeCode),
		RetryAfter:   retryAfter,
		BlockedUntil: blockedUntil,
	}, nil
}

func (c *gatewayCache) ClearBlock(
	ctx context.Context,
	userID, effectiveGroupID int64,
) (bool, error) {
	if c == nil || c.rdb == nil {
		return false, errors.New("cyber policy isolation redis is unavailable")
	}
	if userID <= 0 || effectiveGroupID <= 0 {
		return false, errors.New("cyber policy isolation requires positive user and group IDs")
	}

	redisNow, err := c.rdb.Time(ctx).Result()
	if err != nil {
		return false, fmt.Errorf("get Redis time for cyber policy clear: %w", err)
	}
	businessDate, _ := cyberPolicyBusinessWindow(redisNow)
	keys := buildCyberPolicyIsolationKeys(userID, effectiveGroupID, businessDate, "")
	removed, err := cyberPolicyClearBlockScript.Run(
		ctx,
		c.rdb,
		[]string{keys.day, keys.count},
	).Int64()
	if err != nil {
		return false, fmt.Errorf("clear cyber policy block: %w", err)
	}
	return removed == 1, nil
}

func (c *gatewayCache) GetSessionString(ctx context.Context, groupID int64, sessionHash string) (string, error) {
	key := buildSessionKey(groupID, sessionHash)
	value, err := c.rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", fmt.Errorf("%w: %w", service.ErrGatewaySessionStringNotFound, err)
	}
	return value, err
}

func (c *gatewayCache) SetSessionString(ctx context.Context, groupID int64, sessionHash string, value string, ttl time.Duration) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Set(ctx, key, value, ttl).Err()
}

func (c *gatewayCache) DeleteSessionString(ctx context.Context, groupID int64, sessionHash string) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Del(ctx, key).Err()
}
