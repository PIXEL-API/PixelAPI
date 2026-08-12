package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type codexFingerprintMode string

const (
	codexFingerprintOff     codexFingerprintMode = "off"
	codexFingerprintDevice  codexFingerprintMode = "device"
	codexFingerprintSession codexFingerprintMode = "session"
	codexFingerprintFull    codexFingerprintMode = "full"
)

const (
	codexFingerprintModeExtraKey  = "codex_fingerprint_mode"
	codexFingerprintIDsContextKey = "codex_fingerprint_ids"
)

// GetCodexFingerprintMode returns the account-level convergence policy.
// OpenAI OAuth accounts default to session convergence for backward-compatible
// protection; only an explicit "off" disables it.
func (a *Account) GetCodexFingerprintMode() codexFingerprintMode {
	if a == nil || !a.IsOpenAIOAuth() {
		return codexFingerprintOff
	}
	raw := codexFingerprintMode(strings.TrimSpace(a.GetExtraString(codexFingerprintModeExtraKey)))
	switch raw {
	case codexFingerprintOff, codexFingerprintDevice, codexFingerprintSession, codexFingerprintFull:
		return raw
	default:
		return codexFingerprintSession
	}
}

func deriveStableUUIDv4(seed string) string {
	hash := sha256.Sum256([]byte(seed))
	value := hash[:16]
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		binary.BigEndian.Uint32(value[0:4]),
		binary.BigEndian.Uint16(value[4:6]),
		binary.BigEndian.Uint16(value[6:8]),
		binary.BigEndian.Uint16(value[8:10]),
		value[10:16],
	)
}

func resolveConvergedInstallationID(account *Account) string {
	if account == nil {
		return ""
	}
	if deviceID := account.GetOpenAIDeviceID(); deviceID != "" {
		return deviceID
	}
	return deriveStableUUIDv4(fmt.Sprintf("sub2api:codex-install-id:v1:%d", account.ID))
}

func resolveConvergedSessionID(account *Account) string {
	if account == nil {
		return ""
	}
	return deriveStableUUIDv4(fmt.Sprintf("sub2api:codex-session-id:v1:%d", account.ID))
}

func resolveConvergedThreadID(account *Account, clientSessionID string) string {
	if account == nil || strings.TrimSpace(clientSessionID) == "" {
		return ""
	}
	return deriveStableUUIDv4(fmt.Sprintf("sub2api:codex-thread-id:v1:%d:%s", account.ID, clientSessionID))
}

type codexFingerprintIDs struct {
	accountID         int64
	mode              codexFingerprintMode
	installationID    string
	sessionID         string
	threadID          string
	turnID            string
	windowID          string
	turnStartedAtUnix int64
}

// codexFingerprintApplyPolicy makes the writer precedence explicit. When
// Clean Relay is active it alone owns installation/session/conversation/cache;
// the account policy may only add the non-conflicting thread/turn/window IDs.
type codexFingerprintApplyPolicy struct {
	cleanRelayAuthoritative bool
}

func resolveCodexFingerprintIDs(account *Account, clientSessionID string, mode codexFingerprintMode) *codexFingerprintIDs {
	if account == nil || mode == codexFingerprintOff {
		return nil
	}

	ids := &codexFingerprintIDs{
		accountID:      account.ID,
		mode:           mode,
		installationID: resolveConvergedInstallationID(account),
	}
	if ids.installationID == "" {
		return nil
	}
	if mode == codexFingerprintDevice {
		return ids
	}

	ids.sessionID = resolveConvergedSessionID(account)
	if mode == codexFingerprintFull {
		ids.threadID = ids.sessionID
	} else {
		ids.threadID = resolveConvergedThreadID(account, clientSessionID)
		if ids.threadID == "" {
			ids.threadID = ids.sessionID
		}
	}
	ids.turnID = uuid.Must(uuid.NewV7()).String()
	ids.windowID = ids.threadID + ":0"
	ids.turnStartedAtUnix = time.Now().UnixMilli()
	return ids
}

func extractClientSessionID(headers http.Header) string {
	if value := strings.TrimSpace(headers.Get("session-id")); value != "" {
		return value
	}
	return strings.TrimSpace(headers.Get("session_id"))
}

func resolveCodexFingerprintIDsFromRequest(account *Account, clientHeaders http.Header) *codexFingerprintIDs {
	if account == nil {
		return nil
	}
	mode := account.GetCodexFingerprintMode()
	if mode == codexFingerprintOff {
		return nil
	}
	return resolveCodexFingerprintIDs(account, extractClientSessionID(clientHeaders), mode)
}

