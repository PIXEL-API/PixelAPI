package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestNormalizeAndValidateUserModerationConfigPinsProviderDefaults(t *testing.T) {
	cases := []struct {
		name         string
		provider     string
		wantProvider string
		wantBaseURL  string
		wantModel    string
	}{
		{
			name:         "openai",
			provider:     "openai",
			wantProvider: ContentModerationProviderOpenAI,
			wantBaseURL:  defaultContentModerationBaseURL,
			wantModel:    defaultContentModerationModel,
		},
		{
			name:         "zhipu",
			provider:     " ZhIpU ",
			wantProvider: ContentModerationProviderZhipu,
			wantBaseURL:  defaultZhipuContentModerationBaseURL,
			wantModel:    defaultZhipuContentModerationModel,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &UserContentModerationConfig{
				OwnerUserID:  7,
				AccountID:    42,
				Mode:         ContentModerationModeObserve,
				Provider:     tc.provider,
				BaseURL:      "https://attacker.example",
				Model:        "user-supplied-model",
				SampleRate:   50,
				BlockMessage: "blocked",
			}

			require.NoError(t, normalizeAndValidateUserModerationConfig(cfg))
			require.Equal(t, tc.wantProvider, cfg.Provider)
			require.Equal(t, tc.wantBaseURL, cfg.BaseURL)
			require.Equal(t, tc.wantModel, cfg.Model)
		})
	}
}

func TestNormalizeAndValidateUserModerationConfigRejectsUnsupportedProvider(t *testing.T) {
	cfg := &UserContentModerationConfig{
		OwnerUserID:  7,
		AccountID:    42,
		Mode:         ContentModerationModeObserve,
		Provider:     "custom",
		SampleRate:   100,
		BlockMessage: "blocked",
	}

	err := normalizeAndValidateUserModerationConfig(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "INVALID_USER_CONTENT_MODERATION_PROVIDER")
}

func TestUserModerationShouldSampleUsesDeterministicHashBoundary(t *testing.T) {
	require.True(t, userModerationShouldSample(1, "0000"))
	require.False(t, userModerationShouldSample(1, "0063"))
	require.True(t, userModerationShouldSample(100, "0063"))
}

func TestUserContentModerationUpdateConfigStoresPinnedProviderDefaultsAndKeyHash(t *testing.T) {
	ownerUserID := int64(7)
	accountID := int64(42)
	repo := &userContentModerationRepoStub{}
	svc := newUserContentModerationServiceForTest(t, repo, ownerUserID, accountID, nil)
	enabled := true
	provider := ContentModerationProviderZhipu
	sampleRate := 25
	apiKey := " user-key "

	cfg, err := svc.UpdateConfig(context.Background(), ownerUserID, accountID, UpdateUserContentModerationConfigInput{
		Enabled:    &enabled,
		Provider:   &provider,
		APIKey:     &apiKey,
		SampleRate: &sampleRate,
	})

	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.NotNil(t, repo.upserted)
	require.True(t, repo.upserted.Enabled)
	require.Equal(t, ContentModerationProviderZhipu, repo.upserted.Provider)
	require.Equal(t, defaultZhipuContentModerationBaseURL, repo.upserted.BaseURL)
	require.Equal(t, defaultZhipuContentModerationModel, repo.upserted.Model)
	require.Equal(t, 25, repo.upserted.SampleRate)
	require.Equal(t, "enc:user-key", repo.upserted.APIKeyEncrypted)
	require.Equal(t, moderationAPIKeyHash("user-key"), repo.upserted.APIKeyHash)
	require.Equal(t, moderationAPIKeyHash("user-key"), repo.apiKeyHashQueries[0])
	require.True(t, cfg.APIKeyConfigured)
	require.Equal(t, maskSecretTail("user-key"), cfg.APIKeyMasked)
}

