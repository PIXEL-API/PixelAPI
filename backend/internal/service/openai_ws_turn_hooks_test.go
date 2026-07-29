package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestApplyOpenAIWSFixedTurnModel(t *testing.T) {
	t.Parallel()

	hooks := &OpenAIWSIngressHooks{
		FixedRequestedModel: "alias-model",
		FixedRoutingModel:   "gpt-5.4",
	}

	t.Run("maps every response create to the opening routing model", func(t *testing.T) {
		payload, err := applyOpenAIWSFixedTurnModel(
			[]byte(`{"type":"response.create","model":"alias-model","input":"next"}`),
			hooks,
		)
		require.NoError(t, err)
		require.JSONEq(t, `{"type":"response.create","model":"gpt-5.4","input":"next"}`, string(payload))
	})

	t.Run("accepts a client echo of the already mapped routing model", func(t *testing.T) {
		original := []byte(`{"type":"response.create","model":"gpt-5.4","input":"next"}`)
		payload, err := applyOpenAIWSFixedTurnModel(original, hooks)
		require.NoError(t, err)
		require.Equal(t, original, payload)
	})

	t.Run("maps session update without allowing a model switch", func(t *testing.T) {
		payload, err := applyOpenAIWSFixedTurnModel(
			[]byte(`{"type":"session.update","session":{"model":"alias-model"}}`),
			hooks,
		)
		require.NoError(t, err)
		require.JSONEq(t, `{"type":"session.update","session":{"model":"gpt-5.4"}}`, string(payload))
	})

	t.Run("rejects a later response create that changes model", func(t *testing.T) {
		_, err := applyOpenAIWSFixedTurnModel(
			[]byte(`{"type":"response.create","model":"different-model"}`),
			hooks,
		)
		require.Error(t, err)
		var closeErr *OpenAIWSClientCloseError
		require.ErrorAs(t, err, &closeErr)
		require.Equal(t, coderws.StatusPolicyViolation, closeErr.StatusCode())
		require.Contains(t, closeErr.Reason(), "changing model")
	})

	t.Run("leaves sessions without a fixed model unchanged", func(t *testing.T) {
		original := []byte(`{"type":"response.create","model":"different-model"}`)
		payload, err := applyOpenAIWSFixedTurnModel(original, &OpenAIWSIngressHooks{})
		require.NoError(t, err)
		require.Equal(t, original, payload)
	})
}

func TestOpenAIWSPassthroughTurnLifecyclePayloadHooksAreTurnScoped(t *testing.T) {
	t.Parallel()

	type turnContextKey struct{}
	var (
		mu             sync.Mutex
		beforePayloads []string
		afterPayloads  []string
		legacyBefore   []int
		legacyAfter    []int
	)
	hooks := &OpenAIWSIngressHooks{
		BeforeTurn: func(turn int) error {
			mu.Lock()
			legacyBefore = append(legacyBefore, turn)
			mu.Unlock()
			return nil
		},
		AfterTurn: func(turn int, _ *OpenAIForwardResult, _ error) {
			mu.Lock()
			legacyAfter = append(legacyAfter, turn)
			mu.Unlock()
		},
		BeforeTurnPayload: func(turn int, payload []byte) (context.Context, error) {
			mu.Lock()
			beforePayloads = append(beforePayloads, string(payload))
			mu.Unlock()
			return context.WithValue(context.Background(), turnContextKey{}, turn), nil
		},
		AfterTurnPayload: func(_ int, payload []byte, _ *OpenAIForwardResult, _ error) error {
			mu.Lock()
			afterPayloads = append(afterPayloads, string(payload))
			mu.Unlock()
			return nil
		},
	}
	lifecycle := newOpenAIWSPassthroughTurnLifecycleWithContext(context.Background(), hooks, nil)

	firstPayload := []byte(`{"type":"response.create","model":"gpt-5.1","input":"first"}`)
	firstTurn, err := lifecycle.begin(firstPayload)
	require.NoError(t, err)
	require.Equal(t, 1, firstTurn)
	lifecycle.mu.Lock()
	firstContextTurn := lifecycle.activeContext.Value(turnContextKey{})
	lifecycle.mu.Unlock()
	require.Equal(t, 1, firstContextTurn)
	firstPayload[0] = '['
	finishedTurn, finished, finishErr := lifecycle.finishWithError(&OpenAIForwardResult{RequestID: "resp_1"}, nil)
	require.NoError(t, finishErr)
	require.True(t, finished)
	require.Equal(t, 1, finishedTurn)

	secondPayload := []byte(`{"type":"response.create","model":"gpt-5.1","input":"second"}`)
	secondTurn, err := lifecycle.begin(secondPayload)
	require.NoError(t, err)
	require.Equal(t, 2, secondTurn)
	lifecycle.mu.Lock()
	secondContextTurn := lifecycle.activeContext.Value(turnContextKey{})
	lifecycle.mu.Unlock()
	require.Equal(t, 2, secondContextTurn)
	finishedTurn, finished, finishErr = lifecycle.finishWithError(&OpenAIForwardResult{RequestID: "resp_2"}, nil)
	require.NoError(t, finishErr)
	require.True(t, finished)
	require.Equal(t, 2, finishedTurn)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []int{1, 2}, legacyBefore)
	require.Equal(t, []int{1, 2}, legacyAfter)
	require.Equal(t, []string{
		`{"type":"response.create","model":"gpt-5.1","input":"first"}`,
		`{"type":"response.create","model":"gpt-5.1","input":"second"}`,
	}, beforePayloads)
	require.Equal(t, beforePayloads, afterPayloads)
}

