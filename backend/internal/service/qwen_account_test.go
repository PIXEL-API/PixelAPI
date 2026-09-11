package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func newQwenTestAccount(mode, protocol string) *Account {
	return &Account{ID: 922, Platform: PlatformQwen, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "qwen-test-key", "account_mode": mode, "api_protocol": protocol},
		Extra:       map[string]any{},
	}
}

func TestQwenProtocolEndpointsAndBillingMode(t *testing.T) {
	for _, tt := range []struct{ mode, chat, anthropic string }{
		{AccountModePayG, "https://dashscope.aliyuncs.com/compatible-mode/v1", "https://dashscope.aliyuncs.com/apps/anthropic"},
		{AccountModeCoding, "https://coding.dashscope.aliyuncs.com/v1", "https://coding.dashscope.aliyuncs.com/apps/anthropic"},
	} {
		t.Run(tt.mode, func(t *testing.T) {
			for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolAdaptive} {
				account := newQwenTestAccount(tt.mode, protocol)
				require.NoError(t, validateQwenAccountConfiguration(account.Platform, account.Type, account.Credentials))
				require.Equal(t, tt.chat, account.GetOpenAIBaseURL())
				if protocol != APIProtocolChatCompletions {
					require.Equal(t, tt.anthropic, account.GetAnthropicProtocolBaseURL())
					require.Equal(t, tt.anthropic+"/v1/messages", buildOpenAIMessagesURL(account.GetAnthropicProtocolBaseURL()))
				}
				require.Equal(t, tt.chat+"/chat/completions", buildOpenAIChatCompletionsURL(account.GetOpenAIBaseURL()))
				require.False(t, account.UsesNativeCNResponses())
				if tt.mode == AccountModeCoding {
					require.Equal(t, PlatformQwen, account.GetCodingPlanProvider())
				} else {
					require.Empty(t, account.GetCodingPlanProvider())
				}
			}
		})
	}
}

func TestQwenRejectsIncompatibleConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name    string
		changes map[string]any
	}{
		{"missing mode", map[string]any{"account_mode": ""}},
		{"unknown mode", map[string]any{"account_mode": "subscription"}},
		{"native responses", map[string]any{"api_protocol": APIProtocolResponses}},
		{"missing key", map[string]any{"api_key": ""}},
		{"payg endpoint with coding credentials", map[string]any{"base_url": DefaultQwenBaseURL}},
		{"anthropic path with chat protocol", map[string]any{"base_url": DefaultQwenCodingAnthropicBaseURL}},
		{"invalid endpoint", map[string]any{"base_url": "not-a-url"}},
		{"adaptive payg endpoint", map[string]any{"api_protocol": APIProtocolAdaptive, "api_base_urls": map[string]any{APIProtocolAnthropic: DefaultQwenAnthropicBaseURL}}},
		{"invalid adaptive value", map[string]any{"api_protocol": APIProtocolAdaptive, "api_base_urls": map[string]any{APIProtocolResponses: map[string]any{"url": "bad"}}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			account := newQwenTestAccount(AccountModeCoding, APIProtocolChatCompletions)
			for key, value := range tt.changes {
				account.Credentials[key] = value
			}
			require.Error(t, validateQwenAccountConfiguration(account.Platform, account.Type, account.Credentials))
		})
	}
	account := newQwenTestAccount(AccountModePayG, APIProtocolChatCompletions)
	account.Credentials["base_url"] = DefaultQwenCodingBaseURL
	require.Error(t, validateQwenAccountConfiguration(account.Platform, account.Type, account.Credentials))
	require.Error(t, validateQwenAccountConfiguration(PlatformQwen, AccountTypeOAuth, account.Credentials))
}

func TestQwenCustomAdaptiveEndpointsStayOnConfiguredHosts(t *testing.T) {
	account := newQwenTestAccount(AccountModeCoding, APIProtocolAdaptive)
	account.Credentials["api_base_urls"] = map[string]any{
		APIProtocolChatCompletions: "https://chat.example.test/v1", APIProtocolAnthropic: "https://messages.example.test/anthropic",
	}
	require.NoError(t, validateQwenAccountConfiguration(account.Platform, account.Type, account.Credentials))
	require.Equal(t, "https://chat.example.test/v1", account.GetOpenAIBaseURL())
	require.Equal(t, "https://messages.example.test/anthropic", account.GetAnthropicProtocolBaseURL())
}

func TestQwenAnthropicModelDiscoveryIsExplicitlyUnsupported(t *testing.T) {
	for _, settings := range [][2]string{
		{AccountModeCoding, APIProtocolChatCompletions}, {AccountModeCoding, APIProtocolAnthropic},
		{AccountModeCoding, APIProtocolAdaptive}, {AccountModePayG, APIProtocolAnthropic},
	} {
		account := newQwenTestAccount(settings[0], settings[1])
		request, err := (&AccountTestService{}).buildUpstreamModelsRequest(context.Background(), account)
		require.Nil(t, request)
		var syncError *UpstreamModelSyncError
		require.ErrorAs(t, err, &syncError)
		require.Equal(t, UpstreamModelSyncErrorUnsupported, syncError.Kind)
	}
}

func TestCNAPIKeyImportPreservesModeAndRejectsOtherPlatformOAuth(t *testing.T) {
	sources, failures := ParseCNProviderCredentialImportContents(PlatformQwen, []string{
		"sk-test-one\r\nsk-test-two",
		`{"name":"千问套餐","notes":"保留协议","platform":"qwen","type":"apikey","credentials":{"api_key":"sk-test-three","account_mode":"coding","api_protocol":"anthropic"}}`,
		`{"platform":"openai","type":"oauth","credentials":{"access_token":"foreign-token"}}`,
		`{"type":"oauth","credentials":{"access_token":"foreign-token"}}`,
	})
	require.Len(t, sources, 3)
	require.Len(t, failures, 2)
	for _, source := range sources {
		require.Equal(t, AccountCredentialImportKindCNAPIKey, source.Kind)
		require.Equal(t, PlatformQwen, source.Platform)
		require.Empty(t, source.Token)
	}
	require.Equal(t, "千问套餐", sources[2].Name)
	require.Equal(t, "保留协议", *sources[2].Notes)
	require.Equal(t, AccountModeCoding, sources[2].Credentials["account_mode"])
	require.Equal(t, APIProtocolAnthropic, sources[2].Credentials["api_protocol"])
	require.NotContains(t, sources[2].Credentials, "access_token")
}

func TestCNAPIKeyImportAcceptsAccountExportEnvelope(t *testing.T) {
	sources, failures := ParseCNProviderCredentialImportContents(PlatformQwen, []string{
		`{"accounts":[{"platform":"qwen","credentials":{"api_key":"sk-test-one","account_mode":"coding"}},{"api_key":"sk-test-two"},{"api_key":1234}]}`,
	})
	require.Len(t, sources, 2)
	require.Len(t, failures, 1)
	require.Equal(t, 3, failures[0].Index)
	require.Equal(t, "coding", sources[0].Credentials["account_mode"])
}
