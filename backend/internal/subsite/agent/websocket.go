package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/cespare/xxhash/v2"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	responsesWebSocketReadLimit  = 16 << 20
	responsesWebSocketReadWait   = 30 * time.Second
	responsesWebSocketTurnWait   = 15 * time.Minute
	responsesWebSocketWriteWait  = 2 * time.Minute
	responsesWebSocketDialWait   = 10 * time.Second
	responsesWebSocketBetaHeader = "responses_websockets=2026-02-06"
	codexCLIUserAgent            = "codex_cli_rs/0.125.0"
)

type wsTurn struct {
	authorization *service.AuthorizeSubsiteResponse
	requestBody   []byte
	startedAt     time.Time
	upstreamModel string
}

func (s *Server) handleResponsesWebSocket(c *gin.Context) {
	clientConn, err := coderws.Accept(c.Writer, c.Request, &coderws.AcceptOptions{
		CompressionMode: coderws.CompressionContextTakeover,
	})
	if err != nil {
		return
	}
	defer func() {
		_ = clientConn.CloseNow()
	}()
	clientConn.SetReadLimit(responsesWebSocketReadLimit)

	if err := s.proxyResponsesWebSocket(c, clientConn); err != nil {
		code, reason := wsCloseForError(err)
		closeWebSocket(clientConn, code, reason)
	}
}

func (s *Server) proxyResponsesWebSocket(c *gin.Context, clientConn *coderws.Conn) error {
	ctx := c.Request.Context()
	apiKey := extractClientAPIKey(c)
	if apiKey == "" {
		return wsPolicyError("api key is required")
	}

	firstType, firstPayload, err := readWebSocketFrame(ctx, clientConn)
	if err != nil {
		return wsPolicyError("missing first response.create message")
	}
	if firstType != coderws.MessageText && firstType != coderws.MessageBinary {
		return wsPolicyError("unsupported websocket message type")
	}

	turn, outboundPayload, err := s.authorizeWebSocketTurn(c, apiKey, nil, firstPayload)
	if err != nil {
		return err
	}
	upstreamConn, err := s.dialAuthorizedResponsesWebSocket(c, turn)
	if err != nil {
		retryTurn, retryConn, retryPayload, retryErr := s.retryWebSocketTurn(c, apiKey, turn, err)
		if retryErr != nil {
			return retryErr
		}
		turn = retryTurn
		outboundPayload = retryPayload
		upstreamConn = retryConn
	}
	defer func() {
		if upstreamConn != nil {
			_ = upstreamConn.CloseNow()
		}
	}()

	var activeTurn *wsTurn
	lastAuthorization := turn.authorization
	nextPayload := outboundPayload
	nextType := coderws.MessageText
	_ = firstType
	for {
		activeTurn = turn
		if err := writeWebSocketFrame(ctx, upstreamConn, nextType, nextPayload); err != nil {
			retryTurn, retryConn, retryPayload, retryErr := s.retryWebSocketTurn(c, apiKey, activeTurn, fmt.Errorf("write upstream websocket request: %w", err))
			if retryErr != nil {
				return retryErr
			}
			_ = upstreamConn.CloseNow()
			upstreamConn = retryConn
			turn = retryTurn
			lastAuthorization = retryTurn.authorization
			nextPayload = retryPayload
			continue
		}
		if err := s.relayWebSocketTurn(c, clientConn, upstreamConn, activeTurn); err != nil {
			retryTurn, retryConn, retryPayload, retryErr := s.retryWebSocketTurn(c, apiKey, activeTurn, err)
			if retryErr != nil {
				return retryErr
			}
			_ = upstreamConn.CloseNow()
			upstreamConn = retryConn
			turn = retryTurn
			lastAuthorization = retryTurn.authorization
			nextPayload = retryPayload
			continue
		}

		msgType, payload, err := readWebSocketFrameWithTimeout(ctx, clientConn, responsesWebSocketTurnWait)
		if err != nil {
			if isWebSocketDisconnect(err) {
				return nil
			}
			return fmt.Errorf("read client websocket request: %w", err)
		}
		if msgType != coderws.MessageText && msgType != coderws.MessageBinary {
			return wsPolicyError("unsupported websocket message type")
		}
		turn, nextPayload, err = s.authorizeWebSocketTurn(c, apiKey, lastAuthorization, payload)
		if err != nil {
			return err
		}
		lastAuthorization = turn.authorization
		nextType = coderws.MessageText
		_ = msgType
	}
}

