package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type cyberPolicyRestrictionSettingRepoStub struct {
	values map[string]string
	err    error
}

func (s *cyberPolicyRestrictionSettingRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	if value, ok := s.values[key]; ok {
		return &Setting{Key: key, Value: value}, nil
	}
	return nil, ErrSettingNotFound
}

func (s *cyberPolicyRestrictionSettingRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	if value, ok := s.values[key]; ok {
		return value, nil
	}
	return "", ErrSettingNotFound
}

func (s *cyberPolicyRestrictionSettingRepoStub) Set(ctx context.Context, key, value string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}

func (s *cyberPolicyRestrictionSettingRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func (s *cyberPolicyRestrictionSettingRepoStub) SetMultiple(ctx context.Context, values map[string]string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	for key, value := range values {
		s.values[key] = value
	}
	return nil
}

func (s *cyberPolicyRestrictionSettingRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	result := make(map[string]string, len(s.values))
	for key, value := range s.values {
		result[key] = value
	}
	return result, nil
}

func (s *cyberPolicyRestrictionSettingRepoStub) Delete(ctx context.Context, key string) error {
	delete(s.values, key)
	return nil
}

type cyberPolicyRestrictionCacheStub struct {
	recordCalls    []cyberPolicyRestrictionCall
	checkCalls     []cyberPolicyRestrictionCall
	clearCalls     []cyberPolicyRestrictionCall
	recordDecision CyberPolicyHitDecision
	recordErr      error
	checkState     CyberPolicyBlockState
	checkErr       error
	clearRemoved   bool
	clearErr       error
}

type cyberPolicyRestrictionCall struct {
	userID           int64
	effectiveGroupID int64
	attemptID        string
}

func (c *cyberPolicyRestrictionCacheStub) GetSessionAccountID(context.Context, int64, string) (int64, error) {
	return 0, errors.New("not found")
}

func (c *cyberPolicyRestrictionCacheStub) SetSessionAccountID(context.Context, int64, string, int64, time.Duration) error {
	return nil
}

func (c *cyberPolicyRestrictionCacheStub) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}

func (c *cyberPolicyRestrictionCacheStub) DeleteSessionAccountID(context.Context, int64, string) error {
	return nil
}

func (c *cyberPolicyRestrictionCacheStub) GetSessionString(context.Context, int64, string) (string, error) {
	return "", ErrGatewaySessionStringNotFound
}

func (c *cyberPolicyRestrictionCacheStub) SetSessionString(context.Context, int64, string, string, time.Duration) error {
	return nil
}

func (c *cyberPolicyRestrictionCacheStub) DeleteSessionString(context.Context, int64, string) error {
	return nil
}

func (c *cyberPolicyRestrictionCacheStub) RecordHit(
	_ context.Context,
	userID, effectiveGroupID int64,
	upstreamAttemptID string,
) (CyberPolicyHitDecision, error) {
	c.recordCalls = append(c.recordCalls, cyberPolicyRestrictionCall{
		userID:           userID,
		effectiveGroupID: effectiveGroupID,
		attemptID:        upstreamAttemptID,
	})
	return c.recordDecision, c.recordErr
}

func (c *cyberPolicyRestrictionCacheStub) CheckBlock(
	_ context.Context,
	userID, effectiveGroupID int64,
) (CyberPolicyBlockState, error) {
	c.checkCalls = append(c.checkCalls, cyberPolicyRestrictionCall{
		userID:           userID,
		effectiveGroupID: effectiveGroupID,
	})
	return c.checkState, c.checkErr
}

func (c *cyberPolicyRestrictionCacheStub) ClearBlock(
	_ context.Context,
	userID, effectiveGroupID int64,
) (bool, error) {
	c.clearCalls = append(c.clearCalls, cyberPolicyRestrictionCall{
		userID:           userID,
		effectiveGroupID: effectiveGroupID,
	})
	return c.clearRemoved, c.clearErr
}

func resetOpenAICyberPolicyRuntimeCacheForTest(t *testing.T) {
	t.Helper()
	openAICyberPolicyRuntimeSF.Forget("openai_cyber_policy_runtime")
	openAICyberPolicyRuntimeCache.Store(&cachedOpenAICyberPolicyRuntime{
		enabled:          false,
		enforcedGroupIDs: map[int64]struct{}{},
		expiresAt:        0,
	})
	t.Cleanup(func() {
		openAICyberPolicyRuntimeSF.Forget("openai_cyber_policy_runtime")
		openAICyberPolicyRuntimeCache.Store(&cachedOpenAICyberPolicyRuntime{
			enabled:          false,
			enforcedGroupIDs: map[int64]struct{}{},
			expiresAt:        0,
		})
	})
}

