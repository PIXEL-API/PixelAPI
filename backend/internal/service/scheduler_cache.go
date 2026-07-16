package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	SchedulerModeSingle = "single"
	SchedulerModeMixed  = "mixed"
	SchedulerModeForced = "forced"
)

var (
	ErrSchedulerBucketRetired              = errors.New("scheduler bucket retired")
	ErrSchedulerBucketWriteFenced          = errors.New("scheduler bucket write fenced")
	ErrSchedulerGroupLifecycleLeaseInvalid = errors.New("scheduler group lifecycle lease invalid")
	ErrSchedulerGroupLifecycleLeaseLost    = errors.New("scheduler group lifecycle lease lost")
	ErrSchedulerLifecycleCacheRequired     = errors.New("scheduler cache does not support fenced lifecycle operations")
)

// SchedulerBucketWriteToken fences a snapshot writer to one bucket epoch.
// The token must be captured before loading accounts from the database.
type SchedulerBucketWriteToken struct {
	Bucket SchedulerBucket
	Epoch  int64
}

func (t SchedulerBucketWriteToken) ValidFor(bucket SchedulerBucket) bool {
	return t.Epoch > 0 && t.Bucket == bucket
}

// SchedulerGroupLifecycleLease identifies the owner of one group's short-lived
// retirement/reopen critical section.
type SchedulerGroupLifecycleLease struct {
	GroupID    int64
	OwnerToken string
}

func (l SchedulerGroupLifecycleLease) ValidFor(groupID int64) bool {
	return groupID > 0 && l.GroupID == groupID && l.OwnerToken != ""
}

type SchedulerBucket struct {
	GroupID  int64
	Platform string
	Mode     string
}

type schedulerCandidateIndexBypassKey struct{}

func (b SchedulerBucket) String() string {
	return fmt.Sprintf("%d:%s:%s", b.GroupID, b.Platform, b.Mode)
}

func ParseSchedulerBucket(raw string) (SchedulerBucket, bool) {
	parts := strings.Split(raw, ":")
	if len(parts) != 3 {
		return SchedulerBucket{}, false
	}
	groupID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return SchedulerBucket{}, false
	}
	if parts[1] == "" || parts[2] == "" {
		return SchedulerBucket{}, false
	}
	return SchedulerBucket{
		GroupID:  groupID,
		Platform: parts[1],
		Mode:     parts[2],
	}, true
}

func WithSchedulerCandidateIndexBypass(ctx context.Context) context.Context {
	return context.WithValue(ctx, schedulerCandidateIndexBypassKey{}, true)
}

func IsSchedulerCandidateIndexBypassed(ctx context.Context) bool {
	bypass, _ := ctx.Value(schedulerCandidateIndexBypassKey{}).(bool)
	return bypass
}

// SchedulerCache 负责调度快照与账号快照的缓存读写。
type SchedulerCache interface {
	// GetSnapshot 读取快照并返回命中与否（ready + active + 数据完整）。
	GetSnapshot(ctx context.Context, bucket SchedulerBucket) ([]*Account, bool, error)
	// SetSnapshot 写入快照并切换激活版本。
	SetSnapshot(ctx context.Context, bucket SchedulerBucket, accounts []Account) error
	// GetAccount 获取单账号快照。
	GetAccount(ctx context.Context, accountID int64) (*Account, error)
	// SetAccount 写入单账号快照（包含不可调度状态）。
	SetAccount(ctx context.Context, account *Account) error
	// DeleteAccount 删除单账号快照。
	DeleteAccount(ctx context.Context, accountID int64) error
	// UpdateLastUsed 批量更新账号的最后使用时间。
	UpdateLastUsed(ctx context.Context, updates map[int64]time.Time) error
	// TryLockBucket 尝试获取分桶重建锁。
	TryLockBucket(ctx context.Context, bucket SchedulerBucket, ttl time.Duration) (bool, error)
	// UnlockBucket 释放分桶重建锁。
	UnlockBucket(ctx context.Context, bucket SchedulerBucket) error
	// ListBuckets 返回已注册的分桶集合。
	ListBuckets(ctx context.Context) ([]SchedulerBucket, error)
	// GetOutboxWatermark 读取 outbox 水位。
	GetOutboxWatermark(ctx context.Context) (int64, error)
	// SetOutboxWatermark 保存 outbox 水位。
	SetOutboxWatermark(ctx context.Context, id int64) error
}

// SchedulerLifecycleCache extends SchedulerCache with epoch-fenced writes and
// persistent retirement. The legacy SetSnapshot method remains on
// SchedulerCache for compatibility with narrow readers/test doubles; production
// snapshot rebuilds must require this extension and fail fast when unavailable.
type SchedulerLifecycleCache interface {
	SchedulerCache
	CaptureBucketWriteToken(ctx context.Context, bucket SchedulerBucket) (SchedulerBucketWriteToken, error)
	SetSnapshotFenced(ctx context.Context, bucket SchedulerBucket, token SchedulerBucketWriteToken, accounts []Account) error
	RetireBucket(ctx context.Context, bucket SchedulerBucket) error
	ReopenBucket(ctx context.Context, bucket SchedulerBucket) (SchedulerBucketWriteToken, error)
	TryAcquireGroupLifecycleLease(ctx context.Context, groupID int64, ttl time.Duration) (SchedulerGroupLifecycleLease, bool, error)
	ReleaseGroupLifecycleLease(ctx context.Context, lease SchedulerGroupLifecycleLease) error
}

// SchedulerCandidateCache is an optional extension for caches that can return a
// small sampled candidate set instead of materializing a whole scheduler bucket.
type SchedulerCandidateCache interface {
	// GetCandidateSnapshot reads a capped candidate sample for bucket. Sampling
	// applies when globalEnabled is true (dynamic system setting) or the bucket
	// is in the static config allowlist, and only for buckets whose size exceeds
	// threshold (threshold<=0 falls back to the built-in default). hit=false
	// means callers should fall back to the full scheduler snapshot.
	GetCandidateSnapshot(ctx context.Context, bucket SchedulerBucket, limit, threshold int, globalEnabled bool) ([]*Account, bool, error)
}
