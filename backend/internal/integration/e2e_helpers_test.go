//go:build e2e

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	e2eSuiteEnv           = "E2E_SUITE"
	e2eSuiteContract      = "contract"
	e2eSuiteLive          = "live"
	e2eContractAllowEnv   = "E2E_ALLOW_MUTATION"
	e2eLiveMinAttempts    = "E2E_LIVE_MIN_ATTEMPTS"
	defaultRequestTimeout = 30 * time.Second
)

type apiEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func requireContractMode(t *testing.T) {
	t.Helper()
	if getEnv(e2eSuiteEnv, "") != e2eSuiteContract ||
		!strings.EqualFold(strings.TrimSpace(os.Getenv(e2eContractAllowEnv)), "true") {
		t.Fatalf(
			"contract E2E performs mutations and must run through scripts/e2e-test.sh " +
				"with E2E_SUITE=contract and E2E_ALLOW_MUTATION=true",
		)
	}
}

func doJSONRequest(
	t *testing.T,
	method string,
	path string,
	payload any,
	token string,
	headers map[string]string,
) (*http.Response, []byte) {
	t.Helper()

	var bodyReader io.Reader
	if payload != nil {
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("encode %s %s payload: %v", method, path, err)
		}
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, baseURL+path, bodyReader)
	if err != nil {
		t.Fatalf("create %s %s request: %v", method, path, err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	client := &http.Client{Timeout: defaultRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("execute %s %s: %v", method, path, err)
	}
	body, err := io.ReadAll(resp.Body)
	if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("read %s %s response: %v", method, path, err)
	}
	return resp, body
}

func requireHTTPStatus(t *testing.T, method, path string, resp *http.Response, body []byte, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		t.Fatalf("%s %s returned HTTP %d, want %d: %s", method, path, resp.StatusCode, expected, body)
	}
}

func decodeEnvelopeData(t *testing.T, body []byte, target any) {
	t.Helper()
	var envelope apiEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode response envelope: %v; body=%s", err, body)
	}
	if envelope.Code != 0 {
		t.Fatalf("response envelope code=%d message=%q; body=%s", envelope.Code, envelope.Message, body)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		t.Fatalf("response envelope has no data: %s", body)
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		t.Fatalf("decode response data: %v; body=%s", err, body)
	}
}

type liveProviderStats struct {
	Attempts  int
	Completed int
	Degraded  int
	Missing   bool
}

var liveStats = struct {
	sync.Mutex
	providers map[string]*liveProviderStats
}{providers: make(map[string]*liveProviderStats)}

func mutateLiveProvider(provider string, mutate func(*liveProviderStats)) {
	liveStats.Lock()
	defer liveStats.Unlock()
	stats := liveStats.providers[provider]
	if stats == nil {
		stats = &liveProviderStats{}
		liveStats.providers[provider] = stats
	}
	mutate(stats)
}

func recordLiveMissing(provider string) {
	mutateLiveProvider(provider, func(stats *liveProviderStats) { stats.Missing = true })
}

func recordLiveAttempt(provider string) {
	mutateLiveProvider(provider, func(stats *liveProviderStats) { stats.Attempts++ })
}

func recordLivePass(provider string) {
	mutateLiveProvider(provider, func(stats *liveProviderStats) { stats.Completed++ })
}

func recordLiveDegraded(provider string) {
	mutateLiveProvider(provider, func(stats *liveProviderStats) { stats.Degraded++ })
}

func liveProviderDegraded(t *testing.T, provider, reason string) {
	t.Helper()
	recordLiveDegraded(provider)
	t.Skip(reason)
}

func liveSmokeSummary() (attempts int, report string) {
	liveStats.Lock()
	defer liveStats.Unlock()

	providers := []string{"claude", "gemini"}
	var lines []string
	for _, provider := range providers {
		stats := liveStats.providers[provider]
		if stats == nil {
			stats = &liveProviderStats{}
		}
		attempts += stats.Attempts
		failed := stats.Attempts - stats.Completed
		if failed < 0 {
			failed = 0
		}
		lines = append(lines, fmt.Sprintf(
			"provider=%s configured=%t attempted_suites=%d completed_without_failure=%d degraded_events=%d failed_suites=%d",
			provider,
			!stats.Missing,
			stats.Attempts,
			stats.Completed,
			stats.Degraded,
			failed,
		))
	}
	return attempts, strings.Join(lines, "\n")
}

func liveMinimumAttempts() int {
	raw := strings.TrimSpace(os.Getenv(e2eLiveMinAttempts))
	if raw == "" {
		return 1
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 1
	}
	return value
}

// safeLogKey records only a short prefix so E2E output cannot disclose credentials.
func safeLogKey(t *testing.T, prefix string, key string) {
	t.Helper()
	key = strings.TrimSpace(key)
	if len(key) <= 8 {
		t.Logf("%s: *** (length: %d)", prefix, len(key))
		return
	}
	t.Logf("%s: %s... (length: %d)", prefix, key[:8], len(key))
}
