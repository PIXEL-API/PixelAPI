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
	blocked map[string]time.Duration
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

func (c *cyberSessionBlockCacheStub) SetCyberSessionBlocked(ctx context.Context, key string, ttl time.Duration) error {
	if c.blocked == nil {
		c.blocked = map[string]time.Duration{}
	}
	c.blocked[key] = ttl
	return nil
}

func (c *cyberSessionBlockCacheStub) IsCyberSessionBlocked(ctx context.Context, key string) (bool, error) {
	_, ok := c.blocked[key]
	return ok, nil
}

func resetCyberSessionBlockRuntimeCacheForTest(t *testing.T) {
	t.Helper()
	cyberSessionBlockRuntimeSF.Forget("cyber_session_block_runtime")
	cyberSessionBlockRuntimeCache.Store(&cachedCyberSessionBlockRuntime{
		enabled:   false,
		ttl:       time.Hour,
		expiresAt: 0,
	})
	t.Cleanup(func() {
		cyberSessionBlockRuntimeSF.Forget("cyber_session_block_runtime")
		cyberSessionBlockRuntimeCache.Store(&cachedCyberSessionBlockRuntime{
			enabled:   false,
			ttl:       time.Hour,
			expiresAt: 0,
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

func TestCyberSessionBlockKeyUsesOnlyExplicitSessionSignals(t *testing.T) {
	c := cyberSessionBlockTestContext(t, "session_id", "sess-1")
	body := []byte(`{"prompt_cache_key":"body-key","input":"hello"}`)

	key1 := CyberSessionBlockKey(10, c, body)
	key2 := CyberSessionBlockKey(11, c, body)
	require.NotEmpty(t, key1)
	require.NotEqual(t, key1, key2)

	noExplicit := cyberSessionBlockTestContext(t, "", "")
	require.Empty(t, CyberSessionBlockKey(10, noExplicit, []byte(`{"input":"hello"}`)))
}

func TestOpenAIGatewayServiceCyberSessionBlockHonorsRuntimeSwitch(t *testing.T) {
	resetCyberSessionBlockRuntimeCacheForTest(t)

	repo := &cyberSessionBlockSettingRepoStub{values: map[string]string{
		SettingKeyCyberSessionBlockEnabled:    "false",
		SettingKeyCyberSessionBlockTTLSeconds: "2",
	}}
	cache := &cyberSessionBlockCacheStub{}
	svc := &OpenAIGatewayService{
		cache:          cache,
		settingService: NewSettingService(repo, nil),
	}

	svc.MarkCyberSessionBlocked(context.Background(), "session-a")
	require.False(t, svc.IsCyberSessionBlocked(context.Background(), "session-a"))

	repo.values[SettingKeyCyberSessionBlockEnabled] = "true"
	resetCyberSessionBlockRuntimeCacheForTest(t)

	svc.MarkCyberSessionBlocked(context.Background(), "session-a")
	require.True(t, svc.IsCyberSessionBlocked(context.Background(), "session-a"))
	require.Equal(t, 2*time.Second, cache.blocked["session-a"])
}