func TestOpenAIWSPassthroughTurnLifecyclePropagatesTurnContextLossOnce(t *testing.T) {
	t.Parallel()

	turnCtx, cancelTurn := context.WithCancelCause(context.Background())
	contextDone := make(chan error, 1)
	var (
		mu         sync.Mutex
		afterCalls int
		afterErr   error
	)
	lifecycle := newOpenAIWSPassthroughTurnLifecycleWithContext(
		context.Background(),
		&OpenAIWSIngressHooks{
			BeforeTurnPayload: func(_ int, _ []byte) (context.Context, error) {
				return turnCtx, nil
			},
			AfterTurnPayload: func(_ int, _ []byte, _ *OpenAIForwardResult, turnErr error) error {
				mu.Lock()
				afterCalls++
				afterErr = turnErr
				mu.Unlock()
				return nil
			},
		},
		func(cause error) {
			contextDone <- cause
		},
	)

	turn, err := lifecycle.begin([]byte(`{"type":"response.create","model":"gpt-5.1"}`))
	require.NoError(t, err)
	require.Equal(t, 1, turn)
	cancelTurn(ErrAccountShareRuntimeLeaseLost)
	select {
	case cause := <-contextDone:
		require.ErrorIs(t, cause, ErrAccountShareRuntimeLeaseLost)
	case <-time.After(time.Second):
		t.Fatal("turn context cancellation callback did not run")
	}
	require.ErrorIs(t, lifecycle.activeCause(), ErrAccountShareRuntimeLeaseLost)

	finishedTurn, finished, finishErr := lifecycle.finishWithError(nil, lifecycle.activeCause())
	require.NoError(t, finishErr)
	require.True(t, finished)
	require.Equal(t, 1, finishedTurn)
	_, finished, finishErr = lifecycle.finishWithError(nil, errors.New("duplicate"))
	require.NoError(t, finishErr)
	require.False(t, finished)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, afterCalls)
	require.ErrorIs(t, afterErr, ErrAccountShareRuntimeLeaseLost)
}

func TestOpenAIWSPassthroughTurnLifecycleEnforcesCompleteUsageBeforeTerminal(t *testing.T) {
	tests := []struct {
		name          string
		result        *OpenAIForwardResult
		wantError     bool
		wantGateCalls int
	}{
		{
			name:      "partial usage is rejected",
			result:    &OpenAIForwardResult{RequestID: "resp_partial", Usage: OpenAIUsage{InputTokens: 1}},
			wantError: true,
		},
		{
			name: "explicit zero usage is accepted",
			result: &OpenAIForwardResult{
				RequestID:            "resp_zero",
				BillingUsageComplete: true,
			},
			wantGateCalls: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateCalls := 0
			turnCtx := WithOpenAIForwardResultBillingGate(context.Background(), NewOpenAIForwardResultBillingGate(func(*OpenAIForwardResult) error {
				gateCalls++
				return nil
			}))
			lifecycle := newOpenAIWSPassthroughTurnLifecycleWithContext(
				context.Background(),
				&OpenAIWSIngressHooks{
					BeforeTurnPayload: func(_ int, _ []byte) (context.Context, error) {
						return turnCtx, nil
					},
					AfterTurnPayload: func(_ int, _ []byte, _ *OpenAIForwardResult, turnErr error) error {
						return turnErr
					},
				},
				nil,
			)
			_, err := lifecycle.begin([]byte(`{"type":"response.create","model":"gpt-5.1"}`))
			require.NoError(t, err)

			_, finished, finishErr := lifecycle.finishWithError(tt.result, nil)

			require.True(t, finished)
			if tt.wantError {
				require.ErrorIs(t, finishErr, ErrAccountShareBillingPreTerminalCommit)
			} else {
				require.NoError(t, finishErr)
			}
			require.Equal(t, tt.wantGateCalls, gateCalls)
		})
	}
}
