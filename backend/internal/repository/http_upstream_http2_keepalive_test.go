package repository

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/semver"
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

// grokCLIPinParts 拆出当前 pin（grokCLIStableVersion，派生自 xai.CLIClientVersion）
// 的 major/minor/patch，供下面的相对构造使用。
func grokCLIPinParts(t *testing.T) (int, int, int) {
	t.Helper()
	canonical := semver.Canonical("v" + grokCLIStableVersion)
	require.NotEmpty(t, canonical, "pinned Grok CLI version must be valid semver")
	fields := strings.Split(strings.TrimPrefix(canonical, "v"), ".")
	require.Len(t, fields, 3)
	nums := make([]int, 0, 3)
	for _, field := range fields {
		n, err := strconv.Atoi(field)
		require.NoError(t, err)
		nums = append(nums, n)
	}
	return nums[0], nums[1], nums[2]
}

// grokCLIVersionAbovePin / grokCLIVersionBelowPin 相对当前 pin 生成版本号。
// pin 本身就是 operator override 的下限，写死字面量会在下次 bump 时静默失效：
// 老写法里的 0.2.95-alpha.1 在 pin 抬到 0.2.118 后直接跌破下限被丢弃，
// 那条用例就不再验证"接受 override"，只是又测了一遍回落。
func grokCLIVersionAbovePin(t *testing.T) string {
	t.Helper()
	major, minor, patch := grokCLIPinParts(t)
	return fmt.Sprintf("%d.%d.%d", major, minor, patch+1)
}

func grokCLIVersionBelowPin(t *testing.T) string {
	t.Helper()
	major, minor, patch := grokCLIPinParts(t)
	require.Greater(t, patch, 0, "pinned version needs patch > 0 to derive a lower one")
	return fmt.Sprintf("%d.%d.%d", major, minor, patch-1)
}

func newGrokCLIProxyRequest(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "https://cli-chat-proxy.grok.com/v1/responses", nil)
	require.NoError(t, err)
	return req
}

// requireGrokCLIFallsBackToPin 断言 override 被拒后回落到 pin。
func requireGrokCLIFallsBackToPin(t *testing.T, req *http.Request) {
	t.Helper()
	require.Equal(t, grokCLIStableVersion, req.Header.Get("x-grok-client-version"))
	require.Equal(t, xai.CLIClientIdentifier, req.Header.Get("x-grok-client-identifier"))
	require.Equal(t, xai.CLIUserAgentForVersion(grokCLIStableVersion), req.Header.Get("User-Agent"))
}

func TestApplyGrokCLIProxyHeaders(t *testing.T) {
	t.Run("stable default", func(t *testing.T) {
		t.Setenv(grokCLIVersionOverride, "")
		req := newGrokCLIProxyRequest(t)
		applyGrokCLIProxyHeaders(req)
		require.Equal(t, xai.CLITokenAuthValue, req.Header.Get("X-XAI-Token-Auth"))
		require.Equal(t, grokCLIStableVersion, req.Header.Get("x-grok-client-version"))
		require.Equal(t, xai.CLIClientIdentifier, req.Header.Get("x-grok-client-identifier"))
		require.Equal(t, xai.CLIUserAgentForVersion(grokCLIStableVersion), req.Header.Get("User-Agent"))
	})

	t.Run("newer override", func(t *testing.T) {
		override := grokCLIVersionAbovePin(t) + "-alpha.1"
		// 守住这条用例的前提：override 必须严格高于当前 pin，否则会被
		// isSupportedGrokCLIVersion 丢弃，用例退化成"又测了一遍回落"。
		require.True(t, semver.Compare("v"+override, "v"+grokCLIStableVersion) > 0,
			"override %s must outrank the pin %s", override, grokCLIStableVersion)
		t.Setenv(grokCLIVersionOverride, override)
		req := newGrokCLIProxyRequest(t)
		applyGrokCLIProxyHeaders(req)
		require.Equal(t, override, req.Header.Get("x-grok-client-version"))
		require.Equal(t, xai.CLIClientIdentifier, req.Header.Get("x-grok-client-identifier"))
		require.Equal(t, xai.CLIUserAgentForVersion(override), req.Header.Get("User-Agent"))
	})

	t.Run("override below the pin is rejected", func(t *testing.T) {
		older := grokCLIVersionBelowPin(t)
		require.True(t, semver.Compare("v"+older, "v"+grokCLIStableVersion) < 0,
			"override %s must sit below the pin %s", older, grokCLIStableVersion)
		t.Setenv(grokCLIVersionOverride, older)
		req := newGrokCLIProxyRequest(t)
		applyGrokCLIProxyHeaders(req)
		requireGrokCLIFallsBackToPin(t, req)
	})

	t.Run("prerelease at the pin is rejected", func(t *testing.T) {
		// semver 语义：prerelease 排在同号 release 之前，所以"等于下限的 prerelease"
		// 仍在下限之下，不能拿来顶替 pin。
		t.Setenv(grokCLIVersionOverride, grokCLIStableVersion+"-beta.1")
		req := newGrokCLIProxyRequest(t)
		applyGrokCLIProxyHeaders(req)
		requireGrokCLIFallsBackToPin(t, req)
	})

	// 每一项的数值都高于当前 pin，所以被拒只可能是格式问题，不会是"版本太旧"。
	// 注意 isSupportedGrokCLIVersion 还要求 semver.Canonical(c) == c，
	// 因此前导零 / 缺段 / +build 元数据这类非规范写法同样会被拒。
	major, minor, patch := grokCLIPinParts(t)
	newer := fmt.Sprintf("%d.%d.%d", major, minor, patch+1)
	for _, version := range []string{
		fmt.Sprintf("%d.%d.0%d", major, minor, patch+1), // patch 段带前导零
		newer + "-alpha..1",                             // prerelease 里有空标识符
		fmt.Sprintf("%d.%d", major, minor+1),            // 缺 patch 段
		fmt.Sprintf("%d", major+1),                      // 只有 major
		newer + "+build.1",                              // 带 build 元数据
	} {
		t.Run("rejects invalid semver "+version, func(t *testing.T) {
			t.Setenv(grokCLIVersionOverride, version)
			req := newGrokCLIProxyRequest(t)
			applyGrokCLIProxyHeaders(req)
			requireGrokCLIFallsBackToPin(t, req)
		})
	}

	t.Run("direct xai untouched", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, "https://api.x.ai/v1/responses", nil)
		require.NoError(t, err)
		applyGrokCLIProxyHeaders(req)
		require.Empty(t, req.Header.Get("x-grok-client-version"))
		require.Empty(t, req.Header.Get("x-grok-client-identifier"))
	})
}
