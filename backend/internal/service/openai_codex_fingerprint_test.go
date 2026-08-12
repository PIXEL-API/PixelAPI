//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func newCodexFingerprintTestAccount(id int64, mode string) *Account {
	extra := map[string]any{}
	if mode != "" {
		extra[codexFingerprintModeExtraKey] = mode
	}
	return &Account{ID: id, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: extra}
}

func TestGetCodexFingerprintMode(t *testing.T) {
	tests := []struct {
		name    string
		account *Account
		want    codexFingerprintMode
	}{
		{name: "nil", want: codexFingerprintOff},
		{name: "api key", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, want: codexFingerprintOff},
		{name: "non openai", account: &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth}, want: codexFingerprintOff},
		{name: "missing defaults to session", account: newCodexFingerprintTestAccount(1, ""), want: codexFingerprintSession},
		{name: "invalid defaults to session", account: newCodexFingerprintTestAccount(1, "invalid"), want: codexFingerprintSession},
		{name: "explicit off", account: newCodexFingerprintTestAccount(1, "off"), want: codexFingerprintOff},
		{name: "device", account: newCodexFingerprintTestAccount(1, "device"), want: codexFingerprintDevice},
		{name: "session", account: newCodexFingerprintTestAccount(1, "session"), want: codexFingerprintSession},
		{name: "full", account: newCodexFingerprintTestAccount(1, "full"), want: codexFingerprintFull},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, test.account.GetCodexFingerprintMode())
		})
	}
}

func TestDeriveStableUUIDv4(t *testing.T) {
	first := deriveStableUUIDv4("seed")
	assert.Equal(t, first, deriveStableUUIDv4("seed"))
	assert.NotEqual(t, first, deriveStableUUIDv4("another-seed"))
	parsed, err := uuid.Parse(first)
	require.NoError(t, err)
	assert.Equal(t, uuid.Version(4), parsed.Version())
}

func TestResolveCodexFingerprintIDsFromRequest(t *testing.T) {
	headers := http.Header{"Session-Id": []string{"client-session"}}
	ids := resolveCodexFingerprintIDsFromRequest(newCodexFingerprintTestAccount(17, "session"), headers)
	require.NotNil(t, ids)
	assert.Equal(t, resolveConvergedThreadID(newCodexFingerprintTestAccount(17, "session"), "client-session"), ids.threadID)
	assert.NotEmpty(t, ids.turnID)
	assert.Nil(t, resolveCodexFingerprintIDsFromRequest(newCodexFingerprintTestAccount(17, "off"), headers))
	assert.Nil(t, resolveCodexFingerprintIDsFromRequest(&Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, headers))
}

func TestExtractClientSessionIDPrefersHyphenatedHeader(t *testing.T) {
	headers := http.Header{
		"Session-Id": []string{"hyphenated-session"},
		"Session_id": []string{"underscored-session"},
	}
	assert.Equal(t, "hyphenated-session", extractClientSessionID(headers))
}

func TestCodexFingerprintStableIDsAreAccountScoped(t *testing.T) {
	first := resolveCodexFingerprintIDsFromRequest(
		newCodexFingerprintTestAccount(17, "session"),
		http.Header{"Session-Id": []string{"client-session"}},
	)
	second := resolveCodexFingerprintIDsFromRequest(
		newCodexFingerprintTestAccount(18, "session"),
		http.Header{"Session-Id": []string{"client-session"}},
	)
	require.NotNil(t, first)
	require.NotNil(t, second)
	assert.NotEqual(t, first.installationID, second.installationID)
	assert.NotEqual(t, first.sessionID, second.sessionID)
	assert.NotEqual(t, first.threadID, second.threadID)
}

