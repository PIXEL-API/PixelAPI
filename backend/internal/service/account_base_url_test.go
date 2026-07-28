//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

func TestGetBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		account  Account
		expected string
	}{
		{
			name: "non-apikey type returns empty",
			account: Account{
				Type:     AccountTypeOAuth,
				Platform: PlatformAnthropic,
			},
			expected: "",
		},
		{
			name: "apikey without base_url returns default anthropic",
			account: Account{
				Type:        AccountTypeAPIKey,
				Platform:    PlatformAnthropic,
				Credentials: map[string]any{},
			},
			expected: "https://api.anthropic.com",
		},
		{
			name: "apikey with custom base_url",
			account: Account{
				Type:        AccountTypeAPIKey,
				Platform:    PlatformAnthropic,
				Credentials: map[string]any{"base_url": "https://custom.example.com"},
			},
			expected: "https://custom.example.com",
		},
		{
			name: "antigravity apikey auto-appends /antigravity",
			account: Account{
				Type:        AccountTypeAPIKey,
				Platform:    PlatformAntigravity,
				Credentials: map[string]any{"base_url": "https://upstream.example.com"},
			},
			expected: "https://upstream.example.com/antigravity",
		},
		{
			name: "antigravity apikey trims trailing slash before appending",
			account: Account{
				Type:        AccountTypeAPIKey,
				Platform:    PlatformAntigravity,
				Credentials: map[string]any{"base_url": "https://upstream.example.com/"},
			},
			expected: "https://upstream.example.com/antigravity",
		},
		{
			name: "antigravity non-apikey returns empty",
			account: Account{
				Type:        AccountTypeOAuth,
				Platform:    PlatformAntigravity,
				Credentials: map[string]any{"base_url": "https://upstream.example.com"},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.account.GetBaseURL()
			if result != tt.expected {
				t.Errorf("GetBaseURL() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetGeminiBaseURL(t *testing.T) {
	const defaultGeminiURL = "https://generativelanguage.googleapis.com"

	tests := []struct {
		name     string
		account  Account
		expected string
	}{
		{
			name: "apikey without base_url returns default",
			account: Account{
				Type:        AccountTypeAPIKey,
				Platform:    PlatformGemini,
				Credentials: map[string]any{},
			},
			expected: defaultGeminiURL,
		},
		{
			name: "apikey with custom base_url",
			account: Account{
				Type:        AccountTypeAPIKey,
				Platform:    PlatformGemini,
				Credentials: map[string]any{"base_url": "https://custom-gemini.example.com"},
			},
			expected: "https://custom-gemini.example.com",
		},
		{
			name: "antigravity apikey auto-appends /antigravity",
			account: Account{
				Type:        AccountTypeAPIKey,
				Platform:    PlatformAntigravity,
				Credentials: map[string]any{"base_url": "https://upstream.example.com"},
			},
			expected: "https://upstream.example.com/antigravity",
		},
		{
			name: "antigravity apikey trims trailing slash",
			account: Account{
				Type:        AccountTypeAPIKey,
				Platform:    PlatformAntigravity,
				Credentials: map[string]any{"base_url": "https://upstream.example.com/"},
			},
			expected: "https://upstream.example.com/antigravity",
		},
		{
			name: "antigravity oauth does NOT append /antigravity",
			account: Account{
				Type:        AccountTypeOAuth,
				Platform:    PlatformAntigravity,
				Credentials: map[string]any{"base_url": "https://upstream.example.com"},
			},
			expected: "https://upstream.example.com",
		},
		{
			name: "oauth without base_url returns default",
			account: Account{
				Type:        AccountTypeOAuth,
				Platform:    PlatformAntigravity,
				Credentials: map[string]any{},
			},
			expected: defaultGeminiURL,
		},
		{
			name: "nil credentials returns default",
			account: Account{
				Type:     AccountTypeAPIKey,
				Platform: PlatformGemini,
			},
			expected: defaultGeminiURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.account.GetGeminiBaseURL(defaultGeminiURL)
			if result != tt.expected {
				t.Errorf("GetGeminiBaseURL() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestGetGrokBaseURLUsesConfiguredOAuthEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		account  Account
		expected string
	}{
		{
			name:     "oauth without base_url uses CLI subscription proxy",
			account:  Account{Type: AccountTypeOAuth, Platform: PlatformGrok, Credentials: map[string]any{}},
			expected: xai.DefaultCLIBaseURL,
		},
		{
			name: "oauth official API endpoint is honored",
			account: Account{Type: AccountTypeOAuth, Platform: PlatformGrok, Credentials: map[string]any{
				"base_url": xai.DefaultBaseURL,
			}},
			expected: xai.DefaultBaseURL,
		},
		{
			name: "oauth custom relay is honored",
			account: Account{Type: AccountTypeOAuth, Platform: PlatformGrok, Credentials: map[string]any{
				"base_url": "https://relay.example.com/xai/v1",
			}},
			expected: "https://relay.example.com/xai/v1",
		},
		{
			name: "oauth unparseable base_url falls back to CLI proxy",
			account: Account{Type: AccountTypeOAuth, Platform: PlatformGrok, Credentials: map[string]any{
				"base_url": "not a url",
			}},
			expected: xai.DefaultCLIBaseURL,
		},
		{
			name:     "API key without base_url uses official API",
			account:  Account{Type: AccountTypeAPIKey, Platform: PlatformGrok, Credentials: map[string]any{}},
			expected: xai.DefaultBaseURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.account.GetGrokBaseURL())
		})
	}
}

func TestGetGrokMediaBaseURLOnlyRedirectsCLIGateway(t *testing.T) {
	tests := []struct {
		name     string
		account  Account
		expected string
	}{
		{
			name:     "oauth default CLI uses official media API",
			account:  Account{Type: AccountTypeOAuth, Platform: PlatformGrok, Credentials: map[string]any{}},
			expected: xai.DefaultBaseURL,
		},
		{
			name: "oauth CLI variant uses official media API",
			account: Account{Type: AccountTypeOAuth, Platform: PlatformGrok, Credentials: map[string]any{
				"base_url": "HTTPS://CLI-CHAT-PROXY.GROK.COM:443/%76%31/",
			}},
			expected: xai.DefaultBaseURL,
		},
		{
			name: "oauth custom relay remains selected",
			account: Account{Type: AccountTypeOAuth, Platform: PlatformGrok, Credentials: map[string]any{
				"base_url": "https://relay.example.com/v1",
			}},
			expected: "https://relay.example.com/v1",
		},
		{
			name: "API key custom endpoint remains selected",
			account: Account{Type: AccountTypeAPIKey, Platform: PlatformGrok, Credentials: map[string]any{
				"base_url": "https://grok.example.com/v1",
			}},
			expected: "https://grok.example.com/v1",
		},
		{
			name:     "non-Grok account has no media base URL",
			account:  Account{Type: AccountTypeOAuth, Platform: PlatformOpenAI, Credentials: map[string]any{}},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.account.GetGrokMediaBaseURL())
		})
	}
}
