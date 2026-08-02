# 上游差距盘点与追平计划（2026-08-02）

> 基线：本地 `codex/pixel-ui` @ `996a24979`（1.2.30）
> 上游：`Wei-Shaw/sub2api` @ `upstream/main` = `b74024c78`（v0.1.169 + 52）
> 上次同步点：**v0.1.121**（`9d801595c`，2026-04-30）

---

## 一、差距总账

| 指标 | 数值 |
|---|---|
| 落后上游版本数 | **48 个发布**（v0.1.122 → v0.1.169+） |
| 落后提交数 | **1402 个非 merge 提交**（fix 829 / feat 232 / test 64 / refactor 26 / perf 5 / chore 94 / docs 19） |
| 上游新增、本地无同名文件的后端 Go 文件 | 210 个 |
| 上游新增迁移（本地全无） | 58 个（上游 134 → 191） |
| 本地自研改动量（相对 v0.1.121） | 后端 1208 文件 / 前端 369 文件 / 105 个提交 |
| 历史上主动移植过的上游补丁 | **仅 1 处**（v0.1.169 URL 路径穿越护栏，随 1.2.29 上线） |

### 审计判定分布（274 条判定，8 个领域并行盘点 + 逐域对抗复核）

| 判定 | 条数 | 含义 |
|---|---|---|
| `missing` | 204 | 本地确实没有，且对本地有意义 |
| `superseded` | 21 | 本地自研方案已覆盖同一问题域，**不跟** |
| `ported` | 26 | 已有等价实现（含误报纠正） |
| `conflicting` | 9 | 上游改动直接踩本地魔改，照搬会坏 |
| `na` | 14 | 上游特有，本地用不上 |

| 优先级 | 条数 | 冲突风险 | 条数 |
|---|---|---|---|
| P0 | 16（去重后 **13** 件） | high | 37 |
| P1 | 51（去重后 ~30 件） | medium | 73 |
| P2 | 90 | low | 164 |
| P3 | 117 | | |

跨域重复较多（同一问题被多个领域各自发现），例如 usage_log 丢弃被网关/计费/运维三域同时命中、`audit_logs` 被四域命中。下文批次按**去重后的独立事项**编排。

### 结构性障碍

1. **迁移编号从 134 号起撞车。** 本地 134–262 全是 Pixel 自研（账号广场/席位计费/商城/积分/返利/发票/工单/活动/子站），上游 134–191 是完全不同的内容。**上游迁移一条都不能照编号搬**，全部要重编到本地 263+，且必须逐条判断本地是否已用别的方式实现了等价 schema。
2. **两个核心文件已无法合并。** 上游把 `gateway_service.go`、`openai_gateway_service.go`、`usage_log_repo.go`、`setting_handler.go` 都做了巨型文件拆分，本地在这些文件里叠了 2000+ 行自研逻辑。**跟拆分 = 全量重写核心网关**，明确放弃；后果是上游后续补丁的文件路径与本地永久对不上，所有移植必须手工定位。
3. **上游开发强度不可追。** 3 个月 48 个版本，v0.1.156 单版 +85 提交、v0.1.147 +79、v0.1.162 +77。**"一次性 merge 到 upstream/main" 在工程上不可行也不安全**，本计划一律按主题分批吸收。

### 依赖漂移

- Go：本地 `1.26.4` / 上游 `1.26.5`（含 crypto/tls 漏洞 **GO-2026-5856** 修复）
- 新增依赖：`go-webauthn/webauthn`（passkey）、`tiktoken-go/tokenizer`
- 升级：`golang.org/x/crypto 0.51→0.53`、`net 0.55→0.56`、`sync 0.20→0.21`、`term 0.43→0.44`、`image 0.39→0.41`、`golang-jwt/jwt 5.2.2→5.3.1`、`imroc/req 3.57→3.59`、aws-sdk-go-v2 / s3 一系列
- 前端：本地 `axios ^1.16.0`（上游 `^1.18.0`，修 GHSA-gcfj-64vw-6mp9）、本地 `postcss ^8.5.14` 且无 override（上游 override `>=8.5.18`）；`dompurify` 本地反而更新（3.4.2 vs 3.3.1，保持本地）

---

## 二、必须行动的 P0（13 件，去重后）

| # | 事项 | 类型 | 冲突 | 影响 |
|---|---|---|---|---|
| P0-1 | **usage_logs 队列溢出静默丢弃**：扣费成功但账不落库 | fix | medium | 上游实测单用户丢 **49%**；对账永久缺口 |
| P0-2 | **调度快照 Extra 白名单剥掉全部 `quota_*`**：配额耗尽账号继续被调度 | security | low | 直接资损；且 `codex_usage_updated_at` 被剥导致 headroom 权重永久钉死 0.5 |
| P0-3 | **OAuth 401 回写整份 credentials 快照**：回滚并发刷新的 refresh_token | fix | low | 账号被误判永久失效并禁用 |
| P0-4 | **Gemini `/v1beta` 鉴权中间件绕过** IP ACL / 专属分组 / 过期与配额检查 | security | low | 换端点即绕过 Key 的 IP 白名单 |
| P0-5 | **API Key 专属分组运行时授权复核缺失** | security | **high** | 撤销授权后 Key 仍能访问该分组账号池 |
| P0-6 | **`UserRepository.Update` 整行重写**：余额/积分被陈旧快照回滚（lost update） | security | **high** | 本地 4 个自研资金列在裸奔 |
| P0-7 | **余额扣费缺下限守卫**：并发可无限透支成负数 | security | medium | 资损 |
| P0-8 | **退款 pending 无终态化 + 匿名查单未收敛** | fix | medium | 退款状态与发票/钱包脱节；订单可枚举 |
| P0-9 | **CF-Connecting-IP 被列首位可信来源**：客户端可伪造来源 IP | security | high(照搬)/low(定点修) | IP ACL、审计日志、限流桶全部可被绕过 |
| P0-10 | **Go 停留 1.26.4**，未修 crypto/tls GO-2026-5856 | security | low | 纯版本号 |
| P0-11 | **订阅套餐有效期单复数不匹配**：配"1 个月"实际只给 **1 天** | fix | low | 直接资损 + 用户投诉；前端也显示成"1天"所以看不出来 |
| P0-12 | **登录后面板接口零限流 + 反代下限流桶全站共用** | security/perf | medium | 本地生产就是 nginx 反代且**已发生过连接池打满掉线事故** |
| P0-13 | **`user_provider_default_grants` CHECK 漏放 github/google** | fix | low | 首绑默认额度开启后，绑定事务整体 abort |

