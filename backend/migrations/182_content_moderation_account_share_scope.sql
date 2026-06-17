-- 风控中心账号模式作用域日志字段

ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS scope_type VARCHAR(32) NOT NULL DEFAULT 'group';
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS account_share_listing_id BIGINT REFERENCES account_share_listings(id) ON DELETE SET NULL;
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL;
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS owner_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS consumer_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS membership_id BIGINT REFERENCES account_share_memberships(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_scope_created_at ON content_moderation_logs(scope_type, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_account_share_listing_created_at ON content_moderation_logs(account_share_listing_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_consumer_created_at ON content_moderation_logs(consumer_user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_owner_created_at ON content_moderation_logs(owner_user_id, created_at DESC);
