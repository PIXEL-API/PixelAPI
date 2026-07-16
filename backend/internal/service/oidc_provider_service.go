package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

const (
	oidcProviderRedisPrefix = "sub2api:oidc:v1:"
	maxOIDCParameterLength  = 2048
)

var oidcAllowedScopes = map[string]struct{}{
	"openid":  {},
	"profile": {},
	"email":   {},
}

// OIDCProviderError is an OAuth 2.0 / OpenID Connect protocol error.
type OIDCProviderError struct {
	ErrorCode   string
	Description string
	HTTPStatus  int
	RedirectURI string
	State       string
}

func (e *OIDCProviderError) Error() string {
	if e == nil {
		return ""
	}
	return e.ErrorCode + ": " + e.Description
}

func newOIDCProviderError(code, description string, status int) *OIDCProviderError {
	return &OIDCProviderError{ErrorCode: code, Description: description, HTTPStatus: status}
}

type OIDCProviderAuthorizeInput struct {
	ClientID            string
	RedirectURI         string
	ResponseType        string
	Scope               string
	State               string
	Nonce               string
	CodeChallenge       string
	CodeChallengeMethod string
}

type OIDCProviderAuthorizationRequest struct {
	ClientID      string `json:"client_id"`
	RedirectURI   string `json:"redirect_uri"`
	Scope         string `json:"scope"`
	State         string `json:"state"`
	Nonce         string `json:"nonce"`
	CodeChallenge string `json:"code_challenge"`
}

type OIDCProviderAuthorizationCode struct {
	ClientID      string `json:"client_id"`
	RedirectURI   string `json:"redirect_uri"`
	Scope         string `json:"scope"`
	Nonce         string `json:"nonce"`
	CodeChallenge string `json:"code_challenge"`
	UserID        int64  `json:"user_id"`
	TokenVersion  int64  `json:"token_version"`
	AuthTime      int64  `json:"auth_time"`
}

type OIDCProviderAccessTokenState struct {
	ClientID     string `json:"client_id"`
	Scope        string `json:"scope"`
	UserID       int64  `json:"user_id"`
	TokenVersion int64  `json:"token_version"`
	AuthTime     int64  `json:"auth_time"`
}

type OIDCProviderTokenInput struct {
	ClientID     string
	Code         string
	RedirectURI  string
	CodeVerifier string
}

type OIDCProviderTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	IDToken     string `json:"id_token"`
	Scope       string `json:"scope"`
}

type OIDCProviderUserInfo struct {
	Subject           string `json:"sub"`
	Email             string `json:"email,omitempty"`
	EmailVerified     *bool  `json:"email_verified,omitempty"`
	PreferredUsername string `json:"preferred_username,omitempty"`
	Name              string `json:"name,omitempty"`
	Picture           string `json:"picture,omitempty"`
}

type oidcProviderClient struct {
	secretSHA256 [sha256.Size]byte
	redirectURIs map[string]struct{}
}

type oidcProviderIDTokenClaims struct {
	Email             string `json:"email,omitempty"`
	EmailVerified     *bool  `json:"email_verified,omitempty"`
	PreferredUsername string `json:"preferred_username,omitempty"`
	Name              string `json:"name,omitempty"`
	Picture           string `json:"picture,omitempty"`
	Nonce             string `json:"nonce"`
	AuthTime          int64  `json:"auth_time"`
	AccessTokenHash   string `json:"at_hash"`
	jwt.RegisteredClaims
}

// OIDCProviderService owns the cryptographic and transient-state parts of the
// provider. No authorization request, code, or access token is persisted in SQL.
type OIDCProviderService struct {
	enabled     bool
	issuer      string
	frontendURL string
	requestTTL  time.Duration
	codeTTL     time.Duration
	accessTTL   time.Duration
	redis       *redis.Client
	userService *UserService
	signingKey  *rsa.PrivateKey
	kid         string
	clients     map[string]oidcProviderClient
}

