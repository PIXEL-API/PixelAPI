package service

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// GrokFreeQuotaPolicy describes the effective local soft-gate policy exposed to
// callers that need to display or inspect the scheduling boundary.
type GrokFreeQuotaPolicy struct {
	Enabled         bool  `json:"enabled"`
	TokenLimit      int64 `json:"token_limit"`
	SoftGatePercent int   `json:"soft_gate_percent"`
	SoftGateTokens  int64 `json:"soft_gate_tokens"`
	WindowHours     int   `json:"window_hours"`
}

type grokFreeQuotaGateSettings struct {
	limitTokens int64
	gateTokens  int64
	window      time.Duration
	cacheTTL    time.Duration
}

type grokFreeQuotaGateCacheEntry struct {
	tokens    int64
	checkedAt time.Time
	known     bool
}

var grokFreeQuotaGateQueryFailureTotal atomic.Int64
var grokFreeQuotaGateBlockedTotal atomic.Int64

func resolveGrokFreeQuotaGateSettings(cfg *config.Config) (grokFreeQuotaGateSettings, bool) {
	if cfg == nil || !cfg.Gateway.Grok.FreeQuotaSoftGateEnabled {
		return grokFreeQuotaGateSettings{}, false
	}
	limit := cfg.Gateway.Grok.FreeQuotaTokenLimit
	percent := cfg.Gateway.Grok.FreeQuotaSoftGatePercent
	windowHours := cfg.Gateway.Grok.FreeQuotaWindowHours
	cacheSeconds := cfg.Gateway.Grok.FreeQuotaStatsCacheSeconds
	if limit <= 0 || percent < 1 || percent > 100 || windowHours <= 0 || cacheSeconds < 0 {
		return grokFreeQuotaGateSettings{}, false
	}
	gate := calculateGrokFreeQuotaSoftGateTokens(limit, percent)
	if gate <= 0 {
		return grokFreeQuotaGateSettings{}, false
	}
	return grokFreeQuotaGateSettings{
		limitTokens: limit,
		gateTokens:  gate,
		window:      time.Duration(windowHours) * time.Hour,
		cacheTTL:    time.Duration(cacheSeconds) * time.Second,
	}, true
}

func calculateGrokFreeQuotaSoftGateTokens(limit int64, percent int) int64 {
	if limit <= 0 || percent <= 0 {
		return 0
	}
	return (limit/100)*int64(percent) + (limit%100)*int64(percent)/100
}

// isExplicitGrokFreeOAuthAccount intentionally accepts only an explicit free
// marker. Blank, basic, missing and inferred tiers must remain schedulable.
func isExplicitGrokFreeOAuthAccount(account *Account) bool {
	if account == nil || !account.IsGrokOAuth() {
		return false
	}
	for _, tier := range []string{
		account.GetCredential("subscription_tier"),
		account.GetCredential("plan_type"),
		account.GetExtraString("subscription_tier"),
		account.GetExtraString("plan_type"),
	} {
		if strings.EqualFold(strings.TrimSpace(tier), "free") {
			return true
		}
	}
	return false
}

var gatewayGrokFreeQuotaGateCache sync.Map
var openaiGrokFreeQuotaGateCache sync.Map

// freeQuotaRefreshInFlight coalesces concurrent background refreshes per cache.
var freeQuotaRefreshInFlight sync.Map // *sync.Map -> *sync.Map (accountID -> struct{})

func (s *GatewayService) filterGrokFreeQuotaAccountsForGateway(ctx context.Context, accounts []Account) []Account {
	if s == nil {
		return accounts
	}
	return filterGrokFreeQuotaAccountsCore(ctx, s.cfg, s.usageLogRepo, &gatewayGrokFreeQuotaGateCache, accounts)
}

func (s *OpenAIGatewayService) filterGrokFreeQuotaAccountsForOpenAI(ctx context.Context, accounts []Account) []Account {
	if s == nil {
		return accounts
	}
	return filterGrokFreeQuotaAccountsCore(ctx, s.cfg, s.usageLogRepo, &openaiGrokFreeQuotaGateCache, accounts)
}

