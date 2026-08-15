package xai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type grokPasswordHTTPClientFunc func(*http.Request) (*http.Response, error)

func (f grokPasswordHTTPClientFunc) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestLoginWithPasswordConvertsOnlyToEphemeralSSO(t *testing.T) {
	captchaCalls := 0
	captchaClient := grokPasswordHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
		captchaCalls++
		body := `{"errorId":0,"taskId":123}`
		if strings.HasSuffix(request.URL.Path, "/getTaskResult") {
			body = `{"errorId":0,"status":"ready","solution":{"token":"turnstile-token"}}`
		}
		return passwordLoginTestResponse(http.StatusOK, nil, body), nil
	})
	passwordClient := grokPasswordHTTPClientFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/api/rpc":
			data, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			require.Contains(t, string(data), `"clearTextPassword":"password-secret"`)
			return passwordLoginTestResponse(http.StatusOK, nil, `{"cookieSetterUrl":"https://accounts.x.ai/set-cookie?session=ephemeral"}`), nil
		case "/set-cookie":
			return passwordLoginTestResponse(http.StatusFound, http.Header{
				"Set-Cookie": {"sso=sso-secret; Secure; HttpOnly; Path=/"},
			}, ""), nil
		default:
			t.Fatalf("unexpected password request: %s", request.URL.String())
			return nil, nil
		}
	})

	result, err := LoginWithPassword(context.Background(), "admin@example.com", "password-secret", &GrokPasswordLoginOptions{
		HTTPClient:        passwordClient,
		CaptchaHTTPClient: captchaClient,
		CaptchaClientKey:  "captcha-key",
		CaptchaTimeout:    time.Second,
		CaptchaPollDelay:  time.Millisecond,
		Sleep:             func(context.Context, time.Duration) error { return nil },
	})

	require.NoError(t, err)
	require.Equal(t, "admin@example.com", result.Email)
	require.Equal(t, "sso-secret", result.SSOToken)
	require.Equal(t, 2, captchaCalls)
	require.NotContains(t, result.String(), "sso-secret")
}

func TestLoginWithPasswordFailsClosedWithoutInjectedClients(t *testing.T) {
	result, err := LoginWithPassword(context.Background(), "admin@example.com", "password-secret", nil)
	require.Nil(t, result)
	require.ErrorIs(t, err, ErrGrokCaptchaUnavailable)
	require.NotContains(t, err.Error(), "password-secret")
}

func TestCreateGrokPasswordSessionDoesNotExposeResponseSecrets(t *testing.T) {
	client := grokPasswordHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		return passwordLoginTestResponse(http.StatusUnauthorized, nil, `{"password":"password-secret","sso":"sso-secret"}`), nil
	})

	_, err := createGrokPasswordSession(context.Background(), client, "admin@example.com", "password-secret", "captcha")
	require.ErrorIs(t, err, ErrGrokPasswordLoginFailed)
	require.NotContains(t, err.Error(), "password-secret")
	require.NotContains(t, err.Error(), "sso-secret")
}

func TestValidateGrokCookieSetterURLRejectsCredentialAndForeignHost(t *testing.T) {
	for _, rawURL := range []string{
		"https://example.com/set-cookie",
		"https://user:secret@accounts.x.ai/set-cookie",
		"http://accounts.x.ai/set-cookie",
		"https://accounts.x.ai:8443/set-cookie",
	} {
		_, err := validateGrokCookieSetterURL(rawURL)
		require.Error(t, err, rawURL)
		require.NotContains(t, err.Error(), "secret")
	}
}

func passwordLoginTestResponse(status int, header http.Header, body string) *http.Response {
	if header == nil {
		header = make(http.Header)
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
