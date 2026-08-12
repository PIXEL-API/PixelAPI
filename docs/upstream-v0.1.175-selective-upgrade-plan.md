# Pixel 对上游 v0.1.175 的选择性升级总计划

> 审计日期：2026-08-12
> Pixel 工作区：`codex/pixel-ui`，HEAD `da71b428dda5cafd11d764858e4a6dcdcece7db2`
> 上游稳定基线：`Wei-Shaw/sub2api v0.1.175`，功能提交 `93c32fa1a2450351561abc46156d2e28cb5f74ca`
> 目标：在保留 Pixel 自研产品合同的前提下，选择性吸收安全修复、资损修复、网关可靠性优化和高价值新功能。

## 1. 执行结论

本项目不适合把上游 `v0.1.175` 整体 merge 或按版本号顺序 cherry-pick。最合理的路线是：

1. 先冻结并验证当前未提交的 `v1.2.34` 工作区，建立可复核基线。
2. 第一优先处理不需要数据库迁移的安全、账号池和资损止血项。
3. 第二优先修复 OpenAI/Codex/Grok 网关的错误分类、failover 和 usage 完整性。
4. 第三优先增加“上游响应模型只审计”能力，观察稳定后才评估按响应模型计费。
5. 再处理指纹、Codex instructions、Anthropic dateline 和 WebSocket 每轮风控等身份与风控融合。
6. Grok 先做稳定性和跨实例授权，再做 Voice、搜索、按模型族视频价等产品功能。
7. 运维护栏和数据库索引应先于 Channel Monitor V2 的大规模聚合。
8. Channel Monitor V2、备份分卷、邮箱主域名配额等作为独立产品版本，不阻塞安全和正确性追平。

建议的发布主线是：

```text
阶段 0  冻结 v1.2.34
  ↓
阶段 1  安全与资损止血（无迁移）
  ↓
阶段 2  网关可靠性与 usage 完整性（无迁移为主）
  ↓
阶段 3  计费/观测 Schema：先审计，不改变收费
  ↓
阶段 4  身份、指纹与风控融合
  ↓
阶段 5  Grok 稳定性专项
  ↓
阶段 6  运维护栏与数据库基础设施
  ↓
阶段 7  Channel Monitor V2 暗部署
  ↓
阶段 8  Grok 新产品能力和其他可选功能
```

每个阶段必须独立提交、独立构建、独立发布、独立回滚。阶段 1 的高风险项应进一步拆成单项提交，不得和 Grok、监控 V2 或大型前端改造混发。

## 2. 审计边界和版本事实

### 2.1 Pixel 不是只落后四个上游版本

- 当前 Pixel 与上游的共同祖先约为 `9d801595c95eb5f5616bca0ec409a42d73325987`，处于上游 `v0.1.121` 时期。
- Pixel 后来通过 `b2f5fb7e926813cbf1f1a9c486b2b6bfab62338c` 选择性吸收了 `v0.1.170/v0.1.171` 的 11 项修复，但没有把分叉历史整体合并回来。
- `v0.1.171..v0.1.175` 包含 258 个提交和 567 个变更文件。
- 这些文件中，Pixel 当前有同名路径约 292 个，完全缺少同名路径约 275 个；同名不代表实现仍等价。
- 核心网关、计费、设置、Grok、身份和数据库迁移已发生结构性分叉。

因此，“当前版本 1.2.34 比上游 0.1.175 数字更高或更低”没有可比意义；真正需要比较的是能力、行为合同和修复语义。

### 2.2 最新稳定版与上游 main

- 审计时 GitHub 最新稳定标签为 `v0.1.175`。
- `v0.1.175` 是 annotated tag，标签指向功能提交 `93c32fa1a`。
- 标签提交中的 `backend/cmd/server/VERSION` 仍为 `0.1.173`，随后 `ef4f99f29` 才同步为 `0.1.175`。
- 审计时上游 `main` 为 `5935e674a84341c3536e27e6a968384f67d9062b`。
- 标签后的两个提交仅同步版本号和更新赞助者资源，不含新的业务修复。

所以本计划以 `v0.1.175` 的功能提交作为稳定实施基线，不追逐 `main` 的非功能性尾部提交。

### 2.3 当前工作区状态

- 已提交版本文件为 `1.2.32`，当前工作区版本文件为 `1.2.34`。
- 工作区存在约 70 个已修改文件和 22 个未跟踪项。
- 主要涉及 OpenAI PAT、Agent Identity、Cyber Policy、账号创建/导入/重新授权、Grok 账号等级和风险控制管理端。
- 这些改动属于用户正在完成的功能，不能回退、覆盖或拿上游整文件替换。

阶段 0 必须先把当前行为形成稳定基线，否则后续任何上游移植都无法区分“新引入回归”和“既有未完成改动”。

## 3. 必须保留的 Pixel 合同

以下能力不允许在追平上游时退化：

- 账号广场、房间、席位、生命周期、结算和计费屏障。
- 用户私有账号、自有账号、专属分组、多分组 API Key 路由。
- OpenAI PAT、Owned Agent Identity、组织和账号归属约束。
- 发票管理。
- Cyber Policy、内容审核、实际路由分组和用户级隔离。
- Pixel 自研退款终态化和 gateway-first 退款语义。
- Pixel 自研图片 Token 分类、累加和计费语义。
- 凭据 snapshot CAS、凭据清洗、安全代理、集群协调、迁移验证和显式 `--migrate-only` 发布流程。
- Grok 与账号广场、owner binding、媒体资格、自定义上游地址和本地账号健康状态的集成。
- 当前生产验证过的 `codex_cli_rs` 默认身份策略。

选择性移植时，应以这些合同为边界手工实现上游最终语义，不以“上游文件更新”为理由覆盖 Pixel 实现。

## 4. 上游 v0.1.172～v0.1.175 增量总览

### 4.1 v0.1.172

主要安全、可靠性和观测改动：

