//go:build unit

package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGrokBillingSnapshotIsSchedulerNeutral(t *testing.T) {
	t.Parallel()

	require.False(t, shouldEnqueueSchedulerOutboxForExtraUpdates(map[string]any{
		"grok_billing_snapshot": map[string]any{"status_code": float64(200)},
	}))
	require.True(t, shouldEnqueueSchedulerOutboxForExtraUpdates(map[string]any{
		service.GrokMediaEligibleExtraKey: true,
	}), "operator eligibility overrides must still rebuild affected scheduler buckets")
}
