package service

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const openAICompactSSEKeepaliveKey = "openai_compact_sse_keepalive"

type openAICompactSSEKeepalive struct {
	mu      sync.Mutex
	writer  gin.ResponseWriter
	started bool
	stopped bool
	bytes   int
	stop    chan struct{}
}

// StartOpenAICompactSSEKeepalive prevents proxy idle timeouts while unary compact is running.
func StartOpenAICompactSSEKeepalive(c *gin.Context, interval time.Duration) func() {
	if c == nil || c.Writer == nil || interval <= 0 || !openAICompactClientWantsStream(c) {
		return func() {}
	}
	originalWriter := c.Writer
	k := &openAICompactSSEKeepalive{writer: originalWriter, stop: make(chan struct{})}
	c.Set(openAICompactSSEKeepaliveKey, k)
	wrappedWriter := &openAICompactKeepaliveWriter{ResponseWriter: originalWriter, k: k}
	c.Writer = wrappedWriter

	var reqDone <-chan struct{}
	if c.Request != nil {
		reqDone = c.Request.Context().Done()
	}
	go func() {
		timer := time.NewTimer(interval)
		defer timer.Stop()
		for {
			select {
			case <-k.stop:
				return
			case <-reqDone:
				return
			case <-timer.C:
			}
			if !k.beat() {
				return
			}
			timer.Reset(interval)
		}
	}()
	return func() {
		k.Stop()
		// Avoid retaining a pooled middleware writer through the compact wrapper
		// after the request has finished.
		if current, ok := c.Writer.(*openAICompactKeepaliveWriter); ok && current == wrappedWriter {
			c.Writer = originalWriter
		}
	}
}

func (k *openAICompactSSEKeepalive) beat() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.stopped {
		return false
	}
	if !k.started {
		header := k.writer.Header()
		header.Set("Content-Type", "text/event-stream")
		header.Set("Cache-Control", "no-cache")
		header.Set("Connection", "keep-alive")
		header.Set("X-Accel-Buffering", "no")
		k.writer.WriteHeader(http.StatusOK)
		k.started = true
	}
	n, err := k.writer.Write([]byte(": keepalive\n\n"))
	k.bytes += n
	if err != nil {
		k.stopped = true
		return false
	}
	k.writer.Flush()
	return true
}

func (k *openAICompactSSEKeepalive) Stop() {
	k.mu.Lock()
	k.markStoppedLocked()
	k.mu.Unlock()
}

func (k *openAICompactSSEKeepalive) markStoppedLocked() {
	if k.stopped {
		return
	}
	k.stopped = true
	close(k.stop)
}

func StopOpenAICompactSSEKeepaliveCommitted(c *gin.Context) bool {
	if c == nil {
		return false
	}
	value, ok := c.Get(openAICompactSSEKeepaliveKey)
	if !ok {
		return false
	}
	k, ok := value.(*openAICompactSSEKeepalive)
	if !ok || k == nil {
		return false
	}
	k.mu.Lock()
	k.markStoppedLocked()
	committed := k.started
	k.mu.Unlock()
	return committed
}

// OpenAICompactKeepaliveAdjustedWrittenSize excludes non-semantic heartbeat bytes.
func OpenAICompactKeepaliveAdjustedWrittenSize(c *gin.Context) int {
	if c == nil || c.Writer == nil {
		return -1
	}
	value, ok := c.Get(openAICompactSSEKeepaliveKey)
	if !ok {
		return c.Writer.Size()
	}
	k, ok := value.(*openAICompactSSEKeepalive)
	if !ok || k == nil {
		return c.Writer.Size()
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	size := k.writer.Size()
	if size < 0 {
		return size
	}
	if real := size - k.bytes; real > 0 {
		return real
	}
	return -1
}

type openAICompactKeepaliveWriter struct {
	gin.ResponseWriter
	k *openAICompactSSEKeepalive
}

var errOpenAICompactResponseWriterReleased = errors.New("openai compact response writer released")

func (w *openAICompactKeepaliveWriter) suspend() {
	if w.k != nil {
		w.k.Stop()
	}
}

func (w *openAICompactKeepaliveWriter) Header() http.Header {
	w.suspend()
	if w.ResponseWriter == nil {
		return http.Header{}
	}
	return w.ResponseWriter.Header()
}
func (w *openAICompactKeepaliveWriter) Write(data []byte) (int, error) {
	w.suspend()
	if w.ResponseWriter == nil {
		return 0, errOpenAICompactResponseWriterReleased
	}
	return w.ResponseWriter.Write(data)
}
func (w *openAICompactKeepaliveWriter) WriteString(s string) (int, error) {
	w.suspend()
	if w.ResponseWriter == nil {
		return 0, errOpenAICompactResponseWriterReleased
	}
	return w.ResponseWriter.WriteString(s)
}
func (w *openAICompactKeepaliveWriter) WriteHeader(code int) {
	w.suspend()
	if w.ResponseWriter == nil {
		return
	}
	w.ResponseWriter.WriteHeader(code)
}
func (w *openAICompactKeepaliveWriter) WriteHeaderNow() {
	w.suspend()
	if w.ResponseWriter == nil {
		return
	}
	w.ResponseWriter.WriteHeaderNow()
}
func (w *openAICompactKeepaliveWriter) Flush() {
	w.suspend()
	if w.ResponseWriter == nil {
		return
	}
	w.ResponseWriter.Flush()
}

func (w *openAICompactKeepaliveWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if w.ResponseWriter == nil {
		return nil, nil, errOpenAICompactResponseWriterReleased
	}
	return w.ResponseWriter.Hijack()
}

func (w *openAICompactKeepaliveWriter) CloseNotify() <-chan bool {
	if w.ResponseWriter == nil {
		closed := make(chan bool)
		close(closed)
		return closed
	}
	return w.ResponseWriter.CloseNotify()
}

func (w *openAICompactKeepaliveWriter) Pusher() http.Pusher {
	if w.ResponseWriter == nil {
		return nil
	}
	return w.ResponseWriter.Pusher()
}

func (w *openAICompactKeepaliveWriter) Status() int {
	if w.k == nil || w.ResponseWriter == nil {
		return 0
	}
	w.k.mu.Lock()
	defer w.k.mu.Unlock()
	return w.ResponseWriter.Status()
}
func (w *openAICompactKeepaliveWriter) Size() int {
	if w.k == nil || w.ResponseWriter == nil {
		return 0
	}
	w.k.mu.Lock()
	defer w.k.mu.Unlock()
	return w.ResponseWriter.Size()
}
func (w *openAICompactKeepaliveWriter) Written() bool {
	if w.k == nil || w.ResponseWriter == nil {
		return false
	}
	w.k.mu.Lock()
	defer w.k.mu.Unlock()
	return w.ResponseWriter.Written()
}
