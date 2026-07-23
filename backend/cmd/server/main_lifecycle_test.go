package main

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunServerLifecycleStopRunsShutdownAndCleanup(t *testing.T) {
	stop := make(chan struct{})
	close(stop)
	restart := make(chan struct{})
	serveResults := make(chan serverServeResult)

	var lifecycleStage atomic.Int32
	server := &shutdownServerStub{
		shutdownFn: func(context.Context) error {
			if !lifecycleStage.CompareAndSwap(0, 1) {
				lifecycleStage.Store(-1)
			}
			return nil
		},
	}
	var cleanupCalls atomic.Int32
	cleanup := func(ctx context.Context) error {
		cleanupCalls.Add(1)
		if ctx == nil {
			lifecycleStage.Store(-1)
			return nil
		}
		if !lifecycleStage.CompareAndSwap(1, 2) {
			lifecycleStage.Store(-1)
		}
		return nil
	}

	err := runServerLifecycle(
		stop,
		restart,
		serveResults,
		[]shutdownTarget{{name: "main", server: server, timeout: time.Second}},
		cleanup,
		time.Second,
	)
	if err != nil {
		t.Fatalf("runServerLifecycle() error = %v", err)
	}
	if got := server.shutdownCalls.Load(); got != 1 {
		t.Fatalf("Shutdown() calls = %d, want 1", got)
	}
	if got := server.closeCalls.Load(); got != 0 {
		t.Fatalf("Close() calls = %d, want 0", got)
	}
	if got := cleanupCalls.Load(); got != 1 {
		t.Fatalf("cleanup calls = %d, want 1", got)
	}
	if got := lifecycleStage.Load(); got != 2 {
		t.Fatalf("lifecycle stage = %d, want 2 (shutdown before cleanup)", got)
	}
}

func TestRunServerLifecycleServeFailureShutsDownAndReturnsCause(t *testing.T) {
	serveErr := errors.New("serve failed")
	stop := make(chan struct{})
	restart := make(chan struct{})
	serveResults := make(chan serverServeResult, 1)
	serveResults <- serverServeResult{name: "main", err: serveErr}

	server := &shutdownServerStub{}
	var cleanupCalls atomic.Int32
	err := runServerLifecycle(
		stop,
		restart,
		serveResults,
		[]shutdownTarget{{name: "main", server: server, timeout: time.Second}},
		func(context.Context) error {
			cleanupCalls.Add(1)
			return nil
		},
		time.Second,
	)

	if !errors.Is(err, serveErr) {
		t.Fatalf("runServerLifecycle() error = %v, want wrapping %v", err, serveErr)
	}
	if got := server.shutdownCalls.Load(); got != 1 {
		t.Fatalf("Shutdown() calls = %d, want 1", got)
	}
	if got := cleanupCalls.Load(); got != 1 {
		t.Fatalf("cleanup calls = %d, want 1", got)
	}
}

func TestRunServerLifecycleStopIgnoresShutdownFailureButStillCleansUp(t *testing.T) {
	stop := make(chan struct{})
	close(stop)
	restart := make(chan struct{})
	serveResults := make(chan serverServeResult)
	shutdownErr := errors.New("graceful shutdown failed")

	server := &shutdownServerStub{
		shutdownFn: func(context.Context) error { return shutdownErr },
	}
	var cleanupCalls atomic.Int32
	err := runServerLifecycle(
		stop,
		restart,
		serveResults,
		[]shutdownTarget{{name: "main", server: server, timeout: time.Second}},
		func(context.Context) error {
			cleanupCalls.Add(1)
			return nil
		},
		time.Second,
	)

	if err != nil {
		t.Fatalf("runServerLifecycle() error = %v, want nil for requested stop", err)
	}
	if got := server.shutdownCalls.Load(); got != 1 {
		t.Fatalf("Shutdown() calls = %d, want 1", got)
	}
	if got := server.closeCalls.Load(); got != 1 {
		t.Fatalf("Close() calls = %d, want 1 after shutdown failure", got)
	}
	if got := cleanupCalls.Load(); got != 1 {
		t.Fatalf("cleanup calls = %d, want 1", got)
	}
}

