package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var ErrOpsDisabled = infraerrors.NotFound("OPS_DISABLED", "Ops monitoring is disabled")

const (
	opsMaxStoredRequestBodyBytes = 10 * 1024
	opsMaxStoredErrorBodyBytes   = 20 * 1024
)

// OpsService provides ingestion and query APIs for the Ops monitoring module.
type OpsService struct {
	opsRepo     OpsRepository
	settingRepo SettingRepository
	cfg         *config.Config

	accountRepo AccountRepository

	// getAccountAvailability is a unit-test hook for overriding account availability lookup.
	getAccountAvailability func(ctx context.Context, platformFilter string, groupIDFilter *int64) (*OpsAccountAvailability, error)

	concurrencyService        *ConcurrencyService
	gatewayService            *GatewayService
	openAIGatewayService      *OpenAIGatewayService
	geminiCompatService       *GeminiMessagesCompatService
	antigravityGatewayService *AntigravityGatewayService
}

func NewOpsService(
	opsRepo OpsRepository,
	settingRepo SettingRepository,
	cfg *config.Config,
	accountRepo AccountRepository,
	concurrencyService *ConcurrencyService,
	gatewayService *GatewayService,
	openAIGatewayService *OpenAIGatewayService,
	geminiCompatService *GeminiMessagesCompatService,
	antigravityGatewayService *AntigravityGatewayService,
) *OpsService {
	return &OpsService{
		opsRepo:     opsRepo,
		settingRepo: settingRepo,
		cfg:         cfg,

		accountRepo: accountRepo,

		concurrencyService:        concurrencyService,
		gatewayService:            gatewayService,
		openAIGatewayService:      openAIGatewayService,
		geminiCompatService:       geminiCompatService,
		antigravityGatewayService: antigravityGatewayService,
	}
}

func (s *OpsService) RequireMonitoringEnabled(ctx context.Context) error {
	if s.IsMonitoringEnabled(ctx) {
		return nil
	}
	return ErrOpsDisabled
}

func (s *OpsService) IsMonitoringEnabled(ctx context.Context) bool {
	// Hard switch: disable ops entirely.
	if s.cfg != nil && !s.cfg.Ops.Enabled {
		return false
	}
	if s.settingRepo == nil {
		return true
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeyOpsMonitoringEnabled)
	if err != nil {
		// Default enabled when key is missing, and fail-open on transient errors
		// (ops should never block gateway traffic).
		if errors.Is(err, ErrSettingNotFound) {
			return true
		}
		return true
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "false", "0", "off", "disabled":
		return false
	default:
		return true
	}
}

