-- Pre-cutover validation. This phase remains compatible with the previous
-- release and may run while legacy instances still read and write listing_id.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30min';

DO $$
DECLARE
    progress_complete BOOLEAN;
BEGIN
    SELECT
        completed
        AND last_id = high_water_mark
    INTO progress_complete
    FROM account_share_room_accounts_migration_progress
    WHERE phase = 'legacy_room_placements';

    IF NOT COALESCE(progress_complete, FALSE) THEN
        RAISE EXCEPTION
            'account-share room-account online backfill is incomplete';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM account_external_placements placement
        WHERE placement.placement_type = 'room'
          AND placement.listing_id IS NOT NULL
          AND NOT EXISTS (
              SELECT 1
              FROM account_share_room_accounts room_account
              WHERE room_account.account_id = placement.account_id
                AND room_account.listing_id = placement.listing_id
                AND room_account.owner_user_id = placement.owner_user_id
                AND room_account.platform = placement.platform
                AND room_account.account_level = placement.account_level
          )
        LIMIT 1
    ) THEN
        RAISE EXCEPTION
            'legacy room placement is missing its independent room-account row';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM account_share_room_accounts room_account
        WHERE NOT EXISTS (
            SELECT 1
            FROM account_external_placements placement
            WHERE placement.account_id = room_account.account_id
              AND placement.owner_user_id = room_account.owner_user_id
              AND placement.platform = room_account.platform
              AND placement.account_level = room_account.account_level
              AND placement.placement_type = 'room'
              AND placement.state IN ('active', 'draining')
        )
        LIMIT 1
    ) THEN
        RAISE EXCEPTION
            'room account is missing platform account mode eligibility';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM account_share_memberships membership
        WHERE membership.deleted_at IS NULL
          AND membership.status IN ('active', 'queued')
          AND NOT EXISTS (
              SELECT 1
              FROM account_share_room_accounts room_account
              WHERE room_account.account_id = membership.account_id
                AND room_account.listing_id = membership.listing_id
                AND room_account.state IN ('active', 'draining')
          )
        LIMIT 1
    ) THEN
        RAISE EXCEPTION
            'active account-share membership has no independent room-account row';
    END IF;
END
$$;

ALTER TABLE account_external_placements
    VALIDATE CONSTRAINT account_external_placements_target_chk;

ALTER TABLE account_external_placement_conversions
    VALIDATE CONSTRAINT account_external_placement_conversions_room_chk;

ANALYZE account_share_room_accounts;
ANALYZE account_external_placements;
