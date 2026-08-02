-- 修复：user_provider_default_grants 表的 provider_type check 约束
-- 与 users / auth_identities / auth_identity_channels / pending_auth_sessions 保持一致
-- （迁移 156 放开了那四张表，唯独漏了本表）。
--
-- 影响：管理员一旦开启 auth_source_default_github_grant_on_first_bind
-- 或 auth_source_default_google_grant_on_first_bind，OAuth 首次绑定时
-- 写入本表的 INSERT 会违反 CHECK，导致整个绑定事务 abort。
--
-- 对应上游 migrations/140_extend_user_provider_default_grants_check.sql，
-- 但本地不提供钉钉登录，故取值集合不含 'dingtalk'（避免造出没有写入方的合法值）。

ALTER TABLE user_provider_default_grants
    DROP CONSTRAINT IF EXISTS user_provider_default_grants_provider_type_check;

ALTER TABLE user_provider_default_grants
    ADD CONSTRAINT user_provider_default_grants_provider_type_check
    CHECK (provider_type IN ('email', 'linuxdo', 'wechat', 'oidc', 'github', 'google'));
