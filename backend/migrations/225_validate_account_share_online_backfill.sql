-- Validate the online backfill before any compatibility object is removed.
-- Every assertion is fail-fast so the old release can continue serving while
-- the operator fixes data and safely retries this migration.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10min';

DO $$
DECLARE
    incomplete_phases TEXT;
    unknown_cost_count BIGINT;
    online_index_count INTEGER;
    online_indexes_ready BOOLEAN;
BEGIN
    SELECT STRING_AGG(required.phase, ', ' ORDER BY required.phase)
    INTO incomplete_phases
    FROM (
        VALUES
            ('affiliate_ledger'),
            ('listings'),
            ('public_placements'),
            ('room_groups'),
            ('settlement_cost')
    ) AS required(phase)
    LEFT JOIN account_share_online_migration_progress progress
      ON progress.phase = required.phase
     AND progress.completed
     AND progress.last_id = progress.high_water_mark
    WHERE progress.phase IS NULL;
    IF incomplete_phases IS NOT NULL THEN
        RAISE EXCEPTION 'account-share online backfill phases are incomplete: %', incomplete_phases;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM user_affiliate_ledger ledger
        WHERE (
            ledger.action = 'accrue'
            AND ledger.source_order_id IS NULL
            AND EXISTS (
                SELECT 1
                FROM user_balance_ledger balance_entry
                WHERE balance_entry.user_id = ledger.user_id
                  AND balance_entry.direction = 'credit'
                  AND balance_entry.reason = 'invite_share_income'
                  AND balance_entry.amount = ledger.amount
                  AND balance_entry.created_at = ledger.created_at
                  AND COALESCE(balance_entry.metadata->>'consumer_user_id', '') ~ '^[0-9]+$'
                  AND (balance_entry.metadata->>'consumer_user_id')::bigint = ledger.source_user_id
            )
        ) OR (
            ledger.action = 'reverse'
            AND EXISTS (
                SELECT 1
                FROM user_balance_ledger balance_entry
                WHERE balance_entry.user_id = ledger.user_id
                  AND balance_entry.direction = 'debit'
                  AND balance_entry.reason = 'account_share_mode_invite_waiver_refund'
                  AND balance_entry.amount = ledger.amount
                  AND balance_entry.created_at = ledger.created_at
                  AND COALESCE(balance_entry.metadata->>'consumer_user_id', '') ~ '^[0-9]+$'
                  AND (balance_entry.metadata->>'consumer_user_id')::bigint = ledger.source_user_id
            )
        )
        LIMIT 1
    ) THEN
        RAISE EXCEPTION 'account-share affiliate ledger backfill still has compatible legacy rows';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM account_share_listings listing
        WHERE listing.deleted_at IS NULL
          AND (
              listing.room_name IS NULL
              OR listing.platform IS NULL
              OR listing.account_level IS NULL
              OR BTRIM(listing.room_name) = ''
              OR BTRIM(listing.platform) = ''
              OR BTRIM(listing.account_level) = ''
          )
        LIMIT 1
    ) THEN
        RAISE EXCEPTION 'account-share room identity backfill is incomplete';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM accounts account
        JOIN account_share_listings listing
          ON listing.account_id = account.id
         AND listing.deleted_at IS NULL
        WHERE account.deleted_at IS NULL
          AND account.share_mode = 'public'
        LIMIT 1
    ) THEN
        RAISE EXCEPTION 'an account cannot be placed in both public pool and account-share room';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM accounts account
        WHERE account.deleted_at IS NULL
          AND account.owner_user_id IS NOT NULL
          AND account.share_mode = 'public'
          AND account.share_status <> 'approved'
        LIMIT 1
    ) THEN
        RAISE EXCEPTION 'public account state requires an explicit operator decision before placement migration';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM account_share_listings listing
        WHERE listing.deleted_at IS NULL
          AND listing.account_id IS NOT NULL
          AND NOT EXISTS (
              SELECT 1
              FROM account_external_placements placement
              WHERE placement.account_id = listing.account_id
                AND placement.listing_id = listing.id
                AND placement.placement_type = 'room'
          )
        LIMIT 1
    ) THEN
        RAISE EXCEPTION 'legacy room placement backfill is incomplete';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM accounts account
        WHERE account.deleted_at IS NULL
          AND account.owner_user_id IS NOT NULL
          AND account.share_mode = 'public'
          AND account.share_status = 'approved'
          AND NOT EXISTS (
              SELECT 1
              FROM account_external_placements placement
              WHERE placement.account_id = account.id
                AND placement.placement_type = 'public_pool'
          )
        LIMIT 1
    ) THEN
        RAISE EXCEPTION 'approved public account placement backfill is incomplete';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM account_external_placements placement
        LEFT JOIN accounts account ON account.id = placement.account_id
        WHERE account.id IS NULL
           OR placement.owner_user_id IS DISTINCT FROM account.owner_user_id
           OR placement.platform IS DISTINCT FROM account.platform
           OR placement.account_level IS DISTINCT FROM account.account_level
        LIMIT 1
    ) THEN
        RAISE EXCEPTION 'external placement identity does not match its account';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM account_share_memberships membership
        WHERE membership.deleted_at IS NULL
          AND membership.status IN ('active', 'queued')
          AND NOT EXISTS (
              SELECT 1
              FROM account_external_placements placement
              WHERE placement.account_id = membership.account_id
                AND placement.listing_id = membership.listing_id
                AND placement.placement_type = 'room'
                AND placement.state IN ('active', 'draining')
          )
        LIMIT 1
    ) THEN
        RAISE EXCEPTION 'active account-share membership has no matching room placement';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM account_external_placements placement
        WHERE placement.placement_type = 'room'
          AND (
              (
                  SELECT COUNT(*)
                  FROM groups private_group
                  WHERE private_group.deleted_at IS NULL
                    AND private_group.status = 'active'
                    AND private_group.scope = 'user_private'
                    AND private_group.owner_user_id = placement.owner_user_id
                    AND private_group.platform = placement.platform
                    AND COALESCE(private_group.subscription_type, '') <> 'none'
              ) <> 1
              OR (
                  SELECT COUNT(*)
                  FROM account_share_mode_groups mode_group
                  WHERE mode_group.platform = placement.platform
              ) <> 1
          )
        LIMIT 1
    ) THEN
        RAISE EXCEPTION 'room placement group topology is ambiguous or incomplete';
    END IF;

    SELECT COUNT(*)
    INTO unknown_cost_count
    FROM account_share_mode_settlement_entries
    WHERE account_cost IS NULL;
    IF unknown_cost_count <> 0 THEN
        RAISE EXCEPTION
            'account-share settlement account cost remains unknown for % rows',
            unknown_cost_count
            USING HINT = 'Restore the matching usage logs or explicitly resolve every unknown row before retrying.';
    END IF;

    SELECT COUNT(*), COALESCE(BOOL_AND(index_state.indisvalid AND index_state.indisready), FALSE)
    INTO online_index_count, online_indexes_ready
    FROM pg_class index_relation
    JOIN pg_index index_state ON index_state.indexrelid = index_relation.oid
    WHERE index_relation.relname IN (
        'uq_accounts_owner_identity',
        'uq_accounts_room_identity',
        'idx_user_affiliate_ledger_share_income'
    );
    IF online_index_count <> 3 OR NOT online_indexes_ready THEN
        RAISE EXCEPTION 'one or more account-share online indexes are missing or invalid';
    END IF;
