package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
)

const (
	ContentModerationModeOff      = "off"
	ContentModerationModeObserve  = "observe"
	ContentModerationModePreBlock = "pre_block"

	ContentModerationProviderOpenAI = "openai"
	ContentModerationProviderZhipu  = "zhipu"

	contentModerationAPIKeysModeAppend  = "append"
	contentModerationAPIKeysModeReplace = "replace"

	ContentModerationActionAllow      = "allow"
	ContentModerationActionBlock      = "block"
	ContentModerationActionHashBlock  = "hash_block"
	ContentModerationActionCyberBlock = "cyber_preflight_block"
	ContentModerationActionError      = "error"

	ContentModerationProtocolAnthropicMessages = "anthropic_messages"
	ContentModerationProtocolOpenAIResponses   = "openai_responses"
	ContentModerationProtocolOpenAIChat        = "openai_chat_completions"
	ContentModerationProtocolGemini            = "gemini"
	ContentModerationProtocolOpenAIImages      = "openai_images"

	defaultContentModerationBaseURL      = "https://api.openai.com"
	defaultContentModerationModel        = "omni-moderation-latest"
	defaultZhipuContentModerationBaseURL = "https://open.bigmodel.cn"
	defaultZhipuContentModerationModel   = "moderation"
	defaultContentModerationTimeoutMS    = 3000
	maxContentModerationTimeoutMS        = 30000
	maxModerationInputRunes              = 12000
	maxZhipuModerationInputRunes         = 2000
	maxModerationExcerptRunes            = 240

	defaultContentModerationWorkerCount          = 4
	maxContentModerationWorkerCount              = 32
	defaultContentModerationQueueSize            = 32768
	maxContentModerationQueueSize                = 100000
	defaultContentModerationBanThreshold         = 10
	defaultContentModerationViolationWindowHours = 720
	defaultContentModerationBlockHTTPStatus      = http.StatusForbidden
	defaultContentModerationBlockMessage         = "内容审计命中风险规则，请调整输入后重试"
	defaultContentModerationCyberBlockMessage    = "请求可能涉及网络安全滥用风险，已在账号选择前拦截"
	maxCyberPreflightRulePhrases                 = 512
	maxCyberPreflightRulePhraseRunes             = 200
	// 重试默认 1 次：审核调用同步挡在网关请求前面，每多一次重试就多一个 TimeoutMS
	// 的最坏延迟，默认值优先保证尾延迟而不是审核成功率（失败时本就是放行）。
	defaultContentModerationRetryCount          = 1
	maxContentModerationRetryCount              = 5
	defaultContentModerationHitRetentionDays    = 180
	defaultContentModerationNonHitRetentionDays = 3
	maxContentModerationRetentionDays           = 3650
	maxContentModerationNonHitRetentionDays     = 3
	contentModerationKeyRateLimitFreezeDuration = time.Minute
	contentModerationKeyAuthFreezeDuration      = 10 * time.Minute
	contentModerationKeyHTTPErrorFreezeDuration = 10 * time.Second
	maxContentModerationInputImages             = 1
	maxContentModerationTestImages              = maxContentModerationInputImages
	maxContentModerationTestImageBytes          = 8 * 1024 * 1024
	maxContentModerationTestImageDataURLBytes   = 12 * 1024 * 1024

	contentModerationCleanupInterval = 24 * time.Hour
	contentModerationCleanupTimeout  = 30 * time.Minute
	contentModerationCleanupDelay    = 5 * time.Minute
	contentModerationCleanupTaskName = "content_moderation_cleanup"

	contentModerationRuntimeCacheTTL       = time.Second
	contentModerationRuntimeRefreshTimeout = 5 * time.Second

	// 账号广场模式分组的判定每个网关请求都要做一次，且底层是一次未缓存的 EXISTS 查询。
	// 模式分组极少变动，短 TTL 缓存足以消掉这条每请求查询；最坏陈旧 TTL 后自愈。
	contentModerationModeGroupCacheTTL = 30 * time.Second

	// 命中后的告知邮件走异步、并按用户限频：邮件对同一用户的连续命中没有增量价值，
	// 而同步发信会把 SMTP 握手压进网关请求，且可被用户自行刷量放大。
	contentModerationViolationEmailCooldown = 30 * time.Minute
	contentModerationEmailDispatchLimit     = 16
	contentModerationEmailDispatchTimeout   = 30 * time.Second

	// 少数 Warn 描述的是"持续存在的错误状态"（审核服务不可用、未配置 Key、Redis 故障），
	// 一旦发生就会每个请求各打一条。这类日志按 key 限频，保证问题可见但不刷屏。
	contentModerationWarnLogInterval = time.Minute

	contentModerationScopeTypeGroup            = "group"
	contentModerationScopeTypeAccountShareMode = "account_share_mode"
)

var contentModerationCategoryOrder = []string{
	"harassment",
	"harassment/threatening",
	"hate",
	"hate/threatening",
	"illicit",
	"illicit/violent",
	"self-harm",
	"self-harm/intent",
	"self-harm/instructions",
	"sexual",
	"sexual/minors",
	"violence",
	"violence/graphic",
}

var ErrContentModerationUnsupportedInput = errors.New("content moderation input type is not supported by provider")

func ContentModerationDefaultThresholds() map[string]float64 {
	return map[string]float64{
		"harassment":             0.98,
		"harassment/threatening": 0.90,
		"hate":                   0.65,
		"hate/threatening":       0.65,
		"illicit":                0.95,
		"illicit/violent":        0.95,
		"self-harm":              0.65,
		"self-harm/intent":       0.85,
		"self-harm/instructions": 0.65,
		"sexual":                 0.65,
		"sexual/minors":          0.65,
		"violence":               0.95,
		"violence/graphic":       0.95,
	}
}

func ContentModerationCategories() []string {
	out := make([]string, len(contentModerationCategoryOrder))
	copy(out, contentModerationCategoryOrder)
	return out
}

type ContentModerationConfig struct {
	Enabled                    bool                                         `json:"enabled"`
	CyberPreflightEnabled      bool                                         `json:"cyber_preflight_enabled"`
	CyberPreflightRules        ContentModerationCyberPreflightRulesConfig   `json:"cyber_preflight_rules"`
	CyberPreflightDefaultRules ContentModerationCyberPreflightRulesConfig   `json:"cyber_preflight_default_rules"`
	Mode                       string                                       `json:"mode"`
	Provider                   string                                       `json:"provider"`
	BaseURL                    string                                       `json:"base_url"`
	Model                      string                                       `json:"model"`
	APIKey                     string                                       `json:"api_key,omitempty"`
	APIKeys                    []string                                     `json:"api_keys,omitempty"`
	TimeoutMS                  int                                          `json:"timeout_ms"`
	SampleRate                 int                                          `json:"sample_rate"`
	DynamicSampling            ContentModerationDynamicSamplingConfig       `json:"dynamic_sampling"`
	AllGroups                  bool                                         `json:"all_groups"`
	GroupIDs                   []int64                                      `json:"group_ids"`
	RecordNonHits              bool                                         `json:"record_non_hits"`
	Thresholds                 map[string]float64                           `json:"thresholds"`
	WorkerCount                int                                          `json:"worker_count"`
	QueueSize                  int                                          `json:"queue_size"`
	BlockStatus                int                                          `json:"block_status"`
	BlockMessage               string                                       `json:"block_message"`
	EmailOnHit                 bool                                         `json:"email_on_hit"`
	AutoBanEnabled             bool                                         `json:"auto_ban_enabled"`
	BanThreshold               int                                          `json:"ban_threshold"`
	ViolationWindowHours       int                                          `json:"violation_window_hours"`
	RetryCount                 int                                          `json:"retry_count"`
	HitRetentionDays           int                                          `json:"hit_retention_days"`
	NonHitRetentionDays        int                                          `json:"non_hit_retention_days"`
	PreHashCheckEnabled        bool                                         `json:"pre_hash_check_enabled"`
	AccountShareModeScope      ContentModerationAccountShareModeScopeConfig `json:"account_share_mode_scope"`
}

type ContentModerationConfigView struct {
	Enabled                    bool                                         `json:"enabled"`
	CyberPreflightEnabled      bool                                         `json:"cyber_preflight_enabled"`
	CyberPreflightRules        ContentModerationCyberPreflightRulesConfig   `json:"cyber_preflight_rules"`
	CyberPreflightDefaultRules ContentModerationCyberPreflightRulesConfig   `json:"cyber_preflight_default_rules"`
	Mode                       string                                       `json:"mode"`
	Provider                   string                                       `json:"provider"`
	BaseURL                    string                                       `json:"base_url"`
	Model                      string                                       `json:"model"`
	APIKeyConfigured           bool                                         `json:"api_key_configured"`
	APIKeyMasked               string                                       `json:"api_key_masked"`
	APIKeyCount                int                                          `json:"api_key_count"`
	APIKeyMasks                []string                                     `json:"api_key_masks"`
	APIKeyStatuses             []ContentModerationAPIKeyStatus              `json:"api_key_statuses"`
	TimeoutMS                  int                                          `json:"timeout_ms"`
	SampleRate                 int                                          `json:"sample_rate"`
	DynamicSampling            ContentModerationDynamicSamplingConfig       `json:"dynamic_sampling"`
	AllGroups                  bool                                         `json:"all_groups"`
	GroupIDs                   []int64                                      `json:"group_ids"`
	RecordNonHits              bool                                         `json:"record_non_hits"`
	WorkerCount                int                                          `json:"worker_count"`
	QueueSize                  int                                          `json:"queue_size"`
	BlockStatus                int                                          `json:"block_status"`
	BlockMessage               string                                       `json:"block_message"`
	EmailOnHit                 bool                                         `json:"email_on_hit"`
	AutoBanEnabled             bool                                         `json:"auto_ban_enabled"`
	BanThreshold               int                                          `json:"ban_threshold"`
	ViolationWindowHours       int                                          `json:"violation_window_hours"`
	RetryCount                 int                                          `json:"retry_count"`
	HitRetentionDays           int                                          `json:"hit_retention_days"`
	NonHitRetentionDays        int                                          `json:"non_hit_retention_days"`
	PreHashCheckEnabled        bool                                         `json:"pre_hash_check_enabled"`
	AccountShareModeScope      ContentModerationAccountShareModeScopeConfig `json:"account_share_mode_scope"`
}

type ContentModerationAccountShareModeScopeConfig struct {
	Enabled    bool     `json:"enabled"`
	All        bool     `json:"all"`
	Platforms  []string `json:"platforms"`
	ListingIDs []int64  `json:"listing_ids"`
}

type ContentModerationCyberPreflightRulesConfig struct {
	StandaloneBlockMarkers       []string `json:"standalone_block_markers"`
	HardMarkers                  []string `json:"hard_markers"`
	OffensiveIntentMarkers       []string `json:"offensive_intent_markers"`
	CredentialAbuseIntentMarkers []string `json:"credential_abuse_intent_markers"`
	TechniqueMarkers             []string `json:"technique_markers"`
	CredentialMarkers            []string `json:"credential_markers"`
	TargetMarkers                []string `json:"target_markers"`
	DefensiveMarkers             []string `json:"defensive_markers"`
}

type ContentModerationAPIKeyStatus struct {
	Index          int        `json:"index"`
	KeyHash        string     `json:"key_hash"`
	Masked         string     `json:"masked"`
	Status         string     `json:"status"`
	FailureCount   int        `json:"failure_count"`
	SuccessCount   int64      `json:"success_count"`
	LastError      string     `json:"last_error"`
	LastCheckedAt  *time.Time `json:"last_checked_at,omitempty"`
	FrozenUntil    *time.Time `json:"frozen_until,omitempty"`
	LastLatencyMS  int        `json:"last_latency_ms"`
	LastHTTPStatus int        `json:"last_http_status"`
	LastTested     bool       `json:"last_tested"`
	Configured     bool       `json:"configured"`
}

type TestContentModerationAPIKeysInput struct {
	APIKeys   []string `json:"api_keys"`
	Provider  string   `json:"provider"`
	BaseURL   string   `json:"base_url"`
	Model     string   `json:"model"`
	TimeoutMS int      `json:"timeout_ms"`
	Prompt    string   `json:"prompt"`
	Images    []string `json:"images"`
}

type TestContentModerationAPIKeysResult struct {
	Items       []ContentModerationAPIKeyStatus   `json:"items"`
	AuditResult *ContentModerationTestAuditResult `json:"audit_result,omitempty"`
	ImageCount  int                               `json:"image_count"`
}

type ContentModerationTestAuditResult struct {
	Flagged         bool               `json:"flagged"`
	RiskLevel       string             `json:"risk_level"`
	HighestCategory string             `json:"highest_category"`
	HighestScore    float64            `json:"highest_score"`
	CompositeScore  float64            `json:"composite_score"`
	CategoryScores  map[string]float64 `json:"category_scores"`
	Thresholds      map[string]float64 `json:"thresholds"`
}

type UpdateContentModerationConfigInput struct {
	Enabled               *bool                                         `json:"enabled"`
	CyberPreflightEnabled *bool                                         `json:"cyber_preflight_enabled"`
	CyberPreflightRules   *ContentModerationCyberPreflightRulesConfig   `json:"cyber_preflight_rules"`
	Mode                  *string                                       `json:"mode"`
	Provider              *string                                       `json:"provider"`
	BaseURL               *string                                       `json:"base_url"`
	Model                 *string                                       `json:"model"`
	APIKey                *string                                       `json:"api_key"`
	APIKeys               *[]string                                     `json:"api_keys"`
	APIKeysMode           string                                        `json:"api_keys_mode"`
	DeleteAPIKeyHashes    *[]string                                     `json:"delete_api_key_hashes"`
	ClearAPIKey           bool                                          `json:"clear_api_key"`
	TimeoutMS             *int                                          `json:"timeout_ms"`
	SampleRate            *int                                          `json:"sample_rate"`
	DynamicSampling       *ContentModerationDynamicSamplingConfig       `json:"dynamic_sampling"`
	AllGroups             *bool                                         `json:"all_groups"`
	GroupIDs              *[]int64                                      `json:"group_ids"`
	RecordNonHits         *bool                                         `json:"record_non_hits"`
	WorkerCount           *int                                          `json:"worker_count"`
	QueueSize             *int                                          `json:"queue_size"`
	BlockStatus           *int                                          `json:"block_status"`
	BlockMessage          *string                                       `json:"block_message"`
	EmailOnHit            *bool                                         `json:"email_on_hit"`
	AutoBanEnabled        *bool                                         `json:"auto_ban_enabled"`
	BanThreshold          *int                                          `json:"ban_threshold"`
	ViolationWindowHours  *int                                          `json:"violation_window_hours"`
	RetryCount            *int                                          `json:"retry_count"`
	HitRetentionDays      *int                                          `json:"hit_retention_days"`
	NonHitRetentionDays   *int                                          `json:"non_hit_retention_days"`
	PreHashCheckEnabled   *bool                                         `json:"pre_hash_check_enabled"`
	AccountShareModeScope *ContentModerationAccountShareModeScopeConfig `json:"account_share_mode_scope"`
}

