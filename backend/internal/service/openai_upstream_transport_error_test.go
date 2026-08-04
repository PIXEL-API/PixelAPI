package service

import (
	"errors"
	"testing"
)

func TestClassifyOpenAITransportError_ProxyDialTimeout(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		wantPersistent bool
	}{
		{
			// The exact production shape from the 2026-08-04 incident.
			name:           "http proxyconnect dial i/o timeout",
			err:            errors.New(`Post "https://chatgpt.com/backend-api/codex/responses": proxyconnect tcp: dial tcp 154.44.9.17:23128: i/o timeout`),
			wantPersistent: true,
		},
		{
			name:           "socks connect dial i/o timeout",
			err:            errors.New(`Post "https://chatgpt.com/backend-api/codex/responses": socks connect tcp 10.0.0.9:1080->chatgpt.com:443: dial tcp 10.0.0.9:1080: i/o timeout`),
			wantPersistent: true,
		},
		{
			// Timeout reaching the real upstream through a (healthy) proxy or
			// directly: must stay transient — the account/proxy is not at fault.
			name:           "upstream dial i/o timeout without proxy marker",
			err:            errors.New(`Post "https://chatgpt.com/backend-api/codex/responses": dial tcp 104.18.32.47:443: i/o timeout`),
			wantPersistent: false,
		},
		{
			// Request-context expiry during proxy dial (e.g. first-output budget
			// lapsing) is not evidence the proxy is dead.
			name:           "proxyconnect context deadline exceeded",
			err:            errors.New(`Post "https://chatgpt.com/backend-api/codex/responses": proxyconnect tcp: dial tcp 154.44.9.17:23128: context deadline exceeded`),
			wantPersistent: false,
		},
		{
			name:           "proxyconnect connection refused still persistent",
			err:            errors.New(`Post "https://chatgpt.com/backend-api/codex/responses": proxyconnect tcp: dial tcp 154.44.9.17:23128: connect: connection refused`),
			wantPersistent: true,
		},
		{
			name:           "tls handshake timeout stays transient",
			err:            errors.New(`Post "https://chatgpt.com/backend-api/codex/responses": net/http: TLS handshake timeout`),
			wantPersistent: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyOpenAITransportError(tt.err).Persistent
			if got != tt.wantPersistent {
				t.Fatalf("classifyOpenAITransportError(%q).Persistent = %v, want %v", tt.err, got, tt.wantPersistent)
			}
		})
	}
}
