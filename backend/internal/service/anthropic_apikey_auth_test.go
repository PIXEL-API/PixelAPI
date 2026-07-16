package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAnthropicAPIKeyAuthScheme_DefaultsToXAPIKey(t *testing.T) {
	account := &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Extra:    map[string]any{},
	}

	header := http.Header{}
	setAnthropicAPIKeyAuthHeader(header, account, "sk-test")

	require.Equal(t, AnthropicAPIKeyAuthSchemeXAPIKey, account.GetAnthropicAPIKeyAuthScheme())
	require.Equal(t, "sk-test", header.Get("x-api-key"))
	require.Empty(t, header.Get("authorization"))
}

func TestAnthropicAPIKeyAuthScheme_AuthorizationBearer(t *testing.T) {
	account := &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Extra: map[string]any{
			anthropicAPIKeyAuthSchemeExtraKey: AnthropicAPIKeyAuthSchemeAuthorizationBearer,
		},
	}

	header := http.Header{}
	setAnthropicAPIKeyAuthHeader(header, account, "sk-test")

	require.Equal(t, AnthropicAPIKeyAuthSchemeAuthorizationBearer, account.GetAnthropicAPIKeyAuthScheme())
	require.Equal(t, "Bearer sk-test", header.Get("authorization"))
	require.Empty(t, header.Get("x-api-key"))
}

func TestAnthropicAPIKeyAuthScheme_AppliesToMessagesAndCountTokens(t *testing.T) {
	account := &Account{
		ID:       1,
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Extra: map[string]any{
			anthropicAPIKeyAuthSchemeExtraKey: AnthropicAPIKeyAuthSchemeAuthorizationBearer,
		},
	}
	svc := &GatewayService{
		cfg: &config.Config{
			Security: config.SecurityConfig{
				URLAllowlist: config.URLAllowlistConfig{
					Enabled:           false,
					AllowInsecureHTTP: true,
				},
			},
		},
	}

	messagesReq, err := svc.buildUpstreamRequest(context.Background(), nil, account, []byte(`{}`), "sk-test", "api_key", "claude-3-5-sonnet-latest", false, false)
	require.NoError(t, err)
	require.Equal(t, "Bearer sk-test", messagesReq.Header.Get("authorization"))
	require.Empty(t, messagesReq.Header.Get("x-api-key"))

	countTokensReq, err := svc.buildCountTokensRequest(context.Background(), nil, account, []byte(`{}`), "sk-test", "api_key", "claude-3-5-sonnet-latest", false)
	require.NoError(t, err)
	require.Equal(t, "Bearer sk-test", countTokensReq.Header.Get("authorization"))
	require.Empty(t, countTokensReq.Header.Get("x-api-key"))
}
