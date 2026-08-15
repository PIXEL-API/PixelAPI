package service

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

const grokDefaultAccessTokenTTL = 6 * time.Hour

const grokOAuthSessionKeyPrefix = "oauth:session:grok:"

var ErrGrokPasswordAuthDisabled = infraerrors.New(
	http.StatusForbidden,
	"GROK_OAUTH_PASSWORD_AUTH_DISABLED",
	"Grok password authorization is disabled",
)

type grokPasswordLoginClient interface {
	LoginWithPassword(ctx context.Context, email, password, proxyURL string) (*xai.GrokPasswordLoginResult, error)
}

type GrokOAuthService struct {
	sessionStore        *xai.SessionStore
	ephemeralStateStore EphemeralStateStore
	proxyRepo           ProxyRepository
	oauthClient         GrokOAuthClient
	passwordAuthEnabled bool
}

// SetPasswordAuthEnabled applies the explicit startup configuration gate.
// The zero value remains disabled, including focused tests and manual wiring.
func (s *GrokOAuthService) SetPasswordAuthEnabled(enabled bool) {
	if s != nil {
		s.passwordAuthEnabled = enabled
	}
}

type GrokOAuthCapabilities struct {
	PasswordAuthEnabled bool `json:"password_auth_enabled"`
}

func (s *GrokOAuthService) GetCapabilities() GrokOAuthCapabilities {
	return GrokOAuthCapabilities{PasswordAuthEnabled: s != nil && s.passwordAuthEnabled}
}

func NewGrokOAuthService(proxyRepo ProxyRepository, oauthClient GrokOAuthClient, stateStores ...EphemeralStateStore) *GrokOAuthService {
	var stateStore EphemeralStateStore
	if len(stateStores) > 0 {
		stateStore = stateStores[0]
	}
	return &GrokOAuthService{
		sessionStore:        xai.NewSessionStore(),
		ephemeralStateStore: stateStore,
		proxyRepo:           proxyRepo,
		oauthClient:         oauthClient,
	}
}

func (s *GrokOAuthService) storeOAuthSession(ctx context.Context, sessionID string, session *xai.OAuthSession) error {
	if s == nil || session == nil || strings.TrimSpace(sessionID) == "" {
		return infraerrors.New(http.StatusInternalServerError, "GROK_OAUTH_SESSION_INVALID", "oauth session is invalid")
	}
	if s.ephemeralStateStore == nil {
		s.sessionStore.Set(sessionID, session)
		return nil
	}
	encoded, err := json.Marshal(session)
	if err != nil {
		return infraerrors.New(http.StatusInternalServerError, "GROK_OAUTH_SESSION_ENCODE_FAILED", "failed to encode oauth session")
	}
	if err := s.ephemeralStateStore.Set(ctx, grokOAuthSessionKeyPrefix+sessionID, encoded, xai.SessionTTL); err != nil {
		return infraerrors.New(http.StatusServiceUnavailable, "GROK_OAUTH_SESSION_STORE_UNAVAILABLE", "oauth session store is unavailable")
	}
	return nil
}

func (s *GrokOAuthService) getOAuthSession(ctx context.Context, sessionID string) (*xai.OAuthSession, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_SESSION_NOT_FOUND", "session not found or expired")
	}
	if s == nil || s.ephemeralStateStore == nil {
		if s == nil || s.sessionStore == nil {
			return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_SESSION_NOT_FOUND", "session not found or expired")
		}
		session, ok := s.sessionStore.Get(sessionID)
		if !ok {
			return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_SESSION_NOT_FOUND", "session not found or expired")
		}
		return session, nil
	}
	encoded, found, err := s.ephemeralStateStore.Get(ctx, grokOAuthSessionKeyPrefix+sessionID)
	if err != nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "GROK_OAUTH_SESSION_STORE_UNAVAILABLE", "oauth session store is unavailable")
	}
	if !found {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_SESSION_NOT_FOUND", "session not found or expired")
	}
	return decodeGrokOAuthSession(encoded)
}

