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
        'Anthropic账号模式',
        '统一账号共享模式分组；倍率由消费者绑定的共享账号动态决定。',
        1.0,
        FALSE,
        'active',
        NULL,
        'public',
        'anthropic',
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
        -899,
        TRUE,
        TRUE,
        FALSE,
        '',
        '{}'::jsonb,
        0,
        NOW(),
        NOW()
    WHERE NOT EXISTS (
        SELECT 1 FROM groups
        WHERE name = 'Anthropic账号模式' AND deleted_at IS NULL
    )
    RETURNING id
),
resolved_group AS (
    SELECT id FROM inserted_group
    UNION ALL
    SELECT id FROM groups
    WHERE name = 'Anthropic账号模式' AND deleted_at IS NULL
    LIMIT 1
)
INSERT INTO account_share_mode_groups (platform, group_id, created_at, updated_at)
SELECT 'anthropic', id, NOW(), NOW()
FROM resolved_group
ON CONFLICT (platform) DO UPDATE
SET group_id = EXCLUDED.group_id,
    updated_at = NOW();

INSERT INTO account_share_mode_policies (
    platform,
    platform_share_ratio,
    owner_share_ratio,
    enabled,
    version,
    created_at,
    updated_at
)
SELECT
    'account_share_mode',
    COALESCE(p.platform_share_ratio, 0.10000000),
    COALESCE(p.owner_share_ratio, 0.90000000),
    COALESCE(p.enabled, TRUE),
    1,
    NOW(),
    NOW()
FROM (
    SELECT platform_share_ratio, owner_share_ratio, enabled
    FROM account_share_mode_policies
    WHERE platform = 'openai' AND deleted_at IS NULL
    ORDER BY id ASC
    LIMIT 1
) p
RIGHT JOIN (SELECT 1) seed ON TRUE
ON CONFLICT (platform) DO UPDATE
SET platform_share_ratio = EXCLUDED.platform_share_ratio,
    owner_share_ratio = EXCLUDED.owner_share_ratio,
    enabled = EXCLUDED.enabled,
    deleted_at = NULL,
    updated_at = NOW();

DROP INDEX IF EXISTS uq_account_share_memberships_active_consumer;
