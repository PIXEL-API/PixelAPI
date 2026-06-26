# 后端业务、安全与网关审查

## 已确认问题

### B-01 P1 Gemini/Antigravity v1beta API key 校验绕过 IP、过期和额度限制

- 位置：`backend/internal/server/routes/gateway.go:123`、`backend/internal/server/routes/gateway.go:208`、`backend/internal/server/middleware/api_key_auth_google.go:23`
- 证据：`/v1beta` 和 `/antigravity/v1beta` 使用 `APIKeyAuthWithSubscriptionGoogle`。该中间件只检查 `GetByKey`、`IsActive()`、用户状态、订阅或余额，见 `api_key_auth_google.go:35-108`。
- 对照证据：普通 API key middleware 在 `api_key_auth.go:89-97` 检查 IP 白/黑名单，在 `api_key_auth.go:155-173` 检查 `expired/quota_exhausted` 和运行时 `IsExpired()/IsQuotaExhausted()`。
- 影响：设置了 IP 限制、已过期但 status 仍 active、或 quota 已耗尽的 key，仍可能访问 Gemini native 和 Antigravity v1beta 链路。
- 建议：抽出普通链路和 Google 链路共用的 key 资格校验函数，至少补齐 IP、status、运行时过期、运行时 quota 检查。新增 v1beta 的 IP 拒绝、过期、quota 耗尽测试。
- 置信度：高。

### B-02 P2 登录/注册成功响应在 refresh token 存储失败时静默降级

- 位置：`backend/internal/handler/auth_handler.go:95`、`backend/internal/service/auth_service.go:1394`
- 证据：`respondWithTokenPair` 在 `GenerateTokenPair` 失败后记录 error，但继续生成 access token 并返回成功响应，见 `auth_handler.go:103-117`。
- 对照证据：`GenerateTokenPair` 要求 `refreshTokenCache` 可用，`StoreRefreshToken` 失败会返回错误，见 `auth_service.go:1395-1453`。
- 影响：Redis/cache 或 refresh token 存储故障会被包装成 200 成功登录，客户端没有 refresh token，后续续期失败；服务端也更难从接口成功率发现会话存储故障。
- 建议：默认 fail-fast，refresh token 存储失败时返回明确错误。如果确实要兼容 access-token-only，应在配置和响应字段中显式标记，不应静默降级。
- 置信度：高。

### B-03 P2 粘性会话日志在 Info 级别输出原始 session 和 metadata.user_id

- 位置：`backend/internal/handler/gateway_handler.go:264`、`backend/internal/handler/gateway_handler.go:287`、`backend/internal/handler/gateway_handler.go:769`、`backend/internal/service/gateway_service.go:716`
- 证据：handler 日志记录 `session_hash`、`metadata_user_id_raw`、`session_key`；service 解析 metadata 时记录 `session_id`、`device_id`，解析失败时记录原始 `metadata_user_id`。
- 对照证据：OpenAI 链路已有敏感值 hash/truncate 处理，见 `backend/internal/service/openai_gateway_service.go:1082`、`backend/internal/service/openai_gateway_service.go:1108`。
- 影响：应用日志可能泄露用户设备、会话标识和粘性路由 key，增加用户行为关联和隐私泄漏风险。
- 建议：Info 级别只记录 hash/truncate 后的值；原始 metadata 只能在 Debug 且显式开关下短期启用。
- 置信度：高。

### B-04 P2 账号模式 seat billing worker 未纳入统一 shutdown

- 位置：`backend/internal/service/wire.go:512`、`backend/internal/service/account_share_mode.go:496`、`backend/cmd/server/wire.go:72`
- 证据：`ProvideAccountShareModeService` 创建服务后调用 `svc.StartSeatBillingWorker()`，worker 内部 ticker 定期执行 `processSeatBillingOnce()`。
- 对照证据：`AccountShareModeService` 有 `StopSeatBillingWorker()`，但 `backend/cmd/server/wire.go:72-103` 的 `provideCleanup` 参数不包含该服务，cleanup 步骤在关闭 Redis/Ent 前没有停止它。
- 影响：进程退出时 worker 可能继续访问 repo/cache，而 Redis/Ent 已开始关闭，导致 shutdown 期间竞态、噪音日志或未完成的账单处理。
- 建议：把 `*service.AccountShareModeService` 注入 `provideCleanup`，在 Redis/Ent 关闭前调用 `StopSeatBillingWorker()`。补一个 wire 生成代码检查或生命周期单测。
- 置信度：高。

### B-05 P2 账号模式前端用中文名称识别分组，和后端真实映射解耦

