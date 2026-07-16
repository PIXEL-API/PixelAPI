-- Backfill the Grok private group introduced after the original four-platform provisioning.
-- This migration is additive and idempotent: existing active Grok private groups,
-- subscriptions, and user/group grants are preserved.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM users u
        JOIN groups g
          ON g.name = FORMAT('private-u%s-grok', u.id)
         AND g.deleted_at IS NULL
        WHERE u.deleted_at IS NULL
          AND (
              g.owner_user_id IS DISTINCT FROM u.id
              OR g.scope IS DISTINCT FROM 'user_private'
              OR g.platform IS DISTINCT FROM 'grok'
          )
    ) THEN
        RAISE EXCEPTION 'cannot backfill Grok private groups: a generated private group name is already in use';
    END IF;
END $$;

WITH user_private_templates AS (
    SELECT
        u.id AS user_id,
        COALESCE(template.rate_multiplier, 1.0) AS rate_multiplier,
        template.daily_limit_usd,
        template.weekly_limit_usd,
        template.monthly_limit_usd,
        COALESCE(template.rpm_limit, 0) AS rpm_limit
    FROM users u
    LEFT JOIN LATERAL (
        SELECT
            g.rate_multiplier,
            g.daily_limit_usd,
            g.weekly_limit_usd,
            g.monthly_limit_usd,
            g.rpm_limit
        FROM groups g
        WHERE g.owner_user_id = u.id
          AND g.scope = 'user_private'
          AND g.platform IN ('anthropic', 'openai', 'gemini', 'antigravity')
          AND g.deleted_at IS NULL
        ORDER BY
            CASE g.platform
                WHEN 'anthropic' THEN 1
                WHEN 'openai' THEN 2
                WHEN 'gemini' THEN 3
                WHEN 'antigravity' THEN 4
                ELSE 5
            END,
            g.id
        LIMIT 1
    ) template ON TRUE
    WHERE u.deleted_at IS NULL
)
INSERT INTO groups (
    name,
    description,
    rate_multiplier,
    new_user_rate_multiplier,
    is_exclusive,
    status,
    owner_user_id,
    scope,
    platform,
    subscription_type,
    daily_limit_usd,
    weekly_limit_usd,
    monthly_limit_usd,
    default_validity_days,
    supported_model_scopes,
    allow_messages_dispatch,
    rpm_limit
)
SELECT
    FORMAT('private-u%s-grok', template.user_id),
    FORMAT('Private subscription group for user %s on grok.', template.user_id),
    template.rate_multiplier,
    1.0,
    TRUE,
    'active',
    template.user_id,
    'user_private',
    'grok',
    'subscription',
    template.daily_limit_usd,
    template.weekly_limit_usd,
    template.monthly_limit_usd,
    365,
    '[]'::jsonb,
    FALSE,
    template.rpm_limit
FROM user_private_templates template
ON CONFLICT DO NOTHING;

INSERT INTO user_allowed_groups (user_id, group_id)
SELECT g.owner_user_id, g.id
FROM groups g
JOIN users u ON u.id = g.owner_user_id AND u.deleted_at IS NULL
WHERE g.scope = 'user_private'
  AND g.platform = 'grok'
  AND g.status = 'active'
  AND g.deleted_at IS NULL
ON CONFLICT (user_id, group_id) DO NOTHING;

WITH existing_private_horizons AS (
    SELECT
        g.owner_user_id AS user_id,
        MAX(us.expires_at) AS expires_at
    FROM groups g
    JOIN user_subscriptions us
      ON us.group_id = g.id
     AND us.user_id = g.owner_user_id
     AND us.deleted_at IS NULL
     AND us.status = 'active'
     AND us.expires_at > CURRENT_TIMESTAMP
    WHERE g.scope = 'user_private'
      AND g.platform IN ('anthropic', 'openai', 'gemini', 'antigravity')
      AND g.deleted_at IS NULL
    GROUP BY g.owner_user_id
)
INSERT INTO user_subscriptions (
    user_id,
    group_id,
    starts_at,
    expires_at,
    status,
    assigned_at,
    notes
)
SELECT
    g.owner_user_id,
    g.id,
    CURRENT_TIMESTAMP,
    COALESCE(horizon.expires_at, CURRENT_TIMESTAMP + INTERVAL '365 days'),
    'active',
    CURRENT_TIMESTAMP,
    'auto assigned by Grok private group backfill'
FROM groups g
JOIN users u ON u.id = g.owner_user_id AND u.deleted_at IS NULL
LEFT JOIN existing_private_horizons horizon ON horizon.user_id = g.owner_user_id
WHERE g.scope = 'user_private'
  AND g.platform = 'grok'
  AND g.status = 'active'
  AND g.deleted_at IS NULL
ON CONFLICT DO NOTHING;
