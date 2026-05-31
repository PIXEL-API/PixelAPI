-- 站内双向通信：用户与管理员之间的会话线程。
CREATE TABLE IF NOT EXISTS conversations (
    id                          BIGSERIAL PRIMARY KEY,
    user_id                     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    subject                     VARCHAR(200) NOT NULL,
    status                      VARCHAR(30) NOT NULL DEFAULT 'pending_admin',
    priority                    VARCHAR(20) NOT NULL DEFAULT 'normal',
    type                        VARCHAR(40) NOT NULL DEFAULT 'support',
    source                      VARCHAR(80) NOT NULL DEFAULT '',
    source_id                   VARCHAR(120) NOT NULL DEFAULT '',
    assigned_admin_id           BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    last_message_id             BIGINT NULL,
    last_message_sender_type    VARCHAR(20) NOT NULL DEFAULT '',
    last_message_excerpt        VARCHAR(240) NOT NULL DEFAULT '',
    last_message_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    user_last_read_message_id   BIGINT NULL,
    user_last_read_at           TIMESTAMPTZ NULL,
    admin_last_read_message_id  BIGINT NULL,
    admin_last_read_at          TIMESTAMPTZ NULL,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_conversations_status
        CHECK (status IN ('open', 'pending_user', 'pending_admin', 'resolved', 'closed')),
    CONSTRAINT chk_conversations_priority
        CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    CONSTRAINT chk_conversations_type
        CHECK (type IN ('support', 'notice', 'billing', 'subscription', 'account', 'security'))
);

CREATE TABLE IF NOT EXISTS conversation_messages (
    id              BIGSERIAL PRIMARY KEY,
    conversation_id BIGINT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    sender_type     VARCHAR(20) NOT NULL,
    sender_id       BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    message_type    VARCHAR(30) NOT NULL DEFAULT 'text',
    content_format  VARCHAR(20) NOT NULL DEFAULT 'plain',
    content         TEXT NOT NULL,
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_conversation_messages_sender_type
        CHECK (sender_type IN ('user', 'admin', 'system')),
    CONSTRAINT chk_conversation_messages_sender_required
        CHECK (
            (sender_type = 'system' AND sender_id IS NULL)
            OR (sender_type IN ('user', 'admin') AND sender_id IS NOT NULL)
        ),
    CONSTRAINT chk_conversation_messages_message_type
        CHECK (message_type IN ('text', 'notice', 'operation_log', 'system_event')),
    CONSTRAINT chk_conversation_messages_content_format
        CHECK (content_format IN ('plain', 'markdown'))
);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_conversations_last_message'
    ) THEN
        ALTER TABLE conversations
            ADD CONSTRAINT fk_conversations_last_message
            FOREIGN KEY (last_message_id) REFERENCES conversation_messages(id) ON DELETE SET NULL;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_conversations_user_last_read_message'
    ) THEN
        ALTER TABLE conversations
            ADD CONSTRAINT fk_conversations_user_last_read_message
            FOREIGN KEY (user_last_read_message_id) REFERENCES conversation_messages(id) ON DELETE SET NULL;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_conversations_admin_last_read_message'
    ) THEN
        ALTER TABLE conversations
            ADD CONSTRAINT fk_conversations_admin_last_read_message
            FOREIGN KEY (admin_last_read_message_id) REFERENCES conversation_messages(id) ON DELETE SET NULL;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_conversations_user_last_message
    ON conversations(user_id, last_message_at DESC);

CREATE INDEX IF NOT EXISTS idx_conversations_status_last_message
    ON conversations(status, last_message_at DESC);

CREATE INDEX IF NOT EXISTS idx_conversations_assignee_status
    ON conversations(assigned_admin_id, status);

CREATE INDEX IF NOT EXISTS idx_conversations_source
    ON conversations(source, source_id);

CREATE INDEX IF NOT EXISTS idx_conversation_messages_conversation_id
    ON conversation_messages(conversation_id, id);

CREATE INDEX IF NOT EXISTS idx_conversation_messages_conversation_created
    ON conversation_messages(conversation_id, created_at);

CREATE INDEX IF NOT EXISTS idx_conversation_messages_sender
    ON conversation_messages(sender_type, sender_id);

COMMENT ON TABLE conversations IS '站内双向通信会话';
COMMENT ON TABLE conversation_messages IS '站内双向通信消息';
