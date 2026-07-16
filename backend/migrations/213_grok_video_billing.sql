-- Grok media generation uses the existing image-generation capability gate.
-- Preserve every existing group's explicit permission. Administrators must opt in
-- by enabling allow_image_generation; migrations must never broaden access.

-- Grok video group prices are per-second rates (USD/s), matching xAI billing.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS video_rate_independent BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS video_rate_multiplier DECIMAL(10,4) NOT NULL DEFAULT 1.0,
    ADD COLUMN IF NOT EXISTS video_price_480p DECIMAL(20,8),
    ADD COLUMN IF NOT EXISTS video_price_720p DECIMAL(20,8),
    ADD COLUMN IF NOT EXISTS video_price_1080p DECIMAL(20,8);

COMMENT ON COLUMN groups.video_rate_independent IS '视频生成是否使用独立倍率；false 表示共享分组有效倍率';
COMMENT ON COLUMN groups.video_rate_multiplier IS '视频生成独立倍率，仅 video_rate_independent=true 时生效';
COMMENT ON COLUMN groups.video_price_480p IS '480p 视频生成每秒单价（USD/s），Grok 平台使用';
COMMENT ON COLUMN groups.video_price_720p IS '720p 视频生成每秒单价（USD/s），Grok 平台使用';
COMMENT ON COLUMN groups.video_price_1080p IS '1080p 视频生成每秒单价（USD/s），Grok 平台使用';

-- Persist the exact video billing inputs so every charge remains auditable.
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS video_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS video_resolution VARCHAR(10),
    ADD COLUMN IF NOT EXISTS video_duration_seconds INTEGER;

COMMENT ON COLUMN usage_logs.video_count IS '视频生成数量；大于 0 表示本行是视频生成用量';
COMMENT ON COLUMN usage_logs.video_resolution IS '计费用视频分辨率：480p/720p/1080p';
COMMENT ON COLUMN usage_logs.video_duration_seconds IS '提交时请求的视频时长（秒），用于按秒计费';

-- Video rows keep image_count populated for legacy dashboards but do not have image_size.
-- Keep this constraint NOT VALID because older image rows may predate image_size;
-- PostgreSQL still enforces it for every row written after this migration.
ALTER TABLE usage_logs
    DROP CONSTRAINT IF EXISTS usage_logs_image_billing_size_check;

ALTER TABLE usage_logs
    ADD CONSTRAINT usage_logs_image_billing_size_check
    CHECK (
        image_count <= 0
        OR COALESCE(video_count, 0) > 0
        OR (
            image_size IS NOT NULL
            AND image_size IN ('1K', '2K', '4K', 'mixed')
        )
    ) NOT VALID;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'usage_logs_video_count_non_negative'
          AND conrelid = 'usage_logs'::regclass
    ) THEN
        ALTER TABLE usage_logs
            ADD CONSTRAINT usage_logs_video_count_non_negative
            CHECK (video_count >= 0) NOT VALID;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'usage_logs_video_billing_metadata_consistent'
          AND conrelid = 'usage_logs'::regclass
    ) THEN
        ALTER TABLE usage_logs
            ADD CONSTRAINT usage_logs_video_billing_metadata_consistent
            CHECK (
                (
                    video_count = 0
                    AND video_resolution IS NULL
                    AND video_duration_seconds IS NULL
                )
                OR (
                    video_count > 0
                    AND video_resolution IS NOT NULL
                    AND video_resolution IN ('480p', '720p', '1080p')
                    AND video_duration_seconds IS NOT NULL
                    AND video_duration_seconds BETWEEN 1 AND 15
                )
            ) NOT VALID;
    END IF;
END $$;

-- Validation is intentionally deferred to 213a so this transaction can commit
-- and release the ADD CONSTRAINT locks before PostgreSQL scans historical rows.