- OAuth pending exchange 账号接管修复：`02e50cc22`。
- 上游实际响应模型审计：`db0bff82c`。
- TCP/TLS/SOCKS5 显式建连超时：`66ad405dd`。
- 金额写入 `NUMERIC(20,8)` 前量化：`e2652eb85`。
- 稀疏流量下 transient failure streak 不再错误归零：`7d38e6712`。
- Codex WebSocket 预热续链：`fc5a1b78d`。
- HTML `count_tokens` 403 不再冷却 OAuth：`e93f6b995`。
- 订阅日额度恢复每天零点重置：`99b357083`。
- 系统日志写库失败指数退避：`e687ca3e9`。
- Codex 工具 Schema 中 `parameters.type: null` 修复：`f3c94d209`。
- Grok 405 可 failover，但不做账号级处罚：`a071b27b4`、`146b8b668`。
- 图片模型误发 Codex 文本端点时不写模型冷却：`b5d9fd21b`、`02fbcbe3a`。
- 流内容量错误在输出前恢复 failover：`c33c3208e`。
- Responses 转 Anthropic 无效 content block 修复：`64090de66`。
- Antigravity Gemini 3.6 Flash：`ce1498313`。

### 4.2 v0.1.173

主要新增功能：

- Channel Monitor V2：基于真实请求的被动聚合，不再只依赖主动探测。
- Grok SSO、refresh token 重新授权、跨实例 OAuth Session。
- Grok 模型映射和跨客户端模型映射开关。
- Grok Free 档软门禁、team+model 冷却、7d/30d 阈值。
- Grok 图片、视频、Voice、搜索、custom voices。
- Grok 视频模型族×分辨率定价。
- 邮箱主域名限量注册。

重要修复：

- 非流式生图在客户端断开后仍完成结果处理和计费：`cbf2be05a`。
- Gemini pool 429 不再做账号级处罚：`cbc2a3dd4`。
- Gemini 原生生图按实际图片数计费：`b6eb6c1ef`。
- 上游响应模型观察热路径优化：`6e34fb09c`。

需要注意的行为变化：

- 发布说明与标签最终代码对 Grok 跨客户端映射默认值存在口径差异；`v0.1.175` 标签代码实际仍会把缺失值解析为开启。
- Grok 密码登录不是“无条件硬禁用”，而是默认关闭、操作员可开启。
- 上游迁移 `220` 会清理非 Grok/非 Composite 视频价格，属于数据修改，不是普通 Schema 变更。

### 4.3 v0.1.175

新增能力：

- Codex OAuth 设备指纹 `off/device/session/full` 四档收敛：`c0ab3a00e`。
- 按上游实际响应模型计费：`9096492b5`，后续安全加固 `b689e5b40`、`e5b325e48`。
- 大文件备份分卷上传与恢复：`bbc8b6e90`。

关键修复：

- HTML 403 不再批量处罚 OpenAI 账号：`12abb5470`。
- 空 `response.completed` 触发 failover：`280c1c862`。
- 确定性 400 不再转换成可重试 502：`591d47fb9`。
- Codex 容量错误保持指数退避：`74fcdf3d4`。
- OAuth 图片流错误 failover：`9763765eb`。
- API Key passthrough 清理非法 reasoning/item ID：`9f31df3fa`。
- OpenAI pool 认证失败先按预算重试：`7045f89de`。
- compact keepalive 已提交响应头但没有有效 SSE 时发送失败事件：`2f109e74c`。
- User-Agent 持久化前校验与存量污染自愈：`fe2c265c9`。
- Codex 调度阈值快照百分比、陈旧和重置判断：`99b31067f`、`3d3aee2e7`。
- service tier 进入账号成本：`9261dd773`。
- nested data usage 解析：`04dc540b2`、`a163742fc`。
- HTTP/WS TTFT 语义修复：`ab326c96e`、`900194fab`、`e24cb99b7`。
- WebSocket 每轮安全审计和同轮去重：`2d9920ba7`、`c418fd522`。
- Gemini `exclusiveMinimum` 规范化：`c8d9af6ce`。
- Cyber Policy 审计范围：`6564d376e`。
- API Key 数值和过期输入校验：`f5c108c83`。
- OpenAI 个人订阅到期时间修复：`358e4a89a`。
- Request ID 列恢复可见：`5350b3d98`。
- 未设置的调度阈值结果缓存：`3e1674a06`。
- 风控依赖失败时 fail-closed 改动 `e01c917a9` 最终被 `af6928a26` 回退，最终稳定行为仍是 fail-open。

## 5. Pixel 差距矩阵

状态定义：

- **缺失**：有上游和 Pixel 双向代码证据确认当前没有等价实现。
- **部分覆盖**：Pixel 有相关能力，但入口、边界或最终语义不完整。
- **已覆盖**：Pixel 已有等价或更强实现，不重复移植。
- **冲突**：上游默认行为会改变 Pixel 已冻结的产品合同。
- **待确认**：尚缺完整双证据或需要线上数据/产品选择，不在实现前直接定性。

### 5.1 安全、认证和账号健康

| 优先级 | 事项 | 上游 | Pixel 状态 | 证据和影响 | 推荐动作 |
|---|---|---|---|---|---|
| P0 | OAuth pending exchange 账号接管 | `02e50cc22` | 缺失 | Pixel `auth_oauth_pending_flow.go:1987-2062` 在不能发 token 的部分非终态仍可进入 adoption 并消费 session；上游在 `:2001-2015` 增加终态守卫 | 先补攻击复刻测试，再最小移植守卫；合法 `bind_current_user` 不受影响 |
| P0 | HTML 403 批量污染账号 | `12abb5470` | 缺失 | Pixel `ratelimit_service.go:1029-1069` 会累加计数、冷却并最终禁用；当前无 HTML 前置识别 | HTML 403 只参与当前请求 failover，不写账号健康；结构化 JSON 403 维持原策略 |
| P1 | WebSocket 后续轮次绕过入站审核 | `2d9920ba7`、`c418fd522` | 部分覆盖 | Pixel 首轮在 `openai_gateway_handler.go:1955-2021` 审核；`BeforeTurnPayload` 后续轮次未重新执行 Cyber Preflight、平台和用户审核 | 建立每轮一次的 Pixel 统一审核入口；同轮去重、跨轮重审 |
| P1 | UA 指纹污染 | `fe2c265c9` | 缺失 | Pixel `identity_service.go:23-25,78-130,389-442` 允许非锚定、高版本、本地开发 UA 持久化并长期压过正常版本 | 创建、升级、读取三处统一校验，并自愈存量污染 |
| P1 | API Key `NaN/Inf/负数` 和非正天数 | `f5c108c83` | 部分覆盖 | Pixel 已校验精确过期时间，但缺有限数、非负和 `expires_in_days > 0` 双层检查 | handler 和 service 双层校验，适配 Pixel 精确 `expires_at`、多分组路由和自定义 Key |
| P1 | 密文落库前未校验加密配置 | 旧上游批次 E | 缺失 | 错误配置下可能写入重启后无法恢复的凭据 | 在持久化入口 fail-fast；不做静默 fallback |
| P2 | 简单模式隐藏风险控制菜单 | `0d7b6ae64` | 缺失优化 | Pixel `AppSidebar.vue:1138-1143` 仍隐藏菜单，但路由没有同等限制 | 确认产品定义后仅调整可发现性，不复制上游整套 Security Audit |
| 策略 | 风控依赖故障 | `e01c917a9` → `af6928a26` | 已对齐 | Pixel `content_moderation.go:918-938,1131-1153` 等当前为 fail-open | 保持 fail-open，补错误率、健康状态和 SLO；如改 fail-closed 必须另做产品决策 |

