package service

import (
	"net/http"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

	"golang.org/x/net/http/httpguts"
)

const (
	CredentialKeyHeaderOverrideEnabled = "header_override_enabled"
	CredentialKeyHeaderOverrides       = "header_overrides"

	credKeyHeaderOverrideEnabled = CredentialKeyHeaderOverrideEnabled
	credKeyHeaderOverrides       = CredentialKeyHeaderOverrides

	maxHeaderOverrideEntries     = 64
	maxHeaderOverrideNameLength  = 200
	maxHeaderOverrideValueLength = 8192
)

var headerOverrideBlockedNames = map[string]struct{}{
	"host":                     {},
	"content-length":           {},
	"content-type":             {},
	"transfer-encoding":        {},
	"connection":               {},
	"keep-alive":               {},
	"proxy-authenticate":       {},
	"proxy-authorization":      {},
	"proxy-connection":         {},
	"te":                       {},
	"trailer":                  {},
	"upgrade":                  {},
	"authorization":            {},
	"x-api-key":                {},
	"x-goog-api-key":           {},
	"cookie":                   {},
	"accept-encoding":          {},
	"sec-websocket-key":        {},
	"sec-websocket-version":    {},
	"sec-websocket-extensions": {},
	"sec-websocket-protocol":   {},
	"sec-websocket-accept":     {},
	"session_id":               {},
	"conversation_id":          {},
	"x-codex-turn-state":       {},
	"x-codex-turn-metadata":    {},
	"chatgpt-account-id":       {},
	"x-claude-code-session-id": {},
	"x-client-request-id":      {},
	"x-grok-conv-id":           {},
}

func isHeaderOverrideBlockedName(lowerName string) bool {
	_, blocked := headerOverrideBlockedNames[lowerName]
	return blocked
}

// IsHeaderOverrideEligible reports whether the account type supports header overrides.
func (a *Account) IsHeaderOverrideEligible() bool {
	if a == nil {
		return false
	}
	switch a.Platform {
	case PlatformAnthropic, PlatformOpenAI:
		return a.Type == AccountTypeAPIKey
	case PlatformGrok:
		return a.Type == AccountTypeAPIKey || a.Type == AccountTypeOAuth
	default:
		return false
	}
}

// IsHeaderOverrideEnabled reports whether header overrides are explicitly enabled.
func (a *Account) IsHeaderOverrideEnabled() bool {
	if !a.IsHeaderOverrideEligible() || a.Credentials == nil {
		return false
	}
	enabled, ok := a.Credentials[credKeyHeaderOverrideEnabled].(bool)
	return ok && enabled
}

// GetHeaderOverrides returns effective outbound header overrides keyed by lower-case name.
func (a *Account) GetHeaderOverrides() map[string]string {
	if !a.IsHeaderOverrideEnabled() {
		return nil
	}
	rawMapping, rawIsAnyMap := a.Credentials[credKeyHeaderOverrides].(map[string]any)
	if !rawIsAnyMap {
		return resolveHeaderOverrides(stringMappingFromRaw(a.Credentials[credKeyHeaderOverrides]))
	}

	credentialsPtr := mapPtr(a.Credentials)
	rawPtr := mapPtr(rawMapping)
	rawLen := len(rawMapping)
	rawSig := uint64(0)
	rawSigReady := false

	if a.headerOverrideCacheReady &&
		a.headerOverrideCacheCredentialsPtr == credentialsPtr &&
		a.headerOverrideCacheRawPtr == rawPtr &&
		a.headerOverrideCacheRawLen == rawLen {
		rawSig = modelMappingSignature(rawMapping)
		rawSigReady = true
		if a.headerOverrideCacheRawSig == rawSig {
			return a.headerOverrideCache
		}
	}

	overrides := resolveHeaderOverrides(stringMappingFromRaw(rawMapping))
	if !rawSigReady {
		rawSig = modelMappingSignature(rawMapping)
	}

	a.headerOverrideCache = overrides
	a.headerOverrideCacheReady = true
	a.headerOverrideCacheCredentialsPtr = credentialsPtr
	a.headerOverrideCacheRawPtr = rawPtr
	a.headerOverrideCacheRawLen = rawLen
	a.headerOverrideCacheRawSig = rawSig
	return overrides
}

