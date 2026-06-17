CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_scheduler_outbox_pending_dedup_key
    ON scheduler_outbox (dedup_key)
    WHERE dedup_key IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_accounts_autopause_expiry_due
    ON accounts (expires_at)
    WHERE deleted_at IS NULL
      AND schedulable = TRUE
      AND auto_pause_on_expired = TRUE
      AND expires_at IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_accounts_oauth_refresh_candidates
    ON accounts (priority, id)
    WHERE deleted_at IS NULL
      AND status = 'active'
      AND type = 'oauth'
      AND platform IN ('anthropic', 'openai', 'gemini', 'antigravity')
      AND credentials ? 'refresh_token';
