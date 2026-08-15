package xai

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeSSOTokenRejectsOversizedSecret(t *testing.T) {
	require.Empty(t, NormalizeSSOToken(strings.Repeat("x", ssoMaxTokenLength+1)))
}

func TestSSODeviceCookieJarHonorsDomainAndPath(t *testing.T) {
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	flow := &ssoDeviceFlow{cookieJar: jar}
	accountsURL, err := url.Parse("https://accounts.x.ai/")
	require.NoError(t, err)
	authURL, err := url.Parse("https://auth.x.ai/oauth2/device/verify")
	require.NoError(t, err)

	flow.captureCookies(accountsURL, ssoDeviceTestResponse(http.Header{"Set-Cookie": {
		"host-only=accounts; Path=/",
		"shared=all-xai; Domain=x.ai; Path=/",
		"narrow=oauth-only; Domain=x.ai; Path=/oauth2",
	}}))

	authCookies := flow.cookieHeader(authURL)
	require.NotContains(t, authCookies, "host-only=accounts")
	require.Contains(t, authCookies, "shared=all-xai")
	require.Contains(t, authCookies, "narrow=oauth-only")

	accountsCookies := flow.cookieHeader(accountsURL)
	require.Contains(t, accountsCookies, "host-only=accounts")
	require.Contains(t, accountsCookies, "shared=all-xai")
	require.NotContains(t, accountsCookies, "narrow=oauth-only")
}

func TestSeedSSOCookiesDoesNotLeakToUnrelatedHost(t *testing.T) {
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	seedSSOCookies(jar, "sso-secret")

	unrelatedURL, err := url.Parse("https://example.com/")
	require.NoError(t, err)
	require.Empty(t, jar.Cookies(unrelatedURL))
}

func ssoDeviceTestResponse(header http.Header) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader("")),
	}
}
