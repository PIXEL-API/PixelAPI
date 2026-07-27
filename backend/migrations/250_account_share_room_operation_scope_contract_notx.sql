-- Remove the legacy broad room-operation guard after scoped lifecycle
-- operations have completed their observation window.
DROP INDEX CONCURRENTLY IF EXISTS uq_account_share_room_operations_open_listing;
