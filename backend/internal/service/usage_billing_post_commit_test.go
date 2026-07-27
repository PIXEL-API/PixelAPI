package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsageBillingPostCommitFinalizerClosesDurableSideEffects(t *testing.T) {
	groupID := int64(4)
	shareOwnerID := int64(10)
	newBalance := 4.0
	cache := &usageBillingPostCommitCacheStub{}
	lastUsed := &usageBillingPostCommitLastUsedStub{}
	authCache := &usageBillingPostCommitAuthCacheStub{}
	userReader := &usageBillingPostCommitUserReaderStub{
		user: &User{ID: 3, Balance: newBalance},
	}
	accountReader := &usageBillingPostCommitAccountReaderStub{
		account: &Account{ID: 7},
	}
	notifier := &usageBillingPostCommitNotifierStub{}
	finalizer, err := NewUsageBillingPostCommitFinalizer(
		cache,
		lastUsed,
		authCache,
		userReader,
		accountReader,
		notifier,
	)
	require.NoError(t, err)

	command := &UsageBillingCommand{
		UserID:                     3,
		AccountID:                  7,
		APIKeyID:                   5,
		GroupID:                    &groupID,
		SubscriptionCost:           0.2,
		PrivateGroupCommissionCost: 0.1,
		APIKeyQuotaCost:            0.2,
		APIKeyRateLimitCost:        0.2,
		AccountQuotaCost:           0.4,
		ShareOwnerUserID:           &shareOwnerID,
		AccountShareModeSettlement: &AccountShareModeBillingSnapshot{OwnerUserID: 9},
	}
	result := &UsageBillingApplyResult{
		Applied:              true,
		NewBalance:           &newBalance,
		CommissionDeducted:   0.1,
		BalanceCreditUserIDs: []int64{10, 11},
		QuotaState:           &AccountQuotaState{TotalUsed: 2, TotalLimit: 10},
	}

	require.NoError(t, finalizer.Finalize(context.Background(), command, result))
	require.ElementsMatch(t, []int64{3, 9, 10, 11}, cache.balanceUserIDs)
	require.Equal(t, [][2]int64{{3, 4}}, cache.subscriptions)
	require.Equal(t, []int64{5}, cache.rateLimitKeyIDs)
	require.Equal(t, []int64{3}, authCache.userIDs)
	require.Equal(t, []int64{7}, lastUsed.accountIDs)

	require.Len(t, notifier.balanceCalls, 1)
	require.Equal(t, int64(3), notifier.balanceCalls[0].userID)
	require.InDelta(t, 4.1, notifier.balanceCalls[0].oldBalance, 1e-12)
	require.InDelta(t, 0.1, notifier.balanceCalls[0].cost, 1e-12)
	require.Len(t, notifier.quotaCalls, 1)
	require.Equal(t, int64(7), notifier.quotaCalls[0].accountID)
	require.InDelta(t, 0.4, notifier.quotaCalls[0].cost, 1e-12)
	require.Same(t, result.QuotaState, notifier.quotaCalls[0].state)
}

func TestUsageBillingPostCommitFinalizerReplayInvalidatesCachesWithoutDuplicateNotifications(t *testing.T) {
	cache := &usageBillingPostCommitCacheStub{}
	lastUsed := &usageBillingPostCommitLastUsedStub{}
	authCache := &usageBillingPostCommitAuthCacheStub{}
	notifier := &usageBillingPostCommitNotifierStub{}
	finalizer, err := NewUsageBillingPostCommitFinalizer(
		cache,
		lastUsed,
		authCache,
		&usageBillingPostCommitUserReaderStub{user: &User{ID: 3}},
		&usageBillingPostCommitAccountReaderStub{account: &Account{ID: 7}},
		notifier,
	)
	require.NoError(t, err)

	command := &UsageBillingCommand{
		UserID:           3,
		AccountID:        7,
		APIKeyID:         5,
		BalanceCost:      0.2,
		APIKeyQuotaCost:  0.2,
		AccountQuotaCost: 0.4,
		ShareOwnerUserID: int64TestPointer(9),
	}

	require.NoError(t, finalizer.Finalize(
		context.Background(),
		command,
		&UsageBillingApplyResult{Applied: false},
	))
	require.ElementsMatch(t, []int64{3, 9}, cache.balanceUserIDs)
	require.Equal(t, []int64{3}, authCache.userIDs)
	require.Equal(t, []int64{7}, lastUsed.accountIDs)
	require.Empty(t, notifier.balanceCalls)
	require.Empty(t, notifier.quotaCalls)
}

