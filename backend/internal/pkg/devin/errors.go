package devin

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"

	"connectrpc.com/connect"
)

// Failure 是 Devin 上游错误的结构化分类，供网关层映射 HTTP 响应与
// 账号调度决策（是否换账号、是否标记凭证失效）。
type Failure struct {
	StatusCode int    // 建议的下游 HTTP 状态码
	Code       string // connect code 或本地分类标记
	Message    string
	// Retryable 表示该错误可换账号重试（5xx/传输断裂/429）。
	Retryable bool
	// CredentialFailed 表示凭证失效类错误（unauthenticated），
	// 网关层应把账号标记为 error 并排除本次调度。
	CredentialFailed bool
	// ClientFault 表示请求形状问题（invalid_argument 等），换账号无意义，
	// 应把原始错误透传给客户端修正请求。
	ClientFault bool
}

func (f *Failure) Error() string { return f.Message }

// Classify 把任意错误归一化为 *Failure。context 取消/超时保持原样返回 nil。
func Classify(err error) *Failure {
	if err == nil {
		return nil
	}
	var failure *Failure
	if errors.As(err, &failure) {
		return failure
	}
	if errors.Is(err, context.Canceled) {
		return &Failure{StatusCode: 499, Code: "canceled", Message: "request canceled"}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Failure{StatusCode: 504, Code: "deadline_exceeded", Message: "upstream timeout", Retryable: true}
	}
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return classifyConnectError(connectErr)
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return &Failure{StatusCode: 502, Code: "upstream_transport", Message: err.Error(), Retryable: true}
	}
	return &Failure{StatusCode: 502, Code: "upstream_error", Message: err.Error()}
}

// classifyConnectError 把 connect code 映射到 HTTP 语义。
// resource_exhausted 的 retry hint 文案（"retry in Ns" 等）由上游携带在
// message 里，不在此处解析——调用方需要 Retry-After 时可自行从 Message 提取。
func classifyConnectError(err *connect.Error) *Failure {
	message := err.Message()
	failure := &Failure{Code: err.Code().String(), Message: message}
	switch err.Code() {
	case connect.CodeCanceled:
		failure.StatusCode = 499
	case connect.CodeDeadlineExceeded:
		failure.StatusCode, failure.Retryable = 504, true
	case connect.CodeInvalidArgument, connect.CodeOutOfRange, connect.CodeFailedPrecondition:
		failure.StatusCode, failure.ClientFault = 400, true
	case connect.CodeUnauthenticated:
		// 上游抖动会把内部错误包装成 unauthenticated/permission_denied
		// （如 "an internal error occurred"），此类按可重试上游错误处理，
		// 不能把账号标成凭证失效。
		if isUpstreamInternalError(message) {
			failure.StatusCode, failure.Retryable = 502, true
		} else {
			failure.StatusCode, failure.CredentialFailed = 401, true
		}
	case connect.CodePermissionDenied:
		// 上游对「内容策略拦截」和「凭证无权限」都回 permission_denied。
		// 前者是请求级拒绝，按客户端错误处理；不能把账号标成凭证失效。
		if isContentPolicyRejection(message) {
			failure.StatusCode, failure.ClientFault = 400, true
		} else if isUpstreamInternalError(message) {
			failure.StatusCode, failure.Retryable = 502, true
		} else {
			failure.StatusCode, failure.CredentialFailed = 403, true
		}
	case connect.CodeNotFound:
		failure.StatusCode = 404
	case connect.CodeResourceExhausted:
		failure.StatusCode, failure.Retryable = 429, true
	case connect.CodeAlreadyExists, connect.CodeAborted:
		failure.StatusCode, failure.Retryable = 409, true
	case connect.CodeUnavailable:
		// 无底层错误的 unavailable 是上游语义拒绝；传输断裂在
		// IsTransientConnectError 已过滤，这里仍按可重试处理。
		failure.StatusCode, failure.Retryable = 503, true
	case connect.CodeUnimplemented:
		failure.StatusCode = 501
	case connect.CodeInternal, connect.CodeDataLoss:
		failure.StatusCode, failure.Retryable = 502, true
	default:
		failure.StatusCode, failure.Retryable = 502, true
	}
	if message == "" {
		failure.Message = "devin upstream error: " + failure.Code
	}
	return failure
}

var http2TransportMarkers = []string{
	"stream error", "RST_STREAM", "GOAWAY", "refused stream",
	"ENHANCE_YOUR_CALM", "http2:",
}

// IsTransientConnectError 判断错误是否为传输层断裂（建流重试安全）。
// connect-go 会把 RoundTrip/读写失败统一包成 connect.Error，判据要看
// unwrap 链里的 io/net 错误与固定措辞，而不是 code 本身。
func IsTransientConnectError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var failure *Failure
	if errors.As(err, &failure) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		return true
	}
	message := connectErr.Message()
	for _, marker := range http2TransportMarkers {
		if strings.Contains(message, marker) {
			return true
		}
	}
	// h1 连接池复用到对端已关闭的空闲连接的固定措辞，尚未写出任何字节。
	if strings.Contains(message, "server closed idle connection") {
		return true
	}
	// connect-go 对本地帧解析失败的固定措辞（envelope 截断/垃圾 flag）。
	if strings.HasPrefix(message, "protocol error:") {
		return true
	}
	return false
}

// isContentPolicyRejection 识别上游内容审核拦截文案（permission_denied 复用场景），
// 例如 "Your request was blocked by our content policy..."。
func isContentPolicyRejection(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "content policy") || strings.Contains(lower, "content_policy")
}

// isUpstreamInternalError 识别上游内部错误被包装成 auth 类 code 的措辞，
// 例如 "an internal error occurred (trace ID: ...)"。
func isUpstreamInternalError(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "internal error") || strings.Contains(lower, "internal_error")
}
