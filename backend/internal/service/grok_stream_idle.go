package service

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// Grok 长思考流允许较长静默，但不能无限占用账号并最终把并发池耗尽。
const defaultGrokStreamIdleTimeout = 180 * time.Second

const grokStreamIdleCooldown = 2 * time.Minute

var errGrokStreamIdleTimeout = errors.New("grok upstream stream idle timeout")

type grokStreamIdleReadResult struct {
	data []byte
	err  error
}

type grokStreamIdleReadCloser struct {
	source     io.ReadCloser
	idle       time.Duration
	onIdle     func()
	results    chan grokStreamIdleReadResult
	done       chan struct{}
	pending    []byte
	pendingErr error
	start      sync.Once
	idleOnce   sync.Once
	close      sync.Once
}

func newGrokStreamIdleReadCloser(source io.ReadCloser, idle time.Duration, onIdle func()) io.ReadCloser {
	if source == nil || idle <= 0 {
		return source
	}
	return &grokStreamIdleReadCloser{
		source:  source,
		idle:    idle,
		onIdle:  onIdle,
		results: make(chan grokStreamIdleReadResult, 1),
		done:    make(chan struct{}),
	}
}

func (r *grokStreamIdleReadCloser) Read(buffer []byte) (int, error) {
	if r == nil || r.source == nil {
		return 0, io.ErrClosedPipe
	}
	if len(buffer) == 0 {
		return 0, nil
	}
	if len(r.pending) > 0 {
		n := copy(buffer, r.pending)
		r.pending = r.pending[n:]
		if len(r.pending) == 0 && r.pendingErr != nil {
			err := r.pendingErr
			r.pendingErr = nil
			return n, err
		}
		return n, nil
	}
	if r.pendingErr != nil {
		err := r.pendingErr
		r.pendingErr = nil
		return 0, err
	}

	r.start.Do(func() { go r.readLoop() })
	timer := time.NewTimer(r.idle)
	defer timer.Stop()
	select {
	case result, ok := <-r.results:
		if !ok {
			return 0, io.EOF
		}
		n := copy(buffer, result.data)
		if n < len(result.data) {
			r.pending = result.data[n:]
			r.pendingErr = result.err
			return n, nil
		}
		return n, result.err
	case <-timer.C:
		r.idleOnce.Do(func() {
			if r.onIdle != nil {
				r.onIdle()
			}
		})
		_ = r.closeSource()
		return 0, errGrokStreamIdleTimeout
	}
}

// readLoop is the wrapper's only source reader. A bounded one-result channel
// keeps backpressure while avoiding one goroutine and one scratch allocation
// for every downstream Read call.
func (r *grokStreamIdleReadCloser) readLoop() {
	defer close(r.results)
	buffer := make([]byte, 32*1024)
	for {
		n, err := r.source.Read(buffer)
		result := grokStreamIdleReadResult{err: err}
		if n > 0 {
			result.data = append([]byte(nil), buffer[:n]...)
		}
		if n > 0 || err != nil {
			select {
			case r.results <- result:
			case <-r.done:
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (r *grokStreamIdleReadCloser) Close() error {
	if r == nil {
		return nil
	}
	return r.closeSource()
}

func (r *grokStreamIdleReadCloser) closeSource() error {
	var closeErr error
	r.close.Do(func() {
		close(r.done)
		if r.source != nil {
			closeErr = r.source.Close()
		}
	})
	return closeErr
}

func resolveGrokStreamIdleTimeout(configuredSeconds int) time.Duration {
	if configuredSeconds > 0 {
		return time.Duration(configuredSeconds) * time.Second
	}
	return defaultGrokStreamIdleTimeout
}

func grokStreamIdleFailoverError(account *Account, idle time.Duration) *UpstreamFailoverError {
	message := fmt.Sprintf("Grok stream idle timeout after %s with no upstream data", idle.Round(time.Second))
	return &UpstreamFailoverError{
		StatusCode:               502,
		ResponseBody:             []byte(`{"error":{"code":"empty_upstream","message":"` + strings.ReplaceAll(message, `"`, `'`) + `"}}`),
		SafeToFailoverAfterWrite: true,
		RetryableOnSameAccount:   account != nil && account.IsPoolMode(),
	}
}
