package xai

import (
	"fmt"
	"net/url"
	"strings"
)

// BaseURLValidator applies the caller's outbound trust policy before endpoint
// paths are appended.
type BaseURLValidator func(string) (string, error)

// IsParseableBaseURL 用于读取存量凭据；无法解析出 host 的脏值应回落默认端点。
// 安全准入仍由调用方的 BaseURLValidator 决定。
func IsParseableBaseURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && parsed.Host != ""
}

func validatedBaseURLWithValidator(override string, validator BaseURLValidator) (string, error) {
	if validator == nil {
		return ValidatedBaseURL(override)
	}
	validated, err := validator(EffectiveBaseURL(override))
	if err != nil {
		return "", err
	}
	return normalizeKnownBaseURLPath(validated)
}

func buildURLWithValidator(baseURL, suffix string, validator BaseURLValidator) (string, error) {
	validated, err := validatedBaseURLWithValidator(baseURL, validator)
	if err != nil {
		return "", fmt.Errorf("invalid base url: %w", err)
	}
	return validated + suffix, nil
}

func BuildResponsesURLWithValidator(baseURL string, validator BaseURLValidator) (string, error) {
	return buildURLWithValidator(baseURL, "/responses", validator)
}

func BuildChatCompletionsURLWithValidator(baseURL string, validator BaseURLValidator) (string, error) {
	return buildURLWithValidator(baseURL, "/chat/completions", validator)
}

func BuildImagesGenerationsURLWithValidator(baseURL string, validator BaseURLValidator) (string, error) {
	return buildURLWithValidator(baseURL, "/images/generations", validator)
}

func BuildImagesEditsURLWithValidator(baseURL string, validator BaseURLValidator) (string, error) {
	return buildURLWithValidator(baseURL, "/images/edits", validator)
}

func BuildVideosGenerationsURLWithValidator(baseURL string, validator BaseURLValidator) (string, error) {
	return buildURLWithValidator(baseURL, "/videos/generations", validator)
}

func BuildVideosEditsURLWithValidator(baseURL string, validator BaseURLValidator) (string, error) {
	return buildURLWithValidator(baseURL, "/videos/edits", validator)
}

func BuildVideosExtensionsURLWithValidator(baseURL string, validator BaseURLValidator) (string, error) {
	return buildURLWithValidator(baseURL, "/videos/extensions", validator)
}

func BuildVideoURLWithValidator(baseURL, requestID string, validator BaseURLValidator) (string, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return "", fmt.Errorf("request id is required")
	}
	return buildURLWithValidator(baseURL, "/videos/"+url.PathEscape(requestID), validator)
}
