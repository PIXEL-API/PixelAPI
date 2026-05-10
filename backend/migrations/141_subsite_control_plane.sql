CREATE TABLE IF NOT EXISTS subsites (
    id                   BIGSERIAL PRIMARY KEY,
    subsite_id           VARCHAR(64) NOT NULL UNIQUE,
    name                 VARCHAR(100) NOT NULL,
    public_url           TEXT NOT NULL,
    region               VARCHAR(64) NOT NULL DEFAULT '',
    capabilities         JSONB NOT NULL DEFAULT '[]'::jsonb,
    status               VARCHAR(32) NOT NULL DEFAULT 'pending',
    secret_hash          VARCHAR(128) NOT NULL,
    secret_ciphertext    TEXT NOT NULL,
    max_qps              INT NOT NULL DEFAULT 0,
    max_concurrency      INT NOT NULL DEFAULT 0,
    version              VARCHAR(64) NOT NULL DEFAULT '',
    last_heartbeat_at    TIMESTAMPTZ,
    health_score         INT NOT NULL DEFAULT 100,
    last_seen_ip         VARCHAR(64) NOT NULL DEFAULT '',
    metadata             JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at           TIMESTAMPTZ,
    CONSTRAINT subsites_status_check CHECK (status IN ('pending', 'active', 'maintenance', 'unhealthy', 'disabled'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_subsites_public_url_active
    ON subsites (public_url)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_subsites_status
    ON subsites (status)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_subsites_last_heartbeat_at
    ON subsites (last_heartbeat_at)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS account_leases (
    id                   BIGSERIAL PRIMARY KEY,
    lease_id             VARCHAR(80) NOT NULL UNIQUE,
    subsite_id           VARCHAR(64) NOT NULL REFERENCES subsites(subsite_id) ON DELETE CASCADE,
    account_id           BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    platform             VARCHAR(50) NOT NULL DEFAULT '',
    status               VARCHAR(32) NOT NULL DEFAULT 'active',
    max_concurrency      INT NOT NULL DEFAULT 1,
    max_requests         INT NOT NULL DEFAULT 0,
    max_tokens           BIGINT NOT NULL DEFAULT 0,
    used_requests        BIGINT NOT NULL DEFAULT 0,
    used_tokens          BIGINT NOT NULL DEFAULT 0,
    assigned_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at           TIMESTAMPTZ NOT NULL,
    renewed_at           TIMESTAMPTZ,
    released_at          TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at           TIMESTAMPTZ,
    CONSTRAINT account_leases_status_check CHECK (status IN ('active', 'renewing', 'draining', 'released', 'expired', 'revoked'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_account_leases_one_effective_account
    ON account_leases (account_id)
    WHERE deleted_at IS NULL
      AND status IN ('active', 'renewing', 'draining');
CREATE INDEX IF NOT EXISTS idx_account_leases_subsite_status
    ON account_leases (subsite_id, status)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_account_leases_expires_at
    ON account_leases (expires_at)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS quota_reservations (
    id                      BIGSERIAL PRIMARY KEY,
    reservation_id          VARCHAR(80) NOT NULL UNIQUE,
    request_id              VARCHAR(128) NOT NULL UNIQUE,
    subsite_id              VARCHAR(64) NOT NULL REFERENCES subsites(subsite_id) ON DELETE CASCADE,
    lease_id                VARCHAR(80) NOT NULL REFERENCES account_leases(lease_id) ON DELETE RESTRICT,
    account_id              BIGINT NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    api_key_id              BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    user_id                 BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id                BIGINT REFERENCES groups(id) ON DELETE SET NULL,
    subscription_id         BIGINT REFERENCES user_subscriptions(id) ON DELETE SET NULL,
    platform                VARCHAR(50) NOT NULL DEFAULT '',
    requested_model         VARCHAR(160) NOT NULL DEFAULT '',
    mapped_model            VARCHAR(160) NOT NULL DEFAULT '',
    estimated_cost          DECIMAL(20, 10) NOT NULL,
    actual_cost             DECIMAL(20, 10),
    billing_type            SMALLINT NOT NULL DEFAULT 0,
    status                  VARCHAR(32) NOT NULL DEFAULT 'reserved',
    request_fingerprint     VARCHAR(128) NOT NULL DEFAULT '',
    expires_at              TIMESTAMPTZ NOT NULL,
    settled_at              TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT quota_reservations_status_check CHECK (status IN ('reserved', 'settled', 'canceled', 'expired')),
    CONSTRAINT quota_reservations_estimated_cost_check CHECK (estimated_cost >= 0)
);

CREATE INDEX IF NOT EXISTS idx_quota_reservations_user_active
    ON quota_reservations (user_id, expires_at)
    WHERE status = 'reserved';
CREATE INDEX IF NOT EXISTS idx_quota_reservations_subscription_active
    ON quota_reservations (subscription_id, expires_at)
    WHERE status = 'reserved' AND subscription_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_quota_reservations_api_key
    ON quota_reservations (api_key_id, created_at);

CREATE TABLE IF NOT EXISTS subsite_heartbeats (
    id                   BIGSERIAL PRIMARY KEY,
    subsite_id           VARCHAR(64) NOT NULL REFERENCES subsites(subsite_id) ON DELETE CASCADE,
    status               VARCHAR(32) NOT NULL,
    version              VARCHAR(64) NOT NULL DEFAULT '',
    active_requests      INT NOT NULL DEFAULT 0,
    queued_usage         INT NOT NULL DEFAULT 0,
    qps                  DOUBLE PRECISION NOT NULL DEFAULT 0,
    cpu_percent          DOUBLE PRECISION NOT NULL DEFAULT 0,
    memory_bytes         BIGINT NOT NULL DEFAULT 0,
    metadata             JSONB NOT NULL DEFAULT '{}'::jsonb,
    reported_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    remote_ip            VARCHAR(64) NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_subsite_heartbeats_subsite_created
    ON subsite_heartbeats (subsite_id, created_at DESC);
