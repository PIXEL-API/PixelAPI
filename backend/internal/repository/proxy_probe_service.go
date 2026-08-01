package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func NewProxyExitInfoProber(cfg *config.Config) service.ProxyExitInfoProber {
	insecure := false
	allowPrivate := false
	validateResolvedIP := true
	maxResponseBytes := defaultProxyProbeResponseMaxBytes
	if cfg != nil {
		insecure = cfg.Security.ProxyProbe.InsecureSkipVerify
		allowPrivate = cfg.Security.URLAllowlist.AllowPrivateHosts
		validateResolvedIP = cfg.Security.URLAllowlist.Enabled
		if cfg.Gateway.ProxyProbeResponseReadMaxBytes > 0 {
			maxResponseBytes = cfg.Gateway.ProxyProbeResponseReadMaxBytes
		}
	}
	if insecure {
		log.Printf("[ProxyProbe] Warning: insecure_skip_verify is not allowed and will cause probe failure.")
	}
	return &proxyProbeService{
		insecureSkipVerify: insecure,
		allowPrivateHosts:  allowPrivate,
		validateResolvedIP: validateResolvedIP,
		maxResponseBytes:   maxResponseBytes,
	}
}

const (
	defaultProxyProbeTimeout          = 10 * time.Second
	defaultProxyProbeResponseMaxBytes = int64(1024 * 1024)
)

// probeURLs 按优先级排列的探测 URL 列表
// 某些 AI API 专用代理只允许访问特定域名，因此需要多个备选
var probeURLs = []struct {
	url    string
	name   string // 聚合错误信息里的短名，避免把完整 URL 拼进提示
	parser string // "ip-api" or "ipify"
}{
	{"http://ip-api.com/json/?lang=zh-CN", "ip-api", "ip-api"},
	{"http://api64.ipify.org?format=json", "ipify", "ipify"},
}

// maxProbeReasonLen 单个探测点失败原因在聚合信息里的最大长度（按 rune 计）
const maxProbeReasonLen = 60

type proxyProbeService struct {
	insecureSkipVerify bool
	allowPrivateHosts  bool
	validateResolvedIP bool
	maxResponseBytes   int64
}

func (s *proxyProbeService) ProbeProxy(ctx context.Context, proxyURL string) (*service.ProxyExitInfo, int64, error) {
	client, err := httpclient.GetClient(httpclient.Options{
		ProxyURL:           proxyURL,
		Timeout:            defaultProxyProbeTimeout,
		InsecureSkipVerify: s.insecureSkipVerify,
		ValidateResolvedIP: s.validateResolvedIP,
		AllowPrivateHosts:  s.allowPrivateHosts,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create proxy client: %w", err)
	}

	reasons := make([]string, 0, len(probeURLs))
	errs := make([]error, 0, len(probeURLs))
	for _, probe := range probeURLs {
		exitInfo, latencyMs, err := s.probeWithURL(ctx, client, probe.url, probe.parser)
		if err == nil {
			return exitInfo, latencyMs, nil
		}
		reasons = append(reasons, probe.name+": "+summarizeProbeError(err))
		errs = append(errs, fmt.Errorf("%s: %w", probe.name, err))
	}

	return nil, 0, &probeFailureError{
		summary: fmt.Sprintf("all probe URLs failed (%s)", strings.Join(reasons, "; ")),
		errs:    errs,
	}
}

// probeFailureError 对外只暴露一条精简的聚合提示，完整的逐个探测错误保留在
// Unwrap 链上供 errors.Is/As 使用，避免把每个探测点的原始报文都堆到前端弹窗里。
type probeFailureError struct {
	summary string
	errs    []error
}

func (e *probeFailureError) Error() string { return e.summary }

func (e *probeFailureError) Unwrap() []error { return e.errs }

// summarizeProbeError 把单个探测点的失败原因压成一句短语：
// 常见网络故障归一成固定词，其余去掉 net/http 附带的完整 URL 后截断。
func summarizeProbeError(err error) string {
	if err == nil {
		return "unknown"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return "timeout"
	}

	msg := err.Error()
	// url.Error 会把完整探测地址拼进消息（如 `Get "http://ip-api.com/...": xxx`），剥掉它
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		msg = strings.Replace(msg, fmt.Sprintf("%s %q: ", urlErr.Op, urlErr.URL), "", 1)
	}
	msg = strings.Join(strings.Fields(msg), " ")
	if msg == "" {
		return "unknown"
	}
	if runes := []rune(msg); len(runes) > maxProbeReasonLen {
		msg = strings.TrimSpace(string(runes[:maxProbeReasonLen])) + "…"
	}
	return msg
}

func (s *proxyProbeService) probeWithURL(ctx context.Context, client *http.Client, url string, parser string) (*service.ProxyExitInfo, int64, error) {
	startTime := time.Now()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("proxy connection failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	latencyMs := time.Since(startTime).Milliseconds()

	if resp.StatusCode != http.StatusOK {
		return nil, latencyMs, fmt.Errorf("request failed with status: %d", resp.StatusCode)
	}

	maxResponseBytes := s.maxResponseBytes
	if maxResponseBytes <= 0 {
		maxResponseBytes = defaultProxyProbeResponseMaxBytes
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, latencyMs, fmt.Errorf("failed to read response: %w", err)
	}
	if int64(len(body)) > maxResponseBytes {
		return nil, latencyMs, fmt.Errorf("proxy probe response exceeds limit: %d", maxResponseBytes)
	}

	switch parser {
	case "ip-api":
		return s.parseIPAPI(body, latencyMs)
	case "ipify":
		return s.parseIPify(body, latencyMs)
	default:
		return nil, latencyMs, fmt.Errorf("unknown parser: %s", parser)
	}
}

func (s *proxyProbeService) parseIPAPI(body []byte, latencyMs int64) (*service.ProxyExitInfo, int64, error) {
	var ipInfo struct {
		Status      string `json:"status"`
		Message     string `json:"message"`
		Query       string `json:"query"`
		City        string `json:"city"`
		Region      string `json:"region"`
		RegionName  string `json:"regionName"`
		Country     string `json:"country"`
		CountryCode string `json:"countryCode"`
	}

	if err := json.Unmarshal(body, &ipInfo); err != nil {
		preview := string(body)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		return nil, latencyMs, fmt.Errorf("failed to parse response: %w (body: %s)", err, preview)
	}
	if strings.ToLower(ipInfo.Status) != "success" {
		if ipInfo.Message == "" {
			ipInfo.Message = "ip-api request failed"
		}
		return nil, latencyMs, fmt.Errorf("ip-api request failed: %s", ipInfo.Message)
	}

	region := ipInfo.RegionName
	if region == "" {
		region = ipInfo.Region
	}
	return &service.ProxyExitInfo{
		IP:          ipInfo.Query,
		City:        ipInfo.City,
		Region:      region,
		Country:     ipInfo.Country,
		CountryCode: ipInfo.CountryCode,
	}, latencyMs, nil
}

func (s *proxyProbeService) parseIPify(body []byte, latencyMs int64) (*service.ProxyExitInfo, int64, error) {
	var result struct {
		IP string `json:"ip"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, latencyMs, fmt.Errorf("failed to parse ipify response: %w", err)
	}
	if result.IP == "" {
		return nil, latencyMs, fmt.Errorf("ipify: no IP found in response")
	}
	return &service.ProxyExitInfo{
		IP: result.IP,
	}, latencyMs, nil
}