---

## 三、分批追平计划

原则：**每批独立可发布、可回滚；先止血后重构；高冲突项一律拆成多步，低风险的一半先上。**
每批走既有 pixeldeploy 流程（预检 → 前端构建 → 嵌入式 linux/amd64 二进制 → 压缩上传 → releases 目录 → 冒烟 → `--migrate-only` → 切 current 软链 → 重启 → 双入口验证）。

### 批次 0 — 零代码/极小改动止血 ✅ 已完成（2026-08-02，未发布）

| 项 | 实际做法 | 状态 |
|---|---|---|
| P0-10 Go 工具链 | `backend/go.mod` `1.26.4 → 1.26.5`；`Dockerfile` / `deploy/Dockerfile` 的 `GOLANG_IMAGE` `golang:1.26.2-alpine → golang:1.26.5-alpine`（与上游一致）；`backend-ci.yml`(×2) / `release.yml` / `security-scan.yml` 四处 `go version \| grep -q 'go1.26.4'` → `go1.26.5` | ✅ |
| **依赖 CVE（计划外，govulncheck 挖出）** | 见下表，4 个**可达**漏洞全部修掉 | ✅ |
| 前端 CVE | `axios ^1.16.0 → ^1.18.0`（解析到 **1.19.0**）；`postcss` devDep `^8.5.14 → ^8.5.18` 且 `pnpm.overrides` 加 `postcss@<8.5.18: >=8.5.18`（解析到 **8.5.25**，全部传递依赖被拉齐）。**保留本地 dompurify ^3.4.2 与 js-cookie ^3.0.8**（比上游新，未回退） | ✅ |
| P0-13 迁移 | 新建 `backend/migrations/263_extend_user_provider_default_grants_check.sql`，取值集合 `('email','linuxdo','wechat','oidc','github','google')`，**不含 dingtalk**。迁移用 `//go:embed *.sql` 自动发现，无需注册 | ✅ |
| P0-9 仓库侧 | 删除 `deploy/Caddyfile:37` 的 `header_up CF-Connecting-IP {http.request.header.CF-Connecting-IP}`（与上游一致），改为安全说明注释 | ✅ |
| P0-9 生产侧 | **未执行**（本次不发布）。见下方待办 | ⏸ |

#### govulncheck 发现的可达漏洞（计划外收获）

升 Go 1.26.5 后 stdlib 已干净（`GO-2026-5856` 消失），但扫出 4 个**依赖侧且代码真实可达**的漏洞：

| 漏洞 | 模块 | 原版本 → 修复版本 | 可达路径 |
|---|---|---|---|
| GO-2026-5061 | `golang.org/x/image` | 0.39.0 → **0.43.0** | `compressInlineAvatar`（用户上传头像）、`detectReceiptCodeImage`（收款码识别）→ 构造 WEBP 触发 panic |
| GO-2026-4961 | `golang.org/x/image` | 同上 | 同上（32 位平台大图） |
| GO-2026-5970 | `golang.org/x/text` | 0.37.0 → **0.39.0** | 非法输入导致死循环 |
| GO-2026-5764 | `aws-sdk-go-v2` eventstream / s3 | 1.7.5→**1.7.8** / 1.96.2→**1.97.3** | EventStream Decoder panic |

> 注意：上游 go.mod 的 `x/image` 只到 **0.41.0**，仅修 GO-2026-4961，**没修 GO-2026-5061**。本地取 0.43.0 是超前于上游的，不要在后续同步时被回退。
> 连带升级（x/image、x/text 的传递依赖）：`crypto 0.51→0.53`、`net 0.55→0.56`、`sync 0.20→0.21`、`term 0.43→0.44`、`sys 0.45→0.46`、`mod 0.35→0.37`、`tools 0.44→0.47` —— 恰好与上游版本对齐。

**验收结果**：
- `go build ./...` 全包编译通过
- `go test ./... -count=1` → **43 个包全过，0 失败**（59 个包无测试文件）
- `govulncheck ./...` → **0 vulnerabilities**（升级前为 4 个可达）
- 前端 `vue-tsc -b && vite build` 通过（26.2s）
- 迁移 baseline / Atlas 对齐测试通过，263 不破坏基线

#### 批次 0 遗留待办（发布时执行）

1. 迁移 263 执行前跑 `SELECT DISTINCT provider_type FROM user_provider_default_grants;` 确认无脏值。表极小，无锁风险，可与任意发布同车。
2. ~~本机 `C:` 盘空间不足~~ —— 已于 2026-08-02 清理 Go 构建缓存回收 26GB（4.9G → 31G 可用）。构建缓存会自动重建，首次构建变慢属正常。

