package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestUserAccountRuntimeResponsesStripHeaderOverrideValues(t *testing.T) {
	handler := &UserAccountHandler{}
	account := service.Account{
		ID:       501,
		Platform: service.PlatformGrok,
		Type:     service.AccountTypeOAuth,
		Credentials: map[string]any{
			service.CredentialKeyHeaderOverrideEnabled: true,
			service.CredentialKeyHeaderOverrides: map[string]any{
				"x-relay-token": "relay-secret",
			},
		},
	}

	single := handler.buildAccountResponseWithRuntime(context.Background(), &account)
	assertUserAccountHeaderOverridesRedacted(t, single.Account)

	list := handler.buildAccountListResponseWithRuntime(context.Background(), []service.Account{account})
	if len(list) != 1 {
		t.Fatalf("list response length = %d, want 1", len(list))
	}
	assertUserAccountHeaderOverridesRedacted(t, list[0].Account)
}

func assertUserAccountHeaderOverridesRedacted(t *testing.T, account *dto.Account) {
	t.Helper()
	if account == nil {
		t.Fatal("user account response is nil")
	}
	credentials := account.Credentials
	if _, ok := credentials[service.CredentialKeyHeaderOverrides]; ok {
		t.Fatalf("header override values leaked in user response: %#v", credentials)
	}
}
