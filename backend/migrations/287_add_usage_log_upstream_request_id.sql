ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS upstream_request_id TEXT;

ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS billing_error TEXT;
