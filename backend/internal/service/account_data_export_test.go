package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildAccountDataPayloadExportsProxyLifecycleAndLocalScope(t *testing.T) {
	expiresAt := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	backupID := int64(2)
	payload := BuildAccountDataPayload(nil, []Proxy{
		{
			ID:                   1,
			Name:                 "primary",
			Protocol:             "http",
			Host:                 "primary.example",
			Port:                 8080,
			Status:               StatusActive,
			ExpiresAt:            &expiresAt,
			FallbackMode:         FallbackModeProxy,
			BackupProxyID:        &backupID,
			ExpiryWarnDays:       3,
			Platform:             PlatformOpenAI,
			RequiredAccountLevel: "team",
			MaxAccounts:          8,
		},
		{
			ID:       2,
			Name:     "backup",
			Protocol: "socks5",
			Host:     "backup.example",
			Port:     1080,
			Status:   StatusActive,
		},
	}, func(protocol, host string, port int, username, password string) string {
		return protocol + "|" + host
	})

	require.Len(t, payload.Proxies, 2)
	primary := payload.Proxies[0]
	require.NotNil(t, primary.ExpiresAt)
	require.Equal(t, expiresAt.Unix(), *primary.ExpiresAt)
	require.Equal(t, FallbackModeProxy, primary.FallbackMode)
	require.Equal(t, "backup", primary.BackupProxyName)
	require.Equal(t, "socks5|backup.example", primary.BackupProxyKey)
	require.Equal(t, 3, primary.ExpiryWarnDays)
	require.Equal(t, PlatformOpenAI, primary.Platform)
	require.Equal(t, "team", primary.RequiredAccountLevel)
	require.NotNil(t, primary.MaxAccounts)
	require.Equal(t, 8, *primary.MaxAccounts)
}
