package repository

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

const accountShareListingRevisionsMigration = "234_account_share_listing_revisions.sql"

func TestAccountShareListingRevisionsMigrationIsExpandOnlyAndTraceable(t *testing.T) {
	content, err := migrations.FS.ReadFile(accountShareListingRevisionsMigration)
	require.NoError(t, err)
	sqlText := string(content)

	online, err := validateMigrationExecutionMode(accountShareListingRevisionsMigration, sqlText)
	require.NoError(t, err)
	require.False(t, online)

	for _, column := range []string{
		"row_version BIGINT NOT NULL DEFAULT 1",
		"current_revision_id BIGINT",
		"validated_at TIMESTAMPTZ",
		"draining_at TIMESTAMPTZ",
		"paused_at TIMESTAMPTZ",
		"suspended_at TIMESTAMPTZ",
		"status_reason_code VARCHAR(64)",
		"status_reason TEXT",
		"pending_operation_id UUID",
		"deleted_by_user_id BIGINT",
		"delete_reason TEXT",
		"delete_request_id VARCHAR(128)",
		"deleted_revision_id BIGINT",
		"deletion_snapshot JSONB",
	} {
		require.Contains(t, sqlText, column)
	}

	require.Contains(t, sqlText, "CREATE TABLE IF NOT EXISTS account_share_listing_revisions")
	require.Contains(t, sqlText, "schema_version INTEGER NOT NULL DEFAULT 1")
	require.Contains(t, sqlText, "snapshot_quality VARCHAR(20) NOT NULL DEFAULT 'exact'")
	require.Contains(t, sqlText, "owner_user_id BIGINT NOT NULL")
	require.Contains(t, sqlText, "owner_display_name_snapshot VARCHAR(255) NOT NULL")
	require.Contains(t, sqlText, "operation_id UUID")
	require.Contains(t, sqlText, "CREATE TABLE IF NOT EXISTS account_share_room_events")

	normalized := strings.ToLower(stripSQLLineComment(sqlText))
	require.NotRegexp(t, regexp.MustCompile(`(?m)\bupdate\s+account_share_(listings|memberships)\b`), normalized)
	require.NotContains(t, normalized, "reserved_paid_seats")
	require.NotContains(t, normalized, "reserved_owner_seats")
	require.NotContains(t, normalized, "delete_state")
}

func TestAccountShareListingRevisionsMigrationPreservesImmutableAuditAndHonestSnapshots(t *testing.T) {
	content, err := migrations.FS.ReadFile(accountShareListingRevisionsMigration)
	require.NoError(t, err)
	sqlText := string(content)

	require.Contains(t, sqlText, "snapshot_quality IN ('exact', 'backfilled_current', 'unknown')")
	require.Contains(t, sqlText, "trg_account_share_listing_revisions_immutable")
	require.Contains(t, sqlText, "trg_account_share_room_events_immutable")
	require.GreaterOrEqual(t, strings.Count(sqlText, "BEFORE UPDATE OR DELETE"), 2)
	require.Contains(t, sqlText, "FOREIGN KEY (id, current_revision_id)")
	require.Contains(t, sqlText, "FOREIGN KEY (listing_id, listing_revision_id)")
	require.Contains(t, sqlText, "ON DELETE RESTRICT")
}
