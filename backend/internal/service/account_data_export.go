package service

import (
	"encoding/json"
	"time"
)

type AccountDataPayload struct {
	Type       string               `json:"type,omitempty"`
	Version    int                  `json:"version,omitempty"`
	ExportedAt string               `json:"exported_at"`
	Proxies    []AccountDataProxy   `json:"proxies"`
	Accounts   []AccountDataAccount `json:"accounts"`
}

type AccountDataProxy struct {
	ProxyKey        string `json:"proxy_key"`
	Name            string `json:"name"`
	Protocol        string `json:"protocol"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Username        string `json:"username,omitempty"`
	Password        string `json:"password,omitempty"`
	Status          string `json:"status"`
	ExpiresAt       *int64 `json:"expires_at,omitempty"`
	FallbackMode    string `json:"fallback_mode,omitempty"`
	BackupProxyName string `json:"backup_proxy_name,omitempty"`
	// BackupProxyKey is a local, stable extension. Upstream payloads that only
	// contain backup_proxy_name remain supported, while this key removes name
	// ambiguity when both sides are this implementation.
	BackupProxyKey string `json:"backup_proxy_key,omitempty"`
	ExpiryWarnDays int    `json:"expiry_warn_days,omitempty"`
	// Platform 为空表示通用代理（所有平台可用）。
	Platform string `json:"platform,omitempty"`
	// RequiredAccountLevel 为空表示所有账号等级可用。
	RequiredAccountLevel string `json:"required_account_level,omitempty"`
	MaxAccounts          *int   `json:"max_accounts,omitempty"`

	presence accountDataProxyPresence
}

type accountDataProxyPresence struct {
	expiresAt       bool
	fallbackMode    bool
	backupProxyName bool
	backupProxyKey  bool
	expiryWarnDays  bool
	platform        bool
	requiredLevel   bool
	maxAccounts     bool
}

// UnmarshalJSON records field presence so import can distinguish an omitted
// field (preserve an existing proxy value) from an explicit null/zero (clear or
// set it). The exported JSON shape remains the upstream-compatible flat DTO.
func (p *AccountDataProxy) UnmarshalJSON(data []byte) error {
	type alias AccountDataProxy
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*p = AccountDataProxy(decoded)
	_, p.presence.expiresAt = fields["expires_at"]
	_, p.presence.fallbackMode = fields["fallback_mode"]
	_, p.presence.backupProxyName = fields["backup_proxy_name"]
	_, p.presence.backupProxyKey = fields["backup_proxy_key"]
	_, p.presence.expiryWarnDays = fields["expiry_warn_days"]
	_, p.presence.platform = fields["platform"]
	_, p.presence.requiredLevel = fields["required_account_level"]
	_, p.presence.maxAccounts = fields["max_accounts"]
	return nil
}

func (p AccountDataProxy) HasExpiresAt() bool {
	return p.presence.expiresAt || p.ExpiresAt != nil
}

func (p AccountDataProxy) HasFallbackMode() bool {
	return p.presence.fallbackMode || p.FallbackMode != ""
}

func (p AccountDataProxy) HasBackupProxyName() bool {
	return p.presence.backupProxyName || p.BackupProxyName != ""
}

func (p AccountDataProxy) HasBackupProxyKey() bool {
	return p.presence.backupProxyKey || p.BackupProxyKey != ""
}

func (p AccountDataProxy) HasExpiryWarnDays() bool {
	return p.presence.expiryWarnDays || p.ExpiryWarnDays != 0
}

func (p AccountDataProxy) HasPlatform() bool {
	return p.presence.platform || p.Platform != ""
}

func (p AccountDataProxy) HasRequiredAccountLevel() bool {
	return p.presence.requiredLevel || p.RequiredAccountLevel != ""
}

func (p AccountDataProxy) HasMaxAccounts() bool {
	return p.presence.maxAccounts || p.MaxAccounts != nil
}

type AccountDataAccount struct {
	Name               string         `json:"name"`
	Notes              *string        `json:"notes,omitempty"`
	Platform           string         `json:"platform"`
	Type               string         `json:"type"`
	Credentials        map[string]any `json:"credentials"`
	Extra              map[string]any `json:"extra,omitempty"`
	ProxyKey           *string        `json:"proxy_key,omitempty"`
	Concurrency        int            `json:"concurrency"`
	Priority           int            `json:"priority"`
	RateMultiplier     *float64       `json:"rate_multiplier,omitempty"`
	ExpiresAt          *int64         `json:"expires_at,omitempty"`
	AutoPauseOnExpired *bool          `json:"auto_pause_on_expired,omitempty"`
	OwnerUserID        *int64         `json:"owner_user_id,omitempty"`
	ShareMode          string         `json:"share_mode,omitempty"`
	ShareStatus        string         `json:"share_status,omitempty"`
	SharePolicyID      *int64         `json:"share_policy_id,omitempty"`
}

func BuildAccountDataPayload(accounts []Account, proxies []Proxy, proxyKeyBuilder func(protocol, host string, port int, username, password string) string) AccountDataPayload {
	if proxies == nil {
		proxies = []Proxy{}
	}
	if accounts == nil {
		accounts = []Account{}
	}

	proxyKeyByID := make(map[int64]string, len(proxies))
	proxyNameByID := make(map[int64]string, len(proxies))
	for i := range proxies {
		p := proxies[i]
		proxyKeyByID[p.ID] = proxyKeyBuilder(p.Protocol, p.Host, p.Port, p.Username, p.Password)
		proxyNameByID[p.ID] = p.Name
	}
	dataProxies := make([]AccountDataProxy, 0, len(proxies))
	for i := range proxies {
		p := proxies[i]
		key := proxyKeyByID[p.ID]
		maxAccounts := p.MaxAccounts
		var expiresAt *int64
		if p.ExpiresAt != nil {
			unix := p.ExpiresAt.Unix()
			expiresAt = &unix
		}
		var backupProxyName, backupProxyKey string
		if p.BackupProxyID != nil {
			backupProxyName = proxyNameByID[*p.BackupProxyID]
			backupProxyKey = proxyKeyByID[*p.BackupProxyID]
		}
		dataProxies = append(dataProxies, AccountDataProxy{
			ProxyKey:             key,
			Name:                 p.Name,
			Protocol:             p.Protocol,
			Host:                 p.Host,
			Port:                 p.Port,
			Username:             p.Username,
			Password:             p.Password,
			Status:               p.Status,
			ExpiresAt:            expiresAt,
			FallbackMode:         p.FallbackMode,
			BackupProxyName:      backupProxyName,
			BackupProxyKey:       backupProxyKey,
			ExpiryWarnDays:       p.ExpiryWarnDays,
			Platform:             p.Platform,
			RequiredAccountLevel: p.RequiredAccountLevel,
			MaxAccounts:          &maxAccounts,
		})
	}

	dataAccounts := make([]AccountDataAccount, 0, len(accounts))
	for i := range accounts {
		acc := accounts[i]
		var proxyKey *string
		if acc.ProxyID != nil {
			if key, ok := proxyKeyByID[*acc.ProxyID]; ok {
				proxyKey = &key
			}
		}
		var expiresAt *int64
		if acc.ExpiresAt != nil {
			v := acc.ExpiresAt.Unix()
			expiresAt = &v
		}
		dataAccounts = append(dataAccounts, AccountDataAccount{
			Name:               acc.Name,
			Notes:              acc.Notes,
			Platform:           acc.Platform,
			Type:               acc.Type,
			Credentials:        acc.Credentials,
			Extra:              acc.Extra,
			ProxyKey:           proxyKey,
			Concurrency:        acc.Concurrency,
			Priority:           acc.Priority,
			RateMultiplier:     acc.RateMultiplier,
			ExpiresAt:          expiresAt,
			AutoPauseOnExpired: &acc.AutoPauseOnExpired,
			OwnerUserID:        acc.OwnerUserID,
			ShareMode:          acc.ShareMode,
			ShareStatus:        acc.ShareStatus,
			SharePolicyID:      acc.SharePolicyID,
		})
	}

	return AccountDataPayload{
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Proxies:    dataProxies,
		Accounts:   dataAccounts,
	}
}