func (s *Server) dialAuthorizedResponsesWebSocket(c *gin.Context, turn *wsTurn) (*coderws.Conn, error) {
	if turn == nil || turn.authorization == nil {
		return nil, errors.New("websocket turn authorization is required")
	}
	account := accountFromAuthorization(turn.authorization)
	upstreamURL, err := buildResponsesWebSocketURL(account)
	if err != nil {
		s.cancelWebSocketTurn(c.Request.Context(), turn)
		return nil, fmt.Errorf("build upstream websocket url: %w", err)
	}
	upstreamHeaders := buildResponsesWebSocketHeaders(c, account, turn.authorization)
	dialCtx, cancelDial := context.WithTimeout(c.Request.Context(), responsesWebSocketDialWait)
	defer cancelDial()
	upstreamConn, resp, err := coderws.Dial(dialCtx, upstreamURL, &coderws.DialOptions{
		HTTPHeader: upstreamHeaders,
	})
	if err != nil {
		if resp != nil && resp.Body != nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			_ = resp.Body.Close()
			return nil, newUpstreamError(resp.StatusCode, resp.Header.Get("Content-Type"), body)
		}
		return nil, fmt.Errorf("dial upstream websocket: %w", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	upstreamConn.SetReadLimit(responsesWebSocketReadLimit)
	return upstreamConn, nil
}

func (s *Server) retryWebSocketTurn(c *gin.Context, apiKey string, failedTurn *wsTurn, cause error) (*wsTurn, *coderws.Conn, []byte, error) {
	if failedTurn == nil || failedTurn.authorization == nil {
		return nil, nil, nil, cause
	}
	if !shouldFailoverForUpstreamError(cause) {
		s.cancelWebSocketTurn(c.Request.Context(), failedTurn)
		return nil, nil, nil, cause
	}
	s.cancelWebSocketTurn(c.Request.Context(), failedTurn)
	retryTurn, outboundPayload, err := s.reauthorizeWebSocketTurn(c, apiKey, failedTurn)
	if err != nil {
		return nil, nil, nil, err
	}
	upstreamConn, err := s.dialAuthorizedResponsesWebSocket(c, retryTurn)
	if err != nil {
		s.cancelWebSocketTurn(c.Request.Context(), retryTurn)
		return nil, nil, nil, err
	}
	return retryTurn, upstreamConn, outboundPayload, nil
}

func (s *Server) cancelWebSocketTurn(ctx context.Context, turn *wsTurn) {
	if s == nil || turn == nil || turn.authorization == nil {
		return
	}
	s.credentials.Delete(turn.authorization.RequestID)
	_ = s.cancelReservation(ctx, turn.authorization.RequestID)
}

func (s *Server) authorizeWebSocketTurn(c *gin.Context, apiKey string, previous *service.AuthorizeSubsiteResponse, payload []byte) (*wsTurn, []byte, error) {
	normalized, requestedModel, err := normalizeResponsesWebSocketPayload(payload)
	if err != nil {
		return nil, nil, err
	}
	preferredLeaseID := ""
	var preferredAccountID int64
	if previous != nil {
		preferredLeaseID = previous.LeaseID
		preferredAccountID = previous.AccountID
	}
	input := service.AuthorizeSubsiteRequestInput{
		SubsiteID:          s.cfg.Subsite.ID,
		APIKey:             apiKey,
		Platform:           service.PlatformOpenAI,
		RequestedModel:     requestedModel,
		MappedModel:        requestedModel,
		RequestFingerprint: requestFingerprint(http.MethodGet, c.Request.URL.Path, normalized),
		ClientIP:           clientIP(c),
		UserAgent:          c.GetHeader("User-Agent"),
		InboundEndpoint:    c.Request.URL.Path,
		PreferredLeaseID:   preferredLeaseID,
		PreferredAccountID: preferredAccountID,
	}
	if err := populateAuthorizeCostHints(&input, c, normalized); err != nil {
		return nil, nil, wsPolicyError(err.Error())
	}
	authorization, err := s.master.Authorize(c.Request.Context(), input)
	if err != nil {
		return nil, nil, wsPolicyError(err.Error())
	}
	s.credentials.Set(authorization)

	outbound := normalized
	upstreamModel := strings.TrimSpace(authorization.MappedModel)
	if upstreamModel == "" {
		upstreamModel = strings.TrimSpace(authorization.RequestedModel)
	}
	if upstreamModel != "" && upstreamModel != requestedModel {
		outbound, err = sjson.SetBytes(normalized, "model", upstreamModel)
		if err != nil {
			s.credentials.Delete(authorization.RequestID)
			_ = s.cancelReservation(c.Request.Context(), authorization.RequestID)
			return nil, nil, wsPolicyError("invalid websocket request payload")
		}
	}
	return &wsTurn{
		authorization: authorization,
		requestBody:   normalized,
		startedAt:     time.Now(),
		upstreamModel: upstreamModel,
	}, outbound, nil
}

func (s *Server) reauthorizeWebSocketTurn(c *gin.Context, apiKey string, failedTurn *wsTurn) (*wsTurn, []byte, error) {
	if failedTurn == nil || failedTurn.authorization == nil {
		return nil, nil, wsPolicyError("websocket turn authorization is required")
	}
	normalized := append([]byte(nil), failedTurn.requestBody...)
	requestedModel := strings.TrimSpace(gjson.GetBytes(normalized, "model").String())
	input := service.AuthorizeSubsiteRequestInput{
		SubsiteID:          s.cfg.Subsite.ID,
		APIKey:             apiKey,
		Platform:           service.PlatformOpenAI,
		RequestedModel:     requestedModel,
		MappedModel:        requestedModel,
		RequestFingerprint: requestFingerprint(http.MethodGet, c.Request.URL.Path, normalized),
		ClientIP:           clientIP(c),
		UserAgent:          c.GetHeader("User-Agent"),
		InboundEndpoint:    c.Request.URL.Path,
		ExcludedLeaseIDs:   []string{failedTurn.authorization.LeaseID},
		ExcludedAccountIDs: []int64{failedTurn.authorization.AccountID},
	}
	if err := populateAuthorizeCostHints(&input, c, normalized); err != nil {
		return nil, nil, wsPolicyError(err.Error())
	}
	authorization, err := s.master.Authorize(c.Request.Context(), input)
	if err != nil {
		return nil, nil, err
	}
	s.credentials.Set(authorization)

	outbound := normalized
	upstreamModel := strings.TrimSpace(authorization.MappedModel)
	if upstreamModel == "" {
		upstreamModel = strings.TrimSpace(authorization.RequestedModel)
	}
	if upstreamModel != "" && upstreamModel != requestedModel {
		outbound, err = sjson.SetBytes(normalized, "model", upstreamModel)
		if err != nil {
			s.credentials.Delete(authorization.RequestID)
			_ = s.cancelReservation(c.Request.Context(), authorization.RequestID)
			return nil, nil, wsPolicyError("invalid websocket request payload")
		}
	}
	return &wsTurn{
		authorization: authorization,
		requestBody:   normalized,
		startedAt:     time.Now(),
		upstreamModel: upstreamModel,
	}, outbound, nil
}

func normalizeResponsesWebSocketPayload(payload []byte) ([]byte, string, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return nil, "", wsPolicyError("empty websocket request payload")
	}
	if !gjson.ValidBytes(trimmed) {
		return nil, "", wsPolicyError("invalid websocket request payload")
	}
	eventType := strings.TrimSpace(gjson.GetBytes(trimmed, "type").String())
	switch eventType {
	case "":
		var err error
		trimmed, err = sjson.SetBytes(trimmed, "type", "response.create")
		if err != nil {
			return nil, "", wsPolicyError("invalid websocket request payload")
		}
	case "response.create":
	case "response.append":
		return nil, "", wsPolicyError("response.append is not supported; use response.create with previous_response_id")
	default:
		return nil, "", wsPolicyError("unsupported websocket request type: " + eventType)
	}
	model := strings.TrimSpace(gjson.GetBytes(trimmed, "model").String())
	if model == "" {
		return nil, "", wsPolicyError("model is required in response.create payload")
	}
	if previousID := strings.TrimSpace(gjson.GetBytes(trimmed, "previous_response_id").String()); strings.HasPrefix(previousID, "msg_") {
		return nil, "", wsPolicyError("previous_response_id must be a response.id (resp_*), not a message id")
	}
	return trimmed, model, nil
}

