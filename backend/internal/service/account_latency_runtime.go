package service

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	accountRuntimeEWMAAlpha = 0.35

	accountRuntimeSlowTTFTMs   = 8000
	accountRuntimeSevereTTFTMs = 15000

	accountRuntimeSlowDuration   = 30 * time.Second
	accountRuntimeSevereDuration = 60 * time.Second

	accountRuntimeCacheBenefitWindow = 30 * time.Minute
	accountRuntimeStickySlowWaitCap  = 10 * time.Second
	accountRuntimeStickySlowMaxWait  = 1

	accountRuntimePenaltySlowScoreWeight = 120
	accountRuntimePenaltyStrikeWeight    = 35
	accountRuntimePenaltyStrikeMax       = 140
	accountRuntimePenaltyWindowBoost     = 110
	accountRuntimePenaltyMax             = 300

	accountRuntimeStickySeverePenalty = 120
	accountRuntimeStickyExtremeStrike = 3
)

type accountRuntimeStats struct {
	accounts sync.Map
}

type accountRuntimeStat struct {
	slowScoreEWMABits    atomic.Uint64
	slowStrike           atomic.Int64
	penaltyUntilUnixNano atomic.Int64
	lastCacheBenefitUnix atomic.Int64
}

type accountRuntimeStickyDecision struct {
	Bypass         bool
	LimitWait      bool
	CacheProtected bool
	Penalty        int
	SlowScore      float64
	SlowStrike     int64
}

func newAccountRuntimeStats() *accountRuntimeStats {
	return &accountRuntimeStats{}
}

func (s *accountRuntimeStats) loadOrCreate(accountID int64) *accountRuntimeStat {
	if value, ok := s.accounts.Load(accountID); ok {
		stat, _ := value.(*accountRuntimeStat)
		if stat != nil {
			return stat
		}
	}

	stat := &accountRuntimeStat{}
	actual, loaded := s.accounts.LoadOrStore(accountID, stat)
	if !loaded {
		return stat
	}
	existing, _ := actual.(*accountRuntimeStat)
	if existing != nil {
		return existing
	}
	return stat
}

func (s *accountRuntimeStats) report(accountID int64, result *ForwardResult, err error) {
	if s == nil || accountID <= 0 {
		return
	}

	now := time.Now()
	stat := s.loadOrCreate(accountID)

	if result != nil {
		if accountRuntimeHasCacheBenefitUsage(result.Usage) {
			stat.lastCacheBenefitUnix.Store(now.Unix())
		}
		if result.ClientDisconnect {
			return
		}
	}

	if accountRuntimeIsClientCancel(err) {
		return
	}

	slowScore := accountRuntimeSlowScore(result, err)
	updateAccountRuntimeEWMAAtomic(&stat.slowScoreEWMABits, slowScore, accountRuntimeEWMAAlpha)

	switch {
	case slowScore >= 0.9:
		stat.slowStrike.Add(2)
	case slowScore >= 0.5:
		stat.slowStrike.Add(1)
	case slowScore == 0 && err == nil:
		for {
			current := stat.slowStrike.Load()
			if current <= 0 {
				break
			}
			if stat.slowStrike.CompareAndSwap(current, current-1) {
				break
			}
		}
	}

	strike := stat.slowStrike.Load()
	if slowScore >= 0.9 || strike >= 2 {
		stat.penaltyUntilUnixNano.Store(now.Add(accountRuntimePenaltyDuration(strike)).UnixNano())
	}
}

func (s *accountRuntimeStats) loadPenalty(accountID int64) int {
	if s == nil || accountID <= 0 {
		return 0
	}
	value, ok := s.accounts.Load(accountID)
	if !ok {
		return 0
	}
	stat, _ := value.(*accountRuntimeStat)
	if stat == nil {
		return 0
	}

	now := time.Now()
	slowScore := clamp01(math.Float64frombits(stat.slowScoreEWMABits.Load()))
	strike := stat.slowStrike.Load()
	penalty := int(math.Round(slowScore * accountRuntimePenaltySlowScoreWeight))
	if strike > 0 {
		penalty += int(accountRuntimeMinInt64(strike*accountRuntimePenaltyStrikeWeight, accountRuntimePenaltyStrikeMax))
	}
	if until := stat.penaltyUntilUnixNano.Load(); until > now.UnixNano() {
		penalty += accountRuntimePenaltyWindowBoost
	}
	if penalty > accountRuntimePenaltyMax {
		return accountRuntimePenaltyMax
	}
	return penalty
}

