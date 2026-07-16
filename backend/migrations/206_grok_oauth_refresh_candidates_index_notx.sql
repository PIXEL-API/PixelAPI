CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_accounts_oauth_refresh_candidates_v2
    ON accounts (priority, id)
    WHERE deleted_at IS NULL
      AND status = 'active'
      AND type = 'oauth'
      AND platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok')
      AND credentials ? 'refresh_token';

DROP INDEX CONCURRENTLY IF EXISTS idx_accounts_oauth_refresh_candidates;
