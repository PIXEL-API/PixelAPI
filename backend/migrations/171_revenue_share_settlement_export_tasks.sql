-- Revenue share settlement export task queue.
CREATE TABLE IF NOT EXISTS revenue_share_settlement_export_tasks (
    id BIGSERIAL PRIMARY KEY,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    filters JSONB NOT NULL DEFAULT '{}'::jsonb,
    total_rows BIGINT NOT NULL DEFAULT 0,
    exported_rows BIGINT NOT NULL DEFAULT 0,
    file_count INTEGER NOT NULL DEFAULT 0,
    file_path TEXT,
    file_name TEXT,
    file_size_bytes BIGINT NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    canceled_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT revenue_share_settlement_export_status_check
        CHECK (status IN ('pending', 'running', 'completed', 'failed', 'canceled')),
    CONSTRAINT revenue_share_settlement_export_rows_check
        CHECK (total_rows >= 0 AND exported_rows >= 0 AND exported_rows <= total_rows),
    CONSTRAINT revenue_share_settlement_export_file_count_check CHECK (file_count >= 0),
    CONSTRAINT revenue_share_settlement_export_file_size_check CHECK (file_size_bytes >= 0)
);

CREATE INDEX IF NOT EXISTS idx_revenue_share_settlement_exports_created_by_status
    ON revenue_share_settlement_export_tasks (created_by, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_revenue_share_settlement_exports_status_created
    ON revenue_share_settlement_export_tasks (status, created_at ASC);

CREATE INDEX IF NOT EXISTS idx_revenue_share_settlement_exports_expires_at
    ON revenue_share_settlement_export_tasks (expires_at)
    WHERE expires_at IS NOT NULL;
