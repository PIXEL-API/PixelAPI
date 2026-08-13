package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const (
	userContentModerationErrorConfig = "config_error"
	userContentModerationErrorAPI    = "moderation_api_error"
)

type UserContentModerationConfig struct {
	ID               int64     `json:"id"`
	OwnerUserID      int64     `json:"owner_user_id"`
	AccountID        int64     `json:"account_id"`
	Enabled          bool      `json:"enabled"`
	Mode             string    `json:"mode"`
	Provider         string    `json:"provider"`
	BaseURL          string    `json:"base_url"`
	Model            string    `json:"model"`
	APIKeyEncrypted  string    `json:"-"`
	APIKeyHash       string    `json:"-"`
	APIKeyConfigured bool      `json:"api_key_configured"`
	APIKeyMasked     string    `json:"api_key_masked"`
	SampleRate       int       `json:"sample_rate"`
	BlockMessage     string    `json:"block_message"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type UpdateUserContentModerationConfigInput struct {
	Enabled      *bool   `json:"enabled"`
	Mode         *string `json:"mode"`
	Provider     *string `json:"provider"`
	APIKey       *string `json:"api_key"`
	ClearAPIKey  bool    `json:"clear_api_key"`
	SampleRate   *int    `json:"sample_rate"`
	BlockMessage *string `json:"block_message"`
}

type UserContentModerationLog struct {
	ID              int64              `json:"id"`
	RequestID       string             `json:"request_id"`
	OwnerUserID     int64              `json:"owner_user_id"`
	AccountID       int64              `json:"account_id"`
	ConsumerUserID  *int64             `json:"consumer_user_id,omitempty"`
	APIKeyID        *int64             `json:"api_key_id,omitempty"`
	APIKeyName      string             `json:"api_key_name"`
	GroupID         *int64             `json:"group_id,omitempty"`
	Endpoint        string             `json:"endpoint"`
	Provider        string             `json:"provider"`
	Model           string             `json:"model"`
	Mode            string             `json:"mode"`
	Action          string             `json:"action"`
	Flagged         bool               `json:"flagged"`
	HighestCategory string             `json:"highest_category"`
	HighestScore    float64            `json:"highest_score"`
	CategoryScores  map[string]float64 `json:"category_scores"`
	Sampled         bool               `json:"sampled"`
	Error           string             `json:"error"`
	CreatedAt       time.Time          `json:"created_at"`
}

type UserContentModerationLogFilter struct {
	Pagination  pagination.PaginationParams
	OwnerUserID int64
	AccountID   int64
	Result      string
}

type UserContentModerationCheckInput struct {
	Moderation ContentModerationCheckInput
	Account    *Account
	Content    *ContentModerationInput
}

type UserContentModerationRepository interface {
	GetConfig(ctx context.Context, ownerUserID, accountID int64) (*UserContentModerationConfig, error)
	UpsertConfig(ctx context.Context, config *UserContentModerationConfig) error
	CreateLog(ctx context.Context, log *UserContentModerationLog) error
	ListLogs(ctx context.Context, filter UserContentModerationLogFilter) ([]UserContentModerationLog, *pagination.PaginationResult, error)
	APIKeyHashExists(ctx context.Context, apiKeyHash string, excludeOwnerUserID, excludeAccountID int64) (bool, error)
}

type UserContentModerationService struct {
	repo       UserContentModerationRepository
	accountSvc *AccountService
	encryptor  SecretEncryptor
	moderation *ContentModerationService
}

func NewUserContentModerationService(
	repo UserContentModerationRepository,
	accountSvc *AccountService,
	encryptor SecretEncryptor,
	moderation *ContentModerationService,
) *UserContentModerationService {
	return &UserContentModerationService{
		repo:       repo,
		accountSvc: accountSvc,
		encryptor:  encryptor,
		moderation: moderation,
	}
}

func (s *UserContentModerationService) GetConfig(ctx context.Context, ownerUserID, accountID int64) (*UserContentModerationConfig, error) {
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	if err := s.ensureOwnedAccount(ctx, ownerUserID, accountID); err != nil {
		return nil, err
	}
	cfg, err := s.repo.GetConfig(ctx, ownerUserID, accountID)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		cfg = defaultUserContentModerationConfig(ownerUserID, accountID)
	}
	s.decorateConfigView(cfg)
	return cfg, nil
}

func (s *UserContentModerationService) UpdateConfig(ctx context.Context, ownerUserID, accountID int64, input UpdateUserContentModerationConfigInput) (*UserContentModerationConfig, error) {
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	if err := s.ensureOwnedAccount(ctx, ownerUserID, accountID); err != nil {
		return nil, err
	}
	cfg, err := s.repo.GetConfig(ctx, ownerUserID, accountID)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		cfg = defaultUserContentModerationConfig(ownerUserID, accountID)
	}
	if input.Enabled != nil {
		cfg.Enabled = *input.Enabled
	}
	if input.Mode != nil {
		cfg.Mode = strings.TrimSpace(*input.Mode)
	}
	if input.Provider != nil {
		cfg.Provider = strings.TrimSpace(*input.Provider)
	}
	if input.SampleRate != nil {
		cfg.SampleRate = *input.SampleRate
	}
	if input.BlockMessage != nil {
		cfg.BlockMessage = strings.TrimSpace(*input.BlockMessage)
	}
	if input.ClearAPIKey {
		cfg.APIKeyEncrypted = ""
		cfg.APIKeyHash = ""
	} else if input.APIKey != nil && strings.TrimSpace(*input.APIKey) != "" {
		apiKey := strings.TrimSpace(*input.APIKey)
		apiKeyHash := moderationAPIKeyHash(apiKey)
		if err := s.ensureAPIKeyHashUnique(ctx, ownerUserID, accountID, apiKeyHash); err != nil {
			return nil, err
		}
		encrypted, err := s.encryptor.Encrypt(apiKey)
		if err != nil {
			return nil, fmt.Errorf("encrypt user content moderation api key: %w", err)
		}
		cfg.APIKeyEncrypted = encrypted
		cfg.APIKeyHash = apiKeyHash
	}
	if err := normalizeAndValidateUserModerationConfig(cfg); err != nil {
		return nil, err
	}
	if cfg.Enabled && cfg.APIKeyEncrypted == "" {
		return nil, infraerrors.BadRequest("USER_CONTENT_MODERATION_API_KEY_REQUIRED", "api_key is required when moderation is enabled")
	}
	if err := s.repo.UpsertConfig(ctx, cfg); err != nil {
		return nil, err
	}
	s.decorateConfigView(cfg)
	return cfg, nil
}

func (s *UserContentModerationService) Test(ctx context.Context, ownerUserID, accountID int64, prompt string, images []string) (*ContentModerationTestAuditResult, error) {
	if err := s.ensureReady(); err != nil {
		return nil, err
	}
	if err := s.ensureOwnedAccount(ctx, ownerUserID, accountID); err != nil {
		return nil, err
	}
	cfg, err := s.repo.GetConfig(ctx, ownerUserID, accountID)
	if err != nil {
		return nil, err
	}
	if cfg == nil || cfg.APIKeyEncrypted == "" {
		return nil, infraerrors.BadRequest("USER_CONTENT_MODERATION_API_KEY_REQUIRED", "api_key is required before testing moderation")
	}
	if err := normalizeAndValidateUserModerationConfig(cfg); err != nil {
		return nil, err
	}
	auditCfg, err := s.toAuditConfig(cfg)
	if err != nil {
		return nil, err
	}
	testInput, _, err := buildModerationTestInput(prompt, images)
	if err != nil {
		return nil, err
	}
	result, err := s.moderation.callModeration(ctx, auditCfg, testInput)
	if err != nil {
		return nil, infraerrors.BadRequest("USER_CONTENT_MODERATION_TEST_FAILED", "moderation test failed").WithCause(err)
	}
	return buildContentModerationTestAuditResult(result, auditCfg.Thresholds), nil
}

func (s *UserContentModerationService) ListLogs(ctx context.Context, ownerUserID, accountID int64, filter UserContentModerationLogFilter) ([]UserContentModerationLog, *pagination.PaginationResult, error) {
	if err := s.ensureReady(); err != nil {
		return nil, nil, err
	}
	if err := s.ensureOwnedAccount(ctx, ownerUserID, accountID); err != nil {
		return nil, nil, err
	}
	filter.OwnerUserID = ownerUserID
	filter.AccountID = accountID
	if filter.Pagination.Page <= 0 {
		filter.Pagination.Page = 1
	}
	if filter.Pagination.PageSize <= 0 {
		filter.Pagination.PageSize = 20
	}
	if filter.Pagination.PageSize > 100 {
		filter.Pagination.PageSize = 100
	}
	if filter.Pagination.SortOrder == "" {
		filter.Pagination.SortOrder = pagination.SortOrderDesc
	}
	return s.repo.ListLogs(ctx, filter)
}

func (s *UserContentModerationService) CheckAccountRequest(ctx context.Context, input UserContentModerationCheckInput) (*ContentModerationDecision, error) {
	allow := &ContentModerationDecision{Allowed: true, Action: ContentModerationActionAllow}
	if s == nil || s.repo == nil || s.moderation == nil || input.Account == nil || input.Account.OwnerUserID == nil {
		return allow, nil
	}
	ownerUserID := *input.Account.OwnerUserID
	if ownerUserID <= 0 {
		return allow, nil
	}
	cfg, err := s.repo.GetConfig(ctx, ownerUserID, input.Account.ID)
	if err != nil {
		return allow, err
	}
	if cfg == nil || !cfg.Enabled {
		return allow, nil
	}
	if err := normalizeAndValidateUserModerationConfig(cfg); err != nil {
		return allow, err
	}
	if cfg.Mode == ContentModerationModeOff || cfg.APIKeyEncrypted == "" {
		return allow, nil
	}

	var content ContentModerationInput
	if input.Content != nil {
		content = input.Content.Clone()
	} else if input.Moderation.Content != nil {
		content = input.Moderation.Content.Clone()
	} else if input.Moderation.ContentSource != nil {
		content = input.Moderation.ContentSource.ContentModerationInputCopy()
	} else {
		content = ExtractContentModerationInput(input.Moderation.Protocol, input.Moderation.Body)
	}
	if content.IsEmpty() {
		return allow, nil
	}
	content.Normalize()
	hashText := content.Hash()
	if !userModerationShouldSample(cfg.SampleRate, hashText) {
		return allow, nil
	}

	auditCfg, err := s.toAuditConfig(cfg)
	if err != nil {
		_ = s.repo.CreateLog(ctx, s.buildLog(input, cfg, ContentModerationActionError, false, "", 0, nil, true, userContentModerationErrorConfig))
		return allow, err
	}
	start := time.Now()
	result, err := s.moderation.callModeration(ctx, auditCfg, content)
	latencyMS := int(time.Since(start).Milliseconds())
	if err != nil {
		slog.Warn("user_content_moderation.audit_failed",
			"owner_user_id", ownerUserID,
			"account_id", input.Account.ID,
			"request_id", input.Moderation.RequestID,
			"latency_ms", latencyMS,
			"error", err)
		_ = s.repo.CreateLog(ctx, s.buildLog(input, cfg, ContentModerationActionError, false, "", 0, nil, true, userContentModerationErrorAPI))
		return allow, nil
	}

	action := ContentModerationActionAllow
	blocked := false
	if result.Flagged && cfg.Mode == ContentModerationModePreBlock {
		action = ContentModerationActionBlock
		blocked = true
	}
	_ = s.repo.CreateLog(ctx, s.buildLog(
		input,
		cfg,
		action,
		result.Flagged,
		result.HighestCategory,
		result.HighestScore,
		result.CategoryScores,
		true,
		"",
	))
	if blocked {
		return &ContentModerationDecision{
			Allowed:         false,
			Blocked:         true,
			Flagged:         true,
			Message:         cfg.BlockMessage,
			StatusCode:      defaultContentModerationBlockHTTPStatus,
			HighestCategory: result.HighestCategory,
			HighestScore:    result.HighestScore,
			CategoryScores:  result.CategoryScores,
			Action:          action,
		}, nil
	}
	return &ContentModerationDecision{
		Allowed:         true,
		Flagged:         result.Flagged,
		HighestCategory: result.HighestCategory,
		HighestScore:    result.HighestScore,
		CategoryScores:  result.CategoryScores,
		Action:          action,
	}, nil
}

func (s *UserContentModerationService) ensureReady() error {
	if s == nil || s.repo == nil || s.accountSvc == nil || s.encryptor == nil || s.moderation == nil {
		return infraerrors.ServiceUnavailable("USER_CONTENT_MODERATION_UNAVAILABLE", "user content moderation service is unavailable")
	}
	return nil
}

func (s *UserContentModerationService) ensureOwnedAccount(ctx context.Context, ownerUserID, accountID int64) error {
	if ownerUserID <= 0 || accountID <= 0 {
		return infraerrors.BadRequest("INVALID_USER_CONTENT_MODERATION_ACCOUNT", "invalid account id")
	}
	_, err := s.accountSvc.GetOwnedByID(ctx, ownerUserID, accountID)
	return err
}

func (s *UserContentModerationService) ensureAPIKeyHashUnique(ctx context.Context, ownerUserID, accountID int64, apiKeyHash string) error {
	apiKeyHash = strings.TrimSpace(apiKeyHash)
	if apiKeyHash == "" {
		return infraerrors.BadRequest("USER_CONTENT_MODERATION_API_KEY_REQUIRED", "api_key is required when moderation is enabled")
	}
	if s.moderation != nil {
		cfg, err := s.moderation.loadConfig(ctx)
		if err != nil {
			return err
		}
		for _, key := range cfg.apiKeys() {
			if moderationAPIKeyHash(key) == apiKeyHash {
				return infraerrors.BadRequest("USER_CONTENT_MODERATION_API_KEY_DUPLICATED", "api_key is already used by another moderation config")
			}
		}
	}
	exists, err := s.repo.APIKeyHashExists(ctx, apiKeyHash, ownerUserID, accountID)
	if err != nil {
		return err
	}
	if exists {
		return infraerrors.BadRequest("USER_CONTENT_MODERATION_API_KEY_DUPLICATED", "api_key is already used by another moderation config")
	}
	return nil
}

func (s *UserContentModerationService) toAuditConfig(cfg *UserContentModerationConfig) (*ContentModerationConfig, error) {
	if cfg == nil {
		return nil, errors.New("user content moderation config is nil")
	}
	apiKey, err := s.encryptor.Decrypt(cfg.APIKeyEncrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt user content moderation api key: %w", err)
	}
	auditCfg := &ContentModerationConfig{
		Enabled:       cfg.Enabled,
		Mode:          cfg.Mode,
		Provider:      cfg.Provider,
		BaseURL:       cfg.BaseURL,
		Model:         cfg.Model,
		APIKeys:       []string{apiKey},
		TimeoutMS:     defaultContentModerationTimeoutMS,
		SampleRate:    cfg.SampleRate,
		RecordNonHits: true,
		Thresholds:    ContentModerationDefaultThresholds(),
		RetryCount:    defaultContentModerationRetryCount,
		BlockStatus:   defaultContentModerationBlockHTTPStatus,
		BlockMessage:  cfg.BlockMessage,
	}
	auditCfg.normalize()
	return auditCfg, nil
}

func (s *UserContentModerationService) decorateConfigView(cfg *UserContentModerationConfig) {
	if cfg == nil {
		return
	}
	cfg.APIKeyConfigured = cfg.APIKeyEncrypted != ""
	cfg.APIKeyMasked = ""
	if !cfg.APIKeyConfigured || s == nil || s.encryptor == nil {
		return
	}
	apiKey, err := s.encryptor.Decrypt(cfg.APIKeyEncrypted)
	if err != nil {
		cfg.APIKeyMasked = "********"
		return
	}
	cfg.APIKeyMasked = maskSecretTail(apiKey)
}

func (s *UserContentModerationService) buildLog(input UserContentModerationCheckInput, cfg *UserContentModerationConfig, action string, flagged bool, highestCategory string, highestScore float64, categoryScores map[string]float64, sampled bool, errText string) *UserContentModerationLog {
	ownerUserID := int64(0)
	accountID := int64(0)
	if input.Account != nil {
		accountID = input.Account.ID
		if input.Account.OwnerUserID != nil {
			ownerUserID = *input.Account.OwnerUserID
		}
	}
	if cfg != nil {
		if ownerUserID <= 0 {
			ownerUserID = cfg.OwnerUserID
		}
		if accountID <= 0 {
			accountID = cfg.AccountID
		}
	}
	var consumerUserID *int64
	if input.Moderation.UserID > 0 {
		v := input.Moderation.UserID
		consumerUserID = &v
	}
	var apiKeyID *int64
	if input.Moderation.APIKeyID > 0 {
		v := input.Moderation.APIKeyID
		apiKeyID = &v
	}
	return &UserContentModerationLog{
		RequestID:       input.Moderation.RequestID,
		OwnerUserID:     ownerUserID,
		AccountID:       accountID,
		ConsumerUserID:  consumerUserID,
		APIKeyID:        apiKeyID,
		APIKeyName:      input.Moderation.APIKeyName,
		GroupID:         cloneContentModerationInt64Ptr(input.Moderation.GroupID),
		Endpoint:        input.Moderation.Endpoint,
		Provider:        cfg.Provider,
		Model:           cfg.Model,
		Mode:            cfg.Mode,
		Action:          action,
		Flagged:         flagged,
		HighestCategory: highestCategory,
		HighestScore:    highestScore,
		CategoryScores:  cloneFloatMap(categoryScores),
		Sampled:         sampled,
		Error:           strings.TrimSpace(errText),
	}
}

func defaultUserContentModerationConfig(ownerUserID, accountID int64) *UserContentModerationConfig {
	return &UserContentModerationConfig{
		OwnerUserID:  ownerUserID,
		AccountID:    accountID,
		Enabled:      false,
		Mode:         ContentModerationModeObserve,
		Provider:     ContentModerationProviderOpenAI,
		BaseURL:      defaultContentModerationBaseURL,
		Model:        defaultContentModerationModel,
		SampleRate:   100,
		BlockMessage: defaultContentModerationBlockMessage,
	}
}

func normalizeAndValidateUserModerationConfig(cfg *UserContentModerationConfig) error {
	if cfg == nil {
		return infraerrors.BadRequest("INVALID_USER_CONTENT_MODERATION_CONFIG", "invalid moderation config")
	}
	if cfg.OwnerUserID <= 0 || cfg.AccountID <= 0 {
		return infraerrors.BadRequest("INVALID_USER_CONTENT_MODERATION_ACCOUNT", "invalid account id")
	}
	switch strings.TrimSpace(cfg.Mode) {
	case ContentModerationModeObserve, ContentModerationModePreBlock:
	default:
		return infraerrors.BadRequest("INVALID_USER_CONTENT_MODERATION_MODE", "mode must be observe or pre_block")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "":
		cfg.Provider = ContentModerationProviderOpenAI
	case ContentModerationProviderOpenAI:
		cfg.Provider = ContentModerationProviderOpenAI
	case ContentModerationProviderZhipu:
		cfg.Provider = ContentModerationProviderZhipu
	default:
		return infraerrors.BadRequest("INVALID_USER_CONTENT_MODERATION_PROVIDER", "provider must be openai or zhipu")
	}
	cfg.BaseURL = defaultContentModerationBaseURLForProvider(cfg.Provider)
	cfg.Model = defaultContentModerationModelForProvider(cfg.Provider)
	if cfg.SampleRate < 1 || cfg.SampleRate > 100 {
		return infraerrors.BadRequest("INVALID_USER_CONTENT_MODERATION_SAMPLE_RATE", "sample_rate must be between 1 and 100")
	}
	if cfg.BlockMessage = strings.TrimSpace(cfg.BlockMessage); cfg.BlockMessage == "" {
		cfg.BlockMessage = defaultContentModerationBlockMessage
	}
	return nil
}

func userModerationShouldSample(sampleRate int, hashText string) bool {
	cfg := &ContentModerationConfig{SampleRate: sampleRate}
	return cfg.shouldSample(hashText)
}
