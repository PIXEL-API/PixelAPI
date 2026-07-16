//go:build unit

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQuotaPoolDashboardCacheTTLCoversPollingInterval(t *testing.T) {
	require.Greater(t, accountQuotaPoolDashboardCacheTTL, 60*time.Second)
}

// quotaPoolRepoStub implements just enough of AccountRepository to exercise
// GetQuotaPoolDashboard. The embedded interface is nil; any method other than
// ListQuotaPoolAccounts would panic, which is fine because the dashboard path
// only calls ListQuotaPoolAccounts.
type quotaPoolRepoStub struct {
	AccountRepository

	mu      sync.Mutex
	calls   map[int64]int
	release chan struct{}
}

func newQuotaPoolRepoStub() *quotaPoolRepoStub {
	return &quotaPoolRepoStub{calls: make(map[int64]int)}
}

func (s *quotaPoolRepoStub) ListQuotaPoolAccounts(ctx context.Context, ownerUserID int64) ([]Account, error) {
	s.mu.Lock()
	s.calls[ownerUserID]++
	release := s.release
	s.mu.Unlock()

	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	owner := ownerUserID
	return []Account{{
		ID:          ownerUserID,
		OwnerUserID: &owner,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
	}}, nil
}

func (s *quotaPoolRepoStub) callCount(ownerUserID int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[ownerUserID]
}

// TestGetQuotaPoolDashboard_CacheIsolatedPerUser verifies the dashboard cache
// keeps a separate entry per user. The previous single-entry cache was keyed by
// a single userID, so a second user's request evicted the first user's entry
// and forced a full account scan on every alternating request — the root cause
// of the production memory blow-up.
func TestGetQuotaPoolDashboard_CacheIsolatedPerUser(t *testing.T) {
	repo := newQuotaPoolRepoStub()
	svc := &AccountService{accountRepo: repo}
	ctx := context.Background()

	_, err := svc.GetQuotaPoolDashboard(ctx, 1)
	require.NoError(t, err)
	_, err = svc.GetQuotaPoolDashboard(ctx, 2)
	require.NoError(t, err)
	// User 1 again: must be served from cache, NOT trigger a reload.
	_, err = svc.GetQuotaPoolDashboard(ctx, 1)
	require.NoError(t, err)

	require.Equal(t, 1, repo.callCount(1), "user 1 should load once and then hit cache")
	require.Equal(t, 1, repo.callCount(2), "user 2 should load once")
}

// TestGetQuotaPoolDashboard_CoalescesConcurrentMisses verifies that a burst of
// concurrent requests for the same user collapses into a single DB load via
// singleflight, instead of each request running its own full account scan.
func TestGetQuotaPoolDashboard_CoalescesConcurrentMisses(t *testing.T) {
	repo := newQuotaPoolRepoStub()
	repo.release = make(chan struct{})
	svc := &AccountService{accountRepo: repo}

	const concurrency = 32
	var wg sync.WaitGroup
	var started sync.WaitGroup
	errs := make([]error, concurrency)
	wg.Add(concurrency)
	started.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			defer wg.Done()
			started.Done()
			dashboard, err := svc.GetQuotaPoolDashboard(context.Background(), 7)
			if err == nil {
				require.NotNil(t, dashboard)
			}
			errs[idx] = err
		}(i)
	}

	// Ensure all goroutines have entered before releasing the single load.
	started.Wait()
	close(repo.release)
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 1, repo.callCount(7), "concurrent misses must coalesce into one load")
}
