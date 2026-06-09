-- Add per-proxy account binding limit.

ALTER TABLE proxies
    ADD COLUMN IF NOT EXISTS max_accounts INT NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'chk_proxies_max_accounts_non_negative'
    ) THEN
        ALTER TABLE proxies
            ADD CONSTRAINT chk_proxies_max_accounts_non_negative
            CHECK (max_accounts >= 0);
    END IF;
END $$;
