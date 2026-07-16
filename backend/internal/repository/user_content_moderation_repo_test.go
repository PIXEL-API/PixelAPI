package repository

import (
	"context"
	"os"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserContentModerationRepositoryGetConfigScansAPIKeyHash(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now()
	repo := &userContentModerationRepository{db: db}
	mock.ExpectQuery("SELECT id, owner_user_id, account_id, enabled, mode, provider, base_url, model,").
		WithArgs(int64(7), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "owner_user_id", "account_id", "enabled", "mode", "provider", "base_url", "model",
			"api_key_encrypted", "api_key_hash", "sample_rate", "block_message", "created_at", "updated_at",
		}).AddRow(
			int64(1), int64(7), int64(42), true, service.ContentModerationModeObserve,
			service.ContentModerationProviderZhipu, "https://open.bigmodel.cn", "moderation",
			"enc:key", "hash-key", 30, "blocked", now, now,
		))

	cfg, err := repo.GetConfig(context.Background(), 7, 42)

	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Equal(t, "hash-key", cfg.APIKeyHash)
	require.Equal(t, service.ContentModerationProviderZhipu, cfg.Provider)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserContentModerationRepositoryUpsertConfigPersistsAPIKeyHash(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now()
	repo := &userContentModerationRepository{db: db}
	cfg := &service.UserContentModerationConfig{
		OwnerUserID:     7,
		AccountID:       42,
		Enabled:         true,
		Mode:            service.ContentModerationModePreBlock,
		Provider:        service.ContentModerationProviderOpenAI,
		BaseURL:         "https://api.openai.com",
		Model:           "omni-moderation-latest",
		APIKeyEncrypted: "enc:key",
		APIKeyHash:      "hash-key",
		SampleRate:      50,
		BlockMessage:    "blocked",
	}
	mock.ExpectQuery("INSERT INTO user_content_moderation_configs").
		WithArgs(
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
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(int64(99), now, now))

	err = repo.UpsertConfig(context.Background(), cfg)

	require.NoError(t, err)
	require.Equal(t, int64(99), cfg.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserContentModerationRepositoryAPIKeyHashExistsExcludesCurrentConfig(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := &userContentModerationRepository{db: db}
	mock.ExpectQuery("SELECT EXISTS").
		WithArgs("hash-key", int64(7), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	exists, err := repo.APIKeyHashExists(context.Background(), " hash-key ", 7, 42)

	require.NoError(t, err)
	require.True(t, exists)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserContentModerationRepositoryCreateLogUsesSanitizedColumns(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now()
	repo := &userContentModerationRepository{db: db}
	log := &service.UserContentModerationLog{
		RequestID:       "req-1",
		OwnerUserID:     7,
		AccountID:       42,
		Endpoint:        "/v1/responses",
		Provider:        service.ContentModerationProviderOpenAI,
		Model:           "omni-moderation-latest",
		Mode:            service.ContentModerationModeObserve,
		Action:          service.ContentModerationActionAllow,
		Flagged:         true,
		HighestCategory: "violence",
		HighestScore:    0.9,
		CategoryScores:  map[string]float64{"violence": 0.9},
		Sampled:         true,
		Error:           "moderation_api_error",
	}
	mock.ExpectQuery(`(?s)INSERT INTO user_content_moderation_logs \(\s*request_id, owner_user_id, account_id, consumer_user_id, api_key_id, api_key_name,\s*group_id, endpoint, provider, model, mode, action, flagged, highest_category,\s*highest_score, category_scores, sampled, error\s*\) VALUES`).
		WithArgs(
			log.RequestID,
			log.OwnerUserID,
			log.AccountID,
			nil,
			nil,
			log.APIKeyName,
			nil,
			log.Endpoint,
			log.Provider,
			log.Model,
			log.Mode,
			log.Action,
			log.Flagged,
			log.HighestCategory,
			log.HighestScore,
			sqlmock.AnyArg(),
			log.Sampled,
			log.Error,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(101), now))

	err = repo.CreateLog(context.Background(), log)

	require.NoError(t, err)
	require.Equal(t, int64(101), log.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUserContentModerationMigrationPinsHashAndAvoidsPromptStorage(t *testing.T) {
	raw, err := os.ReadFile("../../migrations/205_user_content_moderation.sql")
	require.NoError(t, err)
	sql := string(raw)

	require.Contains(t, sql, "api_key_hash VARCHAR(128)")
	require.Contains(t, sql, "idx_user_content_moderation_configs_api_key_hash")
	require.NotContains(t, sql, "prompt")
	require.NotContains(t, sql, "input_excerpt")
	require.NotContains(t, sql, "request_body")
}
