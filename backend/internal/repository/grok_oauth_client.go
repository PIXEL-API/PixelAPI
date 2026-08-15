package repository

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	sharedhttp "github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
	"github.com/imroc/req/v3"
)

type grokOAuthClient struct {
	tokenURL string
}

func NewGrokOAuthClient() (service.GrokOAuthClient, error) {
	tokenURL, err := xai.ValidatedTokenURL()
	if err != nil || strings.TrimSpace(tokenURL) == "" {
		return nil, errors.New("xAI OAuth token endpoint configuration is invalid")
	}
	return &grokOAuthClient{tokenURL: tokenURL}, nil
}

func (c *grokOAuthClient) ExchangeCode(ctx context.Context, code, codeVerifier, redirectURI, proxyURL, clientID string) (*xai.TokenResponse, error) {
	client, err := createGrokReqClient(proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "GROK_OAUTH_CLIENT_INIT_FAILED", "create HTTP client: %v", err)
	}

	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = xai.EffectiveClientID()
	}

	formData := url.Values{}
	formData.Set("grant_type", "authorization_code")
	formData.Set("client_id", clientID)
	formData.Set("code", code)
	formData.Set("redirect_uri", xai.EffectiveRedirectURI(redirectURI))
	formData.Set("code_verifier", codeVerifier)

	var tokenResp xai.TokenResponse
	resp, err := client.R().
		SetContext(ctx).
		SetHeader("User-Agent", "sub2api-grok-oauth/1.0").
		SetFormDataFromValues(formData).
		SetSuccessResult(&tokenResp).
		Post(c.tokenURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "GROK_OAUTH_REQUEST_FAILED", "request failed: %v", err)
	}
	if !resp.IsSuccessState() {
		return nil, grokOAuthStatusError("GROK_OAUTH_TOKEN_EXCHANGE_FAILED", "token exchange failed", resp)
	}
	if strings.TrimSpace(tokenResp.AccessToken) == "" {
		return nil, infraerrors.New(http.StatusBadGateway, "GROK_OAUTH_TOKEN_RESPONSE_INVALID", "token exchange response did not include access_token")
	}
	return &tokenResp, nil
}

func (c *grokOAuthClient) RefreshToken(ctx context.Context, refreshToken, proxyURL, clientID string) (*xai.TokenResponse, error) {
	client, err := createGrokReqClient(proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "GROK_OAUTH_CLIENT_INIT_FAILED", "create HTTP client: %v", err)
	}

	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = xai.EffectiveClientID()
	}

	formData := url.Values{}
	formData.Set("grant_type", "refresh_token")
	formData.Set("client_id", clientID)
	formData.Set("refresh_token", refreshToken)

	var tokenResp xai.TokenResponse
	resp, err := client.R().
		SetContext(ctx).
		SetHeader("User-Agent", "sub2api-grok-oauth/1.0").
		SetFormDataFromValues(formData).
		SetSuccessResult(&tokenResp).
		Post(c.tokenURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "GROK_OAUTH_REQUEST_FAILED", "request failed: %v", err)
	}
	if !resp.IsSuccessState() {
		return nil, grokOAuthStatusError("GROK_OAUTH_TOKEN_REFRESH_FAILED", "token refresh failed", resp)
	}
	if strings.TrimSpace(tokenResp.AccessToken) == "" {
		return nil, infraerrors.New(http.StatusBadGateway, "GROK_OAUTH_TOKEN_RESPONSE_INVALID", "token refresh response did not include access_token")
	}
	return &tokenResp, nil
}

func (c *grokOAuthClient) ConvertSSOToBuild(ctx context.Context, ssoToken, proxyURL string) (*xai.TokenResponse, error) {
	client, err := createGrokSSOHTTPClient(proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "GROK_SSO_CLIENT_INIT_FAILED", "create HTTP client: %v", err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, xai.SSOConversionTimeout)
	defer cancel()
	tokenResp, err := xai.ConvertSSOToBuild(requestCtx, ssoToken, &xai.SSODeviceOptions{HTTPClient: client})
	if err != nil {
		return nil, grokSSOConversionError(err)
	}
	return tokenResp, nil
}