func TestCodexFingerprintSessionAndFullThreadSemantics(t *testing.T) {
	account := newCodexFingerprintTestAccount(21, "session")
	first := resolveCodexFingerprintIDsFromRequest(account, http.Header{"Session-Id": []string{"client-a"}})
	second := resolveCodexFingerprintIDsFromRequest(account, http.Header{"Session-Id": []string{"client-b"}})
	require.NotNil(t, first)
	require.NotNil(t, second)
	assert.Equal(t, first.sessionID, second.sessionID)
	assert.NotEqual(t, first.threadID, second.threadID)

	fullAccount := newCodexFingerprintTestAccount(21, "full")
	fullFirst := resolveCodexFingerprintIDsFromRequest(fullAccount, http.Header{"Session-Id": []string{"client-a"}})
	fullSecond := resolveCodexFingerprintIDsFromRequest(fullAccount, http.Header{"Session-Id": []string{"client-b"}})
	require.NotNil(t, fullFirst)
	require.NotNil(t, fullSecond)
	assert.Equal(t, fullFirst.sessionID, fullFirst.threadID)
	assert.Equal(t, fullFirst.threadID, fullSecond.threadID)
}

func TestCodexFingerprintPolicyMatrix(t *testing.T) {
	turnMetadata := `{"installation_id":"client-install","session_id":"client-session","thread_id":"client-thread","turn_id":"client-turn","window_id":"client-window"}`
	tests := []struct {
		name                 string
		mode                 string
		cleanRelay           bool
		wantInstallation     string
		wantSession          string
		wantThreadConverged  bool
		wantBodyThread       bool
		wantBodyInstallation string
	}{
		{name: "off relay off", mode: "off", wantInstallation: "client-install", wantSession: "client-session", wantBodyInstallation: "client-install"},
		{name: "device relay off", mode: "device", wantInstallation: "device-account", wantSession: "client-session", wantBodyInstallation: "device-account"},
		{name: "session relay off", mode: "session", wantInstallation: "device-account", wantSession: "account-session", wantThreadConverged: true, wantBodyThread: true, wantBodyInstallation: "device-account"},
		{name: "full relay off", mode: "full", wantInstallation: "device-account", wantSession: "account-session", wantThreadConverged: true, wantBodyThread: true, wantBodyInstallation: "device-account"},
		{name: "off relay on", mode: "off", cleanRelay: true, wantInstallation: "client-install", wantSession: "client-session", wantBodyInstallation: "client-install"},
		{name: "device relay on", mode: "device", cleanRelay: true, wantInstallation: "client-install", wantSession: "client-session", wantBodyInstallation: "client-install"},
		{name: "session relay on", mode: "session", cleanRelay: true, wantInstallation: "client-install", wantSession: "client-session", wantThreadConverged: true, wantBodyThread: true, wantBodyInstallation: "client-install"},
		{name: "full relay on", mode: "full", cleanRelay: true, wantInstallation: "client-install", wantSession: "client-session", wantThreadConverged: true, wantBodyThread: true, wantBodyInstallation: "client-install"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			account := newCodexFingerprintTestAccount(8, test.mode)
			account.Extra["openai_device_id"] = "device-account"
			ids := resolveCodexFingerprintIDsFromRequest(account, http.Header{"Session-Id": []string{"client-session"}})
			headers := http.Header{
				"X-Codex-Installation-Id": []string{"client-install"},
				"Session-Id":              []string{"client-session"},
				"X-Codex-Turn-Metadata":   []string{turnMetadata},
			}
			body := map[string]any{"client_metadata": map[string]any{
				openAICleanRelayInstallationField: "client-install",
				"session_id":                      "client-session",
				"x-codex-turn-metadata":           turnMetadata,
			}}
			policy := codexFingerprintApplyPolicy{cleanRelayAuthoritative: test.cleanRelay}
			applyCodexFingerprintHeaders(headers, ids, policy)
			applyCodexFingerprintClientMetadata(body, ids, policy)

			assert.Equal(t, test.wantInstallation, headers.Get(openAICleanRelayInstallationField))
			if !test.cleanRelay && (test.mode == "session" || test.mode == "full") {
				assert.Equal(t, ids.sessionID, headers.Get("session_id"))
			} else {
				assert.Equal(t, test.wantSession, headers.Get("session-id"))
			}
			if test.wantThreadConverged {
				assert.Equal(t, ids.threadID, headers.Get("thread-id"))
			} else {
				assert.Empty(t, headers.Get("thread-id"))
			}
			metadata := body["client_metadata"].(map[string]any)
			assert.Equal(t, test.wantBodyInstallation, metadata[openAICleanRelayInstallationField])
			if test.wantBodyThread {
				assert.Equal(t, ids.threadID, metadata["thread_id"])
			} else {
				_, exists := metadata["thread_id"]
				assert.False(t, exists)
			}
		})
	}
}