func NewOIDCProviderService(cfg *config.Config, redisClient *redis.Client, userService *UserService) (*OIDCProviderService, error) {
	if cfg == nil {
		return nil, errors.New("oidc provider: config is nil")
	}
	provider := cfg.OIDCProvider
	service := &OIDCProviderService{enabled: provider.Enabled}
	if !provider.Enabled {
		return service, nil
	}
	if redisClient == nil {
		return nil, errors.New("oidc provider: redis client is required")
	}
	if userService == nil {
		return nil, errors.New("oidc provider: user service is required")
	}

	privateKey, kid, err := loadOIDCProviderSigningKey(provider.SigningKeyPath)
	if err != nil {
		return nil, err
	}
	clients := make(map[string]oidcProviderClient, len(provider.Clients))
	for _, configuredClient := range provider.Clients {
		digest, err := hex.DecodeString(configuredClient.SecretSHA256)
		if err != nil || len(digest) != sha256.Size {
			return nil, fmt.Errorf("oidc provider: invalid secret_sha256 for client %q", configuredClient.ID)
		}
		client := oidcProviderClient{redirectURIs: make(map[string]struct{}, len(configuredClient.RedirectURIs))}
		copy(client.secretSHA256[:], digest)
		for _, redirectURI := range configuredClient.RedirectURIs {
			client.redirectURIs[redirectURI] = struct{}{}
		}
		clients[configuredClient.ID] = client
	}

	service.issuer = provider.Issuer
	service.frontendURL = provider.FrontendAuthorizeURL
	service.requestTTL = time.Duration(provider.RequestTTLSeconds) * time.Second
	service.codeTTL = time.Duration(provider.CodeTTLSeconds) * time.Second
	service.accessTTL = time.Duration(provider.AccessTokenTTLSeconds) * time.Second
	service.redis = redisClient
	service.userService = userService
	service.signingKey = privateKey
	service.kid = kid
	service.clients = clients
	return service, nil
}

