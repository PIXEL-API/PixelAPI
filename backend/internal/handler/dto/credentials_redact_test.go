package dto

import "testing"

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
