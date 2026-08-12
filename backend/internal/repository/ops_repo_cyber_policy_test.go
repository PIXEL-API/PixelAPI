package repository

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpsCyberPolicyPredicateIsPreciseAndIncludesHTTP200(t *testing.T) {
	require.Contains(t, opsCyberPolicyPredicate, "LOWER(COALESCE(e.provider_error_code, '')) = 'cyber_policy'")
	require.Contains(t, opsCyberPolicyPredicate, "ILIKE 'cyber_policy:%'")
	require.Contains(t, opsCyberPolicyPredicate, `"code"[[:space:]]*:[[:space:]]*"cyber_policy"`)
	require.NotContains(t, strings.ToLower(opsCyberPolicyPredicate), "status_code")
	require.NotContains(t, opsCyberPolicyPredicate, "ILIKE '%cyber_policy%'")
}

func TestBuildOpsCyberPolicyRequestWhereFilters(t *testing.T) {
	start := time.Date(2026, 8, 1, 8, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	end := start.Add(24 * time.Hour)

	where, args := buildOpsCyberPolicyRequestWhere(service.CyberPolicyRequestFilter{
		StartTime:  &start,
		EndTime:    &end,
		GroupQuery: "1198",
		UserQuery:  " alice@example.com ",
		Model:      " gpt-5 ",
		Endpoint:   " /v1/responses ",
	})

	require.Contains(t, where, "e.created_at >= $1")
	require.Contains(t, where, "e.created_at < $2")
	require.Contains(t, where, "e.group_id = $3")
	require.Contains(t, where, "fu.username ILIKE $4 OR fu.email ILIKE $4")
	require.Contains(t, where, "e.requested_model ILIKE $5")
	require.Contains(t, where, "e.inbound_endpoint = $6")
	require.Equal(t, []any{
		start.UTC(), end.UTC(), int64(1198), "%alice@example.com%", "%gpt-5%", "/v1/responses",
	}, args)
}

func TestBuildOpsCyberPolicyRequestWhereSupportsGroupNameAndUserID(t *testing.T) {
	where, args := buildOpsCyberPolicyRequestWhere(service.CyberPolicyRequestFilter{
		GroupQuery: "研发一组",
		UserQuery:  "445",
	})

	require.Contains(t, where, "fg.name ILIKE $1")
	require.Contains(t, where, "e.user_id = $2")
	require.Equal(t, []any{"%研发一组%", int64(445)}, args)
}

func TestOpsInsertErrorLogArgsIncludesProviderErrorCodeAsParameter31(t *testing.T) {
	detail := "upstream detail"
	errorsJSON := `[{"code":"cyber_policy"}]`
	input := &service.OpsInsertErrorLogInput{
		UpstreamErrorDetail: &detail,
		ProviderErrorCode:   " cyber_policy ",
		UpstreamErrorsJSON:  &errorsJSON,
		CreatedAt:           time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC),
	}

	args := opsInsertErrorLogArgs(input)

	require.Len(t, args, 44)
	require.Len(t, regexp.MustCompile(`\$\d+`).FindAllString(insertOpsErrorLogSQL, -1), 44)
	require.Equal(t, sql.NullString{String: detail, Valid: true}, args[29])
	require.Equal(t, sql.NullString{String: "cyber_policy", Valid: true}, args[30])
	require.Equal(t, sql.NullString{String: errorsJSON, Valid: true}, args[31])
	require.Regexp(t, `upstream_error_detail,\s+provider_error_code,\s+upstream_errors`, insertOpsErrorLogSQL)
}

