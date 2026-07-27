-- Permit both legacy eager queue bindings and deferred queue bindings.
-- The later contract migration clears queued account_id values only after the
-- previous binary is no longer a rollback target.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE account_share_memberships
    ALTER COLUMN account_id DROP NOT NULL;

DO $$
BEGIN
    ALTER TABLE account_share_memberships
        DROP CONSTRAINT IF EXISTS account_share_memberships_account_state_chk;
    ALTER TABLE account_share_memberships
        ADD CONSTRAINT account_share_memberships_account_state_chk
        CHECK (
            deleted_at IS NOT NULL
            OR status = 'ended'
            OR status = 'queued'
            OR (status IN ('active', 'ending') AND account_id IS NOT NULL)
        ) NOT VALID;
END
$$;

ALTER TABLE account_share_memberships
    VALIDATE CONSTRAINT account_share_memberships_account_state_chk;

COMMENT ON COLUMN account_share_memberships.account_id
    IS 'Legacy queued rows may remain eagerly bound during expand; deferred queue binding is enforced by a later contract migration';
