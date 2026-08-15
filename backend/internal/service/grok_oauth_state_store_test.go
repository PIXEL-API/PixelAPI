//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type grokOAuthStateStoreStub struct {
	mu      sync.Mutex
	values  map[string][]byte
	setErr  error
	getErr  error
	takeErr error
}

func newGrokOAuthStateStoreStub() *grokOAuthStateStoreStub {
	return &grokOAuthStateStoreStub{values: make(map[string][]byte)}
}

func (s *grokOAuthStateStoreStub) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = append([]byte(nil), value...)
	return nil
}

func (s *grokOAuthStateStoreStub) Take(_ context.Context, key string) ([]byte, bool, error) {
	if s.takeErr != nil {
		return nil, false, s.takeErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, found := s.values[key]
	if found {
		delete(s.values, key)
	}
	return append([]byte(nil), value...), found, nil
}

func (s *grokOAuthStateStoreStub) Get(_ context.Context, key string) ([]byte, bool, error) {
	if s.getErr != nil {
		return nil, false, s.getErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, found := s.values[key]
	return append([]byte(nil), value...), found, nil
}

func (s *grokOAuthStateStoreStub) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, key)
	return nil
}

func TestGrokOAuthServiceRedisSessionWorksAcrossInstancesAndConsumesOnce(t *testing.T) {
	stateStore := newGrokOAuthStateStoreStub()
	client := &grokOAuthClientStub{}
	issuer := NewGrokOAuthService(nil, client, stateStore)
	consumer := NewGrokOAuthService(nil, client, stateStore)
	defer issuer.Stop()
	defer consumer.Stop()

	auth, err := issuer.GenerateAuthURL(context.Background(), nil, "")
	require.NoError(t, err)

	_, err = consumer.ExchangeCode(context.Background(), &GrokExchangeCodeInput{
		SessionID: auth.SessionID,
		Code:      "code",
		State:     auth.State,
	})
	require.NoError(t, err)

	_, err = issuer.ExchangeCode(context.Background(), &GrokExchangeCodeInput{
		SessionID: auth.SessionID,
		Code:      "replay",
		State:     auth.State,
	})
	require.Equal(t, "GROK_OAUTH_SESSION_NOT_FOUND", infraerrors.Reason(err))
	require.Equal(t, 1, client.exchangeCalls)
}

func TestGrokOAuthServiceRedisFailuresDoNotFallBackToProcessMemory(t *testing.T) {
	stateStore := newGrokOAuthStateStoreStub()
	stateStore.setErr = errors.New("redis unavailable")
	svc := NewGrokOAuthService(nil, &grokOAuthClientStub{}, stateStore)
	defer svc.Stop()

	_, err := svc.GenerateAuthURL(context.Background(), nil, "")
	require.Equal(t, "GROK_OAUTH_SESSION_STORE_UNAVAILABLE", infraerrors.Reason(err))

	stateStore.setErr = nil
	auth, err := svc.GenerateAuthURL(context.Background(), nil, "")
	require.NoError(t, err)
	stateStore.getErr = errors.New("redis unavailable")

	_, err = svc.ExchangeCode(context.Background(), &GrokExchangeCodeInput{
		SessionID: auth.SessionID,
		Code:      "code",
		State:     auth.State,
	})
	require.Equal(t, "GROK_OAUTH_SESSION_STORE_UNAVAILABLE", infraerrors.Reason(err))
}

func TestGrokOAuthServiceRedisTakeFailureDoesNotCallProvider(t *testing.T) {
	stateStore := newGrokOAuthStateStoreStub()
	client := &grokOAuthClientStub{}
	svc := NewGrokOAuthService(nil, client, stateStore)
	defer svc.Stop()

	auth, err := svc.GenerateAuthURL(context.Background(), nil, "")
	require.NoError(t, err)
	stateStore.takeErr = errors.New("redis unavailable")

	_, err = svc.ExchangeCode(context.Background(), &GrokExchangeCodeInput{
		SessionID: auth.SessionID,
		Code:      "code",
		State:     auth.State,
	})
	require.Equal(t, "GROK_OAUTH_SESSION_STORE_UNAVAILABLE", infraerrors.Reason(err))
	require.Zero(t, client.exchangeCalls)
}
