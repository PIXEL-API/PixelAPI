package admin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type dataProxyReferenceIndex struct {
	byKey  map[string][]int64
	byName map[string][]int64
}

func newDataProxyReferenceIndex(proxies []service.Proxy) *dataProxyReferenceIndex {
	index := &dataProxyReferenceIndex{
		byKey:  make(map[string][]int64, len(proxies)),
		byName: make(map[string][]int64, len(proxies)),
	}
	for i := range proxies {
		index.Add(proxies[i])
	}
	return index
}

func (i *dataProxyReferenceIndex) Add(proxy service.Proxy) {
	if i == nil || proxy.ID <= 0 {
		return
	}
	key := buildProxyKey(proxy.Protocol, proxy.Host, proxy.Port, proxy.Username, proxy.Password)
	i.AddKey(key, proxy.ID)
	name := strings.TrimSpace(proxy.Name)
	if name != "" {
		i.byName[name] = appendUniqueDataProxyID(i.byName[name], proxy.ID)
	}
}

func (i *dataProxyReferenceIndex) AddKey(key string, proxyID int64) {
	if i == nil || proxyID <= 0 {
		return
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	i.byKey[key] = appendUniqueDataProxyID(i.byKey[key], proxyID)
}

func appendUniqueDataProxyID(ids []int64, id int64) []int64 {
	for _, existing := range ids {
		if existing == id {
			return ids
		}
	}
	return append(ids, id)
}

// ResolveBackup returns (id, supplied, warning). backup_proxy_key has strict
// priority over backup_proxy_name. A supplied but unresolved or ambiguous
// reference never falls through to the lower-priority name because guessing
// would make imports order- and environment-dependent.
func (i *dataProxyReferenceIndex) ResolveBackup(item DataProxy, selfID int64) (*int64, bool, string) {
	if item.HasBackupProxyKey() {
		key := strings.TrimSpace(item.BackupProxyKey)
		if key == "" {
			return nil, true, ""
		}
		return resolveUniqueDataProxyReference(i.byKey[key], selfID, "backup_proxy_key", key)
	}
	if item.HasBackupProxyName() {
		name := strings.TrimSpace(item.BackupProxyName)
		if name == "" {
			return nil, true, ""
		}
		return resolveUniqueDataProxyReference(i.byName[name], selfID, "backup_proxy_name", name)
	}
	return nil, false, ""
}

func resolveUniqueDataProxyReference(candidates []int64, selfID int64, field, value string) (*int64, bool, string) {
	if len(candidates) == 0 {
		return nil, true, fmt.Sprintf("%s %q not found, fallback_mode downgraded to none", field, value)
	}
	if len(candidates) > 1 {
		return nil, true, fmt.Sprintf("%s %q is ambiguous (%d matches), fallback_mode downgraded to none", field, value, len(candidates))
	}
	if candidates[0] == selfID {
		return nil, true, fmt.Sprintf("%s %q resolves to the proxy itself, fallback_mode downgraded to none", field, value)
	}
	id := candidates[0]
	return &id, true, ""
}

type dataProxyBatchLoader func(context.Context, []int64) ([]service.Proxy, error)

type dataProxyImportRecord struct {
	item  DataProxy
	key   string
	proxy service.Proxy
}

type dataProxyUpdater func(context.Context, int64, *service.UpdateProxyInput) (*service.Proxy, error)

// applyDataProxyLifecycleRelations is phase two of Data proxy import. Every
// proxy must already exist before this runs, so backup references can point
// forward in the payload without depending on item order.
func applyDataProxyLifecycleRelations(
	ctx context.Context,
	records []dataProxyImportRecord,
	existing []service.Proxy,
	update dataProxyUpdater,
) []DataImportError {
	all := append([]service.Proxy(nil), existing...)
	for i := range records {
		all = append(all, records[i].proxy)
	}
	index := newDataProxyReferenceIndex(all)
	for i := range records {
		// The transport-level proxy_key is the identity used by the Data file.
		// Keep the canonical network key registered as well, but also accept an
		// explicitly declared key so community payloads remain self-consistent.
		index.AddKey(records[i].key, records[i].proxy.ID)
	}
	errorsOut := make([]DataImportError, 0)

	for i := range records {
		record := records[i]
		input := &service.UpdateProxyInput{}
		needsUpdate := false

		if record.item.HasExpiresAt() {
			input.ExpiresAtProvided = true
			if record.item.ExpiresAt != nil {
				value := time.Unix(*record.item.ExpiresAt, 0).UTC()
				input.ExpiresAt = &value
			}
			needsUpdate = true
		}
		if record.item.HasFallbackMode() {
			mode := strings.TrimSpace(record.item.FallbackMode)
			if mode == "" {
				mode = service.FallbackModeNone
			}
			input.FallbackMode = &mode
			needsUpdate = true
		}
		if record.item.HasExpiryWarnDays() {
			warnDays := record.item.ExpiryWarnDays
			input.ExpiryWarnDays = &warnDays
			needsUpdate = true
		}

		backupID, backupSupplied, warning := index.ResolveBackup(record.item, record.proxy.ID)
		if backupSupplied {
			input.BackupProxyIDProvided = true
			input.BackupProxyID = backupID
			needsUpdate = true
		}
		if warning != "" {
			mode := service.FallbackModeNone
			input.FallbackMode = &mode
			input.BackupProxyIDProvided = true
			input.BackupProxyID = nil
			needsUpdate = true
			errorsOut = append(errorsOut, DataImportError{
				Kind:     "proxy",
				Name:     record.item.Name,
				ProxyKey: record.key,
				Message:  warning,
			})
		}

		effectiveMode := record.proxy.FallbackMode
		if input.FallbackMode != nil {
			effectiveMode = *input.FallbackMode
		}
		effectiveBackupID := record.proxy.BackupProxyID
		if input.BackupProxyIDProvided {
			effectiveBackupID = input.BackupProxyID
		}
		if effectiveMode == service.FallbackModeProxy && effectiveBackupID == nil {
			mode := service.FallbackModeNone
			input.FallbackMode = &mode
			input.BackupProxyIDProvided = true
			input.BackupProxyID = nil
			needsUpdate = true
			errorsOut = append(errorsOut, DataImportError{
				Kind:     "proxy",
				Name:     record.item.Name,
				ProxyKey: record.key,
				Message:  "fallback_mode proxy has no resolvable backup, downgraded to none",
			})
		}

		if !needsUpdate || update == nil {
			continue
		}
		if _, err := update(ctx, record.proxy.ID, input); err != nil {
			errorsOut = append(errorsOut, DataImportError{
				Kind:     "proxy",
				Name:     record.item.Name,
				ProxyKey: record.key,
				Message:  "update proxy lifecycle failed: " + err.Error(),
			})
		}
	}
	return errorsOut
}

// expandDataProxyBackupClosure includes every reachable backup proxy, even if
// it was not selected directly. This makes an exported fallback graph portable
// and terminates safely for cycles.
func expandDataProxyBackupClosure(ctx context.Context, seeds []service.Proxy, load dataProxyBatchLoader) ([]service.Proxy, error) {
	out := append([]service.Proxy(nil), seeds...)
	if len(seeds) == 0 || load == nil {
		return out, nil
	}
	seen := make(map[int64]struct{}, len(seeds))
	for i := range seeds {
		if seeds[i].ID > 0 {
			seen[seeds[i].ID] = struct{}{}
		}
	}

	for start := 0; start < len(out); {
		pending := make([]int64, 0)
		end := len(out)
		for ; start < end; start++ {
			backupID := out[start].BackupProxyID
			if backupID == nil || *backupID <= 0 {
				continue
			}
			if _, ok := seen[*backupID]; ok {
				continue
			}
			seen[*backupID] = struct{}{}
			pending = append(pending, *backupID)
		}
		if len(pending) == 0 {
			continue
		}
		loaded, err := load(ctx, pending)
		if err != nil {
			return nil, err
		}
		out = append(out, loaded...)
	}
	return out, nil
}
