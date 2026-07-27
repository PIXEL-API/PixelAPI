package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountShareBillingIntentAdminResolutionMigrationIsAuditedAndImmutable(t *testing.T) {
	content, err := FS.ReadFile("246_account_share_billing_intent_admin_resolution.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	lowerSQL := strings.ToLower(sql)
	upperSQL := strings.ToUpper(sql)

	for _, required := range []string{
		"set local lock_timeout = '2s'",
		"set local statement_timeout = '60s'",
		"account_share_billing_intent_admin_waivers",
		"actor_user_id_snapshot",
		"previous_state_token",
		"resulting_state_token",
		"admin_waiver_audit_id",
		"previous_status = 'needs_attention'",
		"resulting_status = 'cancelled'",
		"before update or delete",
		"before truncate",
		"on delete restrict",
		"on delete set null",
		"new.status = 'cancelled' and admin_waiver_valid",
		"new.client_request_id",
		"new.dispatch_id",
		"new.attempt_no",
		"old.client_request_id",
		"old.dispatch_id",
		"old.attempt_no",
	} {
		require.Contains(t, lowerSQL, required)
	}

	require.Contains(t, sql, "DROP CONSTRAINT IF EXISTS account_share_billing_intent_forward_chk")
	require.Contains(t, sql, "DROP CONSTRAINT IF EXISTS account_share_billing_intent_cancel_chk")
	require.Contains(t, sql, "VALIDATE CONSTRAINT account_share_billing_intent_forward_chk")
	require.Contains(t, sql, "VALIDATE CONSTRAINT account_share_billing_intent_cancel_chk")

	for _, forbidden := range []string{
		"INSERT INTO wallets",
		"UPDATE wallets",
		"INSERT INTO usage_logs",
		"UPDATE usage_logs",
		"UPDATE account_share_request_billing_intents SET",
		"DELETE FROM account_share",
		"TRUNCATE account_share",
	} {
		require.NotContains(t, upperSQL, strings.ToUpper(forbidden))
	}
}