---

### ⚠️ 生产客户端 IP 现状实测（2026-08-02，只读核查，未改动）

对生产环境做了只读核查，结论**修正了 P0-9 / P0-12 的风险判断与执行顺序**：

| 核查项 | 实际状态 |
|---|---|
| 生产 nginx（宝塔面板 `/www/server/panel/vhost/nginx/ai-pixel.online.conf`） | 只设 `X-Real-IP $remote_addr`、`X-Forwarded-For $proxy_add_x_forwarded_for`、`X-Forwarded-Proto`、`Host`；**未清理 `CF-Connecting-IP`**，客户端自带该头会被原样透传给后端 |
| 生产 `config.yaml`（`DATA_DIR=/var/lib/sub2api`） | `server:` 段只有 `host` / `port` / `mode: release`，**无 `trusted_proxies`** |
| 代码 `configureClientIPResolution`（`http.go:97`） | `len(TrustedProxies)==0` → `SetTrustedProxies(nil)` → gin **完全忽略所有转发头** |
| `ip.GetSecurityClientIP`（`pkg/ip/ip.go:29`） | 仅 `normalizeIP(c.ClientIP())`，无旁路 |

**当前不可被 CF-Connecting-IP 伪造打穿**，但属于"侥幸安全"，且代价严重：转发头被整体忽略后，`c.ClientIP()` 拿到的是 nginx 的 TCP 对端（回环地址），导致——

- **API Key 的 IP 白名单/黑名单形同虚设**（拿回环地址比对用户配置的真实 IP）
- 审计日志与会话绑定记录的来源 IP 全站同一个值
- **限流分桶全站坍缩成一个桶** —— 即 P0-12 的后半部分，且影响面比原描述更广（不止限流，是所有 IP 相关功能）

**🔒 强制执行顺序（写死，不得调换）：**

> **A-7（把 `CF-Connecting-IP` 移出 `standardForwardedClientIPHeaders` 默认列表）+ nginx 侧 `proxy_set_header CF-Connecting-IP "";`
> 必须先于或同时于「给 `config.yaml` 加 `server.trusted_proxies`」落地。**
>
> 原因：一旦先加了 `trusted_proxies`，gin 开始信任转发头，而 `CF-Connecting-IP` 在默认列表里排第一、nginx 又不清理它 —— 伪造来源 IP 会**立刻从"不可利用"变成"可利用"**，等于亲手打开一个当前并不存在的漏洞。
>
> 这条约束直接影响 B-3（面板限流）：B-3 第一步"把 `c.ClientIP()` 换成安全客户端 IP 解析"要真正生效必然需要 `trusted_proxies`，所以 **B-3 必须排在 A-7 之后**。

**待确认（运行时验证被权限策略拦截，未执行）：**
```sql
SELECT client_ip, count(*) FROM usage_logs
WHERE created_at > now() - interval '2 hours' GROUP BY 1 ORDER BY 2 DESC LIMIT 5;
```
若结果确实全是同一个回环地址，即坐实上述推导。

---

### 批次 A — P0 资损与正确性（改动面小、冲突低）

**A-1 usage_logs 丢弃（P0-1）** — 移植面比想象小，worker 池那一半（`config.go:2367` overflow_policy 默认 sync）已在本地。
- `backend/internal/repository/usage_log_repo.go:336-338`（`CreateBestEffort`）与 `:459`（`createBatched`）删除 `default:` 立即丢弃分支，改 `select { case ch<-req: ; case <-ctx.Done(): }`
- `backend/internal/service/gateway_service.go:10019-10021` 把 `if IsUsageLogCreateDropped(err) { return }` 改为回落 `repo.Create` 同步兜底；`usageCtx` 已耗尽时用 `detachedBillingContext` 另开窗口
- `:482/:492` 的 `ch != nil` 检查移进各自 `sync.Once` 内部（消数据竞争）
- **必做**：同步改 `gateway_record_usage_test.go:394` 与 `usage_log_repo_integration_test.go:316` —— 这两处正断言当前的丢弃行为
- 风险已复核可控：`detachedBillingContext` 用 `context.WithoutCancel` + `postUsageBillingTimeout=15s`，背压窗口天然有界、不随客户端断连塌缩
- 验证：生产对比 `usage_logs` 行数 vs 计费流水条数

**A-2 调度快照配额白名单（P0-2）** — `backend/internal/repository/scheduler_cache.go` 的 `filterSchedulerExtra`，**做并集不要整段替换**（本地有上游没有的 `codex_5h_limit_percent`/`codex_7d_limit_percent`/`GrokMediaEligibleExtraKey`/`grok_billing_snapshot`）。追加 16 个键：`quota_limit`、`quota_used`、`quota_daily_limit`、`quota_daily_used`、`quota_daily_start`、`quota_daily_reset_mode`、`quota_daily_reset_hour`、`quota_weekly_limit`、`quota_weekly_used`、`quota_weekly_start`、`quota_weekly_reset_mode`、`quota_weekly_reset_day`、`quota_weekly_reset_hour`、`quota_reset_timezone`、`codex_usage_updated_at`、`model_rate_limits`。**不要加** `upstream_billing_probe` 与 `auto_pause_*_threshold`（本地无实现）。
> 上线后必须 flush `sched:meta:*` 或触发全量重建，否则存量快照仍是旧载荷。补单测断言 `quota_daily_limit` 与 `codex_usage_updated_at` 能穿过 `buildSchedulerMetadataAccount`。

