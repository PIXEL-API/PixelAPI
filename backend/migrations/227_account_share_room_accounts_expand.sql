-- Expand phase for separating platform-mode eligibility from room membership.
-- This migration is safe to apply while the previous release is still serving:
-- legacy listing_id writes are mirrored into account_share_room_accounts, while
-- new room-account writes are mirrored back to listing_id until contract.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE IF NOT EXISTS account_share_room_accounts (
    account_id BIGINT PRIMARY KEY,
    listing_id BIGINT NOT NULL,
    owner_user_id BIGINT NOT NULL,
    platform VARCHAR(50) NOT NULL,
    account_level VARCHAR(64) NOT NULL,
    state VARCHAR(20) NOT NULL DEFAULT 'active',
    priority INTEGER NOT NULL DEFAULT 50,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT account_share_room_accounts_room_identity_fk
        FOREIGN KEY (listing_id, owner_user_id, platform, account_level)
        REFERENCES account_share_listings(id, owner_user_id, platform, account_level)
        ON DELETE CASCADE,
    CONSTRAINT account_share_room_accounts_account_identity_fk
        FOREIGN KEY (account_id, owner_user_id, platform, account_level)
        REFERENCES accounts(id, owner_user_id, platform, account_level)
        ON DELETE CASCADE,
    CONSTRAINT account_share_room_accounts_state_chk
        CHECK (state IN ('active', 'draining')),
    CONSTRAINT account_share_room_accounts_version_chk
        CHECK (version > 0)
);

CREATE INDEX IF NOT EXISTS idx_account_share_room_accounts_listing
    ON account_share_room_accounts(listing_id, state, priority, account_id);

CREATE INDEX IF NOT EXISTS idx_account_share_room_accounts_owner_mode
    ON account_share_room_accounts(
        owner_user_id,
        platform,
        account_level,
        state,
        priority,
        account_id
    );

CREATE INDEX IF NOT EXISTS idx_account_external_placements_room_mode
    ON account_external_placements(
        owner_user_id,
        platform,
        account_level,
        state,
        priority,
        account_id
    )
    WHERE placement_type = 'room';

CREATE TABLE IF NOT EXISTS account_share_room_accounts_migration_progress (
    phase VARCHAR(64) PRIMARY KEY,
    last_id BIGINT NOT NULL DEFAULT 0,
    high_water_mark BIGINT NOT NULL DEFAULT 0,
    completed BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT account_share_room_accounts_migration_progress_bounds_chk
        CHECK (
            last_id >= 0
            AND high_water_mark >= 0
            AND last_id <= high_water_mark
        )
);

ALTER TABLE account_external_placements
    DROP CONSTRAINT IF EXISTS account_external_placements_target_chk;

ALTER TABLE account_external_placements
    ADD CONSTRAINT account_external_placements_target_chk
    CHECK (
        (
            placement_type = 'room'
            AND public_group_id IS NULL
        )
        OR
        (
            placement_type = 'public_pool'
            AND listing_id IS NULL
            AND public_group_id IS NOT NULL
        )
    ) NOT VALID;

ALTER TABLE account_external_placement_conversions
    DROP CONSTRAINT IF EXISTS account_external_placement_conversions_room_chk;

ALTER TABLE account_external_placement_conversions
    ADD CONSTRAINT account_external_placement_conversions_room_chk
    CHECK (
        (
            target_type = 'room'
            AND target_public_group_id IS NULL
        )
        OR
        (
            target_type = 'public_pool'
            AND target_listing_id IS NULL
            AND target_public_group_id IS NOT NULL
        )
        OR
        (
            target_type = 'private'
            AND target_listing_id IS NULL
            AND target_public_group_id IS NULL
        )
    ) NOT VALID;

CREATE OR REPLACE FUNCTION account_share_legacy_placement_sync_room_account()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.placement_type = 'room' AND OLD.listing_id IS NOT NULL THEN
            DELETE FROM public.account_share_room_accounts
            WHERE account_id = OLD.account_id
              AND listing_id = OLD.listing_id;
        END IF;
        RETURN OLD;
    END IF;

    IF TG_OP = 'UPDATE'
       AND OLD.placement_type = 'room'
       AND OLD.listing_id IS NOT NULL
       AND NEW.placement_type <> 'room' THEN
        DELETE FROM public.account_share_room_accounts
        WHERE account_id = OLD.account_id
          AND listing_id = OLD.listing_id;
    END IF;

    IF NEW.placement_type = 'room' AND NEW.listing_id IS NOT NULL THEN
        INSERT INTO public.account_share_room_accounts (
            account_id,
            listing_id,
            owner_user_id,
            platform,
            account_level,
            state,
            priority,
            version,
            created_at,
            updated_at
        )
        VALUES (
            NEW.account_id,
            NEW.listing_id,
            NEW.owner_user_id,
            NEW.platform,
            NEW.account_level,
            NEW.state,
            NEW.priority,
            NEW.version,
            NEW.created_at,
            NEW.updated_at
        )
        ON CONFLICT (account_id) DO UPDATE
        SET listing_id = EXCLUDED.listing_id,
            owner_user_id = EXCLUDED.owner_user_id,
            platform = EXCLUDED.platform,
            account_level = EXCLUDED.account_level,
            state = EXCLUDED.state,
            priority = EXCLUDED.priority,
            version = EXCLUDED.version,
            updated_at = EXCLUDED.updated_at;
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_account_share_legacy_placement_sync_room_account
    ON account_external_placements;

CREATE TRIGGER trg_account_share_legacy_placement_sync_room_account
AFTER INSERT OR UPDATE OF
    placement_type,
    listing_id,
    owner_user_id,
    platform,
    account_level,
    state,
    priority,
    version