type ContentModerationCheckInput struct {
	RequestID     string
	UserID        int64
	UserEmail     string
	APIKeyID      int64
	APIKeyName    string
	GroupID       *int64
	GroupName     string
	Endpoint      string
	Provider      string
	Model         string
	Protocol      string
	Body          []byte
	Content       *ContentModerationInput
	ContentSource ContentModerationInputSource
}

type ContentModerationInputSource interface {
	ContentModerationInputCopy() ContentModerationInput
	CyberPreflightInputCopy() ContentModerationInput
}

type ContentModerationInput struct {
	Text            string
	Images          []string
	allImageDigests [][sha256.Size]byte
}

func (in ContentModerationInput) Clone() ContentModerationInput {
	clone := ContentModerationInput{
		Text: in.Text,
	}
	if len(in.Images) > 0 {
		clone.Images = append([]string(nil), in.Images...)
	}
	if len(in.allImageDigests) > 0 {
		clone.allImageDigests = append([][sha256.Size]byte(nil), in.allImageDigests...)
	}
	return clone
}

func (in *ContentModerationInput) Normalize() {
	if in == nil {
		return
	}
	in.Text = trimRunes(normalizeContentModerationText(in.Text), maxModerationInputRunes)
	in.Images = normalizeModerationImages(in.Images)
}

func (in ContentModerationInput) IsEmpty() bool {
	return strings.TrimSpace(in.Text) == "" && len(in.Images) == 0
}

func (in ContentModerationInput) ModerationInput() any {
	images := limitContentModerationImages(in.Images)
	if len(images) == 0 {
		return in.Text
	}
	parts := make([]moderationAPIInputPart, 0, len(images)+1)
	if strings.TrimSpace(in.Text) != "" {
		parts = append(parts, moderationAPIInputPart{Type: "text", Text: in.Text})
	}
	for _, image := range images {
		parts = append(parts, moderationAPIInputPart{
			Type:     "image_url",
			ImageURL: &moderationAPIImageURLRef{URL: image},
		})
	}
	return parts
}

func (in ContentModerationInput) ExcerptText() string {
	return in.Text
}