**A-3 OAuth 401 credentials 回写（P0-3）** — 删 `backend/internal/service/ratelimit_service.go:256-263` 整块（"设置 expires_at 为当前时间" → `persistAccountCredentials`）。保留 `:251-255` 的 `InvalidateToken`、`:265+` 的 `SetTempUnschedulable`、本地自研的 Antigravity 例外（`:249`）与 `OAuth401CooldownMinutes`（`:271-274`）。建议与"缺失 refresh_token 永久禁用"（批次 D）合并改动，两者在同一 case 块内。
> 注意：memory 记录的"自有账号被系统写入锁死"已由迁移 262 闭环，**本条是另一个独立写入点**，需单独验证 owner 账号不受影响。

**A-4 Gemini `/v1beta` 鉴权补齐（P0-4）** — 只取 `29a5fcd25` 对 `api_key_auth_google.go` 的 hunk，插到本地 `:45-56` 区间。四处必须改写：① `IsActive` 按本地主中间件口径放宽（排除 `StatusAPIKeyExpired`/`StatusAPIKeyQuotaExhausted`）；② IP ACL 用 `ip.GetSecurityClientIP(c)`（本地单参签名），错误文案保持本地模糊的 `Access denied`；③ 删掉上游所有 `MarkIngressRejected` 行（本地无此机制）；④ 专属分组校验依赖 P0-5，**排在 B 批之后再补**。

**A-5 套餐有效期单复数（P0-11）** — 后端优先（资损点在后端）。
- `backend/internal/service/payment_service.go:424+` 补 `validityUnitWeeks="weeks"` / `validityUnitMonths="months"`，`:430/:432` 改 `case validityUnitWeek, validityUnitWeeks:` 与 `case validityUnitMonth, validityUnitMonths:`（本地函数与上游同构，可近似 cherry-pick `147c1879d`），带上 `payment_order_result_test.go` 的 28 行用例
- 前端新建 `frontend/src/components/payment/validity.ts` 的 `planValiditySuffix`，`SubscriptionPlanCard.vue:155-160` 与 `PaymentView.vue:926-928` 共用（照 `a6ecc202f`），i18n 补 `payment.months` / `payment.weeks`
- **上线前必查存量**：`SELECT id,name,validity_days,validity_unit FROM subscription_plans WHERE validity_unit IN ('weeks','months')`，再比对这些套餐已售订单的 `subscription_days`，确认有多少用户被少给了周期。需要人工补齐订阅时长时，复用 waiver 补偿那套记账手法打标记

**A-6 余额下限守卫（P0-7）** — `usage_billing_repo.go:425` 改签名为 `(newBalance float64, sufficient bool, err error)`，先执行带 `AND balance >= $1` 的 UPDATE，`sql.ErrNoRows` 时回落无条件 UPDATE 并返回 `sufficient=false`；同步改 `deductUsageBillingWallet`（`:443`，本地自研双钱包 preferPoints 分支）；`UsageBillingResult` 加 `BalanceOverdrafted bool`；`config.go` `BillingConfig` 加 `MinimumBalanceReserve`（默认 `0.000001`）+ Validate 非负；`billing_cache_service.go:805` 的 `balance <= 0` 换阈值判定。建议打一条 ops 事件便于对账。

**A-7 P0-9 代码侧收尾** — `backend/internal/server/http.go:90-94` 把 `CF-Connecting-IP` 移出默认 `RemoteIPHeaders`，改为仅当运维在 `security.forwarded_client_ip_headers` 显式配置时才加入（复用 `config.go:805-830` 的 `NormalizeForwardedClientIPHeaders`）。**不要照搬上游整套 pkg/ip 重做**（会废掉本地自研能力）。补一条"trusted_proxies 非空 + 伪造 CF 头"回归测试。

---

### 批次 B — P0 高冲突项（必须分步，单独发布）

**B-1 users 资金列保护（P0-6）** — 分两批，**不要直接 cherry-pick `86fb4781f`**（本地比上游多 4 个自研金额列 `points_balance` / `load_factor_credits_balance` / `load_factor_credits_used_total` / `prefer_points_billing`，还多一条 `UpdateWithAdminGovernanceGuard` 路径）。
- 第一批（低风险先上）：把 `updateOp` 里 `SetBalance`/`SetPointsBalance`/`SetLoadFactorCreditsBalance`/`SetLoadFactorCreditsUsedTotal`/`SetTotalRecharged` 五个调用摘掉，让 `Update` 永不碰钱列。**落地前逐一 grep 16 个调用方**确认无人靠 `Update` 改余额，重点看 `content_moderation.go:1340/1996` 与 `admin_service.go:1070`
- 第二批（可选）：引入本地版 `UserUpdateFields` 列掩码，字段集含 4 个自研金额列与 `prefer_points_billing`，让 `UpdateWithAdminGovernanceGuard` 复用同一掩码；再覆盖 `api_keys`（配额耗尽标记只写 status）
- 先写集成测试固化"陈旧快照 Update 不得回滚并发 UpdateBalance"

