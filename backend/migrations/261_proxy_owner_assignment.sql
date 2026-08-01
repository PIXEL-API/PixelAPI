SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

-- 代理归属语义更新（自 1.2.29 起）：
--   * owner_user_id 为 NULL：平台代理，所有用户可见可用（按 platform / required_account_level 过滤）。
--   * owner_user_id 非空：专属代理，仅对该用户显示可用（不受平台/等级过滤限制）。
--     来源包括管理员在代理管理页显式指派，以及迁移 256 保留的历史用户自有代理。
-- 本迁移仅更新列注释，无数据与结构变更。

COMMENT ON COLUMN proxies.owner_user_id
    IS 'Owner of the proxy. NULL = platform-managed proxy visible to all users; non-NULL = exclusive proxy visible/usable only by that user (admin-assigned since 1.2.29, or legacy user-uploaded proxies retained by migration 256).';