func (s *GrokOAuthService) consumeOAuthSession(ctx context.Context, sessionID string) (*xai.OAuthSession, error) {
	if s == nil || s.ephemeralStateStore == nil {
		if s == nil || s.sessionStore == nil {
			return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_SESSION_ALREADY_USED", "oauth session is expired or already used")
		}
		session, ok := s.sessionStore.Take(sessionID)
		if !ok {
			return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_SESSION_ALREADY_USED", "oauth session is expired or already used")
		}
		return session, nil
	}
	encoded, found, err := s.ephemeralStateStore.Take(ctx, grokOAuthSessionKeyPrefix+strings.TrimSpace(sessionID))
	if err != nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "GROK_OAUTH_SESSION_STORE_UNAVAILABLE", "oauth session store is unavailable")
	}
	if !found {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_SESSION_ALREADY_USED", "oauth session is expired or already used")
	}
	return decodeGrokOAuthSession(encoded)
}

func decodeGrokOAuthSession(encoded []byte) (*xai.OAuthSession, error) {
	var session xai.OAuthSession
	if err := json.Unmarshal(encoded, &session); err != nil || strings.TrimSpace(session.State) == "" || strings.TrimSpace(session.CodeVerifier) == "" {
		return nil, infraerrors.New(http.StatusInternalServerError, "GROK_OAUTH_SESSION_INVALID", "stored oauth session is invalid")
	}
	if time.Since(session.CreatedAt) > xai.SessionTTL {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_SESSION_NOT_FOUND", "session not found or expired")
	}
	return &session, nil
}

func sameOAuthSession(left, right *xai.OAuthSession) bool {
	if left == nil || right == nil {
		return false
	}
	return constantTimeStringEqual(left.State, right.State) &&
		constantTimeStringEqual(left.CodeVerifier, right.CodeVerifier) &&
		constantTimeStringEqual(left.ClientID, right.ClientID) &&
		constantTimeStringEqual(left.ProxyURL, right.ProxyURL) &&
		constantTimeStringEqual(left.RedirectURI, right.RedirectURI)
}

func constantTimeStringEqual(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

type GrokAuthURLResult struct {
	AuthURL   string `json:"auth_url"`
	SessionID string `json:"session_id"`
	State     string `json:"state"`
}

func (s *GrokOAuthService) GenerateAuthURL(ctx context.Context, proxyID *int64, redirectURI string) (*GrokAuthURLResult, error) {
	if err := s.requireOAuthClient(); err != nil {
		return nil, err
	}
	state, err := xai.GenerateState()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "GROK_OAUTH_STATE_FAILED", "failed to generate state: %v", err)
	}
	nonce, err := xai.GenerateNonce()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "GROK_OAUTH_NONCE_FAILED", "failed to generate nonce: %v", err)
	}
	codeVerifier, err := xai.GenerateCodeVerifier()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "GROK_OAUTH_VERIFIER_FAILED", "failed to generate code verifier: %v", err)
	}
	sessionID, err := xai.GenerateSessionID()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "GROK_OAUTH_SESSION_FAILED", "failed to generate session ID: %v", err)
	}

	proxyURL, err := s.proxyURL(ctx, proxyID)
	if err != nil {
		return nil, err
	}
	redirectURI = xai.EffectiveRedirectURI(redirectURI)
	codeChallenge := xai.GenerateCodeChallenge(codeVerifier)

	authURL, err := xai.BuildAuthorizationURL(state, codeChallenge, redirectURI, nonce)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadRequest, "GROK_OAUTH_INVALID_AUTHORIZE_URL", "%v", err)
	}

	session := &xai.OAuthSession{
		State:         state,
		CodeVerifier:  codeVerifier,
		CodeChallenge: codeChallenge,
		ClientID:      xai.EffectiveClientID(),
		Scope:         xai.EffectiveScope(),
		ProxyURL:      proxyURL,
		RedirectURI:   redirectURI,
		CreatedAt:     time.Now(),
	}
	if err := s.storeOAuthSession(ctx, sessionID, session); err != nil {
		return nil, err
	}

	return &GrokAuthURLResult{
		AuthURL:   authURL,
		SessionID: sessionID,
		State:     state,
	}, nil
}

