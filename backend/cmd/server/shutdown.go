package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

const pprofShutdownTimeout = 5 * time.Second

var errServerRestartRequested = errors.New("service restart requested")

type shutdownHTTPServer interface {
	Shutdown(context.Context) error
	Close() error
}

type shutdownTarget struct {
	name    string
	server  shutdownHTTPServer
	timeout time.Duration
}

type serverServeResult struct {
	name string
	err  error
}

// runServerLifecycle waits for an operator stop or an unexpected server exit,
// then always runs HTTP shutdown and application cleanup. Errors encountered
// during an operator-initiated stop are logged but do not turn a normal
// systemd stop into a failed process exit.
func runServerLifecycle(
	stop <-chan struct{},
	restart <-chan struct{},
	serveResults <-chan serverServeResult,
	targets []shutdownTarget,
	cleanup func(context.Context) error,
	cleanupTimeout time.Duration,
) error {
	if stop == nil {
		return errors.New("server lifecycle: nil stop channel")
	}
	if restart == nil {
		return errors.New("server lifecycle: nil restart channel")
	}
	if serveResults == nil {
		return errors.New("server lifecycle: nil serve result channel")
	}
	if cleanup == nil {
		return errors.New("server lifecycle: nil cleanup function")
	}
	if cleanupTimeout <= 0 {
		return errors.New("server lifecycle: cleanup timeout must be greater than zero")
	}

	lifecycleErr := waitForServerLifecycleTrigger(stop, restart, serveResults)
	switch {
	case lifecycleErr == nil:
		log.Println("Shutting down server...")
	case errors.Is(lifecycleErr, errServerRestartRequested):
		log.Println("Graceful service restart requested...")
	default:
		log.Printf("Server lifecycle error: %v", lifecycleErr)
	}

	if err := shutdownHTTPServers(targets...); err != nil {
		log.Printf("Server shutdown completed with forced-close errors: %v", err)
	}

	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), cleanupTimeout)
	cleanupErr := cleanup(cleanupCtx)
	cancelCleanup()
	if cleanupErr != nil {
		log.Printf("Application cleanup completed with errors: %v", cleanupErr)
	}

	return lifecycleErr
}

func waitForServerLifecycleTrigger(
	stop <-chan struct{},
	restart <-chan struct{},
	serveResults <-chan serverServeResult,
) error {
	select {
	case <-stop:
		return nil
	case <-restart:
		if channelReady(stop) {
			return nil
		}
		return errServerRestartRequested
	case result := <-serveResults:
		// Operator stop has the highest priority, followed by an explicit
		// restart request. This removes random exit-code selection when more
		// than one channel becomes ready at the same time.
		if channelReady(stop) {
			return nil
		}
		if channelReady(restart) {
			return errServerRestartRequested
		}

		name := strings.TrimSpace(result.name)
		if name == "" {
			name = "HTTP server"
		}
		if result.err == nil {
			return fmt.Errorf("%s stopped unexpectedly without an error", name)
		}
		return fmt.Errorf("%s stopped unexpectedly: %w", name, result.err)
	}
}

func channelReady(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// shutdownHTTPServers gives each server an independent graceful-shutdown
// budget. If graceful shutdown fails, Close is used to release HTTP/SSE
// connections before the next target is processed.
func shutdownHTTPServers(targets ...shutdownTarget) error {
	var shutdownErrors []error
	for _, target := range targets {
		name := strings.TrimSpace(target.name)
		if name == "" {
			name = "HTTP server"
		}
		if target.server == nil {
			shutdownErrors = append(shutdownErrors, fmt.Errorf("%s shutdown: nil server", name))
			continue
		}
		if target.timeout <= 0 {
			shutdownErrors = append(shutdownErrors, fmt.Errorf("%s shutdown: timeout must be greater than zero", name))
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), target.timeout)
		shutdownErr := target.server.Shutdown(ctx)
		cancel()
		if shutdownErr == nil {
			continue
		}

		shutdownErr = fmt.Errorf("%s graceful shutdown: %w", name, shutdownErr)
		if closeErr := target.server.Close(); closeErr != nil {
			shutdownErrors = append(shutdownErrors, errors.Join(
				shutdownErr,
				fmt.Errorf("%s forced close: %w", name, closeErr),
			))
			continue
		}
		shutdownErrors = append(shutdownErrors, shutdownErr)
	}
	return errors.Join(shutdownErrors...)
}

