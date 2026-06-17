ALTER TABLE account_share_listings
    ADD COLUMN IF NOT EXISTS hourly_fee_waiver_minimum NUMERIC(20,8) NOT NULL DEFAULT 0;

ALTER TABLE account_share_memberships
    ADD COLUMN IF NOT EXISTS hourly_fee_waiver_minimum_snapshot NUMERIC(20,8) NOT NULL DEFAULT 0;

UPDATE account_share_memberships m
SET hourly_fee_waiver_minimum_snapshot = l.hourly_fee_waiver_minimum,
    updated_at = NOW()
FROM account_share_listings l
WHERE m.listing_id = l.id
    AND m.hourly_fee_waiver_minimum_snapshot = 0
    AND l.hourly_fee_waiver_minimum > 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_listings_hourly_fee_waiver_minimum_chk'
    ) THEN
        ALTER TABLE account_share_listings
            ADD CONSTRAINT account_share_listings_hourly_fee_waiver_minimum_chk
            CHECK (hourly_fee_waiver_minimum >= 0);
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_memberships_hourly_fee_waiver_minimum_chk'
    ) THEN
        ALTER TABLE account_share_memberships
            ADD CONSTRAINT account_share_memberships_hourly_fee_waiver_minimum_chk
            CHECK (hourly_fee_waiver_minimum_snapshot >= 0);
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_mode_settlement_type_chk'
    ) THEN
        ALTER TABLE account_share_mode_settlement_entries
            DROP CONSTRAINT account_share_mode_settlement_type_chk;
    END IF;

    ALTER TABLE account_share_mode_settlement_entries
        ADD CONSTRAINT account_share_mode_settlement_type_chk CHECK (
            settlement_type IN ('usage_request', 'seat_charge', 'seat_refund', 'seat_waiver_refund')
        );
END $$;

ALTER TABLE account_share_mode_settlement_entries
    ADD COLUMN IF NOT EXISTS waiver_minimum_snapshot NUMERIC(20,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS waiver_required_amount NUMERIC(20,10) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS waiver_usage_amount NUMERIC(20,10) NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_mode_settlement_waiver_nonnegative_chk'
    ) THEN
        ALTER TABLE account_share_mode_settlement_entries
            ADD CONSTRAINT account_share_mode_settlement_waiver_nonnegative_chk CHECK (
                waiver_minimum_snapshot >= 0
                AND waiver_required_amount >= 0
                AND waiver_usage_amount >= 0
            );
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_account_share_listings_hourly_fee_waiver
    ON account_share_listings(hourly_fee_waiver_minimum)
    WHERE deleted_at IS NULL AND hourly_fee_waiver_minimum > 0;

CREATE UNIQUE INDEX IF NOT EXISTS uq_account_share_mode_seat_waiver_refund_period
    ON account_share_mode_settlement_entries(membership_id, period_started_at, period_ended_at)
    WHERE settlement_type = 'seat_waiver_refund';
