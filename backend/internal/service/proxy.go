package service

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
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
	// OwnerUserID is nil for platform-managed proxies and set for user-owned proxies.
	OwnerUserID *int64
	Status      string
	// MaxAccounts controls how many accounts may bind to this proxy. 0 means unlimited.
	MaxAccounts int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (p *Proxy) IsActive() bool {
	return p.Status == StatusActive
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