type GrokExchangeCodeInput struct {
	SessionID   string
	Code        string
	State       string
	RedirectURI string
	ProxyID     *int64
}

type GrokTokenInfo struct {
	AccessToken       string `json:"access_token"`
	RefreshToken      string `json:"refresh_token,omitempty"`
	IDToken           string `json:"id_token,omitempty"`
	TokenType         string `json:"token_type,omitempty"`
	ExpiresIn         int64  `json:"expires_in"`
	ExpiresAt         int64  `json:"expires_at"`
	ClientID          string `json:"client_id,omitempty"`
	Scope             string `json:"scope,omitempty"`
	Email             string `json:"email,omitempty"`
	Subject           string `json:"sub,omitempty"`
	TeamID            string `json:"team_id,omitempty"`
	SubscriptionTier  string `json:"subscription_tier,omitempty"`
	EntitlementStatus string `json:"entitlement_status,omitempty"`
}

func (s *GrokOAuthService) ExchangeCode(ctx context.Context, input *GrokExchangeCodeInput) (*GrokTokenInfo, error) {
	if input == nil {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_INVALID_INPUT", "input is required")
	}
	session, err := s.getOAuthSession(ctx, input.SessionID)
	if err != nil {
		return nil, err
	}

	parsed := xai.ParseAuthorizationInput(input.Code)
	code := strings.TrimSpace(parsed.Code)
	if code == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_CODE_REQUIRED", "authorization code is required")
	}
	state := strings.TrimSpace(input.State)
	if state == "" {
		state = strings.TrimSpace(parsed.State)
	}
	if state == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_STATE_REQUIRED", "oauth state is required")
	}
	if state != "" && subtle.ConstantTimeCompare([]byte(state), []byte(session.State)) != 1 {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_INVALID_STATE", "invalid oauth state")
	}

	if redirectURI := strings.TrimSpace(input.RedirectURI); redirectURI != "" &&
		redirectURI != strings.TrimSpace(session.RedirectURI) {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_REDIRECT_URI_MISMATCH", "redirect_uri does not match the OAuth session")
	}

	proxyURL := session.ProxyURL
	if input.ProxyID != nil {
		requestedProxyURL, err := s.proxyURL(ctx, input.ProxyID)
		if err != nil {
			return nil, err
		}
		if !constantTimeStringEqual(requestedProxyURL, session.ProxyURL) {
			return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_PROXY_MISMATCH", "proxy does not match the OAuth session")
		}
	}

	if err := s.requireOAuthClient(); err != nil {
		return nil, err
	}
	consumedSession, err := s.consumeOAuthSession(ctx, input.SessionID)
	if err != nil {
		return nil, err
	}
	if !sameOAuthSession(session, consumedSession) {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_SESSION_CHANGED", "oauth session changed before it could be consumed")
	}

	tokenResp, err := s.oauthClient.ExchangeCode(ctx, code, consumedSession.CodeVerifier, consumedSession.RedirectURI, proxyURL, consumedSession.ClientID)
	if err != nil {
		return nil, err
	}
	if err := validateGrokOAuthTokenResponse(tokenResp); err != nil {
		return nil, err
	}
	return s.tokenInfoFromResponse(tokenResp, consumedSession.ClientID, nil), nil
}

func (s *GrokOAuthService) requireOAuthClient() error {
	if s == nil || s.oauthClient == nil {
		return infraerrors.New(http.StatusInternalServerError, "GROK_OAUTH_CLIENT_NOT_CONFIGURED", "oauth client is not configured")
	}
	return nil
}

