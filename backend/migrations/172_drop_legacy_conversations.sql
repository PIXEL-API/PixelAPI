-- Drop legacy conversation tables after support data was migrated to support_threads/support_messages.
-- This migration intentionally removes old conversation data; current support service uses the new tables only.
DROP TABLE IF EXISTS conversation_messages, conversations;