func TestFingerprintIDsHeaderAndBodyTurnConsistent(t *testing.T) {
	account := newCodexFingerprintTestAccount(5, "session")
	ids := resolveCodexFingerprintIDsFromRequest(account, http.Header{"Session-Id": []string{"client-session"}})
	template := `{"installation_id":"x","session_id":"x","thread_id":"x","turn_id":"x","window_id":"x"}`
	headers := http.Header{"X-Codex-Turn-Metadata": []string{template}}
	body := map[string]any{"client_metadata": map[string]any{"x-codex-turn-metadata": template}}
	applyCodexFingerprintHeaders(headers, ids, codexFingerprintApplyPolicy{})
	applyCodexFingerprintClientMetadata(body, ids, codexFingerprintApplyPolicy{})

	var headerMetadata map[string]any
	require.NoError(t, json.Unmarshal([]byte(headers.Get("x-codex-turn-metadata")), &headerMetadata))
	clientMetadata := body["client_metadata"].(map[string]any)
	var bodyMetadata map[string]any
	require.NoError(t, json.Unmarshal([]byte(clientMetadata["x-codex-turn-metadata"].(string)), &bodyMetadata))
	assert.Equal(t, ids.turnID, headerMetadata["turn_id"])
	assert.Equal(t, ids.turnID, clientMetadata["turn_id"])
	assert.Equal(t, ids.turnID, bodyMetadata["turn_id"])
}

func TestCleanRelayOwnsInstallationAndSessionEverywhere(t *testing.T) {
	account := newCodexFingerprintTestAccount(5, "session")
	ids := resolveCodexFingerprintIDsFromRequest(account, http.Header{"Session-Id": []string{"client-session"}})
	turnMetadata := `{"installation_id":"client-install","session_id":"client-session","thread_id":"client-thread","turn_id":"client-turn","window_id":"client-window"}`
	headers := http.Header{"X-Codex-Turn-Metadata": []string{turnMetadata}}
	body := map[string]any{"client_metadata": map[string]any{"x-codex-turn-metadata": turnMetadata}}
	policy := codexFingerprintApplyPolicy{cleanRelayAuthoritative: true}
	applyCodexFingerprintHeaders(headers, ids, policy)
	applyCodexFingerprintClientMetadata(body, ids, policy)

	state := &openAICleanRelayState{Mapping: openAICleanRelayMapping{
		AccountID:      account.ID,
		InstallationID: "relay-install",
		SessionID:      "relay-session",
		ConversationID: "relay-conversation",
		PromptCacheKey: "relay-cache",
	}, AllowBodyClientMetadata: true}
	applyOpenAICleanRelayMappingToBody(body, state)
	req, err := http.NewRequest(http.MethodPost, "https://example.test", nil)
	require.NoError(t, err)
	req.Header = headers
	c := newCleanRelayGinContext(1, 1)
	setOpenAICleanRelayState(c, state)
	svc := &OpenAIGatewayService{settingService: newCleanRelaySettingService(true)}
	defer func() { svc.settingService = newCleanRelaySettingService(false) }()
	svc.applyOpenAICleanRelayHeaders(context.Background(), c, account, req)

	clientMetadata := body["client_metadata"].(map[string]any)
	assert.Equal(t, "relay-install", clientMetadata[openAICleanRelayInstallationField])
	assert.Equal(t, "relay-session", clientMetadata["session_id"])
	assert.Equal(t, ids.threadID, clientMetadata["thread_id"])
	var bodyTurn map[string]any
	require.NoError(t, json.Unmarshal([]byte(clientMetadata["x-codex-turn-metadata"].(string)), &bodyTurn))
	assert.Equal(t, "relay-install", bodyTurn["installation_id"])
	assert.Equal(t, "relay-session", bodyTurn["session_id"])
	assert.Equal(t, ids.threadID, bodyTurn["thread_id"])
	assert.Equal(t, "relay-install", req.Header.Get(openAICleanRelayInstallationField))
	assert.Equal(t, "relay-session", req.Header.Get("session-id"))
	assert.Equal(t, "relay-session", req.Header.Get("session_id"))
	assert.Equal(t, "relay-conversation", req.Header.Get("conversation_id"))
	var headerTurn map[string]any
	require.NoError(t, json.Unmarshal([]byte(req.Header.Get("x-codex-turn-metadata")), &headerTurn))
	assert.Equal(t, "relay-install", headerTurn["installation_id"])
	assert.Equal(t, "relay-session", headerTurn["session_id"])
	assert.Equal(t, ids.threadID, headerTurn["thread_id"])
}

