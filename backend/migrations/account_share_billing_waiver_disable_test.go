package migrations

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDisableAccountShareBillingAdminWaiversMigrationBlocksInsert(t *testing.T) {
	content, err := os.ReadFile("254_disable_account_share_billing_admin_waivers.sql")
	require.NoError(t, err)
	normalized := strings.ToLower(string(content))

	require.Contains(t, normalized, "before insert or update or delete")
	require.Contains(t, normalized, "guard_account_share_billing_admin_waiver()")
	require.NotContains(t, normalized, "drop table")
	require.NotContains(t, normalized, "delete from")
	require.NotContains(t, normalized, "truncate ")
}
