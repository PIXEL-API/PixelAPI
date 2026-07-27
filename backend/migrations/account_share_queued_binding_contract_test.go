package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountShareQueuedBindingExpandKeepsLegacyAndDeferredRowsCompatible(t *testing.T) {
	content, err := FS.ReadFile("240_account_share_queued_binding_expand.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")

	require.Contains(t, sql, "ALTER COLUMN account_id DROP NOT NULL")
	require.Contains(t, sql, "OR status = 'queued'")
	require.NotContains(t, sql, "account_id = NULL WHERE status = 'queued'")
	require.NotContains(t, sql, "UPDATE account_share_membership_account_bindings")
	require.NotContains(t, strings.ToUpper(sql), "DELETE FROM ")
	require.NotContains(t, strings.ToUpper(sql), "TRUNCATE ")
}

func TestAccountShareQueuedBindingContractDefersAccountSelectionUntilActivation(t *testing.T) {
	content, err := FS.ReadFile("248_account_share_queued_binding_contract.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")

	require.Contains(t, sql, "CREATE OR REPLACE FUNCTION validate_account_share_membership_room_account()")
	require.Contains(t, sql, "NEW.status IN ('active', 'ending')")
	require.NotContains(t, sql, "NEW.status IN ('active', 'queued', 'ending')")
	require.Contains(t, sql, "UPDATE account_share_membership_account_bindings AS binding")
	require.Contains(t, sql, "membership.status = 'queued'")
	require.Contains(t, sql, "binding.unbound_at IS NULL")
	require.Contains(t, sql, "unbind_reason = 'queued_binding_deferred_migration'")
	require.Contains(t, sql, "queue_expires_at = COALESCE(queue_expires_at, created_at + INTERVAL '2 hours')")
	require.Contains(t, sql, "account_id = NULL WHERE status = 'queued'")
	require.Contains(t, sql, "(status = 'queued' AND account_id IS NULL)")
	require.Contains(t, sql, "(status IN ('active', 'ending') AND account_id IS NOT NULL)")
	require.Contains(t, sql, "status = 'ended'")
	require.Contains(t, sql, "ADD CONSTRAINT account_share_memberships_account_state_chk")
	require.Contains(t, sql, "NOT VALID")
	require.Contains(t, sql, "VALIDATE CONSTRAINT account_share_memberships_account_state_chk")
}
