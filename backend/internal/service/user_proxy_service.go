package service

import (
	"context"
	"strings"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// OwnedProxyRepository 将用户写权限与平台代理的可见权限分开。
// UpdateOwned 和 DeleteOwnedIfUnused 必须在行锁内重新核验归属。
type OwnedProxyRepository interface {
	Create(context.Context, *Proxy) error
	GetByID(context.Context, int64) (*Proxy, error)
	ListOwnedWithAccountCount(context.Context, int64) ([]ProxyWithAccountCount, error)
	UpdateOwned(context.Context, int64, *Proxy) error
	DeleteOwnedIfUnused(context.Context, int64, int64) error
}

type CreateOwnedProxyInput struct {
	Name        string
	Protocol    string
	Host        string
	Port        int
	Username    string
	Password    string
	MaxAccounts int
}

type UpdateOwnedProxyInput struct {
	Name        *string
	Protocol    *string
	Host        *string
	Port        *int
	Username    *string
	Password    *string
	Status      *string
	MaxAccounts *int
}

func (s *AccountShareModeService) ownedProxyRepository(ownerUserID int64) (OwnedProxyRepository, error) {
	if ownerUserID <= 0 {
		return nil, ErrProxyNotFound
	}
	if s == nil {
		return nil, ErrServiceUnavailable
	}
	repo, ok := s.proxyRepo.(OwnedProxyRepository)
	if !ok || repo == nil {
		return nil, ErrServiceUnavailable
	}
	return repo, nil
}

func (s *AccountShareModeService) ListOwnedProxies(ctx context.Context, ownerUserID int64) ([]ProxyWithAccountCount, error) {
	repo, err := s.ownedProxyRepository(ownerUserID)
	if err != nil {
		return nil, err
	}
	return repo.ListOwnedWithAccountCount(ctx, ownerUserID)
}

func (s *AccountShareModeService) CreateOwnedProxy(ctx context.Context, ownerUserID int64, input CreateOwnedProxyInput) (*Proxy, error) {
	repo, err := s.ownedProxyRepository(ownerUserID)
	if err != nil {
		return nil, err
	}
	proxy := &Proxy{
		Name: strings.TrimSpace(input.Name), Protocol: strings.ToLower(strings.TrimSpace(input.Protocol)),
		Host: strings.TrimSpace(input.Host), Port: input.Port,
		Username: input.Username, Password: input.Password,
		OwnerUserID: &ownerUserID, Status: StatusActive, MaxAccounts: input.MaxAccounts,
		FallbackMode: FallbackModeNone, ExpiryWarnDays: proxyExpiryWarnDaysOrDefaultValue(nil),
	}
	if err := validateOwnedProxyInput(proxy); err != nil {
		return nil, err
	}
	if err := repo.Create(ctx, proxy); err != nil {
		return nil, err
	}
	return proxy, nil
}

func (s *AccountShareModeService) UpdateOwnedProxy(ctx context.Context, ownerUserID, id int64, input UpdateOwnedProxyInput) (*Proxy, error) {
	repo, err := s.ownedProxyRepository(ownerUserID)
	if err != nil {
		return nil, err
	}
	proxy, err := repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !proxy.IsOwnedBy(ownerUserID) {
		return nil, ErrProxyNotFound
	}
	// 复制快照；未提供的凭据和已有到期、备用配置保持不变。
	candidate := *proxy
	if input.Name != nil {
		candidate.Name = strings.TrimSpace(*input.Name)
	}
	if input.Protocol != nil {
		candidate.Protocol = strings.ToLower(strings.TrimSpace(*input.Protocol))
	}
	if input.Host != nil {
		candidate.Host = strings.TrimSpace(*input.Host)
	}
	if input.Port != nil {
		candidate.Port = *input.Port
	}
	if input.Username != nil {
		candidate.Username = *input.Username
	}
	if input.Password != nil {
		candidate.Password = *input.Password
	}
	if input.Status != nil {
		candidate.Status = *input.Status
	}
	if input.MaxAccounts != nil {
		candidate.MaxAccounts = *input.MaxAccounts
	}
	if err := validateOwnedProxyInput(&candidate); err != nil {
		return nil, err
	}
	recoverCredentials := grokProxyRecoveryRelevantChange(proxy, &candidate)
	recovery, hasRecovery := s.rateLimitService.(grokProxyCredentialRecoveryScheduler)
	if recoverCredentials && !hasRecovery {
		return nil, ErrServiceUnavailable
	}
	if err := repo.UpdateOwned(ctx, ownerUserID, &candidate); err != nil {
		return nil, err
	}
	if recoverCredentials {
		recovery.ScheduleGrokProxyCredentialRecovery(candidate.ID)
	}
	return &candidate, nil
}

func (s *AccountShareModeService) DeleteOwnedProxy(ctx context.Context, ownerUserID, id int64) error {
	repo, err := s.ownedProxyRepository(ownerUserID)
	if err != nil {
		return err
	}
	return repo.DeleteOwnedIfUnused(ctx, ownerUserID, id)
}

func validateOwnedProxyInput(proxy *Proxy) error {
	if proxy.Name == "" || utf8.RuneCountInString(proxy.Name) > 100 {
		return infraerrors.BadRequest("PROXY_NAME_INVALID", "代理名称不能为空且不能超过 100 个字符")
	}
	switch proxy.Protocol {
	case "http", "https", "socks5", "socks5h":
	default:
		return infraerrors.BadRequest("PROXY_PROTOCOL_INVALID", "代理协议必须为 http、https、socks5 或 socks5h")
	}
	if proxy.Host == "" || utf8.RuneCountInString(proxy.Host) > 255 || strings.ContainsAny(proxy.Host, "/\\@?# \t\r\n") {
		return infraerrors.BadRequest("PROXY_HOST_INVALID", "代理地址应为主机名或 IP，不包含协议、路径或空白")
	}
	if proxy.Port < 1 || proxy.Port > 65535 {
		return infraerrors.BadRequest("PROXY_PORT_INVALID", "代理端口必须介于 1 和 65535 之间")
	}
	if utf8.RuneCountInString(proxy.Username) > 100 || utf8.RuneCountInString(proxy.Password) > 100 {
		return infraerrors.BadRequest("PROXY_CREDENTIALS_INVALID", "代理用户名和密码不能超过 100 个字符")
	}
	if proxy.Status != StatusActive && proxy.Status != "inactive" {
		return infraerrors.BadRequest("PROXY_STATUS_INVALID", "代理状态必须为 active 或 inactive")
	}
	return validateProxyMaxAccountsValue(proxy.MaxAccounts)
}
