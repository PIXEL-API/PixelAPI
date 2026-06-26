# 后端核心与网关风险

## 正向控制

- `backend/internal/server/router.go:53` 到 `:62` 全局挂载请求日志、CORS、安全头。
- `backend/internal/server/routes/admin.go:17` 到 `:18` admin 路由统一挂 `adminAuth`，本次未确认到 admin group 完全漏鉴权的问题。
- `backend/internal/server/routes/user.go:33` 到 `:35` user 路由统一挂 `jwtAuth` 和 `BackendModeUserGuard`。
- `backend/internal/repository/migrations_runner.go` 有 advisory lock、checksum、`_notx.sql` 分支和 checksum mismatch 报错，不是无校验迁移。

## [P1] Gemini/Google 鉴权绕过 API Key IP 限制

- 状态：已确认问题
- 类型：安全 / 后端逻辑 / 网关
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\server\middleware\api_key_auth_google.go:23`
- 证据 1：普通 API Key 鉴权在 `backend\internal\server\middleware\api_key_auth.go:89` 到 `:97` 检查 `IPWhitelist/IPBlacklist`，不允许时返回 `ACCESS_DENIED`。
- 证据 2：Google/Gemini 鉴权 `api_key_auth_google.go:23` 到 `:119` 只校验 key、用户、订阅/余额并设置上下文，没有读取 `IPWhitelist/IPBlacklist`；`backend\internal\server\routes\gateway.go:118` 和 `:202` 将 `/v1beta` 与 `/antigravity/v1beta` 绑定到该鉴权。
- 触发场景：用户给 API Key 配置 IP 白名单或黑名单后，通过 Gemini 原生 `/v1beta` 或 Antigravity `/antigravity/v1beta` 请求。
- 用户体验：同一个 key 在普通 `/v1` 被拒绝，但在 Gemini 原生入口可能成功，用户和管理员会误以为 IP 限制已全局生效。
- 代码逻辑影响：API Key 的安全策略在不同网关入口不一致。
- 风险后果：泄漏的 API Key 可绕过 IP 限制调用 Gemini/Antigravity 路径，造成安全和计费风险。
- 建议：抽出统一 API Key 策略校验函数，Google/Gemini 鉴权也执行 IP restriction；补 `/v1beta` whitelist/blacklist 回归测试。
- 置信度：High

## [P2] OpenAI 异步 usage 记录闭包读取 `gin.Context`

- 状态：已确认问题
- 类型：后端逻辑 / 计费审计 / 并发
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\handler\openai_gateway_handler.go:480`
- 证据 1：`backend\internal\service\usage_record_worker_pool.go:81` 到 `:83`、`:143` 到 `:160`、`:317` 到 `:328` 表明 usage 任务进入有界 worker 后异步执行。
- 证据 2：`openai_gateway_handler.go:480` 到 `:489`、`:925` 到 `:934`、`:1502` 到 `:1511` 和 `openai_chat_completions.go:341` 到 `:350` 在异步闭包内调用 `GetInboundEndpoint(c)` / `GetUpstreamEndpoint(c, ...)`；这些函数读取 `gin.Context`。
- 触发场景：OpenAI Responses、Messages、Chat Completions 或 WebSocket 请求成功后提交 usage 任务。
- 用户体验：账单、用量明细、排障信息中的入口/上游 endpoint 可能错乱或为空。
- 代码逻辑影响：请求结束后继续访问请求上下文，存在数据竞争和上下文复用导致的错误归因。
- 风险后果：事故排查和用量审计可信度下降，可能影响计费争议处理。
- 建议：提交异步任务前捕获 `inboundEndpoint/upstreamEndpoint` 普通值，闭包不再读取 `gin.Context`；补并发 usage 记录测试。
- 置信度：High

## [P2] Gemini 原生流式 failover 缺少已写出响应保护

- 状态：待确认风险
- 类型：后端逻辑 / 网关 / 流式响应
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\handler\gemini_v1beta_handler.go:480`
- 证据 1：`gemini_v1beta_handler.go:471` 到 `:486` 遇到 `UpstreamFailoverError` 会直接 `continue` 切换账号，没有比较 `c.Writer.Size()`。
- 证据 2：Anthropic 流式路径在 `backend\internal\handler\gateway_handler.go:846` 到 `:847`、`:977` 到 `:982` 有“已写出后禁止 failover”的保护；`gemini_messages_compat_service.go:2566` 到 `:2639` 流式处理会先写 header/body。
- 触发场景：Gemini native SSE 请求在部分内容已经写出后，上游错误被包装为可 failover 错误。
- 用户体验：客户端可能收到不同账号拼接出的 SSE 片段，表现为解析失败、重复内容或中断。
- 代码逻辑影响：Gemini handler 与已有 Anthropic 防护策略不一致。
- 风险后果：流式请求稳定性下降，且用量/账号归因更难追踪。
- 建议：forward 前记录 writer size，failover 前如已写出则停止切换并按流式错误结束；补 partial SSE 回归测试。
- 置信度：Medium

## [P2] Anthropic 用户消息队列在缓存异常时 fail-open

- 状态：待确认风险
- 类型：后端逻辑 / 限流 / 可用性
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\handler\gateway_handler.go:785`
- 证据 1：`gateway_handler.go:785` 到 `:800` serialize 模式获取队列失败只记录 warn，不阻止请求；`:801` 到 `:815` throttle 模式失败同样只记录 warn。
- 证据 2：`backend\internal\service\user_msg_queue_service.go:39` 到 `:40` 说明该服务用于真实用户消息账号级串行化和 RPM 延迟；`:104` 到 `:120`、`:144` 到 `:170` 在 cache nil 或 Redis/cache 错误时均 fail-open。
- 触发场景：Anthropic OAuth/SetupToken 真实用户消息启用 serialize 或 throttle，同时 Redis/cache 超时或不可用。
- 用户体验：用户仍能发请求，但更容易遇到上游 429、排队策略失效或账号并发异常。
- 代码逻辑影响：配置声明的串行化/RPM 保护在缓存故障时失效。
- 风险后果：上游真实账号被打爆、请求失败率升高、运营误判配置无效。
- 建议：增加严格模式配置；serialize 模式至少支持 fail-closed 返回 429/503，并增加告警指标。
- 置信度：Medium
