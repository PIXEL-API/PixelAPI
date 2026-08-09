package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type cyberSessionBlockSettingRepoStub struct {
	values map[string]string
	err    error
}

func (s *cyberSessionBlockSettingRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	if value, ok := s.values[key]; ok {
		return &Setting{Key: key, Value: value}, nil
	}
	return nil, ErrSettingNotFound
}

func (s *cyberSessionBlockSettingRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	if value, ok := s.values[key]; ok {
		return value, nil
	}
	return "", ErrSettingNotFound
}

func (s *cyberSessionBlockSettingRepoStub) Set(ctx context.Context, key, value string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}

func (s *cyberSessionBlockSettingRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
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

func (s *cyberSessionBlockSettingRepoStub) SetMultiple(ctx context.Context, values map[string]string) error {
	if s.values == nil {
		s.values = map[string]string{}
	}
	for key, value := range values {
		s.values[key] = value
	}
	return nil
}

func (s *cyberSessionBlockSettingRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	result := make(map[string]string, len(s.values))
	for key, value := range s.values {
		result[key] = value
	}
	return result, nil
}

func (s *cyberSessionBlockSettingRepoStub) Delete(ctx context.Context, key string) error {
	delete(s.values, key)
	return nil
}

type cyberSessionBlockCacheStub struct {
	recordCalls    []cyberPolicyIsolationCall
	checkCalls     []cyberPolicyIsolationCall
	recordDecision CyberPolicyHitDecision
	recordErr      error
	checkState     CyberPolicyBlockState
	checkErr       error
}

type cyberPolicyIsolationCall struct {
	apiKeyID         int64
	effectiveGroupID int64
	sessionHash      string
	attemptID        string
}

func (c *cyberSessionBlockCacheStub) GetSessionAccountID(context.Context, int64, string) (int64, error) {
	return 0, errors.New("not found")
}

func (c *cyberSessionBlockCacheStub) SetSessionAccountID(context.Context, int64, string, int64, time.Duration) error {
	return nil
}

func (c *cyberSessionBlockCacheStub) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}

func (c *cyberSessionBlockCacheStub) DeleteSessionAccountID(context.Context, int64, string) error {
	return nil
}

func (c *cyberSessionBlockCacheStub) GetSessionString(context.Context, int64, string) (string, error) {
	return "", ErrGatewaySessionStringNotFound
}

func (c *cyberSessionBlockCacheStub) SetSessionString(context.Context, int64, string, string, time.Duration) error {
	return nil
}

func (c *cyberSessionBlockCacheStub) DeleteSessionString(context.Context, int64, string) error {
	return nil
}

func (c *cyberSessionBlockCacheStub) RecordHit(
	_ context.Context,
	apiKeyID, effectiveGroupID int64,
	sessionHash, upstreamAttemptID string,
) (CyberPolicyHitDecision, error) {
	c.recordCalls = append(c.recordCalls, cyberPolicyIsolationCall{
		apiKeyID:         apiKeyID,
		effectiveGroupID: effectiveGroupID,
		sessionHash:      sessionHash,
		attemptID:        upstreamAttemptID,
	})
	return c.recordDecision, c.recordErr
}

