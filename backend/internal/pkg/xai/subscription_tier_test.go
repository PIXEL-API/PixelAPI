//go:build unit

package xai

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMapJWTSubscriptionTier(t *testing.T) {
	t.Parallel()
	require.Equal(t, "free", MapJWTSubscriptionTier(0))
	require.Equal(t, "supergrok", MapJWTSubscriptionTier(1))
	require.Equal(t, "supergrok_heavy", MapJWTSubscriptionTier(5))
	require.Equal(t, "supergrok_lite", MapJWTSubscriptionTier(6))
	require.Equal(t, "supergrok_plus", MapJWTSubscriptionTier(7))
	require.Equal(t, "9", MapJWTSubscriptionTier(9))
}

func TestNormalizeSubscriptionTier(t *testing.T) {
	t.Parallel()
	require.Equal(t, "free", NormalizeSubscriptionTier("free-tier"))
	require.Equal(t, "supergrok_lite", NormalizeSubscriptionTier("SuperGrok Lite"))
	require.Equal(t, "supergrok_heavy", NormalizeSubscriptionTier("SuperGrok Heavy"))
	require.Equal(t, "supergrok_pro", NormalizeSubscriptionTier("SuperGrokPro"))
}

func TestSubscriptionTierFromJWT(t *testing.T) {
	t.Parallel()
	require.Equal(t, "supergrok_heavy", SubscriptionTierFromJWT(makeSubscriptionTierJWT(t, map[string]any{"tier": 5})))
	require.Equal(t, "supergrok_lite", SubscriptionTierFromJWT(makeSubscriptionTierJWT(t, map[string]any{"tier": "6"})))
	require.Equal(t, "supergrok_plus", SubscriptionTierFromJWT(makeSubscriptionTierJWT(t, map[string]any{"tier": "SuperGrok Plus"})))
	require.Empty(t, SubscriptionTierFromJWT(makeSubscriptionTierJWT(t, map[string]any{"tier": 1.5})))
	require.Empty(t, SubscriptionTierFromJWT("opaque"))
}

func makeSubscriptionTierJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}
