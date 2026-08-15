package xai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	GrokAccountsBaseURL = "https://accounts.x.ai"

	grokPasswordLoginRPCEndpoint = GrokAccountsBaseURL + "/api/rpc"
	grokTurnstileWebsiteKey      = "0x4AAAAAAAhr9JGVDZbrZOo0"
	yesCaptchaCreateTaskURL      = "https://api.yescaptcha.com/createTask"
	yesCaptchaGetTaskResultURL   = "https://api.yescaptcha.com/getTaskResult"

	grokPasswordMaxEmailLength    = 320
	grokPasswordMaxPasswordLength = 4096
	grokPasswordMaxResponseBody   = 1 << 20
	grokCaptchaMaxResponseBody    = 64 << 10
	grokCaptchaDefaultTimeout     = 90 * time.Second
	grokCaptchaDefaultPollDelay   = 5 * time.Second
)

var (
	ErrGrokPasswordInputInvalid = errors.New("grok password login input is invalid")
	ErrGrokCaptchaUnavailable   = errors.New("grok password login captcha service is unavailable")
	ErrGrokPasswordLoginFailed  = errors.New("grok password login failed")
)

// GrokPasswordLoginOptions contains runtime-only dependencies for password
// login. Callers must inject a no-redirect HTTP client that already carries
// the selected account proxy. The helper never falls back to direct access.
type GrokPasswordLoginOptions struct {
	HTTPClient        SSODeviceHTTPClient
	CaptchaHTTPClient SSODeviceHTTPClient
	CaptchaClientKey  string
	CaptchaTimeout    time.Duration
	CaptchaPollDelay  time.Duration
	Sleep             func(context.Context, time.Duration) error
}

// GrokPasswordLoginResult is ephemeral. SSOToken must be passed directly to
// ConvertSSOToBuild and must never be serialized or persisted.
type GrokPasswordLoginResult struct {
	Email    string `json:"-"`
	SSOToken string `json:"-"`
}

// LoginWithPassword performs email/password -> Web SSO. It deliberately does
// not perform OAuth conversion so the service layer can keep SSO lifetime
// limited to a single stack frame before calling ConvertSSOToBuild.
func LoginWithPassword(ctx context.Context, email, password string, opts *GrokPasswordLoginOptions) (*GrokPasswordLoginResult, error) {
	email = strings.TrimSpace(email)
	if !validGrokPasswordEmail(email) || password == "" || len(password) > grokPasswordMaxPasswordLength || strings.ContainsRune(password, '\x00') {
		return nil, ErrGrokPasswordInputInvalid
	}
	if opts == nil || opts.HTTPClient == nil || opts.CaptchaHTTPClient == nil || strings.TrimSpace(opts.CaptchaClientKey) == "" {
		return nil, ErrGrokCaptchaUnavailable
	}

	timeout := opts.CaptchaTimeout
	if timeout <= 0 {
		timeout = grokCaptchaDefaultTimeout
	}
	pollDelay := opts.CaptchaPollDelay
	if pollDelay <= 0 {
		pollDelay = grokCaptchaDefaultPollDelay
	}
	sleep := opts.Sleep
	if sleep == nil {
		sleep = sleepContext
	}

	captchaCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	turnstileToken, err := solveGrokTurnstile(captchaCtx, opts.CaptchaHTTPClient, strings.TrimSpace(opts.CaptchaClientKey), pollDelay, sleep)
	if err != nil {
		return nil, err
	}
	cookieSetterURL, err := createGrokPasswordSession(ctx, opts.HTTPClient, email, password, turnstileToken)
	if err != nil {
		return nil, err
	}
	ssoToken, err := fetchGrokSSOCookie(ctx, opts.HTTPClient, cookieSetterURL)
	if err != nil {
		return nil, err
	}
	return &GrokPasswordLoginResult{Email: email, SSOToken: ssoToken}, nil
}

func validGrokPasswordEmail(email string) bool {
	if email == "" || len(email) > grokPasswordMaxEmailLength || strings.ContainsAny(email, "\r\n\x00 \t") {
		return false
	}
	at := strings.LastIndexByte(email, '@')
	return at > 0 && at < len(email)-1
}

