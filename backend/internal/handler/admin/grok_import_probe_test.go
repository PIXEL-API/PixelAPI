//go:build unit

package admin

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type grokImportProbeTestProber struct {
	mu      sync.Mutex
	calls   map[int64]int
	started chan int64
	block   <-chan struct{}
}

func (p *grokImportProbeTestProber) ProbeUsage(ctx context.Context, accountID int64) (*service.GrokQuotaProbeResult, error) {
	p.mu.Lock()
	if p.calls == nil {
		p.calls = make(map[int64]int)
	}
	p.calls[accountID]++
	p.mu.Unlock()
	if p.started != nil {
		p.started <- accountID
	}
	if p.block != nil {
		select {
		case <-p.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &service.GrokQuotaProbeResult{StatusCode: 200}, nil
}

func TestGrokImportProbeSchedulerDeduplicatesPendingAndInFlight(t *testing.T) {
	release := make(chan struct{})
	prober := &grokImportProbeTestProber{started: make(chan int64, 2), block: release}
	scheduler := newGrokImportProbeScheduler(1, time.Second)
	first := &service.Account{ID: 501, Platform: service.PlatformGrok, Type: service.AccountTypeOAuth}
	second := &service.Account{ID: 502, Platform: service.PlatformGrok, Type: service.AccountTypeOAuth}

	scheduler.schedule(prober, first)
	require.Equal(t, int64(501), awaitGrokImportProbeStart(t, prober.started))
	scheduler.schedule(prober, first)
	scheduler.schedule(prober, second)
	scheduler.schedule(prober, second)

	scheduler.mu.Lock()
	require.Len(t, scheduler.queue, 1)
	require.Contains(t, scheduler.inFlight, int64(501))
	require.Contains(t, scheduler.pending, int64(502))
	scheduler.mu.Unlock()

	close(release)
	require.Equal(t, int64(502), awaitGrokImportProbeStart(t, prober.started))
	require.Eventually(t, func() bool {
		scheduler.mu.Lock()
		defer scheduler.mu.Unlock()
		return scheduler.workers == 0 && len(scheduler.pending) == 0 && len(scheduler.inFlight) == 0
	}, time.Second, 10*time.Millisecond)

	prober.mu.Lock()
	require.Equal(t, 1, prober.calls[501])
	require.Equal(t, 1, prober.calls[502])
	prober.mu.Unlock()
}

func TestGrokImportProbeSchedulerBoundsPendingQueue(t *testing.T) {
	release := make(chan struct{})
	prober := &grokImportProbeTestProber{started: make(chan int64, grokImportProbeQueueLimit+1), block: release}
	scheduler := newGrokImportProbeScheduler(1, time.Second)
	scheduler.schedule(prober, &service.Account{ID: 600, Platform: service.PlatformGrok, Type: service.AccountTypeOAuth})
	require.Equal(t, int64(600), awaitGrokImportProbeStart(t, prober.started))
	for id := int64(601); id < 601+grokImportProbeQueueLimit+10; id++ {
		scheduler.schedule(prober, &service.Account{ID: id, Platform: service.PlatformGrok, Type: service.AccountTypeOAuth})
	}

	scheduler.mu.Lock()
	require.Len(t, scheduler.queue, grokImportProbeQueueLimit)
	require.Equal(t, 1, scheduler.maxWorkers)
	scheduler.mu.Unlock()
	close(release)
}

func awaitGrokImportProbeStart(t *testing.T, started <-chan int64) int64 {
	t.Helper()
	select {
	case accountID := <-started:
		return accountID
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Grok import probe")
		return 0
	}
}
