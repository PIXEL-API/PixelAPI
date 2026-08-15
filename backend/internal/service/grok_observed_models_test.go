//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func TestExtractGrokModelIDsFromModelsBodyDeduplicatesAndSorts(t *testing.T) {
	models := extractGrokModelIDsFromModelsBody([]byte(`{"data":[{"id":"grok-4.6"},{"id":"grok-4.5"},{"id":"grok-4.6"}]}`))
	require.Equal(t, []string{"grok-4.5", "grok-4.6"}, models)
}

func TestSyncGrokObservedModelsUsesAccountProxyAndPersistsSnapshot(t *testing.T) {
	proxyID := int64(78)
	account := healthyGrokQuotaOAuthAccount(902)
	account.ProxyID = &proxyID
	account.Credentials["sub"] = "subject-902"
	repo := &grokQuotaAccountRepo{mockAccountRepoForPlatform: &mockAccountRepoForPlatform{
		accountsByID: map[int64]*Account{account.ID: account},
	}}
	proxyRepo := &grokQuotaProxyRepo{proxies: map[int64]*Proxy{
		proxyID: {ID: proxyID, Protocol: "http", Host: "proxy.test", Port: 3128},
	}}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"grok-4.6"},{"id":"grok-4.5"}]}`)),
	}}
	svc := &GrokQuotaService{
		accountRepo: repo, proxyRepo: proxyRepo, httpUpstream: upstream, cfg: &config.Config{},
	}

	require.NoError(t, svc.syncGrokObservedModels(context.Background(), account))
	require.Equal(t, xai.DefaultCLIBaseURL+"/models", upstream.lastReq.URL.String())
	require.True(t, HTTPUpstreamRedirectsDisabled(upstream.lastReq.Context()))
	require.Equal(t, "Bearer access-token", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, grokCLIVersion, upstream.lastReq.Header.Get("X-Grok-Client-Version"))
	require.Equal(t, xai.CLIClientIdentifier, upstream.lastReq.Header.Get("x-grok-client-identifier"))
	require.Equal(t, "http://proxy.test:3128", upstream.lastProxyURL)
	require.Contains(t, repo.updates[account.ID], grokObservedModelsExtraKey)
	require.NotContains(t, repo.updates[account.ID], "access_token")
}

func TestSyncGrokObservedModelsRejectsDisallowedOAuthRelay(t *testing.T) {
	account := healthyGrokQuotaOAuthAccount(903)
	account.Credentials["base_url"] = "https://blocked.example.test/v1"
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = true
	cfg.Security.URLAllowlist.UpstreamHosts = []string{"allowed.example.test"}
	upstream := &httpUpstreamRecorder{}
	svc := &GrokQuotaService{accountRepo: &grokQuotaAccountRepo{}, httpUpstream: upstream, cfg: cfg}

	err := svc.syncGrokObservedModels(context.Background(), account)
	require.ErrorContains(t, err, "base URL rejected by URL security policy")
	require.Nil(t, upstream.lastReq)
}