func TestUsageBillingPostCommitFinalizerReturnsCacheFailure(t *testing.T) {
	cacheErr := errors.New("redis unavailable")
	finalizer, err := NewUsageBillingPostCommitFinalizer(
		&usageBillingPostCommitCacheStub{balanceErr: cacheErr},
		&usageBillingPostCommitLastUsedStub{},
		&usageBillingPostCommitAuthCacheStub{},
		&usageBillingPostCommitUserReaderStub{user: &User{ID: 3}},
		&usageBillingPostCommitAccountReaderStub{account: &Account{ID: 7}},
		&usageBillingPostCommitNotifierStub{},
	)
	require.NoError(t, err)

	err = finalizer.Finalize(context.Background(), &UsageBillingCommand{
		UserID:      3,
		AccountID:   7,
		BalanceCost: 0.2,
	}, &UsageBillingApplyResult{Applied: false})
	require.ErrorIs(t, err, cacheErr)
}

func int64TestPointer(value int64) *int64 {
	return &value
}

type usageBillingPostCommitCacheStub struct {
	balanceUserIDs  []int64
	subscriptions   [][2]int64
	rateLimitKeyIDs []int64
	balanceErr      error
}

func (s *usageBillingPostCommitCacheStub) InvalidateUserBalance(_ context.Context, userID int64) error {
	s.balanceUserIDs = append(s.balanceUserIDs, userID)
	return s.balanceErr
}

func (s *usageBillingPostCommitCacheStub) InvalidateSubscription(_ context.Context, userID, groupID int64) error {
	s.subscriptions = append(s.subscriptions, [2]int64{userID, groupID})
	return nil
}

func (s *usageBillingPostCommitCacheStub) InvalidateAPIKeyRateLimit(_ context.Context, keyID int64) error {
	s.rateLimitKeyIDs = append(s.rateLimitKeyIDs, keyID)
	return nil
}

type usageBillingPostCommitLastUsedStub struct {
	accountIDs []int64
}

func (s *usageBillingPostCommitLastUsedStub) ScheduleLastUsedUpdate(accountID int64) {
	s.accountIDs = append(s.accountIDs, accountID)
}

type usageBillingPostCommitAuthCacheStub struct {
	userIDs []int64
}

func (*usageBillingPostCommitAuthCacheStub) InvalidateAuthCacheByKey(context.Context, string) {}

func (s *usageBillingPostCommitAuthCacheStub) InvalidateAuthCacheByUserID(_ context.Context, userID int64) {
	s.userIDs = append(s.userIDs, userID)
}

func (*usageBillingPostCommitAuthCacheStub) InvalidateAuthCacheByGroupID(context.Context, int64) {}

type usageBillingPostCommitUserReaderStub struct {
	user *User
	err  error
}

func (s *usageBillingPostCommitUserReaderStub) GetByID(context.Context, int64) (*User, error) {
	return s.user, s.err
}

type usageBillingPostCommitAccountReaderStub struct {
	account *Account
	err     error
}

func (s *usageBillingPostCommitAccountReaderStub) GetByID(context.Context, int64) (*Account, error) {
	return s.account, s.err
}

type usageBillingPostCommitBalanceCall struct {
	userID     int64
	oldBalance float64
	cost       float64
}

type usageBillingPostCommitQuotaCall struct {
	accountID int64
	cost      float64
	state     *AccountQuotaState
}

type usageBillingPostCommitNotifierStub struct {
	balanceCalls []usageBillingPostCommitBalanceCall
	quotaCalls   []usageBillingPostCommitQuotaCall
}

func (s *usageBillingPostCommitNotifierStub) CheckBalanceAfterDeduction(
	_ context.Context,
	user *User,
	oldBalance,
	cost float64,
) {
	s.balanceCalls = append(s.balanceCalls, usageBillingPostCommitBalanceCall{
		userID:     user.ID,
		oldBalance: oldBalance,
		cost:       cost,
	})
}

func (s *usageBillingPostCommitNotifierStub) CheckAccountQuotaAfterIncrement(
	_ context.Context,
	account *Account,
	cost float64,
	state *AccountQuotaState,
) {
	s.quotaCalls = append(s.quotaCalls, usageBillingPostCommitQuotaCall{
		accountID: account.ID,
		cost:      cost,
		state:     state,
	})
}
