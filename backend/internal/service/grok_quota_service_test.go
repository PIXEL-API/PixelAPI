//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

type grokQuotaAccountRepo struct {
	*mockAccountRepoForPlatform
	mu                     sync.Mutex
	updates                map[int64]map[string]any
	credentialErrorCalls   int
	lastCredentialError    string
	lastCredentialSnapshot GrokCredentialMutationSnapshot
	tempUnschedCalls       int
	lastTempUnschedID      int64
	lastTempUnschedUntil   time.Time
	lastTempUnschedReason  string
	rateLimitedCalls       int
	lastRateLimitResetAt   time.Time
}

func (r *grokQuotaAccountRepo) SetGrokCredentialErrorIfMatch(
	_ context.Context,
	_ int64,
	snapshot GrokCredentialMutationSnapshot,
	reason string,
) (bool, error) {
	r.credentialErrorCalls++
	r.lastCredentialSnapshot = snapshot
	r.lastCredentialError = reason
	return true, nil
}

func (r *grokQuotaAccountRepo) SetGrokCredentialTempUnschedulableIfMatch(
	_ context.Context,
	_ int64,
	_ GrokCredentialMutationSnapshot,
	_ time.Time,
	_ string,
) (bool, error) {
	return true, nil
}

func (r *grokQuotaAccountRepo) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updates == nil {
		r.updates = make(map[int64]map[string]any)
	}
	if r.updates[id] == nil {
		r.updates[id] = make(map[string]any)
	}
	for key, value := range updates {
		r.updates[id][key] = value
	}
	return nil
}

func (r *grokQuotaAccountRepo) extraValue(id int64, key string) any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.updates[id][key]
}

func (r *grokQuotaAccountRepo) updateCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.updates)
}

func (r *grokQuotaAccountRepo) SetTempUnschedulable(_ context.Context, id int64, until time.Time, reason string) error {
	r.tempUnschedCalls++
	r.lastTempUnschedID = id
	r.lastTempUnschedUntil = until
	r.lastTempUnschedReason = reason
	return nil
}

func (r *grokQuotaAccountRepo) SetRateLimited(_ context.Context, _ int64, resetAt time.Time) error {
	r.rateLimitedCalls++
	r.lastRateLimitResetAt = resetAt
	return nil
}

func (r *grokQuotaAccountRepo) SetRateLimitedIfLater(ctx context.Context, id int64, resetAt time.Time) error {
	return r.SetRateLimited(ctx, id, resetAt)
}

type grokQuotaProxyRepo struct {
	proxyRepoStub
	proxies map[int64]*Proxy
	calls   int
}

func healthyGrokQuotaOAuthAccount(id int64) *Account {
	return &Account{
		ID:          id,
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  "access-token",
			"refresh_token": "refresh-token",
			"expires_at":    time.Now().Add(2 * grokTokenRefreshSkew).UTC().Format(time.RFC3339),
		},
	}
}

func (r *grokQuotaProxyRepo) GetByID(_ context.Context, id int64) (*Proxy, error) {
	r.calls++
	return r.proxies[id], nil
}

func TestGrokQuotaServiceProbeUsageStoresHeaders(t *testing.T) {
	t.Parallel()

	account := healthyGrokQuotaOAuthAccount(42)
	repo := &grokQuotaAccountRepo{
		mockAccountRepoForPlatform: &mockAccountRepoForPlatform{
			accountsByID: map[int64]*Account{42: account},
		},
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"X-Ratelimit-Limit-Requests":     []string{"10"},
			"X-Ratelimit-Remaining-Requests": []string{"7"},
			"X-Ratelimit-Reset-Requests":     []string{"2000000000"},
			"X-Ratelimit-Limit-Tokens":       []string{"1000"},
			"X-Ratelimit-Remaining-Tokens":   []string{"900"},
		},
		Body: io.NopCloser(strings.NewReader(`{"id":"resp_probe"}`)),
	}}
	svc := NewGrokQuotaService(repo, nil, NewGrokTokenProvider(repo, nil), upstream)

	result, err := svc.ProbeUsage(context.Background(), 42)
	require.NoError(t, err)
	probeReq, probeBody, found := upstream.requestByMethodAndPath(http.MethodPost, "/v1/responses")
	require.True(t, found)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.True(t, result.HeadersObserved)
	require.NotNil(t, result.Snapshot)
	require.True(t, result.Snapshot.HeadersObserved)
	require.Equal(t, "active_probe", result.Snapshot.ObservationSource)
	require.NotEmpty(t, result.Snapshot.LastProbeAt)
	require.NotEmpty(t, result.Snapshot.LastHeadersSeenAt)
	require.NotNil(t, result.Snapshot.Requests)
	require.EqualValues(t, 10, *result.Snapshot.Requests.Limit)
	require.EqualValues(t, 7, *result.Snapshot.Requests.Remaining)
	require.Equal(t, "https://cli-chat-proxy.grok.com/v1/responses", probeReq.URL.String())
	require.Equal(t, "Bearer access-token", probeReq.Header.Get("Authorization"))
	require.JSONEq(t, `{"model":"grok-4.5","input":"hi","stream":true}`, string(probeBody))
	require.Equal(t, "application/json, text/event-stream", probeReq.Header.Get("Accept"))
	require.NotNil(t, repo.extraValue(42, grokQuotaSnapshotExtraKey))
}

