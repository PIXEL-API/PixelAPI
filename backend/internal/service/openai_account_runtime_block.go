package service

import (
	"context"
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
