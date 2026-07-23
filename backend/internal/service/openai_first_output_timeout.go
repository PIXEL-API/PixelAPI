package service

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
)

const (
	openAIFirstOutputStageMemoryLimit        = 64 * 1024
	openAIFirstOutputStageMaxBytes           = 8 * 1024 * 1024
	openAIFirstOutputScannerFramingAllowance = 64
	openAIFirstOutputGuardQueueSize          = 1
	openAIDefaultStreamQueueSize             = 16
	openAIFirstOutputPhaseRoutingBudget      = "routing_budget"
	openAIFirstOutputPhaseResponseHeaders    = "response_headers"
	openAIFirstOutputPhaseSemanticOutput     = "semantic_output"
)

var (
	errOpenAIFirstOutputStageLimit            = errors.New("openai first-output staging limit exceeded")
	errOpenAIFirstOutputScannerLimit          = errors.New("openai pre-output scanner token limit exceeded")
	ErrOpenAIFirstOutputRoutingBudgetExceeded = errors.New("openai first-output routing budget exceeded")
)

// openAIFirstOutputStartContextKey carries the end-to-end gateway routing start
// time from the HTTP handler into every upstream retry. ForwardWithAnalysis is
// called after account selection, so using time.Now() there would exclude
// queueing and account selection from the first-output budget.
type openAIFirstOutputStartContextKey struct{}

type openAIFirstOutputBudgetContextKey struct{}

type openAIFirstOutputBudgetContextValue struct {
	enabled  bool
	deadline time.Time
}

// WithOpenAIFirstOutputStart attaches the routing start time used by the
// native OpenAI Responses first-output guard.  A zero value is ignored so a
// caller cannot accidentally replace a valid start time with an invalid one.
func WithOpenAIFirstOutputStart(ctx context.Context, start time.Time) context.Context {
	if ctx == nil || start.IsZero() {
		return ctx
	}
	return context.WithValue(ctx, openAIFirstOutputStartContextKey{}, start)
}

func openAIFirstOutputStart(ctx context.Context) time.Time {
	if ctx != nil {
		if start, ok := ctx.Value(openAIFirstOutputStartContextKey{}).(time.Time); ok && !start.IsZero() {
			return start
		}
	}
	return time.Now()
}

// ensureOpenAIFirstOutputStart makes the fallback start time stable for
// service callers that invoke ForwardWithAnalysis directly instead of going
// through an HTTP handler. Without this, the header guard and stream timer
// could each choose a different "now" and silently extend the budget.
func ensureOpenAIFirstOutputStart(ctx context.Context) (context.Context, time.Time) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx != nil {
		if start, ok := ctx.Value(openAIFirstOutputStartContextKey{}).(time.Time); ok && !start.IsZero() {
			return ctx, start
		}
	}
	start := time.Now()
	return WithOpenAIFirstOutputStart(ctx, start), start
}

// WithOpenAIFirstOutputBudget stores an absolute, request-scoped routing
// budget without putting a deadline on the request context itself. The latter
// is intentional: once semantic output starts, the normal long-lived stream
// must not be cancelled by the pre-output budget. A non-positive timeout
// explicitly disables the budget and shadows any inherited upper bound.
func WithOpenAIFirstOutputBudget(ctx context.Context, timeout time.Duration) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	value := openAIFirstOutputBudgetContextValue{}
	if timeout > 0 {
		value.enabled = true
		value.deadline = openAIFirstOutputStart(ctx).Add(timeout)
	}
	return context.WithValue(ctx, openAIFirstOutputBudgetContextKey{}, value)
}

// OpenAIFirstOutputBudgetRemaining returns the remaining routing budget and
// whether a budget is enabled. A negative/zero duration means the budget has
// expired; callers should fail fast instead of starting another wait/dial.
func OpenAIFirstOutputBudgetRemaining(ctx context.Context) (time.Duration, bool) {
	if ctx == nil {
		return 0, false
	}
	value, ok := ctx.Value(openAIFirstOutputBudgetContextKey{}).(openAIFirstOutputBudgetContextValue)
	if !ok || !value.enabled || value.deadline.IsZero() {
		return 0, false
	}
	return time.Until(value.deadline), true
}

