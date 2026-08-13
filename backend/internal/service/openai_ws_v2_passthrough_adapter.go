package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type openAIWSClientFrameConn struct {
	conn *coderws.Conn
}

// openAIWSCyberDetectingFrameConn observes upstream frames before the relay
// forwards them to the client. This keeps passthrough WebSocket mode on the
// same structured cyber_policy detector used by HTTP and SSE paths.
type openAIWSCyberDetectingFrameConn struct {
	inner openaiwsv2.FrameConn
	c     *gin.Context
}

var _ openaiwsv2.FrameConn = (*openAIWSCyberDetectingFrameConn)(nil)

func (c *openAIWSCyberDetectingFrameConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	msgType, payload, err := c.inner.ReadFrame(ctx)
	if err == nil && (msgType == coderws.MessageText || msgType == coderws.MessageBinary) {
		markOpsCyberPolicyPayload(c.c, payload, http.StatusOK, 0, 0)
	}
	return msgType, payload, err
}

func (c *openAIWSCyberDetectingFrameConn) WriteFrame(ctx context.Context, msgType coderws.MessageType, payload []byte) error {
	return c.inner.WriteFrame(ctx, msgType, payload)
}

func (c *openAIWSCyberDetectingFrameConn) Close() error {
	return c.inner.Close()
}

// openAIWSPolicyEnforcingFrameConn wraps a client-side FrameConn and runs
// every client→upstream frame through the passthrough request filter. It is
// the relay equivalent of the parseClientPayload integration in the ingress
// session path. filter returns:
//   - newPayload, nil, nil: forward the (possibly mutated) payload
//   - _, *OpenAIFastBlockedError, nil: block — the wrapper sends an error
//     event via onBlock and surfaces a transport-level error so the relay
//     stops reading from the client.
//   - _, _, err: a transport error other than block.
type openAIWSPolicyEnforcingFrameConn struct {
	inner   openaiwsv2.FrameConn
	filter  func(msgType coderws.MessageType, payload []byte) ([]byte, *OpenAIFastBlockedError, error)
	onBlock func(blocked *OpenAIFastBlockedError)
}

var _ openaiwsv2.FrameConn = (*openAIWSPolicyEnforcingFrameConn)(nil)

func (c *openAIWSPolicyEnforcingFrameConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	if c == nil || c.inner == nil {
		return coderws.MessageText, nil, errOpenAIWSConnClosed
	}
	msgType, payload, err := c.inner.ReadFrame(ctx)
	if err != nil {
		return msgType, payload, err
	}
	if msgType == coderws.MessageText && !json.Valid(payload) {
		invalidErr := errors.New("invalid websocket request JSON")
		return msgType, nil, NewOpenAIWSClientCloseError(
			coderws.StatusPolicyViolation,
			"invalid websocket request payload",
			invalidErr,
		)
	}
	if c.filter == nil {
		return msgType, payload, nil
	}
	updated, blocked, filterErr := c.filter(msgType, payload)
	if filterErr != nil {
		return msgType, payload, filterErr
	}
	if blocked != nil {
		if c.onBlock != nil {
			c.onBlock(blocked)
		}
		return msgType, nil, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, blocked.Message, blocked)
	}
	return msgType, updated, nil
}

// openAIWSPassthroughTurnLifecycle serializes response.create turns on a
// passthrough connection. The ingress hooks own concurrency slots, so a new
// turn must not acquire its slots until the preceding terminal/error callback
// has released them.
type openAIWSPassthroughTurnLifecycle struct {
	mu                    sync.Mutex
	ctx                   context.Context
	hooks                 *OpenAIWSIngressHooks
	onTurnContextDone     func(error)
	nextTurn              int
	activeTurn            int
	activePayload         []byte
	activeContext         context.Context
	activeContextCause    error
	stopActiveContext     func() bool
	afterTurnLifecycleErr error
}

func newOpenAIWSPassthroughTurnLifecycle(hooks *OpenAIWSIngressHooks) *openAIWSPassthroughTurnLifecycle {
	return newOpenAIWSPassthroughTurnLifecycleWithContext(context.Background(), hooks, nil)
}

func newOpenAIWSPassthroughTurnLifecycleWithContext(
	ctx context.Context,
	hooks *OpenAIWSIngressHooks,
	onTurnContextDone func(error),
) *openAIWSPassthroughTurnLifecycle {
	if ctx == nil {
		ctx = context.Background()
	}
	return &openAIWSPassthroughTurnLifecycle{
		ctx:               ctx,
		hooks:             hooks,
		onTurnContextDone: onTurnContextDone,
		nextTurn:          1,
	}
}

