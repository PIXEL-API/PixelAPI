# Sub2API / Pixel 全项目审查总览

审查日期：2026-06-14
审查对象：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119` 当前工作区
审查方式：只读代码审查、只读本地命令验证、4 个只读子代理专项审查、主线程二次复核。未连接数据库，未运行 migration apply，未修改业务代码。

## 结论

当前仓库没有发现可确认的 P0。已确认的高风险集中在 API Key 安全策略不一致、支付退款状态机、余额/账务缓存、公开页面 HTML 注入、自定义 iframe token 外传、Release 门禁和商城关键路径。

本次报告去重后列出：

- P0：0 个。
- P1：12 个，其中 10 个已确认问题，2 个待确认风险。
- P2：20 个，其中 14 个已确认问题，6 个待确认风险。
- P3：3 个。

总体健康度判断：后端路由和迁移 runner 有较多正向控制，例如 admin group 统一挂载 `adminAuth`、普通 API Key 鉴权有 IP 限制、migration 有 checksum 和 advisory lock。但是资金、安全、发布和前端关键交易路径存在若干跨模块缺口，必须优先修复。

## 最优先处理清单

1. [P1] Gemini/Google 鉴权绕过 API Key IP 限制。
   位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\server\middleware\api_key_auth_google.go:23`

2. [P1] 更新 API Key 时省略 IP 字段会清空现有限制。
   位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\repository\api_key_repo.go:263`

3. [P1] 支付网关退款返回 pending 时本地直接标记成功。
   位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\service\payment_refund.go:348`

4. [P1] 退款扣减/回滚余额未失效 billing balance cache。
   位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\service\payment_refund.go:293`

5. [P1] 管理员余额调整非原子读改写。
   位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\service\admin_service.go:960`

6. [P1] 管理员余额调整审计失败仍返回成功。
   位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\service\admin_service.go:999`

7. [P1] 首页公开 `home_content` 直接 `v-html` 渲染且未清洗。
   位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\frontend\src\views\HomeView.vue:12`

8. [P1] 自定义 iframe 页面把登录 token 作为 query 参数传给任意 HTTP(S) URL。
   位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\frontend\src\utils\embedded-url.ts:29`

9. [P1] Release workflow 不依赖测试和 lint，且 goreleaser 跳过 validate。
   位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\.github\workflows\release.yml:88`

10. [P1] 商城无限库存商品在前台会被判定为售罄。
   位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\frontend\src\views\StoreView.vue:489`

11. [P1 待确认] 退款幂等 request id 未持久化且按时间戳生成，重试可能重复发起退款。
    位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\payment\provider\wxpay.go:476`

12. [P1 待确认] migration 182 在热表上事务内创建多个普通索引，存在发布锁表风险。
    位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\migrations\182_content_moderation_account_share_scope.sql:10`

## 已执行验证命令

- `git status --short`：工作区存在大量已有修改和未跟踪文件，本报告基于当前工作区，不等同于干净 main。
- `rg --files`：用于生成覆盖台账。
- `go test ./...`，工作目录 `backend`：通过。
- `pnpm --dir frontend run typecheck`：通过。
- `pnpm --dir frontend run lint:check`：通过。
- `pnpm --dir frontend run test:run`：失败，21 个前端测试失败。主要集中在 `BulkEditAccountModal.spec.ts` 缺 Pinia、`useModelWhitelist.spec.ts` 模型预期、`AccountUsageCell.spec.ts`、`settings.authSourceDefaults.spec.ts`。

## 风险热区

- 安全权限：API Key IP 限制在 Gemini 原生入口不一致；API Key 更新 DTO 无法区分“未传”和“清空”；公开 HTML 与 iframe token 外传扩大 XSS 后果。
- 支付资金：退款状态机没有区分 provider pending 和 success；退款幂等键不稳定；余额扣减和缓存失效不统一。
- 管理后台：余额调整缺事务和强审计；账号导出允许完整敏感凭证，需要业务策略确认。
- 数据库迁移：新迁移对日志表做普通索引；历史 migration 编号重复，未来合并风险高。
- 前端体验：商城、弹窗、Toast、注册错误反馈、数量输入等关键路径有用户可感知问题。
- 发布运维：Release 与 CI 门禁割裂；旧 Docker/latest 文档与当前 binary/systemd 生产流程并存，容易误用。

## 审查边界

没有连接生产数据库，因此与生产数据规模、真实订单状态、真实 Redis TTL、线上 migration applied 状态相关的结论均按“待确认风险”处理。没有运行任何写库、迁移、格式化、lint fix、代码生成或自动修复命令。
