package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// OpenAIOAuthService handles OpenAI OAuth authentication flows
type OpenAIOAuthService struct {
	sessionStore         *openai.SessionStore
	proxyRepo            ProxyRepository
	oauthClient          OpenAIOAuthClient
	privacyClientFactory PrivacyClientFactory // 用于调用 chatgpt.com/backend-api（ImpersonateChrome）
	sessionTokenKey      []byte
}

const (
	openAIOAuthSessionTokenPrefix = "oai_oauth_v1."
	openAIOAuthSessionTokenAAD    = "sub2api/openai-oauth-session/v1"
	openAIOAuthSessionTokenSkew   = 5 * time.Minute
)

var (
	errOpenAIOAuthSessionTokenDisabled = errors.New("openai oauth session token is disabled")
	errOpenAIOAuthSessionTokenFormat   = errors.New("invalid openai oauth session token format")
	errOpenAIOAuthSessionTokenExpired  = errors.New("openai oauth session token expired")

	ErrOpenAIOAuthSessionNotFound = infraerrors.BadRequest(
		"OPENAI_OAUTH_SESSION_NOT_FOUND",
		"授权会话不存在或已过期，请重新生成授权链接后再完成授权",
	)
	ErrOpenAIOAuthStateRequired = infraerrors.BadRequest(
		"OPENAI_OAUTH_STATE_REQUIRED",
		"授权回调缺少 state 参数，请粘贴完整回调链接或重新生成授权链接",
	)
	ErrOpenAIOAuthInvalidState = infraerrors.BadRequest(
		"OPENAI_OAUTH_INVALID_STATE",
		"授权回调 state 不匹配，请使用当前弹窗生成的最新授权链接重新授权",
	)
	ErrOpenAIOAuthInvalidRequest = infraerrors.BadRequest(
		"OPENAI_OAUTH_REQUEST_INVALID",
		"授权请求无效，请重新生成授权链接后重试",
	)
)

type openAIOAuthSessionTokenPayload struct {
	Version      int    `json:"v"`
	SessionID    string `json:"sid"`
	State        string `json:"state"`
	CodeVerifier string `json:"code_verifier"`
	ClientID     string `json:"client_id,omitempty"`
	ProxyURL     string `json:"proxy_url,omitempty"`
	RedirectURI  string `json:"redirect_uri"`
	CreatedAt    int64  `json:"created_at"`
}

// NewOpenAIOAuthService creates a new OpenAI OAuth service
func NewOpenAIOAuthService(proxyRepo ProxyRepository, oauthClient OpenAIOAuthClient) *OpenAIOAuthService {
	return &OpenAIOAuthService{
		sessionStore: openai.NewSessionStore(),
		proxyRepo:    proxyRepo,
		oauthClient:  oauthClient,
	}
}

func (s *OpenAIOAuthService) SetSessionTokenSecret(secret string) {
	if s == nil {
		return
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		s.sessionTokenKey = nil
		return
	}
	sum := sha256.Sum256([]byte(secret))
	s.sessionTokenKey = sum[:]
}

// SetPrivacyClientFactory 注入 ImpersonateChrome 客户端工厂，
// 用于调用 chatgpt.com/backend-api 获取账号信息（plan_type 等）。
func (s *OpenAIOAuthService) SetPrivacyClientFactory(factory PrivacyClientFactory) {
	s.privacyClientFactory = factory
}

func (s *OpenAIOAuthService) PrivacyClientFactory() PrivacyClientFactory {
	if s == nil {
		return nil
	}
	return s.privacyClientFactory
}

func (s *OpenAIOAuthService) EnsureProxyVisibleToUser(ctx context.Context, userID int64, proxyID *int64) error {
	_, err := s.visibleProxyForUser(ctx, userID, proxyID)
	return err
}