func (l *openAIWSPassthroughTurnLifecycle) begin(payload ...[]byte) (int, error) {
	if l == nil {
		return 0, errors.New("passthrough turn lifecycle is unavailable")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.activeTurn > 0 {
		return 0, NewOpenAIWSClientCloseError(
			coderws.StatusPolicyViolation,
			"parallel response.create turns are not supported",
			nil,
		)
	}
	turn := l.nextTurn
	if turn <= 0 {
		turn = 1
	}
	var payloadBytes []byte
	if len(payload) > 0 {
		payloadBytes = payload[0]
	}
	turnPayload := cloneOpenAIWSPayloadBytes(payloadBytes)
	turnCtx, err := beginOpenAIWSIngressTurn(l.ctx, l.hooks, turn, turnPayload)
	if err != nil {
		return 0, err
	}
	l.activeTurn = turn
	l.activePayload = turnPayload
	l.activeContext = turnCtx
	l.activeContextCause = nil
	l.nextTurn = turn + 1
	if turnCtx != nil && turnCtx.Done() != nil && l.onTurnContextDone != nil {
		l.stopActiveContext = context.AfterFunc(turnCtx, func() {
			cause := context.Cause(turnCtx)
			if cause == nil {
				cause = turnCtx.Err()
			}
			l.mu.Lock()
			active := l.activeTurn == turn && l.activeContext == turnCtx
			if active {
				l.activeContextCause = cause
			}
			onDone := l.onTurnContextDone
			l.mu.Unlock()
			if active && onDone != nil {
				onDone(cause)
			}
		})
	}
	return turn, nil
}

func (l *openAIWSPassthroughTurnLifecycle) finish(result *OpenAIForwardResult, turnErr error) (int, bool) {
	turn, finished, _ := l.finishWithError(result, turnErr)
	return turn, finished
}

func (l *openAIWSPassthroughTurnLifecycle) finishWithError(result *OpenAIForwardResult, turnErr error) (int, bool, error) {
	if l == nil {
		return 0, false, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	turn := l.activeTurn
	if turn <= 0 {
		return 0, false, nil
	}
	if l.stopActiveContext != nil {
		_ = l.stopActiveContext()
	}
	payload := l.activePayload
	contextCause := l.activeContextCause
	l.activeTurn = 0
	l.activePayload = nil
	l.activeContext = nil
	l.activeContextCause = nil
	l.stopActiveContext = nil
	if turnErr == nil && contextCause != nil {
		turnErr = contextCause
	}
	hookErr := finishOpenAIWSIngressTurn(l.hooks, turn, payload, result, turnErr)
	if hookErr != nil {
		l.afterTurnLifecycleErr = errors.Join(l.afterTurnLifecycleErr, hookErr)
	}
	return turn, true, hookErr
}

func (l *openAIWSPassthroughTurnLifecycle) hasActive() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.activeTurn > 0
}

func (l *openAIWSPassthroughTurnLifecycle) activeCause() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.activeContextCause
}

func (l *openAIWSPassthroughTurnLifecycle) lifecycleError() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.afterTurnLifecycleErr
}

func (c *openAIWSPolicyEnforcingFrameConn) WriteFrame(ctx context.Context, msgType coderws.MessageType, payload []byte) error {
	if c == nil || c.inner == nil {
		return errOpenAIWSConnClosed
	}
	return c.inner.WriteFrame(ctx, msgType, payload)
}

func (c *openAIWSPolicyEnforcingFrameConn) Close() error {
	if c == nil || c.inner == nil {
		return nil
	}
	return c.inner.Close()
}

// openAIWSPassthroughPolicyModelForFrame returns the upstream-perspective
// model name that should be passed to evaluateOpenAIFastPolicy for a single
// passthrough WS frame. Mirrors the HTTP-side normalization
// (account.GetMappedModel + normalizeOpenAIModelForUpstream) so the WS path
// matches model whitelists identically.
func openAIWSPassthroughPolicyModelForFrame(account *Account, payload []byte) string {
	if account == nil || len(payload) == 0 {
		return ""
	}
	original := strings.TrimSpace(gjson.GetBytes(payload, "model").String())
	if original == "" {
		return ""
	}
	return normalizeOpenAIModelForUpstream(account, account.GetMappedModel(original))
}

// openAIWSPassthroughPolicyModelFromSessionFrame returns the upstream model
// derived from a session.update frame's session.model field. Returns "" when
// the frame is not a session.update event or carries no session.model. Used
// by the per-frame policy filter (client→upstream direction) to keep
// capturedSessionModel in sync with the session-level model the client may
// rotate mid-session.
//
// Realtime / Responses WS lets the client change the session model after
// the WS handshake via:
//
//	{"type":"session.update","session":{"model":"gpt-5.5", ...}}
//
// If we only capture the model from the very first frame, a client can ship
// gpt-4o on the first response.create (whitelisted as pass), then
// session.update to gpt-5.5, then send response.create without "model" so
// the per-frame resolver returns "" and the stale capturedSessionModel falls
// back to gpt-4o — defeating the gpt-5.5 fast-policy filter.
func openAIWSPassthroughPolicyModelFromSessionFrame(account *Account, payload []byte) string {
	if account == nil || len(payload) == 0 {
		return ""
	}
	frameType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
	if frameType != "session.update" {
		return ""
	}
	original := strings.TrimSpace(gjson.GetBytes(payload, "session.model").String())
	if original == "" {
		return ""
	}
	return normalizeOpenAIModelForUpstream(account, account.GetMappedModel(original))
}

const openaiWSV2PassthroughModeFields = "ws_mode=passthrough ws_router=v2"

type openAIResponseImageBillingConfigStore struct {
	mu  sync.RWMutex
	cfg openAIResponseImageBillingConfig
}

func (s *openAIResponseImageBillingConfigStore) Store(cfg openAIResponseImageBillingConfig) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
}

func (s *openAIResponseImageBillingConfigStore) Load() openAIResponseImageBillingConfig {
	if s == nil {
		return openAIResponseImageBillingConfig{}
	}
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()
	return cfg
}

