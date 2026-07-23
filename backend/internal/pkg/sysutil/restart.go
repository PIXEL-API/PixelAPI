// Package sysutil provides system-level utilities for process management.
package sysutil

import (
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"syscall"
	"time"
)

const restartSignalDelay = 100 * time.Millisecond

type processSignaler interface {
	Signal(os.Signal) error
}

type processSignalScheduler func(time.Duration, func())

// scheduleProcessSignal defers a process signal long enough for the current
// HTTP response to be flushed. Signal failures are reported asynchronously
// because the scheduling caller has already returned by then.
func scheduleProcessSignal(
	process processSignaler,
	signal os.Signal,
	delay time.Duration,
	schedule processSignalScheduler,
	onSignalError func(error),
) error {
	if process == nil {
		return errors.New("schedule process signal: nil process")
	}
	if signal == nil {
		return errors.New("schedule process signal: nil signal")
	}
	if delay <= 0 {
		return errors.New("schedule process signal: delay must be greater than zero")
	}
	if schedule == nil {
		return errors.New("schedule process signal: nil scheduler")
	}
	if onSignalError == nil {
		return errors.New("schedule process signal: nil signal error handler")
	}

	schedule(delay, func() {
		if err := process.Signal(signal); err != nil {
			onSignalError(err)
		}
	})
	return nil
}

// RestartService triggers a service restart by gracefully exiting.
//
// SIGHUP is handled by the main server lifecycle as a graceful restart request.
// After HTTP and application cleanup completes, the process exits non-zero so
// systemd Restart=on-failure (or Restart=always) starts the new process.
// This approach:
//   - Simple and reliable
//   - No sudo permissions needed
//   - No complex process management
//   - Leverages systemd's native restart capability
//
// Prerequisites:
//   - Linux OS with systemd
//   - Service configured with Restart=on-failure or Restart=always
func RestartService() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("service restart signal is only supported on Linux, current OS is %s", runtime.GOOS)
	}

	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		return fmt.Errorf("find current process: %w", err)
	}

	log.Println("Scheduling graceful service restart...")
	return scheduleProcessSignal(
		process,
		syscall.SIGHUP,
		restartSignalDelay,
		func(delay time.Duration, callback func()) {
			time.AfterFunc(delay, callback)
		},
		func(err error) {
			log.Printf("Failed to signal graceful service restart: %v", err)
		},
	)
}

// RestartServiceAsync is a fire-and-forget version of RestartService.
// It logs errors instead of returning them, suitable for goroutine usage.
func RestartServiceAsync() {
	if err := RestartService(); err != nil {
		log.Printf("Service restart failed: %v", err)
		log.Println("Please restart the service manually through the configured process supervisor")
	}
}