// CapOpenAIFirstOutputWait applies the remaining routing budget to a local
// wait timeout. It never changes the caller's context or affects streaming
// after the first semantic output.
func CapOpenAIFirstOutputWait(ctx context.Context, requested time.Duration) time.Duration {
	remaining, enabled := OpenAIFirstOutputBudgetRemaining(ctx)
	if !enabled || requested <= 0 {
		return requested
	}
	if remaining <= 0 {
		return time.Nanosecond
	}
	if remaining < requested {
		return remaining
	}
	return requested
}

// WithOpenAIFirstOutputRoutingDeadline converts the soft request budget into a
// cancellable child context for routing-only work (moderation, cache/DB lookup,
// and account selection). Callers must cancel it before starting the upstream
// request so the deadline can never terminate an already-producing stream.
func WithOpenAIFirstOutputRoutingDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	remaining, enabled := OpenAIFirstOutputBudgetRemaining(ctx)
	if !enabled {
		return ctx, func() {}
	}
	if remaining <= 0 {
		remaining = time.Nanosecond
	}
	return context.WithTimeoutCause(ctx, remaining, ErrOpenAIFirstOutputRoutingBudgetExceeded)
}

func (s *OpenAIGatewayService) OpenAIFirstOutputRoutingBudget(body []byte, modelCandidates ...string) time.Duration {
	if s == nil || s.cfg == nil {
		return 0
	}
	effort := extractOpenAIReasoningEffortFromBody(body, modelCandidates...)
	effortValue := ""
	if effort != nil {
		effortValue = *effort
	}
	return s.openAIFirstOutputTimeout(effortValue)
}

func (s *OpenAIGatewayService) OpenAIFirstOutputBudgetForAccount(account *Account, body []byte, modelCandidates ...string) time.Duration {
	if s == nil || account == nil || account.Platform != PlatformOpenAI {
		return 0
	}
	effort := extractOpenAIReasoningEffortFromBody(body, modelCandidates...)
	effortValue := ""
	if effort != nil {
		effortValue = *effort
	}
	return s.openAIFirstOutputTimeout(effortValue)
}

type openAIFirstOutputStage struct {
	limit      int64
	size       int64
	memory     bytes.Buffer
	tempFile   *os.File
	tempPath   string
	createTemp func() (*os.File, error)
	removeFile func(string) error
	memoryOnly bool
	cleanupErr error
	closed     bool
}

func newOpenAIFirstOutputStage(limit int64) *openAIFirstOutputStage {
	if limit < 1 {
		limit = 1
	}
	return &openAIFirstOutputStage{
		limit:      limit,
		createTemp: func() (*os.File, error) { return os.CreateTemp("", "sub2api-openai-first-output-*") },
		removeFile: os.Remove,
		memoryOnly: runtime.GOOS == "windows",
	}
}

func newDefaultOpenAIFirstOutputStage() *openAIFirstOutputStage {
	return newOpenAIFirstOutputStage(openAIFirstOutputStageMaxBytes)
}

func openAIFirstOutputEventQueueSize(guardFirstOutput bool) int {
	if guardFirstOutput {
		return openAIFirstOutputGuardQueueSize
	}
	return openAIDefaultStreamQueueSize
}

func openAIFirstOutputDynamicScanLines(guardActive *atomic.Bool) bufio.SplitFunc {
	return func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		advance, token, err = bufio.ScanLines(data, atEOF)
		if err != nil || guardActive == nil || !guardActive.Load() {
			return advance, token, err
		}
		limit := openAIFirstOutputStageMaxBytes + openAIFirstOutputScannerFramingAllowance
		if token != nil {
			if len(token) > limit {
				return 0, nil, errOpenAIFirstOutputScannerLimit
			}
			return advance, token, nil
		}
		// 首输出保护开启时，在 Scanner 向 MaxLineSize 扩容前快速失败。
		if len(data) >= limit {
			return 0, nil, errOpenAIFirstOutputScannerLimit
		}
		return advance, token, nil
	}
}

func (s *openAIFirstOutputStage) Buffered() int64 {
	if s == nil {
		return 0
	}
	return s.size
}

func (s *openAIFirstOutputStage) WriteString(value string) (int, error) {
	if err := s.prepareWrite(len(value)); err != nil {
		return 0, err
	}
	var n int
	var err error
	if s.tempFile == nil {
		n, err = s.memory.WriteString(value)
	} else {
		n, err = io.WriteString(s.tempFile, value)
	}
	s.size += int64(n)
	if err != nil {
		return n, fmt.Errorf("write first-output stage: %w", err)
	}
	return n, nil
}

