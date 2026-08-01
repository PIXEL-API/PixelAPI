-- Identity-scope lookup indexes for account usage stats
-- (resolveAccountUsageStatsScopeIDs two-step rewrite).
-- All three indexes intentionally omit a deleted_at predicate: the
-- usage-stats identity scope also covers soft-deleted account rows.

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_accounts_owner_platform_type
    ON public.accounts (owner_user_id, platform, type);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_accounts_openai_identity_user
    ON public.accounts (owner_user_id, (NULLIF(BTRIM(credentials->>'chatgpt_user_id'), '')))
    WHERE platform = 'openai' AND type = 'oauth';

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_accounts_openai_identity_account
    ON public.accounts (owner_user_id, (NULLIF(BTRIM(credentials->>'chatgpt_account_id'), '')))
    WHERE platform = 'openai' AND type = 'oauth';
