package service

import "sync/atomic"

// ClusterConnectionSnapshot is the point-in-time number of active requests
// owned by this process. A streaming request is counted in exactly one bucket.
type ClusterConnectionSnapshot struct {
	HTTP      int64
	SSE       int64
	WebSocket int64
}

// ClusterConnectionTracker tracks process-local HTTP, SSE and WebSocket
// activity without putting locks on the gateway hot path.
type ClusterConnectionTracker struct {
	http      atomic.Int64
	sse       atomic.Int64
	webSocket atomic.Int64
}

func NewClusterConnectionTracker() *ClusterConnectionTracker {
	return &ClusterConnectionTracker{}
}

func (t *ClusterConnectionTracker) BeginHTTP() func() {
	if t == nil {
		return func() {}
	}
	t.http.Add(1)
	var finished atomic.Bool
	return func() {
		if finished.CompareAndSwap(false, true) {
			t.http.Add(-1)
		}
	}
}

func (t *ClusterConnectionTracker) BeginWebSocket() func() {
	if t == nil {
		return func() {}
	}
	t.webSocket.Add(1)
	var finished atomic.Bool
	return func() {
		if finished.CompareAndSwap(false, true) {
			t.webSocket.Add(-1)
		}
	}
}

// PromoteHTTPToSSE atomically moves an already-counted HTTP request into the
// SSE bucket. The returned function must replace the HTTP completion callback.
func (t *ClusterConnectionTracker) PromoteHTTPToSSE(finishHTTP func()) func() {
	if t == nil {
		return finishHTTP
	}
	finishHTTP()
	t.sse.Add(1)
	var finished atomic.Bool
	return func() {
		if finished.CompareAndSwap(false, true) {
			t.sse.Add(-1)
		}
	}
}

func (t *ClusterConnectionTracker) Snapshot() ClusterConnectionSnapshot {
	if t == nil {
		return ClusterConnectionSnapshot{}
	}
	return ClusterConnectionSnapshot{
		HTTP:      nonNegativeCounter(t.http.Load()),
		SSE:       nonNegativeCounter(t.sse.Load()),
		WebSocket: nonNegativeCounter(t.webSocket.Load()),
	}
}

func nonNegativeCounter(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}
