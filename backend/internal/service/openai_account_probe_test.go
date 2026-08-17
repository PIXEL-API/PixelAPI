//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

func TestOpenAIOAuthServiceProbeChatGPTAccountInfoSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"accounts":{"org-1":{"account":{"plan_type":"plus","is_default":true}}}}`))
	}))
	defer server.Close()

	originalURL := chatGPTAccountsCheckURL
	chatGPTAccountsCheckURL = server.URL
	defer func() { chatGPTAccountsCheckURL = originalURL }()

	svc := NewOpenAIOAuthService(nil, nil)
	svc.privacyClientFactory = func(proxyURL string) (*req.Client, error) {
		return req.C(), nil
	}
	defer svc.Stop()

	info, err := svc.ProbeChatGPTAccountInfo(context.Background(), "access-token", "")
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, "plus", info.PlanType)
}

func TestOpenAIOAuthServiceProbeChatGPTAccountInfoEmptyToken(t *testing.T) {
	svc := NewOpenAIOAuthService(nil, nil)
	defer svc.Stop()

	_, err := svc.ProbeChatGPTAccountInfo(context.Background(), "", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

func TestOpenAIOAuthServiceProbeChatGPTAccountInfoFactoryNil(t *testing.T) {
	svc := NewOpenAIOAuthService(nil, nil)
	defer svc.Stop()

	_, err := svc.ProbeChatGPTAccountInfo(context.Background(), "access-token", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unavailable")
}

func TestOpenAIOAuthServiceProbeChatGPTAccountInfoFactoryError(t *testing.T) {
	svc := NewOpenAIOAuthService(nil, nil)
	svc.privacyClientFactory = func(proxyURL string) (*req.Client, error) {
		return nil, errors.New("factory failed")
	}
	defer svc.Stop()

	_, err := svc.ProbeChatGPTAccountInfo(context.Background(), "access-token", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to verify")
}

func TestOpenAIOAuthServiceProbeChatGPTAccountInfoUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer server.Close()

	originalURL := chatGPTAccountsCheckURL
	chatGPTAccountsCheckURL = server.URL
	defer func() { chatGPTAccountsCheckURL = originalURL }()

	svc := NewOpenAIOAuthService(nil, nil)
	svc.privacyClientFactory = func(proxyURL string) (*req.Client, error) {
		return req.C(), nil
	}
	defer svc.Stop()

	_, err := svc.ProbeChatGPTAccountInfo(context.Background(), "access-token", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to verify")
}
