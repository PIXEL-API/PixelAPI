-- Add the room-scoped uniqueness guard while retaining the legacy broad guard.
-- The broad index is removed only in the later contract release.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_account_share_room_operations_open_room_listing
    ON account_share_room_operations(listing_id)
    WHERE action <> 'end_membership'
      AND status IN ('pending', 'running', 'needs_attention');
