//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/stretchr/testify/require"
)

type stubAntigravityUpstream struct {
	firstBase  string
	secondBase string
	calls      []string
}

func (s *stubAntigravityUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	url := req.URL.String()
	s.calls = append(s.calls, url)
	if strings.HasPrefix(url, s.firstBase) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"Resource has been exhausted"}}`)),
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("ok")),
	}, nil
}

func (s *stubAntigravityUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, enableTLSFingerprint bool) (*http.Response, error) {
	return s.Do(req, proxyURL, accountID, accountConcurrency)
}

type scopeLimitCall struct {
	accountID int64
	scope     AntigravityQuotaScope
	resetAt   time.Time
}

type rateLimitCall struct {
	accountID int64
	resetAt   time.Time
}

type stubAntigravityAccountRepo struct {
	AccountRepository
	scopeCalls []scopeLimitCall
	rateCalls  []rateLimitCall
}

func (s *stubAntigravityAccountRepo) SetAntigravityQuotaScopeLimit(ctx context.Context, id int64, scope AntigravityQuotaScope, resetAt time.Time) error {
	s.scopeCalls = append(s.scopeCalls, scopeLimitCall{accountID: id, scope: scope, resetAt: resetAt})
	return nil
}

func (s *stubAntigravityAccountRepo) SetRateLimited(ctx context.Context, id int64, resetAt time.Time) error {
	s.rateCalls = append(s.rateCalls, rateLimitCall{accountID: id, resetAt: resetAt})
	return nil
}

func TestAntigravityRetryLoop_URLFallback_UsesLatestSuccess(t *testing.T) {
	oldBaseURLs := append([]string(nil), antigravity.BaseURLs...)
	oldAvailability := antigravity.DefaultURLAvailability
	defer func() {
		antigravity.BaseURLs = oldBaseURLs
		antigravity.DefaultURLAvailability = oldAvailability
	}()

	base1 := "https://ag-1.test"
	base2 := "https://ag-2.test"
	antigravity.BaseURLs = []string{base1, base2}
	antigravity.DefaultURLAvailability = antigravity.NewURLAvailability(time.Minute)

	upstream := &stubAntigravityUpstream{firstBase: base1, secondBase: base2}
	account := &Account{
		ID:          1,
		Name:        "acc-1",
		Platform:    PlatformAntigravity,
		Schedulable: true,
		Status:      StatusActive,
		Concurrency: 1,
	}

	var handleErrorCalled bool
	result, err := antigravityRetryLoop(antigravityRetryLoopParams{
		prefix:      "[test]",
		ctx:         context.Background(),
		account:     account,
		proxyURL:    "",
		accessToken: "token",
		action:      "generateContent",
		body:        []byte(`{"input":"test"}`),
		quotaScope:  AntigravityQuotaScopeClaude,
		httpUpstream: upstream,
		handleError: func(ctx context.Context, prefix string, account *Account, statusCode int, headers http.Header, body []byte, quotaScope AntigravityQuotaScope) {
			handleErrorCalled = true
		},
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.resp)
	defer func() { _ = result.resp.Body.Close() }()
	require.Equal(t, http.StatusOK, result.resp.StatusCode)
	require.False(t, handleErrorCalled)
	require.Len(t, upstream.calls, 2)
	require.True(t, strings.HasPrefix(upstream.calls[0], base1))
	require.True(t, strings.HasPrefix(upstream.calls[1], base2))

	available := antigravity.DefaultURLAvailability.GetAvailableURLs()
	require.NotEmpty(t, available)
	require.Equal(t, base2, available[0])
}

func TestAntigravityHandleUpstreamError_UsesScopeLimitWhenEnabled(t *testing.T) {
	t.Setenv(antigravityScopeRateLimitEnv, "true")
	repo := &stubAntigravityAccountRepo{}
	svc := &AntigravityGatewayService{accountRepo: repo}
	account := &Account{ID: 9, Name: "acc-9", Platform: PlatformAntigravity}

	body := buildGeminiRateLimitBody("3s")
	svc.handleUpstreamError(context.Background(), "[test]", account, http.StatusTooManyRequests, http.Header{}, body, AntigravityQuotaScopeClaude)

	require.Len(t, repo.scopeCalls, 1)
	require.Empty(t, repo.rateCalls)
	call := repo.scopeCalls[0]
	require.Equal(t, account.ID, call.accountID)
	require.Equal(t, AntigravityQuotaScopeClaude, call.scope)
	require.WithinDuration(t, time.Now().Add(3*time.Second), call.resetAt, 2*time.Second)
}

func TestAntigravityHandleUpstreamError_UsesAccountLimitWhenScopeDisabled(t *testing.T) {
	t.Setenv(antigravityScopeRateLimitEnv, "false")
	repo := &stubAntigravityAccountRepo{}
	svc := &AntigravityGatewayService{accountRepo: repo}
	account := &Account{ID: 10, Name: "acc-10", Platform: PlatformAntigravity}

	body := buildGeminiRateLimitBody("2s")
	svc.handleUpstreamError(context.Background(), "[test]", account, http.StatusTooManyRequests, http.Header{}, body, AntigravityQuotaScopeClaude)

	require.Len(t, repo.rateCalls, 1)
	require.Empty(t, repo.scopeCalls)
	call := repo.rateCalls[0]
	require.Equal(t, account.ID, call.accountID)
	require.WithinDuration(t, time.Now().Add(2*time.Second), call.resetAt, 2*time.Second)
}

func TestAccountIsSchedulableForModel_AntigravityRateLimits(t *testing.T) {
	now := time.Now()
	future := now.Add(10 * time.Minute)

	account := &Account{
		ID:          1,
		Name:        "acc",
		Platform:    PlatformAntigravity,
		Status:      StatusActive,
		Schedulable: true,
	}

	account.RateLimitResetAt = &future
	require.False(t, account.IsSchedulableForModel("claude-sonnet-4-5"))
	require.False(t, account.IsSchedulableForModel("gemini-3-flash"))

	account.RateLimitResetAt = nil
	account.Extra = map[string]any{
		antigravityQuotaScopesKey: map[string]any{
			"claude": map[string]any{
				"rate_limit_reset_at": future.Format(time.RFC3339),
			},
		},
	}

	require.False(t, account.IsSchedulableForModel("claude-sonnet-4-5"))
	require.True(t, account.IsSchedulableForModel("gemini-3-flash"))
}

func buildGeminiRateLimitBody(delay string) []byte {
	return []byte(fmt.Sprintf(`{"error":{"message":"too many requests","details":[{"metadata":{"quotaResetDelay":%q}}]}}`, delay))
}
