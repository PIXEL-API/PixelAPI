-- Expand the account-share lifecycle schema without deleting historical data.
-- Keep the legacy "disabled" state writable until the later contract release.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE account_share_memberships
    ADD COLUMN IF NOT EXISTS queue_expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS ending_operation_id UUID;

ALTER TABLE account_share_room_operations
    ADD COLUMN IF NOT EXISTS membership_id BIGINT;

ALTER TABLE account_share_listings
    DROP CONSTRAINT IF EXISTS account_share_listings_status_chk;
ALTER TABLE account_share_listings
    ADD CONSTRAINT account_share_listings_status_chk
    CHECK (status IN ('validating', 'active', 'draining', 'paused', 'disabled', 'suspended')) NOT VALID;

UPDATE account_share_memberships
SET queue_expires_at = COALESCE(created_at, NOW()) + INTERVAL '2 hours'
WHERE status = 'queued'
  AND queue_expires_at IS NULL;

DO $$
BEGIN
    ALTER TABLE account_share_memberships
        DROP CONSTRAINT IF EXISTS account_share_memberships_status_chk;
    ALTER TABLE account_share_memberships
        ADD CONSTRAINT account_share_memberships_status_chk
        CHECK (status IN ('active', 'queued', 'ending', 'ended')) NOT VALID;

    ALTER TABLE account_share_memberships
        DROP CONSTRAINT IF EXISTS account_share_memberships_end_chk;
    ALTER TABLE account_share_memberships
        ADD CONSTRAINT account_share_memberships_end_chk
        CHECK (
            (status IN ('active', 'queued', 'ending') AND ended_at IS NULL)
            OR (status = 'ended' AND ended_at IS NOT NULL)
        ) NOT VALID;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_memberships_queue_expiry_chk'
          AND conrelid = 'account_share_memberships'::regclass
    ) THEN
        ALTER TABLE account_share_memberships
            ADD CONSTRAINT account_share_memberships_queue_expiry_chk
            CHECK (status <> 'queued' OR queue_expires_at IS NOT NULL) NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_memberships_ending_state_chk'
          AND conrelid = 'account_share_memberships'::regclass
    ) THEN
        ALTER TABLE account_share_memberships
            ADD CONSTRAINT account_share_memberships_ending_state_chk
            CHECK (
                status <> 'ending'
                OR (
                    ending_requested_at IS NOT NULL
                    AND settlement_status IN ('pending', 'processing', 'failed')
                )
            ) NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_account_share_room_operation_membership'
          AND conrelid = 'account_share_room_operations'::regclass
    ) THEN
        ALTER TABLE account_share_room_operations
            ADD CONSTRAINT fk_account_share_room_operation_membership
            FOREIGN KEY (membership_id)
            REFERENCES account_share_memberships(id)
            ON DELETE RESTRICT
            NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_room_operation_target_chk'
          AND conrelid = 'account_share_room_operations'::regclass
    ) THEN
        ALTER TABLE account_share_room_operations
            ADD CONSTRAINT account_share_room_operation_target_chk
            CHECK (
                (action = 'end_membership' AND membership_id IS NOT NULL)
                OR (action <> 'end_membership' AND membership_id IS NULL)
            ) NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_account_share_membership_ending_operation'
          AND conrelid = 'account_share_memberships'::regclass
    ) THEN
        ALTER TABLE account_share_memberships
            ADD CONSTRAINT fk_account_share_membership_ending_operation
            FOREIGN KEY (ending_operation_id)
            REFERENCES account_share_room_operations(id)
            ON DELETE RESTRICT
            NOT VALID;
    END IF;
END
$$;

ALTER TABLE account_share_listings
    VALIDATE CONSTRAINT account_share_listings_status_chk;

ALTER TABLE account_share_memberships
    VALIDATE CONSTRAINT account_share_memberships_status_chk;

ALTER TABLE account_share_memberships
    VALIDATE CONSTRAINT account_share_memberships_end_chk;

ALTER TABLE account_share_memberships
    VALIDATE CONSTRAINT account_share_memberships_queue_expiry_chk;

ALTER TABLE account_share_memberships
    VALIDATE CONSTRAINT account_share_memberships_ending_state_chk;

CREATE OR REPLACE FUNCTION validate_account_share_membership_room_account()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF NEW.deleted_at IS NULL
       AND NEW.status IN ('active', 'queued', 'ending')
       AND NOT EXISTS (
           SELECT 1
           FROM public.account_share_room_accounts room_account
           WHERE room_account.listing_id = NEW.listing_id
             AND room_account.account_id = NEW.account_id
             AND (
                 NEW.status = 'ending'
                 OR room_account.state IN ('active', 'draining')
             )
       ) THEN
        RAISE EXCEPTION 'live account-share membership account must belong to its room'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$$;

CREATE OR REPLACE FUNCTION validate_account_share_membership_listing_live()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF NEW.deleted_at IS NULL
       AND NEW.status IN ('active', 'queued', 'ending')
       AND NOT EXISTS (
           SELECT 1
           FROM public.account_share_listings listing
           WHERE listing.id = NEW.listing_id
             AND listing.deleted_at IS NULL
       ) THEN
        RAISE EXCEPTION 'live account-share membership cannot reference a deleted room'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_validate_account_share_membership_listing_live
    ON account_share_memberships;
CREATE CONSTRAINT TRIGGER trg_validate_account_share_membership_listing_live
AFTER INSERT OR UPDATE OF listing_id, status, deleted_at
ON account_share_memberships
DEFERRABLE INITIALLY IMMEDIATE
FOR EACH ROW
EXECUTE FUNCTION validate_account_share_membership_listing_live();

CREATE OR REPLACE FUNCTION prevent_account_share_room_delete_with_live_memberships()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF OLD.deleted_at IS NULL
       AND NEW.deleted_at IS NOT NULL
       AND EXISTS (
           SELECT 1
           FROM public.account_share_memberships membership
           WHERE membership.listing_id = NEW.id
             AND membership.status IN ('active', 'queued', 'ending')
             AND membership.deleted_at IS NULL
       ) THEN
        RAISE EXCEPTION 'account-share room still has live memberships'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_prevent_account_share_room_delete_with_live_memberships
    ON account_share_listings;
CREATE CONSTRAINT TRIGGER trg_prevent_account_share_room_delete_with_live_memberships
AFTER UPDATE OF deleted_at
ON account_share_listings
DEFERRABLE INITIALLY IMMEDIATE
FOR EACH ROW
EXECUTE FUNCTION prevent_account_share_room_delete_with_live_memberships();

COMMENT ON COLUMN account_share_memberships.queue_expires_at
    IS 'Expiry for queued admission; queued memberships do not reserve a consumer seat';

COMMENT ON COLUMN account_share_memberships.ending_operation_id
    IS 'Durable operation that fences new requests while an active membership is ending';

COMMENT ON COLUMN account_share_room_operations.membership_id
    IS 'Target membership for end_membership operations; null for room-level operations';
