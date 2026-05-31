-- 工单服务新表：每个用户唯一线程，旧客服工单消息迁移到新表；系统通知不迁移。
CREATE TABLE IF NOT EXISTS support_threads (
    id                          BIGSERIAL PRIMARY KEY,
    user_id                     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    subject                     VARCHAR(200) NOT NULL DEFAULT '工单服务',
    status                      VARCHAR(30) NOT NULL DEFAULT 'open',
    priority                    VARCHAR(20) NOT NULL DEFAULT 'normal',
    type                        VARCHAR(40) NOT NULL DEFAULT 'support',
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
    CONSTRAINT support_threads_user_id_key UNIQUE (user_id),
    CONSTRAINT chk_support_threads_status
        CHECK (status IN ('open', 'pending_user', 'pending_admin', 'resolved', 'closed')),
    CONSTRAINT chk_support_threads_priority
        CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    CONSTRAINT chk_support_threads_type
        CHECK (type IN ('support', 'notice', 'billing', 'subscription', 'account', 'security'))
);

CREATE TABLE IF NOT EXISTS support_messages (
    id              BIGSERIAL PRIMARY KEY,
    thread_id       BIGINT NOT NULL REFERENCES support_threads(id) ON DELETE CASCADE,
    sender_type     VARCHAR(20) NOT NULL,
    sender_id       BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    message_type    VARCHAR(30) NOT NULL DEFAULT 'text',
    content_format  VARCHAR(20) NOT NULL DEFAULT 'plain',
    title           VARCHAR(200) NOT NULL DEFAULT '',
    content         TEXT NOT NULL,
    source          VARCHAR(80) NOT NULL DEFAULT '',
    source_id       VARCHAR(120) NOT NULL DEFAULT '',
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_support_messages_sender_type
        CHECK (sender_type IN ('user', 'admin', 'system')),
    CONSTRAINT chk_support_messages_sender_required
        CHECK (
            (sender_type = 'system' AND sender_id IS NULL)
            OR (sender_type IN ('user', 'admin') AND sender_id IS NOT NULL)
        ),
    CONSTRAINT chk_support_messages_message_type
        CHECK (message_type IN ('text', 'notice', 'operation_log', 'system_event')),
    CONSTRAINT chk_support_messages_content_format
        CHECK (content_format IN ('plain', 'markdown'))
);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_support_threads_last_message'
    ) THEN
        ALTER TABLE support_threads
            ADD CONSTRAINT fk_support_threads_last_message
            FOREIGN KEY (last_message_id) REFERENCES support_messages(id) ON DELETE SET NULL;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_support_threads_user_last_read_message'
    ) THEN
        ALTER TABLE support_threads
            ADD CONSTRAINT fk_support_threads_user_last_read_message
            FOREIGN KEY (user_last_read_message_id) REFERENCES support_messages(id) ON DELETE SET NULL;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'fk_support_threads_admin_last_read_message'
    ) THEN
        ALTER TABLE support_threads
            ADD CONSTRAINT fk_support_threads_admin_last_read_message
            FOREIGN KEY (admin_last_read_message_id) REFERENCES support_messages(id) ON DELETE SET NULL;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_support_threads_status_last_message
    ON support_threads(status, last_message_at DESC);

CREATE INDEX IF NOT EXISTS idx_support_threads_assignee_status
    ON support_threads(assigned_admin_id, status);

CREATE INDEX IF NOT EXISTS idx_support_threads_last_message
    ON support_threads(last_message_at DESC);

CREATE INDEX IF NOT EXISTS idx_support_messages_thread_id
    ON support_messages(thread_id, id);

CREATE INDEX IF NOT EXISTS idx_support_messages_thread_created
    ON support_messages(thread_id, created_at);

CREATE INDEX IF NOT EXISTS idx_support_messages_sender
    ON support_messages(sender_type, sender_id);

CREATE INDEX IF NOT EXISTS idx_support_messages_source
    ON support_messages(source, source_id);

CREATE UNIQUE INDEX IF NOT EXISTS idx_support_messages_thread_source_unique
    ON support_messages(thread_id, source, source_id)
    WHERE source <> '' AND source_id <> '';

