# 数据库迁移、Repository 与事务风险

## 正向控制

- `backend\internal\repository\migrations_runner.go:183` 按文件名排序执行 migration。
- `backend\internal\repository\migrations_runner.go:204` 校验已应用 migration checksum，发现不一致会报错。
- `backend\internal\repository\migrations_runner.go:240` 到 `:255` 对 `_notx.sql` 使用非事务路径，适配 `CREATE INDEX CONCURRENTLY`。
- `backend\internal\repository\migrations_runner.go:831` 到 `:853` 使用 advisory lock 串行化迁移。
- 本次未确认到可复核的 SQL 注入 P0/P1 线索。

## [P1] migration 182 在热表上事务内创建多个普通索引

- 状态：待确认风险
- 类型：数据库 / migration / 运维
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\migrations\182_content_moderation_account_share_scope.sql:10`
- 证据 1：`182_content_moderation_account_share_scope.sql:10` 到 `:13` 在 `content_moderation_logs` 上一次创建 4 个普通索引，没有 `CONCURRENTLY`。
- 证据 2：普通 migration 会在事务中执行，见 `backend\internal\repository\migrations_runner.go:262` 到 `:268`；项目规范 `backend\migrations\README.md:17` 到 `:31` 要求并发索引用 `_notx.sql` 和 `CREATE INDEX CONCURRENTLY IF NOT EXISTS`。
- 触发场景：生产发布时执行 migration 182，`content_moderation_logs` 已有较多写入。
- 用户体验：内容审核日志或相关风控功能可能变慢或不可用，发布健康检查变慢。
- 代码逻辑影响：热表索引建设与事务迁移耦合，锁等待可能拉长。
- 风险后果：发布窗口扩大，服务启动被迁移阻塞。
- 建议：列新增保留事务 migration，索引拆到 `*_notx.sql` 并使用 `CREATE INDEX CONCURRENTLY IF NOT EXISTS`。
- 置信度：Medium，因未连接生产库确认表规模。

## [P2] migration 编号存在历史重复和缺号，缺少 CI 约束

- 状态：已确认问题
- 类型：数据库 / migration 管理 / 可维护性
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\migrations\migrations.go:14`
- 证据 1：runner 在 `backend\internal\repository\migrations_runner.go:183` 按文件名排序执行，migration 文件名承担顺序语义。
- 证据 2：实际存在重复编号，例如 `backend\migrations\028_add_account_notes.sql`、`backend\migrations\028_add_usage_logs_user_agent.sql`、`backend\migrations\028_group_image_pricing.sql`；只读清单还显示历史缺号。
- 触发场景：未来合并新 migration、cherry-pick、回滚补丁或多分支并行开发。
- 用户体验：发布失败时很难判断哪个迁移真正先后执行。
- 代码逻辑影响：数字前缀不再是唯一版本序，checksum 追溯成本变高。
- 风险后果：迁移审查误判、重复编号继续扩散、生产 schema 对齐困难。
- 建议：CI 增加 migration 编号唯一性/单调性检查；对历史重复编号建立 legacy allowlist，新迁移强制唯一递增。
- 置信度：High

## [P2] Repository 完整更新用户对象会放大并发覆盖风险

- 状态：已确认问题
- 类型：Repository / 事务 / 资金一致性
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\repository\user_repo.go:219`
- 证据 1：`user_repo.go:219` 使用 `SetBalance(userIn.Balance)`，同时还写入大量用户字段。
- 证据 2：`user_repo.go:711` 到 `:713` 已存在 `AddBalance(amount)` 原子更新，但管理员余额调整路径未使用。
- 触发场景：某服务读出 user 旧快照，另一个服务同时更新余额、通知配置、角色或状态。
- 用户体验：用户余额或资料设置被旧值覆盖。
- 代码逻辑影响：Repository 的宽更新使局部修改变成全字段覆盖。
- 风险后果：资金、权限或配置状态被并发请求覆盖。
- 建议：把用户更新拆成按字段更新；资金类变更必须使用原子增量或事务行锁。
- 置信度：High

## [P3] Pagination helper 的 Offset 使用原始 PageSize

- 状态：已确认问题
- 类型：性能 / Repository helper
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\pkg\pagination\pagination.go:37`
- 证据 1：`Offset()` 使用原始 `PageSize` 计算偏移，`Limit()` 才把上限裁到 1000。
- 证据 2：repository 组合使用 `Offset(params.Offset()).Limit(params.Limit())`，例如 `backend\internal\repository\account_repo.go:1291` 到 `:1292`、`account_share_policy_repo.go:38` 到 `:39`。
- 触发场景：未来内部调用绕过 HTTP `ParsePagination`，传入超大 PageSize。
- 用户体验：列表查询可能变慢或返回空页。
- 代码逻辑影响：分页 helper 限制语义不一致。
- 风险后果：慢查询和资源浪费。
- 建议：`Offset()` 基于 `p.Limit()` 计算，或提供统一 Normalize 后再进入 repository。
- 置信度：Medium

## 待确认项

- 新增 migration 175 到 185 处于当前脏工作区，需要在发布前确认是否已在任何环境应用过，避免 checksum mismatch。
- `backend\internal\repository\migrations_runner.go` 存在历史 checksum compatibility rules，这是显式兼容历史变更的机制，不应误判为“无校验”，但应持续限制新增白名单。