func (in ContentModerationInput) Hash() string {
	h := sha256.New()
	_, _ = h.Write([]byte("text:"))
	_, _ = h.Write([]byte(in.Text))
	if len(in.allImageDigests) > 0 {
		for _, imageDigest := range in.allImageDigests {
			writeContentModerationImageDigest(h, imageDigest)
		}
	} else {
		for _, image := range in.Images {
			writeContentModerationImageDigest(h, sha256.Sum256([]byte(image)))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeContentModerationImageDigest(h hash.Hash, imageDigest [sha256.Size]byte) {
	if h == nil {
		return
	}
	var encoded [sha256.Size * 2]byte
	hex.Encode(encoded[:], imageDigest[:])
	_, _ = h.Write([]byte("\nimage:"))
	_, _ = h.Write(encoded[:])
}

type ContentModerationDecision struct {
	Allowed         bool               `json:"allowed"`
	Blocked         bool               `json:"blocked"`
	Flagged         bool               `json:"flagged"`
	Message         string             `json:"message"`
	StatusCode      int                `json:"status_code"`
	InputHash       string             `json:"input_hash,omitempty"`
	HighestCategory string             `json:"highest_category"`
	HighestScore    float64            `json:"highest_score"`
	CategoryScores  map[string]float64 `json:"category_scores"`
	Action          string             `json:"action"`
}

type ContentModerationLog struct {
	ID                    int64              `json:"id"`
	RequestID             string             `json:"request_id"`
	UserID                *int64             `json:"user_id,omitempty"`
	UserEmail             string             `json:"user_email"`
	APIKeyID              *int64             `json:"api_key_id,omitempty"`
	APIKeyName            string             `json:"api_key_name"`
	GroupID               *int64             `json:"group_id,omitempty"`
	GroupName             string             `json:"group_name"`
	ScopeType             string             `json:"scope_type"`
	AccountShareListingID *int64             `json:"account_share_listing_id,omitempty"`
	AccountID             *int64             `json:"account_id,omitempty"`
	OwnerUserID           *int64             `json:"owner_user_id,omitempty"`
	ConsumerUserID        *int64             `json:"consumer_user_id,omitempty"`
	MembershipID          *int64             `json:"membership_id,omitempty"`
	Endpoint              string             `json:"endpoint"`
	Provider              string             `json:"provider"`
	Model                 string             `json:"model"`
	Mode                  string             `json:"mode"`
	Action                string             `json:"action"`
	Flagged               bool               `json:"flagged"`
	HighestCategory       string             `json:"highest_category"`
	HighestScore          float64            `json:"highest_score"`
	CategoryScores        map[string]float64 `json:"category_scores"`
	ThresholdSnapshot     map[string]float64 `json:"threshold_snapshot"`
	InputExcerpt          string             `json:"input_excerpt"`
	UpstreamLatencyMS     *int               `json:"upstream_latency_ms,omitempty"`
	Error                 string             `json:"error"`
	ViolationCount        int                `json:"violation_count"`
	AutoBanned            bool               `json:"auto_banned"`
	EmailSent             bool               `json:"email_sent"`
	UserStatus            string             `json:"user_status"`
	QueueDelayMS          *int               `json:"queue_delay_ms,omitempty"`
	CreatedAt             time.Time          `json:"created_at"`
}

type ContentModerationLogFilter struct {
	Pagination pagination.PaginationParams
	Result     string
	GroupID    *int64
	Endpoint   string
	Search     string
	From       *time.Time
	To         *time.Time
}

type ContentModerationCleanupResult struct {
	DeletedHit    int64     `json:"deleted_hit"`
	DeletedNonHit int64     `json:"deleted_non_hit"`
	FinishedAt    time.Time `json:"finished_at"`
}

type ContentModerationRuntimeStatus struct {
	Enabled                  bool                                          `json:"enabled"`
	CyberPreflightEnabled    bool                                          `json:"cyber_preflight_enabled"`
	RiskControlEnabled       bool                                          `json:"risk_control_enabled"`
	Mode                     string                                        `json:"mode"`
	WorkerCount              int                                           `json:"worker_count"`
	MaxWorkers               int                                           `json:"max_workers"`
	ActiveWorkers            int                                           `json:"active_workers"`
	IdleWorkers              int                                           `json:"idle_workers"`
	QueueSize                int                                           `json:"queue_size"`
	QueueLength              int                                           `json:"queue_length"`
	QueueUsagePercent        float64                                       `json:"queue_usage_percent"`
	Enqueued                 int64                                         `json:"enqueued"`
	Dropped                  int64                                         `json:"dropped"`
	Processed                int64                                         `json:"processed"`
	Errors                   int64                                         `json:"errors"`
	DynamicSampling          ContentModerationDynamicSamplingRuntimeStatus `json:"dynamic_sampling"`
	APIKeyStatuses           []ContentModerationAPIKeyStatus               `json:"api_key_statuses"`
	FlaggedHashCount         int64                                         `json:"flagged_hash_count"`
	LastCleanupAt            *time.Time                                    `json:"last_cleanup_at,omitempty"`
	LastCleanupDeletedHit    int64                                         `json:"last_cleanup_deleted_hit"`
	LastCleanupDeletedNonHit int64                                         `json:"last_cleanup_deleted_non_hit"`
}

type ContentModerationUnbanUserResult struct {
	UserID int64  `json:"user_id"`
	Status string `json:"status"`
}

type ContentModerationDeleteHashResult struct {
	InputHash string `json:"input_hash"`
	Deleted   bool   `json:"deleted"`
}

type ContentModerationClearHashesResult struct {
	Deleted int64 `json:"deleted"`
}

type ContentModerationRepository interface {
	CreateLog(ctx context.Context, log *ContentModerationLog) error
	ListLogs(ctx context.Context, filter ContentModerationLogFilter) ([]ContentModerationLog, *pagination.PaginationResult, error)
	CountFlaggedByUserSince(ctx context.Context, userID int64, since time.Time) (int, error)
	CleanupExpiredLogs(ctx context.Context, hitBefore time.Time, nonHitBefore time.Time) (*ContentModerationCleanupResult, error)
}

type ContentModerationAccountShareModeResolver interface {
	IsModeGroup(ctx context.Context, groupID int64) bool
	// IsModeGroupChecked 必须区分"不是模式分组"与"查询失败"，供缓存层判断结果是否可缓存。
	IsModeGroupChecked(ctx context.Context, groupID int64) (bool, error)
	ResolveActiveBindingForRequest(ctx context.Context, userID, apiKeyID, groupID int64) (*AccountShareMembership, *AccountShareListing, error)
}

type ContentModerationScopeContext struct {
	ScopeType             string
	AccountShareListingID *int64
	AccountID             *int64
	OwnerUserID           *int64
	ConsumerUserID        *int64
	MembershipID          *int64
}

type ContentModerationHashCache interface {
	RecordFlaggedInputHash(ctx context.Context, inputHash string) error
	HasFlaggedInputHash(ctx context.Context, inputHash string) (bool, error)
	DeleteFlaggedInputHash(ctx context.Context, inputHash string) (bool, error)
	ClearFlaggedInputHashes(ctx context.Context) (int64, error)
	CountFlaggedInputHashes(ctx context.Context) (int64, error)
	GetUserTrustState(ctx context.Context, userID int64) (*ContentModerationUserTrustState, error)
	SetUserTrustState(ctx context.Context, userID int64, state *ContentModerationUserTrustState, ttl time.Duration) error
	UpdateUserTrustState(ctx context.Context, userID int64, ttl time.Duration, mutate ContentModerationUserTrustStateMutator) (*ContentModerationUserTrustState, error)
}

type ContentModerationUserAPIKeyHashChecker interface {
	APIKeyHashExists(ctx context.Context, apiKeyHash string, excludeOwnerUserID, excludeAccountID int64) (bool, error)
}

type ContentModerationService struct {
	settingRepo               SettingRepository
	repo                      ContentModerationRepository
	hashCache                 ContentModerationHashCache
	groupRepo                 GroupRepository
	accountShareModeResolver  ContentModerationAccountShareModeResolver
	userAPIKeyHashChecker     ContentModerationUserAPIKeyHashChecker
	userRepo                  UserRepository
	authCacheInvalidator      APIKeyAuthCacheInvalidator
	emailService              *EmailService
	systemNoticeService       *SystemNoticeService
	httpClient                *http.Client
	asyncQueue                chan contentModerationTask
	workerCount               int
	apiKeyCursor              atomic.Uint64
	asyncActive               atomic.Int64
	asyncEnqueued             atomic.Int64
	asyncDropped              atomic.Int64
	asyncProcessed            atomic.Int64
	asyncErrors               atomic.Int64
	dynamicSamplingSkipped    atomic.Int64
	dynamicSamplingForced     atomic.Int64
	dynamicSamplingSampled    atomic.Int64
	dynamicSamplingAudited    atomic.Int64
	dynamicSamplingRiskEvents atomic.Int64
	lastCleanupUnix           atomic.Int64
	lastCleanupDeletedHit     atomic.Int64
	lastCleanupDeletedNonHit  atomic.Int64
	runtimeSnapshot           atomic.Pointer[contentModerationRuntimeSnapshot]
	runtimeRefreshMu          sync.Mutex
	runtimeCacheTTL           time.Duration
	runtimeRefreshRetryAt     atomic.Int64
	keyHealthMu               sync.Mutex
	keyHealth                 map[string]*contentModerationKeyHealth
	modeGroupCacheMu          sync.Mutex
	modeGroupCache            map[int64]contentModerationModeGroupCacheEntry
	emailThrottleMu           sync.Mutex
	emailThrottle             map[int64]time.Time
	emailDispatchSlots        chan struct{}
	warnThrottleMu            sync.Mutex
	warnThrottle              map[string]time.Time
	clusterCache              *ClusterCacheCoordinator
	taskExecutor              *ClusterTaskExecutor
	cancelCleanup             context.CancelFunc
	cleanupStopOnce           sync.Once
	cleanupWG                 sync.WaitGroup
}

type contentModerationRuntimeSnapshot struct {
	riskControlEnabled bool
	config             *ContentModerationConfig
	configDigest       [sha256.Size]byte
	loadedAt           time.Time
}

type contentModerationTask struct {
	input      ContentModerationCheckInput
	content    ContentModerationInput
	scope      ContentModerationScopeContext
	inputHash  string
	sampling   *ContentModerationDynamicSamplingDecision
	enqueuedAt time.Time
}

type contentModerationModeGroupCacheEntry struct {
	value     bool
	expiresAt time.Time
}

type contentModerationKeyHealth struct {
	Hash           string
	Masked         string
	FailureCount   int
	SuccessCount   int64
	LastError      string
	LastCheckedAt  time.Time
	FrozenUntil    time.Time
	LastLatencyMS  int
	LastHTTPStatus int
	LastTested     bool
}

func NewContentModerationService(
	settingRepo SettingRepository,
	repo ContentModerationRepository,
	hashCache ContentModerationHashCache,
	groupRepo GroupRepository,
	userRepo UserRepository,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	emailService *EmailService,
	taskExecutors ...*ClusterTaskExecutor,
) *ContentModerationService {
	cleanupCtx, cancelCleanup := context.WithCancel(context.Background())
	svc := &ContentModerationService{
		settingRepo:          settingRepo,
		repo:                 repo,
		hashCache:            hashCache,
		groupRepo:            groupRepo,
		userRepo:             userRepo,
		authCacheInvalidator: authCacheInvalidator,
		emailService:         emailService,
		httpClient:           servertiming.InstrumentClient(nil),
		workerCount:          maxContentModerationWorkerCount,
		asyncQueue:           make(chan contentModerationTask, maxContentModerationQueueSize),
		keyHealth:            make(map[string]*contentModerationKeyHealth),
		modeGroupCache:       make(map[int64]contentModerationModeGroupCacheEntry),
		emailThrottle:        make(map[int64]time.Time),
		emailDispatchSlots:   make(chan struct{}, contentModerationEmailDispatchLimit),
		warnThrottle:         make(map[string]time.Time),
		cancelCleanup:        cancelCleanup,
	}
	if len(taskExecutors) > 0 {
		svc.taskExecutor = taskExecutors[0]
	}
	if settingRepo != nil && repo != nil {
		for i := 0; i < svc.workerCount; i++ {
			go svc.worker(i)
		}
		svc.cleanupWG.Add(1)
		go svc.cleanupWorker(cleanupCtx)
	}
	return svc
}

func (s *ContentModerationService) SetSystemNoticeService(noticeService *SystemNoticeService) {
	if s == nil {
		return
	}
	s.systemNoticeService = noticeService
}

func (s *ContentModerationService) SetClusterCacheCoordinator(coordinator *ClusterCacheCoordinator) {
	if s != nil {
		s.clusterCache = coordinator
	}
}

func (s *ContentModerationService) SetAccountShareModeResolver(resolver ContentModerationAccountShareModeResolver) {
	if s == nil {
		return
	}
	s.accountShareModeResolver = resolver
}

func (s *ContentModerationService) SetUserAPIKeyHashChecker(checker ContentModerationUserAPIKeyHashChecker) {
	if s == nil {
		return
	}
	s.userAPIKeyHashChecker = checker
}

func (s *ContentModerationService) GetConfig(ctx context.Context) (*ContentModerationConfigView, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	return s.configView(cfg), nil
}

func (s *ContentModerationService) UpdateConfig(ctx context.Context, input UpdateContentModerationConfigInput) (*ContentModerationConfigView, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	if input.Enabled != nil {
		cfg.Enabled = *input.Enabled
	}
	if input.CyberPreflightEnabled != nil {
		cfg.CyberPreflightEnabled = *input.CyberPreflightEnabled
	}
	if input.CyberPreflightRules != nil {
		cfg.CyberPreflightRules = *input.CyberPreflightRules
	}
	if input.Mode != nil {
		cfg.Mode = strings.TrimSpace(*input.Mode)
	}
	if input.Provider != nil {
		cfg.Provider = strings.TrimSpace(*input.Provider)
	}
	if input.BaseURL != nil {
		cfg.BaseURL = strings.TrimSpace(*input.BaseURL)
	}
	if input.Model != nil {
		cfg.Model = strings.TrimSpace(*input.Model)
	}
	if input.TimeoutMS != nil {
		cfg.TimeoutMS = *input.TimeoutMS
	}
	if input.SampleRate != nil {
		cfg.SampleRate = *input.SampleRate
	}
	if input.DynamicSampling != nil {
		cfg.DynamicSampling = *input.DynamicSampling
	}
	if input.WorkerCount != nil {
		cfg.WorkerCount = *input.WorkerCount
	}
	if input.QueueSize != nil {
		cfg.QueueSize = *input.QueueSize
	}
	if input.BlockStatus != nil {
		cfg.BlockStatus = *input.BlockStatus
	}
	if input.BlockMessage != nil {
		cfg.BlockMessage = strings.TrimSpace(*input.BlockMessage)
	}
	if input.EmailOnHit != nil {
		cfg.EmailOnHit = *input.EmailOnHit
	}
	if input.AutoBanEnabled != nil {
		cfg.AutoBanEnabled = *input.AutoBanEnabled
	}
	if input.BanThreshold != nil {
		cfg.BanThreshold = *input.BanThreshold
	}
	if input.ViolationWindowHours != nil {
		cfg.ViolationWindowHours = *input.ViolationWindowHours
	}
	if input.RetryCount != nil {
		cfg.RetryCount = *input.RetryCount
	}
	if input.HitRetentionDays != nil {
		cfg.HitRetentionDays = *input.HitRetentionDays
	}
	if input.NonHitRetentionDays != nil {
		cfg.NonHitRetentionDays = *input.NonHitRetentionDays
	}
	if input.PreHashCheckEnabled != nil {
		cfg.PreHashCheckEnabled = *input.PreHashCheckEnabled
	}
	if input.AccountShareModeScope != nil {
		cfg.AccountShareModeScope = *input.AccountShareModeScope
	}
	if input.AllGroups != nil {
		cfg.AllGroups = *input.AllGroups
	}
	if input.GroupIDs != nil {
		cfg.GroupIDs = normalizeInt64IDs(*input.GroupIDs)
	}
	if input.RecordNonHits != nil {
		cfg.RecordNonHits = *input.RecordNonHits
	}
	if input.ClearAPIKey {
		cfg.APIKey = ""
		cfg.APIKeys = []string{}
	} else {
		apiKeysMode := normalizeContentModerationAPIKeysMode(input.APIKeysMode)
		if input.DeleteAPIKeyHashes != nil && apiKeysMode != contentModerationAPIKeysModeReplace {
			cfg.APIKeys = deleteModerationAPIKeysByHash(cfg.apiKeys(), *input.DeleteAPIKeyHashes)
			cfg.APIKey = ""
		}
		if input.APIKeys != nil {
			if err := s.ensureAPIKeysNotUsedByUserModeration(ctx, *input.APIKeys); err != nil {
				return nil, err
			}
			if apiKeysMode == contentModerationAPIKeysModeReplace {
				cfg.APIKeys = normalizeModerationAPIKeys(*input.APIKeys)
			} else {
				cfg.APIKeys = normalizeModerationAPIKeys(append(cfg.apiKeys(), *input.APIKeys...))
			}
			cfg.APIKey = ""
		}
		if input.APIKey != nil && strings.TrimSpace(*input.APIKey) != "" {
			if err := s.ensureAPIKeysNotUsedByUserModeration(ctx, []string{*input.APIKey}); err != nil {
				return nil, err
			}
			cfg.APIKeys = normalizeModerationAPIKeys(append(cfg.APIKeys, *input.APIKey))
			cfg.APIKey = ""
		}
	}
	if err := s.validateConfig(ctx, cfg); err != nil {
		return nil, err
	}
	cfg.normalize()
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal content moderation config: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyContentModerationConfig, string(raw)); err != nil {
		return nil, fmt.Errorf("save content moderation config: %w", err)
	}
	if s.clusterCache != nil {
		if err := s.clusterCache.Advance(ctx, ClusterCacheKeyPolicyMetadata); err != nil {
			slog.Error("failed to advance cluster policy metadata cache version", "error", err)
		}
	}
	s.replaceRuntimeConfig(raw)
	return s.configView(cfg), nil
}

func (s *ContentModerationService) TestAPIKeys(ctx context.Context, input TestContentModerationAPIKeysInput) (*TestContentModerationAPIKeysResult, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	keys := normalizeModerationAPIKeys(input.APIKeys)
	configured := false
	if len(keys) == 0 {
		keys = cfg.apiKeys()
		configured = true
	}
	if strings.TrimSpace(input.BaseURL) != "" {
		cfg.BaseURL = input.BaseURL
	}
	if strings.TrimSpace(input.Provider) != "" {
		cfg.Provider = input.Provider
	}
	if strings.TrimSpace(input.Model) != "" {
		cfg.Model = input.Model
	}
	if input.TimeoutMS > 0 {
		cfg.TimeoutMS = input.TimeoutMS
	}
	cfg.normalize()
	testInput, imageCount, err := buildModerationTestInput(input.Prompt, input.Images)
	if err != nil {
		return nil, err
	}
	auditOnly := contentModerationTestHasAuditInput(input.Prompt, input.Images)
	if configured && auditOnly {
		key, ok := s.nextUsableAPIKey(cfg)
		if !ok {
			return &TestContentModerationAPIKeysResult{
				Items:      s.apiKeyStatuses(keys),
				ImageCount: imageCount,
			}, nil
		}
		keys = []string{key}
	}
	if len(keys) == 0 {
		return &TestContentModerationAPIKeysResult{Items: []ContentModerationAPIKeyStatus{}, ImageCount: imageCount}, nil
	}
	items := make([]ContentModerationAPIKeyStatus, 0, len(keys))
	var auditResult *ContentModerationTestAuditResult
	for idx, key := range keys {
		start := time.Now()
		httpStatus := 0
		result, err := s.callModerationOnceWithContent(ctx, cfg, key, testInput, &httpStatus)
		latency := int(time.Since(start).Milliseconds())
		keyHash := moderationAPIKeyHash(key)
		if err != nil {
			s.markAPIKeyError(key, err.Error(), latency, httpStatus)
		} else {
			s.markAPIKeySuccess(key, latency, httpStatus)
			if auditResult == nil {
				auditResult = buildContentModerationTestAuditResult(result, cfg.Thresholds)
			}
		}
		status := s.apiKeyStatusForHash(idx, keyHash, maskSecretTail(key), configured)
		status.LastTested = true
		items = append(items, status)
	}
	return &TestContentModerationAPIKeysResult{Items: items, AuditResult: auditResult, ImageCount: imageCount}, nil
}

func (s *ContentModerationService) Check(ctx context.Context, input ContentModerationCheckInput) (*ContentModerationDecision, error) {
	allow := &ContentModerationDecision{Allowed: true, Action: ContentModerationActionAllow}
	if s == nil || s.settingRepo == nil || s.repo == nil {
		slog.Debug("content_moderation.skip_unavailable",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol)
		return allow, nil
	}
	runtimeSnapshot, err := s.loadRuntimeSnapshot(ctx)
	if err != nil {
		s.warnThrottled("config_load_failed", "content_moderation.skip_config_load_failed",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"error", err)
		return allow, nil
	}
	if !runtimeSnapshot.riskControlEnabled {
		slog.Debug("content_moderation.skip_feature_disabled",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol)
		return allow, nil
	}
	cfg := runtimeSnapshot.config
	inScope, scopeCtx := s.resolveScope(ctx, cfg, input)
	slog.Debug("content_moderation.config_loaded",
		"user_id", input.UserID,
		"api_key_id", input.APIKeyID,
		"group_id", contentModerationLogGroupID(input.GroupID),
		"group_name", input.GroupName,
		"endpoint", input.Endpoint,
		"provider", input.Provider,
		"protocol", input.Protocol,
		"model", input.Model,
		"enabled", cfg.Enabled,
		"mode", cfg.Mode,
		"audit_provider", cfg.Provider,
		"all_groups", cfg.AllGroups,
		"configured_group_ids", cfg.GroupIDs,
		"account_share_scope_enabled", cfg.AccountShareModeScope.Enabled,
		"scope_type", scopeCtx.ScopeType,
		"in_scope", inScope,
		"sample_rate", cfg.SampleRate,
		"dynamic_sampling_enabled", cfg.DynamicSampling.Enabled,
		"api_key_count", len(cfg.apiKeys()),
		"pre_hash_check_enabled", cfg.PreHashCheckEnabled,
		"record_non_hits", cfg.RecordNonHits)
	if !cfg.Enabled {
		slog.Debug("content_moderation.skip_config_disabled",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol)
		return allow, nil
	}
	if cfg.Mode == ContentModerationModeOff {
		slog.Debug("content_moderation.skip_mode_off",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol)
		return allow, nil
	}
	if !inScope {
		slog.Debug("content_moderation.skip_group_out_of_scope",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"group_name", input.GroupName,
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"all_groups", cfg.AllGroups,
			"configured_group_ids", cfg.GroupIDs,
			"account_share_scope_enabled", cfg.AccountShareModeScope.Enabled,
			"scope_type", scopeCtx.ScopeType)
		return allow, nil
	}
	var content ContentModerationInput
	if input.Content != nil {
		content = input.Content.Clone()
	} else if input.ContentSource != nil {
		content = input.ContentSource.ContentModerationInputCopy()
	} else {
		content = ExtractContentModerationInput(input.Protocol, input.Body)
	}
	if content.IsEmpty() {
		slog.Debug("content_moderation.skip_empty_input",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"body_bytes", len(input.Body))
		return allow, nil
	}
	content.Normalize()
	slog.Debug("content_moderation.input_extracted",
		"user_id", input.UserID,
		"api_key_id", input.APIKeyID,
		"group_id", contentModerationLogGroupID(input.GroupID),
		"endpoint", input.Endpoint,
		"protocol", input.Protocol,
		"text_runes", len([]rune(content.Text)),
		"image_count", len(content.Images))
	hashText := content.Hash()
	if cfg.PreHashCheckEnabled && s.hashCache != nil {
		matched, err := s.hashCache.HasFlaggedInputHash(ctx, hashText)
		if err != nil {
			s.warnThrottled("hash_check_failed", "content_moderation.hash_check_failed", "user_id", input.UserID, "endpoint", input.Endpoint, "error", err)
		}
		if matched {
			slog.Info("content_moderation.hash_block",
				"user_id", input.UserID,
				"api_key_id", input.APIKeyID,
				"group_id", contentModerationLogGroupID(input.GroupID),
				"endpoint", input.Endpoint,
				"protocol", input.Protocol,
				"input_hash", hashText)
			s.recordDynamicSamplingRiskEvent(ctx, cfg, input, "flagged_hash")
			message := cfg.BlockMessage
			if message != "" {
				message = fmt.Sprintf("%s（hash: %s）", message, hashText)
			}
			return &ContentModerationDecision{
				Allowed:    false,
				Blocked:    true,
				Flagged:    true,
				Message:    message,
				StatusCode: cfg.BlockStatus,
				InputHash:  hashText,
				Action:     ContentModerationActionHashBlock,
			}, nil
		}
	}
	var samplingDecision *ContentModerationDynamicSamplingDecision
	if len(cfg.apiKeys()) == 0 {
		s.warnThrottled("no_audit_api_keys", "content_moderation.skip_no_audit_api_keys",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol)
		return allow, nil
	}
	if cfg.DynamicSampling.Enabled && cfg.Mode != ContentModerationModeObserve {
		samplingDecision, err = s.resolveDynamicSamplingDecision(ctx, cfg, input, content, scopeCtx, hashText)
		if err != nil {
			s.warnThrottled("dynamic_sampling_failed", "content_moderation.dynamic_sampling_failed",
				"user_id", input.UserID,
				"api_key_id", input.APIKeyID,
				"group_id", contentModerationLogGroupID(input.GroupID),
				"endpoint", input.Endpoint,
				"protocol", input.Protocol,
				"error", err)
			s.dynamicSamplingForced.Add(1)
			samplingDecision = &ContentModerationDynamicSamplingDecision{
				ShouldAudit:         true,
				EffectiveSampleRate: 100,
				TrustLevel:          ContentModerationTrustLevelNew,
				Reason:              "state_error",
				Forced:              true,
			}
		}
		if samplingDecision != nil && !samplingDecision.ShouldAudit {
			slog.Debug("content_moderation.dynamic_sampling_skip",
				"user_id", input.UserID,
				"api_key_id", input.APIKeyID,
				"group_id", contentModerationLogGroupID(input.GroupID),
				"endpoint", input.Endpoint,
				"protocol", input.Protocol,
				"trust_level", samplingDecision.TrustLevel,
				"sample_rate", samplingDecision.EffectiveSampleRate,
				"reason", samplingDecision.Reason)
			return allow, nil
		}
	} else if !cfg.DynamicSampling.Enabled && !cfg.shouldSample(hashText) {
		slog.Debug("content_moderation.skip_sample_rate",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"sample_rate", cfg.SampleRate)
		return allow, nil
	}
	if cfg.Mode == ContentModerationModeObserve {
		slog.Debug("content_moderation.enqueue_observe",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"queue_len", len(s.asyncQueue))
		s.enqueueAsync(input, cfg, content, scopeCtx, hashText, nil)
		return allow, nil
	}

	return s.checkSync(ctx, input, cfg, content, scopeCtx, hashText, samplingDecision, nil, true), nil
}

func (s *ContentModerationService) checkSync(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, scopeCtx ContentModerationScopeContext, hashText string, samplingDecision *ContentModerationDynamicSamplingDecision, queueDelay *int, allowBlock bool) *ContentModerationDecision {
	allow := &ContentModerationDecision{Allowed: true, Action: ContentModerationActionAllow}
	start := time.Now()
	result, err := s.callModeration(ctx, cfg, content)
	latency := int(time.Since(start).Milliseconds())
	if err != nil {
		s.warnThrottled("audit_api_failed", "content_moderation.audit_api_failed",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"mode", cfg.Mode,
			"allow_block", allowBlock,
			"queue_delay_ms", queueDelay,
			"latency_ms", latency,
			"error", err)
		if queueDelay != nil {
			s.asyncErrors.Add(1)
		}
		if cfg.RecordNonHits || errors.Is(err, ErrContentModerationUnsupportedInput) {
			log := s.buildLog(input, cfg, scopeCtx, ContentModerationActionError, false, "", 0, nil, content.ExcerptText(), &latency, queueDelay, err.Error())
			_ = s.repo.CreateLog(ctx, log)
		}
		s.recordDynamicSamplingAuditError(ctx, cfg, input, samplingDecision)
		return allow
	}

	flagged, highestCategory, highestScore := result.Flagged, result.HighestCategory, result.HighestScore
	action := ContentModerationActionAllow
	blocked := false
	if allowBlock && flagged && cfg.Mode == ContentModerationModePreBlock {
		action = ContentModerationActionBlock
		blocked = true
	}
	// 未命中的审核结果每个被审请求都会产生一条，只在 Debug 保留；
	// 命中是低频且可行动的事件，保持 Info。
	auditResultLog := slog.Debug
	if flagged {
		auditResultLog = slog.Info
	}
	auditResultLog("content_moderation.audit_result",
		"user_id", input.UserID,
		"api_key_id", input.APIKeyID,
		"group_id", contentModerationLogGroupID(input.GroupID),
		"group_name", input.GroupName,
		"endpoint", input.Endpoint,
		"protocol", input.Protocol,
		"mode", cfg.Mode,
		"allow_block", allowBlock,
		"flagged", flagged,
		"blocked", blocked,
		"action", action,
		"highest_category", highestCategory,
		"highest_score", highestScore,
		"latency_ms", latency,
		"queue_delay_ms", queueDelay)
	if flagged || cfg.RecordNonHits {
		log := s.buildLog(input, cfg, scopeCtx, action, flagged, highestCategory, highestScore, result.CategoryScores, content.ExcerptText(), &latency, queueDelay, "")
		if flagged && s.hashCache != nil {
			if err := s.hashCache.RecordFlaggedInputHash(ctx, hashText); err != nil {
				s.warnThrottled("record_hash_failed", "content_moderation.record_hash_failed", "user_id", input.UserID, "endpoint", input.Endpoint, "error", err)
			}
		}
		s.applyFlaggedSideEffects(ctx, cfg, log)
		_ = s.repo.CreateLog(ctx, log)
	}
	s.recordDynamicSamplingAuditResult(ctx, cfg, input, samplingDecision, flagged)
	if blocked {
		decision := &ContentModerationDecision{
			Allowed:         false,
			Blocked:         true,
			Flagged:         true,
			Message:         cfg.BlockMessage,
			StatusCode:      cfg.BlockStatus,
			HighestCategory: highestCategory,
			HighestScore:    highestScore,
			CategoryScores:  result.CategoryScores,
			Action:          action,
		}
		s.notifyRiskControlBlocked(ctx, input, decision)
		return decision
	}
	return &ContentModerationDecision{
		Allowed:         true,
		Flagged:         flagged,
		Message:         "",
		HighestCategory: highestCategory,
		HighestScore:    highestScore,
		CategoryScores:  result.CategoryScores,
		Action:          action,
	}
}

func (s *ContentModerationService) enqueueAsync(input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, scopeCtx ContentModerationScopeContext, hashText string, samplingDecision *ContentModerationDynamicSamplingDecision) {
	if s == nil || s.asyncQueue == nil {
		return
	}
	queueSize := defaultContentModerationQueueSize
	if cfg != nil && cfg.QueueSize > 0 {
		queueSize = cfg.QueueSize
	}
	if len(s.asyncQueue) >= queueSize {
		s.warnThrottled("async_queue_full", "content_moderation.async_queue_full", "user_id", input.UserID, "endpoint", input.Endpoint, "queue_size", queueSize)
		s.asyncDropped.Add(1)
		return
	}
	input.Body = nil
	input.Content = nil
	input.ContentSource = nil
	task := contentModerationTask{
		input:      input,
		content:    content,
		scope:      scopeCtx,
		inputHash:  hashText,
		sampling:   samplingDecision,
		enqueuedAt: time.Now(),
	}
	select {
	case s.asyncQueue <- task:
		s.asyncEnqueued.Add(1)
	default:
		s.warnThrottled("async_queue_full", "content_moderation.async_queue_full", "user_id", input.UserID, "endpoint", input.Endpoint)
		s.asyncDropped.Add(1)
	}
}

func (s *ContentModerationService) worker(id int) {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), maxContentModerationTimeoutMS*time.Millisecond+10*time.Second)
		runtimeSnapshot, err := s.loadRuntimeSnapshot(ctx)
		if err != nil || runtimeSnapshot == nil || runtimeSnapshot.config == nil {
			cancel()
			time.Sleep(time.Second)
			continue
		}
		cfg := runtimeSnapshot.config
		if !runtimeSnapshot.riskControlEnabled || !cfg.Enabled || cfg.Mode == ContentModerationModeOff || len(cfg.apiKeys()) == 0 || id >= cfg.WorkerCount {
			cancel()
			time.Sleep(time.Second)
			continue
		}
		task, ok := s.dequeueAsyncTask(ctx, time.Second)
		if !ok {
			cancel()
			continue
		}
		func() {
			defer cancel()
			defer func() {
				if r := recover(); r != nil {
					slog.Error("content_moderation.worker_panic", "worker_id", id, "recover", r)
				}
			}()
			inScope, scopeCtx := s.resolveScope(ctx, cfg, task.input)
			if !inScope {
				return
			}
			if cfg.DynamicSampling.Enabled {
				samplingDecision, err := s.resolveDynamicSamplingDecision(ctx, cfg, task.input, task.content, scopeCtx, task.inputHash)
				if err != nil {
					s.warnThrottled("dynamic_sampling_failed", "content_moderation.dynamic_sampling_failed",
						"user_id", task.input.UserID,
						"api_key_id", task.input.APIKeyID,
						"group_id", contentModerationLogGroupID(task.input.GroupID),
						"endpoint", task.input.Endpoint,
						"protocol", task.input.Protocol,
						"error", err)
					s.dynamicSamplingForced.Add(1)
					samplingDecision = &ContentModerationDynamicSamplingDecision{
						ShouldAudit:         true,
						EffectiveSampleRate: 100,
						TrustLevel:          ContentModerationTrustLevelNew,
						Reason:              "state_error",
						Forced:              true,
					}
				}
				if samplingDecision != nil && !samplingDecision.ShouldAudit {
					slog.Debug("content_moderation.dynamic_sampling_skip",
						"user_id", task.input.UserID,
						"api_key_id", task.input.APIKeyID,
						"group_id", contentModerationLogGroupID(task.input.GroupID),
						"endpoint", task.input.Endpoint,
						"protocol", task.input.Protocol,
						"trust_level", samplingDecision.TrustLevel,
						"sample_rate", samplingDecision.EffectiveSampleRate,
						"reason", samplingDecision.Reason)
					s.asyncProcessed.Add(1)
					return
				}
				task.sampling = samplingDecision
			}
			s.asyncActive.Add(1)
			defer s.asyncActive.Add(-1)
			queueDelay := int(time.Since(task.enqueuedAt).Milliseconds())
			task.scope = scopeCtx
			_ = s.checkSync(ctx, task.input, cfg, task.content, task.scope, task.inputHash, task.sampling, &queueDelay, false)
			s.asyncProcessed.Add(1)
		}()
	}
}

