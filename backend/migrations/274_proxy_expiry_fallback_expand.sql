SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE proxies ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ NULL;
ALTER TABLE proxies ADD COLUMN IF NOT EXISTS fallback_mode VARCHAR(20) NOT NULL DEFAULT 'none';
ALTER TABLE proxies ADD COLUMN IF NOT EXISTS backup_proxy_id BIGINT NULL;
ALTER TABLE proxies ADD COLUMN IF NOT EXISTS expiry_warn_days INT NOT NULL DEFAULT 7;

ALTER TABLE accounts ADD COLUMN IF NOT EXISTS proxy_fallback_origin_id BIGINT NULL;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'proxies_fallback_mode_check') THEN
        ALTER TABLE proxies
            ADD CONSTRAINT proxies_fallback_mode_check
            CHECK (fallback_mode IN ('none', 'direct', 'proxy')) NOT VALID;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'proxies_expiry_warn_days_check') THEN
        ALTER TABLE proxies
            ADD CONSTRAINT proxies_expiry_warn_days_check
            CHECK (expiry_warn_days >= 0) NOT VALID;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'proxies_backup_proxy_not_self_check') THEN
        ALTER TABLE proxies
            ADD CONSTRAINT proxies_backup_proxy_not_self_check
            CHECK (backup_proxy_id IS NULL OR backup_proxy_id <> id) NOT VALID;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'proxies_proxies_fallback_sources') THEN
        ALTER TABLE proxies
            ADD CONSTRAINT proxies_proxies_fallback_sources
            FOREIGN KEY (backup_proxy_id) REFERENCES proxies(id) ON DELETE SET NULL NOT VALID;
    END IF;
END
$$;
