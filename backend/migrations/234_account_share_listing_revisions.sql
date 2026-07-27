-- Expand-only traceability foundation for account-share rooms.
-- This migration intentionally leaves legacy rows without revisions/snapshots;
-- the application creates immutable revisions for new writes.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE account_share_listings
    ADD COLUMN IF NOT EXISTS row_version BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS current_revision_id BIGINT,
    ADD COLUMN IF NOT EXISTS validated_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS draining_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS paused_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS suspended_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS status_reason_code VARCHAR(64),
    ADD COLUMN IF NOT EXISTS status_reason TEXT,
    ADD COLUMN IF NOT EXISTS pending_operation_id UUID,
    ADD COLUMN IF NOT EXISTS deleted_by_user_id BIGINT,
    ADD COLUMN IF NOT EXISTS delete_reason TEXT,
    ADD COLUMN IF NOT EXISTS delete_request_id VARCHAR(128),
    ADD COLUMN IF NOT EXISTS deleted_revision_id BIGINT,
    ADD COLUMN IF NOT EXISTS deletion_snapshot JSONB;

CREATE TABLE IF NOT EXISTS account_share_listing_revisions (
    id BIGSERIAL PRIMARY KEY,
    listing_id BIGINT NOT NULL REFERENCES account_share_listings(id) ON DELETE RESTRICT,
    revision_number BIGINT NOT NULL,
    schema_version INTEGER NOT NULL DEFAULT 1,
    snapshot_quality VARCHAR(20) NOT NULL DEFAULT 'exact',
    room_name VARCHAR(100) NOT NULL,
    platform VARCHAR(50),
    account_level VARCHAR(64),
    owner_user_id BIGINT NOT NULL,
    owner_display_name_snapshot VARCHAR(255) NOT NULL,
    status VARCHAR(20) NOT NULL,
    seat_limit INTEGER NOT NULL,
    rate_multiplier NUMERIC(10,4) NOT NULL,
    allowed_models JSONB NOT NULL,
    per_user_concurrency INTEGER NOT NULL,
    hourly_rate NUMERIC(20,8) NOT NULL,
    hourly_fee_waiver_minimum NUMERIC(20,8) NOT NULL,
    min_balance_required NUMERIC(20,8) NOT NULL,
    codex_cli_only BOOLEAN NOT NULL,
    codex_5h_limit_percent NUMERIC(5,2) NOT NULL,
    codex_7d_limit_percent NUMERIC(5,2) NOT NULL,
    created_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_by_role VARCHAR(20) NOT NULL,
    source VARCHAR(40) NOT NULL,
    change_reason TEXT,
    operation_id UUID,
    force_applied BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_account_share_listing_revision_number UNIQUE (listing_id, revision_number),
    CONSTRAINT uq_account_share_listing_revision_identity UNIQUE (listing_id, id),
    CONSTRAINT account_share_listing_revision_number_chk CHECK (revision_number > 0),
    CONSTRAINT account_share_listing_revision_schema_version_chk CHECK (schema_version > 0),
    CONSTRAINT account_share_listing_revision_snapshot_quality_chk
        CHECK (snapshot_quality IN ('exact', 'backfilled_current', 'unknown')),
    CONSTRAINT account_share_listing_revision_models_chk CHECK (jsonb_typeof(allowed_models) = 'array'),
    CONSTRAINT account_share_listing_revision_role_chk CHECK (created_by_role IN ('owner', 'admin', 'system'))
);

CREATE TABLE IF NOT EXISTS account_share_room_events (
    id BIGSERIAL PRIMARY KEY,
    listing_id BIGINT NOT NULL REFERENCES account_share_listings(id) ON DELETE RESTRICT,
    revision_id BIGINT,
    event_type VARCHAR(64) NOT NULL,
    actor_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    actor_role VARCHAR(20) NOT NULL,
    reason TEXT,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT account_share_room_event_role_chk CHECK (actor_role IN ('owner', 'admin', 'system')),
    CONSTRAINT account_share_room_event_payload_chk CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT fk_account_share_room_event_revision
        FOREIGN KEY (listing_id, revision_id)
        REFERENCES account_share_listing_revisions(listing_id, id)
        ON DELETE RESTRICT
);

