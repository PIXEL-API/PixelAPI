package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

func TestOpenAIOAuthServiceValidateCodexPersonalAccessToken(t *testing.T) {
	var gotAuthorization string
	var gotAccept string
	var gotOriginator string
	var gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("authorization")
		gotAccept = r.Header.Get("accept")
		gotOriginator = r.Header.Get("originator")
		gotUserAgent = r.Header.Get("user-agent")
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"email":"user@example.com",
			"chatgpt_user_id":"user-test",
			"chatgpt_account_id":"account-test",
			"chatgpt_plan_type":"team",
			"chatgpt_account_is_fedramp":true
		}`))
	}))
	defer server.Close()

	originalURL := openAICodexPATWhoamiURL
	openAICodexPATWhoamiURL = server.URL
	defer func() { openAICodexPATWhoamiURL = originalURL }()

	service := NewOpenAIOAuthService(nil, nil)
	defer service.Stop()

	info, err := service.ValidateCodexPersonalAccessToken(context.Background(), " at-test-token ", "")
	require.NoError(t, err)
	require.Equal(t, "Bearer at-test-token", gotAuthorization)
	require.Equal(t, "application/json", gotAccept)
	require.Equal(t, openai.CodexCLIOriginator, gotOriginator)
	require.Equal(t, codexCLIUserAgent, gotUserAgent)
	require.Equal(t, OpenAIAuthModePersonalAccessToken, info.AuthMode)
	require.Equal(t, "at-test-token", info.AccessToken)
	require.Equal(t, "user@example.com", info.Email)
	require.Equal(t, "user-test", info.ChatGPTUserID)
	require.Equal(t, "account-test", info.ChatGPTAccountID)
	require.Equal(t, "team", info.PlanType)
	require.True(t, info.ChatGPTAccountFedRAMP)
	require.Zero(t, info.ExpiresAt)
	require.Empty(t, info.RefreshToken)
	require.Empty(t, info.IDToken)
}

func TestOpenAIOAuthServiceValidateCodexPersonalAccessTokenRejectsInvalidInput(t *testing.T) {
	service := NewOpenAIOAuthService(nil, nil)
	defer service.Stop()

	_, err := service.ValidateCodexPersonalAccessToken(context.Background(), "", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")

	_, err = service.ValidateCodexPersonalAccessToken(context.Background(), "eyJ.test-token", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "at-")
}

func TestOpenAIOAuthServiceValidateCodexPersonalAccessTokenRejectsUntrustedWhoami(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       string
	}{
		{
			name:       "unauthorized is redacted",
			statusCode: http.StatusUnauthorized,
			body:       `{"error":"upstream must not be trusted"}`,
			want:       "invalid or expired",
		},
		{
			name:       "missing user id",
			statusCode: http.StatusOK,
			body:       `{"email":"user@example.com","chatgpt_account_id":"account-test","chatgpt_plan_type":"team","chatgpt_account_is_fedramp":false}`,
			want:       "chatgpt_user_id",
		},
		{
			name:       "missing fedramp marker",
			statusCode: http.StatusOK,
			body:       `{"email":"user@example.com","chatgpt_user_id":"user-test","chatgpt_account_id":"account-test","chatgpt_plan_type":"team"}`,
			want:       "chatgpt_account_is_fedramp",
		},
		{
			name:       "invalid json",
			statusCode: http.StatusOK,
			body:       `{`,
			want:       "invalid Codex personal access token validation response",
		},
		{
			name:       "trailing json",
			statusCode: http.StatusOK,
			body:       `{"email":"user@example.com","chatgpt_user_id":"user-test","chatgpt_account_id":"account-test","chatgpt_plan_type":"team","chatgpt_account_is_fedramp":false} {}`,
			want:       "invalid Codex personal access token validation response",
		},
		{
			name:       "oversized response",
			statusCode: http.StatusOK,
			body:       `{"email":"user@example.com","chatgpt_user_id":"user-test","chatgpt_account_id":"account-test","chatgpt_plan_type":"team","chatgpt_account_is_fedramp":false,"padding":"` + strings.Repeat("x", 64*1024) + `"}`,
			want:       "response is too large",
		},
		{
			name:       "upstream error is redacted",
			statusCode: http.StatusBadGateway,
			body:       `{"error":"at-test-secret"}`,
			want:       "upstream status 502",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.statusCode)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()

			originalURL := openAICodexPATWhoamiURL
			openAICodexPATWhoamiURL = server.URL
			defer func() { openAICodexPATWhoamiURL = originalURL }()

			service := NewOpenAIOAuthService(nil, nil)
			defer service.Stop()
			_, err := service.ValidateCodexPersonalAccessToken(context.Background(), "at-test-secret", "")
			require.Error(t, err)
			require.Contains(t, err.Error(), test.want)
			require.NotContains(t, err.Error(), "at-test-secret")
		})
	}
}

func TestOpenAIOAuthServiceBuildAccountCredentialsForPersonalAccessToken(t *testing.T) {
	service := NewOpenAIOAuthService(nil, nil)
	defer service.Stop()

	credentials := service.BuildAccountCredentials(&OpenAITokenInfo{
		AccessToken:           "at-test-token",
		RefreshToken:          "must-not-survive",
		IDToken:               "must-not-survive",
		ExpiresAt:             123,
		ExpiresIn:             3600,
		ClientID:              "must-not-survive",
		AuthMode:              OpenAIAuthModePersonalAccessToken,
		Email:                 "user@example.com",
		ChatGPTAccountID:      "account-test",
		ChatGPTUserID:         "user-test",
		ChatGPTAccountFedRAMP: true,
		PlanType:              "team",
	})

	require.Equal(t, "at-test-token", credentials["access_token"])
	require.Equal(t, OpenAIAuthModePersonalAccessToken, credentials["auth_mode"])
	require.Equal(t, "personal_access_token", credentials["openai_auth_mode"])
	require.Equal(t, "Bearer", credentials["token_type"])
	require.Equal(t, true, credentials["chatgpt_account_is_fedramp"])
	for _, key := range openAIPersonalAccessTokenOAuthCredentialKeys {
		require.NotContains(t, credentials, key)
	}
}

func TestOpenAIOAuthServiceRefreshPersonalAccessTokenUsesWhoami(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer at-test-token", r.Header.Get("authorization"))
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{
			"email":"refreshed@example.com",
			"chatgpt_user_id":"user-refreshed",
			"chatgpt_account_id":"account-refreshed",
			"chatgpt_plan_type":"team",
			"chatgpt_account_is_fedramp":false
		}`))
	}))
	defer server.Close()

	originalURL := openAICodexPATWhoamiURL
	openAICodexPATWhoamiURL = server.URL
	defer func() { openAICodexPATWhoamiURL = originalURL }()

	service := NewOpenAIOAuthService(nil, nil)
	defer service.Stop()
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":      "at-test-token",
			"auth_mode":         OpenAIAuthModePersonalAccessToken,
			"refresh_token":     "stale-refresh-token",
			"id_token":          "stale-id-token",
			"expires_at":        "2026-01-01T00:00:00Z",
			"model_mapping":     map[string]any{"gpt-5": "gpt-5-codex"},
			"subscription_note": "preserve-me",
		},
	}

	info, err := service.RefreshAccountToken(context.Background(), account)
	require.NoError(t, err)
	require.True(t, info.personalAccessTokenValidated)
	credentials := MergeCredentials(account.Credentials, service.BuildAccountCredentials(info))
	credentials = NormalizeOpenAIPersonalAccessTokenCredentials(account, info, credentials)
	require.Equal(t, "refreshed@example.com", credentials["email"])
	require.Equal(t, map[string]any{"gpt-5": "gpt-5-codex"}, credentials["model_mapping"])
	require.Equal(t, "preserve-me", credentials["subscription_note"])
	for _, key := range openAIPersonalAccessTokenOAuthCredentialKeys {
		require.NotContains(t, credentials, key)
	}
}

