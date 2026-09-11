package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCNQuotaSnapshotResetRequiresAnExhaustedWindow(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fiveHour, weekly := now.Add(time.Hour), now.Add(24*time.Hour)
	account := &Account{Platform: PlatformKimi, Credentials: map[string]any{"account_mode": AccountModeCoding}, Extra: map[string]any{
		"kimi_5h_used_percent": 10.0, "kimi_5h_reset_at": fiveHour.Format(time.RFC3339),
		"kimi_weekly_used_percent": 20.0, "kimi_weekly_reset_at": weekly.Format(time.RFC3339),
	}}
	require.Nil(t, cnProviderQuotaSnapshotReset(account, now), "healthy quota must not prolong an RPM cooldown")
	account.Extra["kimi_5h_used_percent"] = "NaN"
	require.Nil(t, cnProviderQuotaSnapshotReset(account, now), "invalid snapshot must not suspend an account")
	account.Extra["kimi_5h_used_percent"] = 100.0
	require.Equal(t, &fiveHour, cnProviderQuotaSnapshotReset(account, now))
	account.Extra["kimi_weekly_used_percent"] = 100.0
	require.Equal(t, &weekly, cnProviderQuotaSnapshotReset(account, now), "both exhausted windows must reset")
	require.Nil(t, cnProviderQuotaSnapshotReset(account, weekly.Add(time.Second)))
}

func TestCNQuotaProbeRejectsInvalidSnapshotsWithoutPersistence(t *testing.T) {
	for _, body := range []string{`{}`, `{"usage":`, `{"usage":{}}`, strings.Repeat(" ", cnQuotaMaxBodyBytes+1)} {
		t.Run(body[:min(len(body), 24)], func(t *testing.T) {
			account := &Account{ID: 911, Platform: PlatformKimi, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "test-key", "account_mode": AccountModeCoding}}
			repo := &cnBalanceProbeRepo{account: account}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}}
			svc := NewCNProviderQuotaService(repo, nil, upstream, nil)
			result, err := svc.QueryUsage(context.Background(), account.ID)
			if err == nil {
				require.False(t, result.Success)
				require.NotEmpty(t, result.Error)
				require.Nil(t, result.CredentialValid)
			}
			require.Empty(t, repo.extraWrites)
		})
	}
}
