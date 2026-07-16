package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	gocache "github.com/patrickmn/go-cache"
	"golang.org/x/sync/singleflight"
)

var userGroupRateCacheRegistry sync.Map

type userGroupRateResolver struct {
	repo         UserGroupRateRepository
	usageLogRepo UsageLogRepository
	cache        *gocache.Cache
	cacheTTL     time.Duration
	sf           *singleflight.Group
	logComponent string
}

type userGroupRateResolution struct {
	multiplier  float64
	hasUserRate bool
}

type effectiveGroupRateResolution struct {
	Multiplier float64
	Source     string
}

func newUserGroupRateResolver(repo UserGroupRateRepository, usageLogRepo UsageLogRepository, cache *gocache.Cache, cacheTTL time.Duration, sf *singleflight.Group, logComponent string) *userGroupRateResolver {
	if cacheTTL <= 0 {
		cacheTTL = defaultUserGroupRateCacheTTL
	}
	if cache == nil {
		cache = gocache.New(cacheTTL, time.Minute)
	}
	if logComponent == "" {
		logComponent = "service.gateway"
	}
	if sf == nil {
		sf = &singleflight.Group{}
	}
	if cache != nil {
		userGroupRateCacheRegistry.Store(cache, struct{}{})
	}

	return &userGroupRateResolver{
		repo:         repo,
		usageLogRepo: usageLogRepo,
		cache:        cache,
		cacheTTL:     cacheTTL,
		sf:           sf,
		logComponent: logComponent,
	}
}

func (r *userGroupRateResolver) Resolve(ctx context.Context, userID, groupID int64, groupDefaultMultiplier float64) float64 {
	resolution, ok := r.resolveUserRate(ctx, userID, groupID)
	if ok && resolution.hasUserRate {
		return resolution.multiplier
	}
	return groupDefaultMultiplier
}

func (r *userGroupRateResolver) ResolveEffective(ctx context.Context, user *User, group *Group, groupDefaultMultiplier float64) (float64, error) {
	resolution, err := r.ResolveEffectiveDetailed(ctx, user, group, groupDefaultMultiplier)
	if err != nil {
		return 0, err
	}
	return resolution.Multiplier, nil
}

func (r *userGroupRateResolver) ResolveEffectiveDetailed(ctx context.Context, user *User, group *Group, groupDefaultMultiplier float64) (effectiveGroupRateResolution, error) {
	source := RateMultiplierSourceGroupDefault
	if group != nil {
		groupDefaultMultiplier = group.RateMultiplier
	}
	if user == nil || group == nil || user.ID <= 0 || group.ID <= 0 {
		return resolveNewUserGroupRateMultiplier(ctx, r, user, group, groupDefaultMultiplier, time.Now())
	}
	resolution, ok := r.resolveUserRate(ctx, user.ID, group.ID)
	if ok && resolution.hasUserRate {
		return effectiveGroupRateResolution{Multiplier: resolution.multiplier, Source: RateMultiplierSourceUserGroup}, nil
	}
	if !ok && r != nil && r.repo != nil {
		return effectiveGroupRateResolution{Multiplier: groupDefaultMultiplier, Source: source}, nil
	}
	return resolveNewUserGroupRateMultiplier(ctx, r, user, group, groupDefaultMultiplier, time.Now())
}

