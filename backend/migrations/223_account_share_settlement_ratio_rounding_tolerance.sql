-- Legacy seat settlements stored independently rounded six-decimal ratio
-- snapshots. Two complementary ratios can therefore sum to 1.000002 even
-- though the separately constrained credit amounts differ by at most 1e-10.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE account_share_mode_settlement_entries
    DROP CONSTRAINT IF EXISTS account_share_mode_settlement_invite_amounts_chk;

ALTER TABLE account_share_mode_settlement_entries
    ADD CONSTRAINT account_share_mode_settlement_invite_amounts_chk CHECK (
        policy_version >= 0
        AND invite_share_ratio_snapshot >= 0
        AND invite_share_ratio_snapshot <= 1
        AND owner_share_ratio_snapshot
            + invite_share_ratio_snapshot
            + platform_share_ratio_snapshot <= 1.000002
        AND invite_credit >= 0
        AND (invite_credit = 0 OR inviter_user_id IS NOT NULL)
        AND (
            reversal_of_settlement_id IS NULL
            OR (
                settlement_type = 'seat_waiver_refund'
                AND ABS(
                    owner_credit
                    + invite_credit
                    + platform_credit
                    - refund_amount
                ) <= 0.0000000001
            )
        )
        AND (
            (
                settlement_type = 'seat_waiver_refund'
                AND owner_credit + invite_credit + platform_credit
                    <= refund_amount + 0.0000000001
            )
            OR (
                settlement_type <> 'seat_waiver_refund'
                AND owner_credit + invite_credit + platform_credit
                    <= total_charge + 0.0000000001
            )
        )
    ) NOT VALID;