### 5.2 OpenAI/Codex 网关正确性和协议

| 优先级 | 事项 | 上游 | Pixel 状态 | 影响 | 推荐动作 |
|---|---|---|---|---|---|
| P1 | 空 `response.completed` 记成功 | `280c1c862` | 缺失 | 返回空成功、记录 0/0 usage，且不换号 | 跟踪语义输出；只在客户端未收到有效输出时安全 failover |
| P1 | 确定性 400 被改写为 502 | `591d47fb9` | 缺失 | 客户端放大重试并丢失 `code/param` | 普通参数错误原样返回脱敏 400；容量型 400 保留 failover |
| P1 | nested usage 漏解析 | `04dc540b2`、`a163742fc` | 缺失 | `data.usage` 和 `data.response.usage` 记成 0，造成漏计费 | 固定优先级：`usage → response.usage → data.usage → data.response.usage` |
| P1 | Grok 成功内容但缺 usage | `ba92d7042` 等 | 缺失 | 客户端拿到结果但无法结算 | nested usage 修复后增加 usage integrity guard，避免误判包装 usage |
| P1 | SOCKS5 建连没有显式上限 | `66ad405dd` | 部分覆盖 | 直连/HTTP/TLS 已有约 10 秒上限，`proxyutil/dialer.go:48` 普通 SOCKS5 仍可能等待系统级重传 | 只移植 SOCKS5/context-aware dialer，不覆盖现有 Transport |
| P1 | 流内容量错误输出前 failover | `c33c3208e` | 待确认 | 若缺失会把可恢复降载直接暴露给客户端 | 实施阶段先做 focused audit；确认缺失后适配 Pixel first-output staging |
| P1 | 容量退避指数被压平 | `74fcdf3d4` | 缺失 | 持续撞击同一上游，放大 429/过载 | 保留请求级 `500ms→1s→2s→4s→8s` 级别退避，并响应取消 |
| P1 | WS v2 下行写绑定 `relayCtx` | 旧计划 C-8 | 缺失 | 租约/上游取消可冲掉已到达的终态 frame，用户裸 EOF 但系统可能已计费 | 下行写绑定客户端生命周期，仍保留写超时 |
| P1 | WS v2 终态伪造 TTFT | `ab326c96e`、`e24cb99b7` | 部分缺失 | `response.completed/done` 被当首 Token，污染延迟与调度数据 | 只改 WS v2，Pixel HTTP TTFT 保持现有实现 |
| P1 | Codex instructions 为占位符 | 旧计划 C-2 | 缺失 | `openai_codex_transform.go:113` 仍是简短通用提示词，和真实 Codex 客户端差异明显 | 使用按模型选择的内嵌官方基线，并设计可更新机制 |
| P1 | Anthropic dateline 隐写指纹 | 旧计划 C-1 | 缺失 | OAuth/setup-token 转发可能保留非官方 base URL 指纹 | 仅 OAuth/setup-token 规范化；API Key passthrough 不改 |
| P1 | Responses→Anthropic instructions/developer/tool pairing | `64090de66` 等 | 缺失或待复核 | content block 无效、instructions 丢失或工具配对异常 | 作为单独协议批次，使用转换前后 golden tests |
| P2 | API Key passthrough 非法 item ID | `9f31df3fa` | 部分缺失 | 续链或 reasoning 请求被上游拒绝 | 按 `msg/rs/fc` 前缀删除非法 ID，不伪造新 ID |
| P2 | 工具 Schema `parameters.type:null` | `f3c94d209` | 待确认 | 上游拒绝工具 Schema | focused audit 后适配现有 schema sanitizer |
| P2 | Gemini `exclusiveMinimum` | `c8d9af6ce` | 缺失 | Gemini 工具声明可能被拒或错误放宽约束 | 整数安全转换为更严格 `minimum`；小数/溢出时移除不支持字段 |
| P2 | Chat `reasoning` 别名 | `8aa425d22` | 缺失 | 兼容上游只返回 `reasoning` 时推理内容丢失 | `reasoning_content` 优先，缺失时兼容 `reasoning` |
| 已覆盖 | SSE `response.failed` 语义状态 | `85a27fae3` 等 | 已覆盖 | Pixel `openai_gateway_response_failed.go:56` 已区分 400/401/403/429/503 | 不重复移植，只保留回归 |
| 已覆盖 | OAuth 图片流输出前/后 failover | `9763765eb` | 已覆盖/更强 | Pixel 已区分 keepalive、真实输出和计费边界 | 不覆盖本地实现，只补同类上游测试 |
| 已覆盖 | `UpdateLastUsed` 并发覆盖 | 旧计划 D-2 | 已覆盖 | `account_repo.go:3452` 只更新 `last_used_at` | 不再列入实现 |

以下上游修复有价值，但在本次静态审计中尚未完成双证据定级：低流量 transient streak、OpenAI pool 认证失败重试、compact keepalive 最终失败事件、Codex WS prewarm、HTML `count_tokens` 403、图片模型端点冷却门控。它们应作为阶段 2 的开工前 focused audit，不应仅依据提交标题直接判定 Pixel 缺失。

### 5.3 计费、成本和可观测性

