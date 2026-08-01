package service

import (
	"context"
	"fmt"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

var (
	ErrProxyNotFound             = infraerrors.NotFound("PROXY_NOT_FOUND", "proxy not found")
	ErrProxyInUse                = infraerrors.Conflict("PROXY_IN_USE", "proxy is in use by accounts")
	ErrProxyAccountLimitExceeded = infraerrors.Conflict("PROXY_ACCOUNT_LIMIT_EXCEEDED", "proxy account binding limit exceeded")
	// ErrProxyPlatformInvalid 代理平台归属非法（空字符串表示通用代理）。
	ErrProxyPlatformInvalid = infraerrors.BadRequest("PROXY_PLATFORM_INVALID", "proxy platform is invalid; leave empty for a universal proxy")
	// ErrProxyRequiredAccountLevelInvalid 代理要求的账号等级非法（空字符串表示所有等级可用）。
	ErrProxyRequiredAccountLevelInvalid = infraerrors.BadRequest("PROXY_REQUIRED_ACCOUNT_LEVEL_INVALID", "proxy required_account_level is invalid; leave empty to allow all levels")
	// ErrProxyOwnerNotFound 归属用户不存在。
	ErrProxyOwnerNotFound = infraerrors.NotFound("PROXY_OWNER_NOT_FOUND", "proxy owner user not found")
	// ErrProxyOwnerConflict 代理已被其他用户的账号绑定，不能改归属；需先解绑再操作。
	ErrProxyOwnerConflict = infraerrors.Conflict("PROXY_OWNER_CONFLICT", "proxy is bound to accounts owned by other users; unbind them before changing the owner")
)

type ProxyRepository interface {
	Create(ctx context.Context, proxy *Proxy) error
	GetByID(ctx context.Context, id int64) (*Proxy, error)
	ListByIDs(ctx context.Context, ids []int64) ([]Proxy, error)
	Update(ctx context.Context, proxy *Proxy) error
	// UpdateWithOwnerAssignment 用于变更代理归属：在同一事务内锁定代理行、
	// 校验没有其他用户的账号绑定在该代理上（否则返回 ErrProxyOwnerConflict），再保存。
	// 锁与用户建号路径互斥，避免"改归属"与"绑账号"并发交叉后留下他人账号绑在专属代理上。
	UpdateWithOwnerAssignment(ctx context.Context, proxy *Proxy) error
	Delete(ctx context.Context, id int64) error

	List(ctx context.Context, params pagination.PaginationParams) ([]Proxy, *pagination.PaginationResult, error)
	ListWithFilters(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]Proxy, *pagination.PaginationResult, error)
	ListWithFiltersAndAccountCount(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]ProxyWithAccountCount, *pagination.PaginationResult, error)
	ListActive(ctx context.Context) ([]Proxy, error)
	ListActiveWithAccountCount(ctx context.Context) ([]ProxyWithAccountCount, error)
	ListActiveVisibleWithAccountCount(ctx context.Context, scope ProxyScope) ([]ProxyWithAccountCount, error)
	GetVisibleByID(ctx context.Context, scope ProxyScope, id int64) (*Proxy, error)
	FindVisibleActiveByEndpoint(ctx context.Context, scope ProxyScope, protocol, host string, port int, username, password string) (*Proxy, error)

	ExistsByHostPortAuth(ctx context.Context, host string, port int, username, password string) (bool, error)
	CountAccountsByProxyID(ctx context.Context, proxyID int64) (int64, error)
	ListAccountSummariesByProxyID(ctx context.Context, proxyID int64) ([]ProxyAccountSummary, error)

	// ResetRequiredAccountLevelNotIn 将 required_account_level 不在 keepLevels 内的代理
	// 重置为 ''（所有等级可用），用于账号等级被管理员删除后同步代理。返回受影响的行数。
	ResetRequiredAccountLevelNotIn(ctx context.Context, keepLevels []string) (int64, error)
}

// CreateProxyRequest 创建代理请求
type CreateProxyRequest struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	// Platform 为空表示通用代理（所有平台可用）。
	Platform string `json:"platform"`
	// RequiredAccountLevel 为空表示所有账号等级可用。
	RequiredAccountLevel string `json:"required_account_level"`
	MaxAccounts          int    `json:"max_accounts"`
}

// UpdateProxyRequest 更新代理请求
type UpdateProxyRequest struct {
	Name     *string `json:"name"`
	Protocol *string `json:"protocol"`
	Host     *string `json:"host"`
	Port     *int    `json:"port"`
	Username *string `json:"username"`
	Password *string `json:"password"`
	// Platform 为空字符串表示改为通用代理。
	Platform *string `json:"platform"`
	// RequiredAccountLevel 为空字符串表示改为所有等级可用。
	RequiredAccountLevel *string `json:"required_account_level"`
	Status               *string `json:"status"`
	MaxAccounts          *int    `json:"max_accounts"`
}

// ProxyService 代理管理服务
type ProxyService struct {
	proxyRepo ProxyRepository
}

