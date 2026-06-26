# 安全、鉴权与权限风险

## 正向控制

- Admin 路由整体挂载 `adminAuth`：`backend\internal\server\routes\admin.go:17` 到 `:18`。
- 管理员 JWT 校验包含 token 有效性、用户激活、TokenVersion、管理员角色：`backend\internal\server\middleware\admin_auth.go:153`、`:161`、`:179`、`:185`、`:191`。
- CORS 避免 `* + credentials`：`backend\internal\server\middleware\cors.go:38`、`:42`。
- 基础安全头已挂载：`backend\internal\server\middleware\security_headers.go:67`。

## [P1] Gemini/Google 鉴权绕过 API Key IP 限制

- 状态：已确认问题
- 类型：安全 / 鉴权 / API Key
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\server\middleware\api_key_auth_google.go:23`
- 证据 1：普通 API Key 鉴权在 `backend\internal\server\middleware\api_key_auth.go:89` 到 `:97` 检查 IP 白名单/黑名单。
- 证据 2：Google 鉴权 `api_key_auth_google.go:23` 到 `:119` 未检查 IP 限制；`backend\internal\server\routes\gateway.go:118`、`:202` 将 Gemini 原生入口绑定到该鉴权。
- 触发场景：限制了 IP 的 API Key 被用于 `/v1beta` 或 `/antigravity/v1beta`。
- 用户体验：管理员认为 key 只能从指定 IP 使用，但 Gemini SDK 路径仍可能放行。
- 代码逻辑影响：鉴权策略分叉，导致同一 key 在不同入口安全语义不同。
- 风险后果：API Key 泄漏后攻击者可绕过 IP 限制消费余额或订阅额度。
- 建议：把 IP restriction 抽成共享校验并在所有 API Key 鉴权 middleware 中调用。
- 置信度：High

## [P1] 更新 API Key 时省略 IP 字段会清空现有限制

- 状态：已确认问题
- 类型：安全 / API 契约 / 权限策略
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\repository\api_key_repo.go:263`
- 证据 1：`backend\internal\handler\api_key_handler.go:75` 到 `:76` 使用非指针 `[]string` 接收 `ip_whitelist/ip_blacklist`，省略字段会成为空切片。
- 证据 2：`backend\internal\service\api_key_service.go:182` 到 `:183` 注释定义空数组清空；`backend\internal\repository\api_key_repo.go:263` 到 `:270` 空列表执行 `ClearIPWhitelist/ClearIPBlacklist`。
- 触发场景：用户只想改 key 名称、状态、额度或重置限流，请求体没有带 IP 字段。
- 用户体验：界面提示更新成功，但原有 IP 限制被静默删除。
- 代码逻辑影响：更新 DTO 无法区分“字段未传”和“明确清空”。
- 风险后果：安全策略被误放宽，且鉴权缓存失效后立即生效。
- 建议：更新 DTO 改为 `*[]string` 或使用字段存在性解析；未传保持不变，空数组才清空；补 preserve IP restrictions 测试。
- 置信度：High

## [P1] 首页公开 `home_content` 直接 `v-html` 渲染

- 状态：已确认问题
- 类型：安全 / 前端 / XSS
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\frontend\src\views\HomeView.vue:12`
- 证据 1：`frontend\src\views\HomeView.vue:12` 对 `homeContent` 直接 `v-html`，注释写明这是 admin-only setting，接受 XSS risk。
- 证据 2：`frontend\src\views\HomeView.vue:370` 从 `cachedPublicSettings.home_content` 取值；`backend\internal\handler\dto\settings.go:287` 暴露 `home_content`；对比 `frontend\src\components\common\AnnouncementPopup.vue:90`、`:106` 和 `frontend\src\views\public\LegalDocumentView.vue:90`、`:133` 均使用 DOMPurify。
- 触发场景：管理员误填 HTML、管理员账号被盗、或任何设置写入路径被滥用。
- 用户体验：所有访问首页的用户可能看到被注入的钓鱼表单、跳转、伪按钮或恶意内容。
- 代码逻辑影响：公开首页使用了与公告/法律文档不同的安全策略。
- 风险后果：持久 XSS 可导致登录态、token、API Key 或用户操作被窃取。
- 建议：统一使用 DOMPurify sanitize；URL iframe 模式也应走 URL 白名单或 `sanitizeUrl`。
- 置信度：High

## [P1] 自定义 iframe 把登录 token 传给任意 HTTP(S) URL

- 状态：已确认问题
- 类型：安全 / token 泄漏 / 前端集成
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\frontend\src\utils\embedded-url.ts:29`
- 证据 1：`frontend\src\views\user\CustomPageView.vue:105` 到 `:108` 调用 `buildEmbeddedUrl(... authStore.token ...)`，`CustomPageView.vue:56` 到 `:60` 把结果作为 iframe `src`。
- 证据 2：`frontend\src\utils\embedded-url.ts:7` 到 `:10` 定义 query key，`:29` 到 `:31` 把 `authToken` 写入 `token` query；`CustomPageView.vue:114` 到 `:117` 只检查 URL 是否以 `http://` 或 `https://` 开头。
- 触发场景：管理员配置一个外部自定义 iframe 页面，用户点击侧边栏自定义页面。
- 用户体验：用户无感访问外部页面，但自己的登录 token 已经作为 URL query 发送给第三方。
- 代码逻辑影响：自定义菜单配置与登录凭证传递耦合，缺少同源/白名单边界。
- 风险后果：token 进入第三方日志、浏览器历史、Referer、代理日志，形成会话泄漏。
- 建议：默认不向外部 URL 传 token；只允许同源或明确白名单携带短时一次性 embedded token；优先改为 postMessage 握手。
- 置信度：High