| 优先级 | 事项 | 上游 | Pixel 状态 | 影响 | 推荐动作 |
|---|---|---|---|---|---|
| P0 | 金额未量化到 `NUMERIC(20,8)` | `e2652eb85` | 缺失 | 浮点尾差可能导致写库失败、账单和余额细微漂移 | 在统一 billing boundary 量化，不在各调用点重复 round |
| P1 | 上游响应模型审计 | `db0bff82c`、`6e34fb09c` | 整体缺失 | 无法发现供应商静默换模、降级或冒充 | 先新增字段、索引、只写入和只读展示，不改变计费 |
| P1 | `upstream_model` 比较基准 | 旧计划 C-11 | 可疑/待修 | `gateway_service.go:10498`、`openai_gateway_service.go:7878` 仍比较 `result.UpstreamModel` 和 `result.Model`，可能丢失渠道映射后的实际上游模型 | 与响应模型审计一起定义 requested/sent/responded 三种模型 |
| P1 | service tier 未进入账号成本 | `9261dd773` | 缺失 | 客户收费可能正确，但账号成本、毛利和利润控制仍按 standard | 成本计算明确传入 `ServiceTier`，覆盖 standard/priority/flex |
| P1 | TTFT 样本计数 | 上游迁移 145 等 | 部分缺失 | 非流式和无输出样本稀释分位数，WS 终态误记首 Token | 新增 sample count，先修语义再建指标 |
| 高风险产品项 | 按响应模型计费 | `9096492b5`、`b689e5b40`、`e5b325e48` | 缺失 | 直接开启可能被上游声明操纵、把收费降成 0、绕过渠道价或破坏媒体单位计费 | 审计观察稳定后再以默认关闭开关灰度 |
| 已覆盖 | 图片 Token 分类和累加 | 旧上游图片修复 | Pixel 主动偏离且更适合本地合同 | 照搬上游赋值语义会重引入错分或漏计 | 保留本地累加语义 |
| 已覆盖 | Anthropic 流中断保留 usage | 上游相关修复 | 已覆盖 | Pixel 有 `BillableStreamUsageError` | 不重复移植 |

按响应模型计费必须同时满足：

1. 响应模型非空，且同一请求没有观察到多个冲突模型。
2. 响应模型存在确定性价格。
3. 不能因为上游声明而把原本收费的请求降成 0。
4. 不能绕过渠道 `requested/upstream/channel_mapped` 的价格合同。
5. 响应模型价格不得高于当前可证明的计费基线；不确定时继续使用原基线。
6. 图片、视频、音频、搜索等按数量、时长或请求次数计费的场景不得被文本响应模型覆盖。
7. 管理端开关默认关闭，关闭后写入审计字段但完全不改变账单。

### 5.4 身份、指纹和风控策略

| 事项 | Pixel 状态 | 判断 | 推荐 |
|---|---|---|---|
| Codex 四档指纹收敛 | 部分实现但不等价 | Pixel 有默认关闭的 Clean Relay；它还承担粘性、prompt cache 和 previous response 清理。直接叠加上游会二次改写同一 carrier | 先抽象唯一权威的出站指纹策略；Clean Relay 和账号级模式共享 writer |
| 上游默认 `session` | 冲突 | 会改变所有现有 OAuth 账号行为 | 新能力默认 `off` 或维持当前 Clean Relay 默认；启用必须显式确认并灰度 |
| 默认身份改为 `codex-tui` | 主动不跟 | Pixel 测试和生产观察支持 `codex_cli_rs`，上游策略可能重新进入降载桶 | 只吸收 UA/version 同源格式加固，保留 Pixel 默认 originator |
| Cyber Policy group scope | Pixel 自定义实现已覆盖核心问题 | 当前按实际路由 attempt、用户和有效分组隔离，比请求入口 group 更精确 | 保留现状，不用上游整套 securityaudit 覆盖 |
| Cyber Policy model scope | 产品待确认 | 当前没有独立 model scope；直接复用 Content Moderation model filter 会耦合两个策略 | 如确有业务需求，另建独立 Cyber model filter |
| 风控 fail-open/fail-closed | 已与上游最终版对齐 | 当前 fail-open 是可用性优先策略，不是遗漏 | 保持行为，增加健康告警；切换策略需单独批准 |

### 5.5 Grok 稳定性和新增能力

Grok 详细原子任务继续以 `docs/grok-upstream-parity-optimization-plan.md` 为专项子计划，本总计划只规定跨域顺序和上游新增范围。

#### 已有或 Pixel 自定义更强

- 基础 OAuth/PKCE。
- SSO Cookie 转 OAuth 的管理端导入流程。
- 图片/视频生成、状态和内容查询。
- 媒体 eligibility、owner 隔离、安全内容代理和本地计费元数据。
- 旧的 480p/720p/1080p 视频价格。
- `web_search_price_per_call`，但它和上游 `/v1/web_search` 每千次价格不是同一语义。
- OAuth refresh candidate 索引。
- 非 pool 账号 402 进入永久 `error`；这是 Pixel 主动合同，不跟上游短冷却。

#### 已确认稳定性缺口

| 顺序 | 事项 | 状态和风险 | 推荐 |
|---|---|---|---|
| G1 | 最小健康探针 body | 现有 `max_output_tokens` 可能把健康推理模型误判为 incomplete | 手工测试和 quota 探针共用 builder |
| G2 | SSE `event: ping` 过滤 | 严格 Responses 客户端可能因未知事件中止 | 最终版状态机放在所有 Grok 文本转换器之前 |
| G3 | Chat/Messages transport failover | 部分链路 transport error 没有统一换号 | 统一错误分类和 first-output 边界 |
| G4 | `pool_mode` 健康状态旁路 | 默认 401/402/403/429/5xx 可能污染聚合池账号健康 | 显式管理员规则之后统一旁路本地 mutation，不吞错误 |
| G5 | durable 429 | live 请求未完整复用持久化 quota window | 只能延长不能缩短；旧成功不能清除新 429；使用 CAS/观测代际 |
| G6 | CLI 兼容性 403 回放 | 过宽回放会绕过 entitlement 或内容策略 | 仅精确 host、OAuth、CLI 头、受控文案、可重放 body 时尝试一次 |
| G7 | 媒体 URL 和模型映射 | `reference_images`、别名、上游模型和计费模型可能不一致 | 先规范字段，再统一路由和计费模型 |
| G8 | 视频 owner binding 时序 | 当前成功响应可能先于 binding | prepare → owner/routing binding → commit；失败不得返回不可查询 ID |
| G9 | 跨实例 OAuth Session | Pixel SessionStore 仍是进程内 | 使用 Redis 原子一次性消费，先于新增登录方式 |
| G10 | stream idle、team+model、model quota | 缺失或不完整 | 在核心错误状态机稳定后逐项增加 |

