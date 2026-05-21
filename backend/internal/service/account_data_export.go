package service

import "time"

type AccountDataPayload struct {
	Type       string               `json:"type,omitempty"`
	Version    int                  `json:"version,omitempty"`
	ExportedAt string               `json:"exported_at"`
	Proxies    []AccountDataProxy   `json:"proxies"`
	Accounts   []AccountDataAccount `json:"accounts"`
}

type AccountDataProxy struct {
	ProxyKey string `json:"proxy_key"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Status   string `json:"status"`
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
	dataProxies := make([]AccountDataProxy, 0, len(proxies))
	for i := range proxies {
		p := proxies[i]
		key := proxyKeyBuilder(p.Protocol, p.Host, p.Port, p.Username, p.Password)
		proxyKeyByID[p.ID] = key
		dataProxies = append(dataProxies, AccountDataProxy{
			ProxyKey: key,
			Name:     p.Name,
			Protocol: p.Protocol,
			Host:     p.Host,
			Port:     p.Port,
			Username: p.Username,
			Password: p.Password,
			Status:   p.Status,
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