func TestOpsRepositoryListCyberPolicyRequestsIncludesHTTP200Rows(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &opsRepository{db: db}
	createdAt := time.Date(2026, 8, 11, 1, 2, 3, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM ops_error_logs e")).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`(?s)SELECT\s+e\.id,.*FROM ops_error_logs e.*provider_error_code.*ORDER BY e\.created_at DESC, e\.id DESC.*LIMIT \$1 OFFSET \$2`).
		WithArgs(20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "created_at", "request_id", "user_id", "username", "email", "group_id", "group_name",
			"api_key_id", "api_key_name", "account_id", "account_name", "requested_model", "upstream_model",
			"inbound_endpoint", "upstream_endpoint", "status_code", "upstream_status_code", "provider_error_code",
			"upstream_error_message", "request_body_preview", "request_body_truncated", "request_body_bytes",
		}).AddRow(
			9, createdAt, "req-9", 445, "alice", "alice@example.com", 1198, "研发一组",
			21, "key-a", 88, "account-a", "gpt-5", "gpt-5", "/v1/responses", "/v1/responses",
			200, 403, "cyber_policy", "cyber_policy: blocked", `{"input":"hello"}`, false, 17,
		))

	result, err := repo.ListCyberPolicyRequests(context.Background(), service.CyberPolicyRequestFilter{})

	require.NoError(t, err)
	require.Equal(t, int64(1), result.Total)
	require.Len(t, result.Items, 1)
	require.Equal(t, 200, result.Items[0].StatusCode)
	require.Equal(t, "cyber_policy", result.Items[0].ProviderErrorCode)
	require.Equal(t, "alice@example.com", result.Items[0].UserEmail)
	require.Equal(t, "研发一组", result.Items[0].GroupName)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOpsRepositoryGetCyberPolicyRequestByIDRejectsNonCyberRows(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &opsRepository{db: db}

	mock.ExpectQuery(`(?s)WHERE e\.id = \$1 AND .*provider_error_code.*cyber_policy.*LIMIT 1`).
		WithArgs(int64(77)).
		WillReturnError(sql.ErrNoRows)

	result, err := repo.GetCyberPolicyRequestByID(context.Background(), 77)

	require.Nil(t, result)
	require.ErrorIs(t, err, sql.ErrNoRows)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOpsRepositoryGetCyberPolicyRequestByIDReturnsStoredDetail(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &opsRepository{db: db}
	createdAt := time.Date(2026, 8, 11, 1, 2, 3, 0, time.UTC)

	mock.ExpectQuery(`(?s)WHERE e\.id = \$1 AND .*provider_error_code.*cyber_policy.*LIMIT 1`).
		WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "created_at", "request_id", "user_id", "username", "email", "group_id", "group_name",
			"api_key_id", "api_key_name", "account_id", "account_name", "requested_model", "upstream_model",
			"inbound_endpoint", "upstream_endpoint", "status_code", "upstream_status_code", "provider_error_code",
			"upstream_error_message", "request_body_preview", "request_body_truncated", "request_body_bytes",
			"request_body", "upstream_error_detail", "upstream_errors",
		}).AddRow(
			9, createdAt, "req-9", 445, "alice", "alice@example.com", 1198, "研发一组",
			21, "key-a", 88, "account-a", "gpt-5", "gpt-5", "/v1/responses", "/v1/responses",
			200, 403, "cyber_policy", "cyber_policy: blocked", `{"input":"hello"}`, true, 300000,
			`{"input":"hello"}`, `{"code":"cyber_policy"}`, `[{"status":403}]`,
		))

	detail, err := repo.GetCyberPolicyRequestByID(context.Background(), 9)

	require.NoError(t, err)
	require.Equal(t, int64(9), detail.ID)
	require.Equal(t, `{"input":"hello"}`, detail.RequestContent)
	require.Equal(t, `{"code":"cyber_policy"}`, detail.UpstreamErrorDetail)
	require.Equal(t, `[{"status":403}]`, detail.UpstreamErrors)
	require.True(t, detail.RequestContentTruncated)
	require.NotNil(t, detail.RequestContentBytes)
	require.Equal(t, 300000, *detail.RequestContentBytes)
	require.NoError(t, mock.ExpectationsWereMet())
}
