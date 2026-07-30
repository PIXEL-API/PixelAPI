package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	grokReasoningItemCacheKeyPrefix = "grok:reasoning_item:"
	grokReasoningCacheTimeout       = 2 * time.Second
)

type grokReasoningCompatibilityError struct {
	statusCode    int
	errorType     string
	clientMessage string
	operation     string
	accountID     int64
	digest        string
	cause         error
}

func (e *grokReasoningCompatibilityError) Error() string {
	if e == nil {
		return "grok reasoning compatibility error"
	}
	message := fmt.Sprintf("grok reasoning compatibility %s failed: account_id=%d", e.operation, e.accountID)
	if e.digest != "" {
		message += " digest=" + e.digest
	}
	if e.cause != nil {
		message += ": " + e.cause.Error()
	}
	return message
}

func (e *grokReasoningCompatibilityError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func newGrokReasoningCompatibilityError(
	statusCode int,
	errorType string,
	clientMessage string,
	operation string,
	accountID int64,
	digest string,
	cause error,
) error {
	return &grokReasoningCompatibilityError{
		statusCode:    statusCode,
		errorType:     errorType,
		clientMessage: clientMessage,
		operation:     operation,
		accountID:     accountID,
		digest:        digest,
		cause:         cause,
	}
}

func writeGrokReasoningCompatibilityError(c *gin.Context, err error) bool {
	var compatibilityErr *grokReasoningCompatibilityError
	if !errors.As(err, &compatibilityErr) {
		return false
	}
	MarkResponseCommitted(c)
	writeOpenAICompactAwareJSONError(
		c,
		compatibilityErr.statusCode,
		compatibilityErr.errorType,
		compatibilityErr.clientMessage,
	)
	return true
}

// restoreGrokReasoningItems repairs only malformed Grok reasoning input items.
// The encrypted content remains supplied by the client; Redis stores only the
// original item metadata returned by xAI.
func (s *OpenAIGatewayService) restoreGrokReasoningItems(
	ctx context.Context,
	account *Account,
	body []byte,
) ([]byte, error) {
	if account == nil || account.Platform != PlatformGrok {
		return body, nil
	}

	input := gjson.GetBytes(body, "input")
	if !input.Exists() || !input.IsArray() {
		return body, nil
	}

	items := input.Array()
	patchedItems := make([]json.RawMessage, 0, len(items))
	changed := false
	for _, item := range items {
		encryptedContent, needsRestore, err := inspectGrokReasoningInputItem(item)
		if err != nil {
			return nil, newGrokReasoningCompatibilityError(
				http.StatusBadRequest,
				"invalid_request_error",
				"Invalid Grok reasoning state; start a new conversation.",
				"inspect_request",
				account.ID,
				"",
				err,
			)
		}
		if !needsRestore {
			patchedItems = append(patchedItems, json.RawMessage(item.Raw))
			continue
		}

		restored, err := s.loadGrokReasoningItem(ctx, account.ID, encryptedContent)
		if err != nil {
			if errors.Is(err, ErrGatewaySessionStringNotFound) {
				var inputItem map[string]any
				if decodeErr := json.Unmarshal([]byte(item.Raw), &inputItem); decodeErr != nil {
					return nil, newGrokReasoningCompatibilityError(
						http.StatusBadRequest,
						"invalid_request_error",
						"Invalid Grok reasoning state; start a new conversation.",
						"decode_request_item",
						account.ID,
						"",
						decodeErr,
					)
				}
				sanitized, _, keep := sanitizeEncryptedReasoningInputItem(inputItem)
				if keep {
					encoded, encodeErr := json.Marshal(sanitized)
					if encodeErr != nil {
						return nil, newGrokReasoningCompatibilityError(
							http.StatusInternalServerError,
							"api_error",
							"Failed to restore Grok reasoning state.",
							"encode_sanitized_item",
							account.ID,
							"",
							encodeErr,
						)
					}
					patchedItems = append(patchedItems, encoded)
				}
				digest, _ := grokReasoningItemCacheKey(encryptedContent)
				slog.Warn("grok reasoning state unavailable; continuing with portable history",
					"account_id", account.ID,
					"reasoning_digest", digest,
				)
				changed = true
				continue
			}
			return nil, err
		}
		patchedItems = append(patchedItems, restored)
		changed = true
	}

	if !changed {
		return body, nil
	}
	encodedItems, err := json.Marshal(patchedItems)
	if err != nil {
		return nil, newGrokReasoningCompatibilityError(
			http.StatusInternalServerError,
			"api_error",
			"Failed to restore Grok reasoning state.",
			"encode_request",
			account.ID,
			"",
			err,
		)
	}
	patchedBody, err := sjson.SetRawBytes(body, "input", encodedItems)
	if err != nil {
		return nil, newGrokReasoningCompatibilityError(
			http.StatusInternalServerError,
			"api_error",
			"Failed to restore Grok reasoning state.",
			"patch_request",
			account.ID,
			"",
			err,
		)
	}
	return patchedBody, nil
}

func inspectGrokReasoningInputItem(item gjson.Result) (string, bool, error) {
	if !item.IsObject() || strings.TrimSpace(item.Get("type").String()) != "reasoning" {
		return "", false, nil
	}
	encrypted := item.Get("encrypted_content")
	if !encrypted.Exists() {
		return "", false, nil
	}
	if encrypted.Type != gjson.String || encrypted.String() == "" {
		return "", false, errors.New("encrypted_content must be a non-empty string")
	}

	id := item.Get("id")
	status := item.Get("status")
	summary := item.Get("summary")
	hasID := id.Type == gjson.String && strings.TrimSpace(id.String()) != ""
	hasStatus := status.Type == gjson.String && strings.TrimSpace(status.String()) != ""
	hasSummary := summary.IsArray()
	return encrypted.String(), !hasID || !hasStatus || !hasSummary, nil
}

func (s *OpenAIGatewayService) loadGrokReasoningItem(
	ctx context.Context,
	accountID int64,
	encryptedContent string,
) (json.RawMessage, error) {
	digest, cacheKey := grokReasoningItemCacheKey(encryptedContent)
	if accountID <= 0 || s == nil || s.cache == nil {
		return nil, newGrokReasoningCompatibilityError(
			http.StatusServiceUnavailable,
			"api_error",
			"Grok reasoning state is temporarily unavailable; retry later.",
			"load_cache",
			accountID,
			digest,
			errors.New("gateway cache is unavailable"),
		)
	}

	readCtx, cancelRead := context.WithTimeout(context.WithoutCancel(ctx), grokReasoningCacheTimeout)
	cachedMetadata, err := s.cache.GetSessionString(readCtx, accountID, cacheKey)
	cancelRead()
	if err != nil {
		if errors.Is(err, ErrGatewaySessionStringNotFound) {
			return nil, newGrokReasoningCompatibilityError(
				http.StatusBadRequest,
				"invalid_request_error",
				"Grok reasoning state is missing or expired; start a new conversation.",
				"cache_miss",
				accountID,
				digest,
				err,
			)
		}
		return nil, newGrokReasoningCompatibilityError(
			http.StatusServiceUnavailable,
			"api_error",
			"Grok reasoning state is temporarily unavailable; retry later.",
			"read_cache",
			accountID,
			digest,
			err,
		)
	}

	restored, err := rebuildGrokReasoningItem([]byte(cachedMetadata), encryptedContent)
	if err != nil {
		return nil, newGrokReasoningCompatibilityError(
			http.StatusServiceUnavailable,
			"api_error",
			"Grok reasoning state is temporarily unavailable; retry later.",
			"decode_cache",
			accountID,
			digest,
			err,
		)
	}
	refreshCtx, cancelRefresh := context.WithTimeout(context.WithoutCancel(ctx), grokReasoningCacheTimeout)
	defer cancelRefresh()
	if err := s.cache.SetSessionString(refreshCtx, accountID, cacheKey, cachedMetadata, s.openAIWSSessionStickyTTL()); err != nil {
		return nil, newGrokReasoningCompatibilityError(
			http.StatusServiceUnavailable,
			"api_error",
			"Grok reasoning state is temporarily unavailable; retry later.",
			"refresh_cache",
			accountID,
			digest,
			err,
		)
	}
	return restored, nil
}

func rebuildGrokReasoningItem(metadataJSON []byte, encryptedContent string) (json.RawMessage, error) {
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
		return nil, fmt.Errorf("decode cached metadata: %w", err)
	}
	if err := validateGrokReasoningMetadata(metadata); err != nil {
		return nil, err
	}
	encodedEncryptedContent, err := json.Marshal(encryptedContent)
	if err != nil {
		return nil, fmt.Errorf("encode encrypted content: %w", err)
	}
	metadata["encrypted_content"] = encodedEncryptedContent
	restored, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("encode restored item: %w", err)
	}
	return restored, nil
}