type cleanupStep struct {
	name string
	fn   func() error
}

type cleanupStepResult struct {
	index int
	err   error
}

// runProcessCleanup enforces a process-exit deadline around cleanup
// orchestration. A blocking step is not context-cancelled; on timeout the
// caller must return from the main process so the runtime can reclaim it.
func runProcessCleanup(ctx context.Context, parallelSteps, infraSteps []cleanupStep) error {
	if ctx == nil {
		return errors.New("process cleanup: nil context")
	}
	if err := validateCleanupSteps(parallelSteps, infraSteps); err != nil {
		return err
	}

	var cleanupErrors []error
	results := make(chan cleanupStepResult, len(parallelSteps))
	pending := make(map[int]string, len(parallelSteps))
	for index, step := range parallelSteps {
		pending[index] = step.name
		go func(index int, step cleanupStep) {
			results <- cleanupStepResult{index: index, err: step.fn()}
		}(index, step)
	}

	for len(pending) > 0 {
		select {
		case result := <-results:
			name := pending[result.index]
			delete(pending, result.index)
			if result.err != nil {
				wrapped := fmt.Errorf("cleanup %s: %w", name, result.err)
				cleanupErrors = append(cleanupErrors, wrapped)
				log.Printf("[Cleanup] %s failed: %v", name, result.err)
				continue
			}
			log.Printf("[Cleanup] %s succeeded", name)
		case <-ctx.Done():
			return errors.Join(
				append(cleanupErrors, cleanupDeadlineError(ctx.Err(), pendingCleanupNames(pending)))...,
			)
		}
	}

	for index, step := range infraSteps {
		if err := ctx.Err(); err != nil {
			return errors.Join(
				append(cleanupErrors, cleanupDeadlineError(err, cleanupStepNames(infraSteps[index:])))...,
			)
		}

		result := make(chan error, 1)
		go func(step cleanupStep) {
			result <- step.fn()
		}(step)
		select {
		case err := <-result:
			if err != nil {
				wrapped := fmt.Errorf("cleanup %s: %w", step.name, err)
				cleanupErrors = append(cleanupErrors, wrapped)
				log.Printf("[Cleanup] %s failed: %v", step.name, err)
				continue
			}
			log.Printf("[Cleanup] %s succeeded", step.name)
		case <-ctx.Done():
			return errors.Join(
				append(cleanupErrors, cleanupDeadlineError(ctx.Err(), cleanupStepNames(infraSteps[index:])))...,
			)
		}
	}

	if len(cleanupErrors) == 0 {
		log.Printf("[Cleanup] All cleanup steps completed")
	}
	return errors.Join(cleanupErrors...)
}

func validateCleanupSteps(stepGroups ...[]cleanupStep) error {
	for _, steps := range stepGroups {
		for index, step := range steps {
			if strings.TrimSpace(step.name) == "" {
				return fmt.Errorf("process cleanup: step %d has an empty name", index)
			}
			if step.fn == nil {
				return fmt.Errorf("process cleanup: step %s has a nil function", step.name)
			}
		}
	}
	return nil
}

func cleanupDeadlineError(cause error, pending []string) error {
	return fmt.Errorf(
		"process cleanup deadline reached with pending steps [%s]: %w",
		strings.Join(pending, ", "),
		cause,
	)
}

func pendingCleanupNames(pending map[int]string) []string {
	names := make([]string, 0, len(pending))
	for _, name := range pending {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func cleanupStepNames(steps []cleanupStep) []string {
	names := make([]string, 0, len(steps))
	for _, step := range steps {
		names = append(names, step.name)
	}
	return names
}
