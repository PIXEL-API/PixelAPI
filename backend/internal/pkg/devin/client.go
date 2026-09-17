package devin

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	"github.com/Wei-Shaw/sub2api/internal/pkg/devinproto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/devinproto/devinprotoconnect"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyutil"
)

// DefaultBaseURL 是 Devin CLI 使用的上游 Connect 端点。
const DefaultBaseURL = "https://server.codeium.com"

const (
	// 与真实 Devin CLI 抓包一致的默认客户端身份。
	defaultClientName    = "chisel"
	defaultClientVersion = "3000.2.17"
	defaultClientOS      = "mac"

	// metadataFingerprintBytes 是 metadata.F 随机指纹的字节数（与 CLI 抓包一致）。
	metadataFingerprintBytes = 366

	// maxConnectAttempts 是建流阶段对传输级错误的最大尝试次数。
	maxConnectAttempts = 3

	// apiCallTimeout 是普通一元调用（模型目录、AssignModel）的整体超时。
	apiCallTimeout = 120 * time.Second

	// modelsCacheTTL 是模型目录的缓存时长。
	modelsCacheTTL = 5 * time.Minute
	// modelsErrorCooldown 是目录拉取失败后的冷却窗口，避免每个请求都重试一次。
	modelsErrorCooldown = 30 * time.Second
)

// ClientConfig 是单个 Devin 账号对应的上游连接配置。
type ClientConfig struct {
	BaseURL  string // 缺省 DefaultBaseURL
	Token    string // session token（devin-session-token$... 或同类），不会写日志
	ProxyURL string // 可选 http/https/socks5 代理
}

// Client 是一个 Devin 账号的上游调用入口：stream/api 两个 Connect 客户端
// 共享同一 transport（同一连接池与代理配置）。
type Client struct {
	cfg       ClientConfig
	transport *http.Transport
	stream    devinprotoconnect.ApiServerServiceClient
	api       devinprotoconnect.ApiServerServiceClient

	modelsMu         sync.Mutex
	models           []ModelInfo
	modelsExpiry     time.Time
	modelsRetryUntil time.Time
	modelsErr        error
	modelsFetch      chan struct{}

	assignmentsMu sync.Mutex
	assignments   map[string]resolvedAssignment
}

// resolvedAssignment 是 AssignModel 对 (router uid, cascade id) 的解析结果。
type resolvedAssignment struct {
	modelUID string
	jwt      string
}

// NewClient 构建上游客户端；baseURL/token 为空时报错（fail fast）。
func NewClient(cfg ClientConfig) (*Client, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = DefaultBaseURL
	}
	if _, err := url.Parse(base); err != nil {
		return nil, fmt.Errorf("devin base_url is invalid: %w", err)
	}
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		return nil, errors.New("devin token is required")
	}
	cfg.BaseURL = strings.TrimRight(base, "/")
	cfg.Token = token

	transport, err := buildTransport(cfg.ProxyURL)
	if err != nil {
		return nil, err
	}
	rt := &basicAuthTransport{base: transport, token: token}

	// SSE 长连接不设 Client.Timeout，首包等待由 Transport.ResponseHeaderTimeout 兜底。
	streamHTTP := &http.Client{Transport: rt}
	apiHTTP := &http.Client{Transport: rt, Timeout: apiCallTimeout}
	opts := connect.WithSendGzip()

	return &Client{
		cfg:         cfg,
		transport:   transport,
		stream:      devinprotoconnect.NewApiServerServiceClient(streamHTTP, cfg.BaseURL, opts),
		api:         devinprotoconnect.NewApiServerServiceClient(apiHTTP, cfg.BaseURL, opts),
		assignments: make(map[string]resolvedAssignment),
	}, nil
}

// buildTransport 构建直连/代理 transport。强制 HTTP/1.1：上游连接复用下
// HTTP/2 单连接多流会成为并发瓶颈（devin2api 实测结论）。
func buildTransport(proxyURL string) (*http.Transport, error) {
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   16,
		MaxConnsPerHost:       0,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second,
		ForceAttemptHTTP2:     false,
		TLSNextProto:          map[string]func(string, *tls.Conn) http.RoundTripper{},
	}
	if proxyURL = strings.TrimSpace(proxyURL); proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("devin proxy url is invalid: %w", err)
		}
		if err := proxyutil.ConfigureTransportProxy(transport, parsed); err != nil {
			return nil, fmt.Errorf("devin proxy: %w", err)
		}
	}
	return transport, nil
}

// basicAuthTransport 为出站请求注入 "Basic <token>-<token>" 并清空
// User-Agent（真实 CLI 抓包不发送 UA，connect-go 默认 UA 是指纹破绽）。
type basicAuthTransport struct {
	base  http.RoundTripper
	token string
}

func (t *basicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	if clone.Header.Get("Authorization") == "" {
		clone.Header.Set("Authorization", "Basic "+t.token+"-"+t.token)
	}
	clone.Header.Set("User-Agent", "")
	return t.base.RoundTrip(clone)
}

// Close 释放空闲连接。
func (c *Client) Close() {
	if c.transport != nil {
		c.transport.CloseIdleConnections()
	}
}

// metadata 构建随请求携带的客户端身份元数据。
func (c *Client) metadata(fingerprintBytes int) *devinproto.ExaCodeiumCommonPb_Metadata {
	metadata := &devinproto.ExaCodeiumCommonPb_Metadata{
		ApiKey:           proto.String(c.cfg.Token),
		ExtensionName:    proto.String(defaultClientName),
		ExtensionVersion: proto.String(defaultClientVersion),
		IdeName:          proto.String(defaultClientName),
		IdeVersion:       proto.String(defaultClientVersion),
		Locale:           proto.String("en"),
		Os:               proto.String(defaultClientOS),
	}
	if fingerprintBytes > 0 {
		if fingerprint, err := randomHex(fingerprintBytes); err == nil {
			metadata.F = proto.String(fingerprint)
		}
	}
	return metadata
}

func randomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// Chat 发起一次 GetChatMessage 流式调用。建流阶段的传输级错误做有限重试；
// 返回的 *ChatStream 负责逐帧解码（见 decode.go）。
func (c *Client) Chat(ctx context.Context, req *devinproto.GetChatMessageRequest) (*ChatStream, error) {
	stream, err := c.getChatMessageWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	return &ChatStream{stream: stream, decoder: newResponseDecoder()}, nil
}

// getChatMessageWithRetry 对建流失败（尚未收到任何上游帧）的传输错误重试。
// 语义性 connect 错误（invalid_argument/unauthenticated/resource_exhausted 等）
// 立即返回，重试只会复现同样的失败。
func (c *Client) getChatMessageWithRetry(ctx context.Context, req *devinproto.GetChatMessageRequest) (*connect.ServerStreamForClient[devinproto.GetChatMessageResponse], error) {
	var lastErr error
	for attempt := 0; attempt < maxConnectAttempts; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(float64(attempt)*400*(0.75+0.5*mrand.Float64())) * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
		stream, err := c.stream.GetChatMessage(ctx, connect.NewRequest(req))
		if err == nil {
			return stream, nil
		}
		lastErr = err
		if !IsTransientConnectError(err) {
			break
		}
	}
	return nil, lastErr
}