#### 上游新增但属于产品选择

- Free 档 24h 软门禁。
- refresh token/重新授权闭环。
- observed models。
- spending reauth。
- Voice TTS/STT/Realtime/custom voices。
- `/v1/web_search`。
- 按模型族×分辨率视频价格。
- 管理端真实媒体预览。
- 密码登录。

推荐保持：

- 跨客户端模型映射默认关闭。上游标签最终代码的默认开启语义不适合 Pixel。
- 密码登录本轮不引入。若未来启用，需单独评估上游账号密码的安全、审计和合规责任。
- `/v1/web_search` 使用独立 `search_price_per_1k`，不能复用 Pixel 现有 `web_search_price_per_call`。

### 5.6 Channel Monitor V2、注册和备份

| 功能 | Pixel 状态 | 价值 | 风险 | 优先级 |
|---|---|---|---|---|
| Channel Monitor V2 | 完整缺失 | 用真实流量被动聚合成功率、TTFT、吞吐和缓存率 | 大表回填、用户维度隐私、初期无水位、权限和聚合成本 | 后置独立版本 |
| 邮箱主域名限量注册 | 缺失 | 限制同主域名批量刷注册权益 | 必须事务锁+创建时复查；只 `COUNT→INSERT` 有竞态 | 可选，默认关闭 |
| 大文件备份分卷 | 完整缺失 | 解决超大备份上传、下载和恢复限制 | 新旧格式兼容、卷缺失/顺序/校验、清理一致性 | 独立版本 |
| Composite 图片权限 UI | 部分实现 | 补齐管理端能力 | 只加开关可能形成 UI 假授权 | 先验证网关门禁，再开放 UI |
| Request ID 列 | 缺失优化 | 排障价值高 | 低 | 可在低风险 UI 批次吸收 |
| `nanoid 3.3.17` | 缺失依赖修复 | 修复 `GHSA-2v37-7h3g-55p8` | 锁文件和前端回归 | 阶段 1 |

## 6. 重点风险修复前后对比

| 风险 | 修复前状况 | 当前影响 | 修复后目标 | 新风险或代价 |
|---|---|---|---|---|
| OAuth pending adoption | 非终态 session 仍可能执行 adoption | 攻击者身份可能绑定到受害者账号，形成接管 | 只有可发 token 的终态或合法 `bind_current_user` 能绑定 | 必须防止误伤正常邀请/绑定流程 |
| HTML 403 | 代理/CDN HTML 403 计入账号连续失败 | 健康账号被逐个冷却或禁用，分组被抽空 | HTML 403 仍 failover，但不修改账号健康 | HTML 判断必须窄，不能放过真实结构化鉴权错误 |
| WS 后续轮次审核 | 只审核建连首轮 | 第二轮起可绕过平台、Cyber 和用户审核 | 每个 turn dispatch 前审核一次 | 审核延迟增加；需同轮去重，不能跨轮缓存 |
| 金额精度 | 浮点金额直接落 `NUMERIC(20,8)` | 写库失败或余额/账单尾差 | 统一边界量化 | 需冻结舍入规则，避免各模块不同 |
| 空 completed | 无输出、无 usage、无 error 也记成功 | 空响应、0/0 日志、不换号 | 输出前识别为空并安全换号 | 客户端已收到数据后禁止重放，避免重复生成 |
| 确定性 400 | 普通参数错误转为 502 | 客户端和网关重复重试，放大负载 | 原样 400，容量类错误继续 failover | 错误分类必须白名单，避免误把瞬态错误定死 |
| nested usage | 包装 usage 被当成 0 | 漏计费、成本和额度失真 | 四条路径按固定优先级解析 | 冲突路径必须可观测，不能累加重复 usage |
| 响应模型计费 | 若直接启用，信任上游声明 | 上游可操纵模型名、升价/降零或绕过媒体规则 | 先审计；满足安全准入时才允许替换模型 | 增加 Schema、查询成本和产品复杂度 |
| Codex 指纹 | Clean Relay 与四档模式职责重叠 | 双重改写、会话失配、PAT/Agent Identity 回归 | 唯一策略和 carrier writer | 需要兼容迁移和灰度开关 |
| Cyber fail-open | 依赖故障时审核短时失效 | 安全能力下降但站点可用 | 保持可用性并告警 | 若改 fail-closed 会把依赖故障放大全站故障 |
| Grok 429 | 临时状态缺持久化和代际保护 | 重启/多实例后窗口失真，旧成功清掉新限流 | durable window 只能延长，CAS 清理 | Redis/持久层写入失败必须可观测 |
| Grok owner binding | 成功 ID 先返回，绑定后写 | 用户拿到无法查询的任务 ID | binding 成功后才 commit 响应 | binding 失败会把上游已创建任务记为 orphan，需运维事件 |
| 迁移编号 | 复制上游 `194～220` | 旧编号在 Pixel `268` 后加入，迁移器仍会执行，历史语义混乱 | 全部按 Pixel 当前最大编号后重排 | 每次开工前都要重新确认最大编号 |
| 上游迁移 220 | 自动清理非 Grok 视频价格 | 现有分组配置被改写 | 独立统计、备份、审批、执行和回滚 | 需要明确数据授权，不能随普通发布自动运行 |

## 7. 分阶段实施计划

### 阶段 0：冻结当前 v1.2.34 工作区

目标：得到后续所有上游移植的可信对照组。

原子任务：

1. 记录当前分支、HEAD、`git status`、版本文件和所有未跟踪项。
2. 完成 PAT、Agent Identity、Cyber Policy、Grok 账号等级等正在开发能力的行为收口。
3. 固定以下合同测试：
   - PAT、OAuth、API Key、Agent Identity 的鉴权与出站身份。
   - Cyber Policy 的 user + actual route group + attempt 隔离。
   - 多分组 API Key、专属分组、私有账号和账号广场路由。
   - 图片 Token 分类、退款、发票和 owner binding。
4. 运行：
   - 后端完整单元测试，使用项目要求的构建标签。
   - 前端完整测试、类型检查和生产构建。
   - 迁移 checksum/through 验证，但不连接或修改生产数据库。
5. 只有基线全绿后才进入阶段 1。

发布要求：

- 当前工作区不被上游整文件覆盖。
- 基线失败必须先归因并修复，不把既有失败带入上游升级。

### 阶段 1：安全与资损止血

