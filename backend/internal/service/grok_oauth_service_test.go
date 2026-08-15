//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

type grokOAuthClientStub struct {
	refreshResponse  *xai.TokenResponse
	exchangeResponse *xai.TokenResponse
	ssoResponse      *xai.TokenResponse
	exchangeErr      error
	passwordResult   *xai.GrokPasswordLoginResult
	passwordErr      error
	refreshErr       error
	exchangeCalls    int
	passwordCalls    int
	ssoCalls         int
	lastSSOToken     string
	lastProxyURL     string
}

func (s *grokOAuthClientStub) ExchangeCode(context.Context, string, string, string, string, string) (*xai.TokenResponse, error) {
	s.exchangeCalls++
	if s.exchangeResponse != nil || s.exchangeErr != nil {
		return s.exchangeResponse, s.exchangeErr
	}
	return &xai.TokenResponse{AccessToken: "access-token", ExpiresIn: 3600}, nil
}

func (s *grokOAuthClientStub) RefreshToken(_ context.Context, _, proxyURL, _ string) (*xai.TokenResponse, error) {
	s.lastProxyURL = proxyURL
	return s.refreshResponse, s.refreshErr
}

func (s *grokOAuthClientStub) ConvertSSOToBuild(_ context.Context, ssoToken, proxyURL string) (*xai.TokenResponse, error) {
	s.ssoCalls++
	s.lastSSOToken = ssoToken
	s.lastProxyURL = proxyURL
	if s.ssoResponse != nil {
		return s.ssoResponse, nil
	}
	return &xai.TokenResponse{}, nil
}

func (s *grokOAuthClientStub) LoginWithPassword(_ context.Context, _, _, proxyURL string) (*xai.GrokPasswordLoginResult, error) {
	s.passwordCalls++
	s.lastProxyURL = proxyURL
	return s.passwordResult, s.passwordErr
}

type grokOAuthProxyRepoStub struct {
	ProxyRepository
	proxy *Proxy
	err   error
}

func (s *grokOAuthProxyRepoStub) GetByID(context.Context, int64) (*Proxy, error) {
	return s.proxy, s.err
}

func TestGrokOAuthServiceRefreshTokenPreservesOriginalRefreshTokenWhenNotRotated(t *testing.T) {
	svc := NewGrokOAuthService(nil, &grokOAuthClientStub{
		refreshResponse: &xai.TokenResponse{
			AccessToken: "new-access-token",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
		},
	})
	defer svc.Stop()

	info, err := svc.RefreshToken(context.Background(), "original-refresh-token", "", "client-id")
	require.NoError(t, err)
	require.Equal(t, "new-access-token", info.AccessToken)
	require.Equal(t, "original-refresh-token", info.RefreshToken)
	require.Equal(t, "client-id", info.ClientID)
}

func TestGrokOAuthServicePasswordAuthIsDisabledByDefault(t *testing.T) {
	client := &grokOAuthClientStub{
		passwordResult: &xai.GrokPasswordLoginResult{SSOToken: "sso-secret"},
		ssoResponse:    &xai.TokenResponse{AccessToken: "access-token"},
	}
	svc := NewGrokOAuthService(nil, client)
	defer svc.Stop()

	require.False(t, svc.GetCapabilities().PasswordAuthEnabled)
	info, err := svc.AuthorizePassword(context.Background(), "admin@example.com", "password-secret", nil)

	require.Nil(t, info)
	require.ErrorIs(t, err, ErrGrokPasswordAuthDisabled)
	require.Zero(t, client.passwordCalls)
	require.Zero(t, client.ssoCalls)
	require.NotContains(t, err.Error(), "password-secret")
}

