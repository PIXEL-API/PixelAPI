package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountShareOperationStillPendingRecognizesManagementState(t *testing.T) {
	t.Run("pending operation", func(t *testing.T) {
		state := &service.AccountShareRoomManagementState{PendingOperationID: "operation-1"}
		require.True(t, accountShareOperationStillPending(state))
	})

	t.Run("no pending operation", func(t *testing.T) {
		state := &service.AccountShareRoomManagementState{}
		require.False(t, accountShareOperationStillPending(state))
	})

	t.Run("idempotency replay map", func(t *testing.T) {
		require.True(t, accountShareOperationStillPending(map[string]any{
			"pending_operation_id": "operation-1",
		}))
	})
}
