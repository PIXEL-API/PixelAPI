-- Retire reservations without rewriting historical wallet or settlement entries.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '60s';

-- Queued memberships have never prepaid, or were refunded when returning to
-- the queue. Refuse an unexpected outstanding prepayment rather than discard it.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM account_share_memberships
        WHERE status = 'queued'
          AND paid_until IS NOT NULL
          AND (billed_until IS NULL OR paid_until > billed_until)
          AND hourly_rate_snapshot > 0
    ) THEN
        RAISE EXCEPTION 'queued account-share membership has unsettled prepayment';
    END IF;
END
$$;

ALTER TABLE account_share_memberships
    DROP CONSTRAINT IF EXISTS account_share_memberships_ended_reason_chk;
ALTER TABLE account_share_memberships
    ADD CONSTRAINT account_share_memberships_ended_reason_chk CHECK (
        ended_reason IS NULL OR ended_reason IN (
            'manual', 'idle_timeout', 'prepay_insufficient', 'account_unavailable',
            'queue_expired', 'room_draining', 'queue_removed'
        )
    ) NOT VALID;

UPDATE account_share_membership_account_bindings AS binding
SET unbound_at = GREATEST(binding.bound_at, NOW()),
    unbound_by_user_id = NULL,
    unbound_by_role = 'system',
    unbind_reason = 'queue_removed'
FROM account_share_memberships AS membership
WHERE membership.id = binding.membership_id
  AND membership.status = 'queued'
  AND binding.unbound_at IS NULL;

UPDATE account_share_memberships
SET status = 'ended',
    ended_at = GREATEST(joined_at, COALESCE(dispatch_failed_at, NOW())),
    ended_reason = 'queue_removed',
    queue_expires_at = NULL,
    dispatch_cooldown_until = NULL,
    settlement_status = CASE WHEN billed_until IS NULL THEN 'not_required' ELSE 'settled' END,
    updated_at = NOW()
WHERE status = 'queued';

ALTER TABLE account_share_memberships
    DROP CONSTRAINT IF EXISTS account_share_memberships_status_chk;
ALTER TABLE account_share_memberships
    ADD CONSTRAINT account_share_memberships_status_chk
    CHECK (status IN ('active', 'ending', 'ended')) NOT VALID;
ALTER TABLE account_share_memberships
    VALIDATE CONSTRAINT account_share_memberships_status_chk;
ALTER TABLE account_share_memberships
    VALIDATE CONSTRAINT account_share_memberships_ended_reason_chk;

-- Session locks are retired; their local FK/index dependencies are removed with
-- the columns. Do not use CASCADE, so any unexpected external dependency fails.
ALTER TABLE account_share_listings
    DROP COLUMN IF EXISTS edit_session_id,
    DROP COLUMN IF EXISTS editing_by_user_id,
    DROP COLUMN IF EXISTS editing_started_at,
    DROP COLUMN IF EXISTS editing_expires_at;

-- A signed join intent is bound to exactly one membership. Legacy rows remain
-- NULL; neither their historical terms nor their wallet entries are rewritten.
ALTER TABLE account_share_memberships
    ADD COLUMN IF NOT EXISTS join_intent_nonce VARCHAR(64);
CREATE UNIQUE INDEX IF NOT EXISTS uq_account_share_memberships_join_intent
    ON account_share_memberships (consumer_user_id, join_intent_nonce)
    WHERE join_intent_nonce IS NOT NULL;
