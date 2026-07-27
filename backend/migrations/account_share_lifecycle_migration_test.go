package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountShareLifecycleExpandAddsStateAndDeletionGuards(t *testing.T) {
	content, err := FS.ReadFile("237_account_share_lifecycle_expand.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")

	require.Contains(t, sql, "status IN ('validating', 'active', 'draining', 'paused', 'disabled', 'suspended')")
	require.Contains(t, sql, "status IN ('active', 'queued', 'ending', 'ended')")
	require.Contains(t, sql, "status IN ('active', 'queued', 'ending') AND ended_at IS NULL")
	require.Contains(t, sql, "queue_expires_at TIMESTAMPTZ")
	require.Contains(t, sql, "ending_operation_id UUID")
	require.Contains(t, sql, "membership_id BIGINT")
	require.Contains(t, sql, "validate_account_share_membership_listing_live")
	require.Contains(t, sql, "prevent_account_share_room_delete_with_live_memberships")
	require.Contains(t, sql, "membership.status IN ('active', 'queued', 'ending')")
	require.Contains(t, sql, "NEW.status IN ('active', 'queued', 'ending')")
	require.NotContains(t, sql, "SET status = 'suspended'")
	require.NotContains(t, strings.ToUpper(sql), "DELETE FROM ")
	require.NotContains(t, strings.ToUpper(sql), "TRUNCATE ")
}

func TestAccountShareLifecycleContractRetiresLegacyDisabledState(t *testing.T) {
	content, err := FS.ReadFile("251_account_share_lifecycle_contract.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")

	require.Contains(t, sql, "SET status = 'suspended'")
	require.Contains(t, sql, "WHERE status = 'disabled'")
	require.Contains(t, sql, "status IN ('validating', 'active', 'draining', 'paused', 'suspended')")
	require.NotContains(t, sql, "'disabled', 'suspended'")
}

func TestAccountShareLifecycleIndexesAreConcurrentAndIndependentFromAccountCapacity(t *testing.T) {
	content, err := FS.ReadFile("238_account_share_lifecycle_indexes_notx.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	upperSQL := strings.ToUpper(sql)

	require.Contains(t, sql, "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_account_share_memberships_live_consumer")
	require.Contains(t, sql, "WHERE status IN ('active', 'ending') AND deleted_at IS NULL")
	require.Contains(t, sql, "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_account_share_memberships_live_listing_consumer")
	require.Contains(t, sql, "WHERE status IN ('active', 'queued', 'ending') AND deleted_at IS NULL")
	require.Contains(t, sql, "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_account_share_memberships_queue_expiry")
	require.Contains(t, sql, "ON public.account_share_memberships(consumer_user_id)")
	require.Contains(t, sql, "ON public.account_share_memberships(api_key_id)")
	require.Contains(t, sql, "ON public.account_share_memberships(listing_id, consumer_user_id)")
	require.Contains(t, sql, "ON public.account_share_memberships(queue_expires_at, id)")
	require.Contains(t, sql, "ON public.account_share_room_operations(membership_id)")
	require.NotContains(t, sql, "ON account_share_memberships")
	require.NotContains(t, sql, "ON account_share_room_operations")
	guardNames := []string{
		"uq_as_memberships_live_consumer_rebuild_guard",
		"uq_as_memberships_live_api_key_rebuild_guard",
		"uq_as_memberships_live_listing_consumer_rebuild_guard",
	}
	for _, guardName := range guardNames {
		require.Contains(t, sql, "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS "+guardName)
	}
	for _, indexName := range []string{
		"uq_account_share_memberships_live_consumer",
		"uq_account_share_memberships_live_api_key",
		"uq_account_share_memberships_live_listing_consumer",
		"uq_account_share_room_operations_open_membership",
		"idx_account_share_memberships_queue_expiry",
	} {
		createAt := strings.Index(sql, "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS "+indexName)
		if createAt == -1 {
			createAt = strings.Index(sql, "CREATE INDEX CONCURRENTLY IF NOT EXISTS "+indexName)
		}
		require.NotEqual(t, -1, createAt, indexName)
		require.NotContains(t, sql, "DROP INDEX CONCURRENTLY IF EXISTS "+indexName)
	}
	for i, targetName := range []string{
		"uq_account_share_memberships_live_consumer",
		"uq_account_share_memberships_live_api_key",
		"uq_account_share_memberships_live_listing_consumer",
	} {
		require.Less(
			t,
			strings.Index(sql, "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS "+guardNames[i]),
			strings.Index(sql, "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS "+targetName),
			"temporary uniqueness guard must precede "+targetName,
		)
	}
	require.NotContains(t, sql, "seat_limit")
	require.NotContains(t, sql, "per_user_concurrency")
	require.NotContains(t, sql, "configured_concurrency")
	require.NotContains(t, upperSQL, "BEGIN")
	require.NotContains(t, upperSQL, "COMMIT")
}