func (s *OpenAIGatewayService) cacheGrokReasoningItemsFromResponsePayload(
	ctx context.Context,
	account *Account,
	payload []byte,
) error {
	return s.cacheGrokReasoningItemsFromResponsePayloadOnce(ctx, account, payload, nil)
}

func (s *OpenAIGatewayService) cacheGrokReasoningItemsFromResponsePayloadOnce(
	ctx context.Context,
	account *Account,
	payload []byte,
	seen map[string]struct{},
) error {
	if account == nil || account.Platform != PlatformGrok {
		return nil
	}
	items, err := extractGrokReasoningItemsFromResponsePayload(payload)
	if err != nil {
		return newGrokReasoningCompatibilityError(
			http.StatusBadGateway,
			"api_error",
			"Grok returned an invalid reasoning state.",
			"inspect_response",
			account.ID,
			"",
			err,
		)
	}
	return s.cacheGrokReasoningItems(ctx, account.ID, items, seen)
}

func (s *OpenAIGatewayService) cacheGrokReasoningItemsFromSSEBody(
	ctx context.Context,
	account *Account,
	bodyText string,
) error {
	if account == nil || account.Platform != PlatformGrok {
		return nil
	}
	var items []json.RawMessage
	var collectErr error
	forEachOpenAISSEDataPayload(bodyText, func(data []byte) {
		if collectErr != nil {
			return
		}
		var extracted []json.RawMessage
		extracted, collectErr = extractGrokReasoningItemsFromResponsePayload(data)
		items = append(items, extracted...)
	})
	if collectErr != nil {
		return newGrokReasoningCompatibilityError(
			http.StatusBadGateway,
			"api_error",
			"Grok returned an invalid reasoning state.",
			"inspect_sse_response",
			account.ID,
			"",
			collectErr,
		)
	}
	return s.cacheGrokReasoningItems(ctx, account.ID, items, nil)
}

