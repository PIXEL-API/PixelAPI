-- 279_ideas_plaza.sql
-- 「有个想法」用户经验分享板块：文章、版本、标签、互动、举报、审核与打赏业务表。
-- 说明：
--   * 全部使用幂等 DDL（IF NOT EXISTS / IF EXISTS），可安全重复执行。
--   * 余额打赏复用 users.balance + user_balance_ledger；积分打赏复用 points_ledger。
--     本迁移只建业务记录表，不改动任何既有余额/账本表结构。
--   * 不建硬外键（与仓库 soft-delete 实体约定一致），用索引保证查询与唯一约束。

-- 文章主表
CREATE TABLE IF NOT EXISTS idea_posts (
    id                  BIGSERIAL PRIMARY KEY,
    author_user_id      BIGINT      NOT NULL,
    current_revision_id BIGINT,
    status              TEXT        NOT NULL DEFAULT 'draft',
    published_at        TIMESTAMPTZ,
    like_count          INTEGER     NOT NULL DEFAULT 0,
    favorite_count      INTEGER     NOT NULL DEFAULT 0,
    view_count          INTEGER     NOT NULL DEFAULT 0,
    deleted_at          TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT idea_posts_status_check CHECK (status IN (
        'draft', 'pending_review', 'manual_review', 'published',
        'pending_revision', 'rejected', 'hidden', 'deleted', 'moderation_failed'
    ))
);
CREATE INDEX IF NOT EXISTS idx_idea_posts_author         ON idea_posts (author_user_id);
CREATE INDEX IF NOT EXISTS idx_idea_posts_published      ON idea_posts (status, published_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_idea_posts_author_status  ON idea_posts (author_user_id, status) WHERE deleted_at IS NULL;

-- 文章版本（每次发布/编辑都新增一条 revision，不可变）
CREATE TABLE IF NOT EXISTS idea_post_revisions (
    id                BIGSERIAL PRIMARY KEY,
    post_id           BIGINT      NOT NULL,
    revision_no       INTEGER     NOT NULL,
    title             TEXT        NOT NULL,
    summary           TEXT        NOT NULL DEFAULT '',
    body              TEXT        NOT NULL,
    body_hash         TEXT        NOT NULL,
    moderation_status TEXT        NOT NULL DEFAULT 'pending_review',
    moderation_reason TEXT,
    moderation_attempts      INTEGER     NOT NULL DEFAULT 0,
    moderation_next_retry_at TIMESTAMPTZ,
    published_at      TIMESTAMPTZ,
    created_by        BIGINT      NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT idea_post_revisions_moderation_check CHECK (moderation_status IN (
        'draft', 'pending_review', 'manual_review', 'approved',
        'pending_revision', 'rejected', 'moderation_failed'
    )),
    CONSTRAINT idea_post_revisions_post_revision_unique UNIQUE (post_id, revision_no)
);
CREATE INDEX IF NOT EXISTS idx_idea_post_revisions_post ON idea_post_revisions (post_id);

-- 标签（治理：slug 唯一，禁止硬删有使用记录的标签，用 status 停用 + redirect）
CREATE TABLE IF NOT EXISTS idea_tags (
    id               BIGSERIAL PRIMARY KEY,
    name             TEXT        NOT NULL,
    slug             TEXT        NOT NULL,
    status           TEXT        NOT NULL DEFAULT 'active',
    sort_order       INTEGER     NOT NULL DEFAULT 0,
    usage_count      INTEGER     NOT NULL DEFAULT 0,
    redirect_to_slug TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT idea_tags_slug_unique UNIQUE (slug),
    CONSTRAINT idea_tags_status_check CHECK (status IN ('active', 'disabled'))
);

-- 文章-标签关联
CREATE TABLE IF NOT EXISTS idea_post_tags (
    post_id    BIGINT      NOT NULL,
    tag_id     BIGINT      NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT idea_post_tags_unique UNIQUE (post_id, tag_id)
);
CREATE INDEX IF NOT EXISTS idx_idea_post_tags_tag ON idea_post_tags (tag_id);

-- 点赞
CREATE TABLE IF NOT EXISTS idea_post_likes (
    post_id    BIGINT      NOT NULL,
    user_id    BIGINT      NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT idea_post_likes_unique UNIQUE (post_id, user_id)
);

-- 收藏
CREATE TABLE IF NOT EXISTS idea_post_favorites (
    post_id    BIGINT      NOT NULL,
    user_id    BIGINT      NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT idea_post_favorites_unique UNIQUE (post_id, user_id)
);

-- 举报（同一用户对同一文章的未处理举报去重）
CREATE TABLE IF NOT EXISTS idea_post_reports (
    id                 BIGSERIAL PRIMARY KEY,
    post_id            BIGINT      NOT NULL,
    reporter_user_id   BIGINT      NOT NULL,
    reason             TEXT        NOT NULL,
    detail             TEXT        NOT NULL DEFAULT '',
    status             TEXT        NOT NULL DEFAULT 'pending',
    handled_by_user_id BIGINT,
    resolution         TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT idea_post_reports_status_check CHECK (status IN ('pending', 'resolved', 'dismissed'))
);
CREATE INDEX IF NOT EXISTS idx_idea_post_reports_post   ON idea_post_reports (post_id);
CREATE INDEX IF NOT EXISTS idx_idea_post_reports_status ON idea_post_reports (status, created_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_idea_post_reports_pending_unique
    ON idea_post_reports (post_id, reporter_user_id)
    WHERE status = 'pending';

-- 浏览（去重键按 user+post+时间桶，DB 层唯一约束兜底防刷量）
CREATE TABLE IF NOT EXISTS idea_post_views (
    id          BIGSERIAL PRIMARY KEY,
    post_id     BIGINT      NOT NULL,
    user_id     BIGINT      NOT NULL,
    revision_id BIGINT,
    dedup_key   TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT idea_post_views_dedup_unique UNIQUE (dedup_key)
);
CREATE INDEX IF NOT EXISTS idx_idea_post_views_post ON idea_post_views (post_id, created_at);

-- 审核事件（不可变：决策、风险等级、模型/url/prompt 快照、尝试次数、错误）
CREATE TABLE IF NOT EXISTS idea_moderation_events (
    id               BIGSERIAL PRIMARY KEY,
    post_id          BIGINT      NOT NULL,
    revision_id      BIGINT      NOT NULL,
    stage            TEXT        NOT NULL,
    decision         TEXT        NOT NULL,
    risk_level       TEXT,
    reason           TEXT,
    model_snapshot   TEXT,
    url_snapshot     TEXT,
    prompt_version   TEXT,
    attempt_count    INTEGER     NOT NULL DEFAULT 1,
    next_retry_at    TIMESTAMPTZ,
    last_error       TEXT,
    operator_user_id BIGINT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT idea_moderation_events_decision_check CHECK (decision IN ('pass', 'review', 'reject', 'failed'))
);
CREATE INDEX IF NOT EXISTS idx_idea_moderation_events_post ON idea_moderation_events (post_id, revision_id, created_at);

-- 审计事件（不可变：状态/标签/附件/管理员操作）
CREATE TABLE IF NOT EXISTS idea_post_audit_events (
    id            BIGSERIAL PRIMARY KEY,
    post_id       BIGINT      NOT NULL,
    actor_user_id BIGINT,
    action        TEXT        NOT NULL,
    before        JSONB,
    after         JSONB,
    reason        TEXT,
    request_id    TEXT,
    ip            TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_idea_post_audit_events_post ON idea_post_audit_events (post_id, created_at);

-- 打赏业务记录（幂等键唯一；余额/积分流水各自写入 user_balance_ledger / points_ledger）
CREATE TABLE IF NOT EXISTS idea_post_rewards (
    id                BIGSERIAL PRIMARY KEY,
    payer_user_id     BIGINT           NOT NULL,
    recipient_user_id BIGINT           NOT NULL,
    post_id           BIGINT           NOT NULL,
    revision_id       BIGINT           NOT NULL,
    asset_type        TEXT             NOT NULL,
    amount            NUMERIC(20, 8)   NOT NULL,
    idempotency_key TEXT        NOT NULL,
    status          TEXT        NOT NULL DEFAULT 'completed',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT idea_post_rewards_asset_check CHECK (asset_type IN ('balance', 'points')),
    CONSTRAINT idea_post_rewards_status_check CHECK (status IN ('completed', 'reversed')),
    CONSTRAINT idea_post_rewards_idempotency_unique UNIQUE (idempotency_key)
);
CREATE INDEX IF NOT EXISTS idx_idea_post_rewards_post ON idea_post_rewards (post_id);
CREATE INDEX IF NOT EXISTS idx_idea_post_rewards_payer ON idea_post_rewards (payer_user_id);
CREATE INDEX IF NOT EXISTS idx_idea_post_rewards_recipient ON idea_post_rewards (recipient_user_id);

-- 文章附件（图片/普通附件，私有 OSS，对象 key 唯一）
CREATE TABLE IF NOT EXISTS idea_post_assets (
    id               BIGSERIAL PRIMARY KEY,
    post_id          BIGINT      NOT NULL,
    revision_id      BIGINT      NOT NULL,
    object_key       TEXT        NOT NULL,
    file_name        TEXT        NOT NULL DEFAULT '',
    mime_type        TEXT        NOT NULL,
    size_bytes       BIGINT      NOT NULL DEFAULT 0,
    sha256           TEXT,
    width            INTEGER,
    height           INTEGER,
    status           TEXT        NOT NULL DEFAULT 'active',
    uploader_user_id BIGINT      NOT NULL,
    deleted_at       TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT idea_post_assets_object_key_unique UNIQUE (object_key)
);
CREATE INDEX IF NOT EXISTS idx_idea_post_assets_post ON idea_post_assets (post_id, revision_id);
