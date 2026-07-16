package service

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

type openaiOAuthClientStateStub struct {
	exchangeCalled int32
	lastClientID   string
}

func (s *openaiOAuthClientStateStub) ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID string) (*openai.TokenResponse, error) {
	atomic.AddInt32(&s.exchangeCalled, 1)
	s.lastClientID = clientID
	return &openai.TokenResponse{
		AccessToken:  "at",
		RefreshToken: "rt",
		ExpiresIn:    3600,
	}, nil
}

func (s *openaiOAuthClientStateStub) RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*openai.TokenResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *openaiOAuthClientStateStub) RefreshTokenWithClientID(ctx context.Context, refreshToken, proxyURL string, clientID string) (*openai.TokenResponse, error) {
	return s.RefreshToken(ctx, refreshToken, proxyURL)
}

func TestOpenAIOAuthService_ExchangeCode_StateRequired(t *testing.T) {
	client := &openaiOAuthClientStateStub{}
	svc := NewOpenAIOAuthService(nil, client)
	defer svc.Stop()

	svc.sessionStore.Set("sid", &openai.OAuthSession{
		State:        "expected-state",
		CodeVerifier: "verifier",
		RedirectURI:  openai.DefaultRedirectURI,
		CreatedAt:    time.Now(),
	})

	_, err := svc.ExchangeCode(context.Background(), &OpenAIExchangeCodeInput{
		SessionID: "sid",
		Code:      "auth-code",
	})
	require.Error(t, err)
	require.Equal(t, "OPENAI_OAUTH_STATE_REQUIRED", infraerrors.Reason(err))
	require.Equal(t, int32(0), atomic.LoadInt32(&client.exchangeCalled))
}

func TestOpenAIOAuthService_ExchangeCode_StateMismatch(t *testing.T) {
	client := &openaiOAuthClientStateStub{}
	svc := NewOpenAIOAuthService(nil, client)
	defer svc.Stop()

	svc.sessionStore.Set("sid", &openai.OAuthSession{
		State:        "expected-state",
		CodeVerifier: "verifier",
		RedirectURI:  openai.DefaultRedirectURI,
		CreatedAt:    time.Now(),
	})

	_, err := svc.ExchangeCode(context.Background(), &OpenAIExchangeCodeInput{
		SessionID: "sid",
		Code:      "auth-code",
		State:     "wrong-state",
	})
	require.Error(t, err)
	require.Equal(t, "OPENAI_OAUTH_INVALID_STATE", infraerrors.Reason(err))
	require.Equal(t, int32(0), atomic.LoadInt32(&client.exchangeCalled))
}

func TestOpenAIOAuthService_ExchangeCode_StateMatch(t *testing.T) {
	client := &openaiOAuthClientStateStub{}
	svc := NewOpenAIOAuthService(nil, client)
	defer svc.Stop()

	svc.sessionStore.Set("sid", &openai.OAuthSession{
		State:        "expected-state",
		CodeVerifier: "verifier",
		RedirectURI:  openai.DefaultRedirectURI,
		CreatedAt:    time.Now(),
	})

	info, err := svc.ExchangeCode(context.Background(), &OpenAIExchangeCodeInput{
		SessionID: "sid",
		Code:      "auth-code",
		State:     "expected-state",
	})
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, "at", info.AccessToken)
	require.Equal(t, openai.ClientID, info.ClientID)
	require.Equal(t, openai.ClientID, client.lastClientID)
	require.Equal(t, int32(1), atomic.LoadInt32(&client.exchangeCalled))

	_, ok := svc.sessionStore.Get("sid")
	require.False(t, ok)
}

func TestOpenAIOAuthService_ExchangeCode_StatelessSessionSurvivesMemoryLoss(t *testing.T) {
	client := &openaiOAuthClientStateStub{}
	svc := NewOpenAIOAuthService(nil, client)
	svc.SetSessionTokenSecret(strings.Repeat("s", 32))
	defer svc.Stop()

	result, err := svc.GenerateAuthURL(context.Background(), nil, "", PlatformOpenAI)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(result.SessionID, openAIOAuthSessionTokenPrefix))
	require.NotEmpty(t, result.AuthURL)

	session, ok := svc.sessionStore.Get(result.SessionID)
	require.True(t, ok)
	state := session.State

	svc.sessionStore.Delete(result.SessionID)

	info, err := svc.ExchangeCode(context.Background(), &OpenAIExchangeCodeInput{
		SessionID: result.SessionID,
		Code:      "auth-code",
		State:     state,
	})
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, "at", info.AccessToken)
	require.Equal(t, openai.ClientID, client.lastClientID)
	require.Equal(t, int32(1), atomic.LoadInt32(&client.exchangeCalled))
}

func TestOpenAIOAuthService_ExchangeCode_LegacyMissingSessionUsesChineseError(t *testing.T) {
	client := &openaiOAuthClientStateStub{}
	svc := NewOpenAIOAuthService(nil, client)
	defer svc.Stop()

	_, err := svc.ExchangeCode(context.Background(), &OpenAIExchangeCodeInput{
		SessionID: "missing-session",
		Code:      "auth-code",
		State:     "state",
	})
	require.Error(t, err)
	require.Equal(t, "OPENAI_OAUTH_SESSION_NOT_FOUND", infraerrors.Reason(err))
	require.Equal(t, "授权会话不存在或已过期，请重新生成授权链接后再完成授权", infraerrors.Message(err))
	require.Equal(t, int32(0), atomic.LoadInt32(&client.exchangeCalled))
}
