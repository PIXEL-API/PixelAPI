-- Keep the legacy release from recreating public non-approved account states
-- while migration 225 performs its long-running validation. The guard is a
-- temporary expand/contract compatibility object and is removed by migration
-- 226 after the old release has drained.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '5min';

LOCK TABLE
    accounts,
    account_groups,
    groups,
    account_share_listings,
    account_share_memberships,
    account_external_placements
IN SHARE ROW EXCLUSIVE MODE;

CREATE OR REPLACE FUNCTION account_share_online_guard_pending_public_private()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF NEW.deleted_at IS NULL
       AND NEW.owner_user_id IS NOT NULL
       AND NEW.share_mode = 'public'
       AND NEW.share_status <> 'approved' THEN
        NEW.share_mode := 'private';
        NEW.share_status := 'approved';
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_account_share_online_guard_pending_public_private
    ON accounts;
CREATE TRIGGER trg_account_share_online_guard_pending_public_private
BEFORE INSERT OR UPDATE OF owner_user_id, share_mode, share_status, deleted_at
ON accounts
FOR EACH ROW
EXECUTE FUNCTION account_share_online_guard_pending_public_private();

CREATE TEMP TABLE account_share_pending_private_guard_targets
ON COMMIT DROP
AS
SELECT
    account.id AS account_id,
    account.owner_user_id,
    account.platform
FROM accounts account
WHERE account.deleted_at IS NULL
  AND account.owner_user_id IS NOT NULL
  AND account.share_mode = 'public'
  AND account.share_status <> 'approved';

CREATE UNIQUE INDEX account_share_pending_private_guard_targets_account_id
    ON account_share_pending_private_guard_targets(account_id);

DO $$
DECLARE
    target_count BIGINT;
    updated_count BIGINT;
BEGIN
    SELECT COUNT(*)
    INTO target_count
    FROM account_share_pending_private_guard_targets;

    IF EXISTS (
        SELECT 1
        FROM account_share_pending_private_guard_targets target
        WHERE (
            SELECT COUNT(*)
            FROM groups private_group
            WHERE private_group.owner_user_id = target.owner_user_id
              AND private_group.platform = target.platform
              AND private_group.scope = 'user_private'
              AND private_group.status = 'active'
              AND private_group.deleted_at IS NULL
              AND COALESCE(private_group.subscription_type, '') <> 'none'
        ) <> 1
    ) THEN
        RAISE EXCEPTION
            'pending public account guard requires exactly one active private group per owner and platform';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM account_share_pending_private_guard_targets target
        WHERE (
            SELECT COUNT(*)
            FROM account_groups account_group
            JOIN groups private_group ON private_group.id = account_group.group_id
            WHERE account_group.account_id = target.account_id
              AND private_group.owner_user_id = target.owner_user_id
              AND private_group.platform = target.platform
              AND private_group.scope = 'user_private'
              AND private_group.status = 'active'
              AND private_group.deleted_at IS NULL
              AND COALESCE(private_group.subscription_type, '') <> 'none'
        ) <> 1
    ) THEN
        RAISE EXCEPTION
            'pending public account guard requires every account to be bound to its active private group';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM account_share_pending_private_guard_targets target
        JOIN account_groups account_group
          ON account_group.account_id = target.account_id
        LEFT JOIN groups private_group
          ON private_group.id = account_group.group_id
         AND private_group.owner_user_id = target.owner_user_id
         AND private_group.platform = target.platform
         AND private_group.scope = 'user_private'
         AND private_group.status = 'active'
         AND private_group.deleted_at IS NULL
         AND COALESCE(private_group.subscription_type, '') <> 'none'
        WHERE private_group.id IS NULL
    ) THEN
        RAISE EXCEPTION
            'pending public account guard refuses accounts bound to non-private groups';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM account_share_pending_private_guard_targets target
        JOIN account_share_listings listing
          ON listing.account_id = target.account_id
         AND listing.deleted_at IS NULL
    ) THEN
        RAISE EXCEPTION
            'pending public account guard refuses accounts attached to a legacy room';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM account_share_pending_private_guard_targets target
        JOIN account_share_memberships membership
          ON membership.account_id = target.account_id
         AND membership.deleted_at IS NULL
         AND membership.status IN ('active', 'queued')
    ) THEN
        RAISE EXCEPTION
            'pending public account guard refuses accounts with active room memberships';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM account_share_pending_private_guard_targets target
        JOIN account_external_placements placement
          ON placement.account_id = target.account_id
    ) THEN
        RAISE EXCEPTION
            'pending public account guard refuses accounts with external placements';
    END IF;

    UPDATE accounts account
    SET share_mode = 'private',
        share_status = 'approved',
        updated_at = NOW()
    FROM account_share_pending_private_guard_targets target
    WHERE account.id = target.account_id;

    GET DIAGNOSTICS updated_count = ROW_COUNT;
    IF updated_count <> target_count THEN
        RAISE EXCEPTION
            'pending public account guard updated % of % audited accounts',
            updated_count,
            target_count;
    END IF;

    INSERT INTO scheduler_outbox (
        event_type,
        account_id,
        group_id,
        payload,
        dedup_key
    )
    SELECT
        'account_changed',
        target.account_id,
        NULL,
        NULL,
        'scheduler_outbox:pending-public-private-guard:' || target.account_id::TEXT
    FROM account_share_pending_private_guard_targets target
    ON CONFLICT (dedup_key) WHERE dedup_key IS NOT NULL DO NOTHING;

    IF EXISTS (
        SELECT 1
        FROM accounts account
        WHERE account.deleted_at IS NULL
          AND account.owner_user_id IS NOT NULL
          AND account.share_mode = 'public'
          AND account.share_status <> 'approved'
    ) THEN
        RAISE EXCEPTION
            'public non-approved accounts remain after installing the migration guard';
    END IF;
END
$$;
