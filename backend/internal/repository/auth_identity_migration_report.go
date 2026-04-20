package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type AuthIdentityMigrationReport struct {
	ID         int64
	ReportType string
	ReportKey  string
	Details    map[string]any
	CreatedAt  time.Time
}

type AuthIdentityMigrationReportQuery struct {
	ReportType string
	Limit      int
	Offset     int
}

type AuthIdentityMigrationReportSummary struct {
	Total  int64
	ByType map[string]int64
}

func (r *userRepository) ListAuthIdentityMigrationReports(ctx context.Context, query AuthIdentityMigrationReportQuery) ([]AuthIdentityMigrationReport, error) {
	exec := txAwareSQLExecutor(ctx, r.sql, r.client)
	if exec == nil {
		return nil, fmt.Errorf("sql executor is not configured")
	}

	limit := query.Limit
	if limit <= 0 {
		limit = 100
	}
	rows, err := exec.QueryContext(ctx, `
SELECT id, report_type, report_key, details, created_at
FROM auth_identity_migration_reports
WHERE ($1 = '' OR report_type = $1)
ORDER BY created_at DESC, id DESC
LIMIT $2 OFFSET $3`,
		strings.TrimSpace(query.ReportType),
		limit,
		query.Offset,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	reports := make([]AuthIdentityMigrationReport, 0)
	for rows.Next() {
		report, scanErr := scanAuthIdentityMigrationReport(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		reports = append(reports, report)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return reports, nil
}

func (r *userRepository) GetAuthIdentityMigrationReport(ctx context.Context, reportType, reportKey string) (*AuthIdentityMigrationReport, error) {
	exec := txAwareSQLExecutor(ctx, r.sql, r.client)
	if exec == nil {
		return nil, fmt.Errorf("sql executor is not configured")
	}

	rows, err := exec.QueryContext(ctx, `
SELECT id, report_type, report_key, details, created_at
FROM auth_identity_migration_reports
WHERE report_type = $1 AND report_key = $2
LIMIT 1`,
		strings.TrimSpace(reportType),
		strings.TrimSpace(reportKey),
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	report, err := scanAuthIdentityMigrationReport(rows)
	if err != nil {
		return nil, err
	}
	return &report, rows.Err()
}

func (r *userRepository) SummarizeAuthIdentityMigrationReports(ctx context.Context) (*AuthIdentityMigrationReportSummary, error) {
	exec := txAwareSQLExecutor(ctx, r.sql, r.client)
	if exec == nil {
		return nil, fmt.Errorf("sql executor is not configured")
	}

	rows, err := exec.QueryContext(ctx, `
SELECT report_type, COUNT(*)
FROM auth_identity_migration_reports
GROUP BY report_type
ORDER BY report_type ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	summary := &AuthIdentityMigrationReportSummary{
		ByType: make(map[string]int64),
	}
	for rows.Next() {
		var reportType string
		var count int64
		if err := rows.Scan(&reportType, &count); err != nil {
			return nil, err
		}
		summary.ByType[reportType] = count
		summary.Total += count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return summary, nil
}

func scanAuthIdentityMigrationReport(scanner interface{ Scan(dest ...any) error }) (AuthIdentityMigrationReport, error) {
	var (
		report  AuthIdentityMigrationReport
		details []byte
	)
	if err := scanner.Scan(&report.ID, &report.ReportType, &report.ReportKey, &details, &report.CreatedAt); err != nil {
		return AuthIdentityMigrationReport{}, err
	}
	report.Details = map[string]any{}
	if len(details) > 0 {
		if err := json.Unmarshal(details, &report.Details); err != nil {
			return AuthIdentityMigrationReport{}, err
		}
	}
	return report, nil
}