func (s *OpsService) RecordError(ctx context.Context, entry *OpsInsertErrorLogInput, rawRequestBody []byte) error {
	if entry == nil {
		return nil
	}
	if !s.IsMonitoringEnabled(ctx) {
		return nil
	}
	if s.opsRepo == nil {
		return nil
	}

	// Ensure timestamps are always populated.
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}

	// Ensure required fields exist (DB has NOT NULL constraints).
	entry.ErrorPhase = strings.TrimSpace(entry.ErrorPhase)
	entry.ErrorType = strings.TrimSpace(entry.ErrorType)
	if entry.ErrorPhase == "" {
		entry.ErrorPhase = "internal"
	}
	if entry.ErrorType == "" {
		entry.ErrorType = "api_error"
	}

	// Sanitize + trim request body (errors only).
	if len(rawRequestBody) > 0 {
		sanitized, truncated, bytesLen := sanitizeAndTrimRequestBody(rawRequestBody, opsMaxStoredRequestBodyBytes)
		if sanitized != "" {
			entry.RequestBodyJSON = &sanitized
		}
		entry.RequestBodyTruncated = truncated
		entry.RequestBodyBytes = &bytesLen
	}

	// Sanitize + truncate error_body to avoid storing sensitive data.
	if strings.TrimSpace(entry.ErrorBody) != "" {
		sanitized, _ := sanitizeErrorBodyForStorage(entry.ErrorBody, opsMaxStoredErrorBodyBytes)
		entry.ErrorBody = sanitized
	}

	// Sanitize upstream error context if provided by gateway services.
	if entry.UpstreamStatusCode != nil && *entry.UpstreamStatusCode <= 0 {
		entry.UpstreamStatusCode = nil
	}
	if entry.UpstreamErrorMessage != nil {
		msg := strings.TrimSpace(*entry.UpstreamErrorMessage)
		msg = sanitizeUpstreamErrorMessage(msg)
		msg = truncateString(msg, 2048)
		if strings.TrimSpace(msg) == "" {
			entry.UpstreamErrorMessage = nil
		} else {
			entry.UpstreamErrorMessage = &msg
		}
	}
	if entry.UpstreamErrorDetail != nil {
		detail := strings.TrimSpace(*entry.UpstreamErrorDetail)
		if detail == "" {
			entry.UpstreamErrorDetail = nil
		} else {
			sanitized, _ := sanitizeErrorBodyForStorage(detail, opsMaxStoredErrorBodyBytes)
			if strings.TrimSpace(sanitized) == "" {
				entry.UpstreamErrorDetail = nil
			} else {
				entry.UpstreamErrorDetail = &sanitized
			}
		}
	}

	// Sanitize + serialize upstream error events list.
	if len(entry.UpstreamErrors) > 0 {
		const maxEvents = 32
		events := entry.UpstreamErrors
		if len(events) > maxEvents {
			events = events[len(events)-maxEvents:]
		}

		sanitized := make([]*OpsUpstreamErrorEvent, 0, len(events))
		for _, ev := range events {
			if ev == nil {
				continue
			}
			out := *ev

			out.Platform = strings.TrimSpace(out.Platform)
			out.UpstreamRequestID = truncateString(strings.TrimSpace(out.UpstreamRequestID), 128)
			out.Kind = truncateString(strings.TrimSpace(out.Kind), 64)

			if out.AccountID < 0 {
				out.AccountID = 0
			}
			if out.UpstreamStatusCode < 0 {
				out.UpstreamStatusCode = 0
			}
			if out.AtUnixMs < 0 {
				out.AtUnixMs = 0
			}

			msg := sanitizeUpstreamErrorMessage(strings.TrimSpace(out.Message))
			msg = truncateString(msg, 2048)
			out.Message = msg

			detail := strings.TrimSpace(out.Detail)
			if detail != "" {
				// Keep upstream detail small; request bodies are not stored here, only upstream error payloads.
				sanitizedDetail, _ := sanitizeErrorBodyForStorage(detail, opsMaxStoredErrorBodyBytes)
				out.Detail = sanitizedDetail
			} else {
				out.Detail = ""
			}

			out.UpstreamRequestBody = strings.TrimSpace(out.UpstreamRequestBody)
			if out.UpstreamRequestBody != "" {
				// Reuse the same sanitization/trimming strategy as request body storage.
				// Keep it small so it is safe to persist in ops_error_logs JSON.
				sanitized, truncated, _ := sanitizeAndTrimRequestBody([]byte(out.UpstreamRequestBody), 10*1024)
				if sanitized != "" {
					out.UpstreamRequestBody = sanitized
					if truncated {
						out.Kind = strings.TrimSpace(out.Kind)
						if out.Kind == "" {
							out.Kind = "upstream"
						}
						out.Kind = out.Kind + ":request_body_truncated"
					}
				} else {
					out.UpstreamRequestBody = ""
				}
			}

			// Drop fully-empty events (can happen if only status code was known).
			if out.UpstreamStatusCode == 0 && out.Message == "" && out.Detail == "" {
				continue
			}

			evCopy := out
			sanitized = append(sanitized, &evCopy)
		}

		entry.UpstreamErrorsJSON = marshalOpsUpstreamErrors(sanitized)
		entry.UpstreamErrors = nil
	}

	if _, err := s.opsRepo.InsertErrorLog(ctx, entry); err != nil {
		// Never bubble up to gateway; best-effort logging.
		log.Printf("[Ops] RecordError failed: %v", err)
		return err
	}
	return nil
}

func (s *OpsService) GetErrorLogs(ctx context.Context, filter *OpsErrorLogFilter) (*OpsErrorLogList, error) {
	if err := s.RequireMonitoringEnabled(ctx); err != nil {
		return nil, err
	}
	if s.opsRepo == nil {
		return &OpsErrorLogList{Errors: []*OpsErrorLog{}, Total: 0, Page: 1, PageSize: 20}, nil
	}
	result, err := s.opsRepo.ListErrorLogs(ctx, filter)
	if err != nil {
		log.Printf("[Ops] GetErrorLogs failed: %v", err)
		return nil, err
	}

	return result, nil
}

func (s *OpsService) GetErrorLogByID(ctx context.Context, id int64) (*OpsErrorLogDetail, error) {
	if err := s.RequireMonitoringEnabled(ctx); err != nil {
		return nil, err
	}
	if s.opsRepo == nil {
		return nil, infraerrors.NotFound("OPS_ERROR_NOT_FOUND", "ops error log not found")
	}
	detail, err := s.opsRepo.GetErrorLogByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, infraerrors.NotFound("OPS_ERROR_NOT_FOUND", "ops error log not found")
		}
		return nil, infraerrors.InternalServer("OPS_ERROR_LOAD_FAILED", "Failed to load ops error log").WithCause(err)
	}
	return detail, nil
}

