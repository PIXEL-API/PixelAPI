-- Repair room placements created by the old release for owners whose
-- platform-specific private group has not been provisioned yet. Keep the
-- compatibility trigger only until the old release has drained.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '5min';

CREATE OR REPLACE FUNCTION account_share_online_ensure_room_private_topology(
    owner_key BIGINT,
    platform_key TEXT,
    account_key BIGINT
)
RETURNS BIGINT
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
DECLARE
    normalized_platform TEXT := LOWER(BTRIM(platform_key));
    generated_group_name TEXT;
    private_group_count INTEGER;
    resolved_group_id BIGINT;
    resolved_mode_group_id BIGINT;
    mode_group_count INTEGER;
    template_rate_multiplier NUMERIC := 1;
    template_daily_limit NUMERIC := NULL;
    template_weekly_limit NUMERIC := NULL;
    template_monthly_limit NUMERIC := NULL;
    template_rpm_limit INTEGER := 0;
BEGIN
    IF owner_key IS NULL OR owner_key <= 0 THEN
        RAISE EXCEPTION 'room placement owner is required for private topology repair'
            USING ERRCODE = '23514';
    END IF;
    IF normalized_platform NOT IN (
        'anthropic',
        'openai',
        'gemini',
        'antigravity',
        'grok'
    ) THEN
        RAISE EXCEPTION 'room placement platform % does not support private groups', normalized_platform
            USING ERRCODE = '23514';
    END IF;

    PERFORM 1
    FROM users
    WHERE id = owner_key
      AND deleted_at IS NULL
    FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'room placement owner % is unavailable', owner_key
            USING ERRCODE = '23503';
    END IF;

    SELECT COUNT(*), MIN(private_group.id)
    INTO private_group_count, resolved_group_id
    FROM groups private_group
    WHERE private_group.owner_user_id = owner_key
      AND private_group.platform = normalized_platform
      AND private_group.scope = 'user_private'
      AND private_group.deleted_at IS NULL;

    IF private_group_count > 1 THEN
        RAISE EXCEPTION
            'room placement owner % has ambiguous private groups for platform %',
            owner_key,
            normalized_platform
            USING ERRCODE = '23514';
    END IF;

    IF private_group_count = 1 THEN
        IF NOT EXISTS (
            SELECT 1
            FROM groups private_group
            WHERE private_group.id = resolved_group_id
              AND private_group.status = 'active'
              AND COALESCE(private_group.subscription_type, '') <> 'none'
        ) THEN
            RAISE EXCEPTION
                'room placement owner % has an inactive private group for platform %',
                owner_key,
                normalized_platform
                USING ERRCODE = '23514';
        END IF;
    ELSE
        generated_group_name := FORMAT(
            'private-u%s-%s',
            owner_key,
            normalized_platform
        );
        IF EXISTS (
            SELECT 1
            FROM groups existing_group
            WHERE existing_group.name = generated_group_name
              AND existing_group.deleted_at IS NULL
        ) THEN
            RAISE EXCEPTION
                'generated private group name % is already in use',
                generated_group_name
                USING ERRCODE = '23505';
        END IF;

        WITH template_settings AS (
            SELECT
                MAX(value) FILTER (
                    WHERE key = 'user_private_group_rate_multiplier'
                ) AS rate_multiplier,
                MAX(value) FILTER (
                    WHERE key = 'user_private_group_daily_limit_usd'
                ) AS daily_limit,
                MAX(value) FILTER (
                    WHERE key = 'user_private_group_weekly_limit_usd'
                ) AS weekly_limit,
                MAX(value) FILTER (
                    WHERE key = 'user_private_group_monthly_limit_usd'
                ) AS monthly_limit,
                MAX(value) FILTER (
                    WHERE key = 'user_private_group_rpm_limit'
                ) AS rpm_limit
            FROM settings
            WHERE key IN (
                'user_private_group_rate_multiplier',
                'user_private_group_daily_limit_usd',
                'user_private_group_weekly_limit_usd',
                'user_private_group_monthly_limit_usd',
                'user_private_group_rpm_limit'
            )
        )
        SELECT
            CASE
                WHEN BTRIM(rate_multiplier) ~ '^[+]?[0-9]+([.][0-9]+)?$'
                 AND BTRIM(rate_multiplier)::NUMERIC > 0
                    THEN BTRIM(rate_multiplier)::NUMERIC
                ELSE 1
            END,
            CASE
                WHEN BTRIM(daily_limit) ~ '^[+]?[0-9]+([.][0-9]+)?$'
                 AND BTRIM(daily_limit)::NUMERIC > 0
                    THEN BTRIM(daily_limit)::NUMERIC
                ELSE NULL
            END,
            CASE
                WHEN BTRIM(weekly_limit) ~ '^[+]?[0-9]+([.][0-9]+)?$'
                 AND BTRIM(weekly_limit)::NUMERIC > 0
                    THEN BTRIM(weekly_limit)::NUMERIC
                ELSE NULL
            END,
            CASE
                WHEN BTRIM(monthly_limit) ~ '^[+]?[0-9]+([.][0-9]+)?$'
                 AND BTRIM(monthly_limit)::NUMERIC > 0
                    THEN BTRIM(monthly_limit)::NUMERIC
                ELSE NULL
            END,
            CASE
                WHEN BTRIM(rpm_limit) ~ '^[+]?[0-9]+$'
                 AND BTRIM(rpm_limit)::NUMERIC <= 2147483647
                    THEN BTRIM(rpm_limit)::INTEGER
                ELSE 0
            END
        INTO
            template_rate_multiplier,
            template_daily_limit,
            template_weekly_limit,
            template_monthly_limit,
            template_rpm_limit
        FROM template_settings;

        INSERT INTO groups (
            name,
            description,
            platform,
            rate_multiplier,
            new_user_rate_enabled,
            new_user_rate_multiplier,
            new_user_rate_window_seconds,
            new_user_rate_quota_usd,
            is_exclusive,
            status,
            owner_user_id,
            scope,
            subscription_type,
            required_account_level,
            daily_limit_usd,
            weekly_limit_usd,
            monthly_limit_usd,
            allow_image_generation,
            image_rate_independent,
            image_rate_multiplier,
            video_rate_independent,
            video_rate_multiplier,
            default_validity_days,
            claude_code_only,
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
        VALUES (
            generated_group_name,
            FORMAT(
                'Private subscription group for user %s on %s.',
                owner_key,
                normalized_platform
            ),
            normalized_platform,
            template_rate_multiplier,
            FALSE,
            1,
            0,
            0,
            TRUE,
            'active',
            owner_key,
            'user_private',
            'subscription',
            '',
            template_daily_limit,
            template_weekly_limit,
            template_monthly_limit,
            FALSE,
            FALSE,
            0,
            FALSE,
            0,
            365,
            FALSE,
            FALSE,
            FALSE,
            '[]'::jsonb,
            0,
            normalized_platform = 'openai',
            FALSE,
            FALSE,
            '',
            '{}'::jsonb,
            template_rpm_limit,
            NOW(),
            NOW()
        )
        RETURNING id INTO resolved_group_id;
    END IF;

    INSERT INTO user_allowed_groups (user_id, group_id)
    VALUES (owner_key, resolved_group_id)
    ON CONFLICT (user_id, group_id) DO NOTHING;

    INSERT INTO user_subscriptions (
        user_id,
        group_id,
        starts_at,
        expires_at,
        status,
        assigned_at,
        notes,
        created_at,
        updated_at
    )
    SELECT
        owner_key,
        resolved_group_id,
        NOW(),
        NOW() + INTERVAL '365 days',
        'active',
        NOW(),
        'auto assigned by account-share online room topology repair',
        NOW(),
        NOW()
    WHERE NOT EXISTS (
        SELECT 1
        FROM user_subscriptions subscription
        WHERE subscription.user_id = owner_key
          AND subscription.group_id = resolved_group_id
          AND subscription.deleted_at IS NULL
    );

    SELECT COUNT(*), MIN(mode_group.group_id)
    INTO mode_group_count, resolved_mode_group_id
    FROM account_share_mode_groups mode_group
    WHERE mode_group.platform = normalized_platform;
    IF mode_group_count <> 1 THEN
        RAISE EXCEPTION
            'room placement platform % has ambiguous account-share mode groups',
            normalized_platform
            USING ERRCODE = '23514';
    END IF;

    IF account_key IS NOT NULL THEN
        INSERT INTO account_groups (account_id, group_id, priority, created_at)
        VALUES (account_key, resolved_group_id, 1, NOW())
        ON CONFLICT (account_id, group_id) DO NOTHING;

        INSERT INTO account_groups (account_id, group_id, priority, created_at)
        VALUES (account_key, resolved_mode_group_id, 1, NOW())
        ON CONFLICT (account_id, group_id) DO NOTHING;
    END IF;

    RETURN resolved_group_id;
END
$$;

CREATE OR REPLACE FUNCTION account_share_online_guard_room_private_topology()
RETURNS TRIGGER
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF NEW.placement_type = 'room' THEN
        PERFORM account_share_online_ensure_room_private_topology(
            NEW.owner_user_id,
            NEW.platform,
            NEW.account_id
        );
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_account_share_online_guard_room_private_topology
    ON account_external_placements;
CREATE TRIGGER trg_account_share_online_guard_room_private_topology
AFTER INSERT OR UPDATE OF account_id, owner_user_id, platform, placement_type
ON account_external_placements
FOR EACH ROW
EXECUTE FUNCTION account_share_online_guard_room_private_topology();

DO $$
DECLARE
    room_placement RECORD;
BEGIN
    FOR room_placement IN
        SELECT
            placement.account_id,
            placement.owner_user_id,
            placement.platform
        FROM account_external_placements placement
        WHERE placement.placement_type = 'room'
        ORDER BY placement.account_id
    LOOP
        PERFORM account_share_online_ensure_room_private_topology(
            room_placement.owner_user_id,
            room_placement.platform,
            room_placement.account_id
        );
    END LOOP;
END
$$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM account_external_placements placement
        WHERE placement.placement_type = 'room'
          AND (
              (
                  SELECT COUNT(*)
                  FROM groups private_group
                  WHERE private_group.owner_user_id = placement.owner_user_id
                    AND private_group.platform = placement.platform
                    AND private_group.scope = 'user_private'
                    AND private_group.status = 'active'
                    AND private_group.deleted_at IS NULL
                    AND COALESCE(private_group.subscription_type, '') <> 'none'
              ) <> 1
              OR NOT EXISTS (
                  SELECT 1
                  FROM account_groups account_group
                  JOIN groups private_group ON private_group.id = account_group.group_id
                  WHERE account_group.account_id = placement.account_id
                    AND private_group.owner_user_id = placement.owner_user_id
                    AND private_group.platform = placement.platform
                    AND private_group.scope = 'user_private'
                    AND private_group.status = 'active'
                    AND private_group.deleted_at IS NULL
                    AND COALESCE(private_group.subscription_type, '') <> 'none'
              )
              OR NOT EXISTS (
                  SELECT 1
                  FROM account_groups account_group
                  JOIN account_share_mode_groups mode_group
                    ON mode_group.group_id = account_group.group_id
                  WHERE account_group.account_id = placement.account_id
                    AND mode_group.platform = placement.platform
              )
          )
        LIMIT 1
    ) THEN
        RAISE EXCEPTION 'room placement private topology repair is incomplete';
    END IF;
END
$$;