func (s *ContentModerationService) dequeueAsyncTask(ctx context.Context, idleWait time.Duration) (contentModerationTask, bool) {
	var zero contentModerationTask
	if s == nil || s.asyncQueue == nil {
		return zero, false
	}
	if idleWait <= 0 {
		idleWait = time.Second
	}
	timer := time.NewTimer(idleWait)
	defer timer.Stop()
	select {
	case task, ok := <-s.asyncQueue:
		return task, ok
	case <-ctx.Done():
		return zero, false
	case <-timer.C:
		return zero, false
	}
}

func (s *ContentModerationService) ListLogs(ctx context.Context, filter ContentModerationLogFilter) ([]ContentModerationLog, *pagination.PaginationResult, error) {
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

func (s *ContentModerationService) UnbanUser(ctx context.Context, userID int64) (*ContentModerationUnbanUserResult, error) {
	if s == nil || s.userRepo == nil {
		return nil, infraerrors.InternalServer("CONTENT_MODERATION_USER_REPOSITORY_UNAVAILABLE", "用户仓储不可用")
	}
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER_ID", "用户 ID 无效")
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, infraerrors.NotFound("USER_NOT_FOUND", "用户不存在")
		}
		return nil, fmt.Errorf("get content moderation unban user: %w", err)
	}
	if user.Status != StatusActive {
		user.Status = StatusActive
		if err := s.userRepo.Update(ctx, user); err != nil {
			return nil, fmt.Errorf("update content moderation unban user: %w", err)
		}
	}
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	s.notifyRiskControlUnbanned(ctx, userID)
	return &ContentModerationUnbanUserResult{
		UserID: userID,
		Status: StatusActive,
	}, nil
}

func (s *ContentModerationService) DeleteFlaggedInputHash(ctx context.Context, inputHash string) (*ContentModerationDeleteHashResult, error) {
	inputHash = normalizeContentModerationHash(inputHash)
	if inputHash == "" {
		return nil, infraerrors.BadRequest("INVALID_CONTENT_MODERATION_HASH", "风险输入哈希无效")
	}
	if s == nil || s.hashCache == nil {
		return nil, infraerrors.InternalServer("CONTENT_MODERATION_HASH_CACHE_UNAVAILABLE", "内容审计哈希缓存不可用")
	}
	deleted, err := s.hashCache.DeleteFlaggedInputHash(ctx, inputHash)
	if err != nil {
		return nil, fmt.Errorf("delete content moderation flagged hash: %w", err)
	}
	return &ContentModerationDeleteHashResult{
		InputHash: inputHash,
		Deleted:   deleted,
	}, nil
}

func (s *ContentModerationService) ClearFlaggedInputHashes(ctx context.Context) (*ContentModerationClearHashesResult, error) {
	if s == nil || s.hashCache == nil {
		return nil, infraerrors.InternalServer("CONTENT_MODERATION_HASH_CACHE_UNAVAILABLE", "内容审计哈希缓存不可用")
	}
	deleted, err := s.hashCache.ClearFlaggedInputHashes(ctx)
	if err != nil {
		return nil, fmt.Errorf("clear content moderation flagged hashes: %w", err)
	}
	return &ContentModerationClearHashesResult{Deleted: deleted}, nil
}

func (s *ContentModerationService) GetStatus(ctx context.Context) (*ContentModerationRuntimeStatus, error) {
	if s == nil {
		return &ContentModerationRuntimeStatus{}, nil
	}
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	riskEnabled := s.isRiskControlEnabled(ctx)
	active := int(s.asyncActive.Load())
	if active < 0 {
		active = 0
	}
	if active > cfg.WorkerCount {
		active = cfg.WorkerCount
	}
	queueLength := 0
	if s.asyncQueue != nil {
		queueLength = len(s.asyncQueue)
	}
	queueUsage := 0.0
	if cfg.QueueSize > 0 {
		queueUsage = float64(queueLength) * 100 / float64(cfg.QueueSize)
	}
	var flaggedHashCount int64
	if s.hashCache != nil {
		if n, err := s.hashCache.CountFlaggedInputHashes(ctx); err == nil {
			flaggedHashCount = n
		} else {
			slog.Warn("content_moderation.hash_count_failed", "error", err)
		}
	}
	var lastCleanupAt *time.Time
	if unix := s.lastCleanupUnix.Load(); unix > 0 {
		t := time.Unix(unix, 0)
		lastCleanupAt = &t
	}
	return &ContentModerationRuntimeStatus{
		Enabled:               cfg.Enabled,
		CyberPreflightEnabled: cfg.CyberPreflightEnabled,
		RiskControlEnabled:    riskEnabled,
		Mode:                  cfg.Mode,
		WorkerCount:           cfg.WorkerCount,
		MaxWorkers:            maxContentModerationWorkerCount,
		ActiveWorkers:         active,
		IdleWorkers:           cfg.WorkerCount - active,
		QueueSize:             cfg.QueueSize,
		QueueLength:           queueLength,
		QueueUsagePercent:     queueUsage,
		Enqueued:              s.asyncEnqueued.Load(),
		Dropped:               s.asyncDropped.Load(),
		Processed:             s.asyncProcessed.Load(),
		Errors:                s.asyncErrors.Load(),
		DynamicSampling: ContentModerationDynamicSamplingRuntimeStatus{
			Enabled:    cfg.DynamicSampling.Enabled,
			Skipped:    s.dynamicSamplingSkipped.Load(),
			Forced:     s.dynamicSamplingForced.Load(),
			Sampled:    s.dynamicSamplingSampled.Load(),
			Audited:    s.dynamicSamplingAudited.Load(),
			RiskEvents: s.dynamicSamplingRiskEvents.Load(),
		},
		APIKeyStatuses:           s.apiKeyStatuses(cfg.apiKeys()),
		FlaggedHashCount:         flaggedHashCount,
		LastCleanupAt:            lastCleanupAt,
		LastCleanupDeletedHit:    s.lastCleanupDeletedHit.Load(),
		LastCleanupDeletedNonHit: s.lastCleanupDeletedNonHit.Load(),
	}, nil
}

