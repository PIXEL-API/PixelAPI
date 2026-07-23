package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestOpsCleanupPlan(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, loc)
	dayStart := time.Date(2026, 4, 29, 0, 0, 0, 0, loc)

	cases := []struct {
		name       string
		days       int
		wantOK     bool
		wantCutoff time.Time
	}{
		{name: "negative skips", days: -1, wantOK: false},
		{name: "zero disables", days: 0, wantOK: false},
		{name: "positive yields natural-day cutoff", days: 7, wantOK: true, wantCutoff: dayStart.AddDate(0, 0, -7)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cutoff, ok := opsCleanupPlan(now, tc.days)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if !cutoff.Equal(tc.wantCutoff) {
				t.Fatalf("cutoff = %v, want %v", cutoff, tc.wantCutoff)
			}
		})
	}
}

func TestLoadOpsCleanupLocation(t *testing.T) {
	if _, err := loadOpsCleanupLocation("  "); err == nil {
		t.Fatal("empty timezone must be rejected")
	}
	if _, err := loadOpsCleanupLocation("Invalid/Timezone"); err == nil {
		t.Fatal("invalid timezone must be rejected")
	}
	loc, err := loadOpsCleanupLocation(" Asia/Shanghai ")
	if err != nil {
		t.Fatal(err)
	}
	if loc.String() != "Asia/Shanghai" {
		t.Fatalf("location = %q, want Asia/Shanghai", loc.String())
	}
}

func TestConfiguredOpsCleanupLeaderLockTTL(t *testing.T) {
	cfg := testOpsCleanupConfig()
	if got := configuredOpsCleanupLeaderLockTTL(cfg); got != opsCleanupLeaderLockTTLDefault {
		t.Fatalf("short run lock TTL = %s, want %s", got, opsCleanupLeaderLockTTLDefault)
	}
	cfg.Ops.Cleanup.RunTimeoutSeconds = 5 * 60 * 60
	want := 5*time.Hour + opsCleanupLeaderLockTTLGrace
	if got := configuredOpsCleanupLeaderLockTTL(cfg); got != want {
		t.Fatalf("long run lock TTL = %s, want %s", got, want)
	}
}

type opsCleanupArchiveStub struct {
	create func(context.Context, DataArchiveInput) (*BackupRecord, error)
}

func (s *opsCleanupArchiveStub) CreateDataArchive(ctx context.Context, input DataArchiveInput) (*BackupRecord, error) {
	return s.create(ctx, input)
}

func testOpsCleanupConfig() *config.Config {
	return &config.Config{
		Timezone: "Asia/Shanghai",
		Ops: config.OpsConfig{Enabled: true, Cleanup: config.OpsCleanupConfig{
			Enabled:                    true,
			Schedule:                   "0 4 * * *",
			ArchiveExpireDays:          30,
			ArchiveWindowDays:          1,
			MaxCatchupWindowsPerRun:    2,
			ArchiveTimeoutSeconds:      1,
			DeleteTimeoutSeconds:       1,
			RunTimeoutSeconds:          10,
			DeleteBatchSize:            5000,
			ErrorLogRetentionDays:      30,
			MinuteMetricsRetentionDays: 0,
			HourlyMetricsRetentionDays: 0,
		}},
	}
}

func TestOpsCleanupEffectiveConfigUsesAdvancedSettings(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	advanced := defaultOpsAdvancedSettings()
	advanced.DataRetention = OpsDataRetentionSettings{
		CleanupEnabled:             true,
		CleanupSchedule:            "15 4 * * *",
		ErrorLogRetentionDays:      11,
		MinuteMetricsRetentionDays: 0,
		HourlyMetricsRetentionDays: 22,
	}
	raw, err := json.Marshal(advanced)
	if err != nil {
		t.Fatal(err)
	}
	repo.values[SettingKeyOpsAdvancedSettings] = string(raw)
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := NewOpsCleanupService(&opsRepoMock{}, repo, db, nil, testOpsCleanupConfig(), nil, nil)

	got, err := svc.loadEffectiveCleanupConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.Schedule != "15 4 * * *" {
		t.Fatalf("effective enabled/schedule = %v/%q", got.Enabled, got.Schedule)
	}
	if got.ErrorLogRetentionDays != 11 || got.MinuteMetricsRetentionDays != 0 || got.HourlyMetricsRetentionDays != 22 {
		t.Fatalf("effective retention = %d/%d/%d", got.ErrorLogRetentionDays, got.MinuteMetricsRetentionDays, got.HourlyMetricsRetentionDays)
	}
	if got.ArchiveExpireDays != 30 || got.DeleteBatchSize != 5000 {
		t.Fatalf("static execution controls were not preserved: %+v", got)
	}
}

