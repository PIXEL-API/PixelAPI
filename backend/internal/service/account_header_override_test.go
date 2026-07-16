package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyHeaderOverrides(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"header_override_enabled": true,
			"header_overrides": map[string]any{
				"x-custom-feature": "enabled",
				"user-agent":       "CustomUA/1.0",
			},
		},
	}

	headers := http.Header{}
	headers.Set("X-Custom-Feature", "old")
	headers.Set("User-Agent", "old-ua")
	account.ApplyHeaderOverrides(headers)

	require.Equal(t, "enabled", headers.Get("X-Custom-Feature"))
	require.Equal(t, "CustomUA/1.0", headers.Get("User-Agent"))
	require.Len(t, headers.Values("X-Custom-Feature"), 1)
}

func TestApplyHeaderOverridesNoOpForOAuth(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"header_override_enabled": true,
			"header_overrides": map[string]any{
				"user-agent": "CustomUA/1.0",
			},
		},
	}

	headers := http.Header{}
	headers.Set("User-Agent", "original")
	account.ApplyHeaderOverrides(headers)

	require.Equal(t, "original", headers.Get("User-Agent"))
}

func TestNormalizeHeaderOverrideCredentialsRejectsSensitiveHeaders(t *testing.T) {
	err := NormalizeHeaderOverrideCredentials(map[string]any{
		"header_override_enabled": true,
		"header_overrides": map[string]any{
			"Authorization": "Bearer bad",
		},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "not allowed")
}

func TestNormalizeHeaderOverrideCredentialsNormalizesAndRejectsDuplicates(t *testing.T) {
	creds := map[string]any{
		"header_override_enabled": true,
		"header_overrides": map[string]any{
			" X-Trace-ID ": " trace-1 ",
		},
	}
	require.NoError(t, NormalizeHeaderOverrideCredentials(creds))
	require.Equal(t, map[string]any{"x-trace-id": "trace-1"}, creds["header_overrides"])

	err := NormalizeHeaderOverrideCredentials(map[string]any{
		"header_override_enabled": true,
		"header_overrides": map[string]any{
			"X-Trace-ID": "a",
			"x-trace-id": "b",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate")
}
