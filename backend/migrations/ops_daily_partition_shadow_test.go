package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const opsDailyPartitionShadowMigration = "215_ops_daily_partition_shadow.sql"

func readNormalizedOpsDailyPartitionShadowMigration(t *testing.T) string {
	t.Helper()

	content, err := FS.ReadFile(opsDailyPartitionShadowMigration)
	require.NoError(t, err)
	return strings.Join(strings.Fields(string(content)), " ")
}

func TestOpsDailyPartitionShadowMigrationCreatesOnlyEmptyParents(t *testing.T) {
	sql := readNormalizedOpsDailyPartitionShadowMigration(t)

	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS public.ops_system_logs_daily_shadow")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS public.ops_error_logs_daily_shadow")
	require.Contains(t, sql, "PARTITION BY RANGE (created_at)")
	require.Contains(t, sql, "PRIMARY KEY (created_at, id)")
	require.Contains(t, sql, "id_index_name := parent_name || '_id_idx'")
	require.Contains(t, sql, "'CREATE INDEX %I ON public.%I (id)'")
	require.Contains(t, sql, "ALTER TABLE public.ops_system_logs_daily_shadow ALTER COLUMN id DROP DEFAULT")
	require.Contains(t, sql, "ALTER TABLE public.ops_error_logs_daily_shadow ALTER COLUMN id DROP DEFAULT")

	upperSQL := strings.ToUpper(sql)
	require.NotContains(t, upperSQL, "ALTER TABLE PUBLIC.OPS_SYSTEM_LOGS RENAME")
	require.NotContains(t, upperSQL, "ALTER TABLE PUBLIC.OPS_ERROR_LOGS RENAME")
	require.NotContains(t, upperSQL, "ATTACH PARTITION")
	require.NotContains(t, upperSQL, "INSERT INTO PUBLIC.OPS_SYSTEM_LOGS_DAILY_SHADOW")
	require.NotContains(t, upperSQL, "INSERT INTO PUBLIC.OPS_ERROR_LOGS_DAILY_SHADOW")
	require.NotContains(t, upperSQL, "CREATE TABLE AS")
	require.NotContains(t, upperSQL, "COPY ")
}

func TestOpsDailyPartitionShadowMigrationRequiresExplicitUTCDay(t *testing.T) {
	sql := readNormalizedOpsDailyPartitionShadowMigration(t)

	require.Contains(t, sql, "CREATE OR REPLACE FUNCTION public.create_ops_daily_shadow_partitions(p_day_start timestamptz)")
	require.Contains(t, sql, "SET \"TimeZone\" = 'UTC'")
	require.Contains(t, sql, "p_day_start must be an exact UTC day boundary")
	require.Contains(t, sql, `YYYY-MM-DD"T"HH24:MI:SS"Z"`)
	require.Contains(t, sql, "FOR VALUES FROM (%L::timestamptz) TO (%L::timestamptz)")
	require.Contains(t, sql, "REVOKE ALL ON FUNCTION public.create_ops_daily_shadow_partitions(timestamptz) FROM PUBLIC")
}

func TestOpsDailyPartitionShadowMigrationDoesNotInvokePartitionCreation(t *testing.T) {
	content, err := FS.ReadFile(opsDailyPartitionShadowMigration)
	require.NoError(t, err)

	sql := string(content)
	require.Equal(t, 3, strings.Count(sql, "create_ops_daily_shadow_partitions"),
		"the helper may only appear in its declaration and privilege/comment signature")
	require.NotContains(t, sql, "SELECT public.create_ops_daily_shadow_partitions(")
	require.NotContains(t, sql, "PERFORM public.create_ops_daily_shadow_partitions(")
}
