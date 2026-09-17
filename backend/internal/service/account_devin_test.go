//go:build unit

package service

import (
	"testing"
)

func TestDevinAccountAccessors(t *testing.T) {
	account := &Account{
		Platform: PlatformDevin,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "devin-session-token$abc",
		},
	}
	if !account.IsDevin() || !account.IsDevinAPIKey() {
		t.Fatal("devin apikey account must satisfy IsDevin/IsDevinAPIKey")
	}
	if got := account.GetDevinToken(); got != "devin-session-token$abc" {
		t.Fatalf("token = %q", got)
	}
	if got := account.GetDevinBaseURL(); got != "https://server.codeium.com" {
		t.Fatalf("default base url = %q", got)
	}
}

func TestDevinAccountBaseURLOverride(t *testing.T) {
	account := &Account{
		Platform: PlatformDevin,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "devin-session-token$abc",
			"base_url": "https://devin-relay.example.com/",
		},
	}
	if got := account.GetDevinBaseURL(); got != "https://devin-relay.example.com" {
		t.Fatalf("base url = %q, want trailing slash trimmed", got)
	}
}

func TestDevinHelpersRejectWrongPlatformOrType(t *testing.T) {
	openai := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-x"}}
	if openai.IsDevin() || openai.GetDevinToken() != "" || openai.GetDevinBaseURL() != "" {
		t.Fatal("non-devin account must not expose devin credentials")
	}
	oauth := &Account{Platform: PlatformDevin, Type: AccountTypeOAuth,
		Credentials: map[string]any{"api_key": "devin-session-token$abc"}}
	if oauth.IsDevinAPIKey() || oauth.GetDevinToken() != "" {
		t.Fatal("devin oauth account must not expose token")
	}
	// oauth 平台本身仍是 devin，base_url 兜底默认值可读（用于展示），但 token 必须为空。
	if got := oauth.GetDevinBaseURL(); got != "https://server.codeium.com" {
		t.Fatalf("base url = %q", got)
	}
}

func TestValidateDevinAccountConfiguration(t *testing.T) {
	if err := validateDevinAccountConfiguration(PlatformDevin, AccountTypeAPIKey, map[string]any{
		"api_key": "devin-session-token$abc",
	}); err != nil {
		t.Fatalf("devin apikey configuration was rejected: %v", err)
	}
	if err := validateDevinAccountConfiguration(PlatformDevin, AccountTypeOAuth, map[string]any{
		"api_key": "devin-session-token$abc",
	}); err == nil {
		t.Fatal("devin oauth account must be rejected")
	}
	if err := validateDevinAccountConfiguration(PlatformOpenAI, AccountTypeAPIKey, nil); err != nil {
		t.Fatalf("non-devin platform must not be affected: %v", err)
	}
}

func TestIsAllowedOwnedAccountTypeForDevin(t *testing.T) {
	if !isAllowedOwnedAccountType(PlatformDevin, AccountTypeAPIKey) {
		t.Fatal("devin owned apikey account must be allowed")
	}
	if isAllowedOwnedAccountType(PlatformDevin, AccountTypeOAuth) {
		t.Fatal("devin owned oauth account must be rejected")
	}
}
