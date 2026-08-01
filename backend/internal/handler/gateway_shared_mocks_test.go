package handler

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// concurrencyCacheMock 是 handler 包内共享的 service.ConcurrencyCache 测试替身。
// （原定义在 gateway_helper_fastpath_test.go / gateway_handler_account_share_mode_context_test.go，
// 随 billing intent 机制删除后移植到本文件。）
type concurrencyCacheMock struct {
	acquireUserSlotFn    func(ctx context.Context, userID int64, maxConcurrency int, requestID string) (bool, error)
	acquireAccountSlotFn func(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error)
	releaseUserCalled    int32
	releaseAccountCalled int32
}

func (m *concurrencyCacheMock) AcquireAccountSlot(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error) {
	if m.acquireAccountSlotFn != nil {
		return m.acquireAccountSlotFn(ctx, accountID, maxConcurrency, requestID)
	}
	return false, nil
}

func (m *concurrencyCacheMock) ReleaseAccountSlot(ctx context.Context, accountID int64, requestID string) error {
	atomic.AddInt32(&m.releaseAccountCalled, 1)
	return nil
}

func (m *concurrencyCacheMock) GetAccountConcurrency(ctx context.Context, accountID int64) (int, error) {
	return 0, nil
}

func (m *concurrencyCacheMock) GetAccountConcurrencyBatch(ctx context.Context, accountIDs []int64) (map[int64]int, error) {
	result := make(map[int64]int, len(accountIDs))
	for _, accountID := range accountIDs {
		result[accountID] = 0
	}
	return result, nil
}

func (m *concurrencyCacheMock) IncrementAccountWaitCount(ctx context.Context, accountID int64, maxWait int) (bool, error) {
	return true, nil
}

func (m *concurrencyCacheMock) DecrementAccountWaitCount(ctx context.Context, accountID int64) error {
	return nil
}

func (m *concurrencyCacheMock) GetAccountWaitingCount(ctx context.Context, accountID int64) (int, error) {
	return 0, nil
}

func (m *concurrencyCacheMock) AcquireUserSlot(ctx context.Context, userID int64, maxConcurrency int, requestID string) (bool, error) {
	if m.acquireUserSlotFn != nil {
		return m.acquireUserSlotFn(ctx, userID, maxConcurrency, requestID)
	}
	return false, nil
}

func (m *concurrencyCacheMock) ReleaseUserSlot(ctx context.Context, userID int64, requestID string) error {
	atomic.AddInt32(&m.releaseUserCalled, 1)
	return nil
}

func (m *concurrencyCacheMock) GetUserConcurrency(ctx context.Context, userID int64) (int, error) {
	return 0, nil
}

func (m *concurrencyCacheMock) IncrementWaitCount(ctx context.Context, userID int64, maxWait int) (bool, error) {
	return true, nil
}

func (m *concurrencyCacheMock) DecrementWaitCount(ctx context.Context, userID int64) error {
	return nil
}

func (m *concurrencyCacheMock) GetAccountsLoadBatch(ctx context.Context, accounts []service.AccountWithConcurrency) (map[int64]*service.AccountLoadInfo, error) {
	return map[int64]*service.AccountLoadInfo{}, nil
}

func (m *concurrencyCacheMock) GetUsersLoadBatch(ctx context.Context, users []service.UserWithConcurrency) (map[int64]*service.UserLoadInfo, error) {
	return map[int64]*service.UserLoadInfo{}, nil
}

func (m *concurrencyCacheMock) CleanupExpiredAccountSlots(ctx context.Context, accountID int64) error {
	return nil
}

func (m *concurrencyCacheMock) CleanupExpiredSlots(ctx context.Context) error {
	return nil
}

func (m *concurrencyCacheMock) AcquireAccountShareMembershipSlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}

func (m *concurrencyCacheMock) ReleaseAccountShareMembershipSlot(context.Context, int64, string) error {
	return nil
}

func (m *concurrencyCacheMock) GetAccountShareMembershipConcurrency(context.Context, int64) (int, error) {
	return 0, nil
}

func (m *concurrencyCacheMock) RefreshAccountSlot(context.Context, int64, string) (bool, error) {
	return true, nil
}

func (m *concurrencyCacheMock) RefreshAccountShareMembershipSlot(context.Context, int64, string) (bool, error) {
	return true, nil
}

func (m *concurrencyCacheMock) SlotLeaseTTL() time.Duration {
	return time.Hour
}
