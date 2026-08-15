package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

var supportedGrokVoiceHTTPEndpoints = map[string]struct{}{
	"tts":           {},
	"stt":           {},
	"custom-voices": {},
}

// AudioUsage carries the native unit used by each xAI Voice price family.
// TTS is millions of input characters, STT is audio hours, and Realtime is
// connected minutes after an audio event has actually been observed.
type AudioUsage struct {
	Mode            string
	DurationOrUnits float64
}

// ValidateGrokAudioBillingPrice rejects billable Voice requests before any
// upstream resource is consumed. Custom voice management endpoints do not
// produce native audio usage units and therefore require no price.
func ValidateGrokAudioBillingPrice(group *Group, endpoint string) error {
	if group == nil {
		return fmt.Errorf("grok audio billing group is required")
	}
	baseEndpoint := strings.Split(strings.Trim(strings.TrimSpace(endpoint), "/"), "/")[0]
	var priceName string
	var price *float64
	switch baseEndpoint {
	case "tts":
		priceName = "audio_tts_price_per_million_chars"
		price = group.AudioTTSPricePerMillionChars
	case "stt":
		priceName = "audio_stt_price_per_hour"
		price = group.AudioSTTPricePerHour
	case "realtime":
		priceName = "audio_realtime_price_per_min"
		price = group.AudioRealtimePricePerMin
	case "custom-voices":
		return nil
	default:
		return fmt.Errorf("unsupported grok audio billing endpoint %q", endpoint)
	}
	return validateExplicitGrokUsagePrice(priceName, price)
}

func StableGrokAudioBillingRequestID(upstreamRequestID string) string {
	upstreamRequestID = strings.TrimSpace(upstreamRequestID)
	if strings.HasPrefix(upstreamRequestID, "grok_audio:") {
		return upstreamRequestID
	}
	if upstreamRequestID == "" {
		upstreamRequestID = generateRequestID()
	}
	return "grok_audio:" + upstreamRequestID
}

func StableGrokRealtimeBillingRequestID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if strings.HasPrefix(sessionID, "grok_realtime:") {
		return sessionID
	}
	if sessionID == "" {
		sessionID = generateRequestID()
	}
	return "grok_realtime:" + sessionID
}

func GrokVoiceSessionHash(sessionHash string) string {
	sessionHash = strings.TrimSpace(sessionHash)
	if sessionHash == "" {
		return ""
	}
	return "grok-voice:" + sessionHash
}

