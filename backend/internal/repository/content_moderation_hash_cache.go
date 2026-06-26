package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	contentModerationFlaggedHashSetKey   = "content_moderation:flagged_hashes"
	contentModerationUserTrustKeyPrefix  = "content_moderation:user_trust:"
	contentModerationUserTrustCASRetries = 8
)

type contentModerationHashCache struct {
	rdb *redis.Client
}

func NewContentModerationHashCache(rdb *redis.Client) service.ContentModerationHashCache {
	return &contentModerationHashCache{rdb: rdb}
}

func (c *contentModerationHashCache) RecordFlaggedInputHash(ctx context.Context, inputHash string) error {
	inputHash = strings.TrimSpace(inputHash)
	if c == nil || c.rdb == nil || inputHash == "" {
		return nil
	}
	return c.rdb.SAdd(ctx, contentModerationFlaggedHashSetKey, inputHash).Err()
}

func (c *contentModerationHashCache) HasFlaggedInputHash(ctx context.Context, inputHash string) (bool, error) {
	inputHash = strings.TrimSpace(inputHash)
	if c == nil || c.rdb == nil || inputHash == "" {
		return false, nil
	}
	return c.rdb.SIsMember(ctx, contentModerationFlaggedHashSetKey, inputHash).Result()
}

func (c *contentModerationHashCache) DeleteFlaggedInputHash(ctx context.Context, inputHash string) (bool, error) {
	inputHash = strings.TrimSpace(inputHash)
	if c == nil || c.rdb == nil || inputHash == "" {
		return false, nil
	}
	deleted, err := c.rdb.SRem(ctx, contentModerationFlaggedHashSetKey, inputHash).Result()
	if err != nil {
		return false, err
	}
	return deleted > 0, nil
}

func (c *contentModerationHashCache) ClearFlaggedInputHashes(ctx context.Context) (int64, error) {
	if c == nil || c.rdb == nil {
		return 0, nil
	}
	deleted, err := c.rdb.SCard(ctx, contentModerationFlaggedHashSetKey).Result()
	if err != nil {
		return 0, err
	}
	if deleted == 0 {
		return 0, nil
	}
	if err := c.rdb.Del(ctx, contentModerationFlaggedHashSetKey).Err(); err != nil {
		return 0, err
	}
	return deleted, nil
}

func (c *contentModerationHashCache) CountFlaggedInputHashes(ctx context.Context) (int64, error) {
	if c == nil || c.rdb == nil {
		return 0, nil
	}
	return c.rdb.SCard(ctx, contentModerationFlaggedHashSetKey).Result()
}

func (c *contentModerationHashCache) GetUserTrustState(ctx context.Context, userID int64) (*service.ContentModerationUserTrustState, error) {
	if c == nil || c.rdb == nil || userID <= 0 {
		return nil, nil
	}
	raw, err := c.rdb.Get(ctx, contentModerationUserTrustKey(userID)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state service.ContentModerationUserTrustState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return nil, fmt.Errorf("decode content moderation user trust state: %w", err)
	}
	return &state, nil
}

func (c *contentModerationHashCache) SetUserTrustState(ctx context.Context, userID int64, state *service.ContentModerationUserTrustState, ttl time.Duration) error {
	if c == nil || c.rdb == nil || userID <= 0 || state == nil {
		return nil
	}
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode content moderation user trust state: %w", err)
	}
	return c.rdb.Set(ctx, contentModerationUserTrustKey(userID), raw, ttl).Err()
}

func (c *contentModerationHashCache) UpdateUserTrustState(ctx context.Context, userID int64, ttl time.Duration, mutate service.ContentModerationUserTrustStateMutator) (*service.ContentModerationUserTrustState, error) {
	if c == nil || c.rdb == nil || userID <= 0 || mutate == nil {
		return nil, nil
	}
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}
	key := contentModerationUserTrustKey(userID)
	var saved *service.ContentModerationUserTrustState
	var lastErr error
	for attempt := 0; attempt < contentModerationUserTrustCASRetries; attempt++ {
		err := c.rdb.Watch(ctx, func(tx *redis.Tx) error {
			current, err := getContentModerationUserTrustStateFromRedis(ctx, tx, key)
			if err != nil {
				return err
			}
			next, err := mutate(current)
			if err != nil {
				return err
			}
			if next == nil {
				saved = nil
				return nil
			}
			raw, err := json.Marshal(next)
			if err != nil {
				return fmt.Errorf("encode content moderation user trust state: %w", err)
			}
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, key, raw, ttl)
				return nil
			})
			if err == nil {
				saved = next
			}
			return err
		}, key)
		if err == nil {
			return saved, nil
		}
		lastErr = err
		if !errors.Is(err, redis.TxFailedErr) {
			return nil, err
		}
	}
	return nil, lastErr
}

func getContentModerationUserTrustStateFromRedis(ctx context.Context, getter interface {
	Get(context.Context, string) *redis.StringCmd
}, key string) (*service.ContentModerationUserTrustState, error) {
	raw, err := getter.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state service.ContentModerationUserTrustState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return nil, fmt.Errorf("decode content moderation user trust state: %w", err)
	}
	return &state, nil
}

func contentModerationUserTrustKey(userID int64) string {
	return fmt.Sprintf("%s%d", contentModerationUserTrustKeyPrefix, userID)
}