func TestRunServerLifecycleStopIgnoresCleanupFailure(t *testing.T) {
	stop := make(chan struct{})
	close(stop)
	restart := make(chan struct{})
	serveResults := make(chan serverServeResult)
	cleanupErr := errors.New("cleanup failed")

	server := &shutdownServerStub{}
	var cleanupCalls atomic.Int32
	err := runServerLifecycle(
		stop,
		restart,
		serveResults,
		[]shutdownTarget{{name: "main", server: server, timeout: time.Second}},
		func(context.Context) error {
			cleanupCalls.Add(1)
			return cleanupErr
		},
		time.Second,
	)

	if err != nil {
		t.Fatalf("runServerLifecycle() error = %v, want nil for requested stop", err)
	}
	if got := server.shutdownCalls.Load(); got != 1 {
		t.Fatalf("Shutdown() calls = %d, want 1", got)
	}
	if got := cleanupCalls.Load(); got != 1 {
		t.Fatalf("cleanup calls = %d, want 1", got)
	}
}

func TestRunServerLifecycleRestartRunsShutdownAndCleanup(t *testing.T) {
	stop := make(chan struct{})
	restart := make(chan struct{})
	close(restart)
	serveResults := make(chan serverServeResult)

	server := &shutdownServerStub{}
	var cleanupCalls atomic.Int32
	err := runServerLifecycle(
		stop,
		restart,
		serveResults,
		[]shutdownTarget{{name: "main", server: server, timeout: time.Second}},
		func(context.Context) error {
			cleanupCalls.Add(1)
			return nil
		},
		time.Second,
	)

	if !errors.Is(err, errServerRestartRequested) {
		t.Fatalf("runServerLifecycle() error = %v, want wrapping %v", err, errServerRestartRequested)
	}
	if got := server.shutdownCalls.Load(); got != 1 {
		t.Fatalf("Shutdown() calls = %d, want 1", got)
	}
	if got := cleanupCalls.Load(); got != 1 {
		t.Fatalf("cleanup calls = %d, want 1", got)
	}
}

func TestRunServerLifecycleStopTakesPriorityOverServeFailure(t *testing.T) {
	stop := make(chan struct{})
	close(stop)
	restart := make(chan struct{})
	serveResults := make(chan serverServeResult, 1)
	serveResults <- serverServeResult{name: "main", err: errors.New("serve failed")}

	server := &shutdownServerStub{}
	err := runServerLifecycle(
		stop,
		restart,
		serveResults,
		[]shutdownTarget{{name: "main", server: server, timeout: time.Second}},
		func(context.Context) error { return nil },
		time.Second,
	)

	if err != nil {
		t.Fatalf("runServerLifecycle() error = %v, want nil when stop and serve result are both ready", err)
	}
	if got := server.shutdownCalls.Load(); got != 1 {
		t.Fatalf("Shutdown() calls = %d, want 1", got)
	}
}

func TestRunServerLifecycleStopTakesPriorityOverRestart(t *testing.T) {
	stop := make(chan struct{})
	close(stop)
	restart := make(chan struct{})
	close(restart)
	serveResults := make(chan serverServeResult)

	server := &shutdownServerStub{}
	err := runServerLifecycle(
		stop,
		restart,
		serveResults,
		[]shutdownTarget{{name: "main", server: server, timeout: time.Second}},
		func(context.Context) error { return nil },
		time.Second,
	)

	if err != nil {
		t.Fatalf("runServerLifecycle() error = %v, want nil when stop and restart are both ready", err)
	}
	if got := server.shutdownCalls.Load(); got != 1 {
		t.Fatalf("Shutdown() calls = %d, want 1", got)
	}
}

