package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Sora2APISyncService 用于同步 Sora 账号到 sora2api token 池
type Sora2APISyncService struct {
	sora2api    *Sora2APIService
	accountRepo AccountRepository
	httpClient  *http.Client
}

func NewSora2APISyncService(sora2api *Sora2APIService, accountRepo AccountRepository) *Sora2APISyncService {
	return &Sora2APISyncService{
		sora2api:    sora2api,
		accountRepo: accountRepo,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Sora2APISyncService) Enabled() bool {
	return s != nil && s.sora2api != nil && s.sora2api.AdminEnabled()
}

// SyncAccount 将 Sora 账号同步到 sora2api（导入或更新）
func (s *Sora2APISyncService) SyncAccount(ctx context.Context, account *Account) error {
	if !s.Enabled() {
		return nil
	}
	if account == nil || account.Platform != PlatformSora {
		return nil
	}

	accessToken := strings.TrimSpace(account.GetCredential("access_token"))
	if accessToken == "" {
		return errors.New("sora 账号缺少 access_token")
	}

	email, updated := s.resolveAccountEmail(ctx, account)
	if email == "" {
		return errors.New("无法解析 Sora 账号邮箱")
	}
	if updated && s.accountRepo != nil {
		if err := s.accountRepo.Update(ctx, account); err != nil {
			log.Printf("[SoraSync] 更新账号邮箱失败: account_id=%d err=%v", account.ID, err)
		}
	}

	item := Sora2APIImportTokenItem{
		Email:            email,
		AccessToken:      accessToken,
		SessionToken:     strings.TrimSpace(account.GetCredential("session_token")),
		RefreshToken:     strings.TrimSpace(account.GetCredential("refresh_token")),
		ClientID:         strings.TrimSpace(account.GetCredential("client_id")),
		Remark:           account.Name,
		IsActive:         account.IsActive() && account.Schedulable,
		ImageEnabled:     true,
		VideoEnabled:     true,
		ImageConcurrency: normalizeSoraConcurrency(account.Concurrency),
		VideoConcurrency: normalizeSoraConcurrency(account.Concurrency),
	}

	if err := s.sora2api.ImportTokens(ctx, []Sora2APIImportTokenItem{item}); err != nil {
		return err
	}
	return nil
}

// DisableAccount 禁用 sora2api 中的 token
func (s *Sora2APISyncService) DisableAccount(ctx context.Context, account *Account) error {
	if !s.Enabled() {
		return nil
	}
	if account == nil || account.Platform != PlatformSora {
		return nil
	}
	tokenID, err := s.resolveTokenID(ctx, account)
	if err != nil {
		return err
	}
	return s.sora2api.DisableToken(ctx, tokenID)
}

// DeleteAccount 删除 sora2api 中的 token
func (s *Sora2APISyncService) DeleteAccount(ctx context.Context, account *Account) error {
	if !s.Enabled() {
		return nil
	}
	if account == nil || account.Platform != PlatformSora {
		return nil
	}
	tokenID, err := s.resolveTokenID(ctx, account)
	if err != nil {
		return err
	}
	return s.sora2api.DeleteToken(ctx, tokenID)
}

func normalizeSoraConcurrency(value int) int {
	if value <= 0 {
		return -1
	}
	return value
}

func (s *Sora2APISyncService) resolveAccountEmail(ctx context.Context, account *Account) (string, bool) {
	if account == nil {
		return "", false
	}
	if email := strings.TrimSpace(account.GetCredential("email")); email != "" {
		return email, false
	}
	if email := strings.TrimSpace(account.GetExtraString("email")); email != "" {
		if account.Credentials == nil {
			account.Credentials = map[string]any{}
		}
		account.Credentials["email"] = email
		return email, true
	}
	if email := strings.TrimSpace(account.GetExtraString("sora_email")); email != "" {
		if account.Credentials == nil {
			account.Credentials = map[string]any{}
		}
		account.Credentials["email"] = email
		return email, true
	}

	accessToken := strings.TrimSpace(account.GetCredential("access_token"))
	if accessToken != "" {
		if email := extractEmailFromAccessToken(accessToken); email != "" {
			if account.Credentials == nil {
				account.Credentials = map[string]any{}
			}
			account.Credentials["email"] = email
			return email, true
		}
		if email := s.fetchEmailFromSora(ctx, accessToken); email != "" {
			if account.Credentials == nil {
				account.Credentials = map[string]any{}
			}
			account.Credentials["email"] = email
			return email, true
		}
	}

	return "", false
}

func (s *Sora2APISyncService) resolveTokenID(ctx context.Context, account *Account) (int64, error) {
	if account == nil {
		return 0, errors.New("account is nil")
	}

	if account.Extra != nil {
		if v, ok := account.Extra["sora2api_token_id"]; ok {
			if id, ok := v.(float64); ok && id > 0 {
				return int64(id), nil
			}
			if id, ok := v.(int64); ok && id > 0 {
				return id, nil
			}
			if id, ok := v.(int); ok && id > 0 {
				return int64(id), nil
			}
		}
	}

	email := strings.TrimSpace(account.GetCredential("email"))
	if email == "" {
		email, _ = s.resolveAccountEmail(ctx, account)
	}
	if email == "" {
		return 0, errors.New("sora2api token email missing")
	}

	tokenID, err := s.findTokenIDByEmail(ctx, email)
	if err != nil {
		return 0, err
	}
	return tokenID, nil
}

func (s *Sora2APISyncService) findTokenIDByEmail(ctx context.Context, email string) (int64, error) {
	if !s.Enabled() {
		return 0, errors.New("sora2api admin not configured")
	}
	tokens, err := s.sora2api.ListTokens(ctx)
	if err != nil {
		return 0, err
	}
	for _, token := range tokens {
		if strings.EqualFold(strings.TrimSpace(token.Email), strings.TrimSpace(email)) {
			return token.ID, nil
		}
	}
	return 0, fmt.Errorf("sora2api token not found for email: %s", email)
}

func extractEmailFromAccessToken(accessToken string) string {
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	claims := jwt.MapClaims{}
	_, _, err := parser.ParseUnverified(accessToken, claims)
	if err != nil {
		return ""
	}
	if email, ok := claims["email"].(string); ok && strings.TrimSpace(email) != "" {
		return email
	}
	if profile, ok := claims["https://api.openai.com/profile"].(map[string]any); ok {
		if email, ok := profile["email"].(string); ok && strings.TrimSpace(email) != "" {
			return email
		}
	}
	return ""
}

func (s *Sora2APISyncService) fetchEmailFromSora(ctx context.Context, accessToken string) string {
	if s.httpClient == nil {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, soraMeAPIURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", "Sora/1.2026.007 (Android 15; 24122RKC7C; build 2600700)")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ""
	}
	if email, ok := payload["email"].(string); ok && strings.TrimSpace(email) != "" {
		return email
	}
	return ""
}
