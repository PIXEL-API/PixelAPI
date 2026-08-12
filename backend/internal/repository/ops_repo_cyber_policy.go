package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const opsCyberPolicyPredicate = `(
  LOWER(COALESCE(e.provider_error_code, '')) = 'cyber_policy'
  OR COALESCE(e.upstream_error_message, '') ILIKE 'cyber_policy:%'
  OR COALESCE(e.upstream_error_detail, '') ~* '"code"[[:space:]]*:[[:space:]]*"cyber_policy"'
)`

const opsCyberPolicyRequestSelect = `
SELECT
  e.id,
  e.created_at,
  COALESCE(e.request_id, ''),
  e.user_id,
  COALESCE(u.username, ''),
  COALESCE(u.email, ''),
  e.group_id,
  COALESCE(g.name, ''),
  e.api_key_id,
  COALESCE(k.name, ''),
  e.account_id,
  COALESCE(a.name, ''),
  COALESCE(e.requested_model, e.model, ''),
  COALESCE(e.upstream_model, ''),
  COALESCE(e.inbound_endpoint, e.request_path, ''),
  COALESCE(e.upstream_endpoint, ''),
  COALESCE(e.status_code, 0),
  e.upstream_status_code,
  COALESCE(e.provider_error_code, ''),
  COALESCE(e.upstream_error_message, ''),
  LEFT(COALESCE(e.request_body::text, ''), 320),
  COALESCE(e.request_body_truncated, false),
  e.request_body_bytes`

const opsCyberPolicyRequestFrom = `
FROM ops_error_logs e
LEFT JOIN users u ON u.id = e.user_id
LEFT JOIN groups g ON g.id = e.group_id
LEFT JOIN api_keys k ON k.id = e.api_key_id
LEFT JOIN accounts a ON a.id = e.account_id`

type opsCyberPolicyScanner interface {
	Scan(dest ...any) error
}