func (s *Server) relayWebSocketTurn(c *gin.Context, clientConn, upstreamConn *coderws.Conn, turn *wsTurn) error {
	if turn == nil || turn.authorization == nil {
		return errors.New("websocket turn authorization is required")
	}
	defer s.credentials.Delete(turn.authorization.RequestID)
	var usage parsedUsage
	terminalEvent := ""
	clientDisconnected := false
	var firstTokenMs *int
	for {
		msgType, payload, err := readWebSocketFrameWithTimeout(c.Request.Context(), upstreamConn, responsesWebSocketTurnWait)
		if err != nil {
			return fmt.Errorf("read upstream websocket event: %w", err)
		}
		outbound := payload
		if msgType == coderws.MessageText && gjson.ValidBytes(payload) {
			eventType := parseResponsesWebSocketEventType(payload)
			if firstTokenMs == nil && isResponsesWebSocketTokenEvent(eventType) {
				value := int(time.Since(turn.startedAt).Milliseconds())
				firstTokenMs = &value
			}
			if responsesWebSocketEventShouldParseUsage(eventType) {
				mergeUsage(&usage, openAIUsageFromResult(gjson.GetBytes(payload, "response.usage")))
			}
			if shouldReplaceResponsesWebSocketModel(eventType) {
				outbound = replaceResponsesWebSocketModel(payload, turn.upstreamModel, turn.authorization.RequestedModel)
			}
			if responsesWebSocketTerminalEvent(eventType) {
				terminalEvent = eventType
			}
		}
		if terminalEvent != "" {
			item := buildResponsesWebSocketUsageItem(c, turn, usage, time.Since(turn.startedAt), firstTokenMs)
			if err := s.queue.Enqueue(c.Request.Context(), item); err != nil {
				return fmt.Errorf("enqueue websocket turn usage: %w", err)
			}
		}
		if !clientDisconnected {
			if err := writeWebSocketFrame(c.Request.Context(), clientConn, msgType, outbound); err != nil {
				if isWebSocketDisconnect(err) {
					clientDisconnected = true
				} else if terminalEvent == "" {
					return fmt.Errorf("write client websocket event: %w", err)
				} else {
					clientDisconnected = true
				}
			}
		}
		if terminalEvent == "" {
			continue
		}
		return nil
	}
}