func TestOpsCleanupMalformedAdvancedSettingsFailsClosed(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	repo.values[SettingKeyOpsAdvancedSettings] = "{invalid"
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := NewOpsCleanupService(&opsRepoMock{}, repo, db, nil, testOpsCleanupConfig(), nil, nil)

	if _, err := svc.loadEffectiveCleanupConfig(context.Background()); err == nil {
		t.Fatal("malformed advanced settings must reject cleanup")
	}
}

func TestOpsCleanupReconcileDataRetentionReschedulesAndDisables(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := newRuntimeSettingRepoStub()
	svc := NewOpsCleanupService(&opsRepoMock{}, repo, db, nil, testOpsCleanupConfig(), nil, nil)
	svc.Start()
	defer svc.Stop()

	advanced := defaultOpsAdvancedSettings()
	advanced.DataRetention = OpsDataRetentionSettings{
		CleanupEnabled:             true,
		CleanupSchedule:            "15 4 * * *",
		ErrorLogRetentionDays:      30,
		MinuteMetricsRetentionDays: 30,
		HourlyMetricsRetentionDays: 30,
	}
	raw, err := json.Marshal(advanced)
	if err != nil {
		t.Fatal(err)
	}
	repo.values[SettingKeyOpsAdvancedSettings] = string(raw)
	if err := svc.ReconcileDataRetentionSettings(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc.cronMu.Lock()
	entryID := svc.cronEntryID
	entries := svc.cron.Entries()
	svc.cronMu.Unlock()
	if entryID == 0 || len(entries) != 1 || entries[0].ID != entryID {
		t.Fatalf("rescheduled entries = %+v, entryID=%d", entries, entryID)
	}
	if entries[0].Next.Hour() != 4 || entries[0].Next.Minute() != 15 {
		t.Fatalf("next run = %s, want 04:15", entries[0].Next)
	}

	advanced.DataRetention.CleanupEnabled = false
	advanced.DataRetention.CleanupSchedule = "stale invalid cron"
	raw, err = json.Marshal(advanced)
	if err != nil {
		t.Fatal(err)
	}
	repo.values[SettingKeyOpsAdvancedSettings] = string(raw)
	if err := svc.ReconcileDataRetentionSettings(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc.cronMu.Lock()
	defer svc.cronMu.Unlock()
	if svc.cronEntryID != 0 || len(svc.cron.Entries()) != 0 {
		t.Fatalf("disabled scheduler still has entries: %+v", svc.cron.Entries())
	}
}

func TestOpsCleanupTriggerMatchesEffectiveSchedule(t *testing.T) {
	if !opsCleanupTriggerMatches(" 0 4 * * * ", "0 4 * * *") {
		t.Fatal("equivalent cron specifications should match")
	}
	if opsCleanupTriggerMatches("0 4 * * *", "15 4 * * *") {
		t.Fatal("stale cron specification must not match the effective schedule")
	}
}

func TestOpsCleanupStartupSettingsFailureCanReconcileLater(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	loadFails := true
	repo.getValueFn = func(key string) (string, error) {
		if loadFails {
			return "", errors.New("temporary database error")
		}
		value, ok := repo.values[key]
		if !ok {
			return "", ErrSettingNotFound
		}
		return value, nil
	}
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := NewOpsCleanupService(&opsRepoMock{}, repo, db, nil, testOpsCleanupConfig(), nil, nil)
	svc.Start()
	defer svc.Stop()
	svc.cronMu.Lock()
	initialEntryID := svc.cronEntryID
	svc.cronMu.Unlock()
	if initialEntryID != 0 {
		t.Fatalf("initial entry = %d, want none after settings failure", initialEntryID)
	}

	advanced := defaultOpsAdvancedSettings()
	raw, err := json.Marshal(advanced)
	if err != nil {
		t.Fatal(err)
	}
	repo.values[SettingKeyOpsAdvancedSettings] = string(raw)
	loadFails = false
	if err := svc.ReconcileDataRetentionSettings(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc.cronMu.Lock()
	recoveredEntryID := svc.cronEntryID
	lifecycleCtx := svc.lifecycleCtx
	svc.cronMu.Unlock()
	if recoveredEntryID == 0 {
		t.Fatal("settings reconcile did not recover the cleanup entry")
	}
	svc.Stop()
	select {
	case <-lifecycleCtx.Done():
	default:
		t.Fatal("Stop did not cancel the cleanup lifecycle context")
	}
}

func TestOpsCleanupErrorArchiveFailureStillAdvancesSystemLogs(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	oldest := time.Now().UTC().AddDate(0, 0, -40)
	mock.ExpectQuery(`SELECT MIN\(created_at\) FROM ops_error_logs`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"min"}).AddRow(oldest))
	mock.ExpectQuery(`SELECT MIN\(created_at\) FROM ops_system_logs`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"min"}).AddRow(oldest))
	mock.ExpectExec(`DELETE FROM ops_system_logs`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), 5000).
		WillReturnResult(sqlmock.NewResult(0, 1))
	for _, table := range []string{"ops_retry_attempts", "ops_alert_events", "ops_system_log_cleanup_audits"} {
		mock.ExpectExec(`DELETE FROM `+table).
			WithArgs(sqlmock.AnyArg(), 5000).
			WillReturnResult(sqlmock.NewResult(0, 0))
	}
	repo := &opsRepoMock{
		ExportErrorLogsFn: func(context.Context, *OpsErrorLogCleanupFilter) (io.ReadCloser, error) {
			return nil, errors.New("error archive unavailable")
		},
		ExportSystemLogsFn: func(context.Context, *OpsSystemLogCleanupFilter) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("system\n")), nil
		},
	}
	cfg := testOpsCleanupConfig()
	cfg.Ops.Cleanup.MaxCatchupWindowsPerRun = 1
	svc := NewOpsCleanupService(repo, nil, db, nil, cfg, nil, &opsCleanupArchiveStub{
		create: func(context.Context, DataArchiveInput) (*BackupRecord, error) {
			return &BackupRecord{Status: "completed"}, nil
		},
	})
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	counts, err := svc.runCleanupOnceWithConfig(context.Background(), cfg.Ops.Cleanup, loc)
	if err == nil || !strings.Contains(err.Error(), "error archive unavailable") {
		t.Fatalf("error = %v, want error archive failure", err)
	}
	if counts.errorLogs != 0 || counts.systemLogs != 1 {
		t.Fatalf("counts = %+v, want system logs to advance independently", counts)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOpsCleanupArchiveFailureDoesNotDelete(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	oldest := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT MIN\(created_at\) FROM ops_error_logs`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"min"}).AddRow(oldest))
	repo := &opsRepoMock{ExportErrorLogsFn: func(context.Context, *OpsErrorLogCleanupFilter) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("row\n")), nil
	}}
	svc := NewOpsCleanupService(repo, nil, db, nil, testOpsCleanupConfig(), nil, &opsCleanupArchiveStub{
		create: func(context.Context, DataArchiveInput) (*BackupRecord, error) {
			return nil, errors.New("upload failed")
		},
	})

	deleted, err := svc.cleanupOpsLogWindows(context.Background(), "ops_error_logs", "created_at", oldest.AddDate(0, 0, 10), svc.exportOpsErrorLogWindow)
	if err == nil || !strings.Contains(err.Error(), "upload failed") {
		t.Fatalf("error = %v, want upload failure", err)
	}
	if deleted != 0 {
		t.Fatalf("deleted = %d, want 0", deleted)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOpsCleanupWindowsAdvanceByBoundedDay(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	first := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	second := first.AddDate(0, 0, 1)
	for _, oldest := range []time.Time{first, second} {
		mock.ExpectQuery(`SELECT MIN\(created_at\) FROM ops_error_logs`).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"min"}).AddRow(oldest))
		mock.ExpectExec(`DELETE FROM ops_error_logs`).
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), 5000).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}
	var exported []opsCleanupWindow
	repo := &opsRepoMock{ExportErrorLogsFn: func(_ context.Context, filter *OpsErrorLogCleanupFilter) (io.ReadCloser, error) {
		exported = append(exported, opsCleanupWindow{Start: *filter.StartTime, End: *filter.EndTime})
		return io.NopCloser(strings.NewReader("row\n")), nil
	}}
	svc := NewOpsCleanupService(repo, nil, db, nil, testOpsCleanupConfig(), nil, &opsCleanupArchiveStub{
		create: func(context.Context, DataArchiveInput) (*BackupRecord, error) {
			return &BackupRecord{Status: "completed"}, nil
		},
	})

	deleted, err := svc.cleanupOpsLogWindows(context.Background(), "ops_error_logs", "created_at", first.AddDate(0, 0, 10), svc.exportOpsErrorLogWindow)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 || len(exported) != 2 {
		t.Fatalf("deleted=%d exported=%d, want 2/2", deleted, len(exported))
	}
	if got := exported[0].End.Sub(exported[0].Start); got != 24*time.Hour {
		t.Fatalf("first window duration = %s, want 24h", got)
	}
	if !exported[1].Start.Equal(exported[0].End) {
		t.Fatalf("second window starts at %s, want %s", exported[1].Start, exported[0].End)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOpsCleanupArchiveTimeoutDoesNotDelete(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	oldest := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT MIN\(created_at\) FROM ops_error_logs`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"min"}).AddRow(oldest))
	repo := &opsRepoMock{ExportErrorLogsFn: func(context.Context, *OpsErrorLogCleanupFilter) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("row\n")), nil
	}}
	svc := NewOpsCleanupService(repo, nil, db, nil, testOpsCleanupConfig(), nil, &opsCleanupArchiveStub{
		create: func(ctx context.Context, _ DataArchiveInput) (*BackupRecord, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})

	deleted, err := svc.cleanupOpsLogWindows(context.Background(), "ops_error_logs", "created_at", oldest.AddDate(0, 0, 10), svc.exportOpsErrorLogWindow)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if deleted != 0 {
		t.Fatalf("deleted = %d, want 0", deleted)
	}
}

