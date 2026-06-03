package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	openAICleanRelayContextKey        = "openai_clean_relay_state"
	openAICleanRelayGroupContextKey   = "openai_clean_relay_group_id"
	openAICleanRelayInstallationField = "x-codex-installation-id"
	openAICleanRelayCacheKeyPrefix    = "openai:clean_relay:"
)

type openAICleanRelayMapping struct {
	AccountID      int64  `json:"account_id"`
	Epoch          int64  `json:"epoch"`
	InstallationID string `json:"installation_id"`
	SessionID      string `json:"session_id"`
	ConversationID string `json:"conversation_id"`
	PromptCacheKey string `json:"prompt_cache_key"`
}

type openAICleanRelayState struct {
	Mapping                 openAICleanRelayMapping
	CleanStart              bool
	Ephemeral               bool
	AllowBodyClientMetadata bool
	bodyCleaned             bool
	headersCleaned          bool
}

func (s *OpenAIGatewayService) applyOpenAICleanRelayToRequestBody(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	reqBody map[string]any,
	bodyForSession []byte,
) (*openAICleanRelayState, bool, error) {
	if len(reqBody) == 0 {
		return nil, false, nil
	}
	if existing := getOpenAICleanRelayState(c); existing != nil && account != nil && existing.Mapping.AccountID == account.ID {
		changed := applyOpenAICleanRelayMappingToBody(reqBody, existing)
		return existing, changed, nil
	}
	state, err := s.resolveOpenAICleanRelayState(ctx, c, account, reqBody, bodyForSession)
	if err != nil || state == nil {
		return state, false, err
	}
	changed := applyOpenAICleanRelayMappingToBody(reqBody, state)
	setOpenAICleanRelayState(c, state)
	return state, changed, nil
}

func (s *OpenAIGatewayService) applyOpenAICleanRelayToRawBody(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	bodyForSession []byte,
) ([]byte, *openAICleanRelayState, bool, error) {
	if len(body) == 0 {
		return body, nil, false, nil
	}
	if !s.isOpenAICleanRelayActive(ctx, account) {
		return body, nil, false, nil
	}
	var reqBody map[string]any
	if err := json.Unmarshal(body, &reqBody); err != nil {
		return body, nil, false, fmt.Errorf("openai clean relay parse request body: %w", err)
	}
	if len(bodyForSession) == 0 {
		bodyForSession = body
	}
	state, changed, err := s.applyOpenAICleanRelayToRequestBody(ctx, c, account, reqBody, bodyForSession)
	if err != nil || state == nil || !changed {
		return body, state, changed, err
	}
	rebuilt, err := json.Marshal(reqBody)
	if err != nil {
		return body, state, false, fmt.Errorf("openai clean relay serialize request body: %w", err)
	}
	return rebuilt, state, true, nil
}

func (s *OpenAIGatewayService) SelectAccountWithCleanRelayScheduler(
	ctx context.Context,
	c *gin.Context,
	groupID *int64,
	previousResponseID string,
	sessionHash string,
	requestedModel string,
	routingModel string,
	excludedIDs map[int64]struct{},
	requiredTransport OpenAIUpstreamTransport,
	requireCompact bool,
	bodyForSession []byte,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	setOpenAICleanRelayGroupID(c, groupID)
	effectiveModel := strings.TrimSpace(routingModel)
	if effectiveModel == "" {
		effectiveModel = strings.TrimSpace(requestedModel)
	}
	selection, decision, hit, err := s.selectOpenAICleanRelayMappedAccount(
		ctx,
		c,
		groupID,
		effectiveModel,
		excludedIDs,
		requiredTransport,
		requireCompact,
		bodyForSession,
	)
	if err != nil {
		return nil, decision, err
	}
	if hit {
		return selection, decision, nil
	}
	return s.SelectAccountWithScheduler(
		ctx,
		groupID,
		previousResponseID,
		sessionHash,
		effectiveModel,
		excludedIDs,
		requiredTransport,
		requireCompact,
	)
}

