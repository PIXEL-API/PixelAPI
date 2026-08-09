package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInvoiceRemarkMigrationHasBoundedLockWait(t *testing.T) {
	content, err := FS.ReadFile("267_add_invoice_remarks.sql")
	require.NoError(t, err)

	sql := strings.ToLower(string(content))
	require.Contains(t, sql, "set local lock_timeout = '2s'")
	require.Contains(t, sql, "alter table invoice_profiles")
	require.Contains(t, sql, "alter table invoice_requests")
}

func TestInvoiceLegacyDeliveryMigrationIsExplicitAndBounded(t *testing.T) {
	content, err := FS.ReadFile("268_drop_invoice_legacy_delivery_fields.sql")
	require.NoError(t, err)

	sql := strings.ToLower(string(content))
	require.Contains(t, sql, "set local lock_timeout = '2s'")
	require.NotContains(t, sql, "if exists")
	for _, field := range []string{
		"invoice_number",
		"invoice_code",
		"invoice_file_url",
		"invoice_file_name",
	} {
		require.Contains(t, sql, "drop column "+field)
	}
}
