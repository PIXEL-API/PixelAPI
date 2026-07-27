-- Keep lifecycle end reasons aligned with the application state machine.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE account_share_memberships
    DROP CONSTRAINT IF EXISTS account_share_memberships_ended_reason_chk;

ALTER TABLE account_share_memberships
    ADD CONSTRAINT account_share_memberships_ended_reason_chk
    CHECK (
        ended_reason IS NULL
        OR ended_reason IN (
            'manual',
            'idle_timeout',
            'prepay_insufficient',
            'account_unavailable',
            'queue_expired',
            'room_draining'
        )
    ) NOT VALID;

ALTER TABLE account_share_memberships
    VALIDATE CONSTRAINT account_share_memberships_ended_reason_chk;
