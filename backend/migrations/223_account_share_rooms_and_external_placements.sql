-- Turn the legacy single-account listing into a room that can contain multiple
-- owned accounts. Private self-use remains implicit; only public-pool and room
-- placements are mutually exclusive.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '60s';

CREATE UNIQUE INDEX IF NOT EXISTS uq_account_share_rooms_owner_name_live
    ON account_share_listings(owner_user_id, LOWER(BTRIM(room_name)))
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_account_share_rooms_owner_identity
    ON account_share_listings(id, owner_user_id);

CREATE UNIQUE INDEX IF NOT EXISTS uq_account_share_rooms_identity
    ON account_share_listings(id, owner_user_id, platform, account_level);

CREATE TABLE IF NOT EXISTS account_external_placements (
    account_id BIGINT PRIMARY KEY,
    owner_user_id BIGINT NOT NULL,
    platform VARCHAR(50) NOT NULL,
    account_level VARCHAR(64) NOT NULL,
    placement_type VARCHAR(20) NOT NULL,
    listing_id BIGINT,
    public_group_id BIGINT REFERENCES groups(id) ON DELETE RESTRICT,
    state VARCHAR(20) NOT NULL DEFAULT 'active',
    priority INTEGER NOT NULL DEFAULT 50,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT account_external_placements_account_fk
        FOREIGN KEY (account_id)
        REFERENCES accounts(id)
        ON DELETE CASCADE,
    CONSTRAINT account_external_placements_room_fk
        FOREIGN KEY (listing_id, owner_user_id, platform, account_level)
        REFERENCES account_share_listings(id, owner_user_id, platform, account_level)
        ON DELETE CASCADE,
    CONSTRAINT account_external_placements_type_chk
        CHECK (placement_type IN ('public_pool', 'room')),
    CONSTRAINT account_external_placements_state_chk
        CHECK (state IN ('active', 'draining')),
    CONSTRAINT account_external_placements_target_chk
        CHECK (
            (
                placement_type = 'room'
                AND listing_id IS NOT NULL
                AND public_group_id IS NULL
            )
            OR
            (
                placement_type = 'public_pool'
                AND listing_id IS NULL
                AND public_group_id IS NOT NULL
            )
        )
);

CREATE INDEX IF NOT EXISTS idx_account_external_placements_room
    ON account_external_placements(listing_id, state, priority, account_id)
    WHERE placement_type = 'room';

CREATE INDEX IF NOT EXISTS idx_account_external_placements_owner
    ON account_external_placements(owner_user_id, placement_type, updated_at DESC);

