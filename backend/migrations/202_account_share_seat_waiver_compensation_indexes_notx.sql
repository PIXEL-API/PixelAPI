CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_account_share_mode_seat_charge_waiver_compensation
    ON account_share_mode_settlement_entries(settlement_type, waiver_evaluated_at, period_ended_at, id)
    WHERE settlement_type = 'seat_charge'
        AND hourly_charge > 0
        AND period_started_at IS NOT NULL
        AND period_ended_at IS NOT NULL;
