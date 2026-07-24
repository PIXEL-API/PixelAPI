-- Resolve legacy public-share requests that were never approved. These
-- accounts have no external placement or active room membership, so keeping
-- them private preserves their credentials, runtime state, balance and
-- settlement history while allowing the online contract validation to pass.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '5min';

-- Keep the audited target set and its topology stable for this short
-- transaction. Reads and existing requests continue; concurrent writes fail
-- fast at lock acquisition and can safely be retried by the application.
LOCK TABLE
    accounts,
    account_groups,
    groups,
    account_share_listings,
    account_share_memberships,
    account_external_placements
IN SHARE ROW EXCLUSIVE MODE;

CREATE TEMP TABLE account_share_pending_private_targets
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
  AND account.share_status = 'pending';

CREATE UNIQUE INDEX account_share_pending_private_targets_account_id
    ON account_share_pending_private_targets(account_id);

DO $$
DECLARE
    target_count BIGINT;
    updated_count BIGINT;
BEGIN
    SELECT COUNT(*)
    INTO target_count
    FROM account_share_pending_private_targets;

    IF EXISTS (
        SELECT 1
        FROM account_share_pending_private_targets target
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
            'pending public account conversion requires exactly one active private group per owner and platform';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM account_share_pending_private_targets target
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
            'pending public account conversion requires every account to be bound to its active private group';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM account_share_pending_private_targets target
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
            'pending public account conversion refuses accounts bound to non-private groups';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM account_share_pending_private_targets target
        JOIN account_share_listings listing
          ON listing.account_id = target.account_id
         AND listing.deleted_at IS NULL
    ) THEN
        RAISE EXCEPTION
            'pending public account conversion refuses accounts attached to a legacy room';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM account_share_pending_private_targets target
        JOIN account_share_memberships membership
          ON membership.account_id = target.account_id
         AND membership.deleted_at IS NULL
         AND membership.status IN ('active', 'queued')
    ) THEN
        RAISE EXCEPTION
            'pending public account conversion refuses accounts with active room memberships';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM account_share_pending_private_targets target
        JOIN account_external_placements placement
          ON placement.account_id = target.account_id
    ) THEN
        RAISE EXCEPTION
            'pending public account conversion refuses accounts with external placements';
    END IF;

    UPDATE accounts account
    SET share_mode = 'private',
        share_status = 'approved',
        updated_at = NOW()
    FROM account_share_pending_private_targets target
    WHERE account.id = target.account_id;

    GET DIAGNOSTICS updated_count = ROW_COUNT;
    IF updated_count <> target_count THEN
        RAISE EXCEPTION
            'pending public account conversion updated % of % audited accounts',
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
        'scheduler_outbox:pending-public-to-private:' || target.account_id::TEXT
    FROM account_share_pending_private_targets target
    ON CONFLICT (dedup_key) WHERE dedup_key IS NOT NULL DO NOTHING;

    IF EXISTS (
        SELECT 1
        FROM account_share_pending_private_targets target
        JOIN accounts account ON account.id = target.account_id
        WHERE account.share_mode <> 'private'
           OR account.share_status <> 'approved'
    ) THEN
        RAISE EXCEPTION
            'pending public account conversion postcondition failed';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM accounts account
        WHERE account.deleted_at IS NULL
          AND account.owner_user_id IS NOT NULL
          AND account.share_mode = 'public'
          AND account.share_status <> 'approved'
    ) THEN
        RAISE EXCEPTION
            'public non-approved accounts remain after pending conversion';
    END IF;
END
$$;
