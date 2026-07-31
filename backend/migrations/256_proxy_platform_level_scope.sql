SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '60s';

-- 代理平台归属：'' 表示通用代理（所有平台可用），否则为具体平台标识。
ALTER TABLE proxies
    ADD COLUMN IF NOT EXISTS platform VARCHAR(32) NOT NULL DEFAULT '';

-- 代理账号等级要求：'' 表示所有等级可用，否则仅对应等级的账号可绑定。
-- 注意：账号等级是「动态」的——管理员可在后台自定义增删等级，
-- 因此这里不能对 required_account_level 施加固定取值的 CHECK 约束，
-- 取值合法性由应用层（IsValidRequiredAccountLevel + 等级配置）负责校验。
ALTER TABLE proxies
    ADD COLUMN IF NOT EXISTS required_account_level VARCHAR(20) NOT NULL DEFAULT '';

UPDATE proxies
SET platform = ''
WHERE platform IS NULL;

UPDATE proxies
SET required_account_level = ''
WHERE required_account_level IS NULL;

-- 平台集合是固定的上游平台标识，可以安全地用 CHECK 约束。
ALTER TABLE proxies
    DROP CONSTRAINT IF EXISTS proxies_platform_chk;

ALTER TABLE proxies
    ADD CONSTRAINT proxies_platform_chk
    CHECK (platform IN ('', 'openai', 'anthropic', 'gemini', 'antigravity', 'grok')) NOT VALID;

ALTER TABLE proxies
    VALIDATE CONSTRAINT proxies_platform_chk;

-- 若历史迁移曾对 required_account_level 加过固定取值 CHECK，这里显式移除，
-- 以免管理员新增自定义等级时违反约束。等级取值改由应用层动态校验。
ALTER TABLE proxies
    DROP CONSTRAINT IF EXISTS proxies_required_account_level_chk;

CREATE INDEX IF NOT EXISTS proxy_platform_required_account_level
    ON proxies (platform, required_account_level)
    WHERE deleted_at IS NULL;

-- 归属策略（自本次更新起）：
--   * 保留所有「现有」代理的 owner_user_id 不变——历史上传的自有代理仍归原用户，
--     其已绑定的账号在重新鉴权时依旧可见，避免老用户掉线。
--   * 本次部署后不再允许用户上传代理，用户端只能选择平台代理（owner_user_id IS NULL）。
-- 因此这里「不」清空 owner_user_id，也不做任何数据回收。

COMMENT ON COLUMN proxies.platform
    IS 'Platform scope of the proxy; empty string means universal proxy available to all platforms';

COMMENT ON COLUMN proxies.required_account_level
    IS 'Required upstream account level (dynamic; validated by app layer); empty string means all levels allowed';

COMMENT ON COLUMN proxies.owner_user_id
    IS 'Legacy owner of a user-uploaded proxy; retained for grandfathered proxies. New proxies are platform-managed (NULL).';