func TestResolveCodexFingerprintIDsForTurnKeepsStableIDsAndRotatesTurn(t *testing.T) {
	c := newCleanRelayGinContext(1, 1)
	c.Request.Header.Set("session-id", "client-session")
	account := newCodexFingerprintTestAccount(9, "session")
	first := resolveCodexFingerprintIDsForTurn(c, account)
	second := resolveCodexFingerprintIDsForTurn(c, account)
	require.NotNil(t, first)
	require.NotNil(t, second)
	assert.Equal(t, first.sessionID, second.sessionID)
	assert.Equal(t, first.threadID, second.threadID)
	assert.NotEqual(t, first.turnID, second.turnID)
	assert.Nil(t, getCurrentCodexFingerprintIDs(c))
}

func TestCodexFingerprintCurrentAttemptSwitchesAccountsWithoutLeakingIDs(t *testing.T) {
	c := newCleanRelayGinContext(1, 1)
	c.Request.Header.Set("session-id", "client-session")
	service := &OpenAIGatewayService{}

	firstAccount := newCodexFingerprintTestAccount(41, "session")
	firstBody := map[string]any{}
	firstIDs, _ := service.applyCodexFingerprintToRequestBody(context.Background(), c, firstAccount, firstBody)
	require.NotNil(t, firstIDs)
	firstHeaders := make(http.Header)
	require.True(t, service.applyCurrentCodexFingerprintHeaders(context.Background(), c, firstAccount, firstHeaders))
	assert.Equal(t, firstIDs.installationID, firstHeaders.Get(openAICleanRelayInstallationField))
	assert.Equal(t, firstIDs.threadID, firstHeaders.Get("thread-id"))

	secondAccount := newCodexFingerprintTestAccount(42, "session")
	secondBody := map[string]any{}
	secondIDs, _ := service.applyCodexFingerprintToRequestBody(context.Background(), c, secondAccount, secondBody)
	require.NotNil(t, secondIDs)
	secondHeaders := make(http.Header)
	require.True(t, service.applyCurrentCodexFingerprintHeaders(context.Background(), c, secondAccount, secondHeaders))

	assert.NotEqual(t, firstIDs.installationID, secondIDs.installationID)
	assert.NotEqual(t, firstIDs.sessionID, secondIDs.sessionID)
	assert.NotEqual(t, firstIDs.threadID, secondIDs.threadID)
	assert.NotEqual(t, firstIDs.turnID, secondIDs.turnID)
	assert.Equal(t, secondIDs.installationID, secondHeaders.Get(openAICleanRelayInstallationField))
	assert.Equal(t, secondIDs.sessionID, secondHeaders.Get("session-id"))
	assert.Equal(t, secondIDs.threadID, secondHeaders.Get("thread-id"))
	assert.Equal(t, secondIDs.turnID, secondBody["client_metadata"].(map[string]any)["turn_id"])
}