func (s *OpenAIOAuthService) VisibleProxyURLForUser(ctx context.Context, userID int64, proxyID *int64) (string, error) {
	if proxyID == nil || *proxyID <= 0 {
		return "", nil
	}
	proxy, err := s.visibleProxyForUser(ctx, userID, proxyID)
	if err != nil {
		return "", err
	}
	return proxy.URL(), nil
}

func (s *OpenAIOAuthService) visibleProxyForUser(ctx context.Context, userID int64, proxyID *int64) (*Proxy, error) {
	if userID <= 0 {
		return nil, ErrUserNotFound
	}
	if proxyID == nil || *proxyID <= 0 {
		return nil, ErrAccountShareModeProxyRequired
	}
	if s == nil || s.proxyRepo == nil {
		return nil, ErrServiceUnavailable
	}
	proxy, err := s.proxyRepo.GetVisibleByID(ctx, userID, *proxyID)
	if err != nil {
		return nil, err
	}
	if proxy == nil || !proxy.IsActive() {
		return nil, ErrProxyNotFound
	}
	return proxy, nil
}

// OpenAIAuthURLResult contains the authorization URL and session info
type OpenAIAuthURLResult struct {
	AuthURL   string `json:"auth_url"`
	SessionID string `json:"session_id"`
}

// GenerateAuthURL generates an OpenAI OAuth authorization URL
func (s *OpenAIOAuthService) GenerateAuthURL(ctx context.Context, proxyID *int64, redirectURI, platform string) (*OpenAIAuthURLResult, error) {
	// Generate PKCE values
	state, err := openai.GenerateState()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_OAUTH_STATE_FAILED", "failed to generate state: %v", err)
	}

	codeVerifier, err := openai.GenerateCodeVerifier()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_OAUTH_VERIFIER_FAILED", "failed to generate code verifier: %v", err)
	}

	codeChallenge := openai.GenerateCodeChallenge(codeVerifier)

	// Generate session ID
	sessionID, err := openai.GenerateSessionID()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_OAUTH_SESSION_FAILED", "failed to generate session ID: %v", err)
	}

	// Get proxy URL if specified
	var proxyURL string
	if proxyID != nil {
		proxy, err := s.proxyRepo.GetByID(ctx, *proxyID)
		if err != nil {
			return nil, infraerrors.Newf(http.StatusBadRequest, "OPENAI_OAUTH_PROXY_NOT_FOUND", "proxy not found: %v", err)
		}
		if proxy != nil {
			proxyURL = proxy.URL()
		}
	}

	// Use default redirect URI if not specified
	if redirectURI == "" {
		redirectURI = openai.DefaultRedirectURI
	}
	normalizedPlatform := normalizeOpenAIOAuthPlatform(platform)
	clientID, _ := openai.OAuthClientConfigByPlatform(normalizedPlatform)

	// Store session
	session := &openai.OAuthSession{
		State:        state,
		CodeVerifier: codeVerifier,
		ClientID:     clientID,
		RedirectURI:  redirectURI,
		ProxyURL:     proxyURL,
		CreatedAt:    time.Now(),
	}
	if len(s.sessionTokenKey) > 0 {
		sessionID, err = s.encodeSessionToken(sessionID, session)
		if err != nil {
			return nil, infraerrors.Newf(http.StatusInternalServerError, "OPENAI_OAUTH_SESSION_TOKEN_FAILED", "failed to protect oauth session: %v", err)
		}
	}
	s.sessionStore.Set(sessionID, session)

	// Build authorization URL
	authURL := openai.BuildAuthorizationURLForPlatform(state, codeChallenge, redirectURI, normalizedPlatform)

	return &OpenAIAuthURLResult{
		AuthURL:   authURL,
		SessionID: sessionID,
	}, nil
}

// OpenAIExchangeCodeInput represents the input for code exchange
type OpenAIExchangeCodeInput struct {
	SessionID   string
	Code        string
	State       string
	RedirectURI string
	ProxyID     *int64
}