func buildResponsesWebSocketUsageItem(c *gin.Context, turn *wsTurn, usage parsedUsage, duration time.Duration, firstTokenMs *int) service.UsageIngestItem {
	authorization := turn.authorization
	return service.UsageIngestItem{
		RequestID:             authorization.RequestID,
		ReservationID:         authorization.ReservationID,
		APIKeyID:              authorization.APIKeyID,
		UserID:                authorization.UserID,
		AccountID:             authorization.AccountID,
		GroupID:               authorization.GroupID,
		SubscriptionID:        authorization.SubscriptionID,
		AccountType:           authorization.Credential.AccountType,
		Model:                 authorization.MappedModel,
		RequestedModel:        authorization.RequestedModel,
		UpstreamModel:         stringPtrIfNotEqual(turn.upstreamModel, authorization.RequestedModel),
		ServiceTier:           strings.TrimSpace(gjson.GetBytes(turn.requestBody, "service_tier").String()),
		ReasoningEffort:       extractReasoningEffort(turn.requestBody),
		BillingType:           authorization.BillingType,
		RequestType:           int16(service.RequestTypeWSV2),
		InputTokens:           usage.inputTokens,
		OutputTokens:          usage.outputTokens,
		CacheCreationTokens:   usage.cacheCreationTokens,
		CacheCreation5mTokens: usage.cacheCreation5mTokens,
		CacheCreation1hTokens: usage.cacheCreation1hTokens,
		CacheReadTokens:       usage.cacheReadTokens,
		ImageOutputTokens:     usage.imageOutputTokens,
		RequestFingerprint:    requestFingerprint(http.MethodGet, c.Request.URL.Path, turn.requestBody),
		RequestPayloadHash:    hashBytes(turn.requestBody),
		InboundEndpoint:       c.Request.URL.Path,
		UpstreamEndpoint:      "/v1/responses",
		UserAgent:             c.GetHeader("User-Agent"),
		IPAddress:             clientIP(c),
		DurationMs:            durationMillisPtr(duration),
		FirstTokenMs:          firstTokenMs,
		OccurredAt:            turn.startedAt,
	}
}

