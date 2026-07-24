-- Reconcile approved public accounts created by the legacy release while the
-- long migration 225 validation is running. A valid public-group binding gets
-- its derived placement; an unambiguous private-only orphan is converted back
-- to private. Ambiguous topology fails fast instead of guessing.
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

CREATE OR REPLACE FUNCTION account_share_online_guard_orphan_approved_public()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
DECLARE
    current_account public.accounts%ROWTYPE;
    private_group_count INTEGER;
    private_binding_count INTEGER;
    non_private_binding_count INTEGER;
BEGIN
    SELECT *
    INTO current_account
    FROM public.accounts
    WHERE id = NEW.id;

    IF NOT FOUND
       OR current_account.deleted_at IS NOT NULL
       OR current_account.owner_user_id IS NULL
       OR current_account.share_mode <> 'public'
       OR current_account.share_status <> 'approved' THEN
        RETURN NEW;
    END IF;

    PERFORM public.account_share_online_compat_public_placement(current_account.id);
    IF EXISTS (
        SELECT 1
        FROM public.account_external_placements placement
        WHERE placement.account_id = current_account.id
          AND placement.placement_type = 'public_pool'
    ) THEN
        INSERT INTO public.scheduler_outbox (
            event_type, account_id, group_id, payload, dedup_key
        )
        VALUES (
            'account_changed',
            current_account.id,
            NULL,
            NULL,
            'scheduler_outbox:approved-public-orphan-guard:' || current_account.id::TEXT
        )
        ON CONFLICT (dedup_key) WHERE dedup_key IS NOT NULL DO NOTHING;
        RETURN NEW;
    END IF;

    SELECT COUNT(*)
    INTO private_group_count
    FROM public.groups private_group
    WHERE private_group.owner_user_id = current_account.owner_user_id
      AND private_group.platform = current_account.platform
      AND private_group.scope = 'user_private'
      AND private_group.status = 'active'
      AND private_group.deleted_at IS NULL
      AND COALESCE(private_group.subscription_type, '') <> 'none';

    SELECT COUNT(*)
    INTO private_binding_count
    FROM public.account_groups account_group
    JOIN public.groups private_group ON private_group.id = account_group.group_id
    WHERE account_group.account_id = current_account.id
      AND private_group.owner_user_id = current_account.owner_user_id
      AND private_group.platform = current_account.platform
      AND private_group.scope = 'user_private'
      AND private_group.status = 'active'
      AND private_group.deleted_at IS NULL
      AND COALESCE(private_group.subscription_type, '') <> 'none';

    SELECT COUNT(*)
    INTO non_private_binding_count
    FROM public.account_groups account_group
    LEFT JOIN public.groups private_group
      ON private_group.id = account_group.group_id
     AND private_group.owner_user_id = current_account.owner_user_id
     AND private_group.platform = current_account.platform
     AND private_group.scope = 'user_private'
     AND private_group.status = 'active'
     AND private_group.deleted_at IS NULL
     AND COALESCE(private_group.subscription_type, '') <> 'none'
    WHERE account_group.account_id = current_account.id
      AND private_group.id IS NULL;

    IF private_group_count <> 1
       OR private_binding_count <> 1
       OR non_private_binding_count <> 0
       OR EXISTS (
           SELECT 1
           FROM public.account_share_listings listing
           WHERE listing.account_id = current_account.id
             AND listing.deleted_at IS NULL
       )
       OR EXISTS (
           SELECT 1
           FROM public.account_share_memberships membership
           WHERE membership.account_id = current_account.id
             AND membership.deleted_at IS NULL
             AND membership.status IN ('active', 'queued')
       )
       OR EXISTS (
           SELECT 1
           FROM public.account_external_placements placement
           WHERE placement.account_id = current_account.id
       ) THEN
        RAISE EXCEPTION
            'approved public account % has no resolvable public placement and cannot be safely converted',
            current_account.id;
    END IF;

    UPDATE public.accounts
    SET share_mode = 'private',
        share_status = 'approved',
        updated_at = NOW()
    WHERE id = current_account.id
      AND deleted_at IS NULL
      AND owner_user_id IS NOT NULL
      AND share_mode = 'public'
      AND share_status = 'approved';

    INSERT INTO public.scheduler_outbox (
        event_type, account_id, group_id, payload, dedup_key
    )
    VALUES (
        'account_changed',
        current_account.id,
        NULL,
        NULL,
        'scheduler_outbox:approved-public-orphan-guard:' || current_account.id::TEXT
    )
    ON CONFLICT (dedup_key) WHERE dedup_key IS NOT NULL DO NOTHING;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_account_share_online_guard_orphan_approved_public
    ON accounts;
