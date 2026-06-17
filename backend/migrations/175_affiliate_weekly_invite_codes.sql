-- 邀请码注册合并到邀请返利码：每周配额与可选轮换。

ALTER TABLE user_affiliates
    ADD COLUMN IF NOT EXISTS aff_weekly_limit INTEGER NOT NULL DEFAULT 2,
    ADD COLUMN IF NOT EXISTS aff_weekly_used INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS aff_weekly_window_start TIMESTAMPTZ NOT NULL DEFAULT (
        date_trunc('week', NOW() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai'
    ),
    ADD COLUMN IF NOT EXISTS aff_code_expires_at TIMESTAMPTZ DEFAULT (
        (date_trunc('week', NOW() AT TIME ZONE 'Asia/Shanghai') + INTERVAL '7 days') AT TIME ZONE 'Asia/Shanghai'
    ),
    ADD COLUMN IF NOT EXISTS aff_code_auto_rotate BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS aff_code_rotated_at TIMESTAMPTZ;

UPDATE user_affiliates
SET aff_weekly_window_start = date_trunc('week', NOW() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai'
WHERE aff_weekly_window_start IS NULL;

UPDATE user_affiliates
SET aff_code_expires_at = (date_trunc('week', NOW() AT TIME ZONE 'Asia/Shanghai') + INTERVAL '7 days') AT TIME ZONE 'Asia/Shanghai'
WHERE aff_code_auto_rotate = true
  AND aff_code_expires_at IS NULL;

UPDATE user_affiliates
SET aff_code_expires_at = NULL
WHERE aff_code_auto_rotate = false;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'user_affiliates_weekly_limit_check'
    ) THEN
        ALTER TABLE user_affiliates
            ADD CONSTRAINT user_affiliates_weekly_limit_check
            CHECK (aff_weekly_limit >= 0);
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'user_affiliates_weekly_used_check'
    ) THEN
        ALTER TABLE user_affiliates
            ADD CONSTRAINT user_affiliates_weekly_used_check
            CHECK (aff_weekly_used >= 0);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_user_affiliates_code_cycle
    ON user_affiliates (aff_code, aff_code_expires_at, aff_weekly_window_start);

CREATE INDEX IF NOT EXISTS idx_user_affiliates_cycle_reset
    ON user_affiliates (aff_weekly_window_start, aff_code_auto_rotate);

COMMENT ON COLUMN user_affiliates.aff_weekly_limit IS '每周邀请码可使用次数，0 表示本周不可使用';
COMMENT ON COLUMN user_affiliates.aff_weekly_used IS '当前周窗口内已使用次数';
COMMENT ON COLUMN user_affiliates.aff_weekly_window_start IS '邀请码次数所属周窗口起点，按 Asia/Shanghai 周一 00:00 计算';
COMMENT ON COLUMN user_affiliates.aff_code_expires_at IS '邀请码过期时间；NULL 表示当前邀请码不过期';
COMMENT ON COLUMN user_affiliates.aff_code_auto_rotate IS '是否每周自动轮换邀请码；关闭时保留旧码但仍刷新每周次数';
COMMENT ON COLUMN user_affiliates.aff_code_rotated_at IS '最近一次自动或手动轮换邀请码时间';