func buildResponsesWebSocketURL(account *service.Account) (string, error) {
	if account == nil {
		return "", errors.New("account is required")
	}
	base := strings.TrimRight(strings.TrimSpace(account.GetOpenAIBaseURL()), "/")
	if account.Type == service.AccountTypeOAuth {
		base = "https://chatgpt.com/backend-api/codex/responses"
	}
	if base == "" {
		base = "https://api.openai.com"
	}
	if !strings.HasSuffix(base, "/responses") {
		if strings.HasSuffix(base, "/v1") {
			base += "/responses"
		} else {
			base += "/v1/responses"
		}
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse websocket upstream url: %w", err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	case "wss", "ws":
	default:
		return "", fmt.Errorf("unsupported websocket upstream scheme: %s", parsed.Scheme)
	}
	return parsed.String(), nil
}

func buildResponsesWebSocketHeaders(c *gin.Context, account *service.Account, authorization *service.AuthorizeSubsiteResponse) http.Header {
	headers := make(http.Header)
	applyUpstreamAuth(headers, account)
	headers.Set("OpenAI-Beta", responsesWebSocketBetaHeader)
	if value := strings.TrimSpace(c.GetHeader("Accept-Language")); value != "" {
		headers.Set("Accept-Language", value)
	}
	apiKeyID := int64(0)
	if authorization != nil {
		apiKeyID = authorization.APIKeyID
	}
	isOAuth := account != nil && account.Type == service.AccountTypeOAuth
	if value := strings.TrimSpace(c.GetHeader("OpenAI-Conversation-ID")); value != "" {
		headers.Set("conversation_id", subsiteWebSocketSessionHeaderValue(apiKeyID, value, isOAuth))
	} else if value := strings.TrimSpace(c.GetHeader("conversation_id")); value != "" {
		headers.Set("conversation_id", subsiteWebSocketSessionHeaderValue(apiKeyID, value, isOAuth))
	}
	if value := strings.TrimSpace(c.GetHeader("OpenAI-Session-ID")); value != "" {
		headers.Set("session_id", subsiteWebSocketSessionHeaderValue(apiKeyID, value, isOAuth))
	} else if value := strings.TrimSpace(c.GetHeader("session_id")); value != "" {
		headers.Set("session_id", subsiteWebSocketSessionHeaderValue(apiKeyID, value, isOAuth))
	}
	if value := strings.TrimSpace(c.GetHeader("User-Agent")); value != "" {
		headers.Set("User-Agent", value)
	}
	if isOAuth {
		if accountID := strings.TrimSpace(account.GetCredential("chatgpt_account_id")); accountID != "" {
			headers.Set("chatgpt-account-id", accountID)
		}
		if originator := strings.TrimSpace(c.GetHeader("originator")); originator != "" {
			headers.Set("originator", originator)
		} else {
			headers.Set("originator", "codex_cli_rs")
		}
		if !openai.IsCodexOfficialClientByHeaders(headers.Get("User-Agent"), headers.Get("originator")) {
			headers.Set("User-Agent", codexCLIUserAgent)
		}
	}
	return headers
}

func subsiteWebSocketSessionHeaderValue(apiKeyID int64, raw string, isolate bool) string {
	if !isolate {
		return strings.TrimSpace(raw)
	}
	return isolateSubsiteWebSocketSessionID(apiKeyID, raw)
}

func isolateSubsiteWebSocketSessionID(apiKeyID int64, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if apiKeyID <= 0 {
		return raw
	}
	h := xxhash.New()
	_, _ = fmt.Fprintf(h, "k%d:", apiKeyID)
	_, _ = h.WriteString(raw)
	return fmt.Sprintf("%016x", h.Sum64())
}

