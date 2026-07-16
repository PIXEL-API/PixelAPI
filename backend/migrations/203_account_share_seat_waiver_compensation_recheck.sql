UPDATE account_share_mode_settlement_entries sc
SET waiver_evaluated_at = NULL
FROM account_share_memberships m
WHERE sc.settlement_type = 'seat_charge'
    AND m.id = sc.membership_id
    AND sc.hourly_charge > 0
    AND sc.period_started_at IS NOT NULL
    AND sc.period_ended_at IS NOT NULL
    AND sc.waiver_evaluated_at IS NOT NULL
    AND COALESCE(NULLIF(sc.waiver_minimum_snapshot, 0), m.hourly_fee_waiver_minimum_snapshot) > 0
    AND NOT EXISTS (
        SELECT 1
        FROM account_share_mode_settlement_entries wr
        WHERE wr.membership_id = sc.membership_id
            AND wr.settlement_type = 'seat_waiver_refund'
            AND wr.period_started_at = sc.period_started_at
            AND wr.period_ended_at = sc.period_ended_at
    );