func (s *ContentModerationService) cleanupWorker(ctx context.Context) {
	defer s.cleanupWG.Done()
	timer := time.NewTimer(contentModerationCleanupDelay)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			s.runCleanupOnceWithContext(ctx)
			timer.Reset(contentModerationCleanupInterval)
		case <-ctx.Done():
			return
		}
	}
}

func (s *ContentModerationService) runCleanupOnce() {
	s.runCleanupOnceWithContext(context.Background())
}

func (s *ContentModerationService) runCleanupOnceWithContext(parent context.Context) {
	if s == nil || s.repo == nil || s.settingRepo == nil {
		return
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, contentModerationCleanupTimeout)
	defer cancel()

	run := func(taskCtx context.Context, guard *ClusterLeaseGuard) error {
		cfg, err := s.loadConfig(taskCtx)
		if err != nil {
			return fmt.Errorf("load content moderation cleanup config: %w", err)
		}
		now := time.Now()
		hitBefore := now.AddDate(0, 0, -cfg.HitRetentionDays)
		nonHitBefore := now.AddDate(0, 0, -cfg.NonHitRetentionDays)
		if err := guard.Check(taskCtx); err != nil {
			return err
		}
		result, err := s.repo.CleanupExpiredLogs(taskCtx, hitBefore, nonHitBefore)
		if err != nil {
			return err
		}
		if result == nil {
			return nil
		}
		s.lastCleanupUnix.Store(result.FinishedAt.Unix())
		s.lastCleanupDeletedHit.Store(result.DeletedHit)
		s.lastCleanupDeletedNonHit.Store(result.DeletedNonHit)
		return nil
	}

	var err error
	if s.taskExecutor == nil {
		err = run(ctx, &ClusterLeaseGuard{})
	} else {
		_, err = s.taskExecutor.Run(ctx, contentModerationCleanupTaskName, run)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		slog.Warn("content_moderation.cleanup_failed", "error", err)
	}
}

// StopCleanupWorker 停止日志保留清理循环；异步审核 worker 仍由请求队列生命周期管理。
func (s *ContentModerationService) StopCleanupWorker() {
	if s == nil {
		return
	}
	s.cleanupStopOnce.Do(func() {
		if s.cancelCleanup != nil {
			s.cancelCleanup()
		}
	})
	s.cleanupWG.Wait()
}

func (s *ContentModerationService) loadConfig(ctx context.Context) (*ContentModerationConfig, error) {
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyContentModerationConfig)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return parseContentModerationConfig("")
		}
		return nil, fmt.Errorf("get content moderation config: %w", err)
	}
	return parseContentModerationConfig(raw)
}

func parseContentModerationConfig(raw string) (*ContentModerationConfig, error) {
	cfg := defaultContentModerationConfig()
	if strings.TrimSpace(raw) == "" {
		cfg.normalize()
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), cfg); err != nil {
		return nil, infraerrors.BadRequest("INVALID_CONTENT_MODERATION_CONFIG", "内容审计配置不是有效 JSON")
	}
	cfg.normalize()
	return cfg, nil
}

func (s *ContentModerationService) loadRuntimeSnapshot(ctx context.Context) (*contentModerationRuntimeSnapshot, error) {
	if s == nil || s.settingRepo == nil {
		return nil, errors.New("content moderation setting repository unavailable")
	}
	now := time.Now()
	if snapshot := s.runtimeSnapshot.Load(); snapshot != nil {
		if now.Sub(snapshot.loadedAt) < s.runtimeSnapshotTTL() {
			return snapshot, nil
		}
		s.triggerRuntimeSnapshotRefresh()
		return snapshot, nil
	}

	s.runtimeRefreshMu.Lock()
	defer s.runtimeRefreshMu.Unlock()
	if snapshot := s.runtimeSnapshot.Load(); snapshot != nil {
		return snapshot, nil
	}
	return s.refreshRuntimeSnapshot(ctx)
}

func (s *ContentModerationService) runtimeSnapshotTTL() time.Duration {
	if s != nil && s.runtimeCacheTTL > 0 {
		return s.runtimeCacheTTL
	}
	return contentModerationRuntimeCacheTTL
}

func (s *ContentModerationService) triggerRuntimeSnapshotRefresh() {
	if s == nil || s.runtimeRefreshDeferred() || !s.runtimeRefreshMu.TryLock() {
		return
	}
	if s.runtimeRefreshDeferred() {
		s.runtimeRefreshMu.Unlock()
		return
	}
	go func() {
		defer s.runtimeRefreshMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), contentModerationRuntimeRefreshTimeout)
		defer cancel()
		if _, err := s.refreshRuntimeSnapshot(ctx); err != nil {
			s.runtimeRefreshRetryAt.Store(time.Now().Add(s.runtimeSnapshotTTL()).UnixNano())
			slog.Warn("content_moderation.runtime_snapshot_refresh_failed", "error", err)
		}
	}()
}

func (s *ContentModerationService) runtimeRefreshDeferred() bool {
	return s != nil && time.Now().UnixNano() < s.runtimeRefreshRetryAt.Load()
}

func (s *ContentModerationService) refreshRuntimeSnapshot(ctx context.Context) (*contentModerationRuntimeSnapshot, error) {
	values, err := s.settingRepo.GetMultiple(ctx, []string{
		SettingKeyRiskControlEnabled,
		SettingKeyContentModerationConfig,
	})
	if err != nil {
		return nil, fmt.Errorf("get content moderation runtime settings: %w", err)
	}
	rawConfig := values[SettingKeyContentModerationConfig]
	configDigest := sha256.Sum256([]byte(rawConfig))
	if current := s.runtimeSnapshot.Load(); current != nil && current.configDigest == configDigest {
		snapshot := &contentModerationRuntimeSnapshot{
			riskControlEnabled: values[SettingKeyRiskControlEnabled] == "true",
			config:             current.config,
			configDigest:       configDigest,
			loadedAt:           time.Now(),
		}
		s.runtimeSnapshot.Store(snapshot)
		s.runtimeRefreshRetryAt.Store(0)
		return snapshot, nil
	}
	cfg, err := parseContentModerationConfig(rawConfig)
	if err != nil {
		return nil, err
	}
	snapshot := &contentModerationRuntimeSnapshot{
		riskControlEnabled: values[SettingKeyRiskControlEnabled] == "true",
		config:             cfg,
		configDigest:       configDigest,
		loadedAt:           time.Now(),
	}
	s.runtimeSnapshot.Store(snapshot)
	s.runtimeRefreshRetryAt.Store(0)
	return snapshot, nil
}

// RefreshPolicyMetadataCache 强制刷新本节点的风控策略元数据。
// 该操作不会触碰哈希命中、用户信任、鉴权、余额、限流或并发槽等共享业务状态。
func (s *ContentModerationService) RefreshPolicyMetadataCache(ctx context.Context) error {
	if s == nil || s.settingRepo == nil {
		return errors.New("policy metadata cache unavailable")
	}
	if ctx == nil {
		return errors.New("policy metadata cache refresh requires a context")
	}

	s.runtimeRefreshMu.Lock()
	defer s.runtimeRefreshMu.Unlock()
	_, err := s.refreshRuntimeSnapshot(ctx)
	return err
}

func (s *ContentModerationService) replaceRuntimeConfig(raw []byte) {
	if s == nil || s.runtimeSnapshot.Load() == nil {
		return
	}
	cfg, err := parseContentModerationConfig(string(raw))
	if err != nil {
		return
	}
	s.runtimeRefreshMu.Lock()
	defer s.runtimeRefreshMu.Unlock()
	current := s.runtimeSnapshot.Load()
	if current == nil {
		return
	}
	s.runtimeSnapshot.Store(&contentModerationRuntimeSnapshot{
		riskControlEnabled: current.riskControlEnabled,
		config:             cfg,
		configDigest:       sha256.Sum256(raw),
		loadedAt:           time.Now(),
	})
}

func (s *ContentModerationService) isRiskControlEnabled(ctx context.Context) bool {
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyRiskControlEnabled)
	if err != nil {
		return false
	}
	return raw == "true"
}

func (s *ContentModerationService) validateConfig(ctx context.Context, cfg *ContentModerationConfig) error {
	if cfg == nil {
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_CONFIG", "内容审计配置不能为空")
	}
	if err := validateContentModerationDynamicSamplingConfig(cfg.DynamicSampling); err != nil {
		return err
	}
	cfg.normalize()
	switch cfg.Mode {
	case ContentModerationModeOff, ContentModerationModeObserve, ContentModerationModePreBlock:
	default:
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_MODE", "内容审计模式无效")
	}
	switch cfg.Provider {
	case ContentModerationProviderOpenAI, ContentModerationProviderZhipu:
	default:
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_PROVIDER", "内容审计供应商无效")
	}
	if _, err := url.ParseRequestURI(cfg.BaseURL); err != nil {
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_BASE_URL", "内容审计 Base URL 无效")
	}
	if cfg.BlockStatus < 400 || cfg.BlockStatus > 599 {
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_BLOCK_STATUS", "拦截 HTTP 状态码必须在 400-599 之间")
	}
	if !cfg.AllGroups && len(cfg.GroupIDs) > 0 && s.groupRepo != nil {
		for _, groupID := range cfg.GroupIDs {
			if _, err := s.groupRepo.GetByIDLite(ctx, groupID); err != nil {
				return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_GROUP", fmt.Sprintf("审计分组不存在: %d", groupID))
			}
		}
	}
	if err := validateCyberPreflightRulesConfig(cfg.CyberPreflightRules); err != nil {
		return err
	}
	return nil
}

func (s *ContentModerationService) ensureAPIKeysNotUsedByUserModeration(ctx context.Context, keys []string) error {
	if s == nil || s.userAPIKeyHashChecker == nil {
		return nil
	}
	for _, key := range normalizeModerationAPIKeys(keys) {
		keyHash := moderationAPIKeyHash(key)
		if keyHash == "" {
			continue
		}
		exists, err := s.userAPIKeyHashChecker.APIKeyHashExists(ctx, keyHash, 0, 0)
		if err != nil {
			return err
		}
		if exists {
			return infraerrors.BadRequest("CONTENT_MODERATION_API_KEY_DUPLICATED", "api_key is already used by user moderation config")
		}
	}
	return nil
}

func (s *ContentModerationService) callModeration(ctx context.Context, cfg *ContentModerationConfig, input ContentModerationInput) (*normalizedModerationResult, error) {
	attempts := cfg.RetryCount + 1
	if attempts <= 0 {
		attempts = 1
	}
	if attempts > maxContentModerationRetryCount+1 {
		attempts = maxContentModerationRetryCount + 1
	}

	// 整轮审核（含全部重试、退避与智谱分块）共用一个总预算，保证同步挡在网关请求
	// 前面的最坏附加延迟等于「超时 × 尝试次数」，不会被分块或退避二次放大。
	budget := contentModerationCallBudget(cfg.TimeoutMS, attempts)
	ctx, cancelBudget := context.WithTimeout(ctx, budget)
	defer cancelBudget()

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			if lastErr == nil {
				lastErr = err
			}
			break
		}
		key, ok := s.nextUsableAPIKey(cfg)
		if !ok {
			lastErr = errors.New("no moderation api key available")
			break
		}
		start := time.Now()
		httpStatus := 0
		result, err := s.callModerationOnceWithContent(ctx, cfg, key, input, &httpStatus)
		latency := int(time.Since(start).Milliseconds())
		if err == nil {
			s.markAPIKeySuccess(key, latency, httpStatus)
			return result, nil
		}
		s.markAPIKeyError(key, err.Error(), latency, httpStatus)
		lastErr = err
		if httpStatus == http.StatusBadRequest {
			break
		}
		if attempt == attempts-1 {
			break
		}
		wait := time.Duration(100*(attempt+1)) * time.Millisecond
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil, lastErr
}

// contentModerationCallBudget 返回一整轮审核调用的总时间预算。
func contentModerationCallBudget(timeoutMS int, attempts int) time.Duration {
	if timeoutMS <= 0 {
		timeoutMS = defaultContentModerationTimeoutMS
	}
	if attempts <= 0 {
		attempts = 1
	}
	// 额外留出重试之间的退避时间（第 n 次退避 100n ms）。
	backoff := time.Duration(50*attempts*(attempts-1)) * time.Millisecond
	return time.Duration(timeoutMS)*time.Millisecond*time.Duration(attempts) + backoff
}

func (s *ContentModerationService) callModerationOnceWithContent(ctx context.Context, cfg *ContentModerationConfig, apiKey string, input ContentModerationInput, httpStatus *int) (*normalizedModerationResult, error) {
	if cfg == nil {
		return nil, errors.New("content moderation config is nil")
	}
	switch normalizeContentModerationProvider(cfg.Provider) {
	case ContentModerationProviderZhipu:
		return s.callZhipuModerationOnce(ctx, cfg, apiKey, input, httpStatus)
	default:
		return s.callOpenAIModerationOnce(ctx, cfg, apiKey, input.ModerationInput(), httpStatus)
	}
}

