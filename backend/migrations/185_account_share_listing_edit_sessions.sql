ALTER TABLE account_share_listings
    ADD COLUMN IF NOT EXISTS edit_session_id VARCHAR(64),
    ADD COLUMN IF NOT EXISTS editing_by_user_id BIGINT,
    ADD COLUMN IF NOT EXISTS editing_started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS editing_expires_at TIMESTAMPTZ;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_account_share_listings_editing_by_user'
    ) THEN
        ALTER TABLE account_share_listings
            ADD CONSTRAINT fk_account_share_listings_editing_by_user
            FOREIGN KEY (editing_by_user_id) REFERENCES users(id) ON DELETE SET NULL;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_account_share_listings_editing_expires_at
    ON account_share_listings(editing_expires_at)
    WHERE deleted_at IS NULL
        AND editing_expires_at IS NOT NULL;
