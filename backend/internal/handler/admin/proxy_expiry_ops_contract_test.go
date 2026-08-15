package admin

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpsAlertValidationAcceptsProxyExpiryMetrics(t *testing.T) {
	for _, metricType := range []string{"proxy_expired_count", "proxy_expiring_soon_count"} {
		raw := map[string]json.RawMessage{
			"name":        json.RawMessage(`"proxy lifecycle"`),
			"metric_type": json.RawMessage(`"` + metricType + `"`),
			"operator":    json.RawMessage(`">="`),
			"threshold":   json.RawMessage(`1`),
		}

		validated, err := validateOpsAlertRulePayload(raw)

		require.NoError(t, err)
		require.Equal(t, metricType, validated.MetricType)
	}
}
