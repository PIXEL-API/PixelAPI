package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

const (
	MaxAccountCredentialImportItems         = 1000
	DefaultUserAccountCredentialImportLimit = 100
	MinUserAccountCredentialImportLimit     = 1
	MaxUserAccountCredentialImportLimit     = MaxAccountCredentialImportItems
)

const codexManagerOpenAIIssuer = "https://auth.openai.com"

func NormalizeUserAccountCredentialImportLimit(limit int) int {
	if limit <= 0 {
		return DefaultUserAccountCredentialImportLimit
	}
	if limit < MinUserAccountCredentialImportLimit {
		return MinUserAccountCredentialImportLimit
	}
	if limit > MaxUserAccountCredentialImportLimit {
		return MaxUserAccountCredentialImportLimit
	}
	return limit
}

type AccountCredentialImportKind string

const (
	AccountCredentialImportKindOAuthCredentials    AccountCredentialImportKind = "oauth_credentials"
	AccountCredentialImportKindOpenAIRefreshToken  AccountCredentialImportKind = "openai_refresh_token"
	AccountCredentialImportKindClaudeSessionKey    AccountCredentialImportKind = "claude_session_key"
	AccountCredentialImportKindOpenAIAgentIdentity AccountCredentialImportKind = "openai_agent_identity"
)

type AccountCredentialImportSource struct {
	Kind        AccountCredentialImportKind
	Name        string
	Notes       *string
	Platform    string
	Credentials map[string]any
	Extra       map[string]any
	Token       string
	ClientID    string
}

type AccountCredentialImportError struct {
	Index   int    `json:"index"`
	Kind    string `json:"kind,omitempty"`
	Name    string `json:"name,omitempty"`
	Message string `json:"message"`
}

type AccountCredentialImportResult struct {
	Total   int                            `json:"total"`
	Created int                            `json:"created"`
	Updated int                            `json:"updated"`
	Failed  int                            `json:"failed"`
	Errors  []AccountCredentialImportError `json:"errors"`
}

func ParseAccountCredentialImportContents(contents []string) ([]AccountCredentialImportSource, []AccountCredentialImportError) {
	sources := make([]AccountCredentialImportSource, 0)
	errs := make([]AccountCredentialImportError, 0)
	nextIndex := 1

	for _, content := range contents {
		items, err := parseAccountCredentialImportContent(content)
		if err != nil {
			errs = append(errs, AccountCredentialImportError{
				Index:   nextIndex,
				Message: err.Error(),
			})
			nextIndex++
			continue
		}
		for _, item := range items {
			itemSources, itemErr := accountCredentialImportSourcesFromValue(item)
			if itemErr != nil {
				errs = append(errs, AccountCredentialImportError{
					Index:   nextIndex,
					Message: itemErr.Error(),
				})
				nextIndex++
				continue
			}
			for _, source := range itemSources {
				sources = append(sources, source)
				nextIndex++
			}
		}
	}
	return sources, errs
}

// EnrichOpenAIOAuthCredentialsFromIDToken fills missing OpenAI identity fields
// from an imported ID token. Imported fields always take precedence over token
// claims, and token expiry is intentionally ignored because this data is used
// only to preserve account identity during credential import.
func EnrichOpenAIOAuthCredentialsFromIDToken(credentials map[string]any) error {
	if credentials == nil {
		return nil
	}
	idToken := importStringField(credentials, "id_token", "idToken")
	if idToken == "" {
		return nil
	}

	claims, err := openai.DecodeIDToken(idToken)
	if err != nil {
		return err
	}
	userInfo := claims.GetUserInfo()
	if userInfo == nil {
		return nil
	}

	setIfMissing := func(key, value string) {
		if value == "" {
			return
		}
		if existing, exists := credentials[key]; exists {
			existingString, isString := existing.(string)
			if !isString || existingString != "" {
				return
			}
		}
		credentials[key] = value
	}

	setIfMissing("email", userInfo.Email)
	setIfMissing("plan_type", userInfo.PlanType)
	setIfMissing("chatgpt_account_id", userInfo.ChatGPTAccountID)
	setIfMissing("chatgpt_user_id", userInfo.ChatGPTUserID)
	setIfMissing("organization_id", userInfo.OrganizationID)
	return nil
}

