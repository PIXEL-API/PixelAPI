-- Upstream 237: extend provider/platform checks for MiniMax. Optional tables
-- are guarded because older Pixel installations may not have them yet.

DO $$
BEGIN
    IF to_regclass('user_platform_quotas') IS NOT NULL THEN
        ALTER TABLE user_platform_quotas
            DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;
        ALTER TABLE user_platform_quotas
            ADD CONSTRAINT user_platform_quotas_platform_check
            CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                                'opencode', 'kimi', 'zhipu', 'deepseek', 'minimax', 'qwen'));
    END IF;
    IF to_regclass('composite_model_routes') IS NOT NULL THEN
        ALTER TABLE composite_model_routes
            DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;
        ALTER TABLE composite_model_routes
            ADD CONSTRAINT composite_model_routes_target_platform_check
            CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                                       'opencode', 'kimi', 'zhipu', 'deepseek', 'minimax', 'qwen'));
    END IF;
END $$;

ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;
ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok',
                        'antigravity', 'opencode', 'kimi', 'zhipu', 'deepseek', 'minimax', 'qwen'));

ALTER TABLE channel_monitor_request_templates
    DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;
ALTER TABLE channel_monitor_request_templates
    ADD CONSTRAINT channel_monitor_request_templates_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok',
                        'antigravity', 'opencode', 'kimi', 'zhipu', 'deepseek', 'minimax', 'qwen'));
