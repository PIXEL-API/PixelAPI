// Package service provides business logic and domain services for the application.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

const (
	OpenAIAuthModeAgentIdentity       = "agentIdentity"
	OpenAIAuthModePersonalAccessToken = "personalAccessToken"
	openAIAuthModeCredentialKey       = "auth_mode"
	openAIAuthModeLegacyCredentialKey = "openai_auth_mode"
)

func isOpenAIPersonalAccessTokenAuthMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "personalaccesstoken", "personal_access_token":
		return true
	default:
		return false
	}
}

type Account struct {
	ID            int64
	Name          string
	Notes         *string
	Platform      string
	AccountLevel  string
	Type          string
	Credentials   map[string]any
	Extra         map[string]any
	OwnerUserID   *int64
	ShareMode     string
	ShareStatus   string
	SharePolicyID *int64
	// AccountShareModeListingID is a runtime marker for accounts that back an
	// account-share-mode listing. It is not stored on accounts.
	AccountShareModeListingID *int64
	ExternalPlacement         *AccountExternalPlacement
	ProxyID                   *int64
	// ProxyFallbackOriginID 记录代理到期自动改投前的原始代理，用于管理员显式回切。
	ProxyFallbackOriginID *int64
	Concurrency           int
	Priority              int
	// RateMultiplier 账号计费倍率（>=0，允许 0 表示该账号计费为 0）。
	// 使用指针用于兼容旧版本调度缓存（Redis）中缺字段的情况：nil 表示按 1.0 处理。
	RateMultiplier        *float64
	LoadFactor            *int // 调度负载因子；nil 表示使用 Concurrency
	LoadFactorPaidCeiling int
	Status                string
	ErrorMessage          string
	ErrorSince            *time.Time
	LastUsedAt            *time.Time
	ExpiresAt             *time.Time
	AutoPauseOnExpired    bool
	CreatedAt             time.Time
	UpdatedAt             time.Time

	Schedulable bool

	RateLimitedAt    *time.Time
	RateLimitResetAt *time.Time
	OverloadUntil    *time.Time

	TempUnschedulableUntil  *time.Time
	TempUnschedulableReason string

	SessionWindowStart  *time.Time
	SessionWindowEnd    *time.Time
	SessionWindowStatus string

	Proxy         *Proxy
	AccountGroups []AccountGroup
	GroupIDs      []int64
	Groups        []*Group

	// model_mapping 热路径缓存（非持久化字段）
	modelMappingCache               map[string]string
	modelMappingCacheReady          bool
	modelMappingCacheCredentialsPtr uintptr
	modelMappingCacheRawPtr         uintptr
	modelMappingCacheRawLen         int
	modelMappingCacheRawSig         uint64
	modelMappingCacheRuntimeVersion uint64

	// header_overrides 热路径缓存（非持久化字段，同 model_mapping 缓存先例）
	headerOverrideCache               map[string]string
	headerOverrideCacheReady          bool
	headerOverrideCacheCredentialsPtr uintptr
	headerOverrideCacheRawPtr         uintptr
	headerOverrideCacheRawLen         int
	headerOverrideCacheRawSig         uint64
}

// OpenAIEndpointCapability identifies an endpoint-specific requirement that
// must survive scheduler cache hydration and the final pre-dispatch recheck.
type OpenAIEndpointCapability string

const (
	// OpenAIEndpointCapabilityGrokMediaGeneration keeps new image/video
	// generation requests away from Grok OAuth accounts without positive paid
	// entitlement evidence. Video status/content lookups intentionally do not
	// require this capability so existing tasks remain queryable.
	OpenAIEndpointCapabilityGrokMediaGeneration OpenAIEndpointCapability = "grok_media_generation"
)

// GrokMediaEligibleExtraKey is an optional operator override in accounts.extra.
// A boolean true/false takes precedence over provider observations; absent,
// null, or malformed values do not override observed billing state.
const GrokMediaEligibleExtraKey = "grok_media_eligible"

const (
	AccountShareModePrivate = "private"
	AccountShareModePublic  = "public"

	AccountShareStatusPending   = "pending"
	AccountShareStatusApproved  = "approved"
	AccountShareStatusSuspended = "suspended"
)

const (
	OAuthAccountDefaultConcurrency    = 3
	OpenAIPlusDefaultConcurrency      = 3
	OwnedPersonalDefaultLoadFactor    = 10
	AccountMaxLoadFactor              = 10000
	CodexQuotaDefaultLimitPercent     = 100.0
	CodexQuotaMinLimitPercent         = 1.0
	CodexQuotaMaxLimitPercent         = 100.0
	AnthropicQuotaDefaultLimitPercent = CodexQuotaDefaultLimitPercent
	AnthropicQuotaMinLimitPercent     = CodexQuotaMinLimitPercent
	AnthropicQuotaMaxLimitPercent     = CodexQuotaMaxLimitPercent
)

const (
	CodexQuotaWindow5h     = "5h"
	CodexQuotaWindow7d     = "7d"
	AnthropicQuotaWindow5h = CodexQuotaWindow5h
	AnthropicQuotaWindow7d = CodexQuotaWindow7d
	OpencodeQuotaWindow5h  = CodexQuotaWindow5h
	OpencodeQuotaWindow7d  = CodexQuotaWindow7d
	OpencodeQuotaWindow30d = "30d"
)

const (
	AccountListStatusRateLimited            = "rate_limited"
	AccountListStatusTempUnschedulable      = "temp_unschedulable"
	AccountListStatusUnschedulable          = "unschedulable"
	AccountListStatusCodexQuotaProtected    = "codex_quota_protected"
	AccountListStatusOpencodeQuotaProtected = "opencode_quota_protected"
)

func NormalizeAccountLevel(level string) string {
	normalized := NormalizeAccountLevelKey(level)
	if normalized == "" {
		return AccountLevelUnknown
	}
	return normalized
}

