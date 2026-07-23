-- 216_usage_log_image_input_tokens.sql
-- usage_logs 单独记录图片输入 token 与费用，便于图片编辑、图生图等场景对账。
-- input_tokens 继续保留总输入 token；input_cost 改为仅记录文本输入费用，
-- image_input_cost 单独记录图片输入费用，total_cost 与 actual_cost 总额口径不变。
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS image_input_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS image_input_cost DECIMAL(20, 10) NOT NULL DEFAULT 0;
