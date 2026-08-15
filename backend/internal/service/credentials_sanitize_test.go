package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeStoredCredentialsStripsEphemeralSSOSecrets(t *testing.T) {
	credentials := map[string]any{
		"access_token":      "access-token",
		"refresh_token":     "refresh-token",
		"password":          "secret",
		"sso_token":         "sso-token",
		"sso":               "sso-cookie",
		"sso-rw":            "sso-read-write",
		"clearTextPassword": "plain-text",
		"cookie":            "session-cookie",
		"base_url":          "https://api.x.ai",
	}

	sanitized := SanitizeStoredCredentials(PlatformGrok, credentials)

	require.Equal(t, "access-token", sanitized["access_token"])
	require.Equal(t, "refresh-token", sanitized["refresh_token"])
	require.Equal(t, "https://api.x.ai", sanitized["base_url"])
	for _, key := range []string{"password", "sso_token", "sso", "sso-rw", "clearTextPassword", "cookie"} {
		require.NotContains(t, sanitized, key)
	}
}

func TestSanitizeStoredCredentialsIsNilSafe(t *testing.T) {
	require.Nil(t, SanitizeStoredCredentials(PlatformGrok, nil))
}