- 位置：`frontend/src/views/user/AccountShareView.vue:1456`、`backend/internal/repository/account_share_mode_repo.go:103`
- 证据：前端用 `group.platform === 'openai' && (group.name === 'OpenAI账号模式' || group.name.includes('账号模式'))` 查找模式分组。
- 对照证据：后端真实绑定在 `account_share_mode_groups(platform, group_id)`，见 `account_share_mode_repo.go:103-108`；DTO `backend/internal/handler/dto/types.go:100-112` 暴露的 Group 没有模式字段。
- 影响：管理员一旦重命名分组，后端调度仍能按映射工作，但用户端可能找不到账号模式 API key，导致页面功能失效。
- 建议：后端 Group DTO 增加结构化字段，如 `account_share_mode_enabled` 或 `share_mode_platform`；前端按字段识别，不按中文名称识别。
- 置信度：高。

### B-06 P3 Backend mode 下 refresh 先轮转 token，再拒绝非管理员

- 位置：`backend/internal/handler/auth_handler.go:720`、`backend/internal/service/auth_service.go:1531`、`backend/internal/server/middleware/backend_mode_guard.go:65`
- 证据：`BackendModeAuthGuard` 放行 `/auth/refresh`；handler 先调用 `RefreshTokenPair`，service 先删除旧 refresh token 并生成新 token，然后 handler 才因非 admin 返回 403。
- 影响：非管理员在 backend mode 下刷新会消耗旧 refresh token，但拿不到新 token，表现为被强制掉线。
- 建议：backend mode 角色检查前移到 refresh token 轮转前，或在 service 增加“只验证不轮转”的预检。
- 置信度：高。

### B-07 P3 `openai_gateway_service.go` 存在大量 UTF-8 乱码注释

- 位置：`backend/internal/service/openai_gateway_service.go:42`、`:1521`、`:2633` 等。
- 证据：按 UTF-8 读取出现 `绮樻€т細璇漈TL`、`鍙傜収 Claude BetaPolicy` 等 mojibake。搜索 `鍙|鐨|锛|绋|鏈|璇` 有大量命中。
- 影响：核心网关文件的注释不可读，后续维护者容易误解 sticky session、fast policy、WebSocket、billing 等关键逻辑。
- 建议：用原始编码或历史版本恢复中文注释；如果无法恢复，改成准确英文注释。不要在功能修改中继续叠加乱码。
- 置信度：高。

## 待确认风险

### B-R01 P2 等待计划前注册会话，可能让未拿到账户槽位的请求占用 session limit

- 位置：`backend/internal/service/gateway_service.go:1555`、`:1752`、`:2130`、`:2758`、`backend/internal/repository/session_limit_cache.go:45`
- 证据：选择服务在返回 `AccountWaitPlan` 前调用 `checkAndRegisterSession`；Redis 脚本会 `ZADD` 新 session，并靠 idle timeout 过期。
- 风险：如果等待队列满或 slot 等待失败，未看到失败路径 unregister。大量不同 session_id 的尝试请求可能占满账号 `max_sessions`。
- 建议：先确认产品语义。如果“拿到上游槽位才算会话”，注册应后移或失败时显式 unregister。
- 置信度：中。

### B-R02 P2 账号模式会员并发在依赖缺失时 fail-open

- 位置：`backend/internal/service/account_share_mode.go:1539`、`backend/internal/service/concurrency_service.go:211`
- 证据：`AccountShareModeService.AcquireMembershipSlot` 在 service 或 concurrencyService 为 nil 时直接返回 acquired；`ConcurrencyService` 在 `cache == nil` 或 cache 不实现接口时也直接 acquired。
- 限制：生产 DI 当前确实注入了 Redis concurrency cache，因此这不是已确认生产绕过。
- 风险：未来替换 cache、测试环境或错误 DI 会静默失去 per-user 并发限制。
- 建议：生产路径 fail-closed 或至少启动期验证依赖；测试环境用显式配置开关放行，不要靠 nil 语义。
- 置信度：中高。

### B-R03 P2 仓储层允许余额扣成负数

- 位置：`backend/internal/repository/usage_billing_repo.go:265`、`backend/internal/repository/user_repo.go:728`
- 证据：扣费 SQL 直接 `SET balance = balance - $1`，没有 `balance >= amount` 条件；旧 `DeductBalance` 通过 `AddBalance(-amount)` 扣费且注释允许透支。
- 限制：是否允许透支需要业务确认，不能在未确认规则前判定为 bug。
- 建议：如果不允许透支，把余额条件下沉到 SQL 并返回明确错误；如果允许透支，要有策略字段、上限、审计原因和告警。
- 置信度：中。

## 已覆盖但未列为问题

- Usage billing 重复计费未确认：`usage_billing_repo.go` 有事务和 request id 去重路径。
- Payment/Shop 履约并发问题未确认：webhook 先验签，充值/订阅履约有状态条件锁，发卡使用订单行锁和 `FOR UPDATE SKIP LOCKED`。
- Withdrawal/Affiliate/Admin revenue 权限绕过未确认：相关路由挂在统一 AdminAuth 下，提现提交和关闭路径有行锁与状态检查。
