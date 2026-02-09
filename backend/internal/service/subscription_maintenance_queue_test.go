//go:build unit

package service

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSubscriptionMaintenanceQueue_TryEnqueue_QueueFull(t *testing.T) {
	q := NewSubscriptionMaintenanceQueue(1, 1)
	t.Cleanup(q.Stop)

	block := make(chan struct{})
	var started atomic.Int32

	require.NoError(t, q.TryEnqueue(func() {
		started.Store(1)
		<-block
	}))

	// Wait until worker started consuming the first task.
	require.Eventually(t, func() bool { return started.Load() == 1 }, time.Second, 10*time.Millisecond)

	// Queue size is 1; with the worker blocked, enqueueing one more should fill it.
	require.NoError(t, q.TryEnqueue(func() {}))

	// Now the queue is full; next enqueue must fail.
	err := q.TryEnqueue(func() {})
	require.Error(t, err)
	require.Contains(t, err.Error(), "full")

	close(block)
}

func TestSubscriptionMaintenanceQueue_TryEnqueue_PanicDoesNotKillWorker(t *testing.T) {
	q := NewSubscriptionMaintenanceQueue(1, 8)
	t.Cleanup(q.Stop)

	require.NoError(t, q.TryEnqueue(func() { panic("boom") }))

	done := make(chan struct{})
	require.NoError(t, q.TryEnqueue(func() { close(done) }))

	select {
	case <-done:
		// ok
	case <-time.After(time.Second):
		t.Fatalf("worker did not continue after panic")
	}
}
