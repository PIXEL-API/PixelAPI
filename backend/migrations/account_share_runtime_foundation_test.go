package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountShareRuntimeFoundationIsExpandOnlyAndFenced(t *testing.T) {
	content, err := FS.ReadFile("236_account_share_runtime_foundation.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")

	for _, table := range []string{
		"account_share_room_operations",
		"account_share_room_account_assignments",
		"account_share_membership_account_bindings",
		"account_share_request_billing_intents",
	} {
		require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS "+table)
	}
	require.Contains(t, sql, "UNIQUE (request_id, api_key_id_snapshot)")
	require.Contains(t, sql, "state_token BIGINT NOT NULL DEFAULT 1")
	require.Contains(t, sql, "lease_token BIGINT NOT NULL DEFAULT 0")
	require.Contains(t, sql, "status IN ('created', 'in_flight', 'ready', 'processing', 'settled', 'cancelled', 'failed', 'needs_attention')")
	require.Contains(t, sql, "actor_role IN ('owner', 'consumer', 'admin', 'system')")
	require.Contains(t, sql, "WHERE status NOT IN ('settled', 'cancelled')")
	require.Contains(t, sql, "account-share billing intent routing snapshot is immutable")
	require.Contains(t, sql, "NEW.usage_log_id IS DISTINCT FROM OLD.usage_log_id")
	require.Contains(t, sql, "NEW.snapshot_quality, NEW.created_at")
	require.Contains(t, sql, "OLD.snapshot_quality, OLD.created_at")
	require.Contains(t, sql, "forwarded account-share billing intent cannot be cancelled")
	require.Contains(t, sql, "payloads are versioned allowlists and never contain credentials or proxy secrets")
	require.Contains(t, sql, "account_share_jsonb_has_only_keys(")
	require.Contains(t, sql, "usage_payload ->> 'schema_version' = usage_schema_version::text")
	require.Contains(t, sql, "snapshot_quality IN ('exact', 'backfilled_current', 'unknown')")
	require.NotContains(t, sql, "reserved_paid_seats")
	require.NotContains(t, sql, "owner_reserved")

	upperSQL := strings.ToUpper(sql)
	require.NotContains(t, upperSQL, "INSERT INTO ACCOUNT_SHARE_LISTINGS")
	require.NotContains(t, upperSQL, "UPDATE ACCOUNT_SHARE_LISTINGS")
	require.NotContains(t, upperSQL, "DELETE FROM ")
	require.NotContains(t, upperSQL, "TRUNCATE ")
	require.NotContains(t, upperSQL, "DROP TABLE")
}

func TestAccountShareRuntimeIdentityIndexesAreConcurrentAndOrdered(t *testing.T) {
	content, err := FS.ReadFile("235_account_share_runtime_identity_indexes_notx.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")

	require.Contains(t, sql, "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_account_share_memberships_identity")
	require.Contains(t, sql, "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_account_share_memberships_revision_identity")
	require.Contains(t, sql, "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_account_share_listing_revision_terms_identity")
	require.Contains(t, sql, "ON public.account_share_memberships(id, listing_id)")
	require.Contains(t, sql, "ON public.account_share_memberships(id, listing_id, listing_revision_id)")
	require.Contains(t, sql, "ON public.account_share_listing_revisions(listing_id, id, revision_number)")
	require.NotContains(t, sql, "ON account_share_memberships")
	require.NotContains(t, sql, "ON account_share_listing_revisions")
	for _, indexName := range []string{
		"uq_account_share_memberships_identity",
		"uq_account_share_memberships_revision_identity",
		"uq_account_share_listing_revision_terms_identity",
	} {
		createAt := strings.Index(sql, "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS "+indexName)
		require.NotEqual(t, -1, createAt, indexName)
		require.NotContains(t, sql, "DROP INDEX CONCURRENTLY IF EXISTS "+indexName)
	}
	require.NotContains(t, strings.ToUpper(sql), "BEGIN")
	require.NotContains(t, strings.ToUpper(sql), "COMMIT")
}

func TestAccountShareBillingIntentV2MigrationKeepsVersionedStrictAllowlists(t *testing.T) {
	content, err := FS.ReadFile("239_account_share_billing_intent_v2.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")

	require.Contains(t, sql, "DROP CONSTRAINT IF EXISTS account_share_billing_intent_payload_chk")
	require.Contains(t, sql, "command_schema_version = 1")
	require.Contains(t, sql, "command_schema_version = 2")
	require.Contains(t, sql, "usage_schema_version = 1")
	require.Contains(t, sql, "usage_schema_version = 2")
	require.Contains(t, sql, "account_share_jsonb_has_only_keys(")
	require.Contains(t, sql, "'request_payload_hash'")
	require.Contains(t, sql, "'rate_multiplier_source'")
	require.Contains(t, sql, "'model_mapping_chain'")
	require.Contains(t, sql, "'billing_tier'")
	require.Contains(t, sql, "'cache_ttl_overridden'")
	require.Contains(t, sql, "'account_stats_cost'")
	require.Contains(t, sql, "ADD CONSTRAINT account_share_billing_intent_payload_chk")
	require.Contains(t, sql, "NOT VALID")
	require.Contains(t, sql, "VALIDATE CONSTRAINT account_share_billing_intent_payload_chk")

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
	require.NotContains(t, strings.ToUpper(sql), "UPDATE ACCOUNT_SHARE_REQUEST_BILLING_INTENTS SET")
	require.NotContains(t, strings.ToUpper(sql), "DELETE FROM")
	require.NotContains(t, strings.ToUpper(sql), "TRUNCATE")
}
