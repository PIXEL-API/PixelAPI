ALTER TABLE proxies
    ADD COLUMN IF NOT EXISTS owner_user_id BIGINT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'proxies_users_owned_proxies'
    ) THEN
        ALTER TABLE proxies
            ADD CONSTRAINT proxies_users_owned_proxies
            FOREIGN KEY (owner_user_id)
            REFERENCES users(id)
            ON DELETE SET NULL;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS proxy_owner_user_id ON proxies(owner_user_id);
