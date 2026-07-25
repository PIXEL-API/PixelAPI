-- Add an optional admin-defined category for grouping and filtering redeem codes.

ALTER TABLE redeem_codes
ADD COLUMN IF NOT EXISTS category VARCHAR(64) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_redeem_codes_category
ON redeem_codes(category);

COMMENT ON COLUMN redeem_codes.category IS '管理员定义的兑换码分类；空字符串表示未分类';
