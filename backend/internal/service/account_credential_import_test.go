package service

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func testAgentIdentityPrivateKey(t *testing.T) string {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate Ed25519 key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal PKCS#8 key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(der)
}

func testOpenAIImportIDToken(t *testing.T, chatGPTUserID string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"email": chatGPTUserID + "@school.example",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "school-workspace",
			"chatgpt_user_id":    chatGPTUserID,
			"chatgpt_plan_type":  "chatgpt-k12",
			"organizations": []map[string]any{
				{"id": "school-org", "is_default": true},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal OpenAI ID token payload: %v", err)
	}
	return "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func TestEnrichOpenAIOAuthCredentialsFromIDTokenSeparatesK12WorkspaceMembers(t *testing.T) {
	firstCredentials := map[string]any{
		"access_token": "access-a",
		"id_token":     testOpenAIImportIDToken(t, "teacher-a"),
	}
	secondCredentials := map[string]any{
		"access_token": "access-b",
		"id_token":     testOpenAIImportIDToken(t, "teacher-b"),
	}

	require.NoError(t, EnrichOpenAIOAuthCredentialsFromIDToken(firstCredentials))
	require.NoError(t, EnrichOpenAIOAuthCredentialsFromIDToken(secondCredentials))
	require.Equal(t, "school-workspace", firstCredentials["chatgpt_account_id"])
	require.Equal(t, "school-org", firstCredentials["organization_id"])
	require.Equal(t, "teacher-a", firstCredentials["chatgpt_user_id"])
	require.Equal(t, "teacher-b", secondCredentials["chatgpt_user_id"])

	err := ensureOwnedAccountBatchNotDuplicate([]*Account{
		{
			ID:          1,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Credentials: firstCredentials,
		},
		{
			ID:          2,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Credentials: secondCredentials,
		},
	})
	require.NoError(t, err)
}

func TestEnrichOpenAIOAuthCredentialsFromIDTokenPreservesExplicitFields(t *testing.T) {
	credentials := map[string]any{
		"id_token":           testOpenAIImportIDToken(t, "token-teacher"),
		"email":              "explicit@school.example",
		"chatgpt_user_id":    "explicit-teacher",
		"organization_id":    123,
		"chatgpt_account_id": "",
	}

	require.NoError(t, EnrichOpenAIOAuthCredentialsFromIDToken(credentials))
	require.Equal(t, "explicit@school.example", credentials["email"])
	require.Equal(t, "explicit-teacher", credentials["chatgpt_user_id"])
	require.Equal(t, 123, credentials["organization_id"])
	require.Equal(t, "school-workspace", credentials["chatgpt_account_id"])
	require.Equal(t, "chatgpt-k12", credentials["plan_type"])
}