func (s *GrokOAuthService) RefreshToken(ctx context.Context, refreshToken, proxyURL, clientID string) (*GrokTokenInfo, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_NO_REFRESH_TOKEN", "refresh_token is required")
	}
	if err := s.requireOAuthClient(); err != nil {
		return nil, err
	}
	tokenResp, err := s.oauthClient.RefreshToken(ctx, refreshToken, proxyURL, clientID)
	if err != nil {
		return nil, err
	}
	if err := validateGrokOAuthTokenResponse(tokenResp); err != nil {
		return nil, err
	}
	tokenInfo := s.tokenInfoFromResponse(tokenResp, clientID, nil)
	if tokenInfo.RefreshToken == "" {
		tokenInfo.RefreshToken = refreshToken
	}
	return tokenInfo, nil
}

func (s *GrokOAuthService) ValidateRefreshToken(ctx context.Context, refreshToken string, proxyID *int64) (*GrokTokenInfo, error) {
	proxyURL, err := s.proxyURL(ctx, proxyID)
	if err != nil {
		return nil, err
	}
	return s.RefreshToken(ctx, refreshToken, proxyURL, xai.EffectiveClientID())
}

// ValidateSSOToken converts a Web SSO cookie into Build OAuth tokens. The raw
// SSO token is scoped to this call and is never copied into GrokTokenInfo.
func (s *GrokOAuthService) ValidateSSOToken(ctx context.Context, ssoToken string, proxyID *int64) (*GrokTokenInfo, error) {
	ssoToken = xai.NormalizeSSOToken(ssoToken)
	if ssoToken == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_NO_SSO_TOKEN", "sso_token is required")
	}
	if err := s.requireOAuthClient(); err != nil {
		return nil, err
	}
	proxyURL, err := s.proxyURL(ctx, proxyID)
	if err != nil {
		return nil, err
	}
	tokenResp, err := s.oauthClient.ConvertSSOToBuild(ctx, ssoToken, proxyURL)
	if err != nil {
		return nil, err
	}
	if err := validateGrokOAuthTokenResponse(tokenResp); err != nil {
		return nil, err
	}
	return s.tokenInfoFromResponse(tokenResp, xai.DefaultClientID, nil), nil
}

// ConvertFromSSO is the batch-import entry point and intentionally shares the
// same validation and secret-lifetime rules as one-shot SSO reauthorization.
func (s *GrokOAuthService) ConvertFromSSO(ctx context.Context, ssoToken string, proxyID *int64) (*GrokTokenInfo, error) {
	return s.ValidateSSOToken(ctx, ssoToken, proxyID)
}

// AuthorizePassword performs password -> ephemeral SSO -> Build OAuth. Only
// the final OAuth token response leaves this method; password and SSO are not
// copied into credentials, Extra, logs, or error messages.
func (s *GrokOAuthService) AuthorizePassword(ctx context.Context, email, password string, proxyID *int64) (*GrokTokenInfo, error) {
	if s == nil || !s.passwordAuthEnabled {
		return nil, ErrGrokPasswordAuthDisabled
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_EMAIL_REQUIRED", "email is required")
	}
	if password == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_PASSWORD_REQUIRED", "password is required")
	}
	if err := s.requireOAuthClient(); err != nil {
		return nil, err
	}
	passwordClient, ok := s.oauthClient.(grokPasswordLoginClient)
	if !ok {
		return nil, infraerrors.New(
			http.StatusServiceUnavailable,
			"GROK_OAUTH_PASSWORD_CLIENT_UNAVAILABLE",
			"Grok password authorization client is unavailable",
		)
	}
	proxyURL, err := s.proxyURL(ctx, proxyID)
	if err != nil {
		return nil, err
	}
	loginResult, err := passwordClient.LoginWithPassword(ctx, email, password, proxyURL)
	if err != nil {
		return nil, err
	}
	if loginResult == nil || strings.TrimSpace(loginResult.SSOToken) == "" {
		return nil, infraerrors.New(
			http.StatusBadGateway,
			"GROK_OAUTH_PASSWORD_LOGIN_FAILED",
			"Grok password login did not return an SSO token",
		)
	}

	tokenInfo, err := s.ValidateSSOToken(ctx, loginResult.SSOToken, proxyID)
	if err != nil {
		return nil, err
	}
	if tokenInfo.Email == "" {
		tokenInfo.Email = strings.TrimSpace(loginResult.Email)
	}
	return tokenInfo, nil
}

