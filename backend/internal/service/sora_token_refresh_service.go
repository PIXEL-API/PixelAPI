package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const defaultSoraClientID = "app_LlGpXReQgckcGGUo2JrYvtJK"

// SoraTokenRefreshService handles Sora access token refresh.
type SoraTokenRefreshService struct {
	accountRepo     AccountRepository
	soraAccountRepo SoraAccountRepository
	settingService  *SettingService
	httpUpstream    HTTPUpstream
	cfg             *config.Config
	stopCh          chan struct{}
	stopOnce        sync.Once
}

func NewSoraTokenRefreshService(
	accountRepo AccountRepository,
	soraAccountRepo SoraAccountRepository,
	settingService *SettingService,
	httpUpstream HTTPUpstream,
	cfg *config.Config,
) *SoraTokenRefreshService {
	return &SoraTokenRefreshService{
		accountRepo:     accountRepo,
		soraAccountRepo: soraAccountRepo,
		settingService:  settingService,
		httpUpstream:    httpUpstream,
		cfg:             cfg,
		stopCh:          make(chan struct{}),
	}
}

func (s *SoraTokenRefreshService) Start() {
	if s == nil {
		return
	}
	go s.refreshLoop()
}

func (s *SoraTokenRefreshService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
}

func (s *SoraTokenRefreshService) refreshLoop() {
	for {
		wait := s.nextRunDelay()
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
			s.refreshOnce()
		case <-s.stopCh:
			timer.Stop()
			return
		}
	}
}

func (s *SoraTokenRefreshService) refreshOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	if !s.isEnabled(ctx) {
		log.Println("[SoraTokenRefresh] disabled by settings")
		return
	}
	if s.accountRepo == nil || s.soraAccountRepo == nil {
		log.Println("[SoraTokenRefresh] repository not configured")
		return
	}

	accounts, err := s.accountRepo.ListByPlatform(ctx, PlatformSora)
	if err != nil {
		log.Printf("[SoraTokenRefresh] list accounts failed: %v", err)
		return
	}
	if len(accounts) == 0 {
		log.Println("[SoraTokenRefresh] no sora accounts")
		return
	}
	ids := make([]int64, 0, len(accounts))
	accountMap := make(map[int64]*Account, len(accounts))
	for i := range accounts {
		acc := accounts[i]
		ids = append(ids, acc.ID)
		accountMap[acc.ID] = &acc
	}
	accountExtras, err := s.soraAccountRepo.GetByAccountIDs(ctx, ids)
	if err != nil {
		log.Printf("[SoraTokenRefresh] load sora accounts failed: %v", err)
		return
	}

	success := 0
	failed := 0
	skipped := 0
	for accountID, account := range accountMap {
		extra := accountExtras[accountID]
		if extra == nil {
			skipped++
			continue
		}
		result, err := s.refreshForAccount(ctx, account, extra)
		if err != nil {
			failed++
			log.Printf("[SoraTokenRefresh] account %d refresh failed: %v", accountID, err)
			continue
		}
		if result == nil {
			skipped++
			continue
		}

		updates := map[string]any{
			"access_token": result.AccessToken,
		}
		if result.RefreshToken != "" {
			updates["refresh_token"] = result.RefreshToken
		}
		if result.Email != "" {
			updates["email"] = result.Email
		}
		if err := s.soraAccountRepo.Upsert(ctx, accountID, updates); err != nil {
			failed++
			log.Printf("[SoraTokenRefresh] account %d update failed: %v", accountID, err)
			continue
		}
		success++
	}
	log.Printf("[SoraTokenRefresh] done: success=%d failed=%d skipped=%d", success, failed, skipped)
}

func (s *SoraTokenRefreshService) refreshForAccount(ctx context.Context, account *Account, extra *SoraAccount) (*soraRefreshResult, error) {
	if extra == nil {
		return nil, nil
	}
	if strings.TrimSpace(extra.SessionToken) == "" && strings.TrimSpace(extra.RefreshToken) == "" {
		return nil, nil
	}

	if extra.SessionToken != "" {
		result, err := s.refreshWithSessionToken(ctx, account, extra.SessionToken)
		if err == nil && result != nil && result.AccessToken != "" {
			return result, nil
		}
		if strings.TrimSpace(extra.RefreshToken) == "" {
			return nil, err
		}
	}

	clientID := strings.TrimSpace(extra.ClientID)
	if clientID == "" {
		clientID = defaultSoraClientID
	}
	return s.refreshWithRefreshToken(ctx, account, extra.RefreshToken, clientID)
}

