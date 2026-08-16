package service

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	FallbackModeNone   = "none"
	FallbackModeProxy  = "proxy"
	FallbackModeDirect = "direct"
)

// NormalizeProxyFallbackMode 归一化代理到期回退模式，空值归一为 none（与库表默认一致）。
func NormalizeProxyFallbackMode(mode string) string {
	if normalized := strings.ToLower(strings.TrimSpace(mode)); normalized != "" {
		return normalized
	}
	return FallbackModeNone
}

type Proxy struct {
	ID       int64
	Name     string
	Protocol string
	Host     string
	Port     int
	Username string
	Password string
	// OwnerUserID 为 nil 表示平台代理（所有用户可见）；非 nil 表示专属代理，
	// 仅对该用户显示可用。来源有二：管理员显式指派（自 1.2.29 起），
	// 以及迁移 256 保留的历史用户自有代理。
	OwnerUserID *int64
	// Platform 为空字符串表示通用代理（所有平台可用）。
	Platform string
	// RequiredAccountLevel 为空字符串表示所有账号等级可用。
	RequiredAccountLevel string
	Status               string
	// MaxAccounts controls how many accounts may bind to this proxy. 0 means unlimited.
	MaxAccounts    int
	ExpiresAt      *time.Time
	FallbackMode   string
	BackupProxyID  *int64
	ExpiryWarnDays int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (p *Proxy) IsActive() bool {
	return p.Status == StatusActive
}

// IsExpired 报告代理是否已达到有效期边界；空有效期表示永不过期。
func (p *Proxy) IsExpired(now time.Time) bool {
	return p != nil && p.ExpiresAt != nil && !p.ExpiresAt.After(now)
}

// IsUniversal 判断代理是否为通用代理（不限平台）。
func (p *Proxy) IsUniversal() bool {
	return p != nil && p.Platform == ""
}

// AllowsScope 判断代理是否可用于指定平台与账号等级。
// 平台为空表示不限定平台；账号等级为空/unknown 表示只能选择"所有等级可用"的代理。
func (p *Proxy) AllowsScope(platform, accountLevel string) bool {
	if p == nil {
		return false
	}
	if p.Platform != "" {
		if !strings.EqualFold(p.Platform, strings.TrimSpace(platform)) {
			return false
		}
	}
	if p.RequiredAccountLevel == "" {
		return true
	}
	return p.RequiredAccountLevel == NormalizeRequiredAccountLevel(accountLevel)
}

// IsOwnedBy 判断代理是否归属于指定用户（专属代理）。
func (p *Proxy) IsOwnedBy(userID int64) bool {
	return p != nil && userID > 0 && p.OwnerUserID != nil && *p.OwnerUserID == userID
}

// NormalizeProxyPlatform 归一化代理平台归属，非法值归一为通用代理（空字符串）。
func NormalizeProxyPlatform(platform string) string {
	normalized := strings.ToLower(strings.TrimSpace(platform))
	if normalized == "" || !IsSupportedAccountPlatform(normalized) {
		return ""
	}
	return normalized
}

// IsValidProxyPlatform 校验代理平台归属，空字符串表示通用代理。
func IsValidProxyPlatform(platform string) bool {
	normalized := strings.ToLower(strings.TrimSpace(platform))
	return normalized == "" || IsSupportedAccountPlatform(normalized)
}

// ProxyScope 描述“按账号选代理”的筛选范围：账号所属平台 + 账号等级。
// Platform 为空表示调用方未提供平台信息，此时只有通用代理可用；
// AccountLevel 为空/unknown 表示只有“所有等级可用”的代理可用。
//
// OwnerUserID 控制专属代理的可见性：owner_user_id 非空的代理（管理员指派的
// 专属代理，或迁移 256 保留的历史用户自有代理）仅对其归属用户可见可用。
//   - 传 0 时只返回平台代理（owner_user_id IS NULL）；
//   - 传用户 ID 时，除平台代理外还放行该用户名下的专属代理。
//     专属代理不受平台/等级筛选限制，对归属用户全量可见。
type ProxyScope struct {
	Platform     string
	AccountLevel string
	OwnerUserID  int64
}

// NewProxyScope 基于账号平台与等级构造归一化后的代理筛选范围（不含遗留归属豁免）。
func NewProxyScope(platform, accountLevel string) ProxyScope {
	return ProxyScope{Platform: platform, AccountLevel: accountLevel}.Normalized()
}

// NewOwnedProxyScope 在 NewProxyScope 基础上附带账号 owner 的专属代理可见范围。
func NewOwnedProxyScope(platform, accountLevel string, ownerUserID int64) ProxyScope {
	scope := NewProxyScope(platform, accountLevel)
	if ownerUserID > 0 {
		scope.OwnerUserID = ownerUserID
	}
	return scope
}

// Normalized 归一化平台与等级取值，便于直接用于查询与比较。
func (s ProxyScope) Normalized() ProxyScope {
	ownerUserID := s.OwnerUserID
	if ownerUserID < 0 {
		ownerUserID = 0
	}
	return ProxyScope{
		Platform:     NormalizeProxyPlatform(s.Platform),
		AccountLevel: NormalizeRequiredAccountLevel(s.AccountLevel),
		OwnerUserID:  ownerUserID,
	}
}

// Allows 判断代理是否落在该筛选范围内。
// 平台代理（owner_user_id IS NULL）按平台 + 等级筛选；
// 专属代理仅当归属于 scope 指定的用户时可用（不受平台/等级限制）。
func (s ProxyScope) Allows(p *Proxy) bool {
	if p == nil {
		return false
	}
	if p.OwnerUserID != nil {
		return p.IsOwnedBy(s.OwnerUserID)
	}
	return p.AllowsScope(s.Platform, s.AccountLevel)
}

func (p *Proxy) URL() string {
	u := &url.URL{
		Scheme: p.Protocol,
		Host:   net.JoinHostPort(p.Host, strconv.Itoa(p.Port)),
	}
	if p.Username != "" && p.Password != "" {
		u.User = url.UserPassword(p.Username, p.Password)
	}
	return u.String()
}

type ProxyWithAccountCount struct {
	Proxy
	AccountCount int64
	// OwnerUsername / OwnerEmail 仅在 OwnerUserID 非空时由管理端查询填充，用于展示归属用户。
	OwnerUsername  string
	OwnerEmail     string
	LatencyMs      *int64
	LatencyStatus  string
	LatencyMessage string
	IPAddress      string
	Country        string
	CountryCode    string
	Region         string
	City           string
	QualityStatus  string
	QualityScore   *int
	QualityGrade   string
	QualitySummary string
	QualityChecked *int64
}

func ProxyAccountLimitExceededError(proxyID, current, limit, additional int64) error {
	return infraerrors.Conflict(
		"PROXY_ACCOUNT_LIMIT_EXCEEDED",
		fmt.Sprintf("proxy %d account binding limit exceeded: %d/%d accounts would be bound; choose another proxy or raise the limit", proxyID, current+additional, limit),
	)
}

type ProxyAccountSummary struct {
	ID       int64
	Name     string
	Platform string
	Type     string
	Notes    *string
}
