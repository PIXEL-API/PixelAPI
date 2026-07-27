package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountShareBillingSettlementEnabledMigrationRebuildsStrictContract(t *testing.T) {
	content, err := FS.ReadFile("244_account_share_billing_settlement_enabled_contract.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	previousContent, err := FS.ReadFile("239_account_share_billing_intent_v2.sql")
	require.NoError(t, err)
	previousSQL := strings.Join(strings.Fields(string(previousContent)), " ")

	require.Contains(t, sql, "DROP CONSTRAINT IF EXISTS account_share_billing_intent_payload_chk")
	require.Contains(t, sql, "ADD CONSTRAINT account_share_billing_intent_payload_chk")
	require.Contains(t, sql, "NOT VALID")
	require.Contains(t, sql, "VALIDATE CONSTRAINT account_share_billing_intent_payload_chk")

	v2Start := strings.Index(sql, "command_schema_version = 2")
	require.NotEqual(t, -1, v2Start)
	v3Start := strings.Index(sql, "command_schema_version = 3")
	require.Greater(t, v3Start, v2Start)
	v3EndOffset := strings.Index(sql[v3Start:], "AND (usage_payload IS NULL")
	require.NotEqual(t, -1, v3EndOffset)
	v2CommandContract := sql[v2Start:v3Start]
	v3CommandContract := sql[v3Start : v3Start+v3EndOffset]
	require.NotContains(t, v2CommandContract, "'settlement_enabled'")
	require.Contains(t, v3CommandContract, "'settlement_enabled'")

	previousV2Start := strings.Index(previousSQL, "command_schema_version = 2")
	require.NotEqual(t, -1, previousV2Start)
	previousV2EndOffset := strings.Index(previousSQL[previousV2Start:], "AND (usage_payload IS NULL")
	require.NotEqual(t, -1, previousV2EndOffset)
	previousV2CommandContract := previousSQL[previousV2Start : previousV2Start+previousV2EndOffset]
	require.Equal(
		t,
		accountShareBillingCommandKeyArray(t, previousV2CommandContract),
		accountShareBillingCommandKeyArray(t, v2CommandContract),
		"migration 244 must not invalidate historical V2 payloads",
	)

	v1Start := strings.Index(sql, "command_schema_version = 1")
	require.NotEqual(t, -1, v1Start)
	v1CommandContract := sql[v1Start:v2Start]
	require.NotContains(t, v1CommandContract, "'settlement_enabled'")

	for _, requiredContract := range []string{
		"usage_schema_version = 1",
		"usage_schema_version = 2",
		"'request_payload_hash'",
		"'model_mapping_chain'",
		"'billing_tier'",
		"'cache_ttl_overridden'",
		"'account_stats_cost'",
		"'provider_request_id'",
	} {
		require.Contains(t, sql, requiredContract)
	}

	lowerSQL := strings.ToLower(sql)
	for _, forbidden := range []string{
		"access_token",
		"refresh_token",
		"authorization",
		"api_key_secret",
		"proxy_password",
		"raw_request",
		"raw_response",
		"user_agent",
		"client_ip",
	} {
		require.NotContains(t, lowerSQL, "'"+forbidden+"'")
	}

	upperSQL := strings.ToUpper(sql)
	require.NotContains(t, upperSQL, "INSERT INTO ")
	require.NotContains(t, upperSQL, "UPDATE ")
	require.NotContains(t, upperSQL, "DELETE FROM ")
	require.NotContains(t, upperSQL, "TRUNCATE ")
	require.NotContains(t, upperSQL, "DROP TABLE")
}

func accountShareBillingCommandKeyArray(t *testing.T, contract string) string {
	t.Helper()
	start := strings.Index(contract, "ARRAY[")
	require.NotEqual(t, -1, start)
	endOffset := strings.Index(contract[start:], "]::text[]")
	require.NotEqual(t, -1, endOffset)
	return contract[start : start+endOffset+len("]::text[]")]
}
