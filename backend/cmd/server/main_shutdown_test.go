package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type shutdownServerStub struct {
	shutdownFn    func(context.Context) error
	closeFn       func() error
	shutdownCalls atomic.Int32
	closeCalls    atomic.Int32
}

func (s *shutdownServerStub) Shutdown(ctx context.Context) error {
	s.shutdownCalls.Add(1)
	if s.shutdownFn == nil {
		return nil
	}
	return s.shutdownFn(ctx)
}

func (s *shutdownServerStub) Close() error {
	s.closeCalls.Add(1)
	if s.closeFn == nil {
		return nil
	}
	return s.closeFn()
}

func TestShutdownHTTPServersSuccessfulShutdownDoesNotClose(t *testing.T) {
	server := &shutdownServerStub{}

	err := shutdownHTTPServers(shutdownTarget{
		name:    "api",
		server:  server,
		timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("shutdownHTTPServers() error = %v", err)
	}
	if got := server.shutdownCalls.Load(); got != 1 {
		t.Fatalf("Shutdown() calls = %d, want 1", got)
	}
	if got := server.closeCalls.Load(); got != 0 {
		t.Fatalf("Close() calls = %d, want 0", got)
	}
}

func TestShutdownHTTPServersFailureForcesClose(t *testing.T) {
	wantErr := errors.New("graceful shutdown failed")
	server := &shutdownServerStub{
		shutdownFn: func(context.Context) error { return wantErr },
	}

	err := shutdownHTTPServers(shutdownTarget{
		name:    "api",
		server:  server,
		timeout: time.Second,
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("shutdownHTTPServers() error = %v, want wrapping %v", err, wantErr)
	}
	if got := server.closeCalls.Load(); got != 1 {
		t.Fatalf("Close() calls = %d, want 1", got)
	}
}

func TestShutdownHTTPServersDeadlineForcesClose(t *testing.T) {
	server := &shutdownServerStub{
		shutdownFn: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}

	err := shutdownHTTPServers(shutdownTarget{
		name:    "api",
		server:  server,
		timeout: 20 * time.Millisecond,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdownHTTPServers() error = %v, want wrapping %v", err, context.DeadlineExceeded)
	}
	if got := server.closeCalls.Load(); got != 1 {
		t.Fatalf("Close() calls = %d, want 1", got)
	}
}

func TestShutdownHTTPServersPreservesShutdownAndCloseErrors(t *testing.T) {
	shutdownErr := errors.New("shutdown failed")
	closeErr := errors.New("forced close failed")
	server := &shutdownServerStub{
		shutdownFn: func(context.Context) error { return shutdownErr },
		closeFn:    func() error { return closeErr },
	}

	err := shutdownHTTPServers(shutdownTarget{
		name:    "api",
		server:  server,
		timeout: time.Second,
	})
	if !errors.Is(err, shutdownErr) {
		t.Errorf("shutdownHTTPServers() error = %v, want wrapping shutdown error %v", err, shutdownErr)
	}
	if !errors.Is(err, closeErr) {
		t.Errorf("shutdownHTTPServers() error = %v, want wrapping close error %v", err, closeErr)
	}
}

func TestShutdownHTTPServersContinuesAfterTargetFailure(t *testing.T) {
	firstErr := errors.New("first server failed")
	first := &shutdownServerStub{
		shutdownFn: func(context.Context) error { return firstErr },
	}
	second := &shutdownServerStub{}

	err := shutdownHTTPServers(
		shutdownTarget{name: "api", server: first, timeout: time.Second},
		shutdownTarget{name: "pprof", server: second, timeout: time.Second},
	)
	if !errors.Is(err, firstErr) {
		t.Fatalf("shutdownHTTPServers() error = %v, want wrapping %v", err, firstErr)
	}
	if got := first.closeCalls.Load(); got != 1 {
		t.Fatalf("first Close() calls = %d, want 1", got)
	}
	if got := second.shutdownCalls.Load(); got != 1 {
		t.Fatalf("second Shutdown() calls = %d, want 1", got)
	}
	if got := second.closeCalls.Load(); got != 0 {
		t.Fatalf("second Close() calls = %d, want 0", got)
	}
}

func TestShutdownHTTPServersUsesIndependentContexts(t *testing.T) {
	first := &shutdownServerStub{
		shutdownFn: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	secondContextWasActive := atomic.Bool{}
	second := &shutdownServerStub{
		shutdownFn: func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return errors.New("second server received an expired context")
			default:
				secondContextWasActive.Store(true)
				return nil
			}
		},
	}

	err := shutdownHTTPServers(
		shutdownTarget{name: "api", server: first, timeout: 20 * time.Millisecond},
		shutdownTarget{name: "pprof", server: second, timeout: time.Second},
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdownHTTPServers() error = %v, want wrapping %v", err, context.DeadlineExceeded)
	}
	if !secondContextWasActive.Load() {
		t.Fatal("second server did not receive a fresh active context")
	}
	if got := second.closeCalls.Load(); got != 0 {
		t.Fatalf("second Close() calls = %d, want 0", got)
	}
}

func TestShutdownHTTPServersRejectsInvalidTimeoutWithoutCallingServer(t *testing.T) {
	server := &shutdownServerStub{}

	err := shutdownHTTPServers(shutdownTarget{
		name:    "api",
		server:  server,
		timeout: 0,
	})
	if err == nil {
		t.Fatal("shutdownHTTPServers() error = nil, want invalid timeout error")
	}
	if got := server.shutdownCalls.Load(); got != 0 {
		t.Fatalf("Shutdown() calls = %d, want 0", got)
	}
	if got := server.closeCalls.Load(); got != 0 {
		t.Fatalf("Close() calls = %d, want 0", got)
	}
}