func (s *GrokOAuthService) RefreshAccountToken(ctx context.Context, account *Account) (*GrokTokenInfo, error) {
	if account == nil || account.Platform != PlatformGrok {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_INVALID_ACCOUNT", "account is not a Grok account")
	}
	if account.Type != AccountTypeOAuth {
		return nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_INVALID_ACCOUNT_TYPE", "account is not an OAuth account")
	}

	proxyURL, observedProxy, err := s.proxyURLWithSnapshot(ctx, account.ProxyID)
	if err != nil {
		return nil, withGrokCredentialFailureMutationSnapshot(
			err,
			grokCredentialMutationSnapshotWithObservedProxy(account, observedProxy),
		)
	}
	refreshToken := account.GetCredential("refresh_token")
	if strings.TrimSpace(refreshToken) == "" {
		return nil, withGrokCredentialFailureMutationSnapshot(
			infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_NO_REFRESH_TOKEN", "no refresh token available"),
			grokCredentialMutationSnapshotWithObservedProxy(account, observedProxy),
		)
	}

	clientID := account.GetCredential("client_id")
	tokenInfo, err := s.RefreshToken(ctx, refreshToken, proxyURL, clientID)
	if err != nil {
		return nil, withGrokCredentialFailureMutationSnapshot(
			err,
			grokCredentialMutationSnapshotWithObservedProxy(account, observedProxy),
		)
	}
	if strings.TrimSpace(tokenInfo.SubscriptionTier) == "" {
		tokenInfo.SubscriptionTier = account.GetCredential("subscription_tier")
	}
	if strings.TrimSpace(tokenInfo.EntitlementStatus) == "" {
		tokenInfo.EntitlementStatus = account.GetCredential("entitlement_status")
	}
	return tokenInfo, nil
}

func (s *GrokOAuthService) BuildAccountCredentials(tokenInfo *GrokTokenInfo) map[string]any {
	if tokenInfo == nil {
		return nil
	}
	expiresAt := time.Unix(tokenInfo.ExpiresAt, 0).UTC().Format(time.RFC3339)
	creds := map[string]any{
		"access_token": tokenInfo.AccessToken,
		"expires_at":   expiresAt,
	}
	if tokenInfo.RefreshToken != "" {
		creds["refresh_token"] = tokenInfo.RefreshToken
	}
	if tokenInfo.TokenType != "" {
		creds["token_type"] = tokenInfo.TokenType
	}
	if tokenInfo.IDToken != "" {
		creds["id_token"] = tokenInfo.IDToken
	}
	if tokenInfo.ClientID != "" {
		creds["client_id"] = tokenInfo.ClientID
	}
	if tokenInfo.Scope != "" {
		creds["scope"] = tokenInfo.Scope
	}
	if tokenInfo.Email != "" {
		creds["email"] = tokenInfo.Email
	}
	if tokenInfo.Subject != "" {
		creds["sub"] = tokenInfo.Subject
	}
	if tokenInfo.TeamID != "" {
		creds["team_id"] = tokenInfo.TeamID
	}
	if tokenInfo.SubscriptionTier != "" {
		creds["subscription_tier"] = tokenInfo.SubscriptionTier
	}
	if tokenInfo.EntitlementStatus != "" {
		creds["entitlement_status"] = tokenInfo.EntitlementStatus
	}
	// 这里刻意不写入 base_url。出站地址由 Account.GetGrokBaseURL() 在请求时解析，
	// 凭据缺失时它本来就回退到 xai.DefaultCLIBaseURL，把常量固化进 credentials 没有
	// 任何收益，却会让自有账号的凭证安全扫描把"系统写的默认值"当成用户配置的自定义
	// 上游而拒绝掉——令牌刷新会走同一个构造器，于是账号在首次刷新后被永久锁死。
	// 管理员侧需要在编辑器里看到并覆盖出站地址，默认值由 admin 侧显式补齐。
	return creds
}

