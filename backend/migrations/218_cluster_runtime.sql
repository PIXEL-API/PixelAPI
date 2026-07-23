-- 218_cluster_runtime.sql
-- Multi-instance runtime coordination. This migration is schema-only and
-- intentionally does not rewrite or backfill any existing business table.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS cluster_instances (
    deployment_id VARCHAR(128) NOT NULL,
    node_id VARCHAR(128) NOT NULL,
    boot_id UUID NOT NULL,
    desired_state VARCHAR(16) NOT NULL DEFAULT 'active',
    observed_state VARCHAR(16) NOT NULL DEFAULT 'starting',
    hostname VARCHAR(255) NOT NULL DEFAULT '',
    version VARCHAR(128) NOT NULL DEFAULT '',
    commit_sha VARCHAR(128) NOT NULL DEFAULT '',
    build_date VARCHAR(128) NOT NULL DEFAULT '',
    config_fingerprint VARCHAR(128) NOT NULL DEFAULT '',
    secret_fingerprint VARCHAR(128) NOT NULL DEFAULT '',
    cache_versions JSONB NOT NULL DEFAULT '{}'::jsonb,
    started_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    cpu_percent DOUBLE PRECISION NOT NULL DEFAULT 0,
    rss_bytes BIGINT NOT NULL DEFAULT 0,
    memory_limit_bytes BIGINT NOT NULL DEFAULT 0,
    goroutine_count BIGINT NOT NULL DEFAULT 0,
    fd_open BIGINT NOT NULL DEFAULT 0,
    fd_limit BIGINT NOT NULL DEFAULT 0,
    active_http BIGINT NOT NULL DEFAULT 0,
    active_sse BIGINT NOT NULL DEFAULT 0,
    active_websocket BIGINT NOT NULL DEFAULT 0,
    db_open_connections INTEGER NOT NULL DEFAULT 0,
    db_in_use_connections INTEGER NOT NULL DEFAULT 0,
    db_idle_connections INTEGER NOT NULL DEFAULT 0,
    db_wait_count BIGINT NOT NULL DEFAULT 0,
    db_max_open_connections INTEGER NOT NULL DEFAULT 0,
    redis_pool_connections INTEGER NOT NULL DEFAULT 0,
    redis_idle_connections INTEGER NOT NULL DEFAULT 0,
    redis_pool_size INTEGER NOT NULL DEFAULT 0,
    database_healthy BOOLEAN NOT NULL DEFAULT FALSE,
    redis_healthy BOOLEAN NOT NULL DEFAULT FALSE,
    cache_healthy BOOLEAN NOT NULL DEFAULT FALSE,
    migration_healthy BOOLEAN NOT NULL DEFAULT FALSE,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (deployment_id, node_id),
    CONSTRAINT cluster_instances_desired_state_check
        CHECK (desired_state IN ('active', 'draining')),
    CONSTRAINT cluster_instances_observed_state_check
        CHECK (observed_state IN ('starting', 'ready', 'draining', 'unhealthy')),
    CONSTRAINT cluster_instances_cache_versions_check
        CHECK (
            jsonb_typeof(cache_versions) = 'object'
            AND NOT jsonb_path_exists(
                cache_versions,
                '$.* ? (@.type() != "number" || @ < 0)'
            )
        ),
    CONSTRAINT cluster_instances_metrics_check
        CHECK (
            cpu_percent >= 0
            AND rss_bytes >= 0
            AND memory_limit_bytes >= 0
            AND goroutine_count >= 0
            AND fd_open >= 0
            AND fd_limit >= 0
            AND active_http >= 0
            AND active_sse >= 0
            AND active_websocket >= 0
            AND db_open_connections >= 0
            AND db_in_use_connections >= 0
            AND db_idle_connections >= 0
            AND db_wait_count >= 0
            AND db_max_open_connections >= 0
            AND redis_pool_connections >= 0
            AND redis_idle_connections >= 0
            AND redis_pool_size >= 0
        )
);

CREATE INDEX IF NOT EXISTS idx_cluster_instances_deployment_heartbeat
    ON cluster_instances (deployment_id, heartbeat_at DESC);

CREATE INDEX IF NOT EXISTS idx_cluster_instances_deployment_states
    ON cluster_instances (deployment_id, desired_state, observed_state);

