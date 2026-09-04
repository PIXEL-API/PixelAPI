package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func newOpenAIResponsesBodyBudgetForTest(t *testing.T, capacity int64, waitTimeoutSeconds int) *OpenAIResponsesBodyBudget {
	t.Helper()
	budget, err := NewOpenAIResponsesBodyBudget(config.GatewayOpenAIResponsesBodyBudgetConfig{
		Enabled:            true,
		CapacityBytes:      capacity,
		WaitTimeoutSeconds: waitTimeoutSeconds,
		ReadTimeoutSeconds: 1,
		RetryAfterSeconds:  1,
	})
	require.NoError(t, err)
	return budget
}

func TestOpenAIResponsesBodyBudgetAcquireReleaseIsIdempotent(t *testing.T) {
	budget := newOpenAIResponsesBodyBudgetForTest(t, 16, 1)
	lease, err := budget.Acquire(context.Background(), 12)
	require.NoError(t, err)
	require.Equal(t, int64(12), budget.Snapshot().InUseBytes)

	lease.Release()
	lease.Release()
	require.Equal(t, int64(0), budget.Snapshot().InUseBytes)

	second, err := budget.Acquire(context.Background(), 16)
	require.NoError(t, err, "double release must not corrupt semaphore capacity")
	second.Release()
}

func TestOpenAIResponsesBodyBudgetRejectsWhenCapacityStaysOccupied(t *testing.T) {
	budget := newOpenAIResponsesBodyBudgetForTest(t, 8, 1)
	lease, err := budget.Acquire(context.Background(), 8)
	require.NoError(t, err)
	defer lease.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = budget.Acquire(ctx, 1)
	require.Error(t, err)
	// The caller's shorter deadline is a cancellation, not a gate timeout.
	require.True(t, errors.Is(err, context.DeadlineExceeded))
	snapshot := budget.Snapshot()
	require.Equal(t, int64(8), snapshot.InUseBytes)
	require.Equal(t, uint64(1), snapshot.CanceledTotal)
}

func TestOpenAIResponsesBodyBudgetReportsGateTimeout(t *testing.T) {
	budget := newOpenAIResponsesBodyBudgetForTest(t, 8, 1)
	lease, err := budget.Acquire(context.Background(), 8)
	require.NoError(t, err)
	defer lease.Release()

	_, err = budget.Acquire(context.Background(), 1)
	require.ErrorIs(t, err, ErrOpenAIResponsesBodyBudgetExhausted)
	require.Equal(t, uint64(1), budget.Snapshot().RejectedTotal)
}

func TestNewOpenAIResponsesBodyBudgetDisabledAndInvalid(t *testing.T) {
	budget, err := NewOpenAIResponsesBodyBudget(config.GatewayOpenAIResponsesBodyBudgetConfig{})
	require.NoError(t, err)
	require.Nil(t, budget)

	_, err = NewOpenAIResponsesBodyBudget(config.GatewayOpenAIResponsesBodyBudgetConfig{Enabled: true})
	require.ErrorIs(t, err, ErrOpenAIResponsesBodyBudgetInvalid)
}