func (r *opsRepository) ListCyberPolicyRequests(ctx context.Context, filter service.CyberPolicyRequestFilter) (*service.CyberPolicyRequestList, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil ops repository")
	}
	where, args := buildOpsCyberPolicyRequestWhere(filter)
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM ops_error_logs e "+where, args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("count cyber policy requests: %w", err)
	}

	page := filter.Page
	if page <= 0 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, pageSize, (page-1)*pageSize)
	query := opsCyberPolicyRequestSelect + opsCyberPolicyRequestFrom + "\n" + where +
		"\nORDER BY e.created_at DESC, e.id DESC" +
		"\nLIMIT $" + itoa(len(args)+1) + " OFFSET $" + itoa(len(args)+2)
	rows, err := r.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("list cyber policy requests: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]*service.CyberPolicyRequest, 0, pageSize)
	for rows.Next() {
		item, err := scanOpsCyberPolicyRequest(rows)
		if err != nil {
			return nil, fmt.Errorf("scan cyber policy request: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cyber policy requests: %w", err)
	}
	return &service.CyberPolicyRequestList{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func (r *opsRepository) GetCyberPolicyRequestByID(ctx context.Context, id int64) (*service.CyberPolicyRequestDetail, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil ops repository")
	}
	if id <= 0 {
		return nil, sql.ErrNoRows
	}
	query := opsCyberPolicyRequestSelect + `,
  COALESCE(e.request_body::text, ''),
  COALESCE(e.upstream_error_detail, ''),
  COALESCE(e.upstream_errors::text, '')` + opsCyberPolicyRequestFrom +
		"\nWHERE e.id = $1 AND " + opsCyberPolicyPredicate + "\nLIMIT 1"
	return scanOpsCyberPolicyRequestDetail(r.db.QueryRowContext(ctx, query, id))
}

func (r *opsRepository) ListCyberPolicyRequestsForExport(ctx context.Context, filter service.CyberPolicyRequestFilter, limit int) ([]*service.CyberPolicyRequestDetail, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("nil ops repository")
	}
	if limit <= 0 {
		return []*service.CyberPolicyRequestDetail{}, nil
	}
	where, args := buildOpsCyberPolicyRequestWhere(filter)
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, limit)
	query := opsCyberPolicyRequestSelect + `,
  COALESCE(e.request_body::text, ''),
  COALESCE(e.upstream_error_detail, ''),
  COALESCE(e.upstream_errors::text, '')` + opsCyberPolicyRequestFrom + "\n" + where +
		"\nORDER BY e.created_at DESC, e.id DESC" +
		"\nLIMIT $" + itoa(len(args)+1)
	rows, err := r.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("export cyber policy requests: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]*service.CyberPolicyRequestDetail, 0, limit)
	for rows.Next() {
		item, err := scanOpsCyberPolicyRequestDetail(rows)
		if err != nil {
			return nil, fmt.Errorf("scan cyber policy request export: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cyber policy request export: %w", err)
	}
	return items, nil
}

func buildOpsCyberPolicyRequestWhere(filter service.CyberPolicyRequestFilter) (string, []any) {
	clauses := []string{opsCyberPolicyPredicate}
	args := make([]any, 0, 8)
	add := func(expression string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(expression, len(args)))
	}
	if filter.StartTime != nil && !filter.StartTime.IsZero() {
		add("e.created_at >= $%d", filter.StartTime.UTC())
	}
	if filter.EndTime != nil && !filter.EndTime.IsZero() {
		add("e.created_at < $%d", filter.EndTime.UTC())
	}
	if query := strings.TrimSpace(filter.GroupQuery); query != "" {
		if id, err := strconv.ParseInt(query, 10, 64); err == nil && id > 0 {
			add("e.group_id = $%d", id)
		} else {
			add("EXISTS (SELECT 1 FROM groups fg WHERE fg.id = e.group_id AND fg.name ILIKE $%d)", "%"+query+"%")
		}
	}
	if query := strings.TrimSpace(filter.UserQuery); query != "" {
		if id, err := strconv.ParseInt(query, 10, 64); err == nil && id > 0 {
			add("e.user_id = $%d", id)
		} else {
			add("EXISTS (SELECT 1 FROM users fu WHERE fu.id = e.user_id AND (fu.username ILIKE $%[1]d OR fu.email ILIKE $%[1]d))", "%"+query+"%")
		}
	}
	if model := strings.TrimSpace(filter.Model); model != "" {
		add("(e.requested_model ILIKE $%[1]d OR e.upstream_model ILIKE $%[1]d OR e.model ILIKE $%[1]d)", "%"+model+"%")
	}
	if endpoint := strings.TrimSpace(filter.Endpoint); endpoint != "" {
		add("(e.inbound_endpoint = $%[1]d OR e.upstream_endpoint = $%[1]d OR e.request_path = $%[1]d)", endpoint)
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func scanOpsCyberPolicyRequest(scanner opsCyberPolicyScanner) (*service.CyberPolicyRequest, error) {
	item, _, _, _, err := scanOpsCyberPolicyRequestFields(scanner, false)
	return item, err
}

func scanOpsCyberPolicyRequestDetail(scanner opsCyberPolicyScanner) (*service.CyberPolicyRequestDetail, error) {
	item, content, detail, upstreamErrors, err := scanOpsCyberPolicyRequestFields(scanner, true)
	if err != nil {
		return nil, err
	}
	return &service.CyberPolicyRequestDetail{
		CyberPolicyRequest:  *item,
		RequestContent:      normalizeOpsCyberPolicyJSON(content),
		UpstreamErrorDetail: strings.TrimSpace(detail),
		UpstreamErrors:      normalizeOpsCyberPolicyJSON(upstreamErrors),
	}, nil
}

func scanOpsCyberPolicyRequestFields(scanner opsCyberPolicyScanner, includeDetail bool) (*service.CyberPolicyRequest, string, string, string, error) {
	var item service.CyberPolicyRequest
	var userID, groupID, apiKeyID, accountID, upstreamStatus, requestBytes sql.NullInt64
	var content, detail, upstreamErrors string
	dest := []any{
		&item.ID, &item.CreatedAt, &item.RequestID,
		&userID, &item.UserName, &item.UserEmail,
		&groupID, &item.GroupName,
		&apiKeyID, &item.APIKeyName,
		&accountID, &item.AccountName,
		&item.RequestedModel, &item.UpstreamModel,
		&item.InboundEndpoint, &item.UpstreamEndpoint,
		&item.StatusCode, &upstreamStatus,
		&item.ProviderErrorCode, &item.UpstreamErrorMessage,
		&item.RequestContentPreview, &item.RequestContentTruncated, &requestBytes,
	}
	if includeDetail {
		dest = append(dest, &content, &detail, &upstreamErrors)
	}
	if err := scanner.Scan(dest...); err != nil {
		return nil, "", "", "", err
	}
	if userID.Valid {
		value := userID.Int64
		item.UserID = &value
	}
	if groupID.Valid {
		value := groupID.Int64
		item.GroupID = &value
	}
	if apiKeyID.Valid {
		value := apiKeyID.Int64
		item.APIKeyID = &value
	}
	if accountID.Valid {
		value := accountID.Int64
		item.AccountID = &value
	}
	if upstreamStatus.Valid && upstreamStatus.Int64 > 0 {
		value := int(upstreamStatus.Int64)
		item.UpstreamStatusCode = &value
	}
	if requestBytes.Valid {
		value := int(requestBytes.Int64)
		item.RequestContentBytes = &value
	}
	item.RequestContentPreview = normalizeOpsCyberPolicyJSON(item.RequestContentPreview)
	return &item, content, detail, upstreamErrors, nil
}

func normalizeOpsCyberPolicyJSON(value string) string {
	value = strings.TrimSpace(value)
	if value == "null" {
		return ""
	}
	return value
}