func BuildOpenAIAccountCredentialImportExtra(tokenInfo *OpenAITokenInfo) map[string]any {
	extra := map[string]any{}
	if tokenInfo == nil {
		return extra
	}
	if strings.TrimSpace(tokenInfo.Email) != "" {
		extra["email"] = strings.TrimSpace(tokenInfo.Email)
	}
	if strings.TrimSpace(tokenInfo.PrivacyMode) != "" {
		extra["privacy_mode"] = strings.TrimSpace(tokenInfo.PrivacyMode)
	}
	return extra
}

func BuildClaudeAccountCredentialImportExtra(tokenInfo *TokenInfo) map[string]any {
	extra := map[string]any{}
	if tokenInfo == nil {
		return extra
	}
	if strings.TrimSpace(tokenInfo.OrgUUID) != "" {
		extra["org_uuid"] = strings.TrimSpace(tokenInfo.OrgUUID)
	}
	if strings.TrimSpace(tokenInfo.AccountUUID) != "" {
		extra["account_uuid"] = strings.TrimSpace(tokenInfo.AccountUUID)
	}
	if strings.TrimSpace(tokenInfo.EmailAddress) != "" {
		extra["email_address"] = strings.TrimSpace(tokenInfo.EmailAddress)
	}
	return extra
}

func DeriveAccountCredentialImportName(platform string, credentials, extra map[string]any, sequence int) string {
	for _, source := range []map[string]any{credentials, extra} {
		if name := importStringField(source, "name", "email", "email_address"); name != "" {
			return name
		}
	}
	if platform == PlatformOpenAI && strings.EqualFold(importStringField(credentials, "auth_mode"), OpenAIAuthModeAgentIdentity) {
		return fmt.Sprintf("OpenAI Agent Identity #%d", sequence)
	}
	switch platform {
	case PlatformAnthropic:
		return fmt.Sprintf("Claude OAuth Account #%d", sequence)
	case PlatformGemini:
		return fmt.Sprintf("Gemini OAuth Account #%d", sequence)
	case PlatformAntigravity:
		return fmt.Sprintf("Antigravity OAuth Account #%d", sequence)
	case PlatformGrok:
		return fmt.Sprintf("Grok OAuth Account #%d", sequence)
	default:
		return fmt.Sprintf("OpenAI OAuth Account #%d", sequence)
	}
}

func parseAccountCredentialImportContent(content string) ([]any, error) {
	text := strings.TrimSpace(content)
	if text == "" {
		return nil, nil
	}

	if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
		return decodeAccountCredentialImportJSONValues(text)
	}

	items := make([]any, 0)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "{") || strings.HasPrefix(line, "[") {
			values, err := decodeAccountCredentialImportJSONValues(line)
			if err != nil {
				return nil, err
			}
			items = append(items, values...)
			continue
		}
		items = append(items, line)
	}
	return items, nil
}

func decodeAccountCredentialImportJSONValues(text string) ([]any, error) {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	values := make([]any, 0)
	for {
		var value any
		err := decoder.Decode(&value)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid JSON import content: %w", err)
		}
		if array, ok := value.([]any); ok {
			values = append(values, array...)
			continue
		}
		values = append(values, value)
	}
	return values, nil
}

func accountCredentialImportSourcesFromValue(value any) ([]AccountCredentialImportSource, error) {
	switch typed := value.(type) {
	case string:
		source, err := accountCredentialImportSourceFromString(typed, "", nil)
		if err != nil {
			return nil, err
		}
		return []AccountCredentialImportSource{source}, nil
	case []any:
		out := make([]AccountCredentialImportSource, 0, len(typed))
		for _, item := range typed {
			sources, err := accountCredentialImportSourcesFromValue(item)
			if err != nil {
				return nil, err
			}
			out = append(out, sources...)
		}
		return out, nil
	case map[string]any:
		if accounts, ok := importArrayField(typed, "accounts"); ok {
			out := make([]AccountCredentialImportSource, 0, len(accounts))
			for _, account := range accounts {
				sources, err := accountCredentialImportSourcesFromValue(account)
				if err != nil {
					return nil, err
				}
				out = append(out, sources...)
			}
			return out, nil
		}
		source, err := accountCredentialImportSourceFromMap(typed)
		if err != nil {
			return nil, err
		}
		return []AccountCredentialImportSource{source}, nil
	default:
		return nil, fmt.Errorf("invalid import item")
	}
}