// NewProxyService 创建代理服务实例
func NewProxyService(proxyRepo ProxyRepository) *ProxyService {
	return &ProxyService{
		proxyRepo: proxyRepo,
	}
}

// Create 创建代理
func (s *ProxyService) Create(ctx context.Context, req CreateProxyRequest) (*Proxy, error) {
	if req.MaxAccounts < 0 {
		return nil, infraerrors.BadRequest("PROXY_MAX_ACCOUNTS_INVALID", "max_accounts must be >= 0")
	}
	if !IsValidProxyPlatform(req.Platform) {
		return nil, ErrProxyPlatformInvalid
	}
	if !IsValidRequiredAccountLevel(req.RequiredAccountLevel) {
		return nil, ErrProxyRequiredAccountLevelInvalid
	}
	// 创建代理
	proxy := &Proxy{
		Name:                 req.Name,
		Protocol:             req.Protocol,
		Host:                 req.Host,
		Port:                 req.Port,
		Username:             req.Username,
		Password:             req.Password,
		Platform:             NormalizeProxyPlatform(req.Platform),
		RequiredAccountLevel: NormalizeRequiredAccountLevel(req.RequiredAccountLevel),
		Status:               StatusActive,
		MaxAccounts:          req.MaxAccounts,
	}

	if err := s.proxyRepo.Create(ctx, proxy); err != nil {
		return nil, fmt.Errorf("create proxy: %w", err)
	}

	return proxy, nil
}

// GetByID 根据ID获取代理
func (s *ProxyService) GetByID(ctx context.Context, id int64) (*Proxy, error) {
	proxy, err := s.proxyRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get proxy: %w", err)
	}
	return proxy, nil
}

// List 获取代理列表
func (s *ProxyService) List(ctx context.Context, params pagination.PaginationParams) ([]Proxy, *pagination.PaginationResult, error) {
	proxies, pagination, err := s.proxyRepo.List(ctx, params)
	if err != nil {
		return nil, nil, fmt.Errorf("list proxies: %w", err)
	}
	return proxies, pagination, nil
}

// ListActive 获取活跃代理列表
func (s *ProxyService) ListActive(ctx context.Context) ([]Proxy, error) {
	proxies, err := s.proxyRepo.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active proxies: %w", err)
	}
	return proxies, nil
}

// Update 更新代理
func (s *ProxyService) Update(ctx context.Context, id int64, req UpdateProxyRequest) (*Proxy, error) {
	proxy, err := s.proxyRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get proxy: %w", err)
	}

	// 更新字段
	if req.Name != nil {
		proxy.Name = *req.Name
	}

	if req.Protocol != nil {
		proxy.Protocol = *req.Protocol
	}

	if req.Host != nil {
		proxy.Host = *req.Host
	}

	if req.Port != nil {
		proxy.Port = *req.Port
	}

	if req.Username != nil {
		proxy.Username = *req.Username
	}

	if req.Password != nil {
		proxy.Password = *req.Password
	}

	if req.Platform != nil {
		if !IsValidProxyPlatform(*req.Platform) {
			return nil, ErrProxyPlatformInvalid
		}
		proxy.Platform = NormalizeProxyPlatform(*req.Platform)
	}

	if req.RequiredAccountLevel != nil {
		if !IsValidRequiredAccountLevel(*req.RequiredAccountLevel) {
			return nil, ErrProxyRequiredAccountLevelInvalid
		}
		proxy.RequiredAccountLevel = NormalizeRequiredAccountLevel(*req.RequiredAccountLevel)
	}

	if req.Status != nil {
		proxy.Status = *req.Status
	}
	if req.MaxAccounts != nil {
		if *req.MaxAccounts < 0 {
			return nil, infraerrors.BadRequest("PROXY_MAX_ACCOUNTS_INVALID", "max_accounts must be >= 0")
		}
		proxy.MaxAccounts = *req.MaxAccounts
	}

	if err := s.proxyRepo.Update(ctx, proxy); err != nil {
		return nil, fmt.Errorf("update proxy: %w", err)
	}

	return proxy, nil
}

// Delete 删除代理
func (s *ProxyService) Delete(ctx context.Context, id int64) error {
	// 检查代理是否存在
	_, err := s.proxyRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get proxy: %w", err)
	}

	if err := s.proxyRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete proxy: %w", err)
	}

	return nil
}

// TestConnection 测试代理连接（需要实现具体测试逻辑）
func (s *ProxyService) TestConnection(ctx context.Context, id int64) error {
	proxy, err := s.proxyRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get proxy: %w", err)
	}

	// TODO: 实现代理连接测试逻辑
	// 可以尝试通过代理发送测试请求
	_ = proxy

	return nil
}

// GetURL 获取代理URL
func (s *ProxyService) GetURL(ctx context.Context, id int64) (string, error) {
	proxy, err := s.proxyRepo.GetByID(ctx, id)
	if err != nil {
		return "", fmt.Errorf("get proxy: %w", err)
	}

	return proxy.URL(), nil
}