CREATE TABLE IF NOT EXISTS account_external_placement_conversions (
    id BIGSERIAL PRIMARY KEY,
    owner_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    idempotency_key VARCHAR(128) NOT NULL,
    target_type VARCHAR(20) NOT NULL,
    target_listing_id BIGINT REFERENCES account_share_listings(id) ON DELETE RESTRICT,
    target_public_group_id BIGINT REFERENCES groups(id) ON DELETE RESTRICT,
    placement_version BIGINT NOT NULL,
    result JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT account_external_placement_conversions_target_chk
        CHECK (target_type IN ('private', 'public_pool', 'room')),
    CONSTRAINT account_external_placement_conversions_key_chk
        CHECK (BTRIM(idempotency_key) <> ''),
    CONSTRAINT account_external_placement_conversions_version_chk
        CHECK (placement_version > 0),
    CONSTRAINT account_external_placement_conversions_room_chk
        CHECK (
            (target_type = 'room' AND target_listing_id IS NOT NULL AND target_public_group_id IS NULL)
            OR
            (target_type = 'public_pool' AND target_listing_id IS NULL AND target_public_group_id IS NOT NULL)
            OR
            (target_type = 'private' AND target_listing_id IS NULL AND target_public_group_id IS NULL)
        ),
    CONSTRAINT account_external_placement_conversions_account_owner_fk
        FOREIGN KEY (account_id, owner_user_id)
        REFERENCES accounts(id, owner_user_id)
        ON DELETE RESTRICT,
    CONSTRAINT account_external_placement_conversions_room_owner_fk
        FOREIGN KEY (target_listing_id, owner_user_id)
        REFERENCES account_share_listings(id, owner_user_id)
        ON DELETE RESTRICT,
    CONSTRAINT account_external_placement_conversions_idempotency_uniq
        UNIQUE (owner_user_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_account_external_placement_conversions_account
    ON account_external_placement_conversions(account_id, created_at DESC);

CREATE OR REPLACE FUNCTION account_share_online_compat_listing_identity()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
DECLARE
    account_name VARCHAR(255);
    account_platform VARCHAR(50);
    account_level_value VARCHAR(64);
BEGIN
    IF NEW.account_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT account.name, account.platform, account.account_level
    INTO account_name, account_platform, account_level_value
    FROM public.accounts account
    WHERE account.id = NEW.account_id;
    IF NOT FOUND THEN
        RETURN NEW;
    END IF;
    NEW.room_name := COALESCE(NULLIF(BTRIM(NEW.room_name), ''), account_name);
    NEW.platform := COALESCE(NULLIF(BTRIM(NEW.platform), ''), account_platform);
    NEW.account_level := COALESCE(NULLIF(BTRIM(NEW.account_level), ''), account_level_value);
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_account_share_online_compat_listing_identity
    ON account_share_listings;

CREATE TRIGGER trg_account_share_online_compat_listing_identity
BEFORE INSERT OR UPDATE OF account_id, room_name, platform, account_level
ON account_share_listings
FOR EACH ROW
EXECUTE FUNCTION account_share_online_compat_listing_identity();

CREATE OR REPLACE FUNCTION account_share_online_compat_listing_placement()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
DECLARE
    account_priority INTEGER;
BEGIN
    IF TG_OP = 'UPDATE'
       AND OLD.account_id IS NOT NULL
       AND (
           NEW.account_id IS DISTINCT FROM OLD.account_id
           OR NEW.deleted_at IS NOT NULL
       ) THEN
        DELETE FROM public.account_external_placements
        WHERE account_id = OLD.account_id
          AND placement_type = 'room'
          AND listing_id = OLD.id;
    END IF;

    IF NEW.deleted_at IS NOT NULL OR NEW.account_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT COALESCE(priority, 50)
    INTO account_priority
    FROM public.accounts
    WHERE id = NEW.account_id;

    INSERT INTO public.account_external_placements (
        account_id, owner_user_id, platform, account_level, placement_type,
        listing_id, state, priority, version, created_at, updated_at
    )
    VALUES (
        NEW.account_id, NEW.owner_user_id, NEW.platform, NEW.account_level, 'room',
        NEW.id, 'active', COALESCE(account_priority, 50), 1, NEW.created_at, NEW.updated_at
    )
    ON CONFLICT (account_id) DO UPDATE
    SET owner_user_id = EXCLUDED.owner_user_id,
        platform = EXCLUDED.platform,
        account_level = EXCLUDED.account_level,
        listing_id = EXCLUDED.listing_id,
        state = EXCLUDED.state,
        priority = EXCLUDED.priority,
        updated_at = EXCLUDED.updated_at
    WHERE account_external_placements.placement_type = 'room'
      AND account_external_placements.listing_id = NEW.id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'account % already has a conflicting external placement', NEW.account_id
            USING ERRCODE = '23505';
    END IF;

    INSERT INTO public.account_groups (account_id, group_id, priority, created_at)
    SELECT NEW.account_id, private_group.id, 1, NOW()
    FROM public.groups private_group
    WHERE private_group.owner_user_id = NEW.owner_user_id
      AND private_group.platform = NEW.platform
      AND private_group.scope = 'user_private'
      AND private_group.status = 'active'
      AND private_group.deleted_at IS NULL
      AND COALESCE(private_group.subscription_type, '') <> 'none'
    ON CONFLICT (account_id, group_id) DO NOTHING;

    INSERT INTO public.account_groups (account_id, group_id, priority, created_at)
    SELECT NEW.account_id, mode_group.group_id, 1, NOW()
    FROM public.account_share_mode_groups mode_group
    WHERE mode_group.platform = NEW.platform
    ON CONFLICT (account_id, group_id) DO NOTHING;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_account_share_online_compat_listing_placement
    ON account_share_listings;

CREATE TRIGGER trg_account_share_online_compat_listing_placement
AFTER INSERT OR UPDATE OF account_id, owner_user_id, room_name, platform, account_level, deleted_at
ON account_share_listings
FOR EACH ROW
EXECUTE FUNCTION account_share_online_compat_listing_placement();

CREATE OR REPLACE FUNCTION account_share_online_compat_public_placement(account_key BIGINT)
RETURNS VOID
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
DECLARE
    source_account public.accounts%ROWTYPE;
    resolved_group_id BIGINT;
    resolved_group_count INTEGER;
BEGIN
    SELECT *
    INTO source_account
    FROM public.accounts
    WHERE id = account_key;
    IF NOT FOUND
       OR source_account.deleted_at IS NOT NULL
       OR source_account.owner_user_id IS NULL
       OR source_account.share_mode <> 'public'
       OR source_account.share_status <> 'approved' THEN
        DELETE FROM public.account_external_placements
        WHERE account_id = account_key
          AND placement_type = 'public_pool';
        RETURN;
    END IF;
    IF EXISTS (
        SELECT 1
        FROM public.account_external_placements
        WHERE account_id = account_key
          AND placement_type = 'room'
    ) THEN
        RETURN;
    END IF;

    SELECT COUNT(*), MIN(public_group.id)
    INTO resolved_group_count, resolved_group_id
    FROM public.account_groups account_group
    JOIN public.groups public_group ON public_group.id = account_group.group_id
    WHERE account_group.account_id = account_key
      AND public_group.deleted_at IS NULL
      AND public_group.status = 'active'
      AND public_group.scope = 'public'
      AND public_group.owner_user_id IS NULL
      AND public_group.platform = source_account.platform
      AND NOT EXISTS (
          SELECT 1
          FROM public.account_share_mode_groups mode_group
          WHERE mode_group.group_id = public_group.id
      );
    IF resolved_group_count <> 1 THEN
        RETURN;
    END IF;

    INSERT INTO public.account_external_placements (
        account_id, owner_user_id, platform, account_level, placement_type,
        public_group_id, state, priority, version, created_at, updated_at
    )
    VALUES (
        source_account.id, source_account.owner_user_id, source_account.platform,
        source_account.account_level, 'public_pool', resolved_group_id, 'active',
        source_account.priority, 1, source_account.created_at, source_account.updated_at
    )
    ON CONFLICT (account_id) DO UPDATE
    SET owner_user_id = EXCLUDED.owner_user_id,
        platform = EXCLUDED.platform,
        account_level = EXCLUDED.account_level,
        public_group_id = EXCLUDED.public_group_id,
        state = EXCLUDED.state,
        priority = EXCLUDED.priority,
        updated_at = EXCLUDED.updated_at
    WHERE account_external_placements.placement_type = 'public_pool';
END
$$;

CREATE OR REPLACE FUNCTION account_share_online_compat_public_account_trigger()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    PERFORM public.account_share_online_compat_public_placement(NEW.id);
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_account_share_online_compat_public_account
    ON accounts;

CREATE TRIGGER trg_account_share_online_compat_public_account
AFTER INSERT OR UPDATE OF owner_user_id, platform, account_level, share_mode, share_status, priority, deleted_at
ON accounts
FOR EACH ROW
EXECUTE FUNCTION account_share_online_compat_public_account_trigger();

CREATE OR REPLACE FUNCTION account_share_online_compat_public_group_trigger()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        PERFORM public.account_share_online_compat_public_placement(OLD.account_id);
    END IF;
    IF TG_OP <> 'DELETE' THEN
        PERFORM public.account_share_online_compat_public_placement(NEW.account_id);
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_account_share_online_compat_public_group
    ON account_groups;

CREATE TRIGGER trg_account_share_online_compat_public_group
AFTER INSERT OR UPDATE OF account_id, group_id OR DELETE
ON account_groups
FOR EACH ROW
EXECUTE FUNCTION account_share_online_compat_public_group_trigger();

COMMENT ON TABLE account_external_placements
    IS 'An owned account has at most one external placement; private self-use is implicit and always available';
COMMENT ON COLUMN account_share_listings.room_name
    IS 'Room-level display name; member account names remain on accounts';
