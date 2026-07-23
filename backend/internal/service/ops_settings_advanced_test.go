package service

import (
	"context"
	"encoding/json"
	"testing"
)

func TestGetOpsAdvancedSettings_DefaultHidesOpenAITokenStats(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	svc := &OpsService{settingRepo: repo}

	cfg, err := svc.GetOpsAdvancedSettings(context.Background())
	if err != nil {
		t.Fatalf("GetOpsAdvancedSettings() error = %v", err)
	}
	if cfg.DisplayOpenAITokenStats {
		t.Fatalf("DisplayOpenAITokenStats = true, want false by default")
	}
	if !cfg.DisplayAlertEvents {
		t.Fatalf("DisplayAlertEvents = false, want true by default")
	}
	if !cfg.DataRetention.CleanupEnabled || cfg.DataRetention.CleanupSchedule != "0 4 * * *" {
		t.Fatalf("cleanup default = %v/%q, want enabled at 04:00", cfg.DataRetention.CleanupEnabled, cfg.DataRetention.CleanupSchedule)
	}
	if repo.setCalls != 1 {
		t.Fatalf("expected defaults to be persisted once, got %d", repo.setCalls)
	}
}

type opsDataRetentionApplierStub struct {
	called bool
	err    error
}

func (s *opsDataRetentionApplierStub) ReconcileDataRetentionSettings(context.Context) error {
	s.called = true
	return s.err
}

func TestUpdateOpsAdvancedSettingsAppliesCleanupScheduleImmediately(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	applier := &opsDataRetentionApplierStub{}
	svc := &OpsService{settingRepo: repo, cleanupSettingsApplier: applier}
	cfg := defaultOpsAdvancedSettings()
	cfg.DataRetention.CleanupSchedule = "15 4 * * *"
	cfg.DataRetention.ErrorLogRetentionDays = 0

	if _, err := svc.UpdateOpsAdvancedSettings(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if !applier.called {
		t.Fatal("cleanup settings were not reconciled")
	}
	if repo.setCalls != 1 {
		t.Fatalf("settings persisted %d times, want 1", repo.setCalls)
	}
}

func TestUpdateOpsAdvancedSettingsAllowsDisableWithHiddenInvalidCron(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	applier := &opsDataRetentionApplierStub{}
	svc := &OpsService{settingRepo: repo, cleanupSettingsApplier: applier}
	cfg := defaultOpsAdvancedSettings()
	cfg.DataRetention.CleanupEnabled = false
	cfg.DataRetention.CleanupSchedule = "invalid cron"

	if _, err := svc.UpdateOpsAdvancedSettings(context.Background(), cfg); err != nil {
		t.Fatalf("disabled cleanup must be saveable even with a hidden stale cron: %v", err)
	}
	if repo.setCalls != 1 || !applier.called {
		t.Fatalf("disabled settings not persisted/reconciled: setCalls=%d called=%v", repo.setCalls, applier.called)
	}
}

func TestGetOpsAdvancedSettingsRejectsCorruptedJSON(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	repo.values[SettingKeyOpsAdvancedSettings] = "{invalid"
	svc := &OpsService{settingRepo: repo}

	if _, err := svc.GetOpsAdvancedSettings(context.Background()); err == nil {
		t.Fatal("corrupted advanced settings must not be presented as healthy defaults")
	}
}

func TestUpdateOpsAdvancedSettingsRejectsInvalidCleanupCronBeforePersist(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	applier := &opsDataRetentionApplierStub{}
	svc := &OpsService{settingRepo: repo, cleanupSettingsApplier: applier}
	cfg := defaultOpsAdvancedSettings()
	cfg.DataRetention.CleanupSchedule = "invalid cron"

	if _, err := svc.UpdateOpsAdvancedSettings(context.Background(), cfg); err == nil {
		t.Fatal("invalid cleanup cron must be rejected")
	}
	if repo.setCalls != 0 || applier.called {
		t.Fatalf("invalid settings must not persist or apply: setCalls=%d called=%v", repo.setCalls, applier.called)
	}
}

func TestUpdateOpsAdvancedSettings_PersistsOpenAITokenStatsVisibility(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	svc := &OpsService{settingRepo: repo}

	cfg := defaultOpsAdvancedSettings()
	cfg.DisplayOpenAITokenStats = true
	cfg.DisplayAlertEvents = false

	updated, err := svc.UpdateOpsAdvancedSettings(context.Background(), cfg)
	if err != nil {
		t.Fatalf("UpdateOpsAdvancedSettings() error = %v", err)
	}
	if !updated.DisplayOpenAITokenStats {
		t.Fatalf("DisplayOpenAITokenStats = false, want true")
	}
	if updated.DisplayAlertEvents {
		t.Fatalf("DisplayAlertEvents = true, want false")
	}

	reloaded, err := svc.GetOpsAdvancedSettings(context.Background())
	if err != nil {
		t.Fatalf("GetOpsAdvancedSettings() after update error = %v", err)
	}
	if !reloaded.DisplayOpenAITokenStats {
		t.Fatalf("reloaded DisplayOpenAITokenStats = false, want true")
	}
	if reloaded.DisplayAlertEvents {
		t.Fatalf("reloaded DisplayAlertEvents = true, want false")
	}
}

func TestGetOpsAdvancedSettings_BackfillsNewDisplayFlagsFromDefaults(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	svc := &OpsService{settingRepo: repo}

	legacyCfg := map[string]any{
		"data_retention": map[string]any{
			"cleanup_enabled":               false,
			"cleanup_schedule":              "0 2 * * *",
			"error_log_retention_days":      30,
			"minute_metrics_retention_days": 30,
			"hourly_metrics_retention_days": 30,
		},
		"aggregation": map[string]any{
			"aggregation_enabled": false,
		},
		"ignore_count_tokens_errors":    true,
		"ignore_context_canceled":       true,
		"ignore_no_available_accounts":  false,
		"ignore_invalid_api_key_errors": false,
		"auto_refresh_enabled":          false,
		"auto_refresh_interval_seconds": 30,
	}
	raw, err := json.Marshal(legacyCfg)
	if err != nil {
		t.Fatalf("marshal legacy config: %v", err)
	}
	repo.values[SettingKeyOpsAdvancedSettings] = string(raw)

	cfg, err := svc.GetOpsAdvancedSettings(context.Background())
	if err != nil {
		t.Fatalf("GetOpsAdvancedSettings() error = %v", err)
	}
	if cfg.DisplayOpenAITokenStats {
		t.Fatalf("DisplayOpenAITokenStats = true, want false default backfill")
	}
	if !cfg.DisplayAlertEvents {
		t.Fatalf("DisplayAlertEvents = false, want true default backfill")
	}
}
