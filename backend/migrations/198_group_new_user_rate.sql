ALTER TABLE groups ADD COLUMN IF NOT EXISTS new_user_rate_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE groups ADD COLUMN IF NOT EXISTS new_user_rate_multiplier DECIMAL(10, 4) NOT NULL DEFAULT 1;
ALTER TABLE groups ADD COLUMN IF NOT EXISTS new_user_rate_window_seconds INTEGER NOT NULL DEFAULT 0;
ALTER TABLE groups ADD COLUMN IF NOT EXISTS new_user_rate_quota_usd DECIMAL(20, 10) NOT NULL DEFAULT 0;

ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS rate_multiplier_source VARCHAR(50) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_usage_logs_new_user_rate_quota
    ON usage_logs (user_id, group_id, created_at)
    WHERE group_id IS NOT NULL
      AND rate_multiplier_source = 'new_user_group';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'groups_new_user_rate_multiplier_chk'
    ) THEN
        ALTER TABLE groups
            ADD CONSTRAINT groups_new_user_rate_multiplier_chk
            CHECK (new_user_rate_multiplier > 0);
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'groups_new_user_rate_window_seconds_chk'
    ) THEN
        ALTER TABLE groups
            ADD CONSTRAINT groups_new_user_rate_window_seconds_chk
            CHECK (new_user_rate_window_seconds >= 0);
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'groups_new_user_rate_quota_usd_chk'
    ) THEN
        ALTER TABLE groups
            ADD CONSTRAINT groups_new_user_rate_quota_usd_chk
            CHECK (new_user_rate_quota_usd >= 0);
    END IF;
END $$;