func TestGrokOAuthServicePasswordAuthUsesProxyAndDoesNotReturnSSO(t *testing.T) {
	proxyID := int64(42)
	client := &grokOAuthClientStub{
		passwordResult: &xai.GrokPasswordLoginResult{
			Email:    "admin@example.com",
			SSOToken: "sso-secret",
		},
		ssoResponse: &xai.TokenResponse{
			AccessToken:  "access-token",
			RefreshToken: "refresh-token",
			ExpiresIn:    3600,
		},
	}
	svc := NewGrokOAuthService(&grokOAuthProxyRepoStub{proxy: &Proxy{
		ID: proxyID, Protocol: "http", Host: "127.0.0.1", Port: 8080,
	}}, client)
	svc.SetPasswordAuthEnabled(true)
	defer svc.Stop()

	require.True(t, svc.GetCapabilities().PasswordAuthEnabled)
	info, err := svc.AuthorizePassword(context.Background(), "admin@example.com", "password-secret", &proxyID)

	require.NoError(t, err)
	require.Equal(t, "access-token", info.AccessToken)
	require.Equal(t, "refresh-token", info.RefreshToken)
	require.Equal(t, "admin@example.com", info.Email)
	require.Equal(t, 1, client.passwordCalls)
	require.Equal(t, 1, client.ssoCalls)
	require.Equal(t, "sso-secret", client.lastSSOToken)
	require.Equal(t, "http://127.0.0.1:8080", client.lastProxyURL)

	encoded, err := json.Marshal(info)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "password-secret")
	require.NotContains(t, string(encoded), "sso-secret")
	credentials := svc.BuildAccountCredentials(info)
	require.NotContains(t, credentials, "password")
	require.NotContains(t, credentials, "sso_token")
}

func TestGrokOAuthServiceValidateSSOTokenNormalizesCookieHeader(t *testing.T) {
	client := &grokOAuthClientStub{
		ssoResponse: &xai.TokenResponse{AccessToken: "access-token", ExpiresIn: 3600},
	}
	svc := NewGrokOAuthService(nil, client)
	defer svc.Stop()

	info, err := svc.ValidateSSOToken(context.Background(), "Cookie: other=value; sso=sso-secret; trailing=value", nil)

	require.NoError(t, err)
	require.Equal(t, "access-token", info.AccessToken)
	require.Equal(t, "sso-secret", client.lastSSOToken)
	encoded, err := json.Marshal(info)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "sso-secret")
}

func TestGrokOAuthServiceRefreshTokenRejectsMissingAccessToken(t *testing.T) {
	tests := []struct {
		name     string
		response *xai.TokenResponse
	}{
		{name: "nil response"},
		{name: "empty response", response: &xai.TokenResponse{}},
		{name: "blank access token", response: &xai.TokenResponse{AccessToken: " \t "}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewGrokOAuthService(nil, &grokOAuthClientStub{refreshResponse: tt.response})
			defer svc.Stop()

			info, err := svc.RefreshToken(context.Background(), "refresh-token", "", "client-id")

			require.Nil(t, info)
			require.Error(t, err)
			require.Equal(t, http.StatusBadGateway, infraerrors.Code(err))
			require.Equal(t, "GROK_OAUTH_TOKEN_RESPONSE_INVALID", infraerrors.Reason(err))
		})
	}
}

func TestGrokOAuthServiceProxyURLClassifiesLookupResults(t *testing.T) {
	proxyID := int64(42)
	tests := []struct {
		name       string
		proxy      *Proxy
		err        error
		wantCode   int
		wantReason string
	}{
		{
			name:       "configured proxy not found",
			err:        ErrProxyNotFound,
			wantCode:   http.StatusBadRequest,
			wantReason: "GROK_OAUTH_PROXY_NOT_FOUND",
		},
		{
			name:       "proxy lookup temporarily unavailable",
			err:        errors.New("storage unavailable"),
			wantCode:   http.StatusServiceUnavailable,
			wantReason: "GROK_OAUTH_PROXY_LOOKUP_FAILED",
		},
		{
			name:       "repository returned nil proxy",
			wantCode:   http.StatusBadRequest,
			wantReason: "GROK_OAUTH_PROXY_NOT_FOUND",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewGrokOAuthService(&grokOAuthProxyRepoStub{
				proxy: tt.proxy,
				err:   tt.err,
			}, &grokOAuthClientStub{})
			defer svc.Stop()

			proxyURL, err := svc.proxyURL(context.Background(), &proxyID)

			require.Empty(t, proxyURL)
			require.Error(t, err)
			require.Equal(t, tt.wantCode, infraerrors.Code(err))
			require.Equal(t, tt.wantReason, infraerrors.Reason(err))
			require.NotContains(t, err.Error(), "storage unavailable")
		})
	}
}