func openAIUsageFromWSV2Relay(usage openaiwsv2.Usage) OpenAIUsage {
	return OpenAIUsage{
		InputTokens:               usage.InputTokens,
		TextInputTokens:           usage.TextInputTokens,
		ImageInputTokens:          usage.ImageInputTokens,
		OutputTokens:              usage.OutputTokens,
		TextOutputTokens:          usage.TextOutputTokens,
		CacheCreationInputTokens:  usage.CacheCreationInputTokens,
		CacheReadInputTokens:      usage.CacheReadInputTokens,
		TextCacheReadInputTokens:  usage.TextCacheReadInputTokens,
		ImageCacheReadInputTokens: usage.ImageCacheReadInputTokens,
		ImageOutputTokens:         usage.ImageOutputTokens,
		ImageCount:                usage.ImageCount,
	}
}

func addOpenAIUsage(total *OpenAIUsage, delta OpenAIUsage) {
	if total == nil {
		return
	}
	total.InputTokens += delta.InputTokens
	total.TextInputTokens += delta.TextInputTokens
	total.ImageInputTokens += delta.ImageInputTokens
	total.OutputTokens += delta.OutputTokens
	total.TextOutputTokens += delta.TextOutputTokens
	total.CacheCreationInputTokens += delta.CacheCreationInputTokens
	total.CacheReadInputTokens += delta.CacheReadInputTokens
	total.TextCacheReadInputTokens += delta.TextCacheReadInputTokens
	total.ImageCacheReadInputTokens += delta.ImageCacheReadInputTokens
	total.ImageOutputTokens += delta.ImageOutputTokens
	total.ImageCount += delta.ImageCount
}

func subtractOpenAIUsage(total OpenAIUsage, settled OpenAIUsage) OpenAIUsage {
	nonNegativeDifference := func(value int, deducted int) int {
		if value <= deducted {
			return 0
		}
		return value - deducted
	}
	return OpenAIUsage{
		InputTokens:               nonNegativeDifference(total.InputTokens, settled.InputTokens),
		TextInputTokens:           nonNegativeDifference(total.TextInputTokens, settled.TextInputTokens),
		ImageInputTokens:          nonNegativeDifference(total.ImageInputTokens, settled.ImageInputTokens),
		OutputTokens:              nonNegativeDifference(total.OutputTokens, settled.OutputTokens),
		TextOutputTokens:          nonNegativeDifference(total.TextOutputTokens, settled.TextOutputTokens),
		CacheCreationInputTokens:  nonNegativeDifference(total.CacheCreationInputTokens, settled.CacheCreationInputTokens),
		CacheReadInputTokens:      nonNegativeDifference(total.CacheReadInputTokens, settled.CacheReadInputTokens),
		TextCacheReadInputTokens:  nonNegativeDifference(total.TextCacheReadInputTokens, settled.TextCacheReadInputTokens),
		ImageCacheReadInputTokens: nonNegativeDifference(total.ImageCacheReadInputTokens, settled.ImageCacheReadInputTokens),
		ImageOutputTokens:         nonNegativeDifference(total.ImageOutputTokens, settled.ImageOutputTokens),
		ImageCount:                nonNegativeDifference(total.ImageCount, settled.ImageCount),
	}
}

var _ openaiwsv2.FrameConn = (*openAIWSClientFrameConn)(nil)

