package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type qwenQuotaTestRepo struct {
	cnBalanceProbeRepo
	limitedUntil []time.Time
}

func (r *qwenQuotaTestRepo) SetRateLimited(_ context.Context, _ int64, until time.Time) error {
	r.limitedUntil = append(r.limitedUntil, until)
	return nil
}

func TestQwenCodingLimitsAreSeparateFromTransientRateLimits(t *testing.T) {
	for _, tt := range []struct{ message, window string }{
		{"hour allocated quota exceeded", "5h"}, {"week allocated quota exceeded", "weekly"},
		{"month allocated quota exceeded", "monthly"}, {"concurrency allocated quota exceeded", ""},
		{"usage allocated quota exceeded", ""}, {"Too Many Requests", ""},
	} {
		t.Run(tt.message, func(t *testing.T) {
			account := newQwenTestAccount(AccountModeCoding, APIProtocolChatCompletions)
			repo := &qwenQuotaTestRepo{}
			svc := &RateLimitService{accountRepo: repo}
			body, err := json.Marshal(map[string]any{"error": map[string]string{"message": tt.message}})
			require.NoError(t, err)
			require.False(t, svc.applyCNProviderReactive429(context.Background(), account, nil, body))
			require.Empty(t, repo.limitedUntil, "unknown reset must not be invented")
			if tt.window == "" {
				require.Empty(t, repo.extraWrites)
			} else {
				require.Len(t, repo.extraWrites, 1)
				require.Len(t, repo.extraWrites[0], 1, "one window must not overwrite other windows")
				observation := repo.extraWrites[0][qwenCodingLimitKey(tt.window)].(QwenCodingLimitObservation)
				require.Equal(t, tt.window, observation.Window)
				require.NotEmpty(t, observation.ObservedAt)
				require.Empty(t, observation.RetryAt)
			}
		})
	}
}

func TestQwenRetryAfterAndPayGBillingIsolation(t *testing.T) {
	for _, mode := range []string{AccountModePayG, AccountModeCoding} {
		account := newQwenTestAccount(mode, APIProtocolAnthropic)
		repo := &qwenQuotaTestRepo{}
		svc := &RateLimitService{accountRepo: repo}
		before := time.Now()
		require.True(t, svc.applyCNProviderReactive429(context.Background(), account, http.Header{"Retry-After": []string{"120"}}, []byte(`{"error":{"message":"week allocated quota exceeded"}}`)))
		require.Len(t, repo.limitedUntil, 1)
		require.WithinDuration(t, before.Add(120*time.Second), repo.limitedUntil[0], 2*time.Second)
		require.Equal(t, mode, account.GetAccountMode())
		if mode == AccountModePayG {
			require.Empty(t, repo.extraWrites)
		} else {
			require.Len(t, repo.extraWrites, 1)
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	until := now.Add(time.Hour)
	require.Equal(t, &until, qwenRetryAfter(http.Header{"Retry-After": []string{until.Format(http.TimeFormat)}}, now))
	for _, invalid := range []string{"-2", "invalid", "999999999999999999"} {
		require.Nil(t, qwenRetryAfter(http.Header{"Retry-After": []string{invalid}}, now))
	}
}

func TestQwenQuotaQueryReturnsObservationsWithoutClaimingRemainingQuota(t *testing.T) {
	account := newQwenTestAccount(AccountModeCoding, APIProtocolAdaptive)
	account.Extra[qwenCodingLimitKey("monthly")] = map[string]any{"window": "monthly", "observed_at": "2026-09-11T00:00:00Z"}
	repo := &cnBalanceProbeRepo{account: account}
	// The HTTP stub contains no usable response: a remote probe here would fail.
	svc := NewCNProviderQuotaService(repo, nil, &httpUpstreamRecorder{}, nil)
	result, err := svc.QueryUsage(context.Background(), account.ID)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.NotNil(t, result.AutomaticQuerySupported)
	require.False(t, *result.AutomaticQuerySupported)
	require.Nil(t, result.CredentialValid)
	require.Empty(t, result.Tiers)
	require.False(t, result.Persisted)
	require.Equal(t, "upstream_response", result.Source)
	require.Len(t, result.ObservedLimits, 1)
	require.Equal(t, "monthly", result.ObservedLimits[0].Window)
	require.Empty(t, repo.extraWrites)
}