func TestRunServerLifecycleRestartTakesPriorityOverServeFailure(t *testing.T) {
	stop := make(chan struct{})
	restart := make(chan struct{})
	close(restart)
	serveResults := make(chan serverServeResult, 1)
	serveResults <- serverServeResult{name: "main", err: errors.New("serve failed")}

	server := &shutdownServerStub{}
	var cleanupCalls atomic.Int32
	err := runServerLifecycle(
		stop,
		restart,
		serveResults,
		[]shutdownTarget{{name: "main", server: server, timeout: time.Second}},
		func(context.Context) error {
			cleanupCalls.Add(1)
			return nil
		},
		time.Second,
	)

	if !errors.Is(err, errServerRestartRequested) {
		t.Fatalf("runServerLifecycle() error = %v, want wrapping %v", err, errServerRestartRequested)
	}
	if got := server.shutdownCalls.Load(); got != 1 {
		t.Fatalf("Shutdown() calls = %d, want 1", got)
	}
	if got := cleanupCalls.Load(); got != 1 {
		t.Fatalf("cleanup calls = %d, want 1", got)
	}
}

func TestRunServerLifecycleStopTakesPriorityWhenAllTriggersReady(t *testing.T) {
	stop := make(chan struct{})
	close(stop)
	restart := make(chan struct{})
	close(restart)
	serveResults := make(chan serverServeResult, 1)
	serveResults <- serverServeResult{name: "main", err: errors.New("serve failed")}

	server := &shutdownServerStub{}
	var cleanupCalls atomic.Int32
	err := runServerLifecycle(
		stop,
		restart,
		serveResults,
		[]shutdownTarget{{name: "main", server: server, timeout: time.Second}},
		func(context.Context) error {
			cleanupCalls.Add(1)
			return nil
		},
		time.Second,
	)

	if err != nil {
		t.Fatalf("runServerLifecycle() error = %v, want nil when all triggers are ready", err)
	}
	if got := server.shutdownCalls.Load(); got != 1 {
		t.Fatalf("Shutdown() calls = %d, want 1", got)
	}
	if got := cleanupCalls.Load(); got != 1 {
		t.Fatalf("cleanup calls = %d, want 1", got)
	}
}

func TestStartPprofServerRejectsInvalidEnabledValue(t *testing.T) {
	t.Setenv("PPROF_ENABLED", "not-a-boolean")
	serveResults := make(chan serverServeResult, 1)

	server, err := startPprofServer(serveResults)
	if err == nil {
		t.Fatal("startPprofServer() error = nil, want invalid PPROF_ENABLED error")
	}
	if server != nil {
		t.Fatal("startPprofServer() server is non-nil after invalid PPROF_ENABLED")
	}
	select {
	case result := <-serveResults:
		t.Fatalf("startPprofServer() unexpectedly started a server: %+v", result)
	default:
	}
}

func TestStartPprofServerReportsGracefulShutdown(t *testing.T) {
	t.Setenv("PPROF_ENABLED", "true")
	t.Setenv("PPROF_ADDR", "127.0.0.1:0")
	serveResults := make(chan serverServeResult, 1)

	server, err := startPprofServer(serveResults)
	if err != nil {
		t.Fatalf("startPprofServer() error = %v", err)
	}
	if server == nil {
		t.Fatal("startPprofServer() server = nil, want running server")
	}
	defer func() {
		_ = server.Close()
	}()

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("pprof server Shutdown() error = %v", err)
	}

	select {
	case result := <-serveResults:
		if result.name != "pprof server" {
			t.Errorf("serve result name = %q, want %q", result.name, "pprof server")
		}
		if !errors.Is(result.err, http.ErrServerClosed) {
			t.Errorf("serve result error = %v, want wrapping %v", result.err, http.ErrServerClosed)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pprof serve result after shutdown")
	}
}