func accountCredentialImportSourceFromMap(item map[string]any) (AccountCredentialImportSource, error) {
	if source, handled, err := accountCredentialImportSourceFromAgentIdentityAccountEnvelope(item); handled || err != nil {
		return source, err
	}
	if source, handled, err := accountCredentialImportSourceFromAgentIdentity(item); handled || err != nil {
		return source, err
	}
	if source, handled, err := accountCredentialImportSourceFromCodexManagerExport(item); handled || err != nil {
		return source, err
	}

	if field, found := findDisallowedCredentialImportField(item); found {
		return AccountCredentialImportSource{}, fmt.Errorf("disallowed credential field: %s", field)
	}

	name := credentialImportFirstNonEmptyString(
		importStringField(item, "name"),
		importStringField(item, "label"),
		importStringField(item, "email"),
	)
	notes := importOptionalStringField(item, "notes", "note", "description")
	extra := importMapField(item, "extra", "metadata")
	platform := normalizeCredentialImportPlatform(importStringField(item, "platform", "provider", "service"))

	if sessionKey := importStringField(item, "session_key", "sessionKey", "session_token", "claude_session_key", "claudeSessionKey"); sessionKey != "" {
		source, err := accountCredentialImportSourceFromString(sessionKey, name, notes)
		if err != nil {
			return AccountCredentialImportSource{}, err
		}
		return source, nil
	}

	credentials := importMapField(item, "credentials")
	if len(credentials) > 0 {
		accountType := strings.ToLower(strings.TrimSpace(importStringField(item, "type", "account_type", "accountType")))
		if accountType != "" && accountType != AccountTypeOAuth {
			return AccountCredentialImportSource{}, fmt.Errorf("credential import only supports OAuth account credentials")
		}
		if platform == "" {
			platform = inferOAuthCredentialPlatform(credentials, extra)
		}
		if platform == "" {
			return AccountCredentialImportSource{}, fmt.Errorf("account platform is required")
		}
		if accessToken := importStringField(credentials, "access_token", "accessToken"); accessToken != "" {
			credentials["access_token"] = accessToken
			return AccountCredentialImportSource{
				Kind:        AccountCredentialImportKindOAuthCredentials,
				Name:        name,
				Notes:       notes,
				Platform:    platform,
				Credentials: credentials,
				Extra:       extra,
			}, nil
		}
		if refreshToken := importStringField(credentials, "refresh_token", "refreshToken"); refreshToken != "" && platform == PlatformOpenAI {
			return AccountCredentialImportSource{
				Kind:     AccountCredentialImportKindOpenAIRefreshToken,
				Name:     name,
				Notes:    notes,
				Token:    refreshToken,
				ClientID: importStringField(credentials, "client_id", "clientId"),
			}, nil
		}
		return AccountCredentialImportSource{}, fmt.Errorf("OAuth credentials must include access_token")
	}

	if tokens := importMapField(item, "tokens", "token"); len(tokens) > 0 {
		tokenName := credentialImportFirstNonEmptyString(name, importStringField(tokens, "email", "email_address"))
		if refreshToken := importStringField(tokens, "refresh_token", "refreshToken"); refreshToken != "" && importStringField(tokens, "access_token", "accessToken") == "" {
			return AccountCredentialImportSource{
				Kind:     AccountCredentialImportKindOpenAIRefreshToken,
				Name:     tokenName,
				Notes:    notes,
				Token:    refreshToken,
				ClientID: importStringField(tokens, "client_id", "clientId"),
			}, nil
		}
		if accessToken := importStringField(tokens, "access_token", "accessToken"); accessToken != "" {
			tokens["access_token"] = accessToken
			if idToken := importStringField(tokens, "id_token", "idToken"); idToken != "" {
				tokens["id_token"] = idToken
			}
			if refreshToken := importStringField(tokens, "refresh_token", "refreshToken"); refreshToken != "" {
				tokens["refresh_token"] = refreshToken
			}
			return AccountCredentialImportSource{
				Kind:        AccountCredentialImportKindOAuthCredentials,
				Name:        tokenName,
				Notes:       notes,
				Platform:    PlatformOpenAI,
				Credentials: tokens,
				Extra:       extra,
			}, nil
		}
	}

	if refreshToken := importStringField(item, "refresh_token", "refreshToken"); refreshToken != "" {
		if platform != "" && platform != PlatformOpenAI {
			return AccountCredentialImportSource{}, fmt.Errorf("refresh-token credential import currently supports OpenAI only; use OAuth JSON for this platform")
		}
		return AccountCredentialImportSource{
			Kind:     AccountCredentialImportKindOpenAIRefreshToken,
			Name:     name,
			Notes:    notes,
			Token:    refreshToken,
			ClientID: importStringField(item, "client_id", "clientId"),
		}, nil
	}

	if accessToken := importStringField(item, "access_token", "accessToken"); accessToken != "" {
		if platform == "" {
			platform = inferOAuthCredentialPlatform(item, extra)
		}
		if platform == "" {
			platform = PlatformOpenAI
		}
		credentials := copyImportMap(item)
		credentials["access_token"] = accessToken
		return AccountCredentialImportSource{
			Kind:        AccountCredentialImportKindOAuthCredentials,
			Name:        name,
			Notes:       notes,
			Platform:    platform,
			Credentials: credentials,
			Extra:       extra,
		}, nil
	}

	if value := importStringField(item, "value", "token", "credential"); value != "" {
		return accountCredentialImportSourceFromString(value, name, notes)
	}
	return AccountCredentialImportSource{}, fmt.Errorf("unsupported credential import item")
}