// OpenAITokenInfo represents the token information for OpenAI
type OpenAITokenInfo struct {
	AccessToken           string `json:"access_token"`
	RefreshToken          string `json:"refresh_token"`
	IDToken               string `json:"id_token,omitempty"`
	ExpiresIn             int64  `json:"expires_in"`
	ExpiresAt             int64  `json:"expires_at"`
	ClientID              string `json:"client_id,omitempty"`
	Email                 string `json:"email,omitempty"`
	ChatGPTAccountID      string `json:"chatgpt_account_id,omitempty"`
	ChatGPTUserID         string `json:"chatgpt_user_id,omitempty"`
	OrganizationID        string `json:"organization_id,omitempty"`
	PlanType              string `json:"plan_type,omitempty"`
	SubscriptionExpiresAt string `json:"subscription_expires_at,omitempty"`
	PrivacyMode           string `json:"privacy_mode,omitempty"`
}

// ExchangeCode exchanges authorization code for tokens
func (s *OpenAIOAuthService) ExchangeCode(ctx context.Context, input *OpenAIExchangeCodeInput) (*OpenAITokenInfo, error) {
	if input == nil || strings.TrimSpace(input.SessionID) == "" || strings.TrimSpace(input.Code) == "" {
		return nil, ErrOpenAIOAuthInvalidRequest
	}

	// Get session
	session, ok := s.sessionStore.Get(input.SessionID)
	if !ok {
		recovered, err := s.decodeSessionToken(input.SessionID)
		if err != nil {
			slog.Warn(
				"openai_oauth_session_not_found",
				"session_id_kind", classifyOpenAIOAuthSessionID(input.SessionID),
				"recover_error", err.Error(),
			)
			return nil, ErrOpenAIOAuthSessionNotFound
		}
		session = recovered
	}
	if input.State == "" {
		return nil, ErrOpenAIOAuthStateRequired
	}
	if subtle.ConstantTimeCompare([]byte(input.State), []byte(session.State)) != 1 {
		return nil, ErrOpenAIOAuthInvalidState
	}

	// Get proxy URL: prefer input.ProxyID, fallback to session.ProxyURL
	proxyURL := session.ProxyURL
	if input.ProxyID != nil {
		proxy, err := s.proxyRepo.GetByID(ctx, *input.ProxyID)
		if err != nil {
			return nil, infraerrors.Newf(http.StatusBadRequest, "OPENAI_OAUTH_PROXY_NOT_FOUND", "proxy not found: %v", err)
		}
		if proxy != nil {
			proxyURL = proxy.URL()
		}
	}

	// Use redirect URI from session or input
	redirectURI := session.RedirectURI
	if input.RedirectURI != "" {
		redirectURI = input.RedirectURI
	}
	clientID := strings.TrimSpace(session.ClientID)
	if clientID == "" {
		clientID = openai.ClientID
	}

	// Exchange code for token
	tokenResp, err := s.oauthClient.ExchangeCode(ctx, input.Code, session.CodeVerifier, redirectURI, proxyURL, clientID)
	if err != nil {
		return nil, err
	}

	// Parse ID token to get user info
	var userInfo *openai.UserInfo
	if tokenResp.IDToken != "" {
		claims, parseErr := openai.ParseIDToken(tokenResp.IDToken)
		if parseErr != nil {
			slog.Warn("openai_oauth_id_token_parse_failed", "error", parseErr)
		} else {
			userInfo = claims.GetUserInfo()
		}
	}

	// Delete session after successful exchange
	s.sessionStore.Delete(input.SessionID)

	tokenInfo := &OpenAITokenInfo{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		IDToken:      tokenResp.IDToken,
		ExpiresIn:    int64(tokenResp.ExpiresIn),
		ExpiresAt:    time.Now().Unix() + int64(tokenResp.ExpiresIn),
		ClientID:     clientID,
	}

	if userInfo != nil {
		tokenInfo.Email = userInfo.Email
		tokenInfo.ChatGPTAccountID = userInfo.ChatGPTAccountID
		tokenInfo.ChatGPTUserID = userInfo.ChatGPTUserID
		tokenInfo.OrganizationID = userInfo.OrganizationID
		tokenInfo.PlanType = userInfo.PlanType
	}

	s.enrichTokenInfo(ctx, tokenInfo, proxyURL)

	return tokenInfo, nil
}

