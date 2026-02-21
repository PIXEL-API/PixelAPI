package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
)

const soraCurlCFFISidecarDefaultTimeoutSeconds = 60

type soraCurlCFFISidecarRequest struct {
	Method         string              `json:"method"`
	URL            string              `json:"url"`
	Headers        map[string][]string `json:"headers,omitempty"`
	BodyBase64     string              `json:"body_base64,omitempty"`
	ProxyURL       string              `json:"proxy_url,omitempty"`
	SessionKey     string              `json:"session_key,omitempty"`
	Impersonate    string              `json:"impersonate,omitempty"`
	TimeoutSeconds int                 `json:"timeout_seconds,omitempty"`
}

type soraCurlCFFISidecarResponse struct {
	StatusCode int            `json:"status_code"`
	Status     int            `json:"status"`
	Headers    map[string]any `json:"headers"`
	BodyBase64 string         `json:"body_base64"`
	Body       string         `json:"body"`
	Error      string         `json:"error"`
}

func (c *SoraDirectClient) doHTTPViaCurlCFFISidecar(req *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, errors.New("request url is nil")
	}
	if c == nil || c.cfg == nil {
		return nil, errors.New("sora curl_cffi sidecar config is nil")
	}
	if !c.cfg.Sora.Client.CurlCFFISidecar.Enabled {
		return nil, errors.New("sora curl_cffi sidecar is disabled")
	}
	endpoint := c.curlCFFISidecarEndpoint()
	if endpoint == "" {
		return nil, errors.New("sora curl_cffi sidecar base_url is empty")
	}

	bodyBytes, err := readAndRestoreRequestBody(req)
	if err != nil {
		return nil, fmt.Errorf("sora curl_cffi sidecar read request body failed: %w", err)
	}

	headers := make(map[string][]string, len(req.Header)+1)
	for key, vals := range req.Header {
		copied := make([]string, len(vals))
		copy(copied, vals)
		headers[key] = copied
	}
	if strings.TrimSpace(req.Host) != "" {
		if _, ok := headers["Host"]; !ok {
			headers["Host"] = []string{req.Host}
		}
	}

	payload := soraCurlCFFISidecarRequest{
		Method:         req.Method,
		URL:            req.URL.String(),
		Headers:        headers,
		ProxyURL:       strings.TrimSpace(proxyURL),
		SessionKey:     c.sidecarSessionKey(account, proxyURL),
		Impersonate:    c.curlCFFIImpersonate(),
		TimeoutSeconds: c.curlCFFISidecarTimeoutSeconds(),
	}
	if len(bodyBytes) > 0 {
		payload.BodyBase64 = base64.StdEncoding.EncodeToString(bodyBytes)
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("sora curl_cffi sidecar marshal request failed: %w", err)
	}

	sidecarReq, err := http.NewRequestWithContext(req.Context(), http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("sora curl_cffi sidecar build request failed: %w", err)
	}
	sidecarReq.Header.Set("Content-Type", "application/json")
	sidecarReq.Header.Set("Accept", "application/json")

	httpClient := &http.Client{Timeout: time.Duration(payload.TimeoutSeconds) * time.Second}
	sidecarResp, err := httpClient.Do(sidecarReq)
	if err != nil {
		return nil, fmt.Errorf("sora curl_cffi sidecar request failed: %w", err)
	}
	defer func() {
		_ = sidecarResp.Body.Close()
	}()

	sidecarRespBody, err := io.ReadAll(io.LimitReader(sidecarResp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("sora curl_cffi sidecar read response failed: %w", err)
	}
	if sidecarResp.StatusCode != http.StatusOK {
		redacted := truncateForLog([]byte(logredact.RedactText(string(sidecarRespBody))), 512)
		return nil, fmt.Errorf("sora curl_cffi sidecar http status=%d body=%s", sidecarResp.StatusCode, redacted)
	}

	var payloadResp soraCurlCFFISidecarResponse
	if err := json.Unmarshal(sidecarRespBody, &payloadResp); err != nil {
		return nil, fmt.Errorf("sora curl_cffi sidecar parse response failed: %w", err)
	}
	if msg := strings.TrimSpace(payloadResp.Error); msg != "" {
		return nil, fmt.Errorf("sora curl_cffi sidecar upstream error: %s", msg)
	}
	statusCode := payloadResp.StatusCode
	if statusCode <= 0 {
		statusCode = payloadResp.Status
	}
	if statusCode <= 0 {
		return nil, errors.New("sora curl_cffi sidecar response missing status code")
	}

	responseBody := []byte(payloadResp.Body)
	if strings.TrimSpace(payloadResp.BodyBase64) != "" {
		decoded, err := base64.StdEncoding.DecodeString(payloadResp.BodyBase64)
		if err != nil {
			return nil, fmt.Errorf("sora curl_cffi sidecar decode body failed: %w", err)
		}
		responseBody = decoded
	}

	respHeaders := make(http.Header)
	for key, rawVal := range payloadResp.Headers {
		for _, v := range convertSidecarHeaderValue(rawVal) {
			respHeaders.Add(key, v)
		}
	}

	return &http.Response{
		StatusCode:    statusCode,
		Header:        respHeaders,
		Body:          io.NopCloser(bytes.NewReader(responseBody)),
		ContentLength: int64(len(responseBody)),
		Request:       req,
	}, nil
}

