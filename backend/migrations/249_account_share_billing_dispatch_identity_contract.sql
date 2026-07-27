-- Contract billing dispatch identity after the expand compatibility window.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '60s';

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

ALTER TABLE account_share_request_billing_intents
    ALTER COLUMN client_request_id SET NOT NULL,
    ALTER COLUMN dispatch_id SET NOT NULL,
    ALTER COLUMN attempt_no SET NOT NULL;

ALTER TABLE account_share_request_billing_intents
    DROP CONSTRAINT IF EXISTS uq_account_share_request_billing_intent;

DROP TRIGGER IF EXISTS trg_fill_account_share_billing_dispatch_identity
    ON account_share_request_billing_intents;
DROP FUNCTION IF EXISTS fill_account_share_billing_dispatch_identity();

COMMENT ON COLUMN account_share_request_billing_intents.client_request_id
    IS 'Stable client/root request identity shared by failover attempts; WebSocket callers include the turn identity.';
COMMENT ON COLUMN account_share_request_billing_intents.dispatch_id
    IS 'Immutable UUID for one physical upstream dispatch and its billing intent.';
COMMENT ON COLUMN account_share_request_billing_intents.attempt_no
    IS 'One-based dispatch attempt number within client_request_id and API key.';
