package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInvoiceRequestJSONOmitsLegacyDeliveryFields(t *testing.T) {
	payload, err := json.Marshal(InvoiceRequest{})
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))
	for _, field := range []string{
		"invoice_number",
		"invoice_code",
		"invoice_file_url",
		"invoice_file_name",
	} {
		require.NotContains(t, decoded, field)
	}
	require.Contains(t, decoded, "remark")
}
