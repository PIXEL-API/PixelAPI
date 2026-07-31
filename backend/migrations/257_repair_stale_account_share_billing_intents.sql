-- Resolve the historical account-share billing backlog created before the
-- upstream failure path stopped creating unrecoverable billing intents.
--
-- Intents without a durable usage payload cannot be billed accurately. They
-- are cancelled with an immutable, explicitly system-attributed waiver.
-- Intents that do have a durable usage payload are requeued for the normal
-- idempotent billing worker instead of being waived.

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '5min';

ALTER TABLE account_share_billing_intent_admin_waivers
    ADD COLUMN IF NOT EXISTS actor_kind VARCHAR(32) NOT NULL DEFAULT 'admin';

ALTER TABLE account_share_billing_intent_admin_waivers
    ALTER COLUMN actor_user_id_snapshot DROP NOT NULL,
    DROP CONSTRAINT IF EXISTS account_share_billing_admin_waiver_actor_chk,
    DROP CONSTRAINT IF EXISTS account_share_billing_admin_waiver_actor_identity_chk;

ALTER TABLE account_share_billing_intent_admin_waivers
    ADD CONSTRAINT account_share_billing_admin_waiver_actor_identity_chk
        CHECK (
            (
                actor_kind = 'admin'
                AND actor_user_id_snapshot IS NOT NULL
                AND actor_user_id_snapshot > 0
            )
            OR (
                actor_kind = 'system_migration'
                AND actor_user_id IS NULL
                AND actor_user_id_snapshot IS NULL
            )
        );

-- Migration 254 deliberately disabled all future waiver inserts. Open the
-- insert path only inside this transaction, then restore the guard below.
DROP TRIGGER IF EXISTS trg_account_share_billing_admin_waiver_immutable
    ON account_share_billing_intent_admin_waivers;
CREATE TRIGGER trg_account_share_billing_admin_waiver_immutable
    BEFORE UPDATE OR DELETE ON account_share_billing_intent_admin_waivers
    FOR EACH ROW
    EXECUTE FUNCTION guard_account_share_billing_admin_waiver();

INSERT INTO account_share_billing_intent_admin_waivers (
    intent_id,
    listing_id,
    membership_id,
    actor_user_id,
    actor_user_id_snapshot,
    actor_kind,
    reason,
    action,
    previous_status,
    resulting_status,
    previous_state_token,
    resulting_state_token
)
SELECT
    intent.id,
    intent.listing_id,
    intent.membership_id,
    NULL,
    NULL,
    'system_migration',
    'system migration 257: retire an unrecoverable historical billing intent without durable usage',
    'waive',
    intent.status,
    'cancelled',
    intent.state_token,
    intent.state_token + 1
FROM account_share_request_billing_intents AS intent
WHERE intent.status = 'needs_attention'
  AND intent.last_error_code IN (
      'forward_failed_without_usage_detail',
      'forward_usage_incomplete',
      'runtime_lease_expired_without_usage'
  )
  AND intent.usage_payload IS NULL
  AND intent.usage_log_id IS NULL
  AND intent.admin_waiver_audit_id IS NULL;

UPDATE account_share_request_billing_intents AS intent
SET status = 'cancelled',
    state_token = intent.state_token + 1,
    lease_owner = NULL,
    lease_expires_at = NULL,
    next_attempt_at = NULL,
    completed_at = clock_timestamp(),
    admin_waiver_audit_id = waiver.id,
    updated_at = clock_timestamp()
FROM account_share_billing_intent_admin_waivers AS waiver
WHERE waiver.intent_id = intent.id
  AND waiver.actor_kind = 'system_migration'
  AND waiver.reason =
      'system migration 257: retire an unrecoverable historical billing intent without durable usage'
  AND intent.status = 'needs_attention'
  AND intent.last_error_code IN (
      'forward_failed_without_usage_detail',
      'forward_usage_incomplete',
      'runtime_lease_expired_without_usage'
  )
  AND intent.usage_payload IS NULL
  AND intent.usage_log_id IS NULL
  AND intent.admin_waiver_audit_id IS NULL;

-- These rows contain complete durable usage, so preserve billing correctness
-- and give the idempotent worker a fresh retry budget.
UPDATE account_share_request_billing_intents
SET status = 'ready',
    state_token = state_token + 1,
    attempt_count = 0,
    lease_owner = NULL,
    lease_expires_at = NULL,
    next_attempt_at = NULL,
    updated_at = clock_timestamp()
WHERE status = 'needs_attention'
  AND last_error_code = 'billing_retry_exhausted'
  AND usage_payload IS NOT NULL
  AND usage_log_id IS NULL;

DO $$
DECLARE
    unrecoverable_remaining BIGINT;
    retryable_remaining BIGINT;
BEGIN
    SELECT COUNT(*)
    INTO unrecoverable_remaining
    FROM account_share_request_billing_intents
    WHERE status = 'needs_attention'
      AND last_error_code IN (
          'forward_failed_without_usage_detail',
          'forward_usage_incomplete',
          'runtime_lease_expired_without_usage'
      )
      AND usage_payload IS NULL
      AND usage_log_id IS NULL;

    SELECT COUNT(*)
    INTO retryable_remaining
    FROM account_share_request_billing_intents
    WHERE status = 'needs_attention'
      AND last_error_code = 'billing_retry_exhausted'
      AND usage_payload IS NOT NULL
      AND usage_log_id IS NULL;

    IF unrecoverable_remaining <> 0 OR retryable_remaining <> 0 THEN
        RAISE EXCEPTION
            'billing intent repair incomplete: unrecoverable_remaining=%, retryable_remaining=%',
            unrecoverable_remaining,
            retryable_remaining;
    END IF;
END
$$;

DROP TRIGGER IF EXISTS trg_account_share_billing_admin_waiver_immutable
    ON account_share_billing_intent_admin_waivers;
CREATE TRIGGER trg_account_share_billing_admin_waiver_immutable
    BEFORE INSERT OR UPDATE OR DELETE ON account_share_billing_intent_admin_waivers
    FOR EACH ROW
    EXECUTE FUNCTION guard_account_share_billing_admin_waiver();

COMMENT ON COLUMN account_share_billing_intent_admin_waivers.actor_kind
    IS 'Audit actor kind: admin for historical operator waivers, system_migration for versioned one-time repairs';
COMMENT ON TABLE account_share_billing_intent_admin_waivers
    IS 'Immutable billing-intent waiver audit; future inserts remain disabled outside versioned system repairs';