func (s *ContentModerationService) callOpenAIModerationOnce(ctx context.Context, cfg *ContentModerationConfig, apiKey string, input any, httpStatus *int) (*normalizedModerationResult, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	endpoint, err := url.JoinPath(base, "/v1/moderations")
	if err != nil {
		return nil, err
	}
	payload := moderationAPIRequest{
		Model: cfg.Model,
		Input: input,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if httpStatus != nil {
		*httpStatus = resp.StatusCode
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("moderation api status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out moderationAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Results) == 0 {
		return nil, errors.New("moderation api returned empty results")
	}
	return normalizeOpenAIModerationResult(&out.Results[0], cfg.Thresholds), nil
}

func (s *ContentModerationService) callZhipuModerationOnce(ctx context.Context, cfg *ContentModerationConfig, apiKey string, input ContentModerationInput, httpStatus *int) (*normalizedModerationResult, error) {
	if len(input.Images) > 0 {
		return nil, fmt.Errorf("%w: zhipu moderation image input is not implemented", ErrContentModerationUnsupportedInput)
	}
	text := normalizeContentModerationText(input.Text)
	if strings.TrimSpace(text) == "" {
		return &normalizedModerationResult{
			Flagged:         false,
			RiskLevel:       "PASS",
			HighestCategory: "PASS",
			HighestScore:    0,
			CategoryScores:  map[string]float64{},
		}, nil
	}
	chunks := splitRunesByLimit(text, maxZhipuModerationInputRunes)
	if len(chunks) == 0 {
		chunks = []string{text}
	}

	// 分块并发发起：串行时每块各吃一个 TimeoutMS，12000 字会被切成 6 块，
	// 单次尝试的最坏耗时变成 6×超时，再乘重试次数——这是同步路径上最长的一条尾巴。
	// 并发后整批分块的耗时回到约一个 TimeoutMS，且共用 callModeration 的总预算。
	results := make([]*normalizedModerationResult, len(chunks))
	errs := make([]error, len(chunks))
	statuses := make([]int, len(chunks))
	var wg sync.WaitGroup
	for index, chunk := range chunks {
		wg.Add(1)
		go func(index int, chunk string) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errs[index] = fmt.Errorf("zhipu moderation chunk panic: %v", r)
				}
			}()
			results[index], errs[index] = s.callZhipuModerationChunk(ctx, cfg, apiKey, chunk, &statuses[index])
		}(index, chunk)
	}
	wg.Wait()

	// 任一分块失败即整体失败（与串行实现一致），并回传该分块的 HTTP 状态码，
	// 使上层的冻结与 400 不重试判定保持原有语义。
	for index := range chunks {
		if errs[index] != nil {
			if httpStatus != nil {
				*httpStatus = statuses[index]
			}
			return nil, errs[index]
		}
	}
	if httpStatus != nil && len(statuses) > 0 {
		*httpStatus = statuses[len(statuses)-1]
	}
	return aggregateZhipuModerationResults(results), nil
}

