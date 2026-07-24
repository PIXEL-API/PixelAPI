-- Final contract phase. Run only after the old application has drained and the
-- green release plus migration 225 have passed production verification.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '5min';

ALTER TABLE account_share_listings
    ALTER COLUMN room_name SET NOT NULL,
    ALTER COLUMN platform SET NOT NULL,
    ALTER COLUMN account_level SET NOT NULL;

ALTER TABLE account_share_mode_settlement_entries
    ALTER COLUMN account_cost SET NOT NULL,
    ALTER COLUMN account_cost SET DEFAULT 0;

DROP TRIGGER IF EXISTS trg_account_share_online_compat_affiliate_ledger
    ON user_affiliate_ledger;
DROP TRIGGER IF EXISTS trg_account_share_online_compat_settlement_cost
    ON account_share_mode_settlement_entries;
DROP TRIGGER IF EXISTS trg_account_share_online_compat_listing_identity
    ON account_share_listings;
DROP TRIGGER IF EXISTS trg_account_share_online_compat_listing_placement
    ON account_share_listings;
DROP TRIGGER IF EXISTS trg_account_share_online_compat_public_account
    ON accounts;
DROP TRIGGER IF EXISTS trg_account_share_online_compat_public_group
    ON account_groups;

DROP FUNCTION IF EXISTS account_share_online_compat_affiliate_ledger();
DROP FUNCTION IF EXISTS account_share_online_compat_settlement_cost();
DROP FUNCTION IF EXISTS account_share_online_compat_listing_identity();
DROP FUNCTION IF EXISTS account_share_online_compat_listing_placement();
DROP FUNCTION IF EXISTS account_share_online_compat_public_account_trigger();
DROP FUNCTION IF EXISTS account_share_online_compat_public_group_trigger();
DROP FUNCTION IF EXISTS account_share_online_compat_public_placement(BIGINT);

DROP TABLE IF EXISTS account_share_online_migration_progress;
DROP TABLE IF EXISTS account_share_mode_policies;

ANALYZE account_share_listings;
ANALYZE account_external_placements;
ANALYZE account_share_mode_settlement_entries;
ANALYZE user_affiliate_ledger;
ANALYZE user_affiliates;
