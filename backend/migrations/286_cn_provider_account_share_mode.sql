-- 为国产供应商账号补齐账号广场的公共模式分组映射。
-- 账号模式分组沿用现有共享结算链路；不存在时创建，重复执行安全。
DO $$
DECLARE
    provider TEXT;
    group_id BIGINT;
BEGIN
    FOREACH provider IN ARRAY ARRAY['kimi', 'zhipu', 'deepseek', 'minimax', 'qwen'] LOOP
        -- Preserve an existing operator-selected mode group.
        IF EXISTS (SELECT 1 FROM account_share_mode_groups WHERE platform = provider) THEN
            CONTINUE;
        END IF;
        SELECT id INTO group_id FROM groups
        WHERE name = provider || '账号模式' AND platform = provider AND deleted_at IS NULL
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
                rpm_limit, created_at, updated_at
            ) VALUES (
                provider || '账号模式', '国产供应商账号共享模式分组；倍率由消费者绑定的共享账号动态决定。',
                1.0, FALSE, 'active', NULL, 'public', provider, '', 'standard',
                30, FALSE, FALSE, 1.0, FALSE, '{}'::jsonb, FALSE, TRUE, '[]'::jsonb,
                -900, TRUE, FALSE, FALSE, '', '{}'::jsonb, 0, NOW(), NOW()
            ) RETURNING id INTO group_id;
        END IF;

        INSERT INTO account_share_mode_groups (platform, group_id, created_at, updated_at)
        VALUES (provider, group_id, NOW(), NOW())
        ON CONFLICT (platform) DO NOTHING;
    END LOOP;
END $$;
