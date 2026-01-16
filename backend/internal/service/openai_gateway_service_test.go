package service

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
)

type stubOpenAIAccountRepo struct {
	AccountRepository
	accounts []Account
}

func (r stubOpenAIAccountRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]Account, error) {
	return append([]Account(nil), r.accounts...), nil
}

func (r stubOpenAIAccountRepo) ListSchedulableByPlatform(ctx context.Context, platform string) ([]Account, error) {
	return append([]Account(nil), r.accounts...), nil
}

type stubConcurrencyCache struct {
	ConcurrencyCache
}

func (c stubConcurrencyCache) AcquireAccountSlot(ctx context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error) {
	return true, nil
}

func (c stubConcurrencyCache) ReleaseAccountSlot(ctx context.Context, accountID int64, requestID string) error {
	return nil
}

func (c stubConcurrencyCache) GetAccountsLoadBatch(ctx context.Context, accounts []AccountWithConcurrency) (map[int64]*AccountLoadInfo, error) {
	out := make(map[int64]*AccountLoadInfo, len(accounts))
	for _, acc := range accounts {
		out[acc.ID] = &AccountLoadInfo{AccountID: acc.ID, LoadRate: 0}
	}
	return out, nil
}

func TestOpenAIGatewayService_GenerateSessionHash_Priority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil)

	svc := &OpenAIGatewayService{}

	// 1) session_id header wins
	c.Request.Header.Set("session_id", "sess-123")
	c.Request.Header.Set("conversation_id", "conv-456")
	h1 := svc.GenerateSessionHash(c, map[string]any{"prompt_cache_key": "ses_aaa"})
	if h1 == "" {
		t.Fatalf("expected non-empty hash")
	}

	// 2) conversation_id used when session_id absent
	c.Request.Header.Del("session_id")
	h2 := svc.GenerateSessionHash(c, map[string]any{"prompt_cache_key": "ses_aaa"})
	if h2 == "" {
		t.Fatalf("expected non-empty hash")
	}
	if h1 == h2 {
		t.Fatalf("expected different hashes for different keys")
	}

	// 3) prompt_cache_key used when both headers absent
	c.Request.Header.Del("conversation_id")
	h3 := svc.GenerateSessionHash(c, map[string]any{"prompt_cache_key": "ses_aaa"})
	if h3 == "" {
		t.Fatalf("expected non-empty hash")
	}
	if h2 == h3 {
		t.Fatalf("expected different hashes for different keys")
	}

	// 4) empty when no signals
	h4 := svc.GenerateSessionHash(c, map[string]any{})
	if h4 != "" {
		t.Fatalf("expected empty hash when no signals")
	}
}

