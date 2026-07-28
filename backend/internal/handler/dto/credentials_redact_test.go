package dto

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestRedactCredentialsStripsAgentIdentityPrivateKey(t *testing.T) {
	credentials, status := RedactCredentials(map[string]any{
		"auth_mode":         "agentIdentity",
		"agent_runtime_id":  "runtime-1",
		"agent_private_key": "private-secret",
	})

	if _, ok := credentials["agent_private_key"]; ok {
		t.Fatal("agent_private_key must not be returned")
	}
	if credentials["auth_mode"] != "agentIdentity" || credentials["agent_runtime_id"] != "runtime-1" {
		t.Fatalf("non-sensitive Agent Identity metadata was lost: %#v", credentials)
	}
	if !status["has_agent_private_key"] {
		t.Fatalf("credentials status = %#v, want has_agent_private_key", status)
	}
}

func TestAccountFromServiceForUserStripsHeaderOverridesOnlyFromUserScope(t *testing.T) {
	account := &service.Account{
		ID:       91,
		Platform: service.PlatformGrok,
		Type:     service.AccountTypeOAuth,
		Credentials: map[string]any{
			service.CredentialKeyHeaderOverrideEnabled: true,
			service.CredentialKeyHeaderOverrides: map[string]any{
				"x-relay-token": "relay-secret",
			},
			"base_url": "https://relay.example.test/v1",
		},
	}

	adminAccount := AccountFromService(account)
	if _, ok := adminAccount.Credentials[service.CredentialKeyHeaderOverrides]; !ok {
		t.Fatal("administrator response must retain header overrides for edit flows")
	}

	userAccount := AccountFromServiceForUser(account)
	if _, ok := userAccount.Credentials[service.CredentialKeyHeaderOverrides]; ok {
		t.Fatal("user response must not return header override values")
	}
	if userAccount.Credentials[service.CredentialKeyHeaderOverrideEnabled] != true {
		t.Fatalf("non-secret header override state was lost: %#v", userAccount.Credentials)
	}
	if userAccount.Credentials["base_url"] != "https://relay.example.test/v1" {
		t.Fatalf("unrelated credential metadata was lost: %#v", userAccount.Credentials)
	}
}
