package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

func TestGrokOAuthURLPolicyHonorsConfiguredEndpoints(t *testing.T) {
	t.Run("official API endpoint is honored", func(t *testing.T) {
		account := &Account{
			Platform: PlatformGrok,
			Type:     AccountTypeOAuth,
			Credentials: map[string]any{
				"base_url": xai.DefaultBaseURL,
			},
		}
		target, err := buildGrokResponsesURL(context.Background(), account, nil, nil)
		require.NoError(t, err)
		require.Equal(t, xai.DefaultBaseURL+"/responses", target)
	})

	t.Run("regional official endpoint bypasses operator allowlist", func(t *testing.T) {
		account := &Account{
			Platform: PlatformGrok,
			Type:     AccountTypeOAuth,
			Credentials: map[string]any{
				"base_url": "https://us-west-2.api.x.ai/v1",
			},
		}
		target, err := buildGrokResponsesURL(context.Background(), account, nil, nil)
		require.NoError(t, err)
		require.Equal(t, "https://us-west-2.api.x.ai/v1/responses", target)
	})

	t.Run("custom relay is rejected when allowlist is disabled", func(t *testing.T) {
		tests := []struct {
			name     string
			baseURL  string
			insecure bool
		}{
			{name: "public HTTPS", baseURL: "https://relay.example.test/tenant/xai/v1"},
			{name: "private HTTPS", baseURL: "https://127.0.0.1/v1"},
			{name: "public HTTP", baseURL: "http://relay.example.test/v1", insecure: true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				account := &Account{
					Platform: PlatformGrok,
					Type:     AccountTypeOAuth,
					Credentials: map[string]any{
						"base_url": tt.baseURL,
					},
				}
				cfg := &config.Config{}
				cfg.Security.URLAllowlist.AllowInsecureHTTP = tt.insecure

				_, err := buildGrokResponsesURL(context.Background(), account, cfg, nil)
				require.ErrorContains(t, err, "security.url_allowlist.enabled=true")
			})
		}
	})

	t.Run("custom relay is rejected when private hosts are enabled", func(t *testing.T) {
		account := &Account{
			Platform: PlatformGrok,
			Type:     AccountTypeOAuth,
			Credentials: map[string]any{
				"base_url": "https://relay.example.test/tenant/xai/v1",
			},
		}
		cfg := &config.Config{}
		cfg.Security.URLAllowlist.Enabled = true
		cfg.Security.URLAllowlist.AllowPrivateHosts = true
		cfg.Security.URLAllowlist.UpstreamHosts = []string{"relay.example.test"}

		_, err := buildGrokResponsesURL(context.Background(), account, cfg, nil)
		require.ErrorContains(t, err, "security.url_allowlist.allow_private_hosts=false")
	})

	t.Run("custom relay requires an explicit allowlist host", func(t *testing.T) {
		account := &Account{
			Platform: PlatformGrok,
			Type:     AccountTypeOAuth,
			Credentials: map[string]any{
				"base_url": "https://relay.example.test/tenant/xai/v1",
			},
		}
		cfg := &config.Config{}
		cfg.Security.URLAllowlist.Enabled = true
		cfg.Security.URLAllowlist.UpstreamHosts = []string{"other.example.test"}

		_, err := buildGrokResponsesURL(context.Background(), account, cfg, nil)
		require.Error(t, err)
	})

	t.Run("custom relay rejects insecure or private targets under strict policy", func(t *testing.T) {
		tests := []struct {
			name        string
			baseURL     string
			allowedHost string
		}{
			{name: "HTTP", baseURL: "http://relay.example.test/v1", allowedHost: "relay.example.test"},
			{name: "private literal", baseURL: "https://127.0.0.1/v1", allowedHost: "127.0.0.1"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				account := &Account{
					Platform: PlatformGrok,
					Type:     AccountTypeOAuth,
					Credentials: map[string]any{
						"base_url": tt.baseURL,
					},
				}
				cfg := &config.Config{}
				cfg.Security.URLAllowlist.Enabled = true
				cfg.Security.URLAllowlist.AllowPrivateHosts = false
				cfg.Security.URLAllowlist.AllowInsecureHTTP = true
				cfg.Security.URLAllowlist.UpstreamHosts = []string{tt.allowedHost}

				_, err := buildGrokResponsesURL(context.Background(), account, cfg, nil)
				require.Error(t, err)
			})
		}
	})

	t.Run("custom relay is allowed by strict operator policy", func(t *testing.T) {
		account := &Account{
			Platform: PlatformGrok,
			Type:     AccountTypeOAuth,
			Credentials: map[string]any{
				"base_url": "https://relay.example.test/tenant/xai/v1",
			},
		}
		cfg := &config.Config{}
		cfg.Security.URLAllowlist.Enabled = true
		cfg.Security.URLAllowlist.AllowPrivateHosts = false
		cfg.Security.URLAllowlist.UpstreamHosts = []string{"relay.example.test"}

		target, err := buildGrokResponsesURL(context.Background(), account, cfg, nil)
		require.NoError(t, err)
		require.Equal(t, "https://relay.example.test/tenant/xai/v1/responses", target)
	})

	t.Run("custom relay fails closed without security config", func(t *testing.T) {
		account := &Account{
			Platform: PlatformGrok,
			Type:     AccountTypeOAuth,
			Credentials: map[string]any{
				"base_url": "https://relay.example.test/v1",
			},
		}

		_, err := buildGrokResponsesURL(context.Background(), account, nil, nil)
		require.Error(t, err)
	})
}

func TestGrokAPIKeyURLPolicyKeepsOperatorCompatibility(t *testing.T) {
	account := &Account{
		Platform: PlatformGrok,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url": "https://relay.example.test/tenant/xai/v1",
		},
	}
	cfg := &config.Config{}

	target, err := buildGrokResponsesURL(context.Background(), account, cfg, nil)
	require.NoError(t, err)
	require.Equal(t, "https://relay.example.test/tenant/xai/v1/responses", target)
}

func TestBuildGrokBillingURLFollowsAccountEndpoint(t *testing.T) {
	account := &Account{
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"base_url": "https://relay.example.test/tenant/xai/v1",
		},
	}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = true
	cfg.Security.URLAllowlist.AllowPrivateHosts = false
	cfg.Security.URLAllowlist.UpstreamHosts = []string{"relay.example.test"}

	target, err := buildGrokBillingURL(context.Background(), account, cfg, nil, true)
	require.NoError(t, err)
	require.Equal(t, "https://relay.example.test/tenant/xai/v1"+xai.BillingWeeklyPath, target)
}
