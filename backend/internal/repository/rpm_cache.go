package repository

import (
	"context"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const rpmKeyPrefix = "rpm:"

// Lua scripts use Redis TIME for server-side minute key calculation
var rpmIncrScript = redis.NewScript(`
local timeResult = redis.call('TIME')
local minuteKey = math.floor(tonumber(timeResult[1]) / 60)
local key = ARGV[1] .. ':' .. minuteKey
local count = redis.call('INCR', key)
if count == 1 then
    redis.call('EXPIRE', key, 120)
end
return count
`)

var rpmGetScript = redis.NewScript(`
local timeResult = redis.call('TIME')
local minuteKey = math.floor(tonumber(timeResult[1]) / 60)
local key = ARGV[1] .. ':' .. minuteKey
local count = redis.call('GET', key)
if count == false then
    return 0
end
return tonumber(count)
`)

type RPMCacheImpl struct {
	rdb *redis.Client
}

func NewRPMCache(rdb *redis.Client) service.RPMCache {
	return &RPMCacheImpl{rdb: rdb}
}

func rpmKeyBase(accountID int64) string {
	return fmt.Sprintf("%s%d", rpmKeyPrefix, accountID)
}

func (c *RPMCacheImpl) IncrementRPM(ctx context.Context, accountID int64) (int, error) {
	result, err := rpmIncrScript.Run(ctx, c.rdb, nil, rpmKeyBase(accountID)).Int()
	if err != nil {
		return 0, fmt.Errorf("rpm increment: %w", err)
	}
	return result, nil
}

func (c *RPMCacheImpl) GetRPM(ctx context.Context, accountID int64) (int, error) {
	result, err := rpmGetScript.Run(ctx, c.rdb, nil, rpmKeyBase(accountID)).Int()
	if err != nil {
		return 0, fmt.Errorf("rpm get: %w", err)
	}
	return result, nil
}

func (c *RPMCacheImpl) GetRPMBatch(ctx context.Context, accountIDs []int64) (map[int64]int, error) {
	if len(accountIDs) == 0 {
		return map[int64]int{}, nil
	}

	pipe := c.rdb.Pipeline()
	cmds := make(map[int64]*redis.Cmd, len(accountIDs))
	for _, id := range accountIDs {
		cmds[id] = rpmGetScript.Run(ctx, pipe, nil, rpmKeyBase(id))
	}

	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("rpm batch get: %w", err)
	}

	result := make(map[int64]int, len(accountIDs))
	for id, cmd := range cmds {
		if val, err := cmd.Int(); err == nil {
			result[id] = val
		} else {
			result[id] = 0
		}
	}
	return result, nil
}