func TestGrokOAuthServiceExchangeCodeValidationDoesNotConsumeSessionAndReplayFails(t *testing.T) {
	client := &grokOAuthClientStub{}
	svc := NewGrokOAuthService(nil, client)
	defer svc.Stop()

	auth, err := svc.GenerateAuthURL(context.Background(), nil, "")
	require.NoError(t, err)

	_, err = svc.ExchangeCode(context.Background(), &GrokExchangeCodeInput{
		SessionID: auth.SessionID,
		Code:      "http://127.0.0.1:56121/callback?code=code-without-state",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "GROK_OAUTH_STATE_REQUIRED")
	require.Zero(t, client.exchangeCalls)

	_, err = svc.ExchangeCode(context.Background(), &GrokExchangeCodeInput{
		SessionID: auth.SessionID,
		Code:      "code-with-state",
		State:     auth.State,
	})
	require.NoError(t, err)
	require.Equal(t, 1, client.exchangeCalls)

	_, err = svc.ExchangeCode(context.Background(), &GrokExchangeCodeInput{
		SessionID: auth.SessionID,
		Code:      "replayed-code",
		State:     auth.State,
	})
	require.Error(t, err)
	require.Equal(t, "GROK_OAUTH_SESSION_NOT_FOUND", infraerrors.Reason(err))
	require.Equal(t, 1, client.exchangeCalls)
}

func TestGrokOAuthServiceExchangeCodeRejectsRedirectMismatchWithoutConsuming(t *testing.T) {
	client := &grokOAuthClientStub{}
	svc := NewGrokOAuthService(nil, client)
	defer svc.Stop()

	auth, err := svc.GenerateAuthURL(context.Background(), nil, "http://127.0.0.1:56121/callback")
	require.NoError(t, err)

	_, err = svc.ExchangeCode(context.Background(), &GrokExchangeCodeInput{
		SessionID:   auth.SessionID,
		Code:        "code",
		State:       auth.State,
		RedirectURI: "http://127.0.0.1:56121/other",
	})
	require.Equal(t, "GROK_OAUTH_REDIRECT_URI_MISMATCH", infraerrors.Reason(err))
	require.Zero(t, client.exchangeCalls)

	_, err = svc.ExchangeCode(context.Background(), &GrokExchangeCodeInput{
		SessionID: auth.SessionID,
		Code:      "code",
		State:     auth.State,
	})
	require.NoError(t, err)
	require.Equal(t, 1, client.exchangeCalls)
}

func TestGrokOAuthServiceExchangeCodeRejectsProxyMismatchWithoutConsuming(t *testing.T) {
	proxyID := int64(42)
	client := &grokOAuthClientStub{}
	svc := NewGrokOAuthService(&grokOAuthProxyRepoStub{proxy: &Proxy{
		ID: proxyID, Protocol: "http", Host: "127.0.0.1", Port: 8080,
	}}, client)
	defer svc.Stop()

	auth, err := svc.GenerateAuthURL(context.Background(), nil, "")
	require.NoError(t, err)

	_, err = svc.ExchangeCode(context.Background(), &GrokExchangeCodeInput{
		SessionID: auth.SessionID,
		Code:      "code",
		State:     auth.State,
		ProxyID:   &proxyID,
	})
	require.Equal(t, "GROK_OAUTH_PROXY_MISMATCH", infraerrors.Reason(err))
	require.Zero(t, client.exchangeCalls)

	_, err = svc.ExchangeCode(context.Background(), &GrokExchangeCodeInput{
		SessionID: auth.SessionID,
		Code:      "code",
		State:     auth.State,
	})
	require.NoError(t, err)
}

func TestGrokOAuthServiceRefreshAccountTokenUsesNewJWTSubscriptionTier(t *testing.T) {
	client := &grokOAuthClientStub{refreshResponse: &xai.TokenResponse{
		AccessToken: grokOAuthTestJWT(t, map[string]any{
			"email": "new@example.com", "sub": "user-sub", "team_id": "team-1", "tier": 0,
		}),
		ExpiresIn: 3600,
	}}
	svc := NewGrokOAuthService(nil, client)
	defer svc.Stop()

	info, err := svc.RefreshAccountToken(context.Background(), &Account{
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"refresh_token":     "refresh-token",
			"subscription_tier": "supergrok_heavy",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "free", info.SubscriptionTier)
	require.Equal(t, "new@example.com", info.Email)
	require.Equal(t, "user-sub", info.Subject)
	require.Equal(t, "team-1", info.TeamID)

	credentials := svc.BuildAccountCredentials(info)
	require.Equal(t, "free", credentials["subscription_tier"])
	require.Equal(t, "user-sub", credentials["sub"])
	require.Equal(t, "team-1", credentials["team_id"])
}

func TestGrokOAuthServiceRefreshAccountTokenKeepsStoredTierForOpaqueAccessToken(t *testing.T) {
	client := &grokOAuthClientStub{refreshResponse: &xai.TokenResponse{
		AccessToken: "opaque-access-token",
		IDToken:     grokOAuthTestJWT(t, map[string]any{"tier": 5}),
		ExpiresIn:   3600,
	}}
	svc := NewGrokOAuthService(nil, client)
	defer svc.Stop()

	info, err := svc.RefreshAccountToken(context.Background(), &Account{
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"refresh_token":     "refresh-token",
			"subscription_tier": "supergrok_lite",
		},
	})
	require.NoError(t, err)
	require.Equal(t, "supergrok_lite", info.SubscriptionTier)
}

func TestGrokOAuthServiceRefreshFailureSnapshotsActuallyResolvedProxyVersion(t *testing.T) {
	proxyID := int64(77)
	oldUpdatedAt := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	actualUpdatedAt := oldUpdatedAt.Add(time.Minute)
	client := &grokOAuthClientStub{refreshErr: errors.New("Proxy Authentication Required")}
	svc := NewGrokOAuthService(&grokOAuthProxyRepoStub{proxy: &Proxy{
		ID:        proxyID,
		Protocol:  "http",
		Host:      "proxy.example.com",
		Port:      8080,
		Username:  "new-user",
		Password:  "new-password",
		Status:    StatusActive,
		UpdatedAt: actualUpdatedAt,
	}}, client)
	defer svc.Stop()
	account := &Account{
		ID:       7001,
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		ProxyID:  &proxyID,
		Proxy: &Proxy{
			ID:        proxyID,
			UpdatedAt: oldUpdatedAt,
		},
		Credentials: map[string]any{"refresh_token": "refresh-token"},
	}

	_, err := svc.RefreshAccountToken(context.Background(), account)

	require.Error(t, err)
	require.Contains(t, client.lastProxyURL, "new-user:new-password@proxy.example.com:8080")
	snapshot := grokCredentialMutationSnapshotFromError(err, account)
	require.NotNil(t, snapshot.ProxyUpdatedAt)
	require.Equal(t, actualUpdatedAt, *snapshot.ProxyUpdatedAt)
	require.NotEqual(t, oldUpdatedAt, *snapshot.ProxyUpdatedAt)
}

func TestGrokOAuthServiceMissingRefreshTokenSnapshotsActuallyResolvedProxyVersion(t *testing.T) {
	proxyID := int64(78)
	staleUpdatedAt := time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC)
	actualUpdatedAt := staleUpdatedAt.Add(2 * time.Minute)
	client := &grokOAuthClientStub{}
	svc := NewGrokOAuthService(&grokOAuthProxyRepoStub{proxy: &Proxy{
		ID:        proxyID,
		Protocol:  "http",
		Host:      "proxy.example.com",
		Port:      8080,
		Status:    StatusActive,
		UpdatedAt: actualUpdatedAt,
	}}, client)
	defer svc.Stop()
	account := &Account{
		ID:       7002,
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		ProxyID:  &proxyID,
		Proxy: &Proxy{
			ID:        proxyID,
			UpdatedAt: staleUpdatedAt,
		},
		Credentials: map[string]any{"access_token": "expired-access"},
	}

	_, err := svc.RefreshAccountToken(context.Background(), account)

	require.Error(t, err)
	require.Equal(t, "GROK_OAUTH_NO_REFRESH_TOKEN", infraerrors.Reason(err))
	require.Empty(t, client.lastProxyURL, "缺 refresh token 时不应向上游发请求")
	snapshot := grokCredentialMutationSnapshotFromError(err, account)
	require.NotNil(t, snapshot.ProxyUpdatedAt)
	require.Equal(t, actualUpdatedAt, *snapshot.ProxyUpdatedAt)
	require.NotEqual(t, staleUpdatedAt, *snapshot.ProxyUpdatedAt)
}

func grokOAuthTestJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}
