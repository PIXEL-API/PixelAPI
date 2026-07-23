package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunProcessCleanupWaitsForParallelStepsBeforeSequentialInfra(t *testing.T) {
	applicationStarted := make(chan struct{}, 2)
	releaseApplication := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(releaseApplication) })

	var applicationCompleted atomic.Int32
	parallelSteps := []cleanupStep{
		{
			name: "application-a",
			fn: func() error {
				applicationStarted <- struct{}{}
				<-releaseApplication
				applicationCompleted.Add(1)
				return nil
			},
		},
		{
			name: "application-b",
			fn: func() error {
				applicationStarted <- struct{}{}
				<-releaseApplication
				applicationCompleted.Add(1)
				return nil
			},
		},
	}

	infraStartedTooEarly := errors.New("infrastructure cleanup started before application cleanup completed")
	infraOutOfOrder := errors.New("infrastructure cleanup ran out of order")
	var firstInfraCompleted atomic.Bool
	var secondInfraCompleted atomic.Bool
	infraSteps := []cleanupStep{
		{
			name: "redis",
			fn: func() error {
				if applicationCompleted.Load() != int32(len(parallelSteps)) {
					return infraStartedTooEarly
				}
				firstInfraCompleted.Store(true)
				return nil
			},
		},
		{
			name: "ent",
			fn: func() error {
				if applicationCompleted.Load() != int32(len(parallelSteps)) {
					return infraStartedTooEarly
				}
				if !firstInfraCompleted.Load() {
					return infraOutOfOrder
				}
				secondInfraCompleted.Store(true)
				return nil
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- runProcessCleanup(ctx, parallelSteps, infraSteps)
	}()

	waitForCleanupSignal(t, applicationStarted, "first application cleanup step to start")
	waitForCleanupSignal(t, applicationStarted, "second application cleanup step to start")
	releaseOnce.Do(func() { close(releaseApplication) })

	if err := waitForCleanupResult(t, result); err != nil {
		t.Fatalf("runProcessCleanup() error = %v", err)
	}
	if got := applicationCompleted.Load(); got != int32(len(parallelSteps)) {
		t.Fatalf("completed application steps = %d, want %d", got, len(parallelSteps))
	}
	if !firstInfraCompleted.Load() {
		t.Fatal("first infrastructure cleanup step did not complete")
	}
	if !secondInfraCompleted.Load() {
		t.Fatal("second infrastructure cleanup step did not complete")
	}
}

func TestRunProcessCleanupApplicationTimeoutSkipsInfra(t *testing.T) {
	applicationStarted := make(chan struct{})
	releaseApplication := make(chan struct{})
	applicationExited := make(chan struct{})
	infraStarted := make(chan struct{}, 1)
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(releaseApplication) })

	parallelSteps := []cleanupStep{
		{
			name: "blocked-application",
			fn: func() error {
				close(applicationStarted)
				defer close(applicationExited)
				<-releaseApplication
				return nil
			},
		},
	}
	infraSteps := []cleanupStep{
		{
			name: "redis",
			fn: func() error {
				infraStarted <- struct{}{}
				return nil
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- runProcessCleanup(ctx, parallelSteps, infraSteps)
	}()

	waitForCleanupSignal(t, applicationStarted, "blocked application cleanup step to start")
	err := waitForCleanupResult(t, result)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("runProcessCleanup() error = %v, want wrapping %v", err, context.DeadlineExceeded)
	}
	if !strings.Contains(err.Error(), "blocked-application") {
		t.Fatalf("runProcessCleanup() error = %v, want pending step name", err)
	}
	assertNoCleanupSignal(t, infraStarted, "infrastructure cleanup started after application timeout")

	releaseOnce.Do(func() { close(releaseApplication) })
	waitForCleanupSignal(t, applicationExited, "blocked application cleanup goroutine to exit")
	assertNoCleanupSignal(t, infraStarted, "infrastructure cleanup started after timed-out application step was released")
}

func TestRunProcessCleanupInfraTimeoutStopsRemainingInfra(t *testing.T) {
	firstInfraStarted := make(chan struct{})
	releaseFirstInfra := make(chan struct{})
	firstInfraExited := make(chan struct{})
	secondInfraStarted := make(chan struct{}, 1)
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(releaseFirstInfra) })

	infraSteps := []cleanupStep{
		{
			name: "blocked-redis",
			fn: func() error {
				close(firstInfraStarted)
				defer close(firstInfraExited)
				<-releaseFirstInfra
				return nil
			},
		},
		{
			name: "ent",
			fn: func() error {
				secondInfraStarted <- struct{}{}
				return nil
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- runProcessCleanup(ctx, nil, infraSteps)
	}()

	waitForCleanupSignal(t, firstInfraStarted, "first infrastructure cleanup step to start")
	err := waitForCleanupResult(t, result)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("runProcessCleanup() error = %v, want wrapping %v", err, context.DeadlineExceeded)
	}
	if !strings.Contains(err.Error(), "blocked-redis") {
		t.Fatalf("runProcessCleanup() error = %v, want pending step name", err)
	}
	assertNoCleanupSignal(t, secondInfraStarted, "second infrastructure cleanup step started after timeout")

	releaseOnce.Do(func() { close(releaseFirstInfra) })
	waitForCleanupSignal(t, firstInfraExited, "blocked infrastructure cleanup goroutine to exit")
	assertNoCleanupSignal(t, secondInfraStarted, "second infrastructure cleanup step started after timed-out step was released")
}

func TestRunProcessCleanupStepErrorsDoNotStopRemainingSteps(t *testing.T) {
	applicationErr := errors.New("application cleanup failed")
	infraErr := errors.New("infrastructure cleanup failed")
	var successfulApplicationCalls atomic.Int32
	var successfulInfraCalls atomic.Int32

	parallelSteps := []cleanupStep{
		{name: "failed-application", fn: func() error { return applicationErr }},
		{name: "successful-application", fn: func() error {
			successfulApplicationCalls.Add(1)
			return nil
		}},
	}
	infraSteps := []cleanupStep{
		{name: "failed-infra", fn: func() error { return infraErr }},
		{name: "successful-infra", fn: func() error {
			successfulInfraCalls.Add(1)
			return nil
		}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := runProcessCleanup(ctx, parallelSteps, infraSteps)

	if !errors.Is(err, applicationErr) {
		t.Errorf("runProcessCleanup() error = %v, want wrapping application error %v", err, applicationErr)
	}
	if !errors.Is(err, infraErr) {
		t.Errorf("runProcessCleanup() error = %v, want wrapping infrastructure error %v", err, infraErr)
	}
	if got := successfulApplicationCalls.Load(); got != 1 {
		t.Errorf("successful application cleanup calls = %d, want 1", got)
	}
	if got := successfulInfraCalls.Load(); got != 1 {
		t.Errorf("successful infrastructure cleanup calls = %d, want 1", got)
	}
}

func waitForCleanupSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitForCleanupResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runProcessCleanup to return")
		return nil
	}
}

func assertNoCleanupSignal(t *testing.T, signal <-chan struct{}, failureMessage string) {
	t.Helper()
	select {
	case <-signal:
		t.Fatal(failureMessage)
	case <-time.After(50 * time.Millisecond):
	}
}
