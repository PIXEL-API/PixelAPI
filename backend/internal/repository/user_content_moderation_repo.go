package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type userContentModerationRepository struct {
	db *sql.DB
}

func NewUserContentModerationRepository(db *sql.DB) service.UserContentModerationRepository {
	return &userContentModerationRepository{db: db}
}

func (r *userContentModerationRepository) GetConfig(ctx context.Context, ownerUserID, accountID int64) (*service.UserContentModerationConfig, error) {
	if ownerUserID <= 0 || accountID <= 0 {
		return nil, nil
	}
	var cfg service.UserContentModerationConfig
	err := r.db.QueryRowContext(ctx, `
SELECT id, owner_user_id, account_id, enabled, mode, provider, base_url, model,
       api_key_encrypted, api_key_hash, sample_rate, block_message, created_at, updated_at
FROM user_content_moderation_configs
WHERE owner_user_id = $1 AND account_id = $2
`, ownerUserID, accountID).Scan(
		&cfg.ID,
		&cfg.OwnerUserID,
		&cfg.AccountID,
		&cfg.Enabled,
		&cfg.Mode,
		&cfg.Provider,
		&cfg.BaseURL,
		&cfg.Model,
		&cfg.APIKeyEncrypted,
		&cfg.APIKeyHash,
		&cfg.SampleRate,
		&cfg.BlockMessage,
		&cfg.CreatedAt,
		&cfg.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get user content moderation config: %w", err)
	}
	return &cfg, nil
}

func (r *userContentModerationRepository) UpsertConfig(ctx context.Context, cfg *service.UserContentModerationConfig) error {
	if cfg == nil {
		return nil
	}
	err := r.db.QueryRowContext(ctx, `
INSERT INTO user_content_moderation_configs (
    owner_user_id, account_id, enabled, mode, provider, base_url, model,
    api_key_encrypted, api_key_hash, sample_rate, block_message
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
ON CONFLICT (owner_user_id, account_id) DO UPDATE SET
    enabled = EXCLUDED.enabled,
    mode = EXCLUDED.mode,
    provider = EXCLUDED.provider,
    base_url = EXCLUDED.base_url,
    model = EXCLUDED.model,
    api_key_encrypted = EXCLUDED.api_key_encrypted,
    api_key_hash = EXCLUDED.api_key_hash,
    sample_rate = EXCLUDED.sample_rate,
    block_message = EXCLUDED.block_message,
    updated_at = NOW()
RETURNING id, created_at, updated_at
`,
		cfg.OwnerUserID,
		cfg.AccountID,
		cfg.Enabled,
		cfg.Mode,
		cfg.Provider,
		cfg.BaseURL,
		cfg.Model,
		cfg.APIKeyEncrypted,
		cfg.APIKeyHash,
		cfg.SampleRate,
		cfg.BlockMessage,
	).Scan(&cfg.ID, &cfg.CreatedAt, &cfg.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert user content moderation config: %w", err)
	}
	return nil
}

func (r *userContentModerationRepository) APIKeyHashExists(ctx context.Context, apiKeyHash string, excludeOwnerUserID, excludeAccountID int64) (bool, error) {
	apiKeyHash = strings.TrimSpace(apiKeyHash)
	if apiKeyHash == "" {
		return false, nil
	}
	var exists bool
	err := r.db.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM user_content_moderation_configs
    WHERE api_key_hash = $1
      AND NOT (owner_user_id = $2 AND account_id = $3)
)
`, apiKeyHash, excludeOwnerUserID, excludeAccountID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check user content moderation api key hash: %w", err)
	}
	return exists, nil
}

func (r *userContentModerationRepository) CreateLog(ctx context.Context, log *service.UserContentModerationLog) error {
	if log == nil {
		return nil
	}
	categoryScores, err := json.Marshal(log.CategoryScores)
	if err != nil {
		return fmt.Errorf("marshal user moderation category scores: %w", err)
	}
	err = r.db.QueryRowContext(ctx, `