func TestCodexFingerprintOffAttemptClearsPreviousAccountIDs(t *testing.T) {
	c := newCleanRelayGinContext(1, 1)
	c.Request.Header.Set("session-id", "client-session")
	service := &OpenAIGatewayService{}

	_, _ = service.applyCodexFingerprintToRequestBody(
		context.Background(),
		c,
		newCodexFingerprintTestAccount(51, "session"),
		map[string]any{},
	)
	require.NotNil(t, getCurrentCodexFingerprintIDs(c))

	clientBody := map[string]any{"client_metadata": map[string]any{"session_id": "client-session"}}
	ids, modified := service.applyCodexFingerprintToRequestBody(
		context.Background(),
		c,
		newCodexFingerprintTestAccount(52, "off"),
		clientBody,
	)
	assert.Nil(t, ids)
	assert.False(t, modified)
	assert.Nil(t, getCurrentCodexFingerprintIDs(c))
	assert.False(t, service.applyCurrentCodexFingerprintHeaders(
		context.Background(),
		c,
		newCodexFingerprintTestAccount(52, "off"),
		make(http.Header),
	))
	assert.Equal(t, "client-session", clientBody["client_metadata"].(map[string]any)["session_id"])
}

func TestCodexFingerprintHeadersRejectStaleAccountIDs(t *testing.T) {
	c := newCleanRelayGinContext(1, 1)
	c.Request.Header.Set("session-id", "client-session")
	service := &OpenAIGatewayService{}
	firstAccount := newCodexFingerprintTestAccount(53, "session")
	secondAccount := newCodexFingerprintTestAccount(54, "session")

	ids, _ := service.applyCodexFingerprintToRequestBody(context.Background(), c, firstAccount, map[string]any{})
	require.NotNil(t, ids)
	headers := make(http.Header)

	assert.False(t, service.applyCurrentCodexFingerprintHeaders(context.Background(), c, secondAccount, headers))
	assert.Nil(t, getCurrentCodexFingerprintIDs(c))
	assert.Empty(t, headers.Get(openAICleanRelayInstallationField))
	assert.Empty(t, headers.Get("session-id"))
	assert.Empty(t, headers.Get("thread-id"))
}

func TestCodexFingerprintHeadersRejectStaleIDsAfterModeChangesToOff(t *testing.T) {
	c := newCleanRelayGinContext(1, 1)
	c.Request.Header.Set("session-id", "client-session")
	service := &OpenAIGatewayService{}
	account := newCodexFingerprintTestAccount(55, "session")

	ids, _ := service.applyCodexFingerprintToRequestBody(context.Background(), c, account, map[string]any{})
	require.NotNil(t, ids)
	account.Extra[codexFingerprintModeExtraKey] = "off"
	headers := make(http.Header)

	assert.False(t, service.applyCurrentCodexFingerprintHeaders(context.Background(), c, account, headers))
	assert.Nil(t, getCurrentCodexFingerprintIDs(c))
	assert.Empty(t, headers.Get(openAICleanRelayInstallationField))
	assert.Empty(t, headers.Get("session-id"))
}

func TestApplyCodexFingerprintRawClientMetadataPreservesLargeIntegers(t *testing.T) {
	account := newCodexFingerprintTestAccount(61, "session")
	ids := resolveCodexFingerprintIDsFromRequest(account, http.Header{"Session-Id": []string{"client-session"}})
	require.NotNil(t, ids)

	original := []byte(`{"sequence":9007199254740993123,"client_metadata":{"sequence":9007199254740993123,"session_id":"client-session","x-codex-turn-metadata":"{\"sequence\":9007199254740993123}"}}`)
	rewritten, changed, err := applyCodexFingerprintRawClientMetadata(original, ids, codexFingerprintApplyPolicy{})
	require.NoError(t, err)
	require.True(t, changed)
	assert.Contains(t, string(rewritten), `"sequence":9007199254740993123`)
	assert.Equal(t, "9007199254740993123", gjson.GetBytes(rewritten, "client_metadata.sequence").Raw)
	assert.Equal(t, ids.sessionID, gjson.GetBytes(rewritten, "client_metadata.session_id").String())
	embedded := gjson.GetBytes(rewritten, "client_metadata.x-codex-turn-metadata").String()
	assert.Equal(t, "9007199254740993123", gjson.Get(embedded, "sequence").Raw)
}

