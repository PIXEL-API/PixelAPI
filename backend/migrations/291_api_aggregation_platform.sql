-- API聚合渠道（账号广场 "APIKEY 房间"）：
--   1. 扩展平台 CHECK 约束，放行 api_aggregation；
--   2. 创建 API 聚合账号模式分组并绑定 account_share_mode_groups；
--   3. account_share_memberships 增加未验证上游确认留痕列。
--
-- 设计说明：
--   - api_aggregation 房间账号由房主提供的上游 api_key + base_url 组成，
--     模型定价统一由平台侧 API 聚合分组渠道管理（PricedModelCatalog 按
--     platform=api_aggregation 查询），房主不能自由指定未定价模型。
--   - 消费者加入 api_aggregation 房间前必须确认「该聚合渠道未经过 PIXEL
--     验证」的风险提示，确认时间写入 memberships.unverified_upstream_ack_at。
--   - 模式分组沿用共享结算链路；管理员若已手工配置过分组映射则保留。

DO $$
BEGIN
    IF to_regclass('user_platform_quotas') IS NOT NULL THEN
        ALTER TABLE user_platform_quotas
            DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;
        ALTER TABLE user_platform_quotas
            ADD CONSTRAINT user_platform_quotas_platform_check
            CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                                'opencode', 'kimi', 'zhipu', 'deepseek', 'minimax', 'qwen',
                                'devin', 'api_aggregation'));
    END IF;
    IF to_regclass('composite_model_routes') IS NOT NULL THEN
        ALTER TABLE composite_model_routes
            DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;
        ALTER TABLE composite_model_routes
            ADD CONSTRAINT composite_model_routes_target_platform_check
            CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                                       'opencode', 'kimi', 'zhipu', 'deepseek', 'minimax', 'qwen',
                                       'devin', 'api_aggregation'));
    END IF;
END $$;

ALTER TABLE proxies
    DROP CONSTRAINT IF EXISTS proxies_platform_chk;
ALTER TABLE proxies
    ADD CONSTRAINT proxies_platform_chk
    CHECK (platform IN ('', 'openai', 'anthropic', 'gemini', 'antigravity', 'grok',
                        'opencode', 'kimi', 'zhipu', 'deepseek', 'minimax', 'qwen',
                        'devin', 'api_aggregation')) NOT VALID;
ALTER TABLE proxies
    VALIDATE CONSTRAINT proxies_platform_chk;

ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;
ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok',
                        'antigravity', 'opencode', 'kimi', 'zhipu', 'deepseek', 'minimax', 'qwen',
                        'devin', 'api_aggregation'));

ALTER TABLE channel_monitor_request_templates
    DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;
ALTER TABLE channel_monitor_request_templates
    ADD CONSTRAINT channel_monitor_request_templates_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok',
                        'antigravity', 'opencode', 'kimi', 'zhipu', 'deepseek', 'minimax', 'qwen',
                        'devin', 'api_aggregation'));

-- 消费者加入 APIKEY 房间时「已知晓未验证渠道」的确认留痕。
-- NULL 表示未确认/非聚合房间；只在最终 join 落库时写入，不靠前端字段。
ALTER TABLE account_share_memberships
    ADD COLUMN IF NOT EXISTS unverified_upstream_ack_at TIMESTAMPTZ;

-- API 聚合账号模式分组：消费者 API Key 加入 api_aggregation 房间时必须绑定
-- 该分组（account_share_mode_groups 现有契约），分组给密钥打 APIKEY 徽标。
DO $$
DECLARE
    group_id BIGINT;
BEGIN
    -- 保留管理员手工选定的模式分组映射。
    IF EXISTS (SELECT 1 FROM account_share_mode_groups WHERE platform = 'api_aggregation') THEN
        RETURN;
    END IF;

    SELECT id INTO group_id FROM groups
    WHERE name = 'API聚合分组' AND platform = 'api_aggregation' AND deleted_at IS NULL
    ORDER BY id LIMIT 1;

    IF group_id IS NULL THEN
        INSERT INTO groups (
            name, description, rate_multiplier, is_exclusive, status, owner_user_id,
            scope, platform, required_account_level, subscription_type,
            default_validity_days, allow_image_generation, image_rate_independent,
            image_rate_multiplier, claude_code_only, model_routing,
            model_routing_enabled, mcp_xml_inject, supported_model_scopes,
            sort_order, allow_messages_dispatch, require_oauth_only,
            require_privacy_set, default_mapped_model, messages_dispatch_model_config,
            rpm_limit, api_key_badge_type, api_key_badge_text, created_at, updated_at
        ) VALUES (
            'API聚合分组', '账号广场 APIKEY 聚合渠道模式分组；上游渠道未经过 PIXEL 验证，模型定价由平台统一管理。',
            1.0, FALSE, 'active', NULL, 'public', 'api_aggregation', '', 'standard',
            30, FALSE, FALSE, 1.0, FALSE, '{}'::jsonb, FALSE, TRUE, '[]'::jsonb,
            -900, TRUE, FALSE, FALSE, '', '{}'::jsonb, 0,
            'custom', 'APIKEY', NOW(), NOW()
        ) RETURNING id INTO group_id;
    END IF;

    INSERT INTO account_share_mode_groups (platform, group_id, created_at, updated_at)
    VALUES ('api_aggregation', group_id, NOW(), NOW())
    ON CONFLICT (platform) DO NOTHING;
END $$;
