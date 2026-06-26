# 仓库地图与覆盖台账

## 工作区状态

审查时 `git status --short` 显示当前工作区已有大量修改、删除和未跟踪文件，覆盖后端、前端、migration、deploy 和 docs。本次报告只新增 `docs/review/full-project-audit-2026-06-14/`，没有覆盖已有 `docs/CODE_REVIEW_2026-06-14_*.md`。

该状态影响审查解释：结论针对当前工作区快照，不代表远端 main、生产二进制或任何已发布 tag。

## 文件规模

基于 `rg --files` 的粗略盘点：

- 主要目录：`backend`、`frontend`、`deploy`、`assets`、`docs`、`.github`、`test`、`tools`。
- 主要类型：Go 文件约 1500+，SQL migration 约 200+，Vue/TS 前端文件约 500+，部署脚本和 workflow 分散在 `deploy` 与 `.github`。
- 前端栈：Vue 3、Vite、Pinia、Vitest、TypeScript、Tailwind、ESLint。
- 后端栈：Go module `github.com/Wei-Shaw/sub2api`，Gin、Ent、Postgres/Redis 相关 repository、支付 provider、OpenAI/Gemini/Anthropic 网关。

## 目录职责地图

| 目录 | 职责 | 审查覆盖 |
|---|---|---|
| `backend/cmd` | server 启动、wire 注入、版本 | 已覆盖启动、版本文件、测试结果 |
| `backend/internal/server` | router、routes、middleware | 已覆盖 admin/user/gateway/auth/payment 路由与安全头 |
| `backend/internal/handler` | HTTP handler、gateway、auth、admin、DTO | 已覆盖安全、网关、支付、设置、账号导出 |
| `backend/internal/service` | 核心业务、支付、计费、账号、商城、共享账号、调度 | 已覆盖支付退款、余额、billing cache、调度和前端契约 |
| `backend/internal/repository` | Ent repository、Redis cache、migration runner | 已覆盖 API Key、user balance、billing cache、migration runner |
| `backend/internal/payment` | 支付 provider 抽象和实现 | 已覆盖退款状态、幂等 request id、provider pending |
| `backend/migrations` | schema 演进 | 已覆盖 checksum、notx 规范、182 索引、编号重复 |
| `frontend/src/api` | API client 和 DTO | 已覆盖 auth token、admin settings、store/API Key 契约 |
| `frontend/src/stores` | Pinia 状态 | 已覆盖 auth localStorage、app settings、测试失败上下文 |
| `frontend/src/router` | 路由和权限保护 | 已覆盖正向路由测试和 OAuth 回调 |
| `frontend/src/views` | 用户端、管理端、商城、支付、账号共享 | 已覆盖关键体验和视觉风险 |
| `frontend/src/components` | Dialog、Toast、账号组件、表格、后台组件 | 已覆盖 BaseDialog、Toast、大型组件和测试失败 |
| `frontend/src/composables` | 表格、白名单、刷新、表单 | 已覆盖模型白名单测试失败和交互组件 |
| `deploy` | systemd、Docker、install、配置样例 | 已覆盖 install 回滚、Docker latest、旧/新发布路径并存 |
| `.github/workflows` | CI、release、安全扫描 | 已覆盖 Release 门禁、backend-ci、security scan 例外 |
| `test`、`tools` | 本地测试和工具 | 已覆盖 remote probe 凭据读取、audit exception 工具 |

## 路由覆盖兜底

后端入口：

- `backend/internal/server/router.go:53` 到 `:62` 挂载 request logger、CORS、安全头等全局 middleware。
- `backend/internal/server/router.go:103` 到 `:114` 注册 common/auth/user/admin/gateway/payment routes。
- `backend/internal/server/routes/admin.go:17` 到 `:18` admin group 统一使用 `adminAuth`。
- `backend/internal/server/routes/user.go:33` 到 `:35` user authenticated group 使用 `jwtAuth` 和 `BackendModeUserGuard`。
- `backend/internal/server/routes/gateway.go:118`、`:202` 分别挂载 Gemini `/v1beta` 与 Antigravity `/antigravity/v1beta`，这是 API Key IP 限制不一致问题的入口。

前端入口：

- `frontend/src/router/index.ts` 覆盖公开页、auth、用户页、admin 页。
- `frontend/src/views/HomeView.vue` 公开首页直接读 public settings。
- `frontend/src/views/user/CustomPageView.vue` 是自定义 iframe 入口。
- `frontend/src/views/StoreView.vue` 是商城购买入口。
- `frontend/src/views/user/AccountShareView.vue` 是账号共享用户入口，组件体量极大，维护风险较高。

## 数据流覆盖兜底

重点链路已覆盖：

- API Key 请求：HTTP header/key 参数 → gateway route → middleware → service cache → group/subscription/billing 检查。
- 支付退款：admin request → PaymentService Prepare/Execute → userRepo 扣余额/订阅扣减 → provider Refund → order status/audit。
- 余额调整：admin API → AdminService → userRepo.Update 或 UpdateBalance → billing cache invalidation → redeem code audit。
- 商城购买：StoreView → store API → ShopService transaction → stock/balance/points/auto delivery。
- OAuth 登录：provider callback → 后端生成 token → 前端 callback 页持久化。
- migration：embedded FS → runner sort/checksum/advisory lock → `_notx` 分支/事务分支。

## 未纳入深审或受限项

- 没有连接生产 Postgres/Redis，因此表规模、索引耗时、缓存实际命中、真实订单状态无法验证。
- 没有打开浏览器进行视觉截图，因此移动端弹窗遮挡类问题按代码和 CSS 证据定性。
- 没有运行 migration apply、Docker build、release workflow、install.sh。
- 没有审查第三方依赖源码，只检查了本仓库 lock/audit exception 和依赖使用方式。