func (r *userGroupRateResolver) resolveUserRate(ctx context.Context, userID, groupID int64) (userGroupRateResolution, bool) {
	if r == nil || userID <= 0 || groupID <= 0 {
		return userGroupRateResolution{}, false
	}

	key := fmt.Sprintf("%d:%d", userID, groupID)
	if r.cache != nil {
		if cached, ok := r.cache.Get(key); ok {
			if resolution, castOK := cached.(userGroupRateResolution); castOK {
				userGroupRateCacheHitTotal.Add(1)
				return resolution, true
			}
			if multiplier, castOK := cached.(float64); castOK {
				userGroupRateCacheHitTotal.Add(1)
				return userGroupRateResolution{multiplier: multiplier, hasUserRate: true}, true
			}
		}
	}
	if r.repo == nil {
		return userGroupRateResolution{}, false
	}
	userGroupRateCacheMissTotal.Add(1)

	value, err, shared := r.sf.Do(key, func() (any, error) {
		if r.cache != nil {
			if cached, ok := r.cache.Get(key); ok {
				if resolution, castOK := cached.(userGroupRateResolution); castOK {
					userGroupRateCacheHitTotal.Add(1)
					return resolution, nil
				}
				if multiplier, castOK := cached.(float64); castOK {
					userGroupRateCacheHitTotal.Add(1)
					return userGroupRateResolution{multiplier: multiplier, hasUserRate: true}, nil
				}
			}
		}

		userGroupRateCacheLoadTotal.Add(1)
		userRate, repoErr := r.repo.GetByUserAndGroup(ctx, userID, groupID)
		if repoErr != nil {
			return nil, repoErr
		}

		resolution := userGroupRateResolution{}
		if userRate != nil {
			resolution.multiplier = *userRate
			resolution.hasUserRate = true
		}
		if r.cache != nil {
			r.cache.Set(key, resolution, r.cacheTTL)
		}
		return resolution, nil
	})
	if shared {
		userGroupRateCacheSFSharedTotal.Add(1)
	}
	if err != nil {
		userGroupRateCacheFallbackTotal.Add(1)
		logger.LegacyPrintf(r.logComponent, "get user group rate failed, fallback to group default: user=%d group=%d err=%v", userID, groupID, err)
		return userGroupRateResolution{}, false
	}

	resolution, ok := value.(userGroupRateResolution)
	if !ok {
		userGroupRateCacheFallbackTotal.Add(1)
		return userGroupRateResolution{}, false
	}
	return resolution, true
}

func resolveNewUserGroupRateMultiplier(ctx context.Context, resolver *userGroupRateResolver, user *User, group *Group, groupDefaultMultiplier float64, now time.Time) (effectiveGroupRateResolution, error) {
	defaultResolution := effectiveGroupRateResolution{Multiplier: groupDefaultMultiplier, Source: RateMultiplierSourceGroupDefault}
	if user == nil || group == nil {
		return defaultResolution, nil
	}
	if !group.NewUserRateEnabled || group.NewUserRateMultiplier <= 0 || group.NewUserRateWindowSeconds <= 0 {
		return defaultResolution, nil
	}
	if user.CreatedAt.IsZero() {
		return defaultResolution, nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	expiresAt := user.CreatedAt.Add(time.Duration(group.NewUserRateWindowSeconds) * time.Second)
	if now.After(expiresAt) {
		return defaultResolution, nil
	}
	if group.NewUserRateQuotaUSD > 0 {
		if resolver == nil || resolver.usageLogRepo == nil {
			return effectiveGroupRateResolution{}, fmt.Errorf("new user group rate quota usage repository is nil: user=%d group=%d", user.ID, group.ID)
		}
		used, err := resolver.usageLogRepo.SumUserGroupRateSourceActualCost(
			ctx,
			user.ID,
			group.ID,
			RateMultiplierSourceNewUserGroup,
			user.CreatedAt,
			expiresAt,
		)
		if err != nil {
			logger.LegacyPrintf(resolver.logComponent, "get new user group rate quota usage failed: user=%d group=%d err=%v", user.ID, group.ID, err)
			return effectiveGroupRateResolution{}, fmt.Errorf("get new user group rate quota usage: user=%d group=%d: %w", user.ID, group.ID, err)
		}
		if used >= group.NewUserRateQuotaUSD {
			return defaultResolution, nil
		}
	}
	return effectiveGroupRateResolution{Multiplier: group.NewUserRateMultiplier, Source: RateMultiplierSourceNewUserGroup}, nil
}

func invalidateUserGroupRateCacheByUserID(userID int64) {
	if userID <= 0 {
		return
	}
	prefix := strconv.FormatInt(userID, 10) + ":"
	invalidateUserGroupRateCacheEntries(func(key string) bool {
		return strings.HasPrefix(key, prefix)
	})
}

func invalidateUserGroupRateCacheByGroupID(groupID int64) {
	if groupID <= 0 {
		return
	}
	suffix := ":" + strconv.FormatInt(groupID, 10)
	invalidateUserGroupRateCacheEntries(func(key string) bool {
		return strings.HasSuffix(key, suffix)
	})
}

func invalidateUserGroupRateCacheEntries(match func(string) bool) {
	if match == nil {
		return
	}
	userGroupRateCacheRegistry.Range(func(cacheKey, _ any) bool {
		cache, ok := cacheKey.(*gocache.Cache)
		if !ok || cache == nil {
			return true
		}
		for key := range cache.Items() {
			if match(key) {
				cache.Delete(key)
			}
		}
		return true
	})
}
