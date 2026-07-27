-- The three temporary guards are built before the runner repairs an invalid
-- live-membership target. They keep uniqueness continuously enforced while a
-- failed concurrent target is dropped and recreated. The runner verifies and
-- removes these reserved guards only after every target has been verified.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_as_memberships_live_consumer_rebuild_guard
    ON public.account_share_memberships(consumer_user_id)
    WHERE status IN ('active', 'ending') AND deleted_at IS NULL;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_as_memberships_live_api_key_rebuild_guard
    ON public.account_share_memberships(api_key_id)
    WHERE status IN ('active', 'ending') AND deleted_at IS NULL;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_as_memberships_live_listing_consumer_rebuild_guard
    ON public.account_share_memberships(listing_id, consumer_user_id)
    WHERE status IN ('active', 'queued', 'ending') AND deleted_at IS NULL;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_account_share_memberships_live_consumer
    ON public.account_share_memberships(consumer_user_id)
    WHERE status IN ('active', 'ending') AND deleted_at IS NULL;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_account_share_memberships_live_api_key
    ON public.account_share_memberships(api_key_id)
    WHERE status IN ('active', 'ending') AND deleted_at IS NULL;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_account_share_memberships_live_listing_consumer
    ON public.account_share_memberships(listing_id, consumer_user_id)
    WHERE status IN ('active', 'queued', 'ending') AND deleted_at IS NULL;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_account_share_room_operations_open_membership
    ON public.account_share_room_operations(membership_id)
    WHERE action = 'end_membership'
      AND membership_id IS NOT NULL
      AND status IN ('pending', 'running', 'needs_attention');

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_account_share_memberships_queue_expiry
    ON public.account_share_memberships(queue_expires_at, id)
    WHERE status = 'queued' AND deleted_at IS NULL;