func (s *OpenAIGatewayService) selectOpenAICleanRelayMappedAccount(
	ctx context.Context,
	c *gin.Context,
	groupID *int64,
	requestedModel string,
	excludedIDs map[int64]struct{},
	requiredTransport OpenAIUpstreamTransport,
	requireCompact bool,
	bodyForSession []byte,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, bool, error) {
	decision := OpenAIAccountScheduleDecision{Layer: openAIAccountScheduleLayerCleanRelay}
	mapping, hit, err := s.loadOpenAICleanRelayCachedMapping(ctx, c, bodyForSession)
	if err != nil || !hit {
		return nil, decision, false, err
	}
	if mapping.AccountID <= 0 {
		return nil, decision, false, nil
	}
	if excludedIDs != nil {
		if _, excluded := excludedIDs[mapping.AccountID]; excluded {
			return nil, decision, false, nil
		}
	}
	selection, err := s.selectOpenAICleanRelayAccountByID(
		ctx,
		groupID,
		mapping.AccountID,
		requestedModel,
		requiredTransport,
		requireCompact,
	)
	if err != nil {
		return nil, decision, true, err
	}
	if selection == nil || selection.Account == nil {
		return nil, decision, false, nil
	}
	decision.StickySessionHit = true
	decision.SelectedAccountID = selection.Account.ID
	decision.SelectedAccountType = selection.Account.Type
	return selection, decision, true, nil
}

func (s *OpenAIGatewayService) loadOpenAICleanRelayCachedMapping(
	ctx context.Context,
	c *gin.Context,
	bodyForSession []byte,
) (openAICleanRelayMapping, bool, error) {
	if !s.IsOpenAICleanRelayEnabled(ctx) || c == nil || len(bodyForSession) == 0 || s.cache == nil {
		return openAICleanRelayMapping{}, false, nil
	}
	var reqBody map[string]any
	if err := json.Unmarshal(bodyForSession, &reqBody); err != nil {
		return openAICleanRelayMapping{}, false, fmt.Errorf("openai clean relay parse request body before account selection: %w", err)
	}
	clientInstallationID := openAICleanRelayClientInstallationID(c, reqBody)
	sessionSignal := openAICleanRelayClientSessionSignal(c, reqBody, bodyForSession)
	if strings.TrimSpace(sessionSignal) == "" {
		return openAICleanRelayMapping{}, false, nil
	}
	apiKeyID := getAPIKeyIDFromContext(c)
	groupID := getOpenAICleanRelayGroupID(c)
	cacheKey := openAICleanRelayCacheKey(apiKeyID, groupID, clientInstallationID, sessionSignal)
	cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()

	raw, err := s.cache.GetSessionString(cacheCtx, groupID, cacheKey)
	if err != nil {
		if errors.Is(err, ErrGatewaySessionStringNotFound) {
			return openAICleanRelayMapping{}, false, nil
		}
		return openAICleanRelayMapping{}, false, fmt.Errorf("openai clean relay load mapping before account selection: %w", err)
	}
	if strings.TrimSpace(raw) == "" {
		return openAICleanRelayMapping{}, false, nil
	}
	var mapping openAICleanRelayMapping
	if err := json.Unmarshal([]byte(raw), &mapping); err != nil {
		return openAICleanRelayMapping{}, false, fmt.Errorf("openai clean relay decode mapping before account selection: %w", err)
	}
	if mapping.AccountID <= 0 || mapping.InstallationID == "" || mapping.SessionID == "" || mapping.ConversationID == "" || mapping.PromptCacheKey == "" {
		return openAICleanRelayMapping{}, false, errors.New("openai clean relay mapping is incomplete before account selection")
	}
	return mapping, true, nil
}

func (s *OpenAIGatewayService) selectOpenAICleanRelayAccountByID(
	ctx context.Context,
	groupID *int64,
	accountID int64,
	requestedModel string,
	requiredTransport OpenAIUpstreamTransport,
	requireCompact bool,
) (*AccountSelectionResult, error) {
	account, err := s.getSchedulableAccount(ctx, accountID)
	if err != nil || account == nil {
		return nil, nil
	}
	if !s.isOpenAICleanRelayAccountCandidate(ctx, account) {
		return nil, nil
	}
	if !s.isOpenAIAccountTransportCompatible(account, requiredTransport) {
		return nil, nil
	}
	account = s.recheckSelectedOpenAIAccountFromDB(ctx, groupID, account, requestedModel, requireCompact)
	if account == nil || !s.isOpenAICleanRelayAccountCandidate(ctx, account) {
		return nil, nil
	}
	if !s.isOpenAIAccountTransportCompatible(account, requiredTransport) {
		return nil, nil
	}
	if groupID != nil && s.needsUpstreamChannelRestrictionCheck(ctx, groupID) &&
		s.isUpstreamModelRestrictedByChannel(ctx, *groupID, account, requestedModel, requireCompact) {
		return nil, nil
	}
	result, err := s.tryAcquireAccountSlot(ctx, account.ID, account.Concurrency)
	if err != nil {
		return nil, err
	}
	if result != nil && result.Acquired {
		return s.newSelectionResult(ctx, account, true, result.ReleaseFunc, nil)
	}
	cfg := s.schedulingConfig()
	return s.newSelectionResult(ctx, account, false, nil, &AccountWaitPlan{
		AccountID:      account.ID,
		MaxConcurrency: account.Concurrency,
		Timeout:        cfg.StickySessionWaitTimeout,
		MaxWaiting:     cfg.StickySessionMaxWaiting,
	})
}

