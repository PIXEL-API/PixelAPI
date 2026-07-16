CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_user_balance_ledger_seat_membership_created_at
    ON user_balance_ledger (
        ((NULLIF(metadata->>'membership_id', ''))::bigint),
        created_at
    )
    INCLUDE (direction, amount)
    WHERE reason IN (
        'account_share_mode_seat_prepay',
        'account_share_mode_seat_refund',
        'account_share_mode_seat_waiver_refund'
    )
        AND NULLIF(metadata->>'membership_id', '') IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_user_balance_ledger_seat_user_created_at
    ON user_balance_ledger (user_id, created_at)
    INCLUDE (direction, amount)
    WHERE reason IN (
        'account_share_mode_seat_prepay',
        'account_share_mode_seat_refund',
        'account_share_mode_seat_waiver_refund'
    );

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_account_share_memberships_api_key_id
    ON account_share_memberships (api_key_id, id);