ALTER TABLE account_share_memberships
    ADD COLUMN IF NOT EXISTS listing_revision_id BIGINT,
    ADD COLUMN IF NOT EXISTS listing_version_snapshot BIGINT,
    ADD COLUMN IF NOT EXISTS room_name_snapshot VARCHAR(100),
    ADD COLUMN IF NOT EXISTS owner_user_id_snapshot BIGINT,
    ADD COLUMN IF NOT EXISTS owner_username_snapshot VARCHAR(255),
    ADD COLUMN IF NOT EXISTS platform_snapshot VARCHAR(50),
    ADD COLUMN IF NOT EXISTS account_level_snapshot VARCHAR(64),
    ADD COLUMN IF NOT EXISTS api_key_name_snapshot VARCHAR(255),
    ADD COLUMN IF NOT EXISTS terms_snapshot JSONB,
    ADD COLUMN IF NOT EXISTS snapshot_quality VARCHAR(20),
    ADD COLUMN IF NOT EXISTS ending_requested_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS ending_reason TEXT,
    ADD COLUMN IF NOT EXISTS settlement_status VARCHAR(20);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_listings_row_version_chk'
          AND conrelid = 'account_share_listings'::regclass
    ) THEN
        ALTER TABLE account_share_listings
            ADD CONSTRAINT account_share_listings_row_version_chk
            CHECK (row_version > 0) NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_listings_deletion_snapshot_chk'
          AND conrelid = 'account_share_listings'::regclass
    ) THEN
        ALTER TABLE account_share_listings
            ADD CONSTRAINT account_share_listings_deletion_snapshot_chk
            CHECK (deletion_snapshot IS NULL OR jsonb_typeof(deletion_snapshot) = 'object') NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_account_share_listings_deleted_by_user'
          AND conrelid = 'account_share_listings'::regclass
    ) THEN
        ALTER TABLE account_share_listings
            ADD CONSTRAINT fk_account_share_listings_deleted_by_user
            FOREIGN KEY (deleted_by_user_id) REFERENCES users(id) ON DELETE SET NULL
            NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_account_share_listings_current_revision'
          AND conrelid = 'account_share_listings'::regclass
    ) THEN
        ALTER TABLE account_share_listings
            ADD CONSTRAINT fk_account_share_listings_current_revision
            FOREIGN KEY (id, current_revision_id)
            REFERENCES account_share_listing_revisions(listing_id, id)
            ON DELETE RESTRICT
            NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_account_share_listings_deleted_revision'
          AND conrelid = 'account_share_listings'::regclass
    ) THEN
        ALTER TABLE account_share_listings
            ADD CONSTRAINT fk_account_share_listings_deleted_revision
            FOREIGN KEY (id, deleted_revision_id)
            REFERENCES account_share_listing_revisions(listing_id, id)
            ON DELETE RESTRICT
            NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_account_share_membership_revision'
          AND conrelid = 'account_share_memberships'::regclass
    ) THEN
        ALTER TABLE account_share_memberships
            ADD CONSTRAINT fk_account_share_membership_revision
            FOREIGN KEY (listing_id, listing_revision_id)
            REFERENCES account_share_listing_revisions(listing_id, id)
            ON DELETE RESTRICT
            NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_membership_terms_snapshot_chk'
          AND conrelid = 'account_share_memberships'::regclass
    ) THEN
        ALTER TABLE account_share_memberships
            ADD CONSTRAINT account_share_membership_terms_snapshot_chk
            CHECK (terms_snapshot IS NULL OR jsonb_typeof(terms_snapshot) = 'object') NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_membership_snapshot_quality_chk'
          AND conrelid = 'account_share_memberships'::regclass
    ) THEN
        ALTER TABLE account_share_memberships
            ADD CONSTRAINT account_share_membership_snapshot_quality_chk
            CHECK (snapshot_quality IS NULL OR snapshot_quality IN ('exact', 'backfilled_current', 'unknown')) NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_membership_listing_version_chk'
          AND conrelid = 'account_share_memberships'::regclass
    ) THEN
        ALTER TABLE account_share_memberships
            ADD CONSTRAINT account_share_membership_listing_version_chk
            CHECK (listing_version_snapshot IS NULL OR listing_version_snapshot > 0) NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_membership_settlement_status_chk'
          AND conrelid = 'account_share_memberships'::regclass
    ) THEN
        ALTER TABLE account_share_memberships
            ADD CONSTRAINT account_share_membership_settlement_status_chk
            CHECK (
                settlement_status IS NULL
                OR settlement_status IN ('pending', 'processing', 'settled', 'failed', 'not_required')
            ) NOT VALID;
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS idx_account_share_listing_revisions_listing_created
    ON account_share_listing_revisions(listing_id, revision_number DESC);

CREATE UNIQUE INDEX IF NOT EXISTS uq_account_share_listings_pending_operation
    ON account_share_listings(pending_operation_id)
    WHERE pending_operation_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_account_share_listings_delete_request
    ON account_share_listings(delete_request_id)
    WHERE delete_request_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_account_share_room_events_listing_created
    ON account_share_room_events(listing_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_account_share_memberships_revision
    ON account_share_memberships(listing_revision_id)
    WHERE listing_revision_id IS NOT NULL;

CREATE OR REPLACE FUNCTION prevent_account_share_audit_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    RAISE EXCEPTION '% is immutable', TG_TABLE_NAME
        USING ERRCODE = '55000';
END
$$;

DROP TRIGGER IF EXISTS trg_account_share_listing_revisions_immutable
    ON account_share_listing_revisions;
CREATE TRIGGER trg_account_share_listing_revisions_immutable
    BEFORE UPDATE OR DELETE ON account_share_listing_revisions
    FOR EACH ROW
    EXECUTE FUNCTION prevent_account_share_audit_mutation();

DROP TRIGGER IF EXISTS trg_account_share_room_events_immutable
    ON account_share_room_events;
CREATE TRIGGER trg_account_share_room_events_immutable
    BEFORE UPDATE OR DELETE ON account_share_room_events
    FOR EACH ROW
    EXECUTE FUNCTION prevent_account_share_audit_mutation();