// ForwardGrokVoice relays official xAI /tts, /stt, and custom-voices HTTP
// resources. Responses are passed through because TTS/audio downloads are
// binary while STT and custom voice metadata are JSON.
func (s *OpenAIGatewayService) ForwardGrokVoice(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	endpoint string,
	body []byte,
	contentType string,
) (*OpenAIForwardResult, error) {
	if s == nil || account == nil {
		return nil, fmt.Errorf("grok voice service/account is required")
	}
	if account.Platform != PlatformGrok {
		return nil, fmt.Errorf("account platform %s is not supported for grok voice", account.Platform)
	}
	endpoint, baseEndpoint, err := validateGrokVoiceEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	token, _, err := s.GetRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	targetURL, err := buildGrokVoiceURL(ctx, account, s, endpoint)
	if err != nil {
		return nil, err
	}
	method := http.MethodPost
	if c != nil && c.Request != nil && strings.TrimSpace(c.Request.Method) != "" {
		method = c.Request.Method
	}

	upstreamCtx, release := detachUpstreamContext(ctx)
	defer release()
	req, err := http.NewRequestWithContext(
		WithHTTPUpstreamRedirectsDisabled(upstreamCtx),
		method,
		targetURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json, audio/*")
	if method != http.MethodGet && method != http.MethodDelete {
		if strings.TrimSpace(contentType) == "" {
			contentType = "application/json"
		}
		req.Header.Set("Content-Type", contentType)
	}
	if account.IsGrokOAuth() && isGrokCLIProxyTarget(targetURL) {
		applyGrokCLIHeaders(req.Header)
	}
	account.ApplyHeaderOverrides(req.Header)

	proxyURL, err := grokAccountProxyURL(account)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(started).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = resp.Body.Close() }()
	requestID := firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("xai-request-id"))
	if !isOpenAIUpstreamSuccessStatus(resp.StatusCode) {
		if !isOpenAIUpstreamErrorStatus(resp.StatusCode) {
			return rejectUnexpectedOpenAIUpstreamStatus(
				resp, c, account, false, writeGrokMediaErrorResponse,
			)
		}
		return s.handleGrokMediaErrorResponse(ctx, resp, c, account, requestID, endpoint)
	}
	s.updateGrokUsageFromResponse(ctx, account, resp.Header, resp.StatusCode)
	data, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	writeGrokMediaResponse(c, resp, data, s.responseHeaderFilter)
	elapsed := time.Since(started)
	return &OpenAIForwardResult{
		RequestID:     StableGrokAudioBillingRequestID(requestID),
		Model:         baseEndpoint,
		UpstreamModel: baseEndpoint,
		Duration:      elapsed,
		AudioUsage:    estimateGrokVoiceAudioUsage(baseEndpoint, body, data, elapsed),
	}, nil
}

func validateGrokVoiceEndpoint(endpoint string) (string, string, error) {
	endpoint = strings.Trim(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return "", "", fmt.Errorf("grok voice endpoint is required")
	}
	parts := strings.Split(endpoint, "/")
	baseEndpoint := parts[0]
	if _, ok := supportedGrokVoiceHTTPEndpoints[baseEndpoint]; !ok {
		return "", "", fmt.Errorf("unsupported grok voice endpoint: %s", endpoint)
	}
	if len(parts) > 1 && baseEndpoint != "custom-voices" {
		return "", "", fmt.Errorf("unsupported grok voice endpoint: %s", endpoint)
	}
	if baseEndpoint == "custom-voices" &&
		(len(parts) > 3 || (len(parts) == 3 && parts[2] != "audio")) {
		return "", "", fmt.Errorf("unsupported grok voice endpoint: %s", endpoint)
	}
	for _, part := range parts[1:] {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "?#\\") {
			return "", "", fmt.Errorf("invalid grok voice endpoint path")
		}
	}
	return endpoint, baseEndpoint, nil
}

func buildGrokVoiceURL(
	ctx context.Context,
	account *Account,
	service *OpenAIGatewayService,
	endpoint string,
) (string, error) {
	if service == nil {
		return "", fmt.Errorf("grok voice service is required")
	}
	validator, err := grokBaseURLValidator(ctx, account, service.cfg, service.settingService)
	if err != nil {
		return "", err
	}
	baseURL := account.GetGrokMediaBaseURL()
	// Voice APIs are not implemented by the official CLI chat proxy. This is
	// endpoint routing, not an error fallback: failures never trigger direct use.
	if strings.TrimSpace(baseURL) == "" || isGrokCLIProxyTarget(baseURL) {
		baseURL = xai.DefaultBaseURL
	}
	baseURL, err = validator(baseURL)
	if err != nil {
		return "", err
	}
	parts := strings.Split(strings.Trim(endpoint, "/"), "/")
	encoded := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) == "" || part == "." || part == ".." {
			return "", fmt.Errorf("invalid grok voice endpoint path")
		}
		encoded = append(encoded, url.PathEscape(part))
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.Join(encoded, "/"), nil
}

// ProxyGrokRealtime relays xAI Voice Realtime JSON events without protocol
// translation. Billing eligibility is returned separately and becomes true
// only after actual audio data is observed in either direction.
func (s *OpenAIGatewayService) ProxyGrokRealtime(
	ctx context.Context,
	client *coderws.Conn,
	account *Account,
	token, model string,
) (bool, error) {
	if s == nil || client == nil || account == nil {
		return false, fmt.Errorf("realtime service, client, and account are required")
	}
	if account.Platform != PlatformGrok {
		return false, fmt.Errorf("account platform %s is not supported for grok realtime", account.Platform)
	}
	baseURL, err := buildGrokVoiceURL(ctx, account, s, "realtime")
	if err != nil {
		return false, err
	}
	target, err := url.Parse(baseURL)
	if err != nil {
		return false, fmt.Errorf("parse grok realtime URL: %w", err)
	}
	switch strings.ToLower(target.Scheme) {
	case "https":
		target.Scheme = "wss"
	case "http":
		target.Scheme = "ws"
	default:
		return false, fmt.Errorf("unsupported grok realtime URL scheme")
	}
	query := target.Query()
	query.Set("model", firstNonEmpty(strings.TrimSpace(model), "grok-voice-latest"))
	target.RawQuery = query.Encode()
	headers := http.Header{"Authorization": []string{"Bearer " + token}}
	if account.IsGrokOAuth() && isGrokCLIProxyTarget(target.String()) {
		applyGrokCLIHeaders(headers)
	}
	account.ApplyHeaderOverrides(headers)
	proxyURL, err := grokAccountProxyURL(account)
	if err != nil {
		return false, err
	}
	dialer := s.getOpenAIWSPassthroughDialer()
	if dialer == nil {
		return false, fmt.Errorf("grok realtime websocket dialer is unavailable")
	}
	upstream, _, _, err := dialer.Dial(ctx, target.String(), headers, proxyURL)
	if err != nil {
		return false, err
	}
	defer func() { _ = upstream.Close() }()

	relayCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 2)
	var audioObserved atomic.Bool
	go relayGrokRealtimeUpstreamToClient(relayCtx, upstream, client, &audioObserved, errCh)
	go relayGrokRealtimeClientToUpstream(relayCtx, client, upstream, &audioObserved, errCh)
	return awaitGrokRealtimeAudioObserved(errCh, &audioObserved)
}

func relayGrokRealtimeUpstreamToClient(
	ctx context.Context,
	upstream openAIWSClientConn,
	client *coderws.Conn,
	audioObserved *atomic.Bool,
	errCh chan<- error,
) {
	for {
		message, err := upstream.ReadMessage(ctx)
		if err != nil {
			errCh <- err
			return
		}
		if grokRealtimeEventHasAudio(message) {
			audioObserved.Store(true)
		}
		if err := client.Write(ctx, coderws.MessageText, message); err != nil {
			errCh <- err
			return
		}
	}
}

func relayGrokRealtimeClientToUpstream(
	ctx context.Context,
	client *coderws.Conn,
	upstream openAIWSClientConn,
	audioObserved *atomic.Bool,
	errCh chan<- error,
) {
	for {
		kind, message, err := client.Read(ctx)
		if err != nil {
			errCh <- err
			return
		}
		if kind != coderws.MessageText && kind != coderws.MessageBinary {
			continue
		}
		if !gjson.ValidBytes(message) {
			errCh <- fmt.Errorf("invalid grok realtime event")
			return
		}
		if grokRealtimeEventHasAudio(message) {
			audioObserved.Store(true)
		}
		var raw json.RawMessage
		if err := json.Unmarshal(message, &raw); err != nil {
			errCh <- fmt.Errorf("decode grok realtime event: %w", err)
			return
		}
		if err := upstream.WriteJSON(ctx, raw); err != nil {
			errCh <- err
			return
		}
	}
}

func awaitGrokRealtimeAudioObserved(errCh <-chan error, audioObserved *atomic.Bool) (bool, error) {
	err := <-errCh
	if audioObserved == nil {
		return false, err
	}
	return audioObserved.Load(), err
}

func grokRealtimeEventHasAudio(message []byte) bool {
	if !gjson.ValidBytes(message) {
		return false
	}
	eventType := strings.ToLower(strings.TrimSpace(gjson.GetBytes(message, "type").String()))
	if !strings.Contains(eventType, "audio") || strings.Contains(eventType, "transcript") {
		return false
	}
	for _, path := range []string{"audio", "delta", "data"} {
		value := gjson.GetBytes(message, path)
		if value.Type == gjson.String && strings.TrimSpace(value.String()) != "" {
			return true
		}
	}
	return false
}

func estimateGrokVoiceAudioUsage(
	endpoint string,
	requestBody, responseBody []byte,
	elapsed time.Duration,
) *AudioUsage {
	switch strings.TrimSpace(endpoint) {
	case "tts":
		characters := 0
		if gjson.ValidBytes(requestBody) {
			for _, key := range []string{"input", "text", "prompt"} {
				if text := strings.TrimSpace(gjson.GetBytes(requestBody, key).String()); text != "" {
					characters = len([]rune(text))
					break
				}
			}
		}
		if characters <= 0 {
			characters = len(requestBody)
		}
		if characters <= 0 {
			return nil
		}
		return &AudioUsage{Mode: "tts", DurationOrUnits: float64(characters) / 1_000_000}
	case "stt":
		seconds := 0.0
		if gjson.ValidBytes(responseBody) {
			for _, path := range []string{"duration", "duration_seconds", "audio_duration", "usage.seconds"} {
				value := gjson.GetBytes(responseBody, path)
				if value.Exists() && value.Type == gjson.Number && value.Float() > 0 {
					seconds = value.Float()
					break
				}
			}
		}
		if seconds <= 0 && elapsed > 0 {
			seconds = elapsed.Seconds()
		}
		if seconds <= 0 && len(requestBody) > 0 {
			// Conservative compressed-speech approximation used only when xAI
			// supplies no authoritative duration.
			seconds = float64(len(requestBody)) / 16_000
		}
		if seconds <= 0 {
			return nil
		}
		return &AudioUsage{Mode: "stt", DurationOrUnits: seconds / 3600}
	default:
		return nil
	}
}