CREATE CONSTRAINT TRIGGER trg_account_share_online_guard_orphan_approved_public
AFTER INSERT OR UPDATE OF owner_user_id, platform, account_level, share_mode, share_status, priority, deleted_at
ON accounts
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION account_share_online_guard_orphan_approved_public();

CREATE TEMP TABLE account_share_approved_public_candidates
ON COMMIT DROP
AS
SELECT account.id AS account_id
FROM accounts account
WHERE account.deleted_at IS NULL
  AND account.owner_user_id IS NOT NULL
  AND account.share_mode = 'public'
  AND account.share_status = 'approved'
  AND NOT EXISTS (
      SELECT 1
      FROM account_external_placements placement
      WHERE placement.account_id = account.id
        AND placement.placement_type = 'public_pool'
  );

DO $$
DECLARE
    target_count BIGINT;
    updated_count BIGINT;
BEGIN
    PERFORM account_share_online_compat_public_placement(candidate.account_id)
    FROM account_share_approved_public_candidates candidate;

    CREATE TEMP TABLE account_share_approved_public_orphan_targets
    ON COMMIT DROP
    AS
    SELECT
        account.id AS account_id,
        account.owner_user_id,
        account.platform
    FROM account_share_approved_public_candidates candidate
    JOIN accounts account ON account.id = candidate.account_id
    WHERE account.deleted_at IS NULL
      AND account.owner_user_id IS NOT NULL
      AND account.share_mode = 'public'
      AND account.share_status = 'approved'
      AND NOT EXISTS (
          SELECT 1
          FROM account_external_placements placement
          WHERE placement.account_id = account.id
      );

    SELECT COUNT(*)
    INTO target_count
    FROM account_share_approved_public_orphan_targets;

    IF EXISTS (
        SELECT 1
        FROM account_share_approved_public_orphan_targets target
        WHERE (
            SELECT COUNT(*)
            FROM groups public_group
            JOIN account_groups account_group
              ON account_group.group_id = public_group.id
             AND account_group.account_id = target.account_id
            WHERE public_group.deleted_at IS NULL
              AND public_group.status = 'active'
              AND public_group.scope = 'public'
              AND public_group.owner_user_id IS NULL
              AND public_group.platform = target.platform
              AND NOT EXISTS (
                  SELECT 1
                  FROM account_share_mode_groups mode_group
                  WHERE mode_group.group_id = public_group.id
              )
        ) <> 0
    ) THEN
        RAISE EXCEPTION
            'approved public orphan conversion found an unresolved public-group binding';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM account_share_approved_public_orphan_targets target
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
           OR (
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
            'approved public orphan conversion requires one bound active private group';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM account_share_approved_public_orphan_targets target
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
            'approved public orphan conversion refuses non-private group bindings';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM account_share_approved_public_orphan_targets target
        JOIN account_share_listings listing
          ON listing.account_id = target.account_id
         AND listing.deleted_at IS NULL
    ) OR EXISTS (
        SELECT 1
        FROM account_share_approved_public_orphan_targets target
        JOIN account_share_memberships membership
          ON membership.account_id = target.account_id
         AND membership.deleted_at IS NULL
         AND membership.status IN ('active', 'queued')
    ) THEN
        RAISE EXCEPTION
            'approved public orphan conversion refuses room-linked accounts';
    END IF;

    UPDATE accounts account
    SET share_mode = 'private',
        share_status = 'approved',
        updated_at = NOW()
    FROM account_share_approved_public_orphan_targets target
    WHERE account.id = target.account_id;

    GET DIAGNOSTICS updated_count = ROW_COUNT;
    IF updated_count <> target_count THEN
        RAISE EXCEPTION
            'approved public orphan conversion updated % of % audited accounts',
            updated_count,
            target_count;
    END IF;

    INSERT INTO scheduler_outbox (
        event_type, account_id, group_id, payload, dedup_key
    )
    SELECT
        'account_changed',
        target.account_id,
        NULL,
        NULL,
        'scheduler_outbox:approved-public-orphan-guard:' || target.account_id::TEXT
    FROM account_share_approved_public_orphan_targets target
    ON CONFLICT (dedup_key) WHERE dedup_key IS NOT NULL DO NOTHING;

    IF EXISTS (
        SELECT 1
        FROM accounts account
        WHERE account.deleted_at IS NULL
          AND account.owner_user_id IS NOT NULL
          AND account.share_mode = 'public'
          AND account.share_status = 'approved'
          AND NOT EXISTS (
              SELECT 1
              FROM account_external_placements placement
              WHERE placement.account_id = account.id
                AND placement.placement_type = 'public_pool'
          )
    ) THEN
        RAISE EXCEPTION
            'approved public accounts without placements remain after installing the orphan guard';
    END IF;
END
$$;
