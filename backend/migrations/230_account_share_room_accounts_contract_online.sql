-- Contract phase. Apply this migration only after traffic has switched to the
-- new release and every legacy instance has stopped writing. The procedure
-- performs a fresh resumable catch-up plus a locked final reconciliation; it
-- never relies solely on migration 228's earlier snapshot.
CREATE OR REPLACE PROCEDURE account_share_room_accounts_contract()
LANGUAGE plpgsql
AS $procedure$
DECLARE
    batch_size CONSTANT INTEGER := 2000;
    cursor_id BIGINT;
    high_water BIGINT;
    batch_count INTEGER;
    cutover_complete BOOLEAN;
BEGIN
    PERFORM set_config('search_path', 'pg_catalog, public, pg_temp', FALSE);

    CREATE TABLE IF NOT EXISTS public.account_share_room_accounts_migration_progress (
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

    CREATE TEMP TABLE IF NOT EXISTS account_share_room_accounts_cutover_batch (
        account_id BIGINT PRIMARY KEY
    ) ON COMMIT DELETE ROWS;

    SELECT completed
    INTO cutover_complete
    FROM public.account_share_room_accounts_migration_progress
    WHERE phase = 'room_accounts_cutover';

    INSERT INTO public.account_share_room_accounts_migration_progress (
        phase,
        last_id,
        high_water_mark,
        completed,
        updated_at
    )
    SELECT
        'room_accounts_cutover',
        0,
        COALESCE(MAX(account_id), 0),
        FALSE,
        NOW()
    FROM public.account_external_placements
    WHERE placement_type = 'room'
      AND listing_id IS NOT NULL
    ON CONFLICT (phase) DO UPDATE
    SET last_id = CASE
            WHEN COALESCE(cutover_complete, FALSE) THEN 0
            ELSE account_share_room_accounts_migration_progress.last_id
        END,
        high_water_mark = CASE
            WHEN COALESCE(cutover_complete, FALSE)
                THEN EXCLUDED.high_water_mark
            ELSE GREATEST(
                account_share_room_accounts_migration_progress.high_water_mark,
                EXCLUDED.high_water_mark
            )
        END,
        completed = FALSE,
        updated_at = NOW();
    COMMIT;

    LOOP
        PERFORM set_config('lock_timeout', '2s', TRUE);
        PERFORM set_config('statement_timeout', '5min', TRUE);

        SELECT last_id, high_water_mark
        INTO cursor_id, high_water
        FROM public.account_share_room_accounts_migration_progress
        WHERE phase = 'room_accounts_cutover'
        FOR UPDATE;

        INSERT INTO account_share_room_accounts_cutover_batch (account_id)
        SELECT placement.account_id
        FROM public.account_external_placements placement
        WHERE placement.account_id > cursor_id
          AND placement.account_id <= high_water
          AND placement.placement_type = 'room'
          AND placement.listing_id IS NOT NULL
        ORDER BY placement.account_id
        LIMIT batch_size
        FOR UPDATE OF placement;
        GET DIAGNOSTICS batch_count = ROW_COUNT;

        IF batch_count = 0 THEN
            UPDATE public.account_share_room_accounts_migration_progress
            SET last_id = high_water_mark,
                completed = TRUE,
                updated_at = NOW()
            WHERE phase = 'room_accounts_cutover';
            COMMIT;
            EXIT;
        END IF;

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
        SELECT
            placement.account_id,
            placement.listing_id,
            placement.owner_user_id,
            placement.platform,
            placement.account_level,
            placement.state,
            placement.priority,
            placement.version,
            placement.created_at,
            placement.updated_at
        FROM public.account_external_placements placement
        JOIN account_share_room_accounts_cutover_batch batch
          ON batch.account_id = placement.account_id
        WHERE placement.placement_type = 'room'
          AND placement.listing_id IS NOT NULL
        ON CONFLICT (account_id) DO UPDATE
        SET listing_id = EXCLUDED.listing_id,
            owner_user_id = EXCLUDED.owner_user_id,
            platform = EXCLUDED.platform,
            account_level = EXCLUDED.account_level,
            state = EXCLUDED.state,
            priority = EXCLUDED.priority,
            version = EXCLUDED.version,
            updated_at = EXCLUDED.updated_at;

        UPDATE public.account_share_room_accounts_migration_progress
        SET last_id = (
                SELECT MAX(account_id)
                FROM account_share_room_accounts_cutover_batch
            ),
            updated_at = NOW()
        WHERE phase = 'room_accounts_cutover';
        COMMIT;
    END LOOP;

    PERFORM set_config('lock_timeout', '2s', TRUE);
    PERFORM set_config('statement_timeout', '30min', TRUE);

    LOCK TABLE
        public.account_external_placements,
        public.account_share_room_accounts,
        public.account_share_listings
    IN SHARE ROW EXCLUSIVE MODE;

    -- Close the last race between the cutover high-water scan and the lock.
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
    SELECT
        placement.account_id,
        placement.listing_id,
        placement.owner_user_id,
        placement.platform,
        placement.account_level,
        placement.state,
        placement.priority,
        placement.version,
        placement.created_at,
        placement.updated_at
    FROM public.account_external_placements placement
    WHERE placement.placement_type = 'room'
      AND placement.listing_id IS NOT NULL
    ON CONFLICT (account_id) DO UPDATE
    SET listing_id = EXCLUDED.listing_id,
        owner_user_id = EXCLUDED.owner_user_id,
        platform = EXCLUDED.platform,
        account_level = EXCLUDED.account_level,
        state = EXCLUDED.state,
        priority = EXCLUDED.priority,
        version = EXCLUDED.version,
        updated_at = EXCLUDED.updated_at;

    IF EXISTS (
        SELECT 1
        FROM public.account_external_placements placement
        WHERE placement.placement_type = 'room'
          AND placement.listing_id IS NOT NULL
          AND NOT EXISTS (
              SELECT 1
              FROM public.account_share_room_accounts room_account
              WHERE room_account.account_id = placement.account_id
                AND room_account.listing_id = placement.listing_id
                AND room_account.owner_user_id = placement.owner_user_id
                AND room_account.platform = placement.platform
                AND room_account.account_level = placement.account_level
          )
        LIMIT 1
    ) THEN
        RAISE EXCEPTION
            'cutover reconciliation missed a legacy room placement';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM public.account_share_room_accounts room_account
        WHERE NOT EXISTS (
            SELECT 1
            FROM public.account_external_placements placement
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
            'room account lost platform account mode eligibility before cutover';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM public.account_share_memberships membership
        WHERE membership.deleted_at IS NULL
          AND membership.status IN ('active', 'queued')
          AND NOT EXISTS (
              SELECT 1
              FROM public.account_share_room_accounts room_account
              WHERE room_account.account_id = membership.account_id
                AND room_account.listing_id = membership.listing_id
                AND room_account.state IN ('active', 'draining')
          )
        LIMIT 1
    ) THEN
        RAISE EXCEPTION
            'active account-share membership has no room account at cutover';
    END IF;

    DROP TRIGGER IF EXISTS trg_account_share_legacy_placement_sync_room_account
        ON public.account_external_placements;
    DROP TRIGGER IF EXISTS trg_account_share_room_account_sync_legacy_placement
        ON public.account_share_room_accounts;
    DROP TRIGGER IF EXISTS trg_validate_room_placement_memberships_before_removal
        ON public.account_external_placements;

    UPDATE public.account_external_placements
    SET listing_id = NULL,
        updated_at = NOW()
    WHERE placement_type = 'room'
      AND listing_id IS NOT NULL;

    UPDATE public.account_share_listings
    SET account_id = NULL,
        updated_at = NOW()
    WHERE account_id IS NOT NULL;

    ALTER TABLE public.account_external_placements
        DROP CONSTRAINT IF EXISTS account_external_placements_target_chk;

    ALTER TABLE public.account_external_placements
        ADD CONSTRAINT account_external_placements_target_chk
        CHECK (
            (
                placement_type = 'room'
                AND listing_id IS NULL
                AND public_group_id IS NULL
            )
            OR
            (
                placement_type = 'public_pool'
                AND listing_id IS NULL
                AND public_group_id IS NOT NULL
            )
        ) NOT VALID;

    ALTER TABLE public.account_external_placements
        VALIDATE CONSTRAINT account_external_placements_target_chk;

    ALTER TABLE public.account_external_placements
        DROP CONSTRAINT IF EXISTS account_external_placements_room_fk;

    ALTER TABLE public.account_share_listings
        DROP CONSTRAINT IF EXISTS account_share_listings_legacy_account_fk;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_catalog.pg_constraint
        WHERE conname = 'account_share_listings_account_id_retired_chk'
          AND conrelid = 'public.account_share_listings'::regclass
    ) THEN
        ALTER TABLE public.account_share_listings
            ADD CONSTRAINT account_share_listings_account_id_retired_chk
            CHECK (account_id IS NULL) NOT VALID;
    END IF;

    ALTER TABLE public.account_share_listings
        VALIDATE CONSTRAINT account_share_listings_account_id_retired_chk;

    CREATE OR REPLACE FUNCTION public.validate_account_share_membership_room_account()
    RETURNS TRIGGER
    LANGUAGE plpgsql
    SET search_path = pg_catalog, public
    AS $function$
    BEGIN
        IF NEW.deleted_at IS NULL
           AND NEW.status IN ('active', 'queued')
           AND NOT EXISTS (
               SELECT 1
               FROM public.account_share_room_accounts room_account
               WHERE room_account.account_id = NEW.account_id
                 AND room_account.listing_id = NEW.listing_id
                 AND room_account.state IN ('active', 'draining')
           ) THEN
            RAISE EXCEPTION
                'active or queued account-share membership account must belong to its room'
                USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END
    $function$;

    DROP FUNCTION IF EXISTS public.account_share_legacy_placement_sync_room_account();
    DROP FUNCTION IF EXISTS public.account_share_room_account_sync_legacy_placement();
    DROP FUNCTION IF EXISTS public.validate_room_placement_memberships_before_removal();

    DROP INDEX IF EXISTS public.idx_account_external_placements_room;

    COMMENT ON COLUMN public.account_external_placements.listing_id
        IS 'Retired room linkage; room placement now records platform-mode eligibility only and listing_id must be null';
    COMMENT ON COLUMN public.account_share_listings.account_id
        IS 'Retired single-account compatibility column; room membership is stored in account_share_room_accounts';

    DROP TABLE IF EXISTS public.account_share_room_accounts_migration_progress;

    ANALYZE public.account_share_room_accounts;
    ANALYZE public.account_external_placements;
    ANALYZE public.account_share_listings;
    COMMIT;
END
$procedure$;

CALL account_share_room_accounts_contract();

DROP PROCEDURE IF EXISTS account_share_room_accounts_contract();