func (s *openAIFirstOutputStage) Write(p []byte) (int, error) {
	if err := s.prepareWrite(len(p)); err != nil {
		return 0, err
	}
	var n int
	var err error
	if s.tempFile == nil {
		n, err = s.memory.Write(p)
	} else {
		n, err = s.tempFile.Write(p)
	}
	s.size += int64(n)
	if err != nil {
		return n, fmt.Errorf("write first-output stage: %w", err)
	}
	return n, nil
}

func (s *openAIFirstOutputStage) prepareWrite(incoming int) error {
	if s == nil || s.closed {
		return os.ErrClosed
	}
	if int64(incoming) > s.limit-s.size {
		return fmt.Errorf("%w: buffered=%d incoming=%d limit=%d", errOpenAIFirstOutputStageLimit, s.size, incoming, s.limit)
	}
	if s.tempFile != nil || s.memoryOnly || s.size+int64(incoming) <= openAIFirstOutputStageMemoryLimit {
		return nil
	}
	file, err := s.createTemp()
	if err != nil {
		return fmt.Errorf("create first-output spool: %w", err)
	}
	path := file.Name()
	// Unix 下在写入请求数据前解除文件名关联，进程异常退出也不会留下明文暂存文件。
	if unlinkErr := s.removeFile(path); unlinkErr != nil {
		closeErr := file.Close()
		removeErr := s.removeFile(path)
		if errors.Is(removeErr, os.ErrNotExist) {
			removeErr = nil
		}
		if removeErr != nil {
			s.tempPath = path
		}
		return errors.Join(
			fmt.Errorf("unlink first-output spool before use: %w", unlinkErr),
			closeErr,
			removeErr,
		)
	}
	if _, err := file.Write(s.memory.Bytes()); err != nil {
		_ = file.Close()
		return fmt.Errorf("initialize first-output spool: %w", err)
	}
	s.tempFile = file
	s.tempPath = path
	s.memory.Reset()
	return nil
}

func (s *openAIFirstOutputStage) CommitTo(dst io.Writer) error {
	if s == nil || s.closed {
		return os.ErrClosed
	}
	if s.tempFile == nil {
		if _, err := io.Copy(dst, bytes.NewReader(s.memory.Bytes())); err != nil {
			return err
		}
	} else {
		if _, err := s.tempFile.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("seek first-output spool: %w", err)
		}
		if _, err := io.CopyN(dst, s.tempFile, s.size); err != nil {
			return err
		}
	}
	if err := s.Close(); err != nil {
		// 数据已成功提交，清理错误留给 defer 记录，避免把已提交响应误判为流错误。
		s.cleanupErr = errors.Join(s.cleanupErr, err)
	}
	return nil
}

func (s *openAIFirstOutputStage) Close() error {
	if s == nil {
		return nil
	}
	if s.closed && s.tempFile == nil && s.tempPath == "" && s.cleanupErr == nil {
		return nil
	}
	s.closed = true
	s.size = 0
	s.memory.Reset()
	closeErr := s.cleanupErr
	s.cleanupErr = nil
	if s.tempFile != nil {
		closeErr = errors.Join(closeErr, s.tempFile.Close())
		s.tempFile = nil
	}
	if s.tempPath != "" {
		removeErr := s.removeFile(s.tempPath)
		if removeErr == nil || errors.Is(removeErr, os.ErrNotExist) {
			s.tempPath = ""
		} else {
			closeErr = errors.Join(closeErr, removeErr)
		}
	}
	return closeErr
}

func (s *OpenAIGatewayService) openAIFirstOutputTimeout(reasoningEffort string) time.Duration {
	if s == nil || s.cfg == nil || s.cfg.Gateway.OpenAIFirstOutputTimeoutSeconds <= 0 {
		return 0
	}
	seconds := s.cfg.Gateway.OpenAIFirstOutputTimeoutSeconds
	switch strings.ToLower(strings.TrimSpace(reasoningEffort)) {
	case "high", "xhigh", "max":
		if override := s.cfg.Gateway.OpenAIHighEffortFirstOutputTimeoutSeconds; override > 0 {
			seconds = override
		}
	}
	return time.Duration(seconds) * time.Second
}

