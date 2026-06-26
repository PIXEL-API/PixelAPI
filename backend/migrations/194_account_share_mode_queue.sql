ALTER TABLE account_share_memberships
    ADD COLUMN IF NOT EXISTS queue_rank INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS dispatch_failed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS dispatch_cooldown_until TIMESTAMPTZ;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_memberships_status_chk'
    ) THEN
        ALTER TABLE account_share_memberships
            DROP CONSTRAINT account_share_memberships_status_chk;
    END IF;

    ALTER TABLE account_share_memberships
        ADD CONSTRAINT account_share_memberships_status_chk CHECK (status IN ('active', 'queued', 'ended'));

    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_memberships_end_chk'
    ) THEN
        ALTER TABLE account_share_memberships
            DROP CONSTRAINT account_share_memberships_end_chk;
    END IF;

    ALTER TABLE account_share_memberships
        ADD CONSTRAINT account_share_memberships_end_chk CHECK (
            (status IN ('active', 'queued') AND ended_at IS NULL)
            OR (status = 'ended' AND ended_at IS NOT NULL)
        );
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS uq_account_share_memberships_active_or_queued_listing_consumer
    ON account_share_memberships(listing_id, consumer_user_id)
    WHERE status IN ('active', 'queued') AND deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_account_share_memberships_queue_rank
    ON account_share_memberships(api_key_id, queue_rank)
    WHERE status IN ('active', 'queued') AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_account_share_memberships_queue_request
    ON account_share_memberships(api_key_id, consumer_user_id, status, queue_rank)
    WHERE status IN ('active', 'queued') AND deleted_at IS NULL;
