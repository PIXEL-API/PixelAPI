-- Unify account-share marketplace settlements with the global public-pool
-- owner/inviter/platform policy. This is the expand phase of an online
-- migration: keep the legacy policy table available to the previous release
-- throughout the rollback window, and defer historical constraint validation.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE account_share_mode_settlement_entries
    ADD COLUMN IF NOT EXISTS policy_id BIGINT,
    ADD COLUMN IF NOT EXISTS policy_version INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS inviter_user_id BIGINT,
    ADD COLUMN IF NOT EXISTS invite_bound_at_snapshot TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS invite_expires_at_snapshot TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS invite_share_ratio_snapshot NUMERIC(10,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS invite_credit NUMERIC(20,10) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS reversal_of_settlement_id BIGINT;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_mode_settlement_policy_fk'
          AND conrelid = 'account_share_mode_settlement_entries'::regclass
    ) THEN
        ALTER TABLE account_share_mode_settlement_entries
            ADD CONSTRAINT account_share_mode_settlement_policy_fk
            FOREIGN KEY (policy_id)
            REFERENCES account_share_policies(id)
            ON DELETE SET NULL
            NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_mode_settlement_inviter_fk'
          AND conrelid = 'account_share_mode_settlement_entries'::regclass
    ) THEN
        ALTER TABLE account_share_mode_settlement_entries
            ADD CONSTRAINT account_share_mode_settlement_inviter_fk
            FOREIGN KEY (inviter_user_id)
            REFERENCES users(id)
            ON DELETE SET NULL
            NOT VALID;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_mode_settlement_reversal_fk'
          AND conrelid = 'account_share_mode_settlement_entries'::regclass
    ) THEN
        ALTER TABLE account_share_mode_settlement_entries
            ADD CONSTRAINT account_share_mode_settlement_reversal_fk
            FOREIGN KEY (reversal_of_settlement_id)
            REFERENCES account_share_mode_settlement_entries(id)
            ON DELETE RESTRICT
            NOT VALID;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_mode_settlement_invite_amounts_chk'
          AND conrelid = 'account_share_mode_settlement_entries'::regclass
    ) THEN
        ALTER TABLE account_share_mode_settlement_entries
            ADD CONSTRAINT account_share_mode_settlement_invite_amounts_chk CHECK (
                policy_version >= 0
                AND invite_share_ratio_snapshot >= 0
                AND invite_share_ratio_snapshot <= 1
                AND owner_share_ratio_snapshot + invite_share_ratio_snapshot + platform_share_ratio_snapshot <= 1.000001
                AND invite_credit >= 0
                AND (invite_credit = 0 OR inviter_user_id IS NOT NULL)
                AND (
                    reversal_of_settlement_id IS NULL
                    OR (
                        settlement_type = 'seat_waiver_refund'
                        AND ABS(owner_credit + invite_credit + platform_credit - refund_amount) <= 0.0000000001
                    )
                )
                AND (
                    (
                        settlement_type = 'seat_waiver_refund'
                        AND owner_credit + invite_credit + platform_credit <= refund_amount + 0.0000000001
                    )
                    OR (
                        settlement_type <> 'seat_waiver_refund'
                        AND owner_credit + invite_credit + platform_credit <= total_charge + 0.0000000001
                    )
                )
            ) NOT VALID;
    END IF;
END $$;

COMMENT ON COLUMN account_share_mode_settlement_entries.policy_id
    IS 'Global account_share_policies row captured for this settlement';
COMMENT ON COLUMN account_share_mode_settlement_entries.policy_version
    IS 'Global sharing policy version captured for this settlement';
COMMENT ON COLUMN account_share_mode_settlement_entries.inviter_user_id
    IS 'Eligible inviter captured when the charge was settled';
COMMENT ON COLUMN account_share_mode_settlement_entries.invite_bound_at_snapshot
    IS 'Invite binding timestamp captured for settlement audit';
COMMENT ON COLUMN account_share_mode_settlement_entries.invite_expires_at_snapshot
    IS 'Invite reward expiry captured for settlement audit';
COMMENT ON COLUMN account_share_mode_settlement_entries.invite_share_ratio_snapshot
    IS 'Effective inviter ratio; zero when no eligible inviter existed';
COMMENT ON COLUMN account_share_mode_settlement_entries.invite_credit
    IS 'Inviter credit, or the inviter amount reversed by a waiver-refund row';
COMMENT ON COLUMN account_share_mode_settlement_entries.reversal_of_settlement_id
    IS 'Original seat_charge settlement reversed by this waiver-refund row';

COMMENT ON COLUMN user_affiliate_ledger.action
    IS 'accrue|transfer|reverse';