func codexFingerprintTurnFields(ids *codexFingerprintIDs, includeCleanRelayOwned bool) map[string]any {
	if ids == nil || ids.mode == codexFingerprintDevice {
		return nil
	}
	fields := map[string]any{
		"thread_id":               ids.threadID,
		"turn_id":                 ids.turnID,
		"window_id":               ids.windowID,
		"turn_started_at_unix_ms": ids.turnStartedAtUnix,
	}
	if includeCleanRelayOwned {
		fields["installation_id"] = ids.installationID
		fields["session_id"] = ids.sessionID
	}
	return fields
}

func applyCodexFingerprintHeaders(headers http.Header, ids *codexFingerprintIDs, policy codexFingerprintApplyPolicy) bool {
	if headers == nil || ids == nil {
		return false
	}
	if policy.cleanRelayAuthoritative {
		if ids.mode == codexFingerprintDevice {
			return false
		}
		applyCodexFingerprintThreadHeaders(headers, ids)
		rewriteCodexTurnMetadataFields(headers, codexFingerprintTurnFields(ids, false))
		return true
	}

	headers.Set(openAICleanRelayInstallationField, ids.installationID)
	if ids.mode == codexFingerprintDevice {
		rewriteCodexTurnMetadataFields(headers, map[string]any{"installation_id": ids.installationID})
		return true
	}
	applyCodexFingerprintThreadHeaders(headers, ids)
	headers.Set("session-id", ids.sessionID)
	headers.Set("session_id", ids.sessionID)
	rewriteCodexTurnMetadataFields(headers, codexFingerprintTurnFields(ids, true))
	return true
}

func applyCodexFingerprintThreadHeaders(headers http.Header, ids *codexFingerprintIDs) {
	headers.Set("x-codex-window-id", ids.windowID)
	headers.Set("x-client-request-id", ids.threadID)
	headers.Set("thread-id", ids.threadID)
}

func rewriteCodexTurnMetadataFields(headers http.Header, fields map[string]any) bool {
	if headers == nil || len(fields) == 0 {
		return false
	}
	raw := strings.TrimSpace(headers.Get("x-codex-turn-metadata"))
	if raw == "" {
		return false
	}
	var metadata map[string]any
	if err := decodeJSONPreservingNumbers([]byte(raw), &metadata); err != nil {
		return false
	}
	for key, value := range fields {
		metadata[key] = value
	}
	rebuilt, err := json.Marshal(metadata)
	if err != nil {
		return false
	}
	headers.Set("x-codex-turn-metadata", string(rebuilt))
	return true
}

func applyCodexFingerprintClientMetadata(reqBody map[string]any, ids *codexFingerprintIDs, policy codexFingerprintApplyPolicy) bool {
	if reqBody == nil || ids == nil {
		return false
	}
	if policy.cleanRelayAuthoritative && ids.mode == codexFingerprintDevice {
		return false
	}

	metadata := normalizeCodexClientMetadata(reqBody["client_metadata"])
	if !policy.cleanRelayAuthoritative {
		metadata[openAICleanRelayInstallationField] = ids.installationID
		if ids.mode == codexFingerprintDevice {
			rewriteClientMetadataEmbeddedTurnMetadata(metadata, map[string]any{"installation_id": ids.installationID})
			reqBody["client_metadata"] = metadata
			return true
		}
		metadata["session_id"] = ids.sessionID
	}

	metadata["thread_id"] = ids.threadID
	metadata["turn_id"] = ids.turnID
	metadata["x-codex-window-id"] = ids.windowID
	rewriteClientMetadataEmbeddedTurnMetadata(metadata, codexFingerprintTurnFields(ids, !policy.cleanRelayAuthoritative))
	reqBody["client_metadata"] = metadata
	return true
}

func normalizeCodexClientMetadata(value any) map[string]any {
	switch metadata := value.(type) {
	case map[string]any:
		return metadata
	case map[string]string:
		next := make(map[string]any, len(metadata))
		for key, item := range metadata {
			next[key] = item
		}
		return next
	default:
		return make(map[string]any)
	}
}

func rewriteClientMetadataEmbeddedTurnMetadata(clientMetadata map[string]any, fields map[string]any) bool {
	if clientMetadata == nil || len(fields) == 0 {
		return false
	}
	raw, ok := clientMetadata["x-codex-turn-metadata"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return false
	}
	var metadata map[string]any
	if err := decodeJSONPreservingNumbers([]byte(raw), &metadata); err != nil {
		return false
	}
	for key, value := range fields {
		metadata[key] = value
	}
	rebuilt, err := json.Marshal(metadata)
	if err != nil {
		return false
	}
	clientMetadata["x-codex-turn-metadata"] = string(rebuilt)
	return true
}