func (s *OpenAIOAuthService) encodeSessionToken(sessionID string, session *openai.OAuthSession) (string, error) {
	if s == nil || len(s.sessionTokenKey) == 0 {
		return sessionID, nil
	}
	if session == nil {
		return "", fmt.Errorf("session is nil")
	}

	payload := openAIOAuthSessionTokenPayload{
		Version:      1,
		SessionID:    sessionID,
		State:        session.State,
		CodeVerifier: session.CodeVerifier,
		ClientID:     session.ClientID,
		ProxyURL:     session.ProxyURL,
		RedirectURI:  session.RedirectURI,
		CreatedAt:    session.CreatedAt.UTC().Unix(),
	}
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal oauth session token: %w", err)
	}

	gcm, err := s.sessionTokenGCM()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate oauth session token nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, []byte(openAIOAuthSessionTokenAAD))
	raw := append(nonce, ciphertext...)
	return openAIOAuthSessionTokenPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *OpenAIOAuthService) decodeSessionToken(sessionID string) (*openai.OAuthSession, error) {
	if s == nil || len(s.sessionTokenKey) == 0 {
		return nil, errOpenAIOAuthSessionTokenDisabled
	}
	if !strings.HasPrefix(sessionID, openAIOAuthSessionTokenPrefix) {
		return nil, errOpenAIOAuthSessionTokenFormat
	}
	encoded := strings.TrimPrefix(sessionID, openAIOAuthSessionTokenPrefix)
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errOpenAIOAuthSessionTokenFormat, err)
	}

	gcm, err := s.sessionTokenGCM()
	if err != nil {
		return nil, err
	}
	if len(raw) <= gcm.NonceSize() {
		return nil, errOpenAIOAuthSessionTokenFormat
	}
	nonce := raw[:gcm.NonceSize()]
	ciphertext := raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, []byte(openAIOAuthSessionTokenAAD))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errOpenAIOAuthSessionTokenFormat, err)
	}

	var payload openAIOAuthSessionTokenPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return nil, fmt.Errorf("%w: %v", errOpenAIOAuthSessionTokenFormat, err)
	}
	if payload.Version != 1 ||
		strings.TrimSpace(payload.State) == "" ||
		strings.TrimSpace(payload.CodeVerifier) == "" ||
		strings.TrimSpace(payload.RedirectURI) == "" ||
		payload.CreatedAt <= 0 {
		return nil, errOpenAIOAuthSessionTokenFormat
	}

	createdAt := time.Unix(payload.CreatedAt, 0).UTC()
	now := time.Now().UTC()
	if now.Sub(createdAt) > openai.SessionTTL || createdAt.Sub(now) > openAIOAuthSessionTokenSkew {
		return nil, errOpenAIOAuthSessionTokenExpired
	}

	return &openai.OAuthSession{
		State:        payload.State,
		CodeVerifier: payload.CodeVerifier,
		ClientID:     payload.ClientID,
		ProxyURL:     payload.ProxyURL,
		RedirectURI:  payload.RedirectURI,
		CreatedAt:    createdAt,
	}, nil
}

func (s *OpenAIOAuthService) sessionTokenGCM() (cipher.AEAD, error) {
	if s == nil || len(s.sessionTokenKey) == 0 {
		return nil, errOpenAIOAuthSessionTokenDisabled
	}
	block, err := aes.NewCipher(s.sessionTokenKey)
	if err != nil {
		return nil, fmt.Errorf("create oauth session token cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create oauth session token gcm: %w", err)
	}
	return gcm, nil
}

