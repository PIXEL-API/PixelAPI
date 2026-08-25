WITH inserted_group AS (
    INSERT INTO groups (
        name,
        description,
        rate_multiplier,
        is_exclusive,
        status,
        owner_user_id,
        scope,
        platform,
        required_account_level,
        subscription_type,
        default_validity_days,
        allow_image_generation,
        image_rate_independent,
        image_rate_multiplier,
        claude_code_only,
        model_routing,
        model_routing_enabled,
        mcp_xml_inject,
        supported_model_scopes,
        sort_order,
        allow_messages_dispatch,
        require_oauth_only,
        require_privacy_set,
        default_mapped_model,
        messages_dispatch_model_config,
        rpm_limit,
        created_at,
        updated_at
    )
    SELECT
        'Opencode账号模式',
        '统一账号共享模式分组；倍率由消费者绑定的共享账号动态决定。',
        1.0,
        FALSE,
        'active',
        NULL,
        'public',
        'opencode',
        '',
        'standard',
        30,
        FALSE,
        FALSE,
        1.0,
        FALSE,
        '{}'::jsonb,
        FALSE,
        TRUE,
        '[]'::jsonb,
        -898,
        TRUE,
        FALSE,
        FALSE,
        '',
        '{}'::jsonb,
        0,
        NOW(),
        NOW()
    WHERE NOT EXISTS (
        SELECT 1 FROM groups
        WHERE name = 'Opencode账号模式' AND deleted_at IS NULL
    )
    RETURNING id
),
resolved_group AS (
    SELECT id FROM inserted_group
    UNION ALL
    SELECT id FROM groups
    WHERE name = 'Opencode账号模式' AND deleted_at IS NULL
    LIMIT 1
)
INSERT INTO account_share_mode_groups (platform, group_id, created_at, updated_at)
SELECT 'opencode', id, NOW(), NOW()
FROM resolved_group
ON CONFLICT (platform) DO UPDATE
SET group_id = EXCLUDED.group_id,
    updated_at = NOW();

-- 关联到 OPENCODE 渠道：渠道 restrict_models=t，其白名单 = channel_model_pricing 里
-- platform=opencode 且配了价的 20 个模型（mapping ∪ pricing 并联）。
-- 账号广场房间经此关联后，dispatch 时渠道白名单/定价自动生效，
-- 保证房间上架模型与渠道定价一致（避免把渠道未配价的模型塞进房间导致计费为 0）。
-- 参照 OpenAI账号模式 → CODEX 渠道(id=1) 的既有做法。
INSERT INTO channel_groups (channel_id, group_id)
SELECT c.id, g.id
FROM channels c
JOIN groups g ON g.name = 'Opencode账号模式' AND g.deleted_at IS NULL
WHERE c.name = 'OPENCODE'
ON CONFLICT (group_id) DO NOTHING;
