-- Devin platform: extend provider/platform CHECK constraints. Optional tables
-- are guarded because older installations may not have them yet.

DO $$
BEGIN
    IF to_regclass('user_platform_quotas') IS NOT NULL THEN
        ALTER TABLE user_platform_quotas
            DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;
        ALTER TABLE user_platform_quotas
            ADD CONSTRAINT user_platform_quotas_platform_check
            CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                                'opencode', 'kimi', 'zhipu', 'deepseek', 'minimax', 'qwen',
                                'devin'));
    END IF;
    IF to_regclass('composite_model_routes') IS NOT NULL THEN
        ALTER TABLE composite_model_routes
            DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;
        ALTER TABLE composite_model_routes
            ADD CONSTRAINT composite_model_routes_target_platform_check
            CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                                       'opencode', 'kimi', 'zhipu', 'deepseek', 'minimax', 'qwen',
                                       'devin'));
    END IF;
END $$;

ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;
ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok',
                        'antigravity', 'opencode', 'kimi', 'zhipu', 'deepseek', 'minimax', 'qwen',
                        'devin'));

ALTER TABLE channel_monitor_request_templates
    DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;
ALTER TABLE channel_monitor_request_templates
    ADD CONSTRAINT channel_monitor_request_templates_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok',
                        'antigravity', 'opencode', 'kimi', 'zhipu', 'deepseek', 'minimax', 'qwen',
                        'devin'));

-- 代理平台归属放行 devin，允许管理员配置 Devin 专用代理。
ALTER TABLE proxies
    DROP CONSTRAINT IF EXISTS proxies_platform_chk;
ALTER TABLE proxies
    ADD CONSTRAINT proxies_platform_chk
    CHECK (platform IN ('', 'openai', 'anthropic', 'gemini', 'antigravity', 'grok',
                        'devin')) NOT VALID;
ALTER TABLE proxies
    VALIDATE CONSTRAINT proxies_platform_chk;
