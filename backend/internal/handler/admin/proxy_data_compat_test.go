package admin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDataProxyReferenceIndexPrefersKeyAndRejectsAmbiguousName(t *testing.T) {
	first := service.Proxy{ID: 1, Name: "duplicate", Protocol: "http", Host: "one.example", Port: 8080}
	second := service.Proxy{ID: 2, Name: "duplicate", Protocol: "http", Host: "two.example", Port: 8080}
	index := newDataProxyReferenceIndex([]service.Proxy{first, second})

	byName := decodeDataProxyJSON(t, `{"backup_proxy_name":"duplicate"}`)
	id, supplied, warning := index.ResolveBackup(byName, 99)
	require.True(t, supplied)
	require.Nil(t, id)
	require.Contains(t, warning, "ambiguous")

	byKey := decodeDataProxyJSON(t, `{"backup_proxy_key":"http|two.example|8080||","backup_proxy_name":"duplicate"}`)
	id, supplied, warning = index.ResolveBackup(byKey, 99)
	require.True(t, supplied)
	require.Empty(t, warning)
	require.NotNil(t, id)
	require.Equal(t, int64(2), *id)
}

func TestDataProxyReferenceIndexDoesNotGuessWhenPreferredKeyIsMissing(t *testing.T) {
	proxy := service.Proxy{ID: 1, Name: "backup", Protocol: "http", Host: "backup.example", Port: 8080}
	index := newDataProxyReferenceIndex([]service.Proxy{proxy})
	item := decodeDataProxyJSON(t, `{"backup_proxy_key":"missing","backup_proxy_name":"backup"}`)

	id, supplied, warning := index.ResolveBackup(item, 99)

	require.True(t, supplied)
	require.Nil(t, id)
	require.Contains(t, warning, "backup_proxy_key")
}

func TestApplyDataProxyLifecycleRelationsResolvesDeclaredTransportKey(t *testing.T) {
	primary := service.Proxy{ID: 1, Name: "primary", Protocol: "http", Host: "primary.example", Port: 8080}
	backup := service.Proxy{ID: 2, Name: "backup", Protocol: "http", Host: "backup.example", Port: 8080}
	records := []dataProxyImportRecord{
		{item: decodeDataProxyJSON(t, `{"fallback_mode":"proxy","backup_proxy_key":"community-backup-id"}`), key: "community-primary-id", proxy: primary},
		{item: decodeDataProxyJSON(t, `{}`), key: "community-backup-id", proxy: backup},
	}
	var captured *service.UpdateProxyInput

	errorsOut := applyDataProxyLifecycleRelations(context.Background(), records, nil, func(_ context.Context, id int64, input *service.UpdateProxyInput) (*service.Proxy, error) {
		if id == primary.ID {
			captured = input
		}
		return &service.Proxy{ID: id}, nil
	})

	require.Empty(t, errorsOut)
	require.NotNil(t, captured)
	require.True(t, captured.BackupProxyIDProvided)
	require.NotNil(t, captured.BackupProxyID)
	require.Equal(t, backup.ID, *captured.BackupProxyID)
}

func TestAccountDataProxyTracksLifecycleFieldPresence(t *testing.T) {
	omitted := decodeDataProxyJSON(t, `{}`)
	require.False(t, omitted.HasExpiresAt())
	require.False(t, omitted.HasFallbackMode())
	require.False(t, omitted.HasBackupProxyName())
	require.False(t, omitted.HasBackupProxyKey())
	require.False(t, omitted.HasExpiryWarnDays())

	explicit := decodeDataProxyJSON(t, `{"expires_at":null,"fallback_mode":null,"backup_proxy_name":null,"backup_proxy_key":null,"expiry_warn_days":0}`)
	require.True(t, explicit.HasExpiresAt())
	require.Nil(t, explicit.ExpiresAt)
	require.True(t, explicit.HasFallbackMode())
	require.True(t, explicit.HasBackupProxyName())
	require.True(t, explicit.HasBackupProxyKey())
	require.True(t, explicit.HasExpiryWarnDays())
	require.Zero(t, explicit.ExpiryWarnDays)
}

