//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

type grokOAuthClientStub struct {
	refreshResponse *xai.TokenResponse
	exchangeCalls   int
}

func (s *grokOAuthClientStub) ExchangeCode(context.Context, string, string, string, string, string) (*xai.TokenResponse, error) {
	s.exchangeCalls++
	return &xai.TokenResponse{}, nil
}

func (s *grokOAuthClientStub) RefreshToken(context.Context, string, string, string) (*xai.TokenResponse, error) {
	return s.refreshResponse, nil
}

func (s *grokOAuthClientStub) ConvertSSOToBuild(context.Context, string, string) (*xai.TokenResponse, error) {
	return &xai.TokenResponse{}, nil
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

func TestGrokOAuthServiceExchangeCodeRequiresStateForCallbackURLAndConsumesSession(t *testing.T) {
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
	require.Error(t, err)
	require.Contains(t, err.Error(), "GROK_OAUTH_SESSION_NOT_FOUND")
	require.Zero(t, client.exchangeCalls)
}