type soraRefreshResult struct {
	AccessToken  string
	RefreshToken string
	Email        string
}

type soraSessionResponse struct {
	AccessToken string `json:"accessToken"`
	User        struct {
		Email string `json:"email"`
	} `json:"user"`
}

type soraRefreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func (s *SoraTokenRefreshService) refreshWithSessionToken(ctx context.Context, account *Account, sessionToken string) (*soraRefreshResult, error) {
	if s.httpUpstream == nil {
		return nil, fmt.Errorf("upstream not configured")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://sora.chatgpt.com/api/auth/session", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", "__Secure-next-auth.session-token="+sessionToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Origin", "https://sora.chatgpt.com")
	req.Header.Set("Referer", "https://sora.chatgpt.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	enableTLS := false
	if s.cfg != nil {
		enableTLS = s.cfg.Gateway.TLSFingerprint.Enabled
	}
	proxyURL := ""
	accountConcurrency := 0
	accountID := int64(0)
	if account != nil {
		accountID = account.ID
		accountConcurrency = account.Concurrency
		if account.Proxy != nil {
			proxyURL = account.Proxy.URL()
		}
	}
	resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, accountID, accountConcurrency, enableTLS)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("session refresh failed: %d", resp.StatusCode)
	}
	var payload soraSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.AccessToken == "" {
		return nil, errors.New("session refresh missing access token")
	}
	return &soraRefreshResult{AccessToken: payload.AccessToken, Email: payload.User.Email}, nil
}

func (s *SoraTokenRefreshService) refreshWithRefreshToken(ctx context.Context, account *Account, refreshToken, clientID string) (*soraRefreshResult, error) {
	if s.httpUpstream == nil {
		return nil, fmt.Errorf("upstream not configured")
	}
	payload := map[string]any{
		"client_id":     clientID,
		"grant_type":    "refresh_token",
		"redirect_uri":  "com.openai.chat://auth0.openai.com/ios/com.openai.chat/callback",
		"refresh_token": refreshToken,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://auth.openai.com/oauth/token", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	enableTLS := false
	if s.cfg != nil {
		enableTLS = s.cfg.Gateway.TLSFingerprint.Enabled
	}
	proxyURL := ""
	accountConcurrency := 0
	accountID := int64(0)
	if account != nil {
		accountID = account.ID
		accountConcurrency = account.Concurrency
		if account.Proxy != nil {
			proxyURL = account.Proxy.URL()
		}
	}
	resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, accountID, accountConcurrency, enableTLS)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("refresh token failed: %d", resp.StatusCode)
	}
	var payloadResp soraRefreshResponse
	if err := json.NewDecoder(resp.Body).Decode(&payloadResp); err != nil {
		return nil, err
	}
	if payloadResp.AccessToken == "" {
		return nil, errors.New("refresh token missing access token")
	}
	return &soraRefreshResult{AccessToken: payloadResp.AccessToken, RefreshToken: payloadResp.RefreshToken}, nil
}

func (s *SoraTokenRefreshService) nextRunDelay() time.Duration {
	location := time.Local
	if s.cfg != nil && strings.TrimSpace(s.cfg.Timezone) != "" {
		if tz, err := time.LoadLocation(strings.TrimSpace(s.cfg.Timezone)); err == nil {
			location = tz
		}
	}
	now := time.Now().In(location)
	next := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location).Add(24 * time.Hour)
	return time.Until(next)
}

func (s *SoraTokenRefreshService) isEnabled(ctx context.Context) bool {
	if s.settingService == nil {
		return s.cfg != nil && s.cfg.Sora.TokenRefresh.Enabled
	}
	cfg := s.settingService.GetSoraConfig(ctx)
	return cfg.TokenRefresh.Enabled
}