func TestOpsCleanupZeroRetentionDisablesTargets(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := testOpsCleanupConfig()
	cfg.Ops.Cleanup.ErrorLogRetentionDays = 0
	svc := NewOpsCleanupService(&opsRepoMock{}, nil, db, nil, cfg, nil, nil)
	counts, err := svc.runCleanupOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if counts != (opsCleanupDeletedCounts{}) {
		t.Fatalf("counts = %+v, want zero", counts)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOpsCleanupArchivesBothLogTablesBeforeAuxiliaryDeletes(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	oldest := time.Now().UTC().AddDate(0, 0, -40)
	mock.ExpectQuery(`SELECT MIN\(created_at\) FROM ops_error_logs`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"min"}).AddRow(oldest))
	mock.ExpectExec(`DELETE FROM ops_error_logs`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), 5000).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT MIN\(created_at\) FROM ops_system_logs`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"min"}).AddRow(oldest))
	mock.ExpectExec(`DELETE FROM ops_system_logs`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), 5000).
		WillReturnResult(sqlmock.NewResult(0, 1))
	for _, table := range []string{"ops_retry_attempts", "ops_alert_events", "ops_system_log_cleanup_audits"} {
		mock.ExpectExec(`DELETE FROM `+table).
			WithArgs(sqlmock.AnyArg(), 5000).
			WillReturnResult(sqlmock.NewResult(0, 0))
	}

	repo := &opsRepoMock{
		ExportErrorLogsFn: func(context.Context, *OpsErrorLogCleanupFilter) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("error\n")), nil
		},
		ExportSystemLogsFn: func(context.Context, *OpsSystemLogCleanupFilter) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("system\n")), nil
		},
	}
	cfg := testOpsCleanupConfig()
	cfg.Ops.Cleanup.MaxCatchupWindowsPerRun = 1
	svc := NewOpsCleanupService(repo, nil, db, nil, cfg, nil, &opsCleanupArchiveStub{
		create: func(context.Context, DataArchiveInput) (*BackupRecord, error) {
			return &BackupRecord{Status: "completed"}, nil
		},
	})

	counts, err := svc.runCleanupOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if counts.errorLogs != 1 || counts.systemLogs != 1 {
		t.Fatalf("counts = %+v, want one archived/deleted row per log table", counts)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