func TestGrokQuotaServiceProbeUsageRejectsUnexpectedStatusBeforePersisting(t *testing.T) {
	for _, statusCode := range []int{
		http.StatusContinue,
		http.StatusFound,
		http.StatusNotModified,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			account := healthyGrokQuotaOAuthAccount(142)
			repo := &grokQuotaAccountRepo{
				mockAccountRepoForPlatform: &mockAccountRepoForPlatform{
					accountsByID: map[int64]*Account{142: account},
				},
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: statusCode,
				Header: http.Header{
					"Location":                       []string{"https://attacker.invalid/private"},
					"X-Ratelimit-Limit-Requests":     []string{"999"},
					"X-Ratelimit-Remaining-Requests": []string{"0"},
				},
				Body: io.NopCloser(strings.NewReader(`{"secret":"must-not-be-observed"}`)),
			}}
			svc := NewGrokQuotaService(repo, nil, NewGrokTokenProvider(repo, nil), upstream)

			result, err := svc.ProbeUsage(context.Background(), 142)

			require.Error(t, err)
			require.Nil(t, result)
			require.Equal(t, http.StatusBadGateway, infraerrors.Code(err))
			require.Zero(t, repo.updateCount())
			require.NotNil(t, upstream.lastReq)
			require.True(t, HTTPUpstreamRedirectsDisabled(upstream.lastReq.Context()))
		})
	}
}

func TestGrokQuotaServiceFetchBillingRejectsUnexpectedStatusBeforeParsing(t *testing.T) {
	for _, statusCode := range []int{
		http.StatusContinue,
		http.StatusFound,
		http.StatusNotModified,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			account := healthyGrokQuotaOAuthAccount(143)
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: statusCode,
				Header:     http.Header{"Location": []string{"https://attacker.invalid/private"}},
				Body:       io.NopCloser(strings.NewReader(`{"config":{"plan_name":"forged"}}`)),
			}}
			svc := NewGrokQuotaService(
				&grokQuotaAccountRepo{mockAccountRepoForPlatform: &mockAccountRepoForPlatform{}},
				nil,
				nil,
				upstream,
			)

			summary, gotStatus, err := svc.fetchBilling(
				context.Background(),
				account,
				"access-token",
				"",
				false,
			)

			require.Error(t, err)
			require.Nil(t, summary)
			require.Equal(t, statusCode, gotStatus)
			require.Equal(t, http.StatusBadGateway, infraerrors.Code(err))
			require.NotNil(t, upstream.lastReq)
			require.True(t, HTTPUpstreamRedirectsDisabled(upstream.lastReq.Context()))
		})
	}
}

func TestGrokQuotaServiceProbeUsageLoadsProxyWhenAccountEdgeMissing(t *testing.T) {
	t.Parallel()

	proxyID := int64(7)
	account := healthyGrokQuotaOAuthAccount(46)
	account.ProxyID = &proxyID
	repo := &grokQuotaAccountRepo{
		mockAccountRepoForPlatform: &mockAccountRepoForPlatform{
			accountsByID: map[int64]*Account{46: account},
		},
	}
	proxyRepo := &grokQuotaProxyRepo{
		proxies: map[int64]*Proxy{
			proxyID: {
				ID:       proxyID,
				Protocol: "http",
				Host:     "proxy.test",
				Port:     3128,
			},
		},
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_probe"}`)),
	}}
	svc := NewGrokQuotaService(repo, proxyRepo, NewGrokTokenProvider(repo, nil), upstream)

	_, err := svc.ProbeUsage(context.Background(), 46)
	require.NoError(t, err)
	require.Equal(t, 1, proxyRepo.calls)
	require.Equal(t, "http://proxy.test:3128", upstream.lastProxyURL)
}

func TestGrokQuotaServiceProbeUsageStoresNoHeadersState(t *testing.T) {
	t.Parallel()

	account := healthyGrokQuotaOAuthAccount(45)
	repo := &grokQuotaAccountRepo{
		mockAccountRepoForPlatform: &mockAccountRepoForPlatform{
			accountsByID: map[int64]*Account{45: account},
		},
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_probe"}`)),
	}}
	svc := NewGrokQuotaService(repo, nil, NewGrokTokenProvider(repo, nil), upstream)

	result, err := svc.ProbeUsage(context.Background(), 45)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.False(t, result.HeadersObserved)
	require.NotNil(t, result.Snapshot)
	require.False(t, result.Snapshot.HeadersObserved)
	require.Equal(t, "active_probe", result.Snapshot.ObservationSource)
	require.NotEmpty(t, result.Snapshot.LastProbeAt)
	require.Empty(t, result.Snapshot.LastHeadersSeenAt)

	stored, ok := repo.extraValue(45, grokQuotaSnapshotExtraKey).(*xai.QuotaSnapshot)
	require.True(t, ok)
	require.False(t, stored.HeadersObserved)
	require.Equal(t, http.StatusOK, stored.StatusCode)
}