func accountCredentialImportSourceFromAgentIdentityAccountEnvelope(item map[string]any) (AccountCredentialImportSource, bool, error) {
	credentialsValue, hasCredentials := importAnyField(item, "credentials")
	if !hasCredentials {
		return AccountCredentialImportSource{}, false, nil
	}
	credentials, ok := credentialsValue.(map[string]any)
	if !ok {
		return AccountCredentialImportSource{}, false, nil
	}

	authMode := importStringField(credentials, "auth_mode", "authMode")
	_, hasIdentity := importAnyField(credentials, "agent_identity", "agentIdentity")
	if !hasIdentity && !strings.EqualFold(strings.TrimSpace(authMode), OpenAIAuthModeAgentIdentity) {
		return AccountCredentialImportSource{}, false, nil
	}

	if outerAuthMode := importStringField(item, "auth_mode", "authMode"); outerAuthMode != "" {
		return AccountCredentialImportSource{}, true, fmt.Errorf("Agent Identity auth_mode must be declared only inside credentials")
	}
	if _, hasOuterIdentity := importAnyField(item, "agent_identity", "agentIdentity"); hasOuterIdentity {
		return AccountCredentialImportSource{}, true, fmt.Errorf("Agent Identity must not be declared in both the account and credentials")
	}
	declaredPlatform := importStringField(item, "platform", "provider", "service")
	if declaredPlatform != "" && normalizeCredentialImportPlatform(declaredPlatform) != PlatformOpenAI {
		return AccountCredentialImportSource{}, true, fmt.Errorf("Agent Identity account platform must be OpenAI")
	}
	accountType := strings.ToLower(strings.TrimSpace(importStringField(item, "type", "account_type", "accountType")))
	if accountType != "" && accountType != AccountTypeOAuth {
		return AccountCredentialImportSource{}, true, fmt.Errorf("Agent Identity account type must be OAuth")
	}

	outerSafety := copyImportMap(item)
	removeImportMapField(outerSafety, "credentials")
	if field, found := findOAuthTokenCredentialImportField(outerSafety); found {
		return AccountCredentialImportSource{}, true, fmt.Errorf("Agent Identity must not include OAuth token field: %s", field)
	}
	if field, found := findDisallowedCredentialImportField(outerSafety); found {
		return AccountCredentialImportSource{}, true, fmt.Errorf("disallowed credential field: %s", field)
	}

	// Legacy account exports can contain an OAuth id_token alongside Agent
	// Identity credentials. Agent Identity authentication does not use it, so
	// discard only this direct envelope field before the strict recursive token
	// scan. Access/refresh tokens and every nested token field remain rejected.
	identityInput := copyImportMap(credentials)
	if idTokenValue, hasIDToken := importAnyField(identityInput, "id_token", "idToken"); hasIDToken {
		if _, valid := idTokenValue.(string); !valid {
			return AccountCredentialImportSource{}, true, fmt.Errorf("Agent Identity account id_token must be a string")
		}
		removeImportMapField(identityInput, "id_token")
		removeImportMapField(identityInput, "idToken")
	}

	source, handled, err := accountCredentialImportSourceFromAgentIdentity(identityInput)
	if err != nil {
		return AccountCredentialImportSource{}, true, err
	}
	if !handled {
		return AccountCredentialImportSource{}, true, fmt.Errorf("Agent Identity credentials are invalid")
	}

	source.Name = credentialImportFirstNonEmptyString(
		importStringField(item, "name", "label"),
		source.Name,
	)
	source.Notes = importOptionalStringField(item, "notes", "note", "description")
	source.Extra = importMapField(item, "extra", "metadata")
	return source, true, nil
}

