package service

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"strings"
	"testing"
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
