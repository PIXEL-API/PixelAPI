-- Remove the billing intent mechanism entirely. The two-phase durability layer is
-- replaced by direct synchronous usage recording (release 1.2.28).
--
-- Online (procedure-driven) on purpose: dropping account_share_request_billing_intents
-- needs ACCESS EXCLUSIVE on every table its foreign keys reference, including the hot
-- users / accounts / api_keys / usage_logs tables. Taking all of those locks in one
-- transaction is impossible under live traffic and would queue every request behind the
-- lock wait. This procedure detaches one constraint per transaction, so each lock is
-- held for microseconds, and a lost lock race only costs a retry of that one step.
--
-- Every step is idempotent and the table drops run last, so re-applying after a partial
-- run is safe.
CREATE OR REPLACE PROCEDURE account_share_drop_billing_intent_tables()
LANGUAGE plpgsql
AS $procedure$
DECLARE
    unsettled_with_usage BIGINT;
    stmt TEXT;
    detach_statements CONSTANT TEXT[] := ARRAY[
        'ALTER TABLE IF EXISTS public.account_share_request_billing_intents DROP CONSTRAINT IF EXISTS account_share_request_billing_intents_account_id_fkey',
        'ALTER TABLE IF EXISTS public.account_share_request_billing_intents DROP CONSTRAINT IF EXISTS account_share_request_billing_intents_api_key_id_fkey',
        'ALTER TABLE IF EXISTS public.account_share_request_billing_intents DROP CONSTRAINT IF EXISTS account_share_request_billing_intents_usage_log_id_fkey',
        'ALTER TABLE IF EXISTS public.account_share_request_billing_intents DROP CONSTRAINT IF EXISTS account_share_request_billing_intents_owner_user_id_fkey',
        'ALTER TABLE IF EXISTS public.account_share_request_billing_intents DROP CONSTRAINT IF EXISTS account_share_request_billing_intents_actor_user_id_fkey',
        'ALTER TABLE IF EXISTS public.account_share_request_billing_intents DROP CONSTRAINT IF EXISTS account_share_request_billing_intents_consumer_user_id_fkey',
        'ALTER TABLE IF EXISTS public.account_share_request_billing_intents DROP CONSTRAINT IF EXISTS fk_account_share_billing_intent_membership',
        'ALTER TABLE IF EXISTS public.account_share_request_billing_intents DROP CONSTRAINT IF EXISTS fk_account_share_billing_intent_revision',
        'ALTER TABLE IF EXISTS public.account_share_request_billing_intents DROP CONSTRAINT IF EXISTS fk_account_share_billing_intent_binding',
        'ALTER TABLE IF EXISTS public.account_share_request_billing_intents DROP CONSTRAINT IF EXISTS account_share_billing_intent_admin_waiver_fk',
        'ALTER TABLE IF EXISTS public.account_share_billing_intent_admin_waivers DROP CONSTRAINT IF EXISTS account_share_billing_intent_admin_waivers_intent_id_fkey',
        'ALTER TABLE IF EXISTS public.account_share_billing_intent_admin_waivers DROP CONSTRAINT IF EXISTS account_share_billing_intent_admin_waivers_listing_id_fkey',
        'ALTER TABLE IF EXISTS public.account_share_billing_intent_admin_waivers DROP CONSTRAINT IF EXISTS account_share_billing_intent_admin_waivers_membership_id_fkey',
        'ALTER TABLE IF EXISTS public.account_share_billing_intent_admin_waivers DROP CONSTRAINT IF EXISTS account_share_billing_intent_admin_waivers_actor_user_id_fkey'
    ];
BEGIN
    PERFORM set_config('search_path', 'pg_catalog, public, pg_temp', FALSE);

    IF to_regclass('public.account_share_request_billing_intents') IS NULL THEN
        RAISE NOTICE 'billing intent tables already dropped; nothing to do';
        RETURN;
    END IF;

    -- Safety gate: never drop while an intent still carries unbilled usage. Such rows
    -- must be settled or explicitly waived first (see the 2026-08-01 runbook).
    SELECT COUNT(*)
    INTO unsettled_with_usage
    FROM public.account_share_request_billing_intents
    WHERE status NOT IN ('settled', 'cancelled')
      AND usage_payload IS NOT NULL;
    IF unsettled_with_usage > 0 THEN
        RAISE EXCEPTION
            'Cannot drop billing intent tables: % unsettled intents still carry usage payloads',
            unsettled_with_usage;
    END IF;
    COMMIT;

    -- One constraint per transaction: each lock is taken and released immediately.
    FOREACH stmt IN ARRAY detach_statements LOOP
        PERFORM set_config('lock_timeout', '3s', TRUE);
        EXECUTE stmt;
        COMMIT;
    END LOOP;

    PERFORM set_config('lock_timeout', '3s', TRUE);
    DROP FUNCTION IF EXISTS public.guard_account_share_billing_admin_waiver() CASCADE;
    DROP FUNCTION IF EXISTS public.guard_account_share_billing_intent() CASCADE;
    DROP FUNCTION IF EXISTS public.account_share_jsonb_has_only_keys(jsonb, text[]) CASCADE;
    COMMIT;

    -- Drops run last so a retry still finds the gate's source table in place.
    PERFORM set_config('lock_timeout', '5s', TRUE);
    DROP TABLE IF EXISTS public.account_share_billing_intent_admin_waivers CASCADE;
    COMMIT;

    PERFORM set_config('lock_timeout', '5s', TRUE);
    DROP TABLE IF EXISTS public.account_share_request_billing_intents CASCADE;
    COMMIT;
END;
$procedure$;

CALL account_share_drop_billing_intent_tables();

DROP PROCEDURE IF EXISTS account_share_drop_billing_intent_tables();
