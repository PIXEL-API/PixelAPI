package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountShareRoomOperationScopeExpandRetainsLegacyGuard(t *testing.T) {
	content, err := FS.ReadFile("242_account_share_room_operation_scope_indexes_notx.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	upperSQL := strings.ToUpper(sql)

	require.Contains(
		t,
		sql,
		"CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_account_share_room_operations_open_room_listing",
	)
	require.Contains(t, sql, "ON account_share_room_operations(listing_id)")
	require.Contains(t, sql, "WHERE action <> 'end_membership'")
	require.Contains(t, sql, "status IN ('pending', 'running', 'needs_attention')")
	require.NotContains(t, sql, "DROP INDEX CONCURRENTLY IF EXISTS uq_account_share_room_operations_open_listing")
	require.NotContains(t, upperSQL, "BEGIN")
	require.NotContains(t, upperSQL, "COMMIT")
}

func TestAccountShareRoomOperationScopeContractDropsLegacyGuard(t *testing.T) {
	content, err := FS.ReadFile("250_account_share_room_operation_scope_contract_notx.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")

	require.Contains(t, sql, "DROP INDEX CONCURRENTLY IF EXISTS uq_account_share_room_operations_open_listing")
	require.NotContains(t, strings.ToUpper(sql), "BEGIN")
	require.NotContains(t, strings.ToUpper(sql), "COMMIT")
}
