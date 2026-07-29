package repository

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestHTTPClientForUpstreamRequestDisablesRedirectsWithoutMutatingSharedClient(t *testing.T) {
	shared := &http.Client{}
	plainReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := httpClientForUpstreamRequest(shared, plainReq); got != shared {
		t.Fatal("ordinary request should reuse the shared client")
	}

	secureCtx := service.WithHTTPUpstreamRedirectsDisabled(context.Background())
	secureReq, err := http.NewRequestWithContext(secureCtx, http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := httpClientForUpstreamRequest(shared, secureReq)
	if got == shared {
		t.Fatal("redirect-disabled request should use a shallow client copy")
	}
	if shared.CheckRedirect != nil {
		t.Fatal("shared client redirect policy must remain unchanged")
	}
	if got.CheckRedirect == nil {
		t.Fatal("redirect-disabled client should install a redirect policy")
	}
	if err := got.CheckRedirect(secureReq, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect error = %v, want http.ErrUseLastResponse", err)
	}
}

func TestOpenAIHTTPUpstreamProfileReturnsRedirectWithoutVisitingTarget(t *testing.T) {
	var sourceCalls atomic.Int32
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		sourceCalls.Add(1)
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer source.Close()

	ctx := service.WithHTTPUpstreamProfile(context.Background(), service.HTTPUpstreamProfileOpenAI)
	if !service.HTTPUpstreamRedirectsDisabled(ctx) {
		t.Fatal("OpenAI upstream profile must disable redirects")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	upstream := NewHTTPUpstream(&config.Config{
		Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false},
		},
	})

	resp, err := upstream.Do(req, "", 1, 1)
	if err != nil {
		t.Fatalf("OpenAI upstream request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	if got := resp.Header.Get("Location"); got != target.URL {
		t.Fatalf("Location = %q, want %q", got, target.URL)
	}
	if got := sourceCalls.Load(); got != 1 {
		t.Fatalf("source calls = %d, want 1", got)
	}
	if got := targetCalls.Load(); got != 0 {
		t.Fatalf("redirect target calls = %d, want 0", got)
	}
}
