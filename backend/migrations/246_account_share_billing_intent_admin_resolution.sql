-- Add an immutable operator waiver record for billing intents that cannot be
-- reconstructed after their runtime lease has expired. This migration does
-- not settle usage and does not write wallets or usage logs.

SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE IF NOT EXISTS account_share_billing_intent_admin_waivers (
    id BIGSERIAL PRIMARY KEY,
    intent_id BIGINT NOT NULL
        REFERENCES account_share_request_billing_intents(id) ON DELETE RESTRICT,
    listing_id BIGINT NOT NULL
        REFERENCES account_share_listings(id) ON DELETE RESTRICT,
    membership_id BIGINT NOT NULL
        REFERENCES account_share_memberships(id) ON DELETE RESTRICT,
    actor_user_id BIGINT
        REFERENCES users(id) ON DELETE SET NULL,
    actor_user_id_snapshot BIGINT NOT NULL,
    reason TEXT NOT NULL,
    action VARCHAR(32) NOT NULL,
    previous_status VARCHAR(32) NOT NULL,
    resulting_status VARCHAR(32) NOT NULL,
    previous_state_token BIGINT NOT NULL,
    resulting_state_token BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT account_share_billing_admin_waiver_intent_uniq
        UNIQUE (intent_id),
    CONSTRAINT account_share_billing_admin_waiver_actor_chk
        CHECK (actor_user_id_snapshot > 0),
    CONSTRAINT account_share_billing_admin_waiver_reason_chk
        CHECK (length(btrim(reason)) BETWEEN 1 AND 1000),
    CONSTRAINT account_share_billing_admin_waiver_action_chk
        CHECK (action = 'waive'),
    CONSTRAINT account_share_billing_admin_waiver_transition_chk
        CHECK (
            previous_status = 'needs_attention'
            AND resulting_status = 'cancelled'
            AND previous_state_token > 0
            AND resulting_state_token = previous_state_token + 1
        )
);

ALTER TABLE account_share_request_billing_intents
    ADD COLUMN IF NOT EXISTS admin_waiver_audit_id BIGINT;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_billing_intent_admin_waiver_fk'
          AND conrelid = 'account_share_request_billing_intents'::regclass
    ) THEN
        ALTER TABLE account_share_request_billing_intents
            ADD CONSTRAINT account_share_billing_intent_admin_waiver_fk
            FOREIGN KEY (admin_waiver_audit_id)
            REFERENCES account_share_billing_intent_admin_waivers(id)
            ON DELETE RESTRICT;
    END IF;
END
$$;

ALTER TABLE account_share_request_billing_intents
    DROP CONSTRAINT IF EXISTS account_share_billing_intent_forward_chk,
    DROP CONSTRAINT IF EXISTS account_share_billing_intent_cancel_chk,
    DROP CONSTRAINT IF EXISTS account_share_billing_intent_admin_waiver_link_chk;

ALTER TABLE account_share_request_billing_intents
    ADD CONSTRAINT account_share_billing_intent_forward_chk
        CHECK (
            (
                status IN ('in_flight', 'ready', 'processing', 'settled', 'failed')
                AND forward_started_at IS NOT NULL
            )
            OR (
                status = 'created'
                AND forward_started_at IS NULL
            )
            OR (
                status = 'cancelled'
                AND (
                    forward_started_at IS NULL
                    OR admin_waiver_audit_id IS NOT NULL
                )
            )
            OR status = 'needs_attention'
        ) NOT VALID,
    ADD CONSTRAINT account_share_billing_intent_cancel_chk
        CHECK (
            status <> 'cancelled'
            OR forward_started_at IS NULL
            OR admin_waiver_audit_id IS NOT NULL
        ) NOT VALID,
    ADD CONSTRAINT account_share_billing_intent_admin_waiver_link_chk
        CHECK (
            admin_waiver_audit_id IS NULL
            OR status = 'cancelled'
        ) NOT VALID;

ALTER TABLE account_share_request_billing_intents
    VALIDATE CONSTRAINT account_share_billing_intent_forward_chk;
ALTER TABLE account_share_request_billing_intents
    VALIDATE CONSTRAINT account_share_billing_intent_cancel_chk;