func (s *OpenAIGatewayService) newOpenAIFirstOutputTimeoutError(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	startTime time.Time,
	originalModel string,
	reasoningEffort string,
	timeout time.Duration,
	phase string,
	responseHeaders http.Header,
) *UpstreamFailoverError {
	elapsed := time.Since(startTime)
	logger.LegacyPrintf(
		"service.openai_gateway",
		"OpenAI first output timeout: account=%d model=%s effort=%s phase=%s elapsed=%s limit=%s",
		account.ID, originalModel, reasoningEffort, phase, elapsed, timeout,
	)
	requestID := strings.TrimSpace(responseHeaders.Get("x-request-id"))
	eventMessage := "OpenAI upstream produced no semantic output before the deadline"
	responseType := "first_output_timeout"
	responseMessage := "Upstream produced no output before the deadline"
	if phase == openAIFirstOutputPhaseRoutingBudget {
		eventMessage = "OpenAI gateway routing budget expired before an upstream attempt"
		responseType = "routing_budget_exhausted"
		responseMessage = "Gateway routing budget expired before an upstream attempt could start"
	}
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
		UpstreamStatusCode: http.StatusGatewayTimeout, UpstreamRequestID: requestID,
		Kind: responseType, Message: eventMessage,
		Detail: fmt.Sprintf("phase=%s elapsed_ms=%d timeout_ms=%d", phase, elapsed.Milliseconds(), timeout.Milliseconds()),
	})
	// Do not call RateLimitService.HandleStreamTimeout here. That method models
	// an idle stream-data timeout and may synchronously update persistent
	// account state; a first-output deadline is a separate signal. The handler
	// still reports genuine upstream failures to the scheduler's EWMA/error
	// stats, while routing-budget exhaustion is explicitly excluded below.
	reason := GatewayFailureReasonOpenAIFirstOutputTimeout
	scope := GatewayFailureScopeProvider
	nextAccountAction := NextAccountLegacyRetry
	if phase == openAIFirstOutputPhaseRoutingBudget {
		reason = GatewayFailureReasonRoutingBudgetExhausted
		scope = GatewayFailureScopeRequest
		// No account can recover an already exhausted absolute budget. Stop here
		// so account-switch metrics and selection work are not polluted by a
		// retry that cannot reach the upstream.
		nextAccountAction = NextAccountStop
	}
	return &UpstreamFailoverError{
		StatusCode:               http.StatusGatewayTimeout,
		ResponseBody:             []byte(fmt.Sprintf(`{"error":{"type":%q,"message":%q}}`, responseType, responseMessage)),
		ResponseHeaders:          responseHeaders.Clone(),
		SafeToFailoverAfterWrite: true,
		Scope:                    scope,
		Reason:                   reason,
		NextAccountAction:        nextAccountAction,
	}
}

type openAIFirstOutputHeaderGuard struct {
	cancel  context.CancelFunc
	release context.CancelFunc
	timer   *time.Timer
	fired   chan struct{}
	once    sync.Once
}

func newOpenAIFirstOutputHeaderGuard(
	ctx context.Context,
	release context.CancelFunc,
	deadline time.Time,
) (context.Context, *openAIFirstOutputHeaderGuard) {
	guardedCtx, cancel := context.WithCancel(ctx)
	guard := &openAIFirstOutputHeaderGuard{cancel: cancel, release: release, fired: make(chan struct{})}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		remaining = time.Nanosecond
	}
	guard.timer = time.AfterFunc(remaining, func() {
		close(guard.fired)
		cancel()
	})
	return guardedCtx, guard
}

func (g *openAIFirstOutputHeaderGuard) stopHeaderWait() bool {
	if g.timer.Stop() {
		return false
	}
	<-g.fired
	return true
}

func (g *openAIFirstOutputHeaderGuard) close() {
	g.once.Do(func() {
		g.timer.Stop()
		g.cancel()
		g.release()
	})
}

type openAIRequestContextReadCloser struct {
	io.ReadCloser
	cleanup func()
	once    sync.Once
	err     error
}

func (r *openAIRequestContextReadCloser) Close() error {
	r.once.Do(func() {
		r.cleanup()
		r.err = r.ReadCloser.Close()
	})
	return r.err
}