func readAndRestoreRequestBody(req *http.Request) ([]byte, error) {
	if req == nil || req.Body == nil {
		return nil, nil
	}
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	req.ContentLength = int64(len(bodyBytes))
	return bodyBytes, nil
}

func (c *SoraDirectClient) curlCFFISidecarEndpoint() string {
	if c == nil || c.cfg == nil {
		return ""
	}
	raw := strings.TrimSpace(c.cfg.Sora.Client.CurlCFFISidecar.BaseURL)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
		return raw
	}
	if path := strings.TrimSpace(parsed.Path); path == "" || path == "/" {
		parsed.Path = "/request"
	}
	return parsed.String()
}

func (c *SoraDirectClient) curlCFFISidecarTimeoutSeconds() int {
	if c == nil || c.cfg == nil {
		return soraCurlCFFISidecarDefaultTimeoutSeconds
	}
	timeoutSeconds := c.cfg.Sora.Client.CurlCFFISidecar.TimeoutSeconds
	if timeoutSeconds <= 0 {
		return soraCurlCFFISidecarDefaultTimeoutSeconds
	}
	return timeoutSeconds
}

func (c *SoraDirectClient) curlCFFIImpersonate() string {
	if c == nil || c.cfg == nil {
		return "chrome131"
	}
	impersonate := strings.TrimSpace(c.cfg.Sora.Client.CurlCFFISidecar.Impersonate)
	if impersonate == "" {
		return "chrome131"
	}
	return impersonate
}

func (c *SoraDirectClient) sidecarSessionReuseEnabled() bool {
	if c == nil || c.cfg == nil {
		return true
	}
	return c.cfg.Sora.Client.CurlCFFISidecar.SessionReuseEnabled
}

func (c *SoraDirectClient) sidecarSessionTTLSeconds() int {
	if c == nil || c.cfg == nil {
		return 3600
	}
	ttl := c.cfg.Sora.Client.CurlCFFISidecar.SessionTTLSeconds
	if ttl < 0 {
		return 3600
	}
	return ttl
}

func convertSidecarHeaderValue(raw any) []string {
	switch val := raw.(type) {
	case nil:
		return nil
	case string:
		if strings.TrimSpace(val) == "" {
			return nil
		}
		return []string{val}
	case []any:
		out := make([]string, 0, len(val))
		for _, item := range val {
			s := strings.TrimSpace(fmt.Sprint(item))
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if strings.TrimSpace(item) != "" {
				out = append(out, item)
			}
		}
		return out
	default:
		s := strings.TrimSpace(fmt.Sprint(val))
		if s == "" {
			return nil
		}
		return []string{s}
	}
}
