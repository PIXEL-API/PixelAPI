ALTER TABLE channel_model_pricing
    ADD COLUMN IF NOT EXISTS image_input_price NUMERIC(20, 12),
    ADD COLUMN IF NOT EXISTS image_cache_read_price NUMERIC(20, 12);

ALTER TABLE channel_account_stats_model_pricing
    ADD COLUMN IF NOT EXISTS image_input_price NUMERIC(20, 12),
    ADD COLUMN IF NOT EXISTS image_cache_read_price NUMERIC(20, 12);

COMMENT ON COLUMN channel_model_pricing.image_input_price IS 'Image input token price in USD. NULL means use normal input price.';
COMMENT ON COLUMN channel_model_pricing.image_cache_read_price IS 'Image cached input token price in USD. NULL means use normal cache read price.';
COMMENT ON COLUMN channel_account_stats_model_pricing.image_input_price IS 'Image input token price in USD. NULL means use normal input price.';
COMMENT ON COLUMN channel_account_stats_model_pricing.image_cache_read_price IS 'Image cached input token price in USD. NULL means use normal cache read price.';