func accountCredentialImportSourceFromAgentIdentity(item map[string]any) (AccountCredentialImportSource, bool, error) {
	authMode := importStringField(item, "auth_mode", "authMode")
	identityValue, hasIdentity := importAnyField(item, "agent_identity", "agentIdentity")
	isAgentAuthMode := strings.EqualFold(strings.TrimSpace(authMode), OpenAIAuthModeAgentIdentity)
	if !hasIdentity && !isAgentAuthMode {
		return AccountCredentialImportSource{}, false, nil
	}
	if authMode != "" && !isAgentAuthMode {
		return AccountCredentialImportSource{}, true, fmt.Errorf("Agent Identity auth_mode is invalid")
	}
	if field, found := findOAuthTokenCredentialImportField(item); found {
		return AccountCredentialImportSource{}, true, fmt.Errorf("Agent Identity must not include OAuth token field: %s", field)
	}

	identity := item
	if hasIdentity {
		var ok bool
		identity, ok = identityValue.(map[string]any)
		if !ok {
			return AccountCredentialImportSource{}, true, fmt.Errorf("agent_identity must be an object")
		}
	}

	runtimeID := importStringField(identity, "agent_runtime_id", "agentRuntimeId")
	privateKey := importStringField(identity, "agent_private_key", "agentPrivateKey")
	if runtimeID == "" || privateKey == "" {
		return AccountCredentialImportSource{}, true, fmt.Errorf("Agent Identity requires agent_runtime_id and agent_private_key")
	}
	if err := ValidateOpenAIAgentIdentityPrivateKey(privateKey); err != nil {
		return AccountCredentialImportSource{}, true, err
	}

	// Detection and key validation must happen before the generic credential
	// safety scan. Remove only the recognized auth discriminator from a copy;
	// all unrelated fields, including nested ones, remain subject to the scan.
	safetyItem := copyImportMap(item)
	removeImportMapField(safetyItem, "auth_mode")
	removeImportMapField(safetyItem, "authMode")
	if hasIdentity {
		safetyIdentity := copyImportMap(identity)
		removeImportMapField(safetyIdentity, "auth_mode")
		removeImportMapField(safetyIdentity, "authMode")
		removeImportMapField(safetyItem, "agent_identity")
		removeImportMapField(safetyItem, "agentIdentity")
		safetyItem["agent_identity"] = safetyIdentity
	}
	if field, found := findDisallowedCredentialImportField(safetyItem); found {
		return AccountCredentialImportSource{}, true, fmt.Errorf("disallowed credential field: %s", field)
	}

	credentials := map[string]any{
		"auth_mode":         OpenAIAuthModeAgentIdentity,
		"agent_runtime_id":  runtimeID,
		"agent_private_key": privateKey,
	}
	for _, field := range []struct {
		target string
		keys   []string
	}{
		{target: "task_id", keys: []string{"task_id", "taskId"}},
		{target: "chatgpt_account_id", keys: []string{"chatgpt_account_id", "chatgptAccountId", "account_id", "accountId"}},
		{target: "chatgpt_user_id", keys: []string{"chatgpt_user_id", "chatgptUserId"}},
		{target: "email", keys: []string{"email"}},
		{target: "plan_type", keys: []string{"plan_type", "planType"}},
	} {
		if value := importStringField(identity, field.keys...); value != "" {
			credentials[field.target] = value
		}
	}
	if value, ok := importAnyField(identity, "chatgpt_account_is_fedramp", "chatgptAccountIsFedramp"); ok {
		fedRAMP, valid := value.(bool)
		if !valid {
			return AccountCredentialImportSource{}, true, fmt.Errorf("chatgpt_account_is_fedramp must be a boolean")
		}
		credentials["chatgpt_account_is_fedramp"] = fedRAMP
	}

	name := credentialImportFirstNonEmptyString(
		importStringField(item, "name", "label"),
		importStringField(identity, "name", "label", "email"),
	)
	return AccountCredentialImportSource{
		Kind:        AccountCredentialImportKindOpenAIAgentIdentity,
		Name:        name,
		Notes:       importOptionalStringField(item, "notes", "note", "description"),
		Platform:    PlatformOpenAI,
		Credentials: credentials,
		Extra:       importMapField(item, "extra", "metadata"),
	}, true, nil
}

