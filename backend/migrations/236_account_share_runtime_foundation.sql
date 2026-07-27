-- Expand-only runtime and durable billing foundation for account-share rooms.
-- This migration creates history/barrier tables only. It does not backfill
-- legacy rows, switch request routing, or execute any worker.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS account_share_room_operations (
    id UUID PRIMARY KEY,
    listing_id BIGINT NOT NULL REFERENCES account_share_listings(id) ON DELETE RESTRICT,
    action VARCHAR(40) NOT NULL,
    actor_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    actor_role VARCHAR(20) NOT NULL,
    source VARCHAR(40) NOT NULL,
    request_id VARCHAR(255),
    expected_version BIGINT,
    start_version BIGINT,
    final_version BIGINT,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    blocker JSONB NOT NULL DEFAULT '{}'::jsonb,
    result JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_code VARCHAR(100),
    error_message TEXT,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    state_token BIGINT NOT NULL DEFAULT 1,
    lease_token BIGINT NOT NULL DEFAULT 0,
    lease_owner VARCHAR(128),
    lease_expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT account_share_room_operation_action_chk
        CHECK (action IN ('drain_room', 'drain_accounts', 'rebind_accounts', 'delete_room', 'end_membership')),
    CONSTRAINT account_share_room_operation_actor_role_chk
        CHECK (actor_role IN ('owner', 'consumer', 'admin', 'system')),
    CONSTRAINT account_share_room_operation_status_chk
        CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled', 'needs_attention')),
    CONSTRAINT account_share_room_operation_versions_chk
        CHECK (
            (expected_version IS NULL OR expected_version > 0)
            AND (start_version IS NULL OR start_version > 0)
            AND (final_version IS NULL OR final_version > 0)
        ),
    CONSTRAINT account_share_room_operation_payloads_chk
        CHECK (jsonb_typeof(blocker) = 'object' AND jsonb_typeof(result) = 'object'),
    CONSTRAINT account_share_room_operation_tokens_chk
        CHECK (attempt_count >= 0 AND state_token > 0 AND lease_token >= 0),
    CONSTRAINT account_share_room_operation_lease_chk
        CHECK (
            (status = 'running' AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)
            OR
            (status <> 'running' AND lease_owner IS NULL AND lease_expires_at IS NULL)
        ),
    CONSTRAINT account_share_room_operation_completion_chk
        CHECK (
            (status IN ('succeeded', 'failed', 'cancelled') AND completed_at IS NOT NULL)
            OR
            (status NOT IN ('succeeded', 'failed', 'cancelled') AND completed_at IS NULL)
        )
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_account_share_room_operations_open_listing
    ON account_share_room_operations(listing_id)
    WHERE status IN ('pending', 'running', 'needs_attention');

CREATE INDEX IF NOT EXISTS idx_account_share_room_operations_listing_created
    ON account_share_room_operations(listing_id, created_at DESC, id);

CREATE INDEX IF NOT EXISTS idx_account_share_room_operations_claim
    ON account_share_room_operations(status, lease_expires_at, created_at, id)
    WHERE status IN ('pending', 'running');

CREATE TABLE IF NOT EXISTS account_share_room_account_assignments (
    id BIGSERIAL PRIMARY KEY,
    listing_id BIGINT NOT NULL REFERENCES account_share_listings(id) ON DELETE RESTRICT,
    account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
    account_id_snapshot BIGINT NOT NULL,
    owner_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    owner_user_id_snapshot BIGINT NOT NULL,
    account_name_snapshot VARCHAR(255) NOT NULL,
    platform_snapshot VARCHAR(50) NOT NULL,
    account_level_snapshot VARCHAR(64) NOT NULL,
    configured_concurrency_snapshot INTEGER NOT NULL,
    attached_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attached_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    attached_by_role VARCHAR(20) NOT NULL,
    attach_reason TEXT,
    detached_at TIMESTAMPTZ,
    detached_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    detached_by_role VARCHAR(20),
    detach_reason TEXT,
    operation_id UUID REFERENCES account_share_room_operations(id) ON DELETE RESTRICT,
    snapshot_quality VARCHAR(20) NOT NULL DEFAULT 'exact',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_account_share_room_assignment_identity
        UNIQUE (id, listing_id, account_id_snapshot),
    CONSTRAINT account_share_room_assignment_ids_chk
        CHECK (
            account_id_snapshot > 0
            AND owner_user_id_snapshot > 0
            AND (account_id IS NULL OR account_id = account_id_snapshot)
            AND (owner_user_id IS NULL OR owner_user_id = owner_user_id_snapshot)
        ),
    CONSTRAINT account_share_room_assignment_concurrency_chk
        CHECK (configured_concurrency_snapshot > 0),
    CONSTRAINT account_share_room_assignment_attached_role_chk
        CHECK (attached_by_role IN ('owner', 'admin', 'system')),
    CONSTRAINT account_share_room_assignment_detached_role_chk
        CHECK (detached_by_role IS NULL OR detached_by_role IN ('owner', 'admin', 'system')),
    CONSTRAINT account_share_room_assignment_snapshot_quality_chk
        CHECK (snapshot_quality IN ('exact', 'backfilled_current', 'unknown')),
    CONSTRAINT account_share_room_assignment_interval_chk
        CHECK (
            (detached_at IS NULL AND detached_by_role IS NULL)
            OR
            (detached_at IS NOT NULL AND detached_at >= attached_at AND detached_by_role IS NOT NULL)
        )
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_account_share_room_assignments_open_account
    ON account_share_room_account_assignments(account_id_snapshot)
    WHERE detached_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_account_share_room_assignments_listing_history
    ON account_share_room_account_assignments(listing_id, attached_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_account_share_room_assignments_account_history
    ON account_share_room_account_assignments(account_id_snapshot, attached_at DESC, id DESC);

ALTER TABLE account_share_room_accounts
    ADD COLUMN IF NOT EXISTS last_validated_revision_id BIGINT;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_room_accounts_state_chk'
          AND conrelid = 'account_share_room_accounts'::regclass
    ) THEN
        ALTER TABLE account_share_room_accounts
            DROP CONSTRAINT account_share_room_accounts_state_chk;
    END IF;

    ALTER TABLE account_share_room_accounts
        ADD CONSTRAINT account_share_room_accounts_state_chk
        CHECK (state IN ('validating', 'active', 'draining', 'failed')) NOT VALID;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_account_share_room_accounts_validated_revision'
          AND conrelid = 'account_share_room_accounts'::regclass
    ) THEN
        ALTER TABLE account_share_room_accounts
            ADD CONSTRAINT fk_account_share_room_accounts_validated_revision
            FOREIGN KEY (listing_id, last_validated_revision_id)
            REFERENCES account_share_listing_revisions(listing_id, id)
            ON DELETE RESTRICT
            NOT VALID;
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS account_share_membership_account_bindings (
    id BIGSERIAL PRIMARY KEY,
    membership_id BIGINT NOT NULL,
    listing_id BIGINT NOT NULL,
    account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
    account_id_snapshot BIGINT NOT NULL,
    room_account_assignment_id BIGINT NOT NULL,
    listing_revision_id BIGINT NOT NULL,
    terms_revision_number BIGINT NOT NULL,
    account_name_snapshot VARCHAR(255) NOT NULL,
    platform_snapshot VARCHAR(50) NOT NULL,
    account_level_snapshot VARCHAR(64) NOT NULL,
    configured_concurrency_snapshot INTEGER NOT NULL,
    routing_generation BIGINT NOT NULL,
    bound_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    bound_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    bound_by_role VARCHAR(20) NOT NULL,
    bind_reason TEXT,
    unbound_at TIMESTAMPTZ,
    unbound_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    unbound_by_role VARCHAR(20),
    unbind_reason TEXT,
    snapshot_quality VARCHAR(20) NOT NULL DEFAULT 'exact',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_account_share_membership_binding_identity
        UNIQUE (
            id,
            membership_id,
            listing_id,
            account_id_snapshot,
            listing_revision_id,
            terms_revision_number
        ),
    CONSTRAINT uq_account_share_membership_binding_generation
        UNIQUE (membership_id, routing_generation),
    CONSTRAINT fk_account_share_membership_binding_membership
        FOREIGN KEY (membership_id, listing_id, listing_revision_id)
        REFERENCES account_share_memberships(id, listing_id, listing_revision_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_account_share_membership_binding_assignment
        FOREIGN KEY (room_account_assignment_id, listing_id, account_id_snapshot)
        REFERENCES account_share_room_account_assignments(id, listing_id, account_id_snapshot)
        ON DELETE RESTRICT,
    CONSTRAINT fk_account_share_membership_binding_revision
        FOREIGN KEY (listing_id, listing_revision_id, terms_revision_number)
        REFERENCES account_share_listing_revisions(listing_id, id, revision_number)
        ON DELETE RESTRICT,
    CONSTRAINT account_share_membership_binding_ids_chk
        CHECK (
            account_id_snapshot > 0
            AND terms_revision_number > 0
            AND (account_id IS NULL OR account_id = account_id_snapshot)
        ),
    CONSTRAINT account_share_membership_binding_concurrency_chk
        CHECK (configured_concurrency_snapshot > 0),
    CONSTRAINT account_share_membership_binding_generation_chk
        CHECK (routing_generation > 0),
    CONSTRAINT account_share_membership_binding_bound_role_chk
        CHECK (bound_by_role IN ('owner', 'consumer', 'admin', 'system')),
    CONSTRAINT account_share_membership_binding_unbound_role_chk
        CHECK (unbound_by_role IS NULL OR unbound_by_role IN ('owner', 'consumer', 'admin', 'system')),
    CONSTRAINT account_share_membership_binding_snapshot_quality_chk
        CHECK (snapshot_quality IN ('exact', 'backfilled_current', 'unknown')),
    CONSTRAINT account_share_membership_binding_interval_chk
        CHECK (
            (unbound_at IS NULL AND unbound_by_role IS NULL)
            OR
            (unbound_at IS NOT NULL AND unbound_at >= bound_at AND unbound_by_role IS NOT NULL)
        )
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_account_share_membership_bindings_open_membership
    ON account_share_membership_account_bindings(membership_id)
    WHERE unbound_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_account_share_membership_bindings_listing_history
    ON account_share_membership_account_bindings(listing_id, bound_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_account_share_membership_bindings_account_history
    ON account_share_membership_account_bindings(account_id_snapshot, bound_at DESC, id DESC);

CREATE OR REPLACE FUNCTION account_share_jsonb_has_only_keys(payload JSONB, allowed_keys TEXT[])
RETURNS BOOLEAN
LANGUAGE sql
IMMUTABLE
STRICT
PARALLEL SAFE
SET search_path = pg_catalog, public
AS $$
    SELECT jsonb_typeof(payload) = 'object'
       AND (
           SELECT COUNT(*)
           FROM jsonb_object_keys(payload)
       ) = cardinality(allowed_keys)
       AND NOT EXISTS (
           SELECT 1
           FROM jsonb_object_keys(payload) AS payload_keys(payload_key)
           WHERE NOT (payload_key = ANY(allowed_keys))
       )
$$;

CREATE TABLE IF NOT EXISTS account_share_request_billing_intents (
    id BIGSERIAL PRIMARY KEY,
    request_id VARCHAR(255) NOT NULL,
    api_key_id BIGINT REFERENCES api_keys(id) ON DELETE SET NULL,
    api_key_id_snapshot BIGINT NOT NULL,
    membership_id BIGINT NOT NULL,
    listing_id BIGINT NOT NULL,
    account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
    account_id_snapshot BIGINT NOT NULL,
    binding_id BIGINT NOT NULL,
    listing_revision_id BIGINT NOT NULL,
    terms_revision_number BIGINT NOT NULL,
    actor_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    actor_user_id_snapshot BIGINT,
    actor_role VARCHAR(20) NOT NULL,
    consumer_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    consumer_user_id_snapshot BIGINT NOT NULL,
    owner_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    owner_user_id_snapshot BIGINT NOT NULL,
    requested_model VARCHAR(255) NOT NULL,
    routed_model VARCHAR(255) NOT NULL,
    rate_multiplier_snapshot NUMERIC(20,10) NOT NULL,
    owner_share_ratio_snapshot NUMERIC(20,10) NOT NULL,
    invite_share_ratio_snapshot NUMERIC(20,10) NOT NULL,
    platform_share_ratio_snapshot NUMERIC(20,10) NOT NULL,
    command_schema_version SMALLINT NOT NULL,
    command_payload JSONB NOT NULL,
    command_hash CHAR(64) NOT NULL,
    request_fingerprint CHAR(64) NOT NULL,
    usage_schema_version SMALLINT,
    usage_payload JSONB,
    usage_payload_hash CHAR(64),
    response_summary JSONB,
    status VARCHAR(20) NOT NULL DEFAULT 'created',
    state_token BIGINT NOT NULL DEFAULT 1,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    lease_token BIGINT NOT NULL DEFAULT 0,
    lease_owner VARCHAR(128),
    lease_expires_at TIMESTAMPTZ,
    next_attempt_at TIMESTAMPTZ,
    last_error_code VARCHAR(100),
    last_error_message TEXT,
    forward_started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    settled_at TIMESTAMPTZ,
    usage_log_id BIGINT REFERENCES usage_logs(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_account_share_request_billing_intent
        UNIQUE (request_id, api_key_id_snapshot),
    CONSTRAINT fk_account_share_billing_intent_membership
        FOREIGN KEY (membership_id, listing_id)
        REFERENCES account_share_memberships(id, listing_id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_account_share_billing_intent_binding
        FOREIGN KEY (
            binding_id,
            membership_id,
            listing_id,
            account_id_snapshot,
            listing_revision_id,
            terms_revision_number
        )
        REFERENCES account_share_membership_account_bindings(
            id,
            membership_id,
            listing_id,
            account_id_snapshot,
            listing_revision_id,
            terms_revision_number
        )
        ON DELETE RESTRICT,
    CONSTRAINT fk_account_share_billing_intent_revision
        FOREIGN KEY (listing_id, listing_revision_id, terms_revision_number)
        REFERENCES account_share_listing_revisions(listing_id, id, revision_number)
        ON DELETE RESTRICT,
    CONSTRAINT account_share_billing_intent_ids_chk
        CHECK (
            api_key_id_snapshot > 0
            AND account_id_snapshot > 0
            AND terms_revision_number > 0
            AND consumer_user_id_snapshot > 0
            AND owner_user_id_snapshot > 0
            AND (actor_user_id_snapshot IS NULL OR actor_user_id_snapshot > 0)
            AND (api_key_id IS NULL OR api_key_id = api_key_id_snapshot)
            AND (account_id IS NULL OR account_id = account_id_snapshot)
            AND (actor_user_id IS NULL OR actor_user_id = actor_user_id_snapshot)
            AND (consumer_user_id IS NULL OR consumer_user_id = consumer_user_id_snapshot)
            AND (owner_user_id IS NULL OR owner_user_id = owner_user_id_snapshot)
        ),
    CONSTRAINT account_share_billing_intent_actor_role_chk
        CHECK (actor_role IN ('owner', 'consumer', 'admin', 'system')),
    CONSTRAINT account_share_billing_intent_status_chk
        CHECK (status IN ('created', 'in_flight', 'ready', 'processing', 'settled', 'cancelled', 'failed', 'needs_attention')),
    CONSTRAINT account_share_billing_intent_schema_chk
        CHECK (
            command_schema_version > 0
            AND (usage_schema_version IS NULL OR usage_schema_version > 0)
        ),
    CONSTRAINT account_share_billing_intent_payload_chk
        CHECK (
            jsonb_typeof(command_payload) = 'object'
            AND command_payload ->> 'schema_version' = command_schema_version::text
            AND (usage_payload IS NULL OR jsonb_typeof(usage_payload) = 'object')
            AND (
                usage_payload IS NULL
                OR usage_payload ->> 'schema_version' = usage_schema_version::text
            )
            AND (response_summary IS NULL OR jsonb_typeof(response_summary) = 'object')
            AND account_share_jsonb_has_only_keys(
                command_payload,
                ARRAY[
                    'schema_version',
                    'group_id',
                    'subscription_id',
                    'account_type',
                    'requested_model',
                    'routed_model',
                    'inbound_endpoint',
                    'upstream_endpoint',
                    'request_type',
                    'service_tier',
                    'reasoning_effort',
                    'billing_type',
                    'prefer_points_billing',
                    'rate_multiplier',
                    'owner_share_ratio',
                    'invite_share_ratio',
                    'platform_share_ratio',
                    'policy_id',
                    'policy_version'
                ]::text[]
            )
            AND (
                usage_payload IS NULL
                OR account_share_jsonb_has_only_keys(
                    usage_payload,
                    ARRAY[
                        'schema_version',
                        'usage_occurred_at',
                        'input_tokens',
                        'output_tokens',
                        'cache_creation_tokens',
                        'cache_creation_5m_tokens',
                        'cache_creation_1h_tokens',
                        'cache_read_tokens',
                        'image_input_tokens',
                        'image_output_tokens',
                        'image_count',
                        'image_size',
                        'media_type',
                        'video_count',
                        'video_resolution',
                        'video_duration_seconds',
                        'duration_ms',
                        'first_token_ms',
                        'balance_cost',
                        'subscription_cost',
                        'private_group_commission_cost',
                        'api_key_quota_cost',
                        'api_key_rate_limit_cost',
                        'account_quota_cost',
                        'base_charge',
                        'hourly_charge',
                        'total_charge'
                    ]::text[]
                )
            )
            AND (
                response_summary IS NULL
                OR account_share_jsonb_has_only_keys(
                    response_summary,
                    ARRAY[
                        'schema_version',
                        'http_status',
                        'provider_request_id',
                        'finish_reason',
                        'streamed',
                        'error_code'
                    ]::text[]
                )
            )
        ),
    CONSTRAINT account_share_billing_intent_hash_chk
        CHECK (
            command_hash ~ '^[0-9a-f]{64}$'
            AND request_fingerprint ~ '^[0-9a-f]{64}$'
            AND (usage_payload_hash IS NULL OR usage_payload_hash ~ '^[0-9a-f]{64}$')
        ),
    CONSTRAINT account_share_billing_intent_ratio_chk
        CHECK (
            rate_multiplier_snapshot >= 0
            AND owner_share_ratio_snapshot BETWEEN 0 AND 1
            AND invite_share_ratio_snapshot BETWEEN 0 AND 1
            AND platform_share_ratio_snapshot BETWEEN 0 AND 1
            AND owner_share_ratio_snapshot + invite_share_ratio_snapshot + platform_share_ratio_snapshot <= 1
        ),
    CONSTRAINT account_share_billing_intent_tokens_chk
        CHECK (state_token > 0 AND attempt_count >= 0 AND lease_token >= 0),
    CONSTRAINT account_share_billing_intent_lease_chk
        CHECK (
            (status = 'processing' AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)
            OR
            (status <> 'processing' AND lease_owner IS NULL AND lease_expires_at IS NULL)
        ),
    CONSTRAINT account_share_billing_intent_ready_payload_chk
        CHECK (
            (
                status IN ('ready', 'processing', 'settled', 'failed')
                AND usage_schema_version IS NOT NULL
                AND usage_payload IS NOT NULL
                AND usage_payload_hash IS NOT NULL
                AND response_summary IS NOT NULL
                AND completed_at IS NOT NULL
            )
            OR status NOT IN ('ready', 'processing', 'settled', 'failed')
        ),
    CONSTRAINT account_share_billing_intent_forward_chk
        CHECK (
            (
                status IN ('in_flight', 'ready', 'processing', 'settled', 'failed')
                AND forward_started_at IS NOT NULL
            )
            OR (
                status IN ('created', 'cancelled')
                AND forward_started_at IS NULL
            )
            OR status = 'needs_attention'
        ),
    CONSTRAINT account_share_billing_intent_cancel_chk
        CHECK (status <> 'cancelled' OR forward_started_at IS NULL),
    CONSTRAINT account_share_billing_intent_settled_chk
        CHECK (
            (status = 'settled' AND settled_at IS NOT NULL)
            OR (status <> 'settled' AND settled_at IS NULL)
        )
);

CREATE INDEX IF NOT EXISTS idx_account_share_billing_intents_membership_pending
    ON account_share_request_billing_intents(membership_id, status, request_id, api_key_id_snapshot)
    WHERE status NOT IN ('settled', 'cancelled');

CREATE INDEX IF NOT EXISTS idx_account_share_billing_intents_listing_pending
    ON account_share_request_billing_intents(listing_id, status, updated_at, id)
    WHERE status NOT IN ('settled', 'cancelled');

CREATE INDEX IF NOT EXISTS idx_account_share_billing_intents_account_pending
    ON account_share_request_billing_intents(account_id_snapshot, status, updated_at, id)
    WHERE status NOT IN ('settled', 'cancelled');

CREATE INDEX IF NOT EXISTS idx_account_share_billing_intents_claim
    ON account_share_request_billing_intents(status, next_attempt_at, lease_expires_at, completed_at, id)
    WHERE status IN ('ready', 'processing', 'failed');

CREATE INDEX IF NOT EXISTS idx_account_share_billing_intents_attention
    ON account_share_request_billing_intents(status, updated_at, id)
    WHERE status IN ('created', 'in_flight', 'ready', 'processing', 'failed', 'needs_attention');

CREATE OR REPLACE FUNCTION guard_account_share_assignment_history()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION '% history is immutable', TG_TABLE_NAME
            USING ERRCODE = '55000';
    END IF;
    IF ROW(
        NEW.listing_id,
        NEW.account_id_snapshot,
        NEW.owner_user_id_snapshot,
        NEW.account_name_snapshot,
        NEW.platform_snapshot,
        NEW.account_level_snapshot,
        NEW.configured_concurrency_snapshot,
        NEW.attached_at,
        NEW.attached_by_user_id,
        NEW.attached_by_role,
        NEW.attach_reason,
        NEW.operation_id,
        NEW.snapshot_quality,
        NEW.created_at
    ) IS DISTINCT FROM ROW(
        OLD.listing_id,
        OLD.account_id_snapshot,
        OLD.owner_user_id_snapshot,
        OLD.account_name_snapshot,
        OLD.platform_snapshot,
        OLD.account_level_snapshot,
        OLD.configured_concurrency_snapshot,
        OLD.attached_at,
        OLD.attached_by_user_id,
        OLD.attached_by_role,
        OLD.attach_reason,
        OLD.operation_id,
        OLD.snapshot_quality,
        OLD.created_at
    ) THEN
        RAISE EXCEPTION '% immutable assignment snapshot cannot be changed', TG_TABLE_NAME
            USING ERRCODE = '55000';
    END IF;
    IF OLD.detached_at IS NOT NULL AND ROW(
        NEW.detached_at,
        NEW.detached_by_user_id,
        NEW.detached_by_role,
        NEW.detach_reason
    ) IS DISTINCT FROM ROW(
        OLD.detached_at,
        OLD.detached_by_user_id,
        OLD.detached_by_role,
        OLD.detach_reason
    ) THEN
        RAISE EXCEPTION '% closed assignment cannot be changed', TG_TABLE_NAME
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_account_share_room_assignments_history_guard
    ON account_share_room_account_assignments;
CREATE TRIGGER trg_account_share_room_assignments_history_guard
    BEFORE UPDATE OR DELETE ON account_share_room_account_assignments
    FOR EACH ROW
    EXECUTE FUNCTION guard_account_share_assignment_history();

CREATE OR REPLACE FUNCTION guard_account_share_binding_history()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION '% history is immutable', TG_TABLE_NAME
            USING ERRCODE = '55000';
    END IF;
    IF ROW(
        NEW.membership_id,
        NEW.listing_id,
        NEW.account_id_snapshot,
        NEW.room_account_assignment_id,
        NEW.listing_revision_id,
        NEW.terms_revision_number,
        NEW.account_name_snapshot,
        NEW.platform_snapshot,
        NEW.account_level_snapshot,
        NEW.configured_concurrency_snapshot,
        NEW.routing_generation,
        NEW.bound_at,
        NEW.bound_by_user_id,
        NEW.bound_by_role,
        NEW.bind_reason,
        NEW.snapshot_quality,
        NEW.created_at
    ) IS DISTINCT FROM ROW(
        OLD.membership_id,
        OLD.listing_id,
        OLD.account_id_snapshot,
        OLD.room_account_assignment_id,
        OLD.listing_revision_id,
        OLD.terms_revision_number,
        OLD.account_name_snapshot,
        OLD.platform_snapshot,
        OLD.account_level_snapshot,
        OLD.configured_concurrency_snapshot,
        OLD.routing_generation,
        OLD.bound_at,
        OLD.bound_by_user_id,
        OLD.bound_by_role,
        OLD.bind_reason,
        OLD.snapshot_quality,
        OLD.created_at
    ) THEN
        RAISE EXCEPTION '% immutable binding snapshot cannot be changed', TG_TABLE_NAME
            USING ERRCODE = '55000';
    END IF;
    IF OLD.unbound_at IS NOT NULL AND ROW(
        NEW.unbound_at,
        NEW.unbound_by_user_id,
        NEW.unbound_by_role,
        NEW.unbind_reason
    ) IS DISTINCT FROM ROW(
        OLD.unbound_at,
        OLD.unbound_by_user_id,
        OLD.unbound_by_role,
        OLD.unbind_reason
    ) THEN
        RAISE EXCEPTION '% closed binding cannot be changed', TG_TABLE_NAME
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_account_share_membership_bindings_history_guard
    ON account_share_membership_account_bindings;
CREATE TRIGGER trg_account_share_membership_bindings_history_guard
    BEFORE UPDATE OR DELETE ON account_share_membership_account_bindings
    FOR EACH ROW
    EXECUTE FUNCTION guard_account_share_binding_history();

CREATE OR REPLACE FUNCTION guard_account_share_billing_intent()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
DECLARE
    transition_allowed BOOLEAN;
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'account-share billing intent is immutable'
            USING ERRCODE = '55000';
    END IF;

    IF ROW(
        NEW.request_id,
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
    IF NEW.status = 'cancelled' AND NEW.forward_started_at IS NOT NULL THEN
        RAISE EXCEPTION 'forwarded account-share billing intent cannot be cancelled'
            USING ERRCODE = '55000';
    END IF;

    IF NEW.status IS DISTINCT FROM OLD.status THEN
        transition_allowed := CASE OLD.status
            WHEN 'created' THEN NEW.status IN ('in_flight', 'cancelled', 'needs_attention')
            WHEN 'in_flight' THEN NEW.status IN ('ready', 'needs_attention')
            WHEN 'ready' THEN NEW.status IN ('processing', 'needs_attention')
            WHEN 'processing' THEN NEW.status IN ('processing', 'settled', 'failed', 'needs_attention')
            WHEN 'failed' THEN NEW.status IN ('processing', 'needs_attention')
            WHEN 'needs_attention' THEN NEW.status = 'ready'
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
        -- An expired processing lease is reclaimed with a new fencing token.
        NULL;
    ELSIF NEW.state_token <> OLD.state_token THEN
        RAISE EXCEPTION 'account-share billing intent state token changed without transition or expired-lease reclaim'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_account_share_billing_intent_guard
    ON account_share_request_billing_intents;
CREATE TRIGGER trg_account_share_billing_intent_guard
    BEFORE UPDATE OR DELETE ON account_share_request_billing_intents
    FOR EACH ROW
    EXECUTE FUNCTION guard_account_share_billing_intent();

COMMENT ON TABLE account_share_room_account_assignments
    IS 'Immutable room-account assignment intervals; current projection may be removed but history remains';
COMMENT ON TABLE account_share_membership_account_bindings
    IS 'Immutable membership routing intervals; one membership has at most one open binding';
COMMENT ON TABLE account_share_room_operations
    IS 'Durable long-running room lifecycle operations; HTTP idempotency remains in idempotency_records';
COMMENT ON TABLE account_share_request_billing_intents
    IS 'Durable request billing barrier; payloads are versioned allowlists and never contain credentials or proxy secrets';
