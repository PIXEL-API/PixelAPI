CREATE INDEX CONCURRENTLY IF NOT EXISTS proxies_expires_at_idx
    ON proxies (expires_at);

CREATE INDEX CONCURRENTLY IF NOT EXISTS proxies_backup_proxy_id_idx
    ON proxies (backup_proxy_id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS accounts_proxy_fallback_origin_id_idx
    ON accounts (proxy_fallback_origin_id);