func accountCredentialImportSourceFromCodexManagerExport(item map[string]any) (AccountCredentialImportSource, bool, error) {
	tokens := importMapField(item, "tokens")
	meta := importMapField(item, "meta")
	if len(tokens) == 0 || len(meta) == 0 {
		return AccountCredentialImportSource{}, false, nil
	}

	topLevel := copyImportMap(item)
	removeImportMapField(topLevel, "tokens")
	removeImportMapField(topLevel, "meta")
	if field, found := findDisallowedCredentialImportField(topLevel); found {
		return AccountCredentialImportSource{}, true, fmt.Errorf("disallowed credential field: %s", field)
	}
	if field, found := findDisallowedCredentialImportField(tokens); found {
		return AccountCredentialImportSource{}, true, fmt.Errorf("disallowed credential field: %s", field)
	}
	metaForSafety := copyImportMap(meta)
	removeImportMapField(metaForSafety, "issuer")
	if field, found := findDisallowedCredentialImportField(metaForSafety); found {
		return AccountCredentialImportSource{}, true, fmt.Errorf("disallowed credential field: %s", field)
	}

	issuer := strings.TrimRight(strings.TrimSpace(importStringField(meta, "issuer")), "/")
	if issuer != "" && !strings.EqualFold(issuer, codexManagerOpenAIIssuer) {
		return AccountCredentialImportSource{}, true, fmt.Errorf("unsupported Codex-Manager issuer: %s", issuer)
	}

	accessToken := importStringField(tokens, "access_token", "accessToken")
	if accessToken == "" {
		return AccountCredentialImportSource{}, true, fmt.Errorf("OAuth credentials must include access_token")
	}

	credentials := map[string]any{
		"access_token": accessToken,
	}
	if idToken := importStringField(tokens, "id_token", "idToken"); idToken != "" {
		credentials["id_token"] = idToken
	}
	if refreshToken := importStringField(tokens, "refresh_token", "refreshToken"); refreshToken != "" {
		credentials["refresh_token"] = refreshToken
	}
	if chatgptAccountID := importStringField(meta, "chatgpt_account_id", "chatgptAccountId"); chatgptAccountID != "" {
		credentials["chatgpt_account_id"] = chatgptAccountID
	}
	if workspaceID := importStringField(meta, "workspace_id", "workspaceId"); workspaceID != "" {
		credentials["workspace_id"] = workspaceID
	}

	notes := importOptionalStringField(meta, "note", "notes", "description")
	if notes == nil {
		notes = importOptionalStringField(item, "note", "notes", "description")
	}

	return AccountCredentialImportSource{
		Kind:        AccountCredentialImportKindOAuthCredentials,
		Name:        credentialImportFirstNonEmptyString(importStringField(meta, "label", "name"), importStringField(item, "name", "label")),
		Notes:       notes,
		Platform:    PlatformOpenAI,
		Credentials: credentials,
		Extra:       map[string]any{},
	}, true, nil
}

func accountCredentialImportSourceFromString(value, name string, notes *string) (AccountCredentialImportSource, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return AccountCredentialImportSource{}, fmt.Errorf("empty credential")
	}
	if sessionKey, ok := extractClaudeSessionKey(text); ok {
		return AccountCredentialImportSource{
			Kind:  AccountCredentialImportKindClaudeSessionKey,
			Name:  name,
			Notes: notes,
			Token: sessionKey,
		}, nil
	}
	if reason, blocked := disallowedRawCredentialReason(text); blocked {
		return AccountCredentialImportSource{}, fmt.Errorf("disallowed credential content: %s", reason)
	}
	return AccountCredentialImportSource{
		Kind:  AccountCredentialImportKindOpenAIRefreshToken,
		Name:  name,
		Notes: notes,
		Token: text,
	}, nil
}

