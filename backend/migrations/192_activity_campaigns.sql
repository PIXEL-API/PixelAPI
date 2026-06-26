-- Adds generic activity campaigns for welfare activities and consumption lottery.

CREATE TABLE IF NOT EXISTS activity_campaigns (
    id BIGSERIAL PRIMARY KEY,
    type VARCHAR(50) NOT NULL,
    name VARCHAR(120) NOT NULL,
    description TEXT,
    cover_url TEXT,
    status VARCHAR(20) NOT NULL DEFAULT 'draft',
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NOT NULL,
    draw_at TIMESTAMPTZ,
    timezone VARCHAR(64) NOT NULL DEFAULT 'Asia/Shanghai',
    public_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    rule_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    display_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    updated_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT activity_campaigns_type_check
        CHECK (type IN ('consumption_lottery')),
    CONSTRAINT activity_campaigns_status_check
        CHECK (status IN ('draft', 'active', 'paused', 'ended')),
    CONSTRAINT activity_campaigns_window_check
        CHECK (ends_at > starts_at),
    CONSTRAINT activity_campaigns_draw_window_check
        CHECK (draw_at IS NULL OR draw_at >= starts_at)
);

CREATE INDEX IF NOT EXISTS idx_activity_campaigns_public_window
    ON activity_campaigns (public_enabled, status, starts_at, ends_at, sort_order);

CREATE INDEX IF NOT EXISTS idx_activity_campaigns_draw_at
    ON activity_campaigns (draw_at)
    WHERE draw_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS activity_prizes (
    id BIGSERIAL PRIMARY KEY,
    campaign_id BIGINT NOT NULL REFERENCES activity_campaigns(id) ON DELETE CASCADE,
    name VARCHAR(120) NOT NULL,
    description TEXT,
    prize_type VARCHAR(50) NOT NULL,
    amount DECIMAL(20, 10) NOT NULL DEFAULT 0,
    quantity INTEGER NOT NULL DEFAULT 1,
    weight DECIMAL(20, 10) NOT NULL DEFAULT 1,
    requires_claim_info BOOLEAN NOT NULL DEFAULT FALSE,
    claim_fields JSONB NOT NULL DEFAULT '[]'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT activity_prizes_type_check
        CHECK (prize_type IN ('balance', 'points', 'load_factor_credits', 'manual')),
    CONSTRAINT activity_prizes_amount_check
        CHECK (amount >= 0),
    CONSTRAINT activity_prizes_quantity_check
        CHECK (quantity > 0),
    CONSTRAINT activity_prizes_weight_check
        CHECK (weight > 0)
);

CREATE INDEX IF NOT EXISTS idx_activity_prizes_campaign
    ON activity_prizes (campaign_id, enabled, sort_order, id);

CREATE TABLE IF NOT EXISTS activity_draws (
    id BIGSERIAL PRIMARY KEY,
    campaign_id BIGINT NOT NULL REFERENCES activity_campaigns(id) ON DELETE CASCADE,
    draw_at TIMESTAMPTZ NOT NULL,
    snapshot_start_at TIMESTAMPTZ NOT NULL,
    snapshot_end_at TIMESTAMPTZ NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'completed',
    total_users INTEGER NOT NULL DEFAULT 0,
    total_tickets INTEGER NOT NULL DEFAULT 0,
    winner_count INTEGER NOT NULL DEFAULT 0,
    seed TEXT NOT NULL DEFAULT '',
    executed_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    executed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT activity_draws_status_check
        CHECK (status IN ('completed', 'failed')),
    CONSTRAINT activity_draws_snapshot_window_check
        CHECK (snapshot_end_at > snapshot_start_at)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_activity_draws_campaign_draw_at_unique
    ON activity_draws (campaign_id, draw_at);

CREATE INDEX IF NOT EXISTS idx_activity_draws_campaign
    ON activity_draws (campaign_id, executed_at DESC);

CREATE TABLE IF NOT EXISTS activity_entries (
    id BIGSERIAL PRIMARY KEY,
    campaign_id BIGINT NOT NULL REFERENCES activity_campaigns(id) ON DELETE CASCADE,
    draw_id BIGINT REFERENCES activity_draws(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    draw_at TIMESTAMPTZ NOT NULL,
    snapshot_start_at TIMESTAMPTZ NOT NULL,
    snapshot_end_at TIMESTAMPTZ NOT NULL,
    metric_type VARCHAR(50) NOT NULL,
    metric_value DECIMAL(20, 10) NOT NULL DEFAULT 0,
    ticket_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT activity_entries_metric_check
        CHECK (metric_type IN ('api_cost_amount', 'api_request_count')),
    CONSTRAINT activity_entries_metric_value_check
        CHECK (metric_value >= 0),
    CONSTRAINT activity_entries_ticket_count_check
        CHECK (ticket_count >= 0),
    CONSTRAINT activity_entries_snapshot_window_check
        CHECK (snapshot_end_at > snapshot_start_at)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_activity_entries_campaign_user_draw_unique
    ON activity_entries (campaign_id, user_id, draw_at);

CREATE INDEX IF NOT EXISTS idx_activity_entries_draw
    ON activity_entries (draw_id, ticket_count DESC);

CREATE TABLE IF NOT EXISTS activity_winners (
    id BIGSERIAL PRIMARY KEY,
    campaign_id BIGINT NOT NULL REFERENCES activity_campaigns(id) ON DELETE CASCADE,
    draw_id BIGINT NOT NULL REFERENCES activity_draws(id) ON DELETE CASCADE,
    prize_id BIGINT REFERENCES activity_prizes(id) ON DELETE SET NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    prize_name VARCHAR(120) NOT NULL,
    prize_type VARCHAR(50) NOT NULL,
    prize_amount DECIMAL(20, 10) NOT NULL DEFAULT 0,
    ticket_count INTEGER NOT NULL DEFAULT 0,
    masked_user VARCHAR(160) NOT NULL DEFAULT '',
    status VARCHAR(30) NOT NULL DEFAULT 'pending_delivery',
    claim_status VARCHAR(30) NOT NULL DEFAULT 'not_required',
    claim_info_encrypted TEXT,
    claim_submitted_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    admin_note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT activity_winners_prize_type_check
        CHECK (prize_type IN ('balance', 'points', 'load_factor_credits', 'manual')),
    CONSTRAINT activity_winners_prize_amount_check
        CHECK (prize_amount >= 0),
    CONSTRAINT activity_winners_ticket_count_check
        CHECK (ticket_count >= 0),
    CONSTRAINT activity_winners_status_check
        CHECK (status IN ('pending_claim', 'pending_delivery', 'delivered', 'rejected', 'expired')),
    CONSTRAINT activity_winners_claim_status_check
        CHECK (claim_status IN ('not_required', 'pending', 'submitted'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_activity_winners_draw_user_unique
    ON activity_winners (draw_id, user_id);

CREATE INDEX IF NOT EXISTS idx_activity_winners_campaign_time
    ON activity_winners (campaign_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_activity_winners_user_time
    ON activity_winners (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_activity_winners_public_time
    ON activity_winners (created_at DESC)
    WHERE status IN ('pending_claim', 'pending_delivery', 'delivered');
