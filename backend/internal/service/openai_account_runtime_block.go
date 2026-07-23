package service

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	openAIAccountStateUpdateTimeout    = 5 * time.Second
	openAIStopSchedulingBridgeCooldown = 2 * time.Minute
)

func openAIAccountStateContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	return context.WithTimeout(base, openAIAccountStateUpdateTimeout)
}

func isOpenAIAccountRuntimeBlockable(account *Account) bool {
	return account != nil && (account.Platform == PlatformOpenAI || account.Platform == PlatformGrok)
}

func (s *OpenAIGatewayService) BlockAccountScheduling(account *Account, until time.Time, _ string) {
	if s == nil || !isOpenAIAccountRuntimeBlockable(account) {
		return
	}
	blockUntil := until
	now := time.Now()
	if blockUntil.IsZero() || !blockUntil.After(now) {
		blockUntil = now.Add(openAIStopSchedulingBridgeCooldown)
	}
	s.openaiAccountRuntimeBlockUntil.Store(account.ID, blockUntil)
}

func (s *OpenAIGatewayService) ClearAccountSchedulingBlock(accountID int64) {
	if s == nil || accountID <= 0 {
		return
	}
	s.openaiAccountRuntimeBlockUntil.Delete(accountID)
}

func (s *OpenAIGatewayService) isOpenAIAccountRuntimeBlocked(account *Account) bool {
	if s == nil || !isOpenAIAccountRuntimeBlockable(account) {
		return false
	}
	value, ok := s.openaiAccountRuntimeBlockUntil.Load(account.ID)
	if !ok {
		return false
	}
	until, ok := value.(time.Time)
	if !ok || until.IsZero() {
		s.openaiAccountRuntimeBlockUntil.Delete(account.ID)
		return false
	}
	if time.Now().Before(until) {
		return true
	}
	s.openaiAccountRuntimeBlockUntil.Delete(account.ID)
	return false
}

func (s *OpenAIGatewayService) getOpenAIAccountModelTransientState() *openAIAccountModelTransientState {
	if s == nil {
		return nil
	}
	s.openaiModelTransientOnce.Do(func() {
		if s.openaiModelTransient == nil {
			s.openaiModelTransient = newOpenAIAccountModelTransientState(openAIModelTransientDefaultMax)
		}
	})
	return s.openaiModelTransient
}

func canonicalOpenAIAccountSchedulingModel(account *Account, requestedModel string) string {
	model := strings.TrimSpace(requestedModel)
	if account == nil || model == "" {
		return model
	}
	if mapped := strings.TrimSpace(account.GetMappedModel(model)); mapped != "" {
		return mapped
	}
	return model
}

func (s *OpenAIGatewayService) recordOpenAIAccountModelTransientFailure(account *Account, requestedModel string, now time.Time) openAIAccountModelTransientDecision {
	if s == nil || account == nil {
		return openAIAccountModelTransientDecision{}
	}
	state := s.getOpenAIAccountModelTransientState()
	if state == nil {
		return openAIAccountModelTransientDecision{}
	}
	model := canonicalOpenAIAccountSchedulingModel(account, requestedModel)
	return state.recordFailure(account.ID, model, now)
}

func (s *OpenAIGatewayService) isOpenAIAccountModelRuntimeBlocked(account *Account, requestedModel string) bool {
	if s == nil || account == nil {
		return false
	}
	state := s.getOpenAIAccountModelTransientState()
	if state == nil {
		return false
	}
	model := canonicalOpenAIAccountSchedulingModel(account, requestedModel)
	return state.isBlocked(account.ID, model, time.Now())
}

func (s *OpenAIGatewayService) isOpenAIAccountRequestRuntimeBlocked(account *Account, requestedModel string) bool {
	return s != nil && (s.isOpenAIAccountRuntimeBlocked(account) || s.isOpenAIAccountModelRuntimeBlocked(account, requestedModel))
}

func shouldCooldownOpenAITransientUpstreamError(statusCode int, responseBody []byte) bool {
	switch statusCode {
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, 520, 521, 522, 523, 524:
		return true
	case http.StatusBadRequest:
		return isOpenAITransientProcessingError(statusCode, "", responseBody)
	default:
		return false
	}
}

func (s *OpenAIGatewayService) handleOpenAIAccountUpstreamErrorForModel(
	ctx context.Context,
	account *Account,
	requestedModel string,
	statusCode int,
	headers http.Header,
	responseBody []byte,
) bool {
	if s == nil || account == nil {
		return false
	}
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()

	if account.Platform == PlatformOpenAI && account.Type == AccountTypeAPIKey && strings.TrimSpace(requestedModel) != "" && shouldCooldownOpenAITransientUpstreamError(statusCode, responseBody) {
		decision := s.recordOpenAIAccountModelTransientFailure(account, requestedModel, time.Now())
		if decision.FailureStreak > 0 {
			slog.Warn("openai_model_transient_state",
				"account_id", account.ID,
				"model", canonicalOpenAIAccountSchedulingModel(account, requestedModel),
				"failure_streak", decision.FailureStreak,
				"cooldown_ms", decision.Cooldown.Milliseconds(),
				"block_scope", "account_model",
			)
		}
		return false
	}
	if s.rateLimitService == nil {
		return false
	}
	shouldDisable := s.rateLimitService.HandleUpstreamErrorForModel(stateCtx, account, requestedModel, statusCode, headers, responseBody)
	if shouldDisable {
		s.BlockAccountScheduling(account, time.Time{}, "upstream_disable")
	}
	return shouldDisable
}
