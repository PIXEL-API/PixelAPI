-- Capture usage rows committed while the lifetime group-cost baseline is built
-- by the following migration. Keeping one short-lived row per new usage log
-- lets migration 272 exclude captured rows from its MVCC baseline and then
-- merge every concurrent insert exactly once.
CREATE TABLE IF NOT EXISTS group_usage_cost_catchup (
    usage_log_id BIGINT PRIMARY KEY,
    group_id BIGINT NOT NULL,
    actual_cost NUMERIC(20, 10) NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_group_usage_cost_catchup_group_id
    ON group_usage_cost_catchup (group_id);

CREATE OR REPLACE FUNCTION capture_group_usage_cost_catchup()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO group_usage_cost_catchup (usage_log_id, group_id, actual_cost)
    SELECT inserted.id, inserted.group_id, COALESCE(inserted.actual_cost, 0)
    FROM new_group_usage_logs inserted
    WHERE inserted.group_id IS NOT NULL
      AND inserted.group_id > 0
    ON CONFLICT (usage_log_id) DO NOTHING;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS trg_capture_group_usage_cost_catchup ON usage_logs;
CREATE TRIGGER trg_capture_group_usage_cost_catchup
AFTER INSERT ON usage_logs
REFERENCING NEW TABLE AS new_group_usage_logs
FOR EACH STATEMENT
EXECUTE FUNCTION capture_group_usage_cost_catchup();