ALTER TABLE account_share_request_billing_intents
    VALIDATE CONSTRAINT account_share_billing_intent_admin_waiver_link_chk;

CREATE OR REPLACE FUNCTION guard_account_share_billing_admin_waiver()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF TG_OP = 'TRUNCATE' THEN
        RAISE EXCEPTION 'account-share billing waiver audit is immutable'
            USING ERRCODE = '55000';
    END IF;
    RAISE EXCEPTION 'account-share billing waiver audit is immutable'
        USING ERRCODE = '55000';
END
$$;

DROP TRIGGER IF EXISTS trg_account_share_billing_admin_waiver_immutable
    ON account_share_billing_intent_admin_waivers;
CREATE TRIGGER trg_account_share_billing_admin_waiver_immutable
    BEFORE UPDATE OR DELETE ON account_share_billing_intent_admin_waivers
    FOR EACH ROW
    EXECUTE FUNCTION guard_account_share_billing_admin_waiver();

DROP TRIGGER IF EXISTS trg_account_share_billing_admin_waiver_no_truncate
    ON account_share_billing_intent_admin_waivers;
CREATE TRIGGER trg_account_share_billing_admin_waiver_no_truncate
    BEFORE TRUNCATE ON account_share_billing_intent_admin_waivers
    FOR EACH STATEMENT
    EXECUTE FUNCTION guard_account_share_billing_admin_waiver();