func (c *cyberSessionBlockCacheStub) CheckBlock(
	_ context.Context,
	apiKeyID, effectiveGroupID int64,
	sessionHash string,
) (CyberPolicyBlockState, error) {
	c.checkCalls = append(c.checkCalls, cyberPolicyIsolationCall{
		apiKeyID:         apiKeyID,
		effectiveGroupID: effectiveGroupID,
		sessionHash:      sessionHash,
	})
	return c.checkState, c.checkErr
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

func cyberSessionBlockTestContext(t *testing.T, headerName, headerValue string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	if headerName != "" {
		req.Header.Set(headerName, headerValue)
	}
	c.Request = req
	return c
}

func TestCyberPolicyGroupSessionHashUsesEffectiveGroupAndExplicitSignals(t *testing.T) {
	c := cyberSessionBlockTestContext(t, "session_id", "sess-1")
	body := []byte(`{"prompt_cache_key":"body-key","input":"hello"}`)

	group7 := CyberPolicyGroupSessionHash(10, 7, c, body)
	require.NotEmpty(t, group7)
	require.NotEqual(t, group7, CyberPolicyGroupSessionHash(10, 8, c, body))
	require.NotEqual(t, group7, CyberPolicyGroupSessionHash(11, 7, c, body))

	noExplicit := cyberSessionBlockTestContext(t, "", "")
	require.Empty(t, CyberPolicyGroupSessionHash(10, 7, noExplicit, []byte(`{"input":"hello"}`)))
	require.Empty(t, CyberPolicyGroupSessionHash(10, 0, c, body))

	messagesBody := []byte(`{"metadata":{"user_id":"messages-session"},"messages":[{"role":"user","content":"hello"}]}`)
	require.Empty(t, CyberPolicyGroupSessionHash(10, 7, noExplicit, messagesBody), "metadata.user_id must not become a session on Responses")
	messagesContext := cyberSessionBlockTestContext(t, "", "")
	messagesContext.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	messagesHash := CyberPolicyGroupSessionHash(10, 7, messagesContext, messagesBody)
	require.NotEmpty(t, messagesHash)
	require.NotEqual(t, messagesHash, CyberPolicyGroupSessionHash(10, 8, messagesContext, messagesBody))

	legacyMetadata := "user_a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2_account_550e8400-e29b-41d4-a716-446655440000_session_123e4567-e89b-12d3-a456-426614174000"
	legacyBody := []byte(`{"metadata":{"user_id":"` + legacyMetadata + `"}}`)
	opaqueSessionBody := []byte(`{"metadata":{"user_id":"123e4567-e89b-12d3-a456-426614174000"}}`)
	require.Equal(
		t,
		CyberPolicyGroupSessionHash(10, 7, messagesContext, opaqueSessionBody),
		CyberPolicyGroupSessionHash(10, 7, messagesContext, legacyBody),
		"standard Claude Code metadata must use its parsed session_id component",
	)
}

func TestOpenAIGatewayServiceCyberPolicyIsolationHonorsSelectedGroups(t *testing.T) {
	resetOpenAICyberPolicyRuntimeCacheForTest(t)
	now := time.Date(2026, 8, 10, 12, 5, 0, 0, time.UTC)
	repo := &cyberSessionBlockSettingRepoStub{values: map[string]string{
		SettingKeyCyberSessionBlockEnabled:          "true",
		SettingKeyOpenAICyberPolicyEnforcedGroupIDs: `[7]`,
	}}
	cache := &cyberSessionBlockCacheStub{
		recordDecision: CyberPolicyHitDecision{
			HitSequence:  1,
			Action:       CyberPolicyBlockScopeSession,
			BlockedUntil: now.Add(5 * time.Minute),
		},
		checkState: CyberPolicyBlockState{
			Blocked:      true,
			Scope:        CyberPolicyBlockScopeSession,
			RetryAfter:   5 * time.Minute,
			BlockedUntil: now.Add(5 * time.Minute),
		},
	}
	svc := &OpenAIGatewayService{
		cache:          cache,
		settingService: NewSettingService(repo, nil),
	}

	require.Equal(t, CyberPolicyHitDecision{}, svc.RecordCyberPolicyHit(context.Background(), 10, 8, "s", "attempt-ignored"))
	require.Empty(t, cache.recordCalls)
	require.Equal(t, CyberPolicyBlockState{}, svc.CheckCyberPolicyBlock(context.Background(), 10, 8, "s"))
	require.Empty(t, cache.checkCalls)

	decision := svc.RecordCyberPolicyHit(context.Background(), 10, 7, "s", "attempt-1")
	expectedDecision := cache.recordDecision
	expectedDecision.Enforced = true
	require.Equal(t, expectedDecision, decision)
	require.Equal(t, []cyberPolicyIsolationCall{{
		apiKeyID:         10,
		effectiveGroupID: 7,
		sessionHash:      "s",
		attemptID:        "attempt-1",
	}}, cache.recordCalls)

	state := svc.CheckCyberPolicyBlock(context.Background(), 10, 7, "s")
	require.Equal(t, cache.checkState, state)
	require.Equal(t, []cyberPolicyIsolationCall{{
		apiKeyID:         10,
		effectiveGroupID: 7,
		sessionHash:      "s",
	}}, cache.checkCalls)
}

func TestOpenAIGatewayServiceCyberPolicyIsolationFailsOpenOnStoreError(t *testing.T) {
	resetOpenAICyberPolicyRuntimeCacheForTest(t)
	repo := &cyberSessionBlockSettingRepoStub{values: map[string]string{
		SettingKeyCyberSessionBlockEnabled:          "true",
		SettingKeyOpenAICyberPolicyEnforcedGroupIDs: `[7]`,
	}}
	cache := &cyberSessionBlockCacheStub{
		recordErr: errors.New("record unavailable"),
		checkErr:  errors.New("check unavailable"),
	}
	svc := &OpenAIGatewayService{
		cache:          cache,
		settingService: NewSettingService(repo, nil),
	}

	require.Equal(t, CyberPolicyHitDecision{Enforced: true}, svc.RecordCyberPolicyHit(context.Background(), 10, 7, "", "attempt-1"))
	require.Equal(t, CyberPolicyBlockState{}, svc.CheckCyberPolicyBlock(context.Background(), 10, 7, ""))
}

func TestOpenAIGatewayServiceCyberPolicyIsolationEnforcedWithoutStore(t *testing.T) {
	resetOpenAICyberPolicyRuntimeCacheForTest(t)
	repo := &cyberSessionBlockSettingRepoStub{values: map[string]string{
		SettingKeyCyberSessionBlockEnabled:          "true",
		SettingKeyOpenAICyberPolicyEnforcedGroupIDs: `[7]`,
	}}
	svc := &OpenAIGatewayService{
		settingService: NewSettingService(repo, nil),
	}

	require.Equal(t, CyberPolicyHitDecision{Enforced: true}, svc.RecordCyberPolicyHit(context.Background(), 10, 7, "session", "attempt-1"))
	require.Equal(t, CyberPolicyHitDecision{}, svc.RecordCyberPolicyHit(context.Background(), 10, 8, "session", "attempt-2"))
}

func TestOpenAIGatewayServiceCyberPolicyIsolationHonorsRuntimeSwitch(t *testing.T) {
	resetOpenAICyberPolicyRuntimeCacheForTest(t)

	repo := &cyberSessionBlockSettingRepoStub{values: map[string]string{
		SettingKeyCyberSessionBlockEnabled:          "false",
		SettingKeyOpenAICyberPolicyEnforcedGroupIDs: `[7]`,
	}}
	cache := &cyberSessionBlockCacheStub{}
	svc := &OpenAIGatewayService{
		cache:          cache,
		settingService: NewSettingService(repo, nil),
	}

	group7 := int64(7)
	require.False(t, svc.settingService.IsOpenAICyberPolicyEnforcedGroup(context.Background(), &group7))
	require.Equal(t, CyberPolicyHitDecision{}, svc.RecordCyberPolicyHit(context.Background(), 10, 7, "session-a", "attempt-1"))
	require.Empty(t, cache.recordCalls)

	repo.values[SettingKeyCyberSessionBlockEnabled] = "true"
	resetOpenAICyberPolicyRuntimeCacheForTest(t)

	require.True(t, svc.settingService.IsOpenAICyberPolicyEnforcedGroup(context.Background(), &group7))
	require.True(t, svc.RecordCyberPolicyHit(context.Background(), 10, 7, "session-a", "attempt-2").Enforced)
	require.Len(t, cache.recordCalls, 1)
	group8 := int64(8)
	require.False(t, svc.settingService.IsOpenAICyberPolicyEnforcedGroup(context.Background(), &group8))
	require.False(t, svc.settingService.IsOpenAICyberPolicyEnforcedGroup(context.Background(), nil))
}

func TestSettingServiceOpenAICyberPolicyGroupCacheCopiesConfiguredIDs(t *testing.T) {
	resetOpenAICyberPolicyRuntimeCacheForTest(t)
	svc := NewSettingService(&cyberSessionBlockSettingRepoStub{}, nil)
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
