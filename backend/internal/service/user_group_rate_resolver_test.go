package service

import (
	"context"
	"errors"
	"testing"
	"time"

	gocache "github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/require"
)

type userGroupRateResolverRepoStub struct {
	UserGroupRateRepository

	rate  *float64
	err   error
	calls int
}

type userGroupRateUsageLogRepoStub struct {
	UsageLogRepository

	sum   float64
	err   error
	calls int
}

func (s *userGroupRateUsageLogRepoStub) SumUserGroupRateSourceActualCost(ctx context.Context, userID, groupID int64, source string, startTime, endTime time.Time) (float64, error) {
	s.calls++
	if s.err != nil {
		return 0, s.err
	}
	return s.sum, nil
}

func (s *userGroupRateResolverRepoStub) GetByUserAndGroup(ctx context.Context, userID, groupID int64) (*float64, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.rate, nil
}

func TestNewUserGroupRateResolver_Defaults(t *testing.T) {
	resolver := newUserGroupRateResolver(nil, nil, nil, 0, nil, "")

	require.NotNil(t, resolver)
	require.NotNil(t, resolver.cache)
	require.Equal(t, defaultUserGroupRateCacheTTL, resolver.cacheTTL)
	require.NotNil(t, resolver.sf)
	require.Equal(t, "service.gateway", resolver.logComponent)
}

func TestUserGroupRateResolverResolve_FallbackForNilResolverAndInvalidIDs(t *testing.T) {
	var nilResolver *userGroupRateResolver
	require.Equal(t, 1.4, nilResolver.Resolve(context.Background(), 101, 202, 1.4))

	resolver := newUserGroupRateResolver(nil, nil, nil, time.Second, nil, "service.test")
	require.Equal(t, 1.4, resolver.Resolve(context.Background(), 0, 202, 1.4))
	require.Equal(t, 1.4, resolver.Resolve(context.Background(), 101, 0, 1.4))
}

func TestUserGroupRateResolverResolve_InvalidCacheEntryLoadsRepoAndCaches(t *testing.T) {
	resetGatewayHotpathStatsForTest()

	rate := 1.7
	repo := &userGroupRateResolverRepoStub{rate: &rate}
	cache := gocache.New(time.Minute, time.Minute)
	cache.Set("101:202", "bad-cache", time.Minute)
	resolver := newUserGroupRateResolver(repo, nil, cache, time.Minute, nil, "service.test")

	got := resolver.Resolve(context.Background(), 101, 202, 1.2)
	require.Equal(t, rate, got)
	require.Equal(t, 1, repo.calls)

	cached, ok := cache.Get("101:202")
	require.True(t, ok)
	require.Equal(t, userGroupRateResolution{multiplier: rate, hasUserRate: true}, cached)

	hit, miss, load, _, fallback := GatewayUserGroupRateCacheStats()
	require.Equal(t, int64(0), hit)
	require.Equal(t, int64(1), miss)
	require.Equal(t, int64(1), load)
	require.Equal(t, int64(0), fallback)
}

func TestInvalidateUserGroupRateCacheEntries(t *testing.T) {
	cache := gocache.New(time.Minute, time.Minute)
	_ = newUserGroupRateResolver(nil, nil, cache, time.Minute, nil, "service.test")
	cache.Set("101:202", 1.1, time.Minute)
	cache.Set("101:303", 1.2, time.Minute)
	cache.Set("404:202", 1.3, time.Minute)

	invalidateUserGroupRateCacheByUserID(101)
	_, ok := cache.Get("101:202")
	require.False(t, ok)
	_, ok = cache.Get("101:303")
	require.False(t, ok)
	_, ok = cache.Get("404:202")
	require.True(t, ok)

	invalidateUserGroupRateCacheByGroupID(202)
	_, ok = cache.Get("404:202")
	require.False(t, ok)
}

func TestGatewayServiceGetUserGroupRateMultiplier_FallbacksAndUsesExistingResolver(t *testing.T) {
	var nilSvc *GatewayService
	require.Equal(t, 1.3, nilSvc.getUserGroupRateMultiplier(context.Background(), 101, 202, 1.3))

	rate := 1.9
	repo := &userGroupRateResolverRepoStub{rate: &rate}
	resolver := newUserGroupRateResolver(repo, nil, nil, time.Minute, nil, "service.gateway")
	svc := &GatewayService{userGroupRateResolver: resolver}

	got := svc.getUserGroupRateMultiplier(context.Background(), 101, 202, 1.2)
	require.Equal(t, rate, got)
	require.Equal(t, 1, repo.calls)
}

func TestUserGroupRateResolverResolveEffective_UsesUserRateBeforeNewUserRate(t *testing.T) {
	now := time.Now()
	groupID := int64(202)
	userRate := 1.7
	repo := &userGroupRateResolverRepoStub{rate: &userRate}
	resolver := newUserGroupRateResolver(repo, nil, nil, time.Minute, nil, "service.test")

	got, err := resolver.ResolveEffective(context.Background(), &User{
		ID:        101,
		CreatedAt: now.Add(-time.Hour),
	}, &Group{
		ID:                       groupID,
		RateMultiplier:           1.2,
		NewUserRateEnabled:       true,
		NewUserRateMultiplier:    0.8,
		NewUserRateWindowSeconds: int((24 * time.Hour).Seconds()),
	}, 1.2)

	require.NoError(t, err)
	require.Equal(t, userRate, got)
	require.Equal(t, 1, repo.calls)
}