func resolveHeaderOverrides(raw map[string]string) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	result := make(map[string]string, len(raw))
	for name, value := range raw {
		lowerName, value, err := normalizeHeaderOverrideEntry(name, value)
		if err != nil || lowerName == "" || value == "" {
			continue
		}
		result[lowerName] = value
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func (a *Account) HeaderOverrideValue(lowerName string) (string, bool) {
	value, ok := a.GetHeaderOverrides()[lowerName]
	return value, ok
}

// ApplyHeaderOverrides applies configured overrides to outbound request headers.
func (a *Account) ApplyHeaderOverrides(h http.Header) {
	if h == nil {
		return
	}
	overrides := a.GetHeaderOverrides()
	if len(overrides) == 0 {
		return
	}
	for name, value := range overrides {
		for existing := range h {
			if strings.EqualFold(existing, name) {
				delete(h, existing)
			}
		}
		wireName := resolveWireCasing(name)
		if wireName == name {
			wireName = http.CanonicalHeaderKey(name)
		}
		h[wireName] = []string{value}
	}
}

// NormalizeHeaderOverrideCredentials validates and normalizes header override credentials in-place.
func NormalizeHeaderOverrideCredentials(credentials map[string]any) error {
	if credentials == nil {
		return nil
	}
	if raw, ok := credentials[credKeyHeaderOverrideEnabled]; ok && raw != nil {
		if _, isBool := raw.(bool); !isBool {
			return infraerrors.New(http.StatusBadRequest, "INVALID_HEADER_OVERRIDE", "header_override_enabled must be a boolean")
		}
	}
	raw, ok := credentials[credKeyHeaderOverrides]
	if !ok || raw == nil {
		return nil
	}

	var entries map[string]any
	switch m := raw.(type) {
	case map[string]any:
		entries = m
	case map[string]string:
		entries = make(map[string]any, len(m))
		for k, v := range m {
			entries[k] = v
		}
	default:
		return infraerrors.New(http.StatusBadRequest, "INVALID_HEADER_OVERRIDE", "header_overrides must be an object of header name to string value")
	}

	if len(entries) > maxHeaderOverrideEntries {
		return infraerrors.Newf(http.StatusBadRequest, "INVALID_HEADER_OVERRIDE", "header_overrides supports at most %d entries", maxHeaderOverrideEntries)
	}

	normalized := make(map[string]any, len(entries))
	for name, rawValue := range entries {
		value, isString := rawValue.(string)
		if !isString {
			return infraerrors.Newf(http.StatusBadRequest, "INVALID_HEADER_OVERRIDE", "header %q value must be a string", name)
		}
		lowerName, value, err := normalizeHeaderOverrideEntry(name, value)
		if err != nil {
			return err
		}
		if lowerName == "" {
			continue
		}
		if _, dup := normalized[lowerName]; dup {
			return infraerrors.Newf(http.StatusBadRequest, "INVALID_HEADER_OVERRIDE", "duplicate header name %q (matching is case-insensitive)", lowerName)
		}
		normalized[lowerName] = value
	}
	credentials[credKeyHeaderOverrides] = normalized
	return nil
}

func normalizeHeaderOverrideEntry(name, value string) (string, string, error) {
	lowerName := strings.ToLower(strings.TrimSpace(name))
	value = strings.TrimSpace(value)
	if lowerName == "" {
		if value == "" {
			return "", "", nil
		}
		return "", "", infraerrors.New(http.StatusBadRequest, "INVALID_HEADER_OVERRIDE", "header name must not be empty")
	}
	if len(lowerName) > maxHeaderOverrideNameLength {
		return "", "", infraerrors.Newf(http.StatusBadRequest, "INVALID_HEADER_OVERRIDE", "header name %q exceeds %d characters", lowerName, maxHeaderOverrideNameLength)
	}
	if !httpguts.ValidHeaderFieldName(lowerName) {
		return "", "", infraerrors.Newf(http.StatusBadRequest, "INVALID_HEADER_OVERRIDE", "invalid header name %q", lowerName)
	}
	if isHeaderOverrideBlockedName(lowerName) {
		return "", "", infraerrors.Newf(http.StatusBadRequest, "INVALID_HEADER_OVERRIDE", "header %q is not allowed to be overridden", lowerName)
	}
	if len(value) > maxHeaderOverrideValueLength {
		return "", "", infraerrors.Newf(http.StatusBadRequest, "INVALID_HEADER_OVERRIDE", "header %q value exceeds %d characters", lowerName, maxHeaderOverrideValueLength)
	}
	if !httpguts.ValidHeaderFieldValue(value) {
		return "", "", infraerrors.Newf(http.StatusBadRequest, "INVALID_HEADER_OVERRIDE", "header %q has an invalid value", lowerName)
	}
	return lowerName, value, nil
}