func TestOpenAIGatewayServiceCyberPolicyRestrictionHonorsSelectedGroups(t *testing.T) {
	resetOpenAICyberPolicyRuntimeCacheForTest(t)
	now := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	repo := &cyberPolicyRestrictionSettingRepoStub{values: map[string]string{
		SettingKeyCyberSessionBlockEnabled:          "true",
		SettingKeyOpenAICyberPolicyEnforcedGroupIDs: `[7]`,
	}}
	cache := &cyberPolicyRestrictionCacheStub{
		recordDecision: CyberPolicyHitDecision{
			HitSequence:  1,
			Action:       CyberPolicyBlockScopeUserGroupDay,
			BlockedUntil: now,
		},
		checkState: CyberPolicyBlockState{
			Blocked:      true,
			Scope:        CyberPolicyBlockScopeUserGroupDay,
			RetryAfter:   12 * time.Hour,
			BlockedUntil: now,
		},
	}
	svc := &OpenAIGatewayService{
		cache:          cache,
		settingService: NewSettingService(repo, nil),
	}

	require.Equal(t, CyberPolicyHitDecision{}, svc.RecordCyberPolicyHit(context.Background(), 100, 8, "attempt-ignored"))
	require.Empty(t, cache.recordCalls)
	require.Equal(t, CyberPolicyBlockState{}, svc.CheckCyberPolicyBlock(context.Background(), 100, 8))
	require.Empty(t, cache.checkCalls)

	decision := svc.RecordCyberPolicyHit(context.Background(), 100, 7, "attempt-1")
	expectedDecision := cache.recordDecision
	expectedDecision.Enforced = true
	require.Equal(t, expectedDecision, decision)
	require.Equal(t, []cyberPolicyRestrictionCall{{
		userID:           100,
		effectiveGroupID: 7,
		attemptID:        "attempt-1",
	}}, cache.recordCalls)

	state := svc.CheckCyberPolicyBlock(context.Background(), 100, 7)
	require.Equal(t, cache.checkState, state)
	require.Equal(t, []cyberPolicyRestrictionCall{{
		userID:           100,
		effectiveGroupID: 7,
	}}, cache.checkCalls)
}

func TestOpenAIGatewayServiceCyberPolicyRestrictionFailsOpenOnStoreError(t *testing.T) {
	resetOpenAICyberPolicyRuntimeCacheForTest(t)
	repo := &cyberPolicyRestrictionSettingRepoStub{values: map[string]string{
		SettingKeyCyberSessionBlockEnabled:          "true",
		SettingKeyOpenAICyberPolicyEnforcedGroupIDs: `[7]`,
	}}
	cache := &cyberPolicyRestrictionCacheStub{
		recordErr: errors.New("record unavailable"),
		checkErr:  errors.New("check unavailable"),
	}
	svc := &OpenAIGatewayService{
		cache:          cache,
		settingService: NewSettingService(repo, nil),
	}

	require.Equal(t, CyberPolicyHitDecision{Enforced: true}, svc.RecordCyberPolicyHit(context.Background(), 100, 7, "attempt-1"))
	require.Equal(t, CyberPolicyBlockState{}, svc.CheckCyberPolicyBlock(context.Background(), 100, 7))
}

func TestOpenAIGatewayServiceCyberPolicyRestrictionEnforcedWithoutStore(t *testing.T) {
	resetOpenAICyberPolicyRuntimeCacheForTest(t)
	repo := &cyberPolicyRestrictionSettingRepoStub{values: map[string]string{
		SettingKeyCyberSessionBlockEnabled:          "true",
		SettingKeyOpenAICyberPolicyEnforcedGroupIDs: `[7]`,
	}}
	svc := &OpenAIGatewayService{settingService: NewSettingService(repo, nil)}

	require.Equal(t, CyberPolicyHitDecision{Enforced: true}, svc.RecordCyberPolicyHit(context.Background(), 100, 7, "attempt-1"))
	require.Equal(t, CyberPolicyHitDecision{}, svc.RecordCyberPolicyHit(context.Background(), 100, 8, "attempt-2"))
}

