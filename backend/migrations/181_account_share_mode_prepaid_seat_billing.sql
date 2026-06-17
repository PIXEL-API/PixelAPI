ALTER TABLE account_share_memberships
    ADD COLUMN IF NOT EXISTS paid_until TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS billed_until TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS hourly_rate_snapshot NUMERIC(20,8) NOT NULL DEFAULT 0;

UPDATE account_share_memberships m
SET hourly_rate_snapshot = l.hourly_rate,
    billed_until = COALESCE(m.billed_until, NOW()),
    paid_until = COALESCE(m.paid_until, NOW()),
    updated_at = NOW()
FROM account_share_listings l
WHERE m.listing_id = l.id
    AND m.status = 'active'
    AND m.deleted_at IS NULL
    AND l.hourly_rate > 0
    AND (
        m.hourly_rate_snapshot <= 0
        OR m.billed_until IS NULL
        OR m.paid_until IS NULL
    );

CREATE INDEX IF NOT EXISTS idx_account_share_memberships_paid_until
    ON account_share_memberships(paid_until)
    WHERE status = 'active' AND deleted_at IS NULL;

ALTER TABLE account_share_mode_settlement_entries
    ADD COLUMN IF NOT EXISTS settlement_type VARCHAR(32) NOT NULL DEFAULT 'usage_request',
    ADD COLUMN IF NOT EXISTS period_started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS period_ended_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS refund_amount NUMERIC(20,10) NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_mode_settlement_type_chk'
    ) THEN
        ALTER TABLE account_share_mode_settlement_entries
            ADD CONSTRAINT account_share_mode_settlement_type_chk CHECK (
                settlement_type IN ('usage_request', 'seat_charge', 'seat_refund')
            );
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_mode_settlement_refund_nonnegative_chk'
    ) THEN
        ALTER TABLE account_share_mode_settlement_entries
            ADD CONSTRAINT account_share_mode_settlement_refund_nonnegative_chk CHECK (
                refund_amount >= 0
            );
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_account_share_mode_settlement_type_created
    ON account_share_mode_settlement_entries(settlement_type, created_at DESC);
