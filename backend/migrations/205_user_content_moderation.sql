CREATE TABLE IF NOT EXISTS user_content_moderation_configs (
    id BIGSERIAL PRIMARY KEY,
    owner_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    mode VARCHAR(32) NOT NULL DEFAULT 'observe' CHECK (mode IN ('observe', 'pre_block')),
    provider VARCHAR(64) NOT NULL DEFAULT 'openai' CHECK (provider IN ('openai', 'zhipu')),
    base_url TEXT NOT NULL DEFAULT 'https://api.openai.com',
    model VARCHAR(255) NOT NULL DEFAULT 'omni-moderation-latest',
    api_key_encrypted TEXT NOT NULL DEFAULT '',
    api_key_hash VARCHAR(128) NOT NULL DEFAULT '',
    sample_rate INT NOT NULL DEFAULT 100 CHECK (sample_rate >= 1 AND sample_rate <= 100),
    block_message TEXT NOT NULL DEFAULT '内容审核命中风险规则，请调整输入后重试',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (owner_user_id, account_id)
);

CREATE INDEX IF NOT EXISTS idx_user_content_moderation_configs_owner_account
    ON user_content_moderation_configs(owner_user_id, account_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_content_moderation_configs_api_key_hash
    ON user_content_moderation_configs(api_key_hash)
    WHERE api_key_hash <> '';

CREATE TABLE IF NOT EXISTS user_content_moderation_logs (
    id BIGSERIAL PRIMARY KEY,
    request_id VARCHAR(128) NOT NULL DEFAULT '',
    owner_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    consumer_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    api_key_id BIGINT REFERENCES api_keys(id) ON DELETE SET NULL,
    api_key_name VARCHAR(255) NOT NULL DEFAULT '',
    group_id BIGINT REFERENCES groups(id) ON DELETE SET NULL,
    endpoint VARCHAR(255) NOT NULL DEFAULT '',
    provider VARCHAR(64) NOT NULL DEFAULT 'openai',
    model VARCHAR(255) NOT NULL DEFAULT '',
    mode VARCHAR(32) NOT NULL DEFAULT 'observe',
    action VARCHAR(64) NOT NULL DEFAULT 'allow',
    flagged BOOLEAN NOT NULL DEFAULT FALSE,
    highest_category VARCHAR(128) NOT NULL DEFAULT '',
    highest_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    category_scores JSONB NOT NULL DEFAULT '{}'::jsonb,
    sampled BOOLEAN NOT NULL DEFAULT TRUE,
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_content_moderation_logs_owner_account_created
    ON user_content_moderation_logs(owner_user_id, account_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_user_content_moderation_logs_owner_created
    ON user_content_moderation_logs(owner_user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_user_content_moderation_logs_flagged_created
    ON user_content_moderation_logs(flagged, created_at DESC);