func TestResolveNewUserGroupRateMultiplier(t *testing.T) {
	now := time.Date(2026, 6, 28, 10, 0, 0, 0, time.UTC)
	groupDefault := 1.2

	tests := []struct {
		name  string
		user  *User
		group *Group
		want  float64
	}{
		{
			name: "uses new user rate inside window",
			user: &User{CreatedAt: now.Add(-2 * time.Hour)},
			group: &Group{
				NewUserRateEnabled:       true,
				NewUserRateMultiplier:    0.75,
				NewUserRateWindowSeconds: int((3 * time.Hour).Seconds()),
			},
			want: 0.75,
		},
		{
			name: "falls back to group default after window expires",
			user: &User{CreatedAt: now.Add(-4 * time.Hour)},
			group: &Group{
				NewUserRateEnabled:       true,
				NewUserRateMultiplier:    0.75,
				NewUserRateWindowSeconds: int((3 * time.Hour).Seconds()),
			},
			want: groupDefault,
		},
		{
			name: "disabled falls back to group default",
			user: &User{CreatedAt: now.Add(-time.Hour)},
			group: &Group{
				NewUserRateEnabled:       false,
				NewUserRateMultiplier:    0.75,
				NewUserRateWindowSeconds: int((3 * time.Hour).Seconds()),
			},
			want: groupDefault,
		},
		{
			name: "zero window falls back to group default",
			user: &User{CreatedAt: now.Add(-time.Hour)},
			group: &Group{
				NewUserRateEnabled:       true,
				NewUserRateMultiplier:    0.75,
				NewUserRateWindowSeconds: 0,
			},
			want: groupDefault,
		},
		{
			name: "missing user created time falls back to group default",
			user: &User{},
			group: &Group{
				NewUserRateEnabled:       true,
				NewUserRateMultiplier:    0.75,
				NewUserRateWindowSeconds: int((3 * time.Hour).Seconds()),
			},
			want: groupDefault,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveNewUserGroupRateMultiplier(context.Background(), nil, tt.user, tt.group, groupDefault, now)
			require.NoError(t, err)
			require.Equal(t, tt.want, got.Multiplier)
		})
	}
}

func TestResolveNewUserGroupRateMultiplier_QuotaLimit(t *testing.T) {
	now := time.Date(2026, 6, 28, 10, 0, 0, 0, time.UTC)
	groupDefault := 1.2
	user := &User{ID: 101, CreatedAt: now.Add(-time.Hour)}
	group := &Group{
		ID:                       202,
		NewUserRateEnabled:       true,
		NewUserRateMultiplier:    0.75,
		NewUserRateWindowSeconds: int((3 * time.Hour).Seconds()),
		NewUserRateQuotaUSD:      10,
	}

	t.Run("under quota uses new user rate", func(t *testing.T) {
		usageRepo := &userGroupRateUsageLogRepoStub{sum: 9.99}
		resolver := newUserGroupRateResolver(nil, usageRepo, nil, time.Minute, nil, "service.test")

		got, err := resolveNewUserGroupRateMultiplier(context.Background(), resolver, user, group, groupDefault, now)

		require.NoError(t, err)
		require.Equal(t, 0.75, got.Multiplier)
		require.Equal(t, RateMultiplierSourceNewUserGroup, got.Source)
		require.Equal(t, 1, usageRepo.calls)
	})

	t.Run("at quota falls back to group default", func(t *testing.T) {
		usageRepo := &userGroupRateUsageLogRepoStub{sum: 10}
		resolver := newUserGroupRateResolver(nil, usageRepo, nil, time.Minute, nil, "service.test")

		got, err := resolveNewUserGroupRateMultiplier(context.Background(), resolver, user, group, groupDefault, now)

		require.NoError(t, err)
		require.Equal(t, groupDefault, got.Multiplier)
		require.Equal(t, RateMultiplierSourceGroupDefault, got.Source)
		require.Equal(t, 1, usageRepo.calls)
	})

	t.Run("quota query error returns error", func(t *testing.T) {
		usageRepo := &userGroupRateUsageLogRepoStub{err: errors.New("usage db unavailable")}
		resolver := newUserGroupRateResolver(nil, usageRepo, nil, time.Minute, nil, "service.test")

		got, err := resolveNewUserGroupRateMultiplier(context.Background(), resolver, user, group, groupDefault, now)

		require.Error(t, err)
		require.Contains(t, err.Error(), "get new user group rate quota usage")
		require.Equal(t, effectiveGroupRateResolution{}, got)
		require.Equal(t, 1, usageRepo.calls)
	})
}

func TestResolveNewUserGroupRateMultiplier_QuotaExceededByCurrentOrderUsesNewRateThenFallsBackNextTime(t *testing.T) {
	now := time.Date(2026, 6, 28, 10, 0, 0, 0, time.UTC)
	groupDefault := 1.2
	user := &User{ID: 101, CreatedAt: now.Add(-time.Hour)}
	group := &Group{
		ID:                       202,
		NewUserRateEnabled:       true,
		NewUserRateMultiplier:    0.75,
		NewUserRateWindowSeconds: int((3 * time.Hour).Seconds()),
		NewUserRateQuotaUSD:      10,
	}
	usageRepo := &userGroupRateUsageLogRepoStub{sum: 9.99}
	resolver := newUserGroupRateResolver(nil, usageRepo, nil, time.Minute, nil, "service.test")

	got, err := resolveNewUserGroupRateMultiplier(context.Background(), resolver, user, group, groupDefault, now)
	require.NoError(t, err)
	require.Equal(t, 0.75, got.Multiplier)
	require.Equal(t, RateMultiplierSourceNewUserGroup, got.Source)

	usageRepo.sum = 10.50
	got, err = resolveNewUserGroupRateMultiplier(context.Background(), resolver, user, group, groupDefault, now)
	require.NoError(t, err)
	require.Equal(t, groupDefault, got.Multiplier)
	require.Equal(t, RateMultiplierSourceGroupDefault, got.Source)
	require.Equal(t, 2, usageRepo.calls)
}