func (s *OpenAIGatewayService) isOpenAICleanRelayAccountCandidate(ctx context.Context, account *Account) bool {
	return s.isOpenAICleanRelayActive(ctx, account) && account.IsOpenAI() && account.IsSchedulable()
}

func (s *OpenAIGatewayService) resolveOpenAICleanRelayState(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	reqBody map[string]any,
	bodyForSession []byte,
) (*openAICleanRelayState, error) {
	if !s.isOpenAICleanRelayActive(ctx, account) {
		return nil, nil
	}

	accountID := account.ID
	upstreamInstallationID := openAICleanRelayInstallationID(accountID)
	clientInstallationID := openAICleanRelayClientInstallationID(c, reqBody)
	sessionSignal := openAICleanRelayClientSessionSignal(c, reqBody, bodyForSession)
	allowBodyClientMetadata := !isOpenAICleanRelayCompactRequest(c)
	if sessionSignal == "" {
		return &openAICleanRelayState{
			Mapping:                 newOpenAICleanRelayMapping(accountID, 1, upstreamInstallationID),
			CleanStart:              true,
			Ephemeral:               true,
			AllowBodyClientMetadata: allowBodyClientMetadata,
		}, nil
	}

	if s.cache == nil {
		return nil, errors.New("openai clean relay cache is unavailable")
	}

	apiKeyID := getAPIKeyIDFromContext(c)
	groupID := getOpenAICleanRelayGroupID(c)
	cacheKey := openAICleanRelayCacheKey(apiKeyID, groupID, clientInstallationID, sessionSignal)
	cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()

	raw, err := s.cache.GetSessionString(cacheCtx, groupID, cacheKey)
	if err != nil && !errors.Is(err, ErrGatewaySessionStringNotFound) {
		return nil, fmt.Errorf("openai clean relay load mapping: %w", err)
	}
	if errors.Is(err, ErrGatewaySessionStringNotFound) || strings.TrimSpace(raw) == "" {
		mapping := newOpenAICleanRelayMapping(accountID, 1, upstreamInstallationID)
		encoded, encodeErr := marshalOpenAICleanRelayMapping(mapping)
		if encodeErr != nil {
			return nil, encodeErr
		}
		if err := s.cache.SetSessionString(cacheCtx, groupID, cacheKey, encoded, s.openAIWSSessionStickyTTL()); err != nil {
			return nil, fmt.Errorf("openai clean relay save mapping: %w", err)
		}
		return &openAICleanRelayState{Mapping: mapping, CleanStart: true, AllowBodyClientMetadata: allowBodyClientMetadata}, nil
	}

	var mapping openAICleanRelayMapping
	if err := json.Unmarshal([]byte(raw), &mapping); err != nil {
		return nil, fmt.Errorf("openai clean relay decode mapping: %w", err)
	}
	if mapping.AccountID <= 0 || mapping.InstallationID == "" || mapping.SessionID == "" || mapping.ConversationID == "" || mapping.PromptCacheKey == "" {
		return nil, errors.New("openai clean relay mapping is incomplete")
	}

	if mapping.AccountID == accountID {
		encoded, encodeErr := marshalOpenAICleanRelayMapping(mapping)
		if encodeErr != nil {
			return nil, encodeErr
		}
		if err := s.cache.SetSessionString(cacheCtx, groupID, cacheKey, encoded, s.openAIWSSessionStickyTTL()); err != nil {
			return nil, fmt.Errorf("openai clean relay refresh mapping: %w", err)
		}
		return &openAICleanRelayState{Mapping: mapping, AllowBodyClientMetadata: allowBodyClientMetadata}, nil
	}

	nextEpoch := mapping.Epoch + 1
	if nextEpoch <= 0 {
		nextEpoch = 1
	}
	mapping = newOpenAICleanRelayMapping(accountID, nextEpoch, upstreamInstallationID)
	encoded, encodeErr := marshalOpenAICleanRelayMapping(mapping)
	if encodeErr != nil {
		return nil, encodeErr
	}
	if err := s.cache.SetSessionString(cacheCtx, groupID, cacheKey, encoded, s.openAIWSSessionStickyTTL()); err != nil {
		return nil, fmt.Errorf("openai clean relay migrate mapping: %w", err)
	}
	return &openAICleanRelayState{Mapping: mapping, CleanStart: true, AllowBodyClientMetadata: allowBodyClientMetadata}, nil
}

