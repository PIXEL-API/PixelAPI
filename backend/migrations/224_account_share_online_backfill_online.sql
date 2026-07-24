-- This migration intentionally runs outside one surrounding transaction. The
-- procedure commits every bounded primary-key batch and persists its cursor so
-- an interrupted production backfill resumes without OFFSET scans.
CREATE OR REPLACE PROCEDURE account_share_online_backfill()
LANGUAGE plpgsql
AS $procedure$
DECLARE
    batch_size CONSTANT INTEGER := 5000;
    cursor_id BIGINT;
    high_water BIGINT;
    batch_count INTEGER;
    insufficient_user_id BIGINT;
    listing_record RECORD;
    base_room_name TEXT;
    candidate_room_name TEXT;
    room_name_suffix TEXT;
    room_name_attempt INTEGER;
BEGIN
    PERFORM set_config('search_path', 'pg_catalog, public, pg_temp', FALSE);
    CREATE TEMP TABLE IF NOT EXISTS account_share_online_ledger_batch (
        id BIGINT PRIMARY KEY,
        user_id BIGINT NOT NULL,
        amount NUMERIC(20,8) NOT NULL,
        target_action VARCHAR(32) NOT NULL,
        quota_adjustment NUMERIC(20,8) NOT NULL
    ) ON COMMIT DELETE ROWS;
    CREATE TEMP TABLE IF NOT EXISTS account_share_online_id_batch (
        id BIGINT PRIMARY KEY
    ) ON COMMIT DELETE ROWS;

    INSERT INTO account_share_online_migration_progress (
        phase, last_id, high_water_mark, completed, updated_at
    )
    SELECT 'affiliate_ledger', 0, COALESCE(MAX(id), 0), FALSE, NOW()
    FROM user_affiliate_ledger
    ON CONFLICT (phase) DO NOTHING;
    COMMIT;

    LOOP
        PERFORM set_config('lock_timeout', '2s', TRUE);
        PERFORM set_config('statement_timeout', '5min', TRUE);
        SELECT last_id, high_water_mark
        INTO cursor_id, high_water
        FROM account_share_online_migration_progress
        WHERE phase = 'affiliate_ledger'
        FOR UPDATE;

        INSERT INTO account_share_online_ledger_batch (
            id, user_id, amount, target_action, quota_adjustment
        )
        SELECT
            ledger.id,
            ledger.user_id,
            ledger.amount,
            CASE ledger.action
                WHEN 'accrue' THEN 'share_accrue'
                ELSE 'share_reverse'
            END,
            CASE ledger.action
                WHEN 'accrue' THEN ledger.amount
                ELSE -ledger.amount
            END
        FROM user_affiliate_ledger ledger
        WHERE ledger.id > cursor_id
          AND ledger.id <= high_water
          AND (
              (
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
              )
              OR
              (
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
          )
        ORDER BY ledger.id
        LIMIT batch_size
        FOR UPDATE OF ledger;
        GET DIAGNOSTICS batch_count = ROW_COUNT;

        IF batch_count = 0 THEN
            UPDATE account_share_online_migration_progress
            SET last_id = high_water_mark,
                completed = TRUE,
                updated_at = NOW()
            WHERE phase = 'affiliate_ledger';
            COMMIT;
            EXIT;
        END IF;

        SELECT adjustment.user_id
        INTO insufficient_user_id
        FROM (
            SELECT user_id, SUM(quota_adjustment) AS amount
            FROM account_share_online_ledger_batch
            GROUP BY user_id
        ) adjustment
        LEFT JOIN user_affiliates affiliate ON affiliate.user_id = adjustment.user_id
        WHERE affiliate.user_id IS NULL
           OR adjustment.amount > affiliate.aff_history_quota
        ORDER BY adjustment.user_id
        LIMIT 1;
        IF FOUND THEN
            RAISE EXCEPTION
                'cannot isolate account-share inviter ledger for user_id %',
                insufficient_user_id
                USING ERRCODE = '23514';
        END IF;

        UPDATE user_affiliates affiliate
        SET aff_history_quota = affiliate.aff_history_quota - adjustment.amount,
            updated_at = NOW()
        FROM (
            SELECT user_id, SUM(quota_adjustment) AS amount
            FROM account_share_online_ledger_batch
            GROUP BY user_id
        ) adjustment
        WHERE affiliate.user_id = adjustment.user_id
          AND adjustment.amount <> 0;

        UPDATE user_affiliate_ledger ledger
        SET action = batch.target_action,
            updated_at = NOW()
        FROM account_share_online_ledger_batch batch
        WHERE ledger.id = batch.id;

        UPDATE account_share_online_migration_progress
        SET last_id = (SELECT MAX(id) FROM account_share_online_ledger_batch),
            updated_at = NOW()
        WHERE phase = 'affiliate_ledger';
        COMMIT;
    END LOOP;

    INSERT INTO account_share_online_migration_progress (
        phase, last_id, high_water_mark, completed, updated_at
    )
    SELECT 'listings', 0, COALESCE(MAX(id), 0), FALSE, NOW()
    FROM account_share_listings
    ON CONFLICT (phase) DO NOTHING;
    COMMIT;

    LOOP
        PERFORM set_config('lock_timeout', '2s', TRUE);
        PERFORM set_config('statement_timeout', '5min', TRUE);
        SELECT last_id, high_water_mark
        INTO cursor_id, high_water
        FROM account_share_online_migration_progress
        WHERE phase = 'listings'
        FOR UPDATE;

        INSERT INTO account_share_online_id_batch (id)
        SELECT listing.id
        FROM account_share_listings listing
        WHERE listing.id > cursor_id
          AND listing.id <= high_water
        ORDER BY listing.id
        LIMIT batch_size
        FOR UPDATE OF listing;
        GET DIAGNOSTICS batch_count = ROW_COUNT;
        IF batch_count = 0 THEN
            UPDATE account_share_online_migration_progress
            SET last_id = high_water_mark,
                completed = TRUE,
                updated_at = NOW()
            WHERE phase = 'listings';
            COMMIT;
            EXIT;
        END IF;

        FOR listing_record IN
            SELECT
                listing.id,
                listing.room_name,
                listing.platform,
                listing.account_level,
                listing.updated_at,
                account.name AS account_name,
                account.platform AS account_platform,
                account.account_level AS account_account_level,
                account.updated_at AS account_updated_at
            FROM account_share_listings listing
            JOIN account_share_online_id_batch batch ON batch.id = listing.id
            JOIN accounts account ON account.id = listing.account_id
            ORDER BY listing.id
        LOOP
            base_room_name := COALESCE(
                NULLIF(BTRIM(listing_record.room_name), ''),
                NULLIF(BTRIM(listing_record.account_name), ''),
                '房间'
            );
            candidate_room_name := base_room_name;
            room_name_attempt := 0;

            LOOP
                BEGIN
                    UPDATE account_share_listings
                    SET room_name = candidate_room_name,
                        platform = COALESCE(
                            NULLIF(BTRIM(listing_record.platform), ''),
                            listing_record.account_platform
                        ),
                        account_level = COALESCE(
                            NULLIF(BTRIM(listing_record.account_level), ''),
                            listing_record.account_account_level
                        ),
                        updated_at = GREATEST(
                            listing_record.updated_at,
                            listing_record.account_updated_at
                        )
                    WHERE id = listing_record.id;
                    EXIT;
                EXCEPTION
                    WHEN unique_violation THEN
                        room_name_attempt := room_name_attempt + 1;
                        IF room_name_attempt > 100 THEN
                            RAISE EXCEPTION
                                'cannot allocate a unique room name for listing_id %',
                                listing_record.id
                                USING ERRCODE = '23505';
                        END IF;
                        room_name_suffix := CASE
                            WHEN room_name_attempt = 1
                                THEN FORMAT(' ·%s', listing_record.id)
                            ELSE FORMAT(
                                ' ·%s-%s',
                                listing_record.id,
                                room_name_attempt
                            )
                        END;
                        candidate_room_name :=
                            LEFT(
                                base_room_name,
                                GREATEST(
                                    1,
                                    100 - CHAR_LENGTH(room_name_suffix)
                                )
                            )
                            || room_name_suffix;
                END;
            END LOOP;
        END LOOP;

        UPDATE account_share_online_migration_progress
        SET last_id = (SELECT MAX(id) FROM account_share_online_id_batch),
            updated_at = NOW()
        WHERE phase = 'listings';
        COMMIT;
    END LOOP;

    INSERT INTO account_share_online_migration_progress (
        phase, last_id, high_water_mark, completed, updated_at
    )
    SELECT 'public_placements', 0, COALESCE(MAX(id), 0), FALSE, NOW()
    FROM accounts
    ON CONFLICT (phase) DO NOTHING;
    COMMIT;

    LOOP
        PERFORM set_config('lock_timeout', '2s', TRUE);
        PERFORM set_config('statement_timeout', '5min', TRUE);
        SELECT last_id, high_water_mark
        INTO cursor_id, high_water
        FROM account_share_online_migration_progress
        WHERE phase = 'public_placements'
        FOR UPDATE;

        INSERT INTO account_share_online_id_batch (id)
        SELECT account.id
        FROM accounts account
        WHERE account.id > cursor_id
          AND account.id <= high_water
          AND account.deleted_at IS NULL
          AND account.owner_user_id IS NOT NULL
          AND account.share_mode = 'public'
          AND account.share_status = 'approved'
        ORDER BY account.id
        LIMIT batch_size
        FOR UPDATE OF account;
        GET DIAGNOSTICS batch_count = ROW_COUNT;
        IF batch_count = 0 THEN
            UPDATE account_share_online_migration_progress
            SET last_id = high_water_mark,
                completed = TRUE,
                updated_at = NOW()
            WHERE phase = 'public_placements';
            COMMIT;
            EXIT;
        END IF;

        PERFORM account_share_online_compat_public_placement(batch.id)
        FROM account_share_online_id_batch batch;

        UPDATE account_share_online_migration_progress
        SET last_id = (SELECT MAX(id) FROM account_share_online_id_batch),
            updated_at = NOW()
        WHERE phase = 'public_placements';
        COMMIT;
    END LOOP;

    INSERT INTO account_groups (account_id, group_id, priority, created_at)
    SELECT placement.account_id, private_group.id, 1, NOW()
    FROM account_external_placements placement
    JOIN groups private_group
      ON private_group.owner_user_id = placement.owner_user_id
     AND private_group.platform = placement.platform
     AND private_group.scope = 'user_private'
     AND private_group.status = 'active'
     AND private_group.deleted_at IS NULL
     AND COALESCE(private_group.subscription_type, '') <> 'none'
    WHERE placement.placement_type = 'room'
    ON CONFLICT (account_id, group_id) DO NOTHING;

    INSERT INTO account_groups (account_id, group_id, priority, created_at)
    SELECT placement.account_id, mode_group.group_id, 1, NOW()
    FROM account_external_placements placement
    JOIN account_share_mode_groups mode_group ON mode_group.platform = placement.platform
    WHERE placement.placement_type = 'room'
    ON CONFLICT (account_id, group_id) DO NOTHING;

    INSERT INTO account_share_online_migration_progress (
        phase, last_id, high_water_mark, completed, updated_at
    )
    VALUES ('room_groups', 1, 1, TRUE, NOW())
    ON CONFLICT (phase) DO UPDATE
    SET last_id = EXCLUDED.last_id,
        high_water_mark = EXCLUDED.high_water_mark,
        completed = EXCLUDED.completed,
        updated_at = EXCLUDED.updated_at;
    COMMIT;

    INSERT INTO account_share_online_migration_progress (
        phase, last_id, high_water_mark, completed, updated_at
    )
    SELECT 'settlement_cost', 0, COALESCE(MAX(id), 0), FALSE, NOW()
    FROM account_share_mode_settlement_entries
    ON CONFLICT (phase) DO NOTHING;
    COMMIT;

    LOOP
        PERFORM set_config('lock_timeout', '2s', TRUE);
        PERFORM set_config('statement_timeout', '5min', TRUE);
        SELECT last_id, high_water_mark
        INTO cursor_id, high_water
        FROM account_share_online_migration_progress
        WHERE phase = 'settlement_cost'
        FOR UPDATE;

        INSERT INTO account_share_online_id_batch (id)
        SELECT settlement.id
        FROM account_share_mode_settlement_entries settlement
        WHERE settlement.id > cursor_id
          AND settlement.id <= high_water
        ORDER BY settlement.id
        LIMIT batch_size
        FOR UPDATE OF settlement;
        GET DIAGNOSTICS batch_count = ROW_COUNT;
        IF batch_count = 0 THEN
            UPDATE account_share_online_migration_progress
            SET last_id = high_water_mark,
                completed = TRUE,
                updated_at = NOW()
            WHERE phase = 'settlement_cost';
            COMMIT;
            EXIT;
        END IF;

        UPDATE account_share_mode_settlement_entries settlement
        SET account_cost = CASE
                WHEN settlement.settlement_type = 'usage_request' THEN (
                    SELECT ROUND(
                        COALESCE(usage_log.account_stats_cost, usage_log.total_cost, 0)
                        * COALESCE(usage_log.account_rate_multiplier, 1),
                        10
                    )
                    FROM usage_logs usage_log
                    WHERE usage_log.id = settlement.usage_log_id
                )
                ELSE 0
            END
        FROM account_share_online_id_batch batch
        WHERE settlement.id = batch.id;

        UPDATE account_share_online_migration_progress
        SET last_id = (SELECT MAX(id) FROM account_share_online_id_batch),
            updated_at = NOW()
        WHERE phase = 'settlement_cost';
        COMMIT;
    END LOOP;
END
$procedure$;

CALL account_share_online_backfill();

DROP PROCEDURE IF EXISTS account_share_online_backfill();
