package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupUsageCostCatchupMigrationContract(t *testing.T) {
	content, err := FS.ReadFile("271_group_usage_cost_catchup.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	require.Contains(t, sql, "create table if not exists group_usage_cost_catchup")
	require.Contains(t, sql, "usage_log_id bigint primary key")
	require.Contains(t, sql, "referencing new table as new_group_usage_logs")
	require.Contains(t, sql, "for each statement")
	require.Contains(t, sql, "on conflict (usage_log_id) do nothing")
}

func TestGroupUsageCostTotalsMigrationContract(t *testing.T) {
	content, err := FS.ReadFile("272_group_usage_cost_totals.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(content))

	require.Contains(t, sql, "create table if not exists group_usage_cost_totals")
	require.Contains(t, sql, "set transaction isolation level read committed")
	require.Contains(t, sql, "set local lock_timeout = '5s'")
	require.Contains(t, sql, "set local statement_timeout = '15s'")
	require.Contains(t, sql, "from usage_logs logs")
	require.Contains(t, sql, "from group_usage_cost_catchup catchup")
	require.Contains(t, sql, "delete from group_usage_cost_catchup")
	require.Contains(t, sql, "returning group_id, actual_cost")
	require.NotContains(t, sql, "usage_daily_dimension_snapshots")
	require.Contains(t, sql, "lock table usage_logs in share row exclusive mode")
	require.Contains(t, sql, "total_cost = group_usage_cost_totals.total_cost + excluded.total_cost")
	require.Contains(t, sql, "referencing new table as new_group_usage_logs")
	require.Contains(t, sql, "drop table group_usage_cost_catchup")

	baselinePosition := strings.Index(sql, "from usage_logs logs")
	preDrainPosition := strings.Index(sql, "delete from group_usage_cost_catchup")
	lockPosition := strings.Index(sql, "lock table usage_logs")
	catchupMergePosition := strings.LastIndex(sql, "from group_usage_cost_catchup")
	directTriggerPosition := strings.Index(sql, "create trigger trg_increment_group_usage_cost_totals")
	dropCatchupPosition := strings.Index(sql, "drop table group_usage_cost_catchup")
	require.Greater(t, lockPosition, baselinePosition, "write lock must be acquired after the long baseline scan")
	require.Greater(t, preDrainPosition, baselinePosition, "catch-up rows must be pre-drained after the baseline snapshot")
	require.Greater(t, lockPosition, preDrainPosition, "writers must only be blocked after the catch-up pre-drain")
	require.Greater(t, catchupMergePosition, lockPosition, "catch-up rows must be merged after writers are drained")
	require.Greater(t, directTriggerPosition, catchupMergePosition, "direct trigger must replace catch-up after its final merge")
	require.Greater(t, dropCatchupPosition, directTriggerPosition, "temporary catch-up state must be removed last")
}