func TestExpandDataProxyBackupClosureSupportsForwardChainsAndCycles(t *testing.T) {
	secondID, thirdID, firstID := int64(2), int64(3), int64(1)
	all := map[int64]service.Proxy{
		2: {ID: 2, Name: "second", BackupProxyID: &thirdID},
		3: {ID: 3, Name: "third", BackupProxyID: &firstID},
	}
	loaderCalls := 0
	got, err := expandDataProxyBackupClosure(context.Background(), []service.Proxy{{ID: 1, Name: "first", BackupProxyID: &secondID}}, func(_ context.Context, ids []int64) ([]service.Proxy, error) {
		loaderCalls++
		out := make([]service.Proxy, 0, len(ids))
		for _, id := range ids {
			if proxy, ok := all[id]; ok {
				out = append(out, proxy)
			}
		}
		return out, nil
	})

	require.NoError(t, err)
	require.Equal(t, []int64{1, 2, 3}, []int64{got[0].ID, got[1].ID, got[2].ID})
	require.Equal(t, 2, loaderCalls)
}

func TestApplyDataProxyLifecycleRelationsResolvesForwardReference(t *testing.T) {
	primary := service.Proxy{ID: 1, Name: "primary", Protocol: "http", Host: "primary.example", Port: 8080}
	backup := service.Proxy{ID: 2, Name: "backup", Protocol: "http", Host: "backup.example", Port: 8080}
	records := []dataProxyImportRecord{
		{item: decodeDataProxyJSON(t, `{"fallback_mode":"proxy","backup_proxy_name":"backup"}`), key: "primary-key", proxy: primary},
		{item: decodeDataProxyJSON(t, `{}`), key: "backup-key", proxy: backup},
	}
	updates := map[int64]*service.UpdateProxyInput{}

	errorsOut := applyDataProxyLifecycleRelations(context.Background(), records, nil, func(_ context.Context, id int64, input *service.UpdateProxyInput) (*service.Proxy, error) {
		updates[id] = input
		return &service.Proxy{ID: id}, nil
	})

	require.Empty(t, errorsOut)
	require.Contains(t, updates, int64(1))
	require.NotNil(t, updates[1].FallbackMode)
	require.Equal(t, service.FallbackModeProxy, *updates[1].FallbackMode)
	require.True(t, updates[1].BackupProxyIDProvided)
	require.NotNil(t, updates[1].BackupProxyID)
	require.Equal(t, int64(2), *updates[1].BackupProxyID)
	require.NotContains(t, updates, int64(2), "an item with all lifecycle fields omitted must remain untouched")
}

func TestApplyDataProxyLifecycleRelationsPreservesOmittedAndClearsExplicitNull(t *testing.T) {
	expiresAt := time.Now().Add(time.Hour)
	backupID := int64(2)
	existing := service.Proxy{
		ID:             1,
		Name:           "primary",
		Protocol:       "http",
		Host:           "primary.example",
		Port:           8080,
		ExpiresAt:      &expiresAt,
		FallbackMode:   service.FallbackModeProxy,
		BackupProxyID:  &backupID,
		ExpiryWarnDays: 7,
	}
	item := decodeDataProxyJSON(t, `{"expires_at":null,"backup_proxy_key":null,"expiry_warn_days":0}`)
	var captured *service.UpdateProxyInput

	errorsOut := applyDataProxyLifecycleRelations(context.Background(), []dataProxyImportRecord{{item: item, key: "primary-key", proxy: existing}}, []service.Proxy{existing}, func(_ context.Context, _ int64, input *service.UpdateProxyInput) (*service.Proxy, error) {
		captured = input
		return &existing, nil
	})

	require.Len(t, errorsOut, 1)
	require.Contains(t, errorsOut[0].Message, "no resolvable backup")
	require.NotNil(t, captured)
	require.True(t, captured.ExpiresAtProvided)
	require.Nil(t, captured.ExpiresAt)
	require.True(t, captured.BackupProxyIDProvided)
	require.Nil(t, captured.BackupProxyID)
	require.NotNil(t, captured.ExpiryWarnDays)
	require.Zero(t, *captured.ExpiryWarnDays)
	require.NotNil(t, captured.FallbackMode)
	require.Equal(t, service.FallbackModeNone, *captured.FallbackMode, "clearing the backup of proxy mode must fail closed")
}

func decodeDataProxyJSON(t *testing.T, raw string) DataProxy {
	t.Helper()
	var payload struct {
		Proxy DataProxy `json:"proxy"`
	}
	require.NoError(t, json.Unmarshal([]byte(`{"proxy":`+raw+`}`), &payload))
	return payload.Proxy
}
