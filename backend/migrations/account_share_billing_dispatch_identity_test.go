package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountShareBillingDispatchIdentityExpandKeepsLegacyWritesCompatible(t *testing.T) {
	content, err := FS.ReadFile("241_account_share_billing_dispatch_identity.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")

	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS client_request_id VARCHAR(255)")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS dispatch_id UUID")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS attempt_no INTEGER")
	require.Contains(t, sql, "MD5('account-share-billing-intent:' || id::text || ':' || request_id)::uuid")
	require.Contains(t, sql, "CREATE OR REPLACE FUNCTION fill_account_share_billing_dispatch_identity()")
	require.Contains(t, sql, "CREATE TRIGGER trg_fill_account_share_billing_dispatch_identity")
	require.NotContains(t, sql, "ALTER COLUMN client_request_id SET NOT NULL")
	require.NotContains(t, sql, "DROP CONSTRAINT IF EXISTS uq_account_share_request_billing_intent")
	require.Contains(t, sql, "uq_account_share_billing_intent_dispatch")
	require.Contains(t, sql, "uq_account_share_billing_intent_client_attempt")
	require.Contains(t, sql, "attempt_no > 0")
	require.NotContains(t, strings.ToUpper(sql), "DELETE FROM ")
	require.NotContains(t, strings.ToUpper(sql), "TRUNCATE ")
}

func TestAccountShareBillingDispatchIdentityContractEnforcesNewIdentity(t *testing.T) {
	content, err := FS.ReadFile("249_account_share_billing_dispatch_identity_contract.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")

	require.Contains(t, sql, "ALTER COLUMN client_request_id SET NOT NULL")
	require.Contains(t, sql, "ALTER COLUMN dispatch_id SET NOT NULL")
	require.Contains(t, sql, "ALTER COLUMN attempt_no SET NOT NULL")
	require.Contains(t, sql, "DROP CONSTRAINT IF EXISTS uq_account_share_request_billing_intent")
	require.Contains(t, sql, "DROP TRIGGER IF EXISTS trg_fill_account_share_billing_dispatch_identity")
}
