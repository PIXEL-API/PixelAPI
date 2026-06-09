CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_account_share_settlement_created_id
    ON account_share_settlement_entries (created_at DESC, id DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_account_share_settlement_status_created_id
    ON account_share_settlement_entries (status, created_at DESC, id DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_payment_orders_refund_at
    ON payment_orders (refund_at)
    WHERE refund_at IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_payment_orders_status_created_at
    ON payment_orders (status, created_at);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_user_balance_ledger_reason_direction_created_at
    ON user_balance_ledger (reason, direction, created_at DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_points_ledger_created_at
    ON points_ledger (created_at DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_user_affiliate_ledger_created_action
    ON user_affiliate_ledger (created_at DESC, action);