**B-2 专属分组运行时授权复核（P0-5）** — **四步缺一不可，漏第 1-2 步会自造一次全站 403 故障**。
1. `service/api_key_auth_cache.go`：快照 `User` 加 `AllowedGroups []int64`、`Group` 加 `IsExclusive bool`
2. `service/api_key_auth_cache_impl.go`：两处装配点填这两个字段，`apiKeyAuthSnapshotVersion` **15 → 16**（强制旧快照失效）
3. 把上游 `api_key_auth.go` 的 `validateAPIKeyGroupAvailable` / `validateAPIKeyGroupAllowed` / `abortIfAPIKeyGroup*` 抄进本地同名文件，在 `setGroupContext` 之前调用，删掉 `MarkIngressRejected` 行
4. `admin_service.go:3038/3049` 与 `user_private_group_service.go:208` 的授权增删处补鉴权缓存失效
- **上线前必测三条路径不被误拒**：账号共享房间成员的 Key、`user_private_group` 私有分组的 Key、订阅型分组的 Key（上游对订阅型直接放行）
- 本条完成后回头补 A-4 的第 ③ 项

**B-3 面板限流（P0-12）** — 两步可拆开上线。**⚠️ 前置：必须在 A-7 之后**（见上方「强制执行顺序」——本项需要 `trusted_proxies` 才能生效，而在 CF 头护栏落地前加 `trusted_proxies` 会打开伪造来源 IP 的口子）。
- 第一步（改动最小、收益立竿见影）：`middleware/rate_limiter.go` 把 `c.ClientIP()` 换成与审计/ACL 同源的安全客户端 IP 解析，并尊重"信任反代转发 IP"开关；开关关闭时行为与现状完全一致
- 第二步：移植 `server/middleware/panel_rate_limit.go` + `service/setting_panel_rate_limit.go` + admin 设置接口，按用户 ID 限流（global / heavy 两档）。**本地需额外把账号广场结算、商城下单、发票生成纳入 heavy 档**，并确认计费 worker / 后台任务走内部调用不经中间件（`detached_usage_drain` 等链路不能被误限流）。配置读取必须走 `atomic.Value` + singleflight 缓存（照抄上游），否则限流中间件自己反而增加 DB 压力。面板档 Redis 故障 fail-open，auth 档保持 fail-close
- 前端 `SettingsView` 安全 tab 加配置卡片。上线后用本地 ops 看板观察 429 率，阈值先宽后收

**B-4 退款生命周期（P0-8）** — 按 `93a3bf307` → `7316d8302` 顺序手工移植。`payment/types.go` 加 `OrderStatusRefundPending` + `RefundQueryProvider`；Stripe/微信/Airwallex 各实现 `QueryRefund`；`payment_refund.go` 加 pending 落库 + 回查终态化，**务必把发票冲红与钱包退回挪到"终态 SUCCESS 后"执行**（本地接了自研发票冲红与双钱包退回，直接照搬会让发票/钱包状态与订单状态脱节）；`payment_order_lifecycle.go` 状态机加 `REFUND_PENDING` 合法转移；管理端加终态化路由 + 前端状态徽标。无迁移（order status 是字符串列）。

---

### 批次 C — P1 网关正确性与指纹（10 件）

按价值排序，**前两件对"卖号/共享账号"平台价值最高**：

1. **Claude Code dateline 隐写指纹未抹除**（security，冲突 low）—— CC 客户端检测到非官方 base URL 时，会把 `Today's date is YYYY-MM-DD.` 里的撇号换成 4 种码点之一并可能改日期分隔符，构成 **3 bit 隐写信号**，Anthropic 可据此识别请求经过中转 → 直接对应封号资损。移植 `59e9356c5`：整包复制 `backend/internal/pkg/anthropicfp/{dateline.go,dateline_test.go}`（无本地依赖），在 `gateway_service.go` 的 OAuth/setup-token 请求体改写分支（`normalizeClaudeOAuthRequestBody` 附近，约 1490 与 5350 两处）调用 `NormalizeDateline`；API Key 账号不动；加系统设置开关 `enable_client_dateline_normalization` 默认开启
2. **Codex 合成 instructions 是占位符**（security，冲突 low）—— 本地 `openai_codex_transform.go:113` 是 `"You are a helpful coding assistant."`，真实 Codex CLI 发的是数万字符 base prompt，**上游一眼可辨**。移植 `5e6effd79`+`00d68ff6d`+`709cf6185` 的 pkg/openai 部分：取上游 `instructions.txt`（已刷新）与 `instructions_gpt5_1/5_2/5_5.txt`，`constants.go` 加 `//go:embed` 与 `CodexBaseInstructionsForModel`（未覆盖版本回退到最新）
3. **SSE 内 `rate_limit` 未归一为 429** —— 上游 Responses 会在 HTTP 200 的 `response.failed` 事件里带 `code=rate_limit_exceeded`，本地 failover 状态码在 `openai_gateway_service.go:4733/4759` 硬编码 502，导致 429 同账号重试与 failover 全不生效。移植 `85a27fae3`+`7d3bf86e5`，本地已有 `openAIStreamFailedEventSemanticStatus`（`openai_gateway_response_failed.go:56`）可复用
4. **透传账号残留 model_mapping 被排除出候选**（`83b368553`）—— `account.go:1232` 直接查 model_mapping，passthrough 未短路 → `404 no available account`
5. **池模式可重试状态仍写账号+模型瞬时冷却**（`521db6869`）—— `openai_account_runtime_block.go:143` 无 `poolModeRetryable` 排除，会在同账号重试预算用完前先冷却掉账号，架空 `HandleFailoverErrorWithRetryLimit`
6. **Claude Code 校验器拒绝官方辅助请求**（`2ef124629` 等）—— `claude_code_validator.go` 缺 count_tokens 放行、缺安全监视器分类器识别、缺 `x-anthropic-billing-header` 计费块识别，开启 CC 校验的账号会拒掉真实 CLI
7. **WS 直通按 session 首模型计费**（冲突 **high**）—— `openai_ws_v2_passthrough_adapter.go:809-813` 用 `relayResult.RequestModel` 做 Model/UpstreamModel，session 中途换模型全程按首模型计费
8. **WS 下行写超时挂在 relayCtx**（`21aacde0b`）—— `openai_ws_v2/passthrough_relay.go:264` 仍是 `relayCtx`，外部取消冲掉 close frame，客户端只见裸 EOF
9. **Responses→Anthropic 转换丢弃 instructions**、不识别 developer 角色、工具配对错乱
10. **上游静默拒绝（200 但空流）不触发 failover** —— 客户端收到空响应
11. **`usage_logs.upstream_model` 比较基准错误**（`1f45c99de`+`be65c713f`）—— `gateway_service.go:10483` 与 `openai_gateway_service.go:7826` 仍是 `optionalNonEqualStringPtr(result.UpstreamModel, result.Model)`，渠道映射后的实际上游模型被丢弃

