-- 渠道模型定价：按一天内分钟区间覆盖基础价（峰谷价格段）。
-- start_minute/end_minute 使用本系统配置时区下的一天内分钟数，区间语义为 [start_minute, end_minute)。
-- 与 channel_pricing_intervals（context 维度）正交：命中时逐字段覆盖默认价，未填字段回退。

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

CREATE TABLE IF NOT EXISTS channel_pricing_time_ranges (
    id                   BIGSERIAL      PRIMARY KEY,
    pricing_id           BIGINT         NOT NULL REFERENCES channel_model_pricing(id) ON DELETE CASCADE,
    start_minute         INT            NOT NULL,
    end_minute           INT            NOT NULL,
    input_price          NUMERIC(20,12),
    output_price         NUMERIC(20,12),
    cache_write_price    NUMERIC(20,12),
    cache_read_price     NUMERIC(20,12),
    image_input_price    NUMERIC(20,12),
    image_cache_read_price NUMERIC(20,12),
    image_output_price   NUMERIC(20,12),
    per_request_price    NUMERIC(20,12),
    sort_order           INT            NOT NULL DEFAULT 0,
    created_at           TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_channel_pricing_time_ranges_start_minute
        CHECK (start_minute >= 0 AND start_minute < 1440),
    CONSTRAINT chk_channel_pricing_time_ranges_end_minute
        CHECK (end_minute > 0 AND end_minute <= 1440),
    CONSTRAINT chk_channel_pricing_time_ranges_range
        CHECK (start_minute < end_minute)
);

CREATE INDEX IF NOT EXISTS idx_channel_pricing_time_ranges_pricing_id
    ON channel_pricing_time_ranges (pricing_id);

COMMENT ON TABLE channel_pricing_time_ranges IS '渠道模型定价时间段价格：按一天内分钟区间覆盖基础价（峰谷价格段）';
COMMENT ON COLUMN channel_pricing_time_ranges.start_minute IS '开始分钟，闭区间，0 表示 00:00';
COMMENT ON COLUMN channel_pricing_time_ranges.end_minute IS '结束分钟，开区间，1440 表示 24:00';
COMMENT ON COLUMN channel_pricing_time_ranges.input_price IS 'token 模式：每 token 输入价';
COMMENT ON COLUMN channel_pricing_time_ranges.output_price IS 'token 模式：每 token 输出价';
COMMENT ON COLUMN channel_pricing_time_ranges.cache_write_price IS 'token 模式：缓存写入价';
COMMENT ON COLUMN channel_pricing_time_ranges.cache_read_price IS 'token 模式：缓存读取价';
COMMENT ON COLUMN channel_pricing_time_ranges.image_input_price IS '图片输入 token 价';
COMMENT ON COLUMN channel_pricing_time_ranges.image_cache_read_price IS '图片缓存读取 token 价';
COMMENT ON COLUMN channel_pricing_time_ranges.image_output_price IS '图片输出价（向后兼容）';
COMMENT ON COLUMN channel_pricing_time_ranges.per_request_price IS '按次/图片模式：每次请求价格';