// filterGrokFreeQuotaAccountsCore only reads cached state on the scheduling hot
// path. Missing or stale data fails open and is refreshed asynchronously.
func filterGrokFreeQuotaAccountsCore(
	_ context.Context,
	cfg *config.Config,
	usageLogRepo UsageLogRepository,
	cache *sync.Map,
	accounts []Account,
) []Account {
	if cache == nil {
		return accounts
	}
	settings, enabled := resolveGrokFreeQuotaGateSettings(cfg)
	if !enabled || len(accounts) == 0 || usageLogRepo == nil {
		return accounts
	}

	now := time.Now().UTC()
	tokensByID := make(map[int64]int64)
	missingIDs := make([]int64, 0, len(accounts))
	seenMissing := make(map[int64]struct{})
	for i := range accounts {
		account := &accounts[i]
		if !isExplicitGrokFreeOAuthAccount(account) || account.ID <= 0 {
			continue
		}
		if cached, ok := cache.Load(account.ID); ok {
			entry, valid := cached.(grokFreeQuotaGateCacheEntry)
			if valid {
				age := now.Sub(entry.checkedAt)
				fresh := settings.cacheTTL <= 0 || (age >= 0 && age < settings.cacheTTL)
				if fresh {
					if entry.known {
						tokensByID[account.ID] = entry.tokens
					}
					continue
				}
			}
		}
		if _, exists := seenMissing[account.ID]; !exists {
			seenMissing[account.ID] = struct{}{}
			missingIDs = append(missingIDs, account.ID)
		}
	}

	if len(missingIDs) > 0 {
		scheduleGrokFreeQuotaStatsRefresh(usageLogRepo, cache, settings, missingIDs)
	}

	filtered := make([]Account, 0, len(accounts))
	for i := range accounts {
		account := &accounts[i]
		if isExplicitGrokFreeOAuthAccount(account) {
			if tokens, known := tokensByID[account.ID]; known && tokens >= settings.gateTokens {
				continue
			}
		}
		filtered = append(filtered, *account)
	}
	return filtered
}

func scheduleGrokFreeQuotaStatsRefresh(
	usageLogRepo UsageLogRepository,
	cache *sync.Map,
	settings grokFreeQuotaGateSettings,
	accountIDs []int64,
) {
	if usageLogRepo == nil || cache == nil || len(accountIDs) == 0 {
		return
	}
	inFlightRoot, _ := freeQuotaRefreshInFlight.LoadOrStore(cache, &sync.Map{})
	inFlight, ok := inFlightRoot.(*sync.Map)
	if !ok || inFlight == nil {
		return
	}

	toFetch := make([]int64, 0, len(accountIDs))
	for _, id := range accountIDs {
		if _, loaded := inFlight.LoadOrStore(id, struct{}{}); !loaded {
			toFetch = append(toFetch, id)
		}
	}
	if len(toFetch) == 0 {
		return
	}

	window := settings.window
	gateTokens := settings.gateTokens
	limitTokens := settings.limitTokens
	cacheTTL := settings.cacheTTL
	go func() {
		defer func() {
			for _, id := range toFetch {
				inFlight.Delete(id)
			}
		}()

		now := time.Now().UTC()
		statsByID, err := queryGrokFreeQuotaWindowStats(context.Background(), usageLogRepo, toFetch, now.Add(-window))
		if err != nil {
			grokFreeQuotaGateQueryFailureTotal.Add(1)
			for _, accountID := range toFetch {
				cache.Store(accountID, grokFreeQuotaGateCacheEntry{checkedAt: now})
			}
			slog.Warn("grok_free_quota_soft_gate_stats_failed",
				"account_count", len(toFetch),
				"window_hours", window.Hours(),
				"error", err)
			sweepGrokFreeQuotaGateCache(cache, now, cacheTTL)
			return
		}
		for _, accountID := range toFetch {
			tokens := int64(0)
			if stats := statsByID[accountID]; stats != nil && stats.Tokens > 0 {
				tokens = stats.Tokens
			}
			cache.Store(accountID, grokFreeQuotaGateCacheEntry{tokens: tokens, checkedAt: now, known: true})
			if tokens >= gateTokens {
				grokFreeQuotaGateBlockedTotal.Add(1)
				slog.Info("grok_free_quota_soft_gate_blocked",
					"account_id", accountID,
					"tokens", tokens,
					"gate_tokens", gateTokens,
					"limit_tokens", limitTokens,
					"window_hours", window.Hours())
			}
		}
		sweepGrokFreeQuotaGateCache(cache, now, cacheTTL)
	}()
}

const grokFreeQuotaGateCacheMinSweepAge = 5 * time.Minute

func sweepGrokFreeQuotaGateCache(cache *sync.Map, now time.Time, cacheTTL time.Duration) {
	if cache == nil || cacheTTL <= 0 {
		return
	}
	maxAge := cacheTTL * 20
	if maxAge < grokFreeQuotaGateCacheMinSweepAge {
		maxAge = grokFreeQuotaGateCacheMinSweepAge
	}
	cache.Range(func(key, value any) bool {
		entry, ok := value.(grokFreeQuotaGateCacheEntry)
		if !ok || now.Sub(entry.checkedAt) > maxAge {
			cache.Delete(key)
		}
		return true
	})
}

func queryGrokFreeQuotaWindowStats(ctx context.Context, usageLogRepo UsageLogRepository, accountIDs []int64, start time.Time) (map[int64]*usagestats.AccountStats, error) {
	if usageLogRepo == nil {
		return nil, nil
	}
	if batch, ok := usageLogRepo.(accountWindowStatsBatchReader); ok {
		return batch.GetAccountWindowStatsBatch(ctx, accountIDs, start)
	}
	statsByID := make(map[int64]*usagestats.AccountStats, len(accountIDs))
	for _, accountID := range accountIDs {
		stats, err := usageLogRepo.GetAccountWindowStats(ctx, accountID, start)
		if err != nil {
			return nil, err
		}
		statsByID[accountID] = stats
	}
	return statsByID, nil
}