func extractGrokReasoningItemsFromResponsePayload(payload []byte) ([]json.RawMessage, error) {
	if !bytes.Contains(payload, []byte(`"encrypted_content"`)) {
		return nil, nil
	}
	if !gjson.ValidBytes(payload) {
		return nil, errors.New("response payload containing encrypted_content is not valid JSON")
	}

	root := gjson.ParseBytes(payload)
	items := make([]json.RawMessage, 0, 1)
	appendItem := func(item gjson.Result) {
		if !item.IsObject() || strings.TrimSpace(item.Get("type").String()) != "reasoning" {
			return
		}
		encrypted := item.Get("encrypted_content")
		if encrypted.Type != gjson.String || encrypted.String() == "" {
			return
		}
		items = append(items, json.RawMessage(append([]byte(nil), item.Raw...)))
	}

	switch strings.TrimSpace(root.Get("type").String()) {
	case "response.output_item.done":
		appendItem(root.Get("item"))
	case "response.completed", "response.done":
		for _, item := range root.Get("response.output").Array() {
			appendItem(item)
		}
	case "response":
		for _, item := range root.Get("output").Array() {
			appendItem(item)
		}
	default:
		if root.Get("output").IsArray() {
			for _, item := range root.Get("output").Array() {
				appendItem(item)
			}
		}
	}
	return items, nil
}

