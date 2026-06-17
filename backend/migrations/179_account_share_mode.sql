CREATE TABLE IF NOT EXISTS account_share_mode_groups (
    id BIGSERIAL PRIMARY KEY,
    platform VARCHAR(50) NOT NULL UNIQUE,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

WITH inserted_group AS (
    INSERT INTO groups (
        name,
        description,
        rate_multiplier,
        is_exclusive,
        status,
        owner_user_id,
        scope,
        platform,
        required_account_level,
        subscription_type,
        default_validity_days,
        allow_image_generation,
        image_rate_independent,
        image_rate_multiplier,
        claude_code_only,
        model_routing,
        model_routing_enabled,
        mcp_xml_inject,
        supported_model_scopes,
        sort_order,
        allow_messages_dispatch,
        require_oauth_only,
        require_privacy_set,
        default_mapped_model,
        messages_dispatch_model_config,
        rpm_limit,
        created_at,
        updated_at
    )
    SELECT
        'OpenAI账号模式',
        '统一账号共享模式分组；倍率由消费者绑定的共享账号动态决定。',
        1.0,
        FALSE,
        'active',
        NULL,
        'public',
        'openai',
        '',
        'standard',
        30,
        FALSE,
        FALSE,
        1.0,
        FALSE,
        '{}'::jsonb,
        FALSE,
        TRUE,
        '[]'::jsonb,
        -900,
        TRUE,
        TRUE,
        FALSE,
        '',
        '{}'::jsonb,
        0,
        NOW(),
        NOW()
    WHERE NOT EXISTS (
        SELECT 1 FROM groups
        WHERE name = 'OpenAI账号模式' AND deleted_at IS NULL
    )
    RETURNING id
),
resolved_group AS (
    SELECT id FROM inserted_group
    UNION ALL
    SELECT id FROM groups
    WHERE name = 'OpenAI账号模式' AND deleted_at IS NULL
    LIMIT 1
)
INSERT INTO account_share_mode_groups (platform, group_id, created_at, updated_at)
SELECT 'openai', id, NOW(), NOW()
FROM resolved_group
ON CONFLICT (platform) DO UPDATE
SET group_id = EXCLUDED.group_id,
    updated_at = NOW();

CREATE TABLE IF NOT EXISTS account_share_mode_policies (
    id BIGSERIAL PRIMARY KEY,
    platform VARCHAR(50) NOT NULL UNIQUE,
    platform_share_ratio NUMERIC(10,8) NOT NULL DEFAULT 0.10000000,
    owner_share_ratio NUMERIC(10,8) NOT NULL DEFAULT 0.90000000,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    version INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT account_share_mode_policies_ratio_chk CHECK (
        platform_share_ratio >= 0
        AND owner_share_ratio >= 0
        AND platform_share_ratio + owner_share_ratio <= 1.00000000
    )
);

INSERT INTO account_share_mode_policies (
    platform,
    platform_share_ratio,
    owner_share_ratio,
    enabled,
    version,
    created_at,
    updated_at
)
VALUES ('openai', 0.10000000, 0.90000000, TRUE, 1, NOW(), NOW())
ON CONFLICT (platform) DO NOTHING;

CREATE TABLE IF NOT EXISTS account_share_listings (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL UNIQUE REFERENCES accounts(id) ON DELETE CASCADE,
    owner_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    seat_limit INTEGER NOT NULL,
    rate_multiplier NUMERIC(10,4) NOT NULL DEFAULT 1.0000,
    allowed_models JSONB NOT NULL DEFAULT '[]'::jsonb,
    per_user_concurrency INTEGER NOT NULL DEFAULT 1,
    hourly_rate NUMERIC(20,8) NOT NULL DEFAULT 0,
    min_balance_required NUMERIC(20,8) NOT NULL DEFAULT 1,
    codex_cli_only BOOLEAN NOT NULL DEFAULT FALSE,
    codex_5h_limit_percent NUMERIC(5,2) NOT NULL DEFAULT 100.00,
    codex_7d_limit_percent NUMERIC(5,2) NOT NULL DEFAULT 100.00,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT account_share_listings_status_chk CHECK (status IN ('active', 'paused', 'disabled')),
    CONSTRAINT account_share_listings_seat_limit_chk CHECK (seat_limit BETWEEN 2 AND 5),
    CONSTRAINT account_share_listings_rate_multiplier_chk CHECK (rate_multiplier >= 0),
    CONSTRAINT account_share_listings_per_user_concurrency_chk CHECK (per_user_concurrency > 0),
    CONSTRAINT account_share_listings_hourly_rate_chk CHECK (hourly_rate >= 0),
    CONSTRAINT account_share_listings_min_balance_chk CHECK (min_balance_required >= 0),
    CONSTRAINT account_share_listings_codex_5h_chk CHECK (codex_5h_limit_percent BETWEEN 1 AND 100),
    CONSTRAINT account_share_listings_codex_7d_chk CHECK (codex_7d_limit_percent BETWEEN 1 AND 100)
);

CREATE INDEX IF NOT EXISTS idx_account_share_listings_status
    ON account_share_listings(status)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_account_share_listings_owner
    ON account_share_listings(owner_user_id)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_account_share_listings_seat_limit
    ON account_share_listings(seat_limit)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS account_share_memberships (
    id BIGSERIAL PRIMARY KEY,
    listing_id BIGINT NOT NULL REFERENCES account_share_listings(id) ON DELETE CASCADE,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    consumer_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT account_share_memberships_status_chk CHECK (status IN ('active', 'ended')),
    CONSTRAINT account_share_memberships_end_chk CHECK (
        (status = 'active' AND ended_at IS NULL)
        OR (status = 'ended' AND ended_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_account_share_memberships_active_consumer
    ON account_share_memberships(consumer_user_id)
    WHERE status = 'active' AND deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_account_share_memberships_active_api_key
    ON account_share_memberships(api_key_id)
    WHERE status = 'active' AND deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_account_share_memberships_active_listing_consumer
    ON account_share_memberships(listing_id, consumer_user_id)
    WHERE status = 'active' AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_account_share_memberships_listing
    ON account_share_memberships(listing_id, status)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_account_share_memberships_history_consumer
    ON account_share_memberships(consumer_user_id, joined_at DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS account_share_mode_settlement_entries (
    id BIGSERIAL PRIMARY KEY,
    usage_log_id BIGINT UNIQUE REFERENCES usage_logs(id) ON DELETE SET NULL,
    membership_id BIGINT NOT NULL REFERENCES account_share_memberships(id) ON DELETE RESTRICT,
    listing_id BIGINT NOT NULL REFERENCES account_share_listings(id) ON DELETE RESTRICT,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
    owner_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    consumer_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE RESTRICT,
    base_charge NUMERIC(20,10) NOT NULL DEFAULT 0,
    hourly_charge NUMERIC(20,10) NOT NULL DEFAULT 0,
    total_charge NUMERIC(20,10) NOT NULL DEFAULT 0,
    owner_credit NUMERIC(20,10) NOT NULL DEFAULT 0,
    platform_credit NUMERIC(20,10) NOT NULL DEFAULT 0,
    rate_multiplier_snapshot NUMERIC(10,4) NOT NULL DEFAULT 1,
    hourly_rate_snapshot NUMERIC(20,8) NOT NULL DEFAULT 0,
    owner_share_ratio_snapshot NUMERIC(10,8) NOT NULL DEFAULT 0.90000000,
    platform_share_ratio_snapshot NUMERIC(10,8) NOT NULL DEFAULT 0.10000000,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT account_share_mode_settlement_nonnegative_chk CHECK (
        base_charge >= 0
        AND hourly_charge >= 0
        AND total_charge >= 0
        AND owner_credit >= 0
        AND platform_credit >= 0
    )
);

CREATE INDEX IF NOT EXISTS idx_account_share_mode_settlement_owner
    ON account_share_mode_settlement_entries(owner_user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_account_share_mode_settlement_consumer
    ON account_share_mode_settlement_entries(consumer_user_id, created_at DESC);
