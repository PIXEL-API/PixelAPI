-- The operator explicitly chose a fast zero-value resolution for legacy
-- account-share settlement rows whose source usage logs are no longer
-- available. Process the rows in committed batches so the old release can
-- continue serving throughout the repair.
CREATE OR REPLACE PROCEDURE account_share_online_resolve_unknown_cost_zero()
LANGUAGE plpgsql
AS $procedure$
DECLARE
    batch_count INTEGER;
BEGIN
    LOOP
        PERFORM set_config('search_path', 'pg_catalog, public', TRUE);
        PERFORM set_config('lock_timeout', '2s', TRUE);
        PERFORM set_config('statement_timeout', '5min', TRUE);

        WITH target_batch AS (
            SELECT settlement.id
            FROM account_share_mode_settlement_entries settlement
            WHERE settlement.account_cost IS NULL
            ORDER BY settlement.id
            LIMIT 20000
            FOR UPDATE OF settlement
        )
        UPDATE account_share_mode_settlement_entries settlement
        SET account_cost = 0
        FROM target_batch
        WHERE settlement.id = target_batch.id;
        GET DIAGNOSTICS batch_count = ROW_COUNT;

        COMMIT;
        EXIT WHEN batch_count = 0;
    END LOOP;
END
$procedure$;

CALL account_share_online_resolve_unknown_cost_zero();

DROP PROCEDURE IF EXISTS account_share_online_resolve_unknown_cost_zero();