func (s *GrokOAuthService) Stop() {
	s.sessionStore.Stop()
}

func (s *GrokOAuthService) tokenInfoFromResponse(tokenResp *xai.TokenResponse, clientID string, existing map[string]any) *GrokTokenInfo {
	now := time.Now()
	expiresIn := tokenResp.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = int64(grokDefaultAccessTokenTTL.Seconds())
	}
	info := &GrokTokenInfo{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		IDToken:      tokenResp.IDToken,
		TokenType:    tokenResp.TokenType,
		ExpiresIn:    expiresIn,
		ExpiresAt:    now.Add(time.Duration(expiresIn) * time.Second).Unix(),
		ClientID:     strings.TrimSpace(clientID),
		Scope:        tokenResp.Scope,
	}
	if info.ClientID == "" {
		info.ClientID = xai.EffectiveClientID()
	}
	if info.TokenType == "" {
		info.TokenType = "Bearer"
	}
	applyGrokTokenClaims(info, tokenResp.IDToken, false)
	applyGrokTokenClaims(info, tokenResp.AccessToken, true)
	if info.Email == "" && existing != nil {
		if email, _ := existing["email"].(string); email != "" {
			info.Email = email
		}
	}
	return info
}

func applyGrokTokenClaims(info *GrokTokenInfo, token string, includeTier bool) {
	if info == nil || strings.TrimSpace(token) == "" {
		return
	}
	claims := xai.DecodeJWTClaims(token)
	if claims == nil {
		return
	}
	if info.Email == "" {
		info.Email = xai.JWTClaimString(claims, "email")
	}
	if info.Subject == "" {
		info.Subject = xai.JWTClaimString(claims, "sub")
	}
	if info.TeamID == "" {
		info.TeamID = xai.JWTClaimString(claims, "team_id")
	}
	if includeTier {
		if tier := xai.SubscriptionTierFromJWT(token); tier != "" {
			info.SubscriptionTier = tier
		}
	}
}

func validateGrokOAuthTokenResponse(tokenResp *xai.TokenResponse) error {
	if tokenResp == nil || strings.TrimSpace(tokenResp.AccessToken) == "" {
		return infraerrors.New(http.StatusBadGateway, "GROK_OAUTH_TOKEN_RESPONSE_INVALID", "xAI OAuth token response did not include access_token")
	}
	return nil
}

func (s *GrokOAuthService) proxyURL(ctx context.Context, proxyID *int64) (string, error) {
	proxyURL, _, err := s.proxyURLWithSnapshot(ctx, proxyID)
	return proxyURL, err
}

func (s *GrokOAuthService) proxyURLWithSnapshot(ctx context.Context, proxyID *int64) (string, *Proxy, error) {
	if proxyID == nil {
		return "", nil, nil
	}
	if s.proxyRepo == nil {
		return "", nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_PROXY_NOT_AVAILABLE", "proxy repository is not available")
	}
	proxy, err := s.proxyRepo.GetByID(ctx, *proxyID)
	if err != nil {
		if errors.Is(err, ErrProxyNotFound) {
			return "", nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_PROXY_NOT_FOUND", "configured proxy was not found")
		}
		return "", nil, infraerrors.New(http.StatusServiceUnavailable, "GROK_OAUTH_PROXY_LOOKUP_FAILED", "proxy lookup is temporarily unavailable")
	}
	if proxy == nil {
		return "", nil, infraerrors.New(http.StatusBadRequest, "GROK_OAUTH_PROXY_NOT_FOUND", "configured proxy was not found")
	}
	recordGrokProxyVersionObservation(ctx, proxy)
	return proxy.URL(), proxy, nil
}