func NormalizeAccountLevelKey(level string) string {
	normalized := strings.ToLower(strings.TrimSpace(level))
	normalized = strings.NewReplacer(" ", "-", "_", "-").Replace(normalized)
	var b strings.Builder
	lastDash := false
	for _, r := range normalized {
		switch {
		case r >= 'a' && r <= 'z':
			_, _ = b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			_, _ = b.WriteRune(r)
			lastDash = false
		case r == '-':
			if b.Len() > 0 && !lastDash {
				_, _ = b.WriteRune(r)
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func IsConcreteAccountLevel(level string) bool {
	return NormalizeAccountLevel(level) != AccountLevelUnknown
}

func IsUserSelectableOpenAIAccountLevel(level string) bool {
	return IsUserSelectableOpenAIAccountLevelWithConfigs(level, DefaultOpenAIAccountLevelConfigs())
}

func IsUserSelectableGrokAccountLevel(level string) bool {
	switch NormalizeAccountLevel(level) {
	case AccountLevelFree, AccountLevelHeavy:
		return true
	default:
		return false
	}
}

func RequiresUserOpenAIProxyLogin(level string) bool {
	return RequiresUserOpenAIProxyLoginWithConfigs(level, DefaultOpenAIAccountLevelConfigs())
}

func RequiresUserAccountOAuthProxy(platform, accountLevel string) bool {
	return RequiresUserAccountOAuthProxyWithConfigs(platform, accountLevel, DefaultOpenAIAccountLevelConfigs())
}

func RequiresUserAccountOAuthProxyWithConfigs(platform, accountLevel string, configs []OpenAIAccountLevelConfig) bool {
	switch platform {
	case PlatformAnthropic, PlatformGemini, PlatformAntigravity, PlatformGrok:
		return true
	case PlatformOpenAI:
		return RequiresUserOpenAIProxyLoginWithConfigs(accountLevel, configs)
	default:
		return false
	}
}

func IsUserSelectableOpenAIAccountLevelWithConfigs(level string, configs []OpenAIAccountLevelConfig) bool {
	normalized := NormalizeAccountLevel(level)
	for _, cfg := range NormalizeOpenAIAccountLevelConfigs(configs) {
		if cfg.Enabled && cfg.Key == normalized {
			return true
		}
	}
	return false
}

func RequiresUserOpenAIProxyLoginWithConfigs(level string, configs []OpenAIAccountLevelConfig) bool {
	normalized := NormalizeAccountLevel(level)
	for _, cfg := range NormalizeOpenAIAccountLevelConfigs(configs) {
		if cfg.Enabled && cfg.Key == normalized {
			return cfg.RequiresProxyLogin
		}
	}
	return false
}

func IsOpenAIPlusAccount(platform, accountLevel string) bool {
	return platform == PlatformOpenAI && NormalizeAccountLevel(accountLevel) == AccountLevelPlus
}

func NormalizeOpenAIAccountLevel(platform, accountLevel string, credentials, extra map[string]any) string {
	return NormalizeOpenAIAccountLevelWithConfigs(platform, accountLevel, credentials, extra, DefaultOpenAIAccountLevelConfigs())
}

func NormalizeOpenAIAccountLevelWithConfigs(platform, accountLevel string, credentials, extra map[string]any, configs []OpenAIAccountLevelConfig) string {
	level := NormalizeAccountLevel(accountLevel)
	if platform != PlatformOpenAI {
		return level
	}
	if OpenAIAccountLevelConfigByKeyIncludingDisabled(configs, level) != nil {
		return level
	}
	if inferred := InferOpenAIAccountLevelWithConfigs(credentials, extra, configs); OpenAIAccountLevelConfigByKeyIncludingDisabled(configs, inferred) != nil {
		return inferred
	}
	return level
}

func InferOpenAIAccountLevel(credentials, extra map[string]any) string {
	return InferOpenAIAccountLevelWithConfigs(credentials, extra, DefaultOpenAIAccountLevelConfigs())
}

func InferOpenAIAccountLevelWithConfigs(credentials, extra map[string]any, configs []OpenAIAccountLevelConfig) string {
	for _, values := range []map[string]any{credentials, extra} {
		for _, key := range []string{"plan_type", "chatgpt_plan_type", "subscription_plan"} {
			raw, ok := values[key].(string)
			if !ok {
				continue
			}
			if inferred := NormalizeOpenAIPlanAccountLevelWithConfigs(raw, configs); inferred != AccountLevelUnknown {
				return inferred
			}
		}
	}
	return AccountLevelUnknown
}

func OpenAIAccountPlanType(credentials, extra map[string]any) string {
	for _, values := range []map[string]any{credentials, extra} {
		for _, key := range []string{"plan_type", "chatgpt_plan_type", "subscription_plan"} {
			raw, ok := values[key].(string)
			if !ok {
				continue
			}
			if trimmed := strings.TrimSpace(raw); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func NormalizeOpenAIPlanAccountLevel(planType string) string {
	return NormalizeOpenAIPlanAccountLevelWithConfigs(planType, DefaultOpenAIAccountLevelConfigs())
}

func NormalizeOpenAIPlanAccountLevelWithConfigs(planType string, configs []OpenAIAccountLevelConfig) string {
	token := normalizeOpenAIPlanAliasToken(planType)
	if token == "" {
		return AccountLevelUnknown
	}
	for _, cfg := range NormalizeOpenAIAccountLevelConfigs(configs) {
		if !cfg.Enabled {
			continue
		}
		for _, alias := range openAIAccountLevelAliasTokens(cfg) {
			if matchOpenAIPlanAliasToken(token, alias) {
				return cfg.Key
			}
		}
	}
	return AccountLevelUnknown
}

func NormalizeOpenAISharedPoolAccountLevel(level string) string {
	switch NormalizeAccountLevel(level) {
	case AccountLevelUnknown:
		return AccountLevelFree
	default:
		return NormalizeAccountLevel(level)
	}
}

func NormalizeOpenAISharedPoolRequiredLevel(level string) string {
	return NormalizeRequiredAccountLevel(level)
}

func OpenAISharedPoolLevelRank(level string) int {
	return OpenAISharedPoolLevelRankWithConfigs(level, DefaultOpenAIAccountLevelConfigs())
}

func OpenAISharedPoolLevelRankWithConfigs(level string, configs []OpenAIAccountLevelConfig) int {
	normalized := NormalizeOpenAISharedPoolAccountLevel(level)
	for index, cfg := range NormalizeOpenAIAccountLevelConfigs(configs) {
		if cfg.Enabled && cfg.Key == normalized {
			return index + 1
		}
	}
	return 0
}

func CanOpenAIAccountJoinSharedPool(accountLevel, requiredLevel string) bool {
	return CanOpenAIAccountJoinSharedPoolWithConfigs(accountLevel, requiredLevel, DefaultOpenAIAccountLevelConfigs())
}

func CanOpenAIAccountJoinSharedPoolWithConfigs(accountLevel, requiredLevel string, configs []OpenAIAccountLevelConfig) bool {
	required := NormalizeOpenAISharedPoolRequiredLevel(requiredLevel)
	if required == "" {
		return true
	}
	account := NormalizeOpenAISharedPoolAccountLevel(accountLevel)
	normalizedConfigs := NormalizeOpenAIAccountLevelConfigs(configs)
	accountRank := OpenAISharedPoolLevelRankWithConfigs(account, normalizedConfigs)
	requiredRank := OpenAISharedPoolLevelRankWithConfigs(required, normalizedConfigs)
	if accountRank > 0 || requiredRank > 0 {
		return accountRank > 0 && requiredRank > 0 && account == required
	}
	return account == required
}

func OpenAISharedPoolAllowedAccountLevels(requiredLevel string) []string {
	return OpenAISharedPoolAllowedAccountLevelsWithConfigs(requiredLevel, DefaultOpenAIAccountLevelConfigs())
}

func OpenAISharedPoolAllowedAccountLevelsWithConfigs(requiredLevel string, configs []OpenAIAccountLevelConfig) []string {
	required := NormalizeOpenAISharedPoolRequiredLevel(requiredLevel)
	if required == "" {
		return nil
	}
	normalizedConfigs := NormalizeOpenAIAccountLevelConfigs(configs)
	requiredRank := OpenAISharedPoolLevelRankWithConfigs(required, normalizedConfigs)
	if requiredRank == 0 {
		return []string{required}
	}
	levels := make([]string, 0, 6)
	if required == AccountLevelFree {
		levels = append(levels, AccountLevelUnknown)
	}
	for _, cfg := range normalizedConfigs {
		if cfg.Enabled && CanOpenAIAccountJoinSharedPoolWithConfigs(cfg.Key, required, normalizedConfigs) {
			levels = append(levels, cfg.Key)
		}
	}
	return levels
}

func ValidateConfiguredOpenAIAccountLevel(platform, level string, configs []OpenAIAccountLevelConfig) error {
	if platform != PlatformOpenAI {
		return nil
	}
	normalized := NormalizeAccountLevel(level)
	if normalized == AccountLevelUnknown {
		return nil
	}
	if OpenAIAccountLevelConfigByKeyIncludingDisabled(configs, normalized) == nil {
		return fmt.Errorf("invalid OpenAI account level: %s", normalized)
	}
	return nil
}

func DefaultOpenAIAccountLevelConfigs() []OpenAIAccountLevelConfig {
	return []OpenAIAccountLevelConfig{
		{Key: AccountLevelFree, Label: "Free", Aliases: []string{"free", "chatgptfree"}, SortOrder: 10, Enabled: true},
		{Key: AccountLevelPlus, Label: "Plus", Aliases: []string{"plus", "plus*", "chatgptplus"}, SortOrder: 20, Enabled: true},
		{Key: AccountLevelPro, Label: "Pro", Aliases: []string{"pro", "pro*", "chatgptpro", "chatgptpro*"}, SortOrder: 30, Enabled: true, RequiresProxyLogin: true},
		{Key: AccountLevelTeam, Label: "Team", Aliases: []string{"team", "team*", "chatgptteam"}, SortOrder: 40, Enabled: true},
		{Key: AccountLevelK12, Label: "K12", Aliases: []string{"k12", "chatgptk12", "chatgpt-k12"}, SortOrder: 50, Enabled: true},
	}
}

func NormalizeOpenAIAccountLevelConfigs(configs []OpenAIAccountLevelConfig) []OpenAIAccountLevelConfig {
	if len(configs) == 0 {
		configs = DefaultOpenAIAccountLevelConfigs()
	}
	out := make([]OpenAIAccountLevelConfig, 0, len(configs))
	seenKeys := make(map[string]struct{}, len(configs))
	seenAliases := make(map[string]string)
	for _, cfg := range configs {
		key := NormalizeAccountLevelKey(cfg.Key)
		if key == "" || key == AccountLevelUnknown {
			continue
		}
		if _, ok := seenKeys[key]; ok {
			continue
		}
		label := strings.TrimSpace(cfg.Label)
		if label == "" {
			label = key
		}
		aliases := make([]string, 0, len(cfg.Aliases)+1)
		for _, alias := range append([]string{key}, cfg.Aliases...) {
			normalizedAlias := normalizeOpenAIPlanAliasPattern(alias)
			if normalizedAlias == "" {
				continue
			}
			if owner, ok := seenAliases[normalizedAlias]; ok && owner != key {
				continue
			}
			seenAliases[normalizedAlias] = key
			if !containsString(aliases, normalizedAlias) {
				aliases = append(aliases, normalizedAlias)
			}
		}
		if len(aliases) == 0 {
			aliases = []string{key}
		}
		enabled := cfg.Enabled
		out = append(out, OpenAIAccountLevelConfig{
			Key:                key,
			Label:              label,
			Aliases:            aliases,
			SortOrder:          cfg.SortOrder,
			Enabled:            enabled,
			RequiresProxyLogin: cfg.RequiresProxyLogin,
		})
		seenKeys[key] = struct{}{}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SortOrder == out[j].SortOrder {
			return out[i].Key < out[j].Key
		}
		return out[i].SortOrder < out[j].SortOrder
	})
	return out
}

func OpenAIAccountLevelConfigSelectable(configs []OpenAIAccountLevelConfig) []OpenAIAccountLevelConfig {
	normalized := NormalizeOpenAIAccountLevelConfigs(configs)
	out := make([]OpenAIAccountLevelConfig, 0, len(normalized))
	for _, cfg := range normalized {
		if cfg.Enabled {
			out = append(out, cfg)
		}
	}
	return out
}

func ValidateOpenAIAccountLevelConfigs(configs []OpenAIAccountLevelConfig) ([]OpenAIAccountLevelConfig, error) {
	if len(configs) == 0 {
		return nil, fmt.Errorf("openai_account_levels cannot be empty")
	}
	seenAliases := make(map[string]string)
	for _, cfg := range configs {
		key := NormalizeAccountLevelKey(cfg.Key)
		if key == "" || key == AccountLevelUnknown {
			continue
		}
		for _, alias := range append([]string{key}, cfg.Aliases...) {
			normalizedAlias := normalizeOpenAIPlanAliasPattern(alias)
			if normalizedAlias == "" {
				continue
			}
			if owner, ok := seenAliases[normalizedAlias]; ok && owner != key {
				return nil, fmt.Errorf("openai_account_levels alias %q is used by both %q and %q", normalizedAlias, owner, key)
			}
			seenAliases[normalizedAlias] = key
		}
	}
	normalized := NormalizeOpenAIAccountLevelConfigs(configs)
	if len(normalized) == 0 {
		return nil, fmt.Errorf("openai_account_levels must contain at least one valid level")
	}
	enabledCount := 0
	for _, cfg := range normalized {
		if cfg.Enabled {
			enabledCount++
		}
	}
	if enabledCount == 0 {
		return nil, fmt.Errorf("openai_account_levels must contain at least one enabled level")
	}
	return normalized, nil
}

func OpenAIAccountLevelConfigByKey(configs []OpenAIAccountLevelConfig, key string) *OpenAIAccountLevelConfig {
	return OpenAIAccountLevelConfigByKeyWithEnabled(configs, key, true)
}

func OpenAIAccountLevelConfigByKeyIncludingDisabled(configs []OpenAIAccountLevelConfig, key string) *OpenAIAccountLevelConfig {
	return OpenAIAccountLevelConfigByKeyWithEnabled(configs, key, false)
}

func OpenAIAccountLevelConfigByKeyWithEnabled(configs []OpenAIAccountLevelConfig, key string, requireEnabled bool) *OpenAIAccountLevelConfig {
	normalized := NormalizeAccountLevel(key)
	for _, cfg := range NormalizeOpenAIAccountLevelConfigs(configs) {
		if cfg.Key == normalized && (!requireEnabled || cfg.Enabled) {
			candidate := cfg
			return &candidate
		}
	}
	return nil
}

func openAIAccountLevelAliasTokens(cfg OpenAIAccountLevelConfig) []string {
	return NormalizeOpenAIAccountLevelConfigs([]OpenAIAccountLevelConfig{cfg})[0].Aliases
}

func normalizeOpenAIPlanAliasToken(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.NewReplacer(" ", "", "-", "", "_", "").Replace(normalized)
	return normalized
}

func normalizeOpenAIPlanAliasPattern(value string) string {
	trimmed := strings.TrimSpace(value)
	hasWildcard := strings.HasSuffix(trimmed, "*")
	if hasWildcard {
		trimmed = strings.TrimSuffix(trimmed, "*")
	}
	normalized := normalizeOpenAIPlanAliasToken(trimmed)
	if normalized == "" {
		return ""
	}
	if hasWildcard {
		return normalized + "*"
	}
	return normalized
}

func matchOpenAIPlanAliasToken(token, pattern string) bool {
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return prefix != "" && strings.HasPrefix(token, prefix)
	}
	return token == pattern
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func DefaultOAuthAccountConcurrencyForPlatform(platform string) int {
	if platform == PlatformOpenAI {
		return OpenAIPlusDefaultConcurrency
	}
	return OAuthAccountDefaultConcurrency
}

func NormalizeOpenAIPlusConcurrency(platform, accountLevel string, concurrency int) (int, error) {
	if !IsOpenAIPlusAccount(platform, accountLevel) {
		return concurrency, nil
	}
	if concurrency <= 0 {
		return OpenAIPlusDefaultConcurrency, nil
	}
	return concurrency, nil
}

func ValidateOpenAIPlusConcurrency(platform, accountLevel string, concurrency int) error {
	if !IsOpenAIPlusAccount(platform, accountLevel) {
		return nil
	}
	if concurrency <= 0 {
		return fmt.Errorf("openai plus account concurrency must be > 0")
	}
	return nil
}

func ValidateAccountLoadFactor(loadFactor *int) error {
	if loadFactor == nil || *loadFactor <= 0 {
		return nil
	}
	if *loadFactor > AccountMaxLoadFactor {
		return fmt.Errorf("load_factor must be <= %d", AccountMaxLoadFactor)
	}
	return nil
}

func NormalizeAccountShareMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case AccountShareModePublic:
		return AccountShareModePublic
	default:
		return AccountShareModePrivate
	}
}

func NormalizeAccountShareStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case AccountShareStatusPending:
		return AccountShareStatusPending
	case AccountShareStatusSuspended:
		return AccountShareStatusSuspended
	default:
		return AccountShareStatusApproved
	}
}

func (a *Account) IsPublicShareApproved() bool {
	return a != nil &&
		a.OwnerUserID != nil &&
		NormalizeAccountShareMode(a.ShareMode) == AccountShareModePublic &&
		NormalizeAccountShareStatus(a.ShareStatus) == AccountShareStatusApproved
}

func (a *Account) IsVisibleToConsumer(userID int64) bool {
	if a == nil {
		return false
	}
	if a.OwnerUserID == nil {
		return true
	}
	if userID > 0 && *a.OwnerUserID == userID {
		return true
	}
	return a.IsPublicShareApproved()
}

type TempUnschedulableRule struct {
	ErrorCode       int      `json:"error_code"`
	Keywords        []string `json:"keywords"`
	DurationMinutes int      `json:"duration_minutes"`
	Description     string   `json:"description"`
}

func (a *Account) IsActive() bool {
	return a.Status == StatusActive
}

// BillingRateMultiplier 返回账号计费倍率。
// - nil 表示未配置/旧缓存缺字段，按 1.0 处理
// - 允许 0，表示该账号计费为 0
// - 负数属于非法数据，出于安全考虑按 1.0 处理
func (a *Account) BillingRateMultiplier() float64 {
	if a == nil || a.RateMultiplier == nil {
		return 1.0
	}
	if *a.RateMultiplier < 0 {
		return 1.0
	}
	return *a.RateMultiplier
}

func (a *Account) EffectiveLoadFactor() int {
	if a == nil {
		return 1
	}
	if a.LoadFactor != nil && *a.LoadFactor > 0 {
		return *a.LoadFactor
	}
	if a.Concurrency > 0 {
		return a.Concurrency
	}
	return 1
}

func (a *Account) IsSchedulable() bool {
	return a.IsSchedulableAt(time.Now())
}

func (a *Account) IsSchedulableAt(now time.Time) bool {
	return a.isSchedulableAt(now, true)
}

func (a *Account) IsSchedulableWithoutCodexQuotaProtection() bool {
	return a.isSchedulableAt(time.Now(), false)
}

func (a *Account) isSchedulableAt(now time.Time, includeCodexQuotaProtection bool) bool {
	if !a.IsActive() || !a.Schedulable {
		return false
	}
	if a.AutoPauseOnExpired && a.ExpiresAt != nil && !now.Before(*a.ExpiresAt) {
		return false
	}
	if a.OverloadUntil != nil && now.Before(*a.OverloadUntil) {
		return false
	}
	if a.RateLimitResetAt != nil && now.Before(*a.RateLimitResetAt) {
		return false
	}
	if includeCodexQuotaProtection && (a.IsCodexQuotaProtectionActiveAt(now) || a.IsAnthropicQuotaProtectionActiveAt(now) || a.IsOpencodeQuotaProtectionActiveAt(now)) {
		return false
	}
	if a.TempUnschedulableUntil != nil && now.Before(*a.TempUnschedulableUntil) {
		return false
	}
	if paused, _ := shouldAutoPauseGrokAccountByQuotaAt(a, now); paused {
		return false
	}
	if a.IsAPIKeyOrBedrock() && a.IsQuotaExceededAt(now) {
		return false
	}
	return true
}

func (a *Account) IsRateLimited() bool {
	return a.IsRateLimitedAt(time.Now())
}

func (a *Account) IsRateLimitedAt(now time.Time) bool {
	if a.RateLimitResetAt == nil {
		return false
	}
	return now.Before(*a.RateLimitResetAt)
}

func (a *Account) IsOverloaded() bool {
	return a.IsOverloadedAt(time.Now())
}

func (a *Account) IsOverloadedAt(now time.Time) bool {
	if a.OverloadUntil == nil {
		return false
	}
	return now.Before(*a.OverloadUntil)
}

func (a *Account) IsOAuth() bool {
	return a.Type == AccountTypeOAuth || a.Type == AccountTypeSetupToken
}

// IsPrivacySet 检查账号的 privacy 是否已成功设置。
// OpenAI: privacy_mode == "training_off"
// Antigravity: privacy_mode == "privacy_set"
// 其他平台: 无 privacy 概念，始终返回 true
func (a *Account) IsPrivacySet() bool {
	switch a.Platform {
	case PlatformOpenAI:
		return a.getExtraString("privacy_mode") == PrivacyModeTrainingOff
	case PlatformAntigravity:
		return a.getExtraString("privacy_mode") == AntigravityPrivacySet
	default:
		return true
	}
}

func (a *Account) IsGemini() bool {
	return a.Platform == PlatformGemini
}

func (a *Account) IsGrok() bool {
	return a.Platform == PlatformGrok
}

func (a *Account) IsGrokOAuth() bool {
	return a.IsGrok() && a.Type == AccountTypeOAuth
}

func (a *Account) IsOpenAICompatible() bool {
	return a != nil && (a.Platform == PlatformOpenAI || a.Platform == PlatformGrok || a.Platform == PlatformOpencode)
}

func (a *Account) GeminiOAuthType() string {
	if a.Platform != PlatformGemini || a.Type != AccountTypeOAuth {
		return ""
	}
	oauthType := strings.TrimSpace(a.GetCredential("oauth_type"))
	if oauthType == "" && strings.TrimSpace(a.GetCredential("project_id")) != "" {
		return "code_assist"
	}
	return oauthType
}

func (a *Account) GeminiTierID() string {
	tierID := strings.TrimSpace(a.GetCredential("tier_id"))
	return tierID
}

func (a *Account) IsGeminiCodeAssist() bool {
	if a.Platform != PlatformGemini || a.Type != AccountTypeOAuth {
		return false
	}
	oauthType := a.GeminiOAuthType()
	if oauthType == "" {
		return strings.TrimSpace(a.GetCredential("project_id")) != ""
	}
	return oauthType == "code_assist"
}

// IsGeminiGoogleOne reports whether the account uses the legacy consumer
// Google One OAuth channel.
func (a *Account) IsGeminiGoogleOne() bool {
	return a != nil && a.Platform == PlatformGemini && a.Type == AccountTypeOAuth && a.GeminiOAuthType() == "google_one"
}

func (a *Account) CanGetUsage() bool {
	return a.Type == AccountTypeOAuth
}

func (a *Account) GetCredential(key string) string {
	if a.Credentials == nil {
		return ""
	}
	v, ok := a.Credentials[key]
	if !ok || v == nil {
		return ""
	}

	// 支持多种类型（兼容历史数据中 expires_at 等字段可能是数字或字符串）
	switch val := v.(type) {
	case string:
		return val
	case json.Number:
		// GORM datatypes.JSONMap 使用 UseNumber() 解析，数字类型为 json.Number
		return val.String()
	case float64:
		// JSON 解析后数字默认为 float64
		return strconv.FormatInt(int64(val), 10)
	case int64:
		return strconv.FormatInt(val, 10)
	case int:
		return strconv.Itoa(val)
	default:
		return ""
	}
}

// GetCredentialAsTime 解析凭证中的时间戳字段，支持多种格式
// 兼容以下格式：
//   - RFC3339 字符串: "2025-01-01T00:00:00Z"
//   - Unix 时间戳字符串: "1735689600"
//   - Unix 时间戳数字: 1735689600 (float64/int64/json.Number)
func (a *Account) GetCredentialAsTime(key string) *time.Time {
	s := a.GetCredential(key)
	if s == "" {
		return nil
	}
	// 尝试 RFC3339 格式
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return &t
	}
	// 尝试 Unix 时间戳（纯数字字符串）
	if ts, err := strconv.ParseInt(s, 10, 64); err == nil {
		t := time.Unix(ts, 0)
		return &t
	}
	return nil
}

// GetCredentialAsInt64 解析凭证中的 int64 字段
// 用于读取 _token_version 等内部字段
func (a *Account) GetCredentialAsInt64(key string) int64 {
	if a == nil || a.Credentials == nil {
		return 0
	}
	val, ok := a.Credentials[key]
	if !ok || val == nil {
		return 0
	}
	switch v := val.(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	case int:
		return int64(v)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i
		}
	case string:
		if i, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return i
		}
	}
	return 0
}