func (c *openAIWSClientFrameConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	if c == nil || c.conn == nil {
		return coderws.MessageText, nil, errOpenAIWSConnClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return c.conn.Read(ctx)
}

func (c *openAIWSClientFrameConn) WriteFrame(ctx context.Context, msgType coderws.MessageType, payload []byte) error {
	if c == nil || c.conn == nil {
		return errOpenAIWSConnClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if msgType == coderws.MessageText {
		if normalized, changed := normalizeCompletedImageGenerationStatus(payload); changed {
			payload = normalized
		}
	}
	return c.conn.Write(ctx, msgType, payload)
}

func (c *openAIWSClientFrameConn) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	_ = c.conn.Close(coderws.StatusNormalClosure, "")
	_ = c.conn.CloseNow()
	return nil
}

func (s *OpenAIGatewayService) proxyResponsesWebSocketV2Passthrough(
	ctx context.Context,
	c *gin.Context,
	clientConn *coderws.Conn,
	account *Account,
	token string,
	firstClientMessage []byte,
	hooks *OpenAIWSIngressHooks,
	wsDecision OpenAIWSProtocolDecision,
) error {
	if s == nil {
		return errors.New("service is nil")
	}
	if clientConn == nil {
		return errors.New("client websocket is nil")
	}
	if account == nil {
		return errors.New("account is nil")
	}
	if err := validateOpenAIWSBearerToken(account, token); err != nil {
		return err
	}
	preparedFirstMessage, prepareErr := applyOpenAIWSFixedTurnModel(firstClientMessage, hooks)
	if prepareErr != nil {
		return prepareErr
	}
	firstClientMessage = preparedFirstMessage
	if account.IsOpenAIOAuth() && isOpenAIResponsesLiteWebSocketPayload(firstClientMessage) {
		liteFirstMessage, _, liteErr := normalizeOpenAIResponsesLiteToolsPayload(firstClientMessage)
		if liteErr != nil {
			return NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, liteErr.Error(), liteErr)
		}
		firstClientMessage = liteFirstMessage
	}
	requestModel := strings.TrimSpace(gjson.GetBytes(firstClientMessage, "model").String())
	requestPreviousResponseID := strings.TrimSpace(gjson.GetBytes(firstClientMessage, "previous_response_id").String())
	logOpenAIWSV2Passthrough(
		"relay_start account_id=%d model=%s previous_response_id=%s first_message_type=%s first_message_bytes=%d",
		account.ID,
		truncateOpenAIWSLogValue(requestModel, openAIWSLogValueMaxLen),
		truncateOpenAIWSLogValue(requestPreviousResponseID, openAIWSIDValueMaxLen),
		openaiwsv2RelayMessageTypeName(coderws.MessageText),
		len(firstClientMessage),
	)

	// Apply OpenAI Fast Policy on the first response.create frame. Subsequent
	// frames are filtered via a wrapping FrameConn below so every client→
	// upstream frame goes through the same policy evaluator/normalize/scope as
	// HTTP entrypoints.
	//
	// We capture the session-level model from the first frame here so the
	// per-frame filter (below) can fall back to it when a follow-up frame
	// omits "model" — Realtime clients are allowed to send response.create
	// without re-stating the model, in which case the upstream uses the model
	// negotiated at session.update time. Without this fallback, an empty
	// model would miss the default ["gpt-5.5","gpt-5.5*"] whitelist and be
	// silently passed through, defeating the policy on every frame after
	// the first.
	capturedSessionModel := openAIWSPassthroughPolicyModelForFrame(account, firstClientMessage)
	imageGenerationAllowed := GroupAllowsImageGeneration(apiKeyGroup(getAPIKeyFromContext(c)))
	if permissionErr := openAIWSImageGenerationPermissionError(imageGenerationAllowed, capturedSessionModel, firstClientMessage); permissionErr != nil {
		return permissionErr
	}
	updatedFirst, blocked, policyErr := s.applyOpenAIFastPolicyToWSResponseCreate(ctx, account, capturedSessionModel, firstClientMessage)
	if policyErr != nil {
		return fmt.Errorf("apply openai fast policy on first ws frame: %w", policyErr)
	}
	if blocked != nil {
		// coder/websocket@v1.8.14 Conn.Write is synchronous: it acquires
		// writeFrameMu, writes the entire frame, and Flushes the underlying
		// bufio writer before returning (write.go:42 → write.go:307-311).
		// The subsequent close handshake re-acquires the same writeFrameMu
		// to send the close frame, so the error event is guaranteed to
		// reach the kernel send buffer before any close frame is queued.
		// No explicit flush hop is required here.
		eventBytes := buildOpenAIFastPolicyBlockedWSEvent(blocked)
		if eventBytes != nil {
			writeCtx, cancelWrite := context.WithTimeout(ctx, s.openAIWSWriteTimeout())
			_ = clientConn.Write(writeCtx, coderws.MessageText, eventBytes)
			cancelWrite()
		}
		return NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, blocked.Message, blocked)
	}
	firstClientMessage = updatedFirst
	fingerprintedFirst, _, _, fingerprintErr := s.applyCodexFingerprintToRawBody(ctx, c, account, firstClientMessage)
	if fingerprintErr != nil {
		return fmt.Errorf("apply codex fingerprint on first ws frame: %w", fingerprintErr)
	}
	firstClientMessage = fingerprintedFirst
	cleanedFirst, cleanRelayState, _, cleanRelayErr := s.applyOpenAICleanRelayToRawBody(ctx, c, account, firstClientMessage, firstClientMessage)
	if cleanRelayErr != nil {
		return fmt.Errorf("apply openai clean relay on first ws frame: %w", cleanRelayErr)
	}
	if cleanRelayState != nil {
		firstClientMessage = cleanedFirst
	}

	// 在 policy filter 之后再提取 service_tier 用于 billing 上报：filter
	// 命中时 service_tier 已经从 firstClientMessage 中删除，billing 应当
	// 反映上游实际处理的 tier（nil = default），而不是用户最初请求的
	// "priority"。HTTP 入口（line ~2728 extractOpenAIServiceTier(reqBody)）
	// 与 WS ingress（openai_ws_forwarder.go:2991 取自 payload）的语义一致。
	//
	// 多轮 passthrough：OpenAI Realtime / Responses WS 协议允许客户端在
	// 同一连接的不同 response.create 帧上发送不同 service_tier（参考
	// codex-rs/core/src/client.rs build_responses_request 每次重新填值）。
	// 因此使用 atomic.Pointer[string] 在 filter（runClientToUpstream
	// goroutine）和 OnTurnComplete / final result（runUpstreamToClient
	// goroutine）之间同步当前 turn 的 service_tier。
	// extractOpenAIServiceTierFromBody 返回 *string，本身是指针类型，
	// 可直接 Store/Load 而无需额外封装。
	var requestServiceTierPtr atomic.Pointer[string]
	requestServiceTierPtr.Store(extractOpenAIServiceTierFromBody(firstClientMessage))
	imageBillingConfigStore := &openAIResponseImageBillingConfigStore{}
	imageBillingConfigStore.Store(resolveOpenAIResponseImageBillingConfigFromBody(openAIResponsesEndpoint, requestModel, firstClientMessage))

	wsURL, err := s.buildOpenAIResponsesWSURL(account)
	if err != nil {
		return fmt.Errorf("build ws url: %w", err)
	}
	wsHost := "-"
	wsPath := "-"
	if parsedURL, parseErr := url.Parse(wsURL); parseErr == nil && parsedURL != nil {
		wsHost = normalizeOpenAIWSLogValue(parsedURL.Host)
		wsPath = normalizeOpenAIWSLogValue(parsedURL.Path)
	}
	logOpenAIWSV2Passthrough(
		"relay_dial_start account_id=%d ws_host=%s ws_path=%s proxy_enabled=%v",
		account.ID,
		wsHost,
		wsPath,
		account.ProxyID != nil && account.Proxy != nil,
	)

	isCodexCLI := false
	if c != nil {
		isCodexCLI = openai.IsCodexOfficialClientByHeaders(c.GetHeader("User-Agent"), c.GetHeader("originator"))
	}
	if s.cfg != nil && s.cfg.Gateway.ForceCodexCLI {
		isCodexCLI = true
	}
	headers, _ := s.buildOpenAIWSHeaders(c, account, token, wsDecision, isCodexCLI, "", "", "")
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	dialer := s.getOpenAIWSPassthroughDialer()
	if dialer == nil {
		return errors.New("openai ws passthrough dialer is nil")
	}
	wsHeadersFactory, agentIdentityHeaderState := s.agentIdentityWSHeadersFactory(account)
	var (
		upstreamConn     openAIWSClientConn
		statusCode       int
		handshakeHeaders http.Header
	)
	agentIdentityTaskRecoveryTried := false
	for {
		dialCtx, cancelDial := context.WithTimeout(ctx, s.openAIWSDialTimeout())
		dialHeaders := cloneHeader(headers)
		if wsHeadersFactory != nil {
			var headerErr error
			dialHeaders, headerErr = wsHeadersFactory(dialCtx, dialHeaders)
			if headerErr != nil {
				cancelDial()
				return fmt.Errorf("build Agent Identity websocket headers: %w", headerErr)
			}
		}
		var err error
		upstreamConn, statusCode, handshakeHeaders, err = dialer.Dial(dialCtx, wsURL, dialHeaders, proxyURL)
		cancelDial()
		if err == nil {
			break
		}
		var handshakeErr *openAIWSHandshakeError
		var responseBody []byte
		if errors.As(err, &handshakeErr) && handshakeErr != nil {
			responseBody = append([]byte(nil), handshakeErr.Body...)
		}
		dialErr := &openAIWSDialError{StatusCode: statusCode, ResponseHeaders: cloneHeader(handshakeHeaders), ResponseBody: responseBody, Err: err}
		if account.IsOpenAIAgentIdentity() && !agentIdentityTaskRecoveryTried && isAgentIdentityTaskInvalidWSDialError(dialErr) {
			if recoveryErr := s.recoverAgentIdentityTask(ctx, account, agentIdentityHeaderState.expectedTaskID()); recoveryErr != nil {
				return fmt.Errorf("recover Agent Identity task: %w", recoveryErr)
			}
			agentIdentityTaskRecoveryTried = true
			continue
		}
		logOpenAIWSV2Passthrough(
			"relay_dial_failed account_id=%d status_code=%d err=%s",
			account.ID,
			statusCode,
			truncateOpenAIWSLogValue(err.Error(), openAIWSLogValueMaxLen),
		)
		return s.mapOpenAIWSPassthroughDialError(err, statusCode, handshakeHeaders)
	}
	defer func() {
		_ = upstreamConn.Close()
	}()
	logOpenAIWSV2Passthrough(
		"relay_dial_ok account_id=%d status_code=%d upstream_request_id=%s",
		account.ID,
		statusCode,
		openAIWSHeaderValueForLog(handshakeHeaders, "x-request-id"),
	)

	upstreamFrameConn, ok := upstreamConn.(openaiwsv2.FrameConn)
	if !ok {
		return errors.New("openai ws passthrough upstream connection does not support frame relay")
	}
	cyberDetectingUpstreamConn := &openAIWSCyberDetectingFrameConn{inner: upstreamFrameConn, c: c}

	completedTurns := atomic.Int32{}
	var completedUsageMu sync.Mutex
	var completedUsage OpenAIUsage
	turnLifecycle := newOpenAIWSPassthroughTurnLifecycleWithContext(ctx, hooks, func(cause error) {
		logOpenAIWSV2Passthrough(
			"turn_context_done account_id=%d cause=%s",
			account.ID,
			truncateOpenAIWSLogValue(relayErrorText(cause), openAIWSLogValueMaxLen),
		)
		_ = upstreamFrameConn.Close()
	})
	policyClientConn := &openAIWSPolicyEnforcingFrameConn{
		inner: &openAIWSClientFrameConn{conn: clientConn},
		// 注意线程安全：filter 仅在 runClientToUpstream 这一条
		// goroutine 中被调用（passthrough_relay.go: ReadFrame loop），
		// capturedSessionModel 的读写都发生在该 goroutine 内，因此无需
		// 加锁/原子化。
		filter: func(msgType coderws.MessageType, payload []byte) ([]byte, *OpenAIFastBlockedError, error) {
			if msgType != coderws.MessageText {
				return payload, nil, nil
			}
			preparedPayload, prepareErr := applyOpenAIWSFixedTurnModel(payload, hooks)
			if prepareErr != nil {
				return payload, nil, prepareErr
			}
			payload = preparedPayload
			if strings.TrimSpace(gjson.GetBytes(payload, "type").String()) == "response.create" &&
				account.IsOpenAIOAuth() && isOpenAIResponsesLiteWebSocketPayload(payload) {
				litePayload, _, liteErr := normalizeOpenAIResponsesLiteToolsPayload(payload)
				if liteErr != nil {
					return payload, nil, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, liteErr.Error(), liteErr)
				}
				payload = litePayload
			}
			// 在评估策略前先刷新 capturedSessionModel：客户端可能通过
			// session.update 修改 session-level model（Realtime /
			// Responses WS 协议允许），如果不刷新就会出现
			// "首帧 model=gpt-4o（pass）→ session.update 改成 gpt-5.5
			// → 不带 model 的 response.create fallback 到 gpt-4o" 的
			// 绕过路径。这里只看 session.update 事件中的 session.model
			// 字段，response.create 自己的 model 仍然由其本帧字段决定。
			if updated := openAIWSPassthroughPolicyModelFromSessionFrame(account, payload); updated != "" {
				capturedSessionModel = updated
			}
			// Per-frame model first; if the client omits "model" on a
			// follow-up frame (legal in Realtime), fall back to the
			// session-level model captured from the first frame so the
			// model whitelist still resolves. An empty model would miss
			// any whitelist and silently fall back to pass.
			model := openAIWSPassthroughPolicyModelForFrame(account, payload)
			if model == "" {
				model = capturedSessionModel
			}
			if permissionErr := openAIWSImageGenerationPermissionError(imageGenerationAllowed, model, payload); permissionErr != nil {
				return payload, nil, permissionErr
			}
			out, blocked, policyErr := s.applyOpenAIFastPolicyToWSResponseCreate(ctx, account, model, payload)
			if policyErr == nil && blocked == nil &&
				strings.TrimSpace(gjson.GetBytes(out, "type").String()) == "response.create" {
				fingerprintedOut, _, _, fingerprintErr := s.applyCodexFingerprintToRawBody(ctx, c, account, out)
				if fingerprintErr != nil {
					return out, nil, fingerprintErr
				}
				out = fingerprintedOut
				cleanedOut, _, _, cleanRelayErr := s.applyOpenAICleanRelayToRawBody(ctx, c, account, out, payload)
				if cleanRelayErr != nil {
					return out, nil, cleanRelayErr
				}
				out = cleanedOut
			}
			return out, blocked, policyErr
		},
		onBlock: func(blocked *OpenAIFastBlockedError) {
			// See note above on Conn.Write being synchronous w.r.t. flush;
			// no explicit flush is required to ensure the error event lands
			// before the close frame.
			eventBytes := buildOpenAIFastPolicyBlockedWSEvent(blocked)
			if eventBytes == nil {
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, s.openAIWSWriteTimeout())
			_ = clientConn.Write(writeCtx, coderws.MessageText, eventBytes)
			cancel()
		},
	}
	buildCompletedTurnResult := func(turn openaiwsv2.RelayTurnResult) *OpenAIForwardResult {
		turnUsage := openAIUsageFromWSV2Relay(turn.Usage)
		turnResult := &OpenAIForwardResult{
			RequestID:                            turn.RequestID,
			Usage:                                turnUsage,
			BillingUsageComplete:                 turn.BillingUsageComplete,
			Model:                                turn.RequestModel,
			UpstreamModel:                        turn.RequestModel,
			UpstreamResponseModel:                turn.ResponseModel,
			UpstreamResponseModelConflict:        turn.ResponseModelConflict,
			UpstreamResponseModelBillingEligible: turn.ResponseModelBillingEligible,
			ServiceTier:                          requestServiceTierPtr.Load(),
			Stream:                               true,
			OpenAIWSMode:                         true,
			ResponseHeaders:                      cloneHeader(handshakeHeaders),
			Duration:                             turn.Duration,
			FirstTokenMs:                         turn.FirstTokenMs,
		}
		applyOpenAIResponseImageAccounting(turnResult, imageBillingConfigStore.Load())
		return turnResult
	}
	var completedTurnNo atomic.Int32
	relayResult, relayExit := openaiwsv2.RunEntry(openaiwsv2.EntryInput{
		Ctx:                ctx,
		ClientConn:         policyClientConn,
		UpstreamConn:       cyberDetectingUpstreamConn,
		FirstClientMessage: firstClientMessage,
		Options: openaiwsv2.RelayOptions{
			WriteTimeout:     s.openAIWSWriteTimeout(),
			IdleTimeout:      s.openAIWSPassthroughIdleTimeout(),
			FirstMessageType: coderws.MessageText,
			OnUsageParseFailure: func(eventType string, usageRaw string) {
				logOpenAIWSV2Passthrough(
					"usage_parse_failed event_type=%s usage_raw=%s",
					truncateOpenAIWSLogValue(eventType, openAIWSLogValueMaxLen),
					truncateOpenAIWSLogValue(usageRaw, openAIWSLogValueMaxLen),
				)
			},
			BeforeClientFrame: func(msgType coderws.MessageType, payload []byte) error {
				if msgType != coderws.MessageText || strings.TrimSpace(gjson.GetBytes(payload, "type").String()) != "response.create" {
					return nil
				}
				if _, beginErr := turnLifecycle.begin(payload); beginErr != nil {
					return beginErr
				}
				// Update per-turn accounting only after lifecycle acquisition. A
				// rejected pipelined response.create must not overwrite the active
				// turn's service tier or image billing configuration.
				requestServiceTierPtr.Store(extractOpenAIServiceTierFromBody(payload))
				frameModel := strings.TrimSpace(gjson.GetBytes(payload, "model").String())
				if frameModel == "" {
					frameModel = requestModel
				}
				imageBillingConfigStore.Store(resolveOpenAIResponseImageBillingConfigFromBody(openAIResponsesEndpoint, frameModel, payload))
				return nil
			},
			BeforeTerminalFrame: func(turn openaiwsv2.RelayTurnResult) error {
				turnResult := buildCompletedTurnResult(turn)
				var turnErr error
				if strings.EqualFold(strings.TrimSpace(turn.TerminalEventType), "response.failed") {
					turnErr = errors.New("upstream websocket response failed")
				}
				turnNo, finished, hookErr := turnLifecycle.finishWithError(turnResult, turnErr)
				if !finished {
					return fmt.Errorf(
						"websocket terminal event %q has no active turn",
						strings.TrimSpace(turn.TerminalEventType),
					)
				}
				if hookErr != nil {
					return hookErr
				}
				completedTurnNo.Store(int32(turnNo))
				return nil
			},
			OnTurnComplete: func(turn openaiwsv2.RelayTurnResult) {
				turnResult := buildCompletedTurnResult(turn)
				turnUsage := turnResult.Usage
				turnNo := int(completedTurnNo.Swap(0))
				if turnNo <= 0 {
					logOpenAIWSV2Passthrough(
						"relay_terminal_without_active_turn account_id=%d request_id=%s terminal_event=%s",
						account.ID,
						truncateOpenAIWSLogValue(turnResult.RequestID, openAIWSIDValueMaxLen),
						truncateOpenAIWSLogValue(turn.TerminalEventType, openAIWSLogValueMaxLen),
					)
					return
				}
				completedUsageMu.Lock()
				addOpenAIUsage(&completedUsage, turnUsage)
				completedUsageMu.Unlock()
				completedTurns.Add(1)
				logOpenAIWSV2Passthrough(
					"relay_turn_completed account_id=%d turn=%d request_id=%s terminal_event=%s duration_ms=%d first_token_ms=%d input_tokens=%d output_tokens=%d cache_read_tokens=%d",
					account.ID,
					turnNo,
					truncateOpenAIWSLogValue(turnResult.RequestID, openAIWSIDValueMaxLen),
					truncateOpenAIWSLogValue(turn.TerminalEventType, openAIWSLogValueMaxLen),
					turnResult.Duration.Milliseconds(),
					openAIWSFirstTokenMsForLog(turnResult.FirstTokenMs),
					turnResult.Usage.InputTokens,
					turnResult.Usage.OutputTokens,
					turnResult.Usage.CacheReadInputTokens,
				)
			},
			OnTrace: func(event openaiwsv2.RelayTraceEvent) {
				logOpenAIWSV2Passthrough(
					"relay_trace account_id=%d stage=%s direction=%s msg_type=%s bytes=%d graceful=%v wrote_downstream=%v err=%s",
					account.ID,
					truncateOpenAIWSLogValue(event.Stage, openAIWSLogValueMaxLen),
					truncateOpenAIWSLogValue(event.Direction, openAIWSLogValueMaxLen),
					truncateOpenAIWSLogValue(event.MessageType, openAIWSLogValueMaxLen),
					event.PayloadBytes,
					event.Graceful,
					event.WroteDownstream,
					truncateOpenAIWSLogValue(event.Error, openAIWSLogValueMaxLen),
				)
			},
		},
	})

	result := &OpenAIForwardResult{
		RequestID:                            relayResult.RequestID,
		Usage:                                openAIUsageFromWSV2Relay(relayResult.Usage),
		BillingUsageComplete:                 relayResult.BillingUsageComplete,
		Model:                                relayResult.RequestModel,
		UpstreamModel:                        relayResult.RequestModel,
		UpstreamResponseModel:                relayResult.ResponseModel,
		UpstreamResponseModelConflict:        relayResult.ResponseModelConflict,
		UpstreamResponseModelBillingEligible: relayResult.ResponseModelBillingEligible,
		ServiceTier:                          requestServiceTierPtr.Load(),
		Stream:                               true,
		OpenAIWSMode:                         true,
		ResponseHeaders:                      cloneHeader(handshakeHeaders),
		Duration:                             relayResult.Duration,
		FirstTokenMs:                         relayResult.FirstTokenMs,
	}
	applyOpenAIResponseImageAccounting(result, imageBillingConfigStore.Load())
	buildUnsettledTurnResult := func() *OpenAIForwardResult {
		completedUsageMu.Lock()
		settledUsage := completedUsage
		completedUsageMu.Unlock()
		unsettledUsage := subtractOpenAIUsage(result.Usage, settledUsage)
		partial := &OpenAIForwardResult{
			Usage:         unsettledUsage,
			Model:         relayResult.RequestModel,
			UpstreamModel: relayResult.RequestModel,
			// relayResult summarizes the last terminal response. An unsettled active
			// turn must not inherit that turn's audit model or billing eligibility.
			UpstreamResponseModelBillingEligible: false,
			ServiceTier:                          requestServiceTierPtr.Load(),
			Stream:                               true,
			OpenAIWSMode:                         true,
			ResponseHeaders:                      cloneHeader(handshakeHeaders),
			Duration:                             relayResult.Duration,
			FirstTokenMs:                         relayResult.FirstTokenMs,
		}
		applyOpenAIResponseImageAccounting(partial, imageBillingConfigStore.Load())
		if !OpenAIForwardResultHasBillableUsage(partial) {
			return nil
		}
		return partial
	}

	turnCount := int(completedTurns.Load())
	if relayExit == nil {
		if lifecycleErr := turnLifecycle.lifecycleError(); lifecycleErr != nil {
			return lifecycleErr
		}
		if turnLifecycle.hasActive() {
			turnErr := wrapOpenAIWSIngressTurnError(
				"incomplete_turn",
				errors.New("upstream websocket closed before a terminal response event"),
				relayResult.UpstreamToClientFrames > 0,
			)
			if _, _, hookErr := turnLifecycle.finishWithError(buildUnsettledTurnResult(), turnErr); hookErr != nil {
				return errors.Join(turnErr, hookErr)
			}
			return turnErr
		}
		logOpenAIWSV2Passthrough(
			"relay_completed account_id=%d request_id=%s terminal_event=%s duration_ms=%d c2u_frames=%d u2c_frames=%d dropped_frames=%d turns=%d",
			account.ID,
			truncateOpenAIWSLogValue(result.RequestID, openAIWSIDValueMaxLen),
			truncateOpenAIWSLogValue(relayResult.TerminalEventType, openAIWSLogValueMaxLen),
			result.Duration.Milliseconds(),
			relayResult.ClientToUpstreamFrames,
			relayResult.UpstreamToClientFrames,
			relayResult.DroppedDownstreamFrames,
			turnCount,
		)
		return nil
	}
	logOpenAIWSV2Passthrough(
		"relay_failed account_id=%d stage=%s wrote_downstream=%v err=%s duration_ms=%d c2u_frames=%d u2c_frames=%d dropped_frames=%d turns=%d",
		account.ID,
		truncateOpenAIWSLogValue(relayExit.Stage, openAIWSLogValueMaxLen),
		relayExit.WroteDownstream,
		truncateOpenAIWSLogValue(relayErrorText(relayExit.Err), openAIWSLogValueMaxLen),
		result.Duration.Milliseconds(),
		relayResult.ClientToUpstreamFrames,
		relayResult.UpstreamToClientFrames,
		relayResult.DroppedDownstreamFrames,
		turnCount,
	)

	relayErr := relayExit.Err
	if relayExit.Stage == "idle_timeout" {
		relayErr = NewOpenAIWSClientCloseError(
			coderws.StatusPolicyViolation,
			"client websocket idle timeout",
			relayErr,
		)
	}
	turnErr := wrapOpenAIWSIngressTurnError(
		relayExit.Stage,
		relayErr,
		relayExit.WroteDownstream,
	)
	if cause := turnLifecycle.activeCause(); cause != nil {
		turnErr = wrapOpenAIWSIngressTurnError(relayExit.Stage, cause, relayExit.WroteDownstream)
	}
	_, _, _ = turnLifecycle.finishWithError(buildUnsettledTurnResult(), turnErr)
	if lifecycleErr := turnLifecycle.lifecycleError(); lifecycleErr != nil {
		return errors.Join(turnErr, lifecycleErr)
	}
	return turnErr
}