func (s *ContentModerationService) callZhipuModerationChunk(ctx context.Context, cfg *ContentModerationConfig, apiKey string, input string, httpStatus *int) (*normalizedModerationResult, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	endpoint, err := url.JoinPath(base, "/api/paas/v4/moderations")
	if err != nil {
		return nil, err
	}
	payload := moderationAPIRequest{
		Model: cfg.Model,
		Input: input,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if httpStatus != nil {
		*httpStatus = resp.StatusCode
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("zhipu moderation api status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out zhipuModerationAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.ResultList) == 0 {
		return nil, errors.New("zhipu moderation api returned empty result_list")
	}
	return normalizeZhipuModerationResult(out.ResultList), nil
}

func (s *ContentModerationService) buildLog(input ContentModerationCheckInput, cfg *ContentModerationConfig, scopeCtx ContentModerationScopeContext, action string, flagged bool, highestCategory string, highestScore float64, scores map[string]float64, text string, latency *int, queueDelay *int, errText string) *ContentModerationLog {
	var userID *int64
	if input.UserID > 0 {
		userID = &input.UserID
	}
	var apiKeyID *int64
	if input.APIKeyID > 0 {
		apiKeyID = &input.APIKeyID
	}
	if strings.TrimSpace(scopeCtx.ScopeType) == "" {
		scopeCtx.ScopeType = contentModerationScopeTypeGroup
	}
	return &ContentModerationLog{
		RequestID:             input.RequestID,
		UserID:                userID,
		UserEmail:             input.UserEmail,
		APIKeyID:              apiKeyID,
		APIKeyName:            input.APIKeyName,
		GroupID:               cloneContentModerationInt64Ptr(input.GroupID),
		GroupName:             input.GroupName,
		ScopeType:             scopeCtx.ScopeType,
		AccountShareListingID: cloneContentModerationInt64Ptr(scopeCtx.AccountShareListingID),
		AccountID:             cloneContentModerationInt64Ptr(scopeCtx.AccountID),
		OwnerUserID:           cloneContentModerationInt64Ptr(scopeCtx.OwnerUserID),
		ConsumerUserID:        cloneContentModerationInt64Ptr(scopeCtx.ConsumerUserID),
		MembershipID:          cloneContentModerationInt64Ptr(scopeCtx.MembershipID),
		Endpoint:              input.Endpoint,
		Provider:              input.Provider,
		Model:                 input.Model,
		Mode:                  cfg.Mode,
		Action:                action,
		Flagged:               flagged,
		HighestCategory:       highestCategory,
		HighestScore:          highestScore,
		CategoryScores:        cloneFloatMap(scores),
		ThresholdSnapshot:     cloneFloatMap(cfg.Thresholds),
		InputExcerpt:          trimRunes(redactContentModerationSecrets(text), maxModerationExcerptRunes),
		UpstreamLatencyMS:     latency,
		QueueDelayMS:          queueDelay,
		Error:                 errText,
	}
}

func (s *ContentModerationService) applyFlaggedSideEffects(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog) {
	if s == nil || cfg == nil || log == nil || !log.Flagged || log.UserID == nil || *log.UserID <= 0 {
		return
	}
	count := 1
	if s.repo != nil && cfg.ViolationWindowHours > 0 {
		since := time.Now().Add(-time.Duration(cfg.ViolationWindowHours) * time.Hour)
		if n, err := s.repo.CountFlaggedByUserSince(ctx, *log.UserID, since); err == nil {
			count = n + 1
		}
	}
	log.ViolationCount = count
	autoBanJustApplied := false
	if cfg.AutoBanEnabled && cfg.BanThreshold > 0 && count >= cfg.BanThreshold && s.userRepo != nil {
		user, err := s.userRepo.GetByID(ctx, *log.UserID)
		if err != nil {
			slog.Warn("content_moderation.ban_get_user_failed", "user_id", *log.UserID, "error", err)
			return
		}
		if user.Status != StatusDisabled {
			user.Status = StatusDisabled
			if err := s.userRepo.Update(ctx, user); err != nil {
				slog.Warn("content_moderation.ban_update_user_failed", "user_id", *log.UserID, "error", err)
				return
			}
			if s.authCacheInvalidator != nil {
				s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, *log.UserID)
			}
			autoBanJustApplied = true
			s.notifyRiskControlAutoBanned(ctx, *log.UserID)
		}
		log.AutoBanned = true
	}

	if s.emailService == nil || strings.TrimSpace(log.UserEmail) == "" {
		return
	}
	// 违规告知邮件按用户限频：同一用户连续命中时后续邮件没有增量价值，
	// 不限频则用户可以靠连发命中内容给自己刷信，放大 SMTP 配额与发信信誉损耗。
	sendViolation := cfg.EmailOnHit && s.allowViolationEmail(*log.UserID)
	if !sendViolation && !autoBanJustApplied {
		return
	}
	// 发信改为异步：SMTP 握手耗时不可控，同步执行会把它压进网关请求的响应时间里。
	// EmailSent 记录的是"已派发"，不再是"已投递成功"。
	log.EmailSent = s.dispatchFlaggedEmails(cfg, log, sendViolation, autoBanJustApplied)
}

// warnThrottled 对描述"持续错误状态"的告警按 key 限频，避免故障期间每请求一条。
func (s *ContentModerationService) warnThrottled(key string, msg string, args ...any) {
	if s == nil {
		return
	}
	now := time.Now()
	s.warnThrottleMu.Lock()
	if s.warnThrottle == nil {
		s.warnThrottle = make(map[string]time.Time)
	}
	if last, ok := s.warnThrottle[key]; ok && now.Sub(last) < contentModerationWarnLogInterval {
		s.warnThrottleMu.Unlock()
		return
	}
	s.warnThrottle[key] = now
	s.warnThrottleMu.Unlock()
	slog.Warn(msg, args...)
}

// allowViolationEmail 在冷却窗口内对同一用户只放行一封违规告知邮件。
func (s *ContentModerationService) allowViolationEmail(userID int64) bool {
	if s == nil || userID <= 0 {
		return false
	}
	now := time.Now()
	s.emailThrottleMu.Lock()
	defer s.emailThrottleMu.Unlock()
	if s.emailThrottle == nil {
		s.emailThrottle = make(map[int64]time.Time)
	}
	if last, ok := s.emailThrottle[userID]; ok && now.Sub(last) < contentModerationViolationEmailCooldown {
		return false
	}
	// 顺带清掉已过冷却的条目，避免长期运行后 map 无界增长。
	if len(s.emailThrottle) > 1024 {
		for id, last := range s.emailThrottle {
			if now.Sub(last) >= contentModerationViolationEmailCooldown {
				delete(s.emailThrottle, id)
			}
		}
	}
	s.emailThrottle[userID] = now
	return true
}

// dispatchFlaggedEmails 把发信放到有界的后台协程里执行，返回是否已成功派发。
func (s *ContentModerationService) dispatchFlaggedEmails(cfg *ContentModerationConfig, log *ContentModerationLog, sendViolation bool, sendBanNotice bool) bool {
	if s == nil || log == nil || log.UserID == nil {
		return false
	}
	select {
	case s.emailDispatchSlots <- struct{}{}:
	default:
		slog.Warn("content_moderation.email_dispatch_saturated", "user_id", *log.UserID, "email", log.UserEmail)
		return false
	}

	userID := *log.UserID
	// 快照所需字段：调用方在 CreateLog 时还会写 log，后台协程不再读它。
	logCopy := *log
	go func() {
		defer func() { <-s.emailDispatchSlots }()
		defer func() {
			if r := recover(); r != nil {
				slog.Error("content_moderation.email_dispatch_panic", "user_id", userID, "recover", r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), contentModerationEmailDispatchTimeout)
		defer cancel()
		if sendViolation {
			if err := s.sendViolationEmail(ctx, cfg, &logCopy); err != nil {
				slog.Warn("content_moderation.email_failed", "user_id", userID, "email", logCopy.UserEmail, "error", err)
			}
		}
		if sendBanNotice {
			if err := s.sendAccountDisabledEmail(ctx, cfg, &logCopy); err != nil {
				slog.Warn("content_moderation.ban_email_failed", "user_id", userID, "email", logCopy.UserEmail, "error", err)
			}
		}
	}()
	return true
}

func (s *ContentModerationService) notifyRiskControlBlocked(ctx context.Context, input ContentModerationCheckInput, decision *ContentModerationDecision) {
	if s == nil || s.systemNoticeService == nil {
		return
	}
	s.systemNoticeService.NotifyRiskControlBlocked(ctx, input, decision)
}

func (s *ContentModerationService) notifyRiskControlAutoBanned(ctx context.Context, userID int64) {
	if s == nil || s.systemNoticeService == nil {
		return
	}
	s.systemNoticeService.NotifyRiskControlAutoBanned(ctx, userID)
}

func (s *ContentModerationService) notifyRiskControlUnbanned(ctx context.Context, userID int64) {
	if s == nil || s.systemNoticeService == nil {
		return
	}
	s.systemNoticeService.NotifyRiskControlUnbanned(ctx, userID)
}

func (s *ContentModerationService) sendViolationEmail(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog) error {
	siteName := s.siteName(ctx)
	subject := fmt.Sprintf("[%s] 账户风控提醒 / Risk Control Notice", sanitizeEmailHeader(siteName))
	body := buildContentModerationViolationEmailBody(siteName, log, cfg)
	return s.emailService.SendEmail(ctx, log.UserEmail, subject, body)
}

func (s *ContentModerationService) sendAccountDisabledEmail(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog) error {
	siteName := s.siteName(ctx)
	subject := fmt.Sprintf("[%s] 账户已被禁用 / Account Disabled", sanitizeEmailHeader(siteName))
	body := buildContentModerationAccountDisabledEmailBody(siteName, log, cfg)
	return s.emailService.SendEmail(ctx, log.UserEmail, subject, body)
}

func (s *ContentModerationService) siteName(ctx context.Context) string {
	if s == nil || s.settingRepo == nil {
		return "Sub2API"
	}
	name, err := s.settingRepo.GetValue(ctx, SettingKeySiteName)
	if err != nil || strings.TrimSpace(name) == "" {
		return "Sub2API"
	}
	return strings.TrimSpace(name)
}

func defaultContentModerationConfig() *ContentModerationConfig {
	return &ContentModerationConfig{
		Enabled:               false,
		CyberPreflightEnabled: false,
		CyberPreflightRules:   defaultCyberPreflightRulesConfig(),
		Mode:                  ContentModerationModePreBlock,
		Provider:              ContentModerationProviderOpenAI,
		BaseURL:               defaultContentModerationBaseURL,
		Model:                 defaultContentModerationModel,
		TimeoutMS:             defaultContentModerationTimeoutMS,
		SampleRate:            100,
		DynamicSampling:       defaultContentModerationDynamicSamplingConfig(),
		AllGroups:             true,
		GroupIDs:              []int64{},
		RecordNonHits:         false,
		Thresholds:            ContentModerationDefaultThresholds(),
		WorkerCount:           defaultContentModerationWorkerCount,
		QueueSize:             defaultContentModerationQueueSize,
		BlockStatus:           defaultContentModerationBlockHTTPStatus,
		BlockMessage:          defaultContentModerationBlockMessage,
		EmailOnHit:            true,
		AutoBanEnabled:        true,
		BanThreshold:          defaultContentModerationBanThreshold,
		ViolationWindowHours:  defaultContentModerationViolationWindowHours,
		RetryCount:            defaultContentModerationRetryCount,
		HitRetentionDays:      defaultContentModerationHitRetentionDays,
		NonHitRetentionDays:   defaultContentModerationNonHitRetentionDays,
		PreHashCheckEnabled:   false,
		AccountShareModeScope: ContentModerationAccountShareModeScopeConfig{
			Enabled:    false,
			All:        false,
			Platforms:  []string{AccountShareModeGroupPlatformOpenAI},
			ListingIDs: []int64{},
		},
	}
}

func (cfg *ContentModerationConfig) normalize() {
	if cfg.APIKey != "" {
		cfg.APIKeys = normalizeModerationAPIKeys(append(cfg.APIKeys, cfg.APIKey))
		cfg.APIKey = ""
	} else {
		cfg.APIKeys = normalizeModerationAPIKeys(cfg.APIKeys)
	}
	if cfg.Mode == "" {
		cfg.Mode = ContentModerationModePreBlock
	}
	cfg.Provider = normalizeContentModerationProvider(cfg.Provider)
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultContentModerationBaseURLForProvider(cfg.Provider)
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.Model == "" {
		cfg.Model = defaultContentModerationModelForProvider(cfg.Provider)
	}
	cfg.Model = strings.TrimSpace(cfg.Model)
	if cfg.TimeoutMS <= 0 {
		cfg.TimeoutMS = defaultContentModerationTimeoutMS
	}
	if cfg.TimeoutMS > maxContentModerationTimeoutMS {
		cfg.TimeoutMS = maxContentModerationTimeoutMS
	}
	if cfg.SampleRate < 0 {
		cfg.SampleRate = 0
	}
	if cfg.SampleRate > 100 {
		cfg.SampleRate = 100
	}
	cfg.DynamicSampling.normalize()
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = defaultContentModerationWorkerCount
	}
	if cfg.WorkerCount > maxContentModerationWorkerCount {
		cfg.WorkerCount = maxContentModerationWorkerCount
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = defaultContentModerationQueueSize
	}
	if cfg.QueueSize > maxContentModerationQueueSize {
		cfg.QueueSize = maxContentModerationQueueSize
	}
	if strings.TrimSpace(cfg.BlockMessage) == "" {
		cfg.BlockMessage = defaultContentModerationBlockMessage
	}
	cfg.BlockMessage = strings.TrimSpace(cfg.BlockMessage)
	if cfg.BlockStatus <= 0 {
		cfg.BlockStatus = defaultContentModerationBlockHTTPStatus
	}
	if cfg.BanThreshold <= 0 {
		cfg.BanThreshold = defaultContentModerationBanThreshold
	}
	if cfg.ViolationWindowHours <= 0 {
		cfg.ViolationWindowHours = defaultContentModerationViolationWindowHours
	}
	if cfg.RetryCount < 0 {
		cfg.RetryCount = 0
	}
	if cfg.RetryCount > maxContentModerationRetryCount {
		cfg.RetryCount = maxContentModerationRetryCount
	}
	if cfg.HitRetentionDays <= 0 {
		cfg.HitRetentionDays = defaultContentModerationHitRetentionDays
	}
	if cfg.HitRetentionDays > maxContentModerationRetentionDays {
		cfg.HitRetentionDays = maxContentModerationRetentionDays
	}
	if cfg.NonHitRetentionDays <= 0 {
		cfg.NonHitRetentionDays = defaultContentModerationNonHitRetentionDays
	}
	if cfg.NonHitRetentionDays > maxContentModerationNonHitRetentionDays {
		cfg.NonHitRetentionDays = maxContentModerationNonHitRetentionDays
	}
	cfg.GroupIDs = normalizeInt64IDs(cfg.GroupIDs)
	cfg.Thresholds = mergeContentModerationThresholds(ContentModerationDefaultThresholds(), cfg.Thresholds)
	cfg.AccountShareModeScope.normalize()
	cfg.CyberPreflightRules.normalize()
}

func (cfg *ContentModerationConfig) includesGroup(groupID *int64) bool {
	if cfg.AllGroups {
		return true
	}
	if groupID == nil {
		return false
	}
	for _, id := range cfg.GroupIDs {
		if id == *groupID {
			return true
		}
	}
	return false
}

func (cfg *ContentModerationAccountShareModeScopeConfig) normalize() {
	if cfg == nil {
		return
	}
	cfg.Platforms = normalizeContentModerationPlatforms(cfg.Platforms)
	if len(cfg.Platforms) == 0 {
		cfg.Platforms = []string{AccountShareModeGroupPlatformOpenAI}
	}
	cfg.ListingIDs = normalizeInt64IDs(cfg.ListingIDs)
}

func (cfg ContentModerationAccountShareModeScopeConfig) includesPlatform(platform string) bool {
	platform = strings.ToLower(strings.TrimSpace(platform))
	if platform == "" {
		return false
	}
	for _, item := range cfg.Platforms {
		if strings.EqualFold(strings.TrimSpace(item), platform) {
			return true
		}
	}
	return false
}

func (cfg ContentModerationAccountShareModeScopeConfig) includesListing(listingID int64) bool {
	if listingID <= 0 {
		return false
	}
	for _, id := range cfg.ListingIDs {
		if id == listingID {
			return true
		}
	}
	return false
}

func (s *ContentModerationService) resolveScope(ctx context.Context, cfg *ContentModerationConfig, input ContentModerationCheckInput) (bool, ContentModerationScopeContext) {
	scopeCtx := ContentModerationScopeContext{ScopeType: contentModerationScopeTypeGroup}
	if cfg == nil {
		return false, scopeCtx
	}
	if input.GroupID == nil || *input.GroupID <= 0 {
		return cfg.AllGroups, scopeCtx
	}
	groupID := *input.GroupID
	resolver := s.accountShareModeResolver
	isModeGroup := false
	if resolver != nil {
		isModeGroup = s.isModeGroupCached(ctx, resolver, groupID)
	}
	if !isModeGroup {
		return cfg.includesGroup(input.GroupID), scopeCtx
	}

	scopeCtx.ScopeType = contentModerationScopeTypeAccountShareMode
	if resolver == nil {
		return false, scopeCtx
	}
	membership, listing, err := resolver.ResolveActiveBindingForRequest(ctx, input.UserID, input.APIKeyID, groupID)
	if err != nil {
		if errors.Is(err, ErrAccountShareModeGroupUnbound) {
			slog.Debug("content_moderation.skip_account_share_mode_unbound",
				"user_id", input.UserID,
				"api_key_id", input.APIKeyID,
				"group_id", groupID,
				"endpoint", input.Endpoint,
				"protocol", input.Protocol)
			return false, scopeCtx
		}
		s.warnThrottled("account_share_scope_resolve_failed", "content_moderation.account_share_scope_resolve_failed",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", groupID,
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"error", err)
		return false, scopeCtx
	}
	if membership == nil || listing == nil {
		return false, scopeCtx
	}
	scopeCtx.AccountShareListingID = contentModerationInt64Ptr(listing.ID)
	scopeCtx.AccountID = contentModerationInt64Ptr(listing.AccountID)
	scopeCtx.OwnerUserID = contentModerationInt64Ptr(listing.OwnerUserID)
	scopeCtx.ConsumerUserID = contentModerationInt64Ptr(membership.ConsumerUserID)
	scopeCtx.MembershipID = contentModerationInt64Ptr(membership.ID)

	if cfg.AllGroups {
		return true, scopeCtx
	}
	accountScope := cfg.AccountShareModeScope
	accountScope.normalize()
	if !accountScope.Enabled {
		return false, scopeCtx
	}
	platform := AccountShareModeGroupPlatformOpenAI
	if !accountScope.includesPlatform(platform) {
		return false, scopeCtx
	}
	if accountScope.All {
		return true, scopeCtx
	}
	return accountScope.includesListing(listing.ID), scopeCtx
}

// isModeGroupCached 用短 TTL 缓存账号广场模式分组的判定结果。
// 底层查询是一次未缓存的 EXISTS，而 resolveScope 每个在范围内的网关请求都会走到这里；
// 模式分组本身极少变动，陈旧最多持续一个 TTL。
//
// 只缓存查询真正得出的结论：IsModeGroup 会把查询失败折叠成 false，一旦把它缓存下来，
// 一次客户端断连或数据库抖动就会让该分组在整个 TTL 内被判为非模式分组——请求方可以
// 主动中断一次请求来制造这个窗口。因此这里走 IsModeGroupChecked，出错时只影响当前请求。
func (s *ContentModerationService) isModeGroupCached(ctx context.Context, resolver ContentModerationAccountShareModeResolver, groupID int64) bool {
	if s == nil || resolver == nil || groupID <= 0 {
		return false
	}
	now := time.Now()
	s.modeGroupCacheMu.Lock()
	if entry, ok := s.modeGroupCache[groupID]; ok && now.Before(entry.expiresAt) {
		s.modeGroupCacheMu.Unlock()
		return entry.value
	}
	s.modeGroupCacheMu.Unlock()

	value, err := resolver.IsModeGroupChecked(ctx, groupID)
	if err != nil {
		s.warnThrottled("mode_group_lookup_failed", "content_moderation.mode_group_lookup_failed",
			"group_id", groupID, "error", err)
		return false
	}

	s.modeGroupCacheMu.Lock()
	defer s.modeGroupCacheMu.Unlock()
	if s.modeGroupCache == nil {
		s.modeGroupCache = make(map[int64]contentModerationModeGroupCacheEntry)
	}
	if len(s.modeGroupCache) > 1024 {
		for id, entry := range s.modeGroupCache {
			if !now.Before(entry.expiresAt) {
				delete(s.modeGroupCache, id)
			}
		}
	}
	s.modeGroupCache[groupID] = contentModerationModeGroupCacheEntry{
		value:     value,
		expiresAt: now.Add(contentModerationModeGroupCacheTTL),
	}
	return value
}

func contentModerationLogGroupID(groupID *int64) int64 {
	if groupID == nil {
		return 0
	}
	return *groupID
}

func (cfg *ContentModerationConfig) shouldSample(hashText string) bool {
	if cfg.SampleRate >= 100 {
		return true
	}
	if cfg.SampleRate <= 0 {
		return false
	}
	raw, err := hex.DecodeString(hashText)
	if err != nil || len(raw) < 2 {
		return true
	}
	return int(binary.BigEndian.Uint16(raw[:2])%100) < cfg.SampleRate
}

func (cfg *ContentModerationConfig) apiKeys() []string {
	if cfg == nil {
		return nil
	}
	return normalizeModerationAPIKeys(cfg.APIKeys)
}

func (s *ContentModerationService) nextUsableAPIKey(cfg *ContentModerationConfig) (string, bool) {
	keys := cfg.apiKeys()
	if len(keys) == 0 {
		return "", false
	}
	now := time.Now()
	for i := 0; i < len(keys); i++ {
		idx := int(s.apiKeyCursor.Add(1)-1) % len(keys)
		key := keys[idx]
		if !s.isAPIKeyFrozen(key, now) {
			return key, true
		}
	}
	return "", false
}

func (s *ContentModerationService) isAPIKeyFrozen(key string, now time.Time) bool {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return false
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.keyHealth[hash]
	return state != nil && state.FrozenUntil.After(now)
}

func (s *ContentModerationService) markAPIKeySuccess(key string, latencyMS int, httpStatus int) {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.ensureAPIKeyHealthLocked(hash, maskSecretTail(key))
	state.FailureCount = 0
	state.SuccessCount++
	state.LastError = ""
	state.LastCheckedAt = time.Now()
	state.FrozenUntil = time.Time{}
	state.LastLatencyMS = latencyMS
	state.LastHTTPStatus = httpStatus
	state.LastTested = true
}

func (s *ContentModerationService) markAPIKeyError(key string, errText string, latencyMS int, httpStatus int) {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.ensureAPIKeyHealthLocked(hash, maskSecretTail(key))
	if contentModerationFreezeDurationForHTTPStatus(httpStatus) > 0 {
		state.FailureCount++
	}
	state.LastError = trimRunes(errText, 180)
	state.LastCheckedAt = time.Now()
	state.LastLatencyMS = latencyMS
	state.LastHTTPStatus = httpStatus
	state.LastTested = true
	if freezeDuration := contentModerationFreezeDurationForHTTPStatus(httpStatus); freezeDuration > 0 {
		state.FrozenUntil = time.Now().Add(freezeDuration)
	}
}

func contentModerationFreezeDurationForHTTPStatus(httpStatus int) time.Duration {
	switch httpStatus {
	case 0, http.StatusBadRequest:
		return 0
	case http.StatusUnauthorized, http.StatusForbidden:
		return contentModerationKeyAuthFreezeDuration
	case http.StatusTooManyRequests, 529:
		return contentModerationKeyRateLimitFreezeDuration
	default:
		return contentModerationKeyHTTPErrorFreezeDuration
	}
}

func (s *ContentModerationService) ensureAPIKeyHealthLocked(hash string, masked string) *contentModerationKeyHealth {
	if s.keyHealth == nil {
		s.keyHealth = make(map[string]*contentModerationKeyHealth)
	}
	state := s.keyHealth[hash]
	if state == nil {
		state = &contentModerationKeyHealth{Hash: hash}
		s.keyHealth[hash] = state
	}
	if strings.TrimSpace(masked) != "" {
		state.Masked = masked
	}
	return state
}

func (s *ContentModerationService) configView(cfg *ContentModerationConfig) *ContentModerationConfigView {
	keys := cfg.apiKeys()
	masks := make([]string, 0, len(keys))
	for _, key := range keys {
		masks = append(masks, maskSecretTail(key))
	}
	apiKeyMasked := ""
	if len(masks) > 0 {
		apiKeyMasked = masks[0]
	}
	return &ContentModerationConfigView{
		Enabled:                    cfg.Enabled,
		CyberPreflightEnabled:      cfg.CyberPreflightEnabled,
		CyberPreflightRules:        cfg.CyberPreflightRules.clone(),
		CyberPreflightDefaultRules: defaultCyberPreflightRulesConfig(),
		Mode:                       cfg.Mode,
		Provider:                   cfg.Provider,
		BaseURL:                    cfg.BaseURL,
		Model:                      cfg.Model,
		APIKeyConfigured:           len(keys) > 0,
		APIKeyMasked:               apiKeyMasked,
		APIKeyCount:                len(keys),
		APIKeyMasks:                masks,
		APIKeyStatuses:             s.apiKeyStatuses(keys),
		TimeoutMS:                  cfg.TimeoutMS,
		SampleRate:                 cfg.SampleRate,
		DynamicSampling:            cfg.DynamicSampling,
		AllGroups:                  cfg.AllGroups,
		GroupIDs:                   append([]int64(nil), cfg.GroupIDs...),
		RecordNonHits:              cfg.RecordNonHits,
		WorkerCount:                cfg.WorkerCount,
		QueueSize:                  cfg.QueueSize,
		BlockStatus:                cfg.BlockStatus,
		BlockMessage:               cfg.BlockMessage,
		EmailOnHit:                 cfg.EmailOnHit,
		AutoBanEnabled:             cfg.AutoBanEnabled,
		BanThreshold:               cfg.BanThreshold,
		ViolationWindowHours:       cfg.ViolationWindowHours,
		RetryCount:                 cfg.RetryCount,
		HitRetentionDays:           cfg.HitRetentionDays,
		NonHitRetentionDays:        cfg.NonHitRetentionDays,
		PreHashCheckEnabled:        cfg.PreHashCheckEnabled,
		AccountShareModeScope:      cfg.AccountShareModeScope,
	}
}

func (s *ContentModerationService) apiKeyStatuses(keys []string) []ContentModerationAPIKeyStatus {
	out := make([]ContentModerationAPIKeyStatus, 0, len(keys))
	for idx, key := range keys {
		out = append(out, s.apiKeyStatusForHash(idx, moderationAPIKeyHash(key), maskSecretTail(key), true))
	}
	return out
}

func (s *ContentModerationService) apiKeyStatusForHash(index int, hash string, masked string, configured bool) ContentModerationAPIKeyStatus {
	status := ContentModerationAPIKeyStatus{
		Index:      index,
		KeyHash:    hash,
		Masked:     masked,
		Status:     "unknown",
		Configured: configured,
	}
	if hash == "" || s == nil {
		return status
	}
	now := time.Now()
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.keyHealth[hash]
	if state == nil {
		return status
	}
	status.FailureCount = state.FailureCount
	status.SuccessCount = state.SuccessCount
	status.LastError = state.LastError
	status.LastLatencyMS = state.LastLatencyMS
	status.LastHTTPStatus = state.LastHTTPStatus
	status.LastTested = state.LastTested
	if !state.LastCheckedAt.IsZero() {
		t := state.LastCheckedAt
		status.LastCheckedAt = &t
	}
	if state.FrozenUntil.After(now) {
		t := state.FrozenUntil
		status.FrozenUntil = &t
		status.Status = "frozen"
		return status
	}
	if state.LastError != "" {
		status.Status = "error"
		return status
	}
	if state.SuccessCount > 0 || state.LastTested {
		status.Status = "ok"
	}
	return status
}

func moderationAPIKeyHash(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func buildModerationTestInput(prompt string, images []string) (ContentModerationInput, int, error) {
	prompt = trimRunes(normalizeContentModerationText(prompt), maxModerationInputRunes)
	normalizedImages := make([]string, 0, len(images))
	for _, image := range images {
		image = strings.TrimSpace(image)
		if image == "" {
			continue
		}
		if len(normalizedImages) >= maxContentModerationTestImages {
			return ContentModerationInput{}, 0, infraerrors.BadRequest("TOO_MANY_MODERATION_TEST_IMAGES", fmt.Sprintf("最多上传 %d 张测试图片", maxContentModerationTestImages))
		}
		if err := validateModerationTestImageDataURL(image); err != nil {
			return ContentModerationInput{}, 0, err
		}
		normalizedImages = append(normalizedImages, image)
	}
	if prompt == "" && len(normalizedImages) == 0 {
		prompt = "hello"
	}
	return ContentModerationInput{Text: prompt, Images: normalizedImages}, len(normalizedImages), nil
}

func contentModerationTestHasAuditInput(prompt string, images []string) bool {
	if normalizeContentModerationText(prompt) != "" {
		return true
	}
	for _, image := range images {
		if strings.TrimSpace(image) != "" {
			return true
		}
	}
	return false
}

func validateModerationTestImageDataURL(value string) error {
	if len(value) > maxContentModerationTestImageDataURLBytes {
		return infraerrors.BadRequest("MODERATION_TEST_IMAGE_TOO_LARGE", "测试图片不能超过 8MB")
	}
	if !strings.HasPrefix(value, "data:image/") {
		return infraerrors.BadRequest("INVALID_MODERATION_TEST_IMAGE", "测试图片必须是 data:image/* base64")
	}
	parts := strings.SplitN(value, ",", 2)
	if len(parts) != 2 || !strings.Contains(parts[0], ";base64") {
		return infraerrors.BadRequest("INVALID_MODERATION_TEST_IMAGE", "测试图片必须是 base64 data URL")
	}
	raw, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return infraerrors.BadRequest("INVALID_MODERATION_TEST_IMAGE", "测试图片 base64 无效")
	}
	if len(raw) > maxContentModerationTestImageBytes {
		return infraerrors.BadRequest("MODERATION_TEST_IMAGE_TOO_LARGE", "测试图片不能超过 8MB")
	}
	return nil
}

func buildContentModerationTestAuditResult(result *normalizedModerationResult, thresholds map[string]float64) *ContentModerationTestAuditResult {
	if result == nil {
		return nil
	}
	scores := cloneFloatMap(result.CategoryScores)
	if scores == nil {
		scores = map[string]float64{}
	}
	thresholdSnapshot := mergeContentModerationThresholds(ContentModerationDefaultThresholds(), thresholds)
	return &ContentModerationTestAuditResult{
		Flagged:         result.Flagged,
		RiskLevel:       result.RiskLevel,
		HighestCategory: result.HighestCategory,
		HighestScore:    result.HighestScore,
		CompositeScore:  result.HighestScore,
		CategoryScores:  scores,
		Thresholds:      thresholdSnapshot,
	}
}

type moderationAPIRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type moderationAPIInputPart struct {
	Type     string                    `json:"type"`
	Text     string                    `json:"text,omitempty"`
	ImageURL *moderationAPIImageURLRef `json:"image_url,omitempty"`
}

type moderationAPIImageURLRef struct {
	URL string `json:"url"`
}

type moderationAPIResponse struct {
	Results []moderationAPIResult `json:"results"`
}

type moderationAPIResult struct {
	Flagged        bool               `json:"flagged"`
	CategoryScores map[string]float64 `json:"category_scores"`
}

type zhipuModerationAPIResponse struct {
	ResultList []zhipuModerationAPIResult `json:"result_list"`
}

type zhipuModerationAPIResult struct {
	ContentType string   `json:"content_type"`
	RiskLevel   string   `json:"risk_level"`
	RiskType    []string `json:"risk_type"`
}

type normalizedModerationResult struct {
	Flagged         bool
	RiskLevel       string
	HighestCategory string
	HighestScore    float64
	CategoryScores  map[string]float64
	RawRiskTypes    []string
}

func normalizeOpenAIModerationResult(result *moderationAPIResult, thresholds map[string]float64) *normalizedModerationResult {
	if result == nil {
		return nil
	}
	scores := cloneFloatMap(result.CategoryScores)
	if scores == nil {
		scores = map[string]float64{}
	}
	thresholdSnapshot := mergeContentModerationThresholds(ContentModerationDefaultThresholds(), thresholds)
	flagged, highestCategory, highestScore := evaluateModerationScores(scores, thresholdSnapshot)
	return &normalizedModerationResult{
		Flagged:         flagged,
		HighestCategory: highestCategory,
		HighestScore:    highestScore,
		CategoryScores:  scores,
	}
}

func normalizeZhipuModerationResult(results []zhipuModerationAPIResult) *normalizedModerationResult {
	normalized := &normalizedModerationResult{
		RiskLevel:       "PASS",
		HighestCategory: "PASS",
		HighestScore:    0,
		CategoryScores:  map[string]float64{},
	}
	for _, item := range results {
		riskLevel := normalizeZhipuRiskLevel(item.RiskLevel)
		score := zhipuRiskLevelScore(riskLevel)
		if score > normalized.HighestScore {
			normalized.RiskLevel = riskLevel
			normalized.HighestScore = score
			normalized.HighestCategory = contentModerationFirstNonEmptyString(item.RiskType, riskLevel)
		}
		if riskLevel != "PASS" {
			normalized.Flagged = true
		}
		for _, riskType := range item.RiskType {
			riskType = strings.TrimSpace(riskType)
			if riskType == "" {
				continue
			}
			normalized.RawRiskTypes = append(normalized.RawRiskTypes, riskType)
			if score > normalized.CategoryScores[riskType] {
				normalized.CategoryScores[riskType] = score
			}
		}
		if len(item.RiskType) == 0 && riskLevel != "PASS" {
			normalized.CategoryScores[riskLevel] = score
		}
	}
	return normalized
}

func aggregateZhipuModerationResults(results []*normalizedModerationResult) *normalizedModerationResult {
	aggregated := &normalizedModerationResult{
		RiskLevel:       "PASS",
		HighestCategory: "PASS",
		HighestScore:    0,
		CategoryScores:  map[string]float64{},
	}
	for _, result := range results {
		if result == nil {
			continue
		}
		if result.HighestScore > aggregated.HighestScore {
			aggregated.RiskLevel = normalizeZhipuRiskLevel(result.RiskLevel)
			aggregated.HighestCategory = strings.TrimSpace(result.HighestCategory)
			aggregated.HighestScore = result.HighestScore
		}
		if result.Flagged {
			aggregated.Flagged = true
		}
		for category, score := range result.CategoryScores {
			if score > aggregated.CategoryScores[category] {
				aggregated.CategoryScores[category] = score
			}
		}
		aggregated.RawRiskTypes = append(aggregated.RawRiskTypes, result.RawRiskTypes...)
	}
	if strings.TrimSpace(aggregated.HighestCategory) == "" {
		aggregated.HighestCategory = aggregated.RiskLevel
	}
	return aggregated
}

func normalizeZhipuRiskLevel(level string) string {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "REJECT":
		return "REJECT"
	case "REVIEW":
		return "REVIEW"
	default:
		return "PASS"
	}
}