func (a *Account) IsTempUnschedulableEnabled() bool {
	if a.Credentials == nil {
		return false
	}
	raw, ok := a.Credentials["temp_unschedulable_enabled"]
	if !ok || raw == nil {
		return false
	}
	enabled, ok := raw.(bool)
	return ok && enabled
}

func (a *Account) GetTempUnschedulableRules() []TempUnschedulableRule {
	if a.Credentials == nil {
		return nil
	}
	raw, ok := a.Credentials["temp_unschedulable_rules"]
	if !ok || raw == nil {
		return nil
	}

	arr, ok := raw.([]any)
	if !ok {
		return nil
	}

	rules := make([]TempUnschedulableRule, 0, len(arr))
	for _, item := range arr {
		entry, ok := item.(map[string]any)
		if !ok || entry == nil {
			continue
		}

		rule := TempUnschedulableRule{
			ErrorCode:       parseTempUnschedInt(entry["error_code"]),
			Keywords:        parseTempUnschedStrings(entry["keywords"]),
			DurationMinutes: parseTempUnschedInt(entry["duration_minutes"]),
			Description:     parseTempUnschedString(entry["description"]),
		}

		if rule.ErrorCode <= 0 || rule.DurationMinutes <= 0 || len(rule.Keywords) == 0 {
			continue
		}

		rules = append(rules, rule)
	}

	return rules
}

func parseTempUnschedString(value any) string {
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func parseTempUnschedStrings(value any) []string {
	if value == nil {
		return nil
	}

	var raw []string
	switch v := value.(type) {
	case []string:
		raw = v
	case []any:
		raw = make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				raw = append(raw, s)
			}
		}
	default:
		return nil
	}

	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s := strings.TrimSpace(item)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func normalizeAccountNotes(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func parseTempUnschedInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i)
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return i
		}
	}
	return 0
}

const (
	// OpenAICompactModeAuto follows compact-probe results when deciding compact eligibility.
	OpenAICompactModeAuto = "auto"
	// OpenAICompactModeForceOn always treats the account as compact-supported.
	OpenAICompactModeForceOn = "force_on"
	// OpenAICompactModeForceOff always treats the account as compact-unsupported.
	OpenAICompactModeForceOff = "force_off"
)

func normalizeOpenAICompactMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case OpenAICompactModeForceOn:
		return OpenAICompactModeForceOn
	case OpenAICompactModeForceOff:
		return OpenAICompactModeForceOff
	default:
		return OpenAICompactModeAuto
	}
}

func stringMappingFromRaw(raw any) map[string]string {
	switch mapping := raw.(type) {
	case map[string]any:
		if len(mapping) == 0 {
			return nil
		}
		result := make(map[string]string, len(mapping))
		for key, value := range mapping {
			if str, ok := value.(string); ok {
				result[key] = str
			}
		}
		if len(result) == 0 {
			return nil
		}
		return result
	case map[string]string:
		if len(mapping) == 0 {
			return nil
		}
		result := make(map[string]string, len(mapping))
		for key, value := range mapping {
			result[key] = value
		}
		return result
	default:
		return nil
	}
}

func (a *Account) GetModelMapping() map[string]string {
	credentialsPtr := mapPtr(a.Credentials)
	rawMapping, _ := a.Credentials["model_mapping"].(map[string]any)
	rawPtr := mapPtr(rawMapping)
	rawLen := len(rawMapping)
	rawSig := uint64(0)
	rawSigReady := false
	runtimeVersion := xai.RuntimeModelMappingVersion()

	if a.modelMappingCacheReady &&
		a.modelMappingCacheCredentialsPtr == credentialsPtr &&
		a.modelMappingCacheRawPtr == rawPtr &&
		a.modelMappingCacheRawLen == rawLen &&
		a.modelMappingCacheRuntimeVersion == runtimeVersion {
		rawSig = modelMappingSignature(rawMapping)
		rawSigReady = true
		if a.modelMappingCacheRawSig == rawSig {
			return a.modelMappingCache
		}
	}

	mapping := a.resolveModelMapping(rawMapping)
	if !rawSigReady {
		rawSig = modelMappingSignature(rawMapping)
	}

	a.modelMappingCache = mapping
	a.modelMappingCacheReady = true
	a.modelMappingCacheCredentialsPtr = credentialsPtr
	a.modelMappingCacheRawPtr = rawPtr
	a.modelMappingCacheRawLen = rawLen
	a.modelMappingCacheRawSig = rawSig
	a.modelMappingCacheRuntimeVersion = runtimeVersion
	return mapping
}

func (a *Account) resolveModelMapping(rawMapping map[string]any) map[string]string {
	// 个人账号的模型集合是号主的严格白名单：不得为 Grok/Antigravity
	// 注入平台默认映射，也不得把缺失/空映射解释成全开放。
	if a.OwnerUserID != nil {
		if a.Credentials == nil {
			return nil
		}
		return stringMappingFromRaw(a.Credentials["model_mapping"])
	}
	if a.Credentials == nil {
		// Antigravity 平台使用默认映射
		if a.Platform == domain.PlatformAntigravity {
			return domain.DefaultAntigravityModelMapping
		}
		if a.Platform == domain.PlatformGrok {
			return xai.DefaultModelMapping()
		}
		// Bedrock 默认映射由 forwardBedrock 统一处理（需配合 region prefix 调整）
		return nil
	}
	if len(rawMapping) == 0 {
		if a.IsGeminiGoogleOne() {
			return geminicli.GoogleOneModelMapping()
		}
		// Antigravity 平台使用默认映射
		if a.Platform == domain.PlatformAntigravity {
			return domain.DefaultAntigravityModelMapping
		}
		if a.Platform == domain.PlatformGrok {
			return xai.DefaultModelMapping()
		}
		return nil
	}

	result := make(map[string]string)
	for k, v := range rawMapping {
		if s, ok := v.(string); ok {
			result[k] = s
		}
	}
	if len(result) > 0 {
		if a.Platform == domain.PlatformAntigravity {
			ensureAntigravityDefaultPassthroughs(result, []string{
				"gemini-3-flash",
				"gemini-3.1-pro-high",
				"gemini-3.1-pro-low",
			})
		}
		return result
	}

	// Antigravity 平台使用默认映射
	if a.IsGeminiGoogleOne() {
		// Google One 账号即使历史凭证中的 model_mapping 结构损坏，
		// 也必须回退到保守目录，不能把空映射解释成平台账号的全开放模式。
		return geminicli.GoogleOneModelMapping()
	}
	if a.Platform == domain.PlatformAntigravity {
		return domain.DefaultAntigravityModelMapping
	}
	if a.Platform == domain.PlatformGrok {
		return xai.DefaultModelMapping()
	}
	return nil
}

func mapPtr(m map[string]any) uintptr {
	if m == nil {
		return 0
	}
	return reflect.ValueOf(m).Pointer()
}

func modelMappingSignature(rawMapping map[string]any) uint64 {
	if len(rawMapping) == 0 {
		return 0
	}
	keys := make([]string, 0, len(rawMapping))
	for k := range rawMapping {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := fnv.New64a()
	for _, k := range keys {
		_, _ = h.Write([]byte(k))
		_, _ = h.Write([]byte{0})
		if v, ok := rawMapping[k].(string); ok {
			_, _ = h.Write([]byte(v))
		} else {
			_, _ = h.Write([]byte{1})
		}
		_, _ = h.Write([]byte{0xff})
	}
	return h.Sum64()
}

func ensureAntigravityDefaultPassthrough(mapping map[string]string, model string) {
	if mapping == nil || model == "" {
		return
	}
	if _, exists := mapping[model]; exists {
		return
	}
	for pattern := range mapping {
		if matchWildcard(pattern, model) {
			return
		}
	}
	mapping[model] = model
}

func ensureAntigravityDefaultPassthroughs(mapping map[string]string, models []string) {
	for _, model := range models {
		ensureAntigravityDefaultPassthrough(mapping, model)
	}
}

func normalizeRequestedModelForLookup(platform, requestedModel string) string {
	trimmed := strings.TrimSpace(requestedModel)
	if trimmed == "" {
		return ""
	}
	// Claude Code 用 "[1m]" 表示 1M 上下文选择，属于客户端侧语法而非模型 ID。
	// 任何平台都不应拿带 [1m] 的模型名去做 model_mapping 精确匹配，否则
	// 会因匹配不到裸 slug（如 deepseek-v4-flash）而误判 model_not_found。
	trimmed = normalizeClaudeCodeLongContextModel(trimmed)
	if platform != PlatformGemini && platform != PlatformAntigravity {
		return trimmed
	}
	if trimmed == "gemini-3.1-pro-preview-customtools" {
		return "gemini-3.1-pro-preview"
	}
	return trimmed
}

func mappingSupportsRequestedModel(mapping map[string]string, requestedModel string) bool {
	if requestedModel == "" {
		return false
	}
	if _, exists := mapping[requestedModel]; exists {
		return true
	}
	for pattern := range mapping {
		if matchWildcard(pattern, requestedModel) {
			return true
		}
	}
	return false
}

func resolveRequestedModelInMapping(mapping map[string]string, requestedModel string) (mappedModel string, matched bool) {
	if requestedModel == "" {
		return "", false
	}
	if mappedModel, exists := mapping[requestedModel]; exists {
		return mappedModel, true
	}
	return matchWildcardMappingResult(mapping, requestedModel)
}

// IsModelSupported 检查模型是否在 model_mapping 中（支持通配符）。
// 平台账号未配置 mapping 时保持历史兼容（允许所有）；个人账号的空白名单拒绝全部。
func (a *Account) IsModelSupported(requestedModel string) bool {
	mapping := a.GetModelMapping()
	if len(mapping) == 0 {
		return a.OwnerUserID == nil
	}
	if mappingSupportsRequestedModel(mapping, requestedModel) {
		return true
	}
	normalized := normalizeRequestedModelForLookup(a.Platform, requestedModel)
	return normalized != requestedModel && mappingSupportsRequestedModel(mapping, normalized)
}

// GetMappedModel 获取映射后的模型名（支持通配符，最长优先匹配）
// 如果未配置 mapping，返回原始模型名
func (a *Account) GetMappedModel(requestedModel string) string {
	mappedModel, _ := a.ResolveMappedModel(requestedModel)
	return mappedModel
}

// ResolveMappedModel 获取映射后的模型名，并返回是否命中了账号级映射。
// matched=true 表示命中了精确映射或通配符映射，即使映射结果与原模型名相同。
func (a *Account) ResolveMappedModel(requestedModel string) (mappedModel string, matched bool) {
	mapping := a.GetModelMapping()
	if len(mapping) == 0 {
		return requestedModel, false
	}
	if mappedModel, matched := resolveRequestedModelInMapping(mapping, requestedModel); matched {
		return mappedModel, true
	}
	normalized := normalizeRequestedModelForLookup(a.Platform, requestedModel)
	if normalized != requestedModel {
		if mappedModel, matched := resolveRequestedModelInMapping(mapping, normalized); matched {
			return mappedModel, true
		}
	}
	return requestedModel, false
}

// GetOpenAICompactMode returns the compact routing mode for an OpenAI account.
// Missing or invalid values fall back to "auto".
func (a *Account) GetOpenAICompactMode() string {
	if a == nil || !a.IsOpenAI() || a.Extra == nil {
		return OpenAICompactModeAuto
	}
	mode, _ := a.Extra["openai_compact_mode"].(string)
	return normalizeOpenAICompactMode(mode)
}

// OpenAICompactSupportKnown reports whether compact capability is known for this
// account and, when known, whether it is supported.
func (a *Account) OpenAICompactSupportKnown() (supported bool, known bool) {
	if a == nil || !a.IsOpenAI() {
		return false, false
	}

	switch a.GetOpenAICompactMode() {
	case OpenAICompactModeForceOn:
		return true, true
	case OpenAICompactModeForceOff:
		return false, true
	}

	if a.Extra == nil {
		return false, false
	}
	supported, ok := a.Extra["openai_compact_supported"].(bool)
	if !ok {
		return false, false
	}
	return supported, true
}

// AllowsOpenAICompact reports whether the account may be considered for compact
// requests. Unknown capability remains allowed to avoid breaking older accounts
// before an explicit probe has been run.
func (a *Account) AllowsOpenAICompact() bool {
	if a == nil || !a.IsOpenAI() {
		return false
	}
	supported, known := a.OpenAICompactSupportKnown()
	if !known {
		return true
	}
	return supported
}

// GetCompactModelMapping returns compact-only model remapping configuration.
// This mapping is intended for /responses/compact only and does not affect
// normal /responses traffic.
func (a *Account) GetCompactModelMapping() map[string]string {
	if a == nil || a.Credentials == nil {
		return nil
	}
	return stringMappingFromRaw(a.Credentials["compact_model_mapping"])
}

// ResolveCompactMappedModel resolves compact-only model remapping and reports
// whether a compact-specific mapping rule matched.
func (a *Account) ResolveCompactMappedModel(requestedModel string) (mappedModel string, matched bool) {
	mapping := a.GetCompactModelMapping()
	if len(mapping) == 0 {
		return requestedModel, false
	}
	if mappedModel, matched := resolveRequestedModelInMapping(mapping, requestedModel); matched {
		return mappedModel, true
	}
	return requestedModel, false
}

func (a *Account) GetBaseURL() string {
	if a.Type != AccountTypeAPIKey {
		return ""
	}
	baseURL := a.GetCredential("base_url")
	if baseURL == "" {
		return "https://api.anthropic.com"
	}
	if a.Platform == PlatformAntigravity {
		return strings.TrimRight(baseURL, "/") + "/antigravity"
	}
	return baseURL
}

// GetGeminiBaseURL 返回 Gemini 兼容端点的 base URL。
// Antigravity 平台的 APIKey 账号自动拼接 /antigravity。
func (a *Account) GetGeminiBaseURL(defaultBaseURL string) string {
	baseURL := strings.TrimSpace(a.GetCredential("base_url"))
	if baseURL == "" {
		return defaultBaseURL
	}
	if a.Platform == PlatformAntigravity && a.Type == AccountTypeAPIKey {
		return strings.TrimRight(baseURL, "/") + "/antigravity"
	}
	return baseURL
}

func (a *Account) GetExtraString(key string) string {
	if a.Extra == nil {
		return ""
	}
	if v, ok := a.Extra[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (a *Account) GetClaudeUserID() string {
	if v := strings.TrimSpace(a.GetExtraString("claude_user_id")); v != "" {
		return v
	}
	if v := strings.TrimSpace(a.GetExtraString("anthropic_user_id")); v != "" {
		return v
	}
	if v := strings.TrimSpace(a.GetCredential("claude_user_id")); v != "" {
		return v
	}
	if v := strings.TrimSpace(a.GetCredential("anthropic_user_id")); v != "" {
		return v
	}
	return ""
}

func (a *Account) GetClaudeOrgUUID() string {
	if v := strings.TrimSpace(a.GetExtraString("org_uuid")); v != "" {
		return v
	}
	return strings.TrimSpace(a.GetCredential("org_uuid"))
}

func (a *Account) GetClaudeAccountUUID() string {
	if v := strings.TrimSpace(a.GetExtraString("account_uuid")); v != "" {
		return v
	}
	return strings.TrimSpace(a.GetCredential("account_uuid"))
}

// matchAntigravityWildcard 通配符匹配（仅支持末尾 *）
// 用于 model_mapping 的通配符匹配
func matchAntigravityWildcard(pattern, str string) bool {
	if strings.HasSuffix(pattern, "*") {
		prefix := pattern[:len(pattern)-1]
		return strings.HasPrefix(str, prefix)
	}
	return pattern == str
}

// matchWildcard 通用通配符匹配（仅支持末尾 *）
// 复用 Antigravity 的通配符逻辑，供其他平台使用
func matchWildcard(pattern, str string) bool {
	return matchAntigravityWildcard(pattern, str)
}

func matchWildcardMappingResult(mapping map[string]string, requestedModel string) (string, bool) {
	// 收集所有匹配的 pattern，按长度降序排序（最长优先）
	type patternMatch struct {
		pattern string
		target  string
	}
	var matches []patternMatch

	for pattern, target := range mapping {
		if matchWildcard(pattern, requestedModel) {
			matches = append(matches, patternMatch{pattern, target})
		}
	}

	if len(matches) == 0 {
		return requestedModel, false // 无匹配，返回原始模型名
	}

	// 按 pattern 长度降序排序
	sort.Slice(matches, func(i, j int) bool {
		if len(matches[i].pattern) != len(matches[j].pattern) {
			return len(matches[i].pattern) > len(matches[j].pattern)
		}
		return matches[i].pattern < matches[j].pattern
	})

	return matches[0].target, true
}

func (a *Account) IsCustomErrorCodesEnabled() bool {
	if a.Type != AccountTypeAPIKey || a.Credentials == nil {
		return false
	}
	if v, ok := a.Credentials["custom_error_codes_enabled"]; ok {
		if enabled, ok := v.(bool); ok {
			return enabled
		}
	}
	return false
}

// IsPoolMode 检查 API Key 账号是否启用池模式。
// 池模式下，上游错误不标记本地账号状态，而是在同一账号上重试。
func (a *Account) IsPoolMode() bool {
	if !a.IsAPIKeyOrBedrock() || a.Credentials == nil {
		return false
	}
	if v, ok := a.Credentials["pool_mode"]; ok {
		if enabled, ok := v.(bool); ok {
			return enabled
		}
	}
	return false
}

const (
	defaultPoolModeRetryCount = 3
	maxPoolModeRetryCount     = 10
)

// GetPoolModeRetryCount 返回池模式同账号重试次数。
// 未配置或配置非法时回退为默认值 3；小于 0 按 0 处理；过大则截断到 10。
func (a *Account) GetPoolModeRetryCount() int {
	if a == nil || !a.IsPoolMode() || a.Credentials == nil {
		return defaultPoolModeRetryCount
	}
	raw, ok := a.Credentials["pool_mode_retry_count"]
	if !ok || raw == nil {
		return defaultPoolModeRetryCount
	}
	count := parsePoolModeRetryCount(raw)
	if count < 0 {
		return 0
	}
	if count > maxPoolModeRetryCount {
		return maxPoolModeRetryCount
	}
	return count
}

func parsePoolModeRetryCount(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i)
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return i
		}
	}
	return defaultPoolModeRetryCount
}

