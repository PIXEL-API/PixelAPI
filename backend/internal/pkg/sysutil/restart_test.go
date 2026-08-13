package sysutil

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

type recordingProcessSignaler struct {
	signals   []os.Signal
	signalErr error
}

func (p *recordingProcessSignaler) Signal(signal os.Signal) error {
	p.signals = append(p.signals, signal)
	return p.signalErr
}

func TestScheduleProcessSignalDefersSignal(t *testing.T) {
	process := &recordingProcessSignaler{}
	wantDelay := 175 * time.Millisecond
	var (
		scheduleCalls  int
		scheduledDelay time.Duration
		callback       func()
		handlerCalls   int
	)

	err := scheduleProcessSignal(
		process,
		syscall.SIGHUP,
		wantDelay,
		func(delay time.Duration, scheduledCallback func()) {
			scheduleCalls++
			scheduledDelay = delay
			callback = scheduledCallback
		},
		func(error) {
			handlerCalls++
		},
	)
	if err != nil {
		t.Fatalf("scheduleProcessSignal() error = %v", err)
	}
	if scheduleCalls != 1 {
		t.Fatalf("scheduler calls = %d, want 1", scheduleCalls)
	}
	if scheduledDelay != wantDelay {
		t.Fatalf("scheduled delay = %s, want %s", scheduledDelay, wantDelay)
	}
	if callback == nil {
		t.Fatal("scheduler received a nil callback")
	}
	if len(process.signals) != 0 {
		t.Fatalf("Signal called synchronously %d times, want 0", len(process.signals))
	}
	if handlerCalls != 0 {
		t.Fatalf("error handler called synchronously %d times, want 0", handlerCalls)
	}

	callback()

	if len(process.signals) != 1 {
		t.Fatalf("Signal calls after callback = %d, want 1", len(process.signals))
	}
	if process.signals[0] != syscall.SIGHUP {
		t.Fatalf("Signal argument = %v, want %v", process.signals[0], syscall.SIGHUP)
	}
	if handlerCalls != 0 {
		t.Fatalf("error handler calls after successful signal = %d, want 0", handlerCalls)
	}
}

func TestScheduleProcessSignalReportsAsynchronousSignalError(t *testing.T) {
	signalErr := errors.New("signal failed")
	process := &recordingProcessSignaler{signalErr: signalErr}
	var (
		callback      func()
		reportedError error
		handlerCalls  int
	)

	err := scheduleProcessSignal(
		process,
		syscall.SIGHUP,
		time.Millisecond,
		func(_ time.Duration, scheduledCallback func()) {
			callback = scheduledCallback
		},
		func(err error) {
			handlerCalls++
			reportedError = err
		},
	)
	if err != nil {
		t.Fatalf("scheduleProcessSignal() error = %v", err)
	}
	if callback == nil {
		t.Fatal("scheduler received a nil callback")
	}
	if handlerCalls != 0 {
		t.Fatalf("error handler called before scheduled callback: %d times", handlerCalls)
	}

	callback()

	if len(process.signals) != 1 {
		t.Fatalf("Signal calls = %d, want 1", len(process.signals))
	}
	if handlerCalls != 1 {
		t.Fatalf("error handler calls = %d, want 1", handlerCalls)
	}
	if !errors.Is(reportedError, signalErr) {
		t.Fatalf("reported error = %v, want %v", reportedError, signalErr)
	}
}

func TestScheduleProcessSignalRejectsInvalidArguments(t *testing.T) {
	tests := []struct {
		name          string
		process       processSignaler
		signal        os.Signal
		delay         time.Duration
		nilScheduler  bool
		nilErrHandler bool
	}{
		{name: "nil process", process: nil, signal: syscall.SIGHUP, delay: time.Millisecond},
		{name: "nil signal", process: &recordingProcessSignaler{}, signal: nil, delay: time.Millisecond},
		{name: "zero delay", process: &recordingProcessSignaler{}, signal: syscall.SIGHUP, delay: 0},
		{name: "negative delay", process: &recordingProcessSignaler{}, signal: syscall.SIGHUP, delay: -time.Millisecond},
		{name: "nil scheduler", process: &recordingProcessSignaler{}, signal: syscall.SIGHUP, delay: time.Millisecond, nilScheduler: true},
		{name: "nil error handler", process: &recordingProcessSignaler{}, signal: syscall.SIGHUP, delay: time.Millisecond, nilErrHandler: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheduleCalls := 0
			handlerCalls := 0
			var scheduler processSignalScheduler = func(time.Duration, func()) {
				scheduleCalls++
			}
			if tt.nilScheduler {
				scheduler = nil
			}
			var errorHandler = func(error) {
				handlerCalls++
			}
			if tt.nilErrHandler {
				errorHandler = nil
			}

			err := scheduleProcessSignal(tt.process, tt.signal, tt.delay, scheduler, errorHandler)
			if err == nil {
				t.Fatal("scheduleProcessSignal() error = nil, want validation error")
			}
			if scheduleCalls != 0 {
				t.Fatalf("scheduler calls = %d, want 0", scheduleCalls)
			}
			if handlerCalls != 0 {
				t.Fatalf("error handler calls = %d, want 0", handlerCalls)
			}
			if process, ok := tt.process.(*recordingProcessSignaler); ok && len(process.signals) != 0 {
				t.Fatalf("Signal calls = %d, want 0", len(process.signals))
			}
		})
	}
}

func TestRestartServiceReturnsErrorOutsideLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("calling RestartService on Linux would signal the real test process")
	}

	err := RestartService()
	if err == nil {
		t.Fatalf("RestartService() error = nil on %s, want unsupported-platform error", runtime.GOOS)
	}
	if !strings.Contains(err.Error(), runtime.GOOS) {
		t.Fatalf("RestartService() error = %q, want current OS %q", err, runtime.GOOS)
	}
}