// LoginWithPassword performs password login through the account's selected
// proxy and returns only an ephemeral SSO value. Captcha solving uses a
// separate client because it targets YesCaptcha rather than the xAI account.
func (c *grokOAuthClient) LoginWithPassword(ctx context.Context, email, password, proxyURL string) (*xai.GrokPasswordLoginResult, error) {
	clientKey := strings.TrimSpace(os.Getenv("YESCAPTCHA_CLIENT_KEY"))
	if clientKey == "" {
		clientKey = strings.TrimSpace(os.Getenv("YESCAPTCHA_API_KEY"))
	}
	if clientKey == "" {
		return nil, infraerrors.New(
			http.StatusBadRequest,
			"GROK_OAUTH_CAPTCHA_KEY_REQUIRED",
			"YesCaptcha client key is required for Grok password authorization",
		)
	}

	accountClient, err := createIsolatedGrokHTTPClient(proxyURL, 120*time.Second)
	if err != nil {
		return nil, infraerrors.New(
			http.StatusBadGateway,
			"GROK_OAUTH_CLIENT_INIT_FAILED",
			"failed to initialize the Grok password authorization client",
		)
	}
	captchaClient, err := createIsolatedGrokHTTPClient("", 120*time.Second)
	if err != nil {
		return nil, infraerrors.New(
			http.StatusBadGateway,
			"GROK_OAUTH_CAPTCHA_CLIENT_INIT_FAILED",
			"failed to initialize the captcha client",
		)
	}

	result, err := xai.LoginWithPassword(ctx, email, password, &xai.GrokPasswordLoginOptions{
		HTTPClient:        accountClient,
		CaptchaHTTPClient: captchaClient,
		CaptchaClientKey:  clientKey,
	})
	if err == nil {
		return result, nil
	}
	if errors.Is(err, xai.ErrGrokPasswordInputInvalid) {
		return nil, infraerrors.New(
			http.StatusBadRequest,
			"GROK_OAUTH_PASSWORD_INPUT_INVALID",
			"Grok password authorization input is invalid",
		)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return nil, infraerrors.New(
			http.StatusGatewayTimeout,
			"GROK_OAUTH_CAPTCHA_TIMEOUT",
			"Grok password authorization captcha solving timed out",
		)
	}
	if errors.Is(err, xai.ErrGrokCaptchaUnavailable) {
		return nil, infraerrors.New(
			http.StatusBadGateway,
			"GROK_OAUTH_CAPTCHA_FAILED",
			"Grok password authorization captcha solving failed",
		)
	}
	return nil, infraerrors.New(
		http.StatusBadGateway,
		"GROK_OAUTH_PASSWORD_LOGIN_FAILED",
		"Grok password authorization failed",
	)
}

func createGrokReqClient(proxyURL string) (*req.Client, error) {
	return getSharedReqClient(reqClientOptions{
		ProxyURL: proxyURL,
		Timeout:  60 * time.Second,
	})
}

func createGrokSSOHTTPClient(proxyURL string) (*http.Client, error) {
	client, err := sharedhttp.GetClient(sharedhttp.Options{
		ProxyURL:              proxyURL,
		Timeout:               xai.SSOConversionTimeout,
		ResponseHeaderTimeout: 30 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	clone := *client
	clone.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	clone.Jar = nil
	return &clone, nil
}

func createIsolatedGrokHTTPClient(proxyURL string, timeout time.Duration) (*http.Client, error) {
	client, err := sharedhttp.GetClient(sharedhttp.Options{
		ProxyURL:              proxyURL,
		Timeout:               timeout,
		ResponseHeaderTimeout: 30 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	clone := *client
	clone.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	clone.Jar = nil
	return &clone, nil
}

func grokSSOConversionError(err error) error {
	if errors.Is(err, xai.ErrSSOUnauthorized) {
		return infraerrors.New(http.StatusUnauthorized, "GROK_SSO_UNAUTHORIZED", "Grok Web SSO cookie is invalid or expired")
	}
	if errors.Is(err, xai.ErrSSOAuthorizationDenied) {
		return infraerrors.New(http.StatusForbidden, "GROK_SSO_AUTHORIZATION_DENIED", "xAI device authorization was denied or expired")
	}
	var statusErr xai.SSOHTTPError
	if errors.As(err, &statusErr) {
		statusCode := http.StatusBadGateway
		if statusErr.Status == http.StatusForbidden {
			statusCode = http.StatusForbidden
		}
		return infraerrors.Newf(statusCode, "GROK_SSO_UPSTREAM_FAILED", "xAI SSO conversion failed: %v", err)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return infraerrors.Newf(http.StatusGatewayTimeout, "GROK_SSO_TIMEOUT", "xAI SSO conversion timed out: %v", err)
	}
	return infraerrors.Newf(http.StatusBadGateway, "GROK_SSO_CONVERSION_FAILED", "xAI SSO conversion failed: %v", err)
}

func grokOAuthStatusError(code, message string, resp *req.Response) error {
	statusCode := http.StatusBadGateway
	errorCode := code
	upstreamStatus := 0
	body := ""
	if resp != nil {
		upstreamStatus = resp.StatusCode
		body = logredact.RedactText(resp.String())
		if resp.StatusCode == http.StatusForbidden && grokOAuthHasExplicitEntitlementDenial(body) {
			statusCode = http.StatusForbidden
			errorCode = "GROK_OAUTH_ENTITLEMENT_DENIED"
		}
	}
	return infraerrors.Newf(statusCode, errorCode, "%s: status %d, body: %s", message, upstreamStatus, body)
}

func grokOAuthHasExplicitEntitlementDenial(body string) bool {
	lower := strings.ToLower(body)
	compact := strings.NewReplacer(" ", "", "\n", "", "\r", "", "\t", "").Replace(lower)
	for _, field := range []string{"error", "code", "reason"} {
		for _, value := range []string{"access_denied", "entitlement_denied", "subscription_required", "no_active_subscription"} {
			if strings.Contains(compact, `"`+field+`":"`+value+`"`) {
				return true
			}
		}
	}
	return strings.Contains(lower, "entitlement denied") ||
		strings.Contains(lower, "subscription required") ||
		strings.Contains(lower, "no active grok subscription")
}
