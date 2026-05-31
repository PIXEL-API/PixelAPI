-- Add read-only system notices and allow support tickets to reference them.
ALTER TABLE conversations
    ADD COLUMN IF NOT EXISTS kind VARCHAR(30) NOT NULL DEFAULT 'ticket',
    ADD COLUMN IF NOT EXISTS referenced_notice_id BIGINT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'chk_conversations_kind'
    ) THEN
        ALTER TABLE conversations
            ADD CONSTRAINT chk_conversations_kind
            CHECK (kind IN ('ticket', 'system_notice'));
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'chk_conversations_notice_readonly'
    ) THEN
        ALTER TABLE conversations
            ADD CONSTRAINT chk_conversations_notice_readonly
            CHECK (kind <> 'system_notice' OR (status = 'closed' AND referenced_notice_id IS NULL));
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_conversations_referenced_notice'
    ) THEN
        ALTER TABLE conversations
            ADD CONSTRAINT fk_conversations_referenced_notice
            FOREIGN KEY (referenced_notice_id) REFERENCES conversations(id) ON DELETE SET NULL;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_conversations_kind_last_message
    ON conversations(kind, last_message_at DESC);

CREATE INDEX IF NOT EXISTS idx_conversations_referenced_notice
    ON conversations(referenced_notice_id);