func classifyOpenAIOAuthSessionID(sessionID string) string {
	if strings.HasPrefix(sessionID, openAIOAuthSessionTokenPrefix) {
		return "stateless_token"
	}
	if sessionID == "" {
		return "empty"
	}
	return "legacy_memory_id"
}

// RefreshToken refreshes an OpenAI OAuth token
func (s *OpenAIOAuthService) RefreshToken(ctx context.Context, refreshToken string, proxyURL string) (*OpenAITokenInfo, error) {
	return s.RefreshTokenWithClientID(ctx, refreshToken, proxyURL, "")
}

// RefreshTokenWithClientID refreshes an OpenAI OAuth token with optional client_id.
func (s *OpenAIOAuthService) RefreshTokenWithClientID(ctx context.Context, refreshToken string, proxyURL string, clientID string) (*OpenAITokenInfo, error) {
	tokenResp, err := s.oauthClient.RefreshTokenWithClientID(ctx, refreshToken, proxyURL, clientID)
	if err != nil {
		return nil, err
	}

	// Parse ID token to get user info
	var userInfo *openai.UserInfo
	if tokenResp.IDToken != "" {
		claims, parseErr := openai.ParseIDToken(tokenResp.IDToken)
		if parseErr != nil {
			slog.Warn("openai_oauth_id_token_parse_failed", "error", parseErr)
		} else {
			userInfo = claims.GetUserInfo()
		}
	}

	tokenInfo := &OpenAITokenInfo{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		IDToken:      tokenResp.IDToken,
		ExpiresIn:    int64(tokenResp.ExpiresIn),
		ExpiresAt:    time.Now().Unix() + int64(tokenResp.ExpiresIn),
	}
	if trimmed := strings.TrimSpace(clientID); trimmed != "" {
		tokenInfo.ClientID = trimmed
	}

	if userInfo != nil {
		tokenInfo.Email = userInfo.Email
		tokenInfo.ChatGPTAccountID = userInfo.ChatGPTAccountID
		tokenInfo.ChatGPTUserID = userInfo.ChatGPTUserID
		tokenInfo.OrganizationID = userInfo.OrganizationID
		tokenInfo.PlanType = userInfo.PlanType
	}

	s.enrichTokenInfo(ctx, tokenInfo, proxyURL)

	return tokenInfo, nil
}

// enrichTokenInfo 通过 ChatGPT backend-api 补全 tokenInfo 并设置隐私（best-effort）。
// 从 accounts/check 获取最新 plan_type、subscription_expires_at、email，
// 然后尝试关闭训练数据共享。适用于所有获取/刷新 token 的路径。
func (s *OpenAIOAuthService) enrichTokenInfo(ctx context.Context, tokenInfo *OpenAITokenInfo, proxyURL string) {
	if tokenInfo.AccessToken == "" || s.privacyClientFactory == nil {
		return
	}

	// 从 access_token JWT 中提取 orgID（poid），用于匹配正确的账号
	orgID := tokenInfo.OrganizationID
	if orgID == "" {
		if atClaims, err := openai.DecodeIDToken(tokenInfo.AccessToken); err == nil && atClaims.OpenAIAuth != nil {
			orgID = atClaims.OpenAIAuth.POID
		}
	}
	if info := fetchChatGPTAccountInfo(ctx, s.privacyClientFactory, tokenInfo.AccessToken, proxyURL, orgID); info != nil {
		if info.PlanType != "" {
			tokenInfo.PlanType = info.PlanType
		}
		if info.SubscriptionExpiresAt != "" {
			tokenInfo.SubscriptionExpiresAt = info.SubscriptionExpiresAt
		}
		if tokenInfo.Email == "" && info.Email != "" {
			tokenInfo.Email = info.Email
		}
	}

	// 尝试设置隐私（关闭训练数据共享），best-effort
	tokenInfo.PrivacyMode = disableOpenAITraining(ctx, s.privacyClientFactory, tokenInfo.AccessToken, proxyURL)
}