END
$$;

ALTER TABLE account_share_mode_settlement_entries
    VALIDATE CONSTRAINT account_share_mode_settlement_policy_fk;
ALTER TABLE account_share_mode_settlement_entries
    VALIDATE CONSTRAINT account_share_mode_settlement_inviter_fk;
ALTER TABLE account_share_mode_settlement_entries
    VALIDATE CONSTRAINT account_share_mode_settlement_reversal_fk;
ALTER TABLE account_share_mode_settlement_entries
    VALIDATE CONSTRAINT account_share_mode_settlement_invite_amounts_chk;
ALTER TABLE account_share_mode_settlement_entries
    VALIDATE CONSTRAINT account_share_mode_settlement_account_cost_nonnegative_chk;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_mode_settlement_account_cost_present_chk'
          AND conrelid = 'account_share_mode_settlement_entries'::regclass
    ) THEN
        ALTER TABLE account_share_mode_settlement_entries
            ADD CONSTRAINT account_share_mode_settlement_account_cost_present_chk
            CHECK (account_cost IS NOT NULL) NOT VALID;
    END IF;
END
$$;

ALTER TABLE account_share_mode_settlement_entries
    VALIDATE CONSTRAINT account_share_mode_settlement_account_cost_present_chk;
ALTER TABLE account_share_listings
    VALIDATE CONSTRAINT account_share_listings_legacy_account_fk;

CREATE OR REPLACE FUNCTION validate_account_external_placement_account_identity()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM accounts account
        WHERE account.id = NEW.account_id
          AND account.owner_user_id = NEW.owner_user_id
          AND account.platform = NEW.platform
          AND account.account_level = NEW.account_level
    ) THEN
        RAISE EXCEPTION 'external placement identity must match its account'
            USING
                ERRCODE = '23514',
                CONSTRAINT = 'account_external_placements_account_identity_chk';
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_validate_account_external_placement_account_identity
    ON account_external_placements;