目标：以最小代码面消除账号接管、账号池污染和确定性资损。

推荐严格顺序：

1. OAuth pending exchange 非终态 adoption 守卫。
2. API Key 有限数、非负、过期参数双层校验。
3. UA 合法性验证和存量污染自愈。
4. HTML 403 不写账号健康。
5. 金额统一量化到 `NUMERIC(20,8)`。
6. 确定性 400 原样透传，容量类错误继续 failover。
7. WebSocket 每轮执行 Pixel 三层入站审核。
8. `nanoid` 安全版本升级。

提交纪律：

- 每项独立小提交，至少有一条修复前失败、修复后通过的回归测试。
- OAuth、HTML 403、WebSocket 审核不得合并为同一提交。
- 不含数据库迁移。

阶段门槛：

- pending session 在所有非终态都“不绑定、不消费、可继续验证”。
- 连续 HTML 403 不增加账号 403 计数，不写 cooldown/error。
- 结构化鉴权 403 仍按当前合同处理。
- `NaN/Inf/负数/0 天` 在 handler 和 service 都失败。
- WebSocket 第一轮、第二轮和后续轮分别审核；同一轮不重复调用。

建议发布后观察 24 小时：

- OAuth pending/adoption 错误率。
- OpenAI 403 的 HTML/JSON 分类、账号 temp/error mutation。
- WS 审核 blocked/unavailable/latency。
- billing quantize 前后舍入差值和写库错误。
- 400/502 比例与客户端重试量。

### 阶段 2：网关可靠性和 usage 完整性

目标：保证错误被正确分类、可恢复请求正确换号、已交付结果能够完整结算。

推荐顺序：

1. nested usage 四路径解析和冲突优先级。
2. Grok missing usage integrity guard。
3. 空 `response.completed` 输出前 failover。
4. SOCKS5 显式建连超时和 context 取消。
5. 容量流错误 first-output failover。
6. 容量指数退避。
7. WS v2 下行写生命周期。
8. WS v2 TTFT 终态修复。
9. API Key item ID、`parameters.type:null`、Gemini `exclusiveMinimum`。
10. Responses→Anthropic instructions/developer/tool pairing。
11. focused audit 后再决定是否吸收 pool auth retry、compact keepalive、prewarm、低流量 streak 等项。

依赖关系：

- nested usage 必须早于 Grok missing usage guard，否则合法包装 usage 会被误判。
- first-output failover 必须早于空 completed 和容量回放的扩大覆盖。
- WS TTFT 应在 WS 下行生命周期稳定后修改。

最低测试矩阵：

| 维度 | 场景 |
|---|---|
| usage | `usage`、`response.usage`、`data.usage`、`data.response.usage`、多路径冲突 |
| completed | 空终态、带 output、带 usage、带 error、客户端已提交输出 |
| 错误 | 400、401、HTML/JSON 403、405、429、500/502/503/504、流内 error |
| 网络 | 直连、HTTP 代理、SOCKS5、TLS、context cancel、连接超时 |
| WS | delta、done-only、terminal-only、租约丢失、客户端关闭、写超时 |
| 协议 | Responses、Chat、Messages、compact、API Key passthrough、OAuth |
| 自研回归 | PAT、Agent Identity、Cyber Policy、图片输出后不换号、账号广场 |

### 阶段 3：计费与可观测 Schema

目标：先获得真实数据，再讨论收费策略。

步骤：

1. 新增 `usage_logs.upstream_response_model` 和 `upstream_model_mismatch`。
2. 以非事务并发部分索引支持 mismatch 查询。
3. Ent schema、生成代码、repository、DTO 和前端类型同步。
4. 所有 HTTP/SSE/WS 路径只写入观察值。
5. 管理端展示 requested/sent/responded 三种模型和 mismatch 筛选。
6. 修复当前 `upstream_model` 比较基准。
7. service tier 进入账号成本。
8. 修复 TTFT 样本计数和报表口径。
9. 暂不增加或不启用 `response_model` 计费来源。

观察期建议至少覆盖一个完整业务周期，并包含：

- 各渠道 mismatch 比例。
- 同一请求多个响应模型的冲突率。
- 响应模型无价格率。
- 响应模型价格高于/低于当前基线的比例。
- 图片、视频、音频、搜索等非纯文本请求的模型声明。
- Schema 新列写入延迟、索引体积和查询计划。

只有观察数据证明安全准入可实施，才把“按响应模型计费”放入阶段 8 的可选产品项。

### 阶段 4：身份、指纹与风控融合

目标：消除可识别指纹，同时不破坏 Pixel 的账号粘性、PAT、Agent Identity 和现有生产身份策略。

顺序：

1. Codex instructions 按模型替换占位符。
2. Anthropic OAuth/setup-token dateline 规范化。
3. 设计唯一的 `CodexOutboundFingerprintPolicy`。
4. 合并 Clean Relay 和四档指纹的 carrier 写入逻辑。
5. 明确 HTTP Responses、WS、compact、prompt cache、previous response 的优先级。
6. 保留 `codex_cli_rs` 默认 originator。
7. Cyber Policy 保持 actual attempt/group/user 隔离。
8. 保持 fail-open，但增加依赖健康、错误率和持续时间告警。

本阶段需要用户确认的策略：

- 新账号的指纹模式默认 `off` 还是 `session`。本计划建议 `off`，先对专用账号灰度。
- 是否允许管理员对单个账号开启 `device/session/full`。
- 是否需要独立 Cyber Policy model scope。

回归范围必须覆盖 OAuth、PAT、Agent Identity、API Key、HTTP、WS、compact、Clean Relay 开/关和所有四档模式。

### 阶段 5：Grok 稳定性专项

目标：先让现有 Grok 文本和媒体链路稳定、可计费、可恢复，再扩展产品能力。

推荐顺序：

1. 最小探针 body。
2. SSE ping 过滤并接入所有文本消费者。
3. Chat/Messages transport failover。
4. `pool_mode` 默认健康 mutation 旁路。
5. live 429 接入 durable quota state。
6. CLI 特定 403 的窄匹配安全回放。
7. 跨实例 Redis OAuth Session 和一次性消费。
8. refresh token/重新授权闭环。
9. 媒体 URL、`reference_images`、模型映射和计费模型统一。
10. 视频 prepare/bind/commit。
11. stream idle、team+model、model quota、Free 24h gate。

冻结合同：

