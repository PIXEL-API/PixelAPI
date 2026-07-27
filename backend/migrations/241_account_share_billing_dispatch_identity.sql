-- Expand durable billing identity while keeping legacy inserts valid.
-- NOT NULL enforcement and removal of the old request identity are deferred
-- to a later contract migration.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE account_share_request_billing_intents
    ADD COLUMN IF NOT EXISTS client_request_id VARCHAR(255),
    ADD COLUMN IF NOT EXISTS dispatch_id UUID,
    ADD COLUMN IF NOT EXISTS attempt_no INTEGER;

UPDATE account_share_request_billing_intents
SET client_request_id = COALESCE(NULLIF(BTRIM(client_request_id), ''), request_id),
    dispatch_id = COALESCE(
        dispatch_id,
        MD5('account-share-billing-intent:' || id::text || ':' || request_id)::uuid
    ),
    attempt_no = COALESCE(attempt_no, 1)
WHERE client_request_id IS NULL
   OR BTRIM(client_request_id) = ''
   OR dispatch_id IS NULL
   OR attempt_no IS NULL;

CREATE OR REPLACE FUNCTION fill_account_share_billing_dispatch_identity()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF NEW.client_request_id IS NULL OR BTRIM(NEW.client_request_id) = '' THEN
        NEW.client_request_id := NEW.request_id;
    END IF;
    IF NEW.dispatch_id IS NULL THEN
        NEW.dispatch_id := MD5(
            'account-share-billing-intent:' || NEW.id::text || ':' || NEW.request_id
        )::uuid;
    END IF;
    IF NEW.attempt_no IS NULL THEN
        NEW.attempt_no := 1;
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_fill_account_share_billing_dispatch_identity
    ON account_share_request_billing_intents;
CREATE TRIGGER trg_fill_account_share_billing_dispatch_identity
BEFORE INSERT OR UPDATE OF request_id, client_request_id, dispatch_id, attempt_no
ON account_share_request_billing_intents
FOR EACH ROW
EXECUTE FUNCTION fill_account_share_billing_dispatch_identity();

CREATE UNIQUE INDEX IF NOT EXISTS uq_account_share_billing_intent_dispatch
    ON account_share_request_billing_intents(dispatch_id);

CREATE UNIQUE INDEX IF NOT EXISTS uq_account_share_billing_intent_client_attempt
    ON account_share_request_billing_intents(
        client_request_id,
        api_key_id_snapshot,
        attempt_no
    );

CREATE INDEX IF NOT EXISTS idx_account_share_billing_intent_client_history
    ON account_share_request_billing_intents(
        client_request_id,
        attempt_no,
        created_at,
        id
    );

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_billing_intent_dispatch_identity_chk'
          AND conrelid = 'account_share_request_billing_intents'::regclass
    ) THEN
        ALTER TABLE account_share_request_billing_intents
            ADD CONSTRAINT account_share_billing_intent_dispatch_identity_chk
            CHECK (
                client_request_id IS NULL
                OR (
                    BTRIM(client_request_id) <> ''
                    AND attempt_no > 0
                )
            ) NOT VALID;
    END IF;
END
$$;

ALTER TABLE account_share_request_billing_intents
    VALIDATE CONSTRAINT account_share_billing_intent_dispatch_identity_chk;

COMMENT ON COLUMN account_share_request_billing_intents.client_request_id
    IS 'Stable client/root request identity shared by failover attempts; nullable only for expand compatibility.';
COMMENT ON COLUMN account_share_request_billing_intents.dispatch_id
    IS 'Immutable UUID for one physical upstream dispatch and its billing intent; nullable only for expand compatibility.';
COMMENT ON COLUMN account_share_request_billing_intents.attempt_no
    IS 'One-based dispatch attempt number within client_request_id and API key; nullable only for expand compatibility.';