func TestUserContentModerationUpdateConfigRejectsAdminDuplicateAPIKey(t *testing.T) {
	ownerUserID := int64(7)
	accountID := int64(42)
	adminCfg := defaultContentModerationConfig()
	adminCfg.APIKeys = []string{"shared-key"}
	repo := &userContentModerationRepoStub{}
	svc := newUserContentModerationServiceForTest(t, repo, ownerUserID, accountID, adminCfg)
	apiKey := "shared-key"

	_, err := svc.UpdateConfig(context.Background(), ownerUserID, accountID, UpdateUserContentModerationConfigInput{
		APIKey: &apiKey,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "USER_CONTENT_MODERATION_API_KEY_DUPLICATED")
	require.Nil(t, repo.upserted)
}

func TestUserContentModerationUpdateConfigRejectsUserDuplicateAPIKey(t *testing.T) {
	ownerUserID := int64(7)
	accountID := int64(42)
	repo := &userContentModerationRepoStub{apiKeyHashExists: true}
	svc := newUserContentModerationServiceForTest(t, repo, ownerUserID, accountID, nil)
	apiKey := "shared-user-key"

	_, err := svc.UpdateConfig(context.Background(), ownerUserID, accountID, UpdateUserContentModerationConfigInput{
		APIKey: &apiKey,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "USER_CONTENT_MODERATION_API_KEY_DUPLICATED")
	require.Nil(t, repo.upserted)
}

func TestUserContentModerationUpdateConfigClearAPIKeyClearsHash(t *testing.T) {
	ownerUserID := int64(7)
	accountID := int64(42)
	repo := &userContentModerationRepoStub{
		cfg: &UserContentModerationConfig{
			OwnerUserID:     ownerUserID,
			AccountID:       accountID,
			Enabled:         false,
			Mode:            ContentModerationModeObserve,
			Provider:        ContentModerationProviderOpenAI,
			BaseURL:         defaultContentModerationBaseURL,
			Model:           defaultContentModerationModel,
			APIKeyEncrypted: "enc:old-key",
			APIKeyHash:      moderationAPIKeyHash("old-key"),
			SampleRate:      100,
			BlockMessage:    defaultContentModerationBlockMessage,
		},
	}
	svc := newUserContentModerationServiceForTest(t, repo, ownerUserID, accountID, nil)

	cfg, err := svc.UpdateConfig(context.Background(), ownerUserID, accountID, UpdateUserContentModerationConfigInput{
		ClearAPIKey: true,
	})

	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.NotNil(t, repo.upserted)
	require.Empty(t, repo.upserted.APIKeyEncrypted)
	require.Empty(t, repo.upserted.APIKeyHash)
	require.False(t, cfg.APIKeyConfigured)
	require.Empty(t, cfg.APIKeyMasked)
}

func TestUserContentModerationLogJSONDoesNotExposePromptFields(t *testing.T) {
	raw, err := json.Marshal(UserContentModerationLog{
		RequestID:       "req-1",
		OwnerUserID:     7,
		AccountID:       42,
		Endpoint:        "/v1/responses",
		Provider:        ContentModerationProviderOpenAI,
		Model:           defaultContentModerationModel,
		Mode:            ContentModerationModeObserve,
		Action:          ContentModerationActionAllow,
		HighestCategory: "violence",
		HighestScore:    0.9,
		CategoryScores:  map[string]float64{"violence": 0.9},
		Sampled:         true,
	})
	require.NoError(t, err)
	body := string(raw)
	require.NotContains(t, body, "prompt")
	require.NotContains(t, body, "input_excerpt")
	require.NotContains(t, body, "body")
	require.NotContains(t, body, "content")
}

func TestContentModerationUpdateConfigRejectsUserModerationDuplicateAPIKey(t *testing.T) {
	settingRepo := &contentModerationSettingRepoStub{}
	checker := &contentModerationUserAPIKeyHashCheckerStub{exists: true}
	svc := NewContentModerationService(settingRepo, nil, nil, nil, nil, nil, nil)
	svc.SetUserAPIKeyHashChecker(checker)
	keys := []string{"shared-key"}

	_, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		APIKeys: &keys,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "CONTENT_MODERATION_API_KEY_DUPLICATED")
	require.Equal(t, []string{moderationAPIKeyHash("shared-key")}, checker.queries)
	require.False(t, settingRepo.setCalled)
}

func newUserContentModerationServiceForTest(t *testing.T, repo *userContentModerationRepoStub, ownerUserID, accountID int64, adminCfg *ContentModerationConfig) *UserContentModerationService {
	t.Helper()
	accountRepo := &userContentModerationAccountRepoStub{
		account: &Account{ID: accountID, OwnerUserID: &ownerUserID},
	}
	accountSvc := NewAccountService(accountRepo, nil, nil, nil, nil)
	settingRepo := &contentModerationSettingRepoStub{}
	if adminCfg != nil {
		raw, err := json.Marshal(adminCfg)
		require.NoError(t, err)
		settingRepo.values = map[string]string{SettingKeyContentModerationConfig: string(raw)}
	}
	moderationSvc := NewContentModerationService(settingRepo, nil, nil, nil, nil, nil, nil)
	return NewUserContentModerationService(repo, accountSvc, userContentModerationEncryptorStub{}, moderationSvc)
}

type userContentModerationRepoStub struct {
	cfg               *UserContentModerationConfig
	upserted          *UserContentModerationConfig
	logs              []*UserContentModerationLog
	apiKeyHashExists  bool
	apiKeyHashQueries []string
}

func (r *userContentModerationRepoStub) GetConfig(context.Context, int64, int64) (*UserContentModerationConfig, error) {
	if r.cfg == nil {
		return nil, nil
	}
	cfg := *r.cfg
	return &cfg, nil
}

func (r *userContentModerationRepoStub) UpsertConfig(_ context.Context, cfg *UserContentModerationConfig) error {
	if cfg != nil {
		copied := *cfg
		r.upserted = &copied
	}
	return nil
}

func (r *userContentModerationRepoStub) CreateLog(_ context.Context, log *UserContentModerationLog) error {
	if log != nil {
		copied := *log
		r.logs = append(r.logs, &copied)
	}
	return nil
}

func (r *userContentModerationRepoStub) ListLogs(context.Context, UserContentModerationLogFilter) ([]UserContentModerationLog, *pagination.PaginationResult, error) {
	return nil, nil, nil
}

func (r *userContentModerationRepoStub) APIKeyHashExists(_ context.Context, apiKeyHash string, _, _ int64) (bool, error) {
	r.apiKeyHashQueries = append(r.apiKeyHashQueries, strings.TrimSpace(apiKeyHash))
	return r.apiKeyHashExists, nil
}

type userContentModerationAccountRepoStub struct {
	AccountRepository
	account *Account
	err     error
}

func (r *userContentModerationAccountRepoStub) GetByID(context.Context, int64) (*Account, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.account, nil
}

type userContentModerationEncryptorStub struct{}

func (userContentModerationEncryptorStub) Encrypt(plaintext string) (string, error) {
	return "enc:" + plaintext, nil
}

func (userContentModerationEncryptorStub) Decrypt(ciphertext string) (string, error) {
	return strings.TrimPrefix(ciphertext, "enc:"), nil
}

type contentModerationSettingRepoStub struct {
	SettingRepository
	values    map[string]string
	setCalled bool
}

func (r *contentModerationSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if r.values != nil {
		if value, ok := r.values[key]; ok {
			return value, nil
		}
	}
	return "", ErrSettingNotFound
}

func (r *contentModerationSettingRepoStub) Set(_ context.Context, key, value string) error {
	if r.values == nil {
		r.values = map[string]string{}
	}
	r.setCalled = true
	r.values[key] = value
	return nil
}

type contentModerationUserAPIKeyHashCheckerStub struct {
	exists  bool
	err     error
	queries []string
}

func (c *contentModerationUserAPIKeyHashCheckerStub) APIKeyHashExists(_ context.Context, apiKeyHash string, _, _ int64) (bool, error) {
	c.queries = append(c.queries, strings.TrimSpace(apiKeyHash))
	if c.err != nil {
		return false, c.err
	}
	return c.exists, nil
}

var _ UserContentModerationRepository = (*userContentModerationRepoStub)(nil)
var _ AccountRepository = (*userContentModerationAccountRepoStub)(nil)
var _ SecretEncryptor = userContentModerationEncryptorStub{}
var _ SettingRepository = (*contentModerationSettingRepoStub)(nil)
var _ ContentModerationUserAPIKeyHashChecker = (*contentModerationUserAPIKeyHashCheckerStub)(nil)

func TestContentModerationUpdateConfigPropagatesUserKeyHashCheckerError(t *testing.T) {
	settingRepo := &contentModerationSettingRepoStub{}
	checker := &contentModerationUserAPIKeyHashCheckerStub{err: errors.New("db unavailable")}
	svc := NewContentModerationService(settingRepo, nil, nil, nil, nil, nil, nil)
	svc.SetUserAPIKeyHashChecker(checker)
	key := "shared-key"

	_, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{
		APIKey: &key,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "db unavailable")
	require.False(t, settingRepo.setCalled)
}

func TestUserContentModerationBuildLogUsesSanitizedErrorCodes(t *testing.T) {
	cfg := defaultUserContentModerationConfig(7, 42)
	svc := &UserContentModerationService{}
	log := svc.buildLog(UserContentModerationCheckInput{
		Moderation: ContentModerationCheckInput{RequestID: "req-1"},
		Account:    &Account{ID: 42, OwnerUserID: &cfg.OwnerUserID},
	}, cfg, ContentModerationActionError, false, "", 0, nil, true, userContentModerationErrorAPI)

	require.Equal(t, userContentModerationErrorAPI, log.Error)
	require.NotContains(t, log.Error, "sk-")
	require.NotContains(t, log.Error, "prompt")
}

func TestUserContentModerationUpdateConfigRequiresKeyWhenEnabled(t *testing.T) {
	ownerUserID := int64(7)
	accountID := int64(42)
	repo := &userContentModerationRepoStub{}
	svc := newUserContentModerationServiceForTest(t, repo, ownerUserID, accountID, nil)
	enabled := true

	_, err := svc.UpdateConfig(context.Background(), ownerUserID, accountID, UpdateUserContentModerationConfigInput{
		Enabled: &enabled,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "USER_CONTENT_MODERATION_API_KEY_REQUIRED")
	require.Nil(t, repo.upserted)
}

func TestUserContentModerationUpdateConfigRejectsInvalidSampleRate(t *testing.T) {
	ownerUserID := int64(7)
	accountID := int64(42)
	repo := &userContentModerationRepoStub{}
	svc := newUserContentModerationServiceForTest(t, repo, ownerUserID, accountID, nil)
	sampleRate := 0

	_, err := svc.UpdateConfig(context.Background(), ownerUserID, accountID, UpdateUserContentModerationConfigInput{
		SampleRate: &sampleRate,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "INVALID_USER_CONTENT_MODERATION_SAMPLE_RATE")
	require.Nil(t, repo.upserted)
}

func TestUserContentModerationListLogsCapsPageSize(t *testing.T) {
	ownerUserID := int64(7)
	accountID := int64(42)
	repo := &userContentModerationRepoStub{}
	svc := newUserContentModerationServiceForTest(t, repo, ownerUserID, accountID, nil)

	_, _, err := svc.ListLogs(context.Background(), ownerUserID, accountID, UserContentModerationLogFilter{
		Pagination: pagination.PaginationParams{Page: 0, PageSize: 1000},
	})

	require.NoError(t, err)
}

func TestUserContentModerationDecorateConfigViewMasksConfiguredKey(t *testing.T) {
	cfg := defaultUserContentModerationConfig(7, 42)
	cfg.APIKeyEncrypted = "enc:secret-key"
	svc := &UserContentModerationService{encryptor: userContentModerationEncryptorStub{}}

	svc.decorateConfigView(cfg)

	require.True(t, cfg.APIKeyConfigured)
	require.Equal(t, maskSecretTail("secret-key"), cfg.APIKeyMasked)
}

func TestUserContentModerationEnsureAPIKeyHashUniqueAllowsCurrentConfig(t *testing.T) {
	repo := &userContentModerationRepoStub{}
	settingRepo := &contentModerationSettingRepoStub{}
	moderationSvc := NewContentModerationService(settingRepo, nil, nil, nil, nil, nil, nil)
	svc := &UserContentModerationService{repo: repo, moderation: moderationSvc}

	err := svc.ensureAPIKeyHashUnique(context.Background(), 7, 42, moderationAPIKeyHash("new-key"))

	require.NoError(t, err)
	require.Len(t, repo.apiKeyHashQueries, 1)
}

func TestUserContentModerationConfigDefaults(t *testing.T) {
	cfg := defaultUserContentModerationConfig(7, 42)

	require.False(t, cfg.Enabled)
	require.Equal(t, ContentModerationModeObserve, cfg.Mode)
	require.Equal(t, ContentModerationProviderOpenAI, cfg.Provider)
	require.Equal(t, defaultContentModerationBaseURL, cfg.BaseURL)
	require.Equal(t, defaultContentModerationModel, cfg.Model)
	require.Equal(t, 100, cfg.SampleRate)
	require.WithinDuration(t, time.Time{}, cfg.CreatedAt, 0)
}
