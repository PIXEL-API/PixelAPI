package repository

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"

	"github.com/stretchr/testify/require"
)

const (
	accountShareRoomAccountsExpandMigration   = "227_account_share_room_accounts_expand.sql"
	accountShareRoomAccountsBackfillMigration = "228_account_share_room_accounts_backfill_online.sql"
	accountShareRoomAccountsValidateMigration = "229_validate_account_share_room_accounts_backfill.sql"
	accountShareRoomAccountsContractMigration = "230_account_share_room_accounts_contract_online.sql"
)

func readAccountShareRoomAccountsMigration(t *testing.T, name string) string {
	t.Helper()

	content, err := migrations.FS.ReadFile(name)
	require.NoError(t, err)
	return string(content)
}

func TestAccountShareRoomAccountsExpandSeparatesEligibilityFromMembership(t *testing.T) {
	sqlText := readAccountShareRoomAccountsMigration(t, accountShareRoomAccountsExpandMigration)

	online, err := validateMigrationExecutionMode(accountShareRoomAccountsExpandMigration, sqlText)
	require.NoError(t, err)
	require.False(t, online)

	require.Contains(t, sqlText, "CREATE TABLE IF NOT EXISTS account_share_room_accounts")
	require.Contains(t, sqlText, "account_id BIGINT PRIMARY KEY")
	require.Contains(t, sqlText, "FOREIGN KEY (listing_id, owner_user_id, platform, account_level)")
	require.Contains(t, sqlText, "REFERENCES account_share_listings(id, owner_user_id, platform, account_level)")
	require.Contains(t, sqlText, "CONSTRAINT account_share_room_accounts_room_identity_fk")
	require.Contains(t, sqlText, "FOREIGN KEY (account_id, owner_user_id, platform, account_level)")
	require.Contains(t, sqlText, "REFERENCES accounts(id, owner_user_id, platform, account_level)")
	require.Contains(t, sqlText, "CHECK (state IN ('active', 'draining'))")
	require.Contains(t, sqlText, "CHECK (version > 0)")

	require.Contains(t, sqlText, "placement_type = 'room'\n            AND public_group_id IS NULL")
	require.Contains(t, sqlText, "target_type = 'room'\n            AND target_public_group_id IS NULL")
	require.Contains(t, sqlText, "trg_account_share_legacy_placement_sync_room_account")
	require.Contains(t, sqlText, "trg_account_share_room_account_sync_legacy_placement")
	require.Contains(t, sqlText, "trg_validate_account_share_room_account_qualification")
	require.Contains(t, sqlText, "trg_validate_room_account_memberships_before_removal")
	require.Contains(t, sqlText, "DEFERRABLE INITIALLY IMMEDIATE")
	require.Contains(t, sqlText, "previous release is still serving")
	require.NotContains(t, sqlText, "SET listing_id = NEW.listing_id,\n        state = NEW.state")
	require.NotContains(t, sqlText, "SET listing_id = NEW.listing_id,\n        priority = NEW.priority")
}

func TestAccountShareRoomAccountsBackfillIsBoundedResumableOnlineSQL(t *testing.T) {
	sqlText := readAccountShareRoomAccountsMigration(t, accountShareRoomAccountsBackfillMigration)

	online, err := validateMigrationExecutionMode(accountShareRoomAccountsBackfillMigration, sqlText)
	require.NoError(t, err)
	require.True(t, online)
	require.Len(t, splitSQLStatements(sqlText), 3)

	require.Contains(t, sqlText, "account_share_room_accounts_migration_progress")
	require.Contains(t, sqlText, "'legacy_room_placements'")
	require.Contains(t, sqlText, "high_water_mark")
	require.Contains(t, sqlText, "ORDER BY placement.account_id")
	require.Contains(t, sqlText, "LIMIT batch_size")
	require.Contains(t, sqlText, "FOR UPDATE OF placement")
	require.Contains(t, sqlText, "ON CONFLICT (account_id) DO UPDATE")
	require.GreaterOrEqual(t, strings.Count(sqlText, "COMMIT;"), 3)
	require.NotContains(t, strings.ToUpper(stripSQLLineComment(sqlText)), " OFFSET ")
}

func TestAccountShareRoomAccountsValidationFailsBeforeUnsafeCutover(t *testing.T) {
	sqlText := readAccountShareRoomAccountsMigration(t, accountShareRoomAccountsValidateMigration)

	online, err := validateMigrationExecutionMode(accountShareRoomAccountsValidateMigration, sqlText)
	require.NoError(t, err)
	require.False(t, online)

	require.Contains(t, sqlText, "completed\n        AND last_id = high_water_mark")
	require.Contains(t, sqlText, "legacy room placement is missing its independent room-account row")
	require.Contains(t, sqlText, "room account is missing platform account mode eligibility")
	require.Contains(t, sqlText, "active account-share membership has no independent room-account row")
	require.Contains(t, sqlText, "VALIDATE CONSTRAINT account_external_placements_target_chk")
	require.Contains(t, sqlText, "VALIDATE CONSTRAINT account_external_placement_conversions_room_chk")
}

func TestAccountShareRoomAccountsContractPerformsFreshCatchupBeforeRetiringLegacyLinks(t *testing.T) {
	sqlText := readAccountShareRoomAccountsMigration(t, accountShareRoomAccountsContractMigration)

	online, err := validateMigrationExecutionMode(accountShareRoomAccountsContractMigration, sqlText)
	require.NoError(t, err)
	require.True(t, online)
	require.Len(t, splitSQLStatements(sqlText), 3)

	require.Contains(t, sqlText, "only after traffic has switched")
	require.Contains(t, sqlText, "every legacy instance has stopped writing")
	require.Contains(t, sqlText, "'room_accounts_cutover'")
	require.Contains(t, sqlText, "LIMIT batch_size")
	require.Contains(t, sqlText, "Close the last race between the cutover high-water scan and the lock")
	require.Contains(t, sqlText, "IN SHARE ROW EXCLUSIVE MODE")
	require.Contains(t, sqlText, "cutover reconciliation missed a legacy room placement")

	lastCatchup := strings.Index(sqlText, "Close the last race between the cutover high-water scan and the lock")
	clearPlacement := strings.Index(sqlText, "UPDATE public.account_external_placements\n    SET listing_id = NULL")
	clearListing := strings.Index(sqlText, "UPDATE public.account_share_listings\n    SET account_id = NULL")
	require.NotEqual(t, -1, lastCatchup)
	require.Greater(t, clearPlacement, lastCatchup)
	require.Greater(t, clearListing, clearPlacement)

	require.Contains(t, sqlText, "placement_type = 'room'\n                AND listing_id IS NULL")
	require.Contains(t, sqlText, "CHECK (account_id IS NULL) NOT VALID")
	require.Contains(t, sqlText, "DROP CONSTRAINT IF EXISTS account_external_placements_room_fk")
	require.Contains(t, sqlText, "DROP CONSTRAINT IF EXISTS account_share_listings_legacy_account_fk")
	require.Contains(t, sqlText, "DROP TRIGGER IF EXISTS trg_account_share_legacy_placement_sync_room_account")
	require.Contains(t, sqlText, "DROP TRIGGER IF EXISTS trg_account_share_room_account_sync_legacy_placement")
	require.Contains(t, sqlText, "DROP TABLE IF EXISTS public.account_share_room_accounts_migration_progress")
	require.Contains(t, sqlText, "FROM public.account_share_room_accounts room_account")
	require.NotContains(t, strings.ToUpper(stripSQLLineComment(sqlText)), " OFFSET ")
}