// RefreshAccountToken refreshes token for an OpenAI OAuth account
func (s *OpenAIOAuthService) RefreshAccountToken(ctx context.Context, account *Account) (*OpenAITokenInfo, error) {
	if account.Platform != PlatformOpenAI {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_OAUTH_INVALID_ACCOUNT", "account is not an OpenAI account")
	}
	if account.Type != AccountTypeOAuth {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_OAUTH_INVALID_ACCOUNT_TYPE", "account is not an OAuth account")
	}

	refreshToken := account.GetCredential("refresh_token")
	if refreshToken == "" {
		accessToken := account.GetCredential("access_token")
		if accessToken != "" {
			tokenInfo := &OpenAITokenInfo{
				AccessToken:      accessToken,
				RefreshToken:     "",
				IDToken:          account.GetCredential("id_token"),
				ClientID:         account.GetCredential("client_id"),
				Email:            account.GetCredential("email"),
				ChatGPTAccountID: account.GetCredential("chatgpt_account_id"),
				ChatGPTUserID:    account.GetCredential("chatgpt_user_id"),
				OrganizationID:   account.GetCredential("organization_id"),
				PlanType:         account.GetCredential("plan_type"),
			}
			if expiresAt := account.GetCredentialAsTime("expires_at"); expiresAt != nil {
				tokenInfo.ExpiresAt = expiresAt.Unix()
				tokenInfo.ExpiresIn = int64(time.Until(*expiresAt).Seconds())
			}
			return tokenInfo, nil
		}
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_OAUTH_NO_REFRESH_TOKEN", "no refresh token available")
	}

	var proxyURL string
	if account.ProxyID != nil {
		proxy, err := s.proxyRepo.GetByID(ctx, *account.ProxyID)
		if err == nil && proxy != nil {
			proxyURL = proxy.URL()
		}
	}

	clientID := account.GetCredential("client_id")
	return s.RefreshTokenWithClientID(ctx, refreshToken, proxyURL, clientID)
}

// BuildAccountCredentials builds credentials map from token info
func (s *OpenAIOAuthService) BuildAccountCredentials(tokenInfo *OpenAITokenInfo) map[string]any {
	expiresAt := time.Unix(tokenInfo.ExpiresAt, 0).Format(time.RFC3339)

	creds := map[string]any{
		"access_token": tokenInfo.AccessToken,
		"expires_at":   expiresAt,
	}
	// 仅在刷新响应返回了新的 refresh_token 时才更新，防止用空值覆盖已有令牌
	if strings.TrimSpace(tokenInfo.RefreshToken) != "" {
		creds["refresh_token"] = tokenInfo.RefreshToken
	}

	if tokenInfo.IDToken != "" {
		creds["id_token"] = tokenInfo.IDToken
	}
	if tokenInfo.Email != "" {
		creds["email"] = tokenInfo.Email
	}
	if tokenInfo.ChatGPTAccountID != "" {
		creds["chatgpt_account_id"] = tokenInfo.ChatGPTAccountID
	}
	if tokenInfo.ChatGPTUserID != "" {
		creds["chatgpt_user_id"] = tokenInfo.ChatGPTUserID
	}
	if tokenInfo.OrganizationID != "" {
		creds["organization_id"] = tokenInfo.OrganizationID
	}
	if tokenInfo.PlanType != "" {
		creds["plan_type"] = tokenInfo.PlanType
	}
	if tokenInfo.SubscriptionExpiresAt != "" {
		creds["subscription_expires_at"] = tokenInfo.SubscriptionExpiresAt
	}
	if strings.TrimSpace(tokenInfo.ClientID) != "" {
		creds["client_id"] = strings.TrimSpace(tokenInfo.ClientID)
	}

	return creds
}

// Stop stops the session store cleanup goroutine
func (s *OpenAIOAuthService) Stop() {
	s.sessionStore.Stop()
}

func normalizeOpenAIOAuthPlatform(platform string) string {
	return openai.OAuthPlatformOpenAI
}
