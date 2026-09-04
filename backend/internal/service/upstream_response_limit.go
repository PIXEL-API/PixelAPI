package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
)

var ErrUpstreamResponseBodyTooLarge = errors.New("upstream response body too large")

// defaultUpstreamResponseReadMaxBytes 源自 config.DefaultUpstreamResponseReadMaxBytes，
// 仅在 cfg 为 nil 时作为兜底（测试或极端场景）。
const defaultUpstreamResponseReadMaxBytes = config.DefaultUpstreamResponseReadMaxBytes

func resolveUpstreamResponseReadLimit(cfg *config.Config) int64 {
	if cfg != nil && cfg.Gateway.UpstreamResponseReadMaxBytes > 0 {
		return cfg.Gateway.UpstreamResponseReadMaxBytes
	}
	return defaultUpstreamResponseReadMaxBytes
}

func readUpstreamResponseBodyLimited(reader io.Reader, maxBytes int64) ([]byte, error) {
	if reader == nil {
		return nil, errors.New("response body is nil")
	}
	if maxBytes <= 0 {
		maxBytes = defaultUpstreamResponseReadMaxBytes
	}

	body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("%w: limit=%d", ErrUpstreamResponseBodyTooLarge, maxBytes)
	}
	return body, nil
}

// TooLargeWriter 在响应超限时向客户端写格式化的错误响应。
type TooLargeWriter func(c *gin.Context)

// ReadUpstreamResponseBody 读取上游非流式响应体。
// 超限时自动记录 ops error 并调用 onTooLarge 向客户端写错误。
func ReadUpstreamResponseBody(reader io.Reader, cfg *config.Config, c *gin.Context, onTooLarge TooLargeWriter) ([]byte, error) {
	maxBytes := resolveUpstreamResponseReadLimit(cfg)
	body, err := readUpstreamResponseBodyLimited(reader, maxBytes)
	if err != nil {
		if errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			setOpsUpstreamError(c, http.StatusBadGateway, "upstream response too large", "")
			if onTooLarge != nil {
				onTooLarge(c)
			}
		}
		return nil, err
	}
	return body, nil
}

// ReadUpstreamResponseBodyWithContext 在 ReadUpstreamResponseBody 的字节上限基础上，
// 允许调用方为客户端断开后的上游 body drain 设置独立超时。
func ReadUpstreamResponseBodyWithContext(ctx context.Context, reader io.Reader, cfg *config.Config, c *gin.Context, onTooLarge TooLargeWriter) ([]byte, error) {
	if ctx == nil || ctx.Done() == nil {
		return ReadUpstreamResponseBody(reader, cfg, c, onTooLarge)
	}

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			if closer, ok := reader.(io.Closer); ok {
				_ = closer.Close()
			}
		case <-done:
		}
	}()
	defer close(done)

	body, err := ReadUpstreamResponseBody(reader, cfg, c, onTooLarge)
	if err != nil && ctx.Err() != nil {
		setOpsUpstreamError(c, http.StatusBadGateway, "upstream response read timeout", "")
		return nil, ctx.Err()
	}
	return body, err
}

type upstreamResponseActivityReader struct {
	source       io.Reader
	started      time.Time
	lastActivity atomic.Int64
}

func (r *upstreamResponseActivityReader) Read(p []byte) (int, error) {
	n, err := r.source.Read(p)
	if n > 0 {
		r.lastActivity.Store(time.Since(r.started).Nanoseconds())
	}
	return n, err
}

// ReadUpstreamResponseBodyWithIdleTimeout bounds the gap between upstream
// reads, not the total response duration. ctx independently controls client
// disconnect draining and account-share lease cancellation.
func ReadUpstreamResponseBodyWithIdleTimeout(ctx context.Context, reader io.ReadCloser, cfg *config.Config, c *gin.Context, onTooLarge TooLargeWriter, idleTimeout time.Duration) ([]byte, error) {
	if reader == nil {
		return nil, errors.New("response body is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := context.Cause(ctx); err != nil {
		_ = reader.Close()
		return nil, err
	}
	if ctx.Done() == nil && idleTimeout <= 0 {
		return ReadUpstreamResponseBody(reader, cfg, c, onTooLarge)
	}

	activity := &upstreamResponseActivityReader{source: reader, started: time.Now()}
	done := make(chan struct{})
	watcherDone := make(chan struct{})
	failure := make(chan error, 1)
	const (
		readRunning uint32 = iota
		readCompleted
		readInterrupted
	)
	var readState atomic.Uint32
	go func() {
		defer close(watcherDone)
		var timer *time.Timer
		var idle <-chan time.Time
		if idleTimeout > 0 {
			timer = time.NewTimer(idleTimeout)
			idle = timer.C
			defer timer.Stop()
		}
		for {
			var cause error
			select {
			case <-done:
				return
			case <-ctx.Done():
				cause = context.Cause(ctx)
			case <-idle:
				remaining := idleTimeout - (time.Since(activity.started) - time.Duration(activity.lastActivity.Load()))
				if remaining > 0 {
					timer.Reset(remaining)
					continue
				}
				cause = fmt.Errorf("upstream response idle timeout after %s: %w", idleTimeout, context.DeadlineExceeded)
			}
			// Completed reads win over a cancellation/timer that becomes ready
			// before the watcher observes done. An interrupted EOF must still fail.
			if !readState.CompareAndSwap(readRunning, readInterrupted) {
				return
			}
			failure <- cause
			_ = reader.Close()
			return
		}
	}()

	body, err := func() ([]byte, error) {
		defer func() {
			readState.CompareAndSwap(readRunning, readCompleted)
			close(done)
		}()
		return ReadUpstreamResponseBody(activity, cfg, c, onTooLarge)
	}()
	<-watcherDone
	select {
	case cause := <-failure:
		setOpsUpstreamError(c, http.StatusBadGateway, cause.Error(), "")
		return nil, cause
	default:
		return body, err
	}
}

// anthropicTooLargeError 以 Anthropic Messages API 格式写入超限错误。
func anthropicTooLargeError(c *gin.Context) {
	c.JSON(http.StatusBadGateway, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    "upstream_error",
			"message": "Upstream response too large",
		},
	})
}

// openAITooLargeError 以 OpenAI / Gemini 格式写入超限错误。
func openAITooLargeError(c *gin.Context) {
	c.JSON(http.StatusBadGateway, gin.H{
		"error": gin.H{
			"type":    "upstream_error",
			"message": "Upstream response too large",
		},
	})
}
