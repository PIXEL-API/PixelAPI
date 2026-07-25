package handler

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func testUserK12CredentialImportIDToken(t *testing.T) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "school-workspace",
			"chatgpt_user_id":    "teacher-a",
			"chatgpt_plan_type":  "chatgpt-k12",
			"organizations": []map[string]any{
				{"id": "school-org", "is_default": true},
			},
		},
	})
	require.NoError(t, err)
	return "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func TestEnrichUserK12CredentialImportSourceOnlyAffectsK12OAuth(t *testing.T) {
	tests := []struct {
		name         string
		platform     string
		kind         service.AccountCredentialImportKind
		accountLevel string
		wantEnriched bool
	}{
		{
			name:         "K12 OpenAI OAuth is enriched",
			platform:     service.PlatformOpenAI,
			kind:         service.AccountCredentialImportKindOAuthCredentials,
			accountLevel: service.AccountLevelK12,
			wantEnriched: true,
		},
		{
			name:         "Plus keeps existing import behavior",
			platform:     service.PlatformOpenAI,
			kind:         service.AccountCredentialImportKindOAuthCredentials,
			accountLevel: service.AccountLevelPlus,
		},
		{
			name:         "Team keeps existing import behavior",
			platform:     service.PlatformOpenAI,
			kind:         service.AccountCredentialImportKindOAuthCredentials,
			accountLevel: service.AccountLevelTeam,
		},
		{
			name:         "Free keeps existing import behavior",
			platform:     service.PlatformOpenAI,
			kind:         service.AccountCredentialImportKindOAuthCredentials,
			accountLevel: service.AccountLevelFree,
		},
		{
			name:         "non-OpenAI OAuth is unchanged",
			platform:     service.PlatformAnthropic,
			kind:         service.AccountCredentialImportKindOAuthCredentials,
			accountLevel: service.AccountLevelK12,
		},
		{
			name:         "OpenAI refresh-token source is unchanged",
			platform:     service.PlatformOpenAI,
			kind:         service.AccountCredentialImportKindOpenAIRefreshToken,
			accountLevel: service.AccountLevelK12,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := service.AccountCredentialImportSource{
				Kind:     test.kind,
				Platform: test.platform,
				Credentials: map[string]any{
					"access_token": "access-token",
					"id_token":     testUserK12CredentialImportIDToken(t),
				},
			}

			require.NoError(t, enrichUserK12CredentialImportSource(&source, test.accountLevel))
			if test.wantEnriched {
				require.Equal(t, "teacher-a", source.Credentials["chatgpt_user_id"])
				return
			}
			require.NotContains(t, source.Credentials, "chatgpt_user_id")
		})
	}
}
