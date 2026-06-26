# 修复优先级路线图

## P0

本次未发现可确认 P0。

## 第一批：立即处理的 P1

1. API Key 安全策略统一
   - 修复 Gemini/Google 鉴权绕过 IP 限制。
   - 修复 API Key 更新时省略 IP 字段清空限制。
   - 补 `/v1`、`/v1beta`、`/antigravity/v1beta` 的 IP whitelist/blacklist 回归测试。

2. token 外泄面收口
   - 首页 `home_content` 统一 DOMPurify sanitize。
   - 自定义 iframe 默认不携带真实登录 token，改为同源白名单或短时 embedded token。
   - 评估 OAuth URL fragment 交接和 localStorage refresh token 的替代方案。

3. 退款状态机和幂等
   - `ProviderStatusPending` 不得本地标记 refunded。
   - 持久化 provider refund id/status。
   - 退款重试复用同一 request id。

4. 余额一致性
   - 管理员余额调整改为事务 + 原子增量或 row lock。
   - 审计记录与余额更新同事务。
   - 退款扣减/回滚后失效或更新 billing balance cache。

5. Release 门禁
   - release workflow 必须依赖后端测试、前端 lint/typecheck/关键 vitest。
   - 处理当前全量前端 21 个失败测试中与关键路径相关的失败。

6. 商城前台购买阻断
   - 前台识别 `stock_unlimited`。
   - 数量必须整数校验。
   - 结算弹窗按标准 Dialog 修复移动端可达性。

## 第二批：P1 待确认风险

1. migration 182 锁表风险
   - 先确认生产 `content_moderation_logs` 规模和写入频率。
   - 若表已有明显规模，把索引拆到 `_notx.sql` 并使用 `CREATE INDEX CONCURRENTLY IF NOT EXISTS`。

2. 退款幂等策略
   - 确认现有 provider 是否已在外部做 order-level 幂等。
   - 即便外部有幂等，本地也建议落库 request id，避免审计不可追踪。

3. 账号完整导出策略
   - 确认是否允许管理员导出完整上游凭证。
   - 若允许，必须增加二次确认、审计、加密和短期下载。

4. logout 语义
   - 明确产品承诺：退出是否要立即使 access token 失效。
   - 如果是，则需要 session/jti 黑名单或 TokenVersion 策略。

5. 缓存 fail-open 策略
   - Anthropic 用户消息队列 Redis 异常时是否应继续放行，需要业务策略确认。
   - 若强调上游账号保护，建议提供严格模式。

## 第三批：P2 已确认问题

- 修复 OpenAI 异步 usage 记录读取 `gin.Context`。
- BaseDialog 滚动锁改为引用计数。
- CreateAccountModal 使用合法 BaseDialog width prop。
- 注册促销码/邀请码错误直接进入统一 Toast。
- Toast 移动端响应式修复。
- migration 编号 CI 检查。
- install.sh 升级原子化和健康回滚。
- Docker compose 和文档改为 tag/digest pinning。
- test 目录移除或隔离生产凭据读取脚本。
- 扩展前端 critical vitest 覆盖。

## 第四批：P3 与维护性

- 拆分超大前端组件：`CreateAccountModal.vue`、`EditAccountModal.vue`、`AccountShareView.vue`。
- 修正 pagination helper 的 Offset/Limit 语义。
- 跟踪 audit exception 到期并收口高危依赖。

## 建议实施顺序

1. 先修安全和资金：API Key、iframe token、home_content、退款状态机、余额事务/cache。
2. 再修发布门禁：release workflow、前端失败测试、critical test 扩容。
3. 再处理 migration 182：根据生产表规模决定是否拆 notx 并发索引。
4. 再做 UX 快速修复：商城库存、数量整数、Toast、Dialog。
5. 最后做结构治理：大组件拆分、migration 编号 CI、Docker/latest 文档治理。

## 验收建议

- 所有 P1 修复必须有回归测试。
- 资金类修复必须有幂等、重复回调、pending、并发和回滚测试。
- 安全类修复必须覆盖不同 gateway route 的同一策略。
- 前端关键路径必须至少跑 `pnpm --dir frontend run typecheck`、`lint:check`、相关 vitest。
- 发布前必须跑后端 `go test ./...`，并确认 migration 未改动已上线文件 checksum。
