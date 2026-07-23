package server

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type clusterConnectionTrackingHandler struct {
	next    http.Handler
	tracker *service.ClusterConnectionTracker
}

func newClusterConnectionTrackingHandler(next http.Handler, tracker *service.ClusterConnectionTracker) http.Handler {
	if next == nil || tracker == nil {
		return next
	}
	return &clusterConnectionTrackingHandler{next: next, tracker: tracker}
}

func (h *clusterConnectionTrackingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if isWebSocketRequest(r) {
		finish := h.tracker.BeginWebSocket()
		defer finish()
		h.next.ServeHTTP(w, r)
		return
	}

	finish := h.tracker.BeginHTTP()
	trackedWriter := &clusterTrackedResponseWriter{
		ResponseWriter: w,
		onSSE: func() {
			finish = h.tracker.PromoteHTTPToSSE(finish)
		},
	}
	defer func() {
		finish()
	}()
	h.next.ServeHTTP(trackedWriter, r)
}

func isWebSocketRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
		return false
	}
	return strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

type clusterTrackedResponseWriter struct {
	http.ResponseWriter
	onSSE   func()
	sseOnce sync.Once
}

func (w *clusterTrackedResponseWriter) detectSSE() {
	if w == nil || w.ResponseWriter == nil {
		return
	}
	contentType := strings.ToLower(strings.TrimSpace(w.Header().Get("Content-Type")))
	if !strings.HasPrefix(contentType, "text/event-stream") {
		return
	}
	w.sseOnce.Do(func() {
		if w.onSSE != nil {
			w.onSSE()
		}
	})
}

func (w *clusterTrackedResponseWriter) WriteHeader(statusCode int) {
	w.detectSSE()
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *clusterTrackedResponseWriter) Write(p []byte) (int, error) {
	w.detectSSE()
	return w.ResponseWriter.Write(p)
}

func (w *clusterTrackedResponseWriter) Flush() {
	w.detectSSE()
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *clusterTrackedResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func (w *clusterTrackedResponseWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}

func (w *clusterTrackedResponseWriter) ReadFrom(reader io.Reader) (int64, error) {
	w.detectSSE()
	if readerFrom, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return readerFrom.ReadFrom(reader)
	}
	return io.Copy(struct{ io.Writer }{Writer: w.ResponseWriter}, reader)
}

func (w *clusterTrackedResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
