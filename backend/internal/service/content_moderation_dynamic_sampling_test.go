package service

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestDynamicSamplingAuditResultPreservesCurrentRiskState(t *testing.T) {
	now := time.Now()
	cache := &dynamicSamplingTestHashCache{
		state: &ContentModerationUserTrustState{
			UserID:           7,
			Level:            ContentModerationTrustLevelRiskObserve,
			FlaggedTotal:     1,
			CleanAuditStreak: 0,
			RiskUntil:        now.Add(time.Hour),
			UpdatedAt:        now,
		},
	}
	svc := NewContentModerationService(nil, nil, cache, nil, nil, nil, nil)
	cfg := defaultContentModerationConfig()
	cfg.DynamicSampling = defaultContentModerationDynamicSamplingConfig()
	cfg.DynamicSampling.Enabled = true
	cfg.DynamicSampling.NewUserFullAuditCount = 100

	staleCleanDecision := &ContentModerationDynamicSamplingDecision{
		ShouldAudit: true,
		ContextHash: contentModerationDynamicSamplingContextHash(ContentModerationCheckInput{
			UserID:   7,
			Endpoint: "/v1/chat/completions",
			Protocol: ContentModerationProtocolOpenAIChat,
		}, ContentModerationScopeContext{ScopeType: contentModerationScopeTypeGroup}),
		State: &ContentModerationUserTrustState{
			UserID:           7,
			Level:            ContentModerationTrustLevelTrusted,
			CleanAuditStreak: 100,
			TrustedUntil:     now.Add(time.Hour),
			UpdatedAt:        now.Add(-time.Minute),
		},
	}

	svc.recordDynamicSamplingAuditResult(context.Background(), cfg, ContentModerationCheckInput{
		UserID:    7,
		RequestID: "req-clean",
		Endpoint:  "/v1/chat/completions",
	}, staleCleanDecision, false)

	got, err := cache.GetUserTrustState(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetUserTrustState() error = %v", err)
	}
	if got == nil {
		t.Fatal("expected trust state")
	}
	if got.Level != ContentModerationTrustLevelRiskObserve {
		t.Fatalf("Level = %q, want %q", got.Level, ContentModerationTrustLevelRiskObserve)
	}
	if !got.RiskUntil.After(now) {
		t.Fatalf("RiskUntil = %v, want after %v", got.RiskUntil, now)
	}
	if got.FlaggedTotal != 1 {
		t.Fatalf("FlaggedTotal = %d, want 1", got.FlaggedTotal)
	}
	if got.CleanAuditStreak != 1 {
		t.Fatalf("CleanAuditStreak = %d, want 1", got.CleanAuditStreak)
	}
}

func TestValidateContentModerationDynamicSamplingRejectsInvalidRate(t *testing.T) {
	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	cfg := defaultContentModerationConfig()
	cfg.DynamicSampling.TrustedSampleRate = 0

	if err := svc.validateConfig(context.Background(), cfg); err == nil {
		t.Fatal("validateConfig() error = nil, want invalid dynamic sampling error")
	}
}

type dynamicSamplingTestHashCache struct {
	mu    sync.Mutex
	state *ContentModerationUserTrustState
}

func (c *dynamicSamplingTestHashCache) RecordFlaggedInputHash(context.Context, string) error {
	return nil
}

func (c *dynamicSamplingTestHashCache) HasFlaggedInputHash(context.Context, string) (bool, error) {
	return false, nil
}

func (c *dynamicSamplingTestHashCache) DeleteFlaggedInputHash(context.Context, string) (bool, error) {
	return false, nil
}

func (c *dynamicSamplingTestHashCache) ClearFlaggedInputHashes(context.Context) (int64, error) {
	return 0, nil
}

func (c *dynamicSamplingTestHashCache) CountFlaggedInputHashes(context.Context) (int64, error) {
	return 0, nil
}

func (c *dynamicSamplingTestHashCache) GetUserTrustState(context.Context, int64) (*ContentModerationUserTrustState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return cloneContentModerationUserTrustState(c.state), nil
}

func (c *dynamicSamplingTestHashCache) SetUserTrustState(_ context.Context, _ int64, state *ContentModerationUserTrustState, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = cloneContentModerationUserTrustState(state)
	return nil
}

func (c *dynamicSamplingTestHashCache) UpdateUserTrustState(_ context.Context, _ int64, _ time.Duration, mutate ContentModerationUserTrustStateMutator) (*ContentModerationUserTrustState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	next, err := mutate(cloneContentModerationUserTrustState(c.state))
	if err != nil {
		return nil, err
	}
	if next != nil {
		c.state = cloneContentModerationUserTrustState(next)
	}
	return cloneContentModerationUserTrustState(c.state), nil
}
