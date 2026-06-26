package service

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	ContentModerationTrustLevelNew         = "new"
	ContentModerationTrustLevelTrusted     = "trusted"
	ContentModerationTrustLevelHighTrusted = "high_trusted"
	ContentModerationTrustLevelRiskObserve = "risk_observe"

	defaultDynamicSamplingNewUserFullAuditCount = 100
	defaultDynamicSamplingTrustedSampleRate     = 5
	defaultDynamicSamplingTrustedTTLHours       = 4
	defaultDynamicSamplingHighTrustedRate       = 2
	defaultDynamicSamplingHighTrustedMinHours   = 24
	defaultDynamicSamplingHighTrustedRequests   = 500
	defaultDynamicSamplingHighTrustedAudits     = 10
	defaultDynamicSamplingRiskTTLHours          = 48
	defaultDynamicSamplingMinAuditRequests      = 200
	defaultDynamicSamplingMinAuditMinutes       = 30
	defaultDynamicSamplingLargeTextRunes        = 3000

	maxDynamicSamplingNewUserFullAuditCount = 10000
	maxDynamicSamplingRate                  = 100
	maxDynamicSamplingTTLHours              = 8760
	maxDynamicSamplingTrustedRequests       = 1000000
	maxDynamicSamplingTrustedAudits         = 100000
	maxDynamicSamplingMinAuditRequests      = 10000
	maxDynamicSamplingMinAuditMinutes       = 1440
	maxDynamicSamplingContextHashes         = 64
	minDynamicSamplingStateTTL              = 30 * 24 * time.Hour
)

type ContentModerationDynamicSamplingConfig struct {
	Enabled                bool `json:"enabled"`
	NewUserFullAuditCount  int  `json:"new_user_full_audit_count"`
	TrustedSampleRate      int  `json:"trusted_sample_rate"`
	TrustedTTLHours        int  `json:"trusted_ttl_hours"`
	HighTrustedSampleRate  int  `json:"high_trusted_sample_rate"`
	HighTrustedMinHours    int  `json:"high_trusted_min_hours"`
	HighTrustedMinRequests int  `json:"high_trusted_min_requests"`
	HighTrustedMinAudits   int  `json:"high_trusted_min_audits"`
	RiskFullAuditTTLHours  int  `json:"risk_full_audit_ttl_hours"`
	MinAuditEveryRequests  int  `json:"min_audit_every_requests"`
	MinAuditEveryMinutes   int  `json:"min_audit_every_minutes"`
	LargeTextRunes         int  `json:"large_text_runes"`
}

type ContentModerationDynamicSamplingRuntimeStatus struct {
	Enabled    bool  `json:"enabled"`
	Skipped    int64 `json:"skipped"`
	Forced     int64 `json:"forced"`
	Sampled    int64 `json:"sampled"`
	Audited    int64 `json:"audited"`
	RiskEvents int64 `json:"risk_events"`
}