- pool 旁路的是本地健康 mutation，不是错误、failover 或显式管理员规则。
- 非 pool 402 继续永久 `error`。
- 内容策略 403 不处罚账号。
- 旧成功不能清除新 429。
- 视频返回 2xx 时，request ID 必须已经能够按同 owner 查询。

每个 Sprint 独立发布，先 30 分钟 canary，再观察 24 小时账号状态变化。

### 阶段 6：运维护栏与数据库基础设施

目标：在引入 Channel Monitor V2 聚合和更多 Schema 前先保护 DB、内存和调度热路径。

先做无迁移项：

- 无效鉴权爆破限流和鉴权回源并发上限。
- Ops 高级设置热路径缓存。
- Ops 错误日志队列字节级内存预算。
- 密文落库前加密配置校验。
- non-transactional unique index 的 invalid 自愈通用化。
- 系统日志写库失败退避。

再做独立迁移项：

- 入口拒绝聚合表。
- 管理员 append-only audit logs。
- TTFT sample count。
- `account_groups` 调度复合索引。
- 注册邮箱 alias 并发安全唯一索引。

明确不做：

- 当前生产仍是单实例时，不引入上游 auth cache invalidation outbox。
- 继续复用 Pixel `ClusterCacheCoordinator`；生产切换多实例时重新评估跨实例失效。

### 阶段 7：Channel Monitor V2 暗部署

目标：在不影响现有 V1 的情况下引入被动监控。

步骤：

1. 先部署重新编号后的空表和配置迁移，保持 `channel_monitor_mode=v1`。
2. 后端只启用受限时间窗聚合，不开放用户路由。
3. 核验大表查询计划、索引命中、每批扫描行数、锁等待和写放大。
4. 管理端内部只读展示。
5. 等水位、覆盖率和准确性达到门槛后，灰度用户端 V2。
6. 最后才评估是否停止主动 probe。

隐私默认：

- 默认隐藏吞吐量。
- 用户查询必须证明 user/channel 隔离。
- ignored error categories 和缓存阈值使用安全默认。
- 不对历史全表一次性回填；按小窗口、可暂停、可恢复方式进行。

### 阶段 8：可选产品能力

这些能力不应阻塞前七个阶段：

1. 按上游响应模型计费，默认关闭，观察数据通过后灰度。
2. Grok Voice TTS/STT/Realtime/custom voices。
3. Grok `/v1/web_search` 和每千次价格。
4. Grok 按模型族×分辨率视频价格。
5. 大文件备份分卷。
6. 邮箱主域名限量注册，默认关闭。
7. Composite 图片权限 UI。
8. Request ID 列、简单模式风险控制入口等低风险 UI。
9. Antigravity Gemini 3.6 Flash。

不建议本轮引入：

- Grok 密码登录。
- 默认开启 Grok 跨客户端暗默模型映射。
- 默认切换 `codex-tui`。
- 未经数据审计直接清理非 Grok 视频价格。

## 8. 数据库迁移专项

### 8.1 为什么不能复制上游编号

Pixel 当前迁移已经到：

- `267_add_invoice_remarks.sql`
- `268_drop_invoice_legacy_delivery_fields.sql`

而上游新增使用 `194～220`。Pixel 的同编号已经被完全不同的功能占用，例如：

- Pixel 194：账号广场队列。
- Pixel 195：账号广场评价。
- Pixel 196：发票。
- Pixel 217：OpenAI Owned Agent Identity 唯一索引。
- Pixel 218：集群运行态。
- Pixel 219/220：账号广场全局邀请策略。

`backend/internal/repository/migrations_runner.go:952-967` 对完整文件名执行 `sort.Strings(files)`，并按完整 filename 查询 `schema_migrations`。因此：

- `194_account_share_mode_queue.sql` 和 `194_channel_monitor_v2.sql` 会被视为两个不同迁移。
- 即使生产已经执行到 Pixel 268，后来加入的上游 194 仍会被判定为未执行。
- 低编号文件会插入已经稳定的历史序列，破坏 through 边界、发布追踪和人工判断。

所有上游迁移必须重新编号，不能保留原编号。

### 8.2 推荐编号台账

以下编号是在“当前最大编号仍为 268”的前提下给出的顺序保留。实施前必须再次读取迁移目录和生产 `schema_migrations` 完整文件名集合；若有新迁移占号，整体顺延，不修改已发布迁移。

| Pixel 建议编号 | 来源/用途 | 执行特性 |
|---|---|---|
| 269 | upstream response model 两列 | 普通事务迁移 |
| 270 | mismatch 部分索引 | `_notx.sql`，并发建索引 |
| 271 | ingress reject aggregates | 普通迁移，阶段 6 |
| 272 | audit logs | 普通迁移，阶段 6 |
| 273 | TTFT sample count | 先评估大表影响 |
| 274 | `account_groups` 调度索引 | `_notx.sql` |
| 275 | users email alias dedup 索引 | `_notx.sql`，需 invalid 自愈 |
| 276～288 | Channel Monitor V2 原上游 194、195～206，严格保持内部依赖顺序 | 阶段 7，默认 V1 |
| 289 | Grok `video_model_prices` | 阶段 8 |
| 290 | Grok 音频价格字段 | 阶段 8 |
| 291 | Grok `search_price_per_1k` | 阶段 8 |
| 292/293 | 可选：非 Grok 视频价快照和清理 | 不进入默认批次，单独授权 |

Channel Monitor V2 的上游 13 个迁移按以下顺序映射到 276～288：

1. `194_channel_monitor_v2.sql`
2. `195_channel_monitor_mode.sql`
3. `196_channel_monitor_v2_ignored_error_categories.sql`
4. `197_channel_monitor_v2_seed_popular_models.sql`
5. `198_channel_monitor_v2_health_thresholds.sql`
6. `199_channel_monitor_v2_fixed_rollups.sql`
7. `200_channel_monitor_v2_rollup_permissions.sql`
8. `201_channel_monitor_v2_refresh_5m.sql`
9. `202_channel_monitor_v2_full_table_permissions.sql`
10. `203_channel_monitor_v2_default_ignore_and_cache.sql`
11. `204_channel_monitor_hide_throughput.sql`
12. `205_channel_monitor_v2_reset_factory_cache_thresholds.sql`
13. `206_channel_monitor_v2_privacy_defaults.sql`

### 8.3 上游迁移 220 的数据风险

