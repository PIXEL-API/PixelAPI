ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS api_key_badge_type VARCHAR(20) NOT NULL DEFAULT 'hidden',
    ADD COLUMN IF NOT EXISTS api_key_badge_text VARCHAR(20) NOT NULL DEFAULT '';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'groups_api_key_badge_type_check'
          AND conrelid = 'groups'::regclass
    ) THEN
        ALTER TABLE groups
            ADD CONSTRAINT groups_api_key_badge_type_check
            CHECK (api_key_badge_type IN ('hidden', 'recommended', 'constrained', 'unavailable', 'custom'));
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'groups_api_key_badge_text_check'
          AND conrelid = 'groups'::regclass
    ) THEN
        ALTER TABLE groups
            ADD CONSTRAINT groups_api_key_badge_text_check
            CHECK (
                (api_key_badge_type = 'custom' AND BTRIM(api_key_badge_text) <> '')
                OR
                (api_key_badge_type <> 'custom' AND api_key_badge_text = '')
            );
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'groups_private_api_key_badge_check'
          AND conrelid = 'groups'::regclass
    ) THEN
        ALTER TABLE groups
            ADD CONSTRAINT groups_private_api_key_badge_check
            CHECK (
                scope <> 'user_private'
                OR (api_key_badge_type = 'hidden' AND api_key_badge_text = '')
            );
    END IF;
END $$;

COMMENT ON COLUMN groups.api_key_badge_type IS
    'API 密钥分组选择器标签类型：hidden, recommended, constrained, unavailable, custom';
COMMENT ON COLUMN groups.api_key_badge_text IS
    'API 密钥分组选择器自定义标签文本，仅 custom 类型使用，最多 20 个字符';
