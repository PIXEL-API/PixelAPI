package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	proxyExpiryFallbackExpandMigration   = "274_proxy_expiry_fallback_expand.sql"
	proxyExpiryFallbackIndexMigration    = "275_proxy_expiry_fallback_indexes_notx.sql"
	proxyExpiryFallbackValidateMigration = "276_validate_proxy_expiry_fallback_constraints.sql"
	proxyFallbackForeignKeyConstraint    = "proxies_proxies_fallback_sources"
)

func readNormalizedProxyExpiryMigration(t *testing.T, name string) string {
	t.Helper()
	content, err := FS.ReadFile(name)
	require.NoError(t, err, "proxy expiry migration must be embedded")
	return strings.ToLower(strings.Join(strings.Fields(string(content)), " "))
}

func TestProxyExpiryFallbackExpandMigrationIsAdditiveAndConstrained(t *testing.T) {
	sql := readNormalizedProxyExpiryMigration(t, proxyExpiryFallbackExpandMigration)

	require.Contains(t, sql, "set local lock_timeout")
	require.Contains(t, sql, "alter table proxies")
	require.Contains(t, sql, "add column if not exists expires_at timestamptz")
	require.Contains(t, sql, "add column if not exists fallback_mode")
	require.Contains(t, sql, "add column if not exists backup_proxy_id bigint")
	require.Contains(t, sql, "add column if not exists expiry_warn_days")
	require.Contains(t, sql, "add constraint "+proxyFallbackForeignKeyConstraint)
	require.Contains(t, sql, "references proxies(id) on delete set null")
	require.Contains(t, sql, "fallback_mode in ('none', 'direct', 'proxy')")
	require.Contains(t, sql, "expiry_warn_days >= 0")
	require.Contains(t, sql, "backup_proxy_id <> id")
	require.Contains(t, sql, "alter table accounts")
	require.Contains(t, sql, "add column if not exists proxy_fallback_origin_id bigint")

	for _, forbidden := range []string{"drop table", "drop column", "delete from", "truncate"} {
		require.NotContains(t, sql, forbidden, "expand migration must remain additive")
	}
}

func TestProxyExpiryFallbackValidationUsesExpandConstraintNames(t *testing.T) {
	sql := readNormalizedProxyExpiryMigration(t, proxyExpiryFallbackValidateMigration)

	require.Contains(t, sql, "set local lock_timeout")
	require.Contains(t, sql, "set local statement_timeout")
	for _, constraint := range []string{
		"proxies_fallback_mode_check",
		"proxies_expiry_warn_days_check",
		"proxies_backup_proxy_not_self_check",
		proxyFallbackForeignKeyConstraint,
	} {
		require.Contains(t, sql, "alter table proxies validate constraint "+constraint)
	}

	for _, forbidden := range []string{"drop table", "drop column", "delete from", "truncate"} {
		require.NotContains(t, sql, forbidden, "constraint validation migration must not mutate business rows")
	}
}

func TestProxyExpiryFallbackIndexesAreOnlineAndNonDestructive(t *testing.T) {
	sql := readNormalizedProxyExpiryMigration(t, proxyExpiryFallbackIndexMigration)

	// *_notx.sql 迁移由 runner 统一注入 session 级 lock_timeout/statement_timeout
	// （见 executeNonTransactionalMigration），文件本身只允许 CREATE/DROP INDEX
	// CONCURRENTLY 语句，因此这里不要求文件内出现 SET lock_timeout/statement_timeout。
	require.Contains(t, sql, "create index concurrently if not exists")
	require.Contains(t, sql, "on proxies (expires_at)")
	require.Contains(t, sql, "on proxies (backup_proxy_id)")
	require.Contains(t, sql, "on accounts (proxy_fallback_origin_id)")

	for _, forbidden := range []string{"drop table", "drop column", "delete from", "truncate"} {
		require.NotContains(t, sql, forbidden, "online index migration must not mutate business rows")
	}
}
