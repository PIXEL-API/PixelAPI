package config

import (
	"testing"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesBodyBudgetValidation(t *testing.T) {
	resetViperWithJWTSecret(t)
	base, err := Load()
	require.NoError(t, err)
	require.False(t, base.Gateway.OpenAIResponsesBodyBudget.Enabled)

	valid := base.Gateway.OpenAIResponsesBodyBudget
	valid.Enabled = true
	valid.CapacityBytes = pkghttputil.BoundedRequestBodyMemoryReservation(base.Gateway.MaxBodySize)
	valid.WaitTimeoutSeconds = 1
	valid.ReadTimeoutSeconds = 30
	valid.RetryAfterSeconds = 1
	base.Gateway.OpenAIResponsesBodyBudget = valid
	require.NoError(t, base.Validate())

	tests := []struct {
		name    string
		mutate  func(*GatewayOpenAIResponsesBodyBudgetConfig)
		wantErr string
	}{
		{
			name:    "capacity missing",
			mutate:  func(cfg *GatewayOpenAIResponsesBodyBudgetConfig) { cfg.CapacityBytes = 0 },
			wantErr: "capacity_bytes must be positive",
		},
		{
			name:    "capacity below one legal body",
			mutate:  func(cfg *GatewayOpenAIResponsesBodyBudgetConfig) { cfg.CapacityBytes = base.Gateway.MaxBodySize - 1 },
			wantErr: "capacity_bytes must cover the maximum bounded or compressed request memory reservation",
		},
		{
			name:    "wait timeout missing",
			mutate:  func(cfg *GatewayOpenAIResponsesBodyBudgetConfig) { cfg.WaitTimeoutSeconds = 0 },
			wantErr: "wait_timeout_seconds must be positive",
		},
		{
			name:    "read timeout missing",
			mutate:  func(cfg *GatewayOpenAIResponsesBodyBudgetConfig) { cfg.ReadTimeoutSeconds = 0 },
			wantErr: "read_timeout_seconds must be positive",
		},
		{
			name:    "retry after missing",
			mutate:  func(cfg *GatewayOpenAIResponsesBodyBudgetConfig) { cfg.RetryAfterSeconds = 0 },
			wantErr: "retry_after_seconds must be positive",
		},
	}

	t.Run("capacity also covers compressed request memory", func(t *testing.T) {
		candidate := *base
		candidate.Gateway.MaxBodySize = pkghttputil.MaxZstdRequestBodyMemoryReservation / 2
		budget := valid
		budget.CapacityBytes = candidate.Gateway.MaxBodySize
		candidate.Gateway.OpenAIResponsesBodyBudget = budget
		require.ErrorContains(t, candidate.Validate(), "maximum bounded or compressed request memory reservation")
	})

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			candidate := *base
			budget := valid
			testCase.mutate(&budget)
			candidate.Gateway.OpenAIResponsesBodyBudget = budget
			require.ErrorContains(t, candidate.Validate(), testCase.wantErr)
		})
	}
}