---

### 批次 D — P1 调度与账号池稳定性（9 件）

1. **缺失 refresh_token 的 OAuth 账号不被剔除** —— 反复选中导致持续 502（与 A-3 同 case 块，合并改）
2. **`UpdateLastUsed` 是整份账号 JSON 的读-改-写**（冲突 **high**）—— 覆盖并发写入的其它字段
3. **`BlockAccountScheduling` 无 CAS 与代际保护** —— 短冷却覆盖长冷却
4. **空 model_mapping 的 OAuth 账号吸收全部模型** + passthrough 未短路
5. **分组被停用/删除后，绑定该分组的 API Key 仍可继续用**（security）
6. **分组账号计数口径错误** —— 含软删账号；可用数把限流账号算进去
7. **`ListWithFilters` 的 Count 未 Clone** —— 分页总数被谓词污染
8. **Codex 配额快照陈旧无自愈** —— 账号可被误暂停长达 5h/7d
9. **池模式吞掉管理员显式配置的临时不可调度规则**

---

### 批次 E — P1 运维护栏与 DB（含 5 条新迁移）

| 项 | 说明 | 迁移 |
|---|---|---|
| 无效鉴权爆破限流 + 鉴权回源并发上限 | **直接护住 DB 连接池** —— 对应本地已发生过的打满掉线事故 | 无 |
| ops 高级设置在错误日志热路径每次直查 DB | 且缺失时每次还写一次 —— 与本地刚做的 ops 日志收口互补 | 无 |
| 入口拒绝日志聚合表 | 未鉴权扫描不再一请求一行 `ops_error_logs` —— 对本地 611GB 库直接减负 | 需要 |
| 鉴权缓存失效改 DB outbox 持久投递（冲突 high） | Redis pub/sub 丢消息则已吊销的 Key 仍可用 | 需要 |
| ops 错误日志队列缺内存预算 | 突发时可被大响应体撑爆 | 无 |
| 管理员操作审计日志 append-only `audit_logs` | 上游 180 | 需要 |
| TTFT 分位数被非流式请求稀释 | 上游 145 `ops_metrics_ttft_sample_count` —— 对应 memory 里的 TTFT 事件排查 | 需要 |
| `account_groups` 调度复合索引缺失 | 上游 150 | 需要 |
| 注册邮箱别名去重（plus 地址 / Gmail 点号） | 可批量刷注册赠额；上游 190 + `bc3acd6e2`（含根点绕过 / 误拒 / 无界扫描 / 并发竞态四个后续修复，**要一起跟**） | 需要 |
| 密文落库前未校验 `EncryptionKeyConfigured` | 重启后永久无法解密 | 无 |
| notx 唯一索引缺 invalid 自愈登记 | 上游 `60cf89ae2` 通用化 —— 对应 memory 里的 PG 排序规则索引失效事故，**这条直接补上本地那个坑** | 无 |

> 迁移编号建议：263（批次 0）之后，E 批占 **264–269**。所有涉及大表的一律用 `_notx` 并在低峰执行；`ops_error_logs`/`usage_logs` 是百 GB 级 TOAST 膨胀表，加列前先确认 `pg_total_relation_size`。

---

### 批次 F — P1 计费与前端细项（10 件）

- hosted `image_generation` 工具的图片 token 在网关侧**全部漏计费**
- fallback 定价告警每请求刷屏，直灌 `ops_system_logs`（与本地 ops 收口冲突面为零，纯收益）
- 兜底定价缺 35 个模型（GLM/Kimi/MiniMax/DeepSeek/doubao 等）
- 订阅配额窗口与订阅周期错位 + 日卡不是一次性配额 + 剩余天数向下取整
- 支付宝 `page.pay` 跳转 URL 被当成二维码内容返回
- 支付看板把多币种订单加成一个数并打美元符号
- 优惠码 / Ops 时间选择器 UTC 与本地时区往返错位
- **Stripe 弹窗轮询读错 localStorage key，订单状态查询完全无鉴权**（security）
- Token 趋势图缓存命中率分母算错，OpenAI 模型恒显示 100%
- 验证码与密码重置邮件正文未 HTML 转义站点名与重置链接

---

## 四、上游新增、本地完全没有的功能（78 项）

按"要不要做"分三档。**没有任何一项是 P0/P1 必须**，全部属于产品选择。

### 档 1 — 建议做（对 Pixel 业务有直接价值）