上游 `220_clear_non_grok_video_generation_config.sql` 会：

- 创建备份快照表。
- 对 `platform != grok` 且 `platform != composite` 的分组清空：
  - `video_price_480p`
  - `video_price_720p`
  - `video_price_1080p`
  - `video_model_prices`

这属于数据库数据修改。实施时必须在任何查询或执行前向用户说明：

1. 为什么需要清理。
2. 预计影响哪些平台和分组。
3. 只读统计和备份方案。
4. 新旧代码对这些字段的读写差异。
5. 回滚 SQL 和快照保留期。

只有用户明确批准后，才能连接数据库做影响统计或执行清理。本分析阶段没有进行任何数据库查询或修改。

### 8.4 迁移发布门槛

1. 不修改任何已应用 Pixel 迁移内容和 checksum。
2. `_notx.sql` 继续通过 pinned connection 非事务执行。
3. 对 `usage_logs`、`ops_error_logs` 等大表先做规模和锁风险评估。
4. `DATABASE_MIGRATION_THROUGH` 使用新完整文件名，而不是只写数字。
5. 新二进制切换前显式运行 `--migrate-only`。
6. Schema、Ent、生成代码、repository、DTO 和前端类型必须同一阶段一致。
7. 先验证旧二进制对新增 nullable 字段兼容，再切换 current symlink。
8. 数据清理类迁移和普通 additive Schema 迁移永不混发。

## 9. 发布、观察和回滚

### 9.1 每个阶段的固定门

发布前：

- 工作区基线可复核。
- 定向测试和全量测试通过。
- 前端类型检查和生产构建通过。
- `git diff --check` 通过。
- 涉及迁移时，迁移校验、through 边界和回滚方案通过。
- 没有将密钥、真实配置、备份或数据库导出加入版本控制。

Canary：

- 使用专用测试 Key 和账号。
- 首先观察 30 分钟错误状态、账号 mutation、计费和延迟。
- 再观察至少 24 小时后才进入下一阶段。
- 计费策略、账号健康策略、Grok 状态机和 Channel Monitor V2 应使用更长观察窗口。

### 9.2 核心指标

- OAuth pending/adoption、session consume、账号绑定变化。
- 403 HTML/JSON 分类和账号 `error/temp/rate-limit` mutation。
- 400/502、429/503、first-output failover、空 completed。
- usage 缺失、路径来源、冲突、账单失败和 0 成本异常。
- requested/sent/responded model mismatch。
- standard/priority/flex 的用户收费、账号成本和毛利。
- HTTP/WS TTFT、terminal-only 和 no-delta 样本。
- WS 每轮审核调用、blocked、unavailable、latency。
- Grok pool mutation、durable 429、CAS miss、owner binding 和 orphan task。
- Channel Monitor 聚合延迟、扫描行、锁等待、缓存和水位。

### 9.3 回滚原则

- 代码与数据库 additive 变更分开：优先回滚代码开关，不删除新增 nullable 列。
- 行为开关默认关闭，确保老行为可恢复。
- 账号状态写入无法随代码回滚自动撤销；发布前必须证明错误 mutation 为 0。
- 已交付媒体任务不能通过重试生成重复任务；owner binding 失败进入明确 orphan 事件。
- 按响应模型计费关闭后立即回到原有计费来源，但保留审计数据。
- Channel Monitor V2 回滚为 `mode=v1`，停止聚合 worker，不立即删表。
- 数据清理迁移只能依赖预先批准的快照做恢复。

## 10. 明确不采用的升级方式

- 不整体 merge `upstream/main` 或 `v0.1.175`。
- 不批量 cherry-pick 258 个提交。
- 不拿上游大文件覆盖 Pixel 的 `openai_gateway_service.go`、`ratelimit_service.go`、设置、Grok 或当前 Cyber Policy 工作区文件。
- 不原样复制上游 `194～220` 迁移编号。
- 不把响应模型审计和响应模型计费一次上线。
- 不把 Channel Monitor V2 和 Grok Voice/Search 同一版本发布。
- 不在未观察数据前默认开启 Grok 跨客户端模型映射。
- 不直接切换 `codex-tui`。
- 不叠加 Clean Relay 和上游四档指纹两套独立重写器。
- 不自动执行上游 `220` 数据清理。
- 不因上游曾短暂采用 fail-closed 就静默改变 Pixel 的风控可用性策略。
- 不引入上游 auth cache outbox，除非生产部署拓扑已经变成多实例并证明现有协调机制不足。

## 11. 需要用户在实施前确认的策略

以下选择不会阻塞阶段 0～3，但会阻塞相应后续功能：

1. Codex 指纹新默认：建议 `off`，专用账号灰度 `session`。
2. Cyber Policy 是否需要独立 model scope：建议暂不增加。
3. Grok 跨客户端映射：建议默认 `false`。
4. Grok 密码登录：建议本轮不引入。
5. Channel Monitor V2：建议初始保持 `mode=v1` 并暗部署。
6. Composite 是否继续扩展图片生成：需先确认目标平台和权限继承。
7. 非 Grok 视频价格是否清理：建议默认不清，必须先只读影响报告。
8. 响应模型计费是否最终开放：只能在观察期结束后决定。

## 12. 完成定义

本轮“追平 v0.1.175”不以代码行或提交数量为完成标准，而以以下结果为准：

- P0/P1 已确认安全和资损项全部有攻击/故障复刻测试。
- OpenAI/Codex/Grok 的错误分类、first-output、usage 和账号健康状态合同一致。
- Pixel 自研 PAT、Agent Identity、Cyber Policy、账号广场、退款、图片计费和 owner binding 无回归。
- requested/sent/responded model 可观测，但默认计费不受上游声明控制。
- 所有新迁移使用 Pixel 单调编号，且 through、checksum、notx 和回滚可验证。
- Channel Monitor V2 和 Grok 产品功能以独立开关、独立版本、独立观察窗口发布。
- 所有主动偏离均有测试和注释保护，下一次上游同步不会被误判为遗漏。

历史计划的关系：

- `docs/UPSTREAM_GAP_AND_CATCHUP_PLAN.md` 保留为 v0.1.169 左右的历史审计和已完成批次记录。
- `docs/grok-upstream-parity-optimization-plan.md` 继续作为 Grok 稳定性原子实施子计划。
- 本文是面向 `v0.1.175` 和当前 `v1.2.34` 工作区的总排序与跨域依赖依据。
