CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_accounts_owned_openai_org_user_uniq
    ON accounts (
        owner_user_id,
        LOWER(NULLIF(BTRIM(credentials->>'organization_id'), '')),
        NULLIF(BTRIM(credentials->>'chatgpt_user_id'), '')
    )
    WHERE deleted_at IS NULL
      AND owner_user_id IS NOT NULL
      AND platform = 'openai'
      AND type = 'oauth'
      AND NULLIF(BTRIM(credentials->>'organization_id'), '') IS NOT NULL
      AND NULLIF(BTRIM(credentials->>'chatgpt_user_id'), '') IS NOT NULL;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_accounts_owned_openai_org_account_uniq
    ON accounts (
        owner_user_id,
        LOWER(NULLIF(BTRIM(credentials->>'organization_id'), '')),
        NULLIF(BTRIM(credentials->>'chatgpt_account_id'), '')
    )
    WHERE deleted_at IS NULL
      AND owner_user_id IS NOT NULL
      AND platform = 'openai'
      AND type = 'oauth'
      AND NULLIF(BTRIM(credentials->>'organization_id'), '') IS NOT NULL
      AND NULLIF(BTRIM(credentials->>'chatgpt_user_id'), '') IS NULL
      AND NULLIF(BTRIM(credentials->>'chatgpt_account_id'), '') IS NOT NULL;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_accounts_owned_openai_legacy_user_uniq
    ON accounts (
        owner_user_id,
        NULLIF(BTRIM(credentials->>'chatgpt_user_id'), '')
    )
    WHERE deleted_at IS NULL
      AND owner_user_id IS NOT NULL
      AND platform = 'openai'
      AND type = 'oauth'
      AND NULLIF(BTRIM(credentials->>'organization_id'), '') IS NULL
      AND NULLIF(BTRIM(credentials->>'chatgpt_user_id'), '') IS NOT NULL;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_accounts_owned_openai_legacy_account_uniq
    ON accounts (
        owner_user_id,
        NULLIF(BTRIM(credentials->>'chatgpt_account_id'), '')
    )
    WHERE deleted_at IS NULL
      AND owner_user_id IS NOT NULL
      AND platform = 'openai'
      AND type = 'oauth'
      AND NULLIF(BTRIM(credentials->>'organization_id'), '') IS NULL
      AND NULLIF(BTRIM(credentials->>'chatgpt_user_id'), '') IS NULL
      AND NULLIF(BTRIM(credentials->>'chatgpt_account_id'), '') IS NOT NULL;

DROP INDEX CONCURRENTLY IF EXISTS idx_accounts_owned_openai_chatgpt_account_id_uniq;

DROP INDEX CONCURRENTLY IF EXISTS idx_accounts_owned_openai_chatgpt_user_id_uniq;