func (s *OpenAIGatewayService) isOpenAICleanRelayActive(ctx context.Context, account *Account) bool {
	if s == nil || s.settingService == nil || account == nil {
		return false
	}
	if account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
		return false
	}
	return s.IsOpenAICleanRelayEnabled(ctx)
}

// IsOpenAICleanRelayEnabled reports whether the gateway-level clean relay mode
// is enabled, independent of any account-specific applicability checks.
func (s *OpenAIGatewayService) IsOpenAICleanRelayEnabled(ctx context.Context) bool {
	if s == nil || s.settingService == nil {
		return false
	}
	return s.settingService.IsOpenAICleanRelayEnabled(ctx)
}

func newOpenAICleanRelayMapping(accountID, epoch int64, installationID string) openAICleanRelayMapping {
	sessionID := uuid.NewString()
	return openAICleanRelayMapping{
		AccountID:      accountID,
		Epoch:          epoch,
		InstallationID: installationID,
		SessionID:      sessionID,
		ConversationID: uuid.NewString(),
		PromptCacheKey: "clean_relay:" + sessionID,
	}
}

func openAICleanRelayInstallationID(accountID int64) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("sub2api:openai:clean_relay:account:%d", accountID))).String()
}

func openAICleanRelayCacheKey(apiKeyID, groupID int64, clientInstallationID, sessionSignal string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf(
		"api_key:%d|group:%d|installation:%s|session:%s",
		apiKeyID,
		groupID,
		strings.TrimSpace(clientInstallationID),
		strings.TrimSpace(sessionSignal),
	)))
	return openAICleanRelayCacheKeyPrefix + hex.EncodeToString(sum[:])
}

func marshalOpenAICleanRelayMapping(mapping openAICleanRelayMapping) (string, error) {
	data, err := json.Marshal(mapping)
	if err != nil {
		return "", fmt.Errorf("openai clean relay encode mapping: %w", err)
	}
	return string(data), nil
}

func openAICleanRelayClientInstallationID(c *gin.Context, reqBody map[string]any) string {
	if c != nil {
		if value := strings.TrimSpace(c.GetHeader(openAICleanRelayInstallationField)); value != "" {
			return value
		}
	}
	return strings.TrimSpace(openAICleanRelayClientMetadataString(reqBody, openAICleanRelayInstallationField))
}

func openAICleanRelayClientSessionSignal(c *gin.Context, reqBody map[string]any, bodyForSession []byte) string {
	if signal := strings.TrimSpace(explicitOpenAISessionID(c, bodyForSession)); signal != "" {
		return signal
	}
	if signal := strings.TrimSpace(openAICleanRelayBodyString(reqBody, "prompt_cache_key")); signal != "" {
		return signal
	}
	return ""
}

func applyOpenAICleanRelayMappingToBody(reqBody map[string]any, state *openAICleanRelayState) bool {
	if len(reqBody) == 0 || state == nil {
		return false
	}
	changed := false
	mapping := state.Mapping
	if strings.TrimSpace(mapping.PromptCacheKey) != "" && openAICleanRelayBodyString(reqBody, "prompt_cache_key") != mapping.PromptCacheKey {
		reqBody["prompt_cache_key"] = mapping.PromptCacheKey
		changed = true
	}
	if state.AllowBodyClientMetadata {
		if setOpenAICleanRelayClientMetadata(reqBody, mapping.InstallationID) {
			changed = true
		}
	} else if _, exists := reqBody["client_metadata"]; exists {
		delete(reqBody, "client_metadata")
		changed = true
	}
	if state.CleanStart && !state.bodyCleaned {
		if _, ok := reqBody["previous_response_id"]; ok {
			delete(reqBody, "previous_response_id")
			changed = true
		}
		if trimOpenAIEncryptedReasoningItems(reqBody) {
			changed = true
		}
		state.bodyCleaned = true
	}
	return changed
}