OR DELETE
ON account_external_placements
FOR EACH ROW
EXECUTE FUNCTION account_share_legacy_placement_sync_room_account();

CREATE OR REPLACE FUNCTION account_share_room_account_sync_legacy_placement()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        UPDATE public.account_external_placements
        SET listing_id = NULL,
            updated_at = GREATEST(updated_at, NOW())
        WHERE account_id = OLD.account_id
          AND placement_type = 'room'
          AND listing_id = OLD.listing_id;
        RETURN OLD;
    END IF;

    UPDATE public.account_external_placements
    SET listing_id = NEW.listing_id,
        updated_at = GREATEST(updated_at, NEW.updated_at)
    WHERE account_id = NEW.account_id
      AND placement_type = 'room'
      AND listing_id IS DISTINCT FROM NEW.listing_id;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_account_share_room_account_sync_legacy_placement
    ON account_share_room_accounts;

CREATE TRIGGER trg_account_share_room_account_sync_legacy_placement
AFTER INSERT OR UPDATE OF
    listing_id,
    owner_user_id,
    platform,
    account_level,
    state,
    priority,
    version,
    updated_at
OR DELETE
ON account_share_room_accounts
FOR EACH ROW
EXECUTE FUNCTION account_share_room_account_sync_legacy_placement();

CREATE OR REPLACE FUNCTION validate_account_share_room_account_qualification()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM public.account_external_placements placement
        WHERE placement.account_id = NEW.account_id
          AND placement.owner_user_id = NEW.owner_user_id
          AND placement.platform = NEW.platform
          AND placement.account_level = NEW.account_level
          AND placement.placement_type = 'room'
          AND placement.state IN ('active', 'draining')
    ) THEN
        RAISE EXCEPTION
            'room account must remain eligible for its platform account mode'
            USING
                ERRCODE = '23514',
                CONSTRAINT = 'account_share_room_accounts_qualification_chk';
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_validate_account_share_room_account_qualification
    ON account_share_room_accounts;

CREATE CONSTRAINT TRIGGER trg_validate_account_share_room_account_qualification
AFTER INSERT OR UPDATE OF
    account_id,
    owner_user_id,
    platform,
    account_level,
    state
ON account_share_room_accounts
DEFERRABLE INITIALLY IMMEDIATE
FOR EACH ROW
EXECUTE FUNCTION validate_account_share_room_account_qualification();

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
           FROM public.account_share_room_accounts room_account
           WHERE room_account.account_id = NEW.account_id
             AND room_account.listing_id = NEW.listing_id
             AND room_account.state IN ('active', 'draining')
       )
       AND NOT EXISTS (
           SELECT 1
           FROM public.account_external_placements placement
           WHERE placement.account_id = NEW.account_id
             AND placement.listing_id = NEW.listing_id
             AND placement.placement_type = 'room'
             AND placement.state IN ('active', 'draining')
       ) THEN
        RAISE EXCEPTION
            'active or queued account-share membership account must belong to its room'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$$;

CREATE OR REPLACE FUNCTION validate_room_account_memberships_before_removal()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF (
        TG_OP = 'DELETE'
        OR NEW.listing_id IS DISTINCT FROM OLD.listing_id
        OR NEW.account_id IS DISTINCT FROM OLD.account_id
    )
       AND EXISTS (
           SELECT 1
           FROM public.account_share_memberships membership
           WHERE membership.listing_id = OLD.listing_id
             AND membership.account_id = OLD.account_id
             AND membership.status IN ('active', 'queued')
             AND membership.deleted_at IS NULL
       )
       AND NOT EXISTS (
           SELECT 1
           FROM public.account_share_room_accounts room_account
           WHERE room_account.listing_id = OLD.listing_id
             AND room_account.account_id = OLD.account_id
             AND room_account.state IN ('active', 'draining')
       ) THEN
        RAISE EXCEPTION
            'room account cannot be removed while active or queued memberships reference it'
            USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_validate_room_account_memberships_before_removal
    ON account_share_room_accounts;

CREATE CONSTRAINT TRIGGER trg_validate_room_account_memberships_before_removal
AFTER DELETE OR UPDATE OF listing_id, account_id
ON account_share_room_accounts
DEFERRABLE INITIALLY IMMEDIATE
FOR EACH ROW
EXECUTE FUNCTION validate_room_account_memberships_before_removal();

CREATE OR REPLACE FUNCTION validate_room_placement_memberships_before_removal()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF OLD.placement_type = 'room'
       AND (
           TG_OP = 'DELETE'
           OR NEW.placement_type <> 'room'
       )
       AND EXISTS (
           SELECT 1
           FROM public.account_share_memberships membership
           WHERE membership.account_id = OLD.account_id
             AND membership.status IN ('active', 'queued')
             AND membership.deleted_at IS NULL
       )
       AND NOT EXISTS (
           SELECT 1
           FROM public.account_share_room_accounts room_account
           JOIN public.account_share_memberships membership
             ON membership.listing_id = room_account.listing_id
            AND membership.account_id = room_account.account_id
           WHERE room_account.account_id = OLD.account_id
             AND room_account.state IN ('active', 'draining')
             AND membership.status IN ('active', 'queued')
             AND membership.deleted_at IS NULL
       ) THEN
        RAISE EXCEPTION
            'platform account mode cannot be removed while active or queued room memberships reference it'
            USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END
$$;

COMMENT ON TABLE account_share_room_accounts
    IS 'Independent room membership for owned platform-mode accounts; one account can belong to at most one room';

COMMENT ON COLUMN account_external_placements.listing_id
    IS 'Temporary legacy room linkage during online migration; room-mode eligibility does not require a listing';

COMMENT ON COLUMN account_external_placement_conversions.target_listing_id
    IS 'Optional legacy room target retained for audit; platform account mode conversion does not require a room';
