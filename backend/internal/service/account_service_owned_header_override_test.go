package service

import (
	"reflect"
	"testing"
)

func TestFindDisallowedOwnedAccountFieldRejectsHeaderOverrideCredentials(t *testing.T) {
	for _, key := range []string{
		CredentialKeyHeaderOverrideEnabled,
		CredentialKeyHeaderOverrides,
	} {
		t.Run(key, func(t *testing.T) {
			field, blocked := findDisallowedOwnedAccountField(map[string]any{
				"access_token": "owned-oauth-token",
				key:            map[string]any{"x-relay-token": "relay-secret"},
			})
			if !blocked || field != key {
				t.Fatalf("findDisallowedOwnedAccountField() = %q, %v; want %q, true", field, blocked, key)
			}
		})
	}
}

func TestPreserveOwnedPersonalCredentialPolicyKeepsAdministratorHeaderOverrides(t *testing.T) {
	existingOverrides := map[string]any{"x-relay-token": "administrator-secret"}
	account := &Account{Credentials: map[string]any{
		CredentialKeyHeaderOverrideEnabled: true,
		CredentialKeyHeaderOverrides:       existingOverrides,
	}}
	next := map[string]any{
		CredentialKeyHeaderOverrideEnabled: false,
		CredentialKeyHeaderOverrides: map[string]any{
			"x-relay-token": "user-replacement",
		},
	}

	preserveOwnedPersonalCredentialPolicy(account, next)

	if next[CredentialKeyHeaderOverrideEnabled] != true {
		t.Fatalf("header override enabled state changed: %#v", next)
	}
	if !reflect.DeepEqual(next[CredentialKeyHeaderOverrides], existingOverrides) {
		t.Fatalf("administrator header overrides changed: %#v", next)
	}
}

func TestPreserveOwnedPersonalCredentialPolicyDropsUserInjectedHeaderOverrides(t *testing.T) {
	account := &Account{Credentials: map[string]any{}}
	next := map[string]any{
		CredentialKeyHeaderOverrideEnabled: true,
		CredentialKeyHeaderOverrides: map[string]any{
			"x-relay-token": "user-injected",
		},
	}

	preserveOwnedPersonalCredentialPolicy(account, next)

	if _, ok := next[CredentialKeyHeaderOverrideEnabled]; ok {
		t.Fatalf("user-injected enabled state was retained: %#v", next)
	}
	if _, ok := next[CredentialKeyHeaderOverrides]; ok {
		t.Fatalf("user-injected header overrides were retained: %#v", next)
	}
}
