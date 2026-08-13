-- Seed the aggregate from the raw rows visible at cutover, matching the legacy
-- endpoint's current result without trusting historical daily snapshots whose
-- timezone-boundary overwrite semantics are not exact. Migration 271 captures
-- concurrent inserts; this migration excludes those rows from the MVCC
-- baseline, briefly drains writers, merges the catch-up set once, and atomically
-- switches the trigger to direct statement-level increments. After cutover the
-- aggregate remains stable when retained raw usage rows are deleted.
SET TRANSACTION ISOLATION LEVEL READ COMMITTED;
SET LOCAL lock_timeout = '5s';

CREATE TABLE IF NOT EXISTS group_usage_cost_totals (
    group_id BIGINT PRIMARY KEY,
    total_cost NUMERIC(20, 10) NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

WITH baseline_costs AS (
    SELECT logs.group_id, COALESCE(SUM(logs.actual_cost), 0) AS actual_cost
    FROM usage_logs logs
    WHERE logs.group_id IS NOT NULL
      AND logs.group_id > 0
      AND NOT EXISTS (
          SELECT 1
          FROM group_usage_cost_catchup catchup
          WHERE catchup.usage_log_id = logs.id
      )
    GROUP BY logs.group_id
)
INSERT INTO group_usage_cost_totals (group_id, total_cost, updated_at)
SELECT group_id, actual_cost, NOW()
FROM baseline_costs
ON CONFLICT (group_id) DO UPDATE SET
    total_cost = EXCLUDED.total_cost,
    updated_at = EXCLUDED.updated_at;

-- Drain and aggregate the catch-up rows visible after the baseline before
-- blocking writers. READ COMMITTED gives this statement a fresh snapshot, and
-- DELETE ... RETURNING makes the drain and aggregation one atomic step. Rows
-- committed after this statement remain in the catch-up table for the final
-- locked merge below.
WITH drained_catchup AS (
    DELETE FROM group_usage_cost_catchup
    RETURNING group_id, actual_cost
), drained_costs AS (
    SELECT group_id, COALESCE(SUM(actual_cost), 0) AS actual_cost
    FROM drained_catchup
    GROUP BY group_id
)
INSERT INTO group_usage_cost_totals (group_id, total_cost, updated_at)
SELECT group_id, actual_cost, NOW()
FROM drained_costs
ON CONFLICT (group_id) DO UPDATE SET
    total_cost = group_usage_cost_totals.total_cost + EXCLUDED.total_cost,
    updated_at = EXCLUDED.updated_at;

-- Bound every statement in the final cutover section. The lock is acquired only
-- after the historical scan and the pre-drain; a busy writer window fails fast
-- without replacing the old trigger or exposing a partial aggregate.
SET LOCAL statement_timeout = '15s';
LOCK TABLE usage_logs IN SHARE ROW EXCLUSIVE MODE;

INSERT INTO group_usage_cost_totals (group_id, total_cost, updated_at)
SELECT group_id, COALESCE(SUM(actual_cost), 0), NOW()
FROM group_usage_cost_catchup
GROUP BY group_id
ON CONFLICT (group_id) DO UPDATE SET
    total_cost = group_usage_cost_totals.total_cost + EXCLUDED.total_cost,
    updated_at = EXCLUDED.updated_at;

DROP TRIGGER IF EXISTS trg_capture_group_usage_cost_catchup ON usage_logs;
DROP FUNCTION IF EXISTS capture_group_usage_cost_catchup();

CREATE OR REPLACE FUNCTION increment_group_usage_cost_totals()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO group_usage_cost_totals (group_id, total_cost, updated_at)
    SELECT inserted.group_id, COALESCE(SUM(inserted.actual_cost), 0), NOW()
    FROM new_group_usage_logs inserted
    WHERE inserted.group_id IS NOT NULL
      AND inserted.group_id > 0
    GROUP BY inserted.group_id
    ON CONFLICT (group_id) DO UPDATE SET
        total_cost = group_usage_cost_totals.total_cost + EXCLUDED.total_cost,
        updated_at = EXCLUDED.updated_at;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS trg_increment_group_usage_cost_totals ON usage_logs;
CREATE TRIGGER trg_increment_group_usage_cost_totals
AFTER INSERT ON usage_logs
REFERENCING NEW TABLE AS new_group_usage_logs
FOR EACH STATEMENT
EXECUTE FUNCTION increment_group_usage_cost_totals();

DROP TABLE group_usage_cost_catchup;