// isPoolModeRetryableStatus 池模式下应触发同账号重试的状态码
func isPoolModeRetryableStatus(statusCode int) bool {
	switch statusCode {
	case 401, 403, 429:
		return true
	default:
		return false
	}
}

// GetPoolModeRetryStatusCodes returns account-level retryable statuses for pool-mode retries.
func (a *Account) GetPoolModeRetryStatusCodes() []int {
	if a == nil || a.Credentials == nil {
		return nil
	}
	raw, ok := a.Credentials["pool_mode_retry_status_codes"]
	if !ok || raw == nil {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	seen := make(map[int]struct{}, len(arr))
	codes := make([]int, 0, len(arr))
	for _, v := range arr {
		var code int
		switch n := v.(type) {
		case float64:
			code = int(n)
		case int:
			code = n
		case int64:
			code = int(n)
		case json.Number:
			i, err := n.Int64()
			if err != nil {
				continue
			}
			code = int(i)
		case string:
			i, err := strconv.Atoi(strings.TrimSpace(n))
			if err != nil {
				continue
			}
			code = i
		default:
			continue
		}
		if code < 100 || code > 599 {
			continue
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	sort.Ints(codes)
	return codes
}

// IsPoolModeRetryableStatus checks account-level pool-mode retry statuses.
func (a *Account) IsPoolModeRetryableStatus(statusCode int) bool {
	codes := a.GetPoolModeRetryStatusCodes()
	if codes == nil {
		return isPoolModeRetryableStatus(statusCode)
	}
	for _, code := range codes {
		if code == statusCode {
			return true
		}
	}
	return false
}

func (a *Account) GetCustomErrorCodes() []int {
	if a.Credentials == nil {
		return nil
	}
	raw, ok := a.Credentials["custom_error_codes"]
	if !ok || raw == nil {
		return nil
	}
	if arr, ok := raw.([]any); ok {
		result := make([]int, 0, len(arr))
		for _, v := range arr {
			if f, ok := v.(float64); ok {
				result = append(result, int(f))
			}
		}
		return result
	}
	return nil
}

func (a *Account) ShouldHandleErrorCode(statusCode int) bool {
	if !a.IsCustomErrorCodesEnabled() {
		return true
	}
	codes := a.GetCustomErrorCodes()
	if len(codes) == 0 {
		return true
	}
	for _, code := range codes {
		if code == statusCode {
			return true
		}
	}
	return false
}

func (a *Account) IsInterceptWarmupEnabled() bool {
	if a.Credentials == nil {
		return false
	}
	if v, ok := a.Credentials["intercept_warmup_requests"]; ok {
		if enabled, ok := v.(bool); ok {
			return enabled
		}
	}
	return false
}

func (a *Account) IsBedrock() bool {
	return a.Platform == PlatformAnthropic && a.Type == AccountTypeBedrock
}

func (a *Account) IsBedrockAPIKey() bool {
	return a.IsBedrock() && a.GetCredential("auth_mode") == "apikey"
}

// IsAPIKeyOrBedrock 返回账号类型是否支持配额和池模式等特性
func (a *Account) IsAPIKeyOrBedrock() bool {
	return a.Type == AccountTypeAPIKey || a.Type == AccountTypeBedrock
}

func (a *Account) IsOpenAI() bool {
	return a.Platform == PlatformOpenAI
}

func (a *Account) IsOpencode() bool {
	return a != nil && a.Platform == PlatformOpencode
}

func (a *Account) IsOpencodeApiKey() bool {
	return a.IsOpencode() && a.Type == AccountTypeAPIKey
}

func (a *Account) IsAnthropic() bool {
	return a.Platform == PlatformAnthropic
}

func (a *Account) IsOpenAIOAuth() bool {
	return a.IsOpenAI() && a.Type == AccountTypeOAuth
}

// IsOpenAIPersonalAccessTokenCredentials reports whether credentials select
// Codex Personal Access Token authentication. Platform and account-type checks
// remain the caller's responsibility while create/import input is validated.
func IsOpenAIPersonalAccessTokenCredentials(credentials map[string]any) bool {
	if len(credentials) == 0 {
		return false
	}
	return isOpenAIPersonalAccessTokenAuthMode(openAICredentialString(credentials[openAIAuthModeCredentialKey])) ||
		isOpenAIPersonalAccessTokenAuthMode(openAICredentialString(credentials[openAIAuthModeLegacyCredentialKey]))
}

// IsOpenAIPersonalAccessToken reports whether the OpenAI OAuth account uses a
// non-refreshable Codex at-* personal access token.
func (a *Account) IsOpenAIPersonalAccessToken() bool {
	return a != nil && a.IsOpenAIOAuth() && IsOpenAIPersonalAccessTokenCredentials(a.Credentials)
}

// IsOpenAIAgentIdentityCredentials reports whether credentials select the
// Codex Agent Identity authentication mode. Platform and account-type checks
// remain the caller's responsibility so this helper can also be used while a
// create/import request is still being validated.
func IsOpenAIAgentIdentityCredentials(credentials map[string]any) bool {
	if len(credentials) == 0 {
		return false
	}
	authMode, ok := credentials["auth_mode"].(string)
	return ok && strings.EqualFold(strings.TrimSpace(authMode), OpenAIAuthModeAgentIdentity)
}

// IsOpenAIAgentIdentity reports whether the account uses Codex Agent Identity
// credentials instead of a refreshable OpenAI OAuth token.
func (a *Account) IsOpenAIAgentIdentity() bool {
	if a == nil || !a.IsOpenAIOAuth() {
		return false
	}
	return IsOpenAIAgentIdentityCredentials(a.Credentials)
}

// ValidateOpenAIAgentIdentityPrivateKey validates the base64-encoded PKCS#8
// Ed25519 private key without returning or logging any key material.
func ValidateOpenAIAgentIdentityPrivateKey(encoded string) error {
	_, err := parseAgentIdentityPrivateKey(encoded)
	return err
}

type accountCredentialFieldExistenceChecker interface {
	ExistsByCredentialField(ctx context.Context, key, value string) (bool, error)
}

func credentialFieldExists(ctx context.Context, repository any, key, value string) (bool, error) {
	checker, ok := repository.(accountCredentialFieldExistenceChecker)
	if !ok {
		return false, errors.New("account repository does not support credential field lookup")
	}
	return checker.ExistsByCredentialField(ctx, key, value)
}

// OpenAIAgentIdentityRuntimeIDExists performs a narrow JSONB lookup.
// Repositories that cannot provide the lookup are rejected instead of falling
// back to loading and scanning every account.
func (s *AccountService) OpenAIAgentIdentityRuntimeIDExists(ctx context.Context, runtimeID string) (bool, error) {
	runtimeID = strings.TrimSpace(runtimeID)
	if runtimeID == "" {
		return false, errors.New("agent identity runtime id is required")
	}
	if s == nil || s.accountRepo == nil {
		return false, errors.New("account repository is required for Agent Identity duplicate detection")
	}
	return credentialFieldExists(ctx, s.accountRepo, "agent_runtime_id", runtimeID)
}

func (a *Account) GetCodex5hLimitPercent() float64 {
	return a.getCodexQuotaLimitPercent("codex_5h_limit_percent")
}

func (a *Account) GetCodex7dLimitPercent() float64 {
	return a.getCodexQuotaLimitPercent("codex_7d_limit_percent")
}

func (a *Account) GetAnthropic5hLimitPercent() float64 {
	return a.getAnthropicQuotaLimitPercent("anthropic_5h_limit_percent")
}

func (a *Account) GetAnthropic7dLimitPercent() float64 {
	return a.getAnthropicQuotaLimitPercent("anthropic_7d_limit_percent")
}

func (a *Account) getCodexQuotaLimitPercent(key string) float64 {
	if a == nil || a.Extra == nil {
		return CodexQuotaDefaultLimitPercent
	}
	raw, ok := a.Extra[key]
	if !ok || raw == nil {
		return CodexQuotaDefaultLimitPercent
	}
	limit := parseExtraFloat64(raw)
	if limit < CodexQuotaMinLimitPercent || limit > CodexQuotaMaxLimitPercent {
		return CodexQuotaDefaultLimitPercent
	}
	return limit
}

func (a *Account) getAnthropicQuotaLimitPercent(key string) float64 {
	if a == nil || a.Extra == nil {
		return AnthropicQuotaDefaultLimitPercent
	}
	raw, ok := a.Extra[key]
	if !ok || raw == nil {
		return AnthropicQuotaDefaultLimitPercent
	}
	limit := parseExtraFloat64(raw)
	if limit < AnthropicQuotaMinLimitPercent || limit > AnthropicQuotaMaxLimitPercent {
		return AnthropicQuotaDefaultLimitPercent
	}
	return limit
}

func (a *Account) IsCodexQuotaProtectionActiveAt(now time.Time) bool {
	return a.CodexQuotaProtectionReasonAt(now) != ""
}

func (a *Account) IsAnthropicQuotaProtectionActiveAt(now time.Time) bool {
	return a.AnthropicQuotaProtectionReasonAt(now) != ""
}

func (a *Account) CodexQuotaProtectionReasonAt(now time.Time) string {
	reason, _ := a.codexQuotaProtectionWindowAt(now)
	return reason
}

func (a *Account) AnthropicQuotaProtectionReasonAt(now time.Time) string {
	reason, _ := a.anthropicQuotaProtectionWindowAt(now)
	return reason
}

func (a *Account) CodexQuotaProtectionResetAt(now time.Time) *time.Time {
	_, resetAt := a.codexQuotaProtectionWindowAt(now)
	return resetAt
}

func (a *Account) AnthropicQuotaProtectionResetAt(now time.Time) *time.Time {
	_, resetAt := a.anthropicQuotaProtectionWindowAt(now)
	return resetAt
}

func (a *Account) CodexUsageProgress(window string, now time.Time) *UsageProgress {
	if a == nil || !a.IsOpenAIOAuth() {
		return nil
	}
	return buildCodexUsageProgressFromExtra(a.Extra, window, now)
}

func (a *Account) AnthropicUsageProgress(window string, now time.Time) *UsageProgress {
	if a == nil || !a.IsAnthropicOAuthOrSetupToken() {
		return nil
	}
	switch window {
	case AnthropicQuotaWindow5h:
		resetAt, _ := a.anthropic5hResetAt()
		return buildAnthropicUsageProgressFromExtra(a.Extra, "session_window_utilization", resetAt, now)
	case AnthropicQuotaWindow7d:
		resetAt, _ := anthropicQuotaResetAtFromExtra(a.Extra, "anthropic_7d_reset_at", "passive_usage_7d_reset")
		return buildAnthropicUsageProgressFromExtra(a.Extra, "passive_usage_7d_utilization", resetAt, now)
	default:
		return nil
	}
}

func (a *Account) CodexUsageUpdatedAt() *time.Time {
	if a == nil {
		return nil
	}
	updatedAt := a.getExtraTime("codex_usage_updated_at")
	if updatedAt.IsZero() {
		return nil
	}
	return &updatedAt
}

func (a *Account) AnthropicUsageUpdatedAt() *time.Time {
	if a == nil {
		return nil
	}
	updatedAt := a.getExtraTime("anthropic_usage_updated_at")
	if updatedAt.IsZero() {
		updatedAt = a.getExtraTime("passive_usage_sampled_at")
	}
	if updatedAt.IsZero() {
		return nil
	}
	return &updatedAt
}

func (a *Account) GetOpencode5hLimitPercent() float64 {
	return a.getCodexQuotaLimitPercent("opencode_5h_limit_percent")
}

func (a *Account) GetOpencode7dLimitPercent() float64 {
	return a.getCodexQuotaLimitPercent("opencode_7d_limit_percent")
}

func (a *Account) GetOpencode30dLimitPercent() float64 {
	return a.getCodexQuotaLimitPercent("opencode_30d_limit_percent")
}

func (a *Account) GetOpencode5hUsedPercent() float64 {
	return opencodeUsedPercentFromExtra(a, "opencode_5h_used_percent")
}

func (a *Account) GetOpencode7dUsedPercent() float64 {
	return opencodeUsedPercentFromExtra(a, "opencode_7d_used_percent")
}

func (a *Account) GetOpencode30dUsedPercent() float64 {
	return opencodeUsedPercentFromExtra(a, "opencode_30d_used_percent")
}

func (a *Account) IsOpencodeQuotaProtectionActiveAt(now time.Time) bool {
	return a.OpencodeQuotaProtectionReasonAt(now) != ""
}

func (a *Account) OpencodeQuotaProtectionReasonAt(now time.Time) string {
	reason, _ := a.opencodeQuotaProtectionWindowAt(now)
	return reason
}

func (a *Account) OpencodeQuotaProtectionResetAt(now time.Time) *time.Time {
	_, resetAt := a.opencodeQuotaProtectionWindowAt(now)
	return resetAt
}

func (a *Account) OpencodeUsageProgress(window string, now time.Time) *UsageProgress {
	if a == nil || !a.IsOpencodeApiKey() {
		return nil
	}
	return buildOpencodeUsageProgressFromExtra(a.Extra, window, now)
}

func (a *Account) OpencodeUsageUpdatedAt() *time.Time {
	if a == nil {
		return nil
	}
	updatedAt := a.getExtraTime("opencode_usage_updated_at")
	if updatedAt.IsZero() {
		return nil
	}
	return &updatedAt
}

func opencodeUsedPercentFromExtra(a *Account, key string) float64 {
	if a == nil || a.Extra == nil {
		return 0
	}
	return parseExtraFloat64(a.Extra[key])
}

func (a *Account) opencodeQuotaProtectionWindowAt(now time.Time) (string, *time.Time) {
	if a == nil || !a.IsOpencodeApiKey() || a.Extra == nil {
		return "", nil
	}
	reason, resetAt := "", time.Time{}
	if windowResetAt, ok := codexQuotaProtectedWindowResetAt(a.Extra, "opencode_5h_used_percent", "opencode_5h_reset_at", a.GetOpencode5hLimitPercent(), now); ok {
		reason = OpencodeQuotaWindow5h
		resetAt = windowResetAt
	}
	if windowResetAt, ok := codexQuotaProtectedWindowResetAt(a.Extra, "opencode_7d_used_percent", "opencode_7d_reset_at", a.GetOpencode7dLimitPercent(), now); ok {
		if reason == "" || windowResetAt.After(resetAt) {
			reason = OpencodeQuotaWindow7d
			resetAt = windowResetAt
		}
	}
	if windowResetAt, ok := codexQuotaProtectedWindowResetAt(a.Extra, "opencode_30d_used_percent", "opencode_30d_reset_at", a.GetOpencode30dLimitPercent(), now); ok {
		if reason == "" || windowResetAt.After(resetAt) {
			reason = OpencodeQuotaWindow30d
			resetAt = windowResetAt
		}
	}
	if reason == "" {
		return "", nil
	}
	return reason, &resetAt
}

func (a *Account) codexQuotaProtectionWindowAt(now time.Time) (string, *time.Time) {
	if a == nil || !a.IsOpenAIOAuth() || a.Extra == nil {
		return "", nil
	}
	reason, resetAt := "", time.Time{}
	if windowResetAt, ok := codexQuotaProtectedWindowResetAt(a.Extra, "codex_5h_used_percent", "codex_5h_reset_at", a.GetCodex5hLimitPercent(), now); ok {
		reason = CodexQuotaWindow5h
		resetAt = windowResetAt
	}
	if windowResetAt, ok := codexQuotaProtectedWindowResetAt(a.Extra, "codex_7d_used_percent", "codex_7d_reset_at", a.GetCodex7dLimitPercent(), now); ok {
		if reason == "" || windowResetAt.After(resetAt) {
			reason = CodexQuotaWindow7d
			resetAt = windowResetAt
		}
	}
	if reason == "" {
		return "", nil
	}
	return reason, &resetAt
}

func (a *Account) anthropicQuotaProtectionWindowAt(now time.Time) (string, *time.Time) {
	if a == nil || !a.IsAnthropicOAuthOrSetupToken() || a.Extra == nil {
		return "", nil
	}
	reason, resetAt := "", time.Time{}
	if windowResetAt, ok := a.anthropicQuotaProtectedWindowResetAt("session_window_utilization", "anthropic_5h_reset_at", a.GetAnthropic5hLimitPercent(), now); ok {
		reason = AnthropicQuotaWindow5h
		resetAt = windowResetAt
	}
	if windowResetAt, ok := a.anthropicQuotaProtectedWindowResetAt("passive_usage_7d_utilization", "anthropic_7d_reset_at", a.GetAnthropic7dLimitPercent(), now, "passive_usage_7d_reset"); ok {
		if reason == "" || windowResetAt.After(resetAt) {
			reason = AnthropicQuotaWindow7d
			resetAt = windowResetAt
		}
	}
	if reason == "" {
		return "", nil
	}
	return reason, &resetAt
}

func (a *Account) anthropicQuotaProtectedWindowResetAt(utilizationKey, resetKey string, limitPercent float64, now time.Time, resetFallbackKeys ...string) (time.Time, bool) {
	if limitPercent < AnthropicQuotaMinLimitPercent || limitPercent > AnthropicQuotaMaxLimitPercent {
		limitPercent = AnthropicQuotaDefaultLimitPercent
	}
	usedPercent, ok := anthropicUtilizationPercentFromExtra(a.Extra, utilizationKey)
	if !ok && utilizationKey == "session_window_utilization" {
		switch a.SessionWindowStatus {
		case "rejected":
			usedPercent = 100
			ok = true
		case "allowed_warning":
			usedPercent = 80
			ok = true
		}
	}
	if !ok || usedPercent < limitPercent {
		return time.Time{}, false
	}
	var resetAt time.Time
	var hasReset bool
	if utilizationKey == "session_window_utilization" {
		resetAt, hasReset = a.anthropic5hResetAt()
	}
	if !hasReset {
		keys := append([]string{resetKey}, resetFallbackKeys...)
		resetAt, hasReset = anthropicQuotaResetAtFromExtra(a.Extra, keys...)
	}
	if !hasReset || !now.Before(resetAt) {
		return time.Time{}, false
	}
	return resetAt, true
}

func (a *Account) anthropic5hResetAt() (time.Time, bool) {
	if a != nil && a.SessionWindowEnd != nil && !a.SessionWindowEnd.IsZero() {
		return *a.SessionWindowEnd, true
	}
	if a == nil {
		return time.Time{}, false
	}
	return anthropicQuotaResetAtFromExtra(a.Extra, "anthropic_5h_reset_at", "session_window_reset_at")
}

func isCodexQuotaWindowProtected(extra map[string]any, usedPercentKey, resetAtKey string, limitPercent float64, now time.Time) bool {
	_, ok := codexQuotaProtectedWindowResetAt(extra, usedPercentKey, resetAtKey, limitPercent, now)
	return ok
}

func codexQuotaProtectedWindowResetAt(extra map[string]any, usedPercentKey, resetAtKey string, limitPercent float64, now time.Time) (time.Time, bool) {
	if limitPercent < CodexQuotaMinLimitPercent || limitPercent > CodexQuotaMaxLimitPercent {
		limitPercent = CodexQuotaDefaultLimitPercent
	}
	usedRaw, ok := extra[usedPercentKey]
	if !ok || parseExtraFloat64(usedRaw) < limitPercent {
		return time.Time{}, false
	}
	resetAt, ok := codexQuotaResetAtFromExtra(extra, resetAtKey)
	if !ok {
		return time.Time{}, false
	}
	if !now.Before(resetAt) {
		return time.Time{}, false
	}
	return resetAt, true
}

func codexQuotaResetAtFromExtra(extra map[string]any, key string) (time.Time, bool) {
	if extra == nil {
		return time.Time{}, false
	}
	raw, ok := extra[key]
	if !ok || raw == nil {
		return time.Time{}, false
	}
	value := strings.TrimSpace(fmt.Sprint(raw))
	if value == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func buildAnthropicUsageProgressFromExtra(extra map[string]any, utilizationKey string, resetAt time.Time, now time.Time) *UsageProgress {
	utilization, hasUtilization := anthropicUtilizationPercentFromExtra(extra, utilizationKey)
	if !hasUtilization && resetAt.IsZero() {
		return nil
	}
	progress := &UsageProgress{Utilization: utilization}
	if !resetAt.IsZero() {
		progress.ResetsAt = &resetAt
		progress.RemainingSeconds = int(time.Until(resetAt).Seconds())
		if progress.RemainingSeconds < 0 {
			progress.RemainingSeconds = 0
		}
		if !now.Before(resetAt) {
			progress.Utilization = 0
		}
	}
	return progress
}

func anthropicUtilizationPercentFromExtra(extra map[string]any, key string) (float64, bool) {
	if extra == nil {
		return 0, false
	}
	raw, ok := extra[key]
	if !ok || raw == nil {
		return 0, false
	}
	value := parseExtraFloat64(raw)
	if value < 0 {
		value = 0
	}
	if value <= 1.5 {
		value *= 100
	}
	return value, true
}

func anthropicQuotaResetAtFromExtra(extra map[string]any, keys ...string) (time.Time, bool) {
	if extra == nil {
		return time.Time{}, false
	}
	for _, key := range keys {
		raw, ok := extra[key]
		if !ok || raw == nil {
			continue
		}
		if unix := int64(parseExtraFloat64(raw)); unix > 0 {
			return time.Unix(unix, 0), true
		}
		value := strings.TrimSpace(fmt.Sprint(raw))
		if value == "" {
			continue
		}
		if t, err := parseTime(value); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func (a *Account) IsOpenAIApiKey() bool {
	return a.IsOpenAI() && a.Type == AccountTypeAPIKey
}

func (a *Account) GetOpenAIBaseURL() string {
	if !a.IsOpenAI() {
		return ""
	}
	if a.Type == AccountTypeAPIKey {
		baseURL := a.GetCredential("base_url")
		if baseURL != "" {
			return baseURL
		}
	}
	return "https://api.openai.com"
}

func (a *Account) GetOpenAIAccessToken() string {
	if !a.IsOpenAI() {
		return ""
	}
	return a.GetCredential("access_token")
}

func (a *Account) GetOpenAIRefreshToken() string {
	if !a.IsOpenAIOAuth() {
		return ""
	}
	return a.GetCredential("refresh_token")
}

func (a *Account) GetGrokBaseURL() string {
	if !a.IsGrok() {
		return ""
	}
	baseURL := strings.TrimSpace(a.GetCredential("base_url"))
	if a.IsGrokOAuth() {
		// Grok OAuth 允许管理员在官方 CLI、官方/区域 API 与受信任中继之间切换。
		// 这里只决定账号配置语义；实际出站请求仍必须经过统一 URL 校验器。
		if baseURL == "" || !xai.IsParseableBaseURL(baseURL) {
			return xai.DefaultCLIBaseURL
		}
		return baseURL
	}
	if baseURL != "" {
		return baseURL
	}
	return xai.DefaultBaseURL
}

// GetGrokMediaBaseURL selects the upstream used by Grok Imagine APIs.
// CLI 网关的请求体限制不适合大体积媒体，因此仅当 OAuth 文本端点指向 CLI
// 时把媒体请求切到官方 API；其他管理员选择的官方、区域或中继端点保持不变。
func (a *Account) GetGrokMediaBaseURL() string {
	if !a.IsGrok() {
		return ""
	}
	baseURL := a.GetGrokBaseURL()
	if a.IsGrokOAuth() && isGrokCLIProxyTarget(baseURL) {
		return xai.DefaultBaseURL
	}
	return baseURL
}

func (a *Account) GetGrokAccessToken() string {
	if !a.IsGrok() {
		return ""
	}
	return a.GetCredential("access_token")
}

func (a *Account) GetGrokRefreshToken() string {
	if !a.IsGrokOAuth() {
		return ""
	}
	return a.GetCredential("refresh_token")
}

func (a *Account) GetOpenAIIDToken() string {
	if !a.IsOpenAIOAuth() {
		return ""
	}
	return a.GetCredential("id_token")
}

func (a *Account) GetOpenAIApiKey() string {
	if !a.IsOpenAIApiKey() && !a.IsOpencodeApiKey() {
		return ""
	}
	return a.GetCredential("api_key")
}

// OpencodeDefaultBaseURL 是 opencode OpenCode Go 订阅的官方端点。
// opencode 账号锁定官方地址，用户端不提供 base_url 输入。
const OpencodeDefaultBaseURL = "https://opencode.ai/zen/go/v1"

func (a *Account) GetOpencodeApiKey() string {
	if !a.IsOpencodeApiKey() {
		return ""
	}
	return a.GetCredential("api_key")
}

func (a *Account) GetOpencodeBaseURL() string {
	if !a.IsOpencode() {
		return ""
	}
	return OpencodeDefaultBaseURL
}

func (a *Account) GetOpenAIUserAgent() string {
	if a.IsOpencode() {
		return "opencode/1.0"
	}
	if !a.IsOpenAI() {
		return ""
	}
	return a.GetCredential("user_agent")
}

func (a *Account) GetChatGPTAccountID() string {
	if !a.IsOpenAIOAuth() {
		return ""
	}
	return a.GetCredential("chatgpt_account_id")
}

func (a *Account) IsChatGPTAccountFedRAMP() bool {
	if !a.IsOpenAIOAuth() || a.Credentials == nil {
		return false
	}
	value, ok := a.Credentials["chatgpt_account_is_fedramp"]
	if !ok || value == nil {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		return err == nil && parsed
	case json.Number:
		parsed, err := strconv.ParseBool(typed.String())
		return err == nil && parsed
	case float64:
		return typed != 0
	case int:
		return typed != 0
	case int64:
		return typed != 0
	default:
		return false
	}
}

func (a *Account) GetOpenAIDeviceID() string {
	if !a.IsOpenAIOAuth() {
		return ""
	}
	return strings.TrimSpace(a.GetExtraString("openai_device_id"))
}

func (a *Account) GetOpenAISessionID() string {
	if !a.IsOpenAIOAuth() {
		return ""
	}
	return strings.TrimSpace(a.GetExtraString("openai_session_id"))
}

// SupportsOpenAIEndpointCapability reports whether an account can remain a
// scheduler candidate for an endpoint-specific request. Unobserved Grok OAuth
// accounts remain candidates only so the request path can run the billing
// probe; forwarding still fails closed unless that probe yields paid evidence.
func (a *Account) SupportsOpenAIEndpointCapability(capability OpenAIEndpointCapability) bool {
	if a == nil {
		return false
	}
	if capability == "" {
		return true
	}
	if !a.IsOpenAICompatible() {
		return false
	}
	switch capability {
	case OpenAIEndpointCapabilityGrokMediaGeneration:
		if !a.IsGrok() {
			return false
		}
		eligible, reason := a.GrokMediaGenerationEligibility()
		return eligible || reason == "billing_unobserved"
	default:
		return false
	}
}

// GrokMediaGenerationEligibility reports whether a Grok account may receive
// a new image/video generation request. OAuth accounts fail closed unless
// billing observations provide positive paid-entitlement evidence.
func (a *Account) GrokMediaGenerationEligibility() (bool, string) {
	if a == nil || !a.IsGrok() {
		return false, "not_grok"
	}
	if override, ok := grokMediaEligibilityOverride(a.Extra); ok {
		if override {
			return true, "override_enabled"
		}
		return false, "override_disabled"
	}
	if a.Type != AccountTypeOAuth {
		return true, "non_oauth"
	}

	billing, err := grokBillingSnapshotFromExtra(a.Extra)
	if err != nil || billing == nil {
		return false, "billing_unobserved"
	}
	if billing.StatusCode == http.StatusForbidden ||
		billing.WeeklyStatusCode == http.StatusForbidden ||
		billing.MonthlyStatusCode == http.StatusForbidden {
		return false, "billing_forbidden"
	}
	if isKnownGrokFreeAccount(a) {
		return false, "billing_free_tier"
	}
	if !grokBillingHasAuthoritativeQuota(billing) {
		return false, "billing_inconclusive"
	}
	return true, "eligible"
}

func grokMediaEligibilityOverride(extra map[string]any) (bool, bool) {
	if extra == nil {
		return false, false
	}
	raw, exists := extra[GrokMediaEligibleExtraKey]
	if !exists || raw == nil {
		return false, false
	}
	value, ok := raw.(bool)
	return value, ok
}

func (a *Account) SupportsOpenAIImageCapability(capability OpenAIImagesCapability) bool {
	if !a.IsOpenAI() {
		return false
	}
	switch capability {
	case OpenAIImagesCapabilityBasic:
		return a.Type == AccountTypeOAuth || a.Type == AccountTypeAPIKey
	case OpenAIImagesCapabilityNative:
		return a.Type == AccountTypeAPIKey
	default:
		return true
	}
}

func (a *Account) GetChatGPTUserID() string {
	if !a.IsOpenAIOAuth() {
		return ""
	}
	return a.GetCredential("chatgpt_user_id")
}

func (a *Account) GetOpenAIOrganizationID() string {
	if !a.IsOpenAIOAuth() {
		return ""
	}
	return a.GetCredential("organization_id")
}

func (a *Account) GetOpenAITokenExpiresAt() *time.Time {
	if !a.IsOpenAIOAuth() {
		return nil
	}
	return a.GetCredentialAsTime("expires_at")
}

func (a *Account) IsOpenAITokenExpired() bool {
	expiresAt := a.GetOpenAITokenExpiresAt()
	if expiresAt == nil {
		return false
	}
	return time.Now().Add(60 * time.Second).After(*expiresAt)
}

// IsMixedSchedulingEnabled 检查 antigravity 账户是否启用混合调度
// 启用后可参与 anthropic/gemini 分组的账户调度
func (a *Account) IsMixedSchedulingEnabled() bool {
	if a.Platform != PlatformAntigravity {
		return false
	}
	if a.Extra == nil {
		return false
	}
	if v, ok := a.Extra["mixed_scheduling"]; ok {
		if enabled, ok := v.(bool); ok {
			return enabled
		}
	}
	return false
}

// IsOveragesEnabled 检查 Antigravity 账号是否启用 AI Credits 超量请求。
func (a *Account) IsOveragesEnabled() bool {
	if a.Platform != PlatformAntigravity {
		return false
	}
	if a.Extra == nil {
		return false
	}
	if v, ok := a.Extra["allow_overages"]; ok {
		if enabled, ok := v.(bool); ok {
			return enabled
		}
	}
	return false
}

// IsOpenAIPassthroughEnabled 返回 OpenAI 账号是否启用"自动透传（仅替换认证）"。
//
// 新字段：accounts.extra.openai_passthrough。
// 兼容字段：accounts.extra.openai_oauth_passthrough（历史 OAuth 开关）。
// 字段缺失或类型不正确时，按 false（关闭）处理。
func (a *Account) IsOpenAIPassthroughEnabled() bool {
	if a == nil || !a.IsOpenAI() || a.Extra == nil {
		return false
	}
	if enabled, ok := a.Extra["openai_passthrough"].(bool); ok {
		return enabled
	}
	if enabled, ok := a.Extra["openai_oauth_passthrough"].(bool); ok {
		return enabled
	}
	return false
}

// IsOpenAIResponsesWebSocketV2Enabled 返回 OpenAI 账号是否开启 Responses WebSocket v2。
//
// 分类型新字段：
// - OAuth 账号：accounts.extra.openai_oauth_responses_websockets_v2_enabled
// - API Key 账号：accounts.extra.openai_apikey_responses_websockets_v2_enabled
//
// 兼容字段：
// - accounts.extra.responses_websockets_v2_enabled
// - accounts.extra.openai_ws_enabled（历史开关）
//
// 优先级：
// 1. 按账号类型读取分类型字段
// 2. 分类型字段缺失时，回退兼容字段
func (a *Account) IsOpenAIResponsesWebSocketV2Enabled() bool {
	if a == nil || !a.IsOpenAI() || a.Extra == nil {
		return false
	}
	if a.IsOpenAIOAuth() {
		if enabled, ok := a.Extra["openai_oauth_responses_websockets_v2_enabled"].(bool); ok {
			return enabled
		}
	}
	if a.IsOpenAIApiKey() {
		if enabled, ok := a.Extra["openai_apikey_responses_websockets_v2_enabled"].(bool); ok {
			return enabled
		}
	}
	if enabled, ok := a.Extra["responses_websockets_v2_enabled"].(bool); ok {
		return enabled
	}
	if enabled, ok := a.Extra["openai_ws_enabled"].(bool); ok {
		return enabled
	}
	return false
}

const (
	OpenAIWSIngressModeOff         = "off"
	OpenAIWSIngressModeShared      = "shared"
	OpenAIWSIngressModeDedicated   = "dedicated"
	OpenAIWSIngressModeCtxPool     = "ctx_pool"
	OpenAIWSIngressModePassthrough = "passthrough"
)

func normalizeOpenAIWSIngressMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case OpenAIWSIngressModeOff:
		return OpenAIWSIngressModeOff
	case OpenAIWSIngressModeCtxPool:
		return OpenAIWSIngressModeCtxPool
	case OpenAIWSIngressModePassthrough:
		return OpenAIWSIngressModePassthrough
	case OpenAIWSIngressModeShared:
		return OpenAIWSIngressModeShared
	case OpenAIWSIngressModeDedicated:
		return OpenAIWSIngressModeDedicated
	default:
		return ""
	}
}

func normalizeOpenAIWSIngressDefaultMode(mode string) string {
	if normalized := normalizeOpenAIWSIngressMode(mode); normalized != "" {
		if normalized == OpenAIWSIngressModeShared || normalized == OpenAIWSIngressModeDedicated {
			return OpenAIWSIngressModeCtxPool
		}
		return normalized
	}
	return OpenAIWSIngressModeCtxPool
}

// ResolveOpenAIResponsesWebSocketV2Mode 返回账号在 WSv2 ingress 下的有效模式（off/ctx_pool/passthrough）。
//
// 优先级：
// 1. 分类型 mode 新字段（string）
// 2. 分类型 enabled 旧字段（bool）
// 3. 兼容 enabled 旧字段（bool）
// 4. defaultMode（非法时回退 ctx_pool）
func (a *Account) ResolveOpenAIResponsesWebSocketV2Mode(defaultMode string) string {
	resolvedDefault := normalizeOpenAIWSIngressDefaultMode(defaultMode)
	if a == nil || !a.IsOpenAI() {
		return OpenAIWSIngressModeOff
	}
	if a.Extra == nil {
		return resolvedDefault
	}

	resolveModeString := func(key string) (string, bool) {
		raw, ok := a.Extra[key]
		if !ok {
			return "", false
		}
		mode, ok := raw.(string)
		if !ok {
			return "", false
		}
		normalized := normalizeOpenAIWSIngressMode(mode)
		if normalized == "" {
			return "", false
		}
		return normalized, true
	}
	resolveBoolMode := func(key string) (string, bool) {
		raw, ok := a.Extra[key]
		if !ok {
			return "", false
		}
		enabled, ok := raw.(bool)
		if !ok {
			return "", false
		}
		if enabled {
			return OpenAIWSIngressModeCtxPool, true
		}
		return OpenAIWSIngressModeOff, true
	}

	if a.IsOpenAIOAuth() {
		if mode, ok := resolveModeString("openai_oauth_responses_websockets_v2_mode"); ok {
			return mode
		}
		if mode, ok := resolveBoolMode("openai_oauth_responses_websockets_v2_enabled"); ok {
			return mode
		}
	}
	if a.IsOpenAIApiKey() {
		if mode, ok := resolveModeString("openai_apikey_responses_websockets_v2_mode"); ok {
			return mode
		}
		if mode, ok := resolveBoolMode("openai_apikey_responses_websockets_v2_enabled"); ok {
			return mode
		}
	}
	if mode, ok := resolveBoolMode("responses_websockets_v2_enabled"); ok {
		return mode
	}
	if mode, ok := resolveBoolMode("openai_ws_enabled"); ok {
		return mode
	}
	// 兼容旧值：shared/dedicated 语义都归并到 ctx_pool。
	if resolvedDefault == OpenAIWSIngressModeShared || resolvedDefault == OpenAIWSIngressModeDedicated {
		return OpenAIWSIngressModeCtxPool
	}
	return resolvedDefault
}

// IsOpenAIWSForceHTTPEnabled 返回账号级"强制 HTTP"开关。
// 字段：accounts.extra.openai_ws_force_http。
func (a *Account) IsOpenAIWSForceHTTPEnabled() bool {
	if a == nil || !a.IsOpenAI() || a.Extra == nil {
		return false
	}
	enabled, ok := a.Extra["openai_ws_force_http"].(bool)
	return ok && enabled
}

// IsOpenAIWSAllowStoreRecoveryEnabled 返回账号级 store 恢复开关。
// 字段：accounts.extra.openai_ws_allow_store_recovery。
func (a *Account) IsOpenAIWSAllowStoreRecoveryEnabled() bool {
	if a == nil || !a.IsOpenAI() || a.Extra == nil {
		return false
	}
	enabled, ok := a.Extra["openai_ws_allow_store_recovery"].(bool)
	return ok && enabled
}

// IsOpenAIOAuthPassthroughEnabled 兼容旧接口，等价于 OAuth 账号的 IsOpenAIPassthroughEnabled。
func (a *Account) IsOpenAIOAuthPassthroughEnabled() bool {
	return a != nil && a.IsOpenAIOAuth() && a.IsOpenAIPassthroughEnabled()
}

// IsAnthropicAPIKeyPassthroughEnabled 返回 Anthropic API Key 账号是否启用"自动透传（仅替换认证）"。
// 字段：accounts.extra.anthropic_passthrough。
// 字段缺失或类型不正确时，按 false（关闭）处理。
func (a *Account) IsAnthropicAPIKeyPassthroughEnabled() bool {
	if a == nil || a.Platform != PlatformAnthropic || a.Type != AccountTypeAPIKey || a.Extra == nil {
		return false
	}
	enabled, ok := a.Extra["anthropic_passthrough"].(bool)
	return ok && enabled
}

// WebSearch 模拟三态常量
const (
	WebSearchModeDefault  = "default"  // 跟随渠道配置
	WebSearchModeEnabled  = "enabled"  // 强制开启
	WebSearchModeDisabled = "disabled" // 强制关闭
)

// GetWebSearchEmulationMode 返回账号的 WebSearch 模拟模式。
// 三态：default（跟随渠道）/ enabled（强制开启）/ disabled（强制关闭）。
// 兼容旧 bool 值：true→enabled, false→default（并记录 debug 日志）。
func (a *Account) GetWebSearchEmulationMode() string {
	if a == nil || a.Platform != PlatformAnthropic || a.Type != AccountTypeAPIKey || a.Extra == nil {
		return WebSearchModeDefault
	}
	raw := a.Extra[featureKeyWebSearchEmulation]
	// Tolerant: legacy bool values (pre-migration or stale writes)
	if b, ok := raw.(bool); ok {
		slog.Debug("legacy bool web_search_emulation value", "account_id", a.ID, "value", b)
		if b {
			return WebSearchModeEnabled
		}
		return WebSearchModeDefault
	}
	mode, ok := raw.(string)
	if !ok {
		return WebSearchModeDefault
	}
	switch mode {
	case WebSearchModeEnabled, WebSearchModeDisabled:
		return mode
	default:
		return WebSearchModeDefault
	}
}

// IsCodexCLIOnlyEnabled 返回 OpenAI OAuth 账号是否启用"仅允许 Codex 官方客户端"。
// 字段：accounts.extra.codex_cli_only。
// 字段缺失或类型不正确时，按 false（关闭）处理。
func (a *Account) IsCodexCLIOnlyEnabled() bool {
	if a == nil || !a.IsOpenAIOAuth() || a.Extra == nil {
		return false
	}
	enabled, ok := a.Extra["codex_cli_only"].(bool)
	return ok && enabled
}

// WindowCostSchedulability 窗口费用调度状态
type WindowCostSchedulability int

const (
	// WindowCostSchedulable 可正常调度
	WindowCostSchedulable WindowCostSchedulability = iota
	// WindowCostStickyOnly 仅允许粘性会话
	WindowCostStickyOnly
	// WindowCostNotSchedulable 完全不可调度
	WindowCostNotSchedulable
)

// IsAnthropicOAuthOrSetupToken 判断是否为 Anthropic OAuth 或 SetupToken 类型账号
// 仅这两类账号支持 5h 窗口额度控制和会话数量控制
func (a *Account) IsAnthropicOAuthOrSetupToken() bool {
	return a.Platform == PlatformAnthropic && (a.Type == AccountTypeOAuth || a.Type == AccountTypeSetupToken)
}

// IsTLSFingerprintEnabled 检查是否启用 TLS 指纹伪装
// 仅适用于 Anthropic OAuth/SetupToken 与 opencode 账号
// 启用后将模拟 Claude Code (Node.js) 客户端的 TLS 握手特征
func (a *Account) IsTLSFingerprintEnabled() bool {
	// opencode 账号默认启用 TLS 指纹伪装，规避上游 browser signature 检测。
	if a != nil && a.IsOpencode() {
		return true
	}
	// 仅支持 Anthropic OAuth/SetupToken 账号
	if !a.IsAnthropicOAuthOrSetupToken() {
		return false
	}
	if a.Extra == nil {
		return false
	}
	if v, ok := a.Extra["enable_tls_fingerprint"]; ok {
		if enabled, ok := v.(bool); ok {
			return enabled
		}
	}
	return false
}

// GetTLSFingerprintProfileID 获取账号绑定的 TLS 指纹模板 ID
// 返回 0 表示未绑定（使用内置默认 profile）
func (a *Account) GetTLSFingerprintProfileID() int64 {
	if a.Extra == nil {
		return 0
	}
	v, ok := a.Extra["tls_fingerprint_profile_id"]
	if !ok {
		return 0
	}
	switch id := v.(type) {
	case float64:
		return int64(id)
	case int64:
		return id
	case int:
		return int64(id)
	case json.Number:
		if i, err := id.Int64(); err == nil {
			return i
		}
	}
	return 0
}

// GetUserMsgQueueMode 获取用户消息队列模式
// "serialize" = 串行队列, "throttle" = 软性限速, "" = 未设置（使用全局配置）
func (a *Account) GetUserMsgQueueMode() string {
	if a.Extra == nil {
		return ""
	}
	// 优先读取新字段 user_msg_queue_mode（白名单校验，非法值视为未设置）
	if mode, ok := a.Extra["user_msg_queue_mode"].(string); ok && mode != "" {
		if mode == config.UMQModeSerialize || mode == config.UMQModeThrottle {
			return mode
		}
		return "" // 非法值 fallback 到全局配置
	}
	// 向后兼容: user_msg_queue_enabled: true → "serialize"
	if enabled, ok := a.Extra["user_msg_queue_enabled"].(bool); ok && enabled {
		return config.UMQModeSerialize
	}
	return ""
}

// IsSessionIDMaskingEnabled 检查是否启用会话ID伪装
// 仅适用于 Anthropic OAuth/SetupToken 类型账号
// 启用后将在一段时间内（15分钟）固定 metadata.user_id 中的 session ID，
// 使上游认为请求来自同一个会话
func (a *Account) IsSessionIDMaskingEnabled() bool {
	if !a.IsAnthropicOAuthOrSetupToken() {
		return false
	}
	if a.Extra == nil {
		return false
	}
	if v, ok := a.Extra["session_id_masking_enabled"]; ok {
		if enabled, ok := v.(bool); ok {
			return enabled
		}
	}
	return false
}

// IsCustomBaseURLEnabled 检查是否启用自定义 base URL 中继转发
// 仅适用于 Anthropic OAuth/SetupToken 类型账号
func (a *Account) IsCustomBaseURLEnabled() bool {
	if !a.IsAnthropicOAuthOrSetupToken() {
		return false
	}
	if a.Extra == nil {
		return false
	}
	if v, ok := a.Extra["custom_base_url_enabled"]; ok {
		if enabled, ok := v.(bool); ok {
			return enabled
		}
	}
	return false
}

// GetCustomBaseURL 返回自定义中继服务的 base URL
func (a *Account) GetCustomBaseURL() string {
	return a.GetExtraString("custom_base_url")
}

// IsCacheTTLOverrideEnabled 检查是否启用缓存 TTL 强制替换
// 仅适用于 Anthropic OAuth/SetupToken 类型账号
// 启用后将所有 cache creation tokens 归入指定的 TTL 类型（5m 或 1h）
func (a *Account) IsCacheTTLOverrideEnabled() bool {
	if !a.IsAnthropicOAuthOrSetupToken() {
		return false
	}
	if a.Extra == nil {
		return false
	}
	if v, ok := a.Extra["cache_ttl_override_enabled"]; ok {
		if enabled, ok := v.(bool); ok {
			return enabled
		}
	}
	return false
}

// GetCacheTTLOverrideTarget 获取缓存 TTL 强制替换的目标类型
// 返回 "5m" 或 "1h"，默认 "5m"
func (a *Account) GetCacheTTLOverrideTarget() string {
	if a.Extra == nil {
		return "5m"
	}
	if v, ok := a.Extra["cache_ttl_override_target"]; ok {
		if target, ok := v.(string); ok && (target == "5m" || target == "1h") {
			return target
		}
	}
	return "5m"
}

// GetQuotaLimit 获取 API Key 账号的配额限制（美元）
// 返回 0 表示未启用
func (a *Account) GetQuotaLimit() float64 {
	return a.getExtraFloat64("quota_limit")
}

// GetQuotaUsed 获取 API Key 账号的已用配额（美元）
func (a *Account) GetQuotaUsed() float64 {
	return a.getExtraFloat64("quota_used")
}

// GetQuotaDailyLimit 获取日额度限制（美元），0 表示未启用
func (a *Account) GetQuotaDailyLimit() float64 {
	return a.getExtraFloat64("quota_daily_limit")
}

// GetQuotaDailyUsed 获取当日已用额度（美元）
func (a *Account) GetQuotaDailyUsed() float64 {
	return a.getExtraFloat64("quota_daily_used")
}

// GetQuotaWeeklyLimit 获取周额度限制（美元），0 表示未启用
func (a *Account) GetQuotaWeeklyLimit() float64 {
	return a.getExtraFloat64("quota_weekly_limit")
}

// GetQuotaWeeklyUsed 获取本周已用额度（美元）
func (a *Account) GetQuotaWeeklyUsed() float64 {
	return a.getExtraFloat64("quota_weekly_used")
}

// getExtraFloat64 从 Extra 中读取指定 key 的 float64 值
func (a *Account) getExtraFloat64(key string) float64 {
	if a.Extra == nil {
		return 0
	}
	if v, ok := a.Extra[key]; ok {
		return parseExtraFloat64(v)
	}
	return 0
}

// getExtraTime 从 Extra 中读取 RFC3339 时间戳
func (a *Account) getExtraTime(key string) time.Time {
	if a.Extra == nil {
		return time.Time{}
	}
	if v, ok := a.Extra[key]; ok {
		if s, ok := v.(string); ok {
			if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
				return t
			}
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

// getExtraBool 从 Extra 中读取指定 key 的 bool 值
func (a *Account) getExtraBool(key string) bool {
	if a.Extra == nil {
		return false
	}
	if v, ok := a.Extra[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// getExtraString 从 Extra 中读取指定 key 的字符串值
func (a *Account) getExtraString(key string) string {
	if a.Extra == nil {
		return ""
	}
	if v, ok := a.Extra[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getExtraStringDefault 从 Extra 中读取指定 key 的字符串值，不存在时返回 defaultVal
func (a *Account) getExtraStringDefault(key, defaultVal string) string {
	if v := a.getExtraString(key); v != "" {
		return v
	}
	return defaultVal
}

// getExtraInt 从 Extra 中读取指定 key 的 int 值
func (a *Account) getExtraInt(key string) int {
	if a.Extra == nil {
		return 0
	}
	if v, ok := a.Extra[key]; ok {
		return int(parseExtraFloat64(v))
	}
	return 0
}

// GetQuotaDailyResetMode 获取日额度重置模式："rolling"（默认）或 "fixed"
func (a *Account) GetQuotaDailyResetMode() string {
	if m := a.getExtraString("quota_daily_reset_mode"); m == "fixed" {
		return "fixed"
	}
	return "rolling"
}

// GetQuotaDailyResetHour 获取固定重置的小时（0-23），默认 0
func (a *Account) GetQuotaDailyResetHour() int {
	return a.getExtraInt("quota_daily_reset_hour")
}

// GetQuotaWeeklyResetMode 获取周额度重置模式："rolling"（默认）或 "fixed"
func (a *Account) GetQuotaWeeklyResetMode() string {
	if m := a.getExtraString("quota_weekly_reset_mode"); m == "fixed" {
		return "fixed"
	}
	return "rolling"
}

// GetQuotaWeeklyResetDay 获取固定重置的星期几（0=周日, 1=周一, ..., 6=周六），默认 1（周一）
func (a *Account) GetQuotaWeeklyResetDay() int {
	if a.Extra == nil {
		return 1
	}
	if _, ok := a.Extra["quota_weekly_reset_day"]; !ok {
		return 1
	}
	return a.getExtraInt("quota_weekly_reset_day")
}

// GetQuotaWeeklyResetHour 获取周配额固定重置的小时（0-23），默认 0
func (a *Account) GetQuotaWeeklyResetHour() int {
	return a.getExtraInt("quota_weekly_reset_hour")
}

// GetQuotaResetTimezone 获取固定重置的时区名（IANA），默认 "UTC"
func (a *Account) GetQuotaResetTimezone() string {
	if tz := a.getExtraString("quota_reset_timezone"); tz != "" {
		return tz
	}
	return "UTC"
}

// --- Quota Notification Getters ---

// QuotaNotifyConfig returns the notify configuration for a given quota dimension.
// dim must be one of quotaDimDaily, quotaDimWeekly, quotaDimTotal.
func (a *Account) QuotaNotifyConfig(dim string) (enabled bool, threshold float64, thresholdType string) {
	enabled = a.getExtraBool("quota_notify_" + dim + "_enabled")
	threshold = a.getExtraFloat64("quota_notify_" + dim + "_threshold")
	thresholdType = a.getExtraStringDefault("quota_notify_"+dim+"_threshold_type", thresholdTypeFixed)
	return
}

func (a *Account) GetQuotaNotifyDailyEnabled() bool {
	e, _, _ := a.QuotaNotifyConfig(quotaDimDaily)
	return e
}

func (a *Account) GetQuotaNotifyDailyThreshold() float64 {
	_, t, _ := a.QuotaNotifyConfig(quotaDimDaily)
	return t
}

func (a *Account) GetQuotaNotifyDailyThresholdType() string {
	_, _, tt := a.QuotaNotifyConfig(quotaDimDaily)
	return tt
}

func (a *Account) GetQuotaNotifyWeeklyEnabled() bool {
	e, _, _ := a.QuotaNotifyConfig(quotaDimWeekly)
	return e
}

func (a *Account) GetQuotaNotifyWeeklyThreshold() float64 {
	_, t, _ := a.QuotaNotifyConfig(quotaDimWeekly)
	return t
}

func (a *Account) GetQuotaNotifyWeeklyThresholdType() string {
	_, _, tt := a.QuotaNotifyConfig(quotaDimWeekly)
	return tt
}

func (a *Account) GetQuotaNotifyTotalEnabled() bool {
	e, _, _ := a.QuotaNotifyConfig(quotaDimTotal)
	return e
}

func (a *Account) GetQuotaNotifyTotalThreshold() float64 {
	_, t, _ := a.QuotaNotifyConfig(quotaDimTotal)
	return t
}

func (a *Account) GetQuotaNotifyTotalThresholdType() string {
	_, _, tt := a.QuotaNotifyConfig(quotaDimTotal)
	return tt
}

// nextFixedDailyReset 计算在 after 之后的下一个每日固定重置时间点
func nextFixedDailyReset(hour int, tz *time.Location, after time.Time) time.Time {
	t := after.In(tz)
	today := time.Date(t.Year(), t.Month(), t.Day(), hour, 0, 0, 0, tz)
	if !after.Before(today) {
		return today.AddDate(0, 0, 1)
	}
	return today
}

// lastFixedDailyReset 计算 now 之前最近一次的每日固定重置时间点
func lastFixedDailyReset(hour int, tz *time.Location, now time.Time) time.Time {
	t := now.In(tz)
	today := time.Date(t.Year(), t.Month(), t.Day(), hour, 0, 0, 0, tz)
	if now.Before(today) {
		return today.AddDate(0, 0, -1)
	}
	return today
}

// nextFixedWeeklyReset 计算在 after 之后的下一个每周固定重置时间点
// day: 0=Sunday, 1=Monday, ..., 6=Saturday
func nextFixedWeeklyReset(day, hour int, tz *time.Location, after time.Time) time.Time {
	t := after.In(tz)
	todayReset := time.Date(t.Year(), t.Month(), t.Day(), hour, 0, 0, 0, tz)
	currentDay := int(todayReset.Weekday())

	daysForward := (day - currentDay + 7) % 7
	if daysForward == 0 && !after.Before(todayReset) {
		daysForward = 7
	}
	return todayReset.AddDate(0, 0, daysForward)
}

// lastFixedWeeklyReset 计算 now 之前最近一次的每周固定重置时间点
func lastFixedWeeklyReset(day, hour int, tz *time.Location, now time.Time) time.Time {
	t := now.In(tz)
	todayReset := time.Date(t.Year(), t.Month(), t.Day(), hour, 0, 0, 0, tz)
	currentDay := int(todayReset.Weekday())

	daysBack := (currentDay - day + 7) % 7
	if daysBack == 0 && now.Before(todayReset) {
		daysBack = 7
	}
	return todayReset.AddDate(0, 0, -daysBack)
}

// isFixedDailyPeriodExpired 检查日配额是否在固定时间模式下已过期
func (a *Account) isFixedDailyPeriodExpired(periodStart time.Time) bool {
	return a.isFixedDailyPeriodExpiredAt(periodStart, time.Now())
}

func (a *Account) isFixedDailyPeriodExpiredAt(periodStart time.Time, now time.Time) bool {
	if periodStart.IsZero() {
		return true
	}
	tz, err := time.LoadLocation(a.GetQuotaResetTimezone())
	if err != nil {
		tz = time.UTC
	}
	lastReset := lastFixedDailyReset(a.GetQuotaDailyResetHour(), tz, now)
	return periodStart.Before(lastReset)
}

// isFixedWeeklyPeriodExpired 检查周配额是否在固定时间模式下已过期
func (a *Account) isFixedWeeklyPeriodExpired(periodStart time.Time) bool {
	return a.isFixedWeeklyPeriodExpiredAt(periodStart, time.Now())
}

func (a *Account) isFixedWeeklyPeriodExpiredAt(periodStart time.Time, now time.Time) bool {
	if periodStart.IsZero() {
		return true
	}
	tz, err := time.LoadLocation(a.GetQuotaResetTimezone())
	if err != nil {
		tz = time.UTC
	}
	lastReset := lastFixedWeeklyReset(a.GetQuotaWeeklyResetDay(), a.GetQuotaWeeklyResetHour(), tz, now)
	return periodStart.Before(lastReset)
}

// ComputeQuotaResetAt 根据当前配置计算并填充 extra 中的 quota_daily_reset_at / quota_weekly_reset_at
// 在保存账号配置时调用
func ComputeQuotaResetAt(extra map[string]any) {
	now := time.Now()
	tzName, _ := extra["quota_reset_timezone"].(string)
	if tzName == "" {
		tzName = "UTC"
	}
	tz, err := time.LoadLocation(tzName)
	if err != nil {
		tz = time.UTC
	}

	// 日配额固定重置时间
	if mode, _ := extra["quota_daily_reset_mode"].(string); mode == "fixed" {
		hour := int(parseExtraFloat64(extra["quota_daily_reset_hour"]))
		if hour < 0 || hour > 23 {
			hour = 0
		}
		resetAt := nextFixedDailyReset(hour, tz, now)
		extra["quota_daily_reset_at"] = resetAt.UTC().Format(time.RFC3339)
	} else {
		delete(extra, "quota_daily_reset_at")
	}

	// 周配额固定重置时间
	if mode, _ := extra["quota_weekly_reset_mode"].(string); mode == "fixed" {
		day := 1 // 默认周一
		if d, ok := extra["quota_weekly_reset_day"]; ok {
			day = int(parseExtraFloat64(d))
		}
		if day < 0 || day > 6 {
			day = 1
		}
		hour := int(parseExtraFloat64(extra["quota_weekly_reset_hour"]))
		if hour < 0 || hour > 23 {
			hour = 0
		}
		resetAt := nextFixedWeeklyReset(day, hour, tz, now)
		extra["quota_weekly_reset_at"] = resetAt.UTC().Format(time.RFC3339)
	} else {
		delete(extra, "quota_weekly_reset_at")
	}
}

// ValidateQuotaResetConfig 校验配额固定重置时间配置的合法性
func ValidateQuotaResetConfig(extra map[string]any) error {
	if extra == nil {
		return nil
	}
	// 校验时区
	if tz, ok := extra["quota_reset_timezone"].(string); ok && tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			return errors.New("invalid quota_reset_timezone: must be a valid IANA timezone name")
		}
	}
	// 日配额重置模式
	if mode, ok := extra["quota_daily_reset_mode"].(string); ok {
		if mode != "rolling" && mode != "fixed" {
			return errors.New("quota_daily_reset_mode must be 'rolling' or 'fixed'")
		}
	}
	// 日配额重置小时
	if v, ok := extra["quota_daily_reset_hour"]; ok {
		hour := int(parseExtraFloat64(v))
		if hour < 0 || hour > 23 {
			return errors.New("quota_daily_reset_hour must be between 0 and 23")
		}
	}
	// 周配额重置模式
	if mode, ok := extra["quota_weekly_reset_mode"].(string); ok {
		if mode != "rolling" && mode != "fixed" {
			return errors.New("quota_weekly_reset_mode must be 'rolling' or 'fixed'")
		}
	}
	// 周配额重置星期几
	if v, ok := extra["quota_weekly_reset_day"]; ok {
		day := int(parseExtraFloat64(v))
		if day < 0 || day > 6 {
			return errors.New("quota_weekly_reset_day must be between 0 (Sunday) and 6 (Saturday)")
		}
	}
	// 周配额重置小时
	if v, ok := extra["quota_weekly_reset_hour"]; ok {
		hour := int(parseExtraFloat64(v))
		if hour < 0 || hour > 23 {
			return errors.New("quota_weekly_reset_hour must be between 0 and 23")
		}
	}
	return nil
}

// HasAnyQuotaLimit 检查是否配置了任一维度的配额限制
func (a *Account) HasAnyQuotaLimit() bool {
	return a.GetQuotaLimit() > 0 || a.GetQuotaDailyLimit() > 0 || a.GetQuotaWeeklyLimit() > 0
}

// isPeriodExpired 检查指定周期（自 periodStart 起经过 dur）是否已过期
func isPeriodExpired(periodStart time.Time, dur time.Duration) bool {
	return isPeriodExpiredAt(periodStart, dur, time.Now())
}

func isPeriodExpiredAt(periodStart time.Time, dur time.Duration, now time.Time) bool {
	if periodStart.IsZero() {
		return true // 从未使用过，视为过期（下次 increment 会初始化）
	}
	return !now.Before(periodStart.Add(dur))
}

// IsDailyQuotaPeriodExpired 检查日配额周期是否已过期（用于显示层判断是否需要将 used 归零）
func (a *Account) IsDailyQuotaPeriodExpired() bool {
	return a.IsDailyQuotaPeriodExpiredAt(time.Now())
}

func (a *Account) IsDailyQuotaPeriodExpiredAt(now time.Time) bool {
	start := a.getExtraTime("quota_daily_start")
	if a.GetQuotaDailyResetMode() == "fixed" {
		return a.isFixedDailyPeriodExpiredAt(start, now)
	}
	return isPeriodExpiredAt(start, 24*time.Hour, now)
}

// IsWeeklyQuotaPeriodExpired 检查周配额周期是否已过期（用于显示层判断是否需要将 used 归零）
func (a *Account) IsWeeklyQuotaPeriodExpired() bool {
	return a.IsWeeklyQuotaPeriodExpiredAt(time.Now())
}

func (a *Account) IsWeeklyQuotaPeriodExpiredAt(now time.Time) bool {
	start := a.getExtraTime("quota_weekly_start")
	if a.GetQuotaWeeklyResetMode() == "fixed" {
		return a.isFixedWeeklyPeriodExpiredAt(start, now)
	}
	return isPeriodExpiredAt(start, 7*24*time.Hour, now)
}

// IsQuotaExceeded 检查 API Key 账号配额是否已超限（任一维度超限即返回 true）
func (a *Account) IsQuotaExceeded() bool {
	return a.IsQuotaExceededAt(time.Now())
}

func (a *Account) IsQuotaExceededAt(now time.Time) bool {
	// 总额度
	if limit := a.GetQuotaLimit(); limit > 0 && a.GetQuotaUsed() >= limit {
		return true
	}
	// 日额度（周期过期视为未超限，下次 increment 会重置）
	if limit := a.GetQuotaDailyLimit(); limit > 0 {
		start := a.getExtraTime("quota_daily_start")
		var expired bool
		if a.GetQuotaDailyResetMode() == "fixed" {
			expired = a.isFixedDailyPeriodExpiredAt(start, now)
		} else {
			expired = isPeriodExpiredAt(start, 24*time.Hour, now)
		}
		if !expired && a.GetQuotaDailyUsed() >= limit {
			return true
		}
	}
	// 周额度
	if limit := a.GetQuotaWeeklyLimit(); limit > 0 {
		start := a.getExtraTime("quota_weekly_start")
		var expired bool
		if a.GetQuotaWeeklyResetMode() == "fixed" {
			expired = a.isFixedWeeklyPeriodExpiredAt(start, now)
		} else {
			expired = isPeriodExpiredAt(start, 7*24*time.Hour, now)
		}
		if !expired && a.GetQuotaWeeklyUsed() >= limit {
			return true
		}
	}
	return false
}

// GetWindowCostLimit 获取 5h 窗口费用阈值（美元）
// 返回 0 表示未启用
func (a *Account) GetWindowCostLimit() float64 {
	if a.Extra == nil {
		return 0
	}
	if v, ok := a.Extra["window_cost_limit"]; ok {
		return parseExtraFloat64(v)
	}
	return 0
}

// GetWindowCostStickyReserve 获取粘性会话预留额度（美元）
// 默认值为 10
func (a *Account) GetWindowCostStickyReserve() float64 {
	if a.Extra == nil {
		return 10.0
	}
	if v, ok := a.Extra["window_cost_sticky_reserve"]; ok {
		val := parseExtraFloat64(v)
		if val > 0 {
			return val
		}
	}
	return 10.0
}

// GetMaxSessions 获取最大并发会话数
// 返回 0 表示未启用
func (a *Account) GetMaxSessions() int {
	if a.Extra == nil {
		return 0
	}
	if v, ok := a.Extra["max_sessions"]; ok {
		return parseExtraInt(v)
	}
	return 0
}

// GetSessionIdleTimeoutMinutes 获取会话空闲超时分钟数
// 默认值为 5 分钟
func (a *Account) GetSessionIdleTimeoutMinutes() int {
	if a.Extra == nil {
		return 5
	}
	if v, ok := a.Extra["session_idle_timeout_minutes"]; ok {
		val := parseExtraInt(v)
		if val > 0 {
			return val
		}
	}
	return 5
}

// GetBaseRPM 获取基础 RPM 限制
// 返回 0 表示未启用（负数视为无效配置，按 0 处理）
func (a *Account) GetBaseRPM() int {
	if a.Extra == nil {
		return 0
	}
	if v, ok := a.Extra["base_rpm"]; ok {
		val := parseExtraInt(v)
		if val > 0 {
			return val
		}
	}
	return 0
}

// GetRPMStrategy 获取 RPM 策略
// "tiered" = 三区模型（默认）, "sticky_exempt" = 粘性豁免
func (a *Account) GetRPMStrategy() string {
	if a.Extra == nil {
		return "tiered"
	}
	if v, ok := a.Extra["rpm_strategy"]; ok {
		if s, ok := v.(string); ok && s == "sticky_exempt" {
			return "sticky_exempt"
		}
	}
	return "tiered"
}

// GetRPMStickyBuffer 获取 RPM 粘性缓冲数量
// Cache-driven: buffer = concurrency + maxSessions（覆盖幽灵窗口 + 稳态会话需求）
// floor = baseRPM / 5（向后兼容 maxSessions=0 且 concurrency=0 场景）
func (a *Account) GetRPMStickyBuffer() int {
	if a.Extra == nil {
		return 0
	}

	// 手动 override 最高优先级
	if v, ok := a.Extra["rpm_sticky_buffer"]; ok {
		val := parseExtraInt(v)
		if val > 0 {
			return val
		}
	}

	base := a.GetBaseRPM()
	if base <= 0 {
		return 0
	}

	// Cache-driven buffer = concurrency + maxSessions
	conc := a.Concurrency
	if conc < 0 {
		conc = 0
	}
	sess := a.GetMaxSessions()
	if sess < 0 {
		sess = 0
	}

	buffer := conc + sess

	// floor: 向后兼容
	floor := base / 5
	if floor < 1 {
		floor = 1
	}
	if buffer < floor {
		buffer = floor
	}

	return buffer
}

// CheckRPMSchedulability 根据当前 RPM 计数检查调度状态
// 复用 WindowCostSchedulability 三态：Schedulable / StickyOnly / NotSchedulable
func (a *Account) CheckRPMSchedulability(currentRPM int) WindowCostSchedulability {
	baseRPM := a.GetBaseRPM()
	if baseRPM <= 0 {
		return WindowCostSchedulable
	}

	if currentRPM < baseRPM {
		return WindowCostSchedulable
	}

	strategy := a.GetRPMStrategy()
	if strategy == "sticky_exempt" {
		return WindowCostStickyOnly // 粘性豁免无红区
	}

	// tiered: 黄区 + 红区
	buffer := a.GetRPMStickyBuffer()
	if currentRPM < baseRPM+buffer {
		return WindowCostStickyOnly
	}
	return WindowCostNotSchedulable
}

// CheckWindowCostSchedulability 根据当前窗口费用检查调度状态
// - 费用 < 阈值: WindowCostSchedulable（可正常调度）
// - 费用 >= 阈值 且 < 阈值+预留: WindowCostStickyOnly（仅粘性会话）
// - 费用 >= 阈值+预留: WindowCostNotSchedulable（不可调度）
func (a *Account) CheckWindowCostSchedulability(currentWindowCost float64) WindowCostSchedulability {
	limit := a.GetWindowCostLimit()
	if limit <= 0 {
		return WindowCostSchedulable
	}

	if currentWindowCost < limit {
		return WindowCostSchedulable
	}

	stickyReserve := a.GetWindowCostStickyReserve()
	if currentWindowCost < limit+stickyReserve {
		return WindowCostStickyOnly
	}

	return WindowCostNotSchedulable
}

// GetCurrentWindowStartTime 获取当前有效的窗口开始时间
// 逻辑：
// 1. 如果窗口未过期（SessionWindowEnd 存在且在当前时间之后），使用记录的 SessionWindowStart
// 2. 否则（窗口过期或未设置），使用新的预测窗口开始时间（从当前整点开始）
func (a *Account) GetCurrentWindowStartTime() time.Time {
	now := time.Now()

	// 窗口未过期，使用记录的窗口开始时间
	if a.SessionWindowStart != nil && a.SessionWindowEnd != nil && now.Before(*a.SessionWindowEnd) {
		return *a.SessionWindowStart
	}

	// 窗口已过期或未设置，预测新的窗口开始时间（从当前整点开始）
	// 与 ratelimit_service.go 中 UpdateSessionWindow 的预测逻辑保持一致
	return time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())
}

// parseExtraFloat64 从 extra 字段解析 float64 值
func parseExtraFloat64(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return f
		}
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	}
	return 0
}

// parseExtraInt 从 extra 字段解析 int 值
// ParseExtraInt 从 extra 字段的 any 值解析为 int。
// 支持 int, int64, float64, json.Number, string 类型，无法解析时返回 0。
func ParseExtraInt(value any) int {
	return parseExtraInt(value)
}

func parseExtraInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i)
		}
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return i
		}
	}
	return 0
}