func TestGrokQuotaServiceProbeUsageReturnsRateLimitedSnapshot(t *testing.T) {
	t.Parallel()

	account := healthyGrokQuotaOAuthAccount(43)
	repo := &grokQuotaAccountRepo{
		mockAccountRepoForPlatform: &mockAccountRepoForPlatform{
			accountsByID: map[int64]*Account{43: account},
		},
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": []string{"45"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"rate limited"}}`)),
	}}
	svc := NewGrokQuotaService(repo, nil, NewGrokTokenProvider(repo, nil), upstream)

	result, err := svc.ProbeUsage(context.Background(), 43)
	require.NoError(t, err)
	require.Equal(t, http.StatusTooManyRequests, result.StatusCode)
	require.NotNil(t, result.Snapshot)
	require.NotNil(t, result.Snapshot.RetryAfterSeconds)
	require.Equal(t, 45, *result.Snapshot.RetryAfterSeconds)
}

func TestGrokQuotaServiceProbeUsagePaymentRequiredMarksAccountError(t *testing.T) {
	t.Parallel()

	account := healthyGrokQuotaOAuthAccount(47)
	repo := &grokQuotaAccountRepo{
		mockAccountRepoForPlatform: &mockAccountRepoForPlatform{
			accountsByID: map[int64]*Account{47: account},
		},
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusPaymentRequired,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"payment required"}}`)),
	}}
	svc := NewGrokQuotaService(repo, nil, NewGrokTokenProvider(repo, nil), upstream)

	_, err := svc.ProbeUsage(context.Background(), 47)

	require.Error(t, err)
	require.Equal(t, 1, repo.credentialErrorCalls)
	require.Equal(t, grokPaymentRequiredErrorMessage, repo.lastCredentialError)
	require.Equal(t, grokCredentialMutationSnapshot(account), repo.lastCredentialSnapshot)
	require.Zero(t, repo.tempUnschedCalls)
}

func TestGrokQuotaServiceResetQuotaUnsupported(t *testing.T) {
	t.Parallel()

	account := &Account{
		ID:       44,
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
	}
	repo := &grokQuotaAccountRepo{
		mockAccountRepoForPlatform: &mockAccountRepoForPlatform{
			accountsByID: map[int64]*Account{44: account},
		},
	}
	svc := NewGrokQuotaService(repo, nil, nil, nil)

	_, err := svc.ResetQuota(context.Background(), 44)
	require.Error(t, err)
	require.Equal(t, http.StatusNotImplemented, infraerrors.Code(err))
	require.Equal(t, "GROK_QUOTA_RESET_UNSUPPORTED", infraerrors.Reason(err))
}

func TestShouldAutoPauseGrokAccountByQuota(t *testing.T) {
	t.Parallel()

	zero := int64(0)
	limit := int64(10)
	resetFuture := time.Now().Add(time.Minute).Unix()
	retryAfter := 30
	tests := []struct {
		name     string
		snapshot xai.QuotaSnapshot
		want     bool
	}{
		{
			name: "remaining requests exhausted",
			snapshot: xai.QuotaSnapshot{
				Requests:  &xai.QuotaWindow{Limit: &limit, Remaining: &zero, ResetUnix: &resetFuture},
				UpdatedAt: time.Now().UTC().Format(time.RFC3339),
			},
			want: true,
		},
		{
			name: "retry after active",
			snapshot: xai.QuotaSnapshot{
				RetryAfterSeconds: &retryAfter,
				UpdatedAt:         time.Now().UTC().Format(time.RFC3339),
			},
			want: true,
		},
		{
			name: "retry after expired",
			snapshot: xai.QuotaSnapshot{
				RetryAfterSeconds: &retryAfter,
				UpdatedAt:         time.Now().Add(-time.Duration(retryAfter+1) * time.Second).UTC().Format(time.RFC3339),
			},
			want: false,
		},
		{
			name: "stale snapshot ignored",
			snapshot: xai.QuotaSnapshot{
				Requests:  &xai.QuotaWindow{Limit: &limit, Remaining: &zero, ResetUnix: &resetFuture},
				UpdatedAt: time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			account := &Account{
				Platform: PlatformGrok,
				Type:     AccountTypeOAuth,
				Extra: map[string]any{
					grokQuotaSnapshotExtraKey: tt.snapshot,
				},
			}
			got, _ := shouldAutoPauseGrokAccountByQuota(account)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestGrokOAuthAccountIsUnschedulableWhileQuotaSnapshotActive(t *testing.T) {
	t.Parallel()

	now := time.Now()
	zero := int64(0)
	limit := int64(10)
	resetFuture := now.Add(time.Minute).Unix()
	account := &Account{
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			grokQuotaSnapshotExtraKey: xai.QuotaSnapshot{
				Requests:  &xai.QuotaWindow{Limit: &limit, Remaining: &zero, ResetUnix: &resetFuture},
				UpdatedAt: now.UTC().Format(time.RFC3339),
			},
		},
	}

	require.False(t, account.IsSchedulableAt(now))
}
