package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountShareBillingHistoryIndexesAreRetrySafe(t *testing.T) {
	content, err := FS.ReadFile("245_account_share_billing_history_indexes_notx.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")

	require.Equal(t, 2, strings.Count(sql, "ON public.account_share_request_billing_intents("))
	require.NotContains(t, sql, "ON account_share_request_billing_intents")
	for _, indexName := range []string{
		"idx_account_share_billing_intents_membership_history",
		"idx_account_share_billing_intents_consumer_spend",
	} {
		createAt := strings.Index(sql, "CREATE INDEX CONCURRENTLY IF NOT EXISTS "+indexName)
		require.NotEqual(t, -1, createAt, indexName)
		require.NotContains(t, sql, "DROP INDEX CONCURRENTLY IF EXISTS "+indexName)
	}
	require.NotContains(t, strings.ToUpper(sql), "BEGIN")
	require.NotContains(t, strings.ToUpper(sql), "COMMIT")
}