func applyCodexFingerprintRawClientMetadata(body []byte, ids *codexFingerprintIDs, policy codexFingerprintApplyPolicy) ([]byte, bool, error) {
	if len(body) == 0 || ids == nil {
		return body, false, nil
	}
	if !json.Valid(body) {
		return body, false, fmt.Errorf("parse codex fingerprint request body: invalid JSON")
	}

	var metadataValue any
	metadataResult := gjson.GetBytes(body, "client_metadata")
	if metadataResult.Exists() {
		if metadataResult.Raw == "" || !gjson.Valid(metadataResult.Raw) {
			return body, false, fmt.Errorf("parse codex fingerprint client_metadata: invalid JSON")
		}
		if err := decodeJSONPreservingNumbers([]byte(metadataResult.Raw), &metadataValue); err != nil {
			return body, false, fmt.Errorf("parse codex fingerprint client_metadata: %w", err)
		}
	}
	reqBody := map[string]any{"client_metadata": metadataValue}
	if !applyCodexFingerprintClientMetadata(reqBody, ids, policy) {
		return body, false, nil
	}
	metadata, err := json.Marshal(reqBody["client_metadata"])
	if err != nil {
		return body, false, fmt.Errorf("serialize codex fingerprint client_metadata: %w", err)
	}
	rebuilt, err := sjson.SetRawBytes(body, "client_metadata", metadata)
	if err != nil {
		return body, false, fmt.Errorf("set codex fingerprint client_metadata: %w", err)
	}
	return rebuilt, true, nil
}

// decodeJSONPreservingNumbers keeps opaque integer values exact when a small
// JSON subtree must be rewritten. json.Valid preserves json.Unmarshal's
// single-value/trailing-data validation while UseNumber avoids float64 loss.
func decodeJSONPreservingNumbers(raw []byte, target any) error {
	if !json.Valid(raw) {
		return fmt.Errorf("invalid JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(target)
}

func setCurrentCodexFingerprintIDs(c *gin.Context, ids *codexFingerprintIDs) {
	if c == nil {
		return
	}
	c.Set(codexFingerprintIDsContextKey, ids)
}

func getCurrentCodexFingerprintIDs(c *gin.Context) *codexFingerprintIDs {
	if c == nil {
		return nil
	}
	value, exists := c.Get(codexFingerprintIDsContextKey)
	if !exists {
		return nil
	}
	ids, _ := value.(*codexFingerprintIDs)
	return ids
}

func codexFingerprintClientHeaders(c *gin.Context) http.Header {
	if c == nil || c.Request == nil {
		return nil
	}
	return c.Request.Header
}

func (s *OpenAIGatewayService) codexFingerprintPolicy(ctx context.Context, account *Account) codexFingerprintApplyPolicy {
	return codexFingerprintApplyPolicy{
		cleanRelayAuthoritative: s.isOpenAICleanRelayActive(ctx, account),
	}
}

func (s *OpenAIGatewayService) applyCodexFingerprintToRequestBody(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	reqBody map[string]any,
) (*codexFingerprintIDs, bool) {
	ids := resolveCodexFingerprintIDsForTurn(c, account)
	setCurrentCodexFingerprintIDs(c, ids)
	if ids == nil {
		return nil, false
	}
	return ids, applyCodexFingerprintClientMetadata(reqBody, ids, s.codexFingerprintPolicy(ctx, account))
}

func (s *OpenAIGatewayService) applyCodexFingerprintToRawBody(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
) ([]byte, *codexFingerprintIDs, bool, error) {
	ids := resolveCodexFingerprintIDsForTurn(c, account)
	setCurrentCodexFingerprintIDs(c, ids)
	if ids == nil {
		return body, nil, false, nil
	}
	rewritten, changed, err := applyCodexFingerprintRawClientMetadata(body, ids, s.codexFingerprintPolicy(ctx, account))
	return rewritten, ids, changed, err
}

func resolveCodexFingerprintIDsForTurn(c *gin.Context, account *Account) *codexFingerprintIDs {
	return resolveCodexFingerprintIDsFromRequest(account, codexFingerprintClientHeaders(c))
}

func (s *OpenAIGatewayService) applyCurrentCodexFingerprintHeaders(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	headers http.Header,
) bool {
	ids := getCurrentCodexFingerprintIDs(c)
	if ids == nil {
		return false
	}
	if account == nil || !account.IsOpenAIOAuth() || ids.accountID != account.ID || ids.mode != account.GetCodexFingerprintMode() {
		setCurrentCodexFingerprintIDs(c, nil)
		return false
	}
	return applyCodexFingerprintHeaders(headers, ids, s.codexFingerprintPolicy(ctx, account))
}

func currentCodexFingerprintOwnsSession(c *gin.Context, account *Account) bool {
	ids := getCurrentCodexFingerprintIDs(c)
	if ids == nil || account == nil || !account.IsOpenAIOAuth() || ids.accountID != account.ID {
		return false
	}
	return ids.mode == account.GetCodexFingerprintMode() &&
		(ids.mode == codexFingerprintSession || ids.mode == codexFingerprintFull)
}

func resetOpenAIRequestIdentityState(c *gin.Context) {
	setCurrentCodexFingerprintIDs(c, nil)
	clearOpenAICleanRelayState(c)
}
