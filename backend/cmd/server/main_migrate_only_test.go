package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestCommandOptionsValidateMigrateOnly(t *testing.T) {
	tests := []struct {
		name    string
		options commandOptions
		wantErr string
	}{
		{
			name: "valid",
			options: commandOptions{
				migrateOnly:      true,
				migrationTimeout: defaultMigrationTimeout,
			},
		},
		{
			name: "setup conflict",
			options: commandOptions{
				setupMode:        true,
				migrateOnly:      true,
				migrationTimeout: defaultMigrationTimeout,
			},
			wantErr: "cannot be combined with --setup",
		},
		{
			name: "version conflict",
			options: commandOptions{
				showVersion:      true,
				migrateOnly:      true,
				migrationTimeout: defaultMigrationTimeout,
			},
			wantErr: "cannot be combined with --version",
		},
		{
			name: "zero timeout",
			options: commandOptions{
				migrateOnly: true,
			},
			wantErr: "must be greater than zero",
		},
		{
			name: "negative timeout",
			options: commandOptions{
				migrateOnly:      true,
				migrationTimeout: -time.Second,
			},
			wantErr: "must be greater than zero",
		},
		{
			name:    "timeout ignored without migrate mode",
			options: commandOptions{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.options.validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validate() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validate() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRunMigrationsOnlyUsesLoadedConfigAndDeadline(t *testing.T) {
	wantConfig := &config.Config{}
	var gotConfig *config.Config
	var gotDeadline bool

	err := runMigrationsOnly(
		context.Background(),
		time.Minute,
		func() (*config.Config, error) {
			return wantConfig, nil
		},
		func(ctx context.Context, cfg *config.Config) error {
			gotConfig = cfg
			_, gotDeadline = ctx.Deadline()
			return nil
		},
	)
	if err != nil {
		t.Fatalf("runMigrationsOnly() error = %v", err)
	}
	if gotConfig != wantConfig {
		t.Fatalf("migration config pointer = %p, want %p", gotConfig, wantConfig)
	}
	if !gotDeadline {
		t.Fatal("migration context has no deadline")
	}
}

func TestRunMigrationsOnlyRejectsInvalidDependencies(t *testing.T) {
	validLoader := func() (*config.Config, error) { return &config.Config{}, nil }
	validRunner := func(context.Context, *config.Config) error { return nil }
	var nilContext context.Context

	tests := []struct {
		name       string
		ctx        context.Context
		timeout    time.Duration
		loadConfig bootstrapConfigLoader
		runner     configuredMigrationRunner
		wantErr    string
	}{
		{name: "nil context", ctx: nilContext, timeout: time.Minute, loadConfig: validLoader, runner: validRunner, wantErr: "nil parent context"},
		{name: "zero timeout", ctx: context.Background(), loadConfig: validLoader, runner: validRunner, wantErr: "must be greater than zero"},
		{name: "nil loader", ctx: context.Background(), timeout: time.Minute, runner: validRunner, wantErr: "nil bootstrap config loader"},
		{name: "nil runner", ctx: context.Background(), timeout: time.Minute, loadConfig: validLoader, wantErr: "nil configured migration runner"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runMigrationsOnly(tt.ctx, tt.timeout, tt.loadConfig, tt.runner)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("runMigrationsOnly() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRunMigrationsOnlyStopsOnConfigError(t *testing.T) {
	wantErr := errors.New("config unavailable")
	runnerCalled := false

	err := runMigrationsOnly(
		context.Background(),
		time.Minute,
		func() (*config.Config, error) { return nil, wantErr },
		func(context.Context, *config.Config) error {
			runnerCalled = true
			return nil
		},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("runMigrationsOnly() error = %v, want wrapping %v", err, wantErr)
	}
	if runnerCalled {
		t.Fatal("migration runner called after config load failure")
	}
}

func TestRunMigrationsOnlyPropagatesMigrationError(t *testing.T) {
	wantErr := context.DeadlineExceeded
	err := runMigrationsOnly(
		context.Background(),
		time.Minute,
		func() (*config.Config, error) { return &config.Config{}, nil },
		func(context.Context, *config.Config) error { return wantErr },
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("runMigrationsOnly() error = %v, want wrapping %v", err, wantErr)
	}
}