CREATE CONSTRAINT TRIGGER trg_validate_account_external_placement_account_identity
AFTER INSERT OR UPDATE OF account_id, owner_user_id, platform, account_level
ON account_external_placements
DEFERRABLE INITIALLY IMMEDIATE
FOR EACH ROW
EXECUTE FUNCTION validate_account_external_placement_account_identity();

CREATE OR REPLACE FUNCTION reconcile_account_external_placement_account_identity()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
DECLARE
    placement account_external_placements%ROWTYPE;
BEGIN
    SELECT *
    INTO placement
    FROM account_external_placements
    WHERE account_id = NEW.id
    FOR UPDATE;
    IF NOT FOUND THEN
        RETURN NEW;
    END IF;
    IF NEW.owner_user_id IS DISTINCT FROM OLD.owner_user_id
       OR NEW.platform IS DISTINCT FROM OLD.platform THEN
        RAISE EXCEPTION 'convert the account to private before changing its owner or platform'
            USING
                ERRCODE = '23514',
                CONSTRAINT = 'account_external_placement_identity_change_chk';
    END IF;
    IF NEW.account_level IS DISTINCT FROM OLD.account_level THEN
        IF placement.placement_type = 'room' THEN
            RAISE EXCEPTION 'convert the account out of its room before changing account level'
                USING
                    ERRCODE = '23514',
                    CONSTRAINT = 'account_external_placement_room_level_change_chk';
        END IF;
        RAISE EXCEPTION 'convert the account out of the public pool before changing account level'
            USING
                ERRCODE = '23514',
                CONSTRAINT = 'account_external_placement_level_change_chk';
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_reconcile_account_external_placement_account_identity
    ON accounts;
CREATE TRIGGER trg_reconcile_account_external_placement_account_identity
AFTER UPDATE OF owner_user_id, platform, account_level
ON accounts
FOR EACH ROW
WHEN (
    OLD.owner_user_id IS DISTINCT FROM NEW.owner_user_id
    OR OLD.platform IS DISTINCT FROM NEW.platform
    OR OLD.account_level IS DISTINCT FROM NEW.account_level
)
EXECUTE FUNCTION reconcile_account_external_placement_account_identity();

CREATE OR REPLACE FUNCTION validate_account_share_membership_room_account()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF NEW.deleted_at IS NULL
       AND NEW.status IN ('active', 'queued')
       AND NOT EXISTS (
           SELECT 1
           FROM account_external_placements placement
           WHERE placement.account_id = NEW.account_id
             AND placement.listing_id = NEW.listing_id
             AND placement.placement_type = 'room'
             AND placement.state IN ('active', 'draining')
       ) THEN
        RAISE EXCEPTION 'active or queued account-share membership account must belong to its room'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_validate_account_share_membership_room_account
    ON account_share_memberships;
CREATE CONSTRAINT TRIGGER trg_validate_account_share_membership_room_account
AFTER INSERT OR UPDATE OF listing_id, account_id, status, deleted_at
ON account_share_memberships
DEFERRABLE INITIALLY IMMEDIATE
FOR EACH ROW
EXECUTE FUNCTION validate_account_share_membership_room_account();

CREATE OR REPLACE FUNCTION validate_room_placement_memberships_before_removal()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF OLD.placement_type = 'room'
       AND OLD.listing_id IS NOT NULL
       AND (
           TG_OP = 'DELETE'
           OR NEW.placement_type <> 'room'
           OR NEW.listing_id IS DISTINCT FROM OLD.listing_id
       )
       AND EXISTS (
           SELECT 1
           FROM account_share_memberships membership
           WHERE membership.listing_id = OLD.listing_id
             AND membership.account_id = OLD.account_id
             AND membership.status IN ('active', 'queued')
             AND membership.deleted_at IS NULL
       )
       AND NOT EXISTS (
           SELECT 1
           FROM account_external_placements placement
           WHERE placement.account_id = OLD.account_id
             AND placement.listing_id = OLD.listing_id
             AND placement.placement_type = 'room'
             AND placement.state IN ('active', 'draining')
       ) THEN
        RAISE EXCEPTION 'room placement cannot be removed while active or queued memberships still reference it'
            USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_validate_room_placement_memberships_before_removal
    ON account_external_placements;
CREATE CONSTRAINT TRIGGER trg_validate_room_placement_memberships_before_removal
AFTER DELETE OR UPDATE OF placement_type, listing_id
ON account_external_placements
DEFERRABLE INITIALLY IMMEDIATE
FOR EACH ROW
EXECUTE FUNCTION validate_room_placement_memberships_before_removal();
