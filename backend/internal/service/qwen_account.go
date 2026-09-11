package service

import (
	"net/url"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// Qwen billing modes use separate credentials and endpoints. Validate explicit
// settings before persisting them so switching modes cannot silently incur PAYG charges.
func validateQwenAccountConfiguration(platform, accountType string, credentials map[string]any) error {
	if platform != PlatformQwen {
		return nil
	}
	invalid := func(field string) error {
		return infraerrors.BadRequest("QWEN_ACCOUNT_CONFIGURATION_INVALID", "Qwen account mode, protocol and endpoint must match").WithMetadata(map[string]string{"field": field})
	}
	if accountType != AccountTypeAPIKey || !hasNonEmptyStringField(credentials, "api_key") {
		return invalid("api_key")
	}
	mode, ok := credentials["account_mode"].(string)
	if !ok || (mode != AccountModePayG && mode != AccountModeCoding) {
		return invalid("account_mode")
	}
	protocol := APIProtocolChatCompletions
	if raw, exists := credentials["api_protocol"]; exists {
		value, ok := raw.(string)
		if !ok || (value != APIProtocolChatCompletions && value != APIProtocolAnthropic && value != APIProtocolAdaptive) {
			return invalid("api_protocol")
		}
		protocol = value
	}
	validateURL := func(raw any, field, endpointProtocol string) error {
		value, ok := raw.(string)
		if !ok {
			return invalid(field)
		}
		if strings.TrimSpace(value) == "" {
			return nil // Forwarding resolves the documented default for this mode and protocol.
		}
		u, err := url.Parse(strings.TrimSpace(value))
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return invalid(field)
		}
		host := strings.ToLower(u.Hostname())
		official := host == "dashscope.aliyuncs.com" || host == "dashscope-intl.aliyuncs.com" || host == "dashscope-us.aliyuncs.com" || strings.HasSuffix(host, ".dashscope.aliyuncs.com") || strings.HasSuffix(host, ".maas.aliyuncs.com")
		if !official {
			return nil // Custom upstreams still pass the gateway URL security policy.
		}
		coding := strings.HasPrefix(host, "coding.") || strings.HasPrefix(host, "coding-intl.")
		if strings.HasPrefix(host, "token-plan.") || coding != (mode == AccountModeCoding) {
			return invalid(field)
		}
		path := strings.TrimRight(u.Path, "/")
		if endpointProtocol == APIProtocolAnthropic {
			if path != "/apps/anthropic" {
				return invalid(field)
			}
		} else if (coding && path != "/v1") || (!coding && path != "/compatible-mode/v1") {
			return invalid(field)
		}
		return nil
	}
	if protocol != APIProtocolAdaptive {
		if raw, exists := credentials["base_url"]; exists {
			return validateURL(raw, "base_url", protocol)
		}
		return nil
	}
	if raw, exists := credentials["base_url"]; exists {
		if err := validateURL(raw, "base_url", APIProtocolChatCompletions); err != nil {
			return err
		}
	}
	if raw, exists := credentials["api_base_urls"]; exists {
		urls, ok := raw.(map[string]any)
		if !ok {
			return invalid("api_base_urls")
		}
		for key, value := range urls {
			if key != APIProtocolChatCompletions && key != APIProtocolAnthropic {
				if text, ok := value.(string); ok && text == "" {
					continue
				}
				return invalid("api_base_urls." + key)
			}
			if err := validateURL(value, "api_base_urls."+key, key); err != nil {
				return err
			}
		}
	}
	return nil
}