func (s *OpsService) ListRetryAttemptsByErrorID(ctx context.Context, errorID int64, limit int) ([]*OpsRetryAttempt, error) {
	if err := s.RequireMonitoringEnabled(ctx); err != nil {
		return nil, err
	}
	if s.opsRepo == nil {
		return nil, infraerrors.ServiceUnavailable("OPS_REPO_UNAVAILABLE", "Ops repository not available")
	}
	if errorID <= 0 {
		return nil, infraerrors.BadRequest("OPS_ERROR_INVALID_ID", "invalid error id")
	}
	items, err := s.opsRepo.ListRetryAttemptsByErrorID(ctx, errorID, limit)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []*OpsRetryAttempt{}, nil
		}
		return nil, infraerrors.InternalServer("OPS_RETRY_LIST_FAILED", "Failed to list retry attempts").WithCause(err)
	}
	return items, nil
}

func (s *OpsService) UpdateErrorResolution(ctx context.Context, errorID int64, resolved bool, resolvedByUserID *int64, resolvedRetryID *int64) error {
	if err := s.RequireMonitoringEnabled(ctx); err != nil {
		return err
	}
	if s.opsRepo == nil {
		return infraerrors.ServiceUnavailable("OPS_REPO_UNAVAILABLE", "Ops repository not available")
	}
	if errorID <= 0 {
		return infraerrors.BadRequest("OPS_ERROR_INVALID_ID", "invalid error id")
	}
	// Best-effort ensure the error exists
	if _, err := s.opsRepo.GetErrorLogByID(ctx, errorID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return infraerrors.NotFound("OPS_ERROR_NOT_FOUND", "ops error log not found")
		}
		return infraerrors.InternalServer("OPS_ERROR_LOAD_FAILED", "Failed to load ops error log").WithCause(err)
	}
	return s.opsRepo.UpdateErrorResolution(ctx, errorID, resolved, resolvedByUserID, resolvedRetryID, nil)
}

func sanitizeAndTrimRequestBody(raw []byte, maxBytes int) (jsonString string, truncated bool, bytesLen int) {
	bytesLen = len(raw)
	if len(raw) == 0 {
		return "", false, 0
	}

	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		// If it's not valid JSON, don't store (retry would not be reliable anyway).
		return "", false, bytesLen
	}

	decoded = redactSensitiveJSON(decoded)

	encoded, err := json.Marshal(decoded)
	if err != nil {
		return "", false, bytesLen
	}
	if len(encoded) <= maxBytes {
		return string(encoded), false, bytesLen
	}

	// Trim conversation history to keep the most recent context.
	if root, ok := decoded.(map[string]any); ok {
		if trimmed, ok := trimConversationArrays(root, maxBytes); ok {
			encoded2, err2 := json.Marshal(trimmed)
			if err2 == nil && len(encoded2) <= maxBytes {
				return string(encoded2), true, bytesLen
			}
			// Fallthrough: keep shrinking.
			decoded = trimmed
		}

		essential := shrinkToEssentials(root)
		encoded3, err3 := json.Marshal(essential)
		if err3 == nil && len(encoded3) <= maxBytes {
			return string(encoded3), true, bytesLen
		}
	}

	// Last resort: keep JSON shape but drop big fields.
	// This avoids downstream code that expects certain top-level keys from crashing.
	if root, ok := decoded.(map[string]any); ok {
		placeholder := shallowCopyMap(root)
		placeholder["request_body_truncated"] = true

		// Replace potentially huge arrays/strings, but keep the keys present.
		for _, k := range []string{"messages", "contents", "input", "prompt"} {
			if _, exists := placeholder[k]; exists {
				placeholder[k] = []any{}
			}
		}
		for _, k := range []string{"text"} {
			if _, exists := placeholder[k]; exists {
				placeholder[k] = ""
			}
		}

		encoded4, err4 := json.Marshal(placeholder)
		if err4 == nil {
			if len(encoded4) <= maxBytes {
				return string(encoded4), true, bytesLen
			}
		}
	}

	// Final fallback: minimal valid JSON.
	encoded4, err4 := json.Marshal(map[string]any{"request_body_truncated": true})
	if err4 != nil {
		return "", true, bytesLen
	}
	return string(encoded4), true, bytesLen
}

func redactSensitiveJSON(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			if isSensitiveKey(k) {
				out[k] = "[REDACTED]"
				continue
			}
			out[k] = redactSensitiveJSON(vv)
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for _, vv := range t {
			out = append(out, redactSensitiveJSON(vv))
		}
		return out
	default:
		return v
	}
}

func isSensitiveKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if k == "" {
		return false
	}

	// Whitelist: known non-sensitive fields that contain sensitive substrings
	// (e.g., "max_tokens" contains "token" but is just an API parameter).
	switch k {
	case "max_tokens", "max_completion_tokens", "max_output_tokens",
		"completion_tokens", "prompt_tokens", "total_tokens",
		"input_tokens", "output_tokens",
		"cache_creation_input_tokens", "cache_read_input_tokens":
		return false
	}

	// Exact matches (common credential fields).
	switch k {
	case "authorization",
		"proxy-authorization",
		"x-api-key",
		"api_key",
		"apikey",
		"access_token",
		"refresh_token",
		"id_token",
		"session_token",
		"token",
		"password",
		"passwd",
		"passphrase",
		"secret",
		"client_secret",
		"private_key",
		"jwt",
		"signature",
		"accesskeyid",
		"secretaccesskey":
		return true
	}

	// Suffix matches.
	for _, suffix := range []string{
		"_secret",
		"_token",
		"_id_token",
		"_session_token",
		"_password",
		"_passwd",
		"_passphrase",
		"_key",
		"secret_key",
		"private_key",
	} {
		if strings.HasSuffix(k, suffix) {
			return true
		}
	}

	// Substring matches (conservative, but errs on the side of privacy).
	for _, sub := range []string{
		"secret",
		"token",
		"password",
		"passwd",
		"passphrase",
		"privatekey",
		"private_key",
		"apikey",
		"api_key",
		"accesskeyid",
		"secretaccesskey",
		"bearer",
		"cookie",
		"credential",
		"session",
		"jwt",
		"signature",
	} {
		if strings.Contains(k, sub) {
			return true
		}
	}

	return false
}

func trimConversationArrays(root map[string]any, maxBytes int) (map[string]any, bool) {
	// Supported: anthropic/openai: messages; gemini: contents.
	if out, ok := trimArrayField(root, "messages", maxBytes); ok {
		return out, true
	}
	if out, ok := trimArrayField(root, "contents", maxBytes); ok {
		return out, true
	}
	return root, false
}

func trimArrayField(root map[string]any, field string, maxBytes int) (map[string]any, bool) {
	raw, ok := root[field]
	if !ok {
		return nil, false
	}
	arr, ok := raw.([]any)
	if !ok || len(arr) == 0 {
		return nil, false
	}

	// Keep at least the last message/content. Use binary search so we don't marshal O(n) times.
	// We are dropping from the *front* of the array (oldest context first).
	lo := 0
	hi := len(arr) - 1 // inclusive; hi ensures at least one item remains

	var best map[string]any
	found := false

	for lo <= hi {
		mid := (lo + hi) / 2
		candidateArr := arr[mid:]
		if len(candidateArr) == 0 {
			lo = mid + 1
			continue
		}

		next := shallowCopyMap(root)
		next[field] = candidateArr
		encoded, err := json.Marshal(next)
		if err != nil {
			// If marshal fails, try dropping more.
			lo = mid + 1
			continue
		}

		if len(encoded) <= maxBytes {
			best = next
			found = true
			// Try to keep more context by dropping fewer items.
			hi = mid - 1
			continue
		}

		// Need to drop more.
		lo = mid + 1
	}

	if found {
		return best, true
	}

	// Nothing fit (even with only one element); return the smallest slice and let the
	// caller fall back to shrinkToEssentials().
	next := shallowCopyMap(root)
	next[field] = arr[len(arr)-1:]
	return next, true
}

func shrinkToEssentials(root map[string]any) map[string]any {
	out := make(map[string]any)
	for _, key := range []string{"model", "stream", "max_tokens", "temperature", "top_p", "top_k"} {
		if v, ok := root[key]; ok {
			out[key] = v
		}
	}

	// Keep only the last element of the conversation array.
	if v, ok := root["messages"]; ok {
		if arr, ok := v.([]any); ok && len(arr) > 0 {
			out["messages"] = []any{arr[len(arr)-1]}
		}
	}
	if v, ok := root["contents"]; ok {
		if arr, ok := v.([]any); ok && len(arr) > 0 {
			out["contents"] = []any{arr[len(arr)-1]}
		}
	}
	return out
}

func shallowCopyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func sanitizeErrorBodyForStorage(raw string, maxBytes int) (sanitized string, truncated bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}

	// Prefer JSON-safe sanitization when possible.
	if out, trunc, _ := sanitizeAndTrimRequestBody([]byte(raw), maxBytes); out != "" {
		return out, trunc
	}

	// Non-JSON: best-effort truncate.
	if maxBytes > 0 && len(raw) > maxBytes {
		return truncateString(raw, maxBytes), true
	}
	return raw, false
}
