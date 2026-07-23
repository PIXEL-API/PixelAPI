package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClusterRuntimeMigrationIsSchemaOnlyAndFenced(t *testing.T) {
	content, err := FS.ReadFile("218_cluster_runtime.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")

	for _, table := range []string{
		"cluster_instances",
		"cluster_task_leases",
		"cluster_operations",
		"cluster_cache_versions",
	} {
		require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS "+table)
	}
	require.Contains(t, sql, "PRIMARY KEY (deployment_id, node_id)")
	require.Contains(t, sql, "CHECK (desired_state IN ('active', 'draining'))")
	require.Contains(t, sql, "CHECK (observed_state IN ('starting', 'ready', 'draining', 'unhealthy'))")
	require.Contains(t, sql, "cache_versions JSONB NOT NULL DEFAULT '{}'::jsonb")
	require.Contains(t, sql, "jsonb_typeof(cache_versions) = 'object'")
	require.Contains(t, sql, "jsonb_path_exists(")
	for _, metric := range []string{
		"memory_limit_bytes",
		"goroutine_count",
		"fd_open",
		"fd_limit",
		"db_idle_connections",
		"db_wait_count",
		"db_max_open_connections",
		"redis_idle_connections",
		"redis_pool_size",
	} {
		require.Contains(t, sql, metric+" >= 0")
	}
	require.Contains(t, sql, "fencing_token BIGINT NOT NULL DEFAULT 0")
	require.Contains(t, sql, "UNIQUE (deployment_id, idempotency_key)")
	require.Contains(t, sql, "attempt_token BIGINT NOT NULL DEFAULT 0")
	require.Contains(t, sql, "DEFAULT clock_timestamp()")

	upperSQL := strings.ToUpper(sql)
	require.NotContains(t, upperSQL, "ALTER TABLE USERS")
	require.NotContains(t, upperSQL, "ALTER TABLE ACCOUNTS")
	require.NotContains(t, upperSQL, "INSERT INTO USERS")
	require.NotContains(t, upperSQL, "INSERT INTO ACCOUNTS")
	require.NotContains(t, upperSQL, "DELETE FROM ")
	require.NotContains(t, upperSQL, "TRUNCATE ")
	require.NotContains(t, upperSQL, "DROP TABLE")
}
