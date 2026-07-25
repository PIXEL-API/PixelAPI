-- Online backfill phase. The migration runner requires *_online.sql files to
-- contain exactly CREATE PROCEDURE, CALL, and DROP PROCEDURE statements.
-- Each primary-key batch commits independently and resumes from durable
-- progress without OFFSET scans.
CREATE OR REPLACE PROCEDURE account_share_room_accounts_backfill()
LANGUAGE plpgsql
AS $procedure$
DECLARE
    batch_size CONSTANT INTEGER := 2000;
    cursor_id BIGINT;
    high_water BIGINT;
    batch_count INTEGER;
BEGIN
    PERFORM set_config('search_path', 'pg_catalog, public, pg_temp', FALSE);

    CREATE TEMP TABLE IF NOT EXISTS account_share_room_accounts_id_batch (
        account_id BIGINT PRIMARY KEY
    ) ON COMMIT DELETE ROWS;

    INSERT INTO public.account_share_room_accounts_migration_progress (
        phase,
        last_id,
        high_water_mark,
        completed,
        updated_at
    )
    SELECT
        'legacy_room_placements',
        0,
        COALESCE(MAX(account_id), 0),
        FALSE,
        NOW()
    FROM public.account_external_placements
    WHERE placement_type = 'room'
      AND listing_id IS NOT NULL
    ON CONFLICT (phase) DO NOTHING;
    COMMIT;

    LOOP
        PERFORM set_config('lock_timeout', '2s', TRUE);
        PERFORM set_config('statement_timeout', '5min', TRUE);

        SELECT last_id, high_water_mark
        INTO cursor_id, high_water
        FROM public.account_share_room_accounts_migration_progress
        WHERE phase = 'legacy_room_placements'
        FOR UPDATE;

        INSERT INTO account_share_room_accounts_id_batch (account_id)
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
            WHERE phase = 'legacy_room_placements';
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
        JOIN account_share_room_accounts_id_batch batch
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
                FROM account_share_room_accounts_id_batch
            ),
            updated_at = NOW()
        WHERE phase = 'legacy_room_placements';
        COMMIT;
    END LOOP;
END
$procedure$;

CALL account_share_room_accounts_backfill();

DROP PROCEDURE IF EXISTS account_share_room_accounts_backfill();