func TestAccountServiceRejectsUnvalidatedPersonalAccessTokenWritePaths(t *testing.T) {
	service := &AccountService{}
	credentials := map[string]any{
		"access_token":               "at-test-token",
		"auth_mode":                  OpenAIAuthModePersonalAccessToken,
		"openai_auth_mode":           "personal_access_token",
		"token_type":                 "Bearer",
		"email":                      "user@example.com",
		"chatgpt_user_id":            "user-test",
		"chatgpt_account_id":         "account-test",
		"chatgpt_account_is_fedramp": false,
		"plan_type":                  "team",
	}
	request := CreateAccountRequest{
		Name:         "unvalidated PAT",
		Platform:     PlatformOpenAI,
		AccountLevel: AccountLevelTeam,
		Type:         AccountTypeOAuth,
		Credentials:  credentials,
	}

	_, err := service.CreateOwned(context.Background(), 1, request)
	require.ErrorIs(t, err, ErrOwnedPersonalAccessTokenValidationRequired)

	_, err = service.ImportOwnedWithResult(context.Background(), 1, request)
	require.ErrorIs(t, err, ErrOwnedPersonalAccessTokenValidationRequired)

	_, err = service.ImportOwnedValidatedPersonalAccessTokenWithResult(
		context.Background(),
		1,
		request,
		&OpenAITokenInfo{
			AccessToken:      "at-test-token",
			AuthMode:         OpenAIAuthModePersonalAccessToken,
			Email:            "user@example.com",
			ChatGPTUserID:    "user-test",
			ChatGPTAccountID: "account-test",
			PlanType:         "team",
		},
	)
	require.ErrorIs(t, err, ErrOwnedPersonalAccessTokenValidationRequired)
}