func TestOpenAISelectAccountWithLoadAwareness_FiltersUnschedulable(t *testing.T) {
	now := time.Now()
	resetAt := now.Add(10 * time.Minute)
	groupID := int64(1)

	rateLimited := Account{
		ID:               1,
		Platform:         PlatformOpenAI,
		Type:             AccountTypeAPIKey,
		Status:           StatusActive,
		Schedulable:      true,
		Concurrency:      1,
		Priority:         0,
		RateLimitResetAt: &resetAt,
	}
	available := Account{
		ID:          2,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    1,
	}

	svc := &OpenAIGatewayService{
		accountRepo:        stubOpenAIAccountRepo{accounts: []Account{rateLimited, available}},
		concurrencyService: NewConcurrencyService(stubConcurrencyCache{}),
	}

	selection, err := svc.SelectAccountWithLoadAwareness(context.Background(), &groupID, "", "gpt-5.2", nil)
	if err != nil {
		t.Fatalf("SelectAccountWithLoadAwareness error: %v", err)
	}
	if selection == nil || selection.Account == nil {
		t.Fatalf("expected selection with account")
	}
	if selection.Account.ID != available.ID {
		t.Fatalf("expected account %d, got %d", available.ID, selection.Account.ID)
	}
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAISelectAccountWithLoadAwareness_FiltersUnschedulableWhenNoConcurrencyService(t *testing.T) {
	now := time.Now()
	resetAt := now.Add(10 * time.Minute)
	groupID := int64(1)

	rateLimited := Account{
		ID:               1,
		Platform:         PlatformOpenAI,
		Type:             AccountTypeAPIKey,
		Status:           StatusActive,
		Schedulable:      true,
		Concurrency:      1,
		Priority:         0,
		RateLimitResetAt: &resetAt,
	}
	available := Account{
		ID:          2,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    1,
	}

	svc := &OpenAIGatewayService{
		accountRepo: stubOpenAIAccountRepo{accounts: []Account{rateLimited, available}},
		// concurrencyService is nil, forcing the non-load-batch selection path.
	}

	selection, err := svc.SelectAccountWithLoadAwareness(context.Background(), &groupID, "", "gpt-5.2", nil)
	if err != nil {
		t.Fatalf("SelectAccountWithLoadAwareness error: %v", err)
	}
	if selection == nil || selection.Account == nil {
		t.Fatalf("expected selection with account")
	}
	if selection.Account.ID != available.ID {
		t.Fatalf("expected account %d, got %d", available.ID, selection.Account.ID)
	}
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIStreamingTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		Gateway: config.GatewayConfig{
			StreamDataIntervalTimeout: 1,
			StreamKeepaliveInterval:   0,
			MaxLineSize:               defaultMaxLineSize,
		},
	}
	svc := &OpenAIGatewayService{cfg: cfg}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       pr,
		Header:     http.Header{},
	}

	start := time.Now()
	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1}, start, "model", "model")
	_ = pw.Close()
	_ = pr.Close()

	if err == nil || !strings.Contains(err.Error(), "stream data interval timeout") {
		t.Fatalf("expected stream timeout error, got %v", err)
	}
	if !strings.Contains(rec.Body.String(), "stream_timeout") {
		t.Fatalf("expected stream_timeout SSE error, got %q", rec.Body.String())
	}
}

func TestOpenAIStreamingTooLong(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		Gateway: config.GatewayConfig{
			StreamDataIntervalTimeout: 0,
			StreamKeepaliveInterval:   0,
			MaxLineSize:               64 * 1024,
		},
	}
	svc := &OpenAIGatewayService{cfg: cfg}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       pr,
		Header:     http.Header{},
	}

	go func() {
		defer func() { _ = pw.Close() }()
		// 写入超过 MaxLineSize 的单行数据，触发 ErrTooLong
		payload := "data: " + strings.Repeat("a", 128*1024) + "\n"
		_, _ = pw.Write([]byte(payload))
	}()

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 2}, time.Now(), "model", "model")
	_ = pr.Close()

	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("expected ErrTooLong, got %v", err)
	}
	if !strings.Contains(rec.Body.String(), "response_too_large") {
		t.Fatalf("expected response_too_large SSE error, got %q", rec.Body.String())
	}
}

func TestOpenAINonStreamingContentTypePassThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		Security: config.SecurityConfig{
			ResponseHeaders: config.ResponseHeaderConfig{Enabled: false},
		},
	}
	svc := &OpenAIGatewayService{cfg: cfg}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	body := []byte(`{"usage":{"input_tokens":1,"output_tokens":2,"input_tokens_details":{"cached_tokens":0}}}`)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/vnd.test+json"}},
	}

	_, err := svc.handleNonStreamingResponse(c.Request.Context(), resp, c, &Account{}, "model", "model")
	if err != nil {
		t.Fatalf("handleNonStreamingResponse error: %v", err)
	}

	if !strings.Contains(rec.Header().Get("Content-Type"), "application/vnd.test+json") {
		t.Fatalf("expected Content-Type passthrough, got %q", rec.Header().Get("Content-Type"))
	}
}

func TestOpenAINonStreamingContentTypeDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		Security: config.SecurityConfig{
			ResponseHeaders: config.ResponseHeaderConfig{Enabled: false},
		},
	}
	svc := &OpenAIGatewayService{cfg: cfg}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	body := []byte(`{"usage":{"input_tokens":1,"output_tokens":2,"input_tokens_details":{"cached_tokens":0}}}`)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     http.Header{},
	}

	_, err := svc.handleNonStreamingResponse(c.Request.Context(), resp, c, &Account{}, "model", "model")
	if err != nil {
		t.Fatalf("handleNonStreamingResponse error: %v", err)
	}

	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("expected default Content-Type, got %q", rec.Header().Get("Content-Type"))
	}
}

func TestOpenAIStreamingHeadersOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		Security: config.SecurityConfig{
			ResponseHeaders: config.ResponseHeaderConfig{Enabled: false},
		},
		Gateway: config.GatewayConfig{
			StreamDataIntervalTimeout: 0,
			StreamKeepaliveInterval:   0,
			MaxLineSize:               defaultMaxLineSize,
		},
	}
	svc := &OpenAIGatewayService{cfg: cfg}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	pr, pw := io.Pipe()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       pr,
		Header: http.Header{
			"Cache-Control": []string{"upstream"},
			"X-Request-Id":  []string{"req-123"},
			"Content-Type":  []string{"application/custom"},
		},
	}

	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte("data: {}\n\n"))
	}()

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1}, time.Now(), "model", "model")
	_ = pr.Close()
	if err != nil {
		t.Fatalf("handleStreamingResponse error: %v", err)
	}

	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("expected Cache-Control override, got %q", rec.Header().Get("Cache-Control"))
	}
	if rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("expected Content-Type override, got %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("X-Request-Id") != "req-123" {
		t.Fatalf("expected X-Request-Id passthrough, got %q", rec.Header().Get("X-Request-Id"))
	}
}

func TestOpenAIInvalidBaseURLWhenAllowlistDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false},
		},
	}
	svc := &OpenAIGatewayService{cfg: cfg}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	account := &Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"base_url": "://invalid-url"},
	}

	_, err := svc.buildUpstreamRequest(c.Request.Context(), c, account, []byte("{}"), "token", false, "", false)
	if err == nil {
		t.Fatalf("expected error for invalid base_url when allowlist disabled")
	}
}

func TestOpenAIValidateUpstreamBaseURLDisabledRequiresHTTPS(t *testing.T) {
	cfg := &config.Config{
		Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false},
		},
	}
	svc := &OpenAIGatewayService{cfg: cfg}

	if _, err := svc.validateUpstreamBaseURL("http://not-https.example.com"); err == nil {
		t.Fatalf("expected http to be rejected when allow_insecure_http is false")
	}
	normalized, err := svc.validateUpstreamBaseURL("https://example.com")
	if err != nil {
		t.Fatalf("expected https to be allowed when allowlist disabled, got %v", err)
	}
	if normalized != "https://example.com" {
		t.Fatalf("expected raw url passthrough, got %q", normalized)
	}
}

func TestOpenAIValidateUpstreamBaseURLDisabledAllowsHTTP(t *testing.T) {
	cfg := &config.Config{
		Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{
				Enabled:           false,
				AllowInsecureHTTP: true,
			},
		},
	}
	svc := &OpenAIGatewayService{cfg: cfg}

	normalized, err := svc.validateUpstreamBaseURL("http://not-https.example.com")
	if err != nil {
		t.Fatalf("expected http allowed when allow_insecure_http is true, got %v", err)
	}
	if normalized != "http://not-https.example.com" {
		t.Fatalf("expected raw url passthrough, got %q", normalized)
	}
}

func TestOpenAIValidateUpstreamBaseURLEnabledEnforcesAllowlist(t *testing.T) {
	cfg := &config.Config{
		Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{
				Enabled:       true,
				UpstreamHosts: []string{"example.com"},
			},
		},
	}
	svc := &OpenAIGatewayService{cfg: cfg}

	if _, err := svc.validateUpstreamBaseURL("https://example.com"); err != nil {
		t.Fatalf("expected allowlisted host to pass, got %v", err)
	}
	if _, err := svc.validateUpstreamBaseURL("https://evil.com"); err == nil {
		t.Fatalf("expected non-allowlisted host to fail")
	}
}
