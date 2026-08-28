# 历史账号只读审计与回填方案（模型目录统一 · Sprint 9）

> 状态：**方案草案，未经用户授权，尚未执行任何数据库查询。**
> 本文档对应 `docs/model-catalog-unification-plan.md` 的 Sprint 9（Task 9.1 / 9.2）。
> Task 9.3（执行回填）属于可选数据修复，必须单独取得用户明确授权后才能执行。

## 1. 背景

模型目录统一改造后，用户私有账号（`owner_user_id IS NOT NULL`）在 `model_mapping`
缺失或为空时，单账号 `/models` 与房间创建不再返回 `200 + []`，而是返回明确业务错误码
（`ACCOUNT_TEST_MODEL_WHITELIST_MISSING` / `ACCOUNT_SHARE_MODE_CATALOG_EMPTY` 等）。
这批账号是**历史遗留**：在旧逻辑下空 mapping 会被静默回退为平台静态默认列表，因此
它们长期「看起来可用」，实际白名单为空。

本方案先只读盘点这些账号的数量与分布，再提出可回滚的回填方案；不自动改数据。

## 2. 审计目标

- 识别所有 `owner_user_id IS NOT NULL` 且 `model_mapping` 缺失或为空的账号。
- 输出：账号数量、平台分布、账号归属用户分布、每个账号当前可回填的定价模型数量。
- 输出**不得**包含完整 `credentials`、token、`model_mapping` 全量内容。

## 3. 只读审计查询方案（未执行，需授权）

以下 SQL 仅为方案草案，用于向用户说明查询意图与零写入影响。**当前未执行。**

```sql
-- 只读：统计私有账号中 model_mapping 缺失或为空的账号数量与平台分布
SELECT
  LOWER(BTRIM(a.platform)) AS platform,
  COUNT(*) AS account_count,
  COUNT(DISTINCT a.owner_user_id) AS owner_count
FROM accounts a
WHERE a.owner_user_id IS NOT NULL
  AND a.deleted_at IS NULL
  AND (
    a.credentials IS NULL
    OR a.credentials->'model_mapping' IS NULL
    OR a.credentials->'model_mapping' = 'null'::jsonb
    OR a.credentials->'model_mapping' = '{}'::jsonb
    OR jsonb_typeof(a.credentials->'model_mapping') = 'object'
       AND (a.credentials->'model_mapping')::jsonb = '{}'::jsonb
  )
GROUP BY LOWER(BTRIM(a.platform))
ORDER BY account_count DESC;
```

```sql
-- 只读：列出受影响账号 ID（仅用于核对影响范围，不含凭证）
SELECT a.id, LOWER(BTRIM(a.platform)) AS platform, a.owner_user_id
FROM accounts a
WHERE a.owner_user_id IS NOT NULL
  AND a.deleted_at IS NULL
  AND (
    a.credentials IS NULL
    OR a.credentials->'model_mapping' IS NULL
    OR a.credentials->'model_mapping' = 'null'::jsonb
    OR a.credentials->'model_mapping' = '{}'::jsonb
  )
ORDER BY a.id ASC;
```

### 3.1 平台范围

受影响平台以审计结果为准，但重点核对：
- `grok`（本次改造的直接触发平台，私有账号空 mapping 曾返回 `200 + []`）。
- `opencode`、`anthropic`、`openai` 等所有 `owner_user_id IS NOT NULL` 的私有账号平台。

### 3.2 安全边界

- 只读连接，禁止 `UPDATE / DELETE / INSERT / TRUNCATE`。
- 查询不触碰日志表、usage 表等大表。
- 输出仅含计数与账号 ID（如需），脱敏后展示。

## 4. 回填方案（Task 9.2，未执行）

### 4.1 回填策略

对每个空 mapping 的私有账号，用「该账号平台 + 账号所属分组的定价目录」生成
`model_mapping`，将目录中的模型 ID 映射到自身（`model -> model`）。回填前先 dry-run
输出每个账号将写入的模型集合，供用户确认。

### 4.2 事务边界与备份

- 逐账号或按平台分批，每个批次一个事务。
- 回填前对涉及的 `accounts.id + credentials` 做行级备份（复制到审计备份表或导出）。
- 只改写 `credentials->'model_mapping'`，不改其他字段。

### 4.3 回滚方案

- 记录每个账号回填前的 `credentials` 原文（或仅 `model_mapping` 原值），
  回滚时用备份值覆盖。
- 不删除任何历史模型、不覆盖非空 mapping。

### 4.4 禁止项

- 禁止运行时静默 fallback、全表无条件覆盖、删除历史模型。
- 禁止手写不可回滚 DDL。

## 5. 审批边界

- 执行只读审计（第 3 节）前，需用户明确授权。
- 执行回填（第 4 节）前，需用户看到 dry-run 结果并再次确认。
- 未确认时保持代码 Fail-Fast 与人工修复流程，不改动数据库。