func TestNormalizeOpenAIPersonalAccessTokenCredentialsRemovesOAuthFields(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"auth_mode": OpenAIAuthModePersonalAccessToken,
		},
	}
	credentials := map[string]any{
		"access_token":                "at-test-token",
		"refresh_token":               "stale-refresh-token",
		"id_token":                    "stale-id-token",
		"expires_at":                  "2026-01-01T00:00:00Z",
		"expires_in":                  3600,
		"client_id":                   "stale-client",
		"model_mapping":               map[string]any{"gpt-5": "gpt-5-codex"},
		"chatgpt_account_is_fedramp":  true,
		"subscription_expires_at":     "2026-12-31T00:00:00Z",
		"openai_usage_channel_fields": []any{"custom"},
	}

	got := NormalizeOpenAIPersonalAccessTokenCredentials(account, nil, credentials)

	require.Equal(t, "at-test-token", got["access_token"])
	require.Equal(t, OpenAIAuthModePersonalAccessToken, got["auth_mode"])
	require.Equal(t, "personal_access_token", got["openai_auth_mode"])
	require.Equal(t, "Bearer", got["token_type"])
	for _, key := range openAIPersonalAccessTokenOAuthCredentialKeys {
		require.NotContains(t, got, key)
	}
	require.Equal(t, map[string]any{"gpt-5": "gpt-5-codex"}, got["model_mapping"])
	require.Equal(t, true, got["chatgpt_account_is_fedramp"])
	require.Equal(t, "2026-12-31T00:00:00Z", got["subscription_expires_at"])
	require.Equal(t, []any{"custom"}, got["openai_usage_channel_fields"])
}

func TestSetOpenAIChatGPTAccountHeadersHandlesFedRAMP(t *testing.T) {
	headers := http.Header{
		"X-Openai-Fedramp": []string{"stale"},
	}
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"chatgpt_account_id":         "account-test",
			"chatgpt_account_is_fedramp": true,
		},
	}

	setOpenAIChatGPTAccountHeaders(headers, account)
	require.Equal(t, "account-test", headers.Get("chatgpt-account-id"))
	require.Equal(t, "true", headers.Get("x-openai-fedramp"))

	account.Credentials["chatgpt_account_is_fedramp"] = false
	setOpenAIChatGPTAccountHeaders(headers, account)
	require.Empty(t, headers.Get("x-openai-fedramp"))
	require.False(t, strings.EqualFold(headers.Get("x-openai-fedramp"), "true"))
}
