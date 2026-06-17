DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_memberships_ended_reason_chk'
    ) THEN
        ALTER TABLE account_share_memberships
            DROP CONSTRAINT account_share_memberships_ended_reason_chk;
    END IF;

    ALTER TABLE account_share_memberships
        ADD CONSTRAINT account_share_memberships_ended_reason_chk CHECK (
            ended_reason IS NULL
            OR ended_reason IN (
                'manual',
                'idle_timeout',
                'prepay_insufficient',
                'account_unavailable'
            )
        );
END $$;