func readWebSocketFrame(ctx context.Context, conn *coderws.Conn) (coderws.MessageType, []byte, error) {
	return readWebSocketFrameWithTimeout(ctx, conn, responsesWebSocketReadWait)
}

func readWebSocketFrameWithTimeout(ctx context.Context, conn *coderws.Conn, timeout time.Duration) (coderws.MessageType, []byte, error) {
	if timeout <= 0 {
		timeout = responsesWebSocketReadWait
	}
	readCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return conn.Read(readCtx)
}

func writeWebSocketFrame(ctx context.Context, conn *coderws.Conn, msgType coderws.MessageType, payload []byte) error {
	writeCtx, cancel := context.WithTimeout(ctx, responsesWebSocketWriteWait)
	defer cancel()
	return conn.Write(writeCtx, msgType, payload)
}

func closeWebSocket(conn *coderws.Conn, status coderws.StatusCode, reason string) {
	if conn == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 120 {
		reason = reason[:120]
	}
	_ = conn.Close(status, reason)
	_ = conn.CloseNow()
}

type wsClientError struct {
	status coderws.StatusCode
	reason string
}

func (e *wsClientError) Error() string {
	return e.reason
}

func wsPolicyError(reason string) error {
	return &wsClientError{status: coderws.StatusPolicyViolation, reason: strings.TrimSpace(reason)}
}

func wsCloseForError(err error) (coderws.StatusCode, string) {
	var clientErr *wsClientError
	if errors.As(err, &clientErr) {
		return clientErr.status, clientErr.reason
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return coderws.StatusTryAgainLater, "websocket timeout"
	}
	return coderws.StatusBadGateway, "upstream websocket proxy failed"
}

func isWebSocketDisconnect(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return true
	}
	switch coderws.CloseStatus(err) {
	case coderws.StatusNormalClosure, coderws.StatusGoingAway, coderws.StatusNoStatusRcvd, coderws.StatusAbnormalClosure:
		return true
	default:
		return false
	}
}

func parseResponsesWebSocketEventType(payload []byte) string {
	return strings.TrimSpace(gjson.GetBytes(payload, "type").String())
}

func responsesWebSocketTerminalEvent(eventType string) bool {
	switch eventType {
	case "response.completed", "response.done", "response.failed", "response.incomplete", "response.cancelled", "response.canceled":
		return true
	default:
		return false
	}
}

func responsesWebSocketEventShouldParseUsage(eventType string) bool {
	switch eventType {
	case "response.completed", "response.done", "response.failed":
		return true
	default:
		return false
	}
}

func shouldReplaceResponsesWebSocketModel(eventType string) bool {
	switch eventType {
	case "response.created", "response.completed", "response.done", "response.failed", "response.incomplete":
		return true
	default:
		return false
	}
}

func isResponsesWebSocketTokenEvent(eventType string) bool {
	if eventType == "" {
		return false
	}
	switch eventType {
	case "response.created", "response.in_progress", "response.output_item.added", "response.output_item.done":
		return false
	}
	if strings.Contains(eventType, ".delta") {
		return true
	}
	if strings.HasPrefix(eventType, "response.output_text") || strings.HasPrefix(eventType, "response.output") {
		return true
	}
	return eventType == "response.completed" || eventType == "response.done"
}

func replaceResponsesWebSocketModel(payload []byte, fromModel string, toModel string) []byte {
	fromModel = strings.TrimSpace(fromModel)
	toModel = strings.TrimSpace(toModel)
	if fromModel == "" || toModel == "" || fromModel == toModel {
		return payload
	}
	updated := payload
	if gjson.GetBytes(updated, "model").String() == fromModel {
		if next, err := sjson.SetBytes(updated, "model", toModel); err == nil {
			updated = next
		}
	}
	if gjson.GetBytes(updated, "response.model").String() == fromModel {
		if next, err := sjson.SetBytes(updated, "response.model", toModel); err == nil {
			updated = next
		}
	}
	return updated
}

func stringPtrIfNotEqual(value string, other string) *string {
	value = strings.TrimSpace(value)
	other = strings.TrimSpace(other)
	if value == "" || value == other {
		return nil
	}
	return &value
}
