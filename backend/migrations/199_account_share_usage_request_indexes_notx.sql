CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_account_share_mode_usage_request_membership_period
    ON account_share_mode_settlement_entries(membership_id, period_started_at, period_ended_at, created_at)
    WHERE settlement_type = 'usage_request';
