package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProcessAccountCredentialImportBoundsConcurrencyAndPreservesOrder(t *testing.T) {
	sources := make([]AccountCredentialImportSource, 12)
	for i := range sources {
		sources[i] = AccountCredentialImportSource{Kind: AccountCredentialImportKindOpenAIRefreshToken, Name: fmt.Sprintf("account-%02d", i+1)}
	}
	parseErrors := []AccountCredentialImportError{{Index: 1, Message: "invalid file"}}
	var active, maxActive atomic.Int32
	var mu sync.Mutex
	started := make([]int, 0, len(sources))
	result := ProcessAccountCredentialImport(context.Background(), sources, parseErrors,
		func(ctx context.Context, source AccountCredentialImportSource, sequence int) (bool, bool, error) {
			current := active.Add(1)
			for {
				observed := maxActive.Load()
				if current <= observed || maxActive.CompareAndSwap(observed, current) {
					break
				}
			}
			mu.Lock()
			started = append(started, sequence)
			mu.Unlock()
			time.Sleep(2 * time.Millisecond)
			active.Add(-1)
			if sequence == 3 {
				return false, false, fmt.Errorf("failed-%d", sequence)
			}
			if sequence == 4 {
				return false, true, nil
			}
			return true, false, nil
		})

	if got := maxActive.Load(); got < 2 || got > int32(accountCredentialImportParallel) {
		t.Fatalf("maximum concurrency = %d, want a value in [2, %d]", got, accountCredentialImportParallel)
	}
	if result.Total != 13 || result.Created != 10 || result.Updated != 1 || result.Failed != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(result.Errors) != 2 || result.Errors[0].Index != 1 || result.Errors[1].Index != 4 || result.Errors[1].Message != "failed-3" {
		t.Fatalf("errors are not stable: %+v", result.Errors)
	}
	if len(started) != len(sources) {
		t.Fatalf("started %d processors, want %d", len(started), len(sources))
	}
}

func TestProcessAccountCredentialImportStopsSchedulingAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sources := make([]AccountCredentialImportSource, 20)
	var started atomic.Int32
	result := ProcessAccountCredentialImport(ctx, sources, nil,
		func(ctx context.Context, source AccountCredentialImportSource, sequence int) (bool, bool, error) {
			started.Add(1)
			if sequence == 1 {
				cancel()
			}
			// Keep workers occupied long enough for the scheduler to observe
			// cancellation before it can enqueue the remaining sources.
			time.Sleep(5 * time.Millisecond)
			return true, false, nil
		})
	if started.Load() >= int32(len(sources)) {
		t.Fatalf("cancellation did not stop scheduling; started %d", started.Load())
	}
	if result.Failed+result.Created != result.Total {
		t.Fatalf("result counters do not cover all items: %+v", result)
	}
}