func (s *accountRuntimeStats) stickyDecision(accountID int64) accountRuntimeStickyDecision {
	if s == nil || accountID <= 0 {
		return accountRuntimeStickyDecision{}
	}
	value, ok := s.accounts.Load(accountID)
	if !ok {
		return accountRuntimeStickyDecision{}
	}
	stat, _ := value.(*accountRuntimeStat)
	if stat == nil {
		return accountRuntimeStickyDecision{}
	}

	now := time.Now()
	slowScore := clamp01(math.Float64frombits(stat.slowScoreEWMABits.Load()))
	strike := stat.slowStrike.Load()
	penalty := s.loadPenalty(accountID)
	cacheProtected := accountRuntimeHasCacheBenefit(stat, now)
	severeSlow := penalty >= accountRuntimeStickySeverePenalty || slowScore >= 0.7 || strike >= 3
	extremeSlow := strike >= accountRuntimeStickyExtremeStrike

	return accountRuntimeStickyDecision{
		Bypass:         (severeSlow && !cacheProtected) || extremeSlow,
		LimitWait:      severeSlow,
		CacheProtected: cacheProtected,
		Penalty:        penalty,
		SlowScore:      slowScore,
		SlowStrike:     strike,
	}
}

func updateAccountRuntimeEWMAAtomic(target *atomic.Uint64, sample float64, alpha float64) {
	for {
		oldBits := target.Load()
		oldValue := math.Float64frombits(oldBits)
		newValue := sample
		if !math.IsNaN(oldValue) {
			newValue = alpha*sample + (1-alpha)*oldValue
		}
		if target.CompareAndSwap(oldBits, math.Float64bits(newValue)) {
			return
		}
	}
}

func accountRuntimeHasCacheBenefitUsage(usage ClaudeUsage) bool {
	if usage.CacheReadInputTokens > 0 {
		return true
	}
	if usage.CacheCreationInputTokens > 0 || usage.CacheCreation5mTokens > 0 || usage.CacheCreation1hTokens > 0 {
		return true
	}
	return false
}

func accountRuntimeHasCacheBenefit(stat *accountRuntimeStat, now time.Time) bool {
	if stat == nil {
		return false
	}
	lastCacheUnix := stat.lastCacheBenefitUnix.Load()
	if lastCacheUnix <= 0 {
		return false
	}
	return now.Sub(time.Unix(lastCacheUnix, 0)) <= accountRuntimeCacheBenefitWindow
}

func accountRuntimeSlowScore(result *ForwardResult, err error) float64 {
	score := 0.0
	if result != nil {
		if result.FirstTokenMs != nil && *result.FirstTokenMs > 0 {
			score = math.Max(score, accountRuntimeThresholdScore(float64(*result.FirstTokenMs), accountRuntimeSlowTTFTMs, accountRuntimeSevereTTFTMs))
		} else if result.Duration > 0 {
			score = math.Max(score, accountRuntimeThresholdScore(float64(result.Duration), float64(accountRuntimeSlowDuration), float64(accountRuntimeSevereDuration)))
		}
	}
	if err != nil {
		if accountRuntimeIsTimeoutError(err) {
			score = math.Max(score, 1)
		} else {
			score = math.Max(score, 0.6)
		}
	}
	return clamp01(score)
}

func accountRuntimeThresholdScore(sample float64, slow float64, severe float64) float64 {
	if sample <= slow {
		return 0
	}
	if sample >= severe || severe <= slow {
		return 1
	}
	return 0.5 + 0.5*((sample-slow)/(severe-slow))
}

func accountRuntimePenaltyDuration(strike int64) time.Duration {
	switch {
	case strike >= 6:
		return 15 * time.Minute
	case strike >= 4:
		return 10 * time.Minute
	default:
		return 5 * time.Minute
	}
}

func accountRuntimeIsClientCancel(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "client disconnected") || strings.Contains(msg, "client disconnect")
}

func accountRuntimeIsTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	var firstResponseTimeout *upstreamFirstResponseTimeoutError
	if errors.As(err, &firstResponseTimeout) {
		return true
	}
	var failoverErr *UpstreamFailoverError
	if errors.As(err, &failoverErr) {
		body := strings.ToLower(string(failoverErr.ResponseBody))
		return strings.Contains(body, "upstream_timeout") ||
			strings.Contains(body, "did not return a response in time")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "timed out") ||
		strings.Contains(msg, "deadline exceeded")
}

func accountRuntimeMinInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