func TestApplyCodexFingerprintHeadersPreservesLargeTurnMetadataInteger(t *testing.T) {
	ids := resolveCodexFingerprintIDsFromRequest(
		newCodexFingerprintTestAccount(62, "session"),
		http.Header{"Session-Id": []string{"client-session"}},
	)
	require.NotNil(t, ids)
	headers := http.Header{
		"X-Codex-Turn-Metadata": []string{`{"sequence":9007199254740993123,"session_id":"client-session"}`},
	}

	require.True(t, applyCodexFingerprintHeaders(headers, ids, codexFingerprintApplyPolicy{}))
	assert.Equal(t, "9007199254740993123", gjson.Get(headers.Get("x-codex-turn-metadata"), "sequence").Raw)
	assert.Equal(t, ids.sessionID, gjson.Get(headers.Get("x-codex-turn-metadata"), "session_id").String())
}

func TestBuildOpenAIWSHeadersUsesCurrentCodexFingerprintIDs(t *testing.T) {
	c := newCleanRelayGinContext(1, 1)
	c.Request.Header.Set("session-id", "client-session")
	c.Request.Header.Set("thread-id", "client-thread")
	c.Request.Header.Set(openAICleanRelayInstallationField, "client-installation")
	account := newCodexFingerprintTestAccount(71, "session")
	service := &OpenAIGatewayService{}

	body := map[string]any{}
	ids, _ := service.applyCodexFingerprintToRequestBody(context.Background(), c, account, body)
	require.NotNil(t, ids)
	headers, _ := service.buildOpenAIWSHeaders(
		c,
		account,
		"test-token",
		OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2},
		true,
		"",
		"",
		"",
	)

	assert.Equal(t, ids.installationID, headers.Get(openAICleanRelayInstallationField))
	assert.Equal(t, ids.sessionID, headers.Get("session-id"))
	assert.Equal(t, ids.threadID, headers.Get("thread-id"))
	assert.Equal(t, ids.threadID, headers.Get("x-client-request-id"))
	assert.Equal(t, ids.windowID, headers.Get("x-codex-window-id"))
}

func TestBuildOpenAIWSHeadersOffModePassesThroughClientFingerprint(t *testing.T) {
	c := newCleanRelayGinContext(1, 1)
	c.Request.Header.Set("session-id", "client-session")
	c.Request.Header.Set("session_id", "client-underscore-session")
	c.Request.Header.Set("conversation_id", "client-conversation")
	c.Request.Header.Set("thread-id", "client-thread")
	c.Request.Header.Set("x-client-request-id", "client-request")
	c.Request.Header.Set(openAICleanRelayInstallationField, "client-installation")
	c.Request.Header.Set("x-codex-window-id", "client-window")
	account := newCodexFingerprintTestAccount(72, "off")
	service := &OpenAIGatewayService{}
	setCurrentCodexFingerprintIDs(c, nil)

	headers, _ := service.buildOpenAIWSHeaders(
		c,
		account,
		"test-token",
		OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2},
		true,
		"",
		"",
		"",
	)

	assert.Equal(t, "client-installation", headers.Get(openAICleanRelayInstallationField))
	assert.Equal(t, "client-session", headers.Get("session-id"))
	assert.Equal(t, isolateOpenAISessionID(1, "client-underscore-session"), headers.Get("session_id"))
	assert.Equal(t, isolateOpenAISessionID(1, "client-conversation"), headers.Get("conversation_id"))
	assert.Equal(t, "client-thread", headers.Get("thread-id"))
	assert.Equal(t, "client-request", headers.Get("x-client-request-id"))
	assert.Equal(t, "client-window", headers.Get("x-codex-window-id"))
}
