package service

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

// API聚合渠道凭证字段约定：
//   credentials.api_key        上游 API Key（必填）
//   credentials.base_url       上游基础地址（必填，强制 https + 公网）
//   credentials.api_base_urls  可选的按协议覆盖地址 {"chat_completions","responses","anthropic"}
//   credentials.api_protocol   可选协议偏好，缺省 adaptive
//   credentials.model_mapping  号主模型白名单（沿用个人账号规则，必填且 ⊆ 平台定价目录）

// apiAggregationAllowedProtocols 是聚合渠道支持的上游协议集合。
var apiAggregationAllowedProtocols = map[string]struct{}{
	APIProtocolAdaptive:        {},
	APIProtocolChatCompletions: {},
	APIProtocolResponses:       {},
	APIProtocolAnthropic:       {},
}

// validateAPIAggregationUpstreamURLs 校验聚合渠道上游地址。
// 与网关运行时的 security.url_allowlist 不同：这里无论全局配置如何都强制
// https + 公网目标（字面量私网地址在格式校验阶段拒绝，域名解析出的私网
// 地址在 DNS 解析阶段拒绝），因为上游地址由房主自填，不允许指向内网。
func validateAPIAggregationUpstreamURLs(credentials map[string]any) error {
	baseURL, _ := credentials["base_url"].(string)
	normalizedBase, err := validateAPIAggregationUpstreamURL(baseURL)
	if err != nil {
		return err
	}
	credentials["base_url"] = normalizedBase

	if raw, ok := credentials["api_base_urls"]; ok && raw != nil {
		urls, ok := raw.(map[string]any)
		if !ok {
			return ErrOwnedAccountCredentialsInvalid.WithMetadata(map[string]string{"field": "api_base_urls"})
		}
		normalized := make(map[string]any, len(urls))
		for key, value := range urls {
			protocol := strings.ToLower(strings.TrimSpace(key))
			if _, allowed := apiAggregationAllowedProtocols[protocol]; !allowed || protocol == APIProtocolAdaptive {
				return ErrOwnedAccountCredentialsInvalid.WithMetadata(map[string]string{"field": "api_base_urls." + key})
			}
			text, _ := value.(string)
			normalizedURL, err := validateAPIAggregationUpstreamURL(text)
			if err != nil {
				return ErrOwnedAccountCredentialsInvalid.WithMetadata(map[string]string{"field": "api_base_urls." + protocol})
			}
			normalized[protocol] = normalizedURL
		}
		credentials["api_base_urls"] = normalized
	}

	if raw, ok := credentials["api_protocol"]; ok && raw != nil {
		protocol, _ := raw.(string)
		protocol = strings.ToLower(strings.TrimSpace(protocol))
		if _, allowed := apiAggregationAllowedProtocols[protocol]; !allowed {
			return ErrOwnedAccountCredentialsInvalid.WithMetadata(map[string]string{"field": "api_protocol"})
		}
		credentials["api_protocol"] = protocol
	}
	return nil
}

// validateAPIAggregationUpstreamURL 校验单个上游地址：https scheme、
// 非私网字面量、DNS 必须解析到公网 IP（防域名指向内网）。
func validateAPIAggregationUpstreamURL(raw string) (string, error) {
	normalized, err := urlvalidator.ValidateHTTPSURL(strings.TrimSpace(raw), urlvalidator.ValidationOptions{
		AllowPrivate: false,
	})
	if err != nil {
		return "", ErrOwnedAccountCredentialsInvalid.WithMetadata(map[string]string{
			"field":  "base_url",
			"reason": fmt.Sprintf("upstream url must be a public https address: %v", err),
		})
	}
	parsed, err := url.Parse(normalized)
	if err != nil || parsed.Hostname() == "" {
		return "", ErrOwnedAccountCredentialsInvalid.WithMetadata(map[string]string{
			"field":  "base_url",
			"reason": "invalid upstream host",
		})
	}
	if err := resolveAPIAggregationHost(parsed.Hostname()); err != nil {
		return "", ErrOwnedAccountCredentialsInvalid.WithMetadata(map[string]string{
			"field":  "base_url",
			"reason": fmt.Sprintf("upstream host must resolve to a public ip: %v", err),
		})
	}
	return normalized, nil
}

// apiAggregationBlockedHostSuffixes 是聚合渠道上游禁止使用的内部域名后缀。
var apiAggregationBlockedHostSuffixes = []string{
	".localhost",
	".local",
	".internal",
	".lan",
	".home",
	".corp",
	".home.arpa",
}

// apiAggregationBlockedPrefixes 是通用 urlvalidator 未覆盖的非公网 IPv4 段：
// CGNAT(100.64.0.0/10)、IETF 协议分配(192.0.0.0/24)、文档示例段、基准测试段、
// 保留段(240.0.0.0/4)与受限广播地址。
var apiAggregationBlockedPrefixes = func() []netip.Prefix {
	cidrs := []string{
		"100.64.0.0/10",
		"192.0.0.0/24",
		"192.0.2.0/24",
		"198.18.0.0/15",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"240.0.0.0/4",
		"255.255.255.255/32",
		"2001:db8::/32",
	}
	prefixes := make([]netip.Prefix, 0, len(cidrs))
	for _, cidr := range cidrs {
		if p, err := netip.ParsePrefix(cidr); err == nil {
			prefixes = append(prefixes, p)
		}
	}
	return prefixes
}()

func isAPIAggregationPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	for _, prefix := range apiAggregationBlockedPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

// resolveAPIAggregationHost 校验 host 的 DNS 解析结果必须全部为公网 IP，
// 防止域名指向内网（DNS Rebinding 侧的静态防线；请求时仍由网关 URL
// 校验器按全局策略复核）。
func resolveAPIAggregationHost(host string) error {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" {
		return fmt.Errorf("empty host")
	}
	if host == "localhost" {
		return fmt.Errorf("host is not allowed: %s", host)
	}
	for _, suffix := range apiAggregationBlockedHostSuffixes {
		if strings.HasSuffix(host, suffix) {
			return fmt.Errorf("host is not allowed: %s", host)
		}
	}
	if ip := net.ParseIP(host); ip != nil {
		if !isAPIAggregationPublicIP(ip) {
			return fmt.Errorf("ip %s is not a public address", ip.String())
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("dns resolution failed: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("dns resolution returned no addresses")
	}
	for _, ip := range ips {
		if !isAPIAggregationPublicIP(ip) {
			return fmt.Errorf("resolved ip %s is not a public address", ip.String())
		}
	}
	return nil
}
