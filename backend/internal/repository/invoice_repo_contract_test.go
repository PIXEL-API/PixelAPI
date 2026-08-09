package repository

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestInvoiceRequestQueryContractOmitsLegacyDeliveryFields(t *testing.T) {
	columns := strings.ToLower(invoiceRequestColumns)
	for _, field := range []string{
		"invoice_number",
		"invoice_code",
		"invoice_file_url",
		"invoice_file_name",
	} {
		require.NotContains(t, columns, field)
	}
	require.Contains(t, columns, "remark")

	where, args := buildInvoiceRequestWhere(service.InvoiceRequestListParams{
		Keyword: "request-or-buyer",
	}, false)
	require.Equal(t, []any{"%request-or-buyer%"}, args)
	require.Contains(t, where, "request_no ILIKE $1")
	require.Contains(t, where, "user_email ILIKE $1")
	require.Contains(t, where, "title_name ILIKE $1")
	require.NotContains(t, strings.ToLower(where), "invoice_number")
}
