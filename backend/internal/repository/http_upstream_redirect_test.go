package repository

import (
	"context"
	"errors"
	"net/http"
	"testing"

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
