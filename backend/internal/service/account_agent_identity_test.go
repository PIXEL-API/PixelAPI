package service

import (
	"context"
	"errors"
	"testing"
)

type credentialFieldCheckerStub struct {
	exists bool
	err    error
	key    string
	value  string
}

func (stub *credentialFieldCheckerStub) ExistsByCredentialField(_ context.Context, key, value string) (bool, error) {
	stub.key = key
	stub.value = value
	return stub.exists, stub.err
}

func TestAccountIsOpenAIAgentIdentity(t *testing.T) {
	tests := []struct {
		name    string
		account *Account
		want    bool
	}{
		{name: "nil", account: nil, want: false},
		{name: "agent identity", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"auth_mode": " AgentIdentity "}}, want: true},
		{name: "regular oauth", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"auth_mode": "oauth"}}, want: false},
		{name: "wrong platform", account: &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"auth_mode": OpenAIAuthModeAgentIdentity}}, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.account.IsOpenAIAgentIdentity(); got != test.want {
				t.Fatalf("IsOpenAIAgentIdentity() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestShouldEnsureOAuthPrivacyAfterCreate(t *testing.T) {
	regular := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	agent := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"auth_mode": OpenAIAuthModeAgentIdentity}}

	if !shouldEnsureOAuthPrivacyAfterCreate(regular) {
		t.Fatal("regular OpenAI OAuth account must retain create-time privacy setup")
	}
	if shouldEnsureOAuthPrivacyAfterCreate(agent) {
		t.Fatal("Agent Identity account must skip create-time privacy setup")
	}
}

func TestCredentialFieldExistsUsesNarrowRepositoryCapability(t *testing.T) {
	stub := &credentialFieldCheckerStub{exists: true}
	exists, err := credentialFieldExists(context.Background(), stub, "agent_runtime_id", "runtime-1")
	if err != nil || !exists {
		t.Fatalf("exists = %v, err = %v", exists, err)
	}
	if stub.key != "agent_runtime_id" || stub.value != "runtime-1" {
		t.Fatalf("lookup = %q/%q", stub.key, stub.value)
	}

	if _, err := credentialFieldExists(context.Background(), struct{}{}, "agent_runtime_id", "runtime-1"); err == nil {
		t.Fatal("repository without the narrow lookup must fail fast")
	}

	wantErr := errors.New("lookup failed")
	stub.err = wantErr
	if _, err := credentialFieldExists(context.Background(), stub, "agent_runtime_id", "runtime-1"); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}
