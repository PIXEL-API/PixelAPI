-- Contract queued memberships to deferred account selection.
-- Apply only after the legacy binary is no longer a rollback target.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '60s';

CREATE OR REPLACE FUNCTION validate_account_share_membership_room_account()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF NEW.deleted_at IS NULL
       AND NEW.status IN ('active', 'ending')
       AND NOT EXISTS (
           SELECT 1
           FROM public.account_share_room_accounts room_account
           WHERE room_account.listing_id = NEW.listing_id
             AND room_account.account_id = NEW.account_id
             AND (
                 NEW.status = 'ending'
                 OR room_account.state IN ('active', 'draining')
             )
       ) THEN
        RAISE EXCEPTION 'active or ending account-share membership account must belong to its room'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$$;

UPDATE account_share_membership_account_bindings AS binding
SET unbound_at = GREATEST(binding.bound_at, NOW()),
    unbound_by_user_id = NULL,
    unbound_by_role = 'system',
    unbind_reason = 'queued_binding_deferred_migration'
FROM account_share_memberships AS membership
WHERE membership.id = binding.membership_id
  AND membership.status = 'queued'
  AND membership.deleted_at IS NULL
  AND binding.unbound_at IS NULL;

UPDATE account_share_memberships
SET queue_expires_at = COALESCE(queue_expires_at, created_at + INTERVAL '2 hours'),
    account_id = NULL
WHERE status = 'queued'
  AND deleted_at IS NULL;

DO $$
BEGIN
    ALTER TABLE account_share_memberships
        DROP CONSTRAINT IF EXISTS account_share_memberships_account_state_chk;
    ALTER TABLE account_share_memberships
        ADD CONSTRAINT account_share_memberships_account_state_chk
        CHECK (
            deleted_at IS NOT NULL
            OR status = 'ended'
            OR (status = 'queued' AND account_id IS NULL)
            OR (status IN ('active', 'ending') AND account_id IS NOT NULL)
        ) NOT VALID;
END
$$;

ALTER TABLE account_share_memberships
    VALIDATE CONSTRAINT account_share_memberships_account_state_chk;