func (s *OpenAIGatewayService) mapOpenAIWSPassthroughDialError(
	err error,
	statusCode int,
	handshakeHeaders http.Header,
) error {
	if err == nil {
		return nil
	}
	wrappedErr := err
	var dialErr *openAIWSDialError
	if !errors.As(err, &dialErr) {
		wrappedErr = &openAIWSDialError{
			StatusCode:      statusCode,
			ResponseHeaders: cloneHeader(handshakeHeaders),
			Err:             err,
		}
	}

	if errors.Is(err, context.Canceled) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return NewOpenAIWSClientCloseError(
			coderws.StatusTryAgainLater,
			"upstream websocket connect timeout",
			wrappedErr,
		)
	}
	if statusCode == http.StatusTooManyRequests {
		return NewOpenAIWSClientCloseError(
			coderws.StatusTryAgainLater,
			"upstream websocket is busy, please retry later",
			wrappedErr,
		)
	}
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		return NewOpenAIWSClientCloseError(
			coderws.StatusPolicyViolation,
			"upstream websocket authentication failed",
			wrappedErr,
		)
	}
	if statusCode >= http.StatusBadRequest && statusCode < http.StatusInternalServerError {
		return NewOpenAIWSClientCloseError(
			coderws.StatusPolicyViolation,
			"upstream websocket handshake rejected",
			wrappedErr,
		)
	}
	return fmt.Errorf("openai ws passthrough dial: %w", wrappedErr)
}

func openaiwsv2RelayMessageTypeName(msgType coderws.MessageType) string {
	switch msgType {
	case coderws.MessageText:
		return "text"
	case coderws.MessageBinary:
		return "binary"
	default:
		return fmt.Sprintf("unknown(%d)", msgType)
	}
}

func relayErrorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func openAIWSFirstTokenMsForLog(firstTokenMs *int) int {
	if firstTokenMs == nil {
		return -1
	}
	return *firstTokenMs
}

func logOpenAIWSV2Passthrough(format string, args ...any) {
	logger.LegacyPrintf(
		"service.openai_ws_v2",
		"[OpenAI WS v2 passthrough] %s "+format,
		append([]any{openaiWSV2PassthroughModeFields}, args...)...,
	)
}
