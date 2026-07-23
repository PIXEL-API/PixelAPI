package service

import (
	"context"
	"testing"
)

func TestWithHTTPUpstreamProfileDefaultKeepsContext(t *testing.T) {
	ctx := context.Background()
	if got := WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileDefault); got != ctx {
		t.Fatal("default profile should not wrap context")
	}
}

func TestWithHTTPUpstreamProfileOpenAI(t *testing.T) {
	ctx := WithHTTPUpstreamProfile(context.TODO(), HTTPUpstreamProfileOpenAI)
	if profile := HTTPUpstreamProfileFromContext(ctx); profile != HTTPUpstreamProfileOpenAI {
		t.Fatalf("profile = %q, want %q", profile, HTTPUpstreamProfileOpenAI)
	}
}

func TestHTTPUpstreamProfileRejectsUnknownValue(t *testing.T) {
	ctx := WithHTTPUpstreamProfile(context.Background(), HTTPUpstreamProfile("unknown"))
	if profile := HTTPUpstreamProfileFromContext(ctx); profile != HTTPUpstreamProfileDefault {
		t.Fatalf("profile = %q, want default", profile)
	}
}

func TestWithHTTPUpstreamRedirectsDisabled(t *testing.T) {
	ctx := WithHTTPUpstreamRedirectsDisabled(nil)
	if !HTTPUpstreamRedirectsDisabled(ctx) {
		t.Fatal("redirects should be disabled")
	}
	if HTTPUpstreamRedirectsDisabled(context.Background()) {
		t.Fatal("redirects should remain enabled by default")
	}
}
