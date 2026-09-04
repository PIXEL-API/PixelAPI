package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"golang.org/x/sync/semaphore"
)

var (
	ErrOpenAIResponsesBodyBudgetExhausted = errors.New("openai responses body memory budget exhausted")
	ErrOpenAIResponsesBodyBudgetInvalid   = errors.New("invalid openai responses body memory budget reservation")
)

// OpenAIResponsesBodyBudget is a process-local weighted admission gate. A
// request reserves its complete worst-case body size before the handler starts
// reading, so concurrent readers cannot each retain a partial allocation while
// waiting for more permits.
type OpenAIResponsesBodyBudget struct {
	capacity    int64
	waitTimeout time.Duration
	readTimeout time.Duration
	retryAfter  time.Duration
	semaphore   *semaphore.Weighted

	inUseBytes     atomic.Int64
	peakInUseBytes atomic.Int64
	waiters        atomic.Int64
	acquiredTotal  atomic.Uint64
	rejectedTotal  atomic.Uint64
	canceledTotal  atomic.Uint64
	waitNanosTotal atomic.Uint64
}

// OpenAIResponsesBodyBudgetSnapshot is a low-cardinality runtime snapshot for
// logs and operational metrics.
type OpenAIResponsesBodyBudgetSnapshot struct {
	CapacityBytes  int64
	InUseBytes     int64
	PeakInUseBytes int64
	Waiters        int64
	AcquiredTotal  uint64
	RejectedTotal  uint64
	CanceledTotal  uint64
	WaitNanosTotal uint64
}

// OpenAIResponsesBodyBudgetLease owns one reservation. Release is idempotent so
// every handler exit path, including panic recovery, can safely defer it.
type OpenAIResponsesBodyBudgetLease struct {
	budget   *OpenAIResponsesBodyBudget
	reserved int64
	once     sync.Once
}

func NewOpenAIResponsesBodyBudget(cfg config.GatewayOpenAIResponsesBodyBudgetConfig) (*OpenAIResponsesBodyBudget, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if cfg.CapacityBytes <= 0 || cfg.WaitTimeoutSeconds <= 0 || cfg.ReadTimeoutSeconds <= 0 || cfg.RetryAfterSeconds <= 0 {
		return nil, fmt.Errorf("%w: enabled budget requires positive capacity and timeout values", ErrOpenAIResponsesBodyBudgetInvalid)
	}
	return &OpenAIResponsesBodyBudget{
		capacity:    cfg.CapacityBytes,
		waitTimeout: time.Duration(cfg.WaitTimeoutSeconds) * time.Second,
		readTimeout: time.Duration(cfg.ReadTimeoutSeconds) * time.Second,
		retryAfter:  time.Duration(cfg.RetryAfterSeconds) * time.Second,
		semaphore:   semaphore.NewWeighted(cfg.CapacityBytes),
	}, nil
}

func (b *OpenAIResponsesBodyBudget) Acquire(ctx context.Context, reservationBytes int64) (*OpenAIResponsesBodyBudgetLease, error) {
	if b == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if reservationBytes <= 0 || reservationBytes > b.capacity {
		b.rejectedTotal.Add(1)
		return nil, fmt.Errorf("%w: reservation=%d capacity=%d", ErrOpenAIResponsesBodyBudgetInvalid, reservationBytes, b.capacity)
	}

	waitCtx, cancel := context.WithTimeout(ctx, b.waitTimeout)
	defer cancel()
	b.waiters.Add(1)
	startedAt := time.Now()
	err := b.semaphore.Acquire(waitCtx, reservationBytes)
	b.waiters.Add(-1)
	b.waitNanosTotal.Add(uint64(time.Since(startedAt)))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			b.canceledTotal.Add(1)
			return nil, ctxErr
		}
		b.rejectedTotal.Add(1)
		return nil, fmt.Errorf("%w: reservation=%d capacity=%d: %v", ErrOpenAIResponsesBodyBudgetExhausted, reservationBytes, b.capacity, err)
	}

	inUse := b.inUseBytes.Add(reservationBytes)
	updateOpenAIResponsesBodyBudgetPeak(&b.peakInUseBytes, inUse)
	b.acquiredTotal.Add(1)
	return &OpenAIResponsesBodyBudgetLease{budget: b, reserved: reservationBytes}, nil
}

func updateOpenAIResponsesBodyBudgetPeak(peak *atomic.Int64, value int64) {
	for {
		current := peak.Load()
		if value <= current || peak.CompareAndSwap(current, value) {
			return
		}
	}
}

func (l *OpenAIResponsesBodyBudgetLease) Release() {
	if l == nil || l.budget == nil || l.reserved <= 0 {
		return
	}
	l.once.Do(func() {
		l.budget.inUseBytes.Add(-l.reserved)
		l.budget.semaphore.Release(l.reserved)
	})
}

func (b *OpenAIResponsesBodyBudget) ReadTimeout() time.Duration {
	if b == nil {
		return 0
	}
	return b.readTimeout
}

func (b *OpenAIResponsesBodyBudget) RetryAfterSeconds() int {
	if b == nil || b.retryAfter <= 0 {
		return 0
	}
	seconds := int(b.retryAfter / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

func (b *OpenAIResponsesBodyBudget) Snapshot() OpenAIResponsesBodyBudgetSnapshot {
	if b == nil {
		return OpenAIResponsesBodyBudgetSnapshot{}
	}
	return OpenAIResponsesBodyBudgetSnapshot{
		CapacityBytes:  b.capacity,
		InUseBytes:     b.inUseBytes.Load(),
		PeakInUseBytes: b.peakInUseBytes.Load(),
		Waiters:        b.waiters.Load(),
		AcquiredTotal:  b.acquiredTotal.Load(),
		RejectedTotal:  b.rejectedTotal.Load(),
		CanceledTotal:  b.canceledTotal.Load(),
		WaitNanosTotal: b.waitNanosTotal.Load(),
	}
}
