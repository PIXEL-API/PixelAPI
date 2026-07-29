//go:build unit

package service

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

var accountTestCredentialRedirectStatuses = []int{
	http.StatusFound,
	http.StatusTemporaryRedirect,
	http.StatusPermanentRedirect,
}

func TestAccountTestService_AnthropicAPIKeyDisablesRedirectsAndRejectsRedirectBeforeBody(t *testing.T) {
	for _, statusCode := range accountTestCredentialRedirectStatuses {
		statusCode := statusCode
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			ctx, recorder := newTestContext()
			body := &accountTestTrackingBody{reader: strings.NewReader(`{"error":"redirected forbidden"}`)}
			upstream := &queuedHTTPUpstream{responses: []*http.Response{{
				StatusCode: statusCode,
				Header: http.Header{
					"Location": []string{"https://redirect.invalid/v1/messages"},
				},
				Body: body,
			}}}
			repo := &openAIAccountTestRepo{}
			svc := &AccountTestService{
				accountRepo:  repo,
				httpUpstream: upstream,
				cfg:          &config.Config{},
			}
			account := &Account{
				ID:          201,
				Platform:    PlatformAnthropic,
				Type:        AccountTypeAPIKey,
				Concurrency: 1,
				Credentials: map[string]any{
					"api_key": "anthropic-secret",
				},
			}

			err := svc.testClaudeAccountConnection(ctx, account, "claude-sonnet-4-5")

			require.Error(t, err)
			require.Len(t, upstream.requests, 1)
			require.True(t, HTTPUpstreamRedirectsDisabled(upstream.requests[0].Context()))
			require.Equal(t, "anthropic-secret", upstream.requests[0].Header.Get("x-api-key"))
			require.False(t, body.readCalled)
			require.Zero(t, repo.setErrorID)
			require.NotContains(t, recorder.Body.String(), "\"success\":true")
			require.NotContains(t, recorder.Body.String(), "redirected forbidden")
		})
	}
}

func TestAccountTestService_AnthropicVertexDisablesRedirectsAndRejectsRedirectBeforeBody(t *testing.T) {
	for _, statusCode := range accountTestCredentialRedirectStatuses {
		statusCode := statusCode
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			ctx, recorder := newTestContext()
			account := &Account{
				ID:          202,
				Platform:    PlatformAnthropic,
				Type:        AccountTypeServiceAccount,
				Concurrency: 1,
				Credentials: map[string]any{
					"service_account_json": map[string]any{
						"type":           "service_account",
						"project_id":     "vertex-project",
						"private_key_id": "test-key",
						"private_key":    "not-used-because-token-is-cached",
						"client_email":   "svc@vertex-project.iam.gserviceaccount.com",
					},
					"location": "us-central1",
				},
			}
			key, err := parseVertexServiceAccountKey(account)
			require.NoError(t, err)
			cache := newClaudeTokenCacheStub()
			cache.tokens[vertexServiceAccountCacheKey(account, key)] = "vertex-secret"

			body := &accountTestTrackingBody{reader: strings.NewReader(`{"error":"redirected forbidden"}`)}
			upstream := &queuedHTTPUpstream{responses: []*http.Response{{
				StatusCode: statusCode,
				Header: http.Header{
					"Location": []string{"https://redirect.invalid/rawPredict"},
				},
				Body: body,
			}}}
			repo := &openAIAccountTestRepo{}
			svc := &AccountTestService{
				accountRepo:         repo,
				claudeTokenProvider: NewClaudeTokenProvider(nil, cache, nil),
				httpUpstream:        upstream,
			}

			err = svc.testClaudeAccountConnection(ctx, account, "claude-sonnet-4-5-20250929")

			require.Error(t, err)
			require.Len(t, upstream.requests, 1)
			require.True(t, HTTPUpstreamRedirectsDisabled(upstream.requests[0].Context()))
			require.Equal(t, "Bearer vertex-secret", upstream.requests[0].Header.Get("Authorization"))
			require.False(t, body.readCalled)
			require.Zero(t, repo.setErrorID)
			require.NotContains(t, recorder.Body.String(), "\"success\":true")
			require.NotContains(t, recorder.Body.String(), "redirected forbidden")
		})
	}
}

func TestAccountTestService_BedrockAPIKeyDisablesRedirectsAndRejectsRedirectBeforeBody(t *testing.T) {
	for _, statusCode := range accountTestCredentialRedirectStatuses {
		statusCode := statusCode
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			ctx, recorder := newTestContext()
			body := &accountTestTrackingBody{reader: strings.NewReader(`{"content":[{"text":"redirected success"}]}`)}
			upstream := &queuedHTTPUpstream{responses: []*http.Response{{
				StatusCode: statusCode,
				Header: http.Header{
					"Location": []string{"https://redirect.invalid/model"},
				},
				Body: body,
			}}}
			svc := &AccountTestService{httpUpstream: upstream}
			account := &Account{
				ID:          203,
				Platform:    PlatformAnthropic,
				Type:        AccountTypeBedrock,
				Concurrency: 1,
				Credentials: map[string]any{
					"auth_mode":  "apikey",
					"api_key":    "bedrock-secret",
					"aws_region": "us-east-1",
				},
			}

			err := svc.testClaudeAccountConnection(ctx, account, "claude-sonnet-4-5")

			require.Error(t, err)
			require.Len(t, upstream.requests, 1)
			require.True(t, HTTPUpstreamRedirectsDisabled(upstream.requests[0].Context()))
			require.Equal(t, "Bearer bedrock-secret", upstream.requests[0].Header.Get("Authorization"))
			require.False(t, body.readCalled)
			require.NotContains(t, recorder.Body.String(), "\"success\":true")
			require.NotContains(t, recorder.Body.String(), "redirected success")
		})
	}
}