CREATE OR REPLACE FUNCTION guard_account_share_billing_intent()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
DECLARE
    transition_allowed BOOLEAN;
    admin_waiver_valid BOOLEAN := FALSE;
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'account-share billing intent is immutable'
            USING ERRCODE = '55000';
    END IF;

    IF ROW(
        NEW.request_id,
        NEW.client_request_id,
        NEW.dispatch_id,
        NEW.attempt_no,
        NEW.api_key_id_snapshot,
        NEW.membership_id,
        NEW.listing_id,
        NEW.account_id_snapshot,
        NEW.binding_id,
        NEW.listing_revision_id,
        NEW.terms_revision_number,
        NEW.actor_user_id_snapshot,
        NEW.actor_role,
        NEW.consumer_user_id_snapshot,
        NEW.owner_user_id_snapshot,
        NEW.requested_model,
        NEW.routed_model,
        NEW.rate_multiplier_snapshot,
        NEW.owner_share_ratio_snapshot,
        NEW.invite_share_ratio_snapshot,
        NEW.platform_share_ratio_snapshot,
        NEW.command_schema_version,
        NEW.command_payload,
        NEW.command_hash,
        NEW.request_fingerprint,
        NEW.created_at
    ) IS DISTINCT FROM ROW(
        OLD.request_id,
        OLD.client_request_id,
        OLD.dispatch_id,
        OLD.attempt_no,
        OLD.api_key_id_snapshot,
        OLD.membership_id,
        OLD.listing_id,
        OLD.account_id_snapshot,
        OLD.binding_id,
        OLD.listing_revision_id,
        OLD.terms_revision_number,
        OLD.actor_user_id_snapshot,
        OLD.actor_role,
        OLD.consumer_user_id_snapshot,
        OLD.owner_user_id_snapshot,
        OLD.requested_model,
        OLD.routed_model,
        OLD.rate_multiplier_snapshot,
        OLD.owner_share_ratio_snapshot,
        OLD.invite_share_ratio_snapshot,
        OLD.platform_share_ratio_snapshot,
        OLD.command_schema_version,
        OLD.command_payload,
        OLD.command_hash,
        OLD.request_fingerprint,
        OLD.created_at
    ) THEN
        RAISE EXCEPTION 'account-share billing intent routing snapshot is immutable'
            USING ERRCODE = '55000';
    END IF;

    IF OLD.forward_started_at IS NOT NULL
       AND NEW.forward_started_at IS DISTINCT FROM OLD.forward_started_at THEN
        RAISE EXCEPTION 'account-share billing intent forward timestamp is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.usage_payload IS NOT NULL
       AND ROW(
           NEW.usage_schema_version,
           NEW.usage_payload,
           NEW.usage_payload_hash,
           NEW.response_summary,
           NEW.completed_at
       ) IS DISTINCT FROM ROW(
           OLD.usage_schema_version,
           OLD.usage_payload,
           OLD.usage_payload_hash,
           OLD.response_summary,
           OLD.completed_at
       ) THEN
        RAISE EXCEPTION 'account-share billing intent usage snapshot is immutable'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.usage_log_id IS NOT NULL
       AND NEW.usage_log_id IS DISTINCT FROM OLD.usage_log_id THEN
        RAISE EXCEPTION 'account-share billing intent usage log link is immutable'
            USING ERRCODE = '55000';
    END IF;

    IF OLD.status = 'needs_attention' AND NEW.status = 'cancelled' THEN
        SELECT EXISTS (
            SELECT 1
            FROM account_share_billing_intent_admin_waivers waiver
            WHERE waiver.id = NEW.admin_waiver_audit_id
              AND waiver.intent_id = NEW.id
              AND waiver.listing_id = NEW.listing_id
              AND waiver.membership_id = NEW.membership_id
              AND waiver.action = 'waive'
              AND waiver.previous_status = OLD.status
              AND waiver.resulting_status = NEW.status
              AND waiver.previous_state_token = OLD.state_token
              AND waiver.resulting_state_token = NEW.state_token
        ) INTO admin_waiver_valid;
        IF NOT admin_waiver_valid THEN
            RAISE EXCEPTION 'account-share billing intent cancellation requires a matching admin waiver audit'
                USING ERRCODE = '55000';
        END IF;
    END IF;

    IF NEW.admin_waiver_audit_id IS DISTINCT FROM OLD.admin_waiver_audit_id
       AND NOT (
           OLD.admin_waiver_audit_id IS NULL
           AND NEW.admin_waiver_audit_id IS NOT NULL
           AND OLD.status = 'needs_attention'
           AND NEW.status = 'cancelled'
           AND admin_waiver_valid
       ) THEN
        RAISE EXCEPTION 'account-share billing intent admin waiver link is immutable'
            USING ERRCODE = '55000';
    END IF;

    IF NEW.status = 'cancelled'
       AND NEW.forward_started_at IS NOT NULL
       AND NOT admin_waiver_valid THEN
        RAISE EXCEPTION 'forwarded account-share billing intent cannot be cancelled without an admin waiver'
            USING ERRCODE = '55000';
    END IF;

    IF NEW.status IS DISTINCT FROM OLD.status THEN
        transition_allowed := CASE OLD.status
            WHEN 'created' THEN NEW.status IN ('in_flight', 'cancelled', 'needs_attention')
            WHEN 'in_flight' THEN NEW.status IN ('ready', 'needs_attention')
            WHEN 'ready' THEN NEW.status IN ('processing', 'needs_attention')
            WHEN 'processing' THEN NEW.status IN ('processing', 'settled', 'failed', 'needs_attention')
            WHEN 'failed' THEN NEW.status IN ('processing', 'needs_attention')
            WHEN 'needs_attention' THEN
                NEW.status = 'ready'
                OR (NEW.status = 'cancelled' AND admin_waiver_valid)
            ELSE FALSE
        END;
        IF NOT transition_allowed THEN
            RAISE EXCEPTION 'invalid account-share billing intent transition: % -> %', OLD.status, NEW.status
                USING ERRCODE = '55000';
        END IF;
        IF NEW.state_token <> OLD.state_token + 1 THEN
            RAISE EXCEPTION 'account-share billing intent state token must increment once'
                USING ERRCODE = '55000';
        END IF;
    ELSIF OLD.status = 'processing'
          AND NEW.status = 'processing'
          AND NEW.state_token = OLD.state_token + 1
          AND NEW.lease_token = OLD.lease_token + 1
          AND OLD.lease_expires_at <= clock_timestamp() THEN
        NULL;
    ELSIF NEW.state_token <> OLD.state_token THEN
        RAISE EXCEPTION 'account-share billing intent state token changed without transition or expired-lease reclaim'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END
$$;

COMMENT ON TABLE account_share_billing_intent_admin_waivers
    IS 'Immutable admin waiver audit for unrecoverable account-share billing intents; never represents usage or settlement';
COMMENT ON COLUMN account_share_request_billing_intents.admin_waiver_audit_id
    IS 'Immutable link set only when a needs_attention intent is administratively waived to cancelled';
