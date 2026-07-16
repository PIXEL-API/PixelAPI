package repository

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func http2KeepAliveTestPoolSettings() poolSettings {
	return poolSettings{
		maxIdleConns:          10,
		maxIdleConnsPerHost:   5,
		maxConnsPerHost:       10,
		idleConnTimeout:       90 * time.Second,
		responseHeaderTimeout: time.Minute,
	}
}

func TestEnableOpenAIHTTP2KeepAliveEnablesPingHealthCheck(t *testing.T) {
	transport := &http.Transport{}
	h2, err := enableOpenAIHTTP2KeepAlive(transport)
	require.NoError(t, err)
	require.NotNil(t, h2)
	require.Equal(t, openAIHTTP2ReadIdleTimeout, h2.ReadIdleTimeout)
	require.Equal(t, openAIHTTP2PingTimeout, h2.PingTimeout)
	require.NotNil(t, transport.TLSNextProto["h2"])
}

func TestBuildUpstreamTransportOpenAIH2EnablesPing(t *testing.T) {
	transport, err := buildUpstreamTransport(http2KeepAliveTestPoolSettings(), nil, upstreamProtocolModeOpenAIH2)
	require.NoError(t, err)
	require.True(t, transport.ForceAttemptHTTP2)
	require.NotNil(t, transport.TLSNextProto["h2"])
}

func TestBuildUpstreamTransportNonOpenAIH2DoesNotEagerlyConfigurePing(t *testing.T) {
	transport, err := buildUpstreamTransport(http2KeepAliveTestPoolSettings(), nil, upstreamProtocolModeDefault)
	require.NoError(t, err)
	require.Nil(t, transport.TLSNextProto["h2"])
}

func TestBuildUpstreamTransportOpenAIH2WithHTTPProxy(t *testing.T) {
	proxyURL, err := url.Parse("http://127.0.0.1:8080")
	require.NoError(t, err)
	transport, err := buildUpstreamTransport(http2KeepAliveTestPoolSettings(), proxyURL, upstreamProtocolModeOpenAIH2)
	require.NoError(t, err)
	require.True(t, transport.ForceAttemptHTTP2)
	require.NotNil(t, transport.TLSNextProto["h2"])
	require.NotNil(t, transport.Proxy)
}

func TestOpenAIHTTP2ProfilePoolAndFallbackPolicy(t *testing.T) {
	svc := &httpUpstreamService{cfg: &config.Config{Gateway: config.GatewayConfig{
		ResponseHeaderTimeout:       600,
		OpenAIResponseHeaderTimeout: 0,
		OpenAIHTTP2: config.GatewayOpenAIHTTP2Config{
			Enabled:                   true,
			AllowProxyFallbackToHTTP1: true,
			FallbackErrorThreshold:    1,
			FallbackWindowSeconds:     60,
			FallbackTTLSeconds:        600,
		},
	}}}

	settings := svc.applyProfilePoolSettings(defaultPoolSettings(svc.cfg), service.HTTPUpstreamProfileOpenAI)
	require.Zero(t, settings.responseHeaderTimeout)
	proxyURL, err := url.Parse("http://127.0.0.1:8080")
	require.NoError(t, err)
	require.Equal(t, upstreamProtocolModeOpenAIH2, svc.resolveProtocolMode(service.HTTPUpstreamProfileOpenAI, proxyURL.String(), proxyURL))

	svc.recordOpenAIHTTP2Failure(service.HTTPUpstreamProfileOpenAI, upstreamProtocolModeOpenAIH2, proxyURL.String(), errors.New("http2: protocol error"))
	require.True(t, svc.isOpenAIHTTP2FallbackActive(proxyURL.String()))
	require.Equal(t, upstreamProtocolModeOpenAIH1Fallback, svc.resolveProtocolMode(service.HTTPUpstreamProfileOpenAI, proxyURL.String(), proxyURL))
}

func TestOpenAIHTTP2TimeoutDoesNotActivateProxyFallback(t *testing.T) {
	svc := &httpUpstreamService{cfg: &config.Config{Gateway: config.GatewayConfig{OpenAIHTTP2: config.GatewayOpenAIHTTP2Config{
		Enabled: true, AllowProxyFallbackToHTTP1: true, FallbackErrorThreshold: 1,
	}}}}
	proxyURL := "http://127.0.0.1:8080"
	svc.recordOpenAIHTTP2Failure(service.HTTPUpstreamProfileOpenAI, upstreamProtocolModeOpenAIH2, proxyURL, errors.New("http2: timeout awaiting response headers"))
	require.False(t, svc.isOpenAIHTTP2FallbackActive(proxyURL))
}

func TestApplyGrokCLIProxyHeaders(t *testing.T) {
	t.Run("stable default", func(t *testing.T) {
		t.Setenv(grokCLIVersionOverride, "")
		req, err := http.NewRequest(http.MethodGet, "https://cli-chat-proxy.grok.com/v1/responses", nil)
		require.NoError(t, err)
		applyGrokCLIProxyHeaders(req)
		require.Equal(t, "xai-grok-cli", req.Header.Get("X-XAI-Token-Auth"))
		require.Equal(t, grokCLIStableVersion, req.Header.Get("x-grok-client-version"))
		require.Equal(t, "xai-grok-workspace/"+grokCLIStableVersion, req.Header.Get("User-Agent"))
	})

	t.Run("newer override", func(t *testing.T) {
		t.Setenv(grokCLIVersionOverride, "0.2.95-alpha.1")
		req, err := http.NewRequest(http.MethodGet, "https://cli-chat-proxy.grok.com/v1/responses", nil)
		require.NoError(t, err)
		applyGrokCLIProxyHeaders(req)
		require.Equal(t, "0.2.95-alpha.1", req.Header.Get("x-grok-client-version"))
	})

	t.Run("direct xai untouched", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, "https://api.x.ai/v1/responses", nil)
		require.NoError(t, err)
		applyGrokCLIProxyHeaders(req)
		require.Empty(t, req.Header.Get("x-grok-client-version"))
	})
}