type ContentModerationUserTrustState struct {
	UserID                 int64     `json:"user_id"`
	Level                  string    `json:"level"`
	CleanAuditStreak       int       `json:"clean_audit_streak"`
	AuditedTotal           int64     `json:"audited_total"`
	FlaggedTotal           int64     `json:"flagged_total"`
	TrustedStartedAt       time.Time `json:"trusted_started_at,omitempty"`
	TrustedUntil           time.Time `json:"trusted_until,omitempty"`
	TrustedRequestCount    int64     `json:"trusted_request_count"`
	TrustedAuditCount      int64     `json:"trusted_audit_count"`
	RequestsSinceLastAudit int64     `json:"requests_since_last_audit"`
	LastAuditAt            time.Time `json:"last_audit_at,omitempty"`
	RiskUntil              time.Time `json:"risk_until,omitempty"`
	KnownContextHashes     []string  `json:"known_context_hashes,omitempty"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type ContentModerationUserTrustStateMutator func(current *ContentModerationUserTrustState) (*ContentModerationUserTrustState, error)

type ContentModerationDynamicSamplingDecision struct {
	ShouldAudit         bool
	EffectiveSampleRate int
	TrustLevel          string
	Reason              string
	Forced              bool
	ContextHash         string
	State               *ContentModerationUserTrustState
}

func defaultContentModerationDynamicSamplingConfig() ContentModerationDynamicSamplingConfig {
	return ContentModerationDynamicSamplingConfig{
		Enabled:                false,
		NewUserFullAuditCount:  defaultDynamicSamplingNewUserFullAuditCount,
		TrustedSampleRate:      defaultDynamicSamplingTrustedSampleRate,
		TrustedTTLHours:        defaultDynamicSamplingTrustedTTLHours,
		HighTrustedSampleRate:  defaultDynamicSamplingHighTrustedRate,
		HighTrustedMinHours:    defaultDynamicSamplingHighTrustedMinHours,
		HighTrustedMinRequests: defaultDynamicSamplingHighTrustedRequests,
		HighTrustedMinAudits:   defaultDynamicSamplingHighTrustedAudits,
		RiskFullAuditTTLHours:  defaultDynamicSamplingRiskTTLHours,
		MinAuditEveryRequests:  defaultDynamicSamplingMinAuditRequests,
		MinAuditEveryMinutes:   defaultDynamicSamplingMinAuditMinutes,
		LargeTextRunes:         defaultDynamicSamplingLargeTextRunes,
	}
}

func (cfg *ContentModerationDynamicSamplingConfig) normalize() {
	if cfg == nil {
		return
	}
	if cfg.NewUserFullAuditCount <= 0 {
		cfg.NewUserFullAuditCount = defaultDynamicSamplingNewUserFullAuditCount
	}
	if cfg.NewUserFullAuditCount > maxDynamicSamplingNewUserFullAuditCount {
		cfg.NewUserFullAuditCount = maxDynamicSamplingNewUserFullAuditCount
	}
	cfg.TrustedSampleRate = normalizeDynamicSamplingRate(cfg.TrustedSampleRate, defaultDynamicSamplingTrustedSampleRate)
	if cfg.TrustedTTLHours <= 0 {
		cfg.TrustedTTLHours = defaultDynamicSamplingTrustedTTLHours
	}
	if cfg.TrustedTTLHours > maxDynamicSamplingTTLHours {
		cfg.TrustedTTLHours = maxDynamicSamplingTTLHours
	}
	cfg.HighTrustedSampleRate = normalizeDynamicSamplingRate(cfg.HighTrustedSampleRate, defaultDynamicSamplingHighTrustedRate)
	if cfg.HighTrustedMinHours <= 0 {
		cfg.HighTrustedMinHours = defaultDynamicSamplingHighTrustedMinHours
	}
	if cfg.HighTrustedMinHours > maxDynamicSamplingTTLHours {
		cfg.HighTrustedMinHours = maxDynamicSamplingTTLHours
	}
	if cfg.HighTrustedMinRequests <= 0 {
		cfg.HighTrustedMinRequests = defaultDynamicSamplingHighTrustedRequests
	}
	if cfg.HighTrustedMinRequests > maxDynamicSamplingTrustedRequests {
		cfg.HighTrustedMinRequests = maxDynamicSamplingTrustedRequests
	}
	if cfg.HighTrustedMinAudits <= 0 {
		cfg.HighTrustedMinAudits = defaultDynamicSamplingHighTrustedAudits
	}
	if cfg.HighTrustedMinAudits > maxDynamicSamplingTrustedAudits {
		cfg.HighTrustedMinAudits = maxDynamicSamplingTrustedAudits
	}
	if cfg.RiskFullAuditTTLHours <= 0 {
		cfg.RiskFullAuditTTLHours = defaultDynamicSamplingRiskTTLHours
	}
	if cfg.RiskFullAuditTTLHours > maxDynamicSamplingTTLHours {
		cfg.RiskFullAuditTTLHours = maxDynamicSamplingTTLHours
	}
	if cfg.MinAuditEveryRequests <= 0 {
		cfg.MinAuditEveryRequests = defaultDynamicSamplingMinAuditRequests
	}
	if cfg.MinAuditEveryRequests > maxDynamicSamplingMinAuditRequests {
		cfg.MinAuditEveryRequests = maxDynamicSamplingMinAuditRequests
	}
	if cfg.MinAuditEveryMinutes <= 0 {
		cfg.MinAuditEveryMinutes = defaultDynamicSamplingMinAuditMinutes
	}
	if cfg.MinAuditEveryMinutes > maxDynamicSamplingMinAuditMinutes {
		cfg.MinAuditEveryMinutes = maxDynamicSamplingMinAuditMinutes
	}
	if cfg.LargeTextRunes <= 0 {
		cfg.LargeTextRunes = defaultDynamicSamplingLargeTextRunes
	}
	if cfg.LargeTextRunes > maxModerationInputRunes {
		cfg.LargeTextRunes = maxModerationInputRunes
	}
}

func normalizeDynamicSamplingRate(value int, fallback int) int {
	if value <= 0 {
		value = fallback
	}
	if value > maxDynamicSamplingRate {
		value = maxDynamicSamplingRate
	}
	return value
}

func validateContentModerationDynamicSamplingConfig(cfg ContentModerationDynamicSamplingConfig) error {
	if err := validateDynamicSamplingInt("new_user_full_audit_count", cfg.NewUserFullAuditCount, maxDynamicSamplingNewUserFullAuditCount); err != nil {
		return err
	}
	if err := validateDynamicSamplingInt("trusted_sample_rate", cfg.TrustedSampleRate, maxDynamicSamplingRate); err != nil {
		return err
	}
	if err := validateDynamicSamplingInt("trusted_ttl_hours", cfg.TrustedTTLHours, maxDynamicSamplingTTLHours); err != nil {
		return err
	}
	if err := validateDynamicSamplingInt("high_trusted_sample_rate", cfg.HighTrustedSampleRate, maxDynamicSamplingRate); err != nil {
		return err
	}
	if err := validateDynamicSamplingInt("high_trusted_min_hours", cfg.HighTrustedMinHours, maxDynamicSamplingTTLHours); err != nil {
		return err
	}
	if err := validateDynamicSamplingInt("high_trusted_min_requests", cfg.HighTrustedMinRequests, maxDynamicSamplingTrustedRequests); err != nil {
		return err
	}
	if err := validateDynamicSamplingInt("high_trusted_min_audits", cfg.HighTrustedMinAudits, maxDynamicSamplingTrustedAudits); err != nil {
		return err
	}
	if err := validateDynamicSamplingInt("risk_full_audit_ttl_hours", cfg.RiskFullAuditTTLHours, maxDynamicSamplingTTLHours); err != nil {
		return err
	}
	if err := validateDynamicSamplingInt("min_audit_every_requests", cfg.MinAuditEveryRequests, maxDynamicSamplingMinAuditRequests); err != nil {
		return err
	}
	if err := validateDynamicSamplingInt("min_audit_every_minutes", cfg.MinAuditEveryMinutes, maxDynamicSamplingMinAuditMinutes); err != nil {
		return err
	}
	if err := validateDynamicSamplingInt("large_text_runes", cfg.LargeTextRunes, maxModerationInputRunes); err != nil {
		return err
	}
	return nil
}

func validateDynamicSamplingInt(field string, value int, max int) error {
	if value <= 0 || value > max {
		return infraerrors.BadRequest(
			"INVALID_CONTENT_MODERATION_DYNAMIC_SAMPLING",
			fmt.Sprintf("动态采样参数 %s 必须在 1-%d 之间", field, max),
		)
	}
	return nil
}

func (s *ContentModerationService) resolveDynamicSamplingDecision(
	ctx context.Context,
	cfg *ContentModerationConfig,
	input ContentModerationCheckInput,
	content ContentModerationInput,
	scopeCtx ContentModerationScopeContext,
	hashText string,
) (*ContentModerationDynamicSamplingDecision, error) {
	if cfg == nil || !cfg.DynamicSampling.Enabled {
		return nil, nil
	}
	samplingCfg := cfg.DynamicSampling
	samplingCfg.normalize()
	contextHash := contentModerationDynamicSamplingContextHash(input, scopeCtx)
	if input.UserID <= 0 {
		s.dynamicSamplingForced.Add(1)
		return contentModerationDynamicSamplingForcedDecision(nil, contextHash, "anonymous_user"), nil
	}
	if s.hashCache == nil {
		s.dynamicSamplingForced.Add(1)
		return contentModerationDynamicSamplingForcedDecision(nil, contextHash, "cache_unavailable"), nil
	}
	var decision *ContentModerationDynamicSamplingDecision
	_, err := s.updateDynamicSamplingState(ctx, &samplingCfg, input.UserID, func(current *ContentModerationUserTrustState) (*ContentModerationUserTrustState, error) {
		now := time.Now()
		state := cloneContentModerationUserTrustState(current)
		if state == nil {
			state = &ContentModerationUserTrustState{
				UserID: input.UserID,
				Level:  ContentModerationTrustLevelNew,
			}
		}
		state.normalize(input.UserID, now)
		state.RequestsSinceLastAudit++
		if state.isTrustedLevel() {
			state.TrustedRequestCount++
		}
		state.UpdatedAt = now

		level := state.effectiveLevel(now)
		reason := contentModerationDynamicSamplingForceReason(samplingCfg, content, scopeCtx, contextHash, state, level, now)
		if reason != "" {
			state.Level = contentModerationDynamicSamplingNextLevelAfterForcedAudit(level)
			decision = contentModerationDynamicSamplingForcedDecision(state, contextHash, reason)
			return state, nil
		}

		rate := samplingCfg.TrustedSampleRate
		if level == ContentModerationTrustLevelHighTrusted {
			rate = samplingCfg.HighTrustedSampleRate
		}
		decision = &ContentModerationDynamicSamplingDecision{
			ShouldAudit:         contentModerationDynamicSamplingShouldSample(input, hashText, state, rate),
			EffectiveSampleRate: rate,
			TrustLevel:          level,
			Reason:              "trusted_sample_rate",
			ContextHash:         contextHash,
			State:               cloneContentModerationUserTrustState(state),
		}
		if level == ContentModerationTrustLevelHighTrusted {
			decision.Reason = "high_trusted_sample_rate"
		}
		return state, nil
	})
	if err != nil {
		return nil, err
	}
	if decision == nil {
		return nil, fmt.Errorf("resolve dynamic sampling decision: empty decision")
	}
	if decision.Forced {
		s.dynamicSamplingForced.Add(1)
		return decision, nil
	}
	if decision.ShouldAudit {
		s.dynamicSamplingSampled.Add(1)
	} else {
		s.dynamicSamplingSkipped.Add(1)
	}
	return decision, nil
}

func contentModerationDynamicSamplingForceReason(
	cfg ContentModerationDynamicSamplingConfig,
	content ContentModerationInput,
	scopeCtx ContentModerationScopeContext,
	contextHash string,
	state *ContentModerationUserTrustState,
	level string,
	now time.Time,
) string {
	if len(content.Images) > 0 {
		return "image_input"
	}
	if cfg.LargeTextRunes > 0 && len([]rune(content.Text)) > cfg.LargeTextRunes {
		return "large_text"
	}
	if scopeCtx.ScopeType == contentModerationScopeTypeAccountShareMode {
		return "account_share_mode"
	}
	if state.RiskUntil.After(now) {
		return "risk_observe_window"
	}
	if state.CleanAuditStreak < cfg.NewUserFullAuditCount {
		return "new_user_full_audit"
	}
	if !state.hasKnownContextHash(contextHash) {
		return "new_context"
	}
	if state.RequestsSinceLastAudit >= int64(cfg.MinAuditEveryRequests) {
		return "min_audit_request_interval"
	}
	if state.LastAuditAt.IsZero() || state.LastAuditAt.Add(time.Duration(cfg.MinAuditEveryMinutes)*time.Minute).Before(now) {
		return "min_audit_time_interval"
	}
	if (level == ContentModerationTrustLevelTrusted || level == ContentModerationTrustLevelHighTrusted) && state.TrustedUntil.Before(now) {
		return "trusted_window_expired"
	}
	return ""
}

func contentModerationDynamicSamplingNextLevelAfterForcedAudit(level string) string {
	if level == ContentModerationTrustLevelRiskObserve {
		return ContentModerationTrustLevelRiskObserve
	}
	if level == ContentModerationTrustLevelHighTrusted {
		return ContentModerationTrustLevelHighTrusted
	}
	if level == ContentModerationTrustLevelTrusted {
		return ContentModerationTrustLevelTrusted
	}
	return ContentModerationTrustLevelNew
}

func contentModerationDynamicSamplingForcedDecision(state *ContentModerationUserTrustState, contextHash string, reason string) *ContentModerationDynamicSamplingDecision {
	level := ContentModerationTrustLevelNew
	if state != nil {
		level = state.Level
	}
	return &ContentModerationDynamicSamplingDecision{
		ShouldAudit:         true,
		EffectiveSampleRate: 100,
		TrustLevel:          level,
		Reason:              reason,
		Forced:              true,
		ContextHash:         contextHash,
		State:               state,
	}
}

func (s *ContentModerationService) recordDynamicSamplingAuditResult(
	ctx context.Context,
	cfg *ContentModerationConfig,
	input ContentModerationCheckInput,
	decision *ContentModerationDynamicSamplingDecision,
	flagged bool,
) {
	if s == nil || cfg == nil || !cfg.DynamicSampling.Enabled || s.hashCache == nil || input.UserID <= 0 || decision == nil {
		return
	}
	samplingCfg := cfg.DynamicSampling
	samplingCfg.normalize()
	riskEvent := false
	_, err := s.updateDynamicSamplingState(ctx, &samplingCfg, input.UserID, func(current *ContentModerationUserTrustState) (*ContentModerationUserTrustState, error) {
		now := time.Now()
		state := cloneContentModerationUserTrustState(current)
		if state == nil {
			state = cloneContentModerationUserTrustState(decision.State)
		}
		if state == nil {
			state = &ContentModerationUserTrustState{UserID: input.UserID, Level: ContentModerationTrustLevelNew}
		}
		state.normalize(input.UserID, now)
		state.AuditedTotal++
		state.LastAuditAt = now
		state.RequestsSinceLastAudit = 0
		state.UpdatedAt = now
		if flagged {
			state.FlaggedTotal++
			state.CleanAuditStreak = 0
			state.Level = ContentModerationTrustLevelRiskObserve
			state.RiskUntil = now.Add(time.Duration(samplingCfg.RiskFullAuditTTLHours) * time.Hour)
			state.TrustedStartedAt = time.Time{}
			state.TrustedUntil = time.Time{}
			state.TrustedRequestCount = 0
			state.TrustedAuditCount = 0
			riskEvent = true
			return state, nil
		}

		state.CleanAuditStreak++
		state.addKnownContextHash(decision.ContextHash)
		if state.CleanAuditStreak >= samplingCfg.NewUserFullAuditCount {
			if state.TrustedStartedAt.IsZero() {
				state.TrustedStartedAt = now
				state.TrustedRequestCount = 0
				state.TrustedAuditCount = 0
			}
			state.TrustedUntil = now.Add(time.Duration(samplingCfg.TrustedTTLHours) * time.Hour)
			state.TrustedAuditCount++
			state.Level = ContentModerationTrustLevelTrusted
			if contentModerationDynamicSamplingQualifiesHighTrusted(samplingCfg, state, now) {
				state.Level = ContentModerationTrustLevelHighTrusted
			}
			state.RiskUntil = time.Time{}
			return state, nil
		}
		if state.RiskUntil.After(now) {
			state.Level = ContentModerationTrustLevelRiskObserve
		} else {
			state.Level = ContentModerationTrustLevelNew
		}
		return state, nil
	})
	if err != nil {
		slog.Warn("content_moderation.dynamic_sampling_state_save_failed",
			"user_id", input.UserID,
			"request_id", input.RequestID,
			"endpoint", input.Endpoint,
			"error", err)
	} else if riskEvent {
		s.dynamicSamplingRiskEvents.Add(1)
	}
	s.dynamicSamplingAudited.Add(1)
}

func (s *ContentModerationService) recordDynamicSamplingAuditError(
	ctx context.Context,
	cfg *ContentModerationConfig,
	input ContentModerationCheckInput,
	decision *ContentModerationDynamicSamplingDecision,
) {
	if s == nil || cfg == nil || !cfg.DynamicSampling.Enabled || s.hashCache == nil || input.UserID <= 0 || decision == nil || decision.State == nil {
		return
	}
	samplingCfg := cfg.DynamicSampling
	samplingCfg.normalize()
	_, err := s.updateDynamicSamplingState(ctx, &samplingCfg, input.UserID, func(current *ContentModerationUserTrustState) (*ContentModerationUserTrustState, error) {
		now := time.Now()
		state := cloneContentModerationUserTrustState(current)
		if state == nil {
			state = cloneContentModerationUserTrustState(decision.State)
		}
		if state == nil {
			return nil, nil
		}
		state.normalize(input.UserID, now)
		state.UpdatedAt = now
		return state, nil
	})
	if err != nil {
		slog.Warn("content_moderation.dynamic_sampling_state_save_failed",
			"user_id", input.UserID,
			"request_id", input.RequestID,
			"endpoint", input.Endpoint,
			"error", err)
	}
	s.dynamicSamplingAudited.Add(1)
}

func (s *ContentModerationService) recordDynamicSamplingRiskEvent(ctx context.Context, cfg *ContentModerationConfig, input ContentModerationCheckInput, reason string) {
	if s == nil || cfg == nil || !cfg.DynamicSampling.Enabled || s.hashCache == nil || input.UserID <= 0 {
		return
	}
	samplingCfg := cfg.DynamicSampling
	samplingCfg.normalize()
	_, err := s.updateDynamicSamplingState(ctx, &samplingCfg, input.UserID, func(current *ContentModerationUserTrustState) (*ContentModerationUserTrustState, error) {
		now := time.Now()
		state := cloneContentModerationUserTrustState(current)
		if state == nil {
			state = &ContentModerationUserTrustState{UserID: input.UserID}
		}
		state.normalize(input.UserID, now)
		state.FlaggedTotal++
		state.CleanAuditStreak = 0
		state.Level = ContentModerationTrustLevelRiskObserve
		state.RiskUntil = now.Add(time.Duration(samplingCfg.RiskFullAuditTTLHours) * time.Hour)
		state.TrustedStartedAt = time.Time{}
		state.TrustedUntil = time.Time{}
		state.TrustedRequestCount = 0
		state.TrustedAuditCount = 0
		state.UpdatedAt = now
		return state, nil
	})
	if err != nil {
		slog.Warn("content_moderation.dynamic_sampling_state_save_failed",
			"user_id", input.UserID,
			"request_id", input.RequestID,
			"endpoint", input.Endpoint,
			"reason", reason,
			"error", err)
		return
	}
	s.dynamicSamplingRiskEvents.Add(1)
}

func (s *ContentModerationService) updateDynamicSamplingState(ctx context.Context, cfg *ContentModerationDynamicSamplingConfig, userID int64, mutate ContentModerationUserTrustStateMutator) (*ContentModerationUserTrustState, error) {
	if s == nil || s.hashCache == nil || userID <= 0 || mutate == nil {
		return nil, nil
	}
	return s.hashCache.UpdateUserTrustState(ctx, userID, contentModerationDynamicSamplingStateTTL(cfg), mutate)
}

func contentModerationDynamicSamplingStateTTL(cfg *ContentModerationDynamicSamplingConfig) time.Duration {
	if cfg == nil {
		return minDynamicSamplingStateTTL
	}
	local := *cfg
	local.normalize()
	required := time.Duration(local.RiskFullAuditTTLHours+local.HighTrustedMinHours+local.TrustedTTLHours+24) * time.Hour
	if required < minDynamicSamplingStateTTL {
		return minDynamicSamplingStateTTL
	}
	return required
}

func contentModerationDynamicSamplingShouldSample(input ContentModerationCheckInput, hashText string, state *ContentModerationUserTrustState, rate int) bool {
	if rate >= 100 {
		return true
	}
	if rate <= 0 {
		return false
	}
	seed := fmt.Sprintf("%d:%s:%s:%d:%d", input.UserID, input.RequestID, hashText, state.AuditedTotal, state.RequestsSinceLastAudit)
	digest := sha256.Sum256([]byte(seed))
	return int(binary.BigEndian.Uint16(digest[:2])%100) < rate
}

func contentModerationDynamicSamplingContextHash(input ContentModerationCheckInput, scopeCtx ContentModerationScopeContext) string {
	parts := []string{
		strings.TrimSpace(input.Endpoint),
		strings.TrimSpace(input.Provider),
		strings.TrimSpace(input.Model),
		strings.TrimSpace(input.Protocol),
		strings.TrimSpace(scopeCtx.ScopeType),
	}
	if input.GroupID != nil {
		parts = append(parts, fmt.Sprintf("group:%d", *input.GroupID))
	}
	if scopeCtx.AccountShareListingID != nil {
		parts = append(parts, fmt.Sprintf("listing:%d", *scopeCtx.AccountShareListingID))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func contentModerationDynamicSamplingQualifiesHighTrusted(cfg ContentModerationDynamicSamplingConfig, state *ContentModerationUserTrustState, now time.Time) bool {
	if state == nil || state.TrustedStartedAt.IsZero() {
		return false
	}
	if state.FlaggedTotal > 0 && state.RiskUntil.After(now) {
		return false
	}
	if state.TrustedStartedAt.Add(time.Duration(cfg.HighTrustedMinHours) * time.Hour).After(now) {
		return false
	}
	return state.TrustedRequestCount >= int64(cfg.HighTrustedMinRequests) && state.TrustedAuditCount >= int64(cfg.HighTrustedMinAudits)
}

func cloneContentModerationUserTrustState(state *ContentModerationUserTrustState) *ContentModerationUserTrustState {
	if state == nil {
		return nil
	}
	out := *state
	if len(state.KnownContextHashes) > 0 {
		out.KnownContextHashes = append([]string(nil), state.KnownContextHashes...)
	}
	return &out
}

func (state *ContentModerationUserTrustState) normalize(userID int64, now time.Time) {
	if state == nil {
		return
	}
	state.UserID = userID
	switch state.Level {
	case ContentModerationTrustLevelNew, ContentModerationTrustLevelTrusted, ContentModerationTrustLevelHighTrusted, ContentModerationTrustLevelRiskObserve:
	default:
		state.Level = ContentModerationTrustLevelNew
	}
	if state.CleanAuditStreak < 0 {
		state.CleanAuditStreak = 0
	}
	if state.AuditedTotal < 0 {
		state.AuditedTotal = 0
	}
	if state.FlaggedTotal < 0 {
		state.FlaggedTotal = 0
	}
	if state.TrustedRequestCount < 0 {
		state.TrustedRequestCount = 0
	}
	if state.TrustedAuditCount < 0 {
		state.TrustedAuditCount = 0
	}
	if state.RequestsSinceLastAudit < 0 {
		state.RequestsSinceLastAudit = 0
	}
	state.KnownContextHashes = normalizeDynamicSamplingContextHashes(state.KnownContextHashes)
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = now
	}
}

func (state *ContentModerationUserTrustState) effectiveLevel(now time.Time) string {
	if state == nil {
		return ContentModerationTrustLevelNew
	}
	if state.RiskUntil.After(now) {
		return ContentModerationTrustLevelRiskObserve
	}
	switch state.Level {
	case ContentModerationTrustLevelTrusted, ContentModerationTrustLevelHighTrusted:
		return state.Level
	default:
		return ContentModerationTrustLevelNew
	}
}

func (state *ContentModerationUserTrustState) isTrustedLevel() bool {
	if state == nil {
		return false
	}
	return state.Level == ContentModerationTrustLevelTrusted || state.Level == ContentModerationTrustLevelHighTrusted
}

func (state *ContentModerationUserTrustState) hasKnownContextHash(hash string) bool {
	if state == nil || strings.TrimSpace(hash) == "" {
		return false
	}
	for _, item := range state.KnownContextHashes {
		if item == hash {
			return true
		}
	}
	return false
}

func (state *ContentModerationUserTrustState) addKnownContextHash(hash string) {
	if state == nil || strings.TrimSpace(hash) == "" || state.hasKnownContextHash(hash) {
		return
	}
	state.KnownContextHashes = append([]string{hash}, state.KnownContextHashes...)
	if len(state.KnownContextHashes) > maxDynamicSamplingContextHashes {
		state.KnownContextHashes = state.KnownContextHashes[:maxDynamicSamplingContextHashes]
	}
}

func normalizeDynamicSamplingContextHashes(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if len(item) != sha256.Size*2 {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
		if len(out) >= maxDynamicSamplingContextHashes {
			break
		}
	}
	return out
}