| 功能 | 上游 | 价值 | 代价 |
|---|---|---|---|
| **生图结果落对象存储（S3 兼容）** | ImageStorage 抽象 + AWS S3/R2/OSS/MinIO | 把 b64_json 转存后只回短链，**避免大 base64 落 Redis / 撑爆用量日志** —— 直接对应本地 DB 空间治理 | 中；需 S3 配置，无迁移 |
| **分组级自定义 `/v1/models` 展示列表** | `groups.models_list_config` JSONB（上游 143） | 不同分组暴露不同模型菜单，多租户/子站场景刚需；明确只影响展示不参与调度 | 低；1 条迁移 |
| **分组级 reasoning effort 上限与映射** | `groups.max_reasoning_effort` + `reasoning_effort_mappings`（上游 185） | 直接控成本 | 低；1 条迁移 |
| **渠道监控 OpenAI api_mode 拆分（Responses 协议）** | 上游 138+139 | 本地监控只认 chat/completions，Responses 账号监控是瞎的 | 中；2 条迁移 |
| **代理有效期与失败回退链** | 上游 149 + `af19d4432` | 本地代理到期只能人肉发现；**与本地自研的 proxy owner/归属强耦合，冲突 high** | 高 |
| **渠道模型定价一键同步最新模型** | 前端 | 新模型上线漏配定价 = 按默认价计费的资损口子 | 低 |
| **`usage_logs.session_id`** | 上游 187 | 按会话聚合排障；本地账号广场排障场景很需要 | 低；1 条迁移 |
| **兑换码有效期 + 批量更新 + 邀请码误用修复** | 上游 137 | 最后一项是 bug（邀请码走普通兑换接口报 unsupported） | 低；1 条迁移 |
| **订阅到期提醒邮件 + 管理端开关** | 上游 141 | 续费转化 | 低；1 条迁移 |
| **用户端失败请求列表与错误分类** | 前端 | 本地只做了管理端一半 | 中 |
| **Ops 系统日志按 api_key 过滤** | 上游 154+155 | 本地刚收紧日志保留期，按 key 快速定位价值更高 | 低；2 条迁移 |

### 档 2 — 可做可不做

模型广场（公开定价橱窗）、Passkey/WebAuthn 登录、user×platform USD 配额、支付宝移动端 precreate 深链、异步生图任务（提交+轮询）、`/v1/embeddings` 端点、上游计费倍率探测、Codex PAT 认证、邮件模板编辑器、自定义 Markdown 页面、订阅套餐按币种定价、高峰时段倍率+Key 计费倍率自省、EasyPay 自定义支付方式、用量记录 IP 归属地、公告预览、Select/GroupSelector 自动搜索、`CONFIG_FILE` 环境变量、`SKIP_SETUP`、Redis ACL username、可选 JWT 中间件、管理端用量按 request_id 过滤、已删除 API key 审计。

### 档 3 — 明确不做

| 功能 | 原因 |
|---|---|
| **批量生图（Batch Image）全链路** | 3 张 ent 表 + 12 条迁移 + 余额冻结 + GCS/Vertex Batch API + worker 结算，冲突 high，与 Pixel 业务无关 |
| **Composite 组合平台分组 + 模型路由注册表** | 一个分组跨多平台，与本地分组/账号广场模型强冲突，冲突 high |
| **Spark 链接型影子账号（shadow parent）** | 需要独立配额窗口 + 母账号登录态复用，冲突 high |
| **OpenAI Live 网关（WebSocket 实时）+ macOS DeviceCheck attestation** | 上游特有，本地无场景 |
| **DingTalk OAuth 登录** | 本地已有 GitHub/Google/LinuxDo/微信/OIDC 五种 |
| **Ollama Cloud 官方用量自动刷新** | 无场景 |
| **管理员部署合规承诺确认闸门** | 上游治理需要，本地不适用 |
| **securityaudit 提示词审计整套模块** | 本地 cyber preflight 已覆盖同一问题域（`superseded`）；只缺"按模型生效"与"审计代理"两个点，值得单独补，但不引入上游整套 |

---

## 五、明确不跟的上游改动（保稳定）

| 上游改动 | 不跟的理由 |
|---|---|
| `gateway_service.go` / `openai_gateway_service.go` 巨型文件拆分 | 本地在这两个文件里有账号广场路由、房间席位、计费拦截、grok 分支、cyber policy、clean relay 等大量自研，跟拆分等于重做一次大合并 |
| `usage_log_repo.go` / `setting_handler.go` 拆分 | 同上；且 `setting_handler` 本地因自研设置项（广场/席位/商城/邀请/发票/工单/活动/子站）已大幅分叉，机械拆分会把自研代码切碎 |
| 移除 Ops 重试/回放存储（上游 136） | 会打断 `ops_repo` 5 处 SQL + 管理端重试接口 + 清理分支；且本地已在 `5850e5565` 把 `request_body` 上限 256KB→16KB 限流解决（生产实测 p50≈1.3KB / p95≈124KB / 均值 20KB） |
| LinuxDO 无需邮箱验证时直登 | 会跳过本地 pending 会话 → 砸掉登录协议版本确认、promo code 捕获、账号选择三项业务逻辑 |
| Grok/xAI 上游批量修复整包 | 本地 grok 层已与账号广场共享模式、凭证清洗、迁移 262 深度耦合。**例外**：SSE 计费 ping 帧过滤 + 过滤缓冲上限（`baaae8e12`/`30967d5d9`）是新增独立文件 + 一处 `resp.Body` 包装，冲突面小，**值得单独跟** |
| 订阅撤销的跨实例 L1 缓存失效 | 本地 `RevokeSubscription` 已自研；且不该引入上游新 pub/sub 通道，应复用已有 `ClusterCacheCoordinator` |
| 移动端布局与溢出修复批次 | 上游 tailwind 类名与 Pixel 皮肤不对应，照搬必然破相；本地浮层方案比上游完善，引入 `floatingPanel.ts` 是退化 |
| 上游 pkg/ip 整套客户端 IP 重做 | 会废掉本地自研的 `security.forwarded_client_ip_headers`，改用定点修复（见 A-7） |
| 请求体内存治理 `RequestBodyRef` 重构 | 本地自研方案已覆盖（`b7237e1ec`） |
| 通用 leader lock 抽象 / 后台任务选主锁 | 本地 `ClusterTaskExecutor` 已覆盖 |
| 错误状态账号强制不可调度 | 本地用 `status` + `error_since` 覆盖，**方案更优，不要回退** |
| 上游 token 刷新候选查询重构 | 本地方案更优，不要回退 |
| 流中断保留已观测 usage | 本地已有 `BillableStreamUsageError` 全套（`gateway_service.go:593-615/645`，5908/5925/6204/6915 四处），**零动作** |
| DB 连接池参数 / conn_max_idle_time | 本地已按生产事故调优（PG 400 + 应用池 350） |
| 分组订阅高峰时段倍率 | 本地自研排期方案已覆盖 |