INSERT INTO user_content_moderation_logs (
    request_id, owner_user_id, account_id, consumer_user_id, api_key_id, api_key_name,
    group_id, endpoint, provider, model, mode, action, flagged, highest_category,
    highest_score, category_scores, sampled, error
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11, $12, $13, $14,
    $15, $16::jsonb, $17, $18
) RETURNING id, created_at
`,
		log.RequestID,
		log.OwnerUserID,
		log.AccountID,
		nullableInt64Ptr(log.ConsumerUserID),
		nullableInt64Ptr(log.APIKeyID),
		log.APIKeyName,
		nullableInt64Ptr(log.GroupID),
		log.Endpoint,
		log.Provider,
		log.Model,
		log.Mode,
		log.Action,
		log.Flagged,
		log.HighestCategory,
		log.HighestScore,
		string(categoryScores),
		log.Sampled,
		log.Error,
	).Scan(&log.ID, &log.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert user content moderation log: %w", err)
	}
	return nil
}

func (r *userContentModerationRepository) ListLogs(ctx context.Context, filter service.UserContentModerationLogFilter) ([]service.UserContentModerationLog, *pagination.PaginationResult, error) {
	where, args := buildUserContentModerationLogWhere(filter)
	whereSQL := "WHERE " + strings.Join(where, " AND ")

	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_content_moderation_logs l "+whereSQL, args...).Scan(&total); err != nil {
		return nil, nil, fmt.Errorf("count user content moderation logs: %w", err)
	}

	params := filter.Pagination
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 20
	}
	if params.PageSize > 100 {
		params.PageSize = 100
	}
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, params.Limit(), params.Offset())
	rows, err := r.db.QueryContext(ctx, `
SELECT
    l.id, l.request_id, l.owner_user_id, l.account_id, l.consumer_user_id,
    l.api_key_id, l.api_key_name, l.group_id, l.endpoint, l.provider, l.model,
    l.mode, l.action, l.flagged, l.highest_category, l.highest_score,
    l.category_scores, l.sampled, l.error, l.created_at
FROM user_content_moderation_logs l `+whereSQL+`
ORDER BY l.created_at DESC, l.id DESC
LIMIT $`+fmt.Sprint(len(queryArgs)-1)+` OFFSET $`+fmt.Sprint(len(queryArgs)),
		queryArgs...,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("list user content moderation logs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.UserContentModerationLog, 0)
	for rows.Next() {
		var item service.UserContentModerationLog
		var consumerUserID, apiKeyID, groupID sql.NullInt64
		var scoresRaw []byte
		if err := rows.Scan(
			&item.ID,
			&item.RequestID,
			&item.OwnerUserID,
			&item.AccountID,
			&consumerUserID,
			&apiKeyID,
			&item.APIKeyName,
			&groupID,
			&item.Endpoint,
			&item.Provider,
			&item.Model,
			&item.Mode,
			&item.Action,
			&item.Flagged,
			&item.HighestCategory,
			&item.HighestScore,
			&scoresRaw,
			&item.Sampled,
			&item.Error,
			&item.CreatedAt,
		); err != nil {
			return nil, nil, fmt.Errorf("scan user content moderation log: %w", err)
		}
		if consumerUserID.Valid {
			v := consumerUserID.Int64
			item.ConsumerUserID = &v
		}
		if apiKeyID.Valid {
			v := apiKeyID.Int64
			item.APIKeyID = &v
		}
		if groupID.Valid {
			v := groupID.Int64
			item.GroupID = &v
		}
		item.CategoryScores = map[string]float64{}
		_ = json.Unmarshal(scoresRaw, &item.CategoryScores)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate user content moderation logs: %w", err)
	}
	return items, paginationResultFromTotal(total, params), nil
}

func buildUserContentModerationLogWhere(filter service.UserContentModerationLogFilter) ([]string, []any) {
	where := []string{"l.owner_user_id = $1", "l.account_id = $2"}
	args := []any{filter.OwnerUserID, filter.AccountID}
	switch strings.ToLower(strings.TrimSpace(filter.Result)) {
	case "hit", "flagged":
		where = append(where, "l.flagged = TRUE")
	case "blocked", "block":
		where = append(where, "l.action = 'block'")
	case "pass", "allow":
		where = append(where, "l.flagged = FALSE AND l.error = ''")
	case "error":
		where = append(where, "l.error <> ''")
	}
	return where, args
}