func extractClaudeSessionKey(value string) (string, bool) {
	text := strings.TrimSpace(strings.Trim(value, `"'`))
	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "sk-ant-sid") {
		return text, true
	}
	for _, prefix := range []string{"sessionkey=", "session_key=", "claude_session_key="} {
		idx := strings.Index(lower, prefix)
		if idx < 0 {
			continue
		}
		candidate := strings.TrimSpace(text[idx+len(prefix):])
		if cut := strings.IndexAny(candidate, ";\r\n\t "); cut >= 0 {
			candidate = candidate[:cut]
		}
		candidate = strings.Trim(candidate, `"'`)
		if strings.HasPrefix(strings.ToLower(candidate), "sk-ant-sid") {
			return candidate, true
		}
	}
	return "", false
}

func disallowedRawCredentialReason(value string) (string, bool) {
	return disallowedCredentialStringReason("", value, credentialSafetyOptions{})
}

func findDisallowedCredentialImportField(value any) (string, bool) {
	return findDisallowedCredentialContent(value, credentialSafetyOptions{
		AllowClaudeSessionKeyFields: true,
		AllowOAuthTokenValues:       true,
		AllowOAuthMetadataURLs:      true,
	})
}

func findOAuthTokenCredentialImportField(value any) (string, bool) {
	return findOAuthTokenCredentialContent(value)
}

func importMapField(values map[string]any, keys ...string) map[string]any {
	value, ok := importAnyField(values, keys...)
	if !ok {
		return nil
	}
	if typed, ok := value.(map[string]any); ok {
		return copyImportMap(typed)
	}
	return nil
}

func importArrayField(values map[string]any, keys ...string) ([]any, bool) {
	value, ok := importAnyField(values, keys...)
	if !ok {
		return nil, false
	}
	typed, ok := value.([]any)
	return typed, ok
}

func importStringField(values map[string]any, keys ...string) string {
	value, ok := importAnyField(values, keys...)
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return strings.TrimSpace(typed.String())
	default:
		return ""
	}
}

func importOptionalStringField(values map[string]any, keys ...string) *string {
	value := importStringField(values, keys...)
	if value == "" {
		return nil
	}
	return &value
}

func importAnyField(values map[string]any, keys ...string) (any, bool) {
	if len(values) == 0 {
		return nil, false
	}
	for _, key := range keys {
		normalizedTarget := normalizeCredentialImportKey(key)
		for existingKey, value := range values {
			if normalizeCredentialImportKey(existingKey) == normalizedTarget {
				return value, true
			}
		}
	}
	return nil, false
}

func copyImportMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func removeImportMapField(values map[string]any, key string) {
	normalizedTarget := normalizeCredentialImportKey(key)
	for existingKey := range values {
		if normalizeCredentialImportKey(existingKey) == normalizedTarget {
			delete(values, existingKey)
		}
	}
}

func normalizeCredentialImportKey(key string) string {
	return strings.NewReplacer("-", "_", ".", "_").Replace(strings.ToLower(strings.TrimSpace(key)))
}

func normalizeCredentialImportPlatform(platform string) string {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "anthropic", "claude":
		return PlatformAnthropic
	case "openai", "chatgpt", "codex":
		return PlatformOpenAI
	case "gemini", "google":
		return PlatformGemini
	case "antigravity":
		return PlatformAntigravity
	case "grok", "xai", "x.ai":
		return PlatformGrok
	default:
		return ""
	}
}

func inferOAuthCredentialPlatform(credentials, extra map[string]any) string {
	if importStringField(credentials, "org_uuid", "account_uuid", "email_address") != "" ||
		importStringField(extra, "org_uuid", "account_uuid", "email_address") != "" {
		return PlatformAnthropic
	}
	if importStringField(credentials, "project_id", "oauth_type", "tier_id") != "" {
		return PlatformGemini
	}
	if importStringField(credentials, "chatgpt_account_id", "chatgpt_user_id", "organization_id", "id_token") != "" {
		return PlatformOpenAI
	}
	return ""
}

func credentialImportFirstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
