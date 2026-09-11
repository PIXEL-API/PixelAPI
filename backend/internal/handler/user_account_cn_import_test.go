package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCNImportPreservesSourceModeAndProtocolWithoutMixingFormEndpoints(t *testing.T) {
	source := map[string]any{"api_key": "test-key", "account_mode": "coding", "api_protocol": "anthropic"}
	defaults := importUserAccountCredentialsRequest{AccountMode: "payg", APIProtocol: "chat_completions", BaseURL: service.DefaultQwenBaseURL,
		APIBaseURLs: map[string]string{"chat_completions": service.DefaultQwenBaseURL}}
	credentials := cnCredentialImportCredentials(source, defaults)
	require.Equal(t, "coding", credentials["account_mode"])
	require.Equal(t, "anthropic", credentials["api_protocol"])
	require.NotContains(t, credentials, "base_url")
	require.NotContains(t, credentials, "api_base_urls")
	account := &service.Account{Platform: service.PlatformQwen, Type: service.AccountTypeAPIKey, Credentials: credentials}
	require.Equal(t, service.DefaultQwenCodingAnthropicBaseURL, account.GetAnthropicProtocolBaseURL())
	credentials["api_key"] = "changed"
	require.Equal(t, "test-key", source["api_key"], "defaults must not mutate parsed source")
}

func TestCNImportRawKeyUsesFormSettings(t *testing.T) {
	credentials := cnCredentialImportCredentials(map[string]any{"api_key": "test-key"}, importUserAccountCredentialsRequest{
		AccountMode: "coding", APIProtocol: "adaptive", APIBaseURLs: map[string]string{
			"chat_completions": service.DefaultQwenCodingBaseURL, "anthropic": service.DefaultQwenCodingAnthropicBaseURL,
		},
	})
	require.Equal(t, "coding", credentials["account_mode"])
	require.Equal(t, "adaptive", credentials["api_protocol"])
	account := &service.Account{Platform: service.PlatformQwen, Type: service.AccountTypeAPIKey, Credentials: credentials}
	require.Equal(t, service.DefaultQwenCodingBaseURL, account.GetOpenAIBaseURL())
	require.Equal(t, service.DefaultQwenCodingAnthropicBaseURL, account.GetAnthropicProtocolBaseURL())
	require.False(t, credentialImportSourceIsOpenAI(service.AccountCredentialImportSource{Platform: service.PlatformQwen, Kind: service.AccountCredentialImportKindCNAPIKey}))
	require.True(t, credentialImportSourceIsOpenAI(service.AccountCredentialImportSource{Kind: service.AccountCredentialImportKindOpenAIRefreshToken}))
}