---

## 六、迁移重编号台账

上游 134–191 共 58 条，本地对应编号已被自研占用。**必须逐条重编到 263+**。已判定：

- **必须跟**（P0/P1）：140（`user_provider_default_grants` CHECK）、145（TTFT 采样计数）、150（`account_groups` 复合索引）、180（`audit_logs`）、183（入口拒绝聚合）、184+186（鉴权缓存 outbox）、190（邮箱别名去重）
- **建议跟**（档 1 功能）：137（兑换码 expires_at）、138+139（监控 api_mode + 模板）、141（订阅到期提醒开关）、143（models_list_config）、154+155（ops_system_logs api_key_id）、185（reasoning effort）、187（session_id）
- **可选**（档 2）：142+157（user_platform_quotas）、149（代理有效期）、177（套餐币种）、186（支付宝深链）、191（passkey）
- **不跟**：136（移除 ops 重试回放）、154/154a（Spark 影子账号）、158（高峰倍率 / Grok 媒体回填，本地已覆盖）、159–169+160（批量生图 12 条）、172（composite 路由）、173+188+189（request_type 扩容 / allow_live）、174+175（长上下文计费，本地已覆盖）、181+182（prompt 内容审计，本地 cyber preflight 覆盖）

**迁移执行纪律**（沿用本地既有教训）：
- 所有新迁移从 **263** 起顺序编号，一次发布内不跳号
- 涉及 `usage_logs` / `ops_error_logs` / `ops_system_logs` 的加列一律 `_notx` + 低峰执行，加索引前先看 `pg_total_relation_size`
- 应用一律走 `--migrate-only` 显式执行，不依赖启动自动迁移
- 文本列上的唯一索引记得登记 invalid 自愈（批次 E 那条）—— 对应 glibc 排序规则漂移事故

---

## 七、长效跟进机制（本次追平的真正目的）

### 每次上游发版的固定动作（建议 30 分钟内完成）

```bash
git fetch upstream --tags
LAST=$(cat .upstream-sync)          # 记录上次已评审到的上游 commit
git log --no-merges --format='%h|%s' $LAST..upstream/main
```

分三桶处理：

1. **security / fix 且落在本地也有的文件** → 当期评审，能跟就跟
2. **feat** → 记进 backlog，季度决策一次，默认不跟
3. **refactor / 文件拆分 / chore / i18n / 上游品牌** → 默认不跟，只记录路径漂移

评审完把 `upstream/main` 的 sha 写回 `.upstream-sync` 并提交，这样"落后多少"永远是一条命令能查的。

### 配套建议

- 在仓库根加 `.upstream-sync` 文件（本次追平完成后写入 `b74024c78`）
- 在 `docs/` 维护一份 `UPSTREAM_SKIPPED.md`，把"明确不跟"的决定与理由固化下来，避免下次重新纠结（本文第五节可直接迁过去）
- 每次评审只看 diff 的 `backend/internal/{service,handler,repository,server}` 与 `backend/migrations`，其余目录默认跳过
- **不要再攒三个月**。按上游当前节奏（每周 2–3 个版本、每版 20–85 提交），两周不看就会重新进入"需要开工作流盘点"的量级

---

## 八、执行顺序建议

```
批次 0（止血）          → 发 1.2.31
批次 A（P0 低冲突）      → 发 1.2.32
批次 B-1（资金列第一批） → 发 1.2.33   ← 单独发，便于回滚
批次 B-2（专属分组授权） → 发 1.2.34   ← 单独发，上线后盯 403 率
批次 B-3 + B-4          → 发 1.2.35
批次 C（网关正确性/指纹）→ 发 1.2.36
批次 D（调度稳定性）     → 发 1.2.37
批次 E（运维护栏 + 迁移）→ 发 1.2.38
批次 F（计费/前端细项）  → 发 1.2.39
──────────── 至此 P0/P1 清零，写入 .upstream-sync ────────────
档 1 功能按业务优先级插入后续版本
```

每批发布后在生产观察 24h 再进下一批。**批次 B 的三项互相独立，任何一项出问题不影响其余两项回滚。**