func zhipuRiskLevelScore(level string) float64 {
	switch normalizeZhipuRiskLevel(level) {
	case "REJECT":
		return 1
	case "REVIEW":
		return 0.8
	default:
		return 0
	}
}

func contentModerationFirstNonEmptyString(values []string, fallback string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return strings.TrimSpace(fallback)
}

func splitRunesByLimit(value string, limit int) []string {
	if limit <= 0 {
		return []string{value}
	}
	runes := []rune(value)
	if len(runes) == 0 {
		return nil
	}
	out := make([]string, 0, (len(runes)+limit-1)/limit)
	for start := 0; start < len(runes); start += limit {
		end := start + limit
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[start:end]))
	}
	return out
}

func evaluateModerationScores(scores map[string]float64, thresholds map[string]float64) (bool, string, float64) {
	flagged := false
	highestCategory := ""
	highestScore := 0.0
	for _, category := range contentModerationCategoryOrder {
		score := scores[category]
		if score > highestScore || highestCategory == "" {
			highestScore = score
			highestCategory = category
		}
		if score >= thresholds[category] {
			flagged = true
		}
	}
	for category, score := range scores {
		if score > highestScore || highestCategory == "" {
			highestScore = score
			highestCategory = category
		}
	}
	return flagged, highestCategory, highestScore
}

func mergeContentModerationThresholds(base map[string]float64, override map[string]float64) map[string]float64 {
	out := cloneFloatMap(base)
	if out == nil {
		out = map[string]float64{}
	}
	for _, category := range contentModerationCategoryOrder {
		if v, ok := override[category]; ok {
			if v < 0 {
				v = 0
			}
			if v > 1 {
				v = 1
			}
			out[category] = v
		}
	}
	return out
}

func normalizeInt64IDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return []int64{}
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func normalizeModerationAPIKeys(keys []string) []string {
	if len(keys) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func deleteModerationAPIKeysByHash(keys []string, hashes []string) []string {
	keys = normalizeModerationAPIKeys(keys)
	deleteHashes := make(map[string]struct{}, len(hashes))
	for _, hash := range hashes {
		hash = normalizeContentModerationHash(hash)
		if hash != "" {
			deleteHashes[hash] = struct{}{}
		}
	}
	if len(deleteHashes) == 0 {
		return keys
	}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, ok := deleteHashes[moderationAPIKeyHash(key)]; ok {
			continue
		}
		out = append(out, key)
	}
	return out
}

func normalizeContentModerationProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case ContentModerationProviderZhipu:
		return ContentModerationProviderZhipu
	default:
		return ContentModerationProviderOpenAI
	}
}

func defaultContentModerationBaseURLForProvider(provider string) string {
	switch normalizeContentModerationProvider(provider) {
	case ContentModerationProviderZhipu:
		return defaultZhipuContentModerationBaseURL
	default:
		return defaultContentModerationBaseURL
	}
}

func defaultContentModerationModelForProvider(provider string) string {
	switch normalizeContentModerationProvider(provider) {
	case ContentModerationProviderZhipu:
		return defaultZhipuContentModerationModel
	default:
		return defaultContentModerationModel
	}
}

func normalizeContentModerationPlatforms(platforms []string) []string {
	seen := make(map[string]struct{}, len(platforms))
	out := make([]string, 0, len(platforms))
	for _, platform := range platforms {
		platform = strings.ToLower(strings.TrimSpace(platform))
		if platform == "" {
			continue
		}
		if platform != AccountShareModeGroupPlatformOpenAI {
			continue
		}
		if _, ok := seen[platform]; ok {
			continue
		}
		seen[platform] = struct{}{}
		out = append(out, platform)
	}
	return out
}

func normalizeContentModerationAPIKeysMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case contentModerationAPIKeysModeReplace:
		return contentModerationAPIKeysModeReplace
	default:
		return contentModerationAPIKeysModeAppend
	}
}

func normalizeContentModerationHash(inputHash string) string {
	inputHash = strings.ToLower(strings.TrimSpace(inputHash))
	if len(inputHash) != sha256.Size*2 {
		return ""
	}
	if _, err := hex.DecodeString(inputHash); err != nil {
		return ""
	}
	return inputHash
}

func cloneFloatMap(in map[string]float64) map[string]float64 {
	if in == nil {
		return map[string]float64{}
	}
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneContentModerationInt64Ptr(in *int64) *int64 {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func contentModerationInt64Ptr(v int64) *int64 {
	if v <= 0 {
		return nil
	}
	return &v
}

func trimRunes(text string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max])
}

func maskSecretTail(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	if len(secret) <= 4 {
		return "****"
	}
	return strings.Repeat("*", 8) + secret[len(secret)-4:]
}