func (s *OpenAIGatewayService) cacheGrokReasoningItems(
	ctx context.Context,
	accountID int64,
	items []json.RawMessage,
	seen map[string]struct{},
) error {
	if len(items) == 0 {
		return nil
	}
	if accountID <= 0 || s == nil || s.cache == nil {
		return newGrokReasoningCompatibilityError(
			http.StatusServiceUnavailable,
			"api_error",
			"Grok reasoning state is temporarily unavailable; retry later.",
			"write_cache",
			accountID,
			"",
			errors.New("gateway cache is unavailable"),
		)
	}

	cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), grokReasoningCacheTimeout)
	defer cancel()
	if seen == nil {
		seen = make(map[string]struct{}, len(items))
	}
	for _, item := range items {
		digest, cacheKey, metadata, err := encodeGrokReasoningMetadata(item)
		if err != nil {
			return newGrokReasoningCompatibilityError(
				http.StatusBadGateway,
				"api_error",
				"Grok returned an invalid reasoning state.",
				"encode_response",
				accountID,
				digest,
				err,
			)
		}
		if _, duplicate := seen[cacheKey]; duplicate {
			continue
		}
		seen[cacheKey] = struct{}{}
		if err := s.cache.SetSessionString(cacheCtx, accountID, cacheKey, metadata, s.openAIWSSessionStickyTTL()); err != nil {
			return newGrokReasoningCompatibilityError(
				http.StatusServiceUnavailable,
				"api_error",
				"Grok reasoning state is temporarily unavailable; retry later.",
				"write_cache",
				accountID,
				digest,
				err,
			)
		}
	}
	return nil
}

func encodeGrokReasoningMetadata(item json.RawMessage) (string, string, string, error) {
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(item, &metadata); err != nil {
		return "", "", "", fmt.Errorf("decode reasoning item: %w", err)
	}
	if err := validateGrokReasoningMetadata(metadata); err != nil {
		return "", "", "", err
	}

	var encryptedContent string
	if err := json.Unmarshal(metadata["encrypted_content"], &encryptedContent); err != nil || encryptedContent == "" {
		if err == nil {
			err = errors.New("encrypted_content is empty")
		}
		return "", "", "", fmt.Errorf("decode encrypted_content: %w", err)
	}
	digest, cacheKey := grokReasoningItemCacheKey(encryptedContent)
	delete(metadata, "encrypted_content")
	encodedMetadata, err := json.Marshal(metadata)
	if err != nil {
		return digest, cacheKey, "", fmt.Errorf("encode reasoning metadata: %w", err)
	}
	return digest, cacheKey, string(encodedMetadata), nil
}

func validateGrokReasoningMetadata(metadata map[string]json.RawMessage) error {
	if metadata == nil {
		return errors.New("reasoning item metadata is empty")
	}
	var itemType string
	if err := json.Unmarshal(metadata["type"], &itemType); err != nil || itemType != "reasoning" {
		return errors.New("reasoning item type is invalid")
	}
	var id string
	if err := json.Unmarshal(metadata["id"], &id); err != nil || strings.TrimSpace(id) == "" {
		return errors.New("reasoning item id is missing")
	}
	var status string
	if err := json.Unmarshal(metadata["status"], &status); err != nil || strings.TrimSpace(status) == "" {
		return errors.New("reasoning item status is missing")
	}
	var summary []json.RawMessage
	if rawSummary, ok := metadata["summary"]; !ok || json.Unmarshal(rawSummary, &summary) != nil || summary == nil {
		return errors.New("reasoning item summary is missing")
	}
	return nil
}

func grokReasoningItemCacheKey(encryptedContent string) (string, string) {
	sum := sha256.Sum256([]byte(encryptedContent))
	digest := hex.EncodeToString(sum[:])
	return digest, grokReasoningItemCacheKeyPrefix + digest
}
