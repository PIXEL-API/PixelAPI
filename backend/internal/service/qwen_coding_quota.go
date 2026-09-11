package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Qwen exposes Coding Plan exhaustion in upstream responses, but does not
// expose a public remaining-requests API. These are observations, not a
// remaining quota estimate or a claim that the window is still exhausted.
type QwenCodingLimitObservation struct {
	Window     string `json:"window"`
	ObservedAt string `json:"observed_at"`
	RetryAt    string `json:"retry_at,omitempty"`
}

func qwenCodingLimitWindow(body []byte) string {
	message := strings.ToLower(extractUpstreamErrorMessage(body))
	for _, entry := range []struct{ message, window string }{
		{"hour allocated quota exceeded", "5h"},
		{"week allocated quota exceeded", "weekly"},
		{"month allocated quota exceeded", "monthly"},
	} {
		if strings.Contains(message, entry.message) {
			return entry.window
		}
	}
	return ""
}

func qwenCodingLimitKey(window string) string { return "qwen_coding_" + window + "_limit" }

func qwenRetryAfter(headers http.Header, now time.Time) *time.Time {
	value := strings.TrimSpace(headers.Get("Retry-After"))
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 && seconds <= int64((1<<63-1)/time.Second) {
		until := now.Add(time.Duration(seconds) * time.Second)
		return &until
	}
	if until, err := http.ParseTime(value); err == nil && until.After(now) {
		return &until
	}
	return nil
}

func qwenCodingLimitObservations(account *Account) []QwenCodingLimitObservation {
	var limits []QwenCodingLimitObservation
	for _, window := range []string{"5h", "weekly", "monthly"} {
		raw := account.Extra[qwenCodingLimitKey(window)]
		if raw == nil {
			continue
		}
		data, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		var observation QwenCodingLimitObservation
		if json.Unmarshal(data, &observation) != nil || observation.Window != window {
			continue
		}
		if _, err := time.Parse(time.RFC3339, observation.ObservedAt); err != nil {
			continue
		}
		limits = append(limits, observation)
	}
	return limits
}

func qwenCodingQuotaResult(account *Account) *CNProviderQuotaProbeResult {
	querySupported := false
	return &CNProviderQuotaProbeResult{
		Provider: PlatformQwen, Source: "upstream_response", Success: true,
		AutomaticQuerySupported: &querySupported,
		ObservedLimits:          qwenCodingLimitObservations(account),
		FetchedAt:               time.Now().UTC().Unix(),
	}
}

// Do not infer a five-hour reset from the observation time, or a monthly
// renewal date from the calendar. Without Retry-After the existing configured
// 429 cooldown applies. PAYG is never selected as a billing-mode fallback.
func (s *RateLimitService) applyQwenReactive429(ctx context.Context, account *Account, headers http.Header, body []byte) bool {
	now := time.Now().UTC()
	retryAt := qwenRetryAfter(headers, now)
	if account.IsCodingPlan() {
		if window := qwenCodingLimitWindow(body); window != "" {
			observation := QwenCodingLimitObservation{Window: window, ObservedAt: now.Format(time.RFC3339)}
			if retryAt != nil {
				observation.RetryAt = retryAt.UTC().Format(time.RFC3339)
			}
			if repo, ok := s.accountRepo.(CNProviderStateRepository); ok {
				if _, err := repo.UpdateCNProviderExtraIfMatch(ctx, account.ID, account, map[string]any{qwenCodingLimitKey(window): observation}); err != nil {
					slog.Warn("qwen_coding_quota_observation_failed", "account_id", account.ID, "error", err)
				}
			} else {
				slog.Warn("qwen_coding_quota_state_repository_unavailable", "account_id", account.ID)
			}
		}
	}
	if retryAt == nil {
		return false
	}
	if err := s.accountRepo.SetRateLimited(ctx, account.ID, *retryAt); err != nil {
		slog.Warn("qwen_rate_limit_set_failed", "account_id", account.ID, "error", err)
	}
	return true
}