func (s *OpenAIGatewayService) applyOpenAICleanRelayHeaders(c *gin.Context, req *http.Request) {
	state := getOpenAICleanRelayState(c)
	if state == nil || req == nil {
		return
	}
	mapping := state.Mapping
	req.Header.Set(openAICleanRelayInstallationField, mapping.InstallationID)
	req.Header.Set("session_id", mapping.SessionID)
	req.Header.Set("conversation_id", mapping.ConversationID)
	if state.CleanStart && !state.headersCleaned {
		req.Header.Del(openAIWSTurnStateHeader)
		state.headersCleaned = true
	}
}

func applyOpenAICleanRelayWSHeaders(c *gin.Context, headers http.Header) {
	state := getOpenAICleanRelayState(c)
	if state == nil || headers == nil {
		return
	}
	mapping := state.Mapping
	headers.Set(openAICleanRelayInstallationField, mapping.InstallationID)
	headers.Set("session_id", mapping.SessionID)
	headers.Set("conversation_id", mapping.ConversationID)
	if state.CleanStart && !state.headersCleaned {
		headers.Del(openAIWSTurnStateHeader)
		state.headersCleaned = true
	}
}

func setOpenAICleanRelayState(c *gin.Context, state *openAICleanRelayState) {
	if c != nil && state != nil {
		c.Set(openAICleanRelayContextKey, state)
	}
}

func setOpenAICleanRelayGroupID(c *gin.Context, groupID *int64) {
	if c != nil && groupID != nil && *groupID > 0 {
		c.Set(openAICleanRelayGroupContextKey, *groupID)
	}
}

func isOpenAICleanRelayCompactRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	path := strings.TrimRight(strings.ToLower(strings.TrimSpace(c.Request.URL.Path)), "/")
	return strings.HasSuffix(path, "/responses/compact")
}

func getOpenAICleanRelayGroupID(c *gin.Context) int64 {
	if c == nil {
		return 0
	}
	if value, exists := c.Get(openAICleanRelayGroupContextKey); exists {
		if groupID, ok := value.(int64); ok && groupID > 0 {
			return groupID
		}
	}
	return getOpenAIGroupIDFromContext(c)
}

func getOpenAICleanRelayState(c *gin.Context) *openAICleanRelayState {
	if c == nil {
		return nil
	}
	value, exists := c.Get(openAICleanRelayContextKey)
	if !exists {
		return nil
	}
	state, _ := value.(*openAICleanRelayState)
	return state
}

func openAICleanRelayBodyString(reqBody map[string]any, key string) string {
	if len(reqBody) == 0 {
		return ""
	}
	value, ok := reqBody[key]
	if !ok {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []byte:
		return strings.TrimSpace(string(v))
	default:
		return ""
	}
}

func openAICleanRelayClientMetadataString(reqBody map[string]any, key string) string {
	if len(reqBody) == 0 {
		return ""
	}
	switch metadata := reqBody["client_metadata"].(type) {
	case map[string]any:
		if value, ok := metadata[key]; ok {
			if s, ok := value.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	case map[string]string:
		return strings.TrimSpace(metadata[key])
	}
	return ""
}

func setOpenAICleanRelayClientMetadata(reqBody map[string]any, installationID string) bool {
	installationID = strings.TrimSpace(installationID)
	if len(reqBody) == 0 || installationID == "" {
		return false
	}
	switch metadata := reqBody["client_metadata"].(type) {
	case map[string]any:
		if existing, _ := metadata[openAICleanRelayInstallationField].(string); strings.TrimSpace(existing) == installationID {
			return false
		}
		metadata[openAICleanRelayInstallationField] = installationID
		return true
	case map[string]string:
		if strings.TrimSpace(metadata[openAICleanRelayInstallationField]) == installationID {
			return false
		}
		next := make(map[string]any, len(metadata)+1)
		for k, v := range metadata {
			next[k] = v
		}
		next[openAICleanRelayInstallationField] = installationID
		reqBody["client_metadata"] = next
		return true
	default:
		reqBody["client_metadata"] = map[string]any{
			openAICleanRelayInstallationField: installationID,
		}
		return true
	}
}