func loadOIDCProviderSigningKey(path string) (*rsa.PrivateKey, string, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("oidc provider: read signing key: %w", err)
	}
	block, _ := pem.Decode(encoded)
	if block == nil {
		return nil, "", errors.New("oidc provider: signing key is not PEM encoded")
	}

	var privateKey *rsa.PrivateKey
	if parsed, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes); parseErr == nil {
		var ok bool
		privateKey, ok = parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, "", errors.New("oidc provider: signing key must be RSA")
		}
	} else {
		privateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, "", fmt.Errorf("oidc provider: parse RSA signing key: %w", err)
		}
	}
	if privateKey.N.BitLen() < 2048 {
		return nil, "", errors.New("oidc provider: RSA signing key must be at least 2048 bits")
	}
	if err := privateKey.Validate(); err != nil {
		return nil, "", fmt.Errorf("oidc provider: invalid RSA signing key: %w", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, "", fmt.Errorf("oidc provider: marshal public key: %w", err)
	}
	digest := sha256.Sum256(publicDER)
	return privateKey, base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

func (s *OIDCProviderService) Enabled() bool {
	return s != nil && s.enabled
}

func (s *OIDCProviderService) Discovery() map[string]any {
	return map[string]any{
		"issuer":                                     s.issuer,
		"authorization_endpoint":                     s.issuer + "/api/v1/oidc/authorize",
		"token_endpoint":                             s.issuer + "/api/v1/oidc/token",
		"userinfo_endpoint":                          s.issuer + "/api/v1/oidc/userinfo",
		"jwks_uri":                                   s.issuer + "/api/v1/oidc/jwks",
		"revocation_endpoint":                        s.issuer + "/api/v1/oidc/revoke",
		"response_types_supported":                   []string{"code"},
		"response_modes_supported":                   []string{"query"},
		"grant_types_supported":                      []string{"authorization_code"},
		"subject_types_supported":                    []string{"public"},
		"id_token_signing_alg_values_supported":      []string{"RS256"},
		"scopes_supported":                           []string{"openid", "profile", "email"},
		"claims_supported":                           []string{"sub", "iss", "aud", "exp", "iat", "nbf", "nonce", "auth_time", "email", "email_verified", "preferred_username", "name", "picture", "at_hash"},
		"token_endpoint_auth_methods_supported":      []string{"client_secret_basic", "client_secret_post"},
		"revocation_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
		"code_challenge_methods_supported":           []string{"S256"},
	}
}

func (s *OIDCProviderService) JWKS() map[string]any {
	publicKey := &s.signingKey.PublicKey
	exponent := make([]byte, 4)
	binary.BigEndian.PutUint32(exponent, uint32(publicKey.E))
	exponent = bytesWithoutLeadingZeroes(exponent)
	return map[string]any{"keys": []map[string]string{{
		"kty": "RSA",
		"use": "sig",
		"alg": "RS256",
		"kid": s.kid,
		"n":   base64.RawURLEncoding.EncodeToString(publicKey.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(exponent),
	}}}
}

func bytesWithoutLeadingZeroes(value []byte) []byte {
	for len(value) > 1 && value[0] == 0 {
		value = value[1:]
	}
	return value
}

func (s *OIDCProviderService) BeginAuthorization(ctx context.Context, input OIDCProviderAuthorizeInput) (string, *OIDCProviderError) {
	if len(input.ClientID) == 0 || len(input.ClientID) > 256 {
		return "", newOIDCProviderError("invalid_request", "client_id is required", http.StatusBadRequest)
	}
	client, exists := s.clients[input.ClientID]
	if !exists {
		return "", newOIDCProviderError("unauthorized_client", "unknown client_id", http.StatusBadRequest)
	}
	if len(input.RedirectURI) == 0 || len(input.RedirectURI) > maxOIDCParameterLength {
		return "", newOIDCProviderError("invalid_request", "redirect_uri is required", http.StatusBadRequest)
	}
	if _, exists := client.redirectURIs[input.RedirectURI]; !exists {
		return "", newOIDCProviderError("invalid_request", "redirect_uri is not registered for this client", http.StatusBadRequest)
	}

	protocolError := func(code, description string) *OIDCProviderError {
		err := newOIDCProviderError(code, description, http.StatusBadRequest)
		err.RedirectURI = input.RedirectURI
		if len(input.State) <= 512 {
			err.State = input.State
		}
		return err
	}
	if input.ResponseType != "code" {
		return "", protocolError("unsupported_response_type", "response_type must be code")
	}
	if len(input.State) == 0 || len(input.State) > 512 {
		return "", protocolError("invalid_request", "state is required and must not exceed 512 characters")
	}
	if len(input.Nonce) == 0 || len(input.Nonce) > 512 {
		return "", protocolError("invalid_request", "nonce is required and must not exceed 512 characters")
	}
	if input.CodeChallengeMethod != "S256" {
		return "", protocolError("invalid_request", "code_challenge_method must be S256")
	}
	challenge, err := base64.RawURLEncoding.DecodeString(input.CodeChallenge)
	if err != nil || len(challenge) != sha256.Size || strings.Contains(input.CodeChallenge, "=") {
		return "", protocolError("invalid_request", "code_challenge must be an unpadded base64url-encoded SHA-256 value")
	}
	scope, scopeErr := normalizeOIDCProviderScope(input.Scope)
	if scopeErr != nil {
		return "", protocolError("invalid_scope", scopeErr.Error())
	}

	request := OIDCProviderAuthorizationRequest{
		ClientID: input.ClientID, RedirectURI: input.RedirectURI, Scope: scope,
		State: input.State, Nonce: input.Nonce, CodeChallenge: input.CodeChallenge,
	}
	encodedRequest, err := json.Marshal(request)
	if err != nil {
		return "", newOIDCProviderError("server_error", "failed to encode authorization request", http.StatusInternalServerError)
	}
	requestID, err := randomOIDCProviderToken()
	if err != nil {
		return "", newOIDCProviderError("server_error", "failed to generate authorization request", http.StatusInternalServerError)
	}
	if err := s.redis.Set(ctx, oidcProviderRedisPrefix+"request:"+hashOIDCProviderToken(requestID), encodedRequest, s.requestTTL).Err(); err != nil {
		return "", newOIDCProviderError("temporarily_unavailable", "authorization state store is unavailable", http.StatusServiceUnavailable)
	}
	frontendURL, err := url.Parse(s.frontendURL)
	if err != nil {
		return "", newOIDCProviderError("server_error", "frontend authorization URL is invalid", http.StatusInternalServerError)
	}
	query := frontendURL.Query()
	query.Set("request_id", requestID)
	frontendURL.RawQuery = query.Encode()
	return frontendURL.String(), nil
}

func normalizeOIDCProviderScope(raw string) (string, error) {
	if len(raw) == 0 || len(raw) > maxOIDCParameterLength {
		return "", errors.New("scope is required")
	}
	seen := make(map[string]struct{})
	ordered := make([]string, 0, 3)
	for _, value := range strings.Fields(raw) {
		if _, allowed := oidcAllowedScopes[value]; !allowed {
			return "", fmt.Errorf("unsupported scope %q", value)
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		ordered = append(ordered, value)
	}
	if _, exists := seen["openid"]; !exists {
		return "", errors.New("scope must contain openid")
	}
	return strings.Join(ordered, " "), nil
}

func (s *OIDCProviderService) CompleteAuthorization(ctx context.Context, requestID string, userID int64, authTime time.Time) (string, *OIDCProviderError) {
	if len(requestID) == 0 || len(requestID) > 512 || userID <= 0 {
		return "", newOIDCProviderError("invalid_request", "request_id is invalid", http.StatusBadRequest)
	}
	encodedRequest, err := s.redis.GetDel(ctx, oidcProviderRedisPrefix+"request:"+hashOIDCProviderToken(requestID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return "", newOIDCProviderError("invalid_request", "authorization request is invalid, expired, or already used", http.StatusBadRequest)
	}
	if err != nil {
		return "", newOIDCProviderError("temporarily_unavailable", "authorization state store is unavailable", http.StatusServiceUnavailable)
	}
	var request OIDCProviderAuthorizationRequest
	if err := json.Unmarshal(encodedRequest, &request); err != nil {
		return "", newOIDCProviderError("server_error", "authorization request state is invalid", http.StatusInternalServerError)
	}
	user, err := s.userService.GetByID(ctx, userID)
	if err != nil || user == nil || !user.IsActive() {
		return "", newOIDCProviderError("access_denied", "user account is not active", http.StatusForbidden)
	}
	if authTime.IsZero() {
		authTime = time.Now().UTC()
	}
	codeState := OIDCProviderAuthorizationCode{
		ClientID: request.ClientID, RedirectURI: request.RedirectURI, Scope: request.Scope,
		Nonce: request.Nonce, CodeChallenge: request.CodeChallenge, UserID: user.ID,
		TokenVersion: user.TokenVersion, AuthTime: authTime.Unix(),
	}
	encodedCode, err := json.Marshal(codeState)
	if err != nil {
		return "", newOIDCProviderError("server_error", "failed to encode authorization code", http.StatusInternalServerError)
	}
	code, err := randomOIDCProviderToken()
	if err != nil {
		return "", newOIDCProviderError("server_error", "failed to generate authorization code", http.StatusInternalServerError)
	}
	if err := s.redis.Set(ctx, oidcProviderRedisPrefix+"code:"+hashOIDCProviderToken(code), encodedCode, s.codeTTL).Err(); err != nil {
		return "", newOIDCProviderError("temporarily_unavailable", "authorization code store is unavailable", http.StatusServiceUnavailable)
	}
	redirectURL, err := url.Parse(request.RedirectURI)
	if err != nil {
		return "", newOIDCProviderError("server_error", "registered redirect URI is invalid", http.StatusInternalServerError)
	}
	query := redirectURL.Query()
	query.Set("code", code)
	query.Set("state", request.State)
	redirectURL.RawQuery = query.Encode()
	return redirectURL.String(), nil
}

func (s *OIDCProviderService) AuthenticateClient(clientID, clientSecret string) *OIDCProviderError {
	if len(clientID) == 0 || len(clientID) > 256 || len(clientSecret) == 0 || len(clientSecret) > maxOIDCParameterLength {
		return newOIDCProviderError("invalid_client", "client authentication failed", http.StatusUnauthorized)
	}
	client, exists := s.clients[clientID]
	presentedDigest := sha256.Sum256([]byte(clientSecret))
	configuredDigest := [sha256.Size]byte{}
	if exists {
		configuredDigest = client.secretSHA256
	}
	if subtle.ConstantTimeCompare(presentedDigest[:], configuredDigest[:]) != 1 || !exists {
		return newOIDCProviderError("invalid_client", "client authentication failed", http.StatusUnauthorized)
	}
	return nil
}

func (s *OIDCProviderService) ExchangeCode(ctx context.Context, input OIDCProviderTokenInput) (*OIDCProviderTokenResponse, *OIDCProviderError) {
	if len(input.Code) == 0 || len(input.Code) > 512 {
		return nil, newOIDCProviderError("invalid_grant", "authorization code is invalid", http.StatusBadRequest)
	}
	encodedCode, err := s.redis.GetDel(ctx, oidcProviderRedisPrefix+"code:"+hashOIDCProviderToken(input.Code)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, newOIDCProviderError("invalid_grant", "authorization code is invalid, expired, or already used", http.StatusBadRequest)
	}
	if err != nil {
		return nil, newOIDCProviderError("temporarily_unavailable", "authorization code store is unavailable", http.StatusServiceUnavailable)
	}
	var codeState OIDCProviderAuthorizationCode
	if err := json.Unmarshal(encodedCode, &codeState); err != nil {
		return nil, newOIDCProviderError("server_error", "authorization code state is invalid", http.StatusInternalServerError)
	}
	if input.ClientID != codeState.ClientID {
		return nil, newOIDCProviderError("invalid_grant", "authorization code was not issued to this client", http.StatusBadRequest)
	}
	if input.RedirectURI != codeState.RedirectURI {
		return nil, newOIDCProviderError("invalid_grant", "redirect_uri does not match the authorization request", http.StatusBadRequest)
	}
	if !validOIDCProviderCodeVerifier(input.CodeVerifier) {
		return nil, newOIDCProviderError("invalid_grant", "code_verifier is invalid", http.StatusBadRequest)
	}
	verifierDigest := sha256.Sum256([]byte(input.CodeVerifier))
	expectedChallenge := base64.RawURLEncoding.EncodeToString(verifierDigest[:])
	if subtle.ConstantTimeCompare([]byte(expectedChallenge), []byte(codeState.CodeChallenge)) != 1 {
		return nil, newOIDCProviderError("invalid_grant", "PKCE verification failed", http.StatusBadRequest)
	}
	user, err := s.userService.GetByID(ctx, codeState.UserID)
	if err != nil || user == nil || !user.IsActive() || user.TokenVersion != codeState.TokenVersion {
		return nil, newOIDCProviderError("invalid_grant", "user session is no longer valid", http.StatusBadRequest)
	}

	accessToken, err := randomOIDCProviderToken()
	if err != nil {
		return nil, newOIDCProviderError("server_error", "failed to generate access token", http.StatusInternalServerError)
	}
	accessState := OIDCProviderAccessTokenState{
		ClientID: codeState.ClientID, Scope: codeState.Scope, UserID: codeState.UserID,
		TokenVersion: codeState.TokenVersion, AuthTime: codeState.AuthTime,
	}
	encodedAccessState, err := json.Marshal(accessState)
	if err != nil {
		return nil, newOIDCProviderError("server_error", "failed to encode access token", http.StatusInternalServerError)
	}
	if err := s.redis.Set(ctx, oidcProviderRedisPrefix+"access:"+hashOIDCProviderToken(accessToken), encodedAccessState, s.accessTTL).Err(); err != nil {
		return nil, newOIDCProviderError("temporarily_unavailable", "access token store is unavailable", http.StatusServiceUnavailable)
	}
	idToken, err := s.signIDToken(ctx, user, codeState, accessToken)
	if err != nil {
		_ = s.redis.Del(ctx, oidcProviderRedisPrefix+"access:"+hashOIDCProviderToken(accessToken)).Err()
		return nil, newOIDCProviderError("server_error", "failed to sign ID token", http.StatusInternalServerError)
	}
	return &OIDCProviderTokenResponse{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int(s.accessTTL / time.Second),
		IDToken:     idToken,
		Scope:       codeState.Scope,
	}, nil
}

func validOIDCProviderCodeVerifier(verifier string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	for _, character := range verifier {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("-._~", character) {
			continue
		}
		return false
	}
	return true
}

func (s *OIDCProviderService) signIDToken(ctx context.Context, user *User, codeState OIDCProviderAuthorizationCode, accessToken string) (string, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(s.accessTTL)
	accessDigest := sha256.Sum256([]byte(accessToken))
	claims := oidcProviderIDTokenClaims{
		Nonce:           codeState.Nonce,
		AuthTime:        codeState.AuthTime,
		AccessTokenHash: base64.RawURLEncoding.EncodeToString(accessDigest[:sha256.Size/2]),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: s.issuer, Subject: strconv.FormatInt(user.ID, 10),
			Audience:  jwt.ClaimStrings{codeState.ClientID},
			ExpiresAt: jwt.NewNumericDate(expiresAt), IssuedAt: jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	if oidcProviderScopeContains(codeState.Scope, "email") {
		verified := s.emailVerified(ctx, user)
		claims.Email = user.Email
		claims.EmailVerified = &verified
	}
	if oidcProviderScopeContains(codeState.Scope, "profile") {
		claims.PreferredUsername = user.Username
		claims.Name = user.Username
		claims.Picture = user.AvatarURL
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = s.kid
	return token.SignedString(s.signingKey)
}

func (s *OIDCProviderService) UserInfo(ctx context.Context, accessToken string) (*OIDCProviderUserInfo, *OIDCProviderError) {
	state, protocolErr := s.loadAccessTokenState(ctx, accessToken)
	if protocolErr != nil {
		return nil, protocolErr
	}
	user, err := s.userService.GetByID(ctx, state.UserID)
	if err != nil || user == nil || !user.IsActive() || user.TokenVersion != state.TokenVersion {
		return nil, newOIDCProviderError("invalid_token", "access token is no longer valid", http.StatusUnauthorized)
	}
	info := &OIDCProviderUserInfo{Subject: strconv.FormatInt(user.ID, 10)}
	if oidcProviderScopeContains(state.Scope, "email") {
		verified := s.emailVerified(ctx, user)
		info.Email = user.Email
		info.EmailVerified = &verified
	}
	if oidcProviderScopeContains(state.Scope, "profile") {
		info.PreferredUsername = user.Username
		info.Name = user.Username
		info.Picture = user.AvatarURL
	}
	return info, nil
}

func (s *OIDCProviderService) emailVerified(ctx context.Context, user *User) bool {
	if s == nil || s.userService == nil || user == nil {
		return false
	}
	summaries, err := s.userService.GetProfileIdentitySummaries(ctx, user.ID, user)
	if err != nil {
		return false
	}
	return summaries.Email.Bound && summaries.Email.VerifiedAt != nil && !summaries.Email.VerifiedAt.IsZero()
}

func oidcProviderScopeContains(scope, expected string) bool {
	for _, value := range strings.Fields(scope) {
		if value == expected {
			return true
		}
	}
	return false
}

func (s *OIDCProviderService) loadAccessTokenState(ctx context.Context, accessToken string) (*OIDCProviderAccessTokenState, *OIDCProviderError) {
	if len(accessToken) == 0 || len(accessToken) > 512 {
		return nil, newOIDCProviderError("invalid_token", "access token is invalid", http.StatusUnauthorized)
	}
	encodedState, err := s.redis.Get(ctx, oidcProviderRedisPrefix+"access:"+hashOIDCProviderToken(accessToken)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, newOIDCProviderError("invalid_token", "access token is invalid or expired", http.StatusUnauthorized)
	}
	if err != nil {
		return nil, newOIDCProviderError("temporarily_unavailable", "access token store is unavailable", http.StatusServiceUnavailable)
	}
	var state OIDCProviderAccessTokenState
	if err := json.Unmarshal(encodedState, &state); err != nil {
		return nil, newOIDCProviderError("invalid_token", "access token state is invalid", http.StatusUnauthorized)
	}
	return &state, nil
}

func (s *OIDCProviderService) Revoke(ctx context.Context, clientID, accessToken string) *OIDCProviderError {
	if len(accessToken) == 0 || len(accessToken) > 512 {
		return nil
	}
	key := oidcProviderRedisPrefix + "access:" + hashOIDCProviderToken(accessToken)
	encodedState, err := s.redis.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil {
		return newOIDCProviderError("temporarily_unavailable", "access token store is unavailable", http.StatusServiceUnavailable)
	}
	var state OIDCProviderAccessTokenState
	if json.Unmarshal(encodedState, &state) != nil || state.ClientID != clientID {
		return nil
	}
	if err := s.redis.Del(ctx, key).Err(); err != nil {
		return newOIDCProviderError("temporarily_unavailable", "access token store is unavailable", http.StatusServiceUnavailable)
	}
	return nil
}

func randomOIDCProviderToken() (string, error) {
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

func hashOIDCProviderToken(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