func TestAccountCredentialImportSupportsAgentIdentitySchemas(t *testing.T) {
	privateKey := testAgentIdentityPrivateKey(t)
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "top-level camel case",
			content: `{"authMode":"agentIdentity","agentRuntimeId":"runtime-top","agentPrivateKey":"` + privateKey + `","chatgptAccountId":"acct-1","chatgptUserId":"user-1","planType":"team"}`,
		},
		{
			name:    "nested snake case",
			content: `{"name":"agent account","agent_identity":{"agent_runtime_id":"runtime-nested","agent_private_key":"` + privateKey + `","task_id":"task-1","email":"agent@example.com","chatgpt_account_is_fedramp":true}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sources, errs := ParseAccountCredentialImportContents([]string{test.content})
			if len(errs) != 0 {
				t.Fatalf("errs = %#v, want none", errs)
			}
			if len(sources) != 1 {
				t.Fatalf("sources len = %d, want 1", len(sources))
			}
			source := sources[0]
			if source.Kind != AccountCredentialImportKindOpenAIAgentIdentity {
				t.Fatalf("kind = %s, want %s", source.Kind, AccountCredentialImportKindOpenAIAgentIdentity)
			}
			if source.Platform != PlatformOpenAI {
				t.Fatalf("platform = %s, want %s", source.Platform, PlatformOpenAI)
			}
			if source.Credentials["auth_mode"] != OpenAIAuthModeAgentIdentity {
				t.Fatalf("auth_mode = %#v", source.Credentials["auth_mode"])
			}
			if source.Credentials["agent_private_key"] != privateKey {
				t.Fatal("agent private key was not preserved")
			}
			if _, ok := source.Credentials["access_token"]; ok {
				t.Fatal("Agent Identity must not synthesize an access token")
			}
		})
	}
}

func TestAccountCredentialImportSupportsAgentIdentityAccountExportEnvelope(t *testing.T) {
	privateKey := testAgentIdentityPrivateKey(t)
	content := `{
		"type":"sub2api-data",
		"version":1,
		"exported_at":"2026-07-22T00:00:00Z",
		"proxies":[],
		"accounts":[{
			"name":"exported agent",
			"platform":"openai",
			"type":"oauth",
			"credentials":{
				"account_id":"legacy-account",
				"agent_private_key":"` + privateKey + `",
				"agent_runtime_id":"runtime-export",
				"auth_mode":"agentIdentity",
				"chatgpt_account_id":"team-export",
				"chatgpt_user_id":"user-export",
				"id_token":"legacy-id-token-must-not-be-stored",
				"workspace_id":"workspace-export"
			},
			"extra":{"email":"agent@example.com","email_key":"mail-key"},
			"concurrency":1,
			"priority":50
		}]
	}`

	sources, errs := ParseAccountCredentialImportContents([]string{content})
	if len(errs) != 0 {
		t.Fatalf("errs = %#v, want none", errs)
	}
	if len(sources) != 1 {
		t.Fatalf("sources len = %d, want 1", len(sources))
	}
	source := sources[0]
	if source.Kind != AccountCredentialImportKindOpenAIAgentIdentity {
		t.Fatalf("kind = %s, want %s", source.Kind, AccountCredentialImportKindOpenAIAgentIdentity)
	}
	if source.Name != "exported agent" {
		t.Fatalf("name = %q, want exported agent", source.Name)
	}
	if source.Credentials["agent_runtime_id"] != "runtime-export" || source.Credentials["chatgpt_account_id"] != "team-export" {
		t.Fatalf("credentials identifiers were not preserved: %#v", source.Credentials)
	}
	for _, field := range []string{"access_token", "refresh_token", "id_token", "account_id", "workspace_id"} {
		if _, ok := source.Credentials[field]; ok {
			t.Fatalf("credential field %q must not be retained", field)
		}
	}
	if source.Extra["email_key"] != "mail-key" {
		t.Fatalf("safe extra metadata was not preserved: %#v", source.Extra)
	}
	if err := validateOwnedAccountSourceForPlatform(source.Platform, AccountTypeOAuth, source.Credentials, source.Extra); err != nil {
		t.Fatalf("parsed envelope failed owned-account validation: %v", err)
	}
}

func TestAccountCredentialImportAgentIdentityAccountExportEnvelopeRejectsUnsafeTokens(t *testing.T) {
	privateKey := testAgentIdentityPrivateKey(t)
	tests := []struct {
		name     string
		injected string
		want     string
	}{
		{name: "access token", injected: `"access_token":"must-reject"`, want: "access_token"},
		{name: "refresh token", injected: `"refreshToken":"must-reject"`, want: "refreshToken"},
		{name: "nested id token", injected: `"metadata":{"id_token":"must-reject"}`, want: "id_token"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			content := `{"accounts":[{"name":"agent","platform":"openai","type":"oauth","credentials":{` +
				`"auth_mode":"agentIdentity","agent_runtime_id":"runtime-1","agent_private_key":"` + privateKey + `",` +
				`"chatgpt_account_id":"team-1",` + test.injected + `}}]}`
			_, errs := ParseAccountCredentialImportContents([]string{content})
			if len(errs) != 1 || !strings.Contains(errs[0].Message, test.want) {
				t.Fatalf("errs = %#v, want one error containing %q", errs, test.want)
			}
		})
	}
}

func TestAccountCredentialImportAgentIdentityAccountExportEnvelopeRejectsInvalidMetadata(t *testing.T) {
	privateKey := testAgentIdentityPrivateKey(t)
	tests := []struct {
		name        string
		outerFields string
		want        string
	}{
		{name: "wrong platform", outerFields: `"platform":"anthropic","type":"oauth",`, want: "platform must be OpenAI"},
		{name: "wrong account type", outerFields: `"platform":"openai","type":"api_key",`, want: "type must be OAuth"},
		{name: "duplicate auth mode", outerFields: `"platform":"openai","type":"oauth","auth_mode":"agentIdentity",`, want: "auth_mode must be declared only inside credentials"},
		{name: "duplicate identity", outerFields: `"platform":"openai","type":"oauth","agent_identity":{},`, want: "must not be declared in both"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			content := `{"accounts":[{` + test.outerFields + `"credentials":{` +
				`"auth_mode":"agentIdentity","agent_runtime_id":"runtime-1","agent_private_key":"` + privateKey + `",` +
				`"chatgpt_account_id":"team-1"}}]}`
			_, errs := ParseAccountCredentialImportContents([]string{content})
			if len(errs) != 1 || !strings.Contains(errs[0].Message, test.want) {
				t.Fatalf("errs = %#v, want one error containing %q", errs, test.want)
			}
		})
	}
}

func TestAccountCredentialImportRejectsInvalidAgentIdentity(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "missing runtime id",
			content: `{"auth_mode":"agentIdentity","agent_private_key":"unused"}`,
			want:    "requires agent_runtime_id",
		},
		{
			name:    "invalid private key",
			content: `{"auth_mode":"agentIdentity","agent_runtime_id":"runtime-1","agent_private_key":"not-base64"}`,
			want:    "not valid base64",
		},
		{
			name:    "unsafe nested field",
			content: `{"agent_identity":{"agent_runtime_id":"runtime-1","agent_private_key":"` + testAgentIdentityPrivateKey(t) + `","base_url":"https://evil.example"}}`,
			want:    "disallowed credential field: base_url",
		},
		{
			name:    "top-level OAuth token",
			content: `{"auth_mode":"agentIdentity","agent_runtime_id":"runtime-1","agent_private_key":"` + testAgentIdentityPrivateKey(t) + `","access_token":"must-reject"}`,
			want:    "must not include OAuth token field: access_token",
		},
		{
			name:    "top-level ID token",
			content: `{"auth_mode":"agentIdentity","agent_runtime_id":"runtime-1","agent_private_key":"` + testAgentIdentityPrivateKey(t) + `","id_token":"must-reject"}`,
			want:    "must not include OAuth token field: id_token",
		},
		{
			name:    "nested OAuth token",
			content: `{"agent_identity":{"agent_runtime_id":"runtime-1","agent_private_key":"` + testAgentIdentityPrivateKey(t) + `","tokens":[{"refreshToken":"must-reject"}]}}`,
			want:    "must not include OAuth token field: refreshToken",
		},
		{
			name:    "nested OAuth token normalized key",
			content: `{"agent_identity":{"agent_runtime_id":"runtime-1","agent_private_key":"` + testAgentIdentityPrivateKey(t) + `","metadata":{" ID_TOKEN ":"must-reject"}}}`,
			want:    "must not include OAuth token field:  ID_TOKEN ",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, errs := ParseAccountCredentialImportContents([]string{test.content})
			if len(errs) != 1 || !strings.Contains(errs[0].Message, test.want) {
				t.Fatalf("errs = %#v, want message containing %q", errs, test.want)
			}
		})
	}
}

func TestAccountCredentialImportSupportsGrokOAuthJSON(t *testing.T) {
	sources, errs := ParseAccountCredentialImportContents([]string{`{
		"name": "work grok",
		"platform": "xai",
		"type": "oauth",
		"credentials": {
			"access_token": "access-token",
			"refresh_token": "refresh-token"
		}
	}`})

	if len(errs) != 0 {
		t.Fatalf("errs = %#v, want none", errs)
	}
	if len(sources) != 1 {
		t.Fatalf("sources len = %d, want 1", len(sources))
	}
	source := sources[0]
	if source.Kind != AccountCredentialImportKindOAuthCredentials {
		t.Fatalf("kind = %s, want %s", source.Kind, AccountCredentialImportKindOAuthCredentials)
	}
	if source.Platform != PlatformGrok {
		t.Fatalf("platform = %s, want %s", source.Platform, PlatformGrok)
	}
	if got := source.Credentials["access_token"]; got != "access-token" {
		t.Fatalf("access_token = %#v, want access-token", got)
	}
}

func TestAccountCredentialImportRejectsGrokAPIKeyJSON(t *testing.T) {
	_, errs := ParseAccountCredentialImportContents([]string{`{
		"name": "must reject",
		"platform": "grok",
		"type": "api_key",
		"credentials": {
			"api_key": "xai-secret",
			"base_url": "https://api.x.ai/v1"
		}
	}`})

	if len(errs) != 1 || !strings.Contains(errs[0].Message, "disallowed credential field") {
		t.Fatalf("errs = %#v, want Grok API key rejection", errs)
	}
}

func TestDeriveAccountCredentialImportNameSupportsGrok(t *testing.T) {
	got := DeriveAccountCredentialImportName(PlatformGrok, nil, nil, 3)
	if got != "Grok OAuth Account #3" {
		t.Fatalf("name = %q, want Grok OAuth Account #3", got)
	}
}
