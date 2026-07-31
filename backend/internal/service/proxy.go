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

type Proxy struct {
	ID       int64
	Name     string
	Protocol string
	Host     string
	Port     int
	Username string
	Password string
	// OwnerUserID 自迁移 256 起恒为 nil：所有代理均由平台管理。
	OwnerUserID *int64
	// Platform 为空字符串表示通用代理（所有平台可用）。
	Platform string
	// RequiredAccountLevel 为空字符串表示所有账号等级可用。
	RequiredAccountLevel string
	Status               string
	// MaxAccounts controls how many accounts may bind to this proxy. 0 means unlimited.
	MaxAccounts int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (p *Proxy) IsActive() bool {
	return p.Status == StatusActive
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

// IsOwnedBy 判断代理是否属于指定用户（遗留自有代理）。
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
// OwnerUserID 是历史遗留（grandfather）豁免：自本次更新起用户不能再上传代理，
// 用户端只能选择平台代理（owner_user_id IS NULL）。但更新前已存在、且仍绑定在
// 老用户账号上的自有代理需要在重新鉴权/更新时保持可见，避免老用户掉线。
//   - 选择器等“挑选平台代理”的场景传 0，只返回平台代理；
//   - 账号重新鉴权/更新等场景传账号 owner，使其既能看到平台代理，
//     也能继续看到自己名下的遗留自有代理。
type ProxyScope struct {
	Platform     string
	AccountLevel string
	OwnerUserID  int64
}

// NewProxyScope 基于账号平台与等级构造归一化后的代理筛选范围（不含遗留归属豁免）。
func NewProxyScope(platform, accountLevel string) ProxyScope {
	return ProxyScope{Platform: platform, AccountLevel: accountLevel}.Normalized()
}

// NewOwnedProxyScope 在 NewProxyScope 基础上附带账号 owner 的遗留归属豁免。
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
// 若 scope 带遗留归属豁免，则该用户自己的遗留自有代理也视为可用。
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
	AccountCount   int64
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
