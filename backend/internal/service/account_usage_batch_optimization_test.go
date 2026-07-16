package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type ownedAccountIDBatchRepoStub struct {
	AccountRepository

	ownedIDs  []int64
	err       error
	calls     int
	ownerID   int64
	requested []int64
}

func (s *ownedAccountIDBatchRepoStub) ListOwnedAccountIDs(_ context.Context, ownerUserID int64, accountIDs []int64) ([]int64, error) {
	s.calls++
	s.ownerID = ownerUserID
	s.requested = append([]int64(nil), accountIDs...)
	return append([]int64(nil), s.ownedIDs...), s.err
}

func TestAccountServiceEnsureOwnedByIDsUsesSingleLightweightLookup(t *testing.T) {
	repo := &ownedAccountIDBatchRepoStub{ownedIDs: []int64{2, 1}}
	svc := &AccountService{accountRepo: repo}

	err := svc.EnsureOwnedByIDs(context.Background(), 99, []int64{2, 1, 2})

	require.NoError(t, err)
	require.Equal(t, 1, repo.calls)
	require.Equal(t, int64(99), repo.ownerID)
	require.Equal(t, []int64{2, 1}, repo.requested)
}

func TestAccountServiceEnsureOwnedByIDsRejectsAnyMissingAccount(t *testing.T) {
	repo := &ownedAccountIDBatchRepoStub{ownedIDs: []int64{1}}
	svc := &AccountService{accountRepo: repo}

	err := svc.EnsureOwnedByIDs(context.Background(), 99, []int64{1, 2})

	require.ErrorIs(t, err, ErrAccountNotFound)
	require.Equal(t, 1, repo.calls)
}

type todayStatsBatchRepoStub struct {
	UsageLogRepository

	batchResult map[int64]*usagestats.AccountStats
	batchErr    error
	batchCalls  int
	singleCalls int
	requested   []int64
}

func (s *todayStatsBatchRepoStub) GetAccountWindowStatsBatch(_ context.Context, accountIDs []int64, _ time.Time) (map[int64]*usagestats.AccountStats, error) {
	s.batchCalls++
	s.requested = append([]int64(nil), accountIDs...)
	return s.batchResult, s.batchErr
}

func (s *todayStatsBatchRepoStub) GetAccountWindowStats(context.Context, int64, time.Time) (*usagestats.AccountStats, error) {
	s.singleCalls++
	return &usagestats.AccountStats{}, nil
}

func TestAccountUsageServiceGetTodayStatsBatchFailsFast(t *testing.T) {
	queryErr := errors.New("batch query failed")
	repo := &todayStatsBatchRepoStub{batchErr: queryErr}
	svc := &AccountUsageService{usageLogRepo: repo}

	stats, err := svc.GetTodayStatsBatch(context.Background(), []int64{1, 2})

	require.Nil(t, stats)
	require.ErrorIs(t, err, queryErr)
	require.Equal(t, 1, repo.batchCalls)
	require.Zero(t, repo.singleCalls, "批量查询失败后不得退化为单账号查询")
}

func TestAccountUsageServiceGetTodayStatsBatchFillsOnlyLegitimateMissingRows(t *testing.T) {
	repo := &todayStatsBatchRepoStub{batchResult: map[int64]*usagestats.AccountStats{
		1: {Requests: 3, Tokens: 42, Cost: 1.25},
	}}
	svc := &AccountUsageService{usageLogRepo: repo}

	stats, err := svc.GetTodayStatsBatch(context.Background(), []int64{1, 2, 1, 0})

	require.NoError(t, err)
	require.Equal(t, []int64{1, 2}, repo.requested)
	require.Equal(t, int64(3), stats[1].Requests)
	require.Equal(t, int64(42), stats[1].Tokens)
	require.Equal(t, float64(1.25), stats[1].Cost)
	require.Equal(t, &WindowStats{}, stats[2])
	require.Zero(t, repo.singleCalls)
}

type accountUsageAccountRepoStub struct {
	AccountRepository

	account  *Account
	getCalls int
}

func (s *accountUsageAccountRepoStub) GetByID(context.Context, int64) (*Account, error) {
	s.getCalls++
	return s.account, nil
}

func TestAccountUsageServiceVerifiedAccountEntrySkipsRepositoryReload(t *testing.T) {
	account := &Account{ID: 7, Platform: PlatformAnthropic, Type: AccountTypeAPIKey}
	repo := &accountUsageAccountRepoStub{account: account}
	svc := &AccountUsageService{accountRepo: repo}

	_, err := svc.GetUsageForAccount(context.Background(), account)

	require.Error(t, err)
	require.Zero(t, repo.getCalls)

	_, err = svc.GetUsage(context.Background(), account.ID)
	require.Error(t, err)
	require.Equal(t, 1, repo.getCalls)
}
