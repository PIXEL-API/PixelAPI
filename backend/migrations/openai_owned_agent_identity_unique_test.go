package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const openAIOwnedAgentIdentityUniqueMigration = "217_openai_owned_agent_identity_unique_notx.sql"

func TestOpenAIOwnedAgentIdentityUniqueMigrationBuildsBeforeDropping(t *testing.T) {
	content, err := FS.ReadFile(openAIOwnedAgentIdentityUniqueMigration)
	require.NoError(t, err)

	sql := string(content)
	require.Equal(t, 5, strings.Count(sql, "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS"))
	require.Equal(t, 4, strings.Count(sql, "DROP INDEX CONCURRENTLY IF EXISTS"))
	require.Equal(t, 5, strings.Count(sql, "ON public.accounts ("))

	firstDrop := strings.Index(sql, "DROP INDEX CONCURRENTLY IF EXISTS")
	require.Positive(t, firstDrop)
	for _, indexName := range []string{
		"idx_accounts_owned_openai_org_user_v2_uniq",
		"idx_accounts_owned_openai_org_account_v2_uniq",
		"idx_accounts_owned_openai_legacy_user_v2_uniq",
		"idx_accounts_owned_openai_legacy_account_v2_uniq",
		"idx_accounts_owned_openai_agent_identity_team_uniq",
	} {
		position := strings.Index(sql, "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS "+indexName)
		require.GreaterOrEqual(t, position, 0, indexName)
		require.Less(t, position, firstDrop, indexName)
	}
	for _, indexName := range []string{
		"idx_accounts_owned_openai_org_user_uniq",
		"idx_accounts_owned_openai_org_account_uniq",
		"idx_accounts_owned_openai_legacy_user_uniq",
		"idx_accounts_owned_openai_legacy_account_uniq",
	} {
		require.Contains(t, sql, "DROP INDEX CONCURRENTLY IF EXISTS public."+indexName)
	}
	require.NotContains(t, sql[firstDrop:], "CREATE UNIQUE INDEX")
}

func TestOpenAIOwnedAgentIdentityUniqueMigrationSeparatesOAuthAndTeamSemantics(t *testing.T) {
	content, err := FS.ReadFile(openAIOwnedAgentIdentityUniqueMigration)
	require.NoError(t, err)
	sql := string(content)

	nonAgentPredicate := "COALESCE(LOWER(NULLIF(BTRIM(credentials->>'auth_mode'), '')), '') <> 'agentidentity'"
	require.Equal(t, 4, strings.Count(sql, nonAgentPredicate))

	teamStart := strings.Index(sql, "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_accounts_owned_openai_agent_identity_team_uniq")
	firstDrop := strings.Index(sql, "DROP INDEX CONCURRENTLY IF EXISTS")
	require.Positive(t, teamStart)
	require.Greater(t, firstDrop, teamStart)

	teamSQL := sql[teamStart:firstDrop]
	require.Contains(t, teamSQL, "owner_user_id")
	require.Contains(t, teamSQL, "credentials->>'chatgpt_account_id'")
	require.NotContains(t, teamSQL, "chatgpt_user_id")
	require.Contains(t, teamSQL, "LOWER(NULLIF(BTRIM(credentials->>'auth_mode'), '')) = 'agentidentity'")
	require.NotContains(t, sql, "idx_accounts_owned_openai_agent_identity_runtime_uniq")

	upperSQL := strings.ToUpper(sql)
	require.NotContains(t, upperSQL, "UPDATE ACCOUNTS")
	require.NotContains(t, upperSQL, "DELETE FROM ACCOUNTS")
	require.NotContains(t, upperSQL, "INSERT INTO ACCOUNTS")
}