func TestOpenAIGatewayServiceCyberPolicyRestrictionHonorsRuntimeSwitch(t *testing.T) {
	resetOpenAICyberPolicyRuntimeCacheForTest(t)
	repo := &cyberPolicyRestrictionSettingRepoStub{values: map[string]string{
		SettingKeyCyberSessionBlockEnabled:          "false",
		SettingKeyOpenAICyberPolicyEnforcedGroupIDs: `[7]`,
	}}
	cache := &cyberPolicyRestrictionCacheStub{}
	svc := &OpenAIGatewayService{
		cache:          cache,
		settingService: NewSettingService(repo, nil),
	}

	group7 := int64(7)
	require.False(t, svc.settingService.IsOpenAICyberPolicyEnforcedGroup(context.Background(), &group7))
	require.Equal(t, CyberPolicyHitDecision{}, svc.RecordCyberPolicyHit(context.Background(), 100, 7, "attempt-1"))
	require.Empty(t, cache.recordCalls)

	repo.values[SettingKeyCyberSessionBlockEnabled] = "true"
	resetOpenAICyberPolicyRuntimeCacheForTest(t)

	require.True(t, svc.settingService.IsOpenAICyberPolicyEnforcedGroup(context.Background(), &group7))
	require.True(t, svc.RecordCyberPolicyHit(context.Background(), 100, 7, "attempt-2").Enforced)
	require.Len(t, cache.recordCalls, 1)
	group8 := int64(8)
	require.False(t, svc.settingService.IsOpenAICyberPolicyEnforcedGroup(context.Background(), &group8))
	require.False(t, svc.settingService.IsOpenAICyberPolicyEnforcedGroup(context.Background(), nil))
}

func TestOpenAIGatewayServiceCyberPolicyAdminOperationsFailFast(t *testing.T) {
	expectedState := CyberPolicyBlockState{
		Blocked:      true,
		Scope:        CyberPolicyBlockScopeUserGroupDay,
		RetryAfter:   time.Hour,
		BlockedUntil: time.Now().Add(time.Hour),
	}
	cache := &cyberPolicyRestrictionCacheStub{
		checkState:   expectedState,
		clearRemoved: true,
	}
	svc := &OpenAIGatewayService{cache: cache}

	state, err := svc.GetCyberPolicyRestriction(context.Background(), 200, 9)
	require.NoError(t, err)
	require.Equal(t, expectedState, state)
	removed, err := svc.ClearCyberPolicyRestriction(context.Background(), 200, 9)
	require.NoError(t, err)
	require.True(t, removed)
	require.Equal(t, []cyberPolicyRestrictionCall{{userID: 200, effectiveGroupID: 9}}, cache.checkCalls)
	require.Equal(t, []cyberPolicyRestrictionCall{{userID: 200, effectiveGroupID: 9}}, cache.clearCalls)

	cache.checkErr = errors.New("check down")
	cache.clearErr = errors.New("clear down")
	_, err = svc.GetCyberPolicyRestriction(context.Background(), 200, 9)
	require.ErrorContains(t, err, "get cyber policy restriction")
	_, err = svc.ClearCyberPolicyRestriction(context.Background(), 200, 9)
	require.ErrorContains(t, err, "clear cyber policy restriction")

	missingStore := &OpenAIGatewayService{}
	_, err = missingStore.GetCyberPolicyRestriction(context.Background(), 200, 9)
	require.ErrorContains(t, err, "store is unavailable")
	_, err = missingStore.ClearCyberPolicyRestriction(context.Background(), 200, 9)
	require.ErrorContains(t, err, "store is unavailable")
}

func TestSettingServiceOpenAICyberPolicyGroupCacheCopiesConfiguredIDs(t *testing.T) {
	resetOpenAICyberPolicyRuntimeCacheForTest(t)
	svc := NewSettingService(&cyberPolicyRestrictionSettingRepoStub{}, nil)
	settings := &SystemSettings{
		CyberSessionBlockEnabled:          true,
		OpenAICyberPolicyEnforcedGroupIDs: []int64{7},
	}

	svc.refreshCachedSettings(settings)
	settings.OpenAICyberPolicyEnforcedGroupIDs[0] = 8
	group7 := int64(7)
	group8 := int64(8)

	require.True(t, svc.IsOpenAICyberPolicyEnforcedGroup(context.Background(), &group7))
	require.False(t, svc.IsOpenAICyberPolicyEnforcedGroup(context.Background(), &group8))
}