CREATE TABLE IF NOT EXISTS cluster_task_leases (
    deployment_id VARCHAR(128) NOT NULL,
    task_name VARCHAR(128) NOT NULL,
    owner_node_id VARCHAR(128),
    owner_boot_id UUID,
    fencing_token BIGINT NOT NULL DEFAULT 0,
    lease_expires_at TIMESTAMPTZ,
    last_acquired_at TIMESTAMPTZ,
    last_renewed_at TIMESTAMPTZ,
    last_released_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    last_duration_ms BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (deployment_id, task_name),
    CONSTRAINT cluster_task_leases_owner_pair_check
        CHECK (
            (owner_node_id IS NULL AND owner_boot_id IS NULL)
            OR (owner_node_id IS NOT NULL AND owner_boot_id IS NOT NULL)
        ),
    CONSTRAINT cluster_task_leases_fencing_token_check CHECK (fencing_token >= 0),
    CONSTRAINT cluster_task_leases_duration_check
        CHECK (last_duration_ms IS NULL OR last_duration_ms >= 0)
);

CREATE INDEX IF NOT EXISTS idx_cluster_task_leases_deployment_expiry
    ON cluster_task_leases (deployment_id, lease_expires_at);

CREATE INDEX IF NOT EXISTS idx_cluster_task_leases_owner
    ON cluster_task_leases (deployment_id, owner_node_id, owner_boot_id)
    WHERE owner_node_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS cluster_operations (
    id UUID PRIMARY KEY,
    deployment_id VARCHAR(128) NOT NULL,
    idempotency_key UUID NOT NULL,
    request_fingerprint VARCHAR(64) NOT NULL,
    operation_type VARCHAR(32) NOT NULL,
    target_node_id VARCHAR(128),
    cache_scope VARCHAR(32),
    reason VARCHAR(500) NOT NULL,
    actor_user_id BIGINT NOT NULL,
    actor_name VARCHAR(255) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    attempt_token BIGINT NOT NULL DEFAULT 0,
    claimed_by_node_id VARCHAR(128),
    claimed_by_boot_id UUID,
    claim_expires_at TIMESTAMPTZ,
    claimed_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    result TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT cluster_operations_idempotency_unique
        UNIQUE (deployment_id, idempotency_key),
    CONSTRAINT cluster_operations_type_check
        CHECK (operation_type IN ('drain', 'resume', 'cache_refresh')),
    CONSTRAINT cluster_operations_status_check
        CHECK (status IN ('pending', 'running', 'succeeded', 'failed')),
    CONSTRAINT cluster_operations_cache_scope_check
        CHECK (
            (operation_type = 'cache_refresh'
                AND cache_scope IN (
                    'channel_routing',
                    'runtime_settings',
                    'policy_metadata',
                    'all_safe'
                ))
            OR (operation_type <> 'cache_refresh' AND cache_scope IS NULL)
        ),
    CONSTRAINT cluster_operations_target_check
        CHECK (
            (operation_type IN ('drain', 'resume') AND target_node_id IS NOT NULL)
            OR operation_type = 'cache_refresh'
        ),
    CONSTRAINT cluster_operations_reason_length_check
        CHECK (char_length(reason) BETWEEN 8 AND 500),
    CONSTRAINT cluster_operations_claim_owner_pair_check
        CHECK (
            (claimed_by_node_id IS NULL AND claimed_by_boot_id IS NULL)
            OR (claimed_by_node_id IS NOT NULL AND claimed_by_boot_id IS NOT NULL)
        ),
    CONSTRAINT cluster_operations_attempt_token_check CHECK (attempt_token >= 0)
);

CREATE INDEX IF NOT EXISTS idx_cluster_operations_deployment_status_created
    ON cluster_operations (deployment_id, status, created_at, id);

CREATE INDEX IF NOT EXISTS idx_cluster_operations_target_pending
    ON cluster_operations (deployment_id, target_node_id, created_at, id)
    WHERE status IN ('pending', 'running');

CREATE INDEX IF NOT EXISTS idx_cluster_operations_deployment_created
    ON cluster_operations (deployment_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS cluster_cache_versions (
    deployment_id VARCHAR(128) NOT NULL,
    cache_key VARCHAR(32) NOT NULL,
    version BIGINT NOT NULL DEFAULT 0,
    updated_by_node_id VARCHAR(128),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (deployment_id, cache_key),
    CONSTRAINT cluster_cache_versions_key_check
        CHECK (
            cache_key IN (
                'channel_routing',
                'runtime_settings',
                'policy_metadata'
            )
        ),
    CONSTRAINT cluster_cache_versions_version_check CHECK (version >= 0)
);

CREATE INDEX IF NOT EXISTS idx_cluster_cache_versions_deployment_updated
    ON cluster_cache_versions (deployment_id, updated_at DESC);
