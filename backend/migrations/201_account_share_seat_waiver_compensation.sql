ALTER TABLE account_share_mode_settlement_entries
    ADD COLUMN IF NOT EXISTS waiver_evaluated_at TIMESTAMPTZ;

UPDATE account_share_mode_settlement_entries
SET waiver_evaluated_at = created_at
WHERE settlement_type = 'seat_charge'
    AND waiver_evaluated_at IS NULL;