## [P2] OAuth 成功回调通过 URL fragment 传递 access/refresh token

- 状态：已确认问题
- 类型：安全 / OAuth / token 交接
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\handler\auth_email_oauth.go:209`
- 证据 1：`auth_email_oauth.go:209` 到 `:215`、`auth_linuxdo_oauth.go:771` 到 `:774` 把 `access_token` 和 `refresh_token` 写入回调 URL fragment 后 302。
- 证据 2：前端 `frontend\src\views\auth\OAuthCallbackView.vue:41`、`:46`、`:76` 读取和处理完整 URL；OAuth 路由注册在 `backend\internal\server\routes\auth.go:66`、`:67`、`:74`、`:75`。
- 触发场景：GitHub/Google/LinuxDo 等 OAuth 登录成功。
- 用户体验：用户落到回调页时地址栏曾出现完整 token fragment，排障截图或复制 URL 可能暴露凭证。
- 代码逻辑影响：登录凭证通过浏览器 URL 传递，而不是 opaque code 或 HttpOnly cookie。
- 风险后果：浏览器扩展、历史记录、截图、剪贴板和客服日志都可能接触长期 refresh token。
- 建议：改成 pending session/一次性 code + 前端 POST exchange，或 HttpOnly/Secure/SameSite cookie；前端发现 token fragment 后立即 `history.replaceState` 清理。
- 置信度：High

## [P2] 退出登录只撤销 refresh token，access token 仍可用到过期

- 状态：待确认风险
- 类型：安全 / 会话策略
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\handler\auth_handler.go:757`
- 证据 1：`/api/v1/auth/logout` 注册于 `backend\internal\server\routes\auth.go:47` 到 `:48`，处理逻辑只调用 refresh token revoke。
- 证据 2：`backend\internal\server\middleware\jwt_auth.go:49`、`:73`、`:75` 仍按签名、过期时间、用户状态、TokenVersion 校验 access token；`backend\internal\service\auth_service.go:1570` 到 `:1584` 存在提升 TokenVersion 的 `RevokeAllUserTokens`，logout 未调用。
- 触发场景：用户退出后，攻击者仍持有未过期 access token。
- 用户体验：用户以为“退出即失效”，但旧 access token 在 TTL 内仍可访问。
- 代码逻辑影响：refresh token 撤销和 access token 撤销语义不一致。
- 风险后果：如果 access token 被 XSS 或外部 iframe 泄漏，退出登录无法立即止损。
- 建议：确认产品语义。若要求退出即失效，引入 session id/jti 黑名单或提升当前会话 TokenVersion；否则缩短 access token TTL 并明确 UI 文案。
- 置信度：Medium

## [P2] refresh token 存在 localStorage 中

- 状态：待确认风险
- 类型：安全 / 前端存储
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\frontend\src\api\auth.ts:41`
- 证据 1：`frontend\src\api\auth.ts:34`、`:41` 将 access token 和 refresh token 存入 localStorage；`:58`、`:65` 从 localStorage 读取。
- 证据 2：登录/注册/2FA 后端响应含 token pair，前端请求拦截器会读取 token；未见 HttpOnly cookie 保存 refresh token 的主流程。
- 触发场景：站点存在 XSS、恶意浏览器扩展、第三方脚本污染或公开 HTML 注入被利用。
- 用户体验：正常使用无感，但会话可被持续刷新。
- 代码逻辑影响：长期凭证暴露给同源脚本。
- 风险后果：一旦发生 XSS，攻击者可持久化接管会话。
- 建议：refresh token 改为 HttpOnly/Secure/SameSite cookie，access token 尽量短期并只放内存；配套 CSRF 防护和 token rotation。
- 置信度：Medium

## [P2] 管理员账号数据导出返回完整敏感凭证

- 状态：待确认风险
- 类型：安全 / 管理后台 / 数据导出
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\backend\internal\server\routes\admin.go:375`
- 证据 1：`GET /api/v1/admin/accounts/data` 在 admin group 下，具备 admin JWT 校验。
- 证据 2：`backend\internal\handler\admin\account_data.go:73`、`:105` 与 `backend\internal\service\account_data_export.go:56`、`:66`、`:91`、`:92` 直接导出 `Credentials`、`Extra`、代理 `Password`；普通 DTO 有脱敏逻辑 `backend\internal\handler\dto\credentials_redact.go:5`、`:14`、`:21`。
- 触发场景：管理员导出账号数据，或导出响应被浏览器、代理、日志、共享文件捕获。
- 用户体验：导出迁移最完整，但文件天然携带上游账号 token、cookie、private_key、代理密码等。
- 代码逻辑影响：普通列表脱敏，数据导出不脱敏，形成两套敏感信息边界。
- 风险后果：一份导出文件可复用大量上游账号凭证。
- 建议：默认脱敏；完整导出必须显式开关、二次确认、审计日志、短期下载、加密文件，并限制更高权限或离线工具。
- 置信度：Medium