func solveGrokTurnstile(
	ctx context.Context,
	client SSODeviceHTTPClient,
	clientKey string,
	pollDelay time.Duration,
	sleep func(context.Context, time.Duration) error,
) (string, error) {
	createPayload := map[string]any{
		"clientKey": clientKey,
		"task": map[string]any{
			"type":       "TurnstileTaskProxyless",
			"websiteURL": GrokAccountsBaseURL,
			"websiteKey": grokTurnstileWebsiteKey,
		},
	}
	createBody, err := json.Marshal(createPayload)
	if err != nil {
		return "", ErrGrokCaptchaUnavailable
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, yesCaptchaCreateTaskURL, bytes.NewReader(createBody))
	if err != nil {
		return "", ErrGrokCaptchaUnavailable
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return "", ErrGrokCaptchaUnavailable
	}
	data, status, err := readBoundedPasswordResponse(response, grokCaptchaMaxResponseBody)
	if err != nil || status < 200 || status >= 300 {
		return "", ErrGrokCaptchaUnavailable
	}
	var created struct {
		ErrorID int             `json:"errorId"`
		TaskID  json.RawMessage `json:"taskId"`
	}
	if json.Unmarshal(data, &created) != nil || created.ErrorID != 0 || len(created.TaskID) == 0 || string(created.TaskID) == "null" {
		return "", ErrGrokCaptchaUnavailable
	}

	for {
		if err := sleep(ctx, pollDelay); err != nil {
			return "", errors.Join(ErrGrokCaptchaUnavailable, err)
		}
		pollBody, err := json.Marshal(map[string]any{"clientKey": clientKey, "taskId": created.TaskID})
		if err != nil {
			return "", ErrGrokCaptchaUnavailable
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, yesCaptchaGetTaskResultURL, bytes.NewReader(pollBody))
		if err != nil {
			return "", ErrGrokCaptchaUnavailable
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			return "", ErrGrokCaptchaUnavailable
		}
		data, status, err := readBoundedPasswordResponse(response, grokCaptchaMaxResponseBody)
		if err != nil || status < 200 || status >= 300 {
			return "", ErrGrokCaptchaUnavailable
		}
		var polled struct {
			ErrorID  int    `json:"errorId"`
			Status   string `json:"status"`
			Solution struct {
				Token string `json:"token"`
			} `json:"solution"`
		}
		if json.Unmarshal(data, &polled) != nil || polled.ErrorID != 0 {
			return "", ErrGrokCaptchaUnavailable
		}
		switch strings.ToLower(strings.TrimSpace(polled.Status)) {
		case "ready":
			token := strings.TrimSpace(polled.Solution.Token)
			if token == "" || len(token) > ssoMaxTokenLength || strings.ContainsAny(token, "\r\n\x00") {
				return "", ErrGrokCaptchaUnavailable
			}
			return token, nil
		case "processing":
			continue
		default:
			return "", ErrGrokCaptchaUnavailable
		}
	}
}

func createGrokPasswordSession(ctx context.Context, client SSODeviceHTTPClient, email, password, turnstileToken string) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"rpc": "createSession",
		"req": map[string]any{
			"createSessionRequest": map[string]any{
				"credentials": map[string]any{
					"case": "emailAndPassword",
					"value": map[string]any{
						"email":             email,
						"clearTextPassword": password,
					},
				},
			},
			"turnstileToken": turnstileToken,
		},
	})
	if err != nil {
		return "", ErrGrokPasswordLoginFailed
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, grokPasswordLoginRPCEndpoint, bytes.NewReader(payload))
	if err != nil {
		return "", ErrGrokPasswordLoginFailed
	}
	request.Header.Set("Accept", "*/*")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", GrokAccountsBaseURL)
	request.Header.Set("Referer", GrokAccountsBaseURL+"/sign-in?redirect=grok-com&email=true")
	request.Header.Set("User-Agent", ssoDefaultUA)
	response, err := client.Do(request)
	if err != nil {
		return "", ErrGrokPasswordLoginFailed
	}
	data, status, err := readBoundedPasswordResponse(response, grokPasswordMaxResponseBody)
	if err != nil || status != http.StatusOK {
		return "", ErrGrokPasswordLoginFailed
	}
	var loginResponse struct {
		CookieSetterURL string `json:"cookieSetterUrl"`
		Error           string `json:"error"`
	}
	if json.Unmarshal(data, &loginResponse) != nil || strings.TrimSpace(loginResponse.Error) != "" {
		return "", ErrGrokPasswordLoginFailed
	}
	if _, err := validateGrokCookieSetterURL(loginResponse.CookieSetterURL); err != nil {
		return "", ErrGrokPasswordLoginFailed
	}
	return strings.TrimSpace(loginResponse.CookieSetterURL), nil
}

func fetchGrokSSOCookie(ctx context.Context, client SSODeviceHTTPClient, cookieSetterURL string) (string, error) {
	safeURL, err := validateGrokCookieSetterURL(cookieSetterURL)
	if err != nil {
		return "", ErrGrokPasswordLoginFailed
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, safeURL.String(), nil)
	if err != nil {
		return "", ErrGrokPasswordLoginFailed
	}
	request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	request.Header.Set("Referer", GrokAccountsBaseURL+"/")
	request.Header.Set("User-Agent", ssoDefaultUA)
	response, err := client.Do(request)
	if err != nil {
		return "", ErrGrokPasswordLoginFailed
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return "", ErrGrokPasswordLoginFailed
	}
	var fallback string
	for _, cookie := range response.Cookies() {
		name := strings.ToLower(strings.TrimSpace(cookie.Name))
		value := sanitizeSSOToken(cookie.Value)
		if value == "" {
			continue
		}
		if name == "sso" {
			return value, nil
		}
		if name == "sso-rw" {
			fallback = value
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", ErrGrokPasswordLoginFailed
}

func validateGrokCookieSetterURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "accounts.x.ai") {
		return nil, ErrGrokPasswordLoginFailed
	}
	if parsed.User != nil || parsed.Port() != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return nil, ErrGrokPasswordLoginFailed
	}
	return parsed, nil
}

func readBoundedPasswordResponse(response *http.Response, maxBytes int64) ([]byte, int, error) {
	if response == nil || response.Body == nil {
		return nil, 0, ErrGrokPasswordLoginFailed
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil || int64(len(data)) > maxBytes {
		return nil, response.StatusCode, ErrGrokPasswordLoginFailed
	}
	return data, response.StatusCode, nil
}

func (r *GrokPasswordLoginResult) String() string {
	if r == nil {
		return "<nil>"
	}
	return fmt.Sprintf("GrokPasswordLoginResult{email_present:%t,sso_present:%t}", strings.TrimSpace(r.Email) != "", strings.TrimSpace(r.SSOToken) != "")
}
