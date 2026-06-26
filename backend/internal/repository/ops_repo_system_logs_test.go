package repository

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestBuildOpsSystemLogsWhere_WithClientRequestIDAndUserID(t *testing.T) {
	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)
	userID := int64(12)
	accountID := int64(34)

	filter := &service.OpsSystemLogFilter{
		StartTime:       &start,
		EndTime:         &end,
		Level:           "warn",
		Component:       "http.access",
		RequestID:       "req-1",
		ClientRequestID: "creq-1",
		UserID:          &userID,
		AccountID:       &accountID,
		Platform:        "openai",
		Model:           "gpt-5",
		Query:           "timeout",
	}

	where, args, hasConstraint := buildOpsSystemLogsWhere(filter)
	if !hasConstraint {
		t.Fatalf("expected hasConstraint=true")
	}
	if where == "" {
		t.Fatalf("where should not be empty")
	}
	if len(args) != 11 {
		t.Fatalf("args len = %d, want 11", len(args))
	}
	if !contains(where, "COALESCE(l.client_request_id,'') = $") {
		t.Fatalf("where should include client_request_id condition: %s", where)
	}
	if !contains(where, "l.user_id = $") {
		t.Fatalf("where should include user_id condition: %s", where)
	}
}

func TestBuildOpsSystemLogsCleanupWhere_RequireConstraint(t *testing.T) {
	where, args, hasConstraint := buildOpsSystemLogsCleanupWhere(&service.OpsSystemLogCleanupFilter{})
	if hasConstraint {
		t.Fatalf("expected hasConstraint=false")
	}
	if where == "" {
		t.Fatalf("where should not be empty")
	}
	if len(args) != 0 {
		t.Fatalf("args len = %d, want 0", len(args))
	}
}

func TestBuildOpsSystemLogsCleanupWhere_WithClientRequestIDAndUserID(t *testing.T) {
	userID := int64(9)
	filter := &service.OpsSystemLogCleanupFilter{
		ClientRequestID: "creq-9",
		UserID:          &userID,
	}

	where, args, hasConstraint := buildOpsSystemLogsCleanupWhere(filter)
	if !hasConstraint {
		t.Fatalf("expected hasConstraint=true")
	}
	if len(args) != 2 {
		t.Fatalf("args len = %d, want 2", len(args))
	}
	if !contains(where, "COALESCE(l.client_request_id,'') = $") {
		t.Fatalf("where should include client_request_id condition: %s", where)
	}
	if !contains(where, "l.user_id = $") {
		t.Fatalf("where should include user_id condition: %s", where)
	}
}

func TestOpsRepositoryExportErrorLogs(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &opsRepository{db: db}
	end := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery("SELECT to_jsonb\\(e\\)::text").
		WithArgs(end).
		WillReturnRows(sqlmock.NewRows([]string{"to_jsonb"}).
			AddRow(`{"id":1,"error_type":"timeout"}`).
			AddRow(`{"id":2,"error_type":"upstream"}`))

	reader, err := repo.ExportErrorLogs(context.Background(), &service.OpsErrorLogCleanupFilter{EndTime: &end})
	if err != nil {
		t.Fatalf("ExportErrorLogs() error = %v", err)
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	want := "{\"id\":1,\"error_type\":\"timeout\"}\n{\"id\":2,\"error_type\":\"upstream\"}\n"
	if string(data) != want {
		t.Fatalf("data = %q, want %q", string(data), want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestOpsRepositoryExportSystemLogs(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &opsRepository{db: db}
	end := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery("SELECT to_jsonb\\(l\\)::text").
		WithArgs(end).
		WillReturnRows(sqlmock.NewRows([]string{"to_jsonb"}).
			AddRow(`{"id":1,"level":"info"}`).
			AddRow(`{"id":2,"level":"warn"}`))

	reader, err := repo.ExportSystemLogs(context.Background(), &service.OpsSystemLogCleanupFilter{EndTime: &end})
	if err != nil {
		t.Fatalf("ExportSystemLogs() error = %v", err)
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	want := "{\"id\":1,\"level\":\"info\"}\n{\"id\":2,\"level\":\"warn\"}\n"
	if string(data) != want {
		t.Fatalf("data = %q, want %q", string(data), want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func contains(s string, sub string) bool {
	return strings.Contains(s, sub)
}
