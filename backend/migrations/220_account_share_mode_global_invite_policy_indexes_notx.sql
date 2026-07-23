CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_account_share_mode_settlement_inviter
    ON account_share_mode_settlement_entries(inviter_user_id, created_at DESC)
    WHERE inviter_user_id IS NOT NULL;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_account_share_mode_settlement_reversal
    ON account_share_mode_settlement_entries(reversal_of_settlement_id)
    WHERE reversal_of_settlement_id IS NOT NULL;
