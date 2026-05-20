ALTER TABLE quota_reservations
    ADD COLUMN IF NOT EXISTS reserved_requests BIGINT NOT NULL DEFAULT 1;

ALTER TABLE quota_reservations
    ADD COLUMN IF NOT EXISTS reserved_tokens BIGINT NOT NULL DEFAULT 0;

ALTER TABLE quota_reservations
    ADD COLUMN IF NOT EXISTS active_request_units BIGINT NOT NULL DEFAULT 1;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'quota_reservations_reserved_requests_check'
    ) THEN
        ALTER TABLE quota_reservations
            ADD CONSTRAINT quota_reservations_reserved_requests_check CHECK (reserved_requests >= 0);
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'quota_reservations_reserved_tokens_check'
    ) THEN
        ALTER TABLE quota_reservations
            ADD CONSTRAINT quota_reservations_reserved_tokens_check CHECK (reserved_tokens >= 0);
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'quota_reservations_active_request_units_check'
    ) THEN
        ALTER TABLE quota_reservations
            ADD CONSTRAINT quota_reservations_active_request_units_check CHECK (active_request_units >= 0);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_quota_reservations_lease_active
    ON quota_reservations (lease_id, expires_at)
    WHERE status = 'reserved';
