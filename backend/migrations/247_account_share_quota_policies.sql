-- Persist global account-share quota defaults and auditable owner overrides.
-- Every change appends an immutable revision; this migration does not create
-- owner overrides because each manual/grandfather override requires an
-- explicit expiry, reason, confirmation and administrator decision.

SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE IF NOT EXISTS account_share_quota_policies (
    id BIGSERIAL PRIMARY KEY,
    scope_type VARCHAR(16) NOT NULL,
    owner_user_id BIGINT
        REFERENCES users(id) ON DELETE RESTRICT,
    version BIGINT NOT NULL,
    status VARCHAR(16) NOT NULL,
    override_kind VARCHAR(16) NOT NULL,
    max_live_rooms INTEGER NOT NULL,
    max_room_creates_24_hours INTEGER NOT NULL,
    max_accounts_per_room INTEGER NOT NULL,
    max_room_accounts_per_owner INTEGER NOT NULL,
    effective_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ,
    reason TEXT NOT NULL,
    actor_user_id BIGINT
        REFERENCES users(id) ON DELETE SET NULL,
    actor_user_id_snapshot BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT account_share_quota_policy_version_chk
        CHECK (version > 0),
    CONSTRAINT account_share_quota_policy_status_chk
        CHECK (status IN ('active', 'revoked')),
    CONSTRAINT account_share_quota_policy_kind_chk
        CHECK (override_kind IN ('default', 'manual', 'grandfather')),
    CONSTRAINT account_share_quota_policy_limits_chk
        CHECK (
            max_live_rooms BETWEEN 1 AND 1000000
            AND max_room_creates_24_hours BETWEEN 1 AND 1000000
            AND max_accounts_per_room BETWEEN 1 AND 1000000
            AND max_room_accounts_per_owner BETWEEN max_accounts_per_room AND 1000000
        ),
    CONSTRAINT account_share_quota_policy_reason_chk
        CHECK (length(btrim(reason)) BETWEEN 1 AND 1000),
    CONSTRAINT account_share_quota_policy_actor_chk
        CHECK (
            actor_user_id_snapshot >= 0
            AND (
                actor_user_id_snapshot > 0
                OR (
                    actor_user_id_snapshot = 0
                    AND actor_user_id IS NULL
                    AND scope_type = 'global'
                    AND version = 1
                )
            )
        ),
    CONSTRAINT account_share_quota_policy_scope_chk
        CHECK (
            (
                scope_type = 'global'
                AND owner_user_id IS NULL
                AND status = 'active'
                AND override_kind = 'default'
                AND expires_at IS NULL
            )
            OR (
                scope_type = 'owner'
                AND owner_user_id IS NOT NULL
                AND override_kind IN ('manual', 'grandfather')
                AND (
                    (
                        status = 'active'
                        AND expires_at IS NOT NULL
                        AND expires_at > effective_at
                    )
                    OR (
                        status = 'revoked'
                        AND expires_at IS NULL
                    )
                )
            )
        )
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_account_share_quota_policy_global_version
    ON account_share_quota_policies(version)
    WHERE scope_type = 'global' AND owner_user_id IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_account_share_quota_policy_owner_version
    ON account_share_quota_policies(owner_user_id, version)
    WHERE scope_type = 'owner' AND owner_user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_account_share_quota_policy_global_effective
    ON account_share_quota_policies(effective_at DESC, version DESC, id DESC)
    WHERE scope_type = 'global' AND owner_user_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_account_share_quota_policy_owner_effective
    ON account_share_quota_policies(owner_user_id, effective_at DESC, version DESC, id DESC)
    WHERE scope_type = 'owner' AND owner_user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_account_share_quota_policy_owner_expiry
    ON account_share_quota_policies(expires_at, owner_user_id)
    WHERE scope_type = 'owner' AND status = 'active' AND expires_at IS NOT NULL;

INSERT INTO account_share_quota_policies (
    scope_type,
    owner_user_id,
    version,
    status,
    override_kind,
    max_live_rooms,
    max_room_creates_24_hours,
    max_accounts_per_room,
    max_room_accounts_per_owner,
    effective_at,
    expires_at,
    reason,
    actor_user_id,
    actor_user_id_snapshot
)
SELECT
    'global',
    NULL,
    1,
    'active',
    'default',
    5,
    5,
    20,
    100,
    clock_timestamp(),
    NULL,
    'initial account-share quota defaults',
    NULL,
    0
WHERE NOT EXISTS (
    SELECT 1
    FROM account_share_quota_policies
    WHERE scope_type = 'global'
      AND owner_user_id IS NULL
);

CREATE OR REPLACE FUNCTION prevent_account_share_quota_policy_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    RAISE EXCEPTION 'account-share quota policy revisions are immutable'
        USING ERRCODE = '55000';
END
$$;

DROP TRIGGER IF EXISTS trg_account_share_quota_policy_immutable
    ON account_share_quota_policies;
CREATE TRIGGER trg_account_share_quota_policy_immutable
    BEFORE UPDATE OR DELETE ON account_share_quota_policies
    FOR EACH ROW
    EXECUTE FUNCTION prevent_account_share_quota_policy_mutation();

DROP TRIGGER IF EXISTS trg_account_share_quota_policy_truncate_immutable
    ON account_share_quota_policies;
CREATE TRIGGER trg_account_share_quota_policy_truncate_immutable
    BEFORE TRUNCATE ON account_share_quota_policies
    FOR EACH STATEMENT
    EXECUTE FUNCTION prevent_account_share_quota_policy_mutation();

COMMENT ON TABLE account_share_quota_policies
    IS 'Immutable revisions for global account-share quota defaults and expiring owner overrides';
COMMENT ON COLUMN account_share_quota_policies.override_kind
    IS 'default, manual or grandfather; grandfather revisions block all room/account growth';
COMMENT ON COLUMN account_share_quota_policies.actor_user_id_snapshot
    IS 'Immutable administrator ID snapshot; zero is reserved for the migration-created global version 1';
