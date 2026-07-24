CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_accounts_owner_identity
    ON accounts(id, owner_user_id);

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_accounts_room_identity
    ON accounts(id, owner_user_id, platform, account_level);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_user_affiliate_ledger_share_income
    ON user_affiliate_ledger(user_id, created_at DESC)
    WHERE action IN ('share_accrue', 'share_reverse');