COMMENT ON TABLE support_threads IS '工单服务用户唯一线程';
COMMENT ON TABLE support_messages IS '工单服务消息';

WITH ticket_bounds AS (
    SELECT
        c.user_id,
        MIN(c.created_at) AS created_at,
        MAX(c.updated_at) AS updated_at,
        MAX(c.last_message_at) AS last_message_at
    FROM conversations c
    WHERE c.kind = 'ticket'
    GROUP BY c.user_id
),
latest_ticket AS (
    SELECT DISTINCT ON (c.user_id)
        c.user_id,
        c.status,
        c.priority,
        c.type,
        c.assigned_admin_id,
        c.subject
    FROM conversations c
    WHERE c.kind = 'ticket'
    ORDER BY c.user_id, c.last_message_at DESC, c.id DESC
)
INSERT INTO support_threads (
    user_id,
    subject,
    status,
    priority,
    type,
    assigned_admin_id,
    last_message_at,
    created_at,
    updated_at
)
SELECT
    b.user_id,
    COALESCE(NULLIF(l.subject, ''), '工单服务'),
    COALESCE(NULLIF(l.status, ''), 'open'),
    COALESCE(NULLIF(l.priority, ''), 'normal'),
    COALESCE(NULLIF(l.type, ''), 'support'),
    l.assigned_admin_id,
    b.last_message_at,
    b.created_at,
    b.updated_at
FROM ticket_bounds b
JOIN latest_ticket l ON l.user_id = b.user_id
ON CONFLICT (user_id) DO NOTHING;

INSERT INTO support_messages (
    thread_id,
    sender_type,
    sender_id,
    message_type,
    content_format,
    title,
    content,
    source,
    source_id,
    metadata,
    created_at
)
SELECT
    st.id,
    cm.sender_type,
    cm.sender_id,
    cm.message_type,
    cm.content_format,
    c.subject,
    cm.content,
    'legacy_conversation',
    cm.id::text,
    jsonb_build_object(
        'legacy_conversation_id', c.id,
        'legacy_message_id', cm.id,
        'legacy_subject', c.subject,
        'legacy_type', c.type,
        'legacy_priority', c.priority
    ),
    cm.created_at
FROM conversation_messages cm
JOIN conversations c ON c.id = cm.conversation_id
JOIN support_threads st ON st.user_id = c.user_id
WHERE c.kind = 'ticket'
ON CONFLICT (thread_id, source, source_id) WHERE source <> '' AND source_id <> '' DO NOTHING;

WITH last_messages AS (
    SELECT DISTINCT ON (sm.thread_id)
        sm.thread_id,
        sm.id,
        sm.sender_type,
        left(regexp_replace(trim(sm.content), '\s+', ' ', 'g'), 240) AS excerpt,
        sm.created_at
    FROM support_messages sm
    ORDER BY sm.thread_id, sm.created_at DESC, sm.id DESC
),
read_messages AS (
    SELECT
        st.id AS thread_id,
        MAX(sm.id) FILTER (WHERE sm.sender_type = 'user') AS user_last_read_message_id,
        MAX(sm.created_at) FILTER (WHERE sm.sender_type = 'user') AS user_last_read_at,
        MAX(sm.id) FILTER (WHERE sm.sender_type <> 'user') AS admin_last_read_message_id,
        MAX(sm.created_at) FILTER (WHERE sm.sender_type <> 'user') AS admin_last_read_at
    FROM support_threads st
    LEFT JOIN support_messages sm ON sm.thread_id = st.id
    GROUP BY st.id
)
UPDATE support_threads st
SET
    last_message_id = lm.id,
    last_message_sender_type = COALESCE(lm.sender_type, ''),
    last_message_excerpt = COALESCE(lm.excerpt, ''),
    last_message_at = COALESCE(lm.created_at, st.last_message_at),
    user_last_read_message_id = rm.user_last_read_message_id,
    user_last_read_at = rm.user_last_read_at,
    admin_last_read_message_id = rm.admin_last_read_message_id,
    admin_last_read_at = rm.admin_last_read_at,
    updated_at = NOW()
FROM last_messages lm
JOIN read_messages rm ON rm.thread_id = lm.thread_id
WHERE st.id = lm.thread_id;
