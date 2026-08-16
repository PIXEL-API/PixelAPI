package admin

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestNormalizeCodexImportEntryAcceptsAgentIdentityAuthJSON(t *testing.T) {
	value := buildAgentIdentityImportValue(t, "runtime-import", "team-import", "user-import", "")
	identity, ok := value["agent_identity"].(map[string]any)
	require.True(t, ok)
	identity["email"] = "agent@example.invalid"
	identity["plan_type"] = "pro"
	identity["chatgpt_account_is_fedramp"] = false

	item, err := normalizeCodexImportEntry(codexImportEntry{Index: 1, Value: value})

	require.NoError(t, err)
	require.True(t, item.IsAgentIdentity)
	require.Equal(t, service.OpenAIAuthModeAgentIdentity, item.Credentials["auth_mode"])
	require.Equal(t, "runtime-import", item.Credentials["agent_runtime_id"])
	require.Equal(t, identity["agent_private_key"], item.Credentials["agent_private_key"])
	require.Equal(t, "team-import", item.Credentials["chatgpt_account_id"])
	require.Equal(t, "user-import", item.Credentials["chatgpt_user_id"])
	require.NotContains(t, item.Credentials, "access_token")
	require.NotContains(t, item.Credentials, "refresh_token")
	require.NotEmpty(t, item.WarningTexts)
}

func TestCodexAgentIdentityIndexSeparatesTeamsAndMergesSameTeam(t *testing.T) {
	keys := buildCodexAgentIdentityKeys("team-a")
	require.Equal(t, []string{"account:team-a"}, keys)

	existing := service.Account{ID: 1, Credentials: map[string]any{
		"auth_mode":          service.OpenAIAuthModeAgentIdentity,
		"chatgpt_account_id": "team-a",
		"chatgpt_user_id":    "same-user",
		"agent_runtime_id":   "runtime-a",
	}}
	index := buildCodexAccountIndex([]service.Account{existing})

	matched, _ := index.Find(buildCodexAgentIdentityKeys("team-b"), "same-user")
	require.Nil(t, matched)
	matched, matchedKey := index.Find(buildCodexAgentIdentityKeys("team-a"), "same-user")
	require.NotNil(t, matched)
	require.Equal(t, int64(1), matched.ID)
	require.Equal(t, "account:team-a", matchedKey)
}

func TestImportCodexSessionsMergesAgentRuntimeForSameTeamWithoutOAuthExpiry(t *testing.T) {
	first := buildAgentIdentityImportValue(t, "runtime-a", "team-a", "same-user", "task-a")
	second := buildAgentIdentityImportValue(t, "runtime-b", "team-a", "same-user", "task-b")
	firstIdentity, ok := first["agent_identity"].(map[string]any)
	require.True(t, ok)
	svc := newCodexImportMemoryAdminService([]service.Account{{
		ID: 41, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{
			"auth_mode":          service.OpenAIAuthModeAgentIdentity,
			"agent_runtime_id":   firstIdentity["agent_runtime_id"],
			"agent_private_key":  firstIdentity["agent_private_key"],
			"task_id":            firstIdentity["task_id"],
			"chatgpt_account_id": firstIdentity["account_id"],
			"chatgpt_user_id":    firstIdentity["chatgpt_user_id"],
		},
	}})

	result, err := newCodexImportTestHandler(svc).importCodexSessions(context.Background(), CodexSessionImportRequest{
		SkipDefaultGroupBind: codexImportBoolPtr(true),
	}, []codexImportEntry{{Index: 1, Value: second}})

	require.NoError(t, err)
	require.Zero(t, result.Created)
	require.Equal(t, 1, result.Updated)
	require.Len(t, svc.updatedAccounts, 1)
	require.Equal(t, "runtime-b", svc.updatedAccounts[0].input.Credentials["agent_runtime_id"])
	require.Equal(t, "task-b", svc.updatedAccounts[0].input.Credentials["task_id"])
	require.Nil(t, svc.updatedAccounts[0].input.ExpiresAt)
	require.Nil(t, svc.updatedAccounts[0].input.AutoPauseOnExpired)
}

func TestImportCodexSessionsKeepsAgentIdentityTeamsSeparate(t *testing.T) {
	svc := newCodexImportMemoryAdminService(nil)
	result, err := newCodexImportTestHandler(svc).importCodexSessions(context.Background(), CodexSessionImportRequest{
		SkipDefaultGroupBind: codexImportBoolPtr(true),
	}, []codexImportEntry{
		{Index: 1, Value: buildAgentIdentityImportValue(t, "runtime-a", "team-a", "same-user", "task-a")},
		{Index: 2, Value: buildAgentIdentityImportValue(t, "runtime-b", "team-b", "same-user", "task-b")},
	})

	require.NoError(t, err)
	require.Equal(t, 2, result.Created)
	require.Zero(t, result.Updated)
	require.Zero(t, result.Skipped)
	require.Len(t, svc.createdAccounts, 2)
	for _, created := range svc.createdAccounts {
		require.Nil(t, created.ExpiresAt)
		require.Nil(t, created.AutoPauseOnExpired)
	}
}

func buildAgentIdentityImportValue(t *testing.T, runtimeID, accountID, userID, taskID string) map[string]any {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	require.NoError(t, err)
	return map[string]any{
		"auth_mode": "agentIdentity",
		"agent_identity": map[string]any{
			"agent_runtime_id":  runtimeID,
			"agent_private_key": base64.StdEncoding.EncodeToString(der),
			"task_id":           taskID,
			"account_id":        accountID,
			"chatgpt_user_id":   userID,
		},
	}
}
