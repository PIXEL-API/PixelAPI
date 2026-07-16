ALTER TABLE account_share_memberships
    ADD COLUMN IF NOT EXISTS waiver_window_started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS waiver_window_usage_amount NUMERIC(20,10) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS waiver_window_request_count BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS waiver_window_last_request_at TIMESTAMPTZ;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'account_share_memberships_waiver_window_usage_nonnegative_chk'
    ) THEN
        ALTER TABLE account_share_memberships
            ADD CONSTRAINT account_share_memberships_waiver_window_usage_nonnegative_chk CHECK (
                waiver_window_usage_amount >= 0
                AND waiver_window_request_count >= 0
            );
    END IF;
END $$;

WITH active_windows AS (
    SELECT
        m.id AS membership_id,
        COALESCE(m.billed_until, m.joined_at) AS window_start,
        LEAST(
            COALESCE(m.billed_until, m.joined_at) + INTERVAL '1 hour',
            NOW()
        ) AS window_end
    FROM account_share_memberships m
    WHERE m.status = 'active'
        AND m.deleted_at IS NULL
        AND m.hourly_rate_snapshot > 0
        AND m.hourly_fee_waiver_minimum_snapshot > 0
        AND COALESCE(m.billed_until, m.joined_at) IS NOT NULL
),
usage_rows AS (
    SELECT
        w.membership_id,
        w.window_start,
        e.total_charge,
        COALESCE(
            e.period_started_at,
            COALESCE(ul.created_at, e.created_at) - (GREATEST(e.duration_ms, 0) * INTERVAL '1 millisecond')
        ) AS request_started_at,
        COALESCE(e.period_ended_at, COALESCE(ul.created_at, e.created_at)) AS request_ended_at,
        COALESCE(ul.created_at, e.created_at) AS occurred_at,
        w.window_end
    FROM active_windows w
    JOIN account_share_mode_settlement_entries e ON e.membership_id = w.membership_id
        AND e.settlement_type = 'usage_request'
    LEFT JOIN usage_logs ul ON ul.id = e.usage_log_id
    WHERE w.window_end > w.window_start
        AND COALESCE(
            e.period_started_at,
            COALESCE(ul.created_at, e.created_at) - (GREATEST(e.duration_ms, 0) * INTERVAL '1 millisecond')
        ) < w.window_end
        AND COALESCE(e.period_ended_at, COALESCE(ul.created_at, e.created_at)) >= w.window_start
),
window_stats AS (
    SELECT
        membership_id,
        window_start,
        COALESCE(SUM(
            CASE
                WHEN request_ended_at > request_started_at
                    AND LEAST(request_ended_at, window_end) > GREATEST(request_started_at, window_start)
                THEN total_charge
                    * EXTRACT(EPOCH FROM (LEAST(request_ended_at, window_end) - GREATEST(request_started_at, window_start)))::numeric
                    / NULLIF(EXTRACT(EPOCH FROM (request_ended_at - request_started_at))::numeric, 0)
                WHEN request_ended_at = request_started_at
                    AND occurred_at >= window_start
                    AND occurred_at < window_end
                THEN total_charge
                ELSE 0
            END
        ), 0) AS usage_amount,
        COUNT(*) FILTER (
            WHERE (
                request_ended_at > request_started_at
                AND LEAST(request_ended_at, window_end) > GREATEST(request_started_at, window_start)
            ) OR (
                request_ended_at = request_started_at
                AND occurred_at >= window_start
                AND occurred_at < window_end
            )
        ) AS request_count,
        MAX(occurred_at) FILTER (
            WHERE (
                request_ended_at > request_started_at
                AND LEAST(request_ended_at, window_end) > GREATEST(request_started_at, window_start)
            ) OR (
                request_ended_at = request_started_at
                AND occurred_at >= window_start
                AND occurred_at < window_end
            )
        ) AS last_request_at
    FROM usage_rows
    GROUP BY membership_id, window_start
)
UPDATE account_share_memberships m
SET waiver_window_started_at = s.window_start,
    waiver_window_usage_amount = GREATEST(s.usage_amount, 0),
    waiver_window_request_count = GREATEST(s.request_count, 0),
    waiver_window_last_request_at = s.last_request_at,
    updated_at = NOW()
FROM window_stats s
WHERE m.id = s.membership_id
    AND (
        m.waiver_window_started_at IS DISTINCT FROM s.window_start
        OR m.waiver_window_usage_amount IS DISTINCT FROM GREATEST(s.usage_amount, 0)
        OR m.waiver_window_request_count IS DISTINCT FROM GREATEST(s.request_count, 0)
        OR m.waiver_window_last_request_at IS DISTINCT FROM s.last_request_at
    );

WITH active_windows AS (
    SELECT
        m.id AS membership_id,
        COALESCE(m.billed_until, m.joined_at) AS window_start
    FROM account_share_memberships m
    WHERE m.status = 'active'
        AND m.deleted_at IS NULL
        AND m.hourly_rate_snapshot > 0
        AND m.hourly_fee_waiver_minimum_snapshot > 0
        AND COALESCE(m.billed_until, m.joined_at) IS NOT NULL
)
UPDATE account_share_memberships m
SET waiver_window_started_at = w.window_start,
    waiver_window_usage_amount = 0,
    waiver_window_request_count = 0,
    waiver_window_last_request_at = NULL,
    updated_at = NOW()
FROM active_windows w
WHERE m.id = w.membership_id
    AND m.waiver_window_started_at IS DISTINCT FROM w.window_start;